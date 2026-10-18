// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

// Package osadminconv converts between the ObjectStorageAdmin SDK client
// types and the go-quicbaas service types the server speaks. The embedded
// and REST transports share it so each verb has one conversion.
package osadminconv

import (
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	osadmin "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorageadmin"
)

// BackendTypeName returns the wire name the service uses for t ("local",
// "s3", ...), or "" when t is unspecified.
func BackendTypeName(t osadmin.BackendType) string {
	if t == osadmin.BackendTypeUnspecified {
		return ""
	}
	return strings.ToLower(t.String())
}

// StorageTierName returns the wire name the service uses for t ("hot",
// "warm", ...), or "" when t is unspecified.
func StorageTierName(t osadmin.StorageTier) string {
	if t == osadmin.StorageTierUnspecified {
		return ""
	}
	return strings.ToLower(t.String())
}

var backendTypesByName = map[string]osadmin.BackendType{
	"local":  osadmin.BackendTypeLocal,
	"s3":     osadmin.BackendTypeS3,
	"gcs":    osadmin.BackendTypeGCS,
	"azure":  osadmin.BackendTypeAzure,
	"minio":  osadmin.BackendTypeMinio,
	"pebble": osadmin.BackendTypePebble,
	"raft":   osadmin.BackendTypeRaft,
}

var storageTiersByName = map[string]osadmin.StorageTier{
	"hot":     osadmin.StorageTierHot,
	"warm":    osadmin.StorageTierWarm,
	"cold":    osadmin.StorageTierCold,
	"archive": osadmin.StorageTierArchive,
}

// ParseBackendType maps a service wire name to the SDK enum; an unknown
// name maps to BackendTypeUnspecified.
func ParseBackendType(name string) osadmin.BackendType {
	return backendTypesByName[strings.ToLower(name)]
}

// ParseStorageTier maps a service wire name to the SDK enum; an unknown
// name maps to StorageTierUnspecified.
func ParseStorageTier(name string) osadmin.StorageTier {
	return storageTiersByName[strings.ToLower(name)]
}

// CreateBackendRequest converts an SDK request to the service request.
func CreateBackendRequest(r *osadmin.CreateBackendRequest) *service.CreateBackendRequest {
	return &service.CreateBackendRequest{
		Name:    r.Name,
		Type:    service.BackendType(BackendTypeName(r.Type)),
		Tier:    service.StorageTier(StorageTierName(r.Tier)),
		Config:  r.Config,
		Enabled: r.Enabled,
	}
}

// UpdateBackendRequest converts an SDK request to the service request.
func UpdateBackendRequest(r *osadmin.UpdateBackendRequest) *service.UpdateBackendRequest {
	return &service.UpdateBackendRequest{
		Name:   r.Name,
		Config: r.Config,
		Tier:   service.StorageTier(StorageTierName(r.Tier)),
	}
}

// DeleteBackendRequest converts an SDK request to the service request.
func DeleteBackendRequest(r *osadmin.DeleteBackendRequest) *service.DeleteBackendRequest {
	return &service.DeleteBackendRequest{Name: r.Name, MigrateTo: r.MigrateTo, Force: r.Force}
}

// SetBackendStatusRequest converts an SDK request to the service request.
func SetBackendStatusRequest(r *osadmin.SetBackendStatusRequest) *service.SetBackendStatusRequest {
	return &service.SetBackendStatusRequest{Name: r.Name, Enabled: r.Enabled}
}

// GetStorageStatsRequest converts an SDK request to the service request.
func GetStorageStatsRequest(r *osadmin.GetStorageStatsRequest) *service.GetStorageStatsRequest {
	return &service.GetStorageStatsRequest{IncludeBackends: r.IncludeBackends, IncludeTenants: r.IncludeTenants}
}

// RunGarbageCollectionRequest converts an SDK request to the service request.
func RunGarbageCollectionRequest(r *osadmin.RunGarbageCollectionRequest) *service.RunGarbageCollectionRequest {
	return &service.RunGarbageCollectionRequest{Backends: r.Backends, DryRun: r.DryRun, Aggressive: r.Aggressive}
}

// GetGarbageCollectionProgressRequest converts an SDK request to the service request.
func GetGarbageCollectionProgressRequest(r *osadmin.GetGarbageCollectionProgressRequest) *service.GetGarbageCollectionProgressRequest {
	return &service.GetGarbageCollectionProgressRequest{OperationID: r.OperationID}
}

// RepairObjectRequest converts an SDK request to the service request.
func RepairObjectRequest(r *osadmin.RepairObjectRequest) *service.RepairObjectRequest {
	return &service.RepairObjectRequest{Key: r.Key, SourceLocation: r.SourceLocation, TimeoutMs: r.TimeoutMs}
}

// CompactBackendRequest converts an SDK request to the service request.
func CompactBackendRequest(r *osadmin.CompactBackendRequest) *service.CompactBackendRequest {
	return &service.CompactBackendRequest{Name: r.Name, Aggressive: r.Aggressive}
}

// CreateBackendResponse converts a service response to the SDK response.
func CreateBackendResponse(r *service.CreateBackendResponse) *osadmin.CreateBackendResponse {
	return &osadmin.CreateBackendResponse{Name: r.Name, CreatedAt: r.CreatedAt}
}

// UpdateBackendResponse converts a service response to the SDK response.
func UpdateBackendResponse(r *service.UpdateBackendResponse) *osadmin.UpdateBackendResponse {
	return &osadmin.UpdateBackendResponse{Name: r.Name, UpdatedAt: r.UpdatedAt}
}

// DeleteBackendResponse converts a service response to the SDK response.
func DeleteBackendResponse(r *service.DeleteBackendResponse) *osadmin.DeleteBackendResponse {
	return &osadmin.DeleteBackendResponse{Name: r.Name, Deleted: r.Deleted, MigratedObjects: r.MigratedObjects}
}

// SetBackendStatusResponse converts a service response to the SDK response.
func SetBackendStatusResponse(r *service.SetBackendStatusResponse) *osadmin.SetBackendStatusResponse {
	return &osadmin.SetBackendStatusResponse{Name: r.Name, Enabled: r.Enabled, UpdatedAt: r.UpdatedAt}
}

// GetStorageStatsResponse converts a service response to the SDK response.
func GetStorageStatsResponse(r *service.GetStorageStatsResponse) *osadmin.GetStorageStatsResponse {
	out := &osadmin.GetStorageStatsResponse{
		TotalBytes:      r.TotalBytes,
		TotalObjects:    r.TotalObjects,
		TotalBackends:   r.TotalBackends,
		HealthyBackends: r.HealthyBackends,
		CalculatedAt:    r.CalculatedAt,
	}
	for _, b := range r.BackendStats {
		if b == nil {
			continue
		}
		info := &osadmin.BackendInfo{
			Name:        b.Name,
			Type:        ParseBackendType(b.Type),
			Enabled:     b.Enabled,
			Tier:        ParseStorageTier(b.Tier),
			Region:      b.Region,
			UsedBytes:   b.TotalSize,
			ObjectCount: b.ObjectCount,
		}
		if b.Health != nil {
			info.Healthy = b.Health.Healthy
		}
		out.BackendStats = append(out.BackendStats, info)
	}
	for _, t := range r.TenantStats {
		if t == nil {
			continue
		}
		out.TenantStats = append(out.TenantStats, &osadmin.TenantUsage{
			TenantID:     t.TenantID,
			TotalBytes:   t.TotalBytes,
			ObjectCount:  t.ObjectCount,
			HotBytes:     t.HotBytes,
			WarmBytes:    t.WarmBytes,
			ColdBytes:    t.ColdBytes,
			ArchiveBytes: t.ArchiveBytes,
		})
	}
	return out
}

// RunGarbageCollectionResponse converts a service response to the SDK response.
func RunGarbageCollectionResponse(r *service.RunGarbageCollectionResponse) *osadmin.RunGarbageCollectionResponse {
	return &osadmin.RunGarbageCollectionResponse{OperationID: r.OperationID, StartedAt: r.StartedAt}
}

// GetGarbageCollectionProgressResponse converts a service response to the SDK response.
func GetGarbageCollectionProgressResponse(r *service.GetGarbageCollectionProgressResponse) *osadmin.GetGarbageCollectionProgressResponse {
	return &osadmin.GetGarbageCollectionProgressResponse{
		OperationID:     r.OperationID,
		Status:          osadmin.GarbageCollectionStatus(r.Status),
		ObjectsScanned:  r.ObjectsScanned,
		ObjectsDeleted:  r.ObjectsDeleted,
		BytesReclaimed:  r.BytesReclaimed,
		PercentComplete: r.PercentComplete,
		Error:           r.Error,
		CompletedAt:     r.CompletedAt,
	}
}

// RepairObjectResponse converts a service response to the SDK response.
func RepairObjectResponse(r *service.RepairObjectResponse) *osadmin.RepairObjectResponse {
	return &osadmin.RepairObjectResponse{
		Key:               r.Key,
		Success:           r.Success,
		RepairedLocations: r.RepairedLocations,
		Error:             r.Error,
	}
}

// CompactBackendResponse converts a service response to the SDK response.
func CompactBackendResponse(r *service.CompactBackendResponse) *osadmin.CompactBackendResponse {
	return &osadmin.CompactBackendResponse{Name: r.Name, OperationID: r.OperationID, StartedAt: r.StartedAt}
}
