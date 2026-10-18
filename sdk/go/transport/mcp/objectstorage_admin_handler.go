// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	osadmin "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorageadmin"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/internal/osadminconv"
)

func isMCPObjectStorageAdminService(service string) bool {
	return trimServiceName(service) == "ObjectStorageAdmin"
}

// objectStorageAdminVerb holds the argument builder and reply decoder for
// one quicbaas_object_admin_* tool (pkg/api/mcp/objectstorage_admin_tools.go).
// The tools take enum names as strings, so the generic argument conversion,
// which sends the SDK's int32 enums as numbers, cannot be used.
type objectStorageAdminVerb struct {
	arguments func(req interface{}) (map[string]interface{}, bool)
	decode    func(m map[string]interface{}) interface{}
}

var objectStorageAdminVerbs = map[string]objectStorageAdminVerb{
	"CreateBackend":                {objectStorageAdminCreateBackendArgs, decodeObjectStorageAdminCreateBackend},
	"UpdateBackend":                {objectStorageAdminUpdateBackendArgs, decodeObjectStorageAdminUpdateBackend},
	"DeleteBackend":                {objectStorageAdminDeleteBackendArgs, decodeObjectStorageAdminDeleteBackend},
	"SetBackendStatus":             {objectStorageAdminSetBackendStatusArgs, decodeObjectStorageAdminSetBackendStatus},
	"GetStorageStats":              {objectStorageAdminGetStorageStatsArgs, decodeObjectStorageAdminGetStorageStats},
	"RunGarbageCollection":         {objectStorageAdminRunGarbageCollectionArgs, decodeObjectStorageAdminRunGarbageCollection},
	"GetGarbageCollectionProgress": {objectStorageAdminGCProgressArgs, decodeObjectStorageAdminGCProgress},
	"RepairObject":                 {objectStorageAdminRepairObjectArgs, decodeObjectStorageAdminRepairObject},
	"CompactBackend":               {objectStorageAdminCompactBackendArgs, decodeObjectStorageAdminCompactBackend},
}

func objectStorageAdminOp(method string) string {
	return "mcp.objectstorageadmin." + method
}

func objectStorageAdminVerbFor(method string) (objectStorageAdminVerb, error) {
	verb, ok := objectStorageAdminVerbs[method]
	if !ok {
		return objectStorageAdminVerb{}, errors.New(errors.CodeUnimplemented, objectStorageAdminOp(method),
			"ObjectStorageAdmin method is not served by the MCP API")
	}
	return verb, nil
}

// objectStorageAdminArguments builds the tool arguments for method.
func objectStorageAdminArguments(method string, req interface{}) (map[string]interface{}, error) {
	verb, err := objectStorageAdminVerbFor(method)
	if err != nil {
		return nil, err
	}
	args, ok := verb.arguments(req)
	if !ok {
		return nil, errors.NewInvalidInput(objectStorageAdminOp(method), "request is required")
	}
	return args, nil
}

// objectStorageAdminResponse decodes the tool result for method into the
// objectstorageadmin client's reply type.
func objectStorageAdminResponse(method string, result interface{}) (interface{}, error) {
	verb, err := objectStorageAdminVerbFor(method)
	if err != nil {
		return nil, err
	}
	m, err := asResultMap(result, objectStorageAdminOp(method))
	if err != nil {
		return nil, err
	}
	return verb.decode(m), nil
}

func objectStorageAdminConfig(config map[string]string) map[string]interface{} {
	out := make(map[string]interface{}, len(config))
	for k, v := range config {
		out[k] = v
	}
	return out
}

func objectStorageAdminCreateBackendArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*osadmin.CreateBackendRequest)
	if !ok || r == nil {
		return nil, false
	}
	return map[string]interface{}{
		"name":    r.Name,
		"type":    osadminconv.BackendTypeName(r.Type),
		"tier":    osadminconv.StorageTierName(r.Tier),
		"enabled": r.Enabled,
		"config":  objectStorageAdminConfig(r.Config),
	}, true
}

func objectStorageAdminUpdateBackendArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*osadmin.UpdateBackendRequest)
	if !ok || r == nil {
		return nil, false
	}
	args := map[string]interface{}{"name": r.Name}
	if tier := osadminconv.StorageTierName(r.Tier); tier != "" {
		args["tier"] = tier
	}
	if r.Config != nil {
		args["config"] = objectStorageAdminConfig(r.Config)
	}
	return args, true
}

func objectStorageAdminDeleteBackendArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*osadmin.DeleteBackendRequest)
	if !ok || r == nil {
		return nil, false
	}
	return map[string]interface{}{"name": r.Name, "migrate_to": r.MigrateTo, "force": r.Force}, true
}

func objectStorageAdminSetBackendStatusArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*osadmin.SetBackendStatusRequest)
	if !ok || r == nil {
		return nil, false
	}
	return map[string]interface{}{"name": r.Name, "enabled": r.Enabled}, true
}

func objectStorageAdminGetStorageStatsArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*osadmin.GetStorageStatsRequest)
	if !ok || r == nil {
		return nil, false
	}
	return map[string]interface{}{
		"include_backends": r.IncludeBackends,
		"include_tenants":  r.IncludeTenants,
	}, true
}

func objectStorageAdminRunGarbageCollectionArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*osadmin.RunGarbageCollectionRequest)
	if !ok || r == nil {
		return nil, false
	}
	backends := make([]interface{}, 0, len(r.Backends))
	for _, b := range r.Backends {
		backends = append(backends, b)
	}
	return map[string]interface{}{
		"backends":   backends,
		"dry_run":    r.DryRun,
		"aggressive": r.Aggressive,
	}, true
}

func objectStorageAdminGCProgressArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*osadmin.GetGarbageCollectionProgressRequest)
	if !ok || r == nil {
		return nil, false
	}
	return map[string]interface{}{"operation_id": r.OperationID}, true
}

func objectStorageAdminRepairObjectArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*osadmin.RepairObjectRequest)
	if !ok || r == nil {
		return nil, false
	}
	args := map[string]interface{}{"key": r.Key, "source_location": r.SourceLocation}
	if r.TimeoutMs > 0 {
		args["timeout_ms"] = r.TimeoutMs
	}
	return args, true
}

func objectStorageAdminCompactBackendArgs(req interface{}) (map[string]interface{}, bool) {
	r, ok := req.(*osadmin.CompactBackendRequest)
	if !ok || r == nil {
		return nil, false
	}
	return map[string]interface{}{"name": r.Name, "aggressive": r.Aggressive}, true
}

func decodeObjectStorageAdminCreateBackend(m map[string]interface{}) interface{} {
	return &osadmin.CreateBackendResponse{
		Name:      getResultString(m, "name"),
		CreatedAt: getResultInt64(m, "created_at"),
	}
}

func decodeObjectStorageAdminUpdateBackend(m map[string]interface{}) interface{} {
	return &osadmin.UpdateBackendResponse{
		Name:      getResultString(m, "name"),
		UpdatedAt: getResultInt64(m, "updated_at"),
	}
}

func decodeObjectStorageAdminDeleteBackend(m map[string]interface{}) interface{} {
	return &osadmin.DeleteBackendResponse{
		Name:            getResultString(m, "name"),
		Deleted:         getResultBool(m, "deleted"),
		MigratedObjects: getResultInt64(m, "migrated_objects"),
	}
}

func decodeObjectStorageAdminSetBackendStatus(m map[string]interface{}) interface{} {
	return &osadmin.SetBackendStatusResponse{
		Name:      getResultString(m, "name"),
		Enabled:   getResultBool(m, "enabled"),
		UpdatedAt: getResultInt64(m, "updated_at"),
	}
}

// objectStorageAdminResultMaps returns the JSON objects listed under key.
func objectStorageAdminResultMaps(m map[string]interface{}, key string) []map[string]interface{} {
	items, _ := m[key].([]interface{})
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if entry, ok := item.(map[string]interface{}); ok {
			out = append(out, entry)
		}
	}
	return out
}

func decodeObjectStorageAdminGetStorageStats(m map[string]interface{}) interface{} {
	resp := &osadmin.GetStorageStatsResponse{
		TotalBytes:      getResultInt64(m, "total_bytes"),
		TotalObjects:    getResultInt64(m, "total_objects"),
		TotalBackends:   int32(getResultInt64(m, "total_backends")),
		HealthyBackends: int32(getResultInt64(m, "healthy_backends")),
		CalculatedAt:    getResultInt64(m, "calculated_at"),
	}
	for _, b := range objectStorageAdminResultMaps(m, "backend_stats") {
		resp.BackendStats = append(resp.BackendStats, &osadmin.BackendInfo{
			Name:        getResultString(b, "name"),
			Type:        osadminconv.ParseBackendType(getResultString(b, "type")),
			Enabled:     getResultBool(b, "enabled"),
			Tier:        osadminconv.ParseStorageTier(getResultString(b, "tier")),
			ObjectCount: getResultInt64(b, "object_count"),
			UsedBytes:   getResultInt64(b, "total_size"),
		})
	}
	for _, t := range objectStorageAdminResultMaps(m, "tenant_stats") {
		resp.TenantStats = append(resp.TenantStats, &osadmin.TenantUsage{
			TenantID:     getResultString(t, "tenant_id"),
			TotalBytes:   getResultInt64(t, "total_bytes"),
			ObjectCount:  getResultInt64(t, "object_count"),
			HotBytes:     getResultInt64(t, "hot_bytes"),
			WarmBytes:    getResultInt64(t, "warm_bytes"),
			ColdBytes:    getResultInt64(t, "cold_bytes"),
			ArchiveBytes: getResultInt64(t, "archive_bytes"),
		})
	}
	return resp
}

func decodeObjectStorageAdminRunGarbageCollection(m map[string]interface{}) interface{} {
	return &osadmin.RunGarbageCollectionResponse{
		OperationID: getResultString(m, "operation_id"),
		StartedAt:   getResultInt64(m, "started_at"),
	}
}

func decodeObjectStorageAdminGCProgress(m map[string]interface{}) interface{} {
	return &osadmin.GetGarbageCollectionProgressResponse{
		OperationID:     getResultString(m, "operation_id"),
		Status:          osadmin.GarbageCollectionStatus(getResultString(m, "status")),
		ObjectsScanned:  getResultInt64(m, "objects_scanned"),
		ObjectsDeleted:  getResultInt64(m, "objects_deleted"),
		BytesReclaimed:  getResultInt64(m, "bytes_reclaimed"),
		PercentComplete: getResultFloat64(m, "percent_complete"),
	}
}

func decodeObjectStorageAdminRepairObject(m map[string]interface{}) interface{} {
	resp := &osadmin.RepairObjectResponse{
		Key:     getResultString(m, "key"),
		Success: getResultBool(m, "success"),
	}
	locations, _ := m["repaired_locations"].([]interface{})
	for _, loc := range locations {
		if s, ok := loc.(string); ok {
			resp.RepairedLocations = append(resp.RepairedLocations, s)
		}
	}
	return resp
}

func decodeObjectStorageAdminCompactBackend(m map[string]interface{}) interface{} {
	return &osadmin.CompactBackendResponse{
		Name:        getResultString(m, "name"),
		OperationID: getResultString(m, "operation_id"),
		StartedAt:   getResultInt64(m, "started_at"),
	}
}
