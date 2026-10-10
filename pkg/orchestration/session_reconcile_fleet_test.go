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

// session_reconcile_fleet_test.go holds the FLEET property of session
// reconciliation: the maintenance sweep runs on every daemon, not on an
// elected one (pkg/server/lifecycle_orchestration.go starts it unconditionally,
// exactly as it starts the job reaper), so two daemons WILL reach the same
// stale record and both will find it open.
//
// The guarantee is not that they cannot both try. It is that the durable
// artifacts of a conclusion are written once no matter how many try, because
// each one is arbitrated where it lives:
//
//   - the terminal log event, by the log's own sequence CAS and terminal gate
//     (AppendNextSessionEvent: either the gate sees the conclusion or the
//     sequence collides with it);
//   - the job audit line, by recordSessionConclusionOnJob's own check for an
//     ending already on the trail;
//   - the record's status AND its attribution, by ConcludeSession committing
//     the transition with a compare-and-set on the store's own plane.
//
// The attribution was the last of the three to become real. It used to be a
// plain Put behind a process-local mutex, which arbitrates nothing between
// daemons: two that both read an open record before either wrote both wrote,
// and the later reason stood -- so an operator's deliberate close could come
// back attributed to the sweep that raced it. The backing store has a guarded
// swap (go-qrdb's KVStore.CompareAndSwap, reached through the tenant-routed
// store), so the read that decides and the write that commits are now one
// step. See TestConcludeSession_TheFirstAttributionSurvivesASecondDaemon.
//
// These tests force the interleaving rather than hoping for it: daemon A is
// parked immediately after it reads the open record, daemon B is run to
// completion, and only then is A released to finish against state that moved
// underneath it. That is the worst case -- A's read is maximally stale and it
// has already passed every precondition -- and it is the one the property has
// to survive.

package orchestration

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// holdAfterSessionRead parks the first Get of one session record key until it
// is released, so a caller can be frozen holding a read that is about to go
// stale. Every other key passes straight through.
type holdAfterSessionRead struct {
	interfaces.KVStore

	recordKey string
	parked    chan struct{}
	release   chan struct{}
	once      sync.Once
}

func newHoldAfterSessionRead(inner interfaces.KVStore, recordKey string) *holdAfterSessionRead {
	return &holdAfterSessionRead{
		KVStore:   inner,
		recordKey: recordKey,
		parked:    make(chan struct{}),
		release:   make(chan struct{}),
	}
}

func (k *holdAfterSessionRead) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := k.KVStore.Get(ctx, key)
	if key == k.recordKey {
		k.once.Do(func() {
			close(k.parked)
			select {
			case <-k.release:
			case <-time.After(barrierWait):
			}
		})
	}
	return value, err
}

// holdOnNthSessionRead parks the Nth Get of one session record key. The count
// matters: a conclusion reads that key twice — once to check the record is
// still open, and again inside the store's compare-and-set — and the window
// this file needs to force sits BETWEEN them, after the log has already been
// written.
type holdOnNthSessionRead struct {
	interfaces.KVStore

	recordKey string
	nth       int32
	seen      atomic.Int32
	parked    chan struct{}
	release   chan struct{}
	once      sync.Once
}

func newHoldOnNthSessionRead(inner interfaces.KVStore, recordKey string, nth int32) *holdOnNthSessionRead {
	return &holdOnNthSessionRead{
		KVStore:   inner,
		recordKey: recordKey,
		nth:       nth,
		parked:    make(chan struct{}),
		release:   make(chan struct{}),
	}
}

func (k *holdOnNthSessionRead) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := k.KVStore.Get(ctx, key)
	if key == k.recordKey && k.seen.Add(1) == k.nth {
		k.once.Do(func() {
			close(k.parked)
			select {
			case <-k.release:
			case <-time.After(barrierWait):
			}
		})
	}
	return value, err
}

// CompareAndSwap forwards the guarded swap, so a daemon behind this wrapper
// takes the SAME commit path a fleet takes. Embedding an interface does not
// promote a method the interface does not declare, and without this the store
// would silently fall back to the mutex path and the test would prove nothing
// about arbitration.
func (k *holdOnNthSessionRead) CompareAndSwap(
	ctx context.Context, key string, expected, next []byte) (bool, error) {
	swapper, ok := k.KVStore.(guardedSwapper)
	if !ok {
		return false, errors.New("test wrapper: inner store cannot guard a swap")
	}
	return swapper.CompareAndSwap(ctx, key, expected, next)
}

// fleetDaemon is one daemon's view of shared cluster state: its own service
// and its own store object, over KV stores every daemon shares. The store
// objects are deliberately separate, because a process-local lock in one
// daemon is not a lock in another -- which is the whole point of the test.
type fleetDaemon struct {
	svc   *agentServiceImpl
	store AgentStore
}

// newFleetDaemon builds one daemon over the shared cluster KVs. sessionKV is
// taken per-daemon so a test can wrap one daemon's view without wrapping the
// other's.
func newFleetDaemon(t *testing.T, sessionKV, logKV, jobKV interfaces.KVStore) fleetDaemon {
	t.Helper()
	store := NewAgentStore(sessionKV)
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": &mockAgentBackend{backendType: "mock"}},
		&agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("NewAgentService returned %T, want *agentServiceImpl", svc)
	}
	impl.SetKVStore(jobKV)

	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: logKV})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	impl.SetSessionEventLog(log)

	return fleetDaemon{svc: impl, store: store}
}

// countSessionClosedEvents reports how many terminal events the shared log
// holds for one session.
func countSessionClosedEvents(t *testing.T, log SessionEventLog, tenantID, sessionID string) int {
	t.Helper()
	events, _, err := log.Range(context.Background(), tenantID, sessionID, 0, 1000)
	if err != nil {
		t.Fatalf("Range(%s): %v", sessionID, err)
	}
	var closed int
	for _, event := range events {
		if event.Kind == KindSessionClosed {
			closed++
		}
	}
	return closed
}

// countJobConclusionEvents reports how many session-concluded lines the job's
// audit trail holds.
func countJobConclusionEvents(t *testing.T, svc *agentServiceImpl, tenantID, jobID string) int {
	t.Helper()
	status, err := svc.loadJobStatus(tenantID, jobID)
	if err != nil {
		t.Fatalf("loadJobStatus(%s): %v", jobID, err)
	}
	var concluded int
	for _, event := range status.Events {
		if event.Type == JobAuditTypeSessionConcluded {
			concluded++
		}
	}
	return concluded
}

// TestConcludeAbsentSession_TwoDaemonsWriteOneEnding is the fleet race stated
// as a test.
//
// Daemon A reads the open record and is frozen there. Daemon B then concludes
// the session completely -- log closed and terminal event appended, job told,
// record closed. A is released and runs the whole conclusion holding a read
// that is now a lie: by its own reckoning the session is still open and it is
// entitled to end it.
//
// Every durable artifact must still be singular. A writes no second ending
// because the log refuses one, and no second audit line because the trail
// already carries it.
//
// THIS TEST COVERS THE SEQUENTIAL INTERLEAVING: B's conclusion is COMPLETE
// before A writes anything, so A's re-read finds a closed record and declines.
// The harder interleaving -- both daemons reading an open record before either
// commits -- is arbitrated by the store's compare-and-set and is covered
// separately by TestConcludeSession_TheFirstAttributionSurvivesASecondDaemon,
// which holds one writer between its read and its commit.
func TestConcludeAbsentSession_TwoDaemonsWriteOneEnding(t *testing.T) {
	const (
		tenantID  = "t1"
		sessionID = "s-contested"
		jobID     = "j-contested"
	)

	sharedSessions := kvstore.NewMemory()
	sharedLog := kvstore.NewMemory()
	sharedJobs := NewMemoryTaskKV()

	recordKey, err := sessionRecordKey(tenantID, sessionID)
	if err != nil {
		t.Fatalf("sessionRecordKey: %v", err)
	}
	held := newHoldAfterSessionRead(sharedSessions, recordKey)

	daemonA := newFleetDaemon(t, held, sharedLog, sharedJobs)
	daemonB := newFleetDaemon(t, sharedSessions, sharedLog, sharedJobs)

	seedSession(t, daemonB.store, &SessionRecord{
		SessionID: sessionID, TenantID: tenantID, JobID: jobID,
	})
	seedJob(t, daemonB.svc, tenantID, jobID, JobStateFailed, sessionID)

	// The log needs metadata before it will carry an ending: concludeLog reads
	// it first and treats its absence as "no session to terminate", which
	// would make this test pass without proving anything.
	if putErr := daemonB.svc.sessionLog.PutMeta(context.Background(), &SessionMeta{
		SessionID: sessionID,
		TenantID:  tenantID,
		Backend:   "mock",
		Status:    SessionStateActive,
	}); putErr != nil {
		t.Fatalf("PutMeta: %v", putErr)
	}

	type outcome struct {
		written bool
		err     error
	}
	resultA := make(chan outcome, 1)
	go func() {
		written, err := daemonA.svc.concludeAbsentSession(
			context.Background(), tenantID, tenantID, sessionID, ConcludedByFleetNodeGone)
		resultA <- outcome{written: written, err: err}
	}()

	// A is now holding a read of the open record and has not written anything.
	<-held.parked

	writtenB, errB := daemonB.svc.concludeAbsentSession(
		context.Background(), tenantID, tenantID, sessionID, ConcludedByJobConcluded)
	if errB != nil {
		t.Fatalf("daemon B conclusion: %v", errB)
	}
	if !writtenB {
		t.Fatal("daemon B reported it wrote nothing over a record it found open")
	}

	close(held.release)
	got := <-resultA
	if got.err != nil {
		t.Fatalf("daemon A conclusion: %v", got.err)
	}
	// THIS GUARDS THE TEST'S OWN POWER. A reached its commit point holding the
	// stale read, which is the only reason the assertions below mean anything:
	// if some later change made A bail out early instead, the singular-artifact
	// checks would still pass and would be proving nothing at all.
	if !got.written {
		t.Fatal("daemon A did not run the conclusion to its commit point, so the contested " +
			"interleaving this test exists to force was never actually exercised")
	}

	if closed := countSessionClosedEvents(t, daemonB.svc.sessionLog, tenantID, sessionID); closed != 1 {
		t.Errorf("terminal log events = %d, want 1; a session that ends twice in its own "+
			"transcript is the false liveness this sweep exists to remove, one level down", closed)
	}
	if audited := countJobConclusionEvents(t, daemonB.svc, tenantID, jobID); audited != 1 {
		t.Errorf("job audit conclusion lines = %d, want 1", audited)
	}

	state, reason := storedStatus(t, daemonB.store, tenantID, sessionID)
	if state != SessionStateClosed {
		t.Errorf("status = %q, want closed", state)
	}
	// B finished before A began writing, so A's re-read sees a closed record and
	// leaves the reason alone.
	if reason != ConcludedByJobConcluded {
		t.Errorf("reason = %q, want %q; a conclusion that arrives after a COMPLETED one "+
			"must not restate the cause", reason, ConcludedByJobConcluded)
	}
}

// TestConcludeAbsentSession_TwoDaemonsStartingTogetherWriteOneEnding is the
// same property without the freeze: both daemons are released from one barrier
// and race for real. It is the unforced counterpart of the test above, and it
// is the one that runs under -race.
func TestConcludeAbsentSession_TwoDaemonsStartingTogetherWriteOneEnding(t *testing.T) {
	const (
		tenantID  = "t1"
		sessionID = "s-raced"
		jobID     = "j-raced"
	)

	sharedSessions := kvstore.NewMemory()
	sharedLog := kvstore.NewMemory()
	sharedJobs := NewMemoryTaskKV()

	daemonA := newFleetDaemon(t, sharedSessions, sharedLog, sharedJobs)
	daemonB := newFleetDaemon(t, sharedSessions, sharedLog, sharedJobs)

	seedSession(t, daemonA.store, &SessionRecord{
		SessionID: sessionID, TenantID: tenantID, JobID: jobID,
	})
	seedJob(t, daemonA.svc, tenantID, jobID, JobStateFailed, sessionID)
	if putErr := daemonA.svc.sessionLog.PutMeta(context.Background(), &SessionMeta{
		SessionID: sessionID,
		TenantID:  tenantID,
		Backend:   "mock",
		Status:    SessionStateActive,
	}); putErr != nil {
		t.Fatalf("PutMeta: %v", putErr)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, daemon := range []fleetDaemon{daemonA, daemonB} {
		wg.Add(1)
		go func(d fleetDaemon) {
			defer wg.Done()
			<-start
			_, err := d.svc.concludeAbsentSession(
				context.Background(), tenantID, tenantID, sessionID, ConcludedByFleetNodeGone)
			errs <- err
		}(daemon)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent conclusion: %v", err)
		}
	}

	if closed := countSessionClosedEvents(t, daemonA.svc.sessionLog, tenantID, sessionID); closed != 1 {
		t.Errorf("terminal log events = %d, want 1", closed)
	}
	if audited := countJobConclusionEvents(t, daemonA.svc, tenantID, jobID); audited != 1 {
		t.Errorf("job audit conclusion lines = %d, want 1", audited)
	}
	state, reason := storedStatus(t, daemonA.store, tenantID, sessionID)
	if state != SessionStateClosed {
		t.Errorf("status = %q, want closed", state)
	}
	// Both daemons conclude under the SAME reason here, which is what a sweep
	// does -- both derive it from the same two facts. Whichever wins the swap,
	// the reason on the record is that one.
	if reason != ConcludedByFleetNodeGone {
		t.Errorf("reason = %q, want %q", reason, ConcludedByFleetNodeGone)
	}
}

// =============================================================================
// The attribution IS arbitrated, once the store can decide a swap
// =============================================================================

// swappableKV is an in-process store that also offers the guarded swap the
// tenant-routed qrdb store provides, so a test can drive the path a fleet
// actually takes. pkg/kvstore.Store deliberately has no such verb — it is
// in-process and therefore never contended by a second daemon — so the
// capability is supplied here rather than pretended at.
type swappableKV struct {
	interfaces.KVStore

	mu sync.Mutex
	// beforeSwap runs BEFORE the swap is attempted, letting a test interleave a
	// second writer exactly between another writer's read and its commit. It
	// fires once and is disarmed first, because the writer it runs reaches this
	// same method: re-entering would be a deadlock rather than a race.
	beforeSwap func(key string)
	armed      atomic.Bool
}

func (k *swappableKV) CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error) {
	if k.beforeSwap != nil && k.armed.CompareAndSwap(true, false) {
		k.beforeSwap(key)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	current, err := k.KVStore.Get(ctx, key)
	if err != nil {
		return false, err
	}
	if string(current) != string(expected) {
		return false, nil
	}
	if putErr := k.KVStore.Put(ctx, key, next); putErr != nil {
		return false, putErr
	}
	return true, nil
}

// TestConcludeSession_TheFirstAttributionSurvivesASecondDaemon is the
// attribution guarantee, arbitrated rather than asserted.
//
// Daemon A reads an open record and is held there. Daemon B concludes the
// session completely under its OWN reason. A then commits — and its
// compare-and-set is refused, because the bytes it read are no longer the
// bytes in the store. A re-reads, finds a complete ending, and writes nothing.
//
// Under the plain Put this replaced, A's write landed and an operator's
// deliberate close came back attributed to the sweep that raced it.
func TestConcludeSession_TheFirstAttributionSurvivesASecondDaemon(t *testing.T) {
	const (
		tenantID  = "t1"
		sessionID = "s-attributed"
	)
	shared := &swappableKV{KVStore: kvstore.NewMemory()}

	// Two store objects over one KV: separate process-local locks, exactly as
	// two daemons have.
	daemonA := NewAgentStore(shared)
	daemonB := NewAgentStore(shared)

	seedSession(t, daemonB, &SessionRecord{SessionID: sessionID, TenantID: tenantID})

	// B concludes in the window between A's read and A's commit.
	shared.armed.Store(true)
	shared.beforeSwap = func(string) {
		if err := daemonB.ConcludeSession(
			context.Background(), tenantID, sessionID, ConcludedByOperator); err != nil {
			t.Errorf("daemon B conclusion: %v", err)
		}
	}

	if err := daemonA.ConcludeSession(
		context.Background(), tenantID, sessionID, ConcludedByJobConcluded); err != nil {
		t.Fatalf("daemon A conclusion: %v; losing the swap is not a failure, it is the "+
			"arbitration working", err)
	}

	state, reason := storedStatus(t, daemonB, tenantID, sessionID)
	if state != SessionStateClosed {
		t.Errorf("status = %q, want closed", state)
	}
	if reason != ConcludedByOperator {
		t.Errorf("reason = %q, want %q; the FIRST ending owns the attribution, and a "+
			"deliberate operator close must not come back reported as a sweep", reason, ConcludedByOperator)
	}
}

// TestConcludeSession_AnUncontendedConclusionStillCommits is the converse: the
// swap must not turn the ordinary single-writer case into a refusal.
func TestConcludeSession_AnUncontendedConclusionStillCommits(t *testing.T) {
	shared := &swappableKV{KVStore: kvstore.NewMemory()}
	store := NewAgentStore(shared)
	seedSession(t, store, &SessionRecord{SessionID: "s-plain", TenantID: "t1"})

	if err := store.ConcludeSession(
		context.Background(), "t1", "s-plain", ConcludedByFleetNodeGone); err != nil {
		t.Fatalf("ConcludeSession: %v", err)
	}
	state, reason := storedStatus(t, store, "t1", "s-plain")
	if state != SessionStateClosed || reason != ConcludedByFleetNodeGone {
		t.Errorf("state/reason = %q/%q, want closed/%q", state, reason, ConcludedByFleetNodeGone)
	}

	// And a second call over a complete ending is a no-op, not a rewrite.
	if err := store.ConcludeSession(
		context.Background(), "t1", "s-plain", ConcludedByOperator); err != nil {
		t.Fatalf("second ConcludeSession: %v", err)
	}
	if _, reason := storedStatus(t, store, "t1", "s-plain"); reason != ConcludedByFleetNodeGone {
		t.Errorf("reason = %q, want %q kept", reason, ConcludedByFleetNodeGone)
	}
}

// sessionClosedReason reads the attribution off the log's terminal event.
func sessionClosedReason(t *testing.T, log SessionEventLog, tenantID, sessionID string) string {
	t.Helper()
	events, _, err := log.Range(context.Background(), tenantID, sessionID, 0, 1000)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	for _, event := range events {
		if event.Kind == KindSessionClosed && event.Closed != nil {
			return event.Closed.Reason
		}
	}
	return ""
}

// TestConcludeAbsentSession_OneAttributionAcrossAllThreeArtifacts is the
// finding that each artifact being arbitrated is not the same as them agreeing.
//
// The terminal log event, the job audit line and the record's reason each have
// their own arbitration, and they were INDEPENDENT — so two daemons concluding
// under different reasons could win different ones. A sweep could take the log
// while an operator's close took the record's compare-and-set, and an auditor
// would find the transcript naming one cause and the tombstone another.
// Contradictory durable evidence is worse than either answer alone.
//
// The log's append is the arbitration every daemon already shares, so it is
// made the single one: the job audit and the record are written under the
// reason that actually landed there. Daemon A is frozen holding an open read;
// B concludes completely as `operator`; A is released and runs its whole
// conclusion proposing a different reason, and must adopt what it finds.
func TestConcludeAbsentSession_OneAttributionAcrossAllThreeArtifacts(t *testing.T) {
	const (
		tenantID  = "t1"
		sessionID = "s-one-reason"
		jobID     = "j-one-reason"
	)

	// The shared store can decide a guarded swap, which is what a tenant-routed
	// qrdb store does and what makes this the commit path a real fleet takes.
	sharedSessions := &swappableKV{KVStore: kvstore.NewMemory()}
	sharedLog := kvstore.NewMemory()
	sharedJobs := NewMemoryTaskKV()

	recordKey, err := sessionRecordKey(tenantID, sessionID)
	if err != nil {
		t.Fatalf("sessionRecordKey: %v", err)
	}
	// Parked on the SECOND read of the record: daemon A has by then written the
	// log's terminal event under its own reason, and has not yet reached the
	// compare-and-set that commits the record. That is the only window in which
	// two different artifacts can be won by two different daemons.
	held := newHoldOnNthSessionRead(sharedSessions, recordKey, 2)

	daemonA := newFleetDaemon(t, held, sharedLog, sharedJobs)
	daemonB := newFleetDaemon(t, sharedSessions, sharedLog, sharedJobs)

	seedSession(t, daemonB.store, &SessionRecord{
		SessionID: sessionID, TenantID: tenantID, JobID: jobID,
	})
	seedJob(t, daemonB.svc, tenantID, jobID, JobStateFailed, sessionID)
	if putErr := daemonB.svc.sessionLog.PutMeta(context.Background(), &SessionMeta{
		SessionID: sessionID, TenantID: tenantID, Backend: "mock", Status: SessionStateActive,
	}); putErr != nil {
		t.Fatalf("PutMeta: %v", putErr)
	}

	done := make(chan error, 1)
	go func() {
		_, concludeErr := daemonA.svc.concludeAbsentSession(
			context.Background(), tenantID, tenantID, sessionID, ConcludedByFleetNodeGone)
		done <- concludeErr
	}()

	// A owns the log now, under agent_gone.
	<-held.parked
	if logged := sessionClosedReason(t, daemonA.svc.sessionLog, tenantID, sessionID); logged != ConcludedByFleetNodeGone.String() {
		t.Fatalf("the log was not written before the park: reason = %q; this test is not "+
			"exercising the window it exists for", logged)
	}

	// B now concludes completely, proposing a DIFFERENT reason. Its own log
	// append is refused — A already ended the session — so the reason it
	// carries to the record must be A's, not the one it proposed.
	if _, errB := daemonB.svc.concludeAbsentSession(
		context.Background(), tenantID, tenantID, sessionID, ConcludedByOperator); errB != nil {
		t.Fatalf("daemon B conclusion: %v", errB)
	}

	close(held.release)
	if errA := <-done; errA != nil {
		t.Fatalf("daemon A conclusion: %v", errA)
	}

	// All three must name the SAME cause, and it must be the one the log
	// arbitrated — A's, because A won the append.
	logReason := sessionClosedReason(t, daemonB.svc.sessionLog, tenantID, sessionID)
	if logReason != ConcludedByFleetNodeGone.String() {
		t.Errorf("log terminal event reason = %q, want %q", logReason, ConcludedByFleetNodeGone)
	}

	_, recordReason := storedStatus(t, daemonB.store, tenantID, sessionID)
	if recordReason != ConcludedByFleetNodeGone {
		t.Errorf("record reason = %q, want %q; the tombstone disagreed with the transcript, "+
			"which is worse for an auditor than either answer alone", recordReason, ConcludedByFleetNodeGone)
	}

	status, jobErr := daemonB.svc.loadJobStatus(tenantID, jobID)
	if jobErr != nil {
		t.Fatalf("loadJobStatus: %v", jobErr)
	}
	var audited int
	for _, event := range status.Events {
		if event.Type == JobAuditTypeSessionConcluded {
			audited++
			if !strings.Contains(event.Details, ConcludedByFleetNodeGone.String()) {
				t.Errorf("job audit line = %q, want it to name %q", event.Details, ConcludedByFleetNodeGone)
			}
		}
	}
	if audited != 1 {
		t.Errorf("job audit conclusion lines = %d, want 1", audited)
	}
}
