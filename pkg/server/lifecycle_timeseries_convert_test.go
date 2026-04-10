//go:build timeseries && !integration && !raft && !grpc

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//   See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//   Contact licensing@automatethethings.com for commercial licensing options.

package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtimeseries "github.com/jeremyhahn/go-qrdb/pkg/timeseries"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// float64Ptr is a helper to create a *float64 for test data.
func float64Ptr(v float64) *float64 { return &v }

// =============================================================================
// lifecycle_timeseries.go: conversion functions
// =============================================================================

func TestLC_ConvertToQRDBDataPoint(t *testing.T) {
	t.Parallel()

	now := time.Now()
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
}

func TestLC_ConvertToQRDBDataPoint_EmptyTags(t *testing.T) {
	t.Parallel()

	point := &interfaces.DataPoint{
		Metric:    "mem.free",
		Value:     1024.0,
		Timestamp: time.Now(),
		Tags:      nil,
	}

	result := convertToQRDBDataPoint(point)
	require.NotNil(t, result)
	assert.Equal(t, "mem.free", result.Metric)
	assert.Nil(t, result.Tags)
}

func TestLC_ConvertToQRDBQueryRequest(t *testing.T) {
	t.Parallel()

	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()
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
	assert.Equal(t, []string{"host"}, result.GroupBy)
	assert.Equal(t, 100, result.Limit)
	assert.Equal(t, 95.0, result.Percentile)
}

func TestLC_ConvertToQRDBQueryRequest_MinimalFields(t *testing.T) {
	t.Parallel()

	req := &interfaces.QueryRequest{
		Metric: "disk.io",
	}

	result := convertToQRDBQueryRequest(req)
	require.NotNil(t, result)
	assert.Equal(t, "disk.io", result.Metric)
	assert.Nil(t, result.Tags)
	assert.Equal(t, 0, result.Limit)
}

func TestLC_ConvertFromQRDBQueryResult(t *testing.T) {
	t.Parallel()

	now := time.Now()
	aggVal := float64Ptr(42.5)
	qrdbResult := &qrdbtimeseries.QueryResult{
		Metric:          "cpu.usage",
		Tags:            map[string]string{"host": "server1"},
		AggregatedValue: aggVal,
		Points: []*qrdbtimeseries.DataPoint{
			{
				Metric:    "cpu.usage",
				Value:     40.0,
				Timestamp: now.Add(-5 * time.Minute),
				Tags:      map[string]string{"host": "server1"},
			},
			{
				Metric:    "cpu.usage",
				Value:     45.0,
				Timestamp: now,
				Tags:      map[string]string{"host": "server1"},
			},
		},
	}

	result := convertFromQRDBQueryResult(qrdbResult)
	require.NotNil(t, result)
	assert.Equal(t, "cpu.usage", result.Metric)
	require.NotNil(t, result.AggregatedValue)
	assert.Equal(t, 42.5, *result.AggregatedValue)
	assert.Len(t, result.Points, 2)
	assert.Equal(t, 40.0, result.Points[0].Value)
	assert.Equal(t, 45.0, result.Points[1].Value)
}

func TestLC_ConvertFromQRDBQueryResult_NilPoints(t *testing.T) {
	t.Parallel()

	aggVal := float64Ptr(1024.0)
	qrdbResult := &qrdbtimeseries.QueryResult{
		Metric:          "mem.free",
		AggregatedValue: aggVal,
		Points:          nil,
	}

	result := convertFromQRDBQueryResult(qrdbResult)
	require.NotNil(t, result)
	assert.Equal(t, "mem.free", result.Metric)
	assert.Nil(t, result.Points)
}

func TestLC_ConvertFromQRDBQueryResult_WithGroupedResults(t *testing.T) {
	t.Parallel()

	now := time.Now()
	qrdbResult := &qrdbtimeseries.QueryResult{
		Metric:          "cpu.usage",
		AggregatedValue: float64Ptr(50.0),
		GroupedResults: []*qrdbtimeseries.GroupedResult{
			{
				Tags:            map[string]string{"host": "server1"},
				AggregatedValue: float64Ptr(40.0),
				Points: []*qrdbtimeseries.DataPoint{
					{
						Metric:    "cpu.usage",
						Value:     40.0,
						Timestamp: now,
						Tags:      map[string]string{"host": "server1"},
					},
				},
			},
			{
				Tags:            map[string]string{"host": "server2"},
				AggregatedValue: float64Ptr(60.0),
				Points:          nil,
			},
		},
	}

	result := convertFromQRDBQueryResult(qrdbResult)
	require.NotNil(t, result)
	assert.Len(t, result.GroupedResults, 2)

	// First group has points
	require.NotNil(t, result.GroupedResults[0].AggregatedValue)
	assert.Equal(t, 40.0, *result.GroupedResults[0].AggregatedValue)
	assert.Len(t, result.GroupedResults[0].Points, 1)
	assert.Equal(t, 40.0, result.GroupedResults[0].Points[0].Value)

	// Second group has no points
	require.NotNil(t, result.GroupedResults[1].AggregatedValue)
	assert.Equal(t, 60.0, *result.GroupedResults[1].AggregatedValue)
	assert.Nil(t, result.GroupedResults[1].Points)
}

func TestLC_ConvertFromQRDBQueryResult_EmptyGroupedResults(t *testing.T) {
	t.Parallel()

	qrdbResult := &qrdbtimeseries.QueryResult{
		Metric:         "net.bytes",
		GroupedResults: nil,
	}

	result := convertFromQRDBQueryResult(qrdbResult)
	require.NotNil(t, result)
	assert.Nil(t, result.GroupedResults)
}

// =============================================================================
// lifecycle_timeseries.go: startTimeSeries and stopTimeSeries
// =============================================================================

func TestLC_StartTimeSeries_NilStorage(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.timeSeriesStorage = nil
	// When storage is nil, startTimeSeries calls initTimeSeriesStorage
	// which needs a real directory - provide one
	srv.config.TimeSeries.StoragePath = t.TempDir()
	err := srv.startTimeSeries()
	assert.NoError(t, err)
	assert.NotNil(t, srv.timeSeriesStorage)

	// Cleanup
	require.NoError(t, srv.stopTimeSeries())
}

func TestLC_StopTimeSeries_NilStorage(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.timeSeriesStorage = nil
	assert.NoError(t, srv.stopTimeSeries())
}

func TestLC_InitTimeSeriesStorage_Success(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.TimeSeries.StoragePath = t.TempDir()

	err := srv.initTimeSeriesStorage()
	assert.NoError(t, err)
	assert.NotNil(t, srv.timeSeriesStorage)

	// Cleanup
	require.NoError(t, srv.stopTimeSeries())
}

func TestLC_InitTimeSeriesStorage_DefaultPath(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.TimeSeries.StoragePath = ""
	srv.config.Storage.DataDir = t.TempDir()

	err := srv.initTimeSeriesStorage()
	assert.NoError(t, err)
	assert.NotNil(t, srv.timeSeriesStorage)

	// Cleanup
	require.NoError(t, srv.stopTimeSeries())
}

func TestLC_StopTimeSeries_WithStorage(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.TimeSeries.StoragePath = t.TempDir()

	require.NoError(t, srv.initTimeSeriesStorage())
	require.NotNil(t, srv.timeSeriesStorage)

	err := srv.stopTimeSeries()
	assert.NoError(t, err)
	assert.Nil(t, srv.timeSeriesStorage)
}
