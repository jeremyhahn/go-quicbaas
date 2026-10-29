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
	stderrors "errors"
	"testing"
	"time"
)

// turnSpendHarness is a usage harness whose pooled sessions are served by
// acct-1 under a five-hour cap of 1000 tokens, with the spend authority and
// the ledger it decides from wired the way the server wires them.
type turnSpendHarness struct {
	*usageHarness
	ledger ConsumptionLedger
}

// newTurnSpendHarness builds the harness. withAccount false leaves the account
// out of the store, so the session names an account the pool cannot read.
func newTurnSpendHarness(t *testing.T, withAccount bool) *turnSpendHarness {
	t.Helper()
	harness := newUsageHarness(t, &usageReportingBackend{
		usage:    &TokenUsage{InputTokens: 100, OutputTokens: 50},
		account:  "acct-1",
		strategy: LeastUsageAccountStrategyName,
	}, true)

	accounts := NewInMemoryAccountStore()
	if withAccount {
		if err := accounts.Create(context.Background(), &Account{
			ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude,
			Spend: AccountSpendPolicy{Quota: fiveHourTokens(1000)},
		}); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	harness.service.SetAccountStore(accounts)

	kv := NewMemoryTaskKV()
	ledger := NewConsumptionLedger(kv)
	authority, err := NewSpendAuthority(ledger, kv, spendHolder)
	if err != nil {
		t.Fatalf("NewSpendAuthority: %v", err)
	}
	harness.service.SetSpendAuthority(authority)
	return &turnSpendHarness{usageHarness: harness, ledger: ledger}
}

// spawn starts a pooled session and returns its ID. The spawn itself is not
// gated here: the account is spent AFTER it, which is the case the per-turn
// gate exists for.
func (h *turnSpendHarness) spawn(t *testing.T, ctx context.Context) string {
	t.Helper()
	spawn, err := h.service.SpawnSession(ctx, &SpawnSessionRequest{
		Backend: "usage", WorkDir: "/work", ProjectID: "proj-1", Persistent: true, AccountAuto: true,
	})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	return spawn.SessionID
}

// spendAccount1 records tokens already spent by acct-1 an hour ago, inside
// its five-hour window.
func (h *turnSpendHarness) spendAccount1(t *testing.T, tokens int64) {
	t.Helper()
	entry := validEntry()
	entry.EntryID = "earlier-turn"
	entry.TenantID = "tenant-a"
	entry.AccountID = "acct-1"
	entry.InputTokens = tokens
	entry.OutputTokens = 0
	entry.OccurredAt = time.Now().UTC().Add(-time.Hour)
	if err := h.ledger.Record(context.Background(), entry); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func (h *turnSpendHarness) prompt(ctx context.Context, sessionID string) error {
	_, err := h.service.PromptSession(ctx, &PromptSessionRequest{
		SessionID: sessionID, ProjectID: "proj-1", Prompt: "do the thing",
	})
	return err
}

func (h *turnSpendHarness) accountTokens(t *testing.T, ctx context.Context) int64 {
	t.Helper()
	usage, err := h.tracker.GetAccountUsage(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("GetAccountUsage: %v", err)
	}
	return usage.TotalTokens
}

func TestPromptSession_RefusesTheNextTurnOfASessionOverItsAccountsWindow(t *testing.T) {
	harness := newTurnSpendHarness(t, true)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := harness.spawn(t, ctx)

	// The session crosses its account's window after it was spawned.
	harness.spendAccount1(t, 1200)

	err := harness.prompt(ctx, sessionID)
	var exhausted *AccountQuotaExhaustedError
	if !stderrors.As(err, &exhausted) {
		t.Fatalf("PromptSession over the cap = %v, want an *AccountQuotaExhaustedError — the cap was checked only at spawn", err)
	}
	if exhausted.AccountID != "acct-1" {
		t.Errorf("AccountID = %q, want the account the live session names", exhausted.AccountID)
	}
	if got := harness.accountTokens(t, ctx); got != 0 {
		t.Errorf("account usage = %d, want 0 — a refused turn must not run, so it must not be charged", got)
	}
	fault, ok := classifyEnvironmentFault(err)
	if !ok || fault.Class != FaultAccountQuotaExhausted {
		t.Errorf("classified as %+v (ok=%v), want %q exactly as the spawn refusal", fault, ok, FaultAccountQuotaExhausted)
	}
}

func TestPromptSession_AdmitsATurnUnderItsAccountsWindow(t *testing.T) {
	harness := newTurnSpendHarness(t, true)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := harness.spawn(t, ctx)
	harness.spendAccount1(t, 200)

	if err := harness.prompt(ctx, sessionID); err != nil {
		t.Fatalf("PromptSession under the cap: %v", err)
	}
	if got := harness.accountTokens(t, ctx); got != 150 {
		t.Errorf("account usage = %d, want 150 — the admitted turn is charged as usual", got)
	}
}

func TestPromptSession_RefusesATurnWhoseAccountCannotBeRead(t *testing.T) {
	harness := newTurnSpendHarness(t, false)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := harness.spawn(t, ctx)

	err := harness.prompt(ctx, sessionID)
	if !stderrors.Is(err, ErrAccountNotFound) {
		t.Fatalf("PromptSession with an unreadable account = %v, want ErrAccountNotFound — a cap that cannot be checked is not a cap that was met", err)
	}
	if got := harness.accountTokens(t, ctx); got != 0 {
		t.Errorf("account usage = %d, want 0 — a refused turn must not be charged", got)
	}
}
