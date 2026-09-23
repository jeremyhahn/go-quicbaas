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
	"testing"
	"time"
)

// TestApplyConclusion_ASupersededAttemptCompletesAsSuperseded: a job whose pull
// request was closed without merging completes its task and names why, the
// same way an already-satisfied one does (go-quicbaas#1054). It is neither a
// park waiting on the owner nor a failure. The task is completed, which is
// terminal, and its source is released so a re-triggered issue becomes a new
// task.
func TestApplyConclusion_ASupersededAttemptCompletesAsSuperseded(t *testing.T) {
	t.Parallel()

	task := parkedTask()
	satisfied := parkedTask()
	reason := applyConclusion(task, attemptConclusion{
		status: &JobStatus{JobID: "job-1", State: JobStateCompleted, Superseded: true},
	}, time.Now().UTC())
	applyConclusion(satisfied, attemptConclusion{
		status: &JobStatus{JobID: "job-1", State: JobStateCompleted, Satisfied: true},
	}, time.Now().UTC())

	if reason != TaskConcludedSuperseded {
		t.Fatalf("conclusion = %q, want superseded", reason)
	}
	if !reason.Valid() {
		t.Fatal("superseded is not a member of the closed conclusion vocabulary")
	}
	if task.Status != TaskStatusCompleted {
		t.Fatalf("status = %s, want completed", task.Status)
	}
	if task.BlockKind != "" || task.BlockedReason != "" {
		t.Fatalf("the task still carries a block (%q, %q): nobody is being asked anything",
			task.BlockKind, task.BlockedReason)
	}
	if got, want := task.ChargedAttempts(), satisfied.ChargedAttempts(); got != want {
		t.Fatalf("charged attempts = %d, want %d: counted the same as an already-satisfied attempt", got, want)
	}
	if taskTransitionAllowed(task.Status, TaskStatusPending) {
		t.Fatal("a superseded task can be moved back to pending; the outcome says it cannot be readmitted")
	}
	if !taskSourceReleasable(task.Status, task.BlockFaultClass) {
		t.Fatal("a superseded task holds its source, so re-triggering the issue could not adopt a new task")
	}
}

// TestJobRun_SupersedeTravelsOnTheStatus: the judgement is read from the
// persisted status, so it has to be there.
func TestJobRun_SupersedeTravelsOnTheStatus(t *testing.T) {
	t.Parallel()

	run := NewJobRun(JobRunConfig{JobID: "job-1", Config: &JobConfig{TaskID: "task-1"}})
	if run.Superseded() || run.snapshot(JobStateReviewing).Superseded {
		t.Fatal("a fresh run reports superseded")
	}
	run.Supersede()
	if !run.Superseded() || !run.snapshot(JobStateCompleted).Superseded {
		t.Fatal("Supersede did not reach the status")
	}
}
