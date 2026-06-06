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

package quic

import (
	"encoding/json"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// timeseriesServicePrefix is the service name prefix for TimeSeries operations.
const timeseriesServicePrefix = "quicbaas.TimeSeries"

// isTimeseriesService reports whether svc names the QuicBaaS TimeSeries service.
func isTimeseriesService(svc string) bool {
	return strings.HasPrefix(svc, timeseriesServicePrefix) || svc == "TimeSeries"
}

// =============================================================================
// Request transformation (SDK timeseries client types -> server service.* types)
// =============================================================================
//
// The high-level TimeSeries SDK client (sdk/go/client/timeseries) sends method
// "Write" with a *timeseries.WriteRequest and method "Query" with a
// *timeseries.QueryRequest. The QUIC server's WritePoint / TSQuery handlers
// decode service.WritePointRequest / service.TSQueryRequest, whose JSON field
// names (snake_case) and value encodings differ from the client structs. This
// transform re-encodes every request into the server's wire shape so the QUIC
// path returns exactly what the embedded/REST/MCP paths return.

// transformTimeseriesRequest converts an SDK timeseries request for the given
// method into the canonical service.*Request value the server expects.
func transformTimeseriesRequest(method string, req interface{}) (interface{}, error) {
	switch method {
	case "Write", "WritePoint":
		return transformTSWriteRequest(req)
	case "Query", "TSQuery":
		return transformTSQueryRequest(req)
	default:
		return nil, errors.NewInvalidInput("quic.transformTimeseriesRequest",
			"unsupported timeseries method: "+method)
	}
}

// transformTSWriteRequest maps a *timeseries.WriteRequest to the server's
// service.WritePointRequest.
func transformTSWriteRequest(req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*timeseries.WriteRequest)
	if !ok {
		return nil, errors.NewInvalidInput("quic.transformTSWriteRequest",
			"expected *timeseries.WriteRequest")
	}
	return &service.WritePointRequest{
		Metric:    sdkReq.Metric,
		Tags:      sdkReq.Tags,
		Value:     sdkReq.Value,
		Timestamp: sdkReq.Timestamp,
	}, nil
}

// transformTSQueryRequest maps a *timeseries.QueryRequest to the server's
// service.TSQueryRequest. The SDK encodes Aggregate as an int enum and Interval
// as a time.Duration; the server expects an uppercase aggregation string and an
// interval expressed in whole seconds.
func transformTSQueryRequest(req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*timeseries.QueryRequest)
	if !ok {
		return nil, errors.NewInvalidInput("quic.transformTSQueryRequest",
			"expected *timeseries.QueryRequest")
	}

	out := &service.TSQueryRequest{
		Metric:    sdkReq.Metric,
		Tags:      sdkReq.Tags,
		StartTime: sdkReq.Start,
		EndTime:   sdkReq.End,
	}

	// AggregateNone means "raw points, no aggregation": leave Aggregation empty
	// so the server returns individual data points. Any other function maps to
	// its uppercase string form (SUM, AVG, ...).
	if sdkReq.Aggregate != timeseries.AggregateNone {
		out.Aggregation = sdkReq.Aggregate.String()
	}
	if sdkReq.Interval > 0 {
		out.IntervalSec = int64(sdkReq.Interval.Seconds())
	}
	return out, nil
}

// =============================================================================
// Response decoding (server service.*Response -> SDK timeseries client types)
// =============================================================================

// decodeTimeseriesResponse decodes raw wire bytes into the SDK timeseries type
// for the given method. Write returns no typed body (the client ignores the
// response and treats success as nil error); Query returns a
// *timeseries.QueryResponse built from the server's TSQueryResponse points.
func decodeTimeseriesResponse(method string, data []byte) (interface{}, error) {
	switch method {
	case "Write", "WritePoint":
		// The client's Write ignores the response body; validate it decodes so a
		// malformed server payload still surfaces, then signal success with nil.
		if len(data) == 0 {
			return nil, nil
		}
		var resp service.WritePointResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, wrapTimeseriesDecode("Write", err)
		}
		return nil, nil
	case "Query", "TSQuery":
		return decodeTSQueryResponse(data)
	default:
		return nil, errors.NewInvalidInput("quic.decodeTimeseriesResponse",
			"unsupported timeseries method: "+method)
	}
}

// decodeTSQueryResponse decodes a server service.TSQueryResponse into the SDK
// *timeseries.QueryResponse the client's Query method asserts on.
func decodeTSQueryResponse(data []byte) (interface{}, error) {
	if len(data) == 0 {
		return &timeseries.QueryResponse{Series: []timeseries.Point{}}, nil
	}

	var resp service.TSQueryResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapTimeseriesDecode("Query", err)
	}

	out := &timeseries.QueryResponse{
		Series: make([]timeseries.Point, 0, len(resp.Points)),
	}
	for _, p := range resp.Points {
		if p == nil {
			continue
		}
		out.Series = append(out.Series, timeseries.Point{
			Timestamp: p.Timestamp,
			Value:     p.Value,
		})
	}
	return out, nil
}

func wrapTimeseriesDecode(method string, err error) error {
	return errors.Wrap(err, errors.CodeInternal,
		"quic.decodeTimeseriesResponse", "failed to unmarshal "+method+" response")
}
