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
	"context"
)

// =============================================================================
// Object Storage Admin Service - Administrative Operations
// =============================================================================

// ObjectStorageAdminService provides administrative operations for storage management
// including backend management, garbage collection, and repair operations.
type ObjectStorageAdminService interface {
	// Backend Management Operations

	// CreateBackend registers a new storage backend.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: CreateBackendRequest containing backend configuration
	// Returns:
	//   - CreateBackendResponse: Contains creation confirmation
	//   - error: ErrInvalidInput, ErrAlreadyExists, ErrTimeout
	CreateBackend(ctx context.Context, req *CreateBackendRequest) (*CreateBackendResponse, error)

	// UpdateBackend updates backend configuration.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: UpdateBackendRequest containing updated configuration
	// Returns:
	//   - UpdateBackendResponse: Contains update confirmation
	//   - error: ErrNotFound, ErrInvalidInput, ErrTimeout
	UpdateBackend(ctx context.Context, req *UpdateBackendRequest) (*UpdateBackendResponse, error)

	// DeleteBackend removes a storage backend (requires migration of objects).
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: DeleteBackendRequest containing backend name and migration target
	// Returns:
	//   - DeleteBackendResponse: Contains deletion status and migration count
	//   - error: ErrNotFound, ErrInvalidInput, ErrTimeout
	DeleteBackend(ctx context.Context, req *DeleteBackendRequest) (*DeleteBackendResponse, error)

	// SetBackendStatus enables or disables a backend.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: SetBackendStatusRequest containing backend name and status
	// Returns:
	//   - SetBackendStatusResponse: Contains status update confirmation
	//   - error: ErrNotFound, ErrTimeout
	SetBackendStatus(ctx context.Context, req *SetBackendStatusRequest) (*SetBackendStatusResponse, error)

	// Statistics Operations

	// GetStorageStats returns cluster-wide storage statistics.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: GetStorageStatsRequest with optional breakdown flags
	// Returns:
	//   - GetStorageStatsResponse: Contains storage statistics
	//   - error: ErrTimeout
	GetStorageStats(ctx context.Context, req *GetStorageStatsRequest) (*GetStorageStatsResponse, error)

	// Garbage Collection Operations

	// RunGarbageCollection initiates garbage collection on specified backends.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: RunGarbageCollectionRequest with backend list and options
	// Returns:
	//   - RunGarbageCollectionResponse: Contains operation ID for tracking
	//   - error: ErrInvalidInput, ErrTimeout
	RunGarbageCollection(ctx context.Context, req *RunGarbageCollectionRequest) (*RunGarbageCollectionResponse, error)

	// GetGarbageCollectionProgress returns GC progress.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: GetGarbageCollectionProgressRequest with operation ID
	// Returns:
	//   - GetGarbageCollectionProgressResponse: Contains GC progress
	//   - error: ErrNotFound, ErrTimeout
	GetGarbageCollectionProgress(ctx context.Context, req *GetGarbageCollectionProgressRequest) (*GetGarbageCollectionProgressResponse, error)

	// Repair Operations

	// RepairObject attempts to repair an object with inconsistent replicas.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: RepairObjectRequest with object key and source location
	// Returns:
	//   - RepairObjectResponse: Contains repair results
	//   - error: ErrNotFound, ErrTimeout, ErrStorage
	RepairObject(ctx context.Context, req *RepairObjectRequest) (*RepairObjectResponse, error)

	// Compaction Operations

	// CompactBackend triggers compaction on a backend.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: CompactBackendRequest with backend name and options
	// Returns:
	//   - CompactBackendResponse: Contains operation ID for tracking
	//   - error: ErrNotFound, ErrTimeout
	CompactBackend(ctx context.Context, req *CompactBackendRequest) (*CompactBackendResponse, error)
}
