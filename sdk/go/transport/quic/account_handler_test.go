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

package quic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
)

// TestIsAccountServiceQUIC pins the routing predicate.
func TestIsAccountServiceQUIC(t *testing.T) {
	for _, name := range []string{"Account", "quicbaas.Account"} {
		if !isAccountService(name) {
			t.Errorf("isAccountService(%q) = false", name)
		}
	}
	for _, name := range []string{"quicbaas.Orchestrator", "quicbaas.KV"} {
		if isAccountService(name) {
			t.Errorf("isAccountService(%q) = true", name)
		}
	}
}

// TestAccountMethodToSubOp pins the sub-op codes. They MUST match
// pkg/api/quic/protocol.go byte-for-byte: a drift here silently routes an
// account write to a different server handler.
func TestAccountMethodToSubOp(t *testing.T) {
	want := map[string]byte{
		"Create":           0x30,
		"Get":              0x31,
		"List":             0x32,
		"Update":           0x33,
		"Delete":           0x34,
		"RotateCredential": 0x35,
		"Revoke":           0x36,
		"SetDefault":       0x37,
		"Usage":            0x38,
	}
	for method, expected := range want {
		got, ok := AccountMethodToSubOp(method)
		if !ok {
			t.Errorf("%s has no sub-op", method)
			continue
		}
		if got != expected {
			t.Errorf("%s -> 0x%02X, want 0x%02X", method, got, expected)
		}
	}
	if _, ok := AccountMethodToSubOp("Teleport"); ok {
		t.Error("an unknown method must not resolve to a sub-op")
	}
}

// TestAccountMethodsMapToOrchestratorOpcode pins that every account method
// resolves to the shared OpOrchestrator dispatch byte. A missing entry would
// make MethodToOpcode fail and the call would never leave the client.
func TestAccountMethodsMapToOrchestratorOpcode(t *testing.T) {
	for method := range accountMethodToSubOp {
		opcode, ok := MethodToOpcode("quicbaas.Account", method)
		if !ok {
			t.Errorf("quicbaas.Account/%s has no opcode", method)
			continue
		}
		if opcode != OpOrchestrator {
			t.Errorf("quicbaas.Account/%s -> 0x%02X, want OpOrchestrator (0x%02X)", method, opcode, OpOrchestrator)
		}
	}
}

// TestDecodeAccountResponse_HappyPaths drives every converter against the
// server's real envelope shape (the pkg/service response types).
func TestDecodeAccountResponse_HappyPaths(t *testing.T) {
	envelope := []byte(`{"account":{"id":"acct-1","provider":"claude","status":"active"},"timestamp":"2026-01-01T00:00:00Z"}`)
	list := []byte(`{"accounts":[{"id":"acct-1"}],"total":1,"timestamp":"2026-01-01T00:00:00Z"}`)
	ack := []byte(`{"account_id":"acct-1","timestamp":"2026-01-01T00:00:00Z"}`)
	del := []byte(`{"account_id":"acct-1","deleted":true,"timestamp":"2026-01-01T00:00:00Z"}`)
	usage := []byte(`{"usage":{"total_tokens":42},"timestamp":"2026-01-01T00:00:00Z"}`)

	for _, method := range []string{"Create", "Get", "Update", "Revoke"} {
		out, err := decodeAccountResponse(method, envelope)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		info, ok := out.(*account.AccountInfo)
		if !ok || info.ID != "acct-1" || info.Provider != account.ProviderClaude {
			t.Fatalf("%s decoded to %#v", method, out)
		}
	}

	out, err := decodeAccountResponse("List", list)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if resp, ok := out.(*account.ListAccountsResponse); !ok || resp.Total != 1 || len(resp.Accounts) != 1 {
		t.Fatalf("List decoded to %#v", out)
	}

	for _, method := range []string{"RotateCredential", "SetDefault"} {
		out, err := decodeAccountResponse(method, ack)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		if resp, ok := out.(*account.AccountAckResponse); !ok || resp.AccountID != "acct-1" {
			t.Fatalf("%s decoded to %#v", method, out)
		}
	}

	out, err = decodeAccountResponse("Delete", del)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if resp, ok := out.(*account.DeleteAccountResponse); !ok || !resp.Deleted {
		t.Fatalf("Delete decoded to %#v", out)
	}

	out, err = decodeAccountResponse("Usage", usage)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if report, ok := out.(*account.UsageReport); !ok || report.TotalTokens != 42 {
		t.Fatalf("Usage decoded to %#v", out)
	}
}

// TestDecodeAccountResponse_ErrorPaths covers the decode failure modes.
func TestDecodeAccountResponse_ErrorPaths(t *testing.T) {
	if out, err := decodeAccountResponse("Get", nil); err != nil || out != nil {
		t.Errorf("an empty payload must decode to (nil, nil); got (%#v, %v)", out, err)
	}
	if _, err := decodeAccountResponse("Teleport", []byte(`{}`)); err == nil {
		t.Error("an unknown method must fail rather than degrade to an untyped map")
	}
	if _, err := decodeAccountResponse("Get", []byte("not-json")); err == nil {
		t.Error("malformed JSON must fail")
	}
	if _, err := decodeAccountResponse("Get", []byte(`{}`)); err == nil {
		t.Error("an envelope with no account must fail")
	}
	if _, err := decodeAccountResponse("Usage", []byte(`{}`)); err == nil {
		t.Error("an envelope with no usage report must fail")
	}
	out, err := decodeAccountResponse("List", []byte(`{"total":0}`))
	if err != nil {
		t.Fatalf("List with no accounts: %v", err)
	}
	if resp, ok := out.(*account.ListAccountsResponse); !ok || resp.Accounts == nil {
		t.Fatalf("List must normalize a missing array to an empty slice; got %#v", out)
	}
}

// TestAccountWireShapesCarryNoCredential is the leak guard on the client side:
// even if a server were to send credential material, none of the wire structs
// has a field to receive it, so it cannot reach a caller.
func TestAccountWireShapesCarryNoCredential(t *testing.T) {
	hostile := []byte(`{"account":{"id":"acct-1","access_token":"SECRET-ACCESS-TOKEN",
		"credential":{"refresh_token":"SECRET-REFRESH-TOKEN"}},"timestamp":"2026-01-01T00:00:00Z"}`)
	out, err := decodeAccountResponse("Get", hostile)
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
