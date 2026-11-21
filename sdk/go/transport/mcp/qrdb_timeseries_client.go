// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"encoding/json"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// QRDBTimeSeriesClient is go-qrdb's TimeSeriesClient served over this
// quicbaas MCP transport. It calls go-qrdb's native qrdb_ts_* tools on the
// unified server through the transport's own MCP session, so it reuses the
// transport's MCP token or login credentials and opens no second session.
// Tool names, argument names and response decoding follow go-qrdb's own
// MCP transport (sdk/go/transport/mcp/transport_timeseries.go).
type QRDBTimeSeriesClient struct {
	t *Transport
}

var _ qrdbtransport.TimeSeriesClient = (*QRDBTimeSeriesClient)(nil)

// QRDBTimeSeries returns go-qrdb's TimeSeriesClient over this transport.
func (t *Transport) QRDBTimeSeries() *QRDBTimeSeriesClient {
	return &QRDBTimeSeriesClient{t: t}
}

// callQRDBTool calls a qrdb_ts_* tool the way callBarrier does: it refuses
// a closed transport, initializes the session and authenticates. It returns
// the tool's first text content undecoded so the go-qrdb decoders can
// unmarshal it into their own shapes.
func (t *Transport) callQRDBTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	if t.closed.Load() {
		return "", errors.NewConnection("mcp."+name, "transport is closed")
	}
	if err := t.ensureInitialized(ctx); err != nil {
		return "", err
	}
	if err := t.ensureAuthenticated(ctx); err != nil {
		return "", err
	}
	return t.callToolText(ctx, name, args)
}

// callToolText is callTool's sibling that returns the raw text content
// instead of a JSON-decoded value. Error handling is the same.
func (t *Transport) callToolText(ctx context.Context, name string, arguments map[string]interface{}) (string, error) {
	result, err := t.mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      name,
			Arguments: arguments,
		},
	})
	if err != nil {
		return "", t.convertHTTPError(err, "mcp.callTool")
	}
	if result.IsError {
		return "", t.extractToolError(result)
	}
	t.lastHealth.Store(time.Now().UnixNano())
	for _, item := range result.Content {
		if text, ok := mcp.AsTextContent(item); ok && text.Text != "" {
			return text.Text, nil
		}
	}
	return "", nil
}

// qtsServerDataPoint mirrors go-qrdb timeseries.DataPoint (RFC 3339
// timestamp, map tags).
type qtsServerDataPoint struct {
	Metric    string            `json:"metric"`
	Value     float64           `json:"value"`
	Timestamp time.Time         `json:"timestamp"`
	Tags      map[string]string `json:"tags,omitempty"`
}

// qtsServerAggResult mirrors go-qrdb timeseries.AggResult, which has no
// JSON tags (PascalCase keys).
type qtsServerAggResult struct {
	Timestamp time.Time `json:"Timestamp"`
	Value     float64   `json:"Value"`
	Count     int64     `json:"Count"`
}

// qtsServerRetentionPolicy mirrors go-qrdb timeseries.RetentionPolicy
// (durations in nanoseconds).
type qtsServerRetentionPolicy struct {
	Duration    int64 `json:"duration"`
	Resolution  int64 `json:"resolution,omitempty"`
	Aggregation int   `json:"aggregation,omitempty"`
}

// qtsServerQueryResult mirrors go-qrdb timeseries.QueryResult.
type qtsServerQueryResult struct {
	Metric          string                    `json:"metric"`
	Tags            map[string]string         `json:"tags,omitempty"`
	Points          []*qtsServerDataPoint     `json:"points"`
	AggregatedValue *float64                  `json:"aggregated_value,omitempty"`
	GroupedResults  []*qtsServerGroupedResult `json:"grouped_results,omitempty"`
}

// qtsServerGroupedResult mirrors go-qrdb timeseries.GroupedResult.
type qtsServerGroupedResult struct {
	Tags            map[string]string     `json:"tags"`
	Points          []*qtsServerDataPoint `json:"points,omitempty"`
	AggregatedValue *float64              `json:"aggregated_value,omitempty"`
}

// qtsAggFuncNames maps go-qrdb AggFunc values to the tool argument names.
var qtsAggFuncNames = map[qrdbtransport.AggFunc]string{
	qrdbtransport.AggAvg:        "avg",
	qrdbtransport.AggSum:        "sum",
	qrdbtransport.AggMin:        "min",
	qrdbtransport.AggMax:        "max",
	qrdbtransport.AggCount:      "count",
	qrdbtransport.AggPercentile: "percentile",
	qrdbtransport.AggFirst:      "first",
	qrdbtransport.AggLast:       "last",
}

func qtsPointToTransport(p *qtsServerDataPoint) qrdbtransport.TSDataPoint {
	return qrdbtransport.TSDataPoint{
		Metric:      p.Metric,
		Value:       p.Value,
		TimestampNs: p.Timestamp.UnixNano(),
		Tags:        qtsTagsMapToSlice(p.Tags),
	}
}

func qtsTagsSliceToMap(tags []qrdbtransport.TSTag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	m := make(map[string]string, len(tags))
	for _, tag := range tags {
		m[tag.Key] = tag.Value
	}
	return m
}

func qtsTagsToJSON(tags []qrdbtransport.TSTag) (string, error) {
	m := qtsTagsSliceToMap(tags)
	if len(m) == 0 {
		return "", nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func qtsTagsMapToSlice(m map[string]string) []qrdbtransport.TSTag {
	if len(m) == 0 {
		return nil
	}
	tags := make([]qrdbtransport.TSTag, 0, len(m))
	for k, v := range m {
		tags = append(tags, qrdbtransport.TSTag{Key: k, Value: v})
	}
	return tags
}

func qtsEncodeErr(method string, err error) error {
	return errors.NewInvalidInput("mcp."+method, "failed to encode request: "+err.Error())
}

func qtsDecodeErr(method string, err error) error {
	return errors.NewInternal("mcp."+method, "failed to decode response: "+err.Error())
}

func qtsUnsupportedAgg(method string) error {
	return errors.NewInvalidInput("mcp."+method, "unsupported aggregation function")
}

// writeArgs builds the qrdb_ts_write / qrdb_ts_write_point arguments.
func qtsWriteArgs(method string, point *qrdbtransport.TSDataPoint) (map[string]interface{}, error) {
	args := map[string]interface{}{
		"metric":    point.Metric,
		"value":     point.Value,
		"timestamp": point.TimestampNs,
	}
	tagsJSON, err := qtsTagsToJSON(point.Tags)
	if err != nil {
		return nil, qtsEncodeErr(method, err)
	}
	if tagsJSON != "" {
		args["tags"] = tagsJSON
	}
	return args, nil
}

// TSWrite calls qrdb_ts_write.
func (c *QRDBTimeSeriesClient) TSWrite(ctx context.Context, point *qrdbtransport.TSDataPoint) error {
	args, err := qtsWriteArgs("TSWrite", point)
	if err != nil {
		return err
	}
	_, err = c.t.callQRDBTool(ctx, "qrdb_ts_write", args)
	return err
}

// TSWriteBatch calls qrdb_ts_write_batch with the points as a JSON string.
func (c *QRDBTimeSeriesClient) TSWriteBatch(ctx context.Context, points []qrdbtransport.TSDataPoint) error {
	type pointJSON struct {
		Metric    string            `json:"metric"`
		Value     float64           `json:"value"`
		Timestamp int64             `json:"timestamp"`
		Tags      map[string]string `json:"tags,omitempty"`
	}
	batch := make([]pointJSON, len(points))
	for i, p := range points {
		batch[i] = pointJSON{
			Metric:    p.Metric,
			Value:     p.Value,
			Timestamp: p.TimestampNs,
			Tags:      qtsTagsSliceToMap(p.Tags),
		}
	}
	data, err := json.Marshal(batch)
	if err != nil {
		return qtsEncodeErr("TSWriteBatch", err)
	}
	_, err = c.t.callQRDBTool(ctx, "qrdb_ts_write_batch", map[string]interface{}{
		"points": string(data),
	})
	return err
}

// TSQuery calls qrdb_ts_query and decodes its point array.
func (c *QRDBTimeSeriesClient) TSQuery(ctx context.Context, req *qrdbtransport.TSQueryRequest) (*qrdbtransport.TSQueryResponse, error) {
	args := map[string]interface{}{
		"metric": req.Metric,
		"start":  req.StartNs,
		"end":    req.EndNs,
	}
	tagsJSON, err := qtsTagsToJSON(req.Tags)
	if err != nil {
		return nil, qtsEncodeErr("TSQuery", err)
	}
	if tagsJSON != "" {
		args["tags"] = tagsJSON
	}
	if req.Limit > 0 {
		args["limit"] = int64(req.Limit)
	}
	if req.Descending {
		args["descending"] = true
	}
	if req.Consistency != "" {
		args["consistency"] = req.Consistency
	}

	text, err := c.t.callQRDBTool(ctx, "qrdb_ts_query", args)
	if err != nil {
		return nil, err
	}
	var serverPoints []qtsServerDataPoint
	if err := json.Unmarshal([]byte(text), &serverPoints); err != nil {
		return nil, qtsDecodeErr("TSQuery", err)
	}
	points := make([]qrdbtransport.TSDataPoint, len(serverPoints))
	for i := range serverPoints {
		points[i] = qtsPointToTransport(&serverPoints[i])
	}
	return &qrdbtransport.TSQueryResponse{Points: points}, nil
}

// TSAggregate calls qrdb_ts_aggregate and decodes its bucket array.
func (c *QRDBTimeSeriesClient) TSAggregate(ctx context.Context, req *qrdbtransport.TSAggregateRequest) (*qrdbtransport.TSAggregateResponse, error) {
	funcName, ok := qtsAggFuncNames[req.Function]
	if !ok {
		return nil, qtsUnsupportedAgg("TSAggregate")
	}
	args := map[string]interface{}{
		"metric":      req.Metric,
		"start":       req.StartNs,
		"end":         req.EndNs,
		"func":        funcName,
		"interval_ns": req.IntervalNs,
	}
	if req.Consistency != "" {
		args["consistency"] = req.Consistency
	}

	text, err := c.t.callQRDBTool(ctx, "qrdb_ts_aggregate", args)
	if err != nil {
		return nil, err
	}
	var serverResults []qtsServerAggResult
	if err := json.Unmarshal([]byte(text), &serverResults); err != nil {
		return nil, qtsDecodeErr("TSAggregate", err)
	}
	results := make([]qrdbtransport.TSAggResult, len(serverResults))
	for i, r := range serverResults {
		results[i] = qrdbtransport.TSAggResult{
			TimestampNs: r.Timestamp.UnixNano(),
			Value:       r.Value,
			Count:       r.Count,
		}
	}
	return &qrdbtransport.TSAggregateResponse{Results: results}, nil
}

// TSWritePoint calls qrdb_ts_write_point.
func (c *QRDBTimeSeriesClient) TSWritePoint(ctx context.Context, point *qrdbtransport.TSDataPoint) error {
	args, err := qtsWriteArgs("TSWritePoint", point)
	if err != nil {
		return err
	}
	_, err = c.t.callQRDBTool(ctx, "qrdb_ts_write_point", args)
	return err
}

// TSRichQuery calls qrdb_ts_rich_query and decodes points, the aggregated
// value and grouped results.
func (c *QRDBTimeSeriesClient) TSRichQuery(ctx context.Context, req *qrdbtransport.TSRichQueryRequest) (*qrdbtransport.TSRichQueryResponse, error) {
	args := map[string]interface{}{
		"metric": req.Metric,
		"start":  req.StartNs,
		"end":    req.EndNs,
	}
	tagsJSON, err := qtsTagsToJSON(req.Tags)
	if err != nil {
		return nil, qtsEncodeErr("TSRichQuery", err)
	}
	if tagsJSON != "" {
		args["tags"] = tagsJSON
	}
	if req.Aggregation != qrdbtransport.AggNone {
		funcName, ok := qtsAggFuncNames[req.Aggregation]
		if !ok {
			return nil, qtsUnsupportedAgg("TSRichQuery")
		}
		args["aggregation"] = funcName
	}
	if req.IntervalNs > 0 {
		args["interval_ns"] = req.IntervalNs
	}
	if len(req.GroupBy) > 0 {
		groupByJSON, err := json.Marshal(req.GroupBy)
		if err != nil {
			return nil, qtsEncodeErr("TSRichQuery", err)
		}
		args["group_by"] = string(groupByJSON)
	}
	if req.Limit > 0 {
		args["limit"] = int64(req.Limit)
	}
	if req.Percentile > 0 {
		args["percentile"] = req.Percentile
	}
	if req.Consistency != "" {
		args["consistency"] = req.Consistency
	}

	text, err := c.t.callQRDBTool(ctx, "qrdb_ts_rich_query", args)
	if err != nil {
		return nil, err
	}
	var result qtsServerQueryResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, qtsDecodeErr("TSRichQuery", err)
	}
	points := make([]qrdbtransport.TSDataPoint, len(result.Points))
	for i, p := range result.Points {
		points[i] = qtsPointToTransport(p)
	}
	grouped := make([]qrdbtransport.TSGroupedResult, len(result.GroupedResults))
	for i, g := range result.GroupedResults {
		gPoints := make([]qrdbtransport.TSDataPoint, len(g.Points))
		for j, p := range g.Points {
			gPoints[j] = qtsPointToTransport(p)
		}
		grouped[i] = qrdbtransport.TSGroupedResult{
			Tags:            qtsTagsMapToSlice(g.Tags),
			Points:          gPoints,
			AggregatedValue: g.AggregatedValue,
		}
	}
	return &qrdbtransport.TSRichQueryResponse{
		Metric:          result.Metric,
		Tags:            qtsTagsMapToSlice(result.Tags),
		Points:          points,
		AggregatedValue: result.AggregatedValue,
		GroupedResults:  grouped,
	}, nil
}

// TSDeleteSeries calls qrdb_ts_delete_series.
func (c *QRDBTimeSeriesClient) TSDeleteSeries(ctx context.Context, req *qrdbtransport.TSDeleteRequest) (*qrdbtransport.TSDeleteResponse, error) {
	args := map[string]interface{}{
		"metric": req.Metric,
	}
	tagsJSON, err := qtsTagsToJSON(req.Tags)
	if err != nil {
		return nil, qtsEncodeErr("TSDeleteSeries", err)
	}
	if tagsJSON != "" {
		args["tags"] = tagsJSON
	}
	if req.StartNs != 0 {
		args["start"] = req.StartNs
	}
	if req.EndNs != 0 {
		args["end"] = req.EndNs
	}
	if _, err := c.t.callQRDBTool(ctx, "qrdb_ts_delete_series", args); err != nil {
		return nil, err
	}
	return &qrdbtransport.TSDeleteResponse{}, nil
}

// TSGetRetention calls qrdb_ts_get_retention_policy.
func (c *QRDBTimeSeriesClient) TSGetRetention(ctx context.Context, req *qrdbtransport.TSGetRetentionRequest) (*qrdbtransport.TSGetRetentionResponse, error) {
	text, err := c.t.callQRDBTool(ctx, "qrdb_ts_get_retention_policy", map[string]interface{}{
		"metric": req.Metric,
	})
	if err != nil {
		return nil, err
	}
	var raw qtsServerRetentionPolicy
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, qtsDecodeErr("TSGetRetention", err)
	}
	return &qrdbtransport.TSGetRetentionResponse{
		Policy: &qrdbtransport.TSRetentionPolicy{
			DurationNs:   raw.Duration,
			ResolutionNs: raw.Resolution,
			Aggregation:  qrdbtransport.AggFunc(raw.Aggregation),
		},
	}, nil
}

// TSSetRetention calls qrdb_ts_set_retention_policy (durations in seconds).
func (c *QRDBTimeSeriesClient) TSSetRetention(ctx context.Context, req *qrdbtransport.TSSetRetentionRequest) error {
	if req.Policy == nil {
		return errors.NewInvalidInput("mcp.TSSetRetention", "policy is required")
	}
	args := map[string]interface{}{
		"metric":           req.Metric,
		"duration_seconds": req.Policy.DurationNs / int64(time.Second),
	}
	if req.Policy.ResolutionNs > 0 {
		args["resolution_seconds"] = req.Policy.ResolutionNs / int64(time.Second)
	}
	if req.Policy.Aggregation != qrdbtransport.AggNone {
		funcName, ok := qtsAggFuncNames[req.Policy.Aggregation]
		if !ok {
			return qtsUnsupportedAgg("TSSetRetention")
		}
		args["aggregation"] = funcName
	}
	_, err := c.t.callQRDBTool(ctx, "qrdb_ts_set_retention_policy", args)
	return err
}

// TSListMetrics calls qrdb_ts_list_metrics and decodes its name array.
func (c *QRDBTimeSeriesClient) TSListMetrics(ctx context.Context, req *qrdbtransport.TSListMetricsRequest) (*qrdbtransport.TSListMetricsResponse, error) {
	args := map[string]interface{}{}
	if req.Prefix != "" {
		args["prefix"] = req.Prefix
	}
	if req.Limit > 0 {
		args["limit"] = int64(req.Limit)
	}
	if req.Consistency != "" {
		args["consistency"] = req.Consistency
	}
	text, err := c.t.callQRDBTool(ctx, "qrdb_ts_list_metrics", args)
	if err != nil {
		return nil, err
	}
	var metrics []string
	if err := json.Unmarshal([]byte(text), &metrics); err != nil {
		return nil, qtsDecodeErr("TSListMetrics", err)
	}
	return &qrdbtransport.TSListMetricsResponse{Metrics: metrics}, nil
}
