//go:build vector && !integration

package server

import (
	"context"
	"testing"

	qrdbvector "github.com/jeremyhahn/go-qrdb/pkg/storage/vector"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// createTestVectorStorage creates a PebbleDB-backed vector storage adapter
// using a temp directory for unit testing.
func createTestVectorStorage(t *testing.T) interfaces.VectorStorage {
	t.Helper()
	log := mustCreateLogger()
	qrdbLogger := newQRDBLoggerAdapter(log)
	storageConfig := qrdbvector.DefaultConfig(t.TempDir())
	pebbleStorage, err := qrdbvector.NewPebbleVectorStorage(storageConfig, qrdbLogger)
	require.NoError(t, err)
	t.Cleanup(func() {
		pebbleStorage.Close()
	})
	return newVectorStorageAdapter(pebbleStorage)
}

// ---------------------------------------------------------------------------
// Namespace operations
// ---------------------------------------------------------------------------

func TestVectorStorageAdapter_NamespaceLifecycle(t *testing.T) {
	storage := createTestVectorStorage(t)
	ctx := context.Background()

	t.Run("create namespace", func(t *testing.T) {
		info := &interfaces.VectorNamespaceInfo{
			Name:           "test-ns",
			Dimension:      4,
			DistanceMetric: "l2",
			IndexType:      "flat",
			TenantID:       "t1",
		}
		err := storage.CreateNamespace(ctx, info)
		require.NoError(t, err)
	})

	t.Run("get namespace info", func(t *testing.T) {
		info, err := storage.GetNamespaceInfo(ctx, "test-ns")
		require.NoError(t, err)
		require.NotNil(t, info)
		assert.Equal(t, "test-ns", info.Name)
		assert.Equal(t, uint32(4), info.Dimension)
	})

	t.Run("list namespaces", func(t *testing.T) {
		list, err := storage.ListNamespaces(ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, list)
	})

	t.Run("get namespace info non-existent", func(t *testing.T) {
		_, err := storage.GetNamespaceInfo(ctx, "no-such-ns")
		require.Error(t, err)
	})

	t.Run("delete namespace", func(t *testing.T) {
		err := storage.DeleteNamespace(ctx, "test-ns")
		require.NoError(t, err)
	})
}

// ---------------------------------------------------------------------------
// Vector CRUD operations
// ---------------------------------------------------------------------------

func TestVectorStorageAdapter_VectorCRUD(t *testing.T) {
	storage := createTestVectorStorage(t)
	ctx := context.Background()

	// Create a namespace first
	require.NoError(t, storage.CreateNamespace(ctx, &interfaces.VectorNamespaceInfo{
		Name:           "vec-ns",
		Dimension:      3,
		DistanceMetric: "l2",
		IndexType:      "flat",
	}))

	t.Run("upsert vector", func(t *testing.T) {
		vec := &interfaces.VectorValue{
			ID:        1,
			Embedding: []float32{1.0, 2.0, 3.0},
			Metadata:  map[string]string{"key": "val"},
		}
		err := storage.UpsertVector(ctx, "vec-ns", vec)
		require.NoError(t, err)
	})

	t.Run("get vector", func(t *testing.T) {
		vec, err := storage.GetVector(ctx, "vec-ns", 1)
		require.NoError(t, err)
		require.NotNil(t, vec)
		assert.Equal(t, uint64(1), vec.ID)
		assert.Equal(t, []float32{1.0, 2.0, 3.0}, vec.Embedding)
	})

	t.Run("batch upsert vectors", func(t *testing.T) {
		vecs := []*interfaces.VectorValue{
			{ID: 2, Embedding: []float32{4.0, 5.0, 6.0}},
			{ID: 3, Embedding: []float32{7.0, 8.0, 9.0}},
		}
		err := storage.BatchUpsertVectors(ctx, "vec-ns", vecs)
		require.NoError(t, err)
	})

	t.Run("scan namespace", func(t *testing.T) {
		vecs, err := storage.ScanNamespace(ctx, "vec-ns", nil)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(vecs), 3)
	})

	t.Run("scan namespace with options", func(t *testing.T) {
		opts := &interfaces.VectorScanOptions{Limit: 2, Offset: 0}
		vecs, err := storage.ScanNamespace(ctx, "vec-ns", opts)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(vecs), 2)
	})

	t.Run("delete vector", func(t *testing.T) {
		err := storage.DeleteVector(ctx, "vec-ns", 1)
		require.NoError(t, err)
	})

	t.Run("batch delete vectors", func(t *testing.T) {
		err := storage.BatchDeleteVectors(ctx, "vec-ns", []uint64{2, 3})
		require.NoError(t, err)
	})

	t.Run("get stats", func(t *testing.T) {
		stats, err := storage.GetStats(ctx, "vec-ns")
		require.NoError(t, err)
		require.NotNil(t, stats)
	})

	t.Run("compact", func(t *testing.T) {
		err := storage.Compact(ctx, "vec-ns")
		require.NoError(t, err)
	})
}

// ---------------------------------------------------------------------------
// ScanByMultipleFilters
// ---------------------------------------------------------------------------

func TestVectorStorageAdapter_ScanByMultipleFilters(t *testing.T) {
	storage := createTestVectorStorage(t)
	ctx := context.Background()

	require.NoError(t, storage.CreateNamespace(ctx, &interfaces.VectorNamespaceInfo{
		Name:      "filter-ns",
		Dimension: 2,
		IndexType: "flat",
	}))

	require.NoError(t, storage.UpsertVector(ctx, "filter-ns", &interfaces.VectorValue{
		ID:        1,
		Embedding: []float32{1.0, 2.0},
		Metadata:  map[string]string{"env": "prod"},
	}))

	t.Run("scan with filters", func(t *testing.T) {
		filters := []interfaces.VectorMetadataFilter{
			{Field: "env", Value: "prod", Operator: interfaces.VectorFilterEqual},
		}
		vecs, err := storage.ScanByMultipleFilters(ctx, "filter-ns", filters, nil)
		require.NoError(t, err)
		_ = vecs
	})
}
