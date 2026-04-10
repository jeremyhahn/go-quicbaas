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

//go:build integration

package vector

import "time"

// DistanceMetric defines the distance calculation method for vector similarity.
type DistanceMetric string

const (
	DistanceMetricEuclidean  DistanceMetric = "euclidean"
	DistanceMetricCosine     DistanceMetric = "cosine"
	DistanceMetricDotProduct DistanceMetric = "dot_product"
)

// IndexType defines the type of index used for vector storage.
type IndexType string

const (
	IndexTypeHNSW IndexType = "hnsw"
	IndexTypeFlat IndexType = "flat"
)

// ConsistencyLevel defines the consistency level for vector operations.
type ConsistencyLevel string

const (
	ConsistencyLevelOne    ConsistencyLevel = "one"
	ConsistencyLevelQuorum ConsistencyLevel = "quorum"
	ConsistencyLevelAll    ConsistencyLevel = "all"
)

// NamespaceConfig defines the configuration for a vector namespace.
type NamespaceConfig struct {
	Namespace      string
	Dimensions     int32
	DistanceMetric DistanceMetric
	IndexType      IndexType
	// HNSW parameters
	M              int32
	EfConstruction int32
}

// NamespaceInfo contains information about a vector namespace.
type NamespaceInfo struct {
	Namespace      string
	Dimensions     int32
	DistanceMetric DistanceMetric
	IndexType      IndexType
	VectorCount    int64
	Created        time.Time
	Updated        time.Time
}

// VectorEntity represents a vector with its metadata.
type VectorEntity struct {
	ID        string
	Namespace string
	Vector    []float32
	Metadata  map[string]string
	Created   time.Time
	Updated   time.Time
}

// UpsertRequest represents a request to upsert a vector.
type UpsertRequest struct {
	Namespace   string
	ID          string
	Vector      []float32
	Metadata    map[string]string
	Consistency ConsistencyLevel
}

// UpsertResponse represents the response from an upsert operation.
type UpsertResponse struct {
	ID      string
	Created bool
	Updated bool
}

// GetVectorRequest represents a request to get a vector.
type GetVectorRequest struct {
	Namespace       string
	ID              string
	IncludeVector   bool
	IncludeMetadata bool
	Consistency     ConsistencyLevel
}

// DeleteVectorRequest represents a request to delete a vector.
type DeleteVectorRequest struct {
	Namespace   string
	ID          string
	Consistency ConsistencyLevel
}

// SearchRequest represents a request to search for similar vectors.
type SearchRequest struct {
	Namespace       string
	QueryVector     []float32
	K               int32
	IncludeVectors  bool
	IncludeMetadata bool
	MetadataFilters map[string]string
	Consistency     ConsistencyLevel
	EfSearch        int32
}

// SimilarityResult represents a single search result.
type SimilarityResult struct {
	ID       string
	Score    float32
	Distance float32
	Vector   []float32
	Metadata map[string]string
}

// HybridSearchRequest represents a request for hybrid search (vector + metadata).
type HybridSearchRequest struct {
	Namespace       string
	QueryVector     []float32
	K               int32
	MetadataFilters map[string]string
	IncludeVectors  bool
	IncludeMetadata bool
	Consistency     ConsistencyLevel
	// Weight for vector similarity vs metadata match (0.0 to 1.0)
	VectorWeight float32
}

// BatchUpsertRequest represents a request to upsert multiple vectors.
type BatchUpsertRequest struct {
	Namespace   string
	Vectors     []*VectorEntity
	Consistency ConsistencyLevel
}

// BatchUpsertResponse represents the response from a batch upsert operation.
type BatchUpsertResponse struct {
	Succeeded int32
	Failed    int32
	Errors    []string
}

// IndexStats contains statistics about a namespace index.
type IndexStats struct {
	Namespace       string
	VectorCount     int64
	IndexSize       int64
	IndexType       IndexType
	IsOptimized     bool
	LastRebuildTime *time.Time
	// HNSW specific stats
	M              int32
	EfConstruction int32
	MaxLevel       int32
}

// RebuildIndexRequest represents a request to rebuild a namespace index.
type RebuildIndexRequest struct {
	Namespace string
	Async     bool
}

// RebuildIndexResponse represents the response from a rebuild operation.
type RebuildIndexResponse struct {
	TaskID    string
	Status    string
	StartTime time.Time
}

// ListVectorsRequest represents a request to list vectors in a namespace.
type ListVectorsRequest struct {
	Namespace       string
	PageSize        int32
	PageToken       string
	IncludeVectors  bool
	IncludeMetadata bool
	Consistency     ConsistencyLevel
}

// ListVectorsResponse represents the response from a list vectors operation.
type ListVectorsResponse struct {
	Vectors       []*VectorEntity
	NextPageToken string
	TotalCount    int64
}
