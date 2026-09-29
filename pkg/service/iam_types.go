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

// Package service provides high-level service types for IAM operations.
// These types define the request/response contracts used across the IAM service layer.
package service

import (
	"regexp"
	"strings"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// Email validation regex (RFC 5322 simplified).
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

// =============================================================================
// Tenant Request/Response Types
// =============================================================================

// CreateTenantRequest requests creation of a new tenant.
type CreateTenantRequest struct {
	// ID is the tenant identifier (optional, auto-generated if not provided)
	ID string `json:"id,omitempty"`

	// Name is the human-readable tenant name
	Name string `json:"name"`

	// Settings contains tenant-specific configuration
	Settings *iam.TenantSettings `json:"settings,omitempty"`

	// Quotas defines resource limits for this tenant
	Quotas *iam.TenantQuotas `json:"quotas,omitempty"`

	// IsolationType selects the engine isolation strategy for the tenant's
	// data. The engine binds isolation at creation, so this cannot be changed
	// later; empty defers to the engine default (prefix).
	IsolationType qrdbsdk.TenantIsolationType `json:"isolation_type,omitempty"`

	// EngineQuotas overrides the engine-level resource limits applied to the
	// tenant. Nil leaves the engine's configured defaults in place.
	EngineQuotas *qrdbsdk.Quotas `json:"engine_quotas,omitempty"`

	// EngineEntitlements overrides the engine-level feature entitlements for
	// the tenant. Nil leaves the engine's configured defaults in place.
	EngineEntitlements *qrdbsdk.Entitlements `json:"engine_entitlements,omitempty"`
}

// CreateTenantResponse returns the created tenant.
type CreateTenantResponse struct {
	// Tenant is the created tenant
	Tenant *iam.Tenant `json:"tenant"`

	// Timestamp is when the tenant was created
	Timestamp time.Time `json:"timestamp"`
}

// GetTenantRequest requests a tenant by ID.
type GetTenantRequest struct {
	// ID is the tenant identifier
	ID string `json:"id"`
}

// GetTenantResponse returns a tenant.
type GetTenantResponse struct {
	// Tenant is the requested tenant
	Tenant *iam.Tenant `json:"tenant"`

	// Timestamp is when the request was processed
	Timestamp time.Time `json:"timestamp"`
}

// UpdateTenantRequest updates a tenant.
type UpdateTenantRequest struct {
	// ID is the tenant identifier
	ID string `json:"id"`

	// Name is the human-readable tenant name (optional)
	Name *string `json:"name,omitempty"`

	// Settings contains tenant-specific configuration (optional)
	Settings *iam.TenantSettings `json:"settings,omitempty"`

	// Quotas defines resource limits (optional)
	Quotas *iam.TenantQuotas `json:"quotas,omitempty"`
}

// UpdateTenantResponse confirms tenant update.
type UpdateTenantResponse struct {
	// Tenant is the updated tenant
	Tenant *iam.Tenant `json:"tenant"`

	// Timestamp is when the update occurred
	Timestamp time.Time `json:"timestamp"`
}

// DeleteTenantRequest requests tenant deletion.
type DeleteTenantRequest struct {
	// ID is the tenant identifier
	ID string `json:"id"`
}

// DeleteTenantResponse confirms tenant deletion.
type DeleteTenantResponse struct {
	// Deleted indicates if deletion succeeded
	Deleted bool `json:"deleted"`

	// Timestamp is when deletion occurred
	Timestamp time.Time `json:"timestamp"`
}

// ListTenantsRequest requests a list of tenants.
type ListTenantsRequest struct {
	// Limit is the maximum number of results
	Limit int `json:"limit,omitempty"`

	// Offset is the number of results to skip
	Offset int `json:"offset,omitempty"`

	// Filter contains field-specific filters
	Filter map[string]interface{} `json:"filter,omitempty"`
}

// ListTenantsResponse returns a list of tenants.
type ListTenantsResponse struct {
	// Tenants is the list of tenants
	Tenants []*iam.Tenant `json:"tenants"`

	// TotalCount is the total number of tenants
	TotalCount int64 `json:"total_count"`

	// Timestamp is when the list was generated
	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// User Request/Response Types
// =============================================================================

// IAMCreateUserRequest requests creation of a new user.
type IAMCreateUserRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// Email is the user's email address (unique within tenant)
	Email string `json:"email"`

	// Username is the user's tenant-unique handle — 3-64 characters of
	// [A-Za-z0-9_-], distinct from the display Name and stored as its own
	// field. Optional: if not provided or invalid, it is derived from the local
	// part of the email address.
	Username string `json:"username"`

	// Name is the user's display name
	Name string `json:"name"`

	// Roles is the list of role IDs assigned to this user
	Roles []string `json:"roles,omitempty"`

	// Attributes contains custom user attributes
	Attributes map[string]string `json:"attributes,omitempty"`

	// AuthMethods lists the credential kinds this account may present
	// ("webauthn", "piv"). Empty defaults to webauthn.
	AuthMethods []string `json:"auth_methods,omitempty"`

	// Lifecycle is the state to create in: pending, active or disabled. Empty
	// defaults to pending.
	Lifecycle iam.Lifecycle `json:"lifecycle,omitempty"`
}

// Validate validates the IAMCreateUserRequest.
// Note: Username is optional and will be derived from email if not provided.
func (req *IAMCreateUserRequest) Validate() error {
	if req.TenantID == "" {
		return &api.RequiredFieldError{Field: "tenant_id"}
	}
	if req.Email == "" {
		return &api.RequiredFieldError{Field: "email"}
	}
	email := strings.TrimSpace(req.Email)
	if !emailRegex.MatchString(email) {
		return &api.InvalidEmailFormatError{}
	}
	// Username is optional - it will be derived from email if not provided or invalid
	if req.Name == "" {
		return &api.RequiredFieldError{Field: "name"}
	}
	if strings.TrimSpace(req.Name) == "" {
		return &api.WhitespaceFieldError{Field: "name"}
	}
	return nil
}

// IAMCreateUserResponse returns the created user.
type IAMCreateUserResponse struct {
	// User is the created user
	User *iam.User `json:"user"`

	// Timestamp is when the user was created
	Timestamp time.Time `json:"timestamp"`
}

// IAMGetUserRequest requests a user by ID.
type IAMGetUserRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// UserID is the user identifier
	UserID string `json:"user_id"`
}

// IAMGetUserResponse returns a user.
type IAMGetUserResponse struct {
	// User is the requested user
	User *iam.User `json:"user"`

	// Timestamp is when the request was processed
	Timestamp time.Time `json:"timestamp"`
}

// IAMUpdateUserRequest updates a user.
type IAMUpdateUserRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// UserID is the user identifier
	UserID string `json:"user_id"`

	// Email is the user's email address (optional)
	Email *string `json:"email,omitempty"`

	// Name is the user's display name (optional)
	Name *string `json:"name,omitempty"`

	// Username, when non-nil, changes the tenant-unique handle — the key
	// GetUserByUsername resolves and an administrator addresses the account by.
	// It is applied independently of Name; the two used to share one field, so
	// a request carrying both silently dropped this one.
	Username *string `json:"username,omitempty"`

	// Roles is the list of role IDs (optional)
	Roles *[]string `json:"roles,omitempty"`

	// Attributes, when non-nil, REPLACES the account's custom attributes.
	//
	// It is map[string]string, the shape the store holds and ABAC evaluates,
	// not map[string]interface{}. An untyped map lets a caller express values
	// the record cannot hold, which leaves every consumer to invent its own
	// stringification — and the one conversion that exists must then be
	// duplicated in each of them. A transport converts once, at the edge where
	// the untyped JSON arrives (see the MCP tool's stringAttributes, which
	// REFUSES a non-string rather than dropping it); past that edge the shape
	// is the store's.
	Attributes *map[string]string `json:"attributes,omitempty"`

	// Lifecycle, when non-empty, transitions the account: pending, active,
	// suspended, locked or disabled. `deleting` is the deletion workflow's own
	// claim on the record and is refused here. It replaces the suspended/locked
	// pair, which named a model this layer no longer has — and which no handler
	// ever read, so a caller setting either was silently ignored.
	Lifecycle *iam.Lifecycle `json:"lifecycle,omitempty"`

	// AuthMethods, when non-nil, replaces the credential kinds this account may
	// present.
	AuthMethods *[]string `json:"auth_methods,omitempty"`
}

// Validate validates the IAMUpdateUserRequest.
func (req *IAMUpdateUserRequest) Validate() error {
	if req.TenantID == "" {
		return &api.RequiredFieldError{Field: "tenant_id"}
	}
	if req.UserID == "" {
		return &api.RequiredFieldError{Field: "user_id"}
	}
	// Validate email if provided
	if req.Email != nil {
		email := strings.TrimSpace(*req.Email)
		if email == "" {
			return &api.RequiredFieldError{Field: "email"}
		}
		if !emailRegex.MatchString(email) {
			return &api.InvalidEmailFormatError{}
		}
	}
	// Validate name if provided
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return &api.WhitespaceFieldError{Field: "name"}
		}
	}
	return nil
}

// IAMUpdateUserResponse confirms user update.
type IAMUpdateUserResponse struct {
	// User is the updated user
	User *iam.User `json:"user"`

	// Timestamp is when the update occurred
	Timestamp time.Time `json:"timestamp"`
}

// IAMDeleteUserRequest requests user deletion.
type IAMDeleteUserRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// UserID is the user identifier
	UserID string `json:"user_id"`
}

// IAMDeleteUserResponse confirms user deletion.
type IAMDeleteUserResponse struct {
	// Deleted indicates if deletion succeeded
	Deleted bool `json:"deleted"`

	// Timestamp is when deletion occurred
	Timestamp time.Time `json:"timestamp"`
}

// IAMListUsersRequest requests a list of users.
type IAMListUsersRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// Limit is the maximum number of results
	Limit int `json:"limit,omitempty"`

	// Offset is the number of results to skip
	Offset int `json:"offset,omitempty"`

	// Filter contains field-specific filters
	Filter map[string]interface{} `json:"filter,omitempty"`
}

// IAMListUsersResponse returns a list of users.
type IAMListUsersResponse struct {
	// Users is the list of users
	Users []*iam.User `json:"users"`

	// TotalCount is the total number of users
	TotalCount int64 `json:"total_count,omitempty"`

	// Timestamp is when the list was generated
	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// Role Request/Response Types
// =============================================================================

// IAMCreateRoleRequest requests creation of a new role.
type IAMCreateRoleRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// ID is an optional client-provided role ID
	// If empty, the server will generate a UUID
	ID string `json:"id,omitempty"`

	// Name is the role display name
	Name string `json:"name"`

	// Description explains the role's purpose
	Description string `json:"description,omitempty"`

	// Permissions is the list of permission strings
	Permissions []string `json:"permissions,omitempty"`
}

// Validate validates the IAMCreateRoleRequest.
func (req *IAMCreateRoleRequest) Validate() error {
	if req.TenantID == "" {
		return &api.RequiredFieldError{Field: "tenant_id"}
	}
	if req.Name == "" {
		return &api.RequiredFieldError{Field: "name"}
	}
	if strings.TrimSpace(req.Name) == "" {
		return &api.WhitespaceFieldError{Field: "name"}
	}
	// Validate permissions format
	for i, perm := range req.Permissions {
		if strings.TrimSpace(perm) == "" {
			return &api.IndexedItemError{Index: i, Message: "permission"}
		}
	}
	return nil
}

// IAMCreateRoleResponse returns the created role.
type IAMCreateRoleResponse struct {
	// Role is the created role
	Role *iam.Role `json:"role"`

	// Timestamp is when the role was created
	Timestamp time.Time `json:"timestamp"`
}

// IAMGetRoleRequest requests a role by ID.
type IAMGetRoleRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// RoleID is the role identifier
	RoleID string `json:"role_id"`
}

// IAMGetRoleResponse returns a role.
type IAMGetRoleResponse struct {
	// Role is the requested role
	Role *iam.Role `json:"role"`

	// Timestamp is when the request was processed
	Timestamp time.Time `json:"timestamp"`
}

// IAMUpdateRoleRequest updates a role.
type IAMUpdateRoleRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// RoleID is the role identifier
	RoleID string `json:"role_id"`

	// Name is the role display name (optional)
	Name *string `json:"name,omitempty"`

	// Description explains the role's purpose (optional)
	Description *string `json:"description,omitempty"`

	// Permissions is the list of permission strings (optional)
	Permissions *[]string `json:"permissions,omitempty"`
}

// IAMUpdateRoleResponse confirms role update.
type IAMUpdateRoleResponse struct {
	// Role is the updated role
	Role *iam.Role `json:"role"`

	// Timestamp is when the update occurred
	Timestamp time.Time `json:"timestamp"`
}

// IAMDeleteRoleRequest requests role deletion.
type IAMDeleteRoleRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// RoleID is the role identifier
	RoleID string `json:"role_id"`
}

// IAMDeleteRoleResponse confirms role deletion.
type IAMDeleteRoleResponse struct {
	// Deleted indicates if deletion succeeded
	Deleted bool `json:"deleted"`

	// Timestamp is when deletion occurred
	Timestamp time.Time `json:"timestamp"`
}

// IAMListRolesRequest requests a list of roles.
type IAMListRolesRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// Limit is the maximum number of results
	Limit int `json:"limit,omitempty"`

	// Offset is the number of results to skip
	Offset int `json:"offset,omitempty"`

	// Filter contains field-specific filters
	Filter map[string]interface{} `json:"filter,omitempty"`
}

// IAMListRolesResponse returns a list of roles.
type IAMListRolesResponse struct {
	// Roles is the list of roles
	Roles []*iam.Role `json:"roles"`

	// TotalCount is the total number of roles
	TotalCount int64 `json:"total_count,omitempty"`

	// Timestamp is when the list was generated
	Timestamp time.Time `json:"timestamp"`
}

// IAMReconcileRolesRequest requests built-in role reconciliation for a tenant.
type IAMReconcileRolesRequest struct {
	// TenantID is the tenant whose built-in roles are converged onto the
	// running build's definitions.
	TenantID string `json:"tenant_id"`
}

// IAMReconcileRolesResponse reports what reconciliation changed. An empty
// Roles slice means the tenant already agreed with the build and nothing was
// written.
type IAMReconcileRolesResponse struct {
	// TenantID is the reconciled tenant
	TenantID string `json:"tenant_id"`

	// Roles carries one entry per built-in role that changed
	Roles []iam.RoleReconcileResult `json:"roles,omitempty"`

	// Changed reports whether anything was written
	Changed bool `json:"changed"`

	// Timestamp is when reconciliation ran
	Timestamp time.Time `json:"timestamp"`
}

// IAMAssignRoleRequest assigns a role to a user.
type IAMAssignRoleRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// UserID is the user identifier
	UserID string `json:"user_id"`

	// RoleID is the role identifier
	RoleID string `json:"role_id"`
}

// IAMAssignRoleResponse confirms role assignment.
type IAMAssignRoleResponse struct {
	// User is the updated user
	User *iam.User `json:"user"`

	// Timestamp is when the operation occurred
	Timestamp time.Time `json:"timestamp"`
}

// IAMRevokeRoleRequest revokes a role from a user.
type IAMRevokeRoleRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// UserID is the user identifier
	UserID string `json:"user_id"`

	// RoleID is the role identifier
	RoleID string `json:"role_id"`
}

// IAMRevokeRoleResponse confirms role revocation.
type IAMRevokeRoleResponse struct {
	// User is the updated user
	User *iam.User `json:"user"`

	// Timestamp is when the operation occurred
	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// Application Request/Response Types
// =============================================================================

// RegisterAppRequest requests registration of a new OAuth2 application.
type RegisterAppRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// Name is the application display name
	Name string `json:"name"`

	// RedirectURIs is the list of allowed OAuth redirect URIs
	RedirectURIs []string `json:"redirect_uris"`

	// GrantTypes is the list of allowed OAuth grant types
	GrantTypes []string `json:"grant_types"`

	// ResponseTypes is the list of allowed OAuth response types
	ResponseTypes []string `json:"response_types"`

	// ResourcePrefix is the resource namespace for this application
	ResourcePrefix string `json:"resource_prefix,omitempty"`
}

// Validate validates the RegisterAppRequest.
func (req *RegisterAppRequest) Validate() error {
	if req.TenantID == "" {
		return &api.RequiredFieldError{Field: "tenant_id"}
	}
	if req.Name == "" {
		return &api.RequiredFieldError{Field: "name"}
	}
	if strings.TrimSpace(req.Name) == "" {
		return &api.WhitespaceFieldError{Field: "name"}
	}
	if len(req.RedirectURIs) == 0 {
		return &api.EmptyCollectionError{Field: "at least one redirect_uri"}
	}
	for i, uri := range req.RedirectURIs {
		if strings.TrimSpace(uri) == "" {
			return &api.IndexedItemError{Index: i, Message: "redirect_uri"}
		}
	}
	if len(req.GrantTypes) == 0 {
		return &api.EmptyCollectionError{Field: "at least one grant_type"}
	}
	for i, gt := range req.GrantTypes {
		if strings.TrimSpace(gt) == "" {
			return &api.IndexedItemError{Index: i, Message: "grant_type"}
		}
	}
	if len(req.ResponseTypes) == 0 {
		return &api.EmptyCollectionError{Field: "at least one response_type"}
	}
	for i, rt := range req.ResponseTypes {
		if strings.TrimSpace(rt) == "" {
			return &api.IndexedItemError{Index: i, Message: "response_type"}
		}
	}
	return nil
}

// RegisterAppResponse returns the registered application.
type RegisterAppResponse struct {
	// App is the registered application
	App *iam.Application `json:"app"`

	// ClientSecret is the generated client secret (only returned once)
	ClientSecret string `json:"client_secret"`

	// Timestamp is when the application was registered
	Timestamp time.Time `json:"timestamp"`
}

// GetAppRequest requests an application by client ID.
type GetAppRequest struct {
	// ClientID is the OAuth client identifier
	ClientID string `json:"client_id"`
}

// GetAppResponse returns an application.
type GetAppResponse struct {
	// App is the requested application (without client secret)
	App *iam.Application `json:"app"`

	// Timestamp is when the request was processed
	Timestamp time.Time `json:"timestamp"`
}

// UpdateAppRequest updates an application.
type UpdateAppRequest struct {
	// ClientID is the OAuth client identifier
	ClientID string `json:"client_id"`

	// Name is the application display name (optional)
	Name *string `json:"name,omitempty"`

	// RedirectURIs is the list of allowed OAuth redirect URIs (optional)
	RedirectURIs *[]string `json:"redirect_uris,omitempty"`

	// GrantTypes is the list of allowed OAuth grant types (optional)
	GrantTypes *[]string `json:"grant_types,omitempty"`

	// ResponseTypes is the list of allowed OAuth response types (optional)
	ResponseTypes *[]string `json:"response_types,omitempty"`

	// ResourcePrefix is the resource namespace (optional)
	ResourcePrefix *string `json:"resource_prefix,omitempty"`
}

// UpdateAppResponse confirms application update.
type UpdateAppResponse struct {
	// App is the updated application
	App *iam.Application `json:"app"`

	// Timestamp is when the update occurred
	Timestamp time.Time `json:"timestamp"`
}

// DeleteAppRequest requests application deletion.
type DeleteAppRequest struct {
	// ClientID is the OAuth client identifier
	ClientID string `json:"client_id"`
}

// DeleteAppResponse confirms application deletion.
type DeleteAppResponse struct {
	// Deleted indicates if deletion succeeded
	Deleted bool `json:"deleted"`

	// Timestamp is when deletion occurred
	Timestamp time.Time `json:"timestamp"`
}

// ListAppsRequest requests a list of applications.
type ListAppsRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// Limit is the maximum number of results
	Limit int `json:"limit,omitempty"`

	// Offset is the number of results to skip
	Offset int `json:"offset,omitempty"`

	// Filter contains field-specific filters
	Filter map[string]interface{} `json:"filter,omitempty"`
}

// ListAppsResponse returns a list of applications.
type ListAppsResponse struct {
	// Apps is the list of applications
	Apps []*iam.Application `json:"apps"`

	// TotalCount is the total number of applications
	TotalCount int64 `json:"total_count,omitempty"`

	// Timestamp is when the list was generated
	Timestamp time.Time `json:"timestamp"`
}

// RotateAppSecretRequest requests rotation of an application client secret.
type RotateAppSecretRequest struct {
	// ClientID is the OAuth client identifier
	ClientID string `json:"client_id"`
}

// RotateAppSecretResponse returns the new client secret.
type RotateAppSecretResponse struct {
	// ClientSecret is the new client secret (only returned once)
	ClientSecret string `json:"client_secret"`

	// Timestamp is when the rotation occurred
	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// Policy Request/Response Types
// =============================================================================

// CreatePolicyRequest requests creation of a new policy.
type CreatePolicyRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// Policy is the policy document
	Policy *iam.Policy `json:"policy"`
}

// CreatePolicyResponse returns the created policy.
type CreatePolicyResponse struct {
	// Policy is the created policy
	Policy *iam.Policy `json:"policy"`

	// Timestamp is when the policy was created
	Timestamp time.Time `json:"timestamp"`
}

// GetPolicyRequest requests a policy by name.
type GetPolicyRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// Name is the policy name
	Name string `json:"name"`
}

// GetPolicyResponse returns a policy.
type GetPolicyResponse struct {
	// Policy is the requested policy
	Policy *iam.Policy `json:"policy"`

	// Timestamp is when the request was processed
	Timestamp time.Time `json:"timestamp"`
}

// UpdatePolicyRequest updates a policy.
type UpdatePolicyRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// Name is the policy name
	Name string `json:"name"`

	// Policy is the updated policy document
	Policy *iam.Policy `json:"policy"`
}

// UpdatePolicyResponse confirms policy update.
type UpdatePolicyResponse struct {
	// Policy is the updated policy
	Policy *iam.Policy `json:"policy"`

	// Timestamp is when the update occurred
	Timestamp time.Time `json:"timestamp"`
}

// DeletePolicyRequest requests policy deletion.
type DeletePolicyRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// Name is the policy name
	Name string `json:"name"`
}

// DeletePolicyResponse confirms policy deletion.
type DeletePolicyResponse struct {
	// Deleted indicates if deletion succeeded
	Deleted bool `json:"deleted"`

	// Timestamp is when deletion occurred
	Timestamp time.Time `json:"timestamp"`
}

// ListPoliciesRequest requests a list of policies.
type ListPoliciesRequest struct {
	// TenantID is the owning tenant
	TenantID string `json:"tenant_id"`

	// Limit is the maximum number of results
	Limit int `json:"limit,omitempty"`

	// Offset is the number of results to skip
	Offset int `json:"offset,omitempty"`

	// Filter contains field-specific filters
	Filter map[string]interface{} `json:"filter,omitempty"`
}

// ListPoliciesResponse returns a list of policies.
type ListPoliciesResponse struct {
	// Policies is the list of policies
	Policies []*iam.Policy `json:"policies"`

	// TotalCount is the total number of policies
	TotalCount int64 `json:"total_count,omitempty"`

	// Timestamp is when the list was generated
	Timestamp time.Time `json:"timestamp"`
}

// ValidatePolicyRequest requests policy validation.
type ValidatePolicyRequest struct {
	// Policy is the policy document to validate
	Policy *iam.Policy `json:"policy"`
}

// ValidatePolicyResponse returns validation results.
type ValidatePolicyResponse struct {
	// Valid indicates if the policy is valid
	Valid bool `json:"valid"`

	// Errors contains validation error messages
	Errors []string `json:"errors,omitempty"`

	// Warnings contains validation warnings
	Warnings []string `json:"warnings,omitempty"`

	// Timestamp is when validation was performed
	Timestamp time.Time `json:"timestamp"`
}

// TestPolicyRequest requests policy testing against a scenario.
type TestPolicyRequest struct {
	// Policy is the policy document to test
	Policy *iam.Policy `json:"policy"`

	// Check is the authorization check to test
	Check *iam.CheckRequest `json:"check"`
}

// TestPolicyResponse returns policy test results.
type TestPolicyResponse struct {
	// Allowed indicates whether access is granted
	Allowed bool `json:"allowed"`

	// MatchedRule is the name of the rule that matched
	MatchedRule string `json:"matched_rule,omitempty"`

	// Reason provides additional context for the decision
	Reason string `json:"reason,omitempty"`

	// Timestamp is when the test was performed
	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// PEP Request/Response Types
// =============================================================================

// CheckRequest wraps iam.CheckRequest for service layer.
type CheckRequest = iam.CheckRequest

// CheckResponse wraps iam.CheckResponse for service layer.
type CheckResponse = iam.CheckResponse

// BatchCheckRequest wraps iam.BatchCheckRequest for service layer.
type BatchCheckRequest = iam.BatchCheckRequest

// BatchCheckResponse wraps iam.BatchCheckResponse for service layer.
type BatchCheckResponse = iam.BatchCheckResponse

// SubscribePolicyUpdatesRequest requests policy update subscription.
type SubscribePolicyUpdatesRequest struct {
	// TenantID is the tenant to subscribe to
	TenantID string `json:"tenant_id"`

	// ResourcePatterns filters policy updates by resource patterns
	ResourcePatterns []string `json:"resource_patterns,omitempty"`
}

// PolicyUpdate wraps iam.PolicyUpdate for service layer.
type PolicyUpdate = iam.PolicyUpdate

// =============================================================================
// Type Aliases for Simplified Names
// =============================================================================

// GetUserRequest is an alias for IAMGetUserRequest.
type GetUserRequest = IAMGetUserRequest

// GetUserResponse is an alias for IAMGetUserResponse.
type GetUserResponse = IAMGetUserResponse

// UpdateUserRequest is an alias for IAMUpdateUserRequest.
type UpdateUserRequest = IAMUpdateUserRequest

// UpdateUserResponse is an alias for IAMUpdateUserResponse.
type UpdateUserResponse = IAMUpdateUserResponse

// UpdateRoleRequest is an alias for IAMUpdateRoleRequest.
type UpdateRoleRequest = IAMUpdateRoleRequest

// UpdateRoleResponse is an alias for IAMUpdateRoleResponse.
type UpdateRoleResponse = IAMUpdateRoleResponse

// =============================================================================
// Admin Assignment Types
// =============================================================================

// CreateAdminAssignmentRequest contains the request to create an admin assignment.
type CreateAdminAssignmentRequest struct {
	UserID            string
	AdminRole         iam.AdminRole
	TenantIDs         []string
	Permissions       []iam.AdminPermission
	DeniedPermissions []iam.AdminPermission
	ResourceScopes    []iam.ResourceScope
	GrantedBy         string
	ExpiresAt         *time.Time
	Metadata          map[string]string
}

// CreateAdminAssignmentResponse contains the response from creating an admin assignment.
type CreateAdminAssignmentResponse struct {
	Assignment *iam.AdminAssignment
}

// GetAdminAssignmentRequest contains the request to get an admin assignment.
type GetAdminAssignmentRequest struct {
	AssignmentID string
	UserID       string
}

// GetAdminAssignmentResponse contains the response from getting an admin assignment.
type GetAdminAssignmentResponse struct {
	Assignment *iam.AdminAssignment
}

// UpdateAdminAssignmentRequest contains the request to update an admin assignment.
type UpdateAdminAssignmentRequest struct {
	AssignmentID      string
	UserID            string
	AdminRole         *iam.AdminRole
	TenantIDs         []string
	Permissions       []iam.AdminPermission
	DeniedPermissions []iam.AdminPermission
	ResourceScopes    []iam.ResourceScope
	ExpiresAt         *time.Time
	Active            *bool
	Metadata          map[string]string
}

// UpdateAdminAssignmentResponse contains the response from updating an admin assignment.
type UpdateAdminAssignmentResponse struct {
	Assignment *iam.AdminAssignment
}

// DeleteAdminAssignmentRequest contains the request to delete an admin assignment.
type DeleteAdminAssignmentRequest struct {
	AssignmentID string
	UserID       string
}

// DeleteAdminAssignmentResponse contains the response from deleting an admin assignment.
type DeleteAdminAssignmentResponse struct {
	Success bool
}

// ListAdminAssignmentsRequest contains the request to list admin assignments.
type ListAdminAssignmentsRequest struct {
	TenantID   string
	UserID     string
	AdminRole  iam.AdminRole
	ActiveOnly bool
	Limit      int
	Offset     int
}

// ListAdminAssignmentsResponse contains the response from listing admin assignments.
type ListAdminAssignmentsResponse struct {
	Assignments []*iam.AdminAssignment
	Total       int
}

// CheckAdminPermissionRequest contains the request to check an admin permission.
type CheckAdminPermissionRequest struct {
	UserID       string
	Permission   iam.AdminPermission
	TenantID     string
	ResourceType string
	ResourceID   string
}

// CheckAdminPermissionResponse contains the response from checking an admin permission.
type CheckAdminPermissionResponse struct {
	Allowed bool
	Reason  string
}

// StartImpersonationRequest contains the request to start impersonation.
//
// It carries no acting-admin identity: the actor is the authenticated
// caller, resolved server-side from the verified principal, never named by
// the request (go-quicbaas#383).
type StartImpersonationRequest struct {
	TargetUserID string
	TenantID     string
	Reason       string
	Duration     time.Duration
}

// StartImpersonationResponse contains the response from starting impersonation.
type StartImpersonationResponse struct {
	SessionID string
	ExpiresAt time.Time
}

// EndImpersonationRequest contains the request to end impersonation.
type EndImpersonationRequest struct {
	SessionID string
}

// EndImpersonationResponse contains the response from ending impersonation.
type EndImpersonationResponse struct {
	Success bool
}

// ListActiveImpersonationsRequest contains the request to list active impersonations.
type ListActiveImpersonationsRequest struct {
	AdminUserID string
}

// ListActiveImpersonationsResponse contains the response from listing active impersonations.
type ListActiveImpersonationsResponse struct {
	Impersonations []*iam.ImpersonationSession
}

// =============================================================================
// GDPR Consent Types
// =============================================================================

// GrantConsentRequest contains the request to grant consent.
type GrantConsentRequest struct {
	TenantID  string
	UserID    string
	Purpose   iam.ConsentPurpose
	Version   string
	IPAddress string
	UserAgent string
	ExpiresAt *time.Time
}

// GrantConsentResponse contains the response from granting consent.
type GrantConsentResponse struct {
	ConsentID string
	Success   bool
}

// WithdrawConsentRequest contains the request to withdraw consent.
type WithdrawConsentRequest struct {
	TenantID  string
	UserID    string
	Purpose   iam.ConsentPurpose
	IPAddress string
	UserAgent string
}

// WithdrawConsentResponse contains the response from withdrawing consent.
type WithdrawConsentResponse struct {
	Success bool
}

// GetConsentRequest contains the request to get a consent.
type GetConsentRequest struct {
	TenantID string
	UserID   string
	Purpose  iam.ConsentPurpose
}

// GetConsentResponse contains the response from getting a consent.
type GetConsentResponse struct {
	Consent *iam.Consent
}

// ListUserConsentsRequest contains the request to list user consents.
type ListUserConsentsRequest struct {
	TenantID string
	UserID   string
}

// ListUserConsentsResponse contains the response from listing user consents.
type ListUserConsentsResponse struct {
	Consents []*iam.Consent
}

// RequestExportRequest contains the request to request a data export.
type RequestExportRequest struct {
	TenantID          string
	UserID            string
	RequestedBy       string
	Format            iam.DataExportFormat
	IncludeCategories []string
	ExcludeCategories []string
}

// RequestExportResponse contains the response from requesting a data export.
type RequestExportResponse struct {
	ExportID string
	Status   string
}

// GetExportStatusRequest contains the request to get export status.
type GetExportStatusRequest struct {
	ExportID string
}

// GetExportStatusResponse contains the response from getting export status.
type GetExportStatusResponse struct {
	ExportID    string
	Status      string
	DownloadURL string
	ExpiresAt   time.Time
}

// GenerateExportRequest contains the request to generate an export.
type GenerateExportRequest struct {
	ExportID string
}

// GenerateExportResponse contains the response from generating an export.
type GenerateExportResponse struct {
	Success     bool
	DownloadURL string
}

// RequestDeletionRequest contains the request to request data deletion.
type RequestDeletionRequest struct {
	TenantID           string
	UserID             string
	RequestedBy        string
	Reason             string
	GracePeriodDays    int
	DeleteCategories   []string
	PreserveCategories []string
}

// RequestDeletionResponse contains the response from requesting data deletion.
type RequestDeletionResponse struct {
	DeletionID   string
	Status       string
	ScheduledFor time.Time
}

// CancelDeletionRequest contains the request to cancel a deletion.
type CancelDeletionRequest struct {
	DeletionID  string
	CancelledBy string
}

// CancelDeletionResponse contains the response from cancelling a deletion.
type CancelDeletionResponse struct {
	Success bool
}

// ProcessDeletionRequest contains the request to process a deletion.
type ProcessDeletionRequest struct {
	DeletionID string
}

// ProcessDeletionResponse contains the response from processing a deletion.
type ProcessDeletionResponse struct {
	Success bool
}

// GetDeletionStatusRequest contains the request to get deletion status.
type GetDeletionStatusRequest struct {
	DeletionID string
}

// GetDeletionStatusResponse contains the response from getting deletion status.
type GetDeletionStatusResponse struct {
	DeletionID   string
	Status       string
	ScheduledFor time.Time
	CompletedAt  *time.Time
}
