//go:build timeseries && !integration

package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtimeseries "github.com/jeremyhahn/go-qrdb/pkg/timeseries"

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
		assert.Equal(t, qrdbtimeseries.AggFunc(interfaces.AggAvg), result.Aggregation)
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
		qrdbResult := &qrdbtimeseries.QueryResult{
			Metric:          "cpu.usage",
			Tags:            map[string]string{"host": "server1"},
			AggregatedValue: aggVal(42.5),
			Points: []*qrdbtimeseries.DataPoint{
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
		qrdbResult := &qrdbtimeseries.QueryResult{
			Metric:          "mem.free",
			AggregatedValue: aggVal(1024.0),
			Points:          nil,
		}
		result := convertFromQRDBQueryResult(qrdbResult)
		require.NotNil(t, result)
		assert.Nil(t, result.Points)
	})

	t.Run("converts with grouped results", func(t *testing.T) {
		qrdbResult := &qrdbtimeseries.QueryResult{
			Metric: "cpu.usage",
			GroupedResults: []*qrdbtimeseries.GroupedResult{
				{
					Tags:            map[string]string{"host": "s1"},
					AggregatedValue: aggVal(40.0),
					Points: []*qrdbtimeseries.DataPoint{
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
		qrdbResult := &qrdbtimeseries.QueryResult{
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
	t.Run("init with explicit path and stop", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.TimeSeries.StoragePath = t.TempDir()

		require.NoError(t, srv.initTimeSeriesStorage())
		require.NotNil(t, srv.timeSeriesStorage)

		require.NoError(t, srv.stopTimeSeries())
		assert.Nil(t, srv.timeSeriesStorage)
	})

	t.Run("init with default path from data dir", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.TimeSeries.StoragePath = ""
		srv.config.Storage.DataDir = t.TempDir()

		require.NoError(t, srv.initTimeSeriesStorage())
		require.NotNil(t, srv.timeSeriesStorage)

		require.NoError(t, srv.stopTimeSeries())
	})

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

func TestTSStorage_WriteAndQuery(t *testing.T) {
	srv := withTempServer(t)
	srv.config.TimeSeries.StoragePath = t.TempDir()
	require.NoError(t, srv.initTimeSeriesStorage())
	t.Cleanup(func() {
		srv.stopTimeSeries()
	})

	storage := srv.timeSeriesStorage

	t.Run("WritePoint succeeds", func(t *testing.T) {
		point := &interfaces.DataPoint{
			Metric:    "test.metric",
			Value:     42.0,
			Timestamp: time.Now(),
			Tags:      map[string]string{"host": "test"},
		}
		err := storage.WritePoint(t.Context(), point)
		require.NoError(t, err)
	})

	t.Run("WriteBatch succeeds", func(t *testing.T) {
		points := []interfaces.DataPoint{
			{Metric: "batch.metric", Value: 1.0, Timestamp: time.Now()},
			{Metric: "batch.metric", Value: 2.0, Timestamp: time.Now()},
		}
		err := storage.WriteBatch(points)
		require.NoError(t, err)
	})

	t.Run("QueryRich returns results", func(t *testing.T) {
		req := &interfaces.QueryRequest{
			Metric:    "test.metric",
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now().Add(1 * time.Hour),
		}
		result, err := storage.QueryRich(t.Context(), req)
		require.NoError(t, err)
		require.NotNil(t, result)
	})

	t.Run("DeleteSeries succeeds", func(t *testing.T) {
		req := &interfaces.DeleteRequest{
			Metric:    "test.metric",
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now().Add(1 * time.Hour),
		}
		err := storage.DeleteSeries(t.Context(), req)
		require.NoError(t, err)
	})

	t.Run("ListMetrics returns results", func(t *testing.T) {
		metrics, err := storage.ListMetrics("", 100)
		require.NoError(t, err)
		// May or may not have metrics at this point
		_ = metrics
	})

	t.Run("SetRetentionPolicy and GetRetentionPolicy", func(t *testing.T) {
		policy := &interfaces.RetentionPolicy{
			Duration:    24 * time.Hour,
			Resolution:  time.Minute,
			Aggregation: interfaces.AggAvg,
		}
		err := storage.SetRetentionPolicy("retention.metric", policy)
		require.NoError(t, err)

		got, err := storage.GetRetentionPolicy("retention.metric")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, 24*time.Hour, got.Duration)
	})
}
