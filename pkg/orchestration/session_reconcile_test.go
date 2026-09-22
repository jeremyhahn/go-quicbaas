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
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"sort"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// Session reconciliation
// =============================================================================
//
// The property under test is that a session record cannot outlive the thing it
// describes: the node that ran it, the job it worked for, or the daemon it was
// pinned to. Each test seeds a store, states which of those three is gone, and
// asserts the record agrees — and, in the control, that a session with a live
// process is left exactly as it was.

// reconcileHarness wires a service over an in-memory store with one scripted
// backend, plus the KV the job records live in.
func reconcileHarness(t *testing.T) (*agentServiceImpl, AgentStore) {
	t.Helper()
	store := NewInMemoryAgentStore()
	backend := &mockAgentBackend{backendType: "mock"}
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("NewAgentService returned %T, want *agentServiceImpl", svc)
	}
	impl.SetKVStore(kvstore.NewMemory())
	return impl, store
}

// seedSession writes one durable record and returns it.
func seedSession(t *testing.T, store AgentStore, record *SessionRecord) *SessionRecord {
	t.Helper()
	if record.Status == "" {
		record.Status = SessionStateActive
	}
	if record.Backend == "" {
		record.Backend = "mock"
	}
	if record.CreatedAt == 0 {
		record.CreatedAt = time.Now().Add(-24 * time.Hour).UnixNano()
	}
	if record.LastActivity == 0 {
		record.LastActivity = record.CreatedAt
	}
	if err := store.PutSession(context.Background(), record); err != nil {
		t.Fatalf("PutSession(%s): %v", record.SessionID, err)
	}
	return record
}

// seedJob writes one persisted job status.
func seedJob(t *testing.T, svc *agentServiceImpl, tenantID, jobID string, state JobState, sessionIDs ...string) {
	t.Helper()
	svc.persistJobStatus(tenantID, &JobStatus{
		JobID:      jobID,
		TenantID:   tenantID,
		State:      state,
		SessionIDs: sessionIDs,
		UpdatedAt:  time.Now().UnixNano(),
	})
}

// storedStatus reads one record's durable status and conclusion reason.
func storedStatus(t *testing.T, store AgentStore, tenantID, sessionID string) (SessionState, SessionConclusionReason) {
	t.Helper()
	record, err := store.GetSession(context.Background(), tenantID, sessionID)
	if err != nil {
		t.Fatalf("GetSession(%s): %v", sessionID, err)
	}
	return record.Status, record.ConclusionReason
}

// =============================================================================
// The reason vocabulary
// =============================================================================

func TestSessionConclusionReason_Valid(t *testing.T) {
	for _, reason := range []SessionConclusionReason{
		ConcludedByOperator, ConcludedByProcessExit, ConcludedByReconcile,
		ConcludedByNodeRestart, ConcludedByJobConcluded, ConcludedByAgentGone,
	} {
		if !reason.Valid() {
			t.Errorf("reason %q reports invalid; every reason the daemon writes must be recognisable "+
				"when it is read back off a durable record", reason)
		}
		if reason.String() != string(reason) {
			t.Errorf("String() = %q, want %q", reason.String(), string(reason))
		}
	}
}

func TestSessionConclusionReason_ValidRejectsUnknown(t *testing.T) {
	for _, reason := range []SessionConclusionReason{"", "closed", "because"} {
		if reason.Valid() {
			t.Errorf("reason %q reports valid; a conclusion that names no known cause is the "+
				"unattributed state the reason exists to remove", reason)
		}
	}
}

// =============================================================================
// concludeSessionRecord — the transition itself
// =============================================================================

func TestConcludeSessionRecord_AbsentSessionIsConcludedWithItsReason(t *testing.T) {
	svc, store := reconcileHarness(t)
	record := seedSession(t, store, &SessionRecord{SessionID: "s-absent", TenantID: "t1"})

	written, err := svc.concludeSessionRecord(context.Background(), record, ConcludedByNodeRestart)
	if err != nil {
		t.Fatalf("concludeSessionRecord: %v", err)
	}
	if !written {
		t.Fatal("the conclusion reported that it wrote nothing over an open record")
	}
	state, reason := storedStatus(t, store, "t1", "s-absent")
	if state != SessionStateClosed {
		t.Errorf("status = %q, want closed", state)
	}
	if reason != ConcludedByNodeRestart {
		t.Errorf("reason = %q, want %q; a closed record with no attribution leaves the operator "+
			"inferring the cause from the timestamps", reason, ConcludedByNodeRestart)
	}
}

func TestConcludeSessionRecord_IsIdempotent(t *testing.T) {
	svc, store := reconcileHarness(t)
	record := seedSession(t, store, &SessionRecord{SessionID: "s-twice", TenantID: "t1"})

	if _, err := svc.concludeSessionRecord(context.Background(), record, ConcludedByOperator); err != nil {
		t.Fatalf("first conclusion: %v", err)
	}
	// The caller's copy is deliberately stale, which is the shape a sweep sees.
	written, err := svc.concludeSessionRecord(context.Background(), record, ConcludedByNodeRestart)
	if err != nil {
		t.Fatalf("second conclusion: %v", err)
	}
	if written {
		t.Fatal("a second conclusion claimed to have written the ending")
	}
	_, reason := storedStatus(t, store, "t1", "s-twice")
	if reason != ConcludedByOperator {
		t.Errorf("reason = %q, want %q; the first ending owns the attribution and a later nudge "+
			"must not re-attribute a deliberate close to a restart", reason, ConcludedByOperator)
	}
}

func TestConcludeSessionRecord_RejectsBadInput(t *testing.T) {
	svc, store := reconcileHarness(t)
	record := seedSession(t, store, &SessionRecord{SessionID: "s-bad", TenantID: "t1"})

	if _, err := svc.concludeSessionRecord(context.Background(), nil, ConcludedByNodeRestart); err == nil {
		t.Error("a nil record was accepted")
	} else if !errors.Is(err, ErrSessionIDRequired) {
		t.Errorf("error = %v, want ErrSessionIDRequired", err)
	}

	if _, err := svc.concludeSessionRecord(context.Background(), record, "invented"); err == nil {
		t.Error("an unknown reason was accepted")
	} else if !errors.Is(err, ErrSessionConclusionReasonRequired) {
		t.Errorf("error = %v, want ErrSessionConclusionReasonRequired", err)
	}

	state, _ := storedStatus(t, store, "t1", "s-bad")
	if state != SessionStateActive {
		t.Errorf("status = %q, want active; a refused conclusion must not have written anything", state)
	}
}

// =============================================================================
// Startup — the node restarted
// =============================================================================

func TestLoadPersistedSessions_ConcludesWhatCannotBeReattached(t *testing.T) {
	svc, store := reconcileHarness(t)
	ctx := context.Background()

	// Backend gone: nothing on this node can address the session again.
	seedSession(t, store, &SessionRecord{
		SessionID: "s-orphan-backend", TenantID: "t1", Backend: "retired", Persistent: true, BackendLocal: true})
	// Not persistent: the process was per-prompt and died with the daemon.
	seedSession(t, store, &SessionRecord{
		SessionID: "s-oneshot", TenantID: "t1", Persistent: false, BackendLocal: true})
	// Persistent and local: the reconstitution path CAN bring this one back.
	seedSession(t, store, &SessionRecord{
		SessionID: "s-resumable", TenantID: "t1", Persistent: true, BackendLocal: true})
	// Already closed: untouched, and its original reason preserved.
	seedSession(t, store, &SessionRecord{
		SessionID: "s-closed", TenantID: "t1", Status: SessionStateClosed})

	if err := svc.LoadPersistedSessions(ctx); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}

	for _, id := range []string{"s-orphan-backend", "s-oneshot"} {
		state, reason := storedStatus(t, store, "t1", id)
		if state != SessionStateClosed {
			t.Errorf("%s: status = %q, want closed; a record nothing can re-attach to is the "+
				"stranded-active state the reconciliation exists to remove", id, state)
		}
		if reason != ConcludedByNodeRestart {
			t.Errorf("%s: reason = %q, want %q", id, reason, ConcludedByNodeRestart)
		}
	}

	if state, _ := storedStatus(t, store, "t1", "s-resumable"); state != SessionStateResumable {
		t.Errorf("s-resumable: status = %q, want resumable; a session the reconstitution path can "+
			"bring back must survive the pass", state)
	}
	if _, live := svc.liveSessionRecord("s-resumable"); !live {
		t.Error("s-resumable was not restored to the registry")
	}
	if _, live := svc.liveSessionRecord("s-orphan-backend"); live {
		t.Error("a concluded session was registered as live")
	}
}

func TestLoadPersistedSessions_SurvivesAStoreThatWillNotRecordTheEnding(t *testing.T) {
	backend := &mockAgentBackend{backendType: "mock"}
	store := &refusingStore{AgentStore: NewInMemoryAgentStore()}
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	seedSession(t, store, &SessionRecord{
		SessionID: "s-stuck", TenantID: "t1", Backend: "retired", Persistent: true, BackendLocal: true})
	seedSession(t, store, &SessionRecord{
		SessionID: "s-ok", TenantID: "t1", Persistent: true, BackendLocal: true})

	// A record that cannot be concluded must not stop the boot: the daemon
	// still has to restore every session it CAN address.
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("service is %T", svc)
	}
	if err := impl.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	if _, live := impl.liveSessionRecord("s-ok"); !live {
		t.Error("a restorable session was dropped because another record could not be concluded")
	}
	if state, _ := storedStatus(t, store, "t1", "s-stuck"); state != SessionStateActive {
		t.Errorf("status = %q, want active; a conclusion that failed must leave the record "+
			"retryable rather than half written", state)
	}
}

// =============================================================================
// A job that ended
// =============================================================================

func TestConcludeSessionsForJob_ConcludesEveryBoundSession(t *testing.T) {
	svc, store := reconcileHarness(t)
	ctx := ContextWithTenant(context.Background(), "t1")

	seedSession(t, store, &SessionRecord{SessionID: "s-job-1", TenantID: "t1", JobID: "job-1"})
	seedSession(t, store, &SessionRecord{SessionID: "s-job-2", TenantID: "t1", JobID: "job-1"})
	seedSession(t, store, &SessionRecord{SessionID: "s-other", TenantID: "t1", JobID: "job-2"})
	seedSession(t, store, &SessionRecord{SessionID: "s-free", TenantID: "t1"})
	seedJob(t, svc, "t1", "job-1", JobStateFailed, "s-job-1", "s-job-2")

	report, err := svc.ConcludeSessionsForJob(ctx, "t1", "job-1")
	if err != nil {
		t.Fatalf("ConcludeSessionsForJob: %v", err)
	}
	if got := report.Count(SessionReconcileConcluded); got != 2 {
		t.Fatalf("concluded %d session(s), want 2 (report=%+v)", got, report.Findings)
	}

	for _, id := range []string{"s-job-1", "s-job-2"} {
		state, reason := storedStatus(t, store, "t1", id)
		if state != SessionStateClosed || reason != ConcludedByJobConcluded {
			t.Errorf("%s: status=%q reason=%q, want closed/%s", id, state, reason, ConcludedByJobConcluded)
		}
	}
	for _, id := range []string{"s-other", "s-free"} {
		if state, _ := storedStatus(t, store, "t1", id); state != SessionStateActive {
			t.Errorf("%s: status = %q, want active; only the concluded job's sessions may be ended", id, state)
		}
	}

	// The job carries the endings too, and no longer claims to hold them.
	status, err := svc.loadJobStatus("t1", "job-1")
	if err != nil {
		t.Fatalf("loadJobStatus: %v", err)
	}
	if len(status.SessionIDs) != 0 {
		t.Errorf("job still holds %v; a job naming a closed session is the same false liveness "+
			"one level up", status.SessionIDs)
	}
	var concluded int
	for _, event := range status.Events {
		if event.Type == JobAuditTypeSessionConcluded {
			concluded++
		}
	}
	if concluded != 2 {
		t.Errorf("job audit trail carries %d session conclusion(s), want 2", concluded)
	}
}

func TestConcludeSessionsForJob_RejectsAnUnnamedJob(t *testing.T) {
	svc, store := reconcileHarness(t)
	seedSession(t, store, &SessionRecord{SessionID: "s-1", TenantID: "t1", JobID: "job-1"})

	if _, err := svc.ConcludeSessionsForJob(context.Background(), "t1", ""); err == nil {
		t.Fatal("an empty job ID was accepted; it would match every session with no job")
	} else if !errors.Is(err, ErrJobIDRequired) {
		t.Errorf("error = %v, want ErrJobIDRequired", err)
	}
	if state, _ := storedStatus(t, store, "t1", "s-1"); state != SessionStateActive {
		t.Errorf("status = %q, want active", state)
	}
}

// =============================================================================
// An agent that left the fleet
// =============================================================================

func TestConcludeSessionsForAgent_ConcludesSessionsPinnedToIt(t *testing.T) {
	svc, store := reconcileHarness(t)
	agent := &AgentRegistration{ID: "agent-1", TenantID: "t1", Name: "worker-a", Host: "10.0.0.9:7000"}

	seedSession(t, store, &SessionRecord{SessionID: "s-by-name", TenantID: "t1", HostAffinity: "worker-a"})
	seedSession(t, store, &SessionRecord{SessionID: "s-by-host", TenantID: "t1", HostAffinity: "10.0.0.9:7000"})
	seedSession(t, store, &SessionRecord{SessionID: "s-elsewhere", TenantID: "t1", HostAffinity: "worker-b"})
	seedSession(t, store, &SessionRecord{SessionID: "s-here", TenantID: "t1"})

	report, err := svc.ConcludeSessionsForAgent(context.Background(), "t1", agent, "agent removed")
	if err != nil {
		t.Fatalf("ConcludeSessionsForAgent: %v", err)
	}
	if got := report.Count(SessionReconcileConcluded); got != 2 {
		t.Fatalf("concluded %d session(s), want 2", got)
	}
	for _, id := range []string{"s-by-name", "s-by-host"} {
		state, reason := storedStatus(t, store, "t1", id)
		if state != SessionStateClosed || reason != ConcludedByAgentGone {
			t.Errorf("%s: status=%q reason=%q, want closed/%s", id, state, reason, ConcludedByAgentGone)
		}
	}
	for _, id := range []string{"s-elsewhere", "s-here"} {
		if state, _ := storedStatus(t, store, "t1", id); state != SessionStateActive {
			t.Errorf("%s: status = %q, want active; an unpinned session did not run on the "+
				"removed daemon", id, state)
		}
	}
}

func TestConcludeSessionsForAgent_RejectsANilRegistration(t *testing.T) {
	svc, store := reconcileHarness(t)
	seedSession(t, store, &SessionRecord{SessionID: "s-1", TenantID: "t1", HostAffinity: "worker-a"})

	if _, err := svc.ConcludeSessionsForAgent(context.Background(), "t1", nil, "gone"); err == nil {
		t.Fatal("a nil registration was accepted; there is nothing to match a pin against")
	} else if !errors.Is(err, ErrAgentIDRequired) {
		t.Errorf("error = %v, want ErrAgentIDRequired", err)
	}
	if state, _ := storedStatus(t, store, "t1", "s-1"); state != SessionStateActive {
		t.Errorf("status = %q, want active", state)
	}
}

func TestRemoveAgent_TakesItsSessionsWithIt(t *testing.T) {
	svc, store := reconcileHarness(t)
	registry := NewInMemoryAgentRegistry()
	svc.SetAgentRegistry(registry)
	ctx := ContextWithTenant(context.Background(), "t1")

	if err := registry.RegisterAgent(ctx, &AgentRegistration{
		ID: "agent-1", TenantID: "t1", Name: "worker-a", Host: "10.0.0.9:7000",
		Backend: "mock", AgentType: "worker", Status: AgentStatusIdle,
	}); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	seedSession(t, store, &SessionRecord{SessionID: "s-pinned", TenantID: "t1", HostAffinity: "worker-a"})

	if err := svc.RemoveAgent(ctx, "t1", "agent-1"); err != nil {
		t.Fatalf("RemoveAgent: %v", err)
	}
	state, reason := storedStatus(t, store, "t1", "s-pinned")
	if state != SessionStateClosed || reason != ConcludedByAgentGone {
		t.Errorf("status=%q reason=%q, want closed/%s; an operator who removed a daemon must not "+
			"still see sessions running on it", state, reason, ConcludedByAgentGone)
	}
}

// =============================================================================
// The maintenance sweep
// =============================================================================

func TestSweepSessions_NeedsBothTheClockAndASecondFact(t *testing.T) {
	svc, store := reconcileHarness(t)
	svc.SetIdleThresholds(JobIdleThresholds{})
	ctx := context.Background()
	stale := time.Now().Add(-72 * time.Hour).UnixNano()
	fresh := time.Now().UnixNano()

	// Quiet AND its job is over.
	seedSession(t, store, &SessionRecord{
		SessionID: "s-stale-terminal", TenantID: "t1", JobID: "job-done", LastActivity: stale})
	seedJob(t, svc, "t1", "job-done", JobStateCompleted)

	// Quiet but its job is still running: the agent is slow, not gone.
	seedSession(t, store, &SessionRecord{
		SessionID: "s-stale-running", TenantID: "t1", JobID: "job-live", LastActivity: stale})
	seedJob(t, svc, "t1", "job-live", JobStateExecuting)

	// Its job is over but it spoke a moment ago: the clock has no opinion yet.
	seedSession(t, store, &SessionRecord{
		SessionID: "s-fresh-terminal", TenantID: "t1", JobID: "job-done", LastActivity: fresh})

	// Quiet with no job and no pin: nothing corroborates the clock.
	seedSession(t, store, &SessionRecord{
		SessionID: "s-stale-free", TenantID: "t1", LastActivity: stale})

	report, err := svc.SweepSessions(ctx)
	if err != nil {
		t.Fatalf("SweepSessions: %v", err)
	}
	if got := report.Count(SessionReconcileConcluded); got != 1 {
		t.Fatalf("concluded %d session(s), want 1 (findings=%+v)", got, report.Findings)
	}
	state, reason := storedStatus(t, store, "t1", "s-stale-terminal")
	if state != SessionStateClosed || reason != ConcludedByJobConcluded {
		t.Errorf("s-stale-terminal: status=%q reason=%q, want closed/%s", state, reason, ConcludedByJobConcluded)
	}
	for _, id := range []string{"s-stale-running", "s-fresh-terminal", "s-stale-free"} {
		if state, _ := storedStatus(t, store, "t1", id); state != SessionStateActive {
			t.Errorf("%s: status = %q, want active; the clock alone must not end a session", id, state)
		}
	}
}

func TestSweepSessions_ConcludesASessionWhosePinnedDaemonIsGone(t *testing.T) {
	svc, store := reconcileHarness(t)
	registry := NewInMemoryAgentRegistry()
	svc.SetAgentRegistry(registry)
	ctx := ContextWithTenant(context.Background(), "t1")
	stale := time.Now().Add(-72 * time.Hour).UnixNano()

	if err := registry.RegisterAgent(ctx, &AgentRegistration{
		ID: "agent-live", TenantID: "t1", Name: "worker-live", Host: "10.0.0.1:7000",
		Backend: "mock", AgentType: "worker", Status: AgentStatusIdle,
	}); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	seedSession(t, store, &SessionRecord{
		SessionID: "s-dangling", TenantID: "t1", HostAffinity: "worker-dead", LastActivity: stale})
	seedSession(t, store, &SessionRecord{
		SessionID: "s-attached", TenantID: "t1", HostAffinity: "worker-live", LastActivity: stale})

	if _, err := svc.SweepSessions(ctx); err != nil {
		t.Fatalf("SweepSessions: %v", err)
	}
	state, reason := storedStatus(t, store, "t1", "s-dangling")
	if state != SessionStateClosed || reason != ConcludedByAgentGone {
		t.Errorf("s-dangling: status=%q reason=%q, want closed/%s", state, reason, ConcludedByAgentGone)
	}
	if state, _ := storedStatus(t, store, "t1", "s-attached"); state != SessionStateActive {
		t.Errorf("s-attached: status = %q, want active; its daemon is still in the fleet", state)
	}
}

func TestSweepSessions_LeavesEverythingAloneWhenTheJobRecordCannotBeRead(t *testing.T) {
	// Fail-closed: no KV means no job can be shown terminal, and a sweep that
	// read "unknown" as "over" would close every live session on the node.
	store := NewInMemoryAgentStore()
	backend := &mockAgentBackend{backendType: "mock"}
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("service is %T", svc)
	}
	seedSession(t, store, &SessionRecord{
		SessionID: "s-unknown-job", TenantID: "t1", JobID: "job-?",
		LastActivity: time.Now().Add(-72 * time.Hour).UnixNano()})

	report, err := impl.SweepSessions(context.Background())
	if err != nil {
		t.Fatalf("SweepSessions: %v", err)
	}
	if got := report.Count(SessionReconcileConcluded); got != 0 {
		t.Fatalf("concluded %d session(s) with no job record to justify it", got)
	}
	if state, _ := storedStatus(t, store, "t1", "s-unknown-job"); state != SessionStateActive {
		t.Errorf("status = %q, want active", state)
	}
}

func TestSessionReconcileReport_Count(t *testing.T) {
	report := SessionReconcileReport{Findings: []SessionReconcileFinding{
		{Verdict: SessionReconcileConcluded},
		{Verdict: SessionReconcileConcluded},
		{Verdict: SessionReconcileFailed},
	}}
	if got := report.Count(SessionReconcileConcluded); got != 2 {
		t.Errorf("Count(concluded) = %d, want 2", got)
	}
	if got := report.Count(SessionReconcileAlreadyConcluded); got != 0 {
		t.Errorf("Count(already_concluded) = %d, want 0", got)
	}
	if SessionReconcileLive.String() != "live" {
		t.Errorf("String() = %q, want live", SessionReconcileLive.String())
	}
}

// =============================================================================
// The operator verb
// =============================================================================

func TestCloseSession_ClosesAStaleRecordWithNoProcessBehindIt(t *testing.T) {
	svc, store := reconcileHarness(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	seedSession(t, store, &SessionRecord{SessionID: "s-stale", TenantID: "t1"})

	resp, err := svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-stale"})
	if err != nil {
		t.Fatalf("CloseSession on a stale record: %v; the only sessions that NEED closing were the "+
			"only ones the close verb refused", err)
	}
	if resp.SessionID != "s-stale" {
		t.Errorf("SessionID = %q, want s-stale", resp.SessionID)
	}
	state, reason := storedStatus(t, store, "t1", "s-stale")
	if state != SessionStateClosed || reason != ConcludedByOperator {
		t.Errorf("status=%q reason=%q, want closed/%s", state, reason, ConcludedByOperator)
	}

	// And again: closing a concluded session is a satisfied request.
	if _, err := svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-stale"}); err != nil {
		t.Fatalf("second CloseSession: %v, want an idempotent success", err)
	}
	if _, reason := storedStatus(t, store, "t1", "s-stale"); reason != ConcludedByOperator {
		t.Errorf("reason = %q, want it unchanged by the repeat", reason)
	}
}

func TestCloseSession_StillRefusesASessionThatNeverExisted(t *testing.T) {
	svc, _ := reconcileHarness(t)
	ctx := ContextWithTenant(context.Background(), "t1")

	if _, err := svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-nope"}); err == nil {
		t.Fatal("closing a session with no record reported success")
	}
	if _, err := svc.CloseSession(ctx, &CloseSessionRequest{SessionID: ""}); err == nil {
		t.Fatal("an empty session ID was accepted")
	} else if !errors.Is(err, ErrSessionIDRequired) {
		t.Errorf("error = %v, want ErrSessionIDRequired", err)
	}
}

// =============================================================================
// The list reports a creation time
// =============================================================================

func TestListSessions_ReportsTheCreationTime(t *testing.T) {
	svc, store := reconcileHarness(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	created := time.Now().Add(-3 * time.Hour).UnixNano()
	seedSession(t, store, &SessionRecord{SessionID: "s-listed", TenantID: "t1", CreatedAt: created})

	resp, err := svc.ListSessions(ctx, &ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("listed %d session(s), want 1", len(resp.Sessions))
	}
	if resp.Sessions[0].CreatedAt != created {
		t.Errorf("created_at = %d, want %d; a list that reports the epoch tells an operator "+
			"nothing about when the session started", resp.Sessions[0].CreatedAt, created)
	}
}

func TestListSessions_ALiveRecordWithNoSpawnTimeFallsBackToTheStore(t *testing.T) {
	svc, store := reconcileHarness(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	created := time.Now().Add(-90 * time.Minute).UnixNano()
	seedSession(t, store, &SessionRecord{SessionID: "s-live", TenantID: "t1", CreatedAt: created})

	// The shape a backend that reports no spawn time leaves behind.
	svc.sessions.Store("s-live", &sessionRecord{Backend: "mock", TenantID: "t1"})

	resp, err := svc.ListSessions(ctx, &ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("listed %d session(s), want 1", len(resp.Sessions))
	}
	if resp.Sessions[0].CreatedAt != created {
		t.Errorf("created_at = %d, want %d from the durable record",
			resp.Sessions[0].CreatedAt, created)
	}
}

func TestPutSession_StampsASpawnTimeWhenTheCallerSuppliesNone(t *testing.T) {
	store := NewInMemoryAgentStore()
	before := time.Now().UnixNano()
	if err := store.PutSession(context.Background(), &SessionRecord{
		SessionID: "s-unstamped", TenantID: "t1", Backend: "mock", Status: SessionStateActive,
	}); err != nil {
		t.Fatalf("PutSession: %v", err)
	}
	record, err := store.GetSession(context.Background(), "t1", "s-unstamped")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.CreatedAt < before {
		t.Fatalf("created_at = %d, want it stamped at write time (>= %d); a record that exists "+
			"was created at some point and must say so", record.CreatedAt, before)
	}
}

func TestPutSession_KeepsTheCallersSpawnTime(t *testing.T) {
	store := NewInMemoryAgentStore()
	created := time.Now().Add(-time.Hour).UnixNano()
	if err := store.PutSession(context.Background(), &SessionRecord{
		SessionID: "s-stamped", TenantID: "t1", Backend: "mock",
		Status: SessionStateActive, CreatedAt: created,
	}); err != nil {
		t.Fatalf("PutSession: %v", err)
	}
	record, err := store.GetSession(context.Background(), "t1", "s-stamped")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.CreatedAt != created {
		t.Errorf("created_at = %d, want the caller's %d", record.CreatedAt, created)
	}
}

// =============================================================================
// The store's terminal write
// =============================================================================

func TestAgentStore_ConcludeSession(t *testing.T) {
	store := NewInMemoryAgentStore()
	ctx := context.Background()
	if err := store.PutSession(ctx, &SessionRecord{
		SessionID: "s-1", TenantID: "t1", Backend: "mock", Status: SessionStateActive,
	}); err != nil {
		t.Fatalf("PutSession: %v", err)
	}
	if err := store.ConcludeSession(ctx, "t1", "s-1", ConcludedByProcessExit); err != nil {
		t.Fatalf("ConcludeSession: %v", err)
	}
	record, err := store.GetSession(ctx, "t1", "s-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status != SessionStateClosed || record.ConclusionReason != ConcludedByProcessExit {
		t.Fatalf("status=%q reason=%q, want closed/%s", record.Status, record.ConclusionReason, ConcludedByProcessExit)
	}

	// A second conclusion keeps the first attribution.
	if err := store.ConcludeSession(ctx, "t1", "s-1", ConcludedByOperator); err != nil {
		t.Fatalf("second ConcludeSession: %v", err)
	}
	record, err = store.GetSession(ctx, "t1", "s-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.ConclusionReason != ConcludedByProcessExit {
		t.Errorf("reason = %q, want the first ending's %q", record.ConclusionReason, ConcludedByProcessExit)
	}
}

func TestAgentStore_ConcludeSessionOnAMissingRecord(t *testing.T) {
	store := NewInMemoryAgentStore()
	err := store.ConcludeSession(context.Background(), "t1", "s-absent", ConcludedByOperator)
	if err == nil {
		t.Fatal("concluding a record that does not exist reported success")
	}
	if !errors.Is(err, ErrAgentStoreNotFound) {
		t.Errorf("error = %v, want ErrAgentStoreNotFound", err)
	}
}

func TestSessionRecord_ConclusionReasonSurvivesTheWire(t *testing.T) {
	data, err := json.Marshal(&SessionRecord{
		SessionID: "s-1", TenantID: "t1", Status: SessionStateClosed,
		ConclusionReason: ConcludedByNodeRestart,
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded SessionRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.ConclusionReason != ConcludedByNodeRestart {
		t.Errorf("reason = %q, want %q; the record is the tombstone, so the attribution has to "+
			"survive being written down", decoded.ConclusionReason, ConcludedByNodeRestart)
	}

	// An open record carries none, and the field is omitted rather than empty.
	open, err := json.Marshal(&SessionRecord{SessionID: "s-2", Status: SessionStateActive})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(open), "conclusion_reason") {
		t.Errorf("an open record carries a conclusion_reason field: %s", open)
	}
}

func TestSweepSessions_ASilentDaemonIsAGoneDaemon(t *testing.T) {
	svc, store := reconcileHarness(t)
	kv := kvstore.NewMemory()
	svc.SetAgentRegistry(NewAgentRegistry(kv))
	svc.SetAgentHeartbeatTimeout(time.Minute)
	ctx := ContextWithTenant(context.Background(), "t1")
	stale := time.Now().Add(-72 * time.Hour).UnixNano()

	// The heartbeat stamp belongs to the registry — RegisterAgent and
	// UpdateAgent both set it to now — so an ancient one is seeded through the
	// KV the registry reads rather than through an API that would overwrite
	// the very fact under test.
	seedAgentRecord(t, kv, &AgentRegistration{
		ID: "agent-silent", TenantID: "t1", Name: "worker-silent", Host: "10.0.0.2:7000",
		Backend: "mock", AgentType: "worker", Status: AgentStatusIdle,
		LastSeen: time.Now().Add(-2 * time.Hour).UnixNano(),
	})
	seedSession(t, store, &SessionRecord{
		SessionID: "s-on-silent", TenantID: "t1", HostAffinity: "worker-silent", LastActivity: stale})

	if _, err := svc.SweepSessions(ctx); err != nil {
		t.Fatalf("SweepSessions: %v", err)
	}
	state, reason := storedStatus(t, store, "t1", "s-on-silent")
	if state != SessionStateClosed || reason != ConcludedByAgentGone {
		t.Errorf("status=%q reason=%q, want closed/%s; a registration that survives its daemon is "+
			"a record, not liveness", state, reason, ConcludedByAgentGone)
	}
}

func TestSweepSessions_ARecentlyHeardDaemonKeepsItsSessions(t *testing.T) {
	svc, store := reconcileHarness(t)
	registry := NewInMemoryAgentRegistry()
	svc.SetAgentRegistry(registry)
	svc.SetAgentHeartbeatTimeout(time.Hour)
	ctx := ContextWithTenant(context.Background(), "t1")

	if err := registry.RegisterAgent(ctx, &AgentRegistration{
		ID: "agent-live", TenantID: "t1", Name: "worker-live", Host: "10.0.0.3:7000",
		Backend: "mock", AgentType: "worker", Status: AgentStatusIdle,
	}); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	seedSession(t, store, &SessionRecord{
		SessionID: "s-on-live", TenantID: "t1", HostAffinity: "worker-live",
		LastActivity: time.Now().Add(-72 * time.Hour).UnixNano()})

	if _, err := svc.SweepSessions(ctx); err != nil {
		t.Fatalf("SweepSessions: %v", err)
	}
	if state, _ := storedStatus(t, store, "t1", "s-on-live"); state != SessionStateActive {
		t.Errorf("status = %q, want active; a long-running agent that has not spoken for a while "+
			"is what a long-running agent looks like", state)
	}
}

func TestAgentHeartbeatTimeout_DefaultsWhenUnset(t *testing.T) {
	svc, _ := reconcileHarness(t)
	if got := svc.effectiveAgentHeartbeatTimeout(); got != DefaultAgentHeartbeatTimeout {
		t.Errorf("timeout = %s, want the default %s", got, DefaultAgentHeartbeatTimeout)
	}
	svc.SetAgentHeartbeatTimeout(-time.Second)
	if got := svc.effectiveAgentHeartbeatTimeout(); got != DefaultAgentHeartbeatTimeout {
		t.Errorf("timeout = %s after a non-positive setting, want the default %s",
			got, DefaultAgentHeartbeatTimeout)
	}
	svc.SetAgentHeartbeatTimeout(5 * time.Minute)
	if got := svc.effectiveAgentHeartbeatTimeout(); got != 5*time.Minute {
		t.Errorf("timeout = %s, want 5m", got)
	}
}

// seedAgentRecord writes one registration straight into the KV the registry
// reads, so a test can state a heartbeat age the registry's own writers would
// overwrite.
func seedAgentRecord(t *testing.T, kv interfaces.KVStore, agent *AgentRegistration) {
	t.Helper()
	key, err := agentKey(agent.TenantID, agent.ID)
	if err != nil {
		t.Fatalf("agentKey: %v", err)
	}
	data, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := kv.Put(agentTenantContext(context.Background(), agent.TenantID), key, data); err != nil {
		t.Fatalf("Put(%s): %v", key, err)
	}
}

// =============================================================================
// Enumerating across tenants
// =============================================================================

// routedStore stands for the durable store the daemon actually runs over: it
// answers ListSessions for the tenant named on the CONTEXT and returns nothing
// for a cross-tenant scan, which is what pkg/server's tenant-routing KV does.
type routedStore struct {
	AgentStore
}

func (s *routedStore) ListAllSessions(context.Context) ([]*SessionRecord, error) {
	return nil, nil
}

func (s *routedStore) ListSessions(ctx context.Context, tenantID string) ([]*SessionRecord, error) {
	if TenantFromContext(ctx) != tenantID {
		return nil, nil
	}
	return s.AgentStore.ListSessions(ctx, tenantID)
}

func TestSweepSessions_VisitsEveryTenantOnARoutedStore(t *testing.T) {
	store := &routedStore{AgentStore: NewInMemoryAgentStore()}
	backend := &mockAgentBackend{backendType: "mock"}
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("service is %T", svc)
	}
	impl.SetKVStore(kvstore.NewMemory())
	impl.SetTenantLister(&fakeTenantLister{ids: []string{"t1", "t2"}})

	stale := time.Now().Add(-72 * time.Hour).UnixNano()
	for _, tenant := range []string{"t1", "t2"} {
		seedSession(t, store, &SessionRecord{
			SessionID: "s-" + tenant, TenantID: tenant, JobID: "job-" + tenant, LastActivity: stale})
		seedJob(t, impl, tenant, "job-"+tenant, JobStateCompleted)
	}

	report, err := impl.SweepSessions(context.Background())
	if err != nil {
		t.Fatalf("SweepSessions: %v", err)
	}
	if got := report.Count(SessionReconcileConcluded); got != 2 {
		t.Fatalf("concluded %d session(s), want 2; a cross-tenant scan on a routed store sees the "+
			"system tenant and nothing else, so the pass has to name every tenant", got)
	}
	for _, tenant := range []string{"t1", "t2"} {
		state, reason := storedStatus(t, store, tenant, "s-"+tenant)
		if state != SessionStateClosed || reason != ConcludedByJobConcluded {
			t.Errorf("%s: status=%q reason=%q, want closed/%s", tenant, state, reason, ConcludedByJobConcluded)
		}
	}
}

func TestSweepSessions_OneSealedTenantDoesNotEndTheWalk(t *testing.T) {
	// A tenant whose barrier is sealed cannot be listed. Giving up there
	// would leave every later tenant unswept because of one that is
	// legitimately unavailable.
	store := &sealedTenantStore{AgentStore: NewInMemoryAgentStore(), sealed: "t-sealed"}
	backend := &mockAgentBackend{backendType: "mock"}
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("service is %T", svc)
	}
	impl.SetKVStore(kvstore.NewMemory())
	impl.SetTenantLister(&fakeTenantLister{ids: []string{"t-sealed", "t-open"}})

	stale := time.Now().Add(-72 * time.Hour).UnixNano()
	seedSession(t, store, &SessionRecord{
		SessionID: "s-open", TenantID: "t-open", JobID: "job-open", LastActivity: stale})
	seedJob(t, impl, "t-open", "job-open", JobStateFailed)

	report, err := impl.SweepSessions(context.Background())
	if err != nil {
		t.Fatalf("SweepSessions: %v", err)
	}
	if got := report.Count(SessionReconcileConcluded); got != 1 {
		t.Fatalf("concluded %d session(s), want 1", got)
	}
	if state, _ := storedStatus(t, store, "t-open", "s-open"); state != SessionStateClosed {
		t.Errorf("status = %q, want closed", state)
	}
}

// sealedTenantStore refuses to list one tenant, as a sealed barrier does.
type sealedTenantStore struct {
	AgentStore
	sealed string
}

func (s *sealedTenantStore) ListAllSessions(context.Context) ([]*SessionRecord, error) {
	return nil, nil
}

func (s *sealedTenantStore) ListSessions(ctx context.Context, tenantID string) ([]*SessionRecord, error) {
	if tenantID == s.sealed {
		return nil, errors.New("tenant barrier is sealed")
	}
	return s.AgentStore.ListSessions(ctx, tenantID)
}

func TestEachSessionRecord_ReportsATenantListingThatFailsOutright(t *testing.T) {
	svc, _ := reconcileHarness(t)
	svc.SetTenantLister(&fakeTenantLister{err: errors.New("tenant manager unavailable")})

	if _, err := svc.SweepSessions(context.Background()); err == nil {
		t.Fatal("a sweep that could not learn which tenants exist reported success; it swept nothing")
	}
}

func TestEachSessionRecord_SweepsTheSystemTenantExactlyOnce(t *testing.T) {
	store := &routedStore{AgentStore: NewInMemoryAgentStore()}
	backend := &mockAgentBackend{backendType: "mock"}
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("service is %T", svc)
	}
	// The system tenant is prepended because the tenant manager does not list
	// it; when the manager DOES list it, it must not be visited twice.
	impl.SetTenantLister(&fakeTenantLister{ids: []string{SystemTenantID, "t1"}})
	seedSession(t, store, &SessionRecord{SessionID: "s-sys", TenantID: SystemTenantID})
	seedSession(t, store, &SessionRecord{SessionID: "s-t1", TenantID: "t1"})

	seen := map[string]int{}
	if err := impl.eachSessionRecord(context.Background(), func(_ context.Context, record *SessionRecord) {
		seen[record.SessionID]++
	}); err != nil {
		t.Fatalf("eachSessionRecord: %v", err)
	}
	if seen["s-sys"] != 1 || seen["s-t1"] != 1 {
		t.Fatalf("visits = %v, want each record exactly once", seen)
	}
}

func TestLoadPersistedSessions_ReconcilesEveryTenantOnARoutedStore(t *testing.T) {
	// Boot recovery runs on a context with no principal, so on the routed
	// store it saw the system tenant and nothing else: every tenant's session
	// was neither restored nor concluded.
	store := &routedStore{AgentStore: NewInMemoryAgentStore()}
	backend := &mockAgentBackend{backendType: "mock"}
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("service is %T", svc)
	}
	impl.SetTenantLister(&fakeTenantLister{ids: []string{"t1"}})

	seedSession(t, store, &SessionRecord{
		SessionID: "s-t1-orphan", TenantID: "t1", Backend: "retired", Persistent: true, BackendLocal: true})
	seedSession(t, store, &SessionRecord{
		SessionID: "s-t1-resumable", TenantID: "t1", Persistent: true, BackendLocal: true})

	if err := impl.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	state, reason := storedStatus(t, store, "t1", "s-t1-orphan")
	if state != SessionStateClosed || reason != ConcludedByNodeRestart {
		t.Errorf("s-t1-orphan: status=%q reason=%q, want closed/%s", state, reason, ConcludedByNodeRestart)
	}
	if _, live := impl.liveSessionRecord("s-t1-resumable"); !live {
		t.Error("a tenant's resumable session was not restored into the registry")
	}
}

// =============================================================================
// A remote session is not this daemon's to conclude
// =============================================================================
//
// Its process belongs to another host, which this node's restart did not
// touch. Reading "absent from my local backend map" as "gone" marked a
// still-running remote worker closed and destroyed the addressability an
// operator needs to reach it — go-quicbaas#792's false terminal state,
// inverted. These tests pin both directions.

// remoteSessionHarness wires a service whose LOCAL backend table deliberately
// does NOT serve the remote session's backend, which is the shape that
// produced the bug.
func remoteSessionHarness(t *testing.T) (*agentServiceImpl, AgentStore, AgentRegistry) {
	t.Helper()
	store := NewInMemoryAgentStore()
	svc, err := NewAgentService(store,
		map[string]AgentBackend{"local-only": &mockAgentBackend{backendType: "local-only"}},
		&agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("service is %T", svc)
	}
	registry := NewInMemoryAgentRegistry()
	impl.SetAgentRegistry(registry)
	return impl, store, registry
}

func TestLoadPersistedSessions_ARemoteSessionWhoseDaemonIsRegisteredSurvives(t *testing.T) {
	svc, store, registry := remoteSessionHarness(t)
	ctx := ContextWithTenant(context.Background(), "t1")

	if err := registry.RegisterAgent(ctx, &AgentRegistration{
		ID: "agent-remote", TenantID: "t1", Name: "worker-remote", Host: "10.0.0.7:7000",
		Backend: "remote-acp", AgentType: "worker", Status: AgentStatusIdle,
	}); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	seedSession(t, store, &SessionRecord{
		SessionID: "s-remote-live", TenantID: "t1", Backend: "remote-acp",
		HostAffinity: "worker-remote", Persistent: true, BackendLocal: false})

	if err := svc.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}

	state, reason := storedStatus(t, store, "t1", "s-remote-live")
	if state == SessionStateClosed {
		t.Fatalf("a still-running remote session was concluded %q on restart; its process is on "+
			"another host that this node's restart did not touch, and closing it destroys the "+
			"addressability an operator needs to reach it", reason)
	}
	if _, live := svc.liveSessionRecord("s-remote-live"); !live {
		t.Error("the remote session was not restored into the registry, so it is unaddressable")
	}
}

func TestLoadPersistedSessions_ARemoteSessionWhoseDaemonIsGoneIsConcludedAgentGone(t *testing.T) {
	svc, store, _ := remoteSessionHarness(t)

	// No registration for worker-departed: positive evidence the host left.
	seedSession(t, store, &SessionRecord{
		SessionID: "s-remote-dead", TenantID: "t1", Backend: "remote-acp",
		HostAffinity: "worker-departed", Persistent: true, BackendLocal: false})

	if err := svc.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}

	state, reason := storedStatus(t, store, "t1", "s-remote-dead")
	if state != SessionStateClosed {
		t.Fatalf("status = %q, want closed: the daemon holding the session has left the fleet", state)
	}
	if reason != ConcludedByAgentGone {
		t.Errorf("reason = %q, want %q; attributing another node's departure to this node's "+
			"restart is the wrong fact", reason, ConcludedByAgentGone)
	}
	if _, live := svc.liveSessionRecord("s-remote-dead"); live {
		t.Error("a concluded session was registered as live")
	}
}

func TestLoadPersistedSessions_ARemoteSessionWithNoPinIsLeftAlone(t *testing.T) {
	// No host affinity and no resolver is NO EVIDENCE, and no evidence is not
	// evidence of death. The maintenance sweep revisits it later.
	svc, store, _ := remoteSessionHarness(t)
	seedSession(t, store, &SessionRecord{
		SessionID: "s-remote-unpinned", TenantID: "t1", Backend: "remote-acp",
		Persistent: true, BackendLocal: false})

	if err := svc.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	if state, reason := storedStatus(t, store, "t1", "s-remote-unpinned"); state == SessionStateClosed {
		t.Fatalf("an unpinned remote session was concluded %q on no evidence", reason)
	}
}

func TestLoadPersistedSessions_ARemoteDaemonSilentSinceTheRestartIsNotGone(t *testing.T) {
	// THE BOOT QUESTION IS PRESENCE, NOT RECENCY. Every registration this node
	// can read was written before it restarted, so a live peer necessarily
	// looks silent until it checks in again; concluding on heartbeat age here
	// would close every remote worker's session on every restart.
	svc, store, _ := remoteSessionHarness(t)
	kv := kvstore.NewMemory()
	svc.SetAgentRegistry(NewAgentRegistry(kv))
	svc.SetAgentHeartbeatTimeout(time.Minute)

	seedAgentRecord(t, kv, &AgentRegistration{
		ID: "agent-quiet", TenantID: "t1", Name: "worker-quiet", Host: "10.0.0.8:7000",
		Backend: "remote-acp", AgentType: "worker", Status: AgentStatusIdle,
		LastSeen: time.Now().Add(-6 * time.Hour).UnixNano(),
	})
	seedSession(t, store, &SessionRecord{
		SessionID: "s-remote-quiet", TenantID: "t1", Backend: "remote-acp",
		HostAffinity: "worker-quiet", Persistent: true, BackendLocal: false})

	if err := svc.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	if state, reason := storedStatus(t, store, "t1", "s-remote-quiet"); state == SessionStateClosed {
		t.Fatalf("a remote session was concluded %q because its daemon had not heartbeated since "+
			"before the restart; at boot nothing has heartbeated yet", reason)
	}

	// The SWEEP, by contrast, does ask the stronger question — by then a live
	// daemon has had every chance to speak.
	svc.SetKVStore(kvstore.NewMemory())
	if host, gone := svc.sessionHostGone(context.Background(), &SessionRecord{
		TenantID: "t1", HostAffinity: "worker-quiet"}); !gone || host != "worker-quiet" {
		t.Errorf("sessionHostGone = (%q, %v), want (worker-quiet, true): silence past the timeout "+
			"is absence once the node has been up", host, gone)
	}
}

func TestLoadPersistedSessions_ARemoteSessionTheResolverReachesIsNeverConcluded(t *testing.T) {
	// The resolver is the stronger signal and is consulted first: a backend it
	// resolves is served by a daemon it can reach, whatever the registry says.
	svc, store, _ := remoteSessionHarness(t)
	svc.SetRemoteBackendResolver(func(context.Context, string, string) (AgentBackend, bool) {
		return &mockAgentBackend{backendType: "remote-acp"}, true
	})
	seedSession(t, store, &SessionRecord{
		SessionID: "s-remote-resolved", TenantID: "t1", Backend: "remote-acp",
		HostAffinity: "worker-departed", Persistent: true, BackendLocal: false})

	if err := svc.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	if state, reason := storedStatus(t, store, "t1", "s-remote-resolved"); state == SessionStateClosed {
		t.Fatalf("a remote session the resolver reaches was concluded %q", reason)
	}
}

func TestLoadPersistedSessions_ALocalSessionIsStillConcludedOnRestart(t *testing.T) {
	// The control. Without it a fix that simply stopped concluding anything
	// would pass every test above and reopen go-quicbaas#792.
	svc, store, _ := remoteSessionHarness(t)
	seedSession(t, store, &SessionRecord{
		SessionID: "s-local-orphan", TenantID: "t1", Backend: "retired",
		Persistent: true, BackendLocal: true})

	if err := svc.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	state, reason := storedStatus(t, store, "t1", "s-local-orphan")
	if state != SessionStateClosed || reason != ConcludedByNodeRestart {
		t.Errorf("status=%q reason=%q, want closed/%s", state, reason, ConcludedByNodeRestart)
	}
}

// =============================================================================
// A conclusion is resumable from either half
// =============================================================================
//
// The session record is the COMMIT POINT and is written last, so "closed"
// means "complete". Before that change the store write came first, and a
// `concludeLog` failure behind it left a record saying closed over a log that
// was still open — which no later pass revisited, because every pass reads the
// record, sees closed, and returns. These tests pin the repaired shape: a
// partial conclusion is finished by the next pass, a complete one is a no-op,
// and the job hears about it exactly once however many attempts it took.

// partialConclusionFixture is an ABSENT session — a durable record with no
// live entry, the shape a node restart leaves and the one the half-applied
// conclusion was unrepairable in.
type partialConclusionFixture struct {
	svc     *agentServiceImpl
	store   AgentStore
	log     *recordingSessionLog
	tenant  string
	session string
	job     string
}

func newPartialConclusionFixture(t *testing.T) *partialConclusionFixture {
	t.Helper()
	const tenant, session, job = "t1", "s-partial", "job-partial"

	svc, store := reconcileHarness(t)
	log := newRecordingSessionLog()
	svc.SetSessionEventLog(log)

	seedSession(t, store, &SessionRecord{
		SessionID: session, TenantID: tenant, JobID: job, Backend: "mock"})
	if err := log.PutMeta(context.Background(), &SessionMeta{
		SessionID: session, TenantID: tenant, Status: SessionStateActive,
	}); err != nil {
		t.Fatalf("PutMeta: %v", err)
	}
	seedJob(t, svc, tenant, job, JobStateFailed, session)

	return &partialConclusionFixture{svc: svc, store: store, log: log,
		tenant: tenant, session: session, job: job}
}

// conclude runs one reconciliation pass over the current durable record, the
// way a sweep does: it re-reads rather than reusing a stale copy.
func (f *partialConclusionFixture) conclude(t *testing.T) (bool, error) {
	t.Helper()
	record, err := f.store.GetSession(context.Background(), f.tenant, f.session)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	return f.svc.concludeSessionRecord(context.Background(), record, ConcludedByJobConcluded)
}

// jobState reports what the job records about the session.
func (f *partialConclusionFixture) jobState(t *testing.T) (sessionIDs []string, conclusionLines int) {
	t.Helper()
	status, err := f.svc.loadJobStatus(f.tenant, f.job)
	if err != nil {
		t.Fatalf("loadJobStatus: %v", err)
	}
	for _, e := range status.Events {
		if e.Type == JobAuditTypeSessionConcluded {
			conclusionLines++
		}
	}
	return status.SessionIDs, conclusionLines
}

func TestConcludeSession_AFailedLogWriteLeavesAConclusionTheNextPassCompletes(t *testing.T) {
	f := newPartialConclusionFixture(t)

	// The log refuses the gate write: the conclusion must not commit.
	f.log.failPutMeta(errors.New("log store refused the metadata write"))
	written, err := f.conclude(t)
	if err == nil {
		t.Fatal("a conclusion whose log could not be closed reported success")
	}
	if written {
		t.Fatal("a failed conclusion claimed to have written the ending")
	}

	state, reason := storedStatus(t, f.store, f.tenant, f.session)
	if state == SessionStateClosed {
		t.Fatalf("the record was written closed (%q) over a log that was never closed; that is the "+
			"half-applied conclusion no later pass can repair — every pass reads the record, sees "+
			"closed, and returns", reason)
	}
	if ids, lines := f.jobState(t); len(ids) != 1 || lines != 0 {
		t.Errorf("job says session_ids=%v conclusion_lines=%d; nothing is owed to the job until the "+
			"session's own ending is real", ids, lines)
	}

	// The log recovers. The next pass finds an OPEN record and finishes it.
	f.log.failPutMeta(nil)
	written, err = f.conclude(t)
	if err != nil {
		t.Fatalf("the resuming pass failed: %v", err)
	}
	if !written {
		t.Fatal("the resuming pass skipped the session instead of completing it")
	}

	state, reason = storedStatus(t, f.store, f.tenant, f.session)
	if state != SessionStateClosed || reason != ConcludedByJobConcluded {
		t.Errorf("status=%q reason=%q, want closed/%s", state, reason, ConcludedByJobConcluded)
	}
	if f.log.closed != 1 {
		t.Errorf("terminal events = %d, want exactly 1", f.log.closed)
	}
	meta, err := f.log.GetMeta(context.Background(), f.tenant, f.session)
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if meta.Status != SessionStateClosed {
		t.Errorf("log metadata status = %q, want closed; the gate must be shut behind the ending", meta.Status)
	}
	if ids, lines := f.jobState(t); len(ids) != 0 || lines != 1 {
		t.Errorf("job says session_ids=%v conclusion_lines=%d, want none and exactly 1", ids, lines)
	}
}

func TestConcludeSession_AFailedJobWriteLeavesAConclusionTheNextPassCompletes(t *testing.T) {
	// The job half is inside the transition and ahead of the commit point for
	// the same reason as the log: after it, no retry could reach it.
	f := newPartialConclusionFixture(t)
	f.svc.SetKVStore(&refusingKVStore{KVStore: kvstore.NewMemory()})
	// Seed the job through the refusing store's backing KV so the read works
	// and only the WRITE fails, which is the fault under test.
	seedJob(t, f.svc, f.tenant, f.job, JobStateFailed, f.session)
	f.svc.SetKVStore(&refusingKVStore{KVStore: f.svc.kvStore.(*refusingKVStore).KVStore, refuse: true})

	if _, err := f.conclude(t); err == nil {
		t.Fatal("a conclusion whose job could not be told reported success")
	}
	if state, _ := storedStatus(t, f.store, f.tenant, f.session); state == SessionStateClosed {
		t.Fatal("the record was written closed over a job that was never told; the job would name a " +
			"session that no longer exists, permanently")
	}

	f.svc.SetKVStore(f.svc.kvStore.(*refusingKVStore).KVStore)
	if written, err := f.conclude(t); err != nil || !written {
		t.Fatalf("the resuming pass did not complete the conclusion: written=%v err=%v", written, err)
	}
	if state, _ := storedStatus(t, f.store, f.tenant, f.session); state != SessionStateClosed {
		t.Errorf("status = %q, want closed", state)
	}
	if ids, lines := f.jobState(t); len(ids) != 0 || lines != 1 {
		t.Errorf("job says session_ids=%v conclusion_lines=%d, want none and exactly 1", ids, lines)
	}
}

func TestConcludeSession_ACompleteConclusionIsANoOpOnTheNextPass(t *testing.T) {
	f := newPartialConclusionFixture(t)

	if written, err := f.conclude(t); err != nil || !written {
		t.Fatalf("first pass: written=%v err=%v", written, err)
	}
	idsAfter, linesAfter := f.jobState(t)

	// Two more passes over the same, now-complete session.
	for i := 0; i < 2; i++ {
		written, err := f.conclude(t)
		if err != nil {
			t.Fatalf("repeat pass %d: %v", i, err)
		}
		if written {
			t.Fatalf("repeat pass %d claimed to have written the ending again", i)
		}
	}

	if f.log.closed != 1 {
		t.Errorf("terminal events = %d, want exactly 1; an attacher must not see the session end twice",
			f.log.closed)
	}
	ids, lines := f.jobState(t)
	if lines != 1 || lines != linesAfter {
		t.Errorf("job conclusion lines = %d (was %d), want exactly 1 across every retry", lines, linesAfter)
	}
	if len(ids) != len(idsAfter) {
		t.Errorf("job session_ids changed on a repeat pass: %v -> %v", idsAfter, ids)
	}
	_, reason := storedStatus(t, f.store, f.tenant, f.session)
	if reason != ConcludedByJobConcluded {
		t.Errorf("reason = %q, want the first ending's %q", reason, ConcludedByJobConcluded)
	}
}

// refusingKVStore fails writes, or reads, on demand — a job store that is
// briefly unavailable. Each direction is separately switchable so a test can
// name the one fault it is about.
type refusingKVStore struct {
	interfaces.KVStore
	refuse    bool
	refuseGet bool
}

func (s *refusingKVStore) Put(ctx context.Context, key string, value []byte) error {
	if s.refuse {
		return errors.New("kv store refused the write")
	}
	return s.KVStore.Put(ctx, key, value)
}

func (s *refusingKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	if s.refuseGet {
		return nil, errors.New("kv store could not answer the read")
	}
	return s.KVStore.Get(ctx, key)
}

// =============================================================================
// A job that cannot be READ is not a job that is absent
// =============================================================================
//
// The commit-point ordering only holds while every step before it either
// succeeds or stops the conclusion. Collapsing a failed job-status READ into
// "nothing to tell" let finishDurable walk past the job cleanup and close the
// session anyway — and "closed means complete" then guaranteed no later pass
// would revisit it, so the job pointed at a closed session forever.

func TestConcludeSession_AnUnreadableJobStopsTheConclusionBeforeTheCommit(t *testing.T) {
	f := newPartialConclusionFixture(t)
	backing := f.svc.kvStore
	f.svc.SetKVStore(&refusingKVStore{KVStore: backing, refuseGet: true})

	if _, err := f.conclude(t); err == nil {
		t.Fatal("a conclusion whose job could not be READ reported success")
	}
	if state, _ := storedStatus(t, f.store, f.tenant, f.session); state == SessionStateClosed {
		t.Fatal("the session was closed over a job the store could not answer for; the commit point " +
			"guarantees nothing revisits it, so the job would name a closed session forever")
	}

	// The store answers again: the next pass completes BOTH halves.
	f.svc.SetKVStore(backing)
	if written, err := f.conclude(t); err != nil || !written {
		t.Fatalf("the resuming pass did not complete the conclusion: written=%v err=%v", written, err)
	}
	if state, _ := storedStatus(t, f.store, f.tenant, f.session); state != SessionStateClosed {
		t.Errorf("status = %q, want closed", state)
	}
	if ids, lines := f.jobState(t); len(ids) != 0 || lines != 1 {
		t.Errorf("job says session_ids=%v conclusion_lines=%d, want none and exactly 1", ids, lines)
	}
}

func TestConcludeSession_ACorruptJobRecordIsRetriedNotSwallowed(t *testing.T) {
	f := newPartialConclusionFixture(t)
	// A body that will not unmarshal is a read that FAILED, not an empty
	// keyspace — the same class as an unavailable store.
	if err := f.svc.kvStore.Put(context.Background(),
		jobPersistenceKey(f.tenant, f.job), []byte("{not json")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := f.conclude(t); err == nil {
		t.Fatal("a corrupt job record was swallowed as 'nothing to tell'")
	}
	if state, _ := storedStatus(t, f.store, f.tenant, f.session); state == SessionStateClosed {
		t.Fatal("the session was closed over a job record that could not be parsed")
	}

	// Repair the record; the next pass finishes the conclusion.
	seedJob(t, f.svc, f.tenant, f.job, JobStateFailed, f.session)
	if written, err := f.conclude(t); err != nil || !written {
		t.Fatalf("the resuming pass did not complete the conclusion: written=%v err=%v", written, err)
	}
	if ids, lines := f.jobState(t); len(ids) != 0 || lines != 1 {
		t.Errorf("job says session_ids=%v conclusion_lines=%d, want none and exactly 1", ids, lines)
	}
}

func TestConcludeSession_AReapedJobIsVacuouslyComplete(t *testing.T) {
	// The other side of the distinction: a job record that genuinely is not
	// there has nothing to tell and nothing to retry. Blocking on it would
	// strand every session whose job has been reaped — the stranded state in
	// the other direction.
	f := newPartialConclusionFixture(t)
	if err := f.svc.kvStore.Delete(context.Background(),
		jobPersistenceKey(f.tenant, f.job)); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	written, err := f.conclude(t)
	if err != nil {
		t.Fatalf("a session whose job has been reaped could not be concluded: %v", err)
	}
	if !written {
		t.Fatal("the conclusion reported that it wrote nothing")
	}
	state, reason := storedStatus(t, f.store, f.tenant, f.session)
	if state != SessionStateClosed || reason != ConcludedByJobConcluded {
		t.Errorf("status=%q reason=%q, want closed/%s", state, reason, ConcludedByJobConcluded)
	}
}

func TestJobStatusAbsent(t *testing.T) {
	// The not-found is taken FROM THE STORE rather than constructed, so the
	// test asserts against the shape a real missing key produces rather than
	// against one it assumes.
	t.Run("a not-found cause is absence", func(t *testing.T) {
		_, missing := kvstore.NewMemory().Get(context.Background(), "no-such-key")
		if missing == nil {
			t.Fatal("premise: reading an absent key must report something")
		}
		if !qerrors.IsNotFound(missing) {
			t.Fatalf("premise: the memory store's missing-key error is not a not-found: %#v", missing)
		}
		if !jobStatusAbsent(&JobPersistenceError{
			JobID: "j", Message: "failed to read from KV store", Cause: missing,
		}) {
			t.Error("a KV not-found must read as absence: the record is genuinely not there")
		}
	})

	t.Run("every other failure is not absence", func(t *testing.T) {
		for name, err := range map[string]error{
			"unavailable": &JobPersistenceError{JobID: "j", Message: "read failed",
				Cause: errors.New("kv store could not answer the read")},
			"unmarshal": &JobPersistenceError{JobID: "j", Message: "failed to unmarshal job status",
				Cause: errors.New("invalid character 'n'")},
			"no cause":  &JobPersistenceError{JobID: "j", Message: "KV store not configured"},
			"unrelated": errors.New("something else entirely"),
			"nil":       nil,
		} {
			if jobStatusAbsent(err) {
				t.Errorf("%s: read as absence; a store that failed to ANSWER is not an empty keyspace", name)
			}
		}
	})
}

// =============================================================================
// The status filter
// =============================================================================
//
// The listing MERGES durable records with live ones. While it effectively
// listed live sessions the absent filter was invisible; once reconciliation
// began writing terminal records, a caller asking for ACTIVE sessions was
// handed CLOSED ones back. The SDK had been sending ?status= the whole time.

// statusFilterFixture seeds one session in each of the states a listing can
// report, live and durable.
func statusFilterFixture(t *testing.T) (*agentServiceImpl, context.Context) {
	t.Helper()
	svc, store := reconcileHarness(t)
	ctx := ContextWithTenant(context.Background(), "t1")

	seedSession(t, store, &SessionRecord{SessionID: "s-active", TenantID: "t1"})
	seedSession(t, store, &SessionRecord{
		SessionID: "s-closed", TenantID: "t1", Status: SessionStateClosed,
		ConclusionReason: ConcludedByNodeRestart})
	seedSession(t, store, &SessionRecord{
		SessionID: "s-idle", TenantID: "t1", Status: SessionStateIdle})
	return svc, ctx
}

// listedIDs returns the session IDs a listing reported, sorted.
func listedIDs(t *testing.T, svc *agentServiceImpl, ctx context.Context, req *ListSessionsRequest) []string {
	t.Helper()
	resp, err := svc.ListSessions(ctx, req)
	if err != nil {
		t.Fatalf("ListSessions(%+v): %v", req, err)
	}
	ids := make([]string, 0, len(resp.Sessions))
	for _, s := range resp.Sessions {
		ids = append(ids, s.SessionID)
	}
	sort.Strings(ids)
	return ids
}

func TestListSessions_StatusFilter(t *testing.T) {
	svc, ctx := statusFilterFixture(t)

	for _, tc := range []struct {
		name   string
		status SessionState
		want   []string
	}{
		{"active excludes the closed record", SessionStateActive, []string{"s-active"}},
		{"closed returns only closed", SessionStateClosed, []string{"s-closed"}},
		{"idle returns only idle", SessionStateIdle, []string{"s-idle"}},
		{"empty means every state", "", []string{"s-active", "s-closed", "s-idle"}},
		{"a state nothing is in returns nothing", SessionStateError, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := listedIDs(t, svc, ctx, &ListSessionsRequest{Status: tc.status})
			if len(got) != len(tc.want) {
				t.Fatalf("listed %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("listed %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestListSessions_StatusFilterAppliesToTheLiveBranch(t *testing.T) {
	// The live map's entry is reported ACTIVE unless the durable record says
	// closed, so the filter has to run AFTER that verdict — filtering earlier
	// would answer for a state the listing does not report.
	svc, store := reconcileHarness(t)
	ctx := ContextWithTenant(context.Background(), "t1")

	seedSession(t, store, &SessionRecord{SessionID: "s-live", TenantID: "t1"})
	seedSession(t, store, &SessionRecord{
		SessionID: "s-live-but-closed", TenantID: "t1", Status: SessionStateClosed})
	svc.sessions.Store("s-live", &sessionRecord{Backend: "mock", TenantID: "t1"})
	svc.sessions.Store("s-live-but-closed", &sessionRecord{Backend: "mock", TenantID: "t1"})

	active := listedIDs(t, svc, ctx, &ListSessionsRequest{Status: SessionStateActive})
	if len(active) != 1 || active[0] != "s-live" {
		t.Errorf("status=active listed %v, want [s-live]; a live entry whose durable record is "+
			"closed reports closed and must not answer an active filter", active)
	}
	closed := listedIDs(t, svc, ctx, &ListSessionsRequest{Status: SessionStateClosed})
	if len(closed) != 1 || closed[0] != "s-live-but-closed" {
		t.Errorf("status=closed listed %v, want [s-live-but-closed]", closed)
	}
}

func TestListSessions_StatusFilterCombinesWithBackend(t *testing.T) {
	svc, store := reconcileHarness(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	seedSession(t, store, &SessionRecord{SessionID: "s-mock", TenantID: "t1", Backend: "mock"})
	seedSession(t, store, &SessionRecord{SessionID: "s-other", TenantID: "t1", Backend: "other"})

	got := listedIDs(t, svc, ctx, &ListSessionsRequest{Backend: "mock", Status: SessionStateActive})
	if len(got) != 1 || got[0] != "s-mock" {
		t.Errorf("listed %v, want [s-mock]; the two filters narrow together", got)
	}
}

func TestListSessions_RefusesAnUnknownStatus(t *testing.T) {
	// A filter the caller set and the engine silently misapplied reads as
	// "there are no such sessions", which is the wrong answer to a question
	// that was never understood.
	svc, ctx := statusFilterFixture(t)

	_, err := svc.ListSessions(ctx, &ListSessionsRequest{Status: "acive"})
	if err == nil {
		t.Fatal("a misspelled state was accepted and matched nothing")
	}
	if !errors.Is(err, ErrSessionStateUnknown) {
		t.Errorf("error = %v, want ErrSessionStateUnknown", err)
	}
	if !strings.Contains(err.Error(), "active") {
		t.Errorf("error = %v, want it to name the states that ARE known", err)
	}
}

func TestSessionState_Valid(t *testing.T) {
	for _, state := range []SessionState{
		SessionStateActive, SessionStateIdle, SessionStateResumable,
		SessionStateClosed, SessionStateError,
	} {
		if !state.Valid() {
			t.Errorf("state %q reports invalid", state)
		}
	}
	for _, state := range []SessionState{"", "running", "ACTIVE"} {
		if state.Valid() {
			t.Errorf("state %q reports valid", state)
		}
	}
	if got := SessionStates(); len(got) != 5 || got[0] != "active" {
		t.Errorf("SessionStates() = %v, want the five states sorted", got)
	}
}

// =============================================================================
// The list names the job a session works for
// =============================================================================

// TestListSessions_CarriesTheJobBinding proves the LISTING names each
// session's job, on the durable half and the live half alike.
//
// THE ROLE BADGE DEPENDS ON IT. A session record says nothing about what the
// agent was doing, and a worker transcript discussing "the review's findings"
// reads exactly like a reviewer's (go-quicbaas#969). The role is the runner
// of the session's job, so a listing that does not name the job cannot
// resolve one — the badge was blank for every row until this field existed.
func TestListSessions_CarriesTheJobBinding(t *testing.T) {
	svc, store := reconcileHarness(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	seedSession(t, store, &SessionRecord{
		SessionID: "s-durable", TenantID: "t1", JobID: "job-review",
		CreatedAt: time.Now().Add(-time.Hour).UnixNano(),
	})
	seedSession(t, store, &SessionRecord{
		SessionID: "s-live", TenantID: "t1", JobID: "job-worker",
		CreatedAt: time.Now().Add(-time.Minute).UnixNano(),
	})
	svc.sessions.Store("s-live", &sessionRecord{
		Backend: "mock", TenantID: "t1", JobID: "job-worker",
	})

	resp, err := svc.ListSessions(ctx, &ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	bindings := make(map[string]string, len(resp.Sessions))
	for _, session := range resp.Sessions {
		bindings[session.SessionID] = session.JobID
	}
	if got := bindings["s-durable"]; got != "job-review" {
		t.Errorf("durable session job_id = %q, want %q", got, "job-review")
	}
	if got := bindings["s-live"]; got != "job-worker" {
		t.Errorf("live session job_id = %q, want %q", got, "job-worker")
	}
}

// TestListSessions_NamesTheRunnerBehindTheSession proves the listing resolves
// each row's ROLE — the runner of the job it names — so a consumer holding
// one page never has to go looking for the job.
//
// A worker's transcript discussing "the review's findings" reads exactly like
// a reviewer's, and the owner drew that conclusion from one (go-quicbaas#969).
func TestListSessions_NamesTheRunnerBehindTheSession(t *testing.T) {
	svc, store := reconcileHarness(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	seedJobStatus(t, svc, "t1", &JobStatus{JobID: "job-review", Runner: "review", State: JobStateCompleted})
	seedJobStatus(t, svc, "t1", &JobStatus{JobID: "job-legacy", State: JobStateCompleted})
	for _, seed := range []struct{ session, job string }{
		{"s-reviewer", "job-review"},
		{"s-workflow", "job-legacy"},
		{"s-orphan", "job-pruned"},
		{"s-none", ""},
	} {
		seedSession(t, store, &SessionRecord{
			SessionID: seed.session, TenantID: "t1", JobID: seed.job,
			CreatedAt: time.Now().Add(-time.Hour).UnixNano(),
		})
	}

	resp, err := svc.ListSessions(ctx, &ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	runners := make(map[string]string, len(resp.Sessions))
	for _, session := range resp.Sessions {
		runners[session.SessionID] = session.Runner
	}

	tests := []struct {
		session string
		want    string
		why     string
	}{
		{"s-reviewer", "review", "the runner its job recorded"},
		{"s-workflow", JobRunnerWorkflow, "a job naming no runner is the engine's own"},
		{"s-orphan", "", "a job nobody can read is no role rather than a guess"},
		{"s-none", "", "a session belonging to no job plays no role"},
	}
	for _, tc := range tests {
		if got := runners[tc.session]; got != tc.want {
			t.Errorf("%s runner = %q, want %q — %s", tc.session, got, tc.want, tc.why)
		}
	}
}

// seedJobStatus persists one job record so a session naming it can resolve a
// runner, the way a concluded job's record is the only thing left of it.
func seedJobStatus(t *testing.T, svc *agentServiceImpl, tenantID string, status *JobStatus) {
	t.Helper()
	if err := svc.storeJobStatus(tenantID, status); err != nil {
		t.Fatalf("persisting job %s: %v", status.JobID, err)
	}
}

// TestListSessions_ALiveSessionWithNoBindingReadsItFromTheStore proves a
// session reconstituted after a restart — live entry rebuilt without the
// binding — still names its job, and that a session belonging to no job
// names none rather than inventing one.
func TestListSessions_ALiveSessionWithNoBindingReadsItFromTheStore(t *testing.T) {
	svc, store := reconcileHarness(t)
	ctx := ContextWithTenant(context.Background(), "t1")
	seedSession(t, store, &SessionRecord{
		SessionID: "s-reconstituted", TenantID: "t1", JobID: "job-triage",
		CreatedAt: time.Now().Add(-time.Hour).UnixNano(),
	})
	seedSession(t, store, &SessionRecord{
		SessionID: "s-unbound", TenantID: "t1",
		CreatedAt: time.Now().Add(-time.Hour).UnixNano(),
	})
	svc.sessions.Store("s-reconstituted", &sessionRecord{Backend: "mock", TenantID: "t1"})

	resp, err := svc.ListSessions(ctx, &ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	for _, session := range resp.Sessions {
		switch session.SessionID {
		case "s-reconstituted":
			if session.JobID != "job-triage" {
				t.Errorf("job_id = %q, want %q from the durable record", session.JobID, "job-triage")
			}
		case "s-unbound":
			if session.JobID != "" {
				t.Errorf("job_id = %q for a session that belongs to no job, want empty", session.JobID)
			}
		}
	}
}
