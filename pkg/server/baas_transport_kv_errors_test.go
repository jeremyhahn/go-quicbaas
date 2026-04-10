//go:build !integration

package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingKVStore wraps MemoryKVStore but can selectively fail operations.
type failingKVStore struct {
	*MemoryKVStore
	failPut    bool
	failGet    bool
	failDelete bool
	failScan   bool
	failExists bool
	failList   bool
}

func newFailingKVStore() *failingKVStore {
	return &failingKVStore{MemoryKVStore: NewMemoryKVStore()}
}

func (f *failingKVStore) Put(ctx context.Context, key string, value []byte) error {
	if f.failPut {
		return &ServerMessageError{Message: "put failed"}
	}
	return f.MemoryKVStore.Put(ctx, key, value)
}

func (f *failingKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	if f.failGet {
		return nil, &ServerMessageError{Message: "get failed"}
	}
	return f.MemoryKVStore.Get(ctx, key)
}

func (f *failingKVStore) Delete(ctx context.Context, key string) error {
	if f.failDelete {
		return &ServerMessageError{Message: "delete failed"}
	}
	return f.MemoryKVStore.Delete(ctx, key)
}

func (f *failingKVStore) Scan(ctx context.Context, prefix string, fn func(string, []byte) error) error {
	if f.failScan {
		return &ServerMessageError{Message: "scan failed"}
	}
	return f.MemoryKVStore.Scan(ctx, prefix, fn)
}

func (f *failingKVStore) Exists(ctx context.Context, key string) (bool, error) {
	if f.failExists {
		return false, &ServerMessageError{Message: "exists failed"}
	}
	return f.MemoryKVStore.Exists(ctx, key)
}

func (f *failingKVStore) List(ctx context.Context, prefix string) ([]string, error) {
	if f.failList {
		return nil, &ServerMessageError{Message: "list failed"}
	}
	return f.MemoryKVStore.List(ctx, prefix)
}

// ---------------------------------------------------------------------------
// createNamespaceViaKV error paths (78.6%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_CreateNamespace_KV_ExistsError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("exists check error propagates", func(t *testing.T) {
		store.failExists = true
		_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{
			Namespace: "ns", Dimension: 3,
		})
		require.Error(t, err)
		store.failExists = false
	})
}

func TestBaasTransport_Vector_CreateNamespace_KV_PutError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("put error propagates", func(t *testing.T) {
		store.failPut = true
		_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{
			Namespace: "ns", Dimension: 3,
		})
		require.Error(t, err)
		store.failPut = false
	})
}

// ---------------------------------------------------------------------------
// deleteNamespaceViaKV error paths (77.8%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_DeleteNamespace_KV_ExistsError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("exists check error propagates", func(t *testing.T) {
		store.failExists = true
		_, err := bt.DeleteNamespace(ctx, &transport.DeleteNamespaceRequest{Namespace: "ns"})
		require.Error(t, err)
		store.failExists = false
	})
}

func TestBaasTransport_Vector_DeleteNamespace_KV_ScanError2(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	// Create namespace first.
	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "del-scan-ns", Dimension: 2})
	require.NoError(t, err)

	t.Run("scan error propagates during vector enumeration", func(t *testing.T) {
		store.failScan = true
		_, err := bt.DeleteNamespace(ctx, &transport.DeleteNamespaceRequest{Namespace: "del-scan-ns"})
		require.Error(t, err)
		store.failScan = false
	})
}

func TestBaasTransport_Vector_DeleteNamespace_KV_DeleteVectorError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	// Create namespace and add a vector.
	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "del-vec-ns", Dimension: 2})
	require.NoError(t, err)
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
		Namespace: "del-vec-ns", ID: 1, Vector: []float32{1, 0},
	})
	require.NoError(t, err)

	t.Run("delete error during vector removal propagates", func(t *testing.T) {
		store.failDelete = true
		_, err := bt.DeleteNamespace(ctx, &transport.DeleteNamespaceRequest{Namespace: "del-vec-ns"})
		require.Error(t, err)
		store.failDelete = false
	})
}

// ---------------------------------------------------------------------------
// upsertVectorViaKV error paths (71.4%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_UpsertVector_KV_PutError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	// Create namespace first.
	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "put-ns", Dimension: 2})
	require.NoError(t, err)

	t.Run("put error propagates", func(t *testing.T) {
		store.failPut = true
		_, err := bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
			Namespace: "put-ns", ID: 1, Vector: []float32{1, 0},
		})
		require.Error(t, err)
		store.failPut = false
	})
}

// ---------------------------------------------------------------------------
// deleteVectorViaKV error paths (75%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_DeleteVector_KV_DeleteError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("delete error returns failure response", func(t *testing.T) {
		store.failDelete = true
		resp, err := bt.DeleteVector(ctx, &transport.DeleteVectorRequest{
			Namespace: "ns", ID: 1,
		})
		require.NoError(t, err)
		assert.False(t, resp.Success)
		store.failDelete = false
	})
}

// ---------------------------------------------------------------------------
// batchDeleteVectorsViaKV error paths (77.8%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_BatchDelete_KV_DeleteError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("delete errors result in failed IDs", func(t *testing.T) {
		store.failDelete = true
		resp, err := bt.BatchDeleteVectors(ctx, &transport.BatchDeleteVectorsRequest{
			Namespace: "ns", IDs: []uint64{1, 2, 3},
		})
		require.NoError(t, err)
		assert.Equal(t, 0, resp.DeletedCount)
		assert.Len(t, resp.FailedIDs, 3)
		assert.False(t, resp.Success)
		store.failDelete = false
	})
}

// ---------------------------------------------------------------------------
// tsDeleteSeriesViaStore error paths (78.3%)
// ---------------------------------------------------------------------------

func TestBaasTransport_TSDeleteSeries_KV_ScanError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("scan error propagates", func(t *testing.T) {
		store.failScan = true
		_, err := bt.TSDeleteSeries(ctx, &transport.TSDeleteRequest{
			Metric:  "cpu",
			StartNs: 1000,
			EndNs:   2000,
		})
		require.Error(t, err)
		store.failScan = false
	})
}

func TestBaasTransport_TSDeleteSeries_KV_DeleteError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	now := time.Now().UnixNano()
	require.NoError(t, bt.TSWrite(ctx, &transport.TSDataPoint{
		Metric: "del.err", Value: 1, TimestampNs: now,
	}))

	t.Run("delete error propagates", func(t *testing.T) {
		store.failDelete = true
		_, err := bt.TSDeleteSeries(ctx, &transport.TSDeleteRequest{
			Metric:  "del.err",
			StartNs: now - int64(time.Second),
			EndNs:   now + int64(time.Second),
		})
		require.Error(t, err)
		store.failDelete = false
	})
}

// ---------------------------------------------------------------------------
// listVectorsViaKV scan error path
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_ListVectors_KV_ScanError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("scan error propagates", func(t *testing.T) {
		store.failScan = true
		_, err := bt.ListVectors(ctx, &transport.ListVectorsRequest{
			Namespace: "ns", Limit: 10,
		})
		require.Error(t, err)
		store.failScan = false
	})
}

// ---------------------------------------------------------------------------
// listNamespacesViaKV scan error path
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_ListNamespaces_KV_ScanError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("scan error propagates", func(t *testing.T) {
		store.failScan = true
		_, err := bt.ListNamespaces(ctx, &transport.ListNamespacesRequest{})
		require.Error(t, err)
		store.failScan = false
	})
}

// ---------------------------------------------------------------------------
// updateMetadataViaKV put error path
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_UpdateMetadata_KV_PutError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	// Create namespace and vector.
	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "upm-ns", Dimension: 2})
	require.NoError(t, err)
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
		Namespace: "upm-ns", ID: 1, Vector: []float32{1, 0},
	})
	require.NoError(t, err)

	t.Run("put error propagates", func(t *testing.T) {
		store.failPut = true
		_, err := bt.UpdateMetadata(ctx, &transport.UpdateVectorMetadataRequest{
			Namespace: "upm-ns", ID: 1, Metadata: map[string]interface{}{"k": "v"},
		})
		require.Error(t, err)
		store.failPut = false
	})
}

// ---------------------------------------------------------------------------
// tsQueryViaStore scan error path
// ---------------------------------------------------------------------------

func TestBaasTransport_TSQuery_KV_ScanError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("scan error propagates", func(t *testing.T) {
		store.failScan = true
		_, err := bt.TSQuery(ctx, &transport.TSQueryRequest{
			Metric: "cpu", StartNs: 1000, EndNs: 2000,
		})
		require.Error(t, err)
		store.failScan = false
	})
}

// ---------------------------------------------------------------------------
// tsWritePointToStore put error path
// ---------------------------------------------------------------------------

func TestBaasTransport_TSWritePoint_KV_PutError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("put error propagates", func(t *testing.T) {
		store.failPut = true
		err := bt.TSWritePoint(ctx, &transport.TSDataPoint{
			Metric: "cpu", Value: 42, TimestampNs: time.Now().UnixNano(),
		})
		require.Error(t, err)
		store.failPut = false
	})
}

// ---------------------------------------------------------------------------
// tsSetRetentionViaStore put/delete error paths
// ---------------------------------------------------------------------------

func TestBaasTransport_TSSetRetention_KV_PutError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("put error propagates", func(t *testing.T) {
		store.failPut = true
		err := bt.TSSetRetention(ctx, &transport.TSSetRetentionRequest{
			Metric: "cpu",
			Policy: &transport.TSRetentionPolicy{DurationNs: int64(24 * time.Hour)},
		})
		require.Error(t, err)
		store.failPut = false
	})
}

func TestBaasTransport_TSSetRetention_KV_DeleteError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("delete error propagates when nil policy", func(t *testing.T) {
		store.failDelete = true
		err := bt.TSSetRetention(ctx, &transport.TSSetRetentionRequest{
			Metric: "cpu",
			Policy: nil,
		})
		require.Error(t, err)
		store.failDelete = false
	})
}

// ---------------------------------------------------------------------------
// tsWriteBatch KV error path
// ---------------------------------------------------------------------------

func TestBaasTransport_TSWriteBatch_KV_Error(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("write error on individual point propagates", func(t *testing.T) {
		store.failPut = true
		err := bt.TSWriteBatch(ctx, []transport.TSDataPoint{
			{Metric: "cpu", Value: 10, TimestampNs: 1000},
		})
		require.Error(t, err)
		store.failPut = false
	})
}

// ---------------------------------------------------------------------------
// getNamespaceInfoViaKV error paths
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_GetNamespaceInfo_KV_GetError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("get error propagates as namespace not found", func(t *testing.T) {
		store.failGet = true
		_, err := bt.GetNamespaceInfo(ctx, &transport.GetNamespaceInfoRequest{Namespace: "ns"})
		require.Error(t, err)
		var nfErr *VectorNamespaceNotFoundError
		assert.True(t, errors.As(err, &nfErr))
		store.failGet = false
	})
}

// ---------------------------------------------------------------------------
// SearchSimilar error paths
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_SearchSimilar_KV_ScanError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	// Create namespace first.
	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{
		Namespace: "search-err-ns", Dimension: 2,
	})
	require.NoError(t, err)

	t.Run("scan error propagates", func(t *testing.T) {
		store.failScan = true
		_, err := bt.SearchSimilar(ctx, &transport.SearchSimilarRequest{
			Namespace: "search-err-ns", QueryVector: []float32{1, 0}, K: 5,
		})
		require.Error(t, err)
		store.failScan = false
	})
}

// ---------------------------------------------------------------------------
// HybridSearch scan error path
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_HybridSearch_KV_ScanError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{
		Namespace: "hs-err-ns", Dimension: 2,
	})
	require.NoError(t, err)

	t.Run("scan error propagates", func(t *testing.T) {
		store.failScan = true
		_, err := bt.HybridSearch(ctx, &transport.HybridSearchRequest{
			Namespace: "hs-err-ns", QueryVector: []float32{1, 0}, K: 5,
		})
		require.Error(t, err)
		store.failScan = false
	})
}

// ---------------------------------------------------------------------------
// SearchByMetadata scan error path
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_SearchByMetadata_KV_ScanError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("scan error propagates", func(t *testing.T) {
		store.failScan = true
		_, err := bt.SearchByMetadata(ctx, &transport.SearchByMetadataRequest{
			Namespace: "ns",
			Filters:   map[string]interface{}{"k": "v"},
			Limit:     10,
		})
		require.Error(t, err)
		store.failScan = false
	})
}

// ---------------------------------------------------------------------------
// getIndexStatsViaKV error paths
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_GetIndexStats_KV_CountError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	// Create namespace first.
	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{
		Namespace: "stats-err-ns", Dimension: 2,
	})
	require.NoError(t, err)

	t.Run("scan error during count propagates", func(t *testing.T) {
		store.failScan = true
		_, err := bt.GetIndexStats(ctx, &transport.GetIndexStatsRequest{Namespace: "stats-err-ns"})
		require.Error(t, err)
		store.failScan = false
	})
}
