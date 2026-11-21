package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// Remote handlers (go-quicbaas#1955)
// =============================================================================
//
// A handler registers in-process (RegisterHandler), so a client that reaches
// the scheduler over the wire had nothing to register on: every firing of its
// job failed with HandlerNotFoundError. A remote-backed node renewing its own
// certificate on a schedule never renewed it.
//
// A job whose handler is named RemoteHandlerPrefix+<name> is executed by a
// worker that PULLS its runs instead. The engine fires it as any job — the
// firing claim, the Running transition and the job's timeout are unchanged —
// and then waits for the run's result instead of calling a function:
//
//   - ListRemoteRuns returns the open runs of the named handlers, read from
//     the job store, so a worker can ask any node.
//   - CompleteRemoteRun reports one run's result. On the node that fired the
//     run the result is handed to the waiting execution, which records it
//     exactly as a local handler's (success, error, timeout). On another
//     node of a cluster it is concluded through the run guard (RunConcluder)
//     and recorded there; the firing node's own timeout then loses the guard
//     and writes nothing.
//
// A run is named by its RemoteRunRef — the job, its firing and its running
// transition — so a result for an earlier run of the job, or one already
// concluded, is refused rather than recorded against the wrong run.

// RemoteHandlerPrefix marks a job handler executed by a remote worker.
const RemoteHandlerPrefix = "remote:"

// IsRemoteHandler reports whether a job handler name is executed remotely.
func IsRemoteHandler(handler string) bool {
	return strings.HasPrefix(handler, RemoteHandlerPrefix) && len(handler) > len(RemoteHandlerPrefix)
}

// RemoteRunRef names one run of a remotely handled job.
type RemoteRunRef struct {
	JobID     uint64    `json:"job_id"`
	TenantID  string    `json:"tenant_id,omitempty"`
	Firing    time.Time `json:"firing"`
	StartedAt time.Time `json:"started_at"`
}

// RemoteRun is an open run handed to a remote worker.
type RemoteRun struct {
	RemoteRunRef
	Name    string          `json:"name"`
	Handler string          `json:"handler"`
	Payload json.RawMessage `json:"payload,omitempty"`
	// Deadline is when the engine stops waiting and records the run as
	// timed out; a result reported after it is refused.
	Deadline time.Time `json:"deadline"`
}

// ErrRemoteRunNotOpen is returned by CompleteRemoteRun when the named run is
// not open: the job is not running that run, or it was already concluded.
var ErrRemoteRunNotOpen = errors.New("remote run is not open")

// ErrNotRemoteHandler is returned when a name is not a remote handler name.
var ErrNotRemoteHandler = errors.New("handler is not a remote handler")

// RemoteRunNotOpenError carries the run a refused completion named.
type RemoteRunNotOpenError struct {
	Ref    RemoteRunRef
	Reason string
}

func (e RemoteRunNotOpenError) Error() string {
	return "remote run of job " + strconv.FormatUint(e.Ref.JobID, 10) + " is not open: " + e.Reason
}

// Unwrap lets errors.Is match ErrRemoteRunNotOpen.
func (e RemoteRunNotOpenError) Unwrap() error { return ErrRemoteRunNotOpen }

// remoteOutcome is a remote worker's report of a run.
type remoteOutcome struct {
	result *JobResult
	err    error
}

// remoteWaiter is the execution of a remote run waiting on its result. It is
// open until it receives a result or its wait ends; a result offered after
// that is refused, so a result is never accepted and then discarded.
type remoteWaiter struct {
	ref RemoteRunRef

	mu       sync.Mutex
	closed   bool
	accepted bool               // a result was delivered (set with closed)
	outcome  chan remoteOutcome // buffered 1
}

// deliver hands the waiting execution its result, reporting false when the
// wait has already ended.
func (w *remoteWaiter) deliver(o remoteOutcome) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return false
	}
	w.closed = true
	w.accepted = true
	w.outcome <- o
	return true
}

// close ends the wait; it reports false when a result was delivered first.
func (w *remoteWaiter) close() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return false
	}
	w.closed = true
	return true
}

// delivered reports whether the wait ended with a result rather than by
// close.
func (w *remoteWaiter) delivered() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.accepted
}

func runRefOf(job *Job) RemoteRunRef {
	return RemoteRunRef{JobID: job.ID, TenantID: JobTenantID(job), Firing: job.NextRun, StartedAt: job.UpdatedAt}
}

func (r RemoteRunRef) names(job *Job) bool {
	return job.ID == r.JobID && job.NextRun.Equal(r.Firing) && job.UpdatedAt.Equal(r.StartedAt)
}

func (r RemoteRunRef) same(o RemoteRunRef) bool {
	return r.JobID == o.JobID && r.TenantID == o.TenantID && r.Firing.Equal(o.Firing) && r.StartedAt.Equal(o.StartedAt)
}

// openRemoteRun registers the waiting execution of the run job is now
// Running. Called under the job's stripe lock, right after the Running write,
// so a worker that sees the run in the store always finds it open here.
func (s *SchedulerEngine) openRemoteRun(job *Job) *remoteWaiter {
	w := &remoteWaiter{ref: runRefOf(job), outcome: make(chan remoteOutcome, 1)}
	s.remoteRuns.Store(job.ID, w)
	return w
}

// remoteHandler is the JobHandler of a remote run: it waits for the worker's
// result until the execution's context ends (the job's timeout, or the
// engine stopping), and then returns what the existing outcome paths record.
// executeJob also ends the wait itself, before it records a timeout or an
// interruption, so a result is never accepted after the run's outcome was
// decided; this goroutine's own close is then a no-op.
func (s *SchedulerEngine) remoteHandler(w *remoteWaiter) JobHandler {
	return func(ctx context.Context, _ *Job) (*JobResult, error) {
		defer s.remoteRuns.CompareAndDelete(w.ref.JobID, w)
		select {
		case o := <-w.outcome:
			return o.result, o.err
		case <-ctx.Done():
			if !w.close() {
				o := <-w.outcome
				return o.result, o.err
			}
			return nil, ctx.Err()
		}
	}
}

// ListRemoteRuns returns the open runs of the named remote handlers in a
// tenant. An empty tenant lists system-owned runs.
func (s *SchedulerEngine) ListRemoteRuns(ctx context.Context, tenantID string, handlers []string) ([]*RemoteRun, error) {
	var runs []*RemoteRun
	for _, handler := range handlers {
		if !IsRemoteHandler(handler) {
			return nil, ErrNotRemoteHandler
		}
		jobs, err := s.ListJobs(ctx, &JobQuery{Status: JobStatusRunning, Handler: handler})
		if err != nil {
			return nil, err
		}
		for _, job := range jobs {
			if job.Handler != handler || JobTenantID(job) != tenantID {
				continue
			}
			timeout := job.Timeout
			if timeout == 0 {
				timeout = s.config.DefaultTimeout
			}
			runs = append(runs, &RemoteRun{
				RemoteRunRef: runRefOf(job),
				Name:         job.Name,
				Handler:      job.Handler,
				Payload:      job.Payload,
				Deadline:     job.UpdatedAt.Add(timeout),
			})
		}
	}
	return runs, nil
}

// CompleteRemoteRun reports the result of one remote run: result on success,
// or runErr when the worker's handler failed. A refused completion returns a
// RemoteRunNotOpenError, and records nothing.
func (s *SchedulerEngine) CompleteRemoteRun(ctx context.Context, ref RemoteRunRef, result *JobResult, runErr error) error {
	outcome := remoteOutcome{result: result, err: runErr}
	if v, ok := s.remoteRuns.Load(ref.JobID); ok {
		w, ok := v.(*remoteWaiter)
		if !ok {
			return fmt.Errorf("scheduler: open remote run of job %d is a %T, not a *remoteWaiter", ref.JobID, v)
		}
		if w.ref.same(ref) {
			if w.deliver(outcome) {
				return nil
			}
			return RemoteRunNotOpenError{Ref: ref, Reason: "the run already ended"}
		}
	}
	return s.completeRemoteRunElsewhere(ctx, ref, outcome)
}

// completeRemoteRunElsewhere concludes a run another node fired. Only a
// clustered engine with the run guard can: without it there is no other node
// to have fired the run, and nothing could arbitrate this write against the
// firing node's own.
func (s *SchedulerEngine) completeRemoteRunElsewhere(ctx context.Context, ref RemoteRunRef, o remoteOutcome) error {
	if s.concluder == nil {
		return RemoteRunNotOpenError{Ref: ref, Reason: "no execution on this engine is waiting on it"}
	}
	job, err := s.storage.GetJob(ctx, ref.JobID, ref.TenantID)
	if err != nil || job == nil {
		if err == nil || isJobNotFound(err) {
			return RemoteRunNotOpenError{Ref: ref, Reason: "the job does not exist"}
		}
		s.storageErrors.Add(1)
		return ErrStorageError{Op: "CompleteRemoteRun", Cause: err}
	}
	if job.Status != JobStatusRunning || !IsRemoteHandler(job.Handler) || !ref.names(job) {
		return RemoteRunNotOpenError{Ref: ref, Reason: "the job is not running that run"}
	}
	// The firing node's own execution still owns the run while its started
	// run is recorded: its wait ended (the timeout) and it is concluding the
	// run now. Checked under the stripe lock persistJobOutcome concludes
	// under, so this result never races that conclusion on the same node.
	lock := s.jobLock(job.ID)
	lock.Lock()
	if v, ok := s.startedRuns.Load(job.ID); ok {
		started, ok := v.(RunConclusion)
		if !ok {
			lock.Unlock()
			return fmt.Errorf("scheduler: started run of job %d is a %T, not a RunConclusion", job.ID, v)
		}
		if started.Firing.Equal(ref.Firing) && started.StartedAt.Equal(ref.StartedAt) {
			lock.Unlock()
			return RemoteRunNotOpenError{Ref: ref, Reason: "the run already ended"}
		}
	}
	won, err := s.concluder.ConcludeRun(ctx, &RunConclusion{
		JobID: job.ID, TenantID: JobTenantID(job), Firing: ref.Firing, StartedAt: ref.StartedAt,
		Owner: "remote@" + s.nodeID,
	})
	lock.Unlock()
	if err != nil {
		s.storageErrors.Add(1)
		return ErrStorageError{Op: "CompleteRemoteRun", Cause: err}
	}
	if !won {
		return RemoteRunNotOpenError{Ref: ref, Reason: "another writer concluded the run"}
	}
	exec := &JobExecution{JobID: job.ID, Status: JobStatusRunning, StartedAt: ref.StartedAt}
	if o.err != nil {
		s.handleJobError(ctx, job, exec, o.err)
		return nil
	}
	s.handleJobSuccess(ctx, job, exec, o.result)
	return nil
}
