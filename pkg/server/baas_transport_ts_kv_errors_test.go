//go:build !integration

package server

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// tsQueryViaStore error paths (starting at 68%)
// ---------------------------------------------------------------------------

func TestBaasTransport_TSQuery_KV_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("nil request returns empty response", func(t *testing.T) {
		resp, err := bt.TSQuery(ctx, nil)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_TSQuery_KV_DescendingAndLimit(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	now := time.Now().UnixNano()
	for i := int64(0); i < 5; i++ {
		require.NoError(t, bt.TSWrite(ctx, &transport.TSDataPoint{
			Metric:      "desc.metric",
			Value:       float64(i * 10),
			TimestampNs: now + i*int64(time.Second),
		}))
	}

	t.Run("descending order returns points in reverse", func(t *testing.T) {
		resp, err := bt.TSQuery(ctx, &transport.TSQueryRequest{
			Metric:     "desc.metric",
			StartNs:    now - int64(time.Second),
			EndNs:      now + 10*int64(time.Second),
			Descending: true,
		})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(resp.Points), 2)
		assert.True(t, resp.Points[0].TimestampNs > resp.Points[len(resp.Points)-1].TimestampNs)
	})

	t.Run("limit caps result count", func(t *testing.T) {
		resp, err := bt.TSQuery(ctx, &transport.TSQueryRequest{
			Metric:  "desc.metric",
			StartNs: now - int64(time.Second),
			EndNs:   now + 10*int64(time.Second),
			Limit:   2,
		})
		require.NoError(t, err)
		assert.Len(t, resp.Points, 2)
	})
}

func TestBaasTransport_TSQuery_KV_TagFiltering(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	now := time.Now().UnixNano()
	require.NoError(t, bt.TSWrite(ctx, &transport.TSDataPoint{
		Metric:      "tagged.metric",
		Value:       10,
		TimestampNs: now,
		Tags:        []transport.TSTag{{Key: "host", Value: "s1"}},
	}))
	require.NoError(t, bt.TSWrite(ctx, &transport.TSDataPoint{
		Metric:      "tagged.metric",
		Value:       20,
		TimestampNs: now + int64(time.Second),
		Tags:        []transport.TSTag{{Key: "host", Value: "s2"}},
	}))

	t.Run("tag filter matches subset", func(t *testing.T) {
		resp, err := bt.TSQuery(ctx, &transport.TSQueryRequest{
			Metric:  "tagged.metric",
			StartNs: now - int64(time.Second),
			EndNs:   now + 2*int64(time.Second),
			Tags:    []transport.TSTag{{Key: "host", Value: "s1"}},
		})
		require.NoError(t, err)
		assert.Len(t, resp.Points, 1)
		assert.Equal(t, float64(10), resp.Points[0].Value)
	})
}

// ---------------------------------------------------------------------------
// tsAggregateViaStore error paths (starting at 75%)
// ---------------------------------------------------------------------------

func TestBaasTransport_TSAggregate_KV_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("nil request returns empty response", func(t *testing.T) {
		resp, err := bt.TSAggregate(ctx, nil)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Empty(t, resp.Results)
	})
}

func TestBaasTransport_TSAggregate_KV_EmptyResult(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("no matching points returns empty", func(t *testing.T) {
		resp, err := bt.TSAggregate(ctx, &transport.TSAggregateRequest{
			Metric:   "nonexistent",
			StartNs:  1000,
			EndNs:    2000,
			Function: transport.AggSum,
		})
		require.NoError(t, err)
		assert.Empty(t, resp.Results)
	})
}

// ---------------------------------------------------------------------------
// tsRichQueryViaStore error paths (starting at 60%)
// ---------------------------------------------------------------------------

func TestBaasTransport_TSRichQuery_KV_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("nil request returns empty response", func(t *testing.T) {
		resp, err := bt.TSRichQuery(ctx, nil)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_TSRichQuery_KV_WithAggregation(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	now := time.Now().UnixNano()
	require.NoError(t, bt.TSWrite(ctx, &transport.TSDataPoint{
		Metric: "rich.agg", Value: 10, TimestampNs: now,
	}))
	require.NoError(t, bt.TSWrite(ctx, &transport.TSDataPoint{
		Metric: "rich.agg", Value: 30, TimestampNs: now + int64(time.Second),
	}))

	t.Run("aggregation computes sum", func(t *testing.T) {
		resp, err := bt.TSRichQuery(ctx, &transport.TSRichQueryRequest{
			Metric:      "rich.agg",
			StartNs:     now - int64(time.Second),
			EndNs:       now + 2*int64(time.Second),
			Aggregation: transport.AggSum,
		})
		require.NoError(t, err)
		require.NotNil(t, resp.AggregatedValue)
		assert.InDelta(t, 40.0, *resp.AggregatedValue, 0.01)
	})
}

// ---------------------------------------------------------------------------
// tsDeleteSeriesViaStore error paths (starting at 69.6%)
// ---------------------------------------------------------------------------

func TestBaasTransport_TSDeleteSeries_KV_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("nil request returns empty response", func(t *testing.T) {
		resp, err := bt.TSDeleteSeries(ctx, nil)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_TSDeleteSeries_KV_AllData(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	now := time.Now().UnixNano()
	require.NoError(t, bt.TSWrite(ctx, &transport.TSDataPoint{
		Metric: "del.all", Value: 1, TimestampNs: now,
	}))

	t.Run("delete with zero timestamps removes all and cleans index", func(t *testing.T) {
		resp, err := bt.TSDeleteSeries(ctx, &transport.TSDeleteRequest{
			Metric:  "del.all",
			StartNs: 0,
			EndNs:   0,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)

		// Verify data is gone.
		queryResp, err := bt.TSQuery(ctx, &transport.TSQueryRequest{
			Metric: "del.all",
		})
		require.NoError(t, err)
		assert.Empty(t, queryResp.Points)
	})
}

// ---------------------------------------------------------------------------
// tsSetRetentionViaStore error paths (starting at 66.7%)
// ---------------------------------------------------------------------------

func TestBaasTransport_TSSetRetention_KV_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("nil request is no-op", func(t *testing.T) {
		err := bt.TSSetRetention(ctx, nil)
		require.NoError(t, err)
	})
}

func TestBaasTransport_TSSetRetention_KV_NilPolicy(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	// First set a policy.
	require.NoError(t, bt.TSSetRetention(ctx, &transport.TSSetRetentionRequest{
		Metric: "ret.nil",
		Policy: &transport.TSRetentionPolicy{DurationNs: int64(24 * time.Hour)},
	}))

	t.Run("nil policy deletes retention key", func(t *testing.T) {
		err := bt.TSSetRetention(ctx, &transport.TSSetRetentionRequest{
			Metric: "ret.nil",
			Policy: nil,
		})
		require.NoError(t, err)

		// Verify retention is gone.
		resp, err := bt.TSGetRetention(ctx, &transport.TSGetRetentionRequest{Metric: "ret.nil"})
		require.NoError(t, err)
		assert.NotNil(t, resp.Policy)
		assert.Equal(t, int64(0), resp.Policy.DurationNs)
	})
}

// ---------------------------------------------------------------------------
// tsWritePointToStore error paths (starting at 71.4%)
// ---------------------------------------------------------------------------

func TestBaasTransport_TSWritePoint_KV_NilPoint(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("nil point is no-op", func(t *testing.T) {
		err := bt.TSWritePoint(ctx, nil)
		require.NoError(t, err)
	})
}

func TestBaasTransport_TSWritePoint_KV_ZeroTimestamp(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("zero timestamp uses current time", func(t *testing.T) {
		err := bt.TSWritePoint(ctx, &transport.TSDataPoint{
			Metric: "auto.ts", Value: 42, TimestampNs: 0,
		})
		require.NoError(t, err)
	})
}

// ---------------------------------------------------------------------------
// tsListMetricsViaStore error paths
// ---------------------------------------------------------------------------

func TestBaasTransport_TSListMetrics_KV_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("nil request returns empty response", func(t *testing.T) {
		resp, err := bt.TSListMetrics(ctx, nil)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_TSListMetrics_KV_WithLimit(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	// Write metrics so index entries exist.
	for _, m := range []string{"m.a", "m.b", "m.c"} {
		require.NoError(t, bt.TSWrite(ctx, &transport.TSDataPoint{
			Metric: m, Value: 1, TimestampNs: time.Now().UnixNano(),
		}))
	}

	t.Run("limit caps metric count", func(t *testing.T) {
		resp, err := bt.TSListMetrics(ctx, &transport.TSListMetricsRequest{
			Prefix: "m.",
			Limit:  2,
		})
		require.NoError(t, err)
		assert.Len(t, resp.Metrics, 2)
	})
}

// ---------------------------------------------------------------------------
// tsGetRetentionViaStore error paths (80%)
// ---------------------------------------------------------------------------

func TestBaasTransport_TSGetRetention_KV_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("nil request returns empty response", func(t *testing.T) {
		resp, err := bt.TSGetRetention(ctx, nil)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}
