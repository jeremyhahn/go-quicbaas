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

package objecttransfer

// =============================================================================
// Compression Algorithm Types
// =============================================================================

// CompressionAlgo represents the compression algorithm used for object data.
type CompressionAlgo string

const (
	// CompressionNone indicates no compression.
	CompressionNone CompressionAlgo = "none"

	// CompressionLZ4 indicates LZ4 compression.
	CompressionLZ4 CompressionAlgo = "lz4"

	// CompressionZstd indicates Zstandard compression.
	CompressionZstd CompressionAlgo = "zstd"

	// CompressionSnappy indicates Snappy compression.
	CompressionSnappy CompressionAlgo = "snappy"

	// CompressionGzip indicates Gzip compression.
	CompressionGzip CompressionAlgo = "gzip"
)

// =============================================================================
// Fetch Object Types
// =============================================================================

// FetchObjectRequest contains parameters for fetching a single object.
type FetchObjectRequest struct {
	// ShardID is the shard containing the object.
	ShardID uint64

	// ObjectRef is the object reference in "bucket:key" format.
	ObjectRef string

	// Checksum is the expected checksum for verification (optional).
	// If provided, the server verifies the object's checksum matches.
	Checksum []byte

	// RequestID is a unique identifier for this request (optional).
	// Used for tracing and deduplication.
	RequestID string
}

// FetchObjectResponse contains the result of fetching an object.
type FetchObjectResponse struct {
	// ObjectRef is the object reference that was requested.
	ObjectRef string

	// Data is the object data (empty if not found).
	Data []byte

	// Checksum is the SHA-256 checksum of the data.
	Checksum []byte

	// Size is the uncompressed object size in bytes.
	Size int64

	// Compressed indicates if the data is compressed.
	Compressed bool

	// CompressionAlgo is the compression algorithm used (if compressed).
	CompressionAlgo CompressionAlgo

	// Found indicates if the object was found.
	Found bool

	// ErrorMessage contains an error message if the fetch failed.
	ErrorMessage string
}

// =============================================================================
// Stream Fetch Types
// =============================================================================

// StreamFetchObjectsRequest contains parameters for streaming multiple objects.
type StreamFetchObjectsRequest struct {
	// ShardID is the shard containing the objects.
	ShardID uint64

	// Items is the list of objects to fetch.
	Items []ObjectFetchItem
}

// ObjectFetchItem represents a single object to fetch in a batch request.
type ObjectFetchItem struct {
	// ObjectRef is the object reference in "bucket:key" format.
	ObjectRef string

	// Checksum is the expected checksum for verification (optional).
	Checksum []byte
}
