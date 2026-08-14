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
	"testing"
)

// spawnStreamTestSession builds an agent service wired with a memory event
// log and the given mock backend, spawns a persistent session, and returns
// the pieces the streaming-transcript tests need.
func spawnStreamTestSession(t *testing.T, backend *mockAgentBackend, tenant string) (AgentService, *MemorySessionEventLog, context.Context, string) {
	t.Helper()

	svc, err := NewAgentService(NewInMemoryAgentStore(), map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	log := NewMemorySessionEventLog()
	svc.(*agentServiceImpl).SetSessionEventLog(log)

	ctx := ContextWithTenant(context.Background(), tenant)
	spawnResp, err := svc.SpawnSession(ctx, &SpawnSessionRequest{Backend: "mock", Persistent: true})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	return svc, log, ctx, spawnResp.SessionID
}

// TestRunPromptTurn_StreamsBackendEventsLive is the regression guard for the
// live transcript: when the backend emits structured events mid-turn, each
// one must land on the session log stamped with tenant/source, and the
// post-turn aggregate append must be SKIPPED so the answer does not appear
// twice.
func TestRunPromptTurn_StreamsBackendEventsLive(t *testing.T) {
	t.Parallel()

	const tenant = "tenant-stream"
	backend := &mockAgentBackend{
		promptFn: func(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
			// A Claude-Code-like turn: thought, tool call, tool result, then
			// the streamed answer in two chunks.
			req.EventCallback(&SessionEvent{
				SessionID: req.SessionID,
				Kind:      KindAgentThought,
				Thought:   &AgentThoughtPayload{Text: "planning"},
			})
			req.EventCallback(&SessionEvent{
				SessionID:      req.SessionID,
				Kind:           KindToolInvocation,
				ToolInvocation: &ToolInvocationPayload{InvocationID: "t1", Name: "Read"},
			})
			req.EventCallback(&SessionEvent{
				SessionID:  req.SessionID,
				Kind:       KindToolResult,
				ToolResult: &ToolResultPayload{InvocationID: "t1", Content: "data"},
			})
			req.EventCallback(&SessionEvent{
				SessionID:      req.SessionID,
				Kind:           KindAssistantChunk,
				AssistantChunk: &AssistantChunkPayload{Text: "the answer, "},
			})
			req.EventCallback(&SessionEvent{
				SessionID:      req.SessionID,
				Kind:           KindAssistantChunk,
				AssistantChunk: &AssistantChunkPayload{Text: "streamed", StopReason: "end_turn"},
			})
			return &PromptResponse{
				SessionID:  req.SessionID,
				Content:    []ContentBlock{{Type: "text", Text: "the answer, streamed"}},
				Model:      "m1",
				StopReason: "end_turn",
			}, nil
		},
	}

	svc, log, ctx, sessionID := spawnStreamTestSession(t, backend, tenant)

	if _, err := svc.PromptSession(ctx, &PromptSessionRequest{
		SessionID: sessionID,
		Prompt:    "do it",
	}); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}

	events, _, err := log.Range(ctx, tenant, sessionID, 0, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}

	// spawn marker + prompt_submitted + the 5 streamed events. No final
	// aggregate: the two streamed chunks already carry the answer.
	wantKinds := []SessionEventKind{
		KindSystemMessage, // session spawned
		KindPromptSubmitted,
		KindAgentThought,
		KindToolInvocation,
		KindToolResult,
		KindAssistantChunk,
		KindAssistantChunk,
	}
	if len(events) != len(wantKinds) {
		t.Fatalf("got %d events, want %d: %+v", len(events), len(wantKinds), events)
	}
	for i, want := range wantKinds {
		if events[i].Kind != want {
			t.Fatalf("event[%d] kind = %q, want %q", i, events[i].Kind, want)
		}
	}

	// The streamed agent events must be stamped by the orchestrator.
	for _, ev := range events[2:] {
		if ev.TenantID != tenant {
			t.Fatalf("event %q tenant = %q, want %q", ev.Kind, ev.TenantID, tenant)
		}
		if ev.Source != "agent:mock" {
			t.Fatalf("event %q source = %q, want agent:mock", ev.Kind, ev.Source)
		}
		if ev.Timestamp.IsZero() {
			t.Fatalf("event %q missing timestamp", ev.Kind)
		}
	}

	// Exactly the two streamed chunk texts — proving the aggregate was
	// skipped and nothing was double-recorded.
	if events[5].AssistantChunk.Text != "the answer, " || events[6].AssistantChunk.Text != "streamed" {
		t.Fatalf("chunk texts = %q, %q", events[5].AssistantChunk.Text, events[6].AssistantChunk.Text)
	}
}

// TestRunPromptTurn_AppendsAggregateWhenNothingStreamed is the counterpart:
// a backend that streams no events (codex-like one-shot without callback
// support) must still get its final answer recorded as the single aggregate
// AssistantChunk.
func TestRunPromptTurn_AppendsAggregateWhenNothingStreamed(t *testing.T) {
	t.Parallel()

	const tenant = "tenant-aggregate"
	backend := &mockAgentBackend{
		promptFn: func(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
			// Emits a non-chunk event only; the answer itself is not streamed.
			req.EventCallback(&SessionEvent{
				SessionID: req.SessionID,
				Kind:      KindSystemMessage,
				System:    &SystemMessagePayload{Level: "info", Code: "start", Message: "starting"},
			})
			return &PromptResponse{
				SessionID:  req.SessionID,
				Content:    []ContentBlock{{Type: "text", Text: "full answer"}},
				Model:      "m1",
				StopReason: "end_turn",
			}, nil
		},
	}

	svc, log, ctx, sessionID := spawnStreamTestSession(t, backend, tenant)

	if _, err := svc.PromptSession(ctx, &PromptSessionRequest{
		SessionID: sessionID,
		Prompt:    "do it",
	}); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}

	events, _, err := log.Range(ctx, tenant, sessionID, 0, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}

	wantKinds := []SessionEventKind{
		KindSystemMessage, // session spawned
		KindPromptSubmitted,
		KindSystemMessage,  // the backend's streamed start marker
		KindAssistantChunk, // the post-turn aggregate
	}
	if len(events) != len(wantKinds) {
		t.Fatalf("got %d events, want %d: %+v", len(events), len(wantKinds), events)
	}
	for i, want := range wantKinds {
		if events[i].Kind != want {
			t.Fatalf("event[%d] kind = %q, want %q", i, events[i].Kind, want)
		}
	}
	agg := events[3]
	if agg.AssistantChunk == nil || agg.AssistantChunk.Text != "full answer" ||
		agg.AssistantChunk.StopReason != "end_turn" || agg.AssistantChunk.Model != "m1" {
		t.Fatalf("aggregate chunk = %+v", agg.AssistantChunk)
	}
}

// TestRunPromptTurn_WrappedCallbackReachesCaller verifies the orchestrator's
// log-appending wrapper still delivers every event to a caller-supplied
// EventCallback (the control loop's audit hook).
func TestRunPromptTurn_WrappedCallbackReachesCaller(t *testing.T) {
	t.Parallel()

	const tenant = "tenant-caller"
	backend := &mockAgentBackend{
		promptFn: func(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
			req.EventCallback(&SessionEvent{
				SessionID:      req.SessionID,
				Kind:           KindAssistantChunk,
				AssistantChunk: &AssistantChunkPayload{Text: "hi"},
			})
			return &PromptResponse{
				SessionID: req.SessionID,
				Content:   []ContentBlock{{Type: "text", Text: "hi"}},
			}, nil
		},
	}

	svc, _, ctx, sessionID := spawnStreamTestSession(t, backend, tenant)

	var callerEvents []*SessionEvent
	if _, err := svc.PromptSession(ctx, &PromptSessionRequest{
		SessionID: sessionID,
		Prompt:    "do it",
		EventCallback: func(ev *SessionEvent) {
			callerEvents = append(callerEvents, ev)
		},
	}); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}

	if len(callerEvents) != 1 || callerEvents[0].Kind != KindAssistantChunk {
		t.Fatalf("caller events = %+v", callerEvents)
	}
	// The caller sees the stamped event.
	if callerEvents[0].TenantID != tenant || callerEvents[0].Source != "agent:mock" {
		t.Fatalf("caller event not stamped: %+v", callerEvents[0])
	}
}

// TestSessionEventSummary covers the audit summarizer: one line per kind,
// truncation, and nil-payload tolerance.
func TestSessionEventSummary(t *testing.T) {
	t.Parallel()

	long := make([]byte, sessionEventSummaryMax+50)
	for i := range long {
		long[i] = 'x'
	}

	tests := []struct {
		name string
		ev   *SessionEvent
		want string
	}{
		{
			name: "prompt",
			ev:   &SessionEvent{Kind: KindPromptSubmitted, Prompt: &PromptSubmittedPayload{Prompt: "fix it"}},
			want: "fix it",
		},
		{
			name: "assistant chunk",
			ev:   &SessionEvent{Kind: KindAssistantChunk, AssistantChunk: &AssistantChunkPayload{Text: "done"}},
			want: "done",
		},
		{
			name: "thought",
			ev:   &SessionEvent{Kind: KindAgentThought, Thought: &AgentThoughtPayload{Text: "hmm"}},
			want: "hmm",
		},
		{
			name: "tool invocation",
			ev:   &SessionEvent{Kind: KindToolInvocation, ToolInvocation: &ToolInvocationPayload{Name: "Bash"}},
			want: "tool=Bash",
		},
		{
			name: "tool result error",
			ev:   &SessionEvent{Kind: KindToolResult, ToolResult: &ToolResultPayload{InvocationID: "t9", IsError: true}},
			want: "invocation=t9 error=true",
		},
		{
			name: "system",
			ev:   &SessionEvent{Kind: KindSystemMessage, System: &SystemMessagePayload{Code: "plan", Message: "2 steps"}},
			want: "plan 2 steps",
		},
		{
			name: "nil payload yields empty",
			ev:   &SessionEvent{Kind: KindAssistantChunk},
			want: "",
		},
		{
			name: "long text truncated",
			ev:   &SessionEvent{Kind: KindAssistantChunk, AssistantChunk: &AssistantChunkPayload{Text: string(long)}},
			want: string(long[:sessionEventSummaryMax]) + "...",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := sessionEventSummary(tc.ev); got != tc.want {
				t.Fatalf("summary = %q, want %q", got, tc.want)
			}
		})
	}
}
