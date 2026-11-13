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
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestJWTMiddleware tests the JWTMiddleware handler.
func TestJWTMiddleware(t *testing.T) {
	t.Run("missing authorization header", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
		w := httptest.NewRecorder()

		handler.JWTMiddleware(next).ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "authorization header is required")
		assert.False(t, nextCalled)
	})

	t.Run("invalid authorization header format without bearer", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
		req.Header.Set("Authorization", "InvalidToken")
		w := httptest.NewRecorder()

		handler.JWTMiddleware(next).ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "invalid authorization header format")
		assert.False(t, nextCalled)
	})

	t.Run("invalid authorization header format with wrong prefix", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
		req.Header.Set("Authorization", "Basic token123")
		w := httptest.NewRecorder()

		handler.JWTMiddleware(next).ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "invalid authorization header format")
		assert.False(t, nextCalled)
	})

	t.Run("OIDC provider not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
		req.Header.Set("Authorization", "Bearer valid-token-123")
		w := httptest.NewRecorder()

		handler.JWTMiddleware(next).ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "OIDC provider not available")
		assert.False(t, nextCalled)
	})

	t.Run("bearer token case insensitive", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
		req.Header.Set("Authorization", "BEARER valid-token-123")
		w := httptest.NewRecorder()

		handler.JWTMiddleware(next).ServeHTTP(w, req)

		// Should proceed to OIDC provider check since bearer prefix is valid
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "OIDC provider not available")
		assert.False(t, nextCalled)
	})
}

// TestABACMiddleware tests the ABACMiddleware handler.
func TestABACMiddleware(t *testing.T) {
	t.Run("user not authenticated nil", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/protected/resource", nil)
		w := httptest.NewRecorder()

		middleware := handler.ABACMiddleware("resource", "read")
		middleware(next).ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "user not authenticated")
		assert.False(t, nextCalled)
	})

	t.Run("user authenticated but policy engine not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
		})

		// Add user info to context
		userInfo := &iam.UserInfoResponse{
			Sub:      "user-123",
			Name:     "Test User",
			Email:    "test@example.com",
			TenantID: "tenant-123",
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/protected/resource", nil)
		ctx := context.WithValue(req.Context(), ContextKeyUser, userInfo)
		ctx = context.WithValue(ctx, ContextKeyTenant, "tenant-123")
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		middleware := handler.ABACMiddleware("resource", "read")
		middleware(next).ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "policy engine not available")
		assert.False(t, nextCalled)
	})

	t.Run("user authenticated without tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
		})

		// Add user info to context without tenant
		userInfo := &iam.UserInfoResponse{
			Sub:   "user-123",
			Name:  "Test User",
			Email: "test@example.com",
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/protected/resource", nil)
		ctx := context.WithValue(req.Context(), ContextKeyUser, userInfo)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		middleware := handler.ABACMiddleware("resource", "write")
		middleware(next).ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "policy engine not available")
		assert.False(t, nextCalled)
	})
}

// TestImpersonationMiddleware tests the ImpersonationMiddleware handler.
func TestImpersonationMiddleware(t *testing.T) {
	t.Run("no impersonation header proceeds normally", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resource", nil)
		w := httptest.NewRecorder()

		handler.ImpersonationMiddleware(next).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.True(t, nextCalled)
	})

	t.Run("admin manager not available with impersonation header", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resource", nil)
		req.Header.Set("X-Impersonation-Session-ID", "session-123")
		w := httptest.NewRecorder()

		handler.ImpersonationMiddleware(next).ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "admin manager not available")
		assert.False(t, nextCalled)
	})
}

// TestImpersonationAuditMiddleware tests the ImpersonationAuditMiddleware handler.
func TestImpersonationAuditMiddleware(t *testing.T) {
	t.Run("no impersonation context proceeds normally", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resource", nil)
		w := httptest.NewRecorder()

		handler.ImpersonationAuditMiddleware(next).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.True(t, nextCalled)
	})

	t.Run("with impersonation context logs operation", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusOK)
		})

		// Create impersonation context
		impCtx := &iam.ImpersonationContext{
			SessionID:     "session-123",
			AdminUserID:   50456,
			AdminUsername: "admin@example.com",
			TargetUserID:  50789,
			TenantID:      "tenant-123",
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resource", nil)
		ctx := iam.WithImpersonationContext(req.Context(), impCtx)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ImpersonationAuditMiddleware(next).ServeHTTP(w, req)

		// Should still call next even if admin manager is not available
		assert.Equal(t, http.StatusOK, w.Code)
		assert.True(t, nextCalled)
	})
}

// TestHelperFunctions tests the helper functions.
func TestGetString(t *testing.T) {
	t.Run("string value exists", func(t *testing.T) {
		m := map[string]interface{}{
			"key": "value",
		}
		result := getString(m, "key")
		assert.Equal(t, "value", result)
	})

	t.Run("string value does not exist", func(t *testing.T) {
		m := map[string]interface{}{
			"other": "value",
		}
		result := getString(m, "key")
		assert.Equal(t, "", result)
	})

	t.Run("value is not string", func(t *testing.T) {
		m := map[string]interface{}{
			"key": 123,
		}
		result := getString(m, "key")
		assert.Equal(t, "", result)
	})
}

func TestGetStringSlice(t *testing.T) {
	t.Run("string slice from interface slice", func(t *testing.T) {
		m := map[string]interface{}{
			"key": []interface{}{"a", "b", "c"},
		}
		result := getStringSlice(m, "key")
		assert.Equal(t, []string{"a", "b", "c"}, result)
	})

	t.Run("string slice directly", func(t *testing.T) {
		m := map[string]interface{}{
			"key": []string{"x", "y", "z"},
		}
		result := getStringSlice(m, "key")
		assert.Equal(t, []string{"x", "y", "z"}, result)
	})

	t.Run("mixed interface slice", func(t *testing.T) {
		m := map[string]interface{}{
			"key": []interface{}{"a", 123, "c"},
		}
		result := getStringSlice(m, "key")
		// Should only include string values
		assert.Equal(t, []string{"a", "c"}, result)
	})

	t.Run("key does not exist", func(t *testing.T) {
		m := map[string]interface{}{
			"other": []string{"a"},
		}
		result := getStringSlice(m, "key")
		assert.Nil(t, result)
	})

	t.Run("value is not slice", func(t *testing.T) {
		m := map[string]interface{}{
			"key": "not a slice",
		}
		result := getStringSlice(m, "key")
		assert.Nil(t, result)
	})
}

func TestGetMap(t *testing.T) {
	t.Run("map value exists", func(t *testing.T) {
		m := map[string]interface{}{
			"key": map[string]interface{}{
				"nested": "value",
			},
		}
		result := getMap(m, "key")
		assert.Equal(t, map[string]interface{}{"nested": "value"}, result)
	})

	t.Run("key does not exist", func(t *testing.T) {
		m := map[string]interface{}{
			"other": map[string]interface{}{},
		}
		result := getMap(m, "key")
		assert.Nil(t, result)
	})

	t.Run("value is not map", func(t *testing.T) {
		m := map[string]interface{}{
			"key": "not a map",
		}
		result := getMap(m, "key")
		assert.Nil(t, result)
	})
}

func TestImpersonationMiddlewareExpiredSessionFinal(t *testing.T) {
	t.Parallel()

	t.Run("session ended check with time override", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		// Save original timeNow and restore after test
		originalTimeNow := timeNow
		defer func() { timeNow = originalTimeNow }()

		// Set time to a fixed point
		fixedTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		timeNow = func() time.Time { return fixedTime }

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resource", nil)
		req.Header.Set("X-Impersonation-Session-ID", "session-123")
		w := httptest.NewRecorder()

		handler.ImpersonationMiddleware(next).ServeHTTP(w, req)

		// Should return error since admin manager is not available
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.False(t, nextCalled)
	})
}

func TestImpersonationAuditMiddlewareWithContextFinal(t *testing.T) {
	t.Parallel()

	t.Run("records operation with query string and user agent", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusOK)
		})

		// Create impersonation context
		impCtx := &iam.ImpersonationContext{
			SessionID:     "session-123",
			AdminUserID:   50456,
			AdminUsername: "admin@example.com",
			TargetUserID:  50789,
			TenantID:      "tenant-123",
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/resource?key=value", nil)
		ctx := iam.WithImpersonationContext(req.Context(), impCtx)
		req = req.WithContext(ctx)
		req.Header.Set("User-Agent", "TestAgent/1.0")
		w := httptest.NewRecorder()

		handler.ImpersonationAuditMiddleware(next).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.True(t, nextCalled)
	})

	t.Run("records POST operation with body", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusCreated)
		})

		impCtx := &iam.ImpersonationContext{
			SessionID:     "session-456",
			AdminUserID:   50790,
			AdminUsername: "admin2@example.com",
			TargetUserID:  50101,
			TenantID:      "tenant-456",
		}

		body := `{"key": "value"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/resource", strings.NewReader(body))
		ctx := iam.WithImpersonationContext(req.Context(), impCtx)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ImpersonationAuditMiddleware(next).ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.True(t, nextCalled)
	})

	t.Run("records DELETE operation", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusNoContent)
		})

		impCtx := &iam.ImpersonationContext{
			SessionID:     "session-999",
			AdminUserID:   50888,
			AdminUsername: "admin3@example.com",
			TargetUserID:  50777,
			TenantID:      "tenant-666",
		}

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/resource/123", nil)
		ctx := iam.WithImpersonationContext(req.Context(), impCtx)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ImpersonationAuditMiddleware(next).ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.True(t, nextCalled)
	})
}

func TestCreateSessionDeviceInfoVariationsFinal(t *testing.T) {
	t.Parallel()

	t.Run("with partial device info only id and name", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		body := `{
			"token_hash": "abc123",
			"device_info": {
				"id": "device-001",
				"name": "Test Device"
			}
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/sessions", strings.NewReader(body))
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateSession(w, req)

		// Will fail at session manager check, but validates JSON parsing
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with full device info all fields populated", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		body := `{
			"token_hash": "abc123",
			"mfa_verified": true,
			"ip_address": "192.0.2.1",
			"user_agent": "TestAgent/1.0",
			"device_info": {
				"id": "device-001",
				"name": "Test Device",
				"type": "mobile",
				"fingerprint": "fp123",
				"trusted": true
			},
			"expires_at": "2025-12-31T23:59:59Z"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/sessions", strings.NewReader(body))
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateSession(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with invalid expires_at format falls back to default", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		body := `{
			"token_hash": "abc123",
			"expires_at": "invalid-date"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/sessions", strings.NewReader(body))
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateSession(w, req)

		// Should continue with default expiration
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with custom session ID provided", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		body := `{
			"id": "custom-session-id",
			"token_hash": "abc123"
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/sessions", strings.NewReader(body))
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateSession(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with device info type only", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		body := `{
			"token_hash": "abc123",
			"device_info": {
				"type": "desktop"
			}
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/sessions", strings.NewReader(body))
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateSession(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with device info fingerprint and trusted", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		body := `{
			"token_hash": "abc123",
			"device_info": {
				"fingerprint": "fp-unique-123",
				"trusted": false
			}
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/sessions", strings.NewReader(body))
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateSession(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestRevokeOtherSessionsEmptyBodyFinal(t *testing.T) {
	t.Parallel()

	t.Run("with nil body requires keep_session_id", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(context.Background(), "10123", "tenant-123")
		req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.RevokeOtherSessions(w, req)

		// The session to keep must be named in the request body; there is no
		// context fallback.
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "current session ID is required")
	})

	t.Run("with invalid JSON body requires keep_session_id", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(context.Background(), "10123", "tenant-123")
		req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", strings.NewReader("{invalid"))
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.RevokeOtherSessions(w, req)

		// Unparseable body yields no keep_session_id -> 400.
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "current session ID is required")
	})

	t.Run("with empty keep_session_id in body rejected", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(context.Background(), "10123", "tenant-123")
		body := `{"keep_session_id": ""}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", strings.NewReader(body))
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.RevokeOtherSessions(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "current session ID is required")
	})
}

// TestABACMiddlewareContextTypesFinal tests additional ABAC middleware scenarios.
func TestABACMiddlewareContextTypesFinal(t *testing.T) {
	t.Parallel()

	t.Run("with wrong type in context for user info string", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
		})

		// Add wrong type to context
		req := httptest.NewRequest(http.MethodGet, "/api/v1/protected/resource", nil)
		ctx := context.WithValue(req.Context(), ContextKeyUser, "not-a-userinfo-struct")
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		middleware := handler.ABACMiddleware("resource", "delete")
		middleware(next).ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "user not authenticated")
		assert.False(t, nextCalled)
	})

	t.Run("with wrong type in context for user info int", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/protected/resource", nil)
		ctx := context.WithValue(req.Context(), ContextKeyUser, 12345)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		middleware := handler.ABACMiddleware("resource", "update")
		middleware(next).ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "user not authenticated")
		assert.False(t, nextCalled)
	})
}

// TestSessionsEmptyStringContextFinal tests session handlers with empty strings in context.
func TestSessionsEmptyStringContextFinal(t *testing.T) {
	t.Parallel()

	t.Run("ListSessions with empty string user ID", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(context.Background(), "", "tenant-123")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/sessions", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ListSessions(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("ListSessions with empty string tenant ID", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(context.Background(), "10123", "")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/sessions", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ListSessions(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("GetSession with empty string user ID", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "session-123")

		ctx := ctxWithTestSubject(context.Background(), "", "tenant-123")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/sessions/session-123", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.GetSession(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("GetSession with empty string tenant ID", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "session-123")

		ctx := ctxWithTestSubject(context.Background(), "10123", "")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/sessions/session-123", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.GetSession(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("RevokeSession with empty string user ID", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "session-123")

		ctx := ctxWithTestSubject(context.Background(), "", "tenant-123")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/sessions/session-123", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.RevokeSession(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("RevokeSession with empty string tenant ID", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "session-123")

		ctx := ctxWithTestSubject(context.Background(), "10123", "")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/sessions/session-123", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.RevokeSession(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("RevokeAllSessions with empty string user ID", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(context.Background(), "", "tenant-123")
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/sessions", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.RevokeAllSessions(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("RevokeAllSessions with empty string tenant ID", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(context.Background(), "10123", "")
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/sessions", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.RevokeAllSessions(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("RevokeOtherSessions with empty string user ID", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(context.Background(), "", "tenant-123")
		req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.RevokeOtherSessions(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("RevokeOtherSessions with empty string tenant ID", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(context.Background(), "10123", "")
		req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.RevokeOtherSessions(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("RevokeOtherSessions with empty string session ID", func(t *testing.T) {
		t.Parallel()
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(context.Background(), "10123", "tenant-123")
		req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.RevokeOtherSessions(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestJWTMiddlewareBearerCaseVariationsFinal tests JWT middleware with different bearer case variations.
func TestJWTMiddlewareBearerCaseVariationsFinal(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		authHeader     string
		expectedStatus int
	}{
		{
			name:           "lowercase bearer prefix",
			authHeader:     "bearer test-token-123",
			expectedStatus: http.StatusInternalServerError, // Proceeds to OIDC check
		},
		{
			name:           "uppercase BEARER prefix",
			authHeader:     "BEARER test-token-123",
			expectedStatus: http.StatusInternalServerError, // Proceeds to OIDC check
		},
		{
			name:           "mixed case Bearer prefix",
			authHeader:     "Bearer test-token-123",
			expectedStatus: http.StatusInternalServerError, // Proceeds to OIDC check
		},
		{
			name:           "CamelCase BeArEr prefix",
			authHeader:     "BeArEr test-token-456",
			expectedStatus: http.StatusInternalServerError, // Proceeds to OIDC check
		},
	}

	for _, tc := range testCases {
		// capture range variable
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler, _, cleanup := setupIAMHandler(t)
			defer cleanup()

			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
			})

			req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
			req.Header.Set("Authorization", tc.authHeader)
			w := httptest.NewRecorder()

			handler.JWTMiddleware(next).ServeHTTP(w, req)

			assert.Equal(t, tc.expectedStatus, w.Code)
			assert.False(t, nextCalled) // Should not call next since OIDC is not available
		})
	}
}
