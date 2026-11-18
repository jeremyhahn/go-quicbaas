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

package sdlc

import (
	"context"
	"errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Admission — an attempt acts for its task only while the task is still its
// =============================================================================
//
// A BLOCK OUTRANKS A PUBLICATION. On 2026-09-22 a task was blocked at 23:11,
// and its job pushed a branch and opened a duplicate pull request at 23:34
// anyway, because nothing on the delivery path ever looked at the task again
// after the job started (go-quicbaas#1054). The engine now aborts a blocked
// task's job when that job runs on the node that took the block. That leaves two
// gaps, and this check closes both:
//
//   - A JOB ON ANOTHER NODE cannot be aborted from the node that took the
//     block. It must notice for itself.
//   - A RACE. The block can land after the run decided to publish but before
//     the push or the pull request reaches the forge.
//
// So the runner re-reads its task before every state it enters, before every
// push and before opening a pull request, and stops the moment the task is no
// longer its own. The re-read is the whole mechanism. A cached task would
// answer "still mine" for exactly as long as the block went unnoticed.
//
// THE ATTEMPT'S IDENTITY IS WHAT IS CHECKED, NOT ONLY THE STATUS. A task
// blocked and then readmitted between two checks is pending again, a status a
// running attempt legitimately holds, yet readmission ended this attempt's
// binding. So admission asks whether the task still names THIS job as its open
// attempt (orchestration.Task.HoldsAttempt). A withdrawn attempt cannot have
// its authority restored by the readmission that follows its withdrawal.
//
// A TASK THAT CANNOT BE READ REFUSES TOO. A store read that fails means the
// attempt cannot show the task is still its own, and publishing work nobody
// may still want is the costly mistake: a duplicate pull request, a review
// spent on it, a person to clean it up. A push that does not happen is
// recovered by the next attempt from the branch it already has.

// attemptWithdrawn reports why jobID's attempt may no longer act for its
// task, or nil when it may.
//
// THE TASK MUST STILL NAME THIS JOB, IN A STATUS A RUNNING ATTEMPT HOLDS:
// pending, claimed or in progress. Anything else means the attempt was taken
// away: a person blocked it, a readmission ended its binding, or the task ended
// by some other path.
func attemptWithdrawn(task *orchestration.Task, jobID string) error {
	if task.HoldsAttempt(jobID) {
		return nil
	}
	return &AttemptWithdrawnError{
		TaskID: task.ID, JobID: jobID, Status: task.Status, BlockKind: task.BlockKind, OpenJobID: task.SpawnedJobID,
	}
}

// admitAttempt re-reads the task and answers whether jobID's attempt may still
// act for it. See the note at the top of this file.
func (h *Hooks) admitAttempt(ctx context.Context, tenantID, taskID, jobID string) error {
	task, err := h.tasks.GetTask(ctx, tenantID, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return &NotFoundError{Entity: "task", TenantID: tenantID, ID: taskID}
	}
	return attemptWithdrawn(task, jobID)
}

// =============================================================================
// Errors
// =============================================================================

// ErrAttemptWithdrawn indicates the task an attempt works was blocked or ended
// while the attempt ran, so the attempt stopped without publishing.
var ErrAttemptWithdrawn = errors.New("sdlc runner: the task was withdrawn from this attempt")

// AttemptWithdrawnError is the terminal error of an attempt whose task stopped
// being its own: blocked, readmitted, completed, failed or cancelled while it
// ran. JobID is the withdrawn attempt's job; OpenJobID is the job the task
// names as its open attempt now, empty when it names none.
type AttemptWithdrawnError struct {
	TaskID    string
	JobID     string
	Status    orchestration.TaskStatus
	BlockKind orchestration.TaskBlockKind
	OpenJobID string
}

// Error implements the error interface.
func (e *AttemptWithdrawnError) Error() string {
	msg := ErrAttemptWithdrawn.Error() + ": task " + e.TaskID + " is " + string(e.Status)
	if e.BlockKind != "" {
		msg += " (" + string(e.BlockKind) + ")"
	}
	if e.JobID != "" && e.OpenJobID != e.JobID {
		open := e.OpenJobID
		if open == "" {
			open = "none"
		}
		msg += " and its open attempt is " + open + ", not job " + e.JobID
	}
	return msg + "; nothing further is published"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AttemptWithdrawnError) Unwrap() error { return ErrAttemptWithdrawn }
