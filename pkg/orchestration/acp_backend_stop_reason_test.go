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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

// =============================================================================
// Stop-reason normalisation for the stream-json dialect.
//
// Captured from the real CLI (claude 2.1.x, --print --input-format stream-json
// --output-format stream-json --verbose), one turn looks like:
//
//	{"type":"system","subtype":"init",...}
//	{"type":"assistant","message":{...,"stop_reason":null,"content":[{"type":"text",...}]}}
//	{"type":"result","subtype":"success","is_error":false,"result":"OK"}
//
// The assistant message never carries a stop reason — completion is signalled
// by the result message. A finished turn therefore used to come back with an
// empty StopReason, which every SDK transport read as "the turn is not done"
// and reported as a contentless "Done: false". These tests pin the normalised
// contract: a completed stream-json turn reports StopReasonEndTurn, exactly as
// the JSON-RPC and one-shot backends already do.
// =============================================================================

// TestStreamJSONNullStopReasonHelper is a fake claude that reproduces the real
// CLI's message sequence: an assistant message with no stop_reason followed by
// a successful result message.
func TestStreamJSONNullStopReasonHelper(t *testing.T) {
	if os.Getenv("STREAM_JSON_NULL_STOP_REASON_HELPER") != "1" {
		return
	}
	sessionID := os.Getenv("STREAM_JSON_SESSION_ID")
	model := os.Getenv("STREAM_JSON_MODEL")

	writeMsg := func(m streamJSONMessage) {
		b, _ := json.Marshal(m)
		os.Stdout.Write(append(b, '\n'))
	}

	writeMsg(streamJSONMessage{Type: "system", Subtype: "init", SessionID: sessionID, Model: model})

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var in streamJSONUserMessage
		if json.Unmarshal(scanner.Bytes(), &in) != nil {
			continue
		}
		// Note the absent StopReason: the real CLI sends stop_reason: null.
		writeMsg(streamJSONMessage{
			Type: "assistant",
			Message: &streamJSONAssistant{
				Role:    "assistant",
				Content: []streamJSONContent{{Type: "text", Text: "echo:" + in.Message.Content}},
				Model:   model,
			},
		})
		writeMsg(streamJSONMessage{Type: "result", Subtype: "success", SessionID: sessionID, Result: "echo:" + in.Message.Content})
	}
	os.Exit(0)
}

// TestStreamJSONErrorResultHelper is a fake claude whose turn fails: it answers
// every prompt with an error result carrying the failure text.
func TestStreamJSONErrorResultHelper(t *testing.T) {
	if os.Getenv("STREAM_JSON_ERROR_RESULT_HELPER") != "1" {
		return
	}
	sessionID := os.Getenv("STREAM_JSON_SESSION_ID")

	writeMsg := func(m streamJSONMessage) {
		b, _ := json.Marshal(m)
		os.Stdout.Write(append(b, '\n'))
	}

	writeMsg(streamJSONMessage{Type: "system", Subtype: "init", SessionID: sessionID})

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		writeMsg(streamJSONMessage{
			Type:      "result",
			Subtype:   "error_during_execution",
			SessionID: sessionID,
			IsError:   true,
			Result:    "model provider rejected the request",
		})
	}
	os.Exit(0)
}

// TestPromptStreamJSON_CompletedTurnReportsStopReason is the regression test
// for the silent "Done: false": a turn whose assistant message omits
// stop_reason must still be reported as a completed turn with its text intact.
func TestPromptStreamJSON_CompletedTurnReportsStopReason(t *testing.T) {
	backend := newLifecycleCLIBackend(t, "TestStreamJSONNullStopReasonHelper", "STREAM_JSON_NULL_STOP_REASON_HELPER")

	sess, err := backend.Spawn(context.Background(), &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	resp, err := backend.Prompt(context.Background(), &PromptRequest{
		SessionID: sess.ID,
		Prompt:    "hello world",
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}

	if len(resp.Content) != 1 || resp.Content[0].Text != "echo:hello world" {
		t.Fatalf("Prompt() content = %+v, want the turn's text", resp.Content)
	}
	if resp.StopReason != StopReasonEndTurn {
		t.Errorf("StopReason = %q, want %q: a completed turn must not look unfinished",
			resp.StopReason, StopReasonEndTurn)
	}
}

// TestPromptStreamJSON_AgentReportedStopReasonWins asserts the normalisation
// only fills a gap: when the agent does report a stop reason (for example a
// truncated turn), that value is preserved.
func TestPromptStreamJSON_AgentReportedStopReasonWins(t *testing.T) {
	backend := newLifecycleCLIBackend(t, "TestStreamJSONStaysAliveHelper", "STREAM_JSON_STAYS_ALIVE_HELPER")

	sess, err := backend.Spawn(context.Background(), &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	resp, err := backend.Prompt(context.Background(), &PromptRequest{SessionID: sess.ID, Prompt: "hi"})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if resp.StopReason != StopReasonEndTurn {
		t.Errorf("StopReason = %q, want the agent-reported %q", resp.StopReason, StopReasonEndTurn)
	}
}

// TestPromptStreamJSON_ErrorResultSurfacesTypedError asserts a failed turn
// surfaces a typed StreamJSONError carrying the agent's own failure text — the
// other half of the contract: a prompt yields a real turn or a typed error,
// never an empty success.
func TestPromptStreamJSON_ErrorResultSurfacesTypedError(t *testing.T) {
	backend := newLifecycleCLIBackend(t, "TestStreamJSONErrorResultHelper", "STREAM_JSON_ERROR_RESULT_HELPER")

	sess, err := backend.Spawn(context.Background(), &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	resp, err := backend.Prompt(context.Background(), &PromptRequest{SessionID: sess.ID, Prompt: "do work"})
	if err == nil {
		t.Fatalf("Prompt() on a failed turn returned %+v, want an error", resp)
	}

	var streamErr *StreamJSONError
	if !errors.As(err, &streamErr) {
		t.Fatalf("error %v should be a *StreamJSONError", err)
	}
	if streamErr.Result != "model provider rejected the request" {
		t.Errorf("Result = %q, want the agent's failure text", streamErr.Result)
	}
}
