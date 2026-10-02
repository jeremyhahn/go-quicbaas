//go:build rest

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

package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// =============================================================================
// Mock Task Store
// =============================================================================

// mockTaskStore implements orchestration.TaskStore for test isolation.
// Supports both map-based behavior (for hooks tests) and func-field overrides
// (for orchestrator tests).
type mockTaskStore struct {
	tasks            map[string]*orchestration.Task
	createErr        error
	completeErr      error
	listErr          error
	listResult       []*orchestration.Task
	createTaskFunc   func(ctx context.Context, task *orchestration.Task) error
	getTaskFunc      func(ctx context.Context, tenantID, taskID string) (*orchestration.Task, error)
	updateTaskFunc   func(ctx context.Context, task *orchestration.Task) error
	deleteTaskFunc   func(ctx context.Context, tenantID, taskID string) error
	listTasksFunc    func(ctx context.Context, tenantID string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error)
	claimTaskFunc    func(ctx context.Context, tenantID, taskID, agentID string) error
	completeTaskFunc func(ctx context.Context, tenantID, taskID string, result *orchestration.TaskResult) error
	failTaskFunc     func(ctx context.Context, tenantID, taskID, reason string) error
	deleteTenantFunc func(ctx context.Context, tenantID string) error
}

func newMockTaskStore() *mockTaskStore {
	return &mockTaskStore{
		tasks: make(map[string]*orchestration.Task),
	}
}

func (m *mockTaskStore) CreateTask(ctx context.Context, task *orchestration.Task) error {
	if m.createTaskFunc != nil {
		return m.createTaskFunc(ctx, task)
	}
	if m.createErr != nil {
		return m.createErr
	}
	if m.tasks != nil {
		key := task.TenantID + "/" + task.ID
		if _, exists := m.tasks[key]; exists {
			return &orchestration.TaskAlreadyExistsError{TaskID: task.ID}
		}
		m.tasks[key] = task
	}
	return nil
}

func (m *mockTaskStore) GetTask(ctx context.Context, tenantID, taskID string) (*orchestration.Task, error) {
	if m.getTaskFunc != nil {
		return m.getTaskFunc(ctx, tenantID, taskID)
	}
	if m.tasks != nil {
		key := tenantID + "/" + taskID
		task, ok := m.tasks[key]
		if !ok {
			return nil, &orchestration.TaskNotFoundError{TenantID: tenantID, TaskID: taskID}
		}
		return task, nil
	}
	return &orchestration.Task{ID: taskID, TenantID: tenantID, Subject: "test"}, nil
}

func (m *mockTaskStore) UpdateTask(ctx context.Context, task *orchestration.Task) error {
	if m.updateTaskFunc != nil {
		return m.updateTaskFunc(ctx, task)
	}
	if m.tasks != nil {
		key := task.TenantID + "/" + task.ID
		if _, ok := m.tasks[key]; !ok {
			return &orchestration.TaskNotFoundError{TenantID: task.TenantID, TaskID: task.ID}
		}
		m.tasks[key] = task
	}
	return nil
}

func (m *mockTaskStore) DeleteTask(ctx context.Context, tenantID, taskID string) error {
	if m.deleteTaskFunc != nil {
		return m.deleteTaskFunc(ctx, tenantID, taskID)
	}
	if m.tasks != nil {
		delete(m.tasks, tenantID+"/"+taskID)
	}
	return nil
}

func (m *mockTaskStore) ListTasks(ctx context.Context, tenantID string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error) {
	if m.listTasksFunc != nil {
		return m.listTasksFunc(ctx, tenantID, filter)
	}
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.listResult != nil {
		return m.listResult, nil
	}
	return []*orchestration.Task{}, nil
}

func (m *mockTaskStore) ListTaskRecords(ctx context.Context, tenantID string, filter *orchestration.TaskFilter) (*orchestration.TaskListing, error) {
	tasks, err := m.ListTasks(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	return &orchestration.TaskListing{Tasks: tasks}, nil
}

func (m *mockTaskStore) ClaimTask(ctx context.Context, tenantID, taskID, agentID string) error {
	if m.claimTaskFunc != nil {
		return m.claimTaskFunc(ctx, tenantID, taskID, agentID)
	}
	return nil
}

func (m *mockTaskStore) CompleteTask(ctx context.Context, tenantID, taskID string, result *orchestration.TaskResult) error {
	if m.completeTaskFunc != nil {
		return m.completeTaskFunc(ctx, tenantID, taskID, result)
	}
	if m.completeErr != nil {
		return m.completeErr
	}
	if m.tasks != nil {
		key := tenantID + "/" + taskID
		task, ok := m.tasks[key]
		if !ok {
			return &orchestration.TaskNotFoundError{TenantID: tenantID, TaskID: taskID}
		}
		task.Status = orchestration.TaskStatusCompleted
	}
	return nil
}

func (m *mockTaskStore) FailTask(ctx context.Context, tenantID, taskID, reason string) error {
	if m.failTaskFunc != nil {
		return m.failTaskFunc(ctx, tenantID, taskID, reason)
	}
	return nil
}

func (m *mockTaskStore) BlockTask(ctx context.Context, tenantID, taskID string, kind orchestration.TaskBlockKind, reason string) error {
	return nil
}

func (m *mockTaskStore) ReadmitTask(ctx context.Context, tenantID, taskID string) error {
	return nil
}

func (*mockTaskStore) CloseBlockedTask(_ context.Context, _, _ string, _ *orchestration.TaskResult) error {
	return nil
}

func (m *mockTaskStore) BumpReviewRoundBudget(_ context.Context, _, _ string) (int, error) {
	return 0, nil
}

func (m *mockTaskStore) DeleteTenantTasks(ctx context.Context, tenantID string) error {
	if m.deleteTenantFunc != nil {
		return m.deleteTenantFunc(ctx, tenantID)
	}
	return nil
}

func (m *mockTaskStore) SubmitTaskArtifacts(_ context.Context, _, _ string, _ []orchestration.Artifact) error {
	return nil
}

func (m *mockTaskStore) GetTaskArtifact(_ context.Context, _, _, _ string) (*orchestration.Artifact, error) {
	return nil, nil
}

func (m *mockTaskStore) ListTaskArtifacts(_ context.Context, _, _ string) ([]orchestration.Artifact, error) {
	return nil, nil
}

// =============================================================================
// Mock Messaging Service for Hooks
// =============================================================================

// hookMessagingStub implements messaging.Service for hook handler tests.
type hookMessagingStub struct {
	publishErr    error
	publishCalled bool
	lastChannel   string
}

func (s *hookMessagingStub) CreateChannel(_ context.Context, name string, _ ...messaging.CreateChannelOption) (*messaging.Channel, error) {
	return &messaging.Channel{Name: name}, nil
}

func (s *hookMessagingStub) GetChannel(_ context.Context, name string) (*messaging.Channel, error) {
	return &messaging.Channel{Name: name}, nil
}

func (s *hookMessagingStub) DeleteChannel(_ context.Context, _ string) error {
	return nil
}

func (s *hookMessagingStub) ListChannels(_ context.Context) ([]*messaging.Channel, error) {
	return []*messaging.Channel{}, nil
}

func (s *hookMessagingStub) Subscribe(_ context.Context, channel string, _ ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	return &messaging.Subscription{ID: "sub-001", Channel: channel}, nil
}

func (s *hookMessagingStub) Unsubscribe(_ context.Context, _ string) error {
	return nil
}

func (s *hookMessagingStub) Publish(_ context.Context, channel string, _ interface{}, _ ...messaging.PublishOption) (*messaging.PublishResult, error) {
	s.publishCalled = true
	s.lastChannel = channel
	if s.publishErr != nil {
		return nil, s.publishErr
	}
	return &messaging.PublishResult{MessageID: "msg-001"}, nil
}

func (s *hookMessagingStub) PublishDelta(_ context.Context, _ string, _, _ interface{}, _ ...messaging.PublishOption) (*messaging.PublishResult, error) {
	return &messaging.PublishResult{MessageID: "msg-001"}, nil
}

func (s *hookMessagingStub) WatchChannel(_ context.Context, _ string, _ ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	return &messaging.Subscription{ID: "watch-001"}, nil
}

func (s *hookMessagingStub) Start(_ context.Context) error { return nil }
func (s *hookMessagingStub) Stop(_ context.Context) error  { return nil }
func (s *hookMessagingStub) IsRunning() bool               { return true }
func (s *hookMessagingStub) GetStats() *messaging.Stats    { return &messaging.Stats{} }

// =============================================================================
// Mock QuicBaaS with Hook Dependencies
// =============================================================================

// MockQuicBaaSWithHooks extends MockQuicBaaS with agent and messaging support.
type MockQuicBaaSWithHooks struct {
	*testutil.MockQuicBaaS
	agentSvc     orchestration.AgentService
	messagingSvc messaging.Service
}

// NewMockQuicBaaSWithHooks creates a new hook-aware mock.
func NewMockQuicBaaSWithHooks(agentSvc orchestration.AgentService, messagingSvc messaging.Service) *MockQuicBaaSWithHooks {
	return &MockQuicBaaSWithHooks{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		agentSvc:     agentSvc,
		messagingSvc: messagingSvc,
	}
}

// Agent returns the configured agent service.
func (m *MockQuicBaaSWithHooks) Agent() orchestration.AgentService {
	return m.agentSvc
}

// Messaging returns the configured messaging service.
func (m *MockQuicBaaSWithHooks) Messaging() messaging.Service {
	return m.messagingSvc
}

// SetMessagingService is a no-op for the hook mock.
func (m *MockQuicBaaSWithHooks) SetMessagingService(_ messaging.Service) {}

// =============================================================================
// Test Setup Helpers
// =============================================================================

// setupHookTestServer creates a test server configured for hook handler tests
// with agent service, messaging service, and task store pre-configured.
func setupHookTestServer(t *testing.T) (*Server, *mockTaskStore, *hookMessagingStub, func()) {
	t.Helper()

	taskStore := newMockTaskStore()
	messagingSvc := &hookMessagingStub{}

	mockSvc := NewMockQuicBaaSWithHooks(&mockAgentService{}, messagingSvc)

	server, _, cleanup := setupMockServer(t)

	// Replace the service with our hook-aware mock.
	server.service = mockSvc

	// Set the hook task store on the server.
	server.SetHookTaskStore(taskStore)

	return server, taskStore, messagingSvc, cleanup
}

// MockQuicBaaSWithNilAgent returns nil from Agent() to test not-configured case.
type MockQuicBaaSWithNilAgent struct {
	*testutil.MockQuicBaaS
}

// Agent returns nil to simulate no agent service.
func (m *MockQuicBaaSWithNilAgent) Agent() orchestration.AgentService {
	return nil
}

// MockQuicBaaSWithNilMessaging has agent configured but no messaging.
type MockQuicBaaSWithNilMessaging struct {
	*testutil.MockQuicBaaS
	agentSvc orchestration.AgentService
}

// Agent returns the configured agent service.
func (m *MockQuicBaaSWithNilMessaging) Agent() orchestration.AgentService {
	return m.agentSvc
}

// Messaging returns nil to simulate no messaging service.
func (m *MockQuicBaaSWithNilMessaging) Messaging() messaging.Service {
	return nil
}

// Scheduler returns nil.
func (m *MockQuicBaaSWithNilMessaging) Scheduler() service.SchedulerService {
	return nil
}

// =============================================================================
// HandleHookTaskCreated Tests
// =============================================================================

func TestHandleHookTaskCreated_Success(t *testing.T) {
	server, taskStore, messagingSvc, cleanup := setupHookTestServer(t)
	defer cleanup()

	payload := `{
		"hook_event_name": "TaskCreated",
		"task_id": "task-001",
		"task_subject": "Implement user authentication",
		"task_description": "Add login and signup endpoints",
		"teammate_name": "implementer",
		"team_name": "my-project",
		"session_id": "abc123",
		"cwd": "/current/working/directory"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/task-created", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTaskCreated(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HookResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.ExitCode)

	// Verify task was stored.
	storedTask, ok := taskStore.tasks["my-project/task-001"]
	require.True(t, ok)
	assert.Equal(t, "task-001", storedTask.ID)
	assert.Equal(t, "Implement user authentication", storedTask.Subject)
	assert.Equal(t, orchestration.TaskStatusPending, storedTask.Status)
	assert.Equal(t, "implementer", storedTask.CreatedBy)

	// Verify messaging was published.
	assert.True(t, messagingSvc.publishCalled)
	assert.Equal(t, "orchestrator:tasks:my-project", messagingSvc.lastChannel)
}

func TestHandleHookTaskCreated_InvalidPayload(t *testing.T) {
	server, _, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/task-created", strings.NewReader("{invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTaskCreated(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid JSON body")
}

func TestHandleHookTaskCreated_MissingTaskID(t *testing.T) {
	server, _, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	payload := `{
		"hook_event_name": "TaskCreated",
		"teammate_name": "implementer",
		"team_name": "my-project"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/task-created", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTaskCreated(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "task_id")
}

func TestHandleHookTaskCreated_MissingTeamName(t *testing.T) {
	server, _, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	payload := `{
		"hook_event_name": "TaskCreated",
		"task_id": "task-001",
		"teammate_name": "implementer"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/task-created", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTaskCreated(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "team_name")
}

func TestHandleHookTaskCreated_MissingTeammateName(t *testing.T) {
	server, _, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	payload := `{
		"hook_event_name": "TaskCreated",
		"task_id": "task-001",
		"team_name": "my-project"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/task-created", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTaskCreated(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "teammate_name")
}

func TestHandleHookTaskCreated_DuplicateTask(t *testing.T) {
	server, taskStore, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	// Pre-populate an existing task.
	taskStore.tasks["my-project/task-001"] = &orchestration.Task{
		ID:       "task-001",
		TenantID: "my-project",
	}

	payload := `{
		"hook_event_name": "TaskCreated",
		"task_id": "task-001",
		"task_subject": "Duplicate task",
		"teammate_name": "implementer",
		"team_name": "my-project"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/task-created", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTaskCreated(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HookResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, 2, resp.ExitCode)
	assert.Contains(t, resp.Stderr, "task already exists")
}

func TestHandleHookTaskCreated_WithTenantHeader(t *testing.T) {
	server, taskStore, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	payload := `{
		"hook_event_name": "TaskCreated",
		"task_id": "task-002",
		"task_subject": "Tenant scoped task",
		"teammate_name": "implementer",
		"team_name": "my-project",
		"session_id": "sess-001"
	}`

	req := tenantRequest("custom-tenant", http.MethodPost, "/api/v1/hooks/task-created", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "custom-tenant")
	w := httptest.NewRecorder()

	server.HandleHookTaskCreated(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// The header repeats the caller's own tenant, so the task is stored there.
	storedTask, ok := taskStore.tasks["custom-tenant/task-002"]
	require.True(t, ok)
	assert.Equal(t, "custom-tenant", storedTask.TenantID)
}

func TestHandleHookTaskCreated_MissingHookEventName(t *testing.T) {
	server, _, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	payload := `{
		"task_id": "task-001",
		"teammate_name": "implementer",
		"team_name": "my-project"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/task-created", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTaskCreated(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "hook_event_name")
}

func TestHandleHookTaskCreated_EmptyBody(t *testing.T) {
	server, _, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/task-created", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTaskCreated(w, req)

	// Empty body is invalid JSON.
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleHookTaskCreated_PublishFailureDoesNotBlockResponse(t *testing.T) {
	server, _, messagingSvc, cleanup := setupHookTestServer(t)
	defer cleanup()

	messagingSvc.publishErr = errors.New("messaging unavailable")

	payload := `{
		"hook_event_name": "TaskCreated",
		"task_id": "task-003",
		"task_subject": "Test publish failure",
		"teammate_name": "implementer",
		"team_name": "my-project"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/task-created", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTaskCreated(w, req)

	// Should still succeed - task was stored.
	assert.Equal(t, http.StatusOK, w.Code)
}

// =============================================================================
// HandleHookTaskCompleted Tests
// =============================================================================

func TestHandleHookTaskCompleted_Success(t *testing.T) {
	server, taskStore, messagingSvc, cleanup := setupHookTestServer(t)
	defer cleanup()

	// Pre-populate a pending task.
	taskStore.tasks["my-project/task-001"] = &orchestration.Task{
		ID:       "task-001",
		TenantID: "my-project",
		TeamName: "my-project",
		Status:   orchestration.TaskStatusInProgress,
	}

	payload := `{
		"hook_event_name": "TaskCompleted",
		"task_id": "task-001",
		"task_subject": "Implement user authentication",
		"teammate_name": "implementer",
		"team_name": "my-project",
		"session_id": "abc123"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/task-completed", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTaskCompleted(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HookResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.ExitCode)

	// Verify task was marked completed.
	storedTask := taskStore.tasks["my-project/task-001"]
	assert.Equal(t, orchestration.TaskStatusCompleted, storedTask.Status)

	// Verify messaging was published.
	assert.True(t, messagingSvc.publishCalled)
	assert.Equal(t, "orchestrator:tasks:my-project", messagingSvc.lastChannel)
}

func TestHandleHookTaskCompleted_TaskNotFound(t *testing.T) {
	server, _, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	payload := `{
		"hook_event_name": "TaskCompleted",
		"task_id": "nonexistent-task",
		"team_name": "my-project"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/task-completed", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTaskCompleted(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "task")
}

func TestHandleHookTaskCompleted_InvalidPayload(t *testing.T) {
	server, _, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/task-completed", strings.NewReader("{not valid"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTaskCompleted(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleHookTaskCompleted_MissingTaskID(t *testing.T) {
	server, _, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	payload := `{
		"hook_event_name": "TaskCompleted",
		"team_name": "my-project"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/task-completed", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTaskCompleted(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "task_id")
}

func TestHandleHookTaskCompleted_MissingHookEventName(t *testing.T) {
	server, _, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	payload := `{
		"task_id": "task-001",
		"team_name": "my-project"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/task-completed", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTaskCompleted(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "hook_event_name")
}

func TestHandleHookTaskCompleted_PublishFailureDoesNotBlockResponse(t *testing.T) {
	server, taskStore, messagingSvc, cleanup := setupHookTestServer(t)
	defer cleanup()

	taskStore.tasks["my-project/task-001"] = &orchestration.Task{
		ID:       "task-001",
		TenantID: "my-project",
		Status:   orchestration.TaskStatusInProgress,
	}
	messagingSvc.publishErr = errors.New("messaging unavailable")

	payload := `{
		"hook_event_name": "TaskCompleted",
		"task_id": "task-001",
		"team_name": "my-project"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/task-completed", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTaskCompleted(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// =============================================================================
// HandleHookTeammateIdle Tests
// =============================================================================

func TestHandleHookTeammateIdle_NoAvailableTasks(t *testing.T) {
	server, _, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	payload := `{
		"hook_event_name": "TeammateIdle",
		"teammate_name": "researcher",
		"team_name": "my-project",
		"session_id": "abc123"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/teammate-idle", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTeammateIdle(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HookResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.ExitCode)
	assert.Empty(t, resp.Stderr)
}

func TestHandleHookTeammateIdle_TaskAvailable_BlocksIdle(t *testing.T) {
	server, taskStore, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	// Configure mock to return a pending task.
	taskStore.listResult = []*orchestration.Task{
		{
			ID:       "task-005",
			Subject:  "Implement caching layer",
			TeamName: "my-project",
			Status:   orchestration.TaskStatusPending,
		},
	}

	payload := `{
		"hook_event_name": "TeammateIdle",
		"teammate_name": "researcher",
		"team_name": "my-project"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/teammate-idle", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTeammateIdle(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HookResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, 2, resp.ExitCode)
	assert.Contains(t, resp.Stderr, "New task available: Implement caching layer")
}

func TestHandleHookTeammateIdle_TaskAvailableWithNoSubject(t *testing.T) {
	server, taskStore, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	// Task with no subject falls back to task ID.
	taskStore.listResult = []*orchestration.Task{
		{
			ID:       "task-006",
			TeamName: "my-project",
			Status:   orchestration.TaskStatusPending,
		},
	}

	payload := `{
		"hook_event_name": "TeammateIdle",
		"teammate_name": "researcher",
		"team_name": "my-project"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/teammate-idle", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTeammateIdle(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HookResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, 2, resp.ExitCode)
	assert.Contains(t, resp.Stderr, "New task available: task-006")
}

func TestHandleHookTeammateIdle_InvalidPayload(t *testing.T) {
	server, _, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/teammate-idle", strings.NewReader("{broken"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTeammateIdle(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleHookTeammateIdle_MissingTeammateName(t *testing.T) {
	server, _, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	payload := `{
		"hook_event_name": "TeammateIdle",
		"team_name": "my-project"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/teammate-idle", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTeammateIdle(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "teammate_name")
}

func TestHandleHookTeammateIdle_MissingTeamName(t *testing.T) {
	server, _, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	payload := `{
		"hook_event_name": "TeammateIdle",
		"teammate_name": "researcher"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/teammate-idle", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTeammateIdle(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "team_name")
}

func TestHandleHookTeammateIdle_ListTasksError(t *testing.T) {
	server, taskStore, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	taskStore.listErr = errors.New("storage failure")

	payload := `{
		"hook_event_name": "TeammateIdle",
		"teammate_name": "researcher",
		"team_name": "my-project"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/teammate-idle", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTeammateIdle(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// =============================================================================
// HookPayload Validation Tests
// =============================================================================

func TestHookPayload_Validation(t *testing.T) {
	t.Run("validateTaskCreatedPayload_valid", func(t *testing.T) {
		p := &HookPayload{
			HookEventName: "TaskCreated",
			TaskID:        "task-001",
			TeamName:      "my-project",
			TeammateName:  "implementer",
		}
		assert.NoError(t, validateTaskCreatedPayload(p))
	})

	t.Run("validateTaskCreatedPayload_missing_hook_event", func(t *testing.T) {
		p := &HookPayload{
			TaskID:       "task-001",
			TeamName:     "my-project",
			TeammateName: "implementer",
		}
		err := validateTaskCreatedPayload(p)
		require.Error(t, err)
		var hookErr *HookPayloadError
		require.True(t, errors.As(err, &hookErr))
		assert.Equal(t, "hook_event_name", hookErr.Field)
	})

	t.Run("validateTaskCreatedPayload_missing_task_id", func(t *testing.T) {
		p := &HookPayload{
			HookEventName: "TaskCreated",
			TeamName:      "my-project",
			TeammateName:  "implementer",
		}
		err := validateTaskCreatedPayload(p)
		require.Error(t, err)
		var hookErr *HookPayloadError
		require.True(t, errors.As(err, &hookErr))
		assert.Equal(t, "task_id", hookErr.Field)
	})

	t.Run("validateTaskCreatedPayload_missing_team_name", func(t *testing.T) {
		p := &HookPayload{
			HookEventName: "TaskCreated",
			TaskID:        "task-001",
			TeammateName:  "implementer",
		}
		err := validateTaskCreatedPayload(p)
		require.Error(t, err)
		var hookErr *HookPayloadError
		require.True(t, errors.As(err, &hookErr))
		assert.Equal(t, "team_name", hookErr.Field)
	})

	t.Run("validateTaskCreatedPayload_missing_teammate_name", func(t *testing.T) {
		p := &HookPayload{
			HookEventName: "TaskCreated",
			TaskID:        "task-001",
			TeamName:      "my-project",
		}
		err := validateTaskCreatedPayload(p)
		require.Error(t, err)
		var hookErr *HookPayloadError
		require.True(t, errors.As(err, &hookErr))
		assert.Equal(t, "teammate_name", hookErr.Field)
	})

	t.Run("validateTaskCompletedPayload_valid", func(t *testing.T) {
		p := &HookPayload{
			HookEventName: "TaskCompleted",
			TaskID:        "task-001",
			TeamName:      "my-project",
		}
		assert.NoError(t, validateTaskCompletedPayload(p))
	})

	t.Run("validateTaskCompletedPayload_missing_task_id", func(t *testing.T) {
		p := &HookPayload{
			HookEventName: "TaskCompleted",
			TeamName:      "my-project",
		}
		err := validateTaskCompletedPayload(p)
		require.Error(t, err)
		var hookErr *HookPayloadError
		require.True(t, errors.As(err, &hookErr))
		assert.Equal(t, "task_id", hookErr.Field)
	})

	t.Run("validateTeammateIdlePayload_valid", func(t *testing.T) {
		p := &HookPayload{
			HookEventName: "TeammateIdle",
			TeammateName:  "researcher",
			TeamName:      "my-project",
		}
		assert.NoError(t, validateTeammateIdlePayload(p))
	})

	t.Run("validateTeammateIdlePayload_missing_teammate_name", func(t *testing.T) {
		p := &HookPayload{
			HookEventName: "TeammateIdle",
			TeamName:      "my-project",
		}
		err := validateTeammateIdlePayload(p)
		require.Error(t, err)
		var hookErr *HookPayloadError
		require.True(t, errors.As(err, &hookErr))
		assert.Equal(t, "teammate_name", hookErr.Field)
	})

	t.Run("validateTeammateIdlePayload_missing_team_name", func(t *testing.T) {
		p := &HookPayload{
			HookEventName: "TeammateIdle",
			TeammateName:  "researcher",
		}
		err := validateTeammateIdlePayload(p)
		require.Error(t, err)
		var hookErr *HookPayloadError
		require.True(t, errors.As(err, &hookErr))
		assert.Equal(t, "team_name", hookErr.Field)
	})
}

// =============================================================================
// Error Type Tests
// =============================================================================

func TestHookPayloadError_Error(t *testing.T) {
	err := &HookPayloadError{Field: "task_id", Message: "required"}
	assert.Equal(t, "hook payload error: task_id: required", err.Error())
}

func TestHookProcessingError_Error(t *testing.T) {
	cause := errors.New("connection refused")
	err := &HookProcessingError{Operation: "create task", Cause: cause}
	assert.Equal(t, "hook processing error: create task: connection refused", err.Error())
	assert.True(t, errors.Is(err, cause))
}

func TestHookServiceNotConfiguredError_Error(t *testing.T) {
	err := &HookServiceNotConfiguredError{Component: "task store"}
	assert.Equal(t, "hook service not configured: task store", err.Error())
}

// =============================================================================
// Service Not Configured Tests
// =============================================================================

func TestHandleHookTaskCreated_TaskStoreNotConfigured(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	// Use a custom mock with nil agent service.
	nilAgentMock := &MockQuicBaaSWithNilAgent{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
	}
	server.service = nilAgentMock

	payload := `{
		"hook_event_name": "TaskCreated",
		"task_id": "task-001",
		"teammate_name": "implementer",
		"team_name": "my-project"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/task-created", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTaskCreated(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
	assert.Contains(t, w.Body.String(), "not configured")
}

func TestHandleHookTaskCompleted_MessagingNotConfigured(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	taskStore := newMockTaskStore()
	server.SetHookTaskStore(taskStore)

	nilMsgMock := &MockQuicBaaSWithNilMessaging{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		agentSvc:     &mockAgentService{},
	}
	server.service = nilMsgMock

	payload := `{
		"hook_event_name": "TaskCompleted",
		"task_id": "task-001",
		"team_name": "my-project"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/task-completed", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleHookTaskCompleted(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
	assert.Contains(t, w.Body.String(), "not configured")
}

// =============================================================================
// Route Integration Test
// =============================================================================

func TestHookRoutes_Integration(t *testing.T) {
	server, _, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	// Test that routes are properly registered via the router.
	payload := `{
		"hook_event_name": "TaskCreated",
		"task_id": "route-test-001",
		"task_subject": "Route test",
		"teammate_name": "tester",
		"team_name": "test-team"
	}`

	req := tenantRequest("my-project", http.MethodPost, "/api/v1/hooks/task-created", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// Placement returns nil: this double has no placement engine.
func (m *MockQuicBaaSWithNilMessaging) Placement() service.PlacementService { return nil }
