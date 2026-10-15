//go:build mcp

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// taskVerbService records what the task-verb tools hand the agent service and
// answers with the configured task, child job or error.
type taskVerbService struct {
	fleetMockAgentService
	tenantID, taskID, agentID string
	result                    *orchestration.TaskResult
	artifacts                 []orchestration.Artifact
	spawn                     *orchestration.SpawnChildJobRequest
	task                      *orchestration.Task
	childJobID                string
	err                       error
}

func (s *taskVerbService) GetTask(_ context.Context, tenantID, taskID string) (*orchestration.Task, error) {
	s.tenantID, s.taskID = tenantID, taskID
	return s.task, s.err
}

func (s *taskVerbService) ClaimTask(_ context.Context, tenantID, taskID, agentID string) error {
	s.tenantID, s.taskID, s.agentID = tenantID, taskID, agentID
	return s.err
}

func (s *taskVerbService) CompleteTask(_ context.Context, tenantID, taskID string, result *orchestration.TaskResult) error {
	s.tenantID, s.taskID, s.result = tenantID, taskID, result
	return s.err
}

func (s *taskVerbService) SubmitTaskArtifacts(_ context.Context, tenantID, taskID string, artifacts []orchestration.Artifact) error {
	s.tenantID, s.taskID, s.artifacts = tenantID, taskID, artifacts
	return s.err
}

func (s *taskVerbService) SpawnChildJob(_ context.Context, req *orchestration.SpawnChildJobRequest) (*orchestration.SpawnChildJobResponse, error) {
	s.spawn = req
	if s.err != nil {
		return nil, s.err
	}
	return &orchestration.SpawnChildJobResponse{ChildJobID: s.childJobID}, nil
}

func TestTaskVerbs_GetTask(t *testing.T) {
	svc := &taskVerbService{task: &orchestration.Task{
		ID: "task-1", TenantID: "t1", Subject: "ship it",
		Status: orchestration.TaskStatusInProgress, AssignedTo: "alice",
		Metadata: map[string]string{"issue": "124"}, CreatedAt: 1700000000,
	}}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_task_get")

	task, err := client.GetTask(context.Background(), &orchestrator.GetTaskRequest{TenantID: "t1", TaskID: "task-1"})
	require.NoError(t, err)
	require.Equal(t, "t1", svc.tenantID)
	require.Equal(t, "task-1", svc.taskID)

	require.Equal(t, "task-1", task.ID)
	require.Equal(t, "t1", task.TenantID)
	require.Equal(t, "ship it", task.Subject)
	require.Equal(t, string(orchestration.TaskStatusInProgress), task.Status)
	require.Equal(t, "alice", task.AssignedTo)
	require.Equal(t, map[string]string{"issue": "124"}, task.Metadata)
	require.Equal(t, int64(1700000000), task.CreatedAt)
}

func TestTaskVerbs_GetTask_ServiceRefusal(t *testing.T) {
	svc := &taskVerbService{err: errDriftServiceRefused}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_task_get")

	_, err := client.GetTask(context.Background(), &orchestrator.GetTaskRequest{TenantID: "t1", TaskID: "task-1"})
	requireTypedError(t, err)
}

func TestTaskVerbs_ClaimTask(t *testing.T) {
	svc := &taskVerbService{}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_task_claim")

	resp, err := client.ClaimTask(context.Background(), &orchestrator.ClaimTaskRequest{
		TenantID: "t1", TaskID: "task-1", AgentID: "agent-1",
	})
	require.NoError(t, err)
	require.Equal(t, "t1", svc.tenantID)
	require.Equal(t, "task-1", svc.taskID)
	require.Equal(t, "agent-1", svc.agentID)

	require.Equal(t, &orchestrator.ClaimTaskResponse{TaskID: "task-1", AgentID: "agent-1", Claimed: true}, resp)
}

func TestTaskVerbs_ClaimTask_MissingAgent(t *testing.T) {
	svc := &taskVerbService{}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_task_claim")

	_, err := client.ClaimTask(context.Background(), &orchestrator.ClaimTaskRequest{TenantID: "t1", TaskID: "task-1"})
	requireTypedError(t, err)
	require.Empty(t, svc.taskID, "the service must not be reached without an agent")
}

func TestTaskVerbs_CompleteTask(t *testing.T) {
	svc := &taskVerbService{}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_task_complete")

	resp, err := client.CompleteTask(context.Background(), &orchestrator.CompleteTaskRequest{
		TenantID: "t1", TaskID: "task-1",
		Result: &orchestrator.TaskResult{
			Output: "done", Files: []string{"out.txt"}, Metadata: map[string]string{"outcome": "ok"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "t1", svc.tenantID)
	require.Equal(t, "task-1", svc.taskID)
	require.Equal(t, &orchestration.TaskResult{
		Output: "done", Files: []string{"out.txt"}, Metadata: map[string]string{"outcome": "ok"},
	}, svc.result)

	require.Equal(t, &orchestrator.CompleteTaskResponse{TaskID: "task-1", Completed: true}, resp)
}

func TestTaskVerbs_CompleteTask_ServiceRefusal(t *testing.T) {
	svc := &taskVerbService{err: errDriftServiceRefused}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_task_complete")

	_, err := client.CompleteTask(context.Background(), &orchestrator.CompleteTaskRequest{TenantID: "t1", TaskID: "task-1"})
	requireTypedError(t, err)
}

func TestTaskVerbs_SubmitTaskArtifacts(t *testing.T) {
	svc := &taskVerbService{}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_task_submit_artifacts")

	binary := []byte{0x00, 0xff, 0x10, 0x80}
	resp, err := client.SubmitTaskArtifacts(context.Background(), &orchestrator.SubmitTaskArtifactsRequest{
		TenantID: "t1", TaskID: "task-1",
		Artifacts: []*orchestrator.TaskArtifact{
			{Kind: "inline", Name: "blob", MIMEType: "application/octet-stream", Size: 4, Content: binary},
			{
				Kind: "commit", Name: "change",
				Ref:      &orchestrator.TaskArtifactRef{AgentID: "agent-1", SHA: "abc", Branch: "main"},
				Metadata: map[string]string{"issue": "124"},
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "t1", svc.tenantID)
	require.Equal(t, "task-1", svc.taskID)
	require.Equal(t, []orchestration.Artifact{
		{Kind: orchestration.ArtifactKindInline, Name: "blob", MIMEType: "application/octet-stream", Size: 4, Content: binary},
		{
			Kind: orchestration.ArtifactKind("commit"), Name: "change",
			Ref:      orchestration.ArtifactRef{AgentID: "agent-1", SHA: "abc", Branch: "main"},
			Metadata: map[string]string{"issue": "124"},
		},
	}, svc.artifacts)

	require.Equal(t, &orchestrator.SubmitTaskArtifactsResponse{TaskID: "task-1", Stored: 2}, resp)
}

func TestTaskVerbs_SubmitTaskArtifacts_MissingArtifacts(t *testing.T) {
	svc := &taskVerbService{}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_task_submit_artifacts")

	_, err := client.SubmitTaskArtifacts(context.Background(), &orchestrator.SubmitTaskArtifactsRequest{
		TenantID: "t1", TaskID: "task-1",
	})
	requireTypedError(t, err)
	require.Empty(t, svc.taskID, "the service must not be reached without artifacts")
}

func TestTaskVerbs_SpawnChildJob(t *testing.T) {
	svc := &taskVerbService{childJobID: "job-2"}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_spawn_child_job")

	resp, err := client.SpawnChildJob(context.Background(), &orchestrator.SpawnChildJobRequest{
		ParentJobID: "job-1", CallerAgentID: "agent-1", ProjectID: "proj-1",
		Tasks: []*orchestrator.TaskSpec{{
			Title: "build", Description: "build it", Dependencies: []string{"plan"},
			RequiredCapabilities: []string{"gpu"},
		}},
		HostAffinity: "gpu-1", GpuRequired: true, HostAffinityStrict: true,
	})
	require.NoError(t, err)
	require.Equal(t, &orchestration.SpawnChildJobRequest{
		ParentJobID: "job-1", CallerAgentID: "agent-1", ProjectID: "proj-1",
		Tasks: []*orchestration.TaskSpec{{
			Title: "build", Description: "build it", Dependencies: []string{"plan"},
			RequiredCapabilities: []string{"gpu"},
		}},
		HostAffinity: "gpu-1", GpuRequired: true, HostAffinityStrict: true,
	}, svc.spawn)

	require.Equal(t, &orchestrator.SpawnChildJobResponse{ChildJobID: "job-2"}, resp)
}

func TestTaskVerbs_SpawnChildJob_ServiceRefusal(t *testing.T) {
	svc := &taskVerbService{err: errDriftServiceRefused}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_spawn_child_job")

	_, err := client.SpawnChildJob(context.Background(), &orchestrator.SpawnChildJobRequest{ParentJobID: "job-1"})
	requireTypedError(t, err)
}
