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

// Package mcp implements Dynamic Secrets tool definitions for the Model Context Protocol.
package mcp

// Dynamic Secrets tool definitions for credential role and lease management (8 tools).
// Each tool follows the MCP specification with JSON schema validation.

var (
	// DynamicSecretsCreateRoleTool creates a new credential generation role.
	DynamicSecretsCreateRoleTool = Tool{
		Name:        "quicbaas_dynamic_secrets_create_role",
		Description: "Create a new credential generation role with specified generator type and TTL settings",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"name": {
					Type:        "string",
					Description: "Unique name for the credential role",
				},
				"generator_type": {
					Type:        "string",
					Description: "Type of credential generator (e.g., postgres, mysql, mongodb)",
				},
				"config": {
					Type:        "string",
					Description: "JSON object containing generator-specific configuration",
				},
				"default_ttl": {
					Type:        "string",
					Description: "Default time-to-live for generated credentials (e.g., 1h, 24h)",
				},
				"max_ttl": {
					Type:        "string",
					Description: "Maximum allowed time-to-live for credentials (e.g., 72h, 168h)",
				},
			},
			Required: []string{"name", "generator_type", "default_ttl", "max_ttl"},
		},
	}

	// DynamicSecretsGetRoleTool retrieves a credential role by name.
	DynamicSecretsGetRoleTool = Tool{
		Name:        "quicbaas_dynamic_secrets_get_role",
		Description: "Get details of a credential generation role",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"name": {
					Type:        "string",
					Description: "Name of the role to retrieve",
				},
			},
			Required: []string{"name"},
		},
	}

	// DynamicSecretsListRolesTool lists all credential roles.
	DynamicSecretsListRolesTool = Tool{
		Name:        "quicbaas_dynamic_secrets_list_roles",
		Description: "List all credential generation roles with optional prefix filtering",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"prefix": {
					Type:        "string",
					Description: "Optional prefix to filter roles by name",
				},
			},
			Required: []string{},
		},
	}

	// DynamicSecretsDeleteRoleTool deletes a credential role.
	DynamicSecretsDeleteRoleTool = Tool{
		Name:        "quicbaas_dynamic_secrets_delete_role",
		Description: "Delete a credential generation role (does not revoke existing credentials)",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"name": {
					Type:        "string",
					Description: "Name of the role to delete",
				},
			},
			Required: []string{"name"},
		},
	}

	// DynamicSecretsGenerateTool generates a dynamic credential.
	DynamicSecretsGenerateTool = Tool{
		Name:        "quicbaas_dynamic_secrets_generate",
		Description: "Generate a new dynamic credential based on a role",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"role_name": {
					Type:        "string",
					Description: "Name of the role to use for credential generation",
				},
				"ttl": {
					Type:        "string",
					Description: "Optional TTL for the credential (e.g., 1h, 30m). Uses role default if not specified",
				},
			},
			Required: []string{"role_name"},
		},
	}

	// DynamicSecretsRenewLeaseTool renews a credential lease.
	DynamicSecretsRenewLeaseTool = Tool{
		Name:        "quicbaas_dynamic_secrets_renew_lease",
		Description: "Renew a credential lease to extend its expiration time",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"lease_id": {
					Type:        "string",
					Description: "ID of the lease to renew",
				},
				"increment": {
					Type:        "string",
					Description: "Optional duration to add to expiration (e.g., 1h, 30m). Uses original TTL if not specified",
				},
			},
			Required: []string{"lease_id"},
		},
	}

	// DynamicSecretsRevokeLeaseTool revokes a credential lease.
	DynamicSecretsRevokeLeaseTool = Tool{
		Name:        "quicbaas_dynamic_secrets_revoke_lease",
		Description: "Revoke a credential lease immediately, removing the credential from the backend system",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"lease_id": {
					Type:        "string",
					Description: "ID of the lease to revoke",
				},
			},
			Required: []string{"lease_id"},
		},
	}

	// DynamicSecretsListLeasesTool lists active leases.
	DynamicSecretsListLeasesTool = Tool{
		Name:        "quicbaas_dynamic_secrets_list_leases",
		Description: "List all active credential leases with optional prefix filtering",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"prefix": {
					Type:        "string",
					Description: "Optional prefix to filter leases by path",
				},
			},
			Required: []string{},
		},
	}
)

// getAllDynamicSecretsTools returns all Dynamic Secrets tools (8 tools).
func getAllDynamicSecretsTools() []Tool {
	return []Tool{
		DynamicSecretsCreateRoleTool,
		DynamicSecretsGetRoleTool,
		DynamicSecretsListRolesTool,
		DynamicSecretsDeleteRoleTool,
		DynamicSecretsGenerateTool,
		DynamicSecretsRenewLeaseTool,
		DynamicSecretsRevokeLeaseTool,
		DynamicSecretsListLeasesTool,
	}
}
