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

package interfaces

import (
	"context"
	"time"
)

// AggFunc represents an aggregation function for time series queries.
type AggFunc uint8

const (
	// AggNone performs no aggregation; returns raw data points.
	AggNone AggFunc = 0
	// AggAvg computes the arithmetic mean of values in the interval.
	AggAvg AggFunc = 1
	// AggSum computes the sum of values in the interval.
	AggSum AggFunc = 2
	// AggMin returns the minimum value in the interval.
	AggMin AggFunc = 3
	// AggMax returns the maximum value in the interval.
	AggMax AggFunc = 4
	// AggCount returns the number of data points in the interval.
	AggCount AggFunc = 5
	// AggPercentile computes a percentile value in the interval.
	AggPercentile AggFunc = 6
	// AggFirst returns the first data point in the interval.
	AggFirst AggFunc = 7
	// AggLast returns the last data point in the interval.
	AggLast AggFunc = 8
)

// aggFuncNames maps AggFunc values to their string representations.
var aggFuncNames = map[AggFunc]string{
	AggNone:       "none",
	AggAvg:        "avg",
	AggSum:        "sum",
	AggMin:        "min",
	AggMax:        "max",
	AggCount:      "count",
	AggPercentile: "percentile",
	AggFirst:      "first",
	AggLast:       "last",
}

// String returns the human-readable name of the aggregation function.
func (f AggFunc) String() string {
	if name, ok := aggFuncNames[f]; ok {
		return name
	}
	return "unknown"
}

// DataPoint represents a single time series measurement.
type DataPoint struct {
	Metric    string
	Value     float64
	Timestamp time.Time
	Tags      map[string]string
}

// QueryRequest defines parameters for a time series query.
type QueryRequest struct {
	Metric      string
	Tags        map[string]string
	StartTime   time.Time
	EndTime     time.Time
	Aggregation AggFunc
	Interval    time.Duration
	GroupBy     []string
	Limit       int
	Percentile  float64
}

// GroupedResult holds aggregated results for a tag group.
type GroupedResult struct {
	Tags            map[string]string
	Points          []*DataPoint
	AggregatedValue *float64
}

// QueryResult holds results from a time series query.
type QueryResult struct {
	Metric          string
	Tags            map[string]string
	Points          []*DataPoint
	AggregatedValue *float64
	GroupedResults  []*GroupedResult
}

// DeleteRequest defines criteria for deleting time series data.
type DeleteRequest struct {
	Metric    string
	Tags      map[string]string
	StartTime time.Time
	EndTime   time.Time
}

// RetentionPolicy defines data retention and downsampling rules for a metric.
type RetentionPolicy struct {
	Duration    time.Duration
	Resolution  time.Duration
	Aggregation AggFunc
}

// TimeSeriesStorage defines the interface for time series storage operations.
// It provides methods for writing, querying, and managing time series data.
// This interface enables dependency injection and testability for time series services.
// Every call takes the caller's context, which carries the identity and tenant
// the storage engine authorizes the call as.
type TimeSeriesStorage interface {
	// WritePoint writes a single data point to the time series storage.
	// Returns an error if the point is invalid or the write fails.
	WritePoint(ctx context.Context, point *DataPoint) error

	// WriteBatch writes multiple data points atomically.
	// Returns an error if any point is invalid or the batch write fails.
	WriteBatch(ctx context.Context, points []DataPoint) error

	// QueryRich performs a time range query with optional filtering and aggregation.
	// Returns the query result containing matching data points or aggregated values.
	QueryRich(ctx context.Context, req *QueryRequest) (*QueryResult, error)

	// DeleteSeries deletes data points matching the request criteria.
	// Can delete by metric name, tags, and/or time range.
	DeleteSeries(ctx context.Context, req *DeleteRequest) error

	// GetRetentionPolicy retrieves the retention policy for a metric.
	// Returns a default policy if none is explicitly configured.
	GetRetentionPolicy(ctx context.Context, metric string) (*RetentionPolicy, error)

	// SetRetentionPolicy sets the retention policy for a metric.
	// The policy defines how long data is retained and downsampling rules.
	SetRetentionPolicy(ctx context.Context, metric string, policy *RetentionPolicy) error

	// ListMetrics returns metric names matching the optional prefix.
	// Limit controls the maximum number of metrics returned.
	ListMetrics(ctx context.Context, prefix string, limit int) ([]string, error)
}
