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
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

// TestRegisterDynamicClient tests the RegisterDynamicClient handler.
func TestRegisterDynamicClient(t *testing.T) {
	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader("invalid json"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RegisterDynamicClient(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON request body")
	})

	t.Run("missing redirect_uris for authorization_code grant", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"grant_types":["authorization_code"],"client_name":"Test Client"}`
		req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RegisterDynamicClient(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "redirect_uris required for authorization_code")
	})

	t.Run("invalid redirect_uri - empty string in array", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		// Empty string redirect URI should be invalid
		body := `{"grant_types":["authorization_code"],"redirect_uris":[""]}`
		req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RegisterDynamicClient(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid redirect_uri")
	})

	t.Run("unsupported grant_type", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"grant_types":["invalid_grant"],"redirect_uris":["https://example.com/callback"]}`
		req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RegisterDynamicClient(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "unsupported grant_type")
	})

	t.Run("unsupported response_type", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"grant_types":["client_credentials"],"response_types":["invalid_type"]}`
		req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RegisterDynamicClient(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "unsupported response_type")
	})

	t.Run("unsupported token_endpoint_auth_method", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"grant_types":["client_credentials"],"token_endpoint_auth_method":"invalid_method"}`
		req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RegisterDynamicClient(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "unsupported token_endpoint_auth_method")
	})

	t.Run("dynamic registration not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"grant_types":["client_credentials"],"client_name":"Test Client"}`
		req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RegisterDynamicClient(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "dynamic registration not available")
	})

	t.Run("valid redirect URIs - HTTPS", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"grant_types":["authorization_code"],"redirect_uris":["https://example.com/callback"]}`
		req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RegisterDynamicClient(w, req)

		// Will fail at app manager check, but validates the redirect URI
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "dynamic registration not available")
	})

	t.Run("valid redirect URIs - localhost HTTP", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"grant_types":["authorization_code"],"redirect_uris":["http://localhost:8080/callback"]}`
		req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RegisterDynamicClient(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "dynamic registration not available")
	})

	t.Run("valid redirect URIs - 127.0.0.1 HTTP", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"grant_types":["authorization_code"],"redirect_uris":["http://127.0.0.1:8080/callback"]}`
		req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RegisterDynamicClient(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "dynamic registration not available")
	})

	t.Run("valid redirect URIs - IPv6 localhost", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"grant_types":["authorization_code"],"redirect_uris":["http://[::1]:8080/callback"]}`
		req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RegisterDynamicClient(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "dynamic registration not available")
	})

	t.Run("valid redirect URIs - custom scheme", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"grant_types":["authorization_code"],"redirect_uris":["com.example.app:/callback"]}`
		req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RegisterDynamicClient(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "dynamic registration not available")
	})

	t.Run("with tenant header", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"grant_types":["client_credentials"],"client_name":"Test Client"}`
		req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", "custom-tenant")
		w := httptest.NewRecorder()

		handler.RegisterDynamicClient(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "dynamic registration not available")
	})

	t.Run("default grant type requires redirect_uris", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		// Empty grant_types defaults to authorization_code which requires redirect_uris
		body := `{"client_name":"Test Client"}`
		req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RegisterDynamicClient(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "redirect_uris required for authorization_code")
	})

	t.Run("valid grant types - refresh_token", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		// Test refresh_token grant type
		body := `{"grant_types":["refresh_token"],"client_name":"Test Client"}`
		req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RegisterDynamicClient(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "dynamic registration not available")
	})

	t.Run("device code grant type", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"grant_types":["urn:ietf:params:oauth:grant-type:device_code"],"client_name":"Test Client"}`
		req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RegisterDynamicClient(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "dynamic registration not available")
	})

	t.Run("all valid response types", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"grant_types":["client_credentials"],"response_types":["code","token","id_token"]}`
		req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RegisterDynamicClient(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "dynamic registration not available")
	})

	t.Run("all valid token auth methods", func(t *testing.T) {
		// Test each valid auth method
		validMethods := []string{"client_secret_basic", "client_secret_post", "none", "private_key_jwt"}
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		for _, method := range validMethods {
			body := `{"grant_types":["client_credentials"],"token_endpoint_auth_method":"` + method + `"}`
			req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.RegisterDynamicClient(w, req)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Contains(t, w.Body.String(), "dynamic registration not available")
		}
	})
}

// TestGetDynamicClient tests the GetDynamicClient handler.
func TestGetDynamicClient(t *testing.T) {
	t.Run("missing client_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("client_id", "")

		req := httptest.NewRequest(http.MethodGet, "/oauth2/register/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetDynamicClient(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "client_id required")
	})

	t.Run("missing authorization header", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("client_id", "client-123")

		req := httptest.NewRequest(http.MethodGet, "/oauth2/register/client-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetDynamicClient(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "missing Authorization header")
	})

	t.Run("invalid authorization header format", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("client_id", "client-123")

		req := httptest.NewRequest(http.MethodGet, "/oauth2/register/client-123", nil)
		req.Header.Set("Authorization", "InvalidFormat")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetDynamicClient(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "invalid Authorization header format")
	})

	t.Run("empty bearer token", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("client_id", "client-123")

		req := httptest.NewRequest(http.MethodGet, "/oauth2/register/client-123", nil)
		req.Header.Set("Authorization", "Bearer ")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetDynamicClient(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "empty registration_access_token")
	})

	t.Run("client management not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("client_id", "client-123")

		req := httptest.NewRequest(http.MethodGet, "/oauth2/register/client-123", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetDynamicClient(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "client management not available")
	})
}

// TestUpdateDynamicClient tests the UpdateDynamicClient handler.
func TestUpdateDynamicClient(t *testing.T) {
	t.Run("missing client_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("client_id", "")

		req := httptest.NewRequest(http.MethodPut, "/oauth2/register/", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.UpdateDynamicClient(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "client_id required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("client_id", "client-123")

		req := httptest.NewRequest(http.MethodPut, "/oauth2/register/client-123", strings.NewReader("invalid json"))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.UpdateDynamicClient(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON request body")
	})

	t.Run("invalid redirect_uri - empty string", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("client_id", "client-123")

		// Empty redirect URI in array should be invalid
		body := `{"grant_types":["authorization_code"],"redirect_uris":[""]}`
		req := httptest.NewRequest(http.MethodPut, "/oauth2/register/client-123", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.UpdateDynamicClient(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid redirect_uri")
	})

	t.Run("missing authorization header", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("client_id", "client-123")

		body := `{"grant_types":["client_credentials"]}`
		req := httptest.NewRequest(http.MethodPut, "/oauth2/register/client-123", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.UpdateDynamicClient(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "missing Authorization header")
	})

	t.Run("client management not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("client_id", "client-123")

		body := `{"grant_types":["client_credentials"]}`
		req := httptest.NewRequest(http.MethodPut, "/oauth2/register/client-123", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer valid-token")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.UpdateDynamicClient(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "client management not available")
	})
}

// TestDeleteDynamicClient tests the DeleteDynamicClient handler.
func TestDeleteDynamicClient(t *testing.T) {
	t.Run("missing client_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("client_id", "")

		req := httptest.NewRequest(http.MethodDelete, "/oauth2/register/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.DeleteDynamicClient(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "client_id required")
	})

	t.Run("missing authorization header", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("client_id", "client-123")

		req := httptest.NewRequest(http.MethodDelete, "/oauth2/register/client-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.DeleteDynamicClient(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "missing Authorization header")
	})

	t.Run("client management not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("client_id", "client-123")

		req := httptest.NewRequest(http.MethodDelete, "/oauth2/register/client-123", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.DeleteDynamicClient(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "client management not available")
	})
}

// TestIsValidRedirectURI tests the isValidRedirectURI helper function.
func TestIsValidRedirectURI(t *testing.T) {
	t.Run("empty URI", func(t *testing.T) {
		assert.False(t, isValidRedirectURI(""))
	})

	t.Run("HTTPS URI", func(t *testing.T) {
		assert.True(t, isValidRedirectURI("https://example.com/callback"))
	})

	t.Run("localhost HTTP", func(t *testing.T) {
		assert.True(t, isValidRedirectURI("http://localhost:8080/callback"))
	})

	t.Run("127.0.0.1 HTTP", func(t *testing.T) {
		assert.True(t, isValidRedirectURI("http://127.0.0.1:8080/callback"))
	})

	t.Run("IPv6 localhost", func(t *testing.T) {
		assert.True(t, isValidRedirectURI("http://[::1]:8080/callback"))
	})

	t.Run("custom scheme - native app", func(t *testing.T) {
		assert.True(t, isValidRedirectURI("com.example.app:/callback"))
	})

	t.Run("custom scheme - FTP is valid per RFC 8252", func(t *testing.T) {
		// The implementation allows any valid scheme (alphanumeric with dots/hyphens)
		// This matches RFC 8252 which allows custom URI schemes for native apps
		assert.True(t, isValidRedirectURI("ftp://example.com/callback"))
	})

	t.Run("HTTP non-localhost is valid as custom scheme", func(t *testing.T) {
		// http:// is a valid scheme per the custom scheme validation
		assert.True(t, isValidRedirectURI("http://example.com/callback"))
	})

	t.Run("invalid scheme with special chars", func(t *testing.T) {
		// @ breaks before reaching the colon
		assert.False(t, isValidRedirectURI("invalid@scheme:/callback"))
	})

	t.Run("colon at start is invalid", func(t *testing.T) {
		// Colon at position 0 means empty scheme name
		assert.False(t, isValidRedirectURI(":/callback"))
	})

	t.Run("scheme only without path", func(t *testing.T) {
		// "a:" has length 2, i (position of colon) = 1, len(uri) = 2
		// len(uri) > i+1 means 2 > 2 which is false
		assert.False(t, isValidRedirectURI("a:"))
	})

	t.Run("scheme with path", func(t *testing.T) {
		assert.True(t, isValidRedirectURI("myapp:/callback"))
	})
}

// TestIsValidDCRGrantType tests the isValidDCRGrantType helper function.
func TestIsValidDCRGrantType(t *testing.T) {
	t.Run("authorization_code", func(t *testing.T) {
		assert.True(t, isValidDCRGrantType("authorization_code"))
	})

	t.Run("client_credentials", func(t *testing.T) {
		assert.True(t, isValidDCRGrantType("client_credentials"))
	})

	t.Run("refresh_token", func(t *testing.T) {
		assert.True(t, isValidDCRGrantType("refresh_token"))
	})

	t.Run("device_code", func(t *testing.T) {
		assert.True(t, isValidDCRGrantType("urn:ietf:params:oauth:grant-type:device_code"))
	})

	t.Run("invalid grant type", func(t *testing.T) {
		assert.False(t, isValidDCRGrantType("invalid"))
	})

	t.Run("implicit grant not supported", func(t *testing.T) {
		assert.False(t, isValidDCRGrantType("implicit"))
	})

	t.Run("password grant not supported", func(t *testing.T) {
		assert.False(t, isValidDCRGrantType("password"))
	})
}

// TestIsValidDCRResponseType tests the isValidDCRResponseType helper function.
func TestIsValidDCRResponseType(t *testing.T) {
	t.Run("code", func(t *testing.T) {
		assert.True(t, isValidDCRResponseType("code"))
	})

	t.Run("token", func(t *testing.T) {
		assert.True(t, isValidDCRResponseType("token"))
	})

	t.Run("id_token", func(t *testing.T) {
		assert.True(t, isValidDCRResponseType("id_token"))
	})

	t.Run("invalid response type", func(t *testing.T) {
		assert.False(t, isValidDCRResponseType("invalid"))
	})

	t.Run("code token not supported as single value", func(t *testing.T) {
		assert.False(t, isValidDCRResponseType("code token"))
	})
}

// TestExtractTenantFromRequest tests the extractTenantFromRequest helper function.
func TestExtractTenantFromRequest(t *testing.T) {
	t.Run("with X-Tenant-ID header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-Tenant-ID", "tenant-123")

		tenantID := extractTenantFromRequest(req)
		assert.Equal(t, "tenant-123", tenantID)
	})

	t.Run("without X-Tenant-ID header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		tenantID := extractTenantFromRequest(req)
		assert.Equal(t, "", tenantID)
	})

	t.Run("with empty X-Tenant-ID header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-Tenant-ID", "")

		tenantID := extractTenantFromRequest(req)
		assert.Equal(t, "", tenantID)
	})
}

// TestRespondDCRError tests the respondDCRError helper function.
func TestRespondDCRError(t *testing.T) {
	t.Run("basic error response", func(t *testing.T) {
		w := httptest.NewRecorder()
		respondDCRError(w, http.StatusBadRequest, "invalid_request", "test error")

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		assert.Equal(t, "no-cache", w.Header().Get("Pragma"))
		assert.Contains(t, w.Body.String(), "invalid_request")
		assert.Contains(t, w.Body.String(), "test error")
	})

	t.Run("internal server error", func(t *testing.T) {
		w := httptest.NewRecorder()
		respondDCRError(w, http.StatusInternalServerError, DCRErrorInvalidClientMetadata, "service unavailable")

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), DCRErrorInvalidClientMetadata)
	})

	t.Run("unauthorized error", func(t *testing.T) {
		w := httptest.NewRecorder()
		respondDCRError(w, http.StatusUnauthorized, "invalid_token", "token expired")

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_token")
	})
}

// TestDCRErrorConstants tests the DCR error constants.
func TestDCRErrorConstants(t *testing.T) {
	assert.Equal(t, "invalid_redirect_uri", DCRErrorInvalidRedirectURI)
	assert.Equal(t, "invalid_client_metadata", DCRErrorInvalidClientMetadata)
	assert.Equal(t, "invalid_software_statement", DCRErrorInvalidSoftwareStatement)
	assert.Equal(t, "unapproved_software_statement", DCRErrorUnapprovedSoftwareStatement)
}
