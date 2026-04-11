//go:build timeseries

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

// Package service implements the unified service facade for QuicBaaS.
// This file contains Raft-integrated time series write operations
// delegated through the qrdb SDK.
package service

import (
	"context"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TimeSeriesRaftWriteError indicates a failure to write time series data through Raft.
type TimeSeriesRaftWriteError struct {
	ShardID uint64
	Op      string
	Err     error
}

// Error implements error interface.
func (e *TimeSeriesRaftWriteError) Error() string {
	return "time series raft write failed: shard=" + string(rune(e.ShardID)) + " op=" + e.Op + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *TimeSeriesRaftWriteError) Unwrap() error {
	return e.Err
}

// TimeSeriesRaftNotInitializedError indicates Raft is not initialized for time series writes.
type TimeSeriesRaftNotInitializedError struct{}

// Error implements error interface.
func (e *TimeSeriesRaftNotInitializedError) Error() string {
	return "raft node not initialized for time series writes"
}

// localPointToSDK converts a local tsDataPoint to a qrdbtransport.TSDataPoint
// for use with the SDK client.
func localPointToSDK(p *tsDataPoint) *qrdbtransport.TSDataPoint {
	ts := p.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	return &qrdbtransport.TSDataPoint{
		Metric:      p.Metric,
		Value:       p.Value,
		TimestampNs: ts.UnixNano(),
		Tags:        mapToSDKTags(p.Tags),
	}
}

// writePointViaRaft writes a single data point through the qrdb SDK,
// which handles Raft consensus internally.
func (t *timeSeriesService) writePointViaRaft(ctx context.Context, point *tsDataPoint) error {
	// Route to default shard (single-shard for simplicity)
	shardID := t.shardID

	// Convert local point to SDK transport type
	sdkPoint := localPointToSDK(point)
	client, err := t.svc.qrdbForShard(shardID)
	if err != nil {
		return &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "timeSeriesService.writePointViaRaft",
			Err:  err,
		}
	}
	if err := client.TimeSeries().TSWritePoint(ctx, sdkPoint); err != nil {
		return &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "timeSeriesService.writePointViaRaft",
			Err:  err,
		}
	}

	return nil
}

// writeBatchViaRaft writes multiple data points through the qrdb SDK,
// which handles Raft consensus internally.
func (t *timeSeriesService) writeBatchViaRaft(ctx context.Context, points []*tsDataPoint) (int, error) {
	if len(points) == 0 {
		return 0, nil
	}

	// Route all points to default shard (single-shard for simplicity)
	shardID := t.shardID

	sdkPoints := localPointsToSDK(points)
	client, err := t.svc.qrdbForShard(shardID)
	if err != nil {
		return 0, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "timeSeriesService.writeBatchViaRaft",
			Err:  err,
		}
	}
	if err := client.TimeSeries().TSWriteBatch(ctx, sdkPoints); err != nil {
		return 0, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "timeSeriesService.writeBatchViaRaft",
			Err:  err,
		}
	}

	return len(points), nil
}

// WritePointRaft writes a single data point through Raft consensus via the qrdb SDK.
// This method routes the point to the appropriate shard and delegates through the SDK.
func (t *timeSeriesService) WritePointRaft(ctx context.Context, req *WritePointRequest) (*WritePointResponse, error) {
	// Validate request
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "timeSeriesService.WritePointRaft",
			Err:  &TimeSeriesRequestNilError{},
		}
	}
	if req.Metric == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "timeSeriesService.WritePointRaft",
			Err:  &TimeSeriesMetricRequiredError{},
		}
	}

	// Prepare tenant context for multi-tenant isolation
	tenantID, err := prepareTimeSeriesTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Scope metric to tenant namespace
	scopedMetric := tsTenantScopedMetric(tenantID, req.Metric)
	// Convert to local data point
	ts := req.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	point := &tsDataPoint{
		Metric:    scopedMetric,
		Tags:      req.Tags,
		Value:     req.Value,
		Timestamp: ts,
	}

	// Write through qrdb SDK (Raft consensus handled internally)
	if err := t.writePointViaRaft(ctx, point); err != nil {
		return nil, err
	}

	return &WritePointResponse{
		Success:   true,
		Timestamp: time.Now(),
	}, nil
}

// WriteBatchRaft writes multiple data points through Raft consensus via the qrdb SDK.
// Points are written through the SDK in a single batch.
func (t *timeSeriesService) WriteBatchRaft(ctx context.Context, req *WriteBatchRequest) (*WriteBatchResponse, error) {
	// Validate request
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "timeSeriesService.WriteBatchRaft",
			Err:  &TimeSeriesRequestNilError{},
		}
	}
	if len(req.Points) == 0 {
		return &WriteBatchResponse{
			Success:       true,
			PointsWritten: 0,
			Timestamp:     time.Now(),
		}, nil
	}
	// Prepare tenant context for multi-tenant isolation
	tenantID, err := prepareTimeSeriesTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Convert to local data points and validate
	points := make([]*tsDataPoint, len(req.Points))
	for i, p := range req.Points {
		if p == nil {
			return nil, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "timeSeriesService.WriteBatchRaft",
				Err:  &TimeSeriesPointNilError{Index: i},
			}
		}
		if p.Metric == "" {
			return nil, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "timeSeriesService.WriteBatchRaft",
				Err:  &TimeSeriesMetricRequiredAtIndexError{Index: i},
			}
		}
		ts := p.Timestamp
		if ts.IsZero() {
			ts = time.Now()
		}
		points[i] = &tsDataPoint{
			Metric:    tsTenantScopedMetric(tenantID, p.Metric),
			Tags:      p.Tags,
			Value:     p.Value,
			Timestamp: ts,
		}
	}

	// Write through qrdb SDK (Raft consensus handled internally)
	written, err := t.writeBatchViaRaft(ctx, points)
	if err != nil {
		return nil, err
	}

	return &WriteBatchResponse{
		Success:       true,
		PointsWritten: written,
		Timestamp:     time.Now(),
	}, nil
}

// TimeSeriesRequestNilError indicates the request is nil.
type TimeSeriesRequestNilError struct{}

// Error implements error interface.
func (e *TimeSeriesRequestNilError) Error() string {
	return "request is nil"
}

// TimeSeriesMetricRequiredError indicates the metric field is required.
type TimeSeriesMetricRequiredError struct{}

// Error implements error interface.
func (e *TimeSeriesMetricRequiredError) Error() string {
	return "metric is required"
}

// TimeSeriesPointNilError indicates a point in the batch is nil.
type TimeSeriesPointNilError struct {
	Index int
}

// Error implements error interface.
func (e *TimeSeriesPointNilError) Error() string {
	return "point is nil at index"
}

// TimeSeriesMetricRequiredAtIndexError indicates a metric is required at a specific index.
type TimeSeriesMetricRequiredAtIndexError struct {
	Index int
}

// Error implements error interface.
func (e *TimeSeriesMetricRequiredAtIndexError) Error() string {
	return "metric is required at index"
}
