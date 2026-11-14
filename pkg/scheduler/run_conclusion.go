package scheduler

import (
	"context"
	"time"
)

// RunConclusion names ONE execution of a job and the writer that wants to
// conclude it.
//
// A run is identified by its firing (the job's NextRun when it went Running)
// AND its running transition (the job's UpdatedAt as that transition stored
// it). The firing alone is not enough: a run interrupted by shutdown is
// re-armed at the SAME firing, so two executions can share one.
type RunConclusion struct {
	JobID     uint64
	TenantID  string
	Firing    time.Time
	StartedAt time.Time

	// Owner names the concluding writer. A retry by the same owner of the
	// same run wins again, which is what keeps a recovery retriable after
	// its conclusion was recorded and a later write failed.
	Owner string
}

// RunConcluder is the storage-level guarded transition that makes concluding
// a run safe across nodes (go-quicbaas#105).
//
// Concluding a run is a read-decide-write: the executing node writes its
// outcome, and a recovery writes the abandoned run's failure. Under a stripe
// mutex that is one node's exclusivity, not the fleet's, so a clustered
// engine routes every conclusion through this guard first: exactly one writer
// per run wins, and the loser leaves the record alone.
//
// ConcludeRun reports true when the caller concludes the run (or already did,
// as the same owner), and false when another writer concluded it or a LATER
// run of the job has already been concluded. A false is a loss, never a
// retry.
type RunConcluder interface {
	ConcludeRun(ctx context.Context, run *RunConclusion) (bool, error)
}

// recoveryOwner is the conclusion owner a recovery on this engine writes.
// It is stable per node, so a retried recovery recognises its own mark.
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
func (s *SchedulerEngine) noteRunStarted(job *Job) {
	if s.concluder == nil {
		return
	}
	s.startedRuns.Store(job.ID, RunConclusion{
		JobID:     job.ID,
		TenantID:  JobTenantID(job),
		Firing:    job.NextRun,
		StartedAt: job.UpdatedAt,
		Owner:     s.executionOwner(),
	})
}

// concludeOwnRun takes the guard for the run this node started on the job,
// if it started one. It reports whether the outcome may be written: false
// means another writer (a recovery that judged the run abandoned) concluded
// it, or the guard could not be read, and in both cases the record is left
// alone — an outcome that cannot prove it is the conclusion must not
// overwrite one that can.
func (s *SchedulerEngine) concludeOwnRun(ctx context.Context, job *Job) bool {
	if s.concluder == nil {
		return true
	}
	v, ok := s.startedRuns.LoadAndDelete(job.ID)
	if !ok {
		return true
	}
	run := v.(RunConclusion)
	won, err := s.concluder.ConcludeRun(ctx, &run)
	if err != nil {
		s.storageErrors.Add(1)
		return false
	}
	if !won {
		s.concludedElsewhere.Add(1)
		s.logWarn("Job %d: run of firing %s was concluded by another writer; its outcome is dropped",
			job.ID, run.Firing)
		return false
	}
	return true
}

// ConcludedElsewhere reports how many outcomes this engine dropped because
// another writer had already concluded the run.
func (s *SchedulerEngine) ConcludedElsewhere() uint64 {
	return s.concludedElsewhere.Load()
}
