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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

func TestListMembers_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"}))
	require.NoError(t, memStorage.CreateGroup(ctx, &iam.Group{
		ID: "group1", TenantID: "tenant1", Name: "Group 1", Members: []string{"u1", "u2"},
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant1/groups/group1/members", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "group_id": "group1"})
	w := httptest.NewRecorder()
	handler.ListMembers(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, float64(2), resp["total"])
}

func TestListMembers_EmptyMembers(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"}))
	require.NoError(t, memStorage.CreateGroup(ctx, &iam.Group{ID: "group1", TenantID: "tenant1", Name: "G"}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "group_id": "group1"})
	w := httptest.NewRecorder()
	handler.ListMembers(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, float64(0), resp["total"])
}

func TestListMembers_Validation(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	// Missing tenant
	w := httptest.NewRecorder()
	req := withChiParams(httptest.NewRequest(http.MethodGet, "/x", nil), map[string]string{"tenant_id": ""})
	handler.ListMembers(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Missing group
	w = httptest.NewRecorder()
	req = withChiParams(httptest.NewRequest(http.MethodGet, "/x", nil),
		map[string]string{"tenant_id": "tenant1", "group_id": ""})
	handler.ListMembers(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListMembers_GroupNotFound(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	require.NoError(t, memStorage.CreateTenant(context.Background(), &iam.Tenant{ID: "tenant1", Name: "T"}))

	req := withChiParams(httptest.NewRequest(http.MethodGet, "/x", nil),
		map[string]string{"tenant_id": "tenant1", "group_id": "missing"})
	w := httptest.NewRecorder()
	handler.ListMembers(w, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}
