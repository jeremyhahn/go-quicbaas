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
	"os"
	"path/filepath"
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
	blockCalls    []mockTaskStoreCall
	readmitCalls  []mockTaskStoreCall
	deleteCalls   []mockTaskStoreCall
	bumpCalls     []mockTaskStoreCall
	mu            sync.Mutex

	createErr   error
	getTask     *Task
	getErr      error
	listTasks   []*Task
	listErr     error
	claimErr    error
	completeErr error
	blockErr    error
	readmitErr  error
	deleteErr   error
	bumpBonus   int
	bumpErr     error
}

type mockTaskStoreCall struct {
	TenantID  string
	TaskID    string
	AgentID   string
	Task      *Task
	Result    *TaskResult
	Filter    *TaskFilter
	BlockKind TaskBlockKind
	Reason    string
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

func (m *mockTaskStore) DeleteTask(_ context.Context, tenantID, taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteCalls = append(m.deleteCalls, mockTaskStoreCall{TenantID: tenantID, TaskID: taskID})
	return m.deleteErr
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

func (m *mockTaskStore) ListTaskRecords(ctx context.Context, tenantID string, filter *TaskFilter) (*TaskListing, error) {
	tasks, err := m.ListTasks(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	return &TaskListing{Tasks: tasks}, nil
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

func (m *mockTaskStore) BlockTask(_ context.Context, tenantID, taskID string, kind TaskBlockKind, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blockCalls = append(m.blockCalls, mockTaskStoreCall{TenantID: tenantID, TaskID: taskID, BlockKind: kind, Reason: reason})
	return m.blockErr
}

func (m *mockTaskStore) ReadmitTask(_ context.Context, tenantID, taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readmitCalls = append(m.readmitCalls, mockTaskStoreCall{TenantID: tenantID, TaskID: taskID})
	return m.readmitErr
}

func (*mockTaskStore) CloseBlockedTask(_ context.Context, _, _ string, _ *TaskResult) error {
	return nil
}

func (m *mockTaskStore) BumpReviewRoundBudget(_ context.Context, tenantID, taskID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bumpCalls = append(m.bumpCalls, mockTaskStoreCall{TenantID: tenantID, TaskID: taskID})
	return m.bumpBonus, m.bumpErr
}

func (m *mockTaskStore) FailTask(_ context.Context, _, _, _ string) error {
	return nil
}

func (m *mockTaskStore) SubmitTaskArtifacts(_ context.Context, _, _ string, _ []Artifact) error {
	return nil
}

func (m *mockTaskStore) GetTaskArtifact(_ context.Context, _, _, _ string) (*Artifact, error) {
	return nil, nil
}

func (m *mockTaskStore) ListTaskArtifacts(_ context.Context, _, _ string) ([]Artifact, error) {
	return nil, nil
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
	pullCalls           []mockContainerCall
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
	pullErr            error
	execCalls          []mockExecCall
	execResult         *ExecResult
	execErr            error
}

type mockContainerCall struct {
	Spec        *ContainerSpec
	ContainerID string
}

// mockExecCall records one Exec: which container, and the command it was
// asked to run there.
type mockExecCall struct {
	ContainerID string
	Spec        *ExecSpec
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

// Exec records the command and answers what the test scripted. The zero
// value answers a clean exit with no output, which is what a caller that only
// wanted the command to run sees.
func (m *mockContainerRuntime) Exec(_ context.Context, containerID string, spec *ExecSpec) (*ExecResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.execCalls = append(m.execCalls, mockExecCall{ContainerID: containerID, Spec: spec})
	if m.execErr != nil {
		return nil, m.execErr
	}
	if m.execResult != nil {
		return m.execResult, nil
	}
	return &ExecResult{}, nil
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

func (m *mockContainerRuntime) Pull(_ context.Context, image string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pullCalls = append(m.pullCalls, mockContainerCall{Spec: &ContainerSpec{Image: image}})
	return m.pullErr
}

// Compile-time interface check.
var _ ContainerRuntime = (*mockContainerRuntime)(nil)

// =============================================================================
// Setter Tests
// =============================================================================

func TestOrchestrator_SetLeadAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config *LeadAgentConfig
	}{
		{
			name: "config",
			config: &LeadAgentConfig{
				Agent:   AgentRefTo("claude-lead"),
				Backend: "claude",
				Model:   "opus-4",
			},
		},
		{name: "nil clears the lead agent", config: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
			if err != nil {
				t.Fatalf("failed to create service: %v", err)
			}

			impl := svc.(*agentServiceImpl)
			impl.SetLeadAgent(tt.config)

			if impl.leadAgent != tt.config {
				t.Fatalf("expected lead agent %v, got %v", tt.config, impl.leadAgent)
			}
		})
	}
}

// =============================================================================
// CreateTask Tests
// =============================================================================

func TestOrchestrator_CreateTask_DelegatesToStore(t *testing.T) {
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

	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 messaging call, got %d", len(pub.calls))
	}
	if pub.calls[0].Channel != "orchestrator:events:tenant-1" {
		t.Errorf("expected channel 'orchestrator:events:tenant-1', got %q", pub.calls[0].Channel)
	}
}

func TestOrchestrator_TaskMethods_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// store is the task store wired into the service; nil leaves it unset.
		store   *mockTaskStore
		call    func(svc AgentService) error
		wantErr error
	}{
		{
			name: "CreateTask without task store",
			call: func(svc AgentService) error {
				_, err := svc.CreateTask(context.Background(), &CreateTaskRequest{TenantID: "tenant-1", Subject: "test"})
				return err
			},
			wantErr: ErrTaskStoreRequired,
		},
		{
			name:  "CreateTask store error",
			store: &mockTaskStore{createErr: &TaskAlreadyExistsError{TaskID: "dup"}},
			call: func(svc AgentService) error {
				_, err := svc.CreateTask(context.Background(), &CreateTaskRequest{TenantID: "tenant-1", Subject: "test"})
				return err
			},
			wantErr: ErrTaskAlreadyExists,
		},
		{
			name: "CompleteTask store error",
			store: &mockTaskStore{
				completeErr: &TaskInvalidStateError{TaskID: "task-1", CurrentState: TaskStatusPending, TargetState: TaskStatusCompleted},
			},
			call: func(svc AgentService) error {
				return svc.CompleteTask(context.Background(), "tenant-1", "task-1", nil)
			},
			wantErr: ErrTaskInvalidState,
		},
		{
			name:  "CreateTask empty tenant ID",
			store: &mockTaskStore{},
			call: func(svc AgentService) error {
				_, err := svc.CreateTask(context.Background(), &CreateTaskRequest{TenantID: "", Subject: "test"})
				return err
			},
			wantErr: ErrTenantIDRequired,
		},
		{
			name:  "CreateTask empty subject",
			store: &mockTaskStore{},
			call: func(svc AgentService) error {
				_, err := svc.CreateTask(context.Background(), &CreateTaskRequest{TenantID: "tenant-1", Subject: ""})
				return err
			},
			wantErr: ErrTaskSubjectRequired,
		},
		{
			name:  "GetTask empty tenant ID",
			store: &mockTaskStore{},
			call: func(svc AgentService) error {
				_, err := svc.GetTask(context.Background(), "", "task-1")
				return err
			},
			wantErr: ErrTenantIDRequired,
		},
		{
			name:  "GetTask empty task ID",
			store: &mockTaskStore{},
			call: func(svc AgentService) error {
				_, err := svc.GetTask(context.Background(), "tenant-1", "")
				return err
			},
			wantErr: ErrTaskIDRequired,
		},
		{
			name:  "ListTasks empty tenant ID",
			store: &mockTaskStore{},
			call: func(svc AgentService) error {
				_, err := svc.ListTasks(context.Background(), "", nil)
				return err
			},
			wantErr: ErrTenantIDRequired,
		},
		{
			name:  "CompleteTask empty tenant ID",
			store: &mockTaskStore{},
			call: func(svc AgentService) error {
				return svc.CompleteTask(context.Background(), "", "task-1", nil)
			},
			wantErr: ErrTenantIDRequired,
		},
		{
			name:  "CompleteTask empty task ID",
			store: &mockTaskStore{},
			call: func(svc AgentService) error {
				return svc.CompleteTask(context.Background(), "tenant-1", "", nil)
			},
			wantErr: ErrTaskIDRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
			if err != nil {
				t.Fatalf("failed to create service: %v", err)
			}
			if tt.store != nil {
				svc.(*agentServiceImpl).SetTaskStore(tt.store)
			}

			if callErr := tt.call(svc); !errors.Is(callErr, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, callErr)
			}
		})
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

// =============================================================================
// ClaimTask Tests
// =============================================================================

func TestOrchestrator_ClaimTask_DelegatesToStore(t *testing.T) {
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

	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 messaging call, got %d", len(pub.calls))
	}
	if pub.calls[0].Channel != "orchestrator:events:tenant-1" {
		t.Errorf("expected channel 'orchestrator:events:tenant-1', got %q", pub.calls[0].Channel)
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

	// A containerized spawn bind-mounts its work directory into the container,
	// so the directory has to be one the mount policy authorizes for this
	// tenant — here, its own managed workspace under the projects root. See
	// spawn_mount_policy_test.go for the denial cases.
	projectsRoot := filepath.Join(t.TempDir(), "projects")
	workspace := filepath.Join(projectsRoot, SystemTenantID, "proj-1", workspaceDirName)
	if mkErr := os.MkdirAll(workspace, 0o700); mkErr != nil {
		t.Fatalf("MkdirAll: %v", mkErr)
	}
	impl.SetMountPolicy(&MountPolicy{ProjectsRoot: projectsRoot})

	resp, spawnErr := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:        "mock",
		Sandboxed:      true,
		ContainerImage: "quicbaas/agent:latest",
		Env:            map[string]string{"API_KEY": "test-key"},
		WorkDir:        workspace,
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
			Image: "custom/image:v2",
			Env:   map[string]string{"CUSTOM": "value"},
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
	if rt.runCalls[0].Spec.Env["CUSTOM"] != "value" {
		t.Errorf("expected the spec's environment to survive, got %v", rt.runCalls[0].Spec.Env)
	}
	// The network is operator configuration, not a request field: a
	// caller-supplied spec runs in the isolated default, and a spec naming
	// "host" fails the spawn outright (see
	// TestSpawnSession_ContainerConfigHostNetwork_Refused).
	if got := rt.runCalls[0].Spec.Network; got != SandboxNetworkDefault.String() {
		t.Errorf("expected the operator default network %q, got %q", SandboxNetworkDefault, got)
	}
}

func TestOrchestrator_SpawnSession_Sandboxed_RuntimeUnavailable(t *testing.T) {
	t.Parallel()

	// Under sandbox-first semantics (Phase 1), an explicit sandboxed=true
	// request (SandboxModeRequired) MUST be rejected when no container
	// runtime is available. Legacy silent fallback to raw exec is removed
	// by design — callers wanting fallback should use SandboxModeDefault
	// under SandboxPolicyPreferred.
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

	_, spawnErr := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:        "mock",
		Sandboxed:      true,
		ContainerImage: "quicbaas/agent:latest",
	})
	var violation *SandboxModeViolationError
	if !errors.As(spawnErr, &violation) {
		t.Fatalf("expected SandboxModeViolationError, got %T: %v", spawnErr, spawnErr)
	}
	if violation.Requested != SandboxModeRequired {
		t.Errorf("violation.Requested = %v, want SandboxModeRequired", violation.Requested)
	}
}

// TestOrchestrator_SpawnSession_DefaultPreferred_FallsBack verifies the
// fallback path that the legacy test above used to exercise: a caller with
// SandboxModeDefault under SandboxPolicyPreferred degrades to raw exec when
// no runtime is available. This is how operators opt into "try container,
// but don't fail if impossible" behavior.
func TestOrchestrator_SpawnSession_DefaultPreferred_FallsBack(t *testing.T) {
	t.Parallel()

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
	impl.SetSandboxPolicy(SandboxPolicyPreferred)

	resp, spawnErr := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:        "mock",
		ContainerImage: "quicbaas/agent:latest",
	})
	if spawnErr != nil {
		t.Fatalf("expected no error (preferred policy should fall back), got %v", spawnErr)
	}
	if resp.ContainerID != "" {
		t.Errorf("expected empty container ID (raw exec fallback), got %q", resp.ContainerID)
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
