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

// Package mcp implements Parameter Store tool definitions for the Model Context Protocol.
package mcp

// Parameter Store tool definitions for hierarchical parameter management (8 tools).
// Each tool follows the MCP specification with JSON schema validation.

var (
	// ParameterGetTool retrieves a single parameter by path.
	ParameterGetTool = Tool{
		Name:        "quicbaas_parameter_get",
		Description: "Get a parameter by path with optional decryption",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path": {
					Type:        "string",
					Description: "Parameter path (e.g., /app/database/host)",
				},
				"with_decryption": {
					Type:        "string",
					Description: "Decrypt the value if encrypted (true/false, default: true)",
					Default:     "true",
				},
			},
			Required: []string{"path"},
		},
	}

	// ParameterSetTool creates or updates a parameter.
	ParameterSetTool = Tool{
		Name:        "quicbaas_parameter_set",
		Description: "Create or update a parameter with optional encryption, TTL, and tags",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path": {
					Type:        "string",
					Description: "Parameter path (e.g., /app/database/host)",
				},
				"value": {
					Type:        "string",
					Description: "Parameter value (base64-encoded for binary data)",
				},
				"type": {
					Type:        "string",
					Description: "Value type (string, number, json, binary)",
					Default:     "string",
				},
				"description": {
					Type:        "string",
					Description: "Optional parameter description",
				},
				"kms_key_id": {
					Type:        "string",
					Description: "KMS key ID to encrypt the parameter",
				},
				"tags": {
					Type:        "string",
					Description: "JSON object of key-value tags",
				},
				"ttl_seconds": {
					Type:        "string",
					Description: "Time-to-live in seconds (0 = no expiration)",
					Default:     "0",
				},
				"audit_enabled": {
					Type:        "string",
					Description: "Enable audit logging for this parameter (true/false)",
					Default:     "false",
				},
				"overwrite": {
					Type:        "string",
					Description: "Allow updating existing parameters (true/false, default: true)",
					Default:     "true",
				},
			},
			Required: []string{"path", "value"},
		},
	}

	// ParameterDeleteTool removes a parameter by path.
	ParameterDeleteTool = Tool{
		Name:        "quicbaas_parameter_delete",
		Description: "Delete a parameter by path",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path": {
					Type:        "string",
					Description: "Parameter path to delete",
				},
			},
			Required: []string{"path"},
		},
	}

	// ParameterListTool lists parameters by prefix.
	ParameterListTool = Tool{
		Name:        "quicbaas_parameter_list",
		Description: "List parameters matching a path prefix with optional pagination",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"prefix": {
					Type:        "string",
					Description: "Path prefix to match (e.g., /app/)",
				},
				"recursive": {
					Type:        "string",
					Description: "Include nested paths (true/false, default: true)",
					Default:     "true",
				},
				"with_decryption": {
					Type:        "string",
					Description: "Decrypt values if encrypted (true/false, default: false)",
					Default:     "false",
				},
				"with_values": {
					Type:        "string",
					Description: "Include parameter values in response (true/false, default: false)",
					Default:     "false",
				},
				"limit": {
					Type:        "string",
					Description: "Maximum number of results (default: 100)",
					Default:     "100",
				},
				"cursor": {
					Type:        "string",
					Description: "Pagination cursor from previous response",
				},
			},
			Required: []string{"prefix"},
		},
	}

	// ParameterBatchGetTool retrieves multiple parameters by paths.
	ParameterBatchGetTool = Tool{
		Name:        "quicbaas_parameter_batch_get",
		Description: "Batch get multiple parameters by paths",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"paths": {
					Type:        "string",
					Description: "JSON array of parameter paths to retrieve",
				},
				"with_decryption": {
					Type:        "string",
					Description: "Decrypt values if encrypted (true/false, default: true)",
					Default:     "true",
				},
			},
			Required: []string{"paths"},
		},
	}

	// ParameterBatchSetTool creates or updates multiple parameters atomically.
	ParameterBatchSetTool = Tool{
		Name:        "quicbaas_parameter_batch_set",
		Description: "Batch set multiple parameters atomically",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"parameters": {
					Type:        "string",
					Description: "JSON array of parameter objects with path, value, type, description, kms_key_id, tags, ttl_seconds, audit_enabled, overwrite",
				},
			},
			Required: []string{"parameters"},
		},
	}

	// ParameterBatchDeleteTool removes multiple parameters by paths.
	ParameterBatchDeleteTool = Tool{
		Name:        "quicbaas_parameter_batch_delete",
		Description: "Batch delete multiple parameters by paths",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"paths": {
					Type:        "string",
					Description: "JSON array of parameter paths to delete",
				},
			},
			Required: []string{"paths"},
		},
	}

	// ParameterSearchTool finds parameters matching tag key-value pairs.
	ParameterSearchTool = Tool{
		Name:        "quicbaas_parameter_search",
		Description: "Search parameters by tags with optional path prefix filtering",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"tags": {
					Type:        "string",
					Description: "JSON object of required tag key-value pairs (ANDed together)",
				},
				"prefix": {
					Type:        "string",
					Description: "Optional path prefix to filter results",
				},
				"with_values": {
					Type:        "string",
					Description: "Include parameter values in response (true/false, default: false)",
					Default:     "false",
				},
				"with_decryption": {
					Type:        "string",
					Description: "Decrypt values if encrypted (true/false, default: false)",
					Default:     "false",
				},
				"limit": {
					Type:        "string",
					Description: "Maximum number of results (default: 100)",
					Default:     "100",
				},
				"cursor": {
					Type:        "string",
					Description: "Pagination cursor from previous response",
				},
			},
			Required: []string{"tags"},
		},
	}
)

// getAllParameterTools returns all Parameter Store tools (8 tools).
func getAllParameterTools() []Tool {
	return []Tool{
		ParameterGetTool,
		ParameterSetTool,
		ParameterDeleteTool,
		ParameterListTool,
		ParameterBatchGetTool,
		ParameterBatchSetTool,
		ParameterBatchDeleteTool,
		ParameterSearchTool,
	}
}
