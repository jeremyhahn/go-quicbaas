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

// Package mcp implements mock vector clients for testing
package mcp

import (
	"context"
	"fmt"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// ========================================
// Mock Streaming Clients
// ========================================

// mockBatchUpsertStream implements a mock streaming client for BatchUpsertVectors
type mockBatchUpsertStream struct {
	grpc.ClientStream
	responses []*pb.BatchUpsertVectorsResponse
	index     int
}

func (m *mockBatchUpsertStream) Recv() (*pb.BatchUpsertVectorsResponse, error) {
	if m.index >= len(m.responses) {
		return nil, io.EOF
	}
	resp := m.responses[m.index]
	m.index++
	return resp, nil
}

func (m *mockBatchUpsertStream) Header() (metadata.MD, error) { return nil, nil }
func (m *mockBatchUpsertStream) Trailer() metadata.MD         { return nil }
func (m *mockBatchUpsertStream) CloseSend() error             { return nil }
func (m *mockBatchUpsertStream) Context() context.Context     { return context.Background() }
func (m *mockBatchUpsertStream) SendMsg(interface{}) error    { return nil }
func (m *mockBatchUpsertStream) RecvMsg(interface{}) error    { return nil }

// mockSearchSimilarStream implements a mock streaming client for SearchSimilar
type mockSearchSimilarStream struct {
	grpc.ClientStream
	responses []*pb.SearchSimilarResponse
	index     int
}

func (m *mockSearchSimilarStream) Recv() (*pb.SearchSimilarResponse, error) {
	if m.index >= len(m.responses) {
		return nil, io.EOF
	}
	resp := m.responses[m.index]
	m.index++
	return resp, nil
}

func (m *mockSearchSimilarStream) Header() (metadata.MD, error) { return nil, nil }
func (m *mockSearchSimilarStream) Trailer() metadata.MD         { return nil }
func (m *mockSearchSimilarStream) CloseSend() error             { return nil }
func (m *mockSearchSimilarStream) Context() context.Context     { return context.Background() }
func (m *mockSearchSimilarStream) SendMsg(interface{}) error    { return nil }
func (m *mockSearchSimilarStream) RecvMsg(interface{}) error    { return nil }

// mockSearchByIDStream implements a mock streaming client for SearchByID
type mockSearchByIDStream struct {
	grpc.ClientStream
	responses []*pb.SearchByIDResponse
	index     int
}

func (m *mockSearchByIDStream) Recv() (*pb.SearchByIDResponse, error) {
	if m.index >= len(m.responses) {
		return nil, io.EOF
	}
	resp := m.responses[m.index]
	m.index++
	return resp, nil
}

func (m *mockSearchByIDStream) Header() (metadata.MD, error) { return nil, nil }
func (m *mockSearchByIDStream) Trailer() metadata.MD         { return nil }
func (m *mockSearchByIDStream) CloseSend() error             { return nil }
func (m *mockSearchByIDStream) Context() context.Context     { return context.Background() }
func (m *mockSearchByIDStream) SendMsg(interface{}) error    { return nil }
func (m *mockSearchByIDStream) RecvMsg(interface{}) error    { return nil }

// mockHybridSearchStream implements a mock streaming client for HybridSearch
type mockHybridSearchStream struct {
	grpc.ClientStream
	responses []*pb.HybridSearchResponse
	index     int
}

func (m *mockHybridSearchStream) Recv() (*pb.HybridSearchResponse, error) {
	if m.index >= len(m.responses) {
		return nil, io.EOF
	}
	resp := m.responses[m.index]
	m.index++
	return resp, nil
}

func (m *mockHybridSearchStream) Header() (metadata.MD, error) { return nil, nil }
func (m *mockHybridSearchStream) Trailer() metadata.MD         { return nil }
func (m *mockHybridSearchStream) CloseSend() error             { return nil }
func (m *mockHybridSearchStream) Context() context.Context     { return context.Background() }
func (m *mockHybridSearchStream) SendMsg(interface{}) error    { return nil }
func (m *mockHybridSearchStream) RecvMsg(interface{}) error    { return nil }

// mockListVectorsStream implements a mock streaming client for ListVectors
type mockListVectorsStream struct {
	grpc.ClientStream
	responses []*pb.ListVectorsResponse
	index     int
}

func (m *mockListVectorsStream) Recv() (*pb.ListVectorsResponse, error) {
	if m.index >= len(m.responses) {
		return nil, io.EOF
	}
	resp := m.responses[m.index]
	m.index++
	return resp, nil
}

func (m *mockListVectorsStream) Header() (metadata.MD, error) { return nil, nil }
func (m *mockListVectorsStream) Trailer() metadata.MD         { return nil }
func (m *mockListVectorsStream) CloseSend() error             { return nil }
func (m *mockListVectorsStream) Context() context.Context     { return context.Background() }
func (m *mockListVectorsStream) SendMsg(interface{}) error    { return nil }
func (m *mockListVectorsStream) RecvMsg(interface{}) error    { return nil }

// mockExportVectorsStream implements a mock streaming client for ExportVectors
type mockExportVectorsStream struct {
	grpc.ClientStream
	responses []*pb.ExportVectorsResponse
	index     int
}

func (m *mockExportVectorsStream) Recv() (*pb.ExportVectorsResponse, error) {
	if m.index >= len(m.responses) {
		return nil, io.EOF
	}
	resp := m.responses[m.index]
	m.index++
	return resp, nil
}

func (m *mockExportVectorsStream) Header() (metadata.MD, error) { return nil, nil }
func (m *mockExportVectorsStream) Trailer() metadata.MD         { return nil }
func (m *mockExportVectorsStream) CloseSend() error             { return nil }
func (m *mockExportVectorsStream) Context() context.Context     { return context.Background() }
func (m *mockExportVectorsStream) SendMsg(interface{}) error    { return nil }
func (m *mockExportVectorsStream) RecvMsg(interface{}) error    { return nil }

// mockImportVectorsStream implements a mock streaming client for ImportVectors
type mockImportVectorsStream struct {
	grpc.ClientStream
	sent     []*pb.ImportVectorsRequest
	response *pb.ImportVectorsResponse
}

func (m *mockImportVectorsStream) Send(req *pb.ImportVectorsRequest) error {
	m.sent = append(m.sent, req)
	return nil
}

func (m *mockImportVectorsStream) CloseAndRecv() (*pb.ImportVectorsResponse, error) {
	if m.response == nil {
		// Default response
		m.response = &pb.ImportVectorsResponse{
			VectorsImported: int64(len(m.sent)),
			VectorsSkipped:  0,
			VectorsFailed:   0,
			DurationMs:      100,
			Timestamp:       timestamppb.Now(),
		}
	}
	return m.response, nil
}

func (m *mockImportVectorsStream) Header() (metadata.MD, error) { return nil, nil }
func (m *mockImportVectorsStream) Trailer() metadata.MD         { return nil }
func (m *mockImportVectorsStream) CloseSend() error             { return nil }
func (m *mockImportVectorsStream) Context() context.Context     { return context.Background() }
func (m *mockImportVectorsStream) SendMsg(interface{}) error    { return nil }
func (m *mockImportVectorsStream) RecvMsg(interface{}) error    { return nil }

// ========================================
// Mock VectorDB Client
// ========================================

// mockVectorDBClient wraps the interface and overrides methods for testing
type mockVectorDBClient struct{}

// Ensure mockVectorDBClient implements pb.VectorDBClient
var _ pb.VectorDBClient = (*mockVectorDBClient)(nil)

// UpsertVector mocks the UpsertVector RPC call
func (m *mockVectorDBClient) UpsertVector(ctx context.Context, in *pb.UpsertVectorRequest, opts ...grpc.CallOption) (*pb.UpsertVectorResponse, error) {
	return &pb.UpsertVectorResponse{
		Id:           in.Vector.Id,
		Version:      1,
		Timestamp:    timestamppb.Now(),
		LeaderId:     1,
		IndexUpdated: !in.SkipIndexUpdate,
	}, nil
}

// BatchUpsertVectors mocks batch upsert with streaming response
func (m *mockVectorDBClient) BatchUpsertVectors(ctx context.Context, in *pb.BatchUpsertVectorsRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[pb.BatchUpsertVectorsResponse], error) {
	stream := &mockBatchUpsertStream{
		responses: []*pb.BatchUpsertVectorsResponse{
			{
				VectorsProcessed: int64(len(in.Vectors)),
				VectorsSucceeded: int64(len(in.Vectors)),
				VectorsFailed:    0,
				FailedIds:        []string{},
				IsFinal:          true,
			},
		},
	}
	return stream, nil
}

// GetVector mocks get vector operation
func (m *mockVectorDBClient) GetVector(ctx context.Context, in *pb.GetVectorRequest, opts ...grpc.CallOption) (*pb.GetVectorResponse, error) {
	return &pb.GetVectorResponse{
		Found: true,
		Vector: &pb.VectorEntity{
			Id:        in.Id,
			Namespace: in.Namespace,
			Vector:    []float32{0.1, 0.2, 0.3},
			Metadata:  map[string]string{"type": "test"},
			Version:   1,
		},
		NodeId: 1,
	}, nil
}

// DeleteVector mocks delete vector operation
func (m *mockVectorDBClient) DeleteVector(ctx context.Context, in *pb.DeleteVectorRequest, opts ...grpc.CallOption) (*pb.DeleteVectorResponse, error) {
	return &pb.DeleteVectorResponse{
		Deleted:   true,
		Timestamp: timestamppb.Now(),
	}, nil
}

// BatchDeleteVectors mocks batch delete operation
func (m *mockVectorDBClient) BatchDeleteVectors(ctx context.Context, in *pb.BatchDeleteVectorsRequest, opts ...grpc.CallOption) (*pb.BatchDeleteVectorsResponse, error) {
	return &pb.BatchDeleteVectorsResponse{
		DeletedCount: int64(len(in.Ids)),
		Timestamp:    timestamppb.Now(),
	}, nil
}

// SearchSimilar mocks similarity search with streaming results
func (m *mockVectorDBClient) SearchSimilar(ctx context.Context, in *pb.SearchSimilarRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[pb.SearchSimilarResponse], error) {
	// Create mock results based on k parameter
	results := make([]*pb.SimilarityResult, in.K)
	for i := int32(0); i < in.K; i++ {
		results[i] = &pb.SimilarityResult{
			Vector: &pb.VectorEntity{
				Id:        "test-vector-" + string(rune('1'+i)),
				Namespace: in.Namespace,
				Vector:    in.QueryVector, // Echo back the query vector
				Metadata:  map[string]string{"type": "test"},
			},
			Score: 0.9 - float32(i)*0.05, // Decreasing scores
			Rank:  i + 1,
		}
	}

	stream := &mockSearchSimilarStream{
		responses: []*pb.SearchSimilarResponse{
			{
				Results:         results,
				TotalCandidates: int64(in.K) * 2,
				SearchTimeMs:    10,
				NodeId:          1,
			},
		},
	}

	return stream, nil
}

// SearchByID mocks search by ID with streaming results
func (m *mockVectorDBClient) SearchByID(ctx context.Context, in *pb.SearchByIDRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[pb.SearchByIDResponse], error) {
	// Create mock results
	results := make([]*pb.SimilarityResult, in.K)
	for i := int32(0); i < in.K; i++ {
		results[i] = &pb.SimilarityResult{
			Vector: &pb.VectorEntity{
				Id:        "similar-vector-" + string(rune('1'+i)),
				Namespace: in.Namespace,
				Vector:    []float32{0.1, 0.2, 0.3},
				Metadata:  map[string]string{"similar": "true"},
			},
			Score: 0.85 - float32(i)*0.05,
			Rank:  i + 1,
		}
	}

	stream := &mockSearchByIDStream{
		responses: []*pb.SearchByIDResponse{
			{
				Results:         results,
				TotalCandidates: int64(in.K) * 3,
				SearchTimeMs:    15,
				NodeId:          1,
			},
		},
	}

	return stream, nil
}

// ListVectors mocks list vectors with streaming results
func (m *mockVectorDBClient) ListVectors(ctx context.Context, in *pb.ListVectorsRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[pb.ListVectorsResponse], error) {
	// Create mock vectors
	vectors := make([]*pb.VectorEntity, 0, in.PageSize)
	for i := int32(0); i < in.PageSize && i < 10; i++ {
		vectors = append(vectors, &pb.VectorEntity{
			Id:        "vector-" + string(rune('1'+i)),
			Namespace: in.Namespace,
			Vector:    []float32{0.1, 0.2, 0.3},
			Metadata:  map[string]string{"index": string(rune('1' + i))},
			Version:   1,
		})
	}

	stream := &mockListVectorsStream{
		responses: []*pb.ListVectorsResponse{
			{
				Vectors:       vectors,
				NextPageToken: "",
				TotalCount:    int64(len(vectors)),
			},
		},
	}

	return stream, nil
}

// CreateNamespace mocks namespace creation
func (m *mockVectorDBClient) CreateNamespace(ctx context.Context, in *pb.CreateNamespaceRequest, opts ...grpc.CallOption) (*pb.CreateNamespaceResponse, error) {
	return &pb.CreateNamespaceResponse{
		Namespace: in.Config.Namespace,
		Timestamp: timestamppb.Now(),
	}, nil
}

// UpdateNamespace mocks namespace update
func (m *mockVectorDBClient) UpdateNamespace(ctx context.Context, in *pb.UpdateNamespaceRequest, opts ...grpc.CallOption) (*pb.UpdateNamespaceResponse, error) {
	return &pb.UpdateNamespaceResponse{
		Namespace:       in.Namespace,
		Timestamp:       timestamppb.Now(),
		RequiresRebuild: false,
	}, nil
}

// DeleteNamespace mocks namespace deletion
func (m *mockVectorDBClient) DeleteNamespace(ctx context.Context, in *pb.DeleteNamespaceRequest, opts ...grpc.CallOption) (*pb.DeleteNamespaceResponse, error) {
	return &pb.DeleteNamespaceResponse{
		Deleted:        true,
		VectorsDeleted: 1000,
		Timestamp:      timestamppb.Now(),
	}, nil
}

// GetNamespaceInfo mocks namespace info retrieval
func (m *mockVectorDBClient) GetNamespaceInfo(ctx context.Context, in *pb.GetNamespaceInfoRequest, opts ...grpc.CallOption) (*pb.GetNamespaceInfoResponse, error) {
	info := &pb.NamespaceInfo{
		Config: &pb.NamespaceConfig{
			Namespace:      in.Namespace,
			Dimensions:     384,
			DistanceMetric: pb.DistanceMetric_DISTANCE_METRIC_COSINE,
		},
	}

	if in.IncludeStats {
		info.VectorCount = 5000
		info.IndexedCount = 4800
		info.StorageBytes = 1024 * 1024 * 50
	}

	return &pb.GetNamespaceInfoResponse{
		Info: info,
	}, nil
}

// ListNamespaces mocks namespace listing
func (m *mockVectorDBClient) ListNamespaces(ctx context.Context, in *pb.ListNamespacesRequest, opts ...grpc.CallOption) (*pb.ListNamespacesResponse, error) {
	// Create mock namespaces
	namespaces := []*pb.NamespaceInfo{
		{
			Config: &pb.NamespaceConfig{
				Namespace:      "default",
				Dimensions:     384,
				DistanceMetric: pb.DistanceMetric_DISTANCE_METRIC_COSINE,
			},
			VectorCount: 1000,
		},
		{
			Config: &pb.NamespaceConfig{
				Namespace:      "test-namespace",
				Dimensions:     768,
				DistanceMetric: pb.DistanceMetric_DISTANCE_METRIC_L2,
			},
			VectorCount: 500,
		},
	}

	return &pb.ListNamespacesResponse{
		Namespaces:    namespaces,
		NextPageToken: "",
		TotalCount:    int32(len(namespaces)),
	}, nil
}

// GetVectorStats mocks vector statistics retrieval
func (m *mockVectorDBClient) GetVectorStats(ctx context.Context, in *pb.GetVectorStatsRequest, opts ...grpc.CallOption) (*pb.GetVectorStatsResponse, error) {
	return &pb.GetVectorStatsResponse{
		TotalVectors:      10000,
		TotalNamespaces:   5,
		TotalStorageBytes: 1024 * 1024 * 100,
		TotalIndexBytes:   1024 * 1024 * 50,
		AvgDimension:      512,
	}, nil
}

// VectorHealth mocks health check
func (m *mockVectorDBClient) VectorHealth(ctx context.Context, in *pb.VectorHealthRequest, opts ...grpc.CallOption) (*pb.VectorHealthResponse, error) {
	return m.Health(ctx, in, opts...)
}

// Health mocks health check (alternate method name)
func (m *mockVectorDBClient) Health(ctx context.Context, in *pb.VectorHealthRequest, opts ...grpc.CallOption) (*pb.VectorHealthResponse, error) {
	resp := &pb.VectorHealthResponse{
		Status:              pb.VectorHealthStatus_VECTOR_HEALTH_STATUS_HEALTHY,
		NodeId:              1,
		Version:             "1.0.0",
		UptimeSeconds:       360000, // 100 hours
		NamespacesHealthy:   2,
		NamespacesDegraded:  0,
		NamespacesUnhealthy: 0,
	}

	// Add component details if requested
	if in.IncludeDetails {
		resp.Details = map[string]*pb.VectorComponentHealth{
			"storage": {
				Status:  pb.VectorHealthStatus_VECTOR_HEALTH_STATUS_HEALTHY,
				Message: "Storage is healthy",
			},
			"index": {
				Status:  pb.VectorHealthStatus_VECTOR_HEALTH_STATUS_HEALTHY,
				Message: "Index is healthy",
			},
		}
	}

	return resp, nil
}

// RebuildIndex mocks index rebuild
func (m *mockVectorDBClient) RebuildIndex(ctx context.Context, in *pb.RebuildIndexRequest, opts ...grpc.CallOption) (*pb.RebuildIndexResponse, error) {
	return &pb.RebuildIndexResponse{
		OperationId:         "rebuild-op-123",
		Started:             timestamppb.Now(),
		EstimatedDurationMs: 30000,
	}, nil
}

// GetIndexProgress mocks index rebuild progress
func (m *mockVectorDBClient) GetIndexProgress(ctx context.Context, in *pb.GetIndexProgressRequest, opts ...grpc.CallOption) (*pb.GetIndexProgressResponse, error) {
	return &pb.GetIndexProgressResponse{
		OperationId:          in.OperationId,
		Namespace:            in.Namespace,
		VectorsIndexed:       3000,
		TotalVectors:         5000,
		PercentComplete:      60.0,
		ElapsedTimeMs:        18000,
		EstimatedRemainingMs: 12000,
		Status:               "in_progress",
		ErrorMessage:         "",
	}, nil
}

// UpdateMetadata mocks metadata update
func (m *mockVectorDBClient) UpdateMetadata(ctx context.Context, in *pb.UpdateMetadataRequest, opts ...grpc.CallOption) (*pb.UpdateMetadataResponse, error) {
	return &pb.UpdateMetadataResponse{
		Success:   true,
		Version:   2,
		Timestamp: timestamppb.Now(),
	}, nil
}

// HybridSearch mocks hybrid search with streaming results
func (m *mockVectorDBClient) HybridSearch(ctx context.Context, in *pb.HybridSearchRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[pb.HybridSearchResponse], error) {
	// Create mock results
	results := make([]*pb.SimilarityResult, in.K)
	for i := int32(0); i < in.K; i++ {
		results[i] = &pb.SimilarityResult{
			Vector: &pb.VectorEntity{
				Id:        "hybrid-vector-" + string(rune('1'+i)),
				Namespace: in.Namespace,
				Vector:    []float32{0.1, 0.2, 0.3},
				Metadata:  map[string]string{"hybrid": "true"},
			},
			Score: 0.92 - float32(i)*0.03,
			Rank:  i + 1,
		}
	}

	stream := &mockHybridSearchStream{
		responses: []*pb.HybridSearchResponse{
			{
				Results:          results,
				TotalCandidates:  int64(in.K) * 5,
				TotalFiltered:    int64(in.K) * 2,
				SearchTimeMs:     12,
				RerankingApplied: true,
				NodeId:           1,
				IsFinal:          true,
			},
		},
	}

	return stream, nil
}

// mockSearchByMetadataStream implements a mock streaming client for SearchByMetadata
type mockSearchByMetadataStream struct {
	grpc.ClientStream
	responses []*pb.SearchByMetadataResponse
	index     int
}

func (m *mockSearchByMetadataStream) Recv() (*pb.SearchByMetadataResponse, error) {
	if m.index >= len(m.responses) {
		return nil, io.EOF
	}
	resp := m.responses[m.index]
	m.index++
	return resp, nil
}

func (m *mockSearchByMetadataStream) CloseSend() error {
	return nil
}

func (m *mockSearchByMetadataStream) Context() context.Context {
	return context.Background()
}

func (m *mockSearchByMetadataStream) Header() (metadata.MD, error) {
	return nil, nil
}

func (m *mockSearchByMetadataStream) Trailer() metadata.MD {
	return nil
}

// SearchByMetadata mocks metadata-based search with streaming results
func (m *mockVectorDBClient) SearchByMetadata(ctx context.Context, in *pb.SearchByMetadataRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[pb.SearchByMetadataResponse], error) {
	// Create mock results
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}

	results := make([]*pb.VectorEntity, limit)
	for i := int32(0); i < limit; i++ {
		results[i] = &pb.VectorEntity{
			Id:        fmt.Sprintf("metadata-vector-%d", i+1),
			Namespace: in.Namespace,
			Vector:    []float32{0.1, 0.2, 0.3},
			Metadata:  map[string]string{"filtered": "true"},
		}
	}

	stream := &mockSearchByMetadataStream{
		responses: []*pb.SearchByMetadataResponse{
			{
				Results:      results,
				TotalCount:   int64(limit) * 2,
				SearchTimeMs: 8,
				NodeId:       1,
				IsFinal:      true,
			},
		},
	}

	return stream, nil
}

// ========================================
// Mock VectorAdmin Client
// ========================================

// mockVectorAdminClient wraps the interface and overrides methods for testing
type mockVectorAdminClient struct{}

// Ensure mockVectorAdminClient implements pb.VectorAdminClient
var _ pb.VectorAdminClient = (*mockVectorAdminClient)(nil)

// GetClusterStats mocks cluster statistics retrieval
func (m *mockVectorAdminClient) GetClusterStats(ctx context.Context, in *pb.GetClusterStatsRequest, opts ...grpc.CallOption) (*pb.GetClusterStatsResponse, error) {
	resp := &pb.GetClusterStatsResponse{
		ClusterId:         "test-cluster-123",
		TotalNodes:        3,
		TotalVectors:      10000,
		TotalNamespaces:   5,
		TotalStorageBytes: 1024 * 1024 * 100, // 100MB
		TotalIndexBytes:   1024 * 1024 * 50,  // 50MB
		Timestamp:         timestamppb.Now(),
	}

	// Add namespace stats if requested
	if in.IncludeNamespaces {
		resp.NamespaceStats = []*pb.NamespaceStats{
			{
				Namespace:    "default",
				VectorCount:  5000,
				StorageBytes: 1024 * 1024 * 50,
			},
			{
				Namespace:    "test-namespace",
				VectorCount:  5000,
				StorageBytes: 1024 * 1024 * 50,
			},
		}
	}

	// Add node stats if requested
	if in.IncludeNodes {
		resp.NodeStats = []*pb.NodeStats{
			{
				NodeId:       1,
				VectorCount:  3333,
				StorageBytes: 1024 * 1024 * 33,
			},
			{
				NodeId:       2,
				VectorCount:  3333,
				StorageBytes: 1024 * 1024 * 33,
			},
			{
				NodeId:       3,
				VectorCount:  3334,
				StorageBytes: 1024 * 1024 * 34,
			},
		}
	}

	return resp, nil
}

// CompactNamespace mocks namespace compaction
func (m *mockVectorAdminClient) CompactNamespace(ctx context.Context, in *pb.CompactNamespaceRequest, opts ...grpc.CallOption) (*pb.CompactNamespaceResponse, error) {
	return &pb.CompactNamespaceResponse{
		OperationId: "compact-op-456",
		Started:     timestamppb.Now(),
	}, nil
}

// GetCompactionProgress mocks compaction progress
func (m *mockVectorAdminClient) GetCompactionProgress(ctx context.Context, in *pb.GetCompactionProgressRequest, opts ...grpc.CallOption) (*pb.GetCompactionProgressResponse, error) {
	return &pb.GetCompactionProgressResponse{
		OperationId:         in.OperationId,
		Namespace:           in.Namespace,
		BytesCompacted:      50 * 1024 * 1024,
		TotalBytes:          100 * 1024 * 1024,
		PercentComplete:     50.0,
		ElapsedTimeMs:       60000,
		SpaceReclaimedBytes: 10 * 1024 * 1024,
		Status:              "in_progress",
	}, nil
}

// ExportVectors mocks vector export
func (m *mockVectorAdminClient) ExportVectors(ctx context.Context, in *pb.ExportVectorsRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[pb.ExportVectorsResponse], error) {
	stream := &mockExportVectorsStream{
		responses: []*pb.ExportVectorsResponse{
			{
				BatchData:       []byte("mock-export-data-batch-1"),
				VectorsExported: 500,
				IsFinal:         false,
			},
			{
				BatchData:       []byte("mock-export-data-batch-2"),
				VectorsExported: 500,
				IsFinal:         true,
			},
		},
	}
	return stream, nil
}

// ImportVectors mocks vector import
func (m *mockVectorAdminClient) ImportVectors(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStreamingClient[pb.ImportVectorsRequest, pb.ImportVectorsResponse], error) {
	stream := &mockImportVectorsStream{
		sent: make([]*pb.ImportVectorsRequest, 0),
	}
	return stream, nil
}

// CreateScalarIndex mocks scalar index creation
func (m *mockVectorAdminClient) CreateScalarIndex(ctx context.Context, in *pb.CreateScalarIndexRequest, opts ...grpc.CallOption) (*pb.CreateScalarIndexResponse, error) {
	return &pb.CreateScalarIndexResponse{
		Field: in.GetField(),
	}, nil
}

// DropScalarIndex mocks scalar index deletion
func (m *mockVectorAdminClient) DropScalarIndex(ctx context.Context, in *pb.DropScalarIndexRequest, opts ...grpc.CallOption) (*pb.DropScalarIndexResponse, error) {
	return &pb.DropScalarIndexResponse{}, nil
}

// ListScalarIndexes mocks scalar index listing
func (m *mockVectorAdminClient) ListScalarIndexes(ctx context.Context, in *pb.ListScalarIndexesRequest, opts ...grpc.CallOption) (*pb.ListScalarIndexesResponse, error) {
	return &pb.ListScalarIndexesResponse{}, nil
}
