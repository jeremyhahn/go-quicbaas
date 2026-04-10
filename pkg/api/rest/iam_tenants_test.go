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

// TestListTenants tests the ListTenants handler.
func TestListTenants(t *testing.T) {
	t.Run("tenant manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
		w := httptest.NewRecorder()

		handler.ListTenants(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "tenant manager not available")
	})

	t.Run("with limit and offset query params", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants?limit=50&offset=10", nil)
		w := httptest.NewRecorder()

		handler.ListTenants(w, req)

		// Should fail because tenant manager is not available
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "tenant manager not available")
	})

	t.Run("with invalid limit", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants?limit=invalid", nil)
		w := httptest.NewRecorder()

		handler.ListTenants(w, req)

		// Invalid limit is ignored, should continue to tenant manager check
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "tenant manager not available")
	})

	t.Run("with invalid offset", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants?offset=invalid", nil)
		w := httptest.NewRecorder()

		handler.ListTenants(w, req)

		// Invalid offset is ignored, should continue to tenant manager check
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "tenant manager not available")
	})

	t.Run("with limit exceeding max", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants?limit=5000", nil)
		w := httptest.NewRecorder()

		handler.ListTenants(w, req)

		// Limit exceeding 1000 is ignored, should continue to tenant manager check
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "tenant manager not available")
	})

	t.Run("with negative offset", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants?offset=-5", nil)
		w := httptest.NewRecorder()

		handler.ListTenants(w, req)

		// Negative offset is ignored, should continue to tenant manager check
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "tenant manager not available")
	})

	t.Run("with zero limit", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants?limit=0", nil)
		w := httptest.NewRecorder()

		handler.ListTenants(w, req)

		// Zero limit is ignored, should continue to tenant manager check
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "tenant manager not available")
	})
}

// TestCreateTenant tests the CreateTenant handler.
func TestCreateTenant(t *testing.T) {
	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateTenant(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"id":"","name":"test"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateTenant(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing tenant name", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"id":"tenant-123","name":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateTenant(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant name is required")
	})

	t.Run("tenant manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"id":"tenant-123","name":"Test Tenant"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateTenant(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "tenant manager not available")
	})

	t.Run("with optional settings and quotas", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"id":"tenant-123","name":"Test Tenant","settings":{"enabled":true},"quotas":{"maxUsers":100}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateTenant(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "tenant manager not available")
	})
}

// TestGetTenant tests the GetTenant handler.
func TestGetTenant(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetTenant(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("tenant manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetTenant(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "tenant manager not available")
	})
}

// TestUpdateTenant tests the UpdateTenant handler.
func TestUpdateTenant(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")

		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.UpdateTenant(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant-123", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.UpdateTenant(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("tenant manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"name":"Updated Tenant"}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant-123", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.UpdateTenant(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "tenant manager not available")
	})

	t.Run("with all optional fields", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"name":"Updated Tenant","settings":{"enabled":false},"quotas":{"maxUsers":200}}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant-123", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.UpdateTenant(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "tenant manager not available")
	})
}

// TestDeleteTenant tests the DeleteTenant handler.
func TestDeleteTenant(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.DeleteTenant(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("tenant manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.DeleteTenant(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "tenant manager not available")
	})
}
