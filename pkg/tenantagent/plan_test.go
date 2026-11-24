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

package tenantagent_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantagent"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// planFixture is a fixture with a Planner over in-memory plan and project
// stores.
type planFixture struct {
	*fixture
	planner  *tenantagent.Planner
	plans    orchestration.TenantPlanStore
	projects orchestration.ProjectStore
}

func newPlanFixture(t *testing.T, tenants ...string) *planFixture {
	t.Helper()
	f := &planFixture{
		fixture:  newFixture(t, tenants...),
		plans:    orchestration.NewInMemoryTenantPlanStore(),
		projects: orchestration.NewInMemoryProjectStore(),
	}
	f.planner = f.newPlanner(t, f.plans)
	return f
}

func (f *planFixture) newPlanner(t *testing.T, plans orchestration.TenantPlanStore) *tenantagent.Planner {
	t.Helper()
	planner, err := tenantagent.NewPlanner(tenantagent.PlannerOptions{Agent: f.svc, Plans: plans, Projects: f.projects})
	if err != nil {
		t.Fatalf("NewPlanner() error = %v", err)
	}
	return planner
}

func (f *planFixture) appoint(t *testing.T, tenant, commonName string) {
	t.Helper()
	req := tenantagent.AppointRequest{Agent: orchestration.AgentRefTo("lead-1"), Certificate: newCert(t, commonName)}
	if _, err := f.svc.Appoint(context.Background(), tenant, req); err != nil {
		t.Fatalf("Appoint(%s) error = %v", tenant, err)
	}
}

// addProject creates a minimal valid project id in tenant.
func addProject(t *testing.T, store orchestration.ProjectStore, tenant, id string) {
	t.Helper()
	project := &orchestration.Project{
		ID:        id,
		TenantID:  tenant,
		Name:      id,
		Workspace: orchestration.ProjectWorkspace{FleetNodeID: "agent-1", Path: "/workspace/" + id},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.Create(context.Background(), project); err != nil {
		t.Fatalf("Create project %s/%s error = %v", tenant, id, err)
	}
}

func planRequest(expected uint64, projects ...string) tenantagent.PlanRequest {
	req := tenantagent.PlanRequest{ExpectedRevision: expected}
	for i, id := range projects {
		req.Priorities = append(req.Priorities, orchestration.TenantPriority{ProjectID: id, Rank: i + 1})
	}
	req.Milestones = []orchestration.TenantMilestone{{
		ID:         "m-1",
		Title:      "first release",
		Due:        time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		ProjectIDs: projects,
	}}
	return req
}

// tamperedPlans returns every plan it reads after edit changes it.
type tamperedPlans struct {
	orchestration.TenantPlanStore
	edit func(*orchestration.TenantPlan)
}

func (s tamperedPlans) GetTenantPlan(ctx context.Context, tenantID string) (*orchestration.TenantPlan, error) {
	plan, err := s.TenantPlanStore.GetTenantPlan(ctx, tenantID)
	if err == nil {
		s.edit(plan)
	}
	return plan, err
}

func TestSetPlan_ForAutomateTheThings(t *testing.T) {
	t.Parallel()

	const tenant = "automatethethings"
	const cn = "tenant-agent-automatethethings"
	f := newPlanFixture(t, tenant)
	f.appoint(t, tenant, cn)
	addProject(t, f.projects, tenant, "go-quicbaas")
	addProject(t, f.projects, tenant, "go-qrdb")
	ctx := context.Background()

	req := planRequest(0, "go-quicbaas", "go-qrdb")
	req.Priorities[0].Note = "the tenant agent ships first"
	plan, err := f.planner.SetPlan(ctx, tenant, req)
	if err != nil {
		t.Fatalf("SetPlan() error = %v", err)
	}
	key := tenantagent.SigningKeyFor(tenant)
	if plan.TenantID != tenant || plan.Revision != 1 || plan.Principal != cn || plan.SigningKeyID != key || len(plan.Signature) == 0 {
		t.Fatalf("plan = %+v", plan)
	}
	if !reflect.DeepEqual(plan.Priorities, req.Priorities) || !reflect.DeepEqual(plan.Milestones, req.Milestones) {
		t.Fatalf("plan content = %+v / %+v, want %+v / %+v", plan.Priorities, plan.Milestones, req.Priorities, req.Milestones)
	}
	got, err := f.planner.GetPlan(ctx, tenant)
	if err != nil {
		t.Fatalf("GetPlan() error = %v", err)
	}
	if !reflect.DeepEqual(got, plan) {
		t.Fatalf("GetPlan() = %+v, want %+v", got, plan)
	}
	signed, verified := f.keys.seen()
	if len(signed) != 1 || signed[0] != key {
		t.Fatalf("signed with %v, want once with %q", signed, key)
	}
	if len(verified) == 0 {
		t.Fatal("no read verified the plan")
	}
	for _, id := range verified {
		if id != key {
			t.Fatalf("verified with %v, want only %q", verified, key)
		}
	}
}

func TestSetPlan_RequiresAnAppointment(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a")
	addProject(t, f.projects, "tenant-a", "go-quicbaas")
	ctx := context.Background()

	if _, err := f.planner.SetPlan(ctx, "tenant-a", planRequest(0, "go-quicbaas")); !errors.Is(err, tenantagent.ErrNotAppointed) {
		t.Fatalf("SetPlan() error = %v, want ErrNotAppointed", err)
	}
	if _, err := f.plans.GetTenantPlan(ctx, "tenant-a"); !errors.Is(err, orchestration.ErrTenantPlanUnknown) {
		t.Fatalf("a refused SetPlan stored a plan: %v", err)
	}
	if signed, _ := f.keys.seen(); len(signed) != 0 {
		t.Fatalf("signed %v without an appointment", signed)
	}
}

func TestSetPlan_RefusesAProjectOfAnotherTenant(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a", "tenant-b")
	f.appoint(t, "tenant-a", "tenant-agent-a")
	addProject(t, f.projects, "tenant-a", "go-quicbaas")
	addProject(t, f.projects, "tenant-b", "go-xkms")
	ctx := context.Background()

	inPriority := planRequest(0, "go-quicbaas", "go-xkms")
	inPriority.Milestones[0].ProjectIDs = []string{"go-quicbaas"}
	inMilestone := planRequest(0, "go-quicbaas")
	inMilestone.Milestones[0].ProjectIDs = []string{"go-quicbaas", "go-xkms"}
	for name, req := range map[string]tenantagent.PlanRequest{"priority": inPriority, "milestone": inMilestone} {
		if _, err := f.planner.SetPlan(ctx, "tenant-a", req); !errors.Is(err, tenantagent.ErrUnknownProject) {
			t.Errorf("%s: SetPlan() error = %v, want ErrUnknownProject", name, err)
		}
	}
	if _, err := f.plans.GetTenantPlan(ctx, "tenant-a"); !errors.Is(err, orchestration.ErrTenantPlanUnknown) {
		t.Fatalf("a refused SetPlan stored a plan: %v", err)
	}
	if signed, _ := f.keys.seen(); len(signed) != 0 {
		t.Fatalf("signed %v for a plan naming another tenant's project", signed)
	}
}

func TestSetPlan_RefusesBadInput(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a")
	f.appoint(t, "tenant-a", "tenant-agent-a")
	addProject(t, f.projects, "tenant-a", "go-quicbaas")
	addProject(t, f.projects, "tenant-a", "go-qrdb")
	ctx := context.Background()

	if _, err := f.planner.SetPlan(ctx, " ", planRequest(0, "go-quicbaas")); !errors.Is(err, tenantagent.ErrTenantRequired) {
		t.Fatalf("SetPlan(no tenant) error = %v, want ErrTenantRequired", err)
	}
	dup := planRequest(0, "go-quicbaas", "go-qrdb")
	dup.Priorities[1].Rank = 1
	var invalid *orchestration.InputValidationError
	if _, err := f.planner.SetPlan(ctx, "tenant-a", dup); !errors.As(err, &invalid) {
		t.Fatalf("SetPlan(duplicate rank) error = %v, want *InputValidationError", err)
	}
	if signed, _ := f.keys.seen(); len(signed) != 0 {
		t.Fatalf("signed %v for bad input", signed)
	}
}

func TestSetPlan_Revisions(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a")
	f.appoint(t, "tenant-a", "tenant-agent-a")
	addProject(t, f.projects, "tenant-a", "go-quicbaas")
	ctx := context.Background()

	if _, err := f.planner.SetPlan(ctx, "tenant-a", planRequest(0, "go-quicbaas")); err != nil {
		t.Fatalf("SetPlan(expected 0) error = %v", err)
	}
	second, err := f.planner.SetPlan(ctx, "tenant-a", planRequest(1))
	if err != nil {
		t.Fatalf("SetPlan(expected 1) error = %v", err)
	}
	if second.Revision != 2 || len(second.Priorities) != 0 {
		t.Fatalf("second plan = %+v, want revision 2 and no priorities", second)
	}
	if _, err := f.planner.SetPlan(ctx, "tenant-a", planRequest(0, "go-quicbaas")); !errors.Is(err, orchestration.ErrTenantPlanConflict) {
		t.Fatalf("SetPlan(stale expected 0) error = %v, want ErrTenantPlanConflict", err)
	}

	const writers = 8
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.planner.SetPlan(ctx, "tenant-a", planRequest(2, "go-quicbaas"))
		}()
	}
	wg.Wait()
	wins, conflicts := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, orchestration.ErrTenantPlanConflict):
			conflicts++
		default:
			t.Fatalf("concurrent SetPlan() error = %v", err)
		}
	}
	if wins != 1 || conflicts != writers-1 {
		t.Fatalf("wins = %d, conflicts = %d; want 1 and %d", wins, conflicts, writers-1)
	}
	got, err := f.planner.GetPlan(ctx, "tenant-a")
	if err != nil || got.Revision != 3 {
		t.Fatalf("GetPlan() = %+v, %v; want revision 3", got, err)
	}
}

func TestGetPlan_RefusesATamperedPlan(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a", "tenant-b")
	f.appoint(t, "tenant-a", "tenant-agent-a")
	addProject(t, f.projects, "tenant-a", "go-quicbaas")
	addProject(t, f.projects, "tenant-a", "go-qrdb")
	ctx := context.Background()
	if _, err := f.planner.SetPlan(ctx, "tenant-a", planRequest(0, "go-quicbaas", "go-qrdb")); err != nil {
		t.Fatalf("SetPlan() error = %v", err)
	}

	reranked := f.newPlanner(t, tamperedPlans{TenantPlanStore: f.plans, edit: func(p *orchestration.TenantPlan) {
		p.Priorities[1].Rank = 3
	}})
	if plan, err := reranked.GetPlan(ctx, "tenant-a"); !errors.Is(err, tenantagent.ErrBadSignature) || plan != nil {
		t.Fatalf("GetPlan(changed rank) = %v, %v; want ErrBadSignature and no plan", plan, err)
	}
	rekeyed := f.newPlanner(t, tamperedPlans{TenantPlanStore: f.plans, edit: func(p *orchestration.TenantPlan) {
		p.SigningKeyID = tenantagent.SigningKeyFor("tenant-b")
	}})
	if plan, err := rekeyed.GetPlan(ctx, "tenant-a"); !errors.Is(err, tenantagent.ErrForeignKey) || plan != nil {
		t.Fatalf("GetPlan(foreign key) = %v, %v; want ErrForeignKey and no plan", plan, err)
	}
}

func TestPlansAreIsolated(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a", "tenant-b")
	ctx := context.Background()
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		f.appoint(t, tenant, "tenant-agent-"+tenant)
		addProject(t, f.projects, tenant, "project-"+tenant)
		if _, err := f.planner.SetPlan(ctx, tenant, planRequest(0, "project-"+tenant)); err != nil {
			t.Fatalf("SetPlan(%s) error = %v", tenant, err)
		}
	}
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		got, err := f.planner.GetPlan(ctx, tenant)
		if err != nil || got.TenantID != tenant || got.Priorities[0].ProjectID != "project-"+tenant ||
			got.Principal != "tenant-agent-"+tenant {
			t.Fatalf("GetPlan(%s) = %+v, %v", tenant, got, err)
		}
	}

	scoped := map[string]context.Context{
		"orchestration": orchestration.ContextWithTenant(ctx, "tenant-b"),
		"tenantctx":     tenantctx.WithTenant(ctx, "tenant-b"),
	}
	for name, bctx := range scoped {
		if _, err := f.planner.SetPlan(bctx, "tenant-a", planRequest(1, "project-tenant-a")); !errors.Is(err, tenantagent.ErrWrongTenant) {
			t.Errorf("%s: SetPlan() error = %v, want ErrWrongTenant", name, err)
		}
		if _, err := f.planner.GetPlan(bctx, "tenant-a"); !errors.Is(err, tenantagent.ErrWrongTenant) {
			t.Errorf("%s: GetPlan() error = %v, want ErrWrongTenant", name, err)
		}
		if err := f.planner.ClearPlan(bctx, "tenant-a"); !errors.Is(err, tenantagent.ErrWrongTenant) {
			t.Errorf("%s: ClearPlan() error = %v, want ErrWrongTenant", name, err)
		}
	}

	// tenant-a's plan, replayed into tenant-b, does not verify there.
	planA, err := f.plans.GetTenantPlan(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("GetTenantPlan(tenant-a) error = %v", err)
	}
	payload, err := planA.SigningPayload()
	if err != nil {
		t.Fatalf("SigningPayload() error = %v", err)
	}
	if err := f.svc.Verify(ctx, "tenant-a", planA.SigningKeyID, payload, planA.Signature); err != nil {
		t.Fatalf("Verify(tenant-a) error = %v", err)
	}
	if err := f.svc.Verify(ctx, "tenant-b", planA.SigningKeyID, payload, planA.Signature); !errors.Is(err, tenantagent.ErrForeignKey) {
		t.Fatalf("Verify(tenant-b) error = %v, want ErrForeignKey", err)
	}
	replayed := f.newPlanner(t, tamperedPlans{TenantPlanStore: f.plans, edit: func(p *orchestration.TenantPlan) {
		*p = *planA
		p.TenantID = "tenant-b"
	}})
	if _, err := replayed.GetPlan(ctx, "tenant-b"); !errors.Is(err, tenantagent.ErrForeignKey) {
		t.Fatalf("GetPlan(replayed into tenant-b) error = %v, want ErrForeignKey", err)
	}
}

func TestClearPlan(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a", "tenant-b")
	f.appoint(t, "tenant-a", "tenant-agent-a")
	addProject(t, f.projects, "tenant-a", "go-quicbaas")
	ctx := context.Background()
	if _, err := f.planner.SetPlan(ctx, "tenant-a", planRequest(0, "go-quicbaas")); err != nil {
		t.Fatalf("SetPlan() error = %v", err)
	}

	if err := f.planner.ClearPlan(ctx, "tenant-a"); err != nil {
		t.Fatalf("ClearPlan() error = %v", err)
	}
	if _, err := f.planner.GetPlan(ctx, "tenant-a"); !errors.Is(err, orchestration.ErrTenantPlanUnknown) {
		t.Fatalf("GetPlan() after clear error = %v, want ErrTenantPlanUnknown", err)
	}
	if err := f.planner.ClearPlan(ctx, "tenant-a"); err != nil {
		t.Fatalf("second ClearPlan() error = %v, want nil", err)
	}
	if err := f.planner.ClearPlan(ctx, "tenant-b"); !errors.Is(err, tenantagent.ErrNotAppointed) {
		t.Fatalf("ClearPlan(no appointment) error = %v, want ErrNotAppointed", err)
	}
}

func TestRetire_KeepsThePlan(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a")
	f.appoint(t, "tenant-a", "tenant-agent-a")
	addProject(t, f.projects, "tenant-a", "go-quicbaas")
	ctx := context.Background()
	set, err := f.planner.SetPlan(ctx, "tenant-a", planRequest(0, "go-quicbaas"))
	if err != nil {
		t.Fatalf("SetPlan() error = %v", err)
	}

	if err := f.svc.Retire(ctx, "tenant-a"); err != nil {
		t.Fatalf("Retire() error = %v", err)
	}
	got, err := f.planner.GetPlan(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("GetPlan() after retire error = %v", err)
	}
	if !reflect.DeepEqual(got, set) {
		t.Fatalf("GetPlan() after retire = %+v, want %+v", got, set)
	}
	if _, err := f.planner.SetPlan(ctx, "tenant-a", planRequest(1, "go-quicbaas")); !errors.Is(err, tenantagent.ErrNotAppointed) {
		t.Fatalf("SetPlan() after retire error = %v, want ErrNotAppointed", err)
	}
}

func TestNewPlanner_RequiresEveryDependency(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "tenant-a")
	full := tenantagent.PlannerOptions{
		Agent:    f.svc,
		Plans:    orchestration.NewInMemoryTenantPlanStore(),
		Projects: orchestration.NewInMemoryProjectStore(),
	}
	if _, err := tenantagent.NewPlanner(full); err != nil {
		t.Fatalf("NewPlanner(full) error = %v", err)
	}
	cases := map[string]func(o *tenantagent.PlannerOptions){
		"agent":    func(o *tenantagent.PlannerOptions) { o.Agent = nil },
		"plans":    func(o *tenantagent.PlannerOptions) { o.Plans = nil },
		"projects": func(o *tenantagent.PlannerOptions) { o.Projects = nil },
	}
	for name, drop := range cases {
		opts := full
		drop(&opts)
		if _, err := tenantagent.NewPlanner(opts); !errors.Is(err, tenantagent.ErrNotConfigured) {
			t.Errorf("NewPlanner(no %s) error = %v, want ErrNotConfigured", name, err)
		}
	}
}
