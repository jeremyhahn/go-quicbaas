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
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/api/proto"
)

// emitLine writes one raw stream-json line, for records the typed emit
// cannot produce (a malformed block).
func (a *scriptedAgent) emitLine(t *testing.T, line string) {
	t.Helper()
	if _, err := a.stdoutW.Write([]byte(line + "\n")); err != nil {
		t.Errorf("write a record: %v", err)
	}
}

// promptScripted opens a stream-json session against a scripted agent and
// runs one prompt whose turn is the given stream-json lines.
func promptScripted(t *testing.T, lines ...string) *PromptResponse {
	t.Helper()
	agent := newScriptedAgent()
	defer agent.close()
	b, open := openScripted(t, agent, time.Second, func(string) { agent.reply(t, "hello") })
	sess := sessionOf(open, agent)
	go func() {
		agent.readPrompt(t)
		for _, line := range lines {
			agent.emitLine(t, line)
		}
	}()
	resp, err := b.promptStreamJSON(context.Background(), sess, &PromptRequest{Prompt: "q"})
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	return resp
}

// toolTurn is a stream-json turn with every block type the Claude CLI
// publishes: reasoning, words, two tool calls and their results (one
// failed), then the answer.
var toolTurn = []string{
	`{"type":"assistant","message":{"role":"assistant","model":"m","content":[{"type":"thinking","thinking":"plan the read"},{"type":"text","text":"Looking."},{"type":"tool_use","id":"tu_1","name":"Bash","input":{"command":"ls"}}]}}`,
	`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"a.go\nb.go"}]}}`,
	`{"type":"assistant","message":{"role":"assistant","model":"m","content":[{"type":"tool_use","id":"tu_2","name":"Read","input":{"file_path":"c.go"}}]}}`,
	`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_2","content":[{"type":"text","text":"no such file"}],"is_error":true}]}}`,
	`{"type":"assistant","message":{"role":"assistant","model":"m","content":[{"type":"text","text":"Done."}]}}`,
	`{"type":"result","subtype":"success","result":"Done."}`,
}

func TestPromptStreamJSON_KeepsEveryBlockOfTheTurn(t *testing.T) {
	resp := promptScripted(t, toolTurn...)

	want := []ContentBlock{
		{Type: ContentBlockThinking, Bytes: len("plan the read")},
		{Type: ContentBlockText, Text: "Looking."},
		{Type: ContentBlockToolUse, ToolName: "Bash", ToolUseID: "tu_1", Bytes: len(`{"command":"ls"}`)},
		{Type: ContentBlockToolResult, ToolUseID: "tu_1", Bytes: len(`"a.go\nb.go"`)},
		{Type: ContentBlockToolUse, ToolName: "Read", ToolUseID: "tu_2", Bytes: len(`{"file_path":"c.go"}`)},
		{Type: ContentBlockToolResult, ToolUseID: "tu_2", IsError: true, Bytes: len(`[{"type":"text","text":"no such file"}]`)},
		{Type: ContentBlockText, Text: "Done."},
	}
	if len(resp.Content) != len(want) {
		t.Fatalf("content = %+v, want %d blocks", resp.Content, len(want))
	}
	for i := range want {
		if resp.Content[i] != want[i] {
			t.Errorf("block %d = %+v, want %+v", i, resp.Content[i], want[i])
		}
	}
}

// TestPromptStreamJSON_ATextTurnKeepsOneTextBlock: the result text is not
// added beside streamed text that already carries the answer.
func TestPromptStreamJSON_ATextTurnKeepsOneTextBlock(t *testing.T) {
	resp := promptScripted(t,
		`{"type":"assistant","message":{"role":"assistant","model":"m","content":[{"type":"text","text":"just words"}]}}`,
		`{"type":"result","subtype":"success","result":"just words"}`,
	)
	if len(resp.Content) != 1 || resp.Content[0].Type != ContentBlockText {
		t.Fatalf("content = %+v, want the one text block", resp.Content)
	}
}

func TestPromptStreamJSON_AMalformedBlockKeepsTheText(t *testing.T) {
	resp := promptScripted(t,
		// "name" must be a string: this block cannot be decoded, its
		// neighbours can.
		`{"type":"assistant","message":{"role":"assistant","model":"m","content":[{"type":"text","text":"kept"},{"type":"tool_use","id":"tu_9","name":42}]}}`,
		`{"type":"result","subtype":"success","result":"kept"}`,
	)
	if len(resp.Content) != 2 {
		t.Fatalf("content = %+v, want the text and one unreadable block", resp.Content)
	}
	if resp.Content[0] != (ContentBlock{Type: ContentBlockText, Text: "kept"}) {
		t.Errorf("block 0 = %+v, want the text", resp.Content[0])
	}
	if resp.Content[1].Type != ContentBlockUnreadable || resp.Content[1].Bytes == 0 {
		t.Errorf("block 1 = %+v, want an unreadable block that says how much was lost", resp.Content[1])
	}
}

func TestPromptStreamJSON_ResultTextIsAddedWhenOnlyToolsStreamed(t *testing.T) {
	resp := promptScripted(t,
		`{"type":"assistant","message":{"role":"assistant","model":"m","content":[{"type":"tool_use","id":"tu_1","name":"Bash","input":{}}]}}`,
		`{"type":"result","subtype":"success","result":"from the result"}`,
	)
	if len(resp.Content) != 2 || resp.Content[1] != (ContentBlock{Type: ContentBlockText, Text: "from the result"}) {
		t.Fatalf("content = %+v, want the tool block then the result text", resp.Content)
	}
}

func TestDecodeStreamJSONAssistant_ContentThatIsNotAListFails(t *testing.T) {
	var a streamJSONAssistant
	if err := a.UnmarshalJSON([]byte(`{"role":"user","content":"plain"}`)); err == nil {
		t.Fatal("content that is not a block list decoded without error")
	}
}

func TestRemoteACPBackend_PromptCarriesTheTurnsToolBlocks(t *testing.T) {
	t.Parallel()
	backend := remoteBackendWith(t, &proto.PromptSessionResponse{
		SessionId: "sess-1",
		Content: []*proto.ContentBlock{
			{Type: ContentBlockToolUse, ToolName: "Bash", ToolUseId: "tu_1", Bytes: 16},
			{Type: ContentBlockToolResult, ToolUseId: "tu_1", IsError: true, Bytes: 9},
			{Type: ContentBlockText, Text: "done"},
		},
	})
	var chunks []string
	resp, err := backend.Prompt(context.Background(), &PromptRequest{
		SessionID: "sess-1", Prompt: "go",
		EventCallback: func(ev *SessionEvent) {
			if ev.AssistantChunk != nil {
				chunks = append(chunks, ev.AssistantChunk.Text)
			}
		},
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	want := []ContentBlock{
		{Type: ContentBlockToolUse, ToolName: "Bash", ToolUseID: "tu_1", Bytes: 16},
		{Type: ContentBlockToolResult, ToolUseID: "tu_1", IsError: true, Bytes: 9},
		{Type: ContentBlockText, Text: "done"},
	}
	if len(resp.Content) != len(want) {
		t.Fatalf("content = %+v, want %d blocks", resp.Content, len(want))
	}
	for i := range want {
		if resp.Content[i] != want[i] {
			t.Errorf("block %d = %+v, want %+v", i, resp.Content[i], want[i])
		}
	}
	if resp.ToolUse != nil {
		t.Errorf("ToolUse = %+v, want nil: the fleet wire carries no census, so the blocks are not a complete account", resp.ToolUse)
	}
	if len(chunks) != 1 || chunks[0] != "done" {
		t.Errorf("assistant chunks = %q, want only the words", chunks)
	}
}
