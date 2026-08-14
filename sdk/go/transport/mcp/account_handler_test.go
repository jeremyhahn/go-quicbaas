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
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
)

// TestIsMCPAccountService pins the routing predicate.
func TestIsMCPAccountService(t *testing.T) {
	for _, name := range []string{"Account", "quicbaas.Account"} {
		if !isMCPAccountService(name) {
			t.Errorf("isMCPAccountService(%q) = false", name)
		}
	}
	for _, name := range []string{"quicbaas.Orchestrator", "quicbaas.IAM.Users"} {
		if isMCPAccountService(name) {
			t.Errorf("isMCPAccountService(%q) = true", name)
		}
	}
}

// TestAccountMethodToolNames pins the SDK method -> MCP tool mapping. The
// tools are named after the operation, so the transport's generic
// service_method derivation would ask for tools that do not exist; a drift
// here surfaces as "tool not registered" at runtime.
func TestAccountMethodToolNames(t *testing.T) {
	want := map[string]string{
		"Create":           "quicbaas_agent_account_create",
		"Get":              "quicbaas_agent_account_get",
		"List":             "quicbaas_agent_account_list",
		"Update":           "quicbaas_agent_account_update",
		"Delete":           "quicbaas_agent_account_delete",
		"RotateCredential": "quicbaas_agent_account_rotate",
		"Revoke":           "quicbaas_agent_account_revoke",
		"SetDefault":       "quicbaas_agent_account_set_default",
		"Usage":            "quicbaas_agent_account_usage",
	}
	for method, expected := range want {
		if got := accountMethodTools[method]; got != expected {
			t.Errorf("%s -> %q, want %q", method, got, expected)
		}
	}
	if len(accountMethodTools) != len(want) {
		t.Errorf("tool map has %d entries, want %d", len(accountMethodTools), len(want))
	}
	// Every mapped method must also have a response transform, or the call
	// would succeed on the wire and then fail to decode.
	for method := range accountMethodTools {
		if _, ok := accountMCPTransforms[method]; !ok {
			t.Errorf("%s has a tool name but no response transform", method)
		}
	}
}

// TestAccountMCPTransformResponse_HappyPaths drives every transform against
// the tool result shape the server actually returns (the pkg/service response
// envelopes, decoded by the transport into a generic map).
func TestAccountMCPTransformResponse_HappyPaths(t *testing.T) {
	h := newAccountMCPHandler()

	envelope := map[string]interface{}{
		"account": map[string]interface{}{"id": "acct-1", "provider": "claude", "status": "active"},
	}
	for _, method := range []string{"Create", "Get", "Update", "Revoke"} {
		out, err := h.transformResponse(method, envelope)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		info, ok := out.(*account.AccountInfo)
		if !ok || info.ID != "acct-1" || info.Provider != account.ProviderClaude {
			t.Fatalf("%s decoded to %#v", method, out)
		}
	}

	out, err := h.transformResponse("List", map[string]interface{}{
		"accounts": []interface{}{map[string]interface{}{"id": "acct-1"}},
		"total":    1,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if resp, ok := out.(*account.ListAccountsResponse); !ok || resp.Total != 1 || len(resp.Accounts) != 1 {
		t.Fatalf("List decoded to %#v", out)
	}

	ack := map[string]interface{}{"account_id": "acct-1"}
	for _, method := range []string{"RotateCredential", "SetDefault"} {
		out, err := h.transformResponse(method, ack)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		if resp, ok := out.(*account.AccountAckResponse); !ok || resp.AccountID != "acct-1" {
			t.Fatalf("%s decoded to %#v", method, out)
		}
	}

	out, err = h.transformResponse("Delete", map[string]interface{}{"account_id": "acct-1", "deleted": true})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if resp, ok := out.(*account.DeleteAccountResponse); !ok || !resp.Deleted {
		t.Fatalf("Delete decoded to %#v", out)
	}

	out, err = h.transformResponse("Usage", map[string]interface{}{
		"usage": map[string]interface{}{"total_tokens": 42},
	})
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if report, ok := out.(*account.UsageReport); !ok || report.TotalTokens != 42 {
		t.Fatalf("Usage decoded to %#v", out)
	}
}

// TestAccountMCPTransformResponse_ErrorPaths covers the failure modes.
func TestAccountMCPTransformResponse_ErrorPaths(t *testing.T) {
	h := newAccountMCPHandler()

	if _, err := h.transformResponse("Teleport", map[string]interface{}{}); err == nil {
		t.Error("an unsupported method must fail")
	}
	if _, err := h.transformResponse("Get", nil); err == nil {
		t.Error("an empty tool result must fail")
	}
	if _, err := h.transformResponse("Get", map[string]interface{}{}); err == nil {
		t.Error("an envelope with no account must fail")
	}
	if _, err := h.transformResponse("Usage", map[string]interface{}{}); err == nil {
		t.Error("an envelope with no usage report must fail")
	}
	out, err := h.transformResponse("List", map[string]interface{}{"total": 0})
	if err != nil {
		t.Fatalf("List with no accounts: %v", err)
	}
	if resp, ok := out.(*account.ListAccountsResponse); !ok || resp.Accounts == nil {
		t.Fatalf("List must normalize a missing array to an empty slice; got %#v", out)
	}
}

// TestAccountMCPResultsCarryNoCredential is the client-side leak guard: even a
// hostile tool result cannot deliver credential material, because no decoded
// shape has a field to receive it.
func TestAccountMCPResultsCarryNoCredential(t *testing.T) {
	h := newAccountMCPHandler()
	hostile := map[string]interface{}{
		"account": map[string]interface{}{
			"id":            "acct-1",
			"access_token":  "SECRET-ACCESS-TOKEN",
			"credential":    map[string]interface{}{"refresh_token": "SECRET-REFRESH-TOKEN"},
			"display_name":  "alice-work",
			"refresh_token": "SECRET-REFRESH-TOKEN",
		},
	}
	out, err := h.transformResponse("Get", hostile)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, marker := range []string{"SECRET-ACCESS-TOKEN", "SECRET-REFRESH-TOKEN"} {
		if strings.Contains(string(encoded), marker) {
			t.Errorf("decoded account retained credential marker %q: %s", marker, encoded)
		}
	}
	assertNoCredentialKeys(t, encoded)
}

// nonSecretCredentialKeys are the two metadata field names that contain the
// word "credential" without carrying any. Both are derived from a timestamp
// mirrored onto the account's non-secret record — an expiry is not credential
// material — so the guards below match on JSON KEYS with these two excluded,
// rather than scanning the whole document for the substring and tripping over
// its own vocabulary.
var nonSecretCredentialKeys = map[string]bool{
	"credential_expires_at": true,
	"credential_health":     true,
}

// assertNoCredentialKeys fails when encoded carries a key that looks like
// credential material.
func assertNoCredentialKeys(t *testing.T, encoded []byte) {
	t.Helper()
	var decoded map[string]interface{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for key := range decoded {
		if nonSecretCredentialKeys[key] {
			continue
		}
		for _, marker := range []string{"credential", "access_token", "refresh_token", "env", "files", "secret", "token"} {
			if strings.Contains(key, marker) {
				t.Errorf("serialized a credential-shaped key %q: %s", key, encoded)
			}
		}
	}
}
