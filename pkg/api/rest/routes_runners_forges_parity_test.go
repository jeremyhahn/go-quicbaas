//go:build rest

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

package rest

import "testing"

// =============================================================================
// go-quicbaas#606 — the REST half of ListRunners / ListForges parity.
//
// Both verbs shipped on REST alone; gRPC (and so the Unix socket that serves
// the same wire over a UDS) refused them. Now that they exist on every
// transport, REST is no longer the only definition of what they cost, so this
// file pins REST's side of the contract.
//
// The literals are duplicated here ON PURPOSE, exactly as
// TestWebAuthnCredentialPermissionsMatchOtherTransports duplicates its own:
// none of the three permission tables is exported to the others, so importing
// one would only make this test agree with whatever that package currently
// says. Drift in any plane fails that plane's own test:
//
//	gRPC: pkg/api/grpc/authz_runners_forges_parity_test.go
//	MCP:  pkg/api/mcp/authz_runners_forges_parity_test.go
// =============================================================================

// TestRunnersForgesRoutesExist proves the two routes are still mounted. They
// are the REST end of a verb that now exists on five transports; deleting the
// route here without deleting the RPC would put the surfaces back out of step
// in the other direction.
func TestRunnersForgesRoutesExist(t *testing.T) {
	want := map[string]struct{}{
		"GET /api/v1/orchestrator/runners": {},
		"GET /api/v1/sdlc/forges":          {},
	}

	for _, spec := range Routes() {
		delete(want, spec.Method+" "+spec.Pattern)
	}

	for key := range want {
		t.Errorf("route %q is not in the route table: the verb it serves exists on gRPC, QUIC, "+
			"Unix, MCP and the embedded transport, so dropping it from REST re-opens go-quicbaas#606", key)
	}
}

// TestRunnersForgesPermissionsMatchOtherTransports pins the grants the two
// routes carry to the ones the same verbs carry everywhere else.
func TestRunnersForgesPermissionsMatchOtherTransports(t *testing.T) {
	want := map[string]frozenPermission{
		// Mirrored by /quicbaas.orchestrator.v1.OrchestratorService/ListRunners
		// and the MCP tool quicbaas_orchestrator_list_runners.
		"GET /api/v1/orchestrator/runners": {
			auth: RouteAuthPermission, resource: "orchestrator", action: "status",
		},
		// Mirrored by /quicbaas.sdlc.v1.SDLCService/ListForges and the MCP
		// tool sdlc_forges.
		"GET /api/v1/sdlc/forges": {
			auth: RouteAuthPermission, resource: "sdlc", action: "read",
		},
	}

	for key, expected := range want {
		got, ok := frozenRoutePermissions[key]
		if !ok {
			t.Errorf("route %q has no frozen permission entry", key)
			continue
		}
		if got != expected {
			t.Errorf("route %q is gated %s:%s, but the same verb is %s:%s on gRPC and MCP; "+
				"the grant must not depend on which transport carried the call",
				key, got.resource, got.action, expected.resource, expected.action)
		}
	}
}
