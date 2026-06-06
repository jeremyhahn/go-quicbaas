// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

//go:build quicbaas_signal

package main

import "github.com/spf13/cobra"

// registerSignalBridgeCommand attaches the signalbridge subcommand tree to
// the root cobra command. Only compiled when the quicbaas_signal build tag
// is active.
func registerSignalBridgeCommand(root *cobra.Command) {
	root.AddCommand(newSignalBridgeCommand())
}
