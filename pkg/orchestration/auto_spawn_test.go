// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"testing"
	"time"
)

// =============================================================================
// Auto-Spawn Unit Tests
// =============================================================================

// TestCreateTask_AutoSpawnFalse_NoJobCreated verifies that when AutoSpawn is
// false (the default), task creation does not attempt to start any job and the
// returned task has empty SpawnedJobID.
func TestCreateTask_AutoSpawnFalse_NoJobCreated(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)

	task, createErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:  "tenant-1",
		Subject:   "fix bug",
		AutoSpawn: false,
	})
	if createErr != nil {
		t.Fatalf("expected no error, got %v", createErr)
	}

	if task.AutoSpawn {
		t.Error("expected AutoSpawn=false on returned task")
	}
	if task.SpawnedJobID != "" {
		t.Errorf("expected empty SpawnedJobID, got %q", task.SpawnedJobID)
	}
	if task.SpawnedAt != 0 {
		t.Errorf("expected SpawnedAt=0, got %d", task.SpawnedAt)
	}

	// Verify task persisted in store exactly once.
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.createCalls) != 1 {
		t.Fatalf("expected 1 store.CreateTask call, got %d", len(store.createCalls))
	}
	storedTask := store.createCalls[0].Task
	if storedTask.AutoSpawn {
		t.Error("persisted task should have AutoSpawn=false")
	}
	if storedTask.SpawnedJobID != "" {
		t.Errorf("persisted task should have empty SpawnedJobID, got %q", storedTask.SpawnedJobID)
	}
}

// TestCreateTask_AutoSpawnTrue_NoAgentRegistry returns ErrNoMatchingAgent when
// AutoSpawn is requested but the agent registry is not configured.
func TestCreateTask_AutoSpawnTrue_NoAgentRegistry(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)
	// No agentRegistry set — autoSpawnTask should return NoMatchingAgentError.

	task, spawnErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:  "tenant-1",
		Subject:   "fix bug",
		AutoSpawn: true,
	})

	// Task is still returned (it was persisted before spawn attempted).
	if task == nil {
		t.Fatal("expected non-nil task even when spawn fails")
	}
	if !errors.Is(spawnErr, ErrNoMatchingAgent) {
		t.Fatalf("expected ErrNoMatchingAgent, got %v", spawnErr)
	}
	if task.SpawnedJobID != "" {
		t.Errorf("expected empty SpawnedJobID when spawn fails, got %q", task.SpawnedJobID)
	}

	// Task must have been persisted.
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.createCalls) != 1 {
		t.Fatalf("expected 1 store.CreateTask call, got %d", len(store.createCalls))
	}
}

// TestCreateTask_AutoSpawnTrue_BackendFilterNoMatch returns NoMatchingAgentError
// when a backend constraint is specified but no online agent uses that backend.
// This is Gap 3: autoSpawnTask must surface a typed error rather than silently
// dispatching to any agent.
func TestCreateTask_AutoSpawnTrue_BackendFilterNoMatch(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	registry := NewInMemoryAgentRegistry()

	// Register a live agent that uses a DIFFERENT backend (anthropic).
	onlineAgent := &AgentRegistration{
		ID:        "agent-1",
		TenantID:  "tenant-1",
		Name:      "worker-1",
		Host:      "host-1",
		Backend:   "anthropic",
		AgentType: "worker",
		Status:    AgentStatusIdle,
	}
	if regErr := registry.RegisterAgent(context.Background(), onlineAgent); regErr != nil {
		t.Fatalf("failed to register agent: %v", regErr)
	}

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)
	impl.SetAgentRegistry(registry)

	// Request a task with Backend="ollama"; no agent offers ollama.
	task, spawnErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:  "tenant-1",
		Subject:   "run ollama inference",
		AutoSpawn: true,
		Backend:   "ollama",
	})

	if task == nil {
		t.Fatal("expected non-nil task even when spawn fails")
	}
	var name *NoMatchingAgentError
	if !errors.As(spawnErr, &name) {
		t.Fatalf("expected *NoMatchingAgentError, got %T: %v", spawnErr, spawnErr)
	}
	if task.SpawnedJobID != "" {
		t.Errorf("expected empty SpawnedJobID when backend not found, got %q", task.SpawnedJobID)
	}
}

// TestCreateTask_AutoSpawnTrue_BackendFilterMatch succeeds when the requesting
// backend matches an online agent's backend.
func TestCreateTask_AutoSpawnTrue_BackendFilterMatch(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	registry := NewInMemoryAgentRegistry()

	// Register a live agent that uses ollama.
	onlineAgent := &AgentRegistration{
		ID:        "agent-ollama",
		TenantID:  "tenant-1",
		Name:      "ollama-worker",
		Host:      "host-1",
		Backend:   "ollama",
		AgentType: "worker",
		Status:    AgentStatusIdle,
	}
	if regErr := registry.RegisterAgent(context.Background(), onlineAgent); regErr != nil {
		t.Fatalf("failed to register agent: %v", regErr)
	}

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)
	impl.SetAgentRegistry(registry)

	// Task creation returns the task; spawn may fail because no real job runner
	// is wired — but the backend selection should succeed (no NoMatchingAgentError).
	task, spawnErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:  "tenant-1",
		Subject:   "run ollama inference",
		AutoSpawn: true,
		Backend:   "ollama",
	})

	if task == nil {
		t.Fatal("expected non-nil task")
	}
	// spawnErr may be a job-runner error, but must NOT be a NoMatchingAgentError.
	var name *NoMatchingAgentError
	if errors.As(spawnErr, &name) {
		t.Fatalf("should have matched the ollama agent, got NoMatchingAgentError: %v", spawnErr)
	}
}

// TestCreateTask_AutoSpawnTrue_NoOnlineAgents returns ErrNoMatchingAgent when
// all registered agents are offline.
func TestCreateTask_AutoSpawnTrue_NoOnlineAgents(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	registry := NewInMemoryAgentRegistry()

	// Register an agent that is offline.
	offlineAgent := &AgentRegistration{
		ID:        "agent-offline",
		TenantID:  "tenant-1",
		Name:      "worker-1",
		Host:      "host-1",
		Backend:   "ollama",
		AgentType: "worker",
		Status:    AgentStatusOffline,
	}
	if regErr := registry.RegisterAgent(context.Background(), offlineAgent); regErr != nil {
		t.Fatalf("failed to register agent: %v", regErr)
	}

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)
	impl.SetAgentRegistry(registry)

	task, spawnErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:  "tenant-1",
		Subject:   "fix bug",
		AutoSpawn: true,
	})

	if task == nil {
		t.Fatal("expected non-nil task even when spawn fails")
	}
	if !errors.Is(spawnErr, ErrNoMatchingAgent) {
		t.Fatalf("expected ErrNoMatchingAgent, got %v", spawnErr)
	}
	if task.SpawnedJobID != "" {
		t.Errorf("expected empty SpawnedJobID when no agents available, got %q", task.SpawnedJobID)
	}
}

// TestCreateTask_AutoSpawnTrue_GpuRequired_NoGpuAgents returns ErrNoMatchingAgent
// when GpuRequired is set but no agents advertise GPU capability.
func TestCreateTask_AutoSpawnTrue_GpuRequired_NoGpuAgents(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	registry := NewInMemoryAgentRegistry()

	// Register an idle non-GPU agent.
	nonGPUAgent := &AgentRegistration{
		ID:           "agent-cpu",
		TenantID:     "tenant-1",
		Name:         "cpu-worker",
		Host:         "host-cpu",
		Backend:      "ollama",
		AgentType:    "worker",
		Status:       AgentStatusIdle,
		Capabilities: []string{"inference"},
	}
	if regErr := registry.RegisterAgent(context.Background(), nonGPUAgent); regErr != nil {
		t.Fatalf("failed to register non-GPU agent: %v", regErr)
	}

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)
	impl.SetAgentRegistry(registry)

	task, spawnErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:    "tenant-1",
		Subject:     "train model",
		AutoSpawn:   true,
		GpuRequired: true,
	})

	if task == nil {
		t.Fatal("expected non-nil task even when GPU spawn fails")
	}
	if !errors.Is(spawnErr, ErrNoMatchingAgent) {
		t.Fatalf("expected ErrNoMatchingAgent for GPU-required with no GPU agents, got %v", spawnErr)
	}
	if task.SpawnedJobID != "" {
		t.Errorf("expected empty SpawnedJobID, got %q", task.SpawnedJobID)
	}
}

// TestCreateTask_AutoSpawnTrue_Success verifies that when a matching online agent
// is present, autoSpawnTask starts a job and populates SpawnedJobID on the task.
func TestCreateTask_AutoSpawnTrue_Success(t *testing.T) {
	t.Parallel()

	// Use in-memory task store so CAS semantics work correctly.
	taskStore := NewInMemoryTaskStore()
	registry := NewInMemoryAgentRegistry()

	// Register an idle agent.
	idleAgent := &AgentRegistration{
		ID:        "agent-idle",
		TenantID:  "tenant-1",
		Name:      "worker-gpu",
		Host:      "host-gpu",
		Backend:   "ollama",
		AgentType: "worker",
		Status:    AgentStatusIdle,
	}
	if regErr := registry.RegisterAgent(context.Background(), idleAgent); regErr != nil {
		t.Fatalf("failed to register agent: %v", regErr)
	}

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(taskStore)
	impl.SetAgentRegistry(registry)

	// Register a minimal backend so StartJob can build a control loop.
	backend := &testMinimalBackend{}
	impl.backends.Store("ollama", backend)

	task, createErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:  "tenant-1",
		Subject:   "fix bug",
		AutoSpawn: true,
	})

	if createErr != nil {
		t.Fatalf("expected no error from CreateTask with auto-spawn, got %v", createErr)
	}
	if task == nil {
		t.Fatal("expected non-nil task")
	}
	if !task.AutoSpawn {
		t.Error("expected task.AutoSpawn=true")
	}
	if task.SpawnedJobID == "" {
		t.Error("expected non-empty SpawnedJobID after successful auto-spawn")
	}
	if task.SpawnedAt == 0 {
		t.Error("expected non-zero SpawnedAt after successful auto-spawn")
	}

	// Verify the task was also updated in the store with the spawned job ID.
	stored, getErr := taskStore.GetTask(context.Background(), "tenant-1", task.ID)
	if getErr != nil {
		t.Fatalf("failed to retrieve task: %v", getErr)
	}
	if stored.SpawnedJobID == "" {
		t.Error("expected stored task to have SpawnedJobID populated")
	}
}

// TestCreateTask_AutoSpawnTrue_TaskFieldSet verifies the AutoSpawn field is
// persisted correctly in the task store for later queries.
func TestCreateTask_AutoSpawnTrue_TaskFieldSet(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)
	// No agent registry — spawn will fail, but task is still persisted.

	task, _ := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:  "tenant-1",
		Subject:   "my work",
		AutoSpawn: true,
	})

	if task == nil {
		t.Fatal("expected non-nil task")
	}
	// The AutoSpawn field on the returned task must reflect the request.
	if !task.AutoSpawn {
		t.Error("expected task.AutoSpawn=true")
	}

	// Verify what was passed to the store.
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.createCalls) == 0 {
		t.Fatal("expected store.CreateTask to be called")
	}
	stored := store.createCalls[0].Task
	if !stored.AutoSpawn {
		t.Error("stored task should have AutoSpawn=true")
	}
}

// TestBoolStr verifies boolStr converts bool values correctly.
func TestBoolStr(t *testing.T) {
	t.Parallel()
	if boolStr(true) != "true" {
		t.Errorf("expected 'true', got %q", boolStr(true))
	}
	if boolStr(false) != "false" {
		t.Errorf("expected 'false', got %q", boolStr(false))
	}
}

// =============================================================================
// testMinimalBackend is a minimal AgentBackend for testing auto-spawn.
// It satisfies the AgentBackend interface without doing real work.
// =============================================================================

type testMinimalBackend struct{}

func (b *testMinimalBackend) Type() string                      { return "test" }
func (b *testMinimalBackend) Capabilities() BackendCapabilities { return BackendCapabilities{} }
func (b *testMinimalBackend) Spawn(_ context.Context, _ *SpawnRequest) (*Session, error) {
	return &Session{ID: "test-session"}, nil
}
func (b *testMinimalBackend) Prompt(_ context.Context, _ *PromptRequest) (*PromptResponse, error) {
	return &PromptResponse{}, nil
}
func (b *testMinimalBackend) Close(_ context.Context, _ string) error { return nil }
func (b *testMinimalBackend) Status(_ context.Context, _ string) (*SessionStatus, error) {
	return &SessionStatus{State: SessionStateActive}, nil
}
func (b *testMinimalBackend) ToolGuidance() string { return "" }

var _ AgentBackend = (*testMinimalBackend)(nil)

// =============================================================================
// Auto-spawn under a module's runner
// =============================================================================

// TestCreateTask_ModuleRunnerNeedsNoFleetAgent proves the dispatch rule the
// JobRunner seam introduces: a task whose runner is a module's is dispatched
// with no fleet agent registered, no backend and no project workflow, because
// the runner spawns its own sessions and the job's lease is the capacity
// check. The lead-driven runner still needs all three — that refusal is
// TestCreateTask_AutoSpawnTrue_NoAgentRegistry.
func TestCreateTask_ModuleRunnerNeedsNoFleetAgent(t *testing.T) {
	taskStore := NewInMemoryTaskStore()
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(taskStore)
	// Deliberately no agent registry, no backend and no project.

	ran := make(chan *JobRun, 1)
	release := make(chan struct{})
	defer close(release)
	runner := &fakeRunner{
		name:   "auto-spawn-module-test",
		states: []JobStateSpec{{State: JobStateInitializing, Label: "Initializing"}},
		run: func(_ context.Context, job *JobRun) error {
			ran <- job
			<-release
			return nil
		},
	}
	registerJobRunnerForTest(t, runner)

	task, createErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:  "tenant-1",
		Subject:   "close the issue",
		AutoSpawn: true,
		Runner:    runner.Name(),
	})
	if createErr != nil {
		t.Fatalf("a module runner needs no fleet agent: %v", createErr)
	}
	defer cancelAllJobs(impl)

	if task.Runner != runner.Name() {
		t.Fatalf("the task records its runner, got %q", task.Runner)
	}
	if task.SpawnedJobID == "" || task.SpawnedAt == 0 || task.Attempts != 1 {
		t.Fatalf("the attempt must be recorded on the task: %+v", task)
	}
	if task.DispatchReason != "" {
		t.Fatalf("a dispatched task carries no refusal, got %q", task.DispatchReason)
	}

	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case job := <-ran:
		if job.Config.Runner != runner.Name() || job.Config.TaskID != task.ID || job.Config.Attempt != 1 {
			t.Fatalf("the job is an attempt at the task: %+v", job.Config)
		}
		if job.Config.WorkflowName != "" {
			t.Fatalf("a module runner's job resolves no workflow, got %q", job.Config.WorkflowName)
		}
		if job.JobID != task.SpawnedJobID {
			t.Fatalf("job %s does not match the task's spawned job %s", job.JobID, task.SpawnedJobID)
		}
	case <-timer.C:
		t.Fatal("the module runner never ran")
	}

	stored, getErr := taskStore.GetTask(context.Background(), "tenant-1", task.ID)
	if getErr != nil {
		t.Fatalf("GetTask: %v", getErr)
	}
	if stored.Runner != runner.Name() || stored.SpawnedJobID != task.SpawnedJobID {
		t.Fatalf("the runner and the attempt must survive the store: %+v", stored)
	}
}

// TestCreateTask_DispatchRefusalLandsOnTheTask proves a refused dispatch is
// visible on the record rather than only in the error the caller got: the
// reason is written to the task, so a task left pending says what it is
// waiting for.
func TestCreateTask_DispatchRefusalLandsOnTheTask(t *testing.T) {
	t.Parallel()

	taskStore := NewInMemoryTaskStore()
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	svc.(*agentServiceImpl).SetTaskStore(taskStore)
	// No agent registry: the lead-driven runner has nothing to assign to.

	task, spawnErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:  "tenant-1",
		Subject:   "fix bug",
		AutoSpawn: true,
	})
	if !errors.Is(spawnErr, ErrNoMatchingAgent) {
		t.Fatalf("got %v, want ErrNoMatchingAgent", spawnErr)
	}
	if task.DispatchReason == "" || !strings.Contains(task.DispatchReason, "agent registry not configured") {
		t.Fatalf("the returned task must carry the refusal, got %q", task.DispatchReason)
	}

	stored, getErr := taskStore.GetTask(context.Background(), "tenant-1", task.ID)
	if getErr != nil {
		t.Fatalf("GetTask: %v", getErr)
	}
	if stored.DispatchReason != task.DispatchReason {
		t.Fatalf("the refusal must be persisted: stored %q, returned %q", stored.DispatchReason, task.DispatchReason)
	}
	if stored.Status != TaskStatusPending || stored.SpawnedJobID != "" {
		t.Fatalf("a refused task stays pending and unspawned: %+v", stored)
	}
}

// TestCreateTask_RunnerIsResolvedAndValidated proves the runner is recorded
// on every task — an unnamed one resolving to the engine's lead-driven
// runner — and that a name nothing registered is refused, typed, before a
// task exists.
func TestCreateTask_RunnerIsResolvedAndValidated(t *testing.T) {
	t.Parallel()

	taskStore := NewInMemoryTaskStore()
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	svc.(*agentServiceImpl).SetTaskStore(taskStore)
	ctx := context.Background()

	task, createErr := svc.CreateTask(ctx, &CreateTaskRequest{TenantID: "tenant-1", Subject: "unnamed runner"})
	if createErr != nil {
		t.Fatalf("CreateTask: %v", createErr)
	}
	if task.Runner != JobRunnerWorkflow {
		t.Fatalf("an unnamed runner resolves to %q, got %q", JobRunnerWorkflow, task.Runner)
	}

	refused, refusal := svc.CreateTask(ctx, &CreateTaskRequest{
		TenantID: "tenant-1", Subject: "nobody's runner", Runner: "no-such-runner",
	})
	var unknown *UnknownJobRunnerError
	if !errors.As(refusal, &unknown) || unknown.Name != "no-such-runner" {
		t.Fatalf("got %v, want UnknownJobRunnerError", refusal)
	}
	if refused != nil {
		t.Fatal("a task with an unrunnable runner is never created")
	}
	tasks, listErr := taskStore.ListTasks(ctx, "tenant-1", nil)
	if listErr != nil {
		t.Fatalf("ListTasks: %v", listErr)
	}
	if len(tasks) != 1 {
		t.Fatalf("only the valid task exists, got %d", len(tasks))
	}
}

// TestRecordDispatchRefusal_IsACASWriteThatNeverFailsTheCaller proves the
// refusal write is diagnostic and safe: a lost CAS is retried against the
// fresh record, a task that has since been dispatched keeps its record, an
// unchanged reason is not rewritten, and no store failure escapes.
func TestRecordDispatchRefusal_IsACASWriteThatNeverFailsTheCaller(t *testing.T) {
	svc := newTestService(t)
	inner := NewInMemoryTaskStore()
	store := &conflictingTaskStore{TaskStore: inner}
	svc.SetTaskStore(store)
	ctx := context.Background()

	task := &Task{ID: "task-refused", TenantID: "acme", Subject: "s", Status: TaskStatusPending}
	if err := inner.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	refusal := &NoMatchingAgentError{Constraint: "agent_selection", Detail: "nothing online"}
	svc.recordDispatchRefusal(ctx, task, refusal)
	if store.updates.Load() != 2 {
		t.Fatalf("expected the conflict and one retry, got %d updates", store.updates.Load())
	}
	stored := readTask(t, inner, task.ID)
	if stored.DispatchReason != refusal.Error() {
		t.Fatalf("the refusal must land on the record, got %q", stored.DispatchReason)
	}
	if stored.Metadata["concurrent"] != "yes" || stored.Priority != 7 {
		t.Fatalf("the concurrent change was overwritten: %+v", stored)
	}
	if task.DispatchReason != refusal.Error() {
		t.Fatalf("the caller's copy must match the record, got %q", task.DispatchReason)
	}

	// The same reason is not written a second time.
	before := store.updates.Load()
	svc.recordDispatchRefusal(ctx, task, refusal)
	if store.updates.Load() != before {
		t.Fatal("an unchanged reason must not be rewritten")
	}

	// A task that has since been dispatched keeps its record.
	dispatched := readTask(t, inner, task.ID)
	dispatched.SpawnedJobID = "job-1"
	dispatched.DispatchReason = ""
	if err := inner.UpdateTask(ctx, dispatched); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	svc.recordDispatchRefusal(ctx, dispatched, errors.New("too late"))
	if got := readTask(t, inner, task.ID); got.DispatchReason != "" {
		t.Fatalf("a dispatched task takes no refusal, got %q", got.DispatchReason)
	}

	// Nothing to write to, nothing to write, and a store that never accepts
	// the write: each is silent.
	noStore := newTestService(t)
	noStore.taskStore = nil
	noStore.recordDispatchRefusal(ctx, task, refusal)

	svc.recordDispatchRefusal(ctx, nil, refusal)
	svc.recordDispatchRefusal(ctx, task, nil)

	stubborn := newTestService(t)
	stubbornInner := NewInMemoryTaskStore()
	stubborn.SetTaskStore(&alwaysConflictingTaskStore{TaskStore: stubbornInner})
	pending := &Task{ID: "task-stubborn", TenantID: "acme", Subject: "s", Status: TaskStatusPending}
	if err := stubbornInner.CreateTask(ctx, pending); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	stubborn.recordDispatchRefusal(ctx, pending, refusal)
	if got := readTask(t, stubbornInner, pending.ID); got.DispatchReason != "" {
		t.Fatalf("an exhausted retry writes nothing, got %q", got.DispatchReason)
	}

	// A task the store cannot read is reported, not panicked over.
	gone := &Task{ID: "task-gone", TenantID: "acme"}
	svc.recordDispatchRefusal(ctx, gone, refusal)
}

// TestAutoSpawn_JobDescriptionAndRunnerRefusal proves the two ends of the
// dispatch: the job an attempt runs under carries the task's subject and,
// when it has one, its description; and a runner that refuses the job it is
// handed refuses the dispatch, with its reason on the task.
func TestAutoSpawn_JobDescriptionAndRunnerRefusal(t *testing.T) {
	taskStore := NewInMemoryTaskStore()
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(taskStore)
	ctx := context.Background()

	descriptions := make(chan string, 2)
	release := make(chan struct{})
	defer close(release)
	accepting := &fakeRunner{
		name:   "auto-spawn-description-test",
		states: []JobStateSpec{{State: JobStateInitializing, Label: "Initializing"}},
		run: func(_ context.Context, job *JobRun) error {
			descriptions <- job.Config.Description
			<-release
			return nil
		},
	}
	registerJobRunnerForTest(t, accepting)
	defer cancelAllJobs(impl)

	for name, tc := range map[string]struct {
		req  *CreateTaskRequest
		want string
	}{
		"subject and description": {
			req:  &CreateTaskRequest{TenantID: "tenant-1", Subject: "close it", Description: "the issue body"},
			want: "close it: the issue body",
		},
		"subject alone": {
			req:  &CreateTaskRequest{TenantID: "tenant-1", Subject: "close it"},
			want: "close it",
		},
	} {
		tc.req.AutoSpawn = true
		tc.req.Runner = accepting.Name()
		if _, createErr := svc.CreateTask(ctx, tc.req); createErr != nil {
			t.Fatalf("%s: CreateTask: %v", name, createErr)
		}
		timer := time.NewTimer(10 * time.Second)
		select {
		case got := <-descriptions:
			if got != tc.want {
				t.Errorf("%s: job description = %q, want %q", name, got, tc.want)
			}
		case <-timer.C:
			t.Fatalf("%s: the runner never ran", name)
		}
		timer.Stop()
	}

	// A runner that will not accept the job refuses the dispatch, and the
	// reason is on the task rather than only in the returned error.
	refusing := &fakeRunner{
		name:        "auto-spawn-refusing-test",
		states:      []JobStateSpec{{State: JobStateInitializing, Label: "Initializing"}},
		validateErr: errors.New("this runner wants a project"),
	}
	registerJobRunnerForTest(t, refusing)

	task, spawnErr := svc.CreateTask(ctx, &CreateTaskRequest{
		TenantID: "tenant-1", Subject: "unrunnable", AutoSpawn: true, Runner: refusing.Name(),
	})
	if !errors.Is(spawnErr, ErrAgentServiceUnavailable) {
		t.Fatalf("got %v, want the wrapped start-job refusal", spawnErr)
	}
	if !strings.Contains(task.DispatchReason, "this runner wants a project") {
		t.Fatalf("the runner's refusal must reach the task, got %q", task.DispatchReason)
	}
	if task.SpawnedJobID != "" || task.Attempts != 0 {
		t.Fatalf("a refused dispatch charges no attempt: %+v", task)
	}
}

// TestAutoSpawn_AClaimTheStoreRefusesStartsNoJob proves the dispatch cannot
// get ahead of the record: when the task store will not accept the claim, no
// job is started at all, rather than a job running under a task that never
// took it (#670).
func TestAutoSpawn_AClaimTheStoreRefusesStartsNoJob(t *testing.T) {
	inner := NewInMemoryTaskStore()
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(&alwaysConflictingTaskStore{TaskStore: inner})

	release := make(chan struct{})
	defer close(release)
	runner := &fakeRunner{
		name:   "auto-spawn-unclaimable-test",
		states: []JobStateSpec{{State: JobStateInitializing, Label: "Initializing"}},
		run: func(context.Context, *JobRun) error {
			<-release
			return nil
		},
	}
	registerJobRunnerForTest(t, runner)

	task, createErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID: "tenant-1", Subject: "unclaimable", AutoSpawn: true, Runner: runner.Name(),
	})
	if !errors.Is(createErr, ErrTaskSpawnClaim) {
		t.Fatalf("a claim the store refuses is reported, got %v", createErr)
	}
	defer cancelAllJobs(impl)

	// launchJob records a job before it starts the goroutine, so an empty
	// registry here is proof no job was launched rather than a race with one.
	jobs := 0
	impl.jobs.Range(func(_, _ any) bool {
		jobs++
		return true
	})
	if jobs != 0 {
		t.Fatalf("an unclaimed task starts no job, got %d", jobs)
	}

	if task.SpawnedJobID != "" || task.SpawnedAt != 0 || task.Attempts != 0 {
		t.Fatalf("the returned task must match the persisted one: %+v", task)
	}
	stored, getErr := inner.GetTask(context.Background(), "tenant-1", task.ID)
	if getErr != nil {
		t.Fatalf("GetTask: %v", getErr)
	}
	if stored.SpawnedJobID != "" || stored.Attempts != 0 {
		t.Fatalf("nothing was written: %+v", stored)
	}
}
