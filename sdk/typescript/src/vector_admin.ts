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
// Vector admin domain types.
//
// Mirrors pkg/api/rest/vector_types.go and the five admin routes under
// /api/v1/vector/admin/* (pkg/api/rest/vector_handlers.go): cluster-wide
// stats, namespace compaction and its progress, and JSON Lines export and
// import of a namespace's vectors.
//
// Wire note: timestamps arrive as RFC 3339 strings.
// ---------------------------------------------------------------------------

/** NamespaceStatsItem is one namespace's row in the cluster stats. */
export interface NamespaceStatsItem {
  namespace: string;
  vector_count: number;
  indexed_count: number;
  storage_bytes: number;
  index_bytes: number;
  avg_search_latency_ms: number;
  search_count: number;
}

/** VectorNodeStats is one node's row in the cluster stats. */
export interface VectorNodeStats {
  node_id: string;
  address: string;
  vector_count: number;
  storage_bytes: number;
  index_bytes: number;
  qps: number;
  avg_latency_ms: number;
  cpu_usage: number;
  memory_usage_bytes: number;
}

/** ClusterStatsResponse is the body of GET /api/v1/vector/admin/stats/global. */
export interface ClusterStatsResponse {
  cluster_id: string;
  total_nodes: number;
  total_vectors: number;
  total_namespaces: number;
  total_storage_bytes: number;
  total_index_bytes: number;
  cluster_qps: number;
  namespace_stats?: NamespaceStatsItem[];
  node_stats?: VectorNodeStats[];
  timestamp: string;
}

/** CompactNamespaceResponse is the 202 body of POST /api/v1/vector/admin/compact. */
export interface CompactNamespaceResponse {
  operation_id: string;
  started: boolean;
}

/** CompactionProgress is the body of GET /api/v1/vector/admin/compact/progress. */
export interface CompactionProgress {
  operation_id: string;
  namespace: string;
  bytes_compacted: number;
  total_bytes: number;
  percent_complete: number;
  elapsed_time_ms: number;
  space_reclaimed_bytes: number;
  status: string;
}

/** ExportVectorsRequest is the body of POST /api/v1/vector/admin/export/{namespace}. */
export interface ExportVectorsRequest {
  format?: string;
  compression?: string;
  batch_size?: number;
  metadata_filters?: Record<string, string>;
}

/** VectorExportEntry is one JSON Lines record of an export or import. */
export interface VectorExportEntry {
  id: string;
  vector: number[];
  metadata?: Record<string, string>;
}

/** ImportVectorsResponse is the body of POST /api/v1/vector/admin/import/{namespace}. */
export interface ImportVectorsResponse {
  vectors_imported: number;
  vectors_skipped: number;
  vectors_failed: number;
  duration_ms: number;
  timestamp: string;
}

/** VectorAdminClient covers the /api/v1/vector/admin/* routes. */
export interface VectorAdminClient {
  /** Cluster-wide vector stats. */
  getGlobalStats(): Promise<ClusterStatsResponse>;
  /** Starts compacting a namespace. */
  compactNamespace(namespace: string): Promise<CompactNamespaceResponse>;
  /** Reads a compaction's progress. */
  getCompactionProgress(operationId: string): Promise<CompactionProgress>;
  /** Exports a namespace's vectors, parsed from the JSON Lines body. */
  exportVectors(namespace: string, req?: ExportVectorsRequest): Promise<VectorExportEntry[]>;
  /** Imports vectors into a namespace, sent as JSON Lines. */
  importVectors(namespace: string, entries: VectorExportEntry[]): Promise<ImportVectorsResponse>;
}
