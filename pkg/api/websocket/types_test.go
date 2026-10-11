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
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestNewSession(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		subprotocol string
		wantID      string
		wantProto   string
	}{
		{
			name:        "basic session creation",
			id:          "session-123",
			subprotocol: "echo",
			wantID:      "session-123",
			wantProto:   "echo",
		},
		{
			name:        "empty subprotocol",
			id:          "session-456",
			subprotocol: "",
			wantID:      "session-456",
			wantProto:   "",
		},
		{
			name:        "empty session ID",
			id:          "",
			subprotocol: "mqtt",
			wantID:      "",
			wantProto:   "mqtt",
		},
		{
			name:        "special characters in ID",
			id:          "session-123-abc-!@#",
			subprotocol: "stomp",
			wantID:      "session-123-abc-!@#",
			wantProto:   "stomp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beforeCreate := time.Now()
			session := NewSession(tt.id, tt.subprotocol, nil)
			afterCreate := time.Now()

			if session.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", session.ID, tt.wantID)
			}

			if session.Subprotocol != tt.wantProto {
				t.Errorf("Subprotocol = %q, want %q", session.Subprotocol, tt.wantProto)
			}

			// Verify CreatedAt is within expected range
			if session.CreatedAt.Before(beforeCreate) || session.CreatedAt.After(afterCreate) {
				t.Errorf("CreatedAt = %v, expected between %v and %v",
					session.CreatedAt, beforeCreate, afterCreate)
			}

			// Verify LastActivity is initialized
			lastActivity := session.GetLastActivity()
			if lastActivity.Before(beforeCreate) || lastActivity.After(afterCreate) {
				t.Errorf("LastActivity = %v, expected between %v and %v",
					lastActivity, beforeCreate, afterCreate)
			}

			// Verify maps are initialized
			if session.Metadata == nil {
				t.Error("Metadata map is nil")
			}

			if session.Subscriptions == nil {
				t.Error("Subscriptions map is nil")
			}

			// Verify context is created
			if session.Context() == nil {
				t.Error("Context is nil")
			}

			// Verify session is not closed
			if session.IsClosed() {
				t.Error("Newly created session should not be closed")
			}
		})
	}
}

func TestSession_Context(t *testing.T) {
	session := NewSession("test-session", "echo", nil)

	ctx := session.Context()
	if ctx == nil {
		t.Fatal("Context should not be nil")
	}

	// Context should not be done initially
	select {
	case <-ctx.Done():
		t.Error("Context should not be done for active session")
	default:
		// Expected
	}

	// Manually cancel context (simulating what Close() does internally)
	// Note: Close() cannot be called on sessions with nil connections.
	// This tests the context cancellation mechanism directly.
	session.cancel()

	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Context should be done after cancel")
	}
}

// TestSession_ContextWithRealConnection exercises Session, not the Server. The
// RFC 6455 handshake it needs uses SHA-1, which fips140=only refuses, so the
// handshake is allowed for this test only.
func TestSession_ContextWithRealConnection(t *testing.T) {
	fips140.WithoutEnforcement(func() { sessionContextWithRealConnection(t) })
}

func sessionContextWithRealConnection(t *testing.T) {
	// Create a simple WebSocket server to test context with real connection
	var session *Session
	sessionReady := make(chan struct{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		session = NewSession("test-session", "echo", conn)
		close(sessionReady)

		// Wait for context cancellation
		<-session.Context().Done()
	})

	ts := httptest.NewServer(handler)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	ctx := context.Background()
	clientConn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer clientConn.Close(websocket.StatusNormalClosure, "test cleanup")

	// Wait for session to be created
	<-sessionReady

	// Verify context is not done initially
	select {
	case <-session.Context().Done():
		t.Error("Context should not be done for active session")
	default:
		// Expected
	}

	// Close session and verify context is cancelled
	session.Close()

	select {
	case <-session.Context().Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Context should be done after session close")
	}

	// Verify session is closed
	if !session.IsClosed() {
		t.Error("Session should be closed")
	}
}

func TestSession_Close(t *testing.T) {
	t.Run("close state management without connection", func(t *testing.T) {
		session := NewSession("test-session", "echo", nil)

		if session.IsClosed() {
			t.Error("Session should not be closed initially")
		}

		// Test the closed state transitions directly
		// Note: Close() cannot be called with nil connection
		if !session.closed.CompareAndSwap(false, true) {
			t.Error("Should be able to set closed to true")
		}

		if !session.IsClosed() {
			t.Error("Session should be closed after CompareAndSwap")
		}

		// Second attempt should fail
		if session.closed.CompareAndSwap(false, true) {
			t.Error("Second CompareAndSwap should fail")
		}
	})

	t.Run("close is idempotent", func(t *testing.T) {
		session := NewSession("test-session", "echo", nil)

		// Mark as closed
		session.closed.Store(true)

		// IsClosed should return true
		if !session.IsClosed() {
			t.Error("Session should be closed")
		}
	})
}

func TestSession_IsClosed(t *testing.T) {
	tests := []struct {
		name       string
		setClosed  bool
		wantClosed bool
	}{
		{
			name:       "newly created session",
			setClosed:  false,
			wantClosed: false,
		},
		{
			name:       "closed session",
			setClosed:  true,
			wantClosed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := NewSession("test-session", "echo", nil)

			if tt.setClosed {
				session.closed.Store(true)
			}

			if got := session.IsClosed(); got != tt.wantClosed {
				t.Errorf("IsClosed() = %v, want %v", got, tt.wantClosed)
			}
		})
	}
}

func TestSession_UpdateActivity(t *testing.T) {
	session := NewSession("test-session", "echo", nil)
	initialActivity := session.GetLastActivity()

	// Wait a bit to ensure time difference
	time.Sleep(10 * time.Millisecond)

	beforeUpdate := time.Now()
	session.UpdateActivity()
	afterUpdate := time.Now()

	newActivity := session.GetLastActivity()

	// New activity should be after initial activity
	if !newActivity.After(initialActivity) {
		t.Error("LastActivity should be updated to a later time")
	}

	// New activity should be within expected range
	if newActivity.Before(beforeUpdate) || newActivity.After(afterUpdate) {
		t.Errorf("LastActivity = %v, expected between %v and %v",
			newActivity, beforeUpdate, afterUpdate)
	}
}

func TestSession_GetLastActivity(t *testing.T) {
	session := NewSession("test-session", "echo", nil)

	activity := session.GetLastActivity()

	// Should return a valid time
	if activity.IsZero() {
		t.Error("GetLastActivity should return non-zero time")
	}

	// Should be recent (within last second)
	if time.Since(activity) > time.Second {
		t.Error("GetLastActivity should return recent time")
	}
}

func TestSession_Metadata(t *testing.T) {
	t.Run("set and get metadata", func(t *testing.T) {
		session := NewSession("test-session", "echo", nil)

		session.SetMetadata("key1", "value1")
		session.SetMetadata("key2", "value2")

		val, ok := session.GetMetadata("key1")
		if !ok || val != "value1" {
			t.Errorf("GetMetadata(key1) = %q, %v; want value1, true", val, ok)
		}

		val, ok = session.GetMetadata("key2")
		if !ok || val != "value2" {
			t.Errorf("GetMetadata(key2) = %q, %v; want value2, true", val, ok)
		}
	})

	t.Run("get nonexistent metadata", func(t *testing.T) {
		session := NewSession("test-session", "echo", nil)

		val, ok := session.GetMetadata("nonexistent")
		if ok || val != "" {
			t.Errorf("GetMetadata(nonexistent) = %q, %v; want \"\", false", val, ok)
		}
	})

	t.Run("overwrite metadata", func(t *testing.T) {
		session := NewSession("test-session", "echo", nil)

		session.SetMetadata("key", "value1")
		session.SetMetadata("key", "value2")

		val, ok := session.GetMetadata("key")
		if !ok || val != "value2" {
			t.Errorf("GetMetadata(key) = %q, %v; want value2, true", val, ok)
		}
	})

	t.Run("empty key and value", func(t *testing.T) {
		session := NewSession("test-session", "echo", nil)

		session.SetMetadata("", "empty-key-value")
		val, ok := session.GetMetadata("")
		if !ok || val != "empty-key-value" {
			t.Errorf("GetMetadata(\"\") = %q, %v; want empty-key-value, true", val, ok)
		}

		session.SetMetadata("empty-value", "")
		val, ok = session.GetMetadata("empty-value")
		if !ok || val != "" {
			t.Errorf("GetMetadata(empty-value) = %q, %v; want \"\", true", val, ok)
		}
	})

	t.Run("concurrent metadata access", func(t *testing.T) {
		session := NewSession("test-session", "echo", nil)
		var wg sync.WaitGroup

		// Concurrent writes
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				key := "key"
				session.SetMetadata(key, "value")
			}(i)
		}

		// Concurrent reads
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				session.GetMetadata("key")
			}()
		}

		wg.Wait()
		// No race conditions should occur
	})
}

func TestSession_Subscriptions(t *testing.T) {
	t.Run("subscribe and check", func(t *testing.T) {
		session := NewSession("test-session", "echo", nil)

		session.Subscribe("channel1")
		session.Subscribe("channel2")

		if !session.IsSubscribed("channel1") {
			t.Error("Should be subscribed to channel1")
		}

		if !session.IsSubscribed("channel2") {
			t.Error("Should be subscribed to channel2")
		}

		if session.IsSubscribed("channel3") {
			t.Error("Should not be subscribed to channel3")
		}
	})

	t.Run("unsubscribe", func(t *testing.T) {
		session := NewSession("test-session", "echo", nil)

		session.Subscribe("channel1")
		session.Subscribe("channel2")

		if !session.IsSubscribed("channel1") {
			t.Error("Should be subscribed to channel1")
		}

		session.Unsubscribe("channel1")

		if session.IsSubscribed("channel1") {
			t.Error("Should not be subscribed to channel1 after unsubscribe")
		}

		if !session.IsSubscribed("channel2") {
			t.Error("Should still be subscribed to channel2")
		}
	})

	t.Run("unsubscribe nonexistent channel", func(t *testing.T) {
		session := NewSession("test-session", "echo", nil)

		// Should not panic
		session.Unsubscribe("nonexistent")

		if session.IsSubscribed("nonexistent") {
			t.Error("Should not be subscribed to nonexistent channel")
		}
	})

	t.Run("get subscriptions", func(t *testing.T) {
		session := NewSession("test-session", "echo", nil)

		session.Subscribe("channel1")
		session.Subscribe("channel2")
		session.Subscribe("channel3")

		subs := session.GetSubscriptions()

		if len(subs) != 3 {
			t.Errorf("GetSubscriptions() returned %d items, want 3", len(subs))
		}

		// Sort for deterministic comparison
		sort.Strings(subs)
		expected := []string{"channel1", "channel2", "channel3"}

		for i, exp := range expected {
			if subs[i] != exp {
				t.Errorf("GetSubscriptions()[%d] = %q, want %q", i, subs[i], exp)
			}
		}
	})

	t.Run("get subscriptions returns copy", func(t *testing.T) {
		session := NewSession("test-session", "echo", nil)

		session.Subscribe("channel1")

		subs := session.GetSubscriptions()
		subs = append(subs, "channel2")

		// Original subscriptions should not be modified
		if session.IsSubscribed("channel2") {
			t.Error("Modifying returned slice should not affect original subscriptions")
		}
	})

	t.Run("empty subscriptions", func(t *testing.T) {
		session := NewSession("test-session", "echo", nil)

		subs := session.GetSubscriptions()

		if len(subs) != 0 {
			t.Errorf("GetSubscriptions() returned %d items, want 0", len(subs))
		}
	})

	t.Run("duplicate subscribe", func(t *testing.T) {
		session := NewSession("test-session", "echo", nil)

		session.Subscribe("channel1")
		session.Subscribe("channel1")

		subs := session.GetSubscriptions()

		if len(subs) != 1 {
			t.Errorf("GetSubscriptions() returned %d items, want 1", len(subs))
		}
	})

	t.Run("concurrent subscription access", func(t *testing.T) {
		session := NewSession("test-session", "echo", nil)
		var wg sync.WaitGroup

		// Concurrent subscribes
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				session.Subscribe("channel")
			}(i)
		}

		// Concurrent unsubscribes
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				session.Unsubscribe("channel")
			}()
		}

		// Concurrent reads
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				session.IsSubscribed("channel")
				session.GetSubscriptions()
			}()
		}

		wg.Wait()
		// No race conditions should occur
	})
}

func TestDefaultServerConfig(t *testing.T) {
	config := DefaultServerConfig()

	if config.ReadTimeout != 30*time.Second {
		t.Errorf("ReadTimeout = %v, want 30s", config.ReadTimeout)
	}

	if config.WriteTimeout != 10*time.Second {
		t.Errorf("WriteTimeout = %v, want 10s", config.WriteTimeout)
	}

	if config.PingInterval != 25*time.Second {
		t.Errorf("PingInterval = %v, want 25s", config.PingInterval)
	}

	if config.MaxMessageSize != 1024*1024 {
		t.Errorf("MaxMessageSize = %d, want %d", config.MaxMessageSize, 1024*1024)
	}
}

func TestNoopAuthenticator(t *testing.T) {
	auth := &NoopAuthenticator{}

	tests := []struct {
		name  string
		token string
	}{
		{
			name:  "empty token",
			token: "",
		},
		{
			name:  "valid looking token",
			token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
		},
		{
			name:  "any string",
			token: "anything-goes-here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID, err := auth.Authenticate(context.Background(), tt.token)

			if err != nil {
				t.Errorf("Authenticate() error = %v, want nil", err)
			}

			if userID != "anonymous" {
				t.Errorf("Authenticate() userID = %q, want anonymous", userID)
			}
		})
	}
}

func TestNoopAuthenticator_WithCancelledContext(t *testing.T) {
	auth := &NoopAuthenticator{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should still work with cancelled context
	userID, err := auth.Authenticate(ctx, "token")

	if err != nil {
		t.Errorf("Authenticate() error = %v, want nil", err)
	}

	if userID != "anonymous" {
		t.Errorf("Authenticate() userID = %q, want anonymous", userID)
	}
}

func TestMessageType(t *testing.T) {
	// Verify MessageType constants
	if MessageTypeText != 0 {
		t.Errorf("MessageTypeText = %d, want 0", MessageTypeText)
	}

	if MessageTypeBinary != 1 {
		t.Errorf("MessageTypeBinary = %d, want 1", MessageTypeBinary)
	}
}

func TestSession_ConcurrentCloseDetection(t *testing.T) {
	session := NewSession("test-session", "echo", nil)

	var wg sync.WaitGroup
	closedCount := 0
	var mu sync.Mutex

	// Multiple goroutines trying to detect close
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if session.closed.CompareAndSwap(false, true) {
				mu.Lock()
				closedCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Only one goroutine should succeed
	if closedCount != 1 {
		t.Errorf("closedCount = %d, want 1", closedCount)
	}
}
