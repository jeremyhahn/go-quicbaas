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

// Package main provides the QuicBaaS command-line interface.
package main

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
)

// TimeSeriesClient defines the interface for time-series operations.
// This is the full interface used when timeseries build tag is enabled.
type TimeSeriesClient interface {
	// TSWritePoint writes a single data point to time series storage.
	TSWritePoint(ctx context.Context, req *cli.WritePointRequest) (*cli.WritePointResponse, error)

	// TSWriteBatch writes multiple data points atomically.
	TSWriteBatch(ctx context.Context, req *cli.WriteBatchRequest) (*cli.WriteBatchResponse, error)

	// TSQuery performs time range queries with optional aggregation.
	TSQuery(ctx context.Context, req *cli.TSQueryRequest) (*cli.TSQueryResponse, error)

	// TSDeleteSeries deletes a time series or specific data points.
	TSDeleteSeries(ctx context.Context, req *cli.TSDeleteRequest) (*cli.TSDeleteResponse, error)

	// TSGetRetentionPolicy retrieves the retention policy for a metric.
	TSGetRetentionPolicy(ctx context.Context, req *cli.TSGetRetentionPolicyRequest) (*cli.TSGetRetentionPolicyResponse, error)

	// TSSetRetentionPolicy sets the retention policy for a metric.
	TSSetRetentionPolicy(ctx context.Context, req *cli.TSSetRetentionPolicyRequest) (*cli.TSSetRetentionPolicyResponse, error)

	// TSListMetrics lists all available metrics.
	TSListMetrics(ctx context.Context, req *cli.TSListMetricsRequest) (*cli.TSListMetricsResponse, error)

	// Close closes the client connection.
	Close() error
}
