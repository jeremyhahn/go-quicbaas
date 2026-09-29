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

package orchestration

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Fixtures
// =============================================================================

const (
	admissionTenant  = "tenant-admission"
	admissionTenant2 = "tenant-admission-2"
)

// admissionClock is a fixed instant every test reads, so a lease's liveness is
// arithmetic rather than a race with the wall clock.
var admissionClock = time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

// recordedLeaseStore is a JobLeaseStore whose records a test writes directly.
//
// THE REAL STORE STAMPS ITS OWN CLOCK, which is exactly right in production and
// wrong here: the residual rotation is ordered by when a project last STARTED a
// job, so a test of it has to place three starts at three distinct instants.
// NewProcessJobLeaseStore takes the wall clock for AcquiredAt and keeps it
// through a release, so the only way to write a distinct history through it is
// to wait — and no test waits. Only List is overridden; every other method
// would panic through the embedded nil, which is correct for a double that is
// only ever read.
type recordedLeaseStore struct {
	JobLeaseStore
	records []*JobLease
	err     error
}

func (s *recordedLeaseStore) List(_ context.Context, tenantID, projectID string) ([]*JobLease, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]*JobLease, 0, len(s.records))
	for _, lease := range s.records {
		// A nil record is passed THROUGH rather than filtered here: whether the
		// controller survives one is the controller's property to prove, and a
		// double that quietly dropped them would hide the gap.
		if lease == nil {
			out = append(out, lease)
			continue
		}
		if tenantID != "" && lease.TenantID != tenantID {
			continue
		}
		if projectID != "" && lease.ProjectID != projectID {
			continue
		}
		out = append(out, lease)
	}
	return out, nil
}

// admissionRig assembles a controller over in-memory stores.
type admissionRig struct {
	tenants  *fakeTenantLister
	projects ProjectStore
	queue    *fakeDispatchStore
	leases   *recordedLeaseStore
	headroom *fakePoolHeadroom
	spend    ScopeSpendReader
}

func newAdmissionRig(t *testing.T, tenantIDs ...string) *admissionRig {
	t.Helper()
	if len(tenantIDs) == 0 {
		tenantIDs = []string{admissionTenant}
	}
	return &admissionRig{
		tenants:  &fakeTenantLister{ids: tenantIDs},
		projects: NewInMemoryProjectStore(),
		queue:    newFakeDispatchStore(),
		leases:   &recordedLeaseStore{},
	}
}

// project registers an autonomous project with a concurrency quota.
func (r *admissionRig) project(t *testing.T, tenantID, projectID string, maxConcurrency int) {
	t.Helper()
	r.projectWith(t, &Project{
		ID: projectID, TenantID: tenantID, Name: projectID, Autonomous: true,
		Quotas: ProjectQuotas{MaxConcurrency: maxConcurrency},
	})
}

func (r *admissionRig) projectWith(t *testing.T, project *Project) {
	t.Helper()
	ctx := ContextWithTenant(context.Background(), project.TenantID)
	if err := r.projects.Create(ctx, project); err != nil {
		t.Fatalf("create project %s: %v", project.ID, err)
	}
}

// running records n live leases for a project, each heartbeating at the rig's
// fixed instant so every one of them is live.
func (r *admissionRig) running(t *testing.T, tenantID, projectID string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		r.leases.records = append(r.leases.records, &JobLease{
			TenantID: tenantID, ProjectID: projectID, Workflow: "sdlc",
			Ref: projectID + "#" + strconv.Itoa(i), JobID: projectID + "-job-" + strconv.Itoa(i),
			Generation: 1, AcquiredAt: admissionClock.UnixNano(), HeartbeatAt: admissionClock.UnixNano(),
		})
	}
}

// served records a FINISHED job for a project, started at `at`. It is what the
// residual rotation reads: the project's most recent start, with no slot held.
func (r *admissionRig) served(tenantID, projectID string, at time.Time) {
	r.leases.records = append(r.leases.records, &JobLease{
		TenantID: tenantID, ProjectID: projectID, Workflow: "sdlc",
		Ref:   projectID + "#done-" + strconv.FormatInt(at.UnixNano(), 10),
		JobID: projectID + "-done-" + strconv.FormatInt(at.UnixNano(), 10), Generation: 1,
		AcquiredAt: at.UnixNano(), HeartbeatAt: at.UnixNano(),
		Released: true, ReleasedAt: at.UnixNano(),
	})
}

// stale records a lease whose heartbeat is older than the TTL: a dead holder.
func (r *admissionRig) stale(tenantID, projectID string) {
	at := admissionClock.Add(-10 * testPollerLeaseTTL)
	r.leases.records = append(r.leases.records, &JobLease{
		TenantID: tenantID, ProjectID: projectID, Workflow: "sdlc",
		Ref: projectID + "#stale", JobID: projectID + "-stale", Generation: 1,
		AcquiredAt: at.UnixNano(), HeartbeatAt: at.UnixNano(),
	})
}

// queued adds n pending, auto-spawning tasks to a project's queue.
func (r *admissionRig) queued(tenantID, projectID string, n int) {
	for i := 0; i < n; i++ {
		id := projectID + "-task-" + strconv.Itoa(i)
		r.queue.tasks[id] = &Task{
			ID: id, TenantID: tenantID, ProjectID: projectID, Subject: id,
			Status: TaskStatusPending, AutoSpawn: true, Version: 1,
		}
	}
}

// declaredCapacity is the FleetCapacityReader for a fixed operator-declared slot
// count, which is what most of these tests want: the fleet axis fixed at a known
// number so the division under test is the only variable.
func declaredCapacity(t *testing.T, slots int) FleetCapacityReader {
	t.Helper()
	reader, err := NewDeclaredFleetCapacity(slots)
	if err != nil {
		t.Fatalf("NewDeclaredFleetCapacity(%d): %v", slots, err)
	}
	return reader
}

func (r *admissionRig) controller(t *testing.T, capacity int) *AdmissionController {
	t.Helper()
	cfg := AdmissionControllerConfig{
		Tenants: r.tenants, Projects: r.projects, Queue: r.queue, Leases: r.leases,
		LeaseTTL: testPollerLeaseTTL, Capacity: declaredCapacity(t, capacity),
		Now: func() time.Time { return admissionClock },
	}
	if r.headroom != nil {
		cfg.Headroom = r.headroom
	}
	if r.spend != nil {
		cfg.Spend = r.spend
	}
	controller, err := NewAdmissionController(cfg)
	if err != nil {
		t.Fatalf("NewAdmissionController: %v", err)
	}
	return controller
}

// fakePoolHeadroom answers the provider axis from a table.
type fakePoolHeadroom struct {
	byProject map[string]PoolHeadroom
	err       error
	calls     int
}

func (f *fakePoolHeadroom) Headroom(_ context.Context, _, projectID string, _ time.Time) (PoolHeadroom, error) {
	f.calls++
	if f.err != nil {
		return PoolHeadroom{}, f.err
	}
	if headroom, ok := f.byProject[projectID]; ok {
		return headroom, nil
	}
	// Default: one pooled account with headroom, so a project the table does
	// not name is unaffected by this axis.
	return PoolHeadroom{Pooled: 1, Ready: 1}, nil
}

// failingQueue is a TaskQueueReader that cannot list.
type failingQueue struct{ err error }

func (f *failingQueue) ListTasks(context.Context, string, *TaskFilter) ([]*Task, error) {
	return nil, f.err
}

// failingProjectLister and failingLeaseLister are declared in
// task_dispatcher_test.go, where the dispatcher's own fail-closed tests use
// them. Reused rather than redeclared: one double per unreadable store.

// =============================================================================
// Construction
// =============================================================================

func TestNewAdmissionController_RequiresEveryStore(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	full := AdmissionControllerConfig{
		Tenants: rig.tenants, Projects: rig.projects, Queue: rig.queue,
		Leases: rig.leases, LeaseTTL: testPollerLeaseTTL,
	}
	if _, err := NewAdmissionController(full); err != nil {
		t.Fatalf("a complete configuration was refused: %v", err)
	}

	cases := map[string]func(*AdmissionControllerConfig){
		"tenants":    func(c *AdmissionControllerConfig) { c.Tenants = nil },
		"projects":   func(c *AdmissionControllerConfig) { c.Projects = nil },
		"queue":      func(c *AdmissionControllerConfig) { c.Queue = nil },
		"leases":     func(c *AdmissionControllerConfig) { c.Leases = nil },
		"lease_ttl":  func(c *AdmissionControllerConfig) { c.LeaseTTL = 0 },
		"lease_ttl2": func(c *AdmissionControllerConfig) { c.LeaseTTL = -time.Second },
	}
	for name, break_ := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := full
			break_(&cfg)
			_, err := NewAdmissionController(cfg)
			if !errors.Is(err, ErrAdmissionConfig) {
				t.Fatalf("%s: want ErrAdmissionConfig, got %v", name, err)
			}
		})
	}
}

func TestNewAdmissionController_DefaultsTheClock(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	controller, err := NewAdmissionController(AdmissionControllerConfig{
		Tenants: rig.tenants, Projects: rig.projects, Queue: rig.queue,
		Leases: rig.leases, LeaseTTL: testPollerLeaseTTL, Capacity: declaredCapacity(t, 4),
	})
	if err != nil {
		t.Fatalf("NewAdmissionController: %v", err)
	}
	if controller.now == nil {
		t.Fatal("a controller built with no clock must default to one")
	}
	got, err := controller.FleetCapacity(context.Background(), admissionClock)
	if err != nil {
		t.Fatalf("FleetCapacity: %v", err)
	}
	if got.Slots != 4 || got.Source != FleetCapacityOperator {
		t.Fatalf("FleetCapacity = %+v, want 4 slots from the operator", got)
	}
}

// =============================================================================
// THE HEADLINE SAFETY PROPERTY
// =============================================================================

// TestPlan_ProjectsWithinTheirOwnQuotaCannotExceedFleetCapacity is the issue's
// first acceptance check, and the reason this file exists: before admission, N
// autonomous projects each inside its own MaxConcurrency oversubscribed the
// fleet, because nothing summed them.
//
// Five projects, each permitted three concurrent jobs — fifteen in aggregate —
// against a fleet of four slots. The sum of what admission hands out must be at
// most four, and no project may be given more than its own quota.
// TestPlan_AnOffBudgetLeaseHoldsNoProjectSlotButAFleetSlot proves the plan
// counts leases through the lease's own rules, one per axis.
//
// THE PROJECT AXIS: a live lease a triage or review job holds (OffBudget, see
// JobLeasePolicy) consumes no project slot. Measured need: a freshly onboarded
// project ran one worker beside a 50-issue NPU triage pass and the dispatcher
// reported "inflight=2 slots=0" against a quota of 2 for the whole pass.
//
// THE FLEET AXIS: the same lease DOES consume a fleet slot, because the job
// runs a container (owner ruling, 2026-09-25). The plan used to leave it out
// while fleetCapacityJobLeaseStore counted it, so the plan handed out a slot
// the lease store then refused. A dormant lease, whose job runs nothing, is
// off both axes.
func TestPlan_AnOffBudgetLeaseHoldsNoProjectSlotButAFleetSlot(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.project(t, admissionTenant, "p0", 2)
	rig.queued(admissionTenant, "p0", 3)
	rig.running(t, admissionTenant, "p0", 1)
	rig.leases.records = append(rig.leases.records, &JobLease{
		TenantID: admissionTenant, ProjectID: "p0", Workflow: "triage",
		Ref: "triage:p0", JobID: "p0-triage", Generation: 1,
		AcquiredAt: admissionClock.UnixNano(), HeartbeatAt: admissionClock.UnixNano(),
		OffBudget: true,
	}, &JobLease{
		TenantID: admissionTenant, ProjectID: "p0", Workflow: "sdlc",
		Ref: "sdlc:p0-reviewing", JobID: "p0-reviewing", Generation: 1,
		AcquiredAt: admissionClock.UnixNano(), HeartbeatAt: admissionClock.UnixNano(),
		OffBudget: true, Dormant: true,
	})

	plan, err := rig.controller(t, 3).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	decision := plan.For(admissionTenant, "p0")
	if decision.ProjectInflight != 1 {
		t.Fatalf("project inflight = %d, want 1: the triage and dormant leases are off the project budget", decision.ProjectInflight)
	}
	if plan.FleetInflight != 2 {
		t.Fatalf("fleet inflight = %d, want 2: the worker and the triage container, not the dormant job", plan.FleetInflight)
	}
	if decision.DispatchSlots() != 1 {
		t.Fatalf("dispatch slots = %d, want 1: a quota of 2 with one worker, and one free fleet slot of 3", decision.DispatchSlots())
	}
}

// TestPlan_ATriageContainerCanFillTheFleet is the refusal side of the same
// ruling: a fleet whose last slot is held by a triage container has no slot to
// hand out, however much of the project's own quota is free, and the decision
// names the fleet as the axis that bounded it.
func TestPlan_ATriageContainerCanFillTheFleet(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.project(t, admissionTenant, "p0", 3)
	rig.queued(admissionTenant, "p0", 2)
	rig.running(t, admissionTenant, "p0", 1)
	rig.leases.records = append(rig.leases.records, &JobLease{
		TenantID: admissionTenant, ProjectID: "p0", Workflow: "triage",
		Ref: "triage:p0", JobID: "p0-triage", Generation: 1,
		AcquiredAt: admissionClock.UnixNano(), HeartbeatAt: admissionClock.UnixNano(),
		OffBudget: true,
	})

	plan, err := rig.controller(t, 2).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.FleetFree != 0 {
		t.Fatalf("fleet free = %d, want 0: the worker and the triage container hold both slots", plan.FleetFree)
	}
	decision := plan.For(admissionTenant, "p0")
	if decision.DispatchSlots() != 0 {
		t.Fatalf("dispatch slots = %d, want 0 on a full fleet", decision.DispatchSlots())
	}
	if decision.Axis != AdmissionAxisFleetCapacity {
		t.Fatalf("axis = %q, want %q", decision.Axis, AdmissionAxisFleetCapacity)
	}
}

func TestPlan_ProjectsWithinTheirOwnQuotaCannotExceedFleetCapacity(t *testing.T) {
	t.Parallel()
	const (
		projects      = 5
		perProject    = 3
		fleetCapacity = 4
	)
	rig := newAdmissionRig(t)
	for i := 0; i < projects; i++ {
		id := "p" + strconv.Itoa(i)
		rig.project(t, admissionTenant, id, perProject)
		rig.queued(admissionTenant, id, perProject)
	}

	plan, err := rig.controller(t, fleetCapacity).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	total := 0
	for i := 0; i < projects; i++ {
		decision := plan.For(admissionTenant, "p"+strconv.Itoa(i))
		if !decision.Bounded {
			t.Fatalf("p%d: a project under a declared fleet capacity must be bounded", i)
		}
		if decision.DispatchSlots() > perProject {
			t.Fatalf("p%d: %d slots exceeds the project's own quota of %d",
				i, decision.DispatchSlots(), perProject)
		}
		total += decision.DispatchSlots()
	}
	if total > fleetCapacity {
		t.Fatalf("%d projects each within a quota of %d were admitted %d jobs in aggregate, "+
			"which exceeds the fleet's %d slots", projects, perProject, total, fleetCapacity)
	}
	if total != fleetCapacity {
		t.Fatalf("with more demand than capacity every slot must be used: admitted %d of %d",
			total, fleetCapacity)
	}
}

// TestPlan_CountsLiveJobsAgainstTheFleetCapacity: the slots already held by
// running jobs are not handed out a second time, whichever project holds them.
func TestPlan_CountsLiveJobsAgainstTheFleetCapacity(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.project(t, admissionTenant, "busy", 10)
	rig.project(t, admissionTenant, "idle", 10)
	rig.queued(admissionTenant, "busy", 5)
	rig.queued(admissionTenant, "idle", 5)
	// Three of the fleet's four slots are already running, all in one project.
	rig.running(t, admissionTenant, "busy", 3)

	plan, err := rig.controller(t, 4).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.FleetInflight != 3 {
		t.Fatalf("FleetInflight = %d, want 3", plan.FleetInflight)
	}
	if plan.FleetFree != 1 {
		t.Fatalf("FleetFree = %d, want 1", plan.FleetFree)
	}
	total := plan.For(admissionTenant, "busy").DispatchSlots() +
		plan.For(admissionTenant, "idle").DispatchSlots()
	if total != 1 {
		t.Fatalf("one free slot was handed out %d times", total)
	}
}

// TestPlan_CountsLiveJobsAcrossEveryTenant: the fleet is the deployment, not a
// tenant. A per-tenant count would let two tenants each fill the fleet.
func TestPlan_CountsLiveJobsAcrossEveryTenant(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t, admissionTenant, admissionTenant2)
	rig.project(t, admissionTenant, "a", 5)
	rig.project(t, admissionTenant2, "b", 5)
	rig.queued(admissionTenant, "a", 5)
	rig.queued(admissionTenant2, "b", 5)
	rig.running(t, admissionTenant, "a", 2)
	rig.running(t, admissionTenant2, "b", 1)

	plan, err := rig.controller(t, 4).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.FleetInflight != 3 {
		t.Fatalf("FleetInflight = %d, want 3 (two tenants' leases summed)", plan.FleetInflight)
	}
	total := plan.For(admissionTenant, "a").DispatchSlots() +
		plan.For(admissionTenant2, "b").DispatchSlots()
	if total != 1 {
		t.Fatalf("with 3 of 4 slots live across two tenants, %d were handed out", total)
	}
}

// TestPlan_AStaleLeaseDoesNotHoldASlot: a lease past its TTL is a dead holder,
// and counting it would shrink the fleet permanently after any crash.
func TestPlan_AStaleLeaseDoesNotHoldASlot(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.project(t, admissionTenant, "p", 5)
	rig.queued(admissionTenant, "p", 5)
	rig.stale(admissionTenant, "p")

	plan, err := rig.controller(t, 2).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.FleetInflight != 0 {
		t.Fatalf("FleetInflight = %d, want 0: a lease past its TTL holds nothing", plan.FleetInflight)
	}
	if got := plan.For(admissionTenant, "p").DispatchSlots(); got != 2 {
		t.Fatalf("DispatchSlots = %d, want 2", got)
	}
}

// TestPlan_AReleasedLeaseHoldsNoSlotButRecordsAStart: the two facts a finished
// job carries are deliberately different. It frees its slot immediately, and it
// still counts as the project having been served — which is what the residual
// rotation orders on.
func TestPlan_AReleasedLeaseHoldsNoSlotButRecordsAStart(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.project(t, admissionTenant, "p", 5)
	rig.queued(admissionTenant, "p", 5)
	rig.served(admissionTenant, "p", admissionClock.Add(-time.Hour))

	plan, err := rig.controller(t, 3).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.FleetInflight != 0 {
		t.Fatalf("FleetInflight = %d, want 0: a released lease holds nothing", plan.FleetInflight)
	}
	if got := plan.For(admissionTenant, "p").DispatchSlots(); got != 3 {
		t.Fatalf("DispatchSlots = %d, want the whole free fleet of 3", got)
	}
}

// =============================================================================
// THE FAIRNESS PROPERTY
// =============================================================================

// TestPlan_FairnessIsMaxMin is the stated fairness property's positive half: a
// project asking for less than an equal share gets everything it asked for, and
// the surplus is redistributed rather than idled.
//
// Three projects, six free slots. One asks for one job, the others for ten each.
// An equal split would be two apiece and would waste one slot on the small
// project's unused share; max-min fairness gives the small project its one and
// divides the remaining five between the other two.
func TestPlan_FairnessIsMaxMin(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.project(t, admissionTenant, "small", 1)
	rig.project(t, admissionTenant, "big-a", 10)
	rig.project(t, admissionTenant, "big-b", 10)
	rig.queued(admissionTenant, "small", 1)
	rig.queued(admissionTenant, "big-a", 10)
	rig.queued(admissionTenant, "big-b", 10)

	plan, err := rig.controller(t, 6).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	small := plan.For(admissionTenant, "small").DispatchSlots()
	a := plan.For(admissionTenant, "big-a").DispatchSlots()
	b := plan.For(admissionTenant, "big-b").DispatchSlots()

	if small != 1 {
		t.Fatalf("a project asking for less than its equal share must get all of it: small got %d of 1", small)
	}
	if a+b != 5 {
		t.Fatalf("the small project's unused share must be redistributed: big-a+big-b = %d, want 5", a+b)
	}
	if a+b+small != 6 {
		t.Fatalf("no slot may be idled to preserve a share nobody claimed: total %d of 6", a+b+small)
	}
	// Neither large project may be reduced below the equal split to feed the
	// other: 5 between two is 2 and 3.
	if a < 2 || b < 2 {
		t.Fatalf("max-min fairness reduced a project below the equal split: big-a=%d big-b=%d", a, b)
	}
}

// TestPlan_ABusyProjectYieldsTheResidualToAnUnservedOne is the fairness
// property's no-starvation half, and the regression the issue warned about: a
// fleet-wide bound handed out first-come would let one busy project hold every
// slot forever.
//
// One free slot, two projects both asking. The project running nothing must get
// it, because the residual is ordered by least-served.
func TestPlan_ABusyProjectYieldsTheResidualToAnUnservedOne(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.project(t, admissionTenant, "busy", 10)
	rig.project(t, admissionTenant, "starved", 10)
	rig.queued(admissionTenant, "busy", 10)
	rig.queued(admissionTenant, "starved", 10)
	// The busy project holds three of the fleet's four slots. One is free.
	rig.running(t, admissionTenant, "busy", 3)

	plan, err := rig.controller(t, 4).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	busy := plan.For(admissionTenant, "busy").DispatchSlots()
	starved := plan.For(admissionTenant, "starved").DispatchSlots()
	if starved != 1 || busy != 0 {
		t.Fatalf("the single free slot must go to the project running nothing: busy=%d starved=%d", busy, starved)
	}
	if reason := plan.For(admissionTenant, "busy").Reason; reason == "" {
		t.Fatal("a project handed no slot must say why")
	}
	if axis := plan.For(admissionTenant, "busy").Axis; axis != AdmissionAxisFleetCapacity {
		t.Fatalf("the busy project was refused by the fleet, not by its own quota: axis=%s", axis)
	}
}

// TestPlan_TheResidualRotatesByLastStart: among projects running the same
// number of jobs, the one served longest ago goes first. That is what makes the
// rotation move rather than favouring one name forever.
//
// The three project ids are chosen so ALPHABETICAL order disagrees with the
// answer: sorted by name the winner would be "older", so a test that passed by
// accident of the tie-break would pick the wrong project.
func TestPlan_TheResidualRotatesByLastStart(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	starts := map[string]time.Duration{
		"recent": time.Minute,
		"older":  time.Hour,
		"oldest": 24 * time.Hour,
	}
	for id, ago := range starts {
		rig.project(t, admissionTenant, id, 10)
		rig.queued(admissionTenant, id, 10)
		rig.served(admissionTenant, id, admissionClock.Add(-ago))
	}

	plan, err := rig.controller(t, 1).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := plan.For(admissionTenant, "oldest").DispatchSlots(); got != 1 {
		t.Fatalf("the least recently served project must take the residual: oldest got %d", got)
	}
	for _, id := range []string{"recent", "older"} {
		if got := plan.For(admissionTenant, id).DispatchSlots(); got != 0 {
			t.Fatalf("%s took a slot ahead of the least recently served project: %d", id, got)
		}
	}
}

// TestPlan_EveryDemandingProjectIsServedWithinCeilOfProjectsOverSlots walks the
// rotation forward and asserts the stated liveness bound: with P projects
// sharing F free slots, every asking project is admitted within ceil(P/F)
// sweeps. Taking a slot makes a project the most recently served, which puts it
// behind the others.
func TestPlan_EveryDemandingProjectIsServedWithinCeilOfProjectsOverSlots(t *testing.T) {
	t.Parallel()
	const (
		projects = 5
		free     = 2
		sweeps   = 3 // ceil(5/2)
	)
	rig := newAdmissionRig(t)
	ids := make([]string, 0, projects)
	for i := 0; i < projects; i++ {
		id := "p" + strconv.Itoa(i)
		ids = append(ids, id)
		rig.project(t, admissionTenant, id, 10)
		rig.queued(admissionTenant, id, 10)
	}

	served := make(map[string]bool, projects)
	for sweep := 0; sweep < sweeps; sweep++ {
		at := admissionClock.Add(time.Duration(sweep) * time.Minute)
		controller, err := NewAdmissionController(AdmissionControllerConfig{
			Tenants: rig.tenants, Projects: rig.projects, Queue: rig.queue, Leases: rig.leases,
			LeaseTTL: testPollerLeaseTTL, Capacity: declaredCapacity(t, free),
			Now: func() time.Time { return at },
		})
		if err != nil {
			t.Fatalf("NewAdmissionController: %v", err)
		}
		plan, err := controller.Plan(context.Background())
		if err != nil {
			t.Fatalf("sweep %d: Plan: %v", sweep, err)
		}
		admitted := 0
		for _, id := range ids {
			if plan.For(admissionTenant, id).DispatchSlots() <= 0 {
				continue
			}
			admitted++
			served[id] = true
			// The job starts and finishes within the sweep, exactly as a short
			// attempt does: the lease records the start and frees the slot.
			rig.served(admissionTenant, id, at)
		}
		if admitted != free {
			t.Fatalf("sweep %d admitted %d projects, want the fleet's %d free slots", sweep, admitted, free)
		}
	}
	for _, id := range ids {
		if !served[id] {
			t.Fatalf("%s was never admitted in %d sweeps of %d projects over %d slots; the rotation starves",
				id, sweeps, projects, free)
		}
	}
}

// TestPlan_AnUndeclaredFleetCapacityLeavesTodaysBehaviour: an undeclared capacity
// means nobody counted the deployment's slots, so the fleet axis must not bound
// anything and a project with no quota of its own stays unbounded.
//
// A METERED POOL IS WIRED ONLY TO MAKE A PLAN EXIST. With neither new axis in
// force the controller answers "no plan" at all and the sweeps use their own
// arithmetic — that property has its own test (see
// TestPlan_NoPlanWhenNeitherNewAxisIsInForce) — and this one is about what a plan
// says when the fleet half of it cannot be measured.
func TestPlan_AnUndeclaredFleetCapacityLeavesTodaysBehaviour(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.headroom = &fakePoolHeadroom{}
	rig.project(t, admissionTenant, "quota", 2)
	rig.project(t, admissionTenant, "unlimited", 0)
	rig.queued(admissionTenant, "quota", 10)
	rig.queued(admissionTenant, "unlimited", 10)

	plan, err := rig.controller(t, 0).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.FleetFree != 0 || plan.FleetLimit != 0 {
		t.Fatalf("an undeclared capacity must report no free count: limit=%d free=%d", plan.FleetLimit, plan.FleetFree)
	}
	quota := plan.For(admissionTenant, "quota")
	if !quota.Bounded || quota.DispatchSlots() != 2 {
		t.Fatalf("the project quota must still bound: bounded=%v slots=%d", quota.Bounded, quota.DispatchSlots())
	}
	if quota.Axis != AdmissionAxisProjectQuota {
		t.Fatalf("axis = %s, want project_quota", quota.Axis)
	}
	unlimited := plan.For(admissionTenant, "unlimited")
	if unlimited.Bounded {
		t.Fatal("a project with no quota under an undeclared fleet capacity must be unbounded")
	}
	if unlimited.Axis != AdmissionAxisUnbounded {
		t.Fatalf("axis = %s, want unbounded", unlimited.Axis)
	}
}

// TestPlan_AnUnboundedProjectIsStillHeldByTheFleet: declaring no quota means
// "as much as you can give me", not "ignore the fleet".
func TestPlan_AnUnboundedProjectIsStillHeldByTheFleet(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.project(t, admissionTenant, "unlimited", 0)
	rig.project(t, admissionTenant, "quota", 1)
	rig.queued(admissionTenant, "unlimited", 100)
	rig.queued(admissionTenant, "quota", 1)

	plan, err := rig.controller(t, 3).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	unlimited := plan.For(admissionTenant, "unlimited")
	if !unlimited.Bounded {
		t.Fatal("a declared fleet capacity bounds every project, quota or not")
	}
	if unlimited.DispatchSlots()+plan.For(admissionTenant, "quota").DispatchSlots() > 3 {
		t.Fatalf("the fleet's 3 slots were oversubscribed: unlimited=%d quota=%d",
			unlimited.DispatchSlots(), plan.For(admissionTenant, "quota").DispatchSlots())
	}
}

// =============================================================================
// The project axis
// =============================================================================

// TestPlan_AProjectAtItsOwnQuotaIsRefusedByItsOwnAxis: a project whose quota is
// full must be told that, not told the fleet is full — sending an operator to
// raise the wrong number is the whole cost of getting the axis wrong.
func TestPlan_AProjectAtItsOwnQuotaIsRefusedByItsOwnAxis(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.project(t, admissionTenant, "full", 2)
	rig.project(t, admissionTenant, "other", 5)
	rig.queued(admissionTenant, "full", 5)
	rig.queued(admissionTenant, "other", 5)
	rig.running(t, admissionTenant, "full", 2)

	plan, err := rig.controller(t, 100).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	full := plan.For(admissionTenant, "full")
	if full.DispatchSlots() != 0 {
		t.Fatalf("a project at its quota has no slots: %d", full.DispatchSlots())
	}
	if full.Axis != AdmissionAxisProjectQuota {
		t.Fatalf("axis = %s, want project_quota", full.Axis)
	}
	if full.ProjectInflight != 2 || full.ProjectLimit != 2 {
		t.Fatalf("the decision must carry the numbers it decided from: %+v", full)
	}
}

// TestAdmissionDecision_AdoptionSlotsNetOffTheQueue: the poller may not adopt
// more than the dispatcher could eventually start, so the queue is subtracted.
func TestAdmissionDecision_AdoptionSlotsNetOffTheQueue(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		decision AdmissionDecision
		dispatch int
		adopt    int
	}{
		{
			name:     "room to spare",
			decision: AdmissionDecision{Bounded: true, Entitlement: 5, ProjectPending: 2},
			dispatch: 5, adopt: 3,
		},
		{
			name:     "the queue already claims the whole entitlement",
			decision: AdmissionDecision{Bounded: true, Entitlement: 2, ProjectPending: 2},
			dispatch: 2, adopt: 0,
		},
		{
			name:     "a queue longer than the entitlement is not negative capacity",
			decision: AdmissionDecision{Bounded: true, Entitlement: 1, ProjectPending: 9},
			dispatch: 1, adopt: 0,
		},
		{
			name:     "unbounded answers both questions with the same unbounded count",
			decision: AdmissionDecision{Bounded: false, ProjectPending: 9},
			dispatch: unboundedSlots, adopt: unboundedSlots,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.decision.DispatchSlots(); got != tc.dispatch {
				t.Fatalf("DispatchSlots = %d, want %d", got, tc.dispatch)
			}
			if got := tc.decision.AdoptionSlots(); got != tc.adopt {
				t.Fatalf("AdoptionSlots = %d, want %d", got, tc.adopt)
			}
		})
	}
}

// TestPlan_ANonAutonomousProjectIsNotPlannedFor: the sweeps visit only
// autonomous projects, so planning for the others would divide the fleet among
// claimants that will never ask.
func TestPlan_ANonAutonomousProjectIsNotPlannedFor(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.project(t, admissionTenant, "auto", 10)
	rig.projectWith(t, &Project{
		ID: "manual", TenantID: admissionTenant, Name: "manual", Autonomous: false,
		Quotas: ProjectQuotas{MaxConcurrency: 10},
	})
	rig.queued(admissionTenant, "auto", 10)
	rig.queued(admissionTenant, "manual", 10)

	plan, err := rig.controller(t, 4).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := len(plan.Decisions()); got != 1 {
		t.Fatalf("the plan names %d projects, want only the autonomous one: %+v", got, plan.Decisions())
	}
	if got := plan.For(admissionTenant, "auto").DispatchSlots(); got != 4 {
		t.Fatalf("the only asking project must get the whole fleet: %d of 4", got)
	}
}

// TestFleetPlan_ForAnUnknownProjectIsUnbounded: a project created between the
// plan and the question is admitted as it would have been with no plan at all,
// rather than stalled on a reading that never included it.
func TestFleetPlan_ForAnUnknownProjectIsUnbounded(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.project(t, admissionTenant, "known", 1)
	plan, err := rig.controller(t, 4).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	decision := plan.For(admissionTenant, "appeared-since")
	if decision.Bounded {
		t.Fatalf("an unplanned project must not be bounded by a plan it is not in: %+v", decision)
	}
	if decision.Reason == "" {
		t.Fatal("an unplanned project's decision must say so")
	}

	var absent *FleetPlan
	if absent.For(admissionTenant, "anything").Bounded {
		t.Fatal("a nil plan must not bound anything")
	}
	if absent.Decisions() != nil {
		t.Fatal("a nil plan has no decisions")
	}
}

// =============================================================================
// The provider axis
// =============================================================================

// TestPlan_DefersWhenNoAccountHasUsageHeadroom is the issue's second acceptance
// check: rather than spawn an agent whose first turn the provider will refuse,
// admission defers and says when the answer changes.
func TestPlan_DefersWhenNoAccountHasUsageHeadroom(t *testing.T) {
	t.Parallel()
	retryAt := admissionClock.Add(90 * time.Minute)
	rig := newAdmissionRig(t)
	rig.headroom = &fakePoolHeadroom{byProject: map[string]PoolHeadroom{
		"spent": {Pooled: 2, Ready: 0, RetryAt: retryAt, Reason: "both accounts are in a cooldown"},
	}}
	rig.project(t, admissionTenant, "spent", 5)
	rig.project(t, admissionTenant, "funded", 5)
	rig.queued(admissionTenant, "spent", 5)
	rig.queued(admissionTenant, "funded", 5)

	plan, err := rig.controller(t, 8).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	spent := plan.For(admissionTenant, "spent")
	if spent.DispatchSlots() != 0 {
		t.Fatalf("a project whose pool is spent must get no slots: %d", spent.DispatchSlots())
	}
	if !spent.Deferred {
		t.Fatal("a spent pool is a DEFERRAL, not a refusal: nothing is wrong and it clears itself")
	}
	if spent.Axis != AdmissionAxisProviderHeadroom {
		t.Fatalf("axis = %s, want provider_headroom", spent.Axis)
	}
	if !spent.RetryAt.Equal(retryAt) {
		t.Fatalf("RetryAt = %s, want %s", spent.RetryAt, retryAt)
	}
	if spent.Reason != "both accounts are in a cooldown" {
		t.Fatalf("the pool's own reason must reach the decision: %q", spent.Reason)
	}
	funded := plan.For(admissionTenant, "funded")
	if funded.Deferred || funded.DispatchSlots() != 5 {
		t.Fatalf("a project with headroom is unaffected: %+v", funded)
	}
}

// TestPlan_ADeferredProjectReleasesItsShare: deferring must not idle capacity.
// A project that cannot spend anything asks for nothing, so the fleet's slots go
// to a project that can use them.
func TestPlan_ADeferredProjectReleasesItsShare(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.headroom = &fakePoolHeadroom{byProject: map[string]PoolHeadroom{
		"spent": {Pooled: 1, Ready: 0, Reason: "spent"},
	}}
	rig.project(t, admissionTenant, "spent", 10)
	rig.project(t, admissionTenant, "funded", 10)
	rig.queued(admissionTenant, "spent", 10)
	rig.queued(admissionTenant, "funded", 10)

	plan, err := rig.controller(t, 6).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := plan.For(admissionTenant, "funded").DispatchSlots(); got != 6 {
		t.Fatalf("the deferred project's share must be redistributed: funded got %d of 6", got)
	}
}

// TestPlan_AnEmptyAccountPoolBoundsNothing: a deployment that pools no accounts
// must not have every project deferred. See PoolHeadroom.Unmetered.
func TestPlan_AnEmptyAccountPoolBoundsNothing(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.headroom = &fakePoolHeadroom{byProject: map[string]PoolHeadroom{
		"unpooled": {Pooled: 0, Ready: 0},
	}}
	rig.project(t, admissionTenant, "unpooled", 3)
	rig.queued(admissionTenant, "unpooled", 3)

	plan, err := rig.controller(t, 10).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	decision := plan.For(admissionTenant, "unpooled")
	if decision.Deferred {
		t.Fatal("an empty pool is not a spent pool and must not defer")
	}
	if decision.DispatchSlots() != 3 {
		t.Fatalf("DispatchSlots = %d, want the project's quota of 3", decision.DispatchSlots())
	}
}

// TestPlan_NoHeadroomReaderLeavesTheAxisOut: a deployment with no reader wired
// behaves exactly as one with an empty pool.
func TestPlan_NoHeadroomReaderLeavesTheAxisOut(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.project(t, admissionTenant, "p", 3)
	rig.queued(admissionTenant, "p", 3)
	plan, err := rig.controller(t, 10).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if decision := plan.For(admissionTenant, "p"); decision.Deferred || decision.DispatchSlots() != 3 {
		t.Fatalf("with no reader the axis must bound nothing: %+v", decision)
	}
}

// =============================================================================
// Fail closed
// =============================================================================

// TestPlan_FailsClosedOnEveryUnreadableInput: a plan that could not measure the
// fleet returns an error and NO plan. A partial plan would divide the fleet
// among the projects it could see and hand each of them more than it is owed.
func TestPlan_FailsClosedOnEveryUnreadableInput(t *testing.T) {
	t.Parallel()
	down := errors.New("kv down")

	cases := map[string]func(*testing.T) *AdmissionController{
		"the fleet's leases": func(t *testing.T) *AdmissionController {
			rig := newAdmissionRig(t)
			rig.project(t, admissionTenant, "p", 1)
			rig.leases.err = down
			return rig.controller(t, 4)
		},
		"the tenant list": func(t *testing.T) *AdmissionController {
			rig := newAdmissionRig(t)
			rig.tenants.err = down
			return rig.controller(t, 4)
		},
		"a tenant's projects": func(t *testing.T) *AdmissionController {
			rig := newAdmissionRig(t)
			controller, err := NewAdmissionController(AdmissionControllerConfig{
				Tenants: rig.tenants, Projects: &failingProjectLister{err: down},
				Queue: rig.queue, Leases: rig.leases, LeaseTTL: testPollerLeaseTTL, Capacity: declaredCapacity(t, 4),
			})
			if err != nil {
				t.Fatalf("NewAdmissionController: %v", err)
			}
			return controller
		},
		"a project's queue": func(t *testing.T) *AdmissionController {
			rig := newAdmissionRig(t)
			rig.project(t, admissionTenant, "p", 1)
			controller, err := NewAdmissionController(AdmissionControllerConfig{
				Tenants: rig.tenants, Projects: rig.projects, Queue: &failingQueue{err: down},
				Leases: rig.leases, LeaseTTL: testPollerLeaseTTL, Capacity: declaredCapacity(t, 4),
			})
			if err != nil {
				t.Fatalf("NewAdmissionController: %v", err)
			}
			return controller
		},
		"a project's provider headroom": func(t *testing.T) *AdmissionController {
			rig := newAdmissionRig(t)
			rig.headroom = &fakePoolHeadroom{err: down}
			rig.project(t, admissionTenant, "p", 1)
			return rig.controller(t, 4)
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			plan, err := build(t).Plan(context.Background())
			if plan != nil {
				t.Fatalf("%s: an unmeasurable fleet must yield NO plan, got %+v", name, plan)
			}
			if !errors.Is(err, ErrAdmissionUndecidable) {
				t.Fatalf("%s: want ErrAdmissionUndecidable, got %v", name, err)
			}
			if !errors.Is(err, down) {
				t.Fatalf("%s: the cause must survive for an operator: %v", name, err)
			}
			if err.Error() == "" {
				t.Fatalf("%s: the error must name which input failed", name)
			}
		})
	}
}

// TestPlan_HonoursACancelledContext: a sweep whose context ends mid-plan stops
// rather than finishing a plan nobody will use.
func TestPlan_HonoursACancelledContext(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.project(t, admissionTenant, "p", 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := rig.controller(t, 4).Plan(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Plan on a cancelled context: %v", err)
	}
}

// =============================================================================
// The division, unit-tested directly
// =============================================================================

// TestFairShares covers the arithmetic the plan depends on, including the two
// degenerate inputs a caller can reach: nothing free, and nobody asking.
func TestFairShares(t *testing.T) {
	t.Parallel()
	demand := func(id string, ask, live int) *projectDemand {
		return &projectDemand{
			demand: ask, live: live,
			decision: AdmissionDecision{TenantID: admissionTenant, ProjectID: id},
		}
	}

	cases := []struct {
		name    string
		demands []*projectDemand
		free    int
		want    []int
	}{
		{
			name:    "everybody's demand fits",
			demands: []*projectDemand{demand("a", 1, 0), demand("b", 2, 0)},
			free:    10,
			want:    []int{1, 2},
		},
		{
			name:    "an even split when demands are equal",
			demands: []*projectDemand{demand("a", 5, 0), demand("b", 5, 0)},
			free:    4,
			want:    []int{2, 2},
		},
		{
			name:    "the small ask is satisfied and its surplus redistributed",
			demands: []*projectDemand{demand("a", 1, 0), demand("b", 9, 0)},
			free:    6,
			want:    []int{1, 5},
		},
		{
			name:    "an odd residual goes to the least-served",
			demands: []*projectDemand{demand("busy", 5, 3), demand("idle", 5, 0)},
			free:    3,
			want:    []int{1, 2},
		},
		{
			name:    "nothing free gives nobody anything",
			demands: []*projectDemand{demand("a", 5, 0)},
			free:    0,
			want:    []int{0},
		},
		{
			name:    "a negative free count is not headroom in the other direction",
			demands: []*projectDemand{demand("a", 5, 0)},
			free:    -4,
			want:    []int{0},
		},
		{
			name:    "nobody asking claims nothing",
			demands: []*projectDemand{demand("a", 0, 0), demand("b", 0, 2)},
			free:    8,
			want:    []int{0, 0},
		},
		{
			name:    "no claimants at all",
			demands: nil,
			free:    8,
			want:    []int{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fairShares(tc.demands, tc.free)
			if len(got) != len(tc.want) {
				t.Fatalf("fairShares returned %d shares, want %d", len(got), len(tc.want))
			}
			total := 0
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("fairShares = %v, want %v", got, tc.want)
				}
				total += got[i]
			}
			if tc.free > 0 && total > tc.free {
				t.Fatalf("fairShares handed out %d of %d free slots", total, tc.free)
			}
		})
	}
}

// TestFairShares_NeverOversubscribesForAnyShape is a property check over the
// whole reachable input space at small sizes: whatever the demands, the sum of
// the shares never exceeds the free slots and no share exceeds its demand.
//
// It exists because the safety property is arithmetic and the arithmetic has an
// integer-flooring residual pass, which is exactly the shape that leaks one slot
// under some combination nobody thought to enumerate by hand.
func TestFairShares_NeverOversubscribesForAnyShape(t *testing.T) {
	t.Parallel()
	const maxDemand = 4
	for free := 0; free <= 6; free++ {
		for a := 0; a <= maxDemand; a++ {
			for b := 0; b <= maxDemand; b++ {
				for c := 0; c <= maxDemand; c++ {
					demands := []*projectDemand{
						{demand: a, decision: AdmissionDecision{ProjectID: "a"}},
						{demand: b, decision: AdmissionDecision{ProjectID: "b"}},
						{demand: c, decision: AdmissionDecision{ProjectID: "c"}},
					}
					shares := fairShares(demands, free)
					total := 0
					for i, share := range shares {
						if share < 0 {
							t.Fatalf("free=%d demands=%v: negative share %d", free, []int{a, b, c}, share)
						}
						if share > demands[i].demand {
							t.Fatalf("free=%d demands=%v: share %d exceeds demand %d",
								free, []int{a, b, c}, share, demands[i].demand)
						}
						total += share
					}
					if total > free {
						t.Fatalf("free=%d demands=%v: handed out %d", free, []int{a, b, c}, total)
					}
					// No slot may be idled while somebody still wants one.
					wanted := a + b + c
					if wanted > free && total != free {
						t.Fatalf("free=%d demands=%v: %d slots idled while demand was %d",
							free, []int{a, b, c}, free-total, wanted)
					}
					if wanted <= free && total != wanted {
						t.Fatalf("free=%d demands=%v: satisfiable demand was not satisfied: %d of %d",
							free, []int{a, b, c}, total, wanted)
					}
				}
			}
		}
	}
}

// TestLessServed orders projects by how little the fleet has served them, with
// the project id as the last resort so the order is total.
func TestLessServed(t *testing.T) {
	t.Parallel()
	mk := func(tenant, id string, live int, start int64) *projectDemand {
		return &projectDemand{
			live: live, lastStart: start,
			decision: AdmissionDecision{TenantID: tenant, ProjectID: id},
		}
	}
	cases := []struct {
		name        string
		left, right *projectDemand
		want        bool
	}{
		{"fewer live jobs first", mk("t", "a", 0, 500), mk("t", "b", 3, 1), true},
		{"more live jobs last", mk("t", "a", 3, 1), mk("t", "b", 0, 500), false},
		{"older start first", mk("t", "a", 1, 10), mk("t", "b", 1, 20), true},
		{"newer start last", mk("t", "a", 1, 20), mk("t", "b", 1, 10), false},
		{"tenant breaks a full tie", mk("t1", "a", 1, 5), mk("t2", "a", 1, 5), true},
		{"project id is the last resort", mk("t", "a", 1, 5), mk("t", "b", 1, 5), true},
		{"and is not reflexive", mk("t", "b", 1, 5), mk("t", "a", 1, 5), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lessServed(tc.left, tc.right); got != tc.want {
				t.Fatalf("lessServed = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestFleetPlan_DecisionsAreOrdered: two plans over the same state must produce
// comparable reports, so a diff between sweeps is a change and not a map walk.
func TestFleetPlan_DecisionsAreOrdered(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t, admissionTenant2, admissionTenant)
	rig.project(t, admissionTenant, "zulu", 1)
	rig.project(t, admissionTenant, "alpha", 1)
	rig.project(t, admissionTenant2, "mike", 1)

	plan, err := rig.controller(t, 9).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	got := plan.Decisions()
	want := []string{
		admissionTenant + "/alpha", admissionTenant + "/zulu", admissionTenant2 + "/mike",
	}
	if len(got) != len(want) {
		t.Fatalf("Decisions returned %d, want %d", len(got), len(want))
	}
	for i, decision := range got {
		if key := projectPlanKey(decision.TenantID, decision.ProjectID); key != want[i] {
			t.Fatalf("Decisions[%d] = %s, want %s", i, key, want[i])
		}
	}
}

// TestAdmissionAxis_String covers the axis names the log lines and the refusal
// reasons are built from.
func TestAdmissionAxis_String(t *testing.T) {
	t.Parallel()
	for _, axis := range []AdmissionAxis{
		AdmissionAxisUnbounded, AdmissionAxisProjectQuota,
		AdmissionAxisFleetCapacity, AdmissionAxisProviderHeadroom,
	} {
		if axis.String() != string(axis) {
			t.Fatalf("String() of %q = %q", axis, axis.String())
		}
	}
	if AdmissionAxis("invented").String() != "invented" {
		t.Fatal("an unknown axis must render as itself rather than as a substitute")
	}
}

// TestPoolHeadroom_UnmeteredAndHeadroom covers the two predicates the provider
// axis branches on, including the distinction an empty pool turns on.
func TestPoolHeadroom_UnmeteredAndHeadroom(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		headroom  PoolHeadroom
		unmetered bool
		has       bool
	}{
		{"no account pooled at all", PoolHeadroom{}, true, true},
		{"pooled and ready", PoolHeadroom{Pooled: 2, Ready: 1}, false, true},
		{"pooled and spent", PoolHeadroom{Pooled: 2, Ready: 0}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.headroom.Unmetered(); got != tc.unmetered {
				t.Fatalf("Unmetered = %v, want %v", got, tc.unmetered)
			}
			if got := tc.headroom.HasHeadroom(); got != tc.has {
				t.Fatalf("HasHeadroom = %v, want %v", got, tc.has)
			}
		})
	}
}

// TestPoolHeadroom_NoteRetryKeepsTheEarliestStatedTime: a zero is not a time,
// and treating one as the epoch would report every deferral as clearing in the
// past — which reads downstream as "retry now" and turns a wait into a spin.
func TestPoolHeadroom_NoteRetryKeepsTheEarliestStatedTime(t *testing.T) {
	t.Parallel()
	var headroom PoolHeadroom
	headroom.noteRetry(time.Time{})
	if !headroom.RetryAt.IsZero() {
		t.Fatalf("an unstated return time must leave RetryAt zero, got %s", headroom.RetryAt)
	}
	late := admissionClock.Add(time.Hour)
	early := admissionClock.Add(time.Minute)
	headroom.noteRetry(late)
	headroom.noteRetry(early)
	headroom.noteRetry(late)
	headroom.noteRetry(time.Time{})
	if !headroom.RetryAt.Equal(early) {
		t.Fatalf("RetryAt = %s, want the earliest stated %s", headroom.RetryAt, early)
	}
}

// =============================================================================
// Errors
// =============================================================================

func TestAdmissionConfigError(t *testing.T) {
	t.Parallel()
	// A bare field name is the shape a caller that had nothing to add uses.
	bare := &AdmissionConfigError{Field: "leases"}
	if !errors.Is(bare, ErrAdmissionConfig) {
		t.Fatal("must unwrap to ErrAdmissionConfig")
	}
	if !strings.Contains(bare.Error(), "leases") || !strings.Contains(bare.Error(), "required") {
		t.Fatalf("a bare field error must name the field and say it is required: %q", bare.Error())
	}
	explained := &AdmissionConfigError{Field: "capacity", Message: "must not be negative"}
	if !strings.Contains(explained.Error(), "capacity") || !strings.Contains(explained.Error(), "negative") {
		t.Fatalf("an explained error must carry both: %q", explained.Error())
	}
}

func TestAdmissionUndecidableError(t *testing.T) {
	t.Parallel()
	cause := errors.New("kv down")
	withCause := &AdmissionUndecidableError{Input: "fleet_leases", Cause: cause}
	if !errors.Is(withCause, ErrAdmissionUndecidable) || !errors.Is(withCause, cause) {
		t.Fatal("must unwrap to both the sentinel and the cause")
	}
	if !strings.Contains(withCause.Error(), "fleet_leases") || !strings.Contains(withCause.Error(), "kv down") {
		t.Fatalf("must name the input and the cause: %q", withCause.Error())
	}
	// A CAUSELESS UNDECIDABLE IS STILL UNDECIDABLE. errors.Is must not fall
	// through a nil entry in the unwrap list.
	bare := &AdmissionUndecidableError{Input: "tenants"}
	if !errors.Is(bare, ErrAdmissionUndecidable) {
		t.Fatal("a causeless error must still match the sentinel")
	}
	if !strings.Contains(bare.Error(), "tenants") {
		t.Fatalf("must name the input: %q", bare.Error())
	}
	if got := bare.Unwrap(); len(got) != 1 || got[0] != ErrAdmissionUndecidable {
		t.Fatalf("Unwrap = %v, want just the sentinel", got)
	}
}

// TestPlan_ToleratesANilLeaseAndANilProject: a store that lists a nil entry must
// not panic a sweep. Both lists are read in loops that have to survive one.
func TestPlan_ToleratesANilLeaseAndANilProject(t *testing.T) {
	t.Parallel()
	rig := newAdmissionRig(t)
	rig.project(t, admissionTenant, "p", 2)
	rig.queued(admissionTenant, "p", 2)
	rig.leases.records = append(rig.leases.records, nil)

	plan, err := rig.controller(t, 4).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.FleetInflight != 0 {
		t.Fatalf("a nil lease holds nothing: %d", plan.FleetInflight)
	}
	if got := plan.For(admissionTenant, "p").DispatchSlots(); got != 2 {
		t.Fatalf("DispatchSlots = %d, want 2", got)
	}
}
