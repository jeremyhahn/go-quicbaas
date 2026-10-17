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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/rbactest"
)

// registerUpdateTarget registers an app in appsTenant and returns its client ID.
func registerUpdateTarget(t *testing.T, handler *IAMHandler) string {
	t.Helper()
	w := httptest.NewRecorder()
	handler.RegisterApp(w, appRequest(t, http.MethodPost, map[string]interface{}{
		"name":          "Update Target",
		"redirect_uris": []string{"https://app.example/callback"},
		"grant_types":   []string{"authorization_code"},
	}, map[string]string{"tenant_id": appsTenant}))
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
	var app map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&app))
	clientID, ok := app["client_id"].(string)
	require.True(t, ok)
	return clientID
}

// TestUpdateApp_StoredFieldReachesApp sends each stored field on its own and
// reads the app back from the manager to prove the field was applied.
func TestUpdateApp_StoredFieldReachesApp(t *testing.T) {
	sum := sha256.Sum256([]byte("client certificate"))
	thumbprint := base64.RawURLEncoding.EncodeToString(sum[:])

	tests := []struct {
		name  string
		body  map[string]interface{}
		check func(*testing.T, *iam.Application)
	}{
		{"name", map[string]interface{}{"name": "Renamed"}, func(t *testing.T, a *iam.Application) {
			assert.Equal(t, "Renamed", a.Name)
		}},
		{"redirect_uris", map[string]interface{}{"redirect_uris": []string{"https://app.example/new"}}, func(t *testing.T, a *iam.Application) {
			assert.Equal(t, []string{"https://app.example/new"}, a.RedirectURIs)
		}},
		{"resource_prefix", map[string]interface{}{"resource_prefix": "orders/"}, func(t *testing.T, a *iam.Application) {
			assert.Equal(t, "orders/", a.ResourcePrefix)
		}},
		{"default_audience", map[string]interface{}{"default_audience": "api://orders"}, func(t *testing.T, a *iam.Application) {
			assert.Equal(t, "api://orders", a.DefaultAudience)
		}},
		{"grant_types", map[string]interface{}{"grant_types": []string{"client_credentials"}}, func(t *testing.T, a *iam.Application) {
			assert.Equal(t, []string{"client_credentials"}, a.GrantTypes)
		}},
		{"response_types", map[string]interface{}{"response_types": []string{"code", "id_token"}}, func(t *testing.T, a *iam.Application) {
			assert.Equal(t, []string{"code", "id_token"}, a.ResponseTypes)
		}},
		{"token_endpoint_auth_method", map[string]interface{}{"token_endpoint_auth_method": "client_secret_post"}, func(t *testing.T, a *iam.Application) {
			assert.Equal(t, "client_secret_post", a.TokenEndpointAuthMethod)
		}},
		{"mtls_cert_thumbprint", map[string]interface{}{"mtls_cert_thumbprint": thumbprint}, func(t *testing.T, a *iam.Application) {
			assert.Equal(t, thumbprint, a.MTLSCertThumbprint)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, cleanup := setupAppHandler(t)
			defer cleanup()
			clientID := registerUpdateTarget(t, handler)

			w := httptest.NewRecorder()
			handler.UpdateApp(w, appRequest(t, http.MethodPut, tt.body,
				map[string]string{"tenant_id": appsTenant, "client_id": clientID}))
			require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

			mgr := handler.server.service.(*appMockService).manager
			app, err := mgr.GetAppInTenant(context.Background(), appsTenant, clientID)
			require.NoError(t, err)
			tt.check(t, app)
		})
	}
}

// TestUpdateApp_UnstoredFieldIsRefused proves a field the app cannot store is
// refused with 400 and leaves the app unchanged, rather than being dropped.
func TestUpdateApp_UnstoredFieldIsRefused(t *testing.T) {
	handler, cleanup := setupAppHandler(t)
	defer cleanup()
	clientID := registerUpdateTarget(t, handler)

	w := httptest.NewRecorder()
	handler.UpdateApp(w, appRequest(t, http.MethodPut, map[string]interface{}{
		"name":     "Renamed",
		"logo_uri": "https://app.example/logo.png",
	}, map[string]string{"tenant_id": appsTenant, "client_id": clientID}))
	require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "does not store")

	mgr := handler.server.service.(*appMockService).manager
	app, err := mgr.GetAppInTenant(context.Background(), appsTenant, clientID)
	require.NoError(t, err)
	assert.Equal(t, "Update Target", app.Name)
}

// TestUpdateApp_UnsupportedGrantIsBadRequest proves an unsupported grant type
// is a client error (400), not an internal one, and leaves the grants unchanged.
func TestUpdateApp_UnsupportedGrantIsBadRequest(t *testing.T) {
	handler, cleanup := setupAppHandler(t)
	defer cleanup()
	clientID := registerUpdateTarget(t, handler)

	w := httptest.NewRecorder()
	handler.UpdateApp(w, appRequest(t, http.MethodPut, map[string]interface{}{
		"grant_types": []string{"password"},
	}, map[string]string{"tenant_id": appsTenant, "client_id": clientID}))
	require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())

	mgr := handler.server.service.(*appMockService).manager
	app, err := mgr.GetAppInTenant(context.Background(), appsTenant, clientID)
	require.NoError(t, err)
	assert.Equal(t, []string{"authorization_code"}, app.GrantTypes)
}

// grantorUpdate PUTs body to the app as a caller holding roles and returns
// the response with the stored app.
func grantorUpdate(t *testing.T, body map[string]interface{}, roles ...string) (*httptest.ResponseRecorder, *iam.Application) {
	t.Helper()
	handler, cleanup := setupAppHandler(t)
	t.Cleanup(cleanup)
	clientID := registerUpdateTarget(t, handler)

	req := appRequest(t, http.MethodPut, body, map[string]string{"tenant_id": appsTenant, "client_id": clientID})
	req = req.WithContext(rbactest.CallerContext(req.Context(), "alice", appsTenant, roles...))
	w := httptest.NewRecorder()
	handler.UpdateApp(w, req)

	mgr := handler.server.service.(*appMockService).manager
	app, err := mgr.GetAppInTenant(context.Background(), appsTenant, clientID)
	require.NoError(t, err)
	return w, app
}

func TestUpdateApp_GrantableScopesLand(t *testing.T) {
	w, app := grantorUpdate(t, map[string]interface{}{
		"name":   "Renamed",
		"scopes": []string{"kv:read", "openid"},
	}, appsKVReaderRole)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Equal(t, "Renamed", app.Name)
	assert.Equal(t, []string{"kv:read", "openid"}, app.Scopes)
}

func TestUpdateApp_UngrantableScopeIsForbidden(t *testing.T) {
	w, app := grantorUpdate(t, map[string]interface{}{
		"name":   "Renamed",
		"scopes": []string{"kv:read", "kv:write"},
	}, appsKVReaderRole)
	require.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
	assert.Equal(t, "Update Target", app.Name)
	assert.Empty(t, app.Scopes)
}
