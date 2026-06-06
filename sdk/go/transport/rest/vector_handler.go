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
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// vectorHandler handles Vector-specific request/response conversions for REST transport.
type vectorHandler struct {
	transport *Transport
}

// newVectorHandler creates a new Vector handler.
func newVectorHandler(t *Transport) *vectorHandler {
	return &vectorHandler{transport: t}
}

// handleRequest routes Vector requests to appropriate handlers.
func (h *vectorHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	switch method {
	case "CreateNamespace":
		return h.handleCreateNamespace(ctx, req)
	case "DeleteNamespace":
		return h.handleDeleteNamespace(ctx, req)
	case "ListNamespaces":
		return h.handleListNamespaces(ctx, req)
	case "GetNamespaceInfo":
		return h.handleGetNamespaceInfo(ctx, req)
	case "Upsert":
		return h.handleUpsert(ctx, req)
	case "Get":
		return h.handleGet(ctx, req)
	case "Delete":
		return h.handleDelete(ctx, req)
	case "BatchUpsert":
		return h.handleBatchUpsert(ctx, req)
	case "BatchDelete":
		return h.handleBatchDelete(ctx, req)
	case "SearchSimilar":
		return h.handleSearchSimilar(ctx, req)
	case "SearchByID":
		return h.handleSearchByID(ctx, req)
	case "HybridSearch":
		return h.handleHybridSearch(ctx, req)
	case "SearchByMetadata":
		return h.handleSearchByMetadata(ctx, req)
	case "RebuildIndex":
		return h.handleRebuildIndex(ctx, req)
	case "GetIndexProgress":
		return h.handleGetIndexProgress(ctx, req)
	case "Compact":
		return h.handleCompact(ctx, req)
	case "GetCompactionProgress":
		return h.handleGetCompactionProgress(ctx, req)
	case "List":
		return h.handleListVectors(ctx, req)
	case "UpdateMetadata":
		return h.handleUpdateMetadata(ctx, req)
	case "GetStats":
		return h.handleGetStats(ctx, req)
	default:
		// Fall back to generic handling for other methods
		return nil, errors.NewUnimplemented("vectorHandler.handleRequest",
			"unsupported Vector method: "+method)
	}
}

// handleCreateNamespace handles CreateNamespace requests.
func (h *vectorHandler) handleCreateNamespace(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.CreateNamespaceRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleCreateNamespace",
			"invalid request type, expected *vector.CreateNamespaceRequest")
	}

	if sdkReq.Config == nil {
		return nil, errors.NewInvalidInput("vectorHandler.handleCreateNamespace",
			"config is required")
	}

	// Build flattened request body matching server's CreateNamespaceRequest
	body := map[string]interface{}{
		"namespace":       sdkReq.Config.Namespace,
		"dimensions":      sdkReq.Config.Dimensions,
		"distance_metric": "cosine", // Default distance metric
	}

	if sdkReq.Config.IndexType != "" {
		body["index_type"] = string(sdkReq.Config.IndexType)
	}
	if len(sdkReq.Config.IndexParams) > 0 {
		body["index_params"] = sdkReq.Config.IndexParams
	}
	if len(sdkReq.Config.Metadata) > 0 {
		body["metadata"] = sdkReq.Config.Metadata
	}

	url := h.transport.baseURL + h.transport.basePath + "/api/v1/vector/namespaces"

	resp, err := h.doRequest(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}

	// Parse response
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.handleCreateNamespace",
			"invalid response type")
	}

	return &vector.CreateNamespaceResponse{
		Namespace: getString(respMap, "namespace"),
		Success:   true,
		Timestamp: vectorGetTime(respMap, "timestamp"),
	}, nil
}

// handleDeleteNamespace handles DeleteNamespace requests.
func (h *vectorHandler) handleDeleteNamespace(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.DeleteNamespaceRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleDeleteNamespace",
			"invalid request type, expected *vector.DeleteNamespaceRequest")
	}

	url := h.transport.baseURL + h.transport.basePath + "/api/v1/vector/namespaces/" + sdkReq.Namespace

	resp, err := h.doRequest(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}

	respMap, _ := resp.(map[string]interface{})

	return &vector.DeleteNamespaceResponse{
		Deleted:   vectorGetBool(respMap, "deleted"),
		Timestamp: vectorGetTime(respMap, "timestamp"),
	}, nil
}

// handleListNamespaces handles ListNamespaces requests.
func (h *vectorHandler) handleListNamespaces(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.ListNamespacesRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleListNamespaces",
			"invalid request type, expected *vector.ListNamespacesRequest")
	}

	url := h.transport.baseURL + h.transport.basePath + "/api/v1/vector/namespaces"

	// Add query params
	params := []string{}
	if sdkReq.Prefix != "" {
		params = append(params, "prefix="+sdkReq.Prefix)
	}
	if sdkReq.PageSize > 0 {
		params = append(params, "page_size="+vectorItoa(int(sdkReq.PageSize)))
	}
	if sdkReq.PageToken != "" {
		params = append(params, "page_token="+sdkReq.PageToken)
	}
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}

	resp, err := h.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return &vector.ListNamespacesResponse{Namespaces: []*vector.NamespaceInfo{}}, nil
	}

	// Parse namespaces
	var namespaces []*vector.NamespaceInfo
	if nsArr, ok := respMap["namespaces"].([]interface{}); ok {
		for _, ns := range nsArr {
			if nsMap, ok := ns.(map[string]interface{}); ok {
				namespaces = append(namespaces, parseNamespaceInfo(nsMap))
			}
		}
	}

	return &vector.ListNamespacesResponse{
		Namespaces:    namespaces,
		TotalCount:    vectorGetInt64(respMap, "total_count"),
		NextPageToken: getString(respMap, "next_page_token"),
		Timestamp:     vectorGetTime(respMap, "timestamp"),
	}, nil
}

// handleGetNamespaceInfo handles GetNamespaceInfo requests.
func (h *vectorHandler) handleGetNamespaceInfo(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.GetNamespaceInfoRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleGetNamespaceInfo",
			"invalid request type, expected *vector.GetNamespaceInfoRequest")
	}

	url := h.transport.baseURL + h.transport.basePath + "/api/v1/vector/namespaces/" + sdkReq.Namespace

	resp, err := h.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.handleGetNamespaceInfo",
			"invalid response type")
	}

	return &vector.GetNamespaceInfoResponse{
		Info:      parseNamespaceInfo(respMap),
		Timestamp: vectorGetTime(respMap, "timestamp"),
	}, nil
}

// handleUpsert handles Upsert requests.
func (h *vectorHandler) handleUpsert(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.UpsertRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleUpsert",
			"invalid request type, expected *vector.UpsertRequest")
	}

	if sdkReq.Vector == nil {
		return nil, errors.NewInvalidInput("vectorHandler.handleUpsert",
			"vector is required")
	}

	url := h.transport.baseURL + h.transport.basePath + "/api/v1/vector/namespaces/" +
		sdkReq.Vector.Namespace + "/vectors"

	body := map[string]interface{}{
		"id":     sdkReq.Vector.ID,
		"vector": sdkReq.Vector.Vector,
	}
	if len(sdkReq.Vector.Metadata) > 0 {
		body["metadata"] = sdkReq.Vector.Metadata
	}

	resp, err := h.doRequest(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}

	respMap, _ := resp.(map[string]interface{})

	return &vector.UpsertResponse{
		ID:        getString(respMap, "id"),
		Version:   vectorGetInt64(respMap, "version"),
		Success:   true,
		Timestamp: vectorGetTime(respMap, "timestamp"),
	}, nil
}

// handleGet handles Get requests.
func (h *vectorHandler) handleGet(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.GetRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleGet",
			"invalid request type, expected *vector.GetRequest")
	}

	url := h.transport.baseURL + h.transport.basePath + "/api/v1/vector/namespaces/" +
		sdkReq.Namespace + "/vectors/" + sdkReq.ID

	resp, err := h.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		// Check for not found
		if errors.Code(err) == errors.CodeNotFound {
			return &vector.GetResponse{Found: false}, nil
		}
		return nil, err
	}

	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return &vector.GetResponse{Found: false}, nil
	}

	return &vector.GetResponse{
		Vector:    parseVectorEntity(respMap),
		Found:     true,
		Timestamp: vectorGetTime(respMap, "timestamp"),
	}, nil
}

// handleDelete handles Delete requests.
func (h *vectorHandler) handleDelete(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.DeleteRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleDelete",
			"invalid request type, expected *vector.DeleteRequest")
	}

	url := h.transport.baseURL + h.transport.basePath + "/api/v1/vector/namespaces/" +
		sdkReq.Namespace + "/vectors/" + sdkReq.ID

	resp, err := h.doRequest(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}

	respMap, _ := resp.(map[string]interface{})

	return &vector.DeleteResponse{
		Deleted:   vectorGetBool(respMap, "deleted"),
		Timestamp: vectorGetTime(respMap, "timestamp"),
	}, nil
}

// handleBatchUpsert handles BatchUpsert requests.
func (h *vectorHandler) handleBatchUpsert(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.BatchUpsertRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleBatchUpsert",
			"invalid request type, expected *vector.BatchUpsertRequest")
	}

	if len(sdkReq.Vectors) == 0 {
		return nil, errors.NewInvalidInput("vectorHandler.handleBatchUpsert",
			"vectors are required")
	}

	// Get namespace from first vector
	namespace := sdkReq.Vectors[0].Namespace

	url := h.transport.baseURL + h.transport.basePath + "/api/v1/vector/namespaces/" +
		namespace + "/vectors/batch"

	vectors := make([]map[string]interface{}, len(sdkReq.Vectors))
	for i, v := range sdkReq.Vectors {
		vectors[i] = map[string]interface{}{
			"id":     v.ID,
			"vector": v.Vector,
		}
		if len(v.Metadata) > 0 {
			vectors[i]["metadata"] = v.Metadata
		}
	}

	body := map[string]interface{}{
		"vectors": vectors,
	}

	resp, err := h.doRequest(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}

	respMap, _ := resp.(map[string]interface{})

	return &vector.BatchUpsertResponse{
		VectorsSucceeded: vectorGetInt64(respMap, "vectors_succeeded"),
		VectorsFailed:    vectorGetInt64(respMap, "vectors_failed"),
		Timestamp:        vectorGetTime(respMap, "timestamp"),
	}, nil
}

// handleBatchDelete handles BatchDelete requests.
func (h *vectorHandler) handleBatchDelete(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.BatchDeleteRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleBatchDelete",
			"invalid request type, expected *vector.BatchDeleteRequest")
	}

	url := h.transport.baseURL + h.transport.basePath + "/api/v1/vector/namespaces/" +
		sdkReq.Namespace + "/vectors/batch"

	body := map[string]interface{}{
		"ids": sdkReq.IDs,
	}

	resp, err := h.doRequest(ctx, http.MethodDelete, url, body)
	if err != nil {
		return nil, err
	}

	respMap, _ := resp.(map[string]interface{})

	return &vector.BatchDeleteResponse{
		DeletedCount: vectorGetInt64(respMap, "deleted_count"),
		Timestamp:    vectorGetTime(respMap, "timestamp"),
	}, nil
}

// handleSearchSimilar handles SearchSimilar requests.
func (h *vectorHandler) handleSearchSimilar(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.SearchSimilarRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleSearchSimilar",
			"invalid request type, expected *vector.SearchSimilarRequest")
	}

	url := h.transport.baseURL + h.transport.basePath + "/api/v1/vector/namespaces/" +
		sdkReq.Namespace + "/search/similar"

	body := map[string]interface{}{
		"query_vector":     sdkReq.QueryVector,
		"k":                sdkReq.K,
		"include_vectors":  sdkReq.IncludeVectors,
		"include_metadata": sdkReq.IncludeMetadata,
	}
	if sdkReq.EfSearch > 0 {
		body["ef_search"] = sdkReq.EfSearch
	}
	if sdkReq.MinScore > 0 {
		body["min_score"] = sdkReq.MinScore
	}

	resp, err := h.doRequest(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}

	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return &vector.SearchSimilarResponse{Results: []*vector.SearchResult{}}, nil
	}

	return &vector.SearchSimilarResponse{
		Results:         parseSearchResults(respMap),
		TotalCandidates: vectorGetInt64(respMap, "total_candidates"),
		SearchTimeMs:    vectorGetInt64(respMap, "search_time_ms"),
		Timestamp:       vectorGetTime(respMap, "timestamp"),
	}, nil
}

// handleSearchByID handles SearchByID requests.
func (h *vectorHandler) handleSearchByID(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.SearchByIDRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleSearchByID",
			"invalid request type, expected *vector.SearchByIDRequest")
	}

	url := h.transport.baseURL + h.transport.basePath + "/api/v1/vector/namespaces/" +
		sdkReq.Namespace + "/search/by-id/" + sdkReq.ID

	body := map[string]interface{}{
		"k":                sdkReq.K,
		"include_vectors":  sdkReq.IncludeVectors,
		"include_metadata": sdkReq.IncludeMetadata,
	}

	resp, err := h.doRequest(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}

	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return &vector.SearchByIDResponse{Results: []*vector.SearchResult{}}, nil
	}

	return &vector.SearchByIDResponse{
		Results:         parseSearchResults(respMap),
		TotalCandidates: vectorGetInt64(respMap, "total_candidates"),
		SearchTimeMs:    vectorGetInt64(respMap, "search_time_ms"),
		Timestamp:       vectorGetTime(respMap, "timestamp"),
	}, nil
}

// handleHybridSearch handles HybridSearch requests.
func (h *vectorHandler) handleHybridSearch(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.HybridSearchRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleHybridSearch",
			"invalid request type, expected *vector.HybridSearchRequest")
	}

	url := h.transport.baseURL + h.transport.basePath + "/api/v1/vector/hybrid-search"

	body := map[string]interface{}{
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
		body["metadata_filters"] = filters
	}

	resp, err := h.doRequest(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}

	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return &vector.HybridSearchResponse{Results: []*vector.SearchResult{}}, nil
	}

	return &vector.HybridSearchResponse{
		Results:          parseSearchResults(respMap),
		TotalCandidates:  vectorGetInt64(respMap, "total_candidates"),
		TotalFiltered:    vectorGetInt64(respMap, "total_filtered"),
		RerankingApplied: vectorGetBool(respMap, "reranking_applied"),
		SearchTimeMs:     vectorGetInt64(respMap, "search_time_ms"),
		Timestamp:        vectorGetTime(respMap, "timestamp"),
	}, nil
}

// handleSearchByMetadata handles SearchByMetadata requests.
func (h *vectorHandler) handleSearchByMetadata(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.SearchByMetadataRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleSearchByMetadata",
			"invalid request type, expected *vector.SearchByMetadataRequest")
	}

	url := h.transport.baseURL + h.transport.basePath + "/api/v1/vector/search-by-metadata"

	body := map[string]interface{}{
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
		body["metadata_filters"] = filters
	}

	if sdkReq.Limit > 0 {
		body["limit"] = sdkReq.Limit
	}
	if sdkReq.Offset > 0 {
		body["offset"] = sdkReq.Offset
	}

	resp, err := h.doRequest(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}

	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return &vector.SearchByMetadataResponse{Results: []*vector.VectorEntity{}}, nil
	}

	var results []*vector.VectorEntity
	if arr, ok := respMap["results"].([]interface{}); ok {
		for _, v := range arr {
			if m, ok := v.(map[string]interface{}); ok {
				results = append(results, parseVectorEntity(m))
			}
		}
	}

	return &vector.SearchByMetadataResponse{
		Results:      results,
		TotalCount:   vectorGetInt64(respMap, "total_count"),
		NextCursor:   getString(respMap, "next_cursor"),
		SearchTimeMs: vectorGetInt64(respMap, "search_time_ms"),
		Timestamp:    vectorGetTime(respMap, "timestamp"),
	}, nil
}

// handleRebuildIndex handles RebuildIndex requests.
func (h *vectorHandler) handleRebuildIndex(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.RebuildIndexRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleRebuildIndex",
			"invalid request type, expected *vector.RebuildIndexRequest")
	}

	url := h.transport.baseURL + h.transport.basePath + "/api/v1/vector/namespaces/" +
		sdkReq.Namespace + "/index/rebuild"

	body := map[string]interface{}{}
	if len(sdkReq.IndexParams) > 0 {
		body["index_params"] = sdkReq.IndexParams
	}
	if sdkReq.Async {
		body["async"] = true
	}

	resp, err := h.doRequest(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}

	respMap, _ := resp.(map[string]interface{})

	return &vector.RebuildIndexResponse{
		OperationID:         getString(respMap, "operation_id"),
		Status:              getString(respMap, "status"),
		EstimatedDurationMs: vectorGetInt64(respMap, "estimated_duration_ms"),
		Started:             vectorGetTime(respMap, "started"),
		Timestamp:           vectorGetTime(respMap, "timestamp"),
	}, nil
}

// handleGetIndexProgress handles GetIndexProgress requests.
func (h *vectorHandler) handleGetIndexProgress(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.GetIndexProgressRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleGetIndexProgress",
			"invalid request type, expected *vector.GetIndexProgressRequest")
	}

	url := h.transport.baseURL + h.transport.basePath + "/api/v1/vector/namespaces/" +
		sdkReq.Namespace + "/index/progress"

	resp, err := h.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	respMap, _ := resp.(map[string]interface{})

	return &vector.GetIndexProgressResponse{
		OperationID:          getString(respMap, "operation_id"),
		Namespace:            sdkReq.Namespace,
		Status:               getString(respMap, "status"),
		PercentComplete:      vectorGetFloat32(respMap, "percent_complete"),
		VectorsIndexed:       vectorGetInt64(respMap, "vectors_indexed"),
		TotalVectors:         vectorGetInt64(respMap, "total_vectors"),
		ElapsedTimeMs:        vectorGetInt64(respMap, "elapsed_time_ms"),
		EstimatedRemainingMs: vectorGetInt64(respMap, "estimated_remaining_ms"),
		ErrorMessage:         getString(respMap, "error_message"),
		Timestamp:            vectorGetTime(respMap, "timestamp"),
	}, nil
}

// handleCompact handles Compact requests.
func (h *vectorHandler) handleCompact(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.CompactRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleCompact",
			"invalid request type, expected *vector.CompactRequest")
	}

	url := h.transport.baseURL + h.transport.basePath + "/api/v1/vector/admin/compact"

	body := map[string]interface{}{
		"namespace": sdkReq.Namespace,
	}
	if sdkReq.Async {
		body["async"] = true
	}

	resp, err := h.doRequest(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}

	respMap, _ := resp.(map[string]interface{})

	return &vector.CompactResponse{
		OperationID: getString(respMap, "operation_id"),
		Started:     vectorGetTime(respMap, "started"),
		Timestamp:   vectorGetTime(respMap, "timestamp"),
	}, nil
}

// handleGetCompactionProgress handles GetCompactionProgress requests.
func (h *vectorHandler) handleGetCompactionProgress(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.GetCompactionProgressRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleGetCompactionProgress",
			"invalid request type, expected *vector.GetCompactionProgressRequest")
	}

	url := h.transport.baseURL + h.transport.basePath + "/api/v1/vector/admin/compact/progress"

	resp, err := h.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	respMap, _ := resp.(map[string]interface{})

	return &vector.GetCompactionProgressResponse{
		OperationID:         getString(respMap, "operation_id"),
		Namespace:           sdkReq.Namespace,
		Status:              getString(respMap, "status"),
		PercentComplete:     vectorGetFloat32(respMap, "percent_complete"),
		BytesCompacted:      vectorGetInt64(respMap, "bytes_compacted"),
		TotalBytes:          vectorGetInt64(respMap, "total_bytes"),
		SpaceReclaimedBytes: vectorGetInt64(respMap, "space_reclaimed_bytes"),
		ElapsedTimeMs:       vectorGetInt64(respMap, "elapsed_time_ms"),
		Timestamp:           vectorGetTime(respMap, "timestamp"),
	}, nil
}

// handleListVectors handles List requests.
//
// Earlier revisions of this handler omitted the List, UpdateMetadata and
// GetStats methods entirely, so the SDK vector client received the
// "unsupported Vector method" error from the default branch rather than a
// real response. These handlers substitute the {namespace}/{id} path
// parameters from the typed SDK request and decode the RESTful wire responses.
func (h *vectorHandler) handleListVectors(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.ListVectorsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleListVectors",
			"invalid request type, expected *vector.ListVectorsRequest")
	}
	if sdkReq.Namespace == "" {
		return nil, errors.NewInvalidInput("vectorHandler.handleListVectors", "namespace is required")
	}

	endpoint := "/api/v1/vector/namespaces/" + sdkReq.Namespace + "/vectors"
	params := []string{}
	if sdkReq.PageSize > 0 {
		params = append(params, "page_size="+vectorItoa(int(sdkReq.PageSize)))
	}
	if sdkReq.PageToken != "" {
		params = append(params, "page_token="+sdkReq.PageToken)
	}
	if sdkReq.IncludeVectors {
		params = append(params, "include_vectors=true")
	}
	if sdkReq.IncludeMetadata {
		params = append(params, "include_metadata=true")
	}
	if len(params) > 0 {
		endpoint += "?" + strings.Join(params, "&")
	}

	var wire struct {
		Vectors []struct {
			ID        string            `json:"id"`
			Metadata  map[string]string `json:"metadata,omitempty"`
			Version   int64             `json:"version"`
			CreatedAt time.Time         `json:"created_at,omitempty"`
			UpdatedAt time.Time         `json:"updated_at,omitempty"`
		} `json:"vectors"`
		Pagination struct {
			NextPageToken string `json:"next_page_token"`
			TotalCount    int64  `json:"total_count,omitempty"`
		} `json:"pagination"`
	}
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &wire); err != nil {
		return nil, err
	}

	out := &vector.ListVectorsResponse{
		Vectors:       make([]*vector.VectorEntity, 0, len(wire.Vectors)),
		TotalCount:    wire.Pagination.TotalCount,
		NextPageToken: wire.Pagination.NextPageToken,
	}
	for _, v := range wire.Vectors {
		out.Vectors = append(out.Vectors, &vector.VectorEntity{
			ID:        v.ID,
			Namespace: sdkReq.Namespace,
			Version:   v.Version,
			Metadata:  v.Metadata,
			CreatedAt: v.CreatedAt,
			UpdatedAt: v.UpdatedAt,
		})
	}
	return out, nil
}

// handleUpdateMetadata handles UpdateMetadata requests.
func (h *vectorHandler) handleUpdateMetadata(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.UpdateMetadataRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleUpdateMetadata",
			"invalid request type, expected *vector.UpdateMetadataRequest")
	}
	if sdkReq.Namespace == "" {
		return nil, errors.NewInvalidInput("vectorHandler.handleUpdateMetadata", "namespace is required")
	}
	if sdkReq.ID == "" {
		return nil, errors.NewInvalidInput("vectorHandler.handleUpdateMetadata", "vector ID is required")
	}

	endpoint := "/api/v1/vector/namespaces/" + sdkReq.Namespace + "/vectors/" + sdkReq.ID + "/metadata"
	body := map[string]interface{}{"metadata": sdkReq.Metadata}

	var wire struct {
		ID        string    `json:"id"`
		Version   int64     `json:"version"`
		Timestamp time.Time `json:"timestamp"`
	}
	if err := h.transport.doJSONRequest(ctx, http.MethodPut, endpoint, body, &wire); err != nil {
		return nil, err
	}
	return &vector.UpdateMetadataResponse{
		Success:   true,
		Version:   wire.Version,
		Timestamp: wire.Timestamp,
	}, nil
}

// handleGetStats handles GetStats requests for a namespace.
func (h *vectorHandler) handleGetStats(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.GetStatsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleGetStats",
			"invalid request type, expected *vector.GetStatsRequest")
	}
	if sdkReq.Namespace == "" {
		return nil, errors.NewInvalidInput("vectorHandler.handleGetStats", "namespace is required")
	}

	endpoint := "/api/v1/vector/namespaces/" + sdkReq.Namespace + "/stats"
	var wire struct {
		Namespace    string `json:"namespace"`
		VectorCount  int64  `json:"vector_count"`
		StorageBytes int64  `json:"storage_bytes"`
		IndexBytes   int64  `json:"index_bytes"`
	}
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &wire); err != nil {
		return nil, err
	}
	return &vector.GetStatsResponse{
		Namespace:        sdkReq.Namespace,
		VectorCount:      wire.VectorCount,
		IndexSizeBytes:   wire.IndexBytes,
		StorageSizeBytes: wire.StorageBytes,
	}, nil
}

// doRequest executes an HTTP request.
func (h *vectorHandler) doRequest(ctx context.Context, method, url string, body interface{}) (interface{}, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, errors.Wrap(err, errors.CodeInvalidInput, "vectorHandler.doRequest",
				"failed to marshal request body")
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "vectorHandler.doRequest",
			"failed to create HTTP request")
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	h.transport.mu.RLock()
	for k, v := range h.transport.headers {
		httpReq.Header.Set(k, v)
	}
	h.transport.mu.RUnlock()

	resp, err := h.transport.client.Do(httpReq)
	if err != nil {
		return nil, h.transport.convertHTTPError(err, "vectorHandler.doRequest")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "vectorHandler.doRequest",
			"failed to read response body")
	}

	if resp.StatusCode >= 400 {
		return nil, h.transport.parseErrorResponse(resp.StatusCode, respBody)
	}

	var result interface{}
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal, "vectorHandler.doRequest",
				"failed to decode response")
		}
	}

	h.transport.lastHealth.Store(time.Now().UnixNano())

	return result, nil
}

// Helper functions for parsing responses.

func parseNamespaceInfo(m map[string]interface{}) *vector.NamespaceInfo {
	info := &vector.NamespaceInfo{
		Config: &vector.NamespaceConfig{
			Namespace:   getString(m, "namespace"),
			Dimensions:  int32(vectorGetInt64(m, "dimensions")),
			IndexType:   vector.IndexType(getString(m, "index_type")),
			IndexParams: vectorGetStringMap(m, "index_params"),
			Metadata:    vectorGetStringMap(m, "metadata"),
		},
		VectorCount:  vectorGetInt64(m, "vector_count"),
		IndexBytes:   vectorGetInt64(m, "index_bytes"),
		StorageBytes: vectorGetInt64(m, "storage_bytes"),
		CreatedAt:    vectorGetTime(m, "created_at"),
		UpdatedAt:    vectorGetTime(m, "updated_at"),
	}
	return info
}

func parseVectorEntity(m map[string]interface{}) *vector.VectorEntity {
	entity := &vector.VectorEntity{
		ID:        getString(m, "id"),
		Namespace: getString(m, "namespace"),
		Version:   vectorGetInt64(m, "version"),
		Metadata:  vectorGetStringMap(m, "metadata"),
		CreatedAt: vectorGetTime(m, "created_at"),
		UpdatedAt: vectorGetTime(m, "updated_at"),
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

func parseSearchResults(m map[string]interface{}) []*vector.SearchResult {
	var results []*vector.SearchResult
	if arr, ok := m["results"].([]interface{}); ok {
		for _, r := range arr {
			if rm, ok := r.(map[string]interface{}); ok {
				result := &vector.SearchResult{
					Score:    vectorGetFloat32(rm, "score"),
					Distance: vectorGetFloat32(rm, "distance"),
					Rank:     int32(vectorGetInt64(rm, "rank")),
				}
				if vm, ok := rm["vector"].(map[string]interface{}); ok {
					result.Vector = parseVectorEntity(vm)
				}
				results = append(results, result)
			}
		}
	}
	return results
}

func vectorGetBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func vectorGetInt64(m map[string]interface{}, key string) int64 {
	if v, ok := m[key].(float64); ok {
		return int64(v)
	}
	return 0
}

func vectorGetFloat32(m map[string]interface{}, key string) float32 {
	if v, ok := m[key].(float64); ok {
		return float32(v)
	}
	return 0
}

func vectorGetTime(m map[string]interface{}, key string) time.Time {
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

func vectorGetStringMap(m map[string]interface{}, key string) map[string]string {
	result := make(map[string]string)
	if v, ok := m[key].(map[string]interface{}); ok {
		for k, val := range v {
			if s, ok := val.(string); ok {
				result[k] = s
			}
		}
	}
	return result
}

func vectorItoa(i int) string {
	if i == 0 {
		return "0"
	}
	if i < 0 {
		return "-" + vectorItoa(-i)
	}
	result := ""
	for i > 0 {
		result = string('0'+byte(i%10)) + result
		i /= 10
	}
	return result
}

// isVectorService returns true if the service is a Vector service.
func isVectorService(service string) bool {
	serviceName := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		serviceName = service[idx+1:]
	}
	return serviceName == "Vector"
}
