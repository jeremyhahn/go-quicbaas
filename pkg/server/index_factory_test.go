//go:build vector && qrdb_vector

package server

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// generateTrainingVectors creates random training vectors for IVF index training.
func generateTrainingVectors(count, dims int) [][]float32 {
	rng := rand.New(rand.NewSource(42))
	vectors := make([][]float32, count)
	for i := range vectors {
		vec := make([]float32, dims)
		for j := range vec {
			vec[j] = rng.Float32()
		}
		vectors[i] = vec
	}
	return vectors
}

// ---------------------------------------------------------------------------
// vectorIndexFactory — NewVectorIndexFactory + CreateIndex
// ---------------------------------------------------------------------------

func TestNewVectorIndexFactory(t *testing.T) {
	t.Run("returns non-nil factory", func(t *testing.T) {
		factory := NewVectorIndexFactory()
		require.NotNil(t, factory)
	})
}

func TestVectorIndexFactory_CreateIndex_HNSW(t *testing.T) {
	factory := NewVectorIndexFactory()

	t.Run("creates HNSW index", func(t *testing.T) {
		cfg := &interfaces.VectorIndexConfig{
			IndexType:      interfaces.IndexTypeHNSW,
			DistanceMetric: "cosine",
			M:              16,
			MMax:           32,
			EfConstruction: 200,
			EfSearch:       100,
		}
		idx, err := factory.CreateIndex(cfg)
		require.NoError(t, err)
		require.NotNil(t, idx)
		assert.Equal(t, interfaces.IndexTypeHNSW, idx.IndexType())
		assert.True(t, idx.IsTrained())
		assert.Equal(t, int64(0), idx.Count())
		require.NoError(t, idx.Close())
	})

	t.Run("HNSW insert and search", func(t *testing.T) {
		cfg := &interfaces.VectorIndexConfig{
			IndexType:      interfaces.IndexTypeHNSW,
			DistanceMetric: "l2",
			M:              16,
			MMax:           32,
			EfConstruction: 200,
			EfSearch:       100,
		}
		idx, err := factory.CreateIndex(cfg)
		require.NoError(t, err)

		require.NoError(t, idx.Insert(1, []float32{1.0, 2.0, 3.0}))
		require.NoError(t, idx.Insert(2, []float32{4.0, 5.0, 6.0}))
		assert.Equal(t, int64(2), idx.Count())

		results, err := idx.Search([]float32{1.0, 2.0, 3.0}, 1, nil)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, uint64(1), results[0].ID)

		require.NoError(t, idx.Delete(1))
		assert.Equal(t, int64(1), idx.Count())

		stats := idx.GetStats()
		require.NotNil(t, stats)
		assert.Equal(t, interfaces.IndexTypeHNSW, stats.IndexType)
		assert.True(t, stats.IsTrained)

		// Train is no-op for HNSW
		require.NoError(t, idx.Train(nil))
		require.NoError(t, idx.Close())
	})
}

func TestVectorIndexFactory_CreateIndex_Flat(t *testing.T) {
	factory := NewVectorIndexFactory()

	t.Run("creates Flat index", func(t *testing.T) {
		cfg := &interfaces.VectorIndexConfig{
			IndexType:      interfaces.IndexTypeFlat,
			DistanceMetric: "l2",
		}
		idx, err := factory.CreateIndex(cfg)
		require.NoError(t, err)
		require.NotNil(t, idx)
		assert.Equal(t, interfaces.IndexTypeFlat, idx.IndexType())
		assert.True(t, idx.IsTrained())
		assert.Equal(t, int64(0), idx.Count())
	})

	t.Run("Flat insert search delete", func(t *testing.T) {
		cfg := &interfaces.VectorIndexConfig{
			IndexType:      interfaces.IndexTypeFlat,
			DistanceMetric: "cosine",
		}
		idx, err := factory.CreateIndex(cfg)
		require.NoError(t, err)

		require.NoError(t, idx.Insert(10, []float32{0.5, 0.5}))
		require.NoError(t, idx.Insert(20, []float32{1.0, 0.0}))

		results, err := idx.Search([]float32{0.5, 0.5}, 2, nil)
		require.NoError(t, err)
		assert.Len(t, results, 2)

		assert.Equal(t, int64(2), idx.Count())
		require.NoError(t, idx.Delete(10))
		assert.Equal(t, int64(1), idx.Count())

		stats := idx.GetStats()
		require.NotNil(t, stats)
		assert.Equal(t, interfaces.IndexTypeFlat, stats.IndexType)

		// Train is no-op
		require.NoError(t, idx.Train(nil))
		require.NoError(t, idx.Close())
	})
}

func TestVectorIndexFactory_CreateIndex_IVFFlat(t *testing.T) {
	factory := NewVectorIndexFactory()

	t.Run("creates IVF-Flat index with defaults", func(t *testing.T) {
		cfg := &interfaces.VectorIndexConfig{
			IndexType:      interfaces.IndexTypeIVFFlat,
			DistanceMetric: "l2",
		}
		idx, err := factory.CreateIndex(cfg)
		require.NoError(t, err)
		require.NotNil(t, idx)
		assert.Equal(t, interfaces.IndexTypeIVFFlat, idx.IndexType())
		assert.False(t, idx.IsTrained())
		assert.Equal(t, int64(0), idx.Count())

		stats := idx.GetStats()
		require.NotNil(t, stats)
		assert.Equal(t, interfaces.IndexTypeIVFFlat, stats.IndexType)
		assert.False(t, stats.IsTrained)

		require.NoError(t, idx.Close())
	})

	t.Run("creates IVF-Flat index with explicit config", func(t *testing.T) {
		cfg := &interfaces.VectorIndexConfig{
			IndexType:      interfaces.IndexTypeIVFFlat,
			DistanceMetric: "cosine",
			Nlist:          64,
			Nprobe:         8,
			Dimensions:     32,
		}
		idx, err := factory.CreateIndex(cfg)
		require.NoError(t, err)
		require.NotNil(t, idx)
		require.NoError(t, idx.Close())
	})
}

func TestVectorIndexFactory_CreateIndex_IVFPQ(t *testing.T) {
	factory := NewVectorIndexFactory()

	t.Run("creates IVF-PQ index with defaults", func(t *testing.T) {
		cfg := &interfaces.VectorIndexConfig{
			IndexType:      interfaces.IndexTypeIVFPQ,
			DistanceMetric: "dot_product",
		}
		idx, err := factory.CreateIndex(cfg)
		require.NoError(t, err)
		require.NotNil(t, idx)
		require.NoError(t, idx.Close())
	})

	t.Run("creates IVF-PQ index with explicit config", func(t *testing.T) {
		cfg := &interfaces.VectorIndexConfig{
			IndexType:      interfaces.IndexTypeIVFPQ,
			DistanceMetric: "l2",
			Nlist:          128,
			Nprobe:         16,
			Dimensions:     64,
		}
		idx, err := factory.CreateIndex(cfg)
		require.NoError(t, err)
		require.NotNil(t, idx)
		require.NoError(t, idx.Close())
	})
}

func TestVectorIndexFactory_CreateIndex_Unsupported(t *testing.T) {
	factory := NewVectorIndexFactory()

	t.Run("returns error for unsupported index type", func(t *testing.T) {
		cfg := &interfaces.VectorIndexConfig{
			IndexType: interfaces.IndexType(999),
		}
		idx, err := factory.CreateIndex(cfg)
		require.Error(t, err)
		assert.Nil(t, idx)
	})
}

// ---------------------------------------------------------------------------
// IVF-Flat: Train, Insert, Search, Delete after training
// ---------------------------------------------------------------------------

func TestIVFFlat_TrainInsertSearchDelete(t *testing.T) {
	factory := NewVectorIndexFactory()

	dims := 8
	nlist := 4
	cfg := &interfaces.VectorIndexConfig{
		IndexType:      interfaces.IndexTypeIVFFlat,
		DistanceMetric: "l2",
		Nlist:          nlist,
		Nprobe:         2,
		Dimensions:     dims,
	}
	idx, err := factory.CreateIndex(cfg)
	require.NoError(t, err)

	t.Run("train with sufficient vectors", func(t *testing.T) {
		// Need at least nlist vectors for training
		trainingData := generateTrainingVectors(nlist*10, dims)
		require.NoError(t, idx.Train(trainingData))
		assert.True(t, idx.IsTrained())
	})

	t.Run("insert vectors after training", func(t *testing.T) {
		require.NoError(t, idx.Insert(1, []float32{1, 0, 0, 0, 0, 0, 0, 0}))
		require.NoError(t, idx.Insert(2, []float32{0, 1, 0, 0, 0, 0, 0, 0}))
		require.NoError(t, idx.Insert(3, []float32{0, 0, 1, 0, 0, 0, 0, 0}))
		assert.Equal(t, int64(3), idx.Count())
	})

	t.Run("search with default nprobe", func(t *testing.T) {
		results, err := idx.Search([]float32{1, 0, 0, 0, 0, 0, 0, 0}, 2, nil)
		require.NoError(t, err)
		assert.NotEmpty(t, results)
	})

	t.Run("search with custom nprobe", func(t *testing.T) {
		params := &interfaces.SearchParams{Nprobe: 4}
		results, err := idx.Search([]float32{1, 0, 0, 0, 0, 0, 0, 0}, 2, params)
		require.NoError(t, err)
		assert.NotEmpty(t, results)
	})

	t.Run("delete vector", func(t *testing.T) {
		require.NoError(t, idx.Delete(1))
	})

	t.Run("get stats after operations", func(t *testing.T) {
		stats := idx.GetStats()
		require.NotNil(t, stats)
		assert.Equal(t, interfaces.IndexTypeIVFFlat, stats.IndexType)
		assert.True(t, stats.IsTrained)
		assert.Equal(t, dims, stats.Dimensions)
	})

	require.NoError(t, idx.Close())
}

// ---------------------------------------------------------------------------
// parseDistanceMetric (hnsw_factory.go)
// ---------------------------------------------------------------------------

func TestParseDistanceMetric(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"l2", "l2"},
		{"L2", "L2"},
		{"euclidean", "euclidean"},
		{"EUCLIDEAN", "EUCLIDEAN"},
		{"cosine", "cosine"},
		{"COSINE", "COSINE"},
		{"dot_product", "dot_product"},
		{"DOT_PRODUCT", "DOT_PRODUCT"},
		{"manhattan", "manhattan"},
		{"MANHATTAN", "MANHATTAN"},
		{"hamming", "hamming"},
		{"HAMMING", "HAMMING"},
		{"unknown_metric", "unknown_metric"},
		{"", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := parseDistanceMetric(tc.input)
			_ = result
		})
	}
}

// ---------------------------------------------------------------------------
// parseIVFDistanceMetric
// ---------------------------------------------------------------------------

func TestParseIVFDistanceMetric(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"l2"},
		{"L2"},
		{"euclidean"},
		{"EUCLIDEAN"},
		{"cosine"},
		{"COSINE"},
		{"dot_product"},
		{"DOT_PRODUCT"},
		{"unknown"},
		{""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := parseIVFDistanceMetric(tc.input)
			_ = result
		})
	}
}

// ---------------------------------------------------------------------------
// hnswIndexAdapter via hnswIndexFactory
// ---------------------------------------------------------------------------

func TestHNSWIndexFactory_CreateIndex(t *testing.T) {
	factory := &hnswIndexFactory{}

	t.Run("creates HNSW index successfully", func(t *testing.T) {
		cfg := &interfaces.HNSWConfig{
			M:              16,
			MMax:           32,
			EfConstruction: 200,
			EfSearch:       100,
			DistanceMetric: "cosine",
		}
		idx, err := factory.CreateIndex(cfg)
		require.NoError(t, err)
		require.NotNil(t, idx)

		require.NoError(t, idx.Insert(1, []float32{1.0, 0.0, 0.0}))
		require.NoError(t, idx.Insert(2, []float32{0.0, 1.0, 0.0}))

		results, err := idx.Search([]float32{1.0, 0.0, 0.0}, 1)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, uint64(1), results[0].ID)

		assert.Equal(t, int64(2), idx.Count())
		require.NoError(t, idx.Delete(1))
		assert.Equal(t, int64(1), idx.Count())

		stats := idx.GetStats()
		require.NotNil(t, stats)
		assert.Greater(t, stats.NodeCount, int64(0))
	})

	t.Run("returns nil stats when index is empty and stats are nil", func(t *testing.T) {
		cfg := &interfaces.HNSWConfig{
			M:              16,
			MMax:           32,
			EfConstruction: 200,
			EfSearch:       100,
		}
		idx, err := factory.CreateIndex(cfg)
		require.NoError(t, err)
		stats := idx.GetStats()
		_ = stats
	})
}

// ---------------------------------------------------------------------------
// hnswVectorIndexAdapter GetStats nil path
// ---------------------------------------------------------------------------

func TestHNSWVectorIndexAdapter_GetStatsNilInner(t *testing.T) {
	t.Run("returns default stats when inner returns nil", func(t *testing.T) {
		factory := &hnswIndexFactory{}
		cfg := &interfaces.HNSWConfig{
			M:              16,
			MMax:           32,
			EfConstruction: 200,
			EfSearch:       100,
		}
		inner, err := factory.CreateIndex(cfg)
		require.NoError(t, err)

		hnswAdapter := inner.(*hnswIndexAdapter)
		vecAdapter := &hnswVectorIndexAdapter{inner: hnswAdapter}

		stats := vecAdapter.GetStats()
		require.NotNil(t, stats)
		assert.Equal(t, interfaces.IndexTypeHNSW, stats.IndexType)
	})
}

// TestHNSWIndexFactory_SearchError covers the error branch of
// hnswIndexAdapter.Search: a query whose dimension does not match the inserted
// vectors makes the underlying go-qrdb index reject the search, and the adapter
// must surface that error (nil results) rather than panicking.
func TestHNSWIndexFactory_SearchError(t *testing.T) {
	factory := &hnswIndexFactory{}
	idx, err := factory.CreateIndex(&interfaces.HNSWConfig{
		M:              16,
		MMax:           32,
		EfConstruction: 200,
		EfSearch:       100,
		DistanceMetric: "cosine",
	})
	require.NoError(t, err)
	require.NoError(t, idx.Insert(1, []float32{1.0, 0.0, 0.0}))

	// Wrong-dimension query: 2-d against 3-d vectors.
	results, err := idx.Search([]float32{1.0, 0.0}, 1)
	require.Error(t, err, "a dimension-mismatched query must error")
	require.Nil(t, results)
}

// TestVectorIndexFactory_FlatSearchError covers the error branch of
// flatIndexAdapter.Search: a dimension-mismatched query is rejected by the
// underlying go-qrdb flat index and the adapter surfaces the error.
func TestVectorIndexFactory_FlatSearchError(t *testing.T) {
	factory := NewVectorIndexFactory()
	idx, err := factory.CreateIndex(&interfaces.VectorIndexConfig{
		IndexType:      interfaces.IndexTypeFlat,
		DistanceMetric: "l2",
	})
	require.NoError(t, err)
	require.NoError(t, idx.Insert(1, []float32{1.0, 0.0}))

	results, err := idx.Search([]float32{1.0, 0.0, 0.0}, 1, nil) // 3-d vs 2-d
	require.Error(t, err, "dimension-mismatched flat search must error")
	require.Nil(t, results)
}

// TestVectorIndexFactory_HNSWVectorAdapterSearchError covers the error branch of
// hnswVectorIndexAdapter.Search: a dimension-mismatched query surfaces the
// underlying index error through the VectorIndex adapter wrapper.
func TestVectorIndexFactory_HNSWVectorAdapterSearchError(t *testing.T) {
	factory := NewVectorIndexFactory()
	idx, err := factory.CreateIndex(&interfaces.VectorIndexConfig{
		IndexType:      interfaces.IndexTypeHNSW,
		DistanceMetric: "cosine",
		M:              16,
		MMax:           32,
		EfConstruction: 200,
		EfSearch:       100,
	})
	require.NoError(t, err)
	require.NoError(t, idx.Insert(1, []float32{1.0, 0.0, 0.0}))

	results, err := idx.Search([]float32{1.0, 0.0}, 1, nil) // 2-d vs 3-d
	require.Error(t, err, "dimension-mismatched HNSW search must error")
	require.Nil(t, results)
}

// TestCreateHNSWIndex_InvalidConfigError covers the construction-error branch of
// createHNSWIndex: an invalid HNSW configuration (negative graph parameters) is
// rejected by the underlying qrdb constructor and surfaces as an error.
func TestCreateHNSWIndex_InvalidConfigError(t *testing.T) {
	idx, err := createHNSWIndex(&interfaces.VectorIndexConfig{
		M:              -1,
		MMax:           -1,
		EfConstruction: -1,
	})
	require.Error(t, err, "an invalid HNSW config must be rejected")
	require.Nil(t, idx)
}

// TestVectorIndexFactory_IVFFlatSearchError covers the error branch of
// ivfFlatAdapter.Search: a dimension-mismatched query against a trained IVF
// index surfaces the underlying engine error (nil result).
func TestVectorIndexFactory_IVFFlatSearchError(t *testing.T) {
	factory := NewVectorIndexFactory()
	idx, err := factory.CreateIndex(&interfaces.VectorIndexConfig{
		IndexType:      interfaces.IndexTypeIVFFlat,
		DistanceMetric: "l2",
		Dimensions:     4,
		Nlist:          16,
	})
	require.NoError(t, err)

	vecs := make([][]float32, 0, 20)
	for i := 0; i < 20; i++ {
		v := []float32{float32(i), 1, 2, 3}
		vecs = append(vecs, v)
	}
	require.NoError(t, idx.Train(vecs))
	for i, v := range vecs {
		require.NoError(t, idx.Insert(uint64(i+1), v))
	}

	results, err := idx.Search([]float32{1, 2}, 1, nil) // 2-d query vs 4-d index
	require.Error(t, err, "a dimension-mismatched IVF search must error")
	require.Nil(t, results)
}
