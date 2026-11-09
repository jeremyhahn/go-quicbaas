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
	"log/slog"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
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
	//
	// The STORE assigns the identity: an implementation MUST mint a user id
	// onto a record that arrives without one, and the caller reads it back off
	// the record afterwards. A caller cannot supply an id, which is what keeps
	// there from being a second identifier space for anything to reconcile
	// against — the defect the uuid<->uint64 IDMap existed to paper over.
	CreateUser(ctx context.Context, user *User) error

	// GetUser retrieves a user by ID. A zero id never names a stored user and
	// is refused with ErrInvalidUserID.
	GetUser(ctx context.Context, tenantID string, userID uint64) (*User, error)

	// GetUserByEmail retrieves a user by their canonical login key within a
	// tenant.
	//
	// The tenant is EXPLICIT because qrdb's email index is tenant-scoped: the
	// same person may legitimately hold an account in two tenants, and every
	// login path already knows which one it authenticated into. The global
	// signature this replaced is what forced a broker-owned email side-index
	// to exist alongside qrdb's, and a verified login could miss that copy
	// while the record it named was present all along.
	GetUserByEmail(ctx context.Context, tenantID, email string) (*User, error)

	// GetUserByPIVThumbprint retrieves a user by one of their registered
	// PIV-attested cert-binding lookup thumbprints (hex SHA-256 of the cert
	// DER). A user may hold multiple PIV bindings; any of them resolves the
	// user. Returns ErrNotFound when no user owns that thumbprint.
	GetUserByPIVThumbprint(ctx context.Context, thumbprint string) (*User, error)

	// GetUserByUsername retrieves a user by tenant-scoped username. qrdb owns
	// the username index natively; the local stores maintain a username->id
	// index. Returns ErrUserNotFound when no user has that username in the
	// tenant. This is the seam the RBAC AssignRole/RevokeRole redirect uses.
	GetUserByUsername(ctx context.Context, tenantID, username string) (*User, error)

	// UpdateUser updates an existing user.
	UpdateUser(ctx context.Context, user *User) error

	// MarkUserDeleting transitions a user to LifecycleDeleting — the terminal
	// state a deletion claims the record with before it begins removing what
	// the record owns.
	//
	// It is separate from UpdateUser, and lifecycle-ONLY, for a reason the store
	// enforces: once a record is terminal it refuses any update that would
	// CHANGE it, permitting only a re-assertion of the state it already holds.
	// A full-record write therefore cannot mark a record twice, so a retried
	// deletion would fail on exactly the path that exists to make deletion
	// resumable. Marking an already-marked record is a no-op, not an error.
	MarkUserDeleting(ctx context.Context, tenantID string, userID uint64) error

	// DeleteUser removes a user.
	DeleteUser(ctx context.Context, tenantID string, userID uint64) error

	// ListUsers retrieves users with pagination and filtering.
	ListUsers(ctx context.Context, tenantID string, opts ListOptions) ([]*User, error)
}

// RoleStorage defines the interface for role persistence operations.
type RoleStorage interface {
	// CreateRole stores a new role.
	CreateRole(ctx context.Context, role *Role) error

	// GetRole retrieves a role by ID.
	GetRole(ctx context.Context, tenantID, roleID string) (*Role, error)

	// GetRoleByName retrieves a role by its tenant-scoped name. Role names are
	// unique within a tenant in every store here, so the answer is one record
	// or none: ErrRoleNotFound when the tenant holds no role of that name.
	//
	// It is a LOOKUP, not a filtered listing. The distinction is the point of
	// the method: callers used to answer "which role is named N in tenant T" by
	// listing roles and scanning the page, which the qrdb-backed store caps at
	// its default page size — so a tenant holding more roles than one page
	// could fail to resolve a role that exists. The qrdb store answers this
	// from its own name index, the KVStore-backed store from a tenant-scoped
	// name index it maintains, and the in-memory double by scanning its map.
	//
	// This is the surface qrdb's role.NameResolver needs (wired into first-boot
	// as FirstBootDependencies.RoleResolver), so a grant binds to the
	// identifier a role is actually persisted under.
	GetRoleByName(ctx context.Context, tenantID, name string) (*Role, error)

	// UpdateRole updates an existing role.
	UpdateRole(ctx context.Context, role *Role) error

	// DeleteRole removes a role.
	DeleteRole(ctx context.Context, tenantID, roleID string) error

	// ListRoles retrieves roles with pagination and filtering.
	ListRoles(ctx context.Context, tenantID string, opts ListOptions) ([]*Role, error)
}

// BuiltInRoleReconciler converges a tenant's stored built-in roles onto the
// definitions the running build carries: permissions the build now grants are
// added, permissions it no longer grants are removed, and custom roles are
// left alone.
//
// It is deliberately separate from RoleStorage. Built-in role definitions are
// go-qrdb's, not this package's, so only a role storage that speaks to qrdb
// can honour the contract; a storage that cannot reach those definitions must
// refuse rather than reconcile a tenant against a set it invented. A caller
// probes for this interface and reports ErrRoleReconcileUnsupported when the
// configured storage does not implement it.
//
// Implementations MUST be idempotent: a tenant that already agrees with the
// build reports no changed roles and performs no writes.
type BuiltInRoleReconciler interface {
	// ReconcileBuiltInRoles converges tenantID's built-in roles and reports
	// only the roles that changed.
	ReconcileBuiltInRoles(ctx context.Context, tenantID string) (*RoleReconcileReport, error)
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

	// UpdateEngineQuotas updates the engine-side numeric resource limits for
	// the tenant (databases, keys, storage bytes, etc.). Delegates to the
	// QRDBTenantProvisioner; returns nil when no engine provisioner is wired.
	UpdateEngineQuotas(ctx context.Context, id string, quotas qrdbsdk.Quotas) error

	// UpdateEngineEntitlements updates the engine-side feature entitlements for
	// the tenant (isolation tiers, dedicated-storage features, barrier modes).
	// Delegates to the QRDBTenantProvisioner; returns nil when no engine
	// provisioner is wired.
	UpdateEngineEntitlements(ctx context.Context, id string, entitlements qrdbsdk.Entitlements) error
}

// CreateTenantOptions contains options for creating a tenant.
type CreateTenantOptions struct {
	// ID is the tenant ID (optional, auto-generated if not provided)
	ID string

	// Name is the tenant name
	Name string

	// IsolationType selects the qrdb engine isolation strategy for the
	// tenant's data: qrdbsdk.TenantIsolationBarrier gives the tenant its
	// own DEK and a dedicated "{tenantID}/" namespace, while the prefix
	// mode shares the system barrier with logical key prefixing. An empty
	// value defers to qrdb's default (prefix), matching
	// tenant.TenantInfo.EffectiveIsolationType. The reserved system tenant
	// must remain prefix; qrdb rejects a barrier system tenant with
	// ErrSystemTenantMustBePrefix.
	IsolationType qrdbsdk.TenantIsolationType

	// Settings contains tenant settings
	Settings *TenantSettings

	// Quotas contains the BaaS-layer IAM quotas (max policies, apps)
	// for the tenant. These are distinct from EngineQuotas.
	Quotas *TenantQuotas

	// EngineQuotas overrides the engine-level numeric resource limits
	// (databases, keys, storage bytes, etc.) applied to this tenant at
	// creation. When nil the engine uses its own configured defaults.
	// Applied via QRDBTenantProvisioner.UpdateQuotas immediately after
	// engine-side CreateTenant succeeds.
	EngineQuotas *qrdbsdk.Quotas

	// EngineEntitlements overrides the engine-level feature entitlements
	// (isolation tiers, dedicated-storage, WAL features) for this tenant.
	// When nil the engine uses its configured defaults. Applied via
	// QRDBTenantProvisioner.UpdateEntitlements immediately after engine-side
	// CreateTenant succeeds.
	EngineEntitlements *qrdbsdk.Entitlements
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

	// RoleStorage is the role storage backend
	RoleStorage RoleStorage

	// Tenants is the tenant manager
	Tenants TenantManager

	// Logger records what an update did to a user's enrolled WebAuthn
	// credentials. Optional; the process default is used when nil.
	Logger *slog.Logger
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
	Attributes map[string]string

	// Enabled indicates whether the user may authenticate. Nil means enabled,
	// which is why it is a pointer: a plain bool cannot tell "the caller wants
	// this user disabled" from "the caller said nothing", and the zero value
	// of the wrong one of those suspends every user created without an
	// explicit flag. UpdateUserOptions.Enabled is a pointer for the same
	// reason; this one used to be a plain bool and was therefore ignored.
	Enabled *bool

	// AuthMethods lists the allowed authentication methods (e.g. "webauthn", "piv").
	// Defaults to ["webauthn"] when empty.
	AuthMethods []string

	// Lifecycle is the state the user starts in. Empty defaults to
	// LifecyclePending — a user that exists but has not activated.
	//
	// Callers that are not creating an invite-pending account should say so
	// rather than inherit the default: first-boot's admin is cert-enrolled,
	// and a federated user has already been authenticated by their IdP.
	// Neither is awaiting an invite, and a record that claims otherwise
	// misdescribes the account to everything that reads it.
	//
	// qrdb accepts only pending, active or disabled at creation: suspended and
	// locked are things that HAPPEN to an account, not states one is born in.
	Lifecycle Lifecycle

	// Roles is the list of role IDs to assign to the new user.
	Roles []string
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
	Attributes *map[string]string

	// Enabled indicates if the user is enabled
	Enabled *bool

	// Roles is the list of role IDs assigned to the user
	Roles *[]string

	// Lifecycle, when non-empty, transitions the account to that state. The
	// move is checked against qrdb's transition table, so an illegal one is
	// refused by the store rather than written.
	Lifecycle *Lifecycle

	// AuthMethods, when non-nil, replaces the allowed authentication methods.
	AuthMethods *[]string

	// PIVCertThumbprint, when non-nil, replaces the user's PIV-attested cert
	// bindings with a single binding for this lookup thumbprint (hex SHA-256 of
	// the cert DER). A blank value clears all PIV-attested bindings. Non-PIV
	// bindings are preserved.
	PIVCertThumbprint *string

	// PIVCertThumbprints, when non-nil, replaces the user's PIV-attested cert
	// bindings with one binding per supplied thumbprint. Activation paths bind
	// both the canonical SPKI thumbprint and the legacy hex digest, and qrdb's
	// cert-thumbprint index resolves both to the user; see
	// User.SetPIVThumbprints. Takes precedence over PIVCertThumbprint.
	PIVCertThumbprints *[]string

	// SPKICertThumbprint, when non-nil, replaces the user's canonical
	// SPKI-thumbprint cert bindings with a single binding for this value (the
	// base64url SHA-256 over SubjectPublicKeyInfo produced by
	// qrdbsdk.ComputeSPKIThumbprint). A blank value clears them. Bindings of
	// other kinds are preserved. This is the identifier the admin-mTLS
	// first-boot enrollment binds and that qrdb resolves natively.
	SPKICertThumbprint *string
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
// TenantQuota tracks BaaS-layer IAM quota usage. Engine-level quotas (users,
// roles, storage bytes, etc.) are authoritative in qrdb's TenantServicer; only
// IAM-subsystem concepts not tracked by the engine belong here.
type TenantQuota struct {
	// MaxPolicies is the maximum number of ABAC policies allowed.
	MaxPolicies int64

	// MaxApps is the maximum number of OAuth2/OIDC applications allowed.
	MaxApps int64

	// CurrentPolicies is the current number of ABAC policies.
	CurrentPolicies int64

	// CurrentApps is the current number of OAuth2/OIDC applications.
	CurrentApps int64
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

// PolicyWriteSnapshotter is a PolicyStorage whose one policy write can
// touch more than one stored record. SnapshotPolicy captures every record a
// write of tenantID's policy name may change; restore puts them all back
// as they were, deleting any that did not exist.
type PolicyWriteSnapshotter interface {
	SnapshotPolicy(ctx context.Context, tenantID, name string) (restore func(context.Context) error, err error)
}
