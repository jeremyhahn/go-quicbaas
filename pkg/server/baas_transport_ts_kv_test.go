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
// Time-series KV-backed operations (no timeSeriesStorage)
// ---------------------------------------------------------------------------

func TestBaasTransport_TSWrite_KVBacked(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("writes point to KV store", func(t *testing.T) {
		point := &transport.TSDataPoint{
			Metric:      "cpu.usage",
			Value:       75.5,
			TimestampNs: time.Now().UnixNano(),
			Tags:        []transport.TSTag{{Key: "host", Value: "s1"}},
		}
		err := bt.TSWrite(ctx, point)
		require.NoError(t, err)
	})

	t.Run("TSWritePoint also writes to KV store", func(t *testing.T) {
		point := &transport.TSDataPoint{
			Metric:      "mem.usage",
			Value:       50.0,
			TimestampNs: time.Now().UnixNano(),
		}
		err := bt.TSWritePoint(ctx, point)
		require.NoError(t, err)
	})
}

func TestBaasTransport_TSWriteBatch_KVBacked(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("writes multiple points to KV store", func(t *testing.T) {
		points := []transport.TSDataPoint{
			{Metric: "cpu", Value: 10, TimestampNs: 1000000},
			{Metric: "cpu", Value: 20, TimestampNs: 2000000},
		}
		err := bt.TSWriteBatch(ctx, points)
		require.NoError(t, err)
	})
}

func TestBaasTransport_TSQuery_KVBacked(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	// Write some data points
	now := time.Now().UnixNano()
	for i := int64(0); i < 5; i++ {
		point := &transport.TSDataPoint{
			Metric:      "test.metric",
			Value:       float64(i * 10),
			TimestampNs: now + i*int64(time.Second),
		}
		require.NoError(t, bt.TSWrite(ctx, point))
	}

	t.Run("queries data points within range", func(t *testing.T) {
		req := &transport.TSQueryRequest{
			Metric:  "test.metric",
			StartNs: now - int64(time.Second),
			EndNs:   now + 10*int64(time.Second),
		}
		resp, err := bt.TSQuery(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.GreaterOrEqual(t, len(resp.Points), 1)
	})
}

func TestBaasTransport_TSAggregate_KVBacked(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	now := time.Now().UnixNano()
	for i := int64(0); i < 3; i++ {
		require.NoError(t, bt.TSWrite(ctx, &transport.TSDataPoint{
			Metric:      "agg.metric",
			Value:       float64(10 + i*10),
			TimestampNs: now + i*int64(time.Second),
		}))
	}

	t.Run("computes sum aggregation", func(t *testing.T) {
		req := &transport.TSAggregateRequest{
			Metric:     "agg.metric",
			StartNs:    now - int64(time.Second),
			EndNs:      now + 10*int64(time.Second),
			Function:   transport.AggSum,
			IntervalNs: 100 * int64(time.Second),
		}
		resp, err := bt.TSAggregate(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_TSRichQuery_KVBacked(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	now := time.Now().UnixNano()
	require.NoError(t, bt.TSWrite(ctx, &transport.TSDataPoint{
		Metric:      "rich.metric",
		Value:       42.0,
		TimestampNs: now,
	}))

	t.Run("performs rich query", func(t *testing.T) {
		req := &transport.TSRichQueryRequest{
			Metric:  "rich.metric",
			StartNs: now - int64(time.Second),
			EndNs:   now + int64(time.Second),
		}
		resp, err := bt.TSRichQuery(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_TSDeleteSeries_KVBacked(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	now := time.Now().UnixNano()
	require.NoError(t, bt.TSWrite(ctx, &transport.TSDataPoint{
		Metric:      "del.metric",
		Value:       1.0,
		TimestampNs: now,
	}))

	t.Run("deletes matching series", func(t *testing.T) {
		req := &transport.TSDeleteRequest{
			Metric:  "del.metric",
			StartNs: now - int64(time.Second),
			EndNs:   now + int64(time.Second),
		}
		resp, err := bt.TSDeleteSeries(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_TSRetention_KVBacked(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	t.Run("set retention", func(t *testing.T) {
		req := &transport.TSSetRetentionRequest{
			Metric: "ret.metric",
			Policy: &transport.TSRetentionPolicy{DurationNs: int64(24 * time.Hour)},
		}
		err := bt.TSSetRetention(ctx, req)
		require.NoError(t, err)
	})

	t.Run("get retention", func(t *testing.T) {
		req := &transport.TSGetRetentionRequest{
			Metric: "ret.metric",
		}
		resp, err := bt.TSGetRetention(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.NotNil(t, resp.Policy)
		assert.Equal(t, int64(24*time.Hour), resp.Policy.DurationNs)
	})

	t.Run("get retention for non-existent metric returns default", func(t *testing.T) {
		req := &transport.TSGetRetentionRequest{
			Metric: "nonexistent",
		}
		resp, err := bt.TSGetRetention(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_TSListMetrics_KVBacked(t *testing.T) {
	store := NewMemoryKVStore()
	bt := newBaasTransport(store, nil, nil, nil, nil)
	ctx := context.Background()

	// Write metrics so their index entries exist
	require.NoError(t, bt.TSWrite(ctx, &transport.TSDataPoint{
		Metric:      "list.a",
		Value:       1.0,
		TimestampNs: time.Now().UnixNano(),
	}))
	require.NoError(t, bt.TSWrite(ctx, &transport.TSDataPoint{
		Metric:      "list.b",
		Value:       2.0,
		TimestampNs: time.Now().UnixNano(),
	}))

	t.Run("lists metrics matching prefix", func(t *testing.T) {
		req := &transport.TSListMetricsRequest{
			Prefix: "list.",
		}
		resp, err := bt.TSListMetrics(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.GreaterOrEqual(t, len(resp.Metrics), 2)
	})
}
