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

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Mock Agent Service
// =============================================================================

// grpcTestAgentService implements orchestration.AgentService for gRPC testing.
type grpcTestAgentService struct {
	// listJobs is what ListJobs answers with; listJobsFilter records the
	// narrowing it was asked for, so a transport that drops the caller's
	// filter is caught rather than silently listing the whole tenant.
	listJobs       []*orchestration.JobStatus
	listJobsFilter *orchestration.JobFilter

	createTaskFunc    func(ctx context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error)
	startJobFunc      func(ctx context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error)
	blockTaskFunc     func(ctx context.Context, tenantID, taskID string, kind orchestration.TaskBlockKind, reason string) error
	readmitTaskFunc   func(ctx context.Context, tenantID, taskID string) error
	deleteTaskFunc    func(ctx context.Context, tenantID, taskID string) error
	listTasksFunc     func(ctx context.Context, tenantID string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error)
	getJobStatusFunc  func(ctx context.Context, tenantID, jobID string) (*orchestration.JobStatus, error)
	spawnSessionFunc  func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error)
	promptSessionFunc func(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error)
	sessionStatusFunc func(ctx context.Context, req *orchestration.SessionStatusRequest) (*orchestration.SessionStatusResponse, error)
	closeSessionFunc  func(ctx context.Context, req *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error)
	listSessionsFunc  func(ctx context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error)
	listBackendsFunc  func(ctx context.Context) ([]*orchestration.AgentBackendInfo, error)
	healthCheckFunc   func(ctx context.Context) (*orchestration.HealthCheckReport, error)
	inferFunc         func(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error)
}

func (m *grpcTestAgentService) DiscoverProjects(ctx context.Context, req *orchestration.DiscoverProjectsRequest) (*orchestration.DiscoverProjectsResponse, error) {
	return &orchestration.DiscoverProjectsResponse{Timestamp: time.Now().Unix()}, nil
}

func (m *grpcTestAgentService) RegisterProject(ctx context.Context, req *orchestration.RegisterProjectRequest) (*orchestration.RegisterProjectResponse, error) {
	return &orchestration.RegisterProjectResponse{ProjectID: "proj-1", Timestamp: time.Now().Unix()}, nil
}

func (m *grpcTestAgentService) ListProjects(ctx context.Context, req *orchestration.ListProjectsRequest) (*orchestration.ListProjectsResponse, error) {
	return &orchestration.ListProjectsResponse{Timestamp: time.Now().Unix()}, nil
}

func (m *grpcTestAgentService) SpawnSession(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
	if m.spawnSessionFunc != nil {
		return m.spawnSessionFunc(ctx, req)
	}
	return &orchestration.SpawnSessionResponse{
		SessionID: "session-abc",
		Backend:   req.Backend,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *grpcTestAgentService) PromptSession(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error) {
	if m.promptSessionFunc != nil {
		return m.promptSessionFunc(ctx, req)
	}
	return &orchestration.PromptSessionResponse{
		SessionID: req.SessionID,
		Content:   []orchestration.ContentBlock{{Type: "text", Text: "response"}},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *grpcTestAgentService) SessionStatus(ctx context.Context, req *orchestration.SessionStatusRequest) (*orchestration.SessionStatusResponse, error) {
	if m.sessionStatusFunc != nil {
		return m.sessionStatusFunc(ctx, req)
	}
	return &orchestration.SessionStatusResponse{
		SessionID: req.SessionID,
		State:     orchestration.SessionStateIdle,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *grpcTestAgentService) CloseSession(ctx context.Context, req *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error) {
	if m.closeSessionFunc != nil {
		return m.closeSessionFunc(ctx, req)
	}
	return &orchestration.CloseSessionResponse{
		SessionID: req.SessionID,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *grpcTestAgentService) ListSessions(ctx context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
	if m.listSessionsFunc != nil {
		return m.listSessionsFunc(ctx, req)
	}
	return &orchestration.ListSessionsResponse{Timestamp: time.Now().Unix()}, nil
}

func (m *grpcTestAgentService) DecomposeTask(ctx context.Context, req *orchestration.DecomposeTaskRequest) (*orchestration.DecomposeTaskResponse, error) {
	return &orchestration.DecomposeTaskResponse{Timestamp: time.Now().Unix()}, nil
}

func (m *grpcTestAgentService) ListBackends(ctx context.Context) ([]*orchestration.AgentBackendInfo, error) {
	if m.listBackendsFunc != nil {
		return m.listBackendsFunc(ctx)
	}
	return []*orchestration.AgentBackendInfo{{Name: "claude", Type: "acp"}}, nil
}

func (m *grpcTestAgentService) RegisterBackend(ctx context.Context, req *orchestration.RegisterBackendRequest) error {
	return nil
}

func (m *grpcTestAgentService) RegisterAPIInferenceBackend(_ context.Context, _ *orchestration.RegisterAPIInferenceBackendRequest) error {
	return nil
}

func (m *grpcTestAgentService) HealthCheck(ctx context.Context) (*orchestration.HealthCheckReport, error) {
	if m.healthCheckFunc != nil {
		return m.healthCheckFunc(ctx)
	}
	return &orchestration.HealthCheckReport{
		Timestamp:     time.Now().Unix(),
		TotalSessions: 0,
		Healthy:       0,
	}, nil
}

func (m *grpcTestAgentService) CreateTask(ctx context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
	if m.createTaskFunc != nil {
		return m.createTaskFunc(ctx, req)
	}
	return &orchestration.Task{}, nil
}

func (m *grpcTestAgentService) GetTask(ctx context.Context, tenantID, taskID string) (*orchestration.Task, error) {
	return &orchestration.Task{}, nil
}

func (m *grpcTestAgentService) ListTasks(ctx context.Context, tenantID string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error) {
	if m.listTasksFunc != nil {
		return m.listTasksFunc(ctx, tenantID, filter)
	}
	return nil, nil
}

func (m *grpcTestAgentService) ClaimTask(ctx context.Context, tenantID, taskID, agentID string) error {
	return nil
}

func (m *grpcTestAgentService) CompleteTask(ctx context.Context, tenantID, taskID string, result *orchestration.TaskResult) error {
	return nil
}

func (m *grpcTestAgentService) BlockTask(ctx context.Context, tenantID, taskID string, kind orchestration.TaskBlockKind, reason string) error {
	if m.blockTaskFunc != nil {
		return m.blockTaskFunc(ctx, tenantID, taskID, kind, reason)
	}
	return nil
}

func (m *grpcTestAgentService) ReadmitTask(ctx context.Context, tenantID, taskID string) error {
	if m.readmitTaskFunc != nil {
		return m.readmitTaskFunc(ctx, tenantID, taskID)
	}
	return nil
}

func (m *grpcTestAgentService) DeleteTask(ctx context.Context, tenantID, taskID string) error {
	if m.deleteTaskFunc != nil {
		return m.deleteTaskFunc(ctx, tenantID, taskID)
	}
	return nil
}

func (m *grpcTestAgentService) SetTenantGuard(guard *orchestration.TenantGuard) {}

func (m *grpcTestAgentService) RegisterAgent(ctx context.Context, req *orchestration.RegisterAgentRequest) (*orchestration.RegisterAgentResponse, error) {
	return &orchestration.RegisterAgentResponse{
		Agent: &orchestration.AgentRegistration{
			ID:       "agent-1",
			TenantID: req.TenantID,
			Name:     req.Name,
		},
	}, nil
}

func (m *grpcTestAgentService) GetAgent(ctx context.Context, tenantID, agentID string) (*orchestration.AgentRegistration, error) {
	return &orchestration.AgentRegistration{
		ID:       agentID,
		TenantID: tenantID,
	}, nil
}

func (m *grpcTestAgentService) ListAgents(ctx context.Context, tenantID string, filter *orchestration.AgentFilter) ([]*orchestration.AgentRegistration, error) {
	return nil, nil
}

func (m *grpcTestAgentService) RemoveAgent(ctx context.Context, tenantID, agentID string) error {
	return nil
}

func (m *grpcTestAgentService) AgentHeartbeat(ctx context.Context, tenantID, agentID string, _ *orchestration.AgentObservation) error {
	return nil
}

func (m *grpcTestAgentService) StartJob(ctx context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
	if m.startJobFunc != nil {
		return m.startJobFunc(ctx, req)
	}
	return &orchestration.StartJobResponse{JobID: "test-job"}, nil
}

func (m *grpcTestAgentService) GetJobStatus(ctx context.Context, tenantID, jobID string) (*orchestration.JobStatus, error) {
	if m.getJobStatusFunc != nil {
		return m.getJobStatusFunc(ctx, tenantID, jobID)
	}
	return &orchestration.JobStatus{JobID: jobID, State: "completed"}, nil
}

func (m *grpcTestAgentService) ListJobs(ctx context.Context, tenantID string, filter *orchestration.JobFilter) ([]*orchestration.JobStatus, error) {
	m.listJobsFilter = filter
	return m.listJobs, nil
}

func (m *grpcTestAgentService) CancelJob(ctx context.Context, tenantID, jobID string) error {
	return nil
}

func (m *grpcTestAgentService) SubscribeJobEvents(ctx context.Context, tenantID, jobID string) (<-chan *orchestration.JobEvent, error) {
	return nil, nil
}

func (m *grpcTestAgentService) BackendToolGuidance(_ string) string {
	return ""
}

func (m *grpcTestAgentService) ListBackendModels(_ context.Context, _ string) ([]string, error) {
	return []string{}, nil
}

func (m *grpcTestAgentService) ListWorkflows(_ context.Context) ([]*orchestration.WorkflowDefinition, error) {
	return []*orchestration.WorkflowDefinition{}, nil
}

func (m *grpcTestAgentService) GetWorkflow(_ context.Context, name string) (*orchestration.WorkflowDefinition, error) {
	return nil, &orchestration.WorkflowNotFoundError{Name: name}
}

func (m *grpcTestAgentService) RegisterWorkflow(_ context.Context, def *orchestration.WorkflowDefinition) error {
	return nil
}

func (m *grpcTestAgentService) UpdateWorkflow(_ context.Context, def *orchestration.WorkflowDefinition) error {
	return nil
}

func (m *grpcTestAgentService) DeleteWorkflow(_ context.Context, name string) error {
	return nil
}

func (m *grpcTestAgentService) SpawnChildJob(_ context.Context, _ *orchestration.SpawnChildJobRequest) (*orchestration.SpawnChildJobResponse, error) {
	return &orchestration.SpawnChildJobResponse{ChildJobID: "child-job-1"}, nil
}

func (m *grpcTestAgentService) SubmitTaskArtifacts(_ context.Context, _, _ string, _ []orchestration.Artifact) error {
	return nil
}

func (m *grpcTestAgentService) GetTaskArtifact(_ context.Context, _, _, _ string) (*orchestration.Artifact, error) {
	return nil, nil
}

func (m *grpcTestAgentService) ListTaskArtifacts(_ context.Context, _, _ string) ([]orchestration.Artifact, error) {
	return nil, nil
}

// Infer calls inferFunc if set, otherwise returns an empty closed channel.
func (m *grpcTestAgentService) Infer(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
	if m.inferFunc != nil {
		return m.inferFunc(ctx, req)
	}
	ch := make(chan *orchestration.InferChunk)
	close(ch)
	return ch, nil
}

// =============================================================================
// Test Helper
// =============================================================================

func createOrchestratorGRPCTestLogger() *logger.Logger {
	cfg := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "grpc-orchestrator-test",
	}
	log, err := logger.New(cfg)
	if err != nil {
		panic(err)
	}
	return log
}

// =============================================================================
// Spawn Tests
// =============================================================================

func TestOrchestratorGRPC_Spawn_Success(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.SpawnSession(context.Background(), &pb.SpawnSessionRequest{
		Backend: "claude",
		Model:   "claude-4",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "session-abc", resp.SessionId)
	assert.Equal(t, "claude", resp.Backend)
}

// TestOrchestratorGRPC_Spawn_CarriesPersistentFlag pins the flag that decides
// whether a session holds a long-running agent process. Dropping it here made
// every gRPC-spawned session one-shot: no conversation context across turns,
// and nothing to resume after a daemon restart.
func TestOrchestratorGRPC_Spawn_CarriesPersistentFlag(t *testing.T) {
	var seen *orchestration.SpawnSessionRequest
	agentSvc := &grpcTestAgentService{
		spawnSessionFunc: func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
			seen = req
			return &orchestration.SpawnSessionResponse{SessionID: "session-abc", Backend: req.Backend}, nil
		},
	}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	_, err := server.SpawnSession(context.Background(), &pb.SpawnSessionRequest{
		Backend:    "claude",
		Persistent: true,
	})
	require.NoError(t, err)
	require.NotNil(t, seen)
	assert.True(t, seen.Persistent, "gRPC spawn dropped the persistent flag")

	seen = nil
	_, err = server.SpawnSession(context.Background(), &pb.SpawnSessionRequest{Backend: "claude"})
	require.NoError(t, err)
	require.NotNil(t, seen)
	assert.False(t, seen.Persistent, "gRPC spawn invented a persistent flag the caller did not set")
}

// TestOrchestratorGRPC_SessionStateConversion pins the resumable state across
// the proto boundary: a session restored after a restart must not surface to a
// gRPC client as UNSPECIFIED.
func TestOrchestratorGRPC_SessionStateConversion(t *testing.T) {
	cases := map[orchestration.SessionState]pb.SessionState{
		orchestration.SessionStateActive:    pb.SessionState_SESSION_STATE_ACTIVE,
		orchestration.SessionStateIdle:      pb.SessionState_SESSION_STATE_IDLE,
		orchestration.SessionStateResumable: pb.SessionState_SESSION_STATE_RESUMABLE,
		orchestration.SessionStateClosed:    pb.SessionState_SESSION_STATE_CLOSED,
		orchestration.SessionStateError:     pb.SessionState_SESSION_STATE_ERROR,
		orchestration.SessionState("bogus"): pb.SessionState_SESSION_STATE_UNSPECIFIED,
	}
	for in, want := range cases {
		assert.Equal(t, want, serviceSessionStateToProto(in), "state %q", in)
	}
}

func TestOrchestratorGRPC_Spawn_MissingBackend(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.SpawnSession(context.Background(), &pb.SpawnSessionRequest{})

	assert.Nil(t, resp)
	assert.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestOrchestratorGRPC_Spawn_ServiceError(t *testing.T) {
	agentSvc := &grpcTestAgentService{
		spawnSessionFunc: func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
			return nil, orchestration.ErrAgentServiceUnavailable
		},
	}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.SpawnSession(context.Background(), &pb.SpawnSessionRequest{
		Backend: "claude",
	})

	assert.Nil(t, resp)
	assert.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unavailable, st.Code())
}

// =============================================================================
// Create Task Tests
// =============================================================================

func TestOrchestratorGRPC_CreateTask_Success(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.CreateTask(context.Background(), &pb.CreateTaskRequest{
		Subject:  "implement feature",
		TenantId: "tenant-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestOrchestratorGRPC_CreateTask_MissingSubject(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.CreateTask(context.Background(), &pb.CreateTaskRequest{})

	assert.Nil(t, resp)
	assert.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// =============================================================================
// List Tasks Tests
// =============================================================================

func TestOrchestratorGRPC_ListTasks_Success(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.ListTasks(context.Background(), &pb.ListTasksRequest{
		TenantId: "tenant-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestOrchestratorGRPC_ListTasks_EmptyTenant(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.ListTasks(context.Background(), &pb.ListTasksRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// =============================================================================
// Claim Task Tests
// =============================================================================

func TestOrchestratorGRPC_ClaimTask_Success(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.ClaimTask(context.Background(), &pb.ClaimTaskRequest{
		TenantId: "tenant-1",
		TaskId:   "task-1",
		AgentId:  "agent-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, true, resp.Claimed)
}

func TestOrchestratorGRPC_ClaimTask_MissingTaskID(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.ClaimTask(context.Background(), &pb.ClaimTaskRequest{
		AgentId: "agent-1",
	})

	assert.Nil(t, resp)
	assert.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestOrchestratorGRPC_ClaimTask_MissingAgentID(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.ClaimTask(context.Background(), &pb.ClaimTaskRequest{
		TaskId: "task-1",
	})

	assert.Nil(t, resp)
	assert.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// =============================================================================
// Health Tests
// =============================================================================

func TestOrchestratorGRPC_Health_Success(t *testing.T) {
	agentSvc := &grpcTestAgentService{
		healthCheckFunc: func(ctx context.Context) (*orchestration.HealthCheckReport, error) {
			return &orchestration.HealthCheckReport{
				Timestamp:     time.Now().Unix(),
				TotalSessions: 3,
				Healthy:       2,
				Unhealthy:     1,
			}, nil
		},
	}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.Health(context.Background(), &pb.OrchestratorHealthRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Report)
}

func TestOrchestratorGRPC_Health_ServiceError(t *testing.T) {
	agentSvc := &grpcTestAgentService{
		healthCheckFunc: func(ctx context.Context) (*orchestration.HealthCheckReport, error) {
			return nil, orchestration.ErrAgentServiceUnavailable
		},
	}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.Health(context.Background(), &pb.OrchestratorHealthRequest{})

	assert.Nil(t, resp)
	assert.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unavailable, st.Code())
}

// =============================================================================
// List Backends Tests
// =============================================================================

func TestOrchestratorGRPC_ListBackends_Success(t *testing.T) {
	agentSvc := &grpcTestAgentService{
		listBackendsFunc: func(ctx context.Context) ([]*orchestration.AgentBackendInfo, error) {
			return []*orchestration.AgentBackendInfo{
				{Name: "claude", Type: "acp"},
				{Name: "ollama", Type: "ollama"},
			}, nil
		},
	}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.ListBackends(context.Background(), &pb.OrchestratorListBackendsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// =============================================================================
// Prompt Tests
// =============================================================================

func TestOrchestratorGRPC_Prompt_Success(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.PromptSession(context.Background(), &pb.PromptSessionRequest{
		SessionId: "sess-1",
		Prompt:    "hello",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestOrchestratorGRPC_Prompt_MissingPrompt(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.PromptSession(context.Background(), &pb.PromptSessionRequest{
		SessionId: "sess-1",
	})

	assert.Nil(t, resp)
	assert.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// =============================================================================
// Session Status Tests
// =============================================================================

func TestOrchestratorGRPC_SessionStatus_Success(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.SessionStatus(context.Background(), &pb.SessionStatusRequest{
		SessionId: "sess-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestOrchestratorGRPC_SessionStatus_MissingSessionID(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.SessionStatus(context.Background(), &pb.SessionStatusRequest{})

	assert.Nil(t, resp)
	assert.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// =============================================================================
// Close Session Tests
// =============================================================================

func TestOrchestratorGRPC_CloseSession_Success(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.CloseSession(context.Background(), &pb.CloseSessionRequest{
		SessionId: "sess-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestOrchestratorGRPC_CloseSession_MissingSessionID(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.CloseSession(context.Background(), &pb.CloseSessionRequest{})

	assert.Nil(t, resp)
	assert.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// =============================================================================
// List Sessions Tests
// =============================================================================

func TestOrchestratorGRPC_ListSessions_Success(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.ListSessions(context.Background(), &pb.ListSessionsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// =============================================================================
// Complete Task Tests
// =============================================================================

func TestOrchestratorGRPC_CompleteTask_Success(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.CompleteTask(context.Background(), &pb.CompleteTaskRequest{
		TenantId: "tenant-1",
		TaskId:   "task-1",
		Result:   &pb.TaskResult{Output: "done"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestOrchestratorGRPC_CompleteTask_MissingTaskID(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.CompleteTask(context.Background(), &pb.CompleteTaskRequest{
		TenantId: "tenant-1",
	})

	assert.Nil(t, resp)
	assert.Error(t, err)
}

// =============================================================================
// Get Task Tests
// =============================================================================

func TestOrchestratorGRPC_GetTask_Success(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.GetTask(context.Background(), &pb.GetTaskRequest{
		TenantId: "tenant-1",
		TaskId:   "task-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestOrchestratorGRPC_GetTask_MissingTaskID(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.GetTask(context.Background(), &pb.GetTaskRequest{
		TenantId: "tenant-1",
	})

	assert.Nil(t, resp)
	assert.Error(t, err)
}

// PromptSessionAsync mirrors the production contract: the turn is acknowledged
// immediately and its output would flow to the session event log.
func (m *grpcTestAgentService) PromptSessionAsync(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptTurnAck, error) {
	return &orchestration.PromptTurnAck{
		SessionID: req.SessionID,
		TurnID:    "turn-test",
		Status:    orchestration.TurnStatusAccepted,
		Sequence:  1,
	}, nil
}

// TestTaskToProto_CarriesQuestionAttempts keeps a refund counter from being
// invented on the server and then dropped on the wire.
//
// THAT EXACT GAP COST A DAY: ProjectBinding.Reviewer and TaskArtifact.Ref both
// existed in the domain and had no wire representation, so the CLI reported a
// reviewer that was configured as absent and could not decode a single task.
// A counter the operator cannot see is a counter that will be disbelieved.
func TestTaskToProto_CarriesQuestionAttempts(t *testing.T) {
	task := &orchestration.Task{
		ID: "t1", Attempts: 9, EnvironmentFaults: 2, QuestionAttempts: 5,
	}
	pb := serviceTaskToProto(task)
	if pb.QuestionAttempts != 5 {
		t.Fatalf("question_attempts = %d, want 5", pb.QuestionAttempts)
	}
	if pb.EnvironmentFaults != 2 {
		t.Fatalf("environment_faults = %d, want 2; the two must stay distinct", pb.EnvironmentFaults)
	}
	if pb.Attempts != 9 {
		t.Fatalf("attempts = %d, want 9", pb.Attempts)
	}
}
