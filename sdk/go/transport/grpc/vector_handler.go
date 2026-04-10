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

package grpc

import (
	"context"
	"io"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// Vector service prefix for routing.
const vectorServicePrefix = "quicbaas.Vector"

// isVectorService checks if the service name is a Vector service.
func isVectorService(svc string) bool {
	return strings.HasPrefix(svc, vectorServicePrefix)
}

// vectorHandler handles Vector service requests by converting between SDK types and protobuf types.
type vectorHandler struct {
	conn *grpc.ClientConn
}

// newVectorHandler creates a new vector handler.
func newVectorHandler(conn *grpc.ClientConn) *vectorHandler {
	return &vectorHandler{conn: conn}
}

// handleRequest routes Vector requests to the appropriate handler based on method name.
func (h *vectorHandler) handleRequest(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	vectorClient := pb.NewVectorDBClient(h.conn)
	adminClient := pb.NewVectorAdminClient(h.conn)

	// Route based on method name using map-based dispatch for O(1) lookup.
	handler, ok := vectorHandlers[method]
	if !ok {
		return nil, errors.NewUnimplemented("vectorHandler.handleRequest",
			"unsupported method: "+method)
	}

	return handler(ctx, h, vectorClient, adminClient, req)
}

// vectorHandlerFunc defines the signature for vector method handlers.
type vectorHandlerFunc func(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error)

// vectorHandlers maps method names to their handler functions.
var vectorHandlers = map[string]vectorHandlerFunc{
	// Namespace Management
	"CreateNamespace":  handleCreateNamespace,
	"DeleteNamespace":  handleDeleteNamespace,
	"ListNamespaces":   handleListNamespaces,
	"GetNamespaceInfo": handleGetNamespaceInfo,

	// Vector CRUD
	"Upsert":         handleUpsert,
	"BatchUpsert":    handleBatchUpsert,
	"Get":            handleGet,
	"Delete":         handleDelete,
	"BatchDelete":    handleBatchDelete,
	"List":           handleList,
	"UpdateMetadata": handleUpdateMetadata,

	// Search Operations
	"SearchSimilar":    handleSearchSimilar,
	"SearchByID":       handleSearchByID,
	"HybridSearch":     handleHybridSearch,
	"SearchByMetadata": handleSearchByMetadata,

	// Index Management
	"RebuildIndex":     handleRebuildIndex,
	"GetIndexProgress": handleGetIndexProgress,

	// Admin Operations
	"Export":                handleExport,
	"Import":                handleImport,
	"Compact":               handleCompact,
	"GetCompactionProgress": handleGetCompactionProgress,
	"GetStats":              handleGetStats,
}

// =============================================================================
// Namespace Management Handlers
// =============================================================================

func handleCreateNamespace(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.CreateNamespaceRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleCreateNamespace", "invalid request type, expected *vector.CreateNamespaceRequest")
	}

	pbReq := &pb.CreateNamespaceRequest{
		Config: convertNamespaceConfigToProto(sdkReq.Config),
	}

	pbResp, err := vc.CreateNamespace(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleCreateNamespace")
	}

	return &vector.CreateNamespaceResponse{
		Namespace: pbResp.Namespace,
		Success:   true,
		Timestamp: convertTimestampFromProto(pbResp.Timestamp),
	}, nil
}

func handleDeleteNamespace(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.DeleteNamespaceRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleDeleteNamespace", "invalid request type, expected *vector.DeleteNamespaceRequest")
	}

	pbReq := &pb.DeleteNamespaceRequest{
		Namespace: sdkReq.Namespace,
	}

	pbResp, err := vc.DeleteNamespace(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleDeleteNamespace")
	}

	return &vector.DeleteNamespaceResponse{
		Deleted:        pbResp.Deleted,
		VectorsDeleted: pbResp.VectorsDeleted,
		Timestamp:      convertTimestampFromProto(pbResp.Timestamp),
	}, nil
}

func handleListNamespaces(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.ListNamespacesRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleListNamespaces", "invalid request type, expected *vector.ListNamespacesRequest")
	}

	pbReq := &pb.ListNamespacesRequest{
		Prefix:    sdkReq.Prefix,
		PageSize:  sdkReq.PageSize,
		PageToken: sdkReq.PageToken,
	}

	pbResp, err := vc.ListNamespaces(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleListNamespaces")
	}

	namespaces := make([]*vector.NamespaceInfo, len(pbResp.Namespaces))
	for i, ns := range pbResp.Namespaces {
		namespaces[i] = convertNamespaceInfoFromProto(ns)
	}

	return &vector.ListNamespacesResponse{
		Namespaces:    namespaces,
		TotalCount:    int64(pbResp.TotalCount),
		NextPageToken: pbResp.NextPageToken,
		Timestamp:     time.Now(),
	}, nil
}

func handleGetNamespaceInfo(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.GetNamespaceInfoRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleGetNamespaceInfo", "invalid request type, expected *vector.GetNamespaceInfoRequest")
	}

	pbReq := &pb.GetNamespaceInfoRequest{
		Namespace:    sdkReq.Namespace,
		IncludeStats: sdkReq.IncludeStats,
	}

	pbResp, err := vc.GetNamespaceInfo(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleGetNamespaceInfo")
	}

	return &vector.GetNamespaceInfoResponse{
		Info:      convertNamespaceInfoFromProto(pbResp.Info),
		Timestamp: time.Now(),
	}, nil
}

// =============================================================================
// Vector CRUD Handlers
// =============================================================================

func handleUpsert(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.UpsertRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleUpsert", "invalid request type, expected *vector.UpsertRequest")
	}

	pbReq := &pb.UpsertVectorRequest{
		Vector: convertVectorEntityToProto(sdkReq.Vector),
	}

	pbResp, err := vc.UpsertVector(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleUpsert")
	}

	return &vector.UpsertResponse{
		ID:        pbResp.Id,
		Version:   int64(pbResp.Version),
		Success:   true,
		Timestamp: convertTimestampFromProto(pbResp.Timestamp),
	}, nil
}

func handleBatchUpsert(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.BatchUpsertRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleBatchUpsert", "invalid request type, expected *vector.BatchUpsertRequest")
	}

	pbVectors := make([]*pb.VectorEntity, len(sdkReq.Vectors))
	for i, v := range sdkReq.Vectors {
		pbVectors[i] = convertVectorEntityToProto(v)
	}

	pbReq := &pb.BatchUpsertVectorsRequest{
		Vectors: pbVectors,
	}

	stream, err := vc.BatchUpsertVectors(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleBatchUpsert")
	}

	var vectorsSucceeded, vectorsFailed int64
	errorsMap := make(map[string]string)

	for {
		resp, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, convertGRPCError(err, "vectorHandler.handleBatchUpsert")
		}

		vectorsSucceeded = resp.VectorsSucceeded
		vectorsFailed = resp.VectorsFailed

		for _, failedID := range resp.FailedIds {
			errorsMap[failedID] = resp.ErrorMessage
		}

		if resp.IsFinal {
			break
		}
	}

	return &vector.BatchUpsertResponse{
		VectorsSucceeded: vectorsSucceeded,
		VectorsFailed:    vectorsFailed,
		Errors:           errorsMap,
		Timestamp:        time.Now(),
	}, nil
}

func handleGet(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.GetRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleGet", "invalid request type, expected *vector.GetRequest")
	}

	pbReq := &pb.GetVectorRequest{
		Namespace:       sdkReq.Namespace,
		Id:              sdkReq.ID,
		IncludeVector:   sdkReq.IncludeVector,
		IncludeMetadata: sdkReq.IncludeMetadata,
	}

	pbResp, err := vc.GetVector(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleGet")
	}

	return &vector.GetResponse{
		Vector:    convertVectorEntityFromProto(pbResp.Vector),
		Found:     pbResp.Found,
		Timestamp: time.Now(),
	}, nil
}

func handleDelete(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.DeleteRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleDelete", "invalid request type, expected *vector.DeleteRequest")
	}

	pbReq := &pb.DeleteVectorRequest{
		Namespace: sdkReq.Namespace,
		Id:        sdkReq.ID,
	}

	pbResp, err := vc.DeleteVector(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleDelete")
	}

	return &vector.DeleteResponse{
		Deleted:   pbResp.Deleted,
		Timestamp: convertTimestampFromProto(pbResp.Timestamp),
	}, nil
}

func handleBatchDelete(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.BatchDeleteRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleBatchDelete", "invalid request type, expected *vector.BatchDeleteRequest")
	}

	pbReq := &pb.BatchDeleteVectorsRequest{
		Namespace: sdkReq.Namespace,
		Ids:       sdkReq.IDs,
	}

	pbResp, err := vc.BatchDeleteVectors(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleBatchDelete")
	}

	return &vector.BatchDeleteResponse{
		DeletedCount: pbResp.DeletedCount,
		Errors:       make(map[string]string),
		Timestamp:    convertTimestampFromProto(pbResp.Timestamp),
	}, nil
}

func handleList(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.ListVectorsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleList", "invalid request type, expected *vector.ListVectorsRequest")
	}

	pbReq := &pb.ListVectorsRequest{
		Namespace:       sdkReq.Namespace,
		PageSize:        sdkReq.PageSize,
		PageToken:       sdkReq.PageToken,
		IncludeVectors:  sdkReq.IncludeVectors,
		IncludeMetadata: sdkReq.IncludeMetadata,
	}

	stream, err := vc.ListVectors(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleList")
	}

	var allVectors []*vector.VectorEntity
	var nextPageToken string
	var totalCount int64

	for {
		resp, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, convertGRPCError(err, "vectorHandler.handleList")
		}

		for _, v := range resp.Vectors {
			allVectors = append(allVectors, convertVectorEntityFromProto(v))
		}
		nextPageToken = resp.NextPageToken
		totalCount = resp.TotalCount

		if resp.IsFinal {
			break
		}
	}

	return &vector.ListVectorsResponse{
		Vectors:       allVectors,
		TotalCount:    totalCount,
		NextPageToken: nextPageToken,
		Timestamp:     time.Now(),
	}, nil
}

func handleUpdateMetadata(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.UpdateMetadataRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleUpdateMetadata", "invalid request type, expected *vector.UpdateMetadataRequest")
	}

	pbReq := &pb.UpdateMetadataRequest{
		Namespace: sdkReq.Namespace,
		Id:        sdkReq.ID,
		Metadata:  sdkReq.Metadata,
		Merge:     sdkReq.Merge,
	}

	pbResp, err := vc.UpdateMetadata(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleUpdateMetadata")
	}

	return &vector.UpdateMetadataResponse{
		Success:   pbResp.Success,
		Version:   int64(pbResp.Version),
		Timestamp: convertTimestampFromProto(pbResp.Timestamp),
	}, nil
}

// =============================================================================
// Search Handlers
// =============================================================================

func handleSearchSimilar(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.SearchSimilarRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleSearchSimilar", "invalid request type, expected *vector.SearchSimilarRequest")
	}

	pbReq := &pb.SearchSimilarRequest{
		Namespace:       sdkReq.Namespace,
		QueryVector:     sdkReq.QueryVector,
		K:               sdkReq.K,
		EfSearch:        sdkReq.EfSearch,
		MinScore:        sdkReq.MinScore,
		IncludeVectors:  sdkReq.IncludeVectors,
		IncludeMetadata: sdkReq.IncludeMetadata,
	}

	stream, err := vc.SearchSimilar(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleSearchSimilar")
	}

	var allResults []*vector.SearchResult
	var totalCandidates, searchTimeMs int64

	for {
		resp, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, convertGRPCError(err, "vectorHandler.handleSearchSimilar")
		}

		for _, r := range resp.Results {
			allResults = append(allResults, convertSimilarityResultFromProto(r))
		}
		totalCandidates = resp.TotalCandidates
		searchTimeMs = resp.SearchTimeMs

		if resp.IsFinal {
			break
		}
	}

	return &vector.SearchSimilarResponse{
		Results:         allResults,
		TotalCandidates: totalCandidates,
		SearchTimeMs:    searchTimeMs,
		Timestamp:       time.Now(),
	}, nil
}

func handleSearchByID(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.SearchByIDRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleSearchByID", "invalid request type, expected *vector.SearchByIDRequest")
	}

	pbReq := &pb.SearchByIDRequest{
		Namespace:       sdkReq.Namespace,
		Id:              sdkReq.ID,
		K:               sdkReq.K,
		EfSearch:        sdkReq.EfSearch,
		MinScore:        sdkReq.MinScore,
		IncludeSelf:     sdkReq.IncludeSelf,
		IncludeVectors:  sdkReq.IncludeVectors,
		IncludeMetadata: sdkReq.IncludeMetadata,
	}

	stream, err := vc.SearchByID(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleSearchByID")
	}

	var allResults []*vector.SearchResult
	var totalCandidates, searchTimeMs int64

	for {
		resp, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, convertGRPCError(err, "vectorHandler.handleSearchByID")
		}

		for _, r := range resp.Results {
			allResults = append(allResults, convertSimilarityResultFromProto(r))
		}
		totalCandidates = resp.TotalCandidates
		searchTimeMs = resp.SearchTimeMs

		if resp.IsFinal {
			break
		}
	}

	return &vector.SearchByIDResponse{
		Results:         allResults,
		TotalCandidates: totalCandidates,
		SearchTimeMs:    searchTimeMs,
		Timestamp:       time.Now(),
	}, nil
}

func handleHybridSearch(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.HybridSearchRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleHybridSearch", "invalid request type, expected *vector.HybridSearchRequest")
	}

	pbFilters := make([]*pb.MetadataFilter, len(sdkReq.MetadataFilters))
	for i, f := range sdkReq.MetadataFilters {
		pbFilters[i] = convertMetadataFilterToProto(f)
	}

	pbReq := &pb.HybridSearchRequest{
		Namespace:          sdkReq.Namespace,
		QueryVector:        sdkReq.QueryVector,
		K:                  sdkReq.K,
		EfSearch:           sdkReq.EfSearch,
		MinScore:           sdkReq.MinScore,
		MetadataFilters:    pbFilters,
		FilterOperator:     sdkReq.FilterOperator,
		RerankingAlgorithm: sdkReq.RerankingAlgorithm,
		IncludeVectors:     sdkReq.IncludeVectors,
		IncludeMetadata:    sdkReq.IncludeMetadata,
	}

	stream, err := vc.HybridSearch(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleHybridSearch")
	}

	var allResults []*vector.SearchResult
	var totalCandidates, totalFiltered, searchTimeMs int64
	var rerankingApplied bool

	for {
		resp, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, convertGRPCError(err, "vectorHandler.handleHybridSearch")
		}

		for _, r := range resp.Results {
			allResults = append(allResults, convertSimilarityResultFromProto(r))
		}
		totalCandidates = resp.TotalCandidates
		totalFiltered = resp.TotalFiltered
		searchTimeMs = resp.SearchTimeMs
		rerankingApplied = resp.RerankingApplied

		if resp.IsFinal {
			break
		}
	}

	return &vector.HybridSearchResponse{
		Results:          allResults,
		TotalCandidates:  totalCandidates,
		TotalFiltered:    totalFiltered,
		RerankingApplied: rerankingApplied,
		SearchTimeMs:     searchTimeMs,
		Timestamp:        time.Now(),
	}, nil
}

func handleSearchByMetadata(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.SearchByMetadataRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleSearchByMetadata", "invalid request type, expected *vector.SearchByMetadataRequest")
	}

	pbFilters := make([]*pb.MetadataFilter, len(sdkReq.MetadataFilters))
	for i, f := range sdkReq.MetadataFilters {
		pbFilters[i] = convertMetadataFilterToProto(f)
	}

	pbReq := &pb.SearchByMetadataRequest{
		Namespace:       sdkReq.Namespace,
		MetadataFilters: pbFilters,
		FilterOperator:  sdkReq.FilterOperator,
		Limit:           sdkReq.Limit,
		Offset:          sdkReq.Offset,
		IncludeVectors:  sdkReq.IncludeVectors,
		IncludeMetadata: sdkReq.IncludeMetadata,
	}

	stream, err := vc.SearchByMetadata(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleSearchByMetadata")
	}

	var allResults []*vector.VectorEntity
	var totalCount, searchTimeMs int64
	var nextCursor string

	for {
		resp, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, convertGRPCError(err, "vectorHandler.handleSearchByMetadata")
		}

		for _, v := range resp.Results {
			allResults = append(allResults, convertVectorEntityFromProto(v))
		}
		totalCount = resp.TotalCount
		nextCursor = resp.NextCursor
		searchTimeMs = resp.SearchTimeMs

		if resp.IsFinal {
			break
		}
	}

	return &vector.SearchByMetadataResponse{
		Results:      allResults,
		TotalCount:   totalCount,
		NextCursor:   nextCursor,
		SearchTimeMs: searchTimeMs,
		Timestamp:    time.Now(),
	}, nil
}

// =============================================================================
// Index Management Handlers
// =============================================================================

func handleRebuildIndex(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.RebuildIndexRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleRebuildIndex", "invalid request type, expected *vector.RebuildIndexRequest")
	}

	pbReq := &pb.RebuildIndexRequest{
		Namespace:   sdkReq.Namespace,
		IndexParams: sdkReq.IndexParams,
		Async:       sdkReq.Async,
	}

	pbResp, err := vc.RebuildIndex(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleRebuildIndex")
	}

	return &vector.RebuildIndexResponse{
		OperationID:         pbResp.OperationId,
		Status:              "started",
		EstimatedDurationMs: pbResp.EstimatedDurationMs,
		Started:             convertTimestampFromProto(pbResp.Started),
		Timestamp:           time.Now(),
	}, nil
}

func handleGetIndexProgress(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.GetIndexProgressRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleGetIndexProgress", "invalid request type, expected *vector.GetIndexProgressRequest")
	}

	pbReq := &pb.GetIndexProgressRequest{
		OperationId: sdkReq.OperationID,
		Namespace:   sdkReq.Namespace,
	}

	pbResp, err := vc.GetIndexProgress(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleGetIndexProgress")
	}

	return &vector.GetIndexProgressResponse{
		OperationID:          pbResp.OperationId,
		Namespace:            pbResp.Namespace,
		Status:               pbResp.Status,
		PercentComplete:      float32(pbResp.PercentComplete),
		VectorsIndexed:       pbResp.VectorsIndexed,
		TotalVectors:         pbResp.TotalVectors,
		ElapsedTimeMs:        pbResp.ElapsedTimeMs,
		EstimatedRemainingMs: pbResp.EstimatedRemainingMs,
		ErrorMessage:         pbResp.ErrorMessage,
		Timestamp:            time.Now(),
	}, nil
}

// =============================================================================
// Admin Handlers
// =============================================================================

func handleExport(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.ExportRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleExport", "invalid request type, expected *vector.ExportRequest")
	}

	pbReq := &pb.ExportVectorsRequest{
		Namespace:   sdkReq.Namespace,
		Format:      sdkReq.Format,
		Compression: sdkReq.Compression,
		BatchSize:   sdkReq.BatchSize,
	}

	stream, err := ac.ExportVectors(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleExport")
	}

	var allResponses []*vector.ExportResponse

	for {
		resp, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, convertGRPCError(err, "vectorHandler.handleExport")
		}

		allResponses = append(allResponses, &vector.ExportResponse{
			BatchData:       resp.BatchData,
			VectorsExported: resp.VectorsExported,
			IsFinal:         resp.IsFinal,
			Timestamp:       time.Now(),
		})

		if resp.IsFinal {
			break
		}
	}

	return allResponses, nil
}

func handleImport(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.([]*vector.ImportRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleImport", "invalid request type, expected []*vector.ImportRequest")
	}

	stream, err := ac.ImportVectors(ctx)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleImport")
	}

	for _, batch := range sdkReq {
		pbReq := &pb.ImportVectorsRequest{
			Namespace:         batch.Namespace,
			Format:            batch.Format,
			BatchData:         batch.BatchData,
			OverwriteExisting: batch.OverwriteExisting,
		}

		if err := stream.Send(pbReq); err != nil {
			return nil, convertGRPCError(err, "vectorHandler.handleImport")
		}
	}

	pbResp, err := stream.CloseAndRecv()
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleImport")
	}

	return &vector.ImportResponse{
		VectorsImported: pbResp.VectorsImported,
		VectorsSkipped:  pbResp.VectorsSkipped,
		VectorsFailed:   pbResp.VectorsFailed,
		DurationMs:      pbResp.DurationMs,
		Timestamp:       time.Now(),
	}, nil
}

func handleCompact(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.CompactRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleCompact", "invalid request type, expected *vector.CompactRequest")
	}

	pbReq := &pb.CompactNamespaceRequest{
		Namespace: sdkReq.Namespace,
		Async:     sdkReq.Async,
	}

	pbResp, err := ac.CompactNamespace(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleCompact")
	}

	return &vector.CompactResponse{
		OperationID: pbResp.OperationId,
		Started:     convertTimestampFromProto(pbResp.Started),
		Timestamp:   time.Now(),
	}, nil
}

func handleGetCompactionProgress(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.GetCompactionProgressRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleGetCompactionProgress", "invalid request type, expected *vector.GetCompactionProgressRequest")
	}

	pbReq := &pb.GetCompactionProgressRequest{
		OperationId: sdkReq.OperationID,
		Namespace:   sdkReq.Namespace,
	}

	pbResp, err := ac.GetCompactionProgress(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleGetCompactionProgress")
	}

	return &vector.GetCompactionProgressResponse{
		OperationID:         pbResp.OperationId,
		Namespace:           pbResp.Namespace,
		Status:              pbResp.Status,
		PercentComplete:     float32(pbResp.PercentComplete),
		BytesCompacted:      pbResp.BytesCompacted,
		TotalBytes:          pbResp.TotalBytes,
		SpaceReclaimedBytes: pbResp.SpaceReclaimedBytes,
		ElapsedTimeMs:       pbResp.ElapsedTimeMs,
		Timestamp:           time.Now(),
	}, nil
}

func handleGetStats(ctx context.Context, h *vectorHandler, vc pb.VectorDBClient, ac pb.VectorAdminClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*vector.GetStatsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("vectorHandler.handleGetStats", "invalid request type, expected *vector.GetStatsRequest")
	}

	pbReq := &pb.GetVectorStatsRequest{
		Namespace: sdkReq.Namespace,
	}

	pbResp, err := vc.GetVectorStats(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "vectorHandler.handleGetStats")
	}

	return &vector.GetStatsResponse{
		Namespace:        sdkReq.Namespace,
		VectorCount:      pbResp.TotalVectors,
		IndexSizeBytes:   pbResp.TotalIndexBytes,
		StorageSizeBytes: pbResp.TotalStorageBytes,
		Timestamp:        time.Now(),
	}, nil
}

// =============================================================================
// Type Conversion Functions
// =============================================================================

// convertNamespaceConfigToProto converts SDK NamespaceConfig to protobuf.
func convertNamespaceConfigToProto(cfg *vector.NamespaceConfig) *pb.NamespaceConfig {
	if cfg == nil {
		return nil
	}

	return &pb.NamespaceConfig{
		Namespace:   cfg.Namespace,
		Dimensions:  cfg.Dimensions,
		IndexType:   convertIndexTypeToProto(cfg.IndexType),
		IndexParams: cfg.IndexParams,
		Metadata:    cfg.Metadata,
	}
}

// convertIndexTypeToProto converts SDK IndexType to protobuf.
func convertIndexTypeToProto(indexType vector.IndexType) pb.IndexType {
	switch indexType {
	case vector.IndexTypeHNSW:
		return pb.IndexType_INDEX_TYPE_HNSW
	case vector.IndexTypeFlat:
		return pb.IndexType_INDEX_TYPE_FLAT
	case vector.IndexTypeIVF:
		return pb.IndexType_INDEX_TYPE_IVF_FLAT
	default:
		return pb.IndexType_INDEX_TYPE_UNSPECIFIED
	}
}

// convertVectorEntityToProto converts SDK VectorEntity to protobuf.
func convertVectorEntityToProto(v *vector.VectorEntity) *pb.VectorEntity {
	if v == nil {
		return nil
	}

	return &pb.VectorEntity{
		Id:        v.ID,
		Namespace: v.Namespace,
		Vector:    v.Vector,
		Metadata:  v.Metadata,
		Version:   uint64(v.Version),
	}
}

// convertVectorEntityFromProto converts protobuf VectorEntity to SDK.
func convertVectorEntityFromProto(v *pb.VectorEntity) *vector.VectorEntity {
	if v == nil {
		return nil
	}

	return &vector.VectorEntity{
		ID:        v.Id,
		Namespace: v.Namespace,
		Vector:    v.Vector,
		Metadata:  v.Metadata,
		Version:   int64(v.Version),
		CreatedAt: convertTimestampFromProto(v.CreatedAt),
		UpdatedAt: convertTimestampFromProto(v.UpdatedAt),
	}
}

// convertNamespaceInfoFromProto converts protobuf NamespaceInfo to SDK.
func convertNamespaceInfoFromProto(info *pb.NamespaceInfo) *vector.NamespaceInfo {
	if info == nil {
		return nil
	}

	var cfg *vector.NamespaceConfig
	if info.Config != nil {
		cfg = &vector.NamespaceConfig{
			Namespace:   info.Config.Namespace,
			Dimensions:  info.Config.Dimensions,
			IndexType:   convertIndexTypeFromProto(info.Config.IndexType),
			IndexParams: info.Config.IndexParams,
			Metadata:    info.Config.Metadata,
		}
	}

	return &vector.NamespaceInfo{
		Config:       cfg,
		VectorCount:  info.VectorCount,
		IndexBytes:   info.IndexBytes,
		StorageBytes: info.StorageBytes,
		CreatedAt:    convertTimestampFromProto(info.CreatedAt),
		UpdatedAt:    convertTimestampFromProto(info.UpdatedAt),
	}
}

// convertIndexTypeFromProto converts protobuf IndexType to SDK.
func convertIndexTypeFromProto(indexType pb.IndexType) vector.IndexType {
	switch indexType {
	case pb.IndexType_INDEX_TYPE_HNSW:
		return vector.IndexTypeHNSW
	case pb.IndexType_INDEX_TYPE_FLAT:
		return vector.IndexTypeFlat
	case pb.IndexType_INDEX_TYPE_IVF_FLAT, pb.IndexType_INDEX_TYPE_IVF_PQ:
		return vector.IndexTypeIVF
	default:
		return vector.IndexTypeHNSW
	}
}

// convertSimilarityResultFromProto converts protobuf SimilarityResult to SDK SearchResult.
func convertSimilarityResultFromProto(r *pb.SimilarityResult) *vector.SearchResult {
	if r == nil {
		return nil
	}

	return &vector.SearchResult{
		Vector:   convertVectorEntityFromProto(r.Vector),
		Score:    r.Score,
		Distance: r.Score,
		Rank:     r.Rank,
	}
}

// convertMetadataFilterToProto converts SDK MetadataFilter to protobuf.
func convertMetadataFilterToProto(f *vector.MetadataFilter) *pb.MetadataFilter {
	if f == nil {
		return nil
	}

	return &pb.MetadataFilter{
		Key:      f.Key,
		Operator: convertFilterOperatorToProto(f.Operator),
		Value:    f.Value,
	}
}

// convertFilterOperatorToProto converts SDK FilterOperator to protobuf.
func convertFilterOperatorToProto(op vector.FilterOperator) pb.FilterOperator {
	switch op {
	case vector.FilterOperatorEquals:
		return pb.FilterOperator_FILTER_OPERATOR_EQUALS
	case vector.FilterOperatorNotEquals:
		return pb.FilterOperator_FILTER_OPERATOR_NOT_EQUALS
	case vector.FilterOperatorContains:
		return pb.FilterOperator_FILTER_OPERATOR_CONTAINS
	case vector.FilterOperatorIn:
		return pb.FilterOperator_FILTER_OPERATOR_IN
	default:
		return pb.FilterOperator_FILTER_OPERATOR_UNSPECIFIED
	}
}

// convertTimestampFromProto converts protobuf Timestamp to time.Time.
func convertTimestampFromProto(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}
