// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"strings"
)

// =============================================================================
// The container a job's work happens in
// =============================================================================
//
// JobContext describes a job to the code that judges it, and every field on
// it is a DAEMON-side fact: the prepared directory, the artifacts, the
// operator's guard. That is the whole view a validator has, and it is why a
// command criterion has only ever been able to run on the daemon.
//
// A containerized job's work is not on the daemon. Its checkout, its
// toolchain and its environment are inside the worker's container, and a
// command that judges the work has to run where the work is — in the same
// environment the work was done in, and behind the boundary that exists so a
// tenant's build is not a process on the daemon host.
//
// JobContainer puts that one fact on the context: WHICH session, WHAT the
// container calls the checkout, and the single door into it. It is the same
// door #608 opened for the delivery's git — ContainerRuntime.Exec, reached
// through the session — narrowed to one directory, so a validator holding
// one cannot name a directory the daemon would have run in.

// ContainerExecFunc runs one command inside a session's container. JobRun
// binds one to its own Exec; a test binds one to its scripted container.
type ContainerExecFunc func(ctx context.Context, spec *ExecSpec) (*ExecResult, error)

// JobContainer is the containerized worker session a job's work lives in.
type JobContainer struct {
	// SessionID is the session whose container commands run in. It names the
	// container in evidence and in errors; it is not how the command gets
	// there.
	SessionID string

	// WorkDir is the checkout as the CONTAINER sees it — the directory its
	// own entrypoint cloned into, never the host side of the bind mount.
	WorkDir string

	// exec is the door. It is unexported so the only way to reach the
	// container is Exec, which pins the working directory.
	exec ContainerExecFunc
}

// NewJobContainer binds a session's container to the checkout inside it.
// Every argument is required: a container missing any one of them is a
// container nothing could run a command in.
func NewJobContainer(sessionID, workDir string, exec ContainerExecFunc) (*JobContainer, error) {
	switch {
	case strings.TrimSpace(sessionID) == "":
		return nil, &InputValidationError{Field: "session_id", Message: "a command runs in a named session's container"}
	case strings.TrimSpace(workDir) == "":
		return nil, &InputValidationError{Field: "work_dir", Message: "the container's own path to the checkout is required"}
	case exec == nil:
		return nil, &InputValidationError{Field: "exec", Message: "cannot be nil"}
	}
	return &JobContainer{SessionID: sessionID, WorkDir: workDir, exec: exec}, nil
}

// Exec runs one command in the container's checkout and reports what it did.
//
// THE WORKING DIRECTORY IS NOT THE CALLER'S TO CHOOSE. It is the checkout
// this container was bound to, so a caller cannot name the daemon's copy of
// it — which is the entire property this type exists to hold. A non-zero
// exit is reported on the result exactly as ContainerRuntime.Exec reports
// it; an error means the command could not be run.
func (c *JobContainer) Exec(ctx context.Context, cmd []string) (*ExecResult, error) {
	if c == nil || c.exec == nil {
		return nil, &InputValidationError{Field: "container", Message: "this job spawned no containerized session"}
	}
	return c.exec(ctx, &ExecSpec{Cmd: cmd, WorkDir: c.WorkDir})
}
