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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// The orchestrator tools carry every field the SDK sends and return every
// field it reads (go-quicbaas#1644).

func TestOrchestratorStartJob_CarriesTheSDKFieldsAndTenant(t *testing.T) {
	var start *orchestration.StartJobRequest
	var startTenant string
	svc := &fleetMockAgentService{
		startJobFn: func(ctx context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
			start = req
			startTenant = orchestration.TenantFromContext(ctx)
			return &orchestration.StartJobResponse{JobID: "job-1"}, nil
		},
	}
	reg := newFleetRegistry(t, svc)
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	_, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_start_job", map[string]interface{}{
		"description":   "d",
		"lead_agent_id": "node-a",
		"poll_interval": "30s",
		"workflow_name": "wf",
		"task_id":       "task-1",
		"attempt":       2,
	})
	require.NoError(t, err)
	require.NotNil(t, start)
	assert.Equal(t, "t1", start.TenantID)
	assert.Equal(t, "t1", startTenant, "the service's tenant guard reads the context")
	assert.Equal(t, "node-a", start.LeadAgentID)
	assert.Equal(t, "30s", start.PollInterval)
	assert.Equal(t, "wf", start.WorkflowName)
	assert.Empty(t, start.WorkflowFile)
	assert.Equal(t, "task-1", start.TaskID)
	assert.Equal(t, 2, start.Attempt)
}

func TestOrchestratorStartJob_RefusesAnotherTenant(t *testing.T) {
	svc := &fleetMockAgentService{
		startJobFn: func(context.Context, *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
			t.Fatal("a job for another tenant reached the service")
			return nil, nil
		},
	}
	_, err := newFleetRegistry(t, svc).executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"),
		"quicbaas_orchestrator_start_job", map[string]interface{}{"description": "d", "tenant_id": "t2"})
	require.Error(t, err)
}

func TestOrchestratorSpawn_CarriesEnvAndSandboxedAndReturnsPlacement(t *testing.T) {
	var spawn *orchestration.SpawnSessionRequest
	svc := &fleetMockAgentService{
		mockAgentService: mockAgentService{
			spawnFn: func(_ context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
				spawn = req
				return &orchestration.SpawnSessionResponse{
					SessionID:       "sess-1",
					ContainerID:     "ctr-1",
					ResolvedAgentID: "agent-b",
					Failovers:       []orchestration.AgentAttempt{{AgentID: "agent-a", Reason: "exhausted"}},
				}, nil
			},
		},
	}
	reg := newFleetRegistry(t, svc)
	ctx := orchestration.ContextWithTenant(tenantctx.WithTenant(context.Background(), "t1"), "t1")

	result, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_spawn", map[string]interface{}{
		"tenant_id": "t1", "pool_id": "pool-1", "project_id": "proj-1",
		"env":       map[string]interface{}{"FOO": "bar"},
		"sandboxed": true,
	})
	require.NoError(t, err)
	require.NotNil(t, spawn)
	assert.Equal(t, map[string]string{"FOO": "bar"}, spawn.Env)
	assert.True(t, spawn.Sandboxed)

	decoded, ok := result.(map[string]interface{})
	require.True(t, ok, "tool result is %T, want a JSON object", result)
	assert.Equal(t, "ctr-1", decoded["container_id"])
	assert.Equal(t, "agent-b", decoded["resolved_agent_id"])
	failovers, ok := decoded["failovers"].([]interface{})
	require.True(t, ok, "failovers is %T", decoded["failovers"])
	require.Len(t, failovers, 1)
	assert.Equal(t, "agent-a", failovers[0].(map[string]interface{})["agent_id"])
}

func TestOrchestratorGetJobStatus_ReturnsPromptAppend(t *testing.T) {
	svc := &fleetMockAgentService{}
	svc.getJobStatusFn = func(_ context.Context, _, jobID string) (*orchestration.JobStatus, error) {
		return &orchestration.JobStatus{JobID: jobID, PromptAppend: "job material"}, nil
	}
	result, err := newFleetRegistry(t, svc).executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"),
		"quicbaas_orchestrator_get_job_status", map[string]interface{}{"job_id": "job-1"})
	require.NoError(t, err)
	decoded, ok := result.(map[string]interface{})
	require.True(t, ok, "tool result is %T, want a JSON object", result)
	assert.Equal(t, "job material", decoded["prompt_append"])
}
