// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package orchestrator

import (
	"encoding/json"
	"errors"
	"testing"
)

// livePromptPayload is the exact body a live quicbaasd returned for
// POST /api/v1/orchestrator/sessions/{id}/prompt against a claude ACP session.
// Decoding it straight into PromptSessionResponse yields an all-zero struct —
// the silent "Done: false" — because the two shapes share only session_id.
const livePromptPayload = `{"session_id":"ce4b6a40-9329-4b4d-bf72-4cbf598c3423",` +
	`"content":[{"type":"text","text":"SELF-HOSTING ONLINE"}],` +
	`"model":"claude-fable-5","stop_reason":"end_turn","timestamp":1786207030175886792}`

// liveListSessionsPayload is the body a live quicbaasd returned for
// GET /api/v1/orchestrator/sessions. Note "state", not "status".
const liveListSessionsPayload = `{"sessions":[{"session_id":"ce4b6a40-9329-4b4d-bf72-4cbf598c3423",` +
	`"project_id":"604c52b4-d26b-4fc0-b744-30c7763d4fa6","backend":"claude","model":"claude-fable-5",` +
	`"state":"active","created_at":1786205526,"last_activity":1786205516508700651}],` +
	`"timestamp":1786207020520347903}`

// liveSessionStatusPayload is the body a live quicbaasd returned for
// GET /api/v1/orchestrator/sessions/{id}.
const liveSessionStatusPayload = `{"session_id":"ce4b6a40-9329-4b4d-bf72-4cbf598c3423",` +
	`"state":"active","last_activity":1786205526,"uptime":1494,"timestamp":1786207020331543767}`

func TestPromptSessionWireResponse(t *testing.T) {
	t.Run("live service payload yields the turn text and a done turn", func(t *testing.T) {
		var wire PromptSessionWire
		if err := json.Unmarshal([]byte(livePromptPayload), &wire); err != nil {
			t.Fatalf("decode live payload: %v", err)
		}

		resp, err := wire.Response()
		if err != nil {
			t.Fatalf("Response() error = %v", err)
		}
		if resp.Response != "SELF-HOSTING ONLINE" {
			t.Errorf("Response = %q, want the agent's text", resp.Response)
		}
		if !resp.Done {
			t.Error("Done = false for a completed turn")
		}
		if resp.SessionID != "ce4b6a40-9329-4b4d-bf72-4cbf598c3423" {
			t.Errorf("SessionID = %q", resp.SessionID)
		}
		if resp.Model != "claude-fable-5" {
			t.Errorf("Model = %q", resp.Model)
		}
		if resp.StopReason != "end_turn" {
			t.Errorf("StopReason = %q", resp.StopReason)
		}
	})

	t.Run("concatenates text blocks and ignores non-text blocks", func(t *testing.T) {
		resp, err := NewPromptSessionResponse("s1", []PromptContentBlock{
			{Type: "text", Text: "part one "},
			{Type: "tool_use", Text: "should not appear"},
			{Type: "text", Text: "part two"},
		}, "m1", "end_turn")
		if err != nil {
			t.Fatalf("NewPromptSessionResponse() error = %v", err)
		}
		if resp.Response != "part one part two" {
			t.Errorf("Response = %q", resp.Response)
		}
	})

	t.Run("an untyped block is treated as text rather than dropped", func(t *testing.T) {
		resp, err := NewPromptSessionResponse("s1",
			[]PromptContentBlock{{Text: "untyped output"}}, "", "")
		if err != nil {
			t.Fatalf("NewPromptSessionResponse() error = %v", err)
		}
		if resp.Response != "untyped output" || !resp.Done {
			t.Errorf("response = %+v, want the untyped text kept", resp)
		}
	})

	t.Run("a stop reason without text is still a completed turn", func(t *testing.T) {
		resp, err := NewPromptSessionResponse("s1", nil, "", "max_tokens")
		if err != nil {
			t.Fatalf("NewPromptSessionResponse() error = %v", err)
		}
		if !resp.Done || resp.Response != "" {
			t.Errorf("got %+v, want an empty but done turn", resp)
		}
	})

	t.Run("no content and no stop reason is a typed error, not an empty success", func(t *testing.T) {
		resp, err := NewPromptSessionResponse("s1", nil, "", "")
		if err == nil {
			t.Fatalf("got %+v, want an error for a turn with nothing in it", resp)
		}
		var empty *EmptyTurnError
		if !errors.As(err, &empty) {
			t.Fatalf("error %v should be a *EmptyTurnError", err)
		}
		if empty.SessionID != "s1" {
			t.Errorf("SessionID = %q, want the session the turn belonged to", empty.SessionID)
		}
		if empty.Error() == "" {
			t.Error("Error() must describe the failure")
		}
	})

	t.Run("empty turn without a session id still reports a message", func(t *testing.T) {
		err := &EmptyTurnError{}
		if err.Error() == "" {
			t.Error("Error() must describe the failure even without a session id")
		}
	})

	t.Run("an accepted turn is reported as started, not finished", func(t *testing.T) {
		const acceptedPayload = `{"session_id":"sess-1","turn_id":"turn-1","status":"accepted",` +
			`"sequence":7,"model":"claude-fable-5","timestamp":1786207030175886792}`
		var wire PromptSessionWire
		if err := json.Unmarshal([]byte(acceptedPayload), &wire); err != nil {
			t.Fatalf("decode accepted payload: %v", err)
		}

		resp, err := wire.Response()
		if err != nil {
			t.Fatalf("Response() error = %v", err)
		}
		if !resp.Accepted {
			t.Error("Accepted = false for a turn the server took for background execution")
		}
		if resp.Done {
			t.Error("Done = true for a turn that has only just started")
		}
		if resp.TurnID != "turn-1" {
			t.Errorf("TurnID = %q, want the ID the caller follows the turn by", resp.TurnID)
		}
		if resp.Sequence != 7 {
			t.Errorf("Sequence = %d, want the turn_started sequence to attach from", resp.Sequence)
		}
		if resp.Response != "" {
			t.Errorf("Response = %q, want empty: the output arrives on the event stream", resp.Response)
		}
		if resp.Model != "claude-fable-5" {
			t.Errorf("Model = %q", resp.Model)
		}
	})

	t.Run("an acceptance without a turn id is a typed error", func(t *testing.T) {
		resp, err := NewPromptSessionAccepted("s1", "", 3, "")
		if err == nil {
			t.Fatalf("got %+v, want an error for an ack the caller cannot follow", resp)
		}
		var missing *MissingTurnIDError
		if !errors.As(err, &missing) {
			t.Fatalf("error %v should be a *MissingTurnIDError", err)
		}
		if missing.SessionID != "s1" {
			t.Errorf("SessionID = %q, want the session the ack belonged to", missing.SessionID)
		}
		if missing.Error() == "" {
			t.Error("Error() must describe the failure")
		}
		if (&MissingTurnIDError{}).Error() == "" {
			t.Error("Error() must describe the failure even without a session id")
		}
	})
}

func TestSessionStatusWireResponse(t *testing.T) {
	t.Run("maps the service state field onto the SDK status field", func(t *testing.T) {
		var wire SessionStatusWire
		if err := json.Unmarshal([]byte(liveSessionStatusPayload), &wire); err != nil {
			t.Fatalf("decode live payload: %v", err)
		}

		resp := wire.Response()
		if resp.Status != "active" {
			t.Errorf("Status = %q, want the wire's state value", resp.Status)
		}
		if resp.LastActive != 1786205526 {
			t.Errorf("LastActive = %d", resp.LastActive)
		}
	})

	t.Run("zero wire yields a zero response rather than panicking", func(t *testing.T) {
		wire := SessionStatusWire{}
		if resp := wire.Response(); resp == nil || resp.SessionID != "" || resp.Status != "" {
			t.Errorf("Response() = %+v, want a zero response", resp)
		}
	})
}

func TestListSessionsWireResponse(t *testing.T) {
	t.Run("live service payload populates every column and the count", func(t *testing.T) {
		var wire ListSessionsWire
		if err := json.Unmarshal([]byte(liveListSessionsPayload), &wire); err != nil {
			t.Fatalf("decode live payload: %v", err)
		}

		resp := wire.Response()
		if resp.Count != 1 {
			t.Fatalf("Count = %d, want 1: the service does not send a count", resp.Count)
		}
		if len(resp.Sessions) != 1 {
			t.Fatalf("Sessions length = %d", len(resp.Sessions))
		}
		got := resp.Sessions[0]
		if got.Backend != "claude" || got.Status != "active" {
			t.Errorf("session = %+v, want backend and status populated", got)
		}
		if got.CreatedAt != 1786205526 {
			t.Errorf("CreatedAt = %d", got.CreatedAt)
		}
	})

	t.Run("empty session list yields a zero count and a non-nil slice", func(t *testing.T) {
		wire := ListSessionsWire{}
		resp := wire.Response()
		if resp.Count != 0 || resp.Sessions == nil {
			t.Errorf("Response() = %+v, want an empty non-nil session list", resp)
		}
	})
}

func TestCloseSessionWireResponse(t *testing.T) {
	t.Run("a decoded acknowledgement means the session is closed", func(t *testing.T) {
		var wire CloseSessionWire
		if err := json.Unmarshal([]byte(`{"session_id":"s1","timestamp":1786207020331543767}`), &wire); err != nil {
			t.Fatalf("decode: %v", err)
		}
		resp := wire.Response()
		if resp.SessionID != "s1" || !resp.Closed {
			t.Errorf("Response() = %+v, want the session reported closed", resp)
		}
	})

	t.Run("missing session id still reports closed", func(t *testing.T) {
		wire := CloseSessionWire{}
		if resp := wire.Response(); !resp.Closed {
			t.Errorf("Response() = %+v, want Closed", resp)
		}
	})
}
