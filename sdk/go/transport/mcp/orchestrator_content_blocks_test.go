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
	"reflect"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

func TestMCPTransformInference_KeepsToolBlockMetadata(t *testing.T) {
	h := newOrchestratorMCPHandler()
	result := `{"content":[` +
		`{"type":"tool_use","text":"go test","tool_name":"Bash","tool_use_id":"toolu_01","bytes":42},` +
		`{"type":"tool_result","tool_use_id":"toolu_01","is_error":true,"bytes":7}],` +
		`"model":"m","stop_reason":"end_turn"}`

	out, err := h.transformResponse("Inference", result)
	if err != nil {
		t.Fatalf("transformResponse() error = %v", err)
	}
	resp, ok := out.(*orchestrator.InferenceResponse)
	if !ok {
		t.Fatalf("result type = %T, want *orchestrator.InferenceResponse", out)
	}
	want := []orchestrator.InferenceContentBlock{
		{Type: "tool_use", Text: "go test", ToolName: "Bash", ToolUseID: "toolu_01", Bytes: 42},
		{Type: "tool_result", ToolUseID: "toolu_01", IsError: true, Bytes: 7},
	}
	if !reflect.DeepEqual(resp.Content, want) {
		t.Errorf("Content = %+v, want %+v", resp.Content, want)
	}
}

func TestMCPTransformInference_UndecodableResultIsAnError(t *testing.T) {
	h := newOrchestratorMCPHandler()
	if _, err := h.transformResponse("Inference", `{"content":"not a list"}`); err == nil {
		t.Fatal("transformResponse() error = nil, want a decode error")
	}
}
