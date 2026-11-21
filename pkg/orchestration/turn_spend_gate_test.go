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
	return h.spawnAt(t, ctx, "/work")
}

// spawnAt spawns a session at workDir. The usage backend names a session
// from its work dir, so two sessions need two work dirs.
func (h *turnSpendHarness) spawnAt(t *testing.T, ctx context.Context, workDir string) string {
	t.Helper()
	spawn, err := h.service.SpawnSession(ctx, &SpawnSessionRequest{
		Backend: "usage", WorkDir: workDir, ProjectID: "proj-1", Persistent: true, AccountAuto: true,
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

// flakyLedger is a ConsumptionLedger whose writes fail while failing is set;
// every read goes to the ledger it wraps. It records whether each write saw
// a live context.
type flakyLedger struct {
	ConsumptionLedger
	failing   atomic.Bool
	writes    atomic.Int32
	cancelled atomic.Int32

	// held and release, when set, hold every write: Record signals held,
	// then waits for release to close.
	held    chan struct{}
	release chan struct{}
}

func (l *flakyLedger) Record(ctx context.Context, entry ConsumptionEntry) error {
	l.writes.Add(1)
	if ctx.Err() != nil {
		l.cancelled.Add(1)
		return ctx.Err()
	}
	if l.release != nil {
		l.held <- struct{}{}
		<-l.release
	}
	if l.failing.Load() {
		return stderrors.New("quota plane unavailable")
	}
	return l.ConsumptionLedger.Record(ctx, entry)
}

// A turn whose usage record fails must not release the next turn against a
// spend sum without it: 950 of 1000 recorded, a 150-token turn whose write
// fails, and the next prompt is refused until the entry is in the ledger —
// then the retried entry puts the account over its cap.
func TestPromptSession_RefusesTheNextTurnWhileAFailedUsageRecordIsPending(t *testing.T) {
	harness := newTurnSpendHarness(t, true)
	ledger := &flakyLedger{ConsumptionLedger: harness.ledger}
	harness.service.SetConsumptionLedger(ledger)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := harness.spawn(t, ctx)
	harness.spendAccount1(t, 950)

	var turns atomic.Int32
	harness.backend.beforePrompt = func() { turns.Add(1) }

	ledger.failing.Store(true)
	if err := harness.prompt(ctx, sessionID); err != nil {
		t.Fatalf("first turn = %v, want it admitted under the cap", err)
	}
	if got := harness.service.pendingLedger.size(); got != 1 {
		t.Fatalf("pending entries = %d, want 1 — the failed write was dropped", got)
	}

	err := harness.prompt(ctx, sessionID)
	var pending *ConsumptionPendingError
	if !stderrors.As(err, &pending) {
		t.Fatalf("turn after a failed usage record = %v, want a *ConsumptionPendingError — it was admitted against 950 of the 1100 tokens spent", err)
	}
	if !stderrors.Is(err, ErrConsumptionPending) || pending.AccountID != "acct-1" || pending.Pending != 1 {
		t.Errorf("refusal = %+v, want ErrConsumptionPending naming acct-1 and one entry", pending)
	}
	if got := turns.Load(); got != 1 {
		t.Fatalf("backend ran %d turns, want 1 — the refused turn must not run", got)
	}

	// The quota plane recovers: the retry stores the entry, and the cap now
	// sees 1100 of 1000.
	ledger.failing.Store(false)
	err = harness.prompt(ctx, sessionID)
	var exhausted *AccountQuotaExhaustedError
	if !stderrors.As(err, &exhausted) {
		t.Fatalf("turn after the retry = %v, want an *AccountQuotaExhaustedError — the retried entry spent the account", err)
	}
	if got := harness.service.pendingLedger.size(); got != 0 {
		t.Errorf("pending entries = %d, want 0 after a successful retry", got)
	}
	if got := turns.Load(); got != 1 {
		t.Errorf("backend ran %d turns, want 1", got)
	}
}

// TestPromptSession_APendingEntryStaysVisibleToASessionSharingItsAccount
// holds session a's retry of its pending entry inside the ledger write and
// prompts session b on the same account meanwhile: b must still see the
// entry, retry it too, and be refused at the cap rather than admitted
// against a sum that leaves a's tokens out.
func TestPromptSession_APendingEntryStaysVisibleToASessionSharingItsAccount(t *testing.T) {
	harness := newTurnSpendHarness(t, true)
	ledger := &flakyLedger{ConsumptionLedger: harness.ledger}
	harness.service.SetConsumptionLedger(ledger)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionA := harness.spawn(t, ctx)
	sessionB := harness.spawnAt(t, ctx, "/work-b")
	if sessionA == sessionB {
		t.Fatalf("sessions a and b are both %q — one session serializes on its own turn lock and cannot show the race", sessionA)
	}
	for _, id := range []string{sessionA, sessionB} {
		mem, ok := harness.service.liveSessionRecord(id)
		if !ok || mem.AccountID() != "acct-1" {
			t.Fatalf("session %q is not live on acct-1 — the test needs both sessions on one account", id)
		}
	}
	harness.spendAccount1(t, 950)

	var turns atomic.Int32
	harness.backend.beforePrompt = func() { turns.Add(1) }

	ledger.failing.Store(true)
	if err := harness.prompt(ctx, sessionA); err != nil {
		t.Fatalf("first turn = %v, want it admitted under the cap", err)
	}
	if got := harness.service.pendingLedger.size(); got != 1 {
		t.Fatalf("pending entries = %d, want 1 — the failed write was dropped", got)
	}
	ledger.failing.Store(false)

	ledger.held = make(chan struct{}, 2)
	ledger.release = make(chan struct{})
	resultA := make(chan error, 1)
	go func() { resultA <- harness.prompt(ctx, sessionA) }()
	select {
	case <-ledger.held:
	case <-time.After(10 * time.Second):
		close(ledger.release)
		t.Fatal("session a never retried its pending entry")
	}
	resultB := make(chan error, 1)
	go func() { resultB <- harness.prompt(ctx, sessionB) }()
	time.Sleep(200 * time.Millisecond)
	if got := turns.Load(); got != 1 {
		close(ledger.release)
		t.Fatalf("session b reached the backend while session a's pending entry was being retried (%d turns, want 1) — it was admitted against 950 of the 1100 tokens spent", got)
	}
	close(ledger.release)

	for name, result := range map[string]chan error{"a": resultA, "b": resultB} {
		select {
		case err := <-result:
			var exhausted *AccountQuotaExhaustedError
			if !stderrors.As(err, &exhausted) {
				t.Errorf("session %s turn = %v, want an *AccountQuotaExhaustedError — 1100 of 1000 tokens are spent", name, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("session %s turn never returned", name)
		}
	}
	if got := turns.Load(); got != 1 {
		t.Errorf("backend ran %d turns, want 1", got)
	}
	if got := harness.service.pendingLedger.size(); got != 0 {
		t.Errorf("pending entries = %d, want 0 once the ledger holds the entry", got)
	}
	now := time.Now().UTC()
	usage, err := harness.ledger.WindowUsage(ctx, "tenant-a", "acct-1", now.Add(-2*time.Hour), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("WindowUsage: %v", err)
	}
	if got := usage.InputTokens + usage.OutputTokens; got != 1100 {
		t.Errorf("ledger holds %d tokens for acct-1, want 1100 — the entry retried by both sessions must be stored once", got)
	}
}

// A turn cancelled while it runs still spent its tokens: the usage record is
// written on a context the turn's cancellation does not end.
func TestPromptSession_RecordsTheUsageOfATurnCancelledWhileItRuns(t *testing.T) {
	harness := newTurnSpendHarness(t, true)
	ledger := &flakyLedger{ConsumptionLedger: harness.ledger}
	harness.service.SetConsumptionLedger(ledger)
	base := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := harness.spawn(t, base)

	ctx, cancel := context.WithCancel(base)
	defer cancel()
	harness.backend.beforePrompt = cancel
	_ = harness.prompt(ctx, sessionID)

	if got := ledger.cancelled.Load(); got != 0 {
		t.Errorf("%d ledger writes saw the cancelled turn context, want 0", got)
	}
	if got := ledger.writes.Load(); got == 0 {
		t.Fatal("the cancelled turn wrote nothing to the ledger")
	}
	usage, err := harness.ledger.WindowUsage(base, "tenant-a", "acct-1",
		time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(time.Minute))
	if err != nil {
		t.Fatalf("WindowUsage: %v", err)
	}
	if usage.InputTokens+usage.OutputTokens == 0 {
		t.Errorf("window usage = %+v, want the cancelled turn's tokens", usage)
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
