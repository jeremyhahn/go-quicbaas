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
	"encoding/base64"
	"sync"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// guardedToolServer is a fake MCP server registering one of qrdb's guarded
// KV tools. It records the arguments it was called with and answers with
// {field: decided}, the shape qrdb's guardedOutcome renders.
type guardedToolServer struct {
	mu   sync.Mutex
	args map[string]interface{}
}

func (g *guardedToolServer) start(t *testing.T, tool, field string, decided bool) *Transport {
	t.Helper()
	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	srv.AddTool(mcpgo.NewTool(tool), func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		g.mu.Lock()
		g.args = req.GetArguments()
		g.mu.Unlock()
		return mcpgo.NewToolResultText(string(mustMarshalJSON(map[string]bool{field: decided}))), nil
	})
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv)
	t.Cleanup(httpSrv.Close)

	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return tr
}

func (g *guardedToolServer) arguments() map[string]interface{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.args
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestToolName_KVConditionalWrites(t *testing.T) {
	cases := map[string]string{
		"CompareAndSwap": "qrdb_compare_and_swap",
		"PutIfAbsent":    "qrdb_put_if_absent",
	}
	for method, want := range cases {
		if got := ToolName("quicbaas.KV", method); got != want {
			t.Errorf("ToolName(quicbaas.KV, %s) = %q, want %q", method, got, want)
		}
	}
}

func TestTransport_CompareAndSwap(t *testing.T) {
	tests := []struct {
		name    string
		decided bool
	}{
		{"swapped", true},
		{"conflict", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var g guardedToolServer
			tr := g.start(t, "qrdb_compare_and_swap", "swapped", tt.decided)

			result, err := tr.Request(context.Background(), "quicbaas.KV", "CompareAndSwap",
				&service.CompareAndSwapRequest{EntityType: "users", ID: 42, Expect: []byte("old"), Set: []byte("new")})
			if err != nil {
				t.Fatalf("Request: %v", err)
			}
			resp, ok := result.(*service.ConditionalPutResponse)
			if !ok {
				t.Fatalf("result type = %T, want *service.ConditionalPutResponse", result)
			}
			if resp.Applied != tt.decided {
				t.Errorf("Applied = %v, want %v", resp.Applied, tt.decided)
			}

			args := g.arguments()
			want := map[string]string{"key": "users:42", "expected": b64("old"), "next": b64("new")}
			for k, v := range want {
				if args[k] != v {
					t.Errorf("argument %s = %v, want %q", k, args[k], v)
				}
			}
		})
	}
}

func TestTransport_PutIfAbsent(t *testing.T) {
	tests := []struct {
		name    string
		decided bool
	}{
		{"stored", true},
		{"conflict", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var g guardedToolServer
			tr := g.start(t, "qrdb_put_if_absent", "stored", tt.decided)

			result, err := tr.Request(context.Background(), "quicbaas.KV", "PutIfAbsent",
				&service.PutIfAbsentRequest{EntityType: "locks", ID: 7, Data: []byte("owner")})
			if err != nil {
				t.Fatalf("Request: %v", err)
			}
			resp, ok := result.(*service.ConditionalPutResponse)
			if !ok {
				t.Fatalf("result type = %T, want *service.ConditionalPutResponse", result)
			}
			if resp.Applied != tt.decided {
				t.Errorf("Applied = %v, want %v", resp.Applied, tt.decided)
			}

			args := g.arguments()
			if args["key"] != "locks:7" || args["value"] != b64("owner") {
				t.Errorf("arguments = %v, want key locks:7 and value %q", args, b64("owner"))
			}
		})
	}
}

// TestTransport_CompareAndSwap_AnswerWithoutDecision rejects a tool answer
// that lacks the decision field rather than reading it as applied=false.
func TestTransport_CompareAndSwap_AnswerWithoutDecision(t *testing.T) {
	var g guardedToolServer
	tr := g.start(t, "qrdb_compare_and_swap", "unexpected", true)

	_, err := tr.Request(context.Background(), "quicbaas.KV", "CompareAndSwap",
		&service.CompareAndSwapRequest{EntityType: "users", ID: 1})
	if !errors.IsServerError(err) {
		t.Fatalf("err = %v, want an internal error", err)
	}
}

func TestKVConditionalArguments_InvalidInput(t *testing.T) {
	tests := []struct {
		name string
		args func(interface{}) (map[string]interface{}, error)
		req  interface{}
	}{
		{"CompareAndSwap wrong type", compareAndSwapArguments, &service.PutRequest{}},
		{"CompareAndSwap nil", compareAndSwapArguments, (*service.CompareAndSwapRequest)(nil)},
		{"CompareAndSwap no entity type", compareAndSwapArguments, &service.CompareAndSwapRequest{ID: 1}},
		{"PutIfAbsent wrong type", putIfAbsentArguments, &service.PutRequest{}},
		{"PutIfAbsent nil", putIfAbsentArguments, (*service.PutIfAbsentRequest)(nil)},
		{"PutIfAbsent no entity type", putIfAbsentArguments, &service.PutIfAbsentRequest{ID: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.args(tt.req); !errors.IsInvalidInput(err) {
				t.Errorf("err = %v, want an invalid-input error", err)
			}
		})
	}
}
