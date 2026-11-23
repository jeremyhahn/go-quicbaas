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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	// devices fills the cgroup managers' device hooks in its init. specconv
	// gives every container default device rules, which both managers refuse
	// while the hooks are nil.
	"github.com/opencontainers/cgroups"
	_ "github.com/opencontainers/cgroups/devices"
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

// reclaimPoll and reclaimTimeout bound how long Reclaim waits for a killed
// leftover container to stop. This process is not its parent, so it can only
// poll the container's status.
const (
	reclaimPoll    = 50 * time.Millisecond
	reclaimTimeout = 10 * time.Second
)

// Reclaim loads a container an earlier process left in the state directory,
// kills it if it still runs, and destroys it, as `runc delete --force` does.
func (e *libcontainerEngine) Reclaim(id string) error {
	container, err := libcontainer.Load(e.root, id)
	if errors.Is(err, libcontainer.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	deadline := time.Now().Add(reclaimTimeout)
	for {
		status, err := container.Status()
		if err != nil {
			return err
		}
		if status == libcontainer.Stopped {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("container %s is still %s %s after SIGKILL", id, status, reclaimTimeout)
		}
		if err := container.Signal(unix.SIGKILL); err != nil && !errors.Is(err, libcontainer.ErrNotRunning) {
			return err
		}
		time.Sleep(reclaimPoll)
	}
	return container.Destroy()
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
	var workload string
	if !init {
		// An exec runs in its own sub-cgroup, so its descendants can be
		// killed after its leader has exited and been reaped. One that
		// cannot get a killable sub-cgroup is refused, not run in the
		// container's cgroup where nothing could reach its orphans.
		workload, err = newKillableExecWorkload(execCgroupBase(container))
		if err != nil {
			return nil, err
		}
		process.SubCgroupPaths = map[string]string{"": filepath.Base(workload)}
	}
	if !spec.TTY {
		p, err := runPiped(container, process, workload)
		if err != nil {
			removeExecWorkload(workload)
		}
		return p, err
	}

	parent, child, err := utils.NewSockPair("console")
	if err != nil {
		return nil, err
	}
	process.ConsoleSocket = child

	runErr := container.Run(process)
	childErr := child.Close()
	if runErr != nil {
		removeExecWorkload(workload)
		return nil, errors.Join(runErr, childErr, parent.Close())
	}

	console, recvErr := receiveConsole(parent)
	if err := errors.Join(recvErr, childErr, parent.Close()); err != nil {
		return nil, errors.Join(err, killProcess(process), closeIfSet(console))
	}
	return &libcontainerProcess{process: process, console: console, workload: workload}, nil
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
		process.Stdin = spec.Stdin
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

	// outputs are the pipes the process writes to in place of output
	// writers that are not files; copies drains them into the writers.
	// With exec.Cmd's own copying a descendant holding a pipe kept Wait
	// blocked and nothing could close the pipe; these Release closes.
	outputs []outputPipe
	copies  sync.WaitGroup

	// guard serializes Wait's reap with KillTree's signalling by PID.
	guard reapGuard

	// workload is an exec's own cgroup v2 sub-cgroup; it is "" only for a
	// container's init, whose descendants die with its PID namespace. Every
	// descendant of an exec stays in it however it is reparented, so
	// KillTree reaches them after the leader is reaped.
	workload string
}

// outputPipe is a pipe the runtime owns between a process and an output
// writer.
type outputPipe struct {
	read, write *os.File
	dst         io.Writer
}

// runPiped runs a non-TTY process, its output writers that are not files
// fed through pipes the runtime owns. When workload is set, the process
// runs in that sub-cgroup.
func runPiped(container *libcontainer.Container, process *libcontainer.Process, workload string) (containerProcess, error) {
	p := &libcontainerProcess{process: process, workload: workload}
	var err error
	if process.Stdout, err = p.pipeOutput(process.Stdout); err == nil {
		process.Stderr, err = p.pipeOutput(process.Stderr)
	}
	if err == nil {
		err = container.Run(process)
	}
	// The process holds its own copies of the write ends now.
	var closeErr error
	for _, out := range p.outputs {
		closeErr = errors.Join(closeErr, out.write.Close())
	}
	if err != nil {
		return nil, errors.Join(err, closeErr, p.closeOutputs())
	}
	if closeErr != nil {
		return nil, errors.Join(closeErr, killProcess(process), p.closeOutputs())
	}
	for _, out := range p.outputs {
		p.copies.Add(1)
		go func() {
			defer p.copies.Done()
			// The copy ends at EOF, or when Release closes the read end.
			_, _ = io.Copy(out.dst, out.read)
		}()
	}
	return p, nil
}

// pipeOutput returns what the process writes to in place of w: w itself
// when it is a file or nil, otherwise the write end of a new pipe.
func (p *libcontainerProcess) pipeOutput(w io.Writer) (io.Writer, error) {
	if w == nil {
		return nil, nil
	}
	if _, ok := w.(*os.File); ok {
		return w, nil
	}
	read, write, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	p.outputs = append(p.outputs, outputPipe{read: read, write: write, dst: w})
	return write, nil
}

// closeOutputs closes the read ends of the process's output pipes. One
// already closed is not an error.
func (p *libcontainerProcess) closeOutputs() error {
	var err error
	for _, out := range p.outputs {
		if closeErr := out.read.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			err = errors.Join(err, closeErr)
		}
	}
	return err
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
	state, err := p.guard.reap(p.Pid(), p.process.Wait)
	// The output is complete once every holder of the pipes has closed
	// them, or Release has.
	p.copies.Wait()
	err = errors.Join(err, p.closeOutputs())
	removeExecWorkload(p.workload)
	if state == nil {
		return -1, err
	}
	return exitCode(state), nil
}

// Release kills the process and its descendants and closes the read ends
// of its output pipes, which ends the copies Wait waits for.
func (p *libcontainerProcess) Release() error {
	killErr := p.KillTree()
	if errors.Is(killErr, os.ErrProcessDone) {
		killErr = nil
	}
	return errors.Join(killErr, p.closeOutputs())
}

// Signal delivers sig to the process.
func (p *libcontainerProcess) Signal(sig os.Signal) error {
	return p.process.Signal(sig)
}

// KillTree kills the process and every descendant it has, so none of them
// keeps the process's output pipes open after it dies. With a workload
// sub-cgroup, the kernel kills everything in it through cgroup.kill, the
// leader's orphans included, even after Wait has reaped the leader. A
// container's init has none and is walked by PID only while it is unreaped:
// a leader Wait has already reaped is never signalled by PID, since the host
// may have reused it.
func (p *libcontainerProcess) KillTree() error {
	if p.workload != "" {
		err := killWorkload(p.workload)
		if err == nil || !errors.Is(err, os.ErrNotExist) {
			return err
		}
		// The sub-cgroup is gone: Wait removes it only once it is empty.
	}
	pid, err := p.process.Pid()
	if err != nil {
		pid = 0
	}
	return p.guard.killTree(pid, p.process.Signal)
}

// killProcessTree stops pid so it cannot fork again, kills its descendants
// depth first, then kills it. A process that has already gone is not an error.
func killProcessTree(pid int) error { return signalProcessTree(pid, unix.Kill) }

// signalProcessTree stops pid, kills its descendants, then kills pid,
// sending every signal through kill.
func signalProcessTree(pid int, kill func(int, unix.Signal) error) error {
	if err := kill(pid, unix.SIGSTOP); err != nil {
		if errors.Is(err, unix.ESRCH) {
			return nil
		}
		return err
	}
	var errs []error
	for _, child := range processChildren(pid) {
		errs = append(errs, signalProcessTree(child, kill))
	}
	if err := kill(pid, unix.SIGKILL); err != nil && !errors.Is(err, unix.ESRCH) {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// reapGuard serializes reaping a child process with signalling it by
// PID. Once a process is reaped its PID can be reused by an unrelated
// host process. Until then the PID is pinned, even for a zombie, and a
// stopped parent cannot reap its own children. So killTree walks the
// tree by number only while it holds mu and the leader is unreaped.
type reapGuard struct {
	mu     sync.Mutex
	reaped bool
	// kill signals a PID; nil is unix.Kill. Tests record it.
	kill func(pid int, sig unix.Signal) error
}

// reap waits for pid to exit without reaping it, then reaps it with
// wait while holding mu. If pid is not a waitable child (pid <= 0, or
// waitid fails), the process is marked reaped first, so killTree stops
// signalling by number before wait runs.
func (g *reapGuard) reap(pid int, wait func() (*os.ProcessState, error)) (*os.ProcessState, error) {
	if pid > 0 && waitExited(pid) == nil {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.reaped = true
		return wait()
	}
	g.mu.Lock()
	g.reaped = true
	g.mu.Unlock()
	return wait()
}

// killTree kills pid and its descendants while pid is still unreaped. A
// reaped leader is not signalled at all, since its PID may be reused;
// an exec's workload sub-cgroup reaches what it left behind (KillTree). A
// process that never started (pid <= 0) is signalled through its handle.
func (g *reapGuard) killTree(pid int, signal func(os.Signal) error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.reaped {
		return nil
	}
	if pid <= 0 {
		return signal(unix.SIGKILL)
	}
	kill := g.kill
	if kill == nil {
		kill = unix.Kill
	}
	return signalProcessTree(pid, kill)
}

// waitExited blocks until the child pid has exited, leaving it unreaped.
func waitExited(pid int) error {
	for {
		var info unix.Siginfo
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

// execWorkloadPrefix starts the name of each exec's sub-cgroup.
const execWorkloadPrefix = "exec-"

// execCgroupBase returns the container's cgroup v2 directory, or "" when
// the host is not on the unified hierarchy or the container has none.
func execCgroupBase(container *libcontainer.Container) string {
	if !cgroups.IsCgroup2UnifiedMode() {
		return ""
	}
	state, err := container.State()
	if err != nil {
		return ""
	}
	return state.CgroupPaths[""]
}

// newExecWorkload creates one exec's sub-cgroup under base, the
// container's cgroup v2 directory, and returns its path. An empty base is
// no workload and no error.
func newExecWorkload(base string) (string, error) {
	if base == "" {
		return "", nil
	}
	var name [8]byte
	if _, err := rand.Read(name[:]); err != nil {
		return "", err
	}
	dir := filepath.Join(base, execWorkloadPrefix+hex.EncodeToString(name[:]))
	if err := os.Mkdir(dir, 0o750); err != nil {
		return "", err
	}
	return dir, nil
}

// newKillableExecWorkload creates an exec's sub-cgroup under base and
// requires its cgroup.kill, so every descendant of the exec can be killed
// after its leader is reaped. Anything less is ErrExecNotKillable, and a
// sub-cgroup it created is removed.
func newKillableExecWorkload(base string) (string, error) {
	dir, err := newExecWorkload(base)
	if err != nil {
		return "", errors.Join(ErrExecNotKillable, err)
	}
	if dir == "" {
		return "", ErrExecNotKillable
	}
	if _, err := os.Stat(filepath.Join(dir, "cgroup.kill")); err != nil {
		removeExecWorkload(dir)
		return "", errors.Join(ErrExecNotKillable, err)
	}
	return dir, nil
}

// killWorkload kills every process in the cgroup dir through cgroup.kill.
// The kernel walks the cgroup itself, so a process reparented away from
// the exec's leader is killed too, and no PID is signalled.
func killWorkload(dir string) error {
	f, err := os.OpenFile(filepath.Join(dir, "cgroup.kill"), os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, writeErr := f.WriteString("1")
	return errors.Join(writeErr, f.Close())
}

// removeExecWorkload removes an exec's sub-cgroup once nothing runs in it.
// One that still holds a process stays until its container's Destroy,
// which removes sub-cgroups with the container's cgroup.
func removeExecWorkload(dir string) {
	if dir != "" {
		_ = unix.Rmdir(dir)
	}
}

// processChildren lists pid's children from /proc, across all its threads.
func processChildren(pid int) []int {
	tasks, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid))
	if err != nil {
		return nil
	}
	var children []int
	for _, task := range tasks {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%s/children", pid, task.Name()))
		if err != nil {
			continue
		}
		for _, field := range strings.Fields(string(data)) {
			if child, err := strconv.Atoi(field); err == nil {
				children = append(children, child)
			}
		}
	}
	return children
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
