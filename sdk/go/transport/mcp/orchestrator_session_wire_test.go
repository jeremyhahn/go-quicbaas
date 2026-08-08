// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package mcp

import (
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// MCP session-response contract.
//
// The quicbaas_orchestrator_* session tools serialize the orchestrator
// service's own shape (content blocks, "state", an RFC 3339 timestamp). The
// MCP handler used to pass those results through untouched, so the SDK client
// failed its type assertion instead of returning a turn. These tests pin the
// transformation for each session tool.
// =============================================================================

func TestMCPTransformPromptSession(t *testing.T) {
	h := newOrchestratorMCPHandler()

	t.Run("tool result becomes a done response carrying the turn text", func(t *testing.T) {
		result := map[string]interface{}{
			"session_id": "sess-001",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "SELF-HOSTING ONLINE"},
			},
			"model":       "claude-fable-5",
			"stop_reason": "end_turn",
			// The MCP tools format the envelope timestamp as RFC 3339, unlike
			// the epoch nanoseconds REST and QUIC send.
			"timestamp": "2026-08-08T12:37:10.175886792Z",
		}

		out, err := h.transformResponse("PromptSession", result)
		if err != nil {
			t.Fatalf("transformResponse() error = %v", err)
		}
		resp, ok := out.(*orchestrator.PromptSessionResponse)
		if !ok {
			t.Fatalf("result type = %T, want *orchestrator.PromptSessionResponse", out)
		}
		if resp.Response != "SELF-HOSTING ONLINE" || !resp.Done {
			t.Errorf("response = %+v, want the turn text and Done", resp)
		}
	})

	t.Run("raw JSON string results are decoded too", func(t *testing.T) {
		out, err := h.transformResponse("PromptSession",
			`{"session_id":"sess-001","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`)
		if err != nil {
			t.Fatalf("transformResponse() error = %v", err)
		}
		if resp := out.(*orchestrator.PromptSessionResponse); resp.Response != "ok" {
			t.Errorf("Response = %q", resp.Response)
		}
	})

	t.Run("a turn with nothing in it is a typed error", func(t *testing.T) {
		out, err := h.transformResponse("PromptSession", map[string]interface{}{"session_id": "sess-001"})
		if err == nil {
			t.Fatalf("transformResponse() = %+v, want an error", out)
		}
		var empty *orchestrator.EmptyTurnError
		if !errors.As(err, &empty) {
			t.Fatalf("error %v should be a *orchestrator.EmptyTurnError", err)
		}
	})

	t.Run("nil result is an error, not a nil response", func(t *testing.T) {
		if _, err := h.transformResponse("PromptSession", nil); err == nil {
			t.Fatal("transformResponse(nil) should error")
		}
	})

	t.Run("undecodable result is an error", func(t *testing.T) {
		if _, err := h.transformResponse("PromptSession", "not json at all"); err == nil {
			t.Fatal("transformResponse should reject an undecodable result")
		}
	})
}

func TestMCPTransformSessionTools(t *testing.T) {
	h := newOrchestratorMCPHandler()

	t.Run("SessionStatus maps state onto status", func(t *testing.T) {
		out, err := h.transformResponse("SessionStatus", map[string]interface{}{
			"session_id":    "sess-001",
			"state":         "active",
			"last_activity": float64(1786205526),
			"uptime":        float64(1494),
			"timestamp":     "2026-08-08T12:37:00.331543767Z",
		})
		if err != nil {
			t.Fatalf("transformResponse() error = %v", err)
		}
		resp, ok := out.(*orchestrator.SessionStatusResponse)
		if !ok {
			t.Fatalf("result type = %T", out)
		}
		if resp.Status != "active" {
			t.Errorf("Status = %q", resp.Status)
		}
	})

	t.Run("ListSessions derives the count", func(t *testing.T) {
		out, err := h.transformResponse("ListSessions", map[string]interface{}{
			"sessions": []interface{}{
				map[string]interface{}{"session_id": "sess-001", "backend": "claude", "state": "active"},
			},
			"timestamp": "2026-08-08T12:37:00.520347903Z",
		})
		if err != nil {
			t.Fatalf("transformResponse() error = %v", err)
		}
		resp, ok := out.(*orchestrator.ListSessionsResponse)
		if !ok {
			t.Fatalf("result type = %T", out)
		}
		if resp.Count != 1 || resp.Sessions[0].Backend != "claude" {
			t.Errorf("response = %+v, want one populated session", resp)
		}
	})

	t.Run("CloseSession acknowledgement means closed", func(t *testing.T) {
		out, err := h.transformResponse("CloseSession", map[string]interface{}{
			"session_id": "sess-001",
			"timestamp":  "2026-08-08T12:37:00.331543767Z",
		})
		if err != nil {
			t.Fatalf("transformResponse() error = %v", err)
		}
		resp, ok := out.(*orchestrator.CloseSessionResponse)
		if !ok {
			t.Fatalf("result type = %T", out)
		}
		if !resp.Closed {
			t.Error("Closed = false although the tool acknowledged the close")
		}
	})

	t.Run("malformed session tool results are errors", func(t *testing.T) {
		for _, method := range []string{"SessionStatus", "ListSessions", "CloseSession"} {
			if _, err := h.transformResponse(method, "not json at all"); err == nil {
				t.Errorf("%s: transformResponse should reject an undecodable result", method)
			}
		}
	})

	t.Run("methods without a transform pass through unchanged", func(t *testing.T) {
		in := map[string]interface{}{"backends": []interface{}{}}
		out, err := h.transformResponse("ListBackends", in)
		if err != nil {
			t.Fatalf("transformResponse() error = %v", err)
		}
		if _, ok := out.(map[string]interface{}); !ok {
			t.Fatalf("result type = %T, want the untouched map", out)
		}
	})
}
