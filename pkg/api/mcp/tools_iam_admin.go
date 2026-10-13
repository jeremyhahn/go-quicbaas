//go:build mcp

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

package mcp

import (
	"context"
	"fmt"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// Admin Manager Access
// =============================================================================

// adminManagerAccessor provides access to the admin manager from the QuicBaaS service.
type adminManagerAccessor interface {
	GetAdminManager(ctx context.Context) (*iam.AdminManager, error)
}

// AdminServiceUnavailableError indicates the admin service is not available.
type AdminServiceUnavailableError struct {
	Reason string
}

// Error returns the error message.
func (e *AdminServiceUnavailableError) Error() string {
	return fmt.Sprintf("admin service unavailable: %s", e.Reason)
}

// getAdminManager retrieves the admin manager from the QuicBaaS service.
func (r *ToolRegistry) getAdminManager(ctx context.Context) (*iam.AdminManager, error) {
	accessor, ok := r.service.(adminManagerAccessor)
	if !ok {
		return nil, api.NewServiceUnavailableError("admin")
	}
	mgr, err := accessor.GetAdminManager(ctx)
	if err != nil {
		return nil, api.NewOperationFailedError("get admin manager", err)
	}
	if mgr == nil {
		return nil, api.NewServiceUnavailableError("admin")
	}
	return mgr, nil
}

// =============================================================================
// Admin Assignment Tools Registration
// =============================================================================

func (r *ToolRegistry) registerAdminTools() {
	// Admin Assignment CRUD Tools
	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_admin_assignment_create",
		Description: "Create a new delegated admin assignment for a user",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User ID to grant admin privileges to",
				},
				"admin_role": map[string]interface{}{
					"type":        "string",
					"description": "Admin role type (super_admin, tenant_admin, user_admin, policy_admin, audit_admin, read_only)",
					"enum":        []string{"super_admin", "tenant_admin", "user_admin", "policy_admin", "audit_admin", "read_only"},
				},
				"tenant_ids": map[string]interface{}{
					"type":        "array",
					"description": "List of tenant IDs this assignment applies to (empty for global access)",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
				"permissions": map[string]interface{}{
					"type":        "array",
					"description": "Explicit permissions to grant (overrides role defaults)",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
				"denied_permissions": map[string]interface{}{
					"type":        "array",
					"description": "Permissions to explicitly deny even if role allows",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
				"resource_scopes": map[string]interface{}{
					"type":        "array",
					"description": "Resource scopes limiting access to specific resources",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"resource_type":    map[string]interface{}{"type": "string"},
							"resource_pattern": map[string]interface{}{"type": "string"},
						},
					},
				},
				"expires_at": map[string]interface{}{
					"type":        "string",
					"description": "Expiration time in RFC3339 format (optional)",
					"format":      "date-time",
				},
				"granted_by": map[string]interface{}{
					"type":        "string",
					"description": "User ID of the admin creating this assignment",
				},
				"metadata": map[string]interface{}{
					"type":        "object",
					"description": "Additional metadata for the assignment",
				},
			},
			"required": []string{"user_id", "admin_role", "granted_by"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_admin_assignment_get",
		Description: "Retrieve an admin assignment by ID",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"assignment_id": map[string]interface{}{
					"type":        "string",
					"description": "Admin assignment ID",
				},
			},
			"required": []string{"assignment_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_admin_assignment_update",
		Description: "Update an existing admin assignment",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"assignment_id": map[string]interface{}{
					"type":        "string",
					"description": "Admin assignment ID to update",
				},
				"admin_role": map[string]interface{}{
					"type":        "string",
					"description": "New admin role type",
					"enum":        []string{"super_admin", "tenant_admin", "user_admin", "policy_admin", "audit_admin", "read_only"},
				},
				"tenant_ids": map[string]interface{}{
					"type":        "array",
					"description": "Updated list of tenant IDs",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
				"permissions": map[string]interface{}{
					"type":        "array",
					"description": "Updated explicit permissions",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
				"denied_permissions": map[string]interface{}{
					"type":        "array",
					"description": "Updated denied permissions",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
				"resource_scopes": map[string]interface{}{
					"type":        "array",
					"description": "Updated resource scopes",
					"items": map[string]interface{}{
						"type": "object",
					},
				},
				"expires_at": map[string]interface{}{
					"type":        "string",
					"description": "New expiration time in RFC3339 format",
					"format":      "date-time",
				},
				"active": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether the assignment is active",
				},
				"metadata": map[string]interface{}{
					"type":        "object",
					"description": "Updated metadata",
				},
			},
			"required": []string{"assignment_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_admin_assignment_delete",
		Description: "Delete an admin assignment",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"assignment_id": map[string]interface{}{
					"type":        "string",
					"description": "Admin assignment ID to delete",
				},
			},
			"required": []string{"assignment_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_admin_assignment_list",
		Description: "List admin assignments with filtering and pagination",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "Filter by user ID",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Filter by tenant ID",
				},
				"admin_role": map[string]interface{}{
					"type":        "string",
					"description": "Filter by admin role",
					"enum":        []string{"super_admin", "tenant_admin", "user_admin", "policy_admin", "audit_admin", "read_only"},
				},
				"active_only": map[string]interface{}{
					"type":        "boolean",
					"description": "Only return active assignments",
					"default":     true,
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results",
					"default":     100,
				},
				"offset": map[string]interface{}{
					"type":        "integer",
					"description": "Number of results to skip",
					"default":     0,
				},
			},
		},
	})

	// Admin Permission Check Tool
	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_admin_check_permission",
		Description: "Check if an admin user has a specific permission",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "Admin user ID to check permissions for",
				},
				"permission": map[string]interface{}{
					"type":        "string",
					"description": "Permission to check (e.g., user:create, tenant:read)",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant context for the check (optional)",
				},
				"resource_type": map[string]interface{}{
					"type":        "string",
					"description": "Type of resource being accessed (optional)",
				},
				"resource_id": map[string]interface{}{
					"type":        "string",
					"description": "Specific resource ID being accessed (optional)",
				},
			},
			"required": []string{"user_id", "permission"},
		},
	})

	// Impersonation Tools
	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_impersonation_start",
		Description: "Start an impersonation session to act as another user. The acting admin is the authenticated caller and is never named in the request (go-quicbaas#383).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target_user_id": map[string]interface{}{
					"type":        "string",
					"description": "User ID to impersonate",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant context for impersonation",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Justification for the impersonation (required for audit)",
				},
				"duration_minutes": map[string]interface{}{
					"type":        "integer",
					"description": "Duration of impersonation in minutes (default: 60)",
					"default":     60,
					"minimum":     1,
					"maximum":     480,
				},
			},
			"required": []string{"target_user_id", "tenant_id", "reason"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_impersonation_end",
		Description: "End an active impersonation session",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Impersonation session ID to end",
				},
			},
			"required": []string{"session_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_impersonation_list",
		Description: "List active impersonation sessions for an admin user",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"admin_user_id": map[string]interface{}{
					"type":        "string",
					"description": "Admin user ID to list impersonations for",
				},
			},
			"required": []string{"admin_user_id"},
		},
	})
}

// =============================================================================
// Admin Assignment Execution Handlers
// =============================================================================

func (r *ToolRegistry) executeIAMAdminAssignmentCreate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	adminMgr, err := r.getAdminManager(ctx)
	if err != nil {
		return nil, err
	}

	opts := iam.CreateAdminAssignmentOptions{
		UserID:    getString(args, "user_id"),
		AdminRole: iam.AdminRole(getString(args, "admin_role")),
		GrantedBy: getString(args, "granted_by"),
	}

	// Parse tenant IDs
	if tenantIDsRaw, ok := args["tenant_ids"].([]interface{}); ok {
		opts.TenantIDs = make([]string, len(tenantIDsRaw))
		for i, tid := range tenantIDsRaw {
			if str, ok := tid.(string); ok {
				opts.TenantIDs[i] = str
			}
		}
	}

	// Parse permissions
	if permsRaw, ok := args["permissions"].([]interface{}); ok {
		opts.Permissions = make([]iam.AdminPermission, len(permsRaw))
		for i, p := range permsRaw {
			if str, ok := p.(string); ok {
				opts.Permissions[i] = iam.AdminPermission(str)
			}
		}
	}

	// Parse denied permissions
	if deniedRaw, ok := args["denied_permissions"].([]interface{}); ok {
		opts.DeniedPermissions = make([]iam.AdminPermission, len(deniedRaw))
		for i, p := range deniedRaw {
			if str, ok := p.(string); ok {
				opts.DeniedPermissions[i] = iam.AdminPermission(str)
			}
		}
	}

	// Parse resource scopes
	if scopesRaw, ok := args["resource_scopes"].([]interface{}); ok {
		opts.ResourceScopes = make([]iam.ResourceScope, 0, len(scopesRaw))
		for _, scopeRaw := range scopesRaw {
			if scopeMap, ok := scopeRaw.(map[string]interface{}); ok {
				scope := iam.ResourceScope{}
				if rt, ok := scopeMap["resource_type"].(string); ok {
					scope.ResourceType = rt
				}
				if rp, ok := scopeMap["resource_pattern"].(string); ok {
					scope.ResourcePattern = rp
				}
				opts.ResourceScopes = append(opts.ResourceScopes, scope)
			}
		}
	}

	// Parse expiration
	if expiresStr := getString(args, "expires_at"); expiresStr != "" {
		expiresAt, parseErr := time.Parse(time.RFC3339, expiresStr)
		if parseErr != nil {
			return nil, api.NewInvalidFormatError("expires_at", parseErr)
		}
		opts.ExpiresAt = &expiresAt
	}

	// Parse metadata
	if metaRaw, ok := args["metadata"].(map[string]interface{}); ok {
		opts.Metadata = make(map[string]string)
		for k, v := range metaRaw {
			if str, ok := v.(string); ok {
				opts.Metadata[k] = str
			}
		}
	}

	assignment, err := adminMgr.CreateAssignment(ctx, opts)
	if err != nil {
		return nil, err
	}
	return marshalResult(assignment)
}

func (r *ToolRegistry) executeIAMAdminAssignmentGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	adminMgr, err := r.getAdminManager(ctx)
	if err != nil {
		return nil, err
	}

	assignment, err := adminMgr.GetAssignment(ctx, getString(args, "assignment_id"))
	if err != nil {
		return nil, err
	}
	return marshalResult(assignment)
}

func (r *ToolRegistry) executeIAMAdminAssignmentUpdate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	adminMgr, err := r.getAdminManager(ctx)
	if err != nil {
		return nil, err
	}

	assignmentID := getString(args, "assignment_id")

	opts := iam.UpdateAdminAssignmentOptions{}

	// Parse admin role
	if roleStr := getString(args, "admin_role"); roleStr != "" {
		role := iam.AdminRole(roleStr)
		opts.AdminRole = &role
	}

	// Parse tenant IDs
	if tenantIDsRaw, ok := args["tenant_ids"].([]interface{}); ok {
		tenantIDs := make([]string, len(tenantIDsRaw))
		for i, tid := range tenantIDsRaw {
			if str, ok := tid.(string); ok {
				tenantIDs[i] = str
			}
		}
		opts.TenantIDs = tenantIDs
	}

	// Parse permissions
	if permsRaw, ok := args["permissions"].([]interface{}); ok {
		perms := make([]iam.AdminPermission, len(permsRaw))
		for i, p := range permsRaw {
			if str, ok := p.(string); ok {
				perms[i] = iam.AdminPermission(str)
			}
		}
		opts.Permissions = perms
	}

	// Parse denied permissions
	if deniedRaw, ok := args["denied_permissions"].([]interface{}); ok {
		denied := make([]iam.AdminPermission, len(deniedRaw))
		for i, p := range deniedRaw {
			if str, ok := p.(string); ok {
				denied[i] = iam.AdminPermission(str)
			}
		}
		opts.DeniedPermissions = denied
	}

	// Parse resource scopes
	if scopesRaw, ok := args["resource_scopes"].([]interface{}); ok {
		scopes := make([]iam.ResourceScope, 0, len(scopesRaw))
		for _, scopeRaw := range scopesRaw {
			if scopeMap, ok := scopeRaw.(map[string]interface{}); ok {
				scope := iam.ResourceScope{}
				if rt, ok := scopeMap["resource_type"].(string); ok {
					scope.ResourceType = rt
				}
				if rp, ok := scopeMap["resource_pattern"].(string); ok {
					scope.ResourcePattern = rp
				}
				scopes = append(scopes, scope)
			}
		}
		opts.ResourceScopes = scopes
	}

	// Parse expiration
	if expiresStr := getString(args, "expires_at"); expiresStr != "" {
		expiresAt, parseErr := time.Parse(time.RFC3339, expiresStr)
		if parseErr != nil {
			return nil, api.NewInvalidFormatError("expires_at", parseErr)
		}
		opts.ExpiresAt = &expiresAt
	}

	// Parse active
	if active, ok := args["active"].(bool); ok {
		opts.Active = &active
	}

	// Parse metadata
	if metaRaw, ok := args["metadata"].(map[string]interface{}); ok {
		meta := make(map[string]string)
		for k, v := range metaRaw {
			if str, ok := v.(string); ok {
				meta[k] = str
			}
		}
		opts.Metadata = meta
	}

	assignment, err := adminMgr.UpdateAssignment(ctx, assignmentID, opts)
	if err != nil {
		return nil, err
	}
	return marshalResult(assignment)
}

func (r *ToolRegistry) executeIAMAdminAssignmentDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	adminMgr, err := r.getAdminManager(ctx)
	if err != nil {
		return nil, err
	}

	err = adminMgr.DeleteAssignment(ctx, getString(args, "assignment_id"))
	if err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{
		"deleted":   true,
		"timestamp": time.Now(),
	})
}

func (r *ToolRegistry) executeIAMAdminAssignmentList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	adminMgr, err := r.getAdminManager(ctx)
	if err != nil {
		return nil, err
	}

	ints := newIntArgReader(args)
	limit := ints.IntOr("limit", 100)
	offset := ints.Int("offset")
	if err := ints.Err(); err != nil {
		return nil, err
	}

	activeOnly := false
	if active, ok := args["active_only"].(bool); ok {
		activeOnly = active
	}

	opts := iam.ListAdminAssignmentsOptions{
		UserID:     getString(args, "user_id"),
		TenantID:   getString(args, "tenant_id"),
		AdminRole:  iam.AdminRole(getString(args, "admin_role")),
		ActiveOnly: activeOnly,
		Limit:      limit,
		Offset:     offset,
	}

	assignments, err := adminMgr.ListAssignments(ctx, opts)
	if err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{
		"assignments": assignments,
		"total_count": len(assignments),
		"timestamp":   time.Now(),
	})
}

func (r *ToolRegistry) executeIAMAdminCheckPermission(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	adminMgr, err := r.getAdminManager(ctx)
	if err != nil {
		return nil, err
	}

	req := &iam.AdminCheckRequest{
		UserID:       getString(args, "user_id"),
		Permission:   iam.AdminPermission(getString(args, "permission")),
		TenantID:     getString(args, "tenant_id"),
		ResourceType: getString(args, "resource_type"),
		ResourceID:   getString(args, "resource_id"),
	}

	resp, err := adminMgr.CheckPermission(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

// =============================================================================
// Impersonation Execution Handlers
// =============================================================================

func (r *ToolRegistry) executeIAMImpersonationStart(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	adminMgr, err := r.getAdminManager(ctx)
	if err != nil {
		return nil, err
	}

	durationMinutes := getUint64(args, "duration_minutes")
	if durationMinutes == 0 {
		durationMinutes = 60
	}

	// No admin_user_id from args: the acting admin is the authenticated
	// caller, resolved by the manager from the verified principal in
	// context (go-quicbaas#383).
	opts := &iam.StartImpersonationOptions{
		TargetUserID: getString(args, "target_user_id"),
		TenantID:     getString(args, "tenant_id"),
		Reason:       getString(args, "reason"),
		Duration:     time.Duration(durationMinutes) * time.Minute,
	}

	session, err := adminMgr.StartImpersonation(ctx, opts)
	if err != nil {
		return nil, err
	}
	return marshalResult(session)
}

func (r *ToolRegistry) executeIAMImpersonationEnd(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	adminMgr, err := r.getAdminManager(ctx)
	if err != nil {
		return nil, err
	}

	err = adminMgr.EndImpersonation(ctx, getString(args, "session_id"))
	if err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{
		"ended":     true,
		"timestamp": time.Now(),
	})
}

func (r *ToolRegistry) executeIAMImpersonationList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	adminMgr, err := r.getAdminManager(ctx)
	if err != nil {
		return nil, err
	}

	sessions, err := adminMgr.GetActiveImpersonations(ctx, getString(args, "admin_user_id"))
	if err != nil {
		return nil, err
	}
	if sessions == nil {
		sessions = []*iam.ImpersonationSession{}
	}
	return marshalResult(map[string]interface{}{
		"sessions":  sessions,
		"timestamp": time.Now(),
	})
}
