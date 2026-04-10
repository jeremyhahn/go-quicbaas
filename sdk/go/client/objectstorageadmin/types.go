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

// Package objectstorageadmin provides the SDK client for ObjectStorage admin operations.
package objectstorageadmin

// =============================================================================
// Enums
// =============================================================================

// BackendType defines the type of storage backend.
type BackendType int32

const (
	// BackendTypeUnspecified is an unspecified backend type.
	BackendTypeUnspecified BackendType = 0

	// BackendTypeLocal is a local filesystem backend.
	BackendTypeLocal BackendType = 1

	// BackendTypeS3 is an Amazon S3 or compatible backend.
	BackendTypeS3 BackendType = 2

	// BackendTypeGCS is a Google Cloud Storage backend.
	BackendTypeGCS BackendType = 3

	// BackendTypeAzure is an Azure Blob Storage backend.
	BackendTypeAzure BackendType = 4

	// BackendTypeMinio is a MinIO object storage backend.
	BackendTypeMinio BackendType = 5

	// BackendTypePebble is an embedded PebbleDB backend.
	BackendTypePebble BackendType = 6

	// BackendTypeRaft is a Raft-replicated storage backend.
	BackendTypeRaft BackendType = 7
)

// String returns the string representation of the backend type.
func (t BackendType) String() string {
	switch t {
	case BackendTypeLocal:
		return "LOCAL"
	case BackendTypeS3:
		return "S3"
	case BackendTypeGCS:
		return "GCS"
	case BackendTypeAzure:
		return "AZURE"
	case BackendTypeMinio:
		return "MINIO"
	case BackendTypePebble:
		return "PEBBLE"
	case BackendTypeRaft:
		return "RAFT"
	default:
		return "UNSPECIFIED"
	}
}

// StorageTier defines the storage tier for cost/performance optimization.
type StorageTier int32

const (
	// StorageTierUnspecified is an unspecified storage tier.
	StorageTierUnspecified StorageTier = 0

	// StorageTierHot is frequently accessed, highest performance.
	StorageTierHot StorageTier = 1

	// StorageTierWarm is infrequently accessed, balanced.
	StorageTierWarm StorageTier = 2

	// StorageTierCold is rarely accessed, low cost.
	StorageTierCold StorageTier = 3

	// StorageTierArchive is long-term retention, lowest cost.
	StorageTierArchive StorageTier = 4
)

// String returns the string representation of the storage tier.
func (t StorageTier) String() string {
	switch t {
	case StorageTierHot:
		return "HOT"
	case StorageTierWarm:
		return "WARM"
	case StorageTierCold:
		return "COLD"
	case StorageTierArchive:
		return "ARCHIVE"
	default:
		return "UNSPECIFIED"
	}
}

// GarbageCollectionStatus represents the status of a garbage collection operation.
type GarbageCollectionStatus string

const (
	// GCStatusRunning indicates the GC operation is running.
	GCStatusRunning GarbageCollectionStatus = "running"

	// GCStatusCompleted indicates the GC operation completed successfully.
	GCStatusCompleted GarbageCollectionStatus = "completed"

	// GCStatusFailed indicates the GC operation failed.
	GCStatusFailed GarbageCollectionStatus = "failed"

	// GCStatusCanceled indicates the GC operation was canceled.
	GCStatusCanceled GarbageCollectionStatus = "canceled"
)

// =============================================================================
// Backend Management Types
// =============================================================================

// CreateBackendRequest contains parameters for creating a new storage backend.
type CreateBackendRequest struct {
	// Name is the unique backend name.
	Name string

	// Type is the backend type.
	Type BackendType

	// Tier is the storage tier.
	Tier StorageTier

	// Config contains backend-specific configuration.
	Config map[string]string

	// Enabled indicates if the backend should be enabled.
	Enabled bool
}

// CreateBackendResponse contains the result of creating a backend.
type CreateBackendResponse struct {
	// Name is the created backend name.
	Name string

	// CreatedAt is when the backend was created (Unix nanoseconds).
	CreatedAt int64
}

// UpdateBackendRequest contains parameters for updating backend configuration.
type UpdateBackendRequest struct {
	// Name is the backend name.
	Name string

	// Config contains updated configuration (only specified keys are updated).
	Config map[string]string

	// Tier updates the storage tier (zero value means no change).
	Tier StorageTier
}

// UpdateBackendResponse contains the result of updating a backend.
type UpdateBackendResponse struct {
	// Name is the updated backend name.
	Name string

	// UpdatedAt is when the backend was updated (Unix nanoseconds).
	UpdatedAt int64
}

// DeleteBackendRequest contains parameters for deleting a storage backend.
type DeleteBackendRequest struct {
	// Name is the backend name.
	Name string

	// MigrateTo specifies where to migrate objects before deletion.
	MigrateTo string

	// Force deletes even if objects exist (data loss).
	Force bool
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

// SetBackendStatusRequest contains parameters for enabling or disabling a backend.
type SetBackendStatusRequest struct {
	// Name is the backend name.
	Name string

	// Enabled sets the enabled status.
	Enabled bool
}

// SetBackendStatusResponse contains the result of updating backend status.
type SetBackendStatusResponse struct {
	// Name is the backend name.
	Name string

	// Enabled is the new status.
	Enabled bool

	// UpdatedAt is when the status was updated (Unix nanoseconds).
	UpdatedAt int64
}

// =============================================================================
// Storage Stats Types
// =============================================================================

// GetStorageStatsRequest contains parameters for retrieving storage statistics.
type GetStorageStatsRequest struct {
	// IncludeBackends includes per-backend breakdown.
	IncludeBackends bool

	// IncludeTenants includes per-tenant breakdown.
	IncludeTenants bool
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

	// BackendStats contains per-backend statistics (if requested).
	BackendStats []*BackendInfo

	// TenantStats contains per-tenant statistics (if requested).
	TenantStats []*TenantUsage

	// CalculatedAt is when stats were calculated (Unix nanoseconds).
	CalculatedAt int64
}

// BackendInfo contains information about a storage backend.
type BackendInfo struct {
	// Name is the unique backend name.
	Name string

	// Type is the backend type.
	Type BackendType

	// Enabled indicates if the backend is enabled.
	Enabled bool

	// Healthy indicates if the backend is healthy.
	Healthy bool

	// Tier is the storage tier.
	Tier StorageTier

	// Region is the geographic region.
	Region string

	// Endpoint is the backend endpoint URL.
	Endpoint string

	// Bucket is the bucket/container name (for cloud storage).
	Bucket string

	// CapacityBytes is the total capacity (if known).
	CapacityBytes int64

	// UsedBytes is the current usage.
	UsedBytes int64

	// ObjectCount is the number of objects stored.
	ObjectCount int64

	// LastHealthCheck is when health was last checked (Unix nanoseconds).
	LastHealthCheck int64

	// Metadata contains additional backend-specific metadata.
	Metadata map[string]string
}

// TenantUsage contains storage usage statistics for a tenant.
type TenantUsage struct {
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

	// CalculatedAt is when usage was calculated (Unix nanoseconds).
	CalculatedAt int64
}

// =============================================================================
// Garbage Collection Types
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

// RunGarbageCollectionResponse contains the result of initiating garbage collection.
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

// GetGarbageCollectionProgressResponse contains garbage collection progress.
type GetGarbageCollectionProgressResponse struct {
	// OperationID is the GC operation ID.
	OperationID string

	// Status is the GC status (running, completed, failed).
	Status GarbageCollectionStatus

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

	// CompletedAt is when GC completed (Unix nanoseconds, if finished).
	CompletedAt int64
}

// =============================================================================
// Repair Types
// =============================================================================

// RepairObjectRequest contains parameters for repairing an inconsistent object.
type RepairObjectRequest struct {
	// Key is the object key to repair.
	Key string

	// SourceLocation specifies which location has the correct data.
	SourceLocation string

	// TimeoutMs specifies the maximum time to wait (milliseconds).
	TimeoutMs int64
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
// Compaction Types
// =============================================================================

// CompactBackendRequest contains parameters for triggering compaction on a backend.
type CompactBackendRequest struct {
	// Name is the backend name.
	Name string

	// Aggressive uses more aggressive compaction settings.
	Aggressive bool
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
