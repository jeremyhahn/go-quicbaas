// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// REST session-response contract.
//
// These tests replay the exact bodies a live quicbaasd emitted for an ACP
// (claude) session. The REST handler used to unmarshal them straight into the
// SDK response types, whose field names do not match the service's — so a
// completed agent turn reached the CLI as "Done: false" with no text, an
// active session listed with an empty status, and a session list reported
// "Total: 0 sessions". Every assertion below fails against that decode.
// =============================================================================

// serveJSON starts a server that answers every request with body.
func serveJSON(t *testing.T, body string) *Transport {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return newTestTransport(t, server)
}

func TestRESTPromptSession_LiveServicePayload(t *testing.T) {
	const livePayload = `{"session_id":"ce4b6a40-9329-4b4d-bf72-4cbf598c3423",` +
		`"content":[{"type":"text","text":"SELF-HOSTING ONLINE"}],` +
		`"model":"claude-fable-5","stop_reason":"end_turn","timestamp":1786207030175886792}`

	tr := serveJSON(t, livePayload)

	result, err := tr.Request(context.Background(), "Orchestrator", "PromptSession",
		&orchestrator.PromptSessionRequest{SessionID: "ce4b6a40-9329-4b4d-bf72-4cbf598c3423", Prompt: "hi"})
	if err != nil {
		t.Fatalf("PromptSession() error = %v", err)
	}

	resp, ok := result.(*orchestrator.PromptSessionResponse)
	if !ok {
		t.Fatalf("result type = %T, want *orchestrator.PromptSessionResponse", result)
	}
	if resp.Response != "SELF-HOSTING ONLINE" {
		t.Errorf("Response = %q, want the agent's reply", resp.Response)
	}
	if !resp.Done {
		t.Error("Done = false for a turn the agent completed")
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
}

func TestRESTPromptSession_EmptyTurnIsTypedError(t *testing.T) {
	// A response carrying neither content nor a stop reason is not a turn.
	tr := serveJSON(t, `{"session_id":"s1","content":[],"timestamp":1786207030175886792}`)

	result, err := tr.Request(context.Background(), "Orchestrator", "PromptSession",
		&orchestrator.PromptSessionRequest{SessionID: "s1", Prompt: "hi"})
	if err == nil {
		t.Fatalf("PromptSession() = %+v, want a typed error instead of an empty success", result)
	}
	var empty *orchestrator.EmptyTurnError
	if !errors.As(err, &empty) {
		t.Fatalf("error %v should be a *orchestrator.EmptyTurnError", err)
	}
}

func TestRESTSessionStatus_LiveServicePayload(t *testing.T) {
	const livePayload = `{"session_id":"ce4b6a40-9329-4b4d-bf72-4cbf598c3423",` +
		`"state":"active","last_activity":1786205526,"uptime":1494,"timestamp":1786207020331543767}`

	tr := serveJSON(t, livePayload)

	result, err := tr.Request(context.Background(), "Orchestrator", "SessionStatus",
		&orchestrator.SessionStatusRequest{SessionID: "ce4b6a40-9329-4b4d-bf72-4cbf598c3423"})
	if err != nil {
		t.Fatalf("SessionStatus() error = %v", err)
	}

	resp, ok := result.(*orchestrator.SessionStatusResponse)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if resp.Status != "active" {
		t.Errorf("Status = %q, want the service's state value", resp.Status)
	}
	if resp.LastActive != 1786205526 {
		t.Errorf("LastActive = %d", resp.LastActive)
	}
}

func TestRESTListSessions_LiveServicePayload(t *testing.T) {
	const livePayload = `{"sessions":[{"session_id":"ce4b6a40-9329-4b4d-bf72-4cbf598c3423",` +
		`"project_id":"604c52b4-d26b-4fc0-b744-30c7763d4fa6","backend":"claude","model":"claude-fable-5",` +
		`"state":"active","created_at":1786205526,"last_activity":1786205516508700651}],` +
		`"timestamp":1786207020520347903}`

	tr := serveJSON(t, livePayload)

	result, err := tr.Request(context.Background(), "Orchestrator", "ListSessions",
		&orchestrator.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}

	resp, ok := result.(*orchestrator.ListSessionsResponse)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if resp.Count != 1 {
		t.Fatalf("Count = %d, want 1 for one listed session", resp.Count)
	}
	if resp.Sessions[0].Backend != "claude" || resp.Sessions[0].Status != "active" {
		t.Errorf("session = %+v, want backend and status populated", resp.Sessions[0])
	}
}

func TestRESTCloseSession_LiveServicePayload(t *testing.T) {
	tr := serveJSON(t, `{"session_id":"s1","timestamp":1786207020331543767}`)

	result, err := tr.Request(context.Background(), "Orchestrator", "CloseSession",
		&orchestrator.CloseSessionRequest{SessionID: "s1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}

	resp, ok := result.(*orchestrator.CloseSessionResponse)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if !resp.Closed {
		t.Error("Closed = false although the service acknowledged the close")
	}
}
