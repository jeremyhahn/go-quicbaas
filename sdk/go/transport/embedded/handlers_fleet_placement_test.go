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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

type fleetPlacementMock struct {
	*mockQuicBaaS
	fleetPlacement service.FleetPlacementService
}

func (m *fleetPlacementMock) FleetPlacement() service.FleetPlacementService { return m.fleetPlacement }

// newFleetPlacementClient serves fleet placement in process over a fleet of
// hosts h1 and h2 with one live agent session on h2.
func newFleetPlacementClient(t *testing.T) orchestratorSDK.Client {
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
	tr, err := New(&fleetPlacementMock{mockQuicBaaS: &mockQuicBaaS{}, fleetPlacement: service.NewFleetPlacementService(specs, drift)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return orchestratorSDK.NewClient(tr)
}

func TestEmbeddedFleetPlacement_DeclareGetListDriftWithdraw(t *testing.T) {
	client := newFleetPlacementClient(t)
	acme := transport.ContextWithTenant(context.Background(), "acme")

	stored, err := client.DeclareFleetPlacementSpec(acme, &orchestratorSDK.DeclareFleetPlacementSpecRequest{
		Spec: &orchestratorSDK.FleetPlacementSpec{Name: "agents", Role: "agent", Hosts: []string{"h1"}},
	})
	if err != nil {
		t.Fatalf("DeclareFleetPlacementSpec: %v", err)
	}
	if stored.TenantID != "acme" || stored.Generation != 1 {
		t.Fatalf("stored = %+v, want acme at generation 1", stored)
	}
	got, err := client.GetFleetPlacementSpec(acme, &orchestratorSDK.FleetPlacementSpecRequest{Name: "agents"})
	if err != nil || got.Role != "agent" {
		t.Fatalf("GetFleetPlacementSpec = %+v, %v", got, err)
	}
	list, err := client.ListFleetPlacementSpecs(acme, nil)
	if err != nil || list.Count != 1 || list.Specs[0].Name != "agents" {
		t.Fatalf("ListFleetPlacementSpecs = %+v, %v", list, err)
	}
	report, err := client.GetFleetPlacementDrift(acme, &orchestratorSDK.FleetPlacementSpecRequest{Name: "agents"})
	if err != nil {
		t.Fatalf("GetFleetPlacementDrift: %v", err)
	}
	if report.ObservedWork != 1 || len(report.Findings) != 1 || report.Findings[0].Host != "h2" ||
		report.Findings[0].Kind != string(orchestration.DriftHostNotAdmitted) {
		t.Fatalf("report = %+v, want s1 on h2 not admitted", report)
	}
	if _, err := client.WithdrawFleetPlacementSpec(acme, &orchestratorSDK.FleetPlacementSpecRequest{Name: "agents"}); err != nil {
		t.Fatalf("WithdrawFleetPlacementSpec: %v", err)
	}
	_, err = client.GetFleetPlacementSpec(acme, &orchestratorSDK.FleetPlacementSpecRequest{Name: "agents"})
	if !sdkerrors.IsNotFound(err) {
		t.Fatalf("get after withdraw = %v, want not found", err)
	}
	if _, err := client.ListFleetPlacementSpecs(acme, &orchestratorSDK.ListFleetPlacementSpecsRequest{TenantID: "other"}); err == nil {
		t.Fatal("listing another tenant's specs succeeded")
	}
}

func TestEmbeddedFleetPlacement_UnavailableWithoutTheService(t *testing.T) {
	tr, err := New(&mockQuicBaaS{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client := orchestratorSDK.NewClient(tr)
	acme := transport.ContextWithTenant(context.Background(), "acme")
	if _, err := client.ListFleetPlacementSpecs(acme, nil); err == nil {
		t.Fatal("ListFleetPlacementSpecs without the service succeeded")
	}
}

// The SDK and server types share their JSON form: a fully populated server
// spec and report survive a round trip through the SDK types unchanged, so
// a field added to one side only fails here.
func TestEmbeddedFleetPlacement_TypesRoundTrip(t *testing.T) {
	spec := &orchestration.PlacementSpec{
		TenantID: "acme", Name: "n", Role: "r", HostCount: 2, Hosts: []string{"h"},
		Profile: &orchestration.FleetJobProfile{CPUCores: 1.5, MemoryBytes: 3, Measured: true, SampleSize: 4,
			UpdatedAt: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)},
		RequiredCapabilities: []string{"gpu"}, Actuate: true, Generation: 7,
	}
	sdkSpec, err := convertFleetPlacement[orchestratorSDK.FleetPlacementSpec](spec)
	if err != nil {
		t.Fatal(err)
	}
	back, err := convertFleetPlacement[orchestration.PlacementSpec](sdkSpec)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(spec, back) {
		t.Fatalf("spec round trip:\n got %+v\nwant %+v", back, spec)
	}

	report := &orchestration.DriftReport{
		TenantID: "acme", Spec: "n", Role: "r", Generation: 7, Disposition: orchestration.PlacementForbidden,
		ObservedWork: 3, AdmissibleHosts: []string{"h1"},
		Findings: []orchestration.DriftFinding{{
			Kind: orchestration.DriftHostNotAdmitted, Field: "hosts", Host: "h2", WorkID: "s1", Missing: []string{"gpu"},
			Remedy: orchestration.DriftRemedy{Action: "move", From: "h2", Destinations: []string{"h1"}},
		}},
	}
	sdkReport, err := convertFleetPlacement[orchestratorSDK.FleetPlacementDrift](report)
	if err != nil {
		t.Fatal(err)
	}
	backReport, err := convertFleetPlacement[orchestration.DriftReport](sdkReport)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report, backReport) {
		t.Fatalf("report round trip:\n got %+v\nwant %+v", backReport, report)
	}
}
