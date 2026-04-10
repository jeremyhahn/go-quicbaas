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

//go:build integration && agents

package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// testTenantID is the tenant ID used across E2E orchestration tests.
	testTenantID = "e2e-test"

	// testTeamName is the team name used for task creation.
	testTeamName = "test-team"

	// testAgentID is the agent ID used for task claims.
	testAgentID = "test-agent"

	// requestTimeout is the default timeout for individual HTTP requests.
	requestTimeout = 30 * time.Second
)

// TestE2E_OrchestratorHealthCheck verifies the orchestrator health endpoint
// returns HTTP 200 with a status field.
func TestE2E_OrchestratorHealthCheck(t *testing.T) {
	requireOrchestrator(t)

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var result map[string]interface{}
	resp, err := getJSON(ctx, env.quicbaasURL+"/api/v1/orchestrator/health", &result)
	require.NoError(t, err, "health check request failed")
	assert.Equal(t, http.StatusOK, resp.StatusCode, "expected HTTP 200 from health endpoint")
	assert.Contains(t, result, "status", "health response must contain a 'status' field")
}

// TestE2E_OrchestratorHealthCheck_InvalidMethod verifies the orchestrator health
// endpoint rejects non-GET methods appropriately.
func TestE2E_OrchestratorHealthCheck_InvalidMethod(t *testing.T) {
	requireOrchestrator(t)

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	resp, err := postJSON(ctx, env.quicbaasURL+"/api/v1/orchestrator/health", nil)
	require.NoError(t, err, "POST to health endpoint should not return transport error")
	defer resp.Body.Close()

	// The health endpoint should either return 405 Method Not Allowed or
	// still respond (some routers accept any method on health). Either way,
	// it must not be a server error (5xx).
	assert.Less(t, resp.StatusCode, http.StatusInternalServerError,
		"health endpoint must not return 5xx for POST")
}

// TestE2E_ListBackends verifies the backends endpoint returns available backends
// including the ollama backend.
func TestE2E_ListBackends(t *testing.T) {
	requireOrchestrator(t)
	requireOllama(t)

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var result map[string]interface{}
	resp, err := getJSON(ctx, env.quicbaasURL+"/api/v1/orchestrator/backends", &result)
	require.NoError(t, err, "list backends request failed")
	assert.Equal(t, http.StatusOK, resp.StatusCode, "expected HTTP 200 from backends endpoint")

	// The response should contain backend entries. Look for the ollama backend.
	backends, ok := result["backends"]
	require.True(t, ok, "response must contain 'backends' field")

	backendList, ok := backends.([]interface{})
	require.True(t, ok, "backends field must be an array")

	found := false
	for _, b := range backendList {
		backend, ok := b.(map[string]interface{})
		if !ok {
			continue
		}
		if backend["name"] == "ollama" || backend["type"] == "ollama" {
			found = true
			break
		}
	}
	assert.True(t, found, "backends must contain an 'ollama' backend")
}

// TestE2E_ListBackends_ResponseStructure verifies the backends response has
// the expected structure and field types.
func TestE2E_ListBackends_ResponseStructure(t *testing.T) {
	requireOrchestrator(t)

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var rawResult json.RawMessage
	resp, err := getJSON(ctx, env.quicbaasURL+"/api/v1/orchestrator/backends", &rawResult)
	require.NoError(t, err, "list backends request failed")
	assert.Equal(t, http.StatusOK, resp.StatusCode, "expected HTTP 200 from backends endpoint")

	// Ensure the response is valid JSON.
	var parsed map[string]interface{}
	unmarshalErr := json.Unmarshal(rawResult, &parsed)
	require.NoError(t, unmarshalErr, "backends response must be valid JSON")
	assert.Contains(t, parsed, "backends", "response must contain 'backends' key")
}

// TestE2E_TaskLifecycle_ViaREST tests the full task lifecycle through the REST API:
// create, get, claim, complete, and verify final status.
func TestE2E_TaskLifecycle_ViaREST(t *testing.T) {
	requireOrchestrator(t)

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	baseURL := env.quicbaasURL + "/api/v1/orchestrator"

	// Step 1: Create a task.
	createBody := map[string]string{
		"tenant_id": testTenantID,
		"subject":   "E2E test task",
		"team_name": testTeamName,
	}
	createResp, createErr := postJSON(ctx, baseURL+"/tasks", createBody)
	require.NoError(t, createErr, "create task request failed")

	createBodyBytes, readErr := readResponseBody(createResp)
	require.NoError(t, readErr, "failed to read create task response body")
	require.Equal(t, http.StatusOK, createResp.StatusCode,
		"expected HTTP 200 for task creation, got body: %s", string(createBodyBytes))

	var createResult map[string]interface{}
	unmarshalErr := json.Unmarshal(createBodyBytes, &createResult)
	require.NoError(t, unmarshalErr, "failed to unmarshal create task response")

	taskID, ok := createResult["id"].(string)
	require.True(t, ok, "create task response must contain string 'id' field")
	require.NotEmpty(t, taskID, "task ID must not be empty")

	// Step 2: Get the task and verify it exists with correct subject.
	var getResult map[string]interface{}
	getURL := fmt.Sprintf("%s/tasks/%s?tenant_id=%s", baseURL, taskID, testTenantID)
	getResp, getErr := getJSON(ctx, getURL, &getResult)
	require.NoError(t, getErr, "get task request failed")
	assert.Equal(t, http.StatusOK, getResp.StatusCode, "expected HTTP 200 for get task")
	assert.Equal(t, "E2E test task", getResult["subject"], "task subject mismatch")

	// Step 3: Claim the task.
	claimBody := map[string]string{
		"agent_id":  testAgentID,
		"tenant_id": testTenantID,
	}
	claimResp, claimErr := postJSON(ctx, fmt.Sprintf("%s/tasks/%s/claim", baseURL, taskID), claimBody)
	require.NoError(t, claimErr, "claim task request failed")
	claimResp.Body.Close()
	assert.Equal(t, http.StatusOK, claimResp.StatusCode, "expected HTTP 200 for task claim")

	// Step 4: Complete the task.
	completeBody := map[string]string{
		"output":    "task completed",
		"tenant_id": testTenantID,
	}
	completeResp, completeErr := postJSON(ctx, fmt.Sprintf("%s/tasks/%s/complete", baseURL, taskID), completeBody)
	require.NoError(t, completeErr, "complete task request failed")
	completeResp.Body.Close()
	assert.Equal(t, http.StatusOK, completeResp.StatusCode, "expected HTTP 200 for task complete")

	// Step 5: Verify final task status is completed.
	var finalResult map[string]interface{}
	finalResp, finalErr := getJSON(ctx, getURL, &finalResult)
	require.NoError(t, finalErr, "get final task state failed")
	assert.Equal(t, http.StatusOK, finalResp.StatusCode, "expected HTTP 200 for final get task")
	assert.Equal(t, "completed", finalResult["status"], "task status must be 'completed'")
}

// TestE2E_TaskLifecycle_ViaREST_InvalidTaskID verifies that operating on a
// nonexistent task ID returns an appropriate error.
func TestE2E_TaskLifecycle_ViaREST_InvalidTaskID(t *testing.T) {
	requireOrchestrator(t)

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	baseURL := env.quicbaasURL + "/api/v1/orchestrator"
	getURL := fmt.Sprintf("%s/tasks/%s?tenant_id=%s", baseURL, "nonexistent-task-id", testTenantID)

	resp, err := getJSON(ctx, getURL, nil)
	require.NoError(t, err, "get nonexistent task request should not return transport error")
	defer resp.Body.Close()

	// A nonexistent task should not return 200.
	assert.NotEqual(t, http.StatusOK, resp.StatusCode,
		"getting a nonexistent task must not return HTTP 200")
}

// TestE2E_ListSessions_Empty verifies that listing sessions returns HTTP 200
// with an empty or valid sessions list.
func TestE2E_ListSessions_Empty(t *testing.T) {
	requireOrchestrator(t)

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var result map[string]interface{}
	resp, err := getJSON(ctx, env.quicbaasURL+"/api/v1/orchestrator/sessions", &result)
	require.NoError(t, err, "list sessions request failed")
	assert.Equal(t, http.StatusOK, resp.StatusCode, "expected HTTP 200 from sessions endpoint")

	// Sessions list should be present and empty (or contain only stale entries).
	sessions, ok := result["sessions"]
	if ok {
		sessionList, isList := sessions.([]interface{})
		if isList {
			assert.Empty(t, sessionList, "sessions list should be empty in a clean environment")
		}
	}
}

// TestE2E_ListSessions_Empty_ResponseFormat verifies the sessions endpoint
// returns valid JSON with the expected structure.
func TestE2E_ListSessions_Empty_ResponseFormat(t *testing.T) {
	requireOrchestrator(t)

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var rawResult json.RawMessage
	resp, err := getJSON(ctx, env.quicbaasURL+"/api/v1/orchestrator/sessions", &rawResult)
	require.NoError(t, err, "list sessions request failed")
	assert.Equal(t, http.StatusOK, resp.StatusCode, "expected HTTP 200")

	var parsed map[string]interface{}
	unmarshalErr := json.Unmarshal(rawResult, &parsed)
	require.NoError(t, unmarshalErr, "sessions response must be valid JSON")
}

// TestE2E_ListTasks_ViaREST creates three tasks and verifies they are all
// returned by the list tasks endpoint.
func TestE2E_ListTasks_ViaREST(t *testing.T) {
	requireOrchestrator(t)

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	baseURL := env.quicbaasURL + "/api/v1/orchestrator"

	// Create 3 tasks.
	taskIDs := make([]string, 3)
	for i := range 3 {
		body := map[string]string{
			"tenant_id": testTenantID,
			"subject":   fmt.Sprintf("E2E list task %d", i+1),
			"team_name": testTeamName,
		}

		resp, err := postJSON(ctx, baseURL+"/tasks", body)
		require.NoError(t, err, "create task %d request failed", i+1)

		bodyBytes, readErr := readResponseBody(resp)
		require.NoError(t, readErr, "failed to read create task %d response", i+1)
		require.Equal(t, http.StatusOK, resp.StatusCode,
			"expected HTTP 200 for task %d creation, got body: %s", i+1, string(bodyBytes))

		var result map[string]interface{}
		unmarshalErr := json.Unmarshal(bodyBytes, &result)
		require.NoError(t, unmarshalErr, "failed to unmarshal create task %d response", i+1)

		id, ok := result["id"].(string)
		require.True(t, ok, "task %d response must contain string 'id'", i+1)
		taskIDs[i] = id
	}

	// List all tasks for the tenant.
	var listResult map[string]interface{}
	listURL := fmt.Sprintf("%s/tasks?tenant_id=%s", baseURL, testTenantID)
	listResp, listErr := getJSON(ctx, listURL, &listResult)
	require.NoError(t, listErr, "list tasks request failed")
	assert.Equal(t, http.StatusOK, listResp.StatusCode, "expected HTTP 200 from list tasks")

	tasks, ok := listResult["tasks"]
	require.True(t, ok, "list response must contain 'tasks' field")

	taskList, ok := tasks.([]interface{})
	require.True(t, ok, "tasks field must be an array")

	// Verify all 3 created task IDs are present in the response.
	foundIDs := make(map[string]bool)
	for _, entry := range taskList {
		task, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		if id, ok := task["id"].(string); ok {
			foundIDs[id] = true
		}
	}

	for _, expectedID := range taskIDs {
		assert.True(t, foundIDs[expectedID],
			"task ID %s must be present in list response", expectedID)
	}
}

// TestE2E_ListTasks_ViaREST_MissingTenant verifies that listing tasks without
// a tenant ID returns an appropriate error.
func TestE2E_ListTasks_ViaREST_MissingTenant(t *testing.T) {
	requireOrchestrator(t)

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	baseURL := env.quicbaasURL + "/api/v1/orchestrator"
	resp, err := getJSON(ctx, baseURL+"/tasks", nil)
	require.NoError(t, err, "list tasks without tenant should not return transport error")
	defer resp.Body.Close()

	// Missing tenant should result in a client error (4xx), not success.
	assert.GreaterOrEqual(t, resp.StatusCode, http.StatusBadRequest,
		"listing tasks without tenant_id should return 4xx")
	assert.Less(t, resp.StatusCode, http.StatusInternalServerError,
		"listing tasks without tenant_id should not return 5xx")
}

// TestE2E_BackendNotFound verifies that creating a session with a nonexistent
// backend returns an error response.
func TestE2E_BackendNotFound(t *testing.T) {
	requireOrchestrator(t)

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	body := map[string]string{
		"backend": "nonexistent",
	}
	resp, err := postJSON(ctx, env.quicbaasURL+"/api/v1/orchestrator/sessions", body)
	require.NoError(t, err, "create session with bad backend should not return transport error")
	defer resp.Body.Close()

	// A nonexistent backend should not return 200.
	assert.NotEqual(t, http.StatusOK, resp.StatusCode,
		"creating session with nonexistent backend must not return HTTP 200")
}

// TestE2E_BackendNotFound_ResponseBody verifies the error response for a
// nonexistent backend contains meaningful error information.
func TestE2E_BackendNotFound_ResponseBody(t *testing.T) {
	requireOrchestrator(t)

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	body := map[string]string{
		"backend": "nonexistent-backend-xyz",
	}
	resp, err := postJSON(ctx, env.quicbaasURL+"/api/v1/orchestrator/sessions", body)
	require.NoError(t, err, "create session request should not return transport error")

	bodyBytes, readErr := readResponseBody(resp)
	require.NoError(t, readErr, "failed to read error response body")

	// The error response should be valid JSON with an error field.
	var errorResult map[string]interface{}
	unmarshalErr := json.Unmarshal(bodyBytes, &errorResult)
	if unmarshalErr == nil {
		// If JSON, it should contain an error indicator.
		hasError := errorResult["error"] != nil || errorResult["message"] != nil
		assert.True(t, hasError,
			"error response JSON should contain 'error' or 'message' field, got: %s",
			string(bodyBytes))
	}
}
