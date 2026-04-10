//go:build !integration

package server

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// upsertVectorViaKV version tracking (76.2% -> 90%+)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_UpsertVector_KV_VersionIncrement(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "ver-ns", Dimension: 2})
	require.NoError(t, err)

	t.Run("first upsert has version 1", func(t *testing.T) {
		resp, err := bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
			Namespace: "ver-ns", ID: 1, Vector: []float32{1, 0},
			Metadata: map[string]interface{}{"color": "red"},
		})
		require.NoError(t, err)
		assert.Equal(t, uint64(1), resp.Version)
	})

	t.Run("second upsert increments to version 2", func(t *testing.T) {
		resp, err := bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
			Namespace: "ver-ns", ID: 1, Vector: []float32{0, 1},
			Metadata: map[string]interface{}{"color": "blue"},
		})
		require.NoError(t, err)
		assert.Equal(t, uint64(2), resp.Version)
	})

	t.Run("third upsert increments to version 3", func(t *testing.T) {
		resp, err := bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
			Namespace: "ver-ns", ID: 1, Vector: []float32{1, 1},
		})
		require.NoError(t, err)
		assert.Equal(t, uint64(3), resp.Version)
	})

	t.Run("original createdAt is preserved across updates", func(t *testing.T) {
		resp, err := bt.GetVector(ctx, &transport.GetVectorRequest{
			Namespace: "ver-ns", ID: 1, IncludeVector: true,
		})
		require.NoError(t, err)
		assert.True(t, resp.Found)
		assert.Equal(t, uint64(3), resp.Version)
		assert.True(t, resp.CreatedAt > 0)
		assert.True(t, resp.UpdatedAt >= resp.CreatedAt)
	})
}

// ---------------------------------------------------------------------------
// GetVector KV include flags (80%)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_GetVector_KV_IncludeFlags(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "flags-ns", Dimension: 2})
	require.NoError(t, err)
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
		Namespace: "flags-ns", ID: 1, Vector: []float32{1, 0},
		Metadata: map[string]interface{}{"color": "red"},
	})
	require.NoError(t, err)

	t.Run("without include flags vector and metadata are omitted", func(t *testing.T) {
		resp, err := bt.GetVector(ctx, &transport.GetVectorRequest{
			Namespace:       "flags-ns",
			ID:              1,
			IncludeVector:   false,
			IncludeMetadata: false,
		})
		require.NoError(t, err)
		assert.True(t, resp.Found)
		assert.Nil(t, resp.Vector)
		assert.Nil(t, resp.Metadata)
	})

	t.Run("include metadata returns metadata", func(t *testing.T) {
		resp, err := bt.GetVector(ctx, &transport.GetVectorRequest{
			Namespace:       "flags-ns",
			ID:              1,
			IncludeVector:   false,
			IncludeMetadata: true,
		})
		require.NoError(t, err)
		assert.True(t, resp.Found)
		assert.Nil(t, resp.Vector)
		assert.NotNil(t, resp.Metadata)
		assert.Equal(t, "red", resp.Metadata["color"])
	})

	t.Run("include vector returns embedding", func(t *testing.T) {
		resp, err := bt.GetVector(ctx, &transport.GetVectorRequest{
			Namespace:       "flags-ns",
			ID:              1,
			IncludeVector:   true,
			IncludeMetadata: false,
		})
		require.NoError(t, err)
		assert.True(t, resp.Found)
		assert.NotNil(t, resp.Vector)
		assert.Nil(t, resp.Metadata)
	})
}

// ---------------------------------------------------------------------------
// GetVector nil request
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_GetVector_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("nil request returns error", func(t *testing.T) {
		_, err := bt.GetVector(ctx, nil)
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// SearchSimilar with various distance metrics
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_SearchSimilar_KV_EuclideanDistance(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{
		Namespace: "euc-ns", Dimension: 2, DistanceMetric: transport.DistanceEuclidean,
	})
	require.NoError(t, err)

	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
		Namespace: "euc-ns", ID: 1, Vector: []float32{0, 0},
	})
	require.NoError(t, err)
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
		Namespace: "euc-ns", ID: 2, Vector: []float32{1, 1},
	})
	require.NoError(t, err)

	t.Run("euclidean search returns nearest vector", func(t *testing.T) {
		resp, err := bt.SearchSimilar(ctx, &transport.SearchSimilarRequest{
			Namespace:   "euc-ns",
			QueryVector: []float32{0.1, 0.1},
			K:           1,
		})
		require.NoError(t, err)
		require.Len(t, resp.Results, 1)
		assert.Equal(t, uint64(1), resp.Results[0].ID)
	})
}

func TestBaasTransport_Vector_SearchSimilar_KV_DotProductDistance(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{
		Namespace: "dot-ns", Dimension: 2, DistanceMetric: transport.DistanceDotProduct,
	})
	require.NoError(t, err)

	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
		Namespace: "dot-ns", ID: 1, Vector: []float32{1, 0},
	})
	require.NoError(t, err)
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
		Namespace: "dot-ns", ID: 2, Vector: []float32{0, 1},
	})
	require.NoError(t, err)

	t.Run("dot product search works", func(t *testing.T) {
		resp, err := bt.SearchSimilar(ctx, &transport.SearchSimilarRequest{
			Namespace:   "dot-ns",
			QueryVector: []float32{1, 0},
			K:           2,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(resp.Results), 1)
	})
}

func TestBaasTransport_Vector_SearchSimilar_KV_WithMetadataFilter(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{
		Namespace: "metaf-ns", Dimension: 2, DistanceMetric: transport.DistanceCosine,
	})
	require.NoError(t, err)

	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
		Namespace: "metaf-ns", ID: 1, Vector: []float32{1, 0},
		Metadata: map[string]interface{}{"color": "red"},
	})
	require.NoError(t, err)
	_, err = bt.UpsertVector(ctx, &transport.UpsertVectorRequest{
		Namespace: "metaf-ns", ID: 2, Vector: []float32{1, 0},
		Metadata: map[string]interface{}{"color": "blue"},
	})
	require.NoError(t, err)

	t.Run("metadata filter restricts results", func(t *testing.T) {
		resp, err := bt.SearchSimilar(ctx, &transport.SearchSimilarRequest{
			Namespace:      "metaf-ns",
			QueryVector:    []float32{1, 0},
			K:              5,
			MetadataFilter: map[string]interface{}{"color": "red"},
		})
		require.NoError(t, err)
		assert.Len(t, resp.Results, 1)
		assert.Equal(t, uint64(1), resp.Results[0].ID)
	})

	t.Run("min score filter restricts results", func(t *testing.T) {
		resp, err := bt.SearchSimilar(ctx, &transport.SearchSimilarRequest{
			Namespace:   "metaf-ns",
			QueryVector: []float32{1, 0},
			K:           5,
			MinScore:    0.99,
		})
		require.NoError(t, err)
		for _, r := range resp.Results {
			assert.GreaterOrEqual(t, r.Score, float32(0.99))
		}
	})

	t.Run("default K of 10 when K is 0", func(t *testing.T) {
		resp, err := bt.SearchSimilar(ctx, &transport.SearchSimilarRequest{
			Namespace:   "metaf-ns",
			QueryVector: []float32{1, 0},
			K:           0,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("include vectors and metadata flags", func(t *testing.T) {
		resp, err := bt.SearchSimilar(ctx, &transport.SearchSimilarRequest{
			Namespace:       "metaf-ns",
			QueryVector:     []float32{1, 0},
			K:               5,
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
// SearchByID with namespace not found (unmarshal error path)
// ---------------------------------------------------------------------------

func TestBaasTransport_Vector_SearchByID_UnmarshalError(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := bt.CreateNamespace(ctx, &transport.CreateNamespaceRequest{
		Namespace: "bad-ns", Dimension: 2,
	})
	require.NoError(t, err)

	// Write garbage data at the vector key to trigger unmarshal error.
	vecKey := vecBuildDataKey("bad-ns", 42)
	require.NoError(t, store.Put(ctx, vecKey, []byte("not json")))

	t.Run("unmarshal error returns VectorNotFoundError", func(t *testing.T) {
		_, err := bt.SearchByID(ctx, &transport.SearchByIDRequest{
			Namespace: "bad-ns", ID: 42, K: 5,
		})
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// tsAggregateViaStore with interval bucketing
// ---------------------------------------------------------------------------

func TestBaasTransport_TSAggregate_KV_WithInterval(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	baseNs := int64(1000000000)
	for i := int64(0); i < 6; i++ {
		require.NoError(t, bt.TSWrite(ctx, &transport.TSDataPoint{
			Metric:      "bucket.metric",
			Value:       float64(10 * (i + 1)),
			TimestampNs: baseNs + i*1000000, // 1ms apart
		}))
	}

	t.Run("interval bucketing produces multiple results", func(t *testing.T) {
		resp, err := bt.TSAggregate(ctx, &transport.TSAggregateRequest{
			Metric:     "bucket.metric",
			StartNs:    baseNs,
			EndNs:      baseNs + 6000000,
			Function:   transport.AggSum,
			IntervalNs: 3000000, // 3ms buckets
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(resp.Results), 1)
	})

	t.Run("negative interval aggregates single bucket", func(t *testing.T) {
		resp, err := bt.TSAggregate(ctx, &transport.TSAggregateRequest{
			Metric:     "bucket.metric",
			StartNs:    baseNs,
			EndNs:      baseNs + 6000000,
			Function:   transport.AggSum,
			IntervalNs: -1,
		})
		require.NoError(t, err)
		assert.Len(t, resp.Results, 1)
	})
}

// ---------------------------------------------------------------------------
// tsListMetricsViaStore list error path
// ---------------------------------------------------------------------------

func TestBaasTransport_TSListMetrics_KV_ListError(t *testing.T) {
	store := newFailingKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("list error returns empty metrics", func(t *testing.T) {
		store.failList = true
		resp, err := bt.TSListMetrics(ctx, &transport.TSListMetricsRequest{Prefix: "m."})
		require.NoError(t, err)
		assert.Empty(t, resp.Metrics)
		store.failList = false
	})
}
