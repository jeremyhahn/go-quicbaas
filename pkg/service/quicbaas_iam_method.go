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

package service

import (
	"context"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// IAM returns the IAM service when IAM is enabled.
// This method overrides the stub implementation in quicbaas.go.
func (s *quicBaaS) IAM() IAMService {
	if s.iamProvider == nil {
		return nil
	}
	return &iamServiceWrapper{
		provider: s.iamProvider,
	}
}

// iamServiceWrapper implements IAMService by delegating to IAMServiceProvider.
// Tenant barrier lifecycle is owned by qrdb's TenantServicer; this wrapper
// manages only the BaaS-layer IAM metadata (policies, apps, OIDC, etc.).
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
		Name:               req.Name,
		Settings:           req.Settings,
		Quotas:             req.Quotas,
		IsolationType:      req.IsolationType,
		EngineQuotas:       req.EngineQuotas,
		EngineEntitlements: req.EngineEntitlements,
	}
	if req.ID != "" {
		opts.ID = req.ID
	}

	tenant, err := tm.CreateTenant(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Barrier lifecycle (TenantBarrier allocation, per-tenant DEK provisioning,
	// isolation enforcement) is handled entirely by qrdb's TenantServicer,
	// which is invoked by the embedded qrdb node during tenant creation through
	// the qrdb SDK's embedded transport. No duplicate barrier work is needed here.

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

	// Barrier deregistration and crypto-shred of tenant DEK are handled by qrdb's
	// TenantServicer.DeleteTenant via the embedded qrdb node SDK transport.

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

// ProvisionTenant validates the request, then hands it to the server's
// provisioner. Validation failures are classified ErrInvalidInput with the
// typed cause kept in the chain.
func (w *iamServiceWrapper) ProvisionTenant(ctx context.Context, req *IAMProvisionTenantRequest) (*IAMProvisionTenantResponse, error) {
	if req == nil {
		return nil, errors.New(errors.ErrInvalidInput, "iam.ProvisionTenant", "request is required")
	}
	if err := req.Validate(); err != nil {
		return nil, errors.Wrap(err, errors.ErrInvalidInput, "iam.ProvisionTenant")
	}

	provisioner, err := w.provider.GetTenantProvisioner(ctx)
	if err != nil {
		return nil, err
	}
	if provisioner == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.ProvisionTenant", "tenant provisioner not available")
	}
	return provisioner.Provision(ctx, req)
}

// =============================================================================
// User Operations
// =============================================================================

func (w *iamServiceWrapper) CreateUser(ctx context.Context, req *IAMCreateUserRequest) (*IAMCreateUserResponse, error) {
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
		Email:      req.Email,
		Username:   username,
		Name:       req.Name,
		Attributes: req.Attributes,
		// Roles are validated against the tenant's role store by
		// IdentityStore.CreateUser BEFORE the user record is written
		// (validate-before-create), matching the gRPC/Unix adapter contract.
		AuthMethods: req.AuthMethods,
		Lifecycle:   req.Lifecycle,
		Roles:       req.Roles,
	}

	// The request is judged before the server's wiring: a malformed request
	// is refused for its field, never as an unavailable store (#335).
	if err := iam.ValidateCreateUserOptions(req.TenantID, opts); err != nil {
		return nil, err
	}

	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.CreateUser", "identity store not available")
	}

	// A pending user is useless without its activation invite, so it is issued
	// in the same call; CreateInvitedUser refuses a nil store before writing.
	var invites iam.InviteStore
	if opts.EffectiveLifecycle() == iam.LifecyclePending {
		if invites, err = w.provider.GetInviteStore(ctx); err != nil {
			return nil, err
		}
	}
	inviteTTL := time.Duration(req.InviteExpiryHours) * time.Hour
	user, token, invite, err := is.CreateInvitedUser(ctx, req.TenantID, opts, invites, inviteTTL)
	if err != nil {
		return nil, err
	}

	resp := &IAMCreateUserResponse{
		User:      user,
		Timestamp: time.Now(),
	}
	if token != "" {
		resp.InviteToken = token
		resp.InviteExpiresAt = &invite.ExpiresAt
	}
	return resp, nil
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
	opts := iam.UpdateUserOptions{}
	if req.Email != nil {
		opts.Email = req.Email
	}
	if req.Name != nil {
		opts.Name = req.Name
	}
	// Applied independently of Name: the two used to share one field on the
	// record, so a request carrying both silently dropped this one.
	if req.Username != nil {
		opts.Username = req.Username
	}
	if req.Lifecycle != nil {
		opts.Lifecycle = req.Lifecycle
	}
	if req.AuthMethods != nil {
		opts.AuthMethods = req.AuthMethods
	}
	// Attributes and Roles are forwarded for the same reason the fields above
	// are: a field this request type ACCEPTS and this wrapper never copies is
	// answered with success and changed nothing, and the caller cannot tell.
	// That is not a missing convenience for either — attributes feed ABAC
	// evaluation and roles feed RBAC, so an operator who tightens one is told
	// the tightening took effect while the old grant is still what gets
	// evaluated. REST already forwards both through the same identity store,
	// so dropping them here also made one verb mean two different things
	// depending on the transport carrying it.
	if req.Attributes != nil {
		opts.Attributes = req.Attributes
	}
	if req.Roles != nil {
		opts.Roles = req.Roles
	}

	// Judged before the wiring, as in CreateUser (#335).
	if err := iam.ValidateUpdateUserOptions(opts); err != nil {
		return nil, err
	}

	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.UpdateUser", "identity store not available")
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

// ReconcileBuiltInRoles converges the tenant's built-in roles onto the running
// build's definitions through the identity store.
func (w *iamServiceWrapper) ReconcileBuiltInRoles(ctx context.Context, req *IAMReconcileRolesRequest) (*IAMReconcileRolesResponse, error) {
	if req == nil {
		return nil, errors.New(errors.ErrInvalidInput, "iam.ReconcileBuiltInRoles", "request is required")
	}

	is, err := w.provider.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if is == nil {
		return nil, errors.New(errors.ErrUnavailable, "iam.ReconcileBuiltInRoles", "identity store not available")
	}

	report, err := is.ReconcileBuiltInRoles(ctx, req.TenantID)
	if err != nil {
		return nil, err
	}

	return &IAMReconcileRolesResponse{
		TenantID:  report.TenantID,
		Roles:     report.Roles,
		Changed:   report.Changed(),
		Timestamp: time.Now(),
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

	tenantID, err := tenantctx.Target(ctx, "iam.RegisterApp", req.TenantID)
	if err != nil {
		return nil, err
	}

	app, err := am.RegisterApp(ctx, tenantID, opts)
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

	tenantID, err := tenantctx.Target(ctx, "iam.GetApp", "")
	if err != nil {
		return nil, err
	}

	app, err := am.GetAppInTenant(ctx, tenantID, req.ClientID)
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

	tenantID, err := tenantctx.Target(ctx, "iam.UpdateApp", "")
	if err != nil {
		return nil, err
	}

	app, err := am.UpdateAppInTenant(ctx, tenantID, req.ClientID, opts)
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

	tenantID, err := tenantctx.Target(ctx, "iam.DeleteApp", "")
	if err != nil {
		return nil, err
	}

	err = am.DeleteAppInTenant(ctx, tenantID, req.ClientID)
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

	tenantID, err := tenantctx.Target(ctx, "iam.ListApps", req.TenantID)
	if err != nil {
		return nil, err
	}

	apps, err := am.ListApps(ctx, tenantID, opts)
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

	tenantID, err := tenantctx.Target(ctx, "iam.RotateAppSecret", "")
	if err != nil {
		return nil, err
	}

	app, err := am.RotateSecretInTenant(ctx, tenantID, req.ClientID)
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

	validationErr := iam.CheckPolicyName(req.Policy, req.Name)
	if validationErr == nil {
		validationErr = pe.ValidatePolicy(req.Policy)
	}

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
