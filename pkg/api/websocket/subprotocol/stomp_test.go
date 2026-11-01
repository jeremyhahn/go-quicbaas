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

//go:build quicbaas_stomp

package subprotocol

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
)

func TestNewSTOMPHandler(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	if handler == nil {
		t.Fatal("NewSTOMPHandler(nil) returned nil")
	}

	if handler.Hub() == nil {
		t.Error("NewSTOMPHandler(nil) should create default hub")
	}

	if handler.MessagesReceived() != 0 {
		t.Errorf("MessagesReceived() = %d, want 0", handler.MessagesReceived())
	}

	if handler.MessagesSent() != 0 {
		t.Errorf("MessagesSent() = %d, want 0", handler.MessagesSent())
	}
}

func TestNewSTOMPHandler_WithHub(t *testing.T) {
	hub := NewInMemoryPubSubHub()
	handler := NewSTOMPHandler(hub)

	if handler.Hub() != hub {
		t.Error("NewSTOMPHandler should use provided hub")
	}
}

func TestSTOMPHandler_Name(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	if handler.Name() != "stomp" {
		t.Errorf("Name() = %q, want stomp", handler.Name())
	}
}

func TestSTOMPHandler_ResetCounters(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	handler.messagesReceived.Add(10)
	handler.messagesSent.Add(8)

	if handler.MessagesReceived() != 10 {
		t.Errorf("MessagesReceived() = %d, want 10", handler.MessagesReceived())
	}

	if handler.MessagesSent() != 8 {
		t.Errorf("MessagesSent() = %d, want 8", handler.MessagesSent())
	}

	handler.ResetCounters()

	if handler.MessagesReceived() != 0 {
		t.Errorf("MessagesReceived() after reset = %d, want 0", handler.MessagesReceived())
	}

	if handler.MessagesSent() != 0 {
		t.Errorf("MessagesSent() after reset = %d, want 0", handler.MessagesSent())
	}
}

func TestSTOMPHandler_ClientCount(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	if handler.ClientCount() != 0 {
		t.Errorf("Initial ClientCount() = %d, want 0", handler.ClientCount())
	}

	// Add client state directly
	handler.clientsMu.Lock()
	handler.clients["test-client"] = &stompClientState{
		connected:     true,
		subscriptions: make(map[string]stompSubscription),
		transactions:  make(map[string]*stompTransaction),
		pendingAcks:   make(map[string]*pendingMessage),
	}
	handler.clientsMu.Unlock()

	if handler.ClientCount() != 1 {
		t.Errorf("ClientCount() = %d, want 1", handler.ClientCount())
	}
}

func TestSTOMPHandler_ParseFrame_Empty(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	_, err := handler.parseFrame([]byte{})
	if err == nil {
		t.Error("Expected error for empty frame")
	}

	var protoErr *ErrSTOMPProtocolError
	if !errors.As(err, &protoErr) {
		t.Errorf("Expected ErrSTOMPProtocolError, got %T", err)
	}
}

func TestSTOMPHandler_ParseFrame_MissingCommand(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	_, err := handler.parseFrame([]byte("\n"))
	if err == nil {
		t.Error("Expected error for missing command")
	}
}

func TestSTOMPHandler_ParseFrame_ConnectFrame(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	frameData := []byte("CONNECT\naccept-version:1.2\nhost:localhost\n\n\x00")

	frame, err := handler.parseFrame(frameData)
	if err != nil {
		t.Fatalf("parseFrame failed: %v", err)
	}

	if frame.Command != "CONNECT" {
		t.Errorf("Command = %q, want CONNECT", frame.Command)
	}

	if frame.Headers["accept-version"] != "1.2" {
		t.Errorf("accept-version = %q, want 1.2", frame.Headers["accept-version"])
	}

	if frame.Headers["host"] != "localhost" {
		t.Errorf("host = %q, want localhost", frame.Headers["host"])
	}
}

func TestSTOMPHandler_ParseFrame_SendFrameWithBody(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	frameData := []byte("SEND\ndestination:/queue/test\ncontent-type:text/plain\n\nHello World\x00")

	frame, err := handler.parseFrame(frameData)
	if err != nil {
		t.Fatalf("parseFrame failed: %v", err)
	}

	if frame.Command != "SEND" {
		t.Errorf("Command = %q, want SEND", frame.Command)
	}

	if frame.Headers["destination"] != "/queue/test" {
		t.Errorf("destination = %q, want /queue/test", frame.Headers["destination"])
	}

	if string(frame.Body) != "Hello World" {
		t.Errorf("Body = %q, want Hello World", string(frame.Body))
	}
}

func TestSTOMPHandler_ParseFrame_HeaderEscapes(t *testing.T) {
	// SEND frames decode STOMP 1.2 header escapes; undefined or trailing
	// escapes are fatal protocol errors.
	tests := []struct {
		name    string
		header  string
		want    string
		wantErr bool
	}{
		{name: "colon and newline", header: "/queue/test\\c1\\nline2", want: "/queue/test:1\nline2"},
		{name: "carriage return", header: "/queue/test\\rvalue", want: "/queue/test\rvalue"},
		{name: "undefined escape", header: "/queue/test\\tvalue", wantErr: true},
		{name: "trailing backslash", header: "/queue/test\\", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewSTOMPHandler(nil)

			frame, err := handler.parseFrame([]byte("SEND\ndestination:" + tt.header + "\n\n\x00"))
			if tt.wantErr {
				var escapeErr *STOMPInvalidEscapeError
				if !errors.As(err, &escapeErr) {
					t.Fatalf("Expected STOMPInvalidEscapeError, got %T: %v", err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseFrame failed: %v", err)
			}
			if frame.Headers["destination"] != tt.want {
				t.Errorf("destination = %q, want %q", frame.Headers["destination"], tt.want)
			}
		})
	}
}

func TestSTOMPHandler_ParseFrame_ConnectFrameNoEscaping(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	// CONNECT frames should NOT have escaping applied (backward compatibility with STOMP 1.0)
	// The backslash-n should be preserved literally, not converted to newline
	frameData := []byte("CONNECT\naccept-version:1.2\nhost:localhost\\ntest\n\n\x00")

	frame, err := handler.parseFrame(frameData)
	if err != nil {
		t.Fatalf("parseFrame failed: %v", err)
	}

	// The \n should NOT be converted because CONNECT doesn't use escaping
	expected := "localhost\\ntest"
	if frame.Headers["host"] != expected {
		t.Errorf("host = %q, want %q (CONNECT should not unescape)", frame.Headers["host"], expected)
	}
}

func TestSTOMPHandler_DecodeHeaderValue(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"simple", "simple", false},
		{"with\\nnewline", "with\nnewline", false},
		{"with\\ccolon", "with:colon", false},
		{"with\\\\backslash", "with\\backslash", false},
		{"with\\rcarriage", "with\rcarriage", false},
		{"all\\n\\c\\\\\\rtogether", "all\n:\\\rtogether", false},
		{"trailing\\", "", true},   // trailing backslash is error
		{"test\\xvalue", "", true}, // undefined escape \x is error
		{"test\\tvalue", "", true}, // undefined escape \t is error
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := handler.decodeHeaderValue(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("decodeHeaderValue(%q) expected error, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("decodeHeaderValue(%q) unexpected error: %v", tt.input, err)
				}
				if got != tt.want {
					t.Errorf("decodeHeaderValue(%q) = %q, want %q", tt.input, got, tt.want)
				}
			}
		})
	}
}

func TestSTOMPHandler_EncodeHeaderValue(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"with\nnewline", "with\\nnewline"},
		{"with:colon", "with\\ccolon"},
		{"with\\backslash", "with\\\\backslash"},
		{"with\rcarriage", "with\\rcarriage"},
		{"all\n:\\\rtogether", "all\\n\\c\\\\\\rtogether"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := handler.encodeHeaderValue(tt.input)
			if got != tt.want {
				t.Errorf("encodeHeaderValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSTOMPHandler_BuildConnectedFrame(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	frame := handler.buildConnectedFrame("1.2", "session-123", "")

	if !bytes.HasPrefix(frame, []byte("CONNECTED\n")) {
		t.Error("Frame should start with CONNECTED")
	}

	if !bytes.Contains(frame, []byte("version:1.2")) {
		t.Error("Frame should contain version:1.2")
	}

	if !bytes.Contains(frame, []byte("session:session-123")) {
		t.Error("Frame should contain session:session-123")
	}

	if !bytes.Contains(frame, []byte("server:go-quicbaas/1.0")) {
		t.Error("Frame should contain server:go-quicbaas/1.0")
	}

	if !bytes.HasSuffix(frame, []byte{0x00}) {
		t.Error("Frame should end with NULL byte")
	}
}

func TestSTOMPHandler_BuildMessageFrame(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		ackID       string
		want        []string
		notWant     []string
	}{
		{
			name:        "content type, no ack",
			contentType: "text/plain",
			want: []string{"destination:/queue/test", "subscription:sub-1", "message-id:msg-123",
				"content-type:text/plain", "content-length:5", "Hello"},
			notWant: []string{"ack:"},
		},
		{
			name:        "with ack id",
			contentType: "text/plain",
			ackID:       "ack-456",
			want:        []string{"ack:ack-456"},
		},
		{
			name:    "no content type",
			want:    []string{"content-length:5"},
			notWant: []string{"content-type:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewSTOMPHandler(nil)

			frame := handler.buildMessageFrame("/queue/test", "sub-1", "msg-123", []byte("Hello"), tt.contentType, tt.ackID)

			if !bytes.HasPrefix(frame, []byte("MESSAGE\n")) {
				t.Error("Frame should start with MESSAGE")
			}
			if !bytes.HasSuffix(frame, []byte{0x00}) {
				t.Error("Frame should end with NULL byte")
			}
			for _, s := range tt.want {
				if !bytes.Contains(frame, []byte(s)) {
					t.Errorf("Frame should contain %q", s)
				}
			}
			for _, s := range tt.notWant {
				if bytes.Contains(frame, []byte(s)) {
					t.Errorf("Frame should not contain %q", s)
				}
			}
		})
	}
}

func TestSTOMPHandler_BuildReceiptFrame(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	frame := handler.buildReceiptFrame("receipt-789")

	if !bytes.HasPrefix(frame, []byte("RECEIPT\n")) {
		t.Error("Frame should start with RECEIPT")
	}

	if !bytes.Contains(frame, []byte("receipt-id:receipt-789")) {
		t.Error("Frame should contain receipt-id:receipt-789")
	}

	if !bytes.HasSuffix(frame, []byte{0x00}) {
		t.Error("Frame should end with NULL byte")
	}
}

func TestSTOMPHandler_BuildErrorFrame(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	frame := handler.buildErrorFrame("Something went wrong", "receipt-123", nil)

	if !bytes.HasPrefix(frame, []byte("ERROR\n")) {
		t.Error("Frame should start with ERROR")
	}

	if !bytes.Contains(frame, []byte("message:Something went wrong")) {
		t.Error("Frame should contain message:Something went wrong")
	}

	if !bytes.Contains(frame, []byte("receipt-id:receipt-123")) {
		t.Error("Frame should contain receipt-id:receipt-123")
	}

	if !bytes.HasSuffix(frame, []byte{0x00}) {
		t.Error("Frame should end with NULL byte")
	}
}

func TestSTOMPHandler_BuildErrorFrame_NoReceipt(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	frame := handler.buildErrorFrame("Error message", "", nil)

	if bytes.Contains(frame, []byte("receipt-id")) {
		t.Error("Frame should not contain receipt-id when not provided")
	}
}

func TestSTOMPHandler_BuildErrorFrame_WithBody(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	body := []byte("Detailed error information here")
	frame := handler.buildErrorFrame("Error message", "", body)

	if !bytes.Contains(frame, []byte("content-type:text/plain")) {
		t.Error("Frame should contain content-type:text/plain when body present")
	}

	if !bytes.Contains(frame, []byte("content-length:31")) {
		t.Error("Frame should contain content-length:31 when body present")
	}

	if !bytes.Contains(frame, body) {
		t.Error("Frame should contain body")
	}
}

func TestSTOMPErrorMessages(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "protocol", err: &ErrSTOMPProtocolError{Message: "test error"}, want: "stomp protocol error: test error"},
		{name: "missing header", err: &STOMPMissingHeaderError{Header: "destination"}, want: "stomp protocol error: missing required header: destination"},
		{name: "invalid escape", err: &STOMPInvalidEscapeError{Sequence: "\\t"}, want: "stomp protocol error: undefined escape sequence: \\t"},
		{name: "invalid transaction", err: &STOMPInvalidTransactionError{TransactionID: "tx-1", Reason: "not found"}, want: "stomp protocol error: transaction tx-1: not found"},
		{name: "disconnect", err: ErrSTOMPDisconnect, want: "stomp: client disconnected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

// buildSTOMPFrame creates a STOMP frame for testing.
func buildSTOMPFrame(command string, headers map[string]string, body string) []byte {
	buf := &bytes.Buffer{}
	buf.WriteString(command)
	buf.WriteByte('\n')
	for k, v := range headers {
		buf.WriteString(k)
		buf.WriteByte(':')
		buf.WriteString(v)
		buf.WriteByte('\n')
	}
	buf.WriteByte('\n')
	if body != "" {
		buf.WriteString(body)
	}
	buf.WriteByte(0x00)
	return buf.Bytes()
}

func TestSTOMPHandler_HandleConnection_ConnectDisconnect(t *testing.T) {
	handler := NewSTOMPHandler(nil)
	handlerDone := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Logf("Accept error: %v", err)
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		err = handler.HandleConnection(session.Context(), conn, session)
		handlerDone <- err
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// Send CONNECT frame
	connectFrame := buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, "")
	err = conn.Write(ctx, websocket.MessageText, connectFrame)
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}

	// Read CONNECTED
	_, connectedData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	// Verify CONNECTED frame
	if !bytes.HasPrefix(connectedData, []byte("CONNECTED")) {
		t.Errorf("Expected CONNECTED frame, got: %s", string(connectedData))
	}

	// Send DISCONNECT
	disconnectFrame := buildSTOMPFrame("DISCONNECT", nil, "")
	err = conn.Write(ctx, websocket.MessageText, disconnectFrame)
	if err != nil {
		t.Fatalf("Write DISCONNECT failed: %v", err)
	}

	// Wait for handler to complete
	select {
	case err := <-handlerDone:
		if err != nil {
			t.Errorf("Handler returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Handler did not complete after disconnect")
	}

	// Verify message counters
	if handler.MessagesReceived() != 2 {
		t.Errorf("MessagesReceived() = %d, want 2", handler.MessagesReceived())
	}

	// CONNECTED was sent
	if handler.MessagesSent() < 1 {
		t.Errorf("MessagesSent() = %d, want >= 1", handler.MessagesSent())
	}
}

func TestSTOMPHandler_HandleConnection_SubscribeUnsubscribe(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// Send CONNECT
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}

	// Read CONNECTED
	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	// Send SUBSCRIBE with receipt
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("SUBSCRIBE", map[string]string{
		"id":          "sub-1",
		"destination": "/queue/test",
		"ack":         "auto",
		"receipt":     "receipt-1",
	}, ""))
	if err != nil {
		t.Fatalf("Write SUBSCRIBE failed: %v", err)
	}

	// Read RECEIPT
	_, receiptData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read RECEIPT failed: %v", err)
	}

	if !bytes.HasPrefix(receiptData, []byte("RECEIPT")) {
		t.Errorf("Expected RECEIPT frame, got: %s", string(receiptData))
	}

	// Send UNSUBSCRIBE with receipt
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("UNSUBSCRIBE", map[string]string{
		"id":      "sub-1",
		"receipt": "receipt-2",
	}, ""))
	if err != nil {
		t.Fatalf("Write UNSUBSCRIBE failed: %v", err)
	}

	// Read RECEIPT for UNSUBSCRIBE
	_, receiptData2, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read RECEIPT 2 failed: %v", err)
	}

	if !bytes.HasPrefix(receiptData2, []byte("RECEIPT")) {
		t.Errorf("Expected RECEIPT frame, got: %s", string(receiptData2))
	}
}

func TestSTOMPHandler_HandleConnection_DisconnectWithReceipt(t *testing.T) {
	handler := NewSTOMPHandler(nil)
	handlerDone := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		err = handler.HandleConnection(session.Context(), conn, session)
		handlerDone <- err
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// Send CONNECT
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}

	// Read CONNECTED
	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	// Send DISCONNECT with receipt
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("DISCONNECT", map[string]string{
		"receipt": "disconnect-receipt",
	}, ""))
	if err != nil {
		t.Fatalf("Write DISCONNECT failed: %v", err)
	}

	// Read RECEIPT before disconnect
	_, receiptData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read RECEIPT failed: %v", err)
	}

	if !bytes.HasPrefix(receiptData, []byte("RECEIPT")) {
		t.Errorf("Expected RECEIPT frame, got: %s", string(receiptData))
	}

	if !bytes.Contains(receiptData, []byte("receipt-id:disconnect-receipt")) {
		t.Error("Receipt should contain disconnect-receipt")
	}

	// Wait for handler to complete
	select {
	case <-handlerDone:
		// Handler completed
	case <-time.After(2 * time.Second):
		t.Error("Handler did not complete after disconnect")
	}
}

func TestSTOMPHandler_HandleConnection_StompCommand(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// Send STOMP command (alternative to CONNECT in STOMP 1.2)
	stompFrame := buildSTOMPFrame("STOMP", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, "")
	err = conn.Write(ctx, websocket.MessageText, stompFrame)
	if err != nil {
		t.Fatalf("Write STOMP failed: %v", err)
	}

	// Read CONNECTED
	_, connectedData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	if !bytes.HasPrefix(connectedData, []byte("CONNECTED")) {
		t.Errorf("Expected CONNECTED frame, got: %s", string(connectedData))
	}
}

func TestSTOMPHandler_HandleConnection_VersionNegotiation(t *testing.T) {
	tests := []struct {
		acceptVersion string
		host          string
		wantVersion   string
	}{
		{"1.2", "localhost", "1.2"},
		{"1.1,1.2", "localhost", "1.2"},
		{"1.0,1.1", "", "1.1"},
		{"1.0", "", "1.0"},
		{"", "", "1.0"}, // Default to 1.0
	}

	for _, tt := range tests {
		t.Run(tt.acceptVersion, func(t *testing.T) {
			handler := NewSTOMPHandler(nil)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}

				session := ws.NewSession("test-session", "stomp", conn)

				_ = handler.HandleConnection(session.Context(), conn, session)
			}))
			defer server.Close()

			wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

			headers := make(map[string]string)
			if tt.acceptVersion != "" {
				headers["accept-version"] = tt.acceptVersion
			}
			if tt.host != "" {
				headers["host"] = tt.host
			}

			err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", headers, ""))
			if err != nil {
				t.Fatalf("Write CONNECT failed: %v", err)
			}

			_, connectedData, err := conn.Read(ctx)
			if err != nil {
				t.Fatalf("Read CONNECTED failed: %v", err)
			}

			expected := "version:" + tt.wantVersion
			if !bytes.Contains(connectedData, []byte(expected)) {
				t.Errorf("Expected %s in CONNECTED frame, got: %s", expected, string(connectedData))
			}
		})
	}
}

func TestSTOMPHandler_HandleConnection_NotConnectedError(t *testing.T) {
	handler := NewSTOMPHandler(nil)
	handlerErr := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		err = handler.HandleConnection(session.Context(), conn, session)
		handlerErr <- err
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// Send SUBSCRIBE without CONNECT first
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("SUBSCRIBE", map[string]string{
		"id":          "sub-1",
		"destination": "/queue/test",
	}, ""))
	if err != nil {
		t.Fatalf("Write SUBSCRIBE failed: %v", err)
	}

	// Read ERROR frame
	_, errorData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read ERROR failed: %v", err)
	}

	if !bytes.HasPrefix(errorData, []byte("ERROR")) {
		t.Errorf("Expected ERROR frame, got: %s", string(errorData))
	}

	if !bytes.Contains(errorData, []byte("not connected")) {
		t.Error("Error should mention not connected")
	}

	// Wait for handler error
	select {
	case err := <-handlerErr:
		var protoErr *ErrSTOMPProtocolError
		if !errors.As(err, &protoErr) {
			t.Errorf("Expected ErrSTOMPProtocolError, got %T: %v", err, err)
		}
	case <-time.After(time.Second):
		t.Error("Handler did not return error")
	}
}

func TestSTOMPHandler_HandleConnection_MissingHeaders(t *testing.T) {
	tests := []struct {
		name    string
		command string
		headers map[string]string
		wantErr string
	}{
		{
			name:    "SUBSCRIBE missing destination",
			command: "SUBSCRIBE",
			headers: map[string]string{"id": "sub-1"},
			wantErr: "missing required header",
		},
		{
			name:    "SUBSCRIBE missing id",
			command: "SUBSCRIBE",
			headers: map[string]string{"destination": "/queue/test"},
			wantErr: "missing required header",
		},
		{
			name:    "UNSUBSCRIBE missing id",
			command: "UNSUBSCRIBE",
			headers: map[string]string{},
			wantErr: "missing required header",
		},
		{
			name:    "SEND missing destination",
			command: "SEND",
			headers: map[string]string{},
			wantErr: "missing required header",
		},
		{
			name:    "ACK missing id",
			command: "ACK",
			headers: map[string]string{},
			wantErr: "missing required header",
		},
		{
			name:    "NACK missing id",
			command: "NACK",
			headers: map[string]string{},
			wantErr: "missing required header",
		},
		{
			name:    "BEGIN missing transaction",
			command: "BEGIN",
			headers: map[string]string{},
			wantErr: "missing required header",
		},
		{
			name:    "COMMIT missing transaction",
			command: "COMMIT",
			headers: map[string]string{},
			wantErr: "missing required header",
		},
		{
			name:    "ABORT missing transaction",
			command: "ABORT",
			headers: map[string]string{},
			wantErr: "missing required header",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewSTOMPHandler(nil)
			handlerErr := make(chan error, 1)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}

				session := ws.NewSession("test-session", "stomp", conn)

				err = handler.HandleConnection(session.Context(), conn, session)
				handlerErr <- err
			}))
			defer server.Close()

			wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

			// Connect first
			err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
				"accept-version": "1.2",
				"host":           "localhost",
			}, ""))
			if err != nil {
				t.Fatalf("Write CONNECT failed: %v", err)
			}

			_, _, err = conn.Read(ctx)
			if err != nil {
				t.Fatalf("Read CONNECTED failed: %v", err)
			}

			// Send command with missing headers
			err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame(tt.command, tt.headers, ""))
			if err != nil {
				t.Fatalf("Write %s failed: %v", tt.command, err)
			}

			// Read ERROR frame
			_, errorData, err := conn.Read(ctx)
			if err != nil {
				t.Fatalf("Read ERROR failed: %v", err)
			}

			if !bytes.HasPrefix(errorData, []byte("ERROR")) {
				t.Errorf("Expected ERROR frame, got: %s", string(errorData))
			}

			if !bytes.Contains(errorData, []byte(tt.wantErr)) {
				t.Errorf("Error should mention %q, got: %s", tt.wantErr, string(errorData))
			}
		})
	}
}

func TestSTOMPHandler_HandleConnection_UnknownCommand(t *testing.T) {
	handler := NewSTOMPHandler(nil)
	handlerErr := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		err = handler.HandleConnection(session.Context(), conn, session)
		handlerErr <- err
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// Send unknown command
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("UNKNOWN", nil, ""))
	if err != nil {
		t.Fatalf("Write UNKNOWN failed: %v", err)
	}

	// Read ERROR frame
	_, errorData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read ERROR failed: %v", err)
	}

	if !bytes.HasPrefix(errorData, []byte("ERROR")) {
		t.Errorf("Expected ERROR frame, got: %s", string(errorData))
	}

	if !bytes.Contains(errorData, []byte("unknown command")) {
		t.Error("Error should mention unknown command")
	}
}

func TestSTOMPHandler_HandleConnection_NormalClosure(t *testing.T) {
	handler := NewSTOMPHandler(nil)
	handlerErr := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		err = handler.HandleConnection(session.Context(), conn, session)
		handlerErr <- err
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}

	// Close with normal closure
	conn.Close(websocket.StatusNormalClosure, "test complete")

	// Wait for handler to complete
	select {
	case err := <-handlerErr:
		if err != nil {
			t.Errorf("Handler should return nil on normal closure, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("Handler did not complete after normal closure")
	}
}

func TestSTOMPHandler_HandleConnection_ContextCancel(t *testing.T) {
	handler := NewSTOMPHandler(nil)
	handlerDone := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		go func() {
			_ = handler.HandleConnection(session.Context(), conn, session)
			close(handlerDone)
		}()

		session.Close()
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// Wait for handler to complete
	select {
	case <-handlerDone:
		// Handler completed
	case <-time.After(time.Second):
		t.Error("Handler did not complete after context cancel")
	}
}

func TestSTOMPHandler_ConcurrentClients(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("session-"+r.RemoteAddr, "stomp", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	var wg sync.WaitGroup
	numClients := 5

	for i := 0; i < numClients; i++ {
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
				t.Errorf("Client %d dial failed: %v", idx, err)
				return
			}
			defer conn.Close(websocket.StatusNormalClosure, "test complete")

			// Send CONNECT
			err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
				"accept-version": "1.2",
				"host":           "localhost",
			}, ""))
			if err != nil {
				t.Errorf("Client %d write CONNECT failed: %v", idx, err)
				return
			}

			// Read CONNECTED
			_, _, err = conn.Read(ctx)
			if err != nil {
				t.Errorf("Client %d read CONNECTED failed: %v", idx, err)
				return
			}

			// Send DISCONNECT
			err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("DISCONNECT", nil, ""))
			if err != nil {
				t.Errorf("Client %d write DISCONNECT failed: %v", idx, err)
				return
			}
		}(i)
	}

	wg.Wait()

	// Each client sends CONNECT and DISCONNECT
	expectedReceived := uint64(numClients * 2)
	if handler.MessagesReceived() != expectedReceived {
		t.Errorf("MessagesReceived() = %d, want %d", handler.MessagesReceived(), expectedReceived)
	}

	// Each client receives CONNECTED
	expectedSent := uint64(numClients)
	if handler.MessagesSent() != expectedSent {
		t.Errorf("MessagesSent() = %d, want %d", handler.MessagesSent(), expectedSent)
	}
}

func TestSTOMPHandler_AckNackCommands(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// Connect
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}

	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	// Test ACK with receipt
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("ACK", map[string]string{
		"id":      "msg-1",
		"receipt": "ack-receipt",
	}, ""))
	if err != nil {
		t.Fatalf("Write ACK failed: %v", err)
	}

	_, receiptData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read ACK RECEIPT failed: %v", err)
	}

	if !bytes.HasPrefix(receiptData, []byte("RECEIPT")) {
		t.Errorf("Expected RECEIPT frame for ACK, got: %s", string(receiptData))
	}

	// Test NACK with receipt
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("NACK", map[string]string{
		"id":      "msg-2",
		"receipt": "nack-receipt",
	}, ""))
	if err != nil {
		t.Fatalf("Write NACK failed: %v", err)
	}

	_, receiptData2, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read NACK RECEIPT failed: %v", err)
	}

	if !bytes.HasPrefix(receiptData2, []byte("RECEIPT")) {
		t.Errorf("Expected RECEIPT frame for NACK, got: %s", string(receiptData2))
	}
}

func TestSTOMPHandler_TransactionCommands(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// Connect
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}

	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	// Test BEGIN
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("BEGIN", map[string]string{
		"transaction": "tx-1",
		"receipt":     "begin-receipt",
	}, ""))
	if err != nil {
		t.Fatalf("Write BEGIN failed: %v", err)
	}

	_, receiptData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read BEGIN RECEIPT failed: %v", err)
	}

	if !bytes.HasPrefix(receiptData, []byte("RECEIPT")) {
		t.Errorf("Expected RECEIPT frame for BEGIN, got: %s", string(receiptData))
	}

	// Test COMMIT
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("COMMIT", map[string]string{
		"transaction": "tx-1",
		"receipt":     "commit-receipt",
	}, ""))
	if err != nil {
		t.Fatalf("Write COMMIT failed: %v", err)
	}

	_, receiptData2, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read COMMIT RECEIPT failed: %v", err)
	}

	if !bytes.HasPrefix(receiptData2, []byte("RECEIPT")) {
		t.Errorf("Expected RECEIPT frame for COMMIT, got: %s", string(receiptData2))
	}

	// Start another transaction for ABORT test
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("BEGIN", map[string]string{
		"transaction": "tx-2",
	}, ""))
	if err != nil {
		t.Fatalf("Write BEGIN tx-2 failed: %v", err)
	}

	// Test ABORT
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("ABORT", map[string]string{
		"transaction": "tx-2",
		"receipt":     "abort-receipt",
	}, ""))
	if err != nil {
		t.Fatalf("Write ABORT failed: %v", err)
	}

	_, receiptData3, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read ABORT RECEIPT failed: %v", err)
	}

	if !bytes.HasPrefix(receiptData3, []byte("RECEIPT")) {
		t.Errorf("Expected RECEIPT frame for ABORT, got: %s", string(receiptData3))
	}
}

func TestSTOMPHandler_TransactionSendCommit(t *testing.T) {
	handler := NewSTOMPHandler(nil)
	messageReceived := make(chan []byte, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("session-"+r.RemoteAddr, "stomp", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect subscriber
	subscriber, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("Subscriber dial failed: %v", err)
	}
	defer subscriber.Close(websocket.StatusNormalClosure, "test complete")

	err = subscriber.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Subscriber write CONNECT failed: %v", err)
	}

	_, _, err = subscriber.Read(ctx)
	if err != nil {
		t.Fatalf("Subscriber read CONNECTED failed: %v", err)
	}

	err = subscriber.Write(ctx, websocket.MessageText, buildSTOMPFrame("SUBSCRIBE", map[string]string{
		"id":          "sub-1",
		"destination": "/queue/tx-test",
		"ack":         "auto",
	}, ""))
	if err != nil {
		t.Fatalf("Subscriber write SUBSCRIBE failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Start reading messages in goroutine
	go func() {
		for {
			_, data, err := subscriber.Read(ctx)
			if err != nil {
				return
			}
			if bytes.HasPrefix(data, []byte("MESSAGE")) {
				messageReceived <- data
				return
			}
		}
	}()

	// Connect publisher
	publisher, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("Publisher dial failed: %v", err)
	}
	defer publisher.Close(websocket.StatusNormalClosure, "test complete")

	err = publisher.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Publisher write CONNECT failed: %v", err)
	}

	_, _, err = publisher.Read(ctx)
	if err != nil {
		t.Fatalf("Publisher read CONNECTED failed: %v", err)
	}

	// Begin transaction
	err = publisher.Write(ctx, websocket.MessageText, buildSTOMPFrame("BEGIN", map[string]string{
		"transaction": "tx-send",
	}, ""))
	if err != nil {
		t.Fatalf("Publisher write BEGIN failed: %v", err)
	}

	// Send message in transaction
	err = publisher.Write(ctx, websocket.MessageText, buildSTOMPFrame("SEND", map[string]string{
		"destination": "/queue/tx-test",
		"transaction": "tx-send",
	}, "Transaction message"))
	if err != nil {
		t.Fatalf("Publisher write SEND failed: %v", err)
	}

	// Message should not be delivered yet (transaction not committed)
	select {
	case <-messageReceived:
		t.Error("Message should not be delivered before commit")
	case <-time.After(100 * time.Millisecond):
		// Expected - no message yet
	}

	// Commit transaction
	err = publisher.Write(ctx, websocket.MessageText, buildSTOMPFrame("COMMIT", map[string]string{
		"transaction": "tx-send",
	}, ""))
	if err != nil {
		t.Fatalf("Publisher write COMMIT failed: %v", err)
	}

	// Message should now be delivered
	select {
	case msgData := <-messageReceived:
		if !bytes.Contains(msgData, []byte("Transaction message")) {
			t.Error("Message should contain body")
		}
	case <-time.After(2 * time.Second):
		t.Error("Message not delivered after commit")
	}
}

func TestSTOMPHandler_TransactionAbortNoDelivery(t *testing.T) {
	handler := NewSTOMPHandler(nil)
	messageReceived := make(chan []byte, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("session-"+r.RemoteAddr, "stomp", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect subscriber
	subscriber, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("Subscriber dial failed: %v", err)
	}
	defer subscriber.Close(websocket.StatusNormalClosure, "test complete")

	err = subscriber.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Subscriber write CONNECT failed: %v", err)
	}

	_, _, err = subscriber.Read(ctx)
	if err != nil {
		t.Fatalf("Subscriber read CONNECTED failed: %v", err)
	}

	err = subscriber.Write(ctx, websocket.MessageText, buildSTOMPFrame("SUBSCRIBE", map[string]string{
		"id":          "sub-1",
		"destination": "/queue/abort-test",
		"ack":         "auto",
	}, ""))
	if err != nil {
		t.Fatalf("Subscriber write SUBSCRIBE failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Start reading messages in goroutine
	go func() {
		for {
			_, data, err := subscriber.Read(ctx)
			if err != nil {
				return
			}
			if bytes.HasPrefix(data, []byte("MESSAGE")) {
				messageReceived <- data
				return
			}
		}
	}()

	// Connect publisher
	publisher, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("Publisher dial failed: %v", err)
	}
	defer publisher.Close(websocket.StatusNormalClosure, "test complete")

	err = publisher.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Publisher write CONNECT failed: %v", err)
	}

	_, _, err = publisher.Read(ctx)
	if err != nil {
		t.Fatalf("Publisher read CONNECTED failed: %v", err)
	}

	// Begin transaction
	err = publisher.Write(ctx, websocket.MessageText, buildSTOMPFrame("BEGIN", map[string]string{
		"transaction": "tx-abort",
	}, ""))
	if err != nil {
		t.Fatalf("Publisher write BEGIN failed: %v", err)
	}

	// Send message in transaction
	err = publisher.Write(ctx, websocket.MessageText, buildSTOMPFrame("SEND", map[string]string{
		"destination": "/queue/abort-test",
		"transaction": "tx-abort",
	}, "Aborted message"))
	if err != nil {
		t.Fatalf("Publisher write SEND failed: %v", err)
	}

	// Abort transaction
	err = publisher.Write(ctx, websocket.MessageText, buildSTOMPFrame("ABORT", map[string]string{
		"transaction": "tx-abort",
	}, ""))
	if err != nil {
		t.Fatalf("Publisher write ABORT failed: %v", err)
	}

	// Message should never be delivered
	select {
	case <-messageReceived:
		t.Error("Message should not be delivered after abort")
	case <-time.After(500 * time.Millisecond):
		// Expected - no message
	}
}

func TestSTOMPHandler_MessageDeliveryBetweenClients(t *testing.T) {
	handler := NewSTOMPHandler(nil)
	messageReceived := make(chan []byte, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		// Use a unique session ID per connection
		session := ws.NewSession("session-"+r.RemoteAddr, "stomp", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect subscriber
	subscriber, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("Subscriber dial failed: %v", err)
	}
	defer subscriber.Close(websocket.StatusNormalClosure, "test complete")

	// Connect and subscribe
	err = subscriber.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Subscriber write CONNECT failed: %v", err)
	}

	_, _, err = subscriber.Read(ctx)
	if err != nil {
		t.Fatalf("Subscriber read CONNECTED failed: %v", err)
	}

	err = subscriber.Write(ctx, websocket.MessageText, buildSTOMPFrame("SUBSCRIBE", map[string]string{
		"id":          "sub-1",
		"destination": "/queue/test",
		"ack":         "auto",
	}, ""))
	if err != nil {
		t.Fatalf("Subscriber write SUBSCRIBE failed: %v", err)
	}

	// Give subscription time to register
	time.Sleep(50 * time.Millisecond)

	// Start reading messages in goroutine
	go func() {
		for {
			_, data, err := subscriber.Read(ctx)
			if err != nil {
				return
			}
			if bytes.HasPrefix(data, []byte("MESSAGE")) {
				messageReceived <- data
				return
			}
		}
	}()

	// Connect publisher (separate connection)
	publisher, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("Publisher dial failed: %v", err)
	}
	defer publisher.Close(websocket.StatusNormalClosure, "test complete")

	err = publisher.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Publisher write CONNECT failed: %v", err)
	}

	_, _, err = publisher.Read(ctx)
	if err != nil {
		t.Fatalf("Publisher read CONNECTED failed: %v", err)
	}

	// Publish message
	err = publisher.Write(ctx, websocket.MessageText, buildSTOMPFrame("SEND", map[string]string{
		"destination":  "/queue/test",
		"content-type": "text/plain",
	}, "Hello STOMP"))
	if err != nil {
		t.Fatalf("Publisher write SEND failed: %v", err)
	}

	// Wait for message delivery
	select {
	case msgData := <-messageReceived:
		if !bytes.Contains(msgData, []byte("destination:/queue/test")) {
			t.Error("Message should contain destination header")
		}
		if !bytes.Contains(msgData, []byte("subscription:sub-1")) {
			t.Error("Message should contain subscription header")
		}
		if !bytes.Contains(msgData, []byte("Hello STOMP")) {
			t.Error("Message should contain body")
		}
	case <-time.After(2 * time.Second):
		t.Error("Message not delivered within timeout")
	}
}

func TestSTOMPHandler_ParseFrame_MultipleHeaders(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	// STOMP spec: first occurrence of header wins
	frameData := []byte("SEND\ndestination:/queue/first\ndestination:/queue/second\n\n\x00")

	frame, err := handler.parseFrame(frameData)
	if err != nil {
		t.Fatalf("parseFrame failed: %v", err)
	}

	if frame.Headers["destination"] != "/queue/first" {
		t.Errorf("destination = %q, want /queue/first (first occurrence)", frame.Headers["destination"])
	}
}

func TestSTOMPHandler_ParseFrame_EmptyBody(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	frameData := []byte("DISCONNECT\nreceipt:r-1\n\n\x00")

	frame, err := handler.parseFrame(frameData)
	if err != nil {
		t.Fatalf("parseFrame failed: %v", err)
	}

	if frame.Command != "DISCONNECT" {
		t.Errorf("Command = %q, want DISCONNECT", frame.Command)
	}

	if len(frame.Body) != 0 {
		t.Errorf("Body length = %d, want 0", len(frame.Body))
	}
}

func TestSTOMPHandler_SendWithReceipt(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// Connect
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}

	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	// Send with receipt (no subscription, so no MESSAGE echo back)
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("SEND", map[string]string{
		"destination": "/queue/no-subscriber",
		"receipt":     "send-receipt",
	}, "Test message"))
	if err != nil {
		t.Fatalf("Write SEND failed: %v", err)
	}

	_, receiptData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read RECEIPT failed: %v", err)
	}

	if !bytes.HasPrefix(receiptData, []byte("RECEIPT")) {
		t.Errorf("Expected RECEIPT frame, got: %s", string(receiptData))
	}

	if !bytes.Contains(receiptData, []byte("receipt-id:send-receipt")) {
		t.Error("Receipt should contain send-receipt")
	}
}

func TestSTOMPHandler_CommandsNotConnected(t *testing.T) {
	// Test that commands that require connection return error when not connected
	tests := []struct {
		name    string
		command string
		headers map[string]string
	}{
		{"ACK not connected", "ACK", map[string]string{"id": "msg-1"}},
		{"NACK not connected", "NACK", map[string]string{"id": "msg-1"}},
		{"BEGIN not connected", "BEGIN", map[string]string{"transaction": "tx-1"}},
		{"COMMIT not connected", "COMMIT", map[string]string{"transaction": "tx-1"}},
		{"ABORT not connected", "ABORT", map[string]string{"transaction": "tx-1"}},
		{"SEND not connected", "SEND", map[string]string{"destination": "/queue/test"}},
		{"UNSUBSCRIBE not connected", "UNSUBSCRIBE", map[string]string{"id": "sub-1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewSTOMPHandler(nil)
			handlerErr := make(chan error, 1)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}

				session := ws.NewSession("test-session", "stomp", conn)

				err = handler.HandleConnection(session.Context(), conn, session)
				handlerErr <- err
			}))
			defer server.Close()

			wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

			// Send command without CONNECT first
			err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame(tt.command, tt.headers, ""))
			if err != nil {
				t.Fatalf("Write %s failed: %v", tt.command, err)
			}

			// Read ERROR frame
			_, errorData, err := conn.Read(ctx)
			if err != nil {
				t.Fatalf("Read ERROR failed: %v", err)
			}

			if !bytes.HasPrefix(errorData, []byte("ERROR")) {
				t.Errorf("Expected ERROR frame, got: %s", string(errorData))
			}

			if !bytes.Contains(errorData, []byte("not connected")) {
				t.Errorf("Error should mention not connected, got: %s", string(errorData))
			}

			// Wait for handler error
			select {
			case err := <-handlerErr:
				var protoErr *ErrSTOMPProtocolError
				if !errors.As(err, &protoErr) {
					t.Errorf("Expected ErrSTOMPProtocolError, got %T: %v", err, err)
				}
			case <-time.After(time.Second):
				t.Error("Handler did not return error")
			}
		})
	}
}

func TestSTOMPHandler_AckNackNoReceipt(t *testing.T) {
	// Test ACK/NACK without receipt header (no response expected)
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// Connect
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}

	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	// Test ACK without receipt (no response expected)
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("ACK", map[string]string{
		"id": "msg-1",
	}, ""))
	if err != nil {
		t.Fatalf("Write ACK failed: %v", err)
	}

	// Test NACK without receipt (no response expected)
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("NACK", map[string]string{
		"id": "msg-2",
	}, ""))
	if err != nil {
		t.Fatalf("Write NACK failed: %v", err)
	}

	// Send DISCONNECT with receipt to verify connection is still working
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("DISCONNECT", map[string]string{
		"receipt": "final-receipt",
	}, ""))
	if err != nil {
		t.Fatalf("Write DISCONNECT failed: %v", err)
	}

	// Read RECEIPT for DISCONNECT
	_, receiptData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read RECEIPT failed: %v", err)
	}

	if !bytes.HasPrefix(receiptData, []byte("RECEIPT")) {
		t.Errorf("Expected RECEIPT frame, got: %s", string(receiptData))
	}

	if !bytes.Contains(receiptData, []byte("receipt-id:final-receipt")) {
		t.Error("Receipt should contain final-receipt")
	}
}

func TestSTOMPHandler_TransactionCommandsNoReceipt(t *testing.T) {
	// Test BEGIN/COMMIT/ABORT without receipt header (no response expected)
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// Connect
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}

	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	// Test BEGIN without receipt (no response expected)
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("BEGIN", map[string]string{
		"transaction": "tx-1",
	}, ""))
	if err != nil {
		t.Fatalf("Write BEGIN failed: %v", err)
	}

	// Test COMMIT without receipt (no response expected)
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("COMMIT", map[string]string{
		"transaction": "tx-1",
	}, ""))
	if err != nil {
		t.Fatalf("Write COMMIT failed: %v", err)
	}

	// Start another transaction for ABORT
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("BEGIN", map[string]string{
		"transaction": "tx-2",
	}, ""))
	if err != nil {
		t.Fatalf("Write BEGIN tx-2 failed: %v", err)
	}

	// Test ABORT without receipt (no response expected)
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("ABORT", map[string]string{
		"transaction": "tx-2",
	}, ""))
	if err != nil {
		t.Fatalf("Write ABORT failed: %v", err)
	}

	// Send DISCONNECT with receipt to verify connection is still working
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("DISCONNECT", map[string]string{
		"receipt": "final-receipt",
	}, ""))
	if err != nil {
		t.Fatalf("Write DISCONNECT failed: %v", err)
	}

	// Read RECEIPT for DISCONNECT
	_, receiptData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read RECEIPT failed: %v", err)
	}

	if !bytes.HasPrefix(receiptData, []byte("RECEIPT")) {
		t.Errorf("Expected RECEIPT frame, got: %s", string(receiptData))
	}
}

func TestSTOMPHandler_ParseFrame_ContentLength(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	// Frame with content-length header and binary body containing NULL byte
	body := "Hello\x00World"
	frameData := []byte("SEND\ndestination:/queue/test\ncontent-length:11\n\n" + body + "\x00")

	frame, err := handler.parseFrame(frameData)
	if err != nil {
		t.Fatalf("parseFrame failed: %v", err)
	}

	if frame.Command != "SEND" {
		t.Errorf("Command = %q, want SEND", frame.Command)
	}

	if string(frame.Body) != body {
		t.Errorf("Body = %q, want %q", string(frame.Body), body)
	}
}

func TestSTOMPHandler_ParseFrame_CRLFLineEndings(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	// Test CRLF line endings (STOMP 1.2 allows optional CR before LF)
	frameData := []byte("CONNECT\r\naccept-version:1.2\r\nhost:localhost\r\n\r\n\x00")

	frame, err := handler.parseFrame(frameData)
	if err != nil {
		t.Fatalf("parseFrame failed: %v", err)
	}

	if frame.Command != "CONNECT" {
		t.Errorf("Command = %q, want CONNECT", frame.Command)
	}

	if frame.Headers["accept-version"] != "1.2" {
		t.Errorf("accept-version = %q, want 1.2", frame.Headers["accept-version"])
	}
}

func TestSTOMPHandler_ParseFrame_TrailingEOLs(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	// Test optional trailing EOLs after NULL byte
	frameData := []byte("DISCONNECT\nreceipt:r-1\n\n\x00\n\n\n")

	frame, err := handler.parseFrame(frameData)
	if err != nil {
		t.Fatalf("parseFrame failed: %v", err)
	}

	if frame.Command != "DISCONNECT" {
		t.Errorf("Command = %q, want DISCONNECT", frame.Command)
	}
}

func TestSTOMPHandler_SubscribeWithoutAckMode(t *testing.T) {
	// Test SUBSCRIBE without explicit ack mode (should default to auto)
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// Connect
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}

	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	// Subscribe without ack mode
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("SUBSCRIBE", map[string]string{
		"id":          "sub-1",
		"destination": "/queue/test",
		"receipt":     "sub-receipt",
	}, ""))
	if err != nil {
		t.Fatalf("Write SUBSCRIBE failed: %v", err)
	}

	// Read RECEIPT
	_, receiptData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read RECEIPT failed: %v", err)
	}

	if !bytes.HasPrefix(receiptData, []byte("RECEIPT")) {
		t.Errorf("Expected RECEIPT frame, got: %s", string(receiptData))
	}
}

func TestSTOMPHandler_SubscribeInvalidAckMode(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// Connect
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}

	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	// Subscribe with invalid ack mode
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("SUBSCRIBE", map[string]string{
		"id":          "sub-1",
		"destination": "/queue/test",
		"ack":         "invalid-mode",
	}, ""))
	if err != nil {
		t.Fatalf("Write SUBSCRIBE failed: %v", err)
	}

	// Read ERROR frame
	_, errorData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read ERROR failed: %v", err)
	}

	if !bytes.HasPrefix(errorData, []byte("ERROR")) {
		t.Errorf("Expected ERROR frame, got: %s", string(errorData))
	}

	if !bytes.Contains(errorData, []byte("invalid ack mode")) {
		t.Error("Error should mention invalid ack mode")
	}
}

func TestSTOMPHandler_UnsubscribeNonexistent(t *testing.T) {
	// Test UNSUBSCRIBE for a subscription that doesn't exist
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// Connect
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}

	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	// Unsubscribe from nonexistent subscription with receipt
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("UNSUBSCRIBE", map[string]string{
		"id":      "nonexistent-sub",
		"receipt": "unsub-receipt",
	}, ""))
	if err != nil {
		t.Fatalf("Write UNSUBSCRIBE failed: %v", err)
	}

	// Should still get receipt (subscription just doesn't exist)
	_, receiptData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read RECEIPT failed: %v", err)
	}

	if !bytes.HasPrefix(receiptData, []byte("RECEIPT")) {
		t.Errorf("Expected RECEIPT frame, got: %s", string(receiptData))
	}
}

func TestSTOMPHandler_TransactionErrors(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// Connect
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}

	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	// Try to COMMIT a non-existent transaction
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("COMMIT", map[string]string{
		"transaction": "nonexistent-tx",
	}, ""))
	if err != nil {
		t.Fatalf("Write COMMIT failed: %v", err)
	}

	// Read ERROR frame
	_, errorData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read ERROR failed: %v", err)
	}

	if !bytes.HasPrefix(errorData, []byte("ERROR")) {
		t.Errorf("Expected ERROR frame, got: %s", string(errorData))
	}

	if !bytes.Contains(errorData, []byte("transaction")) {
		t.Error("Error should mention transaction")
	}
}

func TestSTOMPHandler_DuplicateTransaction(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// Connect
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}

	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	// Begin first transaction
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("BEGIN", map[string]string{
		"transaction": "tx-dup",
	}, ""))
	if err != nil {
		t.Fatalf("Write BEGIN failed: %v", err)
	}

	// Try to begin duplicate transaction
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("BEGIN", map[string]string{
		"transaction": "tx-dup",
	}, ""))
	if err != nil {
		t.Fatalf("Write duplicate BEGIN failed: %v", err)
	}

	// Read ERROR frame
	_, errorData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read ERROR failed: %v", err)
	}

	if !bytes.HasPrefix(errorData, []byte("ERROR")) {
		t.Errorf("Expected ERROR frame, got: %s", string(errorData))
	}

	if !bytes.Contains(errorData, []byte("already exists")) {
		t.Error("Error should mention transaction already exists")
	}
}

func TestSTOMPHandler_SendInNonExistentTransaction(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		session := ws.NewSession("test-session", "stomp", conn)

		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// Connect
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}

	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	// Try to send in non-existent transaction
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("SEND", map[string]string{
		"destination": "/queue/test",
		"transaction": "nonexistent-tx",
	}, "Test message"))
	if err != nil {
		t.Fatalf("Write SEND failed: %v", err)
	}

	// Read ERROR frame
	_, errorData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read ERROR failed: %v", err)
	}

	if !bytes.HasPrefix(errorData, []byte("ERROR")) {
		t.Errorf("Expected ERROR frame, got: %s", string(errorData))
	}

	if !bytes.Contains(errorData, []byte("transaction")) {
		t.Error("Error should mention transaction")
	}
}

func TestSTOMPHandler_HeaderValueNoTrimming(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	// STOMP 1.2 spec: MUST NOT trim or pad headers with spaces
	// Header values with leading/trailing spaces should be preserved
	frameData := []byte("SEND\ndestination:  /queue/test  \n\n\x00")

	frame, err := handler.parseFrame(frameData)
	if err != nil {
		t.Fatalf("parseFrame failed: %v", err)
	}

	// Spaces should be preserved
	expected := "  /queue/test  "
	if frame.Headers["destination"] != expected {
		t.Errorf("destination = %q, want %q (spaces should be preserved)", frame.Headers["destination"], expected)
	}
}

// =============================================================================
// Consolidated from stomp_acknack_coverage_test.go
// =============================================================================

// TestSTOMPHandler_AckWithinTransaction verifies that an ACK command with a
// transaction header queues the acknowledgment into the transaction's acks list
// rather than processing it immediately. This covers the transaction branch in
// handleAck that was previously untested.
func TestSTOMPHandler_AckWithinTransaction(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		session := ws.NewSession("test-ack-tx", "stomp", conn)
		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// CONNECT
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}
	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	// BEGIN transaction
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("BEGIN", map[string]string{
		"transaction": "tx-ack-1",
		"receipt":     "begin-r",
	}, ""))
	if err != nil {
		t.Fatalf("Write BEGIN failed: %v", err)
	}
	_, beginReceipt, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read BEGIN RECEIPT failed: %v", err)
	}
	if !bytes.HasPrefix(beginReceipt, []byte("RECEIPT")) {
		t.Errorf("expected RECEIPT for BEGIN, got: %s", string(beginReceipt))
	}

	// ACK within the transaction
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("ACK", map[string]string{
		"id":          "msg-100",
		"transaction": "tx-ack-1",
		"receipt":     "ack-tx-r",
	}, ""))
	if err != nil {
		t.Fatalf("Write ACK with transaction failed: %v", err)
	}
	_, ackReceipt, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read ACK RECEIPT failed: %v", err)
	}
	if !bytes.HasPrefix(ackReceipt, []byte("RECEIPT")) {
		t.Errorf("expected RECEIPT for ACK, got: %s", string(ackReceipt))
	}

	// COMMIT the transaction to finalize
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("COMMIT", map[string]string{
		"transaction": "tx-ack-1",
		"receipt":     "commit-r",
	}, ""))
	if err != nil {
		t.Fatalf("Write COMMIT failed: %v", err)
	}
	_, commitReceipt, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read COMMIT RECEIPT failed: %v", err)
	}
	if !bytes.HasPrefix(commitReceipt, []byte("RECEIPT")) {
		t.Errorf("expected RECEIPT for COMMIT, got: %s", string(commitReceipt))
	}
}

// TestSTOMPHandler_NackWithinTransaction verifies that a NACK command with a
// transaction header queues the negative acknowledgment into the transaction's
// nacks list. This covers the transaction branch in handleNack.
func TestSTOMPHandler_NackWithinTransaction(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		session := ws.NewSession("test-nack-tx", "stomp", conn)
		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

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

	// CONNECT
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}
	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	// BEGIN transaction
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("BEGIN", map[string]string{
		"transaction": "tx-nack-1",
		"receipt":     "begin-r",
	}, ""))
	if err != nil {
		t.Fatalf("Write BEGIN failed: %v", err)
	}
	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read BEGIN RECEIPT failed: %v", err)
	}

	// NACK within the transaction
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("NACK", map[string]string{
		"id":          "msg-200",
		"transaction": "tx-nack-1",
		"receipt":     "nack-tx-r",
	}, ""))
	if err != nil {
		t.Fatalf("Write NACK with transaction failed: %v", err)
	}
	_, nackReceipt, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read NACK RECEIPT failed: %v", err)
	}
	if !bytes.HasPrefix(nackReceipt, []byte("RECEIPT")) {
		t.Errorf("expected RECEIPT for NACK, got: %s", string(nackReceipt))
	}

	// COMMIT the transaction
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("COMMIT", map[string]string{
		"transaction": "tx-nack-1",
		"receipt":     "commit-r",
	}, ""))
	if err != nil {
		t.Fatalf("Write COMMIT failed: %v", err)
	}
	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read COMMIT RECEIPT failed: %v", err)
	}
}

// TestSTOMPHandler_AckInvalidTransaction verifies that an ACK command
// referencing a non-existent transaction results in the server sending an
// ERROR frame and closing the connection.
func TestSTOMPHandler_AckInvalidTransaction(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		session := ws.NewSession("test-ack-invalid-tx", "stomp", conn)
		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	// CONNECT
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}
	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	// ACK with non-existent transaction
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("ACK", map[string]string{
		"id":          "msg-300",
		"transaction": "nonexistent-tx",
	}, ""))
	if err != nil {
		t.Fatalf("Write ACK with invalid transaction failed: %v", err)
	}

	// Server sends ERROR frame then closes -- read may get ERROR or close error
	_, errData, readErr := conn.Read(ctx)
	if readErr != nil {
		// Connection closed by server after error -- this is expected behavior
		return
	}
	if !bytes.HasPrefix(errData, []byte("ERROR")) {
		t.Errorf("expected ERROR frame for invalid transaction, got: %s", string(errData))
	}
}

// TestSTOMPHandler_NackInvalidTransaction verifies that a NACK command
// referencing a non-existent transaction results in the server sending an
// ERROR frame and closing the connection.
func TestSTOMPHandler_NackInvalidTransaction(t *testing.T) {
	handler := NewSTOMPHandler(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		session := ws.NewSession("test-nack-invalid-tx", "stomp", conn)
		_ = handler.HandleConnection(session.Context(), conn, session)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	// CONNECT
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("CONNECT", map[string]string{
		"accept-version": "1.2",
		"host":           "localhost",
	}, ""))
	if err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}
	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read CONNECTED failed: %v", err)
	}

	// NACK with non-existent transaction
	err = conn.Write(ctx, websocket.MessageText, buildSTOMPFrame("NACK", map[string]string{
		"id":          "msg-400",
		"transaction": "nonexistent-tx",
	}, ""))
	if err != nil {
		t.Fatalf("Write NACK with invalid transaction failed: %v", err)
	}

	// Server sends ERROR frame then closes -- read may get ERROR or close error
	_, errData, readErr := conn.Read(ctx)
	if readErr != nil {
		// Connection closed by server after error -- this is expected behavior
		return
	}
	if !bytes.HasPrefix(errData, []byte("ERROR")) {
		t.Errorf("expected ERROR frame for invalid transaction, got: %s", string(errData))
	}
}
