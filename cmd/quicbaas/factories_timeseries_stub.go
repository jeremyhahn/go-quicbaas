//go:build !timeseries

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
	stderrors "errors"
)

// ErrTimeSeriesNotCompiled is returned when time-series functions are called without timeseries build tag.
var ErrTimeSeriesNotCompiled = stderrors.New("timeseries support not compiled - build with -tags timeseries")

// TimeSeriesClientFactory is a function type for creating time-series clients.
type TimeSeriesClientFactory func() (TimeSeriesClient, error)

// timeSeriesClientFactory creates time-series clients.
// This is a stub - timeseries support is not compiled in.
var timeSeriesClientFactory TimeSeriesClientFactory = createTimeSeriesClient

// createTimeSeriesClient returns an error since timeseries is not compiled.
func createTimeSeriesClient() (TimeSeriesClient, error) {
	return nil, ErrTimeSeriesNotCompiled
}
