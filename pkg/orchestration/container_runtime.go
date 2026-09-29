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

package orchestration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

const (
	// RuntimeDocker is the docker container runtime command.
	RuntimeDocker = "docker"

	// RuntimePodman is the podman container runtime command.
	RuntimePodman = "podman"

	// interactiveContainerPrefix is the name prefix for interactive containers.
	interactiveContainerPrefix = "quicbaas-acp-"

	// containerIDRandomBytes is the number of random bytes used to generate
	// unique container names.
	containerIDRandomBytes = 8
)

// ExecContainerRuntime implements ContainerRuntime by shelling out to
// docker or podman. No Go library dependencies - just exec.Command.
//
// It implements Tier 0 of the trust model: a spec that resolves to an
// immutable content digest is launched as "repository@sha256:<hex>" and the
// resulting container is verified to have been created from that exact image.
// It implements no higher tier — see TPMAttestingRuntime for Tiers 1 and 2.
type ExecContainerRuntime struct {
	command string
	logger  interfaces.Logger

	// inspect performs a "<runtime> inspect --format ..." query. It is a
	// field rather than a plain method so the digest-verification path can
	// be exercised without a container runtime installed; nil means use
	// the real exec-backed implementation.
	inspect inspectFunc

	// runImageCommand runs the image lifecycle's engine invocations (see
	// container_image.go). A field for the same reason inspect is one; nil
	// means exec the real engine.
	runImageCommand imageCommandRunner
}

// inspectFunc runs a docker/podman inspect query and returns its trimmed
// stdout.
type inspectFunc func(ctx context.Context, args ...string) (string, error)

// Compile-time interface check.
var _ ContainerRuntime = (*ExecContainerRuntime)(nil)

// NewExecContainerRuntime creates a runtime that auto-detects docker or podman.
// Preference order: docker, then podman.
func NewExecContainerRuntime(logger interfaces.Logger) (*ExecContainerRuntime, error) {
	rt := &ExecContainerRuntime{logger: logger}
	if detectRuntime(RuntimeDocker) {
		rt.command = RuntimeDocker
		logger.Infof("container runtime: detected %s", RuntimeDocker)
		return rt, nil
	}
	if detectRuntime(RuntimePodman) {
		rt.command = RuntimePodman
		logger.Infof("container runtime: detected %s", RuntimePodman)
		return rt, nil
	}
	return nil, &ContainerRuntimeNotAvailableError{
		Message: "neither docker nor podman found in PATH",
	}
}

// NewExecContainerRuntimeWithCommand creates a runtime with a specific command.
func NewExecContainerRuntimeWithCommand(command string, logger interfaces.Logger) (*ExecContainerRuntime, error) {
	if command == "" {
		return nil, &InputValidationError{Field: "command", Message: "cannot be empty"}
	}
	rt := &ExecContainerRuntime{
		command: command,
		logger:  logger,
	}
	if !rt.IsAvailable() {
		return nil, &ContainerRuntimeNotAvailableError{
			Message: command + " is not available",
		}
	}
	return rt, nil
}

// Command returns the resolved container runtime command name.
func (r *ExecContainerRuntime) Command() string {
	return r.command
}

// Run creates and starts a detached container. Returns the container ID.
func (r *ExecContainerRuntime) Run(ctx context.Context, spec *ContainerSpec) (string, error) {
	if spec == nil {
		return "", &InputValidationError{Field: "spec", Message: "cannot be nil"}
	}
	if err := spec.Validate(); err != nil {
		return "", err
	}

	// The environment travels in a private 0600 file, never in argv. The
	// detached run process reads it while it parses its command line, so the
	// file is released as soon as cmd.Run returns.
	envFile, envErr := writeContainerEnvFile(spec.Env)
	if envErr != nil {
		return "", envErr
	}
	if envFile.Cleanup != nil {
		defer envFile.Cleanup()
	}

	args := r.buildRunArgs(spec, envFile.Path)
	r.logger.Debugf("container run: %s %s", r.command, redactRunArgs(args))

	cmd := exec.CommandContext(ctx, r.command, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if strings.Contains(stderrStr, "Unable to find image") ||
			strings.Contains(stderrStr, "image not known") ||
			strings.Contains(stderrStr, "manifest unknown") {
			return "", &ContainerImagePullError{
				Image: spec.Image,
				Cause: &CommandError{Command: r.command, Op: "run", Output: stderrStr},
			}
		}
		return "", &ContainerStartError{
			Image: spec.Image,
			Cause: &CommandError{Command: r.command, Op: "run", Output: stderrStr},
		}
	}

	containerID := strings.TrimSpace(stdout.String())

	// Tier 0: confirm the container the runtime actually created came from
	// the pinned image. Passing repository@digest on the command line is
	// necessary but not sufficient — verifying after the fact closes the
	// gap between "we asked for this image" and "this image is what ran".
	if err := r.VerifyPinnedImage(ctx, spec, containerID); err != nil {
		return containerID, err
	}

	r.logger.Infof("container started: %s (id=%s)", spec.PinnedImageReference(), truncateID(containerID))
	return containerID, nil
}

// RunInteractive starts a container with stdin/stdout pipes for interactive
// stdio communication. The container runs in foreground mode (-i --rm) and the
// caller receives an InteractiveProcess with pipes connected to the container's
// entrypoint. A unique container name is generated if spec.Name is empty.
func (r *ExecContainerRuntime) RunInteractive(ctx context.Context, spec *ContainerSpec) (*InteractiveProcess, error) {
	if spec == nil {
		return nil, &InputValidationError{Field: "spec", Message: "cannot be nil"}
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}

	// Generate a unique container name so we can reference it for stop/remove.
	containerName := spec.Name
	if containerName == "" {
		id, randErr := generateContainerID()
		if randErr != nil {
			return nil, &ContainerInteractiveStartError{
				Image:     spec.Image,
				Operation: "generate-name",
				Cause:     randErr,
			}
		}
		containerName = interactiveContainerPrefix + id
	}

	// The environment travels in a private 0600 file, never in argv. Unlike
	// the detached path, this process runs for the whole session and the
	// runtime may not have parsed the file by the time Start returns, so the
	// file's lifetime is handed to the caller through InteractiveProcess.
	envFile, envErr := writeContainerEnvFile(spec.Env)
	if envErr != nil {
		return nil, envErr
	}
	releaseEnvFile := func() {
		if envFile.Cleanup != nil {
			envFile.Cleanup()
		}
	}

	args := r.buildInteractiveRunArgs(spec, containerName, envFile.Path)
	r.logger.Debugf("container run interactive: %s %s", r.command, redactRunArgs(args))

	cmd := exec.CommandContext(ctx, r.command, args...)
	// stderr goes to discard for now — container logs are captured separately
	cmd.Stderr = io.Discard

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		releaseEnvFile()
		return nil, &ContainerInteractiveStartError{
			Image:     spec.Image,
			Operation: "stdin-pipe",
			Cause:     err,
		}
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		releaseEnvFile()
		closeErr := stdinPipe.Close()
		if closeErr != nil {
			r.logger.Debugf("container interactive: failed to close stdin pipe: %v", closeErr)
		}
		return nil, &ContainerInteractiveStartError{
			Image:     spec.Image,
			Operation: "stdout-pipe",
			Cause:     err,
		}
	}

	// Discard stderr to prevent blocking. In production, stderr from
	// ACP agent containers is not part of the JSON-RPC protocol.
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		releaseEnvFile()
		closeErr := stdinPipe.Close()
		if closeErr != nil {
			r.logger.Debugf("container interactive: failed to close stdin pipe: %v", closeErr)
		}
		return nil, &ContainerInteractiveStartError{
			Image:     spec.Image,
			Operation: "start",
			Cause:     err,
		}
	}

	r.logger.Infof("container interactive started: %s (name=%s, pid=%d, env_file=%s)",
		spec.PinnedImageReference(), containerName, cmd.Process.Pid, envFile.Path)

	// Tier 0: an interactive container is created asynchronously by the
	// foreground `run -i` process, so the verification is performed against
	// the container name once it exists. VerifyPinnedImage tolerates a
	// not-yet-created container by design (see its doc comment) — the pin
	// itself is already enforced on the command line above.
	if err := r.VerifyPinnedImage(ctx, spec, containerName); err != nil {
		releaseEnvFile()
		closeErr := stdinPipe.Close()
		if closeErr != nil {
			r.logger.Debugf("container interactive: failed to close stdin pipe: %v", closeErr)
		}
		return nil, err
	}

	return &InteractiveProcess{
		ContainerID: containerName,
		Stdin:       stdinPipe,
		Stdout:      stdoutPipe,
		Cleanup:     envFile.Cleanup,
		Wait:        newInteractiveExitWaiter(cmd).wait,
	}, nil
}

// interactiveExitWaiter reaps an interactive container's foreground runtime
// process, once, and reports how it ended.
//
// `docker run -i` (and podman's) exits with the container process's own status,
// or 125 for a failure of the runtime itself, so its ProcessState is the
// container's exit. The reap is started lazily on the first wait, because
// exec.Cmd.Wait closes the stdout pipe and must not run while that pipe is
// still being read; callers wait only once stdout has reached EOF.
type interactiveExitWaiter struct {
	cmd   *exec.Cmd
	start sync.Once
	done  chan struct{}
	exit  ContainerExit
}

func newInteractiveExitWaiter(cmd *exec.Cmd) *interactiveExitWaiter {
	return &interactiveExitWaiter{cmd: cmd, done: make(chan struct{})}
}

// wait returns the process's exit, or an unknown exit if ctx ends first.
func (w *interactiveExitWaiter) wait(ctx context.Context) ContainerExit {
	w.start.Do(func() {
		go func() {
			defer close(w.done)
			// The wait error restates the status read below, or says the
			// process was signalled; either way ProcessState is the answer.
			_ = w.cmd.Wait()
			if state := w.cmd.ProcessState; state != nil && state.Exited() {
				w.exit = ContainerExit{Known: true, Code: state.ExitCode()}
			}
		}()
	})
	select {
	case <-w.done:
		return w.exit
	case <-ctx.Done():
		return ContainerExit{}
	}
}

// Stop stops a running container.
func (r *ExecContainerRuntime) Stop(ctx context.Context, containerID string) error {
	if containerID == "" {
		return &InputValidationError{Field: "containerID", Message: "cannot be empty"}
	}

	r.logger.Debugf("container stop: %s", truncateID(containerID))

	cmd := exec.CommandContext(ctx, r.command, "stop", containerID)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if isNotFoundOutput(stderrStr) {
			return &ContainerNotFoundError{ContainerID: containerID}
		}
		return &ContainerStopError{
			ContainerID: containerID,
			Cause:       &CommandError{Command: r.command, Op: "stop", Output: stderrStr},
		}
	}
	return nil
}

// Remove removes a container (force removes if running).
func (r *ExecContainerRuntime) Remove(ctx context.Context, containerID string) error {
	if containerID == "" {
		return &InputValidationError{Field: "containerID", Message: "cannot be empty"}
	}

	r.logger.Debugf("container remove: %s", truncateID(containerID))

	cmd := exec.CommandContext(ctx, r.command, "rm", "-f", containerID)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if isNotFoundOutput(stderrStr) {
			return &ContainerNotFoundError{ContainerID: containerID}
		}
		return &ContainerStopError{
			ContainerID: containerID,
			Cause:       &CommandError{Command: r.command, Op: "rm", Output: stderrStr},
		}
	}
	return nil
}

// Logs returns the container's stdout/stderr logs.
func (r *ExecContainerRuntime) Logs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	if containerID == "" {
		return nil, &InputValidationError{Field: "containerID", Message: "cannot be empty"}
	}

	cmd := exec.CommandContext(ctx, r.command, "logs", containerID)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, &ContainerNotFoundError{ContainerID: containerID}
	}

	if err := cmd.Start(); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if isNotFoundOutput(stderrStr) {
			return nil, &ContainerNotFoundError{ContainerID: containerID}
		}
		return nil, &TransportError{
			Operation: "logs",
			Cause:     &CommandError{Command: r.command, Op: "logs", Output: stderrStr},
		}
	}

	return &cmdReadCloser{cmd: cmd, reader: stdout}, nil
}

// IsRunning checks if a container is running.
func (r *ExecContainerRuntime) IsRunning(ctx context.Context, containerID string) (bool, error) {
	if containerID == "" {
		return false, &InputValidationError{Field: "containerID", Message: "cannot be empty"}
	}

	cmd := exec.CommandContext(ctx, r.command, "inspect",
		"--format", "{{.State.Running}}", containerID)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if isNotFoundOutput(stderrStr) {
			return false, &ContainerNotFoundError{ContainerID: containerID}
		}
		return false, &TransportError{
			Operation: "inspect",
			Cause:     &CommandError{Command: r.command, Op: "inspect", Output: stderrStr},
		}
	}

	return strings.TrimSpace(stdout.String()) == "true", nil
}

// IsAvailable checks if the container runtime (docker/podman) is installed.
func (r *ExecContainerRuntime) IsAvailable() bool {
	return detectRuntime(r.command)
}

// Pull fetches the image from the remote registry via docker/podman pull.
// Returns nil on success (image is locally cached afterwards); returns a
// ContainerImagePullError with the command stderr on failure.
func (r *ExecContainerRuntime) Pull(ctx context.Context, image string) error {
	if image == "" {
		return &InputValidationError{Field: "image", Message: "cannot be empty"}
	}

	r.logger.Debugf("container pull: %s %s", r.command, image)

	cmd := exec.CommandContext(ctx, r.command, "pull", image)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return &ContainerImagePullError{
			Image: image,
			Cause: &CommandError{Command: r.command, Op: "pull", Output: strings.TrimSpace(stderr.String())},
		}
	}

	r.logger.Infof("container image pulled: %s", image)
	return nil
}

// ImageLister is a container runtime that can list the images on its host.
// An agent reports the list so placement sends a spawn only to a host that
// has the spawn's image.
type ImageLister interface {
	ListImages(ctx context.Context) ([]string, error)
}

// Compile-time interface satisfaction check.
var _ ImageLister = (*ExecContainerRuntime)(nil)

// ListImages returns every tagged image on the host as repository:tag.
// Untagged (<none>) images are left out: nothing can name them.
func (r *ExecContainerRuntime) ListImages(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, r.command, "images", "--format", "{{.Repository}}:{{.Tag}}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, &CommandError{Command: r.command, Op: "images", Output: strings.TrimSpace(stderr.String())}
	}
	return parseImageList(stdout.String()), nil
}

// parseImageList turns `images --format {{.Repository}}:{{.Tag}}` output into
// a list of names, dropping blank lines and untagged images.
func parseImageList(out string) []string {
	images := []string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "<none>") {
			continue
		}
		images = append(images, line)
	}
	return images
}

// buildRunArgs constructs the argument list for a detached docker/podman run
// command. envFile is the path of the private file holding spec.Env, as
// produced by writeContainerEnvFile; it is empty for a spec with no
// environment.
func (r *ExecContainerRuntime) buildRunArgs(spec *ContainerSpec, envFile string) []string {
	args := []string{"run", "-d"}

	if spec.Name != "" {
		args = append(args, "--name", spec.Name)
	}

	if spec.AutoRemove {
		args = append(args, "--rm")
	}

	args = r.appendCommonRunArgs(args, spec, envFile)

	return args
}

// buildInteractiveRunArgs constructs the argument list for an interactive
// docker/podman run command. Interactive containers use -i (stdin open)
// and --rm (auto-remove on exit) instead of -d (detached). envFile carries
// spec.Env exactly as it does for the detached form.
func (r *ExecContainerRuntime) buildInteractiveRunArgs(spec *ContainerSpec, containerName, envFile string) []string {
	args := []string{"run", "-i", "--rm"}

	args = append(args, "--name", containerName)

	args = r.appendCommonRunArgs(args, spec, envFile)

	return args
}

// appendCommonRunArgs appends container configuration flags shared between
// detached and interactive run modes: network, workdir, env, mounts, ports,
// labels, GPU, image, command, and args.
//
// envFile is referenced by path; no environment VALUE is ever placed in the
// argument vector. See container_env_file.go for why.
func (r *ExecContainerRuntime) appendCommonRunArgs(args []string, spec *ContainerSpec, envFile string) []string {
	if spec.User != "" {
		args = append(args, "--user", spec.User)
	}

	if spec.Network != "" {
		args = append(args, "--network", spec.Network)
	}

	if spec.WorkDir != "" {
		args = append(args, "--workdir", spec.WorkDir)
	}

	if spec.MemoryBytes > 0 {
		args = append(args, "--memory", strconv.FormatInt(spec.MemoryBytes, 10))
	}

	args = append(args, spec.PrivilegeRunArgs()...)

	// The environment is passed by reference to a private 0600 file. A caller
	// that supplied no file gets a container with no spec environment rather
	// than an inline "-e KEY=VALUE" fallback: failing closed here is what
	// keeps a credential out of /proc/<pid>/cmdline unconditionally.
	if envFile != "" {
		args = append(args, envFileFlag, envFile)
	}

	for i := range spec.Mounts {
		args = append(args, spec.Mounts[i].RunArgs()...)
	}

	for _, p := range spec.Ports {
		args = append(args, "-p", p)
	}

	// Sort label keys for deterministic output.
	if len(spec.Labels) > 0 {
		keys := make([]string, 0, len(spec.Labels))
		for k := range spec.Labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			args = append(args, "--label", k+"="+spec.Labels[k])
		}
	}

	if spec.GPUEnabled {
		args = append(args, r.gpuFlag()...)
	}

	// Override entrypoint if specified (empty slice clears the default entrypoint).
	if spec.Entrypoint != nil {
		if len(spec.Entrypoint) == 0 {
			args = append(args, "--entrypoint", "")
		} else {
			args = append(args, "--entrypoint", spec.Entrypoint[0])
		}
	}

	// Launch by immutable content digest when the spec is pinned. An
	// unpinned spec yields the reference unchanged, so this is a no-op for
	// every pre-existing caller.
	args = append(args, spec.PinnedImageReference())

	if len(spec.Command) > 0 {
		args = append(args, spec.Command...)
	}

	if len(spec.Args) > 0 {
		args = append(args, spec.Args...)
	}

	return args
}

// PrivilegeRunArgs returns the run flags that bound what the container may do:
// --pids-limit, --privileged, --cap-add, --cap-drop and --ulimit, in that
// order. It is
// the ONE renderer of those flags — the runtime launches with it, and
// the worker image probes run with it (ImageRunSpec.Security), both under
// `make test-worker-image` and in the refresh gate
// — so the probe cannot certify a boundary the spawn does not use.
//
// Privilege is emitted so the launched container matches the spec
// ContainerConfigDigest measured. Capabilities and ulimits are sorted by the
// same helpers the digest uses: if the two orderings could differ, a spec
// could measure one way and launch another.
func (s *ContainerSpec) PrivilegeRunArgs() []string {
	var args []string
	if s.PidsLimit > 0 {
		args = append(args, "--pids-limit", strconv.FormatInt(s.PidsLimit, 10))
	}
	if s.Privileged {
		args = append(args, "--privileged")
	}
	for _, capability := range sortedCopy(s.CapAdd) {
		args = append(args, "--cap-add", capability)
	}
	for _, capability := range sortedCopy(s.CapDrop) {
		args = append(args, "--cap-drop", capability)
	}
	for _, u := range sortedUlimits(s.Ulimits) {
		args = append(args, "--ulimit", u.RunArg())
	}
	return args
}

// gpuFlag returns the appropriate GPU passthrough flags for the runtime.
func (r *ExecContainerRuntime) gpuFlag() []string {
	if r.command == RuntimePodman {
		return []string{"--device", "nvidia.com/gpu=all"}
	}
	return []string{"--gpus", "all"}
}

// generateContainerID produces a cryptographically random hex string suitable
// for use as a unique container name suffix.
func generateContainerID() (string, error) {
	b := make([]byte, containerIDRandomBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// detectRuntime checks if a container runtime binary is available.
func detectRuntime(command string) bool {
	cmd := exec.Command(command, "version")
	return cmd.Run() == nil
}

// isNotFoundOutput checks stderr output for container-not-found indicators.
func isNotFoundOutput(stderr string) bool {
	lower := strings.ToLower(stderr)
	// THE RUNTIME'S OWN WORDS, NOT THE COMMAND'S. The stderr an exec returns
	// is mostly the COMMAND's — a compiler, a test runner, make — and "not
	// found" is what half of them say about a symbol, a file or a module.
	// Matched loosely, a failed `make build` whose diagnostics said "not
	// found" was reported as the container having vanished, and the job
	// that had just fixed a review finding was failed and charged for it
	// (go-quicbaas PR #970, 2026-09-21). Docker and podman announce a
	// missing container in exactly these shapes; nothing else is one.
	return strings.Contains(lower, "no such container") ||
		strings.Contains(lower, "no such object") ||
		strings.Contains(lower, "error response from daemon: not found") ||
		strings.Contains(lower, "error: not found") ||
		strings.Contains(lower, "error: no such")
}

// truncateID shortens a container ID for log output.
func truncateID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// CommandError is a container runtime invocation that failed, and the bytes it
// printed while failing.
type CommandError struct {
	// Command and Op name the invocation — the runtime binary and its
	// subcommand, "docker" and "run". They ARE rendered: an operator reading a
	// failure needs to know which command produced it, and neither is
	// process-authored text.
	Command string
	Op      string

	// Output is the process's captured stderr.
	//
	// IT IS NEVER RENDERED BY Error(), and the rule lives on the field rather
	// than at the call sites for the reason StreamJSONProcessExitedError gives
	// at length (errors.go): an error's string reaches every %v, %s, structured
	// logger field and wrapped-error chain in the process, so a redaction
	// applied at one call site is a redaction the next call site does not have.
	// This type was the second instance of that — measured 2026-08-27 (review
	// 1330): Error() returned this field verbatim, and go-quicbaas#198's
	// surfacing path then published a parked item's failure text as a COMMENT
	// ON A FORGE ISSUE. A container command prints whatever it prints, and a
	// build or verification step that echoes a token before dying puts that
	// token here.
	//
	// The bytes stay ON THE FIELD for callers with somewhere safe to put them
	// and the sense to ask by name — a classifier, or an operator surface
	// behind authorization. Nothing gets them by accident.
	Output string
}

// Error implements the error interface.
//
// It names the invocation and the SIZE of the withheld output, never the
// output. The count is what tells an operator there is evidence and roughly
// how much, without being the evidence.
func (e *CommandError) Error() string {
	invocation := strings.TrimSpace(e.Command + " " + e.Op)
	if invocation == "" {
		invocation = "container runtime command"
	}
	msg := invocation + " failed"
	if n := len(e.Output); n > 0 {
		msg += " (" + strconv.Itoa(n) + " bytes of output withheld)"
	}
	return msg
}

// cmdReadCloser wraps an exec.Cmd stdout pipe so the caller can read logs
// and the process is waited on when the reader is closed.
type cmdReadCloser struct {
	cmd    *exec.Cmd
	reader io.ReadCloser
}

// Read implements io.Reader.
func (c *cmdReadCloser) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

// Close waits for the command to finish and closes the reader.
func (c *cmdReadCloser) Close() error {
	closeErr := c.reader.Close()
	waitErr := c.cmd.Wait()
	if closeErr != nil {
		return closeErr
	}
	return waitErr
}
