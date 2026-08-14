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

import "context"

// Compile-time interface check: the ACP backend can reconstitute persistent
// CLI-mode sessions.
var _ SessionResumer = (*ACPBackend)(nil)

// Resume reconstitutes a persistent CLI-mode session whose subprocess is gone,
// registering the replacement under the original session ID.
//
// Two mechanisms are available, selected by ACPBackendConfig.NativeSessionResume:
//
//   - Native: the agent CLI reloads the conversation from its own on-disk
//     session store ("--resume <id>"). Nothing is re-sent, and tool calls and
//     results are restored along with the messages.
//   - Replay: a fresh process is primed with the transcript the caller read
//     back from the durable session event log. The priming message replaces
//     the spawn handshake, so the restore costs no extra turn.
//
// A backend that can do neither returns SessionResumeUnsupportedError; a
// replay with no readable transcript returns SessionTranscriptUnavailableError.
// Neither case silently produces a context-free agent wearing the old ID.
func (b *ACPBackend) Resume(ctx context.Context, req *ResumeRequest) (*ResumeResult, error) {
	if req == nil {
		return nil, &InputValidationError{Field: "request", Message: "cannot be nil"}
	}
	if req.SessionID == "" {
		return nil, &InputValidationError{Field: "session_id", Message: "cannot be empty"}
	}
	if req.Spawn == nil {
		return nil, &InputValidationError{Field: "spawn", Message: "cannot be nil"}
	}
	if req.Spawn.WorkDir == "" {
		return nil, &InputValidationError{Field: "work_dir", Message: "cannot be empty"}
	}

	// A one-shot session starts a fresh process per prompt and holds no
	// conversation in a process, so there is nothing to reconstitute — and
	// nothing lost, which is why the one-shot path rotates accounts in place
	// instead (see RotateAccount).
	if !req.Spawn.Persistent {
		return nil, &SessionResumeUnsupportedError{
			Backend:   b.Type(),
			SessionID: req.SessionID,
			Reason:    "a non-persistent session holds no conversation to restore",
		}
	}

	start, startErr := b.resumeStarter(ctx, req)
	if startErr != nil {
		return nil, startErr
	}

	if _, live := b.sessions.Load(req.SessionID); live {
		return nil, &SessionResumeError{
			SessionID: req.SessionID,
			Cause: &SessionAlreadyLiveError{
				SessionID: req.SessionID,
			},
		}
	}

	plan, result, err := b.planResume(req)
	if err != nil {
		return nil, err
	}

	session, spawnErr := b.provisionAndSpawn(ctx, req.Spawn, func() (*Session, error) {
		return start(plan)
	})
	if spawnErr != nil {
		return nil, &SessionResumeError{
			SessionID: req.SessionID,
			Mode:      result.Mode,
			Cause:     spawnErr,
		}
	}

	result.Session = session
	return result, nil
}

// resumeStarter selects the spawn path that reconstitutes req's session, or
// reports that this backend has none for it.
//
// Two shapes can be reconstituted, and they are chosen by the same rules
// dispatchSpawn uses to create them in the first place, so a session always
// comes back through the path it went out on:
//
//   - persistent stream-json CLI — the original resumable shape.
//   - sandboxed container JSON-RPC — reconstituted by replay into a FRESH
//     container. This is what stops sandboxing and account rotation from being
//     mutually exclusive: a sandboxed session's credential is a bind-mounted
//     per-spawn directory, so the only way to move it onto a different account
//     is to build a new container around a new directory, which is exactly
//     what provisionAndSpawn does for every spawn (materializeAccountFiles
//     mints a fresh tree per resolution, and the outgoing one is released by
//     the Close that preceded this resume). Per-session credential isolation
//     is therefore preserved by construction: no two containers ever share a
//     materialized directory.
func (b *ACPBackend) resumeStarter(ctx context.Context, req *ResumeRequest) (func(spawnPlan) (*Session, error), error) {
	if b.config.CLIMode {
		return func(plan spawnPlan) (*Session, error) {
			return b.spawnCLI(ctx, req.Spawn, plan)
		}, nil
	}

	if req.Spawn.EffectiveSandboxMode() == SandboxModeRequired {
		if b.containerRuntime == nil {
			return nil, &SessionResumeUnsupportedError{
				Backend:   b.Type(),
				SessionID: req.SessionID,
				Reason:    "the session is sandboxed but the backend has no container runtime configured",
			}
		}
		return func(plan spawnPlan) (*Session, error) {
			return b.spawnContainerJSONRPC(ctx, req.Spawn, plan)
		}, nil
	}

	// An unsandboxed JSON-RPC agent runs directly on the host and exposes no
	// way to restore a prior conversation, so it is left exactly as it was.
	return nil, &SessionResumeUnsupportedError{
		Backend:   b.Type(),
		SessionID: req.SessionID,
		Reason:    "only persistent stream-json CLI and sandboxed container sessions can be reconstituted",
	}
}

// planResume selects the resume mechanism and builds the spawn plan that
// implements it. It never returns a plan that would start an agent with less
// context than the caller asked for: a replay with an unreadable transcript is
// an error, not a plain spawn.
func (b *ACPBackend) planResume(req *ResumeRequest) (spawnPlan, *ResumeResult, error) {
	plan := spawnPlan{registerID: req.SessionID}

	// Native resume is the agent CLI's own "--resume <id>" flag. A container
	// JSON-RPC agent has no equivalent and its on-disk session store died with
	// the previous container, so a sandboxed session always replays.
	if b.config.NativeSessionResume && b.config.CLIMode {
		plan.resumeID = req.SessionID
		return plan, &ResumeResult{Mode: ResumeModeNative}, nil
	}

	if req.Transcript == nil {
		return spawnPlan{}, nil, &SessionTranscriptUnavailableError{
			SessionID: req.SessionID,
			Reason:    "backend resumes by transcript replay but no transcript was supplied",
		}
	}

	result := &ResumeResult{
		Mode:          ResumeModeReplay,
		ReplayedTurns: len(req.Transcript.Turns),
		OmittedTurns:  req.Transcript.Omitted,
	}
	// A session that was spawned but never prompted has an empty transcript.
	// A fresh process is then a faithful restoration of an empty conversation,
	// so it starts on the ordinary handshake rather than a priming message
	// that would describe a history that does not exist.
	if !req.Transcript.Empty() {
		plan.priming = req.Transcript.PrimingMessage()
	}
	return plan, result, nil
}

// SessionAlreadyLiveError reports a resume attempted against a session that
// still has a live agent process. It is a caller error: the session needs no
// reconstitution, and replacing the process would discard the context it holds.
type SessionAlreadyLiveError struct {
	// SessionID identifies the session.
	SessionID string
}

// Error implements the error interface.
func (e *SessionAlreadyLiveError) Error() string {
	return "acp: session is already live: " + e.SessionID
}
