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
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// Job operations
// =============================================================================

func TestOrchestratorGRPC_StartJob(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := tenantctx.WithTenant(context.Background(), "t1")

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
	ctx := tenantctx.WithTenant(context.Background(), "t1")

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
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	resp, err := srv.CancelJob(ctx, &pb.OrchestratorCancelJobRequest{TenantId: "t1", JobId: "j1"})
	require.NoError(t, err)
	assert.True(t, resp.Cancelled)

	_, err = srv.CancelJob(ctx, &pb.OrchestratorCancelJobRequest{TenantId: "t1"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestOrchestratorGRPC_ListJobs(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	resp, err := srv.ListJobs(tenantctx.WithTenant(context.Background(), "t1"), &pb.OrchestratorListJobsRequest{TenantId: "t1"})
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

	resp, err := srv.ListJobs(tenantctx.WithTenant(context.Background(), "t1"), &pb.OrchestratorListJobsRequest{
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

	_, err := srv.ListJobs(tenantctx.WithTenant(context.Background(), "t1"), &pb.OrchestratorListJobsRequest{TenantId: "t1"})
	require.NoError(t, err)
	require.NotNil(t, svc.listJobsFilter)
	assert.True(t, svc.listJobsFilter.Empty(), "an absent task_id must narrow nothing")
}

// TestOrchestratorGRPC_ListJobs_CarriesTaskSource pins the forge issue onto
// every row of this transport's listing. A gRPC consumer holds no tasks to
// resolve the issue against, so a summary without it cannot name the issue
// at all. go-quicbaas#976.
func TestOrchestratorGRPC_ListJobs_CarriesTaskSource(t *testing.T) {
	svc := &grpcTestAgentService{listJobs: []*orchestration.JobStatus{{
		JobID:   "job-1",
		TaskID:  "task-a",
		Attempt: 2,
		Source: &orchestration.TaskSourceRef{
			Kind: "forge_issue",
			Ref:  "jeremyhahn/go-quicbaas#62",
			URL:  "https://forge.invalid/jeremyhahn/go-quicbaas/issues/62",
		},
	}}}
	srv := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())

	resp, err := srv.ListJobs(tenantctx.WithTenant(context.Background(), "t1"), &pb.OrchestratorListJobsRequest{TenantId: "t1"})
	require.NoError(t, err)
	require.True(t, svc.listJobsPaged,
		"the listing must read the verb that fills the source, or the rows arrive without one")
	require.Len(t, resp.GetJobs(), 1)

	source := resp.GetJobs()[0].GetSource()
	require.NotNil(t, source, "the row dropped the forge issue behind its task")
	assert.Equal(t, "forge_issue", source.GetKind())
	assert.Equal(t, "jeremyhahn/go-quicbaas#62", source.GetRef())
	assert.Equal(t, "https://forge.invalid/jeremyhahn/go-quicbaas/issues/62", source.GetUrl())
}

// TestOrchestratorGRPC_ListJobs_WithoutTaskSourceCarriesNone is the other
// half: a job whose task records no origin must answer with NO source, not
// an empty one a consumer would render as a chip pointing nowhere.
func TestOrchestratorGRPC_ListJobs_WithoutTaskSourceCarriesNone(t *testing.T) {
	svc := &grpcTestAgentService{listJobs: []*orchestration.JobStatus{
		{JobID: "job-1", TaskID: "task-a"},
	}}
	srv := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())

	resp, err := srv.ListJobs(tenantctx.WithTenant(context.Background(), "t1"), &pb.OrchestratorListJobsRequest{TenantId: "t1"})
	require.NoError(t, err)
	require.Len(t, resp.GetJobs(), 1)
	assert.Nil(t, resp.GetJobs()[0].GetSource())
}

// TestOrchestratorGRPC_ListJobs_AnswersEveryJobNewestFirst pins what reading
// the paged verb with no page must NOT change: the listing still answers the
// whole tenant, ordered by start time descending.
func TestOrchestratorGRPC_ListJobs_AnswersEveryJobNewestFirst(t *testing.T) {
	svc := &grpcTestAgentService{listJobs: []*orchestration.JobStatus{
		{JobID: "older", StartedAt: 100},
		{JobID: "newest", StartedAt: 300},
		{JobID: "middle", StartedAt: 200},
	}}
	srv := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())

	resp, err := srv.ListJobs(tenantctx.WithTenant(context.Background(), "t1"), &pb.OrchestratorListJobsRequest{TenantId: "t1"})
	require.NoError(t, err)
	require.Len(t, resp.GetJobs(), 3, "a nil page must cut nothing")
	assert.Equal(t, []string{"newest", "middle", "older"},
		[]string{resp.GetJobs()[0].GetJobId(), resp.GetJobs()[1].GetJobId(), resp.GetJobs()[2].GetJobId()})
}

// TestOrchestratorGRPC_GetJobStatus_CarriesTaskSource keeps the detail read
// agreeing with the listing: both name the same issue for the same job.
func TestOrchestratorGRPC_GetJobStatus_CarriesTaskSource(t *testing.T) {
	svc := &grpcTestAgentService{getJobStatusFunc: func(_ context.Context, _, jobID string) (*orchestration.JobStatus, error) {
		return &orchestration.JobStatus{
			JobID:  jobID,
			State:  "completed",
			TaskID: "task-a",
			Source: &orchestration.TaskSourceRef{
				Kind: "forge_issue",
				Ref:  "jeremyhahn/go-quicbaas#62",
				URL:  "https://forge.invalid/jeremyhahn/go-quicbaas/issues/62",
			},
		}, nil
	}}
	srv := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())

	resp, err := srv.GetJobStatus(tenantctx.WithTenant(context.Background(), "t1"), &pb.OrchestratorGetJobStatusRequest{TenantId: "t1", JobId: "j1"})
	require.NoError(t, err)
	source := resp.GetSource()
	require.NotNil(t, source, "the detail read dropped the forge issue behind the job's task")
	assert.Equal(t, "forge_issue", source.GetKind())
	assert.Equal(t, "jeremyhahn/go-quicbaas#62", source.GetRef())
	assert.Equal(t, "https://forge.invalid/jeremyhahn/go-quicbaas/issues/62", source.GetUrl())
}

// TestOrchestratorGRPC_GetJobStatus_WithoutTaskSourceCarriesNone is its
// error-side twin: no origin on the job means no source on the wire.
func TestOrchestratorGRPC_GetJobStatus_WithoutTaskSourceCarriesNone(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())

	resp, err := srv.GetJobStatus(tenantctx.WithTenant(context.Background(), "t1"), &pb.OrchestratorGetJobStatusRequest{TenantId: "t1", JobId: "j1"})
	require.NoError(t, err)
	assert.Nil(t, resp.GetSource())
}

func TestOrchestratorGRPC_DecomposeTask(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := tenantctx.WithTenant(context.Background(), "t1")

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
// Fleet operations
// =============================================================================

func TestOrchestratorGRPC_RegisterAgent(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	resp, err := srv.RegisterFleetNode(tenantctx.WithTenant(context.Background(), "t1"), &pb.RegisterFleetNodeRequest{
		TenantId: "t1", Name: "agent-a", Host: "h1", NodeType: "claude",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Node)
	assert.Equal(t, "agent-1", resp.Node.Id)
}

func TestOrchestratorGRPC_GetAgent(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	resp, err := srv.GetFleetNode(ctx, &pb.GetFleetNodeRequest{TenantId: "t1", NodeId: "a1"})
	require.NoError(t, err)
	assert.Equal(t, "a1", resp.Node.Id)

	_, err = srv.GetFleetNode(ctx, &pb.GetFleetNodeRequest{TenantId: "t1"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestOrchestratorGRPC_ListAgents(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	// With filter to exercise the filter conversion branch.
	resp, err := srv.ListFleetNodes(ctx, &pb.ListFleetNodesRequest{
		TenantId: "t1",
		Filter:   &pb.FleetNodeFilter{Host: "h1", NodeType: "claude", Backend: "acp", Limit: 10},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Without filter.
	resp, err = srv.ListFleetNodes(ctx, &pb.ListFleetNodesRequest{TenantId: "t1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestOrchestratorGRPC_RemoveAgent(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	resp, err := srv.RemoveFleetNode(ctx, &pb.RemoveFleetNodeRequest{TenantId: "t1", NodeId: "a1"})
	require.NoError(t, err)
	assert.True(t, resp.Removed)

	_, err = srv.RemoveFleetNode(ctx, &pb.RemoveFleetNodeRequest{TenantId: "t1"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestOrchestratorGRPC_AgentHeartbeat(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	resp, err := srv.FleetNodeHeartbeat(ctx, &pb.FleetNodeHeartbeatRequest{TenantId: "t1", NodeId: "a1"})
	require.NoError(t, err)
	assert.True(t, resp.Acknowledged)

	_, err = srv.FleetNodeHeartbeat(ctx, &pb.FleetNodeHeartbeatRequest{TenantId: "t1"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// =============================================================================
// Child job + artifacts
// =============================================================================

func TestOrchestratorGRPC_SpawnChildJob(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := tenantctx.WithTenant(context.Background(), "t1")

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
	ctx := tenantctx.WithTenant(context.Background(), "t1")

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
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	resp, err := srv.ListTaskArtifacts(ctx, &pb.ListTaskArtifactsRequest{TenantId: "t1", TaskId: "tk1"})
	require.NoError(t, err)
	require.NotNil(t, resp)

	_, err = srv.ListTaskArtifacts(ctx, &pb.ListTaskArtifactsRequest{TenantId: "t1"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestOrchestratorGRPC_GetTaskArtifact(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	ctx := tenantctx.WithTenant(context.Background(), "t1")

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
