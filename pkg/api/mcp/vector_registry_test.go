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

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock Vector Service
// =============================================================================

type mockVectorService struct {
	createNamespaceFunc       func(ctx context.Context, req *service.CreateNamespaceRequest) (*service.CreateNamespaceResponse, error)
	deleteNamespaceFunc       func(ctx context.Context, req *service.DeleteNamespaceRequest) (*service.DeleteNamespaceResponse, error)
	listNamespacesFunc        func(ctx context.Context, req *service.ListNamespacesRequest) (*service.ListNamespacesResponse, error)
	getNamespaceInfoFunc      func(ctx context.Context, req *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error)
	upsertVectorFunc          func(ctx context.Context, req *service.UpsertVectorRequest) (*service.UpsertVectorResponse, error)
	getVectorFunc             func(ctx context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error)
	deleteVectorFunc          func(ctx context.Context, req *service.DeleteVectorRequest) (*service.DeleteVectorResponse, error)
	batchUpsertFunc           func(ctx context.Context, req *service.BatchUpsertRequest) (*service.BatchUpsertResponse, error)
	batchDeleteFunc           func(ctx context.Context, req *service.BatchDeleteVectorsRequest) (*service.BatchDeleteVectorsResponse, error)
	listVectorsFunc           func(ctx context.Context, req *service.ListVectorsRequest) (*service.ListVectorsResponse, error)
	updateMetadataFunc        func(ctx context.Context, req *service.UpdateVectorMetadataRequest) (*service.UpdateVectorMetadataResponse, error)
	searchSimilarFunc         func(ctx context.Context, req *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error)
	searchByMetadataFunc      func(ctx context.Context, req *service.SearchByMetadataRequest) (*service.SearchByMetadataResponse, error)
	hybridSearchFunc          func(ctx context.Context, req *service.HybridSearchRequest) (*service.HybridSearchResponse, error)
	searchByIDFunc            func(ctx context.Context, req *service.SearchByIDRequest) (*service.SearchByIDResponse, error)
	rebuildIndexFunc          func(ctx context.Context, req *service.RebuildIndexRequest) (*service.RebuildIndexResponse, error)
	getRebuildProgressFunc    func(ctx context.Context, req *service.GetRebuildProgressRequest) (*service.GetRebuildProgressResponse, error)
	getIndexStatsFunc         func(ctx context.Context, req *service.GetIndexStatsRequest) (*service.GetIndexStatsResponse, error)
	compactNamespaceFunc      func(ctx context.Context, req *service.CompactNamespaceRequest) (*service.CompactNamespaceResponse, error)
	getCompactionProgressFunc func(ctx context.Context, req *service.GetCompactionProgressRequest) (*service.GetCompactionProgressResponse, error)
}

func (m *mockVectorService) CreateNamespace(ctx context.Context, req *service.CreateNamespaceRequest) (*service.CreateNamespaceResponse, error) {
	if m.createNamespaceFunc != nil {
		return m.createNamespaceFunc(ctx, req)
	}
	return &service.CreateNamespaceResponse{
		Namespace: req.Namespace,
		Dimension: req.Dimension,
		Timestamp: time.Now().Unix(),
		Success:   true,
	}, nil
}

func (m *mockVectorService) DeleteNamespace(ctx context.Context, req *service.DeleteNamespaceRequest) (*service.DeleteNamespaceResponse, error) {
	if m.deleteNamespaceFunc != nil {
		return m.deleteNamespaceFunc(ctx, req)
	}
	return &service.DeleteNamespaceResponse{
		Success:        true,
		VectorsDeleted: 100,
		Timestamp:      time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) ListNamespaces(ctx context.Context, req *service.ListNamespacesRequest) (*service.ListNamespacesResponse, error) {
	if m.listNamespacesFunc != nil {
		return m.listNamespacesFunc(ctx, req)
	}
	return &service.ListNamespacesResponse{
		Namespaces: []*service.NamespaceInfo{
			{Namespace: "test-ns", Dimension: 128, VectorCount: 1000, DistanceMetric: "L2", IndexType: "HNSW"},
		},
		TotalCount: 1,
		Timestamp:  time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) GetNamespaceInfo(ctx context.Context, req *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error) {
	if m.getNamespaceInfoFunc != nil {
		return m.getNamespaceInfoFunc(ctx, req)
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
	if m.upsertVectorFunc != nil {
		return m.upsertVectorFunc(ctx, req)
	}
	return &service.UpsertVectorResponse{
		ID:        req.ID,
		Version:   1,
		Timestamp: time.Now().Unix(),
		Success:   true,
	}, nil
}

func (m *mockVectorService) GetVector(ctx context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error) {
	if m.getVectorFunc != nil {
		return m.getVectorFunc(ctx, req)
	}
	return &service.GetVectorResponse{
		ID:       req.ID,
		Vector:   []float32{0.1, 0.2, 0.3},
		Metadata: map[string]interface{}{"_original_id": "test-id", "type": "test"},
		Version:  1,
		Found:    true,
	}, nil
}

func (m *mockVectorService) DeleteVector(ctx context.Context, req *service.DeleteVectorRequest) (*service.DeleteVectorResponse, error) {
	if m.deleteVectorFunc != nil {
		return m.deleteVectorFunc(ctx, req)
	}
	return &service.DeleteVectorResponse{
		Success:   true,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) BatchUpsert(ctx context.Context, req *service.BatchUpsertRequest) (*service.BatchUpsertResponse, error) {
	if m.batchUpsertFunc != nil {
		return m.batchUpsertFunc(ctx, req)
	}
	return &service.BatchUpsertResponse{
		InsertedCount: len(req.Vectors),
		Timestamp:     time.Now().Unix(),
		Success:       true,
	}, nil
}

func (m *mockVectorService) BatchDeleteVectors(ctx context.Context, req *service.BatchDeleteVectorsRequest) (*service.BatchDeleteVectorsResponse, error) {
	if m.batchDeleteFunc != nil {
		return m.batchDeleteFunc(ctx, req)
	}
	return &service.BatchDeleteVectorsResponse{
		DeletedCount: len(req.IDs),
		Timestamp:    time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) ListVectors(ctx context.Context, req *service.ListVectorsRequest) (*service.ListVectorsResponse, error) {
	if m.listVectorsFunc != nil {
		return m.listVectorsFunc(ctx, req)
	}
	return &service.ListVectorsResponse{
		Vectors:    []*service.VectorData{{ID: 1, Vector: []float32{0.1, 0.2, 0.3}}},
		TotalCount: 1,
		Timestamp:  time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) UpdateMetadata(ctx context.Context, req *service.UpdateVectorMetadataRequest) (*service.UpdateVectorMetadataResponse, error) {
	if m.updateMetadataFunc != nil {
		return m.updateMetadataFunc(ctx, req)
	}
	return &service.UpdateVectorMetadataResponse{
		ID:        req.ID,
		Version:   2,
		Timestamp: time.Now().Unix(),
		Success:   true,
	}, nil
}

func (m *mockVectorService) SearchSimilar(ctx context.Context, req *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
	if m.searchSimilarFunc != nil {
		return m.searchSimilarFunc(ctx, req)
	}
	results := make([]*service.VectorSearchResult, req.K)
	for i := 0; i < req.K; i++ {
		results[i] = &service.VectorSearchResult{
			ID:       uint64(i + 1),
			Score:    0.9 - float32(i)*0.1,
			Rank:     i + 1,
			Vector:   req.QueryVector,
			Metadata: map[string]interface{}{"_original_id": fmt.Sprintf("result-%d", i+1)},
		}
	}
	return &service.SearchSimilarResponse{
		Results:       results,
		QueryDuration: 10,
		Timestamp:     time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) SearchByMetadata(ctx context.Context, req *service.SearchByMetadataRequest) (*service.SearchByMetadataResponse, error) {
	if m.searchByMetadataFunc != nil {
		return m.searchByMetadataFunc(ctx, req)
	}
	return &service.SearchByMetadataResponse{
		Results:   []*service.VectorData{{ID: 1, Vector: []float32{0.1, 0.2, 0.3}}},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) HybridSearch(ctx context.Context, req *service.HybridSearchRequest) (*service.HybridSearchResponse, error) {
	if m.hybridSearchFunc != nil {
		return m.hybridSearchFunc(ctx, req)
	}
	return &service.HybridSearchResponse{
		Results:   []*service.VectorSearchResult{{ID: 1, Score: 0.9, Rank: 1}},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) SearchByID(ctx context.Context, req *service.SearchByIDRequest) (*service.SearchByIDResponse, error) {
	if m.searchByIDFunc != nil {
		return m.searchByIDFunc(ctx, req)
	}
	return &service.SearchByIDResponse{
		Results:   []*service.VectorSearchResult{{ID: 2, Score: 0.85, Rank: 1}},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) RebuildIndex(ctx context.Context, req *service.RebuildIndexRequest) (*service.RebuildIndexResponse, error) {
	if m.rebuildIndexFunc != nil {
		return m.rebuildIndexFunc(ctx, req)
	}
	return &service.RebuildIndexResponse{
		JobID:           "job-123",
		Status:          "started",
		PercentComplete: 0,
		Timestamp:       time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) GetRebuildProgress(ctx context.Context, req *service.GetRebuildProgressRequest) (*service.GetRebuildProgressResponse, error) {
	if m.getRebuildProgressFunc != nil {
		return m.getRebuildProgressFunc(ctx, req)
	}
	return &service.GetRebuildProgressResponse{
		Namespace:                req.Namespace,
		JobID:                    req.JobID,
		Status:                   "in_progress",
		VectorsProcessed:         500,
		TotalVectors:             1000,
		PercentComplete:          50.0,
		ElapsedTimeMs:            5000,
		EstimatedTimeRemainingMs: 5000,
		StartedAt:                time.Now().Add(-5 * time.Second).Unix(),
	}, nil
}

func (m *mockVectorService) GetIndexStats(ctx context.Context, req *service.GetIndexStatsRequest) (*service.GetIndexStatsResponse, error) {
	if m.getIndexStatsFunc != nil {
		return m.getIndexStatsFunc(ctx, req)
	}
	return &service.GetIndexStatsResponse{
		Namespace:        req.Namespace,
		VectorCount:      1000,
		Dimension:        128,
		IndexType:        "HNSW",
		IndexSizeBytes:   1024 * 1024,
		MemoryUsageBytes: 512 * 1024,
		Timestamp:        time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) CompactNamespace(ctx context.Context, req *service.CompactNamespaceRequest) (*service.CompactNamespaceResponse, error) {
	if m.compactNamespaceFunc != nil {
		return m.compactNamespaceFunc(ctx, req)
	}
	return &service.CompactNamespaceResponse{
		OperationID: "compact-123",
		StartedAt:   time.Now().Unix(),
	}, nil
}

func (m *mockVectorService) GetCompactionProgress(ctx context.Context, req *service.GetCompactionProgressRequest) (*service.GetCompactionProgressResponse, error) {
	if m.getCompactionProgressFunc != nil {
		return m.getCompactionProgressFunc(ctx, req)
	}
	return &service.GetCompactionProgressResponse{
		Namespace:       req.Namespace,
		OperationID:     req.OperationID,
		BytesCompacted:  500 * 1024,
		TotalBytes:      1024 * 1024,
		PercentComplete: 50.0,
		InProgress:      true,
		StartedAt:       time.Now().Add(-10 * time.Second).Unix(),
	}, nil
}

// =============================================================================
// Extended Mock Service with Vector Support
// =============================================================================

type vectorMockService struct {
	mockService
	vectorSvc *mockVectorService
}

func (m *vectorMockService) Vector() service.VectorService {
	return m.vectorSvc
}

// =============================================================================
// Test Helpers
// =============================================================================

func newVectorRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	svc := &vectorMockService{
		vectorSvc: &mockVectorService{},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(svc, log)
}

func newVectorRegistryWithMock(t *testing.T, vectorSvc *mockVectorService) *ToolRegistry {
	t.Helper()
	svc := &vectorMockService{
		vectorSvc: vectorSvc,
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(svc, log)
}

// =============================================================================
// Vector Namespace Tests
// =============================================================================

func TestVectorCreateNamespace_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "minimal",
			args: map[string]interface{}{
				"namespace":  "test-ns",
				"dimensions": float64(128),
			},
		},
		{
			name: "with name alias",
			args: map[string]interface{}{
				"name":       "test-ns-2",
				"dimensions": float64(256),
			},
		},
		{
			name: "with all options",
			args: map[string]interface{}{
				"namespace":       "full-ns",
				"dimensions":      float64(384),
				"distance_metric": "COSINE",
				"index_type":      "FLAT",
			},
		},
		{
			name: "with metric alias",
			args: map[string]interface{}{
				"namespace":  "metric-ns",
				"dimensions": float64(128),
				"metric":     "L2",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_create_namespace", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "namespace")
			assert.Contains(t, resultMap, "success")
			assert.Equal(t, true, resultMap["success"])
		})
	}
}

func TestVectorCreateNamespace_Errors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{
			name:        "missing namespace",
			args:        map[string]interface{}{"dimensions": float64(128)},
			errContains: "namespace",
		},
		{
			name:        "missing dimensions",
			args:        map[string]interface{}{"namespace": "test"},
			errContains: "dimensions",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry := newVectorRegistry(t)
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_create_namespace", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestVectorDeleteNamespace_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "with namespace",
			args: map[string]interface{}{"namespace": "test-ns"},
		},
		{
			name: "with name alias",
			args: map[string]interface{}{"name": "test-ns-2"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_delete_namespace", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "success")
		})
	}
}

func TestVectorDeleteNamespace_MissingNamespace(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_delete_namespace", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "namespace")
}

func TestVectorListNamespaces_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "no filters",
			args: map[string]interface{}{},
		},
		{
			name: "with prefix",
			args: map[string]interface{}{"prefix": "test-"},
		},
		{
			name: "with page size",
			args: map[string]interface{}{"page_size": float64(50)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_list_namespaces", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "namespaces")
			assert.Contains(t, resultMap, "total_count")
		})
	}
}

func TestVectorGetNamespaceInfo_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_get_namespace_info", map[string]interface{}{
		"namespace": "test-ns",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "namespace")
	assert.Contains(t, resultMap, "dimensions")
	assert.Contains(t, resultMap, "vector_count")
}

func TestVectorGetNamespaceInfo_MissingNamespace(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_get_namespace_info", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "namespace")
}

func TestVectorUpdateNamespace_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_update_namespace", map[string]interface{}{
		"namespace": "test-ns",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestVectorUpdateNamespace_MissingNamespace(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_update_namespace", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
}

// =============================================================================
// Vector CRUD Tests
// =============================================================================

func TestVectorUpsert_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "with string vector",
			args: map[string]interface{}{
				"namespace": "test-ns",
				"id":        "vec-1",
				"vector":    "[0.1, 0.2, 0.3]",
			},
		},
		{
			name: "with array vector",
			args: map[string]interface{}{
				"namespace": "test-ns",
				"id":        "vec-2",
				"vector":    []interface{}{0.1, 0.2, 0.3},
			},
		},
		{
			name: "with metadata string",
			args: map[string]interface{}{
				"namespace": "test-ns",
				"id":        "vec-3",
				"vector":    "[0.1, 0.2, 0.3]",
				"metadata":  `{"type":"test"}`,
			},
		},
		{
			name: "with metadata map",
			args: map[string]interface{}{
				"namespace": "test-ns",
				"id":        "vec-4",
				"vector":    []interface{}{0.1, 0.2, 0.3},
				"metadata":  map[string]interface{}{"type": "test"},
			},
		},
		{
			name: "with metadata string map",
			args: map[string]interface{}{
				"namespace": "test-ns",
				"id":        "vec-5",
				"vector":    []interface{}{0.1, 0.2, 0.3},
				"metadata":  map[string]string{"type": "test"},
			},
		},
		{
			name: "with float32 vector",
			args: map[string]interface{}{
				"namespace": "test-ns",
				"id":        "vec-6",
				"vector":    []float32{0.1, 0.2, 0.3},
			},
		},
		{
			name: "with float64 vector",
			args: map[string]interface{}{
				"namespace": "test-ns",
				"id":        "vec-7",
				"vector":    []float64{0.1, 0.2, 0.3},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_upsert", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "id")
			assert.Contains(t, resultMap, "success")
		})
	}
}

func TestVectorUpsert_Errors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{
			name:        "missing namespace",
			args:        map[string]interface{}{"id": "vec-1", "vector": "[0.1, 0.2]"},
			errContains: "namespace",
		},
		{
			name:        "missing id",
			args:        map[string]interface{}{"namespace": "test", "vector": "[0.1, 0.2]"},
			errContains: "id",
		},
		{
			name:        "invalid vector json",
			args:        map[string]interface{}{"namespace": "test", "id": "v1", "vector": "invalid"},
			errContains: "invalid vector",
		},
		{
			name:        "missing vector",
			args:        map[string]interface{}{"namespace": "test", "id": "v1"},
			errContains: "vector must be array",
		},
		{
			name:        "invalid metadata json",
			args:        map[string]interface{}{"namespace": "test", "id": "v1", "vector": "[0.1]", "metadata": "invalid"},
			errContains: "invalid metadata",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry := newVectorRegistry(t)
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_upsert", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestVectorBatchUpsert_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	vectors := []map[string]interface{}{
		{"id": "vec-1", "vector": []interface{}{0.1, 0.2, 0.3}},
		{"id": "vec-2", "vector": []interface{}{0.4, 0.5, 0.6}, "metadata": map[string]interface{}{"type": "test"}},
	}
	vectorsJSON, _ := json.Marshal(vectors)

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_batch_upsert", map[string]interface{}{
		"namespace": "test-ns",
		"vectors":   string(vectorsJSON),
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "inserted_count")
	assert.Contains(t, resultMap, "success")
}

func TestVectorBatchUpsert_Errors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{
			name:        "missing namespace",
			args:        map[string]interface{}{"vectors": "[]"},
			errContains: "namespace",
		},
		{
			name:        "missing vectors",
			args:        map[string]interface{}{"namespace": "test"},
			errContains: "vectors",
		},
		{
			name:        "invalid vectors json",
			args:        map[string]interface{}{"namespace": "test", "vectors": "invalid"},
			errContains: "invalid vectors",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry := newVectorRegistry(t)
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_batch_upsert", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestVectorGet_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_get", map[string]interface{}{
		"namespace":        "test-ns",
		"id":               "vec-1",
		"include_vector":   true,
		"include_metadata": true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "id")
	assert.Contains(t, resultMap, "found")
}

func TestVectorGet_Errors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{
			name:        "missing namespace",
			args:        map[string]interface{}{"id": "vec-1"},
			errContains: "namespace",
		},
		{
			name:        "missing id",
			args:        map[string]interface{}{"namespace": "test"},
			errContains: "id",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry := newVectorRegistry(t)
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_get", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestVectorDelete_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_delete", map[string]interface{}{
		"namespace": "test-ns",
		"id":        "vec-1",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "deleted")
}

func TestVectorDelete_Errors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{
			name:        "missing namespace",
			args:        map[string]interface{}{"id": "vec-1"},
			errContains: "namespace",
		},
		{
			name:        "missing id",
			args:        map[string]interface{}{"namespace": "test"},
			errContains: "id",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry := newVectorRegistry(t)
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_delete", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestVectorUpdateMetadata_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "with json metadata",
			args: map[string]interface{}{
				"namespace": "test-ns",
				"id":        "vec-1",
				"metadata":  `{"updated": true}`,
			},
		},
		{
			name: "with map metadata",
			args: map[string]interface{}{
				"namespace": "test-ns",
				"id":        "vec-2",
				"metadata":  map[string]interface{}{"updated": true},
			},
		},
		{
			name: "with merge false",
			args: map[string]interface{}{
				"namespace": "test-ns",
				"id":        "vec-3",
				"metadata":  map[string]interface{}{"new": "value"},
				"merge":     false,
			},
		},
		{
			name: "with merge string false",
			args: map[string]interface{}{
				"namespace": "test-ns",
				"id":        "vec-4",
				"metadata":  map[string]interface{}{"new": "value"},
				"merge":     "false",
			},
		},
		{
			name: "with string map metadata",
			args: map[string]interface{}{
				"namespace": "test-ns",
				"id":        "vec-5",
				"metadata":  map[string]string{"key": "value"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_update_metadata", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "success")
		})
	}
}

func TestVectorUpdateMetadata_Errors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		setupFunc   func() *mockVectorService
		errContains string
	}{
		{
			name:        "missing namespace",
			args:        map[string]interface{}{"id": "v1", "metadata": `{}`},
			errContains: "namespace",
		},
		{
			name:        "missing id",
			args:        map[string]interface{}{"namespace": "ns", "metadata": `{}`},
			errContains: "id",
		},
		{
			name:        "missing metadata",
			args:        map[string]interface{}{"namespace": "ns", "id": "v1"},
			errContains: "metadata",
		},
		{
			name:        "invalid metadata json",
			args:        map[string]interface{}{"namespace": "ns", "id": "v1", "metadata": "invalid"},
			errContains: "invalid metadata",
		},
		{
			name: "vector not found",
			args: map[string]interface{}{"namespace": "ns", "id": "v1", "metadata": `{}`},
			setupFunc: func() *mockVectorService {
				return &mockVectorService{
					getVectorFunc: func(ctx context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error) {
						return &service.GetVectorResponse{Found: false}, nil
					},
				}
			},
			errContains: "not found",
		},
		{
			name: "get vector error",
			args: map[string]interface{}{"namespace": "ns", "id": "v1", "metadata": `{}`},
			setupFunc: func() *mockVectorService {
				return &mockVectorService{
					getVectorFunc: func(ctx context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error) {
						return nil, &errors.MessageError{Message: "get error"}
					},
				}
			},
			errContains: "get vector failed",
		},
		{
			name: "upsert error",
			args: map[string]interface{}{"namespace": "ns", "id": "v1", "metadata": `{}`},
			setupFunc: func() *mockVectorService {
				return &mockVectorService{
					getVectorFunc: func(ctx context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error) {
						return &service.GetVectorResponse{Found: true, Vector: []float32{0.1}}, nil
					},
					upsertVectorFunc: func(ctx context.Context, req *service.UpsertVectorRequest) (*service.UpsertVectorResponse, error) {
						return nil, &errors.MessageError{Message: "upsert error"}
					},
				}
			},
			errContains: "update metadata failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var registry *ToolRegistry
			if tc.setupFunc != nil {
				registry = newVectorRegistryWithMock(t, tc.setupFunc())
			} else {
				registry = newVectorRegistry(t)
			}
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_update_metadata", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestVectorBatchDelete_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	ids := []string{"vec-1", "vec-2", "vec-3"}
	idsJSON, _ := json.Marshal(ids)

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_batch_delete", map[string]interface{}{
		"namespace": "test-ns",
		"ids":       string(idsJSON),
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "deleted_count")
}

func TestVectorBatchDelete_Errors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{
			name:        "missing namespace",
			args:        map[string]interface{}{"ids": `["v1"]`},
			errContains: "namespace",
		},
		{
			name:        "missing ids",
			args:        map[string]interface{}{"namespace": "test"},
			errContains: "ids",
		},
		{
			name:        "invalid ids json",
			args:        map[string]interface{}{"namespace": "test", "ids": "invalid"},
			errContains: "invalid ids",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry := newVectorRegistry(t)
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_batch_delete", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestVectorList_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_list", map[string]interface{}{
		"namespace": "test-ns",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "vector_count")
}

func TestVectorList_MissingNamespace(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_list", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "namespace")
}

// =============================================================================
// Vector Search Tests
// =============================================================================

func TestVectorSearchSimilar_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "with string query_vector",
			args: map[string]interface{}{
				"namespace":    "test-ns",
				"query_vector": "[0.1, 0.2, 0.3]",
				"k":            float64(5),
			},
		},
		{
			name: "with array query_vector",
			args: map[string]interface{}{
				"namespace":    "test-ns",
				"query_vector": []interface{}{0.1, 0.2, 0.3},
				"k":            float64(10),
			},
		},
		{
			name: "with vector alias",
			args: map[string]interface{}{
				"namespace": "test-ns",
				"vector":    []interface{}{0.1, 0.2, 0.3},
				"k":         float64(5),
			},
		},
		{
			name: "with include options",
			args: map[string]interface{}{
				"namespace":        "test-ns",
				"query_vector":     []interface{}{0.1, 0.2, 0.3},
				"k":                float64(5),
				"include_vectors":  true,
				"include_metadata": true,
			},
		},
		{
			name: "with float32 vector",
			args: map[string]interface{}{
				"namespace":    "test-ns",
				"query_vector": []float32{0.1, 0.2, 0.3},
				"k":            float64(5),
			},
		},
		{
			name: "with float64 vector",
			args: map[string]interface{}{
				"namespace":    "test-ns",
				"query_vector": []float64{0.1, 0.2, 0.3},
				"k":            float64(5),
			},
		},
		{
			name: "default k",
			args: map[string]interface{}{
				"namespace":    "test-ns",
				"query_vector": []interface{}{0.1, 0.2, 0.3},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_search_similar", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "results")
			assert.Contains(t, resultMap, "k")
		})
	}
}

func TestVectorSearchSimilar_Errors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{
			name:        "missing namespace",
			args:        map[string]interface{}{"query_vector": "[0.1]", "k": float64(5)},
			errContains: "namespace",
		},
		{
			name:        "missing query_vector",
			args:        map[string]interface{}{"namespace": "test", "k": float64(5)},
			errContains: "query_vector",
		},
		{
			name:        "invalid query_vector json",
			args:        map[string]interface{}{"namespace": "test", "query_vector": "invalid", "k": float64(5)},
			errContains: "invalid query_vector",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry := newVectorRegistry(t)
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_search_similar", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestVectorSearchByID_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "basic",
			args: map[string]interface{}{
				"namespace": "test-ns",
				"id":        "vec-1",
				"k":         float64(5),
			},
		},
		{
			name: "with include_self",
			args: map[string]interface{}{
				"namespace":    "test-ns",
				"id":           "vec-2",
				"k":            float64(5),
				"include_self": true,
			},
		},
		{
			name: "default k",
			args: map[string]interface{}{
				"namespace": "test-ns",
				"id":        "vec-3",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_search_by_id", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "results")
			assert.Contains(t, resultMap, "query_id")
		})
	}
}

func TestVectorSearchByID_Errors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		setupFunc   func() *mockVectorService
		errContains string
	}{
		{
			name:        "missing namespace",
			args:        map[string]interface{}{"id": "v1", "k": float64(5)},
			errContains: "namespace",
		},
		{
			name:        "missing id",
			args:        map[string]interface{}{"namespace": "test", "k": float64(5)},
			errContains: "id",
		},
		{
			name: "vector not found",
			args: map[string]interface{}{"namespace": "test", "id": "v1", "k": float64(5)},
			setupFunc: func() *mockVectorService {
				return &mockVectorService{
					getVectorFunc: func(ctx context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error) {
						return &service.GetVectorResponse{Found: false}, nil
					},
				}
			},
			errContains: "not found",
		},
		{
			name: "get vector error",
			args: map[string]interface{}{"namespace": "test", "id": "v1", "k": float64(5)},
			setupFunc: func() *mockVectorService {
				return &mockVectorService{
					getVectorFunc: func(ctx context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error) {
						return nil, &errors.MessageError{Message: "get error"}
					},
				}
			},
			errContains: "get error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var registry *ToolRegistry
			if tc.setupFunc != nil {
				registry = newVectorRegistryWithMock(t, tc.setupFunc())
			} else {
				registry = newVectorRegistry(t)
			}
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_search_by_id", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

// =============================================================================
// Vector Stats and Health Tests
// =============================================================================

func TestVectorGetStats_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "with namespace",
			args: map[string]interface{}{"namespace": "test-ns"},
		},
		{
			name: "cluster stats (no namespace)",
			args: map[string]interface{}{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_get_stats", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestVectorHealth_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_health", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "status")
}

func TestVectorHealth_Unhealthy(t *testing.T) {
	vectorSvc := &mockVectorService{
		listNamespacesFunc: func(ctx context.Context, req *service.ListNamespacesRequest) (*service.ListNamespacesResponse, error) {
			return nil, &errors.MessageError{Message: "database unavailable"}
		},
	}
	registry := newVectorRegistryWithMock(t, vectorSvc)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_health", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "unhealthy", resultMap["status"])
}

// =============================================================================
// Vector Index Tests
// =============================================================================

func TestVectorRebuildIndex_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "default async",
			args: map[string]interface{}{"namespace": "test-ns"},
		},
		{
			name: "explicit async true",
			args: map[string]interface{}{"namespace": "test-ns", "async": true},
		},
		{
			name: "async false",
			args: map[string]interface{}{"namespace": "test-ns", "async": false},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_rebuild_index", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "job_id")
			assert.Contains(t, resultMap, "status")
		})
	}
}

func TestVectorRebuildIndex_MissingNamespace(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_rebuild_index", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "namespace")
}

func TestVectorGetIndexProgress_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_get_index_progress", map[string]interface{}{
		"namespace":    "test-ns",
		"operation_id": "job-123",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "status")
	assert.Contains(t, resultMap, "percent_complete")
}

func TestVectorGetIndexProgress_MissingNamespace(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_get_index_progress", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "namespace")
}

// =============================================================================
// Vector Admin Tests
// =============================================================================

func TestVectorCompactNamespace_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "default async",
			args: map[string]interface{}{"namespace": "test-ns"},
		},
		{
			name: "explicit async",
			args: map[string]interface{}{"namespace": "test-ns", "async": true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "quicbaas_vector_admin_compact_namespace", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, resultMap, "operation_id")
		})
	}
}

func TestVectorCompactNamespace_MissingNamespace(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_admin_compact_namespace", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "namespace")
}

func TestVectorGetCompactionProgress_Success(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_admin_get_compaction_progress", map[string]interface{}{
		"namespace":    "test-ns",
		"operation_id": "compact-123",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, resultMap, "percent_complete")
	assert.Contains(t, resultMap, "in_progress")
}

func TestVectorGetCompactionProgress_MissingNamespace(t *testing.T) {
	registry := newVectorRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_vector_admin_get_compaction_progress", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "namespace")
}

// =============================================================================
// Vector Service Unavailable Tests
// =============================================================================

func TestVectorTools_ServiceUnavailable(t *testing.T) {
	svc := &mockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	vectorTools := []string{
		"quicbaas_vector_create_namespace",
		"quicbaas_vector_delete_namespace",
		"quicbaas_vector_list_namespaces",
		"quicbaas_vector_get_namespace_info",
		"quicbaas_vector_upsert",
		"quicbaas_vector_get",
		"quicbaas_vector_delete",
		"quicbaas_vector_search_similar",
	}

	for _, toolName := range vectorTools {
		t.Run(toolName, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, toolName, map[string]interface{}{
				"namespace":    "test",
				"id":           "v1",
				"dimensions":   float64(128),
				"vector":       "[0.1]",
				"query_vector": "[0.1]",
				"k":            float64(5),
			})
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "not available")
		})
	}
}

// =============================================================================
// Hash Function Test
// =============================================================================

func TestHashStringToID(t *testing.T) {
	// Test that same input produces same output
	id1 := hashStringToID("test-id-1")
	id2 := hashStringToID("test-id-1")
	assert.Equal(t, id1, id2, "Same input should produce same hash")

	// Test that different inputs produce different outputs
	id3 := hashStringToID("test-id-2")
	assert.NotEqual(t, id1, id3, "Different inputs should produce different hashes")

	// Test empty string
	id4 := hashStringToID("")
	assert.NotZero(t, id4, "Empty string should produce non-zero hash")
}

// =============================================================================
// isVectorTool Test
// =============================================================================

func TestIsVectorTool(t *testing.T) {
	vectorTools := []string{
		"quicbaas_vector_upsert",
		"quicbaas_vector_get",
		"quicbaas_vector_search_similar",
		"quicbaas_vector_admin_compact_namespace",
	}

	for _, name := range vectorTools {
		t.Run(name+"_true", func(t *testing.T) {
			assert.True(t, isVectorTool(name))
		})
	}

	nonVectorTools := []string{
		"quicbaas_put",
		"quicbaas_get",
		"quicbaas_embedding_generate",
	}

	for _, name := range nonVectorTools {
		t.Run(name+"_false", func(t *testing.T) {
			assert.False(t, isVectorTool(name))
		})
	}
}
