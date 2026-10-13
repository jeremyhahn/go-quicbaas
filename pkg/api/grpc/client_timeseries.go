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

package grpc

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
)

// TSWritePoint writes a single data point to time series storage.
func (c *Client) TSWritePoint(ctx context.Context, req *cli.WritePointRequest) (*cli.WritePointResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get client (from pool or single connection)
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}

	// Convert CLI request to proto request
	protoReq := &proto.TSWritePointRequest{
		Metric:    req.Metric,
		Tags:      req.Tags,
		Value:     req.Value,
		Timestamp: req.Timestamp.UnixNano(),
	}

	// Execute with retry
	var resp *proto.TSWritePointResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.TSWritePoint(ctx, protoReq)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "TSWritePoint")
	}

	return &cli.WritePointResponse{
		Success:   resp.Success,
		Timestamp: time.Unix(0, resp.Timestamp),
	}, nil
}

// TSWriteBatch writes multiple data points atomically.
func (c *Client) TSWriteBatch(ctx context.Context, req *cli.WriteBatchRequest) (*cli.WriteBatchResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get client (from pool or single connection)
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}

	// Convert CLI request to proto request
	points := make([]*proto.TSDataPoint, len(req.Points))
	for i, p := range req.Points {
		points[i] = &proto.TSDataPoint{
			Metric:    p.Metric,
			Tags:      p.Tags,
			Value:     p.Value,
			Timestamp: p.Timestamp.UnixNano(),
		}
	}

	protoReq := &proto.TSWriteBatchRequest{
		Points: points,
	}

	// Execute with retry
	var resp *proto.TSWriteBatchResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.TSWriteBatch(ctx, protoReq)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "TSWriteBatch")
	}

	return &cli.WriteBatchResponse{
		Success:       resp.Success,
		PointsWritten: int(resp.PointsWritten),
		Timestamp:     time.Unix(0, resp.Timestamp),
	}, nil
}

// TSQuery performs time range queries with optional aggregation.
func (c *Client) TSQuery(ctx context.Context, req *cli.TSQueryRequest) (*cli.TSQueryResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get client (from pool or single connection)
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}

	// Convert CLI request to proto request
	limit, err := clientInt32("limit", req.Limit)
	if err != nil {
		return nil, err
	}
	protoReq := &proto.TSQueryRequest{
		Metric:          req.Metric,
		Tags:            req.Tags,
		StartTime:       req.StartTime.UnixNano(),
		EndTime:         req.EndTime.UnixNano(),
		Aggregation:     req.Aggregation,
		IntervalSeconds: int64(req.IntervalSec),
		GroupBy:         req.GroupBy,
		Limit:           limit,
		Percentile:      req.Percentile,
	}

	// Execute with retry
	var resp *proto.TSQueryResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.TSQuery(ctx, protoReq)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "TSQuery")
	}

	// Convert proto response to CLI response
	result := &cli.TSQueryResponse{
		Metric:    resp.Metric,
		Tags:      resp.Tags,
		Timestamp: time.Unix(0, resp.Timestamp),
	}

	// Convert points
	if len(resp.Points) > 0 {
		result.Points = make([]cli.TSDataPoint, len(resp.Points))
		for i, p := range resp.Points {
			result.Points[i] = cli.TSDataPoint{
				Metric:    p.Metric,
				Tags:      p.Tags,
				Value:     p.Value,
				Timestamp: time.Unix(0, p.Timestamp),
			}
		}
	}

	// Convert grouped results
	if len(resp.GroupedResults) > 0 {
		result.GroupedResults = make([]cli.TSGroupedResult, len(resp.GroupedResults))
		for i, gr := range resp.GroupedResults {
			grResult := cli.TSGroupedResult{
				Tags: gr.Tags,
			}
			if len(gr.Points) > 0 {
				grResult.Points = make([]cli.TSDataPoint, len(gr.Points))
				for j, p := range gr.Points {
					grResult.Points[j] = cli.TSDataPoint{
						Metric:    p.Metric,
						Tags:      p.Tags,
						Value:     p.Value,
						Timestamp: time.Unix(0, p.Timestamp),
					}
				}
			}
			if gr.HasAggregatedValue {
				val := gr.AggregatedValue
				grResult.AggregatedValue = &val
			}
			result.GroupedResults[i] = grResult
		}
	}

	// Handle aggregated value
	if resp.HasAggregatedValue {
		val := resp.AggregatedValue
		result.AggregatedValue = &val
	}

	return result, nil
}

// TSDeleteSeries deletes a time series or specific data points.
func (c *Client) TSDeleteSeries(ctx context.Context, req *cli.TSDeleteRequest) (*cli.TSDeleteResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get client (from pool or single connection)
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}

	// Convert CLI request to proto request
	protoReq := &proto.TSDeleteSeriesRequest{
		Metric: req.Metric,
		Tags:   req.Tags,
	}

	if req.StartTime != nil && req.EndTime != nil {
		protoReq.HasTimeRange = true
		protoReq.StartTime = req.StartTime.UnixNano()
		protoReq.EndTime = req.EndTime.UnixNano()
	}

	// Execute with retry
	var resp *proto.TSDeleteSeriesResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.TSDeleteSeries(ctx, protoReq)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "TSDeleteSeries")
	}

	return &cli.TSDeleteResponse{
		Success:   resp.Success,
		Timestamp: time.Unix(0, resp.Timestamp),
	}, nil
}

// TSGetRetentionPolicy retrieves the retention policy for a metric.
func (c *Client) TSGetRetentionPolicy(ctx context.Context, req *cli.TSGetRetentionPolicyRequest) (*cli.TSGetRetentionPolicyResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get client (from pool or single connection)
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}

	// Convert CLI request to proto request
	protoReq := &proto.TSGetRetentionPolicyRequest{
		Metric: req.Metric,
	}

	// Execute with retry
	var resp *proto.TSGetRetentionPolicyResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.TSGetRetentionPolicy(ctx, protoReq)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "TSGetRetentionPolicy")
	}

	result := &cli.TSGetRetentionPolicyResponse{
		Metric:    resp.Metric,
		Timestamp: time.Unix(0, resp.Timestamp),
	}

	if resp.Policy != nil {
		result.Policy = &cli.TSRetentionPolicy{
			Duration:    time.Duration(resp.Policy.DurationSeconds) * time.Second,
			Resolution:  time.Duration(resp.Policy.ResolutionSeconds) * time.Second,
			Aggregation: resp.Policy.Aggregation,
		}
	}

	return result, nil
}

// TSSetRetentionPolicy sets the retention policy for a metric.
func (c *Client) TSSetRetentionPolicy(ctx context.Context, req *cli.TSSetRetentionPolicyRequest) (*cli.TSSetRetentionPolicyResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get client (from pool or single connection)
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}

	// Convert CLI request to proto request
	protoReq := &proto.TSSetRetentionPolicyRequest{
		Metric: req.Metric,
	}

	if req.Policy != nil {
		protoReq.Policy = &proto.TSRetentionPolicy{
			DurationSeconds:   int64(req.Policy.Duration.Seconds()),
			ResolutionSeconds: int64(req.Policy.Resolution.Seconds()),
			Aggregation:       req.Policy.Aggregation,
		}
	}

	// Execute with retry
	var resp *proto.TSSetRetentionPolicyResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.TSSetRetentionPolicy(ctx, protoReq)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "TSSetRetentionPolicy")
	}

	return &cli.TSSetRetentionPolicyResponse{
		Success:   resp.Success,
		Timestamp: time.Unix(0, resp.Timestamp),
	}, nil
}

// TSListMetrics lists all available metrics.
func (c *Client) TSListMetrics(ctx context.Context, req *cli.TSListMetricsRequest) (*cli.TSListMetricsResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	// Add metadata
	ctx = c.addMetadata(ctx)

	// Get client (from pool or single connection)
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}

	// Convert CLI request to proto request
	limit, err := clientInt32("limit", req.Limit)
	if err != nil {
		return nil, err
	}
	protoReq := &proto.TSListMetricsRequest{
		Prefix: req.Prefix,
		Tags:   req.Tags,
		Limit:  limit,
		Cursor: req.Cursor,
	}

	// Execute with retry
	var resp *proto.TSListMetricsResponse
	err = retryWithBackoff(
		ctx,
		c.opts.maxRetries,
		c.opts.retryBackoff,
		c.opts.maxRetryBackoff,
		func() error {
			var rpcErr error
			resp, rpcErr = client.TSListMetrics(ctx, protoReq)
			return rpcErr
		},
	)
	if err != nil {
		return nil, convertGRPCError(err, "TSListMetrics")
	}

	return &cli.TSListMetricsResponse{
		Metrics:    resp.Metrics,
		NextCursor: resp.NextCursor,
		Timestamp:  time.Unix(0, resp.Timestamp),
	}, nil
}
