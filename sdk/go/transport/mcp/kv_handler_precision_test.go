package mcp

import (
	"context"
	"strconv"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// kvIDAbove2To53 is 2^53+1: the first integer a float64 cannot hold, so any
// float64 step on the KV path turns it into 2^53.
const kvIDAbove2To53 uint64 = 9007199254740993

// kvToolID reads the id argument the way the quicbaas server's uint64Arg
// does: a JSON number arrives as float64 (mcp-go decodes tool arguments
// with plain encoding/json), a decimal string is parsed exactly.
func kvToolID(t *testing.T, args map[string]any) uint64 {
	t.Helper()
	switch v := args["id"].(type) {
	case float64:
		return uint64(v)
	case string:
		id, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			t.Errorf("id %q: %v", v, err)
		}
		return id
	}
	t.Errorf("id = %T %v, want an integer", args["id"], args["id"])
	return 0
}

// TestTransport_Request_KVIDAbove2To53RoundTrips proves the MCP KV path keeps
// an entity ID above 2^53 exact both ways for Put, Get and Delete: the tool
// reads the ID the client sent, and the client returns the ID the tool
// answered.
func TestTransport_Request_KVIDAbove2To53RoundTrips(t *testing.T) {
	idToken := strconv.FormatUint(kvIDAbove2To53, 10)
	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	tool := func(answer string) mcpserver.ToolHandlerFunc {
		return func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			if got := kvToolID(t, req.GetArguments()); got != kvIDAbove2To53 {
				t.Errorf("%s: tool read id %d, want %d", req.Params.Name, got, kvIDAbove2To53)
			}
			return mcpgo.NewToolResultText(answer), nil
		}
	}
	srv.AddTool(mcpgo.NewTool("quicbaas_kv_put"), tool(`{"id":`+idToken+`,"version":1,"timestamp":2}`))
	srv.AddTool(mcpgo.NewTool("quicbaas_kv_get"), tool(`{"id":`+idToken+`,"data":"dGVzdA==","version":1,"timestamp":2,"found":true}`))
	srv.AddTool(mcpgo.NewTool("quicbaas_kv_delete"), tool(`{"deleted":true,"timestamp":3}`))
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv)
	defer httpSrv.Close()

	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()
	ctx := context.Background()

	result, err := tr.Request(ctx, "quicbaas.KV", "Put",
		&service.PutRequest{EntityType: "users", ID: kvIDAbove2To53, Data: []byte("test")})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if put, ok := result.(*service.PutResponse); !ok || put.ID != kvIDAbove2To53 {
		t.Errorf("Put answered %T %+v, want id %d", result, result, kvIDAbove2To53)
	}

	result, err = tr.Request(ctx, "quicbaas.KV", "Get",
		&service.GetRequest{EntityType: "users", ID: kvIDAbove2To53})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got, ok := result.(*service.GetResponse); !ok || got.ID != kvIDAbove2To53 || !got.Found {
		t.Errorf("Get answered %T %+v, want found id %d", result, result, kvIDAbove2To53)
	}

	result, err = tr.Request(ctx, "quicbaas.KV", "Delete",
		&service.DeleteRequest{EntityType: "users", ID: kvIDAbove2To53})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if del, ok := result.(*service.DeleteResponse); !ok || !del.Deleted {
		t.Errorf("Delete answered %T %+v, want deleted", result, result)
	}
}
