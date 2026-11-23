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
	"strings"
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

// TestPromptSession_ANodeWithoutTheLiveSessionStillChecksItsAccount: node B
// shares node A's storage and worker but never spawned the session, so its
// registry misses it. A turn sent through B is still checked against the
// account the durable record names, and a record B cannot read refuses it.
func TestPromptSession_ANodeWithoutTheLiveSessionStillChecksItsAccount(t *testing.T) {
	harness := newTurnSpendHarness(t, true)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := harness.spawn(t, ctx)
	harness.spendAccount1(t, 1200)

	var turns atomic.Int32
	harness.backend.beforePrompt = func() { turns.Add(1) }

	promptThrough := func(nodeB *agentServiceImpl) error {
		_, err := nodeB.PromptSession(ctx, &PromptSessionRequest{
			SessionID: sessionID, Backend: "usage", ProjectID: "proj-1", Prompt: "do the thing",
		})
		return err
	}

	nodeB := newTurnSpendNodeB(t, harness, NewAgentStore(harness.kv), sessionID)
	err := promptThrough(nodeB)
	var exhausted *AccountQuotaExhaustedError
	if !stderrors.As(err, &exhausted) {
		t.Fatalf("turn through node b = %v, want an *AccountQuotaExhaustedError — 1200 of 1000 tokens are spent", err)
	}
	if exhausted.AccountID != "acct-1" {
		t.Errorf("refusal names account %q, want acct-1", exhausted.AccountID)
	}
	if got := turns.Load(); got != 0 {
		t.Errorf("backend ran %d turns, want 0 — node b admitted the turn over the cap", got)
	}

	t.Run("a record that cannot be read refuses the turn", func(t *testing.T) {
		turns.Store(0)
		nodeB := newTurnSpendNodeB(t, harness, &getFailingStore{
			AgentStore: NewAgentStore(harness.kv), sessionID: sessionID, err: stderrors.New("session store down"),
		}, sessionID)
		err := promptThrough(nodeB)
		var unavailable *SpendCheckUnavailableError
		if !stderrors.As(err, &unavailable) {
			t.Fatalf("turn through node b = %v, want a *SpendCheckUnavailableError", err)
		}
		if got := turns.Load(); got != 0 {
			t.Errorf("backend ran %d turns, want 0", got)
		}
	})
}

// newTurnSpendNodeB builds a second control-plane node over store that shares
// the harness's backend, account store and spend authority but does not hold
// sessionID in its live registry.
func newTurnSpendNodeB(t *testing.T, harness *turnSpendHarness, store AgentStore, sessionID string) *agentServiceImpl {
	t.Helper()
	svc, err := NewAgentService(store, map[string]AgentBackend{"usage": harness.backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	nodeB, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("NewAgentService returned %T, want *agentServiceImpl", svc)
	}
	nodeB.SetAccountStore(harness.service.accountStore)
	nodeB.SetSpendAuthority(harness.service.turnSpend)
	if _, ok := nodeB.liveSessionRecord(sessionID); ok {
		t.Fatalf("node b holds session %q live — the test needs a registry miss", sessionID)
	}
	return nodeB
}

func TestPromptSession_ATurnAdmittedOnAnotherNodeIsChargedToItsAccount(t *testing.T) {
	harness := newTurnSpendHarness(t, true)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := harness.spawn(t, ctx)
	harness.spendAccount1(t, 950)

	var turns atomic.Int32
	harness.backend.beforePrompt = func() { turns.Add(1) }

	nodeB := newTurnSpendNodeB(t, harness, NewAgentStore(harness.kv), sessionID)
	nodeB.SetConsumptionLedger(harness.ledger)
	promptThrough := func() error {
		_, err := nodeB.PromptSession(ctx, &PromptSessionRequest{
			SessionID: sessionID, Backend: "usage", ProjectID: "proj-1", Prompt: "do the thing",
		})
		return err
	}

	if err := promptThrough(); err != nil {
		t.Fatalf("first turn through node b = %v, want it admitted under the cap", err)
	}
	usage, err := harness.ledger.WindowUsage(ctx, "tenant-a", "acct-1", time.Now().Add(-5*time.Hour), time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("WindowUsage: %v", err)
	}
	if usage.Entries != 2 {
		t.Fatalf("acct-1 window holds %d entries, want 2 — node b recorded its turn with no account", usage.Entries)
	}

	err = promptThrough()
	var exhausted *AccountQuotaExhaustedError
	if !stderrors.As(err, &exhausted) {
		t.Fatalf("second turn through node b = %v, want an *AccountQuotaExhaustedError — 1100 of 1000 tokens are spent", err)
	}
	if exhausted.AccountID != "acct-1" {
		t.Errorf("refusal names account %q, want acct-1", exhausted.AccountID)
	}
	if got := turns.Load(); got != 1 {
		t.Errorf("backend ran %d turns, want 1 — node b admitted a turn over the cap", got)
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
	var unavailable *SpendCheckUnavailableError
	if !stderrors.As(err, &unavailable) {
		t.Fatalf("PromptSession with an unreadable account = %v, want a *SpendCheckUnavailableError", err)
	}
	if unavailable.AccountID != "acct-1" {
		t.Errorf("unavailable account = %q, want %q", unavailable.AccountID, "acct-1")
	}
	assertSpendCheckRefunded(t, err)
}

// assertSpendCheckRefunded drives err through a job's end and checks the
// attempt was refunded as a spend_check_unavailable environment fault.
func assertSpendCheckRefunded(t *testing.T, err error) {
	t.Helper()
	task, class := concludeFailedJob(t, err)
	if class != FaultSpendCheckUnavailable {
		t.Errorf("failure class = %q, want %q", class, FaultSpendCheckUnavailable)
	}
	if got := task.ChargedAttempts(); got != 0 {
		t.Errorf("charged attempts = %d, want 0 — a store read that failed is not the work's failure", got)
	}
	if task.EnvironmentFaults != 1 {
		t.Errorf("environment faults = %d, want 1", task.EnvironmentFaults)
	}
	if task.Status != TaskStatusPending {
		t.Errorf("task status = %q, want %q — the refused turn requeues", task.Status, TaskStatusPending)
	}
}

// unreadableLedger fails WindowUsage while failing is set; cancel, when set,
// is called first, so the caller's ctx ends inside the spend check itself.
type unreadableLedger struct {
	ConsumptionLedger
	failing atomic.Bool
	cancel  context.CancelFunc
}

func (l *unreadableLedger) WindowUsage(ctx context.Context, tenantID, accountID string, from, to time.Time) (*ConsumptionRollup, error) {
	if l.failing.Load() {
		if l.cancel != nil {
			l.cancel()
			return nil, context.Canceled
		}
		return nil, stderrors.New("ledger: window scan failed")
	}
	return l.ConsumptionLedger.WindowUsage(ctx, tenantID, accountID, from, to)
}

// withUnreadableLedger rewires the harness's spend authority onto a ledger
// that can be made to fail its window reads.
func withUnreadableLedger(t *testing.T, harness *turnSpendHarness) *unreadableLedger {
	t.Helper()
	ledger := &unreadableLedger{ConsumptionLedger: harness.ledger}
	authority, err := NewSpendAuthority(ledger, NewMemoryTaskKV(), spendHolder)
	if err != nil {
		t.Fatalf("NewSpendAuthority: %v", err)
	}
	harness.service.SetSpendAuthority(authority)
	return ledger
}

func TestPromptSession_ASpendLedgerReadFailureIsRefundedNotCharged(t *testing.T) {
	harness := newTurnSpendHarness(t, true)
	ledger := withUnreadableLedger(t, harness)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := harness.spawn(t, ctx)

	ledger.failing.Store(true)
	err := harness.prompt(ctx, sessionID)
	var unavailable *SpendCheckUnavailableError
	if !stderrors.As(err, &unavailable) {
		t.Fatalf("PromptSession with an unreadable ledger = %v, want a *SpendCheckUnavailableError — a cap that cannot be checked is not a cap that was met", err)
	}
	if got := harness.accountTokens(t, ctx); got != 0 {
		t.Errorf("account usage = %d, want 0 — a refused turn must not be charged", got)
	}
	assertSpendCheckRefunded(t, err)
}

func TestPromptSession_ACancelledSpendCheckIsNotReclassified(t *testing.T) {
	harness := newTurnSpendHarness(t, true)
	ledger := withUnreadableLedger(t, harness)
	base := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := harness.spawn(t, base)

	ctx, cancel := context.WithCancel(base)
	defer cancel()
	ledger.cancel = cancel
	ledger.failing.Store(true)
	err := harness.prompt(ctx, sessionID)
	if err == nil {
		t.Fatal("PromptSession whose spend check was cancelled = nil, want an error")
	}
	if stderrors.Is(err, ErrSpendCheckUnavailable) {
		t.Errorf("PromptSession whose spend check was cancelled = %v, want the caller's cancellation, not a spend-check fault", err)
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

// goneSessionEntry is a consumption entry of a session that is not live and
// drew on no pooled account: no turn will ever flush it.
func goneSessionEntry(now time.Time) ConsumptionEntry {
	return ConsumptionEntry{
		EntryID:      "e-gone",
		TenantID:     "t1",
		SessionID:    "sess-gone",
		Engine:       "claude",
		Model:        "claude-test",
		Outcome:      ConsumptionOutcomeSuccess,
		InputTokens:  70,
		OutputTokens: 30,
		OccurredAt:   now,
	}
}

func TestRecordLedgerEntry_PendingEntryDrainsAfterItsSessionIsGone(t *testing.T) {
	svc, inner, _ := newConsumptionRecorder(t)
	ledger := &flakyLedger{ConsumptionLedger: inner}
	svc.SetConsumptionLedger(ledger)
	svc.pendingLedger.firstRetry = 10 * time.Millisecond
	ledger.failing.Store(true)

	now := time.Now().UTC()
	if svc.recordLedgerEntry(context.Background(), goneSessionEntry(now)) {
		t.Fatal("recordLedgerEntry reported the entry recorded while the ledger fails")
	}
	if n := svc.pendingLedger.size(); n != 1 {
		t.Fatalf("pending = %d, want 1", n)
	}

	ledger.failing.Store(false)
	deadline := time.Now().Add(5 * time.Second)
	for svc.pendingLedger.size() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := svc.pendingLedger.size(); n != 0 {
		t.Fatalf("pending = %d after 5s, want the drainer to have written it", n)
	}

	rollup, err := inner.TenantWindowUsage(ContextWithTenant(context.Background(), "t1"), "t1", now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("TenantWindowUsage: %v", err)
	}
	if rollup.InputTokens != 70 || rollup.OutputTokens != 30 {
		t.Fatalf("ledger holds input=%d output=%d, want 70/30", rollup.InputTokens, rollup.OutputTokens)
	}
}

func TestShutdown_StopsThePendingConsumptionDrainer(t *testing.T) {
	svc, inner, _ := newConsumptionRecorder(t)
	ledger := &flakyLedger{ConsumptionLedger: inner}
	svc.SetConsumptionLedger(ledger)
	svc.pendingLedger.firstRetry = time.Hour
	ledger.failing.Store(true)

	if svc.recordLedgerEntry(context.Background(), goneSessionEntry(time.Now().UTC())) {
		t.Fatal("recordLedgerEntry reported the entry recorded while the ledger fails")
	}
	before := ledger.writes.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if n := svc.pendingLedger.size(); n != 1 {
		t.Fatalf("pending = %d, want 1 (the ledger still fails)", n)
	}
	if got := ledger.writes.Load() - before; got != 1 {
		t.Fatalf("writes during shutdown = %d, want exactly the one last attempt", got)
	}
}

// ctxWaitLedger fails while failing is set, and while blocking is set holds
// every write until its context is done, signalling entered first.
type ctxWaitLedger struct {
	ConsumptionLedger
	failing  atomic.Bool
	blocking atomic.Bool
	writes   atomic.Int32
	inFlight atomic.Int32
	entered  chan struct{}
}

func newCtxWaitLedger(inner ConsumptionLedger) *ctxWaitLedger {
	return &ctxWaitLedger{ConsumptionLedger: inner, entered: make(chan struct{}, 8)}
}

func (l *ctxWaitLedger) Record(ctx context.Context, entry ConsumptionEntry) error {
	l.writes.Add(1)
	l.inFlight.Add(1)
	defer l.inFlight.Add(-1)
	if l.blocking.Load() {
		select {
		case l.entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	}
	if l.failing.Load() {
		return stderrors.New("quota plane unavailable")
	}
	return l.ConsumptionLedger.Record(ctx, entry)
}

func TestShutdown_PendingRetryKeepsTheShutdownDeadline(t *testing.T) {
	svc, inner, _ := newConsumptionRecorder(t)
	ledger := newCtxWaitLedger(inner)
	ledger.blocking.Store(true)
	svc.SetConsumptionLedger(ledger)

	now := time.Now().UTC()
	for _, id := range []string{"e-1", "e-2", "e-3"} {
		entry := goneSessionEntry(now)
		entry.EntryID = id
		svc.pendingLedger.add(entry)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("Shutdown took %s, past its 300ms deadline", took)
	}
	if n := svc.pendingLedger.size(); n != 3 {
		t.Fatalf("pending = %d, want 3", n)
	}
	if got := ledger.writes.Load(); got != 1 {
		t.Fatalf("writes = %d, want 1 (the deadline ends the retry after the first)", got)
	}
}

// startBlockedDrainer starts the pending-consumption drainer and returns
// once it is inside a ledger write that only its context ends.
func startBlockedDrainer(t *testing.T) (*agentServiceImpl, *ctxWaitLedger) {
	t.Helper()
	svc, inner, _ := newConsumptionRecorder(t)
	ledger := newCtxWaitLedger(inner)
	ledger.failing.Store(true)
	svc.SetConsumptionLedger(ledger)
	svc.pendingLedger.firstRetry = 10 * time.Millisecond

	if svc.recordLedgerEntry(context.Background(), goneSessionEntry(time.Now().UTC())) {
		t.Fatal("recordLedgerEntry reported the entry recorded while the ledger fails")
	}
	ledger.blocking.Store(true)
	ledger.failing.Store(false)
	select {
	case <-ledger.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the drainer never retried the pending entry")
	}
	return svc, ledger
}

func TestShutdown_JoinsADrainerMidRetry(t *testing.T) {
	svc, ledger := startBlockedDrainer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if n := ledger.inFlight.Load(); n != 0 {
		t.Fatalf("ledger writes in flight after Shutdown = %d, want 0", n)
	}
}

func TestShutdown_JoinsTheDrainerWhenTheJobsDrainTimesOut(t *testing.T) {
	svc, ledger := startBlockedDrainer(t)
	svc.jobsInFlight.Add(1)
	t.Cleanup(svc.jobsInFlight.Done)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := svc.Shutdown(ctx)
	var drainErr *DrainTimeoutError
	if !stderrors.As(err, &drainErr) || drainErr.Phase != DrainPhaseJobs {
		t.Fatalf("Shutdown = %v, want a jobs-phase *DrainTimeoutError", err)
	}
	if n := ledger.inFlight.Load(); n != 0 {
		t.Fatalf("ledger writes in flight after Shutdown = %d, want 0", n)
	}
}

// stuckLedger is a ledger whose write, once stuck is set, ignores its context:
// it returns only when release closes, as a write queued behind a stalled
// proposal does.
type stuckLedger struct {
	ConsumptionLedger
	failing   atomic.Bool
	stuck     atomic.Bool
	entered   chan struct{}
	release   chan struct{}
	panicWith string
}

func (l *stuckLedger) Record(ctx context.Context, entry ConsumptionEntry) error {
	if l.stuck.Load() {
		select {
		case l.entered <- struct{}{}:
		default:
		}
		<-l.release
		if l.panicWith != "" {
			panic(l.panicWith)
		}
		return nil
	}
	if l.failing.Load() {
		return stderrors.New("quota plane unavailable")
	}
	return l.ConsumptionLedger.Record(ctx, entry)
}

func TestShutdown_ADrainerWriteThatIgnoresCancellationKeepsTheDeadline(t *testing.T) {
	svc, inner, _ := newConsumptionRecorder(t)
	ledger := &stuckLedger{
		ConsumptionLedger: inner,
		entered:           make(chan struct{}, 8),
		release:           make(chan struct{}),
	}
	ledger.failing.Store(true)
	svc.SetConsumptionLedger(ledger)
	svc.pendingLedger.firstRetry = 10 * time.Millisecond

	if svc.recordLedgerEntry(context.Background(), goneSessionEntry(time.Now().UTC())) {
		t.Fatal("recordLedgerEntry reported the entry recorded while the ledger fails")
	}
	// Set before the write sticks: the drainer reads it only after release.
	ledger.panicWith = "pebble: closed"
	ledger.stuck.Store(true)
	ledger.failing.Store(false)
	select {
	case <-ledger.entered:
	case <-time.After(5 * time.Second):
		close(ledger.release)
		t.Fatal("the drainer never retried the pending entry")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- svc.Shutdown(ctx) }()
	var err error
	select {
	case err = <-result:
	case <-time.After(3 * time.Second):
		close(ledger.release)
		t.Fatal("Shutdown blocked past its deadline on a drainer write that ignores cancellation")
	}
	var drainErr *DrainTimeoutError
	if !stderrors.As(err, &drainErr) || drainErr.Phase != DrainPhaseConsumption {
		close(ledger.release)
		t.Fatalf("Shutdown = %v, want a consumption-phase *DrainTimeoutError", err)
	}

	// Storage closes under the abandoned write: the drainer ends through the
	// store-closed guard rather than crashing the process.
	close(ledger.release)
	select {
	case <-waitGroupDone(&svc.pendingLedger.drainers):
	case <-time.After(5 * time.Second):
		t.Fatal("the drainer never ended after its abandoned write returned")
	}
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

// sharedFlakyLedger is a flakyLedger that keeps the wrapped ledger's
// shared pending list, as the server's ledger does.
type sharedFlakyLedger struct {
	*flakyLedger
	shared pendingConsumptionStore
}

func (l *sharedFlakyLedger) MarkPending(ctx context.Context, entry ConsumptionEntry) error {
	return l.shared.MarkPending(ctx, entry)
}

func (l *sharedFlakyLedger) ClearPending(ctx context.Context, entry ConsumptionEntry) error {
	return l.shared.ClearPending(ctx, entry)
}

func (l *sharedFlakyLedger) PendingFor(ctx context.Context, tenantID, accountID string) ([]ConsumptionEntry, error) {
	return l.shared.PendingFor(ctx, tenantID, accountID)
}

func TestPromptSession_AFailedUsageRecordOnOneNodeRefusesTheTurnOnAnother(t *testing.T) {
	harness := newTurnSpendHarness(t, true)
	shared, ok := harness.ledger.(pendingConsumptionStore)
	if !ok {
		t.Fatalf("ledger %T keeps no shared pending list", harness.ledger)
	}
	ledgerA := &sharedFlakyLedger{flakyLedger: &flakyLedger{ConsumptionLedger: harness.ledger}, shared: shared}
	harness.service.SetConsumptionLedger(ledgerA)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := harness.spawn(t, ctx)
	harness.spendAccount1(t, 950)

	var turns atomic.Int32
	harness.backend.beforePrompt = func() { turns.Add(1) }

	ledgerA.failing.Store(true)
	if err := harness.prompt(ctx, sessionID); err != nil {
		t.Fatalf("turn through node a = %v, want it admitted under the cap", err)
	}
	marked, err := shared.PendingFor(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("PendingFor: %v", err)
	}
	if len(marked) != 1 {
		t.Fatalf("shared pending entries = %d, want 1 — node a's failed write is not visible to other nodes", len(marked))
	}

	nodeB := newTurnSpendNodeB(t, harness, NewAgentStore(harness.kv), sessionID)
	ledgerB := &sharedFlakyLedger{flakyLedger: &flakyLedger{ConsumptionLedger: harness.ledger}, shared: shared}
	ledgerB.failing.Store(true)
	nodeB.SetConsumptionLedger(ledgerB)
	t.Cleanup(func() {
		harness.service.pendingLedger.stopDrainer()
		nodeB.pendingLedger.stopDrainer()
	})
	promptThrough := func() error {
		_, err := nodeB.PromptSession(ctx, &PromptSessionRequest{
			SessionID: sessionID, Backend: "usage", ProjectID: "proj-1", Prompt: "do the thing",
		})
		return err
	}

	err = promptThrough()
	var pending *ConsumptionPendingError
	if !stderrors.As(err, &pending) {
		t.Fatalf("turn through node b = %v, want a *ConsumptionPendingError — node b admitted the turn against 950 of the 1100 tokens spent", err)
	}
	if pending.AccountID != "acct-1" {
		t.Errorf("refusal names account %q, want acct-1", pending.AccountID)
	}
	if got := turns.Load(); got != 1 {
		t.Fatalf("backend ran %d turns, want 1 — the refused turn must not run", got)
	}

	// The quota plane recovers: node b's retry stores node a's entry, and
	// the cap now sees 1100 of 1000.
	ledgerA.failing.Store(false)
	ledgerB.failing.Store(false)
	err = promptThrough()
	var exhausted *AccountQuotaExhaustedError
	if !stderrors.As(err, &exhausted) {
		t.Fatalf("turn through node b after the retry = %v, want an *AccountQuotaExhaustedError — node a's entry spent the account", err)
	}
	if got := turns.Load(); got != 1 {
		t.Errorf("backend ran %d turns, want 1", got)
	}
	marked, err = shared.PendingFor(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("PendingFor: %v", err)
	}
	if len(marked) != 0 {
		t.Errorf("shared pending entries = %d, want 0 after node b's retry stored it", len(marked))
	}
}

func TestPromptSession_APendingConsumptionRefusalIsRefundedNotCharged(t *testing.T) {
	harness := newTurnSpendHarness(t, true)
	ledger := &flakyLedger{ConsumptionLedger: harness.ledger}
	harness.service.SetConsumptionLedger(ledger)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := harness.spawn(t, ctx)
	harness.spendAccount1(t, 950)

	ledger.failing.Store(true)
	if err := harness.prompt(ctx, sessionID); err != nil {
		t.Fatalf("first turn = %v, want it admitted under the cap", err)
	}
	err := harness.prompt(ctx, sessionID)
	var pending *ConsumptionPendingError
	if !stderrors.As(err, &pending) {
		t.Fatalf("turn after a failed usage record = %v, want a *ConsumptionPendingError", err)
	}

	task, class := concludeFailedJob(t, err)
	if class != FaultConsumptionPending {
		t.Errorf("failure class = %q, want %q", class, FaultConsumptionPending)
	}
	if got := task.ChargedAttempts(); got != 0 {
		t.Errorf("charged attempts = %d, want 0 — a ledger write that failed is not the work's failure", got)
	}
	if task.EnvironmentFaults != 1 {
		t.Errorf("environment faults = %d, want 1", task.EnvironmentFaults)
	}
	if task.Status != TaskStatusPending {
		t.Errorf("task status = %q, want %q — the refused turn requeues", task.Status, TaskStatusPending)
	}
	if !KnownFaultClass(FaultConsumptionPending) {
		t.Errorf("%q is not a known fault class", FaultConsumptionPending)
	}
	if !faultRequeues(FaultConsumptionPending) {
		t.Errorf("%q does not requeue", FaultConsumptionPending)
	}
}

func TestPromptSession_RefusesAnotherTenantsTurnBeforeTheLedger(t *testing.T) {
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
		t.Fatalf("pending entries = %d, want 1", got)
	}

	// Another tenant names the session while the owner's entry is pending:
	// it is told the session does not exist, and nothing of the owner's is
	// retried, charged or reported to it.
	before := ledger.writes.Load()
	foreign := ContextWithTenant(context.Background(), "tenant-b")
	err := harness.prompt(foreign, sessionID)
	var notFound *SessionNotFoundError
	if !stderrors.As(err, &notFound) {
		t.Fatalf("foreign prompt = %v, want a *SessionNotFoundError", err)
	}
	if stderrors.Is(err, ErrConsumptionPending) {
		t.Errorf("foreign prompt = %v, want no pending-consumption refusal", err)
	}
	if strings.Contains(err.Error(), "acct-1") {
		t.Errorf("foreign prompt = %q, leaks the owner's account ID", err.Error())
	}
	if got := ledger.writes.Load(); got != before {
		t.Errorf("ledger writes = %d, want %d — the foreign prompt retried the owner's entry", got, before)
	}
	if got := turns.Load(); got != 1 {
		t.Errorf("backend ran %d turns, want 1", got)
	}
	if got := harness.service.pendingLedger.size(); got != 1 {
		t.Errorf("pending entries = %d, want 1", got)
	}

	_, err = harness.service.PromptSessionAsync(foreign, &PromptSessionRequest{
		SessionID: sessionID, ProjectID: "proj-1", Prompt: "do the thing",
	})
	if !stderrors.As(err, &notFound) {
		t.Fatalf("foreign async prompt = %v, want a *SessionNotFoundError", err)
	}

	// The owner's flush still runs: the retry stores the entry and the cap
	// sees the account spent.
	ledger.failing.Store(false)
	err = harness.prompt(ctx, sessionID)
	var exhausted *AccountQuotaExhaustedError
	if !stderrors.As(err, &exhausted) {
		t.Fatalf("owner's turn after the retry = %v, want an *AccountQuotaExhaustedError", err)
	}
	if got := harness.service.pendingLedger.size(); got != 0 {
		t.Errorf("pending entries = %d, want 0 after the owner's retry", got)
	}
}

func TestPendingConsumption_MatchingIsScopedToTheTenant(t *testing.T) {
	var pending pendingConsumption
	for _, e := range []struct{ entry, tenant, session, account string }{
		{"a", "tenant-a", "s1", "acct-1"},
		{"b", "tenant-b", "s1", "acct-1"},
		{"c", "tenant-b", "s2", "acct-1"},
	} {
		entry := validEntry()
		entry.EntryID, entry.TenantID, entry.SessionID, entry.AccountID = e.entry, e.tenant, e.session, e.account
		pending.add(entry)
	}
	ids := func(entries []ConsumptionEntry) string {
		var out []string
		for _, entry := range entries {
			out = append(out, entry.EntryID)
		}
		return strings.Join(out, ",")
	}
	if got := ids(pending.matching("tenant-a", "s1", "acct-1")); got != "a" {
		t.Errorf("tenant-a matching = %q, want %q", got, "a")
	}
	if got := ids(pending.matching("tenant-b", "s2", "acct-1")); got != "b,c" {
		t.Errorf("tenant-b matching = %q, want %q", got, "b,c")
	}
	if got := ids(pending.matching("tenant-c", "s1", "acct-1")); got != "" {
		t.Errorf("tenant-c matching = %q, want none", got)
	}
}

// pendingRetryHarness leaves one failed usage record pending for a session
// whose account is at 950 of its 1000-token cap, with the drainer held off and
// the ledger ready to block. It returns the harness, the ledger, the tenant's
// context, the session and the backend's turn counter.
func pendingRetryHarness(t *testing.T) (*turnSpendHarness, *ctxWaitLedger, context.Context, string, *atomic.Int32) {
	t.Helper()
	harness := newTurnSpendHarness(t, true)
	harness.service.pendingLedger.firstRetry = time.Hour
	t.Cleanup(harness.service.pendingLedger.stopDrainer)
	ledger := newCtxWaitLedger(harness.ledger)
	harness.service.SetConsumptionLedger(ledger)
	base := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := harness.spawn(t, base)
	harness.spendAccount1(t, 950)

	turns := &atomic.Int32{}
	harness.backend.beforePrompt = func() { turns.Add(1) }

	ledger.failing.Store(true)
	if err := harness.prompt(base, sessionID); err != nil {
		t.Fatalf("first turn = %v, want it admitted under the cap", err)
	}
	if got := harness.service.pendingLedger.size(); got != 1 {
		t.Fatalf("pending entries = %d, want 1 — the failed write was dropped", got)
	}
	ledger.failing.Store(false)
	ledger.blocking.Store(true)
	return harness, ledger, base, sessionID, turns
}

func TestPromptSession_ACancelledPendingRetryEndsWithTheCaller(t *testing.T) {
	harness, ledger, base, sessionID, turns := pendingRetryHarness(t)

	ctx, cancel := context.WithCancel(base)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- harness.prompt(ctx, sessionID) }()
	select {
	case <-ledger.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the turn never retried the pending entry")
	}
	cancel()

	var err error
	select {
	case err = <-result:
	case <-time.After(3 * time.Second):
		t.Fatal("the cancelled prompt kept retrying the pending entry")
	}
	if !stderrors.Is(err, context.Canceled) {
		t.Errorf("cancelled turn = %v, want context.Canceled", err)
	}
	if stderrors.Is(err, ErrConsumptionPending) {
		t.Errorf("cancelled turn = %v, want its cancellation, not a pending-consumption fault", err)
	}
	if _, class := concludeFailedJob(t, err); class == FaultConsumptionPending {
		t.Errorf("cancelled turn classified %q, want it not requeued as consumption_pending", class)
	}
	if got := harness.service.pendingLedger.size(); got != 1 {
		t.Errorf("pending entries = %d, want 1 — the entry stays for the drainer", got)
	}
	if got := turns.Load(); got != 1 {
		t.Errorf("backend ran %d turns, want 1", got)
	}

	// The turn lock was released: the owner's next turn retries the entry,
	// stores it, and the cap now sees 1100 of 1000.
	ledger.blocking.Store(false)
	err = harness.prompt(base, sessionID)
	var exhausted *AccountQuotaExhaustedError
	if !stderrors.As(err, &exhausted) {
		t.Fatalf("turn after the cancelled retry = %v, want an *AccountQuotaExhaustedError", err)
	}
	if got := harness.service.pendingLedger.size(); got != 0 {
		t.Errorf("pending entries = %d, want 0 after a successful retry", got)
	}
	if got := turns.Load(); got != 1 {
		t.Errorf("backend ran %d turns, want 1", got)
	}
}

func TestPromptSession_APendingRetryKeepsTheCallersDeadline(t *testing.T) {
	harness, _, base, sessionID, _ := pendingRetryHarness(t)

	ctx, cancel := context.WithTimeout(base, 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := harness.prompt(ctx, sessionID)
	if elapsed := time.Since(start); elapsed >= 3*time.Second {
		t.Errorf("the pending retry ran %v past a 300ms deadline", elapsed)
	}
	if !stderrors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expired turn = %v, want context.DeadlineExceeded", err)
	}
	if stderrors.Is(err, ErrConsumptionPending) {
		t.Errorf("expired turn = %v, want its deadline, not a pending-consumption fault", err)
	}
	if got := harness.service.pendingLedger.size(); got != 1 {
		t.Errorf("pending entries = %d, want 1 — the entry stays for the drainer", got)
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

func TestPromptSession_AnInFlightUsageRecordIsVisibleToASessionSharingItsAccount(t *testing.T) {
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

	ledger.held = make(chan struct{}, 2)
	ledger.release = make(chan struct{})
	resultA := make(chan error, 1)
	go func() { resultA <- harness.prompt(ctx, sessionA) }()
	select {
	case <-ledger.held:
	case <-time.After(10 * time.Second):
		close(ledger.release)
		t.Fatal("session a never wrote its usage")
	}
	if got := harness.service.pendingLedger.size(); got != 1 {
		close(ledger.release)
		t.Fatalf("pending entries = %d while session a's usage write is in flight, want 1 — the entry is invisible until the write ends", got)
	}

	resultB := make(chan error, 1)
	go func() { resultB <- harness.prompt(ctx, sessionB) }()
	time.Sleep(200 * time.Millisecond)
	if got := turns.Load(); got != 1 {
		close(ledger.release)
		t.Fatalf("session b reached the backend while session a's usage write was in flight (%d turns, want 1): it was admitted against 950 of the 1100 tokens spent", got)
	}
	close(ledger.release)

	select {
	case err := <-resultA:
		if err != nil {
			t.Errorf("session a turn = %v, want nil — it was admitted under the cap", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("session a turn never returned")
	}
	select {
	case err := <-resultB:
		var exhausted *AccountQuotaExhaustedError
		if !stderrors.As(err, &exhausted) {
			t.Errorf("session b turn = %v, want an *AccountQuotaExhaustedError — 1100 of 1000 tokens are spent", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("session b turn never returned")
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
		t.Errorf("ledger holds %d tokens for acct-1, want 1100 — the entry written by both sessions must be stored once", got)
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
