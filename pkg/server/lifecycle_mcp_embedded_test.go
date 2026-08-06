//go:build mcp && !integration

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

// lifecycle_mcp_embedded_test.go covers the embedded-mode guard of
// startMCPServer: an embedded (library-mode) server exposes NO network
// listeners, so the MCP HTTP listener — and therefore the MCP authorization
// gate — must never be constructed, matching the gRPC/REST/QUIC guards.

package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// startMCPServer must be a silent no-op in embedded mode even when MCP is
// enabled in configuration (the DefaultConfig posture). Constructing the MCP
// authz gate here would refuse-to-start embedded servers that have no OIDC
// provider — and binding port 11443 from a library embed is never correct.
func TestStartMCPServer_EmbeddedModeSkips(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Embedded = true
	cfg.Server.MCP.Enabled = true

	s := &Server{
		config: cfg,
		logger: buildTestLogger(t),
	}

	require.NoError(t, s.startMCPServer(),
		"embedded mode must skip MCP without error")
	require.Nil(t, s.mcpServer, "no MCP server may be constructed in embedded mode")
}

// startMCPServer must still honour the MCP.Enabled=false guard outside
// embedded mode (regression companion to the embedded skip: both guards
// return nil without constructing a server).
func TestStartMCPServer_DisabledSkips(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Embedded = false
	cfg.Server.MCP.Enabled = false

	s := &Server{
		config: cfg,
		logger: buildTestLogger(t),
	}

	require.NoError(t, s.startMCPServer(),
		"disabled MCP must skip without error")
	require.Nil(t, s.mcpServer, "no MCP server may be constructed when disabled")
}
