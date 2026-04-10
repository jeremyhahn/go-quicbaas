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

package service

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
)

// =============================================================================
// Object Storage Admin Types - Backend Management
// =============================================================================

// BackendType represents the type of storage backend.
type BackendType string

const (
	BackendTypeUnspecified BackendType = ""
	BackendTypeLocal       BackendType = "local"
	BackendTypeS3          BackendType = "s3"
	BackendTypeGCS         BackendType = "gcs"
	BackendTypeAzure       BackendType = "azure"
	BackendTypeMemory      BackendType = "memory"
)

// StorageTier represents the storage tier classification.
type StorageTier string

const (
	StorageTierUnspecified StorageTier = ""
	StorageTierHot         StorageTier = "hot"
	StorageTierWarm        StorageTier = "warm"
	StorageTierCold        StorageTier = "cold"
)

// CreateBackendRequest contains parameters for registering a new storage backend.
type CreateBackendRequest struct {
	// Name is the unique backend name.
	Name string

	// Type is the backend type (local, s3, gcs, azure, memory).
	Type BackendType

	// Tier is the storage tier (hot, warm, cold).
	Tier StorageTier

	// Config contains backend-specific configuration.
	Config map[string]string

	// Enabled indicates if the backend should be enabled.
	Enabled bool
}

// Validate validates the CreateBackendRequest.
func (r *CreateBackendRequest) Validate() error {
	if r == nil {
		return api.NewNilRequestError("admin.CreateBackendRequest.Validate")
	}
	if r.Name == "" {
		return api.NewRequiredFieldError("backend name")
	}
	if r.Type == BackendTypeUnspecified {
		return api.NewRequiredFieldError("backend type")
	}
	if r.Tier == StorageTierUnspecified {
		return api.NewRequiredFieldError("storage tier")
	}
	return nil
}

// CreateBackendResponse contains the result of creating a backend.
type CreateBackendResponse struct {
	// Name is the created backend name.
	Name string

	// CreatedAt is the creation timestamp (Unix nanoseconds).
	CreatedAt int64
}

// UpdateBackendRequest contains parameters for updating backend configuration.
type UpdateBackendRequest struct {
	// Name is the backend name.
	Name string

	// Config contains updated configuration (only specified keys are updated).
	Config map[string]string

	// Tier updates the storage tier (empty means no change).
	Tier StorageTier
}

// Validate validates the UpdateBackendRequest.
func (r *UpdateBackendRequest) Validate() error {
	if r == nil {
		return api.NewNilRequestError("admin.UpdateBackendRequest.Validate")
	}
	if r.Name == "" {
		return api.NewRequiredFieldError("backend name")
	}
	return nil
}

// UpdateBackendResponse contains the result of updating a backend.
type UpdateBackendResponse struct {
	// Name is the updated backend name.
	Name string

	// UpdatedAt is the update timestamp (Unix nanoseconds).
	UpdatedAt int64
}

// DeleteBackendRequest contains parameters for removing a storage backend.
type DeleteBackendRequest struct {
	// Name is the backend name.
	Name string

	// MigrateTo specifies where to migrate objects before deletion.
	MigrateTo string

	// Force deletes even if objects exist (data loss).
	Force bool
}

// Validate validates the DeleteBackendRequest.
func (r *DeleteBackendRequest) Validate() error {
	if r == nil {
		return api.NewNilRequestError("admin.DeleteBackendRequest.Validate")
	}
	if r.Name == "" {
		return api.NewRequiredFieldError("backend name")
	}
	return nil
}

// DeleteBackendResponse contains the result of deleting a backend.
type DeleteBackendResponse struct {
	// Name is the deleted backend name.
	Name string

	// Deleted indicates if the backend was deleted.
	Deleted bool

	// MigratedObjects is the count of migrated objects.
	MigratedObjects int64
}

// SetBackendStatusRequest contains parameters for enabling/disabling a backend.
type SetBackendStatusRequest struct {
	// Name is the backend name.
	Name string

	// Enabled sets the enabled status.
	Enabled bool
}

// Validate validates the SetBackendStatusRequest.
func (r *SetBackendStatusRequest) Validate() error {
	if r == nil {
		return api.NewNilRequestError("admin.SetBackendStatusRequest.Validate")
	}
	if r.Name == "" {
		return api.NewRequiredFieldError("backend name")
	}
	return nil
}

// SetBackendStatusResponse contains the result of updating backend status.
type SetBackendStatusResponse struct {
	// Name is the backend name.
	Name string

	// Enabled is the new status.
	Enabled bool

	// UpdatedAt is the update timestamp (Unix nanoseconds).
	UpdatedAt int64
}

// =============================================================================
// Object Storage Admin Types - Statistics
// =============================================================================

// GetStorageStatsRequest contains parameters for retrieving storage statistics.
type GetStorageStatsRequest struct {
	// IncludeBackends includes per-backend breakdown.
	IncludeBackends bool

	// IncludeTenants includes per-tenant breakdown.
	IncludeTenants bool
}

// Validate validates the GetStorageStatsRequest.
func (r *GetStorageStatsRequest) Validate() error {
	if r == nil {
		return api.NewNilRequestError("admin.GetStorageStatsRequest.Validate")
	}
	return nil
}

// GetStorageStatsResponse contains cluster-wide storage statistics.
type GetStorageStatsResponse struct {
	// TotalBytes is the total storage used.
	TotalBytes int64

	// TotalObjects is the total object count.
	TotalObjects int64

	// TotalBackends is the number of backends.
	TotalBackends int32

	// HealthyBackends is the number of healthy backends.
	HealthyBackends int32

	// BackendStats contains per-backend statistics.
	BackendStats []*BackendInfo

	// TenantStats contains per-tenant statistics.
	TenantStats []*TenantStorageStats

	// CalculatedAt is when stats were calculated (Unix nanoseconds).
	CalculatedAt int64
}

// TenantStorageStats contains storage statistics for a tenant.
type TenantStorageStats struct {
	// TenantID identifies the tenant.
	TenantID string

	// TotalBytes is the total storage used.
	TotalBytes int64

	// ObjectCount is the total number of objects.
	ObjectCount int64

	// HotBytes is storage in hot tier.
	HotBytes int64

	// WarmBytes is storage in warm tier.
	WarmBytes int64

	// ColdBytes is storage in cold tier.
	ColdBytes int64

	// ArchiveBytes is storage in archive tier.
	ArchiveBytes int64
}

// =============================================================================
// Object Storage Admin Types - Garbage Collection
// =============================================================================

// RunGarbageCollectionRequest contains parameters for initiating garbage collection.
type RunGarbageCollectionRequest struct {
	// Backends specifies which backends to GC (empty means all backends).
	Backends []string

	// DryRun reports what would be deleted without deleting.
	DryRun bool

	// Aggressive uses more aggressive GC settings.
	Aggressive bool
}

// Validate validates the RunGarbageCollectionRequest.
func (r *RunGarbageCollectionRequest) Validate() error {
	if r == nil {
		return api.NewNilRequestError("admin.RunGarbageCollectionRequest.Validate")
	}
	return nil
}

// RunGarbageCollectionResponse contains the result of initiating GC.
type RunGarbageCollectionResponse struct {
	// OperationID is the async operation ID for tracking.
	OperationID string

	// StartedAt is when GC started (Unix nanoseconds).
	StartedAt int64
}

// GetGarbageCollectionProgressRequest contains parameters for retrieving GC progress.
type GetGarbageCollectionProgressRequest struct {
	// OperationID identifies the GC operation.
	OperationID string
}

// Validate validates the GetGarbageCollectionProgressRequest.
func (r *GetGarbageCollectionProgressRequest) Validate() error {
	if r == nil {
		return api.NewNilRequestError("admin.GetGarbageCollectionProgressRequest.Validate")
	}
	if r.OperationID == "" {
		return api.NewRequiredFieldError("operation ID")
	}
	return nil
}

// GCStatus represents garbage collection operation status.
type GCStatus string

const (
	GCStatusRunning   GCStatus = "running"
	GCStatusCompleted GCStatus = "completed"
	GCStatusFailed    GCStatus = "failed"
)

// GetGarbageCollectionProgressResponse contains GC progress information.
type GetGarbageCollectionProgressResponse struct {
	// OperationID is the GC operation ID.
	OperationID string

	// Status is the GC status (running, completed, failed).
	Status GCStatus

	// ObjectsScanned is the count of scanned objects.
	ObjectsScanned int64

	// ObjectsDeleted is the count of deleted objects.
	ObjectsDeleted int64

	// BytesReclaimed is the storage reclaimed.
	BytesReclaimed int64

	// PercentComplete is the completion percentage.
	PercentComplete float64

	// Error contains error message if failed.
	Error string

	// CompletedAt is when GC completed (Unix nanoseconds, 0 if not finished).
	CompletedAt int64
}

// =============================================================================
// Object Storage Admin Types - Repair
// =============================================================================

// RepairObjectRequest contains parameters for repairing an inconsistent object.
type RepairObjectRequest struct {
	// Key is the object key to repair.
	Key string

	// SourceLocation specifies which location has the correct data.
	// Auto-selects based on checksums if not specified.
	SourceLocation string

	// TimeoutMs specifies the maximum time to wait (milliseconds).
	TimeoutMs int64
}

// Validate validates the RepairObjectRequest.
func (r *RepairObjectRequest) Validate() error {
	if r == nil {
		return api.NewNilRequestError("admin.RepairObjectRequest.Validate")
	}
	if r.Key == "" {
		return api.NewRequiredFieldError("key")
	}
	return nil
}

// RepairObjectResponse contains the result of repairing an object.
type RepairObjectResponse struct {
	// Key is the repaired object key.
	Key string

	// Success indicates if repair succeeded.
	Success bool

	// RepairedLocations lists locations that were repaired.
	RepairedLocations []string

	// Error contains error message if failed.
	Error string
}

// =============================================================================
// Object Storage Admin Types - Compaction
// =============================================================================

// CompactBackendRequest contains parameters for triggering compaction.
type CompactBackendRequest struct {
	// Name is the backend name.
	Name string

	// Aggressive uses more aggressive compaction settings.
	Aggressive bool
}

// Validate validates the CompactBackendRequest.
func (r *CompactBackendRequest) Validate() error {
	if r == nil {
		return api.NewNilRequestError("admin.CompactBackendRequest.Validate")
	}
	if r.Name == "" {
		return api.NewRequiredFieldError("backend name")
	}
	return nil
}

// CompactBackendResponse contains the result of initiating compaction.
type CompactBackendResponse struct {
	// Name is the backend name.
	Name string

	// OperationID is the async operation ID.
	OperationID string

	// StartedAt is when compaction started (Unix nanoseconds).
	StartedAt int64
}

// =============================================================================
// Internal Types for Admin Operations
// =============================================================================

// GCOperation represents an ongoing garbage collection operation.
type GCOperation struct {
	// ID is the operation ID.
	ID string

	// Backends is the list of backends being processed.
	Backends []string

	// DryRun indicates if this is a dry run.
	DryRun bool

	// Aggressive indicates if aggressive mode is enabled.
	Aggressive bool

	// Status is the current status.
	Status GCStatus

	// ObjectsScanned is the count of scanned objects.
	ObjectsScanned int64

	// ObjectsDeleted is the count of deleted objects.
	ObjectsDeleted int64

	// BytesReclaimed is the storage reclaimed.
	BytesReclaimed int64

	// StartedAt is when the operation started (Unix nanoseconds).
	StartedAt int64

	// CompletedAt is when the operation completed (Unix nanoseconds).
	CompletedAt int64

	// Error contains error message if failed.
	Error string
}

// CompactOperation represents an ongoing compaction operation.
type CompactOperation struct {
	// ID is the operation ID.
	ID string

	// Backend is the backend being compacted.
	Backend string

	// Aggressive indicates if aggressive mode is enabled.
	Aggressive bool

	// Status is the current status.
	Status string

	// StartedAt is when the operation started (Unix nanoseconds).
	StartedAt int64

	// CompletedAt is when the operation completed (Unix nanoseconds).
	CompletedAt int64

	// Error contains error message if failed.
	Error string
}

// convertStorageTier converts service StorageTier to catalog StorageTier.
func convertStorageTier(tier StorageTier) catalog.StorageTier {
	switch tier {
	case StorageTierHot:
		return catalog.TierHot
	case StorageTierWarm:
		return catalog.TierWarm
	case StorageTierCold:
		return catalog.TierCold
	default:
		return ""
	}
}
