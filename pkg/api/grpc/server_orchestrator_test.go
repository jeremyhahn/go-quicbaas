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
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
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

	// listJobsPaged records that the PAGED verb answered the listing. Only
	// that verb fills each row's Source, so a transport that reverts to the
	// unpaged twin would answer rows with no forge issue on them.
	listJobsPaged bool
	listWorkflows []*orchestration.WorkflowDefinition

	// listAgents is what ListFleetNodes answers with.
	listAgents []*orchestration.FleetNode

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

func (*grpcTestAgentService) CloseBlockedTask(_ context.Context, _, _ string, _ *orchestration.TaskResult) error {
	return nil
}

func (m *grpcTestAgentService) BumpTaskReviewRoundBudget(_ context.Context, _, _ string) (int, error) {
	return 0, nil
}

func (m *grpcTestAgentService) DeleteTask(ctx context.Context, tenantID, taskID string) error {
	if m.deleteTaskFunc != nil {
		return m.deleteTaskFunc(ctx, tenantID, taskID)
	}
	return nil
}

func (m *grpcTestAgentService) SetTenantGuard(guard *orchestration.TenantGuard) {}

func (m *grpcTestAgentService) RegisterFleetNode(ctx context.Context, req *orchestration.RegisterFleetNodeRequest) (*orchestration.RegisterFleetNodeResponse, error) {
	return &orchestration.RegisterFleetNodeResponse{
		Node: &orchestration.FleetNode{
			ID:       "agent-1",
			TenantID: req.TenantID,
			Name:     req.Name,
		},
	}, nil
}

func (m *grpcTestAgentService) GetFleetNode(ctx context.Context, tenantID, agentID string) (*orchestration.FleetNode, error) {
	return &orchestration.FleetNode{
		ID:       agentID,
		TenantID: tenantID,
	}, nil
}

func (m *grpcTestAgentService) ListFleetNodes(ctx context.Context, tenantID string, filter *orchestration.FleetNodeFilter) ([]*orchestration.FleetNode, error) {
	return m.listAgents, nil
}

func (m *grpcTestAgentService) RemoveFleetNode(ctx context.Context, tenantID, agentID string) error {
	return nil
}

func (m *grpcTestAgentService) FleetNodeHeartbeat(ctx context.Context, tenantID, agentID string, _ *orchestration.FleetNodeObservation) error {
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
	return m.listWorkflows, nil
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

	resp, err := server.SpawnSession(tenantctx.WithSystem(context.Background()), &pb.SpawnSessionRequest{
		Backend: "claude",
		Model:   "claude-4",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "session-abc", resp.SessionId)
	assert.Equal(t, "claude", resp.Backend)
}

func TestOrchestratorGRPC_ListWorkflows_PagesAndFilters(t *testing.T) {
	agentSvc := &grpcTestAgentService{listWorkflows: []*orchestration.WorkflowDefinition{
		{Name: "alpha", Mode: orchestration.WorkflowModeCanned, ProjectID: "p1"},
		{Name: "beta", Mode: orchestration.WorkflowModeAutonomous, ProjectID: "p2"},
		{Name: "gamma", Mode: orchestration.WorkflowModeCanned, ProjectID: "p1"},
	}}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.ListWorkflows(tenantctx.WithSystem(context.Background()), &pb.OrchestratorListWorkflowsRequest{
		ProjectId: "p1",
		Mode:      string(orchestration.WorkflowModeCanned),
		Limit:     1,
		Offset:    1,
		Sort:      "name:desc",
		Q:         "a",
	})
	require.NoError(t, err)
	require.Len(t, resp.GetWorkflows(), 1)
	assert.Equal(t, "alpha", resp.GetWorkflows()[0].GetName())
	assert.Equal(t, int32(2), resp.GetTotal())
	assert.Equal(t, int32(1), resp.GetLimit())
	assert.Equal(t, int32(1), resp.GetOffset())
	assert.Zero(t, resp.GetNextOffset())
}

func TestOrchestratorGRPC_ListWorkflows_RefusesUnknownSort(t *testing.T) {
	server := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())

	_, err := server.ListWorkflows(tenantctx.WithSystem(context.Background()), &pb.OrchestratorListWorkflowsRequest{Sort: "created_at"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "created_at")
}

func TestWorkflowDefinitionToProto_PreservesFullRESTShape(t *testing.T) {
	proto := workflowDefinitionToProto(&orchestration.WorkflowDefinition{
		TenantID:    "tenant-a",
		ProjectID:   "project-a",
		Name:        "full-workflow",
		Description: "full workflow",
		Mode:        orchestration.WorkflowModeCanned,
		Agents: []orchestration.AgentRoleSpec{{
			Role:            "lead",
			Backend:         "ollama",
			Model:           "llama3.3",
			Capabilities:    []string{"plan", "code"},
			MCPScopes:       []string{"orchestrator:task_read"},
			Count:           2,
			WorkspacePath:   "/workspace",
			PermissionMode:  "default",
			SkipPermissions: true,
			Env:             map[string]string{"A": "B"},
			Sandboxed:       true,
			ContainerImage:  "agent:latest",
			HostAffinity:    "node-a",
			NumCtx:          32768,
			SystemPrompt:    "lead",
			PromptAppend:    "append",
		}},
		Settings: orchestration.WorkflowSettings{
			MaxIterations:  11,
			MaxParallelism: 3,
			PollInterval:   "2s",
			TimeoutPerItem: "5m",
			TotalTimeout:   "30m",
			ReviewCriteria: []orchestration.ReviewCriterion{{
				Name:      "tests",
				Validator: "go-test",
				Config:    "pkg/...",
				Required:  true,
				Phase:     orchestration.CriterionPhaseReviewing,
			}},
			Topology:  orchestration.TopologyPeerToPeer,
			Isolation: orchestration.IsolationWorktree,
			PeerMessaging: orchestration.PeerMessagingSettings{
				MaxMessageSize: 4096,
				MaxInboxSize:   99,
				OverflowAction: "drop_oldest",
				InboxTTL:       "1h",
			},
			FileLocking: orchestration.FileLockSettings{
				LeaseTTL:          "10s",
				HeartbeatInterval: "1s",
				MaxRenewals:       7,
			},
			VectorDB: orchestration.VectorDBSettings{
				Enabled:        true,
				IndexType:      "hnsw",
				AutoIndex:      true,
				Triggers:       []string{"artifact", "message"},
				GPUAccelerated: true,
			},
			CostControls: orchestration.CostControlSettings{
				MaxTokensPerJob:   1000,
				MaxTokensPerAgent: 250,
				BudgetAction:      "pause",
			},
			Recovery: orchestration.RecoverySettings{
				AutoRestart:    true,
				MaxRestarts:    4,
				RestartBackoff: "3s",
			},
			Verbosity: "debug",
			NumCtx:    65536,
			Hooks:     "standard",
		},
	})

	require.NotNil(t, proto)
	require.Len(t, proto.GetAgents(), 1)
	agent := proto.GetAgents()[0]
	assert.Equal(t, "lead", agent.GetSystemPrompt())
	assert.Equal(t, []string{"orchestrator:task_read"}, agent.GetMcpScopes())
	assert.Equal(t, int32(32768), agent.GetNumCtx())

	settings := proto.GetSettings()
	require.NotNil(t, settings)
	assert.Equal(t, int32(3), settings.GetMaxParallelism())
	assert.Equal(t, "2s", settings.GetPollInterval())
	assert.Equal(t, "reviewing", settings.GetReviewCriteria()[0].GetPhase())
	assert.Equal(t, int32(99), settings.GetPeerMessaging().GetMaxInboxSize())
	assert.Equal(t, int32(7), settings.GetFileLocking().GetMaxRenewals())
	assert.Equal(t, []string{"artifact", "message"}, settings.GetVectorDb().GetTriggers())
	assert.Equal(t, "pause", settings.GetCostControls().GetBudgetAction())
	assert.Equal(t, int32(4), settings.GetRecovery().GetMaxRestarts())
	assert.Equal(t, int32(65536), settings.GetNumCtx())
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

	_, err := server.SpawnSession(tenantctx.WithSystem(context.Background()), &pb.SpawnSessionRequest{
		Backend:    "claude",
		Persistent: true,
	})
	require.NoError(t, err)
	require.NotNil(t, seen)
	assert.True(t, seen.Persistent, "gRPC spawn dropped the persistent flag")

	seen = nil
	_, err = server.SpawnSession(tenantctx.WithSystem(context.Background()), &pb.SpawnSessionRequest{Backend: "claude"})
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

	resp, err := server.SpawnSession(tenantctx.WithSystem(context.Background()), &pb.SpawnSessionRequest{})

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

	resp, err := server.SpawnSession(tenantctx.WithSystem(context.Background()), &pb.SpawnSessionRequest{
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

	resp, err := server.CreateTask(tenantctx.WithTenant(context.Background(), "tenant-1"), &pb.CreateTaskRequest{
		Subject:  "implement feature",
		TenantId: "tenant-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestOrchestratorGRPC_CreateTask_MissingSubject(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.CreateTask(tenantctx.WithSystem(context.Background()), &pb.CreateTaskRequest{})

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

	resp, err := server.ListTasks(tenantctx.WithTenant(context.Background(), "tenant-1"), &pb.ListTasksRequest{
		TenantId: "tenant-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestOrchestratorGRPC_ListTasks_CarriesAttemptFilters(t *testing.T) {
	var got *orchestration.TaskFilter
	agentSvc := &grpcTestAgentService{
		listTasksFunc: func(_ context.Context, _ string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error) {
			got = filter
			return []*orchestration.Task{}, nil
		},
	}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())
	autoSpawn := false

	_, err := server.ListTasks(tenantctx.WithTenant(context.Background(), "tenant-1"), &pb.ListTasksRequest{
		TenantId: "tenant-1",
		Filter: &pb.TaskFilter{
			ProjectId: "project-1",
			AutoSpawn: &autoSpawn,
			AgentId:   "agent-1",
			JobId:     "job-1",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "project-1", got.ProjectID)
	require.NotNil(t, got.AutoSpawn)
	assert.False(t, *got.AutoSpawn)
	assert.Equal(t, "agent-1", got.AgentID)
	assert.Equal(t, "job-1", got.JobID)
}

func TestOrchestratorGRPC_ListTasks_EmptyTenant(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.ListTasks(tenantctx.WithSystem(context.Background()), &pb.ListTasksRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// =============================================================================
// Claim Task Tests
// =============================================================================

func TestOrchestratorGRPC_ClaimTask_Success(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.ClaimTask(tenantctx.WithTenant(context.Background(), "tenant-1"), &pb.ClaimTaskRequest{
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

	resp, err := server.ClaimTask(tenantctx.WithSystem(context.Background()), &pb.ClaimTaskRequest{
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

	resp, err := server.ClaimTask(tenantctx.WithSystem(context.Background()), &pb.ClaimTaskRequest{
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

	resp, err := server.Health(tenantctx.WithSystem(context.Background()), &pb.OrchestratorHealthRequest{})
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

	resp, err := server.Health(tenantctx.WithSystem(context.Background()), &pb.OrchestratorHealthRequest{})

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

	resp, err := server.ListBackends(tenantctx.WithSystem(context.Background()), &pb.OrchestratorListBackendsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// =============================================================================
// Prompt Tests
// =============================================================================

func TestOrchestratorGRPC_Prompt_Success(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.PromptSession(tenantctx.WithSystem(context.Background()), &pb.PromptSessionRequest{
		SessionId: "sess-1",
		Prompt:    "hello",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestOrchestratorGRPC_Prompt_MissingPrompt(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.PromptSession(tenantctx.WithSystem(context.Background()), &pb.PromptSessionRequest{
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

	resp, err := server.SessionStatus(tenantctx.WithSystem(context.Background()), &pb.SessionStatusRequest{
		SessionId: "sess-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestOrchestratorGRPC_SessionStatus_MissingSessionID(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.SessionStatus(tenantctx.WithSystem(context.Background()), &pb.SessionStatusRequest{})

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

	resp, err := server.CloseSession(tenantctx.WithSystem(context.Background()), &pb.CloseSessionRequest{
		SessionId: "sess-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestOrchestratorGRPC_CloseSession_MissingSessionID(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.CloseSession(tenantctx.WithSystem(context.Background()), &pb.CloseSessionRequest{})

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

	resp, err := server.ListSessions(tenantctx.WithSystem(context.Background()), &pb.ListSessionsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestOrchestratorGRPC_ListSessions_CarriesNarrowings(t *testing.T) {
	var got *orchestration.ListSessionsRequest
	agentSvc := &grpcTestAgentService{
		listSessionsFunc: func(_ context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
			got = req
			return &orchestration.ListSessionsResponse{}, nil
		},
	}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	_, err := server.ListSessions(tenantctx.WithTenant(context.Background(), "tenant-1"), &pb.ListSessionsRequest{
		TenantId:  "tenant-1",
		Backend:   "claude",
		Status:    "active",
		ProjectId: "project-1",
		JobId:     "job-1",
		Runner:    "review",
		AgentId:   "agent-1",
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "claude", got.Backend)
	assert.Equal(t, orchestration.SessionState("active"), got.Status)
	assert.Equal(t, "project-1", got.ProjectID)
	assert.Equal(t, "job-1", got.JobID)
	assert.Equal(t, "review", got.Runner)
	assert.Equal(t, "agent-1", got.AgentID)
}

// =============================================================================
// Complete Task Tests
// =============================================================================

func TestOrchestratorGRPC_CompleteTask_Success(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.CompleteTask(tenantctx.WithTenant(context.Background(), "tenant-1"), &pb.CompleteTaskRequest{
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

	resp, err := server.CompleteTask(tenantctx.WithSystem(context.Background()), &pb.CompleteTaskRequest{
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

	resp, err := server.GetTask(tenantctx.WithTenant(context.Background(), "tenant-1"), &pb.GetTaskRequest{
		TenantId: "tenant-1",
		TaskId:   "task-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestOrchestratorGRPC_GetTask_MissingTaskID(t *testing.T) {
	agentSvc := &grpcTestAgentService{}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.GetTask(tenantctx.WithSystem(context.Background()), &pb.GetTaskRequest{
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

// The ListPage methods answer the paged listings by running the real
// orchestration.ApplyPage over what this double already returns, so a transport test
// exercises the shipped paging contract rather than a second copy of it.
func (m *grpcTestAgentService) ListJobsPage(ctx context.Context, tenantID string, filter *orchestration.JobFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.JobStatus], error) {
	m.listJobsPaged = true
	jobs, err := m.ListJobs(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	return orchestration.ApplyPage(jobs, page, orchestration.JobPageSpec())
}

func (m *grpcTestAgentService) ListTasksPage(ctx context.Context, tenantID string, filter *orchestration.TaskFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.Task], error) {
	tasks, err := m.ListTasks(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	return orchestration.ApplyPage(tasks, page, orchestration.TaskPageSpec())
}

func (m *grpcTestAgentService) ListFleetNodesPage(ctx context.Context, tenantID string, filter *orchestration.FleetNodeFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.FleetNode], error) {
	agents, err := m.ListFleetNodes(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	return orchestration.ApplyPage(agents, page, orchestration.FleetNodePageSpec())
}

func (m *grpcTestAgentService) ListWorkflowsPage(ctx context.Context, filter *orchestration.WorkflowFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.WorkflowDefinition], error) {
	workflows, err := m.ListWorkflows(ctx)
	if err != nil {
		return nil, err
	}
	return orchestration.ApplyFacetedPage(workflows, page, orchestration.WorkflowPageSpec(),
		orchestration.WorkflowFacetSpec(),
		func(workflow *orchestration.WorkflowDefinition) bool {
			return filter == nil || filter.Matches(workflow)
		})
}
