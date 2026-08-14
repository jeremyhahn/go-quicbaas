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
	"testing"
	"time"
)

// blockingBackend is an AgentBackend whose Prompt does not return until the
// test releases it. It stands in for a real agent turn, which runs for minutes:
// everything the asynchronous contract promises — a prompt that returns
// immediately, a turn that outlives the request, a transcript that fills in
// afterwards — is only observable while a turn is still in flight.
type blockingBackend struct {
	// entered is closed when Prompt is invoked, so a test can prove the turn
	// really started without polling.
	entered chan struct{}

	// release gates Prompt's return. The test closes it to finish the turn.
	release chan struct{}

	// promptCtxErr records the turn context's error at the moment Prompt
	// returns. It proves whether the caller's cancellation reached the agent.
	promptCtxErr error

	// err, when set, is returned instead of a response.
	err error

	// text is the assistant output the backend produces.
	text string
}

func newBlockingBackend() *blockingBackend {
	return &blockingBackend{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		text:    "the answer",
	}
}

func (b *blockingBackend) Type() string                      { return "blocking" }
func (b *blockingBackend) Capabilities() BackendCapabilities { return BackendCapabilities{} }
func (b *blockingBackend) ToolGuidance() string              { return "" }

func (b *blockingBackend) Spawn(_ context.Context, _ *SpawnRequest) (*Session, error) {
	return &Session{ID: "sess-1"}, nil
}

func (b *blockingBackend) Prompt(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
	close(b.entered)
	<-b.release
	b.promptCtxErr = ctx.Err()
	if b.err != nil {
		return nil, b.err
	}
	return &PromptResponse{
		SessionID:  req.SessionID,
		Content:    []ContentBlock{{Type: "text", Text: b.text}},
		Model:      "test-model",
		StopReason: StopReasonEndTurn,
	}, nil
}

func (b *blockingBackend) Close(_ context.Context, _ string) error { return nil }

func (b *blockingBackend) Status(_ context.Context, _ string) (*SessionStatus, error) {
	return &SessionStatus{State: SessionStateActive}, nil
}

var _ AgentBackend = (*blockingBackend)(nil)

// newAsyncTurnService builds an orchestrator wired to a blocking backend and an
// in-memory session log — the minimum needed to exercise a turn's full
// lifecycle without touching the host.
func newAsyncTurnService(t *testing.T, backends map[string]AgentBackend) (*agentServiceImpl, *MemorySessionEventLog) {
	t.Helper()
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	log := NewMemorySessionEventLog()
	impl.SetSessionEventLog(log)
	return impl, log
}

// asyncTurnContext returns a tenant-stamped context for turn dispatch.
func asyncTurnContext() context.Context {
	return ContextWithTenant(context.Background(), "tenant-1")
}

// sessionKinds reads the whole transcript and returns the event kinds in order.
func sessionKinds(t *testing.T, log *MemorySessionEventLog) []SessionEventKind {
	t.Helper()
	events, _, err := log.Range(context.Background(), "tenant-1", "sess-1", 0, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	kinds := make([]SessionEventKind, 0, len(events))
	var prev uint64
	for _, ev := range events {
		if ev.Sequence <= prev {
			t.Fatalf("transcript sequences must strictly increase: %d after %d", ev.Sequence, prev)
		}
		prev = ev.Sequence
		kinds = append(kinds, ev.Kind)
	}
	return kinds
}

// TestPromptSessionAsync_ReturnsWhileTurnRuns is the whole point of the
// asynchronous contract: the dispatch answers while the agent is demonstrably
// still working. The backend is held open for the duration of the assertion, so
// a handler that waited for the turn could not reach the acknowledgement at all.
func TestPromptSessionAsync_ReturnsWhileTurnRuns(t *testing.T) {
	backend := newBlockingBackend()
	svc, log := newAsyncTurnService(t, map[string]AgentBackend{"blocking": backend})
	ctx := asyncTurnContext()

	start := time.Now()
	ack, err := svc.PromptSessionAsync(ctx, &PromptSessionRequest{
		SessionID: "sess-1",
		Backend:   "blocking",
		Prompt:    "do the thing",
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("PromptSessionAsync: %v", err)
	}

	// The REST transport caps a request at 25s. The dispatch must finish with
	// room to spare — it does no agent work at all, only two log appends.
	if elapsed > 2*time.Second {
		t.Fatalf("dispatch took %s; it must return well inside the transport's request cap", elapsed)
	}
	if ack.Status != TurnStatusAccepted {
		t.Fatalf("status = %q, want %q", ack.Status, TurnStatusAccepted)
	}
	if ack.TurnID == "" {
		t.Fatal("acknowledgement must carry a turn ID for the caller to follow")
	}
	if ack.SessionID != "sess-1" {
		t.Fatalf("session = %q, want sess-1", ack.SessionID)
	}
	if ack.Backend != "blocking" {
		t.Fatalf("backend = %q, want blocking", ack.Backend)
	}

	// The turn really is still running, and the transcript already says so.
	<-backend.entered
	kinds := sessionKinds(t, log)
	want := []SessionEventKind{KindPromptSubmitted, KindTurnStarted}
	if len(kinds) != len(want) {
		t.Fatalf("in-flight transcript = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("in-flight transcript = %v, want %v", kinds, want)
		}
	}

	// The acknowledgement's sequence must address the turn_started event, so a
	// client attaching there receives the whole turn with no gap.
	events, _, err := log.Range(context.Background(), "tenant-1", "sess-1", ack.Sequence, 1)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(events) != 1 || events[0].Kind != KindTurnStarted {
		t.Fatalf("ack sequence %d does not address the turn_started event", ack.Sequence)
	}
	if events[0].TurnStarted.TurnID != ack.TurnID {
		t.Fatalf("turn_started turn_id = %q, want %q", events[0].TurnStarted.TurnID, ack.TurnID)
	}

	close(backend.release)
	svc.WaitForTurns()
}

// TestPromptSessionAsync_TranscriptHoldsTheWholeTurn asserts the durable log is
// the complete record of the turn: the prompt, the lifecycle bracket, and the
// agent's output, in order, with the terminal event tied back to the ack.
func TestPromptSessionAsync_TranscriptHoldsTheWholeTurn(t *testing.T) {
	backend := newBlockingBackend()
	svc, log := newAsyncTurnService(t, map[string]AgentBackend{"blocking": backend})

	ack, err := svc.PromptSessionAsync(asyncTurnContext(), &PromptSessionRequest{
		SessionID: "sess-1",
		Backend:   "blocking",
		Prompt:    "do the thing",
	})
	if err != nil {
		t.Fatalf("PromptSessionAsync: %v", err)
	}
	close(backend.release)
	svc.WaitForTurns()

	kinds := sessionKinds(t, log)
	want := []SessionEventKind{
		KindPromptSubmitted, KindTurnStarted, KindAssistantChunk, KindTurnCompleted,
	}
	if len(kinds) != len(want) {
		t.Fatalf("transcript = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("transcript = %v, want %v", kinds, want)
		}
	}

	events, _, err := log.Range(context.Background(), "tenant-1", "sess-1", 0, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if got := events[2].AssistantChunk.Text; got != "the answer" {
		t.Fatalf("assistant text = %q, want %q", got, "the answer")
	}
	completed := events[3].TurnCompleted
	if completed.TurnID != ack.TurnID {
		t.Fatalf("turn_completed turn_id = %q, want %q", completed.TurnID, ack.TurnID)
	}
	if completed.StopReason != StopReasonEndTurn {
		t.Fatalf("turn_completed stop_reason = %q, want %q", completed.StopReason, StopReasonEndTurn)
	}
	if completed.Model != "test-model" {
		t.Fatalf("turn_completed model = %q, want test-model", completed.Model)
	}
}

// TestPromptSessionAsync_SurvivesCallerCancellation pins the guarantee that a
// disconnecting client cannot kill an agent turn. The caller's context is
// cancelled the moment the acknowledgement lands — which is exactly what an
// HTTP request ending does — and the turn must still run to completion against
// an uncancelled context and finish its transcript.
func TestPromptSessionAsync_SurvivesCallerCancellation(t *testing.T) {
	backend := newBlockingBackend()
	svc, log := newAsyncTurnService(t, map[string]AgentBackend{"blocking": backend})

	ctx, cancel := context.WithCancel(asyncTurnContext())
	ack, err := svc.PromptSessionAsync(ctx, &PromptSessionRequest{
		SessionID: "sess-1",
		Backend:   "blocking",
		Prompt:    "do the thing",
	})
	if err != nil {
		t.Fatalf("PromptSessionAsync: %v", err)
	}

	<-backend.entered
	cancel() // the client goes away mid-turn
	close(backend.release)
	svc.WaitForTurns()

	if backend.promptCtxErr != nil {
		t.Fatalf("the turn's context was cancelled by the caller: %v", backend.promptCtxErr)
	}

	events, _, err := log.Range(context.Background(), "tenant-1", "sess-1", 0, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	last := events[len(events)-1]
	if last.Kind != KindTurnCompleted {
		t.Fatalf("last event = %s, want turn_completed", last.Kind)
	}
	if last.TurnCompleted.TurnID != ack.TurnID {
		t.Fatalf("turn_completed turn_id = %q, want %q", last.TurnCompleted.TurnID, ack.TurnID)
	}
}

// TestPromptSessionAsync_FailedTurnIsRecorded asserts a backend failure closes
// the turn with turn_failed rather than leaving it open forever. An operator
// watching the stream must be able to tell "the agent broke" from "the agent is
// thinking", and a turn with no terminal event is indistinguishable from the
// latter.
func TestPromptSessionAsync_FailedTurnIsRecorded(t *testing.T) {
	backend := newBlockingBackend()
	backend.err = errors.New("backend exploded")
	svc, log := newAsyncTurnService(t, map[string]AgentBackend{"blocking": backend})

	ack, err := svc.PromptSessionAsync(asyncTurnContext(), &PromptSessionRequest{
		SessionID: "sess-1",
		Backend:   "blocking",
		Prompt:    "do the thing",
	})
	if err != nil {
		t.Fatalf("PromptSessionAsync: %v", err)
	}
	close(backend.release)
	svc.WaitForTurns()

	events, _, err := log.Range(context.Background(), "tenant-1", "sess-1", 0, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	last := events[len(events)-1]
	if last.Kind != KindTurnFailed {
		t.Fatalf("last event = %s, want turn_failed", last.Kind)
	}
	if last.TurnFailed.TurnID != ack.TurnID {
		t.Fatalf("turn_failed turn_id = %q, want %q", last.TurnFailed.TurnID, ack.TurnID)
	}
	if last.TurnFailed.Error != "backend exploded" {
		t.Fatalf("turn_failed error = %q, want %q", last.TurnFailed.Error, "backend exploded")
	}
}

// TestPromptSessionAsync_ConcurrentTurnsKeepGaplessSequences asserts that two
// turns dispatched back to back — plus the goroutines that close them — never
// collide on a sequence number. The log rejects a stale sequence outright, so a
// lost race would silently drop a turn's events from the transcript.
func TestPromptSessionAsync_ConcurrentTurnsKeepGaplessSequences(t *testing.T) {
	backend := newBlockingBackend()
	// The blocking backend's Prompt is single-use (it closes `entered`), so the
	// two concurrent turns run against separate instances on one session.
	second := newBlockingBackend()
	svc, log := newAsyncTurnService(t, map[string]AgentBackend{
		"blocking":  backend,
		"blocking2": second,
	})
	ctx := asyncTurnContext()

	first, err := svc.PromptSessionAsync(ctx, &PromptSessionRequest{
		SessionID: "sess-1", Backend: "blocking", Prompt: "one",
	})
	if err != nil {
		t.Fatalf("first PromptSessionAsync: %v", err)
	}
	secondAck, err := svc.PromptSessionAsync(ctx, &PromptSessionRequest{
		SessionID: "sess-1", Backend: "blocking2", Prompt: "two",
	})
	if err != nil {
		t.Fatalf("second PromptSessionAsync: %v", err)
	}
	if secondAck.Sequence <= first.Sequence {
		t.Fatalf("second turn started at sequence %d, not after the first at %d",
			secondAck.Sequence, first.Sequence)
	}

	close(backend.release)
	close(second.release)
	svc.WaitForTurns()

	// sessionKinds asserts strict monotonicity; both turns must be closed.
	kinds := sessionKinds(t, log)
	var completed int
	for _, k := range kinds {
		if k == KindTurnCompleted {
			completed++
		}
	}
	if completed != 2 {
		t.Fatalf("expected both turns to close, got %d turn_completed events in %v", completed, kinds)
	}
}

// TestPromptSessionAsync_RejectsUnfollowableTurns asserts the dispatch refuses
// anything it could not publish, rather than accepting a turn whose output
// would go nowhere. Every rejection is synchronous, so a caller never gets an
// acknowledgement for a turn that will not happen.
func TestPromptSessionAsync_RejectsUnfollowableTurns(t *testing.T) {
	ctx := asyncTurnContext()

	t.Run("no prompt text", func(t *testing.T) {
		svc, _ := newAsyncTurnService(t, map[string]AgentBackend{"blocking": newBlockingBackend()})
		_, err := svc.PromptSessionAsync(ctx, &PromptSessionRequest{SessionID: "sess-1", Backend: "blocking"})
		if !errors.Is(err, ErrPromptRequired) {
			t.Fatalf("err = %v, want ErrPromptRequired", err)
		}
	})

	t.Run("no session to publish to", func(t *testing.T) {
		svc, _ := newAsyncTurnService(t, map[string]AgentBackend{"blocking": newBlockingBackend()})
		_, err := svc.PromptSessionAsync(ctx, &PromptSessionRequest{Backend: "blocking", Prompt: "hi"})
		if !errors.Is(err, ErrSessionIDRequired) {
			t.Fatalf("err = %v, want ErrSessionIDRequired", err)
		}
	})

	t.Run("unknown backend", func(t *testing.T) {
		svc, _ := newAsyncTurnService(t, map[string]AgentBackend{"blocking": newBlockingBackend()})
		_, err := svc.PromptSessionAsync(ctx, &PromptSessionRequest{
			SessionID: "sess-1", Backend: "nope", Prompt: "hi",
		})
		if err == nil {
			t.Fatal("expected an error for an unregistered backend")
		}
	})

	t.Run("no event log installed", func(t *testing.T) {
		svc, err := NewAgentService(NewInMemoryAgentStore(),
			map[string]AgentBackend{"blocking": newBlockingBackend()}, &agentTestLogger{})
		if err != nil {
			t.Fatalf("NewAgentService: %v", err)
		}
		_, err = svc.PromptSessionAsync(ctx, &PromptSessionRequest{
			SessionID: "sess-1", Backend: "blocking", Prompt: "hi",
		})
		if !errors.Is(err, ErrSessionLogRequired) {
			t.Fatalf("err = %v, want ErrSessionLogRequired", err)
		}
	})
}
