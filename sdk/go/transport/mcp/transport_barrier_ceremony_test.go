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
// wire contract, driving each method through a real in-process MCP server
// (github.com/mark3labs/mcp-go/server).

package mcp

import (
	"context"
	"encoding/base64"
	stderrors "errors"
	"net/http/httptest"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// toolResultFn maps a tool name and its decoded arguments to the JSON object
// the tool returns inside its text content.
type toolResultFn func(t *testing.T, tool string, args map[string]interface{}) map[string]interface{}

// newMultiToolMCPServer starts a real in-process MCP server registering
// every name in tools, delegating each call's result to fn. It records the
// tool names that were called for assertion.
func newMultiToolMCPServer(t *testing.T, tools []string, fn toolResultFn) (*Transport, *[]string, func()) {
	t.Helper()
	srv, called := newMultiToolServer(t, tools, fn)
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv)

	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	if err != nil {
		httpSrv.Close()
		t.Fatalf("New: %v", err)
	}
	return tr, called, func() {
		_ = tr.Close()
		httpSrv.Close()
	}
}

// newMultiToolMCPServerTLS is newMultiToolMCPServer over HTTPS, for calls
// that only travel over TLS, such as one carrying an SO PIN.
func newMultiToolMCPServerTLS(t *testing.T, tools []string, fn toolResultFn) (*Transport, *[]string) {
	t.Helper()
	srv, called := newMultiToolServer(t, tools, fn)
	httpSrv := httptest.NewUnstartedServer(mcpserver.NewStreamableHTTPServer(srv))
	httpSrv.StartTLS()
	t.Cleanup(httpSrv.Close)

	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second},
		WithHTTPClient(httpSrv.Client()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return tr, called
}

// newMultiToolServer registers tools on an MCP server whose handler records
// each called tool and answers with fn's payload.
func newMultiToolServer(t *testing.T, tools []string, fn toolResultFn) (*mcpserver.MCPServer, *[]string) {
	t.Helper()
	var called []string

	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	handler := func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		called = append(called, req.Params.Name)
		payload := map[string]interface{}{}
		if fn != nil {
			payload = fn(t, req.Params.Name, req.GetArguments())
		}
		return mcpgo.NewToolResultText(string(mustMarshalJSON(payload))), nil
	}
	for _, name := range tools {
		srv.AddTool(mcpgo.NewTool(name), handler)
	}
	return srv, &called
}

// =============================================================================
// Barrier happy-path coverage
// =============================================================================

var barrierToolNames = []string{
	"qrdb_barrier_initialize",
	"qrdb_barrier_unseal",
	"qrdb_barrier_seal",
	"qrdb_barrier_status",
	"qrdb_barrier_shamir_initialize",
	"qrdb_barrier_shamir_unseal_share",
	"qrdb_barrier_shamir_unseal_shares",
	"qrdb_barrier_rekey",
	"qrdb_barrier_recovery_generate",
	"qrdb_barrier_recovery_recover",
	"qrdb_barrier_recovery_delete",
	"qrdb_barrier_recovery_status",
	"qrdb_barrier_root_token",
}

func TestBarrierMethodsEndToEnd(t *testing.T) {
	ctx := context.Background()
	tr, called, cleanup := newMultiToolMCPServer(t, barrierToolNames, func(t *testing.T, tool string, args map[string]interface{}) map[string]interface{} {
		switch tool {
		case "qrdb_barrier_status":
			return map[string]interface{}{"sealed": false, "strategy": "shamir", "hardware_backed": true}
		case "qrdb_barrier_shamir_initialize":
			return map[string]interface{}{"threshold": 2, "total_shares": 3, "share_count": 3}
		case "qrdb_barrier_shamir_unseal_share":
			return map[string]interface{}{"required": 2, "submitted": 1, "complete": false}
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

	if err := tr.BarrierInitialize(ctx, &qrdbtransport.BarrierInitializeRequest{Secret: []byte("pw")}); err != nil {
		t.Fatalf("BarrierInitialize: %v", err)
	}
	if err := tr.BarrierUnseal(ctx, &qrdbtransport.BarrierUnsealRequest{Secret: []byte("pw")}); err != nil {
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
		Secret: []byte("pw"), Threshold: 2, TotalShares: 3})
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

var ceremonyToolNames = []string{
	"qrdb_ceremony_initialize",
	"qrdb_ceremony_status",
	"qrdb_ceremony_claim_cert_begin",
	"qrdb_ceremony_claim_cert_complete",
	"qrdb_ceremony_begin_claim_share",
	"qrdb_ceremony_complete_claim_share",
	"qrdb_ceremony_sign_csr",
	"qrdb_ceremony_enroll_external_officer",
}

func TestCeremonyMethodsEndToEnd(t *testing.T) {
	ctx := context.Background()
	tr, _, cleanup := newMultiToolMCPServer(t, ceremonyToolNames, func(t *testing.T, tool string, args map[string]interface{}) map[string]interface{} {
		switch tool {
		case "qrdb_ceremony_initialize":
			return map[string]interface{}{"session_id": "sess-1"}
		case "qrdb_ceremony_status":
			return map[string]interface{}{"state": "in_progress"}
		case "qrdb_ceremony_claim_cert_begin":
			return map[string]interface{}{"nonce_hex": "deadbeef"}
		case "qrdb_ceremony_claim_cert_complete":
			return map[string]interface{}{"success": true}
		case "qrdb_ceremony_begin_claim_share":
			return map[string]interface{}{"nonce_hex": "deadbeef", "expires_at": "2026-08-06T12:00:00Z"}
		case "qrdb_ceremony_complete_claim_share":
			return map[string]interface{}{"share_index": 2, "share": "c2hhcmUtanNvbg=="}
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

	begin, err := tr.CeremonyBeginClaimShare(ctx, &qrdbtransport.BeginClaimShareRequest{Username: "alice"})
	if err != nil {
		t.Fatalf("CeremonyBeginClaimShare: %v", err)
	}
	if begin.NonceHex != "deadbeef" {
		t.Errorf("nonce = %q", begin.NonceHex)
	}

	share, err := tr.CeremonyCompleteClaimShare(ctx, &qrdbtransport.CompleteClaimShareRequest{
		Username: "alice", NonceHex: "deadbeef", Signature: []byte("sig")})
	if err != nil {
		t.Fatalf("CeremonyCompleteClaimShare: %v", err)
	}
	if share.ShareIndex != 2 {
		t.Errorf("share index = %d", share.ShareIndex)
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

// TestCeremonyEnrollExternalOfficer_ShareBytesTravelBase64 proves a binary
// Shamir share crosses MCP base64-encoded, so bytes that are not valid UTF-8
// reach the server unchanged, and that an empty share sends no share_bytes.
func TestCeremonyEnrollExternalOfficer_ShareBytesTravelBase64(t *testing.T) {
	cases := []struct {
		name     string
		share    []byte
		wantSent bool
	}{
		{name: "binary share", share: []byte{0x00, 0xff, 0x80, 0xfe}, wantSent: true},
		{name: "empty share", share: nil, wantSent: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var (
				got  interface{}
				sent bool
			)
			tr, _, cleanup := newMultiToolMCPServer(t, ceremonyToolNames, func(t *testing.T, tool string, args map[string]interface{}) map[string]interface{} {
				if tool == "qrdb_ceremony_enroll_external_officer" {
					got, sent = args["share_bytes"]
				}
				return map[string]interface{}{"sealed_share": "sealed"}
			})
			defer cleanup()

			if _, err := tr.CeremonyEnrollExternalOfficer(context.Background(), &qrdbtransport.EnrollExternalOfficerRequest{
				Username:   "bob",
				AuthFactor: "piv",
				CertPEM:    "CERT",
				ShareBytes: c.share,
			}); err != nil {
				t.Fatalf("CeremonyEnrollExternalOfficer: %v", err)
			}
			if sent != c.wantSent {
				t.Fatalf("share_bytes sent = %v, want %v", sent, c.wantSent)
			}
			if !c.wantSent {
				return
			}
			want := base64.StdEncoding.EncodeToString(c.share)
			if got != want {
				t.Fatalf("share_bytes = %#v, want base64 %q", got, want)
			}
		})
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
	if _, err := tr.CeremonyBeginClaimShare(ctx, nil); err == nil {
		t.Error("CeremonyBeginClaimShare(nil) must error")
	}
	if _, err := tr.CeremonyCompleteClaimShare(ctx, nil); err == nil {
		t.Error("CeremonyCompleteClaimShare(nil) must error")
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

// TestBarrierCredentialsCrossAsPlaintext pins MCP's documented exception to
// go-qrdb's wipeable-credential contract.
//
// The request fields are []byte since go-qrdb#184, and Go's JSON encoder
// renders a []byte as BASE64. Handing one straight to an MCP tool argument
// therefore sends "cHc=" where the daemon expects "pw", and an existing
// barrier can no longer be unsealed with its real passphrase over this
// transport. The failure is invisible to a test that only checks the call
// returned nil, which is exactly what the earlier version of this file did.
//
// So this asserts what the SERVER received, not that the client succeeded.
func TestBarrierCredentialsCrossAsPlaintext(t *testing.T) {
	ctx := context.Background()
	received := map[string]string{}

	tr, _, cleanup := newMultiToolMCPServer(t, barrierToolNames, func(t *testing.T, tool string, args map[string]interface{}) map[string]interface{} {
		if v, ok := args["secret"]; ok {
			s, isString := v.(string)
			if !isString {
				t.Errorf("%s: secret arrived as %T, but an MCP tool argument must be a JSON string", tool, v)
				return map[string]interface{}{"ok": true}
			}
			received[tool] = s
		}
		switch tool {
		case "qrdb_barrier_shamir_initialize":
			return map[string]interface{}{"threshold": 2, "total_shares": 3, "share_count": 3}
		default:
			return map[string]interface{}{"ok": true}
		}
	})
	defer cleanup()

	const passphrase = "correct horse battery staple"

	if err := tr.BarrierInitialize(ctx, &qrdbtransport.BarrierInitializeRequest{Secret: []byte(passphrase)}); err != nil {
		t.Fatalf("BarrierInitialize: %v", err)
	}
	if err := tr.BarrierUnseal(ctx, &qrdbtransport.BarrierUnsealRequest{Secret: []byte(passphrase)}); err != nil {
		t.Fatalf("BarrierUnseal: %v", err)
	}
	if _, err := tr.BarrierInitializeShamir(ctx, &qrdbtransport.BarrierInitializeShamirRequest{
		Secret: []byte(passphrase), Threshold: 2, TotalShares: 3}); err != nil {
		t.Fatalf("BarrierInitializeShamir: %v", err)
	}

	for _, tool := range []string{"qrdb_barrier_initialize", "qrdb_barrier_unseal", "qrdb_barrier_shamir_initialize"} {
		got, ok := received[tool]
		if !ok {
			t.Errorf("%s: no secret argument reached the server", tool)
			continue
		}
		if got != passphrase {
			t.Errorf("%s: server received %q, want the plaintext %q (base64 would be %q)",
				tool, got, passphrase, base64.StdEncoding.EncodeToString([]byte(passphrase)))
		}
	}
}

// recordSealPIN returns a tool handler that keeps the so_pin argument the
// seal tool received, and whether it received one at all.
func recordSealPIN(pin *string, sent *bool) toolResultFn {
	return func(t *testing.T, tool string, args map[string]interface{}) map[string]interface{} {
		if tool == "qrdb_barrier_seal" {
			v, ok := args["so_pin"]
			*sent = ok
			if ok {
				s, isString := v.(string)
				if !isString {
					t.Errorf("so_pin arrived as %T, want a JSON string", v)
				}
				*pin = s
			}
		}
		return map[string]interface{}{"status": "sealed"}
	}
}

func TestBarrierSeal_SendsBoundSOPIN(t *testing.T) {
	const soPIN = "so-pin-1234"
	var (
		got  string
		sent bool
	)
	tr, _ := newMultiToolMCPServerTLS(t, barrierToolNames, recordSealPIN(&got, &sent))

	ctx, release, err := qrdbsdk.WithSOPIN(context.Background(), []byte(soPIN))
	if err != nil {
		t.Fatalf("WithSOPIN: %v", err)
	}
	defer release()

	if err := tr.BarrierSeal(ctx); err != nil {
		t.Fatalf("BarrierSeal: %v", err)
	}
	if !sent || got != soPIN {
		t.Fatalf("server received so_pin %q (sent=%v), want %q", got, sent, soPIN)
	}
}

func TestBarrierSeal_NoBoundSOPINSendsNone(t *testing.T) {
	var (
		got  string
		sent bool
	)
	tr, _ := newMultiToolMCPServerTLS(t, barrierToolNames, recordSealPIN(&got, &sent))

	if err := tr.BarrierSeal(context.Background()); err != nil {
		t.Fatalf("BarrierSeal: %v", err)
	}
	if sent {
		t.Fatalf("an unbound context must send no so_pin, server received %q", got)
	}
}

func TestBarrierSeal_SOPINRefusedOverPlaintext(t *testing.T) {
	var (
		got  string
		sent bool
	)
	tr, called, cleanup := newMultiToolMCPServer(t, barrierToolNames, recordSealPIN(&got, &sent))
	defer cleanup()

	ctx, release, err := qrdbsdk.WithSOPIN(context.Background(), []byte("so-pin-1234"))
	if err != nil {
		t.Fatalf("WithSOPIN: %v", err)
	}
	defer release()

	err = tr.BarrierSeal(ctx)
	var tlsErr *qrdbtransport.SOPINRequiresTLSError
	if !stderrors.As(err, &tlsErr) {
		t.Fatalf("expected SOPINRequiresTLSError, got %v", err)
	}
	if len(*called) != 0 {
		t.Fatalf("the PIN must not be sent over http://, server saw %v", *called)
	}
}
