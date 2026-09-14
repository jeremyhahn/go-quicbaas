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
)

// =============================================================================
// The service's side of job admission
// =============================================================================
//
// The service owns the node's identity, the lease store, and the registry of
// loops running here. It hands the first two to every loop it starts, and it
// is the liveness authority the reaper asks about the third. The finish path
// — what happens on the goroutine that ran a job, after its loop returns — is
// here too, because it is the same for a job started fresh and one resumed
// after a restart.

// jobConcludeTimeout bounds the task write and lease release that follow a
// loop's return.
const jobConcludeTimeout = 30 * time.Second

// SetNode names this daemon on every lease and job status it writes.
func (s *agentServiceImpl) SetNode(node string) {
	s.node = node
}

// SetJobLeaseStore replaces the lease store, and the TTL leases are judged
// by. The service starts with a single-process store so no job it runs is
// ever unleased; a clustered host installs the transaction-plane store.
func (s *agentServiceImpl) SetJobLeaseStore(store JobLeaseStore, ttl time.Duration) {
	if store == nil {
		return
	}
	s.jobLeases = store
	if ttl > 0 {
		s.leaseTTL = ttl
	}
}

// SetIdleThresholds configures the stall detector's per-state bounds. Must
// be called before the first Sweep.
func (s *agentServiceImpl) SetIdleThresholds(thresholds JobIdleThresholds) {
	s.idleThresholds = thresholds
}

// finishJob is what every job goroutine does after its run returns: it
// concludes the task the job was an attempt at, synchronously and exactly
// once, then lets the subscribers go.
func (s *agentServiceImpl) finishJob(tenantID string, run *JobRun, cancel context.CancelFunc, runErr error) {
	cancel()
	status := run.Status()
	if status == nil || status.JobID == "" {
		return
	}
	if run.Bound() {
		s.concludeLoop(tenantID, run, status, runErr)
	}
	// EVERY TERMINAL PATH, NOT ONLY THE DISCARD HOOK. A runner that closes
	// what it spawned finds nothing left to do here; one that failed, was
	// aborted mid-stage or panicked never got to its discard, and until this
	// call its sessions stayed ACTIVE with no process behind them. The
	// conclusion is driven from the durable records, so it covers sessions the
	// run stopped tracking as well as the ones it still holds.
	s.concludeJobSessions(tenantID, status.JobID)
	s.cleanupJobSubscribers(tenantID, status.JobID)
}

// concludeLoop writes a bound job's outcome to its task.
func (s *agentServiceImpl) concludeLoop(tenantID string, run *JobRun, status *JobStatus, loopErr error) {
	taskID := status.TaskID
	ctx, cancel := context.WithTimeout(ContextWithTenant(context.Background(), tenantID), jobConcludeTimeout)
	defer cancel()

	err := s.concludeAttempt(ctx, tenantID, taskID, attemptConclusion{
		status: status,
		fault:  run.Fault(),
		park:   run.Parked(),
		lease:  run.Lease(),
	})
	var concluded *AttemptConcludedError
	switch {
	case errors.As(err, &concluded):
		s.logger.Debugf("orchestration: job %s outcome not applied: %s", status.JobID, err.Error())
	case err != nil:
		s.logger.Warnf("orchestration: job %s outcome could not be written to task %s: %s", status.JobID, taskID, err.Error())
	default:
		s.logger.Debugf("orchestration: task %s concluded from job %s (state=%s class=%s)",
			taskID, status.JobID, status.State, status.FailureClass)
	}
	s.logAttribution(status, loopErr, err)
	s.publishEvent(ctx, tenantID, "task_auto_spawn_completed", map[string]string{
		"task_id":       taskID,
		"job_id":        status.JobID,
		"job_status":    string(status.State),
		"failure_class": status.FailureClass,
	})
}

// logAttribution says WHOSE FAULT a failed job was, once, on the goroutine
// that ran it.
//
// IT USED TO SAY "charged as a failure of the work" FOR EVERY PARK, including
// the ones it had just classified as an operator's to repair — two consecutive
// lines about one job, the second contradicting the first (go-quicbaas#908).
// The line is what a person greps when the accounting looks wrong, so a line
// that disagrees with the accounting is worse than no line: it sends the reader
// after a bug in the wrong half.
//
// Nothing is logged when the conclusion itself failed: the outcome was not
// applied, so no attribution was made, and the failure is already reported by
// the caller.
func (s *agentServiceImpl) logAttribution(status *JobStatus, loopErr, concludeErr error) {
	if loopErr == nil || concludeErr != nil || status.FailureClass != "" {
		return
	}
	if park := status.Park; park != nil && OperatorFaultClass(park.FaultClass) {
		s.logger.Debugf("orchestration: job %s attributed to the factory, not charged to the work (%s): %s",
			status.JobID, park.FaultClass, loopErr.Error())
		return
	}
	s.logger.Debugf("orchestration: job %s charged as a failure of the work: %s", status.JobID, loopErr.Error())
}

// JobRunning implements JobLivenessRegistry: a job is running here while its
// run is in the registry and has not returned.
func (s *agentServiceImpl) JobRunning(tenantID, jobID string) bool {
	val, ok := s.jobs.Load(tenantID + "/" + jobID)
	if !ok {
		return false
	}
	job, ok := val.(*runningJob)
	if !ok || job.run == nil {
		return false
	}
	return !job.run.Finished()
}

// RunningJobs implements JobRunSource: this node's running, task-bound jobs.
func (s *agentServiceImpl) RunningJobs() []*JobRun {
	var runs []*JobRun
	s.jobs.Range(func(_, val any) bool {
		job, ok := val.(*runningJob)
		if ok && job.run != nil && job.run.Bound() && !job.run.Finished() {
			runs = append(runs, job.run)
		}
		return true
	})
	return runs
}

// RecordJobDeath implements JobDeathRecorder: the job is marked failed and
// abandoned on its persisted record, its task is concluded, and its lease is
// released.
func (s *agentServiceImpl) RecordJobDeath(ctx context.Context, lease *JobLease, evidence string) error {
	if lease == nil {
		return &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a death names the lease it concludes"}
	}
	status := s.abandonedJobStatus(lease, evidence)
	s.persistJobStatus(lease.TenantID, status)
	// The job is terminal from this moment, and its sessions outlived the node
	// that ran them. Concluded here rather than left to the next sweep so the
	// operator's list agrees with the job's own record immediately.
	s.concludeJobSessions(lease.TenantID, lease.JobID)
	if lease.TaskID == "" {
		return s.releaseConcludedLease(ctx, lease)
	}
	return s.concludeAttempt(ContextWithTenant(ctx, lease.TenantID), lease.TenantID, lease.TaskID, attemptConclusion{
		status: status,
		// THE DEAD RUN'S JUDGEMENT, read off the record it left. The run
		// object is gone — that is what a death means — so the only place a
		// park can come from here is the persisted status, which is exactly
		// why JobStatus carries one.
		park:  status.Park,
		lease: lease,
	})
}

// abandonedJobStatus is the terminal status of a job whose node is gone,
// laid over whatever record the job persisted before it died.
func (s *agentServiceImpl) abandonedJobStatus(lease *JobLease, evidence string) *JobStatus {
	status := &JobStatus{
		JobID:    lease.JobID,
		TenantID: lease.TenantID,
		TaskID:   lease.TaskID,
		Attempt:  lease.Generation,
	}
	if s.kvStore != nil {
		if persisted, err := s.loadJobStatus(lease.TenantID, lease.JobID); err == nil {
			status = persisted
		}
	}
	status.State = JobStateFailed
	status.Error = evidence
	status.FailureClass = JobFailureClassAbandoned
	status.Node = lease.Node
	status.HeartbeatAt = lease.HeartbeatAt
	status.UpdatedAt = time.Now().UnixNano()
	return status
}

// JobSweepReport is the outcome of one maintenance pass.
type JobSweepReport struct {
	Reap  JobReapReport
	Stall JobStallReport

	// Sessions is what the session reconciliation decided. It runs in the
	// same pass because it reads the same two facts the other two write — a
	// job's terminal state and an agent's liveness — and a separate ticker
	// would only give those facts a second, differently-timed reader.
	Sessions SessionReconcileReport
}

// Sweep runs the reaper and the stall detector once. Both are no-ops when
// nothing is bound: no unreleased leases, no running task-bound loops.
func (s *agentServiceImpl) Sweep(ctx context.Context) (JobSweepReport, error) {
	if err := s.buildSweepers(); err != nil {
		return JobSweepReport{}, err
	}
	reap, err := s.reaper.Sweep(ctx)
	if err != nil {
		return JobSweepReport{}, err
	}
	report := JobSweepReport{Reap: reap, Stall: s.stalls.Detect(ctx)}

	// THE SESSION PASS RUNS LAST AND ITS FAILURE DOES NOT FAIL THE SWEEP. The
	// reaper and the detector have just moved jobs into terminal states, so
	// running after them lets this pass act on those endings in the same tick
	// rather than a tick later; and a session store that cannot be listed is
	// no reason to discard the two reports already in hand.
	sessions, err := s.SweepSessions(ctx)
	if err != nil {
		s.logger.Warnf("orchestration: session reconciliation could not run: %s", err.Error())
		return report, nil
	}
	s.logSessionSweep(sessions)
	report.Sessions = sessions
	return report, nil
}

// buildSweepers constructs the reaper and the detector once, from the wiring
// in place at the first sweep.
func (s *agentServiceImpl) buildSweepers() error {
	s.sweepOnce.Do(func() {
		reaper, err := NewJobReaper(s.jobLeases, s, s, s.leaseTTL)
		if err != nil {
			s.sweepErr = err
			return
		}
		stalls, err := NewJobStallDetector(s, s, s.idleThresholds)
		if err != nil {
			s.sweepErr = err
			return
		}
		s.reaper, s.stalls = reaper, stalls
	})
	return s.sweepErr
}
