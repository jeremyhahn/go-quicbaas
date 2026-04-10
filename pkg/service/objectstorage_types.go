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
	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Object Storage Types - Object CRUD
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
	ReplicationPolicy *catalog.ReplicationPolicy

	// ArchivePolicy overrides the default archive policy.
	ArchivePolicy *catalog.ArchivePolicy

	// RetentionPolicy overrides the default retention policy.
	RetentionPolicy *catalog.RetentionPolicy

	// TargetBackend explicitly specifies the storage backend.
	TargetBackend string

	// Checksum is an optional client-provided checksum for verification.
	Checksum []byte
}

// Validate validates the PutObjectRequest.
func (r *PutObjectRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.PutObjectRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if r.Key == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.PutObjectRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "key"},
		}
	}
	if len(r.Value) == 0 {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.PutObjectRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "value"},
		}
	}
	return nil
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

// Validate validates the GetObjectRequest.
func (r *GetObjectRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.GetObjectRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if r.Key == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.GetObjectRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "key"},
		}
	}
	return nil
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

// Validate validates the DeleteObjectRequest.
func (r *DeleteObjectRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.DeleteObjectRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if r.Key == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.DeleteObjectRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "key"},
		}
	}
	return nil
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

// Validate validates the HeadObjectRequest.
func (r *HeadObjectRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.HeadObjectRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if r.Key == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.HeadObjectRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "key"},
		}
	}
	return nil
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
// Object Storage Types - Listing
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

// Validate validates the ListObjectsRequest.
func (r *ListObjectsRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.ListObjectsRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	return nil
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

// Validate validates the ListByTagRequest.
func (r *ListByTagRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.ListByTagRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if r.TagName == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.ListByTagRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "tag name"},
		}
	}
	return nil
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

// Validate validates the ListByTenantRequest.
func (r *ListByTenantRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.ListByTenantRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if r.TenantID == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.ListByTenantRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "tenant ID"},
		}
	}
	return nil
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
// Object Storage Types - Copy
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

// Validate validates the CopyObjectRequest.
func (r *CopyObjectRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.CopyObjectRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if r.SourceKey == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.CopyObjectRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "source key"},
		}
	}
	if r.DestinationKey == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.CopyObjectRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "destination key"},
		}
	}
	return nil
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
// Object Storage Types - Replication & Migration
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

// Validate validates the ReplicateObjectRequest.
func (r *ReplicateObjectRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.ReplicateObjectRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if r.Key == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.ReplicateObjectRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "key"},
		}
	}
	if len(r.Targets) == 0 {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.ReplicateObjectRequest.Validate",
			Err:  &api.EmptyCollectionError{Field: "targets"},
		}
	}
	return nil
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

// Validate validates the MigrateObjectRequest.
func (r *MigrateObjectRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.MigrateObjectRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if r.Key == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.MigrateObjectRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "key"},
		}
	}
	if r.TargetBackend == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.MigrateObjectRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "target backend"},
		}
	}
	return nil
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
// Object Storage Types - Archive & Restore
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

// Validate validates the ArchiveObjectRequest.
func (r *ArchiveObjectRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.ArchiveObjectRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if r.Key == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.ArchiveObjectRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "key"},
		}
	}
	return nil
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

// Validate validates the RestoreObjectRequest.
func (r *RestoreObjectRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.RestoreObjectRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if r.Key == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.RestoreObjectRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "key"},
		}
	}
	return nil
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
// Object Storage Types - Verification
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

// Validate validates the VerifyObjectRequest.
func (r *VerifyObjectRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.VerifyObjectRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if r.Key == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.VerifyObjectRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "key"},
		}
	}
	return nil
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
// Object Storage Types - Policy Management
// =============================================================================

// SetObjectPolicyRequest contains parameters for setting object policies.
type SetObjectPolicyRequest struct {
	// Key is the object key.
	Key string

	// TenantID for access control.
	TenantID string

	// ReplicationPolicy is the replication policy to set.
	ReplicationPolicy *catalog.ReplicationPolicy

	// ArchivePolicy is the archive policy to set.
	ArchivePolicy *catalog.ArchivePolicy

	// RetentionPolicy is the retention policy to set.
	RetentionPolicy *catalog.RetentionPolicy
}

// Validate validates the SetObjectPolicyRequest.
func (r *SetObjectPolicyRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.SetObjectPolicyRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if r.Key == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.SetObjectPolicyRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "key"},
		}
	}
	return nil
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

// Validate validates the GetObjectPolicyRequest.
func (r *GetObjectPolicyRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.GetObjectPolicyRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if r.Key == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.GetObjectPolicyRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "key"},
		}
	}
	return nil
}

// GetObjectPolicyResponse contains the object policies.
type GetObjectPolicyResponse struct {
	// Key is the object key.
	Key string

	// ReplicationPolicy is the current replication policy.
	ReplicationPolicy *catalog.ReplicationPolicy

	// ArchivePolicy is the current archive policy.
	ArchivePolicy *catalog.ArchivePolicy

	// RetentionPolicy is the current retention policy.
	RetentionPolicy *catalog.RetentionPolicy

	// Timestamp is the query timestamp (Unix nanoseconds).
	Timestamp int64
}

// =============================================================================
// Object Storage Types - Backend Management
// =============================================================================

// ListBackendsRequest contains parameters for listing backends.
type ListBackendsRequest struct {
	// IncludeHealth indicates whether to include health status.
	IncludeHealth bool

	// FilterByType filters backends by type.
	FilterByType string
}

// Validate validates the ListBackendsRequest.
func (r *ListBackendsRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.ListBackendsRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	return nil
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

// Validate validates the GetBackendHealthRequest.
func (r *GetBackendHealthRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.GetBackendHealthRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if r.Name == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.GetBackendHealthRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "backend name"},
		}
	}
	return nil
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
// Object Storage Types - Tenant Management
// =============================================================================

// GetTenantUsageRequest contains parameters for getting tenant usage.
type GetTenantUsageRequest struct {
	// TenantID is the tenant to get usage for.
	TenantID string
}

// Validate validates the GetTenantUsageRequest.
func (r *GetTenantUsageRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.GetTenantUsageRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if r.TenantID == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.GetTenantUsageRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "tenant ID"},
		}
	}
	return nil
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

// Validate validates the GetTenantQuotasRequest.
func (r *GetTenantQuotasRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.GetTenantQuotasRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if r.TenantID == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.GetTenantQuotasRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "tenant ID"},
		}
	}
	return nil
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

// Validate validates the SetTenantQuotasRequest.
func (r *SetTenantQuotasRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.SetTenantQuotasRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if r.TenantID == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.SetTenantQuotasRequest.Validate",
			Err:  &api.RequiredFieldError{Field: "tenant ID"},
		}
	}
	// Negative values are invalid
	if r.MaxStorageBytes < 0 || r.MaxObjectSize < 0 || r.MaxObjectCount < 0 {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.SetTenantQuotasRequest.Validate",
			Err:  &api.InvalidInputError{Message: "quota values cannot be negative"},
		}
	}
	return nil
}

// SetTenantQuotasResponse contains the result of setting tenant quotas.
type SetTenantQuotasResponse struct {
	// TenantID is the tenant.
	TenantID string

	// UpdatedAt is the update timestamp (Unix nanoseconds).
	UpdatedAt int64
}

// =============================================================================
// Object Storage Types - Batch Operations
// =============================================================================

// BatchPutObjectsRequest contains parameters for batch put operations.
type BatchPutObjectsRequest struct {
	// Objects contains the list of objects to put.
	Objects []*PutObjectRequest

	// TenantID is the tenant for all objects (optional, overrides per-object).
	TenantID string

	// ContinueOnError indicates whether to continue on individual failures.
	ContinueOnError bool
}

// Validate validates the BatchPutObjectsRequest.
func (r *BatchPutObjectsRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.BatchPutObjectsRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if len(r.Objects) == 0 {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.BatchPutObjectsRequest.Validate",
			Err:  &api.EmptyCollectionError{Field: "objects"},
		}
	}
	return nil
}

// BatchPutObjectsResponse contains the results of batch put operations.
type BatchPutObjectsResponse struct {
	// Results contains per-object results.
	Results []*BatchPutResult

	// SuccessCount is the number of successful puts.
	SuccessCount int

	// FailureCount is the number of failed puts.
	FailureCount int

	// Timestamp is the operation timestamp (Unix nanoseconds).
	Timestamp int64
}

// BatchPutResult contains the result of a single object put in a batch.
type BatchPutResult struct {
	// Key is the object key.
	Key string

	// Success indicates if the put succeeded.
	Success bool

	// ObjectID is the internal object identifier (on success).
	ObjectID uint64

	// Checksum is the SHA-256 checksum (on success).
	Checksum []byte

	// Error contains the error message (on failure).
	Error string
}

// BatchGetObjectsRequest contains parameters for batch get operations.
type BatchGetObjectsRequest struct {
	// Keys contains the list of object keys to retrieve.
	Keys []string

	// TenantID filters by tenant (optional for access control).
	TenantID string

	// IncludeMetadata indicates whether to include metadata.
	IncludeMetadata bool
}

// Validate validates the BatchGetObjectsRequest.
func (r *BatchGetObjectsRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.BatchGetObjectsRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if len(r.Keys) == 0 {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.BatchGetObjectsRequest.Validate",
			Err:  &api.EmptyCollectionError{Field: "keys"},
		}
	}
	return nil
}

// BatchGetObjectsResponse contains the results of batch get operations.
type BatchGetObjectsResponse struct {
	// Results contains per-object results.
	Results []*BatchGetResult

	// FoundCount is the number of objects found.
	FoundCount int

	// NotFoundCount is the number of objects not found.
	NotFoundCount int

	// Timestamp is the query timestamp (Unix nanoseconds).
	Timestamp int64
}

// BatchGetResult contains the result of a single object get in a batch.
type BatchGetResult struct {
	// Key is the object key.
	Key string

	// Found indicates if the object was found.
	Found bool

	// Value is the object data (if found).
	Value []byte

	// ContentType is the MIME type (if found).
	ContentType string

	// Size is the object size in bytes (if found).
	Size int64

	// Checksum is the SHA-256 checksum (if found).
	Checksum []byte

	// Version is the object version (if found).
	Version uint64

	// Tags are the user-defined tags (if found and requested).
	Tags map[string]string

	// Error contains the error message (on failure).
	Error string
}

// BatchDeleteObjectsRequest contains parameters for batch delete operations.
type BatchDeleteObjectsRequest struct {
	// Keys contains the list of object keys to delete.
	Keys []string

	// TenantID filters by tenant (optional for access control).
	TenantID string

	// ContinueOnError indicates whether to continue on individual failures.
	ContinueOnError bool
}

// Validate validates the BatchDeleteObjectsRequest.
func (r *BatchDeleteObjectsRequest) Validate() error {
	if r == nil {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.BatchDeleteObjectsRequest.Validate",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if len(r.Keys) == 0 {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "objectstorage.BatchDeleteObjectsRequest.Validate",
			Err:  &api.EmptyCollectionError{Field: "keys"},
		}
	}
	return nil
}

// BatchDeleteObjectsResponse contains the results of batch delete operations.
type BatchDeleteObjectsResponse struct {
	// Results contains per-object results.
	Results []*BatchDeleteResult

	// DeletedCount is the number of successful deletes.
	DeletedCount int

	// FailureCount is the number of failed deletes.
	FailureCount int

	// Timestamp is the operation timestamp (Unix nanoseconds).
	Timestamp int64
}

// BatchDeleteResult contains the result of a single object delete in a batch.
type BatchDeleteResult struct {
	// Key is the object key.
	Key string

	// Success indicates if the delete succeeded.
	Success bool

	// Size is the size of the deleted object (if success).
	Size int64

	// Error contains the error message (on failure).
	Error string
}
