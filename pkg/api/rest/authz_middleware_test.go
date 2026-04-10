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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// mockTokenValidator implements TokenValidator for testing.
type mockTokenValidator struct {
	result *auth.TokenValidationResult
	err    error
}

func (m *mockTokenValidator) ValidateToken(ctx context.Context, token string) (*auth.TokenValidationResult, error) {
	return m.result, m.err
}

// mockAuthzService implements interfaces.AuthorizationService for testing.
type mockAuthzService struct {
	decision *interfaces.AuthorizationDecision
	err      error
	// Track calls for verification
	authorizeCallCount int
	lastRequest        *interfaces.AuthorizationRequest
}

func (m *mockAuthzService) Authorize(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
	m.authorizeCallCount++
	m.lastRequest = req
	return m.decision, m.err
}

func (m *mockAuthzService) BatchAuthorize(ctx context.Context, reqs []*interfaces.AuthorizationRequest) ([]*interfaces.AuthorizationDecision, error) {
	return nil, errors.New("not implemented")
}

func (m *mockAuthzService) GetProvider() interfaces.AuthorizationProvider {
	return nil
}

func (m *mockAuthzService) GetProviderType() string {
	return "mock"
}

func (m *mockAuthzService) Close() error {
	return nil
}

// createTestMiddleware creates a middleware instance with mocks for testing.
func createTestMiddleware(validator *mockTokenValidator, authzSvc *mockAuthzService) *AuthzMiddleware {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Output: "stdout", Format: "text"})
	return NewAuthzMiddleware(&AuthzMiddlewareConfig{
		Logger:         log,
		TokenValidator: validator,
		AuthzService:   authzSvc,
	})
}

// testHandler is a simple handler that records if it was called.
type testHandler struct {
	called bool
	userID string
	roles  []string
}

func (h *testHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.called = true
	h.userID = GetUserID(r.Context())
	h.roles = GetUserRoles(r.Context())
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"success": true}`))
}

// TestAuthzMiddleware_PublicEndpoints verifies that public endpoints bypass authorization.
func TestAuthzMiddleware_PublicEndpoints(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"login endpoint", "/api/v1/auth/login"},
		{"health endpoint", "/api/v1/health"},
		{"liveness probe", "/api/v1/health/liveness"},
		{"readiness probe", "/api/v1/health/readiness"},
		{"metrics endpoint", "/metrics"},
		{"health with trailing slash", "/api/v1/health/"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create middleware with mocks that would fail if called
			validator := &mockTokenValidator{
				err: errors.New("should not be called"),
			}
			authzSvc := &mockAuthzService{
				err: errors.New("should not be called"),
			}
			middleware := createTestMiddleware(validator, authzSvc)

			// Create test handler
			handler := &testHandler{}

			// Create request without authorization header
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rr := httptest.NewRecorder()

			// Execute middleware
			middleware.Authorize(handler).ServeHTTP(rr, req)

			// Verify handler was called (bypass succeeded)
			if !handler.called {
				t.Errorf("Expected handler to be called for public endpoint %s", tc.path)
			}

			// Verify 200 status
			if rr.Code != http.StatusOK {
				t.Errorf("Expected status 200, got %d", rr.Code)
			}
		})
	}
}

// TestAuthzMiddleware_MissingToken tests that requests without tokens return 401.
func TestAuthzMiddleware_MissingToken(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
	}{
		{"no authorization header", ""},
		{"empty authorization header", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			validator := &mockTokenValidator{}
			authzSvc := &mockAuthzService{}
			middleware := createTestMiddleware(validator, authzSvc)

			handler := &testHandler{}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rr := httptest.NewRecorder()

			middleware.Authorize(handler).ServeHTTP(rr, req)

			if handler.called {
				t.Error("Expected handler NOT to be called for missing token")
			}

			if rr.Code != http.StatusUnauthorized {
				t.Errorf("Expected status 401, got %d", rr.Code)
			}

			// Verify error response
			var errResp ErrorResponse
			if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
				t.Fatalf("Failed to decode error response: %v", err)
			}
			if errResp.Error == "" {
				t.Error("Expected error message in response")
			}
		})
	}
}

// TestAuthzMiddleware_InvalidAuthHeaderFormat tests various invalid header formats.
func TestAuthzMiddleware_InvalidAuthHeaderFormat(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
	}{
		{"missing bearer prefix", "some-token-value"},
		{"wrong scheme", "Basic dXNlcjpwYXNz"},
		{"bearer lowercase only token", "bearer"},
		{"empty token after bearer", "Bearer "},
		{"bearer with multiple spaces", "Bearer  "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			validator := &mockTokenValidator{}
			authzSvc := &mockAuthzService{}
			middleware := createTestMiddleware(validator, authzSvc)

			handler := &testHandler{}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
			req.Header.Set("Authorization", tc.authHeader)
			rr := httptest.NewRecorder()

			middleware.Authorize(handler).ServeHTTP(rr, req)

			if handler.called {
				t.Error("Expected handler NOT to be called for invalid auth header")
			}

			if rr.Code != http.StatusUnauthorized {
				t.Errorf("Expected status 401, got %d", rr.Code)
			}
		})
	}
}

// TestAuthzMiddleware_InvalidToken tests that invalid tokens return 401.
func TestAuthzMiddleware_InvalidToken(t *testing.T) {
	tests := []struct {
		name   string
		result *auth.TokenValidationResult
		err    error
	}{
		{
			name:   "validation error",
			result: nil,
			err:    errors.New("token validation failed"),
		},
		{
			name: "invalid token result",
			result: &auth.TokenValidationResult{
				Valid: false,
			},
			err: nil,
		},
		{
			name:   "nil result",
			result: nil,
			err:    nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			validator := &mockTokenValidator{
				result: tc.result,
				err:    tc.err,
			}
			authzSvc := &mockAuthzService{}
			middleware := createTestMiddleware(validator, authzSvc)

			handler := &testHandler{}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
			req.Header.Set("Authorization", "Bearer invalid-token")
			rr := httptest.NewRecorder()

			middleware.Authorize(handler).ServeHTTP(rr, req)

			if handler.called {
				t.Error("Expected handler NOT to be called for invalid token")
			}

			if rr.Code != http.StatusUnauthorized {
				t.Errorf("Expected status 401, got %d", rr.Code)
			}
		})
	}
}

// TestAuthzMiddleware_RevokedToken tests that revoked tokens return 401.
func TestAuthzMiddleware_RevokedToken(t *testing.T) {
	validator := &mockTokenValidator{
		result: &auth.TokenValidationResult{
			Valid:     true,
			UserID:    "user-123",
			Username:  "testuser",
			Roles:     []string{"admin"},
			ExpiresAt: time.Now().Add(time.Hour),
			IsRevoked: true, // Token is revoked
		},
	}
	authzSvc := &mockAuthzService{}
	middleware := createTestMiddleware(validator, authzSvc)

	handler := &testHandler{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
	req.Header.Set("Authorization", "Bearer revoked-token")
	rr := httptest.NewRecorder()

	middleware.Authorize(handler).ServeHTTP(rr, req)

	if handler.called {
		t.Error("Expected handler NOT to be called for revoked token")
	}

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rr.Code)
	}
}

// TestAuthzMiddleware_DeniedAccess tests that denied authorization returns 403.
func TestAuthzMiddleware_DeniedAccess(t *testing.T) {
	validator := &mockTokenValidator{
		result: &auth.TokenValidationResult{
			Valid:     true,
			UserID:    "user-123",
			Username:  "testuser",
			Roles:     []string{"reader"},
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}
	authzSvc := &mockAuthzService{
		decision: &interfaces.AuthorizationDecision{
			Allowed:       false,
			Reason:        "insufficient permissions",
			Provider:      "rbac",
			MatchedPolicy: "reader",
		},
	}
	middleware := createTestMiddleware(validator, authzSvc)

	handler := &testHandler{}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/keys/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rr := httptest.NewRecorder()

	middleware.Authorize(handler).ServeHTTP(rr, req)

	if handler.called {
		t.Error("Expected handler NOT to be called for denied access")
	}

	if rr.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", rr.Code)
	}

	// Verify authorization service was called
	if authzSvc.authorizeCallCount != 1 {
		t.Errorf("Expected Authorize to be called once, got %d", authzSvc.authorizeCallCount)
	}

	// Verify request was properly constructed
	if authzSvc.lastRequest == nil {
		t.Fatal("Expected lastRequest to be set")
	}
	if authzSvc.lastRequest.Subject.ID != "user-123" {
		t.Errorf("Expected Subject.ID 'user-123', got '%s'", authzSvc.lastRequest.Subject.ID)
	}
	if authzSvc.lastRequest.Action != interfaces.ActionDelete {
		t.Errorf("Expected action '%s', got '%s'", interfaces.ActionDelete, authzSvc.lastRequest.Action)
	}
}

// TestAuthzMiddleware_AuthorizationError tests that authorization errors return 403.
func TestAuthzMiddleware_AuthorizationError(t *testing.T) {
	validator := &mockTokenValidator{
		result: &auth.TokenValidationResult{
			Valid:     true,
			UserID:    "user-123",
			Username:  "testuser",
			Roles:     []string{"admin"},
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}
	authzSvc := &mockAuthzService{
		err: errors.New("authorization service error"),
	}
	middleware := createTestMiddleware(validator, authzSvc)

	handler := &testHandler{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rr := httptest.NewRecorder()

	middleware.Authorize(handler).ServeHTTP(rr, req)

	if handler.called {
		t.Error("Expected handler NOT to be called for authorization error")
	}

	if rr.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", rr.Code)
	}
}

// TestAuthzMiddleware_AllowedAccess tests that allowed authorization passes through.
func TestAuthzMiddleware_AllowedAccess(t *testing.T) {
	validator := &mockTokenValidator{
		result: &auth.TokenValidationResult{
			Valid:     true,
			UserID:    "user-123",
			Username:  "testuser",
			Roles:     []string{"admin", "writer"},
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}
	authzSvc := &mockAuthzService{
		decision: &interfaces.AuthorizationDecision{
			Allowed:       true,
			Reason:        "admin has full access",
			Provider:      "rbac",
			MatchedPolicy: "admin",
			DecisionID:    "dec-123",
		},
	}
	middleware := createTestMiddleware(validator, authzSvc)

	handler := &testHandler{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/keys/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("User-Agent", "test-client/1.0")
	rr := httptest.NewRecorder()

	middleware.Authorize(handler).ServeHTTP(rr, req)

	if !handler.called {
		t.Error("Expected handler to be called for allowed access")
	}

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	// Verify user context was injected
	if handler.userID != "user-123" {
		t.Errorf("Expected userID 'user-123' in context, got '%s'", handler.userID)
	}
	if len(handler.roles) != 2 || handler.roles[0] != "admin" {
		t.Errorf("Expected roles [admin, writer] in context, got %v", handler.roles)
	}

	// Verify authorization request context
	if authzSvc.lastRequest == nil {
		t.Fatal("Expected lastRequest to be set")
	}
	ctx := authzSvc.lastRequest.Context
	if ctx["http_method"] != "POST" {
		t.Errorf("Expected http_method 'POST', got '%v'", ctx["http_method"])
	}
	if ctx["request_path"] != "/api/v1/keys/test" {
		t.Errorf("Expected request_path '/api/v1/keys/test', got '%v'", ctx["request_path"])
	}
	if ctx["user_agent"] != "test-client/1.0" {
		t.Errorf("Expected user_agent 'test-client/1.0', got '%v'", ctx["user_agent"])
	}
}

// TestAuthzMiddleware_ResourceExtraction tests resource extraction from various paths.
func TestAuthzMiddleware_ResourceExtraction(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/api/v1/keys", interfaces.ResourceKeys},
		{"/api/v1/keys/", interfaces.ResourceKeys},
		{"/api/v1/keys/123", interfaces.ResourceKeys},
		{"/api/v1/keys/test/nested", interfaces.ResourceKeys},
		{"/api/v1/secrets", interfaces.ResourceSecrets},
		{"/api/v1/secrets/my-secret", interfaces.ResourceSecrets},
		{"/api/v1/kms", interfaces.ResourceKMS},
		{"/api/v1/kms/keys/encrypt", interfaces.ResourceKMS},
		{"/api/v1/crypto", interfaces.ResourceCrypto},
		{"/api/v1/crypto/encrypt", interfaces.ResourceCrypto},
		{"/api/v1/seal", interfaces.ResourceSeal},
		{"/api/v1/seal/status", interfaces.ResourceSeal},
		{"/api/v1/cluster", interfaces.ResourceCluster},
		{"/api/v1/cluster/nodes", interfaces.ResourceCluster},
		{"/api/v1/users", interfaces.ResourceUsers},
		{"/api/v1/users/admin", interfaces.ResourceUsers},
		{"/api/v1/roles", interfaces.ResourceRoles},
		{"/api/v1/roles/admin", interfaces.ResourceRoles},
		{"/api/v1/timeseries", interfaces.ResourceTimeseries},
		{"/api/v1/timeseries/metrics/cpu", interfaces.ResourceTimeseries},
		{"/api/v1/vector", interfaces.ResourceVector},
		{"/api/v1/vector/collections/docs", interfaces.ResourceVector},
		{"/api/v1/watch", interfaces.ResourceWatch},
		{"/api/v1/watch/keys/foo", interfaces.ResourceWatch},
		{"/api/v1/transactions", interfaces.ResourceTransactions},
		{"/api/v1/transactions/123", interfaces.ResourceTransactions},
		{"/api/v1/backups", interfaces.ResourceBackups},
		{"/api/v1/backups/latest", interfaces.ResourceBackups},
		{"/api/v1/unknown", "unknown"},
		{"/api/v2/keys", "unknown"},
		{"/other/path", "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			result := extractAuthzResource(tc.path)
			if result != tc.expected {
				t.Errorf("extractAuthzResource(%s) = %s, want %s", tc.path, result, tc.expected)
			}
		})
	}
}

// TestAuthzMiddleware_HTTPMethodMapping tests HTTP method to action mapping.
func TestAuthzMiddleware_HTTPMethodMapping(t *testing.T) {
	tests := []struct {
		method   string
		expected string
	}{
		{http.MethodGet, interfaces.ActionRead},
		{http.MethodHead, interfaces.ActionRead},
		{http.MethodOptions, interfaces.ActionRead},
		{http.MethodPost, interfaces.ActionWrite},
		{http.MethodPut, interfaces.ActionWrite},
		{http.MethodPatch, interfaces.ActionWrite},
		{http.MethodDelete, interfaces.ActionDelete},
		{"UNKNOWN", interfaces.ActionRead},
		{"get", interfaces.ActionRead},   // lowercase
		{"POST", interfaces.ActionWrite}, // uppercase
	}

	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			result := mapHTTPMethodToAction(tc.method)
			if result != tc.expected {
				t.Errorf("mapHTTPMethodToAction(%s) = %s, want %s", tc.method, result, tc.expected)
			}
		})
	}
}

// TestAuthzMiddleware_ContextBuilding tests authorization context building.
func TestAuthzMiddleware_ContextBuilding(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/keys/test?foo=bar", nil)
	req.Header.Set("User-Agent", "TestAgent/1.0")
	req.Header.Set("X-Forwarded-For", "10.0.0.1")

	ctx := buildAuthzContext(req)

	if ctx["http_method"] != "POST" {
		t.Errorf("Expected http_method 'POST', got '%v'", ctx["http_method"])
	}
	if ctx["request_path"] != "/api/v1/keys/test" {
		t.Errorf("Expected request_path '/api/v1/keys/test', got '%v'", ctx["request_path"])
	}
	if ctx["user_agent"] != "TestAgent/1.0" {
		t.Errorf("Expected user_agent 'TestAgent/1.0', got '%v'", ctx["user_agent"])
	}
	// X-Forwarded-For should be used for IP
	ipAddr, ok := ctx["ip_address"].(string)
	if !ok || ipAddr == "" {
		t.Errorf("Expected ip_address to be set, got '%v'", ctx["ip_address"])
	}
}

// TestAuthzMiddleware_ExtractBearerToken tests token extraction from headers.
func TestAuthzMiddleware_ExtractBearerToken(t *testing.T) {
	tests := []struct {
		name        string
		authHeader  string
		expectToken string
		expectError bool
	}{
		{"valid bearer token", "Bearer my-token-123", "my-token-123", false},
		{"bearer lowercase", "bearer my-token-123", "my-token-123", false},
		{"BEARER uppercase", "BEARER my-token-123", "my-token-123", false},
		{"token with spaces trimmed", "Bearer  my-token-123 ", "my-token-123", false},
		{"missing header", "", "", true},
		{"no bearer prefix", "my-token-123", "", true},
		{"wrong scheme basic", "Basic dXNlcjpwYXNz", "", true},
		{"bearer only", "Bearer", "", true},
		{"empty token", "Bearer ", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/keys", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}

			token, err := extractBearerToken(req)

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error, got token '%s'", token)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if token != tc.expectToken {
					t.Errorf("Expected token '%s', got '%s'", tc.expectToken, token)
				}
			}
		})
	}
}

// TestAuthzMiddleware_PublicEndpointChecking tests public endpoint detection.
func TestAuthzMiddleware_PublicEndpointChecking(t *testing.T) {
	tests := []struct {
		path     string
		isPublic bool
	}{
		{"/api/v1/auth/login", true},
		{"/api/v1/auth/login/", true},
		{"/api/v1/health", true},
		{"/api/v1/health/", true},
		{"/api/v1/health/liveness", true},
		{"/api/v1/health/readiness", true},
		{"/metrics", true},
		{"/metrics/", true},
		{"/api/v1/keys", false},
		{"/api/v1/secrets", false},
		{"/api/v1/auth", false}, // not login specifically
		{"/api/v2/health", false},
		{"/healthz", false}, // different path
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			result := isAuthzPublicEndpoint(tc.path)
			if result != tc.isPublic {
				t.Errorf("isAuthzPublicEndpoint(%s) = %v, want %v", tc.path, result, tc.isPublic)
			}
		})
	}
}

// TestAuthzMiddleware_ErrorTypes tests custom error type messages.
func TestAuthzMiddleware_ErrorTypes(t *testing.T) {
	t.Run("MissingAuthHeaderError", func(t *testing.T) {
		err := &MissingAuthHeaderError{}
		if err.Error() == "" {
			t.Error("Expected non-empty error message")
		}
	})

	t.Run("InvalidAuthHeaderError", func(t *testing.T) {
		err := &InvalidAuthHeaderError{}
		if err.Error() == "" {
			t.Error("Expected non-empty error message")
		}
	})

	t.Run("EmptyTokenError", func(t *testing.T) {
		err := &EmptyTokenError{}
		if err.Error() == "" {
			t.Error("Expected non-empty error message")
		}
	})

	t.Run("AuthorizationDeniedError with reason", func(t *testing.T) {
		err := &AuthorizationDeniedError{Reason: "insufficient permissions"}
		if err.Error() != "authorization denied: insufficient permissions" {
			t.Errorf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("AuthorizationDeniedError without reason", func(t *testing.T) {
		err := &AuthorizationDeniedError{}
		if err.Error() != "authorization denied" {
			t.Errorf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("UnauthorizedError with message", func(t *testing.T) {
		err := &UnauthorizedError{Message: "token expired"}
		if err.Error() != "token expired" {
			t.Errorf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("UnauthorizedError without message", func(t *testing.T) {
		err := &UnauthorizedError{}
		if err.Error() != "unauthorized" {
			t.Errorf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("ForbiddenError with reason", func(t *testing.T) {
		err := &ForbiddenError{Reason: "admin only"}
		if err.Error() != "access denied: admin only" {
			t.Errorf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("ForbiddenError without reason", func(t *testing.T) {
		err := &ForbiddenError{}
		if err.Error() != "access denied" {
			t.Errorf("Unexpected error message: %s", err.Error())
		}
	})
}

// TestAuthzMiddleware_GetUsername tests username extraction from context.
func TestAuthzMiddleware_GetUsername(t *testing.T) {
	t.Run("username present", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), usernameKey, "testuser")
		username := GetUsername(ctx)
		if username != "testuser" {
			t.Errorf("Expected 'testuser', got '%s'", username)
		}
	})

	t.Run("username absent", func(t *testing.T) {
		ctx := context.Background()
		username := GetUsername(ctx)
		if username != "" {
			t.Errorf("Expected empty string, got '%s'", username)
		}
	})

	t.Run("wrong type in context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), usernameKey, 12345)
		username := GetUsername(ctx)
		if username != "" {
			t.Errorf("Expected empty string for wrong type, got '%s'", username)
		}
	})
}

// TestAuthzMiddleware_SubjectConstruction tests that the subject is correctly built.
func TestAuthzMiddleware_SubjectConstruction(t *testing.T) {
	validator := &mockTokenValidator{
		result: &auth.TokenValidationResult{
			Valid:     true,
			UserID:    "user-456",
			Username:  "alice",
			Roles:     []string{"developer", "tester"},
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}
	authzSvc := &mockAuthzService{
		decision: &interfaces.AuthorizationDecision{
			Allowed: true,
		},
	}
	middleware := createTestMiddleware(validator, authzSvc)

	handler := &testHandler{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/db-password", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rr := httptest.NewRecorder()

	middleware.Authorize(handler).ServeHTTP(rr, req)

	// Verify subject construction
	if authzSvc.lastRequest == nil {
		t.Fatal("Expected lastRequest to be set")
	}

	subject := authzSvc.lastRequest.Subject
	if subject.ID != "user-456" {
		t.Errorf("Expected subject ID 'user-456', got '%s'", subject.ID)
	}
	if subject.Username != "alice" {
		t.Errorf("Expected username 'alice', got '%s'", subject.Username)
	}
	if len(subject.Roles) != 2 || subject.Roles[0] != "developer" {
		t.Errorf("Expected roles [developer, tester], got %v", subject.Roles)
	}
	if !subject.Authenticated {
		t.Error("Expected subject to be authenticated")
	}
	if subject.Attributes == nil {
		t.Error("Expected attributes map to be initialized")
	}
}

// TestAuthzMiddleware_NilLogger tests middleware works without logger.
func TestAuthzMiddleware_NilLogger(t *testing.T) {
	validator := &mockTokenValidator{
		result: &auth.TokenValidationResult{
			Valid:     true,
			UserID:    "user-123",
			Username:  "testuser",
			Roles:     []string{"admin"},
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}
	authzSvc := &mockAuthzService{
		decision: &interfaces.AuthorizationDecision{
			Allowed: true,
		},
	}

	// Create middleware without logger
	middleware := NewAuthzMiddleware(&AuthzMiddlewareConfig{
		Logger:         nil,
		TokenValidator: validator,
		AuthzService:   authzSvc,
	})

	handler := &testHandler{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rr := httptest.NewRecorder()

	// Should not panic even without logger
	middleware.Authorize(handler).ServeHTTP(rr, req)

	if !handler.called {
		t.Error("Expected handler to be called")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

// TestAuthzMiddleware_AllResourcePaths tests all resource paths end-to-end.
func TestAuthzMiddleware_AllResourcePaths(t *testing.T) {
	resourceTests := []struct {
		path             string
		expectedResource string
	}{
		{"/api/v1/keys/mykey", interfaces.ResourceKeys},
		{"/api/v1/secrets/mysecret", interfaces.ResourceSecrets},
		{"/api/v1/kms/encrypt", interfaces.ResourceKMS},
		{"/api/v1/crypto/sign", interfaces.ResourceCrypto},
		{"/api/v1/seal/unseal", interfaces.ResourceSeal},
		{"/api/v1/cluster/status", interfaces.ResourceCluster},
		{"/api/v1/users/admin", interfaces.ResourceUsers},
		{"/api/v1/roles/superuser", interfaces.ResourceRoles},
		{"/api/v1/timeseries/query", interfaces.ResourceTimeseries},
		{"/api/v1/vector/search", interfaces.ResourceVector},
		{"/api/v1/watch/keys", interfaces.ResourceWatch},
		{"/api/v1/transactions/begin", interfaces.ResourceTransactions},
		{"/api/v1/backups/create", interfaces.ResourceBackups},
	}

	for _, tc := range resourceTests {
		t.Run(tc.path, func(t *testing.T) {
			validator := &mockTokenValidator{
				result: &auth.TokenValidationResult{
					Valid:     true,
					UserID:    "user-123",
					Username:  "testuser",
					Roles:     []string{"admin"},
					ExpiresAt: time.Now().Add(time.Hour),
				},
			}
			authzSvc := &mockAuthzService{
				decision: &interfaces.AuthorizationDecision{
					Allowed: true,
				},
			}
			middleware := createTestMiddleware(validator, authzSvc)

			handler := &testHandler{}

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", "Bearer valid-token")
			rr := httptest.NewRecorder()

			middleware.Authorize(handler).ServeHTTP(rr, req)

			if authzSvc.lastRequest == nil {
				t.Fatal("Expected lastRequest to be set")
			}
			if authzSvc.lastRequest.Resource != tc.expectedResource {
				t.Errorf("Expected resource '%s', got '%s'", tc.expectedResource, authzSvc.lastRequest.Resource)
			}
		})
	}
}

// TestAuthzMiddleware_AllHTTPMethods tests all HTTP methods end-to-end.
func TestAuthzMiddleware_AllHTTPMethods(t *testing.T) {
	methodTests := []struct {
		method         string
		expectedAction string
	}{
		{http.MethodGet, interfaces.ActionRead},
		{http.MethodHead, interfaces.ActionRead},
		{http.MethodOptions, interfaces.ActionRead},
		{http.MethodPost, interfaces.ActionWrite},
		{http.MethodPut, interfaces.ActionWrite},
		{http.MethodPatch, interfaces.ActionWrite},
		{http.MethodDelete, interfaces.ActionDelete},
	}

	for _, tc := range methodTests {
		t.Run(tc.method, func(t *testing.T) {
			validator := &mockTokenValidator{
				result: &auth.TokenValidationResult{
					Valid:     true,
					UserID:    "user-123",
					Username:  "testuser",
					Roles:     []string{"admin"},
					ExpiresAt: time.Now().Add(time.Hour),
				},
			}
			authzSvc := &mockAuthzService{
				decision: &interfaces.AuthorizationDecision{
					Allowed: true,
				},
			}
			middleware := createTestMiddleware(validator, authzSvc)

			handler := &testHandler{}

			req := httptest.NewRequest(tc.method, "/api/v1/keys/test", nil)
			req.Header.Set("Authorization", "Bearer valid-token")
			rr := httptest.NewRecorder()

			middleware.Authorize(handler).ServeHTTP(rr, req)

			if authzSvc.lastRequest == nil {
				t.Fatal("Expected lastRequest to be set")
			}
			if authzSvc.lastRequest.Action != tc.expectedAction {
				t.Errorf("Expected action '%s', got '%s'", tc.expectedAction, authzSvc.lastRequest.Action)
			}
		})
	}
}

// TestAuthzMiddleware_ResponseBody tests error response body format.
func TestAuthzMiddleware_ResponseBody(t *testing.T) {
	t.Run("unauthorized response has correct structure", func(t *testing.T) {
		validator := &mockTokenValidator{}
		authzSvc := &mockAuthzService{}
		middleware := createTestMiddleware(validator, authzSvc)

		handler := &testHandler{}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
		// No Authorization header
		rr := httptest.NewRecorder()

		middleware.Authorize(handler).ServeHTTP(rr, req)

		var errResp ErrorResponse
		if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
			t.Fatalf("Failed to decode error response: %v", err)
		}

		if errResp.Error == "" {
			t.Error("Expected error field to be set")
		}
		if errResp.Timestamp.IsZero() {
			t.Error("Expected timestamp to be set")
		}
	})

	t.Run("forbidden response has correct structure", func(t *testing.T) {
		validator := &mockTokenValidator{
			result: &auth.TokenValidationResult{
				Valid:     true,
				UserID:    "user-123",
				Username:  "testuser",
				Roles:     []string{"reader"},
				ExpiresAt: time.Now().Add(time.Hour),
			},
		}
		authzSvc := &mockAuthzService{
			decision: &interfaces.AuthorizationDecision{
				Allowed: false,
				Reason:  "write permission required",
			},
		}
		middleware := createTestMiddleware(validator, authzSvc)

		handler := &testHandler{}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/keys/test", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		rr := httptest.NewRecorder()

		middleware.Authorize(handler).ServeHTTP(rr, req)

		var errResp ErrorResponse
		if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
			t.Fatalf("Failed to decode error response: %v", err)
		}

		if errResp.Error == "" {
			t.Error("Expected error field to be set")
		}
	})
}

// TestAuthzMiddleware_LoggingCoverage tests logging paths without logger.
func TestAuthzMiddleware_LoggingCoverage(t *testing.T) {
	t.Run("log denied without logger", func(t *testing.T) {
		middleware := &AuthzMiddleware{
			logger: nil, // No logger
		}
		// Should not panic
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		middleware.logDenied(req, &interfaces.AuthorizationRequest{}, &interfaces.AuthorizationDecision{})
	})

	t.Run("log allowed without logger", func(t *testing.T) {
		middleware := &AuthzMiddleware{
			logger: nil, // No logger
		}
		// Should not panic
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		middleware.logAllowed(req, &interfaces.AuthorizationRequest{}, &interfaces.AuthorizationDecision{})
	})

	t.Run("log authz error without logger", func(t *testing.T) {
		middleware := &AuthzMiddleware{
			logger: nil, // No logger
		}
		// Should not panic
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		middleware.logAuthzError(req, &interfaces.AuthorizationRequest{}, errors.New("test error"))
	})
}

// TestAuthzMiddleware_ContextBuildingWithRequestID tests context building with request ID.
func TestAuthzMiddleware_ContextBuildingWithRequestID(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/keys/test", nil)
	req.Header.Set("User-Agent", "TestAgent/1.0")

	// Add request ID to context
	ctx := context.WithValue(req.Context(), requestIDKey, "test-request-id-123")
	req = req.WithContext(ctx)

	authzCtx := buildAuthzContext(req)

	if authzCtx["request_id"] != "test-request-id-123" {
		t.Errorf("Expected request_id 'test-request-id-123', got '%v'", authzCtx["request_id"])
	}
}

// TestAuthzMiddleware_ContextBuildingNoUserAgent tests context building without user agent.
func TestAuthzMiddleware_ContextBuildingNoUserAgent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)
	// Don't set User-Agent header

	authzCtx := buildAuthzContext(req)

	// user_agent should not be in context if not set
	if _, exists := authzCtx["user_agent"]; exists {
		t.Error("Expected user_agent to not be set when header is missing")
	}
}

// TestAuthzMiddleware_LoggingWithLogger tests logging paths with a logger.
func TestAuthzMiddleware_LoggingWithLogger(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Output: "stdout", Format: "text"})

	t.Run("log denied with logger", func(t *testing.T) {
		middleware := &AuthzMiddleware{
			logger: log,
		}
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		middleware.logDenied(req, &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{ID: "user-123", Username: "testuser"},
		}, &interfaces.AuthorizationDecision{
			Reason:        "access denied",
			Provider:      "rbac",
			MatchedPolicy: "default",
			DecisionID:    "dec-123",
		})
	})

	t.Run("log allowed with logger", func(t *testing.T) {
		middleware := &AuthzMiddleware{
			logger: log,
		}
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		middleware.logAllowed(req, &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{ID: "user-123", Username: "testuser"},
		}, &interfaces.AuthorizationDecision{
			Provider:      "rbac",
			MatchedPolicy: "admin",
			DecisionID:    "dec-456",
		})
	})

	t.Run("log authz error with logger", func(t *testing.T) {
		middleware := &AuthzMiddleware{
			logger: log,
		}
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		middleware.logAuthzError(req, &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{ID: "user-123"},
		}, errors.New("test error"))
	})

	t.Run("log unauthorized with logger", func(t *testing.T) {
		middleware := &AuthzMiddleware{
			logger: log,
		}
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		middleware.logUnauthorized(req, "invalid token")
	})
}
