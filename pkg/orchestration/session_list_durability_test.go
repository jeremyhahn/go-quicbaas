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

// session_list_durability_test.go holds one property: the session listing may
// not answer from the live map alone once the durable half is authoritative.
//
// Reconciliation writes the terminal records, so closed, error and resumable
// sessions exist ONLY in the store. A listing that logs a store failure and
// carries on therefore reports "there are no closed sessions" when the truth
// is "I could not look" — and an empty 200 is the one answer a caller cannot
// tell apart from a real one. These tests hold the reads the listing depends
// on to the same fail-closed rule jobTerminalState and concludeLog already
// follow.

package orchestration

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// errSessionStoreDown is the failure a store reports when it cannot answer.
var errSessionStoreDown = errors.New("session store: unavailable for test")

// unlistableSessionStore refuses to list durable records. Everything else is the
// real store, so a test still writes and reads records normally.
type unlistableSessionStore struct {
	AgentStore
}

func (s *unlistableSessionStore) ListSessions(context.Context, string) ([]*SessionRecord, error) {
	return nil, errSessionStoreDown
}

func (s *unlistableSessionStore) ListAllSessions(context.Context) ([]*SessionRecord, error) {
	return nil, errSessionStoreDown
}

// getFailingStore refuses to read one session's record, reporting err. It is
// how the enrichment read inside the live walk is driven.
type getFailingStore struct {
	AgentStore

	sessionID string
	err       error
}

func (s *getFailingStore) GetSession(ctx context.Context, tenantID, sessionID string) (*SessionRecord, error) {
	if sessionID == s.sessionID {
		return nil, s.err
	}
	return s.AgentStore.GetSession(ctx, tenantID, sessionID)
}

// TestListSessions_AStoreThatCannotListIsNotAnEmptyList is the headline case.
//
// `status=closed` reads entirely from the durable half. Answering it with an
// empty success when the store could not be scanned tells an operator the
// sessions they are looking for do not exist — the mirror image of the false
// liveness this PR removes, and just as wrong.
func TestListSessions_AStoreThatCannotListIsNotAnEmptyList(t *testing.T) {
	svc, store := reconcileHarness(t)
	svc.store = &unlistableSessionStore{AgentStore: store}

	for _, status := range []SessionState{"", SessionStateClosed, SessionStateActive} {
		t.Run("status="+string(status), func(t *testing.T) {
			resp, err := svc.ListSessions(context.Background(), &ListSessionsRequest{Status: status})
			if err == nil {
				t.Fatalf("a store that could not be listed produced a successful response with %d sessions; "+
					"an empty success is indistinguishable from a real empty list", len(resp.Sessions))
			}
			if !errors.Is(err, ErrAgentServiceUnavailable) {
				t.Errorf("error = %v, want it to carry ErrAgentServiceUnavailable so the caller can "+
					"tell a broken store from a bad request", err)
			}
		})
	}
}

// TestListSessions_AnUnreadableRecordIsNotALiveSession drives the other read
// the listing depends on.
//
// The live map's ACTIVE is an assumption; the durable record is an
// observation, and it is what overrides that assumption when it says CLOSED.
// Discarding the read that carries it reports the assumption as fact — a
// concluded session listed as live.
func TestListSessions_AnUnreadableRecordIsNotALiveSession(t *testing.T) {
	svc, store := reconcileHarness(t)
	seedSession(t, store, &SessionRecord{SessionID: "s-live", TenantID: "t1"})
	svc.sessions.Store("s-live", &sessionRecord{Backend: "mock", TenantID: "t1"})
	svc.store = &getFailingStore{AgentStore: store, sessionID: "s-live", err: errSessionStoreDown}

	_, err := svc.ListSessions(context.Background(), &ListSessionsRequest{})
	if err == nil {
		t.Fatal("a record that could not be read was reported as a live session")
	}
	if !errors.Is(err, ErrAgentServiceUnavailable) {
		t.Errorf("error = %v, want it to carry ErrAgentServiceUnavailable", err)
	}
}

// TestListSessions_ALiveSessionWithNoRecordYetIsStillListed is the converse,
// and it is why the failure above is matched on the error rather than on any
// failed read.
//
// A live session whose durable record has not been written yet is ordinary —
// the record is written at spawn and may be mid-flight — so a NOT FOUND must
// leave the live assumption standing rather than failing the whole listing.
func TestListSessions_ALiveSessionWithNoRecordYetIsStillListed(t *testing.T) {
	svc, store := reconcileHarness(t)
	svc.sessions.Store("s-fresh", &sessionRecord{Backend: "mock", TenantID: "t1"})
	svc.store = &getFailingStore{AgentStore: store, sessionID: "s-fresh", err: &AgentStoreError{
		Sentinel: ErrAgentStoreNotFound,
		Message:  "no record yet",
	}}

	resp, err := svc.ListSessions(context.Background(), &ListSessionsRequest{})
	if err != nil {
		t.Fatalf("a live session with no durable record yet failed the listing: %v", err)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(resp.Sessions))
	}
	if resp.Sessions[0].State != SessionStateActive {
		t.Errorf("state = %q, want active", resp.Sessions[0].State)
	}
}

// =============================================================================
// Spawn ordering: the log is registered before the session becomes arbitrable
// =============================================================================

// spawnOrderingLog records what the live map held at the moment the session's
// metadata was written, and can refuse the write on demand.
type spawnOrderingLog struct {
	SessionEventLog

	sessions *sync.Map
	// liveAtPutMeta is what the live map said about this session when its
	// metadata was registered. False is the requirement.
	liveAtPutMeta bool
	putMetaCalled bool
	failPutMeta   error
}

func (l *spawnOrderingLog) PutMeta(ctx context.Context, meta *SessionMeta) error {
	l.putMetaCalled = true
	_, l.liveAtPutMeta = l.sessions.Load(meta.SessionID)
	if l.failPutMeta != nil {
		return l.failPutMeta
	}
	return l.SessionEventLog.PutMeta(ctx, meta)
}

// TestSpawnSession_RegistersTheLogBeforeTheSessionIsArbitrable closes the
// half-conclusion window.
//
// Publishing the live record first made the session arbitrable by an exit
// observer while its log had no metadata — and concludeLog reads missing
// metadata as "no session to terminate" and reports SUCCESS. A process that
// died in that window was concluded against a log that had never heard of it:
// the durable record went CLOSED and the live entry was evicted, and then
// spawn wrote ACTIVE metadata for a session that had already ended. The
// operator is left with a transcript that says open, contains no ending, and
// sits behind a record that says closed.
func TestSpawnSession_RegistersTheLogBeforeTheSessionIsArbitrable(t *testing.T) {
	svc, _ := reconcileHarness(t)
	log := &spawnOrderingLog{SessionEventLog: NewMemorySessionEventLog(), sessions: &svc.sessions}
	svc.SetSessionEventLog(log)

	resp, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{Backend: "mock"})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	if !log.putMetaCalled {
		t.Fatal("the session's log metadata was never registered")
	}
	if log.liveAtPutMeta {
		t.Error("the session was published to the live map before its log metadata existed; " +
			"an exit observer arriving in that window concludes it against a log it cannot close")
	}
	if _, live := svc.sessions.Load(resp.SessionID); !live {
		t.Error("the session was never published to the live map at all")
	}
}

// TestSpawnSession_ALogThatRefusesRegistrationFailsTheSpawn is what makes the
// ordering above worth anything.
//
// A swallowed registration failure publishes an arbitrable session over a log
// that cannot be closed, which is the same half-conclusion by another route.
// The spawn fails instead, and — the part that matters — the live record is
// never published, so nothing can conclude it against the absent log. The
// durable record left behind is ACTIVE with no live entry, which is a shape
// the reconciliation sweep already ends.
func TestSpawnSession_ALogThatRefusesRegistrationFailsTheSpawn(t *testing.T) {
	svc, _ := reconcileHarness(t)
	log := &spawnOrderingLog{
		SessionEventLog: NewMemorySessionEventLog(),
		sessions:        &svc.sessions,
		failPutMeta:     errSessionStoreDown,
	}
	svc.SetSessionEventLog(log)

	_, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{Backend: "mock"})
	if err == nil {
		t.Fatal("a session was spawned over a log that refused to register it")
	}
	if !errors.Is(err, ErrAgentServiceUnavailable) {
		t.Errorf("error = %v, want it to carry ErrAgentServiceUnavailable", err)
	}

	var live int
	svc.sessions.Range(func(any, any) bool { live++; return true })
	if live != 0 {
		t.Errorf("live sessions = %d, want 0; a session published over an unregistered log "+
			"is exactly the state the failure exists to avoid", live)
	}
}

// =============================================================================
// Spawn unwind: a failed spawn does not leave its process running
// =============================================================================

// closeRecordingBackend records the sessions it was asked to close, so a test
// can prove a failed spawn released the process it had already started.
type closeRecordingBackend struct {
	*mockAgentBackend

	mu     sync.Mutex
	closed []string
}

func (b *closeRecordingBackend) Close(ctx context.Context, sessionID string) error {
	b.mu.Lock()
	b.closed = append(b.closed, sessionID)
	b.mu.Unlock()
	return b.mockAgentBackend.Close(ctx, sessionID)
}

func (b *closeRecordingBackend) closedSessions() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.closed...)
}

// putFailingStore refuses to persist new records. The rest is the real store.
type putFailingStore struct {
	AgentStore
}

func (s *putFailingStore) PutSession(context.Context, *SessionRecord) error {
	return errSessionStoreDown
}

// newUnwindHarness builds a service whose only backend records its closes.
func newUnwindHarness(t *testing.T) (*agentServiceImpl, *closeRecordingBackend, AgentStore) {
	t.Helper()
	backend := &closeRecordingBackend{mockAgentBackend: &mockAgentBackend{backendType: "mock"}}
	store := NewInMemoryAgentStore()
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("NewAgentService returned %T, want *agentServiceImpl", svc)
	}
	impl.SetKVStore(kvstore.NewMemory())
	return impl, backend, store
}

// TestSpawnSession_AFailedRecordWriteReleasesTheProcess closes the leak on the
// durable-write path.
//
// backend.Spawn has already started a real agent process by the time the
// record is written. Returning the store's failure without releasing it leaks
// the process past every reader: the caller was told the spawn failed so it
// will not close the session, there is no live entry so nothing in this
// process holds it, and reconciliation reads a record with no live entry as a
// session with no process — so it would never ask the backend to close either.
func TestSpawnSession_AFailedRecordWriteReleasesTheProcess(t *testing.T) {
	svc, backend, store := newUnwindHarness(t)
	svc.store = &putFailingStore{AgentStore: store}

	_, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{Backend: "mock"})
	if err == nil {
		t.Fatal("a spawn succeeded over a store that refused to persist it")
	}
	if closed := backend.closedSessions(); len(closed) != 1 {
		t.Fatalf("backend closes = %v, want exactly one; the agent process a failed spawn "+
			"started is unreachable by anything else once the spawn returns", closed)
	}
}

// TestSpawnSession_AFailedLogRegistrationUnwindsInFull is the same property on
// the path that registers the event log, which runs after the record exists.
// Both side effects must be released: the process, and the record that names
// it — a record left ACTIVE would advertise a session nothing is running.
func TestSpawnSession_AFailedLogRegistrationUnwindsInFull(t *testing.T) {
	svc, backend, store := newUnwindHarness(t)
	svc.SetSessionEventLog(&spawnOrderingLog{
		SessionEventLog: NewMemorySessionEventLog(),
		sessions:        &svc.sessions,
		failPutMeta:     errSessionStoreDown,
	})

	_, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{Backend: "mock"})
	if err == nil {
		t.Fatal("a spawn succeeded over a log that refused to register it")
	}
	if closed := backend.closedSessions(); len(closed) != 1 {
		t.Fatalf("backend closes = %v, want exactly one", closed)
	}

	records, listErr := store.ListAllSessions(context.Background())
	if listErr != nil {
		t.Fatalf("ListAllSessions: %v", listErr)
	}
	if len(records) != 0 {
		t.Errorf("durable records = %d, want 0; a record left behind by a failed spawn "+
			"advertises a session that is not running", len(records))
	}
}

// TestSpawnSession_ASuccessfulSpawnClosesNothing is the converse: the unwind
// must fire only on the failure paths. A spawn that works has to leave its
// process alone.
func TestSpawnSession_ASuccessfulSpawnClosesNothing(t *testing.T) {
	svc, backend, _ := newUnwindHarness(t)

	if _, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{Backend: "mock"}); err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	if closed := backend.closedSessions(); len(closed) != 0 {
		t.Errorf("backend closes = %v, want none; the unwind fired on a successful spawn", closed)
	}
}

// =============================================================================
// A recorded resume failure survives the next boot
// =============================================================================

// TestLoadPersistedSessions_AnErroredSessionIsNotSilentlyResumable is the
// operator-visibility half of reconciliation.
//
// recordResumeFailure writes SessionStateError and the ResumeError that
// explains it. Resumable() excluded only CLOSED, so on the next boot the
// restore rewrote that record to RESUMABLE: the failure vanished from the
// record and the same broken session was offered for resume again, looping on
// the identical error with nothing left to say why.
func TestLoadPersistedSessions_AnErroredSessionIsNotSilentlyResumable(t *testing.T) {
	svc, store := reconcileHarness(t)
	seedSession(t, store, &SessionRecord{
		SessionID:    "s-errored",
		TenantID:     "t1",
		Status:       SessionStateError,
		ResumeError:  "transcript could not be read",
		Persistent:   true,
		BackendLocal: true,
	})

	if err := svc.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}

	record, err := store.GetSession(context.Background(), "t1", "s-errored")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status != SessionStateError {
		t.Errorf("status = %q, want error; the boot rewrote a recorded failure", record.Status)
	}
	if record.ResumeError == "" {
		t.Error("the resume failure lost the cause that explains it")
	}
	if mem, live := svc.liveSessionRecord("s-errored"); live && mem.resumable.Load() {
		t.Error("a session that failed to resume was offered for resume again")
	}
}

// TestListSessions_AnErroredSessionIsNotListedActive is the other half. With
// the record no longer rewritten to RESUMABLE, the listing has to honour the
// ERROR the daemon observed rather than the ACTIVE its live map assumes —
// otherwise the fix above simply moves the lie from "resumable" to "active".
func TestListSessions_AnErroredSessionIsNotListedActive(t *testing.T) {
	svc, store := reconcileHarness(t)
	seedSession(t, store, &SessionRecord{
		SessionID:    "s-errored",
		TenantID:     "t1",
		Status:       SessionStateError,
		ResumeError:  "transcript could not be read",
		Persistent:   true,
		BackendLocal: true,
	})
	if err := svc.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}

	resp, err := svc.ListSessions(context.Background(), &ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(resp.Sessions))
	}
	if resp.Sessions[0].State != SessionStateError {
		t.Errorf("state = %q, want error; an operator retrying a session the daemon "+
			"already knows is broken is the worse half of false liveness", resp.Sessions[0].State)
	}

	// And the filter must be able to find it, which is the point of reporting
	// the state the daemon observed.
	filtered, err := svc.ListSessions(context.Background(), &ListSessionsRequest{Status: SessionStateError})
	if err != nil {
		t.Fatalf("ListSessions(status=error): %v", err)
	}
	if len(filtered.Sessions) != 1 {
		t.Errorf("status=error returned %d sessions, want 1", len(filtered.Sessions))
	}
}

// =============================================================================
// The liveness probe asks about the pinned daemon, not about the backend
// =============================================================================

// TestRemoteSessionGone_ProbesThePinnedDaemonNotJustTheBackend is the fleet
// misread.
//
// A remote session pinned to daemon A is probed through the resolver. Given no
// affinity, the resolver answers for whatever daemon serves that backend — so
// in a fleet where B serves it too, a session pinned to a DEAD A came back
// "still reachable". The record was restored as live, and every later prompt,
// status and close re-pinned to the stored host and failed against a daemon
// that is gone.
func TestRemoteSessionGone_ProbesThePinnedDaemonNotJustTheBackend(t *testing.T) {
	svc, _ := reconcileHarness(t)

	var sawAffinity string
	var sawStrict bool
	// The resolver stands for a fleet in which SOME daemon serves "mock", but
	// the one this session is pinned to does not answer.
	svc.SetRemoteBackendResolver(func(ctx context.Context, backend, _ string) (AgentBackend, bool) {
		sawAffinity = HostAffinityFromContext(ctx)
		sawStrict = HostAffinityStrictFromContext(ctx)
		if sawAffinity == "daemon-a" {
			return nil, false
		}
		return &mockAgentBackend{backendType: backend}, true
	})

	// An empty registry is the positive evidence the fall-through needs: the
	// pinned daemon is not registered at all. Without a registry the sweep
	// correctly declines to decide, which would make the assertion below pass
	// for the wrong reason.
	svc.SetFleetRegistry(NewFleetRegistry(kvstore.NewMemory()))

	record := &SessionRecord{
		SessionID:    "s-pinned",
		TenantID:     "t1",
		Backend:      "mock",
		HostAffinity: "daemon-a",
		BackendLocal: false,
	}

	_, gone := svc.remoteSessionGone(context.Background(), record)

	if sawAffinity != "daemon-a" {
		t.Errorf("resolver saw affinity %q, want daemon-a; without it the probe asks "+
			"whether ANY daemon serves the backend, which is not the question", sawAffinity)
	}
	if !sawStrict {
		t.Error("the affinity was passed as a soft hint; a resolver free to fall back to " +
			"another daemon manufactures the wrong answer for a pinned session")
	}
	// The registry holds no agent for this session, so the fall-through has its
	// positive evidence and the session is correctly reported gone.
	if !gone {
		t.Error("a session pinned to an unresolvable daemon, absent from the registry, " +
			"was reported still reachable")
	}
}

// TestRemoteSessionGone_APinnedDaemonThatAnswersIsAlive is the converse: the
// strict probe must not turn a reachable pinned daemon into a dead one.
func TestRemoteSessionGone_APinnedDaemonThatAnswersIsAlive(t *testing.T) {
	svc, _ := reconcileHarness(t)
	svc.SetRemoteBackendResolver(func(ctx context.Context, backend, _ string) (AgentBackend, bool) {
		if HostAffinityFromContext(ctx) != "daemon-a" {
			return nil, false
		}
		return &mockAgentBackend{backendType: backend}, true
	})

	_, gone := svc.remoteSessionGone(context.Background(), &SessionRecord{
		SessionID:    "s-pinned",
		TenantID:     "t1",
		Backend:      "mock",
		HostAffinity: "daemon-a",
	})
	if gone {
		t.Error("a session whose pinned daemon answered the probe was reported gone")
	}
}

// =============================================================================
// A conclusion someone else is running is not a conclusion that failed
// =============================================================================

// TestReconcileSpawnedSession_AnOwnedConclusionIsNotUnwound is the tombstone
// loss.
//
// The spawn's reconcile nudges the conclusion, and a nudge that loses
// arbitration returns immediately rather than waiting for the winner's
// teardown. Reading the store at that instant finds ACTIVE — so the
// postcondition read it as "could not be closed", and SpawnSession unwound by
// deleting the live entry AND the durable record. The winner then reached
// finishDurable, found no record, and correctly treated the absence as
// vacuously recorded. The session was not listed closed; it was not listed at
// all.
func TestReconcileSpawnedSession_AnOwnedConclusionIsNotUnwound(t *testing.T) {
	svc, store := reconcileHarness(t)
	const sessionID = "s-inflight"

	seedSession(t, store, &SessionRecord{SessionID: sessionID, TenantID: "t1"})

	// A live record whose conclusion is OWNED and still tearing down: the swap
	// is taken and the attempt has not announced its end, which is exactly the
	// state the exit observer holds for the length of backend.Close.
	mem := &sessionRecord{Backend: "mock", TenantID: "t1"}
	mem.concluded.Store(true)
	mem.conclusion.Store(&sessionConclusion{
		reason: ConcludedByProcessExit,
		done:   make(chan struct{}),
	})
	svc.sessions.Store(sessionID, mem)

	// The backend reports the process is gone, which is what sends reconcile
	// down the conclusion path at all.
	backend := &mockAgentBackend{
		backendType: "mock",
		statusFn: func(context.Context, string) (*SessionStatus, error) {
			return &SessionStatus{SessionID: sessionID, State: SessionStateClosed}, nil
		},
	}

	reconciled, err := svc.reconcileSpawnedSessionState(
		context.Background(), backend, "t1", "t1", sessionID)
	if !reconciled {
		t.Fatal("reconcile did not act on a session the backend reported closed")
	}
	if err != nil {
		t.Fatalf("a conclusion owned by another caller was reported as a failure to close: %v", err)
	}

	// The record must still be there for the winner to close. Deleting it is
	// what made the session vanish.
	if _, getErr := store.GetSession(context.Background(), "t1", sessionID); getErr != nil {
		t.Errorf("the durable record was removed while its conclusion was still running: %v", getErr)
	}
}

// TestReconcileSpawnedSession_NoOwnerAndStillOpenIsAFailure is the converse,
// and it is what stops the fix above from turning every unclosed record into a
// success. With nobody owning the conclusion there is no winner coming, so an
// ACTIVE record after a nudge really is a conclusion that could not be made.
func TestReconcileSpawnedSession_NoOwnerAndStillOpenIsAFailure(t *testing.T) {
	svc, store := reconcileHarness(t)
	const sessionID = "s-unowned"

	seedSession(t, store, &SessionRecord{SessionID: sessionID, TenantID: "t1"})
	// No live entry at all: concludeSession cannot arbitrate a session it does
	// not hold, so it does nothing and nobody owns the ending.
	backend := &mockAgentBackend{
		backendType: "mock",
		statusFn: func(context.Context, string) (*SessionStatus, error) {
			return &SessionStatus{SessionID: sessionID, State: SessionStateClosed}, nil
		},
	}

	reconciled, err := svc.reconcileSpawnedSessionState(
		context.Background(), backend, "t1", "t1", sessionID)
	if !reconciled {
		t.Fatal("reconcile did not act on a session the backend reported closed")
	}
	if err == nil {
		t.Fatal("a record left ACTIVE with no conclusion running was reported as reconciled")
	}
	if !errors.Is(err, ErrAgentServiceUnavailable) {
		t.Errorf("error = %v, want it to carry ErrAgentServiceUnavailable", err)
	}
}

// TestLoadPersistedSessions_AnErroredSessionIsNotPutBackInTheLiveMap is the
// other half of leaving a recorded resume failure alone.
//
// Excluding ERROR from Resumable() stopped the record being rewritten, but the
// restore still stored a live entry for it — and a live entry that is not
// resumable sends SessionStatus and PromptSession past the resumable
// short-circuit and on to a backend that holds no process for the session. The
// operator got "session not found" instead of the failure the daemon recorded.
// With no live entry both paths read the durable record.
func TestLoadPersistedSessions_AnErroredSessionIsNotPutBackInTheLiveMap(t *testing.T) {
	svc, store := reconcileHarness(t)
	seedSession(t, store, &SessionRecord{
		SessionID:    "s-errored",
		TenantID:     "t1",
		Status:       SessionStateError,
		ResumeError:  "transcript could not be read",
		Persistent:   true,
		BackendLocal: true,
	})

	if err := svc.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	if _, live := svc.liveSessionRecord("s-errored"); live {
		t.Fatal("an errored session was restored into the live map, where status and prompt " +
			"reach a backend holding no process for it")
	}

	// And the durable record is what answers, with the state that was recorded.
	status, err := svc.SessionStatus(ContextWithTenant(context.Background(), "t1"),
		&SessionStatusRequest{SessionID: "s-errored"})
	if err != nil {
		t.Fatalf("SessionStatus: %v", err)
	}
	if status.State != SessionStateError {
		t.Errorf("state = %q, want error; the recorded failure is the answer that exists", status.State)
	}
}
