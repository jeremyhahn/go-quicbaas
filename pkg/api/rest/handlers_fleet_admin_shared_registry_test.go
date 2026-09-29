//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// sharedRegistryFacade wraps MockQuicBaaS so the orchestrator AgentService
// (which serves POST/GET /orchestrator/agents) and the FleetAdminService
// (which serves /orchestrator/agents/{id}/disable|enable) are both reachable
// through one service.QuicBaaS. MockQuicBaaS.Agent() returns nil with no hook,
// so the orchestrator service is supplied by overriding Agent() here.
type sharedRegistryFacade struct {
	*mocks.MockQuicBaaS
	agentSvc orchestration.AgentService
}

func (f *sharedRegistryFacade) Agent() orchestration.AgentService { return f.agentSvc }

// newOrchestratorService builds a real orchestrator AgentService bound to the
// supplied fleet registry. This is the same registry the FleetAdminService must
// resolve disable/enable against.
func newOrchestratorService(t *testing.T, registry orchestration.FleetRegistry) orchestration.AgentService {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout", Format: "console"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	svc, err := orchestration.NewAgentService(orchestration.NewInMemoryAgentStore(), nil, logger.NewAdapter(log))
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	svc.(interface {
		SetFleetRegistry(orchestration.FleetRegistry)
	}).SetFleetRegistry(registry)
	return svc
}

// registerFleetNode POSTs a registration through the orchestrator REST handler
// and returns the server-assigned agent ID. It asserts a 201.
func registerFleetNode(t *testing.T, router http.Handler, tenantID string) string {
	t.Helper()
	body := `{"name":"reviewer-1","host":"host-a","node_type":"reviewer","backend":"claude","tenant_id":"` + tenantID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/fleet", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("register agent: got status %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Node *orchestration.FleetNode `json:"node"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode register response: %v; body=%s", err, w.Body.String())
	}
	if resp.Node == nil || resp.Node.ID == "" {
		t.Fatalf("register response missing agent id; body=%s", w.Body.String())
	}
	return resp.Node.ID
}

// TestRegisterThenDisable_SharedRegistry is the regression test for the 404 on
// agent disable. The orchestrator AgentService and the FleetAdminService were
// wired to SEPARATE in-memory fleet registries, so an agent registered (and
// listed) via /orchestrator/agents was invisible to
// /orchestrator/agents/{id}/disable, which returned 404. With both services
// bound to the SAME registry — the invariant the server now establishes via
// FleetAdminService.SetFleetRegistry — a fleet-registered agent disables 200.
func TestRegisterThenDisable_SharedRegistry(t *testing.T) {
	const tenantID = "system"

	registry := orchestration.NewInMemoryFleetRegistry()

	// Both services share the one registry — exactly what the server's
	// composition root now guarantees.
	orchSvc := newOrchestratorService(t, registry)
	adminSvc := service.NewFleetAdminService(registry)

	mock := mocks.NewMockQuicBaaS()
	mock.FleetAdminServiceFunc = func() service.FleetAdminService { return adminSvc }
	facade := &sharedRegistryFacade{MockQuicBaaS: mock, agentSvc: orchSvc}

	srv := &Server{service: facade}
	router := fleetAdminRouterForTest(srv)

	nodeID := registerFleetNode(t, router, tenantID)

	// The fleet-registered agent must be disable-able through the admin path.
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/fleet/"+nodeID+"/disable?tenant_id="+tenantID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("disable fleet node: got status %d, want 200; body=%s", w.Code, w.Body.String())
	}

	var disableResp DisableFleetNodeRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &disableResp); err != nil {
		t.Fatalf("decode disable response: %v", err)
	}
	if disableResp.NodeID != nodeID || !disableResp.Disabled {
		t.Fatalf("unexpected disable response: %+v", disableResp)
	}

	// The disabled status must be observable in the shared registry.
	agent, err := registry.GetFleetNode(req.Context(), tenantID, nodeID)
	if err != nil {
		t.Fatalf("GetFleetNode after disable: %v", err)
	}
	if agent.Status != orchestration.FleetNodeStatusDisabled {
		t.Fatalf("agent status = %q, want %q", agent.Status, orchestration.FleetNodeStatusDisabled)
	}
}

// TestRegisterThenDisable_SeparateRegistries documents the original defect: when
// FleetAdmin resolves against a DIFFERENT registry than the one the agent was
// registered into, disable returns 404. This is the precise failure the fix
// eliminates by binding both to the same registry.
func TestRegisterThenDisable_SeparateRegistries(t *testing.T) {
	const tenantID = "system"

	fleetRegistry := orchestration.NewInMemoryFleetRegistry()
	adminRegistry := orchestration.NewInMemoryFleetRegistry() // the throwaway store — the bug

	orchSvc := newOrchestratorService(t, fleetRegistry)
	adminSvc := service.NewFleetAdminService(adminRegistry)

	mock := mocks.NewMockQuicBaaS()
	mock.FleetAdminServiceFunc = func() service.FleetAdminService { return adminSvc }
	facade := &sharedRegistryFacade{MockQuicBaaS: mock, agentSvc: orchSvc}

	router := fleetAdminRouterForTest(&Server{service: facade})

	nodeID := registerFleetNode(t, router, tenantID)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/fleet/"+nodeID+"/disable?tenant_id="+tenantID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("separate-registry disable: got status %d, want 404 (defect signature); body=%s",
			w.Code, w.Body.String())
	}
}
