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

//go:build integration && rest

package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/test/integration/authzfake"
)

// testRESTContext provides infrastructure for REST authorization tests.
type testRESTContext struct {
	t               *testing.T
	ctx             context.Context
	cancel          context.CancelFunc
	logger          *logger.Logger
	authManager     *auth.AuthManager
	authzService    interfaces.AuthorizationService
	authzMiddleware *rest.AuthzMiddleware
}

// newTestRESTContext creates a new test context for REST authorization tests.
func newTestRESTContext(t *testing.T, allowed ...string) *testRESTContext {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-rest-secret-key-for-integration-testing",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		MaxTokens:       100,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	authzService := authzfake.New(allowed...)

	// Create middleware with mock token validator using the auth manager
	authzMiddleware := rest.NewAuthzMiddleware(&rest.AuthzMiddlewareConfig{
		Logger:         log,
		TokenValidator: &mockTokenValidator{authManager: authManager},
		AuthzService:   authzService,
	})

	return &testRESTContext{
		t:               t,
		ctx:             ctx,
		cancel:          cancel,
		logger:          log,
		authManager:     authManager,
		authzService:    authzService,
		authzMiddleware: authzMiddleware,
	}
}

// mockTokenValidator wraps auth manager for token validation.
type mockTokenValidator struct {
	authManager *auth.AuthManager
}

// ValidateToken implements rest.TokenValidator.
func (m *mockTokenValidator) ValidateToken(ctx context.Context, token string) (*auth.TokenValidationResult, error) {
	return m.authManager.ValidateToken(ctx, token)
}

// Close releases test resources.
func (tc *testRESTContext) Close() {
	tc.cancel()
	if tc.authzService != nil {
		tc.authzService.Close()
	}
}

// createTestUser creates a user and returns the token.
func (tc *testRESTContext) createTestUser(username string, roles []string) (string, error) {
	_, err := tc.authManager.CreateUser(username, roles)
	if err != nil {
		return "", err
	}

	userInfo := tc.authManager.GetUserStore().GetByUsername(username)
	token, err := tc.authManager.IssueTokenForUser(tc.ctx, userInfo)
	if err != nil {
		return "", err
	}

	return token.AccessToken, nil
}

// mockHandler is a simple handler that returns 200 OK.
func mockHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
}

// TestRESTPublicEndpointsNoAuth tests that public endpoints bypass authorization.
func TestRESTPublicEndpointsNoAuth(t *testing.T) {
	tc := newTestRESTContext(t, "*:*")
	defer tc.Close()

	handler := tc.authzMiddleware.Authorize(mockHandler())

	publicEndpoints := []string{
		"/api/v1/auth/login/piv",
		"/api/v1/health",
		"/api/v1/health/liveness",
		"/api/v1/health/readiness",
		"/metrics",
	}

	for _, endpoint := range publicEndpoints {
		t.Run("public_"+strings.ReplaceAll(endpoint, "/", "_"), func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, endpoint, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			// Public endpoints should return 200 (not 401 or 403)
			assert.Equal(t, http.StatusOK, rec.Code,
				"Public endpoint %s should be accessible without auth", endpoint)
		})
	}
}

// TestRESTProtectedEndpointsRequireToken tests that protected endpoints require a valid JWT.
func TestRESTProtectedEndpointsRequireToken(t *testing.T) {
	tc := newTestRESTContext(t, "*:*")
	defer tc.Close()

	handler := tc.authzMiddleware.Authorize(mockHandler())

	protectedEndpoints := []struct {
		method   string
		path     string
		resource string
	}{
		{http.MethodGet, "/api/v1/keys/test", "keys"},
		{http.MethodPost, "/api/v1/keys", "keys"},
		{http.MethodDelete, "/api/v1/keys/test", "keys"},
		{http.MethodGet, "/api/v1/secrets/test", "secrets"},
		{http.MethodPost, "/api/v1/kms/keys", "kms"},
		{http.MethodPost, "/api/v1/crypto/encrypt", "crypto"},
		{http.MethodGet, "/api/v1/cluster/status", "cluster"},
		{http.MethodGet, "/api/v1/timeseries/query", "timeseries"},
		{http.MethodGet, "/api/v1/vector/search", "vector"},
	}

	for _, ep := range protectedEndpoints {
		t.Run("requires_auth_"+ep.method+"_"+ep.resource, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusUnauthorized, rec.Code,
				"Protected endpoint %s %s should return 401 without token", ep.method, ep.path)
		})
	}
}

// TestREST401ForMissingToken tests 401 Unauthorized for missing token.
func TestREST401ForMissingToken(t *testing.T) {
	tc := newTestRESTContext(t, "*:*")
	defer tc.Close()

	handler := tc.authzMiddleware.Authorize(mockHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	body, _ := io.ReadAll(rec.Body)
	assert.Contains(t, string(body), "Missing or invalid authorization header", "Response should indicate missing authorization")
}

// TestREST401ForInvalidToken tests 401 Unauthorized for invalid token.
func TestREST401ForInvalidToken(t *testing.T) {
	tc := newTestRESTContext(t, "*:*")
	defer tc.Close()

	handler := tc.authzMiddleware.Authorize(mockHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-12345")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestREST401ForExpiredToken tests 401 Unauthorized for expired token.
func TestREST401ForExpiredToken(t *testing.T) {
	// Create a context with very short token TTL
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-expired-token-secret",
		TokenTTL:        1 * time.Millisecond, // Very short TTL
		RefreshTokenTTL: 1 * time.Minute,
		MaxTokens:       100,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	authzService := authzfake.New("*:*")
	defer authzService.Close()

	authzMiddleware := rest.NewAuthzMiddleware(&rest.AuthzMiddlewareConfig{
		Logger:         log,
		TokenValidator: &mockTokenValidator{authManager: authManager},
		AuthzService:   authzService,
	})

	handler := authzMiddleware.Authorize(mockHandler())

	// Create user and get token
	_, err = authManager.CreateUser("expire-test", []string{"admin"})
	require.NoError(t, err)

	userInfo := authManager.GetUserStore().GetByUsername("expire-test")
	token, err := authManager.IssueTokenForUser(ctx, userInfo)
	require.NoError(t, err)

	// Wait for token to expire
	time.Sleep(10 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestREST403ForInsufficientPermissions tests 403 Forbidden for insufficient permissions.
func TestREST403ForInsufficientPermissions(t *testing.T) {
	tc := newTestRESTContext(t, "keys:read")
	defer tc.Close()

	handler := tc.authzMiddleware.Authorize(mockHandler())

	// Create readonly user
	token, err := tc.createTestUser("readonly-rest", []string{"readonly"})
	require.NoError(t, err)

	// Readonly user trying to POST (write action)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/keys", strings.NewReader(`{"key":"test"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// TestRESTAdminRoleAccess tests that admin role has full access.
func TestRESTAdminRoleAccess(t *testing.T) {
	tc := newTestRESTContext(t, "*:*")
	defer tc.Close()

	handler := tc.authzMiddleware.Authorize(mockHandler())

	token, err := tc.createTestUser("admin-rest", []string{"admin"})
	require.NoError(t, err)

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/keys/test"},
		{http.MethodPost, "/api/v1/keys"},
		{http.MethodDelete, "/api/v1/keys/test"},
		{http.MethodGet, "/api/v1/users"},
		{http.MethodPost, "/api/v1/roles"},
		{http.MethodGet, "/api/v1/cluster/status"},
	}

	for _, ep := range endpoints {
		t.Run("admin_can_"+ep.method+"_"+strings.ReplaceAll(ep.path, "/", "_"), func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code,
				"Admin should have access to %s %s", ep.method, ep.path)
		})
	}
}

// TestRESTReadWriteRoleAccess tests readwrite role permissions.
func TestRESTReadWriteRoleAccess(t *testing.T) {
	tc := newTestRESTContext(t, "keys:read", "keys:write", "keys:delete")
	defer tc.Close()

	handler := tc.authzMiddleware.Authorize(mockHandler())

	token, err := tc.createTestUser("readwrite-rest", []string{"readwrite"})
	require.NoError(t, err)

	t.Run("can_read_keys", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("can_write_keys", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/keys", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("can_delete_keys", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/keys/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("cannot_admin_users", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/users", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		// Users resource requires admin permission for POST
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

// TestRESTReadOnlyRoleAccess tests readonly role permissions.
func TestRESTReadOnlyRoleAccess(t *testing.T) {
	tc := newTestRESTContext(t, "keys:read")
	defer tc.Close()

	handler := tc.authzMiddleware.Authorize(mockHandler())

	token, err := tc.createTestUser("readonly-rest-2", []string{"readonly"})
	require.NoError(t, err)

	t.Run("can_read_keys", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("cannot_write_keys", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/keys", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("cannot_delete_keys", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/keys/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

// TestRESTResourceActionMapping tests HTTP method to action mapping.
func TestRESTResourceActionMapping(t *testing.T) {
	tc := newTestRESTContext(t, "keys:read")
	defer tc.Close()

	handler := tc.authzMiddleware.Authorize(mockHandler())

	// Create a user with only read permission
	_, err := tc.authManager.CreateUser("mapping-test", []string{"readonly"})
	require.NoError(t, err)
	userInfo := tc.authManager.GetUserStore().GetByUsername("mapping-test")
	token, err := tc.authManager.IssueTokenForUser(tc.ctx, userInfo)
	require.NoError(t, err)

	methodTests := []struct {
		method      string
		expectCode  int
		description string
	}{
		{http.MethodGet, http.StatusOK, "GET should map to read action"},
		{http.MethodHead, http.StatusOK, "HEAD should map to read action"},
		{http.MethodOptions, http.StatusOK, "OPTIONS should map to read action"},
		{http.MethodPost, http.StatusForbidden, "POST should map to write action"},
		{http.MethodPut, http.StatusForbidden, "PUT should map to write action"},
		{http.MethodPatch, http.StatusForbidden, "PATCH should map to write action"},
		{http.MethodDelete, http.StatusForbidden, "DELETE should map to delete action"},
	}

	for _, test := range methodTests {
		t.Run(test.description, func(t *testing.T) {
			req := httptest.NewRequest(test.method, "/api/v1/keys/test", nil)
			req.Header.Set("Authorization", "Bearer "+token.AccessToken)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)
			assert.Equal(t, test.expectCode, rec.Code, test.description)
		})
	}
}

// TestRESTResourceExtraction tests resource extraction from URL paths.
func TestRESTResourceExtraction(t *testing.T) {
	tc := newTestRESTContext(t, "*:*")
	defer tc.Close()

	handler := tc.authzMiddleware.Authorize(mockHandler())

	// Create admin to verify access patterns
	token, err := tc.createTestUser("resource-test", []string{"admin"})
	require.NoError(t, err)

	resourcePaths := []struct {
		path     string
		expected int
	}{
		{"/api/v1/keys", http.StatusOK},
		{"/api/v1/keys/mykey", http.StatusOK},
		{"/api/v1/keys/namespace/subkey", http.StatusOK},
		{"/api/v1/secrets/mysecret", http.StatusOK},
		{"/api/v1/kms/keys", http.StatusOK},
		{"/api/v1/crypto/encrypt", http.StatusOK},
		{"/api/v1/seal/status", http.StatusOK},
		{"/api/v1/cluster/status", http.StatusOK},
		{"/api/v1/timeseries/query", http.StatusOK},
		{"/api/v1/vector/search", http.StatusOK},
		{"/api/v1/transactions", http.StatusOK},
		{"/api/v1/backups", http.StatusOK},
	}

	for _, rp := range resourcePaths {
		t.Run("extract_resource_"+strings.ReplaceAll(rp.path, "/", "_"), func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, rp.path, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)
			assert.Equal(t, rp.expected, rec.Code,
				"Admin should access %s", rp.path)
		})
	}
}

// TestRESTRevokedTokenRejected tests that revoked tokens are rejected.
func TestRESTRevokedTokenRejected(t *testing.T) {
	tc := newTestRESTContext(t, "keys:read")
	defer tc.Close()

	handler := tc.authzMiddleware.Authorize(mockHandler())

	token, err := tc.createTestUser("revoke-test", []string{"admin"})
	require.NoError(t, err)

	// Verify token works initially
	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Revoke the token
	err = tc.authManager.RevokeToken(tc.ctx, token)
	require.NoError(t, err)

	// Verify token is now rejected
	req = httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestRESTMalformedAuthorizationHeader tests handling of malformed Authorization headers.
func TestRESTMalformedAuthorizationHeader(t *testing.T) {
	tc := newTestRESTContext(t, "*:*")
	defer tc.Close()

	handler := tc.authzMiddleware.Authorize(mockHandler())

	malformedHeaders := []struct {
		header      string
		description string
	}{
		{"", "empty header"},
		{"Basic dXNlcjpwYXNz", "Basic auth instead of Bearer"},
		{"Bearer", "Bearer without token"},
		{"Bearer ", "Bearer with empty token"},
		{"Token abc123", "Wrong scheme"},
		{"bearer abc123", "lowercase bearer"},
	}

	for _, mh := range malformedHeaders {
		t.Run(mh.description, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
			if mh.header != "" {
				req.Header.Set("Authorization", mh.header)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusUnauthorized, rec.Code,
				"Should return 401 for %s", mh.description)
		})
	}
}

// TestRESTCustomRoleAuthorization tests authorization with custom roles.
func TestRESTCustomRoleAuthorization(t *testing.T) {
	tc := newTestRESTContext(t, "secrets:read")
	defer tc.Close()

	handler := tc.authzMiddleware.Authorize(mockHandler())

	// The server's authzfake allow-list grants secrets:read only, modelling a
	// narrow custom "secrets-reader" role.
	token, err := tc.createTestUser("secrets-only", []string{"secrets-reader"})
	require.NoError(t, err)

	t.Run("can_read_secrets", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/mysecret", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("cannot_write_secrets", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("cannot_read_keys", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}
