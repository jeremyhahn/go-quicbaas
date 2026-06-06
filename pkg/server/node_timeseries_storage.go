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

//go:build timeseries

package server

import (
	"context"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// nodeTimeSeriesStorage implements interfaces.TimeSeriesStorage by delegating
// all operations through the qrdb SDK node client's embedded transport.
// When the qrdb node is running in-process every call routes directly to the
// node's time series engine without network round-trips.
// The node owns the underlying storage lifecycle; Close is a no-op here.
type nodeTimeSeriesStorage struct {
	client *qrdbsdk.Client
}

// newNodeTimeSeriesStorage creates a nodeTimeSeriesStorage backed by the
// given qrdb SDK client.
func newNodeTimeSeriesStorage(client *qrdbsdk.Client) interfaces.TimeSeriesStorage {
	return &nodeTimeSeriesStorage{client: client}
}

// WritePoint writes a single data point via the node transport.
func (n *nodeTimeSeriesStorage) WritePoint(ctx context.Context, point *interfaces.DataPoint) error {
	return n.client.TimeSeries().TSWritePoint(ctx, toTSDataPoint(point))
}

// WriteBatch writes multiple data points atomically via the node transport.
func (n *nodeTimeSeriesStorage) WriteBatch(points []interfaces.DataPoint) error {
	pts := make([]qrdbtransport.TSDataPoint, len(points))
	for i := range points {
		pts[i] = *toTSDataPoint(&points[i])
	}
	return n.client.TimeSeries().TSWriteBatch(context.Background(), pts)
}

// QueryRich performs a rich time-range query via the node transport.
func (n *nodeTimeSeriesStorage) QueryRich(ctx context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
	tags := ifaceTagsToTS(req.Tags)
	groupBy := make([]string, len(req.GroupBy))
	copy(groupBy, req.GroupBy)

	resp, err := n.client.TimeSeries().TSRichQuery(ctx, &qrdbtransport.TSRichQueryRequest{
		Metric:      req.Metric,
		StartNs:     req.StartTime.UnixNano(),
		EndNs:       req.EndTime.UnixNano(),
		Tags:        tags,
		Aggregation: qrdbtransport.AggFunc(req.Aggregation),
		IntervalNs:  req.Interval.Nanoseconds(),
		GroupBy:     groupBy,
		Limit:       req.Limit,
		Percentile:  req.Percentile,
	})
	if err != nil {
		return nil, err
	}

	result := &interfaces.QueryResult{
		Metric:          resp.Metric,
		Tags:            tsTagsToIface(resp.Tags),
		AggregatedValue: resp.AggregatedValue,
	}
	for _, dp := range resp.Points {
		result.Points = append(result.Points, fromTSDataPoint(&dp))
	}
	for _, gr := range resp.GroupedResults {
		ifGR := &interfaces.GroupedResult{
			Tags:            tsTagsToIface(gr.Tags),
			AggregatedValue: gr.AggregatedValue,
		}
		for _, dp := range gr.Points {
			ifGR.Points = append(ifGR.Points, fromTSDataPoint(&dp))
		}
		result.GroupedResults = append(result.GroupedResults, ifGR)
	}
	return result, nil
}

// DeleteSeries deletes time-series data matching the request via the node transport.
func (n *nodeTimeSeriesStorage) DeleteSeries(ctx context.Context, req *interfaces.DeleteRequest) error {
	_, err := n.client.TimeSeries().TSDeleteSeries(ctx, &qrdbtransport.TSDeleteRequest{
		Metric:  req.Metric,
		Tags:    ifaceTagsToTS(req.Tags),
		StartNs: req.StartTime.UnixNano(),
		EndNs:   req.EndTime.UnixNano(),
	})
	return err
}

// GetRetentionPolicy retrieves the retention policy for a metric via the node transport.
func (n *nodeTimeSeriesStorage) GetRetentionPolicy(metric string) (*interfaces.RetentionPolicy, error) {
	resp, err := n.client.TimeSeries().TSGetRetention(context.Background(), &qrdbtransport.TSGetRetentionRequest{
		Metric: metric,
	})
	if err != nil {
		return nil, err
	}
	if resp.Policy == nil {
		return nil, nil
	}
	return &interfaces.RetentionPolicy{
		Duration:    time.Duration(resp.Policy.DurationNs),
		Resolution:  time.Duration(resp.Policy.ResolutionNs),
		Aggregation: interfaces.AggFunc(resp.Policy.Aggregation),
	}, nil
}

// SetRetentionPolicy sets the retention policy for a metric via the node transport.
func (n *nodeTimeSeriesStorage) SetRetentionPolicy(metric string, policy *interfaces.RetentionPolicy) error {
	var tsPolicy *qrdbtransport.TSRetentionPolicy
	if policy != nil {
		tsPolicy = &qrdbtransport.TSRetentionPolicy{
			DurationNs:   policy.Duration.Nanoseconds(),
			ResolutionNs: policy.Resolution.Nanoseconds(),
			Aggregation:  qrdbtransport.AggFunc(policy.Aggregation),
		}
	}
	return n.client.TimeSeries().TSSetRetention(context.Background(), &qrdbtransport.TSSetRetentionRequest{
		Metric: metric,
		Policy: tsPolicy,
	})
}

// ListMetrics returns metric names matching the optional prefix via the node transport.
func (n *nodeTimeSeriesStorage) ListMetrics(prefix string, limit int) ([]string, error) {
	resp, err := n.client.TimeSeries().TSListMetrics(context.Background(), &qrdbtransport.TSListMetricsRequest{
		Prefix: prefix,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, err
	}
	return resp.Metrics, nil
}

// Close is a no-op: the node owns the underlying time series storage lifecycle.
func (n *nodeTimeSeriesStorage) Close() error { return nil }

// toTSDataPoint converts an interfaces.DataPoint to a qrdb transport TSDataPoint.
func toTSDataPoint(p *interfaces.DataPoint) *qrdbtransport.TSDataPoint {
	if p == nil {
		return nil
	}
	return &qrdbtransport.TSDataPoint{
		Metric:      p.Metric,
		Value:       p.Value,
		TimestampNs: p.Timestamp.UnixNano(),
		Tags:        ifaceTagsToTS(p.Tags),
	}
}

// fromTSDataPoint converts a qrdb transport TSDataPoint to an interfaces.DataPoint.
func fromTSDataPoint(p *qrdbtransport.TSDataPoint) *interfaces.DataPoint {
	if p == nil {
		return nil
	}
	return &interfaces.DataPoint{
		Metric:    p.Metric,
		Value:     p.Value,
		Timestamp: time.Unix(0, p.TimestampNs),
		Tags:      tsTagsToIface(p.Tags),
	}
}

// ifaceTagsToTS converts a map[string]string to a []qrdbtransport.TSTag slice.
func ifaceTagsToTS(tags map[string]string) []qrdbtransport.TSTag {
	if len(tags) == 0 {
		return nil
	}
	result := make([]qrdbtransport.TSTag, 0, len(tags))
	for k, v := range tags {
		result = append(result, qrdbtransport.TSTag{Key: k, Value: v})
	}
	return result
}

// tsTagsToIface converts a []qrdbtransport.TSTag to a map[string]string.
func tsTagsToIface(tags []qrdbtransport.TSTag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	result := make(map[string]string, len(tags))
	for _, t := range tags {
		result[t.Key] = t.Value
	}
	return result
}
