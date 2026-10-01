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

import quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"

// Registry satisfies the quicbaas.Client interface's service registry
// accessor for the SDK client mocks used across the cmd/quicbaas tests. The
// mock types are defined alongside their respective feature tests; these
// satisfier methods are grouped here because the accessor is shared and
// returns nil for tests that do not exercise the service registry.

func (m *mockClient) Registry() quicbaas.RegistryClient                         { return nil }
func (m *mockSDKClient) Registry() quicbaas.RegistryClient                      { return nil }
func (m *mockSDKClientForMCP) Registry() quicbaas.RegistryClient                { return nil }
func (m *mockRAGSDKClient) Registry() quicbaas.RegistryClient                   { return nil }
func (m *mockSDKClientWithVector) Registry() quicbaas.RegistryClient            { return nil }
func (m *testSDKClientForVector) Registry() quicbaas.RegistryClient             { return nil }
func (m *mockSDKClientForClusterCovPlusTest) Registry() quicbaas.RegistryClient { return nil }
