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
