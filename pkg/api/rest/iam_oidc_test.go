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
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// TestGetOIDCConfig tests the GetOIDCConfig handler.
func TestGetOIDCConfig(t *testing.T) {
	t.Run("OIDC provider not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
		w := httptest.NewRecorder()

		handler.GetOIDCConfig(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "OIDC provider not available")
	})
}

// TestGetJWKS tests the GetJWKS handler.
func TestGetJWKS(t *testing.T) {
	t.Run("OIDC provider not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
		w := httptest.NewRecorder()

		handler.GetJWKS(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "OIDC provider not available")
	})
}

// TestAuthorize tests the Authorize handler.
func TestAuthorize(t *testing.T) {
	t.Run("missing client_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/oauth2/authorize", nil)
		w := httptest.NewRecorder()

		handler.Authorize(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "client_id is required")
	})

	t.Run("missing redirect_uri", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/oauth2/authorize?client_id=test-client", nil)
		w := httptest.NewRecorder()

		handler.Authorize(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "redirect_uri is required")
	})

	t.Run("missing response_type", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/oauth2/authorize?client_id=test-client&redirect_uri=https://example.com/callback", nil)
		w := httptest.NewRecorder()

		handler.Authorize(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "response_type is required")
	})

	t.Run("OIDC provider not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/oauth2/authorize?client_id=test-client&redirect_uri=https://example.com/callback&response_type=code", nil)
		w := httptest.NewRecorder()

		handler.Authorize(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "OIDC provider not available")
	})

	t.Run("with scope parameter", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/oauth2/authorize?client_id=test-client&redirect_uri=https://example.com/callback&response_type=code&scope=openid+profile+email", nil)
		w := httptest.NewRecorder()

		handler.Authorize(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "OIDC provider not available")
	})

	t.Run("with all parameters", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/oauth2/authorize?client_id=test-client&redirect_uri=https://example.com/callback&response_type=code&scope=openid&state=random-state&code_challenge=abc123&code_challenge_method=S256&nonce=nonce123&resource=https://api.example.com", nil)
		w := httptest.NewRecorder()

		handler.Authorize(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "OIDC provider not available")
	})
}

// TestToken tests the Token handler.
func TestToken(t *testing.T) {
	t.Run("missing grant_type", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		form := url.Values{}
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		handler.Token(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "grant_type is required")
	})

	t.Run("OIDC provider not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		form := url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("code", "test-code")
		form.Set("redirect_uri", "https://example.com/callback")
		form.Set("client_id", "test-client")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		handler.Token(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "OIDC provider not available")
	})

	t.Run("with scope parameter", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		form := url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("code", "test-code")
		form.Set("scope", "openid profile")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		handler.Token(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "OIDC provider not available")
	})

	t.Run("with all parameters", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		form := url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("code", "test-code")
		form.Set("redirect_uri", "https://example.com/callback")
		form.Set("client_id", "test-client")
		form.Set("client_secret", "test-secret")
		form.Set("code_verifier", "verifier123")
		form.Set("refresh_token", "refresh-token")
		form.Set("resource", "https://api.example.com")
		form.Set("scope", "openid")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		handler.Token(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "OIDC provider not available")
	})
}

// TestUserInfo tests the UserInfo handler.
func TestUserInfo(t *testing.T) {
	t.Run("missing authorization header", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/oauth2/userinfo", nil)
		w := httptest.NewRecorder()

		handler.UserInfo(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "authorization header is required")
	})

	t.Run("invalid authorization header format without bearer", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/oauth2/userinfo", nil)
		req.Header.Set("Authorization", "InvalidToken")
		w := httptest.NewRecorder()

		handler.UserInfo(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "invalid authorization header format")
	})

	t.Run("invalid authorization header format with wrong prefix", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/oauth2/userinfo", nil)
		req.Header.Set("Authorization", "Basic token123")
		w := httptest.NewRecorder()

		handler.UserInfo(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "invalid authorization header format")
	})

	t.Run("OIDC provider not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/oauth2/userinfo", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		w := httptest.NewRecorder()

		handler.UserInfo(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "OIDC provider not available")
	})
}

// TestRevoke tests the Revoke handler.
func TestRevoke(t *testing.T) {
	t.Run("missing token", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		form := url.Values{}
		req := httptest.NewRequest(http.MethodPost, "/oauth2/revoke", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		handler.Revoke(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "token is required")
	})

	t.Run("OIDC provider not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		form := url.Values{}
		form.Set("token", "test-token")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/revoke", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		handler.Revoke(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "OIDC provider not available")
	})

	t.Run("with token_type_hint", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		form := url.Values{}
		form.Set("token", "test-token")
		form.Set("token_type_hint", "access_token")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/revoke", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		handler.Revoke(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "OIDC provider not available")
	})
}

func TestAuthorizeHandlerScopes(t *testing.T) {
	t.Run("with scope parameter", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/oauth2/authorize?client_id=test&redirect_uri=http://example.com&response_type=code&scope=openid+profile+email", nil)
		w := httptest.NewRecorder()

		handler.Authorize(w, req)

		// Will fail because OIDC provider is not configured
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with state parameter", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/oauth2/authorize?client_id=test&redirect_uri=http://example.com&response_type=code&state=abc123", nil)
		w := httptest.NewRecorder()

		handler.Authorize(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with code_challenge for PKCE", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/oauth2/authorize?client_id=test&redirect_uri=http://example.com&response_type=code&code_challenge=abc123&code_challenge_method=S256", nil)
		w := httptest.NewRecorder()

		handler.Authorize(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with nonce parameter", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/oauth2/authorize?client_id=test&redirect_uri=http://example.com&response_type=code&nonce=xyz789", nil)
		w := httptest.NewRecorder()

		handler.Authorize(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with resource parameter RFC 8707", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/oauth2/authorize?client_id=test&redirect_uri=http://example.com&response_type=code&resource=https://api.example.com", nil)
		w := httptest.NewRecorder()

		handler.Authorize(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestTokenHandlerGrantTypes(t *testing.T) {
	t.Run("authorization_code grant type", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		form := url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("code", "test-code")
		form.Set("redirect_uri", "http://example.com/callback")
		form.Set("client_id", "test-client")

		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		handler.Token(w, req)

		// Will fail because OIDC provider is not configured
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("refresh_token grant type", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		form := url.Values{}
		form.Set("grant_type", "refresh_token")
		form.Set("refresh_token", "test-refresh-token")
		form.Set("client_id", "test-client")

		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		handler.Token(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with client_secret", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		form := url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("code", "test-code")
		form.Set("client_id", "test-client")
		form.Set("client_secret", "test-secret")

		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		handler.Token(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with code_verifier for PKCE", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		form := url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("code", "test-code")
		form.Set("client_id", "test-client")
		form.Set("code_verifier", "test-verifier")

		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		handler.Token(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with scope parameter", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		form := url.Values{}
		form.Set("grant_type", "refresh_token")
		form.Set("refresh_token", "test-token")
		form.Set("scope", "openid profile")

		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		handler.Token(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with resource parameter RFC 8707", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		form := url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("code", "test-code")
		form.Set("resource", "https://api.example.com")

		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		handler.Token(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestUserInfoHandlerVariations(t *testing.T) {
	t.Run("with valid bearer token format", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/oauth2/userinfo", nil)
		req.Header.Set("Authorization", "Bearer valid-access-token")
		w := httptest.NewRecorder()

		handler.UserInfo(w, req)

		// Will fail because OIDC provider is not configured
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with lowercase bearer", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/oauth2/userinfo", nil)
		req.Header.Set("Authorization", "bearer valid-access-token")
		w := httptest.NewRecorder()

		handler.UserInfo(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestRevokeHandlerVariations(t *testing.T) {
	t.Run("with token_type_hint", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		form := url.Values{}
		form.Set("token", "test-token")
		form.Set("token_type_hint", "access_token")

		req := httptest.NewRequest(http.MethodPost, "/oauth2/revoke", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		handler.Revoke(w, req)

		// Will fail because OIDC provider is not configured
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with refresh_token type hint", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		form := url.Values{}
		form.Set("token", "test-refresh-token")
		form.Set("token_type_hint", "refresh_token")

		req := httptest.NewRequest(http.MethodPost, "/oauth2/revoke", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		handler.Revoke(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestCreateSessionWithDeviceInfo(t *testing.T) {
	t.Run("with device info fields", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		body := `{
			"token_hash": "abc123",
			"mfa_verified": true,
			"ip_address": "192.168.1.1",
			"user_agent": "Mozilla/5.0",
			"device_info": {
				"id": "device-123",
				"name": "Chrome Browser",
				"type": "browser",
				"fingerprint": "fp-abc123",
				"trusted": true
			},
			"expires_at": "2025-12-31T23:59:59Z"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/sessions", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateSession(w, req)

		// Will fail because session manager is not available
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "session manager not available")
	})

	t.Run("with partial device info", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		body := `{
			"token_hash": "abc123",
			"device_info": {
				"id": "device-456"
			}
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/sessions", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateSession(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with custom session ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		body := `{
			"id": "custom-session-id",
			"token_hash": "abc123"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/sessions", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateSession(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with invalid expires_at format uses default", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		body := `{
			"token_hash": "abc123",
			"expires_at": "invalid-date"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/sessions", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateSession(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestIAMMiddlewareContextValues(t *testing.T) {
	t.Run("ListSessions with empty user_id value", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(context.Background(), "", "")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/sessions", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ListSessions(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("ListSessions with unauthenticated subject", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		// A Subject that was injected but never authenticated fails closed.
		ctx := auth.SubjectIntoContext(context.Background(),
			interfaces.Subject{ID: "user-123", TenantID: "tenant-123", Authenticated: false})
		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/sessions", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ListSessions(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("ListSessions with empty tenant_id value", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(context.Background(), "user-123", "")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/sessions", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ListSessions(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("GetSession with subject missing tenant", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "session-123")

		ctx := ctxWithTestSubject(context.Background(), "user-123", "")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/sessions/session-123", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.GetSession(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestRevokeOtherSessionsBodyParsing(t *testing.T) {
	t.Run("with empty body requires keep_session_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(context.Background(), "user-123", "tenant-123")
		req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.RevokeOtherSessions(w, req)

		// The session to keep must be named in the request body.
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "current session ID is required")
	})

	t.Run("with invalid JSON body rejected", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(context.Background(), "user-123", "tenant-123")
		body := `{invalid json}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RevokeOtherSessions(w, req)

		// Unparseable body yields no keep_session_id -> 400.
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "current session ID is required")
	})

	t.Run("with valid keep_session_id proceeds to manager", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(context.Background(), "user-123", "tenant-123")
		body := `{"keep_session_id": "session-456"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RevokeOtherSessions(w, req)

		// Fails because the session manager is not available on the mock.
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
