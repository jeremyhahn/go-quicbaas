//go:build rest

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

package rest

import (
	"time"
)

// ========================================
// Vector Request/Response Types
// ========================================

// VectorHealthResponse represents the health status of the vector database.
type VectorHealthResponse struct {
	Status              string                     `json:"status"`
	NodeID              uint64                     `json:"node_id"`
	Version             string                     `json:"version"`
	UptimeSeconds       int64                      `json:"uptime_seconds"`
	NamespacesHealthy   int                        `json:"namespaces_healthy"`
	NamespacesDegraded  int                        `json:"namespaces_degraded"`
	NamespacesUnhealthy int                        `json:"namespaces_unhealthy"`
	Details             map[string]ComponentHealth `json:"details,omitempty"`
}

// ComponentHealth represents the health status of a component.
type ComponentHealth struct {
	Status    string                 `json:"status"`
	Message   string                 `json:"message,omitempty"`
	LastCheck string                 `json:"last_check,omitempty"`
	Metrics   map[string]interface{} `json:"metrics,omitempty"`
}

// VectorStatsResponse represents vector database statistics.
type VectorStatsResponse struct {
	TotalVectors       int64                `json:"total_vectors"`
	TotalNamespaces    int                  `json:"total_namespaces"`
	TotalStorageBytes  int64                `json:"total_storage_bytes"`
	TotalIndexBytes    int64                `json:"total_index_bytes"`
	AvgDimension       float64              `json:"avg_dimension"`
	SearchQPS          float64              `json:"search_qps"`
	AvgSearchLatencyMs float64              `json:"avg_search_latency_ms"`
	P95SearchLatencyMs float64              `json:"p95_search_latency_ms"`
	P99SearchLatencyMs float64              `json:"p99_search_latency_ms"`
	NamespaceStats     []NamespaceStatsItem `json:"namespace_stats,omitempty"`
}

// NamespaceStatsItem represents statistics for a single namespace.
type NamespaceStatsItem struct {
	Namespace          string  `json:"namespace"`
	VectorCount        int64   `json:"vector_count"`
	IndexedCount       int64   `json:"indexed_count"`
	StorageBytes       int64   `json:"storage_bytes"`
	IndexBytes         int64   `json:"index_bytes"`
	AvgSearchLatencyMs float64 `json:"avg_search_latency_ms"`
	SearchCount        int64   `json:"search_count"`
}

// UpsertVectorRequest represents a request to create or update a vector.
type UpsertVectorRequest struct {
	ID              string            `json:"id"`
	Vector          []float32         `json:"vector"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	SkipIndexUpdate bool              `json:"skip_index_update,omitempty"`
	TimeoutMs       int64             `json:"timeout_ms,omitempty"`
	ShardID         uint64            `json:"shard_id,omitempty"`
}

// UpsertVectorResponse represents the response from upserting a vector.
type UpsertVectorResponse struct {
	ID           string    `json:"id"`
	Version      int64     `json:"version"`
	Timestamp    time.Time `json:"timestamp"`
	LeaderID     uint64    `json:"leader_id,omitempty"`
	IndexUpdated bool      `json:"index_updated"`
}

// GetVectorResponse represents a vector entity.
type GetVectorResponse struct {
	ID        string            `json:"id"`
	Namespace string            `json:"namespace"`
	Vector    []float32         `json:"vector,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Version   int64             `json:"version"`
	CreatedAt time.Time         `json:"created_at,omitempty"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
	Status    string            `json:"status,omitempty"`
	ShardID   uint64            `json:"shard_id,omitempty"`
}

// DeleteVectorResponse represents the response from deleting a vector.
type DeleteVectorResponse struct {
	Deleted   bool      `json:"deleted"`
	Timestamp time.Time `json:"timestamp"`
}

// BatchUpsertVectorsRequest represents a batch upsert request.
type BatchUpsertVectorsRequest struct {
	Vectors         []VectorEntity `json:"vectors"`
	BatchSize       int            `json:"batch_size,omitempty"`
	SkipIndexUpdate bool           `json:"skip_index_update,omitempty"`
	TimeoutMs       int64          `json:"timeout_ms,omitempty"`
}

// VectorEntity represents a single vector with metadata.
type VectorEntity struct {
	ID       string            `json:"id"`
	Vector   []float32         `json:"vector"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// BatchUpsertVectorsResponse represents the response from batch upsert.
type BatchUpsertVectorsResponse struct {
	VectorsProcessed int64     `json:"vectors_processed"`
	VectorsSucceeded int64     `json:"vectors_succeeded"`
	VectorsFailed    int64     `json:"vectors_failed"`
	FailedIDs        []string  `json:"failed_ids,omitempty"`
	Timestamp        time.Time `json:"timestamp"`
}

// BatchDeleteVectorsRequest represents a batch delete request.
type BatchDeleteVectorsRequest struct {
	IDs       []string `json:"ids"`
	TimeoutMs int64    `json:"timeout_ms,omitempty"`
}

// BatchDeleteVectorsResponse represents the response from batch delete.
type BatchDeleteVectorsResponse struct {
	DeletedCount int64     `json:"deleted_count"`
	Timestamp    time.Time `json:"timestamp"`
}

// SearchSimilarRequest represents a k-NN similarity search request.
type SearchSimilarRequest struct {
	QueryVector     []float32         `json:"query_vector"`
	K               int               `json:"k"`
	DistanceMetric  string            `json:"distance_metric,omitempty"`
	MetadataFilters map[string]string `json:"metadata_filters,omitempty"`
	IncludeMetadata bool              `json:"include_metadata"`
	IncludeVectors  bool              `json:"include_vectors"`
	EfSearch        int               `json:"ef_search,omitempty"`
	MinScore        float32           `json:"min_score,omitempty"`
	TimeoutMs       int64             `json:"timeout_ms,omitempty"`
}

// SearchSimilarResponse represents the response from similarity search.
type SearchSimilarResponse struct {
	Results         []SimilarityResult `json:"results"`
	TotalCandidates int64              `json:"total_candidates"`
	SearchTimeMs    int64              `json:"search_time_ms"`
	NodeID          uint64             `json:"node_id"`
}

// SimilarityResult represents a single similarity search result.
type SimilarityResult struct {
	ID       string            `json:"id"`
	Vector   []float32         `json:"vector,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Score    float32           `json:"score"`
	Rank     int               `json:"rank"`
}

// SearchByIDRequest represents a search by existing vector ID request.
type SearchByIDRequest struct {
	K               int               `json:"k"`
	IncludeSelf     bool              `json:"include_self"`
	DistanceMetric  string            `json:"distance_metric,omitempty"`
	MetadataFilters map[string]string `json:"metadata_filters,omitempty"`
	IncludeMetadata bool              `json:"include_metadata"`
	IncludeVectors  bool              `json:"include_vectors"`
	EfSearch        int               `json:"ef_search,omitempty"`
	MinScore        float32           `json:"min_score,omitempty"`
	TimeoutMs       int64             `json:"timeout_ms,omitempty"`
}

// SearchByIDResponse represents the response from search by ID.
type SearchByIDResponse struct {
	Results         []SimilarityResult `json:"results"`
	QueryVector     *GetVectorResponse `json:"query_vector,omitempty"`
	TotalCandidates int64              `json:"total_candidates"`
	SearchTimeMs    int64              `json:"search_time_ms"`
	NodeID          uint64             `json:"node_id"`
}

// HybridSearchRequest represents a hybrid search request combining similarity and metadata filtering.
type HybridSearchRequest struct {
	Namespace          string            `json:"namespace"`
	QueryVector        []float32         `json:"query_vector"`
	K                  int               `json:"k"`
	MetadataFilters    map[string]string `json:"metadata_filters,omitempty"`
	FilterOperator     string            `json:"filter_operator,omitempty"`
	RerankingAlgorithm string            `json:"reranking_algorithm,omitempty"`
	IncludeMetadata    bool              `json:"include_metadata"`
	IncludeVectors     bool              `json:"include_vectors"`
	EfSearch           int               `json:"ef_search,omitempty"`
	MinScore           float32           `json:"min_score,omitempty"`
	TimeoutMs          int64             `json:"timeout_ms,omitempty"`
}

// HybridSearchResponse represents the response from hybrid search.
type HybridSearchResponse struct {
	Results         []SimilarityResult `json:"results"`
	TotalCandidates int64              `json:"total_candidates"`
	TotalFiltered   int                `json:"total_filtered"`
	SearchTimeMs    int64              `json:"search_time_ms"`
	NodeID          uint64             `json:"node_id"`
}

// SearchByMetadataRequest represents a metadata-based search request.
type SearchByMetadataRequest struct {
	Namespace       string            `json:"namespace"`
	Filters         map[string]string `json:"filters"`
	FilterOperator  string            `json:"filter_operator,omitempty"`
	Limit           int               `json:"limit,omitempty"`
	Offset          int               `json:"offset,omitempty"`
	IncludeVectors  bool              `json:"include_vectors"`
	IncludeMetadata bool              `json:"include_metadata"`
	TimeoutMs       int64             `json:"timeout_ms,omitempty"`
}

// SearchByMetadataResponse represents the response from metadata-based search.
type SearchByMetadataResponse struct {
	Results    []MetadataSearchResult `json:"results"`
	TotalCount int64                  `json:"total_count"`
	NextCursor string                 `json:"next_cursor,omitempty"`
}

// MetadataSearchResult represents a single metadata search result.
type MetadataSearchResult struct {
	ID        string            `json:"id"`
	Vector    []float32         `json:"vector,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Version   int64             `json:"version"`
	CreatedAt time.Time         `json:"created_at,omitempty"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
}

// ListVectorsResponse represents paginated vector list.
type ListVectorsResponse struct {
	Vectors    []VectorListItem `json:"vectors"`
	Pagination PaginationInfo   `json:"pagination"`
}

// VectorListItem represents a vector in a list response.
type VectorListItem struct {
	ID        string            `json:"id"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Version   int64             `json:"version"`
	CreatedAt time.Time         `json:"created_at,omitempty"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
}

// PaginationInfo represents pagination metadata.
type PaginationInfo struct {
	NextPageToken string `json:"next_page_token"`
	TotalCount    int64  `json:"total_count,omitempty"`
}

// ========================================
// Namespace Management Types
// ========================================

// CreateNamespaceRequest represents a request to create a namespace.
type CreateNamespaceRequest struct {
	Namespace          string            `json:"namespace"`
	Dimensions         int               `json:"dimensions"`
	DistanceMetric     string            `json:"distance_metric"`
	Precision          string            `json:"precision,omitempty"`
	IndexType          string            `json:"index_type,omitempty"`
	IndexParams        map[string]string `json:"index_params,omitempty"`
	ShardID            uint64            `json:"shard_id,omitempty"`
	ReplicationFactor  int               `json:"replication_factor,omitempty"`
	CompressionEnabled bool              `json:"compression_enabled,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
	TimeoutMs          int64             `json:"timeout_ms,omitempty"`
}

// CreateNamespaceResponse represents the response from creating a namespace.
type CreateNamespaceResponse struct {
	Namespace string    `json:"namespace"`
	Timestamp time.Time `json:"timestamp"`
}

// UpdateNamespaceRequest represents a request to update namespace configuration.
type UpdateNamespaceRequest struct {
	IndexParams       map[string]string `json:"index_params,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	ReplicationFactor int               `json:"replication_factor,omitempty"`
	TimeoutMs         int64             `json:"timeout_ms,omitempty"`
}

// UpdateNamespaceResponse represents the response from updating a namespace.
type UpdateNamespaceResponse struct {
	Namespace       string    `json:"namespace"`
	Timestamp       time.Time `json:"timestamp"`
	RequiresRebuild bool      `json:"requires_rebuild"`
}

// DeleteNamespaceResponse represents the response from deleting a namespace.
type DeleteNamespaceResponse struct {
	Deleted        bool      `json:"deleted"`
	VectorsDeleted int64     `json:"vectors_deleted"`
	Timestamp      time.Time `json:"timestamp"`
}

// ListNamespacesResponse represents the response from listing namespaces.
type ListNamespacesResponse struct {
	Namespaces    []NamespaceInfo `json:"namespaces"`
	NextPageToken string          `json:"next_page_token,omitempty"`
	TotalCount    int             `json:"total_count"`
}

// NamespaceInfo represents information about a namespace.
type NamespaceInfo struct {
	Namespace          string            `json:"namespace"`
	Dimensions         int               `json:"dimensions"`
	DistanceMetric     string            `json:"distance_metric"`
	Precision          string            `json:"precision,omitempty"`
	IndexType          string            `json:"index_type"`
	IndexParams        map[string]string `json:"index_params,omitempty"`
	VectorCount        int64             `json:"vector_count"`
	IndexedCount       int64             `json:"indexed_count"`
	PendingCount       int64             `json:"pending_count,omitempty"`
	StorageBytes       int64             `json:"storage_bytes"`
	IndexBytes         int64             `json:"index_bytes"`
	CreatedAt          time.Time         `json:"created_at,omitempty"`
	UpdatedAt          time.Time         `json:"updated_at,omitempty"`
	LastIndexBuild     time.Time         `json:"last_index_build,omitempty"`
	ShardID            uint64            `json:"shard_id,omitempty"`
	ReplicationFactor  int               `json:"replication_factor,omitempty"`
	CompressionEnabled bool              `json:"compression_enabled,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

// ========================================
// Admin Operation Types
// ========================================

// ClusterStatsResponse represents cluster-wide statistics.
type ClusterStatsResponse struct {
	ClusterID         string               `json:"cluster_id"`
	TotalNodes        int                  `json:"total_nodes"`
	TotalVectors      int64                `json:"total_vectors"`
	TotalNamespaces   int                  `json:"total_namespaces"`
	TotalStorageBytes int64                `json:"total_storage_bytes"`
	TotalIndexBytes   int64                `json:"total_index_bytes"`
	ClusterQPS        float64              `json:"cluster_qps"`
	NamespaceStats    []NamespaceStatsItem `json:"namespace_stats,omitempty"`
	NodeStats         []NodeStats          `json:"node_stats,omitempty"`
	Timestamp         time.Time            `json:"timestamp"`
}

// NodeStats represents statistics for a single node.
type NodeStats struct {
	NodeID           uint64  `json:"node_id"`
	Address          string  `json:"address"`
	VectorCount      int64   `json:"vector_count"`
	StorageBytes     int64   `json:"storage_bytes"`
	IndexBytes       int64   `json:"index_bytes"`
	QPS              float64 `json:"qps"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
	CPUUsage         float64 `json:"cpu_usage"`
	MemoryUsageBytes int64   `json:"memory_usage_bytes"`
}

// RebuildIndexRequest represents a request to rebuild an index.
type RebuildIndexRequest struct {
	IndexParams map[string]string `json:"index_params,omitempty"`
	Async       bool              `json:"async"`
	TimeoutMs   int64             `json:"timeout_ms,omitempty"`
}

// RebuildIndexResponse represents the response from rebuilding an index.
type RebuildIndexResponse struct {
	OperationID         string    `json:"operation_id"`
	Started             time.Time `json:"started"`
	EstimatedDurationMs int64     `json:"estimated_duration_ms"`
}

// IndexProgressResponse represents the progress of an index rebuild.
type IndexProgressResponse struct {
	OperationID          string  `json:"operation_id"`
	Namespace            string  `json:"namespace"`
	VectorsIndexed       int64   `json:"vectors_indexed"`
	TotalVectors         int64   `json:"total_vectors"`
	PercentComplete      float64 `json:"percent_complete"`
	ElapsedTimeMs        int64   `json:"elapsed_time_ms"`
	EstimatedRemainingMs int64   `json:"estimated_remaining_ms"`
	Status               string  `json:"status"`
	ErrorMessage         string  `json:"error_message,omitempty"`
}

// CompactNamespaceResponse represents the response from compacting a namespace.
type CompactNamespaceResponse struct {
	OperationID string    `json:"operation_id"`
	Started     time.Time `json:"started"`
}

// CompactionProgressResponse represents the progress of a compaction.
type CompactionProgressResponse struct {
	OperationID         string  `json:"operation_id"`
	Namespace           string  `json:"namespace"`
	BytesCompacted      int64   `json:"bytes_compacted"`
	TotalBytes          int64   `json:"total_bytes"`
	PercentComplete     float64 `json:"percent_complete"`
	ElapsedTimeMs       int64   `json:"elapsed_time_ms"`
	SpaceReclaimedBytes int64   `json:"space_reclaimed_bytes"`
	Status              string  `json:"status"`
}

// ExportVectorsRequest represents a request to export vectors.
type ExportVectorsRequest struct {
	Format          string            `json:"format"`
	Compression     string            `json:"compression"`
	BatchSize       int               `json:"batch_size"`
	MetadataFilters map[string]string `json:"metadata_filters,omitempty"`
}

// ImportVectorsResponse represents the response from importing vectors.
type ImportVectorsResponse struct {
	VectorsImported int64     `json:"vectors_imported"`
	VectorsSkipped  int64     `json:"vectors_skipped"`
	VectorsFailed   int64     `json:"vectors_failed"`
	DurationMs      int64     `json:"duration_ms"`
	Timestamp       time.Time `json:"timestamp"`
}

// ========================================
// Error Response Types
// ========================================

// VectorErrorResponse represents an error response.
type VectorErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail represents error details.
type ErrorDetail struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}
