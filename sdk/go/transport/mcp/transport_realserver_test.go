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

// transport_realserver_test.go covers the core MCP transport contract end to
// end against a real in-process Model Context Protocol server
// (github.com/mark3labs/mcp-go/server, Streamable HTTP): a successful tool
// call and response decode, a tool-level error, a connection failure, a
// context cancellation, and the auto-authenticate (login) flow. This is the
// replacement for the retired hand-rolled JSON-RPC transport's equivalent
// coverage — only the wire engine changed; Request/doRequest's tool-name
// mapping and argument/response transforms are unchanged and still covered
// by TestTransport_MapToToolName and TestTransport_ConvertToArguments in
// transport_test.go.

package mcp

import (
	"context"
	"net/http"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// TestTransport_Request_Success drives a full Request() round trip against a
// real MCP server: "quicbaas.KV"/"Get" maps to quicbaas's entity
// tool "quicbaas_kv_get", and the JSON the tool returns decodes into the
// *service.GetResponse the KV client accepts from every transport.
func TestTransport_Request_Success(t *testing.T) {
	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	srv.AddTool(mcpgo.NewTool("quicbaas_kv_get"), func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		if args["id"] != float64(123) {
			t.Errorf("id = %v, want 123", args["id"])
		}
		return mcpgo.NewToolResultText(string(mustMarshalJSON(map[string]interface{}{
			"id":    123,
			"found": true,
			"data":  "dGVzdA==",
		}))), nil
	})
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv)
	defer httpSrv.Close()

	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KV", "Get", map[string]interface{}{"id": 123})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	got, ok := result.(*service.GetResponse)
	if !ok {
		t.Fatalf("result type = %T, want *service.GetResponse", result)
	}
	if !got.Found || got.ID != 123 || string(got.Data) != "test" {
		t.Errorf("GetResponse = %+v, want found id 123 data \"test\"", got)
	}
	if !tr.IsInitialized() {
		t.Error("expected transport to be initialized after a request")
	}
	if info := tr.ServerInfo(); info == nil || info.Name != "TestServer" {
		t.Errorf("ServerInfo() = %+v, want name TestServer", info)
	}
}

// TestTransport_Request_KVPutReachesEntityTool drives a KV Put over a real
// MCP server and proves it reaches quicbaas's entity tool quicbaas_kv_put
// (not qrdb_put, which a full node serves as go-qrdb's key/value tool) with
// the entity arguments that tool reads: entity type, id and base64 data.
func TestTransport_Request_KVPutReachesEntityTool(t *testing.T) {
	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	var called atomic.Bool
	srv.AddTool(mcpgo.NewTool("quicbaas_kv_put"), func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		called.Store(true)
		args := req.GetArguments()
		if args["entity_type"] != "users" {
			t.Errorf("entity_type = %v, want users", args["entity_type"])
		}
		if args["id"] != "7" {
			t.Errorf("id = %v, want the decimal string 7", args["id"])
		}
		if args["data"] != "dGVzdA==" {
			t.Errorf("data = %v, want base64 of \"test\"", args["data"])
		}
		return mcpgo.NewToolResultText(`{"id":7,"version":3,"timestamp":11}`), nil
	})
	srv.AddTool(mcpgo.NewTool("quicbaas_kv_list"), func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		if args["pageSize"] != float64(5) || args["pageToken"] != "c1" {
			t.Errorf("list paging args = %v, want pageSize 5 and pageToken c1", args)
		}
		return mcpgo.NewToolResultText(`{"entities":[{"entity_type":"users","id":7,"data":"dGVzdA==","version":3}],` +
			`"count":1,"nextPageToken":"c2","totalCount":9}`), nil
	})
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv)
	defer httpSrv.Close()

	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()

	req := &service.PutRequest{EntityType: "users", ID: 7, Data: []byte("test")}
	result, err := tr.Request(context.Background(), "quicbaas.KV", "Put", req)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if !called.Load() {
		t.Fatal("quicbaas_kv_put was never called")
	}
	put, ok := result.(*service.PutResponse)
	if !ok {
		t.Fatalf("Put result type = %T, want *service.PutResponse", result)
	}
	if put.ID != 7 || put.Version != 3 || put.Timestamp != 11 {
		t.Errorf("PutResponse = %+v, want id 7 version 3 timestamp 11", put)
	}

	listed, err := tr.Request(context.Background(), "quicbaas.KV", "List",
		&service.ListRequest{EntityType: "users", PageSize: 5, PageToken: "c1"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	page, ok := listed.(*service.ListResponse)
	if !ok {
		t.Fatalf("List result type = %T, want *service.ListResponse", listed)
	}
	if len(page.Entities) != 1 || page.Entities[0].ID != 7 || string(page.Entities[0].Data) != "test" ||
		page.NextPageToken != "c2" || page.TotalCount != 9 {
		t.Errorf("ListResponse = %+v, want one entity id 7, next c2, total 9", page)
	}
}

// TestTransport_Request_ToolError verifies a tool-level MCP error
// (IsError=true) surfaces as an SDK error carrying the tool's text message.
func TestTransport_Request_ToolError(t *testing.T) {
	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	srv.AddTool(mcpgo.NewTool("quicbaas_kv_get"), func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return mcpgo.NewToolResultError("Key not found"), nil
	})
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv)
	defer httpSrv.Close()

	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second, MaxRetries: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()

	_, err = tr.Request(context.Background(), "quicbaas.KV", "Get", map[string]interface{}{"id": 123})
	if err == nil {
		t.Fatal("expected error from tool error response")
	}
	if errors.Message(err) != "Key not found" {
		t.Errorf("Message(err) = %q, want %q", errors.Message(err), "Key not found")
	}
}

// TestTransport_Request_ConnectionFailure verifies Request surfaces a
// typed, non-nil error when the server is unreachable, instead of hanging
// or panicking.
func TestTransport_Request_ConnectionFailure(t *testing.T) {
	// Port 1 is a privileged, essentially never-listening port; connection
	// is refused immediately rather than timing out, keeping this test fast.
	tr, err := New(&transport.Config{
		Address:    "http://127.0.0.1:1",
		Timeout:    2 * time.Second,
		MaxRetries: 0,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = tr.Request(ctx, "quicbaas.KV", "Get", map[string]interface{}{"id": 123})
	if err == nil {
		t.Fatal("expected a connection error, got nil")
	}
}

// TestTransport_Request_ContextCancellation verifies a caller-canceled
// context aborts the in-flight tool call with a canceled error rather than
// completing successfully or hanging.
func TestTransport_Request_ContextCancellation(t *testing.T) {
	block := make(chan struct{})
	defer close(block)

	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	srv.AddTool(mcpgo.NewTool("quicbaas_kv_get"), func(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		// Block until the client gives up (context canceled) or the test
		// cleans up, whichever happens first.
		select {
		case <-ctx.Done():
		case <-block:
		}
		return mcpgo.NewToolResultText("{}"), nil
	})
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv)
	defer httpSrv.Close()

	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 10 * time.Second, MaxRetries: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err = tr.Request(ctx, "quicbaas.KV", "Get", map[string]interface{}{"id": 123})
	if err == nil {
		t.Fatal("expected a canceled error, got nil")
	}
	if !errors.IsCanceled(err) {
		t.Errorf("IsCanceled(err) = false for %v", err)
	}
}

// TestTransport_AuthSession drives the WithCredentials auto-authenticate
// flow: the first non-public tool call triggers a quicbaas_login tool call,
// the access token is extracted from the login response, and every
// subsequent request carries it as a Bearer Authorization header.
func TestTransport_AuthSession(t *testing.T) {
	var loginCalls int
	var sawAuthHeader string

	type authHeaderKey struct{}

	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	srv.AddTool(mcpgo.NewTool("quicbaas_login"), func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		loginCalls++
		args := req.GetArguments()
		if args["username"] != "alice" || args["password"] != "s3cr3t" {
			t.Errorf("login args = %+v", args)
		}
		return mcpgo.NewToolResultText(string(mustMarshalJSON(map[string]interface{}{
			"access_token": "tok-123",
		}))), nil
	})
	srv.AddTool(mcpgo.NewTool("quicbaas_kv_get"), func(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if v, ok := ctx.Value(authHeaderKey{}).(string); ok {
			sawAuthHeader = v
		}
		return mcpgo.NewToolResultText("{}"), nil
	})

	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv,
		mcpserver.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			return context.WithValue(ctx, authHeaderKey{}, r.Header.Get("Authorization"))
		}),
	)
	defer httpSrv.Close()

	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second},
		WithCredentials("alice", "s3cr3t"),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()

	if _, err := tr.Request(context.Background(), "quicbaas.KV", "Get", map[string]interface{}{"id": 1}); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if loginCalls != 1 {
		t.Errorf("login calls = %d, want 1", loginCalls)
	}
	if sawAuthHeader != "Bearer tok-123" {
		t.Errorf("Authorization header = %q, want %q", sawAuthHeader, "Bearer tok-123")
	}

	// A second request must NOT re-authenticate: the session is cached.
	if _, err := tr.Request(context.Background(), "quicbaas.KV", "Get", map[string]interface{}{"id": 2}); err != nil {
		t.Fatalf("Request (2nd): %v", err)
	}
	if loginCalls != 1 {
		t.Errorf("login calls after 2nd request = %d, want 1 (session should be cached)", loginCalls)
	}
}

func TestTransport_EnsureAuthenticatedRacesWithClose(t *testing.T) {
	var loginCalls atomic.Int32

	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	srv.AddTool(mcpgo.NewTool("quicbaas_login"), func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		loginCalls.Add(1)
		args := req.GetArguments()
		if args["password"] != "s3cr3t" {
			t.Errorf("login password = %q", args["password"])
		}
		return mcpgo.NewToolResultText(string(mustMarshalJSON(map[string]interface{}{
			"access_token": "tok-123",
		}))), nil
	})
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv)
	defer httpSrv.Close()

	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second},
		WithCredentials("alice", "s3cr3t"),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := tr.ensureInitialized(context.Background()); err != nil {
		t.Fatalf("ensureInitialized: %v", err)
	}

	tr.authMu.Lock()
	closeDone := make(chan error, 1)
	go func() {
		closeDone <- tr.Close()
	}()
	for !tr.closed.Load() {
		runtime.Gosched()
	}

	authDone := make(chan error, 1)
	go func() {
		authDone <- tr.ensureAuthenticated(context.Background())
	}()
	tr.authMu.Unlock()

	if err := <-closeDone; err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := <-authDone; err == nil {
		t.Fatal("ensureAuthenticated after close succeeded")
	}
	if got := loginCalls.Load(); got != 0 {
		t.Fatalf("login calls after close = %d, want 0", got)
	}
}
