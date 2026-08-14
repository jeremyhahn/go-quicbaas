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
