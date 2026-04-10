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

//go:build integration && rag

package orchestration

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// initializeChannelServer performs the MCP initialize handshake with a
// ChannelServer over the given reader and writer pipes.
func initializeChannelServer(t *testing.T, writer io.Writer, reader *bufio.Scanner) {
	t.Helper()

	initMsg := orchestration.JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params: json.RawMessage(`{
			"protocolVersion": "2025-03-26",
			"capabilities": {},
			"clientInfo": {"name": "test-client", "version": "1.0.0"}
		}`),
	}
	data, err := json.Marshal(initMsg)
	require.NoError(t, err)
	_, writeErr := writer.Write(append(data, '\n'))
	require.NoError(t, writeErr)

	// Read the initialize response.
	require.True(t, reader.Scan(), "should read initialize response")

	// Send the initialized notification.
	notif := orchestration.JSONRPCMessage{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	data, err = json.Marshal(notif)
	require.NoError(t, err)
	_, writeErr = writer.Write(append(data, '\n'))
	require.NoError(t, writeErr)
}

// TestE2E_ChannelServer_PushEvent verifies that pushing an event through the
// ChannelServer delivers it as an MCP notification to the connected client.
func TestE2E_ChannelServer_PushEvent(t *testing.T) {
	// Create pipes to simulate stdio transport.
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	logger := &testLogger{t: t}
	cs := orchestration.NewChannelServer(
		orchestration.ChannelServerConfig{
			ServerName:      "test-push",
			Instructions:    "Integration test channel server",
			EventBufferSize: 16,
		},
		serverReader,
		serverWriter,
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Start the channel server in a goroutine (Start blocks).
	startErrCh := make(chan error, 1)
	go func() {
		startErrCh <- cs.Start(ctx)
	}()
	defer func() {
		stopErr := cs.Stop()
		assert.NoError(t, stopErr)
		// Verify Start returned without unexpected error.
		startErr := <-startErrCh
		assert.NoError(t, startErr, "ChannelServer.Start should not return an error")
	}()

	// Perform MCP initialize handshake from the client side.
	scanner := bufio.NewScanner(clientReader)
	initializeChannelServer(t, clientWriter, scanner)

	// Wait for initialized state.
	initDone := make(chan struct{})
	go func() {
		for !cs.Initialized() {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
		close(initDone)
	}()

	select {
	case <-initDone:
	case <-ctx.Done():
		t.Fatal("timeout waiting for channel server initialization")
	}

	// Push an event.
	pushErr := cs.PushEvent(ctx, &orchestration.ChannelEvent{
		Content:  "task.assigned: implement health check",
		Metadata: map[string]string{"task_id": "task-001", "team": "backend"},
	})
	require.NoError(t, pushErr, "PushEvent should succeed")

	// Read the notification from the client side.
	received := make(chan string, 1)
	go func() {
		if scanner.Scan() {
			received <- scanner.Text()
		}
	}()

	select {
	case line := <-received:
		assert.Contains(t, line, "task.assigned", "notification should contain the event content")
		assert.Contains(t, line, "task-001", "notification should contain metadata")
	case <-ctx.Done():
		t.Fatal("timeout waiting for push event notification")
	}
}

// TestE2E_ChannelServer_PushEvent_NotInitialized verifies that pushing an
// event before the MCP handshake completes returns an error.
func TestE2E_ChannelServer_PushEvent_NotInitialized(t *testing.T) {
	_, serverWriter := io.Pipe()
	serverReader, _ := io.Pipe()

	logger := &testLogger{t: t}
	cs := orchestration.NewChannelServer(
		orchestration.ChannelServerConfig{
			ServerName:      "test-not-init",
			EventBufferSize: 4,
		},
		serverReader,
		serverWriter,
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Do NOT start or initialize - just try to push.
	err := cs.PushEvent(ctx, &orchestration.ChannelEvent{
		Content: "should fail",
	})
	require.Error(t, err, "PushEvent before initialization should fail")
	assert.ErrorIs(t, err, orchestration.ErrChannelServer)
}

// TestE2E_ChannelServer_Reply verifies two-way communication through the
// channel server's reply tool mechanism.
func TestE2E_ChannelServer_Reply(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	logger := &testLogger{t: t}

	// Track replies received by the handler.
	replyCh := make(chan string, 1)
	replyHandler := func(ctx context.Context, channel string, message string) error {
		replyCh <- channel + ": " + message
		return nil
	}

	cs := orchestration.NewChannelServer(
		orchestration.ChannelServerConfig{
			ServerName:      "test-reply",
			Instructions:    "Reply test server",
			EnableReply:     true,
			EventBufferSize: 16,
		},
		serverReader,
		serverWriter,
		logger,
	)
	cs.SetReplyHandler(replyHandler)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	startErrCh := make(chan error, 1)
	go func() {
		startErrCh <- cs.Start(ctx)
	}()
	defer func() {
		stopErr := cs.Stop()
		assert.NoError(t, stopErr)
		startErr := <-startErrCh
		assert.NoError(t, startErr, "ChannelServer.Start should not return an error")
	}()

	scanner := bufio.NewScanner(clientReader)
	initializeChannelServer(t, clientWriter, scanner)

	// Wait for initialization.
	initDone := make(chan struct{})
	go func() {
		for !cs.Initialized() {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
		close(initDone)
	}()

	select {
	case <-initDone:
	case <-ctx.Done():
		t.Fatal("timeout waiting for channel server initialization")
	}

	// Send a tools/call request for the reply tool from the client side.
	toolCallMsg := orchestration.JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
		Method:  "tools/call",
		Params: json.RawMessage(`{
			"name": "reply",
			"arguments": {
				"channel": "orchestration",
				"message": "task acknowledged"
			}
		}`),
	}
	data, err := json.Marshal(toolCallMsg)
	require.NoError(t, err)

	// Drain the tools/call JSON-RPC response from the pipe so the server's
	// writeResponse does not block on the synchronous io.Pipe.
	go func() {
		scanner.Scan() // consume the tools/call response
	}()

	_, writeErr := clientWriter.Write(append(data, '\n'))
	require.NoError(t, writeErr)

	// Verify the reply handler received the message.
	select {
	case reply := <-replyCh:
		assert.True(t, strings.Contains(reply, "orchestration"), "reply should contain channel name")
		assert.True(t, strings.Contains(reply, "task acknowledged"), "reply should contain message")
	case <-ctx.Done():
		t.Fatal("timeout waiting for reply handler invocation")
	}
}

// TestE2E_ChannelServer_CrossHost simulates a cross-host event relay scenario
// where events are pushed to a channel server that would relay to a remote
// Claude Code session.
func TestE2E_ChannelServer_CrossHost(t *testing.T) {
	// Create two channel servers to simulate cross-host relay.
	clientReaderA, serverWriterA := io.Pipe()
	serverReaderA, clientWriterA := io.Pipe()

	clientReaderB, serverWriterB := io.Pipe()
	serverReaderB, clientWriterB := io.Pipe()

	logger := &testLogger{t: t}

	csA := orchestration.NewChannelServer(
		orchestration.ChannelServerConfig{
			ServerName:      "host-a",
			EventBufferSize: 16,
		},
		serverReaderA,
		serverWriterA,
		logger,
	)

	csB := orchestration.NewChannelServer(
		orchestration.ChannelServerConfig{
			ServerName:      "host-b",
			EventBufferSize: 16,
		},
		serverReaderB,
		serverWriterB,
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Start both servers in goroutines (Start blocks).
	startErrChA := make(chan error, 1)
	go func() {
		startErrChA <- csA.Start(ctx)
	}()
	startErrChB := make(chan error, 1)
	go func() {
		startErrChB <- csB.Start(ctx)
	}()
	defer func() {
		assert.NoError(t, csA.Stop())
		assert.NoError(t, csB.Stop())
		startErrA := <-startErrChA
		assert.NoError(t, startErrA, "csA.Start should not return an error")
		startErrB := <-startErrChB
		assert.NoError(t, startErrB, "csB.Start should not return an error")
	}()

	// Initialize both from client side.
	scannerA := bufio.NewScanner(clientReaderA)
	scannerB := bufio.NewScanner(clientReaderB)
	initializeChannelServer(t, clientWriterA, scannerA)
	initializeChannelServer(t, clientWriterB, scannerB)

	// Wait for both to initialize.
	for _, cs := range []*orchestration.ChannelServer{csA, csB} {
		done := make(chan struct{})
		go func(s *orchestration.ChannelServer) {
			for !s.Initialized() {
				select {
				case <-ctx.Done():
					return
				default:
				}
			}
			close(done)
		}(cs)
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal("timeout waiting for channel server initialization")
		}
	}

	// Simulate cross-host relay: push event from host A, relay to host B.
	event := &orchestration.ChannelEvent{
		Content:  "cross-host task relay",
		Metadata: map[string]string{"origin": "host-a", "target": "host-b"},
	}
	require.NoError(t, csA.PushEvent(ctx, event))
	require.NoError(t, csB.PushEvent(ctx, event))

	// Verify both clients receive the event.
	for i, scanner := range []*bufio.Scanner{scannerA, scannerB} {
		received := make(chan string, 1)
		go func(s *bufio.Scanner) {
			if s.Scan() {
				received <- s.Text()
			}
		}(scanner)

		select {
		case line := <-received:
			assert.Contains(t, line, "cross-host task relay",
				"client %d should receive the relayed event", i)
		case <-ctx.Done():
			t.Fatalf("timeout waiting for event on client %d", i)
		}
	}
}

// TestE2E_ChannelServer_PushEvent_NilEvent verifies that pushing a nil event
// returns a ChannelEventError.
func TestE2E_ChannelServer_PushEvent_NilEvent(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	logger := &testLogger{t: t}
	cs := orchestration.NewChannelServer(
		orchestration.ChannelServerConfig{
			ServerName:      "test-nil-event",
			Instructions:    "Nil event test server",
			EventBufferSize: 4,
		},
		serverReader,
		serverWriter,
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	startErrCh := make(chan error, 1)
	go func() {
		startErrCh <- cs.Start(ctx)
	}()
	defer func() {
		stopErr := cs.Stop()
		assert.NoError(t, stopErr)
		startErr := <-startErrCh
		assert.NoError(t, startErr, "ChannelServer.Start should not return an error")
	}()

	scanner := bufio.NewScanner(clientReader)
	initializeChannelServer(t, clientWriter, scanner)

	// Wait for initialized state.
	initDone := make(chan struct{})
	go func() {
		for !cs.Initialized() {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
		close(initDone)
	}()

	select {
	case <-initDone:
	case <-ctx.Done():
		t.Fatal("timeout waiting for channel server initialization")
	}

	// Push a nil event and verify it returns a ChannelEventError.
	err := cs.PushEvent(ctx, nil)
	require.Error(t, err, "PushEvent with nil event should fail")
	assert.ErrorIs(t, err, orchestration.ErrChannelEvent,
		"nil event error should wrap ErrChannelEvent")
}

// TestE2E_ChannelServer_PushEvent_AfterStop verifies that pushing an event
// after the server has been stopped returns a ChannelConnectionClosedError.
func TestE2E_ChannelServer_PushEvent_AfterStop(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	logger := &testLogger{t: t}
	cs := orchestration.NewChannelServer(
		orchestration.ChannelServerConfig{
			ServerName:      "test-push-after-stop",
			Instructions:    "Push after stop test server",
			EventBufferSize: 4,
		},
		serverReader,
		serverWriter,
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	startErrCh := make(chan error, 1)
	go func() {
		startErrCh <- cs.Start(ctx)
	}()

	scanner := bufio.NewScanner(clientReader)
	initializeChannelServer(t, clientWriter, scanner)

	// Wait for initialized state.
	initDone := make(chan struct{})
	go func() {
		for !cs.Initialized() {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
		close(initDone)
	}()

	select {
	case <-initDone:
	case <-ctx.Done():
		t.Fatal("timeout waiting for channel server initialization")
	}

	// Stop the server, then attempt to push.
	stopErr := cs.Stop()
	require.NoError(t, stopErr, "Stop should succeed")

	// Drain Start goroutine.
	startErr := <-startErrCh
	assert.NoError(t, startErr, "ChannelServer.Start should not return an error")

	err := cs.PushEvent(ctx, &orchestration.ChannelEvent{
		Content: "should fail after stop",
	})
	require.Error(t, err, "PushEvent after Stop should fail")
	assert.ErrorIs(t, err, orchestration.ErrChannelConnectionClosed,
		"error should wrap ErrChannelConnectionClosed")
}

// TestE2E_ChannelServer_Stop_Idempotent verifies that calling Stop() multiple
// times does not panic or return an error on subsequent calls.
func TestE2E_ChannelServer_Stop_Idempotent(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	logger := &testLogger{t: t}
	cs := orchestration.NewChannelServer(
		orchestration.ChannelServerConfig{
			ServerName:      "test-stop-idempotent",
			Instructions:    "Idempotent stop test server",
			EventBufferSize: 4,
		},
		serverReader,
		serverWriter,
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	startErrCh := make(chan error, 1)
	go func() {
		startErrCh <- cs.Start(ctx)
	}()

	scanner := bufio.NewScanner(clientReader)
	initializeChannelServer(t, clientWriter, scanner)

	// Wait for initialized state.
	initDone := make(chan struct{})
	go func() {
		for !cs.Initialized() {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
		close(initDone)
	}()

	select {
	case <-initDone:
	case <-ctx.Done():
		t.Fatal("timeout waiting for channel server initialization")
	}

	// First stop should succeed.
	err1 := cs.Stop()
	assert.NoError(t, err1, "first Stop should succeed")

	// Drain Start goroutine.
	startErr := <-startErrCh
	assert.NoError(t, startErr, "ChannelServer.Start should not return an error")

	// Second and third stops should not panic and should return no error.
	err2 := cs.Stop()
	assert.NoError(t, err2, "second Stop should not error")

	err3 := cs.Stop()
	assert.NoError(t, err3, "third Stop should not error")
}

// TestE2E_ChannelServer_PushEvent_MultipleEvents verifies that multiple events
// can be pushed and all are received by the client in the correct order.
func TestE2E_ChannelServer_PushEvent_MultipleEvents(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	logger := &testLogger{t: t}
	cs := orchestration.NewChannelServer(
		orchestration.ChannelServerConfig{
			ServerName:      "test-multi-events",
			Instructions:    "Multiple events test server",
			EventBufferSize: 16,
		},
		serverReader,
		serverWriter,
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	startErrCh := make(chan error, 1)
	go func() {
		startErrCh <- cs.Start(ctx)
	}()
	defer func() {
		stopErr := cs.Stop()
		assert.NoError(t, stopErr)
		startErr := <-startErrCh
		assert.NoError(t, startErr, "ChannelServer.Start should not return an error")
	}()

	scanner := bufio.NewScanner(clientReader)
	initializeChannelServer(t, clientWriter, scanner)

	// Wait for initialized state.
	initDone := make(chan struct{})
	go func() {
		for !cs.Initialized() {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
		close(initDone)
	}()

	select {
	case <-initDone:
	case <-ctx.Done():
		t.Fatal("timeout waiting for channel server initialization")
	}

	// Push 3 events.
	events := []struct {
		content string
		taskID  string
	}{
		{content: "event-alpha", taskID: "task-001"},
		{content: "event-beta", taskID: "task-002"},
		{content: "event-gamma", taskID: "task-003"},
	}

	for _, ev := range events {
		pushErr := cs.PushEvent(ctx, &orchestration.ChannelEvent{
			Content:  ev.content,
			Metadata: map[string]string{"task_id": ev.taskID},
		})
		require.NoError(t, pushErr, "PushEvent should succeed for %s", ev.content)
	}

	// Read all 3 notifications and verify order.
	for i, ev := range events {
		received := make(chan string, 1)
		go func() {
			if scanner.Scan() {
				received <- scanner.Text()
			}
		}()

		select {
		case line := <-received:
			assert.Contains(t, line, ev.content,
				"notification %d should contain event content %q", i, ev.content)
			assert.Contains(t, line, ev.taskID,
				"notification %d should contain task_id %q", i, ev.taskID)
		case <-ctx.Done():
			t.Fatalf("timeout waiting for event %d (%s)", i, ev.content)
		}
	}
}

// TestE2E_ChannelServer_ToolsList verifies that the MCP tools/list request
// returns the expected tool definitions when reply is enabled.
func TestE2E_ChannelServer_ToolsList(t *testing.T) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	logger := &testLogger{t: t}
	cs := orchestration.NewChannelServer(
		orchestration.ChannelServerConfig{
			ServerName:      "test-tools-list",
			Instructions:    "Tools list test server",
			EnableReply:     true,
			EventBufferSize: 4,
		},
		serverReader,
		serverWriter,
		logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	startErrCh := make(chan error, 1)
	go func() {
		startErrCh <- cs.Start(ctx)
	}()
	defer func() {
		stopErr := cs.Stop()
		assert.NoError(t, stopErr)
		startErr := <-startErrCh
		assert.NoError(t, startErr, "ChannelServer.Start should not return an error")
	}()

	scanner := bufio.NewScanner(clientReader)
	initializeChannelServer(t, clientWriter, scanner)

	// Wait for initialized state.
	initDone := make(chan struct{})
	go func() {
		for !cs.Initialized() {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
		close(initDone)
	}()

	select {
	case <-initDone:
	case <-ctx.Done():
		t.Fatal("timeout waiting for channel server initialization")
	}

	// Send a tools/list request.
	toolsListMsg := orchestration.JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`10`),
		Method:  "tools/list",
	}
	data, err := json.Marshal(toolsListMsg)
	require.NoError(t, err)

	// Start reading before writing to avoid io.Pipe deadlock.
	received := make(chan string, 1)
	go func() {
		if scanner.Scan() {
			received <- scanner.Text()
		}
	}()

	_, writeErr := clientWriter.Write(append(data, '\n'))
	require.NoError(t, writeErr)

	// Read and verify the tools/list response.
	var line string
	select {
	case line = <-received:
	case <-ctx.Done():
		t.Fatal("timeout waiting for tools/list response")
	}

	// Parse as a raw JSON object so we can extract the result field.
	var rawResp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id,omitempty"`
		Result  json.RawMessage `json:"result,omitempty"`
	}
	unmarshalErr := json.Unmarshal([]byte(line), &rawResp)
	require.NoError(t, unmarshalErr, "tools/list response should be valid JSON")
	assert.Equal(t, "2.0", rawResp.JSONRPC)
	require.NotNil(t, rawResp.Result, "tools/list response should contain a result")

	// Parse the result to verify tool names.
	var result struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	unmarshalErr = json.Unmarshal(rawResp.Result, &result)
	require.NoError(t, unmarshalErr, "tools/list result should unmarshal")
	require.Len(t, result.Tools, 1, "should have exactly 1 tool when reply is enabled")
	assert.Equal(t, "reply", result.Tools[0].Name)
	assert.NotEmpty(t, result.Tools[0].Description,
		"reply tool should have a description")
}
