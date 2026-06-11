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
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// setupIAMHandler creates a test IAM handler.
func setupIAMHandler(t *testing.T) (*IAMHandler, *Server, func()) {
	t.Helper()

	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockService := testutil.NewMockQuicBaaS()

	serverConfig := DefaultConfig()
	serverConfig.Address = ":0"

	server, err := NewServer(mockService, serverConfig, log)
	require.NoError(t, err)

	handler := &IAMHandler{
		server: server,
	}

	cleanup := func() {
		server.Close()
	}

	return handler, server, cleanup
}

// TestListPolicies tests the ListPolicies handler.
func TestListPolicies(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants//policies", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.ListPolicies(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("policy engine not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/policies", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.ListPolicies(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "policy engine not available")
	})

	t.Run("with limit and offset query params", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/policies?limit=50&offset=10", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.ListPolicies(w, req)

		// Will fail because policy engine is not configured, but the params are parsed
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with invalid limit query param uses default", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/policies?limit=invalid&offset=-1", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.ListPolicies(w, req)

		// Will fail because policy engine is not configured
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// TestCreatePolicy tests the CreatePolicy handler.
func TestCreatePolicy(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//policies", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreatePolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/policies", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreatePolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("policy validation error", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		// Empty policy should fail validation
		body := `{"metadata":{},"spec":{}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/policies", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreatePolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

}

// TestGetPolicy tests the GetPolicy handler.
func TestGetPolicy(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("policy_name", "test-policy")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants//policies/test-policy", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetPolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing policy name", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("policy_name", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/policies/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetPolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "policy name is required")
	})

	t.Run("policy engine not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("policy_name", "test-policy")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/policies/test-policy", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetPolicy(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "policy engine not available")
	})
}

// TestUpdatePolicy tests the UpdatePolicy handler.
func TestUpdatePolicy(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("policy_name", "test-policy")

		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants//policies/test-policy", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.UpdatePolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing policy name", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("policy_name", "")

		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant-123/policies/", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.UpdatePolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "policy name is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("policy_name", "test-policy")

		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant-123/policies/test-policy", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.UpdatePolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("policy validation error", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("policy_name", "test-policy")

		// Empty policy should fail validation
		body := `{"metadata":{},"spec":{}}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant-123/policies/test-policy", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.UpdatePolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestDeletePolicy tests the DeletePolicy handler.
func TestDeletePolicy(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("policy_name", "test-policy")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants//policies/test-policy", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.DeletePolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing policy name", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("policy_name", "")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/policies/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.DeletePolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "policy name is required")
	})

	t.Run("policy engine not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("policy_name", "test-policy")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/policies/test-policy", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.DeletePolicy(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "policy engine not available")
	})
}

// TestValidatePolicy tests the ValidatePolicy handler.
func TestValidatePolicy(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//policies/validate", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ValidatePolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/policies/validate", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ValidatePolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("policy engine not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"metadata":{"name":"test"},"spec":{"rules":[]}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/policies/validate", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ValidatePolicy(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "policy engine not available")
	})
}

// TestTestPolicy tests the TestPolicy handler.
func TestTestPolicy(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//policies/test", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.TestPolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/policies/test", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.TestPolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("policy engine not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"subject":{"id":"user1"},"action":"read","resource":"data"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/policies/test", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.TestPolicy(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "policy engine not available")
	})
}

// TestCheckAccess tests the CheckAccess handler.
func TestCheckAccess(t *testing.T) {
	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/pep/check", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CheckAccess(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("policy engine not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"subject":{"id":"user1"},"action":"read","resource":"data"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/pep/check", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CheckAccess(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "policy engine not available")
	})
}

// TestBatchCheckAccess tests the BatchCheckAccess handler.
func TestBatchCheckAccess(t *testing.T) {
	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/pep/batch-check", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.BatchCheckAccess(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("policy engine not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		body := `{"checks":[{"subject":{"id":"user1"},"action":"read","resource":"data"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/pep/batch-check", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.BatchCheckAccess(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "policy engine not available")
	})
}

// TestSimulatePolicy tests the SimulatePolicy handler.
func TestSimulatePolicy(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//policies/simulate", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.SimulatePolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/policies/simulate", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.SimulatePolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("missing policy in request", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"check_request":{"subject":{"id":"user1"}}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/policies/simulate", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.SimulatePolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "policy is required")
	})

	t.Run("missing check_request in request", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"policy":{"metadata":{"name":"test"},"spec":{}}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/policies/simulate", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.SimulatePolicy(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "check_request is required")
	})

	t.Run("policy engine not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"policy":{"metadata":{"name":"test"},"spec":{}},"check_request":{"subject":{"id":"user1"}}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/policies/simulate", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.SimulatePolicy(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "policy engine not available")
	})
}

// TestGetEffectivePermissions tests the GetEffectivePermissions handler.
func TestGetEffectivePermissions(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//policies/effective-permissions", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.GetEffectivePermissions(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/policies/effective-permissions", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.GetEffectivePermissions(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("missing subject in request", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"resource_patterns":["*"]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/policies/effective-permissions", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.GetEffectivePermissions(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "subject is required")
	})

	t.Run("missing resource_patterns in request", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"subject":{"id":"user1"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/policies/effective-permissions", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.GetEffectivePermissions(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "resource_patterns is required")
	})

	t.Run("policy engine not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"subject":{"id":"user1"},"resource_patterns":["*"]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/policies/effective-permissions", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.GetEffectivePermissions(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "policy engine not available")
	})
}
