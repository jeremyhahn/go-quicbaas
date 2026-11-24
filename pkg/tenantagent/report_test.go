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
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantagent"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// reportClock is the fixed instant every test report is generated at.
var reportClock = time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)

// newReporter returns a Reporter over planner, the fixture's projects and
// tasks, reading the fixed reportClock.
func (f *planFixture) newReporter(t *testing.T, planner *tenantagent.Planner, tasks orchestration.TaskStore) *tenantagent.Reporter {
	t.Helper()
	reporter, err := tenantagent.NewReporter(tenantagent.ReporterOptions{
		Planner:  planner,
		Projects: f.projects,
		Tasks:    tasks,
		Now:      func() time.Time { return reportClock },
	})
	if err != nil {
		t.Fatalf("NewReporter() error = %v", err)
	}
	return reporter
}

// addTask creates a minimal pending task id in tenant, scoped to project
// (empty for none).
func addTask(t *testing.T, store orchestration.TaskStore, tenant, id, project string) {
	t.Helper()
	task := &orchestration.Task{ID: id, TenantID: tenant, ProjectID: project, Subject: "task " + id, Status: orchestration.TaskStatusPending}
	if err := store.CreateTask(orchestration.ContextWithTenant(context.Background(), tenant), task); err != nil {
		t.Fatalf("CreateTask(%s/%s) error = %v", tenant, id, err)
	}
}

func blockTask(t *testing.T, store orchestration.TaskStore, tenant, id string, kind orchestration.TaskBlockKind) {
	t.Helper()
	ctx := orchestration.ContextWithTenant(context.Background(), tenant)
	if err := store.BlockTask(ctx, tenant, id, kind, "the owner must decide"); err != nil {
		t.Fatalf("BlockTask(%s/%s) error = %v", tenant, id, err)
	}
}

func completeTask(t *testing.T, store orchestration.TaskStore, tenant, id string) {
	t.Helper()
	ctx := orchestration.ContextWithTenant(context.Background(), tenant)
	if err := store.ClaimTask(ctx, tenant, id, "agent-1"); err != nil {
		t.Fatalf("ClaimTask(%s/%s) error = %v", tenant, id, err)
	}
	if err := store.CompleteTask(ctx, tenant, id, &orchestration.TaskResult{}); err != nil {
		t.Fatalf("CompleteTask(%s/%s) error = %v", tenant, id, err)
	}
}

func milestoneStates(report *tenantagent.OwnerReport) []string {
	states := make([]string, 0, len(report.Milestones))
	for _, m := range report.Milestones {
		states = append(states, m.ID+"="+m.State)
	}
	return states
}

func TestReport_ForAutomateTheThings(t *testing.T) {
	t.Parallel()

	const tenant = "automatethethings"
	const cn = "tenant-agent-automatethethings"
	f := newPlanFixture(t, tenant)
	f.appoint(t, tenant, cn)
	for _, id := range []string{"go-quicbaas", "go-qrdb", "docs"} {
		addProject(t, f.projects, tenant, id)
	}
	ctx := context.Background()
	req := planRequest(0, "go-qrdb", "go-quicbaas")
	req.Milestones = append(req.Milestones, orchestration.TenantMilestone{
		ID: "m-0", Title: "foundation", Due: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Done: true,
	})
	if _, err := f.planner.SetPlan(ctx, tenant, req); err != nil {
		t.Fatalf("SetPlan() error = %v", err)
	}
	tasks := orchestration.NewInMemoryTaskStore()
	addTask(t, tasks, tenant, "t-1", "go-quicbaas")
	addTask(t, tasks, tenant, "t-2", "go-quicbaas")
	addTask(t, tasks, tenant, "t-3", "go-quicbaas")
	blockTask(t, tasks, tenant, "t-3", orchestration.TaskBlockKindNeedsInfo)
	addTask(t, tasks, tenant, "t-4", "go-qrdb")
	completeTask(t, tasks, tenant, "t-4")
	addTask(t, tasks, tenant, "t-5", "")

	reporter := f.newReporter(t, f.planner, tasks)
	report, err := reporter.Report(ctx, tenant)
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	want := []tenantagent.ProjectStatus{
		{ProjectID: "go-qrdb", Name: "go-qrdb", Rank: 1, Tasks: map[string]int{"completed": 1}},
		{ProjectID: "go-quicbaas", Name: "go-quicbaas", Rank: 2, Tasks: map[string]int{"pending": 2, "blocked": 1}, NeedsOwner: 1},
		{ProjectID: "docs", Name: "docs"},
	}
	if !reflect.DeepEqual(report.Projects, want) {
		t.Fatalf("Projects = %+v, want %+v", report.Projects, want)
	}
	if !reflect.DeepEqual(report.Unscoped, map[string]int{"pending": 1}) {
		t.Fatalf("Unscoped = %v, want pending 1", report.Unscoped)
	}
	if got := milestoneStates(report); !reflect.DeepEqual(got, []string{"m-1=open", "m-0=done"}) {
		t.Fatalf("milestones = %v", got)
	}
	if !reflect.DeepEqual(report.Milestones[0].ProjectIDs, []string{"go-qrdb", "go-quicbaas"}) {
		t.Fatalf("milestone projects = %v", report.Milestones[0].ProjectIDs)
	}
	key := tenantagent.SigningKeyFor(tenant)
	if report.TenantID != tenant || report.PlanRevision != 1 || !report.GeneratedAt.Equal(reportClock) ||
		report.Principal != cn || report.SigningKeyID != key || len(report.Signature) == 0 {
		t.Fatalf("report = %+v", report)
	}
	if err := reporter.VerifyReport(ctx, report); err != nil {
		t.Fatalf("VerifyReport() error = %v", err)
	}
	signed, _ := f.keys.seen()
	if len(signed) != 2 || signed[1] != key {
		t.Fatalf("signed with %v, want the plan then the report with %q", signed, key)
	}
}

func TestReport_MilestoneOverdue(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a")
	f.appoint(t, "tenant-a", "tenant-agent-a")
	addProject(t, f.projects, "tenant-a", "go-quicbaas")
	ctx := context.Background()
	req := planRequest(0, "go-quicbaas")
	req.Milestones = append(req.Milestones,
		orchestration.TenantMilestone{ID: "late", Title: "late", Due: reportClock.Add(-time.Hour)},
		orchestration.TenantMilestone{ID: "shipped", Title: "shipped", Due: reportClock.Add(-48 * time.Hour), Done: true},
	)
	if _, err := f.planner.SetPlan(ctx, "tenant-a", req); err != nil {
		t.Fatalf("SetPlan() error = %v", err)
	}
	report, err := f.newReporter(t, f.planner, orchestration.NewInMemoryTaskStore()).Report(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	if got := milestoneStates(report); !reflect.DeepEqual(got, []string{"m-1=open", "late=overdue", "shipped=done"}) {
		t.Fatalf("milestones = %v", got)
	}
}

func TestReport_WithoutAPlan(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a")
	f.appoint(t, "tenant-a", "tenant-agent-a")
	addProject(t, f.projects, "tenant-a", "project-b")
	addProject(t, f.projects, "tenant-a", "project-a")
	ctx := context.Background()
	reporter := f.newReporter(t, f.planner, orchestration.NewInMemoryTaskStore())
	report, err := reporter.Report(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	want := []tenantagent.ProjectStatus{
		{ProjectID: "project-a", Name: "project-a"},
		{ProjectID: "project-b", Name: "project-b"},
	}
	if report.PlanRevision != 0 || len(report.Milestones) != 0 || !reflect.DeepEqual(report.Projects, want) {
		t.Fatalf("report = %+v, want revision 0, no milestones and projects %+v", report, want)
	}
	if len(report.Signature) == 0 {
		t.Fatal("a report without a plan is not signed")
	}
	if err := reporter.VerifyReport(ctx, report); err != nil {
		t.Fatalf("VerifyReport() error = %v", err)
	}
}

func TestReport_MissingRankedProject(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a")
	f.appoint(t, "tenant-a", "tenant-agent-a")
	addProject(t, f.projects, "tenant-a", "go-qrdb")
	addProject(t, f.projects, "tenant-a", "go-quicbaas")
	ctx := context.Background()
	if _, err := f.planner.SetPlan(ctx, "tenant-a", planRequest(0, "go-qrdb", "go-quicbaas")); err != nil {
		t.Fatalf("SetPlan() error = %v", err)
	}
	if err := f.projects.Delete(ctx, "tenant-a", "go-qrdb"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	report, err := f.newReporter(t, f.planner, orchestration.NewInMemoryTaskStore()).Report(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	want := []tenantagent.ProjectStatus{
		{ProjectID: "go-qrdb", Rank: 1, Missing: true},
		{ProjectID: "go-quicbaas", Name: "go-quicbaas", Rank: 2},
	}
	if !reflect.DeepEqual(report.Projects, want) {
		t.Fatalf("Projects = %+v, want %+v", report.Projects, want)
	}
}

func TestReport_RequiresAnAppointment(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a")
	addProject(t, f.projects, "tenant-a", "go-quicbaas")
	ctx := context.Background()
	reporter := f.newReporter(t, f.planner, orchestration.NewInMemoryTaskStore())
	if _, err := reporter.Report(ctx, "tenant-a"); !errors.Is(err, tenantagent.ErrNotAppointed) {
		t.Fatalf("Report() error = %v, want ErrNotAppointed", err)
	}
	f.appoint(t, "tenant-a", "tenant-agent-a")
	if err := f.svc.Retire(ctx, "tenant-a"); err != nil {
		t.Fatalf("Retire() error = %v", err)
	}
	if _, err := reporter.Report(ctx, "tenant-a"); !errors.Is(err, tenantagent.ErrNotAppointed) {
		t.Fatalf("Report() after retire error = %v, want ErrNotAppointed", err)
	}
	if signed, _ := f.keys.seen(); len(signed) != 0 {
		t.Fatalf("signed %v without an appointment", signed)
	}
}

func TestReport_RefusesATamperedPlan(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a")
	f.appoint(t, "tenant-a", "tenant-agent-a")
	addProject(t, f.projects, "tenant-a", "go-quicbaas")
	addProject(t, f.projects, "tenant-a", "go-qrdb")
	ctx := context.Background()
	if _, err := f.planner.SetPlan(ctx, "tenant-a", planRequest(0, "go-quicbaas", "go-qrdb")); err != nil {
		t.Fatalf("SetPlan() error = %v", err)
	}
	before, _ := f.keys.seen()
	reranked := f.newPlanner(t, tamperedPlans{TenantPlanStore: f.plans, edit: func(p *orchestration.TenantPlan) {
		p.Priorities[1].Rank = 3
	}})
	reporter := f.newReporter(t, reranked, orchestration.NewInMemoryTaskStore())
	if report, err := reporter.Report(ctx, "tenant-a"); !errors.Is(err, tenantagent.ErrBadSignature) || report != nil {
		t.Fatalf("Report(tampered plan) = %v, %v; want ErrBadSignature and no report", report, err)
	}
	if after, _ := f.keys.seen(); len(after) != len(before) {
		t.Fatalf("signed %v over a tampered plan", after[len(before):])
	}
}

// unreadableTasks names one record it could not read in every listing.
type unreadableTasks struct {
	orchestration.TaskStore
}

func (s unreadableTasks) ListTaskRecords(ctx context.Context, tenantID string, filter *orchestration.TaskFilter) (*orchestration.TaskListing, error) {
	listing, err := s.TaskStore.ListTaskRecords(ctx, tenantID, filter)
	if err == nil {
		listing.Unreadable = append(listing.Unreadable, "t-corrupt")
	}
	return listing, err
}

func TestReport_RefusesAnIncompleteTaskListing(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a")
	f.appoint(t, "tenant-a", "tenant-agent-a")
	addProject(t, f.projects, "tenant-a", "go-quicbaas")
	tasks := orchestration.NewInMemoryTaskStore()
	addTask(t, tasks, "tenant-a", "t-1", "go-quicbaas")
	reporter := f.newReporter(t, f.planner, unreadableTasks{TaskStore: tasks})
	if report, err := reporter.Report(context.Background(), "tenant-a"); !errors.Is(err, orchestration.ErrTaskListingIncomplete) || report != nil {
		t.Fatalf("Report(incomplete listing) = %v, %v; want ErrTaskListingIncomplete and no report", report, err)
	}
	if signed, _ := f.keys.seen(); len(signed) != 0 {
		t.Fatalf("signed %v over an incomplete listing", signed)
	}
}

func TestVerifyReport_RefusesATamperedReport(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a", "tenant-b")
	f.appoint(t, "tenant-a", "tenant-agent-a")
	addProject(t, f.projects, "tenant-a", "go-quicbaas")
	ctx := context.Background()
	if _, err := f.planner.SetPlan(ctx, "tenant-a", planRequest(0, "go-quicbaas")); err != nil {
		t.Fatalf("SetPlan() error = %v", err)
	}
	tasks := orchestration.NewInMemoryTaskStore()
	addTask(t, tasks, "tenant-a", "t-1", "go-quicbaas")
	reporter := f.newReporter(t, f.planner, tasks)
	report, err := reporter.Report(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	edits := map[string]func(r *tenantagent.OwnerReport){
		"count": func(r *tenantagent.OwnerReport) {
			r.Projects = append([]tenantagent.ProjectStatus(nil), r.Projects...)
			r.Projects[0].Tasks = map[string]int{"pending": 2}
		},
		"milestone state": func(r *tenantagent.OwnerReport) {
			r.Milestones = append([]tenantagent.MilestoneStatus(nil), r.Milestones...)
			r.Milestones[0].State = tenantagent.MilestoneDone
		},
		"generated at": func(r *tenantagent.OwnerReport) { r.GeneratedAt = r.GeneratedAt.Add(time.Hour) },
	}
	for name, edit := range edits {
		tampered := *report
		edit(&tampered)
		if err := reporter.VerifyReport(ctx, &tampered); !errors.Is(err, tenantagent.ErrBadSignature) {
			t.Errorf("%s: VerifyReport() error = %v, want ErrBadSignature", name, err)
		}
	}
	if err := reporter.VerifyReport(ctx, report); err != nil {
		t.Fatalf("VerifyReport(untouched) error = %v", err)
	}

	moved := *report
	moved.TenantID = "tenant-b"
	bctx := orchestration.ContextWithTenant(ctx, "tenant-b")
	if err := reporter.VerifyReport(bctx, &moved); !errors.Is(err, tenantagent.ErrForeignKey) && !errors.Is(err, tenantagent.ErrBadSignature) {
		t.Fatalf("VerifyReport(moved to tenant-b) error = %v, want ErrForeignKey or ErrBadSignature", err)
	}
	if err := reporter.VerifyReport(ctx, nil); !errors.Is(err, tenantagent.ErrBadSignature) {
		t.Fatalf("VerifyReport(nil) error = %v, want ErrBadSignature", err)
	}
}

func TestReportsAreIsolated(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a", "tenant-b")
	ctx := context.Background()
	tasks := orchestration.NewInMemoryTaskStore()
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		f.appoint(t, tenant, "tenant-agent-"+tenant)
		addProject(t, f.projects, tenant, "project-"+tenant)
		if _, err := f.planner.SetPlan(ctx, tenant, planRequest(0, "project-"+tenant)); err != nil {
			t.Fatalf("SetPlan(%s) error = %v", tenant, err)
		}
		addTask(t, tasks, tenant, "t-"+tenant, "project-"+tenant)
		addTask(t, tasks, tenant, "u-"+tenant, "")
	}
	addTask(t, tasks, "tenant-b", "t-b-2", "project-tenant-b")

	reporter := f.newReporter(t, f.planner, tasks)
	report, err := reporter.Report(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	want := []tenantagent.ProjectStatus{
		{ProjectID: "project-tenant-a", Name: "project-tenant-a", Rank: 1, Tasks: map[string]int{"pending": 1}},
	}
	if report.TenantID != "tenant-a" || report.Principal != "tenant-agent-tenant-a" || !reflect.DeepEqual(report.Projects, want) ||
		!reflect.DeepEqual(report.Unscoped, map[string]int{"pending": 1}) {
		t.Fatalf("report = %+v, want only tenant-a's projects and tasks", report)
	}
	for _, m := range report.Milestones {
		if !reflect.DeepEqual(m.ProjectIDs, []string{"project-tenant-a"}) {
			t.Fatalf("milestone %s names %v", m.ID, m.ProjectIDs)
		}
	}

	scoped := map[string]context.Context{
		"orchestration": orchestration.ContextWithTenant(ctx, "tenant-b"),
		"tenantctx":     tenantctx.WithTenant(ctx, "tenant-b"),
	}
	for name, bctx := range scoped {
		if _, err := reporter.Report(bctx, "tenant-a"); !errors.Is(err, tenantagent.ErrWrongTenant) {
			t.Errorf("%s: Report() error = %v, want ErrWrongTenant", name, err)
		}
		if err := reporter.VerifyReport(bctx, report); !errors.Is(err, tenantagent.ErrWrongTenant) {
			t.Errorf("%s: VerifyReport() error = %v, want ErrWrongTenant", name, err)
		}
	}
}

func TestNewReporter_RequiresEveryDependency(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a")
	full := tenantagent.ReporterOptions{
		Planner:  f.planner,
		Projects: f.projects,
		Tasks:    orchestration.NewInMemoryTaskStore(),
	}
	if _, err := tenantagent.NewReporter(full); err != nil {
		t.Fatalf("NewReporter(full, no clock) error = %v", err)
	}
	cases := map[string]func(o *tenantagent.ReporterOptions){
		"planner":  func(o *tenantagent.ReporterOptions) { o.Planner = nil },
		"projects": func(o *tenantagent.ReporterOptions) { o.Projects = nil },
		"tasks":    func(o *tenantagent.ReporterOptions) { o.Tasks = nil },
	}
	for name, drop := range cases {
		opts := full
		drop(&opts)
		if _, err := tenantagent.NewReporter(opts); !errors.Is(err, tenantagent.ErrNotConfigured) {
			t.Errorf("NewReporter(no %s) error = %v, want ErrNotConfigured", name, err)
		}
	}
}
