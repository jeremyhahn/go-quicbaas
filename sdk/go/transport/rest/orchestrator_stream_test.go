// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// =============================================================================
// SubscribeSessionEvents over REST — SSE stream.
//
// REST's RequestStream unconditionally rejected every streaming RPC before
// this file's handler was wired in, which broke `orchestrator prompt
// --follow` and `orchestrator attach` on the REST transport with
// "REST transport does not support bidirectional streaming" even though the
// server's SSE endpoint (GET .../sessions/{id}/events) works. These tests
// drive the real HTTP round trip against a server that speaks the exact SSE
// framing pkg/api/rest/handlers_session_events.go emits.
// =============================================================================

// writeTestSSEEvent writes one `event:`/`data:` frame in the same shape the
// server's writeSSEEvent helper produces: id, event kind, and a JSON
// envelope whose payload_json is the full decoded event object (not raw
// bytes) — matching pkg/api/rest's sessionEventEnvelope wire type.
func writeTestSSEEvent(w http.ResponseWriter, seq uint64, kind string, payload map[string]interface{}) {
	envelope := map[string]interface{}{
		"session_id":   "s1",
		"tenant_id":    "system",
		"sequence":     seq,
		"timestamp":    "2026-01-01T00:00:00Z",
		"from_replay":  false,
		"payload_json": payload,
	}
	data, _ := json.Marshal(envelope)
	fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", seq, kind, data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func TestSubscribeSessionEvents_REST_DecodesFramesAndTerminates(t *testing.T) {
	var gotURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		if r.URL.Query().Get("follow") != "true" {
			t.Errorf("expected follow=true, got %q", r.URL.Query().Get("follow"))
		}
		if r.URL.Query().Get("from") != "5" {
			t.Errorf("expected from=5, got %q", r.URL.Query().Get("from"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// A ping/comment line must be skipped transparently.
		fmt.Fprint(w, ": ping 1\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		writeTestSSEEvent(w, 5, "turn_started", map[string]interface{}{
			"kind":         "turn_started",
			"turn_started": map[string]interface{}{"turn_id": "t1", "backend": "ollama"},
		})
		writeTestSSEEvent(w, 6, "assistant_chunk", map[string]interface{}{
			"kind":            "assistant_chunk",
			"assistant_chunk": map[string]interface{}{"text": "hi"},
		})
		writeTestSSEEvent(w, 7, "turn_completed", map[string]interface{}{
			"kind":           "turn_completed",
			"turn_completed": map[string]interface{}{"turn_id": "t1", "stop_reason": "end_turn"},
		})
		// A real follow tail stays open until the client detaches. Returning
		// here would look like a dropped connection, which the reader now
		// resumes rather than reporting as the end of the stream.
		<-r.Context().Done()
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	stream, err := tr.RequestStream(context.Background(), "quicbaas.Orchestrator", "SubscribeSessionEvents",
		&orchestrator.SubscribeSessionEventsRequest{SessionID: "s1", FromSequence: 5, Follow: true})
	if err != nil {
		t.Fatalf("RequestStream returned error: %v", err)
	}
	defer stream.Close()

	var got []*orchestrator.SessionEventEnvelope
	for len(got) < 3 {
		msg, recvErr := stream.Recv()
		if recvErr != nil {
			t.Fatalf("Recv returned unexpected error: %v", recvErr)
		}
		env, ok := msg.(*orchestrator.SessionEventEnvelope)
		if !ok {
			t.Fatalf("Recv returned unexpected type %T", msg)
		}
		got = append(got, env)
	}

	// Detaching ends the stream cleanly, with no reconnect attempt.
	if err := stream.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if _, recvErr := stream.Recv(); recvErr != io.EOF {
		t.Fatalf("expected io.EOF after Close, got %v", recvErr)
	}

	if !strings.Contains(gotURL, "/api/v1/orchestrator/sessions/s1/events") {
		t.Errorf("unexpected request URL: %s", gotURL)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}
	if got[0].Sequence != 5 || got[2].Sequence != 7 {
		t.Errorf("unexpected sequences: %d, %d", got[0].Sequence, got[2].Sequence)
	}

	var turnStarted struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(got[0].PayloadJSON, &turnStarted); err != nil {
		t.Fatalf("failed to decode payload_json: %v", err)
	}
	if turnStarted.Kind != "turn_started" {
		t.Errorf("expected kind turn_started, got %q", turnStarted.Kind)
	}

	var turnCompleted struct {
		Kind          string `json:"kind"`
		TurnCompleted struct {
			TurnID string `json:"turn_id"`
		} `json:"turn_completed"`
	}
	if err := json.Unmarshal(got[2].PayloadJSON, &turnCompleted); err != nil {
		t.Fatalf("failed to decode terminal payload_json: %v", err)
	}
	if turnCompleted.TurnCompleted.TurnID != "t1" {
		t.Errorf("expected turn_id t1, got %q", turnCompleted.TurnCompleted.TurnID)
	}
}

func TestSubscribeSessionEvents_REST_ServerErrorEventSurfacesAsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: error\ndata: subscribe failed: boom\n\n")
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	stream, err := tr.RequestStream(context.Background(), "quicbaas.Orchestrator", "SubscribeSessionEvents",
		&orchestrator.SubscribeSessionEventsRequest{SessionID: "s1", Follow: true})
	if err != nil {
		t.Fatalf("RequestStream returned error: %v", err)
	}
	defer stream.Close()

	_, recvErr := stream.Recv()
	if recvErr == nil {
		t.Fatal("expected an error from Recv, got nil")
	}
	if !strings.Contains(recvErr.Error(), "boom") {
		t.Errorf("expected error to carry server message, got: %v", recvErr)
	}
}

func TestSubscribeSessionEvents_REST_MissingSessionID(t *testing.T) {
	tr := &Transport{}
	tr.orchestratorHandler = newOrchestratorHandler(tr)
	_, err := tr.orchestratorHandler.subscribeSessionEvents(context.Background(),
		&orchestrator.SubscribeSessionEventsRequest{})
	if err == nil {
		t.Fatal("expected an error for missing session_id")
	}
	if errors.Code(err) != errors.CodeInvalidInput {
		t.Errorf("expected CodeInvalidInput, got %v", errors.Code(err))
	}
}

func TestSubscribeSessionEvents_REST_InvalidRequestType(t *testing.T) {
	tr := &Transport{}
	tr.orchestratorHandler = newOrchestratorHandler(tr)
	_, err := tr.orchestratorHandler.subscribeSessionEvents(context.Background(), "not-a-request")
	if err == nil {
		t.Fatal("expected an error for invalid request type")
	}
	if errors.Code(err) != errors.CodeInvalidInput {
		t.Errorf("expected CodeInvalidInput, got %v", errors.Code(err))
	}
}

// TestRequestStream_NonSessionEventsMethodStillRejected asserts every other
// Orchestrator streaming method (and every other service) keeps returning
// ErrStreamNotSupported: only SubscribeSessionEvents has an SSE mapping.
func TestRequestStream_NonSessionEventsMethodStillRejected(t *testing.T) {
	tr, err := New(&transport.Config{Address: "http://127.0.0.1:0"})
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer tr.Close()

	_, err = tr.RequestStream(context.Background(), "quicbaas.Orchestrator", "SubscribeJobEvents", nil)
	if err != ErrStreamNotSupported {
		t.Errorf("expected ErrStreamNotSupported, got %v", err)
	}

	_, err = tr.RequestStream(context.Background(), "quicbaas.KV", "Watch", nil)
	if err != ErrStreamNotSupported {
		t.Errorf("expected ErrStreamNotSupported, got %v", err)
	}
}
