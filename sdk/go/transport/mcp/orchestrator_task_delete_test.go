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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestDeleteTaskToolName pins the SDK method to the tool the MCP server
// registers. The server names the tool after the noun and verb
// (quicbaas_orchestrator_task_delete) while the SDK names the RPC
// (DeleteTask), so the generic derivation would ask for a tool that does not
// exist and the method would be unusable over MCP.
func TestDeleteTaskToolName(t *testing.T) {
	tr := &Transport{}
	if got := tr.mapToToolName("quicbaas.Orchestrator", "DeleteTask"); got != "quicbaas_orchestrator_task_delete" {
		t.Errorf("mapToToolName(DeleteTask) = %q, want quicbaas_orchestrator_task_delete", got)
	}
}

// TestDecodeMCPDeleteTask asserts the tool result becomes the SDK response
// type. Without the transform the client receives the raw tool map and refuses
// it as an invalid response type.
func TestDecodeMCPDeleteTask(t *testing.T) {
	result, err := decodeMCPDeleteTask([]byte(`{"task_id":"task-1","deleted":true,"timestamp":"2026-09-07T00:00:00Z"}`))
	if err != nil {
		t.Fatalf("decodeMCPDeleteTask: %v", err)
	}
	resp, ok := result.(*orchestrator.DeleteTaskResponse)
	if !ok {
		t.Fatalf("result type = %T, want *orchestrator.DeleteTaskResponse", result)
	}
	if resp.TaskID != "task-1" || !resp.Deleted {
		t.Fatalf("decoded response = %+v", resp)
	}
}

func TestDecodeMCPDeleteTask_Malformed(t *testing.T) {
	if _, err := decodeMCPDeleteTask([]byte("{not json")); err == nil {
		t.Fatal("expected an error for a malformed delete result")
	}
}

// TestDeleteTaskTransformIsWired proves the decoder is reachable: a transform
// that exists but is not in the dispatch table never runs.
func TestDeleteTaskTransformIsWired(t *testing.T) {
	h := newOrchestratorMCPHandler()
	raw, err := h.transformResponse("DeleteTask", `{"task_id":"task-1","deleted":true}`)
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	if _, ok := raw.(*orchestrator.DeleteTaskResponse); !ok {
		t.Fatalf("transformResponse returned %T, want *orchestrator.DeleteTaskResponse", raw)
	}
}
