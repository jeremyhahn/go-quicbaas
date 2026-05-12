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
	"context"
	"io"
)

// ContainerRuntime abstracts OCI container operations.
// The default implementation shells out to docker or podman via exec.Command.
// go-trusted-platform can provide an advanced implementation with
// TPM attestation, OPA policies, and podman rootless support.
type ContainerRuntime interface {
	// Run creates and starts a detached container. Returns the container ID.
	Run(ctx context.Context, spec *ContainerSpec) (string, error)

	// RunInteractive starts a container with stdin/stdout pipes for interactive
	// stdio communication (e.g., ACP JSON-RPC). The container runs in
	// foreground mode (-i) and the caller gets pipes to communicate with the
	// container's entrypoint process. Returns an InteractiveProcess that owns
	// the pipes and container lifecycle.
	RunInteractive(ctx context.Context, spec *ContainerSpec) (*InteractiveProcess, error)

	// Stop stops a running container.
	Stop(ctx context.Context, containerID string) error

	// Remove removes a container (force removes if running).
	Remove(ctx context.Context, containerID string) error

	// Logs returns the container's stdout/stderr logs.
	Logs(ctx context.Context, containerID string) (io.ReadCloser, error)

	// IsRunning checks if a container is running.
	IsRunning(ctx context.Context, containerID string) (bool, error)

	// IsAvailable checks if the container runtime (docker/podman) is installed.
	IsAvailable() bool

	// Pull fetches a container image from the remote registry. Used by agent
	// daemons at startup to pre-load the image map so first-spawn latency is
	// not gated on a network round-trip. Returns ContainerImagePullError on
	// failure; nil on success or when the image is already cached locally.
	Pull(ctx context.Context, image string) error
}

// InteractiveProcess represents a running interactive container with stdio pipes.
// The caller uses Stdin and Stdout to communicate with the container's entrypoint
// process over newline-delimited JSON-RPC.
type InteractiveProcess struct {
	// ContainerID is the container name used to identify the container for
	// stop/remove operations. For interactive containers, this is always the
	// generated name passed via --name.
	ContainerID string

	// Stdin is a writer connected to the container's stdin. Close it to
	// signal EOF to the container process.
	Stdin io.WriteCloser

	// Stdout is a reader connected to the container's stdout.
	Stdout io.ReadCloser
}

// ContainerSpec defines a container to run. No external library dependencies.
type ContainerSpec struct {
	Name       string            `json:"name"`
	Image      string            `json:"image"`
	Env        map[string]string `json:"env,omitempty"`
	Mounts     []ContainerMount  `json:"mounts,omitempty"`
	Ports      []string          `json:"ports,omitempty"` // "host:container"
	WorkDir    string            `json:"work_dir,omitempty"`
	Entrypoint []string          `json:"entrypoint,omitempty"` // override container entrypoint
	Command    []string          `json:"command,omitempty"`
	Args       []string          `json:"args,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	AutoRemove bool              `json:"auto_remove,omitempty"` // --rm flag
	Network    string            `json:"network,omitempty"`     // host, bridge, or custom
	GPUEnabled bool              `json:"gpu_enabled,omitempty"` // --gpus all
	User       string            `json:"user,omitempty"`        // --user uid:gid
}

// ContainerMount defines a bind mount between host and container.
type ContainerMount struct {
	Source   string `json:"source"`   // host path
	Target   string `json:"target"`   // container path
	ReadOnly bool   `json:"readonly"` // :ro or :rw
}

// Validate checks that the ContainerSpec has all required fields.
func (s *ContainerSpec) Validate() error {
	if s.Image == "" {
		return &InputValidationError{Field: "image", Message: "cannot be empty"}
	}
	for i, m := range s.Mounts {
		if err := m.Validate(); err != nil {
			return &InputValidationError{
				Field:   "mounts[" + intToStr(i) + "]",
				Message: err.Error(),
			}
		}
	}
	return nil
}

// Validate checks that the ContainerMount has all required fields.
func (m *ContainerMount) Validate() error {
	if m.Source == "" {
		return &InputValidationError{Field: "source", Message: "cannot be empty"}
	}
	if m.Target == "" {
		return &InputValidationError{Field: "target", Message: "cannot be empty"}
	}
	return nil
}

// Format returns the mount as a docker-compatible volume string.
// Format: source:target[:ro].
func (m *ContainerMount) Format() string {
	s := m.Source + ":" + m.Target
	if m.ReadOnly {
		s += ":ro"
	}
	return s
}

// intToStr converts an integer to a string without importing strconv.
func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
