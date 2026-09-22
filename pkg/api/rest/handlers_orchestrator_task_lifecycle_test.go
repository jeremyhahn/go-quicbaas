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

//go:build rest

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func taskLifecycleRequest(t *testing.T, method, path, taskID string, body []byte) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskID", taskID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestOrchestratorBlockTask_Success(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	var got struct {
		tenant, task, reason string
		kind                 orchestration.TaskBlockKind
	}
	agentSvc.blockTaskFunc = func(_ context.Context, tenantID, taskID string, kind orchestration.TaskBlockKind, reason string) error {
		got.tenant, got.task, got.kind, got.reason = tenantID, taskID, kind, reason
		return nil
	}

	body, err := json.Marshal(OrchestratorBlockTaskRequest{Kind: orchestration.TaskBlockKindNeedsInfo, Reason: "which branch?"})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	server.HandleOrchestratorBlockTask(rec, taskLifecycleRequest(t, http.MethodPost, "/api/v1/orchestrator/tasks/task-1/block?tenant_id=t1", "task-1", body))

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, true, resp["blocked"])
	assert.Equal(t, "task-1", resp["task_id"])
	assert.Equal(t, "needs_info", resp["kind"])
	assert.Equal(t, "t1", got.tenant)
	assert.Equal(t, "task-1", got.task)
	assert.Equal(t, orchestration.TaskBlockKindNeedsInfo, got.kind)
	assert.Equal(t, "which branch?", got.reason)
}

func TestOrchestratorBlockTask_BadRequests(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	cases := []struct {
		name   string
		taskID string
		body   []byte
	}{
		{"missing task id", "", []byte(`{"kind":"blocked","reason":"r"}`)},
		{"invalid json", "task-1", []byte(`{`)},
		{"missing kind", "task-1", []byte(`{"reason":"r"}`)},
		{"missing reason", "task-1", []byte(`{"kind":"blocked"}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			server.HandleOrchestratorBlockTask(rec, taskLifecycleRequest(t, http.MethodPost, "/api/v1/orchestrator/tasks/x/block", tc.taskID, tc.body))
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestOrchestratorBlockTask_InvalidStateFromService(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.blockTaskFunc = func(context.Context, string, string, orchestration.TaskBlockKind, string) error {
		return &orchestration.TaskInvalidStateError{TaskID: "task-1", CurrentState: orchestration.TaskStatusCompleted, TargetState: orchestration.TaskStatusBlocked}
	}
	body := []byte(`{"kind":"blocked","reason":"r"}`)
	rec := httptest.NewRecorder()
	server.HandleOrchestratorBlockTask(rec, taskLifecycleRequest(t, http.MethodPost, "/api/v1/orchestrator/tasks/task-1/block", "task-1", body))
	assert.NotEqual(t, http.StatusOK, rec.Code)
	assert.GreaterOrEqual(t, rec.Code, http.StatusBadRequest)
}

func TestOrchestratorReadmitTask_Success(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	var gotTenant, gotTask string
	agentSvc.readmitTaskFunc = func(_ context.Context, tenantID, taskID string) error {
		gotTenant, gotTask = tenantID, taskID
		return nil
	}
	rec := httptest.NewRecorder()
	server.HandleOrchestratorReadmitTask(rec, taskLifecycleRequest(t, http.MethodPost, "/api/v1/orchestrator/tasks/task-1/readmit?tenant_id=t1", "task-1", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, true, resp["readmitted"])
	assert.Equal(t, "task-1", resp["task_id"])
	assert.Equal(t, "t1", gotTenant)
	assert.Equal(t, "task-1", gotTask)
}

func TestOrchestratorReadmitTask_Errors(t *testing.T) {
	t.Run("missing task id", func(t *testing.T) {
		server, _, cleanup := setupOrchestratorMockServer(t)
		defer cleanup()
		rec := httptest.NewRecorder()
		server.HandleOrchestratorReadmitTask(rec, taskLifecycleRequest(t, http.MethodPost, "/api/v1/orchestrator/tasks//readmit", "", nil))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("not found from service", func(t *testing.T) {
		server, agentSvc, cleanup := setupOrchestratorMockServer(t)
		defer cleanup()
		agentSvc.readmitTaskFunc = func(context.Context, string, string) error {
			return &orchestration.TaskNotFoundError{TenantID: "t1", TaskID: "task-1"}
		}
		rec := httptest.NewRecorder()
		server.HandleOrchestratorReadmitTask(rec, taskLifecycleRequest(t, http.MethodPost, "/api/v1/orchestrator/tasks/task-1/readmit", "task-1", nil))
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestOrchestratorListTasks_ProjectAndStatusesFilters(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	var got *orchestration.TaskFilter
	agentSvc.listTasksFunc = func(_ context.Context, _ string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error) {
		got = filter
		return []*orchestration.Task{{ID: "t1", Status: orchestration.TaskStatusBlocked, ProjectID: "proj-1", BlockedReason: "why"}}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/tasks?tenant_id=t1&project_id=proj-1&statuses=blocked,%20pending,,", nil)
	rec := httptest.NewRecorder()
	server.HandleOrchestratorListTasks(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, got)
	// THE PROJECT AND THE STATUSES ARE COUNTED FACETS, so the store is asked
	// for the tenant's tasks and the narrowing runs where the counts can see
	// it. What the transport must be held to is the filter it BUILT, which is
	// what the paged verb was handed.
	require.NotNil(t, agentSvc.listTasksPageFilter)
	assert.Equal(t, "proj-1", agentSvc.listTasksPageFilter.ProjectID)
	assert.Equal(t, []orchestration.TaskStatus{orchestration.TaskStatusBlocked, orchestration.TaskStatusPending},
		agentSvc.listTasksPageFilter.Statuses)

	var resp struct {
		Tasks []map[string]interface{} `json:"tasks"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Tasks, 1)
	assert.Equal(t, "blocked", resp.Tasks[0]["status"])
	assert.Equal(t, "why", resp.Tasks[0]["blocked_reason"])
	assert.Equal(t, "proj-1", resp.Tasks[0]["project_id"])
}

func TestParseTaskStatuses(t *testing.T) {
	cases := []struct {
		raw  string
		want []orchestration.TaskStatus
	}{
		{"", nil},
		{",", nil},
		{" , ", nil},
		{"blocked", []orchestration.TaskStatus{orchestration.TaskStatusBlocked}},
		{"blocked,pending", []orchestration.TaskStatus{orchestration.TaskStatusBlocked, orchestration.TaskStatusPending}},
		{" blocked , pending ", []orchestration.TaskStatus{orchestration.TaskStatusBlocked, orchestration.TaskStatusPending}},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			assert.Equal(t, tc.want, parseTaskStatuses(tc.raw))
		})
	}
}

func TestRoutes_TaskLifecycleRoutesAreRegistered(t *testing.T) {
	want := map[string]string{
		"POST /api/v1/orchestrator/tasks/{taskID}/block":   "orchestrator:task_block",
		"POST /api/v1/orchestrator/tasks/{taskID}/readmit": "orchestrator:task_readmit",
	}
	for _, spec := range Routes() {
		key := spec.Method + " " + spec.Pattern
		perm, ok := want[key]
		if !ok {
			continue
		}
		assert.Equal(t, RouteAuthPermission, spec.Auth, key)
		assert.Equal(t, perm, spec.Resource+":"+spec.Action, key)
		delete(want, key)
	}
	assert.Empty(t, want, "routes missing from the table")
}
