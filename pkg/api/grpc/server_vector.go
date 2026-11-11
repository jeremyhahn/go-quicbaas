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
	"fmt"
	"math"
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// ========================================
// Namespace Management
// ========================================

// CreateNamespace creates a new vector namespace with specified configuration.
func (s *Server) CreateNamespace(ctx context.Context, req *pb.CreateNamespaceRequest) (*pb.CreateNamespaceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Config == nil {
		return nil, status.Error(codes.InvalidArgument, "config is required")
	}

	if req.Config.Namespace == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace name is required")
	}

	if req.Config.Dimensions <= 0 {
		return nil, status.Error(codes.InvalidArgument, "dimensions must be positive")
	}

	// Check if service supports vectors
	if s.service == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}

	// Convert proto config to service request
	indexConfig := make(map[string]string)
	if req.Config.IndexParams != nil {
		for k, v := range req.Config.IndexParams {
			indexConfig[k] = v
		}
	}

	metadata := make(map[string]interface{})
	if req.Config.Metadata != nil {
		for k, v := range req.Config.Metadata {
			metadata[k] = v
		}
	}

	// Determine shard ID
	shardID := req.Config.ShardId
	if shardID == 0 {
		shardID = s.vectorShardID
	}

	// Convert distance metric to string
	distanceMetric := protoDistanceMetricToString(req.Config.DistanceMetric)

	// Convert index type to string
	indexType := protoIndexTypeToString(req.Config.IndexType)

	dimension, err := conv.SafeIntToUint32(int(req.Config.Dimensions))
	if err != nil {
		return nil, requestRangeError("config.dimensions", err)
	}

	serviceReq := &service.CreateNamespaceRequest{
		Namespace:      req.Config.Namespace,
		Dimension:      dimension,
		DistanceMetric: distanceMetric,
		IndexType:      indexType,
		IndexConfig:    indexConfig,
		Metadata:       metadata,
		ShardID:        shardID,
	}

	// Call service
	resp, err := vectorService.CreateNamespace(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.CreateNamespaceResponse{
		Namespace: resp.Namespace,
		Timestamp: timestamppb.New(time.Unix(0, resp.Timestamp)),
	}, nil
}

// UpdateNamespace refuses every namespace update with codes.Unimplemented.
// The qrdb SDK has no in-place namespace update, and deleting and
// recreating the namespace would destroy every vector stored in it.
// A missing namespace still reports its lookup error.
func (s *Server) UpdateNamespace(ctx context.Context, req *pb.UpdateNamespaceRequest) (*pb.UpdateNamespaceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Namespace == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace is required")
	}

	// Check if service supports vectors
	if s.service == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}

	// Confirm the namespace exists so a missing namespace keeps its error
	getReq := &service.GetNamespaceInfoRequest{
		Namespace: req.Namespace,
		ShardID:   s.vectorShardID,
	}

	if _, err := vectorService.GetNamespaceInfo(ctx, getReq); err != nil {
		return nil, toProtoError(err)
	}

	return nil, toProtoError(&NamespaceUpdateUnsupportedError{Namespace: req.Namespace})
}

// DeleteNamespace removes a namespace and all its vectors.
func (s *Server) DeleteNamespace(ctx context.Context, req *pb.DeleteNamespaceRequest) (*pb.DeleteNamespaceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Namespace == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace is required")
	}

	// Check if service supports vectors
	if s.service == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}

	serviceReq := &service.DeleteNamespaceRequest{
		Namespace: req.Namespace,
		ShardID:   s.vectorShardID,
	}

	// Call service
	resp, err := vectorService.DeleteNamespace(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	vectorsDeleted, err := conv.SafeUint64ToInt64(resp.VectorsDeleted)
	if err != nil {
		return nil, responseRangeError("vectors_deleted", err)
	}

	return &pb.DeleteNamespaceResponse{
		Deleted:        resp.Success,
		VectorsDeleted: vectorsDeleted,
		Timestamp:      timestamppb.New(time.Unix(0, resp.Timestamp)),
	}, nil
}

// ListNamespaces returns all available vector namespaces.
func (s *Server) ListNamespaces(ctx context.Context, req *pb.ListNamespacesRequest) (*pb.ListNamespacesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Check if service supports vectors
	if s.service == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}

	serviceReq := &service.ListNamespacesRequest{
		Prefix:  req.Prefix,
		Limit:   int(req.PageSize),
		Offset:  0,
		ShardID: s.vectorShardID,
	}

	// Call service
	resp, err := vectorService.ListNamespaces(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	// Convert response
	namespaces := make([]*pb.NamespaceInfo, 0, len(resp.Namespaces))
	for _, ns := range resp.Namespaces {
		vectorCount, err := conv.SafeUint64ToInt64(ns.VectorCount)
		if err != nil {
			return nil, responseRangeError("vector_count", err)
		}
		dimensions, err := conv.SafeUint32ToInt32(ns.Dimension)
		if err != nil {
			return nil, responseRangeError("dimensions", err)
		}
		namespaces = append(namespaces, &pb.NamespaceInfo{
			Config: &pb.NamespaceConfig{
				Namespace:      ns.Namespace,
				Dimensions:     dimensions,
				DistanceMetric: stringToProtoDistanceMetric(ns.DistanceMetric),
				IndexType:      stringToProtoIndexType(ns.IndexType),
				IndexParams:    ns.IndexConfig,
				Metadata:       ns.Metadata,
			},
			VectorCount: vectorCount,
			CreatedAt:   timestamppb.New(time.Unix(0, ns.CreatedAt)),
			UpdatedAt:   timestamppb.New(time.Unix(0, ns.UpdatedAt)),
		})
	}

	totalCount, err := conv.SafeIntToInt32(resp.TotalCount)
	if err != nil {
		return nil, responseRangeError("total_count", err)
	}

	return &pb.ListNamespacesResponse{
		Namespaces: namespaces,
		TotalCount: totalCount,
	}, nil
}

// GetNamespaceInfo retrieves namespace configuration and statistics.
func (s *Server) GetNamespaceInfo(ctx context.Context, req *pb.GetNamespaceInfoRequest) (*pb.GetNamespaceInfoResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Namespace == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace is required")
	}

	// Check if service supports vectors
	if s.service == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}

	serviceReq := &service.GetNamespaceInfoRequest{
		Namespace: req.Namespace,
		ShardID:   s.vectorShardID,
	}

	// Call service
	resp, err := vectorService.GetNamespaceInfo(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	// Convert index config
	indexConfig := make(map[string]string)
	if resp.IndexConfig != nil {
		for k, v := range resp.IndexConfig {
			indexConfig[k] = v
		}
	}

	// Convert metadata
	metadata := make(map[string]string)
	if resp.Metadata != nil {
		for k, v := range resp.Metadata {
			metadata[k] = fmt.Sprintf("%v", v)
		}
	}

	dimensions, err := conv.SafeUint32ToInt32(resp.Dimension)
	if err != nil {
		return nil, responseRangeError("dimensions", err)
	}
	vectorCount, err := conv.SafeUint64ToInt64(resp.VectorCount)
	if err != nil {
		return nil, responseRangeError("vector_count", err)
	}

	return &pb.GetNamespaceInfoResponse{
		Info: &pb.NamespaceInfo{
			Config: &pb.NamespaceConfig{
				Namespace:      resp.Namespace,
				Dimensions:     dimensions,
				DistanceMetric: stringToProtoDistanceMetric(resp.DistanceMetric),
				IndexType:      stringToProtoIndexType(resp.IndexType),
				IndexParams:    indexConfig,
				Metadata:       metadata,
			},
			VectorCount: vectorCount,
			CreatedAt:   timestamppb.New(time.Unix(0, resp.CreatedAt)),
			UpdatedAt:   timestamppb.New(time.Unix(0, resp.UpdatedAt)),
		},
	}, nil
}

// ========================================
// Vector CRUD Operations
// ========================================

// UpsertVector creates or updates a single vector with metadata.
func (s *Server) UpsertVector(ctx context.Context, req *pb.UpsertVectorRequest) (*pb.UpsertVectorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Vector == nil {
		return nil, status.Error(codes.InvalidArgument, "vector is required")
	}

	if req.Vector.Namespace == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace is required")
	}

	if req.Vector.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "vector ID is required")
	}

	if len(req.Vector.Vector) == 0 {
		return nil, status.Error(codes.InvalidArgument, "vector data is required")
	}

	// Validate vector values for NaN and Inf
	for i, val := range req.Vector.Vector {
		if math.IsNaN(float64(val)) || math.IsInf(float64(val), 0) {
			return nil, status.Errorf(codes.InvalidArgument, "vector contains invalid value at index %d: NaN or Inf not allowed", i)
		}
	}

	// Check if service supports vectors
	if s.service == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}

	// Parse vector ID
	vectorID := parseVectorID(req.Vector.Id)

	// Convert metadata
	metadata := make(map[string]interface{})
	if req.Vector.Metadata != nil {
		for k, v := range req.Vector.Metadata {
			metadata[k] = v
		}
	}

	// Store original ID in metadata if it's not numeric
	storeOriginalID(req.Vector.Id, metadata)

	serviceReq := &service.UpsertVectorRequest{
		Namespace: req.Vector.Namespace,
		ID:        vectorID,
		Vector:    req.Vector.Vector,
		Metadata:  metadata,
		ShardID:   s.vectorShardID,
	}

	// Call service
	resp, err := vectorService.UpsertVector(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.UpsertVectorResponse{
		Id:           req.Vector.Id,
		Version:      resp.Version,
		Timestamp:    timestamppb.New(time.Unix(0, resp.Timestamp)),
		IndexUpdated: !req.SkipIndexUpdate,
	}, nil
}

// UpdateMetadata updates only the metadata for a vector.
func (s *Server) UpdateMetadata(ctx context.Context, req *pb.UpdateMetadataRequest) (*pb.UpdateMetadataResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Namespace == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace is required")
	}

	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "vector ID is required")
	}

	if len(req.Metadata) == 0 {
		return nil, status.Error(codes.InvalidArgument, "metadata cannot be empty")
	}

	// Check if service supports vectors
	if s.service == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}

	// Parse vector ID
	vectorID := parseVectorID(req.Id)

	// Get existing vector
	getReq := &service.GetVectorRequest{
		Namespace:       req.Namespace,
		ID:              vectorID,
		IncludeVector:   true,
		IncludeMetadata: true,
		ShardID:         s.vectorShardID,
		Consistency:     convertProtoConsistencyLevel(req.Consistency),
	}

	getResp, err := vectorService.GetVector(ctx, getReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	if !getResp.Found {
		return nil, status.Error(codes.NotFound, "vector not found")
	}

	// Update metadata
	newMetadata := getResp.Metadata
	if newMetadata == nil {
		newMetadata = make(map[string]interface{})
	}

	if req.Merge {
		// Merge with existing metadata
		for k, v := range req.Metadata {
			newMetadata[k] = v
		}
	} else {
		// Replace all metadata
		newMetadata = make(map[string]interface{})
		for k, v := range req.Metadata {
			newMetadata[k] = v
		}
	}

	// Store original ID
	storeOriginalID(req.Id, newMetadata)

	// Upsert with updated metadata
	upsertReq := &service.UpsertVectorRequest{
		Namespace: req.Namespace,
		ID:        vectorID,
		Vector:    getResp.Vector,
		Metadata:  newMetadata,
		ShardID:   s.vectorShardID,
	}

	upsertResp, err := vectorService.UpsertVector(ctx, upsertReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.UpdateMetadataResponse{
		Success:   upsertResp.Success,
		Version:   upsertResp.Version,
		Timestamp: timestamppb.New(time.Unix(0, upsertResp.Timestamp)),
	}, nil
}

// GetVector retrieves a vector by ID with optional metadata.
func (s *Server) GetVector(ctx context.Context, req *pb.GetVectorRequest) (*pb.GetVectorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Namespace == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace is required")
	}

	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "vector ID is required")
	}

	// Check if service supports vectors
	if s.service == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}

	// Parse vector ID
	vectorID := parseVectorID(req.Id)

	// Convert consistency level
	consistency := convertProtoConsistencyLevel(req.Consistency)

	serviceReq := &service.GetVectorRequest{
		Namespace:       req.Namespace,
		ID:              vectorID,
		IncludeVector:   req.IncludeVector,
		IncludeMetadata: req.IncludeMetadata,
		ShardID:         s.vectorShardID,
		Consistency:     consistency,
	}

	// Call service
	resp, err := vectorService.GetVector(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	if !resp.Found {
		return &pb.GetVectorResponse{
			Found:  false,
			NodeId: s.nodeID,
		}, nil
	}

	// Extract original ID before filtering metadata
	originalID := ""
	if resp.Metadata != nil {
		if val, ok := resp.Metadata[MetadataKeyOriginalID]; ok {
			originalID = fmt.Sprintf("%v", val)
		}
	}

	// Convert metadata, filtering out internal keys
	metadata := make(map[string]string)
	if resp.Metadata != nil {
		for k, v := range resp.Metadata {
			// Filter out internal metadata keys
			if k == MetadataKeyOriginalID {
				continue
			}
			metadata[k] = fmt.Sprintf("%v", v)
		}
	}

	// Use original string ID if found, otherwise format the numeric ID
	vectorIDStr := originalID
	if originalID == "" {
		vectorIDStr = strconv.FormatUint(resp.ID, 10)
	}

	// Convert timestamps
	var createdAt, updatedAt *timestamppb.Timestamp
	if resp.CreatedAt > 0 {
		createdAt = timestamppb.New(time.Unix(0, resp.CreatedAt))
	}
	if resp.UpdatedAt > 0 {
		updatedAt = timestamppb.New(time.Unix(0, resp.UpdatedAt))
	}

	return &pb.GetVectorResponse{
		Vector: &pb.VectorEntity{
			Id:        vectorIDStr,
			Namespace: req.Namespace,
			Vector:    resp.Vector,
			Metadata:  metadata,
			Version:   resp.Version,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		},
		Found:  true,
		NodeId: s.nodeID,
	}, nil
}

// DeleteVector removes a vector by ID.
func (s *Server) DeleteVector(ctx context.Context, req *pb.DeleteVectorRequest) (*pb.DeleteVectorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Namespace == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace is required")
	}

	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "vector ID is required")
	}

	// Check if service supports vectors
	if s.service == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}

	// Parse vector ID
	vectorID := parseVectorID(req.Id)

	serviceReq := &service.DeleteVectorRequest{
		Namespace: req.Namespace,
		ID:        vectorID,
		ShardID:   s.vectorShardID,
	}

	// Call service
	resp, err := vectorService.DeleteVector(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.DeleteVectorResponse{
		Deleted:   resp.Success,
		Timestamp: timestamppb.New(time.Unix(0, resp.Timestamp)),
	}, nil
}

// BatchDeleteVectors removes multiple vectors atomically.
func (s *Server) BatchDeleteVectors(ctx context.Context, req *pb.BatchDeleteVectorsRequest) (*pb.BatchDeleteVectorsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Namespace == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace is required")
	}

	if len(req.Ids) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one vector ID is required")
	}

	// Check if service supports vectors
	if s.service == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}

	// Delete vectors one by one (service layer should optimize this)
	deletedCount := int64(0)
	for _, id := range req.Ids {
		vectorID := parseVectorID(id)
		serviceReq := &service.DeleteVectorRequest{
			Namespace: req.Namespace,
			ID:        vectorID,
			ShardID:   s.vectorShardID,
		}

		resp, err := vectorService.DeleteVector(ctx, serviceReq)
		if err != nil {
			s.logger.WithError(err).Warnf("Failed to delete vector %s", id)
			continue
		}

		if resp.Success {
			deletedCount++
		}
	}

	return &pb.BatchDeleteVectorsResponse{
		DeletedCount: deletedCount,
		Timestamp:    timestamppb.New(time.Now()),
	}, nil
}

// BatchUpsertVectors creates or updates multiple vectors atomically.
func (s *Server) BatchUpsertVectors(req *pb.BatchUpsertVectorsRequest, stream pb.VectorDB_BatchUpsertVectorsServer) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request is nil")
	}

	if len(req.Vectors) == 0 {
		return status.Error(codes.InvalidArgument, "vectors cannot be empty")
	}

	// Check maximum batch size
	const maxBatchSize = 10000
	if len(req.Vectors) > maxBatchSize {
		return status.Errorf(codes.InvalidArgument, "batch size %d exceeds maximum allowed %d", len(req.Vectors), maxBatchSize)
	}

	// Validate all vectors are in the same namespace
	namespace := ""
	for i, vec := range req.Vectors {
		if vec.Namespace == "" {
			return status.Errorf(codes.InvalidArgument, "vector %d: namespace is required", i)
		}
		if namespace == "" {
			namespace = vec.Namespace
		} else if vec.Namespace != namespace {
			return status.Error(codes.InvalidArgument, "all vectors must be in the same namespace")
		}
		if vec.Id == "" {
			return status.Errorf(codes.InvalidArgument, "vector %d: ID is required", i)
		}
		if len(vec.Vector) == 0 {
			return status.Errorf(codes.InvalidArgument, "vector %d: vector data is required", i)
		}

		// Validate vector values for NaN and Inf
		for j, val := range vec.Vector {
			if math.IsNaN(float64(val)) || math.IsInf(float64(val), 0) {
				return status.Errorf(codes.InvalidArgument, "vector %d contains invalid value at index %d: NaN or Inf not allowed", i, j)
			}
		}
	}

	// Check if service supports vectors
	if s.service == nil {
		return status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return status.Error(codes.Unimplemented, "vector service not available")
	}

	// Convert vectors to service format
	vectors := make([]*service.VectorData, len(req.Vectors))
	for i, vec := range req.Vectors {
		vectorID := parseVectorID(vec.Id)
		metadata := make(map[string]interface{})
		if vec.Metadata != nil {
			for k, v := range vec.Metadata {
				metadata[k] = v
			}
		}
		storeOriginalID(vec.Id, metadata)

		vectors[i] = &service.VectorData{
			ID:       vectorID,
			Vector:   vec.Vector,
			Metadata: metadata,
		}
	}

	serviceReq := &service.BatchUpsertRequest{
		Namespace: namespace,
		Vectors:   vectors,
		ShardID:   s.vectorShardID,
	}

	// Call service
	resp, err := vectorService.BatchUpsert(stream.Context(), serviceReq)
	if err != nil {
		return toProtoError(err)
	}

	// Send response
	return stream.Send(&pb.BatchUpsertVectorsResponse{
		VectorsProcessed: int64(len(req.Vectors)),
		VectorsSucceeded: int64(resp.InsertedCount),
		VectorsFailed:    int64(len(req.Vectors) - resp.InsertedCount),
		Timestamp:        timestamppb.New(time.Unix(0, resp.Timestamp)),
		IsFinal:          true,
	})
}

// ========================================
// Search Operations
// ========================================

// SearchSimilar performs k-NN similarity search to find the nearest neighbors.
func (s *Server) SearchSimilar(req *pb.SearchSimilarRequest, stream pb.VectorDB_SearchSimilarServer) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Namespace == "" {
		return status.Error(codes.InvalidArgument, "namespace is required")
	}

	if len(req.QueryVector) == 0 {
		return status.Error(codes.InvalidArgument, "query vector is required")
	}

	if req.K <= 0 {
		return status.Error(codes.InvalidArgument, "K must be positive")
	}

	const maxK = 10000
	if req.K > maxK {
		return status.Errorf(codes.InvalidArgument, "K %d exceeds maximum allowed %d", req.K, maxK)
	}

	// Validate query vector values for NaN and Inf
	for i, val := range req.QueryVector {
		if math.IsNaN(float64(val)) || math.IsInf(float64(val), 0) {
			return status.Errorf(codes.InvalidArgument, "query vector contains invalid value at index %d: NaN or Inf not allowed", i)
		}
	}

	// Check if service supports vectors
	if s.service == nil {
		return status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return status.Error(codes.Unimplemented, "vector service not available")
	}

	// Convert metadata filters
	metadataFilter := make(map[string]interface{})
	// Note: The proto uses repeated MetadataFilter, but the service uses map
	// For now, we'll skip complex filtering

	// Convert consistency level
	consistency := convertProtoConsistencyLevel(req.Consistency)

	serviceReq := &service.SearchSimilarRequest{
		Namespace:       req.Namespace,
		QueryVector:     req.QueryVector,
		K:               int(req.K),
		EfSearch:        int(req.EfSearch),
		MinScore:        req.MinScore,
		IncludeVectors:  req.IncludeVectors,
		IncludeMetadata: req.IncludeMetadata,
		MetadataFilter:  metadataFilter,
		ShardID:         s.vectorShardID,
		Consistency:     consistency,
	}

	// Call service
	startTime := time.Now()
	resp, err := vectorService.SearchSimilar(stream.Context(), serviceReq)
	if err != nil {
		return toProtoError(err)
	}
	searchDuration := time.Since(startTime).Milliseconds()

	// Convert results
	results := make([]*pb.SimilarityResult, 0, len(resp.Results))
	for _, res := range resp.Results {
		// Extract original ID before filtering metadata
		originalID := ""
		if res.Metadata != nil {
			if val, ok := res.Metadata[MetadataKeyOriginalID]; ok {
				originalID = fmt.Sprintf("%v", val)
			}
		}

		// Convert metadata, filtering out internal keys
		metadata := make(map[string]string)
		if res.Metadata != nil {
			for k, v := range res.Metadata {
				// Filter out internal metadata keys
				if k == MetadataKeyOriginalID {
					continue
				}
				metadata[k] = fmt.Sprintf("%v", v)
			}
		}

		// Use original string ID if found, otherwise format the numeric ID
		vectorIDStr := originalID
		if originalID == "" {
			vectorIDStr = strconv.FormatUint(res.ID, 10)
		}

		rank, err := conv.SafeIntToInt32(res.Rank)
		if err != nil {
			return responseRangeError("rank", err)
		}
		results = append(results, &pb.SimilarityResult{
			Vector: &pb.VectorEntity{
				Id:        vectorIDStr,
				Namespace: req.Namespace,
				Vector:    res.Vector,
				Metadata:  metadata,
			},
			Score: res.Score,
			Rank:  rank,
		})
	}

	// Send response
	return stream.Send(&pb.SearchSimilarResponse{
		Results:         results,
		TotalCandidates: int64(len(resp.Results)),
		SearchTimeMs:    searchDuration,
		NodeId:          s.nodeID,
		IsFinal:         true,
	})
}

// SearchByID performs k-NN search using an existing vector as the query.
func (s *Server) SearchByID(req *pb.SearchByIDRequest, stream pb.VectorDB_SearchByIDServer) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Namespace == "" {
		return status.Error(codes.InvalidArgument, "namespace is required")
	}

	if req.Id == "" {
		return status.Error(codes.InvalidArgument, "vector ID is required")
	}

	if req.K <= 0 {
		return status.Error(codes.InvalidArgument, "K must be positive")
	}

	const maxK = 10000
	if req.K > maxK {
		return status.Errorf(codes.InvalidArgument, "K %d exceeds maximum allowed %d", req.K, maxK)
	}

	// Check if service supports vectors
	if s.service == nil {
		return status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return status.Error(codes.Unimplemented, "vector service not available")
	}

	// Parse vector ID
	vectorID := parseVectorID(req.Id)

	// Convert consistency level
	consistency := convertProtoConsistencyLevel(req.Consistency)

	// First, get the vector
	getReq := &service.GetVectorRequest{
		Namespace:       req.Namespace,
		ID:              vectorID,
		IncludeVector:   true,
		IncludeMetadata: false,
		ShardID:         s.vectorShardID,
		Consistency:     consistency,
	}

	getResp, err := vectorService.GetVector(stream.Context(), getReq)
	if err != nil {
		return toProtoError(err)
	}

	if !getResp.Found || len(getResp.Vector) == 0 {
		return status.Error(codes.NotFound, "vector not found")
	}

	// Now perform the search
	searchReq := &service.SearchSimilarRequest{
		Namespace:       req.Namespace,
		QueryVector:     getResp.Vector,
		K:               int(req.K) + 1, // +1 to exclude self
		EfSearch:        int(req.EfSearch),
		MinScore:        req.MinScore,
		IncludeVectors:  req.IncludeVectors,
		IncludeMetadata: req.IncludeMetadata,
		MetadataFilter:  make(map[string]interface{}),
		ShardID:         s.vectorShardID,
		Consistency:     consistency,
	}

	startTime := time.Now()
	searchResp, err := vectorService.SearchSimilar(stream.Context(), searchReq)
	if err != nil {
		return toProtoError(err)
	}
	searchDuration := time.Since(startTime).Milliseconds()

	// Convert results, optionally excluding self
	results := make([]*pb.SimilarityResult, 0, len(searchResp.Results))
	for _, res := range searchResp.Results {
		// Skip self if not requested
		if !req.IncludeSelf && res.ID == vectorID {
			continue
		}

		// Extract original ID before filtering metadata
		originalID := ""
		if res.Metadata != nil {
			if val, ok := res.Metadata[MetadataKeyOriginalID]; ok {
				originalID = fmt.Sprintf("%v", val)
			}
		}

		// Convert metadata, filtering out internal keys
		metadata := make(map[string]string)
		if res.Metadata != nil {
			for k, v := range res.Metadata {
				// Filter out internal metadata keys
				if k == MetadataKeyOriginalID {
					continue
				}
				metadata[k] = fmt.Sprintf("%v", v)
			}
		}

		// Use original string ID if found, otherwise format the numeric ID
		vectorIDStr := originalID
		if originalID == "" {
			vectorIDStr = strconv.FormatUint(res.ID, 10)
		}

		rank, err := conv.SafeIntToInt32(res.Rank)
		if err != nil {
			return responseRangeError("rank", err)
		}
		results = append(results, &pb.SimilarityResult{
			Vector: &pb.VectorEntity{
				Id:        vectorIDStr,
				Namespace: req.Namespace,
				Vector:    res.Vector,
				Metadata:  metadata,
			},
			Score: res.Score,
			Rank:  rank,
		})

		// Limit to K results
		if len(results) >= int(req.K) {
			break
		}
	}

	// Send response
	return stream.Send(&pb.SearchByIDResponse{
		Results:         results,
		TotalCandidates: int64(len(searchResp.Results)),
		SearchTimeMs:    searchDuration,
		NodeId:          s.nodeID,
		IsFinal:         true,
	})
}

// HybridSearch combines similarity search with metadata filtering.
func (s *Server) HybridSearch(req *pb.HybridSearchRequest, stream pb.VectorDB_HybridSearchServer) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Namespace == "" {
		return status.Error(codes.InvalidArgument, "namespace is required")
	}

	if len(req.QueryVector) == 0 {
		return status.Error(codes.InvalidArgument, "query vector is required")
	}

	if req.K <= 0 {
		return status.Error(codes.InvalidArgument, "K must be positive")
	}

	const maxK = 10000
	if req.K > maxK {
		return status.Errorf(codes.InvalidArgument, "K %d exceeds maximum allowed %d", req.K, maxK)
	}

	// Validate query vector values for NaN and Inf
	for i, val := range req.QueryVector {
		if math.IsNaN(float64(val)) || math.IsInf(float64(val), 0) {
			return status.Errorf(codes.InvalidArgument, "query vector contains invalid value at index %d: NaN or Inf not allowed", i)
		}
	}

	// Check if service supports vectors
	if s.service == nil {
		return status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return status.Error(codes.Unimplemented, "vector service not available")
	}

	// Convert metadata filters to map
	metadataFilter := make(map[string]interface{})
	for _, filter := range req.MetadataFilters {
		metadataFilter[filter.Key] = filter.Value
	}

	// Convert consistency level
	consistency := convertProtoConsistencyLevel(req.Consistency)

	// Build service request
	serviceReq := &service.HybridSearchRequest{
		Namespace:          req.Namespace,
		QueryVector:        req.QueryVector,
		K:                  int(req.K),
		EfSearch:           int(req.EfSearch),
		MinScore:           req.MinScore,
		MetadataFilter:     metadataFilter,
		FilterOperator:     req.FilterOperator,
		RerankingAlgorithm: req.RerankingAlgorithm,
		IncludeVectors:     req.IncludeVectors,
		IncludeMetadata:    req.IncludeMetadata,
		ShardID:            s.vectorShardID,
		Consistency:        consistency,
	}

	// Set default filter operator
	if serviceReq.FilterOperator == "" {
		serviceReq.FilterOperator = "AND"
	}

	// Call service
	startTime := time.Now()
	resp, err := vectorService.HybridSearch(stream.Context(), serviceReq)
	if err != nil {
		return toProtoError(err)
	}
	searchDuration := time.Since(startTime).Milliseconds()

	// Convert results
	results := make([]*pb.SimilarityResult, 0, len(resp.Results))
	for _, res := range resp.Results {
		// Extract original ID before filtering metadata
		originalID := ""
		if res.Metadata != nil {
			if val, ok := res.Metadata[MetadataKeyOriginalID]; ok {
				originalID = fmt.Sprintf("%v", val)
			}
		}

		// Convert metadata, filtering out internal keys
		metadata := make(map[string]string)
		if res.Metadata != nil {
			for k, v := range res.Metadata {
				// Filter out internal metadata keys
				if k == MetadataKeyOriginalID {
					continue
				}
				metadata[k] = fmt.Sprintf("%v", v)
			}
		}

		// Use original string ID if found, otherwise format the numeric ID
		vectorIDStr := originalID
		if originalID == "" {
			vectorIDStr = strconv.FormatUint(res.ID, 10)
		}

		rank, err := conv.SafeIntToInt32(res.Rank)
		if err != nil {
			return responseRangeError("rank", err)
		}
		results = append(results, &pb.SimilarityResult{
			Vector: &pb.VectorEntity{
				Id:        vectorIDStr,
				Namespace: req.Namespace,
				Vector:    res.Vector,
				Metadata:  metadata,
			},
			Score: res.Score,
			Rank:  rank,
		})
	}

	// Determine if reranking was applied
	rerankingApplied := req.RerankingAlgorithm != "" && req.RerankingAlgorithm != "none"

	// Send response
	return stream.Send(&pb.HybridSearchResponse{
		Results:          results,
		TotalCandidates:  int64(resp.TotalCandidates),
		TotalFiltered:    int64(resp.TotalFiltered),
		SearchTimeMs:     searchDuration,
		RerankingApplied: rerankingApplied,
		NodeId:           s.nodeID,
		IsFinal:          true,
	})
}

// SearchByMetadata searches vectors by metadata without a query vector.
func (s *Server) SearchByMetadata(req *pb.SearchByMetadataRequest, stream pb.VectorDB_SearchByMetadataServer) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Namespace == "" {
		return status.Error(codes.InvalidArgument, "namespace is required")
	}

	if len(req.MetadataFilters) == 0 {
		return status.Error(codes.InvalidArgument, "at least one metadata filter is required")
	}

	// Validate limit
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 100
	}
	const maxLimit = 10000
	if limit > maxLimit {
		return status.Errorf(codes.InvalidArgument, "limit %d exceeds maximum allowed %d", limit, maxLimit)
	}

	// Validate offset
	offset := int(req.Offset)
	if offset < 0 {
		return status.Error(codes.InvalidArgument, "offset cannot be negative")
	}

	// Check if service supports vectors
	if s.service == nil {
		return status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return status.Error(codes.Unimplemented, "vector service not available")
	}

	// Convert metadata filters to map
	filters := make(map[string]interface{})
	for _, filter := range req.MetadataFilters {
		filters[filter.Key] = filter.Value
	}

	// Convert consistency level
	consistency := convertProtoConsistencyLevel(req.Consistency)

	// Build service request
	serviceReq := &service.SearchByMetadataRequest{
		Namespace:       req.Namespace,
		Filters:         filters,
		FilterOperator:  req.FilterOperator,
		Limit:           limit,
		Offset:          offset,
		IncludeVectors:  req.IncludeVectors,
		IncludeMetadata: req.IncludeMetadata,
		ShardID:         s.vectorShardID,
		Consistency:     consistency,
	}

	// Set default filter operator
	if serviceReq.FilterOperator == "" {
		serviceReq.FilterOperator = "AND"
	}

	// Call service
	startTime := time.Now()
	resp, err := vectorService.SearchByMetadata(stream.Context(), serviceReq)
	if err != nil {
		return toProtoError(err)
	}
	searchDuration := time.Since(startTime).Milliseconds()

	// Convert results
	results := make([]*pb.VectorEntity, 0, len(resp.Results))
	for _, vec := range resp.Results {
		// Extract original ID before filtering metadata
		originalID := ""
		if vec.Metadata != nil {
			if val, ok := vec.Metadata[MetadataKeyOriginalID]; ok {
				originalID = fmt.Sprintf("%v", val)
			}
		}

		// Convert metadata, filtering out internal keys
		metadata := make(map[string]string)
		if vec.Metadata != nil {
			for k, v := range vec.Metadata {
				// Filter out internal metadata keys
				if k == MetadataKeyOriginalID {
					continue
				}
				metadata[k] = fmt.Sprintf("%v", v)
			}
		}

		// Use original string ID if found, otherwise format the numeric ID
		vectorIDStr := originalID
		if originalID == "" {
			vectorIDStr = strconv.FormatUint(vec.ID, 10)
		}

		results = append(results, &pb.VectorEntity{
			Id:        vectorIDStr,
			Namespace: req.Namespace,
			Vector:    vec.Vector,
			Metadata:  metadata,
		})
	}

	totalCount, err := conv.SafeUint64ToInt64(resp.TotalCount)
	if err != nil {
		return responseRangeError("total_count", err)
	}

	// Send response
	return stream.Send(&pb.SearchByMetadataResponse{
		Results:      results,
		TotalCount:   totalCount,
		NextCursor:   resp.NextCursor,
		SearchTimeMs: searchDuration,
		NodeId:       s.nodeID,
		IsFinal:      true,
	})
}

// ListVectors returns all vectors in a namespace with pagination support.
func (s *Server) ListVectors(req *pb.ListVectorsRequest, stream pb.VectorDB_ListVectorsServer) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Namespace == "" {
		return status.Error(codes.InvalidArgument, "namespace is required")
	}

	if req.PageSize < 0 {
		return status.Error(codes.InvalidArgument, "page size cannot be negative")
	}

	const maxPageSize = 1000
	if req.PageSize > maxPageSize {
		return status.Errorf(codes.InvalidArgument, "page size cannot exceed %d", maxPageSize)
	}

	// Check if service supports vectors
	if s.service == nil {
		return status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return status.Error(codes.Unimplemented, "vector service not available")
	}

	// Check if namespace exists first
	nsInfoReq := &service.GetNamespaceInfoRequest{
		Namespace: req.Namespace,
	}
	_, err := vectorService.GetNamespaceInfo(stream.Context(), nsInfoReq)
	if err != nil {
		return toProtoError(err)
	}

	// For now, use SearchByMetadata with no filters to list all vectors
	// This is a simplified implementation
	pageSize := int(req.PageSize)
	if pageSize <= 0 {
		pageSize = 100
	}

	consistency := convertProtoConsistencyLevel(req.Consistency)

	serviceReq := &service.SearchByMetadataRequest{
		Namespace:       req.Namespace,
		Filters:         make(map[string]interface{}),
		FilterOperator:  "AND",
		Limit:           pageSize,
		Offset:          0,
		IncludeVectors:  req.IncludeVectors,
		IncludeMetadata: req.IncludeMetadata,
		ShardID:         s.vectorShardID,
		Consistency:     consistency,
	}

	// Call service
	resp, err := vectorService.SearchByMetadata(stream.Context(), serviceReq)
	if err != nil {
		return toProtoError(err)
	}

	// Convert results
	vectors := make([]*pb.VectorEntity, 0, len(resp.Results))
	for _, vec := range resp.Results {
		// Extract original ID before filtering metadata
		originalID := ""
		if vec.Metadata != nil {
			if val, ok := vec.Metadata[MetadataKeyOriginalID]; ok {
				originalID = fmt.Sprintf("%v", val)
			}
		}

		// Convert metadata, filtering out internal keys
		metadata := make(map[string]string)
		if vec.Metadata != nil {
			for k, v := range vec.Metadata {
				// Filter out internal metadata keys
				if k == MetadataKeyOriginalID {
					continue
				}
				metadata[k] = fmt.Sprintf("%v", v)
			}
		}

		// Use original string ID if found, otherwise format the numeric ID
		vectorIDStr := originalID
		if originalID == "" {
			vectorIDStr = strconv.FormatUint(vec.ID, 10)
		}

		vectors = append(vectors, &pb.VectorEntity{
			Id:        vectorIDStr,
			Namespace: req.Namespace,
			Vector:    vec.Vector,
			Metadata:  metadata,
			Version:   vec.Version,
		})
	}

	totalCount, err := conv.SafeUint64ToInt64(resp.TotalCount)
	if err != nil {
		return responseRangeError("total_count", err)
	}

	// Send response
	return stream.Send(&pb.ListVectorsResponse{
		Vectors:       vectors,
		NextPageToken: resp.NextCursor,
		TotalCount:    totalCount,
		IsFinal:       true,
	})
}

// ========================================
// Statistics and Health
// ========================================

// GetVectorStats returns statistics for a namespace or the entire database.
func (s *Server) GetVectorStats(ctx context.Context, req *pb.GetVectorStatsRequest) (*pb.GetVectorStatsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Check if service supports vectors
	if s.service == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}

	// If namespace is specified, get namespace-specific stats
	if req.Namespace != "" {
		statsReq := &service.GetIndexStatsRequest{
			Namespace: req.Namespace,
			ShardID:   s.vectorShardID,
		}

		stats, err := vectorService.GetIndexStats(ctx, statsReq)
		if err != nil {
			return nil, toProtoError(err)
		}

		nsStats, err := namespaceStatsToProto(stats)
		if err != nil {
			return nil, err
		}

		return &pb.GetVectorStatsResponse{
			TotalVectors:      nsStats.VectorCount,
			TotalNamespaces:   1,
			TotalStorageBytes: nsStats.StorageBytes,
			TotalIndexBytes:   nsStats.IndexBytes,
			AvgDimension:      float64(stats.Dimension),
			NamespaceStats:    []*pb.NamespaceStats{nsStats},
		}, nil
	}

	// Otherwise, get cluster-wide stats by listing all namespaces
	listReq := &service.ListNamespacesRequest{
		Limit:   1000,
		ShardID: s.vectorShardID,
	}

	listResp, err := vectorService.ListNamespaces(ctx, listReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	totalNamespaces, err := conv.SafeIntToInt32(len(listResp.Namespaces))
	if err != nil {
		return nil, responseRangeError("total_namespaces", err)
	}

	var totalVectors int64
	var totalStorage int64
	var totalIndex int64
	var totalDimensions int64
	namespaceStats := make([]*pb.NamespaceStats, 0, len(listResp.Namespaces))

	for _, ns := range listResp.Namespaces {
		statsReq := &service.GetIndexStatsRequest{
			Namespace: ns.Namespace,
			ShardID:   s.vectorShardID,
		}

		stats, err := vectorService.GetIndexStats(ctx, statsReq)
		if err != nil {
			continue
		}

		nsStats, err := namespaceStatsToProto(stats)
		if err != nil {
			return nil, err
		}

		totalVectors += nsStats.VectorCount
		totalStorage += nsStats.StorageBytes
		totalIndex += nsStats.IndexBytes
		totalDimensions += int64(stats.Dimension)

		if req.IncludeDetails {
			namespaceStats = append(namespaceStats, nsStats)
		}
	}

	avgDimension := 0.0
	if len(listResp.Namespaces) > 0 {
		avgDimension = float64(totalDimensions) / float64(len(listResp.Namespaces))
	}

	return &pb.GetVectorStatsResponse{
		TotalVectors:      totalVectors,
		TotalNamespaces:   totalNamespaces,
		TotalStorageBytes: totalStorage,
		TotalIndexBytes:   totalIndex,
		AvgDimension:      avgDimension,
		NamespaceStats:    namespaceStats,
	}, nil
}

// namespaceStatsToProto converts one namespace's index stats to the wire
// form. The service counts in uint64 and the proto in int64, so a counter
// past math.MaxInt64 is reported as codes.Internal rather than wrapped.
func namespaceStatsToProto(stats *service.GetIndexStatsResponse) (*pb.NamespaceStats, error) {
	vectorCount, err := conv.SafeUint64ToInt64(stats.VectorCount)
	if err != nil {
		return nil, responseRangeError("vector_count", err)
	}
	indexBytes, err := conv.SafeUint64ToInt64(stats.IndexSizeBytes)
	if err != nil {
		return nil, responseRangeError("index_bytes", err)
	}

	nsStats := &pb.NamespaceStats{
		Namespace:    stats.Namespace,
		VectorCount:  vectorCount,
		IndexedCount: vectorCount,
		StorageBytes: indexBytes,
		IndexBytes:   indexBytes,
	}
	if perf := stats.SearchPerformance; perf != nil {
		searchCount, err := conv.SafeUint64ToInt64(perf.TotalSearches)
		if err != nil {
			return nil, responseRangeError("search_count", err)
		}
		nsStats.AvgSearchLatencyMs = perf.AverageSearchTimeMs
		nsStats.SearchCount = searchCount
	}
	return nsStats, nil
}

// ========================================
// Index Operations
// ========================================

// RebuildIndex rebuilds the vector index for a namespace.
func (s *Server) RebuildIndex(ctx context.Context, req *pb.RebuildIndexRequest) (*pb.RebuildIndexResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Namespace == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace is required")
	}

	// Check if service supports vectors
	if s.service == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}

	// Convert index params
	indexConfig := make(map[string]string)
	if req.IndexParams != nil {
		for k, v := range req.IndexParams {
			indexConfig[k] = v
		}
	}

	serviceReq := &service.RebuildIndexRequest{
		Namespace:   req.Namespace,
		IndexConfig: indexConfig,
		Async:       req.Async,
		ShardID:     s.vectorShardID,
	}

	// Call service
	resp, err := vectorService.RebuildIndex(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.RebuildIndexResponse{
		OperationId:         resp.JobID,
		Started:             timestamppb.New(time.Unix(0, resp.Timestamp)),
		EstimatedDurationMs: resp.EstimatedTimeRemaining * 1000, // Convert seconds to milliseconds
	}, nil
}

// GetIndexProgress returns the progress of an ongoing index rebuild.
func (s *Server) GetIndexProgress(ctx context.Context, req *pb.GetIndexProgressRequest) (*pb.GetIndexProgressResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.OperationId == "" && req.Namespace == "" {
		return nil, status.Error(codes.InvalidArgument, "operation_id or namespace is required")
	}

	// Check if service supports vectors
	if s.service == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}

	serviceReq := &service.GetRebuildProgressRequest{
		JobID:     req.OperationId,
		Namespace: req.Namespace,
	}

	// Call service to get rebuild progress
	resp, err := vectorService.GetRebuildProgress(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	vectorsIndexed, err := conv.SafeUint64ToInt64(resp.VectorsProcessed)
	if err != nil {
		return nil, responseRangeError("vectors_indexed", err)
	}
	totalVectors, err := conv.SafeUint64ToInt64(resp.TotalVectors)
	if err != nil {
		return nil, responseRangeError("total_vectors", err)
	}

	return &pb.GetIndexProgressResponse{
		OperationId:          resp.JobID,
		Namespace:            resp.Namespace,
		VectorsIndexed:       vectorsIndexed,
		TotalVectors:         totalVectors,
		PercentComplete:      resp.PercentComplete,
		ElapsedTimeMs:        resp.ElapsedTimeMs,
		EstimatedRemainingMs: resp.EstimatedTimeRemainingMs,
		Status:               resp.Status,
	}, nil
}

// ========================================
// VectorAdmin Service Implementation
// ========================================

// GetClusterStats returns cluster-wide vector database statistics.
func (s *Server) GetClusterStats(ctx context.Context, req *pb.GetClusterStatsRequest) (*pb.GetClusterStatsResponse, error) {
	if req == nil {
		req = &pb.GetClusterStatsRequest{}
	}

	// Check if service supports vectors
	if s.service == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}

	// Get cluster status to determine node count and addresses
	clusterReq := &service.ClusterStatusRequest{
		IncludeDetails: true,
	}
	clusterResp, clusterErr := s.service.ClusterStatus(ctx, clusterReq)

	// Calculate actual cluster size and build node address map
	totalNodes := int32(1) // Default to single node
	nodeAddresses := make(map[uint64]string)
	if clusterErr == nil && clusterResp != nil && len(clusterResp.Nodes) > 0 {
		nodeCount, err := conv.SafeIntToInt32(len(clusterResp.Nodes))
		if err != nil {
			return nil, responseRangeError("total_nodes", err)
		}
		totalNodes = nodeCount
		for _, node := range clusterResp.Nodes {
			nodeAddresses[node.NodeID] = node.Address
		}
	}

	// List all namespaces
	listReq := &service.ListNamespacesRequest{
		Limit:   10000,
		ShardID: s.vectorShardID,
	}

	listResp, err := vectorService.ListNamespaces(ctx, listReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	totalNamespaces, err := conv.SafeIntToInt32(len(listResp.Namespaces))
	if err != nil {
		return nil, responseRangeError("total_namespaces", err)
	}

	var totalVectors int64
	var totalStorage int64
	var totalIndex int64
	namespaceStats := make([]*pb.NamespaceStats, 0)

	for _, ns := range listResp.Namespaces {
		statsReq := &service.GetIndexStatsRequest{
			Namespace: ns.Namespace,
			ShardID:   s.vectorShardID,
		}

		stats, err := vectorService.GetIndexStats(ctx, statsReq)
		if err != nil {
			continue
		}

		nsStats, err := namespaceStatsToProto(stats)
		if err != nil {
			return nil, err
		}

		totalVectors += nsStats.VectorCount
		totalStorage += nsStats.StorageBytes
		totalIndex += nsStats.IndexBytes

		if req.IncludeNamespaces {
			namespaceStats = append(namespaceStats, nsStats)
		}
	}

	// Get actual QPS from the QPS tracker
	currentQPS := s.QPS()

	// Get node address - prefer from cluster status, fall back to configured address
	nodeAddress := s.address
	if nodeAddress == "" {
		if addr, ok := nodeAddresses[s.nodeID]; ok && addr != "" {
			nodeAddress = addr
		} else {
			nodeAddress = "localhost"
		}
	}

	nodeStats := make([]*pb.NodeStats, 0)
	if req.IncludeNodes {
		// Create node stats for this node
		nodeStats = append(nodeStats, &pb.NodeStats{
			NodeId:           s.nodeID,
			Address:          nodeAddress,
			VectorCount:      totalVectors,
			StorageBytes:     totalStorage,
			IndexBytes:       totalIndex,
			Qps:              currentQPS,
			AvgLatencyMs:     0.0,
			CpuUsage:         0.0,
			MemoryUsageBytes: 0,
		})
	}

	return &pb.GetClusterStatsResponse{
		ClusterId:         "quicbaas-cluster",
		TotalNodes:        totalNodes,
		TotalVectors:      totalVectors,
		TotalNamespaces:   totalNamespaces,
		TotalStorageBytes: totalStorage,
		TotalIndexBytes:   totalIndex,
		ClusterQps:        currentQPS,
		NamespaceStats:    namespaceStats,
		NodeStats:         nodeStats,
		Timestamp:         timestamppb.Now(),
	}, nil
}

// CompactNamespace performs compaction on a namespace to reclaim disk space.
func (s *Server) CompactNamespace(ctx context.Context, req *pb.CompactNamespaceRequest) (*pb.CompactNamespaceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Namespace == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace is required")
	}

	// Check if service supports vectors
	if s.service == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}

	// Build service request
	serviceReq := &service.CompactNamespaceRequest{
		Namespace: req.Namespace,
		Async:     req.Async,
		TimeoutMs: req.TimeoutMs,
		ShardID:   s.vectorShardID,
	}

	// Call service
	resp, err := vectorService.CompactNamespace(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.CompactNamespaceResponse{
		OperationId: resp.OperationID,
		Started:     timestamppb.New(time.Unix(0, resp.StartedAt)),
	}, nil
}

// GetCompactionProgress returns the progress of an ongoing compaction.
func (s *Server) GetCompactionProgress(ctx context.Context, req *pb.GetCompactionProgressRequest) (*pb.GetCompactionProgressResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Namespace == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace is required")
	}

	// Check if service supports vectors
	if s.service == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return nil, status.Error(codes.Unimplemented, "vector service not available")
	}

	// Build service request
	serviceReq := &service.GetCompactionProgressRequest{
		Namespace:   req.Namespace,
		OperationID: req.OperationId,
		ShardID:     s.vectorShardID,
	}

	// Call service
	resp, err := vectorService.GetCompactionProgress(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	// Calculate elapsed time
	elapsedTimeMs := int64(0)
	if resp.StartedAt > 0 {
		elapsedTimeMs = (time.Now().UnixNano() - resp.StartedAt) / int64(time.Millisecond)
	}

	// Determine status string.
	var statusStr string
	switch {
	case resp.InProgress:
		statusStr = "in_progress"
	case resp.Error != "":
		statusStr = "failed"
	default:
		statusStr = "completed"
	}

	return &pb.GetCompactionProgressResponse{
		OperationId:         resp.OperationID,
		Namespace:           resp.Namespace,
		BytesCompacted:      resp.BytesCompacted,
		TotalBytes:          resp.TotalBytes,
		PercentComplete:     resp.PercentComplete,
		ElapsedTimeMs:       elapsedTimeMs,
		SpaceReclaimedBytes: resp.BytesCompacted, // Same as bytes compacted
		Status:              statusStr,
	}, nil
}

// ExportVectors exports vectors from a namespace for backup or migration.
func (s *Server) ExportVectors(req *pb.ExportVectorsRequest, stream pb.VectorAdmin_ExportVectorsServer) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Namespace == "" {
		return status.Error(codes.InvalidArgument, "namespace is required")
	}

	// Check if service supports vectors
	if s.service == nil {
		return status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return status.Error(codes.Unimplemented, "vector service not available")
	}

	// A failed Send is returned as-is; service errors map to gRPC codes.
	var sendErr error
	err := service.ExportVectors(stream.Context(), vectorService, &service.ExportVectorsRequest{
		Namespace:   req.Namespace,
		Format:      req.Format,
		Compression: req.Compression,
		BatchSize:   int(req.BatchSize),
		ShardID:     s.vectorShardID,
	}, func(chunk *service.ExportVectorsChunk) error {
		sendErr = stream.Send(&pb.ExportVectorsResponse{
			BatchData:       chunk.Data,
			VectorsExported: chunk.VectorsExported,
			BatchNumber:     chunk.BatchNumber,
			IsFinal:         chunk.IsFinal,
			Checksum:        chunk.Checksum,
		})
		return sendErr
	})
	if sendErr != nil {
		return sendErr
	}
	return toProtoError(err)
}

// ImportVectors imports vectors into a namespace from a backup or migration.
func (s *Server) ImportVectors(stream pb.VectorAdmin_ImportVectorsServer) error {
	// Check if service supports vectors
	if s.service == nil {
		return status.Error(codes.Unimplemented, "vector service not available")
	}
	vectorService := s.service.Vector()
	if vectorService == nil {
		return status.Error(codes.Unimplemented, "vector service not available")
	}

	var namespace string
	var format string
	var overwriteExisting bool
	var total service.ImportVectorsResult

	startTime := time.Now()

	for {
		req, err := stream.Recv()
		if err != nil {
			// Stream ended (EOF or error)
			break
		}

		// First message contains namespace and format
		if namespace == "" {
			if req.Namespace == "" {
				return status.Error(codes.InvalidArgument, "namespace required in first message")
			}
			namespace = req.Namespace
			format = req.Format
			overwriteExisting = req.OverwriteExisting
		}

		// A batch that fails is counted in the result; the stream carries on.
		result, err := service.ImportVectors(stream.Context(), vectorService, &service.ImportVectorsRequest{
			Namespace:         namespace,
			Format:            format,
			Data:              req.BatchData,
			OverwriteExisting: overwriteExisting,
			ShardID:           s.vectorShardID,
		})
		if err != nil {
			s.logger.WithError(err).Warnf("Failed to import vector batch into %s", namespace)
		}
		total.Imported += result.Imported
		total.Skipped += result.Skipped
		total.Failed += result.Failed
	}

	return stream.SendAndClose(&pb.ImportVectorsResponse{
		VectorsImported: total.Imported,
		VectorsSkipped:  total.Skipped,
		VectorsFailed:   total.Failed,
		DurationMs:      time.Since(startTime).Milliseconds(),
		Timestamp:       timestamppb.Now(),
	})
}

// ========================================
// Helper Functions
// ========================================

const (
	// MetadataKeyOriginalID is the metadata key for storing original string IDs.
	MetadataKeyOriginalID = "__original_id"
)

// parseVectorID converts a string ID to uint64 and stores the original in metadata.
func parseVectorID(id string) uint64 {
	return service.ParseVectorID(id)
}

// formatVectorID converts a uint64 ID to string, checking metadata for original ID.
func formatVectorID(id uint64, metadata map[string]string) string {
	// Check if we have the original string ID in metadata
	if metadata != nil {
		if originalID, ok := metadata[MetadataKeyOriginalID]; ok {
			return originalID
		}
	}
	// Fall back to numeric representation
	return strconv.FormatUint(id, 10)
}

// storeOriginalID stores the original string ID in metadata if it's not numeric.
func storeOriginalID(id string, metadata map[string]interface{}) {
	// Only store if the ID is not a pure number
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		metadata[MetadataKeyOriginalID] = id
	}
}

// convertProtoConsistencyLevel converts proto consistency level to service consistency level.
func convertProtoConsistencyLevel(level pb.VectorConsistencyLevel) service.ConsistencyLevel {
	switch level {
	case pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_ALL:
		return service.ConsistencyLevelStrong
	case pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM:
		return service.ConsistencyLevelLinearizable
	case pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_ONE:
		return service.ConsistencyLevelEventual
	default:
		return service.ConsistencyLevelLinearizable // Default to quorum
	}
}

// protoDistanceMetricToString converts proto distance metric to string.
func protoDistanceMetricToString(metric pb.DistanceMetric) string {
	switch metric {
	case pb.DistanceMetric_DISTANCE_METRIC_L2:
		return "L2"
	case pb.DistanceMetric_DISTANCE_METRIC_COSINE:
		return "COSINE"
	case pb.DistanceMetric_DISTANCE_METRIC_DOT_PRODUCT:
		return "DOT_PRODUCT"
	case pb.DistanceMetric_DISTANCE_METRIC_MANHATTAN:
		return "MANHATTAN"
	default:
		return "L2"
	}
}

// stringToProtoDistanceMetric converts string distance metric to proto.
func stringToProtoDistanceMetric(metric string) pb.DistanceMetric {
	switch metric {
	case "L2":
		return pb.DistanceMetric_DISTANCE_METRIC_L2
	case "COSINE":
		return pb.DistanceMetric_DISTANCE_METRIC_COSINE
	case "DOT_PRODUCT":
		return pb.DistanceMetric_DISTANCE_METRIC_DOT_PRODUCT
	case "MANHATTAN":
		return pb.DistanceMetric_DISTANCE_METRIC_MANHATTAN
	default:
		return pb.DistanceMetric_DISTANCE_METRIC_L2
	}
}

// protoIndexTypeToString converts proto index type to string.
func protoIndexTypeToString(indexType pb.IndexType) string {
	switch indexType {
	case pb.IndexType_INDEX_TYPE_HNSW:
		return "HNSW"
	case pb.IndexType_INDEX_TYPE_FLAT:
		return "FLAT"
	case pb.IndexType_INDEX_TYPE_IVF_FLAT:
		return "IVF_FLAT"
	case pb.IndexType_INDEX_TYPE_IVF_PQ:
		return "IVF_PQ"
	default:
		return "HNSW"
	}
}

// stringToProtoIndexType converts string index type to proto.
func stringToProtoIndexType(indexType string) pb.IndexType {
	switch indexType {
	case "HNSW":
		return pb.IndexType_INDEX_TYPE_HNSW
	case "FLAT":
		return pb.IndexType_INDEX_TYPE_FLAT
	case "IVF_FLAT":
		return pb.IndexType_INDEX_TYPE_IVF_FLAT
	case "IVF_PQ":
		return pb.IndexType_INDEX_TYPE_IVF_PQ
	default:
		return pb.IndexType_INDEX_TYPE_HNSW
	}
}
