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
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	xkmsstorage "github.com/jeremyhahn/go-xkms/pkg/storage"
	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/rest/secureio"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/test/sshfixture"
)

// newSSHTestServer wires a mock QuicBaaS with a real embedded SSH
// client backed by an in-memory storage backend. This gives handler
// tests an end-to-end round-trip through xkms's actual business logic
// without writing to disk or speaking REST to a second process.
// sshTestFingerprint derives a well-formed OpenSSH SHA256 host-key
// fingerprint from seed. The go-xkms known-hosts store (v0.9.x) validates
// the fingerprint it is handed — "SHA256:" plus an unpadded base64 body that
// decodes to exactly 32 bytes — so a placeholder such as "SHA256:abc" is
// rejected at the door and never reaches the pin comparison.
func sshTestFingerprint(seed string) string {
	digest := sha256.Sum256([]byte(seed))
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:])
}

func newSSHTestServer(t *testing.T) (*Server, xkmsssh.Client, func()) {
	t.Helper()

	backend := xkmsstorage.NewMemory()
	sshClient, err := xkmsssh.NewEmbedded(xkmsssh.EmbeddedOptions{Backend: backend})
	require.NoError(t, err)

	server, mockSvc, cleanup := setupMockServer(t)
	mockSvc.SSHClientOverride = sshClient

	wrappedCleanup := func() {
		_ = sshClient.Close()
		cleanup()
	}
	return server, sshClient, wrappedCleanup
}

// TestSSHGenerateKey exercises HandleGenerateSSHKey end-to-end against
// the real embedded SDK client. A successful generate returns 201 and
// the key DTO.
func TestSSHGenerateKey(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	body := `{"scope":"user","owner":"alice","name":"laptop","algorithm":"ed25519"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleGenerateSSHKey(w, req)

	require.Equal(t, http.StatusCreated, w.Code, "body=%s", w.Body.String())
	var key xkmsssh.Key
	require.NoError(t, json.NewDecoder(w.Body).Decode(&key))
	require.Equal(t, "laptop", key.Name)
	require.NotEmpty(t, key.Fingerprint)
}

// TestSSHGenerateKey_ServiceUnavailable guards the 503 path when
// SSHClient() returns nil.
func TestSSHGenerateKey_ServiceUnavailable(t *testing.T) {
	t.Parallel()
	server, _, cleanup := setupMockServer(t)
	defer cleanup()
	// Leave SSHClientOverride nil to trigger the not-configured path.

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/keys", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	server.HandleGenerateSSHKey(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// TestSSHGenerateKey_BadBody exercises the 400 path for malformed JSON.
func TestSSHGenerateKey_BadBody(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/keys", strings.NewReader(`{not-json}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.HandleGenerateSSHKey(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestSSHListKeys returns 200 with an items array after a prior
// generate.
func TestSSHListKeys(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	// Seed a key.
	genReq := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/keys",
		strings.NewReader(`{"scope":"user","owner":"alice","name":"laptop","algorithm":"ed25519"}`))
	genReq.Header.Set("Content-Type", "application/json")
	server.HandleGenerateSSHKey(httptest.NewRecorder(), genReq)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ssh/keys?scope=user&owner=alice", nil)
	w := httptest.NewRecorder()
	server.HandleListSSHKeys(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp sshKeyListResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Items, 1)
}

// TestSSHGetKey_NotFound triggers the 404 path.
func TestSSHGetKey_NotFound(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ssh/keys/user/alice/missing", nil)
	req = withChiURLParams(req, map[string]string{
		"scope": "user", "owner": "alice", "name": "missing",
	})
	w := httptest.NewRecorder()
	server.HandleGetSSHKey(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

// TestSSHDeleteKey exercises the 204 success path and the 404 path.
func TestSSHDeleteKey(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	// Seed.
	genReq := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/keys",
		strings.NewReader(`{"scope":"user","owner":"alice","name":"laptop","algorithm":"ed25519"}`))
	genReq.Header.Set("Content-Type", "application/json")
	server.HandleGenerateSSHKey(httptest.NewRecorder(), genReq)

	del := httptest.NewRequest(http.MethodDelete, "/api/v1/ssh/keys/user/alice/laptop", nil)
	del = withChiURLParams(del, map[string]string{
		"scope": "user", "owner": "alice", "name": "laptop",
	})
	w := httptest.NewRecorder()
	server.HandleDeleteSSHKey(w, del)
	require.Equal(t, http.StatusNoContent, w.Code)

	// Delete again → 404.
	w = httptest.NewRecorder()
	server.HandleDeleteSSHKey(w, del)
	require.Equal(t, http.StatusNotFound, w.Code)
}

// TestSSHExportPublic returns 200 with the authorized_keys-form line.
func TestSSHExportPublic(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	genReq := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/keys",
		strings.NewReader(`{"scope":"user","owner":"alice","name":"laptop","algorithm":"ed25519"}`))
	genReq.Header.Set("Content-Type", "application/json")
	server.HandleGenerateSSHKey(httptest.NewRecorder(), genReq)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ssh/keys/user/alice/laptop/public", nil)
	req = withChiURLParams(req, map[string]string{
		"scope": "user", "owner": "alice", "name": "laptop",
	})
	w := httptest.NewRecorder()
	server.HandleExportSSHKeyPublic(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var out xkmsssh.PublicKey
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.NotEmpty(t, out.Line)
	require.NotEmpty(t, out.Fingerprint)
}

// TestSSHSignDigest_MissingDigest returns 400 when digest is empty.
func TestSSHSignDigest_MissingDigest(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	genReq := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/keys",
		strings.NewReader(`{"scope":"user","owner":"alice","name":"laptop","algorithm":"ed25519"}`))
	genReq.Header.Set("Content-Type", "application/json")
	server.HandleGenerateSSHKey(httptest.NewRecorder(), genReq)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/keys/user/alice/laptop/sign",
		strings.NewReader(`{"digest":""}`))
	req.Header.Set("Content-Type", "application/json")
	req = withChiURLParams(req, map[string]string{
		"scope": "user", "owner": "alice", "name": "laptop",
	})
	w := httptest.NewRecorder()
	server.HandleSignSSHDigest(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

// Known-host fingerprint fixtures. Each is the OpenSSH SHA256 fingerprint
// of a real deterministic ed25519 key from sshfixture, so a test asserting
// "this host presents this fingerprint" is asserting something a production
// verify path could disagree with. (go-xkms known_hosts.ValidateFingerprint
// also rejects anything whose base64 body is not a 32-byte SHA-256 digest,
// so a bare placeholder such as "SHA256:abc" never reaches the handler.)
const (
	testKnownHostFingerprint          = sshfixture.HostFingerprint
	testKnownHostFingerprintExpected  = sshfixture.HostFingerprint
	testKnownHostFingerprintDifferent = sshfixture.HostAltFingerprint
)

// TestSSHAddAndListKnownHosts exercises the known-hosts add + list
// round-trip.
func TestSSHAddAndListKnownHosts(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	addBody := `{"scope":"user","owner":"alice","host":"bastion.example.com","public_key":"` + sshfixture.Host(t).AuthorizedLine + `"}`
	addReq := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/known-hosts", strings.NewReader(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.HandleAddKnownHost(w, addReq)
	require.Equal(t, http.StatusCreated, w.Code, "body=%s", w.Body.String())

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/ssh/known-hosts?scope=user&owner=alice", nil)
	w = httptest.NewRecorder()
	server.HandleListKnownHosts(w, listReq)
	require.Equal(t, http.StatusOK, w.Code)
	var resp sshKnownHostsListResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Items, 1)
	require.Equal(t, "bastion.example.com", resp.Items[0].Host)
}

// TestSSHAddKnownHost_RequiresThePublicKey pins that a fingerprint-only pin is
// refused: go-xkms trusts only the full host key.
func TestSSHAddKnownHost_RequiresThePublicKey(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	addBody := `{"scope":"user","owner":"alice","host":"bastion.example.com","fingerprint":"` + testKnownHostFingerprint + `"}`
	addReq := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/known-hosts", strings.NewReader(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.HandleAddKnownHost(w, addReq)
	require.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
}

// TestSSHVerifyKnownHost_Match returns 204 when fingerprints match.
func TestSSHVerifyKnownHost_Match(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	addBody := `{"scope":"user","owner":"alice","host":"bastion.example.com","public_key":"` + sshfixture.Host(t).AuthorizedLine + `"}`
	addReq := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/known-hosts", strings.NewReader(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	server.HandleAddKnownHost(httptest.NewRecorder(), addReq)

	verifyBody := `{"scope":"user","owner":"alice","fingerprint":"` + testKnownHostFingerprint + `"}`
	verifyReq := httptest.NewRequest(http.MethodPost,
		"/api/v1/ssh/known-hosts/bastion.example.com/verify",
		strings.NewReader(verifyBody))
	verifyReq.Header.Set("Content-Type", "application/json")
	verifyReq = withChiURLParams(verifyReq, map[string]string{"host": "bastion.example.com"})
	w := httptest.NewRecorder()
	server.HandleVerifyKnownHost(w, verifyReq)

	require.Equal(t, http.StatusNoContent, w.Code)
}

// TestSSHVerifyKnownHost_Mismatch returns 409 with the extra fields.
func TestSSHVerifyKnownHost_Mismatch(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	addBody := `{"scope":"user","owner":"alice","host":"bastion.example.com","public_key":"` + sshfixture.Host(t).AuthorizedLine + `"}`
	addReq := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/known-hosts", strings.NewReader(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	server.HandleAddKnownHost(httptest.NewRecorder(), addReq)

	verifyBody := `{"scope":"user","owner":"alice","fingerprint":"` + testKnownHostFingerprintDifferent + `"}`
	verifyReq := httptest.NewRequest(http.MethodPost,
		"/api/v1/ssh/known-hosts/bastion.example.com/verify",
		strings.NewReader(verifyBody))
	verifyReq.Header.Set("Content-Type", "application/json")
	verifyReq = withChiURLParams(verifyReq, map[string]string{"host": "bastion.example.com"})
	w := httptest.NewRecorder()
	server.HandleVerifyKnownHost(w, verifyReq)

	require.Equal(t, http.StatusConflict, w.Code)
	var resp sshMismatchResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, "bastion.example.com", resp.Host)
	require.Equal(t, testKnownHostFingerprintExpected, resp.Expected)
	require.Equal(t, testKnownHostFingerprintDifferent, resp.Received)
}

// TestSSHEmitAuthorized round-trips a public key through the emit
// endpoint and checks the resulting authorized_keys line. Uses a real
// generated key so the line parses as valid OpenSSH format.
func TestSSHEmitAuthorized(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	// Generate a real key and capture its public line.
	gen := httptest.NewRecorder()
	genReq := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/keys",
		strings.NewReader(`{"scope":"user","owner":"alice","name":"emit","algorithm":"ed25519"}`))
	genReq.Header.Set("Content-Type", "application/json")
	server.HandleGenerateSSHKey(gen, genReq)
	require.Equal(t, http.StatusCreated, gen.Code)
	var key xkmsssh.Key
	require.NoError(t, json.NewDecoder(gen.Body).Decode(&key))

	body := `{"public_key":"` + key.PublicKey + `","comment":"alice@laptop"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/authorized", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.HandleEmitAuthorized(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var resp sshEmitResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Contains(t, resp.Line, "ssh-ed25519")
}

// TestSSHImportKey_PassphraseFieldUnmarshaler is the canary guard for
// the secureio.PasswordField contract. A future refactor that adds a
// plaintext string-typed field to the import DTO or the passphrase
// field type will fail this test loudly.
func TestSSHImportKey_PassphraseFieldUnmarshaler(t *testing.T) {
	t.Parallel()

	// Reflect on the field: the tagged JSON name must match what the
	// handler expects, and the Go field type must be the secure wrapper.
	typ := reflect.TypeOf(sshImportKeyRequest{})
	f, ok := typ.FieldByName("Passphrase")
	require.True(t, ok, "Passphrase field missing")
	require.Equal(t, "secureio.PasswordField", f.Type.String(),
		"passphrase must be secureio.PasswordField — plaintext string fields are forbidden")
	require.Equal(t, "passphrase_b64,omitempty", f.Tag.Get("json"))

	// Reflect on the field type itself: it must not contain a string.
	pfTyp := reflect.TypeOf(secureio.PasswordField{})
	for i := 0; i < pfTyp.NumField(); i++ {
		require.NotEqual(t, reflect.String, pfTyp.Field(i).Type.Kind(),
			"PasswordField.%s is string — plaintext must stay in mlocked buffers only",
			pfTyp.Field(i).Name)
	}
}

// TestSSHImportKey_AcceptsBase64Passphrase exercises the decode
// contract: a base64-encoded passphrase must land in the mlocked
// buffer without exploding. The actual PEM handling (empty here)
// returns a typed SDK error, but the exit status must be a 4xx
// — a 5xx would indicate the passphrase decode path itself broke.
func TestSSHImportKey_AcceptsBase64Passphrase(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	// Use a small dummy PEM payload; we only care that the handler
	// gets past passphrase decoding and into the SDK. The SDK will
	// reject the malformed PEM with its own typed error.
	pem := []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nnot-real-data\n-----END OPENSSH PRIVATE KEY-----\n")
	pemB64, _ := json.Marshal(pem)
	pw := base64.StdEncoding.EncodeToString([]byte("correct horse battery staple"))
	body := `{"scope":"user","owner":"alice","name":"laptop","pem":` + string(pemB64) + `,"passphrase_b64":"` + pw + `"}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/keys/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.HandleImportSSHKey(w, req)

	require.Less(t, w.Code, 500,
		"handler returned 5xx — passphrase decode path may be broken; body=%s", w.Body.String())
}

// =============================================================================
// Test helpers
// =============================================================================

// withChiURLParams installs chi-style URL params in the request context
// so handlers that use chi.URLParam see the expected values. Using this
// helper means tests don't need to mount a full chi.Router per case.
func withChiURLParams(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}
