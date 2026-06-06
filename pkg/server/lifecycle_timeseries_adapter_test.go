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

//go:build timeseries

package server

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/vfs"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// newTSAdapter constructs a timeSeriesStorageAdapter backed by an in-memory
// PebbleDB-backed qrdb time-series storage. This exercises the adapter's
// request/response conversion against the real engine without any node/raft
// machinery, keeping the test fast and hermetic.
func newTSAdapter(t *testing.T) interfaces.TimeSeriesStorage {
	t.Helper()
	db, err := pebble.Open("ts-adapter", &pebble.Options{FS: vfs.NewMem()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	storage, err := qrdbsdk.NewTimeSeriesStorage(db)
	require.NoError(t, err)
	return newTimeSeriesStorageAdapter(storage)
}

// TestTimeSeriesStorageAdapter_E2E exercises every method of the
// timeSeriesStorageAdapter (the qrdbsdk.TimeSeriesStorage-backed adapter used
// when the server owns the storage directly) against an in-memory PebbleDB.
func TestTimeSeriesStorageAdapter_E2E(t *testing.T) {
	a := newTSAdapter(t)
	ctx := context.Background()
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	// WritePoint.
	require.NoError(t, a.WritePoint(ctx, &interfaces.DataPoint{
		Metric:    "disk.io",
		Value:     12.5,
		Timestamp: base,
		Tags:      map[string]string{"dev": "sda"},
	}))

	// WriteBatch.
	require.NoError(t, a.WriteBatch([]interfaces.DataPoint{
		{Metric: "disk.io", Value: 13.0, Timestamp: base.Add(time.Minute), Tags: map[string]string{"dev": "sda"}},
		{Metric: "disk.io", Value: 14.0, Timestamp: base.Add(2 * time.Minute), Tags: map[string]string{"dev": "sda"}},
	}))

	// QueryRich raw.
	res, err := a.QueryRich(ctx, &interfaces.QueryRequest{
		Metric:    "disk.io",
		StartTime: base.Add(-time.Hour),
		EndTime:   base.Add(time.Hour),
		Tags:      map[string]string{"dev": "sda"},
		Limit:     100,
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "disk.io", res.Metric)
	assert.GreaterOrEqual(t, len(res.Points), 1)

	// QueryRich aggregated + grouped (covers the grouped-result conversion).
	agg, err := a.QueryRich(ctx, &interfaces.QueryRequest{
		Metric:      "disk.io",
		StartTime:   base.Add(-time.Hour),
		EndTime:     base.Add(time.Hour),
		Aggregation: interfaces.AggAvg,
		Interval:    time.Hour,
		GroupBy:     []string{"dev"},
		Limit:       100,
	})
	require.NoError(t, err)
	require.NotNil(t, agg)

	// ListMetrics.
	metrics, err := a.ListMetrics("", 100)
	require.NoError(t, err)
	assert.Contains(t, metrics, "disk.io")

	// SetRetentionPolicy + GetRetentionPolicy round-trip.
	require.NoError(t, a.SetRetentionPolicy("disk.io", &interfaces.RetentionPolicy{
		Duration:    48 * time.Hour,
		Resolution:  time.Minute,
		Aggregation: interfaces.AggAvg,
	}))
	policy, err := a.GetRetentionPolicy("disk.io")
	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Equal(t, 48*time.Hour, policy.Duration)

	// DeleteSeries.
	require.NoError(t, a.DeleteSeries(ctx, &interfaces.DeleteRequest{
		Metric:    "disk.io",
		Tags:      map[string]string{"dev": "sda"},
		StartTime: base.Add(-time.Hour),
		EndTime:   base.Add(time.Hour),
	}))
}

// TestTimeSeriesStorageAdapter_QueryRichError covers the QueryRich error branch
// of the timeSeriesStorageAdapter: an empty metric name is rejected by the
// underlying qrdb storage, and the adapter surfaces the error (nil result).
func TestTimeSeriesStorageAdapter_QueryRichError(t *testing.T) {
	a := newTSAdapter(t)

	result, err := a.QueryRich(context.Background(), &interfaces.QueryRequest{
		Metric:    "", // empty metric is rejected by the engine
		StartTime: time.Now().Add(-time.Hour),
		EndTime:   time.Now(),
	})
	require.Error(t, err, "an empty metric name must surface a query error")
	assert.Nil(t, result)
}
