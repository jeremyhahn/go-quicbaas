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

// Package service provides the unified service facade for QuicBaaS.
package service

import (
	"context"
)

// =============================================================================
// Object Storage Service - Object Storage Operations
// =============================================================================

// ObjectStorageService provides object storage operations with support for
// multi-tenancy, replication, archival, and policy-based lifecycle management.
type ObjectStorageService interface {
	// Object CRUD Operations

	// PutObject stores an object with specified metadata and policies.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: PutObjectRequest containing key, value, and options
	// Returns:
	//   - PutObjectResponse: Contains object metadata and storage info
	//   - error: ErrInvalidInput, ErrQuotaExceeded, ErrTimeout, ErrStorage
	PutObject(ctx context.Context, req *PutObjectRequest) (*PutObjectResponse, error)

	// GetObject retrieves an object by key.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: GetObjectRequest containing key and options
	// Returns:
	//   - GetObjectResponse: Contains object data and metadata
	//   - error: ErrNotFound, ErrTimeout, ErrStorage
	GetObject(ctx context.Context, req *GetObjectRequest) (*GetObjectResponse, error)

	// DeleteObject removes an object from storage.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: DeleteObjectRequest containing key
	// Returns:
	//   - DeleteObjectResponse: Contains deletion status
	//   - error: ErrNotFound, ErrTimeout, ErrStorage
	DeleteObject(ctx context.Context, req *DeleteObjectRequest) (*DeleteObjectResponse, error)

	// HeadObject retrieves object metadata without the data.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: HeadObjectRequest containing key
	// Returns:
	//   - HeadObjectResponse: Contains object metadata
	//   - error: ErrNotFound, ErrTimeout, ErrStorage
	HeadObject(ctx context.Context, req *HeadObjectRequest) (*HeadObjectResponse, error)

	// Listing Operations

	// ListObjects lists objects matching the specified criteria.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: ListObjectsRequest with prefix and pagination
	// Returns:
	//   - ListObjectsResponse: Contains matching objects
	//   - error: ErrTimeout, ErrStorage
	ListObjects(ctx context.Context, req *ListObjectsRequest) (*ListObjectsResponse, error)

	// ListByTag lists objects with a specific tag.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: ListByTagRequest with tag name and value
	// Returns:
	//   - ListByTagResponse: Contains matching objects
	//   - error: ErrTimeout, ErrStorage
	ListByTag(ctx context.Context, req *ListByTagRequest) (*ListByTagResponse, error)

	// ListByTenant lists objects owned by a specific tenant.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: ListByTenantRequest with tenant ID
	// Returns:
	//   - ListByTenantResponse: Contains matching objects
	//   - error: ErrForbidden, ErrTimeout, ErrStorage
	ListByTenant(ctx context.Context, req *ListByTenantRequest) (*ListByTenantResponse, error)

	// Copy Operations

	// CopyObject copies an object to a new location.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: CopyObjectRequest with source and destination keys
	// Returns:
	//   - CopyObjectResponse: Contains new object metadata
	//   - error: ErrNotFound, ErrAlreadyExists, ErrTimeout, ErrStorage
	CopyObject(ctx context.Context, req *CopyObjectRequest) (*CopyObjectResponse, error)

	// Replication & Migration Operations

	// ReplicateObject replicates an object to target backends.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: ReplicateObjectRequest with key and targets
	// Returns:
	//   - ReplicateObjectResponse: Contains replication status
	//   - error: ErrNotFound, ErrTimeout, ErrStorage
	ReplicateObject(ctx context.Context, req *ReplicateObjectRequest) (*ReplicateObjectResponse, error)

	// MigrateObject migrates an object to a new backend.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: MigrateObjectRequest with key and target backend
	// Returns:
	//   - MigrateObjectResponse: Contains migration status
	//   - error: ErrNotFound, ErrTimeout, ErrStorage
	MigrateObject(ctx context.Context, req *MigrateObjectRequest) (*MigrateObjectResponse, error)

	// Archive & Restore Operations

	// ArchiveObject moves an object to archive storage.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: ArchiveObjectRequest with key and target store
	// Returns:
	//   - ArchiveObjectResponse: Contains archive status
	//   - error: ErrNotFound, ErrAlreadyArchived, ErrTimeout, ErrStorage
	ArchiveObject(ctx context.Context, req *ArchiveObjectRequest) (*ArchiveObjectResponse, error)

	// RestoreObject restores an archived object to active storage.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: RestoreObjectRequest with key
	// Returns:
	//   - RestoreObjectResponse: Contains restore status
	//   - error: ErrNotFound, ErrNotArchived, ErrTimeout, ErrStorage
	RestoreObject(ctx context.Context, req *RestoreObjectRequest) (*RestoreObjectResponse, error)

	// Verification Operations

	// VerifyObject verifies object integrity across storage backends.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: VerifyObjectRequest with key and stores to verify
	// Returns:
	//   - VerifyObjectResponse: Contains verification results
	//   - error: ErrNotFound, ErrTimeout, ErrStorage
	VerifyObject(ctx context.Context, req *VerifyObjectRequest) (*VerifyObjectResponse, error)

	// Policy Management Operations

	// SetObjectPolicy sets policies for an object.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: SetObjectPolicyRequest with key and policies
	// Returns:
	//   - SetObjectPolicyResponse: Contains updated policy info
	//   - error: ErrNotFound, ErrInvalidInput, ErrTimeout, ErrStorage
	SetObjectPolicy(ctx context.Context, req *SetObjectPolicyRequest) (*SetObjectPolicyResponse, error)

	// GetObjectPolicy retrieves policies for an object.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: GetObjectPolicyRequest with key
	// Returns:
	//   - GetObjectPolicyResponse: Contains policy info
	//   - error: ErrNotFound, ErrTimeout, ErrStorage
	GetObjectPolicy(ctx context.Context, req *GetObjectPolicyRequest) (*GetObjectPolicyResponse, error)

	// Backend Management Operations

	// ListBackends returns available storage backends.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: ListBackendsRequest with optional filters
	// Returns:
	//   - ListBackendsResponse: Contains backend list
	//   - error: ErrTimeout
	ListBackends(ctx context.Context, req *ListBackendsRequest) (*ListBackendsResponse, error)

	// GetBackendHealth returns health status for a backend.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: GetBackendHealthRequest with backend name
	// Returns:
	//   - GetBackendHealthResponse: Contains health status
	//   - error: ErrNotFound, ErrTimeout
	GetBackendHealth(ctx context.Context, req *GetBackendHealthRequest) (*GetBackendHealthResponse, error)

	// Tenant Management Operations

	// GetTenantUsage returns storage usage for a tenant.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: GetTenantUsageRequest with tenant ID
	// Returns:
	//   - GetTenantUsageResponse: Contains usage statistics
	//   - error: ErrForbidden, ErrTimeout, ErrStorage
	GetTenantUsage(ctx context.Context, req *GetTenantUsageRequest) (*GetTenantUsageResponse, error)

	// GetTenantQuotas returns quota settings for a tenant.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: GetTenantQuotasRequest with tenant ID
	// Returns:
	//   - GetTenantQuotasResponse: Contains quota settings
	//   - error: ErrForbidden, ErrTimeout
	GetTenantQuotas(ctx context.Context, req *GetTenantQuotasRequest) (*GetTenantQuotasResponse, error)

	// SetTenantQuotas sets or updates quotas for a tenant.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: SetTenantQuotasRequest with tenant ID and quota settings
	// Returns:
	//   - SetTenantQuotasResponse: Contains update confirmation
	//   - error: ErrForbidden, ErrInvalidInput, ErrTimeout
	SetTenantQuotas(ctx context.Context, req *SetTenantQuotasRequest) (*SetTenantQuotasResponse, error)

	// Batch Operations

	// BatchPutObjects stores multiple objects in a single operation.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: BatchPutObjectsRequest containing objects to store
	// Returns:
	//   - BatchPutObjectsResponse: Contains per-object results
	//   - error: ErrInvalidInput, ErrQuotaExceeded, ErrTimeout
	BatchPutObjects(ctx context.Context, req *BatchPutObjectsRequest) (*BatchPutObjectsResponse, error)

	// BatchGetObjects retrieves multiple objects in a single operation.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: BatchGetObjectsRequest containing keys to retrieve
	// Returns:
	//   - BatchGetObjectsResponse: Contains per-object results
	//   - error: ErrTimeout
	BatchGetObjects(ctx context.Context, req *BatchGetObjectsRequest) (*BatchGetObjectsResponse, error)

	// BatchDeleteObjects deletes multiple objects in a single operation.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: BatchDeleteObjectsRequest containing keys to delete
	// Returns:
	//   - BatchDeleteObjectsResponse: Contains per-object results
	//   - error: ErrTimeout
	BatchDeleteObjects(ctx context.Context, req *BatchDeleteObjectsRequest) (*BatchDeleteObjectsResponse, error)
}
