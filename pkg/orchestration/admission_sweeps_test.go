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
// The two enforcement points consult the one plan
// =============================================================================
//
// The controller's own arithmetic is covered in admission_test.go. What is
// covered here is that both sweeps ASK it — the gap the issue names, since the
// controller could be perfectly correct and still enforce nothing if either
// sweep kept its own bound.

// twoProjectSweepRig is one tenant with two autonomous projects, each with its
// own quota and its own queue, over stores both sweeps share.
type twoProjectSweepRig struct {
	tenants  *fakeTenantLister
	projects ProjectStore
	tasks    *fakeDispatchStore
	leases   *recordedLeaseStore
	spawner  *fakeTaskSpawner
	logger   *controlLoopMockLogger
}

func newTwoProjectSweepRig(t *testing.T, perProjectQuota int) *twoProjectSweepRig {
	t.Helper()
	projects := NewInMemoryProjectStore()
	ctx := ContextWithTenant(context.Background(), admissionTenant)
	for _, id := range []string{"alpha", "bravo"} {
		if err := projects.Create(ctx, &Project{
			ID: id, TenantID: admissionTenant, Name: id, Autonomous: true,
			Quotas: ProjectQuotas{MaxConcurrency: perProjectQuota},
		}); err != nil {
			t.Fatalf("create project %s: %v", id, err)
		}
	}
	return &twoProjectSweepRig{
		tenants:  &fakeTenantLister{ids: []string{admissionTenant}},
		projects: projects,
		tasks:    newFakeDispatchStore(),
		leases:   &recordedLeaseStore{},
		spawner:  &fakeTaskSpawner{},
		logger:   &controlLoopMockLogger{},
	}
}

// queue adds n pending, auto-spawning tasks to a project.
func (r *twoProjectSweepRig) queue(projectID string, n int) {
	for i := 0; i < n; i++ {
		id := projectID + "-t" + strconv.Itoa(i)
		r.tasks.tasks[id] = &Task{
			ID: id, TenantID: admissionTenant, ProjectID: projectID, Subject: id,
			Status: TaskStatusPending, AutoSpawn: true, Version: 1,
			CreatedAt: int64(i),
		}
	}
}

func (r *twoProjectSweepRig) controller(t *testing.T, capacity int, headroom PoolHeadroomReader) *AdmissionController {
	t.Helper()
	controller, err := NewAdmissionController(AdmissionControllerConfig{
		Tenants: r.tenants, Projects: r.projects, Queue: r.tasks, Leases: r.leases,
		LeaseTTL: testPollerLeaseTTL, Capacity: declaredCapacity(t, capacity), Headroom: headroom,
		Now: func() time.Time { return admissionClock },
	})
	if err != nil {
		t.Fatalf("NewAdmissionController: %v", err)
	}
	return controller
}

func (r *twoProjectSweepRig) dispatcher(t *testing.T, admission *AdmissionController) *TaskDispatcher {
	t.Helper()
	dispatcher, err := NewTaskDispatcher(TaskDispatcherConfig{
		Tenants: r.tenants, Projects: r.projects, Tasks: r.tasks, Leases: r.leases,
		LeaseTTL: testPollerLeaseTTL, Spawner: r.spawner, Admission: admission, Logger: r.logger,
	})
	if err != nil {
		t.Fatalf("NewTaskDispatcher: %v", err)
	}
	dispatcher.now = func() time.Time { return admissionClock }
	return dispatcher
}

// =============================================================================
// The dispatcher
// =============================================================================

// TestTaskDispatcher_TwoProjectsWithinTheirQuotasCannotExceedTheFleet is the
// issue's first acceptance check at the enforcement point rather than in the
// controller: both projects are inside their own quota of three, and the fleet
// has two slots, so the sweep must start two jobs and not six.
func TestTaskDispatcher_TwoProjectsWithinTheirQuotasCannotExceedTheFleet(t *testing.T) {
	t.Parallel()
	rig := newTwoProjectSweepRig(t, 3)
	rig.queue("alpha", 3)
	rig.queue("bravo", 3)

	dispatcher := rig.dispatcher(t, rig.controller(t, 2, nil))
	if err := dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := len(rig.spawner.dispatched()); got != 2 {
		t.Fatalf("two projects each inside a quota of 3 started %d jobs on a fleet of 2", got)
	}
}

// TestTaskDispatcher_WithoutAControllerTheProjectQuotaStillBounds: the axis is
// optional and its absence must leave today's behaviour exactly. Two projects at
// three each start six.
func TestTaskDispatcher_WithoutAControllerTheProjectQuotaStillBounds(t *testing.T) {
	t.Parallel()
	rig := newTwoProjectSweepRig(t, 3)
	rig.queue("alpha", 5)
	rig.queue("bravo", 5)

	dispatcher := rig.dispatcher(t, nil)
	if err := dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := len(rig.spawner.dispatched()); got != 6 {
		t.Fatalf("with no fleet axis each project's quota of 3 must still apply: started %d, want 6", got)
	}
}

// TestTaskDispatcher_AControllerWithNeitherNewAxisBehavesLikeNoController proves
// the plan-suppression path end to end: a controller is wired, but the fleet is
// unmeasured and no pool is metered, so the sweep falls back to the per-project
// arithmetic and starts exactly what it started before admission existed.
func TestTaskDispatcher_AControllerWithNeitherNewAxisBehavesLikeNoController(t *testing.T) {
	t.Parallel()
	rig := newTwoProjectSweepRig(t, 3)
	rig.queue("alpha", 5)
	rig.queue("bravo", 5)

	dispatcher := rig.dispatcher(t, rig.controller(t, 0, nil))
	if err := dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := len(rig.spawner.dispatched()); got != 6 {
		t.Fatalf("an unmeasured fleet and no metered pool must leave the project quotas alone: started %d, want 6", got)
	}
}

// TestTaskDispatcher_RecordsTheAxisThatRefused: a task not started must carry
// the reason from the axis that decided, so an operator raises the right number.
func TestTaskDispatcher_RecordsTheAxisThatRefused(t *testing.T) {
	t.Parallel()
	rig := newTwoProjectSweepRig(t, 10)
	rig.queue("alpha", 2)
	rig.queue("bravo", 2)

	dispatcher := rig.dispatcher(t, rig.controller(t, 1, nil))
	if err := dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	refusals := 0
	for _, task := range rig.tasks.tasks {
		if task.DispatchReason == "" {
			continue
		}
		refusals++
		// TWO HONEST SHAPES, and the difference matters to whoever reads it: a
		// project handed no slots at all was NOT ADMITTED and the axis says
		// which number refused it, while a project handed some slots WAS
		// admitted and simply had more ready tasks than slots. Collapsing the
		// second into the first would tell an operator to raise a limit that
		// is not in the way.
		named := strings.Contains(task.DispatchReason, "not admitted") &&
			strings.Contains(task.DispatchReason, string(AdmissionAxisFleetCapacity))
		short := strings.Contains(task.DispatchReason, "more ready tasks than")
		if !named && !short {
			t.Fatalf("a refusal must name the axis that declined it or the slot shortfall: %q", task.DispatchReason)
		}
	}
	if refusals == 0 {
		t.Fatal("with one fleet slot and four ready tasks, something must carry a refusal")
	}
}

// TestTaskDispatcher_DefersAProjectWhoseProviderPoolIsSpent is the issue's second
// acceptance check at the enforcement point: rather than spawn an agent whose
// first turn the provider will refuse, nothing is started for that project and
// the reason names the provider axis.
func TestTaskDispatcher_DefersAProjectWhoseProviderPoolIsSpent(t *testing.T) {
	t.Parallel()
	rig := newTwoProjectSweepRig(t, 5)
	rig.queue("alpha", 2)
	rig.queue("bravo", 2)
	headroom := &fakePoolHeadroom{byProject: map[string]PoolHeadroom{
		"alpha": {Pooled: 1, Ready: 0, RetryAt: admissionClock.Add(time.Hour), Reason: "the only pooled account is in a cooldown"},
	}}

	dispatcher := rig.dispatcher(t, rig.controller(t, 100, headroom))
	if err := dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	for _, task := range rig.spawner.dispatched() {
		if strings.HasPrefix(task, "alpha-") {
			t.Fatalf("a project with no provider headroom must not spawn: started %s", task)
		}
	}
	if got := len(rig.spawner.dispatched()); got != 2 {
		t.Fatalf("the project WITH headroom must still run: started %d, want bravo's 2", got)
	}
	deferred := 0
	for _, task := range rig.tasks.tasks {
		if task.ProjectID != "alpha" {
			continue
		}
		if !strings.Contains(task.DispatchReason, string(AdmissionAxisProviderHeadroom)) {
			t.Fatalf("alpha's tasks must name the provider axis: %q", task.DispatchReason)
		}
		if !strings.Contains(task.DispatchReason, "cooldown") {
			t.Fatalf("the pool's own reason must reach the task: %q", task.DispatchReason)
		}
		deferred++
	}
	if deferred != 2 {
		t.Fatalf("both of alpha's tasks must carry the deferral: %d", deferred)
	}
}

// TestTaskDispatcher_AnUnmeasurableFleetEndsTheSweep: falling back to the project
// axis when the fleet cannot be measured would oversubscribe precisely when the
// fleet is under stress, since that is when a store stops answering.
func TestTaskDispatcher_AnUnmeasurableFleetEndsTheSweep(t *testing.T) {
	t.Parallel()
	down := errors.New("kv down")
	rig := newTwoProjectSweepRig(t, 3)
	rig.queue("alpha", 3)
	controller := rig.controller(t, 4, nil)
	rig.leases.err = down

	err := rig.dispatcher(t, controller).Sweep(context.Background())
	if !errors.Is(err, ErrTaskDispatchSweep) || !errors.Is(err, ErrAdmissionUndecidable) {
		t.Fatalf("Sweep: want a sweep failure carrying ErrAdmissionUndecidable, got %v", err)
	}
	if got := rig.spawner.dispatched(); len(got) != 0 {
		t.Fatalf("nothing may be dispatched against a fleet nobody measured: %v", got)
	}
}

// TestTaskDispatcher_SweepProjectAlsoConsultsTheFleet: the push path is the same
// enforcement point. A nudge that ignored the fleet would be the bypass.
func TestTaskDispatcher_SweepProjectAlsoConsultsTheFleet(t *testing.T) {
	t.Parallel()
	rig := newTwoProjectSweepRig(t, 5)
	rig.queue("alpha", 5)
	// The fleet's two slots are already held by the other project.
	rig.leases.records = append(rig.leases.records,
		&JobLease{
			TenantID: admissionTenant, ProjectID: "bravo", Workflow: "sdlc", Ref: "bravo#1",
			JobID: "bravo-1", Generation: 1,
			AcquiredAt: admissionClock.UnixNano(), HeartbeatAt: admissionClock.UnixNano(),
		},
		&JobLease{
			TenantID: admissionTenant, ProjectID: "bravo", Workflow: "sdlc", Ref: "bravo#2",
			JobID: "bravo-2", Generation: 1,
			AcquiredAt: admissionClock.UnixNano(), HeartbeatAt: admissionClock.UnixNano(),
		})

	dispatcher := rig.dispatcher(t, rig.controller(t, 2, nil))
	if err := dispatcher.SweepProject(context.Background(), admissionTenant, "alpha"); err != nil {
		t.Fatalf("SweepProject: %v", err)
	}
	if got := rig.spawner.dispatched(); len(got) != 0 {
		t.Fatalf("a nudge into a full fleet must start nothing: %v", got)
	}
}

// TestDispatchReasonAdmission renders every shape of decision a refusal can come
// from, including the one with slots left.
func TestDispatchReasonAdmission(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		decision AdmissionDecision
		contains string
	}{
		{
			name: "the decision's own reason wins",
			decision: AdmissionDecision{
				Bounded: true, Axis: AdmissionAxisProviderHeadroom, Reason: "every account is spent",
			},
			contains: "every account is spent",
		},
		{
			name:     "no slots and no reason still names the axis",
			decision: AdmissionDecision{Bounded: true, Axis: AdmissionAxisFleetCapacity},
			contains: string(AdmissionAxisFleetCapacity),
		},
		{
			name:     "slots left means there were simply more ready tasks",
			decision: AdmissionDecision{Bounded: true, Entitlement: 2, Axis: AdmissionAxisFleetCapacity},
			contains: "more ready tasks than the 2 slot(s)",
		},
		{
			name:     "an unbounded project can only run short of slots",
			decision: AdmissionDecision{Bounded: false, Axis: AdmissionAxisUnbounded},
			contains: "more ready tasks",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dispatchReasonAdmission(tc.decision)
			if got == "" {
				t.Fatal("a refusal recorded on a task must never be empty")
			}
			if !strings.Contains(got, tc.contains) {
				t.Fatalf("dispatchReasonAdmission = %q, want it to contain %q", got, tc.contains)
			}
		})
	}
}

// =============================================================================
// The poller
// =============================================================================

// TestTaskSourcePoller_AdoptionIsBoundedByTheFleetNotOnlyTheProject: the poller
// must not fill the queue with work the fleet cannot drain. Its capacity is the
// project's fleet entitlement less what its queue already claims.
func TestTaskSourcePoller_AdoptionIsBoundedByTheFleetNotOnlyTheProject(t *testing.T) {
	t.Parallel()
	fixture := newPollerFixture(t)
	fixture.setMaxConcurrency(t, "proj-flow", 10)

	// Five issues on offer, a fleet of two slots, and one project asking.
	specs := make([]TaskSourceSpec, 0, 5)
	for i := 0; i < 5; i++ {
		specs = append(specs, TaskSourceSpec{
			Source:  TaskSourceRef{Kind: "tracker-fleet", Ref: "issue-" + strconv.Itoa(i)},
			Subject: "issue " + strconv.Itoa(i),
		})
	}
	fixture.register(t, "tracker-fleet", &fakeTaskSource{name: "fleet", specs: specs})

	admission, err := NewAdmissionController(AdmissionControllerConfig{
		Tenants:  &fakeTenantLister{ids: []string{"tenant-a"}},
		Projects: fixture.projects,
		Queue:    fixture.creator.store,
		Leases:   fixture.leases,
		LeaseTTL: testPollerLeaseTTL,
		Capacity: declaredCapacity(t, 2),
	})
	if err != nil {
		t.Fatalf("NewAdmissionController: %v", err)
	}
	fixture.poller.admission = admission

	if err := fixture.poller.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := len(fixture.tasks(t)); got != 2 {
		t.Fatalf("a project with a quota of 10 on a fleet of 2 adopted %d issues, want 2", got)
	}
}

// TestTaskSourcePoller_AnUnmeasurableFleetEndsTheSweep: adopting against the
// project axis alone because the fleet could not be measured would flood the
// queue exactly when the fleet is least able to drain it.
func TestTaskSourcePoller_AnUnmeasurableFleetEndsTheSweep(t *testing.T) {
	t.Parallel()
	down := errors.New("kv down")
	fixture := newPollerFixture(t)
	fixture.setMaxConcurrency(t, "proj-flow", 5)
	fixture.register(t, "tracker-down", &fakeTaskSource{
		name:  "down",
		specs: []TaskSourceSpec{{Source: TaskSourceRef{Kind: "tracker-down", Ref: "issue-1"}, Subject: "s"}},
	})
	admission, err := NewAdmissionController(AdmissionControllerConfig{
		Tenants:  &fakeTenantLister{ids: []string{"tenant-a"}},
		Projects: fixture.projects,
		Queue:    fixture.creator.store,
		Leases:   &failingLeaseLister{err: down},
		LeaseTTL: testPollerLeaseTTL,
		Capacity: declaredCapacity(t, 2),
	})
	if err != nil {
		t.Fatalf("NewAdmissionController: %v", err)
	}
	fixture.poller.admission = admission

	sweepErr := fixture.poller.Sweep(context.Background())
	if !errors.Is(sweepErr, ErrTaskSourceSweep) || !errors.Is(sweepErr, ErrAdmissionUndecidable) {
		t.Fatalf("Sweep: want a sweep failure carrying ErrAdmissionUndecidable, got %v", sweepErr)
	}
	if got := len(fixture.tasks(t)); got != 0 {
		t.Fatalf("nothing may be adopted against a fleet nobody measured: %d tasks", got)
	}
	if err := fixture.poller.SweepProject(context.Background(), "tenant-a", "proj-flow"); !errors.Is(err, ErrAdmissionUndecidable) {
		t.Fatalf("SweepProject must fail closed too: %v", err)
	}
}

// TestTaskSourcePoller_WithoutAControllerTheProjectQuotaStillBounds: the axis is
// optional and its absence leaves the pre-existing arithmetic — the quota, less
// the running jobs, less the queue.
func TestTaskSourcePoller_WithoutAControllerTheProjectQuotaStillBounds(t *testing.T) {
	t.Parallel()
	fixture := newPollerFixture(t)
	fixture.setMaxConcurrency(t, "proj-flow", 2)
	specs := make([]TaskSourceSpec, 0, 5)
	for i := 0; i < 5; i++ {
		specs = append(specs, TaskSourceSpec{
			Source:  TaskSourceRef{Kind: "tracker-noplan", Ref: "issue-" + strconv.Itoa(i)},
			Subject: "issue " + strconv.Itoa(i),
		})
	}
	fixture.register(t, "tracker-noplan", &fakeTaskSource{name: "noplan", specs: specs})

	if err := fixture.poller.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := len(fixture.tasks(t)); got != 2 {
		t.Fatalf("with no fleet axis the project quota of 2 must still bound: adopted %d", got)
	}
}

// =============================================================================
// Parked adoption
// =============================================================================

// TestTaskSourcePoller_ASpecCarryingAParkAdoptsBlockedAndSpawnsNothing is the
// park-and-resume property at the adoption seam: a source that knows the work
// cannot start yet declares the park with the spec, and the task is created
// blocked in ONE write — never pending, so no sweep can start it in between.
func TestTaskSourcePoller_ASpecCarryingAParkAdoptsBlockedAndSpawnsNothing(t *testing.T) {
	t.Parallel()
	fixture := newPollerFixture(t)
	fixture.register(t, "tracker-park", &fakeTaskSource{
		name: "park",
		specs: []TaskSourceSpec{{
			Source:  TaskSourceRef{Kind: "tracker-park", Ref: "issue-blocked"},
			Subject: "blocked issue",
			Park: &BlockReason{
				Kind:    TaskBlockKindBlocked,
				Message: "issue acme/api#7 must be closed unblock[kind=forge_issue_state;subject=acme/api#7;target=closed]",
			},
		}},
	})

	if err := fixture.poller.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	tasks := fixture.tasks(t)
	if len(tasks) != 1 {
		t.Fatalf("one spec must adopt one task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.Status != TaskStatusBlocked {
		t.Fatalf("a parked adoption must land blocked, got %s", task.Status)
	}
	if task.BlockKind != TaskBlockKindBlocked {
		t.Fatalf("BlockKind = %s, want blocked", task.BlockKind)
	}
	if !strings.Contains(task.BlockedReason, "unblock[") {
		t.Fatalf("the typed condition must reach the record so the sweep can clear it: %q", task.BlockedReason)
	}
	// THE AUTO-SPAWN BIT SURVIVES. It is what makes readmission dispatch the
	// task once the condition clears; clearing it here would park the work
	// permanently under a condition that was expected to lapse.
	if !task.AutoSpawn {
		t.Fatal("a parked task must keep its auto-spawn bit, or readmission will never dispatch it")
	}
	if task.SpawnedJobID != "" {
		t.Fatalf("a parked task holds no job: %q", task.SpawnedJobID)
	}
}

// TestTaskDispatcher_AParkedTaskIsNotDispatched: the park holds no session
// because the dispatcher never sees the task — it lists only pending work. This
// is the "parks without holding a session" property, asserted rather than
// assumed.
func TestTaskDispatcher_AParkedTaskIsNotDispatched(t *testing.T) {
	t.Parallel()
	rig := newTwoProjectSweepRig(t, 5)
	rig.tasks.tasks["parked"] = &Task{
		ID: "parked", TenantID: admissionTenant, ProjectID: "alpha", Subject: "parked",
		Status: TaskStatusBlocked, BlockKind: TaskBlockKindBlocked,
		BlockedReason: "waiting on acme/api#7", AutoSpawn: true, Version: 1,
	}

	if err := rig.dispatcher(t, rig.controller(t, 10, nil)).Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := rig.spawner.dispatched(); len(got) != 0 {
		t.Fatalf("a blocked task must not be dispatched: %v", got)
	}
}

// TestApplyCreateTaskPark covers the one-write park's validation, which mirrors
// BlockTask's: a kind from the closed enum and a non-empty reason.
func TestApplyCreateTaskPark(t *testing.T) {
	t.Parallel()
	t.Run("no park leaves the task pending", func(t *testing.T) {
		task := &Task{Status: TaskStatusPending}
		if err := applyCreateTaskPark(task, nil); err != nil {
			t.Fatalf("applyCreateTaskPark(nil): %v", err)
		}
		if task.Status != TaskStatusPending {
			t.Fatalf("Status = %s, want pending", task.Status)
		}
	})

	t.Run("every valid kind parks", func(t *testing.T) {
		for kind := range taskBlockKinds {
			task := &Task{Status: TaskStatusPending}
			if err := applyCreateTaskPark(task, &BlockReason{Kind: kind, Message: "because"}); err != nil {
				t.Fatalf("kind %s: %v", kind, err)
			}
			if task.Status != TaskStatusBlocked || task.BlockKind != kind {
				t.Fatalf("kind %s: status=%s blockKind=%s", kind, task.Status, task.BlockKind)
			}
			if task.BlockedReason != "because" {
				t.Fatalf("kind %s: reason %q", kind, task.BlockedReason)
			}
			if task.ConclusionReason != blockKindConclusions[kind] {
				t.Fatalf("kind %s: a parked task must name itself from the same vocabulary a park does: %s",
					kind, task.ConclusionReason)
			}
		}
	})

	t.Run("an unknown kind is refused", func(t *testing.T) {
		task := &Task{Status: TaskStatusPending}
		err := applyCreateTaskPark(task, &BlockReason{Kind: "invented", Message: "because"})
		if !errors.Is(err, ErrTaskStoreInvalidInput) {
			t.Fatalf("want ErrTaskStoreInvalidInput, got %v", err)
		}
		if task.Status != TaskStatusPending {
			t.Fatalf("a refused park must leave the task alone, got %s", task.Status)
		}
	})

	t.Run("an empty reason is refused", func(t *testing.T) {
		task := &Task{Status: TaskStatusPending}
		err := applyCreateTaskPark(task, &BlockReason{Kind: TaskBlockKindBlocked})
		if !errors.Is(err, ErrTaskStoreInvalidInput) {
			t.Fatalf("want ErrTaskStoreInvalidInput, got %v", err)
		}
		if task.Status != TaskStatusPending {
			t.Fatalf("a refused park must leave the task alone, got %s", task.Status)
		}
	})
}
