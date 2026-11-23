package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// qrdbTSComponent names this adapter in the errors it returns.
const qrdbTSComponent = "rest.QRDBTimeSeriesClient"

// QRDBTimeSeriesClient serves go-qrdb's time-series verbs over the SDK's own
// REST transport, so every time-series request carries the headers the
// transport was built with, the platform bearer set by WithAuthToken among
// them (go-quicbaas#2500). go-qrdb's REST sub-client has no way to add an
// Authorization header, so over REST it reached the server's default-deny
// gate anonymously and was refused with 401.
//
// Each method keeps the HTTP method, path, query, request body and response
// shape of the matching method in go-qrdb sdk/go transport/rest
// (transport_timeseries.go).
type QRDBTimeSeriesClient struct {
	t *Transport
}

var _ qrdbtransport.TimeSeriesClient = (*QRDBTimeSeriesClient)(nil)

// QRDBTimeSeries returns the qrdb time-series client bound to this transport.
func (t *Transport) QRDBTimeSeries() *QRDBTimeSeriesClient {
	return &QRDBTimeSeriesClient{t: t}
}

// qrdbAggToWire maps an aggregation function to go-qrdb's REST wire name.
// AggNone, and any value not listed, has no wire name ("").
var qrdbAggToWire = map[qrdbtransport.AggFunc]string{
	qrdbtransport.AggAvg:        "avg",
	qrdbtransport.AggSum:        "sum",
	qrdbtransport.AggMin:        "min",
	qrdbtransport.AggMax:        "max",
	qrdbtransport.AggCount:      "count",
	qrdbtransport.AggPercentile: "percentile",
	qrdbtransport.AggFirst:      "first",
	qrdbtransport.AggLast:       "last",
}

// qrdbAggFromWire is the inverse of qrdbAggToWire. An unknown or empty wire
// name decodes as AggNone.
var qrdbAggFromWire = map[string]qrdbtransport.AggFunc{
	"avg":        qrdbtransport.AggAvg,
	"sum":        qrdbtransport.AggSum,
	"min":        qrdbtransport.AggMin,
	"max":        qrdbtransport.AggMax,
	"count":      qrdbtransport.AggCount,
	"percentile": qrdbtransport.AggPercentile,
	"first":      qrdbtransport.AggFirst,
	"last":       qrdbtransport.AggLast,
}

// qrdbTSWirePoint is a data point on go-qrdb's time-series REST wire.
type qrdbTSWirePoint struct {
	Metric    string            `json:"metric"`
	Value     float64           `json:"value"`
	Timestamp int64             `json:"timestamp"`
	Tags      map[string]string `json:"tags,omitempty"`
}

type qrdbTSWritePointsWire struct {
	Points []qrdbTSWirePoint `json:"points"`
}

type qrdbTSAggResultWire struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
	Count     int64   `json:"count"`
}

type qrdbTSAggregateWire struct {
	Results []qrdbTSAggResultWire `json:"results"`
}

type qrdbTSRichQueryWire struct {
	Metric      string            `json:"metric"`
	Tags        map[string]string `json:"tags,omitempty"`
	StartNs     int64             `json:"start_ns"`
	EndNs       int64             `json:"end_ns"`
	Aggregation string            `json:"aggregation,omitempty"`
	IntervalNs  int64             `json:"interval_ns,omitempty"`
	GroupBy     []string          `json:"group_by,omitempty"`
	Limit       int               `json:"limit,omitempty"`
	Percentile  float64           `json:"percentile,omitempty"`
}

type qrdbTSGroupedResultWire struct {
	Tags            map[string]string `json:"tags"`
	Points          []qrdbTSWirePoint `json:"points"`
	AggregatedValue *float64          `json:"aggregated_value"`
}

type qrdbTSRichResultWire struct {
	Metric          string                    `json:"metric"`
	Tags            map[string]string         `json:"tags"`
	Points          []qrdbTSWirePoint         `json:"points"`
	AggregatedValue *float64                  `json:"aggregated_value"`
	GroupedResults  []qrdbTSGroupedResultWire `json:"grouped_results"`
}

type qrdbTSPolicyWire struct {
	DurationNs   int64  `json:"duration_ns"`
	ResolutionNs int64  `json:"resolution_ns,omitempty"`
	Aggregation  string `json:"aggregation,omitempty"`
}

type qrdbTSRetentionWire struct {
	Metric string            `json:"metric"`
	Policy *qrdbTSPolicyWire `json:"policy"`
}

type qrdbTSMetricsWire struct {
	Metrics []string `json:"metrics"`
}

// TSWrite stores a single time-series data point.
func (c *QRDBTimeSeriesClient) TSWrite(ctx context.Context, point *qrdbtransport.TSDataPoint) error {
	if point == nil {
		return qrdbTSNilRequest("TSWrite")
	}
	return c.t.doJSONRequest(ctx, http.MethodPost, "/ts/write", qrdbTSPointToWire(*point), nil)
}

// TSWriteBatch stores multiple time-series data points.
func (c *QRDBTimeSeriesClient) TSWriteBatch(ctx context.Context, points []qrdbtransport.TSDataPoint) error {
	wire := qrdbTSWritePointsWire{Points: make([]qrdbTSWirePoint, len(points))}
	for i := range points {
		wire.Points[i] = qrdbTSPointToWire(points[i])
	}
	return c.t.doJSONRequest(ctx, http.MethodPost, "/ts/write-batch", wire, nil)
}

// TSQuery retrieves data points for a metric within a time range.
func (c *QRDBTimeSeriesClient) TSQuery(ctx context.Context, req *qrdbtransport.TSQueryRequest) (*qrdbtransport.TSQueryResponse, error) {
	if req == nil {
		return nil, qrdbTSNilRequest("TSQuery")
	}
	q := qrdbTSRangeQuery(req.Metric, req.StartNs, req.EndNs)
	if len(req.Tags) > 0 {
		q.Set("tags", qrdbTSTagsQuery(req.Tags))
	}
	if req.Limit > 0 {
		q.Set("limit", strconv.FormatInt(int64(req.Limit), 10))
	}
	if req.Descending {
		q.Set("descending", "true")
	}
	if req.Consistency != "" {
		q.Set("consistency", req.Consistency)
	}
	var wire qrdbTSWritePointsWire
	if err := c.doJSONResult(ctx, http.MethodGet, "/ts/query?"+q.Encode(), nil, &wire); err != nil {
		return nil, err
	}
	return &qrdbtransport.TSQueryResponse{Points: qrdbTSPointsFromWire(wire.Points)}, nil
}

// TSAggregate computes an aggregation over data points in a time range.
func (c *QRDBTimeSeriesClient) TSAggregate(ctx context.Context, req *qrdbtransport.TSAggregateRequest) (*qrdbtransport.TSAggregateResponse, error) {
	if req == nil {
		return nil, qrdbTSNilRequest("TSAggregate")
	}
	q := qrdbTSRangeQuery(req.Metric, req.StartNs, req.EndNs)
	q.Set("function", qrdbAggToWire[req.Function])
	if req.IntervalNs > 0 {
		q.Set("interval_ns", strconv.FormatInt(req.IntervalNs, 10))
	}
	if req.Consistency != "" {
		q.Set("consistency", req.Consistency)
	}
	var wire qrdbTSAggregateWire
	if err := c.doJSONResult(ctx, http.MethodGet, "/ts/aggregate?"+q.Encode(), nil, &wire); err != nil {
		return nil, err
	}
	resp := &qrdbtransport.TSAggregateResponse{Results: make([]qrdbtransport.TSAggResult, len(wire.Results))}
	for i, r := range wire.Results {
		resp.Results[i] = qrdbtransport.TSAggResult{TimestampNs: r.Timestamp, Value: r.Value, Count: r.Count}
	}
	return resp, nil
}

// TSWritePoint stores a single validated data point.
func (c *QRDBTimeSeriesClient) TSWritePoint(ctx context.Context, point *qrdbtransport.TSDataPoint) error {
	if point == nil {
		return qrdbTSNilRequest("TSWritePoint")
	}
	return c.t.doJSONRequest(ctx, http.MethodPost, "/ts/write-point", qrdbTSPointToWire(*point), nil)
}

// TSRichQuery performs a rich query with aggregation, downsampling and
// grouping.
func (c *QRDBTimeSeriesClient) TSRichQuery(ctx context.Context, req *qrdbtransport.TSRichQueryRequest) (*qrdbtransport.TSRichQueryResponse, error) {
	if req == nil {
		return nil, qrdbTSNilRequest("TSRichQuery")
	}
	path := "/ts/query-rich"
	if req.Consistency != "" {
		path += "?" + url.Values{"consistency": {req.Consistency}}.Encode()
	}
	body := qrdbTSRichQueryWire{
		Metric:      req.Metric,
		Tags:        qrdbTSTagsToMap(req.Tags),
		StartNs:     req.StartNs,
		EndNs:       req.EndNs,
		Aggregation: qrdbAggToWire[req.Aggregation],
		IntervalNs:  req.IntervalNs,
		GroupBy:     req.GroupBy,
		Limit:       req.Limit,
		Percentile:  req.Percentile,
	}
	var wire qrdbTSRichResultWire
	if err := c.doJSONResult(ctx, http.MethodPost, path, body, &wire); err != nil {
		return nil, err
	}
	resp := &qrdbtransport.TSRichQueryResponse{
		Metric:          wire.Metric,
		Tags:            qrdbTSTagsFromMap(wire.Tags),
		Points:          qrdbTSPointsFromWire(wire.Points),
		AggregatedValue: wire.AggregatedValue,
	}
	if len(wire.GroupedResults) > 0 {
		resp.GroupedResults = make([]qrdbtransport.TSGroupedResult, len(wire.GroupedResults))
		for i, g := range wire.GroupedResults {
			resp.GroupedResults[i] = qrdbtransport.TSGroupedResult{
				Tags:            qrdbTSTagsFromMap(g.Tags),
				Points:          qrdbTSPointsFromWire(g.Points),
				AggregatedValue: g.AggregatedValue,
			}
		}
	}
	return resp, nil
}

// TSDeleteSeries deletes time-series data matching the criteria.
func (c *QRDBTimeSeriesClient) TSDeleteSeries(ctx context.Context, req *qrdbtransport.TSDeleteRequest) (*qrdbtransport.TSDeleteResponse, error) {
	if req == nil {
		return nil, qrdbTSNilRequest("TSDeleteSeries")
	}
	q := url.Values{}
	if len(req.Tags) > 0 {
		q.Set("tags", qrdbTSTagsQuery(req.Tags))
	}
	if req.StartNs > 0 {
		q.Set("start_ns", strconv.FormatInt(req.StartNs, 10))
	}
	if req.EndNs > 0 {
		q.Set("end_ns", strconv.FormatInt(req.EndNs, 10))
	}
	path := qrdbTSSeriesPath(req.Metric)
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	if err := c.t.doJSONRequest(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return nil, err
	}
	return &qrdbtransport.TSDeleteResponse{}, nil
}

// TSGetRetention retrieves the retention policy for a metric. A metric with
// no policy gives a response with a nil Policy.
func (c *QRDBTimeSeriesClient) TSGetRetention(ctx context.Context, req *qrdbtransport.TSGetRetentionRequest) (*qrdbtransport.TSGetRetentionResponse, error) {
	if req == nil {
		return nil, qrdbTSNilRequest("TSGetRetention")
	}
	var wire qrdbTSRetentionWire
	if err := c.doJSONResult(ctx, http.MethodGet, qrdbTSSeriesPath(req.Metric)+"/retention", nil, &wire); err != nil {
		return nil, err
	}
	resp := &qrdbtransport.TSGetRetentionResponse{}
	if wire.Policy != nil {
		resp.Policy = &qrdbtransport.TSRetentionPolicy{
			DurationNs:   wire.Policy.DurationNs,
			ResolutionNs: wire.Policy.ResolutionNs,
			Aggregation:  qrdbAggFromWire[wire.Policy.Aggregation],
		}
	}
	return resp, nil
}

// TSSetRetention sets the retention policy for a metric.
func (c *QRDBTimeSeriesClient) TSSetRetention(ctx context.Context, req *qrdbtransport.TSSetRetentionRequest) error {
	if req == nil || req.Policy == nil {
		return qrdbTSNilRequest("TSSetRetention")
	}
	body := qrdbTSPolicyWire{
		DurationNs:   req.Policy.DurationNs,
		ResolutionNs: req.Policy.ResolutionNs,
		Aggregation:  qrdbAggToWire[req.Policy.Aggregation],
	}
	return c.t.doJSONRequest(ctx, http.MethodPut, qrdbTSSeriesPath(req.Metric)+"/retention", body, nil)
}

// TSListMetrics lists metric names matching a prefix.
func (c *QRDBTimeSeriesClient) TSListMetrics(ctx context.Context, req *qrdbtransport.TSListMetricsRequest) (*qrdbtransport.TSListMetricsResponse, error) {
	if req == nil {
		return nil, qrdbTSNilRequest("TSListMetrics")
	}
	q := url.Values{}
	if req.Prefix != "" {
		q.Set("prefix", req.Prefix)
	}
	if req.Limit > 0 {
		q.Set("limit", strconv.FormatInt(int64(req.Limit), 10))
	}
	if req.Consistency != "" {
		q.Set("consistency", req.Consistency)
	}
	path := "/ts/metrics"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var wire qrdbTSMetricsWire
	if err := c.doJSONResult(ctx, http.MethodGet, path, nil, &wire); err != nil {
		return nil, err
	}
	return &qrdbtransport.TSListMetricsResponse{Metrics: wire.Metrics}, nil
}

// doJSONResult sends a request through the transport, so it carries the
// transport's headers, and decodes the response into result. A verb that
// returns a body refuses an empty one.
func (c *QRDBTimeSeriesClient) doJSONResult(ctx context.Context, method, path string, reqBody, result any) error {
	var body []byte
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return errors.Wrap(err, errors.CodeInvalidInput, qrdbTSComponent,
				"failed to encode request body")
		}
		body = data
	}

	resp, err := c.t.doRawRequest(ctx, method, path, body, nil)
	if err != nil {
		return err
	}
	if len(resp.Body) == 0 {
		return errors.New(errors.CodeInternal, qrdbTSComponent,
			"empty response from "+path)
	}
	if err := json.Unmarshal(resp.Body, result); err != nil {
		return errors.Wrap(err, errors.CodeInternal, qrdbTSComponent,
			"failed to decode response")
	}
	return nil
}

// qrdbTSNilRequest is the typed refusal of a nil request.
func qrdbTSNilRequest(op string) error {
	return errors.New(errors.CodeInvalidInput, qrdbTSComponent, op+": request is required")
}

// qrdbTSSeriesPath is the path of one metric's series, the metric escaped.
func qrdbTSSeriesPath(metric string) string {
	return "/ts/series/" + url.PathEscape(metric)
}

// qrdbTSRangeQuery starts a query with the metric and its time range.
func qrdbTSRangeQuery(metric string, startNs, endNs int64) url.Values {
	q := url.Values{}
	q.Set("metric", metric)
	q.Set("start_ns", strconv.FormatInt(startNs, 10))
	q.Set("end_ns", strconv.FormatInt(endNs, 10))
	return q
}

// qrdbTSTagsQuery renders tags as the k=v pairs, joined by ",", of the
// tags query parameter.
func qrdbTSTagsQuery(tags []qrdbtransport.TSTag) string {
	pairs := make([]string, len(tags))
	for i, tag := range tags {
		pairs[i] = tag.Key + "=" + tag.Value
	}
	return strings.Join(pairs, ",")
}

// qrdbTSTagsToMap converts transport tags to the wire's tag map; no tags
// give a nil map, which the wire omits.
func qrdbTSTagsToMap(tags []qrdbtransport.TSTag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	m := make(map[string]string, len(tags))
	for _, tag := range tags {
		m[tag.Key] = tag.Value
	}
	return m
}

// qrdbTSTagsFromMap converts the wire's tag map to transport tags, ordered by
// key so the result is deterministic.
func qrdbTSTagsFromMap(m map[string]string) []qrdbtransport.TSTag {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	tags := make([]qrdbtransport.TSTag, len(keys))
	for i, k := range keys {
		tags[i] = qrdbtransport.TSTag{Key: k, Value: m[k]}
	}
	return tags
}

func qrdbTSPointToWire(p qrdbtransport.TSDataPoint) qrdbTSWirePoint {
	return qrdbTSWirePoint{
		Metric:    p.Metric,
		Value:     p.Value,
		Timestamp: p.TimestampNs,
		Tags:      qrdbTSTagsToMap(p.Tags),
	}
}

func qrdbTSPointsFromWire(points []qrdbTSWirePoint) []qrdbtransport.TSDataPoint {
	if points == nil {
		return nil
	}
	out := make([]qrdbtransport.TSDataPoint, len(points))
	for i, p := range points {
		out[i] = qrdbtransport.TSDataPoint{
			Metric:      p.Metric,
			Value:       p.Value,
			TimestampNs: p.Timestamp,
			Tags:        qrdbTSTagsFromMap(p.Tags),
		}
	}
	return out
}
