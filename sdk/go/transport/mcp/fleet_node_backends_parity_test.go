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

package mcp

import (
	"reflect"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// go-quicbaas#700 — the fleet listing, and its served "backends", on the MCP
// transport. The SDK derived quicbaas_orchestrator_list_fleet_nodes, which the
// server does not register, so the verb never reached quicbaas_fleet_list.
// =============================================================================

func TestListFleetNodesToolExists(t *testing.T) {
	if got := ToolName("quicbaas.Orchestrator", "ListFleetNodes"); got != "quicbaas_fleet_list" {
		t.Errorf("ListFleetNodes derives the tool %q, but the server registers quicbaas_fleet_list", got)
	}
	if _, ok := orchestratorMCPTransforms["ListFleetNodes"]; !ok {
		t.Error("ListFleetNodes has no result decoder: the tool result would not reach the SDK type")
	}
}

func TestDecodeMCPListFleetNodes_CarriesTheServedBackends(t *testing.T) {
	decoded, err := decodeMCPListFleetNodes([]byte(`{"nodes":[` +
		`{"id":"agent-04","backend":"claude","capabilities":["claude","codex"],"backends":["claude","codex"]},` +
		`{"id":"unknown","backend":"","backends":[]}],"count":2,"total":2,"timestamp":"2026-10-12T00:00:00Z"}`))
	if err != nil {
		t.Fatalf("decodeMCPListFleetNodes: %v", err)
	}
	out, ok := decoded.(*orchestrator.ListFleetNodesResponse)
	if !ok {
		t.Fatalf("got %T, want *orchestrator.ListFleetNodesResponse", decoded)
	}
	if len(out.Nodes) != 2 || out.Count != 2 || out.Total != 2 {
		t.Fatalf("decoded %+v", out)
	}
	if want := []string{"claude", "codex"}; !reflect.DeepEqual(out.Nodes[0].Backends, want) {
		t.Fatalf("Backends = %v, want %v", out.Nodes[0].Backends, want)
	}
	if out.Nodes[1].Backends == nil || len(out.Nodes[1].Backends) != 0 {
		t.Fatalf("a node serving nothing decoded Backends = %#v, want []", out.Nodes[1].Backends)
	}
}

func TestDecodeMCPListFleetNodes_AnEmptyFleetIsAnEmptyList(t *testing.T) {
	decoded, err := decodeMCPListFleetNodes([]byte(`{"count":0}`))
	if err != nil {
		t.Fatalf("decodeMCPListFleetNodes: %v", err)
	}
	if out := decoded.(*orchestrator.ListFleetNodesResponse); out.Nodes == nil {
		t.Fatal("an empty fleet decoded to nil nodes, want []")
	}
}

func TestDecodeMCPListFleetNodes_RejectsGarbage(t *testing.T) {
	if _, err := decodeMCPListFleetNodes([]byte(`["agent-04"]`)); err == nil {
		t.Fatal("a JSON array decoded as the fleet envelope")
	}
}
