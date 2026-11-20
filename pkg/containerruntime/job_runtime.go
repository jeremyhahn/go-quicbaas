// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package containerruntime

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// jobStopTimeoutSeconds is how long Stop waits for a container's init
// process to leave after SIGTERM before it is killed, as `docker stop` does.
const jobStopTimeoutSeconds = 10

// jobNamePrefix names the containers JobRuntime creates for specs that carry
// no name of their own.
const jobNamePrefix = "quicbaas-job-"

// ErrJobSpecUnsupported is returned for a ContainerSpec field the in-process
// runtime cannot honour yet. It is refused rather than dropped, so a job never
// runs with less isolation or fewer devices than it asked for.
var ErrJobSpecUnsupported = errors.New("containerruntime: container spec field not supported")

// ErrNetworkUnsupported refuses a job whose network the in-process runtime
// cannot give it. The runtime gives every container an isolated network
// namespace with no interface, route, DNS or port forward, which is the
// "none" network and nothing else: a job asking for any other network, or
// for published ports, is refused before its container is created rather
// than run without the network it asked for.
var ErrNetworkUnsupported = errors.New("containerruntime: only the \"none\" network is supported")

// JobRuntime runs orchestration jobs on the in-process Runtime: it implements
// orchestration.ContainerRuntime, the interface the fleet node agent launches
// job containers through, with no runtime binary and no socket.
//
// Errors are the orchestration package's typed errors, so callers written
// against ExecContainerRuntime see the same contract.
type JobRuntime struct {
	runtime *Runtime
}

var _ orchestration.ContainerRuntime = (*JobRuntime)(nil)

// NewJobRuntime returns a JobRuntime over runtime. The caller keeps ownership
// of runtime and closes it.
func NewJobRuntime(runtime *Runtime) *JobRuntime {
	return &JobRuntime{runtime: runtime}
}

// Run pulls the spec's image if it is not stored yet, then creates and
// starts a detached container. It returns the container ID. A pinned spec is
// launched by digest, and the stored image must resolve to that digest.
func (j *JobRuntime) Run(ctx context.Context, spec *orchestration.ContainerSpec) (string, error) {
	return j.launch(ctx, spec, false, func(id string) error {
		return j.runtime.StartContainer(ctx, id)
	})
}

// launch creates the container spec describes, pulling its image when the
// store lacks it, and runs start on it. A container whose start fails is
// removed.
func (j *JobRuntime) launch(ctx context.Context, spec *orchestration.ContainerSpec, supervised bool, start func(id string) error) (string, error) {
	config, err := jobContainerConfig(spec)
	if err != nil {
		return "", err
	}
	if supervised {
		// The supervisor removes the container once it has recorded the
		// exit; runtime auto-removal could remove it before the supervisor
		// reads the exit code.
		config.AutoRemove = false
	}
	if err := j.ensureImage(ctx, config.Image); err != nil {
		return "", err
	}

	state, err := j.runtime.CreateContainer(ctx, config)
	if err != nil {
		return "", &orchestration.ContainerStartError{Image: spec.Image, Cause: err}
	}
	if err := start(state.ID); err != nil {
		if rmErr := j.runtime.RemoveContainer(context.WithoutCancel(ctx), state.ID, true); rmErr != nil {
			err = errors.Join(err, rmErr)
		}
		return "", &orchestration.ContainerStartError{Image: spec.Image, Cause: err}
	}
	return state.ID, nil
}

// RunInteractive is not served by the in-process runtime yet: the init
// process's stdio is wired to the container log files. The spec is still
// validated, so a refusal for a bad or unpinned spec is the same as Run's.
// RunInteractive starts a container whose entrypoint's standard input and
// output are the returned process's pipes, as `run -i --rm` does. The
// container is bound to ctx: when ctx ends it is killed. It is removed once
// it exits. The environment never leaves the process, so Cleanup is nil.
func (j *JobRuntime) RunInteractive(ctx context.Context, spec *orchestration.ContainerSpec) (*orchestration.InteractiveProcess, error) {
	var attached *AttachedIO
	id, err := j.launch(ctx, spec, true, func(id string) error {
		var startErr error
		attached, startErr = j.runtime.StartAttached(ctx, id)
		return startErr
	})
	if err != nil {
		return nil, err
	}

	session := &interactiveSession{runtime: j.runtime, id: id, done: make(chan struct{})}
	go session.supervise(ctx)
	return &orchestration.InteractiveProcess{
		ContainerID: id,
		Stdin:       attached.Stdin,
		Stdout:      attached.Stdout,
		Wait:        session.wait,
	}, nil
}

// interactiveSession supervises one RunInteractive container: it kills the
// container when the launch context ends, removes it once it exits, and
// records how it ended.
type interactiveSession struct {
	runtime *Runtime
	id      string

	// done is closed once exit is set and the container is removed.
	done chan struct{}
	exit orchestration.ContainerExit
}

func (s *interactiveSession) supervise(ctx context.Context) {
	defer close(s.done)
	background := context.WithoutCancel(ctx)

	exited := make(chan orchestration.ContainerExit, 1)
	go func() {
		code, err := s.runtime.WaitContainer(background, s.id)
		exited <- orchestration.ContainerExit{Known: err == nil && code >= 0, Code: int(code)}
	}()

	select {
	case s.exit = <-exited:
	case <-ctx.Done():
		// The runtime killed it, so it has no exit of its own to report.
		if err := s.runtime.StopContainer(background, s.id, 0); err != nil {
			s.runtime.logger.Warn("failed to kill interactive container", "id", s.id, "error", err)
		}
		<-exited
	}

	if err := s.runtime.RemoveContainer(background, s.id, true); err != nil {
		s.runtime.logger.Warn("failed to remove interactive container", "id", s.id, "error", err)
	}
}

// wait reports how the container ended: an unknown exit when ctx ends first,
// or when the runtime killed the container because its launch context ended.
func (s *interactiveSession) wait(ctx context.Context) orchestration.ContainerExit {
	select {
	case <-s.done:
		return s.exit
	case <-ctx.Done():
		return orchestration.ContainerExit{}
	}
}

// Exec runs one command inside a running container. A non-zero exit status
// is reported on the result, not as an error.
func (j *JobRuntime) Exec(ctx context.Context, containerID string, spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
	if containerID == "" {
		return nil, &orchestration.InputValidationError{Field: "containerID", Message: "cannot be empty"}
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}

	running, err := j.IsRunning(ctx, containerID)
	if err != nil {
		return nil, err
	}
	if !running {
		return nil, &orchestration.ContainerNotRunningError{ContainerID: containerID, Message: "container is not running"}
	}

	opts := ExecOptions{
		Command:    spec.Cmd,
		Env:        envList(spec.Env),
		WorkingDir: spec.WorkDir,
	}
	if len(spec.Stdin) > 0 {
		opts.Stdin = bytes.NewReader(spec.Stdin)
	}
	result, err := j.runtime.Exec(ctx, containerID, opts)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, &orchestration.ContainerExecError{ContainerID: containerID, Cause: ctxErr}
		}
		if errors.Is(err, ErrContainerNotFound) {
			return nil, &orchestration.ContainerNotFoundError{ContainerID: containerID}
		}
		return nil, &orchestration.ContainerExecError{ContainerID: containerID, Cause: err}
	}
	return &orchestration.ExecResult{
		ExitCode: result.ExitCode,
		Stdout:   string(result.Stdout),
		Stderr:   string(result.Stderr),
	}, nil
}

// Stop stops a running container: SIGTERM, then SIGKILL after the timeout.
func (j *JobRuntime) Stop(ctx context.Context, containerID string) error {
	if containerID == "" {
		return &orchestration.InputValidationError{Field: "containerID", Message: "cannot be empty"}
	}
	if err := j.runtime.StopContainer(ctx, containerID, jobStopTimeoutSeconds); err != nil {
		if errors.Is(err, ErrContainerNotFound) {
			return &orchestration.ContainerNotFoundError{ContainerID: containerID}
		}
		return &orchestration.ContainerStopError{ContainerID: containerID, Cause: err}
	}
	return nil
}

// Remove removes a container, stopping it first if it is running.
func (j *JobRuntime) Remove(ctx context.Context, containerID string) error {
	if containerID == "" {
		return &orchestration.InputValidationError{Field: "containerID", Message: "cannot be empty"}
	}
	err := j.runtime.RemoveContainer(ctx, containerID, true)
	if errors.Is(err, ErrContainerNotFound) {
		// A container an earlier agent left is not registered in this
		// process; only its bundle record names it. The reaper removes those
		// through here too.
		reclaimed, reclaimErr := j.reclaimLeftover(containerID)
		if reclaimErr != nil {
			return &orchestration.ContainerStopError{ContainerID: containerID, Cause: reclaimErr}
		}
		if !reclaimed {
			return &orchestration.ContainerNotFoundError{ContainerID: containerID}
		}
		return nil
	}
	if err != nil {
		return &orchestration.ContainerStopError{ContainerID: containerID, Cause: err}
	}
	return nil
}

// reclaimLeftover reclaims the leftover container id, reporting false when no
// bundle record names it.
func (j *JobRuntime) reclaimLeftover(id string) (bool, error) {
	leftovers, err := j.runtime.ListLeftovers()
	if err != nil {
		return false, err
	}
	if !slices.ContainsFunc(leftovers, func(l LeftoverContainer) bool { return l.ID == id }) {
		return false, nil
	}
	return true, j.runtime.ReclaimLeftover(id)
}

// Logs returns what the container wrote, stdout then stderr.
func (j *JobRuntime) Logs(_ context.Context, containerID string) (io.ReadCloser, error) {
	if containerID == "" {
		return nil, &orchestration.InputValidationError{Field: "containerID", Message: "cannot be empty"}
	}
	stdout, stderr, err := j.runtime.Logs(containerID)
	if err != nil {
		if errors.Is(err, ErrContainerNotFound) {
			return nil, &orchestration.ContainerNotFoundError{ContainerID: containerID}
		}
		return nil, &orchestration.TransportError{Operation: "logs", Cause: err}
	}
	return io.NopCloser(io.MultiReader(bytes.NewReader(stdout), bytes.NewReader(stderr))), nil
}

// IsRunning reports whether the container's init process is running.
func (j *JobRuntime) IsRunning(ctx context.Context, containerID string) (bool, error) {
	if containerID == "" {
		return false, &orchestration.InputValidationError{Field: "containerID", Message: "cannot be empty"}
	}
	state, err := j.runtime.InspectContainer(ctx, containerID)
	if err != nil {
		if errors.Is(err, ErrContainerNotFound) {
			return false, &orchestration.ContainerNotFoundError{ContainerID: containerID}
		}
		return false, &orchestration.TransportError{Operation: "inspect", Cause: err}
	}
	return state.State.IsRunning(), nil
}

// IsAvailable reports whether the in-process runtime is initialized and
// open.
func (j *JobRuntime) IsAvailable() bool {
	return j != nil && j.runtime != nil && j.runtime.IsInitialized() && !j.runtime.closed.Load()
}

// SupportsSandboxNetwork reports whether a sandbox can be given network:
// this runtime gives a container the "none" network and nothing else (see
// ErrNetworkUnsupported), so an agent configured for any other sandbox
// network refuses to start rather than refuse every spawn.
func (*JobRuntime) SupportsSandboxNetwork(network orchestration.SandboxNetwork) bool {
	return network == orchestration.SandboxNetworkNone
}

// Pull fetches an image into the runtime's storage, honouring the signature
// policy and a digest pin.
func (j *JobRuntime) Pull(ctx context.Context, image string) error {
	if image == "" {
		return &orchestration.InputValidationError{Field: "image", Message: "cannot be empty"}
	}
	if err := j.runtime.PullImage(ctx, image); err != nil {
		return &orchestration.ContainerImagePullError{Image: image, Cause: err}
	}
	return nil
}

// ensureImage pulls ref unless the runtime already stores it.
func (j *JobRuntime) ensureImage(ctx context.Context, ref string) error {
	if _, err := j.runtime.backends.images.Inspect(ctx, ref); err == nil {
		return nil
	}
	return j.Pull(ctx, ref)
}

// jobContainerConfig validates spec and maps it onto the runtime's container
// configuration. Fields the runtime cannot honour are refused.
func jobContainerConfig(spec *orchestration.ContainerSpec) (*ContainerConfig, error) {
	if spec == nil {
		return nil, &orchestration.InputValidationError{Field: "spec", Message: "cannot be nil"}
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if spec.GPUEnabled {
		return nil, &orchestration.InputValidationError{Field: "gpu_enabled", Message: ErrJobSpecUnsupported.Error()}
	}

	name := spec.Name
	if name == "" {
		suffix := make([]byte, 8)
		if _, err := rand.Read(suffix); err != nil {
			return nil, err
		}
		name = jobNamePrefix + hex.EncodeToString(suffix)
	}

	config := &ContainerConfig{
		Name:        name,
		Image:       spec.PinnedImageReference(),
		Entrypoint:  slices.Clone(spec.Entrypoint),
		Command:     slices.Concat(spec.Command, spec.Args),
		Environment: maps.Clone(spec.Env),
		Labels:      maps.Clone(spec.Labels),
		NetworkMode: spec.Network,
		User:        spec.User,
		WorkDir:     spec.WorkDir,
		Privileged:  spec.Privileged,
		// A job's container runs as `docker run` runs it, without
		// no-new-privileges: SandboxSecurity keeps it unprivileged with a
		// small capability set and lets a non-root worker use setuid sudo
		// within it.
		AllowNewPrivileges: true,
		AutoRemove:         spec.AutoRemove,
		CapAdd:             slices.Clone(spec.CapAdd),
		CapDrop:            slices.Clone(spec.CapDrop),
	}
	if spec.MemoryBytes > 0 || spec.PidsLimit > 0 {
		config.Resources = &ResourceLimits{MemoryLimit: spec.MemoryBytes, PidsLimit: spec.PidsLimit}
	}
	for _, u := range spec.Ulimits {
		config.Ulimits = append(config.Ulimits, Ulimit{Name: u.Name, Soft: u.Soft, Hard: u.Hard})
	}
	for _, m := range spec.Mounts {
		mount := Mount{Source: m.Source, Target: m.Target, Type: m.Type, ReadOnly: m.ReadOnly}
		if m.TmpfsSizeBytes > 0 {
			mount.Options = append(mount.Options, "size="+strconv.FormatInt(m.TmpfsSizeBytes, 10))
		}
		config.Mounts = append(config.Mounts, mount)
	}
	for i, p := range spec.Ports {
		port, err := parsePortMapping(p)
		if err != nil {
			return nil, &orchestration.InputValidationError{Field: "ports[" + strconv.Itoa(i) + "]", Message: err.Error()}
		}
		config.Ports = append(config.Ports, port)
	}
	if err := config.Validate(); err != nil {
		return nil, &orchestration.InputValidationError{Field: "spec", Message: err.Error()}
	}
	if spec.Network != "none" {
		return nil, fmt.Errorf("%w: job asks for network %q", ErrNetworkUnsupported, spec.Network)
	}
	if len(spec.Ports) > 0 {
		return nil, fmt.Errorf("%w: job asks to publish ports %q", ErrNetworkUnsupported, spec.Ports)
	}
	return config, nil
}

// parsePortMapping parses the "[hostIP:]host:container[/proto]" form of a
// ContainerSpec port.
func parsePortMapping(value string) (PortMapping, error) {
	var mapping PortMapping
	ports, proto, _ := strings.Cut(value, "/")
	mapping.Protocol = proto

	parts := strings.Split(ports, ":")
	switch len(parts) {
	case 2:
	case 3:
		mapping.HostIP = parts[0]
		parts = parts[1:]
	default:
		return mapping, errors.New("port must be host:container")
	}
	host, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		return mapping, err
	}
	ctr, err := strconv.ParseUint(parts[1], 10, 16)
	if err != nil {
		return mapping, err
	}
	mapping.HostPort, mapping.ContainerPort = uint16(host), uint16(ctr)
	return mapping, nil
}

// envList renders env as sorted KEY=VALUE entries.
func envList(env map[string]string) []string {
	keys := slices.Sorted(maps.Keys(env))
	list := make([]string, 0, len(keys))
	for _, key := range keys {
		list = append(list, key+"="+env[key])
	}
	return list
}
