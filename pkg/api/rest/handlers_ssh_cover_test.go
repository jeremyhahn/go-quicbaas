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
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func sshGenerateKey(t *testing.T, server *Server, scope, owner, name string) {
	t.Helper()
	body := `{"scope":"` + scope + `","owner":"` + owner + `","name":"` + name + `","algorithm":"ed25519"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.HandleGenerateSSHKey(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "gen body=%s", w.Body.String())
}

// HandleSignSSHDigest signs a 32-byte digest with a generated key.
func TestSSHSignDigest_Success(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	sshGenerateKey(t, server, "user", "alice", "laptop")

	digest := base64.StdEncoding.EncodeToString(make([]byte, 32))
	body := `{"digest":"` + digest + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/keys/user/alice/laptop/sign",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withChiURLParams(req, map[string]string{"scope": "user", "owner": "alice", "name": "laptop"})
	w := httptest.NewRecorder()
	server.HandleSignSSHDigest(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var resp sshSignResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotEmpty(t, resp.Format)
}

// HandleSignSSHDigest returns the key not-found error when the key is absent.
func TestSSHSignDigest_KeyNotFound(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	digest := base64.StdEncoding.EncodeToString(make([]byte, 32))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/keys/user/alice/missing/sign",
		strings.NewReader(`{"digest":"`+digest+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req = withChiURLParams(req, map[string]string{"scope": "user", "owner": "alice", "name": "missing"})
	w := httptest.NewRecorder()
	server.HandleSignSSHDigest(w, req)

	require.NotEqual(t, http.StatusOK, w.Code)
}

// HandleExportSSHKeyPublic returns the OpenSSH public key for a generated key.
func TestSSHExportPublic_Success(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	sshGenerateKey(t, server, "user", "bob", "work")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ssh/keys/user/bob/work/public", nil)
	req = withChiURLParams(req, map[string]string{"scope": "user", "owner": "bob", "name": "work"})
	w := httptest.NewRecorder()
	server.HandleExportSSHKeyPublic(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
}
