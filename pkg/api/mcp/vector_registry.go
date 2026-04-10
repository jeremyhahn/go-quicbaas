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

// Package mcp implements Vector Database integration with the ToolRegistry.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// registerVectorTools registers all Vector Database tools with the ToolRegistry.
func (r *ToolRegistry) registerVectorTools() {
	vectorTools := getAllVectorTools()
	for _, tool := range vectorTools {
		r.addTool(ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: convertInputSchema(tool.InputSchema),
		})
	}

	// Also register the simplified search alias for compatibility
	r.addTool(ToolDefinition{
		Name:        "quicbaas_vector_search",
		Description: "Perform k-NN similarity search to find nearest neighbors (alias for quicbaas_vector_search_similar)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"namespace": map[string]interface{}{
					"type":        "string",
					"description": "Vector namespace to search",
				},
				"vector": map[string]interface{}{
					"type":        "array",
					"description": "Query vector as array of float values",
				},
				"k": map[string]interface{}{
					"type":        "integer",
					"description": "Number of nearest neighbors to return (1-10000)",
				},
			},
			"required": []string{"namespace", "vector", "k"},
		},
	})
}

// isVectorTool returns true if the tool name is a vector database tool.
func isVectorTool(name string) bool {
	return strings.HasPrefix(name, "quicbaas_vector_")
}

// executeVectorTool executes a vector database tool and returns the result.
func (r *ToolRegistry) executeVectorTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	vectorService := r.service.Vector()
	if vectorService == nil {
		return nil, api.NewServiceUnavailableError("vector")
	}

	switch name {
	// Namespace operations
	case "quicbaas_vector_create_namespace":
		return r.handleVectorCreateNamespace(ctx, vectorService, args)
	case "quicbaas_vector_delete_namespace":
		return r.handleVectorDeleteNamespace(ctx, vectorService, args)
	case "quicbaas_vector_list_namespaces":
		return r.handleVectorListNamespaces(ctx, vectorService, args)
	case "quicbaas_vector_get_namespace_info":
		return r.handleVectorGetNamespaceInfo(ctx, vectorService, args)
	case "quicbaas_vector_update_namespace":
		return r.handleVectorUpdateNamespace(ctx, vectorService, args)

	// Vector CRUD operations
	case "quicbaas_vector_upsert":
		return r.handleVectorUpsert(ctx, vectorService, args)
	case "quicbaas_vector_batch_upsert":
		return r.handleVectorBatchUpsert(ctx, vectorService, args)
	case "quicbaas_vector_get":
		return r.handleVectorGet(ctx, vectorService, args)
	case "quicbaas_vector_delete":
		return r.handleVectorDelete(ctx, vectorService, args)
	case "quicbaas_vector_update_metadata":
		return r.handleVectorUpdateMetadata(ctx, vectorService, args)
	case "quicbaas_vector_batch_delete":
		return r.handleVectorBatchDelete(ctx, vectorService, args)
	case "quicbaas_vector_list":
		return r.handleVectorList(ctx, vectorService, args)

	// Search operations
	case "quicbaas_vector_search", "quicbaas_vector_search_similar":
		return r.handleVectorSearchSimilar(ctx, vectorService, args)
	case "quicbaas_vector_search_by_id":
		return r.handleVectorSearchByID(ctx, vectorService, args)

	// Stats and health operations
	case "quicbaas_vector_get_stats":
		return r.handleVectorGetStats(ctx, vectorService, args)
	case "quicbaas_vector_health":
		return r.handleVectorHealth(ctx, vectorService, args)

	// Index operations
	case "quicbaas_vector_rebuild_index":
		return r.handleVectorRebuildIndex(ctx, vectorService, args)
	case "quicbaas_vector_get_index_progress":
		return r.handleVectorGetIndexProgress(ctx, vectorService, args)

	// Admin operations
	case "quicbaas_vector_admin_get_cluster_stats":
		return r.handleVectorGetStats(ctx, vectorService, args) // Same as get_stats
	case "quicbaas_vector_admin_compact_namespace":
		return r.handleVectorCompactNamespace(ctx, vectorService, args)
	case "quicbaas_vector_admin_get_compaction_progress":
		return r.handleVectorGetCompactionProgress(ctx, vectorService, args)
	case "quicbaas_vector_admin_export_vectors",
		"quicbaas_vector_admin_import_vectors":
		return nil, api.NewFeatureUnsupportedError("ToolRegistry.executeVectorTool", "vector export/import requires gRPC streaming")

	default:
		return nil, api.NewUnknownToolError(name)
	}
}

// handleVectorCreateNamespace creates a new vector namespace.
func (r *ToolRegistry) handleVectorCreateNamespace(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	// Support both "namespace" and "name" parameter names
	namespace := getStringAlt(args, "namespace", "name")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace or name")
	}

	dimensions := getInt(args, "dimensions")
	if dimensions == 0 {
		return nil, api.NewRequiredFieldError("dimensions")
	}

	// Support both "distance_metric" and "metric" parameter names
	distanceMetric := getStringAlt(args, "distance_metric", "metric")
	if distanceMetric == "" {
		distanceMetric = "L2" // Default
	}

	indexType := getString(args, "index_type")
	if indexType == "" {
		indexType = "HNSW" // Default
	}

	req := &service.CreateNamespaceRequest{
		Namespace:      namespace,
		Dimension:      uint32(dimensions), //nolint:gosec // G115: Safe conversion - dimensions validated
		DistanceMetric: distanceMetric,
		IndexType:      indexType,
	}

	resp, err := svc.CreateNamespace(ctx, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"namespace":  resp.Namespace,
		"dimensions": resp.Dimension,
		"timestamp":  resp.Timestamp,
		"success":    resp.Success,
		"message":    "Namespace created successfully",
	}, nil
}

// handleVectorDeleteNamespace deletes a vector namespace.
func (r *ToolRegistry) handleVectorDeleteNamespace(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	// Support both "namespace" and "name" parameter names
	namespace := getStringAlt(args, "namespace", "name")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace or name")
	}

	req := &service.DeleteNamespaceRequest{
		Namespace: namespace,
	}

	resp, err := svc.DeleteNamespace(ctx, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"namespace":       namespace,
		"success":         resp.Success,
		"vectors_deleted": resp.VectorsDeleted,
		"timestamp":       resp.Timestamp,
		"message":         "Namespace deleted successfully",
	}, nil
}

// handleVectorListNamespaces lists all vector namespaces.
func (r *ToolRegistry) handleVectorListNamespaces(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	prefix := getString(args, "prefix")
	limit := getInt(args, "page_size")
	if limit == 0 {
		limit = 100
	}

	req := &service.ListNamespacesRequest{
		Prefix: prefix,
		Limit:  limit,
	}

	resp, err := svc.ListNamespaces(ctx, req)
	if err != nil {
		return nil, err
	}

	namespaces := make([]map[string]interface{}, 0, len(resp.Namespaces))
	for _, ns := range resp.Namespaces {
		namespaces = append(namespaces, map[string]interface{}{
			"namespace":       ns.Namespace,
			"dimensions":      ns.Dimension,
			"vector_count":    ns.VectorCount,
			"distance_metric": ns.DistanceMetric,
			"index_type":      ns.IndexType,
		})
	}

	return map[string]interface{}{
		"namespaces":  namespaces,
		"total_count": resp.TotalCount,
		"message":     "Namespaces listed successfully",
	}, nil
}

// handleVectorGetNamespaceInfo gets namespace information.
func (r *ToolRegistry) handleVectorGetNamespaceInfo(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	namespace := getString(args, "namespace")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	req := &service.GetNamespaceInfoRequest{
		Namespace: namespace,
	}

	resp, err := svc.GetNamespaceInfo(ctx, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"namespace":       resp.Namespace,
		"dimensions":      resp.Dimension,
		"vector_count":    resp.VectorCount,
		"distance_metric": resp.DistanceMetric,
		"index_type":      resp.IndexType,
		"created_at":      resp.CreatedAt,
		"updated_at":      resp.UpdatedAt,
		"message":         "Namespace info retrieved successfully",
	}, nil
}

// handleVectorUpdateNamespace updates namespace configuration.
func (r *ToolRegistry) handleVectorUpdateNamespace(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	// UpdateNamespace is not directly supported by VectorService interface,
	// but we can provide a wrapper that modifies index config if needed
	namespace := getString(args, "namespace")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	// For now, just return the namespace info as updates require index rebuilding
	return r.handleVectorGetNamespaceInfo(ctx, svc, args)
}

// handleVectorUpsert inserts or updates a vector.
func (r *ToolRegistry) handleVectorUpsert(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	namespace := getString(args, "namespace")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	id := getString(args, "id")
	if id == "" {
		return nil, api.NewRequiredFieldError("id")
	}

	// Parse vector - can be JSON string or direct array
	var vector []float32
	switch v := args["vector"].(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &vector); err != nil {
			return nil, api.NewInvalidFormatError("vector", err)
		}
	case []interface{}:
		vector = make([]float32, len(v))
		for i, val := range v {
			if f, ok := val.(float64); ok {
				vector[i] = float32(f)
			}
		}
	case []float32:
		vector = v
	case []float64:
		vector = make([]float32, len(v))
		for i, f := range v {
			vector[i] = float32(f)
		}
	default:
		return nil, api.NewInvalidTypeError("vector", "array")
	}

	// Parse metadata
	var metadata map[string]interface{}
	if metaVal, ok := args["metadata"]; ok {
		switch m := metaVal.(type) {
		case string:
			if err := json.Unmarshal([]byte(m), &metadata); err != nil {
				return nil, api.NewInvalidFormatError("metadata", err)
			}
		case map[string]interface{}:
			metadata = m
		case map[string]string:
			metadata = make(map[string]interface{})
			for k, v := range m {
				metadata[k] = v
			}
		}
	}

	// Initialize metadata if nil
	if metadata == nil {
		metadata = make(map[string]interface{})
	}

	// Store the original string ID in metadata for retrieval
	metadata["_original_id"] = id

	// Convert string ID to uint64 using FNV hash
	idNum := hashStringToID(id)

	req := &service.UpsertVectorRequest{
		Namespace: namespace,
		ID:        idNum,
		Vector:    vector,
		Metadata:  metadata,
	}

	resp, err := svc.UpsertVector(ctx, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"id":        id,
		"namespace": namespace,
		"version":   resp.Version,
		"timestamp": resp.Timestamp,
		"success":   resp.Success,
		"message":   "Vector upserted successfully",
	}, nil
}

// handleVectorBatchUpsert batch upserts multiple vectors.
func (r *ToolRegistry) handleVectorBatchUpsert(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	namespace := getString(args, "namespace")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	vectorsStr, ok := args["vectors"].(string)
	if !ok {
		return nil, api.NewRequiredFieldError("vectors")
	}

	var vectorsRaw []map[string]interface{}
	if err := json.Unmarshal([]byte(vectorsStr), &vectorsRaw); err != nil {
		return nil, api.NewInvalidFormatError("vectors", err)
	}

	vectors := make([]*service.VectorData, 0, len(vectorsRaw))
	for _, v := range vectorsRaw {
		id := getString(v, "id")
		vecData, _ := v["vector"].([]interface{})

		floatVec := make([]float32, len(vecData))
		for i, val := range vecData {
			if f, ok := val.(float64); ok {
				floatVec[i] = float32(f)
			}
		}

		var metadata map[string]interface{}
		if meta, ok := v["metadata"].(map[string]interface{}); ok {
			metadata = meta
		}

		// Initialize metadata if nil and store original string ID
		if metadata == nil {
			metadata = make(map[string]interface{})
		}
		metadata["_original_id"] = id

		vectors = append(vectors, &service.VectorData{
			ID:       hashStringToID(id),
			Vector:   floatVec,
			Metadata: metadata,
		})
	}

	req := &service.BatchUpsertRequest{
		Namespace: namespace,
		Vectors:   vectors,
	}

	resp, err := svc.BatchUpsert(ctx, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"namespace":      namespace,
		"inserted_count": resp.InsertedCount,
		"timestamp":      resp.Timestamp,
		"success":        resp.Success,
		"message":        "Batch upsert completed successfully",
	}, nil
}

// handleVectorGet retrieves a vector by ID.
func (r *ToolRegistry) handleVectorGet(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	namespace := getString(args, "namespace")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	id := getString(args, "id")
	if id == "" {
		return nil, api.NewRequiredFieldError("id")
	}

	req := &service.GetVectorRequest{
		Namespace:       namespace,
		ID:              hashStringToID(id),
		IncludeVector:   getBool(args, "include_vector") || true,
		IncludeMetadata: getBool(args, "include_metadata") || true,
	}

	resp, err := svc.GetVector(ctx, req)
	if err != nil {
		return nil, err
	}

	// Convert metadata to string map for consistency, filtering internal fields
	var metadata map[string]string
	if resp.Metadata != nil {
		metadata = make(map[string]string)
		for k, v := range resp.Metadata {
			// Skip internal metadata fields
			if k == "_original_id" {
				continue
			}
			metadata[k] = fmt.Sprintf("%v", v)
		}
	}

	return map[string]interface{}{
		"id":        id,
		"namespace": namespace,
		"vector":    resp.Vector,
		"metadata":  metadata,
		"version":   resp.Version,
		"found":     resp.Found,
		"message":   "Vector retrieved successfully",
	}, nil
}

// handleVectorDelete deletes a vector by ID.
func (r *ToolRegistry) handleVectorDelete(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	namespace := getString(args, "namespace")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	id := getString(args, "id")
	if id == "" {
		return nil, api.NewRequiredFieldError("id")
	}

	req := &service.DeleteVectorRequest{
		Namespace: namespace,
		ID:        hashStringToID(id),
	}

	resp, err := svc.DeleteVector(ctx, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"id":        id,
		"namespace": namespace,
		"deleted":   resp.Success,
		"timestamp": resp.Timestamp,
		"message":   "Vector deleted successfully",
	}, nil
}

// handleVectorUpdateMetadata updates metadata for an existing vector.
func (r *ToolRegistry) handleVectorUpdateMetadata(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	namespace := getString(args, "namespace")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	id := getString(args, "id")
	if id == "" {
		return nil, api.NewRequiredFieldError("id")
	}

	// Parse metadata - can be JSON string or map
	var metadata map[string]interface{}
	switch m := args["metadata"].(type) {
	case string:
		if err := json.Unmarshal([]byte(m), &metadata); err != nil {
			return nil, api.NewInvalidFormatError("metadata", err)
		}
	case map[string]interface{}:
		metadata = m
	case map[string]string:
		metadata = make(map[string]interface{})
		for k, v := range m {
			metadata[k] = v
		}
	default:
		return nil, api.NewRequiredFieldError("metadata")
	}

	// Get merge flag (default true)
	merge := true
	if mergeVal, ok := args["merge"].(bool); ok {
		merge = mergeVal
	} else if mergeStr, ok := args["merge"].(string); ok {
		merge = mergeStr != "false"
	}

	// If not merging (replace mode), we need to get the vector first and re-upsert
	// Otherwise we merge with existing metadata
	vecID := hashStringToID(id)

	// Get the existing vector
	getReq := &service.GetVectorRequest{
		Namespace:       namespace,
		ID:              vecID,
		IncludeVector:   true,
		IncludeMetadata: true,
	}

	getResp, err := svc.GetVector(ctx, getReq)
	if err != nil {
		return nil, api.NewOperationFailedError("get vector", err)
	}

	if !getResp.Found {
		return nil, api.NewNotFoundError("vector", id)
	}

	// Prepare the updated metadata
	var updatedMetadata map[string]interface{}
	if merge && getResp.Metadata != nil {
		// Merge existing metadata with new metadata
		updatedMetadata = getResp.Metadata
		for k, v := range metadata {
			updatedMetadata[k] = v
		}
	} else {
		// Replace with new metadata
		updatedMetadata = metadata
	}

	// Preserve the original ID
	updatedMetadata["_original_id"] = id

	// Upsert with updated metadata
	upsertReq := &service.UpsertVectorRequest{
		Namespace: namespace,
		ID:        vecID,
		Vector:    getResp.Vector,
		Metadata:  updatedMetadata,
	}

	upsertResp, err := svc.UpsertVector(ctx, upsertReq)
	if err != nil {
		return nil, api.NewOperationFailedError("update metadata", err)
	}

	return map[string]interface{}{
		"id":        id,
		"namespace": namespace,
		"success":   upsertResp.Success,
		"version":   upsertResp.Version,
		"timestamp": upsertResp.Timestamp,
		"message":   "Metadata updated successfully",
	}, nil
}

// handleVectorBatchDelete batch deletes multiple vectors.
func (r *ToolRegistry) handleVectorBatchDelete(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	namespace := getString(args, "namespace")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	idsStr, ok := args["ids"].(string)
	if !ok {
		return nil, api.NewRequiredFieldError("ids")
	}

	var ids []string
	if err := json.Unmarshal([]byte(idsStr), &ids); err != nil {
		return nil, api.NewInvalidFormatError("ids", err)
	}

	// Delete each vector individually (batch delete not directly supported)
	deletedCount := 0
	for _, id := range ids {
		req := &service.DeleteVectorRequest{
			Namespace: namespace,
			ID:        hashStringToID(id),
		}
		resp, err := svc.DeleteVector(ctx, req)
		if err == nil && resp.Success {
			deletedCount++
		}
	}

	return map[string]interface{}{
		"namespace":     namespace,
		"deleted_count": deletedCount,
		"total_ids":     len(ids),
		"timestamp":     time.Now().Unix(),
		"message":       "Batch delete completed successfully",
	}, nil
}

// handleVectorList lists all vectors in a namespace.
func (r *ToolRegistry) handleVectorList(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	namespace := getString(args, "namespace")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	// Get namespace info to retrieve vectors
	req := &service.GetNamespaceInfoRequest{
		Namespace: namespace,
	}

	resp, err := svc.GetNamespaceInfo(ctx, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"namespace":    namespace,
		"vector_count": resp.VectorCount,
		"message":      "Vector list retrieved successfully",
	}, nil
}

// handleVectorSearchSimilar performs similarity search.
func (r *ToolRegistry) handleVectorSearchSimilar(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	namespace := getString(args, "namespace")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	k := getInt(args, "k")
	if k == 0 {
		k = 10
	}

	// Parse query vector - support both "query_vector" and "vector" parameter names
	var queryVector []float32
	vectorArg := args["query_vector"]
	if vectorArg == nil {
		vectorArg = args["vector"]
	}

	switch v := vectorArg.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &queryVector); err != nil {
			return nil, api.NewInvalidFormatError("query_vector", err)
		}
	case []interface{}:
		queryVector = make([]float32, len(v))
		for i, val := range v {
			if f, ok := val.(float64); ok {
				queryVector[i] = float32(f)
			}
		}
	case []float32:
		queryVector = v
	case []float64:
		queryVector = make([]float32, len(v))
		for i, f := range v {
			queryVector[i] = float32(f)
		}
	default:
		return nil, api.NewRequiredFieldError("query_vector or vector")
	}

	req := &service.SearchSimilarRequest{
		Namespace:       namespace,
		QueryVector:     queryVector,
		K:               k,
		IncludeVectors:  getBool(args, "include_vectors"),
		IncludeMetadata: getBool(args, "include_metadata") || true,
	}

	resp, err := svc.SearchSimilar(ctx, req)
	if err != nil {
		return nil, err
	}

	results := make([]map[string]interface{}, 0, len(resp.Results))
	for _, r := range resp.Results {
		// Use original string ID from metadata if available, otherwise use numeric ID
		resultID := fmt.Sprintf("%d", r.ID)
		if r.Metadata != nil {
			if origID, ok := r.Metadata["_original_id"]; ok {
				resultID = fmt.Sprintf("%v", origID)
			}
		}

		result := map[string]interface{}{
			"id":        resultID,
			"namespace": namespace,
			"score":     r.Score,
			"rank":      r.Rank,
		}

		if len(r.Vector) > 0 {
			result["vector"] = r.Vector
		}

		if r.Metadata != nil {
			metadata := make(map[string]string)
			for k, v := range r.Metadata {
				// Skip internal metadata fields
				if k == "_original_id" {
					continue
				}
				metadata[k] = fmt.Sprintf("%v", v)
			}
			result["metadata"] = metadata
		}

		results = append(results, result)
	}

	return map[string]interface{}{
		"namespace":      namespace,
		"k":              k,
		"results":        results,
		"query_duration": resp.QueryDuration,
		"timestamp":      resp.Timestamp,
		"message":        "Similarity search completed successfully",
	}, nil
}

// handleVectorSearchByID performs search using an existing vector.
func (r *ToolRegistry) handleVectorSearchByID(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	namespace := getString(args, "namespace")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	id := getString(args, "id")
	if id == "" {
		return nil, api.NewRequiredFieldError("id")
	}

	k := getInt(args, "k")
	if k == 0 {
		k = 10
	}

	// First get the vector
	getReq := &service.GetVectorRequest{
		Namespace:     namespace,
		ID:            hashStringToID(id),
		IncludeVector: true,
	}

	getResp, err := svc.GetVector(ctx, getReq)
	if err != nil {
		return nil, err
	}

	if !getResp.Found || len(getResp.Vector) == 0 {
		return nil, api.NewNotFoundError("vector", id)
	}

	// Then search using that vector
	searchReq := &service.SearchSimilarRequest{
		Namespace:       namespace,
		QueryVector:     getResp.Vector,
		K:               k + 1, // +1 to account for self
		IncludeVectors:  getBool(args, "include_vectors"),
		IncludeMetadata: getBool(args, "include_metadata") || true,
	}

	resp, err := svc.SearchSimilar(ctx, searchReq)
	if err != nil {
		return nil, err
	}

	results := make([]map[string]interface{}, 0, len(resp.Results))
	for _, r := range resp.Results {
		// Skip self if include_self is false
		if !getBool(args, "include_self") && r.ID == hashStringToID(id) {
			continue
		}

		// Use original string ID from metadata if available, otherwise use numeric ID
		resultID := fmt.Sprintf("%d", r.ID)
		if r.Metadata != nil {
			if origID, ok := r.Metadata["_original_id"]; ok {
				resultID = fmt.Sprintf("%v", origID)
			}
		}

		result := map[string]interface{}{
			"id":        resultID,
			"namespace": namespace,
			"score":     r.Score,
			"rank":      r.Rank,
		}

		if len(r.Vector) > 0 {
			result["vector"] = r.Vector
		}

		if r.Metadata != nil {
			metadata := make(map[string]string)
			for k, v := range r.Metadata {
				// Skip internal metadata fields
				if k == "_original_id" {
					continue
				}
				metadata[k] = fmt.Sprintf("%v", v)
			}
			result["metadata"] = metadata
		}

		results = append(results, result)
	}

	return map[string]interface{}{
		"namespace":      namespace,
		"query_id":       id,
		"k":              k,
		"results":        results,
		"query_duration": resp.QueryDuration,
		"timestamp":      resp.Timestamp,
		"message":        "Search by ID completed successfully",
	}, nil
}

// handleVectorGetStats gets vector database statistics.
func (r *ToolRegistry) handleVectorGetStats(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	namespace := getString(args, "namespace")

	if namespace != "" {
		req := &service.GetIndexStatsRequest{
			Namespace: namespace,
		}

		resp, err := svc.GetIndexStats(ctx, req)
		if err != nil {
			return nil, err
		}

		return map[string]interface{}{
			"namespace":          resp.Namespace,
			"vector_count":       resp.VectorCount,
			"dimensions":         resp.Dimension,
			"index_type":         resp.IndexType,
			"index_size_bytes":   resp.IndexSizeBytes,
			"memory_usage_bytes": resp.MemoryUsageBytes,
			"timestamp":          resp.Timestamp,
			"message":            "Vector stats retrieved successfully",
		}, nil
	}

	// Cluster-wide stats - list all namespaces
	listReq := &service.ListNamespacesRequest{
		Limit: 1000,
	}

	listResp, err := svc.ListNamespaces(ctx, listReq)
	if err != nil {
		return nil, err
	}

	var totalVectors uint64
	for _, ns := range listResp.Namespaces {
		totalVectors += ns.VectorCount
	}

	return map[string]interface{}{
		"total_namespaces": listResp.TotalCount,
		"total_vectors":    totalVectors,
		"timestamp":        time.Now().Unix(),
		"message":          "Cluster stats retrieved successfully",
	}, nil
}

// handleVectorHealth gets vector database health status.
func (r *ToolRegistry) handleVectorHealth(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	// Check service availability by listing namespaces
	listReq := &service.ListNamespacesRequest{
		Limit: 1,
	}

	_, err := svc.ListNamespaces(ctx, listReq)
	if err != nil {
		return map[string]interface{}{
			"status":  "unhealthy",
			"message": err.Error(),
		}, nil
	}

	return map[string]interface{}{
		"status":  "healthy",
		"message": "Vector database is healthy",
	}, nil
}

// handleVectorRebuildIndex rebuilds the vector index.
func (r *ToolRegistry) handleVectorRebuildIndex(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	namespace := getString(args, "namespace")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	async := getBool(args, "async")
	if !async && args["async"] == nil {
		async = true // Default to async
	}

	req := &service.RebuildIndexRequest{
		Namespace: namespace,
		Async:     async,
	}

	resp, err := svc.RebuildIndex(ctx, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"namespace":        namespace,
		"job_id":           resp.JobID,
		"status":           resp.Status,
		"percent_complete": resp.PercentComplete,
		"async":            async,
		"timestamp":        resp.Timestamp,
		"message":          "Index rebuild initiated successfully",
	}, nil
}

// handleVectorGetIndexProgress gets index rebuild progress.
func (r *ToolRegistry) handleVectorGetIndexProgress(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	namespace := getString(args, "namespace")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	jobID := getString(args, "operation_id")

	req := &service.GetRebuildProgressRequest{
		Namespace: namespace,
		JobID:     jobID,
	}

	resp, err := svc.GetRebuildProgress(ctx, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"namespace":                   resp.Namespace,
		"job_id":                      resp.JobID,
		"status":                      resp.Status,
		"vectors_processed":           resp.VectorsProcessed,
		"total_vectors":               resp.TotalVectors,
		"percent_complete":            resp.PercentComplete,
		"elapsed_time_ms":             resp.ElapsedTimeMs,
		"estimated_time_remaining_ms": resp.EstimatedTimeRemainingMs,
		"started_at":                  resp.StartedAt,
		"completed_at":                resp.CompletedAt,
		"error":                       resp.Error,
		"message":                     "Index progress retrieved successfully",
	}, nil
}

// handleVectorCompactNamespace performs namespace compaction.
func (r *ToolRegistry) handleVectorCompactNamespace(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	namespace := getString(args, "namespace")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	async := getBool(args, "async")
	if !async && args["async"] == nil {
		async = true // Default to async
	}

	req := &service.CompactNamespaceRequest{
		Namespace: namespace,
		Async:     async,
	}

	resp, err := svc.CompactNamespace(ctx, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"namespace":    namespace,
		"operation_id": resp.OperationID,
		"started_at":   resp.StartedAt,
		"async":        async,
		"message":      "Compaction initiated successfully",
	}, nil
}

// handleVectorGetCompactionProgress gets compaction progress.
func (r *ToolRegistry) handleVectorGetCompactionProgress(ctx context.Context, svc service.VectorService, args map[string]interface{}) (interface{}, error) {
	namespace := getString(args, "namespace")
	if namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	operationID := getString(args, "operation_id")

	req := &service.GetCompactionProgressRequest{
		Namespace:   namespace,
		OperationID: operationID,
	}

	resp, err := svc.GetCompactionProgress(ctx, req)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"namespace":        resp.Namespace,
		"operation_id":     resp.OperationID,
		"bytes_compacted":  resp.BytesCompacted,
		"total_bytes":      resp.TotalBytes,
		"percent_complete": resp.PercentComplete,
		"in_progress":      resp.InProgress,
		"started_at":       resp.StartedAt,
		"completed_at":     resp.CompletedAt,
		"error":            resp.Error,
		"message":          "Compaction progress retrieved successfully",
	}, nil
}

// hashStringToID converts a string ID to uint64 using FNV-1a hash.
func hashStringToID(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s)) //nolint:gosec // G104: hash.Write never returns error
	return h.Sum64()
}
