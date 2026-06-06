//go:build rest && iam

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

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// registerDCRClient registers a confidential client via the AppManager and
// returns its client_id.
func registerDCRClient(t *testing.T, handler *IAMHandler) string {
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
	return resp.ClientID
}

func dcrWithClientID(r *http.Request, clientID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("client_id", clientID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestRegisterDynamicClient_Success(t *testing.T) {
	handler, cleanup := setupAppHandler(t)
	defer cleanup()
	registerDCRClient(t, handler)
}

func TestRegisterDynamicClient_InvalidJSON(t *testing.T) {
	handler, cleanup := setupAppHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/oauth2/register", bytes.NewReader([]byte("{bad")))
	w := httptest.NewRecorder()
	handler.RegisterDynamicClient(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetDynamicClient_Lifecycle(t *testing.T) {
	handler, cleanup := setupAppHandler(t)
	defer cleanup()

	clientID := registerDCRClient(t, handler)

	// GET without auth -> 401
	w := httptest.NewRecorder()
	handler.GetDynamicClient(w, dcrWithClientID(httptest.NewRequest(http.MethodGet, "/oauth2/register/"+clientID, nil), clientID))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// GET with bearer -> 200
	r := dcrWithClientID(httptest.NewRequest(http.MethodGet, "/oauth2/register/"+clientID, nil), clientID)
	r.Header.Set("Authorization", "Bearer reg-token")
	w = httptest.NewRecorder()
	handler.GetDynamicClient(w, r)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp DynamicClientResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, clientID, resp.ClientID)

	// GET unknown client -> 404
	r = dcrWithClientID(httptest.NewRequest(http.MethodGet, "/oauth2/register/unknown", nil), "unknown")
	r.Header.Set("Authorization", "Bearer reg-token")
	w = httptest.NewRecorder()
	handler.GetDynamicClient(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdateDynamicClient_Success(t *testing.T) {
	handler, cleanup := setupAppHandler(t)
	defer cleanup()

	clientID := registerDCRClient(t, handler)

	body, _ := json.Marshal(DynamicClientRequest{
		ClientName:    "Renamed DCR",
		RedirectURIs:  []string{"https://app.example/cb2"},
		GrantTypes:    []string{"authorization_code"},
		ResponseTypes: []string{"code"},
	})
	r := dcrWithClientID(httptest.NewRequest(http.MethodPut, "/oauth2/register/"+clientID, bytes.NewReader(body)), clientID)
	r.Header.Set("Authorization", "Bearer reg-token")
	w := httptest.NewRecorder()
	handler.UpdateDynamicClient(w, r)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

func TestDeleteDynamicClient_Success(t *testing.T) {
	handler, cleanup := setupAppHandler(t)
	defer cleanup()

	clientID := registerDCRClient(t, handler)

	r := dcrWithClientID(httptest.NewRequest(http.MethodDelete, "/oauth2/register/"+clientID, nil), clientID)
	r.Header.Set("Authorization", "Bearer reg-token")
	w := httptest.NewRecorder()
	handler.DeleteDynamicClient(w, r)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

var _ = iam.RegisterAppOptions{}
