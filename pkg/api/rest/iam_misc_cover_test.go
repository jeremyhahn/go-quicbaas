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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func TestValidateToken_Branches(t *testing.T) {
	handler, server, cleanup := setupIAMHandler(t)
	defer cleanup()
	mockSvc := server.service.(*testutil.MockQuicBaaS)

	// Missing token -> 400
	w := httptest.NewRecorder()
	handler.ValidateToken(w, httptest.NewRequest(http.MethodGet, "/api/v1/auth/validate", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Success
	mockSvc.On("ValidateToken", mock.Anything, mock.Anything).Return(
		&service.ValidateTokenResponse{Valid: true, UserID: "u1", ExpiresAt: 1 << 40}, nil).Once()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/validate", nil)
	req.Header.Set("Authorization", "Bearer tok")
	w = httptest.NewRecorder()
	handler.ValidateToken(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"valid":true`)
}

func TestUpdateRole_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "T"}))
	require.NoError(t, memStorage.CreateRole(ctx, &iam.Role{ID: "role1", TenantID: "tenant1", Name: "R"}))

	body := `{"description":"updated"}`
	req := httptest.NewRequest(http.MethodPut, "/x", bytes.NewReader([]byte(body)))
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "role_id": "role1"})
	w := httptest.NewRecorder()
	handler.UpdateRole(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

func TestUpdateGroup_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "T"}))
	require.NoError(t, memStorage.CreateGroup(ctx, &iam.Group{ID: "group1", TenantID: "tenant1", Name: "G"}))

	body := `{"description":"updated"}`
	req := httptest.NewRequest(http.MethodPut, "/x", bytes.NewReader([]byte(body)))
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "group_id": "group1"})
	w := httptest.NewRecorder()
	handler.UpdateGroup(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

func TestGetEffectivePermissions_Success(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()

	body := map[string]interface{}{
		"subject":           map[string]interface{}{"id": "u1", "tenant_id": policyTenant, "authenticated": true},
		"resource_patterns": []string{"/test/*"},
	}
	req := policyRequest(t, http.MethodPost, "/x", body, map[string]string{"tenant_id": policyTenant})
	w := httptest.NewRecorder()
	handler.GetEffectivePermissions(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

func TestCreatePolicy_ParseError(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()

	// Body that fails parsePolicyFromBody (totally invalid JSON) -> 400.
	req := withChiParams(httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader([]byte("{not-json"))),
		map[string]string{"tenant_id": policyTenant})
	w := httptest.NewRecorder()
	handler.CreatePolicy(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdatePolicy_NotFound(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.UpdatePolicy(w, policyRequest(t, http.MethodPut, "/x", validPolicyDoc("missing"),
		map[string]string{"tenant_id": policyTenant, "policy_name": "missing"}))
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestCreateGroup_DuplicateAndDeleteGroup_NotFound(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "T"}))

	// Delete a non-existent group -> non-204.
	w := httptest.NewRecorder()
	req := withChiParams(httptest.NewRequest(http.MethodDelete, "/x", nil),
		map[string]string{"tenant_id": "tenant1", "group_id": "missing"})
	handler.DeleteGroup(w, req)
	assert.NotEqual(t, http.StatusNoContent, w.Code)
}
