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

// Package main: coverage for the agent-serve MCP preset resolver wiring.
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/fleet"
)

// baseACPConfig returns a minimal valid ACP-backend config with the on-disk
// mTLS material paths populated (as the deploy flow uploads them).
func baseACPConfig() *fleet.Config {
	return &fleet.Config{
		Backends:     []fleet.BackendKind{fleet.BackendACP},
		NodeCertFile: "/var/lib/quicbaas-agent/agent.crt",
		NodeKeyFile:  "/var/lib/quicbaas-agent/agent.key",
		ServerCAFile: "/var/lib/quicbaas-agent/server-ca.crt",
	}
}

// TestBuildAgentServeMCPResolver_NilWhenNoEndpoint verifies that no resolver is
// constructed when MCPEndpoint is empty — preserving current behavior (the
// "quicbaas" preset stays harmlessly unresolved).
func TestBuildAgentServeMCPResolver_NilWhenNoEndpoint(t *testing.T) {
	cfg := baseACPConfig()
	logger := newFleetServeLogger()

	assert.Nil(t, buildFleetServeMCPResolver(cfg, logger), "no endpoint -> nil resolver")

	cfg.MCPEndpoint = "   "
	assert.Nil(t, buildFleetServeMCPResolver(cfg, logger), "blank endpoint -> nil resolver")
}

// TestBuildAgentServeMCPResolver_SetWhenEndpoint verifies a resolver is built
// when MCPEndpoint is configured, pointing at the on-disk cert material.
func TestBuildAgentServeMCPResolver_SetWhenEndpoint(t *testing.T) {
	cfg := baseACPConfig()
	cfg.MCPEndpoint = "https://cp.example.com:11443/mcp"
	logger := newFleetServeLogger()

	resolver := buildFleetServeMCPResolver(cfg, logger)
	require.NotNil(t, resolver, "endpoint set -> resolver wired")

	url, env, cleanup, err := resolver.Resolve(t.Context(), "sess", "acme")
	require.NoError(t, err)
	assert.Equal(t, cfg.MCPEndpoint, url)
	assert.Nil(t, cleanup)
	assert.Equal(t, cfg.NodeCertFile, env["QUICBAAS_MCP_CERT"])
	assert.Equal(t, cfg.NodeKeyFile, env["QUICBAAS_MCP_KEY"])
	assert.Equal(t, cfg.ServerCAFile, env["QUICBAAS_MCP_CA"])
}

// TestBuildAgentServeMCPResolver_NilOnConstructionFailure verifies graceful
// degradation: when the endpoint is set but a required cert path is missing,
// the resolver is disabled (nil) rather than aborting the daemon.
func TestBuildAgentServeMCPResolver_NilOnConstructionFailure(t *testing.T) {
	cfg := baseACPConfig()
	cfg.MCPEndpoint = "https://cp.example.com:11443/mcp"
	cfg.NodeCertFile = "" // force NewFileMCPPresetResolver to fail
	logger := newFleetServeLogger()

	assert.Nil(t, buildFleetServeMCPResolver(cfg, logger))
}

// TestBuildAgentBackends_BuildsWithMCPEndpoint verifies buildFleetBackends
// still constructs the ACP backends when an MCP endpoint is configured (the
// resolver is attached to each ACP backend's config).
func TestBuildAgentBackends_BuildsWithMCPEndpoint(t *testing.T) {
	cfg := baseACPConfig()
	cfg.MCPEndpoint = "https://cp.example.com:11443/mcp"
	logger := newFleetServeLogger()

	backends, err := buildFleetBackends(cfg, logger)
	require.NoError(t, err)
	assert.Contains(t, backends, "claude")
	assert.Contains(t, backends, "codex")
	assert.Contains(t, backends, "opencode")
}
