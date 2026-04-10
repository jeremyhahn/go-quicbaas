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

package objectstorage

import "io"

// =============================================================================
// Object CRUD Types
// =============================================================================

// PutObjectRequest contains parameters for storing an object.
type PutObjectRequest struct {
	// Key is the object key in "namespace:path" format.
	Key string

	// Value is the object data.
	Value []byte

	// ContentType is the MIME type of the object.
	ContentType string

	// TenantID is the tenant that owns the object (optional).
	TenantID string

	// ShardID is the shard for the object (optional, auto-selected if 0).
	ShardID uint64

	// Tags are user-defined key-value pairs.
	Tags map[string]string

	// ReplicationPolicy overrides the default replication policy.
	ReplicationPolicy *ReplicationPolicy

	// ArchivePolicy overrides the default archive policy.
	ArchivePolicy *ArchivePolicy

	// RetentionPolicy overrides the default retention policy.
	RetentionPolicy *RetentionPolicy

	// TargetBackend explicitly specifies the storage backend.
	TargetBackend string

	// Checksum is an optional client-provided checksum for verification.
	Checksum []byte
}

// PutObjectResponse contains the result of storing an object.
type PutObjectResponse struct {
	// Key is the stored object key.
	Key string

	// ObjectID is the internal object identifier.
	ObjectID uint64

	// Checksum is the SHA-256 checksum of the stored data.
	Checksum []byte

	// Size is the size of the stored object in bytes.
	Size int64

	// Backend is the storage backend used.
	Backend string

	// Version is the object version.
	Version uint64

	// CreatedAt is the creation timestamp (Unix nanoseconds).
	CreatedAt int64
}

// GetObjectRequest contains parameters for retrieving an object.
type GetObjectRequest struct {
	// Key is the object key to retrieve.
	Key string

	// TenantID filters by tenant (optional for access control).
	TenantID string

	// IncludeMetadata indicates whether to include metadata in the response.
	IncludeMetadata bool
}

// GetObjectResponse contains the result of retrieving an object.
type GetObjectResponse struct {
	// Key is the object key.
	Key string

	// Value is the object data.
	Value []byte

	// ContentType is the MIME type of the object.
	ContentType string

	// Size is the object size in bytes.
	Size int64

	// Checksum is the SHA-256 checksum.
	Checksum []byte

	// Version is the object version.
	Version uint64

	// Tags are the user-defined tags.
	Tags map[string]string

	// TenantID is the owning tenant.
	TenantID string

	// CreatedAt is the creation timestamp (Unix nanoseconds).
	CreatedAt int64

	// ModifiedAt is the last modification timestamp (Unix nanoseconds).
	ModifiedAt int64

	// Backend is the storage backend serving the object.
	Backend string
}

// DeleteObjectRequest contains parameters for deleting an object.
type DeleteObjectRequest struct {
	// Key is the object key to delete.
	Key string

	// TenantID filters by tenant (optional for access control).
	TenantID string
}

// DeleteObjectResponse contains the result of deleting an object.
type DeleteObjectResponse struct {
	// Key is the deleted object key.
	Key string

	// Success indicates if deletion succeeded.
	Success bool

	// Size is the size of the deleted object.
	Size int64

	// Timestamp is the deletion timestamp (Unix nanoseconds).
	Timestamp int64
}

// HeadObjectRequest contains parameters for retrieving object metadata.
type HeadObjectRequest struct {
	// Key is the object key.
	Key string

	// TenantID filters by tenant (optional for access control).
	TenantID string
}

// HeadObjectResponse contains object metadata.
type HeadObjectResponse struct {
	// Key is the object key.
	Key string

	// Exists indicates if the object exists.
	Exists bool

	// ContentType is the MIME type.
	ContentType string

	// Size is the object size in bytes.
	Size int64

	// Checksum is the SHA-256 checksum.
	Checksum []byte

	// Version is the object version.
	Version uint64

	// Tags are the user-defined tags.
	Tags map[string]string

	// TenantID is the owning tenant.
	TenantID string

	// Status is the object status (active, archived, etc.).
	Status string

	// CreatedAt is the creation timestamp (Unix nanoseconds).
	CreatedAt int64

	// ModifiedAt is the last modification timestamp (Unix nanoseconds).
	ModifiedAt int64

	// LastAccessAt is the last access timestamp (Unix nanoseconds).
	LastAccessAt int64

	// Backend is the primary storage backend.
	Backend string

	// ReplicaCount is the number of replicas.
	ReplicaCount int
}

// =============================================================================
// Listing Types
// =============================================================================

// ListObjectsRequest contains parameters for listing objects.
type ListObjectsRequest struct {
	// Prefix filters objects by key prefix.
	Prefix string

	// TenantID filters by tenant (optional for access control).
	TenantID string

	// Limit is the maximum number of results.
	Limit int

	// Cursor is the pagination cursor.
	Cursor string

	// IncludeTags indicates whether to include tags in results.
	IncludeTags bool
}

// ListObjectsResponse contains the list of objects.
type ListObjectsResponse struct {
	// Objects contains the matching objects.
	Objects []*ObjectSummary

	// NextCursor is the cursor for the next page.
	NextCursor string

	// HasMore indicates if more results are available.
	HasMore bool

	// TotalCount is the approximate total count.
	TotalCount int64

	// Timestamp is the query timestamp (Unix nanoseconds).
	Timestamp int64
}

// ObjectSummary contains summary information for an object.
type ObjectSummary struct {
	// Key is the object key.
	Key string

	// Size is the object size in bytes.
	Size int64

	// ContentType is the MIME type.
	ContentType string

	// Version is the object version.
	Version uint64

	// Status is the object status.
	Status string

	// Tags are the user-defined tags (if requested).
	Tags map[string]string

	// CreatedAt is the creation timestamp (Unix nanoseconds).
	CreatedAt int64

	// ModifiedAt is the last modification timestamp (Unix nanoseconds).
	ModifiedAt int64
}

// ListByTagRequest contains parameters for listing objects by tag.
type ListByTagRequest struct {
	// TagName is the tag name to filter by.
	TagName string

	// TagValue is the tag value to match.
	TagValue string

	// TenantID filters by tenant (optional for access control).
	TenantID string

	// Limit is the maximum number of results.
	Limit int

	// Cursor is the pagination cursor.
	Cursor string
}

// ListByTagResponse contains the list of objects matching the tag.
type ListByTagResponse struct {
	// Objects contains the matching objects.
	Objects []*ObjectSummary

	// NextCursor is the cursor for the next page.
	NextCursor string

	// HasMore indicates if more results are available.
	HasMore bool

	// TotalCount is the approximate total count.
	TotalCount int64

	// Timestamp is the query timestamp (Unix nanoseconds).
	Timestamp int64
}

// ListByTenantRequest contains parameters for listing objects by tenant.
type ListByTenantRequest struct {
	// TenantID is the tenant to list objects for.
	TenantID string

	// Limit is the maximum number of results.
	Limit int

	// Cursor is the pagination cursor.
	Cursor string

	// IncludeTags indicates whether to include tags in results.
	IncludeTags bool
}

// ListByTenantResponse contains the list of objects for a tenant.
type ListByTenantResponse struct {
	// Objects contains the matching objects.
	Objects []*ObjectSummary

	// NextCursor is the cursor for the next page.
	NextCursor string

	// HasMore indicates if more results are available.
	HasMore bool

	// TotalCount is the approximate total count.
	TotalCount int64

	// Timestamp is the query timestamp (Unix nanoseconds).
	Timestamp int64
}

// =============================================================================
// Copy Types
// =============================================================================

// CopyObjectRequest contains parameters for copying an object.
type CopyObjectRequest struct {
	// SourceKey is the source object key.
	SourceKey string

	// DestinationKey is the destination object key.
	DestinationKey string

	// TenantID for the destination (defaults to source tenant).
	TenantID string

	// OverwriteExisting allows overwriting an existing object.
	OverwriteExisting bool

	// TargetBackend specifies the backend for the copy.
	TargetBackend string
}

// CopyObjectResponse contains the result of copying an object.
type CopyObjectResponse struct {
	// Key is the destination object key.
	Key string

	// ObjectID is the new object identifier.
	ObjectID uint64

	// Size is the copied object size.
	Size int64

	// Checksum is the checksum of the copied object.
	Checksum []byte

	// Version is the new object version.
	Version uint64

	// Backend is the storage backend used.
	Backend string

	// CreatedAt is the creation timestamp (Unix nanoseconds).
	CreatedAt int64
}

// =============================================================================
// Replication & Migration Types
// =============================================================================

// ReplicateObjectRequest contains parameters for replicating an object.
type ReplicateObjectRequest struct {
	// Key is the object key to replicate.
	Key string

	// Targets are the target backend names.
	Targets []string

	// TenantID for access control.
	TenantID string

	// Priority is the replication priority (1-10).
	Priority int

	// Synchronous indicates if replication should be synchronous.
	Synchronous bool
}

// ReplicateObjectResponse contains the result of replicating an object.
type ReplicateObjectResponse struct {
	// Key is the replicated object key.
	Key string

	// Status is the overall replication status.
	Status string

	// Results contains per-target replication results.
	Results map[string]*ReplicationResult

	// Timestamp is the operation timestamp (Unix nanoseconds).
	Timestamp int64
}

// ReplicationResult contains the result of replicating to a single target.
type ReplicationResult struct {
	// Target is the backend name.
	Target string

	// Success indicates if replication succeeded.
	Success bool

	// Error contains the error message if failed.
	Error string

	// Duration is the replication duration in milliseconds.
	Duration int64
}

// MigrateObjectRequest contains parameters for migrating an object.
type MigrateObjectRequest struct {
	// Key is the object key to migrate.
	Key string

	// TargetBackend is the destination backend.
	TargetBackend string

	// TenantID for access control.
	TenantID string

	// DeleteFromSource indicates if the object should be deleted from source.
	DeleteFromSource bool
}

// MigrateObjectResponse contains the result of migrating an object.
type MigrateObjectResponse struct {
	// Key is the migrated object key.
	Key string

	// SourceBackend is the original backend.
	SourceBackend string

	// TargetBackend is the new backend.
	TargetBackend string

	// Success indicates if migration succeeded.
	Success bool

	// DeletedFromSource indicates if object was deleted from source.
	DeletedFromSource bool

	// Timestamp is the operation timestamp (Unix nanoseconds).
	Timestamp int64
}

// =============================================================================
// Archive & Restore Types
// =============================================================================

// ArchiveObjectRequest contains parameters for archiving an object.
type ArchiveObjectRequest struct {
	// Key is the object key to archive.
	Key string

	// TargetStore is the archive backend name.
	TargetStore string

	// TenantID for access control.
	TenantID string

	// DeleteFromPrimary indicates if object should be deleted from primary.
	DeleteFromPrimary bool
}

// ArchiveObjectResponse contains the result of archiving an object.
type ArchiveObjectResponse struct {
	// Key is the archived object key.
	Key string

	// ArchiveStore is the archive backend name.
	ArchiveStore string

	// Success indicates if archival succeeded.
	Success bool

	// DeletedFromPrimary indicates if object was deleted from primary.
	DeletedFromPrimary bool

	// ArchivedAt is the archival timestamp (Unix nanoseconds).
	ArchivedAt int64
}

// RestoreObjectRequest contains parameters for restoring an archived object.
type RestoreObjectRequest struct {
	// Key is the object key to restore.
	Key string

	// TargetBackend is the destination backend for restore.
	TargetBackend string

	// TenantID for access control.
	TenantID string
}

// RestoreObjectResponse contains the result of restoring an object.
type RestoreObjectResponse struct {
	// Key is the restored object key.
	Key string

	// Backend is the backend where object was restored.
	Backend string

	// Success indicates if restore succeeded.
	Success bool

	// RestoredAt is the restore timestamp (Unix nanoseconds).
	RestoredAt int64
}

// =============================================================================
// Verification Types
// =============================================================================

// VerifyObjectRequest contains parameters for verifying an object.
type VerifyObjectRequest struct {
	// Key is the object key to verify.
	Key string

	// Stores are the backends to verify (empty = all locations).
	Stores []string

	// TenantID for access control.
	TenantID string

	// RepairMismatches indicates if mismatches should be repaired.
	RepairMismatches bool
}

// VerifyObjectResponse contains the result of verifying an object.
type VerifyObjectResponse struct {
	// Key is the verified object key.
	Key string

	// Valid indicates if the object is valid across all locations.
	Valid bool

	// Results contains per-store verification results.
	Results map[string]*VerificationResult

	// Repaired indicates if any mismatches were repaired.
	Repaired bool

	// Timestamp is the verification timestamp (Unix nanoseconds).
	Timestamp int64
}

// VerificationResult contains the result of verifying a single location.
type VerificationResult struct {
	// Store is the backend name.
	Store string

	// Valid indicates if the copy is valid.
	Valid bool

	// ChecksumMatch indicates if checksums match.
	ChecksumMatch bool

	// SizeMatch indicates if sizes match.
	SizeMatch bool

	// Error contains the error message if verification failed.
	Error string
}

// =============================================================================
// Policy Types
// =============================================================================

// ReplicationMode defines how replication is performed.
type ReplicationMode string

const (
	// ReplicationModeSync waits for all replicas before returning success.
	ReplicationModeSync ReplicationMode = "sync"

	// ReplicationModeAsync returns immediately, replicates in background.
	ReplicationModeAsync ReplicationMode = "async"

	// ReplicationModeQuorum waits for majority of replicas.
	ReplicationModeQuorum ReplicationMode = "quorum"
)

// ReplicationPolicy defines how objects are replicated across backends.
type ReplicationPolicy struct {
	// Enabled indicates whether replication is active.
	Enabled bool

	// MinReplicas is the minimum number of replicas required (including primary).
	MinReplicas int

	// TargetStores contains the backend names to replicate to.
	TargetStores []string

	// Mode specifies the replication mode (sync, async, quorum).
	Mode ReplicationMode

	// Priority determines replication order (1-10, higher = sooner).
	Priority int
}

// ArchivePolicy defines when and how objects are archived to cold storage.
type ArchivePolicy struct {
	// Enabled indicates whether archival is active.
	Enabled bool

	// ArchiveAfterDays specifies days since last access before archiving.
	ArchiveAfterDays int

	// TargetStore is the archive backend name.
	TargetStore string

	// DeleteFromPrimary removes the object from primary after archiving.
	DeleteFromPrimary bool

	// CompressionLevel specifies compression (0=none, 1-9=level).
	CompressionLevel int
}

// RetentionPolicy defines how long objects are kept.
type RetentionPolicy struct {
	// Enabled indicates whether retention enforcement is active.
	Enabled bool

	// RetentionDays specifies auto-delete after N days (0 = never).
	RetentionDays int

	// MaxVersions keeps last N versions (0 = unlimited).
	MaxVersions int

	// DeleteAfter is a Unix timestamp for deletion (0 = use RetentionDays).
	DeleteAfter int64
}

// SetObjectPolicyRequest contains parameters for setting object policies.
type SetObjectPolicyRequest struct {
	// Key is the object key.
	Key string

	// TenantID for access control.
	TenantID string

	// ReplicationPolicy is the replication policy to set.
	ReplicationPolicy *ReplicationPolicy

	// ArchivePolicy is the archive policy to set.
	ArchivePolicy *ArchivePolicy

	// RetentionPolicy is the retention policy to set.
	RetentionPolicy *RetentionPolicy
}

// SetObjectPolicyResponse contains the result of setting object policies.
type SetObjectPolicyResponse struct {
	// Key is the object key.
	Key string

	// Success indicates if policies were set.
	Success bool

	// Version is the new object version.
	Version uint64

	// Timestamp is the operation timestamp (Unix nanoseconds).
	Timestamp int64
}

// GetObjectPolicyRequest contains parameters for getting object policies.
type GetObjectPolicyRequest struct {
	// Key is the object key.
	Key string

	// TenantID for access control.
	TenantID string
}

// GetObjectPolicyResponse contains the object policies.
type GetObjectPolicyResponse struct {
	// Key is the object key.
	Key string

	// ReplicationPolicy is the current replication policy.
	ReplicationPolicy *ReplicationPolicy

	// ArchivePolicy is the current archive policy.
	ArchivePolicy *ArchivePolicy

	// RetentionPolicy is the current retention policy.
	RetentionPolicy *RetentionPolicy

	// Timestamp is the query timestamp (Unix nanoseconds).
	Timestamp int64
}

// =============================================================================
// Backend Management Types
// =============================================================================

// ListBackendsRequest contains parameters for listing backends.
type ListBackendsRequest struct {
	// IncludeHealth indicates whether to include health status.
	IncludeHealth bool

	// FilterByType filters backends by type.
	FilterByType string
}

// ListBackendsResponse contains the list of backends.
type ListBackendsResponse struct {
	// Backends contains the backend information.
	Backends []*BackendInfo

	// Timestamp is the query timestamp (Unix nanoseconds).
	Timestamp int64
}

// BackendInfo contains information about a storage backend.
type BackendInfo struct {
	// Name is the backend name.
	Name string

	// Type is the backend type (s3, gcs, local, etc.).
	Type string

	// Enabled indicates if the backend is enabled.
	Enabled bool

	// Default indicates if this is the default backend.
	Default bool

	// Tier is the storage tier (hot, warm, cold).
	Tier string

	// Region is the geographic region.
	Region string

	// ObjectCount is the number of objects in this backend.
	ObjectCount int64

	// TotalSize is the total size in bytes.
	TotalSize int64

	// Health contains health status (if requested).
	Health *BackendHealthInfo
}

// BackendHealthInfo contains health information for a backend.
type BackendHealthInfo struct {
	// Healthy indicates if the backend is healthy.
	Healthy bool

	// Latency is the last health check latency in milliseconds.
	Latency int64

	// LastCheck is the last health check timestamp (Unix nanoseconds).
	LastCheck int64

	// Error contains the error message if unhealthy.
	Error string
}

// GetBackendHealthRequest contains parameters for getting backend health.
type GetBackendHealthRequest struct {
	// Name is the backend name.
	Name string
}

// GetBackendHealthResponse contains backend health status.
type GetBackendHealthResponse struct {
	// Name is the backend name.
	Name string

	// Healthy indicates if the backend is healthy.
	Healthy bool

	// Latency is the last health check latency in milliseconds.
	Latency int64

	// LastCheck is the last health check timestamp (Unix nanoseconds).
	LastCheck int64

	// Status provides additional status information.
	Status string

	// Error contains the error message if unhealthy.
	Error string
}

// =============================================================================
// Tenant Management Types
// =============================================================================

// GetTenantUsageRequest contains parameters for getting tenant usage.
type GetTenantUsageRequest struct {
	// TenantID is the tenant to get usage for.
	TenantID string
}

// GetTenantUsageResponse contains tenant usage statistics.
type GetTenantUsageResponse struct {
	// TenantID is the tenant.
	TenantID string

	// ObjectCount is the total number of objects.
	ObjectCount int64

	// TotalSize is the total size in bytes.
	TotalSize int64

	// ActiveCount is the number of active objects.
	ActiveCount int64

	// ArchivedCount is the number of archived objects.
	ArchivedCount int64

	// Timestamp is the query timestamp (Unix nanoseconds).
	Timestamp int64
}

// GetTenantQuotasRequest contains parameters for getting tenant quotas.
type GetTenantQuotasRequest struct {
	// TenantID is the tenant to get quotas for.
	TenantID string
}

// GetTenantQuotasResponse contains tenant quota settings.
type GetTenantQuotasResponse struct {
	// TenantID is the tenant.
	TenantID string

	// MaxStorageBytes is the maximum storage quota (0 = unlimited).
	MaxStorageBytes int64

	// MaxObjectSize is the maximum object size (0 = unlimited).
	MaxObjectSize int64

	// MaxObjectCount is the maximum object count (0 = unlimited).
	MaxObjectCount int64

	// UsedStorageBytes is the current storage usage.
	UsedStorageBytes int64

	// UsedObjectCount is the current object count.
	UsedObjectCount int64

	// Timestamp is the query timestamp (Unix nanoseconds).
	Timestamp int64
}

// SetTenantQuotasRequest contains parameters for setting tenant quotas.
type SetTenantQuotasRequest struct {
	// TenantID is the tenant to set quotas for.
	TenantID string

	// MaxStorageBytes is the maximum storage quota (0 = unlimited).
	MaxStorageBytes int64

	// MaxObjectSize is the maximum object size in bytes (0 = unlimited).
	MaxObjectSize int64

	// MaxObjectCount is the maximum number of objects (0 = unlimited).
	MaxObjectCount int64
}

// SetTenantQuotasResponse contains the result of setting tenant quotas.
type SetTenantQuotasResponse struct {
	// TenantID is the tenant.
	TenantID string

	// UpdatedAt is the update timestamp (Unix nanoseconds).
	UpdatedAt int64
}

// =============================================================================
// Batch Operation Types
// =============================================================================

// BatchPutObjectsRequest contains parameters for storing multiple objects.
type BatchPutObjectsRequest struct {
	// Objects is the list of objects to store.
	// Maximum 100 objects per batch.
	Objects []*PutObjectRequest

	// Atomic requires all objects to succeed or all fail.
	// Defaults to true.
	Atomic bool

	// TimeoutMs specifies the maximum time to wait in milliseconds.
	// Defaults to 60000ms.
	TimeoutMs int64
}

// BatchPutObjectsResponse contains the result of storing multiple objects.
type BatchPutObjectsResponse struct {
	// Results contains results for each object.
	Results []*PutObjectResponse

	// Succeeded is the count of successful puts.
	Succeeded int32

	// Failed is the count of failed puts.
	Failed int32

	// Errors contains error messages for failed objects, keyed by object key.
	Errors map[string]string
}

// BatchGetObjectsRequest contains parameters for retrieving multiple objects.
type BatchGetObjectsRequest struct {
	// Keys is the list of object keys to retrieve.
	// Maximum 100 keys per batch.
	Keys []string

	// TenantID identifies the tenant.
	TenantID string

	// IncludeMetadata requests full metadata.
	// Defaults to false.
	IncludeMetadata bool

	// TimeoutMs specifies the maximum time to wait in milliseconds.
	// Defaults to 30000ms.
	TimeoutMs int64
}

// BatchGetObjectsResponse contains the result of retrieving multiple objects.
type BatchGetObjectsResponse struct {
	// Objects contains the retrieved objects.
	Objects []*GetObjectResponse

	// NotFound contains keys that were not found.
	NotFound []string
}

// BatchDeleteObjectsRequest contains parameters for deleting multiple objects.
type BatchDeleteObjectsRequest struct {
	// Keys is the list of object keys to delete.
	// Maximum 1000 keys per batch.
	Keys []string

	// TenantID identifies the tenant.
	TenantID string

	// Permanent bypasses soft delete.
	// Defaults to false.
	Permanent bool

	// Atomic requires all deletes to succeed or all fail.
	// Defaults to false.
	Atomic bool

	// TimeoutMs specifies the maximum time to wait in milliseconds.
	// Defaults to 60000ms.
	TimeoutMs int64
}

// BatchDeleteObjectsResponse contains the result of deleting multiple objects.
type BatchDeleteObjectsResponse struct {
	// DeletedKeys contains keys that were deleted.
	DeletedKeys []string

	// FailedKeys contains keys that failed to delete.
	FailedKeys []string

	// Errors contains error messages for failed keys, keyed by object key.
	Errors map[string]string
}

// =============================================================================
// Streaming Operation Types
// =============================================================================

// StreamPutOptions contains options for streaming object uploads.
type StreamPutOptions struct {
	// Key is the object key in "namespace:path" format.
	Key string

	// ContentType is the MIME type of the object.
	ContentType string

	// TenantID is the tenant that owns the object (optional).
	TenantID string

	// Tags are user-defined key-value pairs.
	Tags map[string]string

	// ReplicationPolicy overrides the default replication policy.
	ReplicationPolicy *ReplicationPolicy

	// ArchivePolicy overrides the default archive policy.
	ArchivePolicy *ArchivePolicy

	// RetentionPolicy overrides the default retention policy.
	RetentionPolicy *RetentionPolicy

	// TargetBackend explicitly specifies the storage backend.
	TargetBackend string

	// ChunkSize is the size of each chunk in bytes.
	// Defaults to 64KB if not specified.
	ChunkSize int
}

// StreamPutWriter provides a streaming interface for uploading large objects.
type StreamPutWriter interface {
	// Write writes data to the stream. Implements io.Writer.
	Write(p []byte) (n int, err error)

	// Close completes the upload and returns the response.
	// Must be called to finalize the upload.
	Close() (*PutObjectResponse, error)

	// Abort cancels the upload without committing.
	Abort() error

	// BytesWritten returns the total bytes written so far.
	BytesWritten() int64
}

// StreamGetReader provides a streaming interface for downloading large objects.
type StreamGetReader interface {
	// Read reads data from the stream. Implements io.Reader.
	Read(p []byte) (n int, err error)

	// Close closes the stream.
	Close() error

	// Metadata returns the object metadata.
	// Available after the first Read call or after calling ReadMetadata.
	Metadata() *ObjectMetadata

	// ReadMetadata reads and returns metadata without consuming data.
	// Useful for checking object properties before reading.
	ReadMetadata() (*ObjectMetadata, error)

	// TotalSize returns the total object size in bytes.
	// Returns -1 if size is unknown.
	TotalSize() int64

	// BytesRead returns the total bytes read so far.
	BytesRead() int64
}

// ObjectMetadata contains metadata for a streamed object.
type ObjectMetadata struct {
	// Key is the object key.
	Key string

	// ContentType is the MIME type.
	ContentType string

	// Size is the total object size in bytes.
	Size int64

	// Checksum is the SHA-256 checksum.
	Checksum []byte

	// Version is the object version.
	Version uint64

	// Tags are the user-defined tags.
	Tags map[string]string

	// TenantID is the owning tenant.
	TenantID string

	// CreatedAt is the creation timestamp (Unix nanoseconds).
	CreatedAt int64

	// ModifiedAt is the last modification timestamp (Unix nanoseconds).
	ModifiedAt int64

	// Backend is the storage backend serving the object.
	Backend string
}

// ObjectChunk represents a chunk of object data for streaming.
type ObjectChunk struct {
	// Data is the chunk data.
	Data []byte

	// Offset is the byte offset within the object.
	Offset int64

	// IsLast indicates this is the final chunk.
	IsLast bool

	// Checksum is the chunk checksum for verification.
	Checksum []byte

	// TotalSize is the total object size (set in first or last chunk).
	TotalSize int64
}

// Ensure StreamPutWriter implements io.Writer.
var _ io.Writer = (StreamPutWriter)(nil)

// Ensure StreamGetReader implements io.Reader.
var _ io.Reader = (StreamGetReader)(nil)
