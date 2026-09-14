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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// =============================================================================
// go-quicbaas#606 — ListRunners / ListForges on the MCP transport.
//
// The MCP surface is a CLOSED set of tools: a method with no tool name is
// refused with a typed Unimplemented before the server is asked for anything.
// That is the fail-closed behaviour a missing verb produces here, and it is
// what these pins prevent.
// =============================================================================

// TestRunnersForgesToolsExist pins both verbs to the tool the server registers.
func TestRunnersForgesToolsExist(t *testing.T) {
	// ListRunners has no orchestratorMethodTools override: the transport's
	// generic service_method derivation already produces the tool the server
	// registers, quicbaas_orchestrator_list_runners.
	if got := ToolName("quicbaas.Orchestrator", "ListRunners"); got != "quicbaas_orchestrator_list_runners" {
		t.Errorf("ListRunners derives the tool %q, but the server registers quicbaas_orchestrator_list_runners", got)
	}
	if _, ok := orchestratorMCPTransforms["ListRunners"]; !ok {
		t.Error("ListRunners has no result decoder: the tool result would not reach the SDK type")
	}

	// The SDLC tools are named after the noun, not the RPC, so ListForges
	// needs an explicit entry or the closed-set check refuses it outright.
	got, ok := sdlcToolName("ListForges")
	if !ok {
		t.Fatal("ListForges has no SDLC tool name: the transport refuses the verb before dialling")
	}
	if got != "sdlc_forges" {
		t.Errorf("ListForges maps to the tool %q, but the server registers sdlc_forges", got)
	}
	if _, ok := sdlcMCPTransforms["ListForges"]; !ok {
		t.Error("ListForges has no result decoder: the tool result would not reach the SDK type")
	}
}

// TestDecodeMCPSDLCForges decodes the server's tool result into the SDK
// envelope, preserving the order the server sorted the adapters into.
func TestDecodeMCPSDLCForges(t *testing.T) {
	decoded, err := decodeMCPSDLCForges([]byte(`{"forges":["gitea","github"],"count":2}`))
	if err != nil {
		t.Fatalf("decodeMCPSDLCForges: %v", err)
	}
	forges, ok := decoded.(*sdlc.ListForgesResponse)
	if !ok {
		t.Fatalf("got %T, want *sdlc.ListForgesResponse", decoded)
	}
	if len(forges.Forges) != 2 || forges.Count != 2 ||
		forges.Forges[0] != "gitea" || forges.Forges[1] != "github" {
		t.Fatalf("decoded %+v; the server's sort order must survive", forges)
	}
}

// TestDecodeMCPSDLCForgesRejectsGarbage refuses a result that is not the
// envelope rather than handing the caller a zero value that reads as "this
// build carries no forge adapters".
func TestDecodeMCPSDLCForgesRejectsGarbage(t *testing.T) {
	if _, err := decodeMCPSDLCForges([]byte(`["gitea"]`)); err == nil {
		t.Fatal("a JSON array decoded as the forges envelope")
	}
}
