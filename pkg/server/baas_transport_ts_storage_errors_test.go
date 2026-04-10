//go:build !integration

package server

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ---------------------------------------------------------------------------
// tsAggregateViaStorage error paths (starting at 29.2%)
// ---------------------------------------------------------------------------

func TestBaasTransport_TSAggregate_ViaStorage_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("nil request returns empty response", func(t *testing.T) {
		resp, err := bt.TSAggregate(ctx, nil)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Empty(t, resp.Results)
	})
}

func TestBaasTransport_TSAggregate_ViaStorage_QueryError(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		queryRichFunc: func(ctx context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
			return nil, &ServerMessageError{Message: "storage failure"}
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("query error propagates", func(t *testing.T) {
		_, err := bt.TSAggregate(ctx, &transport.TSAggregateRequest{
			Metric:   "cpu",
			StartNs:  1000,
			EndNs:    2000,
			Function: transport.AggSum,
		})
		require.Error(t, err)
	})
}

func TestBaasTransport_TSAggregate_ViaStorage_EmptyPoints(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		queryRichFunc: func(ctx context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
			return &interfaces.QueryResult{Points: nil}, nil
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("empty points returns empty response", func(t *testing.T) {
		resp, err := bt.TSAggregate(ctx, &transport.TSAggregateRequest{
			Metric:   "cpu",
			StartNs:  1000,
			EndNs:    2000,
			Function: transport.AggSum,
		})
		require.NoError(t, err)
		assert.Empty(t, resp.Results)
	})
}

func TestBaasTransport_TSAggregate_ViaStorage_NoInterval(t *testing.T) {
	now := time.Now()
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		queryRichFunc: func(ctx context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
			return &interfaces.QueryResult{
				Points: []*interfaces.DataPoint{
					{Metric: "cpu", Value: 10, Timestamp: now},
					{Metric: "cpu", Value: 20, Timestamp: now.Add(time.Second)},
					{Metric: "cpu", Value: 30, Timestamp: now.Add(2 * time.Second)},
				},
			}, nil
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("no interval aggregates all points in single bucket", func(t *testing.T) {
		resp, err := bt.TSAggregate(ctx, &transport.TSAggregateRequest{
			Metric:     "cpu",
			StartNs:    now.UnixNano(),
			EndNs:      now.Add(3 * time.Second).UnixNano(),
			Function:   transport.AggSum,
			IntervalNs: 0,
		})
		require.NoError(t, err)
		require.Len(t, resp.Results, 1)
		assert.Equal(t, float64(60), resp.Results[0].Value)
	})
}

func TestBaasTransport_TSAggregate_ViaStorage_WithInterval(t *testing.T) {
	now := time.Now()
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		queryRichFunc: func(ctx context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
			return &interfaces.QueryResult{
				Points: []*interfaces.DataPoint{
					{Metric: "cpu", Value: 10, Timestamp: now},
					{Metric: "cpu", Value: 20, Timestamp: now.Add(time.Second)},
					{Metric: "cpu", Value: 30, Timestamp: now.Add(5 * time.Second)},
				},
			}, nil
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("interval bucketing produces multiple results", func(t *testing.T) {
		resp, err := bt.TSAggregate(ctx, &transport.TSAggregateRequest{
			Metric:     "cpu",
			StartNs:    now.UnixNano(),
			EndNs:      now.Add(10 * time.Second).UnixNano(),
			Function:   transport.AggSum,
			IntervalNs: 3 * int64(time.Second),
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(resp.Results), 1)
	})
}

func TestBaasTransport_TSAggregate_ViaStorage_AllAggFunctions(t *testing.T) {
	now := time.Now()
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		queryRichFunc: func(ctx context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
			return &interfaces.QueryResult{
				Points: []*interfaces.DataPoint{
					{Metric: "cpu", Value: 10, Timestamp: now},
					{Metric: "cpu", Value: 30, Timestamp: now.Add(time.Second)},
				},
			}, nil
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name     string
		fn       transport.AggFunc
		expected float64
	}{
		{"avg", transport.AggAvg, 20.0},
		{"min", transport.AggMin, 10.0},
		{"max", transport.AggMax, 30.0},
		{"count", transport.AggCount, 2.0},
		{"first", transport.AggFirst, 10.0},
		{"last", transport.AggLast, 30.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := bt.TSAggregate(ctx, &transport.TSAggregateRequest{
				Metric:   "cpu",
				StartNs:  now.UnixNano(),
				EndNs:    now.Add(2 * time.Second).UnixNano(),
				Function: tc.fn,
			})
			require.NoError(t, err)
			require.Len(t, resp.Results, 1)
			assert.InDelta(t, tc.expected, resp.Results[0].Value, 0.01)
		})
	}
}

// ---------------------------------------------------------------------------
// tsRichQueryViaStorage error paths (starting at 60%)
// ---------------------------------------------------------------------------

func TestBaasTransport_TSRichQuery_ViaStorage_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("nil request returns empty response", func(t *testing.T) {
		resp, err := bt.TSRichQuery(ctx, nil)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_TSRichQuery_ViaStorage_QueryError(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		queryRichFunc: func(ctx context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
			return nil, &ServerMessageError{Message: "storage failure"}
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("query error propagates", func(t *testing.T) {
		_, err := bt.TSRichQuery(ctx, &transport.TSRichQueryRequest{
			Metric:  "cpu",
			StartNs: 1000,
			EndNs:   2000,
		})
		require.Error(t, err)
	})
}

func TestBaasTransport_TSRichQuery_ViaStorage_WithAggregation(t *testing.T) {
	now := time.Now()
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		queryRichFunc: func(ctx context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
			return &interfaces.QueryResult{
				Points: []*interfaces.DataPoint{
					{Metric: "cpu", Value: 10, Timestamp: now},
					{Metric: "cpu", Value: 20, Timestamp: now.Add(time.Second)},
				},
			}, nil
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("rich query with aggregation computes value", func(t *testing.T) {
		resp, err := bt.TSRichQuery(ctx, &transport.TSRichQueryRequest{
			Metric:      "cpu",
			StartNs:     now.UnixNano(),
			EndNs:       now.Add(2 * time.Second).UnixNano(),
			Aggregation: transport.AggSum,
		})
		require.NoError(t, err)
		require.NotNil(t, resp.AggregatedValue)
		assert.InDelta(t, 30.0, *resp.AggregatedValue, 0.01)
	})
}

// ---------------------------------------------------------------------------
// tsGetRetentionViaStorage error paths (starting at 62.5%)
// ---------------------------------------------------------------------------

func TestBaasTransport_TSGetRetention_ViaStorage_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("nil request returns empty response", func(t *testing.T) {
		resp, err := bt.TSGetRetention(ctx, nil)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_TSGetRetention_ViaStorage_StorageError(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		getRetentionFunc: func(metric string) (*interfaces.RetentionPolicy, error) {
			return nil, &ServerMessageError{Message: "not found"}
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("storage error returns empty policy", func(t *testing.T) {
		resp, err := bt.TSGetRetention(ctx, &transport.TSGetRetentionRequest{Metric: "missing"})
		require.NoError(t, err)
		assert.NotNil(t, resp.Policy)
		assert.Equal(t, int64(0), resp.Policy.DurationNs)
	})
}

func TestBaasTransport_TSGetRetention_ViaStorage_NilPolicy(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		getRetentionFunc: func(metric string) (*interfaces.RetentionPolicy, error) {
			return nil, nil
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("nil policy from storage returns empty policy", func(t *testing.T) {
		resp, err := bt.TSGetRetention(ctx, &transport.TSGetRetentionRequest{Metric: "cpu"})
		require.NoError(t, err)
		assert.NotNil(t, resp.Policy)
	})
}

func TestBaasTransport_TSGetRetention_ViaStorage_ValidPolicy(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		getRetentionFunc: func(metric string) (*interfaces.RetentionPolicy, error) {
			return &interfaces.RetentionPolicy{
				Duration:    48 * time.Hour,
				Resolution:  time.Minute,
				Aggregation: interfaces.AggFunc(transport.AggAvg),
			}, nil
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("valid policy is correctly converted", func(t *testing.T) {
		resp, err := bt.TSGetRetention(ctx, &transport.TSGetRetentionRequest{Metric: "cpu"})
		require.NoError(t, err)
		assert.Equal(t, int64(48*time.Hour), resp.Policy.DurationNs)
		assert.Equal(t, int64(time.Minute), resp.Policy.ResolutionNs)
	})
}

// ---------------------------------------------------------------------------
// tsSetRetentionViaStorage error paths (starting at 66.7%)
// ---------------------------------------------------------------------------

func TestBaasTransport_TSSetRetention_ViaStorage_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("nil request is no-op", func(t *testing.T) {
		err := bt.TSSetRetention(ctx, nil)
		require.NoError(t, err)
	})
}

func TestBaasTransport_TSSetRetention_ViaStorage_NilPolicy(t *testing.T) {
	store := NewMemoryKVStore()
	var calledWithNil bool
	tss := &mockTimeSeriesStorage{
		setRetentionFunc: func(metric string, policy *interfaces.RetentionPolicy) error {
			calledWithNil = (policy == nil)
			return nil
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("nil policy delegates nil to storage", func(t *testing.T) {
		err := bt.TSSetRetention(ctx, &transport.TSSetRetentionRequest{
			Metric: "cpu",
			Policy: nil,
		})
		require.NoError(t, err)
		assert.True(t, calledWithNil)
	})
}

func TestBaasTransport_TSSetRetention_ViaStorage_StorageError(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		setRetentionFunc: func(metric string, policy *interfaces.RetentionPolicy) error {
			return &ServerMessageError{Message: "write failed"}
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("storage error propagates", func(t *testing.T) {
		err := bt.TSSetRetention(ctx, &transport.TSSetRetentionRequest{
			Metric: "cpu",
			Policy: &transport.TSRetentionPolicy{DurationNs: int64(24 * time.Hour)},
		})
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// tsListMetricsViaStorage error paths (starting at 66.7%)
// ---------------------------------------------------------------------------

func TestBaasTransport_TSListMetrics_ViaStorage_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("nil request returns empty response", func(t *testing.T) {
		resp, err := bt.TSListMetrics(ctx, nil)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_TSListMetrics_ViaStorage_StorageError(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		listMetricsFunc: func(prefix string, limit int) ([]string, error) {
			return nil, &ServerMessageError{Message: "list failed"}
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("storage error returns empty list", func(t *testing.T) {
		resp, err := bt.TSListMetrics(ctx, &transport.TSListMetricsRequest{Prefix: "cpu"})
		require.NoError(t, err)
		assert.Empty(t, resp.Metrics)
	})
}

// ---------------------------------------------------------------------------
// tsDeleteSeriesViaStorage error paths (starting at 75%)
// ---------------------------------------------------------------------------

func TestBaasTransport_TSDeleteSeries_ViaStorage_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("nil request returns empty response", func(t *testing.T) {
		resp, err := bt.TSDeleteSeries(ctx, nil)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_TSDeleteSeries_ViaStorage_StorageError(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		deleteSeriesFunc: func(ctx context.Context, req *interfaces.DeleteRequest) error {
			return &ServerMessageError{Message: "delete failed"}
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("storage error propagates", func(t *testing.T) {
		_, err := bt.TSDeleteSeries(ctx, &transport.TSDeleteRequest{
			Metric:  "old",
			StartNs: 0,
			EndNs:   1000,
		})
		require.Error(t, err)
	})
}

func TestBaasTransport_TSDeleteSeries_ViaStorage_ZeroTimestamps(t *testing.T) {
	store := NewMemoryKVStore()
	var capturedReq *interfaces.DeleteRequest
	tss := &mockTimeSeriesStorage{
		deleteSeriesFunc: func(ctx context.Context, req *interfaces.DeleteRequest) error {
			capturedReq = req
			return nil
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("zero timestamps produce zero time values", func(t *testing.T) {
		resp, err := bt.TSDeleteSeries(ctx, &transport.TSDeleteRequest{
			Metric:  "cpu",
			StartNs: 0,
			EndNs:   0,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, capturedReq.StartTime.IsZero())
		assert.True(t, capturedReq.EndTime.IsZero())
	})
}

// ---------------------------------------------------------------------------
// tsWriteBatchToStorage error paths (starting at 77.8%)
// ---------------------------------------------------------------------------

func TestBaasTransport_TSWriteBatch_ViaStorage_EmptyBatch(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("empty batch is no-op", func(t *testing.T) {
		err := bt.TSWriteBatch(ctx, []transport.TSDataPoint{})
		require.NoError(t, err)
	})
}

func TestBaasTransport_TSWriteBatch_ViaStorage_StorageError(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		writeBatchFunc: func(points []interfaces.DataPoint) error {
			return &ServerMessageError{Message: "batch write failed"}
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("storage error propagates", func(t *testing.T) {
		err := bt.TSWriteBatch(ctx, []transport.TSDataPoint{
			{Metric: "cpu", Value: 10, TimestampNs: 1000},
		})
		require.Error(t, err)
	})
}

func TestBaasTransport_TSWriteBatch_ViaStorage_ZeroTimestamps(t *testing.T) {
	store := NewMemoryKVStore()
	var capturedPoints []interfaces.DataPoint
	tss := &mockTimeSeriesStorage{
		writeBatchFunc: func(points []interfaces.DataPoint) error {
			capturedPoints = points
			return nil
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("zero timestamp in batch uses current time", func(t *testing.T) {
		before := time.Now()
		err := bt.TSWriteBatch(ctx, []transport.TSDataPoint{
			{Metric: "cpu", Value: 10, TimestampNs: 0},
		})
		require.NoError(t, err)
		require.Len(t, capturedPoints, 1)
		assert.False(t, capturedPoints[0].Timestamp.Before(before))
	})
}

// ---------------------------------------------------------------------------
// tsQueryViaStorage error paths (starting at 79.2%)
// ---------------------------------------------------------------------------

func TestBaasTransport_TSQuery_ViaStorage_NilRequest(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("nil request returns empty response", func(t *testing.T) {
		resp, err := bt.TSQuery(ctx, nil)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_TSQuery_ViaStorage_QueryError(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		queryRichFunc: func(ctx context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
			return nil, &ServerMessageError{Message: "query failed"}
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("query error propagates", func(t *testing.T) {
		_, err := bt.TSQuery(ctx, &transport.TSQueryRequest{
			Metric:  "cpu",
			StartNs: 1000,
			EndNs:   2000,
		})
		require.Error(t, err)
	})
}

func TestBaasTransport_TSQuery_ViaStorage_ZeroTimestamps(t *testing.T) {
	store := NewMemoryKVStore()
	var capturedReq *interfaces.QueryRequest
	tss := &mockTimeSeriesStorage{
		queryRichFunc: func(ctx context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
			capturedReq = req
			return &interfaces.QueryResult{}, nil
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("zero start and end produce zero times", func(t *testing.T) {
		_, err := bt.TSQuery(ctx, &transport.TSQueryRequest{
			Metric:  "cpu",
			StartNs: 0,
			EndNs:   0,
		})
		require.NoError(t, err)
		assert.True(t, capturedReq.StartTime.IsZero())
		assert.True(t, capturedReq.EndTime.IsZero())
	})
}

func TestBaasTransport_TSQuery_ViaStorage_Descending(t *testing.T) {
	now := time.Now()
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		queryRichFunc: func(ctx context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
			return &interfaces.QueryResult{
				Points: []*interfaces.DataPoint{
					{Metric: "cpu", Value: 10, Timestamp: now},
					{Metric: "cpu", Value: 20, Timestamp: now.Add(time.Second)},
					{Metric: "cpu", Value: 30, Timestamp: now.Add(2 * time.Second)},
				},
			}, nil
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("descending sort order", func(t *testing.T) {
		resp, err := bt.TSQuery(ctx, &transport.TSQueryRequest{
			Metric:     "cpu",
			StartNs:    now.UnixNano(),
			EndNs:      now.Add(3 * time.Second).UnixNano(),
			Descending: true,
		})
		require.NoError(t, err)
		require.Len(t, resp.Points, 3)
		assert.True(t, resp.Points[0].TimestampNs > resp.Points[2].TimestampNs)
	})

	t.Run("limit applied after sort", func(t *testing.T) {
		resp, err := bt.TSQuery(ctx, &transport.TSQueryRequest{
			Metric:  "cpu",
			StartNs: now.UnixNano(),
			EndNs:   now.Add(3 * time.Second).UnixNano(),
			Limit:   2,
		})
		require.NoError(t, err)
		assert.Len(t, resp.Points, 2)
	})
}

// ---------------------------------------------------------------------------
// tsWritePointToStorage error paths
// ---------------------------------------------------------------------------

func TestBaasTransport_TSWritePoint_ViaStorage_StorageError(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		writePointFunc: func(ctx context.Context, point *interfaces.DataPoint) error {
			return &ServerMessageError{Message: "write failed"}
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("storage error propagates from TSWrite", func(t *testing.T) {
		err := bt.TSWrite(ctx, &transport.TSDataPoint{
			Metric: "cpu", Value: 42, TimestampNs: time.Now().UnixNano(),
		})
		require.Error(t, err)
	})

	t.Run("storage error propagates from TSWritePoint", func(t *testing.T) {
		err := bt.TSWritePoint(ctx, &transport.TSDataPoint{
			Metric: "cpu", Value: 42, TimestampNs: time.Now().UnixNano(),
		})
		require.Error(t, err)
	})
}

func TestBaasTransport_TSWrite_ViaStorage_NilPoint(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("nil point via storage is no-op", func(t *testing.T) {
		err := bt.TSWrite(ctx, nil)
		require.NoError(t, err)
	})
}

func TestBaasTransport_TSWrite_ViaStorage_WithTags(t *testing.T) {
	store := NewMemoryKVStore()
	var capturedPoint *interfaces.DataPoint
	tss := &mockTimeSeriesStorage{
		writePointFunc: func(ctx context.Context, point *interfaces.DataPoint) error {
			capturedPoint = point
			return nil
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("tags are converted to map", func(t *testing.T) {
		err := bt.TSWrite(ctx, &transport.TSDataPoint{
			Metric:      "cpu",
			Value:       42,
			TimestampNs: time.Now().UnixNano(),
			Tags: []transport.TSTag{
				{Key: "host", Value: "server1"},
				{Key: "region", Value: "us-east"},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "server1", capturedPoint.Tags["host"])
		assert.Equal(t, "us-east", capturedPoint.Tags["region"])
	})
}
