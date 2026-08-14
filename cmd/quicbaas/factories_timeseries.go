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
	"github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TimeSeriesClientFactory is a function type for creating time-series clients.
type TimeSeriesClientFactory func() (TimeSeriesClient, error)

// timeSeriesClientFactory creates time-series clients. Defaults to createTimeSeriesClient.
var timeSeriesClientFactory TimeSeriesClientFactory = createTimeSeriesClient

// createTimeSeriesClient creates a client for time-series operations based on transport.
func createTimeSeriesClient() (TimeSeriesClient, error) {
	switch transport {
	case "grpc", "":
		return createGRPCClient()
	case "rest":
		return createTimeSeriesRESTClient()
	case "mcp":
		return createTimeSeriesMCPClient()
	case "quic":
		return nil, errors.Newf(
			errors.ErrConfiguration,
			"createTimeSeriesClient",
			"QUIC transport is not yet supported for time-series CLI commands",
		)
	default:
		return nil, errors.Newf(
			errors.ErrConfiguration,
			"createTimeSeriesClient",
			"unsupported transport: %s (supported: grpc, rest, mcp)", transport,
		)
	}
}

// createTimeSeriesRESTClient creates a REST client for time-series operations.
func createTimeSeriesRESTClient() (TimeSeriesClient, error) {
	return nil, errors.Newf(
		errors.ErrConfiguration,
		"createTimeSeriesRESTClient",
		"REST transport is not yet implemented for time-series commands",
	)
}

// Compile-time interface check: grpc.Client implements TimeSeriesClient.
var _ TimeSeriesClient = (*grpc.Client)(nil)
