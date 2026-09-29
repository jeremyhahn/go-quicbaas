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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// taskAccountingKeys are the fault and budget keys go-quicbaas#947 put on the
// task representation. Each must be PRESENT on every task, zero or not: a key
// that disappears when its value is zero reads as "the server did not say".
var taskAccountingKeys = []string{
	"attempts", "charged_attempts", "effective_budget", "environment_faults",
	"question_attempts", "conclusion_reason", "fault_class", "fault_origin",
	"block_kind", "blocked_reason", "block_fault_class",
}

// failedOnTheWork and heldOnTheEnvironment are the two failures the tasks
// page could not tell apart before go-quicbaas#947.
func failedOnTheWork() *orchestration.Task {
	return &orchestration.Task{
		ID: "task-work", TenantID: "acme", Status: orchestration.TaskStatusFailed, Attempts: 3,
		ConclusionReason: orchestration.TaskConcludedBudgetExhausted,
		Result: &orchestration.TaskResult{Output: "tests failed", Metadata: map[string]string{
			"failure_class": orchestration.JobFailureClassStalled,
		}},
	}
}

func heldOnTheEnvironment() *orchestration.Task {
	return &orchestration.Task{
		ID: "task-env", TenantID: "acme", Status: orchestration.TaskStatusBlocked,
		Attempts: 3, EnvironmentFaults: 3, AttemptBudget: 4,
		BlockKind: orchestration.TaskBlockKindBlocked, BlockedReason: "forge down",
		BlockFaultClass:  orchestration.FaultForgeUnreachable,
		ConclusionReason: orchestration.TaskConcludedFaultBounded,
	}
}

func TestOrchestratorGetTask_CarriesFaultAndBudget(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.getTaskFunc = func(context.Context, string, string) (*orchestration.Task, error) {
		return heldOnTheEnvironment(), nil
	}

	rec := httptest.NewRecorder()
	server.HandleOrchestratorGetTask(rec, taskLifecycleRequest(t, http.MethodGet, "/api/v1/orchestrator/tasks/task-env?tenant_id=acme", "task-env", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range taskAccountingKeys {
		if _, ok := got[key]; !ok {
			t.Errorf("GET /tasks/{id} omits %q", key)
		}
	}
	want := map[string]any{
		"attempts": float64(3), "charged_attempts": float64(0), "effective_budget": float64(4),
		"environment_faults": float64(3), "conclusion_reason": "fault_bounded",
		"fault_class": "forge_unreachable", "fault_origin": "environment",
		"block_kind": "blocked", "block_fault_class": "forge_unreachable",
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %v, want %v", key, got[key], value)
		}
	}

	// THE SDK'S REST CLIENT DECODES THE SAME BYTES into its own Task, so its
	// tags are checked against what the server writes, not against a copy.
	var sdkTask orchestratorSDK.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &sdkTask); err != nil {
		t.Fatalf("decode into the SDK task: %v", err)
	}
	if sdkTask.FaultOrigin != "environment" || sdkTask.FaultClass != "forge_unreachable" ||
		sdkTask.ConclusionReason != "fault_bounded" || sdkTask.BlockFaultClass != "forge_unreachable" ||
		sdkTask.EffectiveBudget != 4 || sdkTask.ChargedAttempts != 0 || sdkTask.EnvironmentFaults != 3 {
		t.Errorf("SDK task = %+v, want the server's accounting", sdkTask)
	}
}

func TestOrchestratorListTasks_TellsTheEnvironmentFromTheWork(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listTasksFunc = func(context.Context, string, *orchestration.TaskFilter) ([]*orchestration.Task, error) {
		return []*orchestration.Task{failedOnTheWork(), heldOnTheEnvironment()}, nil
	}

	w := httptest.NewRecorder()
	server.HandleOrchestratorListTasks(w, orchReq(http.MethodGet, "/api/v1/orchestrator/tasks?tenant_id=acme", nil, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	_, items := decodePageEnvelope(t, w.Body.Bytes(), "tasks")
	origins := map[any]any{}
	for _, item := range items {
		for _, key := range taskAccountingKeys {
			if _, ok := item[key]; !ok {
				t.Errorf("task %v omits %q in the listing", item["id"], key)
			}
		}
		origins[item["id"]] = item["fault_origin"]
	}
	if origins["task-work"] != "work" || origins["task-env"] != "environment" {
		t.Fatalf("origins = %v, want task-work=work and task-env=environment", origins)
	}
}

func TestOrchestratorGetTask_NotFoundCarriesNoTask(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.getTaskFunc = func(_ context.Context, tenantID, taskID string) (*orchestration.Task, error) {
		return nil, &orchestration.TaskNotFoundError{TenantID: tenantID, TaskID: taskID}
	}

	rec := httptest.NewRecorder()
	server.HandleOrchestratorGetTask(rec, taskLifecycleRequest(t, http.MethodGet, "/api/v1/orchestrator/tasks/nope?tenant_id=acme", "nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := got["fault_origin"]; ok {
		t.Fatal("an error body carries task accounting")
	}
}
