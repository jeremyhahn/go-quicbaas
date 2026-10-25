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

package interfaces

import (
	"context"
)

// VectorNamespaceInfo contains metadata about a vector namespace.
type VectorNamespaceInfo struct {
	// Name is the namespace identifier.
	Name string
	// Dimension is the vector dimensionality.
	Dimension uint32
	// VectorCount is the number of vectors in the namespace.
	VectorCount uint64
	// DistanceMetric is the distance metric (e.g. "l2", "cosine", "dot_product").
	DistanceMetric string
	// IndexType is the index algorithm (e.g. "HNSW", "FLAT").
	IndexType string
	// IndexConfig contains index-specific parameters.
	IndexConfig map[string]string
	// Metadata contains namespace metadata.
	Metadata map[string]string
	// TenantID is the owning tenant (empty for shared/system namespaces).
	TenantID string
	// StorageBytes is the total storage size in bytes.
	StorageBytes uint64
	// CreatedAt is the creation timestamp (Unix nanos).
	CreatedAt int64
	// UpdatedAt is the last update timestamp (Unix nanos).
	UpdatedAt int64
}

// VectorValue represents a stored vector with its metadata.
type VectorValue struct {
	// ID is the unique vector identifier.
	ID uint64
	// Embedding contains the vector embedding values.
	Embedding []float32
	// QuantizedEmbedding contains the optional quantized vector data.
	// When present, this holds the compressed representation (SQ8 uint8 codes,
	// PQ codes, or float16/bfloat16 bytes) used for storage efficiency.
	// The Embedding field may be nil when only the quantized form is stored.
	QuantizedEmbedding []byte
	// Metadata contains user-defined attributes.
	Metadata map[string]string
	// Version is the MVCC version.
	Version uint64
	// CreatedAt is the creation timestamp (Unix nanos).
	CreatedAt int64
	// UpdatedAt is the last update timestamp (Unix nanos).
	UpdatedAt int64
}

// VectorMetadataFilter represents a metadata filter for vector search.
type VectorMetadataFilter struct {
	// Field is the metadata field to filter on.
	Field string
	// Value is the value to match against (for single-value operators).
	Value interface{}
	// Values is the set of values to match against (for IN/NOT_IN operators).
	Values []interface{}
	// Operator is the filter operator (e.g. VectorFilterEqual).
	Operator VectorFilterOp
}

// VectorFilterOp defines filter operators for metadata queries.
type VectorFilterOp int

const (
	// VectorFilterEqual matches exact equality.
	VectorFilterEqual VectorFilterOp = iota
	// VectorFilterNotEqual matches when values are not equal.
	VectorFilterNotEqual
	// VectorFilterGreaterThan matches when field value is greater than filter value.
	VectorFilterGreaterThan
	// VectorFilterGreaterEqual matches when field value is greater than or equal.
	VectorFilterGreaterEqual
	// VectorFilterLessThan matches when field value is less than filter value.
	VectorFilterLessThan
	// VectorFilterLessEqual matches when field value is less than or equal.
	VectorFilterLessEqual
	// VectorFilterIn matches when field value is in a set of values.
	VectorFilterIn
	// VectorFilterNotIn matches when field value is not in a set of values.
	VectorFilterNotIn
	// VectorFilterContains matches substring presence.
	VectorFilterContains
	// VectorFilterNotContains matches substring absence.
	VectorFilterNotContains
	// VectorFilterPrefixMatch matches when field value starts with filter value.
	VectorFilterPrefixMatch
	// VectorFilterIsNull matches when the field value is empty.
	VectorFilterIsNull
	// VectorFilterIsNotNull matches when the field value is non-empty.
	VectorFilterIsNotNull
	// VectorFilterRange matches when the field value is in a range.
	VectorFilterRange
	// VectorFilterTextMatch performs a case-insensitive substring match.
	VectorFilterTextMatch
)

// VectorScanOptions configures vector scan/list behavior.
type VectorScanOptions struct {
	// Limit restricts the number of results.
	Limit int
	// Offset is the pagination offset.
	Offset int
	// Cursor is the pagination cursor.
	Cursor string
	// IncludeVectors includes embedding values in results.
	IncludeVectors bool
	// IncludeMetadata includes metadata in results.
	IncludeMetadata bool
}

// VectorStorageStats holds storage statistics for a namespace.
type VectorStorageStats struct {
	// VectorCount is the number of vectors.
	VectorCount uint64
	// Dimension is the namespace's vector dimension.
	Dimension uint32
	// SizeBytes is the total storage size in bytes.
	SizeBytes uint64
	// IndexSizeBytes is the index size in bytes.
	IndexSizeBytes uint64
	// MemoryUsageBytes is the memory usage in bytes.
	MemoryUsageBytes uint64
}

// VectorScanPage is one page of a namespace scan.
type VectorScanPage struct {
	// Vectors are the page's vectors.
	Vectors []*VectorValue
	// TotalCount is the number of vectors in the whole namespace.
	TotalCount uint64
}

// VectorStorage defines the interface for vector storage backends.
// It provides namespace management, vector CRUD, and search operations.
type VectorStorage interface {
	// CreateNamespace creates a new vector namespace.
	CreateNamespace(ctx context.Context, info *VectorNamespaceInfo) error

	// DeleteNamespace removes a namespace and all its vectors.
	DeleteNamespace(ctx context.Context, namespace string) error

	// ListNamespaces returns all available vector namespaces.
	ListNamespaces(ctx context.Context) ([]VectorNamespaceInfo, error)

	// GetNamespaceInfo retrieves metadata about a specific namespace.
	GetNamespaceInfo(ctx context.Context, namespace string) (*VectorNamespaceInfo, error)

	// UpsertVector inserts or updates a single vector.
	UpsertVector(ctx context.Context, namespace string, vec *VectorValue) error

	// GetVector retrieves a vector by namespace and ID.
	GetVector(ctx context.Context, namespace string, id uint64) (*VectorValue, error)

	// DeleteVector removes a single vector by namespace and ID.
	DeleteVector(ctx context.Context, namespace string, id uint64) error

	// BatchUpsertVectors inserts or updates multiple vectors atomically.
	BatchUpsertVectors(ctx context.Context, namespace string, vecs []*VectorValue) error

	// BatchDeleteVectors deletes multiple vectors by IDs.
	BatchDeleteVectors(ctx context.Context, namespace string, ids []uint64) error

	// ScanNamespace lists vectors in a namespace with optional pagination.
	ScanNamespace(ctx context.Context, namespace string, opts *VectorScanOptions) ([]*VectorValue, error)

	// ScanNamespacePage lists one page of a namespace's vectors together with
	// the namespace's total vector count.
	ScanNamespacePage(ctx context.Context, namespace string, opts *VectorScanOptions) (*VectorScanPage, error)

	// ScanByMultipleFilters scans vectors matching multiple metadata filters.
	ScanByMultipleFilters(ctx context.Context, namespace string, filters []VectorMetadataFilter, opts *VectorScanOptions) ([]*VectorValue, error)

	// GetStats returns storage statistics for a namespace.
	GetStats(ctx context.Context, namespace string) (*VectorStorageStats, error)

	// Compact compacts a namespace. When async is set it returns once the
	// compaction is accepted, and the compaction runs in the background.
	Compact(ctx context.Context, namespace string, async bool) error

	// Close releases storage resources.
	Close() error
}
