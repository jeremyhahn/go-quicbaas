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

// Package mcp implements Cluster DEK tool definitions for the Model Context Protocol.
package mcp

// Cluster DEK tool definitions for cluster-wide Data Encryption Key management (9 tools).
// Each tool follows the MCP specification with JSON schema validation.

var (
	// ClusterDEKInitTool initializes the cluster DEK.
	ClusterDEKInitTool = Tool{
		Name:        "quicbaas_cluster_dek_init",
		Description: "Initialize the cluster-wide Data Encryption Key (DEK) for encrypting data at rest",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"algorithm": {
					Type:        "string",
					Description: "Encryption algorithm (e.g., AES-256-GCM)",
				},
				"key_size": {
					Type:        "string",
					Description: "Key size in bits (e.g., 256)",
				},
				"rotation_enabled": {
					Type:        "string",
					Description: "Set to 'true' to enable automatic rotation",
				},
				"rotation_interval": {
					Type:        "string",
					Description: "Interval for automatic rotation (e.g., 720h for 30 days)",
				},
				"auto_reencrypt": {
					Type:        "string",
					Description: "Set to 'true' to automatically start re-encryption after rotation",
				},
			},
			Required: []string{"algorithm", "key_size"},
		},
	}

	// ClusterDEKStatusTool retrieves the cluster DEK status.
	ClusterDEKStatusTool = Tool{
		Name:        "quicbaas_cluster_dek_status",
		Description: "Get the current status of the cluster DEK including version, sync status, and re-encryption progress",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{},
			Required:   []string{},
		},
	}

	// ClusterDEKRotateTool rotates the cluster DEK.
	ClusterDEKRotateTool = Tool{
		Name:        "quicbaas_cluster_dek_rotate",
		Description: "Rotate the cluster DEK to a new version and optionally start re-encryption",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"reason": {
					Type:        "string",
					Description: "Reason for rotation (for audit trail)",
				},
				"scheduled_time": {
					Type:        "string",
					Description: "RFC3339 timestamp to schedule rotation (immediate if not specified)",
				},
				"force_rotation": {
					Type:        "string",
					Description: "Set to 'true' to skip safety checks",
				},
				"auto_start_reencryption": {
					Type:        "string",
					Description: "Set to 'true' to automatically start re-encryption after rotation",
				},
			},
			Required: []string{"reason"},
		},
	}

	// ClusterDEKSyncNodeTool synchronizes the DEK to a node.
	ClusterDEKSyncNodeTool = Tool{
		Name:        "quicbaas_cluster_dek_sync_node",
		Description: "Synchronize the cluster DEK to a specific node",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"node_id": {
					Type:        "string",
					Description: "ID of the node to sync DEK to",
				},
				"current_version": {
					Type:        "string",
					Description: "Current DEK version on the node (0 if none)",
				},
				"force_resync": {
					Type:        "string",
					Description: "Set to 'true' to force sync even if version matches",
				},
			},
			Required: []string{"node_id"},
		},
	}

	// ClusterDEKNodeStatusTool retrieves the DEK status for a specific node.
	ClusterDEKNodeStatusTool = Tool{
		Name:        "quicbaas_cluster_dek_node_status",
		Description: "Get the DEK synchronization status for a specific node",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"node_id": {
					Type:        "string",
					Description: "ID of the node to get status for",
				},
			},
			Required: []string{"node_id"},
		},
	}

	// ClusterDEKStartReencryptionTool starts the re-encryption process.
	ClusterDEKStartReencryptionTool = Tool{
		Name:        "quicbaas_cluster_dek_start_reencryption",
		Description: "Start re-encryption of data with the new cluster DEK",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path": {
					Type:        "string",
					Description: "Optional DEK path to re-encrypt (all DEKs if empty)",
				},
				"from_version": {
					Type:        "string",
					Description: "Source DEK version to re-encrypt from",
				},
				"to_version": {
					Type:        "string",
					Description: "Target DEK version to re-encrypt to",
				},
				"batch_size": {
					Type:        "string",
					Description: "Number of entities to process per batch",
				},
				"concurrency": {
					Type:        "string",
					Description: "Number of parallel re-encryption workers",
				},
				"throttle_rate": {
					Type:        "string",
					Description: "Maximum entities per second (0 for unlimited)",
				},
				"force": {
					Type:        "string",
					Description: "Set to 'true' to force re-encryption even if already in progress",
				},
			},
			Required: []string{"from_version", "to_version"},
		},
	}

	// ClusterDEKReencryptionStatusTool retrieves the re-encryption status.
	ClusterDEKReencryptionStatusTool = Tool{
		Name:        "quicbaas_cluster_dek_reencryption_status",
		Description: "Get the current re-encryption progress and status",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{},
			Required:   []string{},
		},
	}

	// ClusterDEKCancelReencryptionTool cancels the re-encryption process.
	ClusterDEKCancelReencryptionTool = Tool{
		Name:        "quicbaas_cluster_dek_cancel_reencryption",
		Description: "Cancel the ongoing re-encryption process gracefully",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{},
			Required:   []string{},
		},
	}

	// ClusterDEKRecoverTool recovers the cluster DEK.
	ClusterDEKRecoverTool = Tool{
		Name:        "quicbaas_cluster_dek_recover",
		Description: "Recover the cluster DEK using recovery keys or backup",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"recovery_method": {
					Type:        "string",
					Description: "Recovery method: recovery_keys, backup, or manual",
					Enum:        []string{"recovery_keys", "backup", "manual"},
				},
				"recovery_keys": {
					Type:        "string",
					Description: "JSON array of base64-encoded recovery keys (for recovery_keys method)",
				},
				"backup_location": {
					Type:        "string",
					Description: "Path or URL to encrypted DEK backup (for backup method)",
				},
				"manual_key": {
					Type:        "string",
					Description: "Base64-encoded DEK for manual injection (for manual method)",
				},
				"reason": {
					Type:        "string",
					Description: "Reason for recovery (for audit trail)",
				},
			},
			Required: []string{"recovery_method", "reason"},
		},
	}
)

// getAllClusterDEKTools returns all Cluster DEK tools (9 tools).
func getAllClusterDEKTools() []Tool {
	return []Tool{
		ClusterDEKInitTool,
		ClusterDEKStatusTool,
		ClusterDEKRotateTool,
		ClusterDEKSyncNodeTool,
		ClusterDEKNodeStatusTool,
		ClusterDEKStartReencryptionTool,
		ClusterDEKReencryptionStatusTool,
		ClusterDEKCancelReencryptionTool,
		ClusterDEKRecoverTool,
	}
}
