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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
)

// =============================================================================
// A task-bound job started through the public verb holds its attempt
// =============================================================================
//
// A JOB ACTS FOR ITS TASK ONLY WHILE THE TASK NAMES IT (Task.HoldsAttempt, see
// job_block.go). The engine's own spawners bind the task before the job exists
// (ClaimTaskSpawn), but a request through StartJob that names a task had
// nothing to write that binding. So the heartbeat fence and the sdlc runner's
// admission check aborted it as withdrawn before its first state.
//
// THE BINDING IS WRITTEN BEFORE THE JOB RUNS, under the store's version check.
// The verb mints the job's identity, binds the task to it, and only then
// starts the job with that identity, the same order autoSpawnTask uses. So the
// fence is never relaxed: a job that does not hold its task still has no
// authority over it.
//
//   - A task in the queue (pending) is claimed the way the dispatcher claims
//     it: ClaimTaskSpawn moves it to in_progress and charges the attempt.
//   - A task an agent already holds (claimed or in progress) and that names no
//     job is bound to the new job and nothing else. Its status and attempt
//     count stay the agent's; the job is the work the agent started for it.
//   - A task that already names a job has an open attempt, and a second job
//     would only be withdrawn, so the start is refused (TaskAttemptHeldError).
//   - A task that is parked or concluded cannot be attempted, so the start is
//     refused (TaskInvalidStateError).
//
// A START THAT FAILS AFTER THE BINDING GIVES IT BACK, so a refused
// configuration does not strand the task naming a job that never ran.

// startTaskBoundJob is StartJob for a request that names a task.
func (s *agentServiceImpl) startTaskBoundJob(ctx context.Context, req *StartJobRequest) (*StartJobResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}
	if s.taskStore == nil {
		return nil, &AgentServiceError{Sentinel: ErrTaskStoreRequired}
	}
	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = TenantFromContext(ctx)
	}
	if tenantID == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrTenantIDRequired,
			Message:  "tenant ID is required to start a job",
		}
	}

	jobID, err := generateJobID()
	if err != nil {
		return nil, &ControlLoopError{
			Sentinel: ErrControlLoopFailed,
			Message:  "failed to generate job ID: " + err.Error(),
		}
	}

	task, claimed, err := bindTaskJob(ctx, s.taskStore, tenantID, req.TaskID, jobID, time.Now().UnixNano())
	if err != nil {
		return nil, err
	}

	// The request is the caller's; the attempt ordinal the job runs as is the
	// binding's, when the binding charged one.
	bound := *req
	bound.TenantID = tenantID
	if claimed || bound.Attempt == 0 {
		bound.Attempt = task.Attempts
	}

	resp, err := s.startJob(ctx, &bound, jobID)
	if err != nil {
		if releaseErr := releaseTaskJob(ctx, s.taskStore, task, jobID, claimed); releaseErr != nil {
			s.logger.Warnf("orchestration: task=%s job=%s: the binding of a job that did not start could not be released: %s",
				task.ID, jobID, releaseErr.Error())
		}
		return nil, err
	}
	return resp, nil
}

// bindTaskJob makes jobID the task's open attempt. claimed reports that the
// binding came through ClaimTaskSpawn (the task was pending and was charged an
// attempt), which is what a release has to undo. See the note above.
func bindTaskJob(ctx context.Context, store TaskDispatchReasonStore, tenantID, taskID, jobID string, now int64) (*Task, bool, error) {
	var lastErr error
	for i := 0; i < cas.MaxAttempts; i++ {
		current, err := store.GetTask(ctx, tenantID, taskID)
		if err != nil {
			return nil, false, err
		}
		if current == nil {
			return nil, false, &TaskNotFoundError{TenantID: tenantID, TaskID: taskID}
		}
		if current.SpawnedJobID != "" {
			return nil, false, &TaskAttemptHeldError{TaskID: taskID, OpenJobID: current.SpawnedJobID}
		}
		if current.Status == TaskStatusPending {
			claim, claimErr := ClaimTaskSpawn(ctx, store, current, jobID, now)
			if claimErr != nil {
				return nil, false, claimErr
			}
			if !claim.Claimed {
				return nil, false, &TaskAttemptHeldError{TaskID: taskID, OpenJobID: claim.JobID}
			}
			return current, true, nil
		}
		if !taskAttemptOpen(current.Status) {
			return nil, false, &TaskInvalidStateError{
				TaskID:       taskID,
				CurrentState: current.Status,
				TargetState:  TaskStatusInProgress,
			}
		}
		current.SpawnedJobID = jobID
		current.SpawnedAt = now
		err = store.UpdateTask(ctx, current)
		if err == nil {
			return current, false, nil
		}
		var conflict *TaskVersionConflictError
		if !errors.As(err, &conflict) {
			return nil, false, err
		}
		lastErr = err
	}
	return nil, false, &TaskSpawnClaimError{
		TaskID:  taskID,
		Message: "the record changed under every attempt to bind it",
		Cause:   lastErr,
	}
}

// releaseTaskJob gives back a binding whose job did not start. A claim is
// released the way the dispatcher releases one (the attempt is refunded and the
// task returns to the queue); a binding onto a task an agent holds only clears
// the job it names. A task that no longer names jobID has nothing to release.
func releaseTaskJob(ctx context.Context, store TaskDispatchReasonStore, task *Task, jobID string, claimed bool) error {
	if claimed {
		return ReleaseTaskSpawnClaim(ctx, store, task, jobID)
	}
	var lastErr error
	for i := 0; i < cas.MaxAttempts; i++ {
		current, err := store.GetTask(ctx, task.TenantID, task.ID)
		if err != nil {
			return err
		}
		if current == nil || current.SpawnedJobID != jobID {
			return nil
		}
		current.SpawnedJobID = ""
		current.SpawnedAt = 0
		err = store.UpdateTask(ctx, current)
		if err == nil {
			return nil
		}
		var conflict *TaskVersionConflictError
		if !errors.As(err, &conflict) {
			return err
		}
		lastErr = err
	}
	return &TaskSpawnClaimError{
		TaskID:  task.ID,
		Message: "the record changed under every attempt to release the binding",
		Cause:   lastErr,
	}
}

// ErrTaskAttemptHeld indicates a job start refused because the task it names
// already has an open attempt.
var ErrTaskAttemptHeld = errors.New("orchestration: the task already has an open attempt")

// TaskAttemptHeldError reports a task-bound job start refused because the task
// already names another job as its open attempt.
type TaskAttemptHeldError struct {
	TaskID    string
	OpenJobID string
}

// Error implements the error interface.
func (e *TaskAttemptHeldError) Error() string {
	return ErrTaskAttemptHeld.Error() + ": task=" + e.TaskID + " open_job=" + e.OpenJobID
}

// Unwrap returns the sentinel for errors.Is support.
func (e *TaskAttemptHeldError) Unwrap() error { return ErrTaskAttemptHeld }
