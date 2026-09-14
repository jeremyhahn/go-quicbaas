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

import "errors"

// Sentinel errors for container runtime operations.
var (
	// ErrContainerRuntimeNotAvailable indicates no docker or podman binary was found.
	ErrContainerRuntimeNotAvailable = errors.New("container: runtime not available")

	// ErrContainerStart indicates a failure to start a container.
	ErrContainerStart = errors.New("container: start failed")

	// ErrContainerStop indicates a failure to stop a container.
	ErrContainerStop = errors.New("container: stop failed")

	// ErrContainerNotFound indicates the container does not exist.
	ErrContainerNotFound = errors.New("container: not found")

	// ErrContainerImagePull indicates the image could not be pulled.
	ErrContainerImagePull = errors.New("container: image pull failed")

	// ErrContainerInteractiveStart indicates a failure to start an interactive container.
	ErrContainerInteractiveStart = errors.New("container: interactive start failed")

	// ErrContainerEnvFile indicates the private env file that carries a
	// container's environment could not be written.
	ErrContainerEnvFile = errors.New("container: env file failed")

	// ErrContainerExec indicates a command could not be run inside a
	// running container. It never means the command RAN and failed — that
	// is an exit status on ExecResult.
	ErrContainerExec = errors.New("container: exec failed")

	// ErrContainerStreamClosed indicates a containerized agent stopped
	// answering on its stdio before the exchange in flight completed.
	ErrContainerStreamClosed = errors.New("container: agent stream closed")

	// ErrContainerSweep indicates the startup sweep of the containers a
	// previous run left behind could not be completed.
	ErrContainerSweep = errors.New("container: orphan sweep failed")
)

// ContainerRuntimeNotAvailableError indicates no docker or podman binary was found.
type ContainerRuntimeNotAvailableError struct {
	Message string
}

// Error implements the error interface.
func (e *ContainerRuntimeNotAvailableError) Error() string {
	return ErrContainerRuntimeNotAvailable.Error() + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ContainerRuntimeNotAvailableError) Unwrap() error {
	return ErrContainerRuntimeNotAvailable
}

// ContainerStartError indicates a failure to start a container.
type ContainerStartError struct {
	Image string
	Cause error
}

// Error implements the error interface.
func (e *ContainerStartError) Error() string {
	msg := ErrContainerStart.Error() + ": " + e.Image
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns both the sentinel and cause for errors.Is/As support.
func (e *ContainerStartError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrContainerStart, e.Cause}
	}
	return []error{ErrContainerStart}
}

// ContainerStopError indicates a failure to stop a container.
type ContainerStopError struct {
	ContainerID string
	Cause       error
}

// Error implements the error interface.
func (e *ContainerStopError) Error() string {
	msg := ErrContainerStop.Error() + ": " + e.ContainerID
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns both the sentinel and cause for errors.Is/As support.
func (e *ContainerStopError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrContainerStop, e.Cause}
	}
	return []error{ErrContainerStop}
}

// ContainerNotFoundError indicates the container does not exist.
type ContainerNotFoundError struct {
	ContainerID string
}

// Error implements the error interface.
func (e *ContainerNotFoundError) Error() string {
	return ErrContainerNotFound.Error() + ": " + e.ContainerID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ContainerNotFoundError) Unwrap() error {
	return ErrContainerNotFound
}

// ContainerImagePullError indicates the image could not be pulled.
type ContainerImagePullError struct {
	Image string
	Cause error
}

// Error implements the error interface.
func (e *ContainerImagePullError) Error() string {
	msg := ErrContainerImagePull.Error() + ": " + e.Image
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns both the sentinel and cause for errors.Is/As support.
func (e *ContainerImagePullError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrContainerImagePull, e.Cause}
	}
	return []error{ErrContainerImagePull}
}

// ContainerInteractiveStartError indicates a failure to start an interactive container.
type ContainerInteractiveStartError struct {
	Image     string
	Operation string
	Cause     error
}

// Error implements the error interface.
func (e *ContainerInteractiveStartError) Error() string {
	msg := ErrContainerInteractiveStart.Error() + ": " + e.Image
	if e.Operation != "" {
		msg += ": " + e.Operation
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns both the sentinel and cause for errors.Is/As support.
func (e *ContainerInteractiveStartError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrContainerInteractiveStart, e.Cause}
	}
	return []error{ErrContainerInteractiveStart}
}

// ContainerEnvFileError indicates the private --env-file carrying a
// container's environment could not be materialized. The launch fails: there
// is deliberately no fallback to inline "-e KEY=VALUE" arguments, which would
// publish every credential in the environment to /proc/<pid>/cmdline.
type ContainerEnvFileError struct {
	// Operation names the failed step ("mkdir" or "write").
	Operation string
	Cause     error
}

// Error implements the error interface. Neither the environment keys nor their
// values are included: only the failed operation and the OS cause.
func (e *ContainerEnvFileError) Error() string {
	msg := ErrContainerEnvFile.Error()
	if e.Operation != "" {
		msg += ": " + e.Operation
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns both the sentinel and cause for errors.Is/As support.
func (e *ContainerEnvFileError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrContainerEnvFile, e.Cause}
	}
	return []error{ErrContainerEnvFile}
}

// ContainerExecError indicates a command could not be run inside a running
// container: the runtime refused, the container was unreachable, or the
// context ended before the command answered.
//
// A command that ran and exited non-zero is NOT this error — its status is
// on ExecResult, because a non-zero exit is frequently the answer a caller
// asked for.
type ContainerExecError struct {
	ContainerID string
	Cause       error
}

// Error implements the error interface. The command's own output is never
// rendered here; it stays on ExecResult.Stderr for a caller with somewhere
// safe to put it.
func (e *ContainerExecError) Error() string {
	msg := ErrContainerExec.Error() + ": " + truncateID(e.ContainerID)
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns both the sentinel and cause for errors.Is/As support.
func (e *ContainerExecError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrContainerExec, e.Cause}
	}
	return []error{ErrContainerExec}
}

// ContainerStreamClosedError reports that a containerized agent's stdout ended
// before the exchange the caller was waiting for completed — the container is
// gone, or its command never started.
//
// IT CARRIES NO EXIT CODE, deliberately. The `docker run -i` process belongs to
// the container runtime, not to this backend, so unlike the host subprocess
// path (StreamJSONProcessExitedError) there is no ProcessState here to read one
// from. Reporting a zero would be a guess, and "the agent exited successfully"
// is the single most misleading guess available. What IS known is the
// container, the image, and the exchange that was in flight, and that is what
// an operator needs to go read `docker logs` for the rest.
type ContainerStreamClosedError struct {
	// ContainerID is the container whose stdio ended.
	ContainerID string

	// Image is what that container was running.
	Image string

	// Operation names the exchange in flight ("handshake", "prompt").
	Operation string
}

// Error implements the error interface.
func (e *ContainerStreamClosedError) Error() string {
	msg := ErrContainerStreamClosed.Error() + ": " + e.Image
	if e.ContainerID != "" {
		msg += " (" + truncateID(e.ContainerID) + ")"
	}
	if e.Operation != "" {
		msg += ": " + e.Operation
	}
	return msg
}

// Unwrap returns the sentinel for errors.Is support.
func (e *ContainerStreamClosedError) Unwrap() error { return ErrContainerStreamClosed }

// ContainerSweepRefusedError reports a sweep that was declined before it
// enumerated anything.
//
// It is a refusal rather than a no-op because the only way to sweep without an
// owner would be to sweep every container on the host, and a startup that
// cannot say which containers are its own must say so rather than guess.
type ContainerSweepRefusedError struct {
	// Reason says what the sweep would not do and why.
	Reason string
}

// Error implements the error interface.
func (e *ContainerSweepRefusedError) Error() string {
	return ErrContainerSweep.Error() + ": refused: " + e.Reason
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ContainerSweepRefusedError) Unwrap() error { return ErrContainerSweep }

// ContainerSweepError reports a sweep that started and could not finish: the
// runtime would not enumerate the owner's containers, or an identified orphan
// would not be removed.
//
// A sweep that half-ran is reported rather than swallowed, for the reason
// go-quicbaas#676 gives for the signing-socket sweep: an orphan that survives
// its sweep accumulates without bound, and a startup that logged nothing about
// it would leave an operator with no way to know.
type ContainerSweepError struct {
	// Owner is the label value the sweep was claiming containers by.
	Owner string

	// ContainerID is the orphan that refused to be removed, empty when the
	// enumeration itself failed.
	ContainerID string

	// Cause is the runtime's own refusal.
	Cause error
}

// Error implements the error interface.
func (e *ContainerSweepError) Error() string {
	msg := ErrContainerSweep.Error() + ": owner " + e.Owner
	if e.ContainerID != "" {
		msg += ": container " + truncateID(e.ContainerID)
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns both the sentinel and cause for errors.Is/As support.
func (e *ContainerSweepError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrContainerSweep, e.Cause}
	}
	return []error{ErrContainerSweep}
}
