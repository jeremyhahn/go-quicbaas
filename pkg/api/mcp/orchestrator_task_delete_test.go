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

//go:build mcp

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func TestRegisterAgentTools_TaskDeleteToolRegistered(t *testing.T) {
	t.Parallel()
	reg := newAgentRegistry(t, &mockAgentService{})
	names := map[string]bool{}
	for _, tool := range reg.tools {
		names[tool.Name] = true
	}
	assert.True(t, names["quicbaas_orchestrator_task_delete"], "tool not registered")
	assert.True(t, isAgentTool("quicbaas_orchestrator_task_delete"), "tool not routed to the agent dispatcher")
}

func TestExecuteAgentTool_TaskDelete(t *testing.T) {
	t.Parallel()
	t.Run("happy path", func(t *testing.T) {
		var gotTenant, gotTask string
		svc := &mockAgentService{deleteTaskFn: func(_ context.Context, tenantID, taskID string) error {
			gotTenant, gotTask = tenantID, taskID
			return nil
		}}
		ctx := orchestration.ContextWithTenant(context.Background(), "t1")
		result, err := newAgentRegistry(t, svc).executeAgentTool(ctx, "quicbaas_orchestrator_task_delete",
			map[string]interface{}{"task_id": "task-1"})
		require.NoError(t, err)
		out := decodeTaskToolResult(t, result)
		assert.Equal(t, true, out["deleted"])
		assert.Equal(t, "task-1", out["task_id"])
		assert.Equal(t, "t1", gotTenant)
		assert.Equal(t, "task-1", gotTask)
	})
	t.Run("missing task_id", func(t *testing.T) {
		_, err := newAgentRegistry(t, &mockAgentService{}).executeAgentTool(context.Background(),
			"quicbaas_orchestrator_task_delete", map[string]interface{}{})
		require.Error(t, err)
	})
	t.Run("leased task propagates", func(t *testing.T) {
		svc := &mockAgentService{deleteTaskFn: func(context.Context, string, string) error {
			return &orchestration.TaskLeasedError{TenantID: "t", TaskID: "x", JobID: "job-9"}
		}}
		_, err := newAgentRegistry(t, svc).executeAgentTool(context.Background(),
			"quicbaas_orchestrator_task_delete", map[string]interface{}{"task_id": "x"})
		require.ErrorIs(t, err, orchestration.ErrTaskLeased)
	})
	t.Run("service unavailable", func(t *testing.T) {
		_, err := newAgentRegistry(t, nil).executeAgentTool(context.Background(),
			"quicbaas_orchestrator_task_delete", map[string]interface{}{"task_id": "x"})
		require.Error(t, err)
	})
}

func TestMCPAuthz_TaskDeleteToolMapsToTheTaskWriteVerb(t *testing.T) {
	t.Parallel()
	perm, ok := qrdbToolExactMapping["quicbaas_orchestrator_task_delete"]
	require.True(t, ok, "tool has no authz mapping")
	assert.Equal(t, "orchestrator", perm.resource)
	assert.Equal(t, "task_create", perm.action)
}
