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
	"encoding/json"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestOrchestratorMethodToolNames pins the SDK method to the MCP tool the
// server actually registers. The server names session tools after the
// operation (quicbaas_orchestrator_prompt) while the SDK names the RPC
// (PromptSession); the transport's generic service_method derivation produces
// quicbaas_orchestrator_prompt_session, a tool that does not exist. Every
// mismatch here is an unusable method over MCP, so the mapping is asserted
// rather than assumed.
func TestOrchestratorMethodToolNames(t *testing.T) {
	tr := &Transport{}
	cases := map[string]string{
		"SpawnSession":      "quicbaas_orchestrator_spawn",
		"PromptSession":     "quicbaas_orchestrator_prompt",
		"SessionStatus":     "quicbaas_orchestrator_status",
		"CloseSession":      "quicbaas_orchestrator_close",
		"GetSessionHistory": "quicbaas_orchestrator_session_history",
		// Already agreed with the generic derivation; asserted so a future
		// refactor of that derivation cannot silently break them.
		"ListSessions":  "quicbaas_orchestrator_list_sessions",
		"ListWorkflows": "quicbaas_orchestrator_list_workflows",
		"Inference":     "quicbaas_orchestrator_inference",
	}
	for method, want := range cases {
		if got := tr.mapToToolName("quicbaas.Orchestrator", method); got != want {
			t.Errorf("mapToToolName(%q) = %q, want %q", method, got, want)
		}
	}
}

// TestOrchestratorMethodToolNames_NonOrchestratorUnaffected asserts the
// orchestrator lookup does not leak into other services: the map is keyed by
// bare method name, which other services also use.
func TestOrchestratorMethodToolNames_NonOrchestratorUnaffected(t *testing.T) {
	tr := &Transport{}
	if got := tr.mapToToolName("quicbaas.KV", "Get"); got != "quicbaas_kv_get" {
		t.Errorf("KV/Get mapped to %q; the orchestrator table must not apply", got)
	}
}

// TestDecodeMCPSessionHistory asserts a transcript page decodes into SDK
// envelopes with the stored event carried verbatim, which is what lets an MCP
// caller recognise a turn's terminal event without the SDK modelling every
// event kind.
func TestDecodeMCPSessionHistory(t *testing.T) {
	const raw = `{"session_id":"sess-1","tenant_id":"tenant-1","next_seq":0,"events":[` +
		`{"sequence":42,"session_id":"sess-1","tenant_id":"tenant-1",` +
		`"timestamp":"2026-03-16T12:00:00Z","kind":"turn_completed",` +
		`"turn_completed":{"turn_id":"turn-1","stop_reason":"end_turn"}}]}`

	result, err := decodeMCPSessionHistory([]byte(raw))
	if err != nil {
		t.Fatalf("decodeMCPSessionHistory: %v", err)
	}
	resp, ok := result.(*orchestrator.GetSessionHistoryResponse)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if len(resp.Events) != 1 {
		t.Fatalf("decoded %d events, want 1", len(resp.Events))
	}
	ev := resp.Events[0]
	if ev.Sequence != 42 || ev.SessionID != "sess-1" || ev.TenantID != "tenant-1" {
		t.Fatalf("envelope addressing = %+v", ev)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(ev.PayloadJSON, &payload); err != nil {
		t.Fatalf("payload must round-trip verbatim: %v", err)
	}
	if payload["kind"] != "turn_completed" {
		t.Fatalf("payload kind = %v, want turn_completed", payload["kind"])
	}
}

// TestDecodeMCPSessionHistory_Malformed asserts undecodable input is a typed
// error rather than an empty page, which a caller would read as "the session
// has nothing to say".
func TestDecodeMCPSessionHistory_Malformed(t *testing.T) {
	if _, err := decodeMCPSessionHistory([]byte("{not json")); err == nil {
		t.Fatal("expected an error for a malformed history result")
	}
	if _, err := decodeMCPSessionHistory([]byte(`{"events":["not-an-object"]}`)); err == nil {
		t.Fatal("expected an error for an undecodable session event")
	}
}
