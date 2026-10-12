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

package embedded

import (
	"context"
	"reflect"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// go-quicbaas#700 — the served "backends" of a fleet listing on the embedded
// transport, rendered by the same FleetNode.ServedBackends REST renders.
// =============================================================================

func TestEmbeddedListFleetNodes_CarriesTheServedBackends(t *testing.T) {
	svc := &orchMock{agent: &fakeAgentService{fleetNodes: []*orchestration.FleetNode{
		{ID: "agent-04", Backend: "claude", Capabilities: []string{"claude", "codex", "opencode"}},
		{ID: "unknown"},
	}}}

	resp, err := handleFleetList(context.Background(), svc, &orchestratorSDK.ListFleetNodesRequest{TenantID: "t-1"})
	if err != nil {
		t.Fatalf("ListFleetNodes: %v", err)
	}
	out, ok := resp.(*orchestratorSDK.ListFleetNodesResponse)
	if !ok {
		t.Fatalf("got %T, want *orchestrator.ListFleetNodesResponse", resp)
	}
	if len(out.Nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(out.Nodes))
	}
	if want := []string{"claude", "codex", "opencode"}; !reflect.DeepEqual(out.Nodes[0].Backends, want) {
		t.Fatalf("Backends = %v, want %v: the primary once, and every served name", out.Nodes[0].Backends, want)
	}
	if out.Nodes[1].Backends == nil || len(out.Nodes[1].Backends) != 0 {
		t.Fatalf("a node serving nothing rendered Backends = %#v, want []", out.Nodes[1].Backends)
	}
}

func TestEmbeddedListFleetNodes_ServiceErrorIsReturned(t *testing.T) {
	svc := &orchMock{agent: &fakeAgentService{fail: true}}
	if _, err := handleFleetList(context.Background(), svc, &orchestratorSDK.ListFleetNodesRequest{TenantID: "t-1"}); err == nil {
		t.Fatal("a failing registry produced a listing")
	}
}
