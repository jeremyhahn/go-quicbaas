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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	resttransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/rest"
)

// =============================================================================
// `orchestrator prompt --follow` over the REST transport, end to end.
//
// followPromptTurn's other tests drive it against a mock OrchestratorClient,
// which proves the follower's own logic but never exercises the REST
// transport's SubscribeSessionEvents path — the piece that was actually
// broken ("REST transport does not support bidirectional streaming"). These
// tests build a real REST-transport orchestrator client against an httptest
// server that speaks the live SSE contract
// (GET /api/v1/orchestrator/sessions/{id}/events?follow=1&from=N), so a
// regression in the REST SSE wiring fails here even though the mock-backed
// tests above would still pass.
// =============================================================================

// restFollowServer answers PromptSession with an acceptance ack and streams
// the given events as SSE frames from the events endpoint.
func restFollowServer(t *testing.T, sessionID, turnID string, sequence uint64, events []map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/prompt"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"session_id":%q,"status":"accepted","turn_id":%q,"sequence":%d}`,
				sessionID, turnID, sequence)
		case strings.Contains(r.URL.Path, "/events"):
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			for i, ev := range events {
				envelope := map[string]interface{}{
					"session_id":   sessionID,
					"tenant_id":    "system",
					"sequence":     sequence + uint64(i),
					"timestamp":    "2026-01-01T00:00:00Z",
					"from_replay":  false,
					"payload_json": ev,
				}
				data, _ := json.Marshal(envelope)
				fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", sequence+uint64(i), ev["kind"], data)
				if flusher != nil {
					flusher.Flush()
				}
			}
		default:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, "{}")
		}
	}))
}

// restTestOrchestratorClient adapts the SDK's orchestrator.Client (which has
// no Close method of its own — the transport owns its lifecycle) to the CLI's
// OrchestratorClient interface, which does.
type restTestOrchestratorClient struct {
	orchestratorSDK.Client
	tr *resttransport.Transport
}

// Close releases the underlying REST transport.
func (c *restTestOrchestratorClient) Close() error {
	return c.tr.Close()
}

// newRESTOrchestratorClient builds a real SDK orchestrator client wired to
// the REST transport, pointed at the given httptest server.
func newRESTOrchestratorClient(t *testing.T, server *httptest.Server) OrchestratorClient {
	t.Helper()
	tr, err := resttransport.New(&sdktransport.Config{Address: server.URL})
	if err != nil {
		t.Fatalf("failed to create REST transport: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	return &restTestOrchestratorClient{Client: orchestratorSDK.NewClient(tr), tr: tr}
}

func TestFollowPromptTurn_REST_TerminatesOnMatchingTurnID(t *testing.T) {
	server := restFollowServer(t, "sess-rest-1", "turn-rest-1", 5, []map[string]interface{}{
		{"kind": "turn_started", "turn_started": map[string]interface{}{"turn_id": "turn-rest-1"}},
		{"kind": "assistant_chunk", "assistant_chunk": map[string]interface{}{"text": "hello from rest"}},
		{"kind": "turn_completed", "turn_completed": map[string]interface{}{"turn_id": "turn-rest-1", "stop_reason": "end_turn"}},
	})
	defer server.Close()

	client := newRESTOrchestratorClient(t, server)
	defer client.Close()

	ack, err := client.PromptSession(context.Background(), &orchestratorSDK.PromptSessionRequest{
		SessionID: "sess-rest-1", Prompt: "hi",
	})
	if err != nil {
		t.Fatalf("PromptSession: %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("expected an accepted async ack, got %+v", ack)
	}

	var out bytes.Buffer
	if err := followPromptTurn(client, ack, "table", &out); err != nil {
		t.Fatalf("followPromptTurn over REST: %v", err)
	}
	if !strings.Contains(out.String(), "hello from rest") {
		t.Fatalf("expected the agent's output in the transcript; got:\n%s", out.String())
	}
}

func TestFollowPromptTurn_REST_FailedTurnIsNonZero(t *testing.T) {
	server := restFollowServer(t, "sess-rest-2", "turn-rest-2", 9, []map[string]interface{}{
		{"kind": "turn_started", "turn_started": map[string]interface{}{"turn_id": "turn-rest-2"}},
		{"kind": "turn_failed", "turn_failed": map[string]interface{}{"turn_id": "turn-rest-2", "error": "backend crashed"}},
	})
	defer server.Close()

	client := newRESTOrchestratorClient(t, server)
	defer client.Close()

	ack, err := client.PromptSession(context.Background(), &orchestratorSDK.PromptSessionRequest{
		SessionID: "sess-rest-2", Prompt: "hi",
	})
	if err != nil {
		t.Fatalf("PromptSession: %v", err)
	}

	var out bytes.Buffer
	err = followPromptTurn(client, ack, "table", &out)
	var failed *TurnFailedError
	if !errors.As(err, &failed) {
		t.Fatalf("err = %v, want *TurnFailedError (non-zero exit)", err)
	}
	if failed.Reason != "backend crashed" {
		t.Fatalf("reason = %q, want %q", failed.Reason, "backend crashed")
	}
}
