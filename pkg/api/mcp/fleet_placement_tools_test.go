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

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

type fleetPlacementMockQuicBaaS struct {
	mockService
	svc service.FleetPlacementService
}

func (m *fleetPlacementMockQuicBaaS) FleetPlacement() service.FleetPlacementService { return m.svc }

// newFleetPlacementRegistry serves fleet placement over hosts h1 and h2 with
// one live agent session on h2.
func newFleetPlacementRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	ctx := context.Background()
	registry := orchestration.NewInMemoryFleetRegistry()
	for _, node := range []*orchestration.FleetNode{
		{ID: "n1", TenantID: "acme", Name: "n1", Host: "h1", NodeType: "worker", Status: orchestration.FleetNodeStatusRunning},
		{ID: "n2", TenantID: "acme", Name: "n2", Host: "h2", NodeType: "worker", Status: orchestration.FleetNodeStatusRunning},
	} {
		if err := registry.RegisterFleetNode(ctx, node); err != nil {
			t.Fatalf("RegisterFleetNode: %v", err)
		}
	}
	sessions := orchestration.NewInMemoryAgentStore()
	if err := sessions.PutSession(ctx, &orchestration.SessionRecord{
		SessionID: "s1", TenantID: "acme", Backend: "bot", HostAffinity: "h2", Status: orchestration.SessionStateActive,
	}); err != nil {
		t.Fatalf("PutSession: %v", err)
	}
	specs, err := orchestration.NewPlacementStore(orchestration.NewMemoryTaskKV())
	if err != nil {
		t.Fatalf("NewPlacementStore: %v", err)
	}
	inventory, err := orchestration.NewFleetInventory(registry)
	if err != nil {
		t.Fatalf("NewFleetInventory: %v", err)
	}
	work, err := orchestration.NewSessionWorkSource(sessions, func(*orchestration.SessionRecord) string { return "agent" })
	if err != nil {
		t.Fatalf("NewSessionWorkSource: %v", err)
	}
	drift, err := orchestration.NewDriftReporter(specs, inventory, work)
	if err != nil {
		t.Fatalf("NewDriftReporter: %v", err)
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(&fleetPlacementMockQuicBaaS{svc: service.NewFleetPlacementService(specs, drift)}, log)
}

func TestFleetPlacementTools(t *testing.T) {
	reg := newFleetPlacementRegistry(t)
	acme := tenantctx.WithTenant(context.Background(), "acme")
	call := func(name string, args map[string]interface{}) (interface{}, error) {
		if !isFleetPlacementTool(name) {
			t.Fatalf("%s is not dispatched as a fleet placement tool", name)
		}
		return reg.executeFleetPlacementTool(acme, name, args)
	}

	out, err := call("quicbaas_fleet_placement_declare", map[string]interface{}{
		"name": "agents",
		"spec": map[string]interface{}{"role": "agent", "hosts": []interface{}{"h1"}},
	})
	if err != nil {
		t.Fatalf("declare: %v", err)
	}
	if stored := out.(*orchestration.PlacementSpec); stored.Name != "agents" || stored.TenantID != "acme" || stored.Generation != 1 {
		t.Fatalf("declare = %+v, want agents for acme at generation 1", stored)
	}
	if out, err = call("quicbaas_fleet_placement_get", map[string]interface{}{"name": "agents"}); err != nil ||
		out.(*orchestration.PlacementSpec).Role != "agent" {
		t.Fatalf("get = %v, %v", out, err)
	}
	if out, err = call("quicbaas_fleet_placement_list", map[string]interface{}{}); err != nil ||
		out.(map[string]interface{})["count"] != 1 {
		t.Fatalf("list = %v, %v", out, err)
	}
	out, err = call("quicbaas_fleet_placement_drift", map[string]interface{}{"name": "agents"})
	if err != nil {
		t.Fatalf("drift: %v", err)
	}
	if report := out.(*orchestration.DriftReport); report.ObservedWork != 1 || len(report.Findings) != 1 ||
		report.Findings[0].Host != "h2" || report.Findings[0].Kind != orchestration.DriftHostNotAdmitted {
		t.Fatalf("drift = %+v, want s1 on h2 not admitted", report)
	}
	if out, err = call("quicbaas_fleet_placement_withdraw", map[string]interface{}{"name": "agents"}); err != nil ||
		out.(map[string]interface{})["withdrawn"] != true {
		t.Fatalf("withdraw = %v, %v", out, err)
	}
	if _, err = call("quicbaas_fleet_placement_get", map[string]interface{}{"name": "agents"}); !dberrors.IsNotFound(err) {
		t.Fatalf("get after withdraw = %v, want not found", err)
	}
}

func TestFleetPlacementToolsRefuse(t *testing.T) {
	reg := newFleetPlacementRegistry(t)
	acme := tenantctx.WithTenant(context.Background(), "acme")
	if _, err := reg.executeFleetPlacementTool(acme, "quicbaas_fleet_placement_declare", map[string]interface{}{
		"name": "agents", "spec": map[string]interface{}{"role": "agent", "hosts": []interface{}{"h1"}},
	}); err != nil {
		t.Fatalf("declare: %v", err)
	}
	cases := []struct {
		name string
		tool string
		args map[string]interface{}
	}{
		{"spec of the wrong shape", "quicbaas_fleet_placement_declare", map[string]interface{}{"name": "x", "spec": map[string]interface{}{"hosts": "h1"}}},
		{"create an existing spec", "quicbaas_fleet_placement_declare", map[string]interface{}{"name": "agents", "spec": map[string]interface{}{"role": "agent", "hosts": []interface{}{"h2"}}}},
		{"absent spec", "quicbaas_fleet_placement_drift", map[string]interface{}{"name": "absent"}},
		{"another tenant", "quicbaas_fleet_placement_list", map[string]interface{}{"tenant_id": "other"}},
		{"unknown tool", "quicbaas_fleet_placement_nope", nil},
	}
	for _, tc := range cases {
		if _, err := reg.executeFleetPlacementTool(acme, tc.tool, tc.args); err == nil {
			t.Errorf("%s: want an error", tc.name)
		}
	}

	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	bare := NewToolRegistry(&mockService{}, log)
	if _, err := bare.executeFleetPlacementTool(acme, "quicbaas_fleet_placement_list", nil); err == nil {
		t.Fatal("no fleet placement service: want unavailable")
	}
}

func TestFleetPlacementToolsListedAndAuthorized(t *testing.T) {
	reg := newFleetPlacementRegistry(t)
	listed := map[string]bool{}
	for _, tool := range reg.ListTools() {
		listed[tool.Name] = true
	}
	for _, name := range []string{"quicbaas_fleet_placement_declare", "quicbaas_fleet_placement_get",
		"quicbaas_fleet_placement_list", "quicbaas_fleet_placement_withdraw", "quicbaas_fleet_placement_drift"} {
		if !listed[name] || !isFleetPlacementTool(name) {
			t.Errorf("%s: listed %v, dispatched %v", name, listed[name], isFleetPlacementTool(name))
		}
	}
}
