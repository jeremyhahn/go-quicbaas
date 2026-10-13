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
	"strconv"
	"strings"
	"testing"
	"time"
)

// listingStore runs afterList once the sweep has listed the records, so a test
// can change the stored record while the sweep holds a copy that is already
// stale — the window in which a session becomes active again. getErr, when
// set, is what every GetSession answers.
type listingStore struct {
	AgentStore
	afterList func()
	getErr    error
}

func (s *listingStore) ListAllSessions(ctx context.Context) ([]*SessionRecord, error) {
	records, err := s.AgentStore.ListAllSessions(ctx)
	if err == nil && s.afterList != nil {
		s.afterList()
	}
	return records, err
}

func (s *listingStore) GetSession(ctx context.Context, tenantID, sessionID string) (*SessionRecord, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.AgentStore.GetSession(ctx, tenantID, sessionID)
}

// joblessHarness builds a service over a listingStore holding one job-less
// session that has been quiet past the idle bound.
func joblessHarness(t *testing.T) (*agentServiceImpl, *listingStore) {
	t.Helper()
	store := &listingStore{AgentStore: NewInMemoryAgentStore()}
	svc, err := NewAgentService(store, map[string]AgentBackend{
		"mock": &mockAgentBackend{backendType: "mock"},
	}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("NewAgentService returned %T, want *agentServiceImpl", svc)
	}
	impl.SetKVStore(NewMemoryTaskKV())
	seedSession(t, store.AgentStore, &SessionRecord{
		SessionID:    "s-jobless",
		TenantID:     "t1",
		Status:       SessionStateIdle,
		Persistent:   true,
		BackendLocal: true,
		LastActivity: time.Now().Add(-(impl.idleThresholds.longest() + time.Hour)).UnixNano(),
	})
	return impl, store
}

func TestJoblessEvidence_NamesTheQuietTimeTheBoundAndTheMissingJob(t *testing.T) {
	got := joblessEvidence(59*time.Hour+55*time.Minute+400*time.Millisecond, 3*time.Hour)
	want := "session has been quiet for 59h55m0s, past the idle bound of 3h0m0s, and is bound to no job"
	if got != want {
		t.Errorf("evidence = %q, want %q", got, want)
	}
}

func TestSweepSessions_JoblessFindingCarriesItsEvidence(t *testing.T) {
	svc, _ := joblessHarness(t)
	bound := svc.idleThresholds.longest()

	report, err := svc.SweepSessions(context.Background())
	if err != nil {
		t.Fatalf("SweepSessions: %v", err)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("findings = %+v, want one", report.Findings)
	}
	finding := report.Findings[0]
	if finding.Verdict != SessionReconcileConcluded || finding.Reason != ConcludedBySessionIdle {
		t.Errorf("finding = %s/%s, want %s/%s", finding.Verdict, finding.Reason,
			SessionReconcileConcluded, ConcludedBySessionIdle)
	}
	if !strings.HasPrefix(finding.Evidence, "session has been quiet for ") ||
		!strings.HasSuffix(finding.Evidence, ", past the idle bound of "+bound.String()+", and is bound to no job") {
		t.Errorf("evidence = %q, want the quiet time, the bound and the missing job", finding.Evidence)
	}
}

func TestSessionRecord_KeepRoundTripsThroughTheStore(t *testing.T) {
	store := NewInMemoryAgentStore()
	seedSession(t, store, &SessionRecord{SessionID: "s-kept", TenantID: "t1", Keep: true})

	stored, err := store.GetSession(context.Background(), "t1", "s-kept")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !stored.Keep {
		t.Error("the keep marker did not survive the store")
	}
	if !stored.Clone().Keep {
		t.Error("the keep marker did not survive Clone")
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"keep":true`) {
		t.Errorf("encoded record %s does not carry the keep marker", encoded)
	}
}

func TestSessionRecord_OlderRecordDecodesAsNotKept(t *testing.T) {
	// The vintage before the marker, with a project NAME where the id belongs.
	record, err := decodeSessionRecord([]byte(
		`{"session_id":"s-old","tenant_id":"t1","project_id":"billing-service","status":"idle"}`))
	if err != nil {
		t.Fatalf("decodeSessionRecord: %v", err)
	}
	if record.Keep {
		t.Error("a record that never said keep decoded as kept")
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(encoded), `"keep"`) {
		t.Errorf("encoded record %s writes a keep marker nobody set", encoded)
	}
}

// TestSweepSessions_JoblessVerdictIsDecidedByTheStoredRecord is the race: the
// sweep lists the session quiet and unkept, and the stored record changes
// before the ending is written. The stored record decides, so none of these
// is concluded.
func TestSweepSessions_JoblessVerdictIsDecidedByTheStoredRecord(t *testing.T) {
	cases := []struct {
		name   string
		change func(t *testing.T, store AgentStore)
	}{
		{"prompted again", func(t *testing.T, store AgentStore) {
			if err := store.UpdateSessionActivity(context.Background(), "t1", "s-jobless"); err != nil {
				t.Errorf("UpdateSessionActivity: %v", err)
			}
		}},
		{"marked kept", func(t *testing.T, store AgentStore) {
			mutateStored(t, store, func(record *SessionRecord) { record.Keep = true })
		}},
		{"bound to a job", func(t *testing.T, store AgentStore) {
			mutateStored(t, store, func(record *SessionRecord) { record.JobID = "j-late" })
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store := joblessHarness(t)
			store.afterList = func() { tc.change(t, store.AgentStore) }

			report, err := svc.SweepSessions(context.Background())
			if err != nil {
				t.Fatalf("SweepSessions: %v", err)
			}
			if len(report.Findings) != 0 {
				t.Errorf("findings = %+v, want none: the stored record no longer meets the verdict",
					report.Findings)
			}
			stored, err := store.AgentStore.GetSession(context.Background(), "t1", "s-jobless")
			if err != nil {
				t.Fatalf("GetSession: %v", err)
			}
			if stored.Status == SessionStateClosed {
				t.Errorf("the session was concluded on the listed copy (%s)", stored.ConclusionReason)
			}
		})
	}
}

func TestSweepSessions_JoblessVerdictFailsWhenTheStoreCannotAnswer(t *testing.T) {
	svc, store := joblessHarness(t)
	store.getErr = &AgentStoreError{Sentinel: ErrAgentStoreUnavailable, Message: "store is down"}

	report, err := svc.SweepSessions(context.Background())
	if err != nil {
		t.Fatalf("SweepSessions: %v", err)
	}
	if got := report.Count(SessionReconcileFailed); got != 1 {
		t.Fatalf("failed findings = %d, want 1: %+v", got, report.Findings)
	}
	if report.Findings[0].Reason != "" {
		t.Errorf("a failed finding claims reason %q", report.Findings[0].Reason)
	}

	store.getErr = nil
	stored, err := store.GetSession(context.Background(), "t1", "s-jobless")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.Status != SessionStateIdle {
		t.Errorf("status = %q, want idle: an unanswered re-read concludes nothing", stored.Status)
	}
}

// mutateStored rewrites the stored s-jobless record through apply.
func mutateStored(t *testing.T, store AgentStore, apply func(*SessionRecord)) {
	t.Helper()
	record, err := store.GetSession(context.Background(), "t1", "s-jobless")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	apply(record)
	if err := store.PutSession(context.Background(), record); err != nil {
		t.Fatalf("PutSession: %v", err)
	}
}

// TestSpawnSession_KeepIsSetAtSpawnAndHoldsAgainstTheSweep spawns two job-less
// sessions, one asking to be kept and one not, quiets both past the idle
// bound, and sweeps: the spawn's keep is the record's, and only the unkept
// session is concluded.
func TestSpawnSession_KeepIsSetAtSpawnAndHoldsAgainstTheSweep(t *testing.T) {
	store := NewInMemoryAgentStore()
	spawned := 0
	backend := &mockAgentBackend{
		backendType: "mock",
		spawnFn: func(_ context.Context, req *SpawnRequest) (*Session, error) {
			spawned++
			return &Session{ID: "s-spawned-" + strconv.Itoa(spawned), ProjectID: req.ProjectID, Status: SessionStateActive}, nil
		},
	}
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("NewAgentService returned %T, want *agentServiceImpl", svc)
	}
	impl.SetKVStore(NewMemoryTaskKV())
	ctx := ContextWithTenant(context.Background(), "t1")

	kept, err := svc.SpawnSession(ctx, &SpawnSessionRequest{Backend: "mock", Persistent: true, Keep: true})
	if err != nil {
		t.Fatalf("SpawnSession(keep): %v", err)
	}
	unkept, err := svc.SpawnSession(ctx, &SpawnSessionRequest{Backend: "mock", Persistent: true})
	if err != nil {
		t.Fatalf("SpawnSession(default): %v", err)
	}

	quiet := time.Now().Add(-(impl.idleThresholds.longest() + time.Hour)).UnixNano()
	records := storedSessionsByID(t, store)
	for id, record := range records {
		if want := id == kept.SessionID; record.Keep != want {
			t.Fatalf("session %s spawned with keep=%v stored keep=%v", id, want, record.Keep)
		}
		record.Status = SessionStateIdle
		record.LastActivity = quiet
		if err := store.PutSession(context.Background(), record); err != nil {
			t.Fatalf("PutSession: %v", err)
		}
	}

	report, err := impl.SweepSessions(context.Background())
	if err != nil {
		t.Fatalf("SweepSessions: %v", err)
	}
	if len(report.Findings) != 1 || report.Findings[0].SessionID != unkept.SessionID ||
		report.Findings[0].Reason != ConcludedBySessionIdle {
		t.Fatalf("findings = %+v, want the unkept session concluded as %s", report.Findings, ConcludedBySessionIdle)
	}
	records = storedSessionsByID(t, store)
	if records[kept.SessionID].Status == SessionStateClosed {
		t.Error("the session spawned with keep was concluded")
	}
	if records[unkept.SessionID].Status != SessionStateClosed {
		t.Errorf("unkept session status = %s, want %s", records[unkept.SessionID].Status, SessionStateClosed)
	}
}

// storedSessionsByID is every stored session record keyed by its ID.
func storedSessionsByID(t *testing.T, store AgentStore) map[string]*SessionRecord {
	t.Helper()
	records, err := store.ListAllSessions(context.Background())
	if err != nil {
		t.Fatalf("ListAllSessions: %v", err)
	}
	byID := make(map[string]*SessionRecord, len(records))
	for _, record := range records {
		byID[record.SessionID] = record
	}
	return byID
}
