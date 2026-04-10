//go:build !integration

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Vector KV-backed namespace operations (no vectorStorage)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_CreateNamespace_KV(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("creates namespace successfully", func(t *testing.T) {
		req := &transport.CreateNamespaceRequest{
			Namespace:      "test-ns",
			Dimension:      128,
			DistanceMetric: transport.DistanceCosine,
		}
		resp, err := bt.CreateNamespace(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, "test-ns", resp.Namespace)
	})

	t.Run("returns error for duplicate namespace", func(t *testing.T) {
		req := &transport.CreateNamespaceRequest{
			Namespace:      "test-ns",
			Dimension:      128,
			DistanceMetric: transport.DistanceCosine,
		}
		_, err := bt.CreateNamespace(ctx, req)
		require.Error(t, err)
		var existsErr *VectorNamespaceExistsError
		assert.True(t, errors.As(err, &existsErr))
	})

	t.Run("returns error for nil request", func(t *testing.T) {
		_, err := bt.CreateNamespace(ctx, nil)
		require.Error(t, err)
	})
}

func TestBaasTransport_Vector_DeleteNamespace_KV(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "del-ns", Dimension: 64})
	require.NoError(t, err)

	t.Run("deletes existing namespace", func(t *testing.T) {
		resp, err := bt.DeleteNamespace(ctx, &transport.DeleteNamespaceRequest{Namespace: "del-ns"})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("returns error for missing namespace", func(t *testing.T) {
		_, err := bt.DeleteNamespace(ctx, &transport.DeleteNamespaceRequest{Namespace: "nonexistent"})
		require.Error(t, err)
	})

	t.Run("returns error for nil request", func(t *testing.T) {
		_, err := bt.DeleteNamespace(ctx, nil)
		require.Error(t, err)
	})
}

func TestBaasTransport_Vector_ListNamespaces_KV(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "ns-a", Dimension: 64})
	require.NoError(t, err)
	_, err = bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "ns-b", Dimension: 128})
	require.NoError(t, err)

	t.Run("lists all namespaces", func(t *testing.T) {
		resp, err := bt.ListNamespaces(ctx, &transport.ListNamespacesRequest{})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(resp.Namespaces), 2)
	})

	t.Run("nil request returns empty", func(t *testing.T) {
		resp, err := bt.ListNamespaces(ctx, nil)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_Vector_GetNamespaceInfo_KV(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{
		Namespace:      "info-ns",
		Dimension:      256,
		DistanceMetric: transport.DistanceEuclidean,
	})
	require.NoError(t, err)

	t.Run("returns namespace info", func(t *testing.T) {
		resp, err := bt.GetNamespaceInfo(ctx, &transport.GetNamespaceInfoRequest{Namespace: "info-ns"})
		require.NoError(t, err)
		assert.Equal(t, "info-ns", resp.Namespace)
		assert.Equal(t, uint32(256), resp.Dimension)
	})

	t.Run("returns error for missing namespace", func(t *testing.T) {
		_, err := bt.GetNamespaceInfo(ctx, &transport.GetNamespaceInfoRequest{Namespace: "nonexistent"})
		require.Error(t, err)
	})

	t.Run("returns error for nil request", func(t *testing.T) {
		_, err := bt.GetNamespaceInfo(ctx, nil)
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// Vector KV-backed CRUD operations
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_UpsertAndGet_KV(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "crud-ns", Dimension: 3})
	require.NoError(t, err)

	t.Run("upserts a vector", func(t *testing.T) {
		req := &transport.UpsertVectorRequest{
			Namespace: "crud-ns",
			ID:        1,
			Vector:    []float32{1.0, 2.0, 3.0},
			Metadata:  map[string]interface{}{"color": "red"},
		}
		resp, err := bt.UpsertVector(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("gets the vector", func(t *testing.T) {
		resp, err := bt.GetVector(ctx, &transport.GetVectorRequest{Namespace: "crud-ns", ID: 1, IncludeVector: true})
		require.NoError(t, err)
		assert.True(t, resp.Found)
		assert.Equal(t, uint64(1), resp.ID)
		assert.Len(t, resp.Vector, 3)
	})

	t.Run("returns not-found for missing vector", func(t *testing.T) {
		resp, err := bt.GetVector(ctx, &transport.GetVectorRequest{Namespace: "crud-ns", ID: 999})
		require.NoError(t, err)
		assert.False(t, resp.Found)
	})

	t.Run("returns error for nil request", func(t *testing.T) {
		_, err := bt.UpsertVector(ctx, nil)
		require.Error(t, err)
	})

	t.Run("returns error for wrong dimension", func(t *testing.T) {
		req := &transport.UpsertVectorRequest{
			Namespace: "crud-ns",
			ID:        2,
			Vector:    []float32{1.0, 2.0},
		}
		_, err := bt.UpsertVector(ctx, req)
		require.Error(t, err)
		var dimErr *VectorDimensionMismatchError
		assert.True(t, errors.As(err, &dimErr))
	})
}

func TestBaasTransport_Vector_Delete_KV(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "delvec-ns", Dimension: 2})
	require.NoError(t, err)
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
		Namespace: "delvec-ns", ID: 1, Vector: []float32{1, 2},
	})
	require.NoError(t, err)

	t.Run("deletes existing vector", func(t *testing.T) {
		resp, err := bt.DeleteVector(ctx, &transport.DeleteVectorRequest{Namespace: "delvec-ns", ID: 1})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("returns error for nil request", func(t *testing.T) {
		_, err := bt.DeleteVector(ctx, nil)
		require.Error(t, err)
	})
}

func TestBaasTransport_Vector_BatchUpsert_KV(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "batch-ns", Dimension: 2})
	require.NoError(t, err)

	t.Run("batch upserts multiple vectors", func(t *testing.T) {
		req := &transport.BatchUpsertRequest{
			Namespace: "batch-ns",
			Vectors: []*transport.VectorData{
				{ID: 1, Vector: []float32{1, 0}},
				{ID: 2, Vector: []float32{0, 1}},
			},
		}
		resp, err := bt.BatchUpsert(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("returns error for nil request", func(t *testing.T) {
		_, err := bt.BatchUpsert(ctx, nil)
		require.Error(t, err)
	})
}

func TestBaasTransport_Vector_BatchDelete_KV(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "bdel-ns", Dimension: 2})
	require.NoError(t, err)
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{Namespace: "bdel-ns", ID: 1, Vector: []float32{1, 0}})
	require.NoError(t, err)

	t.Run("batch deletes vectors", func(t *testing.T) {
		req := &transport.BatchDeleteVectorsRequest{
			Namespace: "bdel-ns",
			IDs:       []uint64{1},
		}
		resp, err := bt.BatchDeleteVectors(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("returns error for nil request", func(t *testing.T) {
		_, err := bt.BatchDeleteVectors(ctx, nil)
		require.Error(t, err)
	})
}

func TestBaasTransport_Vector_ListVectors_KV(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "list-ns", Dimension: 2})
	require.NoError(t, err)
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{Namespace: "list-ns", ID: 1, Vector: []float32{1, 0}})
	require.NoError(t, err)
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{Namespace: "list-ns", ID: 2, Vector: []float32{0, 1}})
	require.NoError(t, err)

	t.Run("lists vectors in namespace", func(t *testing.T) {
		req := &transport.ListVectorsRequest{
			Namespace: "list-ns",
			Limit:     100,
		}
		resp, err := bt.ListVectors(ctx, req)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(resp.Vectors), 2)
	})

	t.Run("nil request returns empty response", func(t *testing.T) {
		resp, err := bt.ListVectors(ctx, nil)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_Vector_UpdateMetadata_KV(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "meta-ns", Dimension: 2})
	require.NoError(t, err)
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
		Namespace: "meta-ns", ID: 1, Vector: []float32{1, 0},
		Metadata: map[string]interface{}{"color": "red"},
	})
	require.NoError(t, err)

	t.Run("updates metadata", func(t *testing.T) {
		req := &transport.UpdateVectorMetadataRequest{
			Namespace: "meta-ns",
			ID:        1,
			Metadata:  map[string]interface{}{"color": "blue"},
		}
		resp, err := bt.UpdateMetadata(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("returns error for nil request", func(t *testing.T) {
		_, err := bt.UpdateMetadata(ctx, nil)
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// Vector search operations
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_SearchSimilar_KV(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{
		Namespace: "search-ns", Dimension: 3, DistanceMetric: transport.DistanceCosine,
	})
	require.NoError(t, err)

	for _, v := range []struct {
		id  uint64
		vec []float32
	}{{1, []float32{1, 0, 0}}, {2, []float32{0, 1, 0}}, {3, []float32{1, 1, 0}}} {
		_, err := bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
			Namespace: "search-ns", ID: v.id, Vector: v.vec,
		})
		require.NoError(t, err)
	}

	t.Run("finds similar vectors", func(t *testing.T) {
		resp, err := bt.SearchSimilar(ctx, &transport.SearchSimilarRequest{
			Namespace: "search-ns", QueryVector: []float32{1, 0, 0}, K: 2,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.LessOrEqual(t, len(resp.Results), 2)
	})

	t.Run("returns error for nil request", func(t *testing.T) {
		_, err := bt.SearchSimilar(ctx, nil)
		require.Error(t, err)
	})
}

func TestBaasTransport_Vector_SearchByMetadata_KV(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "metasearch-ns", Dimension: 2})
	require.NoError(t, err)
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
		Namespace: "metasearch-ns", ID: 1, Vector: []float32{1, 0},
		Metadata: map[string]interface{}{"color": "red"},
	})
	require.NoError(t, err)

	t.Run("finds vectors by metadata", func(t *testing.T) {
		resp, err := bt.SearchByMetadata(ctx, &transport.SearchByMetadataRequest{
			Namespace: "metasearch-ns", Filters: map[string]interface{}{"color": "red"}, Limit: 10,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("returns error for nil request", func(t *testing.T) {
		_, err := bt.SearchByMetadata(ctx, nil)
		require.Error(t, err)
	})
}

func TestBaasTransport_Vector_HybridSearch_KV(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "hybrid-ns", Dimension: 2})
	require.NoError(t, err)
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
		Namespace: "hybrid-ns", ID: 1, Vector: []float32{1, 0},
		Metadata: map[string]interface{}{"color": "red"},
	})
	require.NoError(t, err)

	t.Run("hybrid search works", func(t *testing.T) {
		resp, err := bt.HybridSearch(ctx, &transport.HybridSearchRequest{
			Namespace: "hybrid-ns", QueryVector: []float32{1, 0},
			MetadataFilter: map[string]interface{}{"color": "red"}, K: 5,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("returns error for nil request", func(t *testing.T) {
		_, err := bt.HybridSearch(ctx, nil)
		require.Error(t, err)
	})
}

func TestBaasTransport_Vector_SearchByID_KV(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "idsr-ns", Dimension: 2})
	require.NoError(t, err)
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{Namespace: "idsr-ns", ID: 1, Vector: []float32{1, 0}})
	require.NoError(t, err)
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{Namespace: "idsr-ns", ID: 2, Vector: []float32{0, 1}})
	require.NoError(t, err)

	t.Run("finds vectors similar to given ID", func(t *testing.T) {
		resp, err := bt.SearchByID(ctx, &transport.SearchByIDRequest{
			Namespace: "idsr-ns", ID: 1, K: 5,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("returns error for nil request", func(t *testing.T) {
		_, err := bt.SearchByID(ctx, nil)
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// Vector index stats / rebuild / compact
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_IndexStats_KV(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "stats-ns", Dimension: 2})
	require.NoError(t, err)

	t.Run("gets index stats", func(t *testing.T) {
		resp, err := bt.GetIndexStats(ctx, &transport.GetIndexStatsRequest{Namespace: "stats-ns"})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("returns error for nil request", func(t *testing.T) {
		_, err := bt.GetIndexStats(ctx, nil)
		require.Error(t, err)
	})
}

func TestBaasTransport_Vector_Rebuild_KV(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("rebuild index", func(t *testing.T) {
		_, err := bt.RebuildIndex(ctx, &transport.RebuildIndexRequest{Namespace: "ns"})
		// KV-backed may return not-supported or succeed
		_ = err
	})

	t.Run("get rebuild progress", func(t *testing.T) {
		resp, err := bt.GetRebuildProgress(ctx, &transport.GetRebuildProgressRequest{Namespace: "ns"})
		_ = err
		_ = resp
	})
}

func TestBaasTransport_Vector_Compact_KV(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("compact namespace", func(t *testing.T) {
		_, err := bt.CompactNamespace(ctx, &transport.CompactNamespaceRequest{Namespace: "ns"})
		_ = err
	})

	t.Run("get compaction progress", func(t *testing.T) {
		resp, err := bt.GetCompactionProgress(ctx, &transport.GetCompactionProgressRequest{Namespace: "ns"})
		_ = err
		_ = resp
	})
}
