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

// TestGrantConsent tests the GrantConsent handler.
func TestGrantConsent(t *testing.T) {
	t.Run("missing tenant_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "user-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//users/user-123/consents", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GrantConsent(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users//consents", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GrantConsent(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/consents", strings.NewReader("invalid json"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GrantConsent(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("missing purpose", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		body := `{"purpose":"","version":"1.0"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/consents", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GrantConsent(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "purpose is required")
	})

	t.Run("missing version", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		body := `{"purpose":"marketing","version":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/consents", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GrantConsent(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "version is required")
	})

	t.Run("GDPR manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		body := `{"purpose":"marketing","version":"1.0"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/consents", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GrantConsent(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "GDPR manager not available")
	})
}

// TestListUserConsents tests the ListUserConsents handler.
func TestListUserConsents(t *testing.T) {
	t.Run("missing tenant_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "user-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants//users/user-123/consents", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.ListUserConsents(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users//consents", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.ListUserConsents(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("GDPR manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users/user-123/consents", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.ListUserConsents(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "GDPR manager not available")
	})
}

// TestGetConsent tests the GetConsent handler.
func TestGetConsent(t *testing.T) {
	t.Run("missing tenant_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("purpose", "marketing")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants//users/user-123/consents/marketing", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetConsent(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "")
		rctx.URLParams.Add("purpose", "marketing")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users//consents/marketing", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetConsent(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("missing purpose", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("purpose", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users/user-123/consents/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetConsent(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "purpose is required")
	})

	t.Run("GDPR manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("purpose", "marketing")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users/user-123/consents/marketing", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetConsent(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "GDPR manager not available")
	})
}

// TestWithdrawConsent tests the WithdrawConsent handler.
func TestWithdrawConsent(t *testing.T) {
	t.Run("missing tenant_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("purpose", "marketing")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants//users/user-123/consents/marketing", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.WithdrawConsent(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "")
		rctx.URLParams.Add("purpose", "marketing")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/users//consents/marketing", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.WithdrawConsent(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("missing purpose", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("purpose", "")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/users/user-123/consents/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.WithdrawConsent(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "purpose is required")
	})

	t.Run("GDPR manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("purpose", "marketing")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/users/user-123/consents/marketing", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.WithdrawConsent(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "GDPR manager not available")
	})
}

// TestRequestDataExport tests the RequestDataExport handler.
func TestRequestDataExport(t *testing.T) {
	t.Run("missing tenant_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "user-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//users/user-123/data-export", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.RequestDataExport(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users//data-export", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.RequestDataExport(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("GDPR manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		body := `{"format":"json"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/data-export", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.RequestDataExport(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "GDPR manager not available")
	})
}

// TestGetExportStatus tests the GetExportStatus handler.
func TestGetExportStatus(t *testing.T) {
	t.Run("missing tenant_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("export_id", "export-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants//users/user-123/data-export/export-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetExportStatus(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "")
		rctx.URLParams.Add("export_id", "export-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users//data-export/export-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetExportStatus(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("missing export_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("export_id", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users/user-123/data-export/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetExportStatus(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "export ID is required")
	})

	t.Run("GDPR manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("export_id", "export-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users/user-123/data-export/export-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetExportStatus(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "GDPR manager not available")
	})
}

// TestRequestDeletion tests the RequestDeletion handler.
func TestRequestDeletion(t *testing.T) {
	t.Run("missing tenant_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "user-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//users/user-123/deletion", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.RequestDeletion(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users//deletion", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.RequestDeletion(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("GDPR manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		body := `{"reason":"user request"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/deletion", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.RequestDeletion(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "GDPR manager not available")
	})
}

// TestGetDeletionStatus tests the GetDeletionStatus handler.
func TestGetDeletionStatus(t *testing.T) {
	t.Run("missing tenant_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("deletion_id", "deletion-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants//users/user-123/deletion/deletion-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetDeletionStatus(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "")
		rctx.URLParams.Add("deletion_id", "deletion-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users//deletion/deletion-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetDeletionStatus(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("missing deletion_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("deletion_id", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users/user-123/deletion/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetDeletionStatus(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "deletion ID is required")
	})

	t.Run("GDPR manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("deletion_id", "deletion-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users/user-123/deletion/deletion-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetDeletionStatus(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "GDPR manager not available")
	})
}

// TestCancelDeletion tests the CancelDeletion handler.
func TestCancelDeletion(t *testing.T) {
	t.Run("missing tenant_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("deletion_id", "deletion-123")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants//users/user-123/deletion/deletion-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.CancelDeletion(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "")
		rctx.URLParams.Add("deletion_id", "deletion-123")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/users//deletion/deletion-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.CancelDeletion(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("missing deletion_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("deletion_id", "")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/users/user-123/deletion/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.CancelDeletion(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "deletion ID is required")
	})

	t.Run("GDPR manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("deletion_id", "deletion-123")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/users/user-123/deletion/deletion-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.CancelDeletion(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "GDPR manager not available")
	})
}

// TestDownloadExport tests the DownloadExport handler.
func TestDownloadExport(t *testing.T) {
	t.Run("missing tenant_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("export_id", "export-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants//users/user-123/data-export/export-123/download", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.DownloadExport(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "")
		rctx.URLParams.Add("export_id", "export-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users//data-export/export-123/download", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.DownloadExport(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("missing export_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("export_id", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users/user-123/data-export//download", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.DownloadExport(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "export ID is required")
	})

	t.Run("GDPR manager not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("export_id", "export-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users/user-123/data-export/export-123/download", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.DownloadExport(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "GDPR manager not available")
	})
}
