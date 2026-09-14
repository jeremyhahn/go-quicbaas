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

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// =============================================================================
// One owner, one transition
// =============================================================================
//
// Session conclusion had three owners — an operator close, the supervisor's
// exit observer, and the spawn-time reconcile — each holding a fragment of the
// same ending. Every repair to one fragment broke an invariant of another: a
// map delete orphaned the backend teardown, a metadata guard still let two
// terminal events through, and a close after an exit reported not-found for a
// session that was simply already closed.
//
// These are the three failure modes that class produces. Each asserts the same
// three properties, because a single-owner transition either holds all of them
// or is not single-owner: EXACTLY ONE terminal event, backend teardown ran
// EXACTLY ONCE, and the session ends CLOSED and closable.

// countingBackend records how many times its teardown ran.
type countingBackend struct {
	mockAgentBackend
	closes atomic.Int32
}

func (b *countingBackend) Close(context.Context, string) error {
	b.closes.Add(1)
	return nil
}

// conclusionFixture wires a service with a live session ready to be concluded.
type conclusionFixture struct {
	svc     AgentService
	impl    *agentServiceImpl
	store   AgentStore
	log     *recordingSessionLog
	backend *countingBackend
	tenant  string
}

func newConclusionFixture(t *testing.T, sessionID string) *conclusionFixture {
	t.Helper()
	backend := &countingBackend{}
	f := newConclusionFixtureOn(t, sessionID, backend)
	f.backend = backend
	return f
}

// newConclusionFixtureOn builds the same live session over a caller-supplied
// backend, for the tests that need to drive the teardown itself. The fixture's
// backend field stays nil there: those tests hold the concrete gate already,
// and a *countingBackend field cannot name it.
func newConclusionFixtureOn(t *testing.T, sessionID string, backend AgentBackend) *conclusionFixture {
	t.Helper()
	ctx := context.Background()
	store := NewInMemoryAgentStore()
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	log := newRecordingSessionLog()
	impl.SetSessionEventLog(log)

	tenant := StorageTenant(ctx)
	if err := store.PutSession(ctx, &SessionRecord{
		SessionID: sessionID, TenantID: tenant, Backend: "mock", Status: SessionStateActive,
	}); err != nil {
		t.Fatalf("PutSession: %v", err)
	}
	if err := log.PutMeta(ctx, &SessionMeta{
		SessionID: sessionID, TenantID: tenant, Status: SessionStateActive,
	}); err != nil {
		t.Fatalf("PutMeta: %v", err)
	}
	impl.sessions.Store(sessionID, &sessionRecord{Backend: "mock", TenantID: tenant})

	return &conclusionFixture{svc: svc, impl: impl, store: store, log: log, tenant: tenant}
}

// assertConcludedExactlyOnce is the shared contract of all three failure modes.
func (f *conclusionFixture) assertConcludedExactlyOnce(t *testing.T, sessionID string) {
	t.Helper()
	if got := f.log.closed; got != 1 {
		t.Errorf("terminal events = %d, want exactly 1; an attacher must not see the session end twice", got)
	}
	if got := f.backend.closes.Load(); got != 1 {
		t.Errorf("backend teardowns = %d, want exactly 1; the process and its provisioned material are released once", got)
	}
	record, err := f.store.GetSession(context.Background(), f.tenant, sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status != SessionStateClosed {
		t.Errorf("store record = %q, want closed", record.Status)
	}
	if got := f.log.status(sessionID); got != SessionStateClosed {
		t.Errorf("session-log metadata = %q, want closed", got)
	}
}

func TestConcludeSession_ConcurrentExitReportsProduceOneEnding(t *testing.T) {
	// Failure mode 1: two RecordSessionExit calls at the same instant. The
	// metadata read-then-write this replaced could let both through, because
	// GetMeta, Append and PutMeta are three operations under three locks.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-concurrent-exit")

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			f.impl.RecordSessionExit(ctx, f.tenant, "s-concurrent-exit", "agent process exited")
		}()
	}
	close(start)
	wg.Wait()

	f.assertConcludedExactlyOnce(t, "s-concurrent-exit")
}

func TestConcludeSession_OperatorCloseRacingTheExitObserverProducesOneEnding(t *testing.T) {
	// Failure mode 2, and the one that actually happens: closing a session ENDS
	// its process, which fires the supervisor's observer. The two paths cross
	// by construction, so the transition has to arbitrate rather than the
	// timing.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-race")

	var wg sync.WaitGroup
	start := make(chan struct{})
	var closeErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, closeErr = f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-race"})
	}()
	go func() {
		defer wg.Done()
		<-start
		f.impl.RecordSessionExit(ctx, f.tenant, "s-race", "agent process exited")
	}()
	close(start)
	wg.Wait()

	if closeErr != nil {
		t.Errorf("CloseSession racing the observer returned %v; the session is closed either way", closeErr)
	}
	f.assertConcludedExactlyOnce(t, "s-race")
}

func TestConcludeSession_ACloseAfterTheExitIsIdempotentSuccess(t *testing.T) {
	// Failure mode 3: the ordered version of the same race. The operator asked
	// for the session to be closed and it IS closed — reporting not-found would
	// tell the truth about who performed the transition and a lie about the
	// outcome.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-late-close")

	f.impl.RecordSessionExit(ctx, f.tenant, "s-late-close", "agent process exited")

	resp, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-late-close"})
	if err != nil {
		t.Fatalf("CloseSession after the exit = %v, want idempotent success", err)
	}
	if resp == nil || resp.SessionID != "s-late-close" {
		t.Fatalf("CloseSession response = %+v, want the session it was asked about", resp)
	}
	f.assertConcludedExactlyOnce(t, "s-late-close")
}

func TestConcludeSession_TheTerminalEventNamesWhoEndedItAndWhy(t *testing.T) {
	// ATTRIBUTION. An event that records only that a session ended leaves its
	// reader to infer the cause from what happened next — which is exactly how
	// a dispatch death got read as a hang for ninety-seven minutes.
	ctx := context.Background()

	for _, tc := range []struct {
		name     string
		session  string
		conclude func(f *conclusionFixture)
		want     SessionConclusionReason
	}{
		{
			name: "process exit", session: "s-attr-exit", want: ConcludedByProcessExit,
			conclude: func(f *conclusionFixture) {
				f.impl.RecordSessionExit(ctx, f.tenant, "s-attr-exit", "agent process exited")
			},
		},
		{
			name: "operator", session: "s-attr-op", want: ConcludedByOperator,
			conclude: func(f *conclusionFixture) {
				if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-attr-op"}); err != nil {
					t.Fatalf("CloseSession: %v", err)
				}
			},
		},
		{
			name: "reconcile", session: "s-attr-rec", want: ConcludedByReconcile,
			conclude: func(f *conclusionFixture) {
				_, _ = f.impl.concludeSession(ctx, f.tenant, f.tenant, "s-attr-rec", ConcludedByReconcile, conclusionNudge, nil)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newConclusionFixture(t, tc.session)
			tc.conclude(f)

			reason, ok := f.log.terminalReason(tc.session)
			if !ok {
				t.Fatalf("no terminal event was recorded for %s", tc.session)
			}
			if reason != string(tc.want) {
				t.Errorf("terminal event reason = %q, want %q — the record must name what actually happened",
					reason, tc.want)
			}
		})
	}
}

func TestConcludeSession_RefusesASessionItHasNoRecordOf(t *testing.T) {
	// The record is the only thing that can arbitrate, so a conclusion without
	// one would be a second owner by another name. It is also the honest answer
	// for the spawn window: the supervisor starts before backend.Spawn returns,
	// so an agent that dies immediately can reach the observer before either the
	// service record or the durable record exists. Nothing is torn down and
	// nothing is written; reconcileSpawnedSessionState closes that window from
	// the other side, once the record is there.
	ctx := context.Background()
	store := NewInMemoryAgentStore()
	backend := &countingBackend{}
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	log := newRecordingSessionLog()
	impl.SetSessionEventLog(log)

	tenant := StorageTenant(ctx)
	if err := store.PutSession(ctx, &SessionRecord{
		SessionID: "s-unknown-to-us", TenantID: tenant, Status: SessionStateActive,
	}); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	concluded, err := impl.concludeSession(
		ctx, tenant, tenant, "s-unknown-to-us", ConcludedByProcessExit, conclusionNudge, backend)
	if err != nil {
		t.Fatalf("concludeSession: %v", err)
	}
	if concluded {
		t.Fatal("a session with no live record was concluded; nothing arbitrated that decision")
	}
	if got := backend.closes.Load(); got != 0 {
		t.Errorf("backend teardowns = %d, want 0", got)
	}
	if log.closed != 0 {
		t.Errorf("terminal events = %d, want 0", log.closed)
	}
	record, err := store.GetSession(ctx, tenant, "s-unknown-to-us")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status != SessionStateActive {
		t.Errorf("store record = %q, want it untouched", record.Status)
	}
}

func TestConcludeSession_AFailedTeardownGivesTheRightToConcludeBack(t *testing.T) {
	// A teardown that failed for a reason other than "already gone" means the
	// process may still be running, so the session is NOT concluded. Holding the
	// swap would make it permanently unconcludable — the same stranded state
	// this whole issue is about — so the winner releases it and the close is
	// retryable.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-stubborn")
	refuse := errors.New("backend refused the teardown")
	f.impl.backends.Store("mock", &refusingBackend{err: refuse})

	_, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-stubborn"})
	if err == nil {
		t.Fatal("CloseSession reported success over a teardown that failed; the agent may still be running")
	}
	if f.log.closed != 0 {
		t.Errorf("terminal events = %d, want 0 — nothing ended", f.log.closed)
	}

	// And the session is still concludable: the second attempt, against a
	// backend that cooperates, succeeds.
	f.impl.backends.Store("mock", f.backend)
	if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-stubborn"}); err != nil {
		t.Fatalf("the retry after a failed teardown was refused: %v", err)
	}
	f.assertConcludedExactlyOnce(t, "s-stubborn")
}

// refusingBackend fails every teardown.
type refusingBackend struct {
	mockAgentBackend
	err error
}

func (b *refusingBackend) Close(context.Context, string) error { return b.err }

// gatedBackend serves its teardown one of two ways, because the tests need two
// different things from it.
//
// HELD: entered/release park each Close until the test releases it, which is
// how a losing caller is placed exactly inside the winner's teardown. Both
// channels are buffered and signalled per call rather than closed once, because
// a session whose teardown failed is still closable and the test has to watch
// that second attempt too.
//
// STANDING: stand() fixes an outcome that every Close returns without parking.
// A test that cannot predict HOW MANY teardown attempts a schedule produces
// must use this — queueing per-call values instead binds each outcome to a
// position in a sequence the scheduler chooses, so leftover queued values are
// served to a later attempt that expected a different answer.
type gatedBackend struct {
	mockAgentBackend
	entered  chan struct{}
	release  chan error
	standing atomic.Pointer[gateOutcome]
	closes   atomic.Int32
}

// gateOutcome boxes an error so a nil outcome is still distinguishable from no
// standing outcome at all.
type gateOutcome struct{ err error }

func newGatedBackend() *gatedBackend {
	return &gatedBackend{entered: make(chan struct{}, 8), release: make(chan error, 8)}
}

// stand fixes the outcome of every subsequent teardown, including a nil one.
func (b *gatedBackend) stand(err error) { b.standing.Store(&gateOutcome{err: err}) }

func (b *gatedBackend) Close(context.Context, string) error {
	b.closes.Add(1)
	if out := b.standing.Load(); out != nil {
		return out.err
	}
	b.entered <- struct{}{}
	return <-b.release
}

func TestConcludeSession_ALoserDoesNotPersistWhileTheWinnerIsStillTearingDown(t *testing.T) {
	// THE BARE CAS SAYS SOMEBODY OWNS THIS; IT DOES NOT SAY THEY HAVE FINISHED.
	// A loser that went straight to the durable write could mark the record
	// CLOSED, emit the terminal event under its OWN reason, and evict the live
	// record while the winner was still inside backend.Close.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-midflight")
	gate := newGatedBackend()
	f.impl.backends.Store("mock", gate)

	// The operator wins and blocks inside its teardown.
	winnerDone := make(chan struct{})
	go func() {
		defer close(winnerDone)
		_, _ = f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-midflight"})
	}()
	<-gate.entered

	// The exit observer arrives mid-teardown and must write NOTHING.
	f.impl.RecordSessionExit(ctx, f.tenant, "s-midflight", "agent process exited")

	if f.log.closed != 0 {
		t.Errorf("a terminal event was written while the winner was still tearing down (%d)", f.log.closed)
	}
	record, err := f.store.GetSession(ctx, f.tenant, "s-midflight")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status == SessionStateClosed {
		t.Error("the record was marked closed before the teardown finished")
	}
	if _, live := f.impl.sessions.Load("s-midflight"); !live {
		t.Error("the live arbitration record was evicted mid-teardown; the winner has nothing to retry with")
	}

	gate.release <- nil
	<-winnerDone

	// And the completed ending is the OPERATOR's, once, with the operator's
	// reason — not the observer's.
	if f.log.closed != 1 {
		t.Errorf("terminal events = %d, want exactly 1", f.log.closed)
	}
	if reason, ok := f.log.terminalReason("s-midflight"); !ok || reason != string(ConcludedByOperator) {
		t.Errorf("terminal reason = %q (present=%v), want %q — a deliberate close must not be recorded as a crash",
			reason, ok, ConcludedByOperator)
	}
	if got := gate.closes.Load(); got != 1 {
		t.Errorf("backend teardowns = %d, want exactly 1", got)
	}
}

func TestConcludeSession_AFailedTeardownMidFlightLeavesTheSessionRetryable(t *testing.T) {
	// The other half of the same race: if the winner's teardown FAILS while a
	// loser is watching, the loser must not have persisted anything — or the
	// winner's release of ownership would have nothing left to retry with, and
	// the durable record would falsely say closed over a process that may still
	// be running.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-midflight-fail")
	gate := newGatedBackend()
	f.impl.backends.Store("mock", gate)

	closeErr := make(chan error, 1)
	go func() {
		_, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-midflight-fail"})
		closeErr <- err
	}()
	<-gate.entered

	f.impl.RecordSessionExit(ctx, f.tenant, "s-midflight-fail", "agent process exited")
	gate.release <- errors.New("backend refused the teardown")

	if err := <-closeErr; err == nil {
		t.Fatal("CloseSession reported success over a teardown that failed")
	}
	if f.log.closed != 0 {
		t.Errorf("terminal events = %d, want 0 — nothing ended", f.log.closed)
	}
	record, err := f.store.GetSession(ctx, f.tenant, "s-midflight-fail")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status == SessionStateClosed {
		t.Error("the record says closed over a process that may still be running")
	}

	// Still concludable: a cooperating backend ends it cleanly.
	f.impl.backends.Store("mock", f.backend)
	if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-midflight-fail"}); err != nil {
		t.Fatalf("the retry after a mid-flight teardown failure was refused: %v", err)
	}
	f.assertConcludedExactlyOnce(t, "s-midflight-fail")
}

func TestConcludedSession_TakesNoFurtherTurnsAndAcceptsNoFurtherEvents(t *testing.T) {
	// TWO GUARDS, ONE INVARIANT. KindSessionClosed is the last event a session
	// has. The prompt path refuses above the backend so nothing is even
	// attempted; the log refuses on the one path every writer shares, so a
	// caller that reaches it another way cannot land a write after the ending —
	// or, because the sequence derives from metadata a re-registration can
	// reset, ON TOP of it.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-done")

	if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-done"}); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	f.assertConcludedExactlyOnce(t, "s-done")

	// The log refuses an ordinary append after the ending, typed.
	err := AppendNextSessionEvent(ctx, f.log, &SessionEvent{
		SessionID: "s-done", TenantID: f.tenant, Kind: KindSystemMessage,
		System: &SystemMessagePayload{Level: "info", Message: "late"},
	})
	if err == nil {
		t.Fatal("the log accepted an append after the terminal event")
	}
	if !errors.Is(err, ErrSessionLogConcluded) {
		t.Errorf("append error = %v, want ErrSessionLogConcluded — the refusal must be identifiable", err)
	}
	if f.log.closed != 1 {
		t.Errorf("terminal events = %d after the late append, want 1", f.log.closed)
	}
}

func TestPromptSession_RefusesAConcludedSessionBeforeTouchingAnything(t *testing.T) {
	// The refusal happens above the backend and above the log, so a prompt to a
	// dead session neither reaches a process that is gone nor writes an attempt
	// after the session's ending.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-prompt-after")
	prompts := f.backend.closes.Load() // unrelated counter; asserted via the log below

	if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-prompt-after"}); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	eventsAfterClose := f.log.closed
	_ = prompts

	_, err := f.svc.PromptSession(ctx, &PromptSessionRequest{
		SessionID: "s-prompt-after", Prompt: "are you there",
	})
	if err == nil {
		t.Fatal("PromptSession accepted a turn on a concluded session")
	}
	if !errors.Is(err, ErrSessionClosed) {
		t.Errorf("prompt error = %v, want ErrSessionClosed", err)
	}
	if f.log.closed != eventsAfterClose {
		t.Errorf("the refused prompt still wrote to the session log")
	}
}

func TestConcludeSession_ARacingAppendCannotLandAfterTheEnding(t *testing.T) {
	// "closed is last" has to hold under concurrency, not merely in sequence.
	// The gate refuses a non-terminal append to a session whose metadata says
	// closed — so the metadata is closed BEFORE the terminal event is written,
	// shutting the gate before the ending exists. Written the other way round,
	// a racing chunk read ACTIVE, passed the gate, and landed after the ending.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-racing-append")

	if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-racing-append"}); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}

	// Every ordinary append from here on is refused, including a retry that
	// would otherwise re-read the sequence and land late.
	for i := 0; i < 4; i++ {
		err := AppendNextSessionEvent(ctx, f.log, &SessionEvent{
			SessionID: "s-racing-append", TenantID: f.tenant, Kind: KindAssistantChunk,
			AssistantChunk: &AssistantChunkPayload{Text: "late"},
		})
		if !errors.Is(err, ErrSessionLogConcluded) {
			t.Fatalf("append %d = %v, want ErrSessionLogConcluded", i, err)
		}
	}
	if f.log.closed != 1 {
		t.Errorf("terminal events = %d, want 1", f.log.closed)
	}
	if got := f.log.status("s-racing-append"); got != SessionStateClosed {
		t.Errorf("log metadata = %q, want closed", got)
	}
}

func TestConcludeSession_AFailedLogCloseWritesNoEndingAndStaysRetryable(t *testing.T) {
	// PutMeta is what shuts the gate that stops every other writer appending
	// after the ending. Treating its failure as a warning and appending the
	// terminal event anyway left the gate OPEN behind a session that had already
	// ended — so an ordinary output event could still land after it — and then
	// marking the conclusion persisted and evicting the live record threw away
	// the state a retry would have needed.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-log-fault")
	f.log.failPutMeta(errors.New("log store refused the metadata write"))

	_, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-log-fault"})
	if err == nil {
		t.Fatal("CloseSession reported success while the session's log was left open")
	}

	// NO ENDING WAS WRITTEN behind the open gate.
	if f.log.closed != 0 {
		t.Errorf("terminal events = %d, want 0 — an ending behind an open gate is unrepairable", f.log.closed)
	}
	if got := f.log.status("s-log-fault"); got != SessionStateActive {
		t.Errorf("log metadata = %q, want it unchanged at active", got)
	}

	// AND THE RETRY STATE SURVIVED: not marked persisted, not evicted.
	mem, live := f.impl.sessions.Load("s-log-fault")
	if !live {
		t.Fatal("the live arbitration record was evicted; the failed log transition can never be retried")
	}
	if mem.(*sessionRecord).persisted.Load() {
		t.Error("the conclusion was marked persisted while its log transition had failed")
	}

	// The teardown already happened, so a retry must not repeat it.
	if got := f.backend.closes.Load(); got != 1 {
		t.Errorf("backend teardowns = %d, want exactly 1", got)
	}

	// THE RETRY COMPLETES IT, exactly once.
	f.log.failPutMeta(nil)
	if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-log-fault"}); err != nil {
		t.Fatalf("the retry after a failed log close was refused: %v", err)
	}
	if f.log.closed != 1 {
		t.Errorf("terminal events after the retry = %d, want exactly 1", f.log.closed)
	}
	if got := f.log.status("s-log-fault"); got != SessionStateClosed {
		t.Errorf("log metadata after the retry = %q, want closed", got)
	}
	if got := f.backend.closes.Load(); got != 1 {
		t.Errorf("backend teardowns after the retry = %d, want still exactly 1", got)
	}

	// And the gate is shut: ordinary appends are now refused.
	appendErr := AppendNextSessionEvent(ctx, f.log, &SessionEvent{
		SessionID: "s-log-fault", TenantID: f.tenant, Kind: KindAssistantChunk,
		AssistantChunk: &AssistantChunkPayload{Text: "late"},
	})
	if !errors.Is(appendErr, ErrSessionLogConcluded) {
		t.Errorf("append after the completed retry = %v, want ErrSessionLogConcluded", appendErr)
	}
}

func TestConcludeSession_AFailedEndingIsWrittenByTheRetryNotSkipped(t *testing.T) {
	// Shutting the gate and writing the ending are two writes, and the second
	// one can fail on its own. Treating the SHUT GATE as the record that the
	// transition happened made that failure permanent: the retry read closed
	// metadata, concluded somebody else had finished, and declined to write the
	// one event that was missing — leaving a session whose listing said closed
	// and whose stream never ended. The ending itself is the marker now.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-ending-fault")
	f.log.failAppend(errors.New("log store refused the event write"))

	_, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-ending-fault"})
	if err == nil {
		t.Fatal("CloseSession reported success while the session's log had no ending")
	}

	// The gate did shut — that write succeeded — but no ending landed.
	if got := f.log.status("s-ending-fault"); got != SessionStateClosed {
		t.Errorf("log metadata = %q, want closed", got)
	}
	if f.log.closed != 0 {
		t.Errorf("terminal events = %d, want 0", f.log.closed)
	}

	// AND THE RETRY STATE SURVIVED: not marked persisted, not evicted.
	mem, live := f.impl.sessions.Load("s-ending-fault")
	if !live {
		t.Fatal("the live arbitration record was evicted; the missing ending can never be written")
	}
	if mem.(*sessionRecord).persisted.Load() {
		t.Error("the conclusion was marked persisted while its log had no ending")
	}
	if got := f.backend.closes.Load(); got != 1 {
		t.Errorf("backend teardowns = %d, want exactly 1", got)
	}

	// THE RETRY WRITES THE MISSING ENDING, exactly once, and does not repeat
	// the teardown that already succeeded.
	f.log.failAppend(nil)
	if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-ending-fault"}); err != nil {
		t.Fatalf("the retry after a failed ending was refused: %v", err)
	}
	if f.log.closed != 1 {
		t.Errorf("terminal events after the retry = %d, want exactly 1", f.log.closed)
	}
	if reason, ok := f.log.terminalReason("s-ending-fault"); !ok || reason != ConcludedByOperator.String() {
		t.Errorf("terminal event reason = %q (present=%v), want %q",
			reason, ok, ConcludedByOperator.String())
	}
	if got := f.backend.closes.Load(); got != 1 {
		t.Errorf("backend teardowns after the retry = %d, want still exactly 1", got)
	}

	// A FURTHER RETRY WRITES NOTHING: the ending in the log is what makes it
	// exactly-once, so the caller that finds one already there stops.
	if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-ending-fault"}); err != nil {
		t.Fatalf("a second retry was refused: %v", err)
	}
	if f.log.closed != 1 {
		t.Errorf("terminal events after a second retry = %d, want still exactly 1", f.log.closed)
	}
}

func TestConcludeSession_TheEndingProbeDoesNotTrustLaggingMetadata(t *testing.T) {
	// The cheap way to find a log's tail is the sequence on its metadata, and
	// the KV log rolls that forward in a SECOND write it does not report the
	// failure of — so the cheap answer can sit behind events that are really
	// there. Reading only the event AT that sequence would read behind the
	// ending, report a concluded session as unfinished, and let the retry
	// append a second one.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-lagging-meta")

	for i := 0; i < 3; i++ {
		if err := AppendNextSessionEvent(ctx, f.log, &SessionEvent{
			SessionID: "s-lagging-meta", TenantID: f.tenant, Kind: KindAssistantChunk,
			AssistantChunk: &AssistantChunkPayload{Text: "output"},
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-lagging-meta"}); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if f.log.closed != 1 {
		t.Fatalf("terminal events = %d, want exactly 1", f.log.closed)
	}

	// A roll-forward write that failed leaves the events dense and correct and
	// the cheap answer stale. THE PROBE MUST STILL FIND THE ENDING.
	f.log.rewindMeta("s-lagging-meta", 0)
	ended, err := sessionLogEnded(ctx, f.log, f.tenant, "s-lagging-meta")
	if err != nil {
		t.Fatalf("sessionLogEnded: %v", err)
	}
	if !ended {
		t.Fatal("the ending probe read behind the ending; a retry would append a second one")
	}

	// And a real retry writes nothing, which is the consequence that matters.
	// It also reports the reason ALREADY on the log rather than its own, since
	// that ending is the one the fleet agreed on.
	effective, err := f.impl.concludeLog(ctx, &sessionRecord{}, f.tenant, "s-lagging-meta", ConcludedByReconcile)
	if err != nil {
		t.Fatalf("retried conclusion: %v", err)
	}
	if effective == "" {
		t.Error("the retry reported no attribution for an ending that is on the log")
	}
	if f.log.closed != 1 {
		t.Errorf("terminal events after the retry = %d, want still exactly 1", f.log.closed)
	}
}

// gatedFixture is newConclusionFixture over a backend whose teardown can be
// held open or given a standing outcome.
func gatedFixture(t *testing.T, sessionID string) (*conclusionFixture, *gatedBackend) {
	t.Helper()
	backend := newGatedBackend()
	return newConclusionFixtureOn(t, sessionID, backend), backend
}

// concludingRecord installs a live session whose ending is already OWNED by
// somebody else, so a losing caller's path can be exercised without racing two
// goroutines for the swap. owner is returned so the test drives the outcome.
func concludingRecord(f *conclusionFixture, sessionID string) (*sessionRecord, *sessionConclusion) {
	mem := &sessionRecord{Backend: "mock", TenantID: f.tenant}
	owner := &sessionConclusion{reason: ConcludedByProcessExit, done: make(chan struct{})}
	mem.concluded.Store(true)
	mem.conclusion.Store(owner)
	f.impl.sessions.Store(sessionID, mem)
	return mem, owner
}

func TestCloseSession_ALoserReportsTheOwnersFailedTeardownAsItsOwn(t *testing.T) {
	// AN OPERATOR'S CLOSE IS A CLAIM ABOUT THE WORLD. The loser used to return
	// idempotent success the instant it saw somebody else owning the ending —
	// fail-open for the one caller that must not fail open. If the owner's
	// teardown then FAILED, the agent process was still running and an operator
	// was holding a successful close for a session that was never closed.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-loser-failed-owner")
	_, owner := concludingRecord(f, "s-loser-failed-owner")

	teardownErr := errors.New("agent refused to shut down")
	owner.err = teardownErr
	owner.failed.Store(true)
	close(owner.done)

	_, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-loser-failed-owner"})
	if err == nil {
		t.Fatal("the losing close reported success over a teardown that failed")
	}
	if !errors.Is(err, teardownErr) {
		t.Errorf("losing close = %v, want it to name the owner's teardown failure", err)
	}
	if f.log.closed != 0 {
		t.Errorf("terminal events = %d, want 0; nothing was concluded", f.log.closed)
	}
	if got := f.backend.closes.Load(); got != 0 {
		t.Errorf("backend teardowns by the loser = %d, want 0; losing forbids doing the work", got)
	}
}

func TestCloseSession_ALoserReportsSuccessOnlyOnceClosureIsConfirmed(t *testing.T) {
	// The mirror case, and the one that says the wait is not merely a way to
	// turn every concurrent close into an error: when the owner's teardown
	// SUCCEEDED, the loser returns success — but only after the durable record
	// confirms it, which here means recording it, since the owner had not.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-loser-ok-owner")
	mem, owner := concludingRecord(f, "s-loser-ok-owner")
	close(owner.done) // teardown finished, durable half still owed

	if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-loser-ok-owner"}); err != nil {
		t.Fatalf("losing close = %v, want success", err)
	}

	// CONFIRMED, not assumed.
	if !mem.persisted.Load() {
		t.Error("success was reported without the durable half being confirmed")
	}
	record, err := f.store.GetSession(ctx, f.tenant, "s-loser-ok-owner")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status != SessionStateClosed {
		t.Errorf("store record = %q, want closed", record.Status)
	}
	if f.log.closed != 1 {
		t.Errorf("terminal events = %d, want exactly 1", f.log.closed)
	}
	if reason, _ := f.log.terminalReason("s-loser-ok-owner"); reason != ConcludedByProcessExit.String() {
		t.Errorf("terminal reason = %q, want the OWNER's reason", reason)
	}
	if got := f.backend.closes.Load(); got != 0 {
		t.Errorf("backend teardowns by the loser = %d, want 0", got)
	}
}

func TestCloseSession_AWaitThatOutlivesItsDeadlineReportsInProgressNotSuccess(t *testing.T) {
	// The wait is bounded by the CALLER'S OWN deadline — there is no timeout of
	// its own to tune — and what it reports when that deadline passes is that a
	// conclusion is running. Reporting success would be the original defect with
	// a longer fuse: an operator holding a successful close for a teardown whose
	// outcome nobody ever learned.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-impatient")
	concludingRecord(f, "s-impatient") // owner in flight: done stays open

	impatient, cancel := context.WithCancel(ctx)
	cancel()

	_, err := f.svc.CloseSession(impatient, &CloseSessionRequest{SessionID: "s-impatient"})
	if err == nil {
		t.Fatal("the impatient close reported success while the outcome was unknown")
	}
	if !errors.Is(err, ErrSessionConclusionInProgress) {
		t.Errorf("impatient close = %v, want ErrSessionConclusionInProgress", err)
	}
	if f.log.closed != 0 {
		t.Errorf("terminal events = %d, want 0", f.log.closed)
	}
}

func TestCloseSession_ConcurrentClosesNeverReportSuccessOverAFailedTeardown(t *testing.T) {
	// The property under real concurrency, rather than one pinned interleaving:
	// whichever of two simultaneous closes wins the swap, NEITHER may answer
	// success while every teardown attempt is failing. A STANDING failure is
	// what makes that a property and not a guess — the number of attempts is
	// the scheduler's to choose, and the assertion holds for every choice.
	ctx := context.Background()
	f, backend := gatedFixture(t, "s-two-closers")
	backend.stand(errors.New("agent refused to shut down"))

	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-two-closers"})
			results <- err
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			t.Fatal("a concurrent close reported success while every teardown was failing")
		}
	}
	if f.log.closed != 0 {
		t.Errorf("terminal events = %d, want 0; nothing was concluded", f.log.closed)
	}

	// AND THE SESSION IS STILL CLOSABLE: the next close retries the teardown.
	backend.stand(nil)
	if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-two-closers"}); err != nil {
		t.Fatalf("the retry after failed teardowns = %v, want success", err)
	}
	if f.log.closed != 1 {
		t.Errorf("terminal events = %d, want exactly 1", f.log.closed)
	}
	record, err := f.store.GetSession(ctx, f.tenant, "s-two-closers")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status != SessionStateClosed {
		t.Errorf("store record = %q, want closed", record.Status)
	}
}

func TestRecordSessionExit_ASystemObserverStillLosesWithoutWaiting(t *testing.T) {
	// The other half of the split: the observer is a NUDGE, so losing to an
	// in-flight conclusion returns immediately rather than parking a supervisor
	// goroutine behind somebody else's teardown. If it waited, this test would
	// hang rather than fail — which is the assertion.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-observer-loses")
	_, owner := concludingRecord(f, "s-observer-loses")

	f.impl.RecordSessionExit(ctx, f.tenant, "s-observer-loses", "agent process exited")

	if f.log.closed != 0 {
		t.Errorf("the observer wrote an ending while another conclusion was in flight (%d)", f.log.closed)
	}
	if got := f.backend.closes.Load(); got != 0 {
		t.Errorf("the observer tore down a session it did not own (%d)", got)
	}
	close(owner.done)
}

func TestSessionLog_TheTerminalExemptionIsKeyedToAMissingEnding(t *testing.T) {
	// The append gate exempts terminal events so a conclusion whose gate closed
	// but whose ending never landed can still be completed. Exempting the KIND
	// outright made "exactly one ending" rest on the conclusion being the only
	// writer that ever constructs one — a fact about today's callers, not about
	// the log. The exemption is granted only while the ending is MISSING.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-second-ending")

	if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-second-ending"}); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if f.log.closed != 1 {
		t.Fatalf("terminal events = %d, want 1", f.log.closed)
	}

	// A SECOND ENDING IS REFUSED LIKE ANY OTHER LATE APPEND, typed.
	err := AppendNextSessionEvent(ctx, f.log, &SessionEvent{
		SessionID: "s-second-ending", TenantID: f.tenant, Kind: KindSessionClosed,
		Closed: &SessionClosedPayload{Reason: "some other writer's idea of an ending"},
	})
	if !errors.Is(err, ErrSessionLogConcluded) {
		t.Fatalf("second terminal append = %v, want ErrSessionLogConcluded", err)
	}
	if f.log.closed != 1 {
		t.Errorf("terminal events = %d, want still exactly 1", f.log.closed)
	}

	// While the ending is MISSING the exemption still applies: that is the
	// repair the exemption exists for.
	g := newConclusionFixture(t, "s-gate-without-ending")
	g.log.failAppend(errors.New("log store refused the event write"))
	if _, err := g.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-gate-without-ending"}); err == nil {
		t.Fatal("precondition: the first close should have failed")
	}
	g.log.failAppend(nil)
	if err := AppendNextSessionEvent(ctx, g.log, &SessionEvent{
		SessionID: "s-gate-without-ending", TenantID: g.tenant, Kind: KindSessionClosed,
		Closed: &SessionClosedPayload{Reason: ConcludedByReconcile.String()},
	}); err != nil {
		t.Fatalf("terminal append into a shut gate with no ending = %v, want it permitted", err)
	}
}

func TestSessionLog_AMetadataBlipMayNotOpenTheGate(t *testing.T) {
	// FAIL-OPEN WAS THE DEFECT, AND IT WAS SILENT. `sessionLogConcluded` turned
	// EVERY failed GetMeta into "not concluded", so a metadata read that merely
	// blipped was enough to carry an ordinary event past a shut gate and land it
	// after KindSessionClosed — the invariant defeated through a path that
	// reported nothing wrong. It is the same misattribution the conclusion
	// transition already fixed — a store that CANNOT ANSWER is not a session the
	// log NEVER REGISTERED — surviving one surface over, on the append gate.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-gate-blip")

	if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-gate-blip"}); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	ended := f.log.eventCount("s-gate-blip")
	if f.log.lastKind("s-gate-blip") != KindSessionClosed {
		t.Fatalf("precondition: the log should end in its ending, got %q", f.log.lastKind("s-gate-blip"))
	}

	// The store blips on exactly the read the gate depends on. The append that
	// would otherwise have succeeded must FAIL.
	blip := errors.New("metadata store blipped")
	f.log.failGetMetaOnce(blip)
	err := AppendNextSessionEvent(ctx, f.log, &SessionEvent{
		SessionID: "s-gate-blip", TenantID: f.tenant, Kind: KindAssistantChunk,
		AssistantChunk: &AssistantChunkPayload{Text: "output from after the ending"},
	})
	if err == nil {
		t.Fatal("the append landed on a gate read that failed — fail-open is back")
	}
	if !errors.Is(err, blip) {
		t.Errorf("append = %v, want it to name the failed metadata read", err)
	}
	// AND IT IS NOT DRESSED UP AS A CONCLUSION. The gate did not answer; saying
	// so is what tells the caller a retry is the right move.
	if errors.Is(err, ErrSessionLogConcluded) {
		t.Error("a read that failed was reported as a session that concluded")
	}
	if got := f.log.eventCount("s-gate-blip"); got != ended {
		t.Errorf("events = %d, want %d; the refused append still landed", got, ended)
	}

	// THE RETRY IS THE POINT OF FAILING CLOSED. With the store answering again
	// the gate is shut, so the event is refused for the real reason rather than
	// landing late behind a blip nobody saw.
	err = AppendNextSessionEvent(ctx, f.log, &SessionEvent{
		SessionID: "s-gate-blip", TenantID: f.tenant, Kind: KindAssistantChunk,
		AssistantChunk: &AssistantChunkPayload{Text: "output from after the ending"},
	})
	if !errors.Is(err, ErrSessionLogConcluded) {
		t.Fatalf("retry after the blip = %v, want ErrSessionLogConcluded", err)
	}
	if got := f.log.eventCount("s-gate-blip"); got != ended {
		t.Errorf("events = %d, want %d; nothing may follow the ending", got, ended)
	}
	if got := f.log.lastKind("s-gate-blip"); got != KindSessionClosed {
		t.Errorf("last event = %q, want the ending to still be last", got)
	}
}

func TestSessionLog_ASessionTheLogNeverRegisteredIsStillAnAnswer(t *testing.T) {
	// The other half of the split, and the reason the fix is not simply
	// "propagate every error": ErrSessionLogNotFound is the store ANSWERING,
	// and the answer is that there is no log here to have concluded. Treating it
	// as a failure would refuse the first event of every session the log has not
	// registered yet — turning a fail-open bug into a fail-shut one.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-unregistered")
	f.log.forget("s-unregistered")

	if err := AppendNextSessionEvent(ctx, f.log, &SessionEvent{
		SessionID: "s-unregistered", TenantID: f.tenant, Kind: KindAssistantChunk,
		AssistantChunk: &AssistantChunkPayload{Text: "the first thing this session ever said"},
	}); err != nil {
		t.Fatalf("append to an unregistered session = %v, want it permitted", err)
	}
	if got := f.log.eventCount("s-unregistered"); got != 1 {
		t.Errorf("events = %d, want 1", got)
	}
}

func TestSessionLog_AWriterOvertakenByTheEndingCannotLandAfterIt(t *testing.T) {
	// THE GATE AND THE SEQUENCE ARE NOT ONE ATOMIC READ, so an ordinary writer
	// can read an OPEN gate and then be overtaken by a conclusion. Checked
	// before the sequence read, such a writer went on to read a sequence PAST
	// the ending and appended after it with nothing violated along the way.
	//
	// The sequence is read first now, so the log's own monotonicity check
	// catches exactly this writer: the ending took the slot it was promised, the
	// append collides, and the retry re-reads a gate that is by then shut.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-overtaken")

	// The next gate read is answered from a snapshot taken just before the
	// conclusion lands — the writer is told ACTIVE by a log that is closing.
	f.log.onceAfterGetMeta(func() {
		if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-overtaken"}); err != nil {
			t.Errorf("the overtaking conclusion failed: %v", err)
		}
	})

	err := AppendNextSessionEvent(ctx, f.log, &SessionEvent{
		SessionID: "s-overtaken", TenantID: f.tenant, Kind: KindAssistantChunk,
		AssistantChunk: &AssistantChunkPayload{Text: "output from a writer that was overtaken"},
	})
	if !errors.Is(err, ErrSessionLogConcluded) {
		t.Fatalf("overtaken append = %v, want ErrSessionLogConcluded", err)
	}

	// THE ENDING IS STILL THE LAST EVENT, which is the invariant itself.
	ended, endedErr := sessionLogEnded(ctx, f.log, f.tenant, "s-overtaken")
	if endedErr != nil {
		t.Fatalf("sessionLogEnded: %v", endedErr)
	}
	if !ended {
		t.Error("an ordinary event landed after the terminal event")
	}
	if f.log.closed != 1 {
		t.Errorf("terminal events = %d, want exactly 1", f.log.closed)
	}
}

func TestConcludeSession_AnEndingWrittenByAnotherProcessIsThisStepSucceeding(t *testing.T) {
	// Callers inside one process are serialized by logMu, but a second daemon
	// concluding the same session is not — so an ending can appear between this
	// caller's probe and its append. What the step is FOR is that the session
	// has an ending, not that this caller wrote it: treating the log's refusal
	// as a failure would leave the conclusion retrying forever against a log
	// that is already complete.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-other-process")

	// The next tail read is followed by somebody else's ending landing.
	f.log.onceAfterRange(func() {
		if err := f.log.Append(ctx, &SessionEvent{
			SessionID: "s-other-process", TenantID: f.tenant, Sequence: 1,
			Kind:   KindSessionClosed,
			Closed: &SessionClosedPayload{Reason: ConcludedByProcessExit.String()},
		}); err != nil {
			t.Errorf("the other process's ending could not be written: %v", err)
		}
	})

	if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-other-process"}); err != nil {
		t.Fatalf("CloseSession = %v, want success; the session has an ending", err)
	}
	if f.log.closed != 1 {
		t.Errorf("terminal events = %d, want exactly 1 — the other process wrote it", f.log.closed)
	}
	if reason, _ := f.log.terminalReason("s-other-process"); reason != ConcludedByProcessExit.String() {
		t.Errorf("terminal reason = %q, want the ending that actually landed", reason)
	}

	// The conclusion is COMPLETE: persisted and evicted, not retrying forever.
	if _, live := f.impl.sessions.Load("s-other-process"); live {
		t.Error("the live record was retained; the conclusion did not complete over an already-ended log")
	}
	record, err := f.store.GetSession(ctx, f.tenant, "s-other-process")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status != SessionStateClosed {
		t.Errorf("store record = %q, want closed", record.Status)
	}
}

func TestConcludeSession_AMetadataReadFailureIsNotAnUnregisteredSession(t *testing.T) {
	// "No metadata" and "the store could not answer" were the same branch, so a
	// transient read failure was read as "this session was never registered,
	// there is nothing to end" — and the conclusion was then marked persisted
	// over a log that was never closed, with the error naming the real cause
	// discarded on the way past.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-meta-read-fault")
	f.log.failGetMeta(errors.New("log store refused the metadata read"))

	_, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-meta-read-fault"})
	if err == nil {
		t.Fatal("CloseSession reported success while the log metadata could not be read")
	}
	// ATTRIBUTION: the error names the read that failed, not a downstream effect.
	if !strings.Contains(err.Error(), "log metadata could not be read") {
		t.Errorf("error = %q, want it to name the failed metadata read", err.Error())
	}

	mem, live := f.impl.sessions.Load("s-meta-read-fault")
	if !live {
		t.Fatal("the live arbitration record was evicted; the log transition can never be retried")
	}
	if mem.(*sessionRecord).persisted.Load() {
		t.Error("the conclusion was marked persisted while its log metadata could not be read")
	}
	if f.log.closed != 0 {
		t.Errorf("terminal events = %d, want 0", f.log.closed)
	}

	// THE RETRY COMPLETES IT once the store answers again.
	f.log.failGetMeta(nil)
	if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-meta-read-fault"}); err != nil {
		t.Fatalf("the retry after a failed metadata read was refused: %v", err)
	}
	if f.log.closed != 1 {
		t.Errorf("terminal events after the retry = %d, want exactly 1", f.log.closed)
	}
	if got := f.backend.closes.Load(); got != 1 {
		t.Errorf("backend teardowns = %d, want exactly 1", got)
	}
}

func TestConcludeSession_AnUnregisteredSessionEndsWithoutALogTransition(t *testing.T) {
	// The other half of the same branch: a session the log never registered has
	// no ending to write, and that is an ANSWER — the conclusion completes.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-unregistered")
	f.log.forget("s-unregistered")

	if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-unregistered"}); err != nil {
		t.Fatalf("CloseSession = %v, want success; an unregistered log has nothing to end", err)
	}
	if f.log.closed != 0 {
		t.Errorf("terminal events = %d, want 0; there was no session in the log to terminate", f.log.closed)
	}
	record, err := f.store.GetSession(ctx, f.tenant, "s-unregistered")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status != SessionStateClosed {
		t.Errorf("store record = %q, want closed", record.Status)
	}
}

func TestConcludeSession_ConcurrentRetriesWriteOneEndingOnly(t *testing.T) {
	// Two conclusion callers that both read ACTIVE metadata would both append a
	// terminal event, so the read-decide-write is one critical section per
	// session. This drives several retries at once against a session whose log
	// close is repaired mid-flight.
	ctx := context.Background()
	f := newConclusionFixture(t, "s-log-retry-race")
	f.log.failPutMeta(errors.New("log store refused the metadata write"))
	if _, err := f.svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-log-retry-race"}); err == nil {
		t.Fatal("precondition: the first close should have failed")
	}
	f.log.failPutMeta(nil)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.impl.RecordSessionExit(ctx, f.tenant, "s-log-retry-race", "agent process exited")
		}()
	}
	wg.Wait()

	if f.log.closed != 1 {
		t.Errorf("terminal events = %d, want exactly 1 across concurrent retries", f.log.closed)
	}
	if got := f.backend.closes.Load(); got != 1 {
		t.Errorf("backend teardowns = %d, want exactly 1", got)
	}
	// The winner's reason stands, not a retrying observer's.
	if reason, ok := f.log.terminalReason("s-log-retry-race"); !ok || reason != string(ConcludedByOperator) {
		t.Errorf("terminal reason = %q (present=%v), want %q", reason, ok, ConcludedByOperator)
	}
}
