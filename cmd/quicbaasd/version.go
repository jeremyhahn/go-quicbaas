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

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/version"
)

var (
	// versionCmd represents the version command.
	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Display version information",
		Long: `Display detailed version information including:
  - Version number
  - Git commit hash
  - Build timestamp
  - Go version
  - Platform information`,
		Run: runVersion,
	}

	// Flags for version command.
	versionShort bool
)

func init() {
	versionCmd.Flags().BoolVarP(&versionShort, "short", "s", false, "print only the version number")
	rootCmd.AddCommand(versionCmd)
}

// runVersion executes the version command.
func runVersion(cmd *cobra.Command, args []string) {
	if versionShort {
		fmt.Fprintln(cmd.OutOrStdout(), version.Short())
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), version.Full())
	}
}
