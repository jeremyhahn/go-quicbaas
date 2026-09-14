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

// =============================================================================
// Job operations
// =============================================================================

func TestOrchestratorGRPC_StartJob(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := context.Background()

	resp, err := srv.StartJob(ctx, &pb.OrchestratorStartJobRequest{
		TenantId: "t1", Description: "build the thing", MaxIterations: 3,
	})
	require.NoError(t, err)
	assert.Equal(t, "test-job", resp.JobId)

	_, err = srv.StartJob(ctx, &pb.OrchestratorStartJobRequest{TenantId: "t1"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestOrchestratorGRPC_GetJobStatus(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := context.Background()

	resp, err := srv.GetJobStatus(ctx, &pb.OrchestratorGetJobStatusRequest{TenantId: "t1", JobId: "j1"})
	require.NoError(t, err)
	assert.Equal(t, "j1", resp.JobId)
	assert.Equal(t, "completed", resp.State)

	_, err = srv.GetJobStatus(ctx, &pb.OrchestratorGetJobStatusRequest{TenantId: "t1"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestOrchestratorGRPC_CancelJob(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := context.Background()

	resp, err := srv.CancelJob(ctx, &pb.OrchestratorCancelJobRequest{TenantId: "t1", JobId: "j1"})
	require.NoError(t, err)
	assert.True(t, resp.Cancelled)

	_, err = srv.CancelJob(ctx, &pb.OrchestratorCancelJobRequest{TenantId: "t1"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestOrchestratorGRPC_ListJobs(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	resp, err := srv.ListJobs(context.Background(), &pb.OrchestratorListJobsRequest{TenantId: "t1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// TestOrchestratorGRPC_ListJobs_NarrowsToTask pins the narrowing this
// transport was ignoring: task_id reaches the service, which is the only
// place it can be applied honestly. go-quicbaas#822.
func TestOrchestratorGRPC_ListJobs_NarrowsToTask(t *testing.T) {
	svc := &grpcTestAgentService{listJobs: []*orchestration.JobStatus{
		{JobID: "job-1", TaskID: "task-a", Attempt: 1},
	}}
	srv := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())

	resp, err := srv.ListJobs(context.Background(), &pb.OrchestratorListJobsRequest{
		TenantId: "t1", TaskId: "task-a",
	})
	require.NoError(t, err)
	require.NotNil(t, svc.listJobsFilter, "the task_id on the request never reached the service")
	assert.Equal(t, "task-a", svc.listJobsFilter.TaskID)
	require.Len(t, resp.GetJobs(), 1)
	assert.Equal(t, "task-a", resp.GetJobs()[0].GetTaskId())
	assert.Equal(t, int64(1), resp.GetJobs()[0].GetAttempt())
}

// TestOrchestratorGRPC_ListJobs_WithoutTaskNarrowsNothing is the other half:
// an absent task_id must list the tenant, not nothing.
func TestOrchestratorGRPC_ListJobs_WithoutTaskNarrowsNothing(t *testing.T) {
	svc := &grpcTestAgentService{}
	srv := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())

	_, err := srv.ListJobs(context.Background(), &pb.OrchestratorListJobsRequest{TenantId: "t1"})
	require.NoError(t, err)
	require.NotNil(t, svc.listJobsFilter)
	assert.True(t, svc.listJobsFilter.Empty(), "an absent task_id must narrow nothing")
}

func TestOrchestratorGRPC_DecomposeTask(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := context.Background()

	resp, err := srv.DecomposeTask(ctx, &pb.DecomposeTaskRequest{
		TenantId: "t1", Backend: "claude", Task: "refactor",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	_, err = srv.DecomposeTask(ctx, &pb.DecomposeTaskRequest{TenantId: "t1"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// =============================================================================
// Agent fleet operations
// =============================================================================

func TestOrchestratorGRPC_RegisterAgent(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	resp, err := srv.RegisterAgent(context.Background(), &pb.RegisterAgentRequest{
		TenantId: "t1", Name: "agent-a", Host: "h1", AgentType: "claude",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Agent)
	assert.Equal(t, "agent-1", resp.Agent.Id)
}

func TestOrchestratorGRPC_GetAgent(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := context.Background()

	resp, err := srv.GetAgent(ctx, &pb.GetAgentRequest{TenantId: "t1", AgentId: "a1"})
	require.NoError(t, err)
	assert.Equal(t, "a1", resp.Agent.Id)

	_, err = srv.GetAgent(ctx, &pb.GetAgentRequest{TenantId: "t1"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestOrchestratorGRPC_ListAgents(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := context.Background()

	// With filter to exercise the filter conversion branch.
	resp, err := srv.ListAgents(ctx, &pb.ListAgentsRequest{
		TenantId: "t1",
		Filter:   &pb.AgentFilter{Host: "h1", AgentType: "claude", Backend: "acp", Limit: 10},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Without filter.
	resp, err = srv.ListAgents(ctx, &pb.ListAgentsRequest{TenantId: "t1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestOrchestratorGRPC_RemoveAgent(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := context.Background()

	resp, err := srv.RemoveAgent(ctx, &pb.RemoveAgentRequest{TenantId: "t1", AgentId: "a1"})
	require.NoError(t, err)
	assert.True(t, resp.Removed)

	_, err = srv.RemoveAgent(ctx, &pb.RemoveAgentRequest{TenantId: "t1"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestOrchestratorGRPC_AgentHeartbeat(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := context.Background()

	resp, err := srv.AgentHeartbeat(ctx, &pb.AgentHeartbeatRequest{TenantId: "t1", AgentId: "a1"})
	require.NoError(t, err)
	assert.True(t, resp.Acknowledged)

	_, err = srv.AgentHeartbeat(ctx, &pb.AgentHeartbeatRequest{TenantId: "t1"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// =============================================================================
// Child job + artifacts
// =============================================================================

func TestOrchestratorGRPC_SpawnChildJob(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := context.Background()

	resp, err := srv.SpawnChildJob(ctx, &pb.SpawnChildJobRequest{
		TenantId: "t1", ParentJobId: "parent-1",
		Tasks: []*pb.TaskSpec{{Title: "sub", Description: "do sub"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "child-job-1", resp.ChildJobId)

	_, err = srv.SpawnChildJob(ctx, &pb.SpawnChildJobRequest{TenantId: "t1"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestOrchestratorGRPC_SubmitTaskArtifacts(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := context.Background()

	resp, err := srv.SubmitTaskArtifacts(ctx, &pb.SubmitTaskArtifactsRequest{
		TenantId: "t1", TaskId: "tk1",
		Artifacts: []*pb.Artifact{{
			Kind: "file", Name: "out.txt", MimeType: "text/plain",
			Content: []byte("hello"), Ref: &pb.ArtifactRef{AgentId: "a1", Path: "/tmp/out.txt"},
		}},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), resp.Stored)

	_, err = srv.SubmitTaskArtifacts(ctx, &pb.SubmitTaskArtifactsRequest{TenantId: "t1"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestOrchestratorGRPC_ListTaskArtifacts(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := context.Background()

	resp, err := srv.ListTaskArtifacts(ctx, &pb.ListTaskArtifactsRequest{TenantId: "t1", TaskId: "tk1"})
	require.NoError(t, err)
	require.NotNil(t, resp)

	_, err = srv.ListTaskArtifacts(ctx, &pb.ListTaskArtifactsRequest{TenantId: "t1"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestOrchestratorGRPC_GetTaskArtifact(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := context.Background()

	resp, err := srv.GetTaskArtifact(ctx, &pb.GetTaskArtifactRequest{TenantId: "t1", TaskId: "tk1", Name: "out.txt"})
	require.NoError(t, err)
	require.NotNil(t, resp)

	_, err = srv.GetTaskArtifact(ctx, &pb.GetTaskArtifactRequest{TenantId: "t1", Name: "out.txt"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = srv.GetTaskArtifact(ctx, &pb.GetTaskArtifactRequest{TenantId: "t1", TaskId: "tk1"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// =============================================================================
// Artifact conversion helpers
// =============================================================================

func TestArtifactConversionHelpers(t *testing.T) {
	assert.Nil(t, serviceArtifactToProto(nil))
	assert.Nil(t, serviceArtifactsToProto(nil))
	assert.Nil(t, protoArtifactsToService(nil))

	in := []*pb.Artifact{{
		Kind: "log", Name: "n", MimeType: "text/plain", Size: 5,
		Content: []byte("hello"), Ref: &pb.ArtifactRef{AgentId: "a", Path: "p", Sha: "s", Branch: "b"},
		Metadata: map[string]string{"k": "v"},
	}}
	svc := protoArtifactsToService(in)
	require.Len(t, svc, 1)
	assert.Equal(t, "n", svc[0].Name)
	assert.Equal(t, "a", svc[0].Ref.AgentID)

	back := serviceArtifactsToProto(svc)
	require.Len(t, back, 1)
	assert.Equal(t, "n", back[0].Name)
	assert.Equal(t, "b", back[0].Ref.Branch)
}
