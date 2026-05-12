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
	"testing"
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
	var nmae *NoMatchingAgentError
	if !errors.As(spawnErr, &nmae) {
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
	var nmae *NoMatchingAgentError
	if errors.As(spawnErr, &nmae) {
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
		ID:       "agent-offline",
		TenantID: "tenant-1",
		Name:     "worker-1",
		Host:     "host-1",
		Backend:  "ollama",
		AgentType: "worker",
		Status:   AgentStatusOffline,
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
