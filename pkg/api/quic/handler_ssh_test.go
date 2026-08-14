//go:build quic

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
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/sshfake"
	sshclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
	sdkquic "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/quic"
)

// =============================================================================
// Harness
// =============================================================================

// mockServiceWithSSH exposes an SSH client to the QUIC handler.
type mockServiceWithSSH struct {
	mockService
	ssh xkmsssh.Client
}

func (m *mockServiceWithSSH) SSHClient() xkmsssh.Client { return m.ssh }

// createSSHHandler builds a QUIC handler wired to an in-memory SSH store.
// The parameter is the interface type, not *sshfake.Client, so passing nil
// yields a genuinely nil SSHClient() rather than a typed-nil interface.
func createSSHHandler(client xkmsssh.Client) *Handler {
	return NewHandler(&mockServiceWithSSH{ssh: client}, nil, nil, nil, createOrchestratorTestLogger())
}

// sshRequest builds an OpOrchestrator request carrying an SSH sub-op.
func sshRequest(t *testing.T, subOp byte, body interface{}) *Request {
	t.Helper()
	payload, err := EncodePayload(body)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	full := make([]byte, 1+len(payload))
	full[0] = subOp
	copy(full[1:], payload)
	return &Request{Opcode: OpOrchestrator, Payload: full}
}

// decodeSSHPayload unmarshals a successful response body.
func decodeSSHPayload(t *testing.T, resp *Response, out interface{}) {
	t.Helper()
	if resp.Status != StatusOK {
		t.Fatalf("unexpected status %d: %s", resp.Status, string(resp.Payload))
	}
	if err := json.Unmarshal(resp.Payload, out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// =============================================================================
// Client / server opcode parity
// =============================================================================

// TestSSHSubOpParity is the wire contract between the SDK client and this
// server. The two constant blocks live in different packages; a divergence
// silently routes an SSH call to the wrong handler (or into the account /
// project ranges that share the OpOrchestrator dispatch byte).
func TestSSHSubOpParity(t *testing.T) {
	pairs := []struct {
		name   string
		server byte
		client byte
	}{
		{"GenerateKey", SubOpSSHGenerateKey, sdkquic.SubOpSSHGenerateKey},
		{"ImportKey", SubOpSSHImportKey, sdkquic.SubOpSSHImportKey},
		{"GetKey", SubOpSSHGetKey, sdkquic.SubOpSSHGetKey},
		{"ListKeys", SubOpSSHListKeys, sdkquic.SubOpSSHListKeys},
		{"DeleteKey", SubOpSSHDeleteKey, sdkquic.SubOpSSHDeleteKey},
		{"ExportPublicKey", SubOpSSHExportPublicKey, sdkquic.SubOpSSHExportPublicKey},
		{"SignDigest", SubOpSSHSignDigest, sdkquic.SubOpSSHSignDigest},
		{"AddKnownHost", SubOpSSHAddKnownHost, sdkquic.SubOpSSHAddKnownHost},
		{"ListKnownHosts", SubOpSSHListKnownHosts, sdkquic.SubOpSSHListKnownHosts},
		{"RemoveKnownHost", SubOpSSHRemoveKnownHost, sdkquic.SubOpSSHRemoveKnownHost},
		{"AddAuthorizedKey", SubOpSSHAddAuthorizedKey, sdkquic.SubOpSSHAddAuthorizedKey},
		{"ListAuthorizedKeys", SubOpSSHListAuthorizedKeys, sdkquic.SubOpSSHListAuthorizedKeys},
		{"RemoveAuthorizedKey", SubOpSSHRemoveAuthorizedKey, sdkquic.SubOpSSHRemoveAuthorizedKey},
		{"EmitAuthorizedKey", SubOpSSHEmitAuthorizedKey, sdkquic.SubOpSSHEmitAuthorizedKey},
	}
	for _, p := range pairs {
		if p.server != p.client {
			t.Errorf("%s: server sub-op 0x%02X != client sub-op 0x%02X", p.name, p.server, p.client)
		}
	}

	// Every SDK method must resolve to a sub-op this server dispatches, and
	// every dispatched sub-op must be reachable from the SDK.
	methods := []string{
		sshclient.MethodGenerateKey, sshclient.MethodImportKey, sshclient.MethodGetKey,
		sshclient.MethodListKeys, sshclient.MethodDeleteKey, sshclient.MethodExportPublicKey,
		sshclient.MethodSignDigest, sshclient.MethodAddKnownHost, sshclient.MethodListKnownHosts,
		sshclient.MethodRemoveKnownHost, sshclient.MethodAddAuthorizedKey,
		sshclient.MethodListAuthorizedKeys, sshclient.MethodRemoveAuthorizedKey,
		sshclient.MethodEmitAuthorizedKey,
	}
	reachable := map[byte]bool{}
	for _, m := range methods {
		subOp, ok := sdkquic.SSHMethodToSubOp(m)
		if !ok {
			t.Errorf("SDK has no sub-op for method %s", m)
			continue
		}
		if !isSSHSubOp(subOp) {
			t.Errorf("method %s maps to 0x%02X, outside the server SSH range", m, subOp)
		}
		if _, dispatched := sshSubHandlers[subOp]; !dispatched {
			t.Errorf("method %s maps to 0x%02X, which the server does not dispatch", m, subOp)
		}
		reachable[subOp] = true
	}
	for subOp := range sshSubHandlers {
		if !reachable[subOp] {
			t.Errorf("server sub-op 0x%02X is unreachable from the SDK", subOp)
		}
	}
}

// TestSSHSubOpRangeIsDisjoint proves the SSH range does not overlap the
// account or project ranges it shares a dispatch byte with.
func TestSSHSubOpRangeIsDisjoint(t *testing.T) {
	for subOp := SubOpSSHGenerateKey; subOp <= SubOpSSHEmitAuthorizedKey; subOp++ {
		if isAccountSubOp(subOp) {
			t.Errorf("SSH sub-op 0x%02X collides with the account range", subOp)
		}
		if isProjectSubOp(subOp) {
			t.Errorf("SSH sub-op 0x%02X collides with the project range", subOp)
		}
	}
	if isSSHSubOp(SubOpAccountCreate) || isSSHSubOp(SubOpProjectCreate) {
		t.Error("isSSHSubOp admits an account or project sub-op")
	}
}

// =============================================================================
// Happy paths
// =============================================================================

func TestSSHDispatch_KeyLifecycle(t *testing.T) {
	fake := sshfake.NewClient()
	handler := createSSHHandler(fake)
	ctx := context.Background()

	resp := handler.HandleRequest(ctx, sshRequest(t, SubOpSSHGenerateKey, sshclient.GenerateRequest{
		Scope: "user", Owner: "alice", Name: "laptop", Algorithm: "ed25519",
	}))
	var key sshclient.Key
	decodeSSHPayload(t, resp, &key)
	if key.Name != "laptop" {
		t.Fatalf("generated key name = %q, want laptop", key.Name)
	}

	resp = handler.HandleRequest(ctx, sshRequest(t, SubOpSSHListKeys, sshclient.ListKeysRequest{
		Scope: "user", Owner: "alice",
	}))
	var list sshclient.ListKeysResponse
	decodeSSHPayload(t, resp, &list)
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 key, got %d", len(list.Items))
	}

	resp = handler.HandleRequest(ctx, sshRequest(t, SubOpSSHDeleteKey, sshclient.DeleteKeyRequest{
		Scope: "user", Owner: "alice", Name: "laptop",
	}))
	var ack sshclient.AckResponse
	decodeSSHPayload(t, resp, &ack)
	if !ack.Success {
		t.Fatal("delete did not acknowledge success")
	}
}

func TestSSHDispatch_ImportCarriesPassphrase(t *testing.T) {
	fake := sshfake.NewClient()
	handler := createSSHHandler(fake)

	resp := handler.HandleRequest(context.Background(), sshRequest(t, SubOpSSHImportKey,
		sshclient.ImportKeyRequest{
			Scope: "user", Owner: "alice", Name: "imported",
			PEM:           []byte("pem"),
			PassphraseB64: base64.StdEncoding.EncodeToString([]byte("s3cret")),
		}))
	var key sshclient.Key
	decodeSSHPayload(t, resp, &key)
	if fake.LastImportPassphrase != "s3cret" {
		t.Fatalf("passphrase = %q, want s3cret", fake.LastImportPassphrase)
	}
}

func TestSSHDispatch_SignDigest(t *testing.T) {
	fake := sshfake.NewClient()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sshPub, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("wrap key: %v", err)
	}
	fake.SignerPublicKey = sshPub
	handler := createSSHHandler(fake)

	resp := handler.HandleRequest(context.Background(), sshRequest(t, SubOpSSHSignDigest,
		sshclient.SignRequest{Scope: "user", Owner: "alice", Name: "laptop", Digest: []byte("digest")}))
	var sig sshclient.SignResponse
	decodeSSHPayload(t, resp, &sig)
	if sig.Format != "ssh-ed25519" {
		t.Fatalf("signature format = %q", sig.Format)
	}
}

func TestSSHDispatch_KnownHostsAndAuthorized(t *testing.T) {
	handler := createSSHHandler(sshfake.NewClient())
	ctx := context.Background()

	resp := handler.HandleRequest(ctx, sshRequest(t, SubOpSSHAddKnownHost, sshclient.AddKnownHostRequest{
		Scope: "user", Owner: "alice", Host: "git.example.com", Fingerprint: "SHA256:abc",
	}))
	var ack sshclient.AckResponse
	decodeSSHPayload(t, resp, &ack)

	resp = handler.HandleRequest(ctx, sshRequest(t, SubOpSSHListKnownHosts, sshclient.ListKnownHostsRequest{
		Scope: "user", Owner: "alice",
	}))
	var pins sshclient.ListKnownHostsResponse
	decodeSSHPayload(t, resp, &pins)
	if len(pins.Items) != 1 {
		t.Fatalf("expected 1 pin, got %d", len(pins.Items))
	}

	resp = handler.HandleRequest(ctx, sshRequest(t, SubOpSSHEmitAuthorizedKey, sshclient.EmitRequest{
		PublicKey: "ssh-ed25519 AAAA", Comment: "alice",
	}))
	var emit sshclient.EmitResponse
	decodeSSHPayload(t, resp, &emit)
	if emit.Line != "ssh-ed25519 AAAA alice" {
		t.Fatalf("emitted line = %q", emit.Line)
	}
}

// =============================================================================
// Error paths
// =============================================================================

func TestSSHDispatch_UnknownSubOp(t *testing.T) {
	handler := createSSHHandler(sshfake.NewClient())
	resp := handler.handleSSHDispatch(context.Background(), 0x5F, &Request{})
	if resp.Status == StatusOK {
		t.Fatal("expected an error for an unregistered ssh sub-op")
	}
}

func TestSSHDispatch_InvalidPayload(t *testing.T) {
	handler := createSSHHandler(sshfake.NewClient())
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpOrchestrator,
		Payload: append([]byte{SubOpSSHGetKey}, []byte("not-json")...),
	})
	if resp.Status == StatusOK {
		t.Fatal("expected an error for an undecodable ssh payload")
	}
}

func TestSSHDispatch_SSHNotConfigured(t *testing.T) {
	// A nil SSH client makes every sub-op fail closed rather than panic.
	handler := createSSHHandler(nil)
	for subOp := SubOpSSHGenerateKey; subOp <= SubOpSSHEmitAuthorizedKey; subOp++ {
		resp := handler.HandleRequest(context.Background(),
			sshRequest(t, subOp, map[string]string{"scope": "user", "owner": "alice", "name": "k"}))
		if resp.Status == StatusOK {
			t.Errorf("sub-op 0x%02X succeeded with no ssh client wired", subOp)
		}
	}
}

func TestSSHDispatch_BackendErrorSurfaces(t *testing.T) {
	fake := sshfake.NewClient()
	fake.Err = &xkmsssh.NotFoundError{Resource: "key", Message: "absent"}
	handler := createSSHHandler(fake)

	resp := handler.HandleRequest(context.Background(), sshRequest(t, SubOpSSHGetKey,
		sshclient.GetKeyRequest{Scope: "user", Owner: "alice", Name: "absent"}))
	if resp.Status == StatusOK {
		t.Fatal("expected the backend not-found error to surface")
	}
}

// =============================================================================
// Authorization
// =============================================================================

// TestSSHSubOpsAuthorizeAsKeys is the authz guard: the SSH sub-ops share the
// OpOrchestrator dispatch byte, so without sub-op-aware resolution they would
// authorize under the materially weaker orchestrator:status the rest of that
// opcode uses. The (resource, action) pairs must match the REST PEP
// (pkg/api/rest/authz_pep_mapping.go sshOps) and gRPC maps exactly.
func TestSSHSubOpsAuthorizeAsKeys(t *testing.T) {
	cases := []struct {
		subOp  byte
		action string
	}{
		{SubOpSSHGenerateKey, "create"},
		{SubOpSSHImportKey, "import"},
		{SubOpSSHGetKey, "get"},
		{SubOpSSHListKeys, "list"},
		{SubOpSSHDeleteKey, "delete"},
		{SubOpSSHExportPublicKey, "get"},
		{SubOpSSHSignDigest, "sign"},
		{SubOpSSHAddKnownHost, "create"},
		{SubOpSSHListKnownHosts, "list"},
		{SubOpSSHRemoveKnownHost, "delete"},
		{SubOpSSHAddAuthorizedKey, "create"},
		{SubOpSSHListAuthorizedKeys, "list"},
		{SubOpSSHRemoveAuthorizedKey, "delete"},
		{SubOpSSHEmitAuthorizedKey, "get"},
	}
	for _, tc := range cases {
		resource, action, ok := getQRDBPermission(OpOrchestrator, []byte{tc.subOp})
		if !ok {
			t.Errorf("sub-op 0x%02X is unmapped (default-denied)", tc.subOp)
			continue
		}
		if resource != "keys" || action != tc.action {
			t.Errorf("sub-op 0x%02X -> %s:%s, want keys:%s", tc.subOp, resource, action, tc.action)
		}
	}
}

// TestSSHSubOpsAreDefaultDeniedWhenUnmapped proves an SSH-range byte with no
// permission entry is denied rather than falling through to the weaker
// surface-level orchestrator mapping.
func TestSSHSubOpsAreDefaultDeniedWhenUnmapped(t *testing.T) {
	const unmapped = byte(0x5F) // inside the range guard below, deliberately unmapped
	if isSSHSubOp(unmapped) {
		if _, _, ok := getQRDBPermission(OpOrchestrator, []byte{unmapped}); ok {
			t.Fatal("an unmapped SSH sub-op resolved to a permission")
		}
	}
	// Every mapped sub-op must be inside the range the resolver guards.
	for subOp := range sshSubOpPermissions {
		if !isSSHSubOp(subOp) {
			t.Errorf("sub-op 0x%02X is mapped but outside isSSHSubOp's range", subOp)
		}
	}
}
