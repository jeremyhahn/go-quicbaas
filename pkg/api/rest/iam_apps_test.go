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

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

// TestListApps tests the ListApps handler.
func TestListApps(t *testing.T) {
	t.Run("missing tenant_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants//apps", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.ListApps(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("app manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/apps", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.ListApps(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "app manager not available")
	})

	t.Run("with pagination parameters", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/apps?limit=50&offset=10", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.ListApps(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "app manager not available")
	})
}

// TestRegisterApp tests the RegisterApp handler.
func TestRegisterApp(t *testing.T) {
	t.Run("missing tenant_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//apps", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.RegisterApp(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/apps", strings.NewReader("invalid json"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.RegisterApp(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("missing application name", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"name":"","redirect_uris":["https://example.com/callback"]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/apps", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.RegisterApp(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "application name is required")
	})

	t.Run("missing redirect URIs", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"name":"Test App","redirect_uris":[]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/apps", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.RegisterApp(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "at least one redirect URI is required")
	})

	t.Run("app manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"name":"Test App","redirect_uris":["https://example.com/callback"],"pkce_required":true}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/apps", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.RegisterApp(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "app manager not available")
	})
}

// TestGetApp tests the GetApp handler.
func TestGetApp(t *testing.T) {
	t.Run("missing tenant_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("client_id", "client-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants//apps/client-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetApp(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing client_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("client_id", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/apps/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetApp(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "client ID is required")
	})

	t.Run("app manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("client_id", "client-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/apps/client-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetApp(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "app manager not available")
	})
}

// TestUpdateApp tests the UpdateApp handler.
func TestUpdateApp(t *testing.T) {
	t.Run("missing tenant_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("client_id", "client-123")

		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants//apps/client-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.UpdateApp(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing client_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("client_id", "")

		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant-123/apps/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.UpdateApp(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "client ID is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("client_id", "client-123")

		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant-123/apps/client-123", strings.NewReader("invalid json"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.UpdateApp(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("app manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("client_id", "client-123")

		body := `{"name":"Updated App","redirect_uris":["https://example.com/callback"]}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant-123/apps/client-123", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.UpdateApp(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "app manager not available")
	})
}

// TestDeleteApp tests the DeleteApp handler.
func TestDeleteApp(t *testing.T) {
	t.Run("missing tenant_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("client_id", "client-123")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants//apps/client-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.DeleteApp(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing client_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("client_id", "")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/apps/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.DeleteApp(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "client ID is required")
	})

	t.Run("app manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("client_id", "client-123")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/apps/client-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.DeleteApp(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "app manager not available")
	})
}

// TestRotateSecret tests the RotateSecret handler.
func TestRotateSecret(t *testing.T) {
	t.Run("missing tenant_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("client_id", "client-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//apps/client-123/rotate-secret", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.RotateSecret(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing client_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("client_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/apps//rotate-secret", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.RotateSecret(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "client ID is required")
	})

	t.Run("app manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("client_id", "client-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/apps/client-123/rotate-secret", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.RotateSecret(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "app manager not available")
	})
}

// TestValidateClient tests the ValidateClient handler.
// Note: ValidateClient does not take a tenant_id URL parameter - it reads client_id/client_secret from the request body.
func TestValidateClient(t *testing.T) {
	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/validate-client", strings.NewReader("invalid json"))
		w := httptest.NewRecorder()

		handler.ValidateClient(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("missing client_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"client_id":"","client_secret":"secret"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/validate-client", strings.NewReader(body))
		w := httptest.NewRecorder()

		handler.ValidateClient(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "client_id is required")
	})

	t.Run("missing client_secret", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"client_id":"client-123","client_secret":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/validate-client", strings.NewReader(body))
		w := httptest.NewRecorder()

		handler.ValidateClient(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "client_secret is required")
	})

	t.Run("app manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"client_id":"client-123","client_secret":"secret"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/validate-client", strings.NewReader(body))
		w := httptest.NewRecorder()

		handler.ValidateClient(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "app manager not available")
	})
}
