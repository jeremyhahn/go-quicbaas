//go:build rest && timeseries

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

package rest

import (
	"time"
)

// ========================================
// Time Series Types
// ========================================

// AggregateRequest represents a request to aggregate time series data.
type AggregateRequest struct {
	// EntityType is the entity type to aggregate
	EntityType string `json:"entity_type"`

	// Field is the field to aggregate
	Field string `json:"field"`

	// Function is the aggregation function (sum, avg, min, max, count)
	Function string `json:"function"`

	// StartTime is the start time for the aggregation window
	StartTime time.Time `json:"start_time"`

	// EndTime is the end time for the aggregation window
	EndTime time.Time `json:"end_time"`

	// Interval is the aggregation interval (e.g., "1m", "5m", "1h")
	Interval string `json:"interval,omitempty"`

	// GroupBy is the list of fields to group by
	GroupBy []string `json:"group_by,omitempty"`

	// Filters is a map of field filters
	Filters map[string]interface{} `json:"filters,omitempty"`
}

// AggregateResponse represents the response from an aggregation query.
type AggregateResponse struct {
	// Function is the aggregation function used
	Function string `json:"function"`

	// Field is the field that was aggregated
	Field string `json:"field"`

	// Result is the aggregation result
	Result interface{} `json:"result"`

	// Series is the list of time series data points (if interval specified)
	Series []TimeSeriesPoint `json:"series,omitempty"`

	// GroupedResults is the list of grouped aggregation results
	GroupedResults []GroupedAggregateResult `json:"grouped_results,omitempty"`

	// StartTime is the start time of the aggregation window
	StartTime time.Time `json:"start_time"`

	// EndTime is the end time of the aggregation window
	EndTime time.Time `json:"end_time"`

	// Count is the number of data points aggregated
	Count int64 `json:"count"`

	// Timestamp is when the query was executed
	Timestamp time.Time `json:"timestamp"`
}

// TimeSeriesPoint represents a single time series data point.
type TimeSeriesPoint struct {
	// Time is the timestamp of the data point
	Time time.Time `json:"time"`

	// Value is the aggregated value at this timestamp
	Value interface{} `json:"value"`

	// Count is the number of raw data points in this aggregation
	Count int64 `json:"count,omitempty"`
}

// GroupedAggregateResult represents an aggregation result for a specific group.
type GroupedAggregateResult struct {
	// Group is the group key
	Group map[string]interface{} `json:"group"`

	// Result is the aggregation result for this group
	Result interface{} `json:"result"`

	// Series is the time series data for this group (if interval specified)
	Series []TimeSeriesPoint `json:"series,omitempty"`

	// Count is the number of data points in this group
	Count int64 `json:"count"`
}

// TimeSeriesQueryRequest represents a request to query time series data.
type TimeSeriesQueryRequest struct {
	// EntityType is the entity type to query
	EntityType string `json:"entity_type"`

	// StartTime is the start time for the query window
	StartTime time.Time `json:"start_time"`

	// EndTime is the end time for the query window
	EndTime time.Time `json:"end_time"`

	// Fields is the list of fields to return (empty = all fields)
	Fields []string `json:"fields,omitempty"`

	// Filters is a map of field filters
	Filters map[string]interface{} `json:"filters,omitempty"`

	// OrderBy is the field to order by
	OrderBy string `json:"order_by,omitempty"`

	// Ascending indicates the sort direction
	Ascending bool `json:"ascending,omitempty"`

	// Limit is the maximum number of results to return
	Limit int `json:"limit,omitempty"`

	// Cursor is the pagination cursor
	Cursor string `json:"cursor,omitempty"`
}

// TimeSeriesQueryResponse represents a single time series data entry.
type TimeSeriesQueryResponse struct {
	// ID is the entity ID
	ID uint64 `json:"id"`

	// Timestamp is the timestamp of this entry
	Timestamp time.Time `json:"timestamp"`

	// Data is the entity data
	Data map[string]interface{} `json:"data"`
}

// ========================================
// New TimeSeries Service Request/Response Types
// ========================================

// WritePointRequest represents a request to write a single data point.
type WritePointRequest struct {
	Metric    string            `json:"metric"`
	Tags      map[string]string `json:"tags,omitempty"`
	Value     float64           `json:"value"`
	Timestamp time.Time         `json:"timestamp"`
}

// WritePointResponse represents the response from writing a data point.
type WritePointResponse struct {
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// WriteBatchRequest represents a request to write multiple data points.
type WriteBatchRequest struct {
	Points []WritePointRequest `json:"points"`
}

// WriteBatchResponse represents the response from batch write.
type WriteBatchResponse struct {
	Success       bool      `json:"success"`
	PointsWritten int       `json:"points_written"`
	Error         string    `json:"error,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
}

// TSQueryRequest represents a time series query request.
type TSQueryRequest struct {
	Metric      string            `json:"metric"`
	Tags        map[string]string `json:"tags,omitempty"`
	StartTime   time.Time         `json:"start_time"`
	EndTime     time.Time         `json:"end_time"`
	Aggregation string            `json:"aggregation,omitempty"`
	IntervalSec int64             `json:"interval_seconds,omitempty"`
	GroupBy     []string          `json:"group_by,omitempty"`
	Limit       int               `json:"limit,omitempty"`
	Percentile  float64           `json:"percentile,omitempty"`
}

// TSQueryResponse represents a time series query response.
type TSQueryResponse struct {
	Metric          string            `json:"metric"`
	Tags            map[string]string `json:"tags,omitempty"`
	Points          []TSDataPoint     `json:"points,omitempty"`
	AggregatedValue *float64          `json:"aggregated_value,omitempty"`
	GroupedResults  []TSGroupedResult `json:"grouped_results,omitempty"`
	Timestamp       time.Time         `json:"timestamp"`
}

// TSDataPoint represents a single time series data point in REST API.
type TSDataPoint struct {
	Metric    string            `json:"metric"`
	Tags      map[string]string `json:"tags,omitempty"`
	Value     float64           `json:"value"`
	Timestamp time.Time         `json:"timestamp"`
}

// TSGroupedResult represents grouped query results.
type TSGroupedResult struct {
	Tags            map[string]string `json:"tags"`
	Points          []TSDataPoint     `json:"points,omitempty"`
	AggregatedValue *float64          `json:"aggregated_value,omitempty"`
}

// DeleteSeriesRequest represents a request to delete time series data.
type DeleteSeriesRequest struct {
	Metric    string            `json:"metric"`
	Tags      map[string]string `json:"tags,omitempty"`
	StartTime *time.Time        `json:"start_time,omitempty"`
	EndTime   *time.Time        `json:"end_time,omitempty"`
}

// DeleteSeriesResponse represents the response from delete operation.
type DeleteSeriesResponse struct {
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// TSRetentionPolicy represents retention policy settings.
type TSRetentionPolicy struct {
	DurationSec   int64  `json:"duration_seconds"`
	ResolutionSec int64  `json:"resolution_seconds,omitempty"`
	Aggregation   string `json:"aggregation,omitempty"`
}

// GetRetentionPolicyResponse represents the response for getting retention policy.
type GetRetentionPolicyResponse struct {
	Metric    string             `json:"metric"`
	Policy    *TSRetentionPolicy `json:"policy"`
	Error     string             `json:"error,omitempty"`
	Timestamp time.Time          `json:"timestamp"`
}

// SetRetentionPolicyRequest represents a request to set retention policy.
type SetRetentionPolicyRequest struct {
	Policy *TSRetentionPolicy `json:"policy"`
}

// SetRetentionPolicyResponse represents the response from setting retention policy.
type SetRetentionPolicyResponse struct {
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// ListMetricsRequest represents a request to list metrics.
type ListMetricsRequest struct {
	Prefix string            `json:"prefix,omitempty"`
	Tags   map[string]string `json:"tags,omitempty"`
	Limit  int               `json:"limit,omitempty"`
	Cursor string            `json:"cursor,omitempty"`
}

// ListMetricsResponse represents the response from listing metrics.
type ListMetricsResponse struct {
	Metrics    []string  `json:"metrics"`
	NextCursor string    `json:"next_cursor,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}
