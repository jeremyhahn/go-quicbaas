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

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

type fleetPlacementFacade struct{ svc service.FleetPlacementService }

func (f fleetPlacementFacade) FleetPlacement() service.FleetPlacementService { return f.svc }

// A remote session is agent work, with or without a recorded host; a local
// session ran on the control plane and no spec governs it.
func TestFleetPlacementSessionRole(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		record *orchestration.SessionRecord
		want   string
	}{
		{"remote hosted", &orchestration.SessionRecord{HostAffinity: "h1"}, "agent"},
		{"local", &orchestration.SessionRecord{BackendLocal: true}, ""},
		{"local with a host", &orchestration.SessionRecord{BackendLocal: true, HostAffinity: "h1"}, ""},
		{"remote hostless", &orchestration.SessionRecord{}, "agent"},
	} {
		if got := fleetPlacementSessionRole(tc.record); got != tc.want {
			t.Fatalf("role of a %s session = %q, want %q", tc.name, got, tc.want)
		}
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

// wantPlacementPrecondition checks err is a precondition failure wrapping a
// *T and returns it.
func wantPlacementPrecondition[T error](t *testing.T, what string, err error) T {
	t.Helper()
	var qb *errors.QBError
	if !stderrors.As(err, &qb) || qb.Code != errors.ErrPreconditionFailed {
		t.Fatalf("%s = %v, want code %v", what, err, errors.ErrPreconditionFailed)
	}
	var target T
	if !stderrors.As(err, &target) {
		t.Fatalf("%s = %v, want a %T", what, err, target)
	}
	return target
}

// The bound reporter refuses a role the session store does not record, and
// a remote session with no recorded host; a local hostless session is not
// agent work and is left out of the report.
func TestBindFleetPlacement_RefusesUnobservedRoleAndHostlessRemoteWork(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bind := func(t *testing.T, records ...*orchestration.SessionRecord) service.FleetPlacementService {
		t.Helper()
		registry := orchestration.NewInMemoryFleetRegistry()
		if err := registry.RegisterFleetNode(ctx, &orchestration.FleetNode{
			ID: "n1", TenantID: "acme", Name: "n1", Host: "h1", NodeType: "worker", Status: orchestration.FleetNodeStatusRunning,
		}); err != nil {
			t.Fatalf("RegisterFleetNode: %v", err)
		}
		sessions := orchestration.NewInMemoryAgentStore()
		for _, rec := range records {
			if err := sessions.PutSession(ctx, rec); err != nil {
				t.Fatalf("PutSession: %v", err)
			}
		}
		svc := service.NewFleetPlacementService(nil, nil)
		if err := bindFleetPlacement(fleetPlacementFacade{svc}, orchestration.NewMemoryTaskKV(), registry, sessions); err != nil {
			t.Fatalf("bindFleetPlacement: %v", err)
		}
		return svc
	}
	acme := tenantctx.WithTenant(ctx, "acme")
	hosted := &orchestration.SessionRecord{SessionID: "s1", TenantID: "acme", Backend: "bot", HostAffinity: "h1", Status: orchestration.SessionStateActive}

	t.Run("unobserved role", func(t *testing.T) {
		svc := bind(t, hosted)
		if _, err := svc.DeclarePlacementSpec(acme, "", &orchestration.PlacementSpec{Name: "reviews", Role: "review", Hosts: []string{"h1"}}); err != nil {
			t.Fatalf("DeclarePlacementSpec: %v", err)
		}
		_, err := svc.GetPlacementDrift(acme, "", "reviews")
		unobserved := wantPlacementPrecondition[*orchestration.PlacementRoleUnobservedError](t, "review drift", err)
		if unobserved.Role != "review" {
			t.Fatalf("unobserved role = %q, want review", unobserved.Role)
		}
	})

	t.Run("hostless remote session", func(t *testing.T) {
		hostless := &orchestration.SessionRecord{SessionID: "s2", TenantID: "acme", Backend: "bot", Status: orchestration.SessionStateActive}
		svc := bind(t, hosted, hostless)
		if _, err := svc.DeclarePlacementSpec(acme, "", &orchestration.PlacementSpec{Name: "agents", Role: "agent", Hosts: []string{"h1"}}); err != nil {
			t.Fatalf("DeclarePlacementSpec: %v", err)
		}
		_, err := svc.GetPlacementDrift(acme, "", "agents")
		unnameable := wantPlacementPrecondition[*orchestration.PlacementWorkUnnameableError](t, "agent drift", err)
		if unnameable.Field != "host" || unnameable.WorkID != "s2" {
			t.Fatalf("unnameable = %+v, want s2 refused for its host", unnameable)
		}
	})

	t.Run("hostless local session", func(t *testing.T) {
		local := &orchestration.SessionRecord{SessionID: "s2", TenantID: "acme", Backend: "bot", BackendLocal: true, Status: orchestration.SessionStateActive}
		svc := bind(t, hosted, local)
		if _, err := svc.DeclarePlacementSpec(acme, "", &orchestration.PlacementSpec{Name: "agents", Role: "agent", Hosts: []string{"h1"}}); err != nil {
			t.Fatalf("DeclarePlacementSpec: %v", err)
		}
		report, err := svc.GetPlacementDrift(acme, "", "agents")
		if err != nil {
			t.Fatalf("GetPlacementDrift: %v", err)
		}
		if report.ObservedWork != 1 || len(report.Findings) != 0 {
			t.Fatalf("report = %+v, want only the hosted session observed and no findings", report)
		}
	})
}
