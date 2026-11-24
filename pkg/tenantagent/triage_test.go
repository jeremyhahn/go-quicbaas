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
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantagent"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// newTriager returns a Triager over planner, the fixture's projects and
// tasks, with its clock at reportClock.
func (f *planFixture) newTriager(t *testing.T, planner *tenantagent.Planner, tasks orchestration.TaskStore) *tenantagent.Triager {
	t.Helper()
	triager, err := tenantagent.NewTriager(tenantagent.TriagerOptions{
		Planner:  planner,
		Projects: f.projects,
		Tasks:    tasks,
		Now:      func() time.Time { return reportClock },
	})
	if err != nil {
		t.Fatalf("NewTriager() error = %v", err)
	}
	return triager
}

// addRankedTask creates a pending task with a priority and dependencies.
func addRankedTask(t *testing.T, store orchestration.TaskStore, tenant, id, project string, priority int, deps ...string) {
	t.Helper()
	task := &orchestration.Task{
		ID: id, TenantID: tenant, ProjectID: project, Subject: "task " + id,
		Status: orchestration.TaskStatusPending, Priority: priority, Dependencies: deps,
	}
	if err := store.CreateTask(orchestration.ContextWithTenant(context.Background(), tenant), task); err != nil {
		t.Fatalf("CreateTask(%s/%s) error = %v", tenant, id, err)
	}
}

// claimTask claims a task for an agent.
func claimTask(t *testing.T, store orchestration.TaskStore, tenant, id string) {
	t.Helper()
	ctx := orchestration.ContextWithTenant(context.Background(), tenant)
	if err := store.ClaimTask(ctx, tenant, id, "agent-1"); err != nil {
		t.Fatalf("ClaimTask(%s/%s) error = %v", tenant, id, err)
	}
}

// triageIDs lists the triage's task IDs in order.
func triageIDs(triage *tenantagent.Triage) []string {
	ids := make([]string, 0, len(triage.Items))
	for _, item := range triage.Items {
		ids = append(ids, item.TaskID)
	}
	return ids
}

func TestTriage_ForAutomateTheThings(t *testing.T) {
	t.Parallel()

	const tenant = "automatethethings"
	const cn = "tenant-agent-automatethethings"
	f := newPlanFixture(t, tenant)
	f.appoint(t, tenant, cn)
	for _, id := range []string{"go-quicbaas", "go-qrdb", "docs"} {
		addProject(t, f.projects, tenant, id)
	}
	ctx := context.Background()
	req := tenantagent.PlanRequest{
		Priorities: []orchestration.TenantPriority{{ProjectID: "go-qrdb", Rank: 1}, {ProjectID: "go-quicbaas", Rank: 2}},
		Milestones: []orchestration.TenantMilestone{
			{ID: "m-late", Title: "late", Due: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), ProjectIDs: []string{"go-quicbaas"}},
			{ID: "m-next", Title: "next", Due: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), ProjectIDs: []string{"go-qrdb"}},
		},
	}
	if _, err := f.planner.SetPlan(ctx, tenant, req); err != nil {
		t.Fatalf("SetPlan() error = %v", err)
	}
	tasks := orchestration.NewInMemoryTaskStore()
	addRankedTask(t, tasks, tenant, "q-ready", "go-qrdb", 1)
	addRankedTask(t, tasks, tenant, "q-done", "go-qrdb", 9)
	completeTask(t, tasks, tenant, "q-done")
	addRankedTask(t, tasks, tenant, "b-needs", "go-quicbaas", 0)
	blockTask(t, tasks, tenant, "b-needs", orchestration.TaskBlockKindNeedsInfo)
	addRankedTask(t, tasks, tenant, "b-esc", "go-quicbaas", 3)
	blockTask(t, tasks, tenant, "b-esc", orchestration.TaskBlockKindEscalated)
	addRankedTask(t, tasks, tenant, "b-ready", "go-quicbaas", 0, "q-done")
	addRankedTask(t, tasks, tenant, "b-wait", "go-quicbaas", 5, "b-ready", "q-done")
	addRankedTask(t, tasks, tenant, "d-claimed", "docs", 7)
	claimTask(t, tasks, tenant, "d-claimed")

	triager := f.newTriager(t, f.planner, tasks)
	triage, err := triager.Triage(ctx, tenant)
	if err != nil {
		t.Fatalf("Triage() error = %v", err)
	}
	const reason = "the owner must decide"
	want := []tenantagent.TriageItem{
		{TaskID: "q-ready", ProjectID: "go-qrdb", Subject: "task q-ready", Status: "pending", Disposition: tenantagent.DispositionReady,
			ProjectRank: 1, Priority: 1, Milestone: "m-next"},
		{TaskID: "b-wait", ProjectID: "go-quicbaas", Subject: "task b-wait", Status: "pending", Disposition: tenantagent.DispositionWaiting,
			ProjectRank: 2, Priority: 5, WaitingOn: []string{"b-ready"}, Milestone: "m-late", Overdue: true},
		{TaskID: "b-esc", ProjectID: "go-quicbaas", Subject: "task b-esc", Status: "blocked", Disposition: tenantagent.DispositionEscalated,
			ProjectRank: 2, Priority: 3, Milestone: "m-late", Overdue: true, Reason: reason},
		{TaskID: "b-needs", ProjectID: "go-quicbaas", Subject: "task b-needs", Status: "blocked", Disposition: tenantagent.DispositionNeedsOwner,
			ProjectRank: 2, Milestone: "m-late", Overdue: true, Reason: reason},
		{TaskID: "b-ready", ProjectID: "go-quicbaas", Subject: "task b-ready", Status: "pending", Disposition: tenantagent.DispositionReady,
			ProjectRank: 2, Milestone: "m-late", Overdue: true},
		{TaskID: "d-claimed", ProjectID: "docs", Subject: "task d-claimed", Status: "claimed", Disposition: tenantagent.DispositionInProgress,
			Priority: 7},
	}
	if !reflect.DeepEqual(triage.Items, want) {
		t.Fatalf("Items = %+v\nwant %+v", triage.Items, want)
	}
	wantCounts := map[string]int{
		tenantagent.DispositionReady: 2, tenantagent.DispositionWaiting: 1, tenantagent.DispositionEscalated: 1,
		tenantagent.DispositionNeedsOwner: 1, tenantagent.DispositionInProgress: 1,
	}
	if !reflect.DeepEqual(triage.Counts, wantCounts) {
		t.Fatalf("Counts = %v, want %v", triage.Counts, wantCounts)
	}
	key := tenantagent.SigningKeyFor(tenant)
	if triage.TenantID != tenant || triage.PlanRevision != 1 || !triage.GeneratedAt.Equal(reportClock) ||
		triage.Principal != cn || triage.SigningKeyID != key || len(triage.Signature) == 0 {
		t.Fatalf("triage = %+v", triage)
	}
	if err := triager.VerifyTriage(ctx, triage); err != nil {
		t.Fatalf("VerifyTriage() error = %v", err)
	}
	signed, _ := f.keys.seen()
	if len(signed) != 2 || signed[1] != key {
		t.Fatalf("signed with %v, want the plan then the triage with %q", signed, key)
	}
}

func TestTriage_WithoutAPlan(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a")
	f.appoint(t, "tenant-a", "tenant-agent-a")
	addProject(t, f.projects, "tenant-a", "go-quicbaas")
	tasks := orchestration.NewInMemoryTaskStore()
	addRankedTask(t, tasks, "tenant-a", "t-1", "go-quicbaas", 0)
	addRankedTask(t, tasks, "tenant-a", "t-2", "", 2)
	addRankedTask(t, tasks, "tenant-a", "t-3", "elsewhere", 0)
	addRankedTask(t, tasks, "tenant-a", "t-4", "go-quicbaas", 2)
	blockTask(t, tasks, "tenant-a", "t-4", orchestration.TaskBlockKindBlocked)

	triager := f.newTriager(t, f.planner, tasks)
	triage, err := triager.Triage(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("Triage() error = %v", err)
	}
	if got := triageIDs(triage); !reflect.DeepEqual(got, []string{"t-2", "t-4", "t-1", "t-3"}) {
		t.Fatalf("order = %v, want priority then creation", got)
	}
	for _, item := range triage.Items {
		if item.ProjectRank != 0 || item.Milestone != "" || item.Overdue {
			t.Fatalf("item %+v ranked or at a milestone without a plan", item)
		}
	}
	if triage.Items[1].Disposition != tenantagent.DispositionHeld {
		t.Fatalf("blocked task disposition = %q, want held", triage.Items[1].Disposition)
	}
	if triage.PlanRevision != 0 || len(triage.Signature) == 0 {
		t.Fatalf("triage = %+v, want revision 0 and signed", triage)
	}
	if signed, _ := f.keys.seen(); len(signed) != 1 {
		t.Fatalf("signed %v, want the triage only", signed)
	}
}

func TestTriage_DependencyNotListed(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a")
	f.appoint(t, "tenant-a", "tenant-agent-a")
	tasks := orchestration.NewInMemoryTaskStore()
	addRankedTask(t, tasks, "tenant-a", "t-1", "", 0, "ghost")
	triage, err := f.newTriager(t, f.planner, tasks).Triage(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("Triage() error = %v", err)
	}
	if len(triage.Items) != 1 || triage.Items[0].Disposition != tenantagent.DispositionWaiting ||
		!reflect.DeepEqual(triage.Items[0].WaitingOn, []string{"ghost"}) {
		t.Fatalf("Items = %+v, want t-1 waiting on ghost", triage.Items)
	}
}

func TestTriage_RequiresAnAppointment(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a")
	ctx := context.Background()
	tasks := orchestration.NewInMemoryTaskStore()
	addRankedTask(t, tasks, "tenant-a", "t-1", "", 0)
	triager := f.newTriager(t, f.planner, tasks)
	if triage, err := triager.Triage(ctx, "tenant-a"); !errors.Is(err, tenantagent.ErrNotAppointed) || triage != nil {
		t.Fatalf("Triage() = %v, %v; want ErrNotAppointed", triage, err)
	}
	f.appoint(t, "tenant-a", "tenant-agent-a")
	if err := f.svc.Retire(ctx, "tenant-a"); err != nil {
		t.Fatalf("Retire() error = %v", err)
	}
	if _, err := triager.Triage(ctx, "tenant-a"); !errors.Is(err, tenantagent.ErrNotAppointed) {
		t.Fatalf("Triage() after retire error = %v, want ErrNotAppointed", err)
	}
	if signed, _ := f.keys.seen(); len(signed) != 0 {
		t.Fatalf("signed %v without an appointment", signed)
	}
}

func TestTriage_RefusesATamperedPlan(t *testing.T) {
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
	triager := f.newTriager(t, reranked, orchestration.NewInMemoryTaskStore())
	if triage, err := triager.Triage(ctx, "tenant-a"); !errors.Is(err, tenantagent.ErrBadSignature) || triage != nil {
		t.Fatalf("Triage(tampered plan) = %v, %v; want ErrBadSignature and no triage", triage, err)
	}
	if after, _ := f.keys.seen(); len(after) != len(before) {
		t.Fatalf("signed %v over a tampered plan", after[len(before):])
	}
}

func TestTriage_RefusesAnIncompleteTaskListing(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a")
	f.appoint(t, "tenant-a", "tenant-agent-a")
	tasks := orchestration.NewInMemoryTaskStore()
	addTask(t, tasks, "tenant-a", "t-1", "")
	triager := f.newTriager(t, f.planner, unreadableTasks{TaskStore: tasks})
	if triage, err := triager.Triage(context.Background(), "tenant-a"); !errors.Is(err, orchestration.ErrTaskListingIncomplete) || triage != nil {
		t.Fatalf("Triage(incomplete listing) = %v, %v; want ErrTaskListingIncomplete and no triage", triage, err)
	}
	if signed, _ := f.keys.seen(); len(signed) != 0 {
		t.Fatalf("signed %v over an incomplete listing", signed)
	}
}

func TestTriage_WritesNothing(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a")
	f.appoint(t, "tenant-a", "tenant-agent-a")
	tasks := orchestration.NewInMemoryTaskStore()
	addRankedTask(t, tasks, "tenant-a", "t-1", "", 0)
	addRankedTask(t, tasks, "tenant-a", "t-2", "", 0, "t-1")
	addRankedTask(t, tasks, "tenant-a", "t-3", "", 0)
	blockTask(t, tasks, "tenant-a", "t-3", orchestration.TaskBlockKindNeedsInfo)
	addRankedTask(t, tasks, "tenant-a", "t-4", "", 0)
	claimTask(t, tasks, "tenant-a", "t-4")
	ids := []string{"t-1", "t-2", "t-3", "t-4"}
	ctx := orchestration.ContextWithTenant(context.Background(), "tenant-a")
	snapshot := func() map[string]string {
		out := map[string]string{}
		for _, id := range ids {
			task, err := tasks.GetTask(ctx, "tenant-a", id)
			if err != nil {
				t.Fatalf("GetTask(%s) error = %v", id, err)
			}
			out[id] = fmt.Sprintf("%s@%d", task.Status, task.Version)
		}
		return out
	}
	before := snapshot()
	if _, err := f.newTriager(t, f.planner, tasks).Triage(context.Background(), "tenant-a"); err != nil {
		t.Fatalf("Triage() error = %v", err)
	}
	if after := snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatalf("tasks changed: before %v, after %v", before, after)
	}
}

func TestVerifyTriage_RefusesATamperedTriage(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a", "tenant-b")
	f.appoint(t, "tenant-a", "tenant-agent-a")
	ctx := context.Background()
	tasks := orchestration.NewInMemoryTaskStore()
	addRankedTask(t, tasks, "tenant-a", "t-1", "", 2)
	addRankedTask(t, tasks, "tenant-a", "t-2", "", 1)
	triager := f.newTriager(t, f.planner, tasks)
	triage, err := triager.Triage(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("Triage() error = %v", err)
	}

	edits := map[string]func(tr *tenantagent.Triage){
		"disposition": func(tr *tenantagent.Triage) {
			tr.Items = append([]tenantagent.TriageItem(nil), tr.Items...)
			tr.Items[0].Disposition = tenantagent.DispositionHeld
		},
		"order": func(tr *tenantagent.Triage) {
			tr.Items = []tenantagent.TriageItem{tr.Items[1], tr.Items[0]}
		},
		"generated at": func(tr *tenantagent.Triage) { tr.GeneratedAt = tr.GeneratedAt.Add(time.Hour) },
	}
	for name, edit := range edits {
		tampered := *triage
		edit(&tampered)
		if err := triager.VerifyTriage(ctx, &tampered); !errors.Is(err, tenantagent.ErrBadSignature) {
			t.Errorf("%s: VerifyTriage() error = %v, want ErrBadSignature", name, err)
		}
	}
	if err := triager.VerifyTriage(ctx, triage); err != nil {
		t.Fatalf("VerifyTriage(untouched) error = %v", err)
	}

	moved := *triage
	moved.TenantID = "tenant-b"
	bctx := orchestration.ContextWithTenant(ctx, "tenant-b")
	if err := triager.VerifyTriage(bctx, &moved); !errors.Is(err, tenantagent.ErrForeignKey) && !errors.Is(err, tenantagent.ErrBadSignature) {
		t.Fatalf("VerifyTriage(moved to tenant-b) error = %v, want ErrForeignKey or ErrBadSignature", err)
	}
	if err := triager.VerifyTriage(ctx, nil); !errors.Is(err, tenantagent.ErrBadSignature) {
		t.Fatalf("VerifyTriage(nil) error = %v, want ErrBadSignature", err)
	}
}

func TestTriagesAreIsolated(t *testing.T) {
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
		addRankedTask(t, tasks, tenant, "t-"+tenant, "project-"+tenant, 0)
	}
	addRankedTask(t, tasks, "tenant-b", "t-b-2", "project-tenant-b", 9)
	// A tenant-a task naming tenant-b's project gets neither rank nor milestone.
	addRankedTask(t, tasks, "tenant-a", "t-a-2", "project-tenant-b", 0)

	triager := f.newTriager(t, f.planner, tasks)
	triage, err := triager.Triage(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("Triage() error = %v", err)
	}
	if got := triageIDs(triage); !reflect.DeepEqual(got, []string{"t-tenant-a", "t-a-2"}) {
		t.Fatalf("items = %v, want only tenant-a's tasks", got)
	}
	if first := triage.Items[0]; first.ProjectRank != 1 || first.Milestone != "m-1" {
		t.Fatalf("tenant-a item = %+v, want rank 1 at m-1", first)
	}
	if foreign := triage.Items[1]; foreign.ProjectRank != 0 || foreign.Milestone != "" {
		t.Fatalf("item naming tenant-b's project = %+v, want no rank and no milestone", foreign)
	}
	if triage.TenantID != "tenant-a" || triage.Principal != "tenant-agent-tenant-a" {
		t.Fatalf("triage = %+v", triage)
	}

	scoped := map[string]context.Context{
		"orchestration": orchestration.ContextWithTenant(ctx, "tenant-b"),
		"tenantctx":     tenantctx.WithTenant(ctx, "tenant-b"),
	}
	for name, bctx := range scoped {
		if _, err := triager.Triage(bctx, "tenant-a"); !errors.Is(err, tenantagent.ErrWrongTenant) {
			t.Errorf("%s: Triage() error = %v, want ErrWrongTenant", name, err)
		}
		if err := triager.VerifyTriage(bctx, triage); !errors.Is(err, tenantagent.ErrWrongTenant) {
			t.Errorf("%s: VerifyTriage() error = %v, want ErrWrongTenant", name, err)
		}
	}
}

func TestNewTriager_RequiresEveryDependency(t *testing.T) {
	t.Parallel()

	f := newPlanFixture(t, "tenant-a")
	full := tenantagent.TriagerOptions{
		Planner:  f.planner,
		Projects: f.projects,
		Tasks:    orchestration.NewInMemoryTaskStore(),
	}
	if _, err := tenantagent.NewTriager(full); err != nil {
		t.Fatalf("NewTriager(full, no clock) error = %v", err)
	}
	cases := map[string]func(o *tenantagent.TriagerOptions){
		"planner":  func(o *tenantagent.TriagerOptions) { o.Planner = nil },
		"projects": func(o *tenantagent.TriagerOptions) { o.Projects = nil },
		"tasks":    func(o *tenantagent.TriagerOptions) { o.Tasks = nil },
	}
	for name, drop := range cases {
		opts := full
		drop(&opts)
		if _, err := tenantagent.NewTriager(opts); !errors.Is(err, tenantagent.ErrNotConfigured) {
			t.Errorf("NewTriager(no %s) error = %v, want ErrNotConfigured", name, err)
		}
	}
}
