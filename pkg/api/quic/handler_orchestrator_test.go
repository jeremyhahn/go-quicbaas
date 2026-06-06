//go:build quic

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

package quic

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Mock Agent Service for QUIC Tests
// =============================================================================

// testAgentService implements orchestration.AgentService for testing.
type testAgentService struct {
	spawnSessionFunc  func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error)
	promptSessionFunc func(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error)
	sessionStatusFunc func(ctx context.Context, req *orchestration.SessionStatusRequest) (*orchestration.SessionStatusResponse, error)
	closeSessionFunc  func(ctx context.Context, req *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error)
	listSessionsFunc  func(ctx context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error)
	listBackendsFunc  func(ctx context.Context) ([]*orchestration.AgentBackendInfo, error)
	healthCheckFunc   func(ctx context.Context) (*orchestration.HealthCheckReport, error)
	createTaskFunc    func(ctx context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error)
	getTaskFunc       func(ctx context.Context, tenantID, taskID string) (*orchestration.Task, error)
	listTasksFunc     func(ctx context.Context, tenantID string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error)
	claimTaskFunc     func(ctx context.Context, tenantID, taskID, agentID string) error
	completeTaskFunc  func(ctx context.Context, tenantID, taskID string, result *orchestration.TaskResult) error
	inferFunc         func(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error)
}

func (m *testAgentService) DiscoverProjects(ctx context.Context, req *orchestration.DiscoverProjectsRequest) (*orchestration.DiscoverProjectsResponse, error) {
	return &orchestration.DiscoverProjectsResponse{Timestamp: time.Now().Unix()}, nil
}

func (m *testAgentService) RegisterProject(ctx context.Context, req *orchestration.RegisterProjectRequest) (*orchestration.RegisterProjectResponse, error) {
	return &orchestration.RegisterProjectResponse{ProjectID: "proj-1", Timestamp: time.Now().Unix()}, nil
}

func (m *testAgentService) ListProjects(ctx context.Context, req *orchestration.ListProjectsRequest) (*orchestration.ListProjectsResponse, error) {
	return &orchestration.ListProjectsResponse{Timestamp: time.Now().Unix()}, nil
}

func (m *testAgentService) SpawnSession(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
	if m.spawnSessionFunc != nil {
		return m.spawnSessionFunc(ctx, req)
	}
	return &orchestration.SpawnSessionResponse{
		SessionID: "session-abc",
		Backend:   req.Backend,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *testAgentService) PromptSession(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error) {
	if m.promptSessionFunc != nil {
		return m.promptSessionFunc(ctx, req)
	}
	return &orchestration.PromptSessionResponse{
		SessionID: req.SessionID,
		Content:   []orchestration.ContentBlock{{Type: "text", Text: "response"}},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *testAgentService) SessionStatus(ctx context.Context, req *orchestration.SessionStatusRequest) (*orchestration.SessionStatusResponse, error) {
	if m.sessionStatusFunc != nil {
		return m.sessionStatusFunc(ctx, req)
	}
	return &orchestration.SessionStatusResponse{
		SessionID: req.SessionID,
		State:     orchestration.SessionStateIdle,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *testAgentService) CloseSession(ctx context.Context, req *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error) {
	if m.closeSessionFunc != nil {
		return m.closeSessionFunc(ctx, req)
	}
	return &orchestration.CloseSessionResponse{
		SessionID: req.SessionID,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *testAgentService) ListSessions(ctx context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
	if m.listSessionsFunc != nil {
		return m.listSessionsFunc(ctx, req)
	}
	return &orchestration.ListSessionsResponse{Timestamp: time.Now().Unix()}, nil
}

func (m *testAgentService) DecomposeTask(ctx context.Context, req *orchestration.DecomposeTaskRequest) (*orchestration.DecomposeTaskResponse, error) {
	return &orchestration.DecomposeTaskResponse{Timestamp: time.Now().Unix()}, nil
}

func (m *testAgentService) ListBackends(ctx context.Context) ([]*orchestration.AgentBackendInfo, error) {
	if m.listBackendsFunc != nil {
		return m.listBackendsFunc(ctx)
	}
	return []*orchestration.AgentBackendInfo{{Name: "claude", Type: "acp"}}, nil
}

func (m *testAgentService) RegisterBackend(ctx context.Context, req *orchestration.RegisterBackendRequest) error {
	return nil
}

func (m *testAgentService) RegisterAPIInferenceBackend(_ context.Context, _ *orchestration.RegisterAPIInferenceBackendRequest) error {
	return nil
}

func (m *testAgentService) HealthCheck(ctx context.Context) (*orchestration.HealthCheckReport, error) {
	if m.healthCheckFunc != nil {
		return m.healthCheckFunc(ctx)
	}
	return &orchestration.HealthCheckReport{
		Timestamp:     time.Now().Unix(),
		TotalSessions: 0,
		Healthy:       0,
	}, nil
}

func (m *testAgentService) SetTenantGuard(guard *orchestration.TenantGuard) {}

func (m *testAgentService) CreateTask(ctx context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
	if m.createTaskFunc != nil {
		return m.createTaskFunc(ctx, req)
	}
	return &orchestration.Task{
		ID:       "task-generated",
		TenantID: req.TenantID,
		Subject:  req.Subject,
	}, nil
}

func (m *testAgentService) GetTask(ctx context.Context, tenantID, taskID string) (*orchestration.Task, error) {
	if m.getTaskFunc != nil {
		return m.getTaskFunc(ctx, tenantID, taskID)
	}
	return &orchestration.Task{
		ID:       taskID,
		TenantID: tenantID,
		Subject:  "fetched task",
	}, nil
}

func (m *testAgentService) ListTasks(ctx context.Context, tenantID string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error) {
	if m.listTasksFunc != nil {
		return m.listTasksFunc(ctx, tenantID, filter)
	}
	return []*orchestration.Task{
		{ID: "task-1", TenantID: tenantID, Subject: "task one"},
		{ID: "task-2", TenantID: tenantID, Subject: "task two"},
	}, nil
}

func (m *testAgentService) ClaimTask(ctx context.Context, tenantID, taskID, agentID string) error {
	if m.claimTaskFunc != nil {
		return m.claimTaskFunc(ctx, tenantID, taskID, agentID)
	}
	return nil
}

func (m *testAgentService) CompleteTask(ctx context.Context, tenantID, taskID string, result *orchestration.TaskResult) error {
	if m.completeTaskFunc != nil {
		return m.completeTaskFunc(ctx, tenantID, taskID, result)
	}
	return nil
}

func (m *testAgentService) AgentHeartbeat(ctx context.Context, tenantID, agentID string) error {
	return nil
}

func (m *testAgentService) RegisterAgent(ctx context.Context, req *orchestration.RegisterAgentRequest) (*orchestration.RegisterAgentResponse, error) {
	return nil, nil
}

func (m *testAgentService) GetAgent(ctx context.Context, tenantID, agentID string) (*orchestration.AgentRegistration, error) {
	return nil, nil
}

func (m *testAgentService) ListAgents(ctx context.Context, tenantID string, filter *orchestration.AgentFilter) ([]*orchestration.AgentRegistration, error) {
	return nil, nil
}

func (m *testAgentService) RemoveAgent(ctx context.Context, tenantID, agentID string) error {
	return nil
}

func (m *testAgentService) StartJob(ctx context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
	return &orchestration.StartJobResponse{}, nil
}

func (m *testAgentService) GetJobStatus(ctx context.Context, tenantID, jobID string) (*orchestration.JobStatus, error) {
	return &orchestration.JobStatus{}, nil
}

func (m *testAgentService) ListJobs(ctx context.Context, tenantID string) ([]*orchestration.JobStatus, error) {
	return nil, nil
}

func (m *testAgentService) CancelJob(ctx context.Context, tenantID, jobID string) error {
	return nil
}

func (m *testAgentService) SubscribeJobEvents(ctx context.Context, tenantID, jobID string) (<-chan *orchestration.JobEvent, error) {
	ch := make(chan *orchestration.JobEvent)
	close(ch)
	return ch, nil
}

func (m *testAgentService) BackendToolGuidance(_ string) string {
	return ""
}

func (m *testAgentService) ListBackendModels(_ context.Context, _ string) ([]string, error) {
	return []string{}, nil
}

func (m *testAgentService) ListWorkflows(_ context.Context) ([]*orchestration.WorkflowDefinition, error) {
	return []*orchestration.WorkflowDefinition{}, nil
}

func (m *testAgentService) GetWorkflow(_ context.Context, name string) (*orchestration.WorkflowDefinition, error) {
	return nil, &orchestration.WorkflowNotFoundError{Name: name}
}

func (m *testAgentService) RegisterWorkflow(_ context.Context, def *orchestration.WorkflowDefinition) error {
	return nil
}

func (m *testAgentService) UpdateWorkflow(_ context.Context, def *orchestration.WorkflowDefinition) error {
	return nil
}

func (m *testAgentService) DeleteWorkflow(_ context.Context, name string) error {
	return nil
}

func (m *testAgentService) SpawnChildJob(_ context.Context, _ *orchestration.SpawnChildJobRequest) (*orchestration.SpawnChildJobResponse, error) {
	return &orchestration.SpawnChildJobResponse{}, nil
}

func (m *testAgentService) SubmitTaskArtifacts(_ context.Context, _, _ string, _ []orchestration.Artifact) error {
	return nil
}

func (m *testAgentService) GetTaskArtifact(_ context.Context, _, _, _ string) (*orchestration.Artifact, error) {
	return nil, nil
}

func (m *testAgentService) ListTaskArtifacts(_ context.Context, _, _ string) ([]orchestration.Artifact, error) {
	return nil, nil
}

// Infer delegates to inferFunc when set; otherwise returns an empty closed channel.
func (m *testAgentService) Infer(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
	if m.inferFunc != nil {
		return m.inferFunc(ctx, req)
	}
	ch := make(chan *orchestration.InferChunk)
	close(ch)
	return ch, nil
}

// =============================================================================
// Mock Service with Agent Support
// =============================================================================

// mockServiceWithAgent wraps mockService and provides an Agent() that returns
// a real test agent service.
type mockServiceWithAgent struct {
	mockService
	agentSvc orchestration.AgentService
}

func (m *mockServiceWithAgent) Agent() orchestration.AgentService {
	return m.agentSvc
}

// =============================================================================
// Test Helper
// =============================================================================

func createOrchestratorTestLogger() *logger.Logger {
	cfg := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "quic-orchestrator-test",
	}
	log, err := logger.New(cfg)
	if err != nil {
		panic(err)
	}
	return log
}

func createOrchestratorHandler(agentSvc orchestration.AgentService) *Handler {
	svc := &mockServiceWithAgent{
		agentSvc: agentSvc,
	}
	return NewHandler(svc, nil, nil, nil, createOrchestratorTestLogger())
}

// buildOrchestratorRequest creates a Request with OpOrchestrator and the given sub-opcode + payload.
func buildOrchestratorRequest(subOp byte, payload []byte) *Request {
	// Prepend the sub-opcode byte to the payload
	fullPayload := make([]byte, 1+len(payload))
	fullPayload[0] = subOp
	copy(fullPayload[1:], payload)

	return &Request{
		Opcode:  OpOrchestrator,
		Payload: fullPayload,
	}
}

// =============================================================================
// Spawn Session Tests
// =============================================================================

func TestHandleOrchestratorSpawn_Success(t *testing.T) {
	agentSvc := &testAgentService{}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestration.SpawnSessionRequest{
		Backend: "claude",
		Model:   "claude-4",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorSpawn, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var spawnResp orchestration.SpawnSessionResponse
	if err := DecodePayload(resp.Payload, &spawnResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if spawnResp.SessionID != "session-abc" {
		t.Errorf("expected session-abc, got %s", spawnResp.SessionID)
	}
}

func TestHandleOrchestratorSpawn_InvalidPayload(t *testing.T) {
	agentSvc := &testAgentService{}
	handler := createOrchestratorHandler(agentSvc)

	req := buildOrchestratorRequest(SubOpOrchestratorSpawn, []byte("invalid"))
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOrchestratorSpawn_NilService(t *testing.T) {
	handler := createOrchestratorHandler(nil)

	payload, err := EncodePayload(&orchestration.SpawnSessionRequest{Backend: "claude"})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorSpawn, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleOrchestratorSpawn_ServiceError(t *testing.T) {
	agentSvc := &testAgentService{
		spawnSessionFunc: func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
			return nil, &orchestration.AgentServiceError{
				Sentinel: orchestration.ErrBackendRequired,
				Message:  "backend not available",
			}
		},
	}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestration.SpawnSessionRequest{Backend: "invalid"})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorSpawn, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Create Task Tests
// =============================================================================

func TestHandleOrchestratorCreateTask_Success(t *testing.T) {
	agentSvc := &testAgentService{
		createTaskFunc: func(ctx context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
			return &orchestration.Task{
				ID:       "task-created",
				TenantID: req.TenantID,
				Subject:  req.Subject,
			}, nil
		},
	}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestration.CreateTaskRequest{
		TenantID: "tenant-1",
		Subject:  "implement feature",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorCreateTask, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var task orchestration.Task
	if err := DecodePayload(resp.Payload, &task); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if task.ID != "task-created" {
		t.Errorf("expected task ID task-created, got %s", task.ID)
	}
	if task.Subject != "implement feature" {
		t.Errorf("expected subject 'implement feature', got %s", task.Subject)
	}
}

func TestHandleOrchestratorCreateTask_ServiceError(t *testing.T) {
	agentSvc := &testAgentService{
		createTaskFunc: func(ctx context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
			return nil, &orchestration.AgentServiceError{
				Sentinel: orchestration.ErrTenantIDRequired,
				Message:  "tenant ID is required",
			}
		},
	}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestration.CreateTaskRequest{})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorCreateTask, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleOrchestratorCreateTask_InvalidPayload(t *testing.T) {
	agentSvc := &testAgentService{}
	handler := createOrchestratorHandler(agentSvc)

	req := buildOrchestratorRequest(SubOpOrchestratorCreateTask, []byte("invalid"))
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

// =============================================================================
// Get Task Tests
// =============================================================================

func TestHandleOrchestratorGetTask_Success(t *testing.T) {
	agentSvc := &testAgentService{
		getTaskFunc: func(ctx context.Context, tenantID, taskID string) (*orchestration.Task, error) {
			return &orchestration.Task{
				ID:       taskID,
				TenantID: tenantID,
				Subject:  "fetched task",
			}, nil
		},
	}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestratorGetTaskRequest{
		TenantID: "tenant-1",
		TaskID:   "task-1",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorGetTask, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var task orchestration.Task
	if err := DecodePayload(resp.Payload, &task); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if task.ID != "task-1" {
		t.Errorf("expected task ID task-1, got %s", task.ID)
	}
	if task.TenantID != "tenant-1" {
		t.Errorf("expected tenant ID tenant-1, got %s", task.TenantID)
	}
}

func TestHandleOrchestratorGetTask_ServiceError(t *testing.T) {
	agentSvc := &testAgentService{
		getTaskFunc: func(ctx context.Context, tenantID, taskID string) (*orchestration.Task, error) {
			return nil, &orchestration.AgentServiceError{
				Sentinel: orchestration.ErrTaskNotFound,
				Message:  "task not found",
			}
		},
	}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestratorGetTaskRequest{
		TenantID: "tenant-1",
		TaskID:   "nonexistent",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorGetTask, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleOrchestratorGetTask_InvalidPayload(t *testing.T) {
	agentSvc := &testAgentService{}
	handler := createOrchestratorHandler(agentSvc)

	req := buildOrchestratorRequest(SubOpOrchestratorGetTask, []byte("bad"))
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

// =============================================================================
// List Tasks Tests
// =============================================================================

func TestHandleOrchestratorListTasks_Success(t *testing.T) {
	agentSvc := &testAgentService{
		listTasksFunc: func(ctx context.Context, tenantID string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error) {
			return []*orchestration.Task{
				{ID: "task-1", TenantID: tenantID, Subject: "task one"},
				{ID: "task-2", TenantID: tenantID, Subject: "task two"},
			}, nil
		},
	}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestratorListTasksRequest{
		TenantID: "tenant-1",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorListTasks, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var tasks []*orchestration.Task
	if err := DecodePayload(resp.Payload, &tasks); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestHandleOrchestratorListTasks_InvalidPayload(t *testing.T) {
	agentSvc := &testAgentService{}
	handler := createOrchestratorHandler(agentSvc)

	req := buildOrchestratorRequest(SubOpOrchestratorListTasks, []byte("bad"))
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOrchestratorListTasks_ServiceError(t *testing.T) {
	agentSvc := &testAgentService{
		listTasksFunc: func(ctx context.Context, tenantID string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error) {
			return nil, &orchestration.AgentServiceError{
				Sentinel: orchestration.ErrTenantIDRequired,
				Message:  "tenant ID is required",
			}
		},
	}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestratorListTasksRequest{})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorListTasks, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Claim Task Tests
// =============================================================================

func TestHandleOrchestratorClaimTask_Success(t *testing.T) {
	agentSvc := &testAgentService{
		claimTaskFunc: func(ctx context.Context, tenantID, taskID, agentID string) error {
			return nil
		},
	}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestratorClaimTaskRequest{
		TenantID: "tenant-1",
		TaskID:   "task-1",
		AgentID:  "agent-1",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorClaimTask, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var successResp orchestratorSuccessResponse
	if err := DecodePayload(resp.Payload, &successResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !successResp.Success {
		t.Error("expected success to be true")
	}
}

func TestHandleOrchestratorClaimTask_InvalidPayload(t *testing.T) {
	agentSvc := &testAgentService{}
	handler := createOrchestratorHandler(agentSvc)

	req := buildOrchestratorRequest(SubOpOrchestratorClaimTask, []byte("bad"))
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOrchestratorClaimTask_ServiceError(t *testing.T) {
	agentSvc := &testAgentService{
		claimTaskFunc: func(ctx context.Context, tenantID, taskID, agentID string) error {
			return &orchestration.AgentServiceError{
				Sentinel: orchestration.ErrTaskNotFound,
				Message:  "task not found",
			}
		},
	}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestratorClaimTaskRequest{
		TenantID: "tenant-1",
		TaskID:   "nonexistent",
		AgentID:  "agent-1",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorClaimTask, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Complete Task Tests
// =============================================================================

func TestHandleOrchestratorCompleteTask_Success(t *testing.T) {
	agentSvc := &testAgentService{
		completeTaskFunc: func(ctx context.Context, tenantID, taskID string, result *orchestration.TaskResult) error {
			return nil
		},
	}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestratorCompleteTaskRequest{
		TenantID: "tenant-1",
		TaskID:   "task-1",
		Result:   &orchestration.TaskResult{Output: "done"},
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorCompleteTask, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var successResp orchestratorSuccessResponse
	if err := DecodePayload(resp.Payload, &successResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !successResp.Success {
		t.Error("expected success to be true")
	}
}

func TestHandleOrchestratorCompleteTask_InvalidPayload(t *testing.T) {
	agentSvc := &testAgentService{}
	handler := createOrchestratorHandler(agentSvc)

	req := buildOrchestratorRequest(SubOpOrchestratorCompleteTask, []byte("bad"))
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOrchestratorCompleteTask_ServiceError(t *testing.T) {
	agentSvc := &testAgentService{
		completeTaskFunc: func(ctx context.Context, tenantID, taskID string, result *orchestration.TaskResult) error {
			return &orchestration.AgentServiceError{
				Sentinel: orchestration.ErrTaskNotFound,
				Message:  "task not found",
			}
		},
	}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestratorCompleteTaskRequest{
		TenantID: "tenant-1",
		TaskID:   "nonexistent",
		Result:   &orchestration.TaskResult{Output: "done"},
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorCompleteTask, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Health Tests
// =============================================================================

func TestHandleOrchestratorHealth_Success(t *testing.T) {
	agentSvc := &testAgentService{
		healthCheckFunc: func(ctx context.Context) (*orchestration.HealthCheckReport, error) {
			return &orchestration.HealthCheckReport{
				Timestamp:     time.Now().Unix(),
				TotalSessions: 5,
				Healthy:       4,
				Unhealthy:     1,
			}, nil
		},
	}
	handler := createOrchestratorHandler(agentSvc)

	// Health doesn't need payload
	req := buildOrchestratorRequest(SubOpOrchestratorHealth, []byte{})
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var report orchestration.HealthCheckReport
	if err := DecodePayload(resp.Payload, &report); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if report.TotalSessions != 5 {
		t.Errorf("expected 5 total sessions, got %d", report.TotalSessions)
	}
}

func TestHandleOrchestratorHealth_NilService(t *testing.T) {
	handler := createOrchestratorHandler(nil)

	req := buildOrchestratorRequest(SubOpOrchestratorHealth, []byte{})
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

// =============================================================================
// List Backends Tests
// =============================================================================

func TestHandleOrchestratorListBackends_Success(t *testing.T) {
	agentSvc := &testAgentService{
		listBackendsFunc: func(ctx context.Context) ([]*orchestration.AgentBackendInfo, error) {
			return []*orchestration.AgentBackendInfo{
				{Name: "claude", Type: "acp"},
				{Name: "ollama", Type: "ollama"},
			}, nil
		},
	}
	handler := createOrchestratorHandler(agentSvc)

	req := buildOrchestratorRequest(SubOpOrchestratorListBackends, []byte{})
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

// =============================================================================
// Prompt Tests
// =============================================================================

func TestHandleOrchestratorPrompt_Success(t *testing.T) {
	agentSvc := &testAgentService{}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestration.PromptSessionRequest{
		SessionID: "sess-1",
		Prompt:    "hello world",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorPrompt, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

// =============================================================================
// Session Status Tests
// =============================================================================

func TestHandleOrchestratorSessionStatus_Success(t *testing.T) {
	agentSvc := &testAgentService{}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestration.SessionStatusRequest{
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorSessionStatus, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

// =============================================================================
// Dispatch Tests
// =============================================================================

func TestHandleOrchestratorDispatch_MissingSubOpcode(t *testing.T) {
	agentSvc := &testAgentService{}
	handler := createOrchestratorHandler(agentSvc)

	req := &Request{
		Opcode:  OpOrchestrator,
		Payload: []byte{}, // empty payload - no sub-opcode
	}

	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error with missing sub-opcode")
	}
}

func TestHandleOrchestratorDispatch_UnknownSubOpcode(t *testing.T) {
	agentSvc := &testAgentService{}
	handler := createOrchestratorHandler(agentSvc)

	req := &Request{
		Opcode:  OpOrchestrator,
		Payload: []byte{0xFF}, // unknown sub-opcode
	}

	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error with unknown sub-opcode")
	}
}

// =============================================================================
// Close Session Tests
// =============================================================================

func TestHandleOrchestratorCloseSession_Success(t *testing.T) {
	agentSvc := &testAgentService{}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestration.CloseSessionRequest{
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorCloseSession, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

// =============================================================================
// List Sessions Tests
// =============================================================================

func TestHandleOrchestratorListSessions_Success(t *testing.T) {
	agentSvc := &testAgentService{}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestration.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorListSessions, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

// =============================================================================
// Inference Tests
// =============================================================================

func TestHandleOrchestratorInference_Success(t *testing.T) {
	agentSvc := &testAgentService{
		inferFunc: func(_ context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			ch := make(chan *orchestration.InferChunk, 1)
			ch <- &orchestration.InferChunk{
				Delta:      []orchestration.ContentBlock{{Type: "text", Text: "hello"}},
				Model:      req.Model,
				StopReason: "end_turn",
				Final:      true,
			}
			close(ch)
			return ch, nil
		},
	}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestratorInferenceRequest{
		Backend: "ollama",
		Model:   "llama3",
		Prompt:  "say hello",
	})
	if err != nil {
		t.Fatalf("EncodePayload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorInference, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var result orchestration.InferResponse
	if err := DecodePayload(resp.Payload, &result); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if result.StopReason != "end_turn" {
		t.Errorf("expected stop_reason=end_turn, got %q", result.StopReason)
	}
	if len(result.Content) == 0 || result.Content[0].Text != "hello" {
		t.Errorf("unexpected content: %+v", result.Content)
	}
}

func TestHandleOrchestratorInference_InvalidPayload(t *testing.T) {
	agentSvc := &testAgentService{}
	handler := createOrchestratorHandler(agentSvc)

	// Build a request with a garbage payload (not valid msgpack).
	fullPayload := []byte{SubOpOrchestratorInference, 0xFF, 0xFE}
	req := &Request{Opcode: OpOrchestrator, Payload: fullPayload}
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error for invalid payload")
	}
}

func TestHandleOrchestratorInference_NoAgentService(t *testing.T) {
	// Handler with no agent service.
	svc := &mockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	handler := NewHandler(svc, nil, nil, nil, log)

	payload, err := EncodePayload(&orchestratorInferenceRequest{
		Backend: "ollama",
		Prompt:  "hi",
	})
	if err != nil {
		t.Fatalf("EncodePayload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorInference, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error when agent service is nil")
	}
}

func TestHandleOrchestratorInference_InferError(t *testing.T) {
	agentSvc := &testAgentService{
		inferFunc: func(_ context.Context, _ *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			return nil, &orchestration.InferUnsupportedError{Backend: "acp"}
		},
	}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestratorInferenceRequest{
		Backend: "acp",
		Prompt:  "hi",
	})
	if err != nil {
		t.Fatalf("EncodePayload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorInference, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error for unsupported backend")
	}
}

func TestHandleOrchestratorInference_WithHostAffinity(t *testing.T) {
	var capturedCtx context.Context
	agentSvc := &testAgentService{
		inferFunc: func(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			capturedCtx = ctx
			ch := make(chan *orchestration.InferChunk, 1)
			ch <- &orchestration.InferChunk{
				Delta: []orchestration.ContentBlock{{Type: "text", Text: "ok"}},
				Final: true,
			}
			close(ch)
			return ch, nil
		},
	}
	handler := createOrchestratorHandler(agentSvc)

	payload, err := EncodePayload(&orchestratorInferenceRequest{
		Backend:      "ollama",
		Prompt:       "hi",
		HostAffinity: "node-1",
	})
	if err != nil {
		t.Fatalf("EncodePayload: %v", err)
	}

	req := buildOrchestratorRequest(SubOpOrchestratorInference, payload)
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	// The host affinity should have been set in the context.
	affinity := orchestration.HostAffinityFromContext(capturedCtx)
	if affinity != "node-1" {
		t.Errorf("expected host affinity=node-1, got %q", affinity)
	}
}
