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

// Package mcp implements tool execution handlers for Vector Database operations.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// normalizeHealthStatus converts proto VectorHealthStatus enum to human-readable lowercase string.
func normalizeHealthStatus(status pb.VectorHealthStatus) string {
	switch status {
	case pb.VectorHealthStatus_VECTOR_HEALTH_STATUS_HEALTHY:
		return "healthy"
	case pb.VectorHealthStatus_VECTOR_HEALTH_STATUS_DEGRADED:
		return "degraded"
	case pb.VectorHealthStatus_VECTOR_HEALTH_STATUS_UNHEALTHY:
		return "unhealthy"
	default:
		return "unknown"
	}
}

// ========================================
// VectorDB Operation Handlers (17)
// ========================================

// handleUpsertVector inserts or updates a single vector.
func (s *MCPServer) handleUpsertVector(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	id, ok := params["id"].(string)
	if !ok || id == "" {
		return nil, api.NewRequiredFieldError("id")
	}

	vectorStr, ok := params["vector"].(string)
	if !ok || vectorStr == "" {
		return nil, api.NewRequiredFieldError("vector")
	}

	// Parse vector array
	var vector []float32
	if err := json.Unmarshal([]byte(vectorStr), &vector); err != nil {
		return nil, api.NewInvalidFormatError("vector", err)
	}

	// Optional parameters
	var metadata map[string]string
	if metadataStr, ok := params["metadata"].(string); ok && metadataStr != "" {
		if err := json.Unmarshal([]byte(metadataStr), &metadata); err != nil {
			return nil, api.NewInvalidFormatError("metadata", err)
		}
	}

	consistency := s.parseStringParam(params, "consistency", "QUORUM")
	skipIndexUpdate := s.parseBoolParam(params, "skip_index_update", false)
	timeoutMs, err := s.parseIntParam(params, "timeout_ms", 5000)
	if err != nil {
		return nil, err
	}

	// Call actual gRPC service
	resp, err := s.vectorClient.UpsertVector(ctx, &pb.UpsertVectorRequest{
		Vector: &pb.VectorEntity{
			Id:        id,
			Namespace: namespace,
			Vector:    vector,
			Metadata:  metadata,
		},
		Consistency:     parseConsistencyLevel(consistency),
		SkipIndexUpdate: skipIndexUpdate,
		TimeoutMs:       int64(timeoutMs),
	})
	if err != nil {
		return nil, api.NewOperationFailedError("upsert", err)
	}

	result := map[string]interface{}{
		"id":            resp.Id,
		"namespace":     namespace,
		"version":       resp.Version,
		"timestamp":     resp.Timestamp.AsTime().Format(time.RFC3339),
		"leader_id":     resp.LeaderId,
		"index_updated": resp.IndexUpdated,
		"consistency":   consistency,
		"timeout_ms":    timeoutMs,
		"vector_dims":   len(vector),
		"message":       "Vector upserted successfully",
	}

	return s.jsonToolResponse(result)
}

// handleBatchUpsertVectors batch upserts multiple vectors.
func (s *MCPServer) handleBatchUpsertVectors(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	vectorsStr, ok := params["vectors"].(string)
	if !ok || vectorsStr == "" {
		return nil, api.NewRequiredFieldError("vectors")
	}

	// Parse vectors array
	var vectors []map[string]interface{}
	if err := json.Unmarshal([]byte(vectorsStr), &vectors); err != nil {
		return nil, api.NewInvalidFormatError("vectors", err)
	}

	// Convert to protobuf vectors
	pbVectors := make([]*pb.VectorEntity, 0, len(vectors))
	for _, vec := range vectors {
		id, _ := vec["id"].(string)
		vecData, _ := vec["vector"].([]interface{})

		// Convert vector to float32
		floatVec := make([]float32, len(vecData))
		for i, v := range vecData {
			if f, ok := v.(float64); ok {
				floatVec[i] = float32(f)
			}
		}

		metadata := make(map[string]string)
		if meta, ok := vec["metadata"].(map[string]interface{}); ok {
			for k, v := range meta {
				metadata[k] = fmt.Sprintf("%v", v)
			}
		}

		pbVectors = append(pbVectors, &pb.VectorEntity{
			Id:        id,
			Namespace: namespace,
			Vector:    floatVec,
			Metadata:  metadata,
		})
	}

	// Optional parameters
	consistency := s.parseStringParam(params, "consistency", "QUORUM")
	skipIndexUpdate := s.parseBoolParam(params, "skip_index_update", false)
	batchSize, err := s.parseInt32Param(params, "batch_size", 1000)
	if err != nil {
		return nil, err
	}
	timeoutMs, err := s.parseIntParam(params, "timeout_ms", 60000)
	if err != nil {
		return nil, err
	}

	// Call streaming gRPC
	stream, err := s.vectorClient.BatchUpsertVectors(ctx, &pb.BatchUpsertVectorsRequest{
		Vectors:         pbVectors,
		Consistency:     parseConsistencyLevel(consistency),
		SkipIndexUpdate: skipIndexUpdate,
		BatchSize:       batchSize,
		TimeoutMs:       int64(timeoutMs),
	})
	if err != nil {
		return nil, api.NewOperationFailedError("batch upsert", err)
	}

	// Collect streaming responses
	var finalResp *pb.BatchUpsertVectorsResponse
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, api.NewStreamError(err)
		}
		finalResp = resp
	}

	if finalResp == nil {
		return nil, api.NewNoResponseError("batch upsert")
	}

	// Return aggregated result
	result := map[string]interface{}{
		"namespace":         namespace,
		"vectors_total":     len(vectors),
		"vectors_processed": finalResp.VectorsProcessed,
		"vectors_succeeded": finalResp.VectorsSucceeded,
		"vectors_failed":    finalResp.VectorsFailed,
		"failed_ids":        finalResp.FailedIds,
		"batch_size":        batchSize,
		"consistency":       consistency,
		"index_updated":     !skipIndexUpdate,
		"timeout_ms":        timeoutMs,
		"is_final":          finalResp.IsFinal,
		"message":           "Batch upsert completed successfully",
	}

	return s.jsonToolResponse(result)
}

// handleGetVector retrieves a vector by ID.
func (s *MCPServer) handleGetVector(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	id, ok := params["id"].(string)
	if !ok || id == "" {
		return nil, api.NewRequiredFieldError("id")
	}

	// Optional parameters
	consistency := s.parseStringParam(params, "consistency", "QUORUM")
	includeMetadata := s.parseBoolParam(params, "include_metadata", true)
	includeVector := s.parseBoolParam(params, "include_vector", true)
	timeoutMs, err := s.parseIntParam(params, "timeout_ms", 5000)
	if err != nil {
		return nil, err
	}

	// Call gRPC service
	resp, err := s.vectorClient.GetVector(ctx, &pb.GetVectorRequest{
		Namespace:       namespace,
		Id:              id,
		Consistency:     parseConsistencyLevel(consistency),
		IncludeMetadata: includeMetadata,
		IncludeVector:   includeVector,
		TimeoutMs:       int64(timeoutMs),
	})
	if err != nil {
		return nil, api.NewOperationFailedError("get vector", err)
	}

	result := map[string]interface{}{
		"id":          id,
		"namespace":   namespace,
		"found":       resp.Found,
		"consistency": consistency,
		"timeout_ms":  timeoutMs,
	}

	if resp.Found && resp.Vector != nil {
		result["version"] = resp.Vector.Version
		result["node_id"] = resp.NodeId

		if includeVector && len(resp.Vector.Vector) > 0 {
			result["vector"] = resp.Vector.Vector
			result["dimensions"] = len(resp.Vector.Vector)
		}

		if includeMetadata && len(resp.Vector.Metadata) > 0 {
			result["metadata"] = resp.Vector.Metadata
		}

		result["message"] = "Vector retrieved successfully"
	} else {
		result["message"] = "Vector not found"
	}

	return s.jsonToolResponse(result)
}

// handleDeleteVector deletes a vector by ID.
func (s *MCPServer) handleDeleteVector(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	id, ok := params["id"].(string)
	if !ok || id == "" {
		return nil, api.NewRequiredFieldError("id")
	}

	// Optional parameters
	consistency := s.parseStringParam(params, "consistency", "QUORUM")
	version, err := s.parseUint64Param(params, "version")
	if err != nil {
		return nil, err
	}
	timeoutMs, err := s.parseIntParam(params, "timeout_ms", 5000)
	if err != nil {
		return nil, err
	}

	// Call gRPC service
	resp, err := s.vectorClient.DeleteVector(ctx, &pb.DeleteVectorRequest{
		Namespace:   namespace,
		Id:          id,
		Consistency: parseConsistencyLevel(consistency),
		Version:     version,
		TimeoutMs:   int64(timeoutMs),
	})
	if err != nil {
		return nil, api.NewOperationFailedError("delete", err)
	}

	result := map[string]interface{}{
		"id":          id,
		"namespace":   namespace,
		"deleted":     resp.Deleted,
		"timestamp":   resp.Timestamp.AsTime().Format(time.RFC3339),
		"consistency": consistency,
		"version":     version,
		"timeout_ms":  timeoutMs,
		"message":     "Vector deleted successfully",
	}

	return s.jsonToolResponse(result)
}

// handleUpdateMetadata updates metadata for a vector.
func (s *MCPServer) handleUpdateMetadata(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	id, ok := params["id"].(string)
	if !ok || id == "" {
		return nil, api.NewRequiredFieldError("id")
	}

	// Parse metadata from params - can be map or JSON string
	var metadata map[string]string
	switch m := params["metadata"].(type) {
	case map[string]interface{}:
		metadata = make(map[string]string)
		for k, v := range m {
			if s, ok := v.(string); ok {
				metadata[k] = s
			}
		}
	case string:
		if err := json.Unmarshal([]byte(m), &metadata); err != nil {
			return nil, api.NewInvalidFormatError("metadata", err)
		}
	default:
		return nil, api.NewRequiredFieldError("metadata")
	}

	// Optional parameters
	merge := s.parseBoolParam(params, "merge", true)
	timeoutMs, err := s.parseIntParam(params, "timeout_ms", 5000)
	if err != nil {
		return nil, err
	}

	// Call gRPC service
	resp, err := s.vectorClient.UpdateMetadata(ctx, &pb.UpdateMetadataRequest{
		Namespace: namespace,
		Id:        id,
		Metadata:  metadata,
		Merge:     merge,
		TimeoutMs: int64(timeoutMs),
	})
	if err != nil {
		return nil, api.NewOperationFailedError("update metadata", err)
	}

	result := map[string]interface{}{
		"id":         id,
		"namespace":  namespace,
		"success":    resp.Success,
		"version":    resp.Version,
		"timestamp":  resp.Timestamp.AsTime().Format(time.RFC3339),
		"timeout_ms": timeoutMs,
		"message":    "Metadata updated successfully",
	}

	return s.jsonToolResponse(result)
}

// handleBatchDeleteVectors batch deletes multiple vectors.
func (s *MCPServer) handleBatchDeleteVectors(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	idsStr, ok := params["ids"].(string)
	if !ok || idsStr == "" {
		return nil, api.NewRequiredFieldError("ids")
	}

	// Parse IDs array
	var ids []string
	if err := json.Unmarshal([]byte(idsStr), &ids); err != nil {
		return nil, api.NewInvalidFormatError("ids", err)
	}

	if len(ids) > 10000 {
		return nil, api.NewMaxLimitError("IDs", 10000)
	}

	// Optional parameters
	consistency := s.parseStringParam(params, "consistency", "QUORUM")
	timeoutMs, err := s.parseIntParam(params, "timeout_ms", 60000)
	if err != nil {
		return nil, err
	}

	// Call gRPC service
	resp, err := s.vectorClient.BatchDeleteVectors(ctx, &pb.BatchDeleteVectorsRequest{
		Namespace:   namespace,
		Ids:         ids,
		Consistency: parseConsistencyLevel(consistency),
		TimeoutMs:   int64(timeoutMs),
	})
	if err != nil {
		return nil, api.NewOperationFailedError("batch delete", err)
	}

	result := map[string]interface{}{
		"namespace":     namespace,
		"ids_total":     len(ids),
		"deleted_count": resp.DeletedCount,
		"timestamp":     resp.Timestamp.AsTime().Format(time.RFC3339),
		"consistency":   consistency,
		"timeout_ms":    timeoutMs,
		"message":       "Batch delete completed successfully",
	}

	return s.jsonToolResponse(result)
}

// handleSearchSimilar performs k-NN similarity search.
func (s *MCPServer) handleSearchSimilar(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	queryVectorStr, ok := params["query_vector"].(string)
	if !ok || queryVectorStr == "" {
		return nil, api.NewRequiredFieldError("query_vector")
	}

	// Parse query vector
	var queryVector []float32
	if err := json.Unmarshal([]byte(queryVectorStr), &queryVector); err != nil {
		return nil, api.NewInvalidFormatError("query_vector", err)
	}

	k, err := s.parseInt32Param(params, "k", 10)
	if err != nil {
		return nil, err
	}
	if k < 1 || k > 10000 {
		return nil, api.NewRangeError("k", 1, 10000)
	}

	// Optional parameters
	distanceMetric := s.parseStringParam(params, "distance_metric", "")
	consistency := s.parseStringParam(params, "consistency", "QUORUM")
	includeMetadata := s.parseBoolParam(params, "include_metadata", true)
	includeVectors := s.parseBoolParam(params, "include_vectors", false)
	efSearch, err := s.parseInt32Param(params, "ef_search", 0)
	if err != nil {
		return nil, err
	}
	minScore := s.parseFloatParam(params, "min_score", 0.0)
	timeoutMs, err := s.parseIntParam(params, "timeout_ms", 10000)
	if err != nil {
		return nil, err
	}

	// Parse metadata filters if provided
	var metadataFilters []*pb.MetadataFilter
	if filtersStr, ok := params["metadata_filters"].(string); ok && filtersStr != "" {
		var filters []map[string]interface{}
		if err := json.Unmarshal([]byte(filtersStr), &filters); err != nil {
			return nil, api.NewInvalidFormatError("metadata_filters", err)
		}

		for _, f := range filters {
			key, _ := f["key"].(string)
			value, _ := f["value"].(string)
			metadataFilters = append(metadataFilters, &pb.MetadataFilter{
				Key:   key,
				Value: value,
			})
		}
	}

	// Call gRPC service
	stream, err := s.vectorClient.SearchSimilar(ctx, &pb.SearchSimilarRequest{
		Namespace:       namespace,
		QueryVector:     queryVector,
		K:               k,
		DistanceMetric:  parseDistanceMetric(distanceMetric),
		Consistency:     parseConsistencyLevel(consistency),
		IncludeMetadata: includeMetadata,
		IncludeVectors:  includeVectors,
		EfSearch:        efSearch,
		MinScore:        float32(minScore),
		MetadataFilters: metadataFilters,
		TimeoutMs:       int64(timeoutMs),
	})
	if err != nil {
		return nil, api.NewOperationFailedError("search", err)
	}

	// Collect all results
	var allResults []*pb.SimilarityResult
	var totalCandidates int64
	var searchTimeMs int64
	var nodeID uint64

	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, api.NewStreamError(err)
		}

		allResults = append(allResults, resp.Results...)
		totalCandidates = resp.TotalCandidates
		searchTimeMs = resp.SearchTimeMs
		nodeID = resp.NodeId
	}

	// Convert results to JSON-friendly format
	results := make([]map[string]interface{}, 0, len(allResults))
	for _, res := range allResults {
		r := map[string]interface{}{
			"id":        res.Vector.Id,
			"namespace": namespace,
			"score":     res.Score,
			"rank":      res.Rank,
		}

		if includeVectors && len(res.Vector.Vector) > 0 {
			r["vector"] = res.Vector.Vector
			r["dimensions"] = len(res.Vector.Vector)
		}

		if includeMetadata && len(res.Vector.Metadata) > 0 {
			r["metadata"] = res.Vector.Metadata
		}

		results = append(results, r)
	}

	result := map[string]interface{}{
		"namespace":        namespace,
		"k":                k,
		"results":          results,
		"total_candidates": totalCandidates,
		"search_time_ms":   searchTimeMs,
		"node_id":          nodeID,
		"consistency":      consistency,
		"distance_metric":  distanceMetric,
		"ef_search":        efSearch,
		"min_score":        minScore,
		"filters_count":    len(metadataFilters),
		"timeout_ms":       timeoutMs,
		"query_dims":       len(queryVector),
		"is_final":         true,
		"message":          "Similarity search completed successfully",
	}

	return s.jsonToolResponse(result)
}

// handleSearchByID performs k-NN search using existing vector.
func (s *MCPServer) handleSearchByID(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	id, ok := params["id"].(string)
	if !ok || id == "" {
		return nil, api.NewRequiredFieldError("id")
	}

	k, err := s.parseInt32Param(params, "k", 10)
	if err != nil {
		return nil, err
	}
	if k < 1 || k > 10000 {
		return nil, api.NewRangeError("k", 1, 10000)
	}

	// Optional parameters
	includeSelf := s.parseBoolParam(params, "include_self", false)
	distanceMetric := s.parseStringParam(params, "distance_metric", "")
	consistency := s.parseStringParam(params, "consistency", "QUORUM")
	includeMetadata := s.parseBoolParam(params, "include_metadata", true)
	includeVectors := s.parseBoolParam(params, "include_vectors", false)
	efSearch, err := s.parseInt32Param(params, "ef_search", 0)
	if err != nil {
		return nil, err
	}
	minScore := s.parseFloatParam(params, "min_score", 0.0)
	timeoutMs, err := s.parseIntParam(params, "timeout_ms", 10000)
	if err != nil {
		return nil, err
	}

	// Call gRPC service
	stream, err := s.vectorClient.SearchByID(ctx, &pb.SearchByIDRequest{
		Namespace:       namespace,
		Id:              id,
		K:               k,
		IncludeSelf:     includeSelf,
		DistanceMetric:  parseDistanceMetric(distanceMetric),
		Consistency:     parseConsistencyLevel(consistency),
		IncludeMetadata: includeMetadata,
		IncludeVectors:  includeVectors,
		EfSearch:        efSearch,
		MinScore:        float32(minScore),
		TimeoutMs:       int64(timeoutMs),
	})
	if err != nil {
		return nil, api.NewOperationFailedError("search by ID", err)
	}

	// Collect all results
	var allResults []*pb.SimilarityResult
	var totalCandidates int64
	var searchTimeMs int64
	var nodeID uint64

	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, api.NewStreamError(err)
		}

		allResults = append(allResults, resp.Results...)
		totalCandidates = resp.TotalCandidates
		searchTimeMs = resp.SearchTimeMs
		nodeID = resp.NodeId
	}

	// Convert results
	results := make([]map[string]interface{}, 0, len(allResults))
	for _, res := range allResults {
		r := map[string]interface{}{
			"id":        res.Vector.Id,
			"namespace": namespace,
			"score":     res.Score,
			"rank":      res.Rank,
		}

		if includeVectors && len(res.Vector.Vector) > 0 {
			r["vector"] = res.Vector.Vector
		}

		if includeMetadata && len(res.Vector.Metadata) > 0 {
			r["metadata"] = res.Vector.Metadata
		}

		results = append(results, r)
	}

	result := map[string]interface{}{
		"namespace":        namespace,
		"query_id":         id,
		"k":                k,
		"include_self":     includeSelf,
		"results":          results,
		"total_candidates": totalCandidates,
		"search_time_ms":   searchTimeMs,
		"node_id":          nodeID,
		"consistency":      consistency,
		"distance_metric":  distanceMetric,
		"ef_search":        efSearch,
		"min_score":        minScore,
		"timeout_ms":       timeoutMs,
		"is_final":         true,
		"message":          "Search by ID completed successfully",
	}

	return s.jsonToolResponse(result)
}

// handleListVectors lists all vectors in a namespace.
func (s *MCPServer) handleListVectors(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	// Optional parameters
	pageSize, err := s.parseInt32Param(params, "page_size", 100)
	if err != nil {
		return nil, err
	}
	// Saturation is the page-size contract: a larger page receives the cap.
	pageSize = min(pageSize, 1000)
	pageToken := s.parseStringParam(params, "page_token", "")
	consistency := s.parseStringParam(params, "consistency", "QUORUM")
	includeMetadata := s.parseBoolParam(params, "include_metadata", true)
	includeVectors := s.parseBoolParam(params, "include_vectors", false)
	timeoutMs, err := s.parseIntParam(params, "timeout_ms", 30000)
	if err != nil {
		return nil, err
	}

	// Call gRPC service
	stream, err := s.vectorClient.ListVectors(ctx, &pb.ListVectorsRequest{
		Namespace:       namespace,
		PageSize:        pageSize,
		PageToken:       pageToken,
		Consistency:     parseConsistencyLevel(consistency),
		IncludeMetadata: includeMetadata,
		IncludeVectors:  includeVectors,
		TimeoutMs:       int64(timeoutMs),
	})
	if err != nil {
		return nil, api.NewOperationFailedError("list vectors", err)
	}

	// Collect all vectors
	var allVectors []*pb.VectorEntity
	var nextPageToken string
	var totalCount int64

	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, api.NewStreamError(err)
		}

		allVectors = append(allVectors, resp.Vectors...)
		nextPageToken = resp.NextPageToken
		totalCount = resp.TotalCount
	}

	// Convert vectors
	vectors := make([]map[string]interface{}, 0, len(allVectors))
	for _, vec := range allVectors {
		v := map[string]interface{}{
			"id":        vec.Id,
			"namespace": namespace,
			"version":   vec.Version,
			"status":    "active",
		}

		if includeVectors && len(vec.Vector) > 0 {
			v["vector"] = vec.Vector
			v["dimensions"] = len(vec.Vector)
		}

		if includeMetadata && len(vec.Metadata) > 0 {
			v["metadata"] = vec.Metadata
		}

		vectors = append(vectors, v)
	}

	result := map[string]interface{}{
		"namespace":       namespace,
		"vectors":         vectors,
		"page_size":       pageSize,
		"page_token":      pageToken,
		"next_page_token": nextPageToken,
		"total_count":     totalCount,
		"consistency":     consistency,
		"timeout_ms":      timeoutMs,
		"is_final":        true,
		"message":         "Vectors listed successfully",
	}

	return s.jsonToolResponse(result)
}

// handleListNamespaces lists all vector namespaces.
func (s *MCPServer) handleListNamespaces(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	// Optional parameters
	prefix := s.parseStringParam(params, "prefix", "")
	pageSize, err := s.parseInt32Param(params, "page_size", 100)
	if err != nil {
		return nil, err
	}
	pageToken := s.parseStringParam(params, "page_token", "")

	// Call gRPC service
	resp, err := s.vectorClient.ListNamespaces(ctx, &pb.ListNamespacesRequest{
		Prefix:    prefix,
		PageSize:  pageSize,
		PageToken: pageToken,
	})
	if err != nil {
		return nil, api.NewOperationFailedError("list namespaces", err)
	}

	// Convert namespaces
	namespaces := make([]map[string]interface{}, 0, len(resp.Namespaces))
	for _, ns := range resp.Namespaces {
		n := map[string]interface{}{
			"namespace":       ns.Config.Namespace,
			"dimensions":      ns.Config.Dimensions,
			"distance_metric": ns.Config.DistanceMetric.String(),
			"vector_count":    ns.VectorCount,
		}
		namespaces = append(namespaces, n)
	}

	result := map[string]interface{}{
		"namespaces":      namespaces,
		"prefix":          prefix,
		"page_size":       pageSize,
		"page_token":      pageToken,
		"next_page_token": resp.NextPageToken,
		"total_count":     resp.TotalCount,
		"message":         "Namespaces listed successfully",
	}

	return s.jsonToolResponse(result)
}

// handleCreateNamespace creates a new vector namespace.
func (s *MCPServer) handleCreateNamespace(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	dimensions, err := s.parseInt32Param(params, "dimensions", 0)
	if err != nil {
		return nil, err
	}
	if dimensions < 1 || dimensions > 4096 {
		return nil, api.NewRangeError("dimensions", 1, 4096)
	}

	distanceMetric, ok := params["distance_metric"].(string)
	if !ok || distanceMetric == "" {
		return nil, api.NewRequiredFieldError("distance_metric")
	}

	// Optional parameters
	precision := s.parseStringParam(params, "precision", "FLOAT32")
	indexType := s.parseStringParam(params, "index_type", "HNSW")
	shardID, err := s.parseUint64Param(params, "shard_id")
	if err != nil {
		return nil, err
	}
	replicationFactor, err := s.parseInt32Param(params, "replication_factor", 3)
	if err != nil {
		return nil, err
	}
	compressionEnabled := s.parseBoolParam(params, "compression_enabled", false)
	timeoutMs, err := s.parseIntParam(params, "timeout_ms", 10000)
	if err != nil {
		return nil, err
	}

	// Parse optional JSON parameters
	var indexParams map[string]string
	if indexParamsStr, ok := params["index_params"].(string); ok && indexParamsStr != "" {
		var rawParams map[string]interface{}
		if err := json.Unmarshal([]byte(indexParamsStr), &rawParams); err != nil {
			return nil, api.NewInvalidFormatError("index_params", err)
		}
		indexParams = make(map[string]string)
		for k, v := range rawParams {
			indexParams[k] = fmt.Sprintf("%v", v)
		}
	}

	var metadata map[string]string
	if metadataStr, ok := params["metadata"].(string); ok && metadataStr != "" {
		if err := json.Unmarshal([]byte(metadataStr), &metadata); err != nil {
			return nil, api.NewInvalidFormatError("metadata", err)
		}
	}

	// Call gRPC service
	resp, err := s.vectorClient.CreateNamespace(ctx, &pb.CreateNamespaceRequest{
		Config: &pb.NamespaceConfig{
			Namespace:          namespace,
			Dimensions:         dimensions,
			DistanceMetric:     parseDistanceMetric(distanceMetric),
			Precision:          parsePrecision(precision),
			IndexType:          parseIndexType(indexType),
			ShardId:            shardID,
			ReplicationFactor:  replicationFactor,
			CompressionEnabled: compressionEnabled,
			IndexParams:        indexParams,
			Metadata:           metadata,
		},
		TimeoutMs: int64(timeoutMs),
	})
	if err != nil {
		return nil, api.NewOperationFailedError("create namespace", err)
	}

	result := map[string]interface{}{
		"namespace":           resp.Namespace,
		"dimensions":          dimensions,
		"distance_metric":     distanceMetric,
		"precision":           precision,
		"index_type":          indexType,
		"shard_id":            shardID,
		"replication_factor":  replicationFactor,
		"compression_enabled": compressionEnabled,
		"index_params":        indexParams,
		"metadata":            metadata,
		"timestamp":           resp.Timestamp.AsTime().Format(time.RFC3339),
		"timeout_ms":          timeoutMs,
		"message":             "Namespace created successfully",
	}

	return s.jsonToolResponse(result)
}

// handleUpdateNamespace updates namespace configuration.
func (s *MCPServer) handleUpdateNamespace(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	// Optional parameters
	replicationFactor, err := s.parseInt32Param(params, "replication_factor", 0)
	if err != nil {
		return nil, err
	}
	timeoutMs, err := s.parseIntParam(params, "timeout_ms", 10000)
	if err != nil {
		return nil, err
	}

	// Parse optional JSON parameters
	var indexParams map[string]string
	if indexParamsStr, ok := params["index_params"].(string); ok && indexParamsStr != "" {
		var rawParams map[string]interface{}
		if err := json.Unmarshal([]byte(indexParamsStr), &rawParams); err != nil {
			return nil, api.NewInvalidFormatError("index_params", err)
		}
		indexParams = make(map[string]string)
		for k, v := range rawParams {
			indexParams[k] = fmt.Sprintf("%v", v)
		}
	}

	var metadata map[string]string
	if metadataStr, ok := params["metadata"].(string); ok && metadataStr != "" {
		if err := json.Unmarshal([]byte(metadataStr), &metadata); err != nil {
			return nil, api.NewInvalidFormatError("metadata", err)
		}
	}

	// Call gRPC service
	resp, err := s.vectorClient.UpdateNamespace(ctx, &pb.UpdateNamespaceRequest{
		Namespace:         namespace,
		ReplicationFactor: replicationFactor,
		IndexParams:       indexParams,
		Metadata:          metadata,
		TimeoutMs:         int64(timeoutMs),
	})
	if status.Code(err) == codes.Unimplemented {
		return nil, &VectorNamespaceUpdateUnsupportedError{Namespace: namespace}
	}
	if err != nil {
		return nil, api.NewOperationFailedError("update namespace", err)
	}

	result := map[string]interface{}{
		"namespace":          resp.Namespace,
		"replication_factor": replicationFactor,
		"index_params":       indexParams,
		"metadata":           metadata,
		"timestamp":          resp.Timestamp.AsTime().Format(time.RFC3339),
		"requires_rebuild":   resp.RequiresRebuild,
		"timeout_ms":         timeoutMs,
		"message":            "Namespace updated successfully",
	}

	return s.jsonToolResponse(result)
}

// handleDeleteNamespace deletes a namespace.
func (s *MCPServer) handleDeleteNamespace(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	// Optional parameters
	force := s.parseBoolParam(params, "force", false)
	timeoutMs, err := s.parseIntParam(params, "timeout_ms", 60000)
	if err != nil {
		return nil, err
	}

	// Call gRPC service
	resp, err := s.vectorClient.DeleteNamespace(ctx, &pb.DeleteNamespaceRequest{
		Namespace: namespace,
		Force:     force,
		TimeoutMs: int64(timeoutMs),
	})
	if err != nil {
		return nil, api.NewOperationFailedError("delete namespace", err)
	}

	result := map[string]interface{}{
		"namespace":       namespace,
		"deleted":         resp.Deleted,
		"vectors_deleted": resp.VectorsDeleted,
		"timestamp":       resp.Timestamp.AsTime().Format(time.RFC3339),
		"force":           force,
		"timeout_ms":      timeoutMs,
		"message":         "Namespace deleted successfully",
	}

	return s.jsonToolResponse(result)
}

// handleGetNamespaceInfo retrieves namespace information.
func (s *MCPServer) handleGetNamespaceInfo(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	// Optional parameters
	includeStats := s.parseBoolParam(params, "include_stats", true)
	timeoutMs, err := s.parseIntParam(params, "timeout_ms", 5000)
	if err != nil {
		return nil, err
	}

	// Call gRPC service
	resp, err := s.vectorClient.GetNamespaceInfo(ctx, &pb.GetNamespaceInfoRequest{
		Namespace:    namespace,
		IncludeStats: includeStats,
		TimeoutMs:    int64(timeoutMs),
	})
	if err != nil {
		return nil, api.NewOperationFailedError("get namespace info", err)
	}

	result := map[string]interface{}{
		"namespace":       namespace,
		"dimensions":      resp.Info.Config.Dimensions,
		"distance_metric": resp.Info.Config.DistanceMetric.String(),
		"timeout_ms":      timeoutMs,
	}

	if includeStats {
		result["vector_count"] = resp.Info.VectorCount
		result["indexed_count"] = resp.Info.IndexedCount
		result["storage_bytes"] = resp.Info.StorageBytes
	}

	result["message"] = "Namespace info retrieved successfully"

	return s.jsonToolResponse(result)
}

// handleGetVectorStats returns vector database statistics.
func (s *MCPServer) handleGetVectorStats(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	// Optional parameters
	namespace := s.parseStringParam(params, "namespace", "")
	includeDetails := s.parseBoolParam(params, "include_details", false)
	timeoutMs, err := s.parseIntParam(params, "timeout_ms", 5000)
	if err != nil {
		return nil, err
	}

	// Call gRPC service
	resp, err := s.vectorClient.GetVectorStats(ctx, &pb.GetVectorStatsRequest{
		Namespace:      namespace,
		IncludeDetails: includeDetails,
		TimeoutMs:      int64(timeoutMs),
	})
	if err != nil {
		return nil, api.NewOperationFailedError("get stats", err)
	}

	result := map[string]interface{}{
		"total_vectors":       resp.TotalVectors,
		"total_namespaces":    resp.TotalNamespaces,
		"total_storage_bytes": resp.TotalStorageBytes,
		"total_index_bytes":   resp.TotalIndexBytes,
		"avg_dimension":       resp.AvgDimension,
		"timeout_ms":          timeoutMs,
		"message":             "Vector stats retrieved successfully",
	}

	if namespace != "" {
		result["namespace"] = namespace
	}

	return s.jsonToolResponse(result)
}

// handleVectorHealth returns health status.
func (s *MCPServer) handleVectorHealth(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	// Optional parameters
	includeDetails := s.parseBoolParam(params, "include_details", false)

	// Call gRPC service
	resp, err := s.vectorClient.Health(ctx, &pb.VectorHealthRequest{
		IncludeDetails: includeDetails,
	})
	if err != nil {
		return nil, api.NewOperationFailedError("health check", err)
	}

	result := map[string]interface{}{
		"status":               normalizeHealthStatus(resp.Status),
		"node_id":              resp.NodeId,
		"version":              resp.Version,
		"namespaces_healthy":   resp.NamespacesHealthy,
		"namespaces_degraded":  resp.NamespacesDegraded,
		"namespaces_unhealthy": resp.NamespacesUnhealthy,
		"message":              "Vector health retrieved successfully",
	}

	// Include component details if requested
	if includeDetails && len(resp.Details) > 0 {
		details := make(map[string]interface{})
		for key, compHealth := range resp.Details {
			details[key] = map[string]interface{}{
				"status":  normalizeHealthStatus(compHealth.Status),
				"message": compHealth.Message,
			}
		}
		result["details"] = details
	}

	return s.jsonToolResponse(result)
}

// handleRebuildIndex rebuilds vector index.
func (s *MCPServer) handleRebuildIndex(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	// Optional parameters
	async := s.parseBoolParam(params, "async", true)
	timeoutMs, err := s.parseIntParam(params, "timeout_ms", 300000)
	if err != nil {
		return nil, err
	}

	var indexParams map[string]string
	if indexParamsStr, ok := params["index_params"].(string); ok && indexParamsStr != "" {
		var rawParams map[string]interface{}
		if err := json.Unmarshal([]byte(indexParamsStr), &rawParams); err != nil {
			return nil, api.NewInvalidFormatError("index_params", err)
		}
		indexParams = make(map[string]string)
		for k, v := range rawParams {
			indexParams[k] = fmt.Sprintf("%v", v)
		}
	}

	// Call gRPC service
	resp, err := s.vectorClient.RebuildIndex(ctx, &pb.RebuildIndexRequest{
		Namespace:   namespace,
		Async:       async,
		IndexParams: indexParams,
		TimeoutMs:   int64(timeoutMs),
	})
	if err != nil {
		return nil, api.NewOperationFailedError("rebuild index", err)
	}

	result := map[string]interface{}{
		"namespace":             namespace,
		"operation_id":          resp.OperationId,
		"started":               resp.Started.AsTime().Format(time.RFC3339),
		"estimated_duration_ms": resp.EstimatedDurationMs,
		"async":                 async,
		"index_params":          indexParams,
		"timeout_ms":            timeoutMs,
		"message":               "Index rebuild initiated successfully",
	}

	return s.jsonToolResponse(result)
}

// handleGetIndexProgress gets index rebuild progress.
func (s *MCPServer) handleGetIndexProgress(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	// Optional parameters
	operationID := s.parseStringParam(params, "operation_id", "")

	// Call gRPC service
	resp, err := s.vectorClient.GetIndexProgress(ctx, &pb.GetIndexProgressRequest{
		Namespace:   namespace,
		OperationId: operationID,
	})
	if err != nil {
		return nil, api.NewOperationFailedError("get index progress", err)
	}

	result := map[string]interface{}{
		"operation_id":           resp.OperationId,
		"namespace":              resp.Namespace,
		"vectors_indexed":        resp.VectorsIndexed,
		"total_vectors":          resp.TotalVectors,
		"percent_complete":       resp.PercentComplete,
		"elapsed_time_ms":        resp.ElapsedTimeMs,
		"estimated_remaining_ms": resp.EstimatedRemainingMs,
		"status":                 resp.Status,
		"error_message":          resp.ErrorMessage,
		"message":                "Index progress retrieved successfully",
	}

	return s.jsonToolResponse(result)
}

// ========================================
// VectorAdmin Operation Handlers (5)
// ========================================

// handleGetVectorClusterStats returns cluster-wide statistics.
func (s *MCPServer) handleGetVectorClusterStats(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	// Optional parameters
	includeNamespaces := s.parseBoolParam(params, "include_namespaces", false)
	includeNodes := s.parseBoolParam(params, "include_nodes", false)

	// Call gRPC admin service
	resp, err := s.adminClient.GetClusterStats(ctx, &pb.GetClusterStatsRequest{
		IncludeNamespaces: includeNamespaces,
		IncludeNodes:      includeNodes,
	})
	if err != nil {
		return nil, api.NewOperationFailedError("get cluster stats", err)
	}

	result := map[string]interface{}{
		"cluster_id":          resp.ClusterId,
		"total_nodes":         resp.TotalNodes,
		"total_vectors":       resp.TotalVectors,
		"total_namespaces":    resp.TotalNamespaces,
		"total_storage_bytes": resp.TotalStorageBytes,
		"total_index_bytes":   resp.TotalIndexBytes,
		"timestamp":           resp.Timestamp.AsTime().Format(time.RFC3339),
		"message":             "Cluster stats retrieved successfully",
	}

	// Include namespace stats if requested
	if includeNamespaces && len(resp.NamespaceStats) > 0 {
		namespaceStats := make([]map[string]interface{}, 0, len(resp.NamespaceStats))
		for _, ns := range resp.NamespaceStats {
			namespaceStats = append(namespaceStats, map[string]interface{}{
				"namespace":     ns.Namespace,
				"vector_count":  ns.VectorCount,
				"storage_bytes": ns.StorageBytes,
			})
		}
		result["namespace_stats"] = namespaceStats
	}

	// Include node stats if requested
	if includeNodes && len(resp.NodeStats) > 0 {
		nodeStats := make([]map[string]interface{}, 0, len(resp.NodeStats))
		for _, node := range resp.NodeStats {
			nodeStats = append(nodeStats, map[string]interface{}{
				"node_id":       node.NodeId,
				"vector_count":  node.VectorCount,
				"storage_bytes": node.StorageBytes,
			})
		}
		result["node_stats"] = nodeStats
	}

	return s.jsonToolResponse(result)
}

// handleCompactNamespace performs namespace compaction.
func (s *MCPServer) handleCompactNamespace(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	// Optional parameters
	async := s.parseBoolParam(params, "async", true)
	timeoutMs, err := s.parseIntParam(params, "timeout_ms", 300000)
	if err != nil {
		return nil, err
	}

	// Call gRPC admin service
	resp, err := s.adminClient.CompactNamespace(ctx, &pb.CompactNamespaceRequest{
		Namespace: namespace,
		Async:     async,
		TimeoutMs: int64(timeoutMs),
	})
	if err != nil {
		return nil, api.NewOperationFailedError("compact namespace", err)
	}

	result := map[string]interface{}{
		"namespace":    namespace,
		"operation_id": resp.OperationId,
		"started":      resp.Started.AsTime().Format(time.RFC3339),
		"async":        async,
		"timeout_ms":   timeoutMs,
		"message":      "Compaction initiated successfully",
	}

	return s.jsonToolResponse(result)
}

// handleGetCompactionProgress gets compaction progress.
func (s *MCPServer) handleGetCompactionProgress(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	// Optional parameters
	operationID := s.parseStringParam(params, "operation_id", "")

	// Call gRPC admin service
	resp, err := s.adminClient.GetCompactionProgress(ctx, &pb.GetCompactionProgressRequest{
		Namespace:   namespace,
		OperationId: operationID,
	})
	if err != nil {
		return nil, api.NewOperationFailedError("get compaction progress", err)
	}

	result := map[string]interface{}{
		"operation_id":          resp.OperationId,
		"namespace":             resp.Namespace,
		"bytes_compacted":       resp.BytesCompacted,
		"total_bytes":           resp.TotalBytes,
		"percent_complete":      resp.PercentComplete,
		"elapsed_time_ms":       resp.ElapsedTimeMs,
		"space_reclaimed_bytes": resp.SpaceReclaimedBytes,
		"status":                resp.Status,
		"message":               "Compaction progress retrieved successfully",
	}

	return s.jsonToolResponse(result)
}

// handleExportVectors exports vectors for backup.
func (s *MCPServer) handleExportVectors(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	// Optional parameters
	format := s.parseStringParam(params, "format", "binary")
	compression := s.parseStringParam(params, "compression", "gzip")
	batchSize, err := s.parseInt32Param(params, "batch_size", 1000)
	if err != nil {
		return nil, err
	}

	// Call gRPC admin service (streaming)
	stream, err := s.adminClient.ExportVectors(ctx, &pb.ExportVectorsRequest{
		Namespace:   namespace,
		Format:      format,
		Compression: compression,
		BatchSize:   batchSize,
	})
	if err != nil {
		return nil, api.NewOperationFailedError("export vectors", err)
	}

	// Collect all responses
	var totalVectors int64
	var totalBytes int64
	var batchCount int

	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, api.NewStreamError(err)
		}

		totalVectors += resp.VectorsExported
		totalBytes += int64(len(resp.BatchData))
		batchCount++
	}

	result := map[string]interface{}{
		"namespace":        namespace,
		"format":           format,
		"compression":      compression,
		"batch_size":       batchSize,
		"vectors_exported": totalVectors,
		"batch_count":      batchCount,
		"total_bytes":      totalBytes,
		"is_final":         true,
		"message":          "Export completed successfully",
	}

	return s.jsonToolResponse(result)
}

// handleImportVectors imports vectors from backup.
func (s *MCPServer) handleImportVectors(ctx context.Context, params map[string]interface{}) (*ToolResponse, error) {
	namespace, ok := params["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	format, ok := params["format"].(string)
	if !ok || format == "" {
		return nil, api.NewRequiredFieldError("format")
	}

	data, ok := params["data"].(string)
	if !ok || data == "" {
		return nil, api.NewRequiredFieldError("data")
	}

	// Optional parameters
	skipValidation := s.parseBoolParam(params, "skip_validation", false)
	overwriteExisting := s.parseBoolParam(params, "overwrite_existing", false)

	// For simplicity, we send a single import request
	// In a real implementation, you would stream chunks of data
	stream, err := s.adminClient.ImportVectors(ctx)
	if err != nil {
		return nil, api.NewOperationFailedError("import vectors", err)
	}

	// Send import request
	if err := stream.Send(&pb.ImportVectorsRequest{
		Namespace:         namespace,
		Format:            format,
		BatchData:         []byte(data),
		SkipValidation:    skipValidation,
		OverwriteExisting: overwriteExisting,
	}); err != nil {
		return nil, api.NewOperationFailedError("send import request", err)
	}

	// Close and receive response
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return nil, api.NewOperationFailedError("import", err)
	}

	result := map[string]interface{}{
		"namespace":          namespace,
		"format":             format,
		"vectors_imported":   resp.VectorsImported,
		"vectors_skipped":    resp.VectorsSkipped,
		"vectors_failed":     resp.VectorsFailed,
		"duration_ms":        resp.DurationMs,
		"skip_validation":    skipValidation,
		"overwrite_existing": overwriteExisting,
		"timestamp":          resp.Timestamp.AsTime().Format(time.RFC3339),
		"message":            "Import completed successfully",
	}

	return s.jsonToolResponse(result)
}

// ========================================
// Helper Methods
// ========================================

// parseConsistencyLevel converts string to proto enum.
func parseConsistencyLevel(level string) pb.VectorConsistencyLevel {
	switch strings.ToUpper(level) {
	case "ALL":
		return pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_ALL
	case "QUORUM":
		return pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM
	case "ONE":
		return pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_ONE
	default:
		return pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM
	}
}

// parseDistanceMetric converts string to proto enum.
func parseDistanceMetric(metric string) pb.DistanceMetric {
	switch strings.ToUpper(metric) {
	case "L2", "EUCLIDEAN":
		return pb.DistanceMetric_DISTANCE_METRIC_L2
	case "COSINE":
		return pb.DistanceMetric_DISTANCE_METRIC_COSINE
	case "DOT_PRODUCT", "DOT":
		return pb.DistanceMetric_DISTANCE_METRIC_DOT_PRODUCT
	case "MANHATTAN", "L1":
		return pb.DistanceMetric_DISTANCE_METRIC_MANHATTAN
	default:
		return pb.DistanceMetric_DISTANCE_METRIC_COSINE
	}
}

// parsePrecision converts string to proto enum.
func parsePrecision(precision string) pb.VectorPrecision {
	switch strings.ToUpper(precision) {
	case "FLOAT32":
		return pb.VectorPrecision_VECTOR_PRECISION_FLOAT32
	case "FLOAT16":
		return pb.VectorPrecision_VECTOR_PRECISION_FLOAT16
	case "BFLOAT16":
		return pb.VectorPrecision_VECTOR_PRECISION_BFLOAT16
	default:
		return pb.VectorPrecision_VECTOR_PRECISION_FLOAT32
	}
}

// parseIndexType converts string to proto enum.
func parseIndexType(indexType string) pb.IndexType {
	switch strings.ToUpper(indexType) {
	case "HNSW":
		return pb.IndexType_INDEX_TYPE_HNSW
	case "FLAT":
		return pb.IndexType_INDEX_TYPE_FLAT
	default:
		return pb.IndexType_INDEX_TYPE_HNSW
	}
}

// parseStringParam safely parses a string parameter with default.
func (s *MCPServer) parseStringParam(params map[string]interface{}, key, defaultValue string) string {
	if val, ok := params[key].(string); ok {
		return val
	}
	return defaultValue
}

// parseBoolParam safely parses a boolean parameter with default.
func (s *MCPServer) parseBoolParam(params map[string]interface{}, key string, defaultValue bool) bool {
	if val, ok := params[key].(string); ok {
		return val == "true"
	}
	if val, ok := params[key].(bool); ok {
		return val
	}
	return defaultValue
}

// parseIntParam parses an integer parameter, or returns defaultValue when it
// is absent or an empty string. A number an int cannot hold, or a string
// that is not a base-10 int, is refused as invalid input.
func (s *MCPServer) parseIntParam(params map[string]interface{}, key string, defaultValue int) (int, error) {
	switch v := params[key].(type) {
	case float64:
		n, err := conv.SafeFloat64ToInt(v)
		if err != nil {
			return 0, newIntArgError(key, err)
		}
		return n, nil
	case string:
		if v == "" {
			return defaultValue, nil
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, newIntArgError(key, err)
		}
		return n, nil
	}
	return defaultValue, nil
}

// parseInt32Param parses an integer parameter for an int32 request field,
// or returns defaultValue when it is absent. A value an int32 cannot hold is
// refused as invalid input.
func (s *MCPServer) parseInt32Param(params map[string]interface{}, key string, defaultValue int32) (int32, error) {
	n, err := s.parseIntParam(params, key, int(defaultValue))
	if err != nil {
		return 0, err
	}
	v, err := conv.SafeIntToInt32(n)
	if err != nil {
		return 0, newIntArgError(key, err)
	}
	return v, nil
}

// parseUint64Param parses an integer parameter for a uint64 request field;
// an absent parameter reads as zero. A negative value is refused as invalid
// input.
func (s *MCPServer) parseUint64Param(params map[string]interface{}, key string) (uint64, error) {
	n, err := s.parseIntParam(params, key, 0)
	if err != nil {
		return 0, err
	}
	v, err := conv.SafeIntToUint64(n)
	if err != nil {
		return 0, newIntArgError(key, err)
	}
	return v, nil
}

// parseFloatParam safely parses a float parameter with default.
func (s *MCPServer) parseFloatParam(params map[string]interface{}, key string, defaultValue float64) float64 {
	if val, ok := params[key].(float64); ok {
		return val
	}
	if val, ok := params[key].(string); ok {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return defaultValue
}

// jsonToolResponse creates a ToolResponse from JSON-serializable result.
func (s *MCPServer) jsonToolResponse(result map[string]interface{}) (*ToolResponse, error) {
	resultJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, api.NewMarshalError(err)
	}

	return &ToolResponse{
		Content: []ContentItem{
			{
				Type: "text",
				Text: string(resultJSON),
			},
		},
		IsError: false,
	}, nil
}
