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

//go:build rest

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func TestOrchestratorDeleteTask_Success(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	var gotTenant, gotTask string
	agentSvc.deleteTaskFunc = func(_ context.Context, tenantID, taskID string) error {
		gotTenant, gotTask = tenantID, taskID
		return nil
	}

	rec := httptest.NewRecorder()
	server.HandleOrchestratorDeleteTask(rec, taskLifecycleRequest(t,
		http.MethodDelete, "/api/v1/orchestrator/tasks/task-1?tenant_id=t1", "task-1", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, true, resp["deleted"])
	assert.Equal(t, "task-1", resp["task_id"])
	assert.Equal(t, "t1", gotTenant)
	assert.Equal(t, "task-1", gotTask)
}

func TestOrchestratorDeleteTask_Errors(t *testing.T) {
	t.Run("missing task id", func(t *testing.T) {
		server, _, cleanup := setupOrchestratorMockServer(t)
		defer cleanup()
		rec := httptest.NewRecorder()
		server.HandleOrchestratorDeleteTask(rec, taskLifecycleRequest(t,
			http.MethodDelete, "/api/v1/orchestrator/tasks/", "", nil))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("not found from service", func(t *testing.T) {
		server, agentSvc, cleanup := setupOrchestratorMockServer(t)
		defer cleanup()
		agentSvc.deleteTaskFunc = func(context.Context, string, string) error {
			return &orchestration.TaskNotFoundError{TenantID: "t1", TaskID: "task-1"}
		}
		rec := httptest.NewRecorder()
		server.HandleOrchestratorDeleteTask(rec, taskLifecycleRequest(t,
			http.MethodDelete, "/api/v1/orchestrator/tasks/task-1", "task-1", nil))
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
	// A task a running job holds must answer 409, not 404 and not 500: the
	// caller named a real task and the answer is "not now".
	t.Run("leased task is a conflict", func(t *testing.T) {
		server, agentSvc, cleanup := setupOrchestratorMockServer(t)
		defer cleanup()
		agentSvc.deleteTaskFunc = func(context.Context, string, string) error {
			return &orchestration.TaskLeasedError{TenantID: "t1", TaskID: "task-1", JobID: "job-9", Node: "node-a"}
		}
		rec := httptest.NewRecorder()
		server.HandleOrchestratorDeleteTask(rec, taskLifecycleRequest(t,
			http.MethodDelete, "/api/v1/orchestrator/tasks/task-1", "task-1", nil))
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Contains(t, rec.Body.String(), "job-9")
	})
}

func TestRoutes_TaskDeleteRouteIsRegistered(t *testing.T) {
	const key = "DELETE /api/v1/orchestrator/tasks/{taskID}"
	for _, spec := range Routes() {
		if spec.Method+" "+spec.Pattern != key {
			continue
		}
		assert.Equal(t, RouteAuthPermission, spec.Auth, key)
		// The task-write family: deleting a task record is the inverse of
		// writing one, and go-qrdb's vocabulary carries no separate delete
		// action for the orchestrator family.
		assert.Equal(t, "orchestrator:task_create", spec.Resource+":"+spec.Action, key)
		return
	}
	t.Fatalf("route not registered: %s", key)
}
