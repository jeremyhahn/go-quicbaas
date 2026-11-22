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
	"encoding/json"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
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
	svc := service.NewFleetPlacementService(specs, drift)
	ledger, err := orchestration.NewConsentLedger(orchestration.NewMemoryTaskKV())
	if err != nil {
		t.Fatalf("NewConsentLedger: %v", err)
	}
	svc.(service.FleetPlacementConsentBinder).BindFleetPlacementConsent(ledger)
	if _, err := ledger.Raise(orchestration.ContextWithTenant(ctx, "acme"), "acme", orchestration.ConsentMove{
		SpecName: "agents", SpecGeneration: 1, Class: "agent", WorkID: "s1", SourceHost: "h2",
	}); err != nil {
		t.Fatalf("Raise: %v", err)
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(&fleetPlacementMockQuicBaaS{svc: svc}, log)
}

// fleetPlacementConsentID returns the one pending consent request the
// registry was built with.
func fleetPlacementConsentID(t *testing.T, reg *ToolRegistry) string {
	t.Helper()
	out, err := reg.executeFleetPlacementTool(tenantctx.WithTenant(context.Background(), "acme"),
		"quicbaas_fleet_placement_consent_list", map[string]interface{}{"state": "pending"})
	if err != nil {
		t.Fatalf("consent list: %v", err)
	}
	listed := out.(map[string]interface{})
	requests := listed["requests"].([]*orchestration.PlacementConsentRequest)
	if listed["count"] != 1 || len(requests) != 1 {
		t.Fatalf("consent list = %v, want the one pending request", listed)
	}
	return requests[0].ID
}

// asAlice returns the acme context carrying the authenticated subject alice.
func asAlice(ctx context.Context) context.Context {
	return auth.SubjectIntoContext(ctx, interfaces.Subject{ID: "alice", TenantID: "acme", Authenticated: true})
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

	id := fleetPlacementConsentID(t, reg)
	out, err = reg.executeFleetPlacementTool(asAlice(acme), "quicbaas_fleet_placement_consent_answer", map[string]interface{}{
		"id": id, "state": "granted", "reason": "fine", "by": "mallory",
	})
	if err != nil {
		t.Fatalf("consent answer: %v", err)
	}
	if answered := out.(*orchestration.PlacementConsentRequest); answered.ID != id || answered.By != "alice" ||
		answered.State != orchestration.ConsentGranted || answered.Reason != "fine" {
		t.Fatalf("consent answer = %+v, want granted by alice", answered)
	}
	if out, err = call("quicbaas_fleet_placement_consent_list", map[string]interface{}{"state": "pending"}); err != nil ||
		out.(map[string]interface{})["count"] != 0 {
		t.Fatalf("pending after answer = %v, %v, want none", out, err)
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

	id := fleetPlacementConsentID(t, reg)
	alice := asAlice(acme)
	answer := "quicbaas_fleet_placement_consent_answer"
	consentCases := []struct {
		name string
		ctx  context.Context
		tool string
		args map[string]interface{}
	}{
		{"answer with an invalid state", alice, answer, map[string]interface{}{"id": id, "state": "maybe"}},
		{"answer an unknown request", alice, answer, map[string]interface{}{"id": "absent", "state": "granted"}},
		{"answer with no subject", acme, answer, map[string]interface{}{"id": id, "state": "granted"}},
		{"consents of another tenant", alice, "quicbaas_fleet_placement_consent_list", map[string]interface{}{"tenant_id": "other"}},
	}
	for _, tc := range consentCases {
		if _, err := reg.executeFleetPlacementTool(tc.ctx, tc.tool, tc.args); err == nil {
			t.Errorf("%s: want an error", tc.name)
		}
	}
	if _, err := reg.executeFleetPlacementTool(alice, answer, map[string]interface{}{"id": id, "state": "refused"}); err != nil {
		t.Fatalf("first answer: %v", err)
	}
	if _, err := reg.executeFleetPlacementTool(alice, answer, map[string]interface{}{"id": id, "state": "granted"}); err == nil {
		t.Error("second answer: want an error")
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
		"quicbaas_fleet_placement_list", "quicbaas_fleet_placement_withdraw", "quicbaas_fleet_placement_drift",
		"quicbaas_fleet_placement_consent_list", "quicbaas_fleet_placement_consent_answer"} {
		if !listed[name] || !isFleetPlacementTool(name) {
			t.Errorf("%s: listed %v, dispatched %v", name, listed[name], isFleetPlacementTool(name))
		}
	}
	for name, want := range map[string][2]string{
		"quicbaas_fleet_placement_consent_list":   {"placement", "read"},
		"quicbaas_fleet_placement_consent_answer": {"placement", "consent"},
	} {
		resource, action, ok := getToolQRDBPermission(name)
		if !ok || resource != want[0] || action != want[1] {
			t.Errorf("%s: permission %s:%s (mapped %v), want %s:%s", name, resource, action, ok, want[0], want[1])
		}
	}
}

// TestFleetPlacementDeclareSchemaMatchesSDKShape checks the arguments the SDK
// sends for DeclareFleetPlacementSpecRequest ({tenant_id, spec} with the name
// inside spec) against the registered declare schema, and declares them.
func TestFleetPlacementDeclareSchemaMatchesSDKShape(t *testing.T) {
	reg := newFleetPlacementRegistry(t)
	tool := toolByName(t, reg, "quicbaas_fleet_placement_declare")
	required := requiredArgs(t, tool)
	if len(required) != 1 || required[0] != "spec" {
		t.Fatalf("declare required = %v, want [spec]", required)
	}
	props := schemaProperties(t, tool)
	for _, key := range []string{"tenant_id", "spec", "name"} {
		if _, ok := props[key]; !ok {
			t.Errorf("declare schema has no %q property", key)
		}
	}

	var args map[string]interface{}
	sdkShape := `{"tenant_id":"","spec":{"name":"agents","role":"agent","hosts":["h1"]}}`
	if err := json.Unmarshal([]byte(sdkShape), &args); err != nil {
		t.Fatalf("unmarshal SDK arguments: %v", err)
	}
	for _, key := range required {
		if _, ok := args[key]; !ok {
			t.Errorf("SDK arguments lack required %q", key)
		}
	}

	out, err := reg.executeFleetPlacementTool(tenantctx.WithTenant(context.Background(), "acme"),
		"quicbaas_fleet_placement_declare", args)
	if err != nil {
		t.Fatalf("declare with SDK arguments: %v", err)
	}
	spec, ok := out.(*orchestration.PlacementSpec)
	if !ok {
		t.Fatalf("declare result is %T, want *orchestration.PlacementSpec", out)
	}
	if spec.Name != "agents" || spec.TenantID != "acme" || spec.Generation != 1 {
		t.Errorf("declared spec = name %q tenant %q generation %d, want agents acme 1", spec.Name, spec.TenantID, spec.Generation)
	}
}
