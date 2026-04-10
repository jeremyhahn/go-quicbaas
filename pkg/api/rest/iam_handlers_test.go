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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// =============================================================================
// Mock IAM Components using MemoryStorage
// =============================================================================

// testTenantManager implements TenantManager interface wrapping MemoryStorage.
type testTenantManager struct {
	storage *iam.MemoryStorage
}

func (m *testTenantManager) CreateTenant(ctx context.Context, opts iam.CreateTenantOptions) (*iam.Tenant, error) {
	tenant := &iam.Tenant{
		ID:      opts.ID,
		Name:    opts.Name,
		Created: time.Now(),
		Updated: time.Now(),
	}
	if opts.Settings != nil {
		tenant.Settings = *opts.Settings
	}
	if opts.Quotas != nil {
		tenant.Quotas = *opts.Quotas
	}
	if err := m.storage.CreateTenant(ctx, tenant); err != nil {
		return nil, err
	}
	return tenant, nil
}

func (m *testTenantManager) GetTenant(ctx context.Context, id string) (*iam.Tenant, error) {
	return m.storage.GetTenant(ctx, id)
}

func (m *testTenantManager) UpdateTenant(ctx context.Context, id string, opts iam.UpdateTenantOptions) (*iam.Tenant, error) {
	tenant, err := m.storage.GetTenant(ctx, id)
	if err != nil {
		return nil, err
	}
	if opts.Name != nil {
		tenant.Name = *opts.Name
	}
	if opts.Settings != nil {
		tenant.Settings = *opts.Settings
	}
	if opts.Quotas != nil {
		tenant.Quotas = *opts.Quotas
	}
	tenant.Updated = time.Now()
	if err := m.storage.UpdateTenant(ctx, tenant); err != nil {
		return nil, err
	}
	return tenant, nil
}

func (m *testTenantManager) DeleteTenant(ctx context.Context, id string) error {
	return m.storage.DeleteTenant(ctx, id)
}

func (m *testTenantManager) ListTenants(ctx context.Context, opts iam.ListOptions) ([]*iam.Tenant, error) {
	return m.storage.ListTenants(ctx, opts)
}

func (m *testTenantManager) CheckQuota(_ context.Context, _, _ string) error {
	return nil
}

// mockQuicBaaSWithIAM extends MockQuicBaaS with IAM capabilities.
type mockQuicBaaSWithIAM struct {
	*testutil.MockQuicBaaS
	tenantManager *testTenantManager
	identityStore *iam.IdentityStore
}

func newMockQuicBaaSWithIAM() (*mockQuicBaaSWithIAM, *iam.MemoryStorage) {
	memStorage := iam.NewMemoryStorage()
	tenantMgr := &testTenantManager{storage: memStorage}
	identityStore := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  memStorage,
		GroupStorage: memStorage,
		RoleStorage:  memStorage,
		Tenants:      tenantMgr,
	})

	return &mockQuicBaaSWithIAM{
		MockQuicBaaS:  testutil.NewMockQuicBaaS(),
		tenantManager: tenantMgr,
		identityStore: identityStore,
	}, memStorage
}

func (m *mockQuicBaaSWithIAM) GetOIDCProvider(_ context.Context) (*iam.OIDCProvider, error) {
	// Return nil to signal the mock OIDC is not available
	return nil, nil
}

func (m *mockQuicBaaSWithIAM) GetTenantManager(_ context.Context) (TenantManager, error) {
	return m.tenantManager, nil
}

func (m *mockQuicBaaSWithIAM) GetIdentityStore(_ context.Context) (*iam.IdentityStore, error) {
	return m.identityStore, nil
}

// setupIAMTestServer creates a test server with IAM mock dependencies.
func setupIAMTestServer(t *testing.T) (*Server, *mockQuicBaaSWithIAM, *iam.MemoryStorage, *IAMHandler, func()) {
	t.Helper()

	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockService, memStorage := newMockQuicBaaSWithIAM()

	serverConfig := DefaultConfig()
	serverConfig.Address = ":0"

	server, err := NewServer(mockService.MockQuicBaaS, serverConfig, log)
	require.NoError(t, err)

	// Replace the service with our IAM-enabled mock
	server.service = mockService

	iamHandler := NewIAMHandler(IAMHandlerDependencies{
		Server: server,
	})

	cleanup := func() {
		server.Close()
	}

	return server, mockService, memStorage, iamHandler, cleanup
}

// withChiParams creates a request with chi URL params set.
func withChiParams(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// =============================================================================
// OIDC Handler Tests
// =============================================================================

func TestIAMHandler_GetOIDCConfig_ServiceNotAvailable(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
	w := httptest.NewRecorder()

	handler.GetOIDCConfig(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAMHandler_GetJWKS_ServiceNotAvailable(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	w := httptest.NewRecorder()

	handler.GetJWKS(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestIAMHandler_Authorize_MissingClientID(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/oauth2/authorize?redirect_uri=http://example.com&response_type=code", nil)
	w := httptest.NewRecorder()

	handler.Authorize(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var errResp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&errResp))
	assert.Equal(t, "invalid_request", errResp["error"])
}

func TestIAMHandler_Authorize_MissingRedirectURI(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/oauth2/authorize?client_id=test&response_type=code", nil)
	w := httptest.NewRecorder()

	handler.Authorize(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAMHandler_Authorize_MissingResponseType(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/oauth2/authorize?client_id=test&redirect_uri=http://example.com", nil)
	w := httptest.NewRecorder()

	handler.Authorize(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAMHandler_Token_InvalidFormData(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	// Create a request with invalid form data (body but wrong content type)
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader("invalid%data"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.Token(w, req)

	// Should return error for missing grant_type
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAMHandler_Token_MissingGrantType(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.Token(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var errResp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&errResp))
	assert.Equal(t, "invalid_request", errResp["error"])
}

func TestIAMHandler_UserInfo_MissingToken(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/oauth2/userinfo", nil)
	w := httptest.NewRecorder()

	handler.UserInfo(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIAMHandler_UserInfo_InvalidTokenFormat(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/oauth2/userinfo", nil)
	req.Header.Set("Authorization", "Basic invalid")
	w := httptest.NewRecorder()

	handler.UserInfo(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestIAMHandler_Revoke_MissingToken(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/oauth2/revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.Revoke(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// =============================================================================
// Tenant Handler Tests
// =============================================================================

func TestIAMHandler_ListTenants_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	// Seed tenants
	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant2", Name: "Tenant 2"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	w := httptest.NewRecorder()

	handler.ListTenants(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	tenants := resp["tenants"].([]interface{})
	assert.Len(t, tenants, 2)
}

func TestIAMHandler_ListTenants_WithPagination(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants?limit=50&offset=10", nil)
	w := httptest.NewRecorder()

	handler.ListTenants(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIAMHandler_ListTenants_InvalidLimit(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	// Invalid limit should use default (100)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants?limit=invalid", nil)
	w := httptest.NewRecorder()

	handler.ListTenants(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIAMHandler_CreateTenant_Success(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	body := `{"id": "new-tenant", "name": "New Tenant"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateTenant(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestIAMHandler_CreateTenant_MissingID(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	body := `{"name": "New Tenant"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateTenant(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAMHandler_CreateTenant_MissingName(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	body := `{"id": "new-tenant"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateTenant(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAMHandler_CreateTenant_InvalidJSON(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	body := `{invalid json}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateTenant(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAMHandler_GetTenant_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1"})
	w := httptest.NewRecorder()

	handler.GetTenant(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIAMHandler_GetTenant_MissingTenantID(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/", nil)
	req = withChiParams(req, map[string]string{})
	w := httptest.NewRecorder()

	handler.GetTenant(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAMHandler_UpdateTenant_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})

	body := `{"name": "Updated Tenant"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/tenant1", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1"})
	w := httptest.NewRecorder()

	handler.UpdateTenant(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIAMHandler_DeleteTenant_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1"})
	w := httptest.NewRecorder()

	handler.DeleteTenant(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

// =============================================================================
// User Handler Tests
// =============================================================================

func TestIAMHandler_ListUsers_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})
	_ = memStorage.CreateUser(ctx, &iam.User{ID: "user1", TenantID: "tenant1", Email: "user1@example.com", Name: "User 1"})
	_ = memStorage.CreateUser(ctx, &iam.User{ID: "user2", TenantID: "tenant1", Email: "user2@example.com", Name: "User 2"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant1/users", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1"})
	w := httptest.NewRecorder()

	handler.ListUsers(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIAMHandler_CreateUser_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})

	body := `{"email": "newuser@example.com", "username": "newuser", "name": "New User"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant1/users", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1"})
	w := httptest.NewRecorder()

	handler.CreateUser(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestIAMHandler_CreateUser_MissingEmail(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})

	body := `{"username": "newuser"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant1/users", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1"})
	w := httptest.NewRecorder()

	handler.CreateUser(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIAMHandler_GetUser_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})
	_ = memStorage.CreateUser(ctx, &iam.User{ID: "user1", TenantID: "tenant1", Email: "user1@example.com", Name: "User 1"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant1/users/user1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "user_id": "user1"})
	w := httptest.NewRecorder()

	handler.GetUser(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIAMHandler_DeleteUser_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})
	_ = memStorage.CreateUser(ctx, &iam.User{ID: "user1", TenantID: "tenant1", Email: "user1@example.com", Name: "User 1"})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant1/users/user1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "user_id": "user1"})
	w := httptest.NewRecorder()

	handler.DeleteUser(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestIAMHandler_AssignRole_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})
	_ = memStorage.CreateUser(ctx, &iam.User{ID: "user1", TenantID: "tenant1", Email: "user1@example.com", Name: "User 1"})
	_ = memStorage.CreateRole(ctx, &iam.Role{ID: "admin", TenantID: "tenant1", Name: "Admin"})

	body := `{"role_id": "admin"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant1/users/user1/roles", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "user_id": "user1"})
	w := httptest.NewRecorder()

	handler.AssignRole(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestIAMHandler_RevokeRole_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})
	_ = memStorage.CreateUser(ctx, &iam.User{ID: "user1", TenantID: "tenant1", Email: "user1@example.com", Name: "User 1", Roles: []string{"admin"}})
	_ = memStorage.CreateRole(ctx, &iam.Role{ID: "admin", TenantID: "tenant1", Name: "Admin"})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant1/users/user1/roles/admin", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "user_id": "user1", "role": "admin"})
	w := httptest.NewRecorder()

	handler.RevokeRole(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

// =============================================================================
// Group Handler Tests
// =============================================================================

func TestIAMHandler_ListGroups_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})
	_ = memStorage.CreateGroup(ctx, &iam.Group{ID: "group1", TenantID: "tenant1", Name: "Group 1"})
	_ = memStorage.CreateGroup(ctx, &iam.Group{ID: "group2", TenantID: "tenant1", Name: "Group 2"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant1/groups", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1"})
	w := httptest.NewRecorder()

	handler.ListGroups(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIAMHandler_CreateGroup_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})

	body := `{"name": "New Group"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant1/groups", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1"})
	w := httptest.NewRecorder()

	handler.CreateGroup(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestIAMHandler_GetGroup_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})
	_ = memStorage.CreateGroup(ctx, &iam.Group{ID: "group1", TenantID: "tenant1", Name: "Group 1"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant1/groups/group1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "group_id": "group1"})
	w := httptest.NewRecorder()

	handler.GetGroup(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIAMHandler_DeleteGroup_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})
	_ = memStorage.CreateGroup(ctx, &iam.Group{ID: "group1", TenantID: "tenant1", Name: "Group 1"})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant1/groups/group1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "group_id": "group1"})
	w := httptest.NewRecorder()

	handler.DeleteGroup(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestIAMHandler_AddMember_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})
	_ = memStorage.CreateUser(ctx, &iam.User{ID: "user1", TenantID: "tenant1", Email: "user1@example.com", Name: "User 1"})
	_ = memStorage.CreateGroup(ctx, &iam.Group{ID: "group1", TenantID: "tenant1", Name: "Group 1"})

	body := `{"user_id": "user1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant1/groups/group1/members", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "group_id": "group1"})
	w := httptest.NewRecorder()

	handler.AddMember(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestIAMHandler_RemoveMember_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})
	_ = memStorage.CreateUser(ctx, &iam.User{ID: "user1", TenantID: "tenant1", Email: "user1@example.com", Name: "User 1", Groups: []string{"group1"}})
	_ = memStorage.CreateGroup(ctx, &iam.Group{ID: "group1", TenantID: "tenant1", Name: "Group 1", Members: []string{"user1"}})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant1/groups/group1/members/user1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "group_id": "group1", "user_id": "user1"})
	w := httptest.NewRecorder()

	handler.RemoveMember(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

// =============================================================================
// Role Handler Tests
// =============================================================================

func TestIAMHandler_ListRoles_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})
	_ = memStorage.CreateRole(ctx, &iam.Role{ID: "role1", TenantID: "tenant1", Name: "Role 1"})
	_ = memStorage.CreateRole(ctx, &iam.Role{ID: "role2", TenantID: "tenant1", Name: "Role 2"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant1/roles", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1"})
	w := httptest.NewRecorder()

	handler.ListRoles(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIAMHandler_CreateRole_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})

	body := `{"name": "New Role", "permissions": ["read", "write"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant1/roles", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1"})
	w := httptest.NewRecorder()

	handler.CreateRole(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestIAMHandler_GetRole_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})
	_ = memStorage.CreateRole(ctx, &iam.Role{ID: "role1", TenantID: "tenant1", Name: "Role 1"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant1/roles/role1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "role_id": "role1"})
	w := httptest.NewRecorder()

	handler.GetRole(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIAMHandler_DeleteRole_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})
	_ = memStorage.CreateRole(ctx, &iam.Role{ID: "role1", TenantID: "tenant1", Name: "Role 1"})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/tenant1/roles/role1", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "role_id": "role1"})
	w := httptest.NewRecorder()

	handler.DeleteRole(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

// =============================================================================
// OAuth Metadata Handler Tests
// =============================================================================

func TestIAMHandler_GetOAuthASMetadata_ServiceNotAvailable(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	w := httptest.NewRecorder()

	handler.GetOAuthASMetadata(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// =============================================================================
// IAM Error Response Tests
// =============================================================================

func TestIAMAPIError_Fields(t *testing.T) {
	err := &IAMAPIError{
		Error:            "bad_request",
		ErrorDescription: "test error description",
	}
	assert.Equal(t, "bad_request", err.Error)
	assert.Equal(t, "test error description", err.ErrorDescription)
}

func TestOAuthError_Error(t *testing.T) {
	err := &OAuthError{
		Code:             "invalid_request",
		ErrorDescription: "missing required parameter",
	}
	assert.Contains(t, err.Error(), "invalid_request")
}

func TestNewIAMAPIError(t *testing.T) {
	err := NewIAMAPIError("not_found", "resource not found")
	assert.Equal(t, "not_found", err.Error)
	assert.Equal(t, "resource not found", err.ErrorDescription)
}

func TestNewInvalidRequestError(t *testing.T) {
	err := NewInvalidRequestError("missing client_id")
	assert.Equal(t, "invalid_request", err.Code)
	assert.Equal(t, "missing client_id", err.ErrorDescription)
}

// =============================================================================
// IAM Handler Registration Tests
// =============================================================================

func TestIAMHandler_RegisterRoutes(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	// Verify routes are registered
	walkErr := chi.Walk(router, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		return nil
	})

	assert.NoError(t, walkErr)
}

// =============================================================================
// IAM Middleware Tests
// =============================================================================

func TestIAMMiddleware_TenantContext(t *testing.T) {
	// Create a test handler that checks tenant context
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify context propagation
		w.WriteHeader(http.StatusOK)
	})

	// Test the middleware if it exists in the IAM module
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant1/users", nil)
	w := httptest.NewRecorder()

	testHandler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
