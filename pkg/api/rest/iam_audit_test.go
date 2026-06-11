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
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestListAuditEvents tests the ListAuditEvents handler.
func TestListAuditEvents(t *testing.T) {
	t.Run("missing tenant_id parameter", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit", nil)
		w := httptest.NewRecorder()

		handler.ListAuditEvents(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant_id parameter is required")
	})

	t.Run("with valid tenant_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit?tenant_id=tenant-123", nil)
		w := httptest.NewRecorder()

		handler.ListAuditEvents(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "audit logger not available")
	})

	t.Run("with all query parameters", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit?tenant_id=tenant-123&event_type=login&category=authentication&outcome=success&actor_id=user-123&resource_id=session-123&start_time=2024-01-01T00:00:00Z&end_time=2024-12-31T23:59:59Z&limit=50&offset=10", nil)
		w := httptest.NewRecorder()

		handler.ListAuditEvents(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "audit logger not available")
	})

	t.Run("with invalid start_time", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit?tenant_id=tenant-123&start_time=invalid", nil)
		w := httptest.NewRecorder()

		handler.ListAuditEvents(w, req)

		// Should proceed even with invalid time (ignored)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "audit logger not available")
	})

	t.Run("with invalid limit", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit?tenant_id=tenant-123&limit=invalid", nil)
		w := httptest.NewRecorder()

		handler.ListAuditEvents(w, req)

		// Should use default limit
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// TestGetAuditEvent tests the GetAuditEvent handler.
func TestGetAuditEvent(t *testing.T) {
	t.Run("missing event ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetAuditEvent(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "event ID is required")
	})

	t.Run("missing tenant_id parameter", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "event-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit/event-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetAuditEvent(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant_id parameter is required")
	})

	t.Run("audit logger not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "event-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit/event-123?tenant_id=tenant-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetAuditEvent(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "audit logger not available")
	})
}

// TestDeleteOldAuditEvents tests the DeleteOldAuditEvents handler.
func TestDeleteOldAuditEvents(t *testing.T) {
	t.Run("missing tenant_id parameter", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/audit", nil)
		w := httptest.NewRecorder()

		handler.DeleteOldAuditEvents(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant_id parameter is required")
	})

	t.Run("missing before parameter", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/audit?tenant_id=tenant-123", nil)
		w := httptest.NewRecorder()

		handler.DeleteOldAuditEvents(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "before parameter is required")
	})

	t.Run("invalid before parameter", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/audit?tenant_id=tenant-123&before=invalid", nil)
		w := httptest.NewRecorder()

		handler.DeleteOldAuditEvents(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid before timestamp format (use ISO8601/RFC3339)")
	})

	t.Run("user not authenticated", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/audit?tenant_id=tenant-123&before=2024-01-01T00:00:00Z", nil)
		w := httptest.NewRecorder()

		handler.DeleteOldAuditEvents(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "user not authenticated")
	})

	t.Run("admin manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		// Add user info to context
		userInfo := &iam.UserInfoResponse{
			Sub:      "user-123",
			Name:     "Test User",
			Email:    "test@example.com",
			TenantID: "tenant-123",
		}

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/audit?tenant_id=tenant-123&before=2024-01-01T00:00:00Z", nil)
		ctx := context.WithValue(req.Context(), ContextKeyUser, userInfo)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.DeleteOldAuditEvents(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "admin manager not available")
	})
}
