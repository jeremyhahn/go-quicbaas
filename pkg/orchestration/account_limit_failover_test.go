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
// A limited turn holds its account, on every turn path
// =============================================================================

// sessionLimitBackend fails every turn with the CLI's structured account of a
// subscription limit, as a remote daemon forwards it.
type sessionLimitBackend struct {
	poolResumeBackend
	resetsAt time.Time
}

func (b *sessionLimitBackend) Prompt(_ context.Context, req *PromptRequest) (*PromptResponse, error) {
	zero := &TokenUsage{}
	return nil, &PromptError{SessionID: req.SessionID, Cause: &RemoteTurnFailureError{
		Op: "Prompt", Err: errors.New("rpc error: code = Internal"),
		TurnFailureEvidence: TurnFailureEvidence{
			TerminalReason: TerminalReasonAPIError,
			NumTurns:       1,
			Usage:          zero,
			ProviderLimit: &ProviderLimitEvidence{
				Source: ProviderLimitSourceRateLimitEvent, ResetsAt: b.resetsAt, Signal: sessionLimitText,
			},
		},
	}}
}

// newLimitHarness spawns one pooled session on acct-a, whose account record
// exists so the hold can be observed on it.
func newLimitHarness(t *testing.T, backend AgentBackend) (*accountResumeHarness, context.Context, string) {
	t.Helper()
	h := newRotationHarness(t, backend)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	if err := h.service.accountStore.Create(ctx, &Account{
		ID: "acct-a", TenantID: "tenant-a", Provider: "pool", Status: AccountStatusActive,
	}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return h, ctx, spawnRotatableSession(t, h, ctx, nil)
}

func TestPromptSession_ALimitedTurnHoldsTheAccountUntilTheStatedReset(t *testing.T) {
	assertHeldUntilStatedReset(t, time.Now().UTC().Add(4*time.Hour).Truncate(time.Second))
}

// TestPromptSession_AWeeklyResetIsHonouredExactly is the subscription's weekly
// limit: a reset days away must survive classification and persistence to the
// second, not collapse to the five-hour fallback (go-quicbaas#1046).
func TestPromptSession_AWeeklyResetIsHonouredExactly(t *testing.T) {
	assertHeldUntilStatedReset(t, time.Now().UTC().Add(6*24*time.Hour).Truncate(time.Second))
}

// assertHeldUntilStatedReset drives one limited turn whose provider stated
// resets, and proves the account is held until exactly that instant.
func assertHeldUntilStatedReset(t *testing.T, resets time.Time) {
	t.Helper()
	backend := &sessionLimitBackend{poolResumeBackend: poolResumeBackend{autoPick: "acct-a"}, resetsAt: resets}
	h, ctx, sessionID := newLimitHarness(t, backend)

	_, err := h.service.PromptSession(ctx, &PromptSessionRequest{SessionID: sessionID, Prompt: "work"})
	if err == nil {
		t.Fatal("PromptSession succeeded, want the limit")
	}

	limit, limited := AccountLimitOf(err)
	if !limited {
		t.Fatalf("err = %v, want it to say the account was refused", err)
	}
	if limit.AccountID != "acct-a" || !limit.AvailableAt.Equal(resets) {
		t.Fatalf("limit = %+v, want acct-a held until %s", limit, resets)
	}
	// The turn's own evidence still travels with it.
	if evidence, ok := EvidenceOf(err); !ok || evidence.ReasonClass() != TerminalReasonAPIError {
		t.Fatalf("EvidenceOf = (%+v, %v), want the CLI's account preserved", evidence, ok)
	}

	held, getErr := h.service.accountStore.Get(ctx, "tenant-a", "acct-a")
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if held.Status != AccountStatusLimited || !held.AvailableAt.Equal(resets) {
		t.Fatalf("account = %s until %s, want limited until %s", held.Status, held.AvailableAt, resets)
	}
	if !strings.Contains(held.LastError, "session limit") {
		t.Fatalf("LastError = %q, want the provider's refusal recorded", held.LastError)
	}
	if held.Selectable(time.Now().UTC()) {
		t.Fatal("a held account must not be selectable before its reset")
	}
	if !held.Selectable(resets.Add(time.Second)) {
		t.Fatal("the account must return by itself once its reset passes")
	}
}

func TestPromptSession_AnUnrelatedFailureHoldsNothing(t *testing.T) {
	backend := &failingPoolBackend{poolResumeBackend: poolResumeBackend{autoPick: "acct-a"}}
	h, ctx, sessionID := newLimitHarness(t, backend)

	_, err := h.service.PromptSession(ctx, &PromptSessionRequest{SessionID: sessionID, Prompt: "work"})
	if err == nil {
		t.Fatal("PromptSession succeeded, want the failure")
	}
	if _, limited := AccountLimitOf(err); limited {
		t.Fatalf("err = %v was reported as an account limit", err)
	}
	account, getErr := h.service.accountStore.Get(ctx, "tenant-a", "acct-a")
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if account.Status != AccountStatusActive {
		t.Fatalf("status = %s, want active — an overload is not this account's limit", account.Status)
	}
}

// =============================================================================
// The attempt is requeued uncharged
// =============================================================================

func TestClassifyEnvironmentFault_ALimitedTurnRequeuesUncharged(t *testing.T) {
	err := &TurnAccountLimitedError{
		SessionID: "s1",
		Limit:     &AccountLimitedError{AccountID: "claude3", AvailableAt: time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC)},
		Cause:     errors.New("turn failed"),
	}
	fault, ok := classifyEnvironmentFault(err)
	if !ok || fault.Class != FaultAccountLimited {
		t.Fatalf("classifyEnvironmentFault = (%+v, %v), want account_limited", fault, ok)
	}
	if !strings.Contains(fault.Reason, "claude3") || !strings.Contains(fault.Reason, "2026-09-23T01:00:00Z") {
		t.Errorf("Reason = %q, want the account and its return", fault.Reason)
	}
	if !fault.RetryNotBefore.IsZero() {
		t.Errorf("RetryNotBefore = %s, want none: another account may be free now", fault.RetryNotBefore)
	}
	outcome := faultOutcomeFor(&Task{}, fault)
	if !outcome.refund || !outcome.requeue {
		t.Fatalf("outcome = %+v, want the attempt refunded and requeued", outcome)
	}
	if !KnownFaultClass(FaultAccountLimited) {
		t.Fatal("the SDLC module must accept account_limited")
	}
}

func TestClassifyEnvironmentFault_ASaturatedPoolRequeuesUncharged(t *testing.T) {
	fault, ok := classifyEnvironmentFault(&AccountsSaturatedError{TenantID: "t", Candidates: 3})
	if !ok || fault.Class != FaultAccountsSaturated {
		t.Fatalf("classifyEnvironmentFault = (%+v, %v), want accounts_saturated, not an empty pool", fault, ok)
	}
	outcome := faultOutcomeFor(&Task{}, fault)
	if !outcome.refund || !outcome.requeue {
		t.Fatalf("outcome = %+v, want refunded and requeued", outcome)
	}
	if meaning, known := FaultClassMeaning(FaultAccountsSaturated); !known || meaning == "" {
		t.Fatal("accounts_saturated has no meaning for the forge comment")
	}
}

func TestAccountLimitOf(t *testing.T) {
	if _, ok := AccountLimitOf(errors.New("plain")); ok {
		t.Fatal("a plain error is not a limit")
	}
	if _, ok := AccountLimitOf(nil); ok {
		t.Fatal("nil is not a limit")
	}
	limit := &AccountLimitedError{AccountID: "a"}
	got, ok := AccountLimitOf(&TurnAccountLimitedError{Limit: limit})
	if !ok || got != limit {
		t.Fatalf("AccountLimitOf = (%v, %v), want the carried limit", got, ok)
	}
}

func TestTurnAccountLimitedError_RendersBothHalves(t *testing.T) {
	err := &TurnAccountLimitedError{
		SessionID: "s1",
		Limit:     &AccountLimitedError{AccountID: "claude3", Signal: "hit your session limit"},
		Cause:     errors.New("turn failed"),
	}
	msg := err.Error()
	for _, want := range []string{"s1", "claude3", "hit your session limit", "turn failed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error() = %q, missing %q", msg, want)
		}
	}
	if len((&TurnAccountLimitedError{SessionID: "s"}).Unwrap()) != 0 {
		t.Error("an empty error unwraps to nothing")
	}
}

// =============================================================================
// A fully exhausted pool is surfaced
// =============================================================================

func TestAccountResolver_AnExhaustedPoolIsAuditedAsSuch(t *testing.T) {
	cases := []struct {
		name     string
		prepare  func(t *testing.T, store AccountStore)
		sessions *fakeSessionLookup
		sentinel error
	}{
		{
			name: "every account at its cap",
			prepare: func(t *testing.T, store AccountStore) {
				account, err := store.Get(context.Background(), "tenant-a", "claude1")
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				account.MaxConcurrentSessions = 1
				if err := store.Update(context.Background(), account); err != nil {
					t.Fatalf("Update: %v", err)
				}
			},
			sessions: &fakeSessionLookup{live: map[string]int{"claude1": 1}},
			sentinel: ErrAccountsSaturated,
		},
		{
			name: "every account held by a limit",
			prepare: func(t *testing.T, store AccountStore) {
				holdAccountUntil(t, store, "tenant-a", "claude1", time.Now().UTC().Add(time.Hour))
			},
			sessions: &fakeSessionLookup{},
			sentinel: ErrAccountsCoolingDown,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewInMemoryAccountStore()
			secrets := newTestAccountSecretStore(t)
			seedPoolAccount(t, store, secrets, &Account{ID: "claude1", TenantID: "tenant-a", Provider: AccountProviderClaude})
			tc.prepare(t, store)
			registry, err := NewBalancedAccountStrategyRegistry(&fakeUsageLookup{}, tc.sessions)
			if err != nil {
				t.Fatalf("registry: %v", err)
			}
			auditor := &recordingAuditor{}
			resolver := NewAccountResolver(store, NewAccountTokenManager(secrets, store, time.Minute), registry,
				WithStoreAuditor(auditor))

			_, err = resolver.Resolve(context.Background(), AccountResolveRequest{
				TenantID: "tenant-a", ProjectID: "proj-1", Provider: AccountProviderClaude, Auto: true,
			})
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("err = %v, want %v", err, tc.sentinel)
			}
			var exhausted *StoreAuditEvent
			events := auditor.all()
			for i := range events {
				if events[i].Action == AuditActionPoolExhausted {
					exhausted = &events[i]
				}
			}
			if exhausted == nil {
				t.Fatalf("no pool_exhausted audit event among %+v", events)
			}
			if exhausted.ResourceID != "pool/proj-1/claude" || exhausted.Success {
				t.Fatalf("event = %+v, want a failed event naming the pool", exhausted)
			}
		})
	}
}

func TestAccountResolver_AnEmptyPoolIsNotReportedAsExhausted(t *testing.T) {
	store := NewInMemoryAccountStore()
	secrets := newTestAccountSecretStore(t)
	registry, err := NewBalancedAccountStrategyRegistry(&fakeUsageLookup{}, &fakeSessionLookup{})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	auditor := &recordingAuditor{}
	resolver := NewAccountResolver(store, NewAccountTokenManager(secrets, store, time.Minute), registry,
		WithStoreAuditor(auditor))
	if _, err := resolver.Resolve(context.Background(), AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true,
	}); !errors.Is(err, ErrNoAccountAvailable) {
		t.Fatalf("err = %v, want no account available", err)
	}
	for _, event := range auditor.all() {
		if event.Action == AuditActionPoolExhausted {
			t.Fatalf("an empty pool is a configuration fault, not a busy one: %+v", event)
		}
	}
}
