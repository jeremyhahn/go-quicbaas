//go:build rest

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

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func orchReq(method, target string, body []byte, params map[string]string) *http.Request {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, target, bytes.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// HandleOrchestratorPrompt: missing session ID, missing prompt, invalid JSON,
// and a service error mapping.
func TestHandleOrchestratorPrompt_Errors(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	// Missing session ID
	w := httptest.NewRecorder()
	server.HandleOrchestratorPrompt(w, orchReq(http.MethodPost, "/x", []byte(`{"prompt":"hi"}`), nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Invalid JSON
	w = httptest.NewRecorder()
	server.HandleOrchestratorPrompt(w, orchReq(http.MethodPost, "/x", []byte("{bad"),
		map[string]string{"sessionID": "s1"}))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Missing prompt
	w = httptest.NewRecorder()
	server.HandleOrchestratorPrompt(w, orchReq(http.MethodPost, "/x", []byte(`{}`),
		map[string]string{"sessionID": "s1"}))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// A dispatch failure is reported synchronously: the turn never started,
	// so there is nothing on the event stream for the caller to discover.
	agentSvc.promptAsyncFunc = func(_ context.Context, _ *orchestration.PromptSessionRequest) (*orchestration.PromptTurnAck, error) {
		return nil, errors.New("backend down")
	}
	w = httptest.NewRecorder()
	server.HandleOrchestratorPrompt(w, orchReq(http.MethodPost, "/x", []byte(`{"prompt":"hi"}`),
		map[string]string{"sessionID": "s1"}))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleOrchestratorSessionStatus_Errors(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	// Missing session ID
	w := httptest.NewRecorder()
	server.HandleOrchestratorSessionStatus(w, orchReq(http.MethodGet, "/x", nil, nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Not found error -> 404
	agentSvc.sessionStatusFunc = func(_ context.Context, _ *orchestration.SessionStatusRequest) (*orchestration.SessionStatusResponse, error) {
		return nil, orchestration.ErrJobNotFound
	}
	w = httptest.NewRecorder()
	server.HandleOrchestratorSessionStatus(w, orchReq(http.MethodGet, "/x", nil,
		map[string]string{"sessionID": "s1"}))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleOrchestratorCloseSession_Errors(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleOrchestratorCloseSession(w, orchReq(http.MethodDelete, "/x", nil, nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	agentSvc.closeSessionFunc = func(_ context.Context, _ *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error) {
		return nil, errors.New("close failed")
	}
	w = httptest.NewRecorder()
	server.HandleOrchestratorCloseSession(w, orchReq(http.MethodDelete, "/x", nil,
		map[string]string{"sessionID": "s1"}))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleOrchestratorListSessions_Error(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.listSessionsFunc = func(_ context.Context, _ *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
		return nil, errors.New("list failed")
	}
	w := httptest.NewRecorder()
	server.HandleOrchestratorListSessions(w, orchReq(http.MethodGet, "/api/v1/orchestrator/sessions", nil, nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleOrchestratorGetTask_Errors(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	// Missing task ID
	w := httptest.NewRecorder()
	server.HandleOrchestratorGetTask(w, orchReq(http.MethodGet, "/x", nil, nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Not found -> 404
	agentSvc.getTaskFunc = func(_ context.Context, _, _ string) (*orchestration.Task, error) {
		return nil, orchestration.ErrTaskNotFound
	}
	w = httptest.NewRecorder()
	server.HandleOrchestratorGetTask(w, orchReq(http.MethodGet, "/x", nil,
		map[string]string{"taskID": "t1"}))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleOrchestratorListTasks_Error(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	agentSvc.listTasksFunc = func(_ context.Context, _ string, _ *orchestration.TaskFilter) ([]*orchestration.Task, error) {
		return nil, errors.New("query failed")
	}
	w := httptest.NewRecorder()
	server.HandleOrchestratorListTasks(w, orchReq(http.MethodGet,
		"/api/v1/orchestrator/tasks?status=pending&team_name=core", nil, nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleOrchestratorClaimTask_Errors(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	// Missing task ID
	w := httptest.NewRecorder()
	server.HandleOrchestratorClaimTask(w, orchReq(http.MethodPost, "/x", []byte(`{"agent_id":"a"}`), nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Invalid JSON
	w = httptest.NewRecorder()
	server.HandleOrchestratorClaimTask(w, orchReq(http.MethodPost, "/x", []byte("{bad"),
		map[string]string{"taskID": "t1"}))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Missing agent_id
	w = httptest.NewRecorder()
	server.HandleOrchestratorClaimTask(w, orchReq(http.MethodPost, "/x", []byte(`{}`),
		map[string]string{"taskID": "t1"}))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Conflict error -> 409
	agentSvc.claimTaskFunc = func(_ context.Context, _, _, _ string) error {
		return orchestration.ErrTaskAlreadyClaimed
	}
	w = httptest.NewRecorder()
	server.HandleOrchestratorClaimTask(w, orchReq(http.MethodPost, "/x", []byte(`{"agent_id":"a"}`),
		map[string]string{"taskID": "t1"}))
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestHandleOrchestratorCompleteTask_Errors(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	// Missing task ID
	w := httptest.NewRecorder()
	server.HandleOrchestratorCompleteTask(w, orchReq(http.MethodPost, "/x", []byte(`{}`), nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Invalid JSON
	w = httptest.NewRecorder()
	server.HandleOrchestratorCompleteTask(w, orchReq(http.MethodPost, "/x", []byte("{bad"),
		map[string]string{"taskID": "t1"}))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Service error -> 500
	agentSvc.completeTaskFunc = func(_ context.Context, _, _ string, _ *orchestration.TaskResult) error {
		return errors.New("complete failed")
	}
	w = httptest.NewRecorder()
	server.HandleOrchestratorCompleteTask(w, orchReq(http.MethodPost, "/x", []byte(`{}`),
		map[string]string{"taskID": "t1"}))
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Success
	agentSvc.completeTaskFunc = func(_ context.Context, _, _ string, _ *orchestration.TaskResult) error { return nil }
	w = httptest.NewRecorder()
	server.HandleOrchestratorCompleteTask(w, orchReq(http.MethodPost, "/x", []byte(`{}`),
		map[string]string{"taskID": "t1"}))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleOrchestratorListWorkflows_Success(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleOrchestratorListWorkflows(w, orchReq(http.MethodGet, "/api/v1/orchestrator/workflows", nil, nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleOrchestratorGetWorkflow(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	// Missing name -> 400
	w := httptest.NewRecorder()
	server.HandleOrchestratorGetWorkflow(w, orchReq(http.MethodGet, "/x", nil, nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Mock returns NotFoundError -> 404
	w = httptest.NewRecorder()
	server.HandleOrchestratorGetWorkflow(w, orchReq(http.MethodGet, "/x", nil, map[string]string{"name": "wf1"}))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleOrchestratorUpdateWorkflow(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	// Missing name -> 400
	w := httptest.NewRecorder()
	server.HandleOrchestratorUpdateWorkflow(w, orchReq(http.MethodPut, "/x", []byte(`{}`), nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Invalid JSON -> 400
	w = httptest.NewRecorder()
	server.HandleOrchestratorUpdateWorkflow(w, orchReq(http.MethodPut, "/x", []byte("{bad"), map[string]string{"name": "wf1"}))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Success
	w = httptest.NewRecorder()
	server.HandleOrchestratorUpdateWorkflow(w, orchReq(http.MethodPut, "/x", []byte(`{}`), map[string]string{"name": "wf1"}))
	assert.Equal(t, http.StatusOK, w.Code)

	// Service error -> 404 (not found)
	agentSvc.updateWorkflowFunc = func(_ *orchestration.WorkflowDefinition) error {
		return orchestration.ErrWorkflowNotFound
	}
	w = httptest.NewRecorder()
	server.HandleOrchestratorUpdateWorkflow(w, orchReq(http.MethodPut, "/x", []byte(`{}`), map[string]string{"name": "wf1"}))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleOrchestratorDeleteWorkflow(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	// Missing name -> 400
	w := httptest.NewRecorder()
	server.HandleOrchestratorDeleteWorkflow(w, orchReq(http.MethodDelete, "/x", nil, nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Success
	w = httptest.NewRecorder()
	server.HandleOrchestratorDeleteWorkflow(w, orchReq(http.MethodDelete, "/x", nil, map[string]string{"name": "wf1"}))
	assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusNoContent)

	// Service error -> 404
	agentSvc.deleteWorkflowFunc = func(_ string) error { return orchestration.ErrWorkflowNotFound }
	w = httptest.NewRecorder()
	server.HandleOrchestratorDeleteWorkflow(w, orchReq(http.MethodDelete, "/x", nil, map[string]string{"name": "wf1"}))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleOrchestratorGetAgent(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleGetFleetNode(w, orchReq(http.MethodGet, "/x", nil, nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = httptest.NewRecorder()
	server.HandleGetFleetNode(w, orchReq(http.MethodGet, "/x", nil, map[string]string{"nodeID": "a1"}))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleOrchestratorRemoveAgent(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleRemoveFleetNode(w, orchReq(http.MethodDelete, "/x", nil, nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = httptest.NewRecorder()
	server.HandleRemoveFleetNode(w, orchReq(http.MethodDelete, "/x", nil, map[string]string{"nodeID": "a1"}))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleOrchestratorGetJobStatus(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleOrchestratorGetJobStatus(w, orchReq(http.MethodGet, "/x", nil, nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = httptest.NewRecorder()
	server.HandleOrchestratorGetJobStatus(w, orchReq(http.MethodGet, "/x", nil, map[string]string{"jobID": "j1"}))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleOrchestratorListJobs(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleOrchestratorListJobs(w, orchReq(http.MethodGet, "/api/v1/orchestrator/jobs", nil, nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestHandleOrchestratorListJobs_NarrowsToTask is the regression for the
// measured answer: before go-quicbaas#822 the query accepted task_id and
// listed the whole tenant anyway, so a task page asking for its own attempts
// was handed seven jobs belonging to two other tasks.
func TestHandleOrchestratorListJobs_NarrowsToTask(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listJobs = []*orchestration.JobStatus{{JobID: "job-1", TaskID: "task-a"}}

	w := httptest.NewRecorder()
	server.HandleOrchestratorListJobs(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/jobs?tenant_id=acme&task_id=task-a", nil, nil))

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, agentSvc.listJobsFilter, "task_id never reached the service")
	assert.Equal(t, "task-a", agentSvc.listJobsFilter.TaskID)

	var body struct {
		Jobs []struct {
			JobID  string `json:"job_id"`
			TaskID string `json:"task_id"`
		} `json:"jobs"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Jobs, 1)
	assert.Equal(t, "task-a", body.Jobs[0].TaskID)
}

// TestHandleOrchestratorListJobs_WithoutTaskNarrowsNothing is the other half:
// no task_id must list the tenant rather than nothing.
func TestHandleOrchestratorListJobs_WithoutTaskNarrowsNothing(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleOrchestratorListJobs(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/jobs?tenant_id=acme", nil, nil))

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, agentSvc.listJobsFilter)
	assert.True(t, agentSvc.listJobsFilter.Empty(), "an absent task_id must narrow nothing")
}

func TestHandleOrchestratorCancelJob(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleOrchestratorCancelJob(w, orchReq(http.MethodPost, "/x", nil, nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = httptest.NewRecorder()
	server.HandleOrchestratorCancelJob(w, orchReq(http.MethodPost, "/x", nil, map[string]string{"jobID": "j1"}))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleOrchestratorSpawnChildJob(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	// Missing parent job ID
	w := httptest.NewRecorder()
	server.HandleOrchestratorSpawnChildJob(w, orchReq(http.MethodPost, "/x", []byte(`{}`), nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Invalid JSON
	w = httptest.NewRecorder()
	server.HandleOrchestratorSpawnChildJob(w, orchReq(http.MethodPost, "/x", []byte("{bad"), map[string]string{"jobID": "j1"}))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Success
	w = httptest.NewRecorder()
	server.HandleOrchestratorSpawnChildJob(w, orchReq(http.MethodPost, "/x", []byte(`{}`), map[string]string{"jobID": "j1"}))
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestHandleOrchestratorArtifacts(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	// SubmitArtifacts: missing task ID
	w := httptest.NewRecorder()
	server.HandleOrchestratorSubmitArtifacts(w, orchReq(http.MethodPost, "/x", []byte(`[]`), nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// SubmitArtifacts: success
	w = httptest.NewRecorder()
	server.HandleOrchestratorSubmitArtifacts(w, orchReq(http.MethodPost, "/x", []byte(`[]`), map[string]string{"taskID": "t1"}))
	assert.Equal(t, http.StatusOK, w.Code)

	// ListArtifacts: missing task ID
	w = httptest.NewRecorder()
	server.HandleOrchestratorListArtifacts(w, orchReq(http.MethodGet, "/x", nil, nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// ListArtifacts: success
	w = httptest.NewRecorder()
	server.HandleOrchestratorListArtifacts(w, orchReq(http.MethodGet, "/x", nil, map[string]string{"taskID": "t1"}))
	assert.Equal(t, http.StatusOK, w.Code)

	// GetArtifact: missing params
	w = httptest.NewRecorder()
	server.HandleOrchestratorGetArtifact(w, orchReq(http.MethodGet, "/x", nil, map[string]string{"taskID": "t1"}))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// GetArtifact: success
	w = httptest.NewRecorder()
	server.HandleOrchestratorGetArtifact(w, orchReq(http.MethodGet, "/x", nil, map[string]string{"taskID": "t1", "name": "a"}))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleOrchestratorCreateWorkflow(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	// Invalid JSON
	w := httptest.NewRecorder()
	server.HandleOrchestratorCreateWorkflow(w, orchReq(http.MethodPost, "/x", []byte("{bad"), nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Missing name
	w = httptest.NewRecorder()
	server.HandleOrchestratorCreateWorkflow(w, orchReq(http.MethodPost, "/x", []byte(`{"definition":{}}`), nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Missing definition
	w = httptest.NewRecorder()
	server.HandleOrchestratorCreateWorkflow(w, orchReq(http.MethodPost, "/x", []byte(`{"name":"wf"}`), nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Success
	w = httptest.NewRecorder()
	server.HandleOrchestratorCreateWorkflow(w, orchReq(http.MethodPost, "/x", []byte(`{"name":"wf","definition":{}}`), nil))
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestHandleOrchestratorStartJob(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	// Invalid JSON
	w := httptest.NewRecorder()
	server.HandleOrchestratorStartJob(w, orchReq(http.MethodPost, "/x", []byte("{bad"), nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Missing description
	w = httptest.NewRecorder()
	server.HandleOrchestratorStartJob(w, orchReq(http.MethodPost, "/x", []byte(`{}`), nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Success
	w = httptest.NewRecorder()
	server.HandleOrchestratorStartJob(w, orchReq(http.MethodPost, "/x?tenant_id=t1",
		[]byte(`{"description":"do work"}`), nil))
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestHandleOrchestratorListBackends_Success(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleOrchestratorListBackends(w, orchReq(http.MethodGet, "/api/v1/orchestrator/backends", nil, nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleOrchestratorListBackendModels(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	// Missing backend name
	w := httptest.NewRecorder()
	server.HandleListFleetNodeBackendModels(w, orchReq(http.MethodGet, "/x", nil,
		map[string]string{"nodeID": "a1"}))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Success
	w = httptest.NewRecorder()
	server.HandleListFleetNodeBackendModels(w, orchReq(http.MethodGet, "/x", nil,
		map[string]string{"nodeID": "a1", "backendName": "ollama"}))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleOrchestratorListAgents_Success(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleListFleetNodes(w, orchReq(http.MethodGet, "/api/v1/fleet", nil, nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleOrchestratorSubscribeJobEvents(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	// Missing job ID
	w := httptest.NewRecorder()
	server.HandleOrchestratorSubscribeJobEvents(w, orchReq(http.MethodGet, "/x", nil, nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Success: mock returns a closed channel -> handler streams a done event.
	w = httptest.NewRecorder()
	server.HandleOrchestratorSubscribeJobEvents(w, orchReq(http.MethodGet, "/x", nil, map[string]string{"jobID": "j1"}))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "done")
}
