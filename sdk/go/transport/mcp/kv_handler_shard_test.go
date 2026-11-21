package mcp

import (
	"context"
	"sync"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// TestTransport_Request_KVShardIDSentAsShardID pins the KV verbs' shard
// argument: the entity tools read only shardID, so every verb must carry
// shardID and never the request type's shard_id json key.
func TestTransport_Request_KVShardIDSentAsShardID(t *testing.T) {
	replies := map[string]string{
		"put":    `{"id":7,"version":1,"timestamp":2}`,
		"get":    `{"id":7,"data":"dGVzdA==","version":1,"timestamp":2,"found":true}`,
		"delete": `{"deleted":true,"timestamp":3}`,
		"list":   `{"entities":[],"nextPageToken":"","totalCount":0}`,
	}
	var mu sync.Mutex
	seen := map[string]bool{}
	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	for verb, reply := range replies {
		verb, reply := verb, reply
		srv.AddTool(mcpgo.NewTool("quicbaas_kv_"+verb), func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			args := req.GetArguments()
			mu.Lock()
			seen[verb] = true
			mu.Unlock()
			if v, ok := args["shardID"].(float64); !ok || v != 2 {
				t.Errorf("%s: shardID = %T %v, want 2", verb, args["shardID"], args["shardID"])
			}
			if v, ok := args["shard_id"]; ok {
				t.Errorf("%s: shard_id = %v sent, want no shard_id key", verb, v)
			}
			return mcpgo.NewToolResultText(reply), nil
		})
	}
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv)
	defer httpSrv.Close()

	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()

	ctx := context.Background()
	calls := []struct {
		method string
		req    interface{}
	}{
		{"Put", &service.PutRequest{EntityType: "users", ID: 7, Data: []byte("test"), ShardID: 2}},
		{"Get", &service.GetRequest{EntityType: "users", ID: 7, ShardID: 2}},
		{"Delete", &service.DeleteRequest{EntityType: "users", ID: 7, ShardID: 2}},
		{"List", &service.ListRequest{EntityType: "users", ShardID: 2}},
	}
	for _, c := range calls {
		if _, err := tr.Request(ctx, "quicbaas.KV", c.method, c.req); err != nil {
			t.Fatalf("%s: %v", c.method, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for verb := range replies {
		if !seen[verb] {
			t.Errorf("quicbaas_kv_%s was never called", verb)
		}
	}
}
