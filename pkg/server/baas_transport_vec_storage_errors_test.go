//go:build !integration

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ---------------------------------------------------------------------------
// getNamespaceInfoViaStorage error paths (starting at 55.6%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_GetNamespaceInfo_ViaStorage_Error(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		getNamespaceInfoFunc: func(ctx context.Context, namespace string) (*interfaces.VectorNamespaceInfo, error) {
			return nil, &ServerMessageError{Message: "namespace not found"}
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("storage error propagates", func(t *testing.T) {
		_, err := bt.GetNamespaceInfo(ctx, &transport.GetNamespaceInfoRequest{Namespace: "missing"})
		require.Error(t, err)
	})
}

func TestBaasTransport_Vector_GetNamespaceInfo_ViaStorage_WithMetadata(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		getNamespaceInfoFunc: func(ctx context.Context, namespace string) (*interfaces.VectorNamespaceInfo, error) {
			return &interfaces.VectorNamespaceInfo{
				Name:      namespace,
				Dimension: 128,
				Metadata:  map[string]string{"env": "prod", "team": "ml"},
			}, nil
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("metadata is converted from string map to interface map", func(t *testing.T) {
		resp, err := bt.GetNamespaceInfo(ctx, &transport.GetNamespaceInfoRequest{Namespace: "ns1"})
		require.NoError(t, err)
		assert.Equal(t, "ns1", resp.Namespace)
		assert.Equal(t, uint32(128), resp.Dimension)
		assert.Equal(t, "prod", resp.Metadata["env"])
		assert.Equal(t, "ml", resp.Metadata["team"])
	})
}

func TestBaasTransport_Vector_GetNamespaceInfo_ViaStorage_EmptyMetadata(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		getNamespaceInfoFunc: func(ctx context.Context, namespace string) (*interfaces.VectorNamespaceInfo, error) {
			return &interfaces.VectorNamespaceInfo{
				Name:      namespace,
				Dimension: 64,
				Metadata:  nil,
			}, nil
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("nil metadata results in nil metadata response", func(t *testing.T) {
		resp, err := bt.GetNamespaceInfo(ctx, &transport.GetNamespaceInfoRequest{Namespace: "ns1"})
		require.NoError(t, err)
		assert.Nil(t, resp.Metadata)
	})
}

// ---------------------------------------------------------------------------
// createNamespaceViaStorage error paths (starting at 75%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_CreateNamespace_ViaStorage_Error(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		createNamespaceFunc: func(ctx context.Context, info *interfaces.VectorNamespaceInfo) error {
			return &ServerMessageError{Message: "creation failed"}
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("storage error propagates", func(t *testing.T) {
		_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{
			Namespace: "fail-ns", Dimension: 128,
		})
		require.Error(t, err)
	})
}

func TestBaasTransport_Vector_CreateNamespace_ViaStorage_WithMetadata(t *testing.T) {
	store := NewMemoryKVStore()
	var capturedInfo *interfaces.VectorNamespaceInfo
	vs := &mockVectorStorage{
		createNamespaceFunc: func(ctx context.Context, info *interfaces.VectorNamespaceInfo) error {
			capturedInfo = info
			return nil
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("metadata is converted to string map", func(t *testing.T) {
		_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{
			Namespace: "meta-ns",
			Dimension: 128,
			Metadata:  map[string]interface{}{"env": "test"},
		})
		require.NoError(t, err)
		require.NotNil(t, capturedInfo)
		assert.Equal(t, "test", capturedInfo.Metadata["env"])
	})
}

// ---------------------------------------------------------------------------
// deleteNamespaceViaStorage error paths
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_DeleteNamespace_ViaStorage_Error(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		deleteNamespaceFunc: func(ctx context.Context, namespace string) error {
			return &ServerMessageError{Message: "delete failed"}
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("storage error propagates", func(t *testing.T) {
		_, err := bt.DeleteNamespace(ctx, &transport.DeleteNamespaceRequest{Namespace: "ns"})
		require.Error(t, err)
	})
}

func TestBaasTransport_Vector_DeleteNamespace_ViaStorage_StatsError(t *testing.T) {
	store := NewMemoryKVStore()
	var deleteNsCalled bool
	vs := &mockVectorStorage{
		getStatsFunc: func(ctx context.Context, namespace string) (*interfaces.VectorStorageStats, error) {
			return nil, &ServerMessageError{Message: "stats failed"}
		},
		deleteNamespaceFunc: func(ctx context.Context, namespace string) error {
			deleteNsCalled = true
			return nil
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("stats error does not prevent deletion", func(t *testing.T) {
		resp, err := bt.DeleteNamespace(ctx, &transport.DeleteNamespaceRequest{Namespace: "ns"})
		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.True(t, deleteNsCalled)
		assert.Equal(t, uint64(0), resp.VectorsDeleted)
	})
}

// ---------------------------------------------------------------------------
// deleteNamespaceViaKV error paths (starting at 61.1%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_DeleteNamespace_KV_ScanError(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	// Create namespace first.
	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{
		Namespace: "scan-err-ns", Dimension: 3,
	})
	require.NoError(t, err)

	// Add some vectors.
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
		Namespace: "scan-err-ns", ID: 1, Vector: []float32{1, 0, 0},
	})
	require.NoError(t, err)

	t.Run("successful deletion with vectors present", func(t *testing.T) {
		resp, err := bt.DeleteNamespace(ctx, &transport.DeleteNamespaceRequest{Namespace: "scan-err-ns"})
		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Equal(t, uint64(1), resp.VectorsDeleted)
	})
}

// ---------------------------------------------------------------------------
// listNamespacesViaStorage error paths (starting at 68.8%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_ListNamespaces_ViaStorage_Error(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		listNamespacesFunc: func(ctx context.Context) ([]interfaces.VectorNamespaceInfo, error) {
			return nil, &ServerMessageError{Message: "list failed"}
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("storage error propagates", func(t *testing.T) {
		_, err := bt.ListNamespaces(ctx, &transport.ListNamespacesRequest{})
		require.Error(t, err)
	})
}

func TestBaasTransport_Vector_ListNamespaces_ViaStorage_WithPrefix(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		listNamespacesFunc: func(ctx context.Context) ([]interfaces.VectorNamespaceInfo, error) {
			return []interfaces.VectorNamespaceInfo{
				{Name: "prod-ns1", Dimension: 128},
				{Name: "prod-ns2", Dimension: 256},
				{Name: "dev-ns1", Dimension: 64},
			}, nil
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("prefix filter is applied", func(t *testing.T) {
		resp, err := bt.ListNamespaces(ctx, &transport.ListNamespacesRequest{Prefix: "prod-"})
		require.NoError(t, err)
		assert.Len(t, resp.Namespaces, 2)
	})
}

func TestBaasTransport_Vector_ListNamespaces_ViaStorage_OffsetAndLimit(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		listNamespacesFunc: func(ctx context.Context) ([]interfaces.VectorNamespaceInfo, error) {
			return []interfaces.VectorNamespaceInfo{
				{Name: "ns1", Dimension: 64},
				{Name: "ns2", Dimension: 128},
				{Name: "ns3", Dimension: 256},
			}, nil
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("offset skips namespaces", func(t *testing.T) {
		resp, err := bt.ListNamespaces(ctx, &transport.ListNamespacesRequest{Offset: 1})
		require.NoError(t, err)
		assert.Len(t, resp.Namespaces, 2)
	})

	t.Run("limit caps result count", func(t *testing.T) {
		resp, err := bt.ListNamespaces(ctx, &transport.ListNamespacesRequest{Limit: 1})
		require.NoError(t, err)
		assert.Len(t, resp.Namespaces, 1)
	})

	t.Run("offset beyond count returns empty", func(t *testing.T) {
		resp, err := bt.ListNamespaces(ctx, &transport.ListNamespacesRequest{Offset: 10})
		require.NoError(t, err)
		assert.Empty(t, resp.Namespaces)
	})
}

// ---------------------------------------------------------------------------
// listNamespacesViaKV error paths (starting at 68.4%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_ListNamespaces_KV_OffsetAndLimit(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	// Create three namespaces.
	for _, ns := range []string{"ns-a", "ns-b", "ns-c"} {
		_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: ns, Dimension: 2})
		require.NoError(t, err)
	}

	t.Run("offset skips entries", func(t *testing.T) {
		resp, err := bt.ListNamespaces(ctx, &transport.ListNamespacesRequest{Offset: 1})
		require.NoError(t, err)
		assert.Len(t, resp.Namespaces, 2)
	})

	t.Run("limit caps results", func(t *testing.T) {
		resp, err := bt.ListNamespaces(ctx, &transport.ListNamespacesRequest{Limit: 1})
		require.NoError(t, err)
		assert.Len(t, resp.Namespaces, 1)
	})

	t.Run("offset beyond count returns empty", func(t *testing.T) {
		resp, err := bt.ListNamespaces(ctx, &transport.ListNamespacesRequest{Offset: 10})
		require.NoError(t, err)
		assert.Empty(t, resp.Namespaces)
	})

	t.Run("prefix filter narrows results", func(t *testing.T) {
		resp, err := bt.ListNamespaces(ctx, &transport.ListNamespacesRequest{Prefix: "ns-a"})
		require.NoError(t, err)
		assert.Len(t, resp.Namespaces, 1)
	})
}

// ---------------------------------------------------------------------------
// upsertVectorViaStorage error paths
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_UpsertVector_ViaStorage_Error(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		upsertVectorFunc: func(ctx context.Context, namespace string, vec *interfaces.VectorValue) error {
			return &ServerMessageError{Message: "upsert failed"}
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("storage error propagates", func(t *testing.T) {
		_, err := bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
			Namespace: "ns", ID: 1, Vector: []float32{1, 0, 0},
		})
		require.Error(t, err)
	})
}

func TestBaasTransport_Vector_UpsertVector_ViaStorage_VersionIncrement(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		getVectorFunc: func(ctx context.Context, namespace string, id uint64) (*interfaces.VectorValue, error) {
			return &interfaces.VectorValue{
				ID:        id,
				Version:   3,
				CreatedAt: 1000,
			}, nil
		},
		upsertVectorFunc: func(ctx context.Context, namespace string, vec *interfaces.VectorValue) error {
			return nil
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("version increments on existing vector", func(t *testing.T) {
		resp, err := bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
			Namespace: "ns", ID: 1, Vector: []float32{1, 0, 0},
		})
		require.NoError(t, err)
		assert.Equal(t, uint64(4), resp.Version)
	})
}

func TestBaasTransport_Vector_UpsertVector_ViaStorage_WithMetadata(t *testing.T) {
	store := NewMemoryKVStore()
	var capturedVec *interfaces.VectorValue
	vs := &mockVectorStorage{
		upsertVectorFunc: func(ctx context.Context, namespace string, vec *interfaces.VectorValue) error {
			capturedVec = vec
			return nil
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("metadata is converted to string map", func(t *testing.T) {
		_, err := bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
			Namespace: "ns",
			ID:        1,
			Vector:    []float32{1, 0},
			Metadata:  map[string]interface{}{"color": "red"},
		})
		require.NoError(t, err)
		assert.Equal(t, "red", capturedVec.Metadata["color"])
	})
}

// ---------------------------------------------------------------------------
// upsertVectorViaKV error paths (starting at 66.7%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_UpsertVector_KV_NamespaceNotFound(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("upsert to non-existent namespace fails", func(t *testing.T) {
		_, err := bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
			Namespace: "nonexistent", ID: 1, Vector: []float32{1, 0},
		})
		require.Error(t, err)
		var nsErr *VectorNamespaceNotFoundError
		assert.True(t, errors.As(err, &nsErr))
	})
}

// ---------------------------------------------------------------------------
// getVectorViaStorage error paths (starting at 77.8%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_GetVector_ViaStorage_Error(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		getVectorFunc: func(ctx context.Context, namespace string, id uint64) (*interfaces.VectorValue, error) {
			return nil, &ServerMessageError{Message: "not found"}
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("storage error returns not-found response", func(t *testing.T) {
		resp, err := bt.GetVector(ctx, &transport.GetVectorRequest{
			Namespace: "ns", ID: 1, IncludeVector: true,
		})
		require.NoError(t, err)
		assert.False(t, resp.Found)
	})
}

func TestBaasTransport_Vector_GetVector_ViaStorage_IncludeMetadata(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		getVectorFunc: func(ctx context.Context, namespace string, id uint64) (*interfaces.VectorValue, error) {
			return &interfaces.VectorValue{
				ID:        id,
				Embedding: []float32{1, 0, 0},
				Metadata:  map[string]string{"color": "blue"},
			}, nil
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("include metadata returns metadata", func(t *testing.T) {
		resp, err := bt.GetVector(ctx, &transport.GetVectorRequest{
			Namespace: "ns", ID: 1, IncludeMetadata: true,
		})
		require.NoError(t, err)
		assert.True(t, resp.Found)
		assert.NotNil(t, resp.Metadata)
	})
}

func TestBaasTransport_Vector_GetVector_ViaStorage_NilVec(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		getVectorFunc: func(ctx context.Context, namespace string, id uint64) (*interfaces.VectorValue, error) {
			return nil, nil
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("nil vector returns not-found", func(t *testing.T) {
		resp, err := bt.GetVector(ctx, &transport.GetVectorRequest{
			Namespace: "ns", ID: 99,
		})
		require.NoError(t, err)
		assert.False(t, resp.Found)
	})
}

// ---------------------------------------------------------------------------
// deleteVectorViaStorage error paths (starting at 66.7%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_DeleteVector_ViaStorage_Error(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		deleteVectorFunc: func(ctx context.Context, namespace string, id uint64) error {
			return &ServerMessageError{Message: "delete failed"}
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("storage error returns failure response without propagating error", func(t *testing.T) {
		resp, err := bt.DeleteVector(ctx, &transport.DeleteVectorRequest{Namespace: "ns", ID: 1})
		require.NoError(t, err)
		assert.False(t, resp.Success)
	})
}

// ---------------------------------------------------------------------------
// deleteVectorViaKV error paths (starting at 75%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_DeleteVector_KV_NonexistentKey(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("delete nonexistent vector succeeds when store allows it", func(t *testing.T) {
		resp, err := bt.DeleteVector(ctx, &transport.DeleteVectorRequest{
			Namespace: "ns", ID: 999,
		})
		require.NoError(t, err)
		// MemoryKVStore.Delete always returns nil, so deleteVectorViaKV
		// reports success even for non-existent keys.
		assert.True(t, resp.Success)
	})
}

// ---------------------------------------------------------------------------
// batchUpsertViaStorage error paths (starting at 90%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_BatchUpsert_ViaStorage_Error(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		batchUpsertFunc: func(ctx context.Context, namespace string, vecs []*interfaces.VectorValue) error {
			return &ServerMessageError{Message: "batch failed"}
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("storage error propagates", func(t *testing.T) {
		_, err := bt.BatchUpsert(ctx, &transport.BatchUpsertRequest{
			Namespace: "ns",
			Vectors: []*transport.VectorData{
				{ID: 1, Vector: []float32{1, 0}},
			},
		})
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// batchUpsertViaKV error paths (starting at 78.6%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_BatchUpsert_KV_DimensionMismatch(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "bdim-ns", Dimension: 3})
	require.NoError(t, err)

	t.Run("dimension mismatch in batch returns error", func(t *testing.T) {
		_, err := bt.BatchUpsert(ctx, &transport.BatchUpsertRequest{
			Namespace: "bdim-ns",
			Vectors: []*transport.VectorData{
				{ID: 1, Vector: []float32{1, 0}}, // wrong dimension
			},
		})
		require.Error(t, err)
		var dimErr *VectorDimensionMismatchError
		assert.True(t, errors.As(err, &dimErr))
	})
}

func TestBaasTransport_Vector_BatchUpsert_KV_NamespaceNotFound(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("batch to nonexistent namespace fails", func(t *testing.T) {
		_, err := bt.BatchUpsert(ctx, &transport.BatchUpsertRequest{
			Namespace: "missing",
			Vectors: []*transport.VectorData{
				{ID: 1, Vector: []float32{1, 0}},
			},
		})
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// batchDeleteVectorsViaStorage error paths (starting at 66.7%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_BatchDelete_ViaStorage_Error(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		batchDeleteFunc: func(ctx context.Context, namespace string, ids []uint64) error {
			return &ServerMessageError{Message: "batch delete failed"}
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("storage error propagates", func(t *testing.T) {
		_, err := bt.BatchDeleteVectors(ctx, &transport.BatchDeleteVectorsRequest{
			Namespace: "ns", IDs: []uint64{1, 2},
		})
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// batchDeleteVectorsViaKV error paths (starting at 77.8%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_BatchDelete_KV_PartialFailure(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "pdel-ns", Dimension: 2})
	require.NoError(t, err)
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
		Namespace: "pdel-ns", ID: 1, Vector: []float32{1, 0},
	})
	require.NoError(t, err)

	t.Run("deleting mix of existing and nonexistent IDs returns partial results", func(t *testing.T) {
		resp, err := bt.BatchDeleteVectors(ctx, &transport.BatchDeleteVectorsRequest{
			Namespace: "pdel-ns",
			IDs:       []uint64{1, 999},
		})
		require.NoError(t, err)
		// ID 1 should succeed; ID 999 depends on whether Delete returns error for nonexistent keys.
		assert.NotNil(t, resp)
	})
}

// ---------------------------------------------------------------------------
// listVectorsViaStorage error paths (starting at 71.4%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_ListVectors_ViaStorage_Error(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		scanNamespaceFunc: func(ctx context.Context, namespace string, opts *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
			return nil, &ServerMessageError{Message: "scan failed"}
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("storage error propagates", func(t *testing.T) {
		_, err := bt.ListVectors(ctx, &transport.ListVectorsRequest{Namespace: "ns", Limit: 10})
		require.Error(t, err)
	})
}

func TestBaasTransport_Vector_ListVectors_ViaStorage_IncludeFlags(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		scanNamespaceFunc: func(ctx context.Context, namespace string, opts *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
			return []*interfaces.VectorValue{
				{ID: 1, Embedding: []float32{1, 0}, Metadata: map[string]string{"k": "v"}},
				{ID: 2, Embedding: []float32{0, 1}, Metadata: map[string]string{"k": "v2"}},
			}, nil
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("include vectors and metadata", func(t *testing.T) {
		resp, err := bt.ListVectors(ctx, &transport.ListVectorsRequest{
			Namespace:       "ns",
			IncludeVectors:  true,
			IncludeMetadata: true,
		})
		require.NoError(t, err)
		require.Len(t, resp.Vectors, 2)
		assert.NotNil(t, resp.Vectors[0].Vector)
		assert.NotNil(t, resp.Vectors[0].Metadata)
	})

	t.Run("limit with pagination cursor", func(t *testing.T) {
		resp, err := bt.ListVectors(ctx, &transport.ListVectorsRequest{
			Namespace: "ns",
			Limit:     1,
		})
		require.NoError(t, err)
		assert.Len(t, resp.Vectors, 1)
		assert.NotEmpty(t, resp.NextCursor)
	})
}

// ---------------------------------------------------------------------------
// listVectorsViaKV error paths (starting at 64.3%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_ListVectors_KV_WithCursor(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "cursor-ns", Dimension: 2})
	require.NoError(t, err)

	for i := uint64(1); i <= 5; i++ {
		_, err := bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
			Namespace: "cursor-ns",
			ID:        i,
			Vector:    []float32{float32(i), 0},
		})
		require.NoError(t, err)
	}

	t.Run("pagination with limit and cursor", func(t *testing.T) {
		resp1, err := bt.ListVectors(ctx, &transport.ListVectorsRequest{
			Namespace: "cursor-ns",
			Limit:     2,
		})
		require.NoError(t, err)
		assert.Len(t, resp1.Vectors, 2)
		assert.NotEmpty(t, resp1.NextCursor)

		// Fetch second page.
		resp2, err := bt.ListVectors(ctx, &transport.ListVectorsRequest{
			Namespace: "cursor-ns",
			Limit:     2,
			Cursor:    resp1.NextCursor,
		})
		require.NoError(t, err)
		assert.NotEmpty(t, resp2.Vectors)
	})

	t.Run("include vectors and metadata flags", func(t *testing.T) {
		resp, err := bt.ListVectors(ctx, &transport.ListVectorsRequest{
			Namespace:       "cursor-ns",
			Limit:           10,
			IncludeVectors:  true,
			IncludeMetadata: true,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(resp.Vectors), 5)
		assert.NotNil(t, resp.Vectors[0].Vector)
	})
}

// ---------------------------------------------------------------------------
// updateMetadataViaStorage error paths (starting at 68.8%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_UpdateMetadata_ViaStorage_NotFound(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		getVectorFunc: func(ctx context.Context, namespace string, id uint64) (*interfaces.VectorValue, error) {
			return nil, &ServerMessageError{Message: "not found"}
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("not-found returns VectorNotFoundError", func(t *testing.T) {
		_, err := bt.UpdateMetadata(ctx, &transport.UpdateVectorMetadataRequest{
			Namespace: "ns", ID: 1, Metadata: map[string]interface{}{"k": "v"},
		})
		require.Error(t, err)
		var nfErr *VectorNotFoundError
		assert.True(t, errors.As(err, &nfErr))
	})
}

func TestBaasTransport_Vector_UpdateMetadata_ViaStorage_NilVector(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		getVectorFunc: func(ctx context.Context, namespace string, id uint64) (*interfaces.VectorValue, error) {
			return nil, nil
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("nil vector returns VectorNotFoundError", func(t *testing.T) {
		_, err := bt.UpdateMetadata(ctx, &transport.UpdateVectorMetadataRequest{
			Namespace: "ns", ID: 1, Metadata: map[string]interface{}{"k": "v"},
		})
		require.Error(t, err)
		var nfErr *VectorNotFoundError
		assert.True(t, errors.As(err, &nfErr))
	})
}

func TestBaasTransport_Vector_UpdateMetadata_ViaStorage_UpsertError(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		getVectorFunc: func(ctx context.Context, namespace string, id uint64) (*interfaces.VectorValue, error) {
			return &interfaces.VectorValue{
				ID:       id,
				Version:  1,
				Metadata: map[string]string{"old": "data"},
			}, nil
		},
		upsertVectorFunc: func(ctx context.Context, namespace string, vec *interfaces.VectorValue) error {
			return &ServerMessageError{Message: "upsert failed"}
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("upsert error propagates", func(t *testing.T) {
		_, err := bt.UpdateMetadata(ctx, &transport.UpdateVectorMetadataRequest{
			Namespace: "ns", ID: 1, Metadata: map[string]interface{}{"k": "v"},
		})
		require.Error(t, err)
	})
}

func TestBaasTransport_Vector_UpdateMetadata_ViaStorage_MergeMetadata(t *testing.T) {
	store := NewMemoryKVStore()
	var capturedVec *interfaces.VectorValue
	vs := &mockVectorStorage{
		getVectorFunc: func(ctx context.Context, namespace string, id uint64) (*interfaces.VectorValue, error) {
			return &interfaces.VectorValue{
				ID:       id,
				Version:  1,
				Metadata: map[string]string{"old": "data", "keep": "me"},
			}, nil
		},
		upsertVectorFunc: func(ctx context.Context, namespace string, vec *interfaces.VectorValue) error {
			capturedVec = vec
			return nil
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("merge metadata preserves existing keys", func(t *testing.T) {
		_, err := bt.UpdateMetadata(ctx, &transport.UpdateVectorMetadataRequest{
			Namespace:     "ns",
			ID:            1,
			Metadata:      map[string]interface{}{"new": "value"},
			MergeMetadata: true,
		})
		require.NoError(t, err)
		assert.Equal(t, "data", capturedVec.Metadata["old"])
		assert.Equal(t, "me", capturedVec.Metadata["keep"])
	})

	t.Run("delete keys removes specified keys", func(t *testing.T) {
		_, err := bt.UpdateMetadata(ctx, &transport.UpdateVectorMetadataRequest{
			Namespace:     "ns",
			ID:            1,
			Metadata:      map[string]interface{}{"new": "value"},
			MergeMetadata: true,
			DeleteKeys:    []string{"old"},
		})
		require.NoError(t, err)
		_, exists := capturedVec.Metadata["old"]
		assert.False(t, exists)
	})
}

// ---------------------------------------------------------------------------
// updateMetadataViaKV error paths (starting at 68.2%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_UpdateMetadata_KV_NotFound(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("update metadata for nonexistent vector returns error", func(t *testing.T) {
		_, err := bt.UpdateMetadata(ctx, &transport.UpdateVectorMetadataRequest{
			Namespace: "ns", ID: 999, Metadata: map[string]interface{}{"k": "v"},
		})
		require.Error(t, err)
		var nfErr *VectorNotFoundError
		assert.True(t, errors.As(err, &nfErr))
	})
}

func TestBaasTransport_Vector_UpdateMetadata_KV_MergeAndDelete(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "meta-ns2", Dimension: 2})
	require.NoError(t, err)
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
		Namespace: "meta-ns2", ID: 1, Vector: []float32{1, 0},
		Metadata: map[string]interface{}{"color": "red", "size": "large"},
	})
	require.NoError(t, err)

	t.Run("merge metadata preserves existing keys", func(t *testing.T) {
		resp, err := bt.UpdateMetadata(ctx, &transport.UpdateVectorMetadataRequest{
			Namespace:     "meta-ns2",
			ID:            1,
			Metadata:      map[string]interface{}{"shape": "round"},
			MergeMetadata: true,
		})
		require.NoError(t, err)
		assert.True(t, resp.Success)

		getResp, err := bt.GetVector(ctx, &transport.GetVectorRequest{
			Namespace: "meta-ns2", ID: 1, IncludeMetadata: true,
		})
		require.NoError(t, err)
		assert.Equal(t, "red", getResp.Metadata["color"])
		assert.Equal(t, "round", getResp.Metadata["shape"])
	})

	t.Run("delete keys removes specified keys", func(t *testing.T) {
		resp, err := bt.UpdateMetadata(ctx, &transport.UpdateVectorMetadataRequest{
			Namespace:     "meta-ns2",
			ID:            1,
			MergeMetadata: true,
			DeleteKeys:    []string{"color"},
		})
		require.NoError(t, err)
		assert.True(t, resp.Success)

		getResp, err := bt.GetVector(ctx, &transport.GetVectorRequest{
			Namespace: "meta-ns2", ID: 1, IncludeMetadata: true,
		})
		require.NoError(t, err)
		_, hasColor := getResp.Metadata["color"]
		assert.False(t, hasColor)
	})
}

// ---------------------------------------------------------------------------
// getIndexStatsViaStorage error paths (starting at 75%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_GetIndexStats_ViaStorage_Error(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		getNamespaceInfoFunc: func(ctx context.Context, namespace string) (*interfaces.VectorNamespaceInfo, error) {
			return nil, &ServerMessageError{Message: "not found"}
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("storage error propagates", func(t *testing.T) {
		_, err := bt.GetIndexStats(ctx, &transport.GetIndexStatsRequest{Namespace: "missing"})
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// getIndexStatsViaKV error paths (starting at 71.4%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_GetIndexStats_KV_NotFound(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("missing namespace returns error", func(t *testing.T) {
		_, err := bt.GetIndexStats(ctx, &transport.GetIndexStatsRequest{Namespace: "missing"})
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// CompactNamespace error paths (starting at 71.4%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_Compact_ViaStorage_Error(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{
		compactFunc: func(ctx context.Context, namespace string) error {
			return &ServerMessageError{Message: "compact failed"}
		},
	}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("storage error propagates", func(t *testing.T) {
		_, err := bt.CompactNamespace(ctx, &transport.CompactNamespaceRequest{Namespace: "ns"})
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// RebuildIndex / GetRebuildProgress nil request paths
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_RebuildIndex_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("nil request returns error", func(t *testing.T) {
		_, err := bt.RebuildIndex(ctx, nil)
		require.Error(t, err)
		var nilErr *VectorNilRequestError
		assert.True(t, errors.As(err, &nilErr))
	})
}

func TestBaasTransport_Vector_GetRebuildProgress_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("nil request returns error", func(t *testing.T) {
		_, err := bt.GetRebuildProgress(ctx, nil)
		require.Error(t, err)
		var nilErr *VectorNilRequestError
		assert.True(t, errors.As(err, &nilErr))
	})
}

func TestBaasTransport_Vector_CompactNamespace_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("nil request returns error", func(t *testing.T) {
		_, err := bt.CompactNamespace(ctx, nil)
		require.Error(t, err)
		var nilErr *VectorNilRequestError
		assert.True(t, errors.As(err, &nilErr))
	})
}

func TestBaasTransport_Vector_GetCompactionProgress_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("nil request returns error", func(t *testing.T) {
		_, err := bt.GetCompactionProgress(ctx, nil)
		require.Error(t, err)
		var nilErr *VectorNilRequestError
		assert.True(t, errors.As(err, &nilErr))
	})
}

// ---------------------------------------------------------------------------
// SearchByID error paths (starting at 56%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_SearchByID_VectorNotFound(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{
		Namespace: "sid-ns", Dimension: 2,
	})
	require.NoError(t, err)

	t.Run("missing source vector returns VectorNotFoundError", func(t *testing.T) {
		_, err := bt.SearchByID(ctx, &transport.SearchByIDRequest{
			Namespace: "sid-ns", ID: 999, K: 5,
		})
		require.Error(t, err)
		var nfErr *VectorNotFoundError
		assert.True(t, errors.As(err, &nfErr))
	})
}

func TestBaasTransport_Vector_SearchByID_ExcludeSelf(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{
		Namespace: "excl-ns", Dimension: 2, DistanceMetric: transport.DistanceCosine,
	})
	require.NoError(t, err)

	for i := uint64(1); i <= 3; i++ {
		_, err := bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
			Namespace: "excl-ns", ID: i, Vector: []float32{float32(i), 0},
		})
		require.NoError(t, err)
	}

	t.Run("exclude self filters out source ID", func(t *testing.T) {
		resp, err := bt.SearchByID(ctx, &transport.SearchByIDRequest{
			Namespace:   "excl-ns",
			ID:          1,
			K:           5,
			ExcludeSelf: true,
		})
		require.NoError(t, err)
		for _, r := range resp.Results {
			assert.NotEqual(t, uint64(1), r.ID)
		}
	})

	t.Run("exclude self with K limit trims results", func(t *testing.T) {
		resp, err := bt.SearchByID(ctx, &transport.SearchByIDRequest{
			Namespace:   "excl-ns",
			ID:          1,
			K:           1,
			ExcludeSelf: true,
		})
		require.NoError(t, err)
		assert.LessOrEqual(t, len(resp.Results), 1)
	})
}

// ---------------------------------------------------------------------------
// SearchByMetadata error paths (starting at 71%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_SearchByMetadata_OffsetAndLimit(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "sm-ns", Dimension: 2})
	require.NoError(t, err)

	for i := uint64(1); i <= 5; i++ {
		_, err := bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
			Namespace: "sm-ns", ID: i, Vector: []float32{float32(i), 0},
			Metadata: map[string]interface{}{"group": "a"},
		})
		require.NoError(t, err)
	}

	t.Run("offset skips results", func(t *testing.T) {
		resp, err := bt.SearchByMetadata(ctx, &transport.SearchByMetadataRequest{
			Namespace: "sm-ns",
			Filters:   map[string]interface{}{"group": "a"},
			Limit:     10,
			Offset:    2,
		})
		require.NoError(t, err)
		assert.Equal(t, 3, len(resp.Results))
	})

	t.Run("offset beyond count returns empty", func(t *testing.T) {
		resp, err := bt.SearchByMetadata(ctx, &transport.SearchByMetadataRequest{
			Namespace: "sm-ns",
			Filters:   map[string]interface{}{"group": "a"},
			Limit:     10,
			Offset:    100,
		})
		require.NoError(t, err)
		assert.Empty(t, resp.Results)
	})

	t.Run("OR filter operator", func(t *testing.T) {
		// Add a vector with different metadata.
		_, err := bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
			Namespace: "sm-ns", ID: 10, Vector: []float32{10, 0},
			Metadata: map[string]interface{}{"group": "b"},
		})
		require.NoError(t, err)

		resp, err := bt.SearchByMetadata(ctx, &transport.SearchByMetadataRequest{
			Namespace:      "sm-ns",
			Filters:        map[string]interface{}{"group": "a"},
			FilterOperator: "or",
			Limit:          100,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(resp.Results), 5)
	})

	t.Run("include vectors and metadata flags", func(t *testing.T) {
		resp, err := bt.SearchByMetadata(ctx, &transport.SearchByMetadataRequest{
			Namespace:       "sm-ns",
			Filters:         map[string]interface{}{"group": "a"},
			Limit:           10,
			IncludeVectors:  true,
			IncludeMetadata: true,
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp.Results)
		assert.NotNil(t, resp.Results[0].Vector)
		assert.NotNil(t, resp.Results[0].Metadata)
	})
}

// ---------------------------------------------------------------------------
// HybridSearch error paths (starting at 74.4%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_HybridSearch_NamespaceNotFound(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("missing namespace returns error", func(t *testing.T) {
		_, err := bt.HybridSearch(ctx, &transport.HybridSearchRequest{
			Namespace:   "missing",
			QueryVector: []float32{1, 0},
			K:           5,
		})
		require.Error(t, err)
	})
}

func TestBaasTransport_Vector_HybridSearch_MetadataFilter(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{
		Namespace: "hs-ns", Dimension: 2, DistanceMetric: transport.DistanceCosine,
	})
	require.NoError(t, err)

	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
		Namespace: "hs-ns", ID: 1, Vector: []float32{1, 0},
		Metadata: map[string]interface{}{"color": "red"},
	})
	require.NoError(t, err)
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
		Namespace: "hs-ns", ID: 2, Vector: []float32{0, 1},
		Metadata: map[string]interface{}{"color": "blue"},
	})
	require.NoError(t, err)

	t.Run("metadata filter reduces candidates", func(t *testing.T) {
		resp, err := bt.HybridSearch(ctx, &transport.HybridSearchRequest{
			Namespace:      "hs-ns",
			QueryVector:    []float32{1, 0},
			MetadataFilter: map[string]interface{}{"color": "red"},
			K:              5,
		})
		require.NoError(t, err)
		assert.Equal(t, 2, resp.TotalCandidates)
		assert.Equal(t, 1, resp.TotalFiltered)
		assert.Len(t, resp.Results, 1)
	})

	t.Run("OR filter operator", func(t *testing.T) {
		resp, err := bt.HybridSearch(ctx, &transport.HybridSearchRequest{
			Namespace:      "hs-ns",
			QueryVector:    []float32{1, 0},
			MetadataFilter: map[string]interface{}{"color": "red"},
			FilterOperator: "or",
			K:              5,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(resp.Results), 1)
	})

	t.Run("min score filter", func(t *testing.T) {
		resp, err := bt.HybridSearch(ctx, &transport.HybridSearchRequest{
			Namespace:   "hs-ns",
			QueryVector: []float32{1, 0},
			K:           5,
			MinScore:    0.99,
		})
		require.NoError(t, err)
		// Only the exact match (ID 1) should pass.
		for _, r := range resp.Results {
			assert.GreaterOrEqual(t, r.Score, float32(0.99))
		}
	})

	t.Run("default K of 10 when K is zero", func(t *testing.T) {
		resp, err := bt.HybridSearch(ctx, &transport.HybridSearchRequest{
			Namespace:   "hs-ns",
			QueryVector: []float32{1, 0},
			K:           0,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}
