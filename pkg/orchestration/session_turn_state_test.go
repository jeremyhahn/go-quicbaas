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
	"errors"
	"strconv"
	"testing"
	"time"
)

// errTurnFailed is the failure a test backend answers a turn with.
var errTurnFailed = errors.New("turn failed")

// turnStateHarness builds a service whose mock backend spawns sessions and
// answers turns with prompt.
func turnStateHarness(
	t *testing.T, prompt func(context.Context, *PromptRequest) (*PromptResponse, error),
) (*agentServiceImpl, AgentStore) {
	t.Helper()
	store := NewInMemoryAgentStore()
	spawned := 0
	backend := &mockAgentBackend{
		backendType: "mock",
		spawnFn: func(_ context.Context, req *SpawnRequest) (*Session, error) {
			spawned++
			return &Session{ID: "s-turn-" + strconv.Itoa(spawned), ProjectID: req.ProjectID, Status: SessionStateActive}, nil
		},
		promptFn: prompt,
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
	return impl, store
}

// answerTurn is a backend turn that succeeds.
func answerTurn(_ context.Context, req *PromptRequest) (*PromptResponse, error) {
	return &PromptResponse{SessionID: req.SessionID, Content: []ContentBlock{{Type: "text", Text: "ok"}}}, nil
}

// spawnTurnSession spawns a persistent session bound to no job.
func spawnTurnSession(t *testing.T, svc *agentServiceImpl, keep bool) string {
	t.Helper()
	resp, err := svc.SpawnSession(turnStateContext(), &SpawnSessionRequest{Backend: "mock", Persistent: true, Keep: keep})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	return resp.SessionID
}

func turnStateContext() context.Context {
	return ContextWithTenant(context.Background(), "t1")
}

// storedTurnSession returns a session's durable record.
func storedTurnSession(t *testing.T, store AgentStore, sessionID string) *SessionRecord {
	t.Helper()
	record, ok := storedSessionsByID(t, store)[sessionID]
	if !ok {
		t.Fatalf("session %s has no durable record", sessionID)
	}
	return record
}

// quietenTurnSession backdates a session's activity past the sweep's idle
// bound, leaving its status as it is.
func quietenTurnSession(t *testing.T, svc *agentServiceImpl, store AgentStore, sessionID string) {
	t.Helper()
	record := storedTurnSession(t, store, sessionID)
	record.LastActivity = time.Now().Add(-(svc.idleThresholds.longest() + time.Hour)).UnixNano()
	if err := store.PutSession(context.Background(), record); err != nil {
		t.Fatalf("PutSession: %v", err)
	}
}

// sweepTurnSessions sweeps and returns the sessions it concluded.
func sweepTurnSessions(t *testing.T, svc *agentServiceImpl) map[string]SessionConclusionReason {
	t.Helper()
	report, err := svc.SweepSessions(context.Background())
	if err != nil {
		t.Fatalf("SweepSessions: %v", err)
	}
	concluded := make(map[string]SessionConclusionReason, len(report.Findings))
	for _, finding := range report.Findings {
		concluded[finding.SessionID] = finding.Reason
	}
	return concluded
}

func TestSweepSessions_ReapsASpawnedJoblessSessionGoneQuiet(t *testing.T) {
	svc, store := turnStateHarness(t, answerTurn)
	sessionID := spawnTurnSession(t, svc, false)
	if _, err := svc.PromptSession(turnStateContext(), &PromptSessionRequest{SessionID: sessionID, Prompt: "hi"}); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}
	if got := storedTurnSession(t, store, sessionID).Status; got != SessionStateIdle {
		t.Fatalf("status after the turn = %s, want %s", got, SessionStateIdle)
	}
	quietenTurnSession(t, svc, store, sessionID)

	if got := sweepTurnSessions(t, svc)[sessionID]; got != ConcludedBySessionIdle {
		t.Fatalf("sweep concluded the quiet jobless session as %q, want %q", got, ConcludedBySessionIdle)
	}
	if got := storedTurnSession(t, store, sessionID).Status; got != SessionStateClosed {
		t.Errorf("status after the sweep = %s, want %s", got, SessionStateClosed)
	}
}

func TestSweepSessions_ReapsASpawnedSessionNeverPrompted(t *testing.T) {
	svc, store := turnStateHarness(t, answerTurn)
	sessionID := spawnTurnSession(t, svc, false)
	quietenTurnSession(t, svc, store, sessionID)

	if got := sweepTurnSessions(t, svc)[sessionID]; got != ConcludedBySessionIdle {
		t.Fatalf("sweep concluded the never-prompted session as %q, want %q", got, ConcludedBySessionIdle)
	}
}

func TestSweepSessions_SparesASessionWithATurnInFlight(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	svc, store := turnStateHarness(t, func(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
		close(entered)
		<-release
		return answerTurn(ctx, req)
	})
	sessionID := spawnTurnSession(t, svc, false)

	done := make(chan error, 1)
	go func() {
		_, err := svc.PromptSession(turnStateContext(), &PromptSessionRequest{SessionID: sessionID, Prompt: "hi"})
		done <- err
	}()
	<-entered
	if got := storedTurnSession(t, store, sessionID).Status; got != SessionStateActive {
		t.Errorf("status during the turn = %s, want %s", got, SessionStateActive)
	}
	quietenTurnSession(t, svc, store, sessionID)
	concluded := sweepTurnSessions(t, svc)
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("PromptSession: %v", err)
	}

	if reason, ok := concluded[sessionID]; ok {
		t.Fatalf("sweep concluded a session with a turn in flight as %q", reason)
	}
	if got := storedTurnSession(t, store, sessionID).Status; got != SessionStateIdle {
		t.Errorf("status after the turn = %s, want %s", got, SessionStateIdle)
	}
}

func TestSweepSessions_NeverReapsAKeptSession(t *testing.T) {
	svc, store := turnStateHarness(t, answerTurn)
	sessionID := spawnTurnSession(t, svc, true)
	if _, err := svc.PromptSession(turnStateContext(), &PromptSessionRequest{SessionID: sessionID, Prompt: "hi"}); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}
	quietenTurnSession(t, svc, store, sessionID)

	if reason, ok := sweepTurnSessions(t, svc)[sessionID]; ok {
		t.Fatalf("sweep concluded a kept session as %q", reason)
	}
	if got := storedTurnSession(t, store, sessionID).Status; got != SessionStateIdle {
		t.Errorf("kept session status = %s, want %s", got, SessionStateIdle)
	}
}

func TestSweepSessions_ReapsASessionWhoseTurnFailed(t *testing.T) {
	svc, store := turnStateHarness(t, func(context.Context, *PromptRequest) (*PromptResponse, error) {
		return nil, errTurnFailed
	})
	sessionID := spawnTurnSession(t, svc, false)
	if _, err := svc.PromptSession(turnStateContext(), &PromptSessionRequest{SessionID: sessionID, Prompt: "hi"}); err == nil {
		t.Fatal("PromptSession succeeded, want the backend's failure")
	}
	if got := storedTurnSession(t, store, sessionID).Status; got != SessionStateIdle {
		t.Fatalf("status after the failed turn = %s, want %s", got, SessionStateIdle)
	}
	quietenTurnSession(t, svc, store, sessionID)

	if got := sweepTurnSessions(t, svc)[sessionID]; got != ConcludedBySessionIdle {
		t.Fatalf("sweep concluded the session as %q, want %q", got, ConcludedBySessionIdle)
	}
}

func TestPromptSession_CancelledTurnLeavesTheSessionIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(turnStateContext())
	defer cancel()
	svc, store := turnStateHarness(t, func(turnCtx context.Context, _ *PromptRequest) (*PromptResponse, error) {
		cancel()
		return nil, turnCtx.Err()
	})
	sessionID := spawnTurnSession(t, svc, false)
	if _, err := svc.PromptSession(ctx, &PromptSessionRequest{SessionID: sessionID, Prompt: "hi"}); err == nil {
		t.Fatal("PromptSession succeeded, want the cancellation")
	}
	if got := storedTurnSession(t, store, sessionID).Status; got != SessionStateIdle {
		t.Errorf("status after the cancelled turn = %s, want %s", got, SessionStateIdle)
	}
}

func TestSessionStore_MarkSessionTurnMovesAnOpenSession(t *testing.T) {
	store := NewInMemoryAgentStore()
	seedSession(t, store, &SessionRecord{SessionID: "s-mark", TenantID: "t1", Status: SessionStateIdle})
	ctx := context.Background()

	if err := store.MarkSessionTurn(ctx, "t1", "s-mark", true); err != nil {
		t.Fatalf("MarkSessionTurn(in flight): %v", err)
	}
	if got := storedTurnSession(t, store, "s-mark").Status; got != SessionStateActive {
		t.Fatalf("status = %s, want %s", got, SessionStateActive)
	}
	if err := store.MarkSessionTurn(ctx, "t1", "s-mark", false); err != nil {
		t.Fatalf("MarkSessionTurn(ended): %v", err)
	}
	if got := storedTurnSession(t, store, "s-mark").Status; got != SessionStateIdle {
		t.Errorf("status = %s, want %s", got, SessionStateIdle)
	}
}

func TestSessionStore_MarkSessionTurnDoesNotReopenAClosedSession(t *testing.T) {
	store := NewInMemoryAgentStore()
	seedSession(t, store, &SessionRecord{SessionID: "s-closed", TenantID: "t1", Status: SessionStateClosed})

	if err := store.MarkSessionTurn(context.Background(), "t1", "s-closed", true); err != nil {
		t.Fatalf("MarkSessionTurn: %v", err)
	}
	if got := storedTurnSession(t, store, "s-closed").Status; got != SessionStateClosed {
		t.Errorf("status = %s, want %s", got, SessionStateClosed)
	}
}

func TestSessionStore_MarkSessionTurnFailsForAnUnknownSession(t *testing.T) {
	store := NewInMemoryAgentStore()
	if err := store.MarkSessionTurn(context.Background(), "t1", "s-missing", false); err == nil {
		t.Fatal("MarkSessionTurn succeeded for a session the store does not hold")
	}
}
