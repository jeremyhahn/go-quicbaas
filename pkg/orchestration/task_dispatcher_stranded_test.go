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

// strandedTask is a pending, auto-spawning task that names a job, with the claim
// aged by age so the sweep will judge it.
func strandedTask(id, jobID string, age time.Duration) *Task {
	task := pendingTask(id, 0, 100)
	task.SpawnedJobID = jobID
	task.SpawnedAt = time.Now().Add(-age).UnixNano()
	return task
}

// TestTaskDispatcher_FreesATaskWhoseJobHoldsNoLease is the regression guard for
// the worst failure mode this queue has: a task that is neither dispatched nor
// refused, so nothing is written to it and no line appears in the log.
//
// ClaimTaskSpawn writes the job's identity onto the task BEFORE starting the
// job, which is what deduplicates concurrent spawners. Its comment says the
// leftover — "a task naming a job with no live lease" — is "the state the
// reaper already exists to resolve". It is not: JobReaper.Sweep lists LEASES,
// so a task whose lease was never acquired, or whose lease was correctly
// released, has nothing for the reaper to find. Measured on the burn-in node:
// pending=4 inflight=0 slots=0 dispatched=0, and not one refusal naming any of
// them.
func TestTaskDispatcher_FreesATaskWhoseJobHoldsNoLease(t *testing.T) {
	t.Parallel()
	fix := newDispatcherFixture(t, 3, strandedTask("stranded", "job-gone", 2*testPollerLeaseTTL))

	if err := fix.dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	after, err := fix.tasks.GetTask(context.Background(), dispatchTenant, "stranded")
	if err != nil {
		t.Fatal(err)
	}
	if after.SpawnedJobID != "" {
		t.Errorf("SpawnedJobID = %q, want empty; the job it names holds no lease and is never coming back", after.SpawnedJobID)
	}
	// The job id is the only record the attempt happened, so freeing the
	// binding must not lose it.
	if got := after.AttemptHistory(); len(got) != 1 || got[0] != "job-gone" {
		t.Errorf("AttemptHistory = %v, want [job-gone]", got)
	}
	// And it is dispatched in the SAME sweep that freed it, rather than sitting
	// idle until the next one.
	if len(fix.spawner.spawned) != 1 {
		t.Errorf("spawned %d task(s), want the freed task dispatched in this sweep", len(fix.spawner.spawned))
	}
}

// TestTaskDispatcher_LeavesAFreshClaimAlone is the safety half, and it is the
// one that matters: the claim is written BEFORE the job starts, so a task
// legitimately names a job whose lease does not exist yet. Freeing that binding
// would let a second spawner charge a second attempt for work already starting
// — the exact double-charge the claim was introduced to prevent (#670).
func TestTaskDispatcher_LeavesAFreshClaimAlone(t *testing.T) {
	t.Parallel()
	// Claimed a moment ago, no lease yet: a spawn in flight.
	fix := newDispatcherFixture(t, 3, strandedTask("starting", "job-starting", time.Second))

	if err := fix.dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	after, err := fix.tasks.GetTask(context.Background(), dispatchTenant, "starting")
	if err != nil {
		t.Fatal(err)
	}
	if after.SpawnedJobID != "job-starting" {
		t.Errorf("SpawnedJobID = %q; a claim younger than the lease TTL must be left alone", after.SpawnedJobID)
	}
	if len(fix.spawner.spawned) != 0 {
		t.Errorf("spawned %d task(s); a task whose job is starting must not be dispatched again", len(fix.spawner.spawned))
	}
}

// TestTaskDispatcher_LeavesARunningJobsTaskAlone: an old claim whose job DOES
// hold a live lease is a long-running attempt, not a stranded one. Freeing it
// would abandon work in progress and start a second job for the same task.
func TestTaskDispatcher_LeavesARunningJobsTaskAlone(t *testing.T) {
	t.Parallel()
	fix := newDispatcherFixture(t, 3, strandedTask("running", "job-live", 10*testPollerLeaseTTL))
	fix.hold(t, "job-live")

	if err := fix.dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	after, err := fix.tasks.GetTask(context.Background(), dispatchTenant, "running")
	if err != nil {
		t.Fatal(err)
	}
	if after.SpawnedJobID != "job-live" {
		t.Errorf("SpawnedJobID = %q; the job holds a live lease and its attempt is under way", after.SpawnedJobID)
	}
	if len(fix.spawner.spawned) != 0 {
		t.Errorf("spawned %d task(s); a task with a live attempt must not be dispatched again", len(fix.spawner.spawned))
	}
}

// TestTaskDispatcher_RepairsNothingWithNoSpawnTimestamp: a binding with no
// SpawnedAt cannot be judged against the grace period, and guessing would mean
// freeing a claim that might be seconds old. It is left for an operator.
func TestTaskDispatcher_RepairsNothingWithNoSpawnTimestamp(t *testing.T) {
	t.Parallel()
	task := pendingTask("undated", 0, 100)
	task.SpawnedJobID = "job-undated"
	task.SpawnedAt = 0
	fix := newDispatcherFixture(t, 3, task)

	if err := fix.dispatcher.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	after, err := fix.tasks.GetTask(context.Background(), dispatchTenant, "undated")
	if err != nil {
		t.Fatal(err)
	}
	if after.SpawnedJobID != "job-undated" {
		t.Errorf("SpawnedJobID = %q; an unjudgeable claim must not be freed on a guess", after.SpawnedJobID)
	}
}
