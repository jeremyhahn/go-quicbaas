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

import "github.com/jeremyhahn/go-quicbaas/pkg/registry"

// Discovery satisfies the quicbaas.Client interface's service-discovery
// accessor for the SDK client mocks used across the cmd/quicbaas tests. The
// mock types are defined alongside their respective feature tests; these
// satisfier methods are grouped here because the accessor is shared and
// returns nil for tests that do not exercise service discovery.

func (m *mockClient) Discovery() (*registry.Registry, error)              { return nil, nil }
func (m *mockSDKClient) Discovery() (*registry.Registry, error)           { return nil, nil }
func (m *mockSDKClientForMCP) Discovery() (*registry.Registry, error)     { return nil, nil }
func (m *mockRAGSDKClient) Discovery() (*registry.Registry, error)        { return nil, nil }
func (m *mockSDKClientWithVector) Discovery() (*registry.Registry, error) { return nil, nil }
func (m *testSDKClientForVector) Discovery() (*registry.Registry, error)  { return nil, nil }
func (m *mockSDKClientForClusterCovPlusTest) Discovery() (*registry.Registry, error) {
	return nil, nil
}
