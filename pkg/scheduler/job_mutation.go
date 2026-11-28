package scheduler

import (
	"context"
	"errors"
)

// JobMutation decides a change against the job record storage holds and
// returns the record to write. It may run once per attempt of a guarded
// write, so it must have no side effects beyond setting locals of its
// caller.
type JobMutation func(held *Job) (*Job, error)

// GuardedJobWriter writes a job change across nodes without losing a
// concurrent write. MutateJob applies mutate to the stored record and writes
// the result only if the record is unchanged since it was read, re-reading
// and re-deciding otherwise. It returns the written record, returns (nil,
// nil) for an absent job (never recreating it), and returns mutate's error
// unchanged without writing.
type GuardedJobWriter interface {
	MutateJob(ctx context.Context, jobID uint64, tenantID string, mutate JobMutation) (*Job, error)
}

// errNoJobWrite is returned by a mutation to decline without error.
var errNoJobWrite = errors.New("scheduler: no job write needed")

// mutateJob applies mutate to the stored job and writes the result. A
// clustered engine writes through its GuardedJobWriter, so a write prepared
// before another node changed the record is re-decided against the change;
// a single-node engine reads and writes as it always has. A nil job means
// the job is absent. mutate's own error is returned as is.
func (s *SchedulerEngine) mutateJob(ctx context.Context, jobID uint64, tenantID string, mutate JobMutation) (*Job, error) {
	if s.jobWriter != nil {
		var decided error
		written, err := s.jobWriter.MutateJob(ctx, jobID, tenantID, func(held *Job) (*Job, error) {
			decided = nil
			next, err := mutate(held)
			decided = err
			return next, err
		})
		if err != nil {
			if decided != nil {
				return nil, decided
			}
			return nil, ErrStorageError{Op: "MutateJob", Cause: err}
		}
		return written, nil
	}
	held, err := s.storage.GetJob(ctx, jobID, tenantID)
	if err != nil {
		return nil, ErrStorageError{Op: "GetJob", Cause: err}
	}
	if held == nil {
		return nil, nil
	}
	next, err := mutate(held)
	if err != nil {
		return nil, err
	}
	if err := s.storage.UpdateJob(ctx, next); err != nil {
		return nil, ErrStorageError{Op: "UpdateJob", Cause: err}
	}
	return next, nil
}
