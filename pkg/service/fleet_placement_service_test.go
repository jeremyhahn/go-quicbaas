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

package service

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// newBoundFleetPlacement builds a fleet placement service over in-memory
// stores: acme has hosts h1 (gpu) and h2, and one live review session on h2.
func newBoundFleetPlacement(t *testing.T) (FleetPlacementService, orchestration.AgentStore) {
	t.Helper()
	ctx := context.Background()
	registry := orchestration.NewInMemoryFleetRegistry()
	for _, node := range []*orchestration.FleetNode{
		{ID: "n1", TenantID: "acme", Name: "n1", Host: "h1", NodeType: "worker", Status: orchestration.FleetNodeStatusRunning, Capabilities: []string{"gpu"}},
		{ID: "n2", TenantID: "acme", Name: "n2", Host: "h2", NodeType: "worker", Status: orchestration.FleetNodeStatusRunning},
	} {
		if err := registry.RegisterFleetNode(ctx, node); err != nil {
			t.Fatalf("RegisterFleetNode(%s): %v", node.ID, err)
		}
	}
	sessions := orchestration.NewInMemoryAgentStore()
	if err := sessions.PutSession(ctx, &orchestration.SessionRecord{
		SessionID: "s1", TenantID: "acme", Backend: "review-bot", HostAffinity: "h2", Status: orchestration.SessionStateActive,
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
	work, err := orchestration.NewSessionWorkSource(sessions, func(*orchestration.SessionRecord) string { return "review" })
	if err != nil {
		t.Fatalf("NewSessionWorkSource: %v", err)
	}
	drift, err := orchestration.NewDriftReporter(specs, inventory, work)
	if err != nil {
		t.Fatalf("NewDriftReporter: %v", err)
	}
	return NewFleetPlacementService(specs, drift), sessions
}

func wantFleetPlacementCode(t *testing.T, what string, err error, code errors.ErrorCode) {
	t.Helper()
	var qb *errors.QBError
	if !stderrors.As(err, &qb) || qb.Code != code {
		t.Fatalf("%s = %v, want code %v", what, err, code)
	}
}

// The verbs round-trip a spec within the caller's tenant, and the drift
// report reads the same spec against the fleet and the live work.
func TestFleetPlacementService_DeclareGetListDriftWithdraw(t *testing.T) {
	t.Parallel()
	svc, _ := newBoundFleetPlacement(t)
	acme := tenantctx.WithTenant(context.Background(), "acme")

	stored, err := svc.DeclarePlacementSpec(acme, "", &orchestration.PlacementSpec{Name: "reviews", Role: "review", Hosts: []string{"h1"}})
	if err != nil {
		t.Fatalf("DeclarePlacementSpec: %v", err)
	}
	if stored.TenantID != "acme" || stored.Generation != 1 {
		t.Fatalf("stored = %+v, want tenant acme at generation 1", stored)
	}
	got, err := svc.GetPlacementSpec(acme, "acme", "reviews")
	if err != nil || got.Role != "review" {
		t.Fatalf("GetPlacementSpec = %+v, %v", got, err)
	}
	list, err := svc.ListPlacementSpecs(acme, "")
	if err != nil || len(list) != 1 || list[0].Name != "reviews" {
		t.Fatalf("ListPlacementSpecs = %+v, %v", list, err)
	}

	report, err := svc.GetPlacementDrift(acme, "", "reviews")
	if err != nil {
		t.Fatalf("GetPlacementDrift: %v", err)
	}
	if report.ObservedWork != 1 || len(report.Findings) != 1 {
		t.Fatalf("report = %+v, want one finding over one session", report)
	}
	if f := report.Findings[0]; f.Kind != orchestration.DriftHostNotAdmitted || f.WorkID != "s1" || f.Host != "h2" {
		t.Fatalf("finding = %+v, want s1 on h2 not admitted", f)
	}

	if err := svc.WithdrawPlacementSpec(acme, "", "reviews"); err != nil {
		t.Fatalf("WithdrawPlacementSpec: %v", err)
	}
	list, err = svc.ListPlacementSpecs(acme, "")
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("ListPlacementSpecs after withdraw = %+v, %v; want an empty list", list, err)
	}
}

// A caller may not name another tenant, and a context with no tenant is
// refused, for every verb.
func TestFleetPlacementService_RefusesCrossTenantAndNoTenant(t *testing.T) {
	t.Parallel()
	svc, _ := newBoundFleetPlacement(t)
	acme := tenantctx.WithTenant(context.Background(), "acme")
	spec := &orchestration.PlacementSpec{Name: "reviews", Role: "review", Hosts: []string{"h1"}}

	verbs := map[string]func(ctx context.Context, tenant string) error{
		"declare": func(ctx context.Context, tenant string) error {
			_, err := svc.DeclarePlacementSpec(ctx, tenant, spec)
			return err
		},
		"get": func(ctx context.Context, tenant string) error {
			_, err := svc.GetPlacementSpec(ctx, tenant, "reviews")
			return err
		},
		"list": func(ctx context.Context, tenant string) error {
			_, err := svc.ListPlacementSpecs(ctx, tenant)
			return err
		},
		"withdraw": func(ctx context.Context, tenant string) error {
			return svc.WithdrawPlacementSpec(ctx, tenant, "reviews")
		},
		"drift": func(ctx context.Context, tenant string) error {
			_, err := svc.GetPlacementDrift(ctx, tenant, "reviews")
			return err
		},
	}
	for name, call := range verbs {
		err := call(acme, "other")
		var cross *tenantctx.CrossTenantError
		if !stderrors.As(err, &cross) {
			t.Errorf("%s naming another tenant = %v, want *tenantctx.CrossTenantError", name, err)
		}
		wantFleetPlacementCode(t, name+" cross-tenant", err, errors.ErrForbidden)

		err = call(context.Background(), "")
		var missing *tenantctx.MissingTenantError
		if !stderrors.As(err, &missing) {
			t.Errorf("%s with no tenant = %v, want *tenantctx.MissingTenantError", name, err)
		}
	}
	// Nothing was written by the refused declare.
	if list, err := svc.ListPlacementSpecs(acme, ""); err != nil || len(list) != 0 {
		t.Fatalf("ListPlacementSpecs = %+v, %v; want none", list, err)
	}
}

// Store and reporter failures reach the transports as coded errors.
func TestFleetPlacementService_ClassifiesFailures(t *testing.T) {
	t.Parallel()
	svc, sessions := newBoundFleetPlacement(t)
	acme := tenantctx.WithTenant(context.Background(), "acme")

	_, err := svc.GetPlacementSpec(acme, "", "absent")
	wantFleetPlacementCode(t, "get absent", err, errors.ErrNotFound)
	err = svc.WithdrawPlacementSpec(acme, "", "absent")
	wantFleetPlacementCode(t, "withdraw absent", err, errors.ErrNotFound)
	_, err = svc.GetPlacementDrift(acme, "", "absent")
	wantFleetPlacementCode(t, "drift absent", err, errors.ErrNotFound)

	_, err = svc.DeclarePlacementSpec(acme, "", &orchestration.PlacementSpec{Name: "both", Role: "review", Hosts: []string{"h1"}, HostCount: 1})
	wantFleetPlacementCode(t, "declare invalid", err, errors.ErrInvalidInput)
	_, err = svc.DeclarePlacementSpec(acme, "", nil)
	wantFleetPlacementCode(t, "declare nil", err, errors.ErrInvalidInput)

	spec := &orchestration.PlacementSpec{Name: "reviews", Role: "review", Hosts: []string{"h1"}}
	if _, err := svc.DeclarePlacementSpec(acme, "", spec); err != nil {
		t.Fatalf("DeclarePlacementSpec: %v", err)
	}
	_, err = svc.DeclarePlacementSpec(acme, "", &orchestration.PlacementSpec{Name: "reviews", Role: "review", Hosts: []string{"h2"}})
	wantFleetPlacementCode(t, "declare over an existing spec", err, errors.ErrConflict)
	var stale *orchestration.PlacementSpecStaleError
	if !stderrors.As(err, &stale) {
		t.Fatalf("declare over an existing spec = %v, want *PlacementSpecStaleError", err)
	}

	// A live session with no recorded host cannot be named, so the report
	// refuses rather than answer without it.
	if err := sessions.PutSession(context.Background(), &orchestration.SessionRecord{
		SessionID: "s2", TenantID: "acme", Backend: "review-bot", Status: orchestration.SessionStateActive,
	}); err != nil {
		t.Fatalf("PutSession: %v", err)
	}
	_, err = svc.GetPlacementDrift(acme, "", "reviews")
	wantFleetPlacementCode(t, "drift over unnameable work", err, errors.ErrPreconditionFailed)
}

// A service the server has not bound answers unavailable, never an empty
// list that would read as "no specs".
func TestFleetPlacementService_UnboundIsUnavailable(t *testing.T) {
	t.Parallel()
	svc := NewFleetPlacementService(nil, nil)
	acme := tenantctx.WithTenant(context.Background(), "acme")

	_, err := svc.ListPlacementSpecs(acme, "")
	wantFleetPlacementCode(t, "list unbound", err, errors.ErrUnavailable)
	if !stderrors.Is(err, ErrFleetPlacementUnbound) {
		t.Fatalf("list unbound = %v, want ErrFleetPlacementUnbound", err)
	}
	_, err = svc.GetPlacementDrift(acme, "", "x")
	wantFleetPlacementCode(t, "drift unbound", err, errors.ErrUnavailable)

	provider, ok := any(&quicBaaS{fleetPlacementService: svc}).(FleetPlacementProvider)
	if !ok || provider.FleetPlacement() != svc {
		t.Fatal("quicBaaS does not provide its fleet placement service")
	}
}
