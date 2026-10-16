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

// Package mcp provides MCP tools that expose QuicBaaS operations.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/id"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// ToolRegistry manages MCP tools and their execution.
type ToolRegistry struct {
	service service.QuicBaaS
	logger  *logger.Logger
	tools   []ToolDefinition

	// barrierSvc backs every quicbaas_barrier_* tool. Wired
	// post-construction via SetBarrierService once the qrdb SDK BarrierClient
	// is online; nil during early boot and unit tests, in which case the
	// barrier dispatcher returns BarrierServiceUnavailableError.
	barrierSvc service.BarrierServicer

	// ceremonySvc backs every quicbaas_ceremony_* tool. Wired
	// post-construction via SetCeremonyService once the qrdb SDK
	// CeremonyClient is online; nil during early boot and unit tests, in
	// which case the ceremony dispatcher returns
	// CeremonyServiceUnavailableError.
	ceremonySvc service.CeremonyServicer

	// credentialSvc backs every qrdb_credential_* tool. Wired
	// post-construction via SetCredentialService once the qrdb credential
	// service is online; nil during early boot and unit tests, in which
	// case the credential dispatcher returns CredentialServiceUnavailableError.
	credentialSvc qrdbsdk.ServiceCredentialServicer

	// tenantBarrierSvc backs every qrdb_tenant_barrier_* tool. Wired
	// post-construction via SetTenantBarrierService once the qrdb SDK
	// TenantClient is online; nil during early boot and unit tests, in which
	// case the tenant barrier dispatcher returns
	// TenantBarrierServiceUnavailableError.
	tenantBarrierSvc service.TenantBarrierServicer

	// tenantGovernance backs qrdb_tenant_update_quotas and
	// qrdb_tenant_update_entitlements. Wired post-construction via
	// SetTenantGovernance once the iam.DefaultTenantManager is online; nil
	// during early boot and unit tests, in which case those tools return
	// GovernanceServiceUnavailableError.
	tenantGovernance mcpTenantGovernancer

	// userSvc backs the qrdb_user_get* lookup tools. Wired post-construction
	// via SetUserService with the lazy per-call embedded-node proxy; nil
	// during early boot and unit tests, in which case those tools return
	// UserServiceUnavailableError.
	userSvc qrdbsdk.UserServicer

	// userAuthFactorSvc backs every qrdb_user_authfactor_* tool. Wired
	// post-construction via SetUserAuthFactorService with the lazy per-call
	// embedded-node proxy; nil during early boot and unit tests, in which
	// case those tools return UserAuthFactorServiceUnavailableError.
	userAuthFactorSvc qrdbsdk.UserAuthFactorServicer
}

// ToolDefinition describes an MCP tool.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// TransactionParseError represents an error parsing transaction arguments.
type TransactionParseError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *TransactionParseError) Error() string {
	return fmt.Sprintf("transaction parse error: %s: %s", e.Field, e.Message)
}

// TransactionInvalidCompareTargetError represents an invalid compare target.
type TransactionInvalidCompareTargetError struct {
	Target string
}

// Error implements the error interface.
func (e *TransactionInvalidCompareTargetError) Error() string {
	return fmt.Sprintf("invalid compare target: %s", e.Target)
}

// TransactionInvalidCompareOperatorError represents an invalid compare operator.
type TransactionInvalidCompareOperatorError struct {
	Operator string
}

// Error implements the error interface.
func (e *TransactionInvalidCompareOperatorError) Error() string {
	return fmt.Sprintf("invalid compare operator: %s", e.Operator)
}

// TransactionInvalidOpTypeError represents an invalid transaction operation type.
type TransactionInvalidOpTypeError struct {
	OpType string
}

// Error implements the error interface.
func (e *TransactionInvalidOpTypeError) Error() string {
	return fmt.Sprintf("invalid transaction operation type: %s", e.OpType)
}

// BatchOperationsEmptyError represents an error when batch operations are empty.
type BatchOperationsEmptyError struct{}

// Error implements the error interface.
func (e *BatchOperationsEmptyError) Error() string {
	return "batch operations cannot be empty"
}

// BatchOperationsInvalidError represents an error when the operations field is not a valid array.
type BatchOperationsInvalidError struct{}

// Error implements the error interface.
func (e *BatchOperationsInvalidError) Error() string {
	return "operations must be a valid array"
}

// BatchOperationParseError represents an error parsing an individual batch operation.
type BatchOperationParseError struct {
	Index   int
	Field   string
	Message string
}

// Error implements the error interface.
func (e *BatchOperationParseError) Error() string {
	return fmt.Sprintf("batch operation[%d] parse error: %s: %s", e.Index, e.Field, e.Message)
}

// NewToolRegistry creates a new tool registry.
func NewToolRegistry(svc service.QuicBaaS, log *logger.Logger) *ToolRegistry {
	r := &ToolRegistry{
		service: svc,
		logger:  log,
		tools:   make([]ToolDefinition, 0, 100),
	}

	// Register all tools
	r.registerTools()

	return r
}

// registerTools registers all available QuicBaaS operations as MCP tools.
func (r *ToolRegistry) registerTools() {
	// Register security service tools
	r.registerSealTools()
	r.registerKMSTools()
	r.registerKMSOpTools()
	r.registerKMSExportTools()
	r.registerSecretsTools()
	r.registerCryptoTools()
	r.registerDEKTools()

	// Register time series tools
	r.registerTimeSeriesTools()

	// Register object storage tools
	r.registerObjectStorageTools()

	// Register object storage admin tools
	r.registerObjectStorageAdminTools()

	// Register object transfer tools
	r.registerObjectTransferTools()

	// Register IAM tools
	r.registerIAMTools()

	// Register Ollama tools
	r.registerOllamaTools()

	// Register RAG tools
	r.registerRAGTools()

	// Register Embedding tools
	r.registerEmbeddingTools()

	// Register Certificate tools
	r.registerCertificateTools()
	r.registerTrustTools()
	r.registerAttestationTools()
	r.registerRegistryTools()

	// Register Dynamic Secrets tools
	r.registerDynamicSecretsTools()

	// Register Parameter Store tools
	r.registerParameterTools()

	// Register Project tools
	r.registerProjectTools()
	r.registerSDLCTools()
	r.registerPromptTools()

	// Register pooled provider account tools
	r.registerAccountTools()
	r.registerCostSettingsTools()
	r.registerAgentCatalogTools()
	r.registerSSHTools()

	// Register Scheduler tools
	r.registerSchedulerTools()
	r.registerPlacementTools()

	// Register Messaging tools
	r.registerMsgTools()

	// Register Vector Database tools
	r.registerVectorTools()

	// Register RAG Indexing tools
	r.registerRAGIndexTools()

	// Register Agent Orchestration tools
	r.registerAgentTools()

	// Register Agent Cache tools
	r.registerAgentCacheTools()

	// Register Agent Admin tools (global disable/enable)
	r.registerFleetAdminTools()

	// Register Barrier lifecycle tools (mirrors REST /api/v1/barrier surface)
	r.registerBarrierTools()

	// Register Ceremony lifecycle tools (mirrors REST /api/v1/init surface)
	r.registerCeremonyTools()

	// Register unified login ceremony tools
	r.registerLoginTools()

	// Register qrdb credential management tools (qrdb_credential_*).
	r.registerCredentialTools()

	// Register per-tenant barrier tools (qrdb_tenant_barrier_*).
	r.registerTenantBarrierTools()

	// Register qrdb user lookup + user-login factor tools
	// (qrdb_user_get* / qrdb_user_authfactor_*).
	r.registerQRDBUserTools()

	// Register XKMS-compatible tools

	r.registerXKMSTools()
	// KV Operations
	r.addTool(ToolDefinition{
		Name:        "qrdb_put",
		Description: "Store a key-value pair in QuicBaaS with optional consistency guarantees",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"entityType": map[string]interface{}{"type": "string", "description": "Entity type"},
				"id":         map[string]interface{}{"type": "integer", "description": "Entity ID"},
				"data":       map[string]interface{}{"type": "string", "description": "Base64-encoded data"},
				"shardID":    map[string]interface{}{"type": "integer", "description": "Target shard ID"},
			},
			"required": []string{"entityType", "id", "data"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_get",
		Description: "Retrieve a value by key from QuicBaaS",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"entityType": map[string]interface{}{"type": "string", "description": "Entity type"},
				"id":         map[string]interface{}{"type": "integer", "description": "Entity ID"},
				"shardID":    map[string]interface{}{"type": "integer", "description": "Target shard ID"},
			},
			"required": []string{"entityType", "id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_delete",
		Description: "Delete a key-value pair from QuicBaaS",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"entityType": map[string]interface{}{"type": "string", "description": "Entity type"},
				"id":         map[string]interface{}{"type": "integer", "description": "Entity ID"},
				"shardID":    map[string]interface{}{"type": "integer", "description": "Target shard ID"},
			},
			"required": []string{"entityType", "id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_list",
		Description: "List all entities of a specific type with pagination",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"entityType": map[string]interface{}{"type": "string", "description": "Entity type"},
				"pageSize":   map[string]interface{}{"type": "integer", "description": "Page size", "default": 100},
				"pageToken":  map[string]interface{}{"type": "string", "description": "Pagination token"},
				"shardID":    map[string]interface{}{"type": "integer", "description": "Target shard ID"},
			},
			"required": []string{"entityType"},
		},
	})

	// Batch Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_batch_put",
		Description: "Perform multiple Put operations atomically",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operations": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"entityType": map[string]interface{}{"type": "string"},
							"id":         map[string]interface{}{"type": "integer"},
							"data":       map[string]interface{}{"type": "string"},
						},
						"required": []string{"entityType", "id", "data"},
					},
				},
				"shardID": map[string]interface{}{"type": "integer"},
			},
			"required": []string{"operations"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_batch_get",
		Description: "Retrieve multiple values in a single request",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operations": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"entityType": map[string]interface{}{"type": "string"},
							"id":         map[string]interface{}{"type": "integer"},
						},
						"required": []string{"entityType", "id"},
					},
				},
				"shardID": map[string]interface{}{"type": "integer"},
			},
			"required": []string{"operations"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_batch_delete",
		Description: "Remove multiple keys atomically",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operations": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"entityType": map[string]interface{}{"type": "string"},
							"id":         map[string]interface{}{"type": "integer"},
						},
						"required": []string{"entityType", "id"},
					},
				},
				"shardID": map[string]interface{}{"type": "integer"},
			},
			"required": []string{"operations"},
		},
	})

	// Transaction Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_transaction",
		Description: "Execute an atomic compare-and-swap transaction",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"compare": map[string]interface{}{
					"type":        "array",
					"description": "Comparison conditions",
				},
				"success": map[string]interface{}{
					"type":        "array",
					"description": "Operations to execute on success",
				},
				"failure": map[string]interface{}{
					"type":        "array",
					"description": "Operations to execute on failure",
				},
				"shardID": map[string]interface{}{"type": "integer"},
			},
		},
	})

	// Query Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_query",
		Description: "Query entities using secondary indexes",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"entityType": map[string]interface{}{"type": "string"},
				"indexName":  map[string]interface{}{"type": "string"},
				"value":      map[string]interface{}{"type": "string"},
				"shardID":    map[string]interface{}{"type": "integer"},
			},
			"required": []string{"entityType", "indexName", "value"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_scan_index",
		Description: "Perform a full index scan",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"entityType": map[string]interface{}{"type": "string"},
				"indexName":  map[string]interface{}{"type": "string"},
				"startKey":   map[string]interface{}{"type": "string"},
				"endKey":     map[string]interface{}{"type": "string"},
				"limit":      map[string]interface{}{"type": "integer"},
				"shardID":    map[string]interface{}{"type": "integer"},
			},
			"required": []string{"entityType", "indexName"},
		},
	})

	// Cluster Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_cluster_status",
		Description: "Get current cluster status including nodes, shards, and health",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"includeDetails": map[string]interface{}{"type": "boolean", "default": false},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_get_leader",
		Description: "Get the current leader for a specific shard",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"shardID": map[string]interface{}{"type": "integer"},
			},
			"required": []string{"shardID"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_add_node",
		Description: "Add a node to a Raft shard",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"shardID": map[string]interface{}{"type": "integer"},
				"nodeID":  map[string]interface{}{"type": "integer"},
				"address": map[string]interface{}{"type": "string"},
				"identity": map[string]interface{}{
					"type":        "string",
					"description": "Raft member identity spki:<sha256-hex>; optional when the cluster already committed one",
				},
			},
			"required": []string{"shardID", "nodeID", "address"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_remove_node",
		Description: "Remove a node from a Raft shard",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"shardID": map[string]interface{}{"type": "integer"},
				"nodeID":  map[string]interface{}{"type": "integer"},
			},
			"required": []string{"shardID", "nodeID"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_create_shard",
		Description: "Create a new Raft shard with initial members",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"shardID":        map[string]interface{}{"type": "integer"},
				"initialMembers": map[string]interface{}{"type": "object"},
			},
			"required": []string{"shardID", "initialMembers"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_rebalance_cluster",
		Description: "Redistribute data across shards for better balance",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"targetShardCount": map[string]interface{}{"type": "integer"},
				"strategy":         map[string]interface{}{"type": "string"},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_rebalance_status",
		Description: "Get the status and progress of an ongoing rebalance operation",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation_id": map[string]interface{}{"type": "string", "description": "The operation ID returned from rebalance start"},
			},
			"required": []interface{}{"operation_id"},
		},
	})
	// Backup Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_backup_create",
		Description: "Create a consistent backup of the database",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":          map[string]interface{}{"type": "string"},
				"description":   map[string]interface{}{"type": "string"},
				"compression":   map[string]interface{}{"type": "string"},
				"encryption":    map[string]interface{}{"type": "string"},
				"includeShards": map[string]interface{}{"type": "array"},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_backup_list",
		Description: "List available backups",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"limit":  map[string]interface{}{"type": "integer"},
				"offset": map[string]interface{}{"type": "integer"},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_backup_restore",
		Description: "Restore the database from a backup",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"backupID":     map[string]interface{}{"type": "string"},
				"targetShards": map[string]interface{}{"type": "array"},
			},
			"required": []string{"backupID"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_backup_delete",
		Description: "Delete a backup",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"backupID": map[string]interface{}{"type": "string"},
			},
			"required": []string{"backupID"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_backup_verify",
		Description: "Verify backup integrity",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"backupID": map[string]interface{}{"type": "string"},
			},
			"required": []string{"backupID"},
		},
	})

	// Auth Operations
	// Password-based login is intentionally not exposed. Use the REST
	// endpoints under /api/v1/tenants/{tid}/webauthn/login/{begin,finish}
	// or /api/v1/auth/login/piv to obtain access tokens.
	r.addTool(ToolDefinition{
		Name:        "quicbaas_logout",
		Description: "Invalidate current session",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"accessToken": map[string]interface{}{"type": "string"},
			},
			"required": []string{"accessToken"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_introspecttoken",
		Description: "Introspect an access token (RFC 7662): reports whether it is active and, when it is, its subject, expiry and attestation level",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"token": map[string]interface{}{"type": "string", "description": "The access token to introspect."},
			},
			"required": []string{"token"},
		},
	})

	// RBAC Operations -- ROLES ONLY.
	//
	// There are deliberately no user tools here. quicbaas_iam_user_* is the one
	// user surface (tools_iam_exec.go, backed by iam.IdentityStore, which writes
	// through to the embedded qrdb node); read-only qrdb-native user lookups are
	// qrdb_user_get* / qrdb_user_authfactor_* in tools_qrdb_users.go. The
	// qrdb_user_create/delete/list trio that used to sit here routed through a
	// local auth manager production never wired, so every call failed; issue
	// #905 removed it. The role tools below are live: they reach the qrdb-backed
	// IdentityStore through service.rbacService (pkg/service/rbac_role_store.go).
	r.addTool(ToolDefinition{
		Name:        "qrdb_role_create",
		Description: "Create a new role with permissions",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"roleName":    map[string]interface{}{"type": "string"},
				"permissions": map[string]interface{}{"type": "array"},
			},
			"required": []string{"roleName"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_role_delete",
		Description: "Delete a role",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"roleName": map[string]interface{}{"type": "string"},
			},
			"required": []string{"roleName"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_role_list",
		Description: "List all roles",
		InputSchema: map[string]interface{}{
			"type": "object",
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_role_assign",
		Description: "Assign a role to a user",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"username": map[string]interface{}{"type": "string"},
				"roleName": map[string]interface{}{"type": "string"},
			},
			"required": []string{"username", "roleName"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_role_revoke",
		Description: "Revoke a role from a user",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"username": map[string]interface{}{"type": "string"},
				"roleName": map[string]interface{}{"type": "string"},
			},
			"required": []string{"username", "roleName"},
		},
	})

	// Cache Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_cache_stats",
		Description: "Get cache statistics",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"entityType": map[string]interface{}{"type": "string"},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_cache_clear",
		Description: "Clear cache entries",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"entityType": map[string]interface{}{"type": "string"},
				"all":        map[string]interface{}{"type": "boolean"},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_cache_config_get",
		Description: "Get cache configuration",
		InputSchema: map[string]interface{}{
			"type": "object",
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_cache_config_update",
		Description: "Update cache configuration",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"l1_size":        map[string]interface{}{"type": "integer"},
				"l1_ttl_seconds": map[string]interface{}{"type": "integer"},
			},
		},
	})

	// Health Operations
	r.addTool(ToolDefinition{
		Name:        "qrdb_health",
		Description: "Get node health status",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"includeDetails": map[string]interface{}{"type": "boolean"},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_liveness",
		Description: "Check if the service is alive",
		InputSchema: map[string]interface{}{
			"type": "object",
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_readiness",
		Description: "Check if the service is ready to serve requests",
		InputSchema: map[string]interface{}{
			"type": "object",
		},
	})

	// Time-Series Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_aggregate",
		Description: "Perform time-series aggregation",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"entityType": map[string]interface{}{"type": "string"},
				"fieldName":  map[string]interface{}{"type": "string"},
				"function":   map[string]interface{}{"type": "string"},
				"startTime":  map[string]interface{}{"type": "integer"},
				"endTime":    map[string]interface{}{"type": "integer"},
				"interval":   map[string]interface{}{"type": "integer"},
			},
			"required": []string{"entityType", "fieldName", "function"},
		},
	})

	// Operations Management
	r.addTool(ToolDefinition{
		Name:        "quicbaas_migrate_data",
		Description: "Migrate data between shards",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"sourceShard": map[string]interface{}{"type": "integer"},
				"targetShard": map[string]interface{}{"type": "integer"},
				"entityTypes": map[string]interface{}{"type": "array"},
				"concurrency": map[string]interface{}{"type": "integer"},
			},
			"required": []string{"sourceShard", "targetShard"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_migration_progress",
		Description: "Get migration progress",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"migrationID": map[string]interface{}{"type": "string"},
			},
			"required": []string{"migrationID"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_replace_node",
		Description: "Replace a failed node",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"shardID":    map[string]interface{}{"type": "integer"},
				"oldNodeID":  map[string]interface{}{"type": "integer"},
				"newNodeID":  map[string]interface{}{"type": "integer"},
				"newAddress": map[string]interface{}{"type": "string"},
			},
			"required": []string{"shardID", "oldNodeID", "newNodeID", "newAddress"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_upgrade_node",
		Description: "Perform rolling upgrade of a node",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"nodeID":        map[string]interface{}{"type": "integer"},
				"targetVersion": map[string]interface{}{"type": "string"},
			},
			"required": []string{"nodeID", "targetVersion"},
		},
	})
}

// addTool adds a tool definition to the registry.
func (r *ToolRegistry) addTool(tool ToolDefinition) {
	r.tools = append(r.tools, tool)
}

// ListTools returns all registered tools.
func (r *ToolRegistry) ListTools() []ToolDefinition {
	return r.tools
}

// SetBarrierService installs the BarrierServicer used by every
// quicbaas_barrier_* tool. Pass nil to take the surface offline; tools will
// then return BarrierServiceUnavailableError.
func (r *ToolRegistry) SetBarrierService(svc service.BarrierServicer) {
	r.barrierSvc = svc
}

// SetCeremonyService installs the CeremonyServicer used by every
// quicbaas_ceremony_* tool. Pass nil to take the surface offline; tools will
// then return CeremonyServiceUnavailableError.
func (r *ToolRegistry) SetCeremonyService(svc service.CeremonyServicer) {
	r.ceremonySvc = svc
}

// SetTenantBarrierService installs the TenantBarrierServicer used by every
// qrdb_tenant_barrier_* tool. Pass nil to take the surface offline; tools will
// then return TenantBarrierServiceUnavailableError.
func (r *ToolRegistry) SetTenantBarrierService(svc service.TenantBarrierServicer) {
	r.tenantBarrierSvc = svc
}

// SetTenantGovernance installs the mcpTenantGovernancer used by the
// qrdb_tenant_update_quotas and qrdb_tenant_update_entitlements tools.
// Pass nil to take the surface offline; tools will then return
// GovernanceServiceUnavailableError.
func (r *ToolRegistry) SetTenantGovernance(g mcpTenantGovernancer) {
	r.tenantGovernance = g
}

// ExecuteTool executes a tool by name with the given arguments.
func (r *ToolRegistry) ExecuteTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	r.logger.WithFields(map[string]interface{}{
		"tool": name,
		"args": args,
	}).Debug("Executing tool")

	// Add timeout to context if not present
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	// Route to appropriate handler
	switch name {
	// Seal Operations
	case "quicbaas_seal_status":
		return r.executeSealStatus(ctx, args)
	case "quicbaas_seal_init":
		return r.executeSealInit(ctx, args)
	case "quicbaas_unseal":
		return r.executeUnseal(ctx, args)
	case "quicbaas_seal":
		return r.executeSeal(ctx, args)
	case "quicbaas_seal_rekey":
		return r.executeSealRekey(ctx, args)
	case "quicbaas_seal_generate_recovery_key":
		return r.executeSealGenerateRecoveryKey(ctx, args)
	case "quicbaas_seal_delete_recovery_key":
		return r.executeSealDeleteRecoveryKey(ctx, args)
	case "quicbaas_seal_generate_root_token":
		return r.executeSealGenerateRootToken(ctx, args)

	// KMS Operations
	case "quicbaas_kms_generate_key":
		return r.executeKMSGenerateKey(ctx, args)
	case "quicbaas_kms_import_key":
		return r.executeKMSImportKey(ctx, args)
	case "quicbaas_kms_get_key":
		return r.executeKMSGetKey(ctx, args)
	case "quicbaas_kms_get_public_key":
		return r.executeKMSGetPublicKey(ctx, args)
	case "quicbaas_kms_list_keys":
		return r.executeKMSListKeys(ctx, args)
	case "quicbaas_kms_rotate_key":
		return r.executeKMSRotateKey(ctx, args)
	case "quicbaas_kms_revoke_key":
		return r.executeKMSRevokeKey(ctx, args)
	case "quicbaas_kms_delete_key":
		return r.executeKMSDeleteKey(ctx, args)
	case "quicbaas_kms_get_metadata":
		return r.executeKMSGetMetadata(ctx, args)
	case "quicbaas_kms_update_metadata":
		return r.executeKMSUpdateMetadata(ctx, args)
	case "quicbaas_kms_set_policy":
		return r.executeKMSSetPolicy(ctx, args)
	case "quicbaas_kms_get_policy":
		return r.executeKMSGetPolicy(ctx, args)
	case "quicbaas_kms_set_rotation_policy":
		return r.executeKMSSetRotationPolicy(ctx, args)
	case "quicbaas_kms_get_rotation_policy":
		return r.executeKMSGetRotationPolicy(ctx, args)
	case "quicbaas_kms_delete_rotation_policy":
		return r.executeKMSDeleteRotationPolicy(ctx, args)
	case "quicbaas_kms_get_supported_algorithms":
		return r.executeKMSGetSupportedAlgorithms(ctx, args)

	// KMS Operations - Encrypt/Decrypt/Backend
	case "quicbaas_kms_encrypt":
		return r.executeKMSEncrypt(ctx, args)
	case "quicbaas_kms_decrypt":
		return r.executeKMSDecrypt(ctx, args)
	case "quicbaas_kms_backend_test":
		return r.executeKMSBackendTest(ctx, args)
	case "quicbaas_kms_key_versions":
		return r.executeKMSKeyVersions(ctx, args)
	case "quicbaas_kms_key_status_update":
		return r.executeKMSKeyStatusUpdate(ctx, args)
	case "quicbaas_kms_backend_status":
		return r.executeKMSBackendStatus(ctx, args)
	case "quicbaas_kms_set_key_safety":
		return r.executeKMSSetKeySafety(ctx, args)
	case "quicbaas_kms_clear_key_safety":
		return r.executeKMSClearKeySafety(ctx, args)
	case "quicbaas_kms_safety_check":
		return r.executeKMSSafetyCheck(ctx, args)
	case "quicbaas_kms_safety_report":
		return r.executeKMSSafetyReport(ctx, args)

	// KMS Operations - Export/Unwrap
	case "quicbaas_kms_export_key":
		return r.executeKMSExportKey(ctx, args)
	case "quicbaas_kms_unwrap_key":
		return r.executeKMSUnwrapKey(ctx, args)

	// DEK (Data Encryption Key) Operations
	case "quicbaas_dek_create":
		return r.executeDEKCreate(ctx, args)
	case "quicbaas_dek_get":
		return r.executeDEKGet(ctx, args)
	case "quicbaas_dek_list":
		return r.executeDEKList(ctx, args)
	case "quicbaas_dek_rotate":
		return r.executeDEKRotate(ctx, args)
	case "quicbaas_dek_delete":
		return r.executeDEKDelete(ctx, args)
	case "quicbaas_dek_status":
		return r.executeDEKStatus(ctx, args)
	case "quicbaas_dek_set_default":
		return r.executeDEKSetDefault(ctx, args)
	case "quicbaas_dek_set_safety":
		return r.executeDEKSetSafety(ctx, args)
	case "quicbaas_dek_clear_safety":
		return r.executeDEKClearSafety(ctx, args)
	case "quicbaas_dek_start_reencryption":
		return r.executeDEKStartReencryption(ctx, args)
	case "quicbaas_dek_get_reencryption_progress":
		return r.executeDEKGetReencryptionProgress(ctx, args)

	// Secrets Operations
	case "quicbaas_secrets_store":
		return r.executeSecretsStore(ctx, args)
	case "quicbaas_secrets_ensure":
		return r.executeSecretsEnsure(ctx, args)
	case "quicbaas_secrets_get":
		return r.executeSecretsGet(ctx, args)
	case "quicbaas_secrets_list":
		return r.executeSecretsList(ctx, args)
	case "quicbaas_secrets_delete":
		return r.executeSecretsDelete(ctx, args)
	case "quicbaas_secrets_versions":
		return r.executeSecretsVersions(ctx, args)
	case "quicbaas_secrets_rollback":
		return r.executeSecretsRollback(ctx, args)
	case "quicbaas_secrets_rotate":
		return r.executeSecretsRotate(ctx, args)
	case "quicbaas_secrets_set_rotation_policy":
		return r.executeSecretsSetRotationPolicy(ctx, args)
	case "quicbaas_secrets_get_rotation_policy":
		return r.executeSecretsGetRotationPolicy(ctx, args)
	case "quicbaas_secrets_get_prefix":
		return r.executeSecretsGetPrefix(ctx, args)
	case "quicbaas_secrets_watch":
		return r.executeSecretsWatch(ctx, args)
	case "quicbaas_secrets_update_status":
		return r.executeSecretsUpdateStatus(ctx, args)

	// Crypto Operations
	case "quicbaas_crypto_encrypt":
		return r.executeCryptoEncrypt(ctx, args)
	case "quicbaas_crypto_decrypt":
		return r.executeCryptoDecrypt(ctx, args)
	case "quicbaas_crypto_rewrap":
		return r.executeCryptoRewrap(ctx, args)
	case "quicbaas_crypto_sign":
		return r.executeCryptoSign(ctx, args)
	case "quicbaas_crypto_verify":
		return r.executeCryptoVerify(ctx, args)
	case "quicbaas_crypto_hmac":
		return r.executeCryptoHMAC(ctx, args)
	case "quicbaas_crypto_verify_hmac":
		return r.executeCryptoVerifyHMAC(ctx, args)
	case "quicbaas_crypto_hash":
		return r.executeCryptoHash(ctx, args)
	case "quicbaas_crypto_random":
		return r.executeCryptoRandom(ctx, args)
	case "quicbaas_crypto_datakey":
		return r.executeCryptoDataKey(ctx, args)
	case "quicbaas_crypto_datakey_wrapped":
		return r.executeCryptoDataKeyWrapped(ctx, args)
	case "quicbaas_crypto_encrypt_batch":
		return r.executeCryptoEncryptBatch(ctx, args)
	case "quicbaas_crypto_decrypt_batch":
		return r.executeCryptoDecryptBatch(ctx, args)

	// KV Operations
	case "qrdb_put":
		return r.executePut(ctx, args)
	case "qrdb_get":
		return r.executeGet(ctx, args)
	case "qrdb_delete":
		return r.executeDelete(ctx, args)
	case "qrdb_list":
		return r.executeList(ctx, args)

	// Batch Operations
	case "quicbaas_batch_put":
		return r.executeBatchPut(ctx, args)
	case "quicbaas_batch_get":
		return r.executeBatchGet(ctx, args)
	case "quicbaas_batch_delete":
		return r.executeBatchDelete(ctx, args)

	// Transaction Operations
	case "quicbaas_transaction":
		return r.executeTransaction(ctx, args)

	// Query Operations
	case "quicbaas_query":
		return r.executeQuery(ctx, args)
	case "quicbaas_scan_index":
		return r.executeScanIndex(ctx, args)

	// Cluster Operations
	case "quicbaas_cluster_status":
		return r.executeClusterStatus(ctx, args)
	case "quicbaas_get_leader":
		return r.executeGetLeader(ctx, args)
	case "quicbaas_add_node":
		return r.executeAddNode(ctx, args)
	case "quicbaas_remove_node":
		return r.executeRemoveNode(ctx, args)
	case "quicbaas_create_shard":
		return r.executeCreateShard(ctx, args)
	case "quicbaas_rebalance_cluster":
		return r.executeRebalanceCluster(ctx, args)

	case "quicbaas_rebalance_status":
		return r.executeRebalanceStatus(ctx, args)
	// Backup Operations
	case "quicbaas_backup_create":
		return r.executeBackupCreate(ctx, args)
	case "quicbaas_backup_list":
		return r.executeBackupList(ctx, args)
	case "quicbaas_backup_restore":
		return r.executeBackupRestore(ctx, args)
	case "quicbaas_backup_delete":
		return r.executeBackupDelete(ctx, args)
	case "quicbaas_backup_verify":
		return r.executeBackupVerify(ctx, args)

	// Auth Operations
	case "quicbaas_logout":
		return r.executeLogout(ctx, args)
	case "quicbaas_introspecttoken":
		return r.executeIntrospectToken(ctx, args)

	// RBAC Operations
	case "qrdb_role_create":
		return r.executeRoleCreate(ctx, args)
	case "qrdb_role_delete":
		return r.executeRoleDelete(ctx, args)
	case "qrdb_role_list":
		return r.executeRoleList(ctx, args)
	case "qrdb_role_assign":
		return r.executeRoleAssign(ctx, args)
	case "quicbaas_role_revoke":
		return r.executeRoleRevoke(ctx, args)

	// Cache Operations
	case "quicbaas_cache_stats":
		return r.executeCacheStats(ctx, args)
	case "quicbaas_cache_clear":
		return r.executeCacheClear(ctx, args)
	case "quicbaas_cache_config_get":
		return r.executeCacheConfig(ctx, args)
	case "quicbaas_cache_config_update":
		return r.executeCacheConfigUpdate(ctx, args)

	// Health Operations
	case "qrdb_health":
		return r.executeHealth(ctx, args)
	case "quicbaas_liveness":
		return r.executeLiveness(ctx, args)
	case "quicbaas_readiness":
		return r.executeReadiness(ctx, args)

	// Time-Series Operations
	case "quicbaas_aggregate":
		return r.executeAggregate(ctx, args)

	// Operations Management
	case "quicbaas_migrate_data":
		return r.executeMigrateData(ctx, args)
	case "quicbaas_migration_progress":
		return r.executeMigrationProgress(ctx, args)
	case "quicbaas_replace_node":
		return r.executeReplaceNode(ctx, args)
	case "quicbaas_upgrade_node":
		return r.executeUpgradeNode(ctx, args)

	// Object Storage Operations
	case "quicbaas_object_put":
		return r.executeObjectPut(ctx, args)
	case "quicbaas_object_get":
		return r.executeObjectGet(ctx, args)
	case "quicbaas_object_delete":
		return r.executeObjectDelete(ctx, args)
	case "quicbaas_object_head":
		return r.executeObjectHead(ctx, args)
	case "quicbaas_object_list":
		return r.executeObjectList(ctx, args)
	case "quicbaas_object_list_by_tag":
		return r.executeObjectListByTag(ctx, args)
	case "quicbaas_object_list_by_tenant":
		return r.executeObjectListByTenant(ctx, args)
	case "quicbaas_object_copy":
		return r.executeObjectCopy(ctx, args)
	case "quicbaas_object_replicate":
		return r.executeObjectReplicate(ctx, args)
	case "quicbaas_object_migrate":
		return r.executeObjectMigrate(ctx, args)
	case "quicbaas_object_archive":
		return r.executeObjectArchive(ctx, args)
	case "quicbaas_object_restore":
		return r.executeObjectRestore(ctx, args)
	case "quicbaas_object_verify":
		return r.executeObjectVerify(ctx, args)
	case "quicbaas_object_set_policy":
		return r.executeObjectSetPolicy(ctx, args)
	case "quicbaas_object_get_policy":
		return r.executeObjectGetPolicy(ctx, args)
	case "quicbaas_object_list_backends":
		return r.executeObjectListBackends(ctx, args)
	case "quicbaas_object_backend_health":
		return r.executeObjectBackendHealth(ctx, args)
	case "quicbaas_object_tenant_usage":
		return r.executeObjectTenantUsage(ctx, args)
	case "quicbaas_object_tenant_quotas":
		return r.executeObjectTenantQuotas(ctx, args)

	// Object Storage Batch Operations
	case "quicbaas_object_batch_put":
		return r.executeObjectBatchPut(ctx, args)
	case "quicbaas_object_batch_get":
		return r.executeObjectBatchGet(ctx, args)
	case "quicbaas_object_batch_delete":
		return r.executeObjectBatchDelete(ctx, args)
	case "quicbaas_object_set_tenant_quotas":
		return r.executeObjectSetTenantQuotas(ctx, args)

	// Object Storage Admin Operations
	case "quicbaas_object_admin_create_backend":
		return r.executeObjectAdminCreateBackend(ctx, args)
	case "quicbaas_object_admin_update_backend":
		return r.executeObjectAdminUpdateBackend(ctx, args)
	case "quicbaas_object_admin_delete_backend":
		return r.executeObjectAdminDeleteBackend(ctx, args)
	case "quicbaas_object_admin_set_backend_status":
		return r.executeObjectAdminSetBackendStatus(ctx, args)
	case "quicbaas_object_admin_get_storage_stats":
		return r.executeObjectAdminGetStorageStats(ctx, args)
	case "quicbaas_object_admin_run_gc":
		return r.executeObjectAdminRunGC(ctx, args)
	case "quicbaas_object_admin_get_gc_progress":
		return r.executeObjectAdminGetGCProgress(ctx, args)
	case "quicbaas_object_admin_repair_object":
		return r.executeObjectAdminRepairObject(ctx, args)
	case "quicbaas_object_admin_compact_backend":
		return r.executeObjectAdminCompactBackend(ctx, args)

	// Object Transfer Operations
	case "quicbaas_object_transfer_fetch":
		return r.executeObjectTransferFetch(ctx, args)
	case "quicbaas_object_transfer_stream_fetch":
		return r.executeObjectTransferStreamFetch(ctx, args)

	default:
		// Check if it's an IAM tool (must be before other checks for proper routing)
		if isIAMTool(name) {
			return r.executeIAMTool(ctx, name, args)
		}

		// Check if it's a Vector Database tool
		if isVectorTool(name) {
			return r.executeVectorTool(ctx, name, args)
		}

		// Check if it's a Project tool
		if isProjectTool(name) {
			return r.executeProjectTool(ctx, name, args)
		}

		// SDLC module: forge bindings and deliveries
		if isSDLCTool(name) {
			return r.executeSDLCTool(ctx, name, args)
		}
		if isPromptTool(name) {
			return r.executePromptTool(ctx, name, args)
		}

		// Check if it's a pooled provider account tool
		if isSSHTool(name) {
			return r.executeSSHTool(ctx, name, args)
		}
		if isAccountTool(name) {
			return r.executeAccountTool(ctx, name, args)
		}
		if isCostSettingsTool(name) {
			return r.executeCostSettingsTool(ctx, name, args)
		}
		if isAgentCatalogTool(name) {
			return r.executeAgentCatalogTool(ctx, name, args)
		}

		// Check if it's a scheduler placement tool
		if isPlacementTool(name) {
			return r.executePlacementTool(ctx, name, args)
		}

		// Check if it's a Scheduler tool
		if isSchedulerTool(name) {
			return r.executeSchedulerTool(ctx, name, args)
		}

		// Check if it's a TimeSeries tool
		if isTimeSeriesTool(name) {
			return r.executeTimeSeriesTool(ctx, name, args)
		}

		// Check if it's an Ollama tool
		if isOllamaTool(name) {
			return r.executeOllamaTool(ctx, name, args)
		}

		// Check if it's a RAG tool
		if isRAGTool(name) {
			return r.executeRAGTool(ctx, name, args)
		}

		// Check if it's an Embedding tool
		if isEmbeddingTool(name) {
			return r.executeEmbeddingTool(ctx, name, args)
		}

		// Check if it's a Certificate tool
		if isCertificateTool(name) {
			return r.executeCertificateTool(ctx, name, args)
		}

		// Check if it's a Trust tool (cross-sign / AIA composition)
		if isTrustTool(name) {
			return r.executeTrustTool(ctx, name, args)
		}

		// Check if it's an Attestation tool (xkms statement/chain
		// verification + the container attestation seam)
		if isAttestationTool(name) {
			return r.executeAttestationTool(ctx, name, args)
		}

		// Check if it's a service registry tool
		if isRegistryTool(name) {
			return r.executeRegistryTool(ctx, name, args)
		}

		// Check if it's a Dynamic Secrets tool
		if isDynamicSecretsTool(name) {
			return r.executeDynamicSecretsTool(ctx, name, args)
		}

		// Check if it's a Parameter Store tool
		if isParameterTool(name) {
			return r.executeParameterTool(ctx, name, args)
		}

		// Check if it's an Object Transfer tool
		if isObjectTransferTool(name) {
			return r.executeObjectTransferTool(ctx, name, args)
		}

		// Check if it is a Messaging tool
		if isMsgTool(name) {
			return r.executeMsgTool(ctx, name, args)
		}

		// Check if it is an XKMS-compatible tool
		if isXKMSTool(name) {
			return r.executeXKMSTool(ctx, name, args)
		}

		// Check if it is a RAG Indexing tool
		if isRAGIndexTool(name) {
			return r.executeRAGIndexTool(ctx, name, args)
		}

		// Check if it is an Agent Orchestration tool
		if isAgentTool(name) {
			return r.executeAgentTool(ctx, name, args)
		}

		// Check if it is an Agent Cache tool
		if isAgentCacheTool(name) {
			return r.executeAgentCacheTool(ctx, name, args)
		}

		// Check if it is an Agent Admin tool (disable/enable globally)
		if isFleetAdminTool(name) {
			return r.executeFleetAdminTool(ctx, name, args)
		}

		// Check if it is a Barrier lifecycle tool.
		if isBarrierTool(name) {
			return r.executeBarrierTool(ctx, name, args)
		}

		// Check if it is a Ceremony lifecycle tool.
		if isCeremonyTool(name) {
			return r.executeCeremonyTool(ctx, name, args)
		}

		// Check if it is a unified login ceremony tool.
		if isLoginTool(name) {
			return r.executeLoginTool(ctx, name, args)
		}

		// Check if it is a qrdb credential management tool.
		if isCredentialTool(name) {
			return r.executeCredentialTool(ctx, name, args)
		}

		// Check if it is a per-tenant barrier tool.
		if isTenantBarrierTool(name) {
			return r.executeTenantBarrierTool(ctx, name, args)
		}

		// Check if it is a qrdb user lookup / user auth-factor tool.
		if isQRDBUserTool(name) {
			return r.executeQRDBUserTool(ctx, name, args)
		}

		return nil, api.NewUnknownToolError(name)
	}
}

// Helper function to extract string argument.
func getString(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// UserIDArgError reports a user id tool argument that is missing or is not
// a non-zero decimal uint64 user id.
type UserIDArgError struct {
	Arg   string
	Value string
	Err   error
}

// Error implements the error interface.
func (e *UserIDArgError) Error() string {
	if e.Err != nil {
		return "mcp: invalid " + e.Arg + " " + strconv.Quote(e.Value) + ": " + e.Err.Error()
	}
	return "mcp: invalid " + e.Arg + " " + strconv.Quote(e.Value)
}

// Unwrap returns the underlying parse error, if any.
func (e *UserIDArgError) Unwrap() error {
	return e.Err
}

// ErrorCode classifies the error as invalid input.
func (e *UserIDArgError) ErrorCode() qberrors.ErrorCode {
	return qberrors.ErrInvalidInput
}

// getUserID parses the user_id argument. A missing, non-numeric,
// overflowing or zero value returns *UserIDArgError.
func getUserID(args map[string]interface{}) (uint64, error) {
	return getUserIDArg(args, "user_id")
}

// getUserIDArg parses the named user id argument. A missing, non-numeric,
// overflowing or zero value returns *UserIDArgError.
func getUserIDArg(args map[string]interface{}, name string) (uint64, error) {
	value := getString(args, name)
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, &UserIDArgError{Arg: name, Value: value, Err: err}
	}
	if id == 0 {
		return 0, &UserIDArgError{Arg: name, Value: value}
	}
	return id, nil
}

// getOptionalUserIDArg parses the named user id argument when present. An
// absent argument yields 0; a present but invalid one returns
// *UserIDArgError.
func getOptionalUserIDArg(args map[string]interface{}, name string) (uint64, error) {
	if getString(args, name) == "" {
		return 0, nil
	}
	return getUserIDArg(args, name)
}

// getStringAlt extracts a string argument, trying key1 first then falling back to key2.
// This supports both camelCase (e.g., entityType) and snake_case (e.g., entity_type) parameters.
func getStringAlt(args map[string]interface{}, key1, key2 string) string {
	if v := getString(args, key1); v != "" {
		return v
	}
	return getString(args, key2)
}

// Helper function to extract uint64 argument.
func getUint64(args map[string]interface{}, key string) uint64 {
	n, err := uint64Arg(args, key)
	if err != nil {
		// A value with no uint64 form reads as absent. Handlers that must
		// refuse it read the argument through uint64Arg or intArgReader.
		return 0
	}
	return n
}

// getUint64Alt extracts a uint64 argument, trying key1 first then falling back to key2.
// This supports both camelCase (e.g., pageSize) and snake_case (e.g., limit) parameters.
func getUint64Alt(args map[string]interface{}, key1, key2 string) uint64 {
	if v := getUint64(args, key1); v != 0 {
		return v
	}
	return getUint64(args, key2)
}

// hashStringKey converts a string key to a uint64 ID using FNV-1a hash.
// The entity type is included in the hash to match the REST KV API convention,
// ensuring consistent ID generation across all protocols.
func hashStringKey(entityType, key string) uint64 {
	return id.HashString(entityType + ":" + key)
}

// getIDOrHashKey returns the ID from args, or if a string "key" is provided, hashes it to uint64.
// The entityType is included in the hash to produce IDs consistent with the REST KV API.
func getIDOrHashKey(entityType string, args map[string]interface{}) uint64 {
	// First try the numeric id parameter
	if id := getUint64(args, "id"); id != 0 {
		return id
	}
	// If string key is provided, hash entityType:key to uint64
	if key := getString(args, "key"); key != "" {
		return hashStringKey(entityType, key)
	}
	return 0
}

// Helper function to extract bool argument.
func getBool(args map[string]interface{}, key string) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return false
}

// Helper function to extract int argument.
func getInt(args map[string]interface{}, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case uint64:
		n, err := conv.SafeUint64ToInt(v)
		if err != nil {
			// A value an int cannot hold reads as absent, as an unparsable
			// string does.
			return 0
		}
		return n
	case string:
		if v != "" {
			i, _ := strconv.Atoi(v)
			return i
		}
	}
	return 0
}

// getStringSlice extracts a string slice argument from the args map.
// It handles both []interface{} (from JSON unmarshaling) and []string types.
func getStringSlice(args map[string]interface{}, key string) []string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return v
	}
	return nil
}

// getStringMap extracts a map[string]string from a JSON-RPC argument map,
// accepting either a map[string]string directly or a map[string]interface{}
// whose values are strings (the JSON-decoded form). Non-string values are
// skipped. Returns nil when the key is absent or holds no usable entries.
func getStringMap(args map[string]interface{}, key string) map[string]string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case map[string]string:
		return v
	case map[string]interface{}:
		result := make(map[string]string, len(v))
		for k, item := range v {
			if s, ok := item.(string); ok {
				result[k] = s
			}
		}
		if len(result) == 0 {
			return nil
		}
		return result
	}
	return nil
}

// Helper function to marshal result to JSON-compatible format.
func marshalResult(v interface{}) (interface{}, error) {
	// Convert to JSON and back to get JSON-compatible types
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	var result interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// parseJSONObject converts a map[string]interface{} to a typed struct via JSON round-trip.
func parseJSONObject(src map[string]interface{}, dst interface{}) error {
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

// Tool execution handlers (implementation stubs - see tools_impl.go for full implementations)

func (r *ToolRegistry) executePut(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Support both camelCase (entityType) and snake_case (entity_type) parameters
	entityType := getStringAlt(args, "entityType", "entity_type")

	// Support both numeric id and string key parameters
	// If string key is provided, hash it to uint64 ID
	id := getIDOrHashKey(entityType, args)

	// Support both camelCase (data) and snake_case (value) for the value parameter
	data := getStringAlt(args, "data", "value")

	req := &service.PutRequest{
		EntityType: entityType,
		ID:         id,
		Data:       []byte(data),
		ShardID:    getUint64(args, "shardID"),
	}
	resp, err := r.service.Put(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Support both camelCase (entityType) and snake_case (entity_type) parameters
	entityType := getStringAlt(args, "entityType", "entity_type")

	// Support both numeric id and string key parameters
	// If string key is provided, hash it to uint64 ID
	id := getIDOrHashKey(entityType, args)

	req := &service.GetRequest{
		EntityType: entityType,
		ID:         id,
		ShardID:    getUint64(args, "shardID"),
	}
	resp, err := r.service.Get(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Support both camelCase (entityType) and snake_case (entity_type) parameters
	entityType := getStringAlt(args, "entityType", "entity_type")

	// Support both numeric id and string key parameters
	// If string key is provided, hash it to uint64 ID
	id := getIDOrHashKey(entityType, args)

	req := &service.DeleteRequest{
		EntityType: entityType,
		ID:         id,
		ShardID:    getUint64(args, "shardID"),
	}
	resp, err := r.service.Delete(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Support both camelCase (entityType) and snake_case (entity_type) parameters
	entityType := getStringAlt(args, "entityType", "entity_type")

	// Support both pageSize and limit for pagination
	pageSize := getUint64Alt(args, "pageSize", "limit")

	// Support both pageToken and cursor for pagination
	pageToken := getStringAlt(args, "pageToken", "cursor")

	req := &service.ListRequest{
		EntityType: entityType,
		PageSize:   pageSize,
		PageToken:  pageToken,
		ShardID:    getUint64(args, "shardID"),
	}

	// Create channel for streaming results
	stream := make(chan *service.ListResponse, 10)

	// Start list operation in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.service.List(ctx, req, stream)
	}()

	// Collect all results
	var entities []interface{}
	for resp := range stream {
		for _, entity := range resp.Entities {
			entities = append(entities, entity)
		}
	}

	// Check for errors
	if err := <-errCh; err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"entities": entities,
		"count":    len(entities),
	}, nil
}

// executeBatchPut parses batch put operations from args and delegates to service.BatchPut.
func (r *ToolRegistry) executeBatchPut(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Parse the operations array from args
	ops, err := parseBatchOperationsArray(args)
	if err != nil {
		return nil, err
	}

	shardID := getUint64(args, "shardID")

	// Build service request from parsed operations
	requests := make([]*service.PutRequest, 0, len(ops))
	for i, op := range ops {
		opMap, ok := op.(map[string]interface{})
		if !ok {
			return nil, &BatchOperationParseError{
				Index:   i,
				Field:   "operation",
				Message: "expected object",
			}
		}

		entityType := getStringAlt(opMap, "entityType", "entity_type")
		if entityType == "" {
			return nil, &BatchOperationParseError{
				Index:   i,
				Field:   "entityType",
				Message: "entity_type is required",
			}
		}

		id := getIDOrHashKey(entityType, opMap)
		if id == 0 {
			return nil, &BatchOperationParseError{
				Index:   i,
				Field:   "id",
				Message: "id is required",
			}
		}

		data := getStringAlt(opMap, "data", "value")
		if data == "" {
			return nil, &BatchOperationParseError{
				Index:   i,
				Field:   "data",
				Message: "data is required",
			}
		}

		requests = append(requests, &service.PutRequest{
			EntityType: entityType,
			ID:         id,
			Data:       []byte(data),
			ShardID:    shardID,
			WriteMode:  service.WriteModeBatched,
		})
	}

	serviceReq := &service.BatchPutRequest{
		Requests: requests,
		ShardID:  shardID,
	}

	resp, err := r.service.BatchPut(ctx, serviceReq)
	if err != nil {
		return nil, err
	}

	return marshalResult(resp)
}

// executeBatchGet parses batch get operations from args and delegates to service.BatchGet.
func (r *ToolRegistry) executeBatchGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Parse the operations array from args
	ops, err := parseBatchOperationsArray(args)
	if err != nil {
		return nil, err
	}

	shardID := getUint64(args, "shardID")

	// Build service request from parsed operations
	requests := make([]*service.GetRequest, 0, len(ops))
	for i, op := range ops {
		opMap, ok := op.(map[string]interface{})
		if !ok {
			return nil, &BatchOperationParseError{
				Index:   i,
				Field:   "operation",
				Message: "expected object",
			}
		}

		entityType := getStringAlt(opMap, "entityType", "entity_type")
		if entityType == "" {
			return nil, &BatchOperationParseError{
				Index:   i,
				Field:   "entityType",
				Message: "entity_type is required",
			}
		}

		id := getIDOrHashKey(entityType, opMap)
		if id == 0 {
			return nil, &BatchOperationParseError{
				Index:   i,
				Field:   "id",
				Message: "id is required",
			}
		}

		requests = append(requests, &service.GetRequest{
			EntityType: entityType,
			ID:         id,
			ShardID:    shardID,
		})
	}

	serviceReq := &service.BatchGetRequest{
		Requests: requests,
		ShardID:  shardID,
	}

	resp, err := r.service.BatchGet(ctx, serviceReq)
	if err != nil {
		return nil, err
	}

	return marshalResult(resp)
}

// executeBatchDelete parses batch delete operations from args and delegates to service.BatchDelete.
func (r *ToolRegistry) executeBatchDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Parse the operations array from args
	ops, err := parseBatchOperationsArray(args)
	if err != nil {
		return nil, err
	}

	shardID := getUint64(args, "shardID")

	// Build service request from parsed operations
	requests := make([]*service.DeleteRequest, 0, len(ops))
	for i, op := range ops {
		opMap, ok := op.(map[string]interface{})
		if !ok {
			return nil, &BatchOperationParseError{
				Index:   i,
				Field:   "operation",
				Message: "expected object",
			}
		}

		entityType := getStringAlt(opMap, "entityType", "entity_type")
		if entityType == "" {
			return nil, &BatchOperationParseError{
				Index:   i,
				Field:   "entityType",
				Message: "entity_type is required",
			}
		}

		id := getIDOrHashKey(entityType, opMap)
		if id == 0 {
			return nil, &BatchOperationParseError{
				Index:   i,
				Field:   "id",
				Message: "id is required",
			}
		}

		requests = append(requests, &service.DeleteRequest{
			EntityType: entityType,
			ID:         id,
			ShardID:    shardID,
		})
	}

	serviceReq := &service.BatchDeleteRequest{
		Requests: requests,
		ShardID:  shardID,
	}

	resp, err := r.service.BatchDelete(ctx, serviceReq)
	if err != nil {
		return nil, err
	}

	return marshalResult(resp)
}

// parseBatchOperationsArray extracts and validates the "operations" array from batch tool args.
func parseBatchOperationsArray(args map[string]interface{}) ([]interface{}, error) {
	opsRaw, ok := args["operations"]
	if !ok || opsRaw == nil {
		return nil, &BatchOperationsEmptyError{}
	}

	opsArray, ok := opsRaw.([]interface{})
	if !ok {
		return nil, &BatchOperationsInvalidError{}
	}

	if len(opsArray) == 0 {
		return nil, &BatchOperationsEmptyError{}
	}

	return opsArray, nil
}

func (r *ToolRegistry) executeTransaction(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Parse compare conditions
	compareConditions, err := parseCompareConditions(args)
	if err != nil {
		return nil, err
	}

	// Parse success operations
	successOps, err := parseTransactionOps(args, "success")
	if err != nil {
		return nil, err
	}

	// Parse failure operations
	failureOps, err := parseTransactionOps(args, "failure")
	if err != nil {
		return nil, err
	}

	// Build the transaction request
	req := &service.TransactionRequest{
		Compare: compareConditions,
		Success: successOps,
		Failure: failureOps,
		ShardID: getUint64(args, "shardID"),
	}

	// Execute the transaction
	resp, err := r.service.Transaction(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(resp)
}

// parseCompareConditions parses the "compare" array from transaction args.
func parseCompareConditions(args map[string]interface{}) ([]*service.CompareCondition, error) {
	compareRaw, ok := args["compare"]
	if !ok || compareRaw == nil {
		return nil, nil
	}

	compareArray, ok := compareRaw.([]interface{})
	if !ok {
		return nil, &TransactionParseError{
			Field:   "compare",
			Message: "expected array of compare conditions",
		}
	}

	conditions := make([]*service.CompareCondition, 0, len(compareArray))
	for i, item := range compareArray {
		condMap, ok := item.(map[string]interface{})
		if !ok {
			return nil, &TransactionParseError{
				Field:   fmt.Sprintf("compare[%d]", i),
				Message: "expected object",
			}
		}

		cond, err := parseCompareCondition(condMap, i)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, cond)
	}

	return conditions, nil
}

// parseCompareCondition parses a single compare condition from a map.
func parseCompareCondition(condMap map[string]interface{}, index int) (*service.CompareCondition, error) {
	entityType := getStringAlt(condMap, "entityType", "entity_type")
	id := getIDOrHashKey(entityType, condMap)

	// Parse target
	targetStr := getStringAlt(condMap, "target", "compare_target")
	target, err := parseCompareTarget(targetStr)
	if err != nil {
		return nil, &TransactionParseError{
			Field:   fmt.Sprintf("compare[%d].target", index),
			Message: err.Error(),
		}
	}

	// Parse operator
	operatorStr := getStringAlt(condMap, "operator", "compare_operator")
	operator, err := parseCompareOperator(operatorStr)
	if err != nil {
		return nil, &TransactionParseError{
			Field:   fmt.Sprintf("compare[%d].operator", index),
			Message: err.Error(),
		}
	}

	// Parse version (for version comparisons)
	version := getUint64(condMap, "version")

	// Parse value (for value comparisons)
	var value []byte
	if v := getString(condMap, "value"); v != "" {
		value = []byte(v)
	}

	return &service.CompareCondition{
		EntityType: entityType,
		ID:         id,
		Target:     target,
		Operator:   operator,
		Version:    version,
		Value:      value,
	}, nil
}

// parseCompareTarget converts a string to a CompareTarget constant.
func parseCompareTarget(s string) (service.CompareTarget, error) {
	targetMap := map[string]service.CompareTarget{
		"":        service.CompareTargetUnspecified,
		"version": service.CompareTargetVersion,
		"value":   service.CompareTargetValue,
		"exists":  service.CompareTargetExists,
		"VERSION": service.CompareTargetVersion,
		"VALUE":   service.CompareTargetValue,
		"EXISTS":  service.CompareTargetExists,
		"Version": service.CompareTargetVersion,
		"Value":   service.CompareTargetValue,
		"Exists":  service.CompareTargetExists,
	}

	if target, ok := targetMap[s]; ok {
		return target, nil
	}
	return service.CompareTargetUnspecified, &TransactionInvalidCompareTargetError{Target: s}
}

// parseCompareOperator converts a string to a CompareOperator constant.
func parseCompareOperator(s string) (service.CompareOperator, error) {
	operatorMap := map[string]service.CompareOperator{
		"":      service.CompareOperatorUnspecified,
		"eq":    service.CompareOperatorEqual,
		"ne":    service.CompareOperatorNotEqual,
		"gt":    service.CompareOperatorGreater,
		"lt":    service.CompareOperatorLess,
		"gte":   service.CompareOperatorGreaterEqual,
		"lte":   service.CompareOperatorLessEqual,
		"=":     service.CompareOperatorEqual,
		"!=":    service.CompareOperatorNotEqual,
		">":     service.CompareOperatorGreater,
		"<":     service.CompareOperatorLess,
		">=":    service.CompareOperatorGreaterEqual,
		"<=":    service.CompareOperatorLessEqual,
		"equal": service.CompareOperatorEqual,
		"EQ":    service.CompareOperatorEqual,
		"NE":    service.CompareOperatorNotEqual,
		"GT":    service.CompareOperatorGreater,
		"LT":    service.CompareOperatorLess,
		"GTE":   service.CompareOperatorGreaterEqual,
		"LTE":   service.CompareOperatorLessEqual,
	}

	if operator, ok := operatorMap[s]; ok {
		return operator, nil
	}
	return service.CompareOperatorUnspecified, &TransactionInvalidCompareOperatorError{Operator: s}
}

// parseTransactionOps parses transaction operations from the specified field.
func parseTransactionOps(args map[string]interface{}, field string) ([]*service.TransactionOp, error) {
	opsRaw, ok := args[field]
	if !ok || opsRaw == nil {
		return nil, nil
	}

	opsArray, ok := opsRaw.([]interface{})
	if !ok {
		return nil, &TransactionParseError{
			Field:   field,
			Message: "expected array of operations",
		}
	}

	ops := make([]*service.TransactionOp, 0, len(opsArray))
	for i, item := range opsArray {
		opMap, ok := item.(map[string]interface{})
		if !ok {
			return nil, &TransactionParseError{
				Field:   fmt.Sprintf("%s[%d]", field, i),
				Message: "expected object",
			}
		}

		op, err := parseTransactionOp(opMap, field, i)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}

	return ops, nil
}

// parseTransactionOp parses a single transaction operation from a map.
func parseTransactionOp(opMap map[string]interface{}, field string, index int) (*service.TransactionOp, error) {
	// Parse operation type
	opTypeStr := getStringAlt(opMap, "opType", "op_type")
	opType, err := parseTransactionOpType(opTypeStr)
	if err != nil {
		return nil, &TransactionParseError{
			Field:   fmt.Sprintf("%s[%d].opType", field, index),
			Message: err.Error(),
		}
	}

	entityType := getStringAlt(opMap, "entityType", "entity_type")
	id := getIDOrHashKey(entityType, opMap)

	// Parse data (for put operations)
	var data []byte
	if v := getStringAlt(opMap, "data", "value"); v != "" {
		data = []byte(v)
	}

	return &service.TransactionOp{
		OpType:     opType,
		EntityType: entityType,
		ID:         id,
		Data:       data,
	}, nil
}

// parseTransactionOpType converts a string to a TransactionOpType constant.
func parseTransactionOpType(s string) (service.TransactionOpType, error) {
	opTypeMap := map[string]service.TransactionOpType{
		"":       service.TransactionOpTypeUnspecified,
		"put":    service.TransactionOpTypePut,
		"get":    service.TransactionOpTypeGet,
		"delete": service.TransactionOpTypeDelete,
		"PUT":    service.TransactionOpTypePut,
		"GET":    service.TransactionOpTypeGet,
		"DELETE": service.TransactionOpTypeDelete,
		"Put":    service.TransactionOpTypePut,
		"Get":    service.TransactionOpTypeGet,
		"Delete": service.TransactionOpTypeDelete,
	}

	if opType, ok := opTypeMap[s]; ok {
		return opType, nil
	}
	return service.TransactionOpTypeUnspecified, &TransactionInvalidOpTypeError{OpType: s}
}

func (r *ToolRegistry) executeQuery(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.QueryRequest{
		EntityType:  getString(args, "entityType"),
		IndexName:   getString(args, "indexName"),
		Value:       []byte(getString(args, "value")),
		Consistency: service.ConsistencyLevel(getUint64(args, "consistency")),
		ShardID:     getUint64(args, "shardID"),
	}

	stream := make(chan *service.QueryResponse, 64)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.service.Query(ctx, req, stream)
	}()

	var results []*service.QueryResponse
	for resp := range stream {
		results = append(results, resp)
	}
	if err := <-errCh; err != nil {
		return nil, err
	}

	return marshalResult(results)
}

func (r *ToolRegistry) executeScanIndex(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.ScanRequest{
		EntityType: getString(args, "entityType"),
		IndexName:  getString(args, "indexName"),
		StartKey:   []byte(getString(args, "startKey")),
		EndKey:     []byte(getString(args, "endKey")),
		Limit:      getUint64(args, "limit"),
		ShardID:    getUint64(args, "shardID"),
	}

	stream := make(chan *service.ScanResponse, 64)
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.service.ScanIndex(ctx, req, stream)
	}()

	var results []*service.ScanResponse
	for resp := range stream {
		results = append(results, resp)
	}
	if err := <-errCh; err != nil {
		return nil, err
	}

	return marshalResult(results)
}

func (r *ToolRegistry) executeClusterStatus(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.ClusterStatusRequest{
		IncludeDetails: getBool(args, "includeDetails"),
	}
	resp, err := r.service.ClusterStatus(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeGetLeader(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.GetLeaderRequest{
		ShardID: getUint64(args, "shardID"),
	}
	resp, err := r.service.GetLeader(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeAddNode(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.AddNodeRequest{
		ShardID:  getUint64(args, "shardID"),
		NodeID:   getUint64(args, "nodeID"),
		Address:  getString(args, "address"),
		Identity: getString(args, "identity"),
	}
	resp, err := r.service.AddNode(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeRemoveNode(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.RemoveNodeRequest{
		ShardID: getUint64(args, "shardID"),
		NodeID:  getUint64(args, "nodeID"),
	}
	resp, err := r.service.RemoveNode(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeCreateShard(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	members := make(map[uint64]string)
	if raw, ok := args["initialMembers"]; ok {
		if m, ok := raw.(map[string]interface{}); ok {
			for k, v := range m {
				id, _ := strconv.ParseUint(k, 10, 64)
				if addr, ok := v.(string); ok {
					members[id] = addr
				}
			}
		}
	}

	req := &service.CreateShardRequest{
		ShardID:        getUint64(args, "shardID"),
		InitialMembers: members,
	}
	resp, err := r.service.CreateShard(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeRebalanceCluster(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.RebalanceClusterRequest{
		TargetShardCount: getUint64(args, "targetShardCount"),
		Strategy:         getString(args, "strategy"),
	}
	resp, err := r.service.RebalanceCluster(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeRebalanceStatus(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.GetRebalanceProgressRequest{
		OperationID: getString(args, "operation_id"),
	}
	resp, err := r.service.GetRebalanceProgress(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeBackupCreate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.CreateBackupRequest{
		Name:        getString(args, "name"),
		Description: getString(args, "description"),
		Compression: getString(args, "compression"),
		Encryption:  getString(args, "encryption"),
	}
	resp, err := r.service.CreateBackup(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeBackupList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.ListBackupsRequest{
		Limit:  getUint64(args, "limit"),
		Offset: getUint64(args, "offset"),
	}
	resp, err := r.service.ListBackups(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeBackupRestore(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.RestoreBackupRequest{
		BackupID: getStringAlt(args, "backupID", "backup_id"),
	}
	resp, err := r.service.RestoreBackup(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeBackupDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.DeleteBackupRequest{
		BackupID: getStringAlt(args, "backupID", "backup_id"),
	}
	resp, err := r.service.DeleteBackup(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeBackupVerify(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.VerifyBackupRequest{
		BackupID: getStringAlt(args, "backupID", "backup_id"),
	}
	resp, err := r.service.VerifyBackup(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeLogout(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.LogoutRequest{
		AccessToken: getString(args, "accessToken"),
	}
	resp, err := r.service.Logout(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeIntrospectToken(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.IntrospectTokenRequest{
		Token: getString(args, "token"),
	}
	resp, err := r.service.IntrospectToken(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeRoleCreate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.AuthCreateRoleRequest{
		RoleName:    getString(args, "roleName"),
		Permissions: getStringSlice(args, "permissions"),
	}
	resp, err := r.service.CreateAuthRole(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeRoleDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.AuthDeleteRoleRequest{
		RoleName: getString(args, "roleName"),
	}
	resp, err := r.service.DeleteAuthRole(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeRoleList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.AuthListRolesRequest{}
	resp, err := r.service.ListAuthRoles(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeRoleAssign(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.AssignRoleRequest{
		Username: getString(args, "username"),
		RoleName: getString(args, "roleName"),
	}
	resp, err := r.service.AssignRole(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeRoleRevoke(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.RevokeRoleRequest{
		Username: getString(args, "username"),
		RoleName: getString(args, "roleName"),
	}
	resp, err := r.service.RevokeRole(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeCacheStats(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.GetCacheStatsRequest{
		EntityType: getString(args, "entityType"),
	}
	resp, err := r.service.GetCacheStats(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeCacheClear(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.ClearCacheRequest{
		EntityType: getString(args, "entityType"),
		All:        getBool(args, "all"),
	}
	resp, err := r.service.ClearCache(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeCacheConfig(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
	req := &service.GetCacheConfigRequest{}
	resp, err := r.service.GetCacheConfig(ctx, req)
	if err != nil {
		return nil, err
	}

	// Return with expected JSON field names
	result := map[string]interface{}{
		"l1_size":                  resp.L1Size,
		"l1_ttl_seconds":           int64(resp.L1TTL.Seconds()),
		"cleanup_interval_seconds": int64(resp.CleanupInterval.Seconds()),
		"enabled":                  resp.Enabled,
	}
	return marshalResult(result)
}

func (r *ToolRegistry) executeCacheConfigUpdate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.UpdateCacheConfigRequest{}

	if l1Size, ok := args["l1_size"].(float64); ok {
		size := int(l1Size)
		req.L1Size = &size
	}

	if ttlSeconds, ok := args["l1_ttl_seconds"].(float64); ok {
		ttl := time.Duration(ttlSeconds) * time.Second
		req.L1TTL = &ttl
	}

	resp, err := r.service.UpdateCacheConfig(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeHealth(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.HealthRequest{
		IncludeDetails: getBool(args, "includeDetails"),
	}
	resp, err := r.service.Health(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeLiveness(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.LivenessRequest{}
	resp, err := r.service.Liveness(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeReadiness(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.ReadinessRequest{}
	resp, err := r.service.Readiness(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeAggregate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.AggregateRequest{
		EntityType: getString(args, "entityType"),
		FieldName:  getString(args, "fieldName"),
		Function:   service.AggregateFunction(getUint64(args, "function")),
		StartTime:  getInt64(args, "startTime"),
		EndTime:    getInt64(args, "endTime"),
		Interval:   getUint64(args, "interval"),
	}
	resp, err := r.service.Aggregate(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeMigrateData(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	ints := newIntArgReader(args)
	req := &service.MigrateDataRequest{
		SourceShard: ints.Uint64("sourceShard"),
		TargetShard: ints.Uint64("targetShard"),
		EntityTypes: getStringSlice(args, "entityTypes"),
		Concurrency: ints.Int("concurrency"),
	}
	if err := ints.Err(); err != nil {
		return nil, err
	}
	resp, err := r.service.MigrateData(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeMigrationProgress(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.GetMigrationProgressRequest{
		MigrationID: getString(args, "migrationID"),
	}
	resp, err := r.service.GetMigrationProgress(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeReplaceNode(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.ReplaceNodeRequest{
		ShardID:    getUint64(args, "shardID"),
		OldNodeID:  getUint64(args, "oldNodeID"),
		NewNodeID:  getUint64(args, "newNodeID"),
		NewAddress: getString(args, "newAddress"),
	}
	resp, err := r.service.ReplaceNode(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeUpgradeNode(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.UpgradeNodeRequest{
		NodeID:        getUint64(args, "nodeID"),
		TargetVersion: getString(args, "targetVersion"),
	}
	resp, err := r.service.UpgradeNode(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}
