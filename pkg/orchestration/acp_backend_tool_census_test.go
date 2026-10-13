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
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"
)

// =============================================================================
// Per-turn tool census (go-quicbaas#799).
//
// A turn's tool use is reported on PromptResponse.ToolUse, whether or not the
// backend also keeps tool_use blocks in Content. These tests pin both halves of
// its contract: a
// backend that reports tool use counts every call, and reports zero rather
// than nothing for a turn that used none; a backend that does not report tool
// use leaves the census nil.
// =============================================================================

// toolCensusTurn is one stream-json turn as the CLI emits it: an assistant
// message with text and two tool calls, the tool results (on a user message,
// which must not be counted), and a second assistant message with one call.
var toolCensusTurn = []string{
	`{"type":"assistant","message":{"role":"assistant","model":"m","content":[` +
		`{"type":"text","text":"Reading two files."},` +
		`{"type":"tool_use","id":"toolu_01","name":"Read","input":{"file_path":"a.go"}},` +
		`{"type":"tool_use","id":"toolu_02","name":"Read","input":{"file_path":"b.go"}}]}}`,
	`{"type":"user","message":{"role":"user","content":[` +
		`{"type":"tool_result","tool_use_id":"toolu_01","content":"package a"},` +
		`{"type":"tool_result","tool_use_id":"toolu_02","content":"package b"}]}}`,
	`{"type":"assistant","message":{"role":"assistant","model":"m","content":[` +
		`{"type":"tool_use","id":"toolu_03","name":"Bash","input":{"command":"go test"}}]}}`,
	`{"type":"assistant","message":{"role":"assistant","model":"m","content":[{"type":"text","text":"done"}]}}`,
	`{"type":"result","subtype":"success","session_id":"census-session","result":"done"}`,
}

// TestStreamJSONToolCensusHelper is a fake claude that answers every prompt
// with toolCensusTurn. It is inert unless the parent process invokes it.
func TestStreamJSONToolCensusHelper(t *testing.T) {
	if os.Getenv("STREAM_JSON_TOOL_CENSUS_HELPER") != "1" {
		return
	}
	writeLine := func(line string) {
		if _, err := os.Stdout.WriteString(line + "\n"); err != nil {
			os.Exit(1)
		}
	}
	writeLine(`{"type":"system","subtype":"init","session_id":"census-session","model":"m"}`)

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		for _, line := range toolCensusTurn {
			writeLine(line)
		}
	}
	os.Exit(0)
}

// promptPersistent spawns a persistent session on backend and runs one turn.
func promptPersistent(t *testing.T, backend *ACPBackend) *PromptResponse {
	t.Helper()
	sess, err := backend.Spawn(context.Background(), &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	resp, err := backend.Prompt(context.Background(), &PromptRequest{SessionID: sess.ID, Prompt: "work"})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	return resp
}

func TestPromptStreamJSON_ToolCensusCountsEveryToolUseBlock(t *testing.T) {
	backend := newLifecycleCLIBackend(t, "TestStreamJSONToolCensusHelper", "STREAM_JSON_TOOL_CENSUS_HELPER")

	resp := promptPersistent(t, backend)

	if resp.ToolUse == nil || resp.ToolUse.Calls != 3 {
		t.Fatalf("ToolUse = %+v, want 3 calls: two on the first assistant message, one on the second", resp.ToolUse)
	}
	toolBlocks := 0
	for _, block := range resp.Content {
		if block.Type == ContentBlockToolUse {
			toolBlocks++
		}
	}
	if toolBlocks != resp.ToolUse.Calls {
		t.Errorf("Content carries %d tool_use blocks, census says %d: the two must agree", toolBlocks, resp.ToolUse.Calls)
	}
}

func TestPromptStreamJSON_TurnWithoutToolsReportsAZeroCensus(t *testing.T) {
	backend := newLifecycleCLIBackend(t, "TestStreamJSONNullStopReasonHelper", "STREAM_JSON_NULL_STOP_REASON_HELPER")

	resp := promptPersistent(t, backend)

	if resp.ToolUse == nil {
		t.Fatal("ToolUse = nil, want a zero census: the stream reports tool use, and this turn used none")
	}
	if resp.ToolUse.Calls != 0 {
		t.Errorf("ToolUse.Calls = %d, want 0", resp.ToolUse.Calls)
	}
}

// toolCallResponder plays an ACP JSON-RPC agent whose turn announces two tool
// calls, updates one of them, and streams its answer as notifications.
func toolCallResponder(r io.Reader, w io.WriteCloser, done <-chan struct{}) {
	defer w.Close()
	scanner := bufio.NewScanner(r)
	write := func(msg JSONRPCMessage) bool {
		data, err := json.Marshal(msg)
		if err != nil {
			return false
		}
		_, err = w.Write(append(data, '\n'))
		return err == nil
	}
	update := func(fields map[string]any) JSONRPCMessage {
		return JSONRPCMessage{JSONRPC: JSONRPCVersion, Method: "session/update", Params: map[string]any{
			"sessionId": "tc-session",
			"update":    fields,
		}}
	}
	for scanner.Scan() {
		select {
		case <-done:
			return
		default:
		}
		var msg JSONRPCMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil || msg.ID == nil {
			continue
		}
		resp := JSONRPCMessage{JSONRPC: JSONRPCVersion, ID: msg.ID}
		switch msg.Method {
		case "initialize":
			resp.Result = InitializeResult{AgentInfo: ServerInfo{Name: "agent", Version: "1"}}
		case "session/new":
			resp.Result = SessionNewResult{SessionID: "tc-session"}
		case "session/prompt":
			for _, u := range []map[string]any{
				{"sessionUpdate": "tool_call", "toolCallId": "tc1", "title": "Read a.go", "kind": "read", "status": "pending"},
				{"sessionUpdate": "tool_call_update", "toolCallId": "tc1", "status": "completed"},
				{"sessionUpdate": "tool_call", "toolCallId": "tc2", "kind": "execute", "status": "pending"},
				{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "done"}},
			} {
				if !write(update(u)) {
					return
				}
			}
			resp.Result = map[string]string{"stopReason": "end_turn"}
		default:
			resp.Result = map[string]string{"status": "ok"}
		}
		if !write(resp) {
			return
		}
	}
}

func TestPromptJSONRPC_ToolCensusCountsToolCallUpdates(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{Command: "agent", DefaultModel: "m"})
	done := make(chan struct{})
	defer close(done)
	backend.transportFactory = func(string, []string, string, []string) (*StdioTransport, error) {
		serverRead, clientWrite, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		clientRead, serverWrite, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		go toolCallResponder(serverRead, serverWrite, done)
		return newStdioTransportFromPipes(clientWrite, clientRead), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := backend.Spawn(ctx, &SpawnRequest{ProjectID: "p1", WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { _ = backend.Close(context.Background(), sess.ID) }()

	resp, err := backend.Prompt(ctx, &PromptRequest{SessionID: sess.ID, Prompt: "work"})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if resp.ToolUse == nil || resp.ToolUse.Calls != 2 {
		t.Fatalf("ToolUse = %+v, want 2 calls: a tool_call_update is progress on a call, not a new one", resp.ToolUse)
	}
}
