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

package websocket

import (
	"context"
	"crypto/fips140"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// testLogger creates a logger for testing
func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "console",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	return log
}

// mockSubprotocolHandler is a test implementation of SubprotocolHandler
type mockSubprotocolHandler struct {
	name         string
	handleCalled bool
	mu           sync.Mutex
}

func (h *mockSubprotocolHandler) Name() string {
	return h.name
}

func (h *mockSubprotocolHandler) HandleConnection(ctx context.Context, conn *websocket.Conn, session *Session) error {
	h.mu.Lock()
	h.handleCalled = true
	h.mu.Unlock()

	// Wait for context cancellation
	<-ctx.Done()
	return ctx.Err()
}

func (h *mockSubprotocolHandler) WasCalled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.handleCalled
}

// mockAuthenticator implements Authenticator for testing
type mockAuthenticator struct {
	shouldFail bool
	userID     string
}

func (a *mockAuthenticator) Authenticate(ctx context.Context, token string) (string, error) {
	if a.shouldFail {
		return "", &AuthenticationFailedError{Reason: "test failure"}
	}
	if a.userID != "" {
		return a.userID, nil
	}
	return "test-user", nil
}

// assertFIPSOnlyRefusal checks the typed refusal NewServer returns while the
// process enforces GODEBUG=fips140=only.
func assertFIPSOnlyRefusal(t *testing.T, server *Server, err error) {
	t.Helper()
	if server != nil {
		t.Error("NewServer must not return a server in FIPS 140-only mode")
	}
	if !IsWebSocketUnavailableInFIPSOnlyMode(err) {
		t.Errorf("want WebSocketUnavailableInFIPSOnlyModeError, got %v", err)
	}
	var qbErr *errors.QBError
	if !stderrors.As(err, &qbErr) || qbErr.Code != ErrCodeUnavailableInFIPSOnlyMode {
		t.Errorf("want QBError code %d, got %v", ErrCodeUnavailableInFIPSOnlyMode, err)
	}
}

// handshakeServer builds a server for tests that perform the RFC 6455
// handshake. Under fips140=only the handshake cannot run (SHA-1), so it asserts
// the refusal and returns nil; the caller returns.
func handshakeServer(t *testing.T, config ServerConfig, auth Authenticator, log *logger.Logger) *Server {
	t.Helper()
	server, err := NewServer(config, auth, log)
	if fips140.Enforced() {
		assertFIPSOnlyRefusal(t, server, err)
		return nil
	}
	if err != nil {
		t.Fatalf("NewServer returned error: %v", err)
	}
	return server
}

// TestNewServer_FIPSOnlyGate proves the gate follows crypto/fips140: the
// refusal under only-mode, a usable server otherwise.
func TestNewServer_FIPSOnlyGate(t *testing.T) {
	server, err := NewServer(DefaultServerConfig(), nil, testLogger(t))
	if fips140.Enforced() {
		assertFIPSOnlyRefusal(t, server, err)
		return
	}
	if err != nil || server == nil {
		t.Fatalf("NewServer = (%v, %v), want a server and no error", server, err)
	}
	if IsWebSocketUnavailableInFIPSOnlyMode(err) {
		t.Error("nil error must not match the FIPS-only refusal")
	}
}

func TestNewServer(t *testing.T) {
	log := testLogger(t)

	tests := []struct {
		name   string
		config ServerConfig
		auth   Authenticator
	}{
		{
			name:   "with default config",
			config: DefaultServerConfig(),
			auth:   nil,
		},
		{
			name: "with custom config",
			config: ServerConfig{
				ReadTimeout:    60 * time.Second,
				WriteTimeout:   30 * time.Second,
				PingInterval:   55 * time.Second,
				MaxMessageSize: 2 * 1024 * 1024,
			},
			auth: nil,
		},
		{
			name:   "with custom authenticator",
			config: DefaultServerConfig(),
			auth:   &mockAuthenticator{userID: "custom-user"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, err := NewServer(tt.config, tt.auth, log)
			if fips140.Enforced() {
				assertFIPSOnlyRefusal(t, server, err)
				return
			}
			if err != nil {
				t.Fatalf("NewServer returned error: %v", err)
			}

			if server == nil {
				t.Fatal("NewServer returned nil")
			}

			if server.sessions == nil {
				t.Error("Sessions manager should be initialized")
			}

			if server.subprotocols == nil {
				t.Error("Subprotocols map should be initialized")
			}

			if server.authenticator == nil {
				t.Error("Authenticator should be initialized (default if nil)")
			}

			if server.IsClosed() {
				t.Error("New server should not be closed")
			}
		})
	}
}

func TestNewServer_NilAuth(t *testing.T) {
	log := testLogger(t)
	server := newServer(DefaultServerConfig(), nil, log)

	// Should use NoopAuthenticator
	if server.authenticator == nil {
		t.Fatal("Authenticator should not be nil")
	}

	_, ok := server.authenticator.(*NoopAuthenticator)
	if !ok {
		t.Error("Default authenticator should be NoopAuthenticator")
	}
}

func TestServer_RegisterSubprotocol(t *testing.T) {
	log := testLogger(t)
	server := newServer(DefaultServerConfig(), nil, log)

	handler := &mockSubprotocolHandler{name: "echo"}
	server.RegisterSubprotocol(handler)

	protocols := server.GetSubprotocols()
	if len(protocols) != 1 {
		t.Errorf("GetSubprotocols() returned %d protocols, want 1", len(protocols))
	}

	if protocols[0] != "echo" {
		t.Errorf("Protocol = %q, want echo", protocols[0])
	}
}

func TestServer_RegisterMultipleSubprotocols(t *testing.T) {
	log := testLogger(t)
	server := newServer(DefaultServerConfig(), nil, log)

	server.RegisterSubprotocol(&mockSubprotocolHandler{name: "echo"})
	server.RegisterSubprotocol(&mockSubprotocolHandler{name: "mqtt"})
	server.RegisterSubprotocol(&mockSubprotocolHandler{name: "stomp"})

	protocols := server.GetSubprotocols()
	if len(protocols) != 3 {
		t.Errorf("GetSubprotocols() returned %d protocols, want 3", len(protocols))
	}

	// Sort for deterministic comparison
	sort.Strings(protocols)
	expected := []string{"echo", "mqtt", "stomp"}

	for i, exp := range expected {
		if protocols[i] != exp {
			t.Errorf("Protocol[%d] = %q, want %q", i, protocols[i], exp)
		}
	}
}

func TestServer_RegisterDuplicateSubprotocol(t *testing.T) {
	log := testLogger(t)
	server := newServer(DefaultServerConfig(), nil, log)

	handler1 := &mockSubprotocolHandler{name: "echo"}
	handler2 := &mockSubprotocolHandler{name: "echo"}

	server.RegisterSubprotocol(handler1)
	server.RegisterSubprotocol(handler2)

	protocols := server.GetSubprotocols()
	if len(protocols) != 1 {
		t.Errorf("GetSubprotocols() returned %d protocols, want 1", len(protocols))
	}
}

func TestServer_GetSubprotocols(t *testing.T) {
	log := testLogger(t)
	server := newServer(DefaultServerConfig(), nil, log)

	// Empty initially
	protocols := server.GetSubprotocols()
	if len(protocols) != 0 {
		t.Errorf("GetSubprotocols() returned %d protocols, want 0", len(protocols))
	}

	// After registration
	server.RegisterSubprotocol(&mockSubprotocolHandler{name: "test"})
	protocols = server.GetSubprotocols()
	if len(protocols) != 1 {
		t.Errorf("GetSubprotocols() returned %d protocols, want 1", len(protocols))
	}
}

func TestServer_Close(t *testing.T) {
	log := testLogger(t)
	server := newServer(DefaultServerConfig(), nil, log)

	if server.IsClosed() {
		t.Error("New server should not be closed")
	}

	err := server.Close()
	if err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}

	if !server.IsClosed() {
		t.Error("Server should be closed after Close()")
	}
}

func TestServer_CloseIdempotent(t *testing.T) {
	log := testLogger(t)
	server := newServer(DefaultServerConfig(), nil, log)

	err := server.Close()
	if err != nil {
		t.Errorf("First Close() error = %v, want nil", err)
	}

	err = server.Close()
	if err != nil {
		t.Errorf("Second Close() error = %v, want nil", err)
	}

	if !server.IsClosed() {
		t.Error("Server should remain closed")
	}
}

func TestServer_CloseClosesSessions(t *testing.T) {
	log := testLogger(t)
	server := handshakeServer(t, DefaultServerConfig(), nil, log)
	if server == nil {
		return
	}

	// Create test server to get real WebSocket connections
	ts := httptest.NewServer(http.HandlerFunc(server.HandleHTTP))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	// Create multiple WebSocket connections
	var connections []*websocket.Conn
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		conn, resp, err := websocket.Dial(ctx, wsURL, nil)
		cancel()
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		if err != nil {
			t.Fatalf("WebSocket dial %d failed: %v", i, err)
		}
		connections = append(connections, conn)
	}

	// Wait for sessions to be created
	time.Sleep(50 * time.Millisecond)

	if server.GetSessionCount() != 3 {
		t.Errorf("Session count = %d, want 3", server.GetSessionCount())
	}

	// Close server (which should close all sessions)
	server.Close()

	// Verify session count is 0
	if server.GetSessionCount() != 0 {
		t.Errorf("Session count after close = %d, want 0", server.GetSessionCount())
	}

	// Clean up connections
	for _, conn := range connections {
		conn.Close(websocket.StatusNormalClosure, "test cleanup")
	}
}

func TestServer_IsClosed(t *testing.T) {
	log := testLogger(t)
	server := newServer(DefaultServerConfig(), nil, log)

	if server.IsClosed() {
		t.Error("New server should not be closed")
	}

	server.closed.Store(true)

	if !server.IsClosed() {
		t.Error("Server should be closed when closed flag is true")
	}
}

func TestServer_GetSession(t *testing.T) {
	log := testLogger(t)
	server := newServer(DefaultServerConfig(), nil, log)

	// Get nonexistent session
	_, exists := server.GetSession("nonexistent")
	if exists {
		t.Error("GetSession should return false for nonexistent session")
	}

	// Add session and get it
	session := NewSession("test-session", "echo", nil)
	server.sessions.Add(session)

	retrieved, exists := server.GetSession("test-session")
	if !exists {
		t.Error("GetSession should return true for existing session")
	}

	if retrieved.ID != "test-session" {
		t.Errorf("Session ID = %q, want test-session", retrieved.ID)
	}
}

func TestServer_GetSessionCount(t *testing.T) {
	log := testLogger(t)
	server := newServer(DefaultServerConfig(), nil, log)

	if server.GetSessionCount() != 0 {
		t.Errorf("Initial session count = %d, want 0", server.GetSessionCount())
	}

	for i := 0; i < 10; i++ {
		server.sessions.Add(NewSession("session-"+string(rune('0'+i)), "echo", nil))
	}

	if server.GetSessionCount() != 10 {
		t.Errorf("Session count = %d, want 10", server.GetSessionCount())
	}
}

func TestServer_GetSessionStats(t *testing.T) {
	log := testLogger(t)
	server := newServer(DefaultServerConfig(), nil, log)

	// Add sessions with different protocols
	for i := 0; i < 3; i++ {
		server.sessions.Add(NewSession("echo-"+string(rune('0'+i)), "echo", nil))
	}

	for i := 0; i < 2; i++ {
		server.sessions.Add(NewSession("mqtt-"+string(rune('0'+i)), "mqtt", nil))
	}

	stats := server.GetSessionStats()

	if stats.TotalSessions != 5 {
		t.Errorf("TotalSessions = %d, want 5", stats.TotalSessions)
	}

	if stats.SessionsByProtocol["echo"] != 3 {
		t.Errorf("SessionsByProtocol[echo] = %d, want 3", stats.SessionsByProtocol["echo"])
	}

	if stats.SessionsByProtocol["mqtt"] != 2 {
		t.Errorf("SessionsByProtocol[mqtt] = %d, want 2", stats.SessionsByProtocol["mqtt"])
	}
}

func TestParseSubprotocols(t *testing.T) {
	tests := []struct {
		name     string
		headers  http.Header
		expected []string
	}{
		{
			name:     "empty headers",
			headers:  http.Header{},
			expected: nil,
		},
		{
			name: "single protocol",
			headers: http.Header{
				"Sec-Websocket-Protocol": []string{"echo"},
			},
			expected: []string{"echo"},
		},
		{
			name: "multiple protocols comma separated",
			headers: http.Header{
				"Sec-Websocket-Protocol": []string{"echo, mqtt, stomp"},
			},
			expected: []string{"echo", "mqtt", "stomp"},
		},
		{
			name: "multiple header lines",
			headers: http.Header{
				"Sec-Websocket-Protocol": []string{"echo", "mqtt"},
			},
			expected: []string{"echo", "mqtt"},
		},
		{
			name: "with whitespace",
			headers: http.Header{
				"Sec-Websocket-Protocol": []string{"  echo  ,  mqtt  "},
			},
			expected: []string{"echo", "mqtt"},
		},
		{
			name: "empty values",
			headers: http.Header{
				"Sec-Websocket-Protocol": []string{"echo,, mqtt"},
			},
			expected: []string{"echo", "mqtt"},
		},
		{
			name:     "mixed case header name via Set",
			headers:  makeHeaderWithSet("SEC-WEBSOCKET-PROTOCOL", "echo"),
			expected: []string{"echo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSubprotocols(tt.headers)

			if len(result) != len(tt.expected) {
				t.Errorf("parseSubprotocols() returned %d protocols, want %d", len(result), len(tt.expected))
				return
			}

			for i, exp := range tt.expected {
				if result[i] != exp {
					t.Errorf("parseSubprotocols()[%d] = %q, want %q", i, result[i], exp)
				}
			}
		})
	}
}

// makeHeaderWithSet creates an http.Header using Set() which canonicalizes keys
func makeHeaderWithSet(key, value string) http.Header {
	h := make(http.Header)
	h.Set(key, value)
	return h
}

func TestServer_SelectSubprotocol(t *testing.T) {
	log := testLogger(t)
	server := newServer(DefaultServerConfig(), nil, log)

	server.RegisterSubprotocol(&mockSubprotocolHandler{name: "echo"})
	server.RegisterSubprotocol(&mockSubprotocolHandler{name: "mqtt"})

	tests := []struct {
		name      string
		requested []string
		expected  string
	}{
		{
			name:      "empty requested",
			requested: nil,
			expected:  "",
		},
		{
			name:      "single match",
			requested: []string{"echo"},
			expected:  "echo",
		},
		{
			name:      "first match wins",
			requested: []string{"echo", "mqtt"},
			expected:  "echo",
		},
		{
			name:      "second matches",
			requested: []string{"stomp", "mqtt"},
			expected:  "mqtt",
		},
		{
			name:      "no match",
			requested: []string{"stomp", "amqp"},
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := server.selectSubprotocol(tt.requested)
			if result != tt.expected {
				t.Errorf("selectSubprotocol(%v) = %q, want %q", tt.requested, result, tt.expected)
			}
		})
	}
}

func TestServer_SelectSubprotocol_Empty(t *testing.T) {
	log := testLogger(t)
	server := newServer(DefaultServerConfig(), nil, log)

	// No registered subprotocols
	result := server.selectSubprotocol([]string{"echo", "mqtt"})
	if result != "" {
		t.Errorf("selectSubprotocol() = %q, want empty string", result)
	}
}

func TestServer_HandleHTTP_ClosedServer(t *testing.T) {
	log := testLogger(t)
	server := newServer(DefaultServerConfig(), nil, log)
	server.Close()

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")

	rec := httptest.NewRecorder()

	server.HandleHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestServer_HandleHTTP_UnsupportedSubprotocol(t *testing.T) {
	log := testLogger(t)
	server := newServer(DefaultServerConfig(), nil, log)

	// Register only echo
	server.RegisterSubprotocol(&mockSubprotocolHandler{name: "echo"})

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Protocol", "mqtt, stomp")

	rec := httptest.NewRecorder()

	server.HandleHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	if !strings.Contains(rec.Body.String(), "no supported subprotocol") {
		t.Errorf("Body = %q, want to contain 'no supported subprotocol'", rec.Body.String())
	}
}

func TestServer_HandleHTTP_WithSupportedSubprotocol(t *testing.T) {
	log := testLogger(t)
	config := DefaultServerConfig()
	server := handshakeServer(t, config, nil, log)
	if server == nil {
		return
	}

	handler := &mockSubprotocolHandler{name: "echo"}
	server.RegisterSubprotocol(handler)

	// Create test server
	ts := httptest.NewServer(http.HandlerFunc(server.HandleHTTP))
	defer ts.Close()

	// Connect with WebSocket
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"echo"},
	})
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	// Wait briefly for connection to be established
	time.Sleep(50 * time.Millisecond)

	// Verify session was created
	if server.GetSessionCount() == 0 {
		t.Error("Session should be created after connection")
	}

	// Verify handler was called
	time.Sleep(50 * time.Millisecond)
	if !handler.WasCalled() {
		t.Error("Subprotocol handler should be called")
	}
}

func TestServer_HandleHTTP_NoSubprotocol(t *testing.T) {
	log := testLogger(t)
	server := handshakeServer(t, DefaultServerConfig(), nil, log)
	if server == nil {
		return
	}

	// Create test server
	ts := httptest.NewServer(http.HandlerFunc(server.HandleHTTP))
	defer ts.Close()

	// Connect without specifying subprotocol
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	// Wait briefly for connection to be established
	time.Sleep(50 * time.Millisecond)

	// Verify session was created with default protocol
	if server.GetSessionCount() == 0 {
		t.Error("Session should be created after connection")
	}

	// Session should have "quicbaas" as default subprotocol
	var foundSession *Session
	server.sessions.ForEach(func(s *Session) bool {
		foundSession = s
		return false
	})

	if foundSession == nil {
		t.Fatal("No session found")
	}

	if foundSession.Subprotocol != "quicbaas" {
		t.Errorf("Session subprotocol = %q, want quicbaas", foundSession.Subprotocol)
	}
}

func TestServer_HandleHTTP_EchoMessages(t *testing.T) {
	log := testLogger(t)
	server := handshakeServer(t, DefaultServerConfig(), nil, log)
	if server == nil {
		return
	}

	// Create test server
	ts := httptest.NewServer(http.HandlerFunc(server.HandleHTTP))
	defer ts.Close()

	// Connect
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

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

	// Send a message
	testMessage := []byte("Hello, WebSocket!")
	err = conn.Write(ctx, websocket.MessageText, testMessage)
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

	if string(data) != string(testMessage) {
		t.Errorf("Echo = %q, want %q", string(data), string(testMessage))
	}
}

func TestServer_HandleHTTP_MultipleMessages(t *testing.T) {
	log := testLogger(t)
	server := handshakeServer(t, DefaultServerConfig(), nil, log)
	if server == nil {
		return
	}

	ts := httptest.NewServer(http.HandlerFunc(server.HandleHTTP))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

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

	messages := []string{"message1", "message2", "message3"}

	for _, msg := range messages {
		err = conn.Write(ctx, websocket.MessageText, []byte(msg))
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}

		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}

		if string(data) != msg {
			t.Errorf("Echo = %q, want %q", string(data), msg)
		}
	}
}

func TestServer_HandleHTTP_BinaryMessages(t *testing.T) {
	log := testLogger(t)
	server := handshakeServer(t, DefaultServerConfig(), nil, log)
	if server == nil {
		return
	}

	ts := httptest.NewServer(http.HandlerFunc(server.HandleHTTP))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

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
	binaryData := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD}
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

func TestServer_ConcurrentConnections(t *testing.T) {
	log := testLogger(t)
	server := handshakeServer(t, DefaultServerConfig(), nil, log)
	if server == nil {
		return
	}

	ts := httptest.NewServer(http.HandlerFunc(server.HandleHTTP))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	var wg sync.WaitGroup
	numConnections := 10

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

			// Send and receive a message
			msg := []byte("test message from " + string(rune('0'+idx)))
			err = conn.Write(ctx, websocket.MessageText, msg)
			if err != nil {
				t.Errorf("Connection %d write failed: %v", idx, err)
				return
			}

			_, data, err := conn.Read(ctx)
			if err != nil {
				t.Errorf("Connection %d read failed: %v", idx, err)
				return
			}

			if string(data) != string(msg) {
				t.Errorf("Connection %d echo mismatch: got %q, want %q", idx, string(data), string(msg))
			}
		}(i)
	}

	wg.Wait()
}

func TestServer_SessionCleanupOnDisconnect(t *testing.T) {
	log := testLogger(t)
	server := handshakeServer(t, DefaultServerConfig(), nil, log)
	if server == nil {
		return
	}

	ts := httptest.NewServer(http.HandlerFunc(server.HandleHTTP))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}

	// Wait for session to be created
	time.Sleep(50 * time.Millisecond)

	initialCount := server.GetSessionCount()
	if initialCount != 1 {
		t.Errorf("Initial session count = %d, want 1", initialCount)
	}

	// Close the connection
	conn.Close(websocket.StatusNormalClosure, "test complete")

	// Wait for cleanup
	time.Sleep(100 * time.Millisecond)

	finalCount := server.GetSessionCount()
	if finalCount != 0 {
		t.Errorf("Final session count = %d, want 0", finalCount)
	}
}

func TestServer_RegisterSubprotocolConcurrent(t *testing.T) {
	log := testLogger(t)
	server := newServer(DefaultServerConfig(), nil, log)

	var wg sync.WaitGroup

	// Concurrent registration
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			handler := &mockSubprotocolHandler{name: "protocol-" + string(rune('0'+idx))}
			server.RegisterSubprotocol(handler)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = server.GetSubprotocols()
		}()
	}

	wg.Wait()

	protocols := server.GetSubprotocols()
	if len(protocols) != 10 {
		t.Errorf("Registered %d protocols, want 10", len(protocols))
	}
}
