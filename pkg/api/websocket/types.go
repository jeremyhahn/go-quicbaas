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

// Package websocket provides WebSocket transport for QuicBaaS messaging.
// It supports multiple subprotocols (echo, mqtt, amqp, stomp) via negotiation
// during the WebSocket upgrade handshake.
package websocket

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// MessageType represents the type of WebSocket message.
type MessageType int

const (
	// MessageTypeText indicates a text message.
	MessageTypeText MessageType = iota
	// MessageTypeBinary indicates a binary message.
	MessageTypeBinary
)

// SubprotocolHandler handles connections for a specific WebSocket subprotocol.
// Each subprotocol (echo, mqtt, amqp, stomp) implements this interface.
type SubprotocolHandler interface {
	// Name returns the subprotocol name as negotiated in Sec-WebSocket-Protocol.
	Name() string

	// HandleConnection manages the WebSocket connection for this subprotocol.
	// This method runs for the lifetime of the connection and should handle
	// all protocol-specific message processing.
	HandleConnection(ctx context.Context, conn *websocket.Conn, session *Session) error
}

// Session represents a WebSocket client session.
// Sessions are sharded by ID for low lock contention (256 shards).
type Session struct {
	// ID is the unique session identifier.
	ID string

	// UserID is the authenticated user ID (empty if not authenticated).
	UserID string

	// Conn is the underlying WebSocket connection.
	Conn *websocket.Conn

	// Subprotocol is the negotiated subprotocol name.
	Subprotocol string

	// CreatedAt is when the session was established.
	CreatedAt time.Time

	// LastActivity is the timestamp of the last message received/sent.
	LastActivity atomic.Value // time.Time

	// Metadata holds session-specific key-value pairs.
	Metadata map[string]string

	// Subscriptions tracks active channel subscriptions for this session.
	Subscriptions map[string]bool

	// mu protects Metadata and Subscriptions.
	mu sync.RWMutex

	// ctx is the session's cancellation context.
	ctx context.Context

	// cancel cancels the session context.
	cancel context.CancelFunc

	// closed indicates if the session has been closed.
	closed atomic.Bool
}

// NewSession creates a new session with the given parameters.
func NewSession(id, subprotocol string, conn *websocket.Conn) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		ID:            id,
		Conn:          conn,
		Subprotocol:   subprotocol,
		CreatedAt:     time.Now(),
		Metadata:      make(map[string]string),
		Subscriptions: make(map[string]bool),
		ctx:           ctx,
		cancel:        cancel,
	}
	s.LastActivity.Store(time.Now())
	return s
}

// Context returns the session's context.
func (s *Session) Context() context.Context {
	return s.ctx
}

// Close closes the session and cancels its context.
func (s *Session) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		s.cancel()
		return s.Conn.Close(websocket.StatusNormalClosure, "session closed")
	}
	return nil
}

// IsClosed returns true if the session has been closed.
func (s *Session) IsClosed() bool {
	return s.closed.Load()
}

// UpdateActivity updates the last activity timestamp.
func (s *Session) UpdateActivity() {
	s.LastActivity.Store(time.Now())
}

// GetLastActivity returns the last activity timestamp.
// GetLastActivity returns the last activity timestamp.
func (s *Session) GetLastActivity() time.Time {
	val, ok := s.LastActivity.Load().(time.Time)
	if !ok {
		return time.Time{}
	}
	return val
}

// SetMetadata sets a metadata key-value pair.
func (s *Session) SetMetadata(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Metadata[key] = value
}

// GetMetadata retrieves a metadata value by key.
func (s *Session) GetMetadata(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.Metadata[key]
	return val, ok
}

// Subscribe adds a channel subscription.
func (s *Session) Subscribe(channel string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Subscriptions[channel] = true
}

// Unsubscribe removes a channel subscription.
func (s *Session) Unsubscribe(channel string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Subscriptions, channel)
}

// IsSubscribed checks if the session is subscribed to a channel.
func (s *Session) IsSubscribed(channel string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Subscriptions[channel]
}

// GetSubscriptions returns a copy of all subscriptions.
func (s *Session) GetSubscriptions() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	subs := make([]string, 0, len(s.Subscriptions))
	for ch := range s.Subscriptions {
		subs = append(subs, ch)
	}
	return subs
}

// ServerConfig holds configuration for the WebSocket server.
type ServerConfig struct {
	// ReadTimeout is the maximum time to wait for a message read.
	ReadTimeout time.Duration

	// WriteTimeout is the maximum time to wait for a message write.
	WriteTimeout time.Duration

	// PingInterval is how often to send ping frames for connection health.
	PingInterval time.Duration

	// MaxMessageSize is the maximum allowed message size in bytes.
	MaxMessageSize int64

	// CompressionMode controls permessage-deflate compression.
	CompressionMode websocket.CompressionMode
}

// DefaultServerConfig returns a ServerConfig with sensible defaults.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    10 * time.Second,
		PingInterval:    25 * time.Second,
		MaxMessageSize:  1024 * 1024, // 1MB
		CompressionMode: websocket.CompressionDisabled,
	}
}

// Authenticator authenticates WebSocket connections.
type Authenticator interface {
	// Authenticate validates credentials and returns a user ID.
	// Returns empty string and error if authentication fails.
	Authenticate(ctx context.Context, token string) (userID string, err error)
}

// NoopAuthenticator allows all connections without authentication.
type NoopAuthenticator struct{}

// Authenticate always returns success with an anonymous user.
func (a *NoopAuthenticator) Authenticate(ctx context.Context, token string) (string, error) {
	return "anonymous", nil
}
