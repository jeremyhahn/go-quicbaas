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

package mcp

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// timeseriesHandler handles TimeSeries-specific request/response transformations for MCP transport.
type timeseriesHandler struct {
	transport *Transport
}

// newTimeseriesHandler creates a new TimeSeries handler.
func newTimeseriesHandler(t *Transport) *timeseriesHandler {
	return &timeseriesHandler{transport: t}
}

// transformRequest transforms SDK timeseries requests to MCP tool arguments format.
func (h *timeseriesHandler) transformRequest(method string, req interface{}) (map[string]interface{}, error) {
	switch method {
	case "Write", "TSWritePoint":
		return h.transformWritePoint(req)
	case "WriteBatch", "TSWriteBatch":
		return h.transformWriteBatch(req)
	case "Query", "TSQuery":
		return h.transformQuery(req)
	case "DeleteSeries", "TSDeleteSeries":
		return h.transformDeleteSeries(req)
	case "GetRetentionPolicy", "TSGetRetentionPolicy":
		return h.transformGetRetentionPolicy(req)
	case "SetRetentionPolicy", "TSSetRetentionPolicy":
		return h.transformSetRetentionPolicy(req)
	case "ListMetrics", "TSListMetrics":
		return h.transformListMetrics(req)
	case "SetRetention":
		return h.transformSetRetention(req)
	case "GetRetention":
		return h.transformGetRetention(req)
	default:
		// Fall back to generic conversion
		return h.transport.convertToArguments(req), nil
	}
}

// transformResponse transforms MCP tool response to SDK timeseries types.
func (h *timeseriesHandler) transformResponse(method string, req, resp interface{}) (interface{}, error) {
	switch method {
	case "Write", "TSWritePoint":
		return h.transformWritePointResponse(resp)
	case "WriteBatch", "TSWriteBatch":
		return h.transformWriteBatchResponse(resp)
	case "Query", "TSQuery":
		return h.transformQueryResponse(resp)
	case "DeleteSeries", "TSDeleteSeries":
		return h.transformDeleteSeriesResponse(resp)
	case "GetRetentionPolicy", "TSGetRetentionPolicy":
		return h.transformGetRetentionPolicyResponse(req, resp)
	case "SetRetentionPolicy", "TSSetRetentionPolicy":
		return h.transformSetRetentionPolicyResponse(resp)
	case "ListMetrics", "TSListMetrics":
		return h.transformListMetricsResponse(resp)
	case "SetRetention":
		return nil, nil
	case "GetRetention":
		return h.transformGetRetentionResponse(req, resp)
	default:
		return resp, nil
	}
}

// Request transformations

func (h *timeseriesHandler) transformWritePoint(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*timeseries.WriteRequest)
	if !ok {
		// Try map-based request from CLI
		if mapReq, ok := req.(map[string]interface{}); ok {
			return h.transformWritePointFromMap(mapReq)
		}
		return nil, errors.NewInvalidInput("timeseriesHandler.transformWritePoint",
			"invalid request type, expected *timeseries.WriteRequest")
	}

	args := map[string]interface{}{
		"metric": sdkReq.Metric,
		"value":  formatFloatValue(sdkReq.Value),
	}

	if !sdkReq.Timestamp.IsZero() {
		args["timestamp"] = sdkReq.Timestamp.UnixNano()
	}

	if len(sdkReq.Tags) > 0 {
		tagsJSON, err := json.Marshal(sdkReq.Tags)
		if err != nil {
			return nil, errors.NewInternal("timeseriesHandler.transformWritePoint",
				"failed to marshal tags: "+err.Error())
		}
		args["tags"] = string(tagsJSON)
	}

	return args, nil
}

func (h *timeseriesHandler) transformWritePointFromMap(req map[string]interface{}) (map[string]interface{}, error) {
	args := map[string]interface{}{}

	if metric, ok := req["metric"].(string); ok {
		args["metric"] = metric
	}
	if value, ok := req["value"].(float64); ok {
		args["value"] = formatFloatValue(value)
	} else if value, ok := req["value"].(string); ok {
		args["value"] = value
	}
	if ts, ok := req["timestamp"].(time.Time); ok {
		args["timestamp"] = ts.UnixNano()
	} else if tsNano, ok := req["timestamp"].(int64); ok {
		args["timestamp"] = tsNano
	}
	if tags, ok := req["tags"].(map[string]string); ok && len(tags) > 0 {
		tagsJSON, err := json.Marshal(tags)
		if err != nil {
			return nil, errors.NewInternal("timeseriesHandler.transformWritePointFromMap",
				"failed to marshal tags: "+err.Error())
		}
		args["tags"] = string(tagsJSON)
	}

	return args, nil
}

func (h *timeseriesHandler) transformWriteBatch(req interface{}) (map[string]interface{}, error) {
	// For batch writes, convert the points array to JSON string
	if mapReq, ok := req.(map[string]interface{}); ok {
		if points, ok := mapReq["points"]; ok {
			pointsJSON, err := json.Marshal(points)
			if err != nil {
				return nil, errors.NewInternal("timeseriesHandler.transformWriteBatch",
					"failed to marshal points: "+err.Error())
			}
			return map[string]interface{}{
				"points": string(pointsJSON),
			}, nil
		}
	}

	// Fall back to generic conversion
	return h.transport.convertToArguments(req), nil
}

func (h *timeseriesHandler) transformQuery(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*timeseries.QueryRequest)
	if !ok {
		// Try map-based request from CLI
		if mapReq, ok := req.(map[string]interface{}); ok {
			return h.transformQueryFromMap(mapReq)
		}
		return nil, errors.NewInvalidInput("timeseriesHandler.transformQuery",
			"invalid request type, expected *timeseries.QueryRequest")
	}

	args := map[string]interface{}{
		"metric":     sdkReq.Metric,
		"start_time": sdkReq.Start.UnixNano(),
		"end_time":   sdkReq.End.UnixNano(),
	}

	if len(sdkReq.Tags) > 0 {
		tagsJSON, err := json.Marshal(sdkReq.Tags)
		if err != nil {
			return nil, errors.NewInternal("timeseriesHandler.transformQuery",
				"failed to marshal tags: "+err.Error())
		}
		args["tags"] = string(tagsJSON)
	}

	if sdkReq.Aggregate != timeseries.AggregateNone {
		args["aggregation"] = strings.ToLower(sdkReq.Aggregate.String())
	}

	if sdkReq.Interval > 0 {
		args["interval_seconds"] = int64(sdkReq.Interval.Seconds())
	}

	return args, nil
}

func (h *timeseriesHandler) transformQueryFromMap(req map[string]interface{}) (map[string]interface{}, error) {
	args := map[string]interface{}{}

	if metric, ok := req["metric"].(string); ok {
		args["metric"] = metric
	}
	if start, ok := req["start_time"].(time.Time); ok {
		args["start_time"] = start.UnixNano()
	} else if startNano, ok := req["start_time"].(int64); ok {
		args["start_time"] = startNano
	}
	if end, ok := req["end_time"].(time.Time); ok {
		args["end_time"] = end.UnixNano()
	} else if endNano, ok := req["end_time"].(int64); ok {
		args["end_time"] = endNano
	}
	if tags, ok := req["tags"].(map[string]string); ok && len(tags) > 0 {
		tagsJSON, err := json.Marshal(tags)
		if err != nil {
			return nil, errors.NewInternal("timeseriesHandler.transformQueryFromMap",
				"failed to marshal tags: "+err.Error())
		}
		args["tags"] = string(tagsJSON)
	}
	if agg, ok := req["aggregation"].(string); ok {
		args["aggregation"] = agg
	}
	if interval, ok := req["interval_seconds"].(int64); ok {
		args["interval_seconds"] = interval
	} else if interval, ok := req["interval_sec"].(int32); ok {
		args["interval_seconds"] = int64(interval)
	}
	if groupBy, ok := req["group_by"].([]string); ok && len(groupBy) > 0 {
		groupByJSON, err := json.Marshal(groupBy)
		if err != nil {
			return nil, errors.NewInternal("timeseriesHandler.transformQueryFromMap",
				"failed to marshal group_by: "+err.Error())
		}
		args["group_by"] = string(groupByJSON)
	}
	if limit, ok := req["limit"].(int); ok {
		args["limit"] = limit
	}
	if percentile, ok := req["percentile"].(float64); ok {
		args["percentile"] = formatFloatValue(percentile)
	}

	return args, nil
}

func (h *timeseriesHandler) transformDeleteSeries(req interface{}) (map[string]interface{}, error) {
	if mapReq, ok := req.(map[string]interface{}); ok {
		args := map[string]interface{}{}
		if metric, ok := mapReq["metric"].(string); ok {
			args["metric"] = metric
		}
		if tags, ok := mapReq["tags"].(map[string]string); ok && len(tags) > 0 {
			tagsJSON, err := json.Marshal(tags)
			if err != nil {
				return nil, errors.NewInternal("timeseriesHandler.transformDeleteSeries",
					"failed to marshal tags: "+err.Error())
			}
			args["tags"] = string(tagsJSON)
		}
		if start, ok := mapReq["start_time"].(time.Time); ok {
			args["start_time"] = start.UnixNano()
		} else if startNano, ok := mapReq["start_time"].(int64); ok {
			args["start_time"] = startNano
		}
		if end, ok := mapReq["end_time"].(time.Time); ok {
			args["end_time"] = end.UnixNano()
		} else if endNano, ok := mapReq["end_time"].(int64); ok {
			args["end_time"] = endNano
		}
		return args, nil
	}
	return h.transport.convertToArguments(req), nil
}

func (h *timeseriesHandler) transformGetRetentionPolicy(req interface{}) (map[string]interface{}, error) {
	if mapReq, ok := req.(map[string]interface{}); ok {
		if metric, ok := mapReq["metric"].(string); ok {
			return map[string]interface{}{"metric": metric}, nil
		}
	}
	return h.transport.convertToArguments(req), nil
}

func (h *timeseriesHandler) transformSetRetentionPolicy(req interface{}) (map[string]interface{}, error) {
	if mapReq, ok := req.(map[string]interface{}); ok {
		args := map[string]interface{}{}
		if metric, ok := mapReq["metric"].(string); ok {
			args["metric"] = metric
		}
		if policy, ok := mapReq["policy"].(map[string]interface{}); ok {
			if duration, ok := policy["duration"].(time.Duration); ok {
				args["duration_seconds"] = int64(duration.Seconds())
			} else if durationSec, ok := policy["duration_seconds"].(int64); ok {
				args["duration_seconds"] = durationSec
			}
			if resolution, ok := policy["resolution"].(time.Duration); ok {
				args["resolution_seconds"] = int64(resolution.Seconds())
			} else if resolutionSec, ok := policy["resolution_seconds"].(int64); ok {
				args["resolution_seconds"] = resolutionSec
			}
			if agg, ok := policy["aggregation"].(string); ok {
				args["aggregation"] = agg
			}
		}
		return args, nil
	}
	return h.transport.convertToArguments(req), nil
}

func (h *timeseriesHandler) transformListMetrics(req interface{}) (map[string]interface{}, error) {
	if mapReq, ok := req.(map[string]interface{}); ok {
		args := map[string]interface{}{}
		if prefix, ok := mapReq["prefix"].(string); ok {
			args["prefix"] = prefix
		}
		if tags, ok := mapReq["tags"].(map[string]string); ok && len(tags) > 0 {
			tagsJSON, err := json.Marshal(tags)
			if err != nil {
				return nil, errors.NewInternal("timeseriesHandler.transformListMetrics",
					"failed to marshal tags: "+err.Error())
			}
			args["tags"] = string(tagsJSON)
		}
		if limit, ok := mapReq["limit"].(int); ok {
			args["limit"] = limit
		}
		if cursor, ok := mapReq["cursor"].(string); ok {
			args["cursor"] = cursor
		}
		return args, nil
	}
	return h.transport.convertToArguments(req), nil
}

// Response transformations

func (h *timeseriesHandler) transformWritePointResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return map[string]interface{}{"success": true}, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return resp, nil
	}

	return map[string]interface{}{
		"success":   tsGetBool(m, "success"),
		"timestamp": tsGetTime(m, "timestamp"),
	}, nil
}

func (h *timeseriesHandler) transformWriteBatchResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return map[string]interface{}{"success": true}, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return resp, nil
	}

	return map[string]interface{}{
		"success":        tsGetBool(m, "success"),
		"points_written": tsGetInt64(m, "points_written"),
		"timestamp":      tsGetTime(m, "timestamp"),
	}, nil
}

func (h *timeseriesHandler) transformQueryResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return &timeseries.QueryResponse{Series: []timeseries.Point{}}, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return resp, nil
	}

	response := &timeseries.QueryResponse{
		Series: []timeseries.Point{},
	}

	if points, ok := m["points"].([]interface{}); ok {
		for _, p := range points {
			if pm, ok := p.(map[string]interface{}); ok {
				point := timeseries.Point{
					Timestamp: tsGetTime(pm, "timestamp"),
					Value:     tsGetFloat64(pm, "value"),
				}
				response.Series = append(response.Series, point)
			}
		}
	}

	return response, nil
}

func (h *timeseriesHandler) transformDeleteSeriesResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return map[string]interface{}{"success": true}, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return resp, nil
	}

	return map[string]interface{}{
		"success":   tsGetBool(m, "success"),
		"timestamp": tsGetTime(m, "timestamp"),
	}, nil
}

func (h *timeseriesHandler) transformGetRetentionPolicyResponse(req, resp interface{}) (interface{}, error) {
	if resp == nil {
		return nil, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return resp, nil
	}

	// The result is read by decodeRetentionResult: go-qrdb's native
	// top-level policy, the nested policy, or the older *_seconds fields.
	// The native result names no metric, so the request's metric stands in.
	policy := decodeRetentionResult(m)
	metric := tsGetString(m, "metric")
	if metric == "" {
		if mapReq, ok := req.(map[string]interface{}); ok {
			metric, _ = mapReq["metric"].(string)
		}
	}

	return map[string]interface{}{
		"metric": metric,
		"policy": map[string]interface{}{
			"duration":    int64(policy.duration / time.Second),
			"resolution":  int64(policy.resolution / time.Second),
			"aggregation": policy.aggregation,
		},
		"timestamp": tsGetTime(m, "timestamp"),
	}, nil
}

func (h *timeseriesHandler) transformSetRetentionPolicyResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return map[string]interface{}{"success": true}, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return resp, nil
	}

	return map[string]interface{}{
		"success":   tsGetBool(m, "success"),
		"timestamp": tsGetTime(m, "timestamp"),
	}, nil
}

func (h *timeseriesHandler) transformListMetricsResponse(resp interface{}) (interface{}, error) {
	if resp == nil {
		return map[string]interface{}{"metrics": []string{}}, nil
	}

	m, ok := resp.(map[string]interface{})
	if !ok {
		return resp, nil
	}

	var metrics []string
	if metricsArr, ok := m["metrics"].([]interface{}); ok {
		for _, metric := range metricsArr {
			if s, ok := metric.(string); ok {
				metrics = append(metrics, s)
			}
		}
	}

	return map[string]interface{}{
		"metrics":     metrics,
		"next_cursor": tsGetString(m, "next_cursor"),
		"timestamp":   tsGetTime(m, "timestamp"),
	}, nil
}

// transformSetRetention turns the SDK's typed SetRetentionRequest into the
// arguments of the server's qrdb_ts_set_retention_policy tool: whole
// seconds and the lower-case aggregation name.
func (h *timeseriesHandler) transformSetRetention(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*timeseries.SetRetentionRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.timeseries.SetRetention", "invalid request type")
	}
	args := map[string]interface{}{
		"metric":           r.Metric,
		"duration_seconds": int64(r.Policy.Duration / time.Second),
	}
	if r.Policy.Resolution > 0 {
		args["resolution_seconds"] = int64(r.Policy.Resolution / time.Second)
	}
	if name := r.Policy.Aggregation.WireName(); name != "" {
		args["aggregation"] = name
	}
	return args, nil
}

// transformGetRetention turns the SDK's typed GetRetentionRequest into the
// arguments of the server's qrdb_ts_get_retention_policy tool.
func (h *timeseriesHandler) transformGetRetention(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*timeseries.GetRetentionRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.timeseries.GetRetention", "invalid request type")
	}
	return map[string]interface{}{"metric": r.Metric}, nil
}

// transformGetRetentionResponse decodes the qrdb_ts_get_retention_policy
// result with decodeRetentionResult and always reports the requested
// metric, since go-qrdb's native result names none. A nil result, or one
// with no duration, resolution or aggregation, has a nil Policy.
func (h *timeseriesHandler) transformGetRetentionResponse(req, resp interface{}) (interface{}, error) {
	m, _ := resp.(map[string]interface{})
	out := &timeseries.GetRetentionResponse{Metric: tsGetString(m, "metric")}
	if r, ok := req.(*timeseries.GetRetentionRequest); ok && r != nil && r.Metric != "" {
		out.Metric = r.Metric
	}
	if m == nil {
		return out, nil
	}
	policy := decodeRetentionResult(m)
	if policy.isZero() {
		return out, nil
	}
	out.Policy = &timeseries.RetentionPolicy{
		Duration:    policy.duration,
		Resolution:  policy.resolution,
		Aggregation: timeseries.ParseAggregateFunction(policy.aggregation),
	}
	return out, nil
}

// retentionResult is a decoded qrdb_ts_get_retention_policy result.
// aggregation is go-qrdb's lower-case aggregation name, empty for none.
type retentionResult struct {
	duration    time.Duration
	resolution  time.Duration
	aggregation string
}

func (r retentionResult) isZero() bool {
	return r.duration == 0 && r.resolution == 0 && r.aggregation == ""
}

// qrdbAggFuncNames maps go-qrdb's timeseries.AggFunc numbers to their
// names. go-qrdb orders AggFunc avg, sum, min, max, count, percentile,
// first, last from 1; the SDK's AggregateFunction is ordered differently,
// so the number is mapped by name and never cast.
var qrdbAggFuncNames = map[int64]string{
	1: "avg", 2: "sum", 3: "min", 4: "max", 5: "count",
	6: "percentile", 7: "first", 8: "last",
}

// retentionAggregationName reads an aggregation that is either go-qrdb's
// AggFunc number or a name string.
func retentionAggregationName(v interface{}) string {
	switch a := v.(type) {
	case string:
		return strings.ToLower(a)
	case float64:
		return qrdbAggFuncNames[int64(a)]
	case int64:
		return qrdbAggFuncNames[a]
	case int:
		return qrdbAggFuncNames[int64(a)]
	default:
		return ""
	}
}

// decodeRetentionResult reads the qrdb_ts_get_retention_policy tool result.
// The unified server serves go-qrdb's own tool, which answers with
// json.Marshal of *timeseries.RetentionPolicy at the top level:
// {"duration": <ns>, "resolution": <ns>, "aggregation": <AggFunc number>},
// no metric, and {"duration":0} for a metric with no policy. It also reads
// a nested {"policy": {"duration": <ns>, "resolution": <ns>, "aggregation":
// <name>}} and the older top-level duration_seconds / resolution_seconds.
//
// go-qrdb's percentile, first and last aggregations have no SDK
// AggregateFunction: the typed GetRetention reports them as AggregateNone,
// while the untyped reader keeps their names.
func decodeRetentionResult(m map[string]interface{}) retentionResult {
	if p, ok := m["policy"].(map[string]interface{}); ok && p != nil {
		return retentionResult{
			duration:    time.Duration(tsGetInt64(p, "duration")),
			resolution:  time.Duration(tsGetInt64(p, "resolution")),
			aggregation: retentionAggregationName(p["aggregation"]),
		}
	}
	_, hasDurationSec := m["duration_seconds"]
	_, hasResolutionSec := m["resolution_seconds"]
	if hasDurationSec || hasResolutionSec {
		return retentionResult{
			duration:    time.Duration(tsGetInt64(m, "duration_seconds")) * time.Second,
			resolution:  time.Duration(tsGetInt64(m, "resolution_seconds")) * time.Second,
			aggregation: retentionAggregationName(m["aggregation"]),
		}
	}
	return retentionResult{
		duration:    time.Duration(tsGetInt64(m, "duration")),
		resolution:  time.Duration(tsGetInt64(m, "resolution")),
		aggregation: retentionAggregationName(m["aggregation"]),
	}
}

// Helper functions

func tsGetString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func tsGetBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func tsGetInt64(m map[string]interface{}, key string) int64 {
	if v, ok := m[key].(float64); ok {
		return int64(v)
	}
	if v, ok := m[key].(int64); ok {
		return v
	}
	return 0
}

func tsGetFloat64(m map[string]interface{}, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}

func tsGetTime(m map[string]interface{}, key string) time.Time {
	if v, ok := m[key].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t
		}
	}
	if v, ok := m[key].(float64); ok {
		// Unix nanoseconds
		return time.Unix(0, int64(v))
	}
	return time.Time{}
}

// formatFloatValue converts a float64 to string representation for MCP tool arguments.
func formatFloatValue(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// isMCPTimeseriesService returns true if the service is a TimeSeries service.
func isMCPTimeseriesService(service string) bool {
	serviceName := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		serviceName = service[idx+1:]
	}
	return serviceName == "TimeSeries" || serviceName == "Timeseries"
}

// mapTimeseriesMethodToTool maps TimeSeries SDK method names to MCP tool names.
// SDK methods use CamelCase while MCP tools use the pattern: quicbaas_ts_<operation>
func mapTimeseriesMethodToTool(method string) string {
	timeseriesToolMap := map[string]string{
		// SDK client methods
		"Write":        "qrdb_ts_write_point",
		"Query":        "qrdb_ts_query",
		"SetRetention": "qrdb_ts_set_retention_policy",
		"GetRetention": "qrdb_ts_get_retention_policy",
		// CLI interface methods
		"TSWritePoint":         "qrdb_ts_write_point",
		"TSWriteBatch":         "qrdb_ts_write_batch",
		"TSQuery":              "qrdb_ts_query",
		"TSDeleteSeries":       "qrdb_ts_delete_series",
		"TSGetRetentionPolicy": "qrdb_ts_get_retention_policy",
		"TSSetRetentionPolicy": "qrdb_ts_set_retention_policy",
		"TSListMetrics":        "qrdb_ts_list_metrics",
		// Alternative method names
		"WritePoint":         "qrdb_ts_write_point",
		"WriteBatch":         "qrdb_ts_write_batch",
		"DeleteSeries":       "qrdb_ts_delete_series",
		"GetRetentionPolicy": "qrdb_ts_get_retention_policy",
		"SetRetentionPolicy": "qrdb_ts_set_retention_policy",
		"ListMetrics":        "qrdb_ts_list_metrics",
	}

	if toolName, ok := timeseriesToolMap[method]; ok {
		return toolName
	}

	// Fallback for unknown methods
	return "quicbaas_ts_" + camelToSnake(method)
}
