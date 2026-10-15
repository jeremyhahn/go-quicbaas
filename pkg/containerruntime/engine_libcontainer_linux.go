// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-trusted-platform.
//
// go-trusted-platform is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

//go:build linux && cgo

package containerruntime

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"syscall"

	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/specconv"
	"github.com/opencontainers/runc/libcontainer/utils"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

// libcontainerEngine is the containerEngine backed by runc's libcontainer,
// running in this process. Container processes are started by re-executing
// the running program as "<argv0> init", which RunContainerInit handles.
type libcontainerEngine struct {
	// root is libcontainer's state directory.
	root string

	mu         sync.Mutex
	containers map[string]*libcontainer.Container
}

// newLibcontainerEngine returns an engine keeping its state under root.
func newLibcontainerEngine(root string) *libcontainerEngine {
	return &libcontainerEngine{
		root:       root,
		containers: make(map[string]*libcontainer.Container),
	}
}

// Create converts the OCI configuration to a libcontainer one and registers
// the container.
func (e *libcontainerEngine) Create(id string, oci *ociConfig) error {
	spec, err := toRuntimeSpec(oci)
	if err != nil {
		return err
	}
	config, err := specconv.CreateLibcontainerConfig(&specconv.CreateOpts{
		CgroupName: id,
		Spec:       spec,
	})
	if err != nil {
		return err
	}

	container, err := libcontainer.Create(e.root, id, config)
	if err != nil {
		if errors.Is(err, libcontainer.ErrExist) {
			return errors.Join(ErrContainerAlreadyExists, err)
		}
		return err
	}

	e.mu.Lock()
	e.containers[id] = container
	e.mu.Unlock()
	return nil
}

// Start runs the container's init process.
func (e *libcontainerEngine) Start(id string, init processSpec) (containerProcess, error) {
	return e.run(id, init, true)
}

// Exec runs an extra process in the running container.
func (e *libcontainerEngine) Exec(id string, process processSpec) (containerProcess, error) {
	return e.run(id, process, false)
}

// Destroy removes a stopped container's state and cgroup.
func (e *libcontainerEngine) Destroy(id string) error {
	container, err := e.container(id)
	if err != nil {
		return err
	}
	if err := container.Destroy(); err != nil {
		return err
	}

	e.mu.Lock()
	delete(e.containers, id)
	e.mu.Unlock()
	return nil
}

// container returns a registered container.
func (e *libcontainerEngine) container(id string) (*libcontainer.Container, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	container, ok := e.containers[id]
	if !ok {
		return nil, ErrContainerNotFound
	}
	return container, nil
}

// run starts spec in the container: as its init process, or as an extra
// process joining a running container's namespaces.
func (e *libcontainerEngine) run(id string, spec processSpec, init bool) (containerProcess, error) {
	container, err := e.container(id)
	if err != nil {
		return nil, err
	}

	process := toLibcontainerProcess(spec, init)
	if !spec.TTY {
		if err := container.Run(process); err != nil {
			return nil, err
		}
		return &libcontainerProcess{process: process}, nil
	}

	parent, child, err := utils.NewSockPair("console")
	if err != nil {
		return nil, err
	}
	process.ConsoleSocket = child

	runErr := container.Run(process)
	childErr := child.Close()
	if runErr != nil {
		return nil, errors.Join(runErr, childErr, parent.Close())
	}

	console, recvErr := receiveConsole(parent)
	if err := errors.Join(recvErr, childErr, parent.Close()); err != nil {
		return nil, errors.Join(err, killProcess(process), closeIfSet(console))
	}
	return &libcontainerProcess{process: process, console: console}, nil
}

// toLibcontainerProcess converts a processSpec. A TTY process gets its stdio
// from the console, so its writers are left unset.
func toLibcontainerProcess(spec processSpec, init bool) *libcontainer.Process {
	process := &libcontainer.Process{
		Args: spec.Args,
		Env:  spec.Env,
		Cwd:  spec.Cwd,
		UID:  int(spec.User.UID),
		GID:  int(spec.User.GID),
		Init: init,
	}
	for _, gid := range spec.User.AdditionalGids {
		process.AdditionalGroups = append(process.AdditionalGroups, int(gid))
	}
	if !spec.TTY {
		process.Stdout = spec.Stdout
		process.Stderr = spec.Stderr
	}
	return process
}

// toRuntimeSpec converts the package's OCI configuration to the runtime-spec
// type specconv reads. The two share the config.json wire format.
func toRuntimeSpec(oci *ociConfig) (*specs.Spec, error) {
	data, err := json.Marshal(oci)
	if err != nil {
		return nil, err
	}
	spec := &specs.Spec{}
	if err := json.Unmarshal(data, spec); err != nil {
		return nil, err
	}
	return spec, nil
}

// receiveConsole reads the pseudo-terminal master the container's init sends
// over the console socket.
func receiveConsole(socket *os.File) (*os.File, error) {
	conn, err := socket.SyscallConn()
	if err != nil {
		return nil, err
	}

	name := make([]byte, 4096)
	oob := make([]byte, unix.CmsgSpace(4))
	var n, oobn int
	var recvErr error
	if err := conn.Read(func(fd uintptr) bool {
		n, oobn, _, _, recvErr = unix.Recvmsg(int(fd), name, oob, unix.MSG_CMSG_CLOEXEC)
		return !errors.Is(recvErr, unix.EAGAIN)
	}); err != nil {
		return nil, err
	}
	if recvErr != nil {
		return nil, recvErr
	}

	messages, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return nil, err
	}
	if len(messages) != 1 {
		return nil, ErrConsoleNotReceived
	}
	fds, err := unix.ParseUnixRights(&messages[0])
	if err != nil {
		return nil, err
	}
	if len(fds) != 1 {
		for _, fd := range fds {
			if closeErr := unix.Close(fd); closeErr != nil {
				return nil, errors.Join(ErrConsoleNotReceived, closeErr)
			}
		}
		return nil, ErrConsoleNotReceived
	}
	return os.NewFile(uintptr(fds[0]), string(name[:n])), nil
}

// killProcess kills a process whose console could not be set up.
func killProcess(process *libcontainer.Process) error {
	if err := process.Signal(unix.SIGKILL); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	_, err := process.Wait()
	if err != nil && !isExitStatus(err) {
		return err
	}
	return nil
}

// closeIfSet closes f unless it is nil.
func closeIfSet(f *os.File) error {
	if f == nil {
		return nil
	}
	return f.Close()
}

// libcontainerProcess adapts a libcontainer process to containerProcess.
type libcontainerProcess struct {
	process *libcontainer.Process
	console *os.File
}

// Pid returns the process ID, or 0 for a process that has been reaped.
func (p *libcontainerProcess) Pid() int {
	pid, err := p.process.Pid()
	if err != nil {
		return 0
	}
	return pid
}

// Wait blocks until the process exits and returns its exit code.
func (p *libcontainerProcess) Wait() (int, error) {
	state, err := p.process.Wait()
	if state == nil {
		return -1, err
	}
	return exitCode(state), nil
}

// Signal delivers sig to the process.
func (p *libcontainerProcess) Signal(sig os.Signal) error {
	return p.process.Signal(sig)
}

// Console returns the pseudo-terminal master, or nil for a non-TTY process.
func (p *libcontainerProcess) Console() io.ReadWriteCloser {
	if p.console == nil {
		return nil
	}
	return p.console
}

// exitCode reports a process's exit status the way a shell does: the status
// for a normal exit, 128 plus the signal for a killed process.
func exitCode(state *os.ProcessState) int {
	status, ok := state.Sys().(syscall.WaitStatus)
	if ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return state.ExitCode()
}

// isExitStatus reports whether err only says that a process exited with a
// non-zero status.
func isExitStatus(err error) bool {
	var exitErr interface{ ExitCode() int }
	return errors.As(err, &exitErr)
}
