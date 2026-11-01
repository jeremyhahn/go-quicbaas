// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package orchestration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// channelTestLogger implements interfaces.Logger for tests.
type channelTestLogger struct{}

func (l *channelTestLogger) Debug(msg string, args ...any)                              {}
func (l *channelTestLogger) Debugf(format string, args ...interface{})                  {}
func (l *channelTestLogger) Info(msg string, args ...any)                               {}
func (l *channelTestLogger) Infof(format string, args ...interface{})                   {}
func (l *channelTestLogger) Warn(msg string, args ...any)                               {}
func (l *channelTestLogger) Warnf(format string, args ...interface{})                   {}
func (l *channelTestLogger) Error(msg string, args ...any)                              {}
func (l *channelTestLogger) Errorf(format string, args ...interface{})                  {}
func (l *channelTestLogger) Fatal(msg string)                                           {}
func (l *channelTestLogger) Fatalf(format string, args ...interface{})                  {}
func (l *channelTestLogger) WithField(key string, value interface{}) interfaces.Logger  { return l }
func (l *channelTestLogger) WithFields(fields map[string]interface{}) interfaces.Logger { return l }
func (l *channelTestLogger) WithError(err error) interfaces.Logger                      { return l }

// channelTestConn holds the pipe endpoints and a shared scanner for reading
// JSON-RPC messages from the server's output.
type channelTestConn struct {
	clientWrite io.WriteCloser // write to simulate client input (stdin)
	clientRead  io.ReadCloser  // read server output (stdout)
	scanner     *bufio.Scanner // shared scanner over clientRead
}

// newTestChannelServer creates a ChannelServer backed by OS pipes for testing.
// Returns the server and a channelTestConn for interacting with it.
func newTestChannelServer(t *testing.T, config ChannelServerConfig) (*ChannelServer, *channelTestConn) {
	t.Helper()

	// clientWrite -> server reads from this (simulated stdin)
	serverRead, clientWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	// server writes to this -> clientRead (simulated stdout)
	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	srv := NewChannelServer(config, serverRead, serverWrite, &channelTestLogger{})

	conn := &channelTestConn{
		clientWrite: clientWrite,
		clientRead:  clientRead,
		scanner:     bufio.NewScanner(clientRead),
	}

	t.Cleanup(func() {
		srv.Stop()
		serverRead.Close()
		serverWrite.Close()
		clientWrite.Close()
		clientRead.Close()
	})

	return srv, conn
}

// sendJSONRPC writes a JSON-RPC message to the writer.
func sendJSONRPC(t *testing.T, w io.Writer, msg *JSONRPCMessage) {
	t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal JSON-RPC: %v", err)
	}
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		t.Fatalf("write JSON-RPC: %v", err)
	}
}

// readJSONRPC reads a single JSON-RPC message using the shared scanner.
func readJSONRPC(t *testing.T, scanner *bufio.Scanner) *JSONRPCMessage {
	t.Helper()
	if !scanner.Scan() {
		t.Fatal("expected to read a JSON-RPC message, got EOF")
	}
	var msg JSONRPCMessage
	if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
		t.Fatalf("unmarshal JSON-RPC response: %v\nraw: %s", err, scanner.Text())
	}
	return &msg
}

// performHandshake sends the MCP initialize request and notifications/initialized
// notification, returning the initialize response.
func performHandshake(t *testing.T, conn *channelTestConn) *JSONRPCMessage {
	t.Helper()

	// Send initialize request
	sendJSONRPC(t, conn.clientWrite, &JSONRPCMessage{
		JSONRPC: JSONRPCVersion,
		ID:      float64(1),
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"clientInfo": map[string]any{
				"name":    "claude-code",
				"version": "1.0.0",
			},
		},
	})

	// Read initialize response
	resp := readJSONRPC(t, conn.scanner)

	// Send initialized notification (no ID)
	sendJSONRPC(t, conn.clientWrite, &JSONRPCMessage{
		JSONRPC: JSONRPCVersion,
		Method:  "notifications/initialized",
	})

	return resp
}

func TestNewChannelServer_Config(t *testing.T) {
	tests := []struct {
		name           string
		config         ChannelServerConfig
		wantServerName string
		wantBufferSize int
	}{
		{"empty config uses defaults", ChannelServerConfig{}, "quicbaas", defaultEventBufferSize},
		{"negative buffer size uses default", ChannelServerConfig{EventBufferSize: -10}, "quicbaas", defaultEventBufferSize},
		{
			"custom config",
			ChannelServerConfig{ServerName: "test-server", Instructions: "test instructions", EnableReply: true, EventBufferSize: 64},
			"test-server",
			64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			srv := NewChannelServer(tt.config, strings.NewReader(""), &buf, &channelTestLogger{})

			if srv.config.ServerName != tt.wantServerName {
				t.Errorf("ServerName = %q, want %q", srv.config.ServerName, tt.wantServerName)
			}
			if cap(srv.eventCh) != tt.wantBufferSize {
				t.Errorf("event buffer size = %d, want %d", cap(srv.eventCh), tt.wantBufferSize)
			}
			if srv.Initialized() {
				t.Error("server should not be initialized before handshake")
			}
		})
	}
}

func TestChannelServer_InitializeHandshake(t *testing.T) {
	srv, conn := newTestChannelServer(t, ChannelServerConfig{
		ServerName:   "test-channel",
		Instructions: "You are a test agent",
		EnableReply:  true,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start(ctx)
	}()

	resp := performHandshake(t, conn)

	// Verify response structure
	if resp.Error != nil {
		t.Fatalf("initialize returned error: %v", resp.Error)
	}

	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	var result mcpInitializeResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.ProtocolVersion != mcpProtocolVersion {
		t.Errorf("protocolVersion = %q, want %q", result.ProtocolVersion, mcpProtocolVersion)
	}

	if result.ServerInfo.Name != "test-channel" {
		t.Errorf("serverInfo.name = %q, want %q", result.ServerInfo.Name, "test-channel")
	}

	if result.Instructions != "You are a test agent" {
		t.Errorf("instructions = %q, want %q", result.Instructions, "You are a test agent")
	}

	// Verify claude/channel capability
	channelCap, ok := result.Capabilities.Experimental["claude/channel"]
	if !ok {
		t.Fatal("missing claude/channel capability")
	}
	if channelCap == nil {
		t.Fatal("claude/channel capability is nil")
	}

	// Verify tools capability is present when reply is enabled
	if result.Capabilities.Tools == nil {
		t.Error("tools capability should be present when EnableReply=true")
	}

	// Wait briefly for the readLoop to process the notifications/initialized message
	deadline := time.Now().Add(2 * time.Second)
	for !srv.Initialized() && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if !srv.Initialized() {
		t.Error("server should be initialized after handshake")
	}

	cancel()
	<-startDone
}

func TestChannelServer_InitializeHandshake_NoReply(t *testing.T) {
	srv, conn := newTestChannelServer(t, ChannelServerConfig{
		ServerName: "no-reply-server",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start(ctx)
	}()

	resp := performHandshake(t, conn)

	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	var result mcpInitializeResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.Capabilities.Tools != nil {
		t.Error("tools capability should be nil when EnableReply=false")
	}

	cancel()
	<-startDone
}

func TestChannelServer_InitializeHandshake_PermissionRelay(t *testing.T) {
	srv, conn := newTestChannelServer(t, ChannelServerConfig{
		ServerName:            "perm-relay-server",
		EnablePermissionRelay: true,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start(ctx)
	}()

	resp := performHandshake(t, conn)

	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	var result mcpInitializeResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	_, ok := result.Capabilities.Experimental["claude/channel/permission"]
	if !ok {
		t.Fatal("missing claude/channel/permission capability")
	}

	cancel()
	<-startDone
}

func TestChannelServer_PushEvent(t *testing.T) {
	srv, conn := newTestChannelServer(t, ChannelServerConfig{
		ServerName: "push-test",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start(ctx)
	}()

	performHandshake(t, conn)

	// Push an event
	event := &ChannelEvent{
		Content: "New task available: Write hello world",
		Metadata: map[string]string{
			"task_id": "t1",
			"team":    "dev",
			"type":    "task_created",
		},
	}

	if err := srv.PushEvent(ctx, event); err != nil {
		t.Fatalf("PushEvent() error = %v", err)
	}

	// Read the notification from stdout
	notification := readJSONRPC(t, conn.scanner)

	if notification.Method != channelNotificationMethod {
		t.Errorf("method = %q, want %q", notification.Method, channelNotificationMethod)
	}

	if notification.ID != nil {
		t.Errorf("notification should not have ID, got %v", notification.ID)
	}

	// Verify params
	paramsBytes, err := json.Marshal(notification.Params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	var params channelNotificationParams
	if err := json.Unmarshal(paramsBytes, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}

	if params.Content != "New task available: Write hello world" {
		t.Errorf("content = %q, want %q", params.Content, "New task available: Write hello world")
	}

	if params.Meta["task_id"] != "t1" {
		t.Errorf("meta.task_id = %q, want %q", params.Meta["task_id"], "t1")
	}

	if params.Meta["team"] != "dev" {
		t.Errorf("meta.team = %q, want %q", params.Meta["team"], "dev")
	}

	if params.Meta["type"] != "task_created" {
		t.Errorf("meta.type = %q, want %q", params.Meta["type"], "task_created")
	}

	cancel()
	<-startDone
}

func TestChannelServer_PushEvent_NilEvent(t *testing.T) {
	var buf bytes.Buffer
	srv := NewChannelServer(ChannelServerConfig{}, strings.NewReader(""), &buf, &channelTestLogger{})

	err := srv.PushEvent(context.Background(), nil)
	if err == nil {
		t.Fatal("PushEvent(nil) should return error")
	}

	if !errors.Is(err, ErrChannelEvent) {
		t.Errorf("error = %v, want ErrChannelEvent", err)
	}
}

func TestChannelServer_PushEvent_AfterStop(t *testing.T) {
	var buf bytes.Buffer
	srv := NewChannelServer(ChannelServerConfig{}, strings.NewReader(""), &buf, &channelTestLogger{})

	srv.Stop()

	err := srv.PushEvent(context.Background(), &ChannelEvent{Content: "test"})
	if err == nil {
		t.Fatal("PushEvent after Stop should return error")
	}

	if !errors.Is(err, ErrChannelConnectionClosed) {
		t.Errorf("error = %v, want ErrChannelConnectionClosed", err)
	}
}

func TestChannelServer_PushEvent_ContextCanceled(t *testing.T) {
	// Create a server with buffer size 1 and fill it
	config := ChannelServerConfig{
		EventBufferSize: 1,
	}
	var buf bytes.Buffer
	srv := NewChannelServer(config, strings.NewReader(""), &buf, &channelTestLogger{})

	// Mark server as started so PushEvent passes the started check.
	srv.started.Store(true)

	// Fill the buffer
	srv.eventCh <- &ChannelEvent{Content: "filler"}

	// Cancel the context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := srv.PushEvent(ctx, &ChannelEvent{Content: "overflow"})
	if err == nil {
		t.Fatal("PushEvent with canceled context should return error")
	}

	if !errors.Is(err, ErrChannelEvent) {
		t.Errorf("error = %v, want ErrChannelEvent", err)
	}
}

func TestChannelServer_EventBuffering(t *testing.T) {
	srv, conn := newTestChannelServer(t, ChannelServerConfig{
		ServerName:      "buffer-test",
		EventBufferSize: 16,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start(ctx)
	}()

	// Wait for the server to mark itself as started.
	for !srv.started.Load() {
		select {
		case <-ctx.Done():
			t.Fatal("context canceled before server started")
		default:
		}
	}

	// Push events BEFORE initialization -- they should be buffered in the channel
	const numEvents = 5
	for i := 0; i < numEvents; i++ {
		if err := srv.PushEvent(ctx, &ChannelEvent{
			Content: "buffered-event",
			Metadata: map[string]string{
				"index": string(rune('0' + i)),
			},
		}); err != nil {
			t.Fatalf("PushEvent() error = %v", err)
		}
	}

	// Now perform handshake -- this triggers the initialized state
	performHandshake(t, conn)

	// Read all buffered events that should now be sent as notifications
	for i := 0; i < numEvents; i++ {
		notification := readJSONRPC(t, conn.scanner)
		if notification.Method != channelNotificationMethod {
			t.Errorf("event %d: method = %q, want %q", i, notification.Method, channelNotificationMethod)
		}
	}

	cancel()
	<-startDone
}

func TestChannelServer_ReplyTool(t *testing.T) {
	var repliedChannel string
	var repliedMessage string
	var replyMu sync.Mutex

	srv, conn := newTestChannelServer(t, ChannelServerConfig{
		ServerName:  "reply-test",
		EnableReply: true,
	})

	srv.SetReplyHandler(func(ctx context.Context, channel string, message string) error {
		replyMu.Lock()
		defer replyMu.Unlock()
		repliedChannel = channel
		repliedMessage = message
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start(ctx)
	}()

	performHandshake(t, conn)

	// Request tools/list
	sendJSONRPC(t, conn.clientWrite, &JSONRPCMessage{
		JSONRPC: JSONRPCVersion,
		ID:      float64(2),
		Method:  "tools/list",
	})

	listResp := readJSONRPC(t, conn.scanner)
	if listResp.Error != nil {
		t.Fatalf("tools/list returned error: %v", listResp.Error)
	}

	resultBytes, err := json.Marshal(listResp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	var toolsList mcpToolsListResult
	if err := json.Unmarshal(resultBytes, &toolsList); err != nil {
		t.Fatalf("unmarshal tools list: %v", err)
	}

	if len(toolsList.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(toolsList.Tools))
	}

	if toolsList.Tools[0].Name != "reply" {
		t.Errorf("tool name = %q, want %q", toolsList.Tools[0].Name, "reply")
	}

	// Call the reply tool
	sendJSONRPC(t, conn.clientWrite, &JSONRPCMessage{
		JSONRPC: JSONRPCVersion,
		ID:      float64(3),
		Method:  "tools/call",
		Params: map[string]any{
			"name": "reply",
			"arguments": map[string]any{
				"channel": "dev-team",
				"message": "Task completed successfully",
			},
		},
	})

	callResp := readJSONRPC(t, conn.scanner)
	if callResp.Error != nil {
		t.Fatalf("tools/call returned error: %v", callResp.Error)
	}

	callResultBytes, err := json.Marshal(callResp.Result)
	if err != nil {
		t.Fatalf("marshal call result: %v", err)
	}

	var callResult mcpToolCallResult
	if err := json.Unmarshal(callResultBytes, &callResult); err != nil {
		t.Fatalf("unmarshal call result: %v", err)
	}

	if callResult.IsError {
		t.Errorf("reply tool returned error: %v", callResult.Content)
	}

	if len(callResult.Content) == 0 {
		t.Fatal("expected content in reply result")
	}

	if !strings.Contains(callResult.Content[0].Text, "dev-team") {
		t.Errorf("result text = %q, expected mention of channel", callResult.Content[0].Text)
	}

	replyMu.Lock()
	defer replyMu.Unlock()
	if repliedChannel != "dev-team" {
		t.Errorf("replied channel = %q, want %q", repliedChannel, "dev-team")
	}
	if repliedMessage != "Task completed successfully" {
		t.Errorf("replied message = %q, want %q", repliedMessage, "Task completed successfully")
	}

	cancel()
	<-startDone
}

func TestChannelServer_ReplyTool_Disabled(t *testing.T) {
	srv, conn := newTestChannelServer(t, ChannelServerConfig{
		ServerName:  "no-reply",
		EnableReply: false,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start(ctx)
	}()

	performHandshake(t, conn)

	// tools/list should return empty
	sendJSONRPC(t, conn.clientWrite, &JSONRPCMessage{
		JSONRPC: JSONRPCVersion,
		ID:      float64(2),
		Method:  "tools/list",
	})

	listResp := readJSONRPC(t, conn.scanner)
	resultBytes, err := json.Marshal(listResp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	var toolsList mcpToolsListResult
	if err := json.Unmarshal(resultBytes, &toolsList); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if len(toolsList.Tools) != 0 {
		t.Errorf("expected 0 tools when reply disabled, got %d", len(toolsList.Tools))
	}

	// Calling reply anyway must be rejected.
	callResult := callReplyTool(t, conn, 3, map[string]any{
		"channel": "dev",
		"message": "test",
	})

	if !callResult.IsError {
		t.Error("expected IsError=true when reply tool is disabled")
	}

	if !strings.Contains(callResult.Content[0].Text, "reply tool is not enabled") {
		t.Errorf("error text = %q", callResult.Content[0].Text)
	}

	cancel()
	<-startDone
}

// callReplyTool sends a tools/call for the reply tool and decodes the result.
func callReplyTool(t *testing.T, conn *channelTestConn, id float64, args map[string]any) mcpToolCallResult {
	t.Helper()

	sendJSONRPC(t, conn.clientWrite, &JSONRPCMessage{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Method:  "tools/call",
		Params: map[string]any{
			"name":      "reply",
			"arguments": args,
		},
	})

	callResp := readJSONRPC(t, conn.scanner)
	callResultBytes, err := json.Marshal(callResp.Result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var callResult mcpToolCallResult
	if err := json.Unmarshal(callResultBytes, &callResult); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(callResult.Content) == 0 {
		t.Fatal("expected content in tool call result")
	}
	return callResult
}

func TestChannelServer_ReplyTool_HandlerError(t *testing.T) {
	srv, conn := newTestChannelServer(t, ChannelServerConfig{
		ServerName:  "reply-error-test",
		EnableReply: true,
	})

	srv.SetReplyHandler(func(ctx context.Context, channel string, message string) error {
		return &ChannelEventError{Message: "downstream failure"}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start(ctx)
	}()

	performHandshake(t, conn)

	sendJSONRPC(t, conn.clientWrite, &JSONRPCMessage{
		JSONRPC: JSONRPCVersion,
		ID:      float64(2),
		Method:  "tools/call",
		Params: map[string]any{
			"name": "reply",
			"arguments": map[string]any{
				"channel": "dev-team",
				"message": "will fail",
			},
		},
	})

	callResp := readJSONRPC(t, conn.scanner)
	callResultBytes, err := json.Marshal(callResp.Result)
	if err != nil {
		t.Fatalf("marshal call result: %v", err)
	}

	var callResult mcpToolCallResult
	if err := json.Unmarshal(callResultBytes, &callResult); err != nil {
		t.Fatalf("unmarshal call result: %v", err)
	}

	if !callResult.IsError {
		t.Error("reply tool should return IsError=true when handler fails")
	}

	if len(callResult.Content) == 0 {
		t.Fatal("expected error content")
	}

	if !strings.Contains(callResult.Content[0].Text, "reply failed") {
		t.Errorf("error text = %q, expected 'reply failed'", callResult.Content[0].Text)
	}

	cancel()
	<-startDone
}

func TestChannelServer_ReplyTool_BadArguments(t *testing.T) {
	srv, conn := newTestChannelServer(t, ChannelServerConfig{
		ServerName:  "reply-bad-args",
		EnableReply: true,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start(ctx)
	}()

	performHandshake(t, conn)

	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{"missing channel", map[string]any{"message": "no channel provided"}, "missing required argument: channel"},
		{"missing message", map[string]any{"channel": "dev"}, "missing required argument: message"},
		{"non-string channel", map[string]any{"channel": 123, "message": "test"}, "channel must be a string"},
		{"non-string message", map[string]any{"channel": "dev", "message": 456}, "message must be a string"},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callResult := callReplyTool(t, conn, float64(i+2), tt.args)

			if !callResult.IsError {
				t.Error("expected IsError=true")
			}
			if !strings.Contains(callResult.Content[0].Text, tt.wantErr) {
				t.Errorf("error text = %q, want it to contain %q", callResult.Content[0].Text, tt.wantErr)
			}
		})
	}

	cancel()
	<-startDone
}

func TestChannelServer_UnknownMethod(t *testing.T) {
	srv, conn := newTestChannelServer(t, ChannelServerConfig{
		ServerName: "unknown-method",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start(ctx)
	}()

	performHandshake(t, conn)

	// Send unknown method with an ID
	sendJSONRPC(t, conn.clientWrite, &JSONRPCMessage{
		JSONRPC: JSONRPCVersion,
		ID:      float64(10),
		Method:  "unknown/method",
	})

	resp := readJSONRPC(t, conn.scanner)
	if resp.Error == nil {
		t.Fatal("expected error response for unknown method")
	}

	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}

	if !strings.Contains(resp.Error.Message, "method not found") {
		t.Errorf("error message = %q, want 'method not found'", resp.Error.Message)
	}

	cancel()
	<-startDone
}

func TestChannelServer_UnknownTool(t *testing.T) {
	srv, conn := newTestChannelServer(t, ChannelServerConfig{
		ServerName:  "unknown-tool",
		EnableReply: true,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start(ctx)
	}()

	performHandshake(t, conn)

	sendJSONRPC(t, conn.clientWrite, &JSONRPCMessage{
		JSONRPC: JSONRPCVersion,
		ID:      float64(5),
		Method:  "tools/call",
		Params: map[string]any{
			"name":      "nonexistent",
			"arguments": map[string]any{},
		},
	})

	resp := readJSONRPC(t, conn.scanner)
	if resp.Error == nil {
		t.Fatal("expected error for unknown tool")
	}

	if !strings.Contains(resp.Error.Message, "unknown tool") {
		t.Errorf("error message = %q, want 'unknown tool'", resp.Error.Message)
	}

	cancel()
	<-startDone
}

func TestChannelServer_Ping(t *testing.T) {
	srv, conn := newTestChannelServer(t, ChannelServerConfig{
		ServerName: "ping-test",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start(ctx)
	}()

	performHandshake(t, conn)

	sendJSONRPC(t, conn.clientWrite, &JSONRPCMessage{
		JSONRPC: JSONRPCVersion,
		ID:      float64(99),
		Method:  "ping",
	})

	resp := readJSONRPC(t, conn.scanner)
	if resp.Error != nil {
		t.Fatalf("ping returned error: %v", resp.Error)
	}

	cancel()
	<-startDone
}

func TestChannelServer_Stop(t *testing.T) {
	srv, _ := newTestChannelServer(t, ChannelServerConfig{
		ServerName: "stop-test",
	})

	// Stop should be idempotent
	if err := srv.Stop(); err != nil {
		t.Fatalf("first Stop() error = %v", err)
	}

	if err := srv.Stop(); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}

	if !srv.closed.Load() {
		t.Error("server should be closed after Stop()")
	}
}

var errReaderClose = errors.New("close failed")

// failingCloseReader is a reader whose Close always fails.
type failingCloseReader struct{ io.Reader }

func (failingCloseReader) Close() error { return errReaderClose }

// warnRecordingLogger records Warn messages and discards everything else.
type warnRecordingLogger struct {
	channelTestLogger
	warnings []string
}

func (l *warnRecordingLogger) Warn(msg string, _ ...any) { l.warnings = append(l.warnings, msg) }

func TestChannelServer_StopReportsReaderCloseFailure(t *testing.T) {
	logger := &warnRecordingLogger{}
	srv := NewChannelServer(ChannelServerConfig{}, failingCloseReader{strings.NewReader("")}, io.Discard, logger)

	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !srv.closed.Load() {
		t.Error("server should be closed after Stop() even when the reader fails to close")
	}
	if len(logger.warnings) != 1 {
		t.Fatalf("warnings = %v, want one reader-close warning", logger.warnings)
	}
}

func TestChannelServer_StartAfterStop(t *testing.T) {
	var buf bytes.Buffer
	srv := NewChannelServer(ChannelServerConfig{}, strings.NewReader(""), &buf, &channelTestLogger{})

	srv.Stop()

	err := srv.Start(context.Background())
	if err == nil {
		t.Fatal("Start after Stop should return error")
	}

	if !errors.Is(err, ErrChannelConnectionClosed) {
		t.Errorf("error = %v, want ErrChannelConnectionClosed", err)
	}
}

func TestChannelEvent_Serialization(t *testing.T) {
	tests := []struct {
		name     string
		event    ChannelEvent
		wantMeta bool
	}{
		{
			name: "with metadata",
			event: ChannelEvent{
				Content: "Task assigned: implement feature X",
				Metadata: map[string]string{
					"task_id":  "t42",
					"priority": "high",
					"assignee": "agent-1",
				},
			},
			wantMeta: true,
		},
		{
			name:     "without metadata omits meta",
			event:    ChannelEvent{Content: "simple event"},
			wantMeta: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.event)
			if err != nil {
				t.Fatalf("marshal event: %v", err)
			}

			if got := strings.Contains(string(data), `"meta"`); got != tt.wantMeta {
				t.Errorf("meta key present = %v, want %v: %s", got, tt.wantMeta, data)
			}

			var decoded ChannelEvent
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal event: %v", err)
			}

			if decoded.Content != tt.event.Content {
				t.Errorf("content = %q, want %q", decoded.Content, tt.event.Content)
			}
			for k, want := range tt.event.Metadata {
				if decoded.Metadata[k] != want {
					t.Errorf("meta.%s = %q, want %q", k, decoded.Metadata[k], want)
				}
			}
		})
	}
}

func TestChannelServer_MalformedJSON(t *testing.T) {
	// Send malformed JSON followed by a valid initialize -- server should recover
	srv, conn := newTestChannelServer(t, ChannelServerConfig{
		ServerName: "malformed-test",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start(ctx)
	}()

	// Write garbage
	conn.clientWrite.Write([]byte("not json at all\n"))
	conn.clientWrite.Write([]byte("{broken\n"))

	// Now valid initialize
	resp := performHandshake(t, conn)

	if resp.Error != nil {
		t.Fatalf("initialize after malformed input should succeed, got error: %v", resp.Error)
	}

	cancel()
	<-startDone
}

func TestChannelServer_ToolsCall_InvalidParams(t *testing.T) {
	srv, conn := newTestChannelServer(t, ChannelServerConfig{
		ServerName:  "invalid-params",
		EnableReply: true,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startDone := make(chan error, 1)
	go func() {
		startDone <- srv.Start(ctx)
	}()

	performHandshake(t, conn)

	// Send tools/call with invalid params (not an object with name/arguments)
	sendJSONRPC(t, conn.clientWrite, &JSONRPCMessage{
		JSONRPC: JSONRPCVersion,
		ID:      float64(5),
		Method:  "tools/call",
		Params:  "not an object",
	})

	resp := readJSONRPC(t, conn.scanner)
	if resp.Error == nil {
		t.Fatal("expected error for invalid params")
	}

	if resp.Error.Code != -32602 {
		t.Errorf("error code = %d, want -32602", resp.Error.Code)
	}

	cancel()
	<-startDone
}

// =============================================================================
// Error type tests
// =============================================================================

func TestChannelErrorTypes(t *testing.T) {
	cause := errors.New("timeout")

	tests := []struct {
		name      string
		err       error
		wantText  []string
		sentinel  error
		wantCause bool
	}{
		{"server error with cause", &ChannelServerError{Message: "connection failed", Cause: cause}, []string{"connection failed", "timeout"}, ErrChannelServer, true},
		{"server error without cause", &ChannelServerError{Message: "general failure"}, []string{"general failure"}, ErrChannelServer, false},
		{"event error with cause", &ChannelEventError{Message: "push failed", Cause: cause}, []string{"push failed", "timeout"}, ErrChannelEvent, true},
		{"event error without cause", &ChannelEventError{Message: "nil event"}, []string{"nil event"}, ErrChannelEvent, false},
		{"connection closed", &ChannelConnectionClosedError{Reason: "pipe broken"}, []string{"pipe broken"}, ErrChannelConnectionClosed, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, want := range tt.wantText {
				if !strings.Contains(tt.err.Error(), want) {
					t.Errorf("error = %q, want to contain %q", tt.err.Error(), want)
				}
			}
			if !errors.Is(tt.err, tt.sentinel) {
				t.Errorf("errors.Is(%v, %v) = false, want true", tt.err, tt.sentinel)
			}
			if got := errors.Is(tt.err, cause); got != tt.wantCause {
				t.Errorf("errors.Is(err, cause) = %v, want %v", got, tt.wantCause)
			}
		})
	}
}

func TestExtractInt64ID(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		want   int64
		wantOK bool
	}{
		{name: "float64", input: float64(42), want: 42, wantOK: true},
		{name: "int64", input: int64(99), want: 99, wantOK: true},
		{name: "json.Number", input: json.Number("7"), want: 7, wantOK: true},
		{name: "string_numeric", input: "123", want: 123, wantOK: true},
		{name: "string_non_numeric", input: "abc", want: 0, wantOK: false},
		{name: "nil", input: nil, want: 0, wantOK: false},
		{name: "bool", input: true, want: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractInt64ID(tt.input)
			if ok != tt.wantOK {
				t.Errorf("extractInt64ID(%v) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("extractInt64ID(%v) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
