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
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// TestCreateSession tests the CreateSession handler.
func TestCreateSession(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "10123")

		req := systemRequest(http.MethodPost, "/api/v1/tenants//users/user-123/sessions", strings.NewReader(`{}`))
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateSession(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "")

		req := systemRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users//sessions", strings.NewReader(`{}`))
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateSession(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		req := systemRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/sessions", strings.NewReader("{invalid"))
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateSession(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid request body")
	})

	t.Run("missing token_hash", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		body := `{"token_hash":""}`
		req := systemRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/sessions", strings.NewReader(body))
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateSession(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "token_hash is required")
	})

	t.Run("session manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		body := `{"token_hash":"abc123"}`
		req := systemRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/sessions", strings.NewReader(body))
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateSession(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "session manager not available")
	})
}

// TestListUserSessions tests the ListUserSessions handler.
func TestListUserSessions(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "10123")

		req := systemRequest(http.MethodGet, "/api/v1/tenants//users/user-123/sessions", nil)
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.ListUserSessions(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "")

		req := systemRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users//sessions", nil)
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.ListUserSessions(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("session manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "10123")

		req := systemRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users/user-123/sessions", nil)
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.ListUserSessions(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "session manager not available")
	})
}

// TestRevokeAllUserSessions tests the path-scoped admin revoke-all handler —
// the REST projection of the parameterized SDK Session/RevokeAll verb.
func TestRevokeAllUserSessions(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := withChiParams(systemRequest(http.MethodDelete, "/api/v1/tenants//users/user-123/sessions", nil),
			map[string]string{"tenant_id": "", "user_id": "10123"})
		w := httptest.NewRecorder()

		handler.RevokeAllUserSessions(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := withChiParams(systemRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/users//sessions", nil),
			map[string]string{"tenant_id": "tenant-123", "user_id": ""})
		w := httptest.NewRecorder()

		handler.RevokeAllUserSessions(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("session manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := withChiParams(systemRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/users/user-123/sessions", nil),
			map[string]string{"tenant_id": "tenant-123", "user_id": "10123"})
		w := httptest.NewRecorder()

		handler.RevokeAllUserSessions(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "session manager not available")
	})

	t.Run("session manager not initialized", func(t *testing.T) {
		svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: nil}
		handler, cleanup := setupHandlerWithService(t, svc)
		defer cleanup()

		req := withChiParams(systemRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/users/user-123/sessions", nil),
			map[string]string{"tenant_id": "tenant-123", "user_id": "10123"})
		w := httptest.NewRecorder()

		handler.RevokeAllUserSessions(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "session manager not initialized")
	})

	t.Run("revokes the path-scoped target user", func(t *testing.T) {
		mgr := &mockSessionManager{}
		svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
		handler, cleanup := setupHandlerWithService(t, svc)
		defer cleanup()

		req := withChiParams(systemRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/users/user-123/sessions", nil),
			map[string]string{"tenant_id": "tenant-123", "user_id": "10123"})
		w := httptest.NewRecorder()

		handler.RevokeAllUserSessions(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Equal(t, uint64(10123), mgr.revokedUserID)
		assert.Equal(t, "tenant-123", mgr.revokedTenantID)
	})

	t.Run("revocation error is surfaced", func(t *testing.T) {
		mgr := &mockSessionManager{revokeErr: errors.New("backend down")}
		svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
		handler, cleanup := setupHandlerWithService(t, svc)
		defer cleanup()

		req := withChiParams(systemRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/users/user-123/sessions", nil),
			map[string]string{"tenant_id": "tenant-123", "user_id": "10123"})
		w := httptest.NewRecorder()

		handler.RevokeAllUserSessions(w, req)

		assert.GreaterOrEqual(t, w.Code, http.StatusBadRequest)
	})
}

// TestListUserSessionsLimit tests the optional limit query parameter on the
// path-scoped admin listing (gRPC ListSessions limit parity).
func TestListUserSessionsLimit(t *testing.T) {
	threeSessions := func() []*iam.SessionExt {
		return []*iam.SessionExt{
			{ID: "s1", UserID: 10123, TenantID: "tenant-123"},
			{ID: "s2", UserID: 10123, TenantID: "tenant-123"},
			{ID: "s3", UserID: 10123, TenantID: "tenant-123"},
		}
	}

	listReq := func(target string) *http.Request {
		return withChiParams(systemRequest(http.MethodGet, target, nil),
			map[string]string{"tenant_id": "tenant-123", "user_id": "10123"})
	}

	countSessions := func(t *testing.T, body []byte) int {
		t.Helper()
		var resp struct {
			Sessions []map[string]interface{} `json:"sessions"`
		}
		require.NoError(t, json.Unmarshal(body, &resp))
		return len(resp.Sessions)
	}

	t.Run("limit truncates", func(t *testing.T) {
		svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: &mockSessionManager{sessions: threeSessions()}}
		handler, cleanup := setupHandlerWithService(t, svc)
		defer cleanup()

		w := httptest.NewRecorder()
		handler.ListUserSessions(w, listReq("/api/v1/tenants/tenant-123/users/user-123/sessions?limit=2"))

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 2, countSessions(t, w.Body.Bytes()))
	})

	t.Run("zero limit returns all", func(t *testing.T) {
		svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: &mockSessionManager{sessions: threeSessions()}}
		handler, cleanup := setupHandlerWithService(t, svc)
		defer cleanup()

		w := httptest.NewRecorder()
		handler.ListUserSessions(w, listReq("/api/v1/tenants/tenant-123/users/user-123/sessions?limit=0"))

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 3, countSessions(t, w.Body.Bytes()))
	})

	t.Run("absent limit returns all", func(t *testing.T) {
		svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: &mockSessionManager{sessions: threeSessions()}}
		handler, cleanup := setupHandlerWithService(t, svc)
		defer cleanup()

		w := httptest.NewRecorder()
		handler.ListUserSessions(w, listReq("/api/v1/tenants/tenant-123/users/user-123/sessions"))

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 3, countSessions(t, w.Body.Bytes()))
	})

	t.Run("non-numeric limit is a client error", func(t *testing.T) {
		svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: &mockSessionManager{sessions: threeSessions()}}
		handler, cleanup := setupHandlerWithService(t, svc)
		defer cleanup()

		w := httptest.NewRecorder()
		handler.ListUserSessions(w, listReq("/api/v1/tenants/tenant-123/users/user-123/sessions?limit=abc"))

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid limit parameter")
	})

	t.Run("negative limit is a client error", func(t *testing.T) {
		svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: &mockSessionManager{sessions: threeSessions()}}
		handler, cleanup := setupHandlerWithService(t, svc)
		defer cleanup()

		w := httptest.NewRecorder()
		handler.ListUserSessions(w, listReq("/api/v1/tenants/tenant-123/users/user-123/sessions?limit=-1"))

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid limit parameter")
	})
}

// TestListSessions tests the ListSessions handler.
func TestListSessions(t *testing.T) {
	t.Run("user not authenticated", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := systemRequest(http.MethodGet, "/api/v1/iam/sessions", nil)
		w := httptest.NewRecorder()

		handler.ListSessions(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "user not authenticated")
	})

	t.Run("missing tenant ID from context", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(tenantctx.WithSystem(context.Background()), "10123", "")
		req := systemRequest(http.MethodGet, "/api/v1/iam/sessions", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ListSessions(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("session manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(tenantctx.WithSystem(context.Background()), "10123", "tenant-123")
		req := systemRequest(http.MethodGet, "/api/v1/iam/sessions", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ListSessions(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "session manager not available")
	})
}

// TestGetSession tests the GetSession handler.
func TestGetSession(t *testing.T) {
	t.Run("missing session ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "")

		ctx := ctxWithTestSubject(tenantctx.WithSystem(context.Background()), "10123", "tenant-123")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

		req := systemRequest(http.MethodGet, "/api/v1/iam/sessions/", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.GetSession(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "session ID is required")
	})

	t.Run("user not authenticated", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "session-123")

		req := systemRequest(http.MethodGet, "/api/v1/iam/sessions/session-123", nil)
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.GetSession(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "user not authenticated")
	})

	t.Run("missing tenant ID from context", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "session-123")

		ctx := ctxWithTestSubject(tenantctx.WithSystem(context.Background()), "10123", "")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

		req := systemRequest(http.MethodGet, "/api/v1/iam/sessions/session-123", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.GetSession(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("session manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "session-123")

		ctx := ctxWithTestSubject(tenantctx.WithSystem(context.Background()), "10123", "tenant-123")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

		req := systemRequest(http.MethodGet, "/api/v1/iam/sessions/session-123", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.GetSession(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "session manager not available")
	})
}

// TestRevokeSession tests the RevokeSession handler.
func TestRevokeSession(t *testing.T) {
	t.Run("missing session ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "")

		ctx := ctxWithTestSubject(tenantctx.WithSystem(context.Background()), "10123", "tenant-123")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

		req := systemRequest(http.MethodDelete, "/api/v1/iam/sessions/", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.RevokeSession(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "session ID is required")
	})

	t.Run("user not authenticated", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "session-123")

		req := systemRequest(http.MethodDelete, "/api/v1/iam/sessions/session-123", nil)
		req = req.WithContext(callerRouteContext(req.Context(), rctx))
		w := httptest.NewRecorder()

		handler.RevokeSession(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "user not authenticated")
	})

	t.Run("missing tenant ID from context", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "session-123")

		ctx := ctxWithTestSubject(tenantctx.WithSystem(context.Background()), "10123", "")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

		req := systemRequest(http.MethodDelete, "/api/v1/iam/sessions/session-123", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.RevokeSession(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("session manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "session-123")

		ctx := ctxWithTestSubject(tenantctx.WithSystem(context.Background()), "10123", "tenant-123")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

		req := systemRequest(http.MethodDelete, "/api/v1/iam/sessions/session-123", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.RevokeSession(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "session manager not available")
	})
}

// TestRevokeAllSessions tests the RevokeAllSessions handler.
func TestRevokeAllSessions(t *testing.T) {
	t.Run("user not authenticated", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := systemRequest(http.MethodDelete, "/api/v1/iam/sessions", nil)
		w := httptest.NewRecorder()

		handler.RevokeAllSessions(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "user not authenticated")
	})

	t.Run("missing tenant ID from context", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(tenantctx.WithSystem(context.Background()), "10123", "")
		req := systemRequest(http.MethodDelete, "/api/v1/iam/sessions", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.RevokeAllSessions(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("session manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(tenantctx.WithSystem(context.Background()), "10123", "tenant-123")
		req := systemRequest(http.MethodDelete, "/api/v1/iam/sessions", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.RevokeAllSessions(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "session manager not available")
	})

	t.Run("with except query param", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(tenantctx.WithSystem(context.Background()), "10123", "tenant-123")
		req := systemRequest(http.MethodDelete, "/api/v1/iam/sessions?except=session-456", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.RevokeAllSessions(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "session manager not available")
	})
}

// TestRevokeOtherSessions tests the RevokeOtherSessions handler.
func TestRevokeOtherSessions(t *testing.T) {
	t.Run("user not authenticated", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := systemRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RevokeOtherSessions(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "user not authenticated")
	})

	t.Run("missing tenant ID from context", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(tenantctx.WithSystem(context.Background()), "10123", "")
		req := systemRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", strings.NewReader(`{}`))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RevokeOtherSessions(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing current session ID from context", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(tenantctx.WithSystem(context.Background()), "10123", "tenant-123")
		req := systemRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", strings.NewReader(`{}`))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RevokeOtherSessions(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "current session ID is required")
	})

	t.Run("session manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(tenantctx.WithSystem(context.Background()), "10123", "tenant-123")
		req := systemRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others",
			strings.NewReader(`{"keep_session_id":"session-456"}`))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RevokeOtherSessions(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "session manager not available")
	})

	t.Run("with keep_session_id in body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		ctx := ctxWithTestSubject(tenantctx.WithSystem(context.Background()), "10123", "tenant-123")
		body := `{"keep_session_id":"session-789"}`
		req := systemRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.RevokeOtherSessions(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "session manager not available")
	})
}

func TestTokenFormParseErrorPath(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	// Create a request with invalid form data that will fail parsing
	req := systemRequest(http.MethodPost, "/oauth2/token", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	handler.Token(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRevokeFormParseErrorPath(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/oauth2/revoke", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Body = io.NopCloser(&errorReader{})
	w := httptest.NewRecorder()

	handler.Revoke(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateSessionReadBodyErrorPath(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant-123")
	rctx.URLParams.Add("user_id", "10123")

	req := systemRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/sessions", nil)
	req = req.WithContext(callerRouteContext(req.Context(), rctx))
	req.Body = io.NopCloser(&errorReader{})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateSession(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "failed to read request body")
}

func TestRevokeOtherSessionsReadBodyErrorPath(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	ctx := ctxWithTestSubject(tenantctx.WithSystem(context.Background()), "10123", "tenant-123")

	req := systemRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", nil)
	req = req.WithContext(ctx)
	req.Body = io.NopCloser(&errorReader{})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.RevokeOtherSessions(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "failed to read request body")
}

func TestTokenWithAllGrantTypesPath(t *testing.T) {
	tests := []struct {
		name      string
		grantType string
		formData  url.Values
	}{
		{
			name:      "authorization_code grant",
			grantType: "authorization_code",
			formData: url.Values{
				"grant_type":   {"authorization_code"},
				"code":         {"test-code"},
				"redirect_uri": {"https://example.com/callback"},
				"client_id":    {"test-client"},
			},
		},
		{
			name:      "refresh_token grant",
			grantType: "refresh_token",
			formData: url.Values{
				"grant_type":    {"refresh_token"},
				"refresh_token": {"test-refresh-token"},
				"client_id":     {"test-client"},
			},
		},
		{
			name:      "client_credentials grant",
			grantType: "client_credentials",
			formData: url.Values{
				"grant_type":    {"client_credentials"},
				"client_id":     {"test-client"},
				"client_secret": {"test-secret"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _, cleanup := setupIAMHandler(t)
			defer cleanup()

			req := systemRequest(http.MethodPost, "/oauth2/token", strings.NewReader(tt.formData.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()

			handler.Token(w, req)

			// Should fail with OIDC provider not available
			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Contains(t, w.Body.String(), "OIDC provider not available")
		})
	}
}

func TestAuthorizeWithPKCEParams(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	params := url.Values{
		"client_id":             {"test-client"},
		"redirect_uri":          {"https://example.com/callback"},
		"response_type":         {"code"},
		"scope":                 {"openid profile email"},
		"state":                 {"random-state-123"},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
		"code_challenge_method": {"S256"},
		"nonce":                 {"nonce-value-456"},
	}

	req := systemRequest(http.MethodGet, "/oauth2/authorize?"+params.Encode(), nil)
	w := httptest.NewRecorder()

	handler.Authorize(w, req)

	// Should fail with OIDC provider not available
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "OIDC provider not available")
}

func TestUserInfoBearerCaseVariationsPath(t *testing.T) {
	tests := []struct {
		name        string
		authHeader  string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "lowercase bearer",
			authHeader:  "bearer valid-token",
			expectError: true,
			errorMsg:    "OIDC provider not available",
		},
		{
			name:        "uppercase BEARER",
			authHeader:  "BEARER valid-token",
			expectError: true,
			errorMsg:    "OIDC provider not available",
		},
		{
			name:        "mixed case Bearer",
			authHeader:  "BeArEr valid-token",
			expectError: true,
			errorMsg:    "OIDC provider not available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _, cleanup := setupIAMHandler(t)
			defer cleanup()

			req := systemRequest(http.MethodGet, "/oauth2/userinfo", nil)
			req.Header.Set("Authorization", tt.authHeader)
			w := httptest.NewRecorder()

			handler.UserInfo(w, req)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Contains(t, w.Body.String(), tt.errorMsg)
		})
	}
}

func TestCreateSessionWithDeviceInfoPath(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant-123")
	rctx.URLParams.Add("user_id", "10123")

	body := `{
		"token_hash": "abc123",
		"mfa_verified": true,
		"ip_address": "192.168.1.1",
		"user_agent": "Mozilla/5.0",
		"device_info": {
			"id": "device-123",
			"name": "Test Device",
			"type": "mobile",
			"fingerprint": "fp-xyz",
			"trusted": true
		},
		"expires_at": "2099-12-31T23:59:59Z"
	}`

	req := systemRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/sessions", strings.NewReader(body))
	req = req.WithContext(callerRouteContext(req.Context(), rctx))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateSession(w, req)

	// Should fail with session manager not available, but parsing should succeed
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "session manager not available")
}

func TestCreateSessionWithCustomIDPath(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant-123")
	rctx.URLParams.Add("user_id", "10123")

	body := `{
		"id": "custom-session-id-123",
		"token_hash": "abc123"
	}`

	req := systemRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/sessions", strings.NewReader(body))
	req = req.WithContext(callerRouteContext(req.Context(), rctx))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateSession(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCreateSessionWithInvalidExpiryPath(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant-123")
	rctx.URLParams.Add("user_id", "10123")

	body := `{
		"token_hash": "abc123",
		"expires_at": "not-a-valid-date"
	}`

	req := systemRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/sessions", strings.NewReader(body))
	req = req.WithContext(callerRouteContext(req.Context(), rctx))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateSession(w, req)

	// Invalid date parsing should fall back to default expiry
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "session manager not available")
}

func TestRevokeWithTokenTypeHintsPath(t *testing.T) {
	tests := []struct {
		name          string
		token         string
		tokenTypeHint string
	}{
		{
			name:          "refresh_token hint",
			token:         "test-refresh-token",
			tokenTypeHint: "refresh_token",
		},
		{
			name:          "access_token hint",
			token:         "test-access-token",
			tokenTypeHint: "access_token",
		},
		{
			name:          "no hint",
			token:         "test-token",
			tokenTypeHint: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _, cleanup := setupIAMHandler(t)
			defer cleanup()

			form := url.Values{
				"token": {tt.token},
			}
			if tt.tokenTypeHint != "" {
				form.Set("token_type_hint", tt.tokenTypeHint)
			}

			req := systemRequest(http.MethodPost, "/oauth2/revoke", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()

			handler.Revoke(w, req)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Contains(t, w.Body.String(), "OIDC provider not available")
		})
	}
}

func TestListSessionsEmptyUserID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	ctx := ctxWithTestSubject(tenantctx.WithSystem(context.Background()), "", "")
	req := systemRequest(http.MethodGet, "/api/v1/iam/sessions", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ListSessions(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "user not authenticated")
}

func TestListSessionsEmptyTenantID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	ctx := ctxWithTestSubject(tenantctx.WithSystem(context.Background()), "10123", "")
	req := systemRequest(http.MethodGet, "/api/v1/iam/sessions", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ListSessions(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tenant ID is required")
}

func TestIAM91_CreateSession_MissingTenantID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/tenants//users/u1/sessions", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": "", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.CreateSession(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tenant ID is required")
}

func TestIAM91_CreateSession_MissingUserID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/tenants/t1/users//sessions", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": ""})
	w := httptest.NewRecorder()
	handler.CreateSession(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "user ID is required")
}

func TestIAM91_CreateSession_InvalidJSON(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/tenants/t1/users/u1/sessions", strings.NewReader("{bad"))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.CreateSession(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid request body")
}

func TestIAM91_CreateSession_MissingTokenHash(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/tenants/t1/users/u1/sessions", strings.NewReader(`{"token_hash":""}`))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.CreateSession(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "token_hash is required")
}

func TestIAM91_CreateSession_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	body := `{"token_hash":"abc123"}`
	req := systemRequest(http.MethodPost, "/api/v1/tenants/t1/users/u1/sessions", strings.NewReader(body))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.CreateSession(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "session manager not available")
}

func TestIAM91_ListUserSessions_MissingTenantID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/tenants//users/u1/sessions", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.ListUserSessions(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_ListUserSessions_MissingUserID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/tenants/t1/users//sessions", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": ""})
	w := httptest.NewRecorder()
	handler.ListUserSessions(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_ListUserSessions_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/tenants/t1/users/u1/sessions", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.ListUserSessions(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "session manager not available")
}

func TestIAM91_ListSessions_MissingUserContext(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/iam/sessions", nil)
	w := httptest.NewRecorder()
	handler.ListSessions(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIAM91_ListSessions_MissingTenantContext(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/iam/sessions", nil)
	ctx := ctxWithTestSubject(req.Context(), "30001", "")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ListSessions(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_ListSessions_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/iam/sessions", nil)
	ctx := ctxWithTestSubject(req.Context(), "30001", "t1")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ListSessions(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_GetSession_MissingSessionID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/iam/sessions/", nil)
	req = withChiParams(req, map[string]string{"id": ""})
	w := httptest.NewRecorder()
	handler.GetSession(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_GetSession_MissingUserContext(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/iam/sessions/s1", nil)
	req = withChiParams(req, map[string]string{"id": "s1"})
	w := httptest.NewRecorder()
	handler.GetSession(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIAM91_GetSession_MissingTenantContext(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/iam/sessions/s1", nil)
	req = withChiParams(req, map[string]string{"id": "s1"})
	ctx := ctxWithTestSubject(req.Context(), "30001", "")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.GetSession(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_GetSession_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/iam/sessions/s1", nil)
	req = withChiParams(req, map[string]string{"id": "s1"})
	ctx := ctxWithTestSubject(req.Context(), "30001", "t1")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.GetSession(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_RevokeSession_MissingSessionID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/api/v1/iam/sessions/", nil)
	req = withChiParams(req, map[string]string{"id": ""})
	w := httptest.NewRecorder()
	handler.RevokeSession(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_RevokeSession_MissingUserContext(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/api/v1/iam/sessions/s1", nil)
	req = withChiParams(req, map[string]string{"id": "s1"})
	w := httptest.NewRecorder()
	handler.RevokeSession(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIAM91_RevokeSession_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/api/v1/iam/sessions/s1", nil)
	req = withChiParams(req, map[string]string{"id": "s1"})
	ctx := ctxWithTestSubject(req.Context(), "30001", "t1")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.RevokeSession(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_RevokeAllSessions_MissingUserContext(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/api/v1/iam/sessions", nil)
	w := httptest.NewRecorder()
	handler.RevokeAllSessions(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIAM91_RevokeAllSessions_MissingTenantContext(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/api/v1/iam/sessions", nil)
	ctx := ctxWithTestSubject(req.Context(), "30001", "")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.RevokeAllSessions(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_RevokeAllSessions_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/api/v1/iam/sessions", nil)
	ctx := ctxWithTestSubject(req.Context(), "30001", "t1")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.RevokeAllSessions(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_RevokeOtherSessions_MissingUserContext(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", nil)
	w := httptest.NewRecorder()
	handler.RevokeOtherSessions(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIAM91_RevokeOtherSessions_MissingTenantContext(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", nil)
	ctx := ctxWithTestSubject(req.Context(), "30001", "")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.RevokeOtherSessions(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_RevokeOtherSessions_MissingSessionContext(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others", nil)
	ctx := ctxWithTestSubject(req.Context(), "30001", "t1")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.RevokeOtherSessions(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "current session ID is required")
}

func TestIAM91_RevokeOtherSessions_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/iam/sessions/revoke-others",
		strings.NewReader(`{"keep_session_id":"s1"}`))
	ctx := ctxWithTestSubject(req.Context(), "30001", "t1")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.RevokeOtherSessions(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// =============================================================================
// Apps: Missing params + service not available
// =============================================================================

func TestIAM91_ListApps_MissingTenantID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/tenants//apps", nil)
	req = withChiParams(req, map[string]string{"tenant_id": ""})
	w := httptest.NewRecorder()
	handler.ListApps(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_ListApps_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/tenants/t1/apps", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	handler.ListApps(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "app manager not available")
}

func TestIAM91_ListApps_WithPagination(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/tenants/t1/apps?limit=50&offset=10", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	handler.ListApps(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_RegisterApp_MissingTenantID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/tenants//apps", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": ""})
	w := httptest.NewRecorder()
	handler.RegisterApp(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_RegisterApp_InvalidJSON(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/tenants/t1/apps", strings.NewReader("{bad"))
	req = withChiParams(req, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	handler.RegisterApp(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_RegisterApp_MissingName(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/tenants/t1/apps", strings.NewReader(`{"redirect_uris":["https://x"]}`))
	req = withChiParams(req, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	handler.RegisterApp(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "application name is required")
}

func TestIAM91_RegisterApp_MissingRedirectURIs(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/tenants/t1/apps", strings.NewReader(`{"name":"myapp"}`))
	req = withChiParams(req, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	handler.RegisterApp(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "redirect URI is required")
}

func TestIAM91_RegisterApp_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	body := `{"name":"myapp","redirect_uris":["https://example.com"]}`
	req := systemRequest(http.MethodPost, "/api/v1/tenants/t1/apps", strings.NewReader(body))
	req = withChiParams(req, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	handler.RegisterApp(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_GetApp_MissingTenantID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/tenants//apps/c1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "", "client_id": "c1"})
	w := httptest.NewRecorder()
	handler.GetApp(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_GetApp_MissingClientID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/tenants/t1/apps/", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "client_id": ""})
	w := httptest.NewRecorder()
	handler.GetApp(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_GetApp_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/tenants/t1/apps/c1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "client_id": "c1"})
	w := httptest.NewRecorder()
	handler.GetApp(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_UpdateApp_MissingTenantID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPut, "/api/v1/tenants//apps/c1", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": "", "client_id": "c1"})
	w := httptest.NewRecorder()
	handler.UpdateApp(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_UpdateApp_MissingClientID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPut, "/api/v1/tenants/t1/apps/", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "client_id": ""})
	w := httptest.NewRecorder()
	handler.UpdateApp(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_UpdateApp_InvalidJSON(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPut, "/api/v1/tenants/t1/apps/c1", strings.NewReader("{bad"))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "client_id": "c1"})
	w := httptest.NewRecorder()
	handler.UpdateApp(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_UpdateApp_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPut, "/api/v1/tenants/t1/apps/c1", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "client_id": "c1"})
	w := httptest.NewRecorder()
	handler.UpdateApp(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_DeleteApp_MissingTenantID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/api/v1/tenants//apps/c1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "", "client_id": "c1"})
	w := httptest.NewRecorder()
	handler.DeleteApp(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_DeleteApp_MissingClientID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/api/v1/tenants/t1/apps/", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "client_id": ""})
	w := httptest.NewRecorder()
	handler.DeleteApp(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_DeleteApp_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/api/v1/tenants/t1/apps/c1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "client_id": "c1"})
	w := httptest.NewRecorder()
	handler.DeleteApp(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_RotateSecret_MissingTenantID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/tenants//apps/c1/rotate-secret", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "", "client_id": "c1"})
	w := httptest.NewRecorder()
	handler.RotateSecret(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_RotateSecret_MissingClientID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/tenants/t1/apps//rotate-secret", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "client_id": ""})
	w := httptest.NewRecorder()
	handler.RotateSecret(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_RotateSecret_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/tenants/t1/apps/c1/rotate-secret", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "client_id": "c1"})
	w := httptest.NewRecorder()
	handler.RotateSecret(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_ValidateClient_InvalidJSON(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/iam/validate-client", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	handler.ValidateClient(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_ValidateClient_MissingClientID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/iam/validate-client", strings.NewReader(`{"client_secret":"x"}`))
	w := httptest.NewRecorder()
	handler.ValidateClient(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_ValidateClient_MissingClientSecret(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/iam/validate-client", strings.NewReader(`{"client_id":"x"}`))
	w := httptest.NewRecorder()
	handler.ValidateClient(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_ValidateClient_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	body := `{"client_id":"x","client_secret":"y"}`
	req := systemRequest(http.MethodPost, "/api/v1/iam/validate-client", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ValidateClient(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// =============================================================================
// GDPR: Missing params + service not available
// =============================================================================

func TestIAM91_GrantConsent_MissingTenantID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/consent", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": "", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.GrantConsent(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_GrantConsent_MissingUserID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/consent", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": ""})
	w := httptest.NewRecorder()
	handler.GrantConsent(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_GrantConsent_InvalidJSON(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/consent", strings.NewReader("{bad"))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.GrantConsent(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_GrantConsent_MissingPurpose(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/consent", strings.NewReader(`{"version":"1"}`))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.GrantConsent(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "purpose is required")
}

func TestIAM91_GrantConsent_MissingVersion(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/consent", strings.NewReader(`{"purpose":"marketing"}`))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.GrantConsent(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "version is required")
}

func TestIAM91_GrantConsent_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	body := `{"purpose":"marketing","version":"1"}`
	req := systemRequest(http.MethodPost, "/consent", strings.NewReader(body))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.GrantConsent(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "GDPR manager not available")
}

func TestIAM91_ListUserConsents_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/consents", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.ListUserConsents(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_GetConsent_MissingPurpose(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/consent/", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001", "purpose": ""})
	w := httptest.NewRecorder()
	handler.GetConsent(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_GetConsent_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/consent/marketing", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001", "purpose": "marketing"})
	w := httptest.NewRecorder()
	handler.GetConsent(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_WithdrawConsent_MissingPurpose(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPut, "/consent//withdraw", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001", "purpose": ""})
	w := httptest.NewRecorder()
	handler.WithdrawConsent(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_WithdrawConsent_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPut, "/consent/marketing/withdraw", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001", "purpose": "marketing"})
	w := httptest.NewRecorder()
	handler.WithdrawConsent(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_RequestDataExport_InvalidJSON(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/export", strings.NewReader("{bad"))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.RequestDataExport(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_RequestDataExport_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/export", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.RequestDataExport(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_GetExportStatus_MissingExportID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/export/", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001", "export_id": ""})
	w := httptest.NewRecorder()
	handler.GetExportStatus(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_GetExportStatus_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/export/e1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001", "export_id": "e1"})
	w := httptest.NewRecorder()
	handler.GetExportStatus(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_DownloadExport_MissingExportID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/export//download", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001", "export_id": ""})
	w := httptest.NewRecorder()
	handler.DownloadExport(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_DownloadExport_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/export/e1/download", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001", "export_id": "e1"})
	w := httptest.NewRecorder()
	handler.DownloadExport(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_RequestDeletion_InvalidJSON(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/deletion", strings.NewReader("{bad"))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.RequestDeletion(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_RequestDeletion_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/deletion", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.RequestDeletion(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_GetDeletionStatus_MissingDeletionID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/deletion/", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001", "deletion_id": ""})
	w := httptest.NewRecorder()
	handler.GetDeletionStatus(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_GetDeletionStatus_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/deletion/d1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001", "deletion_id": "d1"})
	w := httptest.NewRecorder()
	handler.GetDeletionStatus(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_CancelDeletion_MissingDeletionID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/deletion/", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001", "deletion_id": ""})
	w := httptest.NewRecorder()
	handler.CancelDeletion(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_CancelDeletion_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/deletion/d1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001", "deletion_id": "d1"})
	w := httptest.NewRecorder()
	handler.CancelDeletion(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// =============================================================================
// MFA: Missing params + service not available
// =============================================================================

func TestIAM91_getMFAManager_NotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	mgr, err := handler.getMFAManager(tenantctx.WithSystem(context.Background()))
	assert.Nil(t, mgr)
	assert.ErrorIs(t, err, ErrMFANotAvailable)
}

func TestIAM91_BeginMFAEnrollment_MissingTenantID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/mfa/enroll", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.BeginMFAEnrollment(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_BeginMFAEnrollment_MissingUserID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/mfa/enroll", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": ""})
	w := httptest.NewRecorder()
	handler.BeginMFAEnrollment(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_BeginMFAEnrollment_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/mfa/enroll", nil)
	req.ContentLength = 0
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.BeginMFAEnrollment(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_CompleteMFAEnrollment_MissingIDs(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/mfa/verify", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": "", "user_id": ""})
	w := httptest.NewRecorder()
	handler.CompleteMFAEnrollment(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_CompleteMFAEnrollment_InvalidJSON(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/mfa/verify", strings.NewReader("{bad"))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.CompleteMFAEnrollment(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_CompleteMFAEnrollment_MissingFields(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/mfa/verify", strings.NewReader(`{"enrollment_id":""}`))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.CompleteMFAEnrollment(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "enrollment_id and code are required")
}

func TestIAM91_CompleteMFAEnrollment_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	body := `{"enrollment_id":"e1","code":"123456"}`
	req := systemRequest(http.MethodPost, "/mfa/verify", strings.NewReader(body))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.CompleteMFAEnrollment(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_CancelMFAEnrollment_MissingIDs(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/mfa/enroll/e1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "", "user_id": "", "enrollment_id": ""})
	w := httptest.NewRecorder()
	handler.CancelMFAEnrollment(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_CancelMFAEnrollment_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/mfa/enroll/e1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001", "enrollment_id": "e1"})
	w := httptest.NewRecorder()
	handler.CancelMFAEnrollment(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_GetMFAStatus_MissingIDs(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/mfa", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "", "user_id": ""})
	w := httptest.NewRecorder()
	handler.GetMFAStatus(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_GetMFAStatus_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/mfa", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.GetMFAStatus(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_DisableMFA_MissingIDs(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/mfa", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "", "user_id": ""})
	w := httptest.NewRecorder()
	handler.DisableMFA(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_DisableMFA_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/mfa", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.DisableMFA(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_BeginMFAChallenge_MissingIDs(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/mfa/challenge", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": "", "user_id": ""})
	w := httptest.NewRecorder()
	handler.BeginMFAChallenge(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_BeginMFAChallenge_InvalidJSON(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/mfa/challenge", strings.NewReader("{bad"))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.BeginMFAChallenge(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_BeginMFAChallenge_MissingSessionToken(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/mfa/challenge", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.BeginMFAChallenge(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "session_token is required")
}

func TestIAM91_BeginMFAChallenge_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/mfa/challenge", strings.NewReader(`{"session_token":"tok"}`))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.BeginMFAChallenge(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_VerifyMFAChallenge_MissingIDs(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/mfa/verify-challenge", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": "", "user_id": ""})
	w := httptest.NewRecorder()
	handler.VerifyMFAChallenge(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_VerifyMFAChallenge_MissingFields(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/mfa/verify-challenge", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.VerifyMFAChallenge(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "challenge_id and code are required")
}

func TestIAM91_VerifyMFAChallenge_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	body := `{"challenge_id":"c1","code":"123456"}`
	req := systemRequest(http.MethodPost, "/mfa/verify-challenge", strings.NewReader(body))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.VerifyMFAChallenge(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_GenerateBackupCodes_MissingIDs(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/mfa/backup-codes", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "", "user_id": ""})
	w := httptest.NewRecorder()
	handler.GenerateBackupCodes(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_GenerateBackupCodes_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/mfa/backup-codes", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.GenerateBackupCodes(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_GetBackupCodeStatus_MissingIDs(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/mfa/backup-codes", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "", "user_id": ""})
	w := httptest.NewRecorder()
	handler.GetBackupCodeStatus(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_GetBackupCodeStatus_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/mfa/backup-codes", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.GetBackupCodeStatus(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// =============================================================================
// Audit: Missing params + service not available
// =============================================================================

// TestIAM91_ListAuditEvents_MissingTenantID_DefaultsToCallerTenant proves an
// omitted tenant_id no longer 400s: it defaults to the caller's own
// authenticated tenant (iam.ResolveRequestTenant) — empty in this
// unauthenticated test context — and the request proceeds past tenant
// resolution to the next real dependency (the unwired audit logger),
// exactly like the explicit-tenant_id case
// (TestIAM91_ListAuditEvents_ServiceNotAvailable).
func TestIAM91_ListAuditEvents_MissingTenantID_DefaultsToCallerTenant(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/iam/audit", nil)
	w := httptest.NewRecorder()
	handler.ListAuditEvents(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "audit logger not available")
}

func TestIAM91_ListAuditEvents_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/iam/audit?tenant_id=t1", nil)
	req = req.WithContext(tenantctx.WithTenant(req.Context(), "t1"))
	w := httptest.NewRecorder()
	handler.ListAuditEvents(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "audit logger not available")
}

func TestIAM91_ListAuditEvents_WithFilters(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/iam/audit?tenant_id=t1&event_type=login&category=auth&outcome=success&actor_id=a1&resource_id=r1&start_time=2025-01-01T00:00:00Z&end_time=2025-12-31T23:59:59Z&limit=50&offset=10", nil)
	req = req.WithContext(tenantctx.WithTenant(req.Context(), "t1"))
	w := httptest.NewRecorder()
	handler.ListAuditEvents(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_GetAuditEvent_MissingEventID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/iam/audit/", nil)
	req = withChiParams(req, map[string]string{"id": ""})
	w := httptest.NewRecorder()
	handler.GetAuditEvent(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestIAM91_GetAuditEvent_MissingTenantID_DefaultsToCallerTenant proves an
// omitted tenant_id no longer 400s: it defaults to the caller's own
// authenticated tenant (iam.ResolveRequestTenant) — empty in this
// unauthenticated test context — and the request proceeds past tenant
// resolution to the next real dependency (the unwired audit logger).
func TestIAM91_GetAuditEvent_MissingTenantID_DefaultsToCallerTenant(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/iam/audit/ev1", nil)
	req = withChiParams(req, map[string]string{"id": "ev1"})
	w := httptest.NewRecorder()
	handler.GetAuditEvent(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "audit logger not available")
}

func TestIAM91_GetAuditEvent_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/iam/audit/ev1?tenant_id=t1", nil)
	req = withChiParams(req, map[string]string{"id": "ev1"})
	req = req.WithContext(tenantctx.WithTenant(req.Context(), "t1"))
	w := httptest.NewRecorder()
	handler.GetAuditEvent(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_DeleteOldAuditEvents_MissingTenantID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/api/v1/iam/audit", nil)
	w := httptest.NewRecorder()
	handler.DeleteOldAuditEvents(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_DeleteOldAuditEvents_MissingBefore(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/api/v1/iam/audit?tenant_id=t1", nil)
	req = req.WithContext(tenantctx.WithTenant(req.Context(), "t1"))
	w := httptest.NewRecorder()
	handler.DeleteOldAuditEvents(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "before parameter is required")
}

func TestIAM91_DeleteOldAuditEvents_InvalidTimestamp(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/api/v1/iam/audit?tenant_id=t1&before=not-a-date", nil)
	req = req.WithContext(tenantctx.WithTenant(req.Context(), "t1"))
	w := httptest.NewRecorder()
	handler.DeleteOldAuditEvents(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid before timestamp")
}

func TestIAM91_DeleteOldAuditEvents_MissingUser(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/api/v1/iam/audit?tenant_id=t1&before=2025-01-01T00:00:00Z", nil)
	req = req.WithContext(tenantctx.WithTenant(req.Context(), "t1"))
	w := httptest.NewRecorder()
	handler.DeleteOldAuditEvents(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIAM91_RegisterAuditRoutes(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	r := chi.NewRouter()
	RegisterAuditRoutes(r, handler)
	assert.NotNil(t, r)
}

// =============================================================================
// Dynamic Client Registration: validation + error paths
// =============================================================================

func TestIAM91_RegisterDynamicClient_InvalidJSON(t *testing.T) {
	handler, cleanup := setupOpenDCRHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/oauth2/register", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	handler.RegisterDynamicClient(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_RegisterDynamicClient_MissingRedirectURIs(t *testing.T) {
	handler, cleanup := setupOpenDCRHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/oauth2/register", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handler.RegisterDynamicClient(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "redirect_uris required")
}

func TestIAM91_RegisterDynamicClient_ServiceNotAvailable(t *testing.T) {
	handler, cleanup := setupOpenDCRHandler(t)
	defer cleanup()

	body := `{"redirect_uris":["https://example.com/cb"]}`
	req := systemRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.RegisterDynamicClient(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_ValidateDCRRequest(t *testing.T) {
	t.Run("invalid redirect URI", func(t *testing.T) {
		req := &DynamicClientRequest{RedirectURIs: []string{"/no-scheme-path"}}
		err := validateDCRRequest(req)
		require.NotNil(t, err)
		assert.Equal(t, DCRErrorInvalidRedirectURI, err.code)
	})

	t.Run("unsupported grant type", func(t *testing.T) {
		req := &DynamicClientRequest{
			GrantTypes:   []string{"bad_grant"},
			RedirectURIs: []string{"https://example.com/cb"},
		}
		err := validateDCRRequest(req)
		require.NotNil(t, err)
		assert.Equal(t, DCRErrorInvalidClientMetadata, err.code)
	})

	t.Run("unsupported response type", func(t *testing.T) {
		req := &DynamicClientRequest{
			GrantTypes:    []string{"client_credentials"},
			ResponseTypes: []string{"bad_type"},
		}
		err := validateDCRRequest(req)
		require.NotNil(t, err)
		assert.Equal(t, DCRErrorInvalidClientMetadata, err.code)
	})

	t.Run("valid client_credentials", func(t *testing.T) {
		req := &DynamicClientRequest{GrantTypes: []string{"client_credentials"}}
		err := validateDCRRequest(req)
		assert.Nil(t, err)
	})
}

func TestIAM91_IsValidRedirectURI(t *testing.T) {
	assert.True(t, isValidRedirectURI("https://example.com/cb"))
	assert.True(t, isValidRedirectURI("http://localhost:8080/cb"))
	assert.True(t, isValidRedirectURI("http://127.0.0.1:8080/cb"))
	assert.True(t, isValidRedirectURI("http://[::1]:8080/cb"))
	assert.True(t, isValidRedirectURI("com.example.app:/callback"))
	assert.False(t, isValidRedirectURI(""))
	assert.False(t, isValidRedirectURI("/no-scheme-path"))
}

func TestIAM91_IsValidDCRGrantType(t *testing.T) {
	assert.True(t, isValidDCRGrantType("authorization_code"))
	assert.True(t, isValidDCRGrantType("client_credentials"))
	assert.True(t, isValidDCRGrantType("refresh_token"))
	assert.False(t, isValidDCRGrantType("bad"))
}

func TestIAM91_RespondDCRError(t *testing.T) {
	w := httptest.NewRecorder()
	respondDCRError(w, http.StatusBadRequest, "invalid_client_metadata", "test error")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_client_metadata")
}

func TestIAM91_GetDynamicClient_MissingClientID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/oauth2/register/", nil)
	req = withChiParams(req, map[string]string{"client_id": ""})
	w := httptest.NewRecorder()
	handler.GetDynamicClient(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_GetDynamicClient_MissingAuth(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/oauth2/register/c1", nil)
	req = withChiParams(req, map[string]string{"client_id": "c1"})
	w := httptest.NewRecorder()
	handler.GetDynamicClient(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIAM91_GetDynamicClient_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/oauth2/register/c1", nil)
	req.Header.Set("Authorization", "Bearer tok")
	req = withChiParams(req, map[string]string{"client_id": "c1"})
	w := httptest.NewRecorder()
	handler.GetDynamicClient(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_UpdateDynamicClient_MissingClientID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPut, "/oauth2/register/", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"client_id": ""})
	w := httptest.NewRecorder()
	handler.UpdateDynamicClient(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// A malformed body never reaches the parser without a credential: the RFC 7592
// gate runs first, so an anonymous caller gets 401 rather than a 400 that would
// confirm the request was processed. The authorized 400 path is covered by
// TestUpdateDynamicClient_AuthorizedInvalidBody.
func TestIAM91_UpdateDynamicClient_InvalidJSONUnauthorized(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPut, "/oauth2/register/c1", strings.NewReader("{bad"))
	req = withChiParams(req, map[string]string{"client_id": "c1"})
	w := httptest.NewRecorder()
	handler.UpdateDynamicClient(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIAM91_UpdateDynamicClient_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	body := `{"grant_types":["client_credentials"]}`
	req := systemRequest(http.MethodPut, "/oauth2/register/c1", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	req = withChiParams(req, map[string]string{"client_id": "c1"})
	w := httptest.NewRecorder()
	handler.UpdateDynamicClient(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_DeleteDynamicClient_MissingClientID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/oauth2/register/", nil)
	req = withChiParams(req, map[string]string{"client_id": ""})
	w := httptest.NewRecorder()
	handler.DeleteDynamicClient(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_DeleteDynamicClient_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/oauth2/register/c1", nil)
	req.Header.Set("Authorization", "Bearer tok")
	req = withChiParams(req, map[string]string{"client_id": "c1"})
	w := httptest.NewRecorder()
	handler.DeleteDynamicClient(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// =============================================================================
// Middleware helpers
// =============================================================================

func TestIAM91_MiddlewareHelpers(t *testing.T) {
	m := map[string]interface{}{
		"key1": "value1",
		"key2": []interface{}{"a", "b"},
		"key3": []string{"x", "y"},
		"key4": map[string]interface{}{"nested": true},
		"key5": 123,
	}

	assert.Equal(t, "value1", getString(m, "key1"))
	assert.Equal(t, "", getString(m, "key5"))
	assert.Equal(t, "", getString(m, "missing"))

	assert.Equal(t, []string{"a", "b"}, getStringSlice(m, "key2"))
	assert.Equal(t, []string{"x", "y"}, getStringSlice(m, "key3"))
	assert.Nil(t, getStringSlice(m, "missing"))

	assert.Equal(t, map[string]interface{}{"nested": true}, getMap(m, "key4"))
	assert.Nil(t, getMap(m, "missing"))
}

func TestIAM91_JWTMiddleware_MissingHeader(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := systemRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	handler.JWTMiddleware(next).ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIAM91_JWTMiddleware_InvalidFormat(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := systemRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Basic abc")
	w := httptest.NewRecorder()
	handler.JWTMiddleware(next).ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIAM91_JWTMiddleware_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := systemRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	w := httptest.NewRecorder()
	// basic mock does not implement GetOIDCProvider
	handler.JWTMiddleware(next).ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_ABACMiddleware_MissingUser(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := systemRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	handler.ABACMiddleware("resource", "read")(next).ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIAM91_ABACMiddleware_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := systemRequest(http.MethodGet, "/protected", nil)
	ctx := context.WithValue(req.Context(), ContextKeyUser, &iam.UserInfoResponse{Sub: "u1"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ABACMiddleware("resource", "read")(next).ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_ImpersonationMiddleware_NoHeader(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := systemRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ImpersonationMiddleware(next).ServeHTTP(w, req)
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIAM91_ImpersonationMiddleware_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := systemRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Impersonation-Session-ID", "session-123")
	w := httptest.NewRecorder()
	handler.ImpersonationMiddleware(next).ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_ImpersonationAuditMiddleware_NoImpersonation(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := systemRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ImpersonationAuditMiddleware(next).ServeHTTP(w, req)
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, w.Code)
}

// =============================================================================
// Errors: factory functions
// =============================================================================

func TestIAM91_ErrorFactories(t *testing.T) {
	assert.Equal(t, ErrInvalidClient, NewInvalidClientError("x").Code)
	assert.Equal(t, ErrInvalidGrant, NewInvalidGrantError("x").Code)
	assert.Equal(t, ErrUnauthorizedClient, NewUnauthorizedClientError("x").Code)
	assert.Equal(t, ErrUnsupportedGrantType, NewUnsupportedGrantTypeError("x").Code)
	assert.Equal(t, ErrInvalidScope, NewInvalidScopeError("x").Code)
	assert.Equal(t, ErrAccessDenied, NewAccessDeniedError("x").Code)
	assert.Equal(t, ErrUnsupportedResponseType, NewUnsupportedResponseTypeError("x").Code)
}

func TestIAM91_OAuthError_ErrorString(t *testing.T) {
	e := &OAuthError{Code: "test_code"}
	assert.Equal(t, "test_code", e.Error())

	e2 := &OAuthError{Code: "test_code", ErrorDescription: "desc"}
	assert.Equal(t, "test_code: desc", e2.Error())
}

// =============================================================================
// Roles/Users/Tenants: uncovered error paths
// =============================================================================

func TestIAM91_UpdateRole_MissingTenantID(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := systemRequest(http.MethodPut, "/api/v1/tenants//roles/r1", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": "", "role_id": "r1"})
	w := httptest.NewRecorder()
	handler.UpdateRole(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_UpdateRole_MissingRoleID(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := systemRequest(http.MethodPut, "/api/v1/tenants/t1/roles/", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "role_id": ""})
	w := httptest.NewRecorder()
	handler.UpdateRole(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_UpdateRole_InvalidJSON(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := systemRequest(http.MethodPut, "/api/v1/tenants/t1/roles/r1", strings.NewReader("{bad"))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "role_id": "r1"})
	w := httptest.NewRecorder()
	handler.UpdateRole(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_UpdateUser_MissingTenantID(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := systemRequest(http.MethodPut, "/api/v1/tenants//users/u1", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": "", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.UpdateUser(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_UpdateUser_MissingUserID(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := systemRequest(http.MethodPut, "/api/v1/tenants/t1/users/", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": ""})
	w := httptest.NewRecorder()
	handler.UpdateUser(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_UpdateUser_InvalidJSON(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := systemRequest(http.MethodPut, "/api/v1/tenants/t1/users/u1", strings.NewReader("{bad"))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.UpdateUser(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_UpdateTenant_MissingTenantID(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := systemRequest(http.MethodPut, "/api/v1/tenants/", strings.NewReader(`{}`))
	req = withChiParams(req, map[string]string{"tenant_id": ""})
	w := httptest.NewRecorder()
	handler.UpdateTenant(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_UpdateTenant_InvalidJSON(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := systemRequest(http.MethodPut, "/api/v1/tenants/t1", strings.NewReader("{bad"))
	req = withChiParams(req, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	handler.UpdateTenant(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_DeleteTenant_MissingTenantID(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/api/v1/tenants/", nil)
	req = withChiParams(req, map[string]string{"tenant_id": ""})
	w := httptest.NewRecorder()
	handler.DeleteTenant(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// =============================================================================
// Login: additional error paths
// =============================================================================

// TestOIDCAuthorize_RejectsPasswordGrant verifies that POST /api/v1/auth/login
// no longer exists (route deleted). Callers must use WebAuthn or PIV login.
func TestOIDCAuthorize_RejectsPasswordGrant(t *testing.T) {
	// The /api/v1/auth/login route is removed; only /api/v1/auth/login/piv exists.
	// This test documents the expected 404 for the old password login path.
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	// handler.Login no longer exists; the route is 404. Document this via PIVLogin
	// returning 401 piv_login_disabled when PIV is not enabled (the default).
	req := systemRequest(http.MethodPost, "/api/v1/auth/login/piv", nil)
	w := httptest.NewRecorder()
	handler.PIVLogin(w, req)
	// PIV disabled by default → 401 Unauthorized with piv_login_disabled error code.
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), ErrPIVLoginDisabled)
}

func TestIAM91_RefreshToken_InvalidJSON(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	handler.RefreshToken(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_RefreshToken_MissingToken(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handler.RefreshToken(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "refresh_token is required")
}

func TestIAM91_Logout_MissingToken(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/auth/logout", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handler.Logout(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "access token is required")
}

func TestIAM91_ValidateToken_MissingToken(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/auth/validate", nil)
	w := httptest.NewRecorder()
	handler.ValidateToken(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "access token is required")
}

func TestIAM91_GetBearerTokenString(t *testing.T) {
	req := systemRequest(http.MethodGet, "/", nil)
	assert.Equal(t, "", getBearerTokenString(req))

	req.Header.Set("Authorization", "Basic abc")
	assert.Equal(t, "", getBearerTokenString(req))

	req.Header.Set("Authorization", "Bearer my-token")
	assert.Equal(t, "my-token", getBearerTokenString(req))
}

// =============================================================================
// GDPR routes registration
// =============================================================================

func TestIAM91_RegisterGDPRRoutes(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	r := chi.NewRouter()
	RegisterGDPRRoutes(r, handler)
	assert.NotNil(t, r)
}

// =============================================================================
// OIDC: error paths (service not available)
// =============================================================================

func TestIAM91_GetOIDCConfig_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
	w := httptest.NewRecorder()
	handler.GetOIDCConfig(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "OIDC provider not available")
}

func TestIAM91_GetJWKS_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	w := httptest.NewRecorder()
	handler.GetJWKS(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "OIDC provider not available")
}

func TestIAM91_Authorize_MissingClientID(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/oauth2/authorize", nil)
	w := httptest.NewRecorder()
	handler.Authorize(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "client_id is required")
}

func TestIAM91_Authorize_MissingResponseType(t *testing.T) {
	handler := setupAuthorizeHandler(t)

	req := httptest.NewRequest(http.MethodGet, authorizeMissingResponseTypeTarget, nil)
	w := httptest.NewRecorder()
	handler.Authorize(w, req)

	// draft-ietf-oauth-v2-1-16 §4.1.2.1: redirected to the client.
	assertAuthorizeErrorRedirect(t, w, ErrInvalidRequest, "st-1")
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Contains(t, loc.Query().Get("error_description"), "response_type is required")
}

func TestIAM91_Authorize_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/oauth2/authorize?client_id=c1&redirect_uri=https://x.com/cb&response_type=code", nil)
	w := httptest.NewRecorder()
	handler.Authorize(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_Token_MissingGrantType(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/oauth2/token", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Token(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "grant_type is required")
}

func TestIAM91_Token_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/oauth2/token", strings.NewReader("grant_type=authorization_code"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Token(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_UserInfo_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/oauth2/userinfo", nil)
	w := httptest.NewRecorder()
	handler.UserInfo(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIAM91_Revoke_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/oauth2/revoke", strings.NewReader("token=abc123"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Revoke(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// =============================================================================
// Mock services with IAM interfaces that return errors or nil managers
// =============================================================================

// mockServiceWithSessions implements GetSessionManager for deeper testing.
type mockServiceWithSessions struct {
	service.QuicBaaS
	mgr    SessionManager
	mgrErr error
}

func (m *mockServiceWithSessions) GetSessionManager(ctx context.Context) (SessionManager, error) {
	return m.mgr, m.mgrErr
}

// ResolveCallerAccount forwards to the embedded service, which the embedding
// interface field would otherwise hide from the handler's type assertion.
func (m *mockServiceWithSessions) ResolveCallerAccount(ctx context.Context, subject interfaces.Subject) (uint64, error) {
	resolver, ok := m.QuicBaaS.(service.CallerAccountResolver)
	if !ok {
		return 0, &service.CallerAccountRefusedError{Reason: "the embedded service resolves no caller account"}
	}
	return resolver.ResolveCallerAccount(ctx, subject)
}

// mockSessionManager implements SessionManager for testing.
type mockSessionManager struct {
	createErr error
	sessions  []*iam.SessionExt
	listErr   error
	getErr    error
	revokeErr error

	// revokedUserID / revokedTenantID capture the identity RevokeAllForUser
	// was invoked with so tests can pin the path-scoped target contract.
	revokedUserID   uint64
	revokedTenantID string

	// listedUserID captures the account List was scoped to.
	listedUserID uint64
}

func (m *mockSessionManager) Create(ctx context.Context, session *iam.SessionExt) error {
	return m.createErr
}

func (m *mockSessionManager) List(ctx context.Context, userID uint64, tenantID string) ([]*iam.SessionExt, error) {
	m.listedUserID = userID
	return m.sessions, m.listErr
}

func (m *mockSessionManager) Get(ctx context.Context, sessionID string) (*iam.SessionExt, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for _, s := range m.sessions {
		if s.ID == sessionID {
			return s, nil
		}
	}
	return nil, errors.New("session not found")
}

func (m *mockSessionManager) Revoke(ctx context.Context, sessionID string) error {
	return m.revokeErr
}

func (m *mockSessionManager) UpdateActivity(ctx context.Context, sessionID string) error {
	return nil
}

func (m *mockSessionManager) RevokeAllForUser(ctx context.Context, userID uint64, tenantID string) error {
	m.revokedUserID = userID
	m.revokedTenantID = tenantID
	return m.revokeErr
}

func (m *mockSessionManager) RevokeAllExcept(ctx context.Context, userID uint64, tenantID, exceptSessionID string) error {
	return m.revokeErr
}

// mockServiceWithPolicies implements GetPolicyEngine for deeper testing.
type mockServiceWithPolicies struct {
	service.QuicBaaS
	engine    *iam.PolicyEngine
	engineErr error
}

func (m *mockServiceWithPolicies) GetPolicyEngine(ctx context.Context) (*iam.PolicyEngine, error) {
	return m.engine, m.engineErr
}

// mockServiceWithOIDC implements GetOIDCProvider for deeper testing.
type mockServiceWithOIDC struct {
	service.QuicBaaS
	provider    *iam.OIDCProvider
	providerErr error
}

func (m *mockServiceWithOIDC) GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error) {
	return m.provider, m.providerErr
}

// mockServiceWithAudit implements GetAuditLogger for deeper testing.
type mockServiceWithAudit struct {
	service.QuicBaaS
	loggerInst iam.AuditLogger
	loggerErr  error
}

func (m *mockServiceWithAudit) GetAuditLogger(ctx context.Context) (iam.AuditLogger, error) {
	return m.loggerInst, m.loggerErr
}

// mockServiceWithMFA implements GetMFAManager for deeper testing.
type mockServiceWithMFA struct {
	service.QuicBaaS
	mgr    *iam.MFAManager
	mgrErr error
}

func (m *mockServiceWithMFA) GetMFAManager(ctx context.Context) (*iam.MFAManager, error) {
	return m.mgr, m.mgrErr
}

// mockServiceWithGDPR implements GetGDPRManager for deeper testing.
type mockServiceWithGDPR struct {
	service.QuicBaaS
	mgr    *iam.GDPRManager
	mgrErr error
}

func (m *mockServiceWithGDPR) GetGDPRManager(ctx context.Context) (*iam.GDPRManager, error) {
	return m.mgr, m.mgrErr
}

// mockServiceWithApps implements GetAppManager for deeper testing.
type mockServiceWithApps struct {
	service.QuicBaaS
	mgr    *iam.AppManager
	mgrErr error
}

func (m *mockServiceWithApps) GetAppManager(ctx context.Context) (*iam.AppManager, error) {
	return m.mgr, m.mgrErr
}

// mockServiceWithWebAuthn implements GetWebAuthnManager for deeper testing.
// Returns the WebAuthnManager service interface; tests typically pass a nil
// manager to exercise the "manager not initialized" error path.
type mockServiceWithWebAuthn struct {
	service.QuicBaaS
	mgr    service.WebAuthnManager
	mgrErr error
}

func (m *mockServiceWithWebAuthn) GetWebAuthnManager(ctx context.Context) (service.WebAuthnManager, error) {
	return m.mgr, m.mgrErr
}

// mockServiceWithAdmin implements GetAdminManager for deeper testing.
type mockServiceWithAdmin91 struct {
	service.QuicBaaS
	mgr    *iam.AdminManager
	mgrErr error
}

func (m *mockServiceWithAdmin91) GetAdminManager(ctx context.Context) (*iam.AdminManager, error) {
	return m.mgr, m.mgrErr
}

// mockAuditLogger91 implements iam.AuditLogger.
type mockAuditLogger91 struct {
	events   []*iam.AuditEvent
	queryErr error
}

func (m *mockAuditLogger91) Log(ctx context.Context, event *iam.AuditEvent) error {
	m.events = append(m.events, event)
	return nil
}

func (m *mockAuditLogger91) LogAsync(event *iam.AuditEvent) {
	m.events = append(m.events, event)
}

func (m *mockAuditLogger91) Query(ctx context.Context, filter *iam.AuditFilter) ([]*iam.AuditEvent, int, error) {
	if m.queryErr != nil {
		return nil, 0, m.queryErr
	}
	return m.events, len(m.events), nil
}

func (m *mockAuditLogger91) GetEvent(ctx context.Context, tenantID, eventID string) (*iam.AuditEvent, error) {
	for _, e := range m.events {
		if e.ID == eventID {
			return e, nil
		}
	}
	return nil, iam.ErrAuditEventNotFound
}

func (m *mockAuditLogger91) DeleteOlderThan(ctx context.Context, tenantID string, before time.Time) (int, error) {
	return 0, nil
}

func (m *mockAuditLogger91) Close() error {
	return nil
}

// setupHandlerWithService creates an IAMHandler with a custom service.
func setupHandlerWithService(t *testing.T, svc service.QuicBaaS) (*IAMHandler, func()) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	serverConfig := DefaultConfig()
	serverConfig.Address = ":0"
	server, err := NewServer(testutil.NewMockQuicBaaS(), serverConfig, log)
	require.NoError(t, err)
	server.service = svc

	handler := &IAMHandler{server: server}
	return handler, func() { server.Close() }
}

// =============================================================================
// Deep tests: Sessions with mock manager returning errors/nil
// =============================================================================

func TestIAM91_CreateSession_ManagerError(t *testing.T) {
	svc := &mockServiceWithSessions{
		QuicBaaS: testutil.NewMockQuicBaaS(),
		mgrErr:   errors.New("db down"),
	}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	body := `{"token_hash":"abc"}`
	req := systemRequest(http.MethodPost, "/sessions", strings.NewReader(body))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.CreateSession(w, req)
	assert.NotEqual(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_CreateSession_NilManager(t *testing.T) {
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	body := `{"token_hash":"abc"}`
	req := systemRequest(http.MethodPost, "/sessions", strings.NewReader(body))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.CreateSession(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "session manager not initialized")
}

func TestIAM91_CreateSession_Success(t *testing.T) {
	mgr := &mockSessionManager{}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	body := `{"token_hash":"abc","mfa_verified":true,"ip_address":"1.2.3.4","user_agent":"test","device_info":{"id":"d1","name":"phone","type":"mobile","fingerprint":"fp","trusted":true},"expires_at":"2030-01-01T00:00:00Z"}`
	req := systemRequest(http.MethodPost, "/sessions", strings.NewReader(body))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.CreateSession(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestIAM91_CreateSession_ManagerCreateError(t *testing.T) {
	mgr := &mockSessionManager{createErr: errors.New("create failed")}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	body := `{"token_hash":"abc"}`
	req := systemRequest(http.MethodPost, "/sessions", strings.NewReader(body))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.CreateSession(w, req)
	assert.NotEqual(t, http.StatusCreated, w.Code)
}

func TestIAM91_ListUserSessions_NilManager(t *testing.T) {
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/sessions", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.ListUserSessions(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_ListUserSessions_Success(t *testing.T) {
	now := time.Now()
	mgr := &mockSessionManager{sessions: []*iam.SessionExt{
		{ID: "s1", UserID: 30001, TenantID: "t1", Created: now, LastActive: now, ExpiresAt: now.Add(time.Hour),
			DeviceInfo: &iam.DeviceInfo{ID: "d1", Name: "phone"}},
	}}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/sessions", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.ListUserSessions(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIAM91_ListSessions_Success(t *testing.T) {
	now := time.Now()
	mgr := &mockSessionManager{sessions: []*iam.SessionExt{
		{ID: "s1", UserID: 30001, TenantID: "t1", Created: now, LastActive: now, ExpiresAt: now.Add(time.Hour)},
	}}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/sessions", nil)
	ctx := ctxWithTestSubject(req.Context(), "30001", "t1")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ListSessions(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIAM91_GetSession_Success(t *testing.T) {
	now := time.Now()
	mgr := &mockSessionManager{sessions: []*iam.SessionExt{
		{ID: "s1", UserID: 30001, TenantID: "t1", Created: now, LastActive: now, ExpiresAt: now.Add(time.Hour)},
	}}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/sessions/s1", nil)
	req = withChiParams(req, map[string]string{"id": "s1"})
	ctx := ctxWithTestSubject(req.Context(), "30001", "t1")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.GetSession(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIAM91_RevokeSession_Success(t *testing.T) {
	now := time.Now()
	mgr := &mockSessionManager{sessions: []*iam.SessionExt{
		{ID: "s1", UserID: 30001, TenantID: "t1", Created: now, LastActive: now, ExpiresAt: now.Add(time.Hour)},
	}}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/sessions/s1", nil)
	req = withChiParams(req, map[string]string{"id": "s1"})
	ctx := ctxWithTestSubject(req.Context(), "30001", "t1")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.RevokeSession(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestIAM91_RevokeAllSessions_Success(t *testing.T) {
	mgr := &mockSessionManager{}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/sessions", nil)
	ctx := ctxWithTestSubject(req.Context(), "30001", "t1")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.RevokeAllSessions(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestIAM91_RevokeOtherSessions_Success(t *testing.T) {
	now := time.Now()
	mgr := &mockSessionManager{sessions: []*iam.SessionExt{
		{ID: "s1", UserID: 30001, TenantID: "t1", Created: now, LastActive: now, ExpiresAt: now.Add(time.Hour)},
	}}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/sessions/revoke-others",
		strings.NewReader(`{"keep_session_id":"s1"}`))
	ctx := ctxWithTestSubject(req.Context(), "30001", "t1")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.RevokeOtherSessions(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// =============================================================================
// Deep tests: Policies with mock engine returning errors/nil
// =============================================================================

func TestIAM91_ListPolicies_EngineError(t *testing.T) {
	svc := &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS(), engineErr: errors.New("db down")}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/policies", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	handler.ListPolicies(w, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestIAM91_ListPolicies_NilEngine(t *testing.T) {
	svc := &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/policies", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	handler.ListPolicies(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "policy engine not initialized")
}

func TestIAM91_CreatePolicy_InvalidJSON(t *testing.T) {
	svc := &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/policies", strings.NewReader("{bad"))
	req = withChiParams(req, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	handler.CreatePolicy(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAM91_CreatePolicy_NilEngine(t *testing.T) {
	svc := &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	body := `{"apiVersion":"iam.quicbaas.io/v1","kind":"Policy","metadata":{"name":"test-policy","version":"1"},"spec":{"rules":[{"name":"r1","effect":"allow","actions":["read"],"resources":["*"]}]}}`
	req := systemRequest(http.MethodPost, "/policies", strings.NewReader(body))
	req = withChiParams(req, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	handler.CreatePolicy(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_GetPolicy_NilEngine(t *testing.T) {
	svc := &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/policies/p1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "policy_name": "p1"})
	w := httptest.NewRecorder()
	handler.GetPolicy(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_UpdatePolicy_NilEngine(t *testing.T) {
	svc := &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	body := `{"apiVersion":"iam.quicbaas.io/v1","kind":"Policy","metadata":{"name":"test-policy","version":"1"},"spec":{"rules":[{"name":"r1","effect":"allow","actions":["read"],"resources":["*"]}]}}`
	req := systemRequest(http.MethodPut, "/policies/test-policy", strings.NewReader(body))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "policy_name": "test-policy"})
	w := httptest.NewRecorder()
	handler.UpdatePolicy(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_DeletePolicy_NilEngine(t *testing.T) {
	svc := &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/policies/p1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "policy_name": "p1"})
	w := httptest.NewRecorder()
	handler.DeletePolicy(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_CheckAccess_NilEngine(t *testing.T) {
	svc := &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	body := `{"subject":{"id":"u1"},"action":"read","resource":"doc"}`
	req := systemRequest(http.MethodPost, "/pep/check", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.CheckAccess(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_BatchCheckAccess_NilEngine(t *testing.T) {
	svc := &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	body := `{"checks":[{"subject":{"id":"u1"},"action":"read","resource":"doc"}]}`
	req := systemRequest(http.MethodPost, "/pep/batch-check", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.BatchCheckAccess(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_ValidatePolicy_NilEngine(t *testing.T) {
	svc := &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	body := `{"metadata":{"name":"test","version":"1"},"rules":[{"effect":"allow","actions":["read"],"resources":["*"]}]}`
	req := systemRequest(http.MethodPost, "/policies/validate", strings.NewReader(body))
	req = withChiParams(req, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	handler.ValidatePolicy(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_TestPolicy_NilEngine(t *testing.T) {
	svc := &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	body := `{"policy":{"metadata":{"name":"test","version":"1"},"rules":[{"effect":"allow","actions":["read"],"resources":["*"]}]},"request":{"subject":{"id":"u1"},"action":"read","resource":"doc"}}`
	req := systemRequest(http.MethodPost, "/policies/test", strings.NewReader(body))
	req = withChiParams(req, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	handler.TestPolicy(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_SimulatePolicy_NilEngine(t *testing.T) {
	svc := &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	body := `{"policy":{"apiVersion":"iam.quicbaas.io/v1","kind":"Policy","metadata":{"name":"test"},"spec":{"rules":[{"name":"r1","effect":"allow","actions":["read"],"resources":["*"]}]}},"check_request":{"subject":{"id":"u1"},"action":"read","resource":"doc"}}`
	req := systemRequest(http.MethodPost, "/policies/simulate", strings.NewReader(body))
	req = withChiParams(req, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	handler.SimulatePolicy(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_GetEffectivePermissions_NilEngine(t *testing.T) {
	svc := &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	body := `{"subject":{"id":"u1","name":"user1"},"resource_patterns":["orders://*"]}`
	req := systemRequest(http.MethodPost, "/policies/effective", strings.NewReader(body))
	req = withChiParams(req, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	handler.GetEffectivePermissions(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// =============================================================================
// Deep tests: OIDC with mock provider returning errors/nil
// =============================================================================

func TestIAM91_GetOIDCConfig_ProviderError(t *testing.T) {
	svc := &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS(), providerErr: errors.New("init failed")}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
	w := httptest.NewRecorder()
	handler.GetOIDCConfig(w, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestIAM91_GetOIDCConfig_NilProvider(t *testing.T) {
	svc := &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
	w := httptest.NewRecorder()
	handler.GetOIDCConfig(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "OIDC provider not initialized")
}

func TestIAM91_GetJWKS_NilProvider(t *testing.T) {
	svc := &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	w := httptest.NewRecorder()
	handler.GetJWKS(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "OIDC provider not initialized")
}

func TestIAM91_Authorize_NilProvider(t *testing.T) {
	svc := &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/oauth2/authorize?client_id=c1&redirect_uri=https://x.com/cb&response_type=code", nil)
	w := httptest.NewRecorder()
	handler.Authorize(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_Token_NilProvider(t *testing.T) {
	svc := &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/oauth2/token", strings.NewReader("grant_type=authorization_code"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Token(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_UserInfo_NilProvider(t *testing.T) {
	svc := &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/oauth2/userinfo", nil)
	req.Header.Set("Authorization", "Bearer token123")
	w := httptest.NewRecorder()
	handler.UserInfo(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_Revoke_NilProvider(t *testing.T) {
	svc := &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/oauth2/revoke", strings.NewReader("token=abc123"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Revoke(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_UserInfo_InvalidAuthFormat(t *testing.T) {
	svc := &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/oauth2/userinfo", nil)
	req.Header.Set("Authorization", "Basic abc")
	w := httptest.NewRecorder()
	handler.UserInfo(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIAM91_Revoke_MissingToken(t *testing.T) {
	svc := &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/oauth2/revoke", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Revoke(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// =============================================================================
// Deep tests: Audit with mock logger
// =============================================================================

func TestIAM91_ListAuditEvents_NilLogger(t *testing.T) {
	svc := &mockServiceWithAudit{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/audit?tenant_id=t1", nil)
	req = req.WithContext(tenantctx.WithTenant(req.Context(), "t1"))
	w := httptest.NewRecorder()
	handler.ListAuditEvents(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "audit logger not initialized")
}

func TestIAM91_ListAuditEvents_Success(t *testing.T) {
	al := &mockAuditLogger91{events: []*iam.AuditEvent{{ID: "e1", TenantID: "t1"}}}
	svc := &mockServiceWithAudit{QuicBaaS: testutil.NewMockQuicBaaS(), loggerInst: al}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/audit?tenant_id=t1", nil)
	req = req.WithContext(tenantctx.WithTenant(req.Context(), "t1"))
	w := httptest.NewRecorder()
	handler.ListAuditEvents(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIAM91_GetAuditEvent_Success(t *testing.T) {
	al := &mockAuditLogger91{events: []*iam.AuditEvent{{ID: "e1", TenantID: "t1"}}}
	svc := &mockServiceWithAudit{QuicBaaS: testutil.NewMockQuicBaaS(), loggerInst: al}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/audit/e1?tenant_id=t1", nil)
	req = withChiParams(req, map[string]string{"id": "e1"})
	req = req.WithContext(tenantctx.WithTenant(req.Context(), "t1"))
	w := httptest.NewRecorder()
	handler.GetAuditEvent(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIAM91_GetAuditEvent_NotFound(t *testing.T) {
	al := &mockAuditLogger91{events: []*iam.AuditEvent{}}
	svc := &mockServiceWithAudit{QuicBaaS: testutil.NewMockQuicBaaS(), loggerInst: al}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/audit/missing?tenant_id=t1", nil)
	req = withChiParams(req, map[string]string{"id": "missing"})
	req = req.WithContext(tenantctx.WithTenant(req.Context(), "t1"))
	w := httptest.NewRecorder()
	handler.GetAuditEvent(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// =============================================================================
// Deep tests: MFA with mock returning error/nil
// =============================================================================

func TestIAM91_getMFAManager_Error(t *testing.T) {
	svc := &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errors.New("mfa unavailable")}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	mgr, err := handler.getMFAManager(tenantctx.WithSystem(context.Background()))
	assert.Nil(t, mgr)
	assert.Error(t, err)
}

func TestIAM91_getMFAManager_NilManager(t *testing.T) {
	svc := &mockServiceWithMFA{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	mgr, err := handler.getMFAManager(tenantctx.WithSystem(context.Background()))
	assert.Nil(t, mgr)
	assert.ErrorIs(t, err, ErrMFANotAvailable)
}

// =============================================================================
// Deep tests: GDPR with mock returning error/nil
// =============================================================================

func TestIAM91_GrantConsent_NilManager(t *testing.T) {
	svc := &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	body := `{"purpose":"marketing","version":"1"}`
	req := systemRequest(http.MethodPost, "/consent", strings.NewReader(body))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.GrantConsent(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_GrantConsent_ManagerError(t *testing.T) {
	svc := &mockServiceWithGDPR{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errors.New("gdpr down")}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	body := `{"purpose":"marketing","version":"1"}`
	req := systemRequest(http.MethodPost, "/consent", strings.NewReader(body))
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "30001"})
	w := httptest.NewRecorder()
	handler.GrantConsent(w, req)
	assert.NotEqual(t, http.StatusBadRequest, w.Code)
}

// =============================================================================
// Deep tests: Apps with mock returning error/nil
// =============================================================================

func TestIAM91_ListApps_NilManager(t *testing.T) {
	svc := &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/apps", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	handler.ListApps(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_RegisterApp_NilManager(t *testing.T) {
	svc := &mockServiceWithApps{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	body := `{"name":"myapp","redirect_uris":["https://x.com"]}`
	req := systemRequest(http.MethodPost, "/apps", strings.NewReader(body))
	req = withChiParams(req, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	handler.RegisterApp(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// =============================================================================
// Deep tests: Admin with mock returning error/nil
// =============================================================================

func TestIAM91_ImpersonationMiddleware_ManagerError(t *testing.T) {
	svc := &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS(), mgrErr: errors.New("admin down")}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := systemRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Impersonation-Session-ID", "session-123")
	w := httptest.NewRecorder()
	handler.ImpersonationMiddleware(next).ServeHTTP(w, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestIAM91_ImpersonationMiddleware_NoHeader_WithAdmin(t *testing.T) {
	svc := &mockServiceWithAdmin91{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Without the impersonation header, middleware passes through to next handler
	req := systemRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ImpersonationMiddleware(next).ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// =============================================================================
// Deep tests: JWTMiddleware with OIDC provider error/nil
// =============================================================================

func TestIAM91_JWTMiddleware_ProviderError(t *testing.T) {
	svc := &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS(), providerErr: errors.New("oidc down")}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := systemRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	w := httptest.NewRecorder()
	handler.JWTMiddleware(next).ServeHTTP(w, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

// =============================================================================
// Deep tests: ABACMiddleware with policy engine
// =============================================================================

func TestIAM91_ABACMiddleware_EngineError(t *testing.T) {
	svc := &mockServiceWithPolicies{QuicBaaS: testutil.NewMockQuicBaaS(), engineErr: errors.New("engine down")}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := systemRequest(http.MethodGet, "/protected", nil)
	ctx := context.WithValue(req.Context(), ContextKeyUser, &iam.UserInfoResponse{Sub: "u1"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ABACMiddleware("resource", "read")(next).ServeHTTP(w, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

// =============================================================================
// Deep tests: OAuth metadata
// =============================================================================

func TestIAM91_GetOAuthASMetadata_ServiceNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	w := httptest.NewRecorder()
	handler.GetOAuthASMetadata(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAM91_GetOAuthASMetadata_NilProvider(t *testing.T) {
	svc := &mockServiceWithOIDC{QuicBaaS: testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	w := httptest.NewRecorder()
	handler.GetOAuthASMetadata(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestIAM91_GetOAuthASMetadata_ProviderLookupError covers the branch where the
// service exposes GetOIDCProvider but the lookup itself fails: the handler must
// route through handleServiceError rather than emit metadata.
func TestIAM91_GetOAuthASMetadata_ProviderLookupError(t *testing.T) {
	svc := &mockServiceWithOIDC{
		QuicBaaS:    testutil.NewMockQuicBaaS(),
		providerErr: errors.New("oidc provider lookup failed"),
	}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	w := httptest.NewRecorder()
	handler.GetOAuthASMetadata(w, req)
	assert.GreaterOrEqual(t, w.Code, http.StatusInternalServerError)
}
