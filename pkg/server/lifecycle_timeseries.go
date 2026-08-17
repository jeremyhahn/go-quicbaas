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

//go:build timeseries

package server

import (
	"context"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// initTimeSeriesStorage is a no-op. The qrdb node always owns time-series
// storage; startTimeSeries wires the node's SDK transport directly.
func (s *Server) initTimeSeriesStorage() error {
	// The qrdb engine node owns the time series storage and at-rest encryption.
	// Building a separate PebbleDB store here would create a disconnected
	// duplicate. startTimeSeries wires the node-backed adapter.
	s.logger.Info("Time series storage owned by qrdb node, skipping standalone storage init")
	return nil
}

// startTimeSeries wires the time-series storage adapter that delegates through
// the qrdb node's SDK client embedded transport to the node's in-process time
// series engine. The node's lifecycle owns the underlying store.
func (s *Server) startTimeSeries() error {
	if !s.config.TimeSeries.Enabled {
		s.logger.Info("Time series service is disabled, skipping initialization")
		return nil
	}

	s.timeSeriesStorage = newNodeTimeSeriesStorage(s.getQRDBNode().Client())
	s.logger.Info("Time series storage wired via qrdb node SDK transport")
	return nil
}

// stopTimeSeries closes the time series storage and releases all resources.
// On the node path timeSeriesStorage is a nodeTimeSeriesStorage whose Close
// is a no-op — the qrdb node owns the underlying store lifecycle.
func (s *Server) stopTimeSeries() error {
	if s.timeSeriesStorage == nil {
		return nil
	}

	s.logger.Info("Shutting down time series storage...")

	adapter, ok := s.timeSeriesStorage.(*timeSeriesStorageAdapter)
	if ok {
		if err := adapter.storage.Close(); err != nil {
			s.timeSeriesStorage = nil
			return &qberrors.QBError{
				Code: qberrors.ErrInternal,
				Op:   "server.stopTimeSeries",
				Err:  &ServerCauseError{Message: "failed to close time series storage", Cause: err},
			}
		}
	}

	s.timeSeriesStorage = nil
	s.logger.Info("Time series storage shutdown complete")

	return nil
}

// timeSeriesStorageAdapter wraps a qrdb timeseries.Storage to satisfy the
// quicbaas interfaces.TimeSeriesStorage interface. It converts between the
// qrdb time series types and the quicbaas time series types.
type timeSeriesStorageAdapter struct {
	storage *qrdbsdk.TimeSeriesStorage
}

// newTimeSeriesStorageAdapter creates a new adapter around the qrdb storage.
func newTimeSeriesStorageAdapter(storage *qrdbsdk.TimeSeriesStorage) interfaces.TimeSeriesStorage {
	return &timeSeriesStorageAdapter{storage: storage}
}

// WritePoint writes a single data point to the time series storage.
func (a *timeSeriesStorageAdapter) WritePoint(ctx context.Context, point *interfaces.DataPoint) error {
	qrdbPoint := convertToQRDBDataPoint(point)
	return a.storage.WritePoint(ctx, qrdbPoint)
}

// WriteBatch writes multiple data points atomically.
func (a *timeSeriesStorageAdapter) WriteBatch(points []interfaces.DataPoint) error {
	qrdbPoints := make([]qrdbsdk.TimeSeriesDataPoint, len(points))
	for i := range points {
		qrdbPoints[i] = qrdbsdk.TimeSeriesDataPoint{
			Metric:    points[i].Metric,
			Value:     points[i].Value,
			Timestamp: points[i].Timestamp,
			Tags:      points[i].Tags,
		}
	}
	return a.storage.WriteBatch(qrdbPoints)
}

// QueryRich performs a time range query with optional filtering and aggregation.
func (a *timeSeriesStorageAdapter) QueryRich(ctx context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
	qrdbReq := convertToQRDBQueryRequest(req)
	result, err := a.storage.QueryRich(ctx, qrdbReq)
	if err != nil {
		return nil, err
	}
	return convertFromQRDBQueryResult(result), nil
}

// DeleteSeries deletes data points matching the request criteria.
func (a *timeSeriesStorageAdapter) DeleteSeries(ctx context.Context, req *interfaces.DeleteRequest) error {
	qrdbReq := &qrdbsdk.TimeSeriesDeleteRequest{
		Metric:    req.Metric,
		Tags:      req.Tags,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}
	return a.storage.DeleteSeries(ctx, qrdbReq)
}

// GetRetentionPolicy retrieves the retention policy for a metric.
func (a *timeSeriesStorageAdapter) GetRetentionPolicy(metric string) (*interfaces.RetentionPolicy, error) {
	policy, err := a.storage.GetRetentionPolicy(metric)
	if err != nil {
		return nil, err
	}
	return &interfaces.RetentionPolicy{
		Duration:    policy.Duration,
		Resolution:  policy.Resolution,
		Aggregation: interfaces.AggFunc(policy.Aggregation),
	}, nil
}

// SetRetentionPolicy sets the retention policy for a metric.
func (a *timeSeriesStorageAdapter) SetRetentionPolicy(metric string, policy *interfaces.RetentionPolicy) error {
	qrdbPolicy := &qrdbsdk.TimeSeriesRetentionPolicy{
		Duration:    policy.Duration,
		Resolution:  policy.Resolution,
		Aggregation: qrdbsdk.TimeSeriesAggFunc(policy.Aggregation),
	}
	return a.storage.SetRetentionPolicy(metric, qrdbPolicy)
}

// ListMetrics returns metric names matching the optional prefix.
func (a *timeSeriesStorageAdapter) ListMetrics(prefix string, limit int) ([]string, error) {
	return a.storage.ListMetrics(prefix, limit)
}

// convertToQRDBDataPoint converts an interfaces.DataPoint to a qrdb DataPoint.
func convertToQRDBDataPoint(point *interfaces.DataPoint) *qrdbsdk.TimeSeriesDataPoint {
	return &qrdbsdk.TimeSeriesDataPoint{
		Metric:    point.Metric,
		Value:     point.Value,
		Timestamp: point.Timestamp,
		Tags:      point.Tags,
	}
}

// convertToQRDBQueryRequest converts an interfaces.QueryRequest to a qrdb QueryRequest.
func convertToQRDBQueryRequest(req *interfaces.QueryRequest) *qrdbsdk.TimeSeriesQueryRequest {
	return &qrdbsdk.TimeSeriesQueryRequest{
		Metric:      req.Metric,
		Tags:        req.Tags,
		StartTime:   req.StartTime,
		EndTime:     req.EndTime,
		Aggregation: qrdbsdk.TimeSeriesAggFunc(req.Aggregation),
		Interval:    req.Interval,
		GroupBy:     req.GroupBy,
		Limit:       req.Limit,
		Percentile:  req.Percentile,
	}
}

// convertFromQRDBQueryResult converts a qrdb QueryResult to an interfaces.QueryResult.
func convertFromQRDBQueryResult(result *qrdbsdk.TimeSeriesQueryResult) *interfaces.QueryResult {
	ifResult := &interfaces.QueryResult{
		Metric:          result.Metric,
		Tags:            result.Tags,
		AggregatedValue: result.AggregatedValue,
	}

	if result.Points != nil {
		ifResult.Points = make([]*interfaces.DataPoint, len(result.Points))
		for i, p := range result.Points {
			ifResult.Points[i] = &interfaces.DataPoint{
				Metric:    p.Metric,
				Value:     p.Value,
				Timestamp: p.Timestamp,
				Tags:      p.Tags,
			}
		}
	}

	if result.GroupedResults != nil {
		ifResult.GroupedResults = make([]*interfaces.GroupedResult, len(result.GroupedResults))
		for i, gr := range result.GroupedResults {
			ifGR := &interfaces.GroupedResult{
				Tags:            gr.Tags,
				AggregatedValue: gr.AggregatedValue,
			}
			if gr.Points != nil {
				ifGR.Points = make([]*interfaces.DataPoint, len(gr.Points))
				for j, p := range gr.Points {
					ifGR.Points[j] = &interfaces.DataPoint{
						Metric:    p.Metric,
						Value:     p.Value,
						Timestamp: p.Timestamp,
						Tags:      p.Tags,
					}
				}
			}
			ifResult.GroupedResults[i] = ifGR
		}
	}

	return ifResult
}
