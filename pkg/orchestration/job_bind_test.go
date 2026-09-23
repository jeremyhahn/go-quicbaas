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

// TestStartJob_APublicTaskBoundStartHoldsItsAttempt: a job started through the
// public verb with the documented TaskID field binds the task before it runs,
// so its heartbeat finds it the task's open attempt and leaves it running. A
// block then still ends it: the identity fence is not relaxed.
func TestStartJob_APublicTaskBoundStartHoldsItsAttempt(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       TaskStatus
		wantStatus   TaskStatus
		wantAttempts int
	}{
		{"queued task is claimed", TaskStatusPending, TaskStatusInProgress, 1},
		{"agent-held task is bound", TaskStatusClaimed, TaskStatusClaimed, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := newHoldingBackend()
			svc := newHoldingService(t, backend)
			svc.SetJobLeaseStore(svc.jobLeases, time.Hour)
			store := NewInMemoryTaskStore()
			svc.SetTaskStore(store)
			ctx := ContextWithTenant(context.Background(), "acme")

			seeded := seedUnboundTask(t, store, &Task{Status: tc.status, AttemptBudget: 3})
			resp, err := svc.StartJob(ctx, &StartJobRequest{
				TenantID: "acme", Description: "public", LeadBackend: "mock",
				TaskID: seeded.ID, MaxIterations: 1,
			})
			if err != nil {
				t.Fatalf("StartJob: %v", err)
			}
			defer cancelAllJobs(svc)
			awaitArrival(t, backend)
			loop := runningLoop(t, svc, "acme", resp.JobID)

			task := readTask(t, store, seeded.ID)
			if !task.HoldsAttempt(resp.JobID) || task.Status != tc.wantStatus || task.Attempts != tc.wantAttempts {
				t.Fatalf("the task must name the started job as its open attempt: %+v", task)
			}

			loop.heartbeat(ctx)
			if reason := loop.abortReason(); reason != nil {
				t.Fatalf("a publicly started task-bound job was withdrawn by its own heartbeat: %v", reason)
			}

			if err := svc.BlockTask(ctx, "acme", seeded.ID, TaskBlockKindBlocked, "stopped by the operator"); err != nil {
				t.Fatalf("BlockTask: %v", err)
			}
			if !errors.Is(loop.abortReason(), ErrJobTaskBlocked) {
				t.Fatalf("a block must still end the job, got %v", loop.abortReason())
			}
			awaitFinished(t, loop)
		})
	}
}

// TestStartJob_APublicStartOnATaskWithAnOpenAttemptIsRefused: a second job
// for a task that already names one would only be withdrawn, so it is refused
// before anything runs and the existing binding is left alone. So is a start
// for a task that is parked.
func TestStartJob_APublicStartOnATaskWithAnOpenAttemptIsRefused(t *testing.T) {
	svc := newHoldingService(t, newHoldingBackend())
	store := NewInMemoryTaskStore()
	svc.SetTaskStore(store)
	ctx := ContextWithTenant(context.Background(), "acme")

	held := seedSpawnedTask(t, store, &Task{Status: TaskStatusInProgress, SpawnedJobID: "job-open", Attempts: 1})
	_, err := svc.StartJob(ctx, &StartJobRequest{TenantID: "acme", Description: "second", LeadBackend: "mock", TaskID: held.ID})
	var heldErr *TaskAttemptHeldError
	if !errors.As(err, &heldErr) || heldErr.OpenJobID != "job-open" || !errors.Is(err, ErrTaskAttemptHeld) {
		t.Fatalf("want TaskAttemptHeldError naming job-open, got %v", err)
	}
	if !strings.Contains(err.Error(), "open_job=job-open") {
		t.Fatalf("the refusal must name the open attempt: %v", err)
	}
	if after := readTask(t, store, held.ID); after.SpawnedJobID != "job-open" || after.Attempts != 1 {
		t.Fatalf("a refused start touched the task: %+v", after)
	}

	parked := seedUnboundTask(t, store, &Task{Status: TaskStatusPending})
	if err := store.BlockTask(ctx, "acme", parked.ID, TaskBlockKindBlocked, "parked"); err != nil {
		t.Fatalf("BlockTask: %v", err)
	}
	_, err = svc.StartJob(ctx, &StartJobRequest{TenantID: "acme", Description: "parked", LeadBackend: "mock", TaskID: parked.ID})
	if !errors.Is(err, ErrTaskInvalidState) {
		t.Fatalf("a start for a blocked task must be refused, got %v", err)
	}
}

// TestStartJob_APublicStartThatFailsGivesTheBindingBack: a configuration the
// runner refuses is refused after the binding, so the binding is released and
// the task is left as it was, not naming a job that never ran.
func TestStartJob_APublicStartThatFailsGivesTheBindingBack(t *testing.T) {
	for _, status := range []TaskStatus{TaskStatusPending, TaskStatusClaimed} {
		t.Run(string(status), func(t *testing.T) {
			svc := newHoldingService(t, newHoldingBackend())
			store := NewInMemoryTaskStore()
			svc.SetTaskStore(store)
			ctx := ContextWithTenant(context.Background(), "acme")

			seeded := seedUnboundTask(t, store, &Task{Status: status})
			_, err := svc.StartJob(ctx, &StartJobRequest{
				TenantID: "acme", Description: "refused", LeadBackend: "mock",
				TaskID: seeded.ID, Runner: "no-such-runner",
			})
			if !errors.Is(err, ErrUnknownJobRunner) {
				t.Fatalf("want the runner's refusal, got %v", err)
			}
			after := readTask(t, store, seeded.ID)
			if after.SpawnedJobID != "" || after.Status != status || after.Attempts != 0 {
				t.Fatalf("a start that failed left the task bound: %+v", after)
			}
		})
	}
}

// TestBlockTask_ADelayedBlockLeavesTheReadmittedJobRunning is the interleaving
// a job lookup that ignored the task's status got wrong: the task is blocked,
// readmitted and dispatched again before the block's own lookup reads it. The
// job the task names then is the new, authorized attempt, and the stale
// block's lookup must not end it.
func TestBlockTask_ADelayedBlockLeavesTheReadmittedJobRunning(t *testing.T) {
	backend := newHoldingBackend()
	svc := newHoldingService(t, backend)
	svc.SetJobLeaseStore(svc.jobLeases, time.Hour)
	store := NewInMemoryTaskStore()
	svc.SetTaskStore(store)
	ctx := ContextWithTenant(context.Background(), "acme")

	seeded := seedUnboundTask(t, store, &Task{Status: TaskStatusPending, AttemptBudget: 3})
	if err := store.BlockTask(ctx, "acme", seeded.ID, TaskBlockKindBlocked, "blocked"); err != nil {
		t.Fatalf("BlockTask: %v", err)
	}
	if err := store.ReadmitTask(ctx, "acme", seeded.ID); err != nil {
		t.Fatalf("ReadmitTask: %v", err)
	}
	resp, err := svc.StartJob(ctx, &StartJobRequest{
		TenantID: "acme", Description: "readmitted", LeadBackend: "mock",
		TaskID: seeded.ID, MaxIterations: 1,
	})
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	defer cancelAllJobs(svc)
	awaitArrival(t, backend)
	loop := runningLoop(t, svc, "acme", resp.JobID)

	// The block's lookup runs only now, after the readmission and dispatch.
	svc.abortBlockedTaskJob(ctx, "acme", seeded.ID, TaskBlockKindBlocked)

	if reason := loop.abortReason(); reason != nil {
		t.Fatalf("a delayed block ended the readmitted job: %v", reason)
	}
	if !svc.JobRunning("acme", resp.JobID) {
		t.Fatal("a delayed block ended the readmitted job")
	}
	if task := readTask(t, store, seeded.ID); !task.HoldsAttempt(resp.JobID) {
		t.Fatalf("the readmitted task must still name its new job: %+v", task)
	}
}

// seedUnboundTask stores a task that names no job and has no attempts yet, the
// shape a task has before anything starts a job for it.
func seedUnboundTask(t *testing.T, store TaskStore, task *Task) *Task {
	t.Helper()
	task.ID = uniqueTaskID("unbound")
	task.TenantID = "acme"
	task.Subject = "work"
	if err := store.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task
}
