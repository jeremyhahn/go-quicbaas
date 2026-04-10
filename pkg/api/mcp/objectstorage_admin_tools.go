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
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// registerObjectStorageAdminTools registers all object storage admin MCP tools.
func (r *ToolRegistry) registerObjectStorageAdminTools() {
	// Backend Management Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_admin_create_backend",
		Description: "Create a new storage backend for object storage",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Unique backend name",
				},
				"type": map[string]interface{}{
					"type":        "string",
					"description": "Backend type (local, s3, gcs, azure, memory)",
					"enum":        []string{"local", "s3", "gcs", "azure", "memory"},
				},
				"tier": map[string]interface{}{
					"type":        "string",
					"description": "Storage tier (hot, warm, cold)",
					"enum":        []string{"hot", "warm", "cold"},
				},
				"config": map[string]interface{}{
					"type":        "object",
					"description": "Backend-specific configuration (e.g., bucket, region, endpoint)",
				},
				"enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable the backend immediately (default: true)",
					"default":     true,
				},
			},
			"required": []string{"name", "type", "tier"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_admin_update_backend",
		Description: "Update configuration for an existing storage backend",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Backend name to update",
				},
				"config": map[string]interface{}{
					"type":        "object",
					"description": "Updated configuration (only specified keys are updated)",
				},
				"tier": map[string]interface{}{
					"type":        "string",
					"description": "New storage tier (empty means no change)",
					"enum":        []string{"", "hot", "warm", "cold"},
				},
			},
			"required": []string{"name"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_admin_delete_backend",
		Description: "Remove a storage backend (requires migration of objects)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Backend name to delete",
				},
				"migrate_to": map[string]interface{}{
					"type":        "string",
					"description": "Backend to migrate objects to before deletion",
				},
				"force": map[string]interface{}{
					"type":        "boolean",
					"description": "Force deletion even if objects exist (data loss!)",
					"default":     false,
				},
			},
			"required": []string{"name"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_admin_set_backend_status",
		Description: "Enable or disable a storage backend",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Backend name",
				},
				"enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable (true) or disable (false) the backend",
				},
			},
			"required": []string{"name", "enabled"},
		},
	})

	// Statistics Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_admin_get_storage_stats",
		Description: "Get cluster-wide storage statistics",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"include_backends": map[string]interface{}{
					"type":        "boolean",
					"description": "Include per-backend breakdown",
					"default":     false,
				},
				"include_tenants": map[string]interface{}{
					"type":        "boolean",
					"description": "Include per-tenant breakdown",
					"default":     false,
				},
			},
		},
	})

	// Garbage Collection Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_admin_run_gc",
		Description: "Initiate garbage collection on specified backends",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"backends": map[string]interface{}{
					"type":        "array",
					"description": "Backend names to GC (empty = all backends)",
					"items":       map[string]interface{}{"type": "string"},
				},
				"dry_run": map[string]interface{}{
					"type":        "boolean",
					"description": "Report what would be deleted without deleting",
					"default":     false,
				},
				"aggressive": map[string]interface{}{
					"type":        "boolean",
					"description": "Use aggressive GC settings for maximum reclamation",
					"default":     false,
				},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_admin_get_gc_progress",
		Description: "Get garbage collection progress for an operation",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation_id": map[string]interface{}{
					"type":        "string",
					"description": "GC operation ID from run_gc response",
				},
			},
			"required": []string{"operation_id"},
		},
	})

	// Repair Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_admin_repair_object",
		Description: "Repair an object with inconsistent replicas",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Object key to repair",
				},
				"source_location": map[string]interface{}{
					"type":        "string",
					"description": "Backend with correct data (auto-selects based on checksums if not specified)",
				},
				"timeout_ms": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum time to wait in milliseconds",
					"default":     30000,
				},
			},
			"required": []string{"key"},
		},
	})

	// Compaction Operations
	r.addTool(ToolDefinition{
		Name:        "quicbaas_object_admin_compact_backend",
		Description: "Trigger compaction on a storage backend",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Backend name to compact",
				},
				"aggressive": map[string]interface{}{
					"type":        "boolean",
					"description": "Use aggressive compaction settings",
					"default":     false,
				},
			},
			"required": []string{"name"},
		},
	})
}

// =============================================================================
// Object Storage Admin Service Accessor
// =============================================================================

// getObjectStorageAdminService retrieves the ObjectStorageAdminService from the service.
func (r *ToolRegistry) getObjectStorageAdminService() (service.ObjectStorageAdminService, error) {
	svc := r.service.ObjectStorageAdminService()
	if svc == nil {
		return nil, api.NewServiceUnavailableError("object storage admin")
	}
	return svc, nil
}

// =============================================================================
// Backend Management Operations
// =============================================================================

func (r *ToolRegistry) executeObjectAdminCreateBackend(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageAdminService()
	if err != nil {
		return nil, err
	}

	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	backendType := getString(args, "type")
	if backendType == "" {
		return nil, api.NewRequiredFieldError("type")
	}

	tier := getString(args, "tier")
	if tier == "" {
		return nil, api.NewRequiredFieldError("tier")
	}

	enabled := true
	if v, ok := args["enabled"].(bool); ok {
		enabled = v
	}

	req := &service.CreateBackendRequest{
		Name:    name,
		Type:    service.BackendType(backendType),
		Tier:    service.StorageTier(tier),
		Enabled: enabled,
	}

	// Extract config
	if configRaw, ok := args["config"].(map[string]interface{}); ok {
		req.Config = make(map[string]string)
		for k, v := range configRaw {
			if s, ok := v.(string); ok {
				req.Config[k] = s
			}
		}
	}

	resp, err := svc.CreateBackend(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"name":       resp.Name,
		"created_at": resp.CreatedAt,
		"success":    true,
		"timestamp":  time.Now().Unix(),
	})
}

func (r *ToolRegistry) executeObjectAdminUpdateBackend(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageAdminService()
	if err != nil {
		return nil, err
	}

	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	req := &service.UpdateBackendRequest{
		Name: name,
		Tier: service.StorageTier(getString(args, "tier")),
	}

	// Extract config
	if configRaw, ok := args["config"].(map[string]interface{}); ok {
		req.Config = make(map[string]string)
		for k, v := range configRaw {
			if s, ok := v.(string); ok {
				req.Config[k] = s
			}
		}
	}

	resp, err := svc.UpdateBackend(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"name":       resp.Name,
		"updated_at": resp.UpdatedAt,
		"success":    true,
		"timestamp":  time.Now().Unix(),
	})
}

func (r *ToolRegistry) executeObjectAdminDeleteBackend(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageAdminService()
	if err != nil {
		return nil, err
	}

	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	req := &service.DeleteBackendRequest{
		Name:      name,
		MigrateTo: getString(args, "migrate_to"),
		Force:     getBool(args, "force"),
	}

	resp, err := svc.DeleteBackend(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"name":             resp.Name,
		"deleted":          resp.Deleted,
		"migrated_objects": resp.MigratedObjects,
		"timestamp":        time.Now().Unix(),
	})
}

func (r *ToolRegistry) executeObjectAdminSetBackendStatus(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageAdminService()
	if err != nil {
		return nil, err
	}

	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	enabled, ok := args["enabled"].(bool)
	if !ok {
		return nil, api.NewRequiredFieldError("enabled")
	}

	req := &service.SetBackendStatusRequest{
		Name:    name,
		Enabled: enabled,
	}

	resp, err := svc.SetBackendStatus(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"name":       resp.Name,
		"enabled":    resp.Enabled,
		"updated_at": resp.UpdatedAt,
		"success":    true,
		"timestamp":  time.Now().Unix(),
	})
}

// =============================================================================
// Statistics Operations
// =============================================================================

func (r *ToolRegistry) executeObjectAdminGetStorageStats(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageAdminService()
	if err != nil {
		return nil, err
	}

	req := &service.GetStorageStatsRequest{
		IncludeBackends: getBool(args, "include_backends"),
		IncludeTenants:  getBool(args, "include_tenants"),
	}

	resp, err := svc.GetStorageStats(ctx, req)
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"total_bytes":      resp.TotalBytes,
		"total_objects":    resp.TotalObjects,
		"total_backends":   resp.TotalBackends,
		"healthy_backends": resp.HealthyBackends,
		"calculated_at":    resp.CalculatedAt,
		"timestamp":        time.Now().Unix(),
	}

	// Include backend stats if requested
	if len(resp.BackendStats) > 0 {
		backends := make([]map[string]interface{}, 0, len(resp.BackendStats))
		for _, b := range resp.BackendStats {
			backends = append(backends, map[string]interface{}{
				"name":         b.Name,
				"type":         b.Type,
				"enabled":      b.Enabled,
				"tier":         b.Tier,
				"object_count": b.ObjectCount,
				"total_size":   b.TotalSize,
			})
		}
		result["backend_stats"] = backends
	}

	// Include tenant stats if requested
	if len(resp.TenantStats) > 0 {
		tenants := make([]map[string]interface{}, 0, len(resp.TenantStats))
		for _, t := range resp.TenantStats {
			tenants = append(tenants, map[string]interface{}{
				"tenant_id":     t.TenantID,
				"total_bytes":   t.TotalBytes,
				"object_count":  t.ObjectCount,
				"hot_bytes":     t.HotBytes,
				"warm_bytes":    t.WarmBytes,
				"cold_bytes":    t.ColdBytes,
				"archive_bytes": t.ArchiveBytes,
			})
		}
		result["tenant_stats"] = tenants
	}

	return marshalResult(result)
}

// =============================================================================
// Garbage Collection Operations
// =============================================================================

func (r *ToolRegistry) executeObjectAdminRunGC(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageAdminService()
	if err != nil {
		return nil, err
	}

	req := &service.RunGarbageCollectionRequest{
		DryRun:     getBool(args, "dry_run"),
		Aggressive: getBool(args, "aggressive"),
	}

	// Extract backends
	if backendsRaw, ok := args["backends"].([]interface{}); ok {
		for _, b := range backendsRaw {
			if s, ok := b.(string); ok {
				req.Backends = append(req.Backends, s)
			}
		}
	}

	resp, err := svc.RunGarbageCollection(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"operation_id": resp.OperationID,
		"started_at":   resp.StartedAt,
		"timestamp":    time.Now().Unix(),
	})
}

func (r *ToolRegistry) executeObjectAdminGetGCProgress(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageAdminService()
	if err != nil {
		return nil, err
	}

	operationID := getString(args, "operation_id")
	if operationID == "" {
		return nil, api.NewRequiredFieldError("operation_id")
	}

	req := &service.GetGarbageCollectionProgressRequest{
		OperationID: operationID,
	}

	resp, err := svc.GetGarbageCollectionProgress(ctx, req)
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"operation_id":     resp.OperationID,
		"status":           string(resp.Status),
		"objects_scanned":  resp.ObjectsScanned,
		"objects_deleted":  resp.ObjectsDeleted,
		"bytes_reclaimed":  resp.BytesReclaimed,
		"percent_complete": resp.PercentComplete,
		"timestamp":        time.Now().Unix(),
	}

	if resp.Error != "" {
		result["error"] = resp.Error
	}
	if resp.CompletedAt > 0 {
		result["completed_at"] = resp.CompletedAt
	}

	return marshalResult(result)
}

// =============================================================================
// Repair Operations
// =============================================================================

func (r *ToolRegistry) executeObjectAdminRepairObject(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageAdminService()
	if err != nil {
		return nil, err
	}

	key := getString(args, "key")
	if key == "" {
		return nil, api.NewRequiredFieldError("key")
	}

	req := &service.RepairObjectRequest{
		Key:            key,
		SourceLocation: getString(args, "source_location"),
		TimeoutMs:      int64(getUint64(args, "timeout_ms")),
	}

	resp, err := svc.RepairObject(ctx, req)
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"key":                key,
		"success":            resp.Success,
		"repaired_locations": resp.RepairedLocations,
		"timestamp":          time.Now().Unix(),
	}

	if resp.Error != "" {
		result["error"] = resp.Error
	}

	return marshalResult(result)
}

// =============================================================================
// Compaction Operations
// =============================================================================

func (r *ToolRegistry) executeObjectAdminCompactBackend(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getObjectStorageAdminService()
	if err != nil {
		return nil, err
	}

	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	req := &service.CompactBackendRequest{
		Name:       name,
		Aggressive: getBool(args, "aggressive"),
	}

	resp, err := svc.CompactBackend(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"name":         resp.Name,
		"operation_id": resp.OperationID,
		"started_at":   resp.StartedAt,
		"timestamp":    time.Now().Unix(),
	})
}
