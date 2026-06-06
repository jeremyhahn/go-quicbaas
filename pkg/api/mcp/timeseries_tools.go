//go:build mcp

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

// Package mcp implements TimeSeries tool definitions for the Model Context Protocol.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TimeSeriesRequiredFieldError represents a missing required field for a
// timeseries tool operation.
type TimeSeriesRequiredFieldError struct {
	Field string
}

// Error implements the error interface.
func (e *TimeSeriesRequiredFieldError) Error() string {
	return fmt.Sprintf("timeseries: %s is required", e.Field)
}

// TimeSeriesParseError represents a failure to parse a timeseries tool parameter.
type TimeSeriesParseError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *TimeSeriesParseError) Error() string {
	return fmt.Sprintf("timeseries parse error: %s: %s", e.Field, e.Message)
}

// registerTimeSeriesTools registers all time series tools with the registry.
func (r *ToolRegistry) registerTimeSeriesTools() {
	// Write Operations

	r.addTool(ToolDefinition{
		Name:        "qrdb_ts_write_point",
		Description: "Write a single data point to the time series database",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"metric": map[string]interface{}{
					"type":        "string",
					"description": "Metric name (e.g., 'cpu.usage', 'memory.used')",
				},
				"value": map[string]interface{}{
					"type":        "string",
					"description": "Numeric measurement value (as string for precision)",
				},
				"timestamp": map[string]interface{}{
					"type":        "integer",
					"description": "Unix timestamp in nanoseconds (0 = current time)",
				},
				"tags": map[string]interface{}{
					"type":        "string",
					"description": "JSON object with tag key-value pairs for indexing (optional)",
				},
			},
			"required": []string{"metric", "value"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_ts_write_batch",
		Description: "Write multiple data points atomically in a single operation for better throughput",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"points": map[string]interface{}{
					"type":        "string",
					"description": "JSON array of data points with metric, value, timestamp, and tags fields",
				},
			},
			"required": []string{"points"},
		},
	})

	// Query Operations

	r.addTool(ToolDefinition{
		Name:        "qrdb_ts_query",
		Description: "Query time series data with optional aggregation, downsampling, and grouping",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"metric": map[string]interface{}{
					"type":        "string",
					"description": "Metric name to query",
				},
				"start_time": map[string]interface{}{
					"type":        "integer",
					"description": "Inclusive start of time range (Unix nanoseconds)",
				},
				"end_time": map[string]interface{}{
					"type":        "integer",
					"description": "Inclusive end of time range (Unix nanoseconds)",
				},
				"tags": map[string]interface{}{
					"type":        "string",
					"description": "JSON object with tag filters (optional)",
				},
				"aggregation": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"avg", "sum", "min", "max", "count", "percentile", "first", "last"},
					"description": "Aggregation function to apply (optional)",
				},
				"interval_seconds": map[string]interface{}{
					"type":        "integer",
					"description": "Downsampling interval in seconds (0 = no downsampling)",
				},
				"group_by": map[string]interface{}{
					"type":        "string",
					"description": "JSON array of tag names to group by (optional)",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of data points to return (0 = no limit)",
				},
				"percentile": map[string]interface{}{
					"type":        "string",
					"description": "Percentile value (0-100) for percentile aggregation (optional)",
				},
			},
			"required": []string{"metric", "start_time", "end_time"},
		},
	})

	// Delete Operations

	r.addTool(ToolDefinition{
		Name:        "qrdb_ts_delete_series",
		Description: "Delete a time series or specific data points within a time range",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"metric": map[string]interface{}{
					"type":        "string",
					"description": "Metric name to delete",
				},
				"tags": map[string]interface{}{
					"type":        "string",
					"description": "JSON object with tag filters (empty = delete all series with this metric)",
				},
				"start_time": map[string]interface{}{
					"type":        "integer",
					"description": "Start of time range to delete (Unix nanoseconds, 0 = from beginning)",
				},
				"end_time": map[string]interface{}{
					"type":        "integer",
					"description": "End of time range to delete (Unix nanoseconds, 0 = to end)",
				},
			},
			"required": []string{"metric"},
		},
	})

	// Retention Policy Operations

	r.addTool(ToolDefinition{
		Name:        "qrdb_ts_get_retention_policy",
		Description: "Retrieve the retention policy for a specific metric",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"metric": map[string]interface{}{
					"type":        "string",
					"description": "Metric name to get retention policy for",
				},
			},
			"required": []string{"metric"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_ts_set_retention_policy",
		Description: "Set the retention policy for a metric including duration, resolution, and aggregation",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"metric": map[string]interface{}{
					"type":        "string",
					"description": "Metric name to set retention policy for",
				},
				"duration_seconds": map[string]interface{}{
					"type":        "integer",
					"description": "How long to keep data in seconds (0 = infinite retention)",
				},
				"resolution_seconds": map[string]interface{}{
					"type":        "integer",
					"description": "Downsampling resolution in seconds (0 = no downsampling)",
				},
				"aggregation": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"avg", "sum", "min", "max", "count", "percentile", "first", "last"},
					"description": "Aggregation function for downsampling (required if resolution_seconds > 0)",
				},
			},
			"required": []string{"metric", "duration_seconds"},
		},
	})

	// Metric Operations

	r.addTool(ToolDefinition{
		Name:        "qrdb_ts_list_metrics",
		Description: "List all available metrics with optional filtering and pagination",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prefix": map[string]interface{}{
					"type":        "string",
					"description": "Filter metrics by name prefix (optional)",
				},
				"tags": map[string]interface{}{
					"type":        "string",
					"description": "JSON object with tag filters (optional)",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of metrics to return (default: 100)",
				},
				"cursor": map[string]interface{}{
					"type":        "string",
					"description": "Pagination cursor from previous response (optional)",
				},
			},
			"required": []string{},
		},
	})
}

// =============================================================================
// TimeSeries Tool Execution Handlers
// =============================================================================

func (r *ToolRegistry) executeTimeSeriesWritePoint(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	metric := getString(args, "metric")
	if metric == "" {
		return nil, &TimeSeriesRequiredFieldError{Field: "metric"}
	}

	valueStr := getString(args, "value")
	if valueStr == "" {
		return nil, &TimeSeriesRequiredFieldError{Field: "value"}
	}

	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return nil, &TimeSeriesParseError{
			Field:   "value",
			Message: fmt.Sprintf("invalid numeric value: %s", valueStr),
		}
	}

	// Parse timestamp: nanoseconds since Unix epoch, 0 means current time.
	ts := time.Now()
	if tsNano := getInt64(args, "timestamp"); tsNano != 0 {
		ts = time.Unix(0, tsNano)
	}

	// Parse optional tags from JSON string.
	tags, err := parseTagsJSON(args)
	if err != nil {
		return nil, err
	}

	req := &service.WritePointRequest{
		Metric:    metric,
		Value:     value,
		Timestamp: ts,
		Tags:      tags,
	}

	resp, err := r.service.WritePoint(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(resp)
}

// batchPointJSON represents a single data point in a JSON batch payload.
type batchPointJSON struct {
	Metric    string            `json:"metric"`
	Value     string            `json:"value"`
	Timestamp int64             `json:"timestamp"`
	Tags      map[string]string `json:"tags,omitempty"`
}

func (r *ToolRegistry) executeTimeSeriesWriteBatch(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	pointsJSON := getString(args, "points")
	if pointsJSON == "" {
		return nil, &TimeSeriesRequiredFieldError{Field: "points"}
	}

	var rawPoints []batchPointJSON
	if err := json.Unmarshal([]byte(pointsJSON), &rawPoints); err != nil {
		return nil, &TimeSeriesParseError{
			Field:   "points",
			Message: fmt.Sprintf("invalid JSON array: %s", err.Error()),
		}
	}

	if len(rawPoints) == 0 {
		return nil, &TimeSeriesParseError{
			Field:   "points",
			Message: "array must contain at least one data point",
		}
	}

	points := make([]*service.WritePointRequest, 0, len(rawPoints))
	for i, raw := range rawPoints {
		if raw.Metric == "" {
			return nil, &TimeSeriesParseError{
				Field:   fmt.Sprintf("points[%d].metric", i),
				Message: "metric is required",
			}
		}

		value, err := strconv.ParseFloat(raw.Value, 64)
		if err != nil {
			return nil, &TimeSeriesParseError{
				Field:   fmt.Sprintf("points[%d].value", i),
				Message: fmt.Sprintf("invalid numeric value: %s", raw.Value),
			}
		}

		ts := time.Now()
		if raw.Timestamp != 0 {
			ts = time.Unix(0, raw.Timestamp)
		}

		points = append(points, &service.WritePointRequest{
			Metric:    raw.Metric,
			Value:     value,
			Timestamp: ts,
			Tags:      raw.Tags,
		})
	}

	req := &service.WriteBatchRequest{
		Points: points,
	}

	resp, err := r.service.WriteBatch(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(resp)
}

func (r *ToolRegistry) executeTimeSeriesQuery(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	metric := getString(args, "metric")
	if metric == "" {
		return nil, &TimeSeriesRequiredFieldError{Field: "metric"}
	}

	startNano := getInt64(args, "start_time")
	if startNano == 0 {
		return nil, &TimeSeriesRequiredFieldError{Field: "start_time"}
	}

	endNano := getInt64(args, "end_time")
	if endNano == 0 {
		return nil, &TimeSeriesRequiredFieldError{Field: "end_time"}
	}

	startTime := time.Unix(0, startNano)
	endTime := time.Unix(0, endNano)

	// Parse optional tags from JSON string.
	tags, err := parseTagsJSON(args)
	if err != nil {
		return nil, err
	}

	// Parse optional group_by from JSON string.
	var groupBy []string
	if groupByStr := getString(args, "group_by"); groupByStr != "" {
		if err := json.Unmarshal([]byte(groupByStr), &groupBy); err != nil {
			return nil, &TimeSeriesParseError{
				Field:   "group_by",
				Message: fmt.Sprintf("invalid JSON array: %s", err.Error()),
			}
		}
	}

	// Parse optional percentile from string.
	var percentile float64
	if pctStr := getString(args, "percentile"); pctStr != "" {
		pct, parseErr := strconv.ParseFloat(pctStr, 64)
		if parseErr != nil {
			return nil, &TimeSeriesParseError{
				Field:   "percentile",
				Message: fmt.Sprintf("invalid numeric value: %s", pctStr),
			}
		}
		percentile = pct
	}

	req := &service.TSQueryRequest{
		Metric:      metric,
		Tags:        tags,
		StartTime:   startTime,
		EndTime:     endTime,
		Aggregation: getString(args, "aggregation"),
		IntervalSec: getInt64(args, "interval_seconds"),
		GroupBy:     groupBy,
		Limit:       getInt(args, "limit"),
		Percentile:  percentile,
	}

	resp, err := r.service.TSQuery(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(resp)
}

func (r *ToolRegistry) executeTimeSeriesDeleteSeries(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	metric := getString(args, "metric")
	if metric == "" {
		return nil, &TimeSeriesRequiredFieldError{Field: "metric"}
	}

	// Parse optional tags from JSON string.
	tags, err := parseTagsJSON(args)
	if err != nil {
		return nil, err
	}

	req := &service.TSDeleteRequest{
		Metric: metric,
		Tags:   tags,
	}

	// Parse optional time range boundaries.
	if startNano := getInt64(args, "start_time"); startNano != 0 {
		t := time.Unix(0, startNano)
		req.StartTime = &t
	}
	if endNano := getInt64(args, "end_time"); endNano != 0 {
		t := time.Unix(0, endNano)
		req.EndTime = &t
	}

	resp, err := r.service.DeleteSeries(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(resp)
}

func (r *ToolRegistry) executeTimeSeriesGetRetentionPolicy(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	metric := getString(args, "metric")
	if metric == "" {
		return nil, &TimeSeriesRequiredFieldError{Field: "metric"}
	}

	req := &service.TSGetRetentionPolicyRequest{
		Metric: metric,
	}

	resp, err := r.service.GetRetentionPolicy(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(resp)
}

func (r *ToolRegistry) executeTimeSeriesSetRetentionPolicy(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	metric := getString(args, "metric")
	if metric == "" {
		return nil, &TimeSeriesRequiredFieldError{Field: "metric"}
	}

	durationSec := getInt64(args, "duration_seconds")

	duration := time.Duration(durationSec) * time.Second

	resolutionSec := getInt64(args, "resolution_seconds")
	resolution := time.Duration(resolutionSec) * time.Second

	aggregation := getString(args, "aggregation")

	req := &service.TSSetRetentionPolicyRequest{
		Metric: metric,
		Policy: &service.TSRetentionPolicy{
			Duration:    duration,
			Resolution:  resolution,
			Aggregation: aggregation,
		},
	}

	resp, err := r.service.SetRetentionPolicy(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(resp)
}

func (r *ToolRegistry) executeTimeSeriesListMetrics(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Parse optional tags from JSON string.
	tags, err := parseTagsJSON(args)
	if err != nil {
		return nil, err
	}

	limit := getInt(args, "limit")
	if limit == 0 {
		limit = 100
	}

	req := &service.TSListMetricsRequest{
		Prefix: getString(args, "prefix"),
		Tags:   tags,
		Limit:  limit,
		Cursor: getString(args, "cursor"),
	}

	resp, err := r.service.ListMetrics(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(resp)
}

// =============================================================================
// Helper Functions
// =============================================================================

// isTimeSeriesTool checks if a tool name is a timeseries tool.
func isTimeSeriesTool(name string) bool {
	switch name {
	case "qrdb_ts_write_point",
		"qrdb_ts_write_batch",
		"qrdb_ts_query",
		"qrdb_ts_delete_series",
		"qrdb_ts_get_retention_policy",
		"qrdb_ts_set_retention_policy",
		"qrdb_ts_list_metrics":
		return true
	default:
		return false
	}
}

// executeTimeSeriesTool dispatches timeseries tool execution to the
// appropriate handler.
func (r *ToolRegistry) executeTimeSeriesTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "qrdb_ts_write_point":
		return r.executeTimeSeriesWritePoint(ctx, args)
	case "qrdb_ts_write_batch":
		return r.executeTimeSeriesWriteBatch(ctx, args)
	case "qrdb_ts_query":
		return r.executeTimeSeriesQuery(ctx, args)
	case "qrdb_ts_delete_series":
		return r.executeTimeSeriesDeleteSeries(ctx, args)
	case "qrdb_ts_get_retention_policy":
		return r.executeTimeSeriesGetRetentionPolicy(ctx, args)
	case "qrdb_ts_set_retention_policy":
		return r.executeTimeSeriesSetRetentionPolicy(ctx, args)
	case "qrdb_ts_list_metrics":
		return r.executeTimeSeriesListMetrics(ctx, args)
	default:
		return nil, api.NewUnknownToolError(name)
	}
}

// parseTagsJSON parses the "tags" argument from a JSON string into a
// map[string]string. Returns nil without error when no tags are provided.
func parseTagsJSON(args map[string]interface{}) (map[string]string, error) {
	tagsStr := getString(args, "tags")
	if tagsStr == "" {
		return nil, nil
	}

	var tags map[string]string
	if err := json.Unmarshal([]byte(tagsStr), &tags); err != nil {
		return nil, &TimeSeriesParseError{
			Field:   "tags",
			Message: fmt.Sprintf("invalid JSON object: %s", err.Error()),
		}
	}

	return tags, nil
}

// getInt64 extracts an int64 argument from the args map.
func getInt64(args map[string]interface{}, key string) int64 {
	switch v := args[key].(type) {
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	case uint64:
		return int64(v) //nolint:gosec // G115: uint64 to int64 conversion in type helper
	case string:
		if v != "" {
			i, _ := strconv.ParseInt(v, 10, 64)
			return i
		}
	}
	return 0
}
