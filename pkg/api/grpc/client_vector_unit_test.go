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
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

const testBufSize = 1024 * 1024

// minimalVectorServer implements a minimal VectorDBServer for client testing
type minimalVectorServer struct {
	pb.UnimplementedVectorDBServer
	createNamespaceFunc  func(context.Context, *pb.CreateNamespaceRequest) (*pb.CreateNamespaceResponse, error)
	deleteNamespaceFunc  func(context.Context, *pb.DeleteNamespaceRequest) (*pb.DeleteNamespaceResponse, error)
	listNamespacesFunc   func(context.Context, *pb.ListNamespacesRequest) (*pb.ListNamespacesResponse, error)
	getNamespaceInfoFunc func(context.Context, *pb.GetNamespaceInfoRequest) (*pb.GetNamespaceInfoResponse, error)
	upsertVectorFunc     func(context.Context, *pb.UpsertVectorRequest) (*pb.UpsertVectorResponse, error)
	getVectorFunc        func(context.Context, *pb.GetVectorRequest) (*pb.GetVectorResponse, error)
	deleteVectorFunc     func(context.Context, *pb.DeleteVectorRequest) (*pb.DeleteVectorResponse, error)
	batchUpsertFunc      func(*pb.BatchUpsertVectorsRequest, pb.VectorDB_BatchUpsertVectorsServer) error
	batchDeleteFunc      func(context.Context, *pb.BatchDeleteVectorsRequest) (*pb.BatchDeleteVectorsResponse, error)
	searchSimilarFunc    func(*pb.SearchSimilarRequest, pb.VectorDB_SearchSimilarServer) error
	searchByIDFunc       func(*pb.SearchByIDRequest, pb.VectorDB_SearchByIDServer) error
	listVectorsFunc      func(*pb.ListVectorsRequest, pb.VectorDB_ListVectorsServer) error
	rebuildIndexFunc     func(context.Context, *pb.RebuildIndexRequest) (*pb.RebuildIndexResponse, error)
	getIndexProgressFunc func(context.Context, *pb.GetIndexProgressRequest) (*pb.GetIndexProgressResponse, error)
}

func (m *minimalVectorServer) CreateNamespace(ctx context.Context, req *pb.CreateNamespaceRequest) (*pb.CreateNamespaceResponse, error) {
	if m.createNamespaceFunc != nil {
		return m.createNamespaceFunc(ctx, req)
	}
	return &pb.CreateNamespaceResponse{Namespace: "test", Timestamp: timestamppb.Now()}, nil
}

func (m *minimalVectorServer) DeleteNamespace(ctx context.Context, req *pb.DeleteNamespaceRequest) (*pb.DeleteNamespaceResponse, error) {
	if m.deleteNamespaceFunc != nil {
		return m.deleteNamespaceFunc(ctx, req)
	}
	return &pb.DeleteNamespaceResponse{Deleted: true, Timestamp: timestamppb.Now()}, nil
}

func (m *minimalVectorServer) ListNamespaces(ctx context.Context, req *pb.ListNamespacesRequest) (*pb.ListNamespacesResponse, error) {
	if m.listNamespacesFunc != nil {
		return m.listNamespacesFunc(ctx, req)
	}
	return &pb.ListNamespacesResponse{
		Namespaces: []*pb.NamespaceInfo{
			{Config: &pb.NamespaceConfig{Namespace: "test"}, VectorCount: 100},
		},
	}, nil
}

func (m *minimalVectorServer) GetNamespaceInfo(ctx context.Context, req *pb.GetNamespaceInfoRequest) (*pb.GetNamespaceInfoResponse, error) {
	if m.getNamespaceInfoFunc != nil {
		return m.getNamespaceInfoFunc(ctx, req)
	}
	return &pb.GetNamespaceInfoResponse{
		Info: &pb.NamespaceInfo{Config: &pb.NamespaceConfig{Namespace: "test"}, VectorCount: 100},
	}, nil
}

func (m *minimalVectorServer) UpsertVector(ctx context.Context, req *pb.UpsertVectorRequest) (*pb.UpsertVectorResponse, error) {
	if m.upsertVectorFunc != nil {
		return m.upsertVectorFunc(ctx, req)
	}
	return &pb.UpsertVectorResponse{Id: "vec-1", Timestamp: timestamppb.Now()}, nil
}

func (m *minimalVectorServer) GetVector(ctx context.Context, req *pb.GetVectorRequest) (*pb.GetVectorResponse, error) {
	if m.getVectorFunc != nil {
		return m.getVectorFunc(ctx, req)
	}
	return &pb.GetVectorResponse{
		Vector: &pb.VectorEntity{Id: req.Id},
		Found:  true,
	}, nil
}

func (m *minimalVectorServer) DeleteVector(ctx context.Context, req *pb.DeleteVectorRequest) (*pb.DeleteVectorResponse, error) {
	if m.deleteVectorFunc != nil {
		return m.deleteVectorFunc(ctx, req)
	}
	return &pb.DeleteVectorResponse{Deleted: true, Timestamp: timestamppb.Now()}, nil
}

func (m *minimalVectorServer) BatchUpsertVectors(req *pb.BatchUpsertVectorsRequest, stream pb.VectorDB_BatchUpsertVectorsServer) error {
	if m.batchUpsertFunc != nil {
		return m.batchUpsertFunc(req, stream)
	}
	return stream.Send(&pb.BatchUpsertVectorsResponse{
		VectorsProcessed: int64(len(req.Vectors)),
		VectorsSucceeded: int64(len(req.Vectors)),
		IsFinal:          true,
	})
}

func (m *minimalVectorServer) BatchDeleteVectors(ctx context.Context, req *pb.BatchDeleteVectorsRequest) (*pb.BatchDeleteVectorsResponse, error) {
	if m.batchDeleteFunc != nil {
		return m.batchDeleteFunc(ctx, req)
	}
	return &pb.BatchDeleteVectorsResponse{
		DeletedCount: int64(len(req.Ids)),
		Timestamp:    timestamppb.Now(),
	}, nil
}

func (m *minimalVectorServer) SearchSimilar(req *pb.SearchSimilarRequest, stream pb.VectorDB_SearchSimilarServer) error {
	if m.searchSimilarFunc != nil {
		return m.searchSimilarFunc(req, stream)
	}
	return stream.Send(&pb.SearchSimilarResponse{
		Results: []*pb.SimilarityResult{
			{Vector: &pb.VectorEntity{Id: "vec-1"}, Score: 0.95},
		},
		IsFinal: true,
	})
}

func (m *minimalVectorServer) SearchByID(req *pb.SearchByIDRequest, stream pb.VectorDB_SearchByIDServer) error {
	if m.searchByIDFunc != nil {
		return m.searchByIDFunc(req, stream)
	}
	return stream.Send(&pb.SearchByIDResponse{
		Results: []*pb.SimilarityResult{
			{Vector: &pb.VectorEntity{Id: "vec-2"}, Score: 0.90},
		},
		IsFinal: true,
	})
}

func (m *minimalVectorServer) ListVectors(req *pb.ListVectorsRequest, stream pb.VectorDB_ListVectorsServer) error {
	if m.listVectorsFunc != nil {
		return m.listVectorsFunc(req, stream)
	}
	return stream.Send(&pb.ListVectorsResponse{
		Vectors: []*pb.VectorEntity{
			{Id: "vec-1"},
			{Id: "vec-2"},
		},
		IsFinal: true,
	})
}

func (m *minimalVectorServer) RebuildIndex(ctx context.Context, req *pb.RebuildIndexRequest) (*pb.RebuildIndexResponse, error) {
	if m.rebuildIndexFunc != nil {
		return m.rebuildIndexFunc(ctx, req)
	}
	return &pb.RebuildIndexResponse{OperationId: "op-1", Started: timestamppb.Now()}, nil
}

func (m *minimalVectorServer) GetIndexProgress(ctx context.Context, req *pb.GetIndexProgressRequest) (*pb.GetIndexProgressResponse, error) {
	if m.getIndexProgressFunc != nil {
		return m.getIndexProgressFunc(ctx, req)
	}
	return &pb.GetIndexProgressResponse{
		OperationId:     req.OperationId,
		VectorsIndexed:  50,
		TotalVectors:    100,
		PercentComplete: 50.0,
	}, nil
}

// setupVectorTestServer creates a test gRPC server with bufconn
func setupVectorTestServer(mockServer *minimalVectorServer) (*grpc.Server, *bufconn.Listener) {
	lis := bufconn.Listen(testBufSize)
	server := grpc.NewServer()
	pb.RegisterVectorDBServer(server, mockServer)
	go func() {
		if err := server.Serve(lis); err != nil {
			// Server stopped
		}
	}()
	return server, lis
}

// createVectorTestClient creates a client connected to the test server
func createVectorTestClient(lis *bufconn.Listener) (*grpc.ClientConn, error) {
	return grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

// Test helper to create a connected client
func setupConnectedVectorClient(t *testing.T, mockServer *minimalVectorServer) (*Client, *grpc.Server) {
	server, lis := setupVectorTestServer(mockServer)
	conn, err := createVectorTestClient(lis)
	require.NoError(t, err)

	client := &Client{
		conn: conn,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      3,
			retryBackoff:    10 * time.Millisecond,
			maxRetryBackoff: 100 * time.Millisecond,
		},
		connected: true,
	}

	return client, server
}

// ========================================
// VectorCreateNamespace Tests
// ========================================

func TestVectorClient_CreateNamespace_Success(t *testing.T) {
	client, server := setupConnectedVectorClient(t, &minimalVectorServer{})
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.CreateNamespaceRequest{
		Config: &pb.NamespaceConfig{
			Namespace:  "test",
			Dimensions: 128,
		},
	}

	resp, err := client.VectorCreateNamespace(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test", resp.Namespace)
}

func TestVectorClient_CreateNamespace_Error(t *testing.T) {
	mockServer := &minimalVectorServer{
		createNamespaceFunc: func(ctx context.Context, req *pb.CreateNamespaceRequest) (*pb.CreateNamespaceResponse, error) {
			return nil, status.Error(codes.AlreadyExists, "namespace exists")
		},
	}
	client, server := setupConnectedVectorClient(t, mockServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.CreateNamespaceRequest{
		Config: &pb.NamespaceConfig{
			Namespace:  "test",
			Dimensions: 128,
		},
	}

	resp, err := client.VectorCreateNamespace(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// ========================================
// VectorDeleteNamespace Tests
// ========================================

func TestVectorClient_DeleteNamespace_Success(t *testing.T) {
	client, server := setupConnectedVectorClient(t, &minimalVectorServer{})
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.DeleteNamespaceRequest{Namespace: "test"}

	resp, err := client.VectorDeleteNamespace(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Deleted)
}

func TestVectorClient_DeleteNamespace_Error(t *testing.T) {
	mockServer := &minimalVectorServer{
		deleteNamespaceFunc: func(ctx context.Context, req *pb.DeleteNamespaceRequest) (*pb.DeleteNamespaceResponse, error) {
			return nil, status.Error(codes.NotFound, "namespace not found")
		},
	}
	client, server := setupConnectedVectorClient(t, mockServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.DeleteNamespaceRequest{Namespace: "test"}

	resp, err := client.VectorDeleteNamespace(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// ========================================
// VectorListNamespaces Tests
// ========================================

func TestVectorClient_ListNamespaces_Success(t *testing.T) {
	client, server := setupConnectedVectorClient(t, &minimalVectorServer{})
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.ListNamespacesRequest{}

	resp, err := client.VectorListNamespaces(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Namespaces, 1)
}

func TestVectorClient_ListNamespaces_Error(t *testing.T) {
	mockServer := &minimalVectorServer{
		listNamespacesFunc: func(ctx context.Context, req *pb.ListNamespacesRequest) (*pb.ListNamespacesResponse, error) {
			return nil, status.Error(codes.Internal, "internal error")
		},
	}
	client, server := setupConnectedVectorClient(t, mockServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.ListNamespacesRequest{}

	resp, err := client.VectorListNamespaces(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// ========================================
// VectorGetNamespaceInfo Tests
// ========================================

func TestVectorClient_GetNamespaceInfo_Success(t *testing.T) {
	client, server := setupConnectedVectorClient(t, &minimalVectorServer{})
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.GetNamespaceInfoRequest{Namespace: "test"}

	resp, err := client.VectorGetNamespaceInfo(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Info)
}

func TestVectorClient_GetNamespaceInfo_Error(t *testing.T) {
	mockServer := &minimalVectorServer{
		getNamespaceInfoFunc: func(ctx context.Context, req *pb.GetNamespaceInfoRequest) (*pb.GetNamespaceInfoResponse, error) {
			return nil, status.Error(codes.NotFound, "namespace not found")
		},
	}
	client, server := setupConnectedVectorClient(t, mockServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.GetNamespaceInfoRequest{Namespace: "test"}

	resp, err := client.VectorGetNamespaceInfo(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// ========================================
// VectorUpsert Tests
// ========================================

func TestVectorClient_Upsert_Success(t *testing.T) {
	client, server := setupConnectedVectorClient(t, &minimalVectorServer{})
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.UpsertVectorRequest{
		Vector: &pb.VectorEntity{
			Id:     "vec-1",
			Vector: []float32{0.1, 0.2, 0.3},
		},
	}

	resp, err := client.VectorUpsert(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "vec-1", resp.Id)
}

func TestVectorClient_Upsert_Error(t *testing.T) {
	mockServer := &minimalVectorServer{
		upsertVectorFunc: func(ctx context.Context, req *pb.UpsertVectorRequest) (*pb.UpsertVectorResponse, error) {
			return nil, status.Error(codes.InvalidArgument, "invalid vector")
		},
	}
	client, server := setupConnectedVectorClient(t, mockServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.UpsertVectorRequest{
		Vector: &pb.VectorEntity{
			Id:     "vec-1",
			Vector: []float32{0.1, 0.2, 0.3},
		},
	}

	resp, err := client.VectorUpsert(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// ========================================
// VectorGet Tests
// ========================================

func TestVectorClient_Get_Success(t *testing.T) {
	client, server := setupConnectedVectorClient(t, &minimalVectorServer{})
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.GetVectorRequest{
		Namespace: "test",
		Id:        "vec-1",
	}

	resp, err := client.VectorGet(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Found)
}

func TestVectorClient_Get_Error(t *testing.T) {
	mockServer := &minimalVectorServer{
		getVectorFunc: func(ctx context.Context, req *pb.GetVectorRequest) (*pb.GetVectorResponse, error) {
			return nil, status.Error(codes.NotFound, "vector not found")
		},
	}
	client, server := setupConnectedVectorClient(t, mockServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.GetVectorRequest{
		Namespace: "test",
		Id:        "vec-1",
	}

	resp, err := client.VectorGet(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// ========================================
// VectorDelete Tests
// ========================================

func TestVectorClient_Delete_Success(t *testing.T) {
	client, server := setupConnectedVectorClient(t, &minimalVectorServer{})
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.DeleteVectorRequest{
		Namespace: "test",
		Id:        "vec-1",
	}

	resp, err := client.VectorDelete(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Deleted)
}

func TestVectorClient_Delete_Error(t *testing.T) {
	mockServer := &minimalVectorServer{
		deleteVectorFunc: func(ctx context.Context, req *pb.DeleteVectorRequest) (*pb.DeleteVectorResponse, error) {
			return nil, status.Error(codes.NotFound, "vector not found")
		},
	}
	client, server := setupConnectedVectorClient(t, mockServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.DeleteVectorRequest{
		Namespace: "test",
		Id:        "vec-1",
	}

	resp, err := client.VectorDelete(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// ========================================
// VectorBatchUpsert Tests
// ========================================

func TestVectorClient_BatchUpsert_Success(t *testing.T) {
	client, server := setupConnectedVectorClient(t, &minimalVectorServer{})
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.BatchUpsertVectorsRequest{
		Vectors: []*pb.VectorEntity{
			{Id: "vec-1", Vector: []float32{0.1, 0.2}},
			{Id: "vec-2", Vector: []float32{0.3, 0.4}},
		},
	}

	responses, err := client.VectorBatchUpsert(ctx, req)
	require.NoError(t, err)
	assert.Len(t, responses, 1)
	assert.Equal(t, int64(2), responses[0].VectorsProcessed)
}

func TestVectorClient_BatchUpsert_StreamError(t *testing.T) {
	mockServer := &minimalVectorServer{
		batchUpsertFunc: func(req *pb.BatchUpsertVectorsRequest, stream pb.VectorDB_BatchUpsertVectorsServer) error {
			return status.Error(codes.Internal, "stream error")
		},
	}
	client, server := setupConnectedVectorClient(t, mockServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.BatchUpsertVectorsRequest{
		Vectors: []*pb.VectorEntity{{Id: "vec-1", Vector: []float32{0.1}}},
	}

	responses, err := client.VectorBatchUpsert(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, responses)
}

// ========================================
// VectorBatchDelete Tests
// ========================================

func TestVectorClient_BatchDelete_Success(t *testing.T) {
	client, server := setupConnectedVectorClient(t, &minimalVectorServer{})
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.BatchDeleteVectorsRequest{
		Namespace: "test",
		Ids:       []string{"vec-1", "vec-2"},
	}

	resp, err := client.VectorBatchDelete(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, int64(2), resp.DeletedCount)
}

func TestVectorClient_BatchDelete_Error(t *testing.T) {
	mockServer := &minimalVectorServer{
		batchDeleteFunc: func(ctx context.Context, req *pb.BatchDeleteVectorsRequest) (*pb.BatchDeleteVectorsResponse, error) {
			return nil, status.Error(codes.Internal, "delete error")
		},
	}
	client, server := setupConnectedVectorClient(t, mockServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.BatchDeleteVectorsRequest{
		Namespace: "test",
		Ids:       []string{"vec-1", "vec-2"},
	}

	resp, err := client.VectorBatchDelete(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// ========================================
// VectorSearchSimilar Tests
// ========================================

func TestVectorClient_SearchSimilar_Success(t *testing.T) {
	client, server := setupConnectedVectorClient(t, &minimalVectorServer{})
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.SearchSimilarRequest{
		Namespace:   "test",
		QueryVector: []float32{0.1, 0.2, 0.3},
		K:           10,
	}

	responses, err := client.VectorSearchSimilar(ctx, req)
	require.NoError(t, err)
	assert.Len(t, responses, 1)
	assert.True(t, responses[0].IsFinal)
}

func TestVectorClient_SearchSimilar_StreamError(t *testing.T) {
	mockServer := &minimalVectorServer{
		searchSimilarFunc: func(req *pb.SearchSimilarRequest, stream pb.VectorDB_SearchSimilarServer) error {
			return status.Error(codes.Internal, "search error")
		},
	}
	client, server := setupConnectedVectorClient(t, mockServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.SearchSimilarRequest{
		Namespace:   "test",
		QueryVector: []float32{0.1, 0.2, 0.3},
		K:           10,
	}

	responses, err := client.VectorSearchSimilar(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, responses)
}

// ========================================
// VectorSearchByID Tests
// ========================================

func TestVectorClient_SearchByID_Success(t *testing.T) {
	client, server := setupConnectedVectorClient(t, &minimalVectorServer{})
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.SearchByIDRequest{
		Namespace: "test",
		Id:        "vec-1",
		K:         10,
	}

	responses, err := client.VectorSearchByID(ctx, req)
	require.NoError(t, err)
	assert.Len(t, responses, 1)
	assert.True(t, responses[0].IsFinal)
}

func TestVectorClient_SearchByID_StreamError(t *testing.T) {
	mockServer := &minimalVectorServer{
		searchByIDFunc: func(req *pb.SearchByIDRequest, stream pb.VectorDB_SearchByIDServer) error {
			return status.Error(codes.NotFound, "vector not found")
		},
	}
	client, server := setupConnectedVectorClient(t, mockServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.SearchByIDRequest{
		Namespace: "test",
		Id:        "vec-1",
		K:         10,
	}

	responses, err := client.VectorSearchByID(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, responses)
}

// ========================================
// VectorList Tests
// ========================================

func TestVectorClient_List_Success(t *testing.T) {
	client, server := setupConnectedVectorClient(t, &minimalVectorServer{})
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.ListVectorsRequest{Namespace: "test"}

	responses, err := client.VectorList(ctx, req)
	require.NoError(t, err)
	assert.Len(t, responses, 1)
	assert.True(t, responses[0].IsFinal)
}

func TestVectorClient_List_StreamError(t *testing.T) {
	mockServer := &minimalVectorServer{
		listVectorsFunc: func(req *pb.ListVectorsRequest, stream pb.VectorDB_ListVectorsServer) error {
			return status.Error(codes.Internal, "list error")
		},
	}
	client, server := setupConnectedVectorClient(t, mockServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.ListVectorsRequest{Namespace: "test"}

	responses, err := client.VectorList(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, responses)
}

// ========================================
// VectorRebuildIndex Tests
// ========================================

func TestVectorClient_RebuildIndex_Success(t *testing.T) {
	client, server := setupConnectedVectorClient(t, &minimalVectorServer{})
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.RebuildIndexRequest{Namespace: "test"}

	resp, err := client.VectorRebuildIndex(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotEmpty(t, resp.OperationId)
}

func TestVectorClient_RebuildIndex_Error(t *testing.T) {
	mockServer := &minimalVectorServer{
		rebuildIndexFunc: func(ctx context.Context, req *pb.RebuildIndexRequest) (*pb.RebuildIndexResponse, error) {
			return nil, status.Error(codes.Internal, "rebuild error")
		},
	}
	client, server := setupConnectedVectorClient(t, mockServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.RebuildIndexRequest{Namespace: "test"}

	resp, err := client.VectorRebuildIndex(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// ========================================
// VectorGetIndexProgress Tests
// ========================================

func TestVectorClient_GetIndexProgress_Success(t *testing.T) {
	client, server := setupConnectedVectorClient(t, &minimalVectorServer{})
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.GetIndexProgressRequest{OperationId: "op-1"}

	resp, err := client.VectorGetIndexProgress(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "op-1", resp.OperationId)
	assert.InDelta(t, 50.0, resp.PercentComplete, 0.01)
}

func TestVectorClient_GetIndexProgress_Error(t *testing.T) {
	mockServer := &minimalVectorServer{
		getIndexProgressFunc: func(ctx context.Context, req *pb.GetIndexProgressRequest) (*pb.GetIndexProgressResponse, error) {
			return nil, status.Error(codes.NotFound, "operation not found")
		},
	}
	client, server := setupConnectedVectorClient(t, mockServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &pb.GetIndexProgressRequest{OperationId: "op-1"}

	resp, err := client.VectorGetIndexProgress(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}
