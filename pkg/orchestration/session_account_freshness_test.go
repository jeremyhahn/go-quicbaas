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
	"testing"
	"time"
)

// =============================================================================
// Has the pool moved under a live session? (go-quicbaas#997)
// =============================================================================

// TestAccountStore_ACredentialReplacementMovesTheGenerationMarker: only a
// write that actually replaced the envelope moves CredentialRotatedAt. A
// refresh the provider REFUSED moves LastRefreshedAt and nothing else —
// bumping the marker there would declare every session on that account stale
// for a renewal that changed no material.
func TestAccountStore_ACredentialReplacementMovesTheGenerationMarker(t *testing.T) {
	store := NewInMemoryAccountStore()
	ctx := context.Background()

	account := newTestAccount("tenant-a", "acct-1", AccountProviderClaude)
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// A refresh the provider refused: attempted, nothing written.
	if err := store.Touch(ctx, "tenant-a", "acct-1", TouchAccountOptions{
		MarkRefreshed:  true,
		SetStatusValid: true,
		SetStatus:      AccountStatusError,
		SetLastError:   "the provider refused the refresh",
	}); err != nil {
		t.Fatalf("Touch(failed refresh): %v", err)
	}
	got, err := store.Get(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LastRefreshedAt.IsZero() {
		t.Error("a refresh attempt did not move LastRefreshedAt")
	}
	if !got.CredentialRotatedAt.IsZero() {
		t.Errorf("a refresh that wrote no envelope moved the generation marker to %s", got.CredentialRotatedAt)
	}

	// A replacement the caller actually stored.
	if err := store.Touch(ctx, "tenant-a", "acct-1", TouchAccountOptions{
		MarkRefreshed:         true,
		MarkCredentialRotated: true,
		SetStatusValid:        true,
		SetStatus:             AccountStatusActive,
		ClearLastError:        true,
	}); err != nil {
		t.Fatalf("Touch(rotation): %v", err)
	}
	got, err = store.Get(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CredentialRotatedAt.IsZero() {
		t.Error("a replaced credential envelope did not move the generation marker")
	}
}

// TestSessionRecord_AccountMaterializedAtIsTheOpenSegment: the instant a
// session took the copy it is holding is the start of the account segment
// still in force, not the spawn — a session that has rotated took its current
// copy when that segment opened.
func TestSessionRecord_AccountMaterializedAtIsTheOpenSegment(t *testing.T) {
	spawn := time.Date(2026, 9, 22, 1, 0, 0, 0, time.UTC).UnixNano()
	rotated := time.Date(2026, 9, 22, 5, 0, 0, 0, time.UTC).UnixNano()

	record := &SessionRecord{
		SessionID: "session-1",
		CreatedAt: spawn,
		AccountID: "acct-2",
		AccountHistory: []AccountAssignment{
			{AccountID: "acct-1", From: spawn, To: rotated, Reason: "limit"},
			{AccountID: "acct-2", From: rotated},
		},
	}
	if got := record.AccountMaterializedAt(); got != rotated {
		t.Errorf("AccountMaterializedAt = %d, want the open segment's start %d", got, rotated)
	}

	// No history at all — every session written before rotation existed —
	// falls back to the spawn, which is the same instant for a session that
	// never rotated.
	plain := &SessionRecord{SessionID: "session-2", CreatedAt: spawn, AccountID: "acct-1"}
	if got := plain.AccountMaterializedAt(); got != spawn {
		t.Errorf("AccountMaterializedAt = %d, want the spawn %d", got, spawn)
	}

	// A record that can date neither answers zero rather than the epoch: the
	// caller must read that as unknown, not as "stale since 1970".
	undated := &SessionRecord{SessionID: "session-3", AccountID: "acct-1"}
	if got := undated.AccountMaterializedAt(); got != 0 {
		t.Errorf("AccountMaterializedAt = %d, want 0 for a record that dates nothing", got)
	}
	if got := (*SessionRecord)(nil).AccountMaterializedAt(); got != 0 {
		t.Errorf("AccountMaterializedAt on a nil record = %d, want 0", got)
	}
}

// newFreshnessHarness spawns one persistent, pool-backed session and returns
// the daemon, the session and the account it resolved to.
func newFreshnessHarness(t *testing.T) (*accountResumeHarness, context.Context, string) {
	t.Helper()
	backend := &poolResumeBackend{}
	backend.setAutoPick("acct-1")
	h := newRotationHarness(t, backend)
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	account := newTestAccount("tenant-a", "acct-1", AccountProviderClaude)
	account.RefreshPolicy = AccountCredentialModeStaticToken
	if err := h.service.AccountStore().Create(ctx, account); err != nil {
		t.Fatalf("Create account: %v", err)
	}
	return h, ctx, spawnRotatableSession(t, h, ctx, nil)
}

// TestSessionAccountFreshness_ARefreshAfterTheSpawnIsStale: the case
// go-quicbaas#997 measured — the host replaced the pooled account's credential
// while a session was open, so the copy that session holds is no longer the
// one the provider honours.
func TestSessionAccountFreshness_ARefreshAfterTheSpawnIsStale(t *testing.T) {
	h, ctx, sessionID := newFreshnessHarness(t)

	before, err := h.service.SessionAccountFreshness(ctx, sessionID)
	if err != nil {
		t.Fatalf("SessionAccountFreshness: %v", err)
	}
	if before.Stale {
		t.Fatalf("a session spawned against the current credential reported stale: %+v", before)
	}
	if before.AccountID != "acct-1" {
		t.Errorf("AccountID = %q, want the account the spawn resolved to", before.AccountID)
	}

	if err := h.service.AccountStore().Touch(ctx, "tenant-a", "acct-1", TouchAccountOptions{
		MarkRefreshed:         true,
		MarkCredentialRotated: true,
	}); err != nil {
		t.Fatalf("Touch(rotation): %v", err)
	}

	after, err := h.service.SessionAccountFreshness(ctx, sessionID)
	if err != nil {
		t.Fatalf("SessionAccountFreshness: %v", err)
	}
	if !after.Stale {
		t.Fatalf("a session left behind by an account refresh reported fresh: %+v", after)
	}
	if !after.RotatedAt.After(after.MaterializedAt) {
		t.Errorf("rotated_at %s is not after materialized_at %s", after.RotatedAt, after.MaterializedAt)
	}
}

// TestSessionAccountFreshness_ARotationBeforeTheSpawnIsNotStale: a refresh the
// session was spawned AFTER is the material it is holding. Reporting it stale
// would roll every session on a pool that has ever been refreshed.
func TestSessionAccountFreshness_ARotationBeforeTheSpawnIsNotStale(t *testing.T) {
	backend := &poolResumeBackend{}
	backend.setAutoPick("acct-1")
	h := newRotationHarness(t, backend)
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	account := newTestAccount("tenant-a", "acct-1", AccountProviderClaude)
	account.RefreshPolicy = AccountCredentialModeStaticToken
	if err := h.service.AccountStore().Create(ctx, account); err != nil {
		t.Fatalf("Create account: %v", err)
	}
	if err := h.service.AccountStore().Touch(ctx, "tenant-a", "acct-1", TouchAccountOptions{
		MarkCredentialRotated: true,
	}); err != nil {
		t.Fatalf("Touch(rotation): %v", err)
	}

	sessionID := spawnRotatableSession(t, h, ctx, nil)
	freshness, err := h.service.SessionAccountFreshness(ctx, sessionID)
	if err != nil {
		t.Fatalf("SessionAccountFreshness: %v", err)
	}
	if freshness.Stale {
		t.Fatalf("a session spawned after the refresh reported stale: %+v", freshness)
	}
}

// TestSessionAccountFreshness_RefusesWhatItCannotAnswerFor: an empty or
// unknown session is a typed refusal, and another tenant's session does not
// exist as far as this caller is concerned.
func TestSessionAccountFreshness_RefusesWhatItCannotAnswerFor(t *testing.T) {
	h, ctx, sessionID := newFreshnessHarness(t)

	if _, err := h.service.SessionAccountFreshness(ctx, ""); !errors.Is(err, ErrSessionIDRequired) {
		t.Errorf("SessionAccountFreshness(\"\") error = %v, want ErrSessionIDRequired", err)
	}

	var notFound *SessionNotFoundError
	if _, err := h.service.SessionAccountFreshness(ctx, "session-nobody-spawned"); !errors.As(err, &notFound) {
		t.Errorf("an unknown session's error = %v, want SessionNotFoundError", err)
	}

	other := ContextWithTenant(context.Background(), "tenant-b")
	if _, err := h.service.SessionAccountFreshness(other, sessionID); !errors.As(err, &notFound) {
		t.Errorf("another tenant's session error = %v, want SessionNotFoundError", err)
	}
}

// TestSessionAccountFreshness_NoPooledAccountIsNeverStale: a session that
// never used the pool has no materialized copy to be left behind, and a
// deployment that wired no account store has no evidence either way. Both
// answer "not stale" so nothing rolls on missing evidence.
func TestSessionAccountFreshness_NoPooledAccountIsNeverStale(t *testing.T) {
	backend := &poolResumeBackend{}
	h := newRotationHarness(t, backend)
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	spawn, err := h.service.SpawnSession(ctx, &SpawnSessionRequest{
		Backend: "pool", WorkDir: "/plain", Persistent: true,
	})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	freshness, err := h.service.SessionAccountFreshness(ctx, spawn.SessionID)
	if err != nil {
		t.Fatalf("SessionAccountFreshness: %v", err)
	}
	if freshness.Stale || freshness.AccountID != "" {
		t.Fatalf("a session that used no pooled account reported %+v", freshness)
	}
}

// TestJobRun_AccountRotatedIsSilentWhenNothingCanAnswer: the job-facing
// accessor is fail-soft in one direction. An agent service that is not a
// freshness reader — every deployment that runs no pool, and every test double
// — reports nothing to act on, so the runner prompts exactly as it always did.
func TestJobRun_AccountRotatedIsSilentWhenNothingCanAnswer(t *testing.T) {
	run := &JobRun{Agents: &staleAgentsStub{}, Logger: &agentTestLogger{}}
	if got := run.AccountRotated(context.Background(), ""); got != nil {
		t.Errorf("AccountRotated with no session = %+v, want nil", got)
	}

	silent := &JobRun{Agents: nil, Logger: &agentTestLogger{}}
	if got := silent.AccountRotated(context.Background(), "session-1"); got != nil {
		t.Errorf("AccountRotated with no agent service = %+v, want nil", got)
	}

	// An agent service that is not a freshness reader at all — every
	// deployment that predates the question, and every remote proxy.
	plain := &JobRun{Agents: &plainAgentsStub{}, Logger: &agentTestLogger{}}
	if got := plain.AccountRotated(context.Background(), "session-1"); got != nil {
		t.Errorf("AccountRotated over a service that cannot answer = %+v, want nil", got)
	}

	// A reader that fails is not evidence that anything changed.
	failing := &JobRun{Agents: &staleAgentsStub{err: errors.New("the pool is unreachable")}, Logger: &agentTestLogger{}}
	if got := failing.AccountRotated(context.Background(), "session-1"); got != nil {
		t.Errorf("AccountRotated over a failing reader = %+v, want nil", got)
	}

	stale := &JobRun{
		Agents: &staleAgentsStub{answer: &SessionAccountFreshness{SessionID: "session-1", AccountID: "acct-1", Stale: true}},
		Logger: &agentTestLogger{},
	}
	if got := stale.AccountRotated(context.Background(), "session-1"); got == nil || got.AccountID != "acct-1" {
		t.Errorf("AccountRotated over a stale session = %+v, want the rotation reported", got)
	}
}

// plainAgentsStub is an AgentService and nothing more: it cannot answer the
// freshness question, which is what the accessor must survive silently.
type plainAgentsStub struct {
	AgentService
}

// staleAgentsStub answers the freshness question and nothing else: every other
// AgentService method panics on the nil embedded interface, which is what
// keeps this double from quietly serving a caller it was not written for.
type staleAgentsStub struct {
	AgentService

	answer *SessionAccountFreshness
	err    error
}

func (s *staleAgentsStub) SessionAccountFreshness(context.Context, string) (*SessionAccountFreshness, error) {
	return s.answer, s.err
}
