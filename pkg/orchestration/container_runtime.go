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
	"strings"

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
type ExecContainerRuntime struct {
	command string
	logger  interfaces.Logger
}

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

	args := r.buildRunArgs(spec)
	r.logger.Debugf("container run: %s %s", r.command, strings.Join(args, " "))

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
				Cause: &CommandError{Output: stderrStr},
			}
		}
		return "", &ContainerStartError{
			Image: spec.Image,
			Cause: &CommandError{Output: stderrStr},
		}
	}

	containerID := strings.TrimSpace(stdout.String())
	r.logger.Infof("container started: %s (id=%s)", spec.Image, truncateID(containerID))
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

	args := r.buildInteractiveRunArgs(spec, containerName)
	r.logger.Infof("container run interactive: %s %v", r.command, args)

	cmd := exec.CommandContext(ctx, r.command, args...)
	// stderr goes to discard for now — container logs are captured separately
	cmd.Stderr = io.Discard

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, &ContainerInteractiveStartError{
			Image:     spec.Image,
			Operation: "stdin-pipe",
			Cause:     err,
		}
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
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

	r.logger.Infof("container interactive started: %s (name=%s, pid=%d)",
		spec.Image, containerName, cmd.Process.Pid)

	return &InteractiveProcess{
		ContainerID: containerName,
		Stdin:       stdinPipe,
		Stdout:      stdoutPipe,
	}, nil
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
			Cause:       &CommandError{Output: stderrStr},
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
			Cause:       &CommandError{Output: stderrStr},
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
			Cause:     &CommandError{Output: stderrStr},
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
			Cause:     &CommandError{Output: stderrStr},
		}
	}

	return strings.TrimSpace(stdout.String()) == "true", nil
}

// IsAvailable checks if the container runtime (docker/podman) is installed.
func (r *ExecContainerRuntime) IsAvailable() bool {
	return detectRuntime(r.command)
}

// buildRunArgs constructs the argument list for a detached docker/podman run command.
func (r *ExecContainerRuntime) buildRunArgs(spec *ContainerSpec) []string {
	args := []string{"run", "-d"}

	if spec.Name != "" {
		args = append(args, "--name", spec.Name)
	}

	if spec.AutoRemove {
		args = append(args, "--rm")
	}

	args = r.appendCommonRunArgs(args, spec)

	return args
}

// buildInteractiveRunArgs constructs the argument list for an interactive
// docker/podman run command. Interactive containers use -i (stdin open)
// and --rm (auto-remove on exit) instead of -d (detached).
func (r *ExecContainerRuntime) buildInteractiveRunArgs(spec *ContainerSpec, containerName string) []string {
	args := []string{"run", "-i", "--rm"}

	args = append(args, "--name", containerName)

	args = r.appendCommonRunArgs(args, spec)

	return args
}

// appendCommonRunArgs appends container configuration flags shared between
// detached and interactive run modes: network, workdir, env, mounts, ports,
// labels, GPU, image, command, and args.
func (r *ExecContainerRuntime) appendCommonRunArgs(args []string, spec *ContainerSpec) []string {
	if spec.User != "" {
		args = append(args, "--user", spec.User)
	}

	if spec.Network != "" {
		args = append(args, "--network", spec.Network)
	}

	if spec.WorkDir != "" {
		args = append(args, "--workdir", spec.WorkDir)
	}

	// Sort env keys for deterministic output.
	if len(spec.Env) > 0 {
		keys := make([]string, 0, len(spec.Env))
		for k := range spec.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			args = append(args, "-e", k+"="+spec.Env[k])
		}
	}

	for _, m := range spec.Mounts {
		args = append(args, "-v", m.Format())
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

	args = append(args, spec.Image)

	if len(spec.Command) > 0 {
		args = append(args, spec.Command...)
	}

	if len(spec.Args) > 0 {
		args = append(args, spec.Args...)
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
	return strings.Contains(lower, "no such container") ||
		strings.Contains(lower, "no such object") ||
		strings.Contains(lower, "not found")
}

// truncateID shortens a container ID for log output.
func truncateID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// CommandError wraps the stderr output from a failed exec.Command.
type CommandError struct {
	Output string
}

// Error implements the error interface.
func (e *CommandError) Error() string {
	return e.Output
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
