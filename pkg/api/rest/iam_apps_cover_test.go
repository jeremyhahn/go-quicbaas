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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/rbactest"
)

const (
	appsTenant      = "tenant-apps"
	otherAppsTenant = "tenant-other-apps"
	// appsKVReaderRole grants kv:read, the one permission an app-handler
	// caller may grant through the grantor gate.
	appsKVReaderRole = "kv-reader"
)

// appMockService exposes a real AppManager to the IAM handlers.
type appMockService struct {
	*testutil.MockQuicBaaS
	manager *iam.AppManager
}

func (m *appMockService) GetAppManager(_ context.Context) (*iam.AppManager, error) {
	return m.manager, nil
}

func setupAppHandler(t *testing.T) (*IAMHandler, func()) {
	t.Helper()
	memStorage := iam.NewMemoryStorage()
	tenantMgr := &testTenantManager{storage: memStorage}
	_, err := tenantMgr.CreateTenant(context.Background(), iam.CreateTenantOptions{ID: appsTenant, Name: "Apps"})
	require.NoError(t, err)
	_, err = tenantMgr.CreateTenant(context.Background(), iam.CreateTenantOptions{ID: otherAppsTenant, Name: "Other Apps"})
	require.NoError(t, err)

	manager := iam.NewAppManager(iam.NewBackendStorage(newIntrospectMemKV()), tenantMgr,
		iam.WithGrantorPermissions(rbactest.New(map[string][]string{appsKVReaderRole: {"kv:read"}})))

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mockSvc := &appMockService{MockQuicBaaS: testutil.NewMockQuicBaaS(), manager: manager}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc

	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})
	return handler, func() { server.Close() }
}

func appRequest(t *testing.T, method string, body interface{}, params map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, "/x", &buf)
	return withChiParams(req, params)
}

// Register an app then exercise list/get/update/rotate/validate/delete.
func TestAppLifecycle(t *testing.T) {
	handler, cleanup := setupAppHandler(t)
	defer cleanup()
	tenantParams := map[string]string{"tenant_id": appsTenant}

	// Register -> 201
	w := httptest.NewRecorder()
	handler.RegisterApp(w, appRequest(t, http.MethodPost, map[string]interface{}{
		"name":          "My App",
		"redirect_uris": []string{"https://app.example/callback"},
		"grant_types":   []string{"authorization_code"},
	}, tenantParams))
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
	var app map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&app))
	clientID, _ := app["client_id"].(string)
	require.NotEmpty(t, clientID)
	clientSecret, _ := app["client_secret"].(string)

	appParams := map[string]string{"tenant_id": appsTenant, "client_id": clientID}

	// List -> 200
	w = httptest.NewRecorder()
	handler.ListApps(w, appRequest(t, http.MethodGet, nil, tenantParams))
	require.Equal(t, http.StatusOK, w.Code)

	// Get -> 200
	w = httptest.NewRecorder()
	handler.GetApp(w, appRequest(t, http.MethodGet, nil, appParams))
	require.Equal(t, http.StatusOK, w.Code)

	// Update -> 200
	newName := "Renamed App"
	w = httptest.NewRecorder()
	handler.UpdateApp(w, appRequest(t, http.MethodPut, map[string]interface{}{"name": newName}, appParams))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	// Validate client (correct secret) -> 200
	if clientSecret != "" {
		w = httptest.NewRecorder()
		handler.ValidateClient(w, appRequest(t, http.MethodPost, map[string]string{
			"client_id": clientID, "client_secret": clientSecret,
		}, nil))
		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	}

	// Rotate secret -> 200
	w = httptest.NewRecorder()
	handler.RotateSecret(w, appRequest(t, http.MethodPost, nil, appParams))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	// Delete -> 204
	w = httptest.NewRecorder()
	handler.DeleteApp(w, appRequest(t, http.MethodDelete, nil, appParams))
	require.Equal(t, http.StatusNoContent, w.Code)
}

// TestAppVerbs_OtherTenantsClientIsNotFound proves the app verbs look the
// client up inside the caller's tenant: a caller naming its own tenant with
// another tenant's client id gets the same 404 as for a missing client, and
// the other tenant's app is left untouched.
func TestAppVerbs_OtherTenantsClientIsNotFound(t *testing.T) {
	handler, cleanup := setupAppHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.RegisterApp(w, appRequest(t, http.MethodPost, map[string]interface{}{
		"name":          "Victim App",
		"redirect_uris": []string{"https://victim.example/callback"},
		"grant_types":   []string{"authorization_code"},
	}, map[string]string{"tenant_id": otherAppsTenant}))
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
	var app map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&app))
	clientID, _ := app["client_id"].(string)
	require.NotEmpty(t, clientID)

	crossParams := map[string]string{"tenant_id": appsTenant, "client_id": clientID}
	verbs := map[string]func() *httptest.ResponseRecorder{
		"get": func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			handler.GetApp(rec, appRequest(t, http.MethodGet, nil, crossParams))
			return rec
		},
		"update": func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			handler.UpdateApp(rec, appRequest(t, http.MethodPut, map[string]interface{}{"name": "Hijacked"}, crossParams))
			return rec
		},
		"rotate": func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			handler.RotateSecret(rec, appRequest(t, http.MethodPost, nil, crossParams))
			return rec
		},
		"delete": func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			handler.DeleteApp(rec, appRequest(t, http.MethodDelete, nil, crossParams))
			return rec
		},
	}
	for name, call := range verbs {
		rec := call()
		require.Equal(t, http.StatusNotFound, rec.Code, "%s: body: %s", name, rec.Body.String())
	}

	ownerParams := map[string]string{"tenant_id": otherAppsTenant, "client_id": clientID}
	w = httptest.NewRecorder()
	handler.GetApp(w, appRequest(t, http.MethodGet, nil, ownerParams))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var got map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	require.Equal(t, "Victim App", got["name"], "a refused cross-tenant update must not rename the app")
}

func TestRegisterApp_Validation(t *testing.T) {
	handler, cleanup := setupAppHandler(t)
	defer cleanup()
	tenantParams := map[string]string{"tenant_id": appsTenant}

	// Missing name -> 400
	w := httptest.NewRecorder()
	handler.RegisterApp(w, appRequest(t, http.MethodPost,
		map[string]interface{}{"redirect_uris": []string{"https://x/cb"}}, tenantParams))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Missing redirect_uris -> 400
	w = httptest.NewRecorder()
	handler.RegisterApp(w, appRequest(t, http.MethodPost,
		map[string]interface{}{"name": "x"}, tenantParams))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Missing tenant -> 400
	w = httptest.NewRecorder()
	handler.RegisterApp(w, appRequest(t, http.MethodPost,
		map[string]interface{}{"name": "x", "redirect_uris": []string{"https://x/cb"}},
		map[string]string{"tenant_id": ""}))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestUpdateApp_RefusesRedirectURIWithFragment pins draft-ietf-oauth-v2-1
// §2.3 on the update endpoint: a redirect URI with a fragment is answered
// with the same status and error message as registration answers it. The
// body's timestamp differs per response, so only the message is compared.
func TestAppVerbs_RefusedRedirectURIIsBadRequest(t *testing.T) {
	handler, cleanup := setupAppHandler(t)
	defer cleanup()
	tenantParams := map[string]string{"tenant_id": appsTenant}

	w := httptest.NewRecorder()
	handler.RegisterApp(w, appRequest(t, http.MethodPost, map[string]interface{}{
		"name":          "My App",
		"redirect_uris": []string{"https://app.example/callback"},
	}, tenantParams))
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
	var app map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&app))
	clientID, _ := app["client_id"].(string)
	require.NotEmpty(t, clientID)
	appParams := map[string]string{"tenant_id": appsTenant, "client_id": clientID}

	// A refused redirect URI is the caller's mistake: 400 naming the reason
	// on both verbs, never a 500 (go-quicbaas#1463).
	refusals := []struct {
		name   string
		uri    string
		reason string
	}{
		{"fragment", "https://app.example/callback#frag", "must not contain a fragment"},
		{"bad scheme", "ftp://app.example/callback", "URI scheme must be HTTPS"},
		{"empty", "", "redirect URI cannot be empty"},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			badURIs := []string{tc.uri}

			w := httptest.NewRecorder()
			handler.RegisterApp(w, appRequest(t, http.MethodPost, map[string]interface{}{
				"name":          "Refused App",
				"redirect_uris": badURIs,
			}, tenantParams))
			require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
			registerError := appErrorMessage(t, w)
			assert.Contains(t, registerError, tc.reason)

			w = httptest.NewRecorder()
			handler.UpdateApp(w, appRequest(t, http.MethodPut,
				map[string]interface{}{"redirect_uris": badURIs}, appParams))
			assert.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
			assert.Equal(t, registerError, appErrorMessage(t, w))

			w = httptest.NewRecorder()
			handler.GetApp(w, appRequest(t, http.MethodGet, nil, appParams))
			require.Equal(t, http.StatusOK, w.Code)
			var stored map[string]interface{}
			require.NoError(t, json.NewDecoder(w.Body).Decode(&stored))
			assert.Equal(t, []interface{}{"https://app.example/callback"}, stored["redirect_uris"])
		})
	}
}

// appErrorMessage decodes the "error" field of an error response body.
func appErrorMessage(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), "body: %s", w.Body.String())
	require.NotEmpty(t, body.Error)
	return body.Error
}

func TestValidateClient_Validation(t *testing.T) {
	handler, cleanup := setupAppHandler(t)
	defer cleanup()

	// Missing client_id -> 400
	w := httptest.NewRecorder()
	handler.ValidateClient(w, appRequest(t, http.MethodPost, map[string]string{"client_secret": "s"}, nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Missing client_secret -> 400
	w = httptest.NewRecorder()
	handler.ValidateClient(w, appRequest(t, http.MethodPost, map[string]string{"client_id": "c"}, nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
