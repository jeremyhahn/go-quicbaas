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

// TimeSeriesClient defines the interface for time-series operations.
// This is a stub interface used when timeseries build tag is not enabled.
// Note: Time-series support requires the timeseries build tag.
type TimeSeriesClient interface {
	// Close closes the client connection.
	Close() error
}
