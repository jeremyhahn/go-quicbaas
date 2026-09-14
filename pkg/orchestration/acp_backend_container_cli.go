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
	"path/filepath"
	"time"
)

// =============================================================================
// The containerized CLI session
// =============================================================================
//
// This is the path that makes `--sandbox-policy required` mean something for a
// CLI-mode backend. Before it existed the backend had exactly one way to run
// the agent — as a child of the daemon — so dispatchSpawn's Required branch had
// nowhere to send a CLI-mode spawn and (worse, until #741) never looked.
//
// It is the SAME protocol spawnCLI speaks. The agent CLI is the container's
// command, the argument vector is the one cliArgs builds for the host, and the
// stream-json exchange is openStreamJSON's — the only thing that changes is
// which side of a container boundary the stdio crosses. That symmetry is the
// point: an operator turning the sandbox on must get the agent they already
// had, or they will turn it back off.
//
// WHAT THE CONTAINER GETS, AND HOW:
//
//   credential   as container ENVIRONMENT, carried by the runtime's private
//                0600 --env-file (container_env_file.go). Never a bind-mounted
//                file, never a value in argv, so it reaches neither the host
//                filesystem the agent can read nor /proc/<pid>/cmdline.
//   workspace    bind-mounted at /workspace, from a host path the mount policy
//                authorized before the backend was ever called
//                (Runtime.authorizeMounts / authorizeSandboxMounts).
//   network      the backend's configured SandboxNetwork, exactly as the
//                JSON-RPC path — both go through sandboxSpec.
//   identity     the daemon's own uid:gid, so what the agent writes through the
//                bind mount is owned by the daemon rather than by root.
//
// WHAT IT DOES NOT GET: a supervisor. The `docker run -i` process belongs to
// the container runtime, which hands back pipes and a container id and keeps
// the process; there is no *exec.Cmd here for procReaper to Wait on. That is
// the same arrangement spawnContainerJSONRPC has always had — the container's
// life is ended by procCancel from Close (which fires --rm), and its death is
// observed as EOF on the stdio rather than as an exit code.

// spawnContainerCLI starts a persistent stream-json agent as the command of a
// container and establishes the session over the container's stdio pipes.
//
// The plan selects between a plain spawn and a resume, exactly as it does for
// spawnCLI; see spawnPlan.
func (b *ACPBackend) spawnContainerCLI(ctx context.Context, req *SpawnRequest, plan spawnPlan) (*Session, error) {
	image := req.ContainerImage
	if image == "" {
		return nil, &InputValidationError{Field: "container_image", Message: "required for sandboxed sessions"}
	}

	model := req.Model
	if model == "" {
		model = b.config.DefaultModel
	}

	// The agent binary by NAME, not by the host path the daemon found it at:
	// the image installs it somewhere of its own choosing and puts it on PATH
	// (the Software Factory worker image installs claude-code globally via
	// npm). The image's own entrypoint is cleared by sandboxSpec for the same
	// reason — the command here is ours.
	command := append([]string{filepath.Base(b.config.Command)}, b.cliArgs(req, plan, model)...)

	spec, specErr := b.sandboxSpec(req, image, command)
	if specErr != nil {
		return nil, specErr
	}

	// Decouple the container lifetime from the spawning request, exactly as
	// spawnCLI and spawnContainerJSONRPC do. RunInteractive binds `docker run
	// -i --rm` to its ctx via exec.CommandContext, and gRPC cancels the Spawn
	// RPC ctx the moment the handler returns — so passing the request ctx here
	// would kill the container immediately after Spawn and the follow-up
	// Prompt would find a dead stdin. context.WithoutCancel keeps the
	// request-scoped values (tenant, trace) and drops only the cancellation;
	// procCancel goes onto the session and is fired only by Close. The opening
	// exchange below still honours the caller's ctx for spawn-time timeout.
	procCtx, procCancel := context.WithCancel(context.WithoutCancel(ctx))

	proc, runErr := b.containerRuntime.RunInteractive(procCtx, spec)
	if runErr != nil {
		procCancel()
		return nil, &SpawnError{Command: image, Cause: runErr}
	}

	// teardown aborts a partially-spawned container: it closes the agent's
	// stdin, cancels the container process context (killing `docker run` and
	// triggering --rm), and removes the private env file that process was
	// reading its environment from.
	teardown := func() {
		_ = proc.Stdin.Close()
		procCancel()
		if proc.Cleanup != nil {
			proc.Cleanup()
		}
	}

	open, openErr := b.openStreamJSON(ctx, &streamJSONProc{
		stdin:  proc.Stdin,
		stdout: proc.Stdout,
		gone: func() error {
			return &ContainerStreamClosedError{
				ContainerID: proc.ContainerID,
				Image:       image,
				Operation:   "handshake",
			}
		},
	}, plan, model, teardown)
	if openErr != nil {
		return nil, openErr
	}

	now := time.Now().UnixNano()
	sess := &acpSession{
		id:             open.sessionID,
		tenantID:       StorageTenant(ctx),
		procCancel:     procCancel,
		stdin:          proc.Stdin,
		stdout:         open.scanner,
		projectID:      req.ProjectID,
		model:          open.model,
		workDir:        req.WorkDir,
		env:            req.Env,
		systemPrompt:   req.SystemPrompt,
		extraArgs:      req.ExtraArgs,
		createdAt:      now,
		containerID:    proc.ContainerID,
		containerImage: image,

		// The container process outlives this call, so the env file it reads
		// from is released by Close — not here.
		envFileCleanup: proc.Cleanup,
	}
	sess.lastActivity.Store(now)
	b.storeSession(sess)

	return &Session{
		ID:        open.sessionID,
		ProjectID: req.ProjectID,
		Backend:   b.Type(),
		Model:     open.model,
		Status:    SessionStateActive,
		WorkDir:   req.WorkDir,
		CreatedAt: now,

		// THE CONTAINER ID IS THE POINT. Without it the sdlc runner cannot run
		// the checkout, the verification or the publish inside the sandbox, and
		// refuses the worker as "runs no container" — which is how #741 was
		// found.
		ContainerID: proc.ContainerID,
	}, nil
}
