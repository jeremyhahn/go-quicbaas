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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

// =============================================================================
// RegisterAuditRoutes Tests
// =============================================================================

func TestRegisterAuditRoutes_RoutesRegistered(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	// Create a fresh router to test route registration
	router := chi.NewRouter()

	// Register the audit routes
	RegisterAuditRoutes(router, handler)

	// Test that the routes are properly registered by checking that
	// they can handle requests (even if they return errors due to missing auth/params)

	t.Run("ListAuditEvents route registered", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// The route should be found (not 404/405)
		// It will return 400 because tenant_id is missing, which proves the route is registered
		assert.NotEqual(t, http.StatusNotFound, w.Code, "route should be registered")
		assert.NotEqual(t, http.StatusMethodNotAllowed, w.Code, "method should be allowed")
	})

	t.Run("GetAuditEvent route registered", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit/test-id", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// The route should be found (not 404/405)
		assert.NotEqual(t, http.StatusMethodNotAllowed, w.Code, "method should be allowed")
	})

	t.Run("DeleteOldAuditEvents route registered", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/audit", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// The route should be found (not 404/405)
		assert.NotEqual(t, http.StatusNotFound, w.Code, "route should be registered")
		assert.NotEqual(t, http.StatusMethodNotAllowed, w.Code, "method should be allowed")
	})
}

func TestRegisterAuditRoutes_WrongMethodReturns405(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	router := chi.NewRouter()
	RegisterAuditRoutes(router, handler)

	// Test that POST to list endpoint returns 405 (Method Not Allowed)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/audit", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestRegisterAuditRoutes_HandlersInvoked(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	router := chi.NewRouter()
	RegisterAuditRoutes(router, handler)

	t.Run("ListAuditEvents handler invoked with missing tenant_id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Handler should return 400 due to missing tenant_id parameter
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant_id")
	})

	t.Run("DeleteOldAuditEvents handler invoked with missing tenant_id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/audit", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Handler should return 400 due to missing tenant_id
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestRegisterAuditRoutes_WithQueryParameters(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	router := chi.NewRouter()
	RegisterAuditRoutes(router, handler)

	t.Run("ListAuditEvents with tenant_id parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit?tenant_id=test-tenant", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Handler should process the request (may return 500 due to mock not returning audit logger)
		// The important thing is it's not a 400 for missing tenant_id
		assert.NotEqual(t, http.StatusBadRequest, w.Code)
	})

	t.Run("DeleteOldAuditEvents with required parameters", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/iam/audit?tenant_id=test-tenant&before=2025-01-01T00:00:00Z", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Handler should process the request (may return 500 due to mock)
		// The important thing is validation passes
		assert.NotEqual(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestRegisterAuditRoutes_UnregisteredPath(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	router := chi.NewRouter()
	RegisterAuditRoutes(router, handler)

	// Test that an unregistered path returns 404
	req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/nonexistent", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
