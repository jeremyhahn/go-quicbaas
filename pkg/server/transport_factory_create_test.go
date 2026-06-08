//go:build raft

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

package server

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// createNetworkClient builds a working SDK client for each network transport
// type, invoking the per-type constructor in networkTransportHandlers. The
// constructors only build the transport object (no connection is opened), so a
// plausible address / socket path is sufficient to drive every branch — REST,
// QUIC, MCP and Unix, complementing the gRPC path already covered elsewhere.
func TestTransportFactory_CreateNetworkClient_EachTransport(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		cfg       *config.QRDBConfig
	}{
		{
			name:      "rest",
			transport: config.QRDBTransportREST,
			cfg:       &config.QRDBConfig{Transport: config.QRDBTransportREST, Address: "https://localhost:9443"},
		},
		{
			name:      "quic",
			transport: config.QRDBTransportQUIC,
			cfg:       &config.QRDBConfig{Transport: config.QRDBTransportQUIC, Address: "localhost:9444"},
		},
		{
			name:      "mcp",
			transport: config.QRDBTransportMCP,
			cfg:       &config.QRDBConfig{Transport: config.QRDBTransportMCP, Address: "https://localhost:9445"},
		},
		{
			name:      "unix",
			transport: config.QRDBTransportUnix,
			cfg:       &config.QRDBConfig{Transport: config.QRDBTransportUnix, UnixSocketPath: "/tmp/quicbaas-test.sock"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			factory := newTransportFactory(tc.cfg, nil, nil)
			client, err := factory.createNetworkClient()
			if err != nil {
				t.Fatalf("createNetworkClient(%s): %v", tc.transport, err)
			}
			if client == nil {
				t.Fatalf("createNetworkClient(%s) returned nil client", tc.transport)
			}
			_ = client.Close()
		})
	}
}
