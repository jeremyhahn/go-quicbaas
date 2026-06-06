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
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

func TestUpdateUser_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "T"}))
	require.NoError(t, memStorage.CreateUser(ctx, &iam.User{
		ID: "user1", TenantID: "tenant1", Email: "user1@example.com", Name: "User 1",
	}))

	body := `{"name":"Renamed","enabled":true}`
	req := httptest.NewRequest(http.MethodPut, "/x", bytes.NewReader([]byte(body)))
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "user_id": "user1"})
	w := httptest.NewRecorder()
	handler.UpdateUser(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

func TestUpdateUser_NotFound(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()
	require.NoError(t, memStorage.CreateTenant(context.Background(), &iam.Tenant{ID: "tenant1", Name: "T"}))

	req := httptest.NewRequest(http.MethodPut, "/x", bytes.NewReader([]byte(`{"name":"x"}`)))
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "user_id": "missing"})
	w := httptest.NewRecorder()
	handler.UpdateUser(w, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestUpdateUser_Validation(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	// Invalid JSON
	w := httptest.NewRecorder()
	req := withChiParams(httptest.NewRequest(http.MethodPut, "/x", bytes.NewReader([]byte("{bad"))),
		map[string]string{"tenant_id": "tenant1", "user_id": "u1"})
	handler.UpdateUser(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Missing user_id
	w = httptest.NewRecorder()
	req = withChiParams(httptest.NewRequest(http.MethodPut, "/x", bytes.NewReader([]byte(`{}`))),
		map[string]string{"tenant_id": "tenant1", "user_id": ""})
	handler.UpdateUser(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListUsers_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "T"}))
	require.NoError(t, memStorage.CreateUser(ctx, &iam.User{ID: "u1", TenantID: "tenant1", Email: "u1@example.com"}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant1/users?limit=10&offset=0", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1"})
	w := httptest.NewRecorder()
	handler.ListUsers(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}
