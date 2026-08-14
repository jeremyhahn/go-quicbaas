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
	"errors"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Policy: which sessions may rotate at all
// =============================================================================

func TestSessionRecord_CanRotateAccount(t *testing.T) {
	tests := []struct {
		name         string
		record       *SessionRecord
		want         bool
		reasonSubstr string
	}{
		{"nil record", nil, false, "no session record"},
		{
			name:         "never used the pool",
			record:       &SessionRecord{BackendLocal: true},
			want:         false,
			reasonSubstr: "pooled provider account subsystem",
		},
		{
			name:         "deliberately pinned",
			record:       &SessionRecord{AccountID: "acct-1", BackendLocal: true},
			want:         false,
			reasonSubstr: "pinned to a named account",
		},
		{
			name:   "strategy-selected",
			record: &SessionRecord{AccountID: "acct-1", AccountAuto: true, BackendLocal: true},
			want:   true,
		},
		{
			name:   "pinned but with an explicit rotation pool",
			record: &SessionRecord{AccountID: "acct-1", AccountAllow: []string{"acct-1", "acct-2"}, BackendLocal: true},
			want:   true,
		},
		{
			name:         "remote daemon owns its own rotation",
			record:       &SessionRecord{AccountID: "acct-1", AccountAuto: true},
			want:         false,
			reasonSubstr: "remote daemon",
		},
		{
			name:         "closed",
			record:       &SessionRecord{AccountID: "acct-1", AccountAuto: true, BackendLocal: true, Status: SessionStateClosed},
			want:         false,
			reasonSubstr: "closed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := tt.record.CanRotateAccount()
			if got != tt.want {
				t.Fatalf("CanRotateAccount() = %v (%s), want %v", got, reason, tt.want)
			}
			if tt.reasonSubstr != "" && !strings.Contains(reason, tt.reasonSubstr) {
				t.Fatalf("reason = %q, want it to mention %q", reason, tt.reasonSubstr)
			}
		})
	}
}

func TestSessionRecord_OpenAccountSegment_BackfillsTheOriginalSegment(t *testing.T) {
	created := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC).UnixNano()
	rotatedAt := time.Date(2026, 8, 9, 11, 0, 0, 0, time.UTC).UnixNano()

	record := &SessionRecord{
		SessionID: "s1", CreatedAt: created,
		AccountID: "acct-a", AccountStrategy: LeastUsageAccountStrategyName,
	}
	record.OpenAccountSegment("acct-b", LeastUsageAccountStrategyName, "usage limit reached", rotatedAt)

	if len(record.AccountHistory) != 2 {
		t.Fatalf("AccountHistory = %+v, want two segments", record.AccountHistory)
	}
	first := record.AccountHistory[0]
	if first.AccountID != "acct-a" || first.From != created || first.To != rotatedAt {
		t.Fatalf("first segment = %+v, want acct-a spanning the session start to the rotation", first)
	}
	if first.Reason != "usage limit reached" {
		t.Fatalf("first segment reason = %q, want the rotation reason", first.Reason)
	}
	second := record.AccountHistory[1]
	if second.AccountID != "acct-b" || second.From != rotatedAt || second.To != 0 {
		t.Fatalf("second segment = %+v, want an open acct-b segment from the rotation", second)
	}
	if record.AccountID != "acct-b" {
		t.Fatalf("AccountID = %q, want acct-b — the scalar and the history must move together", record.AccountID)
	}
}

func TestSessionRecord_OpenAccountSegment_IgnoresAnEmptyAccount(t *testing.T) {
	record := &SessionRecord{SessionID: "s1", AccountID: "acct-a"}
	record.OpenAccountSegment("", "x", "y", time.Now().UnixNano())
	if record.AccountID != "acct-a" || len(record.AccountHistory) != 0 {
		t.Fatalf("an empty replacement must be a no-op, got AccountID=%q history=%+v",
			record.AccountID, record.AccountHistory)
	}
}

func TestSessionRecord_Clone_DeepCopiesRotationPolicy(t *testing.T) {
	record := &SessionRecord{
		AccountAllow:   []string{"acct-a"},
		AccountHistory: []AccountAssignment{{AccountID: "acct-a"}},
	}
	clone := record.Clone()
	clone.AccountAllow[0] = "mutated"
	clone.AccountHistory[0].AccountID = "mutated"

	if record.AccountAllow[0] != "acct-a" || record.AccountHistory[0].AccountID != "acct-a" {
		t.Fatal("Clone() shared the rotation policy slices with its source")
	}
}

// =============================================================================
// rotationSpawnRequestFromRecord
// =============================================================================

func TestRotationSpawnRequestFromRecord_ReplacesThePinWithTheSessionPolicy(t *testing.T) {
	record := &SessionRecord{
		SessionID: "s1", WorkDir: "/w", Backend: "claude", Persistent: true,
		AccountID: "acct-a", AccountAuto: true, AccountStrategy: LeastUsageAccountStrategyName,
		AccountAllow: []string{"acct-a", "acct-b"},
	}

	req := rotationSpawnRequestFromRecord(record, "acct-a")

	if req.AccountID != "" {
		t.Fatalf("AccountID = %q, want empty — a rotation must not replay the pin it is moving off", req.AccountID)
	}
	if !req.AccountAuto {
		t.Fatal("AccountAuto = false; a rotation must re-run selection")
	}
	if req.AccountStrategy != LeastUsageAccountStrategyName {
		t.Fatalf("AccountStrategy = %q, want the session's own strategy", req.AccountStrategy)
	}
	if len(req.AccountExclude) != 1 || req.AccountExclude[0] != "acct-a" {
		t.Fatalf("AccountExclude = %v, want [acct-a]", req.AccountExclude)
	}
	if len(req.AccountAllow) != 2 {
		t.Fatalf("AccountAllow = %v, want the session's declared pool", req.AccountAllow)
	}
}

func TestRotationSpawnRequestFromRecord_DropsThePinnedStrategyMarker(t *testing.T) {
	record := &SessionRecord{
		SessionID: "s1", WorkDir: "/w", AccountID: "acct-a",
		AccountStrategy: pinnedAccountStrategyName,
		AccountAllow:    []string{"acct-a", "acct-b"},
	}
	req := rotationSpawnRequestFromRecord(record, "acct-a")
	if req.AccountStrategy != "" {
		t.Fatalf("AccountStrategy = %q; %q is a marker, not a registered strategy, so replaying it would fail the lookup",
			req.AccountStrategy, pinnedAccountStrategyName)
	}
}

// =============================================================================
// End-to-end: the one-shot CLI path rotates in place
// =============================================================================

// TestACPBackend_RotateAccount_OneShot_SwapsTheStoredEnvironment is the cheapest
// rotation there is: a one-shot session rebuilds its child process environment
// from the session on every prompt, so replacing the stored map IS the
// rotation. Nothing is closed and no transcript is replayed.
func TestACPBackend_RotateAccount_OneShot_SwapsTheStoredEnvironment(t *testing.T) {
	backend, store, secrets, _, cleanup := newTestACPBackendWithAccountPool(t)
	defer cleanup()

	ctx := ContextWithTenant(context.Background(), "system")
	seedPoolAccount(t, store, secrets, &Account{ID: "acct-a", TenantID: "system", Provider: AccountProviderClaude})
	seedPoolAccount(t, store, secrets, &Account{ID: "acct-b", TenantID: "system", Provider: AccountProviderClaude})

	// A one-shot CLI session: CLIMode with Persistent unset.
	backend.config.CLIMode = true
	sess, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp", AccountID: "acct-a"})
	if err != nil {
		t.Fatalf("Spawn(): %v", err)
	}
	if sess.AccountID != "acct-a" {
		t.Fatalf("Spawn().AccountID = %q, want acct-a", sess.AccountID)
	}

	live := liveACPSession(t, backend, sess.ID)
	if live.env["CLAUDE_CODE_OAUTH_TOKEN"] != "tok-acct-a" {
		t.Fatalf("session env token = %q, want tok-acct-a", live.env["CLAUDE_CODE_OAUTH_TOKEN"])
	}

	rotated, err := backend.RotateAccount(ctx, &RotateAccountRequest{
		SessionID: sess.ID,
		Spawn: &SpawnRequest{
			WorkDir: "/tmp", AccountAuto: true, AccountExclude: []string{"acct-a"},
		},
	})
	if err != nil {
		t.Fatalf("RotateAccount(): %v", err)
	}
	if rotated.AccountID != "acct-b" {
		t.Fatalf("RotateAccount().AccountID = %q, want acct-b", rotated.AccountID)
	}

	live = liveACPSession(t, backend, sess.ID)
	if got := live.env["CLAUDE_CODE_OAUTH_TOKEN"]; got != "tok-acct-b" {
		t.Fatalf("session env token = %q, want tok-acct-b — the next prompt reads this map", got)
	}
	if got := live.env[accountEnvID]; got != "acct-b" {
		t.Fatalf("%s = %q, want acct-b", accountEnvID, got)
	}
}

// TestACPBackend_RotateAccount_RetractsTheOutgoingCredential proves the swap is
// a REPLACEMENT, not a merge: a variable the outgoing account set and the
// replacement does not must not survive into the next process.
func TestACPBackend_RotateAccount_RetractsTheOutgoingCredential(t *testing.T) {
	backend, store, secrets, _, cleanup := newTestACPBackendWithAccountPool(t)
	defer cleanup()

	ctx := ContextWithTenant(context.Background(), "system")
	if err := store.Create(ctx, &Account{ID: "acct-a", TenantID: "system", Provider: AccountProviderClaude}); err != nil {
		t.Fatalf("Create(acct-a): %v", err)
	}
	if err := secrets.Put(ctx, "system", "acct-a", &AccountCredential{
		Env: map[string]string{"LEGACY_ANTHROPIC_KEY": "acct-a-secret"},
	}); err != nil {
		t.Fatalf("Put(acct-a): %v", err)
	}
	seedPoolAccount(t, store, secrets, &Account{ID: "acct-b", TenantID: "system", Provider: AccountProviderClaude})

	backend.config.CLIMode = true
	sess, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp", AccountID: "acct-a"})
	if err != nil {
		t.Fatalf("Spawn(): %v", err)
	}
	if liveACPSession(t, backend, sess.ID).env["LEGACY_ANTHROPIC_KEY"] != "acct-a-secret" {
		t.Fatal("the first account's env was not projected; the test proves nothing")
	}

	if _, err := backend.RotateAccount(ctx, &RotateAccountRequest{
		SessionID: sess.ID,
		Spawn:     &SpawnRequest{WorkDir: "/tmp", AccountAuto: true, AccountExclude: []string{"acct-a"}},
	}); err != nil {
		t.Fatalf("RotateAccount(): %v", err)
	}

	live := liveACPSession(t, backend, sess.ID)
	if _, present := live.env["LEGACY_ANTHROPIC_KEY"]; present {
		t.Fatalf("the outgoing account's credential survived the rotation: %v", live.env)
	}
	if live.env["CLAUDE_CODE_OAUTH_TOKEN"] != "tok-acct-b" {
		t.Fatalf("replacement credential not projected: %v", live.env)
	}
}

func TestACPBackend_RotateAccount_DeclinesALiveProcess(t *testing.T) {
	backend, store, secrets, _, cleanup := newTestACPBackendWithAccountPool(t)
	defer cleanup()

	ctx := ContextWithTenant(context.Background(), "system")
	seedPoolAccount(t, store, secrets, &Account{ID: "acct-a", TenantID: "system", Provider: AccountProviderClaude})

	// A JSON-RPC session holds a transport, so its environment was fixed when
	// the process was exec'd.
	spawnCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	sess, err := backend.Spawn(spawnCtx, &SpawnRequest{WorkDir: "/tmp", AccountID: "acct-a"})
	if err != nil {
		t.Fatalf("Spawn(): %v", err)
	}

	_, err = backend.RotateAccount(ctx, &RotateAccountRequest{
		SessionID: sess.ID,
		Spawn:     &SpawnRequest{WorkDir: "/tmp", AccountAuto: true},
	})
	var unsupported *SessionRotateUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("RotateAccount() error = %v, want SessionRotateUnsupportedError so the caller falls back to close-and-resume", err)
	}
}

func TestACPBackend_RotateAccount_RejectsBadInput(t *testing.T) {
	backend, _, _, _, cleanup := newTestACPBackendWithAccountPool(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := backend.RotateAccount(ctx, nil); err == nil {
		t.Fatal("RotateAccount(nil) succeeded")
	}
	if _, err := backend.RotateAccount(ctx, &RotateAccountRequest{}); err == nil {
		t.Fatal("RotateAccount() with no session ID succeeded")
	}
	if _, err := backend.RotateAccount(ctx, &RotateAccountRequest{SessionID: "s1"}); err == nil {
		t.Fatal("RotateAccount() with no spawn request succeeded")
	}
	_, err := backend.RotateAccount(ctx, &RotateAccountRequest{SessionID: "missing", Spawn: &SpawnRequest{WorkDir: "/tmp"}})
	var notFound *SessionNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("RotateAccount() on an unknown session = %v, want SessionNotFoundError", err)
	}
}

// liveACPSession returns the backend's in-memory session record, failing the
// test when it is absent.
func liveACPSession(t *testing.T, backend *ACPBackend, sessionID string) *acpSession {
	t.Helper()
	val, ok := backend.sessions.Load(sessionID)
	if !ok {
		t.Fatalf("session %s is not registered", sessionID)
	}
	sess, ok := val.(*acpSession)
	if !ok {
		t.Fatalf("session %s is a %T, want *acpSession", sessionID, val)
	}
	return sess
}
