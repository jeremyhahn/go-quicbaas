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
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Fixtures
// =============================================================================

// holdingBackend holds every decomposition prompt open until released, and
// signals each arrival, so a test can hold a job in decomposing — with its
// lease taken — for exactly as long as it needs.
type holdingBackend struct {
	arrived chan struct{}
	release chan struct{}
}

func newHoldingBackend() *holdingBackend {
	return &holdingBackend{arrived: make(chan struct{}, 16), release: make(chan struct{})}
}

func (b *holdingBackend) mock() *mockAgentBackend {
	return &mockAgentBackend{
		backendType: "mock",
		promptFn: func(ctx context.Context, _ *PromptRequest) (*PromptResponse, error) {
			b.arrived <- struct{}{}
			select {
			case <-b.release:
				return &PromptResponse{
					Content:    []ContentBlock{{Type: "text", Text: `{"work_items":[{"description":"task-1","backend":"mock"}]}`}},
					StopReason: "end_turn",
				}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
}

// awaitArrival waits for a prompt to reach the backend or fails the test.
func awaitArrival(t *testing.T, b *holdingBackend) {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-b.arrived:
	case <-timer.C:
		t.Fatal("timed out waiting for the job to reach its backend")
	}
}

// awaitLeaseReleased waits for a BOUND job's slot to be freed, or fails.
//
// awaitFinished IS NOT ENOUGH HERE, and the difference is a real ordering
// rather than a slow machine. `run.finished` is set by the LAST deferred call
// in JobRun.execute; a bound job's lease is released by concludeAttempt, which
// runs afterwards in finishJob on the job's own goroutine. So a test that
// reads the lease the instant the loop reports finished is reading ahead of
// the release, and passed only because nothing else was competing for the
// scheduler. Adding ANY work to Sweep — a single extra store read is enough —
// made it fail one run in ten (go-quicbaas#792 review). The assertion is the
// same; only the moment it is safe to make has been stated.
func awaitLeaseReleased(ctx context.Context, t *testing.T, leases JobLeaseStore,
	tenantID, projectID, workflow, ref string) *JobLease {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		lease, err := leases.Get(ctx, tenantID, projectID, workflow, ref)
		if err != nil {
			t.Fatalf("the lease record must survive the conclusion: %v", err)
		}
		if lease.Released {
			return lease
		}
		select {
		case <-timer.C:
			t.Fatalf("timed out waiting for the slot the dead worker held to be freed: %+v", lease)
		case <-ticker.C:
		}
	}
}

// awaitFinished waits for a loop to return or fails the test.
func awaitFinished(t *testing.T, run *JobRun) {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !run.Finished() {
		select {
		case <-timer.C:
			t.Fatal("timed out waiting for the loop to finish")
		case <-ticker.C:
		}
	}
}

func newHoldingService(t *testing.T, backend *holdingBackend) *agentServiceImpl {
	t.Helper()
	svc, err := NewAgentService(NewInMemoryAgentStore(), map[string]AgentBackend{"mock": backend.mock()}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetNode("node-a")
	return impl
}

func runningLoop(t *testing.T, svc *agentServiceImpl, tenantID, jobID string) *JobRun {
	t.Helper()
	val, ok := svc.jobs.Load(tenantID + "/" + jobID)
	if !ok {
		t.Fatalf("job %s is not registered", jobID)
	}
	return val.(*runningJob).run
}

// =============================================================================
// StartJob and CancelJob
// =============================================================================

// TestStartJob_TakesALeaseAndCancelJobReleasesIt proves a started job holds
// a lease under this node's name, its status carries the node and a
// heartbeat, and CancelJob releases the lease synchronously — the record
// survives, marked released.
func TestStartJob_TakesALeaseAndCancelJobReleasesIt(t *testing.T) {
	backend := newHoldingBackend()
	svc := newHoldingService(t, backend)
	ctx := ContextWithTenant(context.Background(), "acme")

	resp, err := svc.StartJob(ctx, &StartJobRequest{TenantID: "acme", Description: "leased", routedBackend: "mock", MaxIterations: 1})
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	defer cancelAllJobs(svc)
	awaitArrival(t, backend)

	loop := runningLoop(t, svc, "acme", resp.JobID)
	lease := loop.Lease()
	if lease == nil || lease.Node != "node-a" || lease.JobID != resp.JobID || lease.Generation != 1 || lease.Released {
		t.Fatalf("a started job holds a live lease under this node: %+v", lease)
	}
	status, err := svc.GetJobStatus(ctx, "acme", resp.JobID)
	if err != nil {
		t.Fatalf("GetJobStatus: %v", err)
	}
	if status.Node != "node-a" || status.HeartbeatAt == 0 {
		t.Fatalf("status must carry the node and heartbeat: node=%q heartbeat=%d", status.Node, status.HeartbeatAt)
	}
	if !svc.JobRunning("acme", resp.JobID) {
		t.Fatal("a job in decomposing is running here")
	}

	if err := svc.CancelJob(ctx, "acme", resp.JobID); err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	got, err := svc.jobLeases.Get(ctx, "acme", "", JobRunnerWorkflow, resp.JobID)
	if err != nil {
		t.Fatalf("the lease record must survive the cancel: %v", err)
	}
	if !got.Released || got.ReleasedAt == 0 {
		t.Fatalf("CancelJob must release the lease: %+v", got)
	}
	awaitFinished(t, loop)
	if svc.JobRunning("acme", resp.JobID) {
		t.Fatal("a cancelled job is not running")
	}
}

// singletonRunner is a runner whose jobs are per-subject singletons: every
// job of it names the same lease subject, and its run holds the lease until
// cancelled.
type singletonRunner struct{ name string }

func (r *singletonRunner) Name() string { return r.name }
func (r *singletonRunner) States() []JobStateSpec {
	return []JobStateSpec{{State: JobStateExecuting, Label: "Executing"}}
}
func (r *singletonRunner) Validate(context.Context, *JobConfig) error { return nil }
func (r *singletonRunner) LeaseSubject(*JobConfig) string             { return r.name + ":subject" }
func (r *singletonRunner) OffProjectBudget() bool                     { return true }
func (r *singletonRunner) Run(ctx context.Context, job *JobRun) error {
	<-ctx.Done()
	return ctx.Err()
}

// TestStartJob_RefusesASecondJobOnAHeldSubjectBeforeCreatingIt: a runner
// whose jobs are singletons per subject is refused at StartJob while another
// job holds the subject's lease — no second job record, no failed job in the
// queue — and admitted again once the holder has released it.
func TestStartJob_RefusesASecondJobOnAHeldSubjectBeforeCreatingIt(t *testing.T) {
	runner := &singletonRunner{name: "singleton-" + t.Name()}
	registerJobRunnerForTest(t, runner)
	backend := newHoldingBackend()
	svc := newHoldingService(t, backend)
	ctx := ContextWithTenant(context.Background(), "acme")
	defer cancelAllJobs(svc)

	first, err := svc.StartJob(ctx, &StartJobRequest{TenantID: "acme", Runner: runner.name, Description: "holder", routedBackend: "mock"})
	if err != nil {
		t.Fatalf("StartJob (holder): %v", err)
	}
	loop := runningLoop(t, svc, "acme", first.JobID)
	awaitLease(t, loop)

	second, err := svc.StartJob(ctx, &StartJobRequest{TenantID: "acme", Runner: runner.name, Description: "duplicate", routedBackend: "mock"})
	if !errors.Is(err, ErrJobLeaseTaken) {
		t.Fatalf("a duplicate on a held subject was not refused at StartJob: resp=%+v err=%v", second, err)
	}
	var taken *JobLeaseTakenError
	if !errors.As(err, &taken) || taken.HeldByJobID != first.JobID {
		t.Fatalf("the refusal does not name the holder: %v", err)
	}
	if n := countJobs(svc, "acme"); n != 1 {
		t.Fatalf("jobs registered = %d, want only the holder; a refused duplicate must leave no record", n)
	}

	if err := svc.CancelJob(ctx, "acme", first.JobID); err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	awaitFinished(t, loop)
	third, err := svc.StartJob(ctx, &StartJobRequest{TenantID: "acme", Runner: runner.name, Description: "after release", routedBackend: "mock"})
	if err != nil {
		t.Fatalf("StartJob after the holder released: %v", err)
	}
	if third.JobID == first.JobID {
		t.Fatal("the job after release reused the holder's id")
	}
}

// awaitLease waits until the run holds its lease.
func awaitLease(t *testing.T, loop *JobRun) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for loop.Lease() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the run never took its lease")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// countJobs counts the jobs the service has registered for a tenant.
func countJobs(svc *agentServiceImpl, tenantID string) int {
	n := 0
	svc.jobs.Range(func(key, _ any) bool {
		if strings.HasPrefix(key.(string), tenantID+"/") {
			n++
		}
		return true
	})
	return n
}

// TestStartJob_RefusesTheJobOverProjectCapacity proves the typed capacity
// refusal reaches a job's status as its failure class, and that the slot
// frees when the holder is cancelled.
func TestStartJob_RefusesTheJobOverProjectCapacity(t *testing.T) {
	backend := newHoldingBackend()
	svc := newHoldingService(t, backend)
	ctx := ContextWithTenant(context.Background(), "acme")

	projects := NewInMemoryProjectStore()
	if err := projects.Create(ctx, &Project{ID: testProjectID, TenantID: "acme", Name: "capped", Quotas: ProjectQuotas{MaxConcurrency: 1}}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	capped, err := NewProjectCapacityJobLeaseStore(svc.jobLeases, projects, svc.leaseTTL)
	if err != nil {
		t.Fatalf("NewProjectCapacityJobLeaseStore: %v", err)
	}
	svc.SetJobLeaseStore(capped, 0)

	first, err := svc.StartJob(ctx, &StartJobRequest{TenantID: "acme", Description: "one", routedBackend: "mock", ProjectID: testProjectID, MaxIterations: 1})
	if err != nil {
		t.Fatalf("StartJob first: %v", err)
	}
	defer cancelAllJobs(svc)
	awaitArrival(t, backend)

	second, err := svc.StartJob(ctx, &StartJobRequest{TenantID: "acme", Description: "two", routedBackend: "mock", ProjectID: testProjectID, MaxIterations: 1})
	if err != nil {
		t.Fatalf("StartJob second: %v", err)
	}
	loop := runningLoop(t, svc, "acme", second.JobID)
	awaitFinished(t, loop)
	status := loop.Status()
	if status.State != JobStateFailed || status.FailureClass != FaultProjectCapacityExceeded {
		t.Fatalf("the second job is refused with the capacity class: state=%s class=%q err=%q", status.State, status.FailureClass, status.Error)
	}
	if loop.Fault().RetryNotBefore.IsZero() {
		t.Fatal("a capacity refusal names when to retry")
	}

	if err := svc.CancelJob(ctx, "acme", first.JobID); err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	third, err := svc.StartJob(ctx, &StartJobRequest{TenantID: "acme", Description: "three", routedBackend: "mock", ProjectID: testProjectID, MaxIterations: 1})
	if err != nil {
		t.Fatalf("StartJob third: %v", err)
	}
	awaitArrival(t, backend)
	if lease := runningLoop(t, svc, "acme", third.JobID).Lease(); lease == nil || lease.Released {
		t.Fatal("after the holder is cancelled the slot is free")
	}
}

// TestStartJob_BoundJobConcludesItsTaskSynchronously proves the finish path:
// a job bound to a task writes the task's outcome on its own goroutine
// before its subscribers are released, and its lease is released after the
// write.
func TestStartJob_BoundJobConcludesItsTaskSynchronously(t *testing.T) {
	backend := newHoldingBackend()
	svc := newHoldingService(t, backend)
	store := NewInMemoryTaskStore()
	svc.SetTaskStore(store)
	ctx := ContextWithTenant(context.Background(), "acme")

	// The task is bound to the job the way the dispatcher binds it, before
	// the job runs; the job is then cancelled so it fails: a charged failure
	// with budget left requeues.
	seeded := seedSpawnedTask(t, store, &Task{Status: TaskStatusPending, Attempts: 1})
	resp := startBoundJob(ctx, t, svc, store, seeded.ID, &StartJobRequest{TenantID: "acme", Description: "bound", routedBackend: "mock", Attempt: 1, MaxIterations: 1})
	defer cancelAllJobs(svc)
	awaitArrival(t, backend)

	loop := runningLoop(t, svc, "acme", resp.JobID)
	lease := loop.Lease()
	if lease == nil || lease.TaskID != seeded.ID || lease.Ref != seeded.ID {
		t.Fatalf("a bound job leases its task: %+v", lease)
	}
	if err := svc.CancelJob(ctx, "acme", resp.JobID); err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	awaitFinished(t, loop)

	// The conclusion runs on the job goroutine after the loop returns;
	// finished is set before it, so wait for the task to reflect it.
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		task := readTask(t, store, seeded.ID)
		if task.SpawnedJobID == "" {
			if task.Status != TaskStatusPending || task.ChargedAttempts() != 1 || task.AttemptHistory()[0] != resp.JobID {
				t.Fatalf("a cancelled bound job is a charged requeue: %+v", task)
			}
			break
		}
		select {
		case <-timer.C:
			t.Fatalf("the task was never concluded: %+v", task)
		case <-ticker.C:
		}
	}
}

// =============================================================================
// Recovery
// =============================================================================

func persistStatus(t *testing.T, kv *mockKVStore, status *JobStatus) {
	t.Helper()
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := kv.Put(context.Background(), jobPersistenceKey(status.TenantID, status.JobID), data); err != nil {
		t.Fatalf("put: %v", err)
	}
}

// TestRecoverJobs_ResumesOnlyThisNodesJobs proves a job another node owns is
// left to that node — or to the reaper — and this node's own job is resumed
// under its lease.
func TestRecoverJobs_ResumesOnlyThisNodesJobs(t *testing.T) {
	kv := newMockKVStore()
	logger := &controlLoopMockLogger{}
	svc := &agentServiceImpl{store: NewInMemoryAgentStore(), kvStore: kv, logger: logger, node: "node-a"}
	leases, err := NewProcessJobLeaseStore(DefaultJobLeaseTTL)
	if err != nil {
		t.Fatalf("NewProcessJobLeaseStore: %v", err)
	}
	svc.jobLeases, svc.leaseTTL = leases, DefaultJobLeaseTTL

	config := newTestConfig()
	persistStatus(t, kv, &JobStatus{JobID: "job-mine", TenantID: "test-tenant", State: JobStateExecuting, Node: "node-a", Config: config})
	persistStatus(t, kv, &JobStatus{JobID: "job-theirs", TenantID: "test-tenant", State: JobStateExecuting, Node: "node-b", Config: config})

	if err := svc.recoverJobs(context.Background()); err != nil {
		t.Fatalf("recoverJobs: %v", err)
	}
	defer cancelAllJobs(svc)
	if _, ok := svc.jobs.Load("test-tenant/job-mine"); !ok {
		t.Fatal("this node's job must be resumed")
	}
	if _, ok := svc.jobs.Load("test-tenant/job-theirs"); ok {
		t.Fatal("another node's job must not be resumed here")
	}
	theirs, err := svc.loadJobStatus("test-tenant", "job-theirs")
	if err != nil || theirs.State != JobStateExecuting {
		t.Fatalf("the other node's record is left untouched: %+v %v", theirs, err)
	}
	mine := runningLoop(t, svc, "test-tenant", "job-mine")
	if mine.leases == nil || mine.node != "node-a" {
		t.Fatal("a resumed loop is leased under this node")
	}
}

// TestResumeJob_AsksTheRunnerWhereToRestart proves recovery is the runner's
// answer and not a table the engine keeps: a module runner implementing
// JobResumer is asked where its interrupted job restarts, and the workflow
// runner still sends an interrupted lead-driven job back to decomposing.
func TestResumeJob_AsksTheRunnerWhereToRestart(t *testing.T) {
	kv := newMockKVStore()
	svc := &agentServiceImpl{store: NewInMemoryAgentStore(), kvStore: kv, logger: &controlLoopMockLogger{}}
	leases, err := NewProcessJobLeaseStore(DefaultJobLeaseTTL)
	if err != nil {
		t.Fatalf("NewProcessJobLeaseStore: %v", err)
	}
	svc.jobLeases, svc.leaseTTL = leases, DefaultJobLeaseTTL

	// A module runner that owns its own state sequence and answers where an
	// interrupted job of its shape restarts.
	started := make(chan JobState, 1)
	release := make(chan struct{})
	defer close(release)
	module := resumingRunner{&fakeRunner{
		name: "resume-module-test",
		// Its declared state matches the workflow runner's entry spec exactly,
		// so registering it globally leaves KnownJobStates unchanged.
		states: []JobStateSpec{{State: JobStateInitializing, Label: "Initializing"}},
		resume: func(JobState) JobState { return JobStateCompleting },
		run: func(_ context.Context, job *JobRun) error {
			started <- job.StartState
			<-release
			return nil
		},
	}}
	registerJobRunnerForTest(t, module)

	moduleConfig := newTestConfig()
	moduleConfig.Runner = module.Name()
	status := &JobStatus{JobID: "job-module", TenantID: "test-tenant", State: JobStateExecuting,
		Runner: module.Name(), Config: moduleConfig}
	if err := svc.resumeJob(context.Background(), status); err != nil {
		t.Fatalf("resumeJob: %v", err)
	}
	defer cancelAllJobs(svc)

	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case got := <-started:
		if got != JobStateCompleting {
			t.Fatalf("the runner's answer must be the start state, resumed at %s", got)
		}
	case <-timer.C:
		t.Fatal("the resumed module job never ran")
	}

	// The workflow runner keeps its own mapping: executing re-decomposes.
	workflowStatus := &JobStatus{JobID: "job-workflow", TenantID: "test-tenant", State: JobStateExecuting,
		Config: newTestConfig(), WorkItems: []*WorkItem{{ID: "w1", Title: "t"}}}
	if err := svc.resumeJob(context.Background(), workflowStatus); err != nil {
		t.Fatalf("resumeJob: %v", err)
	}
	loop := runningLoop(t, svc, "test-tenant", "job-workflow")
	wfTimer := time.NewTimer(10 * time.Second)
	defer wfTimer.Stop()
	select {
	case ev := <-loop.Events():
		if ev.State != JobStateDecomposing {
			t.Fatalf("a lead-driven job resumed at %s, want decomposing", ev.State)
		}
	case <-wfTimer.C:
		t.Fatal("no start event")
	}
}

// TestRecoverJobs_ClusteredStoreKeepsANodeOwnedLease proves the transaction
// store setter replaces the process store and the TTL, and that a zero TTL
// keeps the previous one.
func TestSetJobLeaseStore(t *testing.T) {
	svc := newTestService(t)
	before := svc.jobLeases
	svc.SetJobLeaseStore(nil, time.Minute)
	if svc.jobLeases != before || svc.leaseTTL != DefaultJobLeaseTTL {
		t.Fatal("a nil store is ignored")
	}
	replacement, err := NewProcessJobLeaseStore(time.Second)
	if err != nil {
		t.Fatalf("NewProcessJobLeaseStore: %v", err)
	}
	svc.SetJobLeaseStore(replacement, 0)
	if svc.jobLeases != replacement || svc.leaseTTL != DefaultJobLeaseTTL {
		t.Fatal("a zero TTL keeps the previous TTL")
	}
	svc.SetJobLeaseStore(replacement, time.Minute)
	if svc.leaseTTL != time.Minute {
		t.Fatal("a positive TTL is taken")
	}
	svc.SetIdleThresholds(JobIdleThresholds{JobStateExecuting: time.Hour})
	if svc.idleThresholds[JobStateExecuting] != time.Hour {
		t.Fatal("thresholds are stored")
	}
	run := newJobRun(JobRunConfig{
		Config:   &JobConfig{},
		Agents:   svc,
		Logger:   &controlLoopMockLogger{},
		Leases:   svc.jobLeases,
		Node:     svc.node,
		LeaseTTL: svc.leaseTTL,
	})
	if run.leases != replacement || run.leaseTTL != time.Minute || run.node != svc.node {
		t.Fatal("the envelope hands the run the service's wiring")
	}
	run.SetJobLeasing(replacement, "n", 0)
	if run.leaseTTL != DefaultJobLeaseTTL {
		t.Fatal("a run given no TTL takes the default")
	}
}

// TestRunningJobs_ListsBoundUnfinishedRuns proves the stall detector's
// source: only bound runs that have not returned.
func TestRunningJobs_ListsBoundUnfinishedRuns(t *testing.T) {
	svc := newTestService(t)
	newRun := func(cfg *JobConfig) *JobRun {
		return newJobRun(JobRunConfig{Config: cfg, Agents: svc, Logger: &controlLoopMockLogger{}})
	}
	bound := newRun(&JobConfig{TaskID: "t"})
	unbound := newRun(&JobConfig{})
	done := newRun(&JobConfig{TaskID: "t2"})
	done.finished.Store(true)
	svc.jobs.Store("acme/bound", &runningJob{run: bound})
	svc.jobs.Store("acme/unbound", &runningJob{run: unbound})
	svc.jobs.Store("acme/done", &runningJob{run: done})
	svc.jobs.Store("acme/junk", "junk")

	runs := svc.RunningJobs()
	if len(runs) != 1 || runs[0] != bound {
		t.Fatalf("expected only the bound running job, got %d", len(runs))
	}
	if !bound.Bound() || unbound.Bound() || (&JobRun{}).Bound() {
		t.Fatal("Bound reads the config's task")
	}
}

// TestFinishJob_WithoutAStatusDoesNothing proves the finish path tolerates a
// loop that never published a status.
func TestFinishJob_WithoutAStatusDoesNothing(t *testing.T) {
	svc := newTestService(t)
	run := newJobRun(JobRunConfig{Config: &JobConfig{}, Agents: svc, Logger: &controlLoopMockLogger{}})
	cancelled := false
	svc.finishJob("acme", run, func() { cancelled = true }, errors.New("x"))
	if !cancelled {
		t.Fatal("the job context is always cancelled")
	}
}

// =============================================================================
// The sweep that concludes a dead remote worker (#774, #776)
// =============================================================================

// TestSweep_ConcludesAJobWhoseWorkerSessionIsGone is the property both defects
// denied the burn-in node: a job whose worker died is ENDED by the maintenance
// sweep, and the slot it held is freed.
//
// It is driven through the real service on both sides of the seam — the sweep
// is the detector the daemon builds, and the session authority is the
// orchestrator's own SessionStatus, tenant guard included — because a double
// on either side would agree with whichever tenant the detector happened to
// send. The sweep context deliberately carries no tenant: that is what the
// daemon's maintenance ticker hands down, and it is what made every probe
// answer "tenant ID required" (#776).
func TestSweep_ConcludesAJobWhoseWorkerSessionIsGone(t *testing.T) {
	backend := newHoldingBackend()
	worker := backend.mock()

	// The worker's container is gone, so the daemon no longer holds the
	// session — the answer a remote daemon gives once `--rm` has removed a
	// dead container (#774).
	worker.statusFn = func(_ context.Context, sessionID string) (*SessionStatus, error) {
		return nil, &SessionNotFoundError{SessionID: sessionID}
	}

	svc, err := NewAgentService(NewInMemoryAgentStore(), map[string]AgentBackend{"mock": worker}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetNode("node-a")
	impl.SetTenantGuard(NewTenantGuard(&agentTestLogger{}))
	impl.SetIdleThresholds(JobIdleThresholds{JobStateExecuting: time.Minute})
	tasks := NewInMemoryTaskStore()
	impl.SetTaskStore(tasks)

	// The sweep examines this node's TASK-BOUND jobs, which is what a
	// dispatched worker always is.
	ctx := ContextWithTenant(context.Background(), "acme")
	seeded := seedUnboundTask(t, tasks, &Task{Status: TaskStatusClaimed, Attempts: 1})
	started, err := impl.StartJob(ctx, &StartJobRequest{
		TenantID: "acme", Description: "remote worker", routedBackend: "mock",
		TaskID: seeded.ID, Attempt: 1, MaxIterations: 1,
	})
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	defer cancelAllJobs(impl)
	awaitArrival(t, backend)

	spawned, err := impl.SpawnSession(ctx, &SpawnSessionRequest{Backend: "mock", JobID: started.JobID})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	loop := runningLoop(t, impl, "acme", started.JobID)
	if lease := loop.Lease(); lease == nil || lease.Released {
		t.Fatalf("a started job holds its slot: %+v", lease)
	}

	// Put the job where the burn-in found it: session-backed, past its bound,
	// with the dead worker's session on it. A copy is published rather than
	// the live snapshot mutated, so no reader sees a half-written status.
	stuck := *loop.Status()
	stuck.State = JobStateExecuting
	stuck.SessionIDs = []string{spawned.SessionID}
	stuck.UpdatedAt = time.Now().Add(-time.Hour).UnixNano()
	loop.status.Store(&stuck)

	report, err := impl.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if report.Stall.Count(JobStallSessionUnknown) != 0 {
		t.Fatalf("the sweep must be able to read the session: %+v", report.Stall.Findings)
	}
	if report.Stall.Count(JobStallStalled) != 1 {
		t.Fatalf("expected the job to be stalled, got %+v", report.Stall.Findings)
	}
	var stalled *JobStalledError
	if !errors.As(loop.abortReason(), &stalled) || stalled.JobID != started.JobID {
		t.Fatalf("the job must be ended with JobStalledError, got %v", loop.abortReason())
	}

	awaitFinished(t, loop)
	if impl.JobRunning("acme", started.JobID) {
		t.Fatal("a stalled job is no longer running")
	}
	awaitLeaseReleased(ctx, t, impl.jobLeases, "acme", "", JobRunnerWorkflow, seeded.ID)
}

// gatedLeaseStore holds every Acquire until released, and signals each
// arrival, so a test can hold a resumed job between launch and lease.
type gatedLeaseStore struct {
	JobLeaseStore
	arrived  chan struct{}
	release  chan struct{}
	acquired chan struct{}
}

func newGatedLeaseStore(t *testing.T) *gatedLeaseStore {
	t.Helper()
	inner, err := NewProcessJobLeaseStore(DefaultJobLeaseTTL)
	if err != nil {
		t.Fatalf("NewProcessJobLeaseStore: %v", err)
	}
	return &gatedLeaseStore{
		JobLeaseStore: inner,
		arrived:       make(chan struct{}, 1),
		release:       make(chan struct{}),
		acquired:      make(chan struct{}, 1),
	}
}

func (g *gatedLeaseStore) Acquire(ctx context.Context, lease *JobLease) error {
	g.arrived <- struct{}{}
	select {
	case <-g.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	err := g.JobLeaseStore.Acquire(ctx, lease)
	g.acquired <- struct{}{}
	return err
}

// TestRecoverJobs_ReturnsOnlyOnceResumedJobsHoldTheirLeases is the regression
// for the dispatcher-vs-recovery race (go-quicbaas#1064): launchJob returns
// before the run takes its lease, so a recovery that returned on launch let
// the dispatcher's first sweep find no live lease and duplicate the job.
func TestRecoverJobs_ReturnsOnlyOnceResumedJobsHoldTheirLeases(t *testing.T) {
	kv := newMockKVStore()
	svc := &agentServiceImpl{store: NewInMemoryAgentStore(), kvStore: kv, logger: &controlLoopMockLogger{}, node: "node-a"}
	gate := newGatedLeaseStore(t)
	svc.jobLeases, svc.leaseTTL = gate, DefaultJobLeaseTTL
	persistStatus(t, kv, &JobStatus{JobID: "job-mine", TenantID: "test-tenant", State: JobStateExecuting, Node: "node-a", Config: newTestConfig()})
	defer cancelAllJobs(svc)

	done := make(chan error, 1)
	go func() { done <- svc.recoverJobs(context.Background()) }()

	<-gate.arrived
	select {
	case err := <-done:
		t.Fatalf("recovery returned (%v) while the resumed job was still waiting for its lease", err)
	default:
	}

	close(gate.release)
	if err := <-done; err != nil {
		t.Fatalf("recoverJobs: %v", err)
	}
	select {
	case <-gate.acquired:
	default:
		t.Fatal("recovery returned before the resumed job's lease was taken")
	}
}

// TestRecoverJobs_ALeaseThatNeverSettlesFailsWithinTheBootBudget proves the
// wait is bounded by the caller's context and reported, not endless.
func TestRecoverJobs_ALeaseThatNeverSettlesFailsWithinTheBootBudget(t *testing.T) {
	kv := newMockKVStore()
	svc := &agentServiceImpl{store: NewInMemoryAgentStore(), kvStore: kv, logger: &controlLoopMockLogger{}, node: "node-a"}
	gate := newGatedLeaseStore(t)
	svc.jobLeases, svc.leaseTTL = gate, DefaultJobLeaseTTL
	persistStatus(t, kv, &JobStatus{JobID: "job-mine", TenantID: "test-tenant", State: JobStateExecuting, Node: "node-a", Config: newTestConfig()})
	defer cancelAllJobs(svc)
	defer close(gate.release)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.recoverJobs(ctx) }()
	<-gate.arrived
	cancel()

	err := <-done
	var recoveryErr *JobRecoveryError
	if !errors.As(err, &recoveryErr) || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want a JobRecoveryError caused by the expired budget", err)
	}

	// THE GATE OUTLIVES THE BUDGET: the resumed job is still waiting for its
	// lease, so the signal the dispatcher waits on must still be open.
	select {
	case <-svc.ResumedJobsSettled():
		t.Fatal("ResumedJobsSettled closed while a resumed job still had no lease")
	default:
	}
	gate.release <- struct{}{}
	<-gate.acquired
	<-svc.ResumedJobsSettled()
}

// TestResumedJobsSettled_IsClosedBeforeAnyRecovery proves a node that
// recovered nothing gates nothing.
func TestResumedJobsSettled_IsClosedBeforeAnyRecovery(t *testing.T) {
	svc := &agentServiceImpl{}
	select {
	case <-svc.ResumedJobsSettled():
	default:
		t.Fatal("with no recovery there is nothing to wait for")
	}
}
