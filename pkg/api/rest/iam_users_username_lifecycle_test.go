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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// This file covers the REST dialect, which the QUIC transport also speaks: the
// SDK's QUIC transport is the REST transport over an HTTP/3 RoundTripper, with
// the same endpoints, the same JSON, and the same decoding. A body this handler
// honours is honoured identically there.

// postUser drives the create endpoint with a raw JSON body.
func postUser(t *testing.T, handler *IAMHandler, tenantID, body string) *httptest.ResponseRecorder {
	t.Helper()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", tenantID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/"+tenantID+"/users/", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.CreateUser(w, req)
	return w
}

// putUser drives the update endpoint with a raw JSON body.
func putUser(t *testing.T, handler *IAMHandler, tenantID, userID, body string) *httptest.ResponseRecorder {
	t.Helper()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", tenantID)
	rctx.URLParams.Add("user_id", userID)
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/tenants/"+tenantID+"/users/"+userID, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.UpdateUser(w, req)
	return w
}

// TestREST_CreateUserKeepsUsernameAndDisplayNameApart proves the requested
// username survives this transport and remains the name the account resolves by
// — while the display name, which differs, does not become one.
func TestREST_CreateUserKeepsUsernameAndDisplayNameApart(t *testing.T) {
	handler, mem, cleanup := setupUserReadHandler(t)
	defer cleanup()

	w := postUser(t, handler, "acme",
		`{"email":"carol@acme.com","username":"carol","name":"Carol Danvers","lifecycle":"active"}`)
	require.Equal(t, http.StatusCreated, w.Code, "CreateUser failed: %s", w.Body.String())

	var created map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	assert.Equal(t, "carol", created["username"], "the response dropped the username: %s", w.Body.String())
	assert.Equal(t, "Carol Danvers", created["name"], "the response dropped the display name")

	ctx := context.Background()
	byUsername, err := mem.GetUserByUsername(ctx, "acme", "carol")
	require.NoError(t, err, "the account must resolve by the username the request asked for")
	assert.Equal(t, "carol", byUsername.Username)
	assert.Equal(t, "Carol Danvers", byUsername.Name)

	_, err = mem.GetUserByUsername(ctx, "acme", "Carol Danvers")
	assert.Error(t, err, "the display name must not resolve as a username")
}

// TestREST_UpdateUserChangesUsernameAndDisplayNameIndependently pins the update
// side of the same split.
func TestREST_UpdateUserChangesUsernameAndDisplayNameIndependently(t *testing.T) {
	handler, mem, cleanup := setupUserReadHandler(t)
	defer cleanup()

	w := postUser(t, handler, "acme",
		`{"email":"dave@acme.com","username":"dave","name":"Dave One","lifecycle":"active"}`)
	require.Equal(t, http.StatusCreated, w.Code, "CreateUser failed: %s", w.Body.String())
	var created map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	userID, _ := created["id"].(string)
	require.NotEmpty(t, userID)

	w = putUser(t, handler, "acme", userID, `{"username":"davey","name":"Dave Two"}`)
	require.Equal(t, http.StatusOK, w.Code, "UpdateUser failed: %s", w.Body.String())

	var updated map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
	assert.Equal(t, "davey", updated["username"], "the update dropped the username: %s", w.Body.String())
	assert.Equal(t, "Dave Two", updated["name"])

	ctx := context.Background()
	_, err := mem.GetUserByUsername(ctx, "acme", "davey")
	require.NoError(t, err, "the renamed account must resolve by its new username")
	_, err = mem.GetUserByUsername(ctx, "acme", "dave")
	assert.Error(t, err, "the old username must stop resolving")
}

// TestREST_UpdateUserRefusesTheDeletingLifecycle proves an ordinary update on
// this transport cannot put an account into the deletion workflow's terminal
// state — which would close the account for good while leaving its credentials
// and group memberships behind.
func TestREST_UpdateUserRefusesTheDeletingLifecycle(t *testing.T) {
	handler, mem, cleanup := setupUserReadHandler(t)
	defer cleanup()

	w := postUser(t, handler, "acme",
		`{"email":"erin@acme.com","username":"erin","name":"Erin","lifecycle":"active"}`)
	require.Equal(t, http.StatusCreated, w.Code, "CreateUser failed: %s", w.Body.String())
	var created map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	userID, _ := created["id"].(string)
	require.NotEmpty(t, userID)

	w = putUser(t, handler, "acme", userID, `{"lifecycle":"deleting"}`)
	assert.NotEqual(t, http.StatusOK, w.Code,
		"an ordinary update was allowed to claim the deleting state: %s", w.Body.String())

	after, err := mem.GetUser(context.Background(), "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecycleActive, after.Lifecycle,
		"the refused update changed the account state anyway")
	assert.True(t, after.CanAuthenticate(), "the refused update disabled the account")
}

// TestREST_UpdateUserStillAcceptsALegitimateLifecycle proves the refusal is
// specific: the states an administrator legitimately moves an account through
// still reach the store through this transport.
func TestREST_UpdateUserStillAcceptsALegitimateLifecycle(t *testing.T) {
	handler, mem, cleanup := setupUserReadHandler(t)
	defer cleanup()

	w := postUser(t, handler, "acme",
		`{"email":"finn@acme.com","username":"finn","name":"Finn","lifecycle":"active"}`)
	require.Equal(t, http.StatusCreated, w.Code, "CreateUser failed: %s", w.Body.String())
	var created map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	userID, _ := created["id"].(string)
	require.NotEmpty(t, userID)

	w = putUser(t, handler, "acme", userID, `{"lifecycle":"suspended"}`)
	require.Equal(t, http.StatusOK, w.Code, "UpdateUser failed: %s", w.Body.String())

	after, err := mem.GetUser(context.Background(), "acme", userID)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecycleSuspended, after.Lifecycle)
}
