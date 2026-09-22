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
	"strings"
	"testing"
	"time"
)

// =============================================================================
// The project budget, per state (go-quicbaas#978)
// =============================================================================
//
// The owner's ruling: a job waiting in a state that holds no worker does not
// hold one of its project's MaxConcurrency slots, and a round that needs a
// worker again takes a slot back through admission — waiting, not failing,
// while the project is full. The lease and the one-job-per-task invariant are
// untouched.

// TestJobLease_BudgetStandingIsReadTheSameByEveryReader pins the two
// predicates the budget's readers answer through: a queued return counts
// against new work but holds no slot.
func TestJobLease_BudgetStandingIsReadTheSameByEveryReader(t *testing.T) {
	now := time.Now().UTC()
	fresh := now.UnixNano()
	stale := now.Add(-2 * leaseTestTTL).UnixNano()
	tests := []struct {
		name       string
		lease      *JobLease
		wantCounts bool
		wantHolds  bool
	}{
		{"a live lease on the budget holds a slot", &JobLease{HeartbeatAt: fresh}, true, true},
		{"a live lease off the budget is free", &JobLease{HeartbeatAt: fresh, OffBudget: true}, false, false},
		{"a lease awaiting a slot counts but holds none", &JobLease{HeartbeatAt: fresh, OffBudget: true, AwaitingSlot: true}, true, false},
		{"a released lease is nothing", &JobLease{HeartbeatAt: fresh, Released: true}, false, false},
		{"a stale lease awaiting a slot is nothing", &JobLease{HeartbeatAt: stale, OffBudget: true, AwaitingSlot: true}, false, false},
		{"no lease is nothing", nil, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.lease.CountsAgainstBudget(now, leaseTestTTL); got != tc.wantCounts {
				t.Errorf("CountsAgainstBudget = %v, want %v", got, tc.wantCounts)
			}
			if got := tc.lease.HoldsSlot(now, leaseTestTTL); got != tc.wantHolds {
				t.Errorf("HoldsSlot = %v, want %v", got, tc.wantHolds)
			}
		})
	}
}

// TestJobLeaseStore_RebudgetMovesTheStandingNotTheLease proves, on both
// stores, that a move changes the budget flags of the holder's own record and
// nothing that makes it the holder.
func TestJobLeaseStore_RebudgetMovesTheStandingNotTheLease(t *testing.T) {
	for _, tc := range leaseStoreCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, clock := tc.build(t, leaseTestTTL)
			ctx := context.Background()
			ref := uniqueTaskID("rebudget")

			held := testLease(ref, "job-1", 1, "node-a")
			if err := store.Acquire(ctx, held); err != nil {
				t.Fatalf("acquire: %v", err)
			}
			clock.Advance(time.Second)

			move := *held
			move.OffBudget = true
			move.AwaitingSlot = true
			if err := store.Rebudget(ctx, &move); err != nil {
				t.Fatalf("rebudget: %v", err)
			}
			got, err := store.Get(ctx, held.TenantID, held.ProjectID, held.Workflow, held.Ref)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if !got.OffBudget || !got.AwaitingSlot {
				t.Fatalf("record off_budget=%v awaiting_slot=%v, want both set", got.OffBudget, got.AwaitingSlot)
			}
			if got.JobID != "job-1" || got.Generation != 1 || got.Released || got.AcquiredAt != held.AcquiredAt {
				t.Fatalf("the move changed the holding: %+v", got)
			}
			if got.HeartbeatAt <= held.HeartbeatAt {
				t.Fatal("a move is a proof of life and must advance the heartbeat")
			}

			// A heartbeat keeps the standing the move wrote.
			beat := *held
			if err := store.Heartbeat(ctx, &beat); err != nil {
				t.Fatalf("heartbeat: %v", err)
			}
			if !beat.OffBudget || !beat.AwaitingSlot {
				t.Fatal("a heartbeat must not undo a budget move")
			}
		})
	}
}

func TestJobLeaseStore_RebudgetRefusesAJobThatNoLongerHoldsTheLease(t *testing.T) {
	for _, tc := range leaseStoreCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := tc.build(t, leaseTestTTL)
			ctx := context.Background()
			ref := uniqueTaskID("rebudget-superseded")

			first := testLease(ref, "job-1", 1, "node-a")
			if err := store.Acquire(ctx, first); err != nil {
				t.Fatalf("acquire first: %v", err)
			}
			second := testLease(ref, "job-2", 2, "node-b")
			if err := store.Acquire(ctx, second); err != nil {
				t.Fatalf("acquire second: %v", err)
			}

			move := *first
			move.OffBudget = true
			var superseded *JobSupersededError
			if err := store.Rebudget(ctx, &move); !errors.As(err, &superseded) {
				t.Fatalf("a superseded holder's move = %v, want JobSupersededError", err)
			}
			got, err := store.Get(ctx, second.TenantID, second.ProjectID, second.Workflow, second.Ref)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.OffBudget {
				t.Fatal("a superseded job moved the new holder off the budget")
			}

			// A released record is left alone, and an unkeyable lease is refused.
			if err := store.Release(ctx, second); err != nil {
				t.Fatalf("release: %v", err)
			}
			after := *second
			after.OffBudget = true
			if err := store.Rebudget(ctx, &after); err != nil {
				t.Fatalf("a released record's move = %v, want a no-op", err)
			}
			if err := store.Rebudget(ctx, &JobLease{JobID: "job-3"}); !errors.Is(err, ErrJobLeaseInvalidInput) {
				t.Fatalf("an unkeyable move = %v, want ErrJobLeaseInvalidInput", err)
			}
		})
	}
}

// TestProjectCapacity_AJobOffTheBudgetAdmitsASecondTask is the issue's first
// acceptance test at the store: quota 1, one job in reviewing, and a second
// task is admitted — then the reviewing job's round is refused while the
// project is full, and its queued demand keeps new work out.
func TestProjectCapacity_AJobOffTheBudgetAdmitsASecondTask(t *testing.T) {
	ctx := context.Background()
	store, _ := cappedCapacityStore(t, 1)

	reviewing := &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "issue-1", JobID: "job-reviewing", Generation: 1}
	if err := store.Acquire(ctx, reviewing); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	off := *reviewing
	off.OffBudget = true
	if err := store.Rebudget(ctx, &off); err != nil {
		t.Fatalf("leaving the budget must never be refused: %v", err)
	}

	second := &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "issue-2", JobID: "job-second", Generation: 1}
	if err := store.Acquire(ctx, second); err != nil {
		t.Fatalf("quota 1 with its only job in reviewing must admit a second task: %v", err)
	}

	back := off
	back.OffBudget = false
	var full *ProjectCapacityExceededError
	if err := store.Rebudget(ctx, &back); !errors.As(err, &full) {
		t.Fatalf("a round on a full project = %v, want ProjectCapacityExceededError", err)
	}
	if full.Live != 1 || full.Limit != 1 {
		t.Fatalf("refusal live=%d limit=%d, want 1/1", full.Live, full.Limit)
	}

	queued := off
	queued.AwaitingSlot = true
	if err := store.Rebudget(ctx, &queued); err != nil {
		t.Fatalf("queueing: %v", err)
	}
	now := time.Now().UTC()
	if n, err := CountLiveJobLeases(ctx, store, "acme", "proj-1", now, leaseTestTTL); err != nil || n != 2 {
		t.Fatalf("budget count = %d, %v; want the holder and the queued round", n, err)
	}

	// A slot frees: the queued round is ahead of new work.
	if err := store.Release(ctx, second); err != nil {
		t.Fatalf("release: %v", err)
	}
	third := &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "issue-3", JobID: "job-third", Generation: 1}
	if err := store.Acquire(ctx, third); !errors.As(err, &full) {
		t.Fatalf("new work past a queued round = %v, want ProjectCapacityExceededError", err)
	}
	if err := store.Rebudget(ctx, &back); err != nil {
		t.Fatalf("the queued round must take the freed slot: %v", err)
	}
}

// TestProjectCapacity_QueuedRoundsDoNotBlockEachOther: two waiters that each
// counted the other would wait for ever. The first to ask takes the slot, and
// the second is then refused by the slot it took.
func TestProjectCapacity_QueuedRoundsDoNotBlockEachOther(t *testing.T) {
	ctx := context.Background()
	store, _ := cappedCapacityStore(t, 1)

	queue := func(ref, jobID string) *JobLease {
		t.Helper()
		lease := &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: ref, JobID: jobID, Generation: 1, OffBudget: true}
		if err := store.Acquire(ctx, lease); err != nil {
			t.Fatalf("acquire %s: %v", jobID, err)
		}
		lease.AwaitingSlot = true
		if err := store.Rebudget(ctx, lease); err != nil {
			t.Fatalf("queue %s: %v", jobID, err)
		}
		return lease
	}
	a := queue("issue-a", "job-a")
	b := queue("issue-b", "job-b")

	a.OffBudget, a.AwaitingSlot = false, false
	if err := store.Rebudget(ctx, a); err != nil {
		t.Fatalf("the first queued round must be admitted past the second: %v", err)
	}
	b.OffBudget, b.AwaitingSlot = false, false
	if err := store.Rebudget(ctx, b); !errors.Is(err, ErrProjectCapacityExceeded) {
		t.Fatalf("the second round = %v, want refused by the slot the first took", err)
	}
}

// =============================================================================
// The envelope's side
// =============================================================================

// budgetRunnerStates is a runner that spends a slot in executing and none in
// reviewing, the sdlc shape reduced to the two states that matter.
var budgetRunnerStates = []JobStateSpec{
	{State: JobStateExecuting, Label: "Executing"},
	{State: JobStateReviewing, Label: "Reviewing", OffProjectBudget: true},
}

// budgetRun builds a leased run over the capped store, reporting its audit
// events on audits.
func budgetRun(t *testing.T, store JobLeaseStore, runner JobRunner, jobID string, audits chan<- string) *JobRun {
	t.Helper()
	config := newTestConfig()
	config.TenantID = "acme"
	config.ProjectID = "proj-1"
	run := newJobRun(JobRunConfig{
		JobID:  jobID,
		Runner: runner.Name(),
		Config: config,
		Agents: &controlLoopMockService{},
		Logger: &controlLoopMockLogger{},
		Leases: store,
		Node:   "node-a",
		// Short, so a queued round retries within the test; the store's own
		// TTL — which decides liveness — is the capped store's minute.
		LeaseTTL: 30 * time.Millisecond,
		AuditFn: func(eventType, _ string) {
			if strings.HasPrefix(eventType, "orchestration:job_slot_") {
				audits <- eventType
			}
		},
	})
	run.StartState = entryStateFor(runner)
	return run
}

// TestJobRun_AReviewingJobFreesItsSlotAndItsRoundWaitsForOne is the issue's
// acceptance test through the envelope: quota 1, a job moves into reviewing
// and a second task is admitted; the first job's round then WAITS — not fails
// — and proceeds when the second job's slot frees.
func TestJobRun_AReviewingJobFreesItsSlotAndItsRoundWaitsForOne(t *testing.T) {
	ctx := context.Background()
	store, _ := cappedCapacityStore(t, 1)
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
	first := budgetRun(t, store, runner, "job-first", audits)
	firstDone := make(chan error, 1)
	go func() { firstDone <- first.execute(ctx, runner) }()

	<-inReview
	if got := <-audits; got != "orchestration:job_slot_released" {
		t.Fatalf("audit %q, want the slot released on entering reviewing", got)
	}
	second := &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "issue-second", JobID: "job-second", Generation: 1}
	if err := store.Acquire(ctx, second); err != nil {
		t.Fatalf("quota 1 with one job in reviewing must admit a second task: %v", err)
	}

	close(askRound)
	awaitRoundQueued(t, audits, roundDone)
	held := first.Lease()
	record, err := store.Get(ctx, held.TenantID, held.ProjectID, held.Workflow, held.Ref)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !record.OffBudget || !record.AwaitingSlot || record.Released {
		t.Fatalf("a waiting round's lease = %+v, want held, off the budget and queued", record)
	}
	if state := first.Status().State; state != JobStateReviewing {
		t.Fatalf("a waiting round reads %s, want still reviewing", state)
	}

	if err := store.Release(ctx, second); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := <-roundDone; err != nil {
		t.Fatalf("the round must proceed once a slot frees: %v", err)
	}
	if got := <-audits; got != "orchestration:job_slot_admitted" {
		t.Fatalf("audit %q, want the round admitted", got)
	}
	if err := <-firstDone; err != nil {
		t.Fatalf("first run: %v", err)
	}
	record, err = store.Get(ctx, held.TenantID, held.ProjectID, held.Workflow, held.Ref)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if record.OffBudget || record.AwaitingSlot {
		t.Fatalf("an admitted round's lease = %+v, want back on the budget", record)
	}
}

// awaitRoundQueued fails the test unless the round is queued before it
// finishes. It selects on both so a round wrongly admitted FAILS the test
// rather than leaving it blocked on an audit that never comes.
func awaitRoundQueued(t *testing.T, audits <-chan string, roundDone <-chan error) {
	t.Helper()
	select {
	case got := <-audits:
		if got != "orchestration:job_slot_awaited" {
			t.Fatalf("audit %q, want the round queued on a full project", got)
		}
	case err := <-roundDone:
		t.Fatalf("the round entered its worker state on a full project without waiting: %v", err)
	}
	select {
	case err := <-roundDone:
		t.Fatalf("the round entered its worker state while the project was still full: %v", err)
	default:
	}
}

// TestJobRun_AWaitingRoundEndsWithItsRun: a round waiting on a full project
// is not a hang. Ending the run — a cancellation, a supersession, the stall
// detector — ends the wait.
func TestJobRun_AWaitingRoundEndsWithItsRun(t *testing.T) {
	store, _ := cappedCapacityStore(t, 1)
	holder := &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "issue-holder", JobID: "job-holder", Generation: 1}
	audits := make(chan string, 8)

	runner := &fakeRunner{name: "budgeted", states: budgetRunnerStates, run: func(ctx context.Context, job *JobRun) error {
		if err := job.SetState(ctx, JobStateReviewing); err != nil {
			return err
		}
		// The project fills while this job is in reviewing.
		if err := store.Acquire(ctx, holder); err != nil {
			return err
		}
		return job.SetState(ctx, JobStateExecuting)
	}}
	run := budgetRun(t, store, runner, "job-waiting", audits)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run.execute(ctx, runner) }()

	for got := range audits {
		if got == "orchestration:job_slot_awaited" {
			break
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled wait = %v, want context.Canceled", err)
	}
}

// TestJobRun_ResumedIntoAStateOffTheBudgetIsAdmittedAtAFullProject: a job
// resumed into reviewing takes its lease off the budget from the start, so a
// project that filled while the daemon was down does not refuse it.
func TestJobRun_ResumedIntoAStateOffTheBudgetIsAdmittedAtAFullProject(t *testing.T) {
	ctx := context.Background()
	store, _ := cappedCapacityStore(t, 1)
	holder := &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "issue-holder", JobID: "job-holder", Generation: 1}
	if err := store.Acquire(ctx, holder); err != nil {
		t.Fatalf("acquire holder: %v", err)
	}

	runner := &fakeRunner{name: "budgeted", states: budgetRunnerStates}
	run := budgetRun(t, store, runner, "job-resumed", make(chan string, 8))
	run.StartState = JobStateReviewing
	if err := run.execute(ctx, runner); err != nil {
		t.Fatalf("a job resumed into reviewing at a full project: %v", err)
	}
	if !run.Lease().OffBudget {
		t.Fatal("the resumed job's lease must start off the budget")
	}

	// The same job resumed into a state that spends a slot is refused, as
	// every job always was.
	onBudget := budgetRun(t, store, runner, "job-resumed-executing", make(chan string, 8))
	if err := onBudget.execute(ctx, runner); !errors.Is(err, ErrProjectCapacityExceeded) {
		t.Fatalf("a job resumed into executing at a full project = %v, want refused", err)
	}
}

// commitThenFailStore commits the first release of a slot and then answers
// an error, as a transaction whose proposal applied but whose answer was lost
// to a deadline does.
type commitThenFailStore struct {
	JobLeaseStore
	failed bool
}

func (s *commitThenFailStore) Rebudget(ctx context.Context, lease *JobLease) error {
	if err := s.JobLeaseStore.Rebudget(ctx, lease); err != nil {
		return err
	}
	if lease.OffBudget && !lease.AwaitingSlot && !s.failed {
		s.failed = true
		return &JobLeaseError{Sentinel: ErrJobLeaseBackend, Message: "the answer was lost after the write applied"}
	}
	return nil
}

// TestJobRun_AReleaseThatCommittedButFailedStillAdmitsTheRound is the review
// finding on #978: a failed release must not leave the run believing it kept
// its slot. The release here commits and answers an error; another task then
// takes the slot; and the round must still WAIT for admission rather than
// run a second worker on a project with quota 1.
func TestJobRun_AReleaseThatCommittedButFailedStillAdmitsTheRound(t *testing.T) {
	ctx := context.Background()
	capped, _ := cappedCapacityStore(t, 1)
	store := &commitThenFailStore{JobLeaseStore: capped}
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
	run := budgetRun(t, store, runner, "job-first", audits)
	runDone := make(chan error, 1)
	go func() { runDone <- run.execute(ctx, runner) }()

	<-inReview
	if !store.failed {
		t.Fatal("the release was not the write that failed; this test proves nothing")
	}
	second := &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "issue-second", JobID: "job-second", Generation: 1}
	if err := capped.Acquire(ctx, second); err != nil {
		t.Fatalf("the committed release freed the slot, so a second task is admitted: %v", err)
	}

	close(askRound)
	awaitRoundQueued(t, audits, roundDone)

	if err := capped.Release(ctx, second); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := <-roundDone; err != nil {
		t.Fatalf("the round must proceed once a slot frees: %v", err)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("run: %v", err)
	}
}

// failWithoutWriteStore answers an error for the first release of a slot
// WITHOUT applying it: the other half of the unknown standing.
type failWithoutWriteStore struct {
	JobLeaseStore
	rebudgets int
}

func (s *failWithoutWriteStore) Rebudget(ctx context.Context, lease *JobLease) error {
	s.rebudgets++
	if s.rebudgets == 1 {
		return &JobLeaseError{Sentinel: ErrJobLeaseBackend, Message: "the write never reached the store"}
	}
	return s.JobLeaseStore.Rebudget(ctx, lease)
}

// TestJobRun_AnUnknownStandingIsSettledByTheStore: a release that failed
// WITHOUT applying left the job on the budget. The store says so, and the
// round proceeds on the slot the job never gave up — no second write, no
// wait — even on a project whose quota that slot fills.
func TestJobRun_AnUnknownStandingIsSettledByTheStore(t *testing.T) {
	ctx := context.Background()
	capped, _ := cappedCapacityStore(t, 1)
	store := &failWithoutWriteStore{JobLeaseStore: capped}
	audits := make(chan string, 8)

	runner := &fakeRunner{name: "budgeted", states: budgetRunnerStates, run: func(ctx context.Context, job *JobRun) error {
		if err := job.SetState(ctx, JobStateReviewing); err != nil {
			return err
		}
		// The slot was never released, so no other task can have it.
		other := &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "issue-other", JobID: "job-other", Generation: 1}
		if err := capped.Acquire(ctx, other); !errors.Is(err, ErrProjectCapacityExceeded) {
			t.Errorf("a slot whose release never applied was handed out: %v", err)
		}
		return job.SetState(ctx, JobStateExecuting)
	}}
	run := budgetRun(t, store, runner, "job-first", audits)
	if err := run.execute(ctx, runner); err != nil {
		t.Fatalf("run: %v", err)
	}
	// The audit is written on the run's goroutine, which has returned, so a
	// missing one is absent now rather than late.
	select {
	case got := <-audits:
		if got != "orchestration:job_slot_reconciled" {
			t.Fatalf("audit %q, want the standing settled from the store", got)
		}
	default:
		t.Fatal("the unknown standing was never settled from the store")
	}
	if store.rebudgets != 1 {
		t.Fatalf("%d budget writes, want only the release that failed", store.rebudgets)
	}
}

func TestRunnerBudgetStates(t *testing.T) {
	declaring := &fakeRunner{name: "budgeted", states: budgetRunnerStates}
	states := runnerBudgetStates(declaring)
	if !states[JobStateReviewing] || states[JobStateExecuting] {
		t.Fatalf("states = %v, want reviewing off and executing on", states)
	}
	if _, declared := states[JobStateCompleted]; declared {
		t.Fatal("a terminal state is not the runner's to declare")
	}
	if got := runnerBudgetStates(nil); got != nil {
		t.Fatalf("a nil runner = %v, want no answer", got)
	}
	if got := runnerBudgetStates(budgetTestRunner{name: "review", off: true}); got != nil {
		t.Fatalf("a runner off the budget as a whole = %v, want no per-state answer", got)
	}
}
