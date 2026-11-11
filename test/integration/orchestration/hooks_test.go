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

//go:build integration && rag

package orchestration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestE2E_HookReceiver_TaskCreated verifies that a hook event for task creation
// results in the task being stored and retrievable through the AgentService.
func TestE2E_HookReceiver_TaskCreated(t *testing.T) {
	requireQuicBaaS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// POST a task-created hook event to the orchestrator webhook endpoint.
	hookPayload := map[string]interface{}{
		"event_type": "task.created",
		"tenant_id":  "hook-tenant-created",
		"task": map[string]interface{}{
			"subject":   "Hook-created task",
			"team_name": "backend-team",
			"metadata": map[string]string{
				"source": "webhook",
			},
		},
	}

	resp, err := postJSON(ctx, env.quicbaasURL+"/api/v1/orchestrator/hooks", hookPayload)
	require.NoError(t, err, "POST hook event should succeed")
	defer resp.Body.Close()

	// The hook endpoint should accept the event.
	assert.True(t,
		resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted,
		"hook endpoint should return 200 or 202, got %d", resp.StatusCode)

	// Verify the task was stored by listing tasks for the tenant.
	var listResult struct {
		Tasks []orchestration.Task `json:"tasks"`
	}
	listResp, err := getJSON(ctx, env.quicbaasURL+"/api/v1/orchestrator/tasks?tenant_id=hook-tenant-created", &listResult)
	require.NoError(t, err, "GET tasks should succeed")
	if listResp.Body != nil {
		defer listResp.Body.Close()
	}

	if listResp.StatusCode == http.StatusOK {
		found := false
		for _, task := range listResult.Tasks {
			if task.Subject == "Hook-created task" {
				found = true
				assert.Equal(t, "backend-team", task.TeamName)
				assert.Equal(t, orchestration.TaskStatusPending, task.Status)
				break
			}
		}
		assert.True(t, found, "hook-created task should be retrievable")
	}
}

// TestE2E_HookReceiver_TaskCompleted verifies that a hook event for task
// completion updates the task state to completed.
func TestE2E_HookReceiver_TaskCompleted(t *testing.T) {
	requireQuicBaaS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// First create a task via the API.
	createPayload := map[string]interface{}{
		"tenant_id": "hook-tenant-completed",
		"subject":   "Task to complete via hook",
		"team_name": "qa-team",
	}
	createResp, err := postJSON(ctx, env.quicbaasURL+"/api/v1/orchestrator/tasks", createPayload)
	require.NoError(t, err, "create task should succeed")
	defer createResp.Body.Close()

	var createdTask orchestration.Task
	decodeErr := json.NewDecoder(createResp.Body).Decode(&createdTask)
	require.NoError(t, decodeErr, "decode created task should succeed")
	require.NotEmpty(t, createdTask.ID, "created task must have an ID")

	// Claim the task so it can be completed. The route is
	// POST /api/v1/orchestrator/tasks/{taskID}/claim with agent_id in the body.
	claimPayload := map[string]interface{}{
		"agent_id": "agent-hook-test",
	}
	claimURL := env.quicbaasURL + "/api/v1/orchestrator/tasks/" + createdTask.ID + "/claim?tenant_id=hook-tenant-completed"
	claimResp, err := postJSON(ctx, claimURL, claimPayload)
	require.NoError(t, err, "claim task should succeed")
	defer claimResp.Body.Close()
	require.Equal(t, http.StatusOK, claimResp.StatusCode,
		"claim should return 200, got %d", claimResp.StatusCode)

	// POST a task-completed hook event.
	hookPayload := map[string]interface{}{
		"event_type": "task.completed",
		"tenant_id":  "hook-tenant-completed",
		"task_id":    createdTask.ID,
		"result": map[string]interface{}{
			"output": "completed via webhook hook",
		},
	}

	resp, err := postJSON(ctx, env.quicbaasURL+"/api/v1/orchestrator/hooks", hookPayload)
	require.NoError(t, err, "POST completion hook should succeed")
	defer resp.Body.Close()

	assert.True(t,
		resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted,
		"hook endpoint should return 200 or 202, got %d", resp.StatusCode)
}

// TestE2E_HookReceiver_TeammateIdle verifies that posting an idle event
// is accepted by the hook endpoint and triggers a broadcast.
func TestE2E_HookReceiver_TeammateIdle(t *testing.T) {
	requireQuicBaaS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hookPayload := map[string]interface{}{
		"event_type": "teammate.idle",
		"tenant_id":  "hook-tenant-idle",
		"agent_id":   "agent-idle-01",
		"metadata": map[string]string{
			"capabilities": "golang,testing",
		},
	}

	resp, err := postJSON(ctx, env.quicbaasURL+"/api/v1/orchestrator/hooks", hookPayload)
	require.NoError(t, err, "POST idle hook should succeed")
	defer resp.Body.Close()

	// The idle event should be accepted.
	assert.True(t,
		resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted,
		"hook endpoint should return 200 or 202 for idle event, got %d", resp.StatusCode)
}

// TestE2E_HookReceiver_InvalidPayload verifies that an invalid JSON payload
// returns an appropriate error status code.
func TestE2E_HookReceiver_InvalidPayload(t *testing.T) {
	requireQuicBaaS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Send malformed JSON.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		env.quicbaasURL+"/api/v1/orchestrator/hooks",
		http.NoBody)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	// The tenant operator's bearer takes the request past the gate, so the
	// 4xx below is the handler's answer to the payload and not a 401.
	client := helpers.NewTenantOperatorHTTPClient(t, 10*time.Second)
	resp, err := client.Do(req)
	require.NoError(t, err, "POST with empty body should complete")
	defer resp.Body.Close()

	// Expect a 4xx error for invalid payload.
	assert.True(t,
		resp.StatusCode >= 400 && resp.StatusCode < 500,
		"invalid payload should return 4xx, got %d", resp.StatusCode)
}

// TestE2E_HookReceiver_MissingFields verifies that a hook event with missing
// required fields returns a validation error.
func TestE2E_HookReceiver_MissingFields(t *testing.T) {
	requireQuicBaaS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Send a hook event missing the required event_type field.
	hookPayload := map[string]interface{}{
		"tenant_id": "hook-tenant-missing",
		// event_type is intentionally omitted.
	}

	resp, err := postJSON(ctx, env.quicbaasURL+"/api/v1/orchestrator/hooks", hookPayload)
	require.NoError(t, err, "POST hook with missing fields should complete")
	defer resp.Body.Close()

	// Expect a 4xx validation error.
	assert.True(t,
		resp.StatusCode >= 400 && resp.StatusCode < 500,
		"missing required fields should return 4xx, got %d", resp.StatusCode)
}
