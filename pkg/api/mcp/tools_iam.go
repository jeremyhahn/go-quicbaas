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
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"

	"gopkg.in/yaml.v3"
)

// iamServiceAccessor is the interface for accessing the IAM service from QuicBaaS.
// The IAM() method is only available when built with the "iam" build tag.
type iamServiceAccessor interface {
	IAM() service.IAMService
}

// getIAMService retrieves the IAM service from the QuicBaaS service.
// It returns an error if the service doesn't support IAM or if IAM is not configured.
func (r *ToolRegistry) getIAMService() (service.IAMService, error) {
	accessor, ok := r.service.(iamServiceAccessor)
	if !ok {
		return nil, api.NewServiceUnavailableError("IAM")
	}
	iamSvc := accessor.IAM()
	if iamSvc == nil {
		return nil, api.NewServiceNotConfiguredError("IAM")
	}
	return iamSvc, nil
}

func (r *ToolRegistry) registerIAMTools() {
	// Register all IAM subsystem tools
	r.registerTenantTools()
	r.registerUserTools()
	r.registerRoleTools()
	r.registerPolicyTools()
	r.registerAppTools()
	r.registerMFATools()
	r.registerIAMWebAuthnTools()
	r.registerPEPTools()
	// Register delegated administration and GDPR tools
	r.registerAdminTools()
	r.registerGDPRTools()
	// Register federation and identity tools
	r.registerFederationTools()
	r.registerIdentityTools()
	// Register session management tools
	r.registerSessionTools()
	// Register audit tools
	r.registerAuditTools()
}

// =============================================================================
// Tenant Tools
// =============================================================================

func (r *ToolRegistry) registerTenantTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_tenant_create",
		Description: "Create a new IAM tenant with optional settings and quotas",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID (optional, auto-generated if not provided)",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Human-readable tenant name",
				},
				"settings": map[string]interface{}{
					"type":        "object",
					"description": "Tenant-specific configuration settings",
				},
				"quotas": map[string]interface{}{
					"type":        "object",
					"description": "Resource limits for this tenant",
				},
				"isolation_type": map[string]interface{}{
					"type":        "string",
					"description": "Engine isolation strategy: prefix (default), barrier, volume, or raft. Only settable at creation.",
				},
				"engine_quotas": map[string]interface{}{
					"type":        "object",
					"description": "Engine-level resource limits (max_databases, max_keys, max_storage_bytes, ...) applied at creation",
				},
				"engine_entitlements": map[string]interface{}{
					"type":        "object",
					"description": "Engine-level feature entitlements (isolation tiers, dedicated storage, WAL features) applied at creation",
				},
			},
			"required": []string{"name"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_tenant_get",
		Description: "Retrieve a tenant by ID",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant identifier",
				},
			},
			"required": []string{"id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_tenant_update",
		Description: "Update an existing tenant",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant identifier",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Updated tenant name",
				},
				"settings": map[string]interface{}{
					"type":        "object",
					"description": "Updated tenant settings",
				},
				"quotas": map[string]interface{}{
					"type":        "object",
					"description": "Updated resource limits",
				},
			},
			"required": []string{"id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_tenant_delete",
		Description: "Delete a tenant and all its resources",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant identifier",
				},
			},
			"required": []string{"id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_tenant_provision",
		Description: "Provision a tenant and its first admin (sysadmin onboarding when public registration is off)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant identifier",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Tenant display name",
				},
				"isolation_type": map[string]interface{}{
					"type":        "string",
					"description": "Tenant isolation type (defaults to barrier)",
				},
				"admin_username": map[string]interface{}{
					"type":        "string",
					"description": "Username of the first tenant admin",
				},
				"admin_enrollment": map[string]interface{}{
					"type":        "string",
					"description": "How the first admin enrolls: cert or invite",
				},
				"admin_csr_pem": map[string]interface{}{
					"type":        "string",
					"description": "PEM-encoded admin CSR, required for cert enrollment",
				},
				"invite_expiry_hours": map[string]interface{}{
					"type":        "integer",
					"description": "Lifetime of the admin invite token in hours",
				},
			},
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_tenant_list",
		Description: "List all tenants with pagination and filtering",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
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
				"filter": map[string]interface{}{
					"type":        "object",
					"description": "Filter by tenant fields",
				},
			},
		},
	})
}

// =============================================================================
// User Tools
// =============================================================================

func (r *ToolRegistry) registerUserTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_user_create",
		Description: "Create a new user in a tenant",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"email": map[string]interface{}{
					"type":        "string",
					"description": "User email address (unique within tenant)",
				},
				"username": map[string]interface{}{
					"type":        "string",
					"description": "Tenant-unique handle: 3-64 characters of letters, digits, dash or underscore. Distinct from the display name; derived from the email when omitted.",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "User display name",
				},
				"roles": map[string]interface{}{
					"type":        "array",
					"description": "List of role IDs to assign",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
				"attributes": map[string]interface{}{
					"type":                 "object",
					"description":          "Custom user attributes. Values must be strings.",
					"additionalProperties": map[string]interface{}{"type": "string"},
				},
				"auth_methods": map[string]interface{}{
					"type":        "array",
					"description": "Credential kinds the account may present. Defaults to [\"webauthn\"].",
					"items":       map[string]interface{}{"type": "string", "enum": []string{"webauthn", "piv"}},
				},
				"lifecycle": map[string]interface{}{
					"type":        "string",
					"description": "State to create in. Suspended and locked describe something that HAPPENS to an account and cannot be asserted at creation.",
					"enum":        []string{"pending", "active", "disabled"},
				},
				"invite_expiry_hours": map[string]interface{}{
					"type":        "integer",
					"description": "Lifetime of a pending user's activation invite in hours. Defaults to 24; capped at 168.",
				},
			},
			"required": []string{"tenant_id", "email", "name"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_user_get",
		Description: "Retrieve a user by ID",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User identifier",
				},
			},
			"required": []string{"tenant_id", "user_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_user_update",
		Description: "Update an existing user",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User identifier",
				},
				"email": map[string]interface{}{
					"type":        "string",
					"description": "Updated email address",
				},
				"username": map[string]interface{}{
					"type":        "string",
					"description": "Updated tenant-unique handle. Applied independently of the display name.",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Updated display name",
				},
				"roles": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string"},
				},
				"attributes": map[string]interface{}{
					"type":                 "object",
					"description":          "Custom user attributes. Values must be strings.",
					"additionalProperties": map[string]interface{}{"type": "string"},
				},
				"lifecycle": map[string]interface{}{
					"type":        "string",
					"description": "Account state: pending, active, suspended, locked or disabled. `deleting` is claimed by the deletion workflow and is refused here — delete the user instead.",
					"enum":        []string{"pending", "active", "suspended", "locked", "disabled"},
				},
				"auth_methods": map[string]interface{}{
					"type":        "array",
					"description": "Credential kinds the account may present.",
					"items":       map[string]interface{}{"type": "string", "enum": []string{"webauthn", "piv"}},
				},
			},
			"required": []string{"tenant_id", "user_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_user_delete",
		Description: "Delete a user from a tenant",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User identifier",
				},
			},
			"required": []string{"tenant_id", "user_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_user_list",
		Description: "List all users in a tenant with pagination",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"limit": map[string]interface{}{
					"type":    "integer",
					"default": 100,
				},
				"offset": map[string]interface{}{
					"type":    "integer",
					"default": 0,
				},
				"filter": map[string]interface{}{
					"type": "object",
				},
			},
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_user_assign_role",
		Description: "Assign a role to a user",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User identifier",
				},
				"role_id": map[string]interface{}{
					"type":        "string",
					"description": "Role identifier",
				},
			},
			"required": []string{"tenant_id", "user_id", "role_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_user_revoke_role",
		Description: "Revoke a role from a user",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User identifier",
				},
				"role_id": map[string]interface{}{
					"type":        "string",
					"description": "Role identifier",
				},
			},
			"required": []string{"tenant_id", "user_id", "role_id"},
		},
	})
}

// =============================================================================
// Role Tools
// =============================================================================

func (r *ToolRegistry) registerRoleTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_role_create",
		Description: "Create a new role in a tenant",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Role display name",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Role purpose description",
				},
				"permissions": map[string]interface{}{
					"type":        "array",
					"description": "List of permission strings",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
			},
			"required": []string{"tenant_id", "name"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_role_get",
		Description: "Retrieve a role by ID",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"role_id": map[string]interface{}{
					"type":        "string",
					"description": "Role identifier",
				},
			},
			"required": []string{"tenant_id", "role_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_role_update",
		Description: "Update an existing role",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"role_id": map[string]interface{}{
					"type":        "string",
					"description": "Role identifier",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Updated role name",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Updated description",
				},
				"permissions": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string"},
				},
			},
			"required": []string{"tenant_id", "role_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_role_delete",
		Description: "Delete a role from a tenant",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"role_id": map[string]interface{}{
					"type":        "string",
					"description": "Role identifier",
				},
			},
			"required": []string{"tenant_id", "role_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name: "quicbaas_iam_role_reconcile",
		Description: "Converge a tenant's built-in roles onto the running build's definitions: " +
			"adds the verbs the build now grants, removes the ones it no longer grants, and " +
			"leaves custom roles alone. Idempotent — a converged tenant reports no changed " +
			"roles. This is the only path that can re-permission a built-in role, because " +
			"qrdb refuses an ordinary update on a system role.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant whose built-in roles are reconciled",
				},
			},
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_role_list",
		Description: "List all roles in a tenant with pagination",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"limit": map[string]interface{}{
					"type":    "integer",
					"default": 100,
				},
				"offset": map[string]interface{}{
					"type":    "integer",
					"default": 0,
				},
				"filter": map[string]interface{}{
					"type": "object",
				},
			},
			"required": []string{"tenant_id"},
		},
	})
}

// =============================================================================
// Policy Tools
// =============================================================================

func (r *ToolRegistry) registerPolicyTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_policy_create",
		Description: "Create a new policy in a tenant",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"policy": map[string]interface{}{
					"type":        "object",
					"description": "Policy document with rules and conditions",
				},
			},
			"required": []string{"tenant_id", "policy"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_policy_get",
		Description: "Retrieve a policy by name",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Policy name",
				},
			},
			"required": []string{"tenant_id", "name"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_policy_update",
		Description: "Update an existing policy",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Policy name",
				},
				"policy": map[string]interface{}{
					"type":        "object",
					"description": "Updated policy document",
				},
			},
			"required": []string{"tenant_id", "name", "policy"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_policy_delete",
		Description: "Delete a policy from a tenant",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Policy name",
				},
			},
			"required": []string{"tenant_id", "name"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_policy_list",
		Description: "List all policies in a tenant with pagination",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"limit": map[string]interface{}{
					"type":    "integer",
					"default": 100,
				},
				"offset": map[string]interface{}{
					"type":    "integer",
					"default": 0,
				},
				"filter": map[string]interface{}{
					"type": "object",
				},
			},
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_policy_test",
		Description: "Test a policy against an authorization check",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"policy": map[string]interface{}{
					"type":        "object",
					"description": "Policy document to test",
				},
				"check": map[string]interface{}{
					"type":        "object",
					"description": "Authorization check request",
					"properties": map[string]interface{}{
						"principal": map[string]interface{}{"type": "string"},
						"action":    map[string]interface{}{"type": "string"},
						"resource":  map[string]interface{}{"type": "string"},
						"context":   map[string]interface{}{"type": "object"},
					},
					"required": []string{"principal", "action", "resource"},
				},
			},
			"required": []string{"policy", "check"},
		},
	})
}

// =============================================================================
// Application Tools
// =============================================================================

func (r *ToolRegistry) registerAppTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_app_register",
		Description: "Register a new OAuth2 application",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Application display name",
				},
				"redirect_uris": map[string]interface{}{
					"type":        "array",
					"description": "Allowed OAuth redirect URIs",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
				"grant_types": map[string]interface{}{
					"type":        "array",
					"description": "Allowed OAuth grant types",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
				"response_types": map[string]interface{}{
					"type":        "array",
					"description": "Allowed OAuth response types",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
				"resource_prefix": map[string]interface{}{
					"type":        "string",
					"description": "Resource namespace for this application",
				},
			},
			"required": []string{"tenant_id", "name", "redirect_uris", "grant_types", "response_types"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_app_get",
		Description: "Retrieve an application by client ID",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"client_id": map[string]interface{}{
					"type":        "string",
					"description": "OAuth client identifier",
				},
			},
			"required": []string{"client_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_app_update",
		Description: "Update an existing application",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"client_id": map[string]interface{}{
					"type":        "string",
					"description": "OAuth client identifier",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Updated application name",
				},
				"redirect_uris": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string"},
				},
				"grant_types": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string"},
				},
				"response_types": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string"},
				},
				"resource_prefix": map[string]interface{}{
					"type": "string",
				},
			},
			"required": []string{"client_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_app_delete",
		Description: "Delete an application",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"client_id": map[string]interface{}{
					"type":        "string",
					"description": "OAuth client identifier",
				},
			},
			"required": []string{"client_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_app_list",
		Description: "List all applications in a tenant with pagination",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"limit": map[string]interface{}{
					"type":    "integer",
					"default": 100,
				},
				"offset": map[string]interface{}{
					"type":    "integer",
					"default": 0,
				},
				"filter": map[string]interface{}{
					"type": "object",
				},
			},
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_app_rotate_secret",
		Description: "Rotate an application's client secret",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"client_id": map[string]interface{}{
					"type":        "string",
					"description": "OAuth client identifier",
				},
			},
			"required": []string{"client_id"},
		},
	})
}

// =============================================================================
// MFA Tools
// =============================================================================

func (r *ToolRegistry) registerMFATools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_mfa_enroll_begin",
		Description: "Begin MFA enrollment for a user",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User identifier",
				},
				"method": map[string]interface{}{
					"type":        "string",
					"description": "MFA method to enroll",
					"enum":        []string{"totp", "webauthn"},
				},
			},
			"required": []string{"tenant_id", "user_id", "method"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_mfa_enroll_complete",
		Description: "Complete MFA enrollment by verifying the initial code",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"enrollment_id": map[string]interface{}{
					"type":        "string",
					"description": "Enrollment identifier from begin step",
				},
				"code": map[string]interface{}{
					"type":        "string",
					"description": "OTP code from authenticator app",
				},
			},
			"required": []string{"enrollment_id", "code"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_mfa_enroll_cancel",
		Description: "Cancel an in-progress MFA enrollment",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"enrollment_id": map[string]interface{}{
					"type":        "string",
					"description": "Enrollment identifier",
				},
			},
			"required": []string{"enrollment_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_mfa_status",
		Description: "Get MFA enrollment status for a user",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User identifier",
				},
			},
			"required": []string{"tenant_id", "user_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_mfa_disable",
		Description: "Disable MFA for a user",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User identifier",
				},
			},
			"required": []string{"tenant_id", "user_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_mfa_challenge_begin",
		Description: "Begin MFA challenge during authentication",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User identifier",
				},
				"session_token": map[string]interface{}{
					"type":        "string",
					"description": "Partial authentication token",
				},
			},
			"required": []string{"tenant_id", "user_id", "session_token"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_mfa_challenge_verify",
		Description: "Verify MFA challenge with OTP code",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"challenge_id": map[string]interface{}{
					"type":        "string",
					"description": "Challenge identifier from begin step",
				},
				"code": map[string]interface{}{
					"type":        "string",
					"description": "OTP code from authenticator app",
				},
				"use_backup_code": map[string]interface{}{
					"type":        "boolean",
					"description": "Use backup code instead of OTP",
					"default":     false,
				},
			},
			"required": []string{"challenge_id", "code"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_mfa_backup_codes_generate",
		Description: "Generate backup codes for a user",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User identifier",
				},
				"count": map[string]interface{}{
					"type":        "integer",
					"description": "Number of backup codes to generate",
					"default":     10,
					"minimum":     1,
					"maximum":     20,
				},
			},
			"required": []string{"tenant_id", "user_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_mfa_backup_codes_verify",
		Description: "Verify a backup code",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User identifier",
				},
				"code": map[string]interface{}{
					"type":        "string",
					"description": "Backup code to verify",
				},
			},
			"required": []string{"tenant_id", "user_id", "code"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_mfa_backup_codes_status",
		Description: "Get backup code status for a user",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User identifier",
				},
			},
			"required": []string{"tenant_id", "user_id"},
		},
	})
}

// =============================================================================
// PEP (Policy Enforcement Point) Tools
// =============================================================================

func (r *ToolRegistry) registerPEPTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_pep_check",
		Description: "Check if an action is authorized",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Owning tenant ID",
				},
				"principal": map[string]interface{}{
					"type":        "string",
					"description": "Principal identifier (user, service, etc.)",
				},
				"action": map[string]interface{}{
					"type":        "string",
					"description": "Action to perform (read, write, delete, etc.)",
				},
				"resource": map[string]interface{}{
					"type":        "string",
					"description": "Resource identifier",
				},
				"context": map[string]interface{}{
					"type":        "object",
					"description": "Additional context for policy evaluation",
				},
			},
			"required": []string{"tenant_id", "principal", "action", "resource"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_pep_check_batch",
		Description: "Check multiple authorization requests in batch",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"checks": map[string]interface{}{
					"type":        "array",
					"description": "List of authorization checks",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"tenant_id": map[string]interface{}{"type": "string"},
							"principal": map[string]interface{}{"type": "string"},
							"action":    map[string]interface{}{"type": "string"},
							"resource":  map[string]interface{}{"type": "string"},
							"context":   map[string]interface{}{"type": "object"},
						},
						"required": []string{"tenant_id", "principal", "action", "resource"},
					},
				},
			},
			"required": []string{"checks"},
		},
	})
}

// =============================================================================
// Execution Handlers - Tenants
// =============================================================================

func (r *ToolRegistry) executeIAMTenantCreate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	req := &service.CreateTenantRequest{
		ID:   getString(args, "id"),
		Name: getString(args, "name"),
	}

	// Parse optional fields
	if settingsRaw, ok := args["settings"].(map[string]interface{}); ok {
		// Convert string numeric values to integers for proper JSON parsing
		convertedSettings := convertStringNumerics(settingsRaw)
		settings := &iam.TenantSettings{}
		if parseErr := parseJSONObject(convertedSettings, settings); parseErr == nil {
			req.Settings = settings
		}
		// max_users from settings is an engine-level quota owned by qrdb; ignore it here.
		_ = settingsRaw
	}
	if quotasRaw, ok := args["quotas"].(map[string]interface{}); ok {
		quotas := &iam.TenantQuotas{}
		if parseErr := parseJSONObject(quotasRaw, quotas); parseErr == nil {
			req.Quotas = quotas
		}
	}
	// Engine provisioning knobs. Isolation is bound by the engine at creation
	// and the quotas/entitlements are applied in the same call, so this tool
	// is the only MCP surface that can set them.
	if raw, present := args["isolation_type"]; present && raw != nil {
		isolation, ok := raw.(string)
		if !ok {
			return nil, &TenantEngineObjectError{Field: "isolation_type", Reason: "expected a string"}
		}
		req.IsolationType = qrdbsdk.TenantIsolationType(isolation)
	}
	// Decoded into the SDK's limits-only view, so the engine-owned current_*
	// counters are rejected as unknown fields — the gRPC create request models
	// limits only, and accepting them here would give the same request a
	// different meaning per transport.
	engineQuotas := &iamsdk.EngineQuotas{}
	present, err := decodeTenantEngineObject(args, "engine_quotas", engineQuotas)
	if err != nil {
		return nil, err
	}
	if present {
		req.EngineQuotas = engineQuotas.Quotas()
	}
	entitlements := &qrdbsdk.Entitlements{}
	present, err = decodeTenantEngineObject(args, "engine_entitlements", entitlements)
	if err != nil {
		return nil, err
	}
	if present {
		req.EngineEntitlements = entitlements
	}
	// max_users is an engine-level quota owned by qrdb TenantServicer; it is
	// not settable via the BaaS IAM quota surface.

	resp, err := iamSvc.CreateTenant(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMTenantGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	req := &service.GetTenantRequest{
		ID: getString(args, "id"),
	}

	resp, err := iamSvc.GetTenant(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMTenantUpdate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	req := &service.UpdateTenantRequest{
		ID: getString(args, "id"),
	}

	if name := getString(args, "name"); name != "" {
		req.Name = &name
	}
	if settingsRaw, ok := args["settings"].(map[string]interface{}); ok {
		// Convert string numeric values to integers for proper JSON parsing
		convertedSettings := convertStringNumerics(settingsRaw)
		settings := &iam.TenantSettings{}
		if parseErr := parseJSONObject(convertedSettings, settings); parseErr == nil {
			req.Settings = settings
		}
		// max_users from settings is an engine-level quota owned by qrdb; ignore it here.
		_ = settingsRaw
	}
	if quotasRaw, ok := args["quotas"].(map[string]interface{}); ok {
		quotas := &iam.TenantQuotas{}
		if parseErr := parseJSONObject(quotasRaw, quotas); parseErr == nil {
			req.Quotas = quotas
		}
	}
	// max_users is an engine-level quota owned by qrdb TenantServicer.

	resp, err := iamSvc.UpdateTenant(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMTenantDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	req := &service.DeleteTenantRequest{
		ID: getString(args, "id"),
	}

	resp, err := iamSvc.DeleteTenant(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMTenantProvision(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	req := &service.IAMProvisionTenantRequest{
		TenantID:          getString(args, "tenant_id"),
		Name:              getString(args, "name"),
		IsolationType:     qrdbsdk.TenantIsolationType(getString(args, "isolation_type")),
		AdminUsername:     getString(args, "admin_username"),
		AdminEnrollment:   service.IAMTenantAdminEnrollment(getString(args, "admin_enrollment")),
		AdminCSRPEM:       getString(args, "admin_csr_pem"),
		InviteExpiryHours: getInt(args, "invite_expiry_hours"),
	}

	resp, err := iamSvc.ProvisionTenant(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMTenantList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	ints := newIntArgReader(args)
	limit := ints.IntOr("limit", 100)
	offset := ints.Int("offset")
	if err := ints.Err(); err != nil {
		return nil, err
	}

	req := &service.ListTenantsRequest{
		Limit:  limit,
		Offset: offset,
	}

	if filterRaw, ok := args["filter"].(map[string]interface{}); ok {
		req.Filter = filterRaw
	}

	resp, err := iamSvc.ListTenants(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

// =============================================================================
// Execution Handlers - Users
// =============================================================================

func (r *ToolRegistry) executeIAMUserCreate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	req := &service.IAMCreateUserRequest{
		TenantID:          getString(args, "tenant_id"),
		Email:             getString(args, "email"),
		Username:          getString(args, "username"),
		Name:              getString(args, "name"),
		Lifecycle:         iam.Lifecycle(getString(args, "lifecycle")),
		InviteExpiryHours: getInt(args, "invite_expiry_hours"),
	}

	if raw, ok := args["auth_methods"].([]interface{}); ok {
		req.AuthMethods = stringSlice(raw)
	}

	if rolesRaw, ok := args["roles"].([]interface{}); ok {
		req.Roles = make([]string, len(rolesRaw))
		for i, role := range rolesRaw {
			if str, ok := role.(string); ok {
				req.Roles[i] = str
			}
		}
	}

	// Attributes are string-valued in the store. A value that is not a string is
	// REFUSED, not dropped: accepting the request and persisting less than it
	// asked for is silent data loss at an API boundary, and these are
	// authorization attributes — a policy evaluated over the subset that
	// survived is not the policy the caller wrote.
	if attrRaw, ok := args["attributes"].(map[string]interface{}); ok {
		attrs, err := stringAttributes(attrRaw)
		if err != nil {
			return nil, err
		}
		req.Attributes = attrs
	}

	resp, err := iamSvc.CreateUser(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMUserGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	userID, err := getUserID(args)
	if err != nil {
		return nil, err
	}
	req := &service.IAMGetUserRequest{
		TenantID: getString(args, "tenant_id"),
		UserID:   userID,
	}

	resp, err := iamSvc.GetUser(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMUserUpdate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	userID, err := getUserID(args)
	if err != nil {
		return nil, err
	}
	req := &service.IAMUpdateUserRequest{
		TenantID: getString(args, "tenant_id"),
		UserID:   userID,
	}

	if email := getString(args, "email"); email != "" {
		req.Email = &email
	}
	if username := getString(args, "username"); username != "" {
		req.Username = &username
	}
	if name := getString(args, "name"); name != "" {
		req.Name = &name
	}
	if rolesRaw, ok := args["roles"].([]interface{}); ok {
		roles := make([]string, len(rolesRaw))
		for i, role := range rolesRaw {
			if str, ok := role.(string); ok {
				roles[i] = str
			}
		}
		req.Roles = &roles
	}
	// The CONVERTED map is what travels on, not the raw one. Validating the
	// argument and then forwarding the untyped original left the service layer
	// holding a shape the store cannot write, and it was dropped there — the
	// update reported success and the old attributes stayed. Sending the
	// converted value means the check and the payload cannot disagree.
	if attrRaw, ok := args["attributes"].(map[string]interface{}); ok {
		attrs, err := stringAttributes(attrRaw)
		if err != nil {
			return nil, err
		}
		req.Attributes = &attrs
	}
	if lifecycle, ok := args["lifecycle"].(string); ok && lifecycle != "" {
		state := iam.Lifecycle(lifecycle)
		req.Lifecycle = &state
	}
	// Presence, not length: an explicitly empty list clears the restriction.
	if raw, ok := args["auth_methods"].([]interface{}); ok {
		methods := stringSlice(raw)
		req.AuthMethods = &methods
	}

	resp, err := iamSvc.UpdateUser(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMUserDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	userID, err := getUserID(args)
	if err != nil {
		return nil, err
	}
	req := &service.IAMDeleteUserRequest{
		TenantID: getString(args, "tenant_id"),
		UserID:   userID,
	}

	resp, err := iamSvc.DeleteUser(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMUserList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	ints := newIntArgReader(args)
	limit := ints.IntOr("limit", 100)
	offset := ints.Int("offset")
	if err := ints.Err(); err != nil {
		return nil, err
	}

	req := &service.IAMListUsersRequest{
		TenantID: getString(args, "tenant_id"),
		Limit:    limit,
		Offset:   offset,
	}

	if filterRaw, ok := args["filter"].(map[string]interface{}); ok {
		req.Filter = filterRaw
	}

	resp, err := iamSvc.ListUsers(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMUserAssignRole(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	// The IAMService interface uses AssignRoleRequest (alias for AuthAssignRoleRequest)
	// which has Username and RoleName fields, not TenantID/UserID/RoleID.
	// Map the MCP args to the expected interface types.
	req := &service.AssignRoleRequest{
		Username: getString(args, "user_id"),
		RoleName: getString(args, "role_id"),
	}

	resp, err := iamSvc.AssignRole(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMUserRevokeRole(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	// The IAMService interface uses RevokeRoleRequest (alias for AuthRevokeRoleRequest)
	// which has Username and RoleName fields, not TenantID/UserID/RoleID.
	// Map the MCP args to the expected interface types.
	req := &service.RevokeRoleRequest{
		Username: getString(args, "user_id"),
		RoleName: getString(args, "role_id"),
	}

	resp, err := iamSvc.RevokeRole(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

// =============================================================================
// Execution Handlers - Roles
// =============================================================================

func (r *ToolRegistry) executeIAMRoleCreate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	req := &service.IAMCreateRoleRequest{
		TenantID:    getString(args, "tenant_id"),
		ID:          getString(args, "id"),
		Name:        getString(args, "name"),
		Description: getString(args, "description"),
	}

	if permsRaw, ok := args["permissions"].([]interface{}); ok {
		req.Permissions = make([]string, len(permsRaw))
		for i, p := range permsRaw {
			if str, ok := p.(string); ok {
				req.Permissions[i] = str
			}
		}
	}

	resp, err := iamSvc.CreateRole(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMRoleGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	req := &service.IAMGetRoleRequest{
		TenantID: getString(args, "tenant_id"),
		RoleID:   getString(args, "role_id"),
	}

	resp, err := iamSvc.GetRole(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMRoleUpdate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	req := &service.IAMUpdateRoleRequest{
		TenantID: getString(args, "tenant_id"),
		RoleID:   getString(args, "role_id"),
	}

	if name := getString(args, "name"); name != "" {
		req.Name = &name
	}
	if desc := getString(args, "description"); desc != "" {
		req.Description = &desc
	}
	if permsRaw, ok := args["permissions"].([]interface{}); ok {
		perms := make([]string, len(permsRaw))
		for i, p := range permsRaw {
			if str, ok := p.(string); ok {
				perms[i] = str
			}
		}
		req.Permissions = &perms
	}

	resp, err := iamSvc.UpdateRole(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMRoleDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	req := &service.IAMDeleteRoleRequest{
		TenantID: getString(args, "tenant_id"),
		RoleID:   getString(args, "role_id"),
	}

	resp, err := iamSvc.DeleteRole(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMRoleList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	ints := newIntArgReader(args)
	limit := ints.IntOr("limit", 100)
	offset := ints.Int("offset")
	if err := ints.Err(); err != nil {
		return nil, err
	}

	req := &service.IAMListRolesRequest{
		TenantID: getString(args, "tenant_id"),
		Limit:    limit,
		Offset:   offset,
	}

	if filterRaw, ok := args["filter"].(map[string]interface{}); ok {
		req.Filter = filterRaw
	}

	resp, err := iamSvc.ListRoles(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMRoleReconcile(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	req := &service.IAMReconcileRolesRequest{
		TenantID: getString(args, "tenant_id"),
	}

	resp, err := iamSvc.ReconcileBuiltInRoles(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

// =============================================================================
// Execution Handlers - Policies
// =============================================================================

func (r *ToolRegistry) executeIAMPolicyCreate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	policyRaw, ok := args["policy"].(map[string]interface{})
	if !ok {
		return nil, api.NewInvalidTypeError("policy", "an object")
	}

	tenantID := getString(args, "tenant_id")

	// Try direct parse as server-format policy first (has apiVersion, kind, metadata, spec)
	policy := &iam.Policy{}
	if err := parseJSONObject(policyRaw, policy); err != nil {
		return nil, api.NewInvalidFormatError("policy", err)
	}

	// If the policy doesn't have apiVersion, try parsing from SDK format with spec_yaml
	if policy.APIVersion == "" {
		policy, err = buildPolicyFromSDKFormat(policyRaw, tenantID)
		if err != nil {
			return nil, err
		}
	}

	req := &service.CreatePolicyRequest{
		TenantID: tenantID,
		Policy:   policy,
	}

	resp, err := iamSvc.CreatePolicy(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

// buildPolicyFromSDKFormat constructs an iam.Policy from SDK-format fields.
// The SDK sends {name, spec_yaml, description, enabled, ...} instead of the
// server format {apiVersion, kind, metadata, spec}.
func buildPolicyFromSDKFormat(policyRaw map[string]interface{}, tenantID string) (*iam.Policy, error) {
	name, _ := policyRaw["name"].(string)

	// Check for spec_yaml (base64-encoded YAML from the SDK)
	var specYAML []byte
	if yamlStr, ok := policyRaw["spec_yaml"].(string); ok && yamlStr != "" {
		specYAML = []byte(yamlStr)
	}

	if len(specYAML) > 0 {
		// Try full policy format first
		var fullPolicy iam.Policy
		if parseErr := yaml.Unmarshal(specYAML, &fullPolicy); parseErr == nil && fullPolicy.APIVersion != "" {
			if name != "" {
				fullPolicy.Metadata.Name = name
			}
			fullPolicy.Metadata.Tenant = tenantID
			if desc, ok := policyRaw["description"].(string); ok {
				fullPolicy.Metadata.Description = desc
			}
			return &fullPolicy, nil
		}

		// Try spec-only YAML
		var spec iam.PolicySpec
		if parseErr := yaml.Unmarshal(specYAML, &spec); parseErr != nil {
			return nil, api.NewInvalidFormatError("spec_yaml", parseErr)
		}

		desc, _ := policyRaw["description"].(string)

		return &iam.Policy{
			APIVersion: "iam.quicbaas.io/v1",
			Kind:       "Policy",
			Metadata: iam.PolicyMetadata{
				Name:        name,
				Tenant:      tenantID,
				Description: desc,
			},
			Spec: spec,
		}, nil
	}

	// No spec_yaml - create a minimal policy from the available fields
	desc, _ := policyRaw["description"].(string)
	policy := &iam.Policy{
		APIVersion: "iam.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: iam.PolicyMetadata{
			Name:        name,
			Tenant:      tenantID,
			Description: desc,
		},
	}

	return policy, nil
}

func (r *ToolRegistry) executeIAMPolicyGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	req := &service.GetPolicyRequest{
		TenantID: getString(args, "tenant_id"),
		Name:     getString(args, "name"),
	}

	resp, err := iamSvc.GetPolicy(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMPolicyUpdate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	policyRaw, ok := args["policy"].(map[string]interface{})
	if !ok {
		return nil, api.NewInvalidTypeError("policy", "an object")
	}

	policy := &iam.Policy{}
	if err := parseJSONObject(policyRaw, policy); err != nil {
		return nil, api.NewInvalidFormatError("policy", err)
	}

	req := &service.UpdatePolicyRequest{
		TenantID: getString(args, "tenant_id"),
		Name:     getString(args, "name"),
		Policy:   policy,
	}

	resp, err := iamSvc.UpdatePolicy(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMPolicyDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	req := &service.DeletePolicyRequest{
		TenantID: getString(args, "tenant_id"),
		Name:     getString(args, "name"),
	}

	resp, err := iamSvc.DeletePolicy(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMPolicyList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	ints := newIntArgReader(args)
	limit := ints.IntOr("limit", 100)
	offset := ints.Int("offset")
	if err := ints.Err(); err != nil {
		return nil, err
	}

	req := &service.ListPoliciesRequest{
		TenantID: getString(args, "tenant_id"),
		Limit:    limit,
		Offset:   offset,
	}

	if filterRaw, ok := args["filter"].(map[string]interface{}); ok {
		req.Filter = filterRaw
	}

	resp, err := iamSvc.ListPolicies(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMPolicyTest(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	// Handle simulate format from SDK: flat args with subject/action/resource
	// (sent by Policy.Simulate which maps to this tool).
	if _, hasPolicy := args["policy"]; !hasPolicy {
		return r.executeIAMPolicySimulate(ctx, iamSvc, args)
	}

	policyRaw, ok := args["policy"].(map[string]interface{})
	if !ok {
		return nil, api.NewInvalidTypeError("policy", "an object")
	}

	policy := &iam.Policy{}
	if err := parseJSONObject(policyRaw, policy); err != nil {
		return nil, api.NewInvalidFormatError("policy", err)
	}

	checkRaw, ok := args["check"].(map[string]interface{})
	if !ok {
		return nil, api.NewInvalidTypeError("check", "an object")
	}

	check := &iam.CheckRequest{}
	if err := parseJSONObject(checkRaw, check); err != nil {
		return nil, api.NewInvalidFormatError("check", err)
	}

	req := &service.TestPolicyRequest{
		Policy: policy,
		Check:  check,
	}

	resp, err := iamSvc.TestPolicy(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

// executeIAMPolicySimulate handles the simulate/evaluate format where the SDK
// sends flat fields (tenant_id, subject, action, resource) rather than the
// test format (policy, check). This builds a CheckRequest and evaluates
// against all policies in the tenant.
func (r *ToolRegistry) executeIAMPolicySimulate(ctx context.Context, iamSvc service.IAMService, args map[string]interface{}) (interface{}, error) {
	tenantID := getString(args, "tenant_id")

	// Build subject from args
	subject := iam.Subject{
		TenantID: tenantID,
		Tenant:   tenantID,
	}
	if subjectRaw, ok := args["subject"].(map[string]interface{}); ok {
		if id, ok := subjectRaw["id"].(string); ok {
			subject.ID = id
		}
		if authenticated, ok := subjectRaw["authenticated"].(bool); ok && authenticated {
			if subject.ID == "" {
				subject.ID = "authenticated"
			}
		}
		if rolesRaw, ok := subjectRaw["roles"].([]interface{}); ok {
			for _, r := range rolesRaw {
				if s, ok := r.(string); ok {
					subject.Roles = append(subject.Roles, s)
				}
			}
		}
		if groupsRaw, ok := subjectRaw["groups"].([]interface{}); ok {
			for _, g := range groupsRaw {
				if s, ok := g.(string); ok {
					subject.Groups = append(subject.Groups, s)
				}
			}
		}
	} else if subjectStr, ok := args["subject"].(string); ok {
		subject.ID = subjectStr
	}

	check := &iam.CheckRequest{
		Subject:  subject,
		Action:   getString(args, "action"),
		Resource: getString(args, "resource"),
	}

	if ctxMap, ok := args["context"].(map[string]interface{}); ok {
		check.Context = ctxMap
	}

	req := &service.TestPolicyRequest{
		Check: check,
	}

	resp, err := iamSvc.TestPolicy(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

// =============================================================================
// Execution Handlers - Applications
// =============================================================================

func (r *ToolRegistry) executeIAMAppRegister(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	req := &service.RegisterAppRequest{
		TenantID:       getString(args, "tenant_id"),
		Name:           getString(args, "name"),
		ResourcePrefix: getString(args, "resource_prefix"),
	}

	if urisRaw, ok := args["redirect_uris"].([]interface{}); ok {
		req.RedirectURIs = make([]string, len(urisRaw))
		for i, u := range urisRaw {
			if str, ok := u.(string); ok {
				req.RedirectURIs[i] = str
			}
		}
	}

	if grantTypesRaw, ok := args["grant_types"].([]interface{}); ok {
		req.GrantTypes = make([]string, len(grantTypesRaw))
		for i, g := range grantTypesRaw {
			if str, ok := g.(string); ok {
				req.GrantTypes[i] = str
			}
		}
	}

	if respTypesRaw, ok := args["response_types"].([]interface{}); ok {
		req.ResponseTypes = make([]string, len(respTypesRaw))
		for i, rt := range respTypesRaw {
			if str, ok := rt.(string); ok {
				req.ResponseTypes[i] = str
			}
		}
	}

	resp, err := iamSvc.RegisterApp(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMAppGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	req := &service.GetAppRequest{
		ClientID: getString(args, "client_id"),
	}

	resp, err := iamSvc.GetApp(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMAppUpdate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	req := &service.UpdateAppRequest{
		ClientID: getString(args, "client_id"),
	}

	if name := getString(args, "name"); name != "" {
		req.Name = &name
	}
	if prefix := getString(args, "resource_prefix"); prefix != "" {
		req.ResourcePrefix = &prefix
	}

	if urisRaw, ok := args["redirect_uris"].([]interface{}); ok {
		uris := make([]string, len(urisRaw))
		for i, u := range urisRaw {
			if str, ok := u.(string); ok {
				uris[i] = str
			}
		}
		req.RedirectURIs = &uris
	}

	if grantTypesRaw, ok := args["grant_types"].([]interface{}); ok {
		grantTypes := make([]string, len(grantTypesRaw))
		for i, g := range grantTypesRaw {
			if str, ok := g.(string); ok {
				grantTypes[i] = str
			}
		}
		req.GrantTypes = &grantTypes
	}

	if respTypesRaw, ok := args["response_types"].([]interface{}); ok {
		respTypes := make([]string, len(respTypesRaw))
		for i, rt := range respTypesRaw {
			if str, ok := rt.(string); ok {
				respTypes[i] = str
			}
		}
		req.ResponseTypes = &respTypes
	}

	resp, err := iamSvc.UpdateApp(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMAppDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	req := &service.DeleteAppRequest{
		ClientID: getString(args, "client_id"),
	}

	resp, err := iamSvc.DeleteApp(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMAppList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	ints := newIntArgReader(args)
	limit := ints.IntOr("limit", 100)
	offset := ints.Int("offset")
	if err := ints.Err(); err != nil {
		return nil, err
	}

	req := &service.ListAppsRequest{
		TenantID: getString(args, "tenant_id"),
		Limit:    limit,
		Offset:   offset,
	}

	if filterRaw, ok := args["filter"].(map[string]interface{}); ok {
		req.Filter = filterRaw
	}

	resp, err := iamSvc.ListApps(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIAMAppRotateSecret(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	clientID := getString(args, "client_id")
	req := &service.RotateAppSecretRequest{
		ClientID: clientID,
	}

	resp, err := iamSvc.RotateAppSecret(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{
		"client_id":     clientID,
		"client_secret": resp.ClientSecret,
		"timestamp":     resp.Timestamp,
	})
}

// =============================================================================
// Policy Validate Handler
// =============================================================================

func (r *ToolRegistry) executeIAMPolicyValidate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	iamSvc, err := r.getIAMService()
	if err != nil {
		return nil, err
	}

	policyRaw, ok := args["policy"].(map[string]interface{})
	if !ok {
		return nil, api.NewInvalidTypeError("policy", "an object")
	}

	tenantID := getString(args, "tenant_id")

	// Try direct parse as server-format policy first
	policy := &iam.Policy{}
	if parseErr := parseJSONObject(policyRaw, policy); parseErr != nil {
		return nil, api.NewInvalidFormatError("policy", parseErr)
	}

	// If the policy doesn't have apiVersion, try parsing from SDK format
	if policy.APIVersion == "" {
		policy, err = buildPolicyFromSDKFormat(policyRaw, tenantID)
		if err != nil {
			return nil, err
		}
	}

	req := &service.ValidatePolicyRequest{
		Policy: policy,
	}

	resp, err := iamSvc.ValidatePolicy(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

// =============================================================================
// Audit Query/Get/Delete Handlers
// =============================================================================

// auditLoggerAccessor provides access to the audit logger from the QuicBaaS service.
type auditLoggerAccessor interface {
	GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
}

// getAuditLogger retrieves the audit logger from the QuicBaaS service.
func (r *ToolRegistry) getAuditLogger(ctx context.Context) (iam.AuditLogger, error) {
	accessor, ok := r.service.(auditLoggerAccessor)
	if !ok {
		return nil, api.NewServiceUnavailableError("audit")
	}
	logger, err := accessor.GetAuditLogger(ctx)
	if err != nil {
		return nil, api.NewOperationFailedError("get audit logger", err)
	}
	if logger == nil {
		return nil, api.NewServiceUnavailableError("audit")
	}
	return logger, nil
}

func (r *ToolRegistry) executeIAMAuditQuery(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	auditLogger, err := r.getAuditLogger(ctx)
	if err != nil {
		return nil, err
	}

	filter := &iam.AuditFilter{
		TenantID: getString(args, "tenant_id"),
	}

	if category := getString(args, "category"); category != "" {
		filter.Categories = []iam.AuditCategory{iam.AuditCategory(category)}
	}
	if outcome := getString(args, "outcome"); outcome != "" {
		filter.Outcomes = []iam.AuditOutcome{iam.AuditOutcome(outcome)}
	}
	if actorID := getString(args, "actor_id"); actorID != "" {
		filter.ActorID = actorID
	}
	if resourceID := getString(args, "resource_id"); resourceID != "" {
		filter.ResourceID = resourceID
	}

	ints := newIntArgReader(args)
	limit := ints.IntOr("limit", 100)
	offset := ints.Int("offset")
	if err := ints.Err(); err != nil {
		return nil, err
	}
	filter.Limit = limit
	filter.Offset = offset

	if startTime := getString(args, "start_time"); startTime != "" {
		if t, parseErr := time.Parse(time.RFC3339, startTime); parseErr == nil {
			filter.StartTime = &t
		}
	}
	if endTime := getString(args, "end_time"); endTime != "" {
		if t, parseErr := time.Parse(time.RFC3339, endTime); parseErr == nil {
			filter.EndTime = &t
		}
	}

	events, total, err := auditLogger.Query(ctx, filter)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"events":    events,
		"total":     total,
		"timestamp": time.Now(),
	})
}

func (r *ToolRegistry) executeIAMAuditGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	auditLogger, err := r.getAuditLogger(ctx)
	if err != nil {
		return nil, err
	}

	tenantID := getString(args, "tenant_id")
	eventID := getString(args, "event_id")

	event, err := auditLogger.GetEvent(ctx, tenantID, eventID)
	if err != nil {
		return nil, &AuditEventGetError{EventID: eventID, Cause: err}
	}
	return marshalResult(event)
}

func (r *ToolRegistry) executeIAMAuditDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	auditLogger, err := r.getAuditLogger(ctx)
	if err != nil {
		return nil, err
	}

	tenantID := getString(args, "tenant_id")
	beforeStr := getString(args, "before")

	before, parseErr := time.Parse(time.RFC3339, beforeStr)
	if parseErr != nil {
		return nil, api.NewInvalidFormatError("before", parseErr)
	}

	deleted, err := auditLogger.DeleteOlderThan(ctx, tenantID, before)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"deleted":   deleted,
		"timestamp": time.Now(),
	})
}

// =============================================================================
// Audit Tools
// =============================================================================

func (r *ToolRegistry) registerAuditTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_audit_query",
		Description: "Query audit events with filters",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID to query audit events for",
				},
				"category": map[string]interface{}{
					"type":        "string",
					"description": "Filter by audit category (authentication, authorization, management, data_access, security)",
				},
				"outcome": map[string]interface{}{
					"type":        "string",
					"description": "Filter by outcome (success, failure, error)",
				},
				"actor_id": map[string]interface{}{
					"type":        "string",
					"description": "Filter by actor user ID",
				},
				"resource_id": map[string]interface{}{
					"type":        "string",
					"description": "Filter by resource ID",
				},
				"start_time": map[string]interface{}{
					"type":        "string",
					"description": "Start time in RFC3339 format",
					"format":      "date-time",
				},
				"end_time": map[string]interface{}{
					"type":        "string",
					"description": "End time in RFC3339 format",
					"format":      "date-time",
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
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_audit_get",
		Description: "Get a specific audit event by ID",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"event_id": map[string]interface{}{
					"type":        "string",
					"description": "Audit event ID",
				},
			},
			"required": []string{"tenant_id", "event_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_audit_delete",
		Description: "Delete audit events older than a specified time",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"before": map[string]interface{}{
					"type":        "string",
					"description": "Delete events before this time (RFC3339 format)",
					"format":      "date-time",
				},
			},
			"required": []string{"tenant_id", "before"},
		},
	})
}

// convertStringNumerics converts string values that look like numbers to their
// numeric equivalents. This is needed when the SDK sends settings as
// map[string]string but the server struct expects int/bool fields.
func convertStringNumerics(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		if strVal, ok := v.(string); ok {
			// Try integer
			if intVal, err := strconv.Atoi(strVal); err == nil {
				result[k] = intVal
				continue
			}
			// Try boolean
			if strVal == "true" {
				result[k] = true
				continue
			}
			if strVal == "false" {
				result[k] = false
				continue
			}
		}
		result[k] = v
	}
	return result
}

// AuditEventGetError indicates a failure to retrieve an audit event by ID.
type AuditEventGetError struct {
	EventID string
	Cause   error
}

// Error implements the error interface.
func (e *AuditEventGetError) Error() string {
	return "audit event " + e.EventID + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is/As support.
func (e *AuditEventGetError) Unwrap() error {
	return e.Cause
}

// stringSlice keeps the string members of a JSON array, which is what every
// enum-valued MCP array field carries.
func stringSlice(in []interface{}) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if str, ok := v.(string); ok {
			out = append(out, str)
		}
	}
	return out
}

// stringAttributes converts an MCP attribute object into the store's
// string-valued map, REFUSING any value that is not a string.
//
// The alternative — keeping the strings and discarding the rest — accepts a
// request and persists less than it asked for. For authorization attributes
// that is not a lossy convenience: a policy evaluated over the subset that
// survived is a different policy from the one the caller wrote, and nothing in
// the response says so.
func stringAttributes(in map[string]interface{}) (map[string]string, error) {
	out := make(map[string]string, len(in))
	for k, v := range in {
		str, ok := v.(string)
		if !ok {
			return nil, api.NewInvalidInputError(
				"attributes." + k + " must be a string; attribute values are stored as strings")
		}
		out[k] = str
	}
	return out, nil
}

// TenantEngineObjectError indicates a malformed engine provisioning object on
// a tenant-create request. It is a distinct type because the alternative —
// ignoring the field — turns an explicit override into engine defaults, and a
// mistyped key into an all-defaults override, neither of which the caller
// asked for.
type TenantEngineObjectError struct {
	Field  string
	Reason string
	Cause  error
}

// Error implements the error interface.
func (e *TenantEngineObjectError) Error() string {
	msg := "iam.tenant.create: " + e.Field + ": " + e.Reason
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying cause for errors.Is/As support.
func (e *TenantEngineObjectError) Unwrap() error {
	return e.Cause
}

// decodeTenantEngineObject strictly decodes one engine provisioning object out
// of the tool arguments into dst, reporting whether the caller supplied it.
//
// Decoding is strict about unknown fields on purpose: these objects are
// overrides whose ABSENCE means "keep the engine's configured defaults", so a
// mistyped key (max_databasses) would otherwise decode to an empty override
// and quietly replace the engine's defaults with an all-defaults, all-denied
// set. Numeric strings are normalized first because a model frequently quotes
// its numbers, and dropping those would zero every limit just as silently.
func decodeTenantEngineObject(args map[string]interface{}, field string, dst interface{}) (bool, error) {
	raw, present := args[field]
	if !present || raw == nil {
		return false, nil
	}
	obj, ok := raw.(map[string]interface{})
	if !ok {
		return false, &TenantEngineObjectError{Field: field, Reason: "expected a JSON object"}
	}

	data, err := json.Marshal(convertStringNumerics(obj))
	if err != nil {
		return false, &TenantEngineObjectError{Field: field, Reason: "is not encodable", Cause: err}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return false, &TenantEngineObjectError{Field: field, Reason: "does not match the expected shape", Cause: err}
	}
	return true, nil
}
