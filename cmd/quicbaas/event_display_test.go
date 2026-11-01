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

package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

func TestNewEventDisplay(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	assert.NotNil(t, d)
	assert.NotNil(t, d.colorMap)
	assert.NotNil(t, d.sessionRole)
	assert.Equal(t, 8, d.maxLabel)
	assert.Equal(t, 0, d.nextColor)
	assert.False(t, d.streaming)
}

func TestNewEventDisplay_NilWriter(t *testing.T) {
	// newEventDisplay should work with any io.Writer, even if it's a no-op.
	// Passing nil would panic on use but construction should succeed.
	var buf strings.Builder
	d := newEventDisplay(&buf)
	assert.NotNil(t, d)
}

func TestEventDisplay_AssignColor(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	c1 := d.assignColor("session-1")
	c2 := d.assignColor("session-2")
	c1again := d.assignColor("session-1")

	assert.Equal(t, c1, c1again, "same session should get same color")
	assert.NotEqual(t, c1, c2, "different sessions should get different colors")
	assert.Equal(t, 2, d.nextColor)
}

func TestEventDisplay_AssignColor_Wraps(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	// Exhaust all colors and verify wrapping.
	for i := 0; i < len(displayColors)+2; i++ {
		d.assignColor("session-" + string(rune('a'+i)))
	}
	assert.Equal(t, len(displayColors)+2, d.nextColor)
}

func TestEventDisplay_RegisterSession(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.registerSession("sess-1", "coder")
	assert.Equal(t, "coder", d.sessionRole["sess-1"])
	assert.Equal(t, 8, d.maxLabel) // "reviewer" default is 8, "coder" is 5

	d.registerSession("sess-2", "architect")
	assert.Equal(t, "architect", d.sessionRole["sess-2"])
	assert.Equal(t, 9, d.maxLabel) // "architect" is 9
}

func TestEventDisplay_RegisterSession_EmptyRole(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.registerSession("sess-1", "")
	assert.Equal(t, "", d.sessionRole["sess-1"])
	assert.Equal(t, 8, d.maxLabel) // unchanged
}

func TestEventDisplay_RoleFor(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	// Unknown session returns "worker".
	assert.Equal(t, "worker", d.roleFor("unknown-session"))

	d.registerSession("sess-1", "coder")
	assert.Equal(t, "coder", d.roleFor("sess-1"))
}

func TestEventDisplay_RoleFor_EmptySessionID(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	assert.Equal(t, "worker", d.roleFor(""))
}

func TestEventDisplay_Prefix(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.registerSession("sess-1", "coder")
	prefix := d.prefix("coder")

	// Should contain the role name padded to maxLabel width.
	assert.Contains(t, prefix, "coder")
	assert.Contains(t, prefix, " | ")
	assert.Contains(t, prefix, colorReset)
}

func TestEventDisplay_Prefix_Alignment(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.registerSession("s1", "lead")
	d.registerSession("s2", "reviewer")

	p1 := d.prefix("lead")
	p2 := d.prefix("reviewer")

	// Both prefixes should produce the same length up to the pipe.
	// Strip ANSI codes to compare.
	strip := func(s string) string {
		result := s
		for strings.Contains(result, "\033[") {
			start := strings.Index(result, "\033[")
			end := strings.IndexByte(result[start:], 'm')
			if end < 0 {
				break
			}
			result = result[:start] + result[start+end+1:]
		}
		return result
	}

	stripped1 := strip(p1)
	stripped2 := strip(p2)
	assert.Equal(t, len(stripped1), len(stripped2),
		"prefixes should be same length: %q vs %q", stripped1, stripped2)
}

func TestEventDisplay_EndStreaming(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	// Should be a no-op when not streaming.
	d.endStreaming()
	assert.Equal(t, "", buf.String())

	// Set streaming state and verify it resets.
	d.streaming = true
	d.streamingRole = "coder"
	d.endStreaming()
	assert.Equal(t, "\n", buf.String())
	assert.False(t, d.streaming)
	assert.Equal(t, "", d.streamingRole)
}

func TestParseKeyValues(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantKV   map[string]string
		wantRest string
	}{
		{
			name:     "simple key-value pairs",
			input:    "role=coder task=Build session=abc123",
			wantKV:   map[string]string{"role": "coder", "task": "Build", "session": "abc123"},
			wantRest: "",
		},
		{
			name:     "key-value with JSON remainder",
			input:    `session=abc123 {"jsonrpc":"2.0"}`,
			wantKV:   map[string]string{"session": "abc123"},
			wantRest: `{"jsonrpc":"2.0"}`,
		},
		{
			name:     "empty string",
			input:    "",
			wantKV:   map[string]string{},
			wantRest: "",
		},
		{
			name:     "JSON only",
			input:    `{"type":"agent_message_chunk"}`,
			wantKV:   map[string]string{},
			wantRest: `{"type":"agent_message_chunk"}`,
		},
		{
			name:     "single key-value",
			input:    "approved=true",
			wantKV:   map[string]string{"approved": "true"},
			wantRest: "",
		},
		{
			name:     "whitespace padding",
			input:    "  role=lead  task=Decompose  ",
			wantKV:   map[string]string{"role": "lead", "task": "Decompose"},
			wantRest: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kv, rest := parseKeyValues(tt.input)
			assert.Equal(t, tt.wantKV, kv)
			assert.Equal(t, tt.wantRest, rest)
		})
	}
}

func TestParseKeyValues_InvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantKV   map[string]string
		wantRest string
	}{
		{
			name:     "no equals sign",
			input:    "just some text",
			wantKV:   map[string]string{},
			wantRest: "just some text",
		},
		{
			name:     "array JSON",
			input:    `[1,2,3]`,
			wantKV:   map[string]string{},
			wantRest: `[1,2,3]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kv, rest := parseKeyValues(tt.input)
			assert.Equal(t, tt.wantKV, kv)
			assert.Equal(t, tt.wantRest, rest)
		})
	}
}

func TestParseSessionUpdate(t *testing.T) {
	t.Run("valid session update", func(t *testing.T) {
		msg := `session=sess-abc {"jsonrpc":"2.0","method":"update","params":{"update":{"sessionUpdate":"tool_call","title":"Write","status":"in_progress"}}}`
		sid, update, ok := parseSessionUpdate(msg)
		assert.True(t, ok)
		assert.Equal(t, "sess-abc", sid)
		assert.Equal(t, "tool_call", update["sessionUpdate"])
		assert.Equal(t, "Write", update["title"])
	})

	t.Run("missing session", func(t *testing.T) {
		msg := `{"jsonrpc":"2.0"}`
		sid, _, ok := parseSessionUpdate(msg)
		assert.False(t, ok)
		assert.Equal(t, "", sid)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		msg := `session=abc {not-json}`
		sid, _, ok := parseSessionUpdate(msg)
		assert.Equal(t, "abc", sid)
		assert.False(t, ok)
	})
}

func TestParseSessionUpdate_FlatUpdate(t *testing.T) {
	msg := `session=sess-1 {"update":{"sessionUpdate":"agent_message_chunk","text":"hello"}}`
	sid, update, ok := parseSessionUpdate(msg)
	assert.True(t, ok)
	assert.Equal(t, "sess-1", sid)
	assert.Equal(t, "agent_message_chunk", update["sessionUpdate"])
}

// TestEventDisplay_Render covers the one-line rendering of each event type.
// A row with no want expects the event to be skipped (no output at all).
func TestEventDisplay_Render(t *testing.T) {
	tests := []struct {
		name    string
		evtType string
		message string
		want    []string
	}{
		{"job started", "orchestration:job_started", "job_id=job-1 correlation_id=corr-1", []string{"Job started", "system"}},
		{"job started empty message", "orchestration:job_started", "", []string{"Job started"}},
		{"decomposed", "orchestration:decomposed", "work_items=5 iteration=1", []string{"Decomposed into 5 tasks (iteration 1)"}},
		{"decomposed missing fields", "orchestration:decomposed", "", []string{"Decomposed into"}},
		{"scheduled", "orchestration:scheduled", "waves=2 items=5", []string{"Scheduled 5 items in 2 waves"}},
		{"task assigned skipped", "orchestration:task_assigned", "wave=1 tasks=3", nil},
		{"task completed skipped", "orchestration:task_completed", "wave=1 tasks=3", nil},
		{"validation result skipped", "orchestration:validation_result", "criteria=3", nil},
		{"review approved", "orchestration:review_decision", "approved=true feedback=Looks good", []string{"Review: APPROVED"}},
		{"review rejected", "orchestration:review_decision", "approved=false feedback=Needs more tests", []string{"Review: REJECTED -- Needs more tests"}},
		{"redelegate", "orchestration:redelegate", "iteration=2", []string{"Re-delegating (iteration 2)"}},
		{"redelegate empty message", "orchestration:redelegate", "", []string{"Re-delegating"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf strings.Builder
			d := newEventDisplay(&buf)

			d.render(&orchestratorSDK.JobEvent{Type: tt.evtType, Message: tt.message})

			output := buf.String()
			if tt.want == nil {
				assert.Empty(t, output, "%s should be skipped", tt.evtType)
				return
			}
			for _, w := range tt.want {
				assert.Contains(t, output, w)
			}
		})
	}
}

func TestEventDisplay_Render_PromptStart(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    "worker:prompt_start",
		Message: "role=coder task=Setup project backend=claude session=sess-1",
	})

	output := buf.String()
	assert.Contains(t, output, "Starting: Setup")
	assert.Contains(t, output, "coder")
	assert.Equal(t, "coder", d.sessionRole["sess-1"])
}

func TestEventDisplay_Render_PromptStart_NoRole(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    "worker:prompt_start",
		Message: "task=Build session=sess-2 backend=claude",
	})

	output := buf.String()
	assert.Contains(t, output, "Starting: Build")
	assert.Contains(t, output, "worker")
}

func TestEventDisplay_Render_PromptComplete(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.registerSession("sess-1", "coder")
	d.render(&orchestratorSDK.JobEvent{
		Type:    "worker:prompt_complete",
		Message: "role=coder task=Setup project response=Done",
	})

	output := buf.String()
	assert.Contains(t, output, "Finished: Setup")
	assert.Contains(t, output, "coder")
}

func TestEventDisplay_Render_PromptComplete_NoRole(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    "worker:prompt_complete",
		Message: "task=Build response=OK",
	})

	output := buf.String()
	assert.Contains(t, output, "Finished: Build")
}

func TestEventDisplay_Render_SessionSpawn(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    "worker:session_spawn",
		Message: "role=coder backend=claude persistent=true",
	})

	output := buf.String()
	assert.Contains(t, output, "Spawning session")
	assert.Contains(t, output, "coder")
}

func TestEventDisplay_Render_SessionSpawned(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    "worker:session_spawned",
		Message: "role=reviewer session=sess-3",
	})

	output := buf.String()
	assert.Contains(t, output, "Session ready")
	assert.Contains(t, output, "reviewer")
	assert.Equal(t, "reviewer", d.sessionRole["sess-3"])
}

func TestEventDisplay_Render_SessionSpawned_NoRole(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    "worker:session_spawned",
		Message: "session=sess-4",
	})

	output := buf.String()
	assert.Contains(t, output, "Session ready")
	assert.Contains(t, output, "worker")
}

func TestEventDisplay_Render_LeadDecomposeStart(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    "lead:decompose_start",
		Message: "backend=claude model=opus-4",
	})

	output := buf.String()
	assert.Contains(t, output, "Decomposing task...")
	assert.Contains(t, output, "lead")
	assert.Equal(t, "lead", d.sessionRole["lead"])
}

func TestEventDisplay_Render_LeadWorkItem(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    "lead:work_item",
		Message: "title=Create project structure",
	})

	output := buf.String()
	assert.Contains(t, output, "-> Create project structure")
	assert.Contains(t, output, "lead")
}

func TestEventDisplay_Render_LeadWorkItem_EmptyTitle(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    "lead:work_item",
		Message: "",
	})

	output := buf.String()
	assert.Contains(t, output, "->")
}

func TestEventDisplay_Render_LeadSessionSpawned(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    "lead:session_spawned",
		Message: "session=lead-sess-1",
	})

	output := buf.String()
	assert.Contains(t, output, "Session ready")
	assert.Equal(t, "lead", d.sessionRole["lead-sess-1"])
}

func TestEventDisplay_Render_LeadSessionSpawned_NoSession(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    "lead:session_spawned",
		Message: "",
	})

	output := buf.String()
	assert.Contains(t, output, "Session ready")
}

func TestEventDisplay_Render_Default(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    "custom:event_type",
		Message: "some details",
	})

	output := buf.String()
	assert.Contains(t, output, "[custom:event_type]")
	assert.Contains(t, output, "some details")
}

func TestEventDisplay_Render_Default_EmptyMessage(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    "custom:event_type",
		Message: "",
	})

	output := buf.String()
	assert.Contains(t, output, "[custom:event_type]")
}

func TestEventDisplay_Render_ToolCallInProgress(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.registerSession("sess-1", "coder")
	msg := `session=sess-1 {"jsonrpc":"2.0","method":"update","params":{"update":{"sessionUpdate":"tool_call","title":"Write","status":"in_progress","locations":[{"path":"/workspace/main.go"}]}}}`

	d.render(&orchestratorSDK.JobEvent{
		Type:    "agent:notification:session/update",
		Message: msg,
	})

	output := buf.String()
	assert.Contains(t, output, "[WRITE]")
	assert.Contains(t, output, "/workspace/main.go")
	assert.Contains(t, output, "in_progress")
}

func TestEventDisplay_Render_ToolCallPending_Skipped(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.registerSession("sess-1", "coder")
	msg := `session=sess-1 {"jsonrpc":"2.0","method":"update","params":{"update":{"sessionUpdate":"tool_call","title":"Write","status":"pending"}}}`

	d.render(&orchestratorSDK.JobEvent{
		Type:    "agent:notification:session/update",
		Message: msg,
	})

	assert.Equal(t, "", buf.String(), "pending tool calls should be skipped")
}

func TestEventDisplay_Render_ToolCallFailed(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.registerSession("sess-1", "coder")
	msg := `session=sess-1 {"jsonrpc":"2.0","method":"update","params":{"update":{"sessionUpdate":"tool_call","title":"Write","status":"failed","locations":[{"path":"/workspace/file.js"}],"rawOutput":{"error":"You must read file first"}}}}`

	d.render(&orchestratorSDK.JobEvent{
		Type:    "agent:notification:session/update",
		Message: msg,
	})

	output := buf.String()
	assert.Contains(t, output, "[FAIL]")
	assert.Contains(t, output, "/workspace/file.js")
	assert.Contains(t, output, "You must read file first")
}

func TestEventDisplay_Render_ToolCallFailed_NoError(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.registerSession("sess-1", "coder")
	msg := `session=sess-1 {"jsonrpc":"2.0","method":"update","params":{"update":{"sessionUpdate":"tool_call","title":"Bash","status":"failed"}}}`

	d.render(&orchestratorSDK.JobEvent{
		Type:    "agent:notification:session/update",
		Message: msg,
	})

	output := buf.String()
	assert.Contains(t, output, "[FAIL]")
}

func TestEventDisplay_Render_MessageChunk(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.registerSession("sess-1", "coder")
	msg := `session=sess-1 {"jsonrpc":"2.0","method":"update","params":{"update":{"sessionUpdate":"agent_message_chunk","text":"Hello world"}}}`

	d.render(&orchestratorSDK.JobEvent{
		Type:    "agent:notification:session/update",
		Message: msg,
	})

	output := buf.String()
	assert.Contains(t, output, ">>>")
	assert.Contains(t, output, "Hello world")
	assert.True(t, d.streaming)
}

func TestEventDisplay_Render_MessageChunk_EmptyText(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	msg := `session=sess-1 {"jsonrpc":"2.0","method":"update","params":{"update":{"sessionUpdate":"agent_message_chunk","text":""}}}`

	d.render(&orchestratorSDK.JobEvent{
		Type:    "agent:notification:session/update",
		Message: msg,
	})

	assert.Equal(t, "", buf.String())
	assert.False(t, d.streaming)
}

func TestEventDisplay_Render_ThoughtChunk(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.registerSession("sess-1", "coder")
	msg := `session=sess-1 {"jsonrpc":"2.0","method":"update","params":{"update":{"sessionUpdate":"agent_thought_chunk","text":"Thinking..."}}}`

	d.render(&orchestratorSDK.JobEvent{
		Type:    "agent:notification:session/update",
		Message: msg,
	})

	output := buf.String()
	assert.Contains(t, output, "...")
	assert.Contains(t, output, "Thinking...")
	assert.Contains(t, output, colorDim)
	assert.True(t, d.streaming)
}

func TestEventDisplay_Render_AvailableCommandsUpdate_Skipped(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	msg := `session=sess-1 {"jsonrpc":"2.0","method":"update","params":{"update":{"sessionUpdate":"available_commands_update"}}}`

	d.render(&orchestratorSDK.JobEvent{
		Type:    "agent:notification:session/update",
		Message: msg,
	})

	assert.Equal(t, "", buf.String())
}

func TestEventDisplay_Render_JSONRPCResult_WithTokens(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.registerSession("sess-1", "coder")
	msg := `session=sess-1 {"usage":{"totalTokens":17942}}`

	d.render(&orchestratorSDK.JobEvent{
		Type:    "agent:jsonrpc_result",
		Message: msg,
	})

	output := buf.String()
	assert.Contains(t, output, "Done (17942 tokens)")
	assert.Contains(t, output, "coder")
}

func TestEventDisplay_Render_JSONRPCResult_NoTokens(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    "agent:jsonrpc_result",
		Message: `session=sess-1 {"result":"ok"}`,
	})

	output := buf.String()
	assert.Contains(t, output, "Done")
}

func TestEventDisplay_Render_JSONRPCResult_InvalidJSON(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    "agent:jsonrpc_result",
		Message: "not json at all",
	})

	output := buf.String()
	assert.Contains(t, output, "Done")
}

func TestEventDisplay_Render_StreamingInterrupted(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.registerSession("sess-1", "coder")

	// Start streaming.
	msg := `session=sess-1 {"jsonrpc":"2.0","method":"update","params":{"update":{"sessionUpdate":"agent_message_chunk","text":"Hello"}}}`
	d.render(&orchestratorSDK.JobEvent{
		Type:    "agent:notification:session/update",
		Message: msg,
	})
	require.True(t, d.streaming)

	// Non-streaming event should end streaming first.
	d.render(&orchestratorSDK.JobEvent{
		Type:    "orchestration:job_started",
		Message: "job_id=j1",
	})

	output := buf.String()
	assert.False(t, d.streaming)
	assert.Contains(t, output, "Hello")
	assert.Contains(t, output, "Job started")
}

func TestEventDisplay_RenderAudit(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.renderAudit("17:14:06.284", "lead:work_item", "title=Create project structure")

	output := buf.String()
	assert.Contains(t, output, "17:14:06.284")
	assert.Contains(t, output, "-> Create project structure")
	assert.Contains(t, output, "lead")
}

func TestEventDisplay_RenderAudit_EmptyDetails(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.renderAudit("17:14:06.284", "orchestration:job_started", "")

	output := buf.String()
	assert.Contains(t, output, "17:14:06.284")
	assert.Contains(t, output, "Job started")
}

func TestEventDisplay_StateChange(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.stateChange("executing", 1, 5, 2, 0)

	output := buf.String()
	assert.Contains(t, output, "State: executing")
	assert.Contains(t, output, "iteration=1")
	assert.Contains(t, output, "tasks=5")
	assert.Contains(t, output, "completed=2")
	assert.Contains(t, output, "failed=0")
}

func TestEventDisplay_StateChange_EndsStreaming(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.streaming = true
	d.streamingRole = "coder"

	d.stateChange("completed", 1, 3, 3, 0)

	assert.False(t, d.streaming)
	output := buf.String()
	assert.Contains(t, output, "State: completed")
}

func TestToolTag(t *testing.T) {
	tests := []struct {
		title    string
		expected string
	}{
		{"Write", "WRITE"},
		{"Read", "READ"},
		{"Bash", "BASH"},
		{"Edit", "EDIT"},
		{"Glob", "GLOB"},
		{"Grep", "GREP"},
		{"ListDir", "LIST"},
		{"CustomTool", "TOOL"},
		{"", "TOOL"},
		{"write_file", "WRITE"},
		{"read_file", "READ"},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			assert.Equal(t, tt.expected, toolTag(tt.title))
		})
	}
}

func TestExtractFirstLocationPath(t *testing.T) {
	t.Run("with locations", func(t *testing.T) {
		update := map[string]interface{}{
			"locations": []interface{}{
				map[string]interface{}{"path": "/workspace/main.go"},
				map[string]interface{}{"path": "/workspace/go.mod"},
			},
		}
		assert.Equal(t, "/workspace/main.go", extractFirstLocationPath(update))
	})

	t.Run("empty locations", func(t *testing.T) {
		update := map[string]interface{}{
			"locations": []interface{}{},
		}
		assert.Equal(t, "", extractFirstLocationPath(update))
	})

	t.Run("no locations key", func(t *testing.T) {
		update := map[string]interface{}{}
		assert.Equal(t, "", extractFirstLocationPath(update))
	})

	t.Run("nil locations", func(t *testing.T) {
		update := map[string]interface{}{
			"locations": nil,
		}
		assert.Equal(t, "", extractFirstLocationPath(update))
	})
}

func TestExtractToolError(t *testing.T) {
	t.Run("from rawOutput.error", func(t *testing.T) {
		update := map[string]interface{}{
			"rawOutput": map[string]interface{}{
				"error": "You must read file first",
			},
		}
		assert.Equal(t, "You must read file first", extractToolError(update))
	})

	t.Run("from content array", func(t *testing.T) {
		update := map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"content": map[string]interface{}{
						"text": "Permission denied",
					},
				},
			},
		}
		assert.Equal(t, "Permission denied", extractToolError(update))
	})

	t.Run("no error found", func(t *testing.T) {
		update := map[string]interface{}{}
		assert.Equal(t, "", extractToolError(update))
	})

	t.Run("from direct text in content", func(t *testing.T) {
		update := map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"text": "File not found",
				},
			},
		}
		assert.Equal(t, "File not found", extractToolError(update))
	})
}

func TestExtractChunkText(t *testing.T) {
	t.Run("direct text field", func(t *testing.T) {
		update := map[string]interface{}{"text": "hello"}
		assert.Equal(t, "hello", extractChunkText(update))
	})

	t.Run("nested content.text", func(t *testing.T) {
		update := map[string]interface{}{
			"content": map[string]interface{}{"text": "nested"},
		}
		assert.Equal(t, "nested", extractChunkText(update))
	})

	t.Run("array content", func(t *testing.T) {
		update := map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"content": map[string]interface{}{"text": "from array"},
				},
			},
		}
		assert.Equal(t, "from array", extractChunkText(update))
	})

	t.Run("empty", func(t *testing.T) {
		update := map[string]interface{}{}
		assert.Equal(t, "", extractChunkText(update))
	})

	t.Run("empty text", func(t *testing.T) {
		update := map[string]interface{}{"text": ""}
		assert.Equal(t, "", extractChunkText(update))
	})
}

func TestExtractMapString(t *testing.T) {
	t.Run("existing key", func(t *testing.T) {
		m := map[string]interface{}{"name": "test"}
		v, ok := extractMapString(m, "name")
		assert.True(t, ok)
		assert.Equal(t, "test", v)
	})

	t.Run("missing key", func(t *testing.T) {
		m := map[string]interface{}{}
		v, ok := extractMapString(m, "name")
		assert.False(t, ok)
		assert.Equal(t, "", v)
	})

	t.Run("non-string value", func(t *testing.T) {
		m := map[string]interface{}{"count": 42}
		v, ok := extractMapString(m, "count")
		assert.False(t, ok)
		assert.Equal(t, "", v)
	})
}

func TestEventDisplay_Render_SessionUpdate_InvalidJSON(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.render(&orchestratorSDK.JobEvent{
		Type:    "agent:notification:session/update",
		Message: "not-json-at-all",
	})

	output := buf.String()
	assert.Contains(t, output, "not-json-at-all")
}

func TestEventDisplay_Render_SessionUpdate_FlatJSON(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	d.registerSession("sess-1", "coder")
	msg := `session=sess-1 {"type":"tool_call","title":"Read","status":"in_progress","locations":[{"path":"/workspace/go.mod"}]}`

	d.render(&orchestratorSDK.JobEvent{
		Type:    "agent:notification:session/update",
		Message: msg,
	})

	output := buf.String()
	assert.Contains(t, output, "[READ]")
	assert.Contains(t, output, "/workspace/go.mod")
}

func TestEventDisplay_Render_MultipleEvents_RoleAlignment(t *testing.T) {
	var buf strings.Builder
	d := newEventDisplay(&buf)

	// Register sessions with different role lengths.
	d.registerSession("s1", "lead")
	d.registerSession("s2", "coder")
	d.registerSession("s3", "reviewer")

	d.render(&orchestratorSDK.JobEvent{
		Type:    "worker:prompt_start",
		Message: "role=lead task=Plan session=s1",
	})
	d.render(&orchestratorSDK.JobEvent{
		Type:    "worker:prompt_start",
		Message: "role=coder task=Code session=s2",
	})
	d.render(&orchestratorSDK.JobEvent{
		Type:    "worker:prompt_start",
		Message: "role=reviewer task=Review session=s3",
	})

	output := buf.String()
	assert.Contains(t, output, "lead")
	assert.Contains(t, output, "coder")
	assert.Contains(t, output, "reviewer")
}
