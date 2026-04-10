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

package mcp

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// vectorHandler handles Vector-specific request/response transformations for MCP transport.
type vectorHandler struct {
	transport *Transport
}

// newVectorHandler creates a new Vector handler.
func newVectorHandler(t *Transport) *vectorHandler {
	return &vectorHandler{transport: t}
}

// transformRequest transforms SDK vector requests to MCP tool arguments format.
func (h *vectorHandler) transformRequest(method string, req interface{}) (map[string]interface{}, error) {
	switch method {
	case "CreateNamespace":
		return h.transformCreateNamespace(req)
	case "DeleteNamespace":
		return h.transformDeleteNamespace(req)
	case "ListNamespaces":
		return h.transformListNamespaces(req)
	case "GetNamespaceInfo":
		return h.transformGetNamespaceInfo(req)
	case "Upsert":
		return h.transformUpsert(req)
	case "Get":
		return h.transformGet(req)
	case "Delete":
		return h.transformDelete(req)
	case "BatchUpsert":
		return h.transformBatchUpsert(req)
	case "BatchDelete":
		return h.transformBatchDelete(req)
	case "SearchSimilar":
		return h.transformSearchSimilar(req)
	case "SearchByID":
		return h.transformSearchByID(req)
	case "HybridSearch":
		return h.transformHybridSearch(req)
	case "SearchByMetadata":
		return h.transformSearchByMetadata(req)
	case "RebuildIndex":
		return h.transformRebuildIndex(req)
	case "GetIndexProgress":
		return h.transformGetIndexProgress(req)
	case "Compact":
		return h.transformCompact(req)
	case "GetCompactionProgress":
		return h.transformGetCompactionProgress(req)
	case "List":
		return h.transformList(req)
	case "UpdateMetadata":
		return h.transformUpdateMetadata(req)
	case "GetStats":
		return h.transformGetStats(req)
	default:
		// Fall back to generic conversion
		return h.transport.convertToArguments(req), nil
	}
}

// transformResponse transforms MCP tool response to SDK vector types.
func (h *vectorHandler) transformResponse(method string, resp interface{}) (interface{}, error) {
	switch method {
	case "CreateNamespace":
		return h.transformCreateNamespaceResponse(resp)
	case "DeleteNamespace":
		return h.transformDeleteNamespaceResponse(resp)
	case "ListNamespaces":
		return h.transformListNamespacesResponse(resp)
	case "GetNamespaceInfo":
		return h.transformGetNamespaceInfoResponse(resp)
	case "Upsert":
		return h.transformUpsertResponse(resp)
	case "Get":
		return h.transformGetResponse(resp)
	case "Delete":
		return h.transformDeleteResponse(resp)
	case "BatchUpsert":
		return h.transformBatchUpsertResponse(resp)
	case "BatchDelete":
		return h.transformBatchDeleteResponse(resp)
	case "SearchSimilar":
		return h.transformSearchSimilarResponse(resp)
	case "SearchByID":
		return h.transformSearchByIDResponse(resp)
	case "HybridSearch":
		return h.transformHybridSearchResponse(resp)
	case "SearchByMetadata":
		return h.transformSearchByMetadataResponse(resp)
	case "RebuildIndex":
		return h.transformRebuildIndexResponse(resp)
	case "GetIndexProgress":
		return h.transformGetIndexProgressResponse(resp)
	case "Compact":
		return h.transformCompactResponse(resp)
	case "GetCompactionProgress":
		return h.transformGetCompactionProgressResponse(resp)
	case "List":
		return h.transformListResponse(resp)
	case "UpdateMetadata":
		return h.transformUpdateMetadataResponse(resp)
	case "GetStats":
		return h.transformGetStatsResponse(resp)
	default:
		return resp, nil
	}
}

// Request transformations

func (h *vectorHandler) transformCreateNamespace(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.CreateNamespaceRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformCreateNamespace",
			"invalid request type, expected *vector.CreateNamespaceRequest")
	}

	if sdkReq.Config == nil {
		return nil, errors.NewInvalidInput("vectorHandler.transformCreateNamespace",
			"config is required")
	}

	args := map[string]interface{}{
		"namespace":       sdkReq.Config.Namespace,
		"dimensions":      sdkReq.Config.Dimensions,
		"distance_metric": "cosine",
	}

	if sdkReq.Config.IndexType != "" {
		args["index_type"] = string(sdkReq.Config.IndexType)
	}
	if len(sdkReq.Config.IndexParams) > 0 {
		args["index_params"] = stringMapToInterface(sdkReq.Config.IndexParams)
	}
	if len(sdkReq.Config.Metadata) > 0 {
		args["metadata"] = stringMapToInterface(sdkReq.Config.Metadata)
	}

	return args, nil
}

func (h *vectorHandler) transformDeleteNamespace(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.DeleteNamespaceRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformDeleteNamespace",
			"invalid request type, expected *vector.DeleteNamespaceRequest")
	}

	return map[string]interface{}{
		"namespace": sdkReq.Namespace,
	}, nil
}

func (h *vectorHandler) transformListNamespaces(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.ListNamespacesRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformListNamespaces",
			"invalid request type, expected *vector.ListNamespacesRequest")
	}

	args := map[string]interface{}{}
	if sdkReq.Prefix != "" {
		args["prefix"] = sdkReq.Prefix
	}
	if sdkReq.PageSize > 0 {
		args["page_size"] = sdkReq.PageSize
	}
	if sdkReq.PageToken != "" {
		args["page_token"] = sdkReq.PageToken
	}

	return args, nil
}

func (h *vectorHandler) transformGetNamespaceInfo(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.GetNamespaceInfoRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformGetNamespaceInfo",
			"invalid request type, expected *vector.GetNamespaceInfoRequest")
	}

	return map[string]interface{}{
		"namespace": sdkReq.Namespace,
	}, nil
}

func (h *vectorHandler) transformUpsert(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.UpsertRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformUpsert",
			"invalid request type, expected *vector.UpsertRequest")
	}

	if sdkReq.Vector == nil {
		return nil, errors.NewInvalidInput("vectorHandler.transformUpsert",
			"vector is required")
	}

	args := map[string]interface{}{
		"namespace": sdkReq.Vector.Namespace,
		"id":        sdkReq.Vector.ID,
		"vector":    sdkReq.Vector.Vector,
	}
	if len(sdkReq.Vector.Metadata) > 0 {
		args["metadata"] = stringMapToInterface(sdkReq.Vector.Metadata)
	}

	return args, nil
}

func (h *vectorHandler) transformGet(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.GetRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformGet",
			"invalid request type, expected *vector.GetRequest")
	}

	return map[string]interface{}{
		"namespace":        sdkReq.Namespace,
		"id":               sdkReq.ID,
		"include_vector":   sdkReq.IncludeVector,
		"include_metadata": sdkReq.IncludeMetadata,
	}, nil
}

func (h *vectorHandler) transformDelete(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.DeleteRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformDelete",
			"invalid request type, expected *vector.DeleteRequest")
	}

	return map[string]interface{}{
		"namespace": sdkReq.Namespace,
		"id":        sdkReq.ID,
	}, nil
}

func (h *vectorHandler) transformBatchUpsert(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.BatchUpsertRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformBatchUpsert",
			"invalid request type, expected *vector.BatchUpsertRequest")
	}

	vectors := make([]map[string]interface{}, len(sdkReq.Vectors))
	for i, v := range sdkReq.Vectors {
		vectors[i] = map[string]interface{}{
			"namespace": v.Namespace,
			"id":        v.ID,
			"vector":    v.Vector,
		}
		if len(v.Metadata) > 0 {
			vectors[i]["metadata"] = stringMapToInterface(v.Metadata)
		}
	}

	return map[string]interface{}{
		"vectors": vectors,
	}, nil
}

func (h *vectorHandler) transformBatchDelete(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.BatchDeleteRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformBatchDelete",
			"invalid request type, expected *vector.BatchDeleteRequest")
	}

	// MCP server expects ids as a JSON-encoded string array
	idsJSON, err := json.Marshal(sdkReq.IDs)
	if err != nil {
		return nil, errors.NewInternal("vectorHandler.transformBatchDelete",
			"failed to marshal ids: "+err.Error())
	}

	return map[string]interface{}{
		"namespace": sdkReq.Namespace,
		"ids":       string(idsJSON),
	}, nil
}

func (h *vectorHandler) transformSearchSimilar(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.SearchSimilarRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformSearchSimilar",
			"invalid request type, expected *vector.SearchSimilarRequest")
	}

	args := map[string]interface{}{
		"namespace":        sdkReq.Namespace,
		"query_vector":     sdkReq.QueryVector,
		"k":                sdkReq.K,
		"include_vectors":  sdkReq.IncludeVectors,
		"include_metadata": sdkReq.IncludeMetadata,
	}
	if sdkReq.EfSearch > 0 {
		args["ef_search"] = sdkReq.EfSearch
	}
	if sdkReq.MinScore > 0 {
		args["min_score"] = sdkReq.MinScore
	}

	return args, nil
}

func (h *vectorHandler) transformSearchByID(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.SearchByIDRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformSearchByID",
			"invalid request type, expected *vector.SearchByIDRequest")
	}

	return map[string]interface{}{
		"namespace":        sdkReq.Namespace,
		"id":               sdkReq.ID,
		"k":                sdkReq.K,
		"include_vectors":  sdkReq.IncludeVectors,
		"include_metadata": sdkReq.IncludeMetadata,
	}, nil
}

func (h *vectorHandler) transformHybridSearch(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.HybridSearchRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformHybridSearch",
			"invalid request type, expected *vector.HybridSearchRequest")
	}

	args := map[string]interface{}{
		"namespace":        sdkReq.Namespace,
		"query_vector":     sdkReq.QueryVector,
		"k":                sdkReq.K,
		"include_vectors":  sdkReq.IncludeVectors,
		"include_metadata": sdkReq.IncludeMetadata,
	}

	if len(sdkReq.MetadataFilters) > 0 {
		filters := make([]map[string]interface{}, len(sdkReq.MetadataFilters))
		for i, f := range sdkReq.MetadataFilters {
			filters[i] = map[string]interface{}{
				"key":      f.Key,
				"operator": string(f.Operator),
				"value":    f.Value,
			}
		}
		args["metadata_filters"] = filters
	}

	return args, nil
}

func (h *vectorHandler) transformSearchByMetadata(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.SearchByMetadataRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformSearchByMetadata",
			"invalid request type, expected *vector.SearchByMetadataRequest")
	}

	args := map[string]interface{}{
		"namespace":        sdkReq.Namespace,
		"include_vectors":  sdkReq.IncludeVectors,
		"include_metadata": sdkReq.IncludeMetadata,
	}

	if len(sdkReq.MetadataFilters) > 0 {
		filters := make([]map[string]interface{}, len(sdkReq.MetadataFilters))
		for i, f := range sdkReq.MetadataFilters {
			filters[i] = map[string]interface{}{
				"key":      f.Key,
				"operator": string(f.Operator),
				"value":    f.Value,
			}
		}
		args["metadata_filters"] = filters
	}

	if sdkReq.Limit > 0 {
		args["limit"] = sdkReq.Limit
	}
	if sdkReq.Offset > 0 {
		args["offset"] = sdkReq.Offset
	}

	return args, nil
}

func (h *vectorHandler) transformRebuildIndex(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.RebuildIndexRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformRebuildIndex",
			"invalid request type, expected *vector.RebuildIndexRequest")
	}

	args := map[string]interface{}{
		"namespace": sdkReq.Namespace,
		"async":     sdkReq.Async,
	}
	if len(sdkReq.IndexParams) > 0 {
		args["index_params"] = stringMapToInterface(sdkReq.IndexParams)
	}

	return args, nil
}

func (h *vectorHandler) transformGetIndexProgress(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.GetIndexProgressRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformGetIndexProgress",
			"invalid request type, expected *vector.GetIndexProgressRequest")
	}

	return map[string]interface{}{
		"namespace":    sdkReq.Namespace,
		"operation_id": sdkReq.OperationID,
	}, nil
}

func (h *vectorHandler) transformCompact(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.CompactRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformCompact",
			"invalid request type, expected *vector.CompactRequest")
	}

	return map[string]interface{}{
		"namespace": sdkReq.Namespace,
		"async":     sdkReq.Async,
	}, nil
}

func (h *vectorHandler) transformGetCompactionProgress(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.GetCompactionProgressRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformGetCompactionProgress",
			"invalid request type, expected *vector.GetCompactionProgressRequest")
	}

	return map[string]interface{}{
		"namespace":    sdkReq.Namespace,
		"operation_id": sdkReq.OperationID,
	}, nil
}

func (h *vectorHandler) transformList(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.ListVectorsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformList",
			"invalid request type, expected *vector.ListVectorsRequest")
	}

	args := map[string]interface{}{
		"namespace":        sdkReq.Namespace,
		"include_vectors":  sdkReq.IncludeVectors,
		"include_metadata": sdkReq.IncludeMetadata,
	}
	if sdkReq.PageSize > 0 {
		args["page_size"] = sdkReq.PageSize
	}
	if sdkReq.PageToken != "" {
		args["page_token"] = sdkReq.PageToken
	}

	return args, nil
}

func (h *vectorHandler) transformUpdateMetadata(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.UpdateMetadataRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformUpdateMetadata",
			"invalid request type, expected *vector.UpdateMetadataRequest")
	}

	return map[string]interface{}{
		"namespace": sdkReq.Namespace,
		"id":        sdkReq.ID,
		"metadata":  stringMapToInterface(sdkReq.Metadata),
		"merge":     sdkReq.Merge,
	}, nil
}

func (h *vectorHandler) transformGetStats(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*vector.GetStatsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformGetStats",
			"invalid request type, expected *vector.GetStatsRequest")
	}

	return map[string]interface{}{
		"namespace": sdkReq.Namespace,
	}, nil
}

// Response transformations

func (h *vectorHandler) transformCreateNamespaceResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformCreateNamespaceResponse",
			"invalid response type")
	}

	return &vector.CreateNamespaceResponse{
		Namespace: mcpGetString(m, "namespace"),
		Success:   true,
		Timestamp: mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformDeleteNamespaceResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		// Try JSON round-trip for complex types
		jsonBytes, err := json.Marshal(resp)
		if err != nil {
			return nil, errors.NewInternal("vectorHandler.transformDeleteNamespaceResponse",
				"failed to marshal response: "+err.Error())
		}
		if err := json.Unmarshal(jsonBytes, &m); err != nil {
			return nil, errors.NewInternal("vectorHandler.transformDeleteNamespaceResponse",
				"failed to unmarshal response: "+err.Error())
		}
	}

	// Default to Deleted: true if we have a valid response with namespace
	// This mirrors the behavior of transformCreateNamespaceResponse which always sets Success: true
	deleted := true
	if v, exists := m["deleted"]; exists {
		if b, ok := v.(bool); ok {
			deleted = b
		}
	}

	return &vector.DeleteNamespaceResponse{
		Deleted:        deleted,
		VectorsDeleted: mcpGetInt64(m, "vectors_deleted"),
		Timestamp:      mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformListNamespacesResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return &vector.ListNamespacesResponse{Namespaces: []*vector.NamespaceInfo{}}, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformListNamespacesResponse",
			"invalid response type")
	}

	var namespaces []*vector.NamespaceInfo
	if nsArr, ok := m["namespaces"].([]interface{}); ok {
		for _, ns := range nsArr {
			if nsMap, ok := ns.(map[string]interface{}); ok {
				namespaces = append(namespaces, parseMCPNamespaceInfo(nsMap))
			}
		}
	}

	return &vector.ListNamespacesResponse{
		Namespaces:    namespaces,
		TotalCount:    mcpGetInt64(m, "total_count"),
		NextPageToken: mcpGetString(m, "next_page_token"),
		Timestamp:     mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformGetNamespaceInfoResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformGetNamespaceInfoResponse",
			"invalid response type")
	}

	return &vector.GetNamespaceInfoResponse{
		Info:      parseMCPNamespaceInfo(m),
		Timestamp: mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformUpsertResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformUpsertResponse",
			"invalid response type")
	}

	return &vector.UpsertResponse{
		ID:        mcpGetString(m, "id"),
		Version:   mcpGetInt64(m, "version"),
		Success:   true,
		Timestamp: mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformGetResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return &vector.GetResponse{Found: false}, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformGetResponse",
			"invalid response type")
	}

	return &vector.GetResponse{
		Vector:    parseMCPVectorEntity(m),
		Found:     true,
		Timestamp: mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformDeleteResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformDeleteResponse",
			"invalid response type")
	}

	return &vector.DeleteResponse{
		Deleted:   mcpGetBool(m, "deleted"),
		Timestamp: mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformBatchUpsertResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformBatchUpsertResponse",
			"invalid response type")
	}

	return &vector.BatchUpsertResponse{
		VectorsSucceeded: mcpGetInt64(m, "vectors_succeeded"),
		VectorsFailed:    mcpGetInt64(m, "vectors_failed"),
		Timestamp:        mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformBatchDeleteResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformBatchDeleteResponse",
			"invalid response type")
	}

	return &vector.BatchDeleteResponse{
		DeletedCount: mcpGetInt64(m, "deleted_count"),
		Timestamp:    mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformSearchSimilarResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return &vector.SearchSimilarResponse{Results: []*vector.SearchResult{}}, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformSearchSimilarResponse",
			"invalid response type")
	}

	return &vector.SearchSimilarResponse{
		Results:         parseMCPSearchResults(m),
		TotalCandidates: mcpGetInt64(m, "total_candidates"),
		SearchTimeMs:    mcpGetInt64(m, "search_time_ms"),
		Timestamp:       mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformSearchByIDResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return &vector.SearchByIDResponse{Results: []*vector.SearchResult{}}, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformSearchByIDResponse",
			"invalid response type")
	}

	return &vector.SearchByIDResponse{
		Results:         parseMCPSearchResults(m),
		TotalCandidates: mcpGetInt64(m, "total_candidates"),
		SearchTimeMs:    mcpGetInt64(m, "search_time_ms"),
		Timestamp:       mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformHybridSearchResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return &vector.HybridSearchResponse{Results: []*vector.SearchResult{}}, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformHybridSearchResponse",
			"invalid response type")
	}

	return &vector.HybridSearchResponse{
		Results:          parseMCPSearchResults(m),
		TotalCandidates:  mcpGetInt64(m, "total_candidates"),
		TotalFiltered:    mcpGetInt64(m, "total_filtered"),
		RerankingApplied: mcpGetBool(m, "reranking_applied"),
		SearchTimeMs:     mcpGetInt64(m, "search_time_ms"),
		Timestamp:        mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformSearchByMetadataResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return &vector.SearchByMetadataResponse{Results: []*vector.VectorEntity{}}, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformSearchByMetadataResponse",
			"invalid response type")
	}

	var results []*vector.VectorEntity
	if arr, ok := m["results"].([]interface{}); ok {
		for _, v := range arr {
			if vm, ok := v.(map[string]interface{}); ok {
				results = append(results, parseMCPVectorEntity(vm))
			}
		}
	}

	return &vector.SearchByMetadataResponse{
		Results:      results,
		TotalCount:   mcpGetInt64(m, "total_count"),
		NextCursor:   mcpGetString(m, "next_cursor"),
		SearchTimeMs: mcpGetInt64(m, "search_time_ms"),
		Timestamp:    mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformRebuildIndexResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformRebuildIndexResponse",
			"invalid response type")
	}

	return &vector.RebuildIndexResponse{
		OperationID:         mcpGetString(m, "operation_id"),
		Status:              mcpGetString(m, "status"),
		EstimatedDurationMs: mcpGetInt64(m, "estimated_duration_ms"),
		Started:             mcpGetTime(m, "started"),
		Timestamp:           mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformGetIndexProgressResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformGetIndexProgressResponse",
			"invalid response type")
	}

	return &vector.GetIndexProgressResponse{
		OperationID:          mcpGetString(m, "operation_id"),
		Namespace:            mcpGetString(m, "namespace"),
		Status:               mcpGetString(m, "status"),
		PercentComplete:      mcpGetFloat32(m, "percent_complete"),
		VectorsIndexed:       mcpGetInt64(m, "vectors_indexed"),
		TotalVectors:         mcpGetInt64(m, "total_vectors"),
		ElapsedTimeMs:        mcpGetInt64(m, "elapsed_time_ms"),
		EstimatedRemainingMs: mcpGetInt64(m, "estimated_remaining_ms"),
		ErrorMessage:         mcpGetString(m, "error_message"),
		Timestamp:            mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformCompactResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformCompactResponse",
			"invalid response type")
	}

	return &vector.CompactResponse{
		OperationID: mcpGetString(m, "operation_id"),
		Started:     mcpGetTime(m, "started"),
		Timestamp:   mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformGetCompactionProgressResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformGetCompactionProgressResponse",
			"invalid response type")
	}

	return &vector.GetCompactionProgressResponse{
		OperationID:         mcpGetString(m, "operation_id"),
		Namespace:           mcpGetString(m, "namespace"),
		Status:              mcpGetString(m, "status"),
		PercentComplete:     mcpGetFloat32(m, "percent_complete"),
		BytesCompacted:      mcpGetInt64(m, "bytes_compacted"),
		TotalBytes:          mcpGetInt64(m, "total_bytes"),
		SpaceReclaimedBytes: mcpGetInt64(m, "space_reclaimed_bytes"),
		ElapsedTimeMs:       mcpGetInt64(m, "elapsed_time_ms"),
		Timestamp:           mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformListResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return &vector.ListVectorsResponse{Vectors: []*vector.VectorEntity{}}, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformListResponse",
			"invalid response type")
	}

	var vectors []*vector.VectorEntity
	if arr, ok := m["vectors"].([]interface{}); ok {
		for _, v := range arr {
			if vm, ok := v.(map[string]interface{}); ok {
				vectors = append(vectors, parseMCPVectorEntity(vm))
			}
		}
	}

	return &vector.ListVectorsResponse{
		Vectors:       vectors,
		TotalCount:    mcpGetInt64(m, "total_count"),
		NextPageToken: mcpGetString(m, "next_page_token"),
		Timestamp:     mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformUpdateMetadataResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformUpdateMetadataResponse",
			"invalid response type")
	}

	return &vector.UpdateMetadataResponse{
		Success:   true,
		Version:   mcpGetInt64(m, "version"),
		Timestamp: mcpGetTime(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformGetStatsResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformGetStatsResponse",
			"invalid response type")
	}

	return &vector.GetStatsResponse{
		Namespace:        mcpGetString(m, "namespace"),
		VectorCount:      mcpGetInt64(m, "total_vectors"),
		IndexSizeBytes:   mcpGetInt64(m, "total_index_bytes"),
		StorageSizeBytes: mcpGetInt64(m, "total_storage_bytes"),
		Timestamp:        mcpGetTime(m, "timestamp"),
	}, nil
}

// Helper functions

func stringMapToInterface(m map[string]string) map[string]interface{} {
	if m == nil {
		return nil
	}
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

func interfaceToStringMap(v interface{}) map[string]string {
	result := make(map[string]string)
	if m, ok := v.(map[string]interface{}); ok {
		for k, val := range m {
			if s, ok := val.(string); ok {
				result[k] = s
			}
		}
	}
	return result
}

func mcpGetString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func mcpGetBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func mcpGetInt64(m map[string]interface{}, key string) int64 {
	if v, ok := m[key].(float64); ok {
		return int64(v)
	}
	return 0
}

func mcpGetFloat32(m map[string]interface{}, key string) float32 {
	if v, ok := m[key].(float64); ok {
		return float32(v)
	}
	return 0
}

func mcpGetTime(m map[string]interface{}, key string) time.Time {
	if v, ok := m[key].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t
		}
	}
	return time.Time{}
}

func parseMCPNamespaceInfo(m map[string]interface{}) *vector.NamespaceInfo {
	return &vector.NamespaceInfo{
		Config: &vector.NamespaceConfig{
			Namespace:   mcpGetString(m, "namespace"),
			Dimensions:  int32(mcpGetInt64(m, "dimensions")),
			IndexType:   vector.IndexType(mcpGetString(m, "index_type")),
			IndexParams: interfaceToStringMap(m["index_params"]),
			Metadata:    interfaceToStringMap(m["metadata"]),
		},
		VectorCount:  mcpGetInt64(m, "vector_count"),
		IndexBytes:   mcpGetInt64(m, "index_bytes"),
		StorageBytes: mcpGetInt64(m, "storage_bytes"),
		CreatedAt:    mcpGetTime(m, "created_at"),
		UpdatedAt:    mcpGetTime(m, "updated_at"),
	}
}

func parseMCPVectorEntity(m map[string]interface{}) *vector.VectorEntity {
	entity := &vector.VectorEntity{
		ID:        mcpGetString(m, "id"),
		Namespace: mcpGetString(m, "namespace"),
		Version:   mcpGetInt64(m, "version"),
		Metadata:  interfaceToStringMap(m["metadata"]),
		CreatedAt: mcpGetTime(m, "created_at"),
		UpdatedAt: mcpGetTime(m, "updated_at"),
	}

	if vec, ok := m["vector"].([]interface{}); ok {
		entity.Vector = make([]float32, len(vec))
		for i, v := range vec {
			if f, ok := v.(float64); ok {
				entity.Vector[i] = float32(f)
			}
		}
	}

	return entity
}

func parseMCPSearchResults(m map[string]interface{}) []*vector.SearchResult {
	var results []*vector.SearchResult
	if arr, ok := m["results"].([]interface{}); ok {
		for _, r := range arr {
			if rm, ok := r.(map[string]interface{}); ok {
				result := &vector.SearchResult{
					Score:    mcpGetFloat32(rm, "score"),
					Distance: mcpGetFloat32(rm, "distance"),
					Rank:     int32(mcpGetInt64(rm, "rank")),
				}
				if vm, ok := rm["vector"].(map[string]interface{}); ok {
					result.Vector = parseMCPVectorEntity(vm)
				}
				results = append(results, result)
			}
		}
	}
	return results
}

// isMCPVectorService returns true if the service is a Vector service.
func isMCPVectorService(service string) bool {
	serviceName := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		serviceName = service[idx+1:]
	}
	return serviceName == "Vector"
}
