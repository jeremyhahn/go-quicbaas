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
// BlockTask ends the job it interrupts (go-quicbaas#1054)
// =============================================================================

// TestBlockTask_EndsTheRunningJobAndReleasesItsSlot is the incident's own
// case: a task is blocked while its job runs. The job is aborted with the
// block as its cause, its lease is released, and the task stays blocked. The
// aborted job's conclusion must not requeue it.
func TestBlockTask_EndsTheRunningJobAndReleasesItsSlot(t *testing.T) {
	backend := newHoldingBackend()
	svc := newHoldingService(t, backend)
	store := NewInMemoryTaskStore()
	svc.SetTaskStore(store)
	ctx := ContextWithTenant(context.Background(), "acme")

	seeded := seedSpawnedTask(t, store, &Task{Status: TaskStatusClaimed, Attempts: 1, AttemptBudget: 3})
	resp := startBoundJob(ctx, t, svc, store, seeded.ID, &StartJobRequest{
		TenantID: "acme", Description: "bound", routedBackend: "mock",
		Attempt: 1, MaxIterations: 1,
	})
	defer cancelAllJobs(svc)
	awaitArrival(t, backend)

	loop := runningLoop(t, svc, "acme", resp.JobID)

	if err := svc.BlockTask(ctx, "acme", seeded.ID, TaskBlockKindBlocked, "an open pull request already closes the issue"); err != nil {
		t.Fatalf("BlockTask: %v", err)
	}

	var blocked *JobTaskBlockedError
	if !errors.As(loop.abortReason(), &blocked) || blocked.JobID != resp.JobID || blocked.TaskID != seeded.ID {
		t.Fatalf("the job must be ended with the block as its cause, got %v", loop.abortReason())
	}
	if !errors.Is(loop.abortReason(), ErrJobTaskBlocked) {
		t.Fatalf("the abort must unwrap to ErrJobTaskBlocked, got %v", loop.abortReason())
	}
	awaitFinished(t, loop)
	if svc.JobRunning("acme", resp.JobID) {
		t.Fatal("a job whose task was blocked is still running")
	}
	awaitLeaseReleased(ctx, t, svc.jobLeases, "acme", "", JobRunnerWorkflow, seeded.ID)

	task := readTask(t, store, seeded.ID)
	if task.Status != TaskStatusBlocked || task.BlockKind != TaskBlockKindBlocked {
		t.Fatalf("the aborted job's conclusion undid the block: %+v", task)
	}
}

// TestBlockTask_ATaskWithNoJobHereIsStillBlocked is the other side: a task
// whose job is not running on this node (or that names no job) is blocked
// exactly as before, and nothing is aborted.
func TestBlockTask_ATaskWithNoJobHereIsStillBlocked(t *testing.T) {
	svc := newHoldingService(t, newHoldingBackend())
	store := NewInMemoryTaskStore()
	svc.SetTaskStore(store)
	ctx := ContextWithTenant(context.Background(), "acme")

	seeded := seedSpawnedTask(t, store, &Task{Status: TaskStatusClaimed, SpawnedJobID: "job-on-another-node"})
	if err := svc.BlockTask(ctx, "acme", seeded.ID, TaskBlockKindEscalated, "held by the operator"); err != nil {
		t.Fatalf("BlockTask: %v", err)
	}
	task := readTask(t, store, seeded.ID)
	if task.Status != TaskStatusBlocked || task.SpawnedJobID != "job-on-another-node" {
		t.Fatalf("the block must land and keep the binding for readmission to clear: %+v", task)
	}
}

// TestBlockTask_ARefusedBlockAbortsNothing: a block the store refuses must
// not end the job. The task is still the job's, so the job keeps it.
func TestBlockTask_ARefusedBlockAbortsNothing(t *testing.T) {
	backend := newHoldingBackend()
	svc := newHoldingService(t, backend)
	store := NewInMemoryTaskStore()
	svc.SetTaskStore(store)
	ctx := ContextWithTenant(context.Background(), "acme")

	seeded := seedSpawnedTask(t, store, &Task{Status: TaskStatusClaimed})
	resp := startBoundJob(ctx, t, svc, store, seeded.ID, &StartJobRequest{
		TenantID: "acme", Description: "bound", routedBackend: "mock",
		Attempt: 1, MaxIterations: 1,
	})
	defer cancelAllJobs(svc)
	awaitArrival(t, backend)
	loop := runningLoop(t, svc, "acme", resp.JobID)

	if err := svc.BlockTask(ctx, "acme", seeded.ID, TaskBlockKindBlocked, ""); err == nil {
		t.Fatal("a block with no reason must be refused")
	}
	if reason := loop.abortReason(); reason != nil {
		t.Fatalf("a refused block aborted the job: %v", reason)
	}
	if !svc.JobRunning("acme", resp.JobID) {
		t.Fatal("a refused block ended the job")
	}
}

func TestJobTaskBlockedError_NamesTheTaskTheJobAndTheKind(t *testing.T) {
	err := &JobTaskBlockedError{TaskID: "task-1", JobID: "job-1", Kind: TaskBlockKindNeedsInfo}
	want := ErrJobTaskBlocked.Error() + ": task=task-1 job=job-1 kind=needs_info"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, ErrJobTaskBlocked) {
		t.Fatal("JobTaskBlockedError must unwrap to ErrJobTaskBlocked")
	}
}

// TestJobRun_AJobWhoseTaskWasBlockedElsewhereEndsItselfOnItsHeartbeat is the
// cross-node case: the block is written by ANOTHER node's verb, so this node's
// registry is never asked. The run's own heartbeat reads the task, aborts the
// job with the block as its cause, and the ordinary ending releases the lease
// and concludes the worker's session. The lease TTL is short so a heartbeat
// comes quickly.
func TestJobRun_AJobWhoseTaskWasBlockedElsewhereEndsItselfOnItsHeartbeat(t *testing.T) {
	backend := newHoldingBackend()
	svc := newHoldingService(t, backend)
	svc.SetTenantGuard(NewTenantGuard(&agentTestLogger{}))
	svc.SetJobLeaseStore(svc.jobLeases, 30*time.Millisecond)
	store := NewInMemoryTaskStore()
	svc.SetTaskStore(store)
	ctx := ContextWithTenant(context.Background(), "acme")

	seeded := seedSpawnedTask(t, store, &Task{Status: TaskStatusClaimed})
	resp := startBoundJob(ctx, t, svc, store, seeded.ID, &StartJobRequest{
		TenantID: "acme", Description: "bound", routedBackend: "mock",
		Attempt: 1, MaxIterations: 1,
	})
	defer cancelAllJobs(svc)
	awaitArrival(t, backend)
	spawned, err := svc.SpawnSession(ctx, &SpawnSessionRequest{Backend: "mock", JobID: resp.JobID})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	loop := runningLoop(t, svc, "acme", resp.JobID)

	// The STORE is written, not the service: this node's verb never runs.
	if err := store.BlockTask(ctx, "acme", seeded.ID, TaskBlockKindBlocked, "blocked on another node"); err != nil {
		t.Fatalf("BlockTask: %v", err)
	}

	awaitFinished(t, loop)
	var blocked *JobTaskBlockedError
	if !errors.As(loop.abortReason(), &blocked) || blocked.JobID != resp.JobID {
		t.Fatalf("the heartbeat must end the job with the block as its cause, got %v", loop.abortReason())
	}
	awaitLeaseReleased(ctx, t, svc.jobLeases, "acme", "", JobRunnerWorkflow, seeded.ID)
	awaitSessionConcluded(ctx, t, svc, "acme", spawned.SessionID)
	if task := readTask(t, store, seeded.ID); task.Status != TaskStatusBlocked {
		t.Fatalf("the aborted job's conclusion undid the block: %+v", task)
	}
}

// TestJobRun_AHeartbeatLeavesAJobWhoseTaskIsNotBlockedRunning is the other
// side: heartbeats that find the task still the job's end nothing.
func TestJobRun_AHeartbeatLeavesAJobWhoseTaskIsNotBlockedRunning(t *testing.T) {
	backend := newHoldingBackend()
	svc := newHoldingService(t, backend)
	svc.SetJobLeaseStore(svc.jobLeases, 30*time.Millisecond)
	store := NewInMemoryTaskStore()
	svc.SetTaskStore(store)
	ctx := ContextWithTenant(context.Background(), "acme")

	seeded := seedSpawnedTask(t, store, &Task{Status: TaskStatusClaimed})
	resp := startBoundJob(ctx, t, svc, store, seeded.ID, &StartJobRequest{
		TenantID: "acme", Description: "bound", routedBackend: "mock",
		Attempt: 1, MaxIterations: 1,
	})
	defer cancelAllJobs(svc)
	awaitArrival(t, backend)
	loop := runningLoop(t, svc, "acme", resp.JobID)

	// Wait for several heartbeats to have landed, measured by the lease.
	first := loop.Lease().HeartbeatAt
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	beats := 0
	for last := first; beats < 3; {
		select {
		case <-timer.C:
			t.Fatal("timed out waiting for heartbeats")
		case <-ticker.C:
		}
		if now := loop.Lease().HeartbeatAt; now != last {
			beats++
			last = now
		}
	}
	if reason := loop.abortReason(); reason != nil {
		t.Fatalf("a heartbeat ended a job whose task is still its own: %v", reason)
	}
}

// TestJobRun_ABlockAndReadmitBetweenHeartbeatsStillEndsTheJob is the gap a
// status-only fence left open: another node blocks the task AND readmits it
// before this job's next heartbeat. The task reads pending again, a status a
// running attempt holds, but readmission moved the job's binding into the
// history. The heartbeat must find the job is no longer the task's open
// attempt, end it, release its lease and conclude its session, and the old
// job's conclusion must leave the readmitted task exactly as readmission left
// it. The lease TTL is long so no ticker beat can land between the block and
// the readmission; the heartbeat is then driven directly.
func TestJobRun_ABlockAndReadmitBetweenHeartbeatsStillEndsTheJob(t *testing.T) {
	backend := newHoldingBackend()
	svc := newHoldingService(t, backend)
	svc.SetJobLeaseStore(svc.jobLeases, time.Hour)
	store := NewInMemoryTaskStore()
	svc.SetTaskStore(store)
	ctx := ContextWithTenant(context.Background(), "acme")

	seeded := seedSpawnedTask(t, store, &Task{Status: TaskStatusClaimed, Attempts: 1, AttemptBudget: 3})
	resp := startBoundJob(ctx, t, svc, store, seeded.ID, &StartJobRequest{
		TenantID: "acme", Description: "bound", routedBackend: "mock",
		Attempt: 1, MaxIterations: 1,
	})
	defer cancelAllJobs(svc)
	awaitArrival(t, backend)
	spawned, err := svc.SpawnSession(ctx, &SpawnSessionRequest{Backend: "mock", JobID: resp.JobID})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	loop := runningLoop(t, svc, "acme", resp.JobID)

	// Both writes land on the STORE, as another node's verbs would.
	if err := store.BlockTask(ctx, "acme", seeded.ID, TaskBlockKindBlocked, "blocked on another node"); err != nil {
		t.Fatalf("BlockTask: %v", err)
	}
	if err := store.ReadmitTask(ctx, "acme", seeded.ID); err != nil {
		t.Fatalf("ReadmitTask: %v", err)
	}
	readmitted := readTask(t, store, seeded.ID)
	if readmitted.Status != TaskStatusPending || readmitted.SpawnedJobID != "" {
		t.Fatalf("precondition: readmission must leave a pending task naming no job: %+v", readmitted)
	}
	if reason := loop.abortReason(); reason != nil {
		t.Fatalf("precondition: the job was ended before its heartbeat: %v", reason)
	}

	loop.heartbeat(ctx)

	var withdrawn *JobAttemptWithdrawnError
	if !errors.As(loop.abortReason(), &withdrawn) || withdrawn.JobID != resp.JobID || withdrawn.TaskID != seeded.ID {
		t.Fatalf("the heartbeat must end a job its task no longer names, got %v", loop.abortReason())
	}
	if !errors.Is(loop.abortReason(), ErrJobAttemptWithdrawn) {
		t.Fatalf("the abort must unwrap to ErrJobAttemptWithdrawn, got %v", loop.abortReason())
	}
	awaitFinished(t, loop)
	awaitLeaseReleased(ctx, t, svc.jobLeases, "acme", "", JobRunnerWorkflow, seeded.ID)
	awaitSessionConcluded(ctx, t, svc, "acme", spawned.SessionID)

	after := readTask(t, store, seeded.ID)
	if after.Status != TaskStatusPending || after.SpawnedJobID != "" || after.Attempts != readmitted.Attempts {
		t.Fatalf("the withdrawn job's conclusion touched the readmitted task: before %+v after %+v", readmitted, after)
	}
}

func TestTask_HoldsAttemptOnlyForTheJobItNamesWhileOpen(t *testing.T) {
	cases := []struct {
		name  string
		task  *Task
		jobID string
		holds bool
	}{
		{"bound and running", &Task{Status: TaskStatusInProgress, SpawnedJobID: "job-1"}, "job-1", true},
		{"bound and claimed", &Task{Status: TaskStatusClaimed, SpawnedJobID: "job-1"}, "job-1", true},
		{"readmitted: pending, no binding", &Task{Status: TaskStatusPending}, "job-1", false},
		{"re-dispatched to another job", &Task{Status: TaskStatusInProgress, SpawnedJobID: "job-2"}, "job-1", false},
		{"blocked, binding kept", &Task{Status: TaskStatusBlocked, SpawnedJobID: "job-1"}, "job-1", false},
		{"completed", &Task{Status: TaskStatusCompleted, SpawnedJobID: "job-1"}, "job-1", false},
		{"no job asks", &Task{Status: TaskStatusInProgress}, "", false},
		{"no task", nil, "job-1", false},
	}
	for _, tc := range cases {
		if got := tc.task.HoldsAttempt(tc.jobID); got != tc.holds {
			t.Errorf("%s: HoldsAttempt = %v, want %v", tc.name, got, tc.holds)
		}
	}
}

func TestJobAttemptWithdrawnError_NamesTheTaskTheJobAndTheOpenAttempt(t *testing.T) {
	err := &JobAttemptWithdrawnError{TaskID: "task-1", JobID: "job-1", Status: TaskStatusPending, OpenJobID: "job-2"}
	for _, want := range []string{"task=task-1", "job=job-1", "status=pending", "open_job=job-2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Error() = %q, missing %q", err.Error(), want)
		}
	}
	if !errors.Is(err, ErrJobAttemptWithdrawn) {
		t.Fatal("JobAttemptWithdrawnError must unwrap to ErrJobAttemptWithdrawn")
	}
}

// startBoundJob starts a job the way auto-spawn does: the task names the job
// as its open attempt BEFORE the job runs, so the job's first heartbeat
// already finds itself the task's attempt (ClaimTaskSpawn, then startJob).
func startBoundJob(ctx context.Context, t *testing.T, svc *agentServiceImpl, store TaskStore, taskID string, req *StartJobRequest) *StartJobResponse {
	t.Helper()
	jobID, err := generateJobID()
	if err != nil {
		t.Fatalf("generateJobID: %v", err)
	}
	bound := readTask(t, store, taskID)
	bound.SpawnedJobID = jobID
	if err := store.UpdateTask(ctx, bound); err != nil {
		t.Fatalf("bind task: %v", err)
	}
	req.TaskID = taskID
	resp, err := svc.startJob(ctx, req, jobID)
	if err != nil {
		t.Fatalf("startJob: %v", err)
	}
	if resp.JobID != jobID {
		t.Fatalf("startJob ran as %s, not the bound job %s", resp.JobID, jobID)
	}
	return resp
}

// awaitSessionConcluded waits for a session's record to stop being active.
func awaitSessionConcluded(ctx context.Context, t *testing.T, svc *agentServiceImpl, tenantID, sessionID string) {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		records, err := svc.store.ListSessions(ctx, tenantID)
		if err != nil {
			t.Fatalf("ListSessions: %v", err)
		}
		for _, record := range records {
			if record.SessionID == sessionID && !sessionRecordUnfinished(record) {
				return
			}
		}
		select {
		case <-timer.C:
			t.Fatalf("session %s was never concluded", sessionID)
		case <-ticker.C:
		}
	}
}

// TestJobRun_ObserveBlockReadsTheTaskUnderTheJobsTenant is the production
// shape of the heartbeat (the watchdog's X1): the lease ticker hands down a
// context that carries NO principal, and under a tenant guard the service
// refuses an untenanted read. The observer must bind the job's own tenant, or
// every read fails and a blocked task's job is never ended.
func TestJobRun_ObserveBlockReadsTheTaskUnderTheJobsTenant(t *testing.T) {
	svc := newHoldingService(t, newHoldingBackend())
	svc.SetTenantGuard(NewTenantGuard(&agentTestLogger{}))
	store := NewInMemoryTaskStore()
	svc.SetTaskStore(store)
	seeded := seedSpawnedTask(t, store, &Task{Status: TaskStatusClaimed, SpawnedJobID: "job-x1"})
	if err := store.BlockTask(context.Background(), "acme", seeded.ID, TaskBlockKindNeedsInfo, "which tenant?"); err != nil {
		t.Fatalf("BlockTask: %v", err)
	}

	run := &JobRun{
		JobID:  "job-x1",
		Config: &JobConfig{TenantID: "acme", TaskID: seeded.ID},
		Agents: svc,
		Logger: &agentTestLogger{},
	}
	run.observeBlock(context.Background())

	var blocked *JobTaskBlockedError
	if !errors.As(run.abortReason(), &blocked) || blocked.TaskID != seeded.ID || blocked.Kind != TaskBlockKindNeedsInfo {
		t.Fatalf("a blocked task must end its job even from an untenanted heartbeat, got %v", run.abortReason())
	}
}

// TestJobRun_ObserveBlockWithNoTenantAbortsNothing is the bad-input side: a
// job whose config names no tenant cannot be read, and a read that fails
// proves nothing, so the run keeps going.
func TestJobRun_ObserveBlockWithNoTenantAbortsNothing(t *testing.T) {
	svc := newHoldingService(t, newHoldingBackend())
	svc.SetTenantGuard(NewTenantGuard(&agentTestLogger{}))
	store := NewInMemoryTaskStore()
	svc.SetTaskStore(store)
	seeded := seedSpawnedTask(t, store, &Task{Status: TaskStatusClaimed, SpawnedJobID: "job-x1"})
	if err := store.BlockTask(context.Background(), "acme", seeded.ID, TaskBlockKindBlocked, "blocked"); err != nil {
		t.Fatalf("BlockTask: %v", err)
	}

	run := &JobRun{
		JobID:  "job-x1",
		Config: &JobConfig{TaskID: seeded.ID},
		Agents: svc,
		Logger: &agentTestLogger{},
	}
	run.observeBlock(context.Background())

	if reason := run.abortReason(); reason != nil {
		t.Fatalf("an unreadable task must abort nothing, got %v", reason)
	}
}
