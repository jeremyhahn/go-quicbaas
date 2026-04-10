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

// mockTimeSeriesStorage implements interfaces.TimeSeriesStorage for testing.
type mockTimeSeriesStorage struct {
	writePointFunc   func(ctx context.Context, point *interfaces.DataPoint) error
	writeBatchFunc   func(points []interfaces.DataPoint) error
	queryRichFunc    func(ctx context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error)
	deleteSeriesFunc func(ctx context.Context, req *interfaces.DeleteRequest) error
	getRetentionFunc func(metric string) (*interfaces.RetentionPolicy, error)
	setRetentionFunc func(metric string, policy *interfaces.RetentionPolicy) error
	listMetricsFunc  func(prefix string, limit int) ([]string, error)
}

func (m *mockTimeSeriesStorage) WritePoint(ctx context.Context, point *interfaces.DataPoint) error {
	if m.writePointFunc != nil {
		return m.writePointFunc(ctx, point)
	}
	return nil
}

func (m *mockTimeSeriesStorage) WriteBatch(points []interfaces.DataPoint) error {
	if m.writeBatchFunc != nil {
		return m.writeBatchFunc(points)
	}
	return nil
}

func (m *mockTimeSeriesStorage) QueryRich(ctx context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
	if m.queryRichFunc != nil {
		return m.queryRichFunc(ctx, req)
	}
	return &interfaces.QueryResult{}, nil
}

func (m *mockTimeSeriesStorage) DeleteSeries(ctx context.Context, req *interfaces.DeleteRequest) error {
	if m.deleteSeriesFunc != nil {
		return m.deleteSeriesFunc(ctx, req)
	}
	return nil
}

func (m *mockTimeSeriesStorage) GetRetentionPolicy(metric string) (*interfaces.RetentionPolicy, error) {
	if m.getRetentionFunc != nil {
		return m.getRetentionFunc(metric)
	}
	return &interfaces.RetentionPolicy{Duration: 24 * time.Hour}, nil
}

func (m *mockTimeSeriesStorage) SetRetentionPolicy(metric string, policy *interfaces.RetentionPolicy) error {
	if m.setRetentionFunc != nil {
		return m.setRetentionFunc(metric, policy)
	}
	return nil
}

func (m *mockTimeSeriesStorage) ListMetrics(prefix string, limit int) ([]string, error) {
	if m.listMetricsFunc != nil {
		return m.listMetricsFunc(prefix, limit)
	}
	return []string{"metric.a", "metric.b"}, nil
}

// ---------------------------------------------------------------------------
// Tests with TimeSeriesStorage delegation
// ---------------------------------------------------------------------------

func TestBaasTransport_TSWrite_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("TSWrite delegates to storage", func(t *testing.T) {
		var called bool
		tss.writePointFunc = func(ctx context.Context, point *interfaces.DataPoint) error {
			called = true
			return nil
		}
		err := bt.TSWrite(ctx, &transport.TSDataPoint{
			Metric: "cpu", Value: 50, TimestampNs: time.Now().UnixNano(),
		})
		require.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("TSWritePoint delegates to storage", func(t *testing.T) {
		var called bool
		tss.writePointFunc = func(ctx context.Context, point *interfaces.DataPoint) error {
			called = true
			return nil
		}
		err := bt.TSWritePoint(ctx, &transport.TSDataPoint{
			Metric: "mem", Value: 60, TimestampNs: time.Now().UnixNano(),
		})
		require.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("TSWritePoint with zero timestamp uses now", func(t *testing.T) {
		tss.writePointFunc = nil
		err := bt.TSWritePoint(ctx, &transport.TSDataPoint{
			Metric: "disk", Value: 30, TimestampNs: 0,
		})
		require.NoError(t, err)
	})

	t.Run("TSWritePoint nil point is no-op", func(t *testing.T) {
		err := bt.TSWritePoint(ctx, nil)
		require.NoError(t, err)
	})
}

func TestBaasTransport_TSWriteBatch_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("batch delegates to storage", func(t *testing.T) {
		var called bool
		tss.writeBatchFunc = func(points []interfaces.DataPoint) error {
			called = true
			return nil
		}
		err := bt.TSWriteBatch(ctx, []transport.TSDataPoint{
			{Metric: "cpu", Value: 10, TimestampNs: 1000},
			{Metric: "cpu", Value: 20, TimestampNs: 2000},
		})
		require.NoError(t, err)
		assert.True(t, called)
	})
}

func TestBaasTransport_TSQuery_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		queryRichFunc: func(ctx context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
			return &interfaces.QueryResult{
				Points: []*interfaces.DataPoint{
					{Metric: "cpu", Value: 42, Timestamp: time.Now()},
				},
			}, nil
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("query delegates to storage", func(t *testing.T) {
		resp, err := bt.TSQuery(ctx, &transport.TSQueryRequest{
			Metric: "cpu", StartNs: 0, EndNs: time.Now().UnixNano(),
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.GreaterOrEqual(t, len(resp.Points), 1)
	})
}

func TestBaasTransport_TSAggregate_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		queryRichFunc: func(ctx context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
			return &interfaces.QueryResult{
				Points: []*interfaces.DataPoint{
					{Metric: "cpu", Value: 10, Timestamp: time.Now()},
					{Metric: "cpu", Value: 20, Timestamp: time.Now()},
				},
			}, nil
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("aggregate delegates to storage", func(t *testing.T) {
		resp, err := bt.TSAggregate(ctx, &transport.TSAggregateRequest{
			Metric: "cpu", StartNs: 0, EndNs: time.Now().UnixNano(),
			Function: transport.AggSum,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_TSRichQuery_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{
		queryRichFunc: func(ctx context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
			return &interfaces.QueryResult{}, nil
		},
	}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("rich query delegates to storage", func(t *testing.T) {
		resp, err := bt.TSRichQuery(ctx, &transport.TSRichQueryRequest{
			Metric: "cpu", StartNs: 0, EndNs: time.Now().UnixNano(),
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestBaasTransport_TSDeleteSeries_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("delete delegates to storage", func(t *testing.T) {
		var called bool
		tss.deleteSeriesFunc = func(ctx context.Context, req *interfaces.DeleteRequest) error {
			called = true
			return nil
		}
		resp, err := bt.TSDeleteSeries(ctx, &transport.TSDeleteRequest{
			Metric: "old", StartNs: 0, EndNs: 1000,
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, called)
	})
}

func TestBaasTransport_TSRetention_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("get retention delegates to storage", func(t *testing.T) {
		resp, err := bt.TSGetRetention(ctx, &transport.TSGetRetentionRequest{Metric: "cpu"})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("set retention delegates to storage", func(t *testing.T) {
		var called bool
		tss.setRetentionFunc = func(metric string, policy *interfaces.RetentionPolicy) error {
			called = true
			return nil
		}
		err := bt.TSSetRetention(ctx, &transport.TSSetRetentionRequest{
			Metric: "cpu",
			Policy: &transport.TSRetentionPolicy{DurationNs: int64(48 * time.Hour)},
		})
		require.NoError(t, err)
		assert.True(t, called)
	})
}

func TestBaasTransport_TSListMetrics_ViaStorage(t *testing.T) {
	store := NewMemoryKVStore()
	tss := &mockTimeSeriesStorage{}
	bt := newBaasTransport(store, nil, tss, nil, nil)
	ctx := context.Background()

	t.Run("list metrics delegates to storage", func(t *testing.T) {
		resp, err := bt.TSListMetrics(ctx, &transport.TSListMetricsRequest{Prefix: "m"})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(resp.Metrics), 2)
	})
}
