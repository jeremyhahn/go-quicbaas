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

package subprotocol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
)

func TestNewEchoHandler(t *testing.T) {
	handler := NewEchoHandler()

	if handler == nil {
		t.Fatal("NewEchoHandler() returned nil")
	}

	if handler.MessagesReceived() != 0 {
		t.Errorf("MessagesReceived() = %d, want 0", handler.MessagesReceived())
	}

	if handler.MessagesSent() != 0 {
		t.Errorf("MessagesSent() = %d, want 0", handler.MessagesSent())
	}
}

func TestEchoHandler_Name(t *testing.T) {
	handler := NewEchoHandler()

	if handler.Name() != "echo" {
		t.Errorf("Name() = %q, want echo", handler.Name())
	}
}

func TestEchoHandler_MessagesReceived(t *testing.T) {
	handler := NewEchoHandler()

	// Initial value
	if handler.MessagesReceived() != 0 {
		t.Errorf("Initial MessagesReceived() = %d, want 0", handler.MessagesReceived())
	}

	// Simulate receiving messages by directly incrementing
	handler.messagesReceived.Add(5)

	if handler.MessagesReceived() != 5 {
		t.Errorf("MessagesReceived() = %d, want 5", handler.MessagesReceived())
	}
}

func TestEchoHandler_MessagesSent(t *testing.T) {
	handler := NewEchoHandler()

	// Initial value
	if handler.MessagesSent() != 0 {
		t.Errorf("Initial MessagesSent() = %d, want 0", handler.MessagesSent())
	}

	// Simulate sending messages by directly incrementing
	handler.messagesSent.Add(3)

	if handler.MessagesSent() != 3 {
		t.Errorf("MessagesSent() = %d, want 3", handler.MessagesSent())
	}
}

func TestEchoHandler_ResetCounters(t *testing.T) {
	handler := NewEchoHandler()

	// Set some values
	handler.messagesReceived.Add(10)
	handler.messagesSent.Add(8)

	if handler.MessagesReceived() != 10 {
		t.Errorf("MessagesReceived() = %d, want 10", handler.MessagesReceived())
	}

	if handler.MessagesSent() != 8 {
		t.Errorf("MessagesSent() = %d, want 8", handler.MessagesSent())
	}

	// Reset
	handler.ResetCounters()

	if handler.MessagesReceived() != 0 {
		t.Errorf("MessagesReceived() after reset = %d, want 0", handler.MessagesReceived())
	}

	if handler.MessagesSent() != 0 {
		t.Errorf("MessagesSent() after reset = %d, want 0", handler.MessagesSent())
	}
}

func TestEchoHandler_ResetCountersMultiple(t *testing.T) {
	handler := NewEchoHandler()

	// Multiple resets should be safe
	for i := 0; i < 5; i++ {
		handler.messagesReceived.Add(5)
		handler.messagesSent.Add(5)
		handler.ResetCounters()

		if handler.MessagesReceived() != 0 {
			t.Errorf("Iteration %d: MessagesReceived() = %d, want 0", i, handler.MessagesReceived())
		}

		if handler.MessagesSent() != 0 {
			t.Errorf("Iteration %d: MessagesSent() = %d, want 0", i, handler.MessagesSent())
		}
	}
}

func TestEchoHandler_ConcurrentCounterAccess(t *testing.T) {
	handler := NewEchoHandler()

	var wg sync.WaitGroup

	// Concurrent increments
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			handler.messagesReceived.Add(1)
		}()
		go func() {
			defer wg.Done()
			handler.messagesSent.Add(1)
		}()
	}

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = handler.MessagesReceived()
		}()
		go func() {
			defer wg.Done()
			_ = handler.MessagesSent()
		}()
	}

	wg.Wait()

	if handler.MessagesReceived() != 100 {
		t.Errorf("MessagesReceived() = %d, want 100", handler.MessagesReceived())
	}

	if handler.MessagesSent() != 100 {
		t.Errorf("MessagesSent() = %d, want 100", handler.MessagesSent())
	}
}

func TestEchoHandler_HandleConnection_ContextCancel(t *testing.T) {
	handler := NewEchoHandler()

	// Create a simple WebSocket server for testing
	echoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Logf("Accept error: %v", err)
			return
		}

		session := ws.NewSession("test-session", "echo", conn)

		// Run handler in goroutine
		go func() {
			_ = handler.HandleConnection(session.Context(), conn, session)
		}()

		// Close session after brief delay
		time.Sleep(50 * time.Millisecond)
		session.Close()
	}))
	defer echoServer.Close()

	// Connect to the test server
	wsURL := "ws" + strings.TrimPrefix(echoServer.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	// Wait for handler to process
	time.Sleep(100 * time.Millisecond)
}

func TestEchoHandler_HandleConnection_Echo(t *testing.T) {
	handler := NewEchoHandler()

	// Create a WebSocket server that uses EchoHandler
	echoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Logf("Accept error: %v", err)
			return
		}

		session := ws.NewSession("test-session", "echo", conn)

		err = handler.HandleConnection(session.Context(), conn, session)
		if err != nil && err != context.Canceled {
			t.Logf("Handler error: %v", err)
		}
	}))
	defer echoServer.Close()

	wsURL := "ws" + strings.TrimPrefix(echoServer.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	// Send a text message
	testMessage := "Hello, Echo!"
	err = conn.Write(ctx, websocket.MessageText, []byte(testMessage))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Read the echoed message
	msgType, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if msgType != websocket.MessageText {
		t.Errorf("Message type = %v, want Text", msgType)
	}

	if string(data) != testMessage {
		t.Errorf("Echo = %q, want %q", string(data), testMessage)
	}

	// Verify counters were updated
	if handler.MessagesReceived() != 1 {
		t.Errorf("MessagesReceived() = %d, want 1", handler.MessagesReceived())
	}

	if handler.MessagesSent() != 1 {
		t.Errorf("MessagesSent() = %d, want 1", handler.MessagesSent())
	}
}

func TestEchoHandler_HandleConnection_MultipleMessages(t *testing.T) {
	handler := NewEchoHandler()

	echoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "echo", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer echoServer.Close()

	wsURL := "ws" + strings.TrimPrefix(echoServer.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	messages := []string{"first", "second", "third", "fourth", "fifth"}

	for i, msg := range messages {
		err = conn.Write(ctx, websocket.MessageText, []byte(msg))
		if err != nil {
			t.Fatalf("Write %d failed: %v", i, err)
		}

		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("Read %d failed: %v", i, err)
		}

		if string(data) != msg {
			t.Errorf("Echo %d = %q, want %q", i, string(data), msg)
		}
	}

	// Verify counters
	if handler.MessagesReceived() != uint64(len(messages)) {
		t.Errorf("MessagesReceived() = %d, want %d", handler.MessagesReceived(), len(messages))
	}

	if handler.MessagesSent() != uint64(len(messages)) {
		t.Errorf("MessagesSent() = %d, want %d", handler.MessagesSent(), len(messages))
	}
}

func TestEchoHandler_HandleConnection_BinaryMessages(t *testing.T) {
	handler := NewEchoHandler()

	echoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "echo", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer echoServer.Close()

	wsURL := "ws" + strings.TrimPrefix(echoServer.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	// Send binary data
	binaryData := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE}
	err = conn.Write(ctx, websocket.MessageBinary, binaryData)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	msgType, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if msgType != websocket.MessageBinary {
		t.Errorf("Message type = %v, want Binary", msgType)
	}

	if len(data) != len(binaryData) {
		t.Errorf("Data length = %d, want %d", len(data), len(binaryData))
	}

	for i, b := range binaryData {
		if data[i] != b {
			t.Errorf("Data[%d] = %x, want %x", i, data[i], b)
		}
	}
}

func TestEchoHandler_HandleConnection_SessionActivityUpdated(t *testing.T) {
	handler := NewEchoHandler()

	echoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "echo", conn)

		initialActivity := session.GetLastActivity()

		// Brief delay to ensure time difference
		time.Sleep(10 * time.Millisecond)

		// Start handler in goroutine
		go func() {
			_ = handler.HandleConnection(session.Context(), conn, session)
		}()

		// Wait for a message to be processed
		time.Sleep(100 * time.Millisecond)

		newActivity := session.GetLastActivity()

		if !newActivity.After(initialActivity) {
			t.Errorf("Session activity should be updated after message")
		}
	}))
	defer echoServer.Close()

	wsURL := "ws" + strings.TrimPrefix(echoServer.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	// Send a message to trigger activity update
	err = conn.Write(ctx, websocket.MessageText, []byte("test"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Read the echo
	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	// Give time for server-side checks
	time.Sleep(150 * time.Millisecond)
}

func TestEchoHandler_HandleConnection_NormalClosure(t *testing.T) {
	handler := NewEchoHandler()
	handlerDone := make(chan struct{})

	echoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "echo", conn)

		err = handler.HandleConnection(session.Context(), conn, session)
		close(handlerDone)

		// Normal closure should return nil error
		if err != nil {
			t.Errorf("Handler returned error on normal closure: %v", err)
		}
	}))
	defer echoServer.Close()

	wsURL := "ws" + strings.TrimPrefix(echoServer.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}

	// Close with normal closure status
	conn.Close(websocket.StatusNormalClosure, "test complete")

	// Wait for handler to complete
	select {
	case <-handlerDone:
		// Handler completed
	case <-time.After(time.Second):
		t.Error("Handler did not complete after connection close")
	}
}

func TestEchoHandler_HandleConnection_AbnormalClosure(t *testing.T) {
	handler := NewEchoHandler()
	handlerErr := make(chan error, 1)

	echoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "echo", conn)

		err = handler.HandleConnection(session.Context(), conn, session)
		handlerErr <- err
	}))
	defer echoServer.Close()

	wsURL := "ws" + strings.TrimPrefix(echoServer.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}

	// Close with an abnormal/protocol error status to trigger error return path
	conn.Close(websocket.StatusProtocolError, "protocol error")

	// Wait for handler to complete
	select {
	case err := <-handlerErr:
		// Abnormal closure should return an error (non-nil)
		if err == nil {
			t.Error("Handler should return error on abnormal closure")
		}
	case <-time.After(2 * time.Second):
		t.Error("Handler did not complete after connection close")
	}
}

func TestEchoHandler_ConcurrentConnections(t *testing.T) {
	handler := NewEchoHandler()

	echoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session-"+r.RemoteAddr, "echo", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer echoServer.Close()

	wsURL := "ws" + strings.TrimPrefix(echoServer.URL, "http")

	var wg sync.WaitGroup
	numConnections := 5
	messagesPerConnection := 3

	for i := 0; i < numConnections; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			conn, resp, err := websocket.Dial(ctx, wsURL, nil)
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			if err != nil {
				t.Errorf("Connection %d dial failed: %v", idx, err)
				return
			}
			defer conn.Close(websocket.StatusNormalClosure, "test complete")

			for j := 0; j < messagesPerConnection; j++ {
				msg := []byte("test message")
				err = conn.Write(ctx, websocket.MessageText, msg)
				if err != nil {
					t.Errorf("Connection %d write %d failed: %v", idx, j, err)
					return
				}

				_, data, err := conn.Read(ctx)
				if err != nil {
					t.Errorf("Connection %d read %d failed: %v", idx, j, err)
					return
				}

				if string(data) != string(msg) {
					t.Errorf("Connection %d message %d: got %q, want %q", idx, j, string(data), string(msg))
				}
			}
		}(i)
	}

	wg.Wait()

	expectedMessages := uint64(numConnections * messagesPerConnection)
	if handler.MessagesReceived() != expectedMessages {
		t.Errorf("MessagesReceived() = %d, want %d", handler.MessagesReceived(), expectedMessages)
	}

	if handler.MessagesSent() != expectedMessages {
		t.Errorf("MessagesSent() = %d, want %d", handler.MessagesSent(), expectedMessages)
	}
}

func TestEchoHandler_LargeMessage(t *testing.T) {
	handler := NewEchoHandler()
	const largeReadLimit = 10 * 1024 * 1024 // 10MB

	echoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		// Set a large read limit on server side
		conn.SetReadLimit(largeReadLimit)

		session := ws.NewSession("test-session", "echo", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer echoServer.Close()

	wsURL := "ws" + strings.TrimPrefix(echoServer.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	// Set a large read limit on client side for reading the echo response
	conn.SetReadLimit(largeReadLimit)

	// Create a 1MB message
	largeMessage := make([]byte, 1024*1024)
	for i := range largeMessage {
		largeMessage[i] = byte(i % 256)
	}

	err = conn.Write(ctx, websocket.MessageBinary, largeMessage)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if len(data) != len(largeMessage) {
		t.Errorf("Echoed data length = %d, want %d", len(data), len(largeMessage))
	}

	// Spot check some bytes
	for _, idx := range []int{0, 100, 1000, 10000, len(largeMessage) - 1} {
		if data[idx] != largeMessage[idx] {
			t.Errorf("Data[%d] = %d, want %d", idx, data[idx], largeMessage[idx])
		}
	}
}

func TestEchoHandler_EmptyMessage(t *testing.T) {
	handler := NewEchoHandler()

	echoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "echo", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer echoServer.Close()

	wsURL := "ws" + strings.TrimPrefix(echoServer.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	// Send empty message
	err = conn.Write(ctx, websocket.MessageText, []byte{})
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if len(data) != 0 {
		t.Errorf("Echoed data length = %d, want 0", len(data))
	}

	// Counter should still be incremented
	if handler.MessagesReceived() != 1 {
		t.Errorf("MessagesReceived() = %d, want 1", handler.MessagesReceived())
	}
}
