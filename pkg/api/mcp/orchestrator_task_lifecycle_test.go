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

//go:build mcp

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// decodeTaskToolResult re-decodes a marshalResult value into a map so a test can
// read the JSON-compatible shape the MCP client receives.
func decodeTaskToolResult(t *testing.T, result interface{}) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func TestRegisterAgentTools_TaskLifecycleToolsRegistered(t *testing.T) {
	t.Parallel()
	reg := newFleetRegistry(t, &mockAgentService{})
	names := map[string]bool{}
	for _, tool := range reg.tools {
		names[tool.Name] = true
	}
	assert.True(t, names["quicbaas_orchestrator_task_block"])
	assert.True(t, names["quicbaas_orchestrator_task_readmit"])
	assert.True(t, isAgentTool("quicbaas_orchestrator_task_block"))
	assert.True(t, isAgentTool("quicbaas_orchestrator_task_readmit"))
}

func TestExecuteAgentTool_TaskBlock_HappyPath(t *testing.T) {
	t.Parallel()
	var got struct {
		tenant, task, reason string
		kind                 orchestration.TaskBlockKind
	}
	svc := &mockAgentService{
		blockTaskFn: func(_ context.Context, tenantID, taskID string, kind orchestration.TaskBlockKind, reason string) error {
			got.tenant, got.task, got.kind, got.reason = tenantID, taskID, kind, reason
			return nil
		},
	}
	reg := newFleetRegistry(t, svc)
	ctx := orchestration.ContextWithTenant(context.Background(), "t1")

	result, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_task_block", map[string]interface{}{
		"task_id": "task-1", "kind": "escalated", "reason": "budget exhausted",
	})
	require.NoError(t, err)
	out := decodeTaskToolResult(t, result)
	assert.Equal(t, true, out["blocked"])
	assert.Equal(t, "task-1", out["task_id"])
	assert.Equal(t, "escalated", out["kind"])
	assert.Equal(t, "t1", got.tenant)
	assert.Equal(t, "task-1", got.task)
	assert.Equal(t, orchestration.TaskBlockKindEscalated, got.kind)
	assert.Equal(t, "budget exhausted", got.reason)
}

func TestExecuteAgentTool_TaskBlock_MissingAndErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args map[string]interface{}
	}{
		{"task_id", map[string]interface{}{"kind": "blocked", "reason": "r"}},
		{"kind", map[string]interface{}{"task_id": "t", "reason": "r"}},
		{"reason", map[string]interface{}{"task_id": "t", "kind": "blocked"}},
	}
	reg := newFleetRegistry(t, &mockAgentService{})
	for _, tc := range cases {
		t.Run("missing "+tc.name, func(t *testing.T) {
			_, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_task_block", tc.args)
			require.Error(t, err)
		})
	}

	t.Run("service error propagates", func(t *testing.T) {
		svc := &mockAgentService{blockTaskFn: func(context.Context, string, string, orchestration.TaskBlockKind, string) error {
			return &orchestration.InvalidTaskBlockKindError{Kind: "nope"}
		}}
		_, err := newFleetRegistry(t, svc).executeAgentTool(context.Background(), "quicbaas_orchestrator_task_block",
			map[string]interface{}{"tenant_id": "t1", "task_id": "t", "kind": "nope", "reason": "r"})
		require.ErrorIs(t, err, orchestration.ErrTaskStoreInvalidInput)
	})
	t.Run("service unavailable", func(t *testing.T) {
		_, err := newFleetRegistry(t, nil).executeAgentTool(context.Background(), "quicbaas_orchestrator_task_block",
			map[string]interface{}{"task_id": "t", "kind": "blocked", "reason": "r"})
		require.Error(t, err)
	})
}

func TestExecuteAgentTool_TaskReadmit(t *testing.T) {
	t.Parallel()
	t.Run("happy path", func(t *testing.T) {
		var gotTenant, gotTask string
		svc := &mockAgentService{readmitTaskFn: func(_ context.Context, tenantID, taskID string) error {
			gotTenant, gotTask = tenantID, taskID
			return nil
		}}
		ctx := orchestration.ContextWithTenant(context.Background(), "t1")
		result, err := newFleetRegistry(t, svc).executeAgentTool(ctx, "quicbaas_orchestrator_task_readmit", map[string]interface{}{"task_id": "task-1"})
		require.NoError(t, err)
		out := decodeTaskToolResult(t, result)
		assert.Equal(t, true, out["readmitted"])
		assert.Equal(t, "task-1", out["task_id"])
		assert.Equal(t, "t1", gotTenant)
		assert.Equal(t, "task-1", gotTask)
	})
	t.Run("missing task_id", func(t *testing.T) {
		_, err := newFleetRegistry(t, &mockAgentService{}).executeAgentTool(context.Background(), "quicbaas_orchestrator_task_readmit", map[string]interface{}{})
		require.Error(t, err)
	})
	t.Run("service error propagates", func(t *testing.T) {
		svc := &mockAgentService{readmitTaskFn: func(context.Context, string, string) error {
			return &orchestration.TaskNotFoundError{TenantID: "t", TaskID: "x"}
		}}
		_, err := newFleetRegistry(t, svc).executeAgentTool(context.Background(), "quicbaas_orchestrator_task_readmit", map[string]interface{}{"tenant_id": "t1", "task_id": "x"})
		require.ErrorIs(t, err, orchestration.ErrTaskNotFound)
	})
	// The tool result carries the typed error's text, so a refused transition
	// reaches the MCP caller naming both states (go-quicbaas#945).
	t.Run("refused transition names both states", func(t *testing.T) {
		svc := &mockAgentService{readmitTaskFn: func(context.Context, string, string) error {
			return &orchestration.TaskInvalidStateError{TaskID: "x", CurrentState: orchestration.TaskStatusCompleted, TargetState: orchestration.TaskStatusPending}
		}}
		_, err := newFleetRegistry(t, svc).executeAgentTool(context.Background(), "quicbaas_orchestrator_task_readmit", map[string]interface{}{"tenant_id": "t1", "task_id": "x"})
		var invalid *orchestration.TaskInvalidStateError
		require.ErrorAs(t, err, &invalid)
		assert.Contains(t, err.Error(), "current=completed")
		assert.Contains(t, err.Error(), "target=pending")
	})
	t.Run("service unavailable", func(t *testing.T) {
		_, err := newFleetRegistry(t, nil).executeAgentTool(context.Background(), "quicbaas_orchestrator_task_readmit", map[string]interface{}{"task_id": "x"})
		require.Error(t, err)
	})
}

func TestExecuteAgentTool_GetJobStatus_CarriesTaskBindingAndSessions(t *testing.T) {
	t.Parallel()
	svc := &mockAgentService{}
	svc.getJobStatusFn = func(_ context.Context, _, jobID string) (*orchestration.JobStatus, error) {
		return &orchestration.JobStatus{
			JobID: jobID, State: orchestration.JobStateExecuting,
			TaskID: "task-1", Attempt: 2, LeadSessionID: "lead", SessionIDs: []string{"lead", "w1"},
		}, nil
	}
	ctx := orchestration.ContextWithTenant(context.Background(), "t1")
	result, err := newFleetRegistry(t, svc).executeAgentTool(ctx, "quicbaas_orchestrator_get_job_status", map[string]interface{}{"job_id": "job-1"})
	require.NoError(t, err)
	out := decodeTaskToolResult(t, result)
	assert.Equal(t, "task-1", out["task_id"])
	assert.Equal(t, float64(2), out["attempt"])
	assert.Equal(t, "lead", out["lead_session_id"])
	assert.Equal(t, []interface{}{"lead", "w1"}, out["session_ids"])
}

func TestMCPAuthz_TaskLifecycleToolsMapToOrchestratorVerbs(t *testing.T) {
	t.Parallel()
	block, ok := qrdbToolExactMapping["quicbaas_orchestrator_task_block"]
	require.True(t, ok)
	assert.Equal(t, "orchestrator", block.resource)
	assert.Equal(t, "task_block", block.action)
	readmit, ok := qrdbToolExactMapping["quicbaas_orchestrator_task_readmit"]
	require.True(t, ok)
	assert.Equal(t, "task_readmit", readmit.action)
}
