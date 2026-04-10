//go:build mcp

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

// Package mcp implements Vector Database tool definitions for the Model Context Protocol.
package mcp

// Vector Database tool definitions for all vector operations (22 tools).
// Each tool follows the MCP specification with JSON schema validation.

var (

	// UpsertVectorTool creates or updates a single vector.
	UpsertVectorTool = Tool{
		Name:        "quicbaas_vector_upsert",
		Description: "Insert or update a vector in a namespace with optional metadata",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Vector namespace identifier (must exist)",
				},
				"id": {
					Type:        "string",
					Description: "Unique vector identifier (UUID, hash, or custom ID)",
				},
				"vector": {
					Type:        "string",
					Description: "JSON array of float values representing the embedding",
				},
				"metadata": {
					Type:        "string",
					Description: "JSON object with key-value metadata for filtering (optional)",
				},
				"consistency": {
					Type:        "string",
					Enum:        []string{"ALL", "QUORUM", "ONE"},
					Description: "Write consistency level (default: QUORUM)",
				},
				"skip_index_update": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Defer index updates for bulk operations (default: false)",
				},
				"timeout_ms": {
					Type:        "integer",
					Description: "Operation timeout in milliseconds (default: 5000)",
				},
			},
			Required: []string{"namespace", "id", "vector"},
		},
	}

	// BatchUpsertVectorsTool creates or updates multiple vectors atomically.
	BatchUpsertVectorsTool = Tool{
		Name:        "quicbaas_vector_batch_upsert",
		Description: "Batch upsert multiple vectors atomically - all succeed or all fail",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Vector namespace identifier",
				},
				"vectors": {
					Type:        "string",
					Description: "JSON array of vector objects with id, vector, and optional metadata fields",
				},
				"consistency": {
					Type:        "string",
					Enum:        []string{"ALL", "QUORUM", "ONE"},
					Description: "Write consistency level (default: QUORUM)",
				},
				"skip_index_update": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Defer index updates (default: false)",
				},
				"batch_size": {
					Type:        "integer",
					Description: "Vectors per internal batch (default: 1000, max: 10000)",
				},
				"timeout_ms": {
					Type:        "integer",
					Description: "Operation timeout in milliseconds (default: 60000)",
				},
			},
			Required: []string{"namespace", "vectors"},
		},
	}

	// GetVectorTool retrieves a vector by ID.
	GetVectorTool = Tool{
		Name:        "quicbaas_vector_get",
		Description: "Retrieve a vector by ID with optional metadata and vector data",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Vector namespace identifier",
				},
				"id": {
					Type:        "string",
					Description: "Unique vector identifier",
				},
				"consistency": {
					Type:        "string",
					Enum:        []string{"ALL", "QUORUM", "ONE"},
					Description: "Read consistency level (default: QUORUM)",
				},
				"include_metadata": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Include metadata in response (default: true)",
				},
				"include_vector": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Include vector data in response (default: true)",
				},
				"timeout_ms": {
					Type:        "integer",
					Description: "Operation timeout in milliseconds (default: 5000)",
				},
			},
			Required: []string{"namespace", "id"},
		},
	}

	// DeleteVectorTool removes a vector by ID.
	DeleteVectorTool = Tool{
		Name:        "quicbaas_vector_delete",
		Description: "Delete a vector by ID (idempotent operation)",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Vector namespace identifier",
				},
				"id": {
					Type:        "string",
					Description: "Unique vector identifier to delete",
				},
				"consistency": {
					Type:        "string",
					Enum:        []string{"ALL", "QUORUM", "ONE"},
					Description: "Delete consistency level (default: QUORUM)",
				},
				"version": {
					Type:        "integer",
					Description: "Expected version for optimistic locking (0 = no check)",
				},
				"timeout_ms": {
					Type:        "integer",
					Description: "Operation timeout in milliseconds (default: 5000)",
				},
			},
			Required: []string{"namespace", "id"},
		},
	}

	// UpdateMetadataTool updates vector metadata.
	UpdateMetadataTool = Tool{
		Name:        "quicbaas_vector_update_metadata",
		Description: "Update metadata for an existing vector",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Vector namespace identifier",
				},
				"id": {
					Type:        "string",
					Description: "Unique vector identifier",
				},
				"metadata": {
					Type:        "object",
					Description: "Metadata key-value pairs to set",
				},
				"merge": {
					Type:        "boolean",
					Description: "Merge with existing metadata (default: true)",
				},
				"timeout_ms": {
					Type:        "integer",
					Description: "Operation timeout in milliseconds (default: 5000)",
				},
			},
			Required: []string{"namespace", "id", "metadata"},
		},
	}

	// BatchDeleteVectorsTool removes multiple vectors atomically.
	BatchDeleteVectorsTool = Tool{
		Name:        "quicbaas_vector_batch_delete",
		Description: "Batch delete multiple vectors atomically - all succeed or all fail",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Vector namespace identifier",
				},
				"ids": {
					Type:        "string",
					Description: "JSON array of vector IDs to delete (max: 10000)",
				},
				"consistency": {
					Type:        "string",
					Enum:        []string{"ALL", "QUORUM", "ONE"},
					Description: "Delete consistency level (default: QUORUM)",
				},
				"timeout_ms": {
					Type:        "integer",
					Description: "Operation timeout in milliseconds (default: 60000)",
				},
			},
			Required: []string{"namespace", "ids"},
		},
	}

	// SearchSimilarTool performs k-NN similarity search.
	SearchSimilarTool = Tool{
		Name:        "quicbaas_vector_search_similar",
		Description: "Perform k-NN similarity search to find nearest neighbors",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Vector namespace to search",
				},
				"query_vector": {
					Type:        "string",
					Description: "JSON array of float values representing the query embedding",
				},
				"k": {
					Type:        "integer",
					Description: "Number of nearest neighbors to return (1-10000)",
				},
				"distance_metric": {
					Type:        "string",
					Enum:        []string{"L2", "COSINE", "DOT_PRODUCT", "MANHATTAN"},
					Description: "Distance/similarity metric (overrides namespace default)",
				},
				"metadata_filters": {
					Type:        "string",
					Description: "JSON array of metadata filter objects with key, operator, and value fields",
				},
				"consistency": {
					Type:        "string",
					Enum:        []string{"ALL", "QUORUM", "ONE"},
					Description: "Read consistency level (default: QUORUM)",
				},
				"include_metadata": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Include metadata in results (default: true)",
				},
				"include_vectors": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Include vector data in results (default: false)",
				},
				"ef_search": {
					Type:        "integer",
					Description: "HNSW efSearch parameter for recall tuning (1-1000)",
				},
				"min_score": {
					Type:        "number",
					Description: "Minimum similarity threshold to filter results",
				},
				"timeout_ms": {
					Type:        "integer",
					Description: "Operation timeout in milliseconds (default: 10000)",
				},
			},
			Required: []string{"namespace", "query_vector", "k"},
		},
	}

	// SearchByIDTool performs k-NN search using an existing vector.
	SearchByIDTool = Tool{
		Name:        "quicbaas_vector_search_by_id",
		Description: "Perform k-NN search using an existing vector as the query",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Vector namespace to search",
				},
				"id": {
					Type:        "string",
					Description: "Vector ID to use as the query",
				},
				"k": {
					Type:        "integer",
					Description: "Number of nearest neighbors to return (1-10000)",
				},
				"include_self": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Include the query vector in results (default: false)",
				},
				"distance_metric": {
					Type:        "string",
					Enum:        []string{"L2", "COSINE", "DOT_PRODUCT", "MANHATTAN"},
					Description: "Distance/similarity metric (overrides namespace default)",
				},
				"metadata_filters": {
					Type:        "string",
					Description: "JSON array of metadata filter objects",
				},
				"consistency": {
					Type:        "string",
					Enum:        []string{"ALL", "QUORUM", "ONE"},
					Description: "Read consistency level (default: QUORUM)",
				},
				"include_metadata": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Include metadata in results (default: true)",
				},
				"include_vectors": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Include vector data in results (default: false)",
				},
				"ef_search": {
					Type:        "integer",
					Description: "HNSW efSearch parameter (1-1000)",
				},
				"min_score": {
					Type:        "number",
					Description: "Minimum similarity threshold",
				},
				"timeout_ms": {
					Type:        "integer",
					Description: "Operation timeout in milliseconds (default: 10000)",
				},
			},
			Required: []string{"namespace", "id", "k"},
		},
	}

	// ListVectorsTool returns all vectors in a namespace.
	ListVectorsTool = Tool{
		Name:        "quicbaas_vector_list",
		Description: "List all vectors in a namespace with pagination support",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Vector namespace identifier",
				},
				"page_size": {
					Type:        "integer",
					Description: "Maximum results per page (default: 100, max: 1000)",
				},
				"page_token": {
					Type:        "string",
					Description: "Continuation token from previous response",
				},
				"consistency": {
					Type:        "string",
					Enum:        []string{"ALL", "QUORUM", "ONE"},
					Description: "Read consistency level (default: QUORUM)",
				},
				"include_metadata": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Include metadata in results (default: true)",
				},
				"include_vectors": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Include vector data in results (default: false)",
				},
				"metadata_filters": {
					Type:        "string",
					Description: "JSON array of metadata filter objects",
				},
				"timeout_ms": {
					Type:        "integer",
					Description: "Operation timeout in milliseconds (default: 30000)",
				},
			},
			Required: []string{"namespace"},
		},
	}

	// ListNamespacesTool lists all vector namespaces.
	ListNamespacesTool = Tool{
		Name:        "quicbaas_vector_list_namespaces",
		Description: "List all available vector namespaces with metadata",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"prefix": {
					Type:        "string",
					Description: "Filter namespaces by prefix (optional)",
				},
				"page_size": {
					Type:        "integer",
					Description: "Maximum results per page (default: 100)",
				},
				"page_token": {
					Type:        "string",
					Description: "Continuation token from previous response",
				},
			},
			Required: []string{},
		},
	}

	// CreateNamespaceTool creates a new vector namespace.
	CreateNamespaceTool = Tool{
		Name:        "quicbaas_vector_create_namespace",
		Description: "Create a new vector namespace with specified configuration",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Unique namespace identifier (1-64 characters, alphanumeric + '-' + '_')",
				},
				"dimensions": {
					Type:        "integer",
					Description: "Vector dimensionality (1-4096, e.g., 384 for all-MiniLM, 1536 for OpenAI)",
				},
				"distance_metric": {
					Type:        "string",
					Enum:        []string{"L2", "COSINE", "DOT_PRODUCT", "MANHATTAN"},
					Description: "Similarity calculation method (cannot be changed after creation)",
				},
				"precision": {
					Type:        "string",
					Enum:        []string{"FLOAT32", "FLOAT16", "BFLOAT16"},
					Description: "Floating-point precision for storage (default: FLOAT32)",
				},
				"index_type": {
					Type:        "string",
					Enum:        []string{"FLAT", "HNSW", "IVF_FLAT", "IVF_PQ"},
					Description: "Indexing algorithm (default: HNSW)",
				},
				"index_params": {
					Type:        "string",
					Description: "JSON object with index-specific tuning parameters (e.g., {\"M\": 16, \"efConstruction\": 200})",
				},
				"shard_id": {
					Type:        "integer",
					Description: "Assign to specific shard (0 = auto-assign)",
				},
				"replication_factor": {
					Type:        "integer",
					Description: "Number of replicas to maintain (1 <= n <= cluster_size)",
				},
				"compression_enabled": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Enable vector compression (default: false)",
				},
				"metadata": {
					Type:        "string",
					Description: "JSON object with namespace-level metadata",
				},
				"timeout_ms": {
					Type:        "integer",
					Description: "Operation timeout in milliseconds (default: 10000)",
				},
			},
			Required: []string{"namespace", "dimensions", "distance_metric"},
		},
	}

	// UpdateNamespaceTool modifies namespace configuration.
	UpdateNamespaceTool = Tool{
		Name:        "quicbaas_vector_update_namespace",
		Description: "Update namespace configuration (index params, metadata, replication)",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Namespace identifier to update",
				},
				"index_params": {
					Type:        "string",
					Description: "JSON object with updated index parameters",
				},
				"metadata": {
					Type:        "string",
					Description: "JSON object with updated metadata (merged with existing)",
				},
				"replication_factor": {
					Type:        "integer",
					Description: "Updated replication factor (0 = no change)",
				},
				"timeout_ms": {
					Type:        "integer",
					Description: "Operation timeout in milliseconds (default: 10000)",
				},
			},
			Required: []string{"namespace"},
		},
	}

	// DeleteNamespaceTool removes a namespace.
	DeleteNamespaceTool = Tool{
		Name:        "quicbaas_vector_delete_namespace",
		Description: "Delete a namespace and all its vectors (DESTRUCTIVE - cannot be undone)",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Namespace identifier to delete",
				},
				"force": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Bypass safety checks and delete even if vectors exist (default: false)",
				},
				"timeout_ms": {
					Type:        "integer",
					Description: "Operation timeout in milliseconds (default: 60000)",
				},
			},
			Required: []string{"namespace"},
		},
	}

	// GetNamespaceInfoTool retrieves namespace information.
	GetNamespaceInfoTool = Tool{
		Name:        "quicbaas_vector_get_namespace_info",
		Description: "Get namespace configuration and statistics",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Namespace identifier",
				},
				"include_stats": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Include detailed statistics (default: true)",
				},
				"timeout_ms": {
					Type:        "integer",
					Description: "Operation timeout in milliseconds (default: 5000)",
				},
			},
			Required: []string{"namespace"},
		},
	}

	// GetVectorStatsTool returns vector database statistics.
	GetVectorStatsTool = Tool{
		Name:        "quicbaas_vector_get_stats",
		Description: "Get vector database statistics for a namespace or cluster-wide",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Filter statistics to specific namespace (optional, empty = cluster-wide)",
				},
				"include_details": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Include per-shard breakdown (default: false)",
				},
				"timeout_ms": {
					Type:        "integer",
					Description: "Operation timeout in milliseconds (default: 5000)",
				},
			},
			Required: []string{},
		},
	}

	// VectorHealthTool returns health status.
	VectorHealthTool = Tool{
		Name:        "quicbaas_vector_health",
		Description: "Get health status of the vector database",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"include_details": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Include component-specific health information (default: false)",
				},
			},
			Required: []string{},
		},
	}

	// RebuildIndexTool rebuilds the vector index.
	RebuildIndexTool = Tool{
		Name:        "quicbaas_vector_rebuild_index",
		Description: "Rebuild the vector index for a namespace to optimize search performance",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Namespace identifier to rebuild",
				},
				"index_params": {
					Type:        "string",
					Description: "JSON object with index parameters to override during rebuild",
				},
				"async": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Return immediately without waiting for completion (default: true)",
				},
				"timeout_ms": {
					Type:        "integer",
					Description: "Operation timeout in milliseconds (default: 300000, only if async=false)",
				},
			},
			Required: []string{"namespace"},
		},
	}

	// GetIndexProgressTool gets index rebuild progress.
	GetIndexProgressTool = Tool{
		Name:        "quicbaas_vector_get_index_progress",
		Description: "Get the progress of an ongoing index rebuild operation",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Namespace identifier",
				},
				"operation_id": {
					Type:        "string",
					Description: "Rebuild operation ID (optional, empty = current/latest operation)",
				},
			},
			Required: []string{"namespace"},
		},
	}

	// ----------------------------------------
	// GetVectorClusterStatsTool returns cluster-wide statistics.
	GetVectorClusterStatsTool = Tool{
		Name:        "quicbaas_vector_admin_get_cluster_stats",
		Description: "Get cluster-wide vector database statistics and metrics",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"include_namespaces": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Include per-namespace breakdown (default: false)",
				},
				"include_nodes": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Include per-node breakdown (default: false)",
				},
			},
			Required: []string{},
		},
	}

	// CompactNamespaceTool performs namespace compaction.
	CompactNamespaceTool = Tool{
		Name:        "quicbaas_vector_admin_compact_namespace",
		Description: "Perform compaction on a namespace to reclaim disk space",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Namespace identifier to compact",
				},
				"async": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Return immediately without waiting for completion (default: true)",
				},
				"timeout_ms": {
					Type:        "integer",
					Description: "Operation timeout in milliseconds (default: 300000)",
				},
			},
			Required: []string{"namespace"},
		},
	}

	// GetCompactionProgressTool gets compaction progress.
	GetCompactionProgressTool = Tool{
		Name:        "quicbaas_vector_admin_get_compaction_progress",
		Description: "Get the progress of an ongoing compaction operation",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Namespace identifier",
				},
				"operation_id": {
					Type:        "string",
					Description: "Compaction operation ID (optional, empty = current/latest operation)",
				},
			},
			Required: []string{"namespace"},
		},
	}

	// ExportVectorsTool exports vectors for backup.
	ExportVectorsTool = Tool{
		Name:        "quicbaas_vector_admin_export_vectors",
		Description: "Export vectors from a namespace for backup or migration",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Namespace identifier to export",
				},
				"format": {
					Type:        "string",
					Enum:        []string{"json", "parquet", "binary"},
					Description: "Export format (default: binary)",
				},
				"compression": {
					Type:        "string",
					Enum:        []string{"none", "gzip", "zstd"},
					Description: "Compression algorithm (default: gzip)",
				},
				"batch_size": {
					Type:        "integer",
					Description: "Vectors per batch (default: 1000)",
				},
				"metadata_filters": {
					Type:        "string",
					Description: "JSON array of metadata filter objects to filter export",
				},
			},
			Required: []string{"namespace"},
		},
	}

	// ImportVectorsTool imports vectors from backup.
	ImportVectorsTool = Tool{
		Name:        "quicbaas_vector_admin_import_vectors",
		Description: "Import vectors into a namespace from a backup or migration",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"namespace": {
					Type:        "string",
					Description: "Target namespace identifier",
				},
				"format": {
					Type:        "string",
					Enum:        []string{"json", "parquet", "binary"},
					Description: "Import format",
				},
				"data": {
					Type:        "string",
					Description: "Base64-encoded batch data or file path",
				},
				"skip_validation": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Bypass vector validation for faster import (default: false)",
				},
				"overwrite_existing": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Overwrite vectors with matching IDs (default: false)",
				},
			},
			Required: []string{"namespace", "format", "data"},
		},
	}
)

// getAllVectorTools returns all Vector Database tools (23 tools).
func getAllVectorTools() []Tool {
	return []Tool{
		// VectorDB Tools (18)
		UpsertVectorTool,
		BatchUpsertVectorsTool,
		GetVectorTool,
		DeleteVectorTool,
		UpdateMetadataTool,
		BatchDeleteVectorsTool,
		SearchSimilarTool,
		SearchByIDTool,
		ListVectorsTool,
		ListNamespacesTool,
		CreateNamespaceTool,
		UpdateNamespaceTool,
		DeleteNamespaceTool,
		GetNamespaceInfoTool,
		GetVectorStatsTool,
		VectorHealthTool,
		RebuildIndexTool,
		GetIndexProgressTool,

		// VectorAdmin Tools (5)
		GetVectorClusterStatsTool,
		CompactNamespaceTool,
		GetCompactionProgressTool,
		ExportVectorsTool,
		ImportVectorsTool,
	}
}
