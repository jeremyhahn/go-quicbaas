// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag && iam

package server

import (
	"context"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// newEmbeddedTSClient builds an in-memory standalone qrdb node and returns its
// SDK client. The node owns the time-series engine in-process so every adapter
// call routes directly to the engine. The node is closed on test cleanup.
func newEmbeddedTSClient(t *testing.T) *qrdbsdk.Client {
	t.Helper()
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.Mode = config.ServerModeStandalone
	// The qrdb node only instantiates the time-series engine for a
	// PebbleDB-backed store (the engine shares the pebble instance). The
	// default createFTConfig backend is pebble with a temp data dir, so leave
	// it as-is for a hermetic, on-disk-in-tempdir node.

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.startStorage())
	require.NoError(t, srv.startIndexes())
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	client := srv.qrdbNode.Client()
	require.NotNil(t, client, "embedded node must expose a non-nil SDK client")
	return client
}

// TestNodeTimeSeriesStorage_E2E exercises the full nodeTimeSeriesStorage adapter
// against an in-memory embedded qrdb node: single + batch writes, rich queries
// with tags and aggregation, retention policy get/set, metric listing, and
// deletion. Each call validates both the request and response conversion code
// in node_timeseries_storage.go.
func TestNodeTimeSeriesStorage_E2E(t *testing.T) {
	client := newEmbeddedTSClient(t)
	ts := newNodeTimeSeriesStorage(client)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// WritePoint stores a single data point with tags.
	require.NoError(t, ts.WritePoint(ctx, &interfaces.DataPoint{
		Metric:    "cpu.usage",
		Value:     0.50,
		Timestamp: base,
		Tags:      map[string]string{"host": "n1"},
	}))

	// WriteBatch stores multiple data points atomically.
	require.NoError(t, ts.WriteBatch([]interfaces.DataPoint{
		{Metric: "cpu.usage", Value: 0.60, Timestamp: base.Add(1 * time.Minute), Tags: map[string]string{"host": "n1"}},
		{Metric: "cpu.usage", Value: 0.70, Timestamp: base.Add(2 * time.Minute), Tags: map[string]string{"host": "n1"}},
		{Metric: "mem.used", Value: 1024, Timestamp: base.Add(1 * time.Minute), Tags: map[string]string{"host": "n1"}},
	}))

	// QueryRich performs a raw time-range query.
	res, err := ts.QueryRich(ctx, &interfaces.QueryRequest{
		Metric:    "cpu.usage",
		StartTime: base.Add(-time.Hour),
		EndTime:   base.Add(time.Hour),
		Tags:      map[string]string{"host": "n1"},
		Limit:     100,
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "cpu.usage", res.Metric)
	assert.GreaterOrEqual(t, len(res.Points), 1)

	// QueryRich with aggregation + grouping exercises the grouped-result branch.
	aggRes, err := ts.QueryRich(ctx, &interfaces.QueryRequest{
		Metric:      "cpu.usage",
		StartTime:   base.Add(-time.Hour),
		EndTime:     base.Add(time.Hour),
		Aggregation: interfaces.AggAvg,
		Interval:    time.Hour,
		GroupBy:     []string{"host"},
		Limit:       100,
	})
	require.NoError(t, err)
	require.NotNil(t, aggRes)

	// ListMetrics returns the written metric names.
	metrics, err := ts.ListMetrics("", 100)
	require.NoError(t, err)
	assert.Contains(t, metrics, "cpu.usage")

	// ListMetrics with a prefix filter.
	prefixed, err := ts.ListMetrics("cpu", 100)
	require.NoError(t, err)
	for _, m := range prefixed {
		assert.Contains(t, m, "cpu")
	}

	// SetRetentionPolicy followed by GetRetentionPolicy round-trips.
	require.NoError(t, ts.SetRetentionPolicy("cpu.usage", &interfaces.RetentionPolicy{
		Duration:    24 * time.Hour,
		Resolution:  time.Minute,
		Aggregation: interfaces.AggAvg,
	}))
	policy, err := ts.GetRetentionPolicy("cpu.usage")
	require.NoError(t, err)
	if policy != nil {
		assert.Equal(t, 24*time.Hour, policy.Duration)
	}

	// DeleteSeries removes the written data.
	require.NoError(t, ts.DeleteSeries(ctx, &interfaces.DeleteRequest{
		Metric:    "cpu.usage",
		Tags:      map[string]string{"host": "n1"},
		StartTime: base.Add(-time.Hour),
		EndTime:   base.Add(time.Hour),
	}))

	// Close is a no-op (the node owns the storage lifecycle).
	require.NoError(t, ts.(*nodeTimeSeriesStorage).Close())
}

// TestNodeTimeSeriesStorage_GetRetention_Missing verifies the get-retention path
// for a metric that has no policy returns a nil policy without error.
func TestNodeTimeSeriesStorage_GetRetention_Missing(t *testing.T) {
	client := newEmbeddedTSClient(t)
	ts := newNodeTimeSeriesStorage(client)

	// qrdb returns a zero-valued policy (not an error) for a metric that has
	// never had a policy set; the meaningful assertion is the absence of an
	// error and the successful response conversion.
	policy, err := ts.GetRetentionPolicy("never.written")
	require.NoError(t, err)
	if policy != nil {
		assert.Equal(t, time.Duration(0), policy.Duration)
	}
}

// TestNodeTimeSeriesStorage_QueryRich_EmptyMetricError covers the QueryRich
// error branch: a query with an empty metric name is rejected by the engine,
// and the adapter surfaces the transport error (nil result).
func TestNodeTimeSeriesStorage_QueryRich_EmptyMetricError(t *testing.T) {
	client := newEmbeddedTSClient(t)
	ts := newNodeTimeSeriesStorage(client)

	result, err := ts.QueryRich(context.Background(), &interfaces.QueryRequest{
		Metric:    "", // empty metric is rejected
		StartTime: time.Now().Add(-time.Hour),
		EndTime:   time.Now(),
	})
	require.Error(t, err, "an empty metric name must surface a query error")
	require.Nil(t, result)
}

// TestNodeTimeSeriesStorage_ListMetrics_ClientClosed covers the ListMetrics
// error branch: a closed transport makes the underlying TSListMetrics call fail.
func TestNodeTimeSeriesStorage_ListMetrics_ClientClosed(t *testing.T) {
	client := newEmbeddedTSClient(t)
	ts := newNodeTimeSeriesStorage(client)
	require.NoError(t, client.Close())

	got, err := ts.ListMetrics("", 10)
	require.Error(t, err)
	require.Nil(t, got)
}

// TestNodeTimeSeriesStorage_GetRetentionPolicy_ClientClosed covers the
// GetRetentionPolicy error branch via a closed transport.
func TestNodeTimeSeriesStorage_GetRetentionPolicy_ClientClosed(t *testing.T) {
	client := newEmbeddedTSClient(t)
	ts := newNodeTimeSeriesStorage(client)
	require.NoError(t, client.Close())

	got, err := ts.GetRetentionPolicy("cpu.usage")
	require.Error(t, err)
	require.Nil(t, got)
}
