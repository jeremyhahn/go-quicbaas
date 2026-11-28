package scheduler

import (
	"context"
	"errors"
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
func outcomeTransition(run RunConclusion, apply runOutcome, owner string) RunTransition {
	return func(held *Job) (*Job, bool) {
		if !runOpen(held, &run) {
			return nil, false
		}
		// The outcome is applied to the record as it stands now, so an update,
		// or pending history, the execution never saw is kept, and the next
		// firing is scheduled from the job's current configuration.
		next := *held
		apply(&next)
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

// runOutcome applies a run's outcome (its status, counters, error and next
// firing) to a job record. It is applied to the record current when the
// outcome is written, never to the snapshot the run started from.
type runOutcome func(job *Job)

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
func (s *SchedulerEngine) concludeOwnRun(ctx context.Context, job *Job, apply runOutcome) (guarded, won bool) {
	if s.concluder == nil {
		return false, false
	}
	own, ok := ctx.Value(ownRunKey{}).(ownRun)
	if !ok || own.run == nil {
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
	transition := outcomeTransition(run, apply, s.executionOwner())
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

// runResultOf returns the result an execution must report for the run it
// owns, or nil when no caller waits on it.
func runResultOf(ctx context.Context) *ownRunResult {
	own, ok := ctx.Value(ownRunKey{}).(ownRun)
	if !ok {
		return nil
	}
	return own.result
}

// noteWriteError records a write of the run's outcome that failed, keeping
// the first one. A nil result (no caller waits) ignores it.
func (r *ownRunResult) noteWriteError(err error) {
	if r != nil && err != nil && r.err == nil {
		r.err = err
	}
}

// historyWriteFailed handles a run's history write that failed after its
// outcome was written. For a run whose result a caller waits on (a remote
// worker's completion), the record is kept pending on the job, where the
// next execution or any recovery writes it; only if that also fails is the
// caller told the result was not recorded. Other runs keep the best-effort
// history they had.
func (s *SchedulerEngine) historyWriteFailed(ctx context.Context, job *Job, exec *JobExecution, err error) {
	res := runResultOf(ctx)
	if res == nil {
		return
	}
	if keepErr := s.keepHistoryPending(ctx, job, exec); keepErr != nil {
		res.noteWriteError(ErrStorageError{Op: "SaveExecution", Cause: errors.Join(err, keepErr)})
	}
}

// keepHistoryPending appends exec to the job's PendingHistory under a
// stable ID. An absent job keeps nothing. The write is taken under the job's
// stripe lock, as every other single-node write of the job is.
func (s *SchedulerEngine) keepHistoryPending(ctx context.Context, job *Job, exec *JobExecution) error {
	lock := s.jobLock(job.ID)
	lock.Lock()
	defer lock.Unlock()

	rec := *exec
	if rec.ID == 0 {
		id, err := stableExecutionID(rec.EndedAt)
		if err != nil {
			return err
		}
		rec.ID = id
	}
	_, err := s.mutateJob(ctx, job.ID, JobTenantID(job), func(held *Job) (*Job, error) {
		next := *held
		next.PendingHistory = append(append([]*JobExecution(nil), held.PendingHistory...), &rec)
		return &next, nil
	})
	return err
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
