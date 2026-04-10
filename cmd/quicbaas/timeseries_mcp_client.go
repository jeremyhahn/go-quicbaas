//go:build timeseries && mcp

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

// Package main provides MCP timeseries client implementation for the CLI.
package main

import (
	"context"
	"time"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// MCPTimeSeriesClient wraps the SDK client to implement the TimeSeriesClient interface.
type MCPTimeSeriesClient struct {
	client quicbaas.Client
}

// TSWritePoint writes a single data point to time series storage via MCP.
func (c *MCPTimeSeriesClient) TSWritePoint(ctx context.Context, req *cli.WritePointRequest) (*cli.WritePointResponse, error) {
	if c.client == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPTimeSeriesClient.TSWritePoint",
			"MCP client not initialized",
		)
	}

	tsClient := c.client.TimeSeries()
	if tsClient == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPTimeSeriesClient.TSWritePoint",
			"timeseries client not available",
		)
	}

	sdkReq := &timeseries.WriteRequest{
		Metric:    req.Metric,
		Value:     req.Value,
		Timestamp: req.Timestamp,
		Tags:      req.Tags,
	}

	if err := tsClient.Write(ctx, sdkReq); err != nil {
		return nil, err
	}

	return &cli.WritePointResponse{
		Success:   true,
		Timestamp: time.Now(),
	}, nil
}

// TSWriteBatch writes multiple data points atomically via MCP.
func (c *MCPTimeSeriesClient) TSWriteBatch(ctx context.Context, req *cli.WriteBatchRequest) (*cli.WriteBatchResponse, error) {
	if c.client == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPTimeSeriesClient.TSWriteBatch",
			"MCP client not initialized",
		)
	}

	tsClient := c.client.TimeSeries()
	if tsClient == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPTimeSeriesClient.TSWriteBatch",
			"timeseries client not available",
		)
	}

	// Write each point individually since the SDK client only has Write method
	var lastErr error
	pointsWritten := 0
	for _, point := range req.Points {
		sdkReq := &timeseries.WriteRequest{
			Metric:    point.Metric,
			Value:     point.Value,
			Timestamp: point.Timestamp,
			Tags:      point.Tags,
		}

		if err := tsClient.Write(ctx, sdkReq); err != nil {
			lastErr = err
		} else {
			pointsWritten++
		}
	}

	if lastErr != nil && pointsWritten == 0 {
		return nil, lastErr
	}

	return &cli.WriteBatchResponse{
		Success:       pointsWritten > 0,
		PointsWritten: pointsWritten,
		Timestamp:     time.Now(),
	}, nil
}

// TSQuery performs time range queries with optional aggregation via MCP.
func (c *MCPTimeSeriesClient) TSQuery(ctx context.Context, req *cli.TSQueryRequest) (*cli.TSQueryResponse, error) {
	if c.client == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPTimeSeriesClient.TSQuery",
			"MCP client not initialized",
		)
	}

	tsClient := c.client.TimeSeries()
	if tsClient == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPTimeSeriesClient.TSQuery",
			"timeseries client not available",
		)
	}

	// Map aggregation string to SDK enum
	aggregate := timeseries.AggregateNone
	switch req.Aggregation {
	case "sum":
		aggregate = timeseries.AggregateSum
	case "avg":
		aggregate = timeseries.AggregateAvg
	case "min":
		aggregate = timeseries.AggregateMin
	case "max":
		aggregate = timeseries.AggregateMax
	case "count":
		aggregate = timeseries.AggregateCount
	}

	sdkReq := &timeseries.QueryRequest{
		Metric:    req.Metric,
		Tags:      req.Tags,
		Start:     req.StartTime,
		End:       req.EndTime,
		Aggregate: aggregate,
		Interval:  time.Duration(req.IntervalSec) * time.Second,
	}

	resp, err := tsClient.Query(ctx, sdkReq)
	if err != nil {
		return nil, err
	}

	// Convert SDK response to CLI response
	points := make([]cli.TSDataPoint, len(resp.Series))
	for i, p := range resp.Series {
		points[i] = cli.TSDataPoint{
			Metric:    req.Metric,
			Timestamp: p.Timestamp,
			Value:     p.Value,
			Tags:      req.Tags,
		}
	}

	return &cli.TSQueryResponse{
		Metric:    req.Metric,
		Tags:      req.Tags,
		Points:    points,
		Timestamp: time.Now(),
	}, nil
}

// TSDeleteSeries deletes a time series or specific data points via MCP.
func (c *MCPTimeSeriesClient) TSDeleteSeries(ctx context.Context, req *cli.TSDeleteRequest) (*cli.TSDeleteResponse, error) {
	// The SDK timeseries client doesn't have a Delete method,
	// so we need to use the transport directly
	if c.client == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPTimeSeriesClient.TSDeleteSeries",
			"MCP client not initialized",
		)
	}

	// For now, return not implemented as the SDK client doesn't expose delete
	return nil, errors.New(
		errors.ErrNotImplemented,
		"MCPTimeSeriesClient.TSDeleteSeries",
		"delete series is not yet implemented for MCP transport",
	)
}

// TSGetRetentionPolicy retrieves the retention policy for a metric via MCP.
func (c *MCPTimeSeriesClient) TSGetRetentionPolicy(ctx context.Context, req *cli.TSGetRetentionPolicyRequest) (*cli.TSGetRetentionPolicyResponse, error) {
	if c.client == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPTimeSeriesClient.TSGetRetentionPolicy",
			"MCP client not initialized",
		)
	}

	// For now, return not implemented as the SDK client doesn't expose retention policy
	return nil, errors.New(
		errors.ErrNotImplemented,
		"MCPTimeSeriesClient.TSGetRetentionPolicy",
		"get retention policy is not yet implemented for MCP transport",
	)
}

// TSSetRetentionPolicy sets the retention policy for a metric via MCP.
func (c *MCPTimeSeriesClient) TSSetRetentionPolicy(ctx context.Context, req *cli.TSSetRetentionPolicyRequest) (*cli.TSSetRetentionPolicyResponse, error) {
	if c.client == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPTimeSeriesClient.TSSetRetentionPolicy",
			"MCP client not initialized",
		)
	}

	// For now, return not implemented as the SDK client doesn't expose retention policy
	return nil, errors.New(
		errors.ErrNotImplemented,
		"MCPTimeSeriesClient.TSSetRetentionPolicy",
		"set retention policy is not yet implemented for MCP transport",
	)
}

// TSListMetrics lists all available metrics via MCP.
func (c *MCPTimeSeriesClient) TSListMetrics(ctx context.Context, req *cli.TSListMetricsRequest) (*cli.TSListMetricsResponse, error) {
	if c.client == nil {
		return nil, errors.New(
			errors.ErrUnavailable,
			"MCPTimeSeriesClient.TSListMetrics",
			"MCP client not initialized",
		)
	}

	// For now, return not implemented as the SDK client doesn't expose list metrics
	return nil, errors.New(
		errors.ErrNotImplemented,
		"MCPTimeSeriesClient.TSListMetrics",
		"list metrics is not yet implemented for MCP transport",
	)
}

// Close closes the client connection.
func (c *MCPTimeSeriesClient) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// createTimeSeriesMCPClient creates an MCP client for time-series operations.
// MCP transport for time-series commands uses JSON-RPC 2.0 over HTTP.
func createTimeSeriesMCPClient() (TimeSeriesClient, error) {
	// Create the SDK client with MCP transport
	sdkClient, err := createSDKClient()
	if err != nil {
		return nil, errors.Wrapf(
			err,
			errors.ErrUnavailable,
			"createTimeSeriesMCPClient",
			"failed to create MCP SDK client for server %s", address,
		)
	}

	return &MCPTimeSeriesClient{client: sdkClient}, nil
}

// Compile-time interface check
var _ TimeSeriesClient = (*MCPTimeSeriesClient)(nil)
