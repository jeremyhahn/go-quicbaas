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

// TestCreateTask_AutoSpawnTrue_NoAgentRegistry returns ErrNoMatchingFleetNode when
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
	// No fleetRegistry set — autoSpawnTask should return NoMatchingFleetNodeError.

	task, spawnErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:  "tenant-1",
		Subject:   "fix bug",
		AutoSpawn: true,
	})

	// Task is still returned (it was persisted before spawn attempted).
	if task == nil {
		t.Fatal("expected non-nil task even when spawn fails")
	}
	if !errors.Is(spawnErr, ErrNoMatchingFleetNode) {
		t.Fatalf("expected ErrNoMatchingFleetNode, got %v", spawnErr)
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

// TestCreateTask_AutoSpawnTrue_BackendFilterNoMatch returns NoMatchingFleetNodeError
// when a backend constraint is specified but no online agent uses that backend.
// This is Gap 3: autoSpawnTask must surface a typed error rather than silently
// dispatching to any agent.
func TestCreateTask_AutoSpawnTrue_BackendFilterNoMatch(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	registry := NewInMemoryFleetRegistry()

	// Register a live agent that uses a DIFFERENT backend (anthropic).
	onlineAgent := &FleetNode{
		ID:       "agent-1",
		TenantID: "tenant-1",
		Name:     "worker-1",
		Host:     "host-1",
		Backend:  "anthropic",
		NodeType: "worker",
		Status:   FleetNodeStatusIdle,
	}
	if regErr := registry.RegisterFleetNode(context.Background(), onlineAgent); regErr != nil {
		t.Fatalf("failed to register agent: %v", regErr)
	}

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)
	impl.SetFleetRegistry(registry)

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
	var name *NoMatchingFleetNodeError
	if !errors.As(spawnErr, &name) {
		t.Fatalf("expected *NoMatchingFleetNodeError, got %T: %v", spawnErr, spawnErr)
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
	registry := NewInMemoryFleetRegistry()

	// Register a live agent that uses ollama.
	onlineAgent := &FleetNode{
		ID:       "agent-ollama",
		TenantID: "tenant-1",
		Name:     "ollama-worker",
		Host:     "host-1",
		Backend:  "ollama",
		NodeType: "worker",
		Status:   FleetNodeStatusIdle,
	}
	if regErr := registry.RegisterFleetNode(context.Background(), onlineAgent); regErr != nil {
		t.Fatalf("failed to register agent: %v", regErr)
	}

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)
	impl.SetFleetRegistry(registry)

	// Task creation returns the task; spawn may fail because no real job runner
	// is wired — but the backend selection should succeed (no NoMatchingFleetNodeError).
	task, spawnErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:  "tenant-1",
		Subject:   "run ollama inference",
		AutoSpawn: true,
		Backend:   "ollama",
	})

	if task == nil {
		t.Fatal("expected non-nil task")
	}
	// spawnErr may be a job-runner error, but must NOT be a NoMatchingFleetNodeError.
	var name *NoMatchingFleetNodeError
	if errors.As(spawnErr, &name) {
		t.Fatalf("should have matched the ollama agent, got NoMatchingFleetNodeError: %v", spawnErr)
	}
}

// TestCreateTask_AutoSpawnTrue_NoOnlineAgents returns ErrNoMatchingFleetNode when
// all registered agents are offline.
func TestCreateTask_AutoSpawnTrue_NoOnlineAgents(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	registry := NewInMemoryFleetRegistry()

	// Register an agent that is offline.
	offlineAgent := &FleetNode{
		ID:       "agent-offline",
		TenantID: "tenant-1",
		Name:     "worker-1",
		Host:     "host-1",
		Backend:  "ollama",
		NodeType: "worker",
		Status:   FleetNodeStatusOffline,
	}
	if regErr := registry.RegisterFleetNode(context.Background(), offlineAgent); regErr != nil {
		t.Fatalf("failed to register agent: %v", regErr)
	}

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)
	impl.SetFleetRegistry(registry)

	task, spawnErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:  "tenant-1",
		Subject:   "fix bug",
		AutoSpawn: true,
	})

	if task == nil {
		t.Fatal("expected non-nil task even when spawn fails")
	}
	if !errors.Is(spawnErr, ErrNoMatchingFleetNode) {
		t.Fatalf("expected ErrNoMatchingFleetNode, got %v", spawnErr)
	}
	if task.SpawnedJobID != "" {
		t.Errorf("expected empty SpawnedJobID when no agents available, got %q", task.SpawnedJobID)
	}
}

// TestCreateTask_AutoSpawnTrue_GpuRequired_NoGpuAgents returns ErrNoMatchingFleetNode
// when GpuRequired is set but no agents advertise GPU capability.
func TestCreateTask_AutoSpawnTrue_GpuRequired_NoGpuAgents(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	registry := NewInMemoryFleetRegistry()

	// Register an idle non-GPU agent.
	nonGPUAgent := &FleetNode{
		ID:           "agent-cpu",
		TenantID:     "tenant-1",
		Name:         "cpu-worker",
		Host:         "host-cpu",
		Backend:      "ollama",
		NodeType:     "worker",
		Status:       FleetNodeStatusIdle,
		Capabilities: []string{"inference"},
	}
	if regErr := registry.RegisterFleetNode(context.Background(), nonGPUAgent); regErr != nil {
		t.Fatalf("failed to register non-GPU agent: %v", regErr)
	}

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)
	impl.SetFleetRegistry(registry)

	task, spawnErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:    "tenant-1",
		Subject:     "train model",
		AutoSpawn:   true,
		GpuRequired: true,
	})

	if task == nil {
		t.Fatal("expected non-nil task even when GPU spawn fails")
	}
	if !errors.Is(spawnErr, ErrNoMatchingFleetNode) {
		t.Fatalf("expected ErrNoMatchingFleetNode for GPU-required with no GPU agents, got %v", spawnErr)
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
	registry := NewInMemoryFleetRegistry()

	// Register an idle agent.
	idleAgent := &FleetNode{
		ID:       "agent-idle",
		TenantID: "tenant-1",
		Name:     "worker-gpu",
		Host:     "host-gpu",
		Backend:  "ollama",
		NodeType: "worker",
		Status:   FleetNodeStatusIdle,
	}
	if regErr := registry.RegisterFleetNode(context.Background(), idleAgent); regErr != nil {
		t.Fatalf("failed to register agent: %v", regErr)
	}

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(taskStore)
	impl.SetFleetRegistry(registry)

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
// with no fleet node registered, no backend and no project workflow, because
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
		t.Fatalf("a module runner needs no fleet node: %v", createErr)
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
	if !errors.Is(spawnErr, ErrNoMatchingFleetNode) {
		t.Fatalf("got %v, want ErrNoMatchingFleetNode", spawnErr)
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

	refusal := &NoMatchingFleetNodeError{Constraint: "agent_selection", Detail: "nothing online"}
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

// awaitTaskStatus waits for a task to reach status, or fails the test. The
// conclusion runs on the job's own goroutine (finishJob), so the moment it is
// safe to read is after the record says so rather than after Run returned; a
// ticker is used rather than time.Sleep, per the project's conventions.
func awaitTaskStatus(t *testing.T, store TaskStore, tenantID, taskID string, status TaskStatus) *Task {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		task, err := store.GetTask(context.Background(), tenantID, taskID)
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		if task.Status == status {
			return task
		}
		select {
		case <-timer.C:
			t.Fatalf("timed out waiting for task %s to read %q, it reads %q", taskID, status, task.Status)
		case <-ticker.C:
		}
	}
}

// TestAutoSpawn_ClaimsTheTaskItDispatches is the defect in go-quicbaas#966:
// the engine's own dispatch left the task PENDING for the whole life of the
// job it started, so every reader of Task.Status — the API, the task list, the
// label reconciler's claim sweep, anything reasoning from StatusChangedAt —
// saw a running delivery as queued, and a healthy fourteen-minute run was
// indistinguishable from a stranded binding.
//
// A job the engine dispatches itself claims the task the way an agent's
// ClaimTask does, so the record says in_progress from the moment the attempt is
// claimed until the moment it concludes.
func TestAutoSpawn_ClaimsTheTaskItDispatches(t *testing.T) {
	taskStore := NewInMemoryTaskStore()
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(taskStore)
	ctx := context.Background()

	ran := make(chan struct{}, 1)
	release := make(chan struct{})
	runner := &fakeRunner{
		name:   "auto-spawn-claims-test",
		states: []JobStateSpec{{State: JobStateInitializing, Label: "Initializing"}},
		run: func(_ context.Context, _ *JobRun) error {
			ran <- struct{}{}
			<-release
			return nil
		},
	}
	registerJobRunnerForTest(t, runner)
	defer cancelAllJobs(impl)

	task, createErr := svc.CreateTask(ctx, &CreateTaskRequest{
		TenantID: "tenant-1", Subject: "claim what you dispatch",
		AutoSpawn: true, Runner: runner.Name(),
	})
	if createErr != nil {
		t.Fatalf("CreateTask: %v", createErr)
	}
	if task.SpawnedJobID == "" || task.Attempts != 1 {
		t.Fatalf("the dispatch must be recorded on the task: %+v", task)
	}

	// THE CALLER'S COPY IS THE PERSISTED ONE. The claim refreshes it from the
	// record the store accepted, so a create response cannot say pending about
	// a task the store holds as running.
	if task.Status != TaskStatusInProgress {
		t.Fatalf("an auto-spawned task is claimed by the job it dispatched, got %q", task.Status)
	}

	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-ran:
	case <-timer.C:
		t.Fatal("the runner never ran")
	}

	// WHILE THE JOB RUNS. This is the whole of the fourteen minutes the issue
	// measured: the record is read here the way `orchestrator task get` and the
	// label reconciler read it, with the attempt genuinely under way.
	running, err := taskStore.GetTask(ctx, "tenant-1", task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if running.Status != TaskStatusInProgress {
		t.Fatalf("a task whose job is running reads %q, want in_progress", running.Status)
	}
	if running.SpawnedJobID != task.SpawnedJobID {
		t.Fatalf("the claim must name the dispatched job: %+v", running)
	}
	if running.ClaimedAt == 0 || running.StatusChangedAt == 0 {
		t.Fatalf("the claim is stamped when it was taken, so a stall heuristic reads the right age: %+v", running)
	}
	if running.ClaimedAt != running.SpawnedAt {
		t.Fatalf("the dispatch and the claim are one decision and share one instant: claimed=%d spawned=%d",
			running.ClaimedAt, running.SpawnedAt)
	}

	// AND completed AFTER, through the conclusion path that already existed.
	close(release)
	concluded := awaitTaskStatus(t, taskStore, "tenant-1", task.ID, TaskStatusCompleted)
	if concluded.SpawnedJobID != "" {
		t.Fatalf("a concluded attempt leaves no open job: %+v", concluded)
	}
	if history := concluded.AttemptHistory(); len(history) != 1 || history[0] != task.SpawnedJobID {
		t.Fatalf("the concluded job must be in the history, got %v", history)
	}
}
