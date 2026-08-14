//go:build rest

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

// Package rest: coverage for the authorized_keys CRUD endpoints added so the
// REST surface matches the SDK SSH client (previously REST served only
// emit/verify, which left `quicbaas ssh authorized add|list|remove`
// unreachable over REST).
package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
)

// authorizedKeyRequest builds a request with the chi URL params the
// path-scoped handlers read.
func authorizedKeyRequest(method, target, scope, owner, name string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("scope", scope)
	rctx.URLParams.Add("owner", owner)
	rctx.URLParams.Add("name", name)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// =============================================================================
// Happy paths
// =============================================================================

func TestSSHAuthorizedKeyCRUD(t *testing.T) {
	t.Parallel()
	server, sshClient, cleanup := newSSHTestServer(t)
	defer cleanup()

	// The xkms store validates the authorized_keys line, so derive a real
	// one from a freshly generated key rather than a synthetic string.
	generated, err := sshClient.Keys().Generate(context.Background(), xkmsssh.GenerateRequest{
		Scope: "user", Owner: "alice", Name: "src", Algorithm: "ed25519",
	})
	require.NoError(t, err)
	line := generated.PublicKey

	// Add.
	body := `{"scope":"user","owner":"alice","name":"deploy","line":"` + line + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/authorized/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.HandleAddAuthorizedKey(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "body=%s", w.Body.String())

	// List.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ssh/authorized/keys?scope=user&owner=alice", nil)
	w = httptest.NewRecorder()
	server.HandleListAuthorizedKeys(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var list struct {
		Items []*xkmsssh.AuthorizedEntry `json:"items"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&list))
	require.Len(t, list.Items, 1)
	require.Equal(t, "deploy", list.Items[0].Name)

	// Get.
	w = httptest.NewRecorder()
	server.HandleGetAuthorizedKey(w,
		authorizedKeyRequest(http.MethodGet, "/api/v1/ssh/authorized/keys/user/alice/deploy", "user", "alice", "deploy"))
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var entry xkmsssh.AuthorizedEntry
	require.NoError(t, json.NewDecoder(w.Body).Decode(&entry))
	require.Equal(t, "deploy", entry.Name)

	// Remove.
	w = httptest.NewRecorder()
	server.HandleRemoveAuthorizedKey(w,
		authorizedKeyRequest(http.MethodDelete, "/api/v1/ssh/authorized/keys/user/alice/deploy", "user", "alice", "deploy"))
	require.Equal(t, http.StatusNoContent, w.Code, "body=%s", w.Body.String())

	// The entry is gone.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ssh/authorized/keys?scope=user&owner=alice", nil)
	w = httptest.NewRecorder()
	server.HandleListAuthorizedKeys(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	list.Items = nil
	require.NoError(t, json.NewDecoder(w.Body).Decode(&list))
	require.Empty(t, list.Items)
}

// =============================================================================
// Error paths
// =============================================================================

func TestSSHAuthorizedKeyCRUD_ServiceUnavailable(t *testing.T) {
	t.Parallel()
	server, _, cleanup := setupMockServer(t)
	defer cleanup()
	// SSHClientOverride left nil => not configured.

	w := httptest.NewRecorder()
	server.HandleAddAuthorizedKey(w, httptest.NewRequest(http.MethodPost, "/api/v1/ssh/authorized/keys", strings.NewReader(`{}`)))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)

	w = httptest.NewRecorder()
	server.HandleListAuthorizedKeys(w, httptest.NewRequest(http.MethodGet, "/api/v1/ssh/authorized/keys", nil))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)

	w = httptest.NewRecorder()
	server.HandleGetAuthorizedKey(w, authorizedKeyRequest(http.MethodGet, "/x", "user", "alice", "deploy"))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)

	w = httptest.NewRecorder()
	server.HandleRemoveAuthorizedKey(w, authorizedKeyRequest(http.MethodDelete, "/x", "user", "alice", "deploy"))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestSSHAuthorizedKeyAdd_BadBody(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/authorized/keys", strings.NewReader(`{not-json}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.HandleAddAuthorizedKey(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSSHAuthorizedKeyGet_NotFound(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleGetAuthorizedKey(w,
		authorizedKeyRequest(http.MethodGet, "/api/v1/ssh/authorized/keys/user/alice/absent", "user", "alice", "absent"))
	require.Equal(t, http.StatusNotFound, w.Code, "body=%s", w.Body.String())
}

func TestSSHAuthorizedKeyRemove_NotFound(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleRemoveAuthorizedKey(w,
		authorizedKeyRequest(http.MethodDelete, "/api/v1/ssh/authorized/keys/user/alice/absent", "user", "alice", "absent"))
	require.Equal(t, http.StatusNotFound, w.Code, "body=%s", w.Body.String())
}

// =============================================================================
// Route table
// =============================================================================

// TestSSHRoutesRegisteredOnBothMounts proves the canonical mount and the
// go-xkms client alias share one route table — the reason registerSSHRoutes
// exists. A route present on only one mount is a silent 404 for whichever
// client uses the other.
func TestSSHRoutesRegisteredOnBothMounts(t *testing.T) {
	t.Parallel()
	router := chi.NewRouter()
	server := &Server{}
	router.Route("/api/v1", func(r chi.Router) {
		r.Route("/ssh", server.registerSSHRoutes)
		r.Route("/xkms/v1/ssh", server.registerSSHRoutes)
	})

	routes := map[string]bool{}
	require.NoError(t, chi.Walk(router,
		func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			routes[method+" "+route] = true
			return nil
		}))

	for _, suffix := range []string{
		"POST /api/v1%s/keys",
		"POST /api/v1%s/keys/import",
		"GET /api/v1%s/keys",
		"GET /api/v1%s/keys/{scope}/{owner}/{name}",
		"DELETE /api/v1%s/keys/{scope}/{owner}/{name}",
		"GET /api/v1%s/keys/{scope}/{owner}/{name}/public",
		"POST /api/v1%s/keys/{scope}/{owner}/{name}/sign",
		"POST /api/v1%s/known-hosts",
		"GET /api/v1%s/known-hosts",
		"DELETE /api/v1%s/known-hosts/{host}",
		"POST /api/v1%s/known-hosts/{host}/verify",
		"POST /api/v1%s/authorized",
		"POST /api/v1%s/authorized/verify",
		"POST /api/v1%s/authorized/keys",
		"GET /api/v1%s/authorized/keys",
		"GET /api/v1%s/authorized/keys/{scope}/{owner}/{name}",
		"DELETE /api/v1%s/authorized/keys/{scope}/{owner}/{name}",
	} {
		for _, mount := range []string{"/ssh", "/xkms/v1/ssh"} {
			want := strings.Replace(suffix, "%s", mount, 1)
			require.True(t, routes[want], "missing route %q", want)
		}
	}
}
