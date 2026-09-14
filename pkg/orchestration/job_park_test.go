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
	"testing"
	"time"
)

// parkTestRun is a job run with just enough around it to record a judgement.
func parkTestRun() *JobRun {
	return NewJobRun(JobRunConfig{
		JobID:  "job-1",
		Config: &JobConfig{TenantID: "tenant-a", TaskID: "task-1", Attempt: 1},
		Logger: &agentTestLogger{},
	})
}

// TestJobRun_ParkRecordsTheRunnersJudgement: a runner that knows another
// attempt would ask the same unanswered question says so, with the kind and
// the round it reached.
func TestJobRun_ParkRecordsTheRunnersJudgement(t *testing.T) {
	t.Parallel()

	run := parkTestRun()
	if run.Parked() != nil {
		t.Fatal("a fresh run reported a park nobody made")
	}
	run.NextIteration()
	run.Park(TaskBlockKindNeedsInfo, "which store should hold the index?", "")

	parked := run.Parked()
	if parked == nil {
		t.Fatal("Park recorded nothing")
	}
	if parked.Kind != TaskBlockKindNeedsInfo {
		t.Fatalf("kind = %q, want needs_info", parked.Kind)
	}
	if parked.Message != "which store should hold the index?" {
		t.Fatalf("message = %q", parked.Message)
	}
	if parked.Iterations != 1 {
		t.Fatalf("iterations = %d, want the round the run reached", parked.Iterations)
	}
}

// TestJobRun_ParkRefusesAKindNothingCanRender: the kinds are a closed enum
// the task store validates, so a park into a kind outside it is dropped
// rather than stored — a task blocked with an unrenderable kind is a task
// nobody can find.
func TestJobRun_ParkRefusesAKindNothingCanRender(t *testing.T) {
	t.Parallel()

	for _, kind := range []TaskBlockKind{"", "made_up", "NEEDS_INFO"} {
		run := parkTestRun()
		run.Park(kind, "a reason", "")
		if parked := run.Parked(); parked != nil {
			t.Fatalf("Park(%q) recorded %+v", kind, parked)
		}
	}
}

// parkedTask is a task mid-attempt, with one job dispatched against it.
func parkedTask() *Task {
	return &Task{
		ID: "task-1", TenantID: "tenant-a", ProjectID: "proj-1",
		Status: TaskStatusInProgress, SpawnedJobID: "job-1",
		Attempts: 1, AttemptBudget: 3,
	}
}

// TestApplyConclusion_AParkStopsTheTaskInsteadOfRequeueing is
// go-quicbaas#787's accounting half: without the park the failed job is an
// ordinary failed attempt, and the engine dispatches the next one — the same
// prompt, the same unanswered question, until the budget is gone.
func TestApplyConclusion_AParkStopsTheTaskInsteadOfRequeueing(t *testing.T) {
	t.Parallel()

	task := parkedTask()
	task.ConsecutiveEnvironmentFaults = 2
	reason := applyConclusion(task, attemptConclusion{
		status: &JobStatus{JobID: "job-1", State: JobStateFailed, Error: "escalated"},
		park:   &BlockReason{Kind: TaskBlockKindNeedsInfo, Message: "which store should hold the index?", Surfaced: true},
	}, time.Now().UTC())

	// The conclusion the observers are told. It is its OWN reason rather than
	// one of the parked_* three: those are the operator's explicit BlockTask,
	// which nothing has announced yet, while a runner park reached the forge
	// through the run's own Blocked moment before the job returned.
	if reason != TaskConcludedRunnerParked {
		t.Fatalf("conclusion = %q, want runner_parked", reason)
	}
	if !reason.Valid() {
		t.Fatal("runner_parked is not in the closed vocabulary")
	}
	if task.Status != TaskStatusBlocked {
		t.Fatalf("status = %s, want blocked", task.Status)
	}
	if task.BlockKind != TaskBlockKindNeedsInfo {
		t.Fatalf("block kind = %q, want needs_info", task.BlockKind)
	}
	if task.BlockedReason != "which store should hold the index?" {
		t.Fatalf("reason = %q", task.BlockedReason)
	}
	if task.BlockFaultClass != "" {
		t.Fatalf("fault class = %q; a park is not an environment fault", task.BlockFaultClass)
	}
	if task.AssignedTo != "" || task.SpawnedJobID != "" {
		t.Fatalf("the attempt was not closed out: assigned=%q spawned=%q", task.AssignedTo, task.SpawnedJobID)
	}
	if task.ConsecutiveEnvironmentFaults != 0 || task.RetryNotBefore != 0 {
		t.Fatalf("the fault counters survived a park: %d / %d",
			task.ConsecutiveEnvironmentFaults, task.RetryNotBefore)
	}
}

// TestApplyConclusion_AParkOutranksTheBudgetAndTheFaults: the two other
// verdicts the failure path can reach must not overrule the runner. A spent
// budget would end the task `failed` with no stated question, and a fault
// class would report the environment for a worker's question.
func TestApplyConclusion_AParkOutranksTheBudgetAndTheFaults(t *testing.T) {
	t.Parallel()

	spent := parkedTask()
	spent.Attempts = 3
	if reason := applyConclusion(spent, attemptConclusion{
		status: &JobStatus{JobID: "job-1", State: JobStateFailed, Error: "escalated"},
		park:   &BlockReason{Kind: TaskBlockKindEscalated, Message: "nobody answered", Surfaced: true},
	}, time.Now().UTC()); reason != TaskConcludedRunnerParked {
		t.Fatalf("a spent budget overruled the park's conclusion: %q", reason)
	}
	if spent.Status != TaskStatusBlocked || spent.BlockKind != TaskBlockKindEscalated {
		t.Fatalf("a spent budget overruled the park: status=%s kind=%q", spent.Status, spent.BlockKind)
	}

	faulted := parkedTask()
	if reason := applyConclusion(faulted, attemptConclusion{
		status: &JobStatus{JobID: "job-1", State: JobStateFailed, FailureClass: FaultNoProviderAccount},
		fault:  EnvironmentFault{Class: FaultNoProviderAccount, Reason: "the pool is empty"},
		park:   &BlockReason{Kind: TaskBlockKindBlocked, Message: "the module proxy is unreachable", Surfaced: true},
	}, time.Now().UTC()); reason != TaskConcludedRunnerParked {
		t.Fatalf("a fault class overruled the park's conclusion: %q", reason)
	}
	if faulted.BlockedReason != "the module proxy is unreachable" {
		t.Fatalf("a fault class overruled the park: %q", faulted.BlockedReason)
	}
	if faulted.EnvironmentFaults != 0 {
		t.Fatalf("a park was counted as an environment fault: %d", faulted.EnvironmentFaults)
	}
}

// TestApplyConclusion_WithoutAParkTheFailureIsAccountedAsItAlwaysWas: the
// control. A failed job that made no judgement about the task still requeues
// while the budget allows, and a completed job ignores a park entirely —
// there is nothing to wait for on work that landed.
func TestApplyConclusion_WithoutAParkTheFailureIsAccountedAsItAlwaysWas(t *testing.T) {
	t.Parallel()

	plain := parkedTask()
	if reason := applyConclusion(plain, attemptConclusion{
		status: &JobStatus{JobID: "job-1", State: JobStateFailed, Error: "the work failed"},
	}, time.Now().UTC()); reason != "" {
		// An empty reason is "this attempt ended, the task did not": a
		// requeue has concluded nothing and announces nothing.
		t.Fatalf("an unparked failure concluded %q", reason)
	}
	if plain.Status != TaskStatusPending {
		t.Fatalf("status = %s, want pending", plain.Status)
	}
	if plain.BlockKind != "" || plain.BlockedReason != "" {
		t.Fatalf("an unparked failure blocked the task: %q / %q", plain.BlockKind, plain.BlockedReason)
	}

	delivered := parkedTask()
	if reason := applyConclusion(delivered, attemptConclusion{
		status: &JobStatus{JobID: "job-1", State: JobStateCompleted},
		park:   &BlockReason{Kind: TaskBlockKindNeedsInfo, Message: "which store?"},
	}, time.Now().UTC()); reason != TaskConcludedCompleted {
		t.Fatalf("a completed job concluded %q", reason)
	}
	if delivered.Status != TaskStatusCompleted {
		t.Fatalf("status = %s, want completed", delivered.Status)
	}
	if delivered.BlockedReason != "" {
		t.Fatalf("a completed task carried a park reason: %q", delivered.BlockedReason)
	}
}

// TestJobRun_ARunnerErrorEndsTheJobFailed is the other half of the park's
// reachability, and it belongs here because writeTerminalStatus is this
// package's.
//
// applyConclusion reads a runner's park ONLY on a failed job — every other
// state answers `completed` and returns first. So "a runner that returned an
// error ends failed" is load-bearing for the park, not incidental: if an
// errored run ended in any other state the park would be dropped and the task
// requeued, which is go-quicbaas#787 one layer down.
func TestJobRun_ARunnerErrorEndsTheJobFailed(t *testing.T) {
	t.Parallel()

	run := parkTestRun()
	run.Park(TaskBlockKindNeedsInfo, "which store should hold the index?", "")
	run.writeTerminalStatus(&CostInvalidInputError{Field: "x", Message: "the run refused"}, false)

	status := run.Status()
	if status == nil || status.State != JobStateFailed {
		t.Fatalf("state = %v, want failed", status)
	}
	// The park survives the terminal write: the conclusion reads both.
	if run.Parked() == nil {
		t.Fatal("the terminal write dropped the park")
	}
}

// TestJobRun_ACleanRunDoesNotEndFailed is the negative: a run that returned
// nothing concludes completed, and a park recorded on it is not read — which
// is why parkTurn always ends the run with a terminal error.
func TestJobRun_ACleanRunDoesNotEndFailed(t *testing.T) {
	t.Parallel()

	run := parkTestRun()
	run.writeTerminalStatus(nil, true)

	status := run.Status()
	if status == nil || status.State != JobStateCompleted {
		t.Fatalf("state = %v, want completed", status)
	}
}

// TestJobRun_ParkIsOnThePersistedStatus: the durability half. A park held
// only in memory dies with the process that made it, so the terminal status
// the run writes must carry it — that record is all the lease reaper gets.
func TestJobRun_ParkIsOnThePersistedStatus(t *testing.T) {
	t.Parallel()

	var persisted []*JobStatus
	run := NewJobRun(JobRunConfig{
		JobID:     "job-1",
		Config:    &JobConfig{TenantID: "tenant-a", TaskID: "task-1", Attempt: 1},
		Logger:    &agentTestLogger{},
		PersistFn: func(s *JobStatus) { persisted = append(persisted, s) },
	})

	run.Park(TaskBlockKindNeedsInfo, "which store should hold the index?", "")
	run.Fail("", &CostInvalidInputError{Field: "x", Message: "the run escalated"})

	if len(persisted) == 0 {
		t.Fatal("the terminal status was never persisted")
	}
	last := persisted[len(persisted)-1]
	if last.State != JobStateFailed {
		t.Fatalf("state = %s, want failed", last.State)
	}
	if last.Park == nil {
		t.Fatal("the persisted status carries no park; a reaper rebuilding the conclusion would lose it")
	}
	if last.Park.Kind != TaskBlockKindNeedsInfo || last.Park.Message != "which store should hold the index?" {
		t.Fatalf("persisted park = %+v", last.Park)
	}
}

// TestJobRun_AnUnparkedRunPersistsNoPark is the negative: the field is absent
// unless a runner actually made the judgement, so nothing downstream can read
// a park nobody recorded.
func TestJobRun_AnUnparkedRunPersistsNoPark(t *testing.T) {
	t.Parallel()

	var persisted []*JobStatus
	run := NewJobRun(JobRunConfig{
		JobID:     "job-1",
		Config:    &JobConfig{TenantID: "tenant-a", TaskID: "task-1", Attempt: 1},
		Logger:    &agentTestLogger{},
		PersistFn: func(s *JobStatus) { persisted = append(persisted, s) },
	})
	run.Fail("", &CostInvalidInputError{Field: "x", Message: "the work failed"})

	last := persisted[len(persisted)-1]
	if last.Park != nil {
		t.Fatalf("an unparked run persisted %+v", last.Park)
	}
}

// TestApplyConclusion_AParkOutranksAReapedDeath: the ordering the crash
// depends on. The reaper stamps every record it concludes `abandoned`, so if
// the death were tested before the park, a task the runner had already parked
// for a person would be read as an undiagnosed node failure and requeued.
func TestApplyConclusion_AParkOutranksAReapedDeath(t *testing.T) {
	t.Parallel()

	task := parkedTask()
	reason := applyConclusion(task, attemptConclusion{
		status: &JobStatus{
			JobID: "job-1", State: JobStateFailed,
			FailureClass: JobFailureClassAbandoned,
			Error:        "node-dead stopped heartbeating",
		},
		park: &BlockReason{Kind: TaskBlockKindNeedsInfo, Message: "which store should hold the index?", Surfaced: true},
	}, time.Now().UTC())

	if reason != TaskConcludedRunnerParked {
		t.Fatalf("conclusion = %q, want runner_parked; the reaper overwrote the runner's judgement", reason)
	}
	if task.Status != TaskStatusBlocked || task.BlockKind != TaskBlockKindNeedsInfo {
		t.Fatalf("task = %s/%s, want blocked/needs_info", task.Status, task.BlockKind)
	}
	if task.BlockedReason != "which store should hold the index?" {
		t.Fatalf("reason = %q", task.BlockedReason)
	}

	// The control: the same death with no park still applies applyDeath.
	died := parkedTask()
	if reason := applyConclusion(died, attemptConclusion{
		status: &JobStatus{JobID: "job-1", State: JobStateFailed, FailureClass: JobFailureClassAbandoned, Error: "gone"},
	}, time.Now().UTC()); reason != "" {
		t.Fatalf("an unparked death concluded %q, want a requeue", reason)
	}
	if died.Status != TaskStatusPending {
		t.Fatalf("status = %s, want pending", died.Status)
	}
}

// TestService_RecordJobDeath_KeepsTheRunnersPark is the crash codex named,
// end to end through the service's own death path.
//
// THE WINDOW: the run parked its task, wrote its terminal failed status, and
// the daemon died before concludeLoop applied the conclusion. Nothing is left
// of the run — the lease reaper rebuilds the conclusion from the persisted
// record alone, and abandonedJobStatus stamps that record `abandoned`.
//
// Before the park was persisted this lost the judgement completely: the task
// went back in the queue and the next attempt ran the same prompt against the
// same unanswered question, which is go-quicbaas#787's loop surviving a
// restart. The task must come out blocked, not pending.
func TestService_RecordJobDeath_KeepsTheRunnersPark(t *testing.T) {
	svc, store := newConcludingService(t)
	svc.kvStore = newMockKVStore()
	ctx := context.Background()
	seeded := seedSpawnedTask(t, store, &Task{Attempts: 1})

	// What the dying run left behind: failed, and carrying its park.
	svc.persistJobStatus("acme", &JobStatus{
		JobID: "job-1", TenantID: "acme", State: JobStateFailed,
		Config: &JobConfig{TenantID: "acme", TaskID: seeded.ID},
		// Posted before the node died: the run announced it, then the
		// daemon went before the conclusion was applied.
		Park: &BlockReason{
			Kind:     TaskBlockKindNeedsInfo,
			Message:  "which store should hold the index?",
			Surfaced: true,
		},
	})

	lease := &JobLease{TenantID: "acme", Ref: seeded.ID, TaskID: seeded.ID, JobID: "job-1", Generation: 1, Node: "node-dead"}
	if err := svc.jobLeases.Acquire(ctx, lease); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := svc.RecordJobDeath(ctx, lease, "node-dead stopped"); err != nil {
		t.Fatalf("RecordJobDeath: %v", err)
	}

	task := readTask(t, store, seeded.ID)
	if task.Status != TaskStatusBlocked {
		t.Fatalf("status = %s, want blocked; the reaper requeued a task the runner parked", task.Status)
	}
	if task.BlockKind != TaskBlockKindNeedsInfo {
		t.Fatalf("block kind = %q, want needs_info", task.BlockKind)
	}
	if task.BlockedReason != "which store should hold the index?" {
		t.Fatalf("the worker's question did not survive the crash: %q", task.BlockedReason)
	}
	if task.BlockFaultClass != "" {
		t.Fatalf("fault class = %q; a park is not an environment fault", task.BlockFaultClass)
	}
}

// TestService_RecordJobDeath_WithoutAParkStillRequeues is the control: the
// death path is unchanged for every job that made no such judgement.
func TestService_RecordJobDeath_WithoutAParkStillRequeues(t *testing.T) {
	svc, store := newConcludingService(t)
	svc.kvStore = newMockKVStore()
	ctx := context.Background()
	seeded := seedSpawnedTask(t, store, &Task{Attempts: 1})

	svc.persistJobStatus("acme", &JobStatus{
		JobID: "job-1", TenantID: "acme", State: JobStateExecuting,
		Config: &JobConfig{TenantID: "acme", TaskID: seeded.ID},
	})
	lease := &JobLease{TenantID: "acme", Ref: seeded.ID, TaskID: seeded.ID, JobID: "job-1", Generation: 1, Node: "node-dead"}
	if err := svc.jobLeases.Acquire(ctx, lease); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := svc.RecordJobDeath(ctx, lease, "node-dead stopped"); err != nil {
		t.Fatalf("RecordJobDeath: %v", err)
	}

	task := readTask(t, store, seeded.ID)
	if task.Status != TaskStatusPending || task.Result.Metadata["failure_class"] != JobFailureClassAbandoned {
		t.Fatalf("expected a charged requeue, got %+v", task)
	}
}

// TestJobRun_ParkIsDurableBeforeTheStateIsPersisted is the earlier crash
// window: SetState SNAPSHOTS AND PERSISTS the state before the handler for
// that state ever runs, so a park recorded inside the handler leaves a
// durable record of the state carrying no park at all.
//
// The reaper rebuilds from that record alone, so the window between "the
// escalating status is durable" and "a later snapshot carries the park" was
// one in which a crash still requeued a parked task.
func TestJobRun_ParkIsDurableBeforeTheStateIsPersisted(t *testing.T) {
	t.Parallel()

	var persisted []*JobStatus
	run := NewJobRun(JobRunConfig{
		JobID:     "job-1",
		Config:    &JobConfig{TenantID: "tenant-a", TaskID: "task-1", Attempt: 1},
		Logger:    &agentTestLogger{},
		PersistFn: func(s *JobStatus) { persisted = append(persisted, s) },
	})

	// What the runner does the moment it decides: record the park, THEN
	// hand the state to the engine.
	run.Park(TaskBlockKindNeedsInfo, "which store should hold the index?", "")
	if err := run.SetState(context.Background(), JobStateEscalating); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	var escalating *JobStatus
	for _, s := range persisted {
		if s.State == JobStateEscalating {
			escalating = s
		}
	}
	if escalating == nil {
		t.Fatal("no escalating status was persisted")
	}
	if escalating.Park == nil {
		t.Fatal("the durable escalating status carries no park; a crash here requeues a parked task")
	}
	if escalating.Park.Kind != TaskBlockKindNeedsInfo {
		t.Fatalf("persisted park = %+v", escalating.Park)
	}
	if escalating.Park.Surfaced {
		t.Fatal("the park claims it was announced before anything posted it")
	}
}

// TestParkConclusion_TurnsOnWhetherTheRunAnnouncedIt: the whole point of the
// Surfaced flag. A park the run posted needs no second comment; a park the
// run could NOT post needs the conclusion to write one, and that is exactly
// the case where the run is unable to.
func TestParkConclusion_TurnsOnWhetherTheRunAnnouncedIt(t *testing.T) {
	t.Parallel()

	if got := parkConclusion(BlockReason{Kind: TaskBlockKindNeedsInfo, Surfaced: true}); got != TaskConcludedRunnerParked {
		t.Fatalf("a surfaced park concluded %q, want runner_parked", got)
	}
	for kind, want := range map[TaskBlockKind]TaskConclusionReason{
		TaskBlockKindNeedsInfo: TaskConcludedParkedNeedsInfo,
		TaskBlockKindBlocked:   TaskConcludedParkedBlocked,
		TaskBlockKindEscalated: TaskConcludedParkedEscalated,
	} {
		if got := parkConclusion(BlockReason{Kind: kind}); got != want {
			t.Fatalf("an unsurfaced %s park concluded %q, want %q", kind, got, want)
		}
	}
	// A kind JobRun.Park would refuse cannot reach here; if one ever did,
	// the answer must be the loud one rather than silence.
	if got := parkConclusion(BlockReason{Kind: "invented"}); got != TaskConcludedParkedEscalated {
		t.Fatalf("an unknown kind concluded %q, want the escalation", got)
	}
}

// TestApplyConclusion_AnUnsurfacedParkStillBlocksButIsNotSilent: the task
// stops either way — what changes is whether anything tells the person.
func TestApplyConclusion_AnUnsurfacedParkStillBlocksButIsNotSilent(t *testing.T) {
	t.Parallel()

	task := parkedTask()
	reason := applyConclusion(task, attemptConclusion{
		status: &JobStatus{JobID: "job-1", State: JobStateFailed, Error: "the park could not be posted"},
		park:   &BlockReason{Kind: TaskBlockKindNeedsInfo, Message: "which store?"},
	}, time.Now().UTC())

	if reason != TaskConcludedParkedNeedsInfo {
		t.Fatalf("conclusion = %q, want parked_needs_info so the seam posts it", reason)
	}
	if task.Status != TaskStatusBlocked || task.BlockKind != TaskBlockKindNeedsInfo {
		t.Fatalf("task = %s/%s, want blocked/needs_info", task.Status, task.BlockKind)
	}
}

// TestJobRun_ParkSurfacedIsPublishedAndPersisted: marking a park announced
// must reach the durable record immediately, not at whatever snapshot
// happens next. A death in that gap makes the reaper treat a park that WAS
// posted as unposted and post it again.
func TestJobRun_ParkSurfacedIsPublishedAndPersisted(t *testing.T) {
	t.Parallel()

	var persisted []*JobStatus
	run := NewJobRun(JobRunConfig{
		JobID:     "job-1",
		Config:    &JobConfig{TenantID: "tenant-a", TaskID: "task-1", Attempt: 1},
		Logger:    &agentTestLogger{},
		PersistFn: func(s *JobStatus) { persisted = append(persisted, s) },
	})
	run.Park(TaskBlockKindNeedsInfo, "which store?", "")
	if err := run.SetState(context.Background(), JobStateEscalating); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	before := len(persisted)

	run.ParkSurfaced()

	if len(persisted) != before+1 {
		t.Fatalf("persisted %d statuses, want one more for the surfaced park", len(persisted)-before)
	}
	if last := persisted[len(persisted)-1]; last.Park == nil || !last.Park.Surfaced {
		t.Fatalf("the persisted park is not marked surfaced: %+v", last.Park)
	}
	if published := run.Status(); published.Park == nil || !published.Park.Surfaced {
		t.Fatalf("the published status is not marked surfaced: %+v", published.Park)
	}
	// Idempotent: a second call publishes nothing new.
	run.ParkSurfaced()
	if len(persisted) != before+1 {
		t.Fatal("marking an already-surfaced park published again")
	}
}

// TestJobRun_ParkSurfacedDoesNotRaceAPublishedStatusReader is the -race
// guard. Status() hands the published status to concurrent readers, so the
// park it carries must be a copy the run cannot write through; before it was
// the run's own value, and marking it surfaced wrote into a struct other
// goroutines were reading.
func TestJobRun_ParkSurfacedDoesNotRaceAPublishedStatusReader(t *testing.T) {
	t.Parallel()

	run := NewJobRun(JobRunConfig{
		JobID:  "job-1",
		Config: &JobConfig{TenantID: "tenant-a", TaskID: "task-1", Attempt: 1},
		Logger: &agentTestLogger{},
	})
	run.Park(TaskBlockKindNeedsInfo, "which store?", "")
	if err := run.SetState(context.Background(), JobStateEscalating); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if status := run.Status(); status != nil && status.Park != nil {
				// Read every field the surfacing touches.
				_ = status.Park.Surfaced
				_ = status.Park.Kind
				_ = status.Park.Message
			}
		}
	}()

	for i := 0; i < 200; i++ {
		run.ParkSurfaced()
	}
	close(stop)
	<-done

	if status := run.Status(); status.Park == nil || !status.Park.Surfaced {
		t.Fatalf("park = %+v, want surfaced", status.Park)
	}
}

// TestJobRun_SnapshotDoesNotShareTheParkWithTheRun: the property behind the
// race guard, asserted directly. A published status must hold its own copy,
// so a later change on the run cannot reach back into a status somebody is
// already holding.
func TestJobRun_SnapshotDoesNotShareTheParkWithTheRun(t *testing.T) {
	t.Parallel()

	run := NewJobRun(JobRunConfig{
		JobID:  "job-1",
		Config: &JobConfig{TenantID: "tenant-a", TaskID: "task-1", Attempt: 1},
		Logger: &agentTestLogger{},
	})
	run.Park(TaskBlockKindNeedsInfo, "which store?", "")
	if err := run.SetState(context.Background(), JobStateEscalating); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	held := run.Status()
	if held.Park == run.Parked() {
		t.Fatal("the published status shares the run's own park value")
	}
	if held.Park.Surfaced {
		t.Fatal("the park is surfaced before anything posted it")
	}
}

// TestService_RecordJobDeath_ASurfacedParkIsNotPostedAgain: a node that dies
// AFTER the comment landed must not have the conclusion write a second one.
func TestService_RecordJobDeath_ASurfacedParkIsNotPostedAgain(t *testing.T) {
	svc, store := newConcludingService(t)
	svc.kvStore = newMockKVStore()
	ctx := context.Background()
	seeded := seedSpawnedTask(t, store, &Task{Attempts: 1})

	svc.persistJobStatus("acme", &JobStatus{
		JobID: "job-1", TenantID: "acme", State: JobStateFailed,
		Config: &JobConfig{TenantID: "acme", TaskID: seeded.ID},
		Park: &BlockReason{
			Kind: TaskBlockKindNeedsInfo, Message: "which store?", Surfaced: true,
		},
	})
	lease := &JobLease{TenantID: "acme", Ref: seeded.ID, TaskID: seeded.ID, JobID: "job-1", Generation: 1, Node: "node-dead"}
	if err := svc.jobLeases.Acquire(ctx, lease); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := svc.RecordJobDeath(ctx, lease, "node-dead stopped"); err != nil {
		t.Fatalf("RecordJobDeath: %v", err)
	}

	task := readTask(t, store, seeded.ID)
	if task.Status != TaskStatusBlocked {
		t.Fatalf("status = %s, want blocked", task.Status)
	}
	// The reason the module renders silent: the comment is already there.
	if got := parkConclusion(BlockReason{Kind: TaskBlockKindNeedsInfo, Surfaced: true}); got != TaskConcludedRunnerParked {
		t.Fatalf("a surfaced park concluded %q; the seam would post a duplicate", got)
	}
}

// TestJobRun_RestoreHydratesThePark is the mirror of making the park durable
// on the way in: a resumed run must bring it BACK, before anything it does
// writes a snapshot.
//
// snapshot() rebuilds JobStatus.Park from r.park every time, so a resumed
// run that started with none would publish a status with none and overwrite
// the only durable record of the judgement. Making the park survive the
// crash is worth nothing if the recovery erases it.
func TestJobRun_RestoreHydratesThePark(t *testing.T) {
	t.Parallel()

	persisted := &JobStatus{
		JobID: "job-1", TenantID: "tenant-a", State: JobStateEscalating,
		Iteration: 2,
		Park:      &BlockReason{Kind: TaskBlockKindNeedsInfo, Message: "which store?", Surfaced: true},
	}
	run := parkTestRun()
	run.restore(persisted)

	parked := run.Parked()
	if parked == nil {
		t.Fatal("the resumed run holds no park; its next snapshot erases the durable one")
	}
	if parked.Kind != TaskBlockKindNeedsInfo || parked.Message != "which store?" || !parked.Surfaced {
		t.Fatalf("restored park = %+v", parked)
	}
	if parked == persisted.Park {
		t.Fatal("the resumed run shares the persisted record's park value")
	}
	// And a run resumed from a status that never parked holds none.
	fresh := parkTestRun()
	fresh.restore(&JobStatus{JobID: "job-1", State: JobStateExecuting})
	if fresh.Parked() != nil {
		t.Fatalf("a resumed unparked run invented %+v", fresh.Parked())
	}
}

// TestJobRun_AResumedSnapshotKeepsThePark is the cycle end to end, and it is
// the one that would have caught this: park, persist, restart, and the FIRST
// status the resumed run writes must still carry the judgement — because a
// second crash concludes from exactly that record.
func TestJobRun_AResumedSnapshotKeepsThePark(t *testing.T) {
	t.Parallel()

	// 1. A run parks and its terminal status is persisted.
	var records []*JobStatus
	first := NewJobRun(JobRunConfig{
		JobID:     "job-1",
		Config:    &JobConfig{TenantID: "tenant-a", TaskID: "task-1", Attempt: 1},
		Logger:    &agentTestLogger{},
		PersistFn: func(s *JobStatus) { records = append(records, s) },
	})
	first.Park(TaskBlockKindNeedsInfo, "which store?", "")
	if err := first.SetState(context.Background(), JobStateEscalating); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	// It posted the comment before the daemon went, which is what makes the
	// conclusion below the silent one; the unsurfaced case is asserted
	// after.
	first.ParkSurfaced()
	durable := records[len(records)-1]
	if durable.Park == nil {
		t.Fatal("the first run persisted no park")
	}

	// 2. The daemon restarts and resumes from that record.
	var resumedRecords []*JobStatus
	second := NewJobRun(JobRunConfig{
		JobID:     "job-1",
		Config:    &JobConfig{TenantID: "tenant-a", TaskID: "task-1", Attempt: 1},
		Logger:    &agentTestLogger{},
		Resumed:   durable,
		PersistFn: func(s *JobStatus) { resumedRecords = append(resumedRecords, s) },
	})
	second.restore(durable)
	second.snapshot(JobStateEscalating)

	if len(resumedRecords) == 0 {
		t.Fatal("the resumed run persisted nothing")
	}
	after := resumedRecords[len(resumedRecords)-1]
	if after.Park == nil {
		t.Fatal("the resumed snapshot erased the park; a second crash requeues the task")
	}

	// 3. A second crash concludes from that record: parked, not abandoned.
	task := parkedTask()
	reason := applyConclusion(task, attemptConclusion{
		status: &JobStatus{
			JobID: "job-1", State: JobStateFailed,
			FailureClass: JobFailureClassAbandoned, Error: "node-dead stopped",
		},
		park: after.Park,
	}, time.Now().UTC())
	if reason != TaskConcludedRunnerParked {
		t.Fatalf("conclusion = %q, want runner_parked across the restart", reason)
	}
	if task.Status != TaskStatusBlocked {
		t.Fatalf("status = %s, want blocked", task.Status)
	}

	// And the same cycle for a park that never reached the issue: it still
	// survives the restart, and the conclusion is the one that POSTS it
	// rather than the silent one.
	unsurfaced := *after.Park
	unsurfaced.Surfaced = false
	quiet := parkedTask()
	if reason := applyConclusion(quiet, attemptConclusion{
		status: &JobStatus{JobID: "job-1", State: JobStateFailed, FailureClass: JobFailureClassAbandoned},
		park:   &unsurfaced,
	}, time.Now().UTC()); reason != TaskConcludedParkedNeedsInfo {
		t.Fatalf("conclusion = %q, want parked_needs_info so the seam posts it", reason)
	}
	if quiet.Status != TaskStatusBlocked {
		t.Fatalf("status = %s, want blocked", quiet.Status)
	}
}
