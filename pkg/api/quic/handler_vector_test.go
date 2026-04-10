//go:build quic

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
	"context"
	"testing"
	"time"

	stderrors "errors"
	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock Vector Service for Testing
// =============================================================================

type mockVectorService struct {
	// Namespace operations
	createNamespaceFn  func(ctx context.Context, req *service.CreateNamespaceRequest) (*service.CreateNamespaceResponse, error)
	deleteNamespaceFn  func(ctx context.Context, req *service.DeleteNamespaceRequest) (*service.DeleteNamespaceResponse, error)
	listNamespacesFn   func(ctx context.Context, req *service.ListNamespacesRequest) (*service.ListNamespacesResponse, error)
	getNamespaceInfoFn func(ctx context.Context, req *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error)

	// Vector CRUD operations
	upsertVectorFn func(ctx context.Context, req *service.UpsertVectorRequest) (*service.UpsertVectorResponse, error)
	getVectorFn    func(ctx context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error)
	deleteVectorFn func(ctx context.Context, req *service.DeleteVectorRequest) (*service.DeleteVectorResponse, error)
	batchUpsertFn  func(ctx context.Context, req *service.BatchUpsertRequest) (*service.BatchUpsertResponse, error)

	// Search operations
	searchSimilarFn    func(ctx context.Context, req *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error)
	searchByMetadataFn func(ctx context.Context, req *service.SearchByMetadataRequest) (*service.SearchByMetadataResponse, error)
	hybridSearchFn     func(ctx context.Context, req *service.HybridSearchRequest) (*service.HybridSearchResponse, error)

	// Index operations
	rebuildIndexFn       func(ctx context.Context, req *service.RebuildIndexRequest) (*service.RebuildIndexResponse, error)
	getRebuildProgressFn func(ctx context.Context, req *service.GetRebuildProgressRequest) (*service.GetRebuildProgressResponse, error)
	getIndexStatsFn      func(ctx context.Context, req *service.GetIndexStatsRequest) (*service.GetIndexStatsResponse, error)

	// Compaction operations
	compactNamespaceFn      func(ctx context.Context, req *service.CompactNamespaceRequest) (*service.CompactNamespaceResponse, error)
	getCompactionProgressFn func(ctx context.Context, req *service.GetCompactionProgressRequest) (*service.GetCompactionProgressResponse, error)
}

func (m *mockVectorService) CreateNamespace(ctx context.Context, req *service.CreateNamespaceRequest) (*service.CreateNamespaceResponse, error) {
	if m.createNamespaceFn != nil {
		return m.createNamespaceFn(ctx, req)
	}
	return &service.CreateNamespaceResponse{
		Namespace: req.Namespace,
		Dimension: req.Dimension,
		Timestamp: time.Now().Unix(),
		Success:   true,
	}, nil
}

func (m *mockVectorService) DeleteNamespace(ctx context.Context, req *service.DeleteNamespaceRequest) (*service.DeleteNamespaceResponse, error) {
	if m.deleteNamespaceFn != nil {
		return m.deleteNamespaceFn(ctx, req)
	}
	return &service.DeleteNamespaceResponse{
		Success:        true,
		VectorsDeleted: 100,
		Timestamp:      time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) ListNamespaces(ctx context.Context, req *service.ListNamespacesRequest) (*service.ListNamespacesResponse, error) {
	if m.listNamespacesFn != nil {
		return m.listNamespacesFn(ctx, req)
	}
	return &service.ListNamespacesResponse{
		Namespaces: []*service.NamespaceInfo{
			{Namespace: "test-ns", Dimension: 128},
		},
		TotalCount: 1,
		Timestamp:  time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) GetNamespaceInfo(ctx context.Context, req *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error) {
	if m.getNamespaceInfoFn != nil {
		return m.getNamespaceInfoFn(ctx, req)
	}
	return &service.GetNamespaceInfoResponse{
		Namespace:      req.Namespace,
		Dimension:      128,
		VectorCount:    1000,
		DistanceMetric: "L2",
		IndexType:      "HNSW",
		CreatedAt:      time.Now().Unix(),
		UpdatedAt:      time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) UpsertVector(ctx context.Context, req *service.UpsertVectorRequest) (*service.UpsertVectorResponse, error) {
	if m.upsertVectorFn != nil {
		return m.upsertVectorFn(ctx, req)
	}
	return &service.UpsertVectorResponse{
		ID:        req.ID,
		Version:   1,
		Timestamp: time.Now().Unix(),
		Success:   true,
	}, nil
}

func (m *mockVectorService) GetVector(ctx context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error) {
	if m.getVectorFn != nil {
		return m.getVectorFn(ctx, req)
	}
	return &service.GetVectorResponse{
		ID:        req.ID,
		Vector:    []float32{0.1, 0.2, 0.3},
		Metadata:  map[string]interface{}{"key": "value"},
		Version:   1,
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		Found:     true,
	}, nil
}

func (m *mockVectorService) DeleteVector(ctx context.Context, req *service.DeleteVectorRequest) (*service.DeleteVectorResponse, error) {
	if m.deleteVectorFn != nil {
		return m.deleteVectorFn(ctx, req)
	}
	return &service.DeleteVectorResponse{
		Success:   true,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) BatchUpsert(ctx context.Context, req *service.BatchUpsertRequest) (*service.BatchUpsertResponse, error) {
	if m.batchUpsertFn != nil {
		return m.batchUpsertFn(ctx, req)
	}
	ids := make([]uint64, len(req.Vectors))
	versions := make([]uint64, len(req.Vectors))
	for i, v := range req.Vectors {
		ids[i] = v.ID
		versions[i] = 1
	}
	return &service.BatchUpsertResponse{
		IDs:           ids,
		Versions:      versions,
		Timestamp:     time.Now().Unix(),
		Success:       true,
		InsertedCount: len(req.Vectors),
	}, nil
}

func (m *mockVectorService) SearchSimilar(ctx context.Context, req *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
	if m.searchSimilarFn != nil {
		return m.searchSimilarFn(ctx, req)
	}
	return &service.SearchSimilarResponse{
		Results: []*service.VectorSearchResult{
			{ID: 1, Score: 0.95, Distance: 0.05, Rank: 1},
			{ID: 2, Score: 0.90, Distance: 0.10, Rank: 2},
		},
		Timestamp:     time.Now().Unix(),
		QueryDuration: 150,
	}, nil
}

func (m *mockVectorService) SearchByMetadata(ctx context.Context, req *service.SearchByMetadataRequest) (*service.SearchByMetadataResponse, error) {
	if m.searchByMetadataFn != nil {
		return m.searchByMetadataFn(ctx, req)
	}
	return &service.SearchByMetadataResponse{
		Results: []*service.VectorData{
			{ID: 1, Vector: []float32{0.1, 0.2}, Metadata: map[string]interface{}{"key": "value"}},
		},
		TotalCount: 1,
		Timestamp:  time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) HybridSearch(ctx context.Context, req *service.HybridSearchRequest) (*service.HybridSearchResponse, error) {
	if m.hybridSearchFn != nil {
		return m.hybridSearchFn(ctx, req)
	}
	return &service.HybridSearchResponse{
		Results: []*service.VectorSearchResult{
			{ID: 1, Score: 0.92, Distance: 0.08, Rank: 1},
		},
		TotalCandidates: 100,
		TotalFiltered:   1,
		Timestamp:       time.Now().Unix(),
		QueryDuration:   200,
	}, nil
}

func (m *mockVectorService) RebuildIndex(ctx context.Context, req *service.RebuildIndexRequest) (*service.RebuildIndexResponse, error) {
	if m.rebuildIndexFn != nil {
		return m.rebuildIndexFn(ctx, req)
	}
	return &service.RebuildIndexResponse{
		JobID:            "rebuild-job-123",
		Status:           "started",
		VectorsProcessed: 0,
		TotalVectors:     1000,
		PercentComplete:  0.0,
		Timestamp:        time.Now().Unix(),
		Success:          true,
	}, nil
}

func (m *mockVectorService) GetRebuildProgress(ctx context.Context, req *service.GetRebuildProgressRequest) (*service.GetRebuildProgressResponse, error) {
	if m.getRebuildProgressFn != nil {
		return m.getRebuildProgressFn(ctx, req)
	}
	return &service.GetRebuildProgressResponse{
		JobID:                    req.JobID,
		Namespace:                req.Namespace,
		Status:                   "running",
		VectorsProcessed:         500,
		TotalVectors:             1000,
		PercentComplete:          50.0,
		ElapsedTimeMs:            5000,
		EstimatedTimeRemainingMs: 5000,
		StartedAt:                time.Now().Add(-5 * time.Second).Unix(),
	}, nil
}

func (m *mockVectorService) GetIndexStats(ctx context.Context, req *service.GetIndexStatsRequest) (*service.GetIndexStatsResponse, error) {
	if m.getIndexStatsFn != nil {
		return m.getIndexStatsFn(ctx, req)
	}
	return &service.GetIndexStatsResponse{
		Namespace:        req.Namespace,
		IndexType:        "HNSW",
		VectorCount:      1000,
		Dimension:        128,
		IndexSizeBytes:   1024 * 1024,
		MemoryUsageBytes: 512 * 1024,
		IndexConfig:      map[string]string{"M": "16", "efConstruction": "200"},
		SearchPerformance: &service.IndexPerformanceMetrics{
			AverageSearchTimeMs: 1.5,
			P50SearchTimeMs:     1.0,
			P95SearchTimeMs:     3.0,
			P99SearchTimeMs:     5.0,
			TotalSearches:       10000,
			CacheHitRate:        85.0,
		},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) CompactNamespace(ctx context.Context, req *service.CompactNamespaceRequest) (*service.CompactNamespaceResponse, error) {
	if m.compactNamespaceFn != nil {
		return m.compactNamespaceFn(ctx, req)
	}
	ts := time.Now().Unix()
	return &service.CompactNamespaceResponse{
		OperationID:    "compact-op-123",
		StartedAt:      ts,
		CompletedAt:    &ts,
		BytesCompacted: 1024 * 1024,
		Success:        true,
	}, nil
}

func (m *mockVectorService) GetCompactionProgress(ctx context.Context, req *service.GetCompactionProgressRequest) (*service.GetCompactionProgressResponse, error) {
	if m.getCompactionProgressFn != nil {
		return m.getCompactionProgressFn(ctx, req)
	}
	return &service.GetCompactionProgressResponse{
		OperationID:     req.OperationID,
		Namespace:       req.Namespace,
		BytesCompacted:  512 * 1024,
		TotalBytes:      1024 * 1024,
		PercentComplete: 50.0,
		InProgress:      true,
		StartedAt:       time.Now().Add(-10 * time.Second).Unix(),
	}, nil
}

// Extended Vector Operations

func (m *mockVectorService) BatchDeleteVectors(ctx context.Context, req *service.BatchDeleteVectorsRequest) (*service.BatchDeleteVectorsResponse, error) {
	return &service.BatchDeleteVectorsResponse{
		DeletedCount: len(req.IDs),
		FailedIDs:    nil,
		Timestamp:    time.Now().Unix(),
		Success:      true,
	}, nil
}

func (m *mockVectorService) ListVectors(ctx context.Context, req *service.ListVectorsRequest) (*service.ListVectorsResponse, error) {
	return &service.ListVectorsResponse{
		Vectors:    []*service.VectorData{},
		NextCursor: "",
		TotalCount: 0,
		Timestamp:  time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) UpdateMetadata(ctx context.Context, req *service.UpdateVectorMetadataRequest) (*service.UpdateVectorMetadataResponse, error) {
	return &service.UpdateVectorMetadataResponse{
		ID:        req.ID,
		Version:   1,
		Timestamp: time.Now().Unix(),
		Success:   true,
	}, nil
}

func (m *mockVectorService) SearchByID(ctx context.Context, req *service.SearchByIDRequest) (*service.SearchByIDResponse, error) {
	return &service.SearchByIDResponse{
		SourceID:      req.ID,
		Results:       []*service.VectorSearchResult{},
		Timestamp:     time.Now().Unix(),
		QueryDuration: 100,
	}, nil
}

// =============================================================================
// Mock Service Wrapper that returns Vector Service
// =============================================================================

type mockVectorEnabledService struct {
	mockService
	vectorSvc service.VectorService
}

func (m *mockVectorEnabledService) Vector() service.VectorService {
	return m.vectorSvc
}

// =============================================================================
// Helper Functions
// =============================================================================

func createVectorTestLogger() *logger.Logger {
	cfg := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "quic-vector-test",
	}
	log, err := logger.New(cfg)
	if err != nil {
		panic(err)
	}
	return log
}

func createVectorTestHandler(vectorSvc service.VectorService) *Handler {
	svc := &mockVectorEnabledService{
		vectorSvc: vectorSvc,
	}
	return NewHandler(svc, nil, nil, nil, nil, createVectorTestLogger())
}

// =============================================================================
// handleVectorCreateNamespace Tests
// =============================================================================

func TestHandleVectorCreateNamespace_Success(t *testing.T) {
	vectorSvc := &mockVectorService{
		createNamespaceFn: func(ctx context.Context, req *service.CreateNamespaceRequest) (*service.CreateNamespaceResponse, error) {
			return &service.CreateNamespaceResponse{
				Namespace: req.Namespace,
				Dimension: req.Dimension,
				Timestamp: time.Now().Unix(),
				Success:   true,
			}, nil
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, err := EncodePayload(&service.CreateNamespaceRequest{
		Namespace:      "test-vectors",
		Dimension:      128,
		DistanceMetric: "L2",
		IndexType:      "HNSW",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpVectorCreateNamespace,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorCreateNamespace_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.CreateNamespaceRequest{
		Namespace: "test-vectors",
		Dimension: 128,
	})
	req := &Request{
		Opcode:  OpVectorCreateNamespace,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorCreateNamespace_DecodeError(t *testing.T) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	req := &Request{
		Opcode:  OpVectorCreateNamespace,
		Payload: []byte("invalid payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorCreateNamespace_ServiceError(t *testing.T) {
	vectorSvc := &mockVectorService{
		createNamespaceFn: func(ctx context.Context, req *service.CreateNamespaceRequest) (*service.CreateNamespaceResponse, error) {
			return nil, &qerrors.DragonError{
				Code: qerrors.ErrAlreadyExists,
				Op:   "vector.CreateNamespace",
				Err:  &qerrors.MessageError{Message: "namespace already exists"},
			}
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.CreateNamespaceRequest{
		Namespace: "test-vectors",
		Dimension: 128,
	})
	req := &Request{
		Opcode:  OpVectorCreateNamespace,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorDeleteNamespace Tests
// =============================================================================

func TestHandleVectorDeleteNamespace_Success(t *testing.T) {
	vectorSvc := &mockVectorService{
		deleteNamespaceFn: func(ctx context.Context, req *service.DeleteNamespaceRequest) (*service.DeleteNamespaceResponse, error) {
			return &service.DeleteNamespaceResponse{
				Success:        true,
				VectorsDeleted: 500,
				Timestamp:      time.Now().Unix(),
			}, nil
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.DeleteNamespaceRequest{
		Namespace: "test-vectors",
	})
	req := &Request{
		Opcode:  OpVectorDeleteNamespace,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorDeleteNamespace_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.DeleteNamespaceRequest{
		Namespace: "test-vectors",
	})
	req := &Request{
		Opcode:  OpVectorDeleteNamespace,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorDeleteNamespace_DecodeError(t *testing.T) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	req := &Request{
		Opcode:  OpVectorDeleteNamespace,
		Payload: []byte("invalid json payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorDeleteNamespace_ServiceError(t *testing.T) {
	vectorSvc := &mockVectorService{
		deleteNamespaceFn: func(ctx context.Context, req *service.DeleteNamespaceRequest) (*service.DeleteNamespaceResponse, error) {
			return nil, &qerrors.DragonError{
				Code: qerrors.ErrNotFound,
				Op:   "vector.DeleteNamespace",
				Err:  &qerrors.MessageError{Message: "namespace not found"},
			}
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.DeleteNamespaceRequest{
		Namespace: "nonexistent",
	})
	req := &Request{
		Opcode:  OpVectorDeleteNamespace,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorListNamespaces Tests
// =============================================================================

func TestHandleVectorListNamespaces_Success(t *testing.T) {
	vectorSvc := &mockVectorService{
		listNamespacesFn: func(ctx context.Context, req *service.ListNamespacesRequest) (*service.ListNamespacesResponse, error) {
			return &service.ListNamespacesResponse{
				Namespaces: []*service.NamespaceInfo{
					{Namespace: "ns1", Dimension: 128, VectorCount: 100},
					{Namespace: "ns2", Dimension: 256, VectorCount: 200},
				},
				TotalCount: 2,
				Timestamp:  time.Now().Unix(),
			}, nil
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.ListNamespacesRequest{
		Limit: 100,
	})
	req := &Request{
		Opcode:  OpVectorListNamespaces,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorListNamespaces_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.ListNamespacesRequest{})
	req := &Request{
		Opcode:  OpVectorListNamespaces,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorListNamespaces_DecodeError(t *testing.T) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	req := &Request{
		Opcode:  OpVectorListNamespaces,
		Payload: []byte("not valid msgpack"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorListNamespaces_ServiceError(t *testing.T) {
	vectorSvc := &mockVectorService{
		listNamespacesFn: func(ctx context.Context, req *service.ListNamespacesRequest) (*service.ListNamespacesResponse, error) {
			return nil, &qerrors.DragonError{
				Code: qerrors.ErrInternal,
				Op:   "vector.ListNamespaces",
				Err:  &qerrors.MessageError{Message: "internal error"},
			}
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.ListNamespacesRequest{})
	req := &Request{
		Opcode:  OpVectorListNamespaces,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorGetNamespaceInfo Tests
// =============================================================================

func TestHandleVectorGetNamespaceInfo_Success(t *testing.T) {
	vectorSvc := &mockVectorService{
		getNamespaceInfoFn: func(ctx context.Context, req *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error) {
			return &service.GetNamespaceInfoResponse{
				Namespace:      req.Namespace,
				Dimension:      128,
				VectorCount:    1000,
				DistanceMetric: "COSINE",
				IndexType:      "HNSW",
				CreatedAt:      time.Now().Unix(),
				UpdatedAt:      time.Now().Unix(),
			}, nil
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.GetNamespaceInfoRequest{
		Namespace: "test-ns",
	})
	req := &Request{
		Opcode:  OpVectorGetNamespaceInfo,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorGetNamespaceInfo_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.GetNamespaceInfoRequest{
		Namespace: "test-ns",
	})
	req := &Request{
		Opcode:  OpVectorGetNamespaceInfo,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorGetNamespaceInfo_DecodeError(t *testing.T) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	req := &Request{
		Opcode:  OpVectorGetNamespaceInfo,
		Payload: []byte("bad data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorGetNamespaceInfo_ServiceError(t *testing.T) {
	vectorSvc := &mockVectorService{
		getNamespaceInfoFn: func(ctx context.Context, req *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error) {
			return nil, &qerrors.DragonError{
				Code: qerrors.ErrNotFound,
				Op:   "vector.GetNamespaceInfo",
				Err:  &qerrors.MessageError{Message: "namespace not found"},
			}
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.GetNamespaceInfoRequest{
		Namespace: "nonexistent",
	})
	req := &Request{
		Opcode:  OpVectorGetNamespaceInfo,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorUpsert Tests
// =============================================================================

func TestHandleVectorUpsert_Success(t *testing.T) {
	vectorSvc := &mockVectorService{
		upsertVectorFn: func(ctx context.Context, req *service.UpsertVectorRequest) (*service.UpsertVectorResponse, error) {
			return &service.UpsertVectorResponse{
				ID:        req.ID,
				Version:   1,
				Timestamp: time.Now().Unix(),
				Success:   true,
			}, nil
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.UpsertVectorRequest{
		Namespace: "test-ns",
		ID:        12345,
		Vector:    []float32{0.1, 0.2, 0.3, 0.4},
		Metadata:  map[string]interface{}{"category": "test"},
	})
	req := &Request{
		Opcode:  OpVectorUpsert,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorUpsert_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.UpsertVectorRequest{
		Namespace: "test-ns",
		ID:        12345,
		Vector:    []float32{0.1, 0.2},
	})
	req := &Request{
		Opcode:  OpVectorUpsert,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorUpsert_DecodeError(t *testing.T) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	req := &Request{
		Opcode:  OpVectorUpsert,
		Payload: []byte("corrupt"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorUpsert_ServiceError(t *testing.T) {
	vectorSvc := &mockVectorService{
		upsertVectorFn: func(ctx context.Context, req *service.UpsertVectorRequest) (*service.UpsertVectorResponse, error) {
			return nil, &qerrors.DragonError{
				Code: qerrors.ErrInvalidInput,
				Op:   "vector.UpsertVector",
				Err:  &qerrors.MessageError{Message: "dimension mismatch"},
			}
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.UpsertVectorRequest{
		Namespace: "test-ns",
		ID:        12345,
		Vector:    []float32{0.1},
	})
	req := &Request{
		Opcode:  OpVectorUpsert,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorGet Tests
// =============================================================================

func TestHandleVectorGet_Success(t *testing.T) {
	vectorSvc := &mockVectorService{
		getVectorFn: func(ctx context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error) {
			return &service.GetVectorResponse{
				ID:        req.ID,
				Vector:    []float32{0.1, 0.2, 0.3},
				Metadata:  map[string]interface{}{"key": "value"},
				Version:   1,
				Found:     true,
				CreatedAt: time.Now().Unix(),
			}, nil
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.GetVectorRequest{
		Namespace:       "test-ns",
		ID:              12345,
		IncludeVector:   true,
		IncludeMetadata: true,
	})
	req := &Request{
		Opcode:  OpVectorGet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorGet_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.GetVectorRequest{
		Namespace: "test-ns",
		ID:        12345,
	})
	req := &Request{
		Opcode:  OpVectorGet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorGet_DecodeError(t *testing.T) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	req := &Request{
		Opcode:  OpVectorGet,
		Payload: []byte("bad payload data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorGet_ServiceError(t *testing.T) {
	vectorSvc := &mockVectorService{
		getVectorFn: func(ctx context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error) {
			return nil, &qerrors.DragonError{
				Code: qerrors.ErrNotFound,
				Op:   "vector.GetVector",
				Err:  &qerrors.MessageError{Message: "vector not found"},
			}
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.GetVectorRequest{
		Namespace: "test-ns",
		ID:        99999,
	})
	req := &Request{
		Opcode:  OpVectorGet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorDelete Tests
// =============================================================================

func TestHandleVectorDelete_Success(t *testing.T) {
	vectorSvc := &mockVectorService{
		deleteVectorFn: func(ctx context.Context, req *service.DeleteVectorRequest) (*service.DeleteVectorResponse, error) {
			return &service.DeleteVectorResponse{
				Success:   true,
				Timestamp: time.Now().Unix(),
			}, nil
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.DeleteVectorRequest{
		Namespace: "test-ns",
		ID:        12345,
	})
	req := &Request{
		Opcode:  OpVectorDelete,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorDelete_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.DeleteVectorRequest{
		Namespace: "test-ns",
		ID:        12345,
	})
	req := &Request{
		Opcode:  OpVectorDelete,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorDelete_DecodeError(t *testing.T) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	req := &Request{
		Opcode:  OpVectorDelete,
		Payload: []byte("garbage data here"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorDelete_ServiceError(t *testing.T) {
	vectorSvc := &mockVectorService{
		deleteVectorFn: func(ctx context.Context, req *service.DeleteVectorRequest) (*service.DeleteVectorResponse, error) {
			return nil, &qerrors.DragonError{
				Code: qerrors.ErrNotFound,
				Op:   "vector.DeleteVector",
				Err:  &qerrors.MessageError{Message: "vector not found"},
			}
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.DeleteVectorRequest{
		Namespace: "test-ns",
		ID:        99999,
	})
	req := &Request{
		Opcode:  OpVectorDelete,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorBatchUpsert Tests
// =============================================================================

func TestHandleVectorBatchUpsert_Success(t *testing.T) {
	vectorSvc := &mockVectorService{
		batchUpsertFn: func(ctx context.Context, req *service.BatchUpsertRequest) (*service.BatchUpsertResponse, error) {
			return &service.BatchUpsertResponse{
				IDs:           []uint64{1, 2, 3},
				Versions:      []uint64{1, 1, 1},
				Timestamp:     time.Now().Unix(),
				Success:       true,
				InsertedCount: 3,
			}, nil
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.BatchUpsertRequest{
		Namespace: "test-ns",
		Vectors: []*service.VectorData{
			{ID: 1, Vector: []float32{0.1, 0.2}},
			{ID: 2, Vector: []float32{0.3, 0.4}},
			{ID: 3, Vector: []float32{0.5, 0.6}},
		},
	})
	req := &Request{
		Opcode:  OpVectorBatchUpsert,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorBatchUpsert_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.BatchUpsertRequest{
		Namespace: "test-ns",
		Vectors:   []*service.VectorData{{ID: 1, Vector: []float32{0.1}}},
	})
	req := &Request{
		Opcode:  OpVectorBatchUpsert,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorBatchUpsert_DecodeError(t *testing.T) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	req := &Request{
		Opcode:  OpVectorBatchUpsert,
		Payload: []byte("not valid at all"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorBatchUpsert_ServiceError(t *testing.T) {
	vectorSvc := &mockVectorService{
		batchUpsertFn: func(ctx context.Context, req *service.BatchUpsertRequest) (*service.BatchUpsertResponse, error) {
			return nil, &qerrors.DragonError{
				Code: qerrors.ErrInvalidInput,
				Op:   "vector.BatchUpsert",
				Err:  &qerrors.MessageError{Message: "batch contains invalid vectors"},
			}
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.BatchUpsertRequest{
		Namespace: "test-ns",
		Vectors:   []*service.VectorData{{ID: 1}},
	})
	req := &Request{
		Opcode:  OpVectorBatchUpsert,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorSearchSimilar Tests
// =============================================================================

func TestHandleVectorSearchSimilar_Success(t *testing.T) {
	vectorSvc := &mockVectorService{
		searchSimilarFn: func(ctx context.Context, req *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
			return &service.SearchSimilarResponse{
				Results: []*service.VectorSearchResult{
					{ID: 1, Score: 0.95, Distance: 0.05, Rank: 1},
					{ID: 2, Score: 0.90, Distance: 0.10, Rank: 2},
					{ID: 3, Score: 0.85, Distance: 0.15, Rank: 3},
				},
				Timestamp:     time.Now().Unix(),
				QueryDuration: 100,
			}, nil
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.SearchSimilarRequest{
		Namespace:       "test-ns",
		QueryVector:     []float32{0.1, 0.2, 0.3},
		K:               10,
		EfSearch:        50,
		IncludeVectors:  true,
		IncludeMetadata: true,
	})
	req := &Request{
		Opcode:  OpVectorSearchSimilar,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorSearchSimilar_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.SearchSimilarRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{0.1, 0.2},
		K:           5,
	})
	req := &Request{
		Opcode:  OpVectorSearchSimilar,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorSearchSimilar_DecodeError(t *testing.T) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	req := &Request{
		Opcode:  OpVectorSearchSimilar,
		Payload: []byte("malformed request"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorSearchSimilar_ServiceError(t *testing.T) {
	vectorSvc := &mockVectorService{
		searchSimilarFn: func(ctx context.Context, req *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
			return nil, &qerrors.DragonError{
				Code: qerrors.ErrNotFound,
				Op:   "vector.SearchSimilar",
				Err:  &qerrors.MessageError{Message: "namespace not found"},
			}
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.SearchSimilarRequest{
		Namespace:   "nonexistent",
		QueryVector: []float32{0.1, 0.2},
		K:           5,
	})
	req := &Request{
		Opcode:  OpVectorSearchSimilar,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorSearchByMetadata Tests
// =============================================================================

func TestHandleVectorSearchByMetadata_Success(t *testing.T) {
	vectorSvc := &mockVectorService{
		searchByMetadataFn: func(ctx context.Context, req *service.SearchByMetadataRequest) (*service.SearchByMetadataResponse, error) {
			return &service.SearchByMetadataResponse{
				Results: []*service.VectorData{
					{ID: 1, Metadata: map[string]interface{}{"category": "A"}},
					{ID: 2, Metadata: map[string]interface{}{"category": "A"}},
				},
				TotalCount: 2,
				Timestamp:  time.Now().Unix(),
			}, nil
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.SearchByMetadataRequest{
		Namespace:       "test-ns",
		Filters:         map[string]interface{}{"category": "A"},
		FilterOperator:  "AND",
		Limit:           100,
		IncludeMetadata: true,
	})
	req := &Request{
		Opcode:  OpVectorSearchByMetadata,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorSearchByMetadata_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.SearchByMetadataRequest{
		Namespace: "test-ns",
		Filters:   map[string]interface{}{"key": "value"},
	})
	req := &Request{
		Opcode:  OpVectorSearchByMetadata,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorSearchByMetadata_DecodeError(t *testing.T) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	req := &Request{
		Opcode:  OpVectorSearchByMetadata,
		Payload: []byte("broken payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorSearchByMetadata_ServiceError(t *testing.T) {
	vectorSvc := &mockVectorService{
		searchByMetadataFn: func(ctx context.Context, req *service.SearchByMetadataRequest) (*service.SearchByMetadataResponse, error) {
			return nil, &qerrors.DragonError{
				Code: qerrors.ErrInvalidInput,
				Op:   "vector.SearchByMetadata",
				Err:  &qerrors.MessageError{Message: "invalid filter expression"},
			}
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.SearchByMetadataRequest{
		Namespace: "test-ns",
		Filters:   map[string]interface{}{"invalid": struct{}{}},
	})
	req := &Request{
		Opcode:  OpVectorSearchByMetadata,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorHybridSearch Tests
// =============================================================================

func TestHandleVectorHybridSearch_Success(t *testing.T) {
	vectorSvc := &mockVectorService{
		hybridSearchFn: func(ctx context.Context, req *service.HybridSearchRequest) (*service.HybridSearchResponse, error) {
			return &service.HybridSearchResponse{
				Results: []*service.VectorSearchResult{
					{ID: 1, Score: 0.92, Distance: 0.08, Rank: 1, Metadata: map[string]interface{}{"category": "A"}},
				},
				TotalCandidates: 100,
				TotalFiltered:   1,
				Timestamp:       time.Now().Unix(),
				QueryDuration:   150,
			}, nil
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.HybridSearchRequest{
		Namespace:          "test-ns",
		QueryVector:        []float32{0.1, 0.2, 0.3},
		K:                  10,
		MetadataFilter:     map[string]interface{}{"category": "A"},
		FilterOperator:     "AND",
		IncludeVectors:     true,
		IncludeMetadata:    true,
		RerankingAlgorithm: "linear",
	})
	req := &Request{
		Opcode:  OpVectorHybridSearch,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorHybridSearch_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.HybridSearchRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{0.1, 0.2},
		K:           5,
	})
	req := &Request{
		Opcode:  OpVectorHybridSearch,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorHybridSearch_DecodeError(t *testing.T) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	req := &Request{
		Opcode:  OpVectorHybridSearch,
		Payload: []byte("invalid hybrid search"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorHybridSearch_ServiceError(t *testing.T) {
	vectorSvc := &mockVectorService{
		hybridSearchFn: func(ctx context.Context, req *service.HybridSearchRequest) (*service.HybridSearchResponse, error) {
			return nil, &qerrors.DragonError{
				Code: qerrors.ErrNotFound,
				Op:   "vector.HybridSearch",
				Err:  &qerrors.MessageError{Message: "namespace not found"},
			}
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.HybridSearchRequest{
		Namespace:   "nonexistent",
		QueryVector: []float32{0.1, 0.2},
		K:           5,
	})
	req := &Request{
		Opcode:  OpVectorHybridSearch,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorRebuildIndex Tests
// =============================================================================

func TestHandleVectorRebuildIndex_Success(t *testing.T) {
	vectorSvc := &mockVectorService{
		rebuildIndexFn: func(ctx context.Context, req *service.RebuildIndexRequest) (*service.RebuildIndexResponse, error) {
			return &service.RebuildIndexResponse{
				JobID:            "rebuild-123",
				Status:           "started",
				VectorsProcessed: 0,
				TotalVectors:     1000,
				PercentComplete:  0.0,
				Timestamp:        time.Now().Unix(),
				Success:          true,
			}, nil
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.RebuildIndexRequest{
		Namespace:   "test-ns",
		IndexConfig: map[string]string{"M": "32"},
		Async:       true,
	})
	req := &Request{
		Opcode:  OpVectorRebuildIndex,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorRebuildIndex_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.RebuildIndexRequest{
		Namespace: "test-ns",
	})
	req := &Request{
		Opcode:  OpVectorRebuildIndex,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorRebuildIndex_DecodeError(t *testing.T) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	req := &Request{
		Opcode:  OpVectorRebuildIndex,
		Payload: []byte("bad rebuild request"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorRebuildIndex_ServiceError(t *testing.T) {
	vectorSvc := &mockVectorService{
		rebuildIndexFn: func(ctx context.Context, req *service.RebuildIndexRequest) (*service.RebuildIndexResponse, error) {
			return nil, &qerrors.DragonError{
				Code: qerrors.ErrNotFound,
				Op:   "vector.RebuildIndex",
				Err:  &qerrors.MessageError{Message: "namespace not found"},
			}
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.RebuildIndexRequest{
		Namespace: "nonexistent",
	})
	req := &Request{
		Opcode:  OpVectorRebuildIndex,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorGetRebuildProgress Tests
// =============================================================================

func TestHandleVectorGetRebuildProgress_Success(t *testing.T) {
	vectorSvc := &mockVectorService{
		getRebuildProgressFn: func(ctx context.Context, req *service.GetRebuildProgressRequest) (*service.GetRebuildProgressResponse, error) {
			return &service.GetRebuildProgressResponse{
				JobID:                    req.JobID,
				Namespace:                "test-ns",
				Status:                   "running",
				VectorsProcessed:         750,
				TotalVectors:             1000,
				PercentComplete:          75.0,
				ElapsedTimeMs:            15000,
				EstimatedTimeRemainingMs: 5000,
				StartedAt:                time.Now().Add(-15 * time.Second).Unix(),
			}, nil
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.GetRebuildProgressRequest{
		JobID:     "rebuild-123",
		Namespace: "test-ns",
	})
	req := &Request{
		Opcode:  OpVectorGetRebuildProgress,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorGetRebuildProgress_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.GetRebuildProgressRequest{
		JobID: "rebuild-123",
	})
	req := &Request{
		Opcode:  OpVectorGetRebuildProgress,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorGetRebuildProgress_DecodeError(t *testing.T) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	req := &Request{
		Opcode:  OpVectorGetRebuildProgress,
		Payload: []byte("invalid progress req"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorGetRebuildProgress_ServiceError(t *testing.T) {
	vectorSvc := &mockVectorService{
		getRebuildProgressFn: func(ctx context.Context, req *service.GetRebuildProgressRequest) (*service.GetRebuildProgressResponse, error) {
			return nil, &qerrors.DragonError{
				Code: qerrors.ErrNotFound,
				Op:   "vector.GetRebuildProgress",
				Err:  &qerrors.MessageError{Message: "no rebuild job found"},
			}
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.GetRebuildProgressRequest{
		JobID: "nonexistent",
	})
	req := &Request{
		Opcode:  OpVectorGetRebuildProgress,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorGetIndexStats Tests
// =============================================================================

func TestHandleVectorGetIndexStats_Success(t *testing.T) {
	vectorSvc := &mockVectorService{
		getIndexStatsFn: func(ctx context.Context, req *service.GetIndexStatsRequest) (*service.GetIndexStatsResponse, error) {
			return &service.GetIndexStatsResponse{
				Namespace:        req.Namespace,
				IndexType:        "HNSW",
				VectorCount:      10000,
				Dimension:        128,
				IndexSizeBytes:   10 * 1024 * 1024,
				MemoryUsageBytes: 5 * 1024 * 1024,
				IndexConfig:      map[string]string{"M": "16", "efConstruction": "200"},
				SearchPerformance: &service.IndexPerformanceMetrics{
					AverageSearchTimeMs: 2.5,
					P50SearchTimeMs:     2.0,
					P95SearchTimeMs:     5.0,
					P99SearchTimeMs:     10.0,
					TotalSearches:       100000,
					CacheHitRate:        90.0,
				},
				Timestamp: time.Now().Unix(),
			}, nil
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.GetIndexStatsRequest{
		Namespace: "test-ns",
	})
	req := &Request{
		Opcode:  OpVectorGetIndexStats,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorGetIndexStats_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.GetIndexStatsRequest{
		Namespace: "test-ns",
	})
	req := &Request{
		Opcode:  OpVectorGetIndexStats,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorGetIndexStats_DecodeError(t *testing.T) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	req := &Request{
		Opcode:  OpVectorGetIndexStats,
		Payload: []byte("broken stats request"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorGetIndexStats_ServiceError(t *testing.T) {
	vectorSvc := &mockVectorService{
		getIndexStatsFn: func(ctx context.Context, req *service.GetIndexStatsRequest) (*service.GetIndexStatsResponse, error) {
			return nil, &qerrors.DragonError{
				Code: qerrors.ErrNotFound,
				Op:   "vector.GetIndexStats",
				Err:  &qerrors.MessageError{Message: "namespace not found"},
			}
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.GetIndexStatsRequest{
		Namespace: "nonexistent",
	})
	req := &Request{
		Opcode:  OpVectorGetIndexStats,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorCompact Tests
// =============================================================================

func TestHandleVectorCompact_Success(t *testing.T) {
	vectorSvc := &mockVectorService{
		compactNamespaceFn: func(ctx context.Context, req *service.CompactNamespaceRequest) (*service.CompactNamespaceResponse, error) {
			ts := time.Now().Unix()
			return &service.CompactNamespaceResponse{
				OperationID:    "compact-456",
				StartedAt:      ts,
				CompletedAt:    &ts,
				BytesCompacted: 5 * 1024 * 1024,
				Success:        true,
			}, nil
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.CompactNamespaceRequest{
		Namespace: "test-ns",
		Async:     false,
		TimeoutMs: 30000,
	})
	req := &Request{
		Opcode:  OpVectorCompact,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorCompact_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.CompactNamespaceRequest{
		Namespace: "test-ns",
	})
	req := &Request{
		Opcode:  OpVectorCompact,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorCompact_DecodeError(t *testing.T) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	req := &Request{
		Opcode:  OpVectorCompact,
		Payload: []byte("invalid compact req"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorCompact_ServiceError(t *testing.T) {
	vectorSvc := &mockVectorService{
		compactNamespaceFn: func(ctx context.Context, req *service.CompactNamespaceRequest) (*service.CompactNamespaceResponse, error) {
			return nil, &qerrors.DragonError{
				Code: qerrors.ErrNotFound,
				Op:   "vector.CompactNamespace",
				Err:  &qerrors.MessageError{Message: "namespace not found"},
			}
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.CompactNamespaceRequest{
		Namespace: "nonexistent",
	})
	req := &Request{
		Opcode:  OpVectorCompact,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorGetCompactionProgress Tests
// =============================================================================

func TestHandleVectorGetCompactionProgress_Success(t *testing.T) {
	vectorSvc := &mockVectorService{
		getCompactionProgressFn: func(ctx context.Context, req *service.GetCompactionProgressRequest) (*service.GetCompactionProgressResponse, error) {
			return &service.GetCompactionProgressResponse{
				OperationID:     req.OperationID,
				Namespace:       req.Namespace,
				BytesCompacted:  3 * 1024 * 1024,
				TotalBytes:      5 * 1024 * 1024,
				PercentComplete: 60.0,
				InProgress:      true,
				StartedAt:       time.Now().Add(-30 * time.Second).Unix(),
			}, nil
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.GetCompactionProgressRequest{
		Namespace:   "test-ns",
		OperationID: "compact-456",
	})
	req := &Request{
		Opcode:  OpVectorGetCompactionProgress,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorGetCompactionProgress_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.GetCompactionProgressRequest{
		Namespace: "test-ns",
	})
	req := &Request{
		Opcode:  OpVectorGetCompactionProgress,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorGetCompactionProgress_DecodeError(t *testing.T) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	req := &Request{
		Opcode:  OpVectorGetCompactionProgress,
		Payload: []byte("garbage progress data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorGetCompactionProgress_ServiceError(t *testing.T) {
	vectorSvc := &mockVectorService{
		getCompactionProgressFn: func(ctx context.Context, req *service.GetCompactionProgressRequest) (*service.GetCompactionProgressResponse, error) {
			return nil, &qerrors.DragonError{
				Code: qerrors.ErrNotFound,
				Op:   "vector.GetCompactionProgress",
				Err:  &qerrors.MessageError{Message: "no compaction in progress"},
			}
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.GetCompactionProgressRequest{
		Namespace:   "test-ns",
		OperationID: "nonexistent",
	})
	req := &Request{
		Opcode:  OpVectorGetCompactionProgress,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Table-Driven Tests for All Vector Operations
// =============================================================================

func TestHandleVectorOperations_AllOpcodes(t *testing.T) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	tests := []struct {
		name    string
		opcode  byte
		payload interface{}
	}{
		// Namespace Operations
		{"VectorCreateNamespace", OpVectorCreateNamespace, &service.CreateNamespaceRequest{Namespace: "ns1", Dimension: 128}},
		{"VectorDeleteNamespace", OpVectorDeleteNamespace, &service.DeleteNamespaceRequest{Namespace: "ns1"}},
		{"VectorListNamespaces", OpVectorListNamespaces, &service.ListNamespacesRequest{Limit: 100}},
		{"VectorGetNamespaceInfo", OpVectorGetNamespaceInfo, &service.GetNamespaceInfoRequest{Namespace: "ns1"}},

		// Vector CRUD Operations
		{"VectorUpsert", OpVectorUpsert, &service.UpsertVectorRequest{Namespace: "ns1", ID: 1, Vector: []float32{0.1}}},
		{"VectorGet", OpVectorGet, &service.GetVectorRequest{Namespace: "ns1", ID: 1}},
		{"VectorDelete", OpVectorDelete, &service.DeleteVectorRequest{Namespace: "ns1", ID: 1}},
		{"VectorBatchUpsert", OpVectorBatchUpsert, &service.BatchUpsertRequest{Namespace: "ns1", Vectors: []*service.VectorData{}}},

		// Search Operations
		{"VectorSearchSimilar", OpVectorSearchSimilar, &service.SearchSimilarRequest{Namespace: "ns1", QueryVector: []float32{0.1}, K: 10}},
		{"VectorSearchByMetadata", OpVectorSearchByMetadata, &service.SearchByMetadataRequest{Namespace: "ns1", Filters: map[string]interface{}{}}},
		{"VectorHybridSearch", OpVectorHybridSearch, &service.HybridSearchRequest{Namespace: "ns1", QueryVector: []float32{0.1}, K: 10}},

		// Index Management Operations
		{"VectorRebuildIndex", OpVectorRebuildIndex, &service.RebuildIndexRequest{Namespace: "ns1"}},
		{"VectorGetRebuildProgress", OpVectorGetRebuildProgress, &service.GetRebuildProgressRequest{Namespace: "ns1"}},
		{"VectorGetIndexStats", OpVectorGetIndexStats, &service.GetIndexStatsRequest{Namespace: "ns1"}},

		// Compaction Operations
		{"VectorCompact", OpVectorCompact, &service.CompactNamespaceRequest{Namespace: "ns1"}},
		{"VectorGetCompactionProgress", OpVectorGetCompactionProgress, &service.GetCompactionProgressRequest{Namespace: "ns1"}},
	}

	// Verify we have all 16 operations
	if len(tests) != 16 {
		t.Errorf("expected 16 test cases, got %d", len(tests))
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := EncodePayload(tt.payload)
			if err != nil {
				t.Fatalf("failed to encode payload: %v", err)
			}

			req := &Request{
				Opcode:  tt.opcode,
				Payload: payload,
			}

			resp := handler.HandleRequest(context.Background(), req)

			if resp.Status != StatusOK {
				t.Errorf("expected StatusOK for %s, got %d", tt.name, resp.Status)
			}
		})
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkHandler_HandleVectorSearchSimilar(b *testing.B) {
	vectorSvc := &mockVectorService{
		searchSimilarFn: func(ctx context.Context, req *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
			return &service.SearchSimilarResponse{
				Results: []*service.VectorSearchResult{
					{ID: 1, Score: 0.95},
				},
				Timestamp:     time.Now().Unix(),
				QueryDuration: 100,
			}, nil
		},
	}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.SearchSimilarRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{0.1, 0.2, 0.3, 0.4, 0.5},
		K:           10,
	})
	req := &Request{
		Opcode:  OpVectorSearchSimilar,
		Payload: payload,
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.HandleRequest(ctx, req)
	}
}

func BenchmarkHandler_HandleVectorUpsert(b *testing.B) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.UpsertVectorRequest{
		Namespace: "test-ns",
		ID:        12345,
		Vector:    []float32{0.1, 0.2, 0.3, 0.4, 0.5},
	})
	req := &Request{
		Opcode:  OpVectorUpsert,
		Payload: payload,
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.HandleRequest(ctx, req)
	}
}

func BenchmarkHandler_HandleVectorGet(b *testing.B) {
	vectorSvc := &mockVectorService{}
	handler := createVectorTestHandler(vectorSvc)

	payload, _ := EncodePayload(&service.GetVectorRequest{
		Namespace:     "test-ns",
		ID:            12345,
		IncludeVector: true,
	})
	req := &Request{
		Opcode:  OpVectorGet,
		Payload: payload,
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.HandleRequest(ctx, req)
	}
}

// --- Content moved from handler_vector_extended_test.go ---

// =============================================================================
// Extended Mock Vector Service with additional methods
// =============================================================================

type extendedMockVectorService struct {
	mockVectorService
	listVectorsFn    func(ctx context.Context, req *service.ListVectorsRequest) (*service.ListVectorsResponse, error)
	updateMetadataFn func(ctx context.Context, req *service.UpdateVectorMetadataRequest) (*service.UpdateVectorMetadataResponse, error)
	searchByIDFn     func(ctx context.Context, req *service.SearchByIDRequest) (*service.SearchByIDResponse, error)
	batchDeleteFn    func(ctx context.Context, req *service.BatchDeleteVectorsRequest) (*service.BatchDeleteVectorsResponse, error)
}

func (m *extendedMockVectorService) ListVectors(ctx context.Context, req *service.ListVectorsRequest) (*service.ListVectorsResponse, error) {
	if m.listVectorsFn != nil {
		return m.listVectorsFn(ctx, req)
	}
	return &service.ListVectorsResponse{
		Vectors:    []*service.VectorData{},
		NextCursor: "",
		TotalCount: 0,
		Timestamp:  time.Now().Unix(),
	}, nil
}

func (m *extendedMockVectorService) UpdateMetadata(ctx context.Context, req *service.UpdateVectorMetadataRequest) (*service.UpdateVectorMetadataResponse, error) {
	if m.updateMetadataFn != nil {
		return m.updateMetadataFn(ctx, req)
	}
	return &service.UpdateVectorMetadataResponse{
		ID:        req.ID,
		Version:   1,
		Timestamp: time.Now().Unix(),
		Success:   true,
	}, nil
}

func (m *extendedMockVectorService) SearchByID(ctx context.Context, req *service.SearchByIDRequest) (*service.SearchByIDResponse, error) {
	if m.searchByIDFn != nil {
		return m.searchByIDFn(ctx, req)
	}
	return &service.SearchByIDResponse{
		SourceID:      req.ID,
		Results:       []*service.VectorSearchResult{},
		Timestamp:     time.Now().Unix(),
		QueryDuration: 100,
	}, nil
}

func (m *extendedMockVectorService) BatchDeleteVectors(ctx context.Context, req *service.BatchDeleteVectorsRequest) (*service.BatchDeleteVectorsResponse, error) {
	if m.batchDeleteFn != nil {
		return m.batchDeleteFn(ctx, req)
	}
	return &service.BatchDeleteVectorsResponse{
		DeletedCount: len(req.IDs),
		FailedIDs:    nil,
		Timestamp:    time.Now().Unix(),
		Success:      true,
	}, nil
}

// extendedMockVectorEnabledService wraps mockService to provide extended VectorService.
type extendedMockVectorEnabledService struct {
	mockService
	vectorSvc *extendedMockVectorService
}

func (m *extendedMockVectorEnabledService) Vector() service.VectorService {
	return m.vectorSvc
}

func createExtendedVectorHandler(vectorSvc *extendedMockVectorService) *Handler {
	svc := &extendedMockVectorEnabledService{
		vectorSvc: vectorSvc,
	}
	return NewHandler(svc, nil, nil, nil, nil, createVectorTestLogger())
}

// =============================================================================
// handleVectorListVectors Tests
// =============================================================================

func TestHandleVectorListVectors_Success(t *testing.T) {
	vectorSvc := &extendedMockVectorService{
		listVectorsFn: func(ctx context.Context, req *service.ListVectorsRequest) (*service.ListVectorsResponse, error) {
			return &service.ListVectorsResponse{
				Vectors: []*service.VectorData{
					{ID: 1, Vector: []float32{0.1, 0.2}, Metadata: map[string]interface{}{"key": "value"}},
					{ID: 2, Vector: []float32{0.3, 0.4}, Metadata: map[string]interface{}{"key": "value2"}},
				},
				NextCursor: "cursor-123",
				TotalCount: 2,
				Timestamp:  time.Now().Unix(),
			}, nil
		},
	}
	handler := createExtendedVectorHandler(vectorSvc)

	payload, err := EncodePayload(&service.ListVectorsRequest{
		Namespace: "test-ns",
		Cursor:    "",
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	// Use OpScheduler (0x00) as main opcode with SubOpVectorListVectors (0x41) sub-operation
	subPayload := append([]byte{SubOpVectorListVectors}, payload...)
	req := &Request{
		Opcode:  OpScheduler,
		Payload: subPayload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorListVectors_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.ListVectorsRequest{})
	subPayload := append([]byte{SubOpVectorListVectors}, payload...)
	req := &Request{
		Opcode:  OpScheduler,
		Payload: subPayload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorListVectors_DecodeError(t *testing.T) {
	vectorSvc := &extendedMockVectorService{}
	handler := createExtendedVectorHandler(vectorSvc)

	// Invalid payload after sub-operation byte
	subPayload := append([]byte{SubOpVectorListVectors}, []byte("invalid json")...)
	req := &Request{
		Opcode:  OpScheduler,
		Payload: subPayload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorListVectors_ServiceError(t *testing.T) {
	vectorSvc := &extendedMockVectorService{
		listVectorsFn: func(ctx context.Context, req *service.ListVectorsRequest) (*service.ListVectorsResponse, error) {
			return nil, stderrors.New("list vectors failed")
		},
	}
	handler := createExtendedVectorHandler(vectorSvc)

	payload, _ := EncodePayload(&service.ListVectorsRequest{Namespace: "test-ns"})
	subPayload := append([]byte{SubOpVectorListVectors}, payload...)
	req := &Request{
		Opcode:  OpScheduler,
		Payload: subPayload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorUpdateMetadata Tests
// =============================================================================

func TestHandleVectorUpdateMetadata_Success(t *testing.T) {
	vectorSvc := &extendedMockVectorService{
		updateMetadataFn: func(ctx context.Context, req *service.UpdateVectorMetadataRequest) (*service.UpdateVectorMetadataResponse, error) {
			return &service.UpdateVectorMetadataResponse{
				ID:        req.ID,
				Version:   2,
				Timestamp: time.Now().Unix(),
				Success:   true,
			}, nil
		},
	}
	handler := createExtendedVectorHandler(vectorSvc)

	payload, err := EncodePayload(&service.UpdateVectorMetadataRequest{
		Namespace: "test-ns",
		ID:        12345,
		Metadata:  map[string]interface{}{"new_key": "new_value"},
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	subPayload := append([]byte{SubOpVectorUpdateMetadata}, payload...)
	req := &Request{
		Opcode:  OpScheduler,
		Payload: subPayload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorUpdateMetadata_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.UpdateVectorMetadataRequest{})
	subPayload := append([]byte{SubOpVectorUpdateMetadata}, payload...)
	req := &Request{
		Opcode:  OpScheduler,
		Payload: subPayload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorUpdateMetadata_DecodeError(t *testing.T) {
	vectorSvc := &extendedMockVectorService{}
	handler := createExtendedVectorHandler(vectorSvc)

	subPayload := append([]byte{SubOpVectorUpdateMetadata}, []byte("invalid json")...)
	req := &Request{
		Opcode:  OpScheduler,
		Payload: subPayload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorUpdateMetadata_ServiceError(t *testing.T) {
	vectorSvc := &extendedMockVectorService{
		updateMetadataFn: func(ctx context.Context, req *service.UpdateVectorMetadataRequest) (*service.UpdateVectorMetadataResponse, error) {
			return nil, stderrors.New("update metadata failed")
		},
	}
	handler := createExtendedVectorHandler(vectorSvc)

	payload, _ := EncodePayload(&service.UpdateVectorMetadataRequest{Namespace: "test-ns", ID: 1})
	subPayload := append([]byte{SubOpVectorUpdateMetadata}, payload...)
	req := &Request{
		Opcode:  OpScheduler,
		Payload: subPayload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorSearchByID Tests
// =============================================================================

func TestHandleVectorSearchByID_Success(t *testing.T) {
	vectorSvc := &extendedMockVectorService{
		searchByIDFn: func(ctx context.Context, req *service.SearchByIDRequest) (*service.SearchByIDResponse, error) {
			return &service.SearchByIDResponse{
				SourceID: req.ID,
				Results: []*service.VectorSearchResult{
					{ID: 2, Score: 0.95, Distance: 0.05, Rank: 1},
					{ID: 3, Score: 0.90, Distance: 0.10, Rank: 2},
				},
				Timestamp:     time.Now().Unix(),
				QueryDuration: 150,
			}, nil
		},
	}
	handler := createExtendedVectorHandler(vectorSvc)

	payload, err := EncodePayload(&service.SearchByIDRequest{
		Namespace: "test-ns",
		ID:        12345,
		K:         10,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpVectorSearchByID,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorSearchByID_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.SearchByIDRequest{})
	req := &Request{
		Opcode:  OpVectorSearchByID,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorSearchByID_DecodeError(t *testing.T) {
	vectorSvc := &extendedMockVectorService{}
	handler := createExtendedVectorHandler(vectorSvc)

	req := &Request{
		Opcode:  OpVectorSearchByID,
		Payload: []byte("invalid json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorSearchByID_ServiceError(t *testing.T) {
	vectorSvc := &extendedMockVectorService{
		searchByIDFn: func(ctx context.Context, req *service.SearchByIDRequest) (*service.SearchByIDResponse, error) {
			return nil, stderrors.New("search by ID failed")
		},
	}
	handler := createExtendedVectorHandler(vectorSvc)

	payload, _ := EncodePayload(&service.SearchByIDRequest{Namespace: "test-ns", ID: 1})
	req := &Request{
		Opcode:  OpVectorSearchByID,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorBatchDelete Tests
// =============================================================================

func TestHandleVectorBatchDelete_Success(t *testing.T) {
	vectorSvc := &extendedMockVectorService{
		batchDeleteFn: func(ctx context.Context, req *service.BatchDeleteVectorsRequest) (*service.BatchDeleteVectorsResponse, error) {
			return &service.BatchDeleteVectorsResponse{
				DeletedCount: len(req.IDs),
				FailedIDs:    nil,
				Timestamp:    time.Now().Unix(),
				Success:      true,
			}, nil
		},
	}
	handler := createExtendedVectorHandler(vectorSvc)

	payload, err := EncodePayload(&service.BatchDeleteVectorsRequest{
		Namespace: "test-ns",
		IDs:       []uint64{1, 2, 3, 4, 5},
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	subPayload := append([]byte{SubOpVectorBatchDelete}, payload...)
	req := &Request{
		Opcode:  OpScheduler,
		Payload: subPayload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVectorBatchDelete_NilService(t *testing.T) {
	handler := createVectorTestHandler(nil)

	payload, _ := EncodePayload(&service.BatchDeleteVectorsRequest{})
	subPayload := append([]byte{SubOpVectorBatchDelete}, payload...)
	req := &Request{
		Opcode:  OpScheduler,
		Payload: subPayload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil vector service")
	}
}

func TestHandleVectorBatchDelete_DecodeError(t *testing.T) {
	vectorSvc := &extendedMockVectorService{}
	handler := createExtendedVectorHandler(vectorSvc)

	subPayload := append([]byte{SubOpVectorBatchDelete}, []byte("invalid json")...)
	req := &Request{
		Opcode:  OpScheduler,
		Payload: subPayload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleVectorBatchDelete_ServiceError(t *testing.T) {
	vectorSvc := &extendedMockVectorService{
		batchDeleteFn: func(ctx context.Context, req *service.BatchDeleteVectorsRequest) (*service.BatchDeleteVectorsResponse, error) {
			return nil, stderrors.New("batch delete failed")
		},
	}
	handler := createExtendedVectorHandler(vectorSvc)

	payload, _ := EncodePayload(&service.BatchDeleteVectorsRequest{Namespace: "test-ns", IDs: []uint64{1}})
	subPayload := append([]byte{SubOpVectorBatchDelete}, payload...)
	req := &Request{
		Opcode:  OpScheduler,
		Payload: subPayload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleVectorExtendedDispatch Tests - Unknown Sub-Op
// =============================================================================

func TestHandleVectorExtendedDispatch_UnknownSubOp(t *testing.T) {
	vectorSvc := &extendedMockVectorService{}
	handler := createExtendedVectorHandler(vectorSvc)

	// Unknown sub-operation code in vector extended range (0x4F)
	subPayload := append([]byte{0x4F}, []byte("{}")...)
	req := &Request{
		Opcode:  OpScheduler,
		Payload: subPayload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with unknown sub-operation")
	}
}

func TestHandleVectorExtendedDispatch_EmptyPayload(t *testing.T) {
	vectorSvc := &extendedMockVectorService{}
	handler := createExtendedVectorHandler(vectorSvc)

	req := &Request{
		Opcode:  OpScheduler,
		Payload: []byte{},
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with empty payload")
	}
}
