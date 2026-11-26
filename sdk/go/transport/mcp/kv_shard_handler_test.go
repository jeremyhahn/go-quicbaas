//go:build mcp

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

package mcp_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/mock"

	// pkg/api/mcp imports this package for the MCP wire error codes, so
	// only an external test package may import it back.
	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// TestTransport_KVShardReachesHandler drives each KV verb from the SDK MCP
// transport through a real MCP server into the server's entity tools and
// checks the handler receives the shard the caller asked for.
func TestTransport_KVShardReachesHandler(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}

	var mu sync.Mutex
	shards := make(map[string]uint64)
	record := func(verb string, shard uint64) {
		mu.Lock()
		defer mu.Unlock()
		shards[verb] = shard
	}

	qb := testutil.NewMockQuicBaaS()
	qb.On("Put", mock.Anything, mock.Anything).Run(func(a mock.Arguments) {
		record("Put", a.Get(1).(*service.PutRequest).ShardID)
	}).Return(&service.PutResponse{ID: 7, Version: 1}, nil)
	qb.On("Get", mock.Anything, mock.Anything).Run(func(a mock.Arguments) {
		record("Get", a.Get(1).(*service.GetRequest).ShardID)
	}).Return(&service.GetResponse{ID: 7, Found: true, Data: []byte("v")}, nil)
	qb.On("Delete", mock.Anything, mock.Anything).Run(func(a mock.Arguments) {
		record("Delete", a.Get(1).(*service.DeleteRequest).ShardID)
	}).Return(&service.DeleteResponse{Deleted: true}, nil)
	qb.On("List", mock.Anything, mock.Anything, mock.Anything).Run(func(a mock.Arguments) {
		record("List", a.Get(1).(*service.ListRequest).ShardID)
		close(a.Get(2).(chan<- *service.ListResponse))
	}).Return(nil)

	registry := mcpapi.NewToolRegistry(qb, log)

	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	verbs := []string{"Put", "Get", "Delete", "List"}
	for _, verb := range verbs {
		srv.AddTool(mcpgo.NewTool(mcp.ToolName("quicbaas.KV", verb)),
			func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
				result, err := registry.ExecuteTool(ctx, req.Params.Name, req.GetArguments())
				if err != nil {
					return mcpgo.NewToolResultError(err.Error()), nil
				}
				raw, err := json.Marshal(result)
				if err != nil {
					return mcpgo.NewToolResultError(err.Error()), nil
				}
				return mcpgo.NewToolResultText(string(raw)), nil
			})
	}
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv)
	defer httpSrv.Close()

	tr, err := mcp.New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()

	requests := map[string]interface{}{
		"Put":    &service.PutRequest{EntityType: "users", ID: 7, Data: []byte("v"), ShardID: 2},
		"Get":    &service.GetRequest{EntityType: "users", ID: 7, ShardID: 2},
		"Delete": &service.DeleteRequest{EntityType: "users", ID: 7, ShardID: 2},
		"List":   &service.ListRequest{EntityType: "users", PageSize: 5, ShardID: 2},
	}
	for _, verb := range verbs {
		if _, err := tr.Request(context.Background(), "quicbaas.KV", verb, requests[verb]); err != nil {
			t.Fatalf("%s: %v", verb, err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	for _, verb := range verbs {
		got, ok := shards[verb]
		if !ok {
			t.Errorf("%s never reached the handler", verb)
			continue
		}
		if got != 2 {
			t.Errorf("%s reached the handler on shard %d, want 2", verb, got)
		}
	}
}
