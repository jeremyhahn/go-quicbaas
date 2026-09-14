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
	"time"
)

// =============================================================================
// The job's side of the lease
// =============================================================================
//
// The run takes its lease before the first lifecycle moment — before any hook
// has a side effect and long before any session exists — and proves it is
// alive two ways: on every state transition (snapshot) and on a ticker at a
// third of the TTL, so a job parked in one long state still heartbeats. A
// heartbeat that finds another job holding the lease ABORTS the run — its
// context is cancelled and the terminal error names the generation that took
// over — because two runs on one unit of work is the failure the lease exists
// to prevent, and the older one standing down is the only correct answer.
//
// The ticker goroutine is owned by the run: started after the lease is taken,
// stopped before the run returns, and its exit is observable (heartbeatDone)
// so a test can prove nothing leaked.

// jobLeaseWriteTimeout bounds each lease write the run makes from its own
// goroutines. A wedged store must not hold a state transition open forever.
const jobLeaseWriteTimeout = 5 * time.Second

// abortCause is the error a run was told to stop with, from outside the
// runner: a heartbeat that found it superseded, or a stall detector.
type abortCause struct {
	err error
}

// SetJobLeasing gives the run a lease store, the node it runs on and the
// lease TTL. Without a store the run is unleased, which is correct for a run
// constructed outside a service.
func (r *JobRun) SetJobLeasing(store JobLeaseStore, node string, ttl time.Duration) {
	r.leases = store
	r.node = node
	r.leaseTTL = ttl
	if r.leaseTTL <= 0 {
		r.leaseTTL = DefaultJobLeaseTTL
	}
}

// Lease returns the lease the run holds, or nil before it is taken.
func (r *JobRun) Lease() *JobLease {
	return r.lease.Load()
}

// Finished reports whether the run has returned from execute.
func (r *JobRun) Finished() bool {
	return r.finished.Load()
}

// acquireJobLease takes the run's lease and starts the heartbeat. A run with
// no store is admitted without one.
func (r *JobRun) acquireJobLease(ctx context.Context, runner JobRunner) error {
	if r.leases == nil {
		return nil
	}
	// The task the lease names as its subject is read once, here, before
	// the lease is written.
	if err := r.loadTask(ctx); err != nil {
		return err
	}
	generation := r.Config.Attempt
	if generation < 1 {
		generation = 1
	}
	// THE RUNNER'S OWN LEASE POLICY IS READ HERE, once, before the record is
	// written, and from the runner the ENVELOPE IS ABOUT TO EXECUTE rather
	// than from a second lookup by name — one run has one runner, and a
	// lease named by a different one than the job goes on to use would be a
	// record about work nobody does. See JobLeasePolicy for why the subject
	// belongs to the runner and not to whoever started the job.
	lease := &JobLease{
		TenantID:   r.Config.TenantID,
		ProjectID:  r.Config.ProjectID,
		Workflow:   r.leaseWorkflow(),
		Ref:        r.leaseSubject(runner),
		TaskID:     r.Config.TaskID,
		JobID:      r.JobID,
		Generation: generation,
		Node:       r.node,
		Backend:    strings.TrimSpace(r.Config.LeadBackend),
		OffBudget:  runnerOffProjectBudget(runner),
	}
	if err := r.leases.Acquire(ctx, lease); err != nil {
		return err
	}
	r.lease.Store(lease)
	r.heartbeatAt.Store(lease.HeartbeatAt)
	r.Audit("orchestration:job_lease_acquired", "job_id="+r.JobID+" generation="+itoa(generation)+" node="+r.node)
	r.startHeartbeat(ctx)
	return nil
}

// leaseWorkflow names the shape of work the lease holds: the workflow a
// workflow job runs, or the runner driving every other shape.
func (r *JobRun) leaseWorkflow() string {
	if name := r.workflowName(); name != "" {
		return name
	}
	return r.Runner
}

// leaseSubject names the work the lease holds: the runner's own answer where
// it declares one, then the task's source reference, the task ID when the
// task has no source, and the job ID for a job bound to no task. The task
// itself was loaded by the envelope before the run started.
//
// THE RUNNER IS ASKED FIRST, and only a runner that declares a
// JobLeasePolicy answers at all. Without it a job bound to no task falls back
// to its own job id, which is unique per job and therefore serializes with
// nothing — right for a one-off, wrong for a runner whose jobs are
// per-subject singletons.
func (r *JobRun) leaseSubject(runner JobRunner) string {
	if subject := runnerLeaseSubject(runner, r.Config); subject != "" {
		return subject
	}
	if r.Config.TaskID == "" {
		return r.JobID
	}
	if r.Task != nil && r.Task.Source != nil && r.Task.Source.Ref != "" {
		return r.Task.Source.Ref
	}
	return r.Config.TaskID
}

// startHeartbeat runs the ticker goroutine until ctx ends.
func (r *JobRun) startHeartbeat(ctx context.Context) {
	done := make(chan struct{})
	r.heartbeatDone = done
	interval := r.leaseTTL / 3
	if interval <= 0 {
		interval = time.Millisecond
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.heartbeat(ctx)
			}
		}
	}()
}

// stopHeartbeat cancels the run and waits for the ticker goroutine to exit.
// Safe when no heartbeat was started.
func (r *JobRun) stopHeartbeat() {
	if cancel := r.cancelRun.Load(); cancel != nil {
		(*cancel)()
	}
	if r.heartbeatDone != nil {
		<-r.heartbeatDone
	}
}

// heartbeat proves the lease is still held, and aborts the run when it is
// not. Called from the ticker and from every state transition.
func (r *JobRun) heartbeat(ctx context.Context) {
	lease := r.lease.Load()
	if lease == nil || r.leases == nil {
		return
	}
	if ctx.Err() != nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, jobLeaseWriteTimeout)
	defer cancel()

	beat := *lease
	err := r.leases.Heartbeat(writeCtx, &beat)
	var superseded *JobSupersededError
	switch {
	case errors.As(err, &superseded):
		r.Abort(superseded)
		return
	case err != nil:
		r.Logger.Warnf("job run: job %s heartbeat failed: %s", r.JobID, err.Error())
		return
	}
	r.lease.Store(&beat)
	r.heartbeatAt.Store(beat.HeartbeatAt)
}

// releaseLease gives the lease back. Idempotent; a lease already released or
// held by another job is left alone by the store.
//
// Called from the run's own exit for a job bound to no task, and from
// CancelJob for any job. A BOUND job's lease is released by concludeAttempt
// AFTER the task is written: releasing first would open a window in which a
// crash leaves the task open with a released lease, which the reaper — which
// acts only on unreleased leases — could never repair.
func (r *JobRun) releaseLease() {
	lease := r.lease.Load()
	if lease == nil || r.leases == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), jobLeaseWriteTimeout)
	defer cancel()
	release := *lease
	if err := r.leases.Release(ctx, &release); err != nil {
		r.Logger.Warnf("job run: job %s lease release failed: %s", r.JobID, err.Error())
		return
	}
	r.lease.Store(&release)
}

// releaseOwnLease is the run-exit release, which applies only to a job bound
// to no task.
func (r *JobRun) releaseOwnLease() {
	if r.Bound() {
		return
	}
	r.releaseLease()
}

// Abort stops the run with err from outside the runner. The first cause
// wins; later ones are dropped so the terminal error is the one that
// actually ended the run.
func (r *JobRun) Abort(err error) {
	if err == nil {
		return
	}
	r.abortCause.CompareAndSwap(nil, &abortCause{err: err})
	if cancel := r.cancelRun.Load(); cancel != nil {
		(*cancel)()
	}
}

// abortReason returns the error the run was aborted with, or nil.
func (r *JobRun) abortReason() error {
	if cause := r.abortCause.Load(); cause != nil {
		return cause.err
	}
	return nil
}

// terminalError decides which error the run reports: an abort from outside
// outranks the runner error it caused, which is only ever the context
// cancellation the abort produced.
func (r *JobRun) terminalError(runnerErr error) error {
	if reason := r.abortReason(); reason != nil {
		return reason
	}
	return runnerErr
}
