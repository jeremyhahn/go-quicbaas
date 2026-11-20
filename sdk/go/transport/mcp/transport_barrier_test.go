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

// transport_barrier_test.go pins the MCP barrier verbs to the wire contract
// of the go-qrdb MCP client they replace: the SO and user PINs reach the
// server as the tools' so_pin and user_pin arguments, and an officer
// registration decodes the server's plain-text cert_pem.

package mcp

import (
	"context"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

const (
	testSOPIN   = "so-pin-1234"
	testUserPIN = "user-pin-5678"
)

var barrierPINToolNames = []string{
	"qrdb_barrier_initialize",
	"qrdb_barrier_shamir_initialize",
	"qrdb_barrier_rekey",
	"qrdb_barrier_recovery_generate",
	"qrdb_barrier_officer_register",
	"qrdb_barrier_recovery_officer_register",
}

// recordArgs returns a tool handler that keeps the arguments each tool
// received, answering with reply's payload for that tool.
func recordArgs(got map[string]map[string]interface{}, reply map[string]interface{}) toolResultFn {
	return func(_ *testing.T, tool string, args map[string]interface{}) map[string]interface{} {
		got[tool] = args
		if reply != nil {
			return reply
		}
		return map[string]interface{}{"ok": true}
	}
}

// assertStringArg fails unless args carries key as the JSON string want.
func assertStringArg(t *testing.T, tool string, args map[string]interface{}, key, want string) {
	t.Helper()
	if args == nil {
		t.Fatalf("%s: the tool was not called", tool)
	}
	v, ok := args[key]
	if !ok {
		t.Fatalf("%s: no %s argument reached the server (args %v)", tool, key, keysOf(args))
	}
	if s, isString := v.(string); !isString || s != want {
		t.Fatalf("%s: %s = %#v, want %q", tool, key, v, want)
	}
}

func keysOf(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func boundSOPIN(t *testing.T) context.Context {
	t.Helper()
	ctx, release, err := qrdbsdk.WithSOPIN(context.Background(), []byte(testSOPIN))
	if err != nil {
		t.Fatalf("WithSOPIN: %v", err)
	}
	t.Cleanup(release)
	return ctx
}

func TestBarrierInitialize_ForwardsPINs(t *testing.T) {
	got := map[string]map[string]interface{}{}
	tr, _ := newMultiToolMCPServerTLS(t, barrierPINToolNames, recordArgs(got, nil))

	if err := tr.BarrierInitialize(context.Background(), &qrdbtransport.BarrierInitializeRequest{
		Secret: []byte("pw"), SOPin: testSOPIN, UserPin: testUserPIN,
	}); err != nil {
		t.Fatalf("BarrierInitialize: %v", err)
	}
	args := got["qrdb_barrier_initialize"]
	assertStringArg(t, "qrdb_barrier_initialize", args, "so_pin", testSOPIN)
	assertStringArg(t, "qrdb_barrier_initialize", args, "user_pin", testUserPIN)
}

func TestBarrierInitializeShamir_ForwardsPINs(t *testing.T) {
	const tool = "qrdb_barrier_shamir_initialize"
	reply := map[string]interface{}{"threshold": 2, "total_shares": 3, "share_count": 3}
	cases := []struct {
		name string
		ctx  func(t *testing.T) context.Context
		req  qrdbtransport.BarrierInitializeShamirRequest
	}{
		{
			name: "request PINs",
			ctx:  func(*testing.T) context.Context { return context.Background() },
			req:  qrdbtransport.BarrierInitializeShamirRequest{SOPin: testSOPIN, UserPin: testUserPIN},
		},
		{
			name: "context-bound SO PIN",
			ctx:  boundSOPIN,
			req:  qrdbtransport.BarrierInitializeShamirRequest{UserPin: testUserPIN},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := map[string]map[string]interface{}{}
			tr, _ := newMultiToolMCPServerTLS(t, barrierPINToolNames, recordArgs(got, reply))
			req := c.req
			req.Secret, req.Threshold, req.TotalShares = []byte("pw"), 2, 3
			if _, err := tr.BarrierInitializeShamir(c.ctx(t), &req); err != nil {
				t.Fatalf("BarrierInitializeShamir: %v", err)
			}
			assertStringArg(t, tool, got[tool], "so_pin", testSOPIN)
			assertStringArg(t, tool, got[tool], "user_pin", testUserPIN)
		})
	}
}

// soPINCases are the two ways go-qrdb resolves the SO PIN of a request:
// the request's own SOPin, or the PIN bound to the context by WithSOPIN.
func soPINCases() []struct {
	name     string
	ctx      func(t *testing.T) context.Context
	reqSOPIN string
} {
	return []struct {
		name     string
		ctx      func(t *testing.T) context.Context
		reqSOPIN string
	}{
		{name: "request SO PIN", ctx: func(*testing.T) context.Context { return context.Background() }, reqSOPIN: testSOPIN},
		{name: "context-bound SO PIN", ctx: boundSOPIN},
	}
}

func TestBarrierRekey_ForwardsSOPin(t *testing.T) {
	const tool = "qrdb_barrier_rekey"
	reply := map[string]interface{}{"threshold": 2, "total_shares": 2, "share_count": 2}
	for _, c := range soPINCases() {
		t.Run(c.name, func(t *testing.T) {
			got := map[string]map[string]interface{}{}
			tr, _ := newMultiToolMCPServerTLS(t, barrierPINToolNames, recordArgs(got, reply))
			if _, err := tr.BarrierRekey(c.ctx(t), &qrdbtransport.BarrierRekeyRequest{
				Threshold: 2, Total: 2, SOPin: c.reqSOPIN,
			}); err != nil {
				t.Fatalf("BarrierRekey: %v", err)
			}
			assertStringArg(t, tool, got[tool], "so_pin", testSOPIN)
		})
	}
}

func TestBarrierGenerateRecoveryKeys_ForwardsSOPin(t *testing.T) {
	const tool = "qrdb_barrier_recovery_generate"
	reply := map[string]interface{}{"threshold": 2, "total": 2, "share_count": 2}
	for _, c := range soPINCases() {
		t.Run(c.name, func(t *testing.T) {
			got := map[string]map[string]interface{}{}
			tr, _ := newMultiToolMCPServerTLS(t, barrierPINToolNames, recordArgs(got, reply))
			if _, err := tr.BarrierGenerateRecoveryKeys(c.ctx(t), &qrdbtransport.BarrierGenerateRecoveryKeysRequest{
				Threshold: 2, Total: 2, SOPin: c.reqSOPIN,
			}); err != nil {
				t.Fatalf("BarrierGenerateRecoveryKeys: %v", err)
			}
			assertStringArg(t, tool, got[tool], "so_pin", testSOPIN)
		})
	}
}

// testCertPEM is what the server returns in cert_pem: PEM text, not base64.
const testCertPEM = "-----BEGIN CERTIFICATE-----\nMIIBszCCAVmgAwIBAgIUQ2VydA==\n-----END CERTIFICATE-----\n"

func TestOfficerRegister_DecodesPEMText(t *testing.T) {
	got := map[string]map[string]interface{}{}
	tr, _ := newMultiToolMCPServerTLS(t, barrierPINToolNames,
		recordArgs(got, map[string]interface{}{"cert_pem": testCertPEM, "share_index": 2}))

	resp, err := tr.OfficerRegister(context.Background(), &qrdbtransport.OfficerRegisterRequest{
		Username: "alice", CSRPEM: []byte("CSR"),
	})
	if err != nil {
		t.Fatalf("OfficerRegister: %v", err)
	}
	if string(resp.CertPEM) != testCertPEM || resp.ShareIndex != 2 {
		t.Fatalf("OfficerRegister = {CertPEM: %q, ShareIndex: %d}, want {%q, 2}", resp.CertPEM, resp.ShareIndex, testCertPEM)
	}
}

func TestRecoveryOfficerRegister_DecodesPEMText(t *testing.T) {
	got := map[string]map[string]interface{}{}
	tr, _ := newMultiToolMCPServerTLS(t, barrierPINToolNames,
		recordArgs(got, map[string]interface{}{"cert_pem": testCertPEM, "share_index": 3}))

	resp, err := tr.RecoveryOfficerRegister(context.Background(), &qrdbtransport.OfficerRegisterRequest{
		Username: "bob", CSRPEM: []byte("CSR"),
	})
	if err != nil {
		t.Fatalf("RecoveryOfficerRegister: %v", err)
	}
	if string(resp.CertPEM) != testCertPEM || resp.ShareIndex != 3 {
		t.Fatalf("RecoveryOfficerRegister = {CertPEM: %q, ShareIndex: %d}, want {%q, 3}", resp.CertPEM, resp.ShareIndex, testCertPEM)
	}
}

// TestBarrierShareListsAreCommaSeparatedStrings pins the wire contract of the
// native go-qrdb v0.30.1-alpha MCP barrier handlers the composed node serves:
// qrdb_barrier_shamir_unseal_shares, qrdb_barrier_recovery_recover and
// qrdb_barrier_root_token declare shares/keys with mcp.WithString, read them
// with mcp.ParseString and split the value on ",". A JSON array arrives there
// as "shares is required"/"keys is required", so the lists go as one
// comma-separated string, as go-qrdb's own MCP client sends them.
func TestBarrierShareListsAreCommaSeparatedStrings(t *testing.T) {
	const (
		unsealTool  = "qrdb_barrier_shamir_unseal_shares"
		recoverTool = "qrdb_barrier_recovery_recover"
		rootTool    = "qrdb_barrier_root_token"
	)
	got := map[string]map[string]interface{}{}
	tr, _ := newMultiToolMCPServerTLS(t, []string{unsealTool, recoverTool, rootTool}, recordArgs(got, nil))
	ctx := context.Background()

	if err := tr.BarrierUnsealWithShares(ctx, &qrdbtransport.BarrierUnsealSharesRequest{
		Shares: []string{"s1", "s2", "s3"},
	}); err != nil {
		t.Fatalf("BarrierUnsealWithShares: %v", err)
	}
	if err := tr.BarrierRecoverWithKeys(ctx, &qrdbtransport.BarrierRecoverWithKeysRequest{
		Keys: []string{"k1", "k2"},
	}); err != nil {
		t.Fatalf("BarrierRecoverWithKeys: %v", err)
	}
	if _, err := tr.BarrierGenerateRootToken(ctx, &qrdbtransport.BarrierGenerateRootTokenRequest{
		Shares: []string{"s1", "s2"},
	}); err != nil {
		t.Fatalf("BarrierGenerateRootToken: %v", err)
	}

	assertStringArg(t, unsealTool, got[unsealTool], "shares", "s1,s2,s3")
	assertStringArg(t, recoverTool, got[recoverTool], "keys", "k1,k2")
	assertStringArg(t, rootTool, got[rootTool], "shares", "s1,s2")
}
