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

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestIAMListUsers tests the ListUsers handler.
func TestIAMListUsers(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants//users", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.ListUsers(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("identity store not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.ListUsers(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "identity store not available")
	})

	t.Run("with limit and offset query params", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users?limit=50&offset=10", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.ListUsers(w, req)

		// Will fail because identity store is not available
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// TestIAMCreateUser tests the CreateUser handler.
func TestIAMCreateUser(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//users", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateUser(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateUser(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("missing email", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"email":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateUser(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "email is required")
	})

	t.Run("identity store not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")

		body := `{"email":"user@example.com","name":"Test User"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.CreateUser(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "identity store not available")
	})

}

// TestIAMGetUser tests the GetUser handler.
func TestIAMGetUser(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "user-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants//users/user-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetUser(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetUser(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("identity store not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant-123/users/user-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetUser(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "identity store not available")
	})
}

// TestIAMUpdateUser tests the UpdateUser handler.
func TestIAMUpdateUser(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "user-123")

		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants//users/user-123", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.UpdateUser(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "")

		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant-123/users/", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.UpdateUser(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant-123/users/user-123", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.UpdateUser(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("identity store not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		body := `{"name":"Updated Name"}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant-123/users/user-123", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.UpdateUser(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "identity store not available")
	})
}

// TestIAMDeleteUser tests the DeleteUser handler.
func TestIAMDeleteUser(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "user-123")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants//users/user-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.DeleteUser(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/users/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.DeleteUser(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("identity store not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/users/user-123", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.DeleteUser(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "identity store not available")
	})
}

// TestIAMAssignRole tests the AssignRole handler.
func TestIAMAssignRole(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "user-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants//users/user-123/roles", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.AssignRole(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users//roles", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.AssignRole(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/roles", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.AssignRole(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("missing role_id", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		body := `{"role_id":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/roles", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.AssignRole(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "role_id is required")
	})

	t.Run("identity store not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		body := `{"role_id":"role-456"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/roles", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.AssignRole(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "identity store not available")
	})

	// Regression: the TS SDK was sending { "role": "..." } but the handler
	// expects { "role_id": "..." }. An empty or absent role_id must always 400.

	t.Run("empty body omitting role_id is rejected with 400", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		// {} decodes successfully but role_id is zero-value — must 400.
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/roles", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.AssignRole(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "role_id is required")
	})

	t.Run("wrong wire field 'role' (old TS SDK format) is rejected with 400", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")

		// Pre-fix, the TS SDK sent { "role": "admin" }. Because the handler
		// decodes into a struct with json:"role_id", "role" is silently ignored
		// and role_id remains empty → must 400. The fix makes the SDK send
		// { "role_id": "admin" } instead.
		body := `{"role":"admin"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-123/users/user-123/roles", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.AssignRole(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "role_id is required")
	})
}

// TestIAMAssignRole_CorrectBodySucceeds is a regression test proving that
// AssignRole returns 204 when the corrected wire body {"role_id":"<uuid>"}
// is sent and a real identity store has the tenant, user, and role seeded.
func TestIAMAssignRole_CorrectBodySucceeds(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "t1", Name: "Tenant 1"})
	_ = memStorage.CreateUser(ctx, &iam.User{ID: "u1", TenantID: "t1", Email: "user@example.com", Name: "User"})
	_ = memStorage.CreateRole(ctx, &iam.Role{ID: "editor", TenantID: "t1", Name: "Editor"})

	body := `{"role_id":"editor"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/t1/users/u1/roles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"tenant_id": "t1", "user_id": "u1"})
	w := httptest.NewRecorder()

	handler.AssignRole(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

// TestIAMRevokeRole tests the RevokeRole handler.
func TestIAMRevokeRole(t *testing.T) {
	t.Run("missing tenant ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("role", "role-456")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants//users/user-123/roles/role-456", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.RevokeRole(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "tenant ID is required")
	})

	t.Run("missing user ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "")
		rctx.URLParams.Add("role", "role-456")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/users//roles/role-456", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.RevokeRole(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "user ID is required")
	})

	t.Run("missing role ID", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("role", "")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/users/user-123/roles/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.RevokeRole(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "role ID is required")
	})

	t.Run("identity store not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", "tenant-123")
		rctx.URLParams.Add("user_id", "user-123")
		rctx.URLParams.Add("role", "role-456")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant-123/users/user-123/roles/role-456", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.RevokeRole(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "identity store not available")
	})
}
