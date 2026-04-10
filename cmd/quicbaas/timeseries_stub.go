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

import "github.com/spf13/cobra"

// newTimeSeriesCommands returns a stub command when timeseries support is not compiled.
func newTimeSeriesCommands() *cobra.Command {
	return &cobra.Command{
		Use:   "timeseries",
		Short: "Time series database commands",
		Long: `Time series commands for storing and querying time-stamped data.

Note: Time series feature is currently disabled. Build with -tags=timeseries to enable.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("Time series feature is not enabled in this build.")
			cmd.Println("Build with: go build -tags=timeseries ./cmd/quicbaas/...")
			return nil
		},
	}
}
