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
	"testing"
)

// =============================================================================
// The spawn/exit race
// =============================================================================
//
// A supervised agent subprocess can die between backend.Spawn returning and the
// durable record being persisted — a CLI that emits its init response and then
// exits on a bad credential does exactly that. In that window the supervisor's
// exit observer fires, finds no record to close, and does not retry; PutSession
// then writes the record ACTIVE and every store-backed reader afterwards — the
// stall detector included — believes a dead agent is live, permanently.
//
// The window has two halves and they are closed by two different things: an exit
// BEFORE the write by the post-write reconciliation these tests cover, an exit
// AFTER it by the observer. Both converge on one closed record.

// spawnRaceHarness wires the service over a backend whose Status is scripted,
// which is what makes this deterministic: the race is a timing window, and a
// test that tried to hit it by racing a real process would be flaky in exactly
// the direction that hides the bug.
func spawnRaceHarness(t *testing.T, state SessionState) (AgentService, AgentStore) {
	t.Helper()
	return spawnRaceHarnessOver(t, NewInMemoryAgentStore(), state)
}

// spawnRaceHarnessOver is spawnRaceHarness over a caller-supplied store, so a
// test can supply one that refuses the closing write.
func spawnRaceHarnessOver(t *testing.T, store AgentStore, state SessionState) (AgentService, AgentStore) {
	t.Helper()
	backend := &mockAgentBackend{
		spawnFn: func(context.Context, *SpawnRequest) (*Session, error) {
			return &Session{
				ID:        "session-race",
				Backend:   "mock",
				Status:    SessionStateActive,
				CreatedAt: 1,
			}, nil
		},
		statusFn: func(context.Context, string) (*SessionStatus, error) {
			return &SessionStatus{SessionID: "session-race", State: state}, nil
		},
	}
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	return svc, store
}

// refusingStore takes the create and refuses the closing update.
//
// It stands for the one case where best effort is the wrong posture: the daemon
// KNOWS the record it just wrote is false and cannot correct it. Reporting a
// successful spawn over that would knowingly leave the durable false-liveness
// state this whole path exists to remove.
type refusingStore struct {
	AgentStore
	deleted bool
}

func (s *refusingStore) UpdateSessionStatus(context.Context, string, string, SessionState) error {
	return errors.New("store refused the write")
}

// ConcludeSession is the terminal write the conclusion path takes, and it is
// refused for the same reason: this double stands for a store that will not
// record an ending the daemon knows it has to record.
func (s *refusingStore) ConcludeSession(context.Context, string, string, SessionConclusionReason) error {
	return errors.New("store refused the write")
}

func (s *refusingStore) DeleteSession(ctx context.Context, tenantID, sessionID string) error {
	s.deleted = true
	return s.AgentStore.DeleteSession(ctx, tenantID, sessionID)
}

func TestSpawnSession_AProcessThatDiedBeforePersistenceIsNotRecordedActive(t *testing.T) {
	ctx := context.Background()
	svc, store := spawnRaceHarness(t, SessionStateClosed)

	resp, err := svc.SpawnSession(ctx, &SpawnSessionRequest{Backend: "mock", Persistent: true})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	record, err := store.GetSession(ctx, StorageTenant(ctx), resp.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status != SessionStateClosed {
		t.Fatalf("durable record status = %q, want closed; a dead agent persisted as active is a "+
			"permanently false liveness state for every store-backed reader", record.Status)
	}
}

func TestSpawnSession_ALiveProcessIsLeftActive(t *testing.T) {
	// The control. Without it, a reconciliation that closed every record would
	// pass the test above and break every spawn.
	ctx := context.Background()
	svc, store := spawnRaceHarness(t, SessionStateActive)

	resp, err := svc.SpawnSession(ctx, &SpawnSessionRequest{Backend: "mock", Persistent: true})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	record, err := store.GetSession(ctx, StorageTenant(ctx), resp.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status == SessionStateClosed {
		t.Fatal("a live session was persisted closed")
	}
}

func TestSpawnSession_AKnownFalseRecordFailsTheSpawnRatherThanStanding(t *testing.T) {
	ctx := context.Background()
	store := &refusingStore{AgentStore: NewInMemoryAgentStore()}
	svc, _ := spawnRaceHarnessOver(t, store, SessionStateClosed)

	_, err := svc.SpawnSession(ctx, &SpawnSessionRequest{Backend: "mock", Persistent: true})
	if err == nil {
		t.Fatal("SpawnSession reported success over a record it knows is false")
	}
	if !strings.Contains(err.Error(), "could not be recorded") {
		t.Errorf("error = %v, want it to name what could not be done", err)
	}
	if !store.deleted {
		t.Fatal("the unwind never attempted to remove the false record")
	}
	if _, getErr := store.GetSession(ctx, StorageTenant(ctx), "session-race"); getErr == nil {
		t.Error("the false record is still readable from the store after the unwind")
	}
}

func TestListSessions_ADurableClosedVerdictBeatsTheLiveMapsAssumption(t *testing.T) {
	// A live map entry is reported active because nothing asked otherwise; a
	// record marked closed is something the daemon OBSERVED. Preferring the
	// assumption listed a dead agent as active for as long as its map entry
	// survived — which, for a session closed at spawn, is the whole life of the
	// daemon.
	ctx := context.Background()
	svc, store := spawnRaceHarness(t, SessionStateClosed)

	resp, err := svc.SpawnSession(ctx, &SpawnSessionRequest{Backend: "mock", Persistent: true})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	record, err := store.GetSession(ctx, StorageTenant(ctx), resp.SessionID)
	if err != nil || record.Status != SessionStateClosed {
		t.Fatalf("precondition: durable record is not closed (%v, %v)", record, err)
	}

	listed, err := svc.ListSessions(ctx, &ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	for _, info := range listed.Sessions {
		if info.SessionID != resp.SessionID {
			continue
		}
		if info.State != SessionStateClosed {
			t.Fatalf("listed state = %q, want closed; the durable verdict must beat the live map", info.State)
		}
		return
	}
	t.Fatal("the session was not listed at all")
}

func TestSpawnSession_AProbeThatCannotAnswerLeavesTheSpawnAlone(t *testing.T) {
	// Best effort IS right for this half: a backend with no Status to give
	// answers nothing, and a check that added information when it could must
	// take nothing away when it cannot.
	ctx := context.Background()
	store := NewInMemoryAgentStore()
	backend := &mockAgentBackend{
		spawnFn: func(context.Context, *SpawnRequest) (*Session, error) {
			return &Session{ID: "session-quiet", Backend: "mock", Status: SessionStateActive, CreatedAt: 1}, nil
		},
		statusFn: func(context.Context, string) (*SessionStatus, error) {
			return nil, errors.New("this backend does not report status")
		},
	}
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}

	resp, err := svc.SpawnSession(ctx, &SpawnSessionRequest{Backend: "mock", Persistent: true})
	if err != nil {
		t.Fatalf("SpawnSession failed because a probe could not answer: %v", err)
	}
	record, err := store.GetSession(ctx, StorageTenant(ctx), resp.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status == SessionStateClosed {
		t.Error("an unanswerable probe closed the record; nothing was known and nothing should have been written")
	}
}

func TestReconcileSpawnedContainerState_AnAutoRemovedContainerCountsAsExited(t *testing.T) {
	// The detached sandbox spec sets AutoRemove, so `--rm` deletes the
	// container the moment it exits and an inspect a heartbeat later finds
	// nothing. Reading that as "unanswerable" would leave the fast-failing
	// container — the exact case this probe exists for — persisted active.
	ctx := context.Background()
	store := NewInMemoryAgentStore()
	svc, err := NewAgentService(store, map[string]AgentBackend{}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetContainerRuntime(&mockContainerRuntime{
		available: true,
		isRunErr:  &ContainerNotFoundError{ContainerID: "c-gone"},
	})

	tenant := StorageTenant(ctx)
	// The live record is what arbitrates a conclusion, so a session that has
	// one is the shape reconcile actually meets: SpawnSession stores it before
	// the reconcile runs.
	impl.sessions.Store("s-gone", &sessionRecord{TenantID: tenant, ContainerID: "c-gone"})
	if putErr := store.PutSession(ctx, &SessionRecord{
		SessionID: "s-gone", TenantID: tenant, Status: SessionStateActive,
	}); putErr != nil {
		t.Fatalf("PutSession: %v", putErr)
	}

	closed, reconcileErr := impl.reconcileSpawnedContainerState(ctx, tenant, tenant, "s-gone", "c-gone")
	if reconcileErr != nil {
		t.Fatalf("reconcile: %v", reconcileErr)
	}
	if !closed {
		t.Fatal("an auto-removed container was read as unanswerable rather than exited")
	}
	record, err := store.GetSession(ctx, tenant, "s-gone")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status != SessionStateClosed {
		t.Errorf("record status = %q, want closed", record.Status)
	}
}

func TestReconcileSpawnedContainerState_AnUnreadableRuntimeChangesNothing(t *testing.T) {
	// The control: a runtime that cannot answer is not evidence of anything,
	// and treating every inspect failure as an exit would close live sessions.
	ctx := context.Background()
	store := NewInMemoryAgentStore()
	svc, err := NewAgentService(store, map[string]AgentBackend{}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetContainerRuntime(&mockContainerRuntime{
		available: true,
		isRunErr:  errors.New("docker daemon unreachable"),
	})

	tenant := StorageTenant(ctx)
	impl.sessions.Store("s-unknown", &sessionRecord{TenantID: tenant, ContainerID: "c-unknown"})
	if putErr := store.PutSession(ctx, &SessionRecord{
		SessionID: "s-unknown", TenantID: tenant, Status: SessionStateActive,
	}); putErr != nil {
		t.Fatalf("PutSession: %v", putErr)
	}

	closed, reconcileErr := impl.reconcileSpawnedContainerState(ctx, tenant, tenant, "s-unknown", "c-unknown")
	if reconcileErr != nil || closed {
		t.Fatalf("an unreadable runtime produced a verdict (closed=%v err=%v)", closed, reconcileErr)
	}
	record, err := store.GetSession(ctx, tenant, "s-unknown")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status != SessionStateActive {
		t.Errorf("record status = %q, want it untouched at active", record.Status)
	}
}

// recordingSessionLog captures what the lifecycle writes to the event log.
type recordingSessionLog struct {
	SessionEventLog
	mu      sync.Mutex
	meta    map[string]*SessionMeta
	events  map[string][]*SessionEvent
	closed  int
	reasons map[string]string

	// putMetaErr, when set, makes the metadata write fail — the fault that
	// leaves the append gate open behind an ended session.
	putMetaErr error

	// appendErr, when set, makes the event write fail — the fault that leaves
	// an ended session with a shut gate and no ending in its log.
	appendErr error

	// getMetaErr, when set, makes the metadata READ fail — a store that cannot
	// answer, which is not the same as a session it never registered.
	getMetaErr error

	// getMetaErrOnce, when set, fails the NEXT metadata read and then clears
	// itself: a transient blip rather than a store that is down. The gate's
	// behaviour differs between the two only if it stops treating an
	// unanswerable read as an answer.
	getMetaErrOnce error

	// afterRange runs once the tail has been read but before the caller sees
	// it, which is where a second writer gets in.
	afterRange func()

	// afterGetMeta is the same seam on the gate read: the answer is
	// snapshotted, then the world moves before the caller acts on it.
	afterGetMeta func()
}

func newRecordingSessionLog() *recordingSessionLog {
	return &recordingSessionLog{
		meta:    map[string]*SessionMeta{},
		events:  map[string][]*SessionEvent{},
		reasons: map[string]string{},
	}
}

func (l *recordingSessionLog) PutMeta(_ context.Context, meta *SessionMeta) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.putMetaErr != nil {
		return l.putMetaErr
	}
	copied := *meta
	l.meta[meta.SessionID] = &copied
	return nil
}

// failPutMeta turns the metadata write on and off between phases of a test.
func (l *recordingSessionLog) failPutMeta(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.putMetaErr = err
}

func (l *recordingSessionLog) GetMeta(_ context.Context, _, sessionID string) (*SessionMeta, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.getMetaErr != nil {
		return nil, l.getMetaErr
	}
	if err := l.getMetaErrOnce; err != nil {
		l.getMetaErrOnce = nil
		return nil, err
	}
	meta, ok := l.meta[sessionID]
	if !ok {
		// The contract both real logs implement: a session that was never
		// registered is ErrSessionLogNotFound, which is an ANSWER — distinct
		// from a store that could not answer at all.
		return nil, ErrSessionLogNotFound
	}
	copied := *meta

	// The snapshot this caller will act on has now been taken, and the world is
	// let move before the caller sees it — which is what being overtaken looks
	// like from inside a reader.
	if after := l.afterGetMeta; after != nil {
		l.afterGetMeta = nil
		l.mu.Unlock()
		after()
		l.mu.Lock()
	}
	return &copied, nil
}

// onceAfterGetMeta runs fn after the NEXT metadata read has been snapshotted
// but before its caller sees the answer.
func (l *recordingSessionLog) onceAfterGetMeta(fn func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.afterGetMeta = fn
}

func (l *recordingSessionLog) Append(_ context.Context, event *SessionEvent) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.appendErr != nil {
		return l.appendErr
	}
	if event == nil {
		return nil
	}
	// MONOTONICITY, AS BOTH REAL LOGS ENFORCE IT. An append is admitted only at
	// the next sequence, which is what makes a writer that was overtaken collide
	// instead of landing.
	if event.Sequence != uint64(len(l.events[event.SessionID]))+1 {
		return ErrSessionLogGap
	}
	stored := *event
	l.events[event.SessionID] = append(l.events[event.SessionID], &stored)
	// The real KV log rolls the metadata's sequence forward in a SECOND write,
	// after the event has landed — so the metadata can end up describing fewer
	// events than the log holds. rewindMeta is that gap, opened deliberately.
	if meta, ok := l.meta[event.SessionID]; ok {
		meta.LatestSequence = uint64(len(l.events[event.SessionID]))
	}
	if event.Kind == KindSessionClosed {
		l.closed++
		if event.Closed != nil {
			l.reasons[event.SessionID] = event.Closed.Reason
		}
	}
	return nil
}

// failAppend turns the event write on and off between phases of a test.
func (l *recordingSessionLog) failAppend(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.appendErr = err
}

// eventCount reports how many events a session's log actually holds, which is
// the only way to tell a refused append from one that merely reported an error.
func (l *recordingSessionLog) eventCount(sessionID string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.events[sessionID])
}

// lastKind reports the kind of a session's final event, so a test can assert
// that the ending is still the ending.
func (l *recordingSessionLog) lastKind(sessionID string) SessionEventKind {
	l.mu.Lock()
	defer l.mu.Unlock()
	events := l.events[sessionID]
	if len(events) == 0 {
		return ""
	}
	return events[len(events)-1].Kind
}

// terminalReason returns the attribution recorded on a session's terminal
// event, and whether one was appended at all.
func (l *recordingSessionLog) terminalReason(sessionID string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	reason, ok := l.reasons[sessionID]
	return reason, ok
}

// LatestSequence answers from the metadata when there is one, exactly as the KV
// log does — which is what makes it a hint that can sit behind the events.
func (l *recordingSessionLog) LatestSequence(_ context.Context, _, sessionID string) (uint64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if meta, ok := l.meta[sessionID]; ok {
		return meta.LatestSequence, nil
	}
	return uint64(len(l.events[sessionID])), nil
}

// failGetMeta turns the metadata read on and off between phases of a test.
func (l *recordingSessionLog) failGetMeta(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.getMetaErr = err
}

// failGetMetaOnce fails only the next metadata read, then heals.
func (l *recordingSessionLog) failGetMetaOnce(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.getMetaErrOnce = err
}

// forget drops a session's metadata, leaving a log that never registered it.
func (l *recordingSessionLog) forget(sessionID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.meta, sessionID)
}

// rewindMeta puts the metadata's sequence BEHIND the events the log actually
// holds — the state a failed roll-forward write leaves. The events stay dense
// and correct; only the cheap answer to "where is the tail" is stale.
func (l *recordingSessionLog) rewindMeta(sessionID string, seq uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if meta, ok := l.meta[sessionID]; ok {
		meta.LatestSequence = seq
	}
}

// Range serves the dense, 1-indexed, fromSeq-inclusive window the real logs
// serve — the conclusion asks it for the last event to decide whether the
// ending is already there.
func (l *recordingSessionLog) Range(
	_ context.Context, _, sessionID string, fromSeq uint64, limit int) ([]*SessionEvent, uint64, error) {
	l.mu.Lock()
	events := l.events[sessionID]
	start := 0
	if fromSeq > 0 {
		start = int(fromSeq - 1)
	}
	var window []*SessionEvent
	if start < len(events) {
		end := len(events)
		if limit > 0 && start+limit < end {
			end = start + limit
		}
		window = append([]*SessionEvent(nil), events[start:end]...)
	}
	after := l.afterRange
	l.mu.Unlock()

	// The snapshot is taken, THEN the world is allowed to move — which is what
	// another process concluding the same session looks like from in here.
	if after != nil {
		after()
	}
	return window, 0, nil
}

// onceAfterRange runs fn after the NEXT read of the log's tail, so a test can
// make an ending appear in the window between a caller's probe and its append.
func (l *recordingSessionLog) onceAfterRange(fn func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.afterRange = func() {
		l.mu.Lock()
		l.afterRange = nil
		l.mu.Unlock()
		fn()
	}
}

func (l *recordingSessionLog) status(sessionID string) SessionState {
	l.mu.Lock()
	defer l.mu.Unlock()
	if meta, ok := l.meta[sessionID]; ok {
		return meta.Status
	}
	return ""
}

func TestRecordSessionExit_ClosesEverySurfaceTheStateLivesOn(t *testing.T) {
	// A session's state lives in three places: the store record that the stall
	// detector reads, the session-log metadata that a LISTING reads, and the
	// terminal event an attacher watching the stream sees. Closing only the
	// first left a crashed agent listed active with its stream open forever —
	// the same false liveness the record fix removed, one surface over.
	ctx := context.Background()
	store := NewInMemoryAgentStore()
	svc, err := NewAgentService(store, map[string]AgentBackend{}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	log := newRecordingSessionLog()
	impl.SetSessionEventLog(log)

	tenant := StorageTenant(ctx)
	if putErr := store.PutSession(ctx, &SessionRecord{
		SessionID: "s-crashed", TenantID: tenant, Status: SessionStateActive,
	}); putErr != nil {
		t.Fatalf("PutSession: %v", putErr)
	}
	if putErr := log.PutMeta(ctx, &SessionMeta{
		SessionID: "s-crashed", TenantID: tenant, Status: SessionStateActive,
	}); putErr != nil {
		t.Fatalf("PutMeta: %v", putErr)
	}
	impl.sessions.Store("s-crashed", &sessionRecord{TenantID: tenant})

	impl.RecordSessionExit(ctx, tenant, "s-crashed", "agent process exited")

	record, err := store.GetSession(ctx, tenant, "s-crashed")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status != SessionStateClosed {
		t.Errorf("store record = %q, want closed", record.Status)
	}
	if got := log.status("s-crashed"); got != SessionStateClosed {
		t.Errorf("session-log metadata = %q, want closed — a listing reads this, not the store", got)
	}
	if log.closed == 0 {
		t.Error("no terminal event was appended; an attacher's stream never ends")
	}
}

func TestRecordSessionExit_IsSafeToRepeatAndLeavesTheSessionClosable(t *testing.T) {
	// Two properties that pull in opposite directions and are both required.
	//
	// An operator close ENDS the process, which fires this observer — so the
	// terminal event must be appended at most once or an attacher sees the
	// session end twice. And the live map entry must SURVIVE, because
	// CloseSession finds a session's backend through it: deleting it makes the
	// backend's own teardown unreachable, and with it the ephemeral MCP
	// certificate directory, the pooled account credential tree and the
	// container env file.
	ctx := context.Background()
	store := NewInMemoryAgentStore()
	backend := &mockAgentBackend{}
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	log := newRecordingSessionLog()
	impl.SetSessionEventLog(log)

	tenant := StorageTenant(ctx)
	if putErr := store.PutSession(ctx, &SessionRecord{
		SessionID: "s-twice", TenantID: tenant, Status: SessionStateActive,
	}); putErr != nil {
		t.Fatalf("PutSession: %v", putErr)
	}
	if putErr := log.PutMeta(ctx, &SessionMeta{
		SessionID: "s-twice", TenantID: tenant, Status: SessionStateActive,
	}); putErr != nil {
		t.Fatalf("PutMeta: %v", putErr)
	}
	impl.sessions.Store("s-twice", &sessionRecord{Backend: "mock", TenantID: tenant})

	impl.RecordSessionExit(ctx, tenant, "s-twice", "agent process exited")
	impl.RecordSessionExit(ctx, tenant, "s-twice", "agent process exited")

	if log.closed != 1 {
		t.Errorf("terminal events = %d, want exactly 1; an attacher must not see the session end twice", log.closed)
	}

	// THE DURABLE RECORD IS THE TOMBSTONE, so the live entry is released once
	// the terminal state is confirmed. Retaining it was how idempotence worked
	// before the concluded/persisted split, and it made the map grow without
	// bound — one record, with its tenant-supplied strings, per session the
	// daemon ever ran.
	if _, live := impl.sessions.Load("s-twice"); live {
		t.Error("the live entry survived a confirmed conclusion; the map grows without bound")
	}
	record, err := store.GetSession(ctx, tenant, "s-twice")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status != SessionStateClosed {
		t.Errorf("durable record = %q, want closed — it is what a late caller reads now", record.Status)
	}
}

func TestSpawnSession_ADeadAtSpawnSessionKeepsItsEndingLast(t *testing.T) {
	// KindSessionClosed is documented as always the LAST event of a session,
	// and the KV log derives its next sequence from the metadata — so a
	// terminal event appended before the session is registered lands at a
	// sequence the registration then resets, and the next append overwrites it.
	// A session that died inside the spawn window would have its ending erased
	// by its own beginning.
	ctx := context.Background()
	store := NewInMemoryAgentStore()
	svc, _ := spawnRaceHarnessOver(t, store, SessionStateClosed)
	impl := svc.(*agentServiceImpl)
	log := newRecordingSessionLog()
	impl.SetSessionEventLog(log)

	resp, err := svc.SpawnSession(ctx, &SpawnSessionRequest{Backend: "mock", Persistent: true})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	// The record is closed, the log agrees, and the ending was written exactly
	// once — after the registration rather than before it.
	record, err := store.GetSession(ctx, StorageTenant(ctx), resp.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status != SessionStateClosed {
		t.Errorf("durable record = %q, want closed", record.Status)
	}
	if got := log.status(resp.SessionID); got != SessionStateClosed {
		t.Errorf("session-log metadata = %q, want closed", got)
	}
	if log.closed != 1 {
		t.Errorf("terminal events = %d, want exactly 1", log.closed)
	}
	if reason, ok := log.terminalReason(resp.SessionID); !ok || reason != string(ConcludedByReconcile) {
		t.Errorf("terminal reason = %q (present=%v), want %q — the ending must name what happened",
			reason, ok, ConcludedByReconcile)
	}
}
