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

// rpc_test.go covers CallRPC against a real in-process Model Context Protocol
// server (github.com/mark3labs/mcp-go/server, Streamable HTTP). The server is
// session-based: every request other than "initialize" must carry the
// Mcp-Session-Id the server issued, or it is answered 404. CallRPC performs
// no implicit handshake, so its caller must send "initialize" first.

package mcp

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcpclienttransport "github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	mcpservertest "github.com/mark3labs/mcp-go/server/servertest"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// newRPCTestTransport starts a real Streamable HTTP MCP server publishing one
// tool and returns a transport pointed at it.
func newRPCTestTransport(t *testing.T) *Transport {
	t.Helper()
	tr, _ := newRPCTestTransportWithNotifications(t)
	return tr
}

// newRPCTestTransportWithNotifications is newRPCTestTransport that also
// reports the method of every "notifications/initialized" the server receives.
func newRPCTestTransportWithNotifications(t *testing.T) (*Transport, <-chan string) {
	t.Helper()
	notified := make(chan string, 4)
	srv := mcpserver.NewMCPServer("TestServer", "1.0.0", mcpserver.WithToolCapabilities(false))
	srv.AddNotificationHandler("notifications/initialized", func(_ context.Context, n mcpgo.JSONRPCNotification) {
		notified <- n.Method
	})
	srv.AddTool(mcpgo.NewTool("qrdb_health"), func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return mcpgo.NewToolResultText("ok"), nil
	})
	httpSrv := mcpservertest.NewTestStreamableHTTPServer(srv)
	t.Cleanup(httpSrv.Close)

	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return tr, notified
}

// rpcInitializeParams is a spec-compliant "initialize" params object.
func rpcInitializeParams(t *testing.T) json.RawMessage {
	t.Helper()
	params, err := json.Marshal(map[string]interface{}{
		"protocolVersion": MCPVersion,
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "rpc-test", "version": "0.0.0"},
	})
	if err != nil {
		t.Fatalf("marshal initialize params: %v", err)
	}
	return params
}

// TestCallRPC_WithoutInitialize_SessionTerminated reproduces the defect: a
// non-"initialize" request with no session is rejected with 404, surfaced as a
// session-terminated error.
func TestCallRPC_WithoutInitialize_SessionTerminated(t *testing.T) {
	tr := newRPCTestTransport(t)

	_, err := tr.CallRPC(context.Background(), "tools/list", nil)
	if err == nil {
		t.Fatal("expected tools/list before initialize to fail")
	}
	if !IsSessionTerminated(err) {
		t.Fatalf("IsSessionTerminated(%v) = false, want true", err)
	}
}

// TestCallRPC_InitializeThenToolsList proves the fix: once "initialize" has
// been sent on the same transport, the session id is carried and subsequent
// requests succeed.
func TestCallRPC_InitializeThenToolsList(t *testing.T) {
	tr, notified := newRPCTestTransportWithNotifications(t)
	ctx := context.Background()

	initResult, err := tr.CallRPC(ctx, "initialize", rpcInitializeParams(t))
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	var init struct {
		ServerInfo struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(initResult, &init); err != nil {
		t.Fatalf("decode initialize result: %v", err)
	}
	if init.ServerInfo.Name != "TestServer" {
		t.Errorf("serverInfo.name = %q, want TestServer", init.ServerInfo.Name)
	}

	// Complete the lifecycle handshake before any other request.
	if err := tr.NotifyRPC(ctx, "notifications/initialized", nil); err != nil {
		t.Fatalf("notifications/initialized: %v", err)
	}
	select {
	case method := <-notified:
		if method != "notifications/initialized" {
			t.Errorf("server received %q, want notifications/initialized", method)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server never received notifications/initialized")
	}

	listResult, err := tr.CallRPC(ctx, "tools/list", nil)
	if err != nil {
		t.Fatalf("tools/list after initialize: %v", err)
	}
	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(listResult, &list); err != nil {
		t.Fatalf("decode tools/list result: %v", err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "qrdb_health" {
		t.Errorf("tools = %+v, want [qrdb_health]", list.Tools)
	}

	callParams, err := json.Marshal(map[string]interface{}{"name": "qrdb_health"})
	if err != nil {
		t.Fatalf("marshal tools/call params: %v", err)
	}
	if _, err := tr.CallRPC(ctx, "tools/call", callParams); err != nil {
		t.Fatalf("tools/call after initialize: %v", err)
	}
}

// TestIsSessionTerminated covers the predicate through the SDK's wrapped
// error chain and rejects unrelated errors.
func TestIsSessionTerminated(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "bare sentinel", err: mcpclienttransport.ErrSessionTerminated, want: true},
		{
			name: "wrapped by SDK",
			err: errors.Wrap(fmt.Errorf("failed to send request: %w", mcpclienttransport.ErrSessionTerminated),
				errors.CodeConnection, "mcp.sendRPC", "HTTP request failed"),
			want: true,
		},
		{name: "legacy SSE", err: mcpclienttransport.ErrLegacySSEServer, want: false},
		{name: "unrelated", err: stderrors.New("boom"), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSessionTerminated(tc.err); got != tc.want {
				t.Errorf("IsSessionTerminated(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestNotifyRPC_ForwardsParams verifies a notification's params object
// reaches the server intact.
func TestNotifyRPC_ForwardsParams(t *testing.T) {
	// mcp-go v1.3 consumes notifications/cancelled inside the server and
	// never reaches a registered handler, so the fixture uses a method the
	// server dispatches to its handlers.
	const method = "notifications/quicbaas/test"
	received := make(chan mcpgo.JSONRPCNotification, 1)
	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	srv.AddNotificationHandler(method, func(_ context.Context, n mcpgo.JSONRPCNotification) {
		received <- n
	})
	httpSrv := mcpservertest.NewTestStreamableHTTPServer(srv)
	t.Cleanup(httpSrv.Close)
	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	ctx := context.Background()
	if _, err := tr.CallRPC(ctx, "initialize", rpcInitializeParams(t)); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := tr.NotifyRPC(ctx, method, json.RawMessage(`{"requestId":7,"reason":"user"}`)); err != nil {
		t.Fatalf("NotifyRPC: %v", err)
	}
	select {
	case n := <-received:
		if n.Method != method {
			t.Errorf("method = %q, want %q", n.Method, method)
		}
		if n.Params.AdditionalFields["reason"] != "user" || n.Params.AdditionalFields["requestId"] != float64(7) {
			t.Errorf("params = %+v, want requestId=7 reason=user", n.Params.AdditionalFields)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server never received the notification")
	}
}

// TestNotifyRPC_WithoutInitialize_SessionTerminated verifies a notification
// with no live session surfaces the same session-terminated error CallRPC
// does.
func TestNotifyRPC_WithoutInitialize_SessionTerminated(t *testing.T) {
	tr := newRPCTestTransport(t)

	err := tr.NotifyRPC(context.Background(), "notifications/initialized", nil)
	if !IsSessionTerminated(err) {
		t.Fatalf("IsSessionTerminated(%v) = false, want true", err)
	}
}

// TestNotifyRPC_Validation covers the local argument and state checks.
func TestNotifyRPC_Validation(t *testing.T) {
	tr := newRPCTestTransport(t)
	ctx := context.Background()

	if err := tr.NotifyRPC(ctx, "", nil); errors.Code(err) != errors.CodeInvalidInput {
		t.Errorf("empty method: code = %v, want %v (err %v)", errors.Code(err), errors.CodeInvalidInput, err)
	}
	if err := tr.NotifyRPC(ctx, "notifications/initialized", json.RawMessage(`[1,2]`)); errors.Code(err) != errors.CodeInvalidInput {
		t.Errorf("array params: code = %v, want %v (err %v)", errors.Code(err), errors.CodeInvalidInput, err)
	}

	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := tr.NotifyRPC(ctx, "notifications/initialized", nil); errors.Code(err) != errors.CodeConnection {
		t.Errorf("closed transport: code = %v, want %v (err %v)", errors.Code(err), errors.CodeConnection, err)
	}
}

// TestCallRPC_InitializeAgainstMissingEndpoint pins the diagnostic an operator
// sees when MCP is not served at the address (wrong port, or MCP disabled):
// the stock 404 on "initialize" surfaces as the legacy-SSE error, not as a
// session-terminated one.
func TestCallRPC_InitializeAgainstMissingEndpoint(t *testing.T) {
	httpSrv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(httpSrv.Close)
	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	_, err = tr.CallRPC(context.Background(), "initialize", rpcInitializeParams(t))
	if !stderrors.Is(err, mcpclienttransport.ErrLegacySSEServer) {
		t.Fatalf("err = %v, want ErrLegacySSEServer", err)
	}
	if IsSessionTerminated(err) {
		t.Error("a missing endpoint must not read as session-terminated")
	}
	if !strings.Contains(err.Error(), "likely a legacy SSE server") {
		t.Errorf("err = %q, want the legacy-SSE diagnostic", err.Error())
	}
}
