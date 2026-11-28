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

// ErrNilRunConclusion indicates a nil run was passed to ConcludeRun.
var ErrNilRunConclusion = errors.New("run conclusion cannot be nil")

// ErrIncompleteRunConclusion indicates a run that does not name its job and
// start, or carries no transition.
var ErrIncompleteRunConclusion = errors.New("a run conclusion must name its job and start")

// ErrNoCompareAndSwap indicates a KV store without a guarded swap.
var ErrNoCompareAndSwap = errors.New("the kv store has no CompareAndSwap")

// ErrConclusionContended indicates a conclusion interrupted cas.MaxAttempts times.
var ErrConclusionContended = errors.New("run conclusion contended")

type compareAndSwapper interface {
	CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error)
}

// ConcludeRun implements scheduler.RunConcluder as a compare-and-swap of the
// job record itself: the transition decides against the record storage
// holds, and its result replaces exactly those bytes. An absent job loses,
// so a deleted job is never recreated; a record that moved is re-read and
// re-decided.
func (s *ClusteredStorage) ConcludeRun(ctx context.Context, run *scheduler.RunConclusion, transition scheduler.RunTransition) (bool, error) {
	if run == nil {
		return false, &scheduler.ErrStorageError{Op: "ConcludeRun", Cause: ErrNilRunConclusion}
	}
	if run.JobID == 0 || run.StartedAt.IsZero() || transition == nil {
		return false, &scheduler.ErrStorageError{Op: "ConcludeRun", Cause: ErrIncompleteRunConclusion}
	}
	swapper, ok := s.kv.(compareAndSwapper)
	if !ok {
		return false, &scheduler.ErrStorageError{Op: "ConcludeRun", Cause: ErrNoCompareAndSwap}
	}
	key := keyForJobByID(run.JobID, run.TenantID)
	for attempt := 0; attempt < cas.MaxAttempts; attempt++ {
		held, err := s.kv.Get(ctx, key)
		if err != nil {
			if dberrors.IsNotFound(err) {
				return false, nil
			}
			return false, &scheduler.ErrStorageError{Op: "ConcludeRun.get", Cause: err}
		}
		if len(held) == 0 {
			return false, nil
		}
		var job scheduler.Job
		if err := json.Unmarshal(held, &job); err != nil {
			return false, &scheduler.ErrStorageError{Op: "ConcludeRun.unmarshal", Cause: err}
		}
		next, take := transition(&job)
		if !take || next == nil {
			return false, nil
		}
		next.UpdatedAt = time.Now()
		data, err := json.Marshal(next)
		if err != nil {
			return false, &scheduler.ErrStorageError{Op: "ConcludeRun.marshal", Cause: err}
		}
		swapped, err := swapper.CompareAndSwap(ctx, key, held, data)
		if err != nil {
			return false, &scheduler.ErrStorageError{Op: "ConcludeRun.swap", Cause: err}
		}
		if swapped {
			return true, nil
		}
	}
	return false, &scheduler.ErrStorageError{Op: "ConcludeRun", Cause: ErrConclusionContended}
}
