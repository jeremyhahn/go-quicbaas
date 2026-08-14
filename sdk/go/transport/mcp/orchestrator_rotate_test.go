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

package mcp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestRotateSessionAccountToolName pins the SDK method to the tool the server
// registers. The generic service_method derivation would ask for
// quicbaas_orchestrator_rotate_session_account, which does not exist.
func TestRotateSessionAccountToolName(t *testing.T) {
	tr := &Transport{}
	got := tr.mapToToolName("quicbaas.Orchestrator", "RotateSessionAccount")
	if got != "quicbaas_orchestrator_rotate_account" {
		t.Fatalf("mapToToolName = %q, want quicbaas_orchestrator_rotate_account", got)
	}
}

// TestDecodeMCPRotateSessionAccount decodes the tool result the server's
// executeAgentRotateAccount marshals.
func TestDecodeMCPRotateSessionAccount(t *testing.T) {
	raw := []byte(`{"session_id":"s1","previous_account_id":"acct-a","account_id":"acct-b","strategy":"least_usage","mode":"in_place"}`)

	got, err := decodeMCPRotateSessionAccount(raw)
	if err != nil {
		t.Fatalf("decodeMCPRotateSessionAccount: %v", err)
	}
	resp, ok := got.(*orchestrator.RotateSessionAccountResponse)
	if !ok {
		t.Fatalf("decoded %T, want *orchestrator.RotateSessionAccountResponse", got)
	}
	if resp.PreviousAccountID != "acct-a" || resp.AccountID != "acct-b" {
		t.Fatalf("rotation = %s -> %s, want acct-a -> acct-b", resp.PreviousAccountID, resp.AccountID)
	}
	if resp.Mode != "in_place" {
		t.Fatalf("Mode = %q, want in_place", resp.Mode)
	}
}

func TestDecodeMCPRotateSessionAccount_InvalidJSON(t *testing.T) {
	if _, err := decodeMCPRotateSessionAccount([]byte(`{"session_id":`)); err == nil {
		t.Fatal("a malformed tool result was accepted")
	}
}

// TestRotateSessionAccountTransformIsRegistered proves the method goes through
// the typed decoder rather than being handed back as a raw map, which would
// fail the SDK client's response type assertion.
func TestRotateSessionAccountTransformIsRegistered(t *testing.T) {
	h := newOrchestratorMCPHandler()

	got, err := h.transformResponse("RotateSessionAccount",
		`{"session_id":"s1","account_id":"acct-b","mode":"resume"}`)
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	if _, ok := got.(*orchestrator.RotateSessionAccountResponse); !ok {
		t.Fatalf("transformResponse returned %T, want *orchestrator.RotateSessionAccountResponse", got)
	}
}

// TestRotateSessionAccountArgumentsAreSnakeCase asserts the tool arguments the
// transport derives from the SDK request match the server's declared schema.
// A camelCase key would be silently ignored by the tool.
func TestRotateSessionAccountArgumentsAreSnakeCase(t *testing.T) {
	tr := &Transport{}
	args := tr.convertToArguments(&orchestrator.RotateSessionAccountRequest{
		SessionID:    "s1",
		Reason:       "seat retired",
		HoldOutgoing: true,
		AvailableAt:  time.Now().UTC(),
	})

	for _, key := range []string{"session_id", "reason", "hold_outgoing", "available_at"} {
		if _, ok := args[key]; !ok {
			t.Fatalf("tool arguments %v are missing %q", args, key)
		}
	}
}

// TestSpawnSessionArgumentsCarryAccountAllow is the MCP-SDK half of gap 2.
func TestSpawnSessionArgumentsCarryAccountAllow(t *testing.T) {
	tr := &Transport{}
	args := tr.convertToArguments(&orchestrator.SpawnSessionRequest{
		Backend:      "claude",
		AccountID:    "acct-a",
		AccountAllow: []string{"acct-a", "acct-b"},
	})

	raw, err := json.Marshal(args["account_allow"])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(raw) != `["acct-a","acct-b"]` {
		t.Fatalf("account_allow = %s, want [\"acct-a\",\"acct-b\"]", raw)
	}
}
