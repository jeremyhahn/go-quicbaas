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

package grpc

import (
	"context"
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// timeSeriesServiceName is the service name the sdk/go TimeSeries client sends.
const timeSeriesServiceName = "quicbaas.TimeSeries"

// isTimeSeriesService reports whether the service name is the TimeSeries service.
func isTimeSeriesService(service string) bool {
	return service == timeSeriesServiceName
}

type timeSeriesHandlerFunc func(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error)

// timeSeriesMethodMap dispatches TimeSeries methods onto the QuicBaaS TS* RPCs.
// Write carries one point, so it maps to TSWritePoint, as the REST transport
// posts it to the single-point write route.
var timeSeriesMethodMap = map[string]timeSeriesHandlerFunc{
	"Write": handleTimeSeriesWrite,
	"Query": handleTimeSeriesQuery,
}

// handleTimeSeriesRequest routes one TimeSeries method over conn.
func handleTimeSeriesRequest(ctx context.Context, conn *grpc.ClientConn, method string, req interface{}) (interface{}, error) {
	handler, ok := timeSeriesMethodMap[method]
	if !ok {
		return nil, errors.NewInvalidInput("grpc.timeseries."+method, "unknown timeseries method")
	}
	return handler(ctx, pb.NewQuicBaaSClient(conn), req)
}

func handleTimeSeriesWrite(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	const op = "grpc.timeseries.Write"
	r, ok := req.(*timeseries.WriteRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	_, err := client.TSWritePoint(ctx, &pb.TSWritePointRequest{
		Metric:    r.Metric,
		Tags:      r.Tags,
		Value:     r.Value,
		Timestamp: r.Timestamp.UnixNano(),
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return nil, nil
}

func handleTimeSeriesQuery(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	const op = "grpc.timeseries.Query"
	r, ok := req.(*timeseries.QueryRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	in := &pb.TSQueryRequest{
		Metric:    r.Metric,
		Tags:      r.Tags,
		StartTime: r.Start.UnixNano(),
		EndTime:   r.End.UnixNano(),
	}
	if r.Aggregate != timeseries.AggregateNone {
		in.Aggregation = r.Aggregate.String()
		in.IntervalSeconds = int64(r.Interval.Seconds())
	}
	resp, err := client.TSQuery(ctx, in)
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	out := &timeseries.QueryResponse{Series: make([]timeseries.Point, 0, len(resp.GetPoints()))}
	for _, p := range resp.GetPoints() {
		out.Series = append(out.Series, timeseries.Point{
			Timestamp: time.Unix(0, p.GetTimestamp()),
			Value:     p.GetValue(),
		})
	}
	return out, nil
}
