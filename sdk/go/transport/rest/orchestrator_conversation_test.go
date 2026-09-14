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

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// GetSessionConversation over REST (and, by embedding, over QUIC)
// =============================================================================

// TestOrchestratorGetSessionConversation_GetsTheHistorySubresource pins the URL
// the SDK builds. The server registers the route as
// GET /api/v1/orchestrator/sessions/{sessionID}/history and the PEP authorizes
// on that exact suffix, so a path drift silently changes which permission the
// call is checked against.
func TestOrchestratorGetSessionConversation_GetsTheHistorySubresource(t *testing.T) {
	var gotMethod, gotPath, gotQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"session_id":"s1","tenant_id":"t1","next_cursor":9,"oldest_sequence":3,
			"turns":[{"sequence":4,"session_id":"s1","role":"user","text":"hi"},
			         {"sequence":5,"session_id":"s1","role":"assistant","text":"hello",
			          "model":"m1","stop_reason":"end_turn","truncated_bytes":12,
			          "tools":[{"invocation_id":"i1","name":"Read","result_bytes":5}],
			          "usage":{"input_tokens":7,"output_tokens":8}}]}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	resp, err := tr.Request(context.Background(), "Orchestrator", "GetSessionConversation",
		&orchestrator.GetSessionConversationRequest{
			SessionID: "s1",
			TenantID:  "t1",
			Cursor:    4,
			Limit:     25,
		})
	if err != nil {
		t.Fatalf("GetSessionConversation: %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Errorf("method = %s, want GET", gotMethod)
	}
	if gotPath != "/api/v1/orchestrator/sessions/s1/history" {
		t.Errorf("path = %s, want /api/v1/orchestrator/sessions/s1/history", gotPath)
	}
	for _, want := range []string{"tenant_id=t1", "cursor=4", "limit=25"} {
		if !containsQuery(gotQuery, want) {
			t.Errorf("query %q dropped %q", gotQuery, want)
		}
	}

	page, ok := resp.(*orchestrator.GetSessionConversationResponse)
	if !ok {
		t.Fatalf("response is %T, want *orchestrator.GetSessionConversationResponse", resp)
	}
	if len(page.Turns) != 2 {
		t.Fatalf("got %d turns, want 2", len(page.Turns))
	}
	if page.Turns[0].Role != orchestrator.SessionTurnRoleUser {
		t.Errorf("turn[0] role = %q, want user", page.Turns[0].Role)
	}
	reply := page.Turns[1]
	if reply.Role != orchestrator.SessionTurnRoleAssistant || reply.Text != "hello" {
		t.Errorf("turn[1] = %q/%q", reply.Role, reply.Text)
	}
	if reply.Usage == nil || reply.Usage.OutputTokens != 8 {
		t.Errorf("turn[1] usage = %+v, want output_tokens 8", reply.Usage)
	}
	if reply.TruncatedBytes != 12 {
		t.Errorf("turn[1] truncated bytes = %d, want 12", reply.TruncatedBytes)
	}
	if len(reply.Tools) != 1 || reply.Tools[0].Name != "Read" {
		t.Errorf("turn[1] tools = %+v", reply.Tools)
	}
	if page.NextCursor != 9 || page.OldestSequence != 3 {
		t.Errorf("cursors = next %d oldest %d, want 9 / 3", page.NextCursor, page.OldestSequence)
	}
}

// TestOrchestratorGetSessionConversation_RejectsBadInput asserts the transport
// refuses an unusable request locally rather than issuing a request that can
// only fail at the server.
func TestOrchestratorGetSessionConversation_RejectsBadInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("an invalid request reached the server: %s", r.URL.Path)
	}))
	defer server.Close()
	tr := newTestTransport(t, server)

	tests := []struct {
		name string
		req  interface{}
	}{
		{"wrong request type", &orchestrator.SessionStatusRequest{SessionID: "s1"}},
		{"no session id", &orchestrator.GetSessionConversationRequest{TenantID: "t1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tr.Request(context.Background(), "Orchestrator", "GetSessionConversation", tt.req); err == nil {
				t.Fatal("expected a typed refusal")
			}
		})
	}
}

// containsQuery reports whether an encoded query string carries a key=value
// pair, without depending on parameter order.
func containsQuery(query, pair string) bool {
	for _, part := range splitQuery(query) {
		if part == pair {
			return true
		}
	}
	return false
}

// splitQuery splits an encoded query on '&'.
func splitQuery(query string) []string {
	var out []string
	start := 0
	for i := 0; i < len(query); i++ {
		if query[i] == '&' {
			out = append(out, query[start:i])
			start = i + 1
		}
	}
	if start < len(query) {
		out = append(out, query[start:])
	}
	return out
}
