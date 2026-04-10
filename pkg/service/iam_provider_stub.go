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

package service

// initializeIAMService creates the IAM service provider if IAM is enabled.
// Returns nil if IAM is not configured or not enabled.
func initializeIAMService(_ *serviceBuilder) (*iamServiceProvider, error) {
	// IAM service initialization is handled by the iam build tag.
	// This stub returns nil when IAM is not enabled.
	return nil, nil
}

// initializeIAMProvider initializes the IAM provider for REST handlers.
// This stub returns nil when IAM is not enabled.
func initializeIAMProvider(_ *serviceBuilder) IAMServiceProvider {
	return nil
}
