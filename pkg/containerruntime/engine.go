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

package containerruntime

import (
	"context"
	"io"
	"os"
	"time"
)

// The Runtime drives three in-process backends through the narrow interfaces
// below. In production they are runc libcontainer (containerEngine),
// go.podman.io/image/v5 (imageStore) and go.podman.io/storage (rootfsStore),
// built by newDefaultBackends on linux with cgo. No binary is looked up or
// executed: the engine re-execs the running program as the container's init
// (see RunContainerInit).

// processSpec describes one process to run inside a container: the init
// process at start, or an extra process for exec and attach.
type processSpec struct {
	Args []string
	Env  []string
	Cwd  string
	User ociUser

	// TTY gives the process a pseudo-terminal, read and written through
	// containerProcess.Console. Stdout and Stderr are unused when it is set.
	TTY bool

	Stdout io.Writer
	Stderr io.Writer
}

// containerProcess is a process running inside a container.
type containerProcess interface {
	// Pid is the process ID in the host PID namespace.
	Pid() int

	// Wait blocks until the process exits and returns its exit code. A
	// process killed by a signal reports 128 plus the signal number.
	Wait() (int, error)

	// Signal delivers sig to the process.
	Signal(sig os.Signal) error

	// Console is the pseudo-terminal master of a TTY process, nil otherwise.
	Console() io.ReadWriteCloser
}

// containerEngine creates, runs and destroys containers.
type containerEngine interface {
	// Create registers a container from its OCI configuration. Nothing runs
	// until Start.
	Create(id string, spec *ociConfig) error

	// Start runs the container's init process.
	Start(id string, init processSpec) (containerProcess, error)

	// Exec runs an extra process in a running container.
	Exec(id string, process processSpec) (containerProcess, error)

	// Destroy removes a stopped container's state.
	Destroy(id string) error
}

// imageRecord is an image held in local storage.
type imageRecord struct {
	ID      string
	Names   []string
	Digests []string
	Size    int64
	Created time.Time
}

// imageStore pulls, inspects and removes images in local storage.
type imageStore interface {
	// Pull copies ref from its registry into local storage, honouring the
	// signature policy, and returns the stored image.
	Pull(ctx context.Context, ref string) (imageRecord, error)

	// Inspect returns the stored image ref names, or an error matching
	// ErrImageNotFound.
	Inspect(ctx context.Context, ref string) (imageRecord, error)

	// Remove deletes the stored image ref names. With force, an image that
	// is not stored is not an error.
	Remove(ctx context.Context, ref string, force bool) error
}

// rootfsStore unpacks an image's layers into a container root filesystem.
type rootfsStore interface {
	// Create unpacks imageID into a new read-write layer for containerID,
	// mounts it, and returns the mount point.
	Create(ctx context.Context, containerID, imageID string) (string, error)

	// Remove unmounts and deletes containerID's root filesystem.
	Remove(containerID string) error
}

// backends bundles the Runtime's backends with the function that releases
// them on Close.
type backends struct {
	engine containerEngine
	images imageStore
	rootfs rootfsStore
	close  func() error
}

// withBackends injects backends in place of the defaults NewRuntime builds.
// It is package-internal: tests use it to run the Runtime against in-memory
// doubles.
func withBackends(b *backends) RuntimeOption {
	return func(r *Runtime) {
		r.backends = b
	}
}
