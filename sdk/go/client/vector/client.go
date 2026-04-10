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

package vector

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Vector"
)

// =============================================================================
// Type Definitions
// =============================================================================

// IndexType specifies the type of vector index.
type IndexType string

const (
	// IndexTypeHNSW uses Hierarchical Navigable Small World graph.
	IndexTypeHNSW IndexType = "hnsw"

	// IndexTypeFlat uses flat (brute-force) search.
	IndexTypeFlat IndexType = "flat"

	// IndexTypeIVF uses Inverted File Index.
	IndexTypeIVF IndexType = "ivf"
)

// FilterOperator specifies the operator for metadata filters.
type FilterOperator string

const (
	// FilterOperatorEquals checks for equality.
	FilterOperatorEquals FilterOperator = "eq"

	// FilterOperatorNotEquals checks for inequality.
	FilterOperatorNotEquals FilterOperator = "ne"

	// FilterOperatorGreaterThan checks if greater than.
	FilterOperatorGreaterThan FilterOperator = "gt"

	// FilterOperatorGreaterThanOrEqual checks if greater than or equal.
	FilterOperatorGreaterThanOrEqual FilterOperator = "gte"

	// FilterOperatorLessThan checks if less than.
	FilterOperatorLessThan FilterOperator = "lt"

	// FilterOperatorLessThanOrEqual checks if less than or equal.
	FilterOperatorLessThanOrEqual FilterOperator = "lte"

	// FilterOperatorIn checks if value is in a set.
	FilterOperatorIn FilterOperator = "in"

	// FilterOperatorContains checks if string contains substring.
	FilterOperatorContains FilterOperator = "contains"
)

// =============================================================================
// Namespace Types
// =============================================================================

// NamespaceConfig contains configuration for a vector namespace.
type NamespaceConfig struct {
	// Namespace is the namespace name.
	Namespace string

	// Dimensions is the vector dimensionality.
	Dimensions int32

	// IndexType is the type of index to use.
	IndexType IndexType

	// IndexParams are additional index-specific parameters.
	IndexParams map[string]string

	// Metadata is optional namespace metadata.
	Metadata map[string]string
}

// NamespaceInfo contains information about a namespace.
type NamespaceInfo struct {
	// Config is the namespace configuration.
	Config *NamespaceConfig

	// VectorCount is the number of vectors in the namespace.
	VectorCount int64

	// IndexBytes is the index size in bytes.
	IndexBytes int64

	// StorageBytes is the total storage size in bytes.
	StorageBytes int64

	// CreatedAt is when the namespace was created.
	CreatedAt time.Time

	// UpdatedAt is when the namespace was last updated.
	UpdatedAt time.Time
}

// CreateNamespaceRequest is a request to create a namespace.
type CreateNamespaceRequest struct {
	// Config is the namespace configuration.
	Config *NamespaceConfig
}

// CreateNamespaceResponse is the response from namespace creation.
type CreateNamespaceResponse struct {
	// Namespace is the created namespace name.
	Namespace string

	// Success indicates if the operation succeeded.
	Success bool

	// Timestamp is when the namespace was created.
	Timestamp time.Time
}

// DeleteNamespaceRequest is a request to delete a namespace.
type DeleteNamespaceRequest struct {
	// Namespace is the namespace to delete.
	Namespace string
}

// DeleteNamespaceResponse is the response from namespace deletion.
type DeleteNamespaceResponse struct {
	// Deleted indicates if the namespace was deleted.
	Deleted bool

	// VectorsDeleted is the number of vectors deleted.
	VectorsDeleted int64

	// Timestamp is when the namespace was deleted.
	Timestamp time.Time
}

// ListNamespacesRequest is a request to list namespaces.
type ListNamespacesRequest struct {
	// Prefix filters namespaces by prefix.
	Prefix string

	// PageSize is the maximum number of results per page.
	PageSize int32

	// PageToken is the pagination token.
	PageToken string
}

// ListNamespacesResponse is the response from listing namespaces.
type ListNamespacesResponse struct {
	// Namespaces is the list of namespace info.
	Namespaces []*NamespaceInfo

	// TotalCount is the total number of namespaces.
	TotalCount int64

	// NextPageToken is the token for the next page.
	NextPageToken string

	// Timestamp is when the list was generated.
	Timestamp time.Time
}

// GetNamespaceInfoRequest is a request to get namespace info.
type GetNamespaceInfoRequest struct {
	// Namespace is the namespace name.
	Namespace string

	// IncludeStats includes detailed statistics.
	IncludeStats bool
}

// GetNamespaceInfoResponse is the response from getting namespace info.
type GetNamespaceInfoResponse struct {
	// Info is the namespace information.
	Info *NamespaceInfo

	// Timestamp is when the info was retrieved.
	Timestamp time.Time
}

// =============================================================================
// Vector Types
// =============================================================================

// VectorEntity represents a vector with its metadata.
type VectorEntity struct {
	// ID is the vector identifier.
	ID string

	// Namespace is the namespace containing the vector.
	Namespace string

	// Vector is the vector data.
	Vector []float32

	// Metadata is optional key-value metadata.
	Metadata map[string]string

	// Version is the vector version for MVCC.
	Version int64

	// CreatedAt is when the vector was created.
	CreatedAt time.Time

	// UpdatedAt is when the vector was last updated.
	UpdatedAt time.Time
}

// UpsertRequest is a request to insert or update a vector.
type UpsertRequest struct {
	// Vector is the vector to upsert.
	Vector *VectorEntity
}

// UpsertResponse is the response from upserting a vector.
type UpsertResponse struct {
	// ID is the vector ID.
	ID string

	// Version is the new version number.
	Version int64

	// Success indicates if the operation succeeded.
	Success bool

	// Timestamp is when the vector was upserted.
	Timestamp time.Time
}

// BatchUpsertRequest is a request to batch upsert vectors.
type BatchUpsertRequest struct {
	// Vectors is the list of vectors to upsert.
	Vectors []*VectorEntity
}

// BatchUpsertResponse is the response from batch upserting.
type BatchUpsertResponse struct {
	// VectorsSucceeded is the number of vectors successfully upserted.
	VectorsSucceeded int64

	// VectorsFailed is the number of vectors that failed.
	VectorsFailed int64

	// Errors contains per-vector error messages.
	Errors map[string]string

	// Timestamp is when the batch was processed.
	Timestamp time.Time
}

// GetRequest is a request to get a vector.
type GetRequest struct {
	// Namespace is the namespace name.
	Namespace string

	// ID is the vector ID.
	ID string

	// IncludeVector includes the vector data in the response.
	IncludeVector bool

	// IncludeMetadata includes the metadata in the response.
	IncludeMetadata bool
}

// GetResponse is the response from getting a vector.
type GetResponse struct {
	// Vector is the retrieved vector.
	Vector *VectorEntity

	// Found indicates if the vector was found.
	Found bool

	// Timestamp is when the vector was retrieved.
	Timestamp time.Time
}

// DeleteRequest is a request to delete a vector.
type DeleteRequest struct {
	// Namespace is the namespace name.
	Namespace string

	// ID is the vector ID.
	ID string
}

// DeleteResponse is the response from deleting a vector.
type DeleteResponse struct {
	// Deleted indicates if the vector was deleted.
	Deleted bool

	// Timestamp is when the vector was deleted.
	Timestamp time.Time
}

// BatchDeleteRequest is a request to batch delete vectors.
type BatchDeleteRequest struct {
	// Namespace is the namespace name.
	Namespace string

	// IDs is the list of vector IDs to delete.
	IDs []string
}

// BatchDeleteResponse is the response from batch deleting.
type BatchDeleteResponse struct {
	// DeletedCount is the number of vectors deleted.
	DeletedCount int64

	// Errors contains per-vector error messages.
	Errors map[string]string

	// Timestamp is when the batch was processed.
	Timestamp time.Time
}

// ListVectorsRequest is a request to list vectors.
type ListVectorsRequest struct {
	// Namespace is the namespace name.
	Namespace string

	// PageSize is the maximum number of results per page.
	PageSize int32

	// PageToken is the pagination token.
	PageToken string

	// IncludeVectors includes vector data in the response.
	IncludeVectors bool

	// IncludeMetadata includes metadata in the response.
	IncludeMetadata bool
}

// ListVectorsResponse is the response from listing vectors.
type ListVectorsResponse struct {
	// Vectors is the list of vectors.
	Vectors []*VectorEntity

	// TotalCount is the total number of vectors.
	TotalCount int64

	// NextPageToken is the token for the next page.
	NextPageToken string

	// Timestamp is when the list was generated.
	Timestamp time.Time
}

// UpdateMetadataRequest is a request to update vector metadata.
type UpdateMetadataRequest struct {
	// Namespace is the namespace name.
	Namespace string

	// ID is the vector ID.
	ID string

	// Metadata is the new metadata.
	Metadata map[string]string

	// Merge merges with existing metadata if true.
	Merge bool
}

// UpdateMetadataResponse is the response from updating metadata.
type UpdateMetadataResponse struct {
	// Success indicates if the operation succeeded.
	Success bool

	// Version is the new version number.
	Version int64

	// Timestamp is when the metadata was updated.
	Timestamp time.Time
}

// =============================================================================
// Search Types
// =============================================================================

// MetadataFilter represents a metadata filter condition.
type MetadataFilter struct {
	// Key is the metadata key to filter on.
	Key string

	// Operator is the filter operator.
	Operator FilterOperator

	// Value is the filter value.
	Value string
}

// SearchResult represents a single search result.
type SearchResult struct {
	// Vector is the matching vector.
	Vector *VectorEntity

	// Score is the similarity score.
	Score float32

	// Distance is the distance from the query.
	Distance float32

	// Rank is the result rank.
	Rank int32
}

// SearchSimilarRequest is a request to search for similar vectors.
type SearchSimilarRequest struct {
	// Namespace is the namespace to search.
	Namespace string

	// QueryVector is the query vector.
	QueryVector []float32

	// K is the number of results to return.
	K int32

	// EfSearch is the HNSW efSearch parameter.
	EfSearch int32

	// MinScore is the minimum similarity score.
	MinScore float32

	// IncludeVectors includes vector data in results.
	IncludeVectors bool

	// IncludeMetadata includes metadata in results.
	IncludeMetadata bool
}

// SearchSimilarResponse is the response from similarity search.
type SearchSimilarResponse struct {
	// Results is the list of search results.
	Results []*SearchResult

	// TotalCandidates is the total candidates considered.
	TotalCandidates int64

	// SearchTimeMs is the search time in milliseconds.
	SearchTimeMs int64

	// Timestamp is when the search was performed.
	Timestamp time.Time
}

// SearchByIDRequest is a request to search by vector ID.
type SearchByIDRequest struct {
	// Namespace is the namespace to search.
	Namespace string

	// ID is the vector ID to search from.
	ID string

	// K is the number of results to return.
	K int32

	// EfSearch is the HNSW efSearch parameter.
	EfSearch int32

	// MinScore is the minimum similarity score.
	MinScore float32

	// IncludeSelf includes the query vector in results.
	IncludeSelf bool

	// IncludeVectors includes vector data in results.
	IncludeVectors bool

	// IncludeMetadata includes metadata in results.
	IncludeMetadata bool
}

// SearchByIDResponse is the response from search by ID.
type SearchByIDResponse struct {
	// Results is the list of search results.
	Results []*SearchResult

	// TotalCandidates is the total candidates considered.
	TotalCandidates int64

	// SearchTimeMs is the search time in milliseconds.
	SearchTimeMs int64

	// Timestamp is when the search was performed.
	Timestamp time.Time
}

// HybridSearchRequest is a request for hybrid search.
type HybridSearchRequest struct {
	// Namespace is the namespace to search.
	Namespace string

	// QueryVector is the query vector.
	QueryVector []float32

	// K is the number of results to return.
	K int32

	// EfSearch is the HNSW efSearch parameter.
	EfSearch int32

	// MinScore is the minimum similarity score.
	MinScore float32

	// MetadataFilters are the metadata filter conditions.
	MetadataFilters []*MetadataFilter

	// FilterOperator is the operator for combining filters (AND/OR).
	FilterOperator string

	// RerankingAlgorithm is the reranking algorithm to use.
	RerankingAlgorithm string

	// IncludeVectors includes vector data in results.
	IncludeVectors bool

	// IncludeMetadata includes metadata in results.
	IncludeMetadata bool
}

// HybridSearchResponse is the response from hybrid search.
type HybridSearchResponse struct {
	// Results is the list of search results.
	Results []*SearchResult

	// TotalCandidates is the total candidates considered.
	TotalCandidates int64

	// TotalFiltered is the count after filtering.
	TotalFiltered int64

	// RerankingApplied indicates if reranking was applied.
	RerankingApplied bool

	// SearchTimeMs is the search time in milliseconds.
	SearchTimeMs int64

	// Timestamp is when the search was performed.
	Timestamp time.Time
}

// SearchByMetadataRequest is a request to search by metadata.
type SearchByMetadataRequest struct {
	// Namespace is the namespace to search.
	Namespace string

	// MetadataFilters are the metadata filter conditions.
	MetadataFilters []*MetadataFilter

	// FilterOperator is the operator for combining filters (AND/OR).
	FilterOperator string

	// Limit is the maximum number of results.
	Limit int32

	// Offset is the pagination offset.
	Offset int32

	// IncludeVectors includes vector data in results.
	IncludeVectors bool

	// IncludeMetadata includes metadata in results.
	IncludeMetadata bool
}

// SearchByMetadataResponse is the response from metadata search.
type SearchByMetadataResponse struct {
	// Results is the list of matching vectors.
	Results []*VectorEntity

	// TotalCount is the total matching count.
	TotalCount int64

	// NextCursor is the cursor for the next page.
	NextCursor string

	// SearchTimeMs is the search time in milliseconds.
	SearchTimeMs int64

	// Timestamp is when the search was performed.
	Timestamp time.Time
}

// =============================================================================
// Index Types
// =============================================================================

// RebuildIndexRequest is a request to rebuild the index.
type RebuildIndexRequest struct {
	// Namespace is the namespace name.
	Namespace string

	// IndexParams are the new index parameters.
	IndexParams map[string]string

	// Async runs the rebuild asynchronously.
	Async bool
}

// RebuildIndexResponse is the response from index rebuild.
type RebuildIndexResponse struct {
	// OperationID is the operation identifier.
	OperationID string

	// Status is the operation status.
	Status string

	// EstimatedDurationMs is the estimated duration.
	EstimatedDurationMs int64

	// Started is when the operation started.
	Started time.Time

	// Timestamp is when the response was generated.
	Timestamp time.Time
}

// GetIndexProgressRequest is a request to get index progress.
type GetIndexProgressRequest struct {
	// OperationID is the operation identifier.
	OperationID string

	// Namespace is the namespace name.
	Namespace string
}

// GetIndexProgressResponse is the response from getting index progress.
type GetIndexProgressResponse struct {
	// OperationID is the operation identifier.
	OperationID string

	// Namespace is the namespace name.
	Namespace string

	// Status is the operation status.
	Status string

	// PercentComplete is the completion percentage.
	PercentComplete float32

	// VectorsIndexed is the number of vectors indexed.
	VectorsIndexed int64

	// TotalVectors is the total number of vectors.
	TotalVectors int64

	// ElapsedTimeMs is the elapsed time in milliseconds.
	ElapsedTimeMs int64

	// EstimatedRemainingMs is the estimated remaining time.
	EstimatedRemainingMs int64

	// ErrorMessage contains any error message.
	ErrorMessage string

	// Timestamp is when the progress was retrieved.
	Timestamp time.Time
}

// =============================================================================
// Admin Types
// =============================================================================

// ExportRequest is a request to export vectors.
type ExportRequest struct {
	// Namespace is the namespace to export.
	Namespace string

	// Format is the export format (json, parquet, etc.).
	Format string

	// Compression is the compression type (none, gzip, etc.).
	Compression string

	// BatchSize is the number of vectors per batch.
	BatchSize int32
}

// ExportResponse is the response from exporting vectors.
type ExportResponse struct {
	// BatchData is the exported data.
	BatchData []byte

	// VectorsExported is the number of vectors exported.
	VectorsExported int64

	// IsFinal indicates if this is the final batch.
	IsFinal bool

	// Timestamp is when the export was generated.
	Timestamp time.Time
}

// ImportRequest is a request to import vectors.
type ImportRequest struct {
	// Namespace is the namespace to import into.
	Namespace string

	// Format is the import format.
	Format string

	// BatchData is the data to import.
	BatchData []byte

	// OverwriteExisting overwrites existing vectors.
	OverwriteExisting bool
}

// ImportResponse is the response from importing vectors.
type ImportResponse struct {
	// VectorsImported is the number of vectors imported.
	VectorsImported int64

	// VectorsSkipped is the number of vectors skipped.
	VectorsSkipped int64

	// VectorsFailed is the number of vectors that failed.
	VectorsFailed int64

	// DurationMs is the import duration in milliseconds.
	DurationMs int64

	// Timestamp is when the import completed.
	Timestamp time.Time
}

// CompactRequest is a request to compact a namespace.
type CompactRequest struct {
	// Namespace is the namespace to compact.
	Namespace string

	// Async runs compaction asynchronously.
	Async bool
}

// CompactResponse is the response from compaction.
type CompactResponse struct {
	// OperationID is the operation identifier.
	OperationID string

	// Started is when the operation started.
	Started time.Time

	// Timestamp is when the response was generated.
	Timestamp time.Time
}

// GetCompactionProgressRequest is a request to get compaction progress.
type GetCompactionProgressRequest struct {
	// OperationID is the operation identifier.
	OperationID string

	// Namespace is the namespace name.
	Namespace string
}

// GetCompactionProgressResponse is the response from getting compaction progress.
type GetCompactionProgressResponse struct {
	// OperationID is the operation identifier.
	OperationID string

	// Namespace is the namespace name.
	Namespace string

	// Status is the operation status.
	Status string

	// PercentComplete is the completion percentage.
	PercentComplete float32

	// BytesCompacted is the number of bytes compacted.
	BytesCompacted int64

	// TotalBytes is the total bytes to compact.
	TotalBytes int64

	// SpaceReclaimedBytes is the space reclaimed.
	SpaceReclaimedBytes int64

	// ElapsedTimeMs is the elapsed time in milliseconds.
	ElapsedTimeMs int64

	// Timestamp is when the progress was retrieved.
	Timestamp time.Time
}

// GetStatsRequest is a request to get vector stats.
type GetStatsRequest struct {
	// Namespace is the namespace name.
	Namespace string
}

// GetStatsResponse is the response from getting stats.
type GetStatsResponse struct {
	// Namespace is the namespace name.
	Namespace string

	// VectorCount is the number of vectors.
	VectorCount int64

	// IndexSizeBytes is the index size in bytes.
	IndexSizeBytes int64

	// StorageSizeBytes is the total storage size.
	StorageSizeBytes int64

	// Timestamp is when the stats were retrieved.
	Timestamp time.Time
}

// =============================================================================
// Client Interface
// =============================================================================

// Client provides vector similarity search operations.
type Client interface {
	// Namespace Management
	CreateNamespace(ctx context.Context, req *CreateNamespaceRequest) (*CreateNamespaceResponse, error)
	DeleteNamespace(ctx context.Context, req *DeleteNamespaceRequest) (*DeleteNamespaceResponse, error)
	ListNamespaces(ctx context.Context, req *ListNamespacesRequest) (*ListNamespacesResponse, error)
	GetNamespaceInfo(ctx context.Context, req *GetNamespaceInfoRequest) (*GetNamespaceInfoResponse, error)

	// Vector CRUD
	Upsert(ctx context.Context, req *UpsertRequest) (*UpsertResponse, error)
	BatchUpsert(ctx context.Context, req *BatchUpsertRequest) (*BatchUpsertResponse, error)
	Get(ctx context.Context, req *GetRequest) (*GetResponse, error)
	Delete(ctx context.Context, req *DeleteRequest) (*DeleteResponse, error)
	BatchDelete(ctx context.Context, req *BatchDeleteRequest) (*BatchDeleteResponse, error)
	List(ctx context.Context, req *ListVectorsRequest) (*ListVectorsResponse, error)
	UpdateMetadata(ctx context.Context, req *UpdateMetadataRequest) (*UpdateMetadataResponse, error)

	// Search Operations
	SearchSimilar(ctx context.Context, req *SearchSimilarRequest) (*SearchSimilarResponse, error)
	SearchByID(ctx context.Context, req *SearchByIDRequest) (*SearchByIDResponse, error)
	HybridSearch(ctx context.Context, req *HybridSearchRequest) (*HybridSearchResponse, error)
	SearchByMetadata(ctx context.Context, req *SearchByMetadataRequest) (*SearchByMetadataResponse, error)

	// Index Management
	RebuildIndex(ctx context.Context, req *RebuildIndexRequest) (*RebuildIndexResponse, error)
	GetIndexProgress(ctx context.Context, req *GetIndexProgressRequest) (*GetIndexProgressResponse, error)

	// Admin Operations
	Export(ctx context.Context, req *ExportRequest) ([]*ExportResponse, error)
	Import(ctx context.Context, batches []*ImportRequest) (*ImportResponse, error)
	Compact(ctx context.Context, req *CompactRequest) (*CompactResponse, error)
	GetCompactionProgress(ctx context.Context, req *GetCompactionProgressRequest) (*GetCompactionProgressResponse, error)
	GetStats(ctx context.Context, req *GetStatsRequest) (*GetStatsResponse, error)
}

// =============================================================================
// Client Implementation
// =============================================================================

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new vector client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// =============================================================================
// Namespace Management
// =============================================================================

// CreateNamespace creates a new vector namespace.
func (c *client) CreateNamespace(ctx context.Context, req *CreateNamespaceRequest) (*CreateNamespaceResponse, error) {
	if err := validateCreateNamespaceRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.CreateNamespace", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "CreateNamespace", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("vector.CreateNamespace", "nil response from server")
	}

	createResp, ok := resp.(*CreateNamespaceResponse)
	if !ok {
		return nil, errors.NewInternal("vector.CreateNamespace", "invalid response type")
	}

	return createResp, nil
}

// DeleteNamespace deletes a vector namespace.
func (c *client) DeleteNamespace(ctx context.Context, req *DeleteNamespaceRequest) (*DeleteNamespaceResponse, error) {
	if err := validateDeleteNamespaceRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.DeleteNamespace", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "DeleteNamespace", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("vector.DeleteNamespace", "nil response from server")
	}

	deleteResp, ok := resp.(*DeleteNamespaceResponse)
	if !ok {
		return nil, errors.NewInternal("vector.DeleteNamespace", "invalid response type")
	}

	return deleteResp, nil
}

// ListNamespaces lists all vector namespaces.
func (c *client) ListNamespaces(ctx context.Context, req *ListNamespacesRequest) (*ListNamespacesResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("vector.ListNamespaces", "request is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.ListNamespaces", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListNamespaces", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListNamespacesResponse{Namespaces: []*NamespaceInfo{}}, nil
	}

	listResp, ok := resp.(*ListNamespacesResponse)
	if !ok {
		return nil, errors.NewInternal("vector.ListNamespaces", "invalid response type")
	}

	return listResp, nil
}

// GetNamespaceInfo gets information about a namespace.
func (c *client) GetNamespaceInfo(ctx context.Context, req *GetNamespaceInfoRequest) (*GetNamespaceInfoResponse, error) {
	if err := validateGetNamespaceInfoRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.GetNamespaceInfo", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetNamespaceInfo", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("vector.GetNamespaceInfo", "nil response from server")
	}

	infoResp, ok := resp.(*GetNamespaceInfoResponse)
	if !ok {
		return nil, errors.NewInternal("vector.GetNamespaceInfo", "invalid response type")
	}

	return infoResp, nil
}

// =============================================================================
// Vector CRUD
// =============================================================================

// Upsert inserts or updates a vector.
func (c *client) Upsert(ctx context.Context, req *UpsertRequest) (*UpsertResponse, error) {
	if err := validateUpsertRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.Upsert", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Upsert", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("vector.Upsert", "nil response from server")
	}

	upsertResp, ok := resp.(*UpsertResponse)
	if !ok {
		return nil, errors.NewInternal("vector.Upsert", "invalid response type")
	}

	return upsertResp, nil
}

// BatchUpsert batch inserts or updates vectors.
func (c *client) BatchUpsert(ctx context.Context, req *BatchUpsertRequest) (*BatchUpsertResponse, error) {
	if err := validateBatchUpsertRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.BatchUpsert", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "BatchUpsert", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("vector.BatchUpsert", "nil response from server")
	}

	batchResp, ok := resp.(*BatchUpsertResponse)
	if !ok {
		return nil, errors.NewInternal("vector.BatchUpsert", "invalid response type")
	}

	return batchResp, nil
}

// Get retrieves a vector by ID.
func (c *client) Get(ctx context.Context, req *GetRequest) (*GetResponse, error) {
	if err := validateGetRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.Get", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Get", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &GetResponse{Found: false}, nil
	}

	getResp, ok := resp.(*GetResponse)
	if !ok {
		return nil, errors.NewInternal("vector.Get", "invalid response type")
	}

	return getResp, nil
}

// Delete deletes a vector.
func (c *client) Delete(ctx context.Context, req *DeleteRequest) (*DeleteResponse, error) {
	if err := validateDeleteRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.Delete", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Delete", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &DeleteResponse{Deleted: false}, nil
	}

	deleteResp, ok := resp.(*DeleteResponse)
	if !ok {
		return nil, errors.NewInternal("vector.Delete", "invalid response type")
	}

	return deleteResp, nil
}

// BatchDelete batch deletes vectors.
func (c *client) BatchDelete(ctx context.Context, req *BatchDeleteRequest) (*BatchDeleteResponse, error) {
	if err := validateBatchDeleteRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.BatchDelete", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "BatchDelete", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("vector.BatchDelete", "nil response from server")
	}

	batchResp, ok := resp.(*BatchDeleteResponse)
	if !ok {
		return nil, errors.NewInternal("vector.BatchDelete", "invalid response type")
	}

	return batchResp, nil
}

// List lists vectors in a namespace.
func (c *client) List(ctx context.Context, req *ListVectorsRequest) (*ListVectorsResponse, error) {
	if err := validateListVectorsRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.List", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "List", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListVectorsResponse{Vectors: []*VectorEntity{}}, nil
	}

	listResp, ok := resp.(*ListVectorsResponse)
	if !ok {
		return nil, errors.NewInternal("vector.List", "invalid response type")
	}

	return listResp, nil
}

// UpdateMetadata updates vector metadata.
func (c *client) UpdateMetadata(ctx context.Context, req *UpdateMetadataRequest) (*UpdateMetadataResponse, error) {
	if err := validateUpdateMetadataRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.UpdateMetadata", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "UpdateMetadata", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("vector.UpdateMetadata", "nil response from server")
	}

	updateResp, ok := resp.(*UpdateMetadataResponse)
	if !ok {
		return nil, errors.NewInternal("vector.UpdateMetadata", "invalid response type")
	}

	return updateResp, nil
}

// =============================================================================
// Search Operations
// =============================================================================

// SearchSimilar searches for similar vectors.
func (c *client) SearchSimilar(ctx context.Context, req *SearchSimilarRequest) (*SearchSimilarResponse, error) {
	if err := validateSearchSimilarRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.SearchSimilar", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "SearchSimilar", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &SearchSimilarResponse{Results: []*SearchResult{}}, nil
	}

	searchResp, ok := resp.(*SearchSimilarResponse)
	if !ok {
		return nil, errors.NewInternal("vector.SearchSimilar", "invalid response type")
	}

	return searchResp, nil
}

// SearchByID searches for similar vectors by ID.
func (c *client) SearchByID(ctx context.Context, req *SearchByIDRequest) (*SearchByIDResponse, error) {
	if err := validateSearchByIDRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.SearchByID", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "SearchByID", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &SearchByIDResponse{Results: []*SearchResult{}}, nil
	}

	searchResp, ok := resp.(*SearchByIDResponse)
	if !ok {
		return nil, errors.NewInternal("vector.SearchByID", "invalid response type")
	}

	return searchResp, nil
}

// HybridSearch performs hybrid search with metadata filtering.
func (c *client) HybridSearch(ctx context.Context, req *HybridSearchRequest) (*HybridSearchResponse, error) {
	if err := validateHybridSearchRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.HybridSearch", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "HybridSearch", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &HybridSearchResponse{Results: []*SearchResult{}}, nil
	}

	searchResp, ok := resp.(*HybridSearchResponse)
	if !ok {
		return nil, errors.NewInternal("vector.HybridSearch", "invalid response type")
	}

	return searchResp, nil
}

// SearchByMetadata searches vectors by metadata.
func (c *client) SearchByMetadata(ctx context.Context, req *SearchByMetadataRequest) (*SearchByMetadataResponse, error) {
	if err := validateSearchByMetadataRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.SearchByMetadata", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "SearchByMetadata", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &SearchByMetadataResponse{Results: []*VectorEntity{}}, nil
	}

	searchResp, ok := resp.(*SearchByMetadataResponse)
	if !ok {
		return nil, errors.NewInternal("vector.SearchByMetadata", "invalid response type")
	}

	return searchResp, nil
}

// =============================================================================
// Index Management
// =============================================================================

// RebuildIndex rebuilds the vector index.
func (c *client) RebuildIndex(ctx context.Context, req *RebuildIndexRequest) (*RebuildIndexResponse, error) {
	if err := validateRebuildIndexRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.RebuildIndex", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "RebuildIndex", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("vector.RebuildIndex", "nil response from server")
	}

	rebuildResp, ok := resp.(*RebuildIndexResponse)
	if !ok {
		return nil, errors.NewInternal("vector.RebuildIndex", "invalid response type")
	}

	return rebuildResp, nil
}

// GetIndexProgress gets the progress of an index operation.
func (c *client) GetIndexProgress(ctx context.Context, req *GetIndexProgressRequest) (*GetIndexProgressResponse, error) {
	if err := validateGetIndexProgressRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.GetIndexProgress", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetIndexProgress", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("vector.GetIndexProgress", "nil response from server")
	}

	progressResp, ok := resp.(*GetIndexProgressResponse)
	if !ok {
		return nil, errors.NewInternal("vector.GetIndexProgress", "invalid response type")
	}

	return progressResp, nil
}

// =============================================================================
// Admin Operations
// =============================================================================

// Export exports vectors from a namespace.
func (c *client) Export(ctx context.Context, req *ExportRequest) ([]*ExportResponse, error) {
	if err := validateExportRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.Export", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Export", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return []*ExportResponse{}, nil
	}

	// Handle both single response and slice
	if exportResp, ok := resp.(*ExportResponse); ok {
		return []*ExportResponse{exportResp}, nil
	}

	exportResponses, ok := resp.([]*ExportResponse)
	if !ok {
		return nil, errors.NewInternal("vector.Export", "invalid response type")
	}

	return exportResponses, nil
}

// Import imports vectors into a namespace.
func (c *client) Import(ctx context.Context, batches []*ImportRequest) (*ImportResponse, error) {
	if len(batches) == 0 {
		return nil, errors.NewInvalidInput("vector.Import", "batches are required")
	}

	for i, batch := range batches {
		if err := validateImportRequest(batch); err != nil {
			return nil, errors.NewInvalidInput("vector.Import", "invalid batch").
				WithContext("index", i)
		}
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.Import", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Import", batches)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("vector.Import", "nil response from server")
	}

	importResp, ok := resp.(*ImportResponse)
	if !ok {
		return nil, errors.NewInternal("vector.Import", "invalid response type")
	}

	return importResp, nil
}

// Compact compacts a namespace to reclaim space.
func (c *client) Compact(ctx context.Context, req *CompactRequest) (*CompactResponse, error) {
	if err := validateCompactRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.Compact", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Compact", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("vector.Compact", "nil response from server")
	}

	compactResp, ok := resp.(*CompactResponse)
	if !ok {
		return nil, errors.NewInternal("vector.Compact", "invalid response type")
	}

	return compactResp, nil
}

// GetCompactionProgress gets the progress of a compaction operation.
func (c *client) GetCompactionProgress(ctx context.Context, req *GetCompactionProgressRequest) (*GetCompactionProgressResponse, error) {
	if err := validateGetCompactionProgressRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.GetCompactionProgress", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetCompactionProgress", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("vector.GetCompactionProgress", "nil response from server")
	}

	progressResp, ok := resp.(*GetCompactionProgressResponse)
	if !ok {
		return nil, errors.NewInternal("vector.GetCompactionProgress", "invalid response type")
	}

	return progressResp, nil
}

// GetStats gets statistics for a namespace.
func (c *client) GetStats(ctx context.Context, req *GetStatsRequest) (*GetStatsResponse, error) {
	if err := validateGetStatsRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("vector.GetStats", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetStats", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("vector.GetStats", "nil response from server")
	}

	statsResp, ok := resp.(*GetStatsResponse)
	if !ok {
		return nil, errors.NewInternal("vector.GetStats", "invalid response type")
	}

	return statsResp, nil
}

// =============================================================================
// Validation Functions
// =============================================================================

func validateCreateNamespaceRequest(req *CreateNamespaceRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.CreateNamespace", "request is required")
	}
	if req.Config == nil {
		return errors.NewInvalidInput("vector.CreateNamespace", "config is required")
	}
	if req.Config.Namespace == "" {
		return errors.NewInvalidInput("vector.CreateNamespace", "namespace name is required")
	}
	if req.Config.Dimensions <= 0 {
		return errors.NewInvalidInput("vector.CreateNamespace", "dimensions must be positive")
	}
	return nil
}

func validateDeleteNamespaceRequest(req *DeleteNamespaceRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.DeleteNamespace", "request is required")
	}
	if req.Namespace == "" {
		return errors.NewInvalidInput("vector.DeleteNamespace", "namespace is required")
	}
	return nil
}

func validateGetNamespaceInfoRequest(req *GetNamespaceInfoRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.GetNamespaceInfo", "request is required")
	}
	if req.Namespace == "" {
		return errors.NewInvalidInput("vector.GetNamespaceInfo", "namespace is required")
	}
	return nil
}

func validateUpsertRequest(req *UpsertRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.Upsert", "request is required")
	}
	if req.Vector == nil {
		return errors.NewInvalidInput("vector.Upsert", "vector is required")
	}
	if req.Vector.ID == "" {
		return errors.NewInvalidInput("vector.Upsert", "vector ID is required")
	}
	if req.Vector.Namespace == "" {
		return errors.NewInvalidInput("vector.Upsert", "namespace is required")
	}
	if len(req.Vector.Vector) == 0 {
		return errors.NewInvalidInput("vector.Upsert", "vector data is required")
	}
	return nil
}

func validateBatchUpsertRequest(req *BatchUpsertRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.BatchUpsert", "request is required")
	}
	if len(req.Vectors) == 0 {
		return errors.NewInvalidInput("vector.BatchUpsert", "vectors are required")
	}
	return nil
}

func validateGetRequest(req *GetRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.Get", "request is required")
	}
	if req.Namespace == "" {
		return errors.NewInvalidInput("vector.Get", "namespace is required")
	}
	if req.ID == "" {
		return errors.NewInvalidInput("vector.Get", "ID is required")
	}
	return nil
}

func validateDeleteRequest(req *DeleteRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.Delete", "request is required")
	}
	if req.Namespace == "" {
		return errors.NewInvalidInput("vector.Delete", "namespace is required")
	}
	if req.ID == "" {
		return errors.NewInvalidInput("vector.Delete", "ID is required")
	}
	return nil
}

func validateBatchDeleteRequest(req *BatchDeleteRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.BatchDelete", "request is required")
	}
	if req.Namespace == "" {
		return errors.NewInvalidInput("vector.BatchDelete", "namespace is required")
	}
	if len(req.IDs) == 0 {
		return errors.NewInvalidInput("vector.BatchDelete", "IDs are required")
	}
	return nil
}

func validateListVectorsRequest(req *ListVectorsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.List", "request is required")
	}
	if req.Namespace == "" {
		return errors.NewInvalidInput("vector.List", "namespace is required")
	}
	return nil
}

func validateUpdateMetadataRequest(req *UpdateMetadataRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.UpdateMetadata", "request is required")
	}
	if req.Namespace == "" {
		return errors.NewInvalidInput("vector.UpdateMetadata", "namespace is required")
	}
	if req.ID == "" {
		return errors.NewInvalidInput("vector.UpdateMetadata", "ID is required")
	}
	if len(req.Metadata) == 0 {
		return errors.NewInvalidInput("vector.UpdateMetadata", "metadata is required")
	}
	return nil
}

func validateSearchSimilarRequest(req *SearchSimilarRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.SearchSimilar", "request is required")
	}
	if req.Namespace == "" {
		return errors.NewInvalidInput("vector.SearchSimilar", "namespace is required")
	}
	if len(req.QueryVector) == 0 {
		return errors.NewInvalidInput("vector.SearchSimilar", "query vector is required")
	}
	if req.K < 1 {
		return errors.NewInvalidInput("vector.SearchSimilar", "K must be at least 1")
	}
	return nil
}

func validateSearchByIDRequest(req *SearchByIDRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.SearchByID", "request is required")
	}
	if req.Namespace == "" {
		return errors.NewInvalidInput("vector.SearchByID", "namespace is required")
	}
	if req.ID == "" {
		return errors.NewInvalidInput("vector.SearchByID", "ID is required")
	}
	if req.K < 1 {
		return errors.NewInvalidInput("vector.SearchByID", "K must be at least 1")
	}
	return nil
}

func validateHybridSearchRequest(req *HybridSearchRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.HybridSearch", "request is required")
	}
	if req.Namespace == "" {
		return errors.NewInvalidInput("vector.HybridSearch", "namespace is required")
	}
	if len(req.QueryVector) == 0 {
		return errors.NewInvalidInput("vector.HybridSearch", "query vector is required")
	}
	if req.K < 1 {
		return errors.NewInvalidInput("vector.HybridSearch", "K must be at least 1")
	}
	return nil
}

func validateSearchByMetadataRequest(req *SearchByMetadataRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.SearchByMetadata", "request is required")
	}
	if req.Namespace == "" {
		return errors.NewInvalidInput("vector.SearchByMetadata", "namespace is required")
	}
	if len(req.MetadataFilters) == 0 {
		return errors.NewInvalidInput("vector.SearchByMetadata", "metadata filters are required")
	}
	return nil
}

func validateRebuildIndexRequest(req *RebuildIndexRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.RebuildIndex", "request is required")
	}
	if req.Namespace == "" {
		return errors.NewInvalidInput("vector.RebuildIndex", "namespace is required")
	}
	return nil
}

func validateGetIndexProgressRequest(req *GetIndexProgressRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.GetIndexProgress", "request is required")
	}
	if req.OperationID == "" && req.Namespace == "" {
		return errors.NewInvalidInput("vector.GetIndexProgress", "operation ID or namespace is required")
	}
	return nil
}

func validateExportRequest(req *ExportRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.Export", "request is required")
	}
	if req.Namespace == "" {
		return errors.NewInvalidInput("vector.Export", "namespace is required")
	}
	return nil
}

func validateImportRequest(req *ImportRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.Import", "request is required")
	}
	if req.Namespace == "" {
		return errors.NewInvalidInput("vector.Import", "namespace is required")
	}
	if len(req.BatchData) == 0 {
		return errors.NewInvalidInput("vector.Import", "batch data is required")
	}
	return nil
}

func validateCompactRequest(req *CompactRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.Compact", "request is required")
	}
	if req.Namespace == "" {
		return errors.NewInvalidInput("vector.Compact", "namespace is required")
	}
	return nil
}

func validateGetCompactionProgressRequest(req *GetCompactionProgressRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.GetCompactionProgress", "request is required")
	}
	if req.OperationID == "" && req.Namespace == "" {
		return errors.NewInvalidInput("vector.GetCompactionProgress", "operation ID or namespace is required")
	}
	return nil
}

func validateGetStatsRequest(req *GetStatsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("vector.GetStats", "request is required")
	}
	if req.Namespace == "" {
		return errors.NewInvalidInput("vector.GetStats", "namespace is required")
	}
	return nil
}
