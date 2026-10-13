// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// SPDX-License-Identifier: GPL-3.0-only
// See the LICENSE file in the root of this repository.

package service

import (
	"context"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/iamtest"
)

func (m *mockIAMComponents) GetTenantManager() iam.TenantManager { return m.tenantManager }

func (m *mockIAMComponents) GetIdentityStore() *iam.IdentityStore { return m.identityStore }

func (m *mockIAMComponents) GetPolicyEngine() *iam.PolicyEngine { return m.policyEngine }

func (m *mockIAMComponents) GetAppManager() *iam.AppManager { return m.appManager }

func (m *mockIAMComponents) GetOIDCProvider() *iam.OIDCProvider { return m.oidcProvider }

func (m *mockIAMComponents) GetFederationManager() iam.FederationManager { return m.federationManager }

func (m *mockIAMComponents) GetMFAManager() MFAManager { return m.mfaManager }

func (m *mockIAMComponents) GetWebAuthnManager() WebAuthnManager { return m.webAuthnMgr }

func (m *mockIAMComponents) GetAuditLogger() iam.AuditLogger { return m.auditLogger }

func (m *mockIAMComponents) GetSessionStorage() iam.SessionStorage { return m.sessionStorage }

func (m *mockIAMComponents) GetSessionManager() iam.SessionManager { return m.sessionManager }

func (m *mockIAMComponents) GetInviteStore() iam.InviteStore { return m.inviteStore }

func (m *mockIAMComponents) GetPIVActivator() *iam.PIVActivator { return m.pivActivator }

func (m *mockIAMComponents) GetTenantProvisioner() TenantProvisioner { return m.provisioner }

func (m *mockIAMComponents) GetAdminManager() *iam.AdminManager { return m.adminManager }

func (m *mockIAMComponents) GetGDPRManager() *iam.GDPRManager { return m.gdprManager }

func (m *mockIAMComponents) GetAppStorage() iam.AppStorage { return nil }

func (m *mockTenantManager) CreateTenant(ctx context.Context, opts iam.CreateTenantOptions) (*iam.Tenant, error) {
	if m.createFn != nil {
		return m.createFn(ctx, opts)
	}
	return &iam.Tenant{ID: "t-1", Name: opts.Name}, nil
}

func (m *mockTenantManager) GetTenant(ctx context.Context, id string) (*iam.Tenant, error) {
	if m.getFn != nil {
		return m.getFn(ctx, id)
	}
	return &iam.Tenant{ID: id, Name: "test-tenant"}, nil
}

func (m *mockTenantManager) UpdateTenant(ctx context.Context, id string, opts iam.UpdateTenantOptions) (*iam.Tenant, error) {
	if m.updateFn != nil {
		return m.updateFn(ctx, id, opts)
	}
	return &iam.Tenant{ID: id}, nil
}

func (m *mockTenantManager) DeleteTenant(ctx context.Context, id string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

func (m *mockTenantManager) ListTenants(ctx context.Context, opts iam.ListOptions) ([]*iam.Tenant, error) {
	if m.listFn != nil {
		return m.listFn(ctx, opts)
	}
	return []*iam.Tenant{{ID: "t-1"}}, nil
}

func (m *mockTenantManager) CheckQuota(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockTenantManager) UpdateEngineQuotas(_ context.Context, _ string, _ qrdbsdk.Quotas) error {
	return nil
}

func (m *mockTenantManager) UpdateEngineEntitlements(_ context.Context, _ string, _ qrdbsdk.Entitlements) error {
	return nil
}

// testPolicy creates a test policy with proper struct hierarchy.
func testPolicy(name string) *iam.Policy {
	return &iam.Policy{
		Metadata: iam.PolicyMetadata{Name: name},
	}
}

func TestSanitizeUsername_ValidCharacters(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "alphanumeric with hyphen and underscore",
			input:    "user-name_123",
			expected: "user-name_123",
		},
		{
			name:     "removes special characters",
			input:    "user@name!#$%",
			expected: "username",
		},
		{
			name:     "removes spaces",
			input:    "user name",
			expected: "username",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "all invalid characters",
			input:    "!@#$%^&*()",
			expected: "",
		},
		{
			name:     "unicode characters removed",
			input:    "user\u00e9name",
			expected: "username",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeUsername(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIAMServiceWrapper_CreateTenant_Success(t *testing.T) {
	mockTM := &mockTenantManager{}
	provider := &mockIAMServiceProvider{tenantMgr: mockTM}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.CreateTenant(context.Background(), &CreateTenantRequest{
		Name: "new-tenant",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "new-tenant", resp.Tenant.Name)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestIAMServiceWrapper_CreateTenant_WithID(t *testing.T) {
	mockTM := &mockTenantManager{
		createFn: func(_ context.Context, opts iam.CreateTenantOptions) (*iam.Tenant, error) {
			return &iam.Tenant{ID: opts.ID, Name: opts.Name}, nil
		},
	}
	provider := &mockIAMServiceProvider{tenantMgr: mockTM}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.CreateTenant(context.Background(), &CreateTenantRequest{
		ID:   "custom-id",
		Name: "custom-tenant",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "custom-id", resp.Tenant.ID)
}

func TestIAMServiceWrapper_CreateTenant_ManagerUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{tenantMgr: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.CreateTenant(context.Background(), &CreateTenantRequest{Name: "t"})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "tenant manager not available")
}

func TestIAMServiceWrapper_CreateTenant_ProviderError(t *testing.T) {
	provider := &mockIAMServiceProvider{
		tenantMgrErr: &testIAMError{msg: "provider failed"},
	}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.CreateTenant(context.Background(), &CreateTenantRequest{Name: "t"})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_GetTenant_Success(t *testing.T) {
	mockTM := &mockTenantManager{}
	provider := &mockIAMServiceProvider{tenantMgr: mockTM}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.GetTenant(context.Background(), &GetTenantRequest{ID: "t-1"})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "t-1", resp.Tenant.ID)
}

func TestIAMServiceWrapper_GetTenant_ManagerUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{tenantMgr: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.GetTenant(context.Background(), &GetTenantRequest{ID: "t-1"})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "tenant manager not available")
}

func TestIAMServiceWrapper_UpdateTenant_Success(t *testing.T) {
	mockTM := &mockTenantManager{}
	provider := &mockIAMServiceProvider{tenantMgr: mockTM}
	wrapper := &iamServiceWrapper{provider: provider}

	name := "updated-name"
	resp, err := wrapper.UpdateTenant(context.Background(), &UpdateTenantRequest{
		ID:   "t-1",
		Name: &name,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "t-1", resp.Tenant.ID)
}

func TestIAMServiceWrapper_UpdateTenant_ManagerUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{tenantMgr: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.UpdateTenant(context.Background(), &UpdateTenantRequest{ID: "t-1"})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_DeleteTenant_Success(t *testing.T) {
	mockTM := &mockTenantManager{}
	provider := &mockIAMServiceProvider{tenantMgr: mockTM}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.DeleteTenant(context.Background(), &DeleteTenantRequest{ID: "t-1"})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Deleted)
}

func TestIAMServiceWrapper_DeleteTenant_ManagerUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{tenantMgr: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.DeleteTenant(context.Background(), &DeleteTenantRequest{ID: "t-1"})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_ListTenants_Success(t *testing.T) {
	mockTM := &mockTenantManager{}
	provider := &mockIAMServiceProvider{tenantMgr: mockTM}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.ListTenants(context.Background(), &ListTenantsRequest{Limit: 10})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, int64(1), resp.TotalCount)
}

func TestIAMServiceWrapper_ListTenants_ManagerUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{tenantMgr: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.ListTenants(context.Background(), &ListTenantsRequest{})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_CreateUser_IdentityStoreUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{identityStore: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.CreateUser(context.Background(), &IAMCreateUserRequest{
		Email: "user@example.com",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "identity store not available")
}

func TestIAMServiceWrapper_CreateUser_ProviderError(t *testing.T) {
	provider := &mockIAMServiceProvider{
		identityStoreErr: &testIAMError{msg: "identity store error"},
	}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.CreateUser(context.Background(), &IAMCreateUserRequest{
		Email: "user@example.com",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_GetUser_IdentityStoreUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{identityStore: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.GetUser(context.Background(), &IAMGetUserRequest{
		TenantID: "t-1",
		UserID:   "u-1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "identity store not available")
}

func TestIAMServiceWrapper_UpdateUser_IdentityStoreUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{identityStore: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.UpdateUser(context.Background(), &IAMUpdateUserRequest{
		TenantID: "t-1",
		UserID:   "u-1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_DeleteUser_IdentityStoreUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{identityStore: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.DeleteUser(context.Background(), &IAMDeleteUserRequest{
		TenantID: "t-1",
		UserID:   "u-1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_ListUsers_IdentityStoreUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{identityStore: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.ListUsers(context.Background(), &IAMListUsersRequest{
		TenantID: "t-1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_CreateRole_IdentityStoreUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{identityStore: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.CreateRole(context.Background(), &IAMCreateRoleRequest{
		TenantID: "t-1",
		Name:     "admin",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_GetRole_IdentityStoreUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{identityStore: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.GetRole(context.Background(), &IAMGetRoleRequest{
		TenantID: "t-1",
		RoleID:   "r-1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_UpdateRole_IdentityStoreUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{identityStore: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.UpdateRole(context.Background(), &IAMUpdateRoleRequest{
		TenantID: "t-1",
		RoleID:   "r-1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_DeleteRole_IdentityStoreUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{identityStore: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.DeleteRole(context.Background(), &IAMDeleteRoleRequest{
		TenantID: "t-1",
		RoleID:   "r-1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_ListRoles_IdentityStoreUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{identityStore: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.ListRoles(context.Background(), &IAMListRolesRequest{
		TenantID: "t-1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_AssignRole_ReturnsNotSupported(t *testing.T) {
	provider := &mockIAMServiceProvider{}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.AssignRole(context.Background(), &AssignRoleRequest{
		Username: "user1",
		RoleName: "admin",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "IAM AssignRole requires TenantID")
}

func TestIAMServiceWrapper_RevokeRole_ReturnsNotSupported(t *testing.T) {
	provider := &mockIAMServiceProvider{}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.RevokeRole(context.Background(), &RevokeRoleRequest{
		Username: "user1",
		RoleName: "admin",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "IAM RevokeRole requires TenantID")
}

func TestIAMServiceWrapper_RegisterApp_AppManagerUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{appMgr: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.RegisterApp(context.Background(), &RegisterAppRequest{
		TenantID: "t-1",
		Name:     "my-app",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "app manager not available")
}

func TestIAMServiceWrapper_RegisterApp_ProviderError(t *testing.T) {
	provider := &mockIAMServiceProvider{
		appMgrErr: &testIAMError{msg: "app manager error"},
	}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.RegisterApp(context.Background(), &RegisterAppRequest{
		TenantID: "t-1",
		Name:     "my-app",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_GetApp_AppManagerUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{appMgr: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.GetApp(context.Background(), &GetAppRequest{ClientID: "client-1"})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_UpdateApp_AppManagerUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{appMgr: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.UpdateApp(context.Background(), &UpdateAppRequest{ClientID: "client-1"})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_DeleteApp_AppManagerUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{appMgr: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.DeleteApp(context.Background(), &DeleteAppRequest{ClientID: "client-1"})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_ListApps_AppManagerUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{appMgr: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.ListApps(context.Background(), &ListAppsRequest{TenantID: "t-1"})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_RotateAppSecret_AppManagerUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{appMgr: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.RotateAppSecret(context.Background(), &RotateAppSecretRequest{ClientID: "client-1"})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_CreatePolicy_PolicyEngineUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{policyEngine: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.CreatePolicy(context.Background(), &CreatePolicyRequest{
		TenantID: "t-1",
		Policy:   testPolicy("test-policy"),
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "policy engine not available")
}

func TestIAMServiceWrapper_CreatePolicy_ProviderError(t *testing.T) {
	provider := &mockIAMServiceProvider{
		policyEngineErr: &testIAMError{msg: "policy engine error"},
	}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.CreatePolicy(context.Background(), &CreatePolicyRequest{
		TenantID: "t-1",
		Policy:   testPolicy("test-policy"),
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_GetPolicy_PolicyEngineUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{policyEngine: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.GetPolicy(context.Background(), &GetPolicyRequest{
		TenantID: "t-1",
		Name:     "test-policy",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_UpdatePolicy_PolicyEngineUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{policyEngine: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.UpdatePolicy(context.Background(), &UpdatePolicyRequest{
		TenantID: "t-1",
		Policy:   testPolicy("test-policy"),
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_DeletePolicy_PolicyEngineUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{policyEngine: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.DeletePolicy(context.Background(), &DeletePolicyRequest{
		TenantID: "t-1",
		Name:     "test-policy",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_ListPolicies_PolicyEngineUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{policyEngine: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.ListPolicies(context.Background(), &ListPoliciesRequest{
		TenantID: "t-1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_ValidatePolicy_PolicyEngineUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{policyEngine: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.ValidatePolicy(context.Background(), &ValidatePolicyRequest{
		Policy: testPolicy("test-policy"),
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_TestPolicy_PolicyEngineUnavailable(t *testing.T) {
	provider := &mockIAMServiceProvider{policyEngine: nil}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.TestPolicy(context.Background(), &TestPolicyRequest{
		Check: &iam.CheckRequest{},
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestIAMServiceWrapper_TestPolicy_ProviderError(t *testing.T) {
	provider := &mockIAMServiceProvider{
		policyEngineErr: &testIAMError{msg: "policy engine error"},
	}
	wrapper := &iamServiceWrapper{provider: provider}

	resp, err := wrapper.TestPolicy(context.Background(), &TestPolicyRequest{
		Check: &iam.CheckRequest{},
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func (e *testIAMError) Error() string {
	return e.msg
}

// =============================================================================
// CreateUser role forwarding (QUIC + MCP transports converge on this wrapper)
// =============================================================================

// newRoleTestIdentityStore builds a real IdentityStore over MemoryStorage so
// role validation and persistence are exercised end-to-end (no tenant manager:
// quota checks are skipped when Tenants is nil).
func newRoleTestIdentityStore() (*iam.IdentityStore, *iam.MemoryStorage) {
	mem := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: mem,
		RoleStorage: mem,
	})
	return store, mem
}

// TestIAMServiceWrapper_CreateUser_ForwardsRoles proves the wrapper no longer
// drops IAMCreateUserRequest.Roles: the roles reach the identity store and are
// persisted on the created user.
func TestIAMServiceWrapper_CreateUser_ForwardsRoles(t *testing.T) {
	store, _ := newRoleTestIdentityStore()
	ctx := context.Background()

	role, err := store.CreateRole(ctx, "t-1", iam.CreateRoleOptions{Name: "auditor"})
	require.NoError(t, err)

	wrapper := &iamServiceWrapper{provider: &mockIAMServiceProvider{identityStore: store, inviteStore: iamtest.NewMemoryInviteStore()}}

	resp, err := wrapper.CreateUser(ctx, &IAMCreateUserRequest{
		TenantID: "t-1",
		Email:    "roles@example.com",
		Name:     "Role User",
		Roles:    []string{role.ID},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.User)
	assert.Equal(t, []string{role.ID}, resp.User.Roles)

	// Persisted record carries the roles too.
	stored, err := store.GetUser(ctx, "t-1", resp.User.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{role.ID}, stored.Roles)
}

// TestIAMServiceWrapper_CreateUser_UnknownRole_NoUserCreated proves the
// validate-before-create contract on the wrapper path (QUIC/MCP): an unknown
// role reference fails with a typed not-found error and no user is written.
func TestIAMServiceWrapper_CreateUser_UnknownRole_NoUserCreated(t *testing.T) {
	store, _ := newRoleTestIdentityStore()
	ctx := context.Background()

	wrapper := &iamServiceWrapper{provider: &mockIAMServiceProvider{identityStore: store, inviteStore: iamtest.NewMemoryInviteStore()}}

	resp, err := wrapper.CreateUser(ctx, &IAMCreateUserRequest{
		TenantID: "t-1",
		Email:    "phantom@example.com",
		Name:     "Phantom User",
		Roles:    []string{"no-such-role"},
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.True(t, dberrors.IsNotFound(err), "expected typed not-found, got %T: %v", err, err)

	// Zero side effects: the user must NOT exist.
	_, err = store.GetUserByEmail(ctx, "t-1", "phantom@example.com")
	require.Error(t, err, "no user must be created after failed role validation")
}

// =============================================================================
// CreateUser attribute forwarding (QUIC + MCP transports converge on
// this wrapper)
// =============================================================================

// TestIAMServiceWrapper_CreateUser_ReturnsTheInvite proves the wrapper the
// QUIC and MCP transports share issues a pending user's activation invite, as
// REST and gRPC do, and hands back a token that redeems for that user.
func TestIAMServiceWrapper_CreateUser_ReturnsTheInvite(t *testing.T) {
	store, _ := newRoleTestIdentityStore()
	invites := iamtest.NewMemoryInviteStore()
	ctx := context.Background()
	wrapper := &iamServiceWrapper{provider: &mockIAMServiceProvider{identityStore: store, inviteStore: invites}}

	resp, err := wrapper.CreateUser(ctx, &IAMCreateUserRequest{
		TenantID: "t-1", Email: "pat@example.com", Name: "Pat", InviteExpiryHours: 48,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.InviteToken, "a pending user came back without its invite")
	require.NotNil(t, resp.InviteExpiresAt)
	assert.WithinDuration(t, time.Now().Add(48*time.Hour), *resp.InviteExpiresAt, time.Minute)

	record, err := invites.Redeem(ctx, "t-1", resp.InviteToken)
	require.NoError(t, err)
	assert.Equal(t, resp.User.ID, record.UserID)
}

// TestIAMServiceWrapper_CreateUser_NoInviteStoreRefusesPending proves a node
// without an invite store refuses a pending create before writing.
func TestIAMServiceWrapper_CreateUser_NoInviteStoreRefusesPending(t *testing.T) {
	store, _ := newRoleTestIdentityStore()
	ctx := context.Background()
	wrapper := &iamServiceWrapper{provider: &mockIAMServiceProvider{identityStore: store}}

	_, err := wrapper.CreateUser(ctx, &IAMCreateUserRequest{TenantID: "t-1", Email: "sam@example.com", Name: "Sam"})
	require.ErrorIs(t, err, iam.ErrInviteStoreUnavailable)

	_, err = store.GetUserByEmail(ctx, "t-1", "sam@example.com")
	assert.Error(t, err, "a pending user was written with no way to activate it")
}

// TestIAMServiceWrapper_CreateUser_InviteStoreErrorWritesNothing proves a
// failure to reach the invite store aborts the create before writing.
func TestIAMServiceWrapper_CreateUser_InviteStoreErrorWritesNothing(t *testing.T) {
	store, _ := newRoleTestIdentityStore()
	ctx := context.Background()
	cause := dberrors.New(dberrors.ErrUnavailable, "test", "invite store unreachable")
	wrapper := &iamServiceWrapper{provider: &mockIAMServiceProvider{identityStore: store, inviteStoreErr: cause}}

	_, err := wrapper.CreateUser(ctx, &IAMCreateUserRequest{TenantID: "t-1", Email: "rae@example.com", Name: "Rae"})
	require.ErrorIs(t, err, cause)

	_, err = store.GetUserByEmail(ctx, "t-1", "rae@example.com")
	assert.Error(t, err, "the user was written although its invite could not be issued")
}

// TestIAMServiceWrapper_CreateUser_ForwardsAttributes proves the wrapper no
// longer drops IAMCreateUserRequest.Attributes: custom attributes reach the
// identity store and are persisted on the created user (gRPC/REST parity).
func TestIAMServiceWrapper_CreateUser_ForwardsAttributes(t *testing.T) {
	store, _ := newRoleTestIdentityStore()
	ctx := context.Background()

	wrapper := &iamServiceWrapper{provider: &mockIAMServiceProvider{identityStore: store, inviteStore: iamtest.NewMemoryInviteStore()}}

	attrs := map[string]string{"department": "engineering", "cost_center": "cc-42"}
	resp, err := wrapper.CreateUser(ctx, &IAMCreateUserRequest{
		TenantID:   "t-1",
		Email:      "attrs@example.com",
		Name:       "Attr User",
		Attributes: attrs,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.User)
	assert.Equal(t, attrs, resp.User.Attributes)

	// Persisted record carries the attributes too.
	stored, err := store.GetUser(ctx, "t-1", resp.User.ID)
	require.NoError(t, err)
	assert.Equal(t, attrs, stored.Attributes)
}

// =============================================================================
// UpdateUser attribute + role + group forwarding (MCP/QUIC converge here)
// =============================================================================

// TestIAMServiceWrapper_UpdateUser_ForwardsAttributes pins the fix for a
// silent-success defect: the wrapper accepted IAMUpdateUserRequest.Attributes
// and never copied it into iam.UpdateUserOptions, so the update returned 200
// and the OLD attributes stayed in the store.
//
// These attributes are what ABAC evaluates. An operator who tightens one is
// told the tightening took effect while the old value is still the one policy
// is decided on, which is a fail-open contract breach rather than a missing
// convenience — and nothing in the response says otherwise, because the store
// returns the record it did not change.
func TestIAMServiceWrapper_UpdateUser_ForwardsAttributes(t *testing.T) {
	store, _ := newRoleTestIdentityStore()
	ctx := context.Background()

	wrapper := &iamServiceWrapper{provider: &mockIAMServiceProvider{identityStore: store, inviteStore: iamtest.NewMemoryInviteStore()}}

	created, err := wrapper.CreateUser(ctx, &IAMCreateUserRequest{
		TenantID:   "t-1",
		Email:      "abac@example.com",
		Name:       "ABAC User",
		Attributes: map[string]string{"clearance": "secret"},
	})
	require.NoError(t, err)
	require.NotNil(t, created.User)

	tightened := map[string]string{"clearance": "public"}
	resp, err := wrapper.UpdateUser(ctx, &IAMUpdateUserRequest{
		TenantID:   "t-1",
		UserID:     created.User.ID,
		Attributes: &tightened,
	})
	require.NoError(t, err)
	require.NotNil(t, resp.User)
	assert.Equal(t, tightened, resp.User.Attributes,
		"the response reported attributes the update did not apply")

	// The persisted record is the one ABAC reads, so it is the one that has to
	// carry the change.
	stored, err := store.GetUser(ctx, "t-1", created.User.ID)
	require.NoError(t, err)
	assert.Equal(t, tightened, stored.Attributes)
}

// TestIAMServiceWrapper_UpdateUser_ForwardsRoles covers the sibling field found
// by auditing the rest of IAMUpdateUserRequest after the attribute defect:
// Roles was dropped by the same wrapper, in the same way, and it feeds RBAC
// rather than ABAC. REST forwards both through this identity store, so
// dropping it here also made one SDK verb mean two different things depending
// on the transport underneath it.
func TestIAMServiceWrapper_UpdateUser_ForwardsRoles(t *testing.T) {
	store, _ := newRoleTestIdentityStore()
	ctx := context.Background()

	role, err := store.CreateRole(ctx, "t-1", iam.CreateRoleOptions{Name: "auditor"})
	require.NoError(t, err)
	wrapper := &iamServiceWrapper{provider: &mockIAMServiceProvider{identityStore: store, inviteStore: iamtest.NewMemoryInviteStore()}}

	created, err := wrapper.CreateUser(ctx, &IAMCreateUserRequest{
		TenantID: "t-1",
		Email:    "rbac@example.com",
		Name:     "RBAC User",
	})
	require.NoError(t, err)

	roles := []string{role.ID}
	resp, err := wrapper.UpdateUser(ctx, &IAMUpdateUserRequest{
		TenantID: "t-1",
		UserID:   created.User.ID,
		Roles:    &roles,
	})
	require.NoError(t, err)
	require.NotNil(t, resp.User)
	assert.Equal(t, roles, resp.User.Roles)

	stored, err := store.GetUser(ctx, "t-1", created.User.ID)
	require.NoError(t, err)
	assert.Equal(t, roles, stored.Roles, "the role grant was reported and not stored")
}

// TestIAMServiceWrapper_UpdateUser_OmittedFieldsAreLeftAlone is the other half
// of the rule: forwarding must be driven by PRESENCE, not by value. A request
// that names only one field must not blank the others — the whole reason these
// are pointers — so this guards the fix from being written as an unconditional
// copy.
func TestIAMServiceWrapper_UpdateUser_OmittedFieldsAreLeftAlone(t *testing.T) {
	store, _ := newRoleTestIdentityStore()
	ctx := context.Background()

	role, err := store.CreateRole(ctx, "t-1", iam.CreateRoleOptions{Name: "auditor"})
	require.NoError(t, err)

	wrapper := &iamServiceWrapper{provider: &mockIAMServiceProvider{identityStore: store, inviteStore: iamtest.NewMemoryInviteStore()}}

	created, err := wrapper.CreateUser(ctx, &IAMCreateUserRequest{
		TenantID:   "t-1",
		Email:      "partial@example.com",
		Name:       "Partial User",
		Roles:      []string{role.ID},
		Attributes: map[string]string{"clearance": "secret"},
	})
	require.NoError(t, err)

	newName := "Renamed User"
	_, err = wrapper.UpdateUser(ctx, &IAMUpdateUserRequest{
		TenantID: "t-1",
		UserID:   created.User.ID,
		Name:     &newName,
	})
	require.NoError(t, err)

	stored, err := store.GetUser(ctx, "t-1", created.User.ID)
	require.NoError(t, err)
	assert.Equal(t, newName, stored.Name)
	assert.Equal(t, []string{role.ID}, stored.Roles, "a rename revoked the user's roles")
	assert.Equal(t, map[string]string{"clearance": "secret"}, stored.Attributes,
		"a rename cleared the user's ABAC attributes")
}

// TestIAMServiceWrapper_UpdateUser_RefusesTheTerminalDeletingState covers the
// MCP/QUIC side of the same guard. Both transports converge on this wrapper,
// which forwards Lifecycle straight to the identity store, so the state that
// claims a record for deletion was assertable as an ordinary field update on
// every one of them — the claim without the cascade that gives it meaning.
func TestIAMServiceWrapper_UpdateUser_RefusesTheTerminalDeletingState(t *testing.T) {
	store, _ := newRoleTestIdentityStore()
	ctx := context.Background()

	wrapper := &iamServiceWrapper{provider: &mockIAMServiceProvider{identityStore: store, inviteStore: iamtest.NewMemoryInviteStore()}}

	created, err := wrapper.CreateUser(ctx, &IAMCreateUserRequest{
		TenantID:  "t-1",
		Email:     "terminal@example.com",
		Username:  "terminal",
		Name:      "Terminal User",
		Lifecycle: iam.LifecycleActive,
	})
	require.NoError(t, err)

	deleting := iam.LifecycleDeleting
	resp, err := wrapper.UpdateUser(ctx, &IAMUpdateUserRequest{
		TenantID:  "t-1",
		UserID:    created.User.ID,
		Lifecycle: &deleting,
	})
	require.Error(t, err, "a deletion claim was accepted as an ordinary update")
	assert.Nil(t, resp)

	stored, err := store.GetUser(ctx, "t-1", created.User.ID)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecycleActive, stored.Lifecycle,
		"the refused update stranded the account in a terminal state")
	assert.True(t, stored.CanAuthenticate())
}

// TestIAMServiceWrapper_UpdateUser_StillSuspends is the counterweight: the
// administrative states this surface exists to set must keep working. A guard
// that refused every lifecycle change would satisfy the test above and disable
// account administration on two transports.
func TestIAMServiceWrapper_UpdateUser_StillSuspends(t *testing.T) {
	store, _ := newRoleTestIdentityStore()
	ctx := context.Background()

	wrapper := &iamServiceWrapper{provider: &mockIAMServiceProvider{identityStore: store, inviteStore: iamtest.NewMemoryInviteStore()}}

	created, err := wrapper.CreateUser(ctx, &IAMCreateUserRequest{
		TenantID:  "t-1",
		Email:     "hold@example.com",
		Username:  "hold",
		Name:      "Hold User",
		Lifecycle: iam.LifecycleActive,
	})
	require.NoError(t, err)

	suspended := iam.LifecycleSuspended
	resp, err := wrapper.UpdateUser(ctx, &IAMUpdateUserRequest{
		TenantID:  "t-1",
		UserID:    created.User.ID,
		Lifecycle: &suspended,
	})
	require.NoError(t, err)
	require.NotNil(t, resp.User)
	assert.Equal(t, iam.LifecycleSuspended, resp.User.Lifecycle)
	assert.False(t, resp.User.CanAuthenticate())
}
