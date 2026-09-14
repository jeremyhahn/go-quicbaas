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

package mcp

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestGetSessionConversationToolName pins the SDK method to the tool the
// server registers. The generic service_method derivation would ask for
// quicbaas_orchestrator_get_session_conversation, which does not exist.
func TestGetSessionConversationToolName(t *testing.T) {
	tr := &Transport{}
	got := tr.mapToToolName("quicbaas.Orchestrator", "GetSessionConversation")
	if got != "quicbaas_orchestrator_session_conversation" {
		t.Fatalf("mapToToolName = %q, want quicbaas_orchestrator_session_conversation", got)
	}
}

// TestDecodeMCPSessionConversation decodes the page the server's
// executeAgentSessionConversation marshals.
func TestDecodeMCPSessionConversation(t *testing.T) {
	raw := []byte(`{"session_id":"s1","tenant_id":"t1","next_cursor":9,"oldest_sequence":3,
		"turns":[{"sequence":4,"session_id":"s1","role":"user","text":"hi"},
		         {"sequence":5,"session_id":"s1","role":"assistant","text":"hello",
		          "tools":[{"invocation_id":"i1","name":"Bash","is_error":true}],
		          "usage":{"input_tokens":7,"output_tokens":8}}]}`)

	got, err := decodeMCPSessionConversation(raw)
	if err != nil {
		t.Fatalf("decodeMCPSessionConversation: %v", err)
	}
	page, ok := got.(*orchestrator.GetSessionConversationResponse)
	if !ok {
		t.Fatalf("decoded %T, want *orchestrator.GetSessionConversationResponse", got)
	}
	if len(page.Turns) != 2 {
		t.Fatalf("got %d turns, want 2", len(page.Turns))
	}
	if page.Turns[0].Role != orchestrator.SessionTurnRoleUser {
		t.Fatalf("turn[0] role = %q, want user", page.Turns[0].Role)
	}
	reply := page.Turns[1]
	if reply.Usage == nil || reply.Usage.OutputTokens != 8 {
		t.Fatalf("turn[1] usage = %+v, want output_tokens 8", reply.Usage)
	}
	if len(reply.Tools) != 1 || !reply.Tools[0].IsError {
		t.Fatalf("turn[1] tools = %+v, want one failing Bash call", reply.Tools)
	}
	if page.NextCursor != 9 || page.OldestSequence != 3 {
		t.Fatalf("cursors = next %d oldest %d, want 9 / 3", page.NextCursor, page.OldestSequence)
	}
}

// TestDecodeMCPSessionConversation_InvalidJSON asserts an undecodable result
// is a typed failure rather than an empty conversation, which would read as a
// session that said nothing.
func TestDecodeMCPSessionConversation_InvalidJSON(t *testing.T) {
	if _, err := decodeMCPSessionConversation([]byte(`{"turns":`)); err == nil {
		t.Fatal("a malformed tool result was accepted")
	}
}

// TestGetSessionConversationTransformIsRegistered proves the method goes
// through the typed decoder rather than being handed back as a raw map, which
// would fail the SDK client's response type assertion.
func TestGetSessionConversationTransformIsRegistered(t *testing.T) {
	h := newOrchestratorMCPHandler()

	got, err := h.transformResponse("GetSessionConversation",
		`{"session_id":"s1","turns":[{"sequence":1,"role":"system","text":"session spawned"}]}`)
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	if _, ok := got.(*orchestrator.GetSessionConversationResponse); !ok {
		t.Fatalf("transformResponse returned %T, want *orchestrator.GetSessionConversationResponse", got)
	}
}

// TestGetSessionConversationArgumentsAreSnakeCase asserts the tool arguments
// the transport derives from the SDK request match the server's declared
// schema. A camelCase key would be silently ignored by the tool.
func TestGetSessionConversationArgumentsAreSnakeCase(t *testing.T) {
	tr := &Transport{}
	args := tr.convertToArguments(&orchestrator.GetSessionConversationRequest{
		TenantID:  "t1",
		SessionID: "s1",
		Cursor:    12,
		Limit:     25,
	})

	for _, key := range []string{"tenant_id", "session_id", "cursor", "limit"} {
		if _, ok := args[key]; !ok {
			t.Fatalf("tool arguments %v are missing %q", args, key)
		}
	}
}
