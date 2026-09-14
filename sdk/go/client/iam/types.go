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
	"encoding/json"
	"strconv"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// Flexible JSON Types
// =============================================================================

// FlexInt is an integer type that handles both string and int JSON values.
// Some transports (REST/MCP) may serialize integers as strings, so this type
// provides seamless deserialization from either format.
type FlexInt int

// MarshalJSON implements json.Marshaler for FlexInt.
func (f FlexInt) MarshalJSON() ([]byte, error) {
	return json.Marshal(int(f))
}

// UnmarshalJSON implements json.Unmarshaler for FlexInt.
// It handles both integer and string JSON values.
func (f *FlexInt) UnmarshalJSON(data []byte) error {
	// Try integer first
	var intVal int
	if err := json.Unmarshal(data, &intVal); err == nil {
		*f = FlexInt(intVal)
		return nil
	}

	// Try string
	var strVal string
	if err := json.Unmarshal(data, &strVal); err == nil {
		// Try parsing as integer
		if i, err := strconv.Atoi(strVal); err == nil {
			*f = FlexInt(i)
			return nil
		}
		// Try parsing as float (e.g., "1.0")
		if fv, err := strconv.ParseFloat(strVal, 64); err == nil {
			*f = FlexInt(int(fv))
			return nil
		}
		return &IntParseError{Value: strVal}
	}

	// Try float (JSON numbers may be float64)
	var floatVal float64
	if err := json.Unmarshal(data, &floatVal); err == nil {
		*f = FlexInt(int(floatVal))
		return nil
	}

	return &JSONUnmarshalTypeError{Target: "FlexInt", Data: string(data)}
}

// Int returns the underlying int value.
func (f FlexInt) Int() int {
	return int(f)
}

// =============================================================================
// Status Types
// =============================================================================

// TenantStatus represents the status of a tenant.
type TenantStatus int

const (
	// TenantStatusUnspecified is the default unspecified status.
	TenantStatusUnspecified TenantStatus = iota
	// TenantStatusActive indicates the tenant is active.
	TenantStatusActive
	// TenantStatusSuspended indicates the tenant is suspended.
	TenantStatusSuspended
	// TenantStatusDeleted indicates the tenant is marked for deletion.
	TenantStatusDeleted
)

// String returns the string representation of the tenant status.
func (s TenantStatus) String() string {
	switch s {
	case TenantStatusActive:
		return "ACTIVE"
	case TenantStatusSuspended:
		return "SUSPENDED"
	case TenantStatusDeleted:
		return "DELETED"
	default:
		return "UNSPECIFIED"
	}
}

// UserStatus represents the status of a user.
type UserStatus int

const (
	// UserStatusUnspecified is the default unspecified status.
	UserStatusUnspecified UserStatus = iota
	// UserStatusActive indicates the user is active.
	UserStatusActive
	// UserStatusSuspended indicates the user is suspended.
	UserStatusSuspended
	// UserStatusPendingVerification indicates the user needs email verification.
	UserStatusPendingVerification
	// UserStatusDeleted indicates the user is marked for deletion.
	UserStatusDeleted
)

// String returns the string representation of the user status.
func (s UserStatus) String() string {
	switch s {
	case UserStatusActive:
		return "ACTIVE"
	case UserStatusSuspended:
		return "SUSPENDED"
	case UserStatusPendingVerification:
		return "PENDING_VERIFICATION"
	case UserStatusDeleted:
		return "DELETED"
	default:
		return "UNSPECIFIED"
	}
}

// =============================================================================
// Core Entity Types
// =============================================================================

// Tenant represents a tenant in the system.
type Tenant struct {
	// ID is the unique identifier for the tenant.
	ID string `json:"id"`

	// Name is the unique name of the tenant.
	Name string `json:"name"`

	// DisplayName is the human-readable display name.
	DisplayName string `json:"display_name"`

	// Description is an optional description.
	Description string `json:"description,omitempty"`

	// Status is the current status of the tenant.
	Status TenantStatus `json:"status"`

	// Settings contains tenant-specific settings.
	Settings map[string]string `json:"settings,omitempty"`

	// Quotas defines resource limits for the tenant.
	Quotas *TenantQuotas `json:"quotas,omitempty"`

	// Metadata contains additional metadata.
	Metadata map[string]string `json:"metadata,omitempty"`

	// CreatedAt is when the tenant was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the tenant was last updated.
	UpdatedAt time.Time `json:"updated_at"`
}

// TenantSettings contains tenant-specific configuration.
type TenantSettings struct {
	// MFARequired requires MFA for all users in this tenant.
	MFARequired bool `json:"mfa_required"`

	// MFAMethods lists allowed MFA methods.
	MFAMethods []string `json:"mfa_methods,omitempty"`

	// SessionTimeout is the maximum session duration in seconds.
	SessionTimeout int `json:"session_timeout,omitempty"`

	// IdleTimeout is the session idle timeout in seconds.
	IdleTimeout int `json:"idle_timeout,omitempty"`

	// AllowedDomains restricts user email domains.
	AllowedDomains []string `json:"allowed_domains,omitempty"`

	// DefaultRoles are roles assigned to new users.
	DefaultRoles []string `json:"default_roles,omitempty"`

	// DefaultGroups are groups assigned to new users.
	DefaultGroups []string `json:"default_groups,omitempty"`

	// EnableAuditLogging enables audit logging for this tenant.
	EnableAuditLogging bool `json:"enable_audit_logging"`

	// AuditRetentionDays is how long to retain audit logs.
	AuditRetentionDays int `json:"audit_retention_days,omitempty"`
}

// TenantQuotas defines resource limits for a tenant.
type TenantQuotas struct {
	// MaxUsers is the maximum number of users.
	MaxUsers int `json:"max_users"`

	// MaxGroups is the maximum number of groups.
	MaxGroups int `json:"max_groups"`

	// MaxRoles is the maximum number of roles.
	MaxRoles int `json:"max_roles"`

	// MaxPolicies is the maximum number of policies.
	MaxPolicies int `json:"max_policies"`

	// MaxApplications is the maximum number of OAuth2 applications.
	MaxApplications int `json:"max_applications"`

	// MaxSessionsPerUser is the maximum concurrent sessions per user.
	MaxSessionsPerUser int `json:"max_sessions_per_user"`
}

// User represents a user in the system.
type User struct {
	// ID is the unique identifier for the user.
	ID string `json:"id"`

	// TenantID is the tenant this user belongs to.
	TenantID string `json:"tenant_id"`

	// Username is the unique username within the tenant.
	Username string `json:"username"`

	// Email is the user's email address.
	Email string `json:"email"`

	// DisplayName is the human-readable display name.
	DisplayName string `json:"display_name,omitempty"`

	// Status is the current status of the user.
	Status UserStatus `json:"status"`

	// Lifecycle is the canonical account state: pending, active, suspended,
	// locked, disabled or deleting. It is the value the store actually holds;
	// Status is this SDK's own coarser enum and is not populated by every
	// transport.
	Lifecycle string `json:"lifecycle,omitempty"`

	// AuthMethods lists the authentication methods this account is permitted to
	// present ("webauthn", "piv"). Empty states no restriction.
	AuthMethods []string `json:"auth_methods,omitempty"`

	// Roles are the role IDs assigned to the user.
	Roles []string `json:"roles,omitempty"`

	// Groups are the group IDs the user belongs to.
	Groups []string `json:"groups,omitempty"`

	// Attributes contains custom user attributes for ABAC.
	Attributes *UserAttributes `json:"attributes,omitempty"`

	// Metadata contains additional user metadata.
	Metadata map[string]string `json:"metadata,omitempty"`

	// EmailVerified indicates if the email is verified.
	EmailVerified bool `json:"email_verified"`

	// LastLoginAt is when the user last logged in.
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`

	// CreatedAt is when the user was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the user was last updated.
	UpdatedAt time.Time `json:"updated_at"`
}

// UserAttributes contains custom attributes for ABAC.
type UserAttributes struct {
	// Department is the user's department.
	Department string `json:"department,omitempty"`

	// TeamID is the user's team identifier.
	TeamID string `json:"team_id,omitempty"`

	// Location is the user's location.
	Location string `json:"location,omitempty"`

	// CostCenter is the user's cost center.
	CostCenter string `json:"cost_center,omitempty"`

	// EmployeeID is the user's employee ID.
	EmployeeID string `json:"employee_id,omitempty"`

	// Manager is the user's manager ID.
	Manager string `json:"manager,omitempty"`

	// Classification is the user's security classification.
	Classification string `json:"classification,omitempty"`

	// Custom contains arbitrary custom attributes.
	Custom map[string]string `json:"custom,omitempty"`
}

// Group represents a group of users.
type Group struct {
	// ID is the unique identifier for the group.
	ID string `json:"id"`

	// TenantID is the tenant this group belongs to.
	TenantID string `json:"tenant_id"`

	// Name is the unique name within the tenant.
	Name string `json:"name"`

	// DisplayName is the human-readable display name.
	DisplayName string `json:"display_name,omitempty"`

	// Description is an optional description.
	Description string `json:"description,omitempty"`

	// Roles are the role IDs assigned to the group.
	Roles []string `json:"roles,omitempty"`

	// MemberCount is the number of members in the group.
	MemberCount int `json:"member_count"`

	// Metadata contains additional group metadata.
	Metadata map[string]string `json:"metadata,omitempty"`

	// CreatedAt is when the group was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the group was last updated.
	UpdatedAt time.Time `json:"updated_at"`
}

// Role represents a role with permissions.
type Role struct {
	// ID is the unique identifier for the role.
	ID string `json:"id"`

	// TenantID is the tenant this role belongs to.
	TenantID string `json:"tenant_id"`

	// Name is the unique name within the tenant.
	Name string `json:"name"`

	// DisplayName is the human-readable display name.
	DisplayName string `json:"display_name,omitempty"`

	// Description is an optional description.
	Description string `json:"description,omitempty"`

	// Permissions are the permissions granted by this role.
	Permissions []string `json:"permissions"`

	// RetiredPermissions lists the members of Permissions the server's build
	// no longer defines. Derived on read from the live permission registry and
	// never stored, so it names the drift between what the tenant was seeded
	// with and what the server now grants — visible on an ordinary listing,
	// before Reconcile repairs it.
	RetiredPermissions []string `json:"retired_permissions,omitempty"`

	// IsSystem indicates if this is a system-defined role.
	IsSystem bool `json:"is_system"`

	// Metadata contains additional role metadata.
	Metadata map[string]string `json:"metadata,omitempty"`

	// CreatedAt is when the role was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the role was last updated.
	UpdatedAt time.Time `json:"updated_at"`
}

// GroupMember represents a member of a group.
type GroupMember struct {
	// UserID is the user's ID.
	UserID string `json:"user_id"`

	// Username is the user's username.
	Username string `json:"username"`

	// Email is the user's email.
	Email string `json:"email"`

	// DisplayName is the user's display name.
	DisplayName string `json:"display_name,omitempty"`

	// JoinedAt is when the user joined the group.
	JoinedAt time.Time `json:"joined_at"`
}

// =============================================================================
// Session Types
// =============================================================================

// Session represents a user session.
type Session struct {
	// ID is the unique session identifier.
	ID string `json:"id"`

	// UserID is the user this session belongs to.
	UserID string `json:"user_id"`

	// TenantID is the tenant this session is in.
	TenantID string `json:"tenant_id"`

	// CreatedAt is when the session was created.
	CreatedAt time.Time `json:"created_at"`

	// Created is an alias for CreatedAt for CLI compatibility.
	Created time.Time `json:"created,omitempty"`

	// LastActive is when the session was last used.
	LastActive time.Time `json:"last_active"`

	// ExpiresAt is when the session expires.
	ExpiresAt time.Time `json:"expires_at"`

	// MFAVerified indicates if MFA was completed for this session.
	MFAVerified bool `json:"mfa_verified"`

	// IPAddress is the IP address that created the session.
	IPAddress string `json:"ip_address"`

	// UserAgent is the user agent string.
	UserAgent string `json:"user_agent"`

	// DeviceInfo contains device identification info.
	DeviceInfo string `json:"device_info,omitempty"`

	// AuthMethod is how the user authenticated.
	AuthMethod string `json:"auth_method"`
}

// =============================================================================
// Audit Types (see audit.go for comprehensive definitions)
// =============================================================================

// AuditCategory represents the category of an audit event.
type AuditCategory int

const (
	// AuditCategoryAuthentication covers login, logout, token events.
	AuditCategoryAuthentication AuditCategory = iota
	// AuditCategoryAuthorization covers access check events.
	AuditCategoryAuthorization
	// AuditCategoryManagement covers resource management events.
	AuditCategoryManagement
	// AuditCategoryDataAccess covers data access events.
	AuditCategoryDataAccess
	// AuditCategorySecurity covers security-related events.
	AuditCategorySecurity
)

// String returns the string representation of the audit category.
func (c AuditCategory) String() string {
	switch c {
	case AuditCategoryAuthentication:
		return "authentication"
	case AuditCategoryAuthorization:
		return "authorization"
	case AuditCategoryManagement:
		return "management"
	case AuditCategoryDataAccess:
		return "data_access"
	case AuditCategorySecurity:
		return "security"
	default:
		return "unknown"
	}
}

// =============================================================================
// Admin Types (see admin.go for comprehensive definitions)
// =============================================================================

// AdminRole represents a delegated administration role.
type AdminRole int

const (
	// AdminRoleSuperAdmin has full access to all tenants and system settings.
	AdminRoleSuperAdmin AdminRole = iota
	// AdminRoleTenantAdmin has full access to a specific tenant.
	AdminRoleTenantAdmin
	// AdminRoleUserAdmin can manage users within a tenant.
	AdminRoleUserAdmin
	// AdminRoleGroupAdmin can manage groups within a tenant.
	AdminRoleGroupAdmin
	// AdminRolePolicyAdmin can manage policies within a tenant.
	AdminRolePolicyAdmin
	// AdminRoleAppAdmin can manage applications within a tenant.
	AdminRoleAppAdmin
	// AdminRoleAuditViewer can view audit logs.
	AdminRoleAuditViewer
	// AdminRoleReadOnly has read-only access.
	AdminRoleReadOnly
)

// String returns the string representation of the admin role.
func (r AdminRole) String() string {
	switch r {
	case AdminRoleSuperAdmin:
		return "super_admin"
	case AdminRoleTenantAdmin:
		return "tenant_admin"
	case AdminRoleUserAdmin:
		return "user_admin"
	case AdminRoleGroupAdmin:
		return "group_admin"
	case AdminRolePolicyAdmin:
		return "policy_admin"
	case AdminRoleAppAdmin:
		return "app_admin"
	case AdminRoleAuditViewer:
		return "audit_viewer"
	case AdminRoleReadOnly:
		return "read_only"
	default:
		return "unknown"
	}
}

// =============================================================================
// Pagination Types
// =============================================================================

// PaginationInfo contains pagination metadata.
type PaginationInfo struct {
	// TotalCount is the total number of items.
	TotalCount int `json:"total_count"`

	// PageSize is the number of items per page.
	PageSize int `json:"page_size"`

	// NextPageToken is the token for the next page.
	NextPageToken string `json:"next_page_token,omitempty"`

	// HasMore indicates if there are more pages.
	HasMore bool `json:"has_more"`
}

// =============================================================================
// Response Conversion Utilities
// =============================================================================

// convertMapResponse converts a map[string]interface{} response (from REST/MCP
// transports) to a typed struct using JSON round-trip serialization. This handles
// the common case where transport layers return generic maps but SDK clients
// expect strongly-typed structs.
func convertMapResponse[T any](resp interface{}, context string) (*T, error) {
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, context, "failed to marshal map response")
	}
	result := new(T)
	if err := json.Unmarshal(data, result); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, context, "failed to unmarshal map response")
	}
	return result, nil
}
