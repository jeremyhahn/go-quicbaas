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

//go:build iam

package service

import (
	"context"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// IAM returns the IAM service when IAM is enabled.
// This method overrides the stub implementation in quicbaas.go.
func (s *quicBaaS) IAM() IAMService {
	if s.iamProvider == nil {
		return nil
	}
	return &iamServiceWrapper{provider: s.iamProvider}
}

// iamServiceWrapper implements IAMService by delegating to IAMServiceProvider.
type iamServiceWrapper struct {
	provider IAMServiceProvider
}

// Compile-time interface verification.
var _ IAMService = (*iamServiceWrapper)(nil)

// sanitizeUsername removes invalid characters and returns a valid username.
// Valid characters are: a-z, A-Z, 0-9, hyphen, and underscore.
func sanitizeUsername(s string) string {
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// =============================================================================
// Tenant Operations
// =============================================================================

func (w *iamServiceWrapper) CreateTenant(ctx context.Context, req *CreateTenantRequest) (*CreateTenantResponse, error) {
	tm, err := w.provider.GetTenantManager(ctx)
	if err != nil {
		return nil, err
	}
	if tm == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.CreateTenant", "tenant manager not available")
	}

	opts := iam.CreateTenantOptions{
		Name:     req.Name,
		Settings: req.Settings,
		Quotas:   req.Quotas,
	}
	if req.ID != "" {
		opts.ID = req.ID
	}

	tenant, err := tm.CreateTenant(ctx, opts)
	if err != nil {
		return nil, err
	}

	return &CreateTenantResponse{
		Tenant:    tenant,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) GetTenant(ctx context.Context, req *GetTenantRequest) (*GetTenantResponse, error) {
	tm, err := w.provider.GetTenantManager(ctx)
	if err != nil {
		return nil, err
	}
	if tm == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.GetTenant", "tenant manager not available")
	}

	tenant, err := tm.GetTenant(ctx, req.ID)
	if err != nil {
		return nil, err
	}

	return &GetTenantResponse{
		Tenant:    tenant,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) UpdateTenant(ctx context.Context, req *UpdateTenantRequest) (*UpdateTenantResponse, error) {
	tm, err := w.provider.GetTenantManager(ctx)
	if err != nil {
		return nil, err
	}
	if tm == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.UpdateTenant", "tenant manager not available")
	}

	opts := iam.UpdateTenantOptions{
		Settings: req.Settings,
		Quotas:   req.Quotas,
	}
	if req.Name != nil {
		opts.Name = req.Name
	}

	tenant, err := tm.UpdateTenant(ctx, req.ID, opts)
	if err != nil {
		return nil, err
	}

	return &UpdateTenantResponse{
		Tenant:    tenant,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) DeleteTenant(ctx context.Context, req *DeleteTenantRequest) (*DeleteTenantResponse, error) {
	tm, err := w.provider.GetTenantManager(ctx)
	if err != nil {
		return nil, err
	}
	if tm == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.DeleteTenant", "tenant manager not available")
	}

	err = tm.DeleteTenant(ctx, req.ID)
	if err != nil {
		return nil, err
	}

	return &DeleteTenantResponse{
		Deleted:   true,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) ListTenants(ctx context.Context, req *ListTenantsRequest) (*ListTenantsResponse, error) {
	tm, err := w.provider.GetTenantManager(ctx)
	if err != nil {
		return nil, err
	}
	if tm == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.ListTenants", "tenant manager not available")
	}

	opts := iam.ListOptions{
		Limit:  req.Limit,
		Offset: req.Offset,
		Filter: req.Filter,
	}

	tenants, err := tm.ListTenants(ctx, opts)
	if err != nil {
		return nil, err
	}

	return &ListTenantsResponse{
		Tenants:    tenants,
		TotalCount: int64(len(tenants)),
		Timestamp:  time.Now(),
	}, nil
}

// =============================================================================
// User Operations
// =============================================================================

func (w *iamServiceWrapper) CreateUser(ctx context.Context, req *IAMCreateUserRequest) (*IAMCreateUserResponse, error) {
	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.CreateUser", "identity store not available")
	}

	// Generate username from email if not provided or invalid.
	// The IdentityStore validates that Username matches a strict regex (3-64 alphanumeric characters).
	username := req.Username
	if username == "" {
		// Extract username from email (part before @)
		if idx := strings.Index(req.Email, "@"); idx > 0 {
			username = req.Email[:idx]
		}
	}

	// Clean username to only contain valid characters (a-z, A-Z, 0-9, -, _)
	username = sanitizeUsername(username)
	if username == "" {
		username = "user"
	}

	opts := iam.CreateUserOptions{
		Email:    req.Email,
		Username: username,
		Name:     req.Name,
	}

	user, err := is.CreateUser(ctx, req.TenantID, opts)
	if err != nil {
		return nil, err
	}

	return &IAMCreateUserResponse{
		User:      user,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) GetUser(ctx context.Context, req *IAMGetUserRequest) (*IAMGetUserResponse, error) {
	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.GetUser", "identity store not available")
	}

	user, err := is.GetUser(ctx, req.TenantID, req.UserID)
	if err != nil {
		return nil, err
	}

	return &IAMGetUserResponse{
		User:      user,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) UpdateUser(ctx context.Context, req *IAMUpdateUserRequest) (*IAMUpdateUserResponse, error) {
	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.UpdateUser", "identity store not available")
	}

	opts := iam.UpdateUserOptions{}
	if req.Email != nil {
		opts.Email = req.Email
	}
	if req.Name != nil {
		opts.Name = req.Name
	}

	user, err := is.UpdateUser(ctx, req.TenantID, req.UserID, opts)
	if err != nil {
		return nil, err
	}

	return &IAMUpdateUserResponse{
		User:      user,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) DeleteUser(ctx context.Context, req *IAMDeleteUserRequest) (*IAMDeleteUserResponse, error) {
	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.DeleteUser", "identity store not available")
	}

	err = is.DeleteUser(ctx, req.TenantID, req.UserID)
	if err != nil {
		return nil, err
	}

	return &IAMDeleteUserResponse{
		Deleted:   true,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) ListUsers(ctx context.Context, req *IAMListUsersRequest) (*IAMListUsersResponse, error) {
	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.ListUsers", "identity store not available")
	}

	opts := iam.ListOptions{
		Limit:  req.Limit,
		Offset: req.Offset,
	}

	users, err := is.ListUsers(ctx, req.TenantID, opts)
	if err != nil {
		return nil, err
	}

	return &IAMListUsersResponse{
		Users:      users,
		TotalCount: int64(len(users)),
		Timestamp:  time.Now(),
	}, nil
}

// =============================================================================
// Group Operations
// =============================================================================

func (w *iamServiceWrapper) CreateGroup(ctx context.Context, req *CreateGroupRequest) (*CreateGroupResponse, error) {
	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.CreateGroup", "identity store not available")
	}

	opts := iam.CreateGroupOptions{
		ID:          req.ID,
		Name:        req.Name,
		Description: req.Description,
	}

	group, err := is.CreateGroup(ctx, req.TenantID, opts)
	if err != nil {
		return nil, err
	}

	return &CreateGroupResponse{
		Group:     group,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) GetGroup(ctx context.Context, req *GetGroupRequest) (*GetGroupResponse, error) {
	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.GetGroup", "identity store not available")
	}

	group, err := is.GetGroup(ctx, req.TenantID, req.GroupID)
	if err != nil {
		return nil, err
	}

	return &GetGroupResponse{
		Group:     group,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) UpdateGroup(ctx context.Context, req *UpdateGroupRequest) (*UpdateGroupResponse, error) {
	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.UpdateGroup", "identity store not available")
	}

	opts := iam.UpdateGroupOptions{}
	if req.Name != nil {
		opts.Name = req.Name
	}
	if req.Description != nil {
		opts.Description = req.Description
	}

	group, err := is.UpdateGroup(ctx, req.TenantID, req.GroupID, opts)
	if err != nil {
		return nil, err
	}

	return &UpdateGroupResponse{
		Group:     group,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) DeleteGroup(ctx context.Context, req *DeleteGroupRequest) (*DeleteGroupResponse, error) {
	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.DeleteGroup", "identity store not available")
	}

	err = is.DeleteGroup(ctx, req.TenantID, req.GroupID)
	if err != nil {
		return nil, err
	}

	return &DeleteGroupResponse{
		Deleted:   true,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) ListGroups(ctx context.Context, req *ListGroupsRequest) (*ListGroupsResponse, error) {
	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.ListGroups", "identity store not available")
	}

	opts := iam.ListOptions{
		Limit:  req.Limit,
		Offset: req.Offset,
	}

	groups, err := is.ListGroups(ctx, req.TenantID, opts)
	if err != nil {
		return nil, err
	}

	return &ListGroupsResponse{
		Groups:     groups,
		TotalCount: int64(len(groups)),
		Timestamp:  time.Now(),
	}, nil
}

func (w *iamServiceWrapper) AddUserToGroup(ctx context.Context, req *AddUserToGroupRequest) (*AddUserToGroupResponse, error) {
	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.AddUserToGroup", "identity store not available")
	}

	err = is.AddUserToGroup(ctx, req.TenantID, req.GroupID, req.UserID)
	if err != nil {
		return nil, err
	}

	// Re-fetch the group to return updated state
	group, err := is.GetGroup(ctx, req.TenantID, req.GroupID)
	if err != nil {
		return nil, err
	}

	return &AddUserToGroupResponse{
		Group:     group,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) RemoveUserFromGroup(ctx context.Context, req *RemoveUserFromGroupRequest) (*RemoveUserFromGroupResponse, error) {
	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.RemoveUserFromGroup", "identity store not available")
	}

	err = is.RemoveUserFromGroup(ctx, req.TenantID, req.GroupID, req.UserID)
	if err != nil {
		return nil, err
	}

	// Re-fetch the group to return updated state
	group, err := is.GetGroup(ctx, req.TenantID, req.GroupID)
	if err != nil {
		return nil, err
	}

	return &RemoveUserFromGroupResponse{
		Group:     group,
		Timestamp: time.Now(),
	}, nil
}

// =============================================================================
// Role Operations
// =============================================================================

func (w *iamServiceWrapper) CreateRole(ctx context.Context, req *IAMCreateRoleRequest) (*IAMCreateRoleResponse, error) {
	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.CreateRole", "identity store not available")
	}

	opts := iam.CreateRoleOptions{
		ID:          req.ID,
		Name:        req.Name,
		Description: req.Description,
		Permissions: req.Permissions,
	}

	role, err := is.CreateRole(ctx, req.TenantID, opts)
	if err != nil {
		return nil, err
	}

	return &IAMCreateRoleResponse{
		Role:      role,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) GetRole(ctx context.Context, req *IAMGetRoleRequest) (*IAMGetRoleResponse, error) {
	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.GetRole", "identity store not available")
	}

	role, err := is.GetRole(ctx, req.TenantID, req.RoleID)
	if err != nil {
		return nil, err
	}

	return &IAMGetRoleResponse{
		Role:      role,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) UpdateRole(ctx context.Context, req *IAMUpdateRoleRequest) (*IAMUpdateRoleResponse, error) {
	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.UpdateRole", "identity store not available")
	}

	opts := iam.UpdateRoleOptions{}
	if req.Name != nil {
		opts.Name = req.Name
	}
	if req.Description != nil {
		opts.Description = req.Description
	}
	if req.Permissions != nil {
		opts.Permissions = req.Permissions
	}

	role, err := is.UpdateRole(ctx, req.TenantID, req.RoleID, opts)
	if err != nil {
		return nil, err
	}

	return &IAMUpdateRoleResponse{
		Role:      role,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) DeleteRole(ctx context.Context, req *IAMDeleteRoleRequest) (*IAMDeleteRoleResponse, error) {
	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.DeleteRole", "identity store not available")
	}

	err = is.DeleteRole(ctx, req.TenantID, req.RoleID)
	if err != nil {
		return nil, err
	}

	return &IAMDeleteRoleResponse{
		Deleted:   true,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) ListRoles(ctx context.Context, req *IAMListRolesRequest) (*IAMListRolesResponse, error) {
	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.ListRoles", "identity store not available")
	}

	opts := iam.ListOptions{
		Limit:  req.Limit,
		Offset: req.Offset,
	}

	roles, err := is.ListRoles(ctx, req.TenantID, opts)
	if err != nil {
		return nil, err
	}

	return &IAMListRolesResponse{
		Roles:      roles,
		TotalCount: int64(len(roles)),
		Timestamp:  time.Now(),
	}, nil
}

// AssignRole assigns a role to a user.
// Note: This uses the auth RBAC types (AuthAssignRoleRequest) which don't have TenantID.
// For full IAM support, the service should use IAMAssignRoleRequest instead.
func (w *iamServiceWrapper) AssignRole(ctx context.Context, req *AssignRoleRequest) (*AssignRoleResponse, error) {
	// The AssignRoleRequest is aliased to AuthAssignRoleRequest which has Username and RoleName.
	// This is incompatible with the IAM IdentityStore which expects TenantID, UserID, and RoleID.
	return &AssignRoleResponse{
		Success: false,
		Message: "IAM AssignRole requires TenantID; use IAM-specific request types",
	}, nil
}

// RevokeRole revokes a role from a user.
// Note: This uses the auth RBAC types (AuthRevokeRoleRequest) which don't have TenantID.
// For full IAM support, the service should use IAMRevokeRoleRequest instead.
func (w *iamServiceWrapper) RevokeRole(ctx context.Context, req *RevokeRoleRequest) (*RevokeRoleResponse, error) {
	// The RevokeRoleRequest is aliased to AuthRevokeRoleRequest which has Username and RoleName.
	// This is incompatible with the IAM IdentityStore which expects TenantID, UserID, and RoleID.
	return &RevokeRoleResponse{
		Success: false,
		Message: "IAM RevokeRole requires TenantID; use IAM-specific request types",
	}, nil
}

// =============================================================================
// Application Operations
// =============================================================================

func (w *iamServiceWrapper) RegisterApp(ctx context.Context, req *RegisterAppRequest) (*RegisterAppResponse, error) {
	am, err := w.provider.GetAppManager(ctx)
	if err != nil {
		return nil, err
	}
	if am == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.RegisterApp", "app manager not available")
	}

	opts := iam.RegisterAppOptions{
		Name:         req.Name,
		RedirectURIs: req.RedirectURIs,
		GrantTypes:   req.GrantTypes,
	}

	app, err := am.RegisterApp(ctx, req.TenantID, opts)
	if err != nil {
		return nil, err
	}

	return &RegisterAppResponse{
		App:          app,
		ClientSecret: app.ClientSecret,
		Timestamp:    time.Now(),
	}, nil
}

func (w *iamServiceWrapper) GetApp(ctx context.Context, req *GetAppRequest) (*GetAppResponse, error) {
	am, err := w.provider.GetAppManager(ctx)
	if err != nil {
		return nil, err
	}
	if am == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.GetApp", "app manager not available")
	}

	app, err := am.GetApp(ctx, req.ClientID)
	if err != nil {
		return nil, err
	}

	return &GetAppResponse{
		App:       app,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) UpdateApp(ctx context.Context, req *UpdateAppRequest) (*UpdateAppResponse, error) {
	am, err := w.provider.GetAppManager(ctx)
	if err != nil {
		return nil, err
	}
	if am == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.UpdateApp", "app manager not available")
	}

	opts := iam.UpdateAppOptions{}
	if req.RedirectURIs != nil {
		opts.RedirectURIs = req.RedirectURIs
	}
	if req.GrantTypes != nil {
		opts.GrantTypes = req.GrantTypes
	}
	if req.Name != nil {
		opts.Name = req.Name
	}

	app, err := am.UpdateApp(ctx, req.ClientID, opts)
	if err != nil {
		return nil, err
	}

	return &UpdateAppResponse{
		App:       app,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) DeleteApp(ctx context.Context, req *DeleteAppRequest) (*DeleteAppResponse, error) {
	am, err := w.provider.GetAppManager(ctx)
	if err != nil {
		return nil, err
	}
	if am == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.DeleteApp", "app manager not available")
	}

	err = am.DeleteApp(ctx, req.ClientID)
	if err != nil {
		return nil, err
	}

	return &DeleteAppResponse{
		Deleted:   true,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) ListApps(ctx context.Context, req *ListAppsRequest) (*ListAppsResponse, error) {
	am, err := w.provider.GetAppManager(ctx)
	if err != nil {
		return nil, err
	}
	if am == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.ListApps", "app manager not available")
	}

	opts := iam.ListOptions{
		Limit:  req.Limit,
		Offset: req.Offset,
	}

	apps, err := am.ListApps(ctx, req.TenantID, opts)
	if err != nil {
		return nil, err
	}

	return &ListAppsResponse{
		Apps:       apps,
		TotalCount: int64(len(apps)),
		Timestamp:  time.Now(),
	}, nil
}

func (w *iamServiceWrapper) RotateAppSecret(ctx context.Context, req *RotateAppSecretRequest) (*RotateAppSecretResponse, error) {
	am, err := w.provider.GetAppManager(ctx)
	if err != nil {
		return nil, err
	}
	if am == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.RotateAppSecret", "app manager not available")
	}

	app, err := am.RotateSecret(ctx, req.ClientID)
	if err != nil {
		return nil, err
	}

	return &RotateAppSecretResponse{
		ClientSecret: app.ClientSecret,
		Timestamp:    time.Now(),
	}, nil
}

// =============================================================================
// Policy Operations
// =============================================================================

func (w *iamServiceWrapper) CreatePolicy(ctx context.Context, req *CreatePolicyRequest) (*CreatePolicyResponse, error) {
	pe, err := w.provider.GetPolicyEngine(ctx)
	if err != nil {
		return nil, err
	}
	if pe == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.CreatePolicy", "policy engine not available")
	}

	err = pe.CreatePolicy(ctx, req.TenantID, req.Policy)
	if err != nil {
		return nil, err
	}

	return &CreatePolicyResponse{
		Policy:    req.Policy,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) GetPolicy(ctx context.Context, req *GetPolicyRequest) (*GetPolicyResponse, error) {
	pe, err := w.provider.GetPolicyEngine(ctx)
	if err != nil {
		return nil, err
	}
	if pe == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.GetPolicy", "policy engine not available")
	}

	policy, err := pe.GetPolicy(ctx, req.TenantID, req.Name)
	if err != nil {
		return nil, err
	}

	return &GetPolicyResponse{
		Policy:    policy,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) UpdatePolicy(ctx context.Context, req *UpdatePolicyRequest) (*UpdatePolicyResponse, error) {
	pe, err := w.provider.GetPolicyEngine(ctx)
	if err != nil {
		return nil, err
	}
	if pe == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.UpdatePolicy", "policy engine not available")
	}

	err = pe.UpdatePolicy(ctx, req.TenantID, req.Policy)
	if err != nil {
		return nil, err
	}

	return &UpdatePolicyResponse{
		Policy:    req.Policy,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) DeletePolicy(ctx context.Context, req *DeletePolicyRequest) (*DeletePolicyResponse, error) {
	pe, err := w.provider.GetPolicyEngine(ctx)
	if err != nil {
		return nil, err
	}
	if pe == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.DeletePolicy", "policy engine not available")
	}

	err = pe.DeletePolicy(ctx, req.TenantID, req.Name)
	if err != nil {
		return nil, err
	}

	return &DeletePolicyResponse{
		Deleted:   true,
		Timestamp: time.Now(),
	}, nil
}

func (w *iamServiceWrapper) ListPolicies(ctx context.Context, req *ListPoliciesRequest) (*ListPoliciesResponse, error) {
	pe, err := w.provider.GetPolicyEngine(ctx)
	if err != nil {
		return nil, err
	}
	if pe == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.ListPolicies", "policy engine not available")
	}

	opts := iam.ListOptions{
		Limit:  req.Limit,
		Offset: req.Offset,
	}

	policies, err := pe.ListPolicies(ctx, req.TenantID, opts)
	if err != nil {
		return nil, err
	}

	return &ListPoliciesResponse{
		Policies:   policies,
		TotalCount: int64(len(policies)),
		Timestamp:  time.Now(),
	}, nil
}

func (w *iamServiceWrapper) ValidatePolicy(ctx context.Context, req *ValidatePolicyRequest) (*ValidatePolicyResponse, error) {
	pe, err := w.provider.GetPolicyEngine(ctx)
	if err != nil {
		return nil, err
	}
	if pe == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.ValidatePolicy", "policy engine not available")
	}

	validationErr := pe.ValidatePolicy(req.Policy)

	resp := &ValidatePolicyResponse{
		Valid:     validationErr == nil,
		Timestamp: time.Now(),
	}
	if validationErr != nil {
		resp.Errors = []string{validationErr.Error()}
	}

	return resp, nil
}

func (w *iamServiceWrapper) TestPolicy(ctx context.Context, req *TestPolicyRequest) (*TestPolicyResponse, error) {
	pe, err := w.provider.GetPolicyEngine(ctx)
	if err != nil {
		return nil, err
	}
	if pe == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.TestPolicy", "policy engine not available")
	}

	// req.Check contains the iam.CheckRequest
	checkResp, err := pe.Evaluate(ctx, req.Check)
	if err != nil {
		return nil, err
	}

	return &TestPolicyResponse{
		Allowed:   checkResp.Allowed,
		Reason:    checkResp.Reason,
		Timestamp: time.Now(),
	}, nil
}
