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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// setupDCRLifecycleHandler wires a real AppManager (setupAppHandler) and enables
// anonymous Dynamic Client Registration into the non-system appsTenant, so the
// full RFC 7591 -> RFC 7592 round trip is exercised end to end.
func setupDCRLifecycleHandler(t *testing.T) (*IAMHandler, func()) {
	t.Helper()
	handler, cleanup := setupAppHandler(t)
	handler.server.config.IAMDynamicClientRegistration = config.IAMDynamicClientRegistrationConfig{
		Enabled:           true,
		AnonymousTenantID: appsTenant,
	}
	return handler, cleanup
}

// registerDCRClient registers a confidential client via the AppManager and
// returns its client_id together with the RFC 7592 registration access token
// that is the ONLY credential for the management endpoints.
func registerDCRClient(t *testing.T, handler *IAMHandler) (clientID, registrationToken string) {
	t.Helper()
	body, _ := json.Marshal(DynamicClientRequest{
		ClientName:    "DCR App",
		RedirectURIs:  []string{"https://app.example/cb"},
		GrantTypes:    []string{"authorization_code"},
		ResponseTypes: []string{"code"},
	})
	req := httptest.NewRequest(http.MethodPost, "/oauth2/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.RegisterDynamicClient(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	var resp DynamicClientResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotEmpty(t, resp.ClientID)
	require.NotEmpty(t, resp.RegistrationAccessToken, "registration response must carry the RFC 7592 credential")
	require.Contains(t, resp.RegistrationClientURI, resp.ClientID)
	return resp.ClientID, resp.RegistrationAccessToken
}

func dcrWithClientID(r *http.Request, clientID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("client_id", clientID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// dcrManagementRequest builds an RFC 7592 client-configuration request carrying
// the supplied registration access token.
func dcrManagementRequest(method, clientID, token string, body []byte) *http.Request {
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, "/oauth2/register/"+clientID, nil)
	} else {
		r = httptest.NewRequest(method, "/oauth2/register/"+clientID, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return dcrWithClientID(r, clientID)
}

func TestRegisterDynamicClient_Success(t *testing.T) {
	handler, cleanup := setupDCRLifecycleHandler(t)
	defer cleanup()
	registerDCRClient(t, handler)
}

// The registration response is the one and only time the credential is visible;
// a second registration must never reuse it.
func TestRegisterDynamicClient_TokensAreUnique(t *testing.T) {
	handler, cleanup := setupDCRLifecycleHandler(t)
	defer cleanup()

	idA, tokenA := registerDCRClient(t, handler)
	idB, tokenB := registerDCRClient(t, handler)

	assert.NotEqual(t, idA, idB)
	assert.NotEqual(t, tokenA, tokenB)
}

func TestRegisterDynamicClient_InvalidJSON(t *testing.T) {
	handler, cleanup := setupDCRLifecycleHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/oauth2/register", bytes.NewReader([]byte("{bad")))
	w := httptest.NewRecorder()
	handler.RegisterDynamicClient(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetDynamicClient_Lifecycle(t *testing.T) {
	handler, cleanup := setupDCRLifecycleHandler(t)
	defer cleanup()

	clientID, token := registerDCRClient(t, handler)

	// GET without auth -> 401
	w := httptest.NewRecorder()
	handler.GetDynamicClient(w, dcrManagementRequest(http.MethodGet, clientID, "", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// GET with the issued registration access token -> 200
	w = httptest.NewRecorder()
	handler.GetDynamicClient(w, dcrManagementRequest(http.MethodGet, clientID, token, nil))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp DynamicClientResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, clientID, resp.ClientID)
	// The read view never re-issues (or echoes) the credential.
	assert.Empty(t, resp.RegistrationAccessToken)
	assert.Empty(t, resp.ClientSecret)

	// GET an unknown client with a real token -> 401, NOT 404: the endpoint must
	// not be usable to probe which client ids exist.
	w = httptest.NewRecorder()
	handler.GetDynamicClient(w, dcrManagementRequest(http.MethodGet, "unknown", token, nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUpdateDynamicClient_Success(t *testing.T) {
	handler, cleanup := setupDCRLifecycleHandler(t)
	defer cleanup()

	clientID, token := registerDCRClient(t, handler)

	body, _ := json.Marshal(DynamicClientRequest{
		ClientName:    "Renamed DCR",
		RedirectURIs:  []string{"https://app.example/cb2"},
		GrantTypes:    []string{"authorization_code"},
		ResponseTypes: []string{"code"},
	})
	w := httptest.NewRecorder()
	handler.UpdateDynamicClient(w, dcrManagementRequest(http.MethodPut, clientID, token, body))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp DynamicClientResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "Renamed DCR", resp.ClientName)
	assert.Equal(t, []string{"https://app.example/cb2"}, resp.RedirectURIs)
}

// An authorized update with a malformed body is a 400 — the body is only parsed
// after the credential check succeeds.
func TestUpdateDynamicClient_AuthorizedInvalidBody(t *testing.T) {
	handler, cleanup := setupDCRLifecycleHandler(t)
	defer cleanup()

	clientID, token := registerDCRClient(t, handler)

	w := httptest.NewRecorder()
	handler.UpdateDynamicClient(w, dcrManagementRequest(http.MethodPut, clientID, token, []byte("not json")))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid JSON request body")
}

func TestDeleteDynamicClient_Success(t *testing.T) {
	handler, cleanup := setupDCRLifecycleHandler(t)
	defer cleanup()

	clientID, token := registerDCRClient(t, handler)

	w := httptest.NewRecorder()
	handler.DeleteDynamicClient(w, dcrManagementRequest(http.MethodDelete, clientID, token, nil))
	assert.Equal(t, http.StatusNoContent, w.Code)

	// The credential dies with the client: a replay now resolves to nothing.
	w = httptest.NewRecorder()
	handler.GetDynamicClient(w, dcrManagementRequest(http.MethodGet, clientID, token, nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

var _ = iam.RegisterAppOptions{}
