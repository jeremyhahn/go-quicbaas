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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// =============================================================================
// Fixtures
// =============================================================================

// scriptedLeaseStore wraps a real store and lets a test decide what one
// heartbeat answers, and observe when heartbeats happen — so a loop can be
// held in a state until the ticker has fired, without a sleep.
type scriptedLeaseStore struct {
	JobLeaseStore

	acquireErr error

	heartbeats atomic.Int64

	// supersedeAt is the heartbeat ordinal that answers superseded; zero
	// never does.
	supersedeAt int64

	// superseded is closed when the superseded answer has been given.
	superseded chan struct{}
	supOnce    sync.Once

	// tickerBeat is closed when a heartbeat past the first has happened —
	// the first is the transition heartbeat, every later one is the ticker.
	tickerBeat chan struct{}
	tickOnce   sync.Once
}

func newScriptedLeaseStore(t *testing.T, ttl time.Duration) *scriptedLeaseStore {
	t.Helper()
	inner, err := NewProcessJobLeaseStore(ttl)
	if err != nil {
		t.Fatalf("NewProcessJobLeaseStore: %v", err)
	}
	return &scriptedLeaseStore{
		JobLeaseStore: inner,
		superseded:    make(chan struct{}),
		tickerBeat:    make(chan struct{}),
	}
}

func (s *scriptedLeaseStore) Acquire(ctx context.Context, lease *JobLease) error {
	if s.acquireErr != nil {
		return s.acquireErr
	}
	return s.JobLeaseStore.Acquire(ctx, lease)
}

func (s *scriptedLeaseStore) Heartbeat(ctx context.Context, lease *JobLease) error {
	n := s.heartbeats.Add(1)
	if n > 1 {
		s.tickOnce.Do(func() { close(s.tickerBeat) })
	}
	if s.supersedeAt != 0 && n == s.supersedeAt {
		s.supOnce.Do(func() { close(s.superseded) })
		return &JobSupersededError{JobID: lease.JobID, By: lease.Generation + 1, Node: "node-b"}
	}
	return s.JobLeaseStore.Heartbeat(ctx, lease)
}

// boundTaskMockService is the control-loop mock with a task to bind to.
type boundTaskMockService struct {
	*controlLoopMockService
	task *Task
}

func (m *boundTaskMockService) GetTask(_ context.Context, _, taskID string) (*Task, error) {
	if m.task == nil || m.task.ID != taskID {
		return nil, &TaskNotFoundError{TaskID: taskID}
	}
	return m.task, nil
}

// loopTestTTL is short so the ticker fires within a test, and long enough
// that the loop's own writes never go stale against it.
const loopTestTTL = 30 * time.Millisecond

// newLeasedRun builds a leased run over a scripted store whose decomposition
// waits on gate before failing, so the loop sits in decomposing for exactly
// as long as the test needs. The run is not started: the caller executes it
// through the workflow runner, as the envelope's goroutine does.
func newLeasedRun(t *testing.T, store *scriptedLeaseStore, gate <-chan struct{}) (*JobRun, *controlLoopMockService) {
	t.Helper()
	svc := &controlLoopMockService{
		backends: newTestBackends(),
		decomposeFn: func() (*DecomposeTaskResponse, error) {
			if gate != nil {
				<-gate
			}
			return nil, errors.New("decomposition interrupted by the test")
		},
	}
	launch := jobLaunch{Agents: svc, Leases: store, Node: "node-a", LeaseTTL: loopTestTTL}
	return launch.newRun(t), svc
}

// =============================================================================
// The heartbeat goroutine
// =============================================================================

// TestControlLoop_HeartbeatTickerStopsOnLoopExit is the goroutine-leak
// check. The ticker goroutine's only exit closes heartbeatDone, so a closed
// channel after Run returns is proof the goroutine is gone; a still-open one
// would block the receive and fail the test by timeout. The loop is held in
// decomposing until the ticker has actually fired, so the test proves a
// RUNNING ticker was stopped rather than one that never started.
func TestControlLoop_HeartbeatTickerStopsOnLoopExit(t *testing.T) {
	store := newScriptedLeaseStore(t, loopTestTTL)
	run, _ := newLeasedRun(t, store, store.tickerBeat)

	err := run.execute(context.Background(), workflowRunner{})
	if err == nil {
		t.Fatal("expected the interrupted decomposition to fail the job")
	}
	if !run.Finished() {
		t.Fatal("a returned run reports itself finished")
	}
	select {
	case <-run.heartbeatDone:
	default:
		t.Fatal("the heartbeat goroutine is still running after the run returned")
	}
	if store.heartbeats.Load() < 2 {
		t.Fatalf("expected the transition heartbeat and at least one ticker heartbeat, got %d", store.heartbeats.Load())
	}
	beats := store.heartbeats.Load()
	// A stopped ticker beats no more: the count is settled the moment the
	// run returns, because stopHeartbeat waited for the goroutine.
	if store.heartbeats.Load() != beats {
		t.Fatal("heartbeats continued after the loop returned")
	}

	// An unbound job releases its own lease on exit; the record survives.
	lease := run.Lease()
	if lease == nil || !lease.Released {
		t.Fatalf("an unbound job releases its lease on exit: %+v", lease)
	}
	got, err := store.Get(context.Background(), "test-tenant", "", JobRunnerWorkflow, lease.Ref)
	if err != nil {
		t.Fatalf("the released record must still exist: %v", err)
	}
	if !got.Released || got.Node != "node-a" || got.Generation != 1 || got.JobID != lease.JobID {
		t.Fatalf("released record is wrong: %+v", got)
	}
	status := run.Status()
	if status.Node != "node-a" || status.HeartbeatAt == 0 || status.State != JobStateFailed {
		t.Fatalf("status must carry the node and heartbeat: %+v", status)
	}
}

// TestControlLoop_SupersededHeartbeatAbortsTheLoop proves a heartbeat that
// finds the generation advanced ends the run with JobSupersededError, that
// the abort outranks the handler error it caused, and that the status carries
// the class.
func TestControlLoop_SupersededHeartbeatAbortsTheLoop(t *testing.T) {
	store := newScriptedLeaseStore(t, loopTestTTL)
	store.supersedeAt = 2 // the first is the transition heartbeat; the second is the ticker
	run, _ := newLeasedRun(t, store, store.superseded)

	err := run.execute(context.Background(), workflowRunner{})
	var superseded *JobSupersededError
	if !errors.As(err, &superseded) {
		t.Fatalf("expected JobSupersededError, got %T: %v", err, err)
	}
	if superseded.By != 2 || superseded.Node != "node-b" {
		t.Fatalf("superseded error names the wrong successor: %+v", superseded)
	}
	status := run.Status()
	if status.State != JobStateFailed || status.FailureClass != JobFailureClassSuperseded {
		t.Fatalf("status must be failed/superseded: state=%s class=%q", status.State, status.FailureClass)
	}
	if status.Error == "" {
		t.Fatal("status must carry the terminal error")
	}
	select {
	case <-run.heartbeatDone:
	default:
		t.Fatal("the heartbeat goroutine leaked past an abort")
	}
}

// TestControlLoop_LeaseRefusalFailsBeforeAnySession proves a job that cannot
// take its lease spawns nothing and reports the refusal as its class.
func TestControlLoop_LeaseRefusalFailsBeforeAnySession(t *testing.T) {
	for name, tc := range map[string]struct {
		refusal error
		class   string
	}{
		"taken":    {&JobLeaseTakenError{JobID: "j", HeldBy: "node-b", Generation: 1}, JobFailureClassSuperseded},
		"capacity": {&ProjectCapacityExceededError{TenantID: "t", ProjectID: "p", Live: 1, Limit: 1, RetryAt: time.Now().Add(time.Minute)}, FaultProjectCapacityExceeded},
	} {
		t.Run(name, func(t *testing.T) {
			store := newScriptedLeaseStore(t, loopTestTTL)
			store.acquireErr = tc.refusal
			svc := &controlLoopMockService{backends: newTestBackendsWithPersistent()}
			run, err := jobLaunch{
				Config: &JobConfig{
					TenantID: "test-tenant", Description: "d", LeadBackend: "claude", MaxIterations: 1,
				},
				Agents: svc, Leases: store, Node: "node-a", LeaseTTL: loopTestTTL,
			}.run(context.Background(), t)
			if !errors.Is(err, tc.refusal) {
				t.Fatalf("expected the refusal to end the run, got %v", err)
			}
			if svc.sessionIDCounter.Load() != 0 {
				t.Fatal("a refused job must spawn no session")
			}
			if run.Lease() != nil {
				t.Fatal("a refused job holds no lease")
			}
			status := run.Status()
			if status.FailureClass != tc.class {
				t.Fatalf("expected class %q, got %q", tc.class, status.FailureClass)
			}
			if name == "capacity" && run.Fault().RetryNotBefore.IsZero() {
				t.Fatal("a capacity refusal carries when to retry")
			}
			if run.heartbeatDone != nil {
				t.Fatal("no heartbeat starts for a lease that was never taken")
			}
		})
	}
}

// TestControlLoop_BoundJobKeepsItsLeaseForConclusion proves a job bound to a
// task leases the task's source reference and does NOT release on exit — the
// conclusion path does, after the task is written.
func TestControlLoop_BoundJobKeepsItsLeaseForConclusion(t *testing.T) {
	store := newScriptedLeaseStore(t, loopTestTTL)
	base := &controlLoopMockService{
		backends:    newTestBackends(),
		decomposeFn: func() (*DecomposeTaskResponse, error) { return nil, errors.New("interrupted") },
	}
	svc := &boundTaskMockService{
		controlLoopMockService: base,
		task:                   &Task{ID: "task-7", TenantID: "test-tenant", Source: &TaskSourceRef{Kind: TaskSourceKindOperator, Ref: "refs/heads/feature/x"}},
	}
	leased := jobLaunch{Agents: svc, Leases: store, Node: "node-a", LeaseTTL: loopTestTTL}

	config := newTestConfig()
	config.TaskID = "task-7"
	config.Attempt = 3
	config.ProjectID = "proj-1"
	leased.Config = config
	run, err := leased.run(context.Background(), t)
	if err == nil {
		t.Fatal("expected the interrupted decomposition to fail the job")
	}
	lease := run.Lease()
	if lease == nil {
		t.Fatal("a bound job takes a lease")
	}
	if lease.Ref != "refs/heads/feature/x" || lease.TaskID != "task-7" || lease.Generation != 3 || lease.ProjectID != "proj-1" {
		t.Fatalf("lease names the wrong subject: %+v", lease)
	}
	got, err := store.Get(context.Background(), "test-tenant", "proj-1", JobRunnerWorkflow, "refs/heads/feature/x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Released {
		t.Fatal("a bound job's lease is released by the conclusion, not by the run's exit")
	}

	// A task with no source leases its own ID.
	svc.task = &Task{ID: "task-8", TenantID: "test-tenant"}
	config.TaskID = "task-8"
	run, err = leased.run(context.Background(), t)
	if err == nil {
		t.Fatal("expected failure")
	}
	if run.Lease().Ref != "task-8" {
		t.Fatalf("a sourceless task leases its ID, got %q", run.Lease().Ref)
	}

	// A task the service does not know fails the job before any lease.
	config.TaskID = "task-missing"
	run, err = leased.run(context.Background(), t)
	var missing *TaskNotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("expected TaskNotFoundError, got %v", err)
	}
	if run.Lease() != nil {
		t.Fatal("no lease for a task that does not exist")
	}
}

// TestControlLoop_UnleasedWithoutAStore proves a loop with no store runs
// exactly as before: no lease, no heartbeat, no node on the status.
func TestControlLoop_UnleasedWithoutAStore(t *testing.T) {
	svc := &controlLoopMockService{
		backends:    newTestBackends(),
		decomposeFn: func() (*DecomposeTaskResponse, error) { return nil, errors.New("interrupted") },
	}
	run, err := runJob(context.Background(), t, svc, &controlLoopMockLogger{}, newTestConfig())
	if err == nil {
		t.Fatal("expected failure")
	}
	if run.Lease() != nil || run.heartbeatDone != nil {
		t.Fatal("an unleased run takes no lease and starts no heartbeat")
	}
	if status := run.Status(); status.Node != "" || status.HeartbeatAt != 0 {
		t.Fatalf("an unleased status carries no node or heartbeat: %+v", status)
	}
	if !run.Finished() {
		t.Fatal("finished is reported regardless of leasing")
	}
}

// TestControlLoop_AbortFromOutsideEndsTheRun proves the abort seam a stall
// detector uses: the loop's context is cancelled and the terminal error is
// the abort's, classified as stalled. The abort lands after the loop has
// published its cancel, so the handler in flight is interrupted.
func TestControlLoop_AbortFromOutsideEndsTheRun(t *testing.T) {
	store := newScriptedLeaseStore(t, loopTestTTL)
	gate := make(chan struct{})
	run, _ := newLeasedRun(t, store, gate)

	done := make(chan error, 1)
	go func() { done <- run.execute(context.Background(), workflowRunner{}) }()

	// Wait for the loop to be inside decomposing (the ticker has fired), then
	// abort it, then let decomposition return.
	<-store.tickerBeat
	stalled := &JobStalledError{JobID: "j", State: JobStateDecomposing, Idle: time.Hour}
	run.Abort(stalled)
	run.Abort(errors.New("a later abort must not replace the first"))
	close(gate)

	err := <-done
	if !errors.Is(err, stalled) {
		t.Fatalf("expected the first abort as the terminal error, got %v", err)
	}
	if run.Status().FailureClass != JobFailureClassStalled {
		t.Fatalf("expected class stalled, got %q", run.Status().FailureClass)
	}
	if run.abortReason() != stalled {
		t.Fatal("the first abort cause wins")
	}
	run.Abort(nil)
	run.recordFailure(nil)
	if run.abortReason() != stalled || run.failureClass != JobFailureClassStalled {
		t.Fatal("a nil abort or failure changes nothing")
	}
}

// TestControlLoop_HeartbeatSurvivesAStoreHiccup proves a heartbeat that fails
// for any reason other than supersession is logged and the run continues.
func TestControlLoop_HeartbeatSurvivesAStoreHiccup(t *testing.T) {
	logger := &controlLoopMockLogger{}
	failing := &failingHeartbeatStore{JobLeaseStore: newScriptedLeaseStore(t, loopTestTTL)}
	svc := &controlLoopMockService{
		backends:    newTestBackends(),
		decomposeFn: func() (*DecomposeTaskResponse, error) { return nil, errors.New("interrupted") },
	}
	run, err := jobLaunch{
		Agents: svc, Logger: logger, Leases: failing, Node: "node-a", LeaseTTL: loopTestTTL,
	}.run(context.Background(), t)
	if errors.Is(err, ErrJobSuperseded) {
		t.Fatal("a store hiccup is not a supersession")
	}
	if run.abortReason() != nil {
		t.Fatal("a failed heartbeat must not abort the loop")
	}
	found := false
	for _, line := range logger.warnLines() {
		if strings.Contains(line, "heartbeat failed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the hiccup to be logged, got %v", logger.warnLines())
	}
}

// failingHeartbeatStore answers every heartbeat with a backend failure.
type failingHeartbeatStore struct {
	JobLeaseStore
}

func (s *failingHeartbeatStore) Heartbeat(_ context.Context, _ *JobLease) error {
	return &JobLeaseError{Sentinel: ErrJobLeaseBackend, Message: "lease.swap", Cause: errors.New("store away")}
}

// =============================================================================
// The runner's own lease subject
// =============================================================================
//
// A JOB BOUND TO NO TASK IS LEASED BY ITS OWN ID, and an id is unique per
// job, so two such jobs contend with nothing and both run. For a runner
// whose jobs are per-subject singletons — one pull request, one project —
// that means every duplicate trigger spends a second run and writes a
// competing answer, and the later write wins. JobLeasePolicy is how such a
// runner says what its jobs are mutually exclusive WITH.

// subjectRunner is a runner declaring a JobLeasePolicy. Its Run body reports
// that it ran; the FIRST entrant then holds until the test releases it, so a
// contender meets a job that is demonstrably still running.
//
// ONLY THE FIRST ENTRANT HOLDS, deliberately. A body that blocked for every
// entrant would DEADLOCK rather than fail when the seam is missing and the
// contender is wrongly admitted, and a regression test that hangs reports
// nothing.
type subjectRunner struct {
	*fakeRunner
	subject   string
	offBudget bool

	entrants atomic.Int64
	entered  chan string
	release  chan struct{}
}

func newSubjectRunner(name, subject string, entered chan string, release chan struct{}) *subjectRunner {
	r := &subjectRunner{subject: subject, entered: entered, release: release}
	r.fakeRunner = &fakeRunner{name: name, run: func(_ context.Context, job *JobRun) error {
		first := r.entrants.Add(1) == 1
		r.entered <- job.JobID
		if first && r.release != nil {
			<-r.release
		}
		return nil
	}}
	return r
}

func (r *subjectRunner) LeaseSubject(*JobConfig) string { return r.subject }

func (r *subjectRunner) OffProjectBudget() bool { return r.offBudget }

// unsubjectedRunner is the same runner WITHOUT the policy, which is what
// every runner whose jobs are attempts at a task looks like.
type unsubjectedRunner struct{ *fakeRunner }

// leasedRunFor builds a run of one runner over a shared store, as the
// envelope builds one.
func leasedRunFor(t *testing.T, store JobLeaseStore, runner JobRunner, jobID string) *JobRun {
	t.Helper()
	config := newTestConfig()
	config.TenantID = "acme"
	config.ProjectID = "api"
	config.TaskID = ""
	run := newJobRun(JobRunConfig{
		JobID:    jobID,
		Runner:   runner.Name(),
		Config:   config,
		Agents:   &controlLoopMockService{},
		Logger:   &controlLoopMockLogger{},
		Leases:   store,
		Node:     "node-a",
		LeaseTTL: time.Minute,
	})
	run.StartState = entryStateFor(runner)
	return run
}

func TestJobLeasePolicy_TwoJobsOnOneSubjectAreOneRun(t *testing.T) {
	store, err := NewProcessJobLeaseStore(time.Minute)
	if err != nil {
		t.Fatalf("NewProcessJobLeaseStore: %v", err)
	}
	entered := make(chan string, 2)
	release := make(chan struct{})
	runner := newSubjectRunner("subjected", "review:api:#41", entered, release)

	// The first job takes the subject and is held INSIDE its Run body, so
	// the second contends with a job that is demonstrably still running
	// rather than with a record left behind by one that finished.
	first := leasedRunFor(t, store, runner, "job-first")
	firstDone := make(chan error, 1)
	go func() { firstDone <- first.execute(context.Background(), runner) }()
	if got := <-entered; got != "job-first" {
		t.Fatalf("the first run to enter was %q", got)
	}

	second := leasedRunFor(t, store, runner, "job-second")
	secondErr := second.execute(context.Background(), runner)

	// THE OBSERVABLE PROPERTY: the second job never reached its own work.
	var taken *JobLeaseTakenError
	if !errors.As(secondErr, &taken) {
		t.Fatalf("second err = %T %v, want the lease declined", secondErr, secondErr)
	}
	if taken.HeldByJobID != "job-first" {
		t.Fatalf("declined by job %q, want job-first", taken.HeldByJobID)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first run: %v", err)
	}
	if entrants := runner.entrants.Load(); entrants != 1 {
		t.Fatalf("%d jobs ran their work, want only the holder", entrants)
	}

	// And the record the two contended over names the runner's subject,
	// which is what makes it shared at all.
	if ref := first.Lease().Ref; ref != "review:api:#41" {
		t.Fatalf("lease ref = %q, want the runner's subject", ref)
	}
}

func TestJobLeasePolicy_WithoutOneTwoJobsBothRun(t *testing.T) {
	// The control. Without the policy the envelope names each lease after
	// the job's own id, and this is the behaviour the review runner had:
	// two triggers, two runs, two answers.
	store, err := NewProcessJobLeaseStore(time.Minute)
	if err != nil {
		t.Fatalf("NewProcessJobLeaseStore: %v", err)
	}
	entered := make(chan string, 2)
	release := make(chan struct{})
	close(release)
	inner := &fakeRunner{name: "unsubjected", run: func(_ context.Context, job *JobRun) error {
		entered <- job.JobID
		return nil
	}}
	runner := unsubjectedRunner{fakeRunner: inner}

	for _, jobID := range []string{"job-first", "job-second"} {
		if err := leasedRunFor(t, store, runner, jobID).execute(context.Background(), runner); err != nil {
			t.Fatalf("%s: %v", jobID, err)
		}
	}
	if len(entered) != 2 {
		t.Fatalf("%d of 2 unsubjected jobs ran; they are supposed to contend with nothing", len(entered))
	}
}

func TestJobLeasePolicy_ASubjectedJobIsStillItsOwnHolder(t *testing.T) {
	// Resumption after a restart is the same job taking the same subject
	// again, and refusing it would strand every job whose node restarted
	// inside one TTL.
	store, err := NewProcessJobLeaseStore(time.Minute)
	if err != nil {
		t.Fatalf("NewProcessJobLeaseStore: %v", err)
	}
	release := make(chan struct{})
	close(release)
	entered := make(chan string, 2)
	runner := newSubjectRunner("subjected-resume", "review:api:#41", entered, release)

	for range 2 {
		if err := leasedRunFor(t, store, runner, "job-same").execute(context.Background(), runner); err != nil {
			t.Fatalf("the holder could not re-take its own subject: %v", err)
		}
	}
	if len(entered) != 2 {
		t.Fatalf("%d of 2 resumptions ran", len(entered))
	}
}

func TestRunnerLeaseSubject_AnEmptyAnswerTakesTheEnvelopes(t *testing.T) {
	// A runner that declares the policy but answers nothing for this
	// configuration must not collapse every such job onto one lease.
	cfg := newTestConfig()
	cases := []struct {
		name   string
		runner JobRunner
		want   string
	}{
		{"no policy", &fakeRunner{name: "plain"}, ""},
		{"empty answer", newSubjectRunner("blank", "", nil, nil), ""},
		{"whitespace answer", newSubjectRunner("spaces", "   ", nil, nil), ""},
		{"declared", newSubjectRunner("named", " review:api:#41 ", nil, nil), "review:api:#41"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runnerLeaseSubject(tc.runner, cfg); got != tc.want {
				t.Fatalf("subject = %q, want %q", got, tc.want)
			}
		})
	}
}
