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

package embedded

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
)

// =============================================================================
// Vector Handlers
// =============================================================================
//
// The Vector service is the re-export of go-qrdb's vector service. These
// embedded handlers delegate to the service-layer VectorService (backed by
// the embedded qrdb node through the go-qrdb sdk/go re-exports) and return
// the client/vector wire types the SDK vector client type-asserts on.
//
// Export and Import are not served: their batch codec lives in the gRPC
// server and VectorService has no export/import verb.

const vectorServiceName = "quicbaas.Vector"

// vectorRequest resolves the VectorService and type-asserts the SDK request.
func vectorRequest[T any](svc service.QuicBaaS, method string, req interface{}) (service.VectorService, *T, error) {
	vectorSvc := svc.Vector()
	if vectorSvc == nil {
		return nil, nil, NewServiceNotConfiguredError("Vector")
	}
	r, ok := req.(*T)
	if !ok || r == nil {
		return nil, nil, NewInvalidRequestTypeError(vectorServiceName, method,
			reflect.TypeOf((*T)(nil)).String(), typeNameOf(req))
	}
	return vectorSvc, r, nil
}

// parseVectorID converts an SDK vector id into the service uint64 id.
func parseVectorID(method, value string) (uint64, error) {
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, &InvalidVectorIDError{Service: vectorServiceName, Method: method, Value: value, Err: err}
	}
	return id, nil
}

func parseVectorIDs(method string, values []string) ([]uint64, error) {
	ids := make([]uint64, 0, len(values))
	for _, v := range values {
		id, err := parseVectorID(method, v)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func formatVectorID(id uint64) string {
	return strconv.FormatUint(id, 10)
}

// unixNanoTime converts a service nanosecond timestamp; zero stays zero.
func unixNanoTime(ns int64) time.Time {
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func toServiceMetadata(m map[string]string) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func fromServiceMetadata(m map[string]interface{}) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
			continue
		}
		out[k] = fmt.Sprint(v)
	}
	return out
}

// toServiceFilters converts SDK metadata filters into the service equality
// filter map. The service matches on equality only, so any other operator
// is refused rather than silently widened.
func toServiceFilters(method string, filters []*vector.MetadataFilter) (map[string]interface{}, error) {
	if len(filters) == 0 {
		return nil, nil
	}
	out := make(map[string]interface{}, len(filters))
	for _, f := range filters {
		if f == nil {
			continue
		}
		if f.Operator != vector.FilterOperatorEquals {
			return nil, &UnsupportedVectorFilterError{Service: vectorServiceName, Method: method, Key: f.Key, Operator: string(f.Operator)}
		}
		out[f.Key] = f.Value
	}
	return out, nil
}

func vectorDataToEntity(namespace string, d *service.VectorData) *vector.VectorEntity {
	return &vector.VectorEntity{
		ID:        formatVectorID(d.ID),
		Namespace: namespace,
		Vector:    d.Vector,
		Metadata:  fromServiceMetadata(d.Metadata),
		Version:   int64(d.Version),
		CreatedAt: unixNanoTime(d.CreatedAt),
		UpdatedAt: unixNanoTime(d.UpdatedAt),
	}
}

func vectorDataListToEntities(namespace string, data []*service.VectorData) []*vector.VectorEntity {
	out := make([]*vector.VectorEntity, 0, len(data))
	for _, d := range data {
		out = append(out, vectorDataToEntity(namespace, d))
	}
	return out
}

func searchResultsToSDK(namespace string, results []*service.VectorSearchResult) []*vector.SearchResult {
	out := make([]*vector.SearchResult, 0, len(results))
	for _, r := range results {
		out = append(out, &vector.SearchResult{
			Vector: &vector.VectorEntity{
				ID:        formatVectorID(r.ID),
				Namespace: namespace,
				Vector:    r.Vector,
				Metadata:  fromServiceMetadata(r.Metadata),
			},
			Score:    r.Score,
			Distance: r.Distance,
			Rank:     int32(r.Rank),
		})
	}
	return out
}

func namespaceInfoToSDK(n *service.NamespaceInfo) *vector.NamespaceInfo {
	return &vector.NamespaceInfo{
		Config: &vector.NamespaceConfig{
			Namespace:   n.Namespace,
			Dimensions:  int32(n.Dimension),
			IndexType:   vector.IndexType(strings.ToLower(n.IndexType)),
			IndexParams: n.IndexConfig,
			Metadata:    n.Metadata,
		},
		VectorCount: int64(n.VectorCount),
		CreatedAt:   unixNanoTime(n.CreatedAt),
		UpdatedAt:   unixNanoTime(n.UpdatedAt),
	}
}

// --- namespaces -------------------------------------------------------------

func handleVectorCreateNamespace(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.CreateNamespaceRequest](svc, "CreateNamespace", req)
	if err != nil {
		return nil, err
	}
	if r.Config == nil {
		return nil, NewInvalidRequestTypeError(vectorServiceName, "CreateNamespace", "*vector.NamespaceConfig", "<nil>")
	}
	resp, err := vs.CreateNamespace(ctx, &service.CreateNamespaceRequest{
		Namespace:   r.Config.Namespace,
		Dimension:   uint32(r.Config.Dimensions),
		IndexType:   strings.ToUpper(string(r.Config.IndexType)),
		IndexConfig: r.Config.IndexParams,
		Metadata:    toServiceMetadata(r.Config.Metadata),
	})
	if err != nil {
		return nil, err
	}
	return &vector.CreateNamespaceResponse{
		Namespace: resp.Namespace,
		Success:   resp.Success,
		Timestamp: unixNanoTime(resp.Timestamp),
	}, nil
}

func handleVectorDeleteNamespace(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.DeleteNamespaceRequest](svc, "DeleteNamespace", req)
	if err != nil {
		return nil, err
	}
	resp, err := vs.DeleteNamespace(ctx, &service.DeleteNamespaceRequest{Namespace: r.Namespace})
	if err != nil {
		return nil, err
	}
	return &vector.DeleteNamespaceResponse{
		Deleted:        resp.Success,
		VectorsDeleted: int64(resp.VectorsDeleted),
		Timestamp:      unixNanoTime(resp.Timestamp),
	}, nil
}

// handleVectorListNamespaces maps the SDK page token onto the service offset:
// the token is the decimal offset of the next page.
func handleVectorListNamespaces(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.ListNamespacesRequest](svc, "ListNamespaces", req)
	if err != nil {
		return nil, err
	}
	offset := 0
	if r.PageToken != "" {
		offset, err = strconv.Atoi(r.PageToken)
		if err != nil || offset < 0 {
			return nil, &InvalidPageTokenError{Service: vectorServiceName, Method: "ListNamespaces", Value: r.PageToken}
		}
	}
	resp, err := vs.ListNamespaces(ctx, &service.ListNamespacesRequest{
		Prefix: r.Prefix,
		Limit:  int(r.PageSize),
		Offset: offset,
	})
	if err != nil {
		return nil, err
	}
	out := &vector.ListNamespacesResponse{
		Namespaces: make([]*vector.NamespaceInfo, 0, len(resp.Namespaces)),
		TotalCount: int64(resp.TotalCount),
		Timestamp:  unixNanoTime(resp.Timestamp),
	}
	for _, n := range resp.Namespaces {
		out.Namespaces = append(out.Namespaces, namespaceInfoToSDK(n))
	}
	if next := offset + len(resp.Namespaces); r.PageSize > 0 && next < resp.TotalCount {
		out.NextPageToken = strconv.Itoa(next)
	}
	return out, nil
}

func handleVectorGetNamespaceInfo(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.GetNamespaceInfoRequest](svc, "GetNamespaceInfo", req)
	if err != nil {
		return nil, err
	}
	resp, err := vs.GetNamespaceInfo(ctx, &service.GetNamespaceInfoRequest{Namespace: r.Namespace})
	if err != nil {
		return nil, err
	}
	return &vector.GetNamespaceInfoResponse{
		Info: namespaceInfoToSDK(&service.NamespaceInfo{
			Namespace:      resp.Namespace,
			Dimension:      resp.Dimension,
			VectorCount:    resp.VectorCount,
			DistanceMetric: resp.DistanceMetric,
			IndexType:      resp.IndexType,
			IndexConfig:    resp.IndexConfig,
			Metadata:       fromServiceMetadata(resp.Metadata),
			CreatedAt:      resp.CreatedAt,
			UpdatedAt:      resp.UpdatedAt,
		}),
		Timestamp: time.Now(),
	}, nil
}

// --- vectors ----------------------------------------------------------------

func handleVectorUpsert(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.UpsertRequest](svc, "Upsert", req)
	if err != nil {
		return nil, err
	}
	if r.Vector == nil {
		return nil, NewInvalidRequestTypeError(vectorServiceName, "Upsert", "*vector.VectorEntity", "<nil>")
	}
	id, err := parseVectorID("Upsert", r.Vector.ID)
	if err != nil {
		return nil, err
	}
	resp, err := vs.UpsertVector(ctx, &service.UpsertVectorRequest{
		Namespace: r.Vector.Namespace,
		ID:        id,
		Vector:    r.Vector.Vector,
		Metadata:  toServiceMetadata(r.Vector.Metadata),
	})
	if err != nil {
		return nil, err
	}
	return &vector.UpsertResponse{
		ID:        formatVectorID(resp.ID),
		Version:   int64(resp.Version),
		Success:   resp.Success,
		Timestamp: unixNanoTime(resp.Timestamp),
	}, nil
}

// handleVectorBatchUpsert requires every vector to share one namespace, the
// unit the service batches on.
func handleVectorBatchUpsert(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.BatchUpsertRequest](svc, "BatchUpsert", req)
	if err != nil {
		return nil, err
	}
	namespace := ""
	data := make([]*service.VectorData, 0, len(r.Vectors))
	for _, v := range r.Vectors {
		if v == nil {
			continue
		}
		if namespace == "" {
			namespace = v.Namespace
		} else if v.Namespace != namespace {
			return nil, &MixedVectorNamespaceError{Service: vectorServiceName, Method: "BatchUpsert", First: namespace, Other: v.Namespace}
		}
		id, err := parseVectorID("BatchUpsert", v.ID)
		if err != nil {
			return nil, err
		}
		data = append(data, &service.VectorData{ID: id, Vector: v.Vector, Metadata: toServiceMetadata(v.Metadata)})
	}
	resp, err := vs.BatchUpsert(ctx, &service.BatchUpsertRequest{Namespace: namespace, Vectors: data})
	if err != nil {
		return nil, err
	}
	return &vector.BatchUpsertResponse{
		VectorsSucceeded: int64(resp.InsertedCount),
		VectorsFailed:    int64(len(data) - resp.InsertedCount),
		Timestamp:        unixNanoTime(resp.Timestamp),
	}, nil
}

func handleVectorGet(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.GetRequest](svc, "Get", req)
	if err != nil {
		return nil, err
	}
	id, err := parseVectorID("Get", r.ID)
	if err != nil {
		return nil, err
	}
	resp, err := vs.GetVector(ctx, &service.GetVectorRequest{
		Namespace:       r.Namespace,
		ID:              id,
		IncludeVector:   r.IncludeVector,
		IncludeMetadata: r.IncludeMetadata,
	})
	if err != nil {
		return nil, err
	}
	out := &vector.GetResponse{Found: resp.Found, Timestamp: time.Now()}
	if resp.Found {
		out.Vector = vectorDataToEntity(r.Namespace, &service.VectorData{
			ID:        resp.ID,
			Vector:    resp.Vector,
			Metadata:  resp.Metadata,
			Version:   resp.Version,
			CreatedAt: resp.CreatedAt,
			UpdatedAt: resp.UpdatedAt,
		})
	}
	return out, nil
}

func handleVectorDelete(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.DeleteRequest](svc, "Delete", req)
	if err != nil {
		return nil, err
	}
	id, err := parseVectorID("Delete", r.ID)
	if err != nil {
		return nil, err
	}
	resp, err := vs.DeleteVector(ctx, &service.DeleteVectorRequest{Namespace: r.Namespace, ID: id})
	if err != nil {
		return nil, err
	}
	return &vector.DeleteResponse{Deleted: resp.Success, Timestamp: unixNanoTime(resp.Timestamp)}, nil
}

func handleVectorBatchDelete(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.BatchDeleteRequest](svc, "BatchDelete", req)
	if err != nil {
		return nil, err
	}
	ids, err := parseVectorIDs("BatchDelete", r.IDs)
	if err != nil {
		return nil, err
	}
	resp, err := vs.BatchDeleteVectors(ctx, &service.BatchDeleteVectorsRequest{Namespace: r.Namespace, IDs: ids})
	if err != nil {
		return nil, err
	}
	out := &vector.BatchDeleteResponse{
		DeletedCount: int64(resp.DeletedCount),
		Timestamp:    unixNanoTime(resp.Timestamp),
	}
	if len(resp.FailedIDs) > 0 {
		out.Errors = make(map[string]string, len(resp.FailedIDs))
		for _, id := range resp.FailedIDs {
			out.Errors[formatVectorID(id)] = "delete failed"
		}
	}
	return out, nil
}

func handleVectorList(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.ListVectorsRequest](svc, "List", req)
	if err != nil {
		return nil, err
	}
	resp, err := vs.ListVectors(ctx, &service.ListVectorsRequest{
		Namespace:       r.Namespace,
		Limit:           int(r.PageSize),
		Cursor:          r.PageToken,
		IncludeVectors:  r.IncludeVectors,
		IncludeMetadata: r.IncludeMetadata,
	})
	if err != nil {
		return nil, err
	}
	return &vector.ListVectorsResponse{
		Vectors:       vectorDataListToEntities(r.Namespace, resp.Vectors),
		TotalCount:    int64(resp.TotalCount),
		NextPageToken: resp.NextCursor,
		Timestamp:     unixNanoTime(resp.Timestamp),
	}, nil
}

func handleVectorUpdateMetadata(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.UpdateMetadataRequest](svc, "UpdateMetadata", req)
	if err != nil {
		return nil, err
	}
	id, err := parseVectorID("UpdateMetadata", r.ID)
	if err != nil {
		return nil, err
	}
	resp, err := vs.UpdateMetadata(ctx, &service.UpdateVectorMetadataRequest{
		Namespace:     r.Namespace,
		ID:            id,
		Metadata:      toServiceMetadata(r.Metadata),
		MergeMetadata: r.Merge,
	})
	if err != nil {
		return nil, err
	}
	return &vector.UpdateMetadataResponse{
		Success:   resp.Success,
		Version:   int64(resp.Version),
		Timestamp: unixNanoTime(resp.Timestamp),
	}, nil
}

// --- search -----------------------------------------------------------------

func handleVectorSearchSimilar(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.SearchSimilarRequest](svc, "SearchSimilar", req)
	if err != nil {
		return nil, err
	}
	resp, err := vs.SearchSimilar(ctx, &service.SearchSimilarRequest{
		Namespace:       r.Namespace,
		QueryVector:     r.QueryVector,
		K:               int(r.K),
		EfSearch:        int(r.EfSearch),
		MinScore:        r.MinScore,
		IncludeVectors:  r.IncludeVectors,
		IncludeMetadata: r.IncludeMetadata,
	})
	if err != nil {
		return nil, err
	}
	return &vector.SearchSimilarResponse{
		Results:         searchResultsToSDK(r.Namespace, resp.Results),
		TotalCandidates: int64(len(resp.Results)),
		SearchTimeMs:    resp.QueryDuration / 1000,
		Timestamp:       unixNanoTime(resp.Timestamp),
	}, nil
}

// handleVectorSearchByID maps IncludeSelf onto the service's ExcludeSelf.
func handleVectorSearchByID(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.SearchByIDRequest](svc, "SearchByID", req)
	if err != nil {
		return nil, err
	}
	id, err := parseVectorID("SearchByID", r.ID)
	if err != nil {
		return nil, err
	}
	resp, err := vs.SearchByID(ctx, &service.SearchByIDRequest{
		Namespace:       r.Namespace,
		ID:              id,
		K:               int(r.K),
		EfSearch:        int(r.EfSearch),
		MinScore:        r.MinScore,
		IncludeVectors:  r.IncludeVectors,
		IncludeMetadata: r.IncludeMetadata,
		ExcludeSelf:     !r.IncludeSelf,
	})
	if err != nil {
		return nil, err
	}
	return &vector.SearchByIDResponse{
		Results:         searchResultsToSDK(r.Namespace, resp.Results),
		TotalCandidates: int64(len(resp.Results)),
		SearchTimeMs:    resp.QueryDuration / 1000,
		Timestamp:       unixNanoTime(resp.Timestamp),
	}, nil
}

func handleVectorHybridSearch(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.HybridSearchRequest](svc, "HybridSearch", req)
	if err != nil {
		return nil, err
	}
	filters, err := toServiceFilters("HybridSearch", r.MetadataFilters)
	if err != nil {
		return nil, err
	}
	resp, err := vs.HybridSearch(ctx, &service.HybridSearchRequest{
		Namespace:          r.Namespace,
		QueryVector:        r.QueryVector,
		K:                  int(r.K),
		EfSearch:           int(r.EfSearch),
		MinScore:           r.MinScore,
		MetadataFilter:     filters,
		FilterOperator:     r.FilterOperator,
		RerankingAlgorithm: r.RerankingAlgorithm,
		IncludeVectors:     r.IncludeVectors,
		IncludeMetadata:    r.IncludeMetadata,
	})
	if err != nil {
		return nil, err
	}
	return &vector.HybridSearchResponse{
		Results:          searchResultsToSDK(r.Namespace, resp.Results),
		TotalCandidates:  int64(resp.TotalCandidates),
		TotalFiltered:    int64(resp.TotalFiltered),
		RerankingApplied: r.RerankingAlgorithm != "",
		SearchTimeMs:     resp.QueryDuration / 1000,
		Timestamp:        unixNanoTime(resp.Timestamp),
	}, nil
}

func handleVectorSearchByMetadata(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.SearchByMetadataRequest](svc, "SearchByMetadata", req)
	if err != nil {
		return nil, err
	}
	filters, err := toServiceFilters("SearchByMetadata", r.MetadataFilters)
	if err != nil {
		return nil, err
	}
	resp, err := vs.SearchByMetadata(ctx, &service.SearchByMetadataRequest{
		Namespace:       r.Namespace,
		Filters:         filters,
		FilterOperator:  r.FilterOperator,
		Limit:           int(r.Limit),
		Offset:          int(r.Offset),
		IncludeVectors:  r.IncludeVectors,
		IncludeMetadata: r.IncludeMetadata,
	})
	if err != nil {
		return nil, err
	}
	return &vector.SearchByMetadataResponse{
		Results:    vectorDataListToEntities(r.Namespace, resp.Results),
		TotalCount: int64(resp.TotalCount),
		NextCursor: resp.NextCursor,
		Timestamp:  unixNanoTime(resp.Timestamp),
	}, nil
}

// --- admin ------------------------------------------------------------------

func handleVectorRebuildIndex(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.RebuildIndexRequest](svc, "RebuildIndex", req)
	if err != nil {
		return nil, err
	}
	resp, err := vs.RebuildIndex(ctx, &service.RebuildIndexRequest{
		Namespace:   r.Namespace,
		IndexConfig: r.IndexParams,
		Async:       r.Async,
	})
	if err != nil {
		return nil, err
	}
	ts := unixNanoTime(resp.Timestamp)
	return &vector.RebuildIndexResponse{
		OperationID:         resp.JobID,
		Status:              resp.Status,
		EstimatedDurationMs: resp.EstimatedTimeRemaining * 1000,
		Started:             ts,
		Timestamp:           ts,
	}, nil
}

func handleVectorGetIndexProgress(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.GetIndexProgressRequest](svc, "GetIndexProgress", req)
	if err != nil {
		return nil, err
	}
	resp, err := vs.GetRebuildProgress(ctx, &service.GetRebuildProgressRequest{JobID: r.OperationID, Namespace: r.Namespace})
	if err != nil {
		return nil, err
	}
	return &vector.GetIndexProgressResponse{
		OperationID:          resp.JobID,
		Namespace:            resp.Namespace,
		Status:               resp.Status,
		PercentComplete:      float32(resp.PercentComplete),
		VectorsIndexed:       int64(resp.VectorsProcessed),
		TotalVectors:         int64(resp.TotalVectors),
		ElapsedTimeMs:        resp.ElapsedTimeMs,
		EstimatedRemainingMs: resp.EstimatedTimeRemainingMs,
		ErrorMessage:         resp.Error,
		Timestamp:            time.Now(),
	}, nil
}

func handleVectorCompact(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.CompactRequest](svc, "Compact", req)
	if err != nil {
		return nil, err
	}
	resp, err := vs.CompactNamespace(ctx, &service.CompactNamespaceRequest{Namespace: r.Namespace, Async: r.Async})
	if err != nil {
		return nil, err
	}
	return &vector.CompactResponse{
		OperationID: resp.OperationID,
		Started:     unixNanoTime(resp.StartedAt),
		Timestamp:   time.Now(),
	}, nil
}

func handleVectorGetCompactionProgress(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.GetCompactionProgressRequest](svc, "GetCompactionProgress", req)
	if err != nil {
		return nil, err
	}
	resp, err := vs.GetCompactionProgress(ctx, &service.GetCompactionProgressRequest{Namespace: r.Namespace, OperationID: r.OperationID})
	if err != nil {
		return nil, err
	}
	status := "completed"
	switch {
	case resp.Error != "":
		status = "failed"
	case resp.InProgress:
		status = "running"
	}
	out := &vector.GetCompactionProgressResponse{
		OperationID:         resp.OperationID,
		Namespace:           resp.Namespace,
		Status:              status,
		PercentComplete:     float32(resp.PercentComplete),
		BytesCompacted:      resp.BytesCompacted,
		TotalBytes:          resp.TotalBytes,
		SpaceReclaimedBytes: resp.BytesCompacted,
		Timestamp:           time.Now(),
	}
	if resp.StartedAt != 0 {
		end := time.Now()
		if resp.CompletedAt != nil {
			end = unixNanoTime(*resp.CompletedAt)
		}
		out.ElapsedTimeMs = end.Sub(unixNanoTime(resp.StartedAt)).Milliseconds()
	}
	return out, nil
}

func handleVectorGetStats(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	vs, r, err := vectorRequest[vector.GetStatsRequest](svc, "GetStats", req)
	if err != nil {
		return nil, err
	}
	resp, err := vs.GetIndexStats(ctx, &service.GetIndexStatsRequest{Namespace: r.Namespace})
	if err != nil {
		return nil, err
	}
	return &vector.GetStatsResponse{
		Namespace:      r.Namespace,
		VectorCount:    int64(resp.VectorCount),
		IndexSizeBytes: int64(resp.IndexSizeBytes),
		Timestamp:      unixNanoTime(resp.Timestamp),
	}, nil
}
