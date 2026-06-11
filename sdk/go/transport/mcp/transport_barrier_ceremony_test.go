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

// transport_barrier_ceremony_test.go exercises the BarrierClient and
// CeremonyClient implementations that *Transport satisfies for the qrdb
// wire contract, driving each method through a fake JSON-RPC MCP server.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// toolResultFn maps a tool name and its decoded arguments to the JSON object
// the tool returns inside its text content.
type toolResultFn func(t *testing.T, tool string, args map[string]interface{}) map[string]interface{}

// newMultiToolMCPServer starts an httptest server that answers initialize and
// any number of tools/call invocations, delegating the result to fn. It
// records the tool names that were called for assertion.
func newMultiToolMCPServer(t *testing.T, fn toolResultFn) (*Transport, *[]string, func()) {
	t.Helper()
	var called []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}

		var result interface{}
		switch req.Method {
		case "initialize":
			result = map[string]interface{}{
				"protocolVersion": MCPVersion,
				"serverInfo":      map[string]interface{}{"name": "TestServer", "version": "1.0.0"},
				"capabilities":    map[string]interface{}{},
			}
		case "tools/call":
			params, _ := req.Params.(map[string]interface{})
			toolName, _ := params["name"].(string)
			called = append(called, toolName)
			args, _ := params["arguments"].(map[string]interface{})
			payload := map[string]interface{}{}
			if fn != nil {
				payload = fn(t, toolName, args)
			}
			result = map[string]interface{}{
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": string(mustMarshalJSON(payload))},
				},
				"isError": false,
			}
		default:
			t.Errorf("unexpected method %q", req.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mustMarshalJSON(result),
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))

	tr, err := New(&transport.Config{Address: srv.URL, Timeout: 5 * time.Second})
	if err != nil {
		srv.Close()
		t.Fatalf("New: %v", err)
	}
	return tr, &called, func() {
		tr.Close()
		srv.Close()
	}
}

// =============================================================================
// Barrier happy-path coverage
// =============================================================================

func TestBarrierMethodsEndToEnd(t *testing.T) {
	ctx := context.Background()
	tr, called, cleanup := newMultiToolMCPServer(t, func(t *testing.T, tool string, args map[string]interface{}) map[string]interface{} {
		switch tool {
		case "qrdb_barrier_status":
			return map[string]interface{}{"sealed": false, "strategy": "shamir", "hardware_backed": true}
		case "qrdb_barrier_shamir_initialize":
			return map[string]interface{}{"threshold": 2, "total_shares": 3, "share_count": 3}
		case "qrdb_barrier_shamir_unseal_share":
			return map[string]interface{}{"required": 2, "submitted": 1, "complete": false}
		case "qrdb_barrier_shamir_list_shares":
			return map[string]interface{}{"count": 3, "threshold": 2, "total": 3}
		case "qrdb_barrier_rekey":
			return map[string]interface{}{"threshold": 2, "total_shares": 2, "share_count": 2}
		case "qrdb_barrier_recovery_generate":
			return map[string]interface{}{"threshold": 2, "total": 2, "share_count": 2}
		case "qrdb_barrier_recovery_status":
			return map[string]interface{}{"has_recovery_keys": true}
		case "qrdb_barrier_root_token":
			return map[string]interface{}{"token": "root-token", "created_at": "2026-05-29T00:00:00Z"}
		default:
			return map[string]interface{}{"ok": true}
		}
	})
	defer cleanup()

	if err := tr.BarrierInitialize(ctx, &qrdbtransport.BarrierInitializeRequest{Secret: "pw"}); err != nil {
		t.Fatalf("BarrierInitialize: %v", err)
	}
	if err := tr.BarrierUnseal(ctx, &qrdbtransport.BarrierUnsealRequest{Secret: "pw"}); err != nil {
		t.Fatalf("BarrierUnseal: %v", err)
	}
	if err := tr.BarrierSeal(ctx); err != nil {
		t.Fatalf("BarrierSeal: %v", err)
	}

	status, err := tr.BarrierStatus(ctx)
	if err != nil {
		t.Fatalf("BarrierStatus: %v", err)
	}
	if status.Sealed || status.Strategy != "shamir" || !status.HardwareBacked {
		t.Errorf("status mismatch: %+v", status)
	}

	shamir, err := tr.BarrierInitializeShamir(ctx, &qrdbtransport.BarrierInitializeShamirRequest{
		Secret: "pw", Threshold: 2, TotalShares: 3})
	if err != nil {
		t.Fatalf("BarrierInitializeShamir: %v", err)
	}
	if shamir.ShareCount != 3 || shamir.Threshold != 2 || shamir.TotalShares != 3 {
		t.Errorf("shamir mismatch: %+v", shamir)
	}

	share, err := tr.BarrierUnsealWithShare(ctx, &qrdbtransport.BarrierUnsealShareRequest{Share: "s1"})
	if err != nil {
		t.Fatalf("BarrierUnsealWithShare: %v", err)
	}
	if share.Required != 2 || share.Submitted != 1 || share.Complete {
		t.Errorf("share progress mismatch: %+v", share)
	}

	if err := tr.BarrierUnsealWithShares(ctx, &qrdbtransport.BarrierUnsealSharesRequest{Shares: []string{"s1", "s2"}}); err != nil {
		t.Fatalf("BarrierUnsealWithShares: %v", err)
	}

	rekey, err := tr.BarrierRekey(ctx, &qrdbtransport.BarrierRekeyRequest{Threshold: 2, Total: 2})
	if err != nil {
		t.Fatalf("BarrierRekey: %v", err)
	}
	if rekey.ShareCount != 2 || rekey.Threshold != 2 || rekey.TotalShares != 2 {
		t.Errorf("rekey mismatch: %+v", rekey)
	}

	recovery, err := tr.BarrierGenerateRecoveryKeys(ctx, &qrdbtransport.BarrierGenerateRecoveryKeysRequest{Threshold: 2, Total: 2})
	if err != nil {
		t.Fatalf("BarrierGenerateRecoveryKeys: %v", err)
	}
	if recovery.ShareCount != 2 || recovery.Threshold != 2 || recovery.Total != 2 {
		t.Errorf("recovery mismatch: %+v", recovery)
	}

	if err := tr.BarrierRecoverWithKeys(ctx, &qrdbtransport.BarrierRecoverWithKeysRequest{Keys: []string{"r1", "r2"}}); err != nil {
		t.Fatalf("BarrierRecoverWithKeys: %v", err)
	}
	if err := tr.BarrierDeleteRecoveryKeys(ctx); err != nil {
		t.Fatalf("BarrierDeleteRecoveryKeys: %v", err)
	}

	has, err := tr.BarrierHasRecoveryKeys(ctx)
	if err != nil {
		t.Fatalf("BarrierHasRecoveryKeys: %v", err)
	}
	if !has.HasKeys {
		t.Error("expected HasKeys true")
	}

	root, err := tr.BarrierGenerateRootToken(ctx, &qrdbtransport.BarrierGenerateRootTokenRequest{Shares: []string{"s1", "s2"}})
	if err != nil {
		t.Fatalf("BarrierGenerateRootToken: %v", err)
	}
	if root.Token != "root-token" {
		t.Errorf("root token = %q", root.Token)
	}

	if len(*called) == 0 {
		t.Error("expected tool calls to be recorded")
	}
}

// =============================================================================
// Barrier nil-request validation
// =============================================================================

func TestBarrierNilRequestErrors(t *testing.T) {
	tr, err := New(&transport.Config{Address: "http://localhost:1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()
	ctx := context.Background()

	if err := tr.BarrierInitialize(ctx, nil); err == nil {
		t.Error("BarrierInitialize(nil) must error")
	}
	if err := tr.BarrierUnseal(ctx, nil); err == nil {
		t.Error("BarrierUnseal(nil) must error")
	}
	if _, err := tr.BarrierInitializeShamir(ctx, nil); err == nil {
		t.Error("BarrierInitializeShamir(nil) must error")
	}
	if _, err := tr.BarrierUnsealWithShare(ctx, nil); err == nil {
		t.Error("BarrierUnsealWithShare(nil) must error")
	}
	if err := tr.BarrierUnsealWithShares(ctx, nil); err == nil {
		t.Error("BarrierUnsealWithShares(nil) must error")
	}
	if _, err := tr.BarrierRekey(ctx, nil); err == nil {
		t.Error("BarrierRekey(nil) must error")
	}
	if _, err := tr.BarrierGenerateRecoveryKeys(ctx, nil); err == nil {
		t.Error("BarrierGenerateRecoveryKeys(nil) must error")
	}
	if err := tr.BarrierRecoverWithKeys(ctx, nil); err == nil {
		t.Error("BarrierRecoverWithKeys(nil) must error")
	}
	if _, err := tr.BarrierGenerateRootToken(ctx, nil); err == nil {
		t.Error("BarrierGenerateRootToken(nil) must error")
	}
}

// =============================================================================
// Barrier closed-transport short circuit
// =============================================================================

func TestBarrierClosedTransport(t *testing.T) {
	tr, err := New(&transport.Config{Address: "http://localhost:1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	ctx := context.Background()

	if err := tr.BarrierSeal(ctx); err == nil {
		t.Error("BarrierSeal on closed transport must error")
	}
	if _, err := tr.BarrierStatus(ctx); err == nil {
		t.Error("BarrierStatus on closed transport must error")
	}
	if _, err := tr.BarrierRekey(ctx, &qrdbtransport.BarrierRekeyRequest{Threshold: 2, Total: 2}); err == nil {
		t.Error("BarrierRekey on closed transport must error")
	}
}

// =============================================================================
// Ceremony coverage
// =============================================================================

func TestCeremonyMethodsEndToEnd(t *testing.T) {
	ctx := context.Background()
	tr, _, cleanup := newMultiToolMCPServer(t, func(t *testing.T, tool string, args map[string]interface{}) map[string]interface{} {
		switch tool {
		case "qrdb_ceremony_initialize":
			return map[string]interface{}{"session_id": "sess-1"}
		case "qrdb_ceremony_status":
			return map[string]interface{}{"state": "in_progress"}
		case "qrdb_ceremony_claim_cert_begin":
			return map[string]interface{}{"nonce_hex": "deadbeef"}
		case "qrdb_ceremony_claim_cert_complete":
			return map[string]interface{}{"success": true}
		case "qrdb_ceremony_claim_share":
			return map[string]interface{}{"share": "share-json"}
		case "qrdb_ceremony_sign_csr":
			return map[string]interface{}{"certificate_pem": "CERT"}
		case "qrdb_ceremony_enroll_external_officer":
			return map[string]interface{}{"success": true}
		default:
			return map[string]interface{}{"ok": true}
		}
	})
	defer cleanup()

	if _, err := tr.CeremonyInitialize(ctx, &qrdbtransport.CeremonyInitializeRequest{
		SOPin:                  "so",
		UserPin:                "user",
		Threshold:              2,
		CredentialSealStrategy: "shamir",
		Hostname:               "host",
		SANs:                   []string{"a", "b"},
		Officers: []qrdbtransport.CeremonyOfficerRequest{
			{Username: "alice", CSRPEM: "CSR"},
		},
	}); err != nil {
		t.Fatalf("CeremonyInitialize: %v", err)
	}

	if _, err := tr.CeremonyStatus(ctx); err != nil {
		t.Fatalf("CeremonyStatus: %v", err)
	}

	if _, err := tr.CeremonyClaimCertBegin(ctx, &qrdbtransport.ClaimCertBeginRequest{Username: "alice"}); err != nil {
		t.Fatalf("CeremonyClaimCertBegin: %v", err)
	}

	if _, err := tr.CeremonyClaimCertComplete(ctx, &qrdbtransport.ClaimCertCompleteRequest{
		Username: "alice", NonceHex: "deadbeef", Signature: "sig"}); err != nil {
		t.Fatalf("CeremonyClaimCertComplete: %v", err)
	}

	share, err := tr.CeremonyClaimShare(ctx, &qrdbtransport.ClaimShareRequest{Username: "alice"})
	if err != nil {
		t.Fatalf("CeremonyClaimShare: %v", err)
	}
	if share.ShareJSON != "share-json" {
		t.Errorf("share json = %q", share.ShareJSON)
	}

	if _, err := tr.CeremonySignCSR(ctx, &qrdbtransport.SignCSRInitRequest{
		Username: "alice", SOPin: "so", CSRPEM: "CSR", Role: "officer"}); err != nil {
		t.Fatalf("CeremonySignCSR: %v", err)
	}

	if _, err := tr.CeremonyEnrollExternalOfficer(ctx, &qrdbtransport.EnrollExternalOfficerRequest{
		Username:   "bob",
		ShareIndex: 1,
		AuthFactor: "piv",
		CertPEM:    "CERT",
		PubKeyPEM:  "PUB",
		ShareBytes: []byte("share-bytes"),
	}); err != nil {
		t.Fatalf("CeremonyEnrollExternalOfficer: %v", err)
	}
}

func TestCeremonyNilRequestErrors(t *testing.T) {
	tr, err := New(&transport.Config{Address: "http://localhost:1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()
	ctx := context.Background()

	if _, err := tr.CeremonyInitialize(ctx, nil); err == nil {
		t.Error("CeremonyInitialize(nil) must error")
	}
	if _, err := tr.CeremonyClaimCertBegin(ctx, nil); err == nil {
		t.Error("CeremonyClaimCertBegin(nil) must error")
	}
	if _, err := tr.CeremonyClaimCertComplete(ctx, nil); err == nil {
		t.Error("CeremonyClaimCertComplete(nil) must error")
	}
	if _, err := tr.CeremonyClaimShare(ctx, nil); err == nil {
		t.Error("CeremonyClaimShare(nil) must error")
	}
	if _, err := tr.CeremonySignCSR(ctx, nil); err == nil {
		t.Error("CeremonySignCSR(nil) must error")
	}
	if _, err := tr.CeremonyEnrollExternalOfficer(ctx, nil); err == nil {
		t.Error("CeremonyEnrollExternalOfficer(nil) must error")
	}
}

// =============================================================================
// Helper unit tests
// =============================================================================

func TestStringsToInterfaces(t *testing.T) {
	if got := stringsToInterfaces(nil); len(got) != 0 {
		t.Errorf("nil input = %v, want empty", got)
	}
	got := stringsToInterfaces([]string{"a", "b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("stringsToInterfaces = %v", got)
	}
}

func TestRemarshalBarrierNil(t *testing.T) {
	var dst struct{ X int }
	if err := remarshalBarrier(nil, &dst); err != nil {
		t.Errorf("remarshalBarrier(nil) = %v", err)
	}
	if err := remarshalBarrier(map[string]interface{}{"X": 5}, &dst); err != nil {
		t.Fatalf("remarshalBarrier: %v", err)
	}
	if dst.X != 5 {
		t.Errorf("X = %d, want 5", dst.X)
	}
}
