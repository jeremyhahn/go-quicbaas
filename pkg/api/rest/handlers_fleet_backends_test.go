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

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestListFleetNodes_RendersTheServedBackends proves the REST fleet listing
// renders "backends" through the shared orchestration renderer
// (go-quicbaas#700), and never as null.
func TestListFleetNodes_RendersTheServedBackends(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listAgents = []*orchestration.FleetNode{
		{ID: "agent-04", Backend: "claude", Capabilities: []string{"claude", "codex", "opencode"}, RegisteredAt: 2},
		{ID: "unknown", RegisteredAt: 1},
	}

	w := httptest.NewRecorder()
	server.HandleListFleetNodes(w, orchReq(http.MethodGet, "/api/v1/fleet?tenant_id=acme", nil, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	var body struct {
		Nodes []struct {
			ID       string    `json:"id"`
			Backends *[]string `json:"backends"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding the listing: %v", err)
	}
	served := map[string]*[]string{}
	for _, node := range body.Nodes {
		served[node.ID] = node.Backends
	}
	if got := served["agent-04"]; got == nil || !reflect.DeepEqual(*got, []string{"claude", "codex", "opencode"}) {
		t.Errorf("agent-04 backends = %v, want [claude codex opencode]", got)
	}
	if got := served["unknown"]; got == nil || len(*got) != 0 {
		t.Errorf("a node serving nothing rendered backends = %v, want []", got)
	}
}

// TestListFleetNodes_RefusalRendersNoNodes proves a failing registry is an
// error response, not a listing.
func TestListFleetNodes_RefusalRendersNoNodes(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listAgentsErr = orchestration.ErrFleetNodeNotFound

	w := httptest.NewRecorder()
	server.HandleListFleetNodes(w, orchReq(http.MethodGet, "/api/v1/fleet?tenant_id=acme", nil, nil))
	if w.Code == http.StatusOK {
		t.Fatalf("a failing registry answered 200: %s", w.Body.String())
	}
}
