package quicbaas

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// ErrIncompleteJobMutation indicates a job mutation that does not name its
// job, carries no mutation, or whose mutation produced no record.
var ErrIncompleteJobMutation = errors.New("a job mutation must name its job and carry a mutation")

// ErrJobMutationContended indicates a job mutation interrupted
// cas.MaxAttempts times.
var ErrJobMutationContended = errors.New("job mutation contended")

// MutateJob implements scheduler.GuardedJobWriter as a compare-and-swap of
// the job record: mutate decides against the record storage holds, and its
// result replaces exactly those bytes. A stale write is re-read and
// re-decided, so it can never restore a record another node has since
// concluded. An absent job is never written, so a mutation cannot recreate
// a job another node deleted.
func (s *ClusteredStorage) MutateJob(ctx context.Context, jobID uint64, tenantID string, mutate scheduler.JobMutation) (*scheduler.Job, error) {
	if jobID == 0 || mutate == nil {
		return nil, &scheduler.ErrStorageError{Op: "MutateJob", Cause: ErrIncompleteJobMutation}
	}
	swapper, ok := s.kv.(compareAndSwapper)
	if !ok {
		return nil, &scheduler.ErrStorageError{Op: "MutateJob", Cause: ErrNoCompareAndSwap}
	}
	key := keyForJobByID(jobID, tenantID)
	for attempt := 0; attempt < cas.MaxAttempts; attempt++ {
		held, err := s.kv.Get(ctx, key)
		if err != nil {
			if dberrors.IsNotFound(err) {
				return nil, nil
			}
			return nil, &scheduler.ErrStorageError{Op: "MutateJob.get", Cause: err}
		}
		if len(held) == 0 {
			return nil, nil
		}
		var job scheduler.Job
		if err := json.Unmarshal(held, &job); err != nil {
			return nil, &scheduler.ErrStorageError{Op: "MutateJob.unmarshal", Cause: err}
		}
		next, err := mutate(&job)
		if err != nil {
			return nil, err
		}
		if next == nil {
			return nil, &scheduler.ErrStorageError{Op: "MutateJob", Cause: ErrIncompleteJobMutation}
		}
		next.UpdatedAt = time.Now()
		data, err := json.Marshal(next)
		if err != nil {
			return nil, &scheduler.ErrStorageError{Op: "MutateJob.marshal", Cause: err}
		}
		swapped, err := swapper.CompareAndSwap(ctx, key, held, data)
		if err != nil {
			return nil, &scheduler.ErrStorageError{Op: "MutateJob.swap", Cause: err}
		}
		if swapped {
			return next, nil
		}
	}
	return nil, &scheduler.ErrStorageError{Op: "MutateJob", Cause: ErrJobMutationContended}
}
