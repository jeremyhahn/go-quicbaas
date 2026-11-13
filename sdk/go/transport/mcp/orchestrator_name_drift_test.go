// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package mcp

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// The fleet, task-create, decompose and artifact verbs reach tools named
// after the surface, not the RPC, and each tool's result differs from the SDK
// response in shape. The full round trip is pinned server-side by
// pkg/api/mcp's TestNameDrift_* tests; these cover the decoders' refusals.

func TestMCPNameDriftVerbsMapToServerTools(t *testing.T) {
	want := map[string]string{
		"RegisterFleetNode": "quicbaas_fleet_register",
		"RemoveFleetNode":   "quicbaas_fleet_remove",
		"DisableFleetNode":  "quicbaas_fleet_disable",
		"EnableFleetNode":   "quicbaas_fleet_enable",
		"CreateTask":        "quicbaas_orchestrator_task_create",
		"DecomposeTask":     "quicbaas_orchestrator_decompose",
		"GetTaskArtifact":   "quicbaas_task_get_artifact",

		"GetTask":             "quicbaas_orchestrator_task_get",
		"ClaimTask":           "quicbaas_orchestrator_task_claim",
		"CompleteTask":        "quicbaas_orchestrator_task_complete",
		"SubmitTaskArtifacts": "quicbaas_task_submit_artifacts",
		"SpawnChildJob":       "quicbaas_orchestrator_spawn_child_job",
	}
	for method, tool := range want {
		if got := ToolName("quicbaas.Orchestrator", method); got != tool {
			t.Errorf("ToolName(%s) = %q, want %q", method, got, tool)
		}
		if _, ok := orchestratorMCPTransforms[method]; !ok {
			t.Errorf("%s has no result decoder", method)
		}
	}
}

func TestMCPNameDriftDecodersRejectUndecodableResults(t *testing.T) {
	h := newOrchestratorMCPHandler()
	for _, method := range []string{
		"RegisterFleetNode", "RemoveFleetNode", "DisableFleetNode", "EnableFleetNode",
		"CreateTask", "DecomposeTask", "GetTaskArtifact",
		"GetTask", "ClaimTask", "CompleteTask", "SubmitTaskArtifacts", "SpawnChildJob",
	} {
		_, err := h.transformResponse(method, "not json at all")
		if !errors.IsServerError(err) {
			t.Errorf("%s: err = %v, want a coded internal decode error", method, err)
		}
	}
}

func TestMCPTransformGetTaskRequiresTask(t *testing.T) {
	h := newOrchestratorMCPHandler()

	_, err := h.transformResponse("GetTask", `{"timestamp":"2026-01-01T00:00:00Z"}`)
	if !errors.IsServerError(err) {
		t.Fatalf("err = %v, want a coded internal error for a result with no task", err)
	}

	got, err := h.transformResponse("GetTask", `{"task":{"id":"task-1"}}`)
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	task, ok := got.(*orchestrator.Task)
	if !ok || task.ID != "task-1" {
		t.Fatalf("got %#v, want the task with id task-1", got)
	}
}

func TestMCPWorkflowVerbsMapToServerTools(t *testing.T) {
	want := map[string]string{
		"CreateWorkflow": "quicbaas_orchestrator_create_workflow",
		"GetWorkflow":    "quicbaas_orchestrator_get_workflow",
		"UpdateWorkflow": "quicbaas_orchestrator_update_workflow",
		"DeleteWorkflow": "quicbaas_orchestrator_delete_workflow",
	}
	for method, tool := range want {
		if got := ToolName("quicbaas.Orchestrator", method); got != tool {
			t.Errorf("ToolName(%s) = %q, want %q", method, got, tool)
		}
	}
}

func TestMCPFleetNodeAndBackendModelsToolNames(t *testing.T) {
	want := map[string]string{
		"GetFleetNode":      "quicbaas_fleet_status",
		"ListBackendModels": "quicbaas_orchestrator_list_backend_models",
	}
	for method, tool := range want {
		if got := ToolName("quicbaas.Orchestrator", method); got != tool {
			t.Errorf("ToolName(%s) = %q, want %q", method, got, tool)
		}
	}
}

func TestMCPTransformGetFleetNode(t *testing.T) {
	h := newOrchestratorMCPHandler()
	if _, err := h.transformResponse("GetFleetNode", "not json at all"); !errors.IsServerError(err) {
		t.Errorf("err = %v, want a coded internal decode error", err)
	}
	if _, err := h.transformResponse("GetFleetNode", `{"timestamp":"now"}`); !errors.IsServerError(err) {
		t.Errorf("err = %v, want a coded internal error for a result with no node", err)
	}

	got, err := h.transformResponse("GetFleetNode",
		`{"node":{"id":"node-1","tenant_id":"t1","backend":"claude","backends":["claude","codex"]},"timestamp":"now"}`)
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	node, ok := got.(*orchestrator.FleetNode)
	if !ok || node.ID != "node-1" || node.TenantID != "t1" || len(node.Backends) != 2 {
		t.Fatalf("got %#v, want fleet node node-1", got)
	}
}

func TestMCPTransformListBackendModels(t *testing.T) {
	h := newOrchestratorMCPHandler()
	if _, err := h.transformResponse("ListBackendModels", "not json at all"); !errors.IsServerError(err) {
		t.Errorf("err = %v, want a coded internal decode error", err)
	}

	got, err := h.transformResponse("ListBackendModels", `{"backend":"ollama","models":["llama3"],"count":1}`)
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	resp, ok := got.(*orchestrator.ListBackendModelsResponse)
	if !ok || resp.Backend != "ollama" || resp.Count != 1 || len(resp.Models) != 1 || resp.Models[0] != "llama3" {
		t.Fatalf("got %#v, want the ollama model list", got)
	}
}

func TestMCPTransformWorkflowRequiresWorkflow(t *testing.T) {
	h := newOrchestratorMCPHandler()
	for _, method := range []string{"CreateWorkflow", "GetWorkflow", "UpdateWorkflow"} {
		if _, err := h.transformResponse(method, "not json at all"); !errors.IsServerError(err) {
			t.Errorf("%s: err = %v, want a coded internal decode error", method, err)
		}
		if _, err := h.transformResponse(method, `{"name":"wf"}`); !errors.IsServerError(err) {
			t.Errorf("%s: err = %v, want a coded internal error for a result with no workflow", method, err)
		}

		got, err := h.transformResponse(method, `{"workflow":{"name":"wf","mode":"constrained"}}`)
		if err != nil {
			t.Fatalf("%s: transformResponse: %v", method, err)
		}
		wf, ok := got.(*orchestrator.WorkflowDefinition)
		if !ok || wf.Name != "wf" || wf.Mode != orchestrator.WorkflowModeConstrained {
			t.Fatalf("%s: got %#v, want the workflow wf", method, got)
		}
	}
}

func TestMCPTransformCreateTask(t *testing.T) {
	h := newOrchestratorMCPHandler()

	t.Run("a flattened create error becomes an invalid-input error", func(t *testing.T) {
		result := map[string]interface{}{"task_id": "", "error": "no online agent matches constraints"}
		_, err := h.transformResponse("CreateTask", result)
		if !errors.IsInvalidInput(err) {
			t.Fatalf("err = %v, want invalid input", err)
		}
	})

	t.Run("a summary without the task record is refused", func(t *testing.T) {
		result := map[string]interface{}{"task_id": "task-1", "status": "pending"}
		_, err := h.transformResponse("CreateTask", result)
		if errors.Code(err) != errors.CodeInternal {
			t.Fatalf("err = %v, want internal", err)
		}
	})
}
