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

// Package main provides IAM client factory functions for the QuicBaaS CLI.
package main

import (
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// IAMClientFactory is a function type for creating IAM clients.
type IAMClientFactory func() (iam.Client, error)

// iamClientFactory creates IAM clients. Defaults to createSDKIAMClient.
var iamClientFactory IAMClientFactory = createSDKIAMClient

// createIAMClient is a convenience function that calls the iamClientFactory.
func createIAMClient() (iam.Client, error) {
	return iamClientFactory()
}

// createSDKIAMClient creates an IAM client using the SDK directly.
func createSDKIAMClient() (iam.Client, error) {
	client, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return client.Identity().IAM(), nil
}

// resetIAMFactory resets the IAM factory to its default implementation.
// This is useful in tests to restore default behavior after using mocks.
func resetIAMFactory() {
	iamClientFactory = createSDKIAMClient
}
