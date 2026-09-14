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
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// The envelope, driven by a fake runner
// =============================================================================
//
// These prove what the envelope owes EVERY runner, whatever shape it is: the
// lease before the first lifecycle moment, the heartbeat while it runs, the
// release on the way out, a terminal status the runner did not have to write,
// a panic classified rather than propagated, and Discard on every exit.

// envelopeTestTTL keeps the ticker fast enough that a heartbeat happens
// within a test without any wait.
const envelopeTestTTL = 30 * time.Millisecond

// newEnvelopeRun builds a run over a lease store with a fake runner's config.
func newEnvelopeRun(t *testing.T, cfg JobRunConfig) *JobRun {
	t.Helper()
	if cfg.Config == nil {
		cfg.Config = &JobConfig{TenantID: "acme", Description: "envelope test"}
	}
	if cfg.Agents == nil {
		cfg.Agents = &controlLoopMockService{}
	}
	if cfg.Logger == nil {
		cfg.Logger = &controlLoopMockLogger{}
	}
	if cfg.JobID == "" {
		cfg.JobID = "job-envelope"
	}
	run := newJobRun(cfg)
	run.StartState = JobStateInitializing
	return run
}

// TestJobEnvelope_LeaseIsHeldBeforeTheRunnerRuns proves the deliberate
// ordering change: the lease is taken before the first lifecycle moment and
// before the runner sees the job, it is heartbeaten while the runner runs,
// and an unbound job releases it on the way out.
func TestJobEnvelope_LeaseIsHeldBeforeTheRunnerRuns(t *testing.T) {
	t.Parallel()
	store := newScriptedLeaseStore(t, envelopeTestTTL)
	hooks := &recordingHooks{}

	var leaseAtPrepare, leaseAtRun *JobLease
	run := newEnvelopeRun(t, JobRunConfig{
		Leases: store, Node: "node-a", LeaseTTL: envelopeTestTTL, Hooks: hooks, HooksName: "recording",
	})
	hooks.discardFn = func(_ context.Context, _ *JobContext, _ bool) error { return nil }
	prepared := make(chan struct{})
	hooks.prepareFn = func() { leaseAtPrepare = run.Lease(); close(prepared) }

	runner := &fakeRunner{name: "fake", run: func(ctx context.Context, job *JobRun) error {
		leaseAtRun = job.Lease()
		// Hold the job until the ticker has beaten at least once, so the
		// heartbeat is observed rather than assumed.
		<-store.tickerBeat
		return nil
	}}

	if err := run.execute(context.Background(), runner); err != nil {
		t.Fatalf("execute: %v", err)
	}

	<-prepared
	if leaseAtPrepare == nil {
		t.Fatal("the lease must be held before Prepare runs")
	}
	if leaseAtRun == nil || leaseAtRun.JobID != run.JobID || leaseAtRun.Node != "node-a" {
		t.Fatalf("the runner must see the job's lease: %+v", leaseAtRun)
	}
	if store.heartbeats.Load() < 2 {
		t.Fatalf("expected a transition heartbeat and a ticker heartbeat, got %d", store.heartbeats.Load())
	}
	lease := run.Lease()
	if lease == nil || !lease.Released {
		t.Fatalf("an unbound job releases its lease on exit: %+v", lease)
	}
	if !run.Finished() {
		t.Fatal("a returned run reports itself finished")
	}
	if status := run.Status(); status == nil || status.State != JobStateCompleted {
		t.Fatalf("a runner that returned nil completes the job: %+v", status)
	}
	if calls := hooks.Calls(); calls[len(calls)-1] != "discard(completed)" {
		t.Fatalf("Discard runs on the way out: %v", calls)
	}
}

// TestJobEnvelope_BoundJobKeepsItsLeaseForTheConclusion proves a job that is
// an attempt at a task does NOT release its own lease: the conclusion does,
// after the task is written.
func TestJobEnvelope_BoundJobKeepsItsLeaseForTheConclusion(t *testing.T) {
	t.Parallel()
	store := newScriptedLeaseStore(t, envelopeTestTTL)
	svc := &boundTaskMockService{
		controlLoopMockService: &controlLoopMockService{},
		task:                   &Task{ID: "task-9", TenantID: "acme"},
	}
	run := newEnvelopeRun(t, JobRunConfig{
		Config: &JobConfig{TenantID: "acme", Description: "bound", TaskID: "task-9", Attempt: 2},
		Agents: svc, Leases: store, Node: "node-a", LeaseTTL: envelopeTestTTL,
	})

	if err := run.execute(context.Background(), &fakeRunner{name: "fake"}); err != nil {
		t.Fatalf("execute: %v", err)
	}

	lease := run.Lease()
	if lease == nil || lease.Released {
		t.Fatalf("a bound job's lease is released by the conclusion, not the run: %+v", lease)
	}
	if lease.TaskID != "task-9" || lease.Generation != 2 || lease.Ref != "task-9" {
		t.Fatalf("the lease names the task it is an attempt at: %+v", lease)
	}
	if !run.Bound() {
		t.Fatal("a job with a task is bound")
	}
	if run.Task == nil || run.Task.ID != "task-9" {
		t.Fatal("the envelope loads the task the job is an attempt at")
	}
}

// TestJobEnvelope_RunnerFailureIsClassifiedAndTerminal proves a runner that
// returns an error fails the job with a class, and Discard still runs.
func TestJobEnvelope_RunnerFailureIsClassifiedAndTerminal(t *testing.T) {
	t.Parallel()
	hooks := &recordingHooks{}
	run := newEnvelopeRun(t, JobRunConfig{Hooks: hooks, HooksName: "recording"})
	boom := errors.New("the runner could not finish")

	err := run.execute(context.Background(), &fakeRunner{name: "fake", run: func(context.Context, *JobRun) error {
		return boom
	}})
	if !errors.Is(err, boom) {
		t.Fatalf("execute must report the runner's error, got %v", err)
	}
	status := run.Status()
	if status == nil || status.State != JobStateFailed {
		t.Fatalf("a runner error fails the job: %+v", status)
	}
	if status.Error != boom.Error() {
		t.Fatalf("the failure must carry the cause: %+v", status)
	}
	if calls := hooks.Calls(); calls[len(calls)-1] != "discard(not completed)" {
		t.Fatalf("Discard runs on a failed exit too: %v", calls)
	}
}

// TestJobEnvelope_PanicIsClassifiedWithoutLeakingAGoroutine proves a runner
// that panics fails the job with FailureClassPanic, reports a typed error,
// still discards, and leaves no heartbeat goroutine behind.
func TestJobEnvelope_PanicIsClassifiedWithoutLeakingAGoroutine(t *testing.T) {
	store := newScriptedLeaseStore(t, envelopeTestTTL)
	hooks := &recordingHooks{}
	run := newEnvelopeRun(t, JobRunConfig{
		Leases: store, Node: "node-a", LeaseTTL: envelopeTestTTL, Hooks: hooks, HooksName: "recording",
	})

	before := runtime.NumGoroutine()
	err := run.execute(context.Background(), &fakeRunner{name: "fake", run: func(context.Context, *JobRun) error {
		panic("the runner blew up")
	}})

	var panicked *JobPanicError
	if !errors.As(err, &panicked) || !errors.Is(err, ErrJobPanic) {
		t.Fatalf("got %v, want JobPanicError", err)
	}
	if !strings.Contains(panicked.Value, "blew up") || panicked.Stack == "" {
		t.Fatalf("the panic must carry its value and stack: %+v", panicked)
	}
	status := run.Status()
	if status == nil || status.State != JobStateFailed || status.FailureClass != FailureClassPanic {
		t.Fatalf("a panic is its own failure class: %+v", status)
	}
	if calls := hooks.Calls(); calls[len(calls)-1] != "discard(not completed)" {
		t.Fatalf("Discard runs after a panic: %v", calls)
	}
	// stopHeartbeat waited for the ticker goroutine, so the count is settled.
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("a panicking run leaked goroutines: %d -> %d", before, after)
	}
}

// TestJobEnvelope_SupersededHeartbeatAbortsTheRun proves the lease is the
// authority: a heartbeat that finds another generation holding it cancels the
// run's context and the terminal error is the supersession, not the
// cancellation it caused.
func TestJobEnvelope_SupersededHeartbeatAbortsTheRun(t *testing.T) {
	t.Parallel()
	store := newScriptedLeaseStore(t, envelopeTestTTL)
	store.supersedeAt = 2
	run := newEnvelopeRun(t, JobRunConfig{Leases: store, Node: "node-a", LeaseTTL: envelopeTestTTL})

	err := run.execute(context.Background(), &fakeRunner{name: "fake", run: func(ctx context.Context, job *JobRun) error {
		<-store.superseded
		<-ctx.Done()
		// The runner reports what it saw: the cancellation. The envelope
		// must report the abort that caused it instead.
		return ctx.Err()
	}})

	var superseded *JobSupersededError
	if !errors.As(err, &superseded) {
		t.Fatalf("got %v, want JobSupersededError", err)
	}
	if status := run.Status(); status == nil || status.FailureClass != JobFailureClassSuperseded {
		t.Fatalf("a superseded job is classified as such: %+v", status)
	}
}

// TestJobRun_SetStateFiresEveryMomentInOrderAndPersistsEach proves SetState is
// the single choke point: each moment runs on entering its state, in order,
// and each transition writes and persists a snapshot.
func TestJobRun_SetStateFiresEveryMomentInOrderAndPersistsEach(t *testing.T) {
	t.Parallel()
	hooks := &recordingHooks{}
	var mu sync.Mutex
	var persisted []JobState
	run := newEnvelopeRun(t, JobRunConfig{
		Hooks: hooks, HooksName: "recording",
		PersistFn: func(status *JobStatus) {
			mu.Lock()
			defer mu.Unlock()
			persisted = append(persisted, status.State)
		},
	})

	ctx := context.Background()
	for _, state := range []JobState{
		JobStateInitializing, JobStateReviewing, JobStateCompleting, JobStateEscalating, JobStateCleanup,
	} {
		if err := run.SetState(ctx, state); err != nil {
			t.Fatalf("SetState(%s): %v", state, err)
		}
		if got := run.Status(); got == nil || got.State != state {
			t.Fatalf("SetState(%s) must write the status, got %+v", state, got)
		}
	}

	want := []string{"prepare", "reviewable", "complete", "blocked"}
	if got := hooks.Calls(); !equalStrings(got, want) {
		t.Fatalf("hook calls = %v, want %v", got, want)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, state := range []JobState{
		JobStateInitializing, JobStateReviewing, JobStateCompleting, JobStateEscalating, JobStateCleanup,
	} {
		found := false
		for _, p := range persisted {
			if p == state {
				found = true
			}
		}
		if !found {
			t.Fatalf("state %s was never persisted; persisted = %v", state, persisted)
		}
	}
}

// TestJobRun_SetStateFailsTheJobOnAHookError proves a lifecycle hook that
// fails is FailureClassHook, typed as a JobHookError, and the status says so.
func TestJobRun_SetStateFailsTheJobOnAHookError(t *testing.T) {
	t.Parallel()
	hooks := &recordingHooks{prepareErr: errors.New("no worktree")}
	run := newEnvelopeRun(t, JobRunConfig{Hooks: hooks, HooksName: "recording"})

	err := run.SetState(context.Background(), JobStateInitializing)
	var hookErr *JobHookError
	if !errors.As(err, &hookErr) || hookErr.Stage != JobStagePrepare {
		t.Fatalf("got %v, want JobHookError on prepare", err)
	}
	status := run.Status()
	if status == nil || status.State != JobStateFailed || status.FailureClass != FailureClassHook {
		t.Fatalf("a hook failure is its own class: %+v", status)
	}
}

// TestJobRun_UseHooksIsRefusedAfterTheFirstState proves a hook set installed
// once a moment has passed is refused rather than silently skipping it.
func TestJobRun_UseHooksIsRefusedAfterTheFirstState(t *testing.T) {
	t.Parallel()
	run := newEnvelopeRun(t, JobRunConfig{})
	hooks := &recordingHooks{}

	if err := run.UseHooks("recording", hooks); err != nil {
		t.Fatalf("UseHooks before the first state: %v", err)
	}
	if run.Hooks() == nil {
		t.Fatal("the installed hook set must be the job's")
	}
	if err := run.UseHooks("", nil); err == nil {
		t.Fatal("a nil hook set is refused")
	}

	if err := run.SetState(context.Background(), JobStateInitializing); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	err := run.UseHooks("late", &recordingHooks{})
	var refusal *JobHooksRegistrationError
	if !errors.As(err, &refusal) {
		t.Fatalf("got %v, want JobHooksRegistrationError", err)
	}
	if run.Hooks() != JobLifecycleHooks(hooks) {
		t.Fatal("a refused installation must not replace the hook set")
	}
}

// TestJobRun_SpawnAppliesTheEnvironmentAndTracksTheSession proves every
// session a runner spawns carries the engine's grant and configuration
// directory, is stamped with the job and project, and is released by CloseAll.
func TestJobRun_SpawnAppliesTheEnvironmentAndTracksTheSession(t *testing.T) {
	t.Parallel()
	svc := &spawnRecordingService{controlLoopMockService: &controlLoopMockService{}}
	run := newEnvelopeRun(t, JobRunConfig{
		Config: &JobConfig{TenantID: "acme", Description: "d", ProjectID: "proj-1"},
		Agents: svc,
		Policy: &JobLifecyclePolicy{AgentConfigDir: "/etc/quicbaas/agent"},
	})

	ctx := context.Background()
	resp, err := run.Spawn(ctx, &SpawnSessionRequest{Backend: "claude"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if resp.SessionID == "" {
		t.Fatal("a spawn returns a session")
	}

	spawns := svc.Spawns()
	if len(spawns) != 1 {
		t.Fatalf("expected one spawn, got %d", len(spawns))
	}
	got := spawns[0]
	if got.JobID != run.JobID || got.ProjectID != "proj-1" {
		t.Fatalf("the spawn must carry the job and project: %+v", got)
	}
	if got.Env["CLAUDE_CONFIG_DIR"] != "/etc/quicbaas/agent" {
		t.Fatalf("the operator's agent config dir must reach the session: %v", got.Env)
	}
	if !hasPermissionGrant(got.ExtraArgs) {
		t.Fatalf("the engine's own grant must reach the session: %v", got.ExtraArgs)
	}
	if ids := run.Sessions(); len(ids) != 1 || ids[0] != resp.SessionID {
		t.Fatalf("the session must be tracked: %v", ids)
	}

	if _, err := run.Spawn(ctx, nil); err == nil {
		t.Fatal("a nil spawn request is refused")
	}

	run.CloseAll(ctx)
	if ids := run.Sessions(); len(ids) != 0 {
		t.Fatalf("CloseAll releases every session, still holding %v", ids)
	}
	if closed := svc.closedSessions; len(closed) != 1 || closed[0] != resp.SessionID {
		t.Fatalf("CloseAll must close what it tracked: %v", closed)
	}

	// Close of one session is idempotent about tracking.
	run.Close(ctx, "")
	if ids := run.Sessions(); len(ids) != 0 {
		t.Fatalf("closing nothing changes nothing: %v", ids)
	}
}

// TestLaunchJob_RefusesAnUnknownRunnerBeforeAnyGoroutine proves the envelope
// admits a job only after its runner is resolved, and that a nil config is
// refused too.
func TestLaunchJob_RefusesAnUnknownRunnerBeforeAnyGoroutine(t *testing.T) {
	svc := newTestService(t)

	_, err := svc.launchJob(context.Background(), &JobConfig{
		TenantID: "acme", Description: "d", Runner: "nobody-registered-this",
	}, "", nil)
	var unknown *UnknownJobRunnerError
	if !errors.As(err, &unknown) {
		t.Fatalf("got %v, want UnknownJobRunnerError", err)
	}
	count := 0
	svc.jobs.Range(func(_, _ any) bool { count++; return true })
	if count != 0 {
		t.Fatalf("a refused job must not be registered, found %d", count)
	}

	if _, err := svc.launchJob(context.Background(), nil, "", nil); err == nil {
		t.Fatal("a nil config is refused")
	}
}

// TestLaunchJob_RegistersTheJobWithAStatusBeforeItRuns proves the record and
// its status exist the moment launchJob returns, so a caller that polls
// immediately finds the job rather than a gap — the guarantee StartJob used
// to buy with a two-second poll.
func TestLaunchJob_RegistersTheJobWithAStatusBeforeItRuns(t *testing.T) {
	svc := newTestService(t)
	blocked := make(chan struct{})
	defer close(blocked)

	runner := &fakeRunner{name: "envelope-registration-test", run: func(context.Context, *JobRun) error {
		<-blocked
		return nil
	}}
	registerJobRunnerForTest(t, runner)

	jobID, err := svc.launchJob(context.Background(), &JobConfig{
		TenantID: "acme", Description: "registered before it runs", Runner: runner.Name(),
	}, "", nil)
	if err != nil {
		t.Fatalf("launchJob: %v", err)
	}
	if jobID == "" {
		t.Fatal("the envelope mints the job ID")
	}
	defer cancelAllJobs(svc)

	status, err := svc.GetJobStatus(ContextWithTenant(context.Background(), "acme"), "acme", jobID)
	if err != nil {
		t.Fatalf("GetJobStatus immediately after launch: %v", err)
	}
	if status.JobID != jobID || status.Runner != runner.Name() {
		t.Fatalf("the status names the job and its runner: %+v", status)
	}
	if !svc.JobRunning("acme", jobID) {
		t.Fatal("a launched job is running here")
	}
}

// TestJobRun_ReadOnlyViewsAndPrompt proves the views a runner reads the job
// through — its memory, review round, prepared directory, failure class and
// the JobContext hooks receive — and that Prompt reaches the agent service.
func TestJobRun_ReadOnlyViewsAndPrompt(t *testing.T) {
	t.Parallel()
	svc := &controlLoopMockService{
		promptResponse: &PromptSessionResponse{SessionID: "s-1"},
	}
	run := newEnvelopeRun(t, JobRunConfig{
		Config: &JobConfig{TenantID: "acme", Description: "d", ProjectID: "proj-1", TaskID: "t-1", Attempt: 2},
		Agents: svc,
	})

	if run.Memory() == nil {
		t.Fatal("a run starts with an empty memory, not none")
	}
	if run.Iteration() != 0 || run.WorkDir() != "" || run.FailureClass() != "" {
		t.Fatalf("a fresh run has no round, directory or failure: %d %q %q",
			run.Iteration(), run.WorkDir(), run.FailureClass())
	}
	run.memory.AddSuccess("what worked")
	run.iteration = 3
	run.workDir = "/work/repo"
	run.Fail("", errors.New("it broke"))
	if run.FailureClass() != "" {
		t.Fatalf("an unclassifiable error carries no class, got %q", run.FailureClass())
	}
	if run.Status().Error != "it broke" {
		t.Fatalf("the failure carries its cause: %+v", run.Status())
	}

	job := run.Context()
	if job.Iteration != 3 || job.WorkDir != "/work/repo" || job.Memory != run.Memory() {
		t.Fatalf("the job context is refreshed from the run: %+v", job)
	}
	if job.TenantID != "acme" || job.ProjectID != "proj-1" || job.TaskID != "t-1" || job.Attempt != 2 {
		t.Fatalf("the job context carries the job's identity: %+v", job)
	}
	if run.Validators() != validators {
		t.Fatal("a run with no injected registry judges through the engine's")
	}
	if err := run.Guard().CheckCommand("v", "rm -rf /"); err == nil {
		t.Fatal("the zero policy's guard refuses every command")
	}

	resp, err := run.Prompt(context.Background(), &PromptSessionRequest{SessionID: "s-1", Prompt: "go"})
	if err != nil || resp.SessionID != "s-1" {
		t.Fatalf("Prompt must reach the agent service: %+v %v", resp, err)
	}

	svc.promptErr = errors.New("session gone")
	if _, err := run.Prompt(context.Background(), &PromptSessionRequest{SessionID: "s-1"}); err == nil {
		t.Fatal("a prompt failure must reach the runner")
	}
}

// TestJobRun_FanoutIsRecordedOnTheRunNotThePublishedStatus proves the
// invariant a published JobStatus carries: it is never written again. A child
// job spawned while the run is transitioning is recorded on the run and
// republished as a copy, so a status poll and the run's own next snapshot
// read a value nothing is writing.
//
// The defect this pins: appending to the published status's slice in place
// raced with the snapshot that read it (CI race detector, 2026-09-06).
func TestJobRun_FanoutIsRecordedOnTheRunNotThePublishedStatus(t *testing.T) {
	t.Parallel()
	run := newEnvelopeRun(t, JobRunConfig{})
	run.snapshot(JobStateExecuting)
	published := run.Status()

	run.RecordParent("job-parent", "agent-lead", 1234)
	run.AddChildJob("child-a")
	run.AddChildJob("child-b")
	run.AddChildJob("")

	// The status the run had already published is untouched.
	if published.ParentJobID != "" || len(published.ChildJobIDs) != 0 {
		t.Fatalf("a published status must never be written again: %+v", published)
	}

	got := run.Status()
	if got == published {
		t.Fatal("recording lineage must publish a new status, not mutate the old one")
	}
	if got.ParentJobID != "job-parent" || got.SpawnedBy != "agent-lead" || got.SpawnedAt != 1234 {
		t.Fatalf("lineage must reach the record at once: %+v", got)
	}
	if len(got.ChildJobIDs) != 2 || got.ChildJobIDs[0] != "child-a" || got.ChildJobIDs[1] != "child-b" {
		t.Fatalf("children = %v, want [child-a child-b]; an empty id is ignored", got.ChildJobIDs)
	}
	if ids := run.ChildJobIDs(); len(ids) != 2 {
		t.Fatalf("the run holds the lineage too: %v", ids)
	}

	// A later state change carries the lineage forward rather than dropping it.
	run.snapshot(JobStateCompleting)
	after := run.Status()
	if len(after.ChildJobIDs) != 2 || after.ParentJobID != "job-parent" {
		t.Fatalf("a snapshot must carry the lineage forward: %+v", after)
	}
	// The carried slice is the run's own copy, not the previous status's.
	if &after.ChildJobIDs[0] == &got.ChildJobIDs[0] {
		t.Fatal("each snapshot must own its slice so no two statuses alias one")
	}
}

// TestJobRun_FanoutRacesWithTheRunsOwnSnapshots is the race-detector guard:
// a caller recording children while the run transitions must produce no data
// race and lose no child.
func TestJobRun_FanoutRacesWithTheRunsOwnSnapshots(t *testing.T) {
	t.Parallel()
	run := newEnvelopeRun(t, JobRunConfig{})
	run.snapshot(JobStateInitializing)

	const children = 50
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < children; i++ {
			run.AddChildJob("child-" + itoa(i))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < children; i++ {
			run.snapshot(JobStateExecuting)
			_ = run.Status().ChildJobIDs
		}
	}()
	wg.Wait()

	run.snapshot(JobStateCompleting)
	if got := run.Status().ChildJobIDs; len(got) != children {
		t.Fatalf("every child must survive the transitions: got %d, want %d", len(got), children)
	}
}
