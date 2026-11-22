// Copyright 2026 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package quicbaas

import (
	"context"
	"sync/atomic"

	qrdbtimeseries "github.com/jeremyhahn/go-qrdb/pkg/timeseries"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// QRDBTimeSeriesContextReader is the context-carrying read surface of a
// time-series servicer installed through WithEmbeddedQRDB. The broker's
// scoped time-series servicer implements it: it resolves the caller's
// database from ctx, which the contextless GetRetentionPolicy and
// ListMetrics of the qrdb servicer interface cannot do, so they fail closed.
// An embedded client whose time-series servicer implements this interface
// reads retention policies and lists metrics through it.
type QRDBTimeSeriesContextReader interface {
	// GetRetentionPolicyContext returns the metric's retention policy in
	// the scope resolved from ctx, or nil when none is set.
	GetRetentionPolicyContext(ctx context.Context, metric string) (*qrdbtimeseries.RetentionPolicy, error)
	// ListMetricsContext lists metric names with the given prefix in the
	// scope resolved from ctx, up to limit names.
	ListMetricsContext(ctx context.Context, prefix string, limit int) ([]string, error)
}

// embeddedQRDBTimeSeries is the TimeSeriesClient of an embedded client whose
// time-series servicer implements QRDBTimeSeriesContextReader. It routes the
// two reads the embedded qrdb transport would send to the contextless
// servicer methods through the scoped reader, and delegates every other verb
// to the embedded qrdb client. Once the parent client is closed, the two
// scoped reads return qrdbtransport.ClosedError, the error the closed
// embedded qrdb transport returns for every other verb.
type embeddedQRDBTimeSeries struct {
	QRDBTimeSeriesClient
	scoped QRDBTimeSeriesContextReader
	closed *atomic.Bool
}

// isClosed reports whether the parent client has been closed.
func (e *embeddedQRDBTimeSeries) isClosed() bool {
	return e.closed != nil && e.closed.Load()
}

// TSGetRetention reads the metric's retention policy in the caller's scope.
func (e *embeddedQRDBTimeSeries) TSGetRetention(ctx context.Context, req *qrdbtransport.TSGetRetentionRequest) (*qrdbtransport.TSGetRetentionResponse, error) {
	if e.isClosed() {
		return nil, &qrdbtransport.ClosedError{}
	}
	if req == nil {
		return nil, sdkerrors.NewInvalidInput("TSGetRetention", "request is required")
	}
	p, err := e.scoped.GetRetentionPolicyContext(ctx, req.Metric)
	if err != nil {
		return nil, err
	}
	resp := &qrdbtransport.TSGetRetentionResponse{}
	if p != nil {
		resp.Policy = &qrdbtransport.TSRetentionPolicy{
			DurationNs:   int64(p.Duration),
			ResolutionNs: int64(p.Resolution),
			Aggregation:  qrdbtransport.AggFunc(p.Aggregation),
		}
	}
	return resp, nil
}

// TSListMetrics lists metric names in the caller's scope. A request with a
// consistency level goes to the embedded qrdb client, whose path already
// carries ctx.
func (e *embeddedQRDBTimeSeries) TSListMetrics(ctx context.Context, req *qrdbtransport.TSListMetricsRequest) (*qrdbtransport.TSListMetricsResponse, error) {
	if e.isClosed() {
		return nil, &qrdbtransport.ClosedError{}
	}
	if req == nil {
		return nil, sdkerrors.NewInvalidInput("TSListMetrics", "request is required")
	}
	if req.Consistency != "" {
		return e.QRDBTimeSeriesClient.TSListMetrics(ctx, req)
	}
	metrics, err := e.scoped.ListMetricsContext(ctx, req.Prefix, int(req.Limit))
	if err != nil {
		return nil, err
	}
	return &qrdbtransport.TSListMetricsResponse{Metrics: metrics}, nil
}
