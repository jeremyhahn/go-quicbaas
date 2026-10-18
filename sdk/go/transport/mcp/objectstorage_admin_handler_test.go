// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"encoding/json"
	"reflect"
	"testing"

	osadmin "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorageadmin"
)

// wireResult round-trips a server tool result through JSON, as the MCP
// transport receives it.
func wireResult(t *testing.T, result map[string]interface{}) interface{} {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return decoded
}

// TestObjectStorageAdminVerbs checks, per verb, the tool name, the
// arguments the server tool reads and the typed reply decoded from the
// result keys pkg/api/mcp/objectstorage_admin_tools.go writes.
func TestObjectStorageAdminVerbs(t *testing.T) {
	tests := []struct {
		method   string
		tool     string
		req      interface{}
		wantArgs map[string]interface{}
		result   map[string]interface{}
		want     interface{}
	}{
		{
			method: "CreateBackend", tool: "quicbaas_object_admin_create_backend",
			req: &osadmin.CreateBackendRequest{Name: "b1", Type: osadmin.BackendTypeS3, Tier: osadmin.StorageTierHot,
				Enabled: true, Config: map[string]string{"bucket": "x"}},
			wantArgs: map[string]interface{}{"name": "b1", "type": "s3", "tier": "hot", "enabled": true,
				"config": map[string]interface{}{"bucket": "x"}},
			result: map[string]interface{}{"name": "b1", "created_at": 7, "success": true},
			want:   &osadmin.CreateBackendResponse{Name: "b1", CreatedAt: 7},
		},
		{
			method: "UpdateBackend", tool: "quicbaas_object_admin_update_backend",
			req:      &osadmin.UpdateBackendRequest{Name: "b1", Tier: osadmin.StorageTierCold},
			wantArgs: map[string]interface{}{"name": "b1", "tier": "cold"},
			result:   map[string]interface{}{"name": "b1", "updated_at": 8},
			want:     &osadmin.UpdateBackendResponse{Name: "b1", UpdatedAt: 8},
		},
		{
			method: "DeleteBackend", tool: "quicbaas_object_admin_delete_backend",
			req:      &osadmin.DeleteBackendRequest{Name: "b1", MigrateTo: "b2", Force: true},
			wantArgs: map[string]interface{}{"name": "b1", "migrate_to": "b2", "force": true},
			result:   map[string]interface{}{"name": "b1", "deleted": true, "migrated_objects": 3},
			want:     &osadmin.DeleteBackendResponse{Name: "b1", Deleted: true, MigratedObjects: 3},
		},
		{
			method: "SetBackendStatus", tool: "quicbaas_object_admin_set_backend_status",
			req:      &osadmin.SetBackendStatusRequest{Name: "b1", Enabled: false},
			wantArgs: map[string]interface{}{"name": "b1", "enabled": false},
			result:   map[string]interface{}{"name": "b1", "enabled": false, "updated_at": 9},
			want:     &osadmin.SetBackendStatusResponse{Name: "b1", UpdatedAt: 9},
		},
		{
			method: "GetStorageStats", tool: "quicbaas_object_admin_get_storage_stats",
			req:      &osadmin.GetStorageStatsRequest{IncludeBackends: true, IncludeTenants: true},
			wantArgs: map[string]interface{}{"include_backends": true, "include_tenants": true},
			result: map[string]interface{}{
				"total_bytes": 100, "total_objects": 4, "total_backends": 1, "healthy_backends": 1, "calculated_at": 5,
				"backend_stats": []interface{}{map[string]interface{}{"name": "b1", "type": "gcs", "enabled": true,
					"tier": "warm", "object_count": 4, "total_size": 100}},
				"tenant_stats": []interface{}{map[string]interface{}{"tenant_id": "t1", "total_bytes": 100,
					"object_count": 4, "hot_bytes": 1, "warm_bytes": 2, "cold_bytes": 3, "archive_bytes": 4}},
			},
			want: &osadmin.GetStorageStatsResponse{
				TotalBytes: 100, TotalObjects: 4, TotalBackends: 1, HealthyBackends: 1, CalculatedAt: 5,
				BackendStats: []*osadmin.BackendInfo{{Name: "b1", Type: osadmin.BackendTypeGCS, Enabled: true,
					Tier: osadmin.StorageTierWarm, ObjectCount: 4, UsedBytes: 100}},
				TenantStats: []*osadmin.TenantUsage{{TenantID: "t1", TotalBytes: 100, ObjectCount: 4,
					HotBytes: 1, WarmBytes: 2, ColdBytes: 3, ArchiveBytes: 4}},
			},
		},
		{
			method: "RunGarbageCollection", tool: "quicbaas_object_admin_run_gc",
			req:      &osadmin.RunGarbageCollectionRequest{Backends: []string{"b1"}, DryRun: true},
			wantArgs: map[string]interface{}{"backends": []interface{}{"b1"}, "dry_run": true, "aggressive": false},
			result:   map[string]interface{}{"operation_id": "gc-1", "started_at": 11},
			want:     &osadmin.RunGarbageCollectionResponse{OperationID: "gc-1", StartedAt: 11},
		},
		{
			method: "GetGarbageCollectionProgress", tool: "quicbaas_object_admin_get_gc_progress",
			req:      &osadmin.GetGarbageCollectionProgressRequest{OperationID: "gc-1"},
			wantArgs: map[string]interface{}{"operation_id": "gc-1"},
			result: map[string]interface{}{"operation_id": "gc-1", "status": "running", "objects_scanned": 10,
				"objects_deleted": 2, "bytes_reclaimed": 64, "percent_complete": 50.5},
			want: &osadmin.GetGarbageCollectionProgressResponse{OperationID: "gc-1",
				Status: osadmin.GarbageCollectionStatus("running"), ObjectsScanned: 10, ObjectsDeleted: 2,
				BytesReclaimed: 64, PercentComplete: 50.5},
		},
		{
			method: "RepairObject", tool: "quicbaas_object_admin_repair_object",
			req:      &osadmin.RepairObjectRequest{Key: "k1", SourceLocation: "b2", TimeoutMs: 500},
			wantArgs: map[string]interface{}{"key": "k1", "source_location": "b2", "timeout_ms": int64(500)},
			result:   map[string]interface{}{"key": "k1", "success": true, "repaired_locations": []string{"b1"}},
			want:     &osadmin.RepairObjectResponse{Key: "k1", Success: true, RepairedLocations: []string{"b1"}},
		},
		{
			method: "CompactBackend", tool: "quicbaas_object_admin_compact_backend",
			req:      &osadmin.CompactBackendRequest{Name: "b1", Aggressive: true},
			wantArgs: map[string]interface{}{"name": "b1", "aggressive": true},
			result:   map[string]interface{}{"name": "b1", "operation_id": "c-1", "started_at": 12},
			want:     &osadmin.CompactBackendResponse{Name: "b1", OperationID: "c-1", StartedAt: 12},
		},
	}

	tr := &Transport{}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			if got := tr.mapToToolName("quicbaas.ObjectStorageAdmin", tt.method); got != tt.tool {
				t.Fatalf("tool = %q, want %q", got, tt.tool)
			}
			args, err := objectStorageAdminArguments(tt.method, tt.req)
			if err != nil {
				t.Fatalf("arguments: %v", err)
			}
			if !reflect.DeepEqual(args, tt.wantArgs) {
				t.Fatalf("arguments = %#v, want %#v", args, tt.wantArgs)
			}
			got, err := objectStorageAdminResponse(tt.method, wireResult(t, tt.result))
			if err != nil {
				t.Fatalf("response: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("response = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestObjectStorageAdminArgumentsRejectsWrongRequest(t *testing.T) {
	if _, err := objectStorageAdminArguments("CreateBackend", &osadmin.CompactBackendRequest{}); err == nil {
		t.Fatal("CreateBackend with a CompactBackendRequest: want error")
	}
	if _, err := objectStorageAdminArguments("CreateBackend", (*osadmin.CreateBackendRequest)(nil)); err == nil {
		t.Fatal("CreateBackend with a nil request: want error")
	}
}

func TestObjectStorageAdminResponseErrors(t *testing.T) {
	if _, err := objectStorageAdminResponse("CreateBackend", "not an object"); err == nil {
		t.Fatal("non-object result: want error")
	}
	if _, err := objectStorageAdminResponse("ListBackends", map[string]interface{}{}); err == nil {
		t.Fatal("unserved verb: want error")
	}
	if _, err := objectStorageAdminArguments("ListBackends", nil); err == nil {
		t.Fatal("unserved verb arguments: want error")
	}
}
