//go:build mcp

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
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// sdlcRunningQuicBaaS is a node whose SDLC module is up. executeSDLCForges
// reads the process-wide forge registry rather than the service, so the service
// only has to be present — which is exactly the distinction the tool gates on.
type sdlcRunningQuicBaaS struct {
	service.QuicBaaS
}

func (sdlcRunningQuicBaaS) SDLC() *service.SDLCService { return &service.SDLCService{} }

// newSDLCToolRegistry builds a tool registry over a node whose SDLC module is
// running or absent.
func newSDLCToolRegistry(t *testing.T, moduleRunning bool) *ToolRegistry {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	var svc service.QuicBaaS = testutil.NewMockQuicBaaS()
	if moduleRunning {
		svc = sdlcRunningQuicBaaS{QuicBaaS: svc}
	}
	return NewToolRegistry(svc, log)
}

// =============================================================================
// go-quicbaas#606 — the MCP half of ListRunners / ListForges parity.
//
// quicbaas_orchestrator_list_runners already existed here; sdlc_forges did not
// exist anywhere but REST. Both are now served on every transport, so this file
// pins MCP's gate and MCP's rendering to the same contract the other planes
// pin in their own packages:
//
//	REST: pkg/api/rest/routes_runners_forges_parity_test.go
//	gRPC: pkg/api/grpc/authz_runners_forges_parity_test.go
// =============================================================================

// TestRunnersForgesMCPPermissions pins the MCP gate on both tools.
func TestRunnersForgesMCPPermissions(t *testing.T) {
	want := map[string]qrdbToolResource{
		// Mirrors GET /api/v1/orchestrator/runners and
		// /quicbaas.orchestrator.v1.OrchestratorService/ListRunners.
		"quicbaas_orchestrator_list_runners": {"orchestrator", "status"},
		// Mirrors GET /api/v1/sdlc/forges and
		// /quicbaas.sdlc.v1.SDLCService/ListForges.
		"sdlc_forges": {"sdlc", "read"},
	}

	for tool, expected := range want {
		got, ok := qrdbToolExactMapping[tool]
		if !ok {
			t.Errorf("tool %q has no permission mapping: the authz gate is default-deny, "+
				"so it would be invisible in tools/list", tool)
			continue
		}
		if got != expected {
			t.Errorf("tool %q is gated %v, but REST and gRPC gate the same verb %v; "+
				"the grant must not depend on which transport carried the call",
				tool, got, expected)
		}
	}
}

// TestSDLCForgesToolIsDispatchable proves sdlc_forges is a tool the registry
// actually routes, not only a permission entry. An entry with no dispatch is a
// grant for a verb nobody can call.
func TestSDLCForgesToolIsDispatchable(t *testing.T) {
	if !isSDLCTool("sdlc_forges") {
		t.Fatal("sdlc_forges is not in the SDLC tool dispatch table")
	}
}

// TestExecuteSDLCForges renders the build's registered forge adapters. The
// sdlc package registers the Gitea factory in its own init, so a build carrying
// the module always carries at least that one.
func TestExecuteSDLCForges(t *testing.T) {
	registry := newSDLCToolRegistry(t, true)

	result, err := registry.executeSDLCForges(context.Background(), nil)
	if err != nil {
		t.Fatalf("executeSDLCForges: %v", err)
	}

	payload, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("executeSDLCForges returned %T, want map[string]interface{}", result)
	}
	forges, ok := payload["forges"].([]string)
	if !ok {
		t.Fatalf("forges is %T, want []string", payload["forges"])
	}
	registered := sdlc.RegisteredForgeTypes()
	if len(forges) != len(registered) {
		t.Fatalf("got %d forges, registry holds %d", len(forges), len(registered))
	}
	if payload["count"] != len(registered) {
		t.Errorf("count is %v but %d forges were returned", payload["count"], len(forges))
	}
	for i, want := range registered {
		if forges[i] != string(want) {
			t.Fatalf("forge %d: got %q, want %q (the registry's sort order must be preserved)",
				i, forges[i], want)
		}
	}
}

// TestExecuteSDLCForgesWithoutTheModule refuses on a node that does not run the
// module, matching the REST handler's 503 and the gRPC handler's Unavailable:
// "the module is off" and "this build carries no adapters" are different
// answers and must stay different.
func TestExecuteSDLCForgesWithoutTheModule(t *testing.T) {
	registry := newSDLCToolRegistry(t, false)

	if _, err := registry.executeSDLCForges(context.Background(), nil); err == nil {
		t.Fatal("executeSDLCForges answered on a node with no SDLC module")
	}
}
