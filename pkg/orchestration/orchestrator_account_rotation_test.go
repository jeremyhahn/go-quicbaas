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
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// Orchestrator-level rotation, over the same two-lifetime harness the resume
// pin tests use (see session_resume_account_test.go).
// =============================================================================

// limitingPoolBackend is a poolResumeBackend whose next prompt fails with a
// provider limit, which is what the orchestrator's automatic rotation reacts
// to. The failure is armed once so the turn AFTER a rotation succeeds on the
// replacement account, exactly as a real pool behaves.
type limitingPoolBackend struct {
	poolResumeBackend

	armMu sync.Mutex
	armed bool
}

func (b *limitingPoolBackend) armLimit() {
	b.armMu.Lock()
	defer b.armMu.Unlock()
	b.armed = true
}

func (b *limitingPoolBackend) Prompt(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
	b.armMu.Lock()
	fire := b.armed
	b.armed = false
	b.armMu.Unlock()
	if fire {
		return nil, &PromptError{SessionID: req.SessionID, Cause: &OneShotExecutionError{
			Command: "claude",
			Stderr:  "Claude usage limit reached. retry-after: 300",
		}}
	}
	return b.poolResumeBackend.Prompt(ctx, req)
}

// inPlacePoolBackend rotates its sessions without a resume, like the one-shot
// CLI path. It exists to prove the orchestrator prefers the cheap mechanism.
type inPlacePoolBackend struct {
	poolResumeBackend
}

func (b *inPlacePoolBackend) RotateAccount(_ context.Context, req *RotateAccountRequest) (*RotateAccountResult, error) {
	accountID, strategy := b.resolve(req.Spawn)
	if accountID == "" {
		return nil, &NoAccountAvailableError{Strategy: strategy}
	}
	return &RotateAccountResult{AccountID: accountID, Strategy: strategy}, nil
}

var _ SessionAccountRotator = (*inPlacePoolBackend)(nil)

// newRotationHarness builds one daemon lifetime around backend, registered
// under the "pool" name the harness's spawn requests use.
func newRotationHarness(t *testing.T, backend AgentBackend) *accountResumeHarness {
	t.Helper()
	kv := kvstore.NewMemory()
	svc, err := NewAgentService(NewAgentStore(kv), map[string]AgentBackend{"pool": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("NewAgentService returned %T", svc)
	}
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: kv})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	impl.SetSessionEventLog(log)
	tracker := NewCostTracker(kv, &agentTestLogger{})
	impl.SetCostTracker(tracker)
	impl.SetAccountStore(NewInMemoryAccountStore())
	return &accountResumeHarness{service: impl, tracker: tracker, kv: kv}
}

// spawnRotatableSession spawns a persistent, strategy-selected session — the
// shape rotation is authorized for.
func spawnRotatableSession(t *testing.T, h *accountResumeHarness, ctx context.Context, allow []string) string {
	t.Helper()
	spawn, err := h.service.SpawnSession(ctx, &SpawnSessionRequest{
		Backend:      "pool",
		WorkDir:      "/rotate",
		Persistent:   true,
		AccountAuto:  true,
		AccountAllow: allow,
	})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	return spawn.SessionID
}

// =============================================================================
// Rotation on the persistent path: close, switch, resume
// =============================================================================

func TestRotateSessionAccount_PersistentPath_ClosesSwitchesAndResumes(t *testing.T) {
	backend := &poolResumeBackend{autoPick: "acct-a"}
	h := newRotationHarness(t, backend)
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	sessionID := spawnRotatableSession(t, h, ctx, nil)
	if _, err := h.service.PromptSession(ctx, &PromptSessionRequest{SessionID: sessionID, Prompt: "hi"}); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}

	// The pool moves: the strategy would now pick acct-b.
	backend.setAutoPick("acct-b")

	result, err := h.service.RotateSessionAccount(ctx, &RotateSessionAccountRequest{
		SessionID:    sessionID,
		Reason:       "usage limit reached",
		HoldOutgoing: true,
		AvailableAt:  time.Now().UTC().Add(30 * time.Minute),
	})
	if err != nil {
		t.Fatalf("RotateSessionAccount: %v", err)
	}
	if result.Mode != AccountRotationModeResume {
		t.Fatalf("Mode = %q, want %q — a persistent session has a process to replace",
			result.Mode, AccountRotationModeResume)
	}
	if result.PreviousAccountID != "acct-a" || result.AccountID != "acct-b" {
		t.Fatalf("rotation = %s -> %s, want acct-a -> acct-b", result.PreviousAccountID, result.AccountID)
	}

	// The resume must have re-run SELECTION (not replayed the pin) and excluded
	// the account it was moving off.
	backend.mu.Lock()
	resumeCalls := append([]*ResumeRequest(nil), backend.resumeCalls...)
	backend.mu.Unlock()
	if len(resumeCalls) != 1 {
		t.Fatalf("resume calls = %d, want 1", len(resumeCalls))
	}
	spawn := resumeCalls[0].Spawn
	if spawn.AccountID != "" || !spawn.AccountAuto {
		t.Fatalf("rotation spawn = {id:%q auto:%v}, want a fresh selection", spawn.AccountID, spawn.AccountAuto)
	}
	if len(spawn.AccountExclude) != 1 || spawn.AccountExclude[0] != "acct-a" {
		t.Fatalf("rotation spawn AccountExclude = %v, want [acct-a]", spawn.AccountExclude)
	}

	// The session keeps its identifier and stays promptable.
	if _, err := h.service.PromptSession(ctx, &PromptSessionRequest{SessionID: sessionID, Prompt: "carry on"}); err != nil {
		t.Fatalf("PromptSession after rotation: %v", err)
	}
}

func TestRotateSessionAccount_PrefersInPlaceRotation(t *testing.T) {
	backend := &inPlacePoolBackend{poolResumeBackend: poolResumeBackend{autoPick: "acct-a"}}
	h := newRotationHarness(t, backend)
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	sessionID := spawnRotatableSession(t, h, ctx, nil)
	backend.setAutoPick("acct-b")

	result, err := h.service.RotateSessionAccount(ctx, &RotateSessionAccountRequest{SessionID: sessionID})
	if err != nil {
		t.Fatalf("RotateSessionAccount: %v", err)
	}
	if result.Mode != AccountRotationModeInPlace {
		t.Fatalf("Mode = %q, want %q", result.Mode, AccountRotationModeInPlace)
	}
	backend.mu.Lock()
	resumes := len(backend.resumeCalls)
	backend.mu.Unlock()
	if resumes != 0 {
		t.Fatalf("resume calls = %d, want 0 — an in-place rotation must not restart the agent", resumes)
	}
}

// =============================================================================
// Policy and history persistence
// =============================================================================

func TestRotateSessionAccount_PersistsPolicyAndHistoryAcrossRestart(t *testing.T) {
	backend := &poolResumeBackend{autoPick: "acct-a"}
	h := newRotationHarness(t, backend)
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	sessionID := spawnRotatableSession(t, h, ctx, []string{"acct-a", "acct-b"})
	backend.setAutoPick("acct-b")
	if _, err := h.service.RotateSessionAccount(ctx, &RotateSessionAccountRequest{
		SessionID: sessionID, Reason: "usage limit reached",
	}); err != nil {
		t.Fatalf("RotateSessionAccount: %v", err)
	}

	// A second daemon lifetime over the same durable store.
	restarted := newAccountResumeHarness(t, h.kv, backend)
	if err := restarted.service.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}

	record, err := restarted.service.store.GetSession(ctx, "tenant-a", sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !record.AccountAuto {
		t.Fatal("AccountAuto did not survive the restart; the session would no longer be rotatable")
	}
	if len(record.AccountAllow) != 2 {
		t.Fatalf("AccountAllow = %v, want the declared pool to survive the restart", record.AccountAllow)
	}
	if record.AccountID != "acct-b" {
		t.Fatalf("AccountID = %q, want acct-b", record.AccountID)
	}
	if len(record.AccountHistory) != 2 {
		t.Fatalf("AccountHistory = %+v, want two segments", record.AccountHistory)
	}
	if record.AccountHistory[0].AccountID != "acct-a" || record.AccountHistory[0].To == 0 {
		t.Fatalf("first segment = %+v, want a CLOSED acct-a segment", record.AccountHistory[0])
	}
	if record.AccountHistory[1].AccountID != "acct-b" || record.AccountHistory[1].To != 0 {
		t.Fatalf("second segment = %+v, want an OPEN acct-b segment", record.AccountHistory[1])
	}

	// And the restored session is still rotatable, which is the point of
	// persisting the policy rather than the outcome.
	if rotatable, reason := record.CanRotateAccount(); !rotatable {
		t.Fatalf("restored session is not rotatable: %s", reason)
	}
}

// =============================================================================
// Cost attribution: one account per segment, never one bill for two
// =============================================================================

func TestRotateSessionAccount_ChargesEachAccountItsOwnTurns(t *testing.T) {
	backend := &poolResumeBackend{autoPick: "acct-a"}
	h := newRotationHarness(t, backend)
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	sessionID := spawnRotatableSession(t, h, ctx, nil)

	// Two turns on acct-a (100 tokens each).
	for i := 0; i < 2; i++ {
		if _, err := h.service.PromptSession(ctx, &PromptSessionRequest{SessionID: sessionID, Prompt: "a"}); err != nil {
			t.Fatalf("PromptSession: %v", err)
		}
	}

	backend.setAutoPick("acct-b")
	if _, err := h.service.RotateSessionAccount(ctx, &RotateSessionAccountRequest{SessionID: sessionID}); err != nil {
		t.Fatalf("RotateSessionAccount: %v", err)
	}

	// One turn on acct-b.
	if _, err := h.service.PromptSession(ctx, &PromptSessionRequest{SessionID: sessionID, Prompt: "b"}); err != nil {
		t.Fatalf("PromptSession after rotation: %v", err)
	}

	usageA, err := h.tracker.GetAccountUsage(ctx, "tenant-a", "acct-a")
	if err != nil {
		t.Fatalf("GetAccountUsage(acct-a): %v", err)
	}
	if usageA.TotalTokens != 200 {
		t.Fatalf("acct-a usage = %d, want 200 — the two turns it actually served", usageA.TotalTokens)
	}
	usageB, err := h.tracker.GetAccountUsage(ctx, "tenant-a", "acct-b")
	if err != nil {
		t.Fatalf("GetAccountUsage(acct-b): %v", err)
	}
	if usageB.TotalTokens != 100 {
		t.Fatalf("acct-b usage = %d, want 100 — no account may be billed for another's tokens", usageB.TotalTokens)
	}
}

// =============================================================================
// Automatic rotation from a classified provider limit
// =============================================================================

func TestDispatchTurn_LimitedTurnRotatesTheSession(t *testing.T) {
	backend := &limitingPoolBackend{poolResumeBackend: poolResumeBackend{autoPick: "acct-a"}}
	h := newRotationHarness(t, backend)
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	sessionID := spawnRotatableSession(t, h, ctx, nil)
	backend.setAutoPick("acct-b")
	backend.armLimit()

	if _, err := h.service.PromptSessionAsync(ctx, &PromptSessionRequest{SessionID: sessionID, Prompt: "spend it"}); err != nil {
		t.Fatalf("PromptSessionAsync: %v", err)
	}
	h.service.WaitForTurns()

	record, err := h.service.store.GetSession(ctx, "tenant-a", sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.AccountID != "acct-b" {
		t.Fatalf("AccountID = %q, want acct-b — a classified provider limit must move the session", record.AccountID)
	}

	// The outgoing account is durably held, so no OTHER session rediscovers the
	// same limit for itself.
	held, err := h.service.accountStore.Get(ctx, "tenant-a", "acct-a")
	if err == nil {
		if held.Status != AccountStatusLimited {
			t.Fatalf("acct-a status = %q, want limited", held.Status)
		}
		if held.AvailableAt.IsZero() {
			t.Fatal("acct-a has no AvailableAt; it would stay held forever")
		}
	}

	// And the move is in the transcript where an operator will look for it.
	if !sessionLogHasKind(t, h, "tenant-a", sessionID, KindAccountRotated) {
		t.Fatal("no account_rotated event on the session log")
	}
}

func TestDispatchTurn_UnrelatedFailureDoesNotRotate(t *testing.T) {
	backend := &failingPoolBackend{poolResumeBackend: poolResumeBackend{autoPick: "acct-a"}}
	h := newRotationHarness(t, backend)
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	sessionID := spawnRotatableSession(t, h, ctx, nil)
	backend.setAutoPick("acct-b")

	if _, err := h.service.PromptSessionAsync(ctx, &PromptSessionRequest{SessionID: sessionID, Prompt: "break"}); err != nil {
		t.Fatalf("PromptSessionAsync: %v", err)
	}
	h.service.WaitForTurns()

	record, err := h.service.store.GetSession(ctx, "tenant-a", sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.AccountID != "acct-a" {
		t.Fatalf("AccountID = %q, want acct-a — a failure another account cannot fix must not burn the pool", record.AccountID)
	}
}

// failingPoolBackend fails every turn with an error that is NOT a provider
// limit.
type failingPoolBackend struct {
	poolResumeBackend
}

func (b *failingPoolBackend) Prompt(_ context.Context, req *PromptRequest) (*PromptResponse, error) {
	return nil, &PromptError{SessionID: req.SessionID, Cause: &OneShotExecutionError{
		Command: "claude",
		Stderr:  `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`,
	}}
}

// =============================================================================
// Refusals
// =============================================================================

func TestRotateSessionAccount_RefusesAPinnedSession(t *testing.T) {
	backend := &poolResumeBackend{autoPick: "acct-a"}
	h := newRotationHarness(t, backend)
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	spawn, err := h.service.SpawnSession(ctx, &SpawnSessionRequest{
		Backend: "pool", WorkDir: "/pinned", Persistent: true, AccountID: "acct-pinned",
	})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	_, err = h.service.RotateSessionAccount(ctx, &RotateSessionAccountRequest{SessionID: spawn.SessionID})
	var notRotatable *SessionNotRotatableError
	if !errors.As(err, &notRotatable) {
		t.Fatalf("RotateSessionAccount() = %v, want SessionNotRotatableError — a deliberate pin must be honored", err)
	}
	if !errors.Is(err, ErrSessionNotRotatable) {
		t.Fatal("errors.Is(err, ErrSessionNotRotatable) = false")
	}
}

// TestRotateSessionAccount_AccountAllowMakesAPinnedSessionRotatable is the
// other half of TestRotateSessionAccount_RefusesAPinnedSession, and the reason
// account_allow has to reach the server from every transport: the SAME spawn
// that is refused with a bare account_id becomes rotatable the moment the
// caller declares the pool it may move within. Without the transport plumbing
// only account_auto sessions could ever rotate through a public API.
func TestRotateSessionAccount_AccountAllowMakesAPinnedSessionRotatable(t *testing.T) {
	backend := &inPlacePoolBackend{poolResumeBackend: poolResumeBackend{autoPick: "acct-b"}}
	h := newRotationHarness(t, backend)
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	spawn, err := h.service.SpawnSession(ctx, &SpawnSessionRequest{
		Backend:    "pool",
		WorkDir:    "/pinned-with-allow",
		Persistent: true,
		AccountID:  "acct-a",
		// No AccountAuto: the caller named the starting seat AND the pool it
		// may rotate within.
		AccountAllow: []string{"acct-a", "acct-b"},
	})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	record, err := h.service.store.GetSession(ctx, "tenant-a", spawn.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.AccountAuto {
		t.Fatal("AccountAuto is set; this test must exercise the allow-list path, not the auto path")
	}
	if rotatable, reason := record.CanRotateAccount(); !rotatable {
		t.Fatalf("session with account_allow is not rotatable: %s", reason)
	}

	result, err := h.service.RotateSessionAccount(ctx, &RotateSessionAccountRequest{SessionID: spawn.SessionID})
	if err != nil {
		t.Fatalf("RotateSessionAccount: %v", err)
	}
	if result.PreviousAccountID != "acct-a" || result.AccountID != "acct-b" {
		t.Fatalf("rotation = %s -> %s, want acct-a -> acct-b within the declared pool",
			result.PreviousAccountID, result.AccountID)
	}
}

// TestRotateSessionAccount_AllowListBoundsTheReplacement proves account_allow
// is a real filter and not decoration: a pool that names only the account the
// session is already on has nowhere to move it.
func TestRotateSessionAccount_AllowListBoundsTheReplacement(t *testing.T) {
	backend := &inPlacePoolBackend{poolResumeBackend: poolResumeBackend{autoPick: "acct-b"}}
	h := newRotationHarness(t, backend)
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	spawn, err := h.service.SpawnSession(ctx, &SpawnSessionRequest{
		Backend:      "pool",
		WorkDir:      "/narrow-allow",
		Persistent:   true,
		AccountID:    "acct-a",
		AccountAllow: []string{"acct-a"},
	})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	_, err = h.service.RotateSessionAccount(ctx, &RotateSessionAccountRequest{SessionID: spawn.SessionID})
	if err == nil {
		t.Fatal("RotateSessionAccount succeeded with an allow-list that names only the current account")
	}
	var noAccount *NoAccountAvailableError
	if !errors.As(err, &noAccount) {
		t.Fatalf("RotateSessionAccount() = %v, want NoAccountAvailableError", err)
	}
}

func TestRotateSessionAccount_RefusesAnotherTenantsSession(t *testing.T) {
	backend := &poolResumeBackend{autoPick: "acct-a"}
	h := newRotationHarness(t, backend)
	owner := ContextWithTenant(context.Background(), "tenant-a")

	sessionID := spawnRotatableSession(t, h, owner, nil)

	intruder := ContextWithTenant(context.Background(), "tenant-b")
	_, err := h.service.RotateSessionAccount(intruder, &RotateSessionAccountRequest{SessionID: sessionID})
	var notFound *SessionNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("RotateSessionAccount() = %v, want SessionNotFoundError — a cross-tenant caller is told nothing", err)
	}
}

func TestRotateSessionAccount_RejectsBadInput(t *testing.T) {
	h := newRotationHarness(t, &poolResumeBackend{autoPick: "acct-a"})
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	if _, err := h.service.RotateSessionAccount(ctx, nil); err == nil {
		t.Fatal("RotateSessionAccount(nil) succeeded")
	}
	if _, err := h.service.RotateSessionAccount(ctx, &RotateSessionAccountRequest{}); err == nil {
		t.Fatal("RotateSessionAccount() with no session ID succeeded")
	}
	_, err := h.service.RotateSessionAccount(ctx, &RotateSessionAccountRequest{SessionID: "missing"})
	var notFound *SessionNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("RotateSessionAccount() on an unknown session = %v, want SessionNotFoundError", err)
	}
}

// TestRotateSessionAccount_StrandedResumeLeavesTheSessionRecoverable covers the
// worst moment in the whole flow: the old process is already gone and no
// replacement account can be selected. The session must come back as RESUMABLE
// with the reason recorded — not silently dead, which is what "not live and not
// resumable" would look like to every follow-up prompt.
func TestRotateSessionAccount_StrandedResumeLeavesTheSessionRecoverable(t *testing.T) {
	backend := &poolResumeBackend{autoPick: "acct-a"}
	h := newRotationHarness(t, backend)
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	sessionID := spawnRotatableSession(t, h, ctx, nil)

	// The pool is exhausted: the only account it would pick is the one the
	// rotation is moving off, and the exclusion removes it.
	_, err := h.service.RotateSessionAccount(ctx, &RotateSessionAccountRequest{
		SessionID: sessionID, Reason: "usage limit reached",
	})
	if err == nil {
		t.Fatal("RotateSessionAccount() succeeded with an exhausted pool")
	}

	mem, ok := h.service.liveSessionRecord(sessionID)
	if !ok {
		t.Fatal("the session vanished from the registry")
	}
	if !mem.resumable.Load() {
		t.Fatal("the session is neither live nor resumable; every follow-up prompt would fail with \"session not found\"")
	}

	record, getErr := h.service.store.GetSession(ctx, "tenant-a", sessionID)
	if getErr != nil {
		t.Fatalf("GetSession: %v", getErr)
	}
	if record.Status != SessionStateError || !strings.Contains(record.ResumeError, "account rotation failed") {
		t.Fatalf("record = {status:%s resume_error:%q}, want the stranded rotation recorded",
			record.Status, record.ResumeError)
	}
	if record.AccountID != "acct-a" {
		t.Fatalf("AccountID = %q, want acct-a — a failed rotation must not pretend it moved", record.AccountID)
	}
}

// =============================================================================
// Helpers
// =============================================================================

func sessionLogHasKind(t *testing.T, h *accountResumeHarness, tenantID, sessionID string, kind SessionEventKind) bool {
	t.Helper()
	events, _, err := h.service.sessionLog.Range(context.Background(), tenantID, sessionID, 0, 0)
	if err != nil {
		t.Fatalf("sessionLog.Range: %v", err)
	}
	for _, ev := range events {
		if ev.Kind == kind {
			return true
		}
	}
	return false
}
