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

//go:build !iam

// Package main provides stub IAM command when IAM is not enabled.
package main

import "github.com/spf13/cobra"

// newIAMCommand returns a stub command when IAM feature is not enabled.
func newIAMCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "iam",
		Short: "Identity and Access Management (IAM) commands",
		Long: `IAM commands for managing tenants, users, groups, roles, and policies.

Note: IAM feature is currently disabled. Build with -tags=iam to enable.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("IAM feature is not enabled in this build.")
			cmd.Println("Build with: go build -tags=iam ./cmd/quicbaas/...")
			return nil
		},
	}
}
