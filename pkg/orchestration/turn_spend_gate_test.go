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
	"strconv"
	"sync/atomic"
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

// A turn queued on a session behind the turn that spends its account is asked
// again after that turn is charged, and refused. Asked before, it would pass
// against a ledger that did not yet hold the turn ahead of it.
func TestPromptSession_RefusesATurnQueuedBehindTheTurnThatSpentTheAccount(t *testing.T) {
	harness := newTurnSpendHarness(t, true)
	// The authority decides from this ledger, so each turn's usage must reach it.
	harness.service.SetConsumptionLedger(harness.ledger)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := harness.spawn(t, ctx)

	// 950 of 1000 spent: the first turn is admitted, and its 150 tokens cross the cap.
	harness.spendAccount1(t, 950)

	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var turns atomic.Int32
	harness.backend.beforePrompt = func() {
		entered <- struct{}{}
		if turns.Add(1) == 1 {
			<-release
		}
	}

	first := make(chan error, 1)
	go func() { first <- harness.prompt(ctx, sessionID) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first turn never reached the backend")
	}

	second := make(chan error, 1)
	go func() { second <- harness.prompt(ctx, sessionID) }()
	select {
	case <-entered:
		close(release)
		t.Fatal("the second turn reached the backend while the first was still running — it was admitted before the first was charged")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)

	select {
	case err := <-first:
		if err != nil {
			t.Fatalf("first turn = %v, want it admitted under the cap", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the first turn never returned")
	}
	select {
	case err := <-second:
		var exhausted *AccountQuotaExhaustedError
		if !stderrors.As(err, &exhausted) {
			t.Fatalf("queued turn = %v, want an *AccountQuotaExhaustedError — the first turn spent the account", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the queued turn never returned")
	}
	if got := turns.Load(); got != 1 {
		t.Errorf("backend ran %d turns, want 1 — the refused turn must not run", got)
	}
}

func TestLockSessionTurn_DropsTheEntryWhenTheLastTurnLeaves(t *testing.T) {
	svc := &agentServiceImpl{}
	unlockFirst, err := svc.lockSessionTurn(context.Background(), "s1")
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	if got := svc.turnLocks.size(); got != 1 {
		t.Fatalf("entries after the first lock = %d, want 1", got)
	}

	acquired := make(chan func(), 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		unlock, err := svc.lockSessionTurn(ctx, "s1")
		if err != nil {
			t.Errorf("second lock: %v", err)
			acquired <- nil
			return
		}
		acquired <- unlock
	}()

	select {
	case <-acquired:
		t.Fatal("the second turn acquired the lock while the first held it")
	case <-time.After(200 * time.Millisecond):
	}

	unlockFirst()
	var unlockSecond func()
	select {
	case unlockSecond = <-acquired:
	case <-time.After(10 * time.Second):
		t.Fatal("the second turn did not acquire the lock after the first left")
	}
	if unlockSecond == nil {
		t.Fatal("the second turn failed to lock")
	}
	if got := svc.turnLocks.size(); got != 1 {
		t.Fatalf("entries while the second turn holds the lock = %d, want 1", got)
	}
	unlockSecond()
	if got := svc.turnLocks.size(); got != 0 {
		t.Fatalf("entries after the last turn left = %d, want 0", got)
	}
}

func TestLockSessionTurn_ACancelledWaiterLeavesNoEntry(t *testing.T) {
	svc := &agentServiceImpl{}
	unlock, err := svc.lockSessionTurn(context.Background(), "s1")
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	defer cancel()
	if _, err := svc.lockSessionTurn(ctx, "s1"); !stderrors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter error = %v, want context.Canceled", err)
	}

	unlock()
	if got := svc.turnLocks.size(); got != 0 {
		t.Fatalf("entries after the holder left = %d, want 0", got)
	}
}

func TestLockSessionTurn_ManySessionsLeaveNoEntries(t *testing.T) {
	svc := &agentServiceImpl{}
	for i := 0; i < 100; i++ {
		id := "s" + strconv.Itoa(i)
		unlock, err := svc.lockSessionTurn(context.Background(), id)
		if err != nil {
			t.Fatalf("lock %s: %v", id, err)
		}
		unlock()
	}
	if got := svc.turnLocks.size(); got != 0 {
		t.Fatalf("entries after 100 sessions = %d, want 0", got)
	}
}
