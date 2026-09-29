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

package rest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	sshclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/test/sshfixture"
)

// TestIsSSHService pins the routing predicate, including the
// package-qualified form the SDK client actually sends.
func TestIsSSHService(t *testing.T) {
	for _, name := range []string{"SSH", sshclient.ServiceName} {
		if !isSSHService(name) {
			t.Errorf("isSSHService(%q) = false", name)
		}
	}
	for _, name := range []string{"Account", "quicbaas.Auth", "quicbaas.Certificate"} {
		if isSSHService(name) {
			t.Errorf("isSSHService(%q) = true", name)
		}
	}
}

// TestSSHHandlerRoutes drives every SSH method through the REST handler and
// asserts the HTTP method and path each builds. A handler that hits the
// wrong route is a silent 404 in production.
func TestSSHHandlerRoutes(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		// One envelope satisfying every shape the handlers decode.
		_, _ = w.Write([]byte(`{"name":"laptop","algorithm":"ed25519","line":"ssh-ed25519 AAAA",` +
			`"fingerprint":"` + sshfixture.UserFingerprint + `","format":"ssh-ed25519",` +
			`"items":[{"name":"laptop"}]}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	cases := []struct {
		name       string
		method     string
		req        interface{}
		wantMethod string
		wantPath   string
		wantQuery  string
	}{
		{
			name: "GenerateKey", method: sshclient.MethodGenerateKey,
			req:        &sshclient.GenerateRequest{Scope: "user", Owner: "alice", Name: "laptop"},
			wantMethod: http.MethodPost, wantPath: "/api/v1/ssh/keys",
		},
		{
			name: "ImportKey", method: sshclient.MethodImportKey,
			req:        &sshclient.ImportKeyRequest{Scope: "user", Owner: "alice", Name: "laptop"},
			wantMethod: http.MethodPost, wantPath: "/api/v1/ssh/keys/import",
		},
		{
			name: "GetKey", method: sshclient.MethodGetKey,
			req:        &sshclient.GetKeyRequest{Scope: "user", Owner: "alice", Name: "laptop"},
			wantMethod: http.MethodGet, wantPath: "/api/v1/ssh/keys/user/alice/laptop",
		},
		{
			name: "ListKeys", method: sshclient.MethodListKeys,
			req:        &sshclient.ListKeysRequest{Scope: "user", Owner: "alice"},
			wantMethod: http.MethodGet, wantPath: "/api/v1/ssh/keys", wantQuery: "owner=alice&scope=user",
		},
		{
			name: "DeleteKey", method: sshclient.MethodDeleteKey,
			req:        &sshclient.DeleteKeyRequest{Scope: "user", Owner: "alice", Name: "laptop"},
			wantMethod: http.MethodDelete, wantPath: "/api/v1/ssh/keys/user/alice/laptop",
		},
		{
			name: "ExportPublicKey", method: sshclient.MethodExportPublicKey,
			req:        &sshclient.ExportRequest{Scope: "user", Owner: "alice", Name: "laptop"},
			wantMethod: http.MethodGet, wantPath: "/api/v1/ssh/keys/user/alice/laptop/public",
		},
		{
			name: "SignDigest", method: sshclient.MethodSignDigest,
			req:        &sshclient.SignRequest{Scope: "user", Owner: "alice", Name: "laptop", Digest: []byte("d")},
			wantMethod: http.MethodPost, wantPath: "/api/v1/ssh/keys/user/alice/laptop/sign",
		},
		{
			name: "AddKnownHost", method: sshclient.MethodAddKnownHost,
			req: &sshclient.AddKnownHostRequest{Scope: "user", Owner: "alice", Host: "h",
				PublicKey: sshfixture.Host(t).AuthorizedLine},
			wantMethod: http.MethodPost, wantPath: "/api/v1/ssh/known-hosts",
		},
		{
			name: "ListKnownHosts", method: sshclient.MethodListKnownHosts,
			req:        &sshclient.ListKnownHostsRequest{Scope: "user", Owner: "alice"},
			wantMethod: http.MethodGet, wantPath: "/api/v1/ssh/known-hosts", wantQuery: "owner=alice&scope=user",
		},
		{
			name: "RemoveKnownHost", method: sshclient.MethodRemoveKnownHost,
			req:        &sshclient.RemoveKnownHostRequest{Scope: "user", Owner: "alice", Host: "git.example.com"},
			wantMethod: http.MethodDelete, wantPath: "/api/v1/ssh/known-hosts/git.example.com",
			wantQuery: "owner=alice&scope=user",
		},
		{
			name: "AddAuthorizedKey", method: sshclient.MethodAddAuthorizedKey,
			req:        &sshclient.AddAuthorizedKeyRequest{Scope: "user", Owner: "alice", Name: "deploy", Line: "l"},
			wantMethod: http.MethodPost, wantPath: "/api/v1/ssh/authorized/keys",
		},
		{
			name: "ListAuthorizedKeys", method: sshclient.MethodListAuthorizedKeys,
			req:        &sshclient.ListAuthorizedKeysRequest{Scope: "user", Owner: "alice"},
			wantMethod: http.MethodGet, wantPath: "/api/v1/ssh/authorized/keys", wantQuery: "owner=alice&scope=user",
		},
		{
			name: "RemoveAuthorizedKey", method: sshclient.MethodRemoveAuthorizedKey,
			req:        &sshclient.RemoveAuthorizedKeyRequest{Scope: "user", Owner: "alice", Name: "deploy"},
			wantMethod: http.MethodDelete, wantPath: "/api/v1/ssh/authorized/keys/user/alice/deploy",
		},
		{
			name: "EmitAuthorizedKey", method: sshclient.MethodEmitAuthorizedKey,
			req:        &sshclient.EmitRequest{PublicKey: "ssh-ed25519 AAAA"},
			wantMethod: http.MethodPost, wantPath: "/api/v1/ssh/authorized",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotMethod, gotPath, gotQuery = "", "", ""
			resp, err := tr.sshHandler.handleRequest(ctx, tc.method, tc.req)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.method, err)
			}
			if resp == nil {
				t.Fatalf("%s: nil response", tc.method)
			}
			if gotMethod != tc.wantMethod {
				t.Errorf("%s: HTTP method = %q, want %q", tc.method, gotMethod, tc.wantMethod)
			}
			if gotPath != tc.wantPath {
				t.Errorf("%s: path = %q, want %q", tc.method, gotPath, tc.wantPath)
			}
			if tc.wantQuery != "" && gotQuery != tc.wantQuery {
				t.Errorf("%s: query = %q, want %q", tc.method, gotQuery, tc.wantQuery)
			}
		})
	}
}

// TestSSHHandler_GetKeyDecodesFingerprint pins the fingerprint field name
// on the wire. The server answers with the fingerprint of a real ed25519
// key; a renamed or dropped JSON tag would leave it empty and every caller
// pinning a host would silently pin nothing.
func TestSSHHandler_GetKeyDecodesFingerprint(t *testing.T) {
	user := sshfixture.User(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"laptop","fingerprint":"` + user.Fingerprint + `"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	resp, err := tr.sshHandler.handleRequest(context.Background(),
		sshclient.MethodGetKey, &sshclient.GetKeyRequest{Scope: "user", Owner: "alice", Name: "laptop"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	key, ok := resp.(*sshclient.Key)
	if !ok {
		t.Fatalf("expected *ssh.Key, got %T", resp)
	}
	if key.Fingerprint != user.Fingerprint {
		t.Fatalf("fingerprint = %q, want %q", key.Fingerprint, user.Fingerprint)
	}
}

// TestSSHHandler_ListDecodesItemsEnvelope pins the list envelope shape. The
// server writes {"items":[...]}; a handler that expected a bare array would
// silently return an empty list.
func TestSSHHandler_ListDecodesItemsEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"name":"laptop"},{"name":"desktop"}]}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	resp, err := tr.sshHandler.handleRequest(context.Background(),
		sshclient.MethodListKeys, &sshclient.ListKeysRequest{Scope: "user", Owner: "alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list, ok := resp.(*sshclient.ListKeysResponse)
	if !ok {
		t.Fatalf("expected *ssh.ListKeysResponse, got %T", resp)
	}
	if len(list.Items) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(list.Items))
	}
}

// TestSSHHandler_ImportSendsPassphraseB64 pins the request body field name.
// A rename here silently drops the passphrase and every encrypted import
// fails with a confusing "invalid PEM".
func TestSSHHandler_ImportSendsPassphraseB64(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"laptop"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.sshHandler.handleRequest(context.Background(), sshclient.MethodImportKey,
		&sshclient.ImportKeyRequest{Name: "laptop", PEM: []byte("pem"), PassphraseB64: "aHVudGVyMg=="})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := body["passphrase_b64"]; got != "aHVudGVyMg==" {
		t.Fatalf("passphrase_b64 = %v, want aHVudGVyMg==", got)
	}
}

// =============================================================================
// Error paths
// =============================================================================

func TestSSHHandler_UnknownMethod(t *testing.T) {
	tr := newTestTransport(t, httptest.NewServer(http.NotFoundHandler()))
	_, err := tr.sshHandler.handleRequest(context.Background(), "Teleport", nil)
	if err == nil {
		t.Fatal("expected an error for an unknown ssh method")
	}
}

func TestSSHHandler_WrongRequestType(t *testing.T) {
	tr := newTestTransport(t, httptest.NewServer(http.NotFoundHandler()))
	ctx := context.Background()
	methods := []string{
		sshclient.MethodGenerateKey, sshclient.MethodImportKey, sshclient.MethodGetKey,
		sshclient.MethodListKeys, sshclient.MethodDeleteKey, sshclient.MethodExportPublicKey,
		sshclient.MethodSignDigest, sshclient.MethodAddKnownHost, sshclient.MethodListKnownHosts,
		sshclient.MethodRemoveKnownHost, sshclient.MethodAddAuthorizedKey,
		sshclient.MethodListAuthorizedKeys, sshclient.MethodRemoveAuthorizedKey,
		sshclient.MethodEmitAuthorizedKey,
	}
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			_, err := tr.sshHandler.handleRequest(ctx, m, &struct{ Nope string }{})
			if err == nil {
				t.Fatalf("%s: expected an invalid-request-type error", m)
			}
		})
	}
}

func TestSSHHandler_ServerErrorPropagates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"ssh: permission denied"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.sshHandler.handleRequest(context.Background(),
		sshclient.MethodListKeys, &sshclient.ListKeysRequest{})
	if err == nil {
		t.Fatal("expected the 403 to surface as an error")
	}
}
