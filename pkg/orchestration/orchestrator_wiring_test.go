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
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
)

// =============================================================================
// Mock TaskStore
// =============================================================================

type mockTaskStore struct {
	createCalls   []mockTaskStoreCall
	getCalls      []mockTaskStoreCall
	listCalls     []mockTaskStoreCall
	claimCalls    []mockTaskStoreCall
	completeCalls []mockTaskStoreCall
	mu            sync.Mutex

	createErr   error
	getTask     *Task
	getErr      error
	listTasks   []*Task
	listErr     error
	claimErr    error
	completeErr error
}

type mockTaskStoreCall struct {
	TenantID string
	TaskID   string
	AgentID  string
	Task     *Task
	Result   *TaskResult
	Filter   *TaskFilter
}

func (m *mockTaskStore) CreateTask(_ context.Context, task *Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createCalls = append(m.createCalls, mockTaskStoreCall{Task: task})
	if m.createErr != nil {
		return m.createErr
	}
	task.Version = 1
	return nil
}

func (m *mockTaskStore) GetTask(_ context.Context, tenantID, taskID string) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getCalls = append(m.getCalls, mockTaskStoreCall{TenantID: tenantID, TaskID: taskID})
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.getTask != nil {
		return m.getTask, nil
	}
	return &Task{ID: taskID, TenantID: tenantID, Subject: "test"}, nil
}

func (m *mockTaskStore) UpdateTask(_ context.Context, task *Task) error {
	return nil
}

func (m *mockTaskStore) DeleteTask(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockTaskStore) ListTasks(_ context.Context, tenantID string, filter *TaskFilter) ([]*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listCalls = append(m.listCalls, mockTaskStoreCall{TenantID: tenantID, Filter: filter})
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.listTasks, nil
}

func (m *mockTaskStore) ClaimTask(_ context.Context, tenantID, taskID, agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.claimCalls = append(m.claimCalls, mockTaskStoreCall{TenantID: tenantID, TaskID: taskID, AgentID: agentID})
	return m.claimErr
}

func (m *mockTaskStore) CompleteTask(_ context.Context, tenantID, taskID string, result *TaskResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completeCalls = append(m.completeCalls, mockTaskStoreCall{TenantID: tenantID, TaskID: taskID, Result: result})
	return m.completeErr
}

func (m *mockTaskStore) FailTask(_ context.Context, _, _, _ string) error {
	return nil
}

func (m *mockTaskStore) DeleteTenantTasks(_ context.Context, _ string) error {
	return nil
}

// Compile-time interface check.
var _ TaskStore = (*mockTaskStore)(nil)

// =============================================================================
// Mock MessagingPublisher
// =============================================================================

type mockMessagingPublisher struct {
	calls []mockMessagingCall
	mu    sync.Mutex
	err   error
}

type mockMessagingCall struct {
	Channel string
	Payload []byte
}

func (m *mockMessagingPublisher) Publish(_ context.Context, channel string, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, mockMessagingCall{Channel: channel, Payload: payload})
	return m.err
}

// Compile-time interface check.
var _ MessagingPublisher = (*mockMessagingPublisher)(nil)

// =============================================================================
// Mock ContainerRuntime
// =============================================================================

type mockContainerRuntime struct {
	runCalls            []mockContainerCall
	runInteractiveCalls []mockContainerCall
	stopCalls           []mockContainerCall
	mu                  sync.Mutex

	available          bool
	runID              string
	runErr             error
	runInteractiveProc *InteractiveProcess
	runInteractiveErr  error
	stopErr            error
	isRunning          bool
	isRunErr           error
	removeErr          error
}

type mockContainerCall struct {
	Spec        *ContainerSpec
	ContainerID string
}

func (m *mockContainerRuntime) Run(_ context.Context, spec *ContainerSpec) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runCalls = append(m.runCalls, mockContainerCall{Spec: spec})
	if m.runErr != nil {
		return "", m.runErr
	}
	return m.runID, nil
}

func (m *mockContainerRuntime) RunInteractive(_ context.Context, spec *ContainerSpec) (*InteractiveProcess, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runInteractiveCalls = append(m.runInteractiveCalls, mockContainerCall{Spec: spec})
	if m.runInteractiveErr != nil {
		return nil, m.runInteractiveErr
	}
	return m.runInteractiveProc, nil
}

func (m *mockContainerRuntime) Stop(_ context.Context, containerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalls = append(m.stopCalls, mockContainerCall{ContainerID: containerID})
	return m.stopErr
}

func (m *mockContainerRuntime) Remove(_ context.Context, _ string) error {
	return m.removeErr
}

func (m *mockContainerRuntime) Logs(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (m *mockContainerRuntime) IsRunning(_ context.Context, _ string) (bool, error) {
	return m.isRunning, m.isRunErr
}

func (m *mockContainerRuntime) IsAvailable() bool {
	return m.available
}

// Compile-time interface check.
var _ ContainerRuntime = (*mockContainerRuntime)(nil)

// =============================================================================
// Setter Tests
// =============================================================================

func TestOrchestrator_SetTaskStore(t *testing.T) {
	t.Parallel()

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	store := &mockTaskStore{}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)

	if impl.taskStore != store {
		t.Fatal("expected task store to be set")
	}
}

func TestOrchestrator_SetChannelServer(t *testing.T) {
	t.Parallel()

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	var buf bytes.Buffer
	cs := NewChannelServer(ChannelServerConfig{ServerName: "test"}, bytes.NewReader(nil), &buf, &agentTestLogger{})
	impl := svc.(*agentServiceImpl)
	impl.SetChannelServer(cs)

	if impl.channelServer != cs {
		t.Fatal("expected channel server to be set")
	}
}

func TestOrchestrator_SetContainerRuntime(t *testing.T) {
	t.Parallel()

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	rt := &mockContainerRuntime{available: true}
	impl := svc.(*agentServiceImpl)
	impl.SetContainerRuntime(rt)

	if impl.containerRuntime != rt {
		t.Fatal("expected container runtime to be set")
	}
}

func TestOrchestrator_SetMessaging(t *testing.T) {
	t.Parallel()

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	pub := &mockMessagingPublisher{}
	impl := svc.(*agentServiceImpl)
	impl.SetMessaging(pub)

	if impl.messaging != pub {
		t.Fatal("expected messaging publisher to be set")
	}
}

func TestOrchestrator_SetLeadAgent(t *testing.T) {
	t.Parallel()

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	config := &LeadAgentConfig{
		Backend:      "claude",
		Model:        "opus-4",
		SystemPrompt: "You are a lead agent.",
	}
	impl := svc.(*agentServiceImpl)
	impl.SetLeadAgent(config)

	if impl.leadAgent != config {
		t.Fatal("expected lead agent config to be set")
	}
	if impl.leadAgent.Backend != "claude" {
		t.Errorf("expected backend 'claude', got %q", impl.leadAgent.Backend)
	}
	if impl.leadAgent.Model != "opus-4" {
		t.Errorf("expected model 'opus-4', got %q", impl.leadAgent.Model)
	}
}

func TestOrchestrator_SetLeadAgent_Nil(t *testing.T) {
	t.Parallel()

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	impl := svc.(*agentServiceImpl)
	impl.SetLeadAgent(nil)

	if impl.leadAgent != nil {
		t.Fatal("expected lead agent config to be nil")
	}
}

// =============================================================================
// CreateTask Tests
// =============================================================================

func TestOrchestrator_CreateTask_DelegatesToStore(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)

	task, err := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:    "tenant-1",
		TeamName:    "backend-team",
		Subject:     "Build API endpoint",
		Description: "Create the REST handler for /api/v1/tasks",
		Metadata:    map[string]string{"priority": "high"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if task.ID == "" {
		t.Fatal("expected non-empty task ID")
	}
	if task.TenantID != "tenant-1" {
		t.Errorf("expected tenant 'tenant-1', got %q", task.TenantID)
	}
	if task.Subject != "Build API endpoint" {
		t.Errorf("expected subject 'Build API endpoint', got %q", task.Subject)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.createCalls) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(store.createCalls))
	}
}

func TestOrchestrator_CreateTask_NoTaskStore(t *testing.T) {
	t.Parallel()

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, createErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID: "tenant-1",
		Subject:  "test",
	})
	if createErr == nil {
		t.Fatal("expected error when no task store configured")
	}
	if !errors.Is(createErr, ErrTaskStoreRequired) {
		t.Fatalf("expected ErrTaskStoreRequired, got %v", createErr)
	}
}

func TestOrchestrator_CreateTask_EmptyTenantID(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)

	_, createErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID: "",
		Subject:  "test",
	})
	if createErr == nil {
		t.Fatal("expected error for empty tenant ID")
	}
	if !errors.Is(createErr, ErrTenantIDRequired) {
		t.Fatalf("expected ErrTenantIDRequired, got %v", createErr)
	}
}

func TestOrchestrator_CreateTask_EmptySubject(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)

	_, createErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID: "tenant-1",
		Subject:  "",
	})
	if createErr == nil {
		t.Fatal("expected error for empty subject")
	}
	if !errors.Is(createErr, ErrTaskSubjectRequired) {
		t.Fatalf("expected ErrTaskSubjectRequired, got %v", createErr)
	}
}

func TestOrchestrator_CreateTask_PublishesToMessaging(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	pub := &mockMessagingPublisher{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)
	impl.SetMessaging(pub)

	_, createErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID: "tenant-1",
		Subject:  "test task",
	})
	if createErr != nil {
		t.Fatalf("expected no error, got %v", createErr)
	}

	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 messaging call, got %d", len(pub.calls))
	}
	if pub.calls[0].Channel != "orchestrator:events:tenant-1" {
		t.Errorf("expected channel 'orchestrator:events:tenant-1', got %q", pub.calls[0].Channel)
	}
}

func TestOrchestrator_CreateTask_PushesToChannel(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}

	// Create a channel server with buffered writer
	var buf bytes.Buffer
	cs := NewChannelServer(ChannelServerConfig{ServerName: "test"}, bytes.NewReader(nil), &buf, &agentTestLogger{})
	// Mark initialized so events are accepted
	cs.initialized.Store(true)
	cs.started.Store(true)

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)
	impl.SetChannelServer(cs)

	_, createErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID: "tenant-1",
		Subject:  "test channel task",
	})
	if createErr != nil {
		t.Fatalf("expected no error, got %v", createErr)
	}

	// The event should have been pushed to the channel (buffered, not yet drained)
	// Verify the event was queued by checking the buffer length
	if len(cs.eventCh) == 0 {
		t.Error("expected at least 1 event in channel server buffer")
	}
}

func TestOrchestrator_CreateTask_StoreError(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{
		createErr: &TaskAlreadyExistsError{TaskID: "dup"},
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)

	_, createErr := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID: "tenant-1",
		Subject:  "test",
	})
	if createErr == nil {
		t.Fatal("expected error from store")
	}
	if !errors.Is(createErr, ErrTaskAlreadyExists) {
		t.Fatalf("expected ErrTaskAlreadyExists, got %v", createErr)
	}
}

// =============================================================================
// GetTask Tests
// =============================================================================

func TestOrchestrator_GetTask(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{
		getTask: &Task{ID: "task-1", TenantID: "tenant-1", Subject: "found"},
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)

	task, getErr := svc.GetTask(context.Background(), "tenant-1", "task-1")
	if getErr != nil {
		t.Fatalf("expected no error, got %v", getErr)
	}
	if task.Subject != "found" {
		t.Errorf("expected subject 'found', got %q", task.Subject)
	}
}

func TestOrchestrator_GetTask_EmptyTenantID(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)

	_, getErr := svc.GetTask(context.Background(), "", "task-1")
	if getErr == nil {
		t.Fatal("expected error for empty tenant ID")
	}
	if !errors.Is(getErr, ErrTenantIDRequired) {
		t.Fatalf("expected ErrTenantIDRequired, got %v", getErr)
	}
}

func TestOrchestrator_GetTask_EmptyTaskID(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)

	_, getErr := svc.GetTask(context.Background(), "tenant-1", "")
	if getErr == nil {
		t.Fatal("expected error for empty task ID")
	}
	if !errors.Is(getErr, ErrTaskIDRequired) {
		t.Fatalf("expected ErrTaskIDRequired, got %v", getErr)
	}
}

// =============================================================================
// ListTasks Tests
// =============================================================================

func TestOrchestrator_ListTasks(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{
		listTasks: []*Task{
			{ID: "task-1", TenantID: "tenant-1"},
			{ID: "task-2", TenantID: "tenant-1"},
		},
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)

	tasks, listErr := svc.ListTasks(context.Background(), "tenant-1", nil)
	if listErr != nil {
		t.Fatalf("expected no error, got %v", listErr)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestOrchestrator_ListTasks_EmptyTenantID(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)

	_, listErr := svc.ListTasks(context.Background(), "", nil)
	if listErr == nil {
		t.Fatal("expected error for empty tenant ID")
	}
}

// =============================================================================
// ClaimTask Tests
// =============================================================================

func TestOrchestrator_ClaimTask_DelegatesToStore(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)

	claimErr := svc.ClaimTask(context.Background(), "tenant-1", "task-1", "agent-1")
	if claimErr != nil {
		t.Fatalf("expected no error, got %v", claimErr)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.claimCalls) != 1 {
		t.Fatalf("expected 1 claim call, got %d", len(store.claimCalls))
	}
	if store.claimCalls[0].TenantID != "tenant-1" {
		t.Errorf("expected tenant 'tenant-1', got %q", store.claimCalls[0].TenantID)
	}
	if store.claimCalls[0].TaskID != "task-1" {
		t.Errorf("expected task 'task-1', got %q", store.claimCalls[0].TaskID)
	}
	if store.claimCalls[0].AgentID != "agent-1" {
		t.Errorf("expected agent 'agent-1', got %q", store.claimCalls[0].AgentID)
	}
}

func TestOrchestrator_ClaimTask_TenantIsolation(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)

	// Empty tenant ID should fail
	claimErr := svc.ClaimTask(context.Background(), "", "task-1", "agent-1")
	if claimErr == nil {
		t.Fatal("expected error for empty tenant ID")
	}
	if !errors.Is(claimErr, ErrTenantIDRequired) {
		t.Fatalf("expected ErrTenantIDRequired, got %v", claimErr)
	}

	// Empty task ID should fail
	claimErr = svc.ClaimTask(context.Background(), "tenant-1", "", "agent-1")
	if claimErr == nil {
		t.Fatal("expected error for empty task ID")
	}
	if !errors.Is(claimErr, ErrTaskIDRequired) {
		t.Fatalf("expected ErrTaskIDRequired, got %v", claimErr)
	}

	// Empty agent ID should fail
	claimErr = svc.ClaimTask(context.Background(), "tenant-1", "task-1", "")
	if claimErr == nil {
		t.Fatal("expected error for empty agent ID")
	}
	if !errors.Is(claimErr, ErrAgentIDRequired) {
		t.Fatalf("expected ErrAgentIDRequired, got %v", claimErr)
	}
}

func TestOrchestrator_ClaimTask_PublishesEvent(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	pub := &mockMessagingPublisher{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)
	impl.SetMessaging(pub)

	claimErr := svc.ClaimTask(context.Background(), "tenant-1", "task-1", "agent-1")
	if claimErr != nil {
		t.Fatalf("expected no error, got %v", claimErr)
	}

	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 messaging call, got %d", len(pub.calls))
	}
	if pub.calls[0].Channel != "orchestrator:events:tenant-1" {
		t.Errorf("expected channel 'orchestrator:events:tenant-1', got %q", pub.calls[0].Channel)
	}
}

// =============================================================================
// CompleteTask Tests
// =============================================================================

func TestOrchestrator_CompleteTask_PublishesEvent(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	pub := &mockMessagingPublisher{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)
	impl.SetMessaging(pub)

	result := &TaskResult{Output: "done"}
	completeErr := svc.CompleteTask(context.Background(), "tenant-1", "task-1", result)
	if completeErr != nil {
		t.Fatalf("expected no error, got %v", completeErr)
	}

	// Verify store was called
	store.mu.Lock()
	if len(store.completeCalls) != 1 {
		t.Fatalf("expected 1 complete call, got %d", len(store.completeCalls))
	}
	if store.completeCalls[0].TaskID != "task-1" {
		t.Errorf("expected task 'task-1', got %q", store.completeCalls[0].TaskID)
	}
	store.mu.Unlock()

	// Verify messaging was called
	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 messaging call, got %d", len(pub.calls))
	}
}

func TestOrchestrator_CompleteTask_EmptyTenantID(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)

	completeErr := svc.CompleteTask(context.Background(), "", "task-1", nil)
	if completeErr == nil {
		t.Fatal("expected error for empty tenant ID")
	}
	if !errors.Is(completeErr, ErrTenantIDRequired) {
		t.Fatalf("expected ErrTenantIDRequired, got %v", completeErr)
	}
}

func TestOrchestrator_CompleteTask_EmptyTaskID(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)

	completeErr := svc.CompleteTask(context.Background(), "tenant-1", "", nil)
	if completeErr == nil {
		t.Fatal("expected error for empty task ID")
	}
	if !errors.Is(completeErr, ErrTaskIDRequired) {
		t.Fatalf("expected ErrTaskIDRequired, got %v", completeErr)
	}
}

func TestOrchestrator_CompleteTask_StoreError(t *testing.T) {
	t.Parallel()

	store := &mockTaskStore{
		completeErr: &TaskInvalidStateError{TaskID: "task-1", CurrentState: TaskStatusPending, TargetState: TaskStatusCompleted},
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetTaskStore(store)

	completeErr := svc.CompleteTask(context.Background(), "tenant-1", "task-1", nil)
	if completeErr == nil {
		t.Fatal("expected error from store")
	}
	if !errors.Is(completeErr, ErrTaskInvalidState) {
		t.Fatalf("expected ErrTaskInvalidState, got %v", completeErr)
	}
}

// =============================================================================
// SpawnSession Sandboxed Tests
// =============================================================================

func TestOrchestrator_SpawnSession_Sandboxed(t *testing.T) {
	t.Parallel()

	rt := &mockContainerRuntime{
		available: true,
		runID:     "container-abc123",
	}
	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{},
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetContainerRuntime(rt)

	resp, spawnErr := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:        "mock",
		Sandboxed:      true,
		ContainerImage: "quicbaas/agent:latest",
		Env:            map[string]string{"API_KEY": "test-key"},
		WorkDir:        "/workspace",
	})
	if spawnErr != nil {
		t.Fatalf("expected no error, got %v", spawnErr)
	}

	if resp.ContainerID != "container-abc123" {
		t.Errorf("expected container ID 'container-abc123', got %q", resp.ContainerID)
	}
	if resp.SessionID == "" {
		t.Fatal("expected non-empty session ID")
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.runCalls) != 1 {
		t.Fatalf("expected 1 container run call, got %d", len(rt.runCalls))
	}
	if rt.runCalls[0].Spec.Image != "quicbaas/agent:latest" {
		t.Errorf("expected image 'quicbaas/agent:latest', got %q", rt.runCalls[0].Spec.Image)
	}
}

func TestOrchestrator_SpawnSession_Sandboxed_NoImage(t *testing.T) {
	t.Parallel()

	rt := &mockContainerRuntime{available: true}
	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{},
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetContainerRuntime(rt)

	_, spawnErr := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "mock",
		Sandboxed: true,
		// No ContainerImage or ContainerConfig
	})
	if spawnErr == nil {
		t.Fatal("expected error for sandboxed session without image")
	}
	if !errors.Is(spawnErr, ErrContainerImageRequired) {
		t.Fatalf("expected ErrContainerImageRequired, got %v", spawnErr)
	}
}

func TestOrchestrator_SpawnSession_Sandboxed_WithContainerConfig(t *testing.T) {
	t.Parallel()

	rt := &mockContainerRuntime{
		available: true,
		runID:     "container-custom",
	}
	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{},
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetContainerRuntime(rt)

	resp, spawnErr := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "mock",
		Sandboxed: true,
		ContainerConfig: &ContainerSpec{
			Image:   "custom/image:v2",
			Network: "host",
			Env:     map[string]string{"CUSTOM": "value"},
		},
	})
	if spawnErr != nil {
		t.Fatalf("expected no error, got %v", spawnErr)
	}
	if resp.ContainerID != "container-custom" {
		t.Errorf("expected container ID 'container-custom', got %q", resp.ContainerID)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.runCalls[0].Spec.Image != "custom/image:v2" {
		t.Errorf("expected custom image, got %q", rt.runCalls[0].Spec.Image)
	}
	if rt.runCalls[0].Spec.Network != "host" {
		t.Errorf("expected host network, got %q", rt.runCalls[0].Spec.Network)
	}
}

func TestOrchestrator_SpawnSession_Sandboxed_RuntimeUnavailable(t *testing.T) {
	t.Parallel()

	// Runtime is not available - should fall back to standard spawn
	rt := &mockContainerRuntime{available: false}
	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{},
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetContainerRuntime(rt)

	resp, spawnErr := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:        "mock",
		Sandboxed:      true,
		ContainerImage: "quicbaas/agent:latest",
	})
	if spawnErr != nil {
		t.Fatalf("expected no error (should fall back), got %v", spawnErr)
	}
	if resp.ContainerID != "" {
		t.Errorf("expected empty container ID (standard spawn), got %q", resp.ContainerID)
	}
}

func TestOrchestrator_SpawnSession_NonSandboxed(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{},
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	resp, spawnErr := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "mock",
		Sandboxed: false,
	})
	if spawnErr != nil {
		t.Fatalf("expected no error, got %v", spawnErr)
	}
	if resp.ContainerID != "" {
		t.Errorf("expected empty container ID, got %q", resp.ContainerID)
	}
	if resp.SessionID == "" {
		t.Fatal("expected non-empty session ID")
	}
}

func TestOrchestrator_SpawnSession_WithEnv(t *testing.T) {
	t.Parallel()

	var capturedReq *SpawnRequest
	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{
			spawnFn: func(_ context.Context, req *SpawnRequest) (*Session, error) {
				capturedReq = req
				return &Session{
					ID:        "session-env-test",
					ProjectID: req.ProjectID,
					Backend:   "mock",
					Model:     req.Model,
					Status:    SessionStateActive,
					CreatedAt: 1000,
				}, nil
			},
		},
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	_, spawnErr := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend: "mock",
		Env:     map[string]string{"API_KEY": "secret", "DEBUG": "true"},
		WorkDir: "/custom/workspace",
	})
	if spawnErr != nil {
		t.Fatalf("expected no error, got %v", spawnErr)
	}

	if capturedReq == nil {
		t.Fatal("expected spawn request to be captured")
	}
	if capturedReq.Env["API_KEY"] != "secret" {
		t.Errorf("expected API_KEY='secret', got %q", capturedReq.Env["API_KEY"])
	}
	if capturedReq.Env["DEBUG"] != "true" {
		t.Errorf("expected DEBUG='true', got %q", capturedReq.Env["DEBUG"])
	}
	if capturedReq.WorkDir != "/custom/workspace" {
		t.Errorf("expected WorkDir '/custom/workspace', got %q", capturedReq.WorkDir)
	}
}

// =============================================================================
// SpawnSession Event Publishing Tests
// =============================================================================

func TestOrchestrator_SpawnSession_PublishesEvent(t *testing.T) {
	t.Parallel()

	pub := &mockMessagingPublisher{}
	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{},
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetMessaging(pub)

	_, spawnErr := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend: "mock",
	})
	if spawnErr != nil {
		t.Fatalf("expected no error, got %v", spawnErr)
	}

	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 messaging call, got %d", len(pub.calls))
	}
	if pub.calls[0].Channel != "orchestrator:events" {
		t.Errorf("expected channel 'orchestrator:events', got %q", pub.calls[0].Channel)
	}
}

// =============================================================================
// CloseSession Container Cleanup Tests
// =============================================================================

func TestOrchestrator_CloseSession_StopsContainer(t *testing.T) {
	t.Parallel()

	rt := &mockContainerRuntime{
		available: true,
		runID:     "container-to-stop",
	}
	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{},
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetContainerRuntime(rt)

	// Spawn a sandboxed session
	spawnResp, spawnErr := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:        "mock",
		Sandboxed:      true,
		ContainerImage: "quicbaas/agent:latest",
	})
	if spawnErr != nil {
		t.Fatalf("failed to spawn: %v", spawnErr)
	}

	// Close the session - should also stop the container
	_, closeErr := svc.CloseSession(context.Background(), &CloseSessionRequest{
		SessionID: spawnResp.SessionID,
	})
	if closeErr != nil {
		t.Fatalf("expected no error, got %v", closeErr)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.stopCalls) != 1 {
		t.Fatalf("expected 1 container stop call, got %d", len(rt.stopCalls))
	}
	if rt.stopCalls[0].ContainerID != "container-to-stop" {
		t.Errorf("expected container ID 'container-to-stop', got %q", rt.stopCalls[0].ContainerID)
	}
}
