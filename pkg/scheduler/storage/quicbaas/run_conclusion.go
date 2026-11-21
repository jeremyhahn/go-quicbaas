package quicbaas

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// ConclusionsPrefix stores one run-conclusion mark per job: the last run of
// the job that a writer concluded, and which writer did.
//
// Same classification and namespace as ClaimsPrefix (see its comment): raw
// KV coordination metadata of the scheduler's own, in the reserved "__sys:"
// namespace, arbitrated by the transaction plane that decides firings.
const ConclusionsPrefix = "__sys:scheduler/conclusions/"

// ErrNilRunConclusion indicates a nil run was passed to ConcludeRun.
var ErrNilRunConclusion = errors.New("run conclusion cannot be nil")

// ErrIncompleteRunConclusion indicates a run that does not name its job,
// firing and running transition: it would collide with every other run.
var ErrIncompleteRunConclusion = errors.New("a run conclusion must name its job, firing and start")

// conclusionMark is the stored form. It carries no timestamps of its own, so
// the same owner concluding the same run marshals to the same bytes.
type conclusionMark struct {
	JobID        uint64 `json:"job_id"`
	FiringNanos  int64  `json:"firing_nanos"`
	StartedNanos int64  `json:"started_nanos"`
	Owner        string `json:"owner"`
}

func keyForConclusion(jobID uint64, tenantID string) string {
	return ConclusionsPrefix + scopeToken(tenantID) + strconv.FormatUint(jobID, 10)
}

// before reports whether m names an earlier run of the job than o.
func (m conclusionMark) before(o conclusionMark) bool {
	if m.FiringNanos != o.FiringNanos {
		return m.FiringNanos < o.FiringNanos
	}
	return m.StartedNanos < o.StartedNanos
}

// ConcludeRun implements scheduler.RunConcluder with a guarded write.
//
// The mark only moves forward. The caller wins when no mark exists, when the
// mark names an EARLIER run (that run's conclusion is history), or when it
// names this very run concluded by this same owner (a retry). It loses when
// another owner concluded this run, or when a LATER run of the job has been
// concluded — a zombie outcome of an old run must not overwrite a newer one.
func (s *ClusteredStorage) ConcludeRun(ctx context.Context, run *scheduler.RunConclusion) (bool, error) {
	if run == nil {
		return false, &scheduler.ErrStorageError{Op: "ConcludeRun", Cause: ErrNilRunConclusion}
	}
	if run.JobID == 0 || run.Firing.IsZero() || run.StartedAt.IsZero() {
		return false, &scheduler.ErrStorageError{Op: "ConcludeRun", Cause: ErrIncompleteRunConclusion}
	}
	mine := conclusionMark{
		JobID:        run.JobID,
		FiringNanos:  run.Firing.UnixNano(),
		StartedNanos: run.StartedAt.UnixNano(),
		Owner:        run.Owner,
	}
	data, err := json.Marshal(mine)
	if err != nil {
		return false, &scheduler.ErrStorageError{Op: "ConcludeRun.marshal", Cause: err}
	}

	return s.conclusions.Handover(ctx, keyForConclusion(run.JobID, run.TenantID), data,
		func(held []byte) (cas.Decision, error) {
			var current conclusionMark
			if err := json.Unmarshal(held, &current); err != nil {
				return cas.Decline, &scheduler.ErrStorageError{Op: "ConcludeRun.unmarshal", Cause: err}
			}
			if current.FiringNanos <= 0 {
				return cas.Decline, &scheduler.ErrStorageError{
					Op:    "ConcludeRun.read",
					Cause: ErrUnreadableClaimMark,
				}
			}
			switch {
			case current.before(mine):
				return cas.Take, nil
			case mine.before(current):
				return cas.Decline, nil
			case current.Owner == mine.Owner:
				// The same run, concluded by this owner already: swapping
				// the identical bytes succeeds, so the retry wins again.
				return cas.Take, nil
			default:
				return cas.Decline, nil
			}
		})
}
