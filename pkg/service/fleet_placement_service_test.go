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
	"sort"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
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

	for _, tc := range []struct {
		name string
		err  error
		want errors.ErrorCode
	}{
		{"consent invalid", &orchestration.ConsentInvalidError{Field: "state", Reason: "is not an answer"}, errors.ErrInvalidInput},
		{"consent not found", &orchestration.ConsentNotFoundError{TenantID: "acme", RequestID: "r"}, errors.ErrNotFound},
		{"consent not pending", &orchestration.ConsentNotPendingError{TenantID: "acme", RequestID: "r", State: orchestration.ConsentGranted}, errors.ErrConflict},
		{"consent stale", &orchestration.ConsentStaleError{TenantID: "acme", RequestID: "r"}, errors.ErrConflict},
		{"consent incoherent", &orchestration.ConsentIncoherentError{TenantID: "acme", RequestID: "r"}, errors.ErrPreconditionFailed},
		{"role unobserved", &orchestration.PlacementRoleUnobservedError{Role: "review"}, errors.ErrPreconditionFailed},
	} {
		wantFleetPlacementCode(t, tc.name, classifyFleetPlacementError("AnswerConsent", tc.err), tc.want)
	}
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

// newConsentFleetPlacement returns a bound fleet placement service whose
// consent ledger is bound too, and the ledger itself, so a test raises
// requests the way the actuator will.
func newConsentFleetPlacement(t *testing.T) (FleetPlacementService, orchestration.ConsentLedger) {
	t.Helper()
	svc, _ := newBoundFleetPlacement(t)
	ledger, err := orchestration.NewConsentLedger(orchestration.NewMemoryTaskKV())
	if err != nil {
		t.Fatalf("NewConsentLedger: %v", err)
	}
	binder, ok := svc.(FleetPlacementConsentBinder)
	if !ok {
		t.Fatal("the fleet placement service does not bind a consent ledger")
	}
	binder.BindFleetPlacementConsent(ledger)
	return svc, ledger
}

func raiseFleetPlacementConsent(t *testing.T, ledger orchestration.ConsentLedger, workID string) *orchestration.PlacementConsentRequest {
	t.Helper()
	ctx := orchestration.ContextWithTenant(context.Background(), "acme")
	req, err := ledger.Raise(ctx, "acme", orchestration.ConsentMove{
		SpecName: "reviews", SpecGeneration: 1, Class: "review", WorkID: workID, SourceHost: "h1",
	})
	if err != nil {
		t.Fatalf("Raise(%s): %v", workID, err)
	}
	return req
}

func fleetPlacementConsentSubject(id string, authenticated bool) context.Context {
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	return auth.SubjectIntoContext(ctx, interfaces.Subject{ID: id, TenantID: "acme", Authenticated: authenticated})
}

func TestFleetPlacementService_ConsentListAndAnswer(t *testing.T) {
	t.Parallel()
	svc, ledger := newConsentFleetPlacement(t)
	first := raiseFleetPlacementConsent(t, ledger, "s1")
	second := raiseFleetPlacementConsent(t, ledger, "s2")
	ids := []string{first.ID, second.ID}
	sort.Strings(ids)
	alice := fleetPlacementConsentSubject("alice", true)

	pending, err := svc.ListConsentRequests(alice, "", orchestration.ConsentPending)
	if err != nil {
		t.Fatalf("ListConsentRequests(pending): %v", err)
	}
	if len(pending) != 2 || pending[0].ID != ids[0] || pending[1].ID != ids[1] {
		t.Fatalf("pending = %v, want %v in id order", pending, ids)
	}
	for _, req := range pending {
		if req.State != orchestration.ConsentPending {
			t.Fatalf("request %s state = %s, want pending", req.ID, req.State)
		}
	}

	answered, err := svc.AnswerConsent(alice, "acme", first.ID, orchestration.ConsentAnswer{
		State: orchestration.ConsentGranted, By: "mallory", Reason: "maintenance window",
	})
	if err != nil {
		t.Fatalf("AnswerConsent: %v", err)
	}
	if answered.State != orchestration.ConsentGranted || answered.By != "alice" || answered.Reason != "maintenance window" {
		t.Fatalf("answered = %+v, want granted by alice", answered)
	}
	stored, err := ledger.Get(orchestration.ContextWithTenant(context.Background(), "acme"), "acme", first.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.By != "alice" {
		t.Fatalf("stored By = %q, want alice (never the body's)", stored.By)
	}

	granted, err := svc.ListConsentRequests(alice, "", orchestration.ConsentGranted)
	if err != nil || len(granted) != 1 || granted[0].ID != first.ID {
		t.Fatalf("granted = %v, %v; want only %s", granted, err, first.ID)
	}
	pending, err = svc.ListConsentRequests(alice, "", orchestration.ConsentPending)
	if err != nil || len(pending) != 1 || pending[0].ID != second.ID {
		t.Fatalf("pending = %v, %v; want only %s", pending, err, second.ID)
	}
	all, err := svc.ListConsentRequests(alice, "", "")
	if err != nil || len(all) != 2 {
		t.Fatalf("all = %v, %v; want both", all, err)
	}
	refused, err := svc.ListConsentRequests(alice, "", orchestration.ConsentRefused)
	if err != nil || refused == nil || len(refused) != 0 {
		t.Fatalf("refused = %#v, %v; want an empty, non-nil slice", refused, err)
	}
}

func TestFleetPlacementService_ConsentRefuses(t *testing.T) {
	t.Parallel()
	svc, ledger := newConsentFleetPlacement(t)
	req := raiseFleetPlacementConsent(t, ledger, "s1")
	grant := orchestration.ConsentAnswer{State: orchestration.ConsentGranted}
	alice := fleetPlacementConsentSubject("alice", true)

	_, err := svc.AnswerConsent(tenantctx.WithTenant(context.Background(), "acme"), "", req.ID, grant)
	wantFleetPlacementCode(t, "answer with no subject", err, errors.ErrForbidden)
	_, err = svc.AnswerConsent(fleetPlacementConsentSubject("eve", false), "", req.ID, grant)
	wantFleetPlacementCode(t, "answer by an unauthenticated subject", err, errors.ErrForbidden)
	stored, err := ledger.Get(orchestration.ContextWithTenant(context.Background(), "acme"), "acme", req.ID)
	if err != nil || stored.State != orchestration.ConsentPending {
		t.Fatalf("after refused answers = %+v, %v; want pending", stored, err)
	}

	_, err = svc.AnswerConsent(alice, "globex", req.ID, grant)
	wantFleetPlacementCode(t, "answer in another tenant", err, errors.ErrForbidden)
	_, err = svc.ListConsentRequests(alice, "globex", "")
	wantFleetPlacementCode(t, "list in another tenant", err, errors.ErrForbidden)
	_, err = svc.AnswerConsent(alice, "", "unknown", grant)
	wantFleetPlacementCode(t, "answer unknown", err, errors.ErrNotFound)
	_, err = svc.AnswerConsent(alice, "", req.ID, orchestration.ConsentAnswer{State: "maybe"})
	wantFleetPlacementCode(t, "answer maybe", err, errors.ErrInvalidInput)
	_, err = svc.ListConsentRequests(alice, "", "bogus")
	wantFleetPlacementCode(t, "list bogus", err, errors.ErrInvalidInput)

	if _, err := svc.AnswerConsent(alice, "", req.ID, grant); err != nil {
		t.Fatalf("AnswerConsent: %v", err)
	}
	_, err = svc.AnswerConsent(alice, "", req.ID, orchestration.ConsentAnswer{State: orchestration.ConsentRefused})
	wantFleetPlacementCode(t, "second answer", err, errors.ErrConflict)

	unbound, _ := newBoundFleetPlacement(t)
	_, err = unbound.ListConsentRequests(alice, "", "")
	wantFleetPlacementCode(t, "list on an unbound ledger", err, errors.ErrUnavailable)
	if !stderrors.Is(err, ErrFleetPlacementConsentUnbound) {
		t.Fatalf("list on an unbound ledger = %v, want ErrFleetPlacementConsentUnbound", err)
	}
	_, err = unbound.AnswerConsent(alice, "", req.ID, grant)
	wantFleetPlacementCode(t, "answer on an unbound ledger", err, errors.ErrUnavailable)
	if _, err := unbound.ListPlacementSpecs(alice, ""); err != nil {
		t.Fatalf("ListPlacementSpecs with no ledger bound: %v", err)
	}
}
