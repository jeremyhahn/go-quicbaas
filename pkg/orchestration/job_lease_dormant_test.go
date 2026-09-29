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
	"testing"
	"time"
)

// =============================================================================
// Dormant jobs hold no worker on any axis (go-quicbaas#938, #1036)
// =============================================================================
//
// The owner's ruling: an sdlc job parked in reviewing closes its worker
// container, frees its concurrency slot, and stops counting against fleet
// capacity. The project axis already moved in #978 (OffBudget); these tests
// pin the fleet and backend axes, which read the lease's Dormant flag.

// TestJobLease_WorkerStandingIsReadTheSameOnTheFleetAndBackendAxes pins the
// two predicates both axes answer through.
func TestJobLease_WorkerStandingIsReadTheSameOnTheFleetAndBackendAxes(t *testing.T) {
	now := time.Now().UTC()
	fresh := now.UnixNano()
	stale := now.Add(-2 * leaseTestTTL).UnixNano()
	tests := []struct {
		name       string
		lease      *JobLease
		wantClaims bool
		wantHolds  bool
	}{
		{"a live worker holds one", &JobLease{HeartbeatAt: fresh}, true, true},
		{"a review off the project budget still holds a worker", &JobLease{HeartbeatAt: fresh, OffBudget: true}, true, true},
		{"a dormant job holds none", &JobLease{HeartbeatAt: fresh, OffBudget: true, Dormant: true}, false, false},
		{"a dormant job queued for a worker claims one but holds none", &JobLease{HeartbeatAt: fresh, OffBudget: true, Dormant: true, AwaitingSlot: true}, true, false},
		{"a released lease is nothing", &JobLease{HeartbeatAt: fresh, Released: true}, false, false},
		{"a stale queued lease is nothing", &JobLease{HeartbeatAt: stale, Dormant: true, AwaitingSlot: true}, false, false},
		{"no lease is nothing", nil, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.lease.ClaimsWorker(now, leaseTestTTL); got != tc.wantClaims {
				t.Errorf("ClaimsWorker = %v, want %v", got, tc.wantClaims)
			}
			if got := tc.lease.HoldsWorker(now, leaseTestTTL); got != tc.wantHolds {
				t.Errorf("HoldsWorker = %v, want %v", got, tc.wantHolds)
			}
		})
	}
}

// dormant moves a held lease to the dormant standing an sdlc job takes in
// reviewing, as JobRun.rebudget writes it.
func dormant(t *testing.T, store JobLeaseStore, lease *JobLease, awaiting bool) *JobLease {
	t.Helper()
	move := *lease
	move.OffBudget, move.Dormant, move.AwaitingSlot = true, true, awaiting
	if err := store.Rebudget(context.Background(), &move); err != nil {
		t.Fatalf("going dormant must never be refused: %v", err)
	}
	return &move
}

// woken is the move back onto a worker a review round asks for.
func woken(lease *JobLease) *JobLease {
	next := *lease
	next.OffBudget, next.Dormant, next.AwaitingSlot = false, false, false
	return &next
}

// TestFleetCapacity_NDormantJobsAtCapacityNStillAdmitNewWork is #1036's
// acceptance test at the fleet check: a fleet of two slots, both jobs parked in
// reviewing, admits two more — and a waking round then waits its turn.
func TestFleetCapacity_NDormantJobsAtCapacityNStillAdmitNewWork(t *testing.T) {
	ctx := context.Background()
	rig := newFleetLeaseRig(t, 2)
	lease := func(ref string) *JobLease {
		return &JobLease{TenantID: "acme", ProjectID: "p", Ref: ref, JobID: ref, Generation: 1}
	}

	first, second := lease("review-1"), lease("review-2")
	for _, l := range []*JobLease{first, second} {
		if err := rig.store.Acquire(ctx, l); err != nil {
			t.Fatalf("acquire %s: %v", l.JobID, err)
		}
	}
	parked := dormant(t, rig.store, first, false)
	dormant(t, rig.store, second, false)

	for _, ref := range []string{"new-1", "new-2"} {
		if err := rig.store.Acquire(ctx, lease(ref)); err != nil {
			t.Fatalf("a fleet whose every job is dormant refused %s: %v", ref, err)
		}
	}

	// The fleet is now full of workers, so the waking round is refused...
	var full *FleetCapacityExceededError
	if err := rig.store.Rebudget(ctx, woken(parked)); !errors.As(err, &full) {
		t.Fatalf("waking on a full fleet = %v, want FleetCapacityExceededError", err)
	}
	if full.Live != 2 || full.Limit != 2 {
		t.Fatalf("refusal live=%d limit=%d, want 2/2", full.Live, full.Limit)
	}
	// ...queues, and its queued demand keeps new work from taking the next
	// worker to free.
	queued := dormant(t, rig.store, parked, true)
	if err := rig.store.Release(ctx, lease("new-1")); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := rig.store.Acquire(ctx, lease("new-3")); !errors.As(err, &full) {
		t.Fatalf("new work past a queued round = %v, want FleetCapacityExceededError", err)
	}
	if err := rig.store.Rebudget(ctx, woken(queued)); err != nil {
		t.Fatalf("the queued round must take the freed worker: %v", err)
	}
}

// TestFleetCapacity_AResumedDormantJobIsAdmittedAtAFullFleet is the restart
// case: a job resumed into reviewing acquires its lease dormant, and a fleet
// that filled while the daemon was down does not refuse a job that will run no
// worker.
func TestFleetCapacity_AResumedDormantJobIsAdmittedAtAFullFleet(t *testing.T) {
	rig := newFleetLeaseRig(t, 1)
	if err := rig.acquire("p", 1); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	resumed := &JobLease{TenantID: "acme", ProjectID: "p", Ref: "resumed", JobID: "resumed", Generation: 1,
		OffBudget: true, Dormant: true}
	if err := rig.store.Acquire(context.Background(), resumed); err != nil {
		t.Fatalf("a dormant job resumed at a full fleet: %v", err)
	}
	if err := rig.acquire("p", 2); !errors.Is(err, ErrFleetCapacityExceeded) {
		t.Fatalf("the full fleet admitted a second worker = %v", err)
	}
}

// TestFleetCapacity_WakingOnAnUnreadableCapacityIsRefused is the error path:
// a waking job is admitted like new work, so a capacity nobody can read
// refuses it — and going dormant, which reads nothing, still succeeds.
func TestFleetCapacity_WakingOnAnUnreadableCapacityIsRefused(t *testing.T) {
	ctx := context.Background()
	inner := mustProcessLeaseStore(t)
	store, err := NewFleetCapacityJobLeaseStore(inner,
		&erroringFleetCapacity{err: errors.New("registry unreachable")}, leaseTestTTL)
	if err != nil {
		t.Fatalf("NewFleetCapacityJobLeaseStore: %v", err)
	}
	held := &JobLease{TenantID: "acme", ProjectID: "p", Ref: "r", JobID: "job-1", Generation: 1,
		OffBudget: true, Dormant: true}
	if err := store.Acquire(ctx, held); err != nil {
		t.Fatalf("a dormant acquire reads no capacity: %v", err)
	}
	if err := store.Rebudget(ctx, woken(held)); !errors.Is(err, ErrJobLeaseBackend) {
		t.Fatalf("waking on an unreadable fleet = %v, want ErrJobLeaseBackend", err)
	}
	if err := store.Rebudget(ctx, &JobLease{JobID: "job-2"}); !errors.Is(err, ErrJobLeaseInvalidInput) {
		t.Fatalf("an unkeyable move = %v, want ErrJobLeaseInvalidInput", err)
	}
}

// TestBackendCapacity_ADormantJobHoldsNoBackend: a job parked in reviewing has
// closed its model session, so the backend it ran on is free for another job,
// and its waking round is admitted against the backend's cap.
func TestBackendCapacity_ADormantJobHoldsNoBackend(t *testing.T) {
	ctx := context.Background()
	store := backendCappedStore(t, map[string]int{"qwen": 1})
	worker := &JobLease{TenantID: "acme", ProjectID: "p1", Workflow: "sdlc", Ref: "issue-1",
		JobID: "job-1", Generation: 1, Backend: "qwen"}
	if err := store.Acquire(ctx, worker); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	parked := dormant(t, store, worker, false)

	reader, ok := store.(ProfileLoadReader)
	if !ok {
		t.Fatal("the backend store must answer placement's load question")
	}
	if n, err := reader.LiveJobsForBackend(ctx, "acme", "qwen"); err != nil || n != 0 {
		t.Fatalf("load = %d, %v; want a dormant job to hold no backend", n, err)
	}

	review := backendLease("p1", "review-1", "qwen")
	if err := store.Acquire(ctx, review); err != nil {
		t.Fatalf("a backend whose only job is dormant refused a review: %v", err)
	}
	if err := store.Rebudget(ctx, woken(parked)); !errors.Is(err, ErrBackendCapacityExceeded) {
		t.Fatalf("waking onto a full backend = %v, want ErrBackendCapacityExceeded", err)
	}
	queued := dormant(t, store, parked, true)
	if n, err := reader.LiveJobsForBackend(ctx, "acme", "qwen"); err != nil || n != 2 {
		t.Fatalf("load = %d, %v; want the review and the queued round", n, err)
	}
	if err := store.Release(ctx, review); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := store.Rebudget(ctx, woken(queued)); err != nil {
		t.Fatalf("the queued round must take the freed backend: %v", err)
	}
	if err := store.Rebudget(ctx, &JobLease{JobID: "job-2"}); !errors.Is(err, ErrJobLeaseInvalidInput) {
		t.Fatalf("an unkeyable move = %v, want ErrJobLeaseInvalidInput", err)
	}
}

// fleetCappedRun is a leased run over a store bounded by a fleet of slots
// only, so a refusal can only come from the fleet axis.
func fleetCappedRun(t *testing.T, slots int, runner JobRunner, jobID string, audits chan<- string) (*JobRun, JobLeaseStore) {
	t.Helper()
	capacity, err := NewDeclaredFleetCapacity(slots)
	if err != nil {
		t.Fatalf("NewDeclaredFleetCapacity: %v", err)
	}
	store, err := NewFleetCapacityJobLeaseStore(mustProcessLeaseStore(t), capacity, time.Minute)
	if err != nil {
		t.Fatalf("NewFleetCapacityJobLeaseStore: %v", err)
	}
	return budgetRun(t, store, runner, jobID, audits), store
}

// TestJobRun_AReviewingJobIsDormantAndItsRoundWaitsForTheFleet is the ruling
// through the envelope on the fleet axis: entering reviewing marks the lease
// dormant, so a fleet of one admits other work; the round then WAITS on the
// fleet — not fails — and wakes the lease when a worker frees.
func TestJobRun_AReviewingJobIsDormantAndItsRoundWaitsForTheFleet(t *testing.T) {
	ctx := context.Background()
	audits := make(chan string, 8)
	inReview := make(chan struct{})
	askRound := make(chan struct{})
	roundDone := make(chan error, 1)

	runner := &fakeRunner{name: "budgeted", states: budgetRunnerStates, run: func(ctx context.Context, job *JobRun) error {
		if err := job.SetState(ctx, JobStateReviewing); err != nil {
			return err
		}
		close(inReview)
		<-askRound
		err := job.SetState(ctx, JobStateExecuting)
		roundDone <- err
		return err
	}}
	run, store := fleetCappedRun(t, 1, runner, "job-reviewing", audits)
	done := make(chan error, 1)
	go func() { done <- run.execute(ctx, runner) }()

	<-inReview
	if got := <-audits; got != "orchestration:job_slot_released" {
		t.Fatalf("audit %q, want the slot released on entering reviewing", got)
	}
	held := run.Lease()
	record, err := store.Get(ctx, held.TenantID, held.ProjectID, held.Workflow, held.Ref)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !record.Dormant || !record.OffBudget {
		t.Fatalf("a reviewing job's lease = %+v, want dormant and off the budget", record)
	}
	other := &JobLease{TenantID: "acme", ProjectID: "proj-2", Ref: "other", JobID: "job-other", Generation: 1}
	if err := store.Acquire(ctx, other); err != nil {
		t.Fatalf("a fleet of one whose only job is reviewing must admit other work: %v", err)
	}

	close(askRound)
	awaitRoundQueued(t, audits, roundDone)
	if err := store.Release(ctx, other); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := <-roundDone; err != nil {
		t.Fatalf("the round did not take the freed worker: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("execute: %v", err)
	}
}

// TestJobRun_AResumedReviewingJobIsDormant is the restart case through the
// envelope: a job resumed into reviewing holds a dormant lease from its first
// write, at a fleet that is full.
func TestJobRun_AResumedReviewingJobIsDormant(t *testing.T) {
	ctx := context.Background()
	runner := &fakeRunner{name: "budgeted", states: budgetRunnerStates}
	run, store := fleetCappedRun(t, 1, runner, "job-resumed", make(chan string, 8))
	holder := &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "holder", JobID: "job-holder", Generation: 1}
	if err := store.Acquire(ctx, holder); err != nil {
		t.Fatalf("acquire holder: %v", err)
	}
	run.StartState = JobStateReviewing
	if err := run.execute(ctx, runner); err != nil {
		t.Fatalf("a job resumed into reviewing at a full fleet: %v", err)
	}
	if lease := run.Lease(); !lease.Dormant || !lease.OffBudget {
		t.Fatalf("the resumed job's lease = %+v, want dormant from the start", lease)
	}
}

// wholeOffBudgetRunner is a runner whose every job is off the project budget,
// as the review and triage runners are.
type wholeOffBudgetRunner struct{ *fakeRunner }

func (wholeOffBudgetRunner) LeaseSubject(*JobConfig) string { return "" }

func (wholeOffBudgetRunner) OffProjectBudget() bool { return true }

// TestJobRun_AJobOffTheBudgetForItsWholeLifeIsNeverDormant: a review or triage
// job runs a container and a model session, so it goes on counting on the
// fleet and backend axes even though no project budget charges it.
func TestJobRun_AJobOffTheBudgetForItsWholeLifeIsNeverDormant(t *testing.T) {
	ctx := context.Background()
	var store JobLeaseStore
	admitted := make(chan error, 1)
	runner := wholeOffBudgetRunner{&fakeRunner{name: "reviewish", run: func(ctx context.Context, job *JobRun) error {
		// While the review runs, its live lease fills a fleet of one.
		other := &JobLease{TenantID: "acme", ProjectID: "proj-2", Ref: "other", JobID: "job-other", Generation: 1}
		admitted <- store.Acquire(ctx, other)
		return nil
	}}}
	run, capped := fleetCappedRun(t, 1, runner, "job-review", make(chan string, 8))
	store = capped
	if err := run.execute(ctx, runner); err != nil {
		t.Fatalf("execute: %v", err)
	}
	lease := run.Lease()
	if !lease.OffBudget || lease.Dormant {
		t.Fatalf("a whole-runner off-budget lease = %+v, want off the budget and not dormant", lease)
	}
	if err := <-admitted; !errors.Is(err, ErrFleetCapacityExceeded) {
		t.Fatalf("a fleet of one holding a review admitted a worker = %v, want refused", err)
	}
}

// TestJobStallDetector_ReadsTheJobsOwnRunnerForSessionBacking: two runners
// name the same state and mean different things by it. The merged declaration
// is session-backed; the sdlc-shaped runner's own is not, because it releases
// its worker there — and a job of that runner must be reported, never ended,
// for holding no session.
func TestJobStallDetector_ReadsTheJobsOwnRunnerForSessionBacking(t *testing.T) {
	start := time.Now().UTC()
	released := loopInState("job-released", JobStateReviewing, start)
	released.Runner = "delivery"
	lead := loopInState("job-lead", JobStateReviewing, start)
	lead.Runner = "workflow"
	unknown := loopInState("job-unknown", JobStateReviewing, start)
	unknown.Runner = "unregistered"
	loops := fakeRuns{released, lead, unknown}

	specs := []JobStateSpec{{State: JobStateReviewing, Label: "Reviewing", SessionBacked: true, IdleThreshold: time.Minute}}
	runnerBacked := runnerSessionBackedIn(registryOf(t,
		&fakeRunner{name: "delivery", states: []JobStateSpec{{State: JobStateReviewing}}},
		&fakeRunner{name: "workflow", states: []JobStateSpec{{State: JobStateReviewing, SessionBacked: true}}},
	))
	detector, err := newJobStallDetector(loops, &fakeSessions{}, nil, specs, runnerBacked)
	if err != nil {
		t.Fatalf("newJobStallDetector: %v", err)
	}
	clock := newFakeClock()
	clock.now = start
	detector.now = clock.Now
	clock.Advance(2 * time.Minute)

	verdicts := map[string]JobStallVerdict{}
	for _, finding := range detector.Detect(context.Background()).Findings {
		verdicts[finding.JobID] = finding.Verdict
	}
	want := map[string]JobStallVerdict{
		"job-released": JobStallIdle,
		"job-lead":     JobStallStalled,
		// A runner nobody registered falls back to the merged answer.
		"job-unknown": JobStallStalled,
	}
	for jobID, verdict := range want {
		if verdicts[jobID] != verdict {
			t.Errorf("%s verdict = %q, want %q (all: %v)", jobID, verdicts[jobID], verdict, verdicts)
		}
	}
	if released.abortReason() != nil {
		t.Fatal("the job that released its worker on purpose was ended")
	}
}

// registryOf builds a runner registry a test owns.
func registryOf(t *testing.T, runners ...JobRunner) *JobRunnerRegistry {
	t.Helper()
	registry := NewJobRunnerRegistry()
	for _, runner := range runners {
		if err := registry.Register(runner); err != nil {
			t.Fatalf("register %s: %v", runner.Name(), err)
		}
	}
	return registry
}

// TestJobRun_ClosingTheContainersSessionForgetsTheContainer: nothing may exec
// into a container whose session the job has closed; closing another session
// leaves the container alone.
func TestJobRun_ClosingTheContainersSessionForgetsTheContainer(t *testing.T) {
	ctx := context.Background()
	run := loopInState("job-1", JobStateExecuting, time.Now())
	if err := run.SetContainer("session-worker", "/workspace/api"); err != nil {
		t.Fatalf("SetContainer: %v", err)
	}
	run.Close(ctx, "session-other")
	if run.Container() == nil {
		t.Fatal("closing a different session forgot the worker's container")
	}
	run.Close(ctx, "")
	if run.Container() == nil {
		t.Fatal("an empty close forgot the worker's container")
	}
	run.Close(ctx, "session-worker")
	if run.Container() != nil {
		t.Fatalf("container = %+v after its session closed, want none", run.Container())
	}
	var invalid *InputValidationError
	if err := run.SetContainer("", "/workspace/api"); !errors.As(err, &invalid) || invalid.Field != "session_id" {
		t.Fatalf("a container with no session = %v, want refused on session_id", err)
	}
}
