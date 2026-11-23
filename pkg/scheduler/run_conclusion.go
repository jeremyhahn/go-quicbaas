package scheduler

import (
	"context"
	"time"
)

// RunConclusion names ONE execution of a job.
//
// A run is identified by its running transition: the job's RunStartedAt,
// which nothing but the next Running transition changes. Firing is carried
// for the log only and is never compared. Firing times are no generation:
// TriggerJob runs a job ahead of its NextRun, and the retry of that run is
// scheduled at now plus the backoff, which can be EARLIER than the firing
// the triggered run carried.
type RunConclusion struct {
	JobID     uint64
	TenantID  string
	Firing    time.Time
	StartedAt time.Time
}

// RunTransition computes the record that concludes a run from the job as
// storage holds it at the moment of the guarded write. ok=false means the run
// is not this writer's to conclude: the job moved on to another run, or the
// run was already concluded.
type RunTransition func(held *Job) (next *Job, ok bool)

// RunConcluder is the storage-level guarded transition that makes concluding
// a run safe across nodes (go-quicbaas#105).
//
// Concluding a run is a read-decide-write: the executing node writes its
// outcome, and a recovery writes the abandoned run's failure. Under a stripe
// mutex that is one node's exclusivity, not the fleet's, so a clustered
// engine writes every conclusion through this guard: a compare-and-swap of
// the JOB RECORD ITSELF. ConcludeRun reads the job, asks transition for the
// concluding record, and swaps it in only while the job still reads exactly
// as it was read; a moved record is re-read and re-decided.
//
// Arbitration and the transition are one write, so there is no window in
// which a run is decided but its job not yet written: a writer that dies, or
// whose write fails, leaves the job Running and the run open for recovery.
// A job that is absent (deleted) is never written, so a conclusion cannot
// recreate a job another node deleted.
//
// ConcludeRun reports true when the caller's record was written, and false
// when the job is gone or transition declined it. A false is a loss, never a
// retry.
type RunConcluder interface {
	ConcludeRun(ctx context.Context, run *RunConclusion, transition RunTransition) (bool, error)
}

// runOpen reports whether held is still running run and the run has not been
// concluded. ConcludedRunAt is what makes a run concludable once even when
// its conclusion keeps the record's status (a cancel written during the run).
func runOpen(held *Job, run *RunConclusion) bool {
	return runStartedAt(held).Equal(run.StartedAt) && !held.ConcludedRunAt.Equal(run.StartedAt)
}

// outcomeTransition concludes run with an execution's outcome. The outcome
// replaces the job while the run is open. A cancel or completion written
// during the run keeps its terminal status, the rule persistJobOutcome
// follows on one node; any other status means another writer concluded it.
func outcomeTransition(run RunConclusion, outcome *Job, owner string) RunTransition {
	return func(held *Job) (*Job, bool) {
		if !runOpen(held, &run) {
			return nil, false
		}
		next := *outcome
		switch held.Status {
		case JobStatusRunning:
		case JobStatusCanceled, JobStatusCompleted:
			next.Status = held.Status
		default:
			return nil, false
		}
		next.RunStartedAt, next.ConcludedRunAt, next.ConcludedBy = run.StartedAt, run.StartedAt, owner
		return &next, true
	}
}

// recoveryOwner is the conclusion owner a recovery on this engine writes.
// It is stable per node, so a retried recovery recognises its own conclusion.
func (s *SchedulerEngine) recoveryOwner() string {
	return "recovery@" + s.nodeID
}

// executionOwner is the conclusion owner the executing node writes.
func (s *SchedulerEngine) executionOwner() string {
	return "execution@" + s.nodeID
}

// noteRunStarted remembers the identity of the run this node just moved to
// Running, so its outcome can be concluded through the guard. Only a
// clustered engine with a concluder needs it.
func (s *SchedulerEngine) noteRunStarted(ctx context.Context, job *Job) context.Context {
	if s.concluder == nil {
		return ctx
	}
	run := RunConclusion{
		JobID:     job.ID,
		TenantID:  JobTenantID(job),
		Firing:    job.NextRun,
		StartedAt: runStartedAt(job),
	}
	s.startedRuns.Store(runKeyOf(job), run)
	return withOwnRun(ctx, ownRun{run: &run, started: true})
}

// concludeOwnRun writes job as the outcome of the run this execution owns,
// through the guarded transition. guarded is false when there is no guard or
// no run to conclude (the execution failed before its Running transition),
// and the caller then writes as a single node does. When guarded, won reports
// whether the outcome was written: false means another writer (a recovery
// that judged the run abandoned) concluded the run, the job was deleted, or
// the write failed, and in every case nothing was written. A failed write
// leaves the job Running, so the run stays open for recovery.
func (s *SchedulerEngine) concludeOwnRun(ctx context.Context, job *Job) (guarded, won bool) {
	if s.concluder == nil {
		return false, false
	}
	own, ok := ctx.Value(ownRunKey{}).(ownRun)
	if !ok {
		return false, false
	}
	// The execution carries the run it owns, so its conclusion always goes
	// through the guard, whatever the job's bookkeeping now holds: an old
	// execution finishing after a newer run started finds the job running
	// that newer run, loses, and removes only its own entry.
	run := *own.run
	if own.started {
		s.startedRuns.CompareAndDelete(runKeyOf(job), run)
	}
	var written *Job
	transition := outcomeTransition(run, job, s.executionOwner())
	won, err := s.concluder.ConcludeRun(ctx, &run, func(held *Job) (*Job, bool) {
		next, ok := transition(held)
		written = next
		return next, ok
	})
	if own.result != nil {
		own.result.won, own.result.err = won && err == nil, err
	}
	if err != nil {
		s.storageErrors.Add(1)
		s.logWarn("Job %d: the outcome of its run started %s could not be written: %v; the run stays open for recovery",
			job.ID, run.StartedAt, err)
		return true, false
	}
	if !won {
		s.concludedElsewhere.Add(1)
		s.logWarn("Job %d: run started %s was concluded by another writer, or the job is gone; its outcome is dropped",
			job.ID, run.StartedAt)
		return true, false
	}
	job.Status, job.ConcludedRunAt, job.ConcludedBy = written.Status, written.ConcludedRunAt, written.ConcludedBy
	return true, true
}

// ownRunKey is the context key of the run an execution owns.
type ownRunKey struct{}

// ownRun is an execution's authority over a run: the run to conclude;
// started when this node's noteRunStarted registered it; and result, when
// set, receives the conclusion's outcome for a caller that must report it.
type ownRun struct {
	run     *RunConclusion
	started bool
	result  *ownRunResult
}

// ownRunResult is how a guarded conclusion ended.
type ownRunResult struct {
	won bool
	err error
}

func withOwnRun(ctx context.Context, own ownRun) context.Context {
	return context.WithValue(ctx, ownRunKey{}, own)
}

// runKey names a job across tenants: two tenants' jobs may share an ID, so
// the per-run bookkeeping is keyed by both.
type runKey struct {
	TenantID string
	JobID    uint64
}

func runKeyOf(job *Job) runKey { return runKey{TenantID: JobTenantID(job), JobID: job.ID} }

// ConcludedElsewhere reports how many outcomes this engine dropped because
// another writer had already concluded the run.
func (s *SchedulerEngine) ConcludedElsewhere() uint64 {
	return s.concludedElsewhere.Load()
}
