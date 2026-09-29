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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func TestOrchestratorGRPC_BlockTask_Success(t *testing.T) {
	var got struct {
		tenant, task, reason string
		kind                 orchestration.TaskBlockKind
	}
	agentSvc := &grpcTestAgentService{
		blockTaskFunc: func(_ context.Context, tenantID, taskID string, kind orchestration.TaskBlockKind, reason string) error {
			got.tenant, got.task, got.kind, got.reason = tenantID, taskID, kind, reason
			return nil
		},
	}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.BlockTask(context.Background(), &pb.BlockTaskRequest{
		TenantId: "tenant-1", TaskId: "task-1", Kind: "needs_info", Reason: "which branch?",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Blocked)
	assert.Equal(t, "task-1", resp.TaskId)
	assert.Equal(t, "tenant-1", got.tenant)
	assert.Equal(t, "task-1", got.task)
	assert.Equal(t, orchestration.TaskBlockKindNeedsInfo, got.kind)
	assert.Equal(t, "which branch?", got.reason)
}

func TestOrchestratorGRPC_BlockTask_MissingFields(t *testing.T) {
	server := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	cases := []struct {
		name string
		req  *pb.BlockTaskRequest
	}{
		{"task id", &pb.BlockTaskRequest{Kind: "blocked", Reason: "r"}},
		{"kind", &pb.BlockTaskRequest{TaskId: "t", Reason: "r"}},
		{"reason", &pb.BlockTaskRequest{TaskId: "t", Kind: "blocked"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := server.BlockTask(context.Background(), tc.req)
			assert.Nil(t, resp)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, codes.InvalidArgument, st.Code())
		})
	}
}

func TestOrchestratorGRPC_BlockTask_ServiceError(t *testing.T) {
	agentSvc := &grpcTestAgentService{
		blockTaskFunc: func(context.Context, string, string, orchestration.TaskBlockKind, string) error {
			return &orchestration.TaskInvalidStateError{TaskID: "t", CurrentState: orchestration.TaskStatusCompleted, TargetState: orchestration.TaskStatusBlocked}
		},
	}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())
	resp, err := server.BlockTask(context.Background(), &pb.BlockTaskRequest{TaskId: "t", Kind: "blocked", Reason: "r"})
	assert.Nil(t, resp)
	require.Error(t, err)
	_, ok := status.FromError(err)
	assert.True(t, ok, "service errors must be converted to gRPC status")
}

func TestOrchestratorGRPC_ReadmitTask_Success(t *testing.T) {
	var gotTenant, gotTask string
	agentSvc := &grpcTestAgentService{
		readmitTaskFunc: func(_ context.Context, tenantID, taskID string) error {
			gotTenant, gotTask = tenantID, taskID
			return nil
		},
	}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())
	resp, err := server.ReadmitTask(context.Background(), &pb.ReadmitTaskRequest{TenantId: "tenant-1", TaskId: "task-1"})
	require.NoError(t, err)
	assert.True(t, resp.Readmitted)
	assert.Equal(t, "task-1", resp.TaskId)
	assert.Equal(t, "tenant-1", gotTenant)
	assert.Equal(t, "task-1", gotTask)
}

func TestOrchestratorGRPC_ReadmitTask_Errors(t *testing.T) {
	t.Run("missing task id", func(t *testing.T) {
		server := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
		resp, err := server.ReadmitTask(context.Background(), &pb.ReadmitTaskRequest{TenantId: "t"})
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
	t.Run("service error", func(t *testing.T) {
		agentSvc := &grpcTestAgentService{
			readmitTaskFunc: func(context.Context, string, string) error {
				return &orchestration.TaskNotFoundError{TenantID: "t", TaskID: "x"}
			},
		}
		server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())
		resp, err := server.ReadmitTask(context.Background(), &pb.ReadmitTaskRequest{TenantId: "t", TaskId: "x"})
		assert.Nil(t, resp)
		require.Error(t, err)
	})
	// A refused transition is the caller's condition, never Internal, and the
	// message names both states (go-quicbaas#945).
	t.Run("refused transition is FailedPrecondition naming both states", func(t *testing.T) {
		agentSvc := &grpcTestAgentService{
			readmitTaskFunc: func(context.Context, string, string) error {
				return &orchestration.TaskInvalidStateError{TaskID: "x", CurrentState: orchestration.TaskStatusCompleted, TargetState: orchestration.TaskStatusPending}
			},
		}
		server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())
		resp, err := server.ReadmitTask(context.Background(), &pb.ReadmitTaskRequest{TenantId: "t", TaskId: "x"})
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.FailedPrecondition, st.Code())
		assert.Contains(t, st.Message(), "current=completed")
		assert.Contains(t, st.Message(), "target=pending")
	})
}

func TestOrchestratorGRPC_ListTasks_ProjectAndStatusesFilter(t *testing.T) {
	var got *orchestration.TaskFilter
	agentSvc := &grpcTestAgentService{
		listTasksFunc: func(_ context.Context, _ string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error) {
			got = filter
			return []*orchestration.Task{{
				ID: "t1", TenantID: "tenant-1", Status: orchestration.TaskStatusBlocked, ProjectID: "proj-1",
				Source:        &orchestration.TaskSourceRef{Kind: orchestration.TaskSourceKindOperator, Ref: "42", URL: "https://example.invalid/42"},
				Priority:      3,
				Attempts:      2,
				AttemptBudget: 5,
				BlockKind:     orchestration.TaskBlockKindEscalated,
				BlockedReason: "budget",
			}}, nil
		},
	}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.ListTasks(context.Background(), &pb.ListTasksRequest{
		TenantId: "tenant-1",
		Filter: &pb.TaskFilter{
			ProjectId: "proj-1",
			Statuses:  []pb.TaskStatus{pb.TaskStatus_TASK_STATUS_BLOCKED, pb.TaskStatus_TASK_STATUS_UNSPECIFIED, pb.TaskStatus_TASK_STATUS_PENDING},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "proj-1", got.ProjectID)
	assert.Equal(t, []orchestration.TaskStatus{orchestration.TaskStatusBlocked, orchestration.TaskStatusPending}, got.Statuses)

	require.Len(t, resp.Tasks, 1)
	task := resp.Tasks[0]
	assert.Equal(t, pb.TaskStatus_TASK_STATUS_BLOCKED, task.Status)
	assert.Equal(t, "proj-1", task.ProjectId)
	require.NotNil(t, task.Source)
	assert.Equal(t, "operator", task.Source.Kind)
	assert.Equal(t, "42", task.Source.Ref)
	assert.Equal(t, int64(3), task.Priority)
	assert.Equal(t, int64(2), task.Attempts)
	assert.Equal(t, int64(5), task.AttemptBudget)
	assert.Equal(t, "escalated", task.BlockKind)
	assert.Equal(t, "budget", task.BlockedReason)
}

func TestOrchestratorGRPC_TaskStatusBlocked_RoundTrips(t *testing.T) {
	assert.Equal(t, pb.TaskStatus_TASK_STATUS_BLOCKED, serviceTaskStatusToProto(orchestration.TaskStatusBlocked))
	assert.Equal(t, orchestration.TaskStatusBlocked, protoTaskStatusToService(pb.TaskStatus_TASK_STATUS_BLOCKED))
	assert.Nil(t, protoTaskStatusesToService(nil))
	assert.Nil(t, protoTaskStatusesToService([]pb.TaskStatus{pb.TaskStatus_TASK_STATUS_UNSPECIFIED}))
	assert.Nil(t, protoTaskSourceToService(nil))
	assert.Nil(t, serviceTaskToProto(&orchestration.Task{}).Source)
}

func TestOrchestratorGRPC_CreateTask_CarriesSourcePriorityBudget(t *testing.T) {
	var got *orchestration.CreateTaskRequest
	agentSvc := &grpcTestAgentService{
		createTaskFunc: func(_ context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
			got = req
			return &orchestration.Task{ID: "t1", TenantID: req.TenantID, Subject: req.Subject, Source: req.Source}, nil
		},
	}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())
	resp, err := server.CreateTask(context.Background(), &pb.CreateTaskRequest{
		TenantId: "tenant-1", Subject: "s",
		Source:        &pb.TaskSource{Kind: "operator", Ref: "alice", Url: "u"},
		Priority:      9,
		AttemptBudget: 4,
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Source)
	assert.Equal(t, orchestration.TaskSourceKindOperator, got.Source.Kind)
	assert.Equal(t, "alice", got.Source.Ref)
	assert.Equal(t, "u", got.Source.URL)
	assert.Equal(t, 9, got.Priority)
	assert.Equal(t, 4, got.AttemptBudget)
	require.NotNil(t, resp.Task.Source)
	assert.Equal(t, "alice", resp.Task.Source.Ref)
}

func TestOrchestratorGRPC_JobStatus_CarriesTaskBindingAndSessions(t *testing.T) {
	agentSvc := &grpcTestAgentService{
		getJobStatusFunc: func(_ context.Context, _, jobID string) (*orchestration.JobStatus, error) {
			return &orchestration.JobStatus{
				JobID: jobID, State: orchestration.JobStateExecuting,
				TaskID: "task-1", Attempt: 2, Node: "node-a", HeartbeatAt: 123, FailureClass: "",
				LeadSessionID: "lead", SessionIDs: []string{"lead", "w1"},
			}, nil
		},
	}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())
	resp, err := server.GetJobStatus(context.Background(), &pb.OrchestratorGetJobStatusRequest{TenantId: "t", JobId: "job-1"})
	require.NoError(t, err)
	assert.Equal(t, "task-1", resp.TaskId)
	assert.Equal(t, int64(2), resp.Attempt)
	assert.Equal(t, "node-a", resp.Node)
	assert.Equal(t, int64(123), resp.HeartbeatAt)
	assert.Equal(t, "lead", resp.LeadSessionId)
	assert.Equal(t, []string{"lead", "w1"}, resp.SessionIds)
}

func TestOrchestratorGRPC_StartJob_CarriesTaskBinding(t *testing.T) {
	var got *orchestration.StartJobRequest
	agentSvc := &grpcTestAgentService{
		startJobFunc: func(_ context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
			got = req
			return &orchestration.StartJobResponse{JobID: "job-1"}, nil
		},
	}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())
	_, err := server.StartJob(context.Background(), &pb.OrchestratorStartJobRequest{TenantId: "t", Description: "d", TaskId: "task-1", Attempt: 3})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "task-1", got.TaskID)
	assert.Equal(t, 3, got.Attempt)
}

// TestOrchestratorGRPC_PromptAppendCrossesTheWire: run-time prompt material
// reaches the service on a spawn, a job and a task, and a task's and a job's
// come back on their records.
func TestOrchestratorGRPC_PromptAppendCrossesTheWire(t *testing.T) {
	var spawn *orchestration.SpawnSessionRequest
	var start *orchestration.StartJobRequest
	var create *orchestration.CreateTaskRequest
	agentSvc := &grpcTestAgentService{
		spawnSessionFunc: func(_ context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
			spawn = req
			return &orchestration.SpawnSessionResponse{SessionID: "s1"}, nil
		},
		startJobFunc: func(_ context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
			start = req
			return &orchestration.StartJobResponse{JobID: "job-1"}, nil
		},
		createTaskFunc: func(_ context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
			create = req
			return &orchestration.Task{ID: "t1", TenantID: req.TenantID, Subject: req.Subject, PromptAppend: req.PromptAppend}, nil
		},
		getJobStatusFunc: func(_ context.Context, _, jobID string) (*orchestration.JobStatus, error) {
			return &orchestration.JobStatus{JobID: jobID, State: orchestration.JobStateExecuting, PromptAppend: "job material"}, nil
		},
	}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())
	ctx := context.Background()

	_, err := server.SpawnSession(ctx, &pb.SpawnSessionRequest{AgentId: "claude-coder", PromptAppend: "spawn material"})
	require.NoError(t, err)
	_, err = server.StartJob(ctx, &pb.OrchestratorStartJobRequest{TenantId: "t", Description: "d", PromptAppend: "job material"})
	require.NoError(t, err)
	task, err := server.CreateTask(ctx, &pb.CreateTaskRequest{TenantId: "t", Subject: "s", PromptAppend: "task material"})
	require.NoError(t, err)
	status, err := server.GetJobStatus(ctx, &pb.OrchestratorGetJobStatusRequest{TenantId: "t", JobId: "job-1"})
	require.NoError(t, err)

	assert.Equal(t, "spawn material", spawn.PromptAppend)
	assert.Equal(t, "job material", start.PromptAppend)
	assert.Equal(t, "task material", create.PromptAppend)
	assert.Equal(t, "task material", task.GetTask().GetPromptAppend())
	assert.Equal(t, "job material", status.GetPromptAppend())
}
