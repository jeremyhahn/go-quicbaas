// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package quicbaas

import (
	"context"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"google.golang.org/grpc/metadata"
)

// qrdbTimeSeriesBearer adds the configured WithAuthToken bearer to every
// go-qrdb time-series RPC over gRPC or Unix. go-qrdb's own gRPC and Unix
// transports install no bearer interceptor but forward outgoing-context
// metadata, so the decorator sends the same `authorization: Bearer <token>`
// metadata the quicbaas gRPC transport sends.
type qrdbTimeSeriesBearer struct {
	next  qrdbtransport.TimeSeriesClient
	token string
}

var _ qrdbtransport.TimeSeriesClient = (*qrdbTimeSeriesBearer)(nil)

func (b *qrdbTimeSeriesBearer) ctx(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+b.token)
}

func (b *qrdbTimeSeriesBearer) TSWrite(ctx context.Context, point *qrdbtransport.TSDataPoint) error {
	return b.next.TSWrite(b.ctx(ctx), point)
}

func (b *qrdbTimeSeriesBearer) TSWriteBatch(ctx context.Context, points []qrdbtransport.TSDataPoint) error {
	return b.next.TSWriteBatch(b.ctx(ctx), points)
}

func (b *qrdbTimeSeriesBearer) TSQuery(ctx context.Context, req *qrdbtransport.TSQueryRequest) (*qrdbtransport.TSQueryResponse, error) {
	return b.next.TSQuery(b.ctx(ctx), req)
}

func (b *qrdbTimeSeriesBearer) TSAggregate(ctx context.Context, req *qrdbtransport.TSAggregateRequest) (*qrdbtransport.TSAggregateResponse, error) {
	return b.next.TSAggregate(b.ctx(ctx), req)
}

func (b *qrdbTimeSeriesBearer) TSWritePoint(ctx context.Context, point *qrdbtransport.TSDataPoint) error {
	return b.next.TSWritePoint(b.ctx(ctx), point)
}

func (b *qrdbTimeSeriesBearer) TSRichQuery(ctx context.Context, req *qrdbtransport.TSRichQueryRequest) (*qrdbtransport.TSRichQueryResponse, error) {
	return b.next.TSRichQuery(b.ctx(ctx), req)
}

func (b *qrdbTimeSeriesBearer) TSDeleteSeries(ctx context.Context, req *qrdbtransport.TSDeleteRequest) (*qrdbtransport.TSDeleteResponse, error) {
	return b.next.TSDeleteSeries(b.ctx(ctx), req)
}

func (b *qrdbTimeSeriesBearer) TSGetRetention(ctx context.Context, req *qrdbtransport.TSGetRetentionRequest) (*qrdbtransport.TSGetRetentionResponse, error) {
	return b.next.TSGetRetention(b.ctx(ctx), req)
}

func (b *qrdbTimeSeriesBearer) TSSetRetention(ctx context.Context, req *qrdbtransport.TSSetRetentionRequest) error {
	return b.next.TSSetRetention(b.ctx(ctx), req)
}

func (b *qrdbTimeSeriesBearer) TSListMetrics(ctx context.Context, req *qrdbtransport.TSListMetricsRequest) (*qrdbtransport.TSListMetricsResponse, error) {
	return b.next.TSListMetrics(b.ctx(ctx), req)
}
