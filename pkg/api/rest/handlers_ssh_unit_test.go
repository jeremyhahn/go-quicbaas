//go:build rest

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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/test/sshfixture"

	"github.com/stretchr/testify/require"
)

// TestSSHRemoveKnownHost removes a previously pinned host and asserts the
// 204 No Content response, then confirms the pin is actually gone.
func TestSSHRemoveKnownHost(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	addBody := `{"scope":"user","owner":"alice","host":"bastion.example.com","public_key":"` + sshfixture.Host(t).AuthorizedLine + `"}`
	addReq := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/known-hosts", strings.NewReader(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	server.HandleAddKnownHost(httptest.NewRecorder(), addReq)

	delReq := httptest.NewRequest(http.MethodDelete,
		"/api/v1/ssh/known-hosts/bastion.example.com?scope=user&owner=alice", nil)
	delReq = withChiURLParams(delReq, map[string]string{"host": "bastion.example.com"})
	w := httptest.NewRecorder()
	server.HandleRemoveKnownHost(w, delReq)
	require.Equal(t, http.StatusNoContent, w.Code, "body=%s", w.Body.String())

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/ssh/known-hosts?scope=user&owner=alice", nil)
	lw := httptest.NewRecorder()
	server.HandleListKnownHosts(lw, listReq)
	require.Equal(t, http.StatusOK, lw.Code)
	var resp sshKnownHostsListResponse
	require.NoError(t, json.NewDecoder(lw.Body).Decode(&resp))
	require.Empty(t, resp.Items, "pin should be removed")
}

// TestSSHRemoveKnownHost_ServiceUnavailable guards the 503 path when the
// SSH client is not configured.
func TestSSHRemoveKnownHost_ServiceUnavailable(t *testing.T) {
	t.Parallel()
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/ssh/known-hosts/h", nil)
	req = withChiURLParams(req, map[string]string{"host": "h"})
	w := httptest.NewRecorder()
	server.HandleRemoveKnownHost(w, req)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// TestSSHVerifyAuthorized_Match adds an authorized entry through the SDK
// client, then verifies a matching fingerprint returns 200 with the entry.
func TestSSHVerifyAuthorized_Match(t *testing.T) {
	t.Parallel()
	server, client, cleanup := newSSHTestServer(t)
	defer cleanup()

	// Generate a real key so its public line parses as valid OpenSSH and
	// produces a deterministic SHA256 fingerprint.
	gen := httptest.NewRecorder()
	genReq := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/keys",
		strings.NewReader(`{"scope":"user","owner":"alice","name":"auth","algorithm":"ed25519"}`))
	genReq.Header.Set("Content-Type", "application/json")
	server.HandleGenerateSSHKey(gen, genReq)
	require.Equal(t, http.StatusCreated, gen.Code)
	var key xkmsssh.Key
	require.NoError(t, json.NewDecoder(gen.Body).Decode(&key))

	pub, _, _, _, err := gossh.ParseAuthorizedKey([]byte(key.PublicKey))
	require.NoError(t, err)
	fingerprint := gossh.FingerprintSHA256(pub)

	require.NoError(t, client.AuthorizedKeys().Add(context.Background(), xkmsssh.AddAuthorizedKeyRequest{
		Scope: "user",
		Owner: "alice",
		Name:  "auth",
		Line:  key.PublicKey,
	}))

	verifyBody := `{"scope":"user","owner":"alice","fingerprint":"` + fingerprint + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/authorized/verify", strings.NewReader(verifyBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.HandleVerifyAuthorized(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
}

// TestSSHVerifyAuthorized_NotFound returns a not-found error when no stored
// entry matches the supplied fingerprint.
func TestSSHVerifyAuthorized_NotFound(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	verifyBody := `{"scope":"user","owner":"alice","fingerprint":"` + sshTestFingerprint("does-not-exist") + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/authorized/verify", strings.NewReader(verifyBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.HandleVerifyAuthorized(w, req)

	require.GreaterOrEqual(t, w.Code, 400, "expected an error status, body=%s", w.Body.String())
}

// TestSSHVerifyAuthorized_ServiceUnavailable guards the 503 path.
func TestSSHVerifyAuthorized_ServiceUnavailable(t *testing.T) {
	t.Parallel()
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/authorized/verify", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	server.HandleVerifyAuthorized(w, req)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// TestSSHVerifyAuthorized_GoldenFingerprint is the load-bearing check for
// the whole migration away from placeholder fingerprints.
//
// The stored authorized_keys line and the fingerprint in the request come
// from two independent places: the line is marshalled from the fixture's
// ed25519 public key, while the fingerprint is the checked-in golden
// constant. HandleVerifyAuthorized matches them only by re-deriving the
// fingerprint from the parsed key with gossh.FingerprintSHA256. Change that
// derivation and this test goes red — which a placeholder such as
// "SHA256:abc" never could, because nothing computed it.
func TestSSHVerifyAuthorized_GoldenFingerprint(t *testing.T) {
	t.Parallel()
	server, client, cleanup := newSSHTestServer(t)
	defer cleanup()

	user := sshfixture.User(t)
	require.NoError(t, client.AuthorizedKeys().Add(context.Background(), xkmsssh.AddAuthorizedKeyRequest{
		Scope: "user", Owner: "alice", Name: "golden", Line: user.AuthorizedLine,
	}))

	verify := func(fingerprint string) *httptest.ResponseRecorder {
		body := `{"scope":"user","owner":"alice","fingerprint":"` + fingerprint + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/authorized/verify", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.HandleVerifyAuthorized(w, req)
		return w
	}

	w := verify(sshfixture.UserFingerprint)
	require.Equal(t, http.StatusOK, w.Code,
		"the golden fingerprint must match the stored key's own, body=%s", w.Body.String())

	var entry xkmsssh.AuthorizedEntry
	require.NoError(t, json.NewDecoder(w.Body).Decode(&entry))
	require.Equal(t, "golden", entry.Name)

	// A different key's fingerprint must not match the stored entry.
	require.GreaterOrEqual(t, verify(sshfixture.HostFingerprint).Code, 400,
		"an unrelated key's fingerprint must not authorize")
}
