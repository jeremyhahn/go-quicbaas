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

package quic

import (
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// vectorHandler handles Vector-specific request transformations for QUIC transport.
type vectorHandler struct {
	transport *Transport
}

// newVectorHandler creates a new Vector handler.
func newVectorHandler(t *Transport) *vectorHandler {
	return &vectorHandler{transport: t}
}

// transformRequest transforms SDK vector requests to server-side format.
func (h *vectorHandler) transformRequest(method string, req interface{}) (interface{}, error) {
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
	default:
		// Return as-is for unknown methods
		return req, nil
	}
}

// transformResponse transforms server responses back to SDK types.
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
	default:
		return resp, nil
	}
}

// Request transformations

func (h *vectorHandler) transformCreateNamespace(req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.CreateNamespaceRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformCreateNamespace",
			"invalid request type, expected *vector.CreateNamespaceRequest")
	}

	if sdkReq.Config == nil {
		return nil, errors.NewInvalidInput("vectorHandler.transformCreateNamespace",
			"config is required")
	}

	// Convert to server-side flat structure (service.CreateNamespaceRequest)
	return map[string]interface{}{
		"namespace":       sdkReq.Config.Namespace,
		"dimension":       sdkReq.Config.Dimensions,
		"distance_metric": "cosine",
		"index_type":      string(sdkReq.Config.IndexType),
		"index_config":    sdkReq.Config.IndexParams,
		"metadata":        stringMapToInterfaceMap(sdkReq.Config.Metadata),
	}, nil
}

func (h *vectorHandler) transformDeleteNamespace(req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.DeleteNamespaceRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformDeleteNamespace",
			"invalid request type, expected *vector.DeleteNamespaceRequest")
	}

	return map[string]interface{}{
		"namespace": sdkReq.Namespace,
	}, nil
}

func (h *vectorHandler) transformListNamespaces(req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.ListNamespacesRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformListNamespaces",
			"invalid request type, expected *vector.ListNamespacesRequest")
	}

	return map[string]interface{}{
		"prefix":     sdkReq.Prefix,
		"page_size":  sdkReq.PageSize,
		"page_token": sdkReq.PageToken,
	}, nil
}

func (h *vectorHandler) transformGetNamespaceInfo(req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.GetNamespaceInfoRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformGetNamespaceInfo",
			"invalid request type, expected *vector.GetNamespaceInfoRequest")
	}

	return map[string]interface{}{
		"namespace": sdkReq.Namespace,
	}, nil
}

func (h *vectorHandler) transformUpsert(req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.UpsertRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformUpsert",
			"invalid request type, expected *vector.UpsertRequest")
	}

	if sdkReq.Vector == nil {
		return nil, errors.NewInvalidInput("vectorHandler.transformUpsert",
			"vector is required")
	}

	return map[string]interface{}{
		"namespace": sdkReq.Vector.Namespace,
		"id":        sdkReq.Vector.ID,
		"vector":    sdkReq.Vector.Vector,
		"metadata":  stringMapToInterfaceMap(sdkReq.Vector.Metadata),
	}, nil
}

func (h *vectorHandler) transformGet(req interface{}) (interface{}, error) {
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

func (h *vectorHandler) transformDelete(req interface{}) (interface{}, error) {
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

func (h *vectorHandler) transformBatchUpsert(req interface{}) (interface{}, error) {
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
			"metadata":  stringMapToInterfaceMap(v.Metadata),
		}
	}

	return map[string]interface{}{
		"vectors": vectors,
	}, nil
}

func (h *vectorHandler) transformBatchDelete(req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.BatchDeleteRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformBatchDelete",
			"invalid request type, expected *vector.BatchDeleteRequest")
	}

	return map[string]interface{}{
		"namespace": sdkReq.Namespace,
		"ids":       sdkReq.IDs,
	}, nil
}

func (h *vectorHandler) transformSearchSimilar(req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.SearchSimilarRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformSearchSimilar",
			"invalid request type, expected *vector.SearchSimilarRequest")
	}

	return map[string]interface{}{
		"namespace":        sdkReq.Namespace,
		"query_vector":     sdkReq.QueryVector,
		"k":                sdkReq.K,
		"ef_search":        sdkReq.EfSearch,
		"min_score":        sdkReq.MinScore,
		"include_vectors":  sdkReq.IncludeVectors,
		"include_metadata": sdkReq.IncludeMetadata,
	}, nil
}

func (h *vectorHandler) transformSearchByID(req interface{}) (interface{}, error) {
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

func (h *vectorHandler) transformHybridSearch(req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.HybridSearchRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformHybridSearch",
			"invalid request type, expected *vector.HybridSearchRequest")
	}

	filters := make([]map[string]interface{}, len(sdkReq.MetadataFilters))
	for i, f := range sdkReq.MetadataFilters {
		filters[i] = map[string]interface{}{
			"key":      f.Key,
			"operator": string(f.Operator),
			"value":    f.Value,
		}
	}

	return map[string]interface{}{
		"namespace":        sdkReq.Namespace,
		"query_vector":     sdkReq.QueryVector,
		"k":                sdkReq.K,
		"metadata_filters": filters,
		"include_vectors":  sdkReq.IncludeVectors,
		"include_metadata": sdkReq.IncludeMetadata,
	}, nil
}

func (h *vectorHandler) transformSearchByMetadata(req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.SearchByMetadataRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformSearchByMetadata",
			"invalid request type, expected *vector.SearchByMetadataRequest")
	}

	filters := make([]map[string]interface{}, len(sdkReq.MetadataFilters))
	for i, f := range sdkReq.MetadataFilters {
		filters[i] = map[string]interface{}{
			"key":      f.Key,
			"operator": string(f.Operator),
			"value":    f.Value,
		}
	}

	return map[string]interface{}{
		"namespace":        sdkReq.Namespace,
		"metadata_filters": filters,
		"limit":            sdkReq.Limit,
		"offset":           sdkReq.Offset,
		"include_vectors":  sdkReq.IncludeVectors,
		"include_metadata": sdkReq.IncludeMetadata,
	}, nil
}

func (h *vectorHandler) transformRebuildIndex(req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.RebuildIndexRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.transformRebuildIndex",
			"invalid request type, expected *vector.RebuildIndexRequest")
	}

	return map[string]interface{}{
		"namespace":    sdkReq.Namespace,
		"index_params": sdkReq.IndexParams,
		"async":        sdkReq.Async,
	}, nil
}

func (h *vectorHandler) transformGetIndexProgress(req interface{}) (interface{}, error) {
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

func (h *vectorHandler) transformCompact(req interface{}) (interface{}, error) {
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

func (h *vectorHandler) transformGetCompactionProgress(req interface{}) (interface{}, error) {
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
		Namespace: vectorGetString(m, "namespace"),
		Success:   true,
		Timestamp: vectorGetTimeField(m, "timestamp"),
	}, nil
}

func (h *vectorHandler) transformDeleteNamespaceResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("vectorHandler.transformDeleteNamespaceResponse",
			"invalid response type")
	}

	return &vector.DeleteNamespaceResponse{
		Deleted:   vectorGetBoolField(m, "deleted"),
		Timestamp: vectorGetTimeField(m, "timestamp"),
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
				namespaces = append(namespaces, parseVectorNamespaceInfo(nsMap))
			}
		}
	}

	return &vector.ListNamespacesResponse{
		Namespaces:    namespaces,
		TotalCount:    vectorGetInt64Field(m, "total_count"),
		NextPageToken: vectorGetString(m, "next_page_token"),
		Timestamp:     vectorGetTimeField(m, "timestamp"),
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
		Info:      parseVectorNamespaceInfo(m),
		Timestamp: vectorGetTimeField(m, "timestamp"),
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
		ID:        vectorGetString(m, "id"),
		Version:   vectorGetInt64Field(m, "version"),
		Success:   true,
		Timestamp: vectorGetTimeField(m, "timestamp"),
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
		Vector:    parseVectorVectorEntity(m),
		Found:     true,
		Timestamp: vectorGetTimeField(m, "timestamp"),
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
		Deleted:   vectorGetBoolField(m, "deleted"),
		Timestamp: vectorGetTimeField(m, "timestamp"),
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
		VectorsSucceeded: vectorGetInt64Field(m, "vectors_succeeded"),
		VectorsFailed:    vectorGetInt64Field(m, "vectors_failed"),
		Timestamp:        vectorGetTimeField(m, "timestamp"),
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
		DeletedCount: vectorGetInt64Field(m, "deleted_count"),
		Timestamp:    vectorGetTimeField(m, "timestamp"),
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
		Results:         parseVectorSearchResults(m),
		TotalCandidates: vectorGetInt64Field(m, "total_candidates"),
		SearchTimeMs:    vectorGetInt64Field(m, "search_time_ms"),
		Timestamp:       vectorGetTimeField(m, "timestamp"),
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
		Results:         parseVectorSearchResults(m),
		TotalCandidates: vectorGetInt64Field(m, "total_candidates"),
		SearchTimeMs:    vectorGetInt64Field(m, "search_time_ms"),
		Timestamp:       vectorGetTimeField(m, "timestamp"),
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
		Results:          parseVectorSearchResults(m),
		TotalCandidates:  vectorGetInt64Field(m, "total_candidates"),
		TotalFiltered:    vectorGetInt64Field(m, "total_filtered"),
		RerankingApplied: vectorGetBoolField(m, "reranking_applied"),
		SearchTimeMs:     vectorGetInt64Field(m, "search_time_ms"),
		Timestamp:        vectorGetTimeField(m, "timestamp"),
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
				results = append(results, parseVectorVectorEntity(vm))
			}
		}
	}

	return &vector.SearchByMetadataResponse{
		Results:      results,
		TotalCount:   vectorGetInt64Field(m, "total_count"),
		NextCursor:   vectorGetString(m, "next_cursor"),
		SearchTimeMs: vectorGetInt64Field(m, "search_time_ms"),
		Timestamp:    vectorGetTimeField(m, "timestamp"),
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
		OperationID:         vectorGetString(m, "operation_id"),
		Status:              vectorGetString(m, "status"),
		EstimatedDurationMs: vectorGetInt64Field(m, "estimated_duration_ms"),
		Started:             vectorGetTimeField(m, "started"),
		Timestamp:           vectorGetTimeField(m, "timestamp"),
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
		OperationID:          vectorGetString(m, "operation_id"),
		Namespace:            vectorGetString(m, "namespace"),
		Status:               vectorGetString(m, "status"),
		PercentComplete:      vectorGetFloat32Field(m, "percent_complete"),
		VectorsIndexed:       vectorGetInt64Field(m, "vectors_indexed"),
		TotalVectors:         vectorGetInt64Field(m, "total_vectors"),
		ElapsedTimeMs:        vectorGetInt64Field(m, "elapsed_time_ms"),
		EstimatedRemainingMs: vectorGetInt64Field(m, "estimated_remaining_ms"),
		ErrorMessage:         vectorGetString(m, "error_message"),
		Timestamp:            vectorGetTimeField(m, "timestamp"),
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
		OperationID: vectorGetString(m, "operation_id"),
		Started:     vectorGetTimeField(m, "started"),
		Timestamp:   vectorGetTimeField(m, "timestamp"),
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
		OperationID:         vectorGetString(m, "operation_id"),
		Namespace:           vectorGetString(m, "namespace"),
		Status:              vectorGetString(m, "status"),
		PercentComplete:     vectorGetFloat32Field(m, "percent_complete"),
		BytesCompacted:      vectorGetInt64Field(m, "bytes_compacted"),
		TotalBytes:          vectorGetInt64Field(m, "total_bytes"),
		SpaceReclaimedBytes: vectorGetInt64Field(m, "space_reclaimed_bytes"),
		ElapsedTimeMs:       vectorGetInt64Field(m, "elapsed_time_ms"),
		Timestamp:           vectorGetTimeField(m, "timestamp"),
	}, nil
}

// Helper functions

func stringMapToInterfaceMap(m map[string]string) map[string]interface{} {
	if m == nil {
		return nil
	}
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

func vectorGetString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func vectorGetBoolField(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func vectorGetInt64Field(m map[string]interface{}, key string) int64 {
	if v, ok := m[key].(float64); ok {
		return int64(v)
	}
	return 0
}

func vectorGetFloat32Field(m map[string]interface{}, key string) float32 {
	if v, ok := m[key].(float64); ok {
		return float32(v)
	}
	return 0
}

func vectorGetTimeField(m map[string]interface{}, key string) time.Time {
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

func parseVectorNamespaceInfo(m map[string]interface{}) *vector.NamespaceInfo {
	return &vector.NamespaceInfo{
		Config: &vector.NamespaceConfig{
			Namespace:   vectorGetString(m, "namespace"),
			Dimensions:  int32(vectorGetInt64Field(m, "dimension")),
			IndexType:   vector.IndexType(vectorGetString(m, "index_type")),
			IndexParams: interfaceMapToStringMap(m["index_config"]),
			Metadata:    interfaceMapToStringMap(m["metadata"]),
		},
		VectorCount:  vectorGetInt64Field(m, "vector_count"),
		IndexBytes:   vectorGetInt64Field(m, "index_bytes"),
		StorageBytes: vectorGetInt64Field(m, "storage_bytes"),
		CreatedAt:    vectorGetTimeField(m, "created_at"),
		UpdatedAt:    vectorGetTimeField(m, "updated_at"),
	}
}

func parseVectorVectorEntity(m map[string]interface{}) *vector.VectorEntity {
	entity := &vector.VectorEntity{
		ID:        vectorGetString(m, "id"),
		Namespace: vectorGetString(m, "namespace"),
		Version:   vectorGetInt64Field(m, "version"),
		Metadata:  interfaceMapToStringMap(m["metadata"]),
		CreatedAt: vectorGetTimeField(m, "created_at"),
		UpdatedAt: vectorGetTimeField(m, "updated_at"),
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

func parseVectorSearchResults(m map[string]interface{}) []*vector.SearchResult {
	var results []*vector.SearchResult
	if arr, ok := m["results"].([]interface{}); ok {
		for _, r := range arr {
			if rm, ok := r.(map[string]interface{}); ok {
				result := &vector.SearchResult{
					Score:    vectorGetFloat32Field(rm, "score"),
					Distance: vectorGetFloat32Field(rm, "distance"),
					Rank:     int32(vectorGetInt64Field(rm, "rank")),
				}
				if vm, ok := rm["vector"].(map[string]interface{}); ok {
					result.Vector = parseVectorVectorEntity(vm)
				}
				results = append(results, result)
			}
		}
	}
	return results
}

func interfaceMapToStringMap(v interface{}) map[string]string {
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

// isVectorService returns true if the service is a Vector service.
func isVectorService(service string) bool {
	return service == "quicbaas.Vector" || service == "Vector"
}
