//go:build timeseries && !integration

package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ---------------------------------------------------------------------------
// convertToQRDBDataPoint
// ---------------------------------------------------------------------------

func TestTSConvert_DataPoint(t *testing.T) {
	now := time.Now()

	t.Run("converts all fields", func(t *testing.T) {
		point := &interfaces.DataPoint{
			Metric:    "cpu.usage",
			Value:     42.5,
			Timestamp: now,
			Tags:      map[string]string{"host": "server1"},
		}
		result := convertToQRDBDataPoint(point)
		require.NotNil(t, result)
		assert.Equal(t, "cpu.usage", result.Metric)
		assert.Equal(t, 42.5, result.Value)
		assert.Equal(t, now, result.Timestamp)
		assert.Equal(t, "server1", result.Tags["host"])
	})

	t.Run("converts with nil tags", func(t *testing.T) {
		point := &interfaces.DataPoint{
			Metric:    "mem.free",
			Value:     1024.0,
			Timestamp: now,
			Tags:      nil,
		}
		result := convertToQRDBDataPoint(point)
		require.NotNil(t, result)
		assert.Nil(t, result.Tags)
	})
}

// ---------------------------------------------------------------------------
// convertToQRDBQueryRequest
// ---------------------------------------------------------------------------

func TestTSConvert_QueryRequest(t *testing.T) {
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()

	t.Run("converts all fields", func(t *testing.T) {
		req := &interfaces.QueryRequest{
			Metric:      "cpu.usage",
			Tags:        map[string]string{"host": "server1"},
			StartTime:   start,
			EndTime:     end,
			Aggregation: interfaces.AggAvg,
			Interval:    5 * time.Minute,
			GroupBy:     []string{"host"},
			Limit:       100,
			Percentile:  95.0,
		}
		result := convertToQRDBQueryRequest(req)
		require.NotNil(t, result)
		assert.Equal(t, "cpu.usage", result.Metric)
		assert.Equal(t, start, result.StartTime)
		assert.Equal(t, end, result.EndTime)
		assert.Equal(t, qrdbsdk.TimeSeriesAggFunc(interfaces.AggAvg), result.Aggregation)
		assert.Equal(t, 5*time.Minute, result.Interval)
		assert.Equal(t, 100, result.Limit)
	})

	t.Run("converts minimal request", func(t *testing.T) {
		req := &interfaces.QueryRequest{
			Metric: "disk.io",
		}
		result := convertToQRDBQueryRequest(req)
		require.NotNil(t, result)
		assert.Equal(t, "disk.io", result.Metric)
	})
}

// ---------------------------------------------------------------------------
// convertFromQRDBQueryResult
// ---------------------------------------------------------------------------

func TestTSConvert_QueryResult(t *testing.T) {
	now := time.Now()
	aggVal := func(v float64) *float64 { return &v }

	t.Run("converts with points", func(t *testing.T) {
		qrdbResult := &qrdbsdk.TimeSeriesQueryResult{
			Metric:          "cpu.usage",
			Tags:            map[string]string{"host": "server1"},
			AggregatedValue: aggVal(42.5),
			Points: []*qrdbsdk.TimeSeriesDataPoint{
				{Metric: "cpu.usage", Value: 40.0, Timestamp: now.Add(-5 * time.Minute)},
				{Metric: "cpu.usage", Value: 45.0, Timestamp: now},
			},
		}
		result := convertFromQRDBQueryResult(qrdbResult)
		require.NotNil(t, result)
		assert.Equal(t, "cpu.usage", result.Metric)
		require.NotNil(t, result.AggregatedValue)
		assert.Equal(t, 42.5, *result.AggregatedValue)
		assert.Len(t, result.Points, 2)
	})

	t.Run("converts with nil points", func(t *testing.T) {
		qrdbResult := &qrdbsdk.TimeSeriesQueryResult{
			Metric:          "mem.free",
			AggregatedValue: aggVal(1024.0),
			Points:          nil,
		}
		result := convertFromQRDBQueryResult(qrdbResult)
		require.NotNil(t, result)
		assert.Nil(t, result.Points)
	})

	t.Run("converts with grouped results", func(t *testing.T) {
		qrdbResult := &qrdbsdk.TimeSeriesQueryResult{
			Metric: "cpu.usage",
			GroupedResults: []*qrdbsdk.TimeSeriesGroupedResult{
				{
					Tags:            map[string]string{"host": "s1"},
					AggregatedValue: aggVal(40.0),
					Points: []*qrdbsdk.TimeSeriesDataPoint{
						{Metric: "cpu.usage", Value: 40.0, Timestamp: now},
					},
				},
				{
					Tags:            map[string]string{"host": "s2"},
					AggregatedValue: aggVal(60.0),
					Points:          nil,
				},
			},
		}
		result := convertFromQRDBQueryResult(qrdbResult)
		require.Len(t, result.GroupedResults, 2)
		assert.Len(t, result.GroupedResults[0].Points, 1)
		assert.Nil(t, result.GroupedResults[1].Points)
	})

	t.Run("converts with nil grouped results", func(t *testing.T) {
		qrdbResult := &qrdbsdk.TimeSeriesQueryResult{
			Metric:         "net.bytes",
			GroupedResults: nil,
		}
		result := convertFromQRDBQueryResult(qrdbResult)
		assert.Nil(t, result.GroupedResults)
	})
}

// ---------------------------------------------------------------------------
// timeSeriesStorageAdapter init and stop (uses real PebbleDB storage)
// ---------------------------------------------------------------------------

func TestTSStorage_InitAndStop(t *testing.T) {
	t.Run("stop with nil storage is no-op", func(t *testing.T) {
		srv := withTempServer(t)
		srv.timeSeriesStorage = nil
		require.NoError(t, srv.stopTimeSeries())
	})

	t.Run("startTimeSeries is no-op when storage is nil (disabled)", func(t *testing.T) {
		srv := withTempServer(t)
		srv.timeSeriesStorage = nil

		require.NoError(t, srv.startTimeSeries())
		require.Nil(t, srv.timeSeriesStorage, "startTimeSeries should not auto-initialize disabled storage")
	})
}

// ---------------------------------------------------------------------------
// timeSeriesStorageAdapter methods via real storage
// ---------------------------------------------------------------------------
