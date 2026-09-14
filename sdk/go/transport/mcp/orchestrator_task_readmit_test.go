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

// TestReadmitTaskToolName pins the SDK method to the tool the MCP server
// registers: the server names the tool after the noun and verb, the SDK names
// the RPC.
func TestReadmitTaskToolName(t *testing.T) {
	tr := &Transport{}
	if got := tr.mapToToolName("quicbaas.Orchestrator", "ReadmitTask"); got != "quicbaas_orchestrator_task_readmit" {
		t.Errorf("mapToToolName(ReadmitTask) = %q, want quicbaas_orchestrator_task_readmit", got)
	}
}

// TestDecodeMCPReadmitTask asserts the tool result becomes the SDK response
// type.
func TestDecodeMCPReadmitTask(t *testing.T) {
	result, err := decodeMCPReadmitTask([]byte(`{"task_id":"task-26","readmitted":true,"timestamp":"2026-09-11T00:00:00Z"}`))
	if err != nil {
		t.Fatalf("decodeMCPReadmitTask: %v", err)
	}
	resp, ok := result.(*orchestrator.ReadmitTaskResponse)
	if !ok {
		t.Fatalf("result type = %T, want *orchestrator.ReadmitTaskResponse", result)
	}
	if resp.TaskID != "task-26" || !resp.Readmitted {
		t.Fatalf("decoded response = %+v", resp)
	}
}

func TestDecodeMCPReadmitTask_Malformed(t *testing.T) {
	if _, err := decodeMCPReadmitTask([]byte("{not json")); err == nil {
		t.Fatal("expected an error for a malformed readmit result")
	}
}

// TestReadmitTaskTransformIsWired is the test that would have caught the bug:
// the tool was registered and the RPC was reachable, but the method was absent
// from the transform table, so a SUCCESSFUL readmit fell through as the raw
// tool map and the client refused it as an invalid response type. A decoder
// that exists but is not in the dispatch table never runs.
func TestReadmitTaskTransformIsWired(t *testing.T) {
	h := newOrchestratorMCPHandler()
	raw, err := h.transformResponse("ReadmitTask", `{"task_id":"task-26","readmitted":true}`)
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	resp, ok := raw.(*orchestrator.ReadmitTaskResponse)
	if !ok {
		t.Fatalf("transformResponse returned %T, want *orchestrator.ReadmitTaskResponse", raw)
	}
	if !resp.Readmitted {
		t.Fatalf("decoded response = %+v, want the readmit reported", resp)
	}
}

// TestEveryTaskLifecycleToolHasATransform closes the class rather than the
// instance: each task-lifecycle RPC the transport maps to a tool must also
// decode its result, or the verb is unusable over MCP in exactly the way
// ReadmitTask was. It caught BlockTask having the identical gap, which is
// fixed with it.
func TestEveryTaskLifecycleToolHasATransform(t *testing.T) {
	for _, method := range []string{"BlockTask", "ReadmitTask", "DeleteTask"} {
		if _, ok := orchestratorMCPTransforms[method]; !ok {
			t.Errorf("%s maps to a tool but has no result transform", method)
		}
	}
}

// TestDecodeMCPBlockTask asserts the block tool result becomes the SDK type.
func TestDecodeMCPBlockTask(t *testing.T) {
	result, err := decodeMCPBlockTask([]byte(`{"task_id":"task-26","kind":"needs_info","blocked":true}`))
	if err != nil {
		t.Fatalf("decodeMCPBlockTask: %v", err)
	}
	resp, ok := result.(*orchestrator.BlockTaskResponse)
	if !ok {
		t.Fatalf("result type = %T, want *orchestrator.BlockTaskResponse", result)
	}
	if resp.TaskID != "task-26" || !resp.Blocked || resp.Kind != "needs_info" {
		t.Fatalf("decoded response = %+v", resp)
	}
}

func TestDecodeMCPBlockTask_Malformed(t *testing.T) {
	if _, err := decodeMCPBlockTask([]byte("{not json")); err == nil {
		t.Fatal("expected an error for a malformed block result")
	}
}
