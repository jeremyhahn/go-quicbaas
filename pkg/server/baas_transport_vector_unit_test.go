//go:build !integration

package server

import (
	"container/heap"
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Vector error types
// ---------------------------------------------------------------------------

func TestVectorNamespaceExistsError(t *testing.T) {
	t.Run("includes namespace", func(t *testing.T) {
		err := &VectorNamespaceExistsError{Namespace: "my-ns"}
		assert.Contains(t, err.Error(), "my-ns")
		assert.Contains(t, err.Error(), "already exists")
	})
}

func TestVectorNamespaceNotFoundError(t *testing.T) {
	t.Run("includes namespace", func(t *testing.T) {
		err := &VectorNamespaceNotFoundError{Namespace: "gone"}
		assert.Contains(t, err.Error(), "gone")
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestVectorNotFoundError(t *testing.T) {
	t.Run("includes namespace and id", func(t *testing.T) {
		err := &VectorNotFoundError{Namespace: "ns1", ID: 42}
		assert.Contains(t, err.Error(), "ns1")
		assert.Contains(t, err.Error(), "42")
	})
}

func TestVectorDimensionMismatchError(t *testing.T) {
	t.Run("includes expected and got dimensions", func(t *testing.T) {
		err := &VectorDimensionMismatchError{Expected: 128, Got: 256}
		assert.Contains(t, err.Error(), "128")
		assert.Contains(t, err.Error(), "256")
	})
}

func TestVectorNilRequestError(t *testing.T) {
	t.Run("includes operation", func(t *testing.T) {
		err := &VectorNilRequestError{Operation: "UpsertVector"}
		assert.Contains(t, err.Error(), "UpsertVector")
	})
}

// ---------------------------------------------------------------------------
// Distance functions
// ---------------------------------------------------------------------------

func TestVecCosineDistance(t *testing.T) {
	t.Run("identical vectors have zero distance", func(t *testing.T) {
		v := []float32{1, 0, 0}
		d := vecCosineDistance(v, v)
		assert.InDelta(t, 0.0, float64(d), 1e-6)
	})

	t.Run("orthogonal vectors have distance 1", func(t *testing.T) {
		a := []float32{1, 0}
		b := []float32{0, 1}
		d := vecCosineDistance(a, b)
		assert.InDelta(t, 1.0, float64(d), 1e-6)
	})

	t.Run("zero vector returns 1", func(t *testing.T) {
		a := []float32{0, 0}
		b := []float32{1, 1}
		d := vecCosineDistance(a, b)
		assert.InDelta(t, 1.0, float64(d), 1e-6)
	})
}

func TestVecEuclideanDistance(t *testing.T) {
	t.Run("identical vectors have zero distance", func(t *testing.T) {
		v := []float32{3, 4}
		d := vecEuclideanDistance(v, v)
		assert.InDelta(t, 0.0, float64(d), 1e-6)
	})

	t.Run("computes correct L2 distance", func(t *testing.T) {
		a := []float32{0, 0}
		b := []float32{3, 4}
		d := vecEuclideanDistance(a, b)
		assert.InDelta(t, 5.0, float64(d), 1e-6)
	})
}

func TestVecDotProductDistance(t *testing.T) {
	t.Run("returns negative dot product", func(t *testing.T) {
		a := []float32{1, 2, 3}
		b := []float32{4, 5, 6}
		d := vecDotProductDistance(a, b)
		assert.InDelta(t, -32.0, float64(d), 1e-6)
	})

	t.Run("orthogonal vectors have zero", func(t *testing.T) {
		a := []float32{1, 0}
		b := []float32{0, 1}
		d := vecDotProductDistance(a, b)
		assert.InDelta(t, 0.0, float64(d), 1e-6)
	})
}

// ---------------------------------------------------------------------------
// vecSearchHeap
// ---------------------------------------------------------------------------

func TestVecSearchHeap(t *testing.T) {
	t.Run("maintains max-heap ordering", func(t *testing.T) {
		h := &vecSearchHeap{}
		heap.Init(h)

		heap.Push(h, vecSearchEntry{id: 1, distance: 0.5})
		heap.Push(h, vecSearchEntry{id: 2, distance: 0.8})
		heap.Push(h, vecSearchEntry{id: 3, distance: 0.2})

		assert.Equal(t, 3, h.Len())

		popped := heap.Pop(h).(vecSearchEntry)
		assert.Equal(t, uint64(2), popped.id)
		assert.InDelta(t, 0.8, float64(popped.distance), 1e-6)
	})

	t.Run("push with non-entry type is no-op", func(t *testing.T) {
		h := &vecSearchHeap{}
		heap.Init(h)
		h.Push("invalid")
		assert.Equal(t, 0, h.Len())
	})
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

func TestVecMetadataToStringMap(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		result := vecMetadataToStringMap(nil)
		assert.Nil(t, result)
	})

	t.Run("converts string values directly", func(t *testing.T) {
		m := map[string]interface{}{"key": "value"}
		result := vecMetadataToStringMap(m)
		assert.Equal(t, "value", result["key"])
	})

	t.Run("converts numeric values", func(t *testing.T) {
		m := map[string]interface{}{"num": float64(42)}
		result := vecMetadataToStringMap(m)
		assert.Equal(t, "42", result["num"])
	})
}

func TestVecStringMapToMetadata(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		result := vecStringMapToMetadata(nil)
		assert.Nil(t, result)
	})

	t.Run("converts string map to interface map", func(t *testing.T) {
		m := map[string]string{"k": "v"}
		result := vecStringMapToMetadata(m)
		assert.Equal(t, "v", result["k"])
	})
}

func TestVecToFloat64Safe(t *testing.T) {
	t.Run("returns value for float64", func(t *testing.T) {
		assert.InDelta(t, 3.14, vecToFloat64Safe(3.14), 1e-10)
	})

	t.Run("returns 0 for string", func(t *testing.T) {
		assert.InDelta(t, 0.0, vecToFloat64Safe("not a number"), 1e-10)
	})
}

func TestVecToFloat64(t *testing.T) {
	t.Run("float64", func(t *testing.T) {
		v, ok := vecToFloat64(float64(1.5))
		assert.True(t, ok)
		assert.InDelta(t, 1.5, v, 1e-10)
	})

	t.Run("float32", func(t *testing.T) {
		v, ok := vecToFloat64(float32(2.5))
		assert.True(t, ok)
		assert.InDelta(t, 2.5, v, 1e-2)
	})

	t.Run("int", func(t *testing.T) {
		v, ok := vecToFloat64(int(10))
		assert.True(t, ok)
		assert.InDelta(t, 10.0, v, 1e-10)
	})

	t.Run("int64", func(t *testing.T) {
		v, ok := vecToFloat64(int64(100))
		assert.True(t, ok)
		assert.InDelta(t, 100.0, v, 1e-10)
	})

	t.Run("uint64", func(t *testing.T) {
		v, ok := vecToFloat64(uint64(200))
		assert.True(t, ok)
		assert.InDelta(t, 200.0, v, 1e-10)
	})

	t.Run("string returns false", func(t *testing.T) {
		_, ok := vecToFloat64("str")
		assert.False(t, ok)
	})
}

func TestVecBuildDataKey(t *testing.T) {
	t.Run("builds correct key format", func(t *testing.T) {
		key := vecBuildDataKey("myns", 42)
		assert.Equal(t, "vec:data:myns:42", key)
	})
}

func TestVecResolveDistanceFunc(t *testing.T) {
	t.Run("returns function for known metric", func(t *testing.T) {
		fn := vecResolveDistanceFunc(transport.DistanceCosine)
		assert.NotNil(t, fn)
	})

	t.Run("defaults to cosine for unknown metric", func(t *testing.T) {
		fn := vecResolveDistanceFunc("unknown-metric")
		assert.NotNil(t, fn)
		d := fn([]float32{1, 0}, []float32{1, 0})
		assert.InDelta(t, 0.0, float64(d), 1e-6)
	})
}

func TestVecMetadataMatches(t *testing.T) {
	meta := map[string]interface{}{"color": "red", "size": float64(10)}

	t.Run("all filters match (AND)", func(t *testing.T) {
		filter := map[string]interface{}{"color": "red"}
		assert.True(t, vecMetadataMatches(meta, filter))
	})

	t.Run("filter not in metadata returns false", func(t *testing.T) {
		filter := map[string]interface{}{"shape": "circle"}
		assert.False(t, vecMetadataMatches(meta, filter))
	})

	t.Run("filter value mismatch returns false", func(t *testing.T) {
		filter := map[string]interface{}{"color": "blue"}
		assert.False(t, vecMetadataMatches(meta, filter))
	})

	t.Run("empty filter always matches", func(t *testing.T) {
		assert.True(t, vecMetadataMatches(meta, map[string]interface{}{}))
	})
}

func TestVecMetadataMatchesAny(t *testing.T) {
	meta := map[string]interface{}{"color": "red", "size": float64(10)}

	t.Run("at least one filter matches (OR)", func(t *testing.T) {
		filter := map[string]interface{}{"color": "red", "shape": "circle"}
		assert.True(t, vecMetadataMatchesAny(meta, filter))
	})

	t.Run("no filters match returns false", func(t *testing.T) {
		filter := map[string]interface{}{"shape": "circle"}
		assert.False(t, vecMetadataMatchesAny(meta, filter))
	})

	t.Run("empty filter returns false", func(t *testing.T) {
		assert.False(t, vecMetadataMatchesAny(meta, map[string]interface{}{}))
	})
}

func TestVecValuesEqual(t *testing.T) {
	t.Run("same string", func(t *testing.T) {
		assert.True(t, vecValuesEqual("abc", "abc"))
	})

	t.Run("different string", func(t *testing.T) {
		assert.False(t, vecValuesEqual("abc", "def"))
	})

	t.Run("numeric equality across types", func(t *testing.T) {
		assert.True(t, vecValuesEqual(float64(10), int(10)))
	})

	t.Run("nil equals nil", func(t *testing.T) {
		assert.True(t, vecValuesEqual(nil, nil))
	})

	t.Run("mixed types return false", func(t *testing.T) {
		assert.False(t, vecValuesEqual("10", float64(10)))
	})
}

func TestVecDistanceToScore(t *testing.T) {
	t.Run("zero distance returns 1", func(t *testing.T) {
		assert.InDelta(t, 1.0, float64(vecDistanceToScore(0)), 1e-6)
	})

	t.Run("negative distance returns 1", func(t *testing.T) {
		assert.InDelta(t, 1.0, float64(vecDistanceToScore(-1)), 1e-6)
	})

	t.Run("positive distance returns score in (0,1)", func(t *testing.T) {
		score := vecDistanceToScore(1.0)
		assert.InDelta(t, 0.5, float64(score), 1e-6)
	})
}

func TestVecHeapToResults(t *testing.T) {
	t.Run("converts heap to sorted results", func(t *testing.T) {
		h := &vecSearchHeap{}
		heap.Init(h)
		heap.Push(h, vecSearchEntry{id: 1, distance: 0.5, vector: []float32{1}, metadata: map[string]interface{}{"k": "v"}})
		heap.Push(h, vecSearchEntry{id: 2, distance: 0.1, vector: []float32{2}, metadata: map[string]interface{}{"k": "v2"}})

		results := vecHeapToResults(h, true, true)
		require.Len(t, results, 2)
		assert.Equal(t, uint64(2), results[0].ID)
		assert.Equal(t, uint64(1), results[1].ID)
		assert.Equal(t, 1, results[0].Rank)
		assert.Equal(t, 2, results[1].Rank)
		assert.NotNil(t, results[0].Vector)
		assert.NotNil(t, results[0].Metadata)
	})

	t.Run("excludes vectors and metadata when flags are false", func(t *testing.T) {
		h := &vecSearchHeap{}
		heap.Init(h)
		heap.Push(h, vecSearchEntry{id: 1, distance: 0.3, vector: []float32{1}, metadata: map[string]interface{}{"k": "v"}})

		results := vecHeapToResults(h, false, false)
		require.Len(t, results, 1)
		assert.Nil(t, results[0].Vector)
		assert.Nil(t, results[0].Metadata)
	})

	t.Run("empty heap returns empty results", func(t *testing.T) {
		h := &vecSearchHeap{}
		heap.Init(h)
		results := vecHeapToResults(h, true, true)
		assert.Len(t, results, 0)
	})
}

func TestVecGetNamespace(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("returns error for missing namespace", func(t *testing.T) {
		_, err := bt.vecGetNamespace(ctx, "nonexistent")
		require.Error(t, err)
		var nsErr *VectorNamespaceNotFoundError
		assert.True(t, errors.As(err, &nsErr))
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		require.NoError(t, store.Put(ctx, vecNSPrefix+"bad", []byte("not-json")))
		_, err := bt.vecGetNamespace(ctx, "bad")
		require.Error(t, err)
	})
}

func TestVecCountVectors(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("returns zero for empty namespace", func(t *testing.T) {
		count, err := bt.vecCountVectors(ctx, "empty")
		require.NoError(t, err)
		assert.Equal(t, uint64(0), count)
	})

	t.Run("counts vectors correctly", func(t *testing.T) {
		require.NoError(t, store.Put(ctx, vecDataPrefix+"myns:1", []byte("data1")))
		require.NoError(t, store.Put(ctx, vecDataPrefix+"myns:2", []byte("data2")))
		count, err := bt.vecCountVectors(ctx, "myns")
		require.NoError(t, err)
		assert.Equal(t, uint64(2), count)
	})
}
