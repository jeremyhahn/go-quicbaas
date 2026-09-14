// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package scheduler

import (
	"context"
	"fmt"
	"time"
)

// =============================================================================
// Abandoned-run recovery
// =============================================================================
//
// A node that dies mid-execution leaves its job in running forever. Every later
// tick skips it, because a running job is not due, and nothing ever concludes
// it: the record says work is happening and no work is happening. That is the
// stranding docs/18 V4 records as pre-existing, and this is what closes it.
//
// THE ENGINE CANNOT DETECT THIS ITSELF, which is why the method takes an
// attestation instead of a flag. Deciding that a run is gone requires asking
// the plane the run lives on — a container that is no longer there, an agent
// daemon that no longer knows the session — and the scheduler has no such
// authority. The caller does. So the caller brings the proof, and the engine
// contributes what only it can: the lock, the guards, and the conclusion.

// Error codes for recovery refusals.
const (
	ErrCodeInvalidAttestation = "SCHEDULER_INVALID_ATTESTATION"
	ErrCodeRecoveryRefused    = "SCHEDULER_RECOVERY_REFUSED"
)

// AbandonedRunAttestation is a caller's evidence that a running job's execution
// is gone.
//
// Firing IS THE BINDING, and it is what makes a stale attestation harmless. It
// is the job's NextRun as the caller observed it, which for a running job names
// the firing being executed — the value advances only when a run completes. An
// attestation therefore describes ONE execution, and an observation made before
// a job completed and was rescheduled cannot conclude the execution that
// followed it. Structurally this is a review bound to a head SHA: the verdict
// does not become wrong later, it simply stops matching.
type AbandonedRunAttestation struct {
	// Firing is the job's NextRun when the caller observed it.
	Firing time.Time

	// Detector names what observed the run to be gone, and Detail says what it
	// saw. BOTH REQUIRED, for the reason a death report carries them: a job
	// returned to the queue by an evidence-free recovery is a job nobody can
	// explain, and LastError is the only place an operator will look.
	Detector string
	Detail   string

	// ObservedAt is when. When unset, the recovery resolves it from the
	// job's running transition — STABLE run data, never the clock: the
	// rendered evidence is the identity the idempotent history write is
	// looked up by, and a clock-stamped default made every retry render a
	// different record. A missing timestamp is a formatting gap rather than
	// missing evidence, so it is filled, not refused.
	ObservedAt time.Time
}

// RecoveryRefusal names why a recovery was declined.
//
// A FIELD RATHER THAN A MESSAGE, because the caller of this method is a triage
// sweep that must tell the two apart on every pass and act differently: one
// means the job already concluded and there is nothing to do, the other means
// the attestation is stale and the observation must be made again. Both are
// quiet, expected outcomes on a healthy fleet, and neither is discoverable by
// reading an error string — a caller reduced to matching text is the defect
// this codebase has already paid for once.
type RecoveryRefusal string

const (
	// RefusedNotRunning means the job is no longer in running: it concluded
	// between the observation and the recovery, or it was never running.
	//
	// This is the guard that makes the method unusable against healthy work.
	// There is no attestation that reaches a pending, paused, completed or
	// canceled job, so nothing here can requeue work that is waiting its turn
	// or reopen work that finished.
	RefusedNotRunning RecoveryRefusal = "not_running"

	// RefusedFiringMoved means the job is running a DIFFERENT firing from the
	// one attested: it completed, rescheduled and started again between the
	// observation and the recovery.
	//
	// Concluding on this would end a run nobody observed, which is precisely
	// the double-fire failure inverted — killing live work instead of
	// duplicating it.
	RefusedFiringMoved RecoveryRefusal = "firing_moved"
)

// String implements fmt.Stringer.
func (r RecoveryRefusal) String() string { return string(r) }

// InvalidAttestationError indicates an attestation that cannot support a
// conclusion. Every field it names is load-bearing: an attestation with no
// firing binds to no execution, and one with no evidence produces a requeued
// job nobody can explain.
type InvalidAttestationError struct {
	Field   string
	Message string
}

func (e InvalidAttestationError) Error() string {
	return fmt.Sprintf("invalid abandoned-run attestation: %s: %s", e.Field, e.Message)
}

// RecoveryRefusedError indicates a recovery the engine declined because the
// attestation no longer applies to the job in front of it.
//
// NOT A FAILURE. Both refusals are ordinary outcomes of a fleet where work
// completes while it is being examined, and a caller that treated them as
// errors would raise an alert every time the scheduler worked correctly.
type RecoveryRefusedError struct {
	JobID uint64

	// Refusal says which guard declined.
	Refusal RecoveryRefusal

	// Status is the status found. Meaningful for RefusedNotRunning.
	Status JobStatus

	// Attested and Found are the firing the caller observed and the firing the
	// job now carries. Meaningful for RefusedFiringMoved, and the pair is the
	// diagnosis: an operator needs to see that the job moved on, not merely
	// that something did not match.
	Attested time.Time
	Found    time.Time
}

func (e RecoveryRefusedError) Error() string {
	switch e.Refusal {
	case RefusedNotRunning:
		return fmt.Sprintf("job %d is %s, not running: an abandoned run can only be concluded "+
			"while the job still claims to be executing", e.JobID, e.Status)
	case RefusedFiringMoved:
		return fmt.Sprintf("job %d is running the firing at %s, not the attested %s: the job "+
			"moved on between the observation and this call",
			e.JobID, e.Found.UTC().Format(time.RFC3339Nano), e.Attested.UTC().Format(time.RFC3339Nano))
	default:
		return fmt.Sprintf("job %d recovery refused: %s", e.JobID, e.Refusal)
	}
}

// RecoverAbandonedRun concludes a job whose execution is gone, returning it to
// the queue or failing it as its retry budget allows.
//
// It routes through the engine's OWN failure path rather than moving the job
// itself. An abandoned run and a handler that returned an error are the same
// event as far as the retry budget is concerned — a run that produced no
// result — so they get the same accounting, the same backoff and the same
// terminal behaviour. A second transition written here would be a second
// policy, and the two would drift.
//
// That choice also disposes of the claim mark without touching it. A retry sets
// NextRun to now plus the backoff, which is ABOVE the mark the dead node left
// behind, so the recovered job is claimable again by construction. Lowering a
// monotonic mark — the alternative — un-decides a decided firing and lets a
// stalled contender win it a second time.
//
// WHAT THIS METHOD VERIFIES, AND WHAT IT DOES NOT. It cannot re-verify that the
// run is dead: it has no run authority, which is the entire reason the
// attestation is passed in. What it verifies is that the attestation STILL
// APPLIES — the job is still running, and still running the firing that was
// observed — which is the only part of the question the scheduler can decide
// from its own records. That is not complete protection, and the gap is stated
// rather than papered over: a long-running healthy handler leaves both status
// and firing unmoved, so an attestation that was WRONG WHEN MADE will conclude
// a live run. What protects healthy work is the caller's contract — that its
// liveness authority reports cannot-tell as an error and never as false — and
// that contract lives at the caller's seam because that is where it can be
// enforced. A method claiming more protection than it performs would be worse
// than this one, because somebody would rely on it.
//
// The engine need not be running. Unlike TriggerJob, which hands work to a
// dispatch loop, this only reads and writes the job record — and refusing while
// stopped would make recovery impossible in exactly the situation that produces
// abandoned runs.
func (s *SchedulerEngine) RecoverAbandonedRun(
	ctx context.Context,
	job *Job,
	att AbandonedRunAttestation,
) (JobStatus, error) {
	// A CLUSTERED ENGINE REFUSES, at the gate rather than in a comment. The
	// guards below run under a stripe mutex local to THIS engine and persist
	// with an unconditional UpdateJob; a second node can complete or advance
	// the same run between this node's read and its write, and recovery
	// would overwrite that newer state. On a single-node engine (claims are
	// nil exactly when New built it) in-process exclusivity is the COMPLETE
	// answer, and the mutex is that exclusivity. On a clustered engine
	// (NewWithClustering, which requires the claim plane) safe recovery
	// needs a storage-level guarded transition that does not exist yet —
	// so the capability is refused by name, never run with a known race.
	if s.claims != nil {
		return "", RecoveryUnsupportedClusteredError{
			Missing: "a storage-level guarded (compare-and-swap) job transition",
		}
	}
	if job == nil {
		return "", InvalidJobError{Field: "job", Message: "job cannot be nil"}
	}
	if att.Firing.IsZero() {
		return "", InvalidAttestationError{
			Field:   "firing",
			Message: "an attestation with no firing binds to no execution, so it could conclude any of them",
		}
	}
	if att.Detector == "" {
		return "", InvalidAttestationError{
			Field:   "detector",
			Message: "a recovery must name what observed the run to be gone",
		}
	}
	if att.Detail == "" {
		return "", InvalidAttestationError{
			Field:   "detail",
			Message: "a recovery must say what was observed; LastError is the only record an operator gets",
		}
	}

	// The engine's own stripe lock, taken for the whole read-guard-write, so
	// the guards decide against the record that is actually written. Every
	// other status writer takes it; a recovery that did not would be an
	// unguarded write beside guarded ones.
	lock := s.jobLock(job.ID)
	lock.Lock()
	defer lock.Unlock()

	storageCtx, cancel := context.WithTimeout(ctx, s.config.DefaultTimeout)
	defer cancel()

	current, err := s.storage.GetJob(storageCtx, job.ID, JobTenantID(job))
	if err != nil {
		if isJobNotFound(err) {
			return "", JobNotFoundError{JobID: job.ID}
		}
		s.storageErrors.Add(1)
		return "", ErrStorageError{Op: "GetJob", Cause: err}
	}
	if current == nil {
		// Deleted concurrently. Do not resurrect it — the same rule
		// persistJobOutcome follows.
		return "", JobNotFoundError{JobID: job.ID}
	}

	if current.Status != JobStatusRunning {
		return current.Status, RecoveryRefusedError{
			JobID:   job.ID,
			Refusal: RefusedNotRunning,
			Status:  current.Status,
		}
	}
	if !current.NextRun.Equal(att.Firing) {
		return current.Status, RecoveryRefusedError{
			JobID:    job.ID,
			Refusal:  RefusedFiringMoved,
			Status:   current.Status,
			Attested: att.Firing,
			Found:    current.NextRun,
		}
	}

	now := time.Now()
	// StartedAt is the running transition, which is the best start time that
	// survives the node that died: the execution record it would have written
	// was never saved, so UpdatedAt is the only evidence of when the run began.
	startedAt := current.UpdatedAt

	// An unset observation resolves HERE, from the same stable instant — not
	// from the clock inside the renderer. The evidence string is the identity
	// abandonedRunRecorded matches on; stamped with time.Now(), a retry whose
	// first attempt wrote history and then failed the transition rendered
	// DIFFERENT evidence, missed its own record, and duplicated the abandoned
	// run's history. The firing checker deliberately sends no timestamp, so
	// this is the production path, not an edge.
	if att.ObservedAt.IsZero() {
		att.ObservedAt = startedAt
	}

	// THE HISTORY RECORD IS WRITTEN BEFORE THE TRANSITION, and the order is
	// the contract. Committed the other way round, a SaveExecution failure
	// was PERMANENT: the job was already out of Running, a retried recovery
	// refused it, and the abandoned run's sole audit record could never be
	// written — history loss the method's own promise forbids. This way
	// every failure leaves the job Running and the whole recovery retriable.
	//
	// The write is idempotent by lookup, not by hope: a retry whose first
	// attempt wrote history and then failed the transition finds its own
	// record — same firing evidence, same StartedAt, which is stable because
	// the record it derives from has not changed while the job stays Running
	// — and does not write a second one.
	evidence := renderAttestation(att)
	already, err := s.abandonedRunRecorded(storageCtx, current.ID, JobTenantID(current), startedAt, evidence)
	if err != nil {
		s.storageErrors.Add(1)
		return "", ErrStorageError{Op: "GetExecutions", Cause: err}
	}
	if !already {
		exec := &JobExecution{
			JobID:     current.ID,
			Status:    JobStatusFailed,
			StartedAt: startedAt,
			EndedAt:   now,
			Duration:  now.Sub(startedAt),
			Error:     evidence,
		}
		if err := s.storage.SaveExecution(storageCtx, exec); err != nil {
			s.storageErrors.Add(1)
			return "", ErrStorageError{Op: "SaveExecution", Cause: err}
		}
	}

	s.applyRunFailure(current, evidence, now)

	// Written directly rather than through persistJobOutcome, which takes this
	// same non-reentrant lock. Its terminal-status guard is already satisfied
	// here by something stronger: the refusal above means a canceled or
	// completed job never reaches this line. A failure here leaves the job
	// Running WITH its history already recorded; the retry passes back
	// through the idempotent lookup and completes only the transition.
	if err := s.storage.UpdateJob(storageCtx, current); err != nil {
		s.storageErrors.Add(1)
		return "", ErrStorageError{Op: "UpdateJob", Cause: err}
	}
	s.failureCount.Add(1)

	return current.Status, nil
}

// abandonedRunRecorded reports whether a previous recovery attempt already
// wrote this abandoned run's history record.
//
// Matched on the firing's own identity — the job, the StartedAt the dead
// run's transition stamped, a failed status and the same evidence — so a
// retry after a failed transition completes instead of duplicating, and a
// LATER abandoned run of the same job (different StartedAt) is never
// mistaken for this one.
func (s *SchedulerEngine) abandonedRunRecorded(ctx context.Context, jobID uint64, tenantID string, startedAt time.Time, evidence string) (bool, error) {
	executions, err := s.storage.GetExecutions(ctx, jobID, tenantID, recoveryHistoryLookback)
	if err != nil {
		return false, err
	}
	for _, exec := range executions {
		if exec != nil && exec.Status == JobStatusFailed &&
			exec.StartedAt.Equal(startedAt) && exec.Error == evidence {
			return true, nil
		}
	}
	return false, nil
}

// recoveryHistoryLookback bounds the idempotency lookup. The record a retry
// is looking for is the newest entry or close to it — its own previous
// attempt wrote it — so a small window is enough, and an unbounded listing
// would scale the lookup with the job's whole history.
const recoveryHistoryLookback = 25

// renderAttestation composes what LastError and the execution record carry.
//
// The detector comes first because that is what tells an operator whether to
// believe it: a stall detector's observation and an operator's manual call are
// different claims about the same job.
func renderAttestation(att AbandonedRunAttestation) string {
	// No clock fallback here: the caller resolves an unset observation from
	// stable run data BEFORE rendering, because this string is the
	// idempotency identity and a renderer that consulted the clock made
	// every retry a different record.
	return fmt.Sprintf("run abandoned without a result: %s (observed by %s at %s, firing %s)",
		att.Detail, att.Detector,
		att.ObservedAt.UTC().Format(time.RFC3339Nano),
		att.Firing.UTC().Format(time.RFC3339Nano))
}

// RecoveryUnsupportedClusteredError reports RecoverAbandonedRun called on a
// clustered engine, where the recovery's read-modify-write is not safe.
//
// The refusal is the enforcement of a limitation that used to be prose in the
// doc-comment. It is lifted by giving JobStorage a guarded transition and
// routing the recovery through it; until then a clustered deployment must not
// be able to reach the race.
type RecoveryUnsupportedClusteredError struct {
	// Missing names the capability whose absence forces the refusal.
	Missing string
}

// Error implements the error interface.
func (e RecoveryUnsupportedClusteredError) Error() string {
	return "scheduler: abandoned-run recovery is single-node only: a clustered engine needs " +
		e.Missing + " before recovery may write over a run another node can still advance"
}
