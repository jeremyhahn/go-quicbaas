//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"testing"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Tool Registration Tests
// =============================================================================

func TestRegisterAgentTools(t *testing.T) {
	t.Parallel()

	mockSvc := &mockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(mockSvc, log)

	expectedTools := []string{
		"quicbaas_orchestrator_discover",
		"quicbaas_orchestrator_register",
		"quicbaas_orchestrator_list",
		"quicbaas_orchestrator_spawn",
		"quicbaas_orchestrator_prompt",
		"quicbaas_orchestrator_status",
		"quicbaas_orchestrator_close",
		"quicbaas_orchestrator_list_sessions",
		"quicbaas_orchestrator_list_backends",
		"quicbaas_orchestrator_decompose",
		"quicbaas_orchestrator_broadcast",
	}

	registeredNames := make(map[string]bool)
	for _, tool := range registry.tools {
		registeredNames[tool.Name] = true
	}

	for _, name := range expectedTools {
		if !registeredNames[name] {
			t.Errorf("expected tool %q to be registered", name)
		}
	}
}

// =============================================================================
// isAgentTool Tests
// =============================================================================

func TestIsAgentTool(t *testing.T) {
	t.Parallel()

	agentTools := []string{
		"quicbaas_orchestrator_discover",
		"quicbaas_orchestrator_register",
		"quicbaas_orchestrator_list",
		"quicbaas_orchestrator_spawn",
		"quicbaas_orchestrator_prompt",
		"quicbaas_orchestrator_status",
		"quicbaas_orchestrator_close",
		"quicbaas_orchestrator_list_sessions",
		"quicbaas_orchestrator_list_backends",
		"quicbaas_orchestrator_decompose",
		"quicbaas_orchestrator_broadcast",
	}

	for _, name := range agentTools {
		if !isAgentTool(name) {
			t.Errorf("expected isAgentTool(%q) to return true", name)
		}
	}

	nonAgentTools := []string{
		"quicbaas_scheduler_create_job",
		"quicbaas_ollama_embed",
		"qrdb_put",
		"nonexistent_tool",
		"",
	}

	for _, name := range nonAgentTools {
		if isAgentTool(name) {
			t.Errorf("expected isAgentTool(%q) to return false", name)
		}
	}
}

// =============================================================================
// executeAgentTool Dispatch Tests
// =============================================================================

func TestExecuteAgentTool_UnknownTool(t *testing.T) {
	t.Parallel()

	mockSvc := &mockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(mockSvc, log)

	_, err := registry.executeAgentTool(context.Background(), "quicbaas_agent_nonexistent", map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for unknown agent tool")
	}
}

func TestExecuteAgentTool_ServiceUnavailable(t *testing.T) {
	t.Parallel()

	// mockService returns nil for Agent()
	mockSvc := &mockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(mockSvc, log)

	// All agent tools should fail when service is nil
	toolsRequiringService := []struct {
		name string
		args map[string]interface{}
	}{
		{"quicbaas_orchestrator_discover", map[string]interface{}{"root_path": "/tmp"}},
		{"quicbaas_orchestrator_register", map[string]interface{}{"name": "test", "path": "/tmp"}},
		{"quicbaas_orchestrator_list", map[string]interface{}{}},
		{"quicbaas_orchestrator_spawn", map[string]interface{}{"backend": "mock", "project_id": "p1"}},
		{"quicbaas_orchestrator_prompt", map[string]interface{}{"prompt": "hello"}},
		{"quicbaas_orchestrator_status", map[string]interface{}{"session_id": "s1"}},
		{"quicbaas_orchestrator_close", map[string]interface{}{"session_id": "s1"}},
		{"quicbaas_orchestrator_list_sessions", map[string]interface{}{}},
		{"quicbaas_orchestrator_list_backends", map[string]interface{}{}},
		{"quicbaas_orchestrator_decompose", map[string]interface{}{"backend": "mock", "task": "test"}},
	}

	for _, tc := range toolsRequiringService {
		t.Run(tc.name, func(t *testing.T) {
			_, err := registry.executeAgentTool(context.Background(), tc.name, tc.args)
			if err == nil {
				t.Fatalf("expected error for tool %q when agent service is nil", tc.name)
			}
		})
	}
}

func TestExecuteAgentTool_MissingRequiredFields(t *testing.T) {
	t.Parallel()

	// Create a mock service that returns a non-nil agent service.
	// Since mockService.Agent() returns nil, these tests verify the
	// required field validation happens before the service call.
	mockSvc := &mockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(mockSvc, log)

	// Test broadcast which doesn't call getAgentService
	t.Run("broadcast_missing_channel", func(t *testing.T) {
		_, err := registry.executeAgentTool(context.Background(), "quicbaas_orchestrator_broadcast", map[string]interface{}{
			"message": "hello",
		})
		if err == nil {
			t.Fatal("expected error for missing channel")
		}
	})

	t.Run("broadcast_missing_message", func(t *testing.T) {
		_, err := registry.executeAgentTool(context.Background(), "quicbaas_orchestrator_broadcast", map[string]interface{}{
			"channel": "test-channel",
		})
		if err == nil {
			t.Fatal("expected error for missing message")
		}
	})
}

// =============================================================================
// Agent Service Mock for Tools Tests
// =============================================================================

// mockAgentService implements orchestration.AgentService for testing.
type mockAgentService struct {
	// listJobs is what ListJobs answers with; listJobsFilter records the
	// narrowing it was asked for, so a transport that drops the caller's
	// filter is caught rather than silently listing the whole tenant.
	listJobs       []*orchestration.JobStatus
	listJobsFilter *orchestration.JobFilter

	blockTaskFn       func(ctx context.Context, tenantID, taskID string, kind orchestration.TaskBlockKind, reason string) error
	getJobStatusFn    func(ctx context.Context, tenantID, jobID string) (*orchestration.JobStatus, error)
	readmitTaskFn     func(ctx context.Context, tenantID, taskID string) error
	deleteTaskFn      func(ctx context.Context, tenantID, taskID string) error
	discoverFn        func(ctx context.Context, req *orchestration.DiscoverProjectsRequest) (*orchestration.DiscoverProjectsResponse, error)
	registerFn        func(ctx context.Context, req *orchestration.RegisterProjectRequest) (*orchestration.RegisterProjectResponse, error)
	listProjectsFn    func(ctx context.Context, req *orchestration.ListProjectsRequest) (*orchestration.ListProjectsResponse, error)
	spawnFn           func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error)
	promptFn          func(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error)
	promptAsyncFn     func(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptTurnAck, error)
	statusFn          func(ctx context.Context, req *orchestration.SessionStatusRequest) (*orchestration.SessionStatusResponse, error)
	closeFn           func(ctx context.Context, req *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error)
	listSessionsFn    func(ctx context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error)
	decomposeFn       func(ctx context.Context, req *orchestration.DecomposeTaskRequest) (*orchestration.DecomposeTaskResponse, error)
	listBackendsFn    func(ctx context.Context) ([]*orchestration.AgentBackendInfo, error)
	registerBackendFn func(ctx context.Context, req *orchestration.RegisterBackendRequest) error
	inferFn           func(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error)
}

func (m *mockAgentService) DiscoverProjects(ctx context.Context, req *orchestration.DiscoverProjectsRequest) (*orchestration.DiscoverProjectsResponse, error) {
	if m.discoverFn != nil {
		return m.discoverFn(ctx, req)
	}
	return &orchestration.DiscoverProjectsResponse{}, nil
}

func (m *mockAgentService) RegisterProject(ctx context.Context, req *orchestration.RegisterProjectRequest) (*orchestration.RegisterProjectResponse, error) {
	if m.registerFn != nil {
		return m.registerFn(ctx, req)
	}
	return &orchestration.RegisterProjectResponse{ProjectID: "proj-1"}, nil
}

func (m *mockAgentService) ListProjects(ctx context.Context, req *orchestration.ListProjectsRequest) (*orchestration.ListProjectsResponse, error) {
	if m.listProjectsFn != nil {
		return m.listProjectsFn(ctx, req)
	}
	return &orchestration.ListProjectsResponse{}, nil
}

func (m *mockAgentService) SpawnSession(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
	if m.spawnFn != nil {
		return m.spawnFn(ctx, req)
	}
	return &orchestration.SpawnSessionResponse{SessionID: "sess-1"}, nil
}

func (m *mockAgentService) PromptSession(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error) {
	if m.promptFn != nil {
		return m.promptFn(ctx, req)
	}
	return &orchestration.PromptSessionResponse{}, nil
}

func (m *mockAgentService) SessionStatus(ctx context.Context, req *orchestration.SessionStatusRequest) (*orchestration.SessionStatusResponse, error) {
	if m.statusFn != nil {
		return m.statusFn(ctx, req)
	}
	return &orchestration.SessionStatusResponse{SessionID: req.SessionID}, nil
}

func (m *mockAgentService) CloseSession(ctx context.Context, req *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error) {
	if m.closeFn != nil {
		return m.closeFn(ctx, req)
	}
	return &orchestration.CloseSessionResponse{SessionID: req.SessionID}, nil
}

func (m *mockAgentService) ListSessions(ctx context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
	if m.listSessionsFn != nil {
		return m.listSessionsFn(ctx, req)
	}
	return &orchestration.ListSessionsResponse{}, nil
}

func (m *mockAgentService) DecomposeTask(ctx context.Context, req *orchestration.DecomposeTaskRequest) (*orchestration.DecomposeTaskResponse, error) {
	if m.decomposeFn != nil {
		return m.decomposeFn(ctx, req)
	}
	return &orchestration.DecomposeTaskResponse{}, nil
}

func (m *mockAgentService) ListBackends(ctx context.Context) ([]*orchestration.AgentBackendInfo, error) {
	if m.listBackendsFn != nil {
		return m.listBackendsFn(ctx)
	}
	return nil, nil
}

func (m *mockAgentService) RegisterBackend(ctx context.Context, req *orchestration.RegisterBackendRequest) error {
	if m.registerBackendFn != nil {
		return m.registerBackendFn(ctx, req)
	}
	return nil
}

func (m *mockAgentService) RegisterAPIInferenceBackend(_ context.Context, _ *orchestration.RegisterAPIInferenceBackendRequest) error {
	return nil
}

func (m *mockAgentService) HealthCheck(ctx context.Context) (*orchestration.HealthCheckReport, error) {
	return &orchestration.HealthCheckReport{}, nil
}

func (m *mockAgentService) CreateTask(ctx context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
	return &orchestration.Task{}, nil
}

func (m *mockAgentService) GetTask(ctx context.Context, tenantID, taskID string) (*orchestration.Task, error) {
	return &orchestration.Task{}, nil
}

func (m *mockAgentService) ListTasks(ctx context.Context, tenantID string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error) {
	return nil, nil
}

func (m *mockAgentService) ClaimTask(ctx context.Context, tenantID, taskID, agentID string) error {
	return nil
}

func (m *mockAgentService) CompleteTask(ctx context.Context, tenantID, taskID string, result *orchestration.TaskResult) error {
	return nil
}

func (m *mockAgentService) BlockTask(ctx context.Context, tenantID, taskID string, kind orchestration.TaskBlockKind, reason string) error {
	if m.blockTaskFn != nil {
		return m.blockTaskFn(ctx, tenantID, taskID, kind, reason)
	}
	return nil
}

func (m *mockAgentService) ReadmitTask(ctx context.Context, tenantID, taskID string) error {
	if m.readmitTaskFn != nil {
		return m.readmitTaskFn(ctx, tenantID, taskID)
	}
	return nil
}

func (m *mockAgentService) DeleteTask(ctx context.Context, tenantID, taskID string) error {
	if m.deleteTaskFn != nil {
		return m.deleteTaskFn(ctx, tenantID, taskID)
	}
	return nil
}

func (m *mockAgentService) SetTenantGuard(guard *orchestration.TenantGuard) {}

func (m *mockAgentService) AgentHeartbeat(ctx context.Context, tenantID, agentID string, _ *orchestration.AgentObservation) error {
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
	if m.getJobStatusFn != nil {
		return m.getJobStatusFn(ctx, tenantID, jobID)
	}
	return &orchestration.JobStatus{}, nil
}

func (m *mockAgentService) ListJobs(ctx context.Context, tenantID string, filter *orchestration.JobFilter) ([]*orchestration.JobStatus, error) {
	m.listJobsFilter = filter
	return m.listJobs, nil
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

func (m *mockAgentService) ListBackendModels(_ context.Context, _ string) ([]string, error) {
	return []string{}, nil
}

func (m *mockAgentService) ListWorkflows(_ context.Context) ([]*orchestration.WorkflowDefinition, error) {
	return []*orchestration.WorkflowDefinition{}, nil
}

func (m *mockAgentService) GetWorkflow(_ context.Context, name string) (*orchestration.WorkflowDefinition, error) {
	return nil, &orchestration.WorkflowNotFoundError{Name: name}
}

func (m *mockAgentService) RegisterWorkflow(_ context.Context, def *orchestration.WorkflowDefinition) error {
	return nil
}

func (m *mockAgentService) UpdateWorkflow(_ context.Context, def *orchestration.WorkflowDefinition) error {
	return nil
}

func (m *mockAgentService) DeleteWorkflow(_ context.Context, name string) error {
	return nil
}

// Infer delegates to inferFn when set; otherwise returns an empty closed channel.
func (m *mockAgentService) Infer(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
	if m.inferFn != nil {
		return m.inferFn(ctx, req)
	}
	ch := make(chan *orchestration.InferChunk)
	close(ch)
	return ch, nil
}

func (m *mockAgentService) SpawnChildJob(_ context.Context, req *orchestration.SpawnChildJobRequest) (*orchestration.SpawnChildJobResponse, error) {
	return nil, nil
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

// Compile-time check.
var _ orchestration.AgentService = (*mockAgentService)(nil)

// Helper to check if an error is a specific API error type.
func isRequiredFieldError(err error, field string) bool {
	if err == nil {
		return false
	}
	var de *api.RequiredFieldError
	if errors.As(err, &de) {
		return de.Field == field
	}
	return false
}

// PromptSessionAsync mirrors the production contract: the turn is acknowledged
// immediately and its output would flow to the session event log.
func (m *mockAgentService) PromptSessionAsync(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptTurnAck, error) {
	if m.promptAsyncFn != nil {
		return m.promptAsyncFn(ctx, req)
	}
	return &orchestration.PromptTurnAck{
		SessionID: req.SessionID,
		TurnID:    "turn-test",
		Status:    orchestration.TurnStatusAccepted,
		Sequence:  1,
	}, nil
}

// The ListPage methods answer the paged listings by running the real
// orchestration.ApplyPage over what this double already returns, so a transport test
// exercises the shipped paging contract rather than a second copy of it.
func (m *mockAgentService) ListJobsPage(ctx context.Context, tenantID string, filter *orchestration.JobFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.JobStatus], error) {
	jobs, err := m.ListJobs(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	return orchestration.ApplyPage(jobs, page, orchestration.JobPageSpec())
}

func (m *mockAgentService) ListTasksPage(ctx context.Context, tenantID string, filter *orchestration.TaskFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.Task], error) {
	tasks, err := m.ListTasks(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	return orchestration.ApplyPage(tasks, page, orchestration.TaskPageSpec())
}

func (m *mockAgentService) ListAgentsPage(ctx context.Context, tenantID string, filter *orchestration.AgentFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.AgentRegistration], error) {
	agents, err := m.ListAgents(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	return orchestration.ApplyPage(agents, page, orchestration.AgentPageSpec())
}

func (m *mockAgentService) ListWorkflowsPage(ctx context.Context, page *orchestration.Page) (*orchestration.PageResult[*orchestration.WorkflowDefinition], error) {
	workflows, err := m.ListWorkflows(ctx)
	if err != nil {
		return nil, err
	}
	return orchestration.ApplyPage(workflows, page, orchestration.WorkflowPageSpec())
}
