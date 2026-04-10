//go:build !integration

package server

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// mockVectorStorage implements interfaces.VectorStorage for testing.
type mockVectorStorage struct {
	createNamespaceFunc   func(ctx context.Context, info *interfaces.VectorNamespaceInfo) error
	deleteNamespaceFunc   func(ctx context.Context, namespace string) error
	listNamespacesFunc    func(ctx context.Context) ([]interfaces.VectorNamespaceInfo, error)
	getNamespaceInfoFunc  func(ctx context.Context, namespace string) (*interfaces.VectorNamespaceInfo, error)
	upsertVectorFunc      func(ctx context.Context, namespace string, vec *interfaces.VectorValue) error
	getVectorFunc         func(ctx context.Context, namespace string, id uint64) (*interfaces.VectorValue, error)
	deleteVectorFunc      func(ctx context.Context, namespace string, id uint64) error
	batchUpsertFunc       func(ctx context.Context, namespace string, vecs []*interfaces.VectorValue) error
	batchDeleteFunc       func(ctx context.Context, namespace string, ids []uint64) error
	scanNamespaceFunc     func(ctx context.Context, namespace string, opts *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error)
	scanByMultiFilterFunc func(ctx context.Context, namespace string, filters []interfaces.VectorMetadataFilter, opts *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error)
	getStatsFunc          func(ctx context.Context, namespace string) (*interfaces.VectorStorageStats, error)
	compactFunc           func(ctx context.Context, namespace string) error
	closeFunc             func() error
}

func (m *mockVectorStorage) CreateNamespace(ctx context.Context, info *interfaces.VectorNamespaceInfo) error {
	if m.createNamespaceFunc != nil {
		return m.createNamespaceFunc(ctx, info)
	}
	return nil
}

func (m *mockVectorStorage) DeleteNamespace(ctx context.Context, namespace string) error {
	if m.deleteNamespaceFunc != nil {
		return m.deleteNamespaceFunc(ctx, namespace)
	}
	return nil
}

func (m *mockVectorStorage) ListNamespaces(ctx context.Context) ([]interfaces.VectorNamespaceInfo, error) {
	if m.listNamespacesFunc != nil {
		return m.listNamespacesFunc(ctx)
	}
	return []interfaces.VectorNamespaceInfo{{Name: "ns1", Dimension: 128}}, nil
}

func (m *mockVectorStorage) GetNamespaceInfo(ctx context.Context, namespace string) (*interfaces.VectorNamespaceInfo, error) {
	if m.getNamespaceInfoFunc != nil {
		return m.getNamespaceInfoFunc(ctx, namespace)
	}
	return &interfaces.VectorNamespaceInfo{Name: namespace, Dimension: 128}, nil
}

func (m *mockVectorStorage) UpsertVector(ctx context.Context, namespace string, vec *interfaces.VectorValue) error {
	if m.upsertVectorFunc != nil {
		return m.upsertVectorFunc(ctx, namespace, vec)
	}
	return nil
}

func (m *mockVectorStorage) GetVector(ctx context.Context, namespace string, id uint64) (*interfaces.VectorValue, error) {
	if m.getVectorFunc != nil {
		return m.getVectorFunc(ctx, namespace, id)
	}
	return &interfaces.VectorValue{ID: id, Embedding: []float32{1, 0, 0}}, nil
}

func (m *mockVectorStorage) DeleteVector(ctx context.Context, namespace string, id uint64) error {
	if m.deleteVectorFunc != nil {
		return m.deleteVectorFunc(ctx, namespace, id)
	}
	return nil
}

func (m *mockVectorStorage) BatchUpsertVectors(ctx context.Context, namespace string, vecs []*interfaces.VectorValue) error {
	if m.batchUpsertFunc != nil {
		return m.batchUpsertFunc(ctx, namespace, vecs)
	}
	return nil
}

func (m *mockVectorStorage) BatchDeleteVectors(ctx context.Context, namespace string, ids []uint64) error {
	if m.batchDeleteFunc != nil {
		return m.batchDeleteFunc(ctx, namespace, ids)
	}
	return nil
}

func (m *mockVectorStorage) ScanNamespace(ctx context.Context, namespace string, opts *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
	if m.scanNamespaceFunc != nil {
		return m.scanNamespaceFunc(ctx, namespace, opts)
	}
	return []*interfaces.VectorValue{
		{ID: 1, Embedding: []float32{1, 0}},
	}, nil
}

func (m *mockVectorStorage) ScanByMultipleFilters(ctx context.Context, namespace string, filters []interfaces.VectorMetadataFilter, opts *interfaces.VectorScanOptions) ([]*interfaces.VectorValue, error) {
	if m.scanByMultiFilterFunc != nil {
		return m.scanByMultiFilterFunc(ctx, namespace, filters, opts)
	}
	return []*interfaces.VectorValue{}, nil
}

func (m *mockVectorStorage) GetStats(ctx context.Context, namespace string) (*interfaces.VectorStorageStats, error) {
	if m.getStatsFunc != nil {
		return m.getStatsFunc(ctx, namespace)
	}
	return &interfaces.VectorStorageStats{VectorCount: 10}, nil
}

func (m *mockVectorStorage) Compact(ctx context.Context, namespace string) error {
	if m.compactFunc != nil {
		return m.compactFunc(ctx, namespace)
	}
	return nil
}

func (m *mockVectorStorage) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tests with VectorStorage delegation
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_CreateNamespace_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("creates namespace via storage", func(t *testing.T) {
		var called bool
		vs.createNamespaceFunc = func(ctx context.Context, info *interfaces.VectorNamespaceInfo) error {
			called = true
			return nil
		}
		resp, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{
			Namespace: "vs-ns", Dimension: 128,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, called)
	})
}

func TestBaasTransport_Vector_DeleteNamespace_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("deletes namespace via storage", func(t *testing.T) {
		var called bool
		vs.deleteNamespaceFunc = func(ctx context.Context, namespace string) error {
			called = true
			return nil
		}
		resp, err := bt.DeleteNamespace(ctx, &transport.DeleteNamespaceRequest{Namespace: "vs-ns"})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, called)
	})
}

func TestBaasTransport_Vector_ListNamespaces_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("lists namespaces via storage", func(t *testing.T) {
		resp, err := bt.ListNamespaces(ctx, &transport.ListNamespacesRequest{})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(resp.Namespaces), 1)
	})
}

func TestBaasTransport_Vector_GetNamespaceInfo_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("gets namespace info via storage", func(t *testing.T) {
		resp, err := bt.GetNamespaceInfo(ctx, &transport.GetNamespaceInfoRequest{Namespace: "vs-ns"})
		require.NoError(t, err)
		assert.Equal(t, "vs-ns", resp.Namespace)
	})
}

func TestBaasTransport_Vector_UpsertVector_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("upserts vector via storage", func(t *testing.T) {
		var called bool
		vs.upsertVectorFunc = func(ctx context.Context, namespace string, vec *interfaces.VectorValue) error {
			called = true
			return nil
		}
		resp, err := bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
			Namespace: "vs-ns", ID: 1, Vector: []float32{1, 0, 0},
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, called)
	})
}

func TestBaasTransport_Vector_GetVector_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("gets vector via storage", func(t *testing.T) {
		resp, err := bt.GetVector(ctx, &transport.GetVectorRequest{
			Namespace: "vs-ns", ID: 1, IncludeVector: true,
		})
		require.NoError(t, err)
		assert.True(t, resp.Found)
	})
}

func TestBaasTransport_Vector_DeleteVector_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("deletes vector via storage", func(t *testing.T) {
		var called bool
		vs.deleteVectorFunc = func(ctx context.Context, namespace string, id uint64) error {
			called = true
			return nil
		}
		resp, err := bt.DeleteVector(ctx, &transport.DeleteVectorRequest{Namespace: "vs-ns", ID: 1})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, called)
	})
}

func TestBaasTransport_Vector_BatchUpsert_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("batch upserts via storage", func(t *testing.T) {
		var called bool
		vs.batchUpsertFunc = func(ctx context.Context, namespace string, vecs []*interfaces.VectorValue) error {
			called = true
			return nil
		}
		resp, err := bt.BatchUpsert(ctx, &transport.BatchUpsertRequest{
			Namespace: "vs-ns",
			Vectors: []*transport.VectorData{
				{ID: 1, Vector: []float32{1, 0}},
			},
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, called)
	})
}

func TestBaasTransport_Vector_BatchDelete_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("batch deletes via storage", func(t *testing.T) {
		var called bool
		vs.batchDeleteFunc = func(ctx context.Context, namespace string, ids []uint64) error {
			called = true
			return nil
		}
		resp, err := bt.BatchDeleteVectors(ctx, &transport.BatchDeleteVectorsRequest{
			Namespace: "vs-ns", IDs: []uint64{1, 2},
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, called)
	})
}

func TestBaasTransport_Vector_ListVectors_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("lists vectors via storage", func(t *testing.T) {
		resp, err := bt.ListVectors(ctx, &transport.ListVectorsRequest{Namespace: "vs-ns", Limit: 100})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_Vector_UpdateMetadata_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("updates metadata via storage", func(t *testing.T) {
		resp, err := bt.UpdateMetadata(ctx, &transport.UpdateVectorMetadataRequest{
			Namespace: "vs-ns", ID: 1, Metadata: map[string]interface{}{"color": "blue"},
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_Vector_GetIndexStats_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("gets stats via storage", func(t *testing.T) {
		resp, err := bt.GetIndexStats(ctx, &transport.GetIndexStatsRequest{Namespace: "vs-ns"})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_Vector_Compact_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	vs := &mockVectorStorage{}
	bt := newBaasTransport(store, vs, nil, nil, nil)
	ctx := context.Background()

	t.Run("compacts via storage", func(t *testing.T) {
		var called bool
		vs.compactFunc = func(ctx context.Context, namespace string) error {
			called = true
			return nil
		}
		resp, err := bt.CompactNamespace(ctx, &transport.CompactNamespaceRequest{Namespace: "vs-ns"})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, called)
	})
}
