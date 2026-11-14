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
	"context"
	"reflect"
	"testing"

	batchclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/batch"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

const batchServiceName = "quicbaas.Batch"

// TestBatchMCP_Execute drives Execute through the SDK client to the
// quicbaas_batch_execute tool and asserts the encoded operations and the
// typed response.
func TestBatchMCP_Execute(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_batch_execute",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			want := map[string]interface{}{"operations": []interface{}{
				map[string]interface{}{"type": "PUT", "entity_type": "users", "key": "alice", "value": "dmFsdWUx"},
				map[string]interface{}{"type": "DELETE", "entity_type": "users", "key": "bob"},
			}}
			if !reflect.DeepEqual(args, want) {
				t.Errorf("tool arguments = %v, want %v", args, want)
			}
		},
		result: map[string]interface{}{
			"success": true,
			"results": []interface{}{
				map[string]interface{}{"success": true},
				map[string]interface{}{"success": false},
			},
		},
	})
	defer cleanup()

	got, err := batchclient.NewClient(tr).Execute(context.Background(), &batchclient.BatchRequest{
		Operations: []batchclient.BatchOperation{
			{Type: batchclient.OpPut, EntityType: "users", Key: "alice", Value: []byte("value1")},
			{Type: batchclient.OpDelete, EntityType: "users", Key: "bob"},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := &batchclient.BatchResponse{
		Results: []batchclient.BatchResult{{Success: true}, {Success: false}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("response = %+v, want %+v", got, want)
	}
}

// TestBatchMCP_Execute_BadRequest checks a request of the wrong type is
// refused before it reaches the tool.
func TestBatchMCP_Execute_BadRequest(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_batch_execute",
		assertArgs: func(t *testing.T, _ map[string]interface{}) {
			t.Error("a refused request must not reach the tool")
		},
	})
	defer cleanup()

	_, err := tr.Request(context.Background(), batchServiceName, "Execute", &batchclient.BatchOperation{})
	if !sdkerrors.IsInvalidInput(err) {
		t.Errorf("err = %v, want InvalidInput", err)
	}
}

// TestBatchMCP_Execute_ToolError checks a failing tool surfaces as an error,
// not as an empty response.
func TestBatchMCP_Execute_ToolError(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:  "quicbaas_batch_execute",
		result:    map[string]interface{}{"error": "batch commit failed"},
		toolError: true,
	})
	defer cleanup()

	_, err := batchclient.NewClient(tr).Execute(context.Background(), &batchclient.BatchRequest{
		Operations: []batchclient.BatchOperation{{Type: batchclient.OpDelete, EntityType: "users", Key: "bob"}},
	})
	if err == nil {
		t.Error("Execute returned no error for a failing tool")
	}
}

// TestBatchMCP_ToolNames pins the Execute tool name the contract test in
// pkg/api/mcp checks against the server registry.
func TestBatchMCP_ToolNames(t *testing.T) {
	if got := ToolName(batchServiceName, "Execute"); got != "quicbaas_batch_execute" {
		t.Errorf("ToolName(Execute) = %q, want quicbaas_batch_execute", got)
	}
}
