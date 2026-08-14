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
	"encoding/json"
	"testing"
)

// =============================================================================
// KindAgentThought — payload round-trip
// =============================================================================

// TestSessionEvent_AgentThought_JSONRoundTrip verifies KindAgentThought
// serializes with exactly the `thought` payload populated and survives a
// marshal/unmarshal cycle intact.
func TestSessionEvent_AgentThought_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	in := &SessionEvent{
		Sequence:  7,
		SessionID: "sess-1",
		TenantID:  "tenant-1",
		Source:    "agent:claude",
		Kind:      KindAgentThought,
		Thought:   &AgentThoughtPayload{Text: "I should check the parser first"},
	}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// The wire form must carry the thought payload under `thought` and no
	// other payload keys.
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("Unmarshal wire map: %v", err)
	}
	if _, ok := wire["thought"]; !ok {
		t.Fatalf("wire form missing `thought` key: %s", raw)
	}
	for _, key := range []string{"prompt", "assistant_chunk", "tool_invocation", "tool_result", "system"} {
		if _, ok := wire[key]; ok {
			t.Fatalf("wire form unexpectedly carries %q: %s", key, raw)
		}
	}

	var out SessionEvent
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Kind != KindAgentThought {
		t.Fatalf("kind = %q, want %q", out.Kind, KindAgentThought)
	}
	if out.Thought == nil || out.Thought.Text != in.Thought.Text {
		t.Fatalf("thought payload = %+v, want %+v", out.Thought, in.Thought)
	}
}

// TestSessionEvent_AgentThought_UnmarshalMissingPayload verifies a thought
// event without its payload unmarshals to a nil Thought pointer rather than
// erroring, so consumers can nil-check.
func TestSessionEvent_AgentThought_UnmarshalMissingPayload(t *testing.T) {
	t.Parallel()

	var out SessionEvent
	if err := json.Unmarshal([]byte(`{"sequence":1,"session_id":"s","tenant_id":"t","kind":"agent_thought"}`), &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Kind != KindAgentThought {
		t.Fatalf("kind = %q, want %q", out.Kind, KindAgentThought)
	}
	if out.Thought != nil {
		t.Fatalf("expected nil Thought, got %+v", out.Thought)
	}
}

// =============================================================================
// stream-json dialect translation
// =============================================================================

// collectEvents returns a callback that records every event it receives.
func collectEvents(events *[]*SessionEvent) SessionEventCallback {
	return func(ev *SessionEvent) {
		*events = append(*events, ev)
	}
}

// TestEmitStreamJSONEvents_Translation feeds stream-json fixture lines
// through emitStreamJSONEvents and asserts the exact SessionEvent sequence.
func TestEmitStreamJSONEvents_Translation(t *testing.T) {
	t.Parallel()

	lines := []string{
		// init: skipped as noise.
		`{"type":"system","subtype":"init","session_id":"s1","model":"m1"}`,
		// assistant message carrying thinking + text + tool_use blocks.
		`{"type":"assistant","message":{"role":"assistant","model":"m1","stop_reason":"tool_use","content":[` +
			`{"type":"thinking","thinking":"need to read the file"},` +
			`{"type":"text","text":"Reading the file now."},` +
			`{"type":"tool_use","id":"toolu_01","name":"Read","input":{"file_path":"/tmp/x"}}]}}`,
		// user message echoing the tool result (array content shape).
		`{"type":"user","message":{"role":"user","content":[` +
			`{"type":"tool_result","tool_use_id":"toolu_01","content":[{"type":"text","text":"line one"},{"type":"text","text":" line two"}],"is_error":false}]}}`,
		// non-init system message.
		`{"type":"system","subtype":"compact_boundary","session_id":"s1"}`,
		// result: no event, the turn lifecycle covers it.
		`{"type":"result","subtype":"success","result":"done","is_error":false}`,
	}

	var events []*SessionEvent
	cb := collectEvents(&events)
	for _, line := range lines {
		var msg streamJSONMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("fixture line unparseable: %v\n%s", err, line)
		}
		emitStreamJSONEvents(cb, "sess-1", &msg)
	}

	wantKinds := []SessionEventKind{
		KindAgentThought,
		KindAssistantChunk,
		KindToolInvocation,
		KindToolResult,
		KindSystemMessage,
	}
	if len(events) != len(wantKinds) {
		t.Fatalf("got %d events, want %d: %+v", len(events), len(wantKinds), events)
	}
	for i, want := range wantKinds {
		if events[i].Kind != want {
			t.Fatalf("event[%d] kind = %q, want %q", i, events[i].Kind, want)
		}
		if events[i].SessionID != "sess-1" {
			t.Fatalf("event[%d] session = %q, want sess-1", i, events[i].SessionID)
		}
	}

	if events[0].Thought == nil || events[0].Thought.Text != "need to read the file" {
		t.Fatalf("thought payload = %+v", events[0].Thought)
	}
	chunk := events[1].AssistantChunk
	if chunk == nil || chunk.Text != "Reading the file now." || chunk.Model != "m1" || chunk.StopReason != "tool_use" {
		t.Fatalf("assistant chunk payload = %+v", chunk)
	}
	inv := events[2].ToolInvocation
	if inv == nil || inv.InvocationID != "toolu_01" || inv.Name != "Read" {
		t.Fatalf("tool invocation payload = %+v", inv)
	}
	if string(inv.Args) != `{"file_path":"/tmp/x"}` {
		t.Fatalf("tool args = %s", inv.Args)
	}
	res := events[3].ToolResult
	if res == nil || res.InvocationID != "toolu_01" || res.Content != "line one line two" || res.IsError {
		t.Fatalf("tool result payload = %+v", res)
	}
	sys := events[4].System
	if sys == nil || sys.Level != "info" || sys.Code != "compact_boundary" {
		t.Fatalf("system payload = %+v", sys)
	}
}

// TestEmitStreamJSONEvents_ToolResultStringContentAndError covers the
// tool_result "content" carried as a plain JSON string plus is_error=true.
func TestEmitStreamJSONEvents_ToolResultStringContentAndError(t *testing.T) {
	t.Parallel()

	line := `{"type":"user","message":{"role":"user","content":[` +
		`{"type":"tool_result","tool_use_id":"toolu_02","content":"exit status 1","is_error":true}]}}`
	var msg streamJSONMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		t.Fatalf("fixture line unparseable: %v", err)
	}

	var events []*SessionEvent
	emitStreamJSONEvents(collectEvents(&events), "sess-1", &msg)

	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	res := events[0].ToolResult
	if res == nil || res.InvocationID != "toolu_02" || res.Content != "exit status 1" || !res.IsError {
		t.Fatalf("tool result payload = %+v", res)
	}
}

// TestEmitStreamJSONEvents_NilSafety covers the guard clauses: nil callback
// and nil message must not panic and must emit nothing.
func TestEmitStreamJSONEvents_NilSafety(t *testing.T) {
	t.Parallel()

	emitStreamJSONEvents(nil, "s", &streamJSONMessage{Type: "assistant"})

	var events []*SessionEvent
	cb := collectEvents(&events)
	emitStreamJSONEvents(cb, "s", nil)
	// Assistant/user messages with no body are skipped, not panicked on.
	emitStreamJSONEvents(cb, "s", &streamJSONMessage{Type: "assistant"})
	emitStreamJSONEvents(cb, "s", &streamJSONMessage{Type: "user"})
	if len(events) != 0 {
		t.Fatalf("expected no events, got %d", len(events))
	}
}

// TestFlattenToolResultContent covers all three wire shapes of a
// tool_result content field.
func TestFlattenToolResultContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", ""},
		{"string", `"plain text"`, "plain text"},
		{"blocks", `[{"type":"text","text":"a"},{"type":"text","text":"b"}]`, "ab"},
		{"unknown shape passes through raw", `{"weird":true}`, `{"weird":true}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := flattenToolResultContent(json.RawMessage(tc.raw)); got != tc.want {
				t.Fatalf("flattenToolResultContent(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// =============================================================================
// JSON-RPC ACP session/update translation
// =============================================================================

// TestEmitACPSessionUpdate_Translation is the table-driven mapping test for
// every session/update dialect value.
func TestEmitACPSessionUpdate_Translation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params string // the notification's params JSON
		want   func(t *testing.T, events []*SessionEvent)
	}{
		{
			name:   "agent_message_chunk",
			params: `{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello"}}}`,
			want: func(t *testing.T, events []*SessionEvent) {
				if len(events) != 1 || events[0].Kind != KindAssistantChunk {
					t.Fatalf("events = %+v", events)
				}
				if events[0].AssistantChunk == nil || events[0].AssistantChunk.Text != "hello" {
					t.Fatalf("chunk = %+v", events[0].AssistantChunk)
				}
			},
		},
		{
			name:   "agent_thought_chunk",
			params: `{"sessionId":"s","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"hmm"}}}`,
			want: func(t *testing.T, events []*SessionEvent) {
				if len(events) != 1 || events[0].Kind != KindAgentThought {
					t.Fatalf("events = %+v", events)
				}
				if events[0].Thought == nil || events[0].Thought.Text != "hmm" {
					t.Fatalf("thought = %+v", events[0].Thought)
				}
			},
		},
		{
			name:   "tool_call with title",
			params: `{"update":{"sessionUpdate":"tool_call","toolCallId":"tc1","title":"Read main.go","kind":"read","status":"pending","rawInput":{"path":"main.go"}}}`,
			want: func(t *testing.T, events []*SessionEvent) {
				if len(events) != 1 || events[0].Kind != KindToolInvocation {
					t.Fatalf("events = %+v", events)
				}
				inv := events[0].ToolInvocation
				if inv == nil || inv.InvocationID != "tc1" || inv.Name != "Read main.go" {
					t.Fatalf("invocation = %+v", inv)
				}
				if string(inv.Args) != `{"path":"main.go"}` {
					t.Fatalf("args = %s", inv.Args)
				}
			},
		},
		{
			name:   "tool_call falls back to kind when title empty",
			params: `{"update":{"sessionUpdate":"tool_call","toolCallId":"tc2","kind":"execute"}}`,
			want: func(t *testing.T, events []*SessionEvent) {
				if len(events) != 1 || events[0].ToolInvocation == nil || events[0].ToolInvocation.Name != "execute" {
					t.Fatalf("events = %+v", events)
				}
			},
		},
		{
			name:   "tool_call_update completed",
			params: `{"update":{"sessionUpdate":"tool_call_update","toolCallId":"tc1","status":"completed","content":[{"type":"content","content":{"type":"text","text":"file contents"}}]}}`,
			want: func(t *testing.T, events []*SessionEvent) {
				if len(events) != 1 || events[0].Kind != KindToolResult {
					t.Fatalf("events = %+v", events)
				}
				res := events[0].ToolResult
				if res == nil || res.InvocationID != "tc1" || res.Content != "file contents" || res.IsError {
					t.Fatalf("result = %+v", res)
				}
			},
		},
		{
			name:   "tool_call_update failed",
			params: `{"update":{"sessionUpdate":"tool_call_update","toolCallId":"tc3","status":"failed","content":[{"type":"content","content":{"type":"text","text":"boom"}}]}}`,
			want: func(t *testing.T, events []*SessionEvent) {
				if len(events) != 1 || events[0].ToolResult == nil {
					t.Fatalf("events = %+v", events)
				}
				if !events[0].ToolResult.IsError || events[0].ToolResult.Content != "boom" {
					t.Fatalf("result = %+v", events[0].ToolResult)
				}
			},
		},
		{
			name:   "tool_call_update in_progress emits nothing",
			params: `{"update":{"sessionUpdate":"tool_call_update","toolCallId":"tc1","status":"in_progress"}}`,
			want: func(t *testing.T, events []*SessionEvent) {
				if len(events) != 0 {
					t.Fatalf("expected no events, got %+v", events)
				}
			},
		},
		{
			name:   "plan",
			params: `{"update":{"sessionUpdate":"plan","entries":[{"content":"fix parser","priority":"high","status":"in_progress"},{"content":"run tests","status":"pending"}]}}`,
			want: func(t *testing.T, events []*SessionEvent) {
				if len(events) != 1 || events[0].Kind != KindSystemMessage {
					t.Fatalf("events = %+v", events)
				}
				sys := events[0].System
				if sys == nil || sys.Code != "plan" || sys.Level != "info" {
					t.Fatalf("system = %+v", sys)
				}
				want := "plan (2 steps): fix parser [in_progress]; run tests [pending]"
				if sys.Message != want {
					t.Fatalf("message = %q, want %q", sys.Message, want)
				}
			},
		},
		{
			name:   "unknown sessionUpdate becomes system message",
			params: `{"update":{"sessionUpdate":"available_commands_update"}}`,
			want: func(t *testing.T, events []*SessionEvent) {
				if len(events) != 1 || events[0].Kind != KindSystemMessage {
					t.Fatalf("events = %+v", events)
				}
				if events[0].System == nil || events[0].System.Code != "available_commands_update" {
					t.Fatalf("system = %+v", events[0].System)
				}
				if events[0].System.Message == "" {
					t.Fatal("expected compact JSON message, got empty")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var params acpSessionUpdateParams
			if err := json.Unmarshal([]byte(tc.params), &params); err != nil {
				t.Fatalf("fixture unparseable: %v", err)
			}
			var events []*SessionEvent
			emitACPSessionUpdate(collectEvents(&events), "sess-rpc", &params.Update)
			for _, ev := range events {
				if ev.SessionID != "sess-rpc" {
					t.Fatalf("session = %q, want sess-rpc", ev.SessionID)
				}
			}
			tc.want(t, events)
		})
	}
}

// TestEmitACPSessionUpdate_NilSafety verifies the guard clauses.
func TestEmitACPSessionUpdate_NilSafety(t *testing.T) {
	t.Parallel()

	emitACPSessionUpdate(nil, "s", &acpSessionUpdate{SessionUpdate: "plan"})

	var events []*SessionEvent
	emitACPSessionUpdate(collectEvents(&events), "s", nil)
	if len(events) != 0 {
		t.Fatalf("expected no events for nil update, got %d", len(events))
	}
}

// TestSummarizePlanEntries_Empty covers the no-entries branch.
func TestSummarizePlanEntries_Empty(t *testing.T) {
	t.Parallel()

	if got := summarizePlanEntries(nil); got != "plan updated (no entries)" {
		t.Fatalf("summarizePlanEntries(nil) = %q", got)
	}
}

// =============================================================================
// emitPromptResponseChunks
// =============================================================================

// TestEmitPromptResponseChunks verifies the one-shot helper emits exactly one
// KindAssistantChunk per non-empty text block and skips everything else.
func TestEmitPromptResponseChunks(t *testing.T) {
	t.Parallel()

	var events []*SessionEvent
	emitPromptResponseChunks(collectEvents(&events), &PromptResponse{
		SessionID:  "sess-os",
		Model:      "m2",
		StopReason: "end_turn",
		Content: []ContentBlock{
			{Type: "text", Text: "part one"},
			{Type: "image", Text: "ignored"},
			{Type: "text", Text: ""},
			{Type: "text", Text: "part two"},
		},
	})

	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(events), events)
	}
	for i, want := range []string{"part one", "part two"} {
		ev := events[i]
		if ev.Kind != KindAssistantChunk || ev.SessionID != "sess-os" {
			t.Fatalf("event[%d] = %+v", i, ev)
		}
		if ev.AssistantChunk == nil || ev.AssistantChunk.Text != want ||
			ev.AssistantChunk.Model != "m2" || ev.AssistantChunk.StopReason != "end_turn" {
			t.Fatalf("event[%d] chunk = %+v", i, ev.AssistantChunk)
		}
	}
}

// TestEmitPromptResponseChunks_NilSafety verifies nil callback / nil response
// are ignored.
func TestEmitPromptResponseChunks_NilSafety(t *testing.T) {
	t.Parallel()

	emitPromptResponseChunks(nil, &PromptResponse{Content: []ContentBlock{{Type: "text", Text: "x"}}})

	var events []*SessionEvent
	emitPromptResponseChunks(collectEvents(&events), nil)
	if len(events) != 0 {
		t.Fatalf("expected no events, got %d", len(events))
	}
}
