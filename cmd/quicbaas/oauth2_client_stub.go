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

// Package main provides a stub OAuth2 command when IAM is not enabled.
package main

import "github.com/spf13/cobra"

// newOAuth2Command returns a stub command when IAM is not enabled.
func newOAuth2Command() *cobra.Command {
	return &cobra.Command{
		Use:   "oauth2",
		Short: "OAuth2 client and token management",
		Long: `OAuth2 client management commands.

Note: OAuth2 client management requires the iam build tag.
Build with: go build -tags=iam ./cmd/quicbaas/...`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("OAuth2 client management is not enabled in this build.")
			cmd.Println("Build with: go build -tags=iam ./cmd/quicbaas/...")
			return nil
		},
	}
}
