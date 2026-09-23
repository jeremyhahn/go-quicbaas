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
)

// =============================================================================
// A block ends the job it interrupts
// =============================================================================
//
// A BLOCK THAT LEAVES ITS JOB RUNNING IS NOT A BLOCK. The verb used to write
// the task and nothing else, so the attempt it was meant to stop carried on:
// on 2026-09-22 a task blocked at 23:11 had its job open a duplicate pull
// request at 23:34, spending a worker session and a review on a change the
// operator had already stopped (go-quicbaas#1054).
//
// THE JOB IS ENDED THE WAY THE STALL DETECTOR ENDS ONE: JobRun.Abort. The run's
// context is cancelled, the runner unwinds through its own teardown (which
// closes the container), and finishJob concludes it. The conclusion is refused
// — the task is blocked, so its attempt is no longer open — and the lease is
// released either way (concludeAttempt). So the block needs no second
// conclusion path; it reuses the one every other ending takes.
//
// A JOB ON ANOTHER NODE IS ENDED BY ITS OWN HEARTBEAT. The verb can abort only
// what this node's registry holds, so every bound run also reads its task on
// each lease heartbeat (observeBlock) and aborts itself when the task no longer
// names it as the open attempt (see the fence below). That is the same Abort on the node that owns the run, within a
// third of the lease TTL, whatever the run is doing: a worker turn, a review
// wait, anything. The verb's own abort is the fast path for the common case.
// Meanwhile the sdlc delivery runner re-reads its task before every state and
// every publication, so nothing reaches the forge in the gap.
//
// THE FENCE IS THE ATTEMPT'S IDENTITY, NOT THE TASK'S STATUS. A heartbeat that
// asked only "is the task blocked?" is answered "no" by a task that was blocked
// AND readmitted between two beats: readmission returns it to pending and
// clears the binding, so the withdrawn job saw a pending task, kept running and
// could still publish. A job acts for its task only while the task still names
// it as the open attempt (Task.HoldsAttempt), the same binding concludeAttempt
// guards the conclusion with. Readmission moves the binding into the history,
// so the old job has no authority left to be restored.

// HoldsAttempt reports whether jobID is the task's open attempt: the task
// still names jobID in SpawnedJobID and its status still admits a running
// attempt. Anything else means the attempt was withdrawn, by a block, a
// readmission, a conclusion or another spawn, and the job may no longer act for
// the task.
func (t *Task) HoldsAttempt(jobID string) bool {
	return t != nil && jobID != "" && t.SpawnedJobID == jobID && taskAttemptOpen(t.Status)
}

// abortBlockedTaskJob ends the job a just-blocked task still names, when that
// job runs here.
//
// NEVER AN ERROR TO THE CALLER. The block is already durable when this runs,
// and a task that could not be read back is still blocked. The job that is not
// aborted here refuses to publish anyway (see above), so the failure is logged
// and the verb still succeeds.
func (s *agentServiceImpl) abortBlockedTaskJob(ctx context.Context, tenantID, taskID string, kind TaskBlockKind) {
	task, err := s.taskStore.GetTask(ctx, tenantID, taskID)
	if err != nil {
		s.logger.Warnf("orchestration: task %s was blocked but could not be read back to end its job: %s",
			taskID, err.Error())
		return
	}
	// ONLY A TASK THAT IS STILL BLOCKED names the job the block interrupted.
	// Between the block and this read the task can be readmitted and
	// dispatched again, and the job it names then is the new, authorized
	// attempt, which this block never meant to end.
	if task == nil || task.Status != TaskStatusBlocked {
		return
	}
	jobID := task.SpawnedJobID
	if jobID == "" {
		return
	}
	val, ok := s.jobs.Load(tenantID + "/" + jobID)
	if !ok {
		s.logger.Debugf("orchestration: task %s was blocked; its job %s is not running on this node and stops at its next state boundary",
			taskID, jobID)
		return
	}
	job, ok := val.(*runningJob)
	if !ok || job.run == nil || job.run.Finished() {
		return
	}
	job.run.Abort(&JobTaskBlockedError{TaskID: taskID, JobID: jobID, Kind: kind})
	s.logger.Infof("orchestration: task %s was blocked (%s); its running job %s was ended", taskID, kind, jobID)
}

// observeBlock is the heartbeat's half: a bound run whose task no longer names
// it as the open attempt aborts itself. See the note at the top of this file.
//
// A READ THAT FAILS PROVES NOTHING and aborts nothing. The block is durable,
// so the next heartbeat asks again. Ending a healthy job on a store hiccup
// would charge the work for the store's fault.
func (r *JobRun) observeBlock(ctx context.Context) {
	if r.Config == nil || r.Config.TaskID == "" || r.Agents == nil || ctx.Err() != nil {
		return
	}
	readCtx, cancel := context.WithTimeout(ctx, jobLeaseWriteTimeout)
	defer cancel()
	task, err := r.Agents.GetTask(readCtx, r.Config.TenantID, r.Config.TaskID)
	if err != nil {
		r.Logger.Debugf("job run: job %s could not read task %s to check for a block: %s",
			r.JobID, r.Config.TaskID, err.Error())
		return
	}
	if task == nil || task.HoldsAttempt(r.JobID) {
		return
	}
	if task.Status == TaskStatusBlocked && task.SpawnedJobID == r.JobID {
		r.Abort(&JobTaskBlockedError{TaskID: task.ID, JobID: r.JobID, Kind: task.BlockKind})
		return
	}
	r.Abort(&JobAttemptWithdrawnError{TaskID: task.ID, JobID: r.JobID, Status: task.Status, OpenJobID: task.SpawnedJobID})
}

// =============================================================================
// Errors
// =============================================================================

// ErrJobTaskBlocked indicates a job ended because its task was blocked while
// it ran.
var ErrJobTaskBlocked = errors.New("orchestration: job ended because its task was blocked")

// JobTaskBlockedError is the terminal error of a job whose task was blocked
// while it ran.
type JobTaskBlockedError struct {
	TaskID string
	JobID  string
	Kind   TaskBlockKind
}

// Error implements the error interface.
func (e *JobTaskBlockedError) Error() string {
	return ErrJobTaskBlocked.Error() + ": task=" + e.TaskID + " job=" + e.JobID + " kind=" + string(e.Kind)
}

// Unwrap returns the sentinel for errors.Is support.
func (e *JobTaskBlockedError) Unwrap() error { return ErrJobTaskBlocked }

// ErrJobAttemptWithdrawn indicates a job ended because its task no longer
// names it as the open attempt: the task was readmitted, concluded or bound to
// another job while this one ran.
var ErrJobAttemptWithdrawn = errors.New("orchestration: job ended because its task no longer names it as the open attempt")

// JobAttemptWithdrawnError is the terminal error of a job whose task stopped
// naming it as the open attempt while it ran. OpenJobID is the job the task
// names now, empty when it names none.
type JobAttemptWithdrawnError struct {
	TaskID    string
	JobID     string
	Status    TaskStatus
	OpenJobID string
}

// Error implements the error interface.
func (e *JobAttemptWithdrawnError) Error() string {
	return ErrJobAttemptWithdrawn.Error() + ": task=" + e.TaskID + " job=" + e.JobID +
		" status=" + string(e.Status) + " open_job=" + e.OpenJobID
}

// Unwrap returns the sentinel for errors.Is support.
func (e *JobAttemptWithdrawnError) Unwrap() error { return ErrJobAttemptWithdrawn }
