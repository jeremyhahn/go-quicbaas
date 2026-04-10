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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// =============================================================================
// Mock Agent Service
// =============================================================================

// mockAgentService implements orchestration.AgentService for testing.
type mockAgentService struct {
	discoverProjectsFunc func(ctx context.Context, req *orchestration.DiscoverProjectsRequest) (*orchestration.DiscoverProjectsResponse, error)
	registerProjectFunc  func(ctx context.Context, req *orchestration.RegisterProjectRequest) (*orchestration.RegisterProjectResponse, error)
	listProjectsFunc     func(ctx context.Context, req *orchestration.ListProjectsRequest) (*orchestration.ListProjectsResponse, error)
	spawnSessionFunc     func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error)
	promptSessionFunc    func(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error)
	sessionStatusFunc    func(ctx context.Context, req *orchestration.SessionStatusRequest) (*orchestration.SessionStatusResponse, error)
	closeSessionFunc     func(ctx context.Context, req *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error)
	listSessionsFunc     func(ctx context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error)
	decomposeTaskFunc    func(ctx context.Context, req *orchestration.DecomposeTaskRequest) (*orchestration.DecomposeTaskResponse, error)
	listBackendsFunc     func(ctx context.Context) ([]*orchestration.AgentBackendInfo, error)
	registerBackendFunc  func(ctx context.Context, req *orchestration.RegisterBackendRequest) error
	healthCheckFunc      func(ctx context.Context) (*orchestration.HealthCheckReport, error)
	createTaskFunc       func(ctx context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error)
	getTaskFunc          func(ctx context.Context, tenantID, taskID string) (*orchestration.Task, error)
	listTasksFunc        func(ctx context.Context, tenantID string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error)
	claimTaskFunc        func(ctx context.Context, tenantID, taskID, agentID string) error
	completeTaskFunc     func(ctx context.Context, tenantID, taskID string, result *orchestration.TaskResult) error
}

func (m *mockAgentService) DiscoverProjects(ctx context.Context, req *orchestration.DiscoverProjectsRequest) (*orchestration.DiscoverProjectsResponse, error) {
	if m.discoverProjectsFunc != nil {
		return m.discoverProjectsFunc(ctx, req)
	}
	return &orchestration.DiscoverProjectsResponse{Timestamp: time.Now().Unix()}, nil
}

func (m *mockAgentService) RegisterProject(ctx context.Context, req *orchestration.RegisterProjectRequest) (*orchestration.RegisterProjectResponse, error) {
	if m.registerProjectFunc != nil {
		return m.registerProjectFunc(ctx, req)
	}
	return &orchestration.RegisterProjectResponse{ProjectID: "proj-1", Timestamp: time.Now().Unix()}, nil
}

func (m *mockAgentService) ListProjects(ctx context.Context, req *orchestration.ListProjectsRequest) (*orchestration.ListProjectsResponse, error) {
	if m.listProjectsFunc != nil {
		return m.listProjectsFunc(ctx, req)
	}
	return &orchestration.ListProjectsResponse{Timestamp: time.Now().Unix()}, nil
}

func (m *mockAgentService) SpawnSession(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
	if m.spawnSessionFunc != nil {
		return m.spawnSessionFunc(ctx, req)
	}
	return &orchestration.SpawnSessionResponse{
		SessionID: "session-abc",
		Backend:   req.Backend,
		Model:     req.Model,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *mockAgentService) PromptSession(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error) {
	if m.promptSessionFunc != nil {
		return m.promptSessionFunc(ctx, req)
	}
	return &orchestration.PromptSessionResponse{
		SessionID: req.SessionID,
		Content:   []orchestration.ContentBlock{{Type: "text", Text: "response"}},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *mockAgentService) SessionStatus(ctx context.Context, req *orchestration.SessionStatusRequest) (*orchestration.SessionStatusResponse, error) {
	if m.sessionStatusFunc != nil {
		return m.sessionStatusFunc(ctx, req)
	}
	return &orchestration.SessionStatusResponse{
		SessionID: req.SessionID,
		State:     orchestration.SessionStateIdle,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *mockAgentService) CloseSession(ctx context.Context, req *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error) {
	if m.closeSessionFunc != nil {
		return m.closeSessionFunc(ctx, req)
	}
	return &orchestration.CloseSessionResponse{
		SessionID: req.SessionID,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *mockAgentService) ListSessions(ctx context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
	if m.listSessionsFunc != nil {
		return m.listSessionsFunc(ctx, req)
	}
	return &orchestration.ListSessionsResponse{Timestamp: time.Now().Unix()}, nil
}

func (m *mockAgentService) DecomposeTask(ctx context.Context, req *orchestration.DecomposeTaskRequest) (*orchestration.DecomposeTaskResponse, error) {
	if m.decomposeTaskFunc != nil {
		return m.decomposeTaskFunc(ctx, req)
	}
	return &orchestration.DecomposeTaskResponse{Timestamp: time.Now().Unix()}, nil
}

func (m *mockAgentService) ListBackends(ctx context.Context) ([]*orchestration.AgentBackendInfo, error) {
	if m.listBackendsFunc != nil {
		return m.listBackendsFunc(ctx)
	}
	return []*orchestration.AgentBackendInfo{
		{Name: "claude", Type: "acp"},
	}, nil
}

func (m *mockAgentService) RegisterBackend(ctx context.Context, req *orchestration.RegisterBackendRequest) error {
	if m.registerBackendFunc != nil {
		return m.registerBackendFunc(ctx, req)
	}
	return nil
}

func (m *mockAgentService) HealthCheck(ctx context.Context) (*orchestration.HealthCheckReport, error) {
	if m.healthCheckFunc != nil {
		return m.healthCheckFunc(ctx)
	}
	return &orchestration.HealthCheckReport{
		Timestamp:     time.Now().Unix(),
		TotalSessions: 0,
		Healthy:       0,
		Unhealthy:     0,
		Closed:        0,
	}, nil
}

func (m *mockAgentService) SetTenantGuard(guard *orchestration.TenantGuard) {}

func (m *mockAgentService) CreateTask(ctx context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
	if m.createTaskFunc != nil {
		return m.createTaskFunc(ctx, req)
	}
	return &orchestration.Task{
		ID:      "task-default",
		Subject: req.Subject,
	}, nil
}

func (m *mockAgentService) GetTask(ctx context.Context, tenantID, taskID string) (*orchestration.Task, error) {
	if m.getTaskFunc != nil {
		return m.getTaskFunc(ctx, tenantID, taskID)
	}
	return &orchestration.Task{
		ID:       taskID,
		TenantID: tenantID,
	}, nil
}

func (m *mockAgentService) ListTasks(ctx context.Context, tenantID string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error) {
	if m.listTasksFunc != nil {
		return m.listTasksFunc(ctx, tenantID, filter)
	}
	return []*orchestration.Task{}, nil
}

func (m *mockAgentService) ClaimTask(ctx context.Context, tenantID, taskID, agentID string) error {
	if m.claimTaskFunc != nil {
		return m.claimTaskFunc(ctx, tenantID, taskID, agentID)
	}
	return nil
}

func (m *mockAgentService) CompleteTask(ctx context.Context, tenantID, taskID string, result *orchestration.TaskResult) error {
	if m.completeTaskFunc != nil {
		return m.completeTaskFunc(ctx, tenantID, taskID, result)
	}
	return nil
}

func (m *mockAgentService) AgentHeartbeat(ctx context.Context, tenantID, agentID string) error {
	return nil
}

func (m *mockAgentService) RegisterAgent(ctx context.Context, req *orchestration.RegisterAgentRequest) (*orchestration.RegisterAgentResponse, error) {
	return nil, nil
}

func (m *mockAgentService) GetAgent(ctx context.Context, tenantID, agentID string) (*orchestration.AgentRegistration, error) {
	return nil, nil
}

func (m *mockAgentService) ListAgents(ctx context.Context, tenantID string, filter *orchestration.AgentFilter) ([]*orchestration.AgentRegistration, error) {
	return nil, nil
}

func (m *mockAgentService) RemoveAgent(ctx context.Context, tenantID, agentID string) error {
	return nil
}

func (m *mockAgentService) StartJob(ctx context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
	return &orchestration.StartJobResponse{}, nil
}

func (m *mockAgentService) GetJobStatus(ctx context.Context, tenantID, jobID string) (*orchestration.JobStatus, error) {
	return &orchestration.JobStatus{}, nil
}

func (m *mockAgentService) ListJobs(ctx context.Context, tenantID string) ([]*orchestration.JobStatus, error) {
	return nil, nil
}

func (m *mockAgentService) CancelJob(ctx context.Context, tenantID, jobID string) error {
	return nil
}

func (m *mockAgentService) SubscribeJobEvents(ctx context.Context, tenantID, jobID string) (<-chan *orchestration.JobEvent, error) {
	ch := make(chan *orchestration.JobEvent)
	close(ch)
	return ch, nil
}

func (m *mockAgentService) BackendToolGuidance(_ string) string {
	return ""
}

// =============================================================================
// Mock QuicBaaS with Agent Service
// =============================================================================

// MockQuicBaaSWithAgent extends MockQuicBaaS with agent service support.
type MockQuicBaaSWithAgent struct {
	*testutil.MockQuicBaaS
	agentService orchestration.AgentService
}

// Agent returns the mock agent service.
func (m *MockQuicBaaSWithAgent) Agent() orchestration.AgentService {
	return m.agentService
}

// =============================================================================
// Test Helper Functions
// =============================================================================

// setupOrchestratorMockServer creates a test server with mock agent service.
func setupOrchestratorMockServer(t *testing.T) (*Server, *mockAgentService, func()) {
	t.Helper()

	agentSvc := &mockAgentService{}

	mockService := &MockQuicBaaSWithAgent{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		agentService: agentSvc,
	}

	server, _, cleanup := setupMockServer(t)
	server.service = mockService

	return server, agentSvc, cleanup
}

// =============================================================================
// Spawn Session Tests
// =============================================================================

func TestOrchestratorSpawn_Success(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.spawnSessionFunc = func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
		return &orchestration.SpawnSessionResponse{
			SessionID: "session-123",
			Backend:   req.Backend,
			Model:     "claude-4",
			Timestamp: time.Now().Unix(),
		}, nil
	}

	body, err := json.Marshal(orchestration.SpawnSessionRequest{
		Backend: "claude",
		Model:   "claude-4",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleOrchestratorSpawn(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp orchestration.SpawnSessionResponse
	err = json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "session-123", resp.SessionID)
	assert.Equal(t, "claude", resp.Backend)
}

func TestOrchestratorSpawn_InvalidPayload(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/sessions", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleOrchestratorSpawn(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOrchestratorSpawn_MissingBackend(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	body, err := json.Marshal(orchestration.SpawnSessionRequest{})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleOrchestratorSpawn(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOrchestratorSpawn_ServiceUnavailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	// Default mock returns nil for Agent()
	body, err := json.Marshal(orchestration.SpawnSessionRequest{Backend: "claude"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleOrchestratorSpawn(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestOrchestratorSpawn_ServiceError(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.spawnSessionFunc = func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
		return nil, &orchestration.AgentServiceError{
			Sentinel: orchestration.ErrBackendRequired,
			Message:  "backend not found",
		}
	}

	body, err := json.Marshal(orchestration.SpawnSessionRequest{Backend: "invalid"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleOrchestratorSpawn(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// =============================================================================
// Create Task Tests
// =============================================================================

func TestOrchestratorCreateTask_Success(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.createTaskFunc = func(ctx context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
		return &orchestration.Task{
			ID:          "task-001",
			Subject:     req.Subject,
			Description: req.Description,
			TenantID:    req.TenantID,
		}, nil
	}

	body, err := json.Marshal(orchestration.CreateTaskRequest{
		Subject:     "implement feature X",
		Description: "full description",
		TenantID:    "tenant-1",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleOrchestratorCreateTask(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp orchestration.Task
	err = json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "task-001", resp.ID)
	assert.Equal(t, "implement feature X", resp.Subject)
}

func TestOrchestratorCreateTask_InvalidPayload(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/tasks", bytes.NewReader([]byte("not-json")))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleOrchestratorCreateTask(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOrchestratorCreateTask_MissingSubject(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	body, err := json.Marshal(orchestration.CreateTaskRequest{Description: "no subject"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleOrchestratorCreateTask(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOrchestratorCreateTask_ServiceUnavailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body, err := json.Marshal(orchestration.CreateTaskRequest{Subject: "test"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleOrchestratorCreateTask(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestOrchestratorCreateTask_ServiceError(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.createTaskFunc = func(ctx context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
		return nil, &orchestration.TaskAlreadyExistsError{TaskID: "task-dup"}
	}

	body, err := json.Marshal(orchestration.CreateTaskRequest{
		Subject: "duplicate",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleOrchestratorCreateTask(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

// =============================================================================
// List Tasks Tests
// =============================================================================

func TestOrchestratorListTasks_Success(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.listTasksFunc = func(ctx context.Context, tenantID string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error) {
		return []*orchestration.Task{
			{ID: "task-1", Subject: "first", Status: orchestration.TaskStatusPending},
			{ID: "task-2", Subject: "second", Status: orchestration.TaskStatusClaimed},
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/tasks?tenant_id=t1", nil)

	rec := httptest.NewRecorder()
	server.HandleOrchestratorListTasks(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]json.RawMessage
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Contains(t, string(resp["tasks"]), "task-1")
}

func TestOrchestratorListTasks_WithFilter(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	var capturedFilter *orchestration.TaskFilter
	agentSvc.listTasksFunc = func(ctx context.Context, tenantID string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error) {
		capturedFilter = filter
		return []*orchestration.Task{}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/tasks?tenant_id=t1&status=pending&team_name=alpha", nil)

	rec := httptest.NewRecorder()
	server.HandleOrchestratorListTasks(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, capturedFilter)
	assert.Equal(t, orchestration.TaskStatus("pending"), capturedFilter.Status)
	assert.Equal(t, "alpha", capturedFilter.TeamName)
}

// =============================================================================
// Claim Task Tests
// =============================================================================

func TestOrchestratorClaimTask_Success(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	body, err := json.Marshal(OrchestratorClaimTaskRequest{AgentID: "agent-1"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/tasks/task-1/claim?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskID", "task-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorClaimTask(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err = json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, true, resp["claimed"])
	assert.Equal(t, "agent-1", resp["agent_id"])
}

func TestOrchestratorClaimTask_MissingAgentID(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	body, err := json.Marshal(OrchestratorClaimTaskRequest{})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/tasks/task-1/claim", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskID", "task-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorClaimTask(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOrchestratorClaimTask_AlreadyClaimed(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.claimTaskFunc = func(ctx context.Context, tenantID, taskID, agentID string) error {
		return &orchestration.TaskAlreadyClaimedError{
			TaskID:       taskID,
			ClaimedBy:    "agent-other",
			RequestingBy: agentID,
		}
	}

	body, err := json.Marshal(OrchestratorClaimTaskRequest{AgentID: "agent-1"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/tasks/task-1/claim?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskID", "task-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorClaimTask(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

// =============================================================================
// Health Tests
// =============================================================================

func TestOrchestratorHealth_Success(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/health", nil)

	rec := httptest.NewRecorder()
	server.HandleOrchestratorHealth(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]string
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "healthy", resp["status"])
}

func TestOrchestratorHealth_ServiceUnavailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/health", nil)

	rec := httptest.NewRecorder()
	server.HandleOrchestratorHealth(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// =============================================================================
// Prompt Tests
// =============================================================================

func TestOrchestratorPrompt_Success(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.promptSessionFunc = func(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error) {
		return &orchestration.PromptSessionResponse{
			SessionID: req.SessionID,
			Content:   []orchestration.ContentBlock{{Type: "text", Text: "hello world"}},
			Timestamp: time.Now().Unix(),
		}, nil
	}

	body, err := json.Marshal(orchestration.PromptSessionRequest{
		Prompt: "say hello",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/sessions/sess-1/prompt", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionID", "sess-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorPrompt(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestOrchestratorPrompt_MissingPrompt(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	body, err := json.Marshal(orchestration.PromptSessionRequest{})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/sessions/sess-1/prompt", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionID", "sess-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorPrompt(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// =============================================================================
// List Backends Tests
// =============================================================================

func TestOrchestratorListBackends_Success(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.listBackendsFunc = func(ctx context.Context) ([]*orchestration.AgentBackendInfo, error) {
		return []*orchestration.AgentBackendInfo{
			{Name: "claude", Type: "acp"},
			{Name: "ollama", Type: "ollama"},
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/backends", nil)

	rec := httptest.NewRecorder()
	server.HandleOrchestratorListBackends(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// =============================================================================
// Discover Tests
// =============================================================================

func TestOrchestratorDiscover_Success(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.discoverProjectsFunc = func(ctx context.Context, req *orchestration.DiscoverProjectsRequest) (*orchestration.DiscoverProjectsResponse, error) {
		return &orchestration.DiscoverProjectsResponse{
			Projects: []*orchestration.DiscoveredProject{
				{Name: "my-project", Path: req.RootPath + "/my-project"},
			},
			Timestamp: time.Now().Unix(),
		}, nil
	}

	body, err := json.Marshal(orchestration.DiscoverProjectsRequest{RootPath: "/tmp"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/discover", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleOrchestratorDiscover(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestOrchestratorDiscover_MissingRootPath(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	body, err := json.Marshal(orchestration.DiscoverProjectsRequest{})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/discover", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleOrchestratorDiscover(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// =============================================================================
// Session Status Tests
// =============================================================================

func TestOrchestratorSessionStatus_Success(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/sessions/sess-1", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionID", "sess-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorSessionStatus(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestOrchestratorCloseSession_Success(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/orchestrator/sessions/sess-1", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionID", "sess-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorCloseSession(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestOrchestratorListSessions_Success(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/sessions", nil)

	rec := httptest.NewRecorder()
	server.HandleOrchestratorListSessions(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// =============================================================================
// Complete Task Tests
// =============================================================================

func TestOrchestratorCompleteTask_Success(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	body, err := json.Marshal(OrchestratorCompleteTaskRequest{
		Result: &orchestration.TaskResult{
			Output: "done",
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/tasks/task-1/complete?tenant_id=t1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskID", "task-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorCompleteTask(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestOrchestratorGetTask_Success(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/tasks/task-1?tenant_id=t1", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskID", "task-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorGetTask(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// =============================================================================
// Error Mapping Tests
// =============================================================================

func TestOrchestratorErrorMapping_NotFound(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.getTaskFunc = func(ctx context.Context, tenantID, taskID string) (*orchestration.Task, error) {
		return nil, &orchestration.TaskNotFoundError{TenantID: tenantID, TaskID: taskID}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/tasks/missing?tenant_id=t1", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskID", "missing")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorGetTask(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
