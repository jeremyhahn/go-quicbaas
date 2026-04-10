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
	"encoding/base64"
	"encoding/hex"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// registerObjectStorageTools registers all object storage MCP tools.
func (r *ToolRegistry) registerObjectStorageTools() {
	// Object CRUD Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_put",
		Description: "Store an object in QuicBaaS object storage with optional metadata and policies",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Object key in namespace:path format",
				},
				"value": map[string]interface{}{
					"type":        "string",
					"description": "Object data (base64 encoded for binary)",
				},
				"content_type": map[string]interface{}{
					"type":        "string",
					"description": "MIME type (e.g., application/json, text/plain)",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for multi-tenancy",
				},
				"tags": map[string]interface{}{
					"type":        "object",
					"description": "User-defined key-value tags",
				},
				"target_backend": map[string]interface{}{
					"type":        "string",
					"description": "Storage backend name (default: auto-selected)",
				},
			},
			"required": []string{"key", "value"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_get",
		Description: "Retrieve an object from QuicBaaS object storage",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Object key to retrieve",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control",
				},
				"include_metadata": map[string]interface{}{
					"type":        "boolean",
					"description": "Include metadata in response (default: true)",
					"default":     true,
				},
			},
			"required": []string{"key"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_delete",
		Description: "Delete an object from QuicBaaS object storage",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Object key to delete",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control",
				},
			},
			"required": []string{"key"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_head",
		Description: "Retrieve object metadata without downloading the data",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Object key",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control",
				},
			},
			"required": []string{"key"},
		},
	})

	// Batch Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_batch_put",
		Description: "Store multiple objects in QuicBaaS object storage in a single request",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"objects": map[string]interface{}{
					"type":        "array",
					"description": "Array of objects to store",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"key": map[string]interface{}{
								"type":        "string",
								"description": "Object key",
							},
							"value": map[string]interface{}{
								"type":        "string",
								"description": "Object data (base64 encoded for binary)",
							},
							"content_type": map[string]interface{}{
								"type":        "string",
								"description": "MIME type",
							},
							"tags": map[string]interface{}{
								"type":        "object",
								"description": "User-defined key-value tags",
							},
						},
						"required": []string{"key", "value"},
					},
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for multi-tenancy",
				},
				"target_backend": map[string]interface{}{
					"type":        "string",
					"description": "Storage backend name (default: auto-selected)",
				},
			},
			"required": []string{"objects"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_batch_get",
		Description: "Retrieve multiple objects from QuicBaaS object storage in a single request",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"keys": map[string]interface{}{
					"type":        "array",
					"description": "Array of object keys to retrieve",
					"items":       map[string]interface{}{"type": "string"},
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control",
				},
				"include_metadata": map[string]interface{}{
					"type":        "boolean",
					"description": "Include metadata in response (default: true)",
					"default":     true,
				},
			},
			"required": []string{"keys"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_batch_delete",
		Description: "Delete multiple objects from QuicBaaS object storage in a single request",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"keys": map[string]interface{}{
					"type":        "array",
					"description": "Array of object keys to delete",
					"items":       map[string]interface{}{"type": "string"},
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control",
				},
			},
			"required": []string{"keys"},
		},
	})

	// Listing Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_list",
		Description: "List objects matching a prefix with pagination",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prefix": map[string]interface{}{
					"type":        "string",
					"description": "Key prefix to filter objects",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum results (default: 100, max: 1000)",
					"default":     100,
				},
				"cursor": map[string]interface{}{
					"type":        "string",
					"description": "Pagination cursor from previous response",
				},
				"include_tags": map[string]interface{}{
					"type":        "boolean",
					"description": "Include tags in results",
					"default":     false,
				},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_list_by_tag",
		Description: "List objects with a specific tag",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tag_name": map[string]interface{}{
					"type":        "string",
					"description": "Tag name to filter by",
				},
				"tag_value": map[string]interface{}{
					"type":        "string",
					"description": "Tag value to match (optional)",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum results (default: 100)",
					"default":     100,
				},
				"cursor": map[string]interface{}{
					"type":        "string",
					"description": "Pagination cursor",
				},
			},
			"required": []string{"tag_name"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_list_by_tenant",
		Description: "List all objects owned by a specific tenant",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID to list objects for",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum results (default: 100)",
					"default":     100,
				},
				"cursor": map[string]interface{}{
					"type":        "string",
					"description": "Pagination cursor",
				},
				"include_tags": map[string]interface{}{
					"type":        "boolean",
					"description": "Include tags in results",
					"default":     false,
				},
			},
			"required": []string{"tenant_id"},
		},
	})

	// Copy Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_copy",
		Description: "Copy an object to a new location",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"source_key": map[string]interface{}{
					"type":        "string",
					"description": "Source object key",
				},
				"destination_key": map[string]interface{}{
					"type":        "string",
					"description": "Destination object key",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for destination",
				},
				"overwrite_existing": map[string]interface{}{
					"type":        "boolean",
					"description": "Allow overwriting existing object",
					"default":     false,
				},
				"target_backend": map[string]interface{}{
					"type":        "string",
					"description": "Target storage backend",
				},
			},
			"required": []string{"source_key", "destination_key"},
		},
	})

	// Replication & Migration Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_replicate",
		Description: "Replicate an object to target storage backends",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Object key to replicate",
				},
				"targets": map[string]interface{}{
					"type":        "array",
					"description": "Target backend names",
					"items":       map[string]interface{}{"type": "string"},
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control",
				},
				"synchronous": map[string]interface{}{
					"type":        "boolean",
					"description": "Wait for replication to complete",
					"default":     false,
				},
				"priority": map[string]interface{}{
					"type":        "integer",
					"description": "Replication priority (1-10)",
					"default":     5,
				},
			},
			"required": []string{"key", "targets"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_migrate",
		Description: "Migrate an object to a different storage backend",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Object key to migrate",
				},
				"target_backend": map[string]interface{}{
					"type":        "string",
					"description": "Destination storage backend",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control",
				},
				"delete_from_source": map[string]interface{}{
					"type":        "boolean",
					"description": "Delete from source after migration",
					"default":     true,
				},
			},
			"required": []string{"key", "target_backend"},
		},
	})

	// Archive & Restore Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_archive",
		Description: "Move an object to archive storage tier",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Object key to archive",
				},
				"target_store": map[string]interface{}{
					"type":        "string",
					"description": "Archive storage backend",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control",
				},
				"delete_from_primary": map[string]interface{}{
					"type":        "boolean",
					"description": "Delete from primary storage after archiving",
					"default":     true,
				},
			},
			"required": []string{"key"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_restore",
		Description: "Restore an archived object to active storage",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Object key to restore",
				},
				"target_backend": map[string]interface{}{
					"type":        "string",
					"description": "Target storage backend for restore",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control",
				},
			},
			"required": []string{"key"},
		},
	})

	// Verification Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_verify",
		Description: "Verify object integrity across storage backends",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Object key to verify",
				},
				"stores": map[string]interface{}{
					"type":        "array",
					"description": "Backends to verify (empty = all)",
					"items":       map[string]interface{}{"type": "string"},
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control",
				},
				"repair_mismatches": map[string]interface{}{
					"type":        "boolean",
					"description": "Automatically repair mismatches",
					"default":     false,
				},
			},
			"required": []string{"key"},
		},
	})

	// Policy Management Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_set_policy",
		Description: "Set replication, archive, and retention policies for an object",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Object key",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control",
				},
				"replication": map[string]interface{}{
					"type":        "object",
					"description": "Replication policy settings",
					"properties": map[string]interface{}{
						"enabled":       map[string]interface{}{"type": "boolean"},
						"min_replicas":  map[string]interface{}{"type": "integer"},
						"target_stores": map[string]interface{}{"type": "array"},
						"mode":          map[string]interface{}{"type": "string"},
						"priority":      map[string]interface{}{"type": "integer"},
					},
				},
				"archive": map[string]interface{}{
					"type":        "object",
					"description": "Archive policy settings",
					"properties": map[string]interface{}{
						"enabled":             map[string]interface{}{"type": "boolean"},
						"after_days":          map[string]interface{}{"type": "integer"},
						"target_store":        map[string]interface{}{"type": "string"},
						"delete_from_primary": map[string]interface{}{"type": "boolean"},
					},
				},
				"retention": map[string]interface{}{
					"type":        "object",
					"description": "Retention policy settings",
					"properties": map[string]interface{}{
						"enabled":    map[string]interface{}{"type": "boolean"},
						"days":       map[string]interface{}{"type": "integer"},
						"legal_hold": map[string]interface{}{"type": "boolean"},
						"mode":       map[string]interface{}{"type": "string"},
					},
				},
			},
			"required": []string{"key"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_get_policy",
		Description: "Get policies for an object",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Object key",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control",
				},
			},
			"required": []string{"key"},
		},
	})

	// Backend Management Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_list_backends",
		Description: "List available storage backends",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"include_health": map[string]interface{}{
					"type":        "boolean",
					"description": "Include health status",
					"default":     false,
				},
				"filter_by_type": map[string]interface{}{
					"type":        "string",
					"description": "Filter by backend type (s3, gcs, local, etc.)",
				},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_backend_health",
		Description: "Get health status for a storage backend",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Backend name",
				},
			},
			"required": []string{"name"},
		},
	})

	// Tenant Management Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_tenant_usage",
		Description: "Get storage usage statistics for a tenant",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
			},
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_tenant_quotas",
		Description: "Get quota settings and usage for a tenant",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
			},
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_set_tenant_quotas",
		Description: "Set quota limits for a tenant",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"max_storage_bytes": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum storage quota in bytes (0 = unlimited)",
				},
				"max_object_size": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum object size in bytes (0 = unlimited)",
				},
				"max_object_count": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of objects (0 = unlimited)",
				},
			},
			"required": []string{"tenant_id"},
		},
	})
}

// =============================================================================
// Object Storage Service Accessor
// =============================================================================

// getObjectStorageService retrieves the ObjectStorageService from the service.
func (r *ToolRegistry) getObjectStorageService() (service.ObjectStorageService, error) {
	svc := r.service.ObjectStorageService()
	if svc == nil {
		return nil, api.NewServiceUnavailableError("object storage")
	}
	return svc, nil
}

// =============================================================================
// Object CRUD Operations
// =============================================================================

func (r *ToolRegistry) executeObjectPut(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	key := getString(args, "key")
	if key == "" {
		return nil, api.NewRequiredFieldError("key")
	}

	valueStr := getString(args, "value")
	if valueStr == "" {
		return nil, api.NewRequiredFieldError("value")
	}

	// Try to decode as base64, otherwise use as-is
	value, err := base64.StdEncoding.DecodeString(valueStr)
	if err != nil {
		value = []byte(valueStr)
	}

	req := &service.PutObjectRequest{
		Key:           key,
		Value:         value,
		ContentType:   getString(args, "content_type"),
		TenantID:      getString(args, "tenant_id"),
		TargetBackend: getString(args, "target_backend"),
	}

	// Extract tags
	if tagsRaw, ok := args["tags"].(map[string]interface{}); ok {
		req.Tags = make(map[string]string)
		for k, v := range tagsRaw {
			if s, ok := v.(string); ok {
				req.Tags[k] = s
			}
		}
	}

	resp, err := svc.PutObject(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"key":        resp.Key,
		"object_id":  resp.ObjectID,
		"checksum":   hex.EncodeToString(resp.Checksum),
		"size":       resp.Size,
		"backend":    resp.Backend,
		"version":    resp.Version,
		"created_at": resp.CreatedAt,
		"timestamp":  time.Now().Unix(),
	})
}

func (r *ToolRegistry) executeObjectGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	key := getString(args, "key")
	if key == "" {
		return nil, api.NewRequiredFieldError("key")
	}

	includeMetadata := true
	if v, ok := args["include_metadata"].(bool); ok {
		includeMetadata = v
	}

	req := &service.GetObjectRequest{
		Key:             key,
		TenantID:        getString(args, "tenant_id"),
		IncludeMetadata: includeMetadata,
	}

	resp, err := svc.GetObject(ctx, req)
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"key":          resp.Key,
		"value":        base64.StdEncoding.EncodeToString(resp.Value),
		"content_type": resp.ContentType,
		"size":         resp.Size,
		"checksum":     hex.EncodeToString(resp.Checksum),
		"version":      resp.Version,
		"backend":      resp.Backend,
		"created_at":   resp.CreatedAt,
		"modified_at":  resp.ModifiedAt,
		"timestamp":    time.Now().Unix(),
	}

	if includeMetadata && resp.Tags != nil {
		result["tags"] = resp.Tags
	}
	if resp.TenantID != "" {
		result["tenant_id"] = resp.TenantID
	}

	return marshalResult(result)
}

func (r *ToolRegistry) executeObjectDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	key := getString(args, "key")
	if key == "" {
		return nil, api.NewRequiredFieldError("key")
	}

	req := &service.DeleteObjectRequest{
		Key:      key,
		TenantID: getString(args, "tenant_id"),
	}

	resp, err := svc.DeleteObject(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"key":       resp.Key,
		"success":   resp.Success,
		"size":      resp.Size,
		"timestamp": resp.Timestamp,
	})
}

func (r *ToolRegistry) executeObjectHead(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	key := getString(args, "key")
	if key == "" {
		return nil, api.NewRequiredFieldError("key")
	}

	req := &service.HeadObjectRequest{
		Key:      key,
		TenantID: getString(args, "tenant_id"),
	}

	resp, err := svc.HeadObject(ctx, req)
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"key":       resp.Key,
		"exists":    resp.Exists,
		"timestamp": time.Now().Unix(),
	}

	if resp.Exists {
		result["content_type"] = resp.ContentType
		result["size"] = resp.Size
		result["checksum"] = hex.EncodeToString(resp.Checksum)
		result["version"] = resp.Version
		result["status"] = resp.Status
		result["backend"] = resp.Backend
		result["replica_count"] = resp.ReplicaCount
		result["created_at"] = resp.CreatedAt
		result["modified_at"] = resp.ModifiedAt
		result["last_access_at"] = resp.LastAccessAt
		if resp.Tags != nil {
			result["tags"] = resp.Tags
		}
		if resp.TenantID != "" {
			result["tenant_id"] = resp.TenantID
		}
	}

	return marshalResult(result)
}

// =============================================================================
// Batch Operations
// =============================================================================

func (r *ToolRegistry) executeObjectBatchPut(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	objectsRaw, ok := args["objects"].([]interface{})
	if !ok || len(objectsRaw) == 0 {
		return nil, api.NewEmptyCollectionError("objects")
	}

	tenantID := getString(args, "tenant_id")
	targetBackend := getString(args, "target_backend")

	results := make([]map[string]interface{}, 0, len(objectsRaw))
	successCount := 0
	failCount := 0

	for _, objRaw := range objectsRaw {
		objMap, ok := objRaw.(map[string]interface{})
		if !ok {
			failCount++
			results = append(results, map[string]interface{}{
				"success": false,
				"error":   "invalid object format",
			})
			continue
		}

		key := getStringFromMap(objMap, "key")
		valueStr := getStringFromMap(objMap, "value")

		if key == "" || valueStr == "" {
			failCount++
			results = append(results, map[string]interface{}{
				"key":     key,
				"success": false,
				"error":   "key and value are required",
			})
			continue
		}

		// Try to decode as base64, otherwise use as-is
		value, decErr := base64.StdEncoding.DecodeString(valueStr)
		if decErr != nil {
			value = []byte(valueStr)
		}

		req := &service.PutObjectRequest{
			Key:           key,
			Value:         value,
			ContentType:   getStringFromMap(objMap, "content_type"),
			TenantID:      tenantID,
			TargetBackend: targetBackend,
		}

		// Extract tags
		if tagsRaw, ok := objMap["tags"].(map[string]interface{}); ok {
			req.Tags = make(map[string]string)
			for k, v := range tagsRaw {
				if s, ok := v.(string); ok {
					req.Tags[k] = s
				}
			}
		}

		resp, err := svc.PutObject(ctx, req)
		if err != nil {
			failCount++
			results = append(results, map[string]interface{}{
				"key":     key,
				"success": false,
				"error":   err.Error(),
			})
			continue
		}

		successCount++
		results = append(results, map[string]interface{}{
			"key":       resp.Key,
			"object_id": resp.ObjectID,
			"checksum":  hex.EncodeToString(resp.Checksum),
			"size":      resp.Size,
			"backend":   resp.Backend,
			"success":   true,
		})
	}

	return marshalResult(map[string]interface{}{
		"results":       results,
		"success_count": successCount,
		"fail_count":    failCount,
		"total":         len(objectsRaw),
		"timestamp":     time.Now().Unix(),
	})
}

func (r *ToolRegistry) executeObjectBatchGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	keysRaw, ok := args["keys"].([]interface{})
	if !ok || len(keysRaw) == 0 {
		return nil, api.NewEmptyCollectionError("keys")
	}

	tenantID := getString(args, "tenant_id")
	includeMetadata := true
	if v, ok := args["include_metadata"].(bool); ok {
		includeMetadata = v
	}

	results := make([]map[string]interface{}, 0, len(keysRaw))
	successCount := 0
	failCount := 0

	for _, keyRaw := range keysRaw {
		key, ok := keyRaw.(string)
		if !ok || key == "" {
			failCount++
			results = append(results, map[string]interface{}{
				"success": false,
				"error":   "invalid key format",
			})
			continue
		}

		req := &service.GetObjectRequest{
			Key:             key,
			TenantID:        tenantID,
			IncludeMetadata: includeMetadata,
		}

		resp, err := svc.GetObject(ctx, req)
		if err != nil {
			failCount++
			results = append(results, map[string]interface{}{
				"key":     key,
				"success": false,
				"error":   err.Error(),
			})
			continue
		}

		successCount++
		result := map[string]interface{}{
			"key":          resp.Key,
			"value":        base64.StdEncoding.EncodeToString(resp.Value),
			"content_type": resp.ContentType,
			"size":         resp.Size,
			"checksum":     hex.EncodeToString(resp.Checksum),
			"version":      resp.Version,
			"backend":      resp.Backend,
			"success":      true,
		}
		if includeMetadata && resp.Tags != nil {
			result["tags"] = resp.Tags
		}
		results = append(results, result)
	}

	return marshalResult(map[string]interface{}{
		"results":       results,
		"success_count": successCount,
		"fail_count":    failCount,
		"total":         len(keysRaw),
		"timestamp":     time.Now().Unix(),
	})
}

func (r *ToolRegistry) executeObjectBatchDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	keysRaw, ok := args["keys"].([]interface{})
	if !ok || len(keysRaw) == 0 {
		return nil, api.NewEmptyCollectionError("keys")
	}

	tenantID := getString(args, "tenant_id")

	results := make([]map[string]interface{}, 0, len(keysRaw))
	successCount := 0
	failCount := 0
	var totalBytesDeleted int64

	for _, keyRaw := range keysRaw {
		key, ok := keyRaw.(string)
		if !ok || key == "" {
			failCount++
			results = append(results, map[string]interface{}{
				"success": false,
				"error":   "invalid key format",
			})
			continue
		}

		req := &service.DeleteObjectRequest{
			Key:      key,
			TenantID: tenantID,
		}

		resp, err := svc.DeleteObject(ctx, req)
		if err != nil {
			failCount++
			results = append(results, map[string]interface{}{
				"key":     key,
				"success": false,
				"error":   err.Error(),
			})
			continue
		}

		successCount++
		totalBytesDeleted += resp.Size
		results = append(results, map[string]interface{}{
			"key":     resp.Key,
			"size":    resp.Size,
			"success": resp.Success,
		})
	}

	return marshalResult(map[string]interface{}{
		"results":       results,
		"success_count": successCount,
		"fail_count":    failCount,
		"total":         len(keysRaw),
		"bytes_deleted": totalBytesDeleted,
		"timestamp":     time.Now().Unix(),
	})
}

// =============================================================================
// Listing Operations
// =============================================================================

func (r *ToolRegistry) executeObjectList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	req := &service.ListObjectsRequest{
		Prefix:      getString(args, "prefix"),
		TenantID:    getString(args, "tenant_id"),
		Limit:       int(getUint64(args, "limit")), // #nosec G115 -- limit is user-provided pagination value, reasonable size
		Cursor:      getString(args, "cursor"),
		IncludeTags: getBool(args, "include_tags"),
	}

	resp, err := svc.ListObjects(ctx, req)
	if err != nil {
		return nil, err
	}

	objects := make([]map[string]interface{}, 0, len(resp.Objects))
	for _, obj := range resp.Objects {
		item := map[string]interface{}{
			"key":          obj.Key,
			"size":         obj.Size,
			"content_type": obj.ContentType,
			"version":      obj.Version,
			"status":       obj.Status,
			"created_at":   obj.CreatedAt,
			"modified_at":  obj.ModifiedAt,
		}
		if obj.Tags != nil {
			item["tags"] = obj.Tags
		}
		objects = append(objects, item)
	}

	return marshalResult(map[string]interface{}{
		"objects":     objects,
		"next_cursor": resp.NextCursor,
		"has_more":    resp.HasMore,
		"total_count": resp.TotalCount,
		"timestamp":   resp.Timestamp,
	})
}

func (r *ToolRegistry) executeObjectListByTag(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	tagName := getString(args, "tag_name")
	if tagName == "" {
		return nil, api.NewRequiredFieldError("tag_name")
	}

	req := &service.ListByTagRequest{
		TagName:  tagName,
		TagValue: getString(args, "tag_value"),
		TenantID: getString(args, "tenant_id"),
		Limit:    int(getUint64(args, "limit")), // #nosec G115 -- limit is user-provided pagination value, reasonable size
		Cursor:   getString(args, "cursor"),
	}

	resp, err := svc.ListByTag(ctx, req)
	if err != nil {
		return nil, err
	}

	objects := make([]map[string]interface{}, 0, len(resp.Objects))
	for _, obj := range resp.Objects {
		objects = append(objects, map[string]interface{}{
			"key":          obj.Key,
			"size":         obj.Size,
			"content_type": obj.ContentType,
			"version":      obj.Version,
			"status":       obj.Status,
			"created_at":   obj.CreatedAt,
			"modified_at":  obj.ModifiedAt,
		})
	}

	return marshalResult(map[string]interface{}{
		"objects":     objects,
		"next_cursor": resp.NextCursor,
		"has_more":    resp.HasMore,
		"total_count": resp.TotalCount,
		"timestamp":   resp.Timestamp,
	})
}

func (r *ToolRegistry) executeObjectListByTenant(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, api.NewRequiredFieldError("tenant_id")
	}

	req := &service.ListByTenantRequest{
		TenantID:    tenantID,
		Limit:       int(getUint64(args, "limit")), // #nosec G115 -- limit is user-provided pagination value, reasonable size
		Cursor:      getString(args, "cursor"),
		IncludeTags: getBool(args, "include_tags"),
	}

	resp, err := svc.ListByTenant(ctx, req)
	if err != nil {
		return nil, err
	}

	objects := make([]map[string]interface{}, 0, len(resp.Objects))
	for _, obj := range resp.Objects {
		item := map[string]interface{}{
			"key":          obj.Key,
			"size":         obj.Size,
			"content_type": obj.ContentType,
			"version":      obj.Version,
			"status":       obj.Status,
			"created_at":   obj.CreatedAt,
			"modified_at":  obj.ModifiedAt,
		}
		if obj.Tags != nil {
			item["tags"] = obj.Tags
		}
		objects = append(objects, item)
	}

	return marshalResult(map[string]interface{}{
		"objects":     objects,
		"next_cursor": resp.NextCursor,
		"has_more":    resp.HasMore,
		"total_count": resp.TotalCount,
		"timestamp":   resp.Timestamp,
	})
}

// =============================================================================
// Copy Operations
// =============================================================================

func (r *ToolRegistry) executeObjectCopy(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	sourceKey := getString(args, "source_key")
	if sourceKey == "" {
		return nil, api.NewRequiredFieldError("source_key")
	}

	destKey := getString(args, "destination_key")
	if destKey == "" {
		return nil, api.NewRequiredFieldError("destination_key")
	}

	req := &service.CopyObjectRequest{
		SourceKey:         sourceKey,
		DestinationKey:    destKey,
		TenantID:          getString(args, "tenant_id"),
		OverwriteExisting: getBool(args, "overwrite_existing"),
		TargetBackend:     getString(args, "target_backend"),
	}

	resp, err := svc.CopyObject(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"key":        resp.Key,
		"object_id":  resp.ObjectID,
		"size":       resp.Size,
		"checksum":   hex.EncodeToString(resp.Checksum),
		"version":    resp.Version,
		"backend":    resp.Backend,
		"created_at": resp.CreatedAt,
		"timestamp":  time.Now().Unix(),
	})
}

// =============================================================================
// Replication & Migration Operations
// =============================================================================

func (r *ToolRegistry) executeObjectReplicate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	key := getString(args, "key")
	if key == "" {
		return nil, api.NewRequiredFieldError("key")
	}

	// Extract targets
	var targets []string
	if targetsRaw, ok := args["targets"].([]interface{}); ok {
		for _, t := range targetsRaw {
			if s, ok := t.(string); ok {
				targets = append(targets, s)
			}
		}
	}
	if len(targets) == 0 {
		return nil, api.NewEmptyCollectionError("targets")
	}

	req := &service.ReplicateObjectRequest{
		Key:         key,
		Targets:     targets,
		TenantID:    getString(args, "tenant_id"),
		Synchronous: getBool(args, "synchronous"),
		Priority:    int(getUint64(args, "priority")), // #nosec G115 -- user input validated by service layer
	}

	resp, err := svc.ReplicateObject(ctx, req)
	if err != nil {
		return nil, err
	}

	results := make(map[string]interface{})
	for target, result := range resp.Results {
		results[target] = map[string]interface{}{
			"success":  result.Success,
			"error":    result.Error,
			"duration": result.Duration,
		}
	}

	return marshalResult(map[string]interface{}{
		"key":       resp.Key,
		"status":    resp.Status,
		"results":   results,
		"timestamp": resp.Timestamp,
	})
}

func (r *ToolRegistry) executeObjectMigrate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	key := getString(args, "key")
	if key == "" {
		return nil, api.NewRequiredFieldError("key")
	}

	targetBackend := getString(args, "target_backend")
	if targetBackend == "" {
		return nil, api.NewRequiredFieldError("target_backend")
	}

	deleteFromSource := true
	if v, ok := args["delete_from_source"].(bool); ok {
		deleteFromSource = v
	}

	req := &service.MigrateObjectRequest{
		Key:              key,
		TargetBackend:    targetBackend,
		TenantID:         getString(args, "tenant_id"),
		DeleteFromSource: deleteFromSource,
	}

	resp, err := svc.MigrateObject(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"key":                 resp.Key,
		"source_backend":      resp.SourceBackend,
		"target_backend":      resp.TargetBackend,
		"success":             resp.Success,
		"deleted_from_source": resp.DeletedFromSource,
		"timestamp":           resp.Timestamp,
	})
}

// =============================================================================
// Archive & Restore Operations
// =============================================================================

func (r *ToolRegistry) executeObjectArchive(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	key := getString(args, "key")
	if key == "" {
		return nil, api.NewRequiredFieldError("key")
	}

	deleteFromPrimary := true
	if v, ok := args["delete_from_primary"].(bool); ok {
		deleteFromPrimary = v
	}

	req := &service.ArchiveObjectRequest{
		Key:               key,
		TargetStore:       getString(args, "target_store"),
		TenantID:          getString(args, "tenant_id"),
		DeleteFromPrimary: deleteFromPrimary,
	}

	resp, err := svc.ArchiveObject(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"key":                  resp.Key,
		"archive_store":        resp.ArchiveStore,
		"success":              resp.Success,
		"deleted_from_primary": resp.DeletedFromPrimary,
		"archived_at":          resp.ArchivedAt,
	})
}

func (r *ToolRegistry) executeObjectRestore(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	key := getString(args, "key")
	if key == "" {
		return nil, api.NewRequiredFieldError("key")
	}

	req := &service.RestoreObjectRequest{
		Key:           key,
		TargetBackend: getString(args, "target_backend"),
		TenantID:      getString(args, "tenant_id"),
	}

	resp, err := svc.RestoreObject(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"key":         resp.Key,
		"backend":     resp.Backend,
		"success":     resp.Success,
		"restored_at": resp.RestoredAt,
	})
}

// =============================================================================
// Verification Operations
// =============================================================================

func (r *ToolRegistry) executeObjectVerify(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	key := getString(args, "key")
	if key == "" {
		return nil, api.NewRequiredFieldError("key")
	}

	// Extract stores
	var stores []string
	if storesRaw, ok := args["stores"].([]interface{}); ok {
		for _, s := range storesRaw {
			if str, ok := s.(string); ok {
				stores = append(stores, str)
			}
		}
	}

	req := &service.VerifyObjectRequest{
		Key:              key,
		Stores:           stores,
		TenantID:         getString(args, "tenant_id"),
		RepairMismatches: getBool(args, "repair_mismatches"),
	}

	resp, err := svc.VerifyObject(ctx, req)
	if err != nil {
		return nil, err
	}

	results := make(map[string]interface{})
	for store, result := range resp.Results {
		results[store] = map[string]interface{}{
			"valid":          result.Valid,
			"checksum_match": result.ChecksumMatch,
			"size_match":     result.SizeMatch,
			"error":          result.Error,
		}
	}

	return marshalResult(map[string]interface{}{
		"key":       resp.Key,
		"valid":     resp.Valid,
		"results":   results,
		"repaired":  resp.Repaired,
		"timestamp": resp.Timestamp,
	})
}

// =============================================================================
// Policy Management Operations
// =============================================================================

func (r *ToolRegistry) executeObjectSetPolicy(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	key := getString(args, "key")
	if key == "" {
		return nil, api.NewRequiredFieldError("key")
	}

	req := &service.SetObjectPolicyRequest{
		Key:      key,
		TenantID: getString(args, "tenant_id"),
	}

	// Parse replication policy
	if repRaw, ok := args["replication"].(map[string]interface{}); ok {
		req.ReplicationPolicy = &catalog.ReplicationPolicy{
			Enabled:     getBoolFromMap(repRaw, "enabled"),
			MinReplicas: int(getUint64FromMap(repRaw, "min_replicas")), // #nosec G115 -- user input bounded by service
			Mode:        catalog.ReplicationMode(getStringFromMap(repRaw, "mode")),
			Priority:    int(getUint64FromMap(repRaw, "priority")), // #nosec G115 -- user input bounded by service
		}
		if targets, ok := repRaw["target_stores"].([]interface{}); ok {
			for _, t := range targets {
				if s, ok := t.(string); ok {
					req.ReplicationPolicy.TargetStores = append(req.ReplicationPolicy.TargetStores, s)
				}
			}
		}
	}

	// Parse archive policy
	if archRaw, ok := args["archive"].(map[string]interface{}); ok {
		req.ArchivePolicy = &catalog.ArchivePolicy{
			Enabled:           getBoolFromMap(archRaw, "enabled"),
			ArchiveAfterDays:  int(getUint64FromMap(archRaw, "after_days")), // #nosec G115 -- config value bounded by service
			TargetStore:       getStringFromMap(archRaw, "target_store"),
			DeleteFromPrimary: getBoolFromMap(archRaw, "delete_from_primary"),
		}
	}

	// Parse retention policy
	if retRaw, ok := args["retention"].(map[string]interface{}); ok {
		req.RetentionPolicy = &catalog.RetentionPolicy{
			Enabled:       getBoolFromMap(retRaw, "enabled"),
			RetentionDays: int(getUint64FromMap(retRaw, "days")), // #nosec G115 -- config value bounded by service
		}
	}

	resp, err := svc.SetObjectPolicy(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"key":       resp.Key,
		"success":   resp.Success,
		"version":   resp.Version,
		"timestamp": resp.Timestamp,
	})
}

func (r *ToolRegistry) executeObjectGetPolicy(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	key := getString(args, "key")
	if key == "" {
		return nil, api.NewRequiredFieldError("key")
	}

	req := &service.GetObjectPolicyRequest{
		Key:      key,
		TenantID: getString(args, "tenant_id"),
	}

	resp, err := svc.GetObjectPolicy(ctx, req)
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"key":       resp.Key,
		"timestamp": resp.Timestamp,
	}

	if resp.ReplicationPolicy != nil {
		result["replication"] = map[string]interface{}{
			"enabled":       resp.ReplicationPolicy.Enabled,
			"min_replicas":  resp.ReplicationPolicy.MinReplicas,
			"target_stores": resp.ReplicationPolicy.TargetStores,
			"mode":          string(resp.ReplicationPolicy.Mode),
			"priority":      resp.ReplicationPolicy.Priority,
		}
	}

	if resp.ArchivePolicy != nil {
		result["archive"] = map[string]interface{}{
			"enabled":             resp.ArchivePolicy.Enabled,
			"after_days":          resp.ArchivePolicy.ArchiveAfterDays,
			"target_store":        resp.ArchivePolicy.TargetStore,
			"delete_from_primary": resp.ArchivePolicy.DeleteFromPrimary,
		}
	}

	if resp.RetentionPolicy != nil {
		result["retention"] = map[string]interface{}{
			"enabled": resp.RetentionPolicy.Enabled,
			"days":    resp.RetentionPolicy.RetentionDays,
		}
	}

	return marshalResult(result)
}

// =============================================================================
// Backend Management Operations
// =============================================================================

func (r *ToolRegistry) executeObjectListBackends(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	req := &service.ListBackendsRequest{
		IncludeHealth: getBool(args, "include_health"),
		FilterByType:  getString(args, "filter_by_type"),
	}

	resp, err := svc.ListBackends(ctx, req)
	if err != nil {
		return nil, err
	}

	backends := make([]map[string]interface{}, 0, len(resp.Backends))
	for _, b := range resp.Backends {
		item := map[string]interface{}{
			"name":         b.Name,
			"type":         b.Type,
			"enabled":      b.Enabled,
			"default":      b.Default,
			"tier":         b.Tier,
			"region":       b.Region,
			"object_count": b.ObjectCount,
			"total_size":   b.TotalSize,
		}
		if b.Health != nil {
			item["health"] = map[string]interface{}{
				"healthy":    b.Health.Healthy,
				"latency":    b.Health.Latency,
				"last_check": b.Health.LastCheck,
				"error":      b.Health.Error,
			}
		}
		backends = append(backends, item)
	}

	return marshalResult(map[string]interface{}{
		"backends":  backends,
		"timestamp": resp.Timestamp,
	})
}

func (r *ToolRegistry) executeObjectBackendHealth(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	req := &service.GetBackendHealthRequest{
		Name: name,
	}

	resp, err := svc.GetBackendHealth(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"name":       resp.Name,
		"healthy":    resp.Healthy,
		"latency":    resp.Latency,
		"last_check": resp.LastCheck,
		"status":     resp.Status,
		"error":      resp.Error,
	})
}

// =============================================================================
// Tenant Management Operations
// =============================================================================

func (r *ToolRegistry) executeObjectTenantUsage(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, api.NewRequiredFieldError("tenant_id")
	}

	req := &service.GetTenantUsageRequest{
		TenantID: tenantID,
	}

	resp, err := svc.GetTenantUsage(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"tenant_id":      resp.TenantID,
		"object_count":   resp.ObjectCount,
		"total_size":     resp.TotalSize,
		"active_count":   resp.ActiveCount,
		"archived_count": resp.ArchivedCount,
		"timestamp":      resp.Timestamp,
	})
}

func (r *ToolRegistry) executeObjectTenantQuotas(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, api.NewRequiredFieldError("tenant_id")
	}

	req := &service.GetTenantQuotasRequest{
		TenantID: tenantID,
	}

	resp, err := svc.GetTenantQuotas(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"tenant_id":          resp.TenantID,
		"max_storage_bytes":  resp.MaxStorageBytes,
		"max_object_size":    resp.MaxObjectSize,
		"max_object_count":   resp.MaxObjectCount,
		"used_storage_bytes": resp.UsedStorageBytes,
		"used_object_count":  resp.UsedObjectCount,
		"timestamp":          resp.Timestamp,
	})
}

func (r *ToolRegistry) executeObjectSetTenantQuotas(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageService()
	if err != nil {
		return nil, err
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, api.NewRequiredFieldError("tenant_id")
	}

	req := &service.SetTenantQuotasRequest{
		TenantID:        tenantID,
		MaxStorageBytes: int64(getUint64(args, "max_storage_bytes")),
		MaxObjectSize:   int64(getUint64(args, "max_object_size")),
		MaxObjectCount:  int64(getUint64(args, "max_object_count")),
	}

	resp, err := svc.SetTenantQuotas(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"tenant_id":  resp.TenantID,
		"updated_at": resp.UpdatedAt,
		"success":    true,
	})
}

// =============================================================================
// Helper Functions
// =============================================================================

// getStringFromMap extracts a string from a map.
func getStringFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// getUint64FromMap extracts a uint64 from a map.
func getUint64FromMap(m map[string]interface{}, key string) uint64 {
	switch v := m[key].(type) {
	case float64:
		return uint64(v)
	case int:
		return uint64(v) //nolint:gosec
	case int64:
		return uint64(v) //nolint:gosec
	case uint64:
		return v
	}
	return 0
}

// getBoolFromMap extracts a bool from a map.
func getBoolFromMap(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}
