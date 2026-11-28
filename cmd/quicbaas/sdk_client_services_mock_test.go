// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

import quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"

// Services satisfies the quicbaas.Client interface's service plane accessor
// for the SDK client mocks used across the cmd/quicbaas tests, grouped here
// as the registry accessor is in sdk_client_registry_mock_test.go. It
// returns nil for tests that do not exercise the service plane.

func (m *mockClient) Services() quicbaas.ServicesClient                         { return nil }
func (m *mockSDKClient) Services() quicbaas.ServicesClient                      { return nil }
func (m *mockSDKClientForMCP) Services() quicbaas.ServicesClient                { return nil }
func (m *mockRAGSDKClient) Services() quicbaas.ServicesClient                   { return nil }
func (m *mockSDKClientWithVector) Services() quicbaas.ServicesClient            { return nil }
func (m *testSDKClientForVector) Services() quicbaas.ServicesClient             { return nil }
func (m *mockSDKClientForClusterCovPlusTest) Services() quicbaas.ServicesClient { return nil }
