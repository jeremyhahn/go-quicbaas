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

package server

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

type fleetPlacementFacade struct{ svc service.FleetPlacementService }

func (f fleetPlacementFacade) FleetPlacement() service.FleetPlacementService { return f.svc }

// A session that landed on a fleet host is agent work; one with no recorded
// host ran on the control plane and no spec governs it.
func TestFleetPlacementSessionRole(t *testing.T) {
	t.Parallel()
	if got := fleetPlacementSessionRole(&orchestration.SessionRecord{HostAffinity: "h1"}); got != "agent" {
		t.Fatalf("role of a hosted session = %q, want agent", got)
	}
	if got := fleetPlacementSessionRole(&orchestration.SessionRecord{}); got != "" {
		t.Fatalf("role of a hostless session = %q, want none", got)
	}
}

// Binding turns an unbound service into one that declares specs and reports
// drift over the registry and the session store; a facade without fleet
// placement is left alone.
func TestBindFleetPlacement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if err := bindFleetPlacement(struct{}{}, nil, nil, nil); err != nil {
		t.Fatalf("bind on a facade without fleet placement = %v, want nil", err)
	}

	registry := orchestration.NewInMemoryFleetRegistry()
	if err := registry.RegisterFleetNode(ctx, &orchestration.FleetNode{
		ID: "n1", TenantID: "acme", Name: "n1", Host: "h1", NodeType: "worker", Status: orchestration.FleetNodeStatusRunning,
	}); err != nil {
		t.Fatalf("RegisterFleetNode: %v", err)
	}
	sessions := orchestration.NewInMemoryAgentStore()
	for _, rec := range []*orchestration.SessionRecord{
		{SessionID: "s1", TenantID: "acme", Backend: "bot", HostAffinity: "h1", Status: orchestration.SessionStateActive},
		{SessionID: "s2", TenantID: "acme", Backend: "bot", HostAffinity: "h9", Status: orchestration.SessionStateActive},
	} {
		if err := sessions.PutSession(ctx, rec); err != nil {
			t.Fatalf("PutSession: %v", err)
		}
	}

	svc := service.NewFleetPlacementService(nil, nil)
	acme := tenantctx.WithTenant(ctx, "acme")
	if _, err := svc.ListPlacementSpecs(acme, ""); !stderrors.Is(err, service.ErrFleetPlacementUnbound) {
		t.Fatalf("list before bind = %v, want ErrFleetPlacementUnbound", err)
	}
	if _, err := svc.ListConsentRequests(acme, "", ""); !stderrors.Is(err, service.ErrFleetPlacementConsentUnbound) {
		t.Fatalf("consent list before bind = %v, want ErrFleetPlacementConsentUnbound", err)
	}
	if err := bindFleetPlacement(fleetPlacementFacade{svc}, orchestration.NewMemoryTaskKV(), registry, sessions); err != nil {
		t.Fatalf("bindFleetPlacement: %v", err)
	}
	if _, err := svc.DeclarePlacementSpec(acme, "", &orchestration.PlacementSpec{Name: "agents", Role: "agent", Hosts: []string{"h1"}}); err != nil {
		t.Fatalf("DeclarePlacementSpec after bind: %v", err)
	}
	report, err := svc.GetPlacementDrift(acme, "", "agents")
	if err != nil {
		t.Fatalf("GetPlacementDrift: %v", err)
	}
	if report.ObservedWork != 2 || len(report.Findings) != 1 || report.Findings[0].WorkID != "s2" {
		t.Fatalf("report = %+v, want two sessions observed and s2 off the admitted hosts", report)
	}
	consents, err := svc.ListConsentRequests(acme, "", "")
	if err != nil {
		t.Fatalf("ListConsentRequests after bind: %v", err)
	}
	if consents == nil || len(consents) != 0 {
		t.Fatalf("consents = %#v, want an empty list", consents)
	}
}
