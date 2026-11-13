package orchestration

import (
	"context"
	"errors"
	"testing"
)

// repairRestart is a daemon started over a store and a session log that an
// earlier daemon left behind: nothing is live, and only the durable state says
// what happened.
func repairRestart(t *testing.T, store AgentStore, log *recordingSessionLog) *agentServiceImpl {
	t.Helper()
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": &countingBackend{}}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetSessionEventLog(log)
	return impl
}

// seedClosedRecord writes a CLOSED durable record and, when meta is non-nil,
// the session log metadata a failed log transition left behind.
func seedClosedRecord(t *testing.T, store AgentStore, log *recordingSessionLog,
	sessionID string, reason SessionConclusionReason, meta SessionState) string {
	t.Helper()
	ctx := context.Background()
	tenant := StorageTenant(ctx)
	if err := store.PutSession(ctx, &SessionRecord{
		SessionID: sessionID, TenantID: tenant, Backend: "mock", Persistent: true,
		Status: SessionStateClosed, ConclusionReason: reason,
	}); err != nil {
		t.Fatalf("PutSession: %v", err)
	}
	if meta != "" {
		if err := log.PutMeta(ctx, &SessionMeta{
			SessionID: sessionID, TenantID: tenant, Status: meta,
		}); err != nil {
			t.Fatalf("PutMeta: %v", err)
		}
	}
	return tenant
}

// TestLoadPersistedSessions_RepairsUnendedLogOfClosedRecord is #201's
// acceptance: a daemon restarted over a CLOSED record whose log has no ending
// writes the ending, under the record's reason, with the gate shut — whether
// the failed transition left the gate ACTIVE or shut it and lost the ending.
func TestLoadPersistedSessions_RepairsUnendedLogOfClosedRecord(t *testing.T) {
	t.Parallel()
	for name, meta := range map[string]SessionState{
		"gate still active":      SessionStateActive,
		"gate shut and no event": SessionStateClosed,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := NewInMemoryAgentStore()
			log := newRecordingSessionLog()
			seedClosedRecord(t, store, log, "s-unended", ConcludedByOperator, meta)

			svc := repairRestart(t, store, log)
			if err := svc.LoadPersistedSessions(context.Background()); err != nil {
				t.Fatalf("LoadPersistedSessions: %v", err)
			}

			if got := log.lastKind("s-unended"); got != KindSessionClosed {
				t.Fatalf("last event after restart = %q, want %q", got, KindSessionClosed)
			}
			if reason, ok := log.terminalReason("s-unended"); !ok || reason != ConcludedByOperator.String() {
				t.Errorf("ending reason = %q (present %v), want the record's %q", reason, ok, ConcludedByOperator)
			}
			if got := log.status("s-unended"); got != SessionStateClosed {
				t.Errorf("log metadata = %q, want %q", got, SessionStateClosed)
			}
			if _, live := svc.liveSessionRecord("s-unended"); live {
				t.Error("a closed record was restored as a live session")
			}

			// A second restart finds the ending and writes no other.
			if err := repairRestart(t, store, log).LoadPersistedSessions(context.Background()); err != nil {
				t.Fatalf("second LoadPersistedSessions: %v", err)
			}
			if got := log.eventCount("s-unended"); got != 1 {
				t.Errorf("events after a second restart = %d, want the one ending", got)
			}
		})
	}
}

// TestLoadPersistedSessions_RepairAttributesUnnamedConclusion: a closed record
// that names no reason still gets its ending, attributed to reconcile.
func TestLoadPersistedSessions_RepairAttributesUnnamedConclusion(t *testing.T) {
	t.Parallel()
	store := NewInMemoryAgentStore()
	log := newRecordingSessionLog()
	seedClosedRecord(t, store, log, "s-unnamed", "", SessionStateActive)

	if err := repairRestart(t, store, log).LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	if reason, ok := log.terminalReason("s-unnamed"); !ok || reason != ConcludedByReconcile.String() {
		t.Errorf("ending reason = %q (present %v), want %q", reason, ok, ConcludedByReconcile)
	}
}

// TestLoadPersistedSessions_ClosedRecordWithoutLogIsLeftAlone: a session the
// log never registered has nothing to end, and the repair writes nothing.
func TestLoadPersistedSessions_ClosedRecordWithoutLogIsLeftAlone(t *testing.T) {
	t.Parallel()
	store := NewInMemoryAgentStore()
	log := newRecordingSessionLog()
	seedClosedRecord(t, store, log, "s-unlogged", ConcludedByOperator, "")

	if err := repairRestart(t, store, log).LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	if got := log.eventCount("s-unlogged"); got != 0 {
		t.Errorf("events = %d, want none for an unregistered log", got)
	}
}

// TestLoadPersistedSessions_FailedRepairDoesNotStopBoot: a log that cannot
// answer leaves the closed record for a later repair, and the boot completes.
func TestLoadPersistedSessions_FailedRepairDoesNotStopBoot(t *testing.T) {
	t.Parallel()
	store := NewInMemoryAgentStore()
	log := newRecordingSessionLog()
	seedClosedRecord(t, store, log, "s-unreadable", ConcludedByOperator, SessionStateActive)
	log.failGetMeta(errors.New("log unavailable"))

	if err := repairRestart(t, store, log).LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions = %v, want the boot to complete", err)
	}
	if got := log.eventCount("s-unreadable"); got != 0 {
		t.Errorf("events = %d, want none while the log cannot answer", got)
	}
}

// TestCloseSession_ClosedRecordRepairsUnendedLog: CloseSession on a closed
// record is satisfied only once the log agrees, so a repair that a boot could
// not do is done by the next operator close — and refused while it cannot be.
func TestCloseSession_ClosedRecordRepairsUnendedLog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewInMemoryAgentStore()
	log := newRecordingSessionLog()
	seedClosedRecord(t, store, log, "s-close-repair", ConcludedByProcessExit, SessionStateActive)
	svc := repairRestart(t, store, log)

	log.failGetMeta(errors.New("log unavailable"))
	if _, err := svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-close-repair"}); !errors.Is(err, ErrAgentServiceUnavailable) {
		t.Fatalf("CloseSession over an unreadable log = %v, want ErrAgentServiceUnavailable", err)
	}
	log.failGetMeta(nil)

	if _, err := svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-close-repair"}); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if reason, ok := log.terminalReason("s-close-repair"); !ok || reason != ConcludedByProcessExit.String() {
		t.Errorf("ending reason = %q (present %v), want the record's %q", reason, ok, ConcludedByProcessExit)
	}
	if _, err := svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-close-repair"}); err != nil {
		t.Fatalf("idempotent CloseSession: %v", err)
	}
	if got := log.eventCount("s-close-repair"); got != 1 {
		t.Errorf("events = %d, want the one ending", got)
	}
}
