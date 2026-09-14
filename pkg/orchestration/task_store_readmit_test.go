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
)

// TestTaskTransitions_FailedIsReadmissible is the regression guard for
// go-quicbaas#945. `failed` was a terminal sink with NO outgoing transitions,
// so ReadmitTask — the only recovery verb — could never move a failed task and
// answered a bare 500. Nine tasks on the burn-in node died permanently from
// container and daemon faults during a deploy window because of it.
func TestTaskTransitions_FailedIsReadmissible(t *testing.T) {
	t.Parallel()
	if !taskTransitionAllowed(TaskStatusFailed, TaskStatusPending) {
		t.Fatal("failed -> pending must be allowed; without it no failed task can ever be recovered")
	}
}

// TestTaskTransitions_FailedGainsOnlyTheOneEdge pins the blast radius. Making
// `failed` re-openable in general would let a task that failed because the work
// is IMPOSSIBLE be picked up again and again; only an explicit, audited
// ReadmitTask moves it, and only to pending.
func TestTaskTransitions_FailedGainsOnlyTheOneEdge(t *testing.T) {
	t.Parallel()
	for _, target := range []TaskStatus{
		TaskStatusClaimed, TaskStatusInProgress, TaskStatusCompleted, TaskStatusBlocked,
	} {
		if taskTransitionAllowed(TaskStatusFailed, target) {
			t.Errorf("failed -> %s must stay refused; readmission goes through pending", target)
		}
	}
}

// TestTaskTransitions_CompletedStaysTerminal: the fix is about a task the FLEET
// broke, not about reopening finished work.
func TestTaskTransitions_CompletedStaysTerminal(t *testing.T) {
	t.Parallel()
	for _, target := range []TaskStatus{
		TaskStatusPending, TaskStatusClaimed, TaskStatusInProgress, TaskStatusFailed, TaskStatusBlocked,
	} {
		if taskTransitionAllowed(TaskStatusCompleted, target) {
			t.Errorf("completed -> %s must be refused; completed is terminal", target)
		}
	}
}

// TestReadmitTask_GrantsAnAllowanceToAnExhaustedTask is the other half of
// go-quicbaas#945. Moving a task to pending restores its state; the dispatcher
// then refuses it with "attempt budget exhausted" unless readmission also grants
// an allowance. Measured on the burn-in node: twelve readmitted tasks sat at
// pending=12 dispatched=0 until this existed.
func TestReadmitTask_GrantsAnAllowanceToAnExhaustedTask(t *testing.T) {
	t.Parallel()
	store := NewInMemoryTaskStore()
	seedTaskInStatus(t, store, "tenant-a", "task-1", TaskStatusFailed)

	// Spend the whole default budget, as a task that failed three times has.
	before, err := store.GetTask(context.Background(), "tenant-a", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	before.Attempts = DefaultAttemptBudget
	if err := store.UpdateTask(context.Background(), before); err != nil {
		t.Fatal(err)
	}
	if !before.BudgetExhausted() {
		t.Fatalf("fixture is not exhausted: charged=%d budget=%d", before.ChargedAttempts(), before.EffectiveAttemptBudget())
	}

	if err := store.ReadmitTask(context.Background(), "tenant-a", "task-1"); err != nil {
		t.Fatalf("ReadmitTask: %v", err)
	}

	after, err := store.GetTask(context.Background(), "tenant-a", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != TaskStatusPending {
		t.Fatalf("status = %s, want pending", after.Status)
	}
	if after.BudgetExhausted() {
		t.Error("a readmitted task must be dispatchable; it is still budget-exhausted")
	}
	// The history is preserved, not rewritten: the attempts it really made
	// remain, and none of them is reclassified as an environment fault.
	if after.Attempts != DefaultAttemptBudget {
		t.Errorf("Attempts = %d, want %d preserved", after.Attempts, DefaultAttemptBudget)
	}
	if after.EnvironmentFaults != 0 {
		t.Errorf("EnvironmentFaults = %d; readmission must not claim spent attempts were faults", after.EnvironmentFaults)
	}
	if after.ConclusionReason != TaskConcludedReadmitted {
		t.Errorf("ConclusionReason = %q, want %q", after.ConclusionReason, TaskConcludedReadmitted)
	}
}

// TestReadmitTask_LeavesAHealthyBudgetAlone: a task readmitted with attempts to
// spare needs no grant, and inflating its budget would quietly raise the ceiling
// for work that never hit it.
func TestReadmitTask_LeavesAHealthyBudgetAlone(t *testing.T) {
	t.Parallel()
	store := NewInMemoryTaskStore()
	seedTaskInStatus(t, store, "tenant-a", "task-1", TaskStatusBlocked)

	if err := store.ReadmitTask(context.Background(), "tenant-a", "task-1"); err != nil {
		t.Fatalf("ReadmitTask: %v", err)
	}
	after, err := store.GetTask(context.Background(), "tenant-a", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if after.AttemptBudget != 0 {
		t.Errorf("AttemptBudget = %d, want 0 (untouched, so the engine default applies)", after.AttemptBudget)
	}
}

// TestReadmitTask_ReturnsTheTaskToTheDISPATCHER is the third and last half of
// go-quicbaas#945, and the only one whose failure is silent.
//
// Readmission cleared the block, the claim, the retry hold and the budget, but
// left SpawnedJobID naming the dead job. partitionDispatchCandidates reads that
// field as "this task has a live attempt" and skips it with a bare `continue` —
// so the task is not dispatched AND not refused, meaning no reason is ever
// written to it and no line ever appears in the log. Measured on the burn-in
// node: pending=5 inflight=1 slots=0 dispatched=0, five readmitted tasks, and
// not one refusal naming any of them.
//
// The assertion is deliberately made through the dispatcher's own predicate
// rather than on the field: the field is the mechanism, but "a readmitted task
// is dispatchable" is the property, and asserting the property is what stops
// the next skip condition from re-stranding it.
func TestReadmitTask_ReturnsTheTaskToTheDispatcher(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewInMemoryTaskStore()
	seedTaskInStatus(t, store, "tenant-a", "task-1", TaskStatusBlocked)

	// A task blocked while a job held it: the block verb does not reach
	// applyConclusion, so nothing has cleared the binding.
	blocked, err := store.GetTask(ctx, "tenant-a", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	blocked.AutoSpawn = true
	blocked.SpawnedJobID = "job-dead"
	blocked.SpawnedAt = 1
	if err := store.UpdateTask(ctx, blocked); err != nil {
		t.Fatal(err)
	}

	if err := store.ReadmitTask(ctx, "tenant-a", "task-1"); err != nil {
		t.Fatalf("ReadmitTask: %v", err)
	}
	after, err := store.GetTask(ctx, "tenant-a", "task-1")
	if err != nil {
		t.Fatal(err)
	}

	ready, held, _ := partitionDispatchCandidates([]*Task{after}, 2)
	if len(ready) != 1 {
		t.Fatalf("ready = %d, held = %d; a readmitted task must be dispatchable, and being neither ready nor held makes it invisible", len(ready), len(held))
	}
	if after.SpawnedJobID != "" {
		t.Errorf("SpawnedJobID = %q, want empty; the attempt that job ran is over", after.SpawnedJobID)
	}
	if after.SpawnedAt != 0 {
		t.Errorf("SpawnedAt = %d, want 0", after.SpawnedAt)
	}
	// The job id is the only evidence the attempt happened — a task blocked by
	// the block verb never reached applyConclusion, so readmission is the one
	// chance to record it.
	if got := after.AttemptHistory(); len(got) != 1 || got[0] != "job-dead" {
		t.Errorf("AttemptHistory = %v, want [job-dead]; clearing the binding must not lose the attempt", got)
	}
}

// TestReadmitTask_WithNoOpenAttemptWritesNoHistory: the ordinary path, where
// applyConclusion already cleared the binding and recorded the job. Readmission
// must not invent a second, empty history entry — an attempt count that grows
// by readmission rather than by work would misreport every recovered task.
func TestReadmitTask_WithNoOpenAttemptWritesNoHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewInMemoryTaskStore()
	seedTaskInStatus(t, store, "tenant-a", "task-1", TaskStatusFailed)

	if err := store.ReadmitTask(ctx, "tenant-a", "task-1"); err != nil {
		t.Fatalf("ReadmitTask: %v", err)
	}
	after, err := store.GetTask(ctx, "tenant-a", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := after.AttemptHistory(); len(got) != 0 {
		t.Errorf("AttemptHistory = %v, want empty; there was no open attempt to record", got)
	}
}
