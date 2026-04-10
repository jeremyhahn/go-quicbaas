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

package server

// TestableApp provides a testable server wrapper for integration testing.
type TestableApp struct {
	server *Server
}

// NewTestableApp creates a TestableApp wrapper around the given server.
func NewTestableApp(s *Server) *TestableApp {
	return &TestableApp{server: s}
}

// GetServer returns the underlying server.
func (a *TestableApp) GetServer() *Server {
	return a.server
}

// WaitForLeader is a no-op. QuicBaaS operates as a BaaS client.
func (a *TestableApp) WaitForLeader(shardID uint64) error {
	return nil
}

// GetLeaderID is a no-op. QuicBaaS operates as a BaaS client.
func (a *TestableApp) GetLeaderID(shardID uint64) (uint64, error) {
	return 0, nil
}

// IsLeader returns false. QuicBaaS operates as a BaaS client.
func (a *TestableApp) IsLeader(shardID uint64) bool {
	return false
}

// GetNodeHostInfo returns nil. QuicBaaS operates as a BaaS client.
func (a *TestableApp) GetNodeHostInfo() interface{} {
	return nil
}

// WaitForShards is a no-op. QuicBaaS operates as a BaaS client.
func (a *TestableApp) WaitForShards(shardIDs []uint64) error {
	return nil
}
