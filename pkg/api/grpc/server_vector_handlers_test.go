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
	"math"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// Mock Vector Streams
// =============================================================================

// MockBatchUpsertVectorsStream implements pb.VectorDB_BatchUpsertVectorsServer
type MockBatchUpsertVectorsStream struct {
	MockServerStreamBase
	messages []*pb.BatchUpsertVectorsResponse
}

func NewMockBatchUpsertVectorsStream(ctx context.Context) *MockBatchUpsertVectorsStream {
	return &MockBatchUpsertVectorsStream{
		MockServerStreamBase: MockServerStreamBase{ctx: ctx},
		messages:             make([]*pb.BatchUpsertVectorsResponse, 0),
	}
}

func (m *MockBatchUpsertVectorsStream) Send(resp *pb.BatchUpsertVectorsResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.messages = append(m.messages, resp)
	return nil
}

func (m *MockBatchUpsertVectorsStream) GetMessages() []*pb.BatchUpsertVectorsResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages
}

// MockSearchSimilarStream implements pb.VectorDB_SearchSimilarServer
type MockSearchSimilarStream struct {
	MockServerStreamBase
	messages []*pb.SearchSimilarResponse
}

func NewMockSearchSimilarStream(ctx context.Context) *MockSearchSimilarStream {
	return &MockSearchSimilarStream{
		MockServerStreamBase: MockServerStreamBase{ctx: ctx},
		messages:             make([]*pb.SearchSimilarResponse, 0),
	}
}

func (m *MockSearchSimilarStream) Send(resp *pb.SearchSimilarResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.messages = append(m.messages, resp)
	return nil
}

func (m *MockSearchSimilarStream) GetMessages() []*pb.SearchSimilarResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages
}

// MockSearchByIDStream implements pb.VectorDB_SearchByIDServer
type MockSearchByIDStream struct {
	MockServerStreamBase
	messages []*pb.SearchByIDResponse
}

func NewMockSearchByIDStream(ctx context.Context) *MockSearchByIDStream {
	return &MockSearchByIDStream{
		MockServerStreamBase: MockServerStreamBase{ctx: ctx},
		messages:             make([]*pb.SearchByIDResponse, 0),
	}
}

func (m *MockSearchByIDStream) Send(resp *pb.SearchByIDResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.messages = append(m.messages, resp)
	return nil
}

func (m *MockSearchByIDStream) GetMessages() []*pb.SearchByIDResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages
}

// MockHybridSearchStream implements pb.VectorDB_HybridSearchServer
type MockHybridSearchStream struct {
	MockServerStreamBase
	messages []*pb.HybridSearchResponse
}

func NewMockHybridSearchStream(ctx context.Context) *MockHybridSearchStream {
	return &MockHybridSearchStream{
		MockServerStreamBase: MockServerStreamBase{ctx: ctx},
		messages:             make([]*pb.HybridSearchResponse, 0),
	}
}

func (m *MockHybridSearchStream) Send(resp *pb.HybridSearchResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.messages = append(m.messages, resp)
	return nil
}

func (m *MockHybridSearchStream) GetMessages() []*pb.HybridSearchResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages
}

// MockSearchByMetadataStream implements pb.VectorDB_SearchByMetadataServer
type MockSearchByMetadataStream struct {
	MockServerStreamBase
	messages []*pb.SearchByMetadataResponse
}

func NewMockSearchByMetadataStream(ctx context.Context) *MockSearchByMetadataStream {
	return &MockSearchByMetadataStream{
		MockServerStreamBase: MockServerStreamBase{ctx: ctx},
		messages:             make([]*pb.SearchByMetadataResponse, 0),
	}
}

func (m *MockSearchByMetadataStream) Send(resp *pb.SearchByMetadataResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.messages = append(m.messages, resp)
	return nil
}

func (m *MockSearchByMetadataStream) GetMessages() []*pb.SearchByMetadataResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages
}

// MockListVectorsStream implements pb.VectorDB_ListVectorsServer
type MockListVectorsStream struct {
	MockServerStreamBase
	messages []*pb.ListVectorsResponse
}

func NewMockListVectorsStream(ctx context.Context) *MockListVectorsStream {
	return &MockListVectorsStream{
		MockServerStreamBase: MockServerStreamBase{ctx: ctx},
		messages:             make([]*pb.ListVectorsResponse, 0),
	}
}

func (m *MockListVectorsStream) Send(resp *pb.ListVectorsResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.messages = append(m.messages, resp)
	return nil
}

func (m *MockListVectorsStream) GetMessages() []*pb.ListVectorsResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages
}

// MockExportVectorsStream implements pb.VectorAdmin_ExportVectorsServer
type MockExportVectorsStream struct {
	MockServerStreamBase
	messages []*pb.ExportVectorsResponse
}

func NewMockExportVectorsStream(ctx context.Context) *MockExportVectorsStream {
	return &MockExportVectorsStream{
		MockServerStreamBase: MockServerStreamBase{ctx: ctx},
		messages:             make([]*pb.ExportVectorsResponse, 0),
	}
}

func (m *MockExportVectorsStream) Send(resp *pb.ExportVectorsResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.messages = append(m.messages, resp)
	return nil
}

func (m *MockExportVectorsStream) GetMessages() []*pb.ExportVectorsResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages
}

// MockImportVectorsStream implements pb.VectorAdmin_ImportVectorsServer
type MockImportVectorsStream struct {
	MockServerStreamBase
	recvQueue []*pb.ImportVectorsRequest
	recvIndex int
	response  *pb.ImportVectorsResponse
}

func NewMockImportVectorsStream(ctx context.Context) *MockImportVectorsStream {
	return &MockImportVectorsStream{
		MockServerStreamBase: MockServerStreamBase{ctx: ctx},
		recvQueue:            make([]*pb.ImportVectorsRequest, 0),
	}
}

func (m *MockImportVectorsStream) SendAndClose(resp *pb.ImportVectorsResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.response = resp
	return nil
}

func (m *MockImportVectorsStream) Recv() (*pb.ImportVectorsRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recvErr != nil {
		return nil, m.recvErr
	}
	if m.recvIndex >= len(m.recvQueue) {
		return nil, io.EOF
	}
	msg := m.recvQueue[m.recvIndex]
	m.recvIndex++
	return msg, nil
}

func (m *MockImportVectorsStream) AddRecvMessage(req *pb.ImportVectorsRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recvQueue = append(m.recvQueue, req)
}

func (m *MockImportVectorsStream) GetResponse() *pb.ImportVectorsResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.response
}

// =============================================================================
// CreateNamespace Tests
// =============================================================================

func TestCreateNamespace_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.CreateNamespace(tenantctx.WithSystem(context.Background()), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "request is nil")
}

func TestCreateNamespace_NilConfig(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.CreateNamespace(tenantctx.WithSystem(context.Background()), &pb.CreateNamespaceRequest{})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "config is required")
}

func TestCreateNamespace_EmptyNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.CreateNamespace(tenantctx.WithSystem(context.Background()), &pb.CreateNamespaceRequest{
		Config: &pb.NamespaceConfig{
			Namespace:  "",
			Dimensions: 128,
		},
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "namespace name is required")
}

func TestCreateNamespace_InvalidDimensions(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.CreateNamespace(tenantctx.WithSystem(context.Background()), &pb.CreateNamespaceRequest{
		Config: &pb.NamespaceConfig{
			Namespace:  "test-ns",
			Dimensions: 0,
		},
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "dimensions must be positive")
}

func TestCreateNamespace_Success(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("CreateNamespace", mock.Anything, mock.Anything).
		Return(&service.CreateNamespaceResponse{
			Namespace: "test-ns",
			Timestamp: 1234567890,
		}, nil)

	resp, err := srv.CreateNamespace(tenantctx.WithSystem(context.Background()), &pb.CreateNamespaceRequest{
		Config: &pb.NamespaceConfig{
			Namespace:      "test-ns",
			Dimensions:     128,
			DistanceMetric: pb.DistanceMetric_DISTANCE_METRIC_COSINE,
			IndexType:      pb.IndexType_INDEX_TYPE_HNSW,
			IndexParams:    map[string]string{"M": "16", "efConstruction": "200"},
			Metadata:       map[string]string{"owner": "test"},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "test-ns", resp.Namespace)
	mockSvc.AssertExpectations(t)
}

func TestCreateNamespace_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("CreateNamespace", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrAlreadyExists, Op: "CreateNamespace"})

	resp, err := srv.CreateNamespace(tenantctx.WithSystem(context.Background()), &pb.CreateNamespaceRequest{
		Config: &pb.NamespaceConfig{
			Namespace:  "test-ns",
			Dimensions: 128,
		},
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// UpdateNamespace Tests
// =============================================================================

func TestUpdateNamespace_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.UpdateNamespace(tenantctx.WithSystem(context.Background()), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUpdateNamespace_EmptyNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.UpdateNamespace(tenantctx.WithSystem(context.Background()), &pb.UpdateNamespaceRequest{
		Namespace: "",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// assertNamespaceUpdateRefused checks that UpdateNamespace answered with the
// typed refusal, mapped to codes.Unimplemented, and no response.
func assertNamespaceUpdateRefused(t *testing.T, resp *pb.UpdateNamespaceResponse, err error, namespace string) {
	t.Helper()

	require.Error(t, err)
	assert.Nil(t, resp)
	var refusal *NamespaceUpdateUnsupportedError
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, namespace, refusal.Namespace)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unimplemented, st.Code())
}

// TestUpdateNamespace_RefusesWithoutDeleting pins the contract: with no
// in-place namespace update in the qrdb SDK, UpdateNamespace refuses rather
// than deleting and recreating the namespace, which would destroy its vectors.
func TestUpdateNamespace_RefusesWithoutDeleting(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	resp, err := srv.UpdateNamespace(tenantctx.WithSystem(context.Background()), &pb.UpdateNamespaceRequest{
		Namespace:   "test-ns",
		IndexParams: map[string]string{"M": "32"},
		Metadata:    map[string]string{"env": "prod"},
	})

	assertNamespaceUpdateRefused(t, resp, err, "test-ns")
	mockSvc.AssertNotCalled(t, "DeleteNamespace", mock.Anything, mock.Anything)
	mockSvc.AssertNotCalled(t, "CreateNamespace", mock.Anything, mock.Anything)
}

// TestUpdateNamespace_PopulatedNamespaceKeepsVectors is the regression test for
// Codex review 8162: updating a populated namespace must leave its vectors
// readable. The vectors are upserted and read back through the server's own
// handlers, around the refused update.
func TestUpdateNamespace_PopulatedNamespaceKeepsVectors(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)
	ctx := tenantctx.WithSystem(context.Background())
	stored := map[string][]float32{
		"vec-1": {0.1, 0.2, 0.3},
		"vec-2": {0.4, 0.5, 0.6},
	}

	mockSvc.On("UpsertVector", mock.Anything, mock.Anything).
		Return(&service.UpsertVectorResponse{Timestamp: 1234567890}, nil)
	for id, values := range stored {
		vectorID := parseVectorID(id)
		mockSvc.On("GetVector", mock.Anything, mock.MatchedBy(func(req *service.GetVectorRequest) bool {
			return req.ID == vectorID
		})).Return(&service.GetVectorResponse{ID: vectorID, Vector: values, Found: true}, nil)
	}

	for id, values := range stored {
		_, err := srv.UpsertVector(ctx, &pb.UpsertVectorRequest{
			Vector: &pb.VectorEntity{Id: id, Namespace: "populated-ns", Vector: values},
		})
		require.NoError(t, err)
	}

	resp, err := srv.UpdateNamespace(ctx, &pb.UpdateNamespaceRequest{
		Namespace:   "populated-ns",
		IndexParams: map[string]string{"M": "32"},
	})
	assertNamespaceUpdateRefused(t, resp, err, "populated-ns")

	for id, values := range stored {
		got, err := srv.GetVector(ctx, &pb.GetVectorRequest{Namespace: "populated-ns", Id: id, IncludeVector: true})
		require.NoError(t, err)
		require.True(t, got.Found)
		assert.Equal(t, values, got.Vector.Vector)
	}
	mockSvc.AssertNotCalled(t, "DeleteNamespace", mock.Anything, mock.Anything)
	mockSvc.AssertNotCalled(t, "CreateNamespace", mock.Anything, mock.Anything)
}

// =============================================================================
// DeleteNamespace Tests
// =============================================================================

func TestDeleteNamespace_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.DeleteNamespace(tenantctx.WithSystem(context.Background()), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestDeleteNamespace_EmptyNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.DeleteNamespace(tenantctx.WithSystem(context.Background()), &pb.DeleteNamespaceRequest{
		Namespace: "",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestDeleteNamespace_Success(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("DeleteNamespace", mock.Anything, mock.Anything).
		Return(&service.DeleteNamespaceResponse{
			Success:        true,
			VectorsDeleted: 100,
			Timestamp:      1234567890,
		}, nil)

	resp, err := srv.DeleteNamespace(tenantctx.WithSystem(context.Background()), &pb.DeleteNamespaceRequest{
		Namespace: "test-ns",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Deleted)
	assert.Equal(t, int64(100), resp.VectorsDeleted)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListNamespaces Tests
// =============================================================================

func TestListNamespaces_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.ListNamespaces(tenantctx.WithSystem(context.Background()), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestListNamespaces_Success(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("ListNamespaces", mock.Anything, mock.Anything).
		Return(&service.ListNamespacesResponse{
			Namespaces: []*service.NamespaceInfo{
				{
					Namespace:      "ns1",
					Dimension:      128,
					DistanceMetric: "COSINE",
					IndexType:      "HNSW",
					VectorCount:    100,
					IndexConfig:    map[string]string{"M": "16"},
					Metadata:       map[string]string{"owner": "test"},
					CreatedAt:      1234567890,
					UpdatedAt:      1234567890,
				},
			},
			TotalCount: 1,
		}, nil)

	resp, err := srv.ListNamespaces(tenantctx.WithSystem(context.Background()), &pb.ListNamespacesRequest{
		Prefix:   "ns",
		PageSize: 10,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Namespaces, 1)
	assert.Equal(t, int32(1), resp.TotalCount)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetNamespaceInfo Tests
// =============================================================================

func TestGetNamespaceInfo_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.GetNamespaceInfo(tenantctx.WithSystem(context.Background()), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetNamespaceInfo_EmptyNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.GetNamespaceInfo(tenantctx.WithSystem(context.Background()), &pb.GetNamespaceInfoRequest{
		Namespace: "",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetNamespaceInfo_Success(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetNamespaceInfo", mock.Anything, mock.Anything).
		Return(&service.GetNamespaceInfoResponse{
			Namespace:      "test-ns",
			Dimension:      128,
			DistanceMetric: "COSINE",
			IndexType:      "HNSW",
			VectorCount:    100,
			IndexConfig:    map[string]string{"M": "16"},
			Metadata:       map[string]interface{}{"owner": "test"},
			CreatedAt:      1234567890,
			UpdatedAt:      1234567890,
		}, nil)

	resp, err := srv.GetNamespaceInfo(tenantctx.WithSystem(context.Background()), &pb.GetNamespaceInfoRequest{
		Namespace: "test-ns",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Info)
	assert.Equal(t, "test-ns", resp.Info.Config.Namespace)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// UpsertVector Tests
// =============================================================================

func TestUpsertVector_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.UpsertVector(tenantctx.WithSystem(context.Background()), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUpsertVector_NilVector(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.UpsertVector(tenantctx.WithSystem(context.Background()), &pb.UpsertVectorRequest{})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "vector is required")
}

func TestUpsertVector_EmptyNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.UpsertVector(tenantctx.WithSystem(context.Background()), &pb.UpsertVectorRequest{
		Vector: &pb.VectorEntity{
			Namespace: "",
			Id:        "vec1",
			Vector:    []float32{1.0, 2.0, 3.0},
		},
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "namespace is required")
}

func TestUpsertVector_EmptyID(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.UpsertVector(tenantctx.WithSystem(context.Background()), &pb.UpsertVectorRequest{
		Vector: &pb.VectorEntity{
			Namespace: "test-ns",
			Id:        "",
			Vector:    []float32{1.0, 2.0, 3.0},
		},
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "vector ID is required")
}

func TestUpsertVector_EmptyVectorData(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.UpsertVector(tenantctx.WithSystem(context.Background()), &pb.UpsertVectorRequest{
		Vector: &pb.VectorEntity{
			Namespace: "test-ns",
			Id:        "vec1",
			Vector:    []float32{},
		},
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "vector data is required")
}

func TestUpsertVector_NaNValue(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.UpsertVector(tenantctx.WithSystem(context.Background()), &pb.UpsertVectorRequest{
		Vector: &pb.VectorEntity{
			Namespace: "test-ns",
			Id:        "vec1",
			Vector:    []float32{1.0, float32(math.NaN()), 3.0},
		},
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "NaN or Inf")
}

func TestUpsertVector_InfValue(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.UpsertVector(tenantctx.WithSystem(context.Background()), &pb.UpsertVectorRequest{
		Vector: &pb.VectorEntity{
			Namespace: "test-ns",
			Id:        "vec1",
			Vector:    []float32{1.0, float32(math.Inf(1)), 3.0},
		},
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "NaN or Inf")
}

func TestUpsertVector_Success(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("UpsertVector", mock.Anything, mock.Anything).
		Return(&service.UpsertVectorResponse{
			Success:   true,
			Version:   1,
			Timestamp: 1234567890,
		}, nil)

	resp, err := srv.UpsertVector(tenantctx.WithSystem(context.Background()), &pb.UpsertVectorRequest{
		Vector: &pb.VectorEntity{
			Namespace: "test-ns",
			Id:        "vec1",
			Vector:    []float32{1.0, 2.0, 3.0},
			Metadata:  map[string]string{"category": "test"},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "vec1", resp.Id)
	assert.Equal(t, uint64(1), resp.Version)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetVector Tests
// =============================================================================

func TestGetVector_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.GetVector(tenantctx.WithSystem(context.Background()), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetVector_EmptyNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.GetVector(tenantctx.WithSystem(context.Background()), &pb.GetVectorRequest{
		Namespace: "",
		Id:        "vec1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetVector_EmptyID(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.GetVector(tenantctx.WithSystem(context.Background()), &pb.GetVectorRequest{
		Namespace: "test-ns",
		Id:        "",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetVector_NotFound(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetVector", mock.Anything, mock.Anything).
		Return(&service.GetVectorResponse{
			Found: false,
		}, nil)

	resp, err := srv.GetVector(tenantctx.WithSystem(context.Background()), &pb.GetVectorRequest{
		Namespace: "test-ns",
		Id:        "vec1",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Found)
	mockSvc.AssertExpectations(t)
}

func TestGetVector_Success(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetVector", mock.Anything, mock.Anything).
		Return(&service.GetVectorResponse{
			Found:     true,
			ID:        123,
			Vector:    []float32{1.0, 2.0, 3.0},
			Metadata:  map[string]interface{}{"category": "test"},
			Version:   1,
			CreatedAt: 1234567890,
			UpdatedAt: 1234567890,
		}, nil)

	resp, err := srv.GetVector(tenantctx.WithSystem(context.Background()), &pb.GetVectorRequest{
		Namespace:       "test-ns",
		Id:              "123",
		IncludeVector:   true,
		IncludeMetadata: true,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Found)
	assert.NotNil(t, resp.Vector)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// DeleteVector Tests
// =============================================================================

func TestDeleteVector_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.DeleteVector(tenantctx.WithSystem(context.Background()), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestDeleteVector_EmptyNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.DeleteVector(tenantctx.WithSystem(context.Background()), &pb.DeleteVectorRequest{
		Namespace: "",
		Id:        "vec1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestDeleteVector_EmptyID(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.DeleteVector(tenantctx.WithSystem(context.Background()), &pb.DeleteVectorRequest{
		Namespace: "test-ns",
		Id:        "",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestDeleteVector_Success(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("DeleteVector", mock.Anything, mock.Anything).
		Return(&service.DeleteVectorResponse{
			Success:   true,
			Timestamp: 1234567890,
		}, nil)

	resp, err := srv.DeleteVector(tenantctx.WithSystem(context.Background()), &pb.DeleteVectorRequest{
		Namespace: "test-ns",
		Id:        "vec1",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Deleted)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// BatchDeleteVectors Tests
// =============================================================================

func TestBatchDeleteVectors_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.BatchDeleteVectors(tenantctx.WithSystem(context.Background()), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestBatchDeleteVectors_EmptyNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.BatchDeleteVectors(tenantctx.WithSystem(context.Background()), &pb.BatchDeleteVectorsRequest{
		Namespace: "",
		Ids:       []string{"vec1"},
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestBatchDeleteVectors_EmptyIDs(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.BatchDeleteVectors(tenantctx.WithSystem(context.Background()), &pb.BatchDeleteVectorsRequest{
		Namespace: "test-ns",
		Ids:       []string{},
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestBatchDeleteVectors_Success(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("DeleteVector", mock.Anything, mock.Anything).
		Return(&service.DeleteVectorResponse{
			Success:   true,
			Timestamp: 1234567890,
		}, nil)

	resp, err := srv.BatchDeleteVectors(tenantctx.WithSystem(context.Background()), &pb.BatchDeleteVectorsRequest{
		Namespace: "test-ns",
		Ids:       []string{"vec1", "vec2"},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, int64(2), resp.DeletedCount)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// BatchUpsertVectors Tests
// =============================================================================

func TestBatchUpsertVectors_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockBatchUpsertVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.BatchUpsertVectors(nil, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestBatchUpsertVectors_EmptyVectors(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockBatchUpsertVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.BatchUpsertVectors(&pb.BatchUpsertVectorsRequest{
		Vectors: []*pb.VectorEntity{},
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestBatchUpsertVectors_ExceedsMaxBatchSize(t *testing.T) {
	srv, _ := newMockTestServer(t)

	// Create more than max batch size vectors
	vectors := make([]*pb.VectorEntity, 10001)
	for i := range vectors {
		vectors[i] = &pb.VectorEntity{
			Namespace: "test-ns",
			Id:        "vec",
			Vector:    []float32{1.0},
		}
	}

	stream := NewMockBatchUpsertVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.BatchUpsertVectors(&pb.BatchUpsertVectorsRequest{
		Vectors: vectors,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "exceeds maximum")
}

func TestBatchUpsertVectors_MissingNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockBatchUpsertVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.BatchUpsertVectors(&pb.BatchUpsertVectorsRequest{
		Vectors: []*pb.VectorEntity{
			{Namespace: "", Id: "vec1", Vector: []float32{1.0}},
		},
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "namespace is required")
}

func TestBatchUpsertVectors_MixedNamespaces(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockBatchUpsertVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.BatchUpsertVectors(&pb.BatchUpsertVectorsRequest{
		Vectors: []*pb.VectorEntity{
			{Namespace: "ns1", Id: "vec1", Vector: []float32{1.0}},
			{Namespace: "ns2", Id: "vec2", Vector: []float32{2.0}},
		},
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "same namespace")
}

func TestBatchUpsertVectors_MissingID(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockBatchUpsertVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.BatchUpsertVectors(&pb.BatchUpsertVectorsRequest{
		Vectors: []*pb.VectorEntity{
			{Namespace: "test-ns", Id: "", Vector: []float32{1.0}},
		},
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "ID is required")
}

func TestBatchUpsertVectors_EmptyVectorData(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockBatchUpsertVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.BatchUpsertVectors(&pb.BatchUpsertVectorsRequest{
		Vectors: []*pb.VectorEntity{
			{Namespace: "test-ns", Id: "vec1", Vector: []float32{}},
		},
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "vector data is required")
}

func TestBatchUpsertVectors_NaNValue(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockBatchUpsertVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.BatchUpsertVectors(&pb.BatchUpsertVectorsRequest{
		Vectors: []*pb.VectorEntity{
			{Namespace: "test-ns", Id: "vec1", Vector: []float32{float32(math.NaN())}},
		},
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "NaN or Inf")
}

func TestBatchUpsertVectors_Success(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("BatchUpsert", mock.Anything, mock.Anything).
		Return(&service.BatchUpsertResponse{
			InsertedCount: 2,
			Timestamp:     1234567890,
		}, nil)

	stream := NewMockBatchUpsertVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.BatchUpsertVectors(&pb.BatchUpsertVectorsRequest{
		Vectors: []*pb.VectorEntity{
			{Namespace: "test-ns", Id: "vec1", Vector: []float32{1.0, 2.0}},
			{Namespace: "test-ns", Id: "vec2", Vector: []float32{3.0, 4.0}},
		},
	}, stream)

	require.NoError(t, err)
	msgs := stream.GetMessages()
	require.Len(t, msgs, 1)
	assert.Equal(t, int64(2), msgs[0].VectorsProcessed)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// SearchSimilar Tests
// =============================================================================

func TestSearchSimilar_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockSearchSimilarStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchSimilar(nil, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestSearchSimilar_EmptyNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockSearchSimilarStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchSimilar(&pb.SearchSimilarRequest{
		Namespace:   "",
		QueryVector: []float32{1.0},
		K:           10,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestSearchSimilar_EmptyQueryVector(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockSearchSimilarStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchSimilar(&pb.SearchSimilarRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{},
		K:           10,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "query vector is required")
}

func TestSearchSimilar_InvalidK(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockSearchSimilarStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchSimilar(&pb.SearchSimilarRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{1.0},
		K:           0,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "K must be positive")
}

func TestSearchSimilar_KExceedsMax(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockSearchSimilarStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchSimilar(&pb.SearchSimilarRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{1.0},
		K:           10001,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "exceeds maximum")
}

func TestSearchSimilar_NaNInQueryVector(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockSearchSimilarStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchSimilar(&pb.SearchSimilarRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{float32(math.NaN())},
		K:           10,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "NaN or Inf")
}

func TestSearchSimilar_Success(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("SearchSimilar", mock.Anything, mock.Anything).
		Return(&service.SearchSimilarResponse{
			Results: []*service.VectorSearchResult{
				{ID: 1, Score: 0.95, Vector: []float32{1.0}, Metadata: map[string]interface{}{"cat": "A"}, Rank: 1},
				{ID: 2, Score: 0.85, Vector: []float32{2.0}, Metadata: map[string]interface{}{"cat": "B"}, Rank: 2},
			},
		}, nil)

	stream := NewMockSearchSimilarStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchSimilar(&pb.SearchSimilarRequest{
		Namespace:       "test-ns",
		QueryVector:     []float32{1.0},
		K:               10,
		IncludeVectors:  true,
		IncludeMetadata: true,
	}, stream)

	require.NoError(t, err)
	msgs := stream.GetMessages()
	require.Len(t, msgs, 1)
	assert.Len(t, msgs[0].Results, 2)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// SearchByID Tests
// =============================================================================

func TestSearchByID_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockSearchByIDStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByID(nil, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestSearchByID_EmptyNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockSearchByIDStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByID(&pb.SearchByIDRequest{
		Namespace: "",
		Id:        "vec1",
		K:         10,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestSearchByID_EmptyID(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockSearchByIDStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByID(&pb.SearchByIDRequest{
		Namespace: "test-ns",
		Id:        "",
		K:         10,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestSearchByID_InvalidK(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockSearchByIDStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByID(&pb.SearchByIDRequest{
		Namespace: "test-ns",
		Id:        "vec1",
		K:         0,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "K must be positive")
}

func TestSearchByID_KExceedsMax(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockSearchByIDStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByID(&pb.SearchByIDRequest{
		Namespace: "test-ns",
		Id:        "vec1",
		K:         10001,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "exceeds maximum")
}

func TestSearchByID_VectorNotFound(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetVector", mock.Anything, mock.Anything).
		Return(&service.GetVectorResponse{
			Found: false,
		}, nil)

	stream := NewMockSearchByIDStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByID(&pb.SearchByIDRequest{
		Namespace: "test-ns",
		Id:        "vec1",
		K:         10,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockSvc.AssertExpectations(t)
}

func TestSearchByID_Success(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock GetVector to return the source vector
	mockSvc.On("GetVector", mock.Anything, mock.Anything).
		Return(&service.GetVectorResponse{
			Found:  true,
			ID:     123,
			Vector: []float32{1.0, 2.0, 3.0},
		}, nil)

	// Mock SearchSimilar to return results
	mockSvc.On("SearchSimilar", mock.Anything, mock.Anything).
		Return(&service.SearchSimilarResponse{
			Results: []*service.VectorSearchResult{
				{ID: 123, Score: 1.0, Vector: []float32{1.0, 2.0, 3.0}, Rank: 1},
				{ID: 456, Score: 0.9, Vector: []float32{1.1, 2.1, 3.1}, Rank: 2},
			},
		}, nil)

	stream := NewMockSearchByIDStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByID(&pb.SearchByIDRequest{
		Namespace:       "test-ns",
		Id:              "123",
		K:               10,
		IncludeVectors:  true,
		IncludeMetadata: true,
		IncludeSelf:     false,
	}, stream)

	require.NoError(t, err)
	msgs := stream.GetMessages()
	require.Len(t, msgs, 1)
	// Should exclude self (ID 123)
	assert.LessOrEqual(t, len(msgs[0].Results), 1)
	mockSvc.AssertExpectations(t)
}

func TestSearchByID_IncludeSelf(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock GetVector to return the source vector
	mockSvc.On("GetVector", mock.Anything, mock.Anything).
		Return(&service.GetVectorResponse{
			Found:  true,
			ID:     123,
			Vector: []float32{1.0, 2.0, 3.0},
		}, nil)

	// Mock SearchSimilar to return results including self
	mockSvc.On("SearchSimilar", mock.Anything, mock.Anything).
		Return(&service.SearchSimilarResponse{
			Results: []*service.VectorSearchResult{
				{ID: 123, Score: 1.0, Vector: []float32{1.0, 2.0, 3.0}, Rank: 1},
				{ID: 456, Score: 0.9, Vector: []float32{1.1, 2.1, 3.1}, Rank: 2},
			},
		}, nil)

	stream := NewMockSearchByIDStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByID(&pb.SearchByIDRequest{
		Namespace:   "test-ns",
		Id:          "123",
		K:           10,
		IncludeSelf: true,
	}, stream)

	require.NoError(t, err)
	msgs := stream.GetMessages()
	require.Len(t, msgs, 1)
	// Should include self
	assert.Equal(t, 2, len(msgs[0].Results))
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// HybridSearch Tests
// =============================================================================

func TestHybridSearch_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockHybridSearchStream(tenantctx.WithSystem(context.Background()))
	err := srv.HybridSearch(nil, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestHybridSearch_EmptyNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockHybridSearchStream(tenantctx.WithSystem(context.Background()))
	err := srv.HybridSearch(&pb.HybridSearchRequest{
		Namespace:   "",
		QueryVector: []float32{1.0},
		K:           10,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestHybridSearch_EmptyQueryVector(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockHybridSearchStream(tenantctx.WithSystem(context.Background()))
	err := srv.HybridSearch(&pb.HybridSearchRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{},
		K:           10,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "query vector is required")
}

func TestHybridSearch_InvalidK(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockHybridSearchStream(tenantctx.WithSystem(context.Background()))
	err := srv.HybridSearch(&pb.HybridSearchRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{1.0},
		K:           0,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "K must be positive")
}

func TestHybridSearch_KExceedsMax(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockHybridSearchStream(tenantctx.WithSystem(context.Background()))
	err := srv.HybridSearch(&pb.HybridSearchRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{1.0},
		K:           10001,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "exceeds maximum")
}

func TestHybridSearch_NaNInQueryVector(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockHybridSearchStream(tenantctx.WithSystem(context.Background()))
	err := srv.HybridSearch(&pb.HybridSearchRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{float32(math.NaN())},
		K:           10,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "NaN or Inf")
}

func TestHybridSearch_Success(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("HybridSearch", mock.Anything, mock.Anything).
		Return(&service.HybridSearchResponse{
			Results: []*service.VectorSearchResult{
				{ID: 1, Score: 0.95, Vector: []float32{1.0}, Metadata: map[string]interface{}{"cat": "A"}, Rank: 1},
				{ID: 2, Score: 0.85, Vector: []float32{2.0}, Metadata: map[string]interface{}{"cat": "B"}, Rank: 2},
			},
			TotalCandidates: 100,
			TotalFiltered:   50,
		}, nil)

	stream := NewMockHybridSearchStream(tenantctx.WithSystem(context.Background()))
	err := srv.HybridSearch(&pb.HybridSearchRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{1.0},
		K:           10,
		MetadataFilters: []*pb.MetadataFilter{
			{Key: "category", Value: "test"},
		},
		FilterOperator:     "AND",
		RerankingAlgorithm: "none",
		IncludeVectors:     true,
		IncludeMetadata:    true,
	}, stream)

	require.NoError(t, err)
	msgs := stream.GetMessages()
	require.Len(t, msgs, 1)
	assert.Len(t, msgs[0].Results, 2)
	assert.Equal(t, int64(100), msgs[0].TotalCandidates)
	assert.Equal(t, int64(50), msgs[0].TotalFiltered)
	mockSvc.AssertExpectations(t)
}

func TestHybridSearch_WithReranking(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("HybridSearch", mock.Anything, mock.Anything).
		Return(&service.HybridSearchResponse{
			Results: []*service.VectorSearchResult{
				{ID: 1, Score: 0.95, Rank: 1},
			},
			TotalCandidates: 50,
			TotalFiltered:   25,
		}, nil)

	stream := NewMockHybridSearchStream(tenantctx.WithSystem(context.Background()))
	err := srv.HybridSearch(&pb.HybridSearchRequest{
		Namespace:          "test-ns",
		QueryVector:        []float32{1.0},
		K:                  10,
		RerankingAlgorithm: "bm25",
	}, stream)

	require.NoError(t, err)
	msgs := stream.GetMessages()
	require.Len(t, msgs, 1)
	assert.True(t, msgs[0].RerankingApplied)
	mockSvc.AssertExpectations(t)
}

func TestHybridSearch_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("HybridSearch", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrInternal, Op: "HybridSearch"})

	stream := NewMockHybridSearchStream(tenantctx.WithSystem(context.Background()))
	err := srv.HybridSearch(&pb.HybridSearchRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{1.0},
		K:           10,
	}, stream)

	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// SearchByMetadata Tests
// =============================================================================

func TestSearchByMetadata_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockSearchByMetadataStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByMetadata(nil, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestSearchByMetadata_EmptyNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockSearchByMetadataStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByMetadata(&pb.SearchByMetadataRequest{
		Namespace: "",
		MetadataFilters: []*pb.MetadataFilter{
			{Key: "category", Value: "test"},
		},
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestSearchByMetadata_EmptyFilters(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockSearchByMetadataStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByMetadata(&pb.SearchByMetadataRequest{
		Namespace:       "test-ns",
		MetadataFilters: []*pb.MetadataFilter{},
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "at least one metadata filter is required")
}

func TestSearchByMetadata_LimitExceedsMax(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockSearchByMetadataStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByMetadata(&pb.SearchByMetadataRequest{
		Namespace: "test-ns",
		MetadataFilters: []*pb.MetadataFilter{
			{Key: "category", Value: "test"},
		},
		Limit: 10001,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "exceeds maximum")
}

func TestSearchByMetadata_NegativeOffset(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockSearchByMetadataStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByMetadata(&pb.SearchByMetadataRequest{
		Namespace: "test-ns",
		MetadataFilters: []*pb.MetadataFilter{
			{Key: "category", Value: "test"},
		},
		Offset: -1,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "offset cannot be negative")
}

func TestSearchByMetadata_Success(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("SearchByMetadata", mock.Anything, mock.Anything).
		Return(&service.SearchByMetadataResponse{
			Results: []*service.VectorData{
				{ID: 1, Vector: []float32{1.0}, Metadata: map[string]interface{}{"category": "test"}},
				{ID: 2, Vector: []float32{2.0}, Metadata: map[string]interface{}{"category": "test"}},
			},
			TotalCount: 2,
			NextCursor: "cursor123",
		}, nil)

	stream := NewMockSearchByMetadataStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByMetadata(&pb.SearchByMetadataRequest{
		Namespace: "test-ns",
		MetadataFilters: []*pb.MetadataFilter{
			{Key: "category", Value: "test"},
		},
		FilterOperator:  "AND",
		Limit:           100,
		Offset:          0,
		IncludeVectors:  true,
		IncludeMetadata: true,
	}, stream)

	require.NoError(t, err)
	msgs := stream.GetMessages()
	require.Len(t, msgs, 1)
	assert.Len(t, msgs[0].Results, 2)
	assert.Equal(t, int64(2), msgs[0].TotalCount)
	assert.Equal(t, "cursor123", msgs[0].NextCursor)
	mockSvc.AssertExpectations(t)
}

func TestSearchByMetadata_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("SearchByMetadata", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrInternal, Op: "SearchByMetadata"})

	stream := NewMockSearchByMetadataStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByMetadata(&pb.SearchByMetadataRequest{
		Namespace: "test-ns",
		MetadataFilters: []*pb.MetadataFilter{
			{Key: "category", Value: "test"},
		},
	}, stream)

	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListVectors Tests
// =============================================================================

func TestListVectors_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockListVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.ListVectors(nil, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestListVectors_EmptyNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockListVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.ListVectors(&pb.ListVectorsRequest{
		Namespace: "",
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestListVectors_NegativePageSize(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockListVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.ListVectors(&pb.ListVectorsRequest{
		Namespace: "test-ns",
		PageSize:  -1,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "page size cannot be negative")
}

func TestListVectors_PageSizeExceedsMax(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockListVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.ListVectors(&pb.ListVectorsRequest{
		Namespace: "test-ns",
		PageSize:  1001,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "page size cannot exceed")
}

func TestListVectors_NamespaceNotFound(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetNamespaceInfo", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrNotFound, Op: "GetNamespaceInfo"})

	stream := NewMockListVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.ListVectors(&pb.ListVectorsRequest{
		Namespace: "nonexistent-ns",
	}, stream)

	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}

func TestListVectors_Success(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetNamespaceInfo", mock.Anything, mock.Anything).
		Return(&service.GetNamespaceInfoResponse{
			Namespace: "test-ns",
			Dimension: 128,
		}, nil)

	mockSvc.On("SearchByMetadata", mock.Anything, mock.Anything).
		Return(&service.SearchByMetadataResponse{
			Results: []*service.VectorData{
				{ID: 1, Vector: []float32{1.0}, Metadata: map[string]interface{}{"cat": "A"}, Version: 1},
				{ID: 2, Vector: []float32{2.0}, Metadata: map[string]interface{}{"cat": "B"}, Version: 1},
			},
			TotalCount: 2,
			NextCursor: "next_page",
		}, nil)

	stream := NewMockListVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.ListVectors(&pb.ListVectorsRequest{
		Namespace:       "test-ns",
		PageSize:        100,
		IncludeVectors:  true,
		IncludeMetadata: true,
	}, stream)

	require.NoError(t, err)
	msgs := stream.GetMessages()
	require.Len(t, msgs, 1)
	assert.Len(t, msgs[0].Vectors, 2)
	assert.Equal(t, int64(2), msgs[0].TotalCount)
	assert.Equal(t, "next_page", msgs[0].NextPageToken)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetVectorStats Tests
// =============================================================================

func TestGetVectorStats_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.GetVectorStats(tenantctx.WithSystem(context.Background()), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetVectorStats_WithNamespace(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetIndexStats", mock.Anything, mock.Anything).
		Return(&service.GetIndexStatsResponse{
			Namespace:      "test-ns",
			VectorCount:    100,
			Dimension:      128,
			IndexSizeBytes: 1024000,
			SearchPerformance: &service.IndexPerformanceMetrics{
				AverageSearchTimeMs: 5.0,
				TotalSearches:       1000,
			},
		}, nil)

	resp, err := srv.GetVectorStats(tenantctx.WithSystem(context.Background()), &pb.GetVectorStatsRequest{
		Namespace: "test-ns",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, int64(100), resp.TotalVectors)
	mockSvc.AssertExpectations(t)
}

func TestGetVectorStats_ClusterWide(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("ListNamespaces", mock.Anything, mock.Anything).
		Return(&service.ListNamespacesResponse{
			Namespaces: []*service.NamespaceInfo{
				{Namespace: "ns1", Dimension: 128, VectorCount: 100},
				{Namespace: "ns2", Dimension: 256, VectorCount: 200},
			},
			TotalCount: 2,
		}, nil)

	mockSvc.On("GetIndexStats", mock.Anything, mock.MatchedBy(func(req *service.GetIndexStatsRequest) bool {
		return req.Namespace == "ns1"
	})).Return(&service.GetIndexStatsResponse{
		Namespace:      "ns1",
		VectorCount:    100,
		Dimension:      128,
		IndexSizeBytes: 1024000,
		SearchPerformance: &service.IndexPerformanceMetrics{
			AverageSearchTimeMs: 5.0,
			TotalSearches:       500,
		},
	}, nil)

	mockSvc.On("GetIndexStats", mock.Anything, mock.MatchedBy(func(req *service.GetIndexStatsRequest) bool {
		return req.Namespace == "ns2"
	})).Return(&service.GetIndexStatsResponse{
		Namespace:      "ns2",
		VectorCount:    200,
		Dimension:      256,
		IndexSizeBytes: 2048000,
		SearchPerformance: &service.IndexPerformanceMetrics{
			AverageSearchTimeMs: 10.0,
			TotalSearches:       500,
		},
	}, nil)

	resp, err := srv.GetVectorStats(tenantctx.WithSystem(context.Background()), &pb.GetVectorStatsRequest{
		Namespace:      "",
		IncludeDetails: false,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, int64(300), resp.TotalVectors)
	assert.Equal(t, int32(2), resp.TotalNamespaces)
	mockSvc.AssertExpectations(t)
}

func TestGetVectorStats_ClusterWideWithDetails(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("ListNamespaces", mock.Anything, mock.Anything).
		Return(&service.ListNamespacesResponse{
			Namespaces: []*service.NamespaceInfo{
				{Namespace: "ns1"},
			},
			TotalCount: 1,
		}, nil)

	mockSvc.On("GetIndexStats", mock.Anything, mock.Anything).
		Return(&service.GetIndexStatsResponse{
			Namespace:      "ns1",
			VectorCount:    100,
			Dimension:      128,
			IndexSizeBytes: 1024000,
			SearchPerformance: &service.IndexPerformanceMetrics{
				AverageSearchTimeMs: 5.0,
				TotalSearches:       500,
			},
		}, nil)

	resp, err := srv.GetVectorStats(tenantctx.WithSystem(context.Background()), &pb.GetVectorStatsRequest{
		Namespace:      "",
		IncludeDetails: true,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.NamespaceStats, 1)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// RebuildIndex Tests
// =============================================================================

func TestRebuildIndex_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.RebuildIndex(tenantctx.WithSystem(context.Background()), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestRebuildIndex_EmptyNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.RebuildIndex(tenantctx.WithSystem(context.Background()), &pb.RebuildIndexRequest{
		Namespace: "",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestRebuildIndex_Success(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("RebuildIndex", mock.Anything, mock.Anything).
		Return(&service.RebuildIndexResponse{
			JobID:                  "job-123",
			Timestamp:              1234567890,
			EstimatedTimeRemaining: 60,
		}, nil)

	resp, err := srv.RebuildIndex(tenantctx.WithSystem(context.Background()), &pb.RebuildIndexRequest{
		Namespace:   "test-ns",
		IndexParams: map[string]string{"M": "32"},
		Async:       true,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "job-123", resp.OperationId)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetIndexProgress Tests
// =============================================================================

func TestGetIndexProgress_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.GetIndexProgress(tenantctx.WithSystem(context.Background()), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetIndexProgress_NoOperationOrNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.GetIndexProgress(tenantctx.WithSystem(context.Background()), &pb.GetIndexProgressRequest{
		OperationId: "",
		Namespace:   "",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetIndexProgress_Success(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetRebuildProgress", mock.Anything, mock.Anything).
		Return(&service.GetRebuildProgressResponse{
			JobID:                    "job-123",
			Namespace:                "test-ns",
			VectorsProcessed:         500,
			TotalVectors:             1000,
			PercentComplete:          50.0,
			ElapsedTimeMs:            30000,
			EstimatedTimeRemainingMs: 30000,
			Status:                   "in_progress",
		}, nil)

	resp, err := srv.GetIndexProgress(tenantctx.WithSystem(context.Background()), &pb.GetIndexProgressRequest{
		OperationId: "job-123",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, float64(50.0), resp.PercentComplete)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetClusterStats Tests
// =============================================================================

func TestGetClusterStats_NilRequest(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock ClusterStatus
	mockSvc.On("ClusterStatus", mock.Anything, mock.Anything).
		Return(&service.ClusterStatusResponse{
			Nodes: []*service.NodeInfo{
				{NodeID: 1, Address: "node1:5000", IsLeader: true},
			},
		}, nil)

	// Mock ListNamespaces
	mockSvc.On("ListNamespaces", mock.Anything, mock.Anything).
		Return(&service.ListNamespacesResponse{
			Namespaces: []*service.NamespaceInfo{},
			TotalCount: 0,
		}, nil)

	// With nil request, the handler creates an empty request
	resp, err := srv.GetClusterStats(tenantctx.WithSystem(context.Background()), nil)

	require.NoError(t, err)
	require.NotNil(t, resp)
	mockSvc.AssertExpectations(t)
}

func TestGetClusterStats_WithNamespaces(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock ClusterStatus
	mockSvc.On("ClusterStatus", mock.Anything, mock.Anything).
		Return(&service.ClusterStatusResponse{
			Nodes: []*service.NodeInfo{
				{NodeID: 1, Address: "node1:5000", IsLeader: true},
				{NodeID: 2, Address: "node2:5000", IsLeader: false},
			},
		}, nil)

	// Mock ListNamespaces
	mockSvc.On("ListNamespaces", mock.Anything, mock.Anything).
		Return(&service.ListNamespacesResponse{
			Namespaces: []*service.NamespaceInfo{
				{Namespace: "ns1"},
				{Namespace: "ns2"},
			},
			TotalCount: 2,
		}, nil)

	// Mock GetIndexStats for each namespace
	mockSvc.On("GetIndexStats", mock.Anything, mock.MatchedBy(func(req *service.GetIndexStatsRequest) bool {
		return req.Namespace == "ns1"
	})).Return(&service.GetIndexStatsResponse{
		Namespace:      "ns1",
		VectorCount:    100,
		IndexSizeBytes: 1024000,
		SearchPerformance: &service.IndexPerformanceMetrics{
			AverageSearchTimeMs: 5.0,
			TotalSearches:       500,
		},
	}, nil)

	mockSvc.On("GetIndexStats", mock.Anything, mock.MatchedBy(func(req *service.GetIndexStatsRequest) bool {
		return req.Namespace == "ns2"
	})).Return(&service.GetIndexStatsResponse{
		Namespace:      "ns2",
		VectorCount:    200,
		IndexSizeBytes: 2048000,
		SearchPerformance: &service.IndexPerformanceMetrics{
			AverageSearchTimeMs: 10.0,
			TotalSearches:       500,
		},
	}, nil)

	resp, err := srv.GetClusterStats(tenantctx.WithSystem(context.Background()), &pb.GetClusterStatsRequest{
		IncludeNamespaces: true,
		IncludeNodes:      true,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, int32(2), resp.TotalNodes)
	assert.Equal(t, int64(300), resp.TotalVectors)
	assert.Equal(t, int32(2), resp.TotalNamespaces)
	assert.Len(t, resp.NamespaceStats, 2)
	assert.Len(t, resp.NodeStats, 1)
	mockSvc.AssertExpectations(t)
}

func TestGetClusterStats_ClusterStatusError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock ClusterStatus to return error - should continue with defaults
	mockSvc.On("ClusterStatus", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrInternal, Op: "ClusterStatus"})

	// Mock ListNamespaces
	mockSvc.On("ListNamespaces", mock.Anything, mock.Anything).
		Return(&service.ListNamespacesResponse{
			Namespaces: []*service.NamespaceInfo{},
			TotalCount: 0,
		}, nil)

	resp, err := srv.GetClusterStats(tenantctx.WithSystem(context.Background()), &pb.GetClusterStatsRequest{})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, int32(1), resp.TotalNodes) // Default to 1
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// CompactNamespace Tests
// =============================================================================

func TestCompactNamespace_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.CompactNamespace(tenantctx.WithSystem(context.Background()), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestCompactNamespace_EmptyNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.CompactNamespace(tenantctx.WithSystem(context.Background()), &pb.CompactNamespaceRequest{
		Namespace: "",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestCompactNamespace_Success(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("CompactNamespace", mock.Anything, mock.Anything).
		Return(&service.CompactNamespaceResponse{
			OperationID: "compact-123",
			StartedAt:   1234567890,
		}, nil)

	resp, err := srv.CompactNamespace(tenantctx.WithSystem(context.Background()), &pb.CompactNamespaceRequest{
		Namespace: "test-ns",
		Async:     true,
		TimeoutMs: 60000,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "compact-123", resp.OperationId)
	mockSvc.AssertExpectations(t)
}

func TestCompactNamespace_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("CompactNamespace", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrInternal, Op: "CompactNamespace"})

	resp, err := srv.CompactNamespace(tenantctx.WithSystem(context.Background()), &pb.CompactNamespaceRequest{
		Namespace: "test-ns",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetCompactionProgress Tests
// =============================================================================

func TestGetCompactionProgress_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.GetCompactionProgress(tenantctx.WithSystem(context.Background()), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetCompactionProgress_EmptyNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.GetCompactionProgress(tenantctx.WithSystem(context.Background()), &pb.GetCompactionProgressRequest{
		Namespace: "",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetCompactionProgress_InProgress(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetCompactionProgress", mock.Anything, mock.Anything).
		Return(&service.GetCompactionProgressResponse{
			OperationID:     "compact-123",
			Namespace:       "test-ns",
			BytesCompacted:  512000,
			TotalBytes:      1024000,
			PercentComplete: 50.0,
			InProgress:      true,
			StartedAt:       1234567890,
		}, nil)

	resp, err := srv.GetCompactionProgress(tenantctx.WithSystem(context.Background()), &pb.GetCompactionProgressRequest{
		Namespace:   "test-ns",
		OperationId: "compact-123",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "compact-123", resp.OperationId)
	assert.Equal(t, float64(50.0), resp.PercentComplete)
	assert.Equal(t, "in_progress", resp.Status)
	mockSvc.AssertExpectations(t)
}

func TestGetCompactionProgress_Completed(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetCompactionProgress", mock.Anything, mock.Anything).
		Return(&service.GetCompactionProgressResponse{
			OperationID:     "compact-123",
			Namespace:       "test-ns",
			BytesCompacted:  1024000,
			TotalBytes:      1024000,
			PercentComplete: 100.0,
			InProgress:      false,
			StartedAt:       1234567890,
		}, nil)

	resp, err := srv.GetCompactionProgress(tenantctx.WithSystem(context.Background()), &pb.GetCompactionProgressRequest{
		Namespace: "test-ns",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "completed", resp.Status)
	mockSvc.AssertExpectations(t)
}

func TestGetCompactionProgress_Failed(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetCompactionProgress", mock.Anything, mock.Anything).
		Return(&service.GetCompactionProgressResponse{
			OperationID: "compact-123",
			Namespace:   "test-ns",
			InProgress:  false,
			Error:       "disk space exhausted",
		}, nil)

	resp, err := srv.GetCompactionProgress(tenantctx.WithSystem(context.Background()), &pb.GetCompactionProgressRequest{
		Namespace: "test-ns",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "failed", resp.Status)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ExportVectors Tests
// =============================================================================

func TestExportVectors_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockExportVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.ExportVectors(nil, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestExportVectors_EmptyNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockExportVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.ExportVectors(&pb.ExportVectorsRequest{
		Namespace: "",
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestExportVectors_EmptyNamespace_EmptyBatch(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock SearchByMetadata to return empty results
	mockSvc.On("SearchByMetadata", mock.Anything, mock.Anything).
		Return(&service.SearchByMetadataResponse{
			Results:    []*service.VectorData{},
			TotalCount: 0,
		}, nil)

	stream := NewMockExportVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.ExportVectors(&pb.ExportVectorsRequest{
		Namespace: "empty-ns",
	}, stream)

	require.NoError(t, err)
	msgs := stream.GetMessages()
	require.Len(t, msgs, 1)
	assert.True(t, msgs[0].IsFinal)
	assert.Equal(t, int64(0), msgs[0].VectorsExported)
	mockSvc.AssertExpectations(t)
}

func TestExportVectors_SingleBatch(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock SearchByMetadata to return a small batch
	mockSvc.On("SearchByMetadata", mock.Anything, mock.Anything).
		Return(&service.SearchByMetadataResponse{
			Results: []*service.VectorData{
				{ID: 1, Vector: []float32{1.0, 2.0}, Metadata: map[string]interface{}{"cat": "A"}},
				{ID: 2, Vector: []float32{3.0, 4.0}, Metadata: map[string]interface{}{"cat": "B"}},
			},
			TotalCount: 2,
		}, nil).Once()

	stream := NewMockExportVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.ExportVectors(&pb.ExportVectorsRequest{
		Namespace:   "test-ns",
		BatchSize:   1000,
		Format:      "json",
		Compression: "none",
	}, stream)

	require.NoError(t, err)
	msgs := stream.GetMessages()
	require.Len(t, msgs, 1)
	assert.True(t, msgs[0].IsFinal)
	assert.Equal(t, int64(2), msgs[0].VectorsExported)
	mockSvc.AssertExpectations(t)
}

func TestExportVectors_MultipleBatches(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// First batch - full batch size
	mockSvc.On("SearchByMetadata", mock.Anything, mock.MatchedBy(func(req *service.SearchByMetadataRequest) bool {
		return req.Offset == 0
	})).Return(&service.SearchByMetadataResponse{
		Results: []*service.VectorData{
			{ID: 1, Vector: []float32{1.0}, Metadata: map[string]interface{}{}},
			{ID: 2, Vector: []float32{2.0}, Metadata: map[string]interface{}{}},
		},
		TotalCount: 3,
	}, nil).Once()

	// Second batch - partial (last batch)
	mockSvc.On("SearchByMetadata", mock.Anything, mock.MatchedBy(func(req *service.SearchByMetadataRequest) bool {
		return req.Offset == 2
	})).Return(&service.SearchByMetadataResponse{
		Results: []*service.VectorData{
			{ID: 3, Vector: []float32{3.0}, Metadata: map[string]interface{}{}},
		},
		TotalCount: 3,
	}, nil).Once()

	stream := NewMockExportVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.ExportVectors(&pb.ExportVectorsRequest{
		Namespace: "test-ns",
		BatchSize: 2,
	}, stream)

	require.NoError(t, err)
	msgs := stream.GetMessages()
	require.Len(t, msgs, 2)
	assert.False(t, msgs[0].IsFinal)
	assert.True(t, msgs[1].IsFinal)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ImportVectors Tests
// =============================================================================

func TestImportVectors_EmptyStream(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockImportVectorsStream(tenantctx.WithSystem(context.Background()))
	// Stream ends immediately with EOF
	stream.SetRecvError(io.EOF)

	err := srv.ImportVectors(stream)

	require.NoError(t, err)
	resp := stream.GetResponse()
	require.NotNil(t, resp)
	assert.Equal(t, int64(0), resp.VectorsImported)
}

func TestImportVectors_MissingNamespaceInFirstMessage(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockImportVectorsStream(tenantctx.WithSystem(context.Background()))
	stream.AddRecvMessage(&pb.ImportVectorsRequest{
		Namespace: "",
		BatchData: []byte("{}"),
	})

	err := srv.ImportVectors(stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "namespace required")
}

func TestImportVectors_Success(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock BatchUpsert
	mockSvc.On("BatchUpsert", mock.Anything, mock.Anything).
		Return(&service.BatchUpsertResponse{
			InsertedCount: 2,
			Timestamp:     1234567890,
		}, nil)

	stream := NewMockImportVectorsStream(tenantctx.WithSystem(context.Background()))

	// Create proper JSON batch data
	batchData := `{"vectors":[{"id":"1","vector":[1.0,2.0]},{"id":"2","vector":[3.0,4.0]}],"namespace":"test-ns","batch_num":1,"total":2}`

	stream.AddRecvMessage(&pb.ImportVectorsRequest{
		Namespace:         "test-ns",
		BatchData:         []byte(batchData),
		Format:            "json",
		OverwriteExisting: true,
	})

	err := srv.ImportVectors(stream)

	require.NoError(t, err)
	resp := stream.GetResponse()
	require.NotNil(t, resp)
	assert.Equal(t, int64(2), resp.VectorsImported)
	mockSvc.AssertExpectations(t)
}

func TestImportVectors_SkipExisting(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock GetVector to return that vector exists
	mockSvc.On("GetVector", mock.Anything, mock.Anything).
		Return(&service.GetVectorResponse{
			Found: true,
			ID:    1,
		}, nil)

	stream := NewMockImportVectorsStream(tenantctx.WithSystem(context.Background()))

	// Create proper JSON batch data with one vector
	batchData := `{"vectors":[{"id":"1","vector":[1.0,2.0]}],"namespace":"test-ns","batch_num":1,"total":1}`

	stream.AddRecvMessage(&pb.ImportVectorsRequest{
		Namespace:         "test-ns",
		BatchData:         []byte(batchData),
		Format:            "json",
		OverwriteExisting: false,
	})

	err := srv.ImportVectors(stream)

	require.NoError(t, err)
	resp := stream.GetResponse()
	require.NotNil(t, resp)
	assert.Equal(t, int64(1), resp.VectorsSkipped)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// UpdateMetadata Tests
// =============================================================================

func TestUpdateMetadata_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.UpdateMetadata(tenantctx.WithSystem(context.Background()), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUpdateMetadata_EmptyNamespace(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.UpdateMetadata(tenantctx.WithSystem(context.Background()), &pb.UpdateMetadataRequest{
		Namespace: "",
		Id:        "vec1",
		Metadata:  map[string]string{"key": "value"},
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUpdateMetadata_EmptyID(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.UpdateMetadata(tenantctx.WithSystem(context.Background()), &pb.UpdateMetadataRequest{
		Namespace: "test-ns",
		Id:        "",
		Metadata:  map[string]string{"key": "value"},
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUpdateMetadata_EmptyMetadata(t *testing.T) {
	srv, _ := newMockTestServer(t)

	resp, err := srv.UpdateMetadata(tenantctx.WithSystem(context.Background()), &pb.UpdateMetadataRequest{
		Namespace: "test-ns",
		Id:        "vec1",
		Metadata:  map[string]string{},
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "metadata cannot be empty")
}

func TestUpdateMetadata_VectorNotFound(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetVector", mock.Anything, mock.Anything).
		Return(&service.GetVectorResponse{
			Found: false,
		}, nil)

	resp, err := srv.UpdateMetadata(tenantctx.WithSystem(context.Background()), &pb.UpdateMetadataRequest{
		Namespace: "test-ns",
		Id:        "vec1",
		Metadata:  map[string]string{"key": "value"},
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockSvc.AssertExpectations(t)
}

func TestUpdateMetadata_Success_Merge(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetVector", mock.Anything, mock.Anything).
		Return(&service.GetVectorResponse{
			Found:    true,
			ID:       123,
			Vector:   []float32{1.0, 2.0},
			Metadata: map[string]interface{}{"existing": "value"},
		}, nil)

	mockSvc.On("UpsertVector", mock.Anything, mock.Anything).
		Return(&service.UpsertVectorResponse{
			Success:   true,
			Version:   2,
			Timestamp: 1234567890,
		}, nil)

	resp, err := srv.UpdateMetadata(tenantctx.WithSystem(context.Background()), &pb.UpdateMetadataRequest{
		Namespace: "test-ns",
		Id:        "vec1",
		Metadata:  map[string]string{"new": "data"},
		Merge:     true,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	mockSvc.AssertExpectations(t)
}

func TestUpdateMetadata_Success_Replace(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetVector", mock.Anything, mock.Anything).
		Return(&service.GetVectorResponse{
			Found:    true,
			ID:       123,
			Vector:   []float32{1.0, 2.0},
			Metadata: map[string]interface{}{"existing": "value"},
		}, nil)

	mockSvc.On("UpsertVector", mock.Anything, mock.Anything).
		Return(&service.UpsertVectorResponse{
			Success:   true,
			Version:   2,
			Timestamp: 1234567890,
		}, nil)

	resp, err := srv.UpdateMetadata(tenantctx.WithSystem(context.Background()), &pb.UpdateMetadataRequest{
		Namespace: "test-ns",
		Id:        "vec1",
		Metadata:  map[string]string{"new": "data"},
		Merge:     false,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// MockServerStreamBase Concurrency Tests
// =============================================================================

func TestMockServerStreamBase_ConcurrentAccess(t *testing.T) {
	base := &MockServerStreamBase{ctx: tenantctx.WithSystem(context.Background())}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = base.SetHeader(metadata.New(map[string]string{"key": "value"}))
			_ = base.SendHeader(metadata.New(map[string]string{"key": "value"}))
			base.SetTrailer(metadata.New(map[string]string{"key": "value"}))
			_ = base.SendMsg("test")
			_ = base.RecvMsg(nil)
			_ = base.Context()
		}()
	}
	wg.Wait()
}

// =============================================================================
// Additional SearchByID Tests for Coverage
// =============================================================================

func TestSearchByID_GetVectorError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock GetVector to return an error
	mockSvc.On("GetVector", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrInternal, Op: "GetVector"})

	stream := NewMockSearchByIDStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByID(&pb.SearchByIDRequest{
		Namespace: "test-ns",
		Id:        "vec1",
		K:         10,
	}, stream)

	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}

func TestSearchByID_SearchSimilarError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock GetVector to return the source vector
	mockSvc.On("GetVector", mock.Anything, mock.Anything).
		Return(&service.GetVectorResponse{
			Found:  true,
			ID:     123,
			Vector: []float32{1.0, 2.0, 3.0},
		}, nil)

	// Mock SearchSimilar to return an error
	mockSvc.On("SearchSimilar", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrInternal, Op: "SearchSimilar"})

	stream := NewMockSearchByIDStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByID(&pb.SearchByIDRequest{
		Namespace: "test-ns",
		Id:        "123",
		K:         10,
	}, stream)

	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}

func TestSearchByID_WithOriginalIDMetadata(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock GetVector to return the source vector
	mockSvc.On("GetVector", mock.Anything, mock.Anything).
		Return(&service.GetVectorResponse{
			Found:  true,
			ID:     123,
			Vector: []float32{1.0, 2.0, 3.0},
		}, nil)

	// Mock SearchSimilar to return results with __original_id metadata
	mockSvc.On("SearchSimilar", mock.Anything, mock.Anything).
		Return(&service.SearchSimilarResponse{
			Results: []*service.VectorSearchResult{
				{
					ID:       456,
					Score:    0.95,
					Vector:   []float32{1.1, 2.1, 3.1},
					Rank:     1,
					Metadata: map[string]interface{}{"__original_id": "my-string-id", "category": "test"},
				},
				{
					ID:       789,
					Score:    0.85,
					Vector:   []float32{1.2, 2.2, 3.2},
					Rank:     2,
					Metadata: map[string]interface{}{"category": "other"},
				},
			},
		}, nil)

	stream := NewMockSearchByIDStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByID(&pb.SearchByIDRequest{
		Namespace:       "test-ns",
		Id:              "123",
		K:               10,
		IncludeVectors:  true,
		IncludeMetadata: true,
		IncludeSelf:     false,
	}, stream)

	require.NoError(t, err)
	msgs := stream.GetMessages()
	require.Len(t, msgs, 1)
	require.Len(t, msgs[0].Results, 2)
	// First result should use __original_id as the ID
	assert.Equal(t, "my-string-id", msgs[0].Results[0].Vector.Id)
	// __original_id should be filtered from metadata
	_, hasOriginalID := msgs[0].Results[0].Vector.Metadata["__original_id"]
	assert.False(t, hasOriginalID)
	assert.Equal(t, "test", msgs[0].Results[0].Vector.Metadata["category"])
	// Second result should use numeric ID
	assert.Equal(t, "789", msgs[0].Results[1].Vector.Id)
	mockSvc.AssertExpectations(t)
}

func TestSearchByID_VectorEmptyAfterGet(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock GetVector to return found=true but empty vector
	mockSvc.On("GetVector", mock.Anything, mock.Anything).
		Return(&service.GetVectorResponse{
			Found:  true,
			ID:     123,
			Vector: []float32{}, // Empty vector
		}, nil)

	stream := NewMockSearchByIDStream(tenantctx.WithSystem(context.Background()))
	err := srv.SearchByID(&pb.SearchByIDRequest{
		Namespace: "test-ns",
		Id:        "vec1",
		K:         10,
	}, stream)

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Additional ImportVectors Tests for Coverage
// =============================================================================

func TestImportVectors_BatchUpsertError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock BatchUpsert to return an error
	mockSvc.On("BatchUpsert", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrInternal, Op: "BatchUpsert"})

	stream := NewMockImportVectorsStream(tenantctx.WithSystem(context.Background()))

	// Create proper JSON batch data
	batchData := `{"vectors":[{"id":"1","vector":[1.0,2.0]}],"namespace":"test-ns","batch_num":1,"total":1}`

	stream.AddRecvMessage(&pb.ImportVectorsRequest{
		Namespace:         "test-ns",
		BatchData:         []byte(batchData),
		Format:            "json",
		OverwriteExisting: true,
	})

	err := srv.ImportVectors(stream)

	require.NoError(t, err) // The import should still complete
	resp := stream.GetResponse()
	require.NotNil(t, resp)
	assert.Equal(t, int64(1), resp.VectorsFailed) // But the vector should be marked as failed
	mockSvc.AssertExpectations(t)
}

func TestImportVectors_DeserializeError(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockImportVectorsStream(tenantctx.WithSystem(context.Background()))

	// Create invalid JSON batch data
	stream.AddRecvMessage(&pb.ImportVectorsRequest{
		Namespace:         "test-ns",
		BatchData:         []byte("invalid json{{{"),
		Format:            "json",
		OverwriteExisting: true,
	})

	err := srv.ImportVectors(stream)

	require.NoError(t, err) // The import should still complete
	resp := stream.GetResponse()
	require.NotNil(t, resp)
	assert.Equal(t, int64(1), resp.VectorsFailed) // But the batch should be marked as failed
}

func TestImportVectors_EmptyBatchData(t *testing.T) {
	srv, _ := newMockTestServer(t)

	stream := NewMockImportVectorsStream(tenantctx.WithSystem(context.Background()))

	// First message with namespace but empty batch data
	stream.AddRecvMessage(&pb.ImportVectorsRequest{
		Namespace:         "test-ns",
		BatchData:         []byte{}, // Empty batch data
		Format:            "json",
		OverwriteExisting: true,
	})

	err := srv.ImportVectors(stream)

	require.NoError(t, err)
	resp := stream.GetResponse()
	require.NotNil(t, resp)
	assert.Equal(t, int64(0), resp.VectorsImported)
	assert.Equal(t, int64(0), resp.VectorsFailed)
}

func TestImportVectors_DefaultFormat(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock BatchUpsert
	mockSvc.On("BatchUpsert", mock.Anything, mock.Anything).
		Return(&service.BatchUpsertResponse{
			InsertedCount: 1,
			Timestamp:     1234567890,
		}, nil)

	stream := NewMockImportVectorsStream(tenantctx.WithSystem(context.Background()))

	// Create proper JSON batch data but with empty format (should default to json)
	batchData := `{"vectors":[{"id":"1","vector":[1.0,2.0]}],"namespace":"test-ns","batch_num":1,"total":1}`

	stream.AddRecvMessage(&pb.ImportVectorsRequest{
		Namespace:         "test-ns",
		BatchData:         []byte(batchData),
		Format:            "", // Empty format should default to json
		OverwriteExisting: true,
	})

	err := srv.ImportVectors(stream)

	require.NoError(t, err)
	resp := stream.GetResponse()
	require.NotNil(t, resp)
	assert.Equal(t, int64(1), resp.VectorsImported)
	mockSvc.AssertExpectations(t)
}

func TestImportVectors_MultipleBatches(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock BatchUpsert to be called twice
	mockSvc.On("BatchUpsert", mock.Anything, mock.Anything).
		Return(&service.BatchUpsertResponse{
			InsertedCount: 2,
			Timestamp:     1234567890,
		}, nil).Times(2)

	stream := NewMockImportVectorsStream(tenantctx.WithSystem(context.Background()))

	// First batch
	batchData1 := `{"vectors":[{"id":"1","vector":[1.0,2.0]},{"id":"2","vector":[3.0,4.0]}],"namespace":"test-ns","batch_num":1,"total":2}`
	stream.AddRecvMessage(&pb.ImportVectorsRequest{
		Namespace:         "test-ns",
		BatchData:         []byte(batchData1),
		Format:            "json",
		OverwriteExisting: true,
	})

	// Second batch (namespace already set, so it can be empty)
	batchData2 := `{"vectors":[{"id":"3","vector":[5.0,6.0]},{"id":"4","vector":[7.0,8.0]}],"namespace":"test-ns","batch_num":2,"total":2}`
	stream.AddRecvMessage(&pb.ImportVectorsRequest{
		Namespace:         "",
		BatchData:         []byte(batchData2),
		Format:            "",
		OverwriteExisting: true,
	})

	err := srv.ImportVectors(stream)

	require.NoError(t, err)
	resp := stream.GetResponse()
	require.NotNil(t, resp)
	assert.Equal(t, int64(4), resp.VectorsImported) // 2 vectors per batch x 2 batches
	mockSvc.AssertExpectations(t)
}

func TestImportVectors_SkipExistingVectorNotFound(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock GetVector to return not found error (vector doesn't exist)
	mockSvc.On("GetVector", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrNotFound, Op: "GetVector"})

	// Mock BatchUpsert to insert the vector
	mockSvc.On("BatchUpsert", mock.Anything, mock.Anything).
		Return(&service.BatchUpsertResponse{
			InsertedCount: 1,
			Timestamp:     1234567890,
		}, nil)

	stream := NewMockImportVectorsStream(tenantctx.WithSystem(context.Background()))

	batchData := `{"vectors":[{"id":"1","vector":[1.0,2.0]}],"namespace":"test-ns","batch_num":1,"total":1}`

	stream.AddRecvMessage(&pb.ImportVectorsRequest{
		Namespace:         "test-ns",
		BatchData:         []byte(batchData),
		Format:            "json",
		OverwriteExisting: false, // Don't overwrite - check if exists first
	})

	err := srv.ImportVectors(stream)

	require.NoError(t, err)
	resp := stream.GetResponse()
	require.NotNil(t, resp)
	assert.Equal(t, int64(1), resp.VectorsImported) // Vector was imported since it didn't exist
	assert.Equal(t, int64(0), resp.VectorsSkipped)
	mockSvc.AssertExpectations(t)
}

func TestImportVectors_AllVectorsSkipped(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock GetVector to return that vectors exist (for skip check)
	mockSvc.On("GetVector", mock.Anything, mock.Anything).
		Return(&service.GetVectorResponse{Found: true}, nil).Times(2)

	stream := NewMockImportVectorsStream(tenantctx.WithSystem(context.Background()))

	// Batch with 2 vectors that will all be skipped
	batchData := `{"vectors":[{"id":"1","vector":[1.0,2.0]},{"id":"2","vector":[3.0,4.0]}],"namespace":"test-ns","batch_num":1,"total":1}`

	stream.AddRecvMessage(&pb.ImportVectorsRequest{
		Namespace:         "test-ns",
		BatchData:         []byte(batchData),
		Format:            "json",
		OverwriteExisting: false, // Don't overwrite existing
	})

	err := srv.ImportVectors(stream)

	require.NoError(t, err)
	resp := stream.GetResponse()
	require.NotNil(t, resp)
	assert.Equal(t, int64(0), resp.VectorsImported)
	assert.Equal(t, int64(2), resp.VectorsSkipped) // Both vectors were skipped
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Additional Tests for Coverage Improvement
// =============================================================================

func TestDeleteNamespace_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("DeleteNamespace", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrInternal, Op: "DeleteNamespace"})

	resp, err := srv.DeleteNamespace(tenantctx.WithSystem(context.Background()), &pb.DeleteNamespaceRequest{
		Namespace: "test-ns",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	mockSvc.AssertExpectations(t)
}

func TestListNamespaces_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("ListNamespaces", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrInternal, Op: "ListNamespaces"})

	resp, err := srv.ListNamespaces(tenantctx.WithSystem(context.Background()), &pb.ListNamespacesRequest{})

	require.Error(t, err)
	assert.Nil(t, resp)
	mockSvc.AssertExpectations(t)
}

func TestGetVector_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetVector", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrInternal, Op: "GetVector"})

	resp, err := srv.GetVector(tenantctx.WithSystem(context.Background()), &pb.GetVectorRequest{
		Namespace: "test-ns",
		Id:        "vec1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	mockSvc.AssertExpectations(t)
}

func TestDeleteVector_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("DeleteVector", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrInternal, Op: "DeleteVector"})

	resp, err := srv.DeleteVector(tenantctx.WithSystem(context.Background()), &pb.DeleteVectorRequest{
		Namespace: "test-ns",
		Id:        "vec1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	mockSvc.AssertExpectations(t)
}

func TestGetVectorStats_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetIndexStats", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrInternal, Op: "GetStats"})

	resp, err := srv.GetVectorStats(tenantctx.WithSystem(context.Background()), &pb.GetVectorStatsRequest{
		Namespace: "test-ns",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	mockSvc.AssertExpectations(t)
}

func TestRebuildIndex_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("RebuildIndex", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrInternal, Op: "RebuildIndex"})

	resp, err := srv.RebuildIndex(tenantctx.WithSystem(context.Background()), &pb.RebuildIndexRequest{
		Namespace: "test-ns",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	mockSvc.AssertExpectations(t)
}

func TestGetIndexProgress_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetRebuildProgress", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrInternal, Op: "GetIndexProgress"})

	resp, err := srv.GetIndexProgress(tenantctx.WithSystem(context.Background()), &pb.GetIndexProgressRequest{
		Namespace:   "test-ns",
		OperationId: "op-123",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	mockSvc.AssertExpectations(t)
}

func TestExportVectors_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("SearchByMetadata", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrInternal, Op: "SearchByMetadata"})

	stream := NewMockExportVectorsStream(tenantctx.WithSystem(context.Background()))
	err := srv.ExportVectors(&pb.ExportVectorsRequest{
		Namespace: "test-ns",
		BatchSize: 100,
	}, stream)

	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}

func TestUpdateMetadata_ServiceError(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	// Mock GetVector to return an existing vector
	mockSvc.On("GetVector", mock.Anything, mock.Anything).
		Return(&service.GetVectorResponse{
			Found:    true,
			ID:       123,
			Metadata: map[string]interface{}{"existing": "value"},
		}, nil)

	// Mock UpsertVector to return an error
	mockSvc.On("UpsertVector", mock.Anything, mock.Anything).
		Return(nil, &errors.QBError{Code: errors.ErrInternal, Op: "UpsertVector"})

	resp, err := srv.UpdateMetadata(tenantctx.WithSystem(context.Background()), &pb.UpdateMetadataRequest{
		Namespace: "test-ns",
		Id:        "vec1",
		Metadata:  map[string]string{"new": "value"},
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	mockSvc.AssertExpectations(t)
}

func TestGetVector_WithOriginalIDMetadata(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)

	mockSvc.On("GetVector", mock.Anything, mock.Anything).
		Return(&service.GetVectorResponse{
			Found:     true,
			ID:        123,
			Vector:    []float32{1.0, 2.0, 3.0},
			Metadata:  map[string]interface{}{"__original_id": "my-string-id", "key": "value"},
			Version:   5,
			CreatedAt: 1000000000,
			UpdatedAt: 2000000000,
		}, nil)

	resp, err := srv.GetVector(tenantctx.WithSystem(context.Background()), &pb.GetVectorRequest{
		Namespace:       "test-ns",
		Id:              "my-string-id",
		IncludeVector:   true,
		IncludeMetadata: true,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Found)
	// Should use __original_id from metadata
	assert.Equal(t, "my-string-id", resp.Vector.Id)
	// __original_id should be filtered from metadata
	_, hasOriginalID := resp.Vector.Metadata["__original_id"]
	assert.False(t, hasOriginalID)
	assert.Equal(t, "value", resp.Vector.Metadata["key"])
	mockSvc.AssertExpectations(t)
}
