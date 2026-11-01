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
	"sort"
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
	// listJobs is what ListJobs answers with; listJobsFilter records the
	// narrowing it was asked for, so a transport that drops the caller's
	// filter is caught rather than silently listing the whole tenant.
	listJobs       []*orchestration.JobStatus
	listJobsFilter *orchestration.JobFilter

	// listJobsPageFilter and listTasksPageFilter record the WHOLE filter the
	// handler built, before the service splits off the counted half. It is
	// the assertion that matters for a transport: listJobsFilter sees only
	// what survives into the store read, so a handler that dropped `runner`
	// — a counted narrowing — would look identical there.
	listJobsPageFilter  *orchestration.JobFilter
	listTasksPageFilter *orchestration.TaskFilter

	// listAgents, listWorkflows and listSessions back the fleet, workflow
	// and session listings the same way listJobs backs the job listing, so
	// a paging test has records to page through.
	listAgents    []*orchestration.FleetNode
	listAgentsErr error
	listWorkflows []*orchestration.WorkflowDefinition
	listSessions  []*orchestration.SessionInfo

	blockTaskFunc        func(ctx context.Context, tenantID, taskID string, kind orchestration.TaskBlockKind, reason string) error
	readmitTaskFunc      func(ctx context.Context, tenantID, taskID string) error
	deleteTaskFunc       func(ctx context.Context, tenantID, taskID string) error
	discoverProjectsFunc func(ctx context.Context, req *orchestration.DiscoverProjectsRequest) (*orchestration.DiscoverProjectsResponse, error)
	registerProjectFunc  func(ctx context.Context, req *orchestration.RegisterProjectRequest) (*orchestration.RegisterProjectResponse, error)
	listProjectsFunc     func(ctx context.Context, req *orchestration.ListProjectsRequest) (*orchestration.ListProjectsResponse, error)
	spawnSessionFunc     func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error)
	agentHeartbeatFunc   func(ctx context.Context, tenantID, agentID string, observed *orchestration.FleetNodeObservation) error
	promptSessionFunc    func(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error)
	promptAsyncFunc      func(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptTurnAck, error)
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
	updateWorkflowFunc   func(def *orchestration.WorkflowDefinition) error
	deleteWorkflowFunc   func(name string) error
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

// ListSessions pages the canned records through the real ApplyPage, exactly
// as the service does, so the sessions endpoint's envelope is the shipped one
// under test rather than a second implementation of it.
func (m *mockAgentService) ListSessions(ctx context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
	if m.listSessionsFunc != nil {
		return m.listSessionsFunc(ctx, req)
	}
	// The BASE narrowings — the project and the job — are the ones the
	// service applies while it walks its records; the rest are counted
	// facets and run inside ApplyFacetedPage, where the counts can see them.
	matched := make([]*orchestration.SessionInfo, 0, len(m.listSessions))
	for _, session := range m.listSessions {
		if req.ProjectID != "" && session.ProjectID != req.ProjectID {
			continue
		}
		if req.JobID != "" && session.JobID != req.JobID {
			continue
		}
		matched = append(matched, session)
	}
	paged, err := orchestration.ApplyFacetedPage(matched, req.Page, orchestration.SessionPageSpec(),
		orchestration.SessionFacetSpec(),
		func(session *orchestration.SessionInfo) bool {
			return orchestration.MatchesSessionFacets(req, session)
		})
	if err != nil {
		return nil, err
	}
	return &orchestration.ListSessionsResponse{
		Sessions:   paged.Items,
		Timestamp:  time.Now().Unix(),
		Total:      paged.Total,
		Limit:      paged.Limit,
		Offset:     paged.Offset,
		NextOffset: paged.NextOffset,
		Facets:     paged.Facets,
	}, nil
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

func (m *mockAgentService) RegisterAPIInferenceBackend(_ context.Context, _ *orchestration.RegisterAPIInferenceBackendRequest) error {
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

func (m *mockAgentService) BlockTask(ctx context.Context, tenantID, taskID string, kind orchestration.TaskBlockKind, reason string) error {
	if m.blockTaskFunc != nil {
		return m.blockTaskFunc(ctx, tenantID, taskID, kind, reason)
	}
	return nil
}

func (m *mockAgentService) ReadmitTask(ctx context.Context, tenantID, taskID string) error {
	if m.readmitTaskFunc != nil {
		return m.readmitTaskFunc(ctx, tenantID, taskID)
	}
	return nil
}

func (*mockAgentService) CloseBlockedTask(_ context.Context, _, _ string, _ *orchestration.TaskResult) error {
	return nil
}

func (m *mockAgentService) BumpTaskReviewRoundBudget(_ context.Context, _, _ string) (int, error) {
	return 0, nil
}

func (m *mockAgentService) DeleteTask(ctx context.Context, tenantID, taskID string) error {
	if m.deleteTaskFunc != nil {
		return m.deleteTaskFunc(ctx, tenantID, taskID)
	}
	return nil
}

func (m *mockAgentService) CompleteTask(ctx context.Context, tenantID, taskID string, result *orchestration.TaskResult) error {
	if m.completeTaskFunc != nil {
		return m.completeTaskFunc(ctx, tenantID, taskID, result)
	}
	return nil
}

func (m *mockAgentService) FleetNodeHeartbeat(ctx context.Context, tenantID, agentID string, observed *orchestration.FleetNodeObservation) error {
	if m.agentHeartbeatFunc != nil {
		return m.agentHeartbeatFunc(ctx, tenantID, agentID, observed)
	}
	return nil
}

func (m *mockAgentService) RegisterFleetNode(ctx context.Context, req *orchestration.RegisterFleetNodeRequest) (*orchestration.RegisterFleetNodeResponse, error) {
	return nil, nil
}

func (m *mockAgentService) GetFleetNode(ctx context.Context, tenantID, agentID string) (*orchestration.FleetNode, error) {
	return nil, nil
}

func (m *mockAgentService) ListFleetNodes(_ context.Context, _ string, _ *orchestration.FleetNodeFilter) ([]*orchestration.FleetNode, error) {
	if m.listAgentsErr != nil {
		return nil, m.listAgentsErr
	}
	return m.listAgents, nil
}

func (m *mockAgentService) RemoveFleetNode(ctx context.Context, tenantID, agentID string) error {
	return nil
}

func (m *mockAgentService) StartJob(ctx context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
	return &orchestration.StartJobResponse{}, nil
}

func (m *mockAgentService) GetJobStatus(ctx context.Context, tenantID, jobID string) (*orchestration.JobStatus, error) {
	return &orchestration.JobStatus{}, nil
}

// ListJobs applies the REAL JobFilter to the canned records rather than
// handing back everything: a handler that drops the caller's narrowing must
// fail the test, and the narrowing that decides is the shipped one.
func (m *mockAgentService) ListJobs(_ context.Context, _ string, filter *orchestration.JobFilter) ([]*orchestration.JobStatus, error) {
	m.listJobsFilter = filter
	matched := make([]*orchestration.JobStatus, 0, len(m.listJobs))
	for _, job := range m.listJobs {
		if filter.Matches(job) {
			matched = append(matched, job)
		}
	}
	return matched, nil
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

// Infer is a no-op stub satisfying the orchestration.AgentService interface.
func (m *mockAgentService) Infer(_ context.Context, _ *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
	ch := make(chan *orchestration.InferChunk)
	close(ch)
	return ch, nil
}

func (m *mockAgentService) ListWorkflows(_ context.Context) ([]*orchestration.WorkflowDefinition, error) {
	return m.listWorkflows, nil
}

func (m *mockAgentService) GetWorkflow(_ context.Context, name string) (*orchestration.WorkflowDefinition, error) {
	return nil, &orchestration.WorkflowNotFoundError{Name: name}
}

func (m *mockAgentService) RegisterWorkflow(_ context.Context, def *orchestration.WorkflowDefinition) error {
	return nil
}

func (m *mockAgentService) UpdateWorkflow(_ context.Context, def *orchestration.WorkflowDefinition) error {
	if m.updateWorkflowFunc != nil {
		return m.updateWorkflowFunc(def)
	}
	return nil
}

func (m *mockAgentService) DeleteWorkflow(_ context.Context, name string) error {
	if m.deleteWorkflowFunc != nil {
		return m.deleteWorkflowFunc(name)
	}
	return nil
}

func (m *mockAgentService) SpawnChildJob(_ context.Context, _ *orchestration.SpawnChildJobRequest) (*orchestration.SpawnChildJobResponse, error) {
	return &orchestration.SpawnChildJobResponse{ChildJobID: "child-job-1"}, nil
}

func (m *mockAgentService) SubmitTaskArtifacts(_ context.Context, _, _ string, _ []orchestration.Artifact) error {
	return nil
}

func (m *mockAgentService) GetTaskArtifact(_ context.Context, _, _, _ string) (*orchestration.Artifact, error) {
	return nil, nil
}

func (m *mockAgentService) ListTaskArtifacts(_ context.Context, _, _ string) ([]orchestration.Artifact, error) {
	return nil, nil
}

func (m *mockAgentService) ListBackendModels(_ context.Context, backendName string) ([]string, error) {
	switch backendName {
	case "ollama":
		return []string{"llama3.2:latest", "qwen3:1.7b"}, nil
	case "claude":
		return []string{"claude-sonnet-4-7", "claude-opus-4-7"}, nil
	default:
		return []string{}, nil
	}
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

	req := systemRequest(http.MethodPost, "/api/v1/orchestrator/sessions", bytes.NewReader(body))
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

// TestOrchestratorSpawn_Persistent proves the sessions endpoint defaults
// Persistent=true when the body omits it, and honours an explicit value
// either way, so callers that want one-shot behaviour can still get it.
func TestOrchestratorSpawn_Persistent(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "omitted defaults to true", body: `{"backend":"claude"}`, want: true},
		{name: "explicit false is honoured", body: `{"backend":"claude","persistent":false}`, want: false},
		{name: "explicit true is honoured", body: `{"backend":"claude","persistent":true}`, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, agentSvc, cleanup := setupOrchestratorMockServer(t)
			defer cleanup()

			var captured *orchestration.SpawnSessionRequest
			agentSvc.spawnSessionFunc = func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
				captured = req
				return &orchestration.SpawnSessionResponse{SessionID: "s1", Backend: req.Backend}, nil
			}

			req := systemRequest(http.MethodPost, "/api/v1/orchestrator/sessions", bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			server.HandleOrchestratorSpawn(rec, req)

			assert.Equal(t, http.StatusCreated, rec.Code)
			require.NotNil(t, captured)
			assert.Equal(t, tc.want, captured.Persistent)
		})
	}
}

// TestOrchestratorSpawn_Errors maps each spawn failure to its HTTP status.
// The UnsupportedOperation and BackendNotFound rows are Bug #3 regressions:
// both once fell through to an opaque 500 "internal server error", and the
// body must now carry the actionable message instead.
func TestOrchestratorSpawn_Errors(t *testing.T) {
	tests := []struct {
		name        string
		noAgent     bool
		spawnErr    error
		body        string
		wantStatus  int
		wantContain []string
	}{
		{name: "invalid payload", body: "invalid-json", wantStatus: http.StatusBadRequest},
		{name: "missing backend", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "service unavailable", noAgent: true, body: `{"backend":"claude"}`, wantStatus: http.StatusServiceUnavailable},
		{
			name: "service error",
			spawnErr: &orchestration.AgentServiceError{
				Sentinel: orchestration.ErrBackendRequired,
				Message:  "backend not found",
			},
			body:       `{"backend":"invalid"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:        "unsupported operation",
			spawnErr:    &orchestration.UnsupportedOperationError{Backend: "ollama", Operation: "spawn"},
			body:        `{"backend":"ollama"}`,
			wantStatus:  http.StatusBadRequest,
			wantContain: []string{"does not support spawn"},
		},
		{
			name:        "backend not found",
			spawnErr:    &orchestration.BackendNotFoundError{Backend: "ollama"},
			body:        `{"backend":"ollama"}`,
			wantStatus:  http.StatusNotFound,
			wantContain: []string{"backend not found", "ollama"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var server *Server
			if tc.noAgent {
				// The default mock returns nil for Agent().
				s, _, cleanup := setupMockServer(t)
				defer cleanup()
				server = s
			} else {
				s, agentSvc, cleanup := setupOrchestratorMockServer(t)
				defer cleanup()
				if tc.spawnErr != nil {
					agentSvc.spawnSessionFunc = func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
						return nil, tc.spawnErr
					}
				}
				server = s
			}

			req := systemRequest(http.MethodPost, "/api/v1/orchestrator/sessions", bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			server.HandleOrchestratorSpawn(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			respBody := rec.Body.String()
			for _, want := range tc.wantContain {
				assert.Contains(t, respBody, want)
			}
			if tc.spawnErr != nil {
				assert.NotContains(t, respBody, "internal server error")
			}
		})
	}
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

	req := systemRequest(http.MethodPost, "/api/v1/orchestrator/tasks", bytes.NewReader(body))
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

// TestOrchestratorCreateTask_Errors maps each create-task failure to its
// HTTP status.
func TestOrchestratorCreateTask_Errors(t *testing.T) {
	tests := []struct {
		name       string
		noAgent    bool
		createErr  error
		body       string
		wantStatus int
	}{
		{name: "invalid payload", body: "not-json", wantStatus: http.StatusBadRequest},
		{name: "missing subject", body: `{"description":"no subject"}`, wantStatus: http.StatusBadRequest},
		{name: "service unavailable", noAgent: true, body: `{"subject":"test"}`, wantStatus: http.StatusServiceUnavailable},
		{
			name:       "task already exists",
			createErr:  &orchestration.TaskAlreadyExistsError{TaskID: "task-dup"},
			body:       `{"subject":"duplicate"}`,
			wantStatus: http.StatusConflict,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var server *Server
			if tc.noAgent {
				// The default mock returns nil for Agent().
				s, _, cleanup := setupMockServer(t)
				defer cleanup()
				server = s
			} else {
				s, agentSvc, cleanup := setupOrchestratorMockServer(t)
				defer cleanup()
				if tc.createErr != nil {
					agentSvc.createTaskFunc = func(ctx context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
						return nil, tc.createErr
					}
				}
				server = s
			}

			req := systemRequest(http.MethodPost, "/api/v1/orchestrator/tasks", bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			server.HandleOrchestratorCreateTask(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}
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

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/tasks?tenant_id=t1", nil)

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

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/tasks?tenant_id=t1&status=pending&team_name=alpha", nil)

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

	req := systemRequest(http.MethodPost, "/api/v1/orchestrator/tasks/task-1/claim?tenant_id=t1", bytes.NewReader(body))
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

	req := systemRequest(http.MethodPost, "/api/v1/orchestrator/tasks/task-1/claim", bytes.NewReader(body))
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

	req := systemRequest(http.MethodPost, "/api/v1/orchestrator/tasks/task-1/claim?tenant_id=t1", bytes.NewReader(body))
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

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/health", nil)

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

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/health", nil)

	rec := httptest.NewRecorder()
	server.HandleOrchestratorHealth(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// =============================================================================
// Prompt Tests
// =============================================================================

// TestOrchestratorPrompt_Success pins the asynchronous turn contract: the
// handler answers 202 Accepted with the turn's identity, never the agent's
// output. A 200 carrying content would mean the handler had blocked on the
// agent, which no REST request can survive.
func TestOrchestratorPrompt_Success(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.promptAsyncFunc = func(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptTurnAck, error) {
		return &orchestration.PromptTurnAck{
			SessionID: req.SessionID,
			TurnID:    "turn-1",
			Status:    orchestration.TurnStatusAccepted,
			Sequence:  7,
			Timestamp: time.Now().UnixNano(),
		}, nil
	}

	body, err := json.Marshal(orchestration.PromptSessionRequest{
		Prompt: "say hello",
	})
	require.NoError(t, err)

	req := systemRequest(http.MethodPost, "/api/v1/orchestrator/sessions/sess-1/prompt", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionID", "sess-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorPrompt(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)

	var ack orchestration.PromptTurnAck
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &ack))
	assert.Equal(t, "sess-1", ack.SessionID)
	assert.Equal(t, "turn-1", ack.TurnID)
	assert.Equal(t, orchestration.TurnStatusAccepted, ack.Status)
	assert.Equal(t, uint64(7), ack.Sequence)
}

func TestOrchestratorPrompt_MissingPrompt(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	body, err := json.Marshal(orchestration.PromptSessionRequest{})
	require.NoError(t, err)

	req := systemRequest(http.MethodPost, "/api/v1/orchestrator/sessions/sess-1/prompt", bytes.NewReader(body))
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

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/backends", nil)

	rec := httptest.NewRecorder()
	server.HandleOrchestratorListBackends(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// =============================================================================
// List Runners Tests
// =============================================================================

// TestOrchestratorListRunners_ReportsRegisteredRunners proves the endpoint
// serves the registry rather than a copy of it: the engine's own "workflow"
// runner is present with the states it declares, in the order it declares
// them, and session-backed states are marked as such. This is what the job
// detail stepper reads instead of hardcoding the engine's thirteen states.
func TestOrchestratorListRunners_ReportsRegisteredRunners(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/runners", nil)
	rec := httptest.NewRecorder()
	server.HandleOrchestratorListRunners(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got OrchestratorListRunnersResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, len(got.Runners), got.Count)
	require.NotEmpty(t, got.Runners)

	byName := make(map[string]OrchestratorJobRunnerResponse, len(got.Runners))
	for _, runner := range got.Runners {
		byName[runner.Name] = runner
	}
	workflow, ok := byName[orchestration.JobRunnerWorkflow]
	require.True(t, ok, "the engine's own runner must be registered: %v", byName)

	// The response mirrors what the runner itself declares — state, label and
	// session-backed flag, in declaration order.
	declared, found := orchestration.LookupJobRunner(orchestration.JobRunnerWorkflow)
	require.True(t, found)
	specs := declared.States()
	require.Len(t, workflow.States, len(specs))
	for i, spec := range specs {
		assert.Equal(t, string(spec.State), workflow.States[i].State)
		assert.Equal(t, spec.Label, workflow.States[i].Label)
		assert.Equal(t, spec.SessionBacked, workflow.States[i].SessionBacked)
	}
	assert.True(t, sort.SliceIsSorted(got.Runners, func(i, j int) bool {
		return got.Runners[i].Name < got.Runners[j].Name
	}), "runners must be ordered by name so two reads serialize identically")
}

// TestOrchestratorListRunners_SessionBackedIsAlwaysSerialized proves the
// flag is on the wire even when false. A consumer deciding whether a state
// has a session to address must not have to infer the default from a key
// that omitempty dropped.
func TestOrchestratorListRunners_SessionBackedIsAlwaysSerialized(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/runners", nil)
	rec := httptest.NewRecorder()
	server.HandleOrchestratorListRunners(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var raw struct {
		Runners []struct {
			States []map[string]any `json:"states"`
		} `json:"runners"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	require.NotEmpty(t, raw.Runners)
	for _, runner := range raw.Runners {
		require.NotEmpty(t, runner.States)
		for _, state := range runner.States {
			assert.Contains(t, state, "session_backed")
			assert.Contains(t, state, "state")
			assert.Contains(t, state, "label")
		}
	}
}

// TestOrchestratorListRunners_WithoutOrchestratorAnswers503 proves a node
// running no orchestrator does not claim to drive runners. The registry is
// package-level and would answer regardless, which is exactly why the gate
// is explicit.
func TestOrchestratorListRunners_WithoutOrchestratorAnswers503(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/runners", nil)
	rec := httptest.NewRecorder()
	server.HandleOrchestratorListRunners(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
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

	req := systemRequest(http.MethodPost, "/api/v1/orchestrator/discover", bytes.NewReader(body))
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

	req := systemRequest(http.MethodPost, "/api/v1/orchestrator/discover", bytes.NewReader(body))
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

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/sessions/sess-1", nil)

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

	req := systemRequest(http.MethodDelete, "/api/v1/orchestrator/sessions/sess-1", nil)

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

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/sessions", nil)

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

	req := systemRequest(http.MethodPost, "/api/v1/orchestrator/tasks/task-1/complete?tenant_id=t1", bytes.NewReader(body))
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

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/tasks/task-1?tenant_id=t1", nil)

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

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/tasks/missing?tenant_id=t1", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskID", "missing")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorGetTask(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// =============================================================================
// Workflow Handler Tests
// =============================================================================

func TestHandleOrchestratorListWorkflows_Empty(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/workflows", nil)
	rec := httptest.NewRecorder()
	server.HandleOrchestratorListWorkflows(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	wfs, ok := body["workflows"]
	require.True(t, ok, "response must contain 'workflows' key")
	// Should be an empty array, not null.
	require.NotNil(t, wfs)
}

func TestHandleOrchestratorListWorkflows_NoAgent(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	// Remove agent service.
	server.service = testutil.NewMockQuicBaaS()

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/workflows", nil)
	rec := httptest.NewRecorder()
	server.HandleOrchestratorListWorkflows(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHandleOrchestratorGetWorkflow_NotFound(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/workflows/does-not-exist", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "does-not-exist")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorGetWorkflow(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleOrchestratorGetWorkflow_MissingName(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/orchestrator/workflows/", nil)
	rctx := chi.NewRouteContext()
	// Do not set "name" URL param — simulates a missing name.
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorGetWorkflow(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleOrchestratorCreateWorkflow_Success(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	body := map[string]interface{}{
		"name": "test-workflow",
		"definition": map[string]interface{}{
			"name":        "test-workflow",
			"description": "A test workflow",
			"mode":        "autonomous",
			"agents":      []interface{}{},
			"settings": map[string]interface{}{
				"max_iterations": 10,
			},
		},
	}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req := systemRequest(http.MethodPost, "/api/v1/orchestrator/workflows", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.HandleOrchestratorCreateWorkflow(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestHandleOrchestratorCreateWorkflow_MissingName(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	body := map[string]interface{}{
		"definition": map[string]interface{}{
			"mode": "autonomous",
		},
	}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req := systemRequest(http.MethodPost, "/api/v1/orchestrator/workflows", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.HandleOrchestratorCreateWorkflow(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleOrchestratorCreateWorkflow_MissingDefinition(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	body := map[string]interface{}{
		"name": "test-workflow",
	}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req := systemRequest(http.MethodPost, "/api/v1/orchestrator/workflows", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.HandleOrchestratorCreateWorkflow(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleOrchestratorCreateWorkflow_InvalidJSON(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	req := systemRequest(http.MethodPost, "/api/v1/orchestrator/workflows", bytes.NewReader([]byte(`{invalid`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.HandleOrchestratorCreateWorkflow(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// =============================================================================
// Update Workflow Tests
// =============================================================================

// TestHandleUpdateWorkflow_Success verifies a valid PUT request updates the workflow.
func TestHandleUpdateWorkflow_Success(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	updated := &orchestration.WorkflowDefinition{
		Name:        "my-workflow",
		Description: "Updated description",
		Mode:        "autonomous",
	}
	agentSvc.updateWorkflowFunc = func(def *orchestration.WorkflowDefinition) error {
		*updated = *def
		return nil
	}

	body, err := json.Marshal(updated)
	require.NoError(t, err)

	req := systemRequest(http.MethodPut, "/api/v1/orchestrator/workflows/my-workflow", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "my-workflow")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorUpdateWorkflow(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err = json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.NotNil(t, resp["workflow"])
}

// retiredSystemPromptDefinition is a workflow definition whose worker still
// sets the retired system_prompt.
const retiredSystemPromptDefinition = `{"name":"my-workflow","mode":"autonomous",` +
	`"agents":[{"role":"worker","backend":"claude","system_prompt":"be careful"}]}`

func TestHandleUpdateWorkflow_RefusesRetiredSystemPrompt(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.updateWorkflowFunc = func(*orchestration.WorkflowDefinition) error {
		t.Fatal("a definition carrying a retired field must not reach the service")
		return nil
	}

	req := systemRequest(http.MethodPut, "/api/v1/orchestrator/workflows/my-workflow",
		bytes.NewReader([]byte(retiredSystemPromptDefinition)))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "my-workflow")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	server.HandleOrchestratorUpdateWorkflow(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "agents[0].system_prompt")
}

func TestHandleOrchestratorCreateWorkflow_RefusesRetiredSystemPrompt(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	body := `{"name":"my-workflow","definition":` + retiredSystemPromptDefinition + `}`
	req := systemRequest(http.MethodPost, "/api/v1/orchestrator/workflows", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.HandleOrchestratorCreateWorkflow(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "agents[0].system_prompt")
}

// TestHandleUpdateWorkflow_NotFound verifies that a missing workflow returns 404.
func TestHandleUpdateWorkflow_NotFound(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.updateWorkflowFunc = func(def *orchestration.WorkflowDefinition) error {
		return &orchestration.WorkflowNotFoundError{Name: def.Name}
	}

	body, err := json.Marshal(orchestration.WorkflowDefinition{
		Name: "ghost",
		Mode: "autonomous",
	})
	require.NoError(t, err)

	req := systemRequest(http.MethodPut, "/api/v1/orchestrator/workflows/ghost", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "ghost")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorUpdateWorkflow(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// =============================================================================
// Delete Workflow Tests
// =============================================================================

// TestHandleDeleteWorkflow_Success verifies a valid DELETE request removes the workflow.
func TestHandleDeleteWorkflow_Success(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	req := systemRequest(http.MethodDelete, "/api/v1/orchestrator/workflows/my-workflow", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "my-workflow")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorDeleteWorkflow(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, true, resp["deleted"])
}

// TestHandleDeleteWorkflow_NotFound verifies that deleting a missing workflow returns 404.
func TestHandleDeleteWorkflow_NotFound(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.deleteWorkflowFunc = func(name string) error {
		return &orchestration.WorkflowNotFoundError{Name: name}
	}

	req := systemRequest(http.MethodDelete, "/api/v1/orchestrator/workflows/ghost", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "ghost")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorDeleteWorkflow(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// =============================================================================
// ListBackendModels Tests
// =============================================================================

// TestHandleOrchestratorListBackendModels_OllamaSuccess verifies that the
// handler returns the model list for a known ollama backend.
func TestHandleOrchestratorListBackendModels_OllamaSuccess(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/fleet/gpu-host/backends/ollama/models", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("nodeID", "gpu-host")
	rctx.URLParams.Add("backendName", "ollama")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleListFleetNodeBackendModels(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, "ollama", resp["backend"])
	models, ok := resp["models"].([]interface{})
	require.True(t, ok, "models should be an array")
	assert.Contains(t, []string{models[0].(string), models[1].(string)}, "qwen3:1.7b")
}

// TestHandleOrchestratorListBackendModels_MissingBackend verifies that
// the handler returns 400 when no backend name is provided.
func TestHandleOrchestratorListBackendModels_MissingBackend(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/fleet/gpu-host/backends//models", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("nodeID", "gpu-host")
	// backendName is intentionally omitted to trigger validation.
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleListFleetNodeBackendModels(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandleOrchestratorListBackendModels_UnknownReturnsEmpty verifies that an
// unregistered backend name returns 200 with an empty model list (not an error),
// matching the mock's default behaviour.
func TestHandleOrchestratorListBackendModels_UnknownReturnsEmpty(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	req := systemRequest(http.MethodGet, "/api/v1/fleet/any/backends/unknown/models", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("nodeID", "any")
	rctx.URLParams.Add("backendName", "unknown")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	server.HandleListFleetNodeBackendModels(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	models, ok := resp["models"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, models)
}

// =============================================================================
// CreateTask with HostAffinity + GpuRequired Tests
// =============================================================================

// TestOrchestratorCreateTask_WithHostAffinityAndGPU verifies that host_affinity and
// gpu_required fields are parsed and passed through to the service layer.
func TestOrchestratorCreateTask_WithHostAffinityAndGPU(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	var captured *orchestration.CreateTaskRequest
	agentSvc.createTaskFunc = func(ctx context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
		captured = req
		return &orchestration.Task{
			ID:       "task-gpu-01",
			TenantID: req.TenantID,
			Subject:  req.Subject,
			Metadata: req.Metadata,
		}, nil
	}

	body := `{
		"tenant_id": "default",
		"subject": "Train ML model",
		"host_affinity": "gpu-node-01",
		"gpu_required": true
	}`
	req := systemRequest(http.MethodPost, "/api/v1/orchestrator/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleOrchestratorCreateTask(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.NotNil(t, captured, "createTaskFunc must have been called")

	// Verify the REST handler decoded and forwarded the new fields.
	assert.Equal(t, "gpu-node-01", captured.HostAffinity)
	assert.True(t, captured.GpuRequired)
}

// Ensure time import doesn't get removed.
var _ = time.Second

// PromptSessionAsync mirrors the production contract: the turn is acknowledged
// immediately and its output would flow to the session event log.
func (m *mockAgentService) PromptSessionAsync(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptTurnAck, error) {
	if m.promptAsyncFunc != nil {
		return m.promptAsyncFunc(ctx, req)
	}
	return &orchestration.PromptTurnAck{
		SessionID: req.SessionID,
		TurnID:    "turn-test",
		Status:    orchestration.TurnStatusAccepted,
		Sequence:  1,
	}, nil
}

// The ListPage methods answer the paged listings by running the REAL
// orchestration.ApplyFacetedPage — the same spec, the same facet counts and
// the same narrowing predicate the service uses — over what this double
// already returns. A transport test therefore exercises the shipped contract
// rather than a second copy of it, and a handler that drops a filter on the
// floor fails here rather than in production.
func (m *mockAgentService) ListJobsPage(ctx context.Context, tenantID string, filter *orchestration.JobFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.JobStatus], error) {
	m.listJobsPageFilter = filter
	jobs, err := m.ListJobs(ctx, tenantID, filter.FacetBase())
	if err != nil {
		return nil, err
	}
	return orchestration.ApplyFacetedPage(jobs, page, orchestration.JobPageSpec(),
		orchestration.JobFacetSpec(), filter.MatchesFacets)
}

func (m *mockAgentService) ListTasksPage(ctx context.Context, tenantID string, filter *orchestration.TaskFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.Task], error) {
	m.listTasksPageFilter = filter
	tasks, err := m.ListTasks(ctx, tenantID, filter.FacetBase())
	if err != nil {
		return nil, err
	}
	return orchestration.ApplyFacetedPage(tasks, page, orchestration.TaskPageSpec(),
		orchestration.TaskFacetSpec(), filter.MatchesFacets)
}

func (m *mockAgentService) ListFleetNodesPage(ctx context.Context, tenantID string, filter *orchestration.FleetNodeFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.FleetNode], error) {
	agents, err := m.ListFleetNodes(ctx, tenantID, filter.FacetBase())
	if err != nil {
		return nil, err
	}
	return orchestration.ApplyFacetedPage(agents, page, orchestration.FleetNodePageSpec(),
		orchestration.FleetNodeFacetSpec(), filter.MatchesFacets)
}

func (m *mockAgentService) ListWorkflowsPage(ctx context.Context, filter *orchestration.WorkflowFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.WorkflowDefinition], error) {
	workflows, err := m.ListWorkflows(ctx)
	if err != nil {
		return nil, err
	}
	return orchestration.ApplyFacetedPage(workflows, page, orchestration.WorkflowPageSpec(),
		orchestration.WorkflowFacetSpec(), filter.Matches)
}
