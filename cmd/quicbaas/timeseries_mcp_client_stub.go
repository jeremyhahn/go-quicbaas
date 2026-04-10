//go:build timeseries && !mcp

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

// Package main provides stub implementation when MCP is not enabled.
package main

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// createTimeSeriesMCPClient is a stub that returns an error when MCP is not enabled.
func createTimeSeriesMCPClient() (TimeSeriesClient, error) {
	return nil, errors.New(
		errors.ErrNotImplemented,
		"createTimeSeriesMCPClient",
		"MCP transport requires -tags=mcp build flag",
	)
}
