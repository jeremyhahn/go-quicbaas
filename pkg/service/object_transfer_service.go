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
// Object Transfer Service - Peer-to-Peer Object Transfer Operations
// =============================================================================

// ObjectTransferService provides peer-to-peer object data transfer operations.
// This service is used for lazy snapshot recovery where recovering nodes fetch
// objects marked as pending after metadata-only snapshot restoration.
type ObjectTransferService interface {
	// FetchObject retrieves object data from a peer's local object store.
	// This is used when a node needs to recover object data after snapshot restore.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: FetchObjectRequest containing shard ID and object reference
	// Returns:
	//   - FetchObjectResponse: Contains object data, checksum, and metadata
	//   - error: ErrNotFound, ErrTimeout, ErrStorage, ErrShardNotFound
	FetchObject(ctx context.Context, req *FetchObjectRequest) (*FetchObjectResponse, error)

	// StreamFetchObjects retrieves multiple objects in a single operation.
	// More efficient for bulk recovery after snapshot restoration.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: StreamFetchObjectsRequest containing shard ID and object items
	// Returns:
	//   - StreamFetchObjectsResponse: Contains array of fetched objects
	//   - error: ErrShardNotFound, ErrTimeout
	StreamFetchObjects(ctx context.Context, req *StreamFetchObjectsRequest) (*StreamFetchObjectsResponse, error)
}

// =============================================================================
// Object Transfer Request/Response Types
// =============================================================================

// FetchObjectRequest requests object data from a peer.
type FetchObjectRequest struct {
	// ShardID identifies the Raft shard owning the object.
	ShardID uint64

	// ObjectRef is the storage path/key in the peer's object store.
	ObjectRef string

	// Checksum is the expected SHA-256 for verification (optional).
	// If provided, the peer will verify before sending.
	Checksum []byte

	// RequestID for tracing and deduplication (optional).
	RequestID string
}

// FetchObjectResponse returns object data from a peer.
type FetchObjectResponse struct {
	// ObjectRef echoes back the requested reference.
	ObjectRef string

	// Data contains the object bytes.
	Data []byte

	// Checksum is the computed SHA-256 of data.
	Checksum []byte

	// Size is the uncompressed size in bytes.
	Size int64

	// Compressed indicates if data is compressed.
	Compressed bool

	// CompressionAlgo if compressed (e.g., "lz4", "zstd").
	CompressionAlgo string

	// Found indicates if the object exists.
	Found bool

	// ErrorMessage if not found or other error.
	ErrorMessage string
}

// StreamFetchObjectsRequest requests multiple objects in one operation.
type StreamFetchObjectsRequest struct {
	// ShardID identifies the Raft shard.
	ShardID uint64

	// Items lists the objects to fetch.
	Items []ObjectFetchItem
}

// ObjectFetchItem identifies a single object to fetch.
type ObjectFetchItem struct {
	// ObjectRef is the storage path/key.
	ObjectRef string

	// Checksum is the expected SHA-256 for verification (optional).
	Checksum []byte
}

// StreamFetchObjectsResponse contains results for multiple fetched objects.
type StreamFetchObjectsResponse struct {
	// Objects contains the fetched object responses.
	Objects []FetchObjectResponse

	// SuccessCount is the number of successfully fetched objects.
	SuccessCount int

	// FailureCount is the number of failed fetches.
	FailureCount int

	// TotalCount is the total number of requested objects.
	TotalCount int
}
