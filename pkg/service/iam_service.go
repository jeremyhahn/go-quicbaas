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

// Package service provides the IAM service interface for QuicBaaS.
package service

import (
	"context"
)

// =============================================================================
// IAM Service - Identity and Access Management
// =============================================================================

// IAMService handles identity and access management operations including
// tenants, users, roles, applications, and policies.
type IAMService interface {
	// =============================================================================
	// Tenant Operations
	// =============================================================================

	// CreateTenant creates a new tenant.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Create tenant request with name, settings, and quotas
	// Returns:
	//   - CreateTenantResponse: Contains created tenant and timestamp
	//   - error: ErrInvalidInput, ErrAlreadyExists, ErrResourceExhausted, ErrTimeout
	CreateTenant(ctx context.Context, req *CreateTenantRequest) (*CreateTenantResponse, error)

	// GetTenant retrieves a tenant by ID.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get tenant request with tenant ID
	// Returns:
	//   - GetTenantResponse: Contains tenant and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	GetTenant(ctx context.Context, req *GetTenantRequest) (*GetTenantResponse, error)

	// UpdateTenant updates an existing tenant.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Update tenant request with optional fields
	// Returns:
	//   - UpdateTenantResponse: Contains updated tenant and timestamp
	//   - error: ErrNotFound, ErrInvalidInput, ErrUnauthorized, ErrTimeout
	UpdateTenant(ctx context.Context, req *UpdateTenantRequest) (*UpdateTenantResponse, error)

	// DeleteTenant deletes a tenant and all its resources.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Delete tenant request with tenant ID
	// Returns:
	//   - DeleteTenantResponse: Contains deletion status and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrConflict, ErrTimeout
	DeleteTenant(ctx context.Context, req *DeleteTenantRequest) (*DeleteTenantResponse, error)

	// ListTenants lists all tenants with pagination and filtering.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List tenants request with pagination and filter options
	// Returns:
	//   - ListTenantsResponse: Contains tenants, total count, and timestamp
	//   - error: ErrUnauthorized, ErrTimeout
	ListTenants(ctx context.Context, req *ListTenantsRequest) (*ListTenantsResponse, error)

	// ProvisionTenant creates a tenant, seeds its built-in roles and enrolls
	// its first admin, either by signing an admin CSR (cert) or by creating
	// the admin pending and minting a one-time invite (invite). It never
	// initializes a tenant barrier and never returns a private key. A re-run
	// for the same tenant and admin is idempotent.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Provision request with tenant id and admin enrollment mode
	// Returns:
	//   - IAMProvisionTenantResponse: Admin identity plus certificate or invite
	//   - error: ErrInvalidInput, ErrUnavailable, ErrInternal, ErrTimeout
	ProvisionTenant(ctx context.Context, req *IAMProvisionTenantRequest) (*IAMProvisionTenantResponse, error)

	// =============================================================================
	// User Operations
	// =============================================================================

	// CreateUser creates a new user in a tenant.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Create user request with email, name, and attributes
	// Returns:
	//   - IAMCreateUserResponse: Contains created user and timestamp
	//   - error: ErrInvalidInput, ErrAlreadyExists, ErrResourceExhausted, ErrTimeout
	CreateUser(ctx context.Context, req *IAMCreateUserRequest) (*IAMCreateUserResponse, error)

	// GetUser retrieves a user by ID.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get user request with tenant ID and user ID
	// Returns:
	//   - IAMGetUserResponse: Contains user and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	GetUser(ctx context.Context, req *IAMGetUserRequest) (*IAMGetUserResponse, error)

	// UpdateUser updates an existing user.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Update user request with optional fields
	// Returns:
	//   - IAMUpdateUserResponse: Contains updated user and timestamp
	//   - error: ErrNotFound, ErrInvalidInput, ErrUnauthorized, ErrTimeout
	UpdateUser(ctx context.Context, req *IAMUpdateUserRequest) (*IAMUpdateUserResponse, error)

	// DeleteUser deletes a user from a tenant.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Delete user request with tenant ID and user ID
	// Returns:
	//   - IAMDeleteUserResponse: Contains deletion status and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	DeleteUser(ctx context.Context, req *IAMDeleteUserRequest) (*IAMDeleteUserResponse, error)

	// ListUsers lists users in a tenant with pagination and filtering.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List users request with tenant ID and pagination options
	// Returns:
	//   - IAMListUsersResponse: Contains users, total count, and timestamp
	//   - error: ErrUnauthorized, ErrTimeout
	ListUsers(ctx context.Context, req *IAMListUsersRequest) (*IAMListUsersResponse, error)

	// =============================================================================
	// Role Operations
	// =============================================================================

	// CreateRole creates a new role in a tenant.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Create role request with name and permissions
	// Returns:
	//   - IAMCreateRoleResponse: Contains created role and timestamp
	//   - error: ErrInvalidInput, ErrAlreadyExists, ErrResourceExhausted, ErrTimeout
	CreateRole(ctx context.Context, req *IAMCreateRoleRequest) (*IAMCreateRoleResponse, error)

	// GetRole retrieves a role by ID.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get role request with tenant ID and role ID
	// Returns:
	//   - IAMGetRoleResponse: Contains role and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	GetRole(ctx context.Context, req *IAMGetRoleRequest) (*IAMGetRoleResponse, error)

	// UpdateRole updates an existing role.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Update role request with optional fields
	// Returns:
	//   - IAMUpdateRoleResponse: Contains updated role and timestamp
	//   - error: ErrNotFound, ErrInvalidInput, ErrUnauthorized, ErrTimeout
	UpdateRole(ctx context.Context, req *IAMUpdateRoleRequest) (*IAMUpdateRoleResponse, error)

	// DeleteRole deletes a role from a tenant.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Delete role request with tenant ID and role ID
	// Returns:
	//   - IAMDeleteRoleResponse: Contains deletion status and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrConflict, ErrTimeout
	DeleteRole(ctx context.Context, req *IAMDeleteRoleRequest) (*IAMDeleteRoleResponse, error)

	// ListRoles lists roles in a tenant with pagination and filtering.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List roles request with tenant ID and pagination options
	// Returns:
	//   - IAMListRolesResponse: Contains roles, total count, and timestamp
	//   - error: ErrUnauthorized, ErrTimeout
	ListRoles(ctx context.Context, req *IAMListRolesRequest) (*IAMListRolesResponse, error)

	// ReconcileBuiltInRoles converges a tenant's stored built-in roles onto
	// the running build's definitions: verbs the build now grants are added,
	// verbs it no longer grants are removed, and custom roles are untouched.
	//
	// It is the only supported repair for a tenant seeded before a permission
	// rename — every ordinary write path refuses a built-in role — and it is
	// idempotent, so a converged tenant reports no changed roles.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Reconcile request naming the tenant
	// Returns:
	//   - IAMReconcileRolesResponse: Contains the changed roles and timestamp
	//   - error: ErrInvalidInput, ErrUnauthorized, ErrUnavailable, ErrTimeout
	ReconcileBuiltInRoles(ctx context.Context, req *IAMReconcileRolesRequest) (*IAMReconcileRolesResponse, error)

	// AssignRole assigns a role to a user.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Assign role request with tenant ID, user ID, and role ID
	// Returns:
	//   - AssignRoleResponse: Contains updated user and timestamp
	//   - error: ErrNotFound, ErrAlreadyExists, ErrUnauthorized, ErrTimeout
	AssignRole(ctx context.Context, req *AssignRoleRequest) (*AssignRoleResponse, error)

	// RevokeRole revokes a role from a user.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Revoke role request with tenant ID, user ID, and role ID
	// Returns:
	//   - RevokeRoleResponse: Contains updated user and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	RevokeRole(ctx context.Context, req *RevokeRoleRequest) (*RevokeRoleResponse, error)

	// =============================================================================
	// Application Operations
	// =============================================================================

	// RegisterApp registers a new OAuth2 application.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Register app request with name and OAuth configuration
	// Returns:
	//   - RegisterAppResponse: Contains registered app, client secret, and timestamp
	//   - error: ErrInvalidInput, ErrAlreadyExists, ErrResourceExhausted, ErrTimeout
	RegisterApp(ctx context.Context, req *RegisterAppRequest) (*RegisterAppResponse, error)

	// GetApp retrieves an application by client ID.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get app request with client ID
	// Returns:
	//   - GetAppResponse: Contains app and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	GetApp(ctx context.Context, req *GetAppRequest) (*GetAppResponse, error)

	// UpdateApp updates an existing application.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Update app request with optional fields
	// Returns:
	//   - UpdateAppResponse: Contains updated app and timestamp
	//   - error: ErrNotFound, ErrInvalidInput, ErrUnauthorized, ErrTimeout
	UpdateApp(ctx context.Context, req *UpdateAppRequest) (*UpdateAppResponse, error)

	// DeleteApp deletes an application.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Delete app request with client ID
	// Returns:
	//   - DeleteAppResponse: Contains deletion status and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	DeleteApp(ctx context.Context, req *DeleteAppRequest) (*DeleteAppResponse, error)

	// ListApps lists applications in a tenant with pagination and filtering.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List apps request with tenant ID and pagination options
	// Returns:
	//   - ListAppsResponse: Contains apps, total count, and timestamp
	//   - error: ErrUnauthorized, ErrTimeout
	ListApps(ctx context.Context, req *ListAppsRequest) (*ListAppsResponse, error)

	// RotateAppSecret rotates an application's client secret.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Rotate app secret request with client ID
	// Returns:
	//   - RotateAppSecretResponse: Contains new client secret and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	RotateAppSecret(ctx context.Context, req *RotateAppSecretRequest) (*RotateAppSecretResponse, error)

	// =============================================================================
	// Policy Operations
	// =============================================================================

	// CreatePolicy creates a new ABAC policy.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Create policy request with policy document
	// Returns:
	//   - CreatePolicyResponse: Contains created policy and timestamp
	//   - error: ErrInvalidInput, ErrAlreadyExists, ErrResourceExhausted, ErrTimeout
	CreatePolicy(ctx context.Context, req *CreatePolicyRequest) (*CreatePolicyResponse, error)

	// GetPolicy retrieves a policy by name.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get policy request with tenant ID and policy name
	// Returns:
	//   - GetPolicyResponse: Contains policy and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	GetPolicy(ctx context.Context, req *GetPolicyRequest) (*GetPolicyResponse, error)

	// UpdatePolicy updates an existing policy.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Update policy request with policy document
	// Returns:
	//   - UpdatePolicyResponse: Contains updated policy and timestamp
	//   - error: ErrNotFound, ErrInvalidInput, ErrUnauthorized, ErrTimeout
	UpdatePolicy(ctx context.Context, req *UpdatePolicyRequest) (*UpdatePolicyResponse, error)

	// DeletePolicy deletes a policy.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Delete policy request with tenant ID and policy name
	// Returns:
	//   - DeletePolicyResponse: Contains deletion status and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	DeletePolicy(ctx context.Context, req *DeletePolicyRequest) (*DeletePolicyResponse, error)

	// ListPolicies lists policies in a tenant with pagination and filtering.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List policies request with tenant ID and pagination options
	// Returns:
	//   - ListPoliciesResponse: Contains policies, total count, and timestamp
	//   - error: ErrUnauthorized, ErrTimeout
	ListPolicies(ctx context.Context, req *ListPoliciesRequest) (*ListPoliciesResponse, error)

	// ValidatePolicy validates a policy document for syntax and semantics.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Validate policy request with policy document
	// Returns:
	//   - ValidatePolicyResponse: Contains validation results and timestamp
	//   - error: ErrInvalidInput, ErrTimeout
	ValidatePolicy(ctx context.Context, req *ValidatePolicyRequest) (*ValidatePolicyResponse, error)

	// TestPolicy tests a policy against a scenario without persisting it.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Test policy request with policy and check scenario
	// Returns:
	//   - TestPolicyResponse: Contains test results and timestamp
	//   - error: ErrInvalidInput, ErrTimeout
	TestPolicy(ctx context.Context, req *TestPolicyRequest) (*TestPolicyResponse, error)
}
