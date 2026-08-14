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

import "context"

// =============================================================================
// In-place account rotation
//
// A live session's credential reaches its agent by one of two mechanisms, and
// only one of them can be changed without restarting anything:
//
//   - PROCESS ENVIRONMENT, captured at exec time. A persistent stream-json
//     subprocess and a JSON-RPC agent both hold theirs for as long as they
//     live, so moving them onto a different account means a new process — that
//     is the close-then-resume path in orchestrator_account_rotation.go.
//
//   - SESSION-HELD CONFIGURATION, re-read per prompt. The one-shot CLI path
//     stores its environment on the session and rebuilds the child process's
//     env from it on every prompt (see promptOneShotCLI), so swapping the
//     stored map IS the rotation. No process dies, no transcript is replayed,
//     and no context is lost, because a one-shot session carries no context in
//     a process to begin with.
//
// RotateAccount implements the second case and declines the first, so the
// caller can try the cheap mechanism unconditionally and fall back to the
// expensive one only when the session actually needs it.
// =============================================================================

// SessionAccountRotator is implemented by a backend that can move one of its
// live sessions onto a different pooled provider account without a resume.
//
// A backend that cannot returns SessionRotateUnsupportedError, which is a
// routing signal rather than a failure: the orchestrator answers it by closing
// the session and resuming it against a freshly selected account.
type SessionAccountRotator interface {
	// RotateAccount re-resolves req.Spawn's account and installs the result on
	// the live session named by req.SessionID.
	RotateAccount(ctx context.Context, req *RotateAccountRequest) (*RotateAccountResult, error)
}

// RotateAccountRequest asks a backend to move one live session onto a
// different pooled account.
type RotateAccountRequest struct {
	// SessionID is the live session to move. It keeps this identifier.
	SessionID string

	// Spawn carries the resolve scope for the replacement account: the
	// session's rotation policy with the outgoing account excluded. It is
	// built by rotationSpawnRequestFromRecord, never by a transport caller.
	Spawn *SpawnRequest
}

// RotateAccountResult reports which account a session was moved onto.
type RotateAccountResult struct {
	// AccountID is the replacement account now in force.
	AccountID string

	// Strategy names the policy that selected it.
	Strategy string
}

// SessionRotateUnsupportedError reports that a backend cannot move a session
// onto a different account in place.
//
// It is deliberately distinct from a failure: the account subsystem is healthy
// and a replacement may well be available, the session's credential simply
// lives somewhere that cannot be rewritten under a running process.
type SessionRotateUnsupportedError struct {
	// Backend names the backend that declined.
	Backend string

	// SessionID identifies the session.
	SessionID string

	// Reason states why an in-place rotation is impossible.
	Reason string
}

// Error implements the error interface.
func (e *SessionRotateUnsupportedError) Error() string {
	return "acp: in-place account rotation unsupported: backend=" + e.Backend +
		" session=" + e.SessionID + ": " + e.Reason
}

// Compile-time interface check: the ACP backend can rotate the accounts of its
// one-shot CLI sessions in place.
var _ SessionAccountRotator = (*ACPBackend)(nil)

// RotateAccount implements SessionAccountRotator.
//
// The swap is atomic from a prompt's point of view: promptOneShotCLI reads
// sess.env under no lock of its own, so the map is REPLACED rather than mutated
// in place. A prompt that captured the old map keeps a coherent old-account
// environment for the rest of that one exec instead of observing a half-swapped
// mixture of two credentials.
func (b *ACPBackend) RotateAccount(ctx context.Context, req *RotateAccountRequest) (*RotateAccountResult, error) {
	if req == nil {
		return nil, &InputValidationError{Field: "request", Message: "cannot be nil"}
	}
	if req.SessionID == "" {
		return nil, &InputValidationError{Field: "session_id", Message: "cannot be empty"}
	}
	if req.Spawn == nil {
		return nil, &InputValidationError{Field: "spawn", Message: "cannot be nil"}
	}

	val, ok := b.sessions.Load(req.SessionID)
	if !ok {
		b.logLookupMiss("rotate_account", req.SessionID)
		return nil, &SessionNotFoundError{SessionID: req.SessionID}
	}
	sess, ok := val.(*acpSession)
	if !ok {
		return nil, &SessionNotFoundError{SessionID: req.SessionID}
	}

	if sess.cmd != nil || sess.transport != nil {
		return nil, &SessionRotateUnsupportedError{
			Backend:   b.Type(),
			SessionID: req.SessionID,
			Reason:    "the session holds a running process whose environment was fixed at exec time; it must be closed and resumed",
		}
	}

	// Seed the resolve with the session's CURRENT environment minus the
	// outgoing account's contribution. Retracting first is what keeps a
	// credential the replacement account happens not to overwrite from
	// surviving the rotation.
	sess.mu.Lock()
	seed := make(map[string]string, len(sess.env))
	for k, v := range sess.env {
		seed[k] = v
	}
	for _, key := range sess.accountEnvKeys {
		delete(seed, key)
	}
	sess.mu.Unlock()

	spawn := *req.Spawn
	spawn.Env = seed

	account, err := b.resolveAccount(ctx, &spawn)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, &SessionNotRotatableError{
			SessionID: req.SessionID,
			Reason:    "the rotation request asked for no account selection",
		}
	}

	sess.mu.Lock()
	sess.env = spawn.Env
	sess.accountID = account.AccountID
	sess.accountEnvKeys = account.EnvKeys
	previous := sess.accountCleanup
	sess.accountCleanup = account.cleanup
	sess.mu.Unlock()

	if previous != nil {
		previous()
	}

	return &RotateAccountResult{AccountID: account.AccountID, Strategy: account.Strategy}, nil
}
