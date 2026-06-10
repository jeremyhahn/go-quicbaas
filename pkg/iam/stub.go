//go:build !iam

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

// Package iam provides Identity and Access Management functionality for QuicBaaS.
// This is a stub file that provides minimal types when the iam build tag is not set.
package iam

import (
	"context"
	"errors"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ErrIAMNotCompiled is returned when IAM functions are called but IAM support
// was not compiled into the binary (build with -tags iam).
var ErrIAMNotCompiled = errors.New("iam support not compiled - build with -tags iam")

// Context key types for type safety.
type tenantContextKey struct{}
type userContextKey struct{}
type parameterContextKey struct{}

// Core types - minimal stubs for compilation.

// UserState represents the lifecycle state of a user account.
type UserState string

// UserState constants define the possible states of a user account.
const (
	UserStatePending  UserState = "pending"
	UserStateActive   UserState = "active"
	UserStateDisabled UserState = "disabled"
)

// Tenant represents a logical isolation boundary in QuicBaaS IAM.
type Tenant struct {
	ID       string         `json:"id" yaml:"id"`
	Name     string         `json:"name" yaml:"name"`
	Settings TenantSettings `json:"settings" yaml:"settings"`
	Quotas   TenantQuotas   `json:"quotas,omitempty" yaml:"quotas,omitempty"`
	Created  time.Time      `json:"created" yaml:"created"`
	Updated  time.Time      `json:"updated" yaml:"updated"`
}

// TenantSettings contains tenant-specific configuration options.
type TenantSettings struct {
	MaxUsers           int                 `json:"max_users,omitempty" yaml:"max_users,omitempty"`
	MFARequired        bool                `json:"mfa_required" yaml:"mfa_required"`
	MFAPolicy          *MFAPolicy          `json:"mfa_policy,omitempty" yaml:"mfa_policy,omitempty"`
	NamedNetworks      map[string][]string `json:"named_networks,omitempty" yaml:"named_networks,omitempty"`
	SessionTimeout     time.Duration       `json:"session_timeout,omitempty" yaml:"session_timeout,omitempty"`
	SessionIdleTimeout time.Duration       `json:"session_idle_timeout,omitempty" yaml:"session_idle_timeout,omitempty"`
	TokenTTL           time.Duration       `json:"token_ttl,omitempty" yaml:"token_ttl,omitempty"`
	RefreshTokenTTL    time.Duration       `json:"refresh_token_ttl,omitempty" yaml:"refresh_token_ttl,omitempty"`
	// AuthzMode optionally overrides the deployment-level security.authz_mode
	// for this tenant. Valid values: "offline", "online", "hybrid". Empty means
	// "use the deployment default".
	AuthzMode string `json:"authz_mode,omitempty" yaml:"authz_mode,omitempty"`
}

// TenantQuotas defines BaaS-layer IAM resource limits for a tenant.
// Engine-level quotas (users, roles, storage, etc.) are owned by qrdb TenantServicer.
type TenantQuotas struct {
	MaxGroups   int `json:"max_groups,omitempty" yaml:"max_groups,omitempty"`
	MaxPolicies int `json:"max_policies,omitempty" yaml:"max_policies,omitempty"`
	MaxApps     int `json:"max_apps,omitempty" yaml:"max_apps,omitempty"`
}

// TenantQuota defines quota information for a tenant.
type TenantQuota struct {
	TenantID string `json:"tenant_id" yaml:"tenant_id"`
}

// User represents an authenticated principal in the IAM system.
type User struct {
	ID                string                 `json:"id" yaml:"id"`
	Email             string                 `json:"email" yaml:"email"`
	Name              string                 `json:"name" yaml:"name"`
	TenantID          string                 `json:"tenant_id" yaml:"tenant_id"`
	Roles             []string               `json:"roles,omitempty" yaml:"roles,omitempty"`
	Groups            []string               `json:"groups,omitempty" yaml:"groups,omitempty"`
	Attributes        map[string]interface{} `json:"attributes,omitempty" yaml:"attributes,omitempty"`
	Created           time.Time              `json:"created" yaml:"created"`
	Updated           time.Time              `json:"updated" yaml:"updated"`
	LastAuth          *time.Time             `json:"last_auth,omitempty" yaml:"last_auth,omitempty"`
	MFAEnabled        bool                   `json:"mfa_enabled" yaml:"mfa_enabled"`
	MFAMethod         MFAMethod              `json:"mfa_method,omitempty" yaml:"mfa_method,omitempty"`
	MFAEnrolledAt     *time.Time             `json:"mfa_enrolled_at,omitempty" yaml:"mfa_enrolled_at,omitempty"`
	MFAVerified       bool                   `json:"mfa_verified" yaml:"mfa_verified"`
	MFATimestamp      *time.Time             `json:"mfa_timestamp,omitempty" yaml:"mfa_timestamp,omitempty"`
	Suspended         bool                   `json:"suspended,omitempty" yaml:"suspended,omitempty"`
	Locked            bool                   `json:"locked,omitempty" yaml:"locked,omitempty"`
	State             UserState              `json:"state,omitempty" yaml:"state,omitempty"`
	AuthMethods       []string               `json:"auth_methods,omitempty" yaml:"auth_methods,omitempty"`
	PIVCertThumbprint string                 `json:"piv_cert_thumbprint,omitempty" yaml:"piv_cert_thumbprint,omitempty"`
	InviteToken       string                 `json:"invite_token,omitempty" yaml:"invite_token,omitempty"`
	InviteExpiresAt   *time.Time             `json:"invite_expires_at,omitempty" yaml:"invite_expires_at,omitempty"`
}

// Group represents a collection of users for policy targeting.
type Group struct {
	ID          string    `json:"id" yaml:"id"`
	Name        string    `json:"name" yaml:"name"`
	Description string    `json:"description,omitempty" yaml:"description,omitempty"`
	TenantID    string    `json:"tenant_id" yaml:"tenant_id"`
	Members     []string  `json:"members,omitempty" yaml:"members,omitempty"`
	Created     time.Time `json:"created" yaml:"created"`
	Updated     time.Time `json:"updated" yaml:"updated"`
}

// Role represents a named set of permissions.
type Role struct {
	ID          string    `json:"id" yaml:"id"`
	Name        string    `json:"name" yaml:"name"`
	Description string    `json:"description,omitempty" yaml:"description,omitempty"`
	TenantID    string    `json:"tenant_id" yaml:"tenant_id"`
	Permissions []string  `json:"permissions,omitempty" yaml:"permissions,omitempty"`
	Created     time.Time `json:"created" yaml:"created"`
	Updated     time.Time `json:"updated" yaml:"updated"`
}

// Application represents an OAuth2/OIDC client application.
type Application struct {
	ClientID                string    `json:"client_id" yaml:"client_id"`
	ClientSecret            string    `json:"client_secret,omitempty" yaml:"client_secret,omitempty"`
	Name                    string    `json:"name" yaml:"name"`
	TenantID                string    `json:"tenant_id" yaml:"tenant_id"`
	RedirectURIs            []string  `json:"redirect_uris,omitempty" yaml:"redirect_uris,omitempty"`
	ResourcePrefix          string    `json:"resource_prefix,omitempty" yaml:"resource_prefix,omitempty"`
	GrantTypes              []string  `json:"grant_types,omitempty" yaml:"grant_types,omitempty"`
	ResponseTypes           []string  `json:"response_types,omitempty" yaml:"response_types,omitempty"`
	TokenEndpointAuthMethod string    `json:"token_endpoint_auth_method,omitempty" yaml:"token_endpoint_auth_method,omitempty"`
	MTLSCertThumbprint      string    `json:"mtls_cert_thumbprint,omitempty" yaml:"mtls_cert_thumbprint,omitempty"`
	MTLSSubjectDN           string    `json:"mtls_subject_dn,omitempty" yaml:"mtls_subject_dn,omitempty"`
	MTLSSANDNS              string    `json:"mtls_san_dns,omitempty" yaml:"mtls_san_dns,omitempty"`
	Created                 time.Time `json:"created" yaml:"created"`
	Updated                 time.Time `json:"updated" yaml:"updated"`
}

// Policy represents an ABAC policy.
type Policy struct {
	APIVersion string         `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	Kind       string         `json:"kind,omitempty" yaml:"kind,omitempty"`
	Metadata   PolicyMetadata `json:"metadata" yaml:"metadata"`
	Spec       PolicySpec     `json:"spec" yaml:"spec"`
}

// PolicyMetadata contains policy metadata.
type PolicyMetadata struct {
	Name        string            `json:"name" yaml:"name"`
	Tenant      string            `json:"tenant" yaml:"tenant"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Priority    int               `json:"priority,omitempty" yaml:"priority,omitempty"`
	Created     time.Time         `json:"created,omitempty" yaml:"created,omitempty"`
	Updated     time.Time         `json:"updated,omitempty" yaml:"updated,omitempty"`
}

// PolicySpec contains policy specification.
type PolicySpec struct {
	Subjects    SubjectSelector   `json:"subjects,omitempty" yaml:"subjects,omitempty"`
	Variables   map[string]string `json:"variables,omitempty" yaml:"variables,omitempty"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
	Rules       []Rule            `json:"rules,omitempty" yaml:"rules,omitempty"`
}

// SubjectSelector defines criteria for matching subjects.
type SubjectSelector struct {
	Users         []string               `json:"users,omitempty" yaml:"users,omitempty"`
	Groups        []string               `json:"groups,omitempty" yaml:"groups,omitempty"`
	Roles         []string               `json:"roles,omitempty" yaml:"roles,omitempty"`
	Services      []string               `json:"services,omitempty" yaml:"services,omitempty"`
	Match         map[string]interface{} `json:"match,omitempty" yaml:"match,omitempty"`
	Authenticated bool                   `json:"authenticated,omitempty" yaml:"authenticated,omitempty"`
	Any           bool                   `json:"any,omitempty" yaml:"any,omitempty"`
}

// Rule represents a single policy rule.
type Rule struct {
	Name      string      `json:"name,omitempty" yaml:"name,omitempty"`
	Effect    Effect      `json:"effect" yaml:"effect"`
	Actions   []string    `json:"actions,omitempty" yaml:"actions,omitempty"`
	Resources []string    `json:"resources,omitempty" yaml:"resources,omitempty"`
	When      []Condition `json:"when,omitempty" yaml:"when,omitempty"`
}

// Effect represents the effect of a policy rule (allow/deny).
type Effect string

// Effect constants.
const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
)

// Condition represents a policy condition.
type Condition struct {
	Expression string `json:"expression,omitempty" yaml:"expression,omitempty"`
}

// CheckRequest represents an authorization check request.
type CheckRequest struct {
	Subject  Subject                `json:"subject" yaml:"subject"`
	Action   string                 `json:"action" yaml:"action"`
	Resource string                 `json:"resource" yaml:"resource"`
	Context  map[string]interface{} `json:"context,omitempty" yaml:"context,omitempty"`
}

// Subject represents the subject of an authorization request.
// This stub includes all fields needed for RBAC+ABAC hybrid authorization.
type Subject struct {
	// ID is the subject identifier
	ID string `json:"id" yaml:"id"`

	// Name is the subject's display name
	Name string `json:"name,omitempty" yaml:"name,omitempty"`

	// Email is the subject's email address
	Email string `json:"email,omitempty" yaml:"email,omitempty"`

	// Tenant is the subject's tenant identifier (alias for TenantID for compatibility)
	Tenant string `json:"tenant,omitempty" yaml:"tenant,omitempty"`

	// TenantID is the subject's tenant
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Roles is the list of role IDs assigned to the subject
	Roles []string `json:"roles,omitempty" yaml:"roles,omitempty"`

	// Groups is the list of group IDs the subject belongs to
	Groups []string `json:"groups,omitempty" yaml:"groups,omitempty"`

	// Attributes contains custom attributes for ABAC evaluation
	Attributes map[string]interface{} `json:"attributes,omitempty" yaml:"attributes,omitempty"`

	// Attrs is an alias for Attributes for backward compatibility
	Attrs map[string]interface{} `json:"attrs,omitempty" yaml:"attrs,omitempty"`

	// Authenticated indicates whether the subject is authenticated
	Authenticated bool `json:"authenticated,omitempty" yaml:"authenticated,omitempty"`
}

// CheckResponse represents an authorization check response.
type CheckResponse struct {
	Allowed          bool   `json:"allowed" yaml:"allowed"`
	Reason           string `json:"reason,omitempty" yaml:"reason,omitempty"`
	MatchedPolicy    string `json:"matched_policy,omitempty" yaml:"matched_policy,omitempty"`
	MatchedRule      string `json:"matched_rule,omitempty" yaml:"matched_rule,omitempty"`
	EvaluationTimeNs int64  `json:"evaluation_time_ns,omitempty" yaml:"evaluation_time_ns,omitempty"`
}

// ListOptions provides pagination options.
type ListOptions struct {
	Limit  int                    `json:"limit,omitempty" yaml:"limit,omitempty"`
	Offset int                    `json:"offset,omitempty" yaml:"offset,omitempty"`
	Cursor string                 `json:"cursor,omitempty" yaml:"cursor,omitempty"`
	Filter map[string]interface{} `json:"filter,omitempty" yaml:"filter,omitempty"`
}

// MFAMethod represents the type of MFA method.
type MFAMethod string

// MFAPolicy defines MFA requirements.
type MFAPolicy struct {
	Required bool `json:"required" yaml:"required"`
}

// MFAChallenge represents an MFA challenge.
type MFAChallenge struct {
	ID string `json:"id" yaml:"id"`
}

// MFAEnrollResponse represents an MFA enrollment response.
type MFAEnrollResponse struct {
	EnrollmentID string `json:"enrollment_id" yaml:"enrollment_id"`
}

// MFAVerifyResponse represents an MFA verification response.
type MFAVerifyResponse struct {
	Success bool `json:"success" yaml:"success"`
}

// MFAStatus represents the MFA status for a user.
type MFAStatus struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

// BackupCodeStatus represents backup code status.
type BackupCodeStatus struct {
	Remaining int `json:"remaining" yaml:"remaining"`
}

// AdminRole represents an admin role type.
type AdminRole string

// AdminPermission represents an admin permission.
type AdminPermission string

// Admin role constants.
const (
	AdminRoleSuperAdmin  AdminRole = "super_admin"
	AdminRoleTenantAdmin AdminRole = "tenant_admin"
	AdminRoleUserAdmin   AdminRole = "user_admin"
	AdminRoleGroupAdmin  AdminRole = "group_admin"
	AdminRolePolicyAdmin AdminRole = "policy_admin"
	AdminRoleAuditAdmin  AdminRole = "audit_admin"
	AdminRoleReadOnly    AdminRole = "read_only"
)

// AdminAssignment represents an admin assignment.
type AdminAssignment struct {
	ID        string    `json:"id" yaml:"id"`
	UserID    string    `json:"user_id" yaml:"user_id"`
	AdminRole AdminRole `json:"admin_role" yaml:"admin_role"`
	Created   time.Time `json:"created" yaml:"created"`
}

// ResourceScope defines the scope of admin permissions.
type ResourceScope struct {
	Type string `json:"type" yaml:"type"`
	ID   string `json:"id" yaml:"id"`
}

// SessionExt represents an extended session.
type SessionExt struct {
	ID        string    `json:"id" yaml:"id"`
	UserID    string    `json:"user_id" yaml:"user_id"`
	TenantID  string    `json:"tenant_id" yaml:"tenant_id"`
	CreatedAt time.Time `json:"created_at" yaml:"created_at"`
}

// AuthorizeRequest represents an OIDC authorization request.
type AuthorizeRequest struct {
	ClientID     string `json:"client_id" yaml:"client_id"`
	RedirectURI  string `json:"redirect_uri" yaml:"redirect_uri"`
	ResponseType string `json:"response_type" yaml:"response_type"`
}

// AuthorizeResponse represents an OIDC authorization response.
type AuthorizeResponse struct {
	Code string `json:"code" yaml:"code"`
}

// TokenRequest represents an OIDC token request.
type TokenRequest struct {
	GrantType string `json:"grant_type" yaml:"grant_type"`
	Code      string `json:"code" yaml:"code"`
}

// TokenResponse represents an OIDC token response.
type TokenResponse struct {
	AccessToken string `json:"access_token" yaml:"access_token"`
}

// LoginRedirect represents a login redirect response.
type LoginRedirect struct {
	URL string `json:"url" yaml:"url"`
}

// Consent represents a GDPR consent record.
type Consent struct {
	ID       string `json:"id" yaml:"id"`
	UserID   string `json:"user_id" yaml:"user_id"`
	TenantID string `json:"tenant_id" yaml:"tenant_id"`
}

// DataExportFormat represents export format options.
type DataExportFormat string

// ParameterOperation represents the type of parameter operation.
type ParameterOperation = string

// Parameter operation constants.
const (
	ParameterOperationGet         = "get"
	ParameterOperationSet         = "set"
	ParameterOperationDelete      = "delete"
	ParameterOperationList        = "list"
	ParameterOperationSearch      = "search"
	ParameterOperationBatchGet    = "batch_get"
	ParameterOperationBatchSet    = "batch_set"
	ParameterOperationBatchDelete = "batch_delete"
)

// ParameterContext holds parameter store operation context.
type ParameterContext struct {
	// Path is the hierarchical parameter path (e.g., "/app/database/host").
	Path string

	// Type is the parameter value type (string, number, json, binary).
	Type string

	// IsEncrypted indicates if the parameter value is encrypted.
	IsEncrypted bool

	// Tags contains the parameter's tags for tag-based policy evaluation.
	Tags map[string]string

	// Owner is the user ID who owns this parameter (if applicable).
	Owner string

	// TenantID is the tenant that owns this parameter.
	TenantID string

	// Operation is the operation being performed (get, set, delete, list, search).
	Operation string

	// BatchSize is the number of parameters in batch operations.
	BatchSize int

	// AuditEnabled indicates if audit logging is enabled for this parameter.
	AuditEnabled bool

	// Description is the parameter description (if available).
	Description string
}

// ParameterContextFromContext extracts parameter context from the request context.
func ParameterContextFromContext(ctx context.Context) *ParameterContext {
	if v := ctx.Value(parameterContextKey{}); v != nil {
		if pc, ok := v.(*ParameterContext); ok {
			return pc
		}
	}
	return nil
}

// CacheConfig holds policy cache configuration.
type CacheConfig struct {
	MaxSize           int           `json:"max_size" yaml:"max_size"`
	TTL               time.Duration `json:"ttl" yaml:"ttl"`
	BloomFilterSize   int           `json:"bloom_filter_size" yaml:"bloom_filter_size"`
	BloomFilterHashes int           `json:"bloom_filter_hashes" yaml:"bloom_filter_hashes"`
}

// NewCacheConfig creates a new CacheConfig from PEPConfig.
func NewCacheConfig(pepConfig *PEPConfig) *CacheConfig {
	if pepConfig == nil {
		return &CacheConfig{
			MaxSize:           1000,
			TTL:               5 * time.Minute,
			BloomFilterSize:   10000,
			BloomFilterHashes: 4,
		}
	}
	return &CacheConfig{
		MaxSize:           pepConfig.CacheSize,
		TTL:               pepConfig.CacheTTL,
		BloomFilterSize:   pepConfig.BloomFilterSize,
		BloomFilterHashes: pepConfig.BloomFilterHashes,
	}
}

// PEPConfig holds Policy Enforcement Point configuration.
type PEPConfig struct {
	TenantID          string        `json:"tenant_id" yaml:"tenant_id"`
	CacheEnabled      bool          `json:"cache_enabled" yaml:"cache_enabled"`
	CacheSize         int           `json:"cache_size" yaml:"cache_size"`
	CacheTTL          time.Duration `json:"cache_ttl" yaml:"cache_ttl"`
	BloomFilterSize   int           `json:"bloom_filter_size" yaml:"bloom_filter_size"`
	BloomFilterHashes int           `json:"bloom_filter_hashes" yaml:"bloom_filter_hashes"`
	AuditEnabled      bool          `json:"audit_enabled" yaml:"audit_enabled"`
	AuditAsync        bool          `json:"audit_async" yaml:"audit_async"`
}

// DefaultPEPConfig returns a default PEP configuration.
func DefaultPEPConfig() *PEPConfig {
	return &PEPConfig{
		CacheEnabled:      true,
		CacheSize:         1000,
		CacheTTL:          5 * time.Minute,
		BloomFilterSize:   10000,
		BloomFilterHashes: 4,
		AuditEnabled:      true,
		AuditAsync:        true,
	}
}

// IAMClient defines the interface for IAM client operations.
type IAMClient interface {
	// Tenant operations
	CreateTenant(ctx context.Context, opts CreateTenantOptions) (*Tenant, error)
	GetTenant(ctx context.Context, tenantID string) (*Tenant, error)
	UpdateTenant(ctx context.Context, tenantID string, opts UpdateTenantOptions) (*Tenant, error)
	DeleteTenant(ctx context.Context, tenantID string) error
	ListTenants(ctx context.Context, opts ListTenantsOptions) ([]*Tenant, string, error)

	// User operations
	CreateUser(ctx context.Context, tenantID string, opts CreateUserOptions) (*User, error)
	GetUser(ctx context.Context, tenantID, userID string) (*User, error)
	UpdateUser(ctx context.Context, tenantID, userID string, opts UpdateUserOptions) (*User, error)
	DeleteUser(ctx context.Context, tenantID, userID string) error
	ListUsers(ctx context.Context, tenantID string, opts ListUsersOptions) ([]*User, string, error)

	// Group operations
	CreateGroup(ctx context.Context, tenantID string, opts CreateGroupOptions) (*Group, error)
	GetGroup(ctx context.Context, tenantID, groupID string) (*Group, error)
	UpdateGroup(ctx context.Context, tenantID, groupID string, opts UpdateGroupOptions) (*Group, error)
	DeleteGroup(ctx context.Context, tenantID, groupID string) error
	ListGroups(ctx context.Context, tenantID string, opts ListGroupsOptions) ([]*Group, string, error)

	// Role operations
	CreateRole(ctx context.Context, tenantID string, opts CreateRoleOptions) (*Role, error)
	GetRole(ctx context.Context, tenantID, roleID string) (*Role, error)
	UpdateRole(ctx context.Context, tenantID, roleID string, opts UpdateRoleOptions) (*Role, error)
	DeleteRole(ctx context.Context, tenantID, roleID string) error
	ListRoles(ctx context.Context, tenantID string, opts ListRolesOptions) ([]*Role, string, error)

	// Policy operations
	CreatePolicy(ctx context.Context, tenantID string, policy *Policy) error
	GetPolicy(ctx context.Context, tenantID, name string) (*Policy, error)
	UpdatePolicy(ctx context.Context, tenantID string, policy *Policy) error
	DeletePolicy(ctx context.Context, tenantID, name string) error
	ListPolicies(ctx context.Context, tenantID string, opts ListOptions) ([]*Policy, string, error)

	// Authorization
	Check(ctx context.Context, req *CheckRequest) (*CheckResponse, error)
}

// CompiledPolicy represents a compiled policy.
type CompiledPolicy struct {
	Name string `json:"name" yaml:"name"`
}

// AuditEventType classifies audit events.
type AuditEventType string

const (
	AuditEventAuthentication  AuditEventType = "authentication"
	AuditEventAuthorization   AuditEventType = "authorization"
	AuditEventUserManagement  AuditEventType = "user_management"
	AuditEventGroupManagement AuditEventType = "group_management"
	AuditEventRoleManagement  AuditEventType = "role_management"
	AuditEventPolicyChange    AuditEventType = "policy_change"
	AuditEventMFAEvent        AuditEventType = "mfa"
	AuditEventSessionEvent    AuditEventType = "session"
	AuditEventTenantEvent     AuditEventType = "tenant"
	AuditEventAppEvent        AuditEventType = "application"
)

// AuditCategory is the high-level category for an audit event.
type AuditCategory string

const (
	AuditCategoryAuthentication AuditCategory = "authentication"
	AuditCategoryAuthorization  AuditCategory = "authorization"
	AuditCategoryAdministration AuditCategory = "administration"
	AuditCategorySecurity       AuditCategory = "security"
)

// AuditOutcome represents the result of an audited action.
type AuditOutcome string

const (
	AuditOutcomeSuccess AuditOutcome = "success"
	AuditOutcomeFailure AuditOutcome = "failure"
	AuditOutcomeDenied  AuditOutcome = "denied"
)

// AuditResource represents a resource affected by an action.
type AuditResource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// AuditEvent represents an audit event.
type AuditEvent struct {
	ID        string                 `json:"id" yaml:"id"`
	Timestamp time.Time              `json:"timestamp" yaml:"timestamp"`
	TenantID  string                 `json:"tenant_id,omitempty"`
	EventType AuditEventType         `json:"event_type,omitempty"`
	Category  AuditCategory          `json:"category,omitempty"`
	Action    string                 `json:"action,omitempty"`
	Outcome   AuditOutcome           `json:"outcome,omitempty"`
	Resource  *AuditResource         `json:"resource,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// AuditActor represents the actor in an audit event.
type AuditActor struct {
	ID       string `json:"id" yaml:"id"`
	TenantID string `json:"tenant_id" yaml:"tenant_id"`
}

// Options types for various operations.

// CreateTenantOptions contains options for creating a tenant.
type CreateTenantOptions struct {
	ID   string `json:"id,omitempty" yaml:"id,omitempty"`
	Name string `json:"name" yaml:"name"`
}

// UpdateTenantOptions contains options for updating a tenant.
type UpdateTenantOptions struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
}

// ListTenantsOptions contains options for listing tenants.
type ListTenantsOptions struct {
	Limit  int    `json:"limit,omitempty" yaml:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty" yaml:"cursor,omitempty"`
}

// CreateUserOptions contains options for creating a user.
type CreateUserOptions struct {
	ID       string `json:"id,omitempty" yaml:"id,omitempty"`
	Email    string `json:"email" yaml:"email"`
	Username string `json:"username" yaml:"username"`
	Name     string `json:"name,omitempty" yaml:"name,omitempty"`
}

// UpdateUserOptions contains options for updating a user.
type UpdateUserOptions struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
}

// ListUsersOptions contains options for listing users.
type ListUsersOptions struct {
	Limit  int    `json:"limit,omitempty" yaml:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty" yaml:"cursor,omitempty"`
}

// CreateGroupOptions contains options for creating a group.
type CreateGroupOptions struct {
	ID          string `json:"id,omitempty" yaml:"id,omitempty"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// UpdateGroupOptions contains options for updating a group.
type UpdateGroupOptions struct {
	Name        string `json:"name,omitempty" yaml:"name,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// ListGroupsOptions contains options for listing groups.
type ListGroupsOptions struct {
	Limit  int    `json:"limit,omitempty" yaml:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty" yaml:"cursor,omitempty"`
}

// CreateRoleOptions contains options for creating a role.
type CreateRoleOptions struct {
	ID          string   `json:"id,omitempty" yaml:"id,omitempty"`
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Permissions []string `json:"permissions,omitempty" yaml:"permissions,omitempty"`
}

// UpdateRoleOptions contains options for updating a role.
type UpdateRoleOptions struct {
	Name        string   `json:"name,omitempty" yaml:"name,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Permissions []string `json:"permissions,omitempty" yaml:"permissions,omitempty"`
}

// ListRolesOptions contains options for listing roles.
type ListRolesOptions struct {
	Limit  int    `json:"limit,omitempty" yaml:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty" yaml:"cursor,omitempty"`
}

// RegisterAppOptions contains options for registering an app.
type RegisterAppOptions struct {
	Name         string   `json:"name" yaml:"name"`
	RedirectURIs []string `json:"redirect_uris,omitempty" yaml:"redirect_uris,omitempty"`
}

// UpdateAppOptions contains options for updating an app.
type UpdateAppOptions struct {
	Name         string   `json:"name,omitempty" yaml:"name,omitempty"`
	RedirectURIs []string `json:"redirect_uris,omitempty" yaml:"redirect_uris,omitempty"`
}

// ListAppsOptions contains options for listing apps.
type ListAppsOptions struct {
	Limit  int    `json:"limit,omitempty" yaml:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty" yaml:"cursor,omitempty"`
}

// IdentityStoreOptions contains options for creating an identity store.
type IdentityStoreOptions struct {
	UserStorage  UserStorage
	GroupStorage GroupStorage
	RoleStorage  RoleStorage
	Tenants      TenantManager
}

// PolicyEngineOptions contains options for creating a policy engine.
type PolicyEngineOptions struct {
	Storage PolicyStorage
	Cache   *PolicyCache
}

// Interfaces - minimal stubs for compilation.

// TenantManager defines tenant management operations.
type TenantManager interface {
	CreateTenant(ctx context.Context, opts CreateTenantOptions) (*Tenant, error)
	GetTenant(ctx context.Context, tenantID string) (*Tenant, error)
	UpdateTenant(ctx context.Context, tenantID string, opts UpdateTenantOptions) (*Tenant, error)
	DeleteTenant(ctx context.Context, tenantID string) error
	ListTenants(ctx context.Context, opts ListTenantsOptions) ([]*Tenant, string, error)
	CheckQuota(ctx context.Context, tenantID string, resource string) error
}

// UserStorage defines user storage operations.
type UserStorage interface {
	CreateUser(ctx context.Context, user *User) error
	GetUser(ctx context.Context, tenantID, userID string) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	UpdateUser(ctx context.Context, user *User) error
	DeleteUser(ctx context.Context, tenantID, userID string) error
	ListUsers(ctx context.Context, tenantID string, opts ListOptions) ([]*User, error)
}

// GroupStorage defines group storage operations.
type GroupStorage interface {
	CreateGroup(ctx context.Context, group *Group) error
	GetGroup(ctx context.Context, tenantID, groupID string) (*Group, error)
	UpdateGroup(ctx context.Context, group *Group) error
	DeleteGroup(ctx context.Context, tenantID, groupID string) error
	ListGroups(ctx context.Context, tenantID string, opts ListOptions) ([]*Group, error)
}

// RoleStorage defines role storage operations.
type RoleStorage interface {
	CreateRole(ctx context.Context, role *Role) error
	GetRole(ctx context.Context, tenantID, roleID string) (*Role, error)
	UpdateRole(ctx context.Context, role *Role) error
	DeleteRole(ctx context.Context, tenantID, roleID string) error
	ListRoles(ctx context.Context, tenantID string, opts ListOptions) ([]*Role, error)
}

// PolicyStorage defines policy storage operations.
type PolicyStorage interface {
	CreatePolicy(ctx context.Context, tenantID string, policy *Policy) error
	GetPolicy(ctx context.Context, tenantID, name string) (*Policy, error)
	UpdatePolicy(ctx context.Context, tenantID string, policy *Policy) error
	DeletePolicy(ctx context.Context, tenantID, name string) error
	ListPolicies(ctx context.Context, tenantID string, opts ListOptions) ([]*Policy, error)
}

// SessionStorage defines session storage operations.
type SessionStorage interface {
	CreateSession(ctx context.Context, session *SessionExt) error
	GetSession(ctx context.Context, sessionID string) (*SessionExt, error)
	DeleteSession(ctx context.Context, sessionID string) error
}

// AuditLogger defines audit logging operations.
type AuditLogger interface {
	Log(ctx context.Context, event *AuditEvent) error
}

// SessionManager defines session management operations.
type SessionManager interface {
	List(ctx context.Context, userID, tenantID string) ([]*SessionExt, error)
	Get(ctx context.Context, sessionID string) (*SessionExt, error)
	Revoke(ctx context.Context, sessionID string) error
	RevokeAllForUser(ctx context.Context, userID, tenantID string) error
	RevokeAllExcept(ctx context.Context, userID, tenantID, exceptSessionID string) error
}

// Stub structs - minimal implementations that return ErrIAMNotCompiled.

// IdentityStore stub for identity management.
type IdentityStore struct{}

// PolicyEngine stub for policy management.
// When storage is provided, it delegates to the storage.
// Otherwise, it returns ErrIAMNotCompiled.
type PolicyEngine struct {
	storage PolicyStorage
}

// CreatePolicy delegates to storage if available, otherwise returns error.
func (p *PolicyEngine) CreatePolicy(ctx context.Context, tenantID string, policy *Policy) error {
	if p.storage != nil {
		return p.storage.CreatePolicy(ctx, tenantID, policy)
	}
	return ErrIAMNotCompiled
}

// Check returns default allow if storage is configured, otherwise error.
func (p *PolicyEngine) Check(ctx context.Context, req *CheckRequest) (*CheckResponse, error) {
	if p.storage != nil {
		// When storage is available, return default allow for basic testing
		return &CheckResponse{Allowed: true, Reason: "stub: default allow"}, nil
	}
	return nil, ErrIAMNotCompiled
}

// GetPolicy delegates to storage if available, otherwise returns error.
func (p *PolicyEngine) GetPolicy(ctx context.Context, tenantID, name string) (*Policy, error) {
	if p.storage != nil {
		return p.storage.GetPolicy(ctx, tenantID, name)
	}
	return nil, ErrIAMNotCompiled
}

// UpdatePolicy delegates to storage if available, otherwise returns error.
func (p *PolicyEngine) UpdatePolicy(ctx context.Context, tenantID string, policy *Policy) error {
	if p.storage != nil {
		return p.storage.UpdatePolicy(ctx, tenantID, policy)
	}
	return ErrIAMNotCompiled
}

// DeletePolicy delegates to storage if available, otherwise returns error.
func (p *PolicyEngine) DeletePolicy(ctx context.Context, tenantID, name string) error {
	if p.storage != nil {
		return p.storage.DeletePolicy(ctx, tenantID, name)
	}
	return ErrIAMNotCompiled
}

// ListPolicies delegates to storage if available, otherwise returns error.
func (p *PolicyEngine) ListPolicies(ctx context.Context, tenantID string, opts ListOptions) ([]*Policy, error) {
	if p.storage != nil {
		return p.storage.ListPolicies(ctx, tenantID, opts)
	}
	return nil, ErrIAMNotCompiled
}

// Evaluate returns default allow if storage is configured, otherwise error.
func (p *PolicyEngine) Evaluate(ctx context.Context, req *CheckRequest) (*CheckResponse, error) {
	if p.storage != nil {
		// When storage is available, return default allow for basic testing
		return &CheckResponse{Allowed: true, Reason: "stub: default allow"}, nil
	}
	return nil, ErrIAMNotCompiled
}

// PolicyCache stub for policy caching.
type PolicyCache struct{}

// PolicyCompiler stub for policy compilation.
type PolicyCompiler struct{}

// PolicyEvaluator stub for policy evaluation.
type PolicyEvaluator struct{}

// AppManager stub for application management.
type AppManager struct{}

// OIDCProvider stub for OIDC operations.
type OIDCProvider struct{}

// DefaultMFAManager stub for MFA operations.
type DefaultMFAManager struct{}

// AdminManager stub for admin management.
type AdminManager struct{}

// GDPRManager stub for GDPR compliance.
type GDPRManager struct{}

// BackendStorage stub for IAM storage backend.
type BackendStorage struct{}

// DefaultTenantManager stub for tenant management.
type DefaultTenantManager struct{}

// MemoryStorage stub for in-memory storage.
type MemoryStorage struct{}

// ResourceTrie stub for resource matching.
type ResourceTrie struct{}

// DefaultSessionManager stub for session management.
type DefaultSessionManager struct{}

// Memory storage stubs.

// MemoryMFAStorage stub for MFA storage.
type MemoryMFAStorage struct{}

// MemorySessionStorage stub for session storage.
type MemorySessionStorage struct{}

// MemorySessionExtStorage stub for extended session storage.
type MemorySessionExtStorage struct{}

// MemoryAdminStorage stub for admin storage.
type MemoryAdminStorage struct{}

// MemoryImpersonationStorage stub for impersonation storage.
type MemoryImpersonationStorage struct{}

// MemoryConsentStorage stub for consent storage.
type MemoryConsentStorage struct{}

// MemoryDataExportStorage stub for data export storage.
type MemoryDataExportStorage struct{}

// MemoryDeletionStorage stub for deletion storage.
type MemoryDeletionStorage struct{}

// Constructor functions - all return ErrIAMNotCompiled.

// NewIdentityStore returns an error indicating IAM is not compiled.
func NewIdentityStore(_ IdentityStoreOptions) *IdentityStore {
	return nil
}

// NewPolicyEngine returns a PolicyEngine.
// If storage is provided in options, it delegates to the storage.
// Otherwise, all methods return ErrIAMNotCompiled.
func NewPolicyEngine(opts PolicyEngineOptions) *PolicyEngine {
	return &PolicyEngine{storage: opts.Storage}
}

// NewPolicyCache returns an error indicating IAM is not compiled.
func NewPolicyCache(_ *CacheConfig) *PolicyCache {
	return nil
}

// NewPolicyCompiler returns an error indicating IAM is not compiled.
func NewPolicyCompiler() *PolicyCompiler {
	return nil
}

// NewPolicyEvaluator returns an error indicating IAM is not compiled.
func NewPolicyEvaluator(_ *PolicyCompiler, _ *PolicyCache) *PolicyEvaluator {
	return nil
}

// NewAppManager returns an error indicating IAM is not compiled.
func NewAppManager(_ interface{}, _ TenantManager) *AppManager {
	return nil
}

// NewAdminManager returns an error indicating IAM is not compiled.
func NewAdminManager(_ AdminManagerOptions) (*AdminManager, error) {
	return nil, ErrIAMNotCompiled
}

// AdminManagerOptions contains options for creating an admin manager.
type AdminManagerOptions struct {
	AdminStorage         interface{}
	ImpersonationStorage interface{}
	AuditLogger          AuditLogger
}

// NewAuditLogger returns an error indicating IAM is not compiled.
func NewAuditLogger(_ ...interface{}) (*DefaultAuditLogger, error) {
	return nil, ErrIAMNotCompiled
}

// DefaultAuditLogger stub for audit logging.
type DefaultAuditLogger struct{}

// NewGDPRManager returns an error indicating IAM is not compiled.
func NewGDPRManager(_ GDPRManagerDependencies) (*GDPRManager, error) {
	return nil, ErrIAMNotCompiled
}

// GDPRManagerDependencies contains dependencies for GDPR manager.
type GDPRManagerDependencies struct {
	ConsentStorage    interface{}
	DataExportStorage interface{}
	DeletionStorage   interface{}
}

// NewBackendStorage returns an error indicating IAM is not compiled.
func NewBackendStorage(_ interfaces.KVStore) *BackendStorage {
	return nil
}

// NewDefaultTenantManager returns an error indicating IAM is not compiled.
func NewDefaultTenantManager(_ interface{}) *DefaultTenantManager {
	return nil
}

// NewMemoryStorage returns an error indicating IAM is not compiled.
func NewMemoryStorage() *MemoryStorage {
	return nil
}

// MemoryStorage PolicyStorage interface implementation stubs.

// CreatePolicy stub - returns error since IAM is not compiled.
func (m *MemoryStorage) CreatePolicy(ctx context.Context, tenantID string, policy *Policy) error {
	return ErrIAMNotCompiled
}

// GetPolicy stub - returns error since IAM is not compiled.
func (m *MemoryStorage) GetPolicy(ctx context.Context, tenantID, name string) (*Policy, error) {
	return nil, ErrIAMNotCompiled
}

// UpdatePolicy stub - returns error since IAM is not compiled.
func (m *MemoryStorage) UpdatePolicy(ctx context.Context, tenantID string, policy *Policy) error {
	return ErrIAMNotCompiled
}

// DeletePolicy stub - returns error since IAM is not compiled.
func (m *MemoryStorage) DeletePolicy(ctx context.Context, tenantID, name string) error {
	return ErrIAMNotCompiled
}

// ListPolicies stub - returns error since IAM is not compiled.
func (m *MemoryStorage) ListPolicies(ctx context.Context, tenantID string, opts ListOptions) ([]*Policy, error) {
	return nil, ErrIAMNotCompiled
}

// NewResourceTrie returns an error indicating IAM is not compiled.
func NewResourceTrie() *ResourceTrie {
	return nil
}

// NewSessionManager returns an error indicating IAM is not compiled.
func NewSessionManager(_ ...SessionManagerOption) (*DefaultSessionManager, error) {
	return nil, ErrIAMNotCompiled
}

// SessionManagerOption is a functional option for session manager.
type SessionManagerOption func(*DefaultSessionManager)

// WithSessionStorage returns a session manager option.
func WithSessionStorage(_ interface{}) SessionManagerOption {
	return func(_ *DefaultSessionManager) {}
}

// NewMFAManager returns an error indicating IAM is not compiled.
func NewMFAManager(_ ...interface{}) (*DefaultMFAManager, error) {
	return nil, ErrIAMNotCompiled
}

// NewOIDCProvider returns an error indicating IAM is not compiled.
func NewOIDCProvider(_ *OIDCConfig, _ OIDCDependencies) (*OIDCProvider, error) {
	return nil, ErrIAMNotCompiled
}

// OIDCConfig contains OIDC configuration.
type OIDCConfig struct {
	Issuer string
}

// OIDCDependencies contains OIDC dependencies.
type OIDCDependencies struct{}

// Memory storage constructors.

// NewMemoryMFAStorage returns an error indicating IAM is not compiled.
func NewMemoryMFAStorage() *MemoryMFAStorage {
	return nil
}

// NewMemorySessionStorage returns an error indicating IAM is not compiled.
func NewMemorySessionStorage() *MemorySessionStorage {
	return nil
}

// NewMemorySessionExtStorage returns an error indicating IAM is not compiled.
func NewMemorySessionExtStorage() *MemorySessionExtStorage {
	return nil
}

// NewMemoryAdminStorage returns an error indicating IAM is not compiled.
func NewMemoryAdminStorage() *MemoryAdminStorage {
	return nil
}

// NewMemoryImpersonationStorage returns an error indicating IAM is not compiled.
func NewMemoryImpersonationStorage() *MemoryImpersonationStorage {
	return nil
}

// NewMemoryConsentStorage returns an error indicating IAM is not compiled.
func NewMemoryConsentStorage() *MemoryConsentStorage {
	return nil
}

// NewMemoryDataExportStorage returns an error indicating IAM is not compiled.
func NewMemoryDataExportStorage() *MemoryDataExportStorage {
	return nil
}

// NewMemoryDeletionStorage returns an error indicating IAM is not compiled.
func NewMemoryDeletionStorage() *MemoryDeletionStorage {
	return nil
}

// Context functions.
// These functions work even without the iam build tag to support basic
// tenant context propagation for multi-tenant features like secrets management.

// ContextWithTenant returns a new context with the tenant ID set.
func ContextWithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tenantID)
}

// ContextWithUser returns a new context with the user ID set.
func ContextWithUser(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userContextKey{}, userID)
}

// ContextWithParameterContext returns a new context with the parameter context set.
func ContextWithParameterContext(ctx context.Context, pc *ParameterContext) context.Context {
	return context.WithValue(ctx, parameterContextKey{}, pc)
}

// TenantFromContext extracts the tenant ID from context.
// Returns empty string if not set.
func TenantFromContext(ctx context.Context) string {
	if v := ctx.Value(tenantContextKey{}); v != nil {
		if tenantID, ok := v.(string); ok {
			return tenantID
		}
	}
	return ""
}

// UserFromContext extracts the user ID from context.
// Returns empty string if not set.
func UserFromContext(ctx context.Context) string {
	if v := ctx.Value(userContextKey{}); v != nil {
		if userID, ok := v.(string); ok {
			return userID
		}
	}
	return ""
}

// Error types and functions.

// IAMError represents an IAM-specific error.
type IAMError struct {
	Code    int
	Op      string
	Message string
}

// Error returns the error message.
func (e *IAMError) Error() string {
	return e.Message
}

// Error codes.
const (
	ErrWebAuthnRegistrationFailed   = 1
	ErrWebAuthnAuthenticationFailed = 2
)

// NewIAMError creates a new IAM error.
func NewIAMError(code int, op, message string) *IAMError {
	return &IAMError{Code: code, Op: op, Message: message}
}

// Consent constants.
const (
	ConsentPurposeAnalytics       = "analytics"
	ConsentPurposeDataProcessing  = "data_processing"
	ConsentPurposeDataRetention   = "data_retention"
	ConsentPurposeFunctional      = "functional"
	ConsentPurposeMarketing       = "marketing"
	ConsentPurposePersonalization = "personalization"
	ConsentPurposeThirdParty      = "third_party"
	ConsentStatusDenied           = "denied"
	ConsentStatusGranted          = "granted"
	ConsentStatusPending          = "pending"
)

// MFA constants.
const (
	MFAAlgorithmSHA1 = "SHA1"
)
