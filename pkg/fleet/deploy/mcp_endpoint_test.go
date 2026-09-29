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

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet"
)

// mcpEndpointSpec returns a minimal valid spec with the given MCP endpoint.
func mcpEndpointSpec(endpoint string) *Spec {
	return &Spec{
		Server:      "https://c.example.com:8443",
		Tenant:      "acme",
		NodeBind:    "0.0.0.0:9091",
		Backends:    []string{"acp"},
		InstallDir:  "bin",
		DataDir:     "state",
		MCPEndpoint: endpoint,
		Node:        fleet.Config{MaxConcurrentJobs: 1},
	}
}

// TestRenderAgentConfig_MCPEndpoint_Included verifies the rendered daemon
// config carries the MCP endpoint when the spec sets it.
func TestRenderAgentConfig_MCPEndpoint_Included(t *testing.T) {
	const endpoint = "https://cp.example.com:11443/mcp"
	body, err := RenderNodeConfig(mcpEndpointSpec(endpoint), mcpEndpointSpec(endpoint).Layout("/home/u"))
	if err != nil {
		t.Fatalf("RenderNodeConfig: %v", err)
	}
	if !strings.Contains(string(body), "mcp_endpoint: "+endpoint) {
		t.Fatalf("config missing mcp_endpoint %q:\n%s", endpoint, body)
	}
}

// TestRenderAgentConfig_MCPEndpoint_OmittedWhenEmpty verifies the key is
// absent when no endpoint is configured, whitespace included (no behavior
// change for non-MCP deployments).
func TestRenderAgentConfig_MCPEndpoint_OmittedWhenEmpty(t *testing.T) {
	for _, endpoint := range []string{"", "   "} {
		spec := mcpEndpointSpec(endpoint)
		body, err := RenderNodeConfig(spec, spec.Layout("/home/u"))
		if err != nil {
			t.Fatalf("RenderNodeConfig: %v", err)
		}
		if strings.Contains(string(body), "mcp_endpoint") {
			t.Fatalf("config unexpectedly contains mcp_endpoint for endpoint=%q:\n%s", endpoint, body)
		}
	}
}
