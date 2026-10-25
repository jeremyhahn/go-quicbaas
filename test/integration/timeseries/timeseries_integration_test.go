// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

//go:build integration

package timeseries

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// createStorage creates a TimeSeriesStorage for integration testing and returns
// a cleanup function. The concrete implementation is provided by the test
// infrastructure (see helpers or main_test.go).
func createStorage(t *testing.T) (interfaces.TimeSeriesStorage, func()) {
	t.Helper()
	storage, cleanup, err := newTimeSeriesTestStorage(t)
	require.NoError(t, err)
	return storage, cleanup
}

// TestTimeSeriesStorage_BasicOperations tests basic CRUD operations.
func TestTimeSeriesStorage_BasicOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	storage, cleanup := createStorage(t)
	defer cleanup()

	t.Run("WriteAndQuerySinglePoint", func(t *testing.T) {
		// Write a single point
		point := &interfaces.DataPoint{
			Metric:    "cpu.usage",
			Tags:      map[string]string{"host": "server1", "region": "us-east"},
			Value:     75.5,
			Timestamp: time.Now(),
		}
		err := storage.WritePoint(ctx, point)
		require.NoError(t, err)

		// Query the point back
		result, err := storage.QueryRich(ctx, &interfaces.QueryRequest{
			Metric:    "cpu.usage",
			StartTime: time.Now().Add(-1 * time.Minute),
			EndTime:   time.Now().Add(1 * time.Minute),
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "cpu.usage", result.Metric)
		assert.Len(t, result.Points, 1)
		assert.Equal(t, 75.5, result.Points[0].Value)
	})

	t.Run("WriteBatchAndQuery", func(t *testing.T) {
		now := time.Now()
		points := []interfaces.DataPoint{
			{Metric: "memory.used", Tags: map[string]string{"host": "server1"}, Value: 4096, Timestamp: now.Add(-2 * time.Minute)},
			{Metric: "memory.used", Tags: map[string]string{"host": "server1"}, Value: 4200, Timestamp: now.Add(-1 * time.Minute)},
			{Metric: "memory.used", Tags: map[string]string{"host": "server1"}, Value: 4300, Timestamp: now},
		}

		// Write batch
		err := storage.WriteBatch(ctx, points)
		require.NoError(t, err)

		// Query all points
		result, err := storage.QueryRich(ctx, &interfaces.QueryRequest{
			Metric:    "memory.used",
			StartTime: now.Add(-5 * time.Minute),
			EndTime:   now.Add(1 * time.Minute),
		})
		require.NoError(t, err)
		assert.Len(t, result.Points, 3)
	})

	t.Run("QueryWithTagFilter", func(t *testing.T) {
		// Write points with different tags
		err := storage.WritePoint(ctx, &interfaces.DataPoint{
			Metric:    "disk.io",
			Tags:      map[string]string{"host": "server1", "disk": "sda"},
			Value:     100,
			Timestamp: time.Now(),
		})
		require.NoError(t, err)

		err = storage.WritePoint(ctx, &interfaces.DataPoint{
			Metric:    "disk.io",
			Tags:      map[string]string{"host": "server1", "disk": "sdb"},
			Value:     200,
			Timestamp: time.Now(),
		})
		require.NoError(t, err)

		// Query with tag filter
		result, err := storage.QueryRich(ctx, &interfaces.QueryRequest{
			Metric:    "disk.io",
			Tags:      map[string]string{"disk": "sda"},
			StartTime: time.Now().Add(-1 * time.Minute),
			EndTime:   time.Now().Add(1 * time.Minute),
		})
		require.NoError(t, err)
		assert.Len(t, result.Points, 1)
		assert.Equal(t, 100.0, result.Points[0].Value)
	})
}

// TestTimeSeriesStorage_Aggregations tests aggregation operations.
func TestTimeSeriesStorage_Aggregations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	storage, cleanup := createStorage(t)
	defer cleanup()

	// Write test data
	now := time.Now()
	points := []interfaces.DataPoint{
		{Metric: "test.metric", Tags: nil, Value: 10, Timestamp: now.Add(-4 * time.Minute)},
		{Metric: "test.metric", Tags: nil, Value: 20, Timestamp: now.Add(-3 * time.Minute)},
		{Metric: "test.metric", Tags: nil, Value: 30, Timestamp: now.Add(-2 * time.Minute)},
		{Metric: "test.metric", Tags: nil, Value: 40, Timestamp: now.Add(-1 * time.Minute)},
	}
	err := storage.WriteBatch(ctx, points)
	require.NoError(t, err)

	t.Run("AggregationSum", func(t *testing.T) {
		result, err := storage.QueryRich(ctx, &interfaces.QueryRequest{
			Metric:      "test.metric",
			StartTime:   now.Add(-5 * time.Minute),
			EndTime:     now.Add(1 * time.Minute),
			Aggregation: interfaces.AggSum,
		})
		require.NoError(t, err)
		require.NotNil(t, result.AggregatedValue)
		assert.Equal(t, 100.0, *result.AggregatedValue) // 10+20+30+40 = 100
	})

	t.Run("AggregationAvg", func(t *testing.T) {
		result, err := storage.QueryRich(ctx, &interfaces.QueryRequest{
			Metric:      "test.metric",
			StartTime:   now.Add(-5 * time.Minute),
			EndTime:     now.Add(1 * time.Minute),
			Aggregation: interfaces.AggAvg,
		})
		require.NoError(t, err)
		require.NotNil(t, result.AggregatedValue)
		assert.Equal(t, 25.0, *result.AggregatedValue) // (10+20+30+40)/4 = 25
	})

	t.Run("AggregationMin", func(t *testing.T) {
		result, err := storage.QueryRich(ctx, &interfaces.QueryRequest{
			Metric:      "test.metric",
			StartTime:   now.Add(-5 * time.Minute),
			EndTime:     now.Add(1 * time.Minute),
			Aggregation: interfaces.AggMin,
		})
		require.NoError(t, err)
		require.NotNil(t, result.AggregatedValue)
		assert.Equal(t, 10.0, *result.AggregatedValue)
	})

	t.Run("AggregationMax", func(t *testing.T) {
		result, err := storage.QueryRich(ctx, &interfaces.QueryRequest{
			Metric:      "test.metric",
			StartTime:   now.Add(-5 * time.Minute),
			EndTime:     now.Add(1 * time.Minute),
			Aggregation: interfaces.AggMax,
		})
		require.NoError(t, err)
		require.NotNil(t, result.AggregatedValue)
		assert.Equal(t, 40.0, *result.AggregatedValue)
	})

	t.Run("AggregationCount", func(t *testing.T) {
		result, err := storage.QueryRich(ctx, &interfaces.QueryRequest{
			Metric:      "test.metric",
			StartTime:   now.Add(-5 * time.Minute),
			EndTime:     now.Add(1 * time.Minute),
			Aggregation: interfaces.AggCount,
		})
		require.NoError(t, err)
		require.NotNil(t, result.AggregatedValue)
		assert.Equal(t, 4.0, *result.AggregatedValue)
	})
}

// TestTimeSeriesStorage_RetentionPolicy tests retention policy operations.
func TestTimeSeriesStorage_RetentionPolicy(t *testing.T) {
	storage, cleanup := createStorage(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("GetDefaultRetentionPolicy", func(t *testing.T) {
		policy, err := storage.GetRetentionPolicy(ctx, "nonexistent.metric")
		require.NoError(t, err)
		require.NotNil(t, policy)
		// Default policy has infinite retention (0 duration)
		assert.Equal(t, time.Duration(0), policy.Duration)
	})

	t.Run("SetAndGetRetentionPolicy", func(t *testing.T) {
		metric := "retention.test"
		policy := &interfaces.RetentionPolicy{
			Duration:    30 * 24 * time.Hour, // 30 days
			Resolution:  5 * time.Minute,
			Aggregation: interfaces.AggAvg,
		}

		// Set policy
		err := storage.SetRetentionPolicy(ctx, metric, policy)
		require.NoError(t, err)

		// Get policy back
		retrieved, err := storage.GetRetentionPolicy(ctx, metric)
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		assert.Equal(t, policy.Duration, retrieved.Duration)
		assert.Equal(t, policy.Resolution, retrieved.Resolution)
		assert.Equal(t, policy.Aggregation, retrieved.Aggregation)
	})
}

// TestTimeSeriesStorage_DeleteSeries tests delete operations.
func TestTimeSeriesStorage_DeleteSeries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	storage, cleanup := createStorage(t)
	defer cleanup()

	t.Run("DeleteAllPoints", func(t *testing.T) {
		// Write some points
		now := time.Now()
		points := []interfaces.DataPoint{
			{Metric: "delete.test", Tags: nil, Value: 1, Timestamp: now.Add(-2 * time.Minute)},
			{Metric: "delete.test", Tags: nil, Value: 2, Timestamp: now.Add(-1 * time.Minute)},
			{Metric: "delete.test", Tags: nil, Value: 3, Timestamp: now},
		}
		err := storage.WriteBatch(ctx, points)
		require.NoError(t, err)

		// Verify points exist
		result, err := storage.QueryRich(ctx, &interfaces.QueryRequest{
			Metric:    "delete.test",
			StartTime: now.Add(-5 * time.Minute),
			EndTime:   now.Add(1 * time.Minute),
		})
		require.NoError(t, err)
		assert.Len(t, result.Points, 3)

		// Delete all points
		err = storage.DeleteSeries(ctx, &interfaces.DeleteRequest{
			Metric: "delete.test",
		})
		require.NoError(t, err)

		// Verify points are gone
		result, err = storage.QueryRich(ctx, &interfaces.QueryRequest{
			Metric:    "delete.test",
			StartTime: now.Add(-5 * time.Minute),
			EndTime:   now.Add(1 * time.Minute),
		})
		require.NoError(t, err)
		assert.Len(t, result.Points, 0)
	})
}

// TestTimeSeriesStorage_ListMetrics tests the list metrics operation.
func TestTimeSeriesStorage_ListMetrics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	storage, cleanup := createStorage(t)
	defer cleanup()

	// Write points for multiple metrics
	metrics := []string{"cpu.usage", "cpu.system", "memory.used", "memory.free", "disk.io"}
	for _, metric := range metrics {
		err := storage.WritePoint(ctx, &interfaces.DataPoint{
			Metric:    metric,
			Value:     100,
			Timestamp: time.Now(),
		})
		require.NoError(t, err)
	}

	t.Run("ListAllMetrics", func(t *testing.T) {
		result, err := storage.ListMetrics(ctx, "", 0)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(result), 5)
	})

	t.Run("ListMetricsWithPrefix", func(t *testing.T) {
		result, err := storage.ListMetrics(ctx, "cpu", 0)
		require.NoError(t, err)
		assert.Len(t, result, 2) // cpu.usage, cpu.system
	})

	t.Run("ListMetricsWithLimit", func(t *testing.T) {
		result, err := storage.ListMetrics(ctx, "", 2)
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})
}
