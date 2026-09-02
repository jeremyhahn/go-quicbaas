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
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// assignmentMatchesTenant reports whether an assignment applies to the
// given tenant filter under the #357 contract, stated once for every
// consumer (CheckPermission and both ListAssignments storages): an empty
// filter matches every assignment; explicit TenantIDs must name the
// tenant; empty TenantIDs is system-tenant scope alone, so it matches
// only a system-tenant filter — never another tenant's.
func assignmentMatchesTenant(assignment *AdminAssignment, tenantID string) bool {
	if tenantID == "" {
		return true
	}
	if len(assignment.TenantIDs) > 0 {
		for _, tid := range assignment.TenantIDs {
			if tid == tenantID {
				return true
			}
		}
		return false
	}
	return qrdbsdk.IsSystemTenant(tenantID)
}

// AdminRole represents a delegated administration role type.
type AdminRole string

const (
	// AdminRoleSuperAdmin holds the full administrative permission set.
	// The role grants permissions only — tenant REACH is decided by the
	// assignment's TenantIDs (#357): a super_admin acts in the tenants its
	// assignment names, or system-tenant scope alone when TenantIDs is
	// empty. No role reaches every tenant implicitly.
	AdminRoleSuperAdmin AdminRole = "super_admin"

	// AdminRoleTenantAdmin has full access within their assigned tenant(s).
	AdminRoleTenantAdmin AdminRole = "tenant_admin"

	// AdminRoleUserAdmin can manage users within their assigned tenant(s).
	AdminRoleUserAdmin AdminRole = "user_admin"

	// AdminRoleGroupAdmin can manage groups within their assigned tenant(s).
	AdminRoleGroupAdmin AdminRole = "group_admin"

	// AdminRolePolicyAdmin can manage policies within their assigned tenant(s).
	AdminRolePolicyAdmin AdminRole = "policy_admin"

	// AdminRoleAuditAdmin can view audit logs within their assigned tenant(s).
	AdminRoleAuditAdmin AdminRole = "audit_admin"

	// AdminRoleReadOnly has read-only access to assigned resources.
	AdminRoleReadOnly AdminRole = "read_only"

	// AdminRoleAgentAdmin has full access to all agent orchestration operations.
	AdminRoleAgentAdmin AdminRole = "agent_admin"

	// AdminRoleAgentOperator can manage agent sessions and projects but not administer backends.
	AdminRoleAgentOperator AdminRole = "agent_operator"

	// AdminRoleAgentViewer has read-only access to agent discovery and cache.
	AdminRoleAgentViewer AdminRole = "agent_viewer"
)

// AdminPermission represents a specific permission that can be granted.
type AdminPermission string

const (
	// Tenant permissions.
	PermTenantCreate AdminPermission = "tenant:create"
	PermTenantRead   AdminPermission = "tenant:read"
	PermTenantUpdate AdminPermission = "tenant:update"
	PermTenantDelete AdminPermission = "tenant:delete"

	// User permissions.
	PermUserCreate      AdminPermission = "user:create"
	PermUserRead        AdminPermission = "user:read"
	PermUserUpdate      AdminPermission = "user:update"
	PermUserDelete      AdminPermission = "user:delete"
	PermUserImpersonate AdminPermission = "user:impersonate"
	PermUserResetMFA    AdminPermission = "user:reset_mfa"
	PermUserSuspend     AdminPermission = "user:suspend"
	PermUserUnlock      AdminPermission = "user:unlock"

	// Group permissions.
	PermGroupCreate AdminPermission = "group:create"
	PermGroupRead   AdminPermission = "group:read"
	PermGroupUpdate AdminPermission = "group:update"
	PermGroupDelete AdminPermission = "group:delete"

	// Role permissions.
	PermRoleCreate AdminPermission = "role:create"
	PermRoleRead   AdminPermission = "role:read"
	PermRoleUpdate AdminPermission = "role:update"
	PermRoleDelete AdminPermission = "role:delete"
	PermRoleAssign AdminPermission = "role:assign"

	// Policy permissions.
	PermPolicyCreate   AdminPermission = "policy:create"
	PermPolicyRead     AdminPermission = "policy:read"
	PermPolicyUpdate   AdminPermission = "policy:update"
	PermPolicyDelete   AdminPermission = "policy:delete"
	PermPolicySimulate AdminPermission = "policy:simulate"

	// Audit permissions.
	PermAuditRead   AdminPermission = "audit:read"
	PermAuditExport AdminPermission = "audit:export"
	PermAuditDelete AdminPermission = "audit:delete"

	// Session permissions.
	PermSessionRead   AdminPermission = "session:read"
	PermSessionRevoke AdminPermission = "session:revoke"

	// GDPR permissions.
	PermGDPRExport    AdminPermission = "gdpr:export"
	PermGDPRDelete    AdminPermission = "gdpr:delete"
	PermConsentManage AdminPermission = "consent:manage"

	// Agent Orchestration permissions.
	PermAgentDiscover   AdminPermission = "agent:discover"
	PermAgentRegister   AdminPermission = "agent:register"
	PermAgentSpawn      AdminPermission = "agent:spawn"
	PermAgentPrompt     AdminPermission = "agent:prompt"
	PermAgentClose      AdminPermission = "agent:close"
	PermAgentDecompose  AdminPermission = "agent:decompose"
	PermAgentCacheRead  AdminPermission = "agent:cache:read"
	PermAgentCacheWrite AdminPermission = "agent:cache:write"
	PermAgentAdmin      AdminPermission = "agent:admin"
)

// AdminAssignment represents a user's administrative assignment.
type AdminAssignment struct {
	// ID is the unique identifier for this assignment.
	ID string `json:"id" yaml:"id"`

	// UserID is the user being granted administrative privileges.
	UserID string `json:"user_id" yaml:"user_id"`

	// AdminRole is the type of administrative role.
	AdminRole AdminRole `json:"admin_role" yaml:"admin_role"`

	// TenantIDs are the tenants this assignment applies to, named
	// explicitly. Empty means the assignment is scoped to the SYSTEM
	// tenant alone (the firstboot super_admin's shape): it matches an
	// unscoped permission check or a system-tenant target, and never any
	// other tenant. Empty must NOT be read as "every tenant" — that
	// reading let the platform operator pass tenant-scoped checks,
	// impersonation included, for any tenant on the platform (#357).
	// There is no all-tenants sentinel; reach into a tenant requires
	// naming that tenant here.
	TenantIDs []string `json:"tenant_ids,omitempty" yaml:"tenant_ids,omitempty"`

	// Permissions are explicit permissions granted (overrides role defaults).
	Permissions []AdminPermission `json:"permissions,omitempty" yaml:"permissions,omitempty"`

	// DeniedPermissions are explicitly denied even if role allows.
	DeniedPermissions []AdminPermission `json:"denied_permissions,omitempty" yaml:"denied_permissions,omitempty"`

	// ResourceScopes limit access to specific resources within tenants.
	ResourceScopes []ResourceScope `json:"resource_scopes,omitempty" yaml:"resource_scopes,omitempty"`

	// ExpiresAt is when this assignment expires (nil = never).
	ExpiresAt *time.Time `json:"expires_at,omitempty" yaml:"expires_at,omitempty"`

	// GrantedBy is the user who created this assignment.
	GrantedBy string `json:"granted_by" yaml:"granted_by"`

	// GrantedAt is when this assignment was created.
	GrantedAt time.Time `json:"granted_at" yaml:"granted_at"`

	// LastUsedAt is when this assignment was last used.
	LastUsedAt *time.Time `json:"last_used_at,omitempty" yaml:"last_used_at,omitempty"`

	// Active indicates if the assignment is currently active.
	Active bool `json:"active" yaml:"active"`

	// Metadata contains additional context.
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// ResourceScope defines a scope limitation for admin access.
type ResourceScope struct {
	// ResourceType is the type of resource (user, group, role, policy).
	ResourceType string `json:"resource_type" yaml:"resource_type"`

	// ResourcePattern is a pattern matching specific resources.
	// Supports wildcards: "user:*", "group:engineering-*"
	ResourcePattern string `json:"resource_pattern" yaml:"resource_pattern"`
}

// AdminCheckRequest represents a request to check admin permissions.
type AdminCheckRequest struct {
	// UserID is the admin user making the request.
	UserID string `json:"user_id" yaml:"user_id"`

	// Permission is the permission being checked.
	Permission AdminPermission `json:"permission" yaml:"permission"`

	// TenantID is the tenant context for the check.
	TenantID string `json:"tenant_id,omitempty" yaml:"tenant_id,omitempty"`

	// ResourceType is the type of resource being accessed.
	ResourceType string `json:"resource_type,omitempty" yaml:"resource_type,omitempty"`

	// ResourceID is the specific resource being accessed.
	ResourceID string `json:"resource_id,omitempty" yaml:"resource_id,omitempty"`
}

// AdminCheckResponse represents the result of an admin permission check.
type AdminCheckResponse struct {
	// Allowed indicates if the action is permitted.
	Allowed bool `json:"allowed" yaml:"allowed"`

	// AssignmentID is the assignment that granted access (if allowed).
	AssignmentID string `json:"assignment_id,omitempty" yaml:"assignment_id,omitempty"`

	// AdminRole is the role that granted access.
	AdminRole AdminRole `json:"admin_role,omitempty" yaml:"admin_role,omitempty"`

	// Reason explains the decision.
	Reason string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

// ImpersonationSession represents an active impersonation session.
type ImpersonationSession struct {
	// ID is the unique session identifier.
	ID string `json:"id" yaml:"id"`

	// AdminUserID is the admin performing impersonation.
	AdminUserID string `json:"admin_user_id" yaml:"admin_user_id"`

	// AdminUsername is the admin's username for audit trails.
	AdminUsername string `json:"admin_username,omitempty" yaml:"admin_username,omitempty"`

	// TargetUserID is the user being impersonated.
	TargetUserID string `json:"target_user_id" yaml:"target_user_id"`

	// TargetUsername is the target user's username for audit trails.
	TargetUsername string `json:"target_username,omitempty" yaml:"target_username,omitempty"`

	// TenantID is the tenant context.
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Reason is the justification for impersonation.
	Reason string `json:"reason" yaml:"reason"`

	// StartedAt is when impersonation began.
	StartedAt time.Time `json:"started_at" yaml:"started_at"`

	// ExpiresAt is when impersonation expires.
	ExpiresAt time.Time `json:"expires_at" yaml:"expires_at"`

	// EndedAt is when impersonation was terminated (nil if active).
	EndedAt *time.Time `json:"ended_at,omitempty" yaml:"ended_at,omitempty"`

	// OperationCount tracks the number of operations performed during this session.
	OperationCount int64 `json:"operation_count" yaml:"operation_count"`

	// AuditLogID links to the audit log entry.
	AuditLogID string `json:"audit_log_id" yaml:"audit_log_id"`
}

// CreateAdminAssignmentOptions contains options for creating an admin assignment.
type CreateAdminAssignmentOptions struct {
	UserID            string            `json:"user_id"`
	AdminRole         AdminRole         `json:"admin_role"`
	TenantIDs         []string          `json:"tenant_ids,omitempty"`
	Permissions       []AdminPermission `json:"permissions,omitempty"`
	DeniedPermissions []AdminPermission `json:"denied_permissions,omitempty"`
	ResourceScopes    []ResourceScope   `json:"resource_scopes,omitempty"`
	ExpiresAt         *time.Time        `json:"expires_at,omitempty"`
	GrantedBy         string            `json:"granted_by"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

// UpdateAdminAssignmentOptions contains options for updating an admin assignment.
type UpdateAdminAssignmentOptions struct {
	AdminRole         *AdminRole        `json:"admin_role,omitempty"`
	TenantIDs         []string          `json:"tenant_ids,omitempty"`
	Permissions       []AdminPermission `json:"permissions,omitempty"`
	DeniedPermissions []AdminPermission `json:"denied_permissions,omitempty"`
	ResourceScopes    []ResourceScope   `json:"resource_scopes,omitempty"`
	ExpiresAt         *time.Time        `json:"expires_at,omitempty"`
	Active            *bool             `json:"active,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

// ListAdminAssignmentsOptions contains options for listing admin assignments.
type ListAdminAssignmentsOptions struct {
	UserID     string    `json:"user_id,omitempty"`
	TenantID   string    `json:"tenant_id,omitempty"`
	AdminRole  AdminRole `json:"admin_role,omitempty"`
	ActiveOnly bool      `json:"active_only,omitempty"`
	Limit      int       `json:"limit,omitempty"`
	Offset     int       `json:"offset,omitempty"`
}

// StartImpersonationOptions contains options for starting impersonation.
type StartImpersonationOptions struct {
	AdminUserID    string        `json:"admin_user_id"`
	AdminUsername  string        `json:"admin_username,omitempty"`
	TargetUserID   string        `json:"target_user_id"`
	TargetUsername string        `json:"target_username,omitempty"`
	TenantID       string        `json:"tenant_id"`
	Reason         string        `json:"reason"`
	Duration       time.Duration `json:"duration,omitempty"`
}

// GetDefaultPermissions returns the default permissions for an admin role.
func GetDefaultPermissions(role AdminRole) []AdminPermission {
	switch role {
	case AdminRoleSuperAdmin:
		return []AdminPermission{
			PermTenantCreate, PermTenantRead, PermTenantUpdate, PermTenantDelete,
			PermUserCreate, PermUserRead, PermUserUpdate, PermUserDelete, PermUserImpersonate, PermUserResetMFA, PermUserSuspend, PermUserUnlock,
			PermGroupCreate, PermGroupRead, PermGroupUpdate, PermGroupDelete,
			PermRoleCreate, PermRoleRead, PermRoleUpdate, PermRoleDelete, PermRoleAssign,
			PermPolicyCreate, PermPolicyRead, PermPolicyUpdate, PermPolicyDelete, PermPolicySimulate,
			PermAuditRead, PermAuditExport, PermAuditDelete,
			PermSessionRead, PermSessionRevoke,
			PermGDPRExport, PermGDPRDelete, PermConsentManage,
			PermAgentDiscover, PermAgentRegister, PermAgentSpawn,
			PermAgentPrompt, PermAgentClose, PermAgentDecompose,
			PermAgentCacheRead, PermAgentCacheWrite, PermAgentAdmin,
		}
	case AdminRoleTenantAdmin:
		return []AdminPermission{
			PermTenantRead, PermTenantUpdate,
			PermUserCreate, PermUserRead, PermUserUpdate, PermUserDelete, PermUserResetMFA, PermUserSuspend, PermUserUnlock,
			PermGroupCreate, PermGroupRead, PermGroupUpdate, PermGroupDelete,
			PermRoleCreate, PermRoleRead, PermRoleUpdate, PermRoleDelete, PermRoleAssign,
			PermPolicyCreate, PermPolicyRead, PermPolicyUpdate, PermPolicyDelete, PermPolicySimulate,
			PermAuditRead,
			PermSessionRead, PermSessionRevoke,
			PermGDPRExport, PermGDPRDelete, PermConsentManage,
		}
	case AdminRoleUserAdmin:
		return []AdminPermission{
			PermUserCreate, PermUserRead, PermUserUpdate, PermUserDelete, PermUserResetMFA, PermUserSuspend, PermUserUnlock,
			PermGroupRead,
			PermRoleRead, PermRoleAssign,
		}
	case AdminRoleGroupAdmin:
		return []AdminPermission{
			PermUserRead,
			PermGroupCreate, PermGroupRead, PermGroupUpdate, PermGroupDelete,
		}
	case AdminRolePolicyAdmin:
		return []AdminPermission{
			PermPolicyCreate, PermPolicyRead, PermPolicyUpdate, PermPolicyDelete, PermPolicySimulate,
		}
	case AdminRoleAuditAdmin:
		return []AdminPermission{
			PermAuditRead, PermAuditExport,
		}
	case AdminRoleReadOnly:
		return []AdminPermission{
			PermTenantRead,
			PermUserRead,
			PermGroupRead,
			PermRoleRead,
			PermPolicyRead,
			PermAuditRead,
			PermSessionRead,
		}
	case AdminRoleAgentAdmin:
		return []AdminPermission{
			PermAgentDiscover, PermAgentRegister, PermAgentSpawn,
			PermAgentPrompt, PermAgentClose, PermAgentDecompose,
			PermAgentCacheRead, PermAgentCacheWrite, PermAgentAdmin,
		}
	case AdminRoleAgentOperator:
		return []AdminPermission{
			PermAgentDiscover, PermAgentRegister, PermAgentSpawn,
			PermAgentPrompt, PermAgentClose, PermAgentDecompose,
			PermAgentCacheRead, PermAgentCacheWrite,
		}
	case AdminRoleAgentViewer:
		return []AdminPermission{
			PermAgentDiscover, PermAgentCacheRead,
		}
	default:
		return nil
	}
}

// IsValidAdminRole checks if a role is valid.
func IsValidAdminRole(role AdminRole) bool {
	switch role {
	case AdminRoleSuperAdmin, AdminRoleTenantAdmin, AdminRoleUserAdmin,
		AdminRoleGroupAdmin, AdminRolePolicyAdmin, AdminRoleAuditAdmin, AdminRoleReadOnly,
		AdminRoleAgentAdmin, AdminRoleAgentOperator, AdminRoleAgentViewer:
		return true
	default:
		return false
	}
}

// Validate validates an admin assignment.
func (a *AdminAssignment) Validate() error {
	if a.UserID == "" {
		return NewIAMError(ErrAdminAssignmentInvalid, "iam.admin.validate", "user_id is required", nil)
	}
	if !IsValidAdminRole(a.AdminRole) {
		return NewIAMError(ErrAdminAssignmentInvalid, "iam.admin.validate", "invalid admin_role", nil)
	}
	if a.GrantedBy == "" {
		return NewIAMError(ErrAdminAssignmentInvalid, "iam.admin.validate", "granted_by is required", nil)
	}
	return nil
}
