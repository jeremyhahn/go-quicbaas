// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// timeseries_handler.go provides typed REST routing for the quicbaas.TimeSeries
// service. The generic dispatch path returned an opaque map that the SDK
// TimeSeries client could not type-assert into its *QueryResponse. This handler
// posts the typed write/query requests to the concrete RESTful endpoints and
// decodes the REST wire response into the SDK QueryResponse type.

package rest

import (
	"context"
	"net/http"
	"net/url"
	"time"

	timeseriesclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// timeseriesServicePrefix is the gRPC-style service identifier the SDK uses to
// route TimeSeries requests to this handler.
const timeseriesServicePrefix = "quicbaas.TimeSeries"

// isTimeseriesService reports whether the supplied service name resolves to the
// TimeSeries service.
func isTimeseriesService(service string) bool {
	switch trimRESTServicePrefix(service) {
	case "TimeSeries", "Timeseries":
		return true
	default:
		return false
	}
}

// timeseriesRESTHandler delegates TimeSeries service calls to the quicbaasd REST
// endpoints.
type timeseriesRESTHandler struct {
	transport *Transport
}

// newTimeseriesRESTHandler constructs a handler bound to t.
func newTimeseriesRESTHandler(t *Transport) *timeseriesRESTHandler {
	return &timeseriesRESTHandler{transport: t}
}

// handleRequest dispatches the TimeSeries service method to the matching REST
// endpoint.
func (h *timeseriesRESTHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	switch method {
	case "Write", "WritePoint":
		return h.handleWrite(ctx, req)
	case "Query":
		return h.handleQuery(ctx, req)
	case "SetRetention":
		return h.handleSetRetention(ctx, req)
	case "GetRetention":
		return h.handleGetRetention(ctx, req)
	default:
		return h.transport.requestWithRetry(ctx, timeseriesServicePrefix, method, req)
	}
}

// =============================================================================
// REST wire types
// =============================================================================

type timeseriesWritePointRequest struct {
	Metric    string            `json:"metric"`
	Tags      map[string]string `json:"tags,omitempty"`
	Value     float64           `json:"value"`
	Timestamp time.Time         `json:"timestamp"`
}

type timeseriesQueryRequest struct {
	Metric      string            `json:"metric"`
	Tags        map[string]string `json:"tags,omitempty"`
	StartTime   time.Time         `json:"start_time"`
	EndTime     time.Time         `json:"end_time"`
	Aggregation string            `json:"aggregation,omitempty"`
	IntervalSec int64             `json:"interval_seconds,omitempty"`
}

type timeseriesDataPointWire struct {
	Metric    string            `json:"metric"`
	Tags      map[string]string `json:"tags,omitempty"`
	Value     float64           `json:"value"`
	Timestamp time.Time         `json:"timestamp"`
}

type timeseriesQueryResponseWire struct {
	Metric          string                    `json:"metric"`
	Points          []timeseriesDataPointWire `json:"points,omitempty"`
	AggregatedValue *float64                  `json:"aggregated_value,omitempty"`
	Timestamp       time.Time                 `json:"timestamp"`
}

// =============================================================================
// Handlers
// =============================================================================

func (h *timeseriesRESTHandler) handleWrite(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*timeseriesclient.WriteRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.timeseries.Write", "request is required")
	}
	if r.Metric == "" {
		return nil, errors.NewInvalidInput("rest.timeseries.Write", "metric is required")
	}
	if r.Timestamp.IsZero() {
		return nil, errors.NewInvalidInput("rest.timeseries.Write", "timestamp is required")
	}
	body := &timeseriesWritePointRequest{
		Metric:    r.Metric,
		Tags:      r.Tags,
		Value:     r.Value,
		Timestamp: r.Timestamp,
	}
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/timeseries/write", body, nil); err != nil {
		return nil, err
	}
	return nil, nil
}

func (h *timeseriesRESTHandler) handleQuery(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*timeseriesclient.QueryRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.timeseries.Query", "request is required")
	}
	if r.Metric == "" {
		return nil, errors.NewInvalidInput("rest.timeseries.Query", "metric is required")
	}
	body := &timeseriesQueryRequest{
		Metric:    r.Metric,
		Tags:      r.Tags,
		StartTime: r.Start,
		EndTime:   r.End,
	}
	if r.Aggregate != timeseriesclient.AggregateNone {
		body.Aggregation = r.Aggregate.String()
		body.IntervalSec = int64(r.Interval.Seconds())
	}
	var wire timeseriesQueryResponseWire
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/timeseries/query", body, &wire); err != nil {
		return nil, err
	}
	out := &timeseriesclient.QueryResponse{
		Series: make([]timeseriesclient.Point, 0, len(wire.Points)),
	}
	for _, p := range wire.Points {
		out.Series = append(out.Series, timeseriesclient.Point{
			Timestamp: p.Timestamp,
			Value:     p.Value,
		})
	}
	return out, nil
}

// timeseriesRetentionPolicyWire is the server's TSRetentionPolicy JSON
// (pkg/api/rest/timeseries_types.go): whole seconds, lower-case aggregation.
type timeseriesRetentionPolicyWire struct {
	DurationSec   int64  `json:"duration_seconds"`
	ResolutionSec int64  `json:"resolution_seconds,omitempty"`
	Aggregation   string `json:"aggregation,omitempty"`
}

// timeseriesSetRetentionWire is the body of PUT
// /api/v1/timeseries/retention/{metric}.
type timeseriesSetRetentionWire struct {
	Policy *timeseriesRetentionPolicyWire `json:"policy"`
}

// timeseriesGetRetentionWire is the body GET
// /api/v1/timeseries/retention/{metric} answers with.
type timeseriesGetRetentionWire struct {
	Metric string                         `json:"metric"`
	Policy *timeseriesRetentionPolicyWire `json:"policy,omitempty"`
}

func timeseriesRetentionPath(metric string) string {
	return "/api/v1/timeseries/retention/" + url.PathEscape(metric)
}

func (h *timeseriesRESTHandler) handleSetRetention(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*timeseriesclient.SetRetentionRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.timeseries.SetRetention", "request is required")
	}
	if r.Metric == "" {
		return nil, errors.NewInvalidInput("rest.timeseries.SetRetention", "metric is required")
	}
	body := &timeseriesSetRetentionWire{Policy: &timeseriesRetentionPolicyWire{
		DurationSec:   int64(r.Policy.Duration / time.Second),
		ResolutionSec: int64(r.Policy.Resolution / time.Second),
		Aggregation:   r.Policy.Aggregation.WireName(),
	}}
	if err := h.transport.doJSONRequest(ctx, http.MethodPut, timeseriesRetentionPath(r.Metric), body, nil); err != nil {
		return nil, err
	}
	return nil, nil
}

// handleGetRetention reads a metric's policy through GET
// /api/v1/timeseries/retention/{metric}; a metric without a policy has a nil
// Policy. A zero policy from the server is reported as nil by the client
// (timeseries.Client.GetRetention).
func (h *timeseriesRESTHandler) handleGetRetention(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*timeseriesclient.GetRetentionRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.timeseries.GetRetention", "request is required")
	}
	if r.Metric == "" {
		return nil, errors.NewInvalidInput("rest.timeseries.GetRetention", "metric is required")
	}
	var wire timeseriesGetRetentionWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, timeseriesRetentionPath(r.Metric), nil, &wire); err != nil {
		return nil, err
	}
	out := &timeseriesclient.GetRetentionResponse{Metric: r.Metric}
	if wire.Policy != nil {
		out.Policy = &timeseriesclient.RetentionPolicy{
			Duration:    time.Duration(wire.Policy.DurationSec) * time.Second,
			Resolution:  time.Duration(wire.Policy.ResolutionSec) * time.Second,
			Aggregation: timeseriesclient.ParseAggregateFunction(wire.Policy.Aggregation),
		}
	}
	return out, nil
}
