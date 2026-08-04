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

package deploy

import (
	"strings"
	"testing"
)

// mcpEndpointSpec returns a minimal valid spec with the given MCP endpoint.
func mcpEndpointSpec(endpoint string) *Spec {
	return &Spec{
		Server:      "https://c.example.com:8443",
		Tenant:      "acme",
		AgentBind:   "0.0.0.0:9091",
		Backends:    []string{"acp"},
		InstallDir:  "bin",
		DataDir:     "state",
		MCPEndpoint: endpoint,
	}
}

// TestRenderUnit_MCPEndpoint_Included verifies the rendered systemd unit
// carries --mcp-endpoint when the spec sets it.
func TestRenderUnit_MCPEndpoint_Included(t *testing.T) {
	const endpoint = "https://cp.example.com:11443/mcp"
	unit := RenderUnit(mcpEndpointSpec(endpoint))
	if !strings.Contains(unit, "--mcp-endpoint") {
		t.Fatalf("unit missing --mcp-endpoint:\n%s", unit)
	}
	if !strings.Contains(unit, endpoint) {
		t.Fatalf("unit missing endpoint %q:\n%s", endpoint, unit)
	}
}

// TestRenderUnit_MCPEndpoint_OmittedWhenEmpty verifies the flag is absent when
// no endpoint is configured (no behavior change for non-MCP deployments).
func TestRenderUnit_MCPEndpoint_OmittedWhenEmpty(t *testing.T) {
	for _, endpoint := range []string{"", "   "} {
		unit := RenderUnit(mcpEndpointSpec(endpoint))
		if strings.Contains(unit, "--mcp-endpoint") {
			t.Fatalf("unit unexpectedly contains --mcp-endpoint for endpoint=%q:\n%s", endpoint, unit)
		}
	}
}

// TestRenderStartScript_MCPEndpoint_Included verifies the nohup start script
// carries --mcp-endpoint when set, since it shares agentServeArgs with the
// systemd renderer.
func TestRenderStartScript_MCPEndpoint_Included(t *testing.T) {
	const endpoint = "https://cp.example.com:11443/mcp"
	script := RenderStartScript(mcpEndpointSpec(endpoint))
	if !strings.Contains(script, "--mcp-endpoint") {
		t.Fatalf("script missing --mcp-endpoint:\n%s", script)
	}
	if !strings.Contains(script, endpoint) {
		t.Fatalf("script missing endpoint %q:\n%s", endpoint, script)
	}
}

// TestRenderStartScript_MCPEndpoint_OmittedWhenEmpty verifies the flag is
// absent from the start script when no endpoint is configured.
func TestRenderStartScript_MCPEndpoint_OmittedWhenEmpty(t *testing.T) {
	script := RenderStartScript(mcpEndpointSpec(""))
	if strings.Contains(script, "--mcp-endpoint") {
		t.Fatalf("script unexpectedly contains --mcp-endpoint:\n%s", script)
	}
}
