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
	mcpservertest "github.com/mark3labs/mcp-go/server/servertest"

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
	httpSrv := mcpservertest.NewTestStreamableHTTPServer(srv)

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
	"qrdb_barrier_recovery_generate",
	"qrdb_barrier_recovery_delete",
	"qrdb_barrier_recovery_status",
	"qrdb_barrier_cluster_unseal",
	"qrdb_barrier_quorum_begin",
	"qrdb_barrier_quorum_submit",
	"qrdb_barrier_rekey_start",
	"qrdb_barrier_officer_revoke",
}

func TestBarrierMethodsEndToEnd(t *testing.T) {
	ctx := context.Background()
	tr, called := newMultiToolMCPServerTLS(t, barrierToolNames, func(t *testing.T, tool string, args map[string]interface{}) map[string]interface{} {
		switch tool {
		case "qrdb_barrier_status":
			return map[string]interface{}{"sealed": false, "strategy": "shamir", "hardware_backed": true}
		case "qrdb_barrier_shamir_initialize":
			return map[string]interface{}{"threshold": 2, "total_shares": 3, "share_count": 3}
		case "qrdb_barrier_recovery_generate":
			return map[string]interface{}{"threshold": 2, "total": 2, "share_count": 2}
		case "qrdb_barrier_recovery_status":
			return map[string]interface{}{"has_recovery_keys": true}
		case "qrdb_barrier_cluster_unseal":
			return map[string]interface{}{"active": true, "sealed": false, "epoch": 3}
		case "qrdb_barrier_quorum_begin":
			return map[string]interface{}{"challenge_id": "ch-1", "domain": args["domain"], "op": args["op"], "username": args["username"]}
		case "qrdb_barrier_quorum_submit":
			return map[string]interface{}{"domain": args["domain"], "op": args["op"], "required": 2, "submitted": 1, "complete": false}
		case "qrdb_barrier_rekey_start":
			return map[string]interface{}{"domain": args["domain"], "op": "rekey", "required": 2, "new_threshold": args["threshold"]}
		case "qrdb_barrier_officer_revoke":
			return map[string]interface{}{"domain": args["domain"], "op": "rekey", "required": 2, "remove": []interface{}{args["username"]}}
		default:
			return map[string]interface{}{"ok": true}
		}
	})

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
		Threshold: 2, TotalShares: 3, SOPin: "so"})
	if err != nil {
		t.Fatalf("BarrierInitializeShamir: %v", err)
	}
	if shamir.ShareCount != 3 || shamir.Threshold != 2 || shamir.TotalShares != 3 {
		t.Errorf("shamir mismatch: %+v", shamir)
	}

	recovery, err := tr.BarrierGenerateRecoveryKeys(ctx, &qrdbtransport.BarrierGenerateRecoveryKeysRequest{Threshold: 2, SOPin: "so"})
	if err != nil {
		t.Fatalf("BarrierGenerateRecoveryKeys: %v", err)
	}
	if recovery.ShareCount != 2 || recovery.Threshold != 2 || recovery.Total != 2 {
		t.Errorf("recovery mismatch: %+v", recovery)
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

	cluster, err := tr.BarrierClusterUnseal(ctx)
	if err != nil {
		t.Fatalf("BarrierClusterUnseal: %v", err)
	}
	if !cluster.Active || cluster.Sealed || cluster.Epoch != 3 {
		t.Errorf("cluster seal mismatch: %+v", cluster)
	}

	challenge, err := tr.BarrierQuorumBegin(ctx, &qrdbtransport.QuorumBeginRequest{Domain: "unseal", Op: "unseal", Username: "alice"})
	if err != nil {
		t.Fatalf("BarrierQuorumBegin: %v", err)
	}
	if challenge.ChallengeID != "ch-1" || challenge.Domain != "unseal" || challenge.Username != "alice" {
		t.Errorf("quorum challenge mismatch: %+v", challenge)
	}

	outcome, err := tr.BarrierQuorumSubmit(ctx, &qrdbtransport.QuorumSubmitRequest{
		Domain: "unseal", Op: "unseal", Username: "alice", ChallengeID: "ch-1", Factor: "mtls"})
	if err != nil {
		t.Fatalf("BarrierQuorumSubmit: %v", err)
	}
	if outcome.Required != 2 || outcome.Submitted != 1 || outcome.Complete {
		t.Errorf("quorum outcome mismatch: %+v", outcome)
	}

	round, err := tr.BarrierRekeyStart(ctx, &qrdbtransport.RekeyStartRequest{Domain: "unseal", Threshold: 3, SOPin: "so"})
	if err != nil {
		t.Fatalf("BarrierRekeyStart: %v", err)
	}
	if round.Op != "rekey" || round.NewThreshold != 3 {
		t.Errorf("rekey round mismatch: %+v", round)
	}

	revoke, err := tr.BarrierOfficerRevoke(ctx, &qrdbtransport.OfficerRevokeRequest{Domain: "unseal", Username: "bob", SOPin: "so"})
	if err != nil {
		t.Fatalf("BarrierOfficerRevoke: %v", err)
	}
	if len(revoke.Remove) != 1 || revoke.Remove[0] != "bob" {
		t.Errorf("revoke round mismatch: %+v", revoke)
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
	if _, err := tr.BarrierGenerateRecoveryKeys(ctx, nil); err == nil {
		t.Error("BarrierGenerateRecoveryKeys(nil) must error")
	}
	if _, err := tr.BarrierRekeyStart(ctx, nil); err == nil {
		t.Error("BarrierRekeyStart(nil) must error")
	}
	if _, err := tr.BarrierOfficerRevoke(ctx, nil); err == nil {
		t.Error("BarrierOfficerRevoke(nil) must error")
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
	if _, err := tr.BarrierQuorumSubmit(ctx, &qrdbtransport.QuorumSubmitRequest{Domain: "unseal", Op: "unseal"}); err == nil {
		t.Error("BarrierQuorumSubmit on closed transport must error")
	}
}

// =============================================================================
// Ceremony coverage
// =============================================================================

var ceremonyToolNames = []string{
	"qrdb_ceremony_initialize",
	"qrdb_ceremony_status",
	"qrdb_ceremony_sign_csr",
}

func TestCeremonyMethodsEndToEnd(t *testing.T) {
	ctx := context.Background()
	tr, _ := newMultiToolMCPServerTLS(t, ceremonyToolNames, func(t *testing.T, tool string, args map[string]interface{}) map[string]interface{} {
		switch tool {
		case "qrdb_ceremony_initialize":
			return map[string]interface{}{"session_id": "sess-1"}
		case "qrdb_ceremony_status":
			return map[string]interface{}{"state": "in_progress"}
		case "qrdb_ceremony_sign_csr":
			return map[string]interface{}{"certificate_pem": "CERT"}
		default:
			return map[string]interface{}{"ok": true}
		}
	})

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

	if _, err := tr.CeremonySignCSR(ctx, &qrdbtransport.SignCSRInitRequest{
		Username: "alice", SOPin: "so", CSRPEM: "CSR", Role: "officer"}); err != nil {
		t.Fatalf("CeremonySignCSR: %v", err)
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
	if _, err := tr.CeremonySignCSR(ctx, nil); err == nil {
		t.Error("CeremonySignCSR(nil) must error")
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

	tr, _ := newMultiToolMCPServerTLS(t, barrierToolNames, func(t *testing.T, tool string, args map[string]interface{}) map[string]interface{} {
		if v, ok := args["secret"]; ok {
			s, isString := v.(string)
			if !isString {
				t.Errorf("%s: secret arrived as %T, but an MCP tool argument must be a JSON string", tool, v)
				return map[string]interface{}{"ok": true}
			}
			received[tool] = s
		}
		return map[string]interface{}{"ok": true}
	})

	const passphrase = "correct horse battery staple"

	if err := tr.BarrierInitialize(ctx, &qrdbtransport.BarrierInitializeRequest{Secret: []byte(passphrase)}); err != nil {
		t.Fatalf("BarrierInitialize: %v", err)
	}
	if err := tr.BarrierUnseal(ctx, &qrdbtransport.BarrierUnsealRequest{Secret: []byte(passphrase)}); err != nil {
		t.Fatalf("BarrierUnseal: %v", err)
	}

	for _, tool := range []string{"qrdb_barrier_initialize", "qrdb_barrier_unseal"} {
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

// TestSOPINVerbsRefusedOverPlaintext covers every MCP verb that sends an SO
// PIN: over http:// each refuses with *SOPINRequiresTLSError before any tool
// call reaches the server, whether the PIN comes from the request or from
// the context bound by WithSOPIN.
func TestSOPINVerbsRefusedOverPlaintext(t *testing.T) {
	const pin = "so-pin-1234"
	tests := []struct {
		name  string
		bound bool
		call  func(ctx context.Context, tr *Transport) error
	}{
		{"BarrierSeal", true, func(ctx context.Context, tr *Transport) error {
			return tr.BarrierSeal(ctx)
		}},
		{"BarrierInitializeShamir", false, func(ctx context.Context, tr *Transport) error {
			_, err := tr.BarrierInitializeShamir(ctx, &qrdbtransport.BarrierInitializeShamirRequest{Threshold: 2, TotalShares: 3, SOPin: pin})
			return err
		}},
		{"BarrierInitializeShamir bound", true, func(ctx context.Context, tr *Transport) error {
			_, err := tr.BarrierInitializeShamir(ctx, &qrdbtransport.BarrierInitializeShamirRequest{Threshold: 2, TotalShares: 3})
			return err
		}},
		{"BarrierGenerateRecoveryKeys", false, func(ctx context.Context, tr *Transport) error {
			_, err := tr.BarrierGenerateRecoveryKeys(ctx, &qrdbtransport.BarrierGenerateRecoveryKeysRequest{Threshold: 2, SOPin: pin})
			return err
		}},
		{"BarrierRekeyStart", false, func(ctx context.Context, tr *Transport) error {
			_, err := tr.BarrierRekeyStart(ctx, &qrdbtransport.RekeyStartRequest{SOPin: pin})
			return err
		}},
		{"BarrierRekeyStart bound", true, func(ctx context.Context, tr *Transport) error {
			_, err := tr.BarrierRekeyStart(ctx, &qrdbtransport.RekeyStartRequest{})
			return err
		}},
		{"BarrierOfficerRevoke", false, func(ctx context.Context, tr *Transport) error {
			_, err := tr.BarrierOfficerRevoke(ctx, &qrdbtransport.OfficerRevokeRequest{SOPin: pin})
			return err
		}},
		{"CeremonyInitialize", false, func(ctx context.Context, tr *Transport) error {
			_, err := tr.CeremonyInitialize(ctx, &qrdbtransport.CeremonyInitializeRequest{SOPin: pin, Threshold: 1})
			return err
		}},
		{"CeremonySignCSR", false, func(ctx context.Context, tr *Transport) error {
			_, err := tr.CeremonySignCSR(ctx, &qrdbtransport.SignCSRInitRequest{Username: "officer", SOPin: pin})
			return err
		}},
	}
	tools := append(append([]string{}, barrierToolNames...), ceremonyToolNames...)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr, called, cleanup := newMultiToolMCPServer(t, tools, func(*testing.T, string, map[string]interface{}) map[string]interface{} {
				return map[string]interface{}{"ok": true}
			})
			defer cleanup()

			ctx := context.Background()
			if tc.bound {
				bound, release, err := qrdbsdk.WithSOPIN(ctx, []byte(pin))
				if err != nil {
					t.Fatalf("WithSOPIN: %v", err)
				}
				defer release()
				ctx = bound
			}

			err := tc.call(ctx, tr)
			var tlsErr *qrdbtransport.SOPINRequiresTLSError
			if !stderrors.As(err, &tlsErr) {
				t.Fatalf("expected SOPINRequiresTLSError, got %v", err)
			}
			if len(*called) != 0 {
				t.Fatalf("the PIN must not be sent over http://, server saw %v", *called)
			}
		})
	}
}
