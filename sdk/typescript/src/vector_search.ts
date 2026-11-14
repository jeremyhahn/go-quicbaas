// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// ---------------------------------------------------------------------------
// Vector health, cluster-stats, hybrid-search and metadata-search types.
//
// Mirrors pkg/api/rest/vector_handlers.go and vector_types.go
// (/api/v1/vector/health, /api/v1/vector/cluster/stats,
// /api/v1/vector/hybrid-search, /api/v1/vector/search-by-metadata).
//
// Wire note: every reply is 200 JSON, and times arrive as RFC 3339 strings.
// ---------------------------------------------------------------------------

/** VectorComponentHealth is the health of one vector-store component. */
export interface VectorComponentHealth {
  status: string;
  message?: string;
  last_check?: string;
  metrics?: Record<string, unknown>;
}

/** VectorHealthResponse reports the vector store's health. */
export interface VectorHealthResponse {
  status: string;
  node_id: number;
  version: string;
  uptime_seconds: number;
  namespaces_healthy: number;
  namespaces_degraded: number;
  namespaces_unhealthy: number;
  details?: Record<string, VectorComponentHealth>;
}

/** VectorNamespaceStatsItem is one namespace's statistics. */
export interface VectorNamespaceStatsItem {
  namespace: string;
  vector_count: number;
  indexed_count: number;
  storage_bytes: number;
  index_bytes: number;
  avg_search_latency_ms: number;
  search_count: number;
}

/** VectorClusterStatsResponse reports cluster-wide vector statistics. */
export interface VectorClusterStatsResponse {
  cluster_id: string;
  total_nodes: number;
  total_vectors: number;
  total_namespaces: number;
  total_storage_bytes: number;
  total_index_bytes: number;
  cluster_qps: number;
  namespace_stats?: VectorNamespaceStatsItem[];
  node_stats?: Record<string, unknown>[];
  timestamp: string;
}

/** VectorHybridSearchRequest is a similarity search narrowed by metadata filters. */
export interface VectorHybridSearchRequest {
  namespace: string;
  query_vector: number[];
  k: number;
  metadata_filters?: Record<string, string>;
  filter_operator?: string;
  reranking_algorithm?: string;
  include_metadata?: boolean;
  include_vectors?: boolean;
  ef_search?: number;
  min_score?: number;
  timeout_ms?: number;
}

/** VectorHybridSearchResult is one hit of a hybrid search. */
export interface VectorHybridSearchResult {
  id: string;
  vector?: number[];
  metadata?: Record<string, string>;
  score: number;
  rank: number;
}

/** VectorHybridSearchResponse carries the hits of a hybrid search. */
export interface VectorHybridSearchResponse {
  results: VectorHybridSearchResult[];
  total_candidates: number;
  total_filtered: number;
  search_time_ms: number;
  node_id: number;
}

/** VectorMetadataSearchRequest finds vectors by metadata alone. */
export interface VectorMetadataSearchRequest {
  namespace: string;
  filters: Record<string, string>;
  filter_operator?: string;
  limit?: number;
  offset?: number;
  include_vectors?: boolean;
  include_metadata?: boolean;
  timeout_ms?: number;
}

/** VectorMetadataSearchResult is one hit of a metadata search. */
export interface VectorMetadataSearchResult {
  id: string;
  vector?: number[];
  metadata?: Record<string, string>;
  version: number;
  created_at?: string;
  updated_at?: string;
}

/** VectorMetadataSearchResponse carries the hits of a metadata search. */
export interface VectorMetadataSearchResponse {
  results: VectorMetadataSearchResult[];
  total_count: number;
  next_cursor?: string;
}
