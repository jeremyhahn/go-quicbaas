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
	"sort"
	"strconv"
	"time"
)

// =============================================================================
// The job reaper
// =============================================================================
//
// A JOB WHOSE NODE DIED LEAVES A TASK THAT NOBODY WILL EVER CONCLUDE. The
// loop that would have written the outcome is gone with its process; the
// restart-safety that used to come from a goroutine surviving now comes from
// here. The reaper reads the leases, and a lease whose heartbeat is older
// than the TTL, that is not released, and whose job is not running IN THIS
// PROCESS, is a job that stopped without saying so. It is concluded as a
// death: charged, requeued while the budget lasts, blocked with the evidence
// when it does not.
//
// THE LOCAL REGISTRY IS THE AUTHORITY FOR THIS NODE'S OWN JOBS. A lease held
// by a loop still running here is never reaped, however late its heartbeat —
// the store may be slow, and a slow holder is not a dead one. For another
// node's lease there is no authority to ask: the heartbeat IS the node's
// liveness, and the TTL is the deadline the node agreed to when it took the
// lease. That is the difference between reaping on a clock and reaping on a
// missed promise.
//
// Driven from the leases rather than from the tasks, because the leases are
// enumerable in one scan and each names its task; a task-driven pass would
// need every tenant listed first, and a task whose lease record is absent
// was never admitted at all.

// JobReapVerdict is what the reaper decided about one lease.
type JobReapVerdict string

const (
	// JobReapHealthy means the lease is live: its heartbeat is within the
	// TTL.
	JobReapHealthy JobReapVerdict = "healthy"

	// JobReapRunLive means the heartbeat is late but the job's loop is
	// running in this process, so nothing was reaped.
	JobReapRunLive JobReapVerdict = "run_live"

	// JobReapReaped means the lease was stale, the job was not running
	// here, and its death was recorded against its task.
	JobReapReaped JobReapVerdict = "reaped"

	// JobReapOrphaned means the lease was stale and dead but its task had
	// already moved past the job, or the job had no task; the lease was
	// released and nothing else was written.
	JobReapOrphaned JobReapVerdict = "orphaned"
)

// String implements fmt.Stringer.
func (v JobReapVerdict) String() string { return string(v) }

// JobReapFinding is one lease's outcome from a reaping pass.
type JobReapFinding struct {
	TenantID string
	JobID    string
	TaskID   string
	Node     string

	// Age is how long since the holder's last heartbeat.
	Age time.Duration

	Verdict  JobReapVerdict
	Evidence string
}

// JobReapReport is the outcome of one pass.
type JobReapReport struct {
	Findings []JobReapFinding
}

// Count returns how many findings reached verdict v.
func (r JobReapReport) Count(v JobReapVerdict) int {
	n := 0
	for _, f := range r.Findings {
		if f.Verdict == v {
			n++
		}
	}
	return n
}

// JobLivenessRegistry answers whether a job's loop is running in this
// process.
//
// The agent service is this registry: its jobs map holds every loop it
// started or resumed, and a loop that has returned reports itself finished.
type JobLivenessRegistry interface {
	JobRunning(tenantID, jobID string) bool
}

// JobDeathRecorder concludes a job whose node is gone.
//
// A PORT ONTO THE SERVICE'S OWN CONCLUSION PATH, so the reaper does not grow
// a second one. The service marks the job failed, writes the task through
// concludeAttempt and releases the lease; a task that has already moved past
// the job answers with an AttemptConcludedError, which the reaper reports as
// an orphaned lease rather than a failure.
type JobDeathRecorder interface {
	RecordJobDeath(ctx context.Context, lease *JobLease, evidence string) error
}

// JobReaper finds leases whose holders stopped and concludes them.
type JobReaper struct {
	leases   JobLeaseStore
	registry JobLivenessRegistry
	deaths   JobDeathRecorder
	ttl      time.Duration
	now      jobLeaseClock
}

// NewJobReaper builds a reaper. Every dependency is required: without the
// registry the reaper would reap this node's own slow jobs, and without the
// death recorder it would find dead jobs and repair nothing.
func NewJobReaper(leases JobLeaseStore, registry JobLivenessRegistry, deaths JobDeathRecorder, ttl time.Duration) (*JobReaper, error) {
	if leases == nil {
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a reaper reads leases and needs the store that holds them"}
	}
	if registry == nil {
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a reaper must be able to ask whether a job is running here, or it reaps its own slow jobs"}
	}
	if deaths == nil {
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a reaper concludes a job; without the death path it finds deaths and repairs none"}
	}
	if ttl <= 0 {
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "the TTL decides which leases are stale; a non-positive one makes every lease stale"}
	}
	return &JobReaper{leases: leases, registry: registry, deaths: deaths, ttl: ttl, now: utcNow}, nil
}

// Sweep examines every unreleased lease and concludes the dead ones.
//
// One lease's failure does not stop the pass: the finding carries the error
// and the sweep continues, because a store that cannot conclude one task is
// no reason to leave every other dead job unconcluded. The pass fails only
// when the leases cannot be listed at all.
func (r *JobReaper) Sweep(ctx context.Context) (JobReapReport, error) {
	leases, err := r.leases.List(ctx, "", "")
	if err != nil {
		return JobReapReport{}, err
	}
	var report JobReapReport
	for _, lease := range leases {
		if lease.Released {
			continue
		}
		report.Findings = append(report.Findings, r.examine(ctx, lease))
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].TenantID != report.Findings[j].TenantID {
			return report.Findings[i].TenantID < report.Findings[j].TenantID
		}
		return report.Findings[i].JobID < report.Findings[j].JobID
	})
	return report, nil
}

// examine decides one lease's fate.
func (r *JobReaper) examine(ctx context.Context, lease *JobLease) JobReapFinding {
	now := r.now()
	finding := JobReapFinding{
		TenantID: lease.TenantID,
		JobID:    lease.JobID,
		TaskID:   lease.TaskID,
		Node:     lease.Node,
		Age:      now.Sub(time.Unix(0, lease.HeartbeatAt)),
	}
	if !lease.Stale(now, r.ttl) {
		finding.Verdict = JobReapHealthy
		return finding
	}
	if r.registry.JobRunning(lease.TenantID, lease.JobID) {
		finding.Verdict = JobReapRunLive
		finding.Evidence = "heartbeat is " + finding.Age.String() +
			" old but the job's loop is running in this process; a slow holder is not a dead one"
		return finding
	}

	evidence := "job " + lease.JobID + " on node " + lease.Node + " stopped heartbeating " +
		finding.Age.String() + " ago at generation " + strconv.Itoa(lease.Generation) +
		" and is not running on this node (detected by the job reaper at " +
		now.Format(time.RFC3339) + ")"
	err := r.deaths.RecordJobDeath(ctx, lease, evidence)
	var concluded *AttemptConcludedError
	switch {
	case err == nil && lease.TaskID == "":
		finding.Verdict = JobReapOrphaned
		finding.Evidence = evidence + "; the job was bound to no task, so its lease was released"
	case err == nil:
		finding.Verdict = JobReapReaped
		finding.Evidence = evidence
	case errors.As(err, &concluded):
		finding.Verdict = JobReapOrphaned
		finding.Evidence = evidence + "; its task had already moved past it: " + err.Error()
	default:
		finding.Verdict = JobReapRunLive
		finding.Evidence = evidence + "; the death could not be recorded: " + err.Error()
	}
	return finding
}
