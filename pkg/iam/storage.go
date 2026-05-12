//go:build iam

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

package iam

import (
	"context"
	"time"
)

// TenantStorage defines the interface for tenant persistence operations.
// Implementations must provide thread-safe, atomic operations for tenant data.
type TenantStorage interface {
	// CreateTenant stores a new tenant.
	// Returns ErrAlreadyExists if a tenant with the same ID exists.
	CreateTenant(ctx context.Context, tenant *Tenant) error

	// GetTenant retrieves a tenant by ID.
	// Returns ErrNotFound if the tenant does not exist.
	GetTenant(ctx context.Context, id string) (*Tenant, error)

	// UpdateTenant updates an existing tenant.
	// Returns ErrNotFound if the tenant does not exist.
	UpdateTenant(ctx context.Context, tenant *Tenant) error

	// DeleteTenant removes a tenant.
	// Returns ErrNotFound if the tenant does not exist.
	DeleteTenant(ctx context.Context, id string) error

	// ListTenants retrieves tenants with pagination and filtering.
	ListTenants(ctx context.Context, opts ListOptions) ([]*Tenant, error)

	// CountTenants returns the total number of tenants matching the filter.
	CountTenants(ctx context.Context, opts ListOptions) (int64, error)

	// TenantExists checks if a tenant with the given ID exists.
	TenantExists(ctx context.Context, id string) (bool, error)

	// GetSettings retrieves tenant settings.
	// Returns ErrNotFound if the tenant does not exist.
	GetSettings(ctx context.Context, tenantID string) (*TenantSettings, error)

	// UpdateSettings updates tenant settings.
	// Returns ErrNotFound if the tenant does not exist.
	UpdateSettings(ctx context.Context, tenantID string, settings *TenantSettings) error

	// GetQuota retrieves tenant quota information.
	// Returns ErrNotFound if the tenant does not exist.
	GetQuota(ctx context.Context, tenantID string) (*TenantQuota, error)

	// UpdateQuota updates tenant quota information.
	// Returns ErrNotFound if the tenant does not exist.
	UpdateQuota(ctx context.Context, tenantID string, quota *TenantQuota) error

	// IncrementQuotaUsage atomically increments a quota usage counter.
	// Returns ErrResourceExhausted if the increment would exceed the limit.
	IncrementQuotaUsage(ctx context.Context, tenantID string, resource string, delta int64) error

	// DecrementQuotaUsage atomically decrements a quota usage counter.
	DecrementQuotaUsage(ctx context.Context, tenantID string, resource string, delta int64) error
}

// UserStorage defines the interface for user persistence operations.
type UserStorage interface {
	// CreateUser stores a new user.
	CreateUser(ctx context.Context, user *User) error

	// GetUser retrieves a user by ID.
	GetUser(ctx context.Context, tenantID, userID string) (*User, error)

	// GetUserByEmail retrieves a user by email (global lookup).
	GetUserByEmail(ctx context.Context, email string) (*User, error)

	// GetUserByPIVThumbprint retrieves a user by their registered PIV certificate
	// SHA-256 thumbprint. Returns ErrNotFound when no user owns that thumbprint.
	GetUserByPIVThumbprint(ctx context.Context, thumbprint string) (*User, error)

	// UpdateUser updates an existing user.
	UpdateUser(ctx context.Context, user *User) error

	// DeleteUser removes a user.
	DeleteUser(ctx context.Context, tenantID, userID string) error

	// ListUsers retrieves users with pagination and filtering.
	ListUsers(ctx context.Context, tenantID string, opts ListOptions) ([]*User, error)
}

// GroupStorage defines the interface for group persistence operations.
type GroupStorage interface {
	// CreateGroup stores a new group.
	CreateGroup(ctx context.Context, group *Group) error

	// GetGroup retrieves a group by ID.
	GetGroup(ctx context.Context, tenantID, groupID string) (*Group, error)

	// UpdateGroup updates an existing group.
	UpdateGroup(ctx context.Context, group *Group) error

	// DeleteGroup removes a group.
	DeleteGroup(ctx context.Context, tenantID, groupID string) error

	// ListGroups retrieves groups with pagination and filtering.
	ListGroups(ctx context.Context, tenantID string, opts ListOptions) ([]*Group, error)
}

// RoleStorage defines the interface for role persistence operations.
type RoleStorage interface {
	// CreateRole stores a new role.
	CreateRole(ctx context.Context, role *Role) error

	// GetRole retrieves a role by ID.
	GetRole(ctx context.Context, tenantID, roleID string) (*Role, error)

	// UpdateRole updates an existing role.
	UpdateRole(ctx context.Context, role *Role) error

	// DeleteRole removes a role.
	DeleteRole(ctx context.Context, tenantID, roleID string) error

	// ListRoles retrieves roles with pagination and filtering.
	ListRoles(ctx context.Context, tenantID string, opts ListOptions) ([]*Role, error)
}

// AppStorage defines the interface for OAuth2 application persistence.
type AppStorage interface {
	// CreateApp stores a new OAuth2 application.
	CreateApp(ctx context.Context, app *Application) error

	// GetApp retrieves an application by client ID.
	GetApp(ctx context.Context, clientID string) (*Application, error)

	// GetAppByTenant retrieves an application by client ID and tenant.
	GetAppByTenant(ctx context.Context, tenantID, clientID string) (*Application, error)

	// UpdateApp updates an existing application.
	UpdateApp(ctx context.Context, app *Application) error

	// DeleteApp removes an application.
	DeleteApp(ctx context.Context, clientID string) error

	// ListApps retrieves applications for a tenant.
	ListApps(ctx context.Context, tenantID string, opts ListOptions) ([]*Application, error)

	// ValidateClientSecret verifies a client secret (constant-time comparison).
	ValidateClientSecret(ctx context.Context, clientID, clientSecret string) (bool, error)
}

// TokenStorage defines the interface for OAuth2 token persistence.
type TokenStorage interface {
	// StoreAuthCode stores an authorization code.
	StoreAuthCode(ctx context.Context, code *AuthCodeData) error

	// GetAuthCode retrieves an authorization code.
	GetAuthCode(ctx context.Context, code string) (*AuthCodeData, error)

	// DeleteAuthCode removes an authorization code (one-time use).
	DeleteAuthCode(ctx context.Context, code string) error

	// StoreToken stores an access or refresh token.
	StoreToken(ctx context.Context, token *Token) error

	// GetToken retrieves a token by JTI.
	GetToken(ctx context.Context, jti string) (*Token, error)

	// GetTokenByValue retrieves a token by its string value (for refresh tokens).
	GetTokenByValue(ctx context.Context, tokenValue string) (*Token, error)

	// RevokeToken marks a token as revoked.
	RevokeToken(ctx context.Context, jti string) error

	// IsTokenRevoked checks if a token is revoked.
	IsTokenRevoked(ctx context.Context, jti string) (bool, error)

	// DeleteExpiredTokens removes expired tokens (cleanup).
	DeleteExpiredTokens(ctx context.Context) error

	// ListTokensByUser retrieves tokens for a user.
	ListTokensByUser(ctx context.Context, userID string) ([]*Token, error)

	// RevokeUserTokens revokes all tokens for a user.
	RevokeUserTokens(ctx context.Context, userID string) error
}

// ListOptions defines pagination and filtering options for list operations.
type ListOptions struct {
	// Limit is the maximum number of results to return.
	Limit int

	// Offset is the number of results to skip.
	Offset int

	// Filter contains field-specific filters.
	Filter map[string]interface{}

	// SortBy specifies the field to sort by.
	SortBy string

	// SortDesc indicates descending sort order.
	SortDesc bool
}

// TenantManager interface defines tenant management operations.
type TenantManager interface {
	// CreateTenant creates a new tenant.
	CreateTenant(ctx context.Context, opts CreateTenantOptions) (*Tenant, error)

	// GetTenant retrieves a tenant by ID.
	GetTenant(ctx context.Context, id string) (*Tenant, error)

	// UpdateTenant updates a tenant.
	UpdateTenant(ctx context.Context, id string, opts UpdateTenantOptions) (*Tenant, error)

	// DeleteTenant deletes a tenant.
	DeleteTenant(ctx context.Context, id string) error

	// ListTenants lists tenants.
	ListTenants(ctx context.Context, opts ListOptions) ([]*Tenant, error)

	// CheckQuota checks if a tenant can create a resource.
	CheckQuota(ctx context.Context, tenantID, resourceType string) error
}

// CreateTenantOptions contains options for creating a tenant.
type CreateTenantOptions struct {
	// ID is the tenant ID (optional, auto-generated if not provided)
	ID string

	// Name is the tenant name
	Name string

	// Settings contains tenant settings
	Settings *TenantSettings

	// Quotas contains tenant quotas
	Quotas *TenantQuotas
}

// UpdateTenantOptions contains options for updating a tenant.
type UpdateTenantOptions struct {
	// Name is the tenant name
	Name *string

	// Settings contains tenant settings
	Settings *TenantSettings

	// Quotas contains tenant quotas
	Quotas *TenantQuotas
}

// IdentityStoreOptions contains options for creating an identity store.
type IdentityStoreOptions struct {
	// UserStorage is the user storage backend
	UserStorage UserStorage

	// GroupStorage is the group storage backend
	GroupStorage GroupStorage

	// RoleStorage is the role storage backend
	RoleStorage RoleStorage

	// Tenants is the tenant manager
	Tenants TenantManager
}

// CreateUserOptions contains options for creating a user.
type CreateUserOptions struct {
	// ID is an optional user ID. If empty, a UUID will be generated.
	ID string

	// Email is the user's email
	Email string

	// Username is the user's username
	Username string

	// Name is the user's display name
	Name string

	// Attributes contains custom attributes
	Attributes map[string]interface{}

	// Enabled indicates if the user is enabled
	Enabled bool

	// AuthMethods lists the allowed authentication methods (e.g. "webauthn", "piv").
	// Defaults to ["webauthn"] when empty.
	AuthMethods []string

	// Roles is the list of role IDs to assign to the new user.
	Roles []string

	// InviteExpiry is the duration until the invite token expires.
	// Defaults to 24 hours when zero.
	InviteExpiry time.Duration
}

// UpdateUserOptions contains options for updating a user.
type UpdateUserOptions struct {
	// Email is the user's email
	Email *string

	// Username is the user's username
	Username *string

	// Name is the user's display name
	Name *string

	// Attributes contains custom attributes
	Attributes *map[string]interface{}

	// Enabled indicates if the user is enabled
	Enabled *bool

	// Roles is the list of role IDs assigned to the user
	Roles *[]string

	// Groups is the list of group IDs the user belongs to
	Groups *[]string

	// State, when non-empty, overrides the user lifecycle state.
	State *UserState

	// AuthMethods, when non-nil, replaces the allowed authentication methods.
	AuthMethods *[]string

	// PIVCertThumbprint, when non-nil, sets or clears the PIV certificate thumbprint.
	PIVCertThumbprint *string

	// InviteToken, when non-nil, sets or clears the pending invite token.
	InviteToken *string

	// InviteExpiresAt, when non-nil, sets or clears the invite expiry timestamp.
	InviteExpiresAt **time.Time
}

// CreateGroupOptions contains options for creating a group.
type CreateGroupOptions struct {
	// ID is an optional group ID. If empty, a UUID will be generated.
	ID string

	// Name is the group name
	Name string

	// Description is the group description
	Description string

	// Attributes contains custom attributes
	Attributes map[string]interface{}
}

// UpdateGroupOptions contains options for updating a group.
type UpdateGroupOptions struct {
	// Name is the group name
	Name *string

	// Description is the group description
	Description *string

	// Attributes contains custom attributes
	Attributes *map[string]interface{}

	// Members is the list of user IDs that are members of this group
	Members *[]string
}

// CreateRoleOptions contains options for creating a role.
type CreateRoleOptions struct {
	// ID is an optional role ID. If empty, a UUID will be generated.
	ID string

	// Name is the role name
	Name string

	// Description is the role description
	Description string

	// Permissions is the list of permissions
	Permissions []string

	// Attributes contains custom attributes
	Attributes map[string]interface{}
}

// UpdateRoleOptions contains options for updating a role.
type UpdateRoleOptions struct {
	// Name is the role name
	Name *string

	// Description is the role description
	Description *string

	// Permissions is the list of permissions
	Permissions *[]string

	// Attributes contains custom attributes
	Attributes *map[string]interface{}
}

// TenantQuota represents tenant resource quotas.
type TenantQuota struct {
	// MaxUsers is the maximum number of users
	MaxUsers int64

	// MaxGroups is the maximum number of groups
	MaxGroups int64

	// MaxRoles is the maximum number of roles
	MaxRoles int64

	// MaxPolicies is the maximum number of policies
	MaxPolicies int64

	// MaxApps is the maximum number of applications
	MaxApps int64

	// MaxStorageBytes is the maximum storage in bytes
	MaxStorageBytes int64

	// CurrentUsers is the current number of users
	CurrentUsers int64

	// CurrentGroups is the current number of groups
	CurrentGroups int64

	// CurrentRoles is the current number of roles
	CurrentRoles int64

	// CurrentPolicies is the current number of policies
	CurrentPolicies int64

	// CurrentApps is the current number of applications
	CurrentApps int64

	// CurrentStorageBytes is the current storage usage in bytes
	CurrentStorageBytes int64
}

// PolicyStorage defines the interface for policy persistence operations.
type PolicyStorage interface {
	// CreatePolicy creates a new policy.
	CreatePolicy(ctx context.Context, tenantID string, policy *Policy) error

	// GetPolicy retrieves a policy by name.
	GetPolicy(ctx context.Context, tenantID, name string) (*Policy, error)

	// UpdatePolicy updates an existing policy.
	UpdatePolicy(ctx context.Context, tenantID string, policy *Policy) error

	// DeletePolicy deletes a policy by name.
	DeletePolicy(ctx context.Context, tenantID, name string) error

	// ListPolicies lists policies for a tenant.
	ListPolicies(ctx context.Context, tenantID string, opts ListOptions) ([]*Policy, error)
}
