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
	"net/http"
	"net/textproto"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// Server handles WebSocket connections with subprotocol negotiation.
type Server struct {
	config        ServerConfig
	sessions      *SessionManager
	subprotocols  map[string]SubprotocolHandler
	authenticator Authenticator
	logger        *logger.Logger

	// closed indicates if the server has been shut down.
	closed atomic.Bool

	// mu protects subprotocols map during registration.
	mu sync.RWMutex
}

// NewServer creates a new WebSocket server.
func NewServer(config ServerConfig, auth Authenticator, log *logger.Logger) *Server {
	if auth == nil {
		auth = &NoopAuthenticator{}
	}

	return &Server{
		config:        config,
		sessions:      NewSessionManager(),
		subprotocols:  make(map[string]SubprotocolHandler),
		authenticator: auth,
		logger:        log,
	}
}

// RegisterSubprotocol registers a subprotocol handler.
// This should be called during server initialization before handling connections.
func (s *Server) RegisterSubprotocol(handler SubprotocolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subprotocols[handler.Name()] = handler
}

// GetSubprotocols returns the list of registered subprotocol names.
func (s *Server) GetSubprotocols() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	protocols := make([]string, 0, len(s.subprotocols))
	for name := range s.subprotocols {
		protocols = append(protocols, name)
	}
	return protocols
}

// HandleHTTP handles the WebSocket upgrade request.
// This should be called from an HTTP handler.
func (s *Server) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	if s.closed.Load() {
		http.Error(w, "server closed", http.StatusServiceUnavailable)
		return
	}

	// Get requested subprotocols from Sec-WebSocket-Protocol header
	requestedProtocols := parseSubprotocols(r.Header)

	// Select a supported subprotocol
	selectedProtocol := s.selectSubprotocol(requestedProtocols)
	if selectedProtocol == "" && len(requestedProtocols) > 0 {
		// Client requested specific protocols but we don't support any
		http.Error(w, "no supported subprotocol", http.StatusBadRequest)
		return
	}

	// Build accept options
	acceptOpts := &websocket.AcceptOptions{
		CompressionMode: s.config.CompressionMode,
	}

	if selectedProtocol != "" {
		acceptOpts.Subprotocols = []string{selectedProtocol}
	}

	// Accept the WebSocket connection
	conn, err := websocket.Accept(w, r, acceptOpts)
	if err != nil {
		s.logger.Warnf("WebSocket upgrade failed: %v", err)
		return
	}

	// Set read limit for message size
	conn.SetReadLimit(s.config.MaxMessageSize)

	// Generate session ID
	sessionID := uuid.New().String()

	// Determine the protocol (use selected or default to "quicbaas")
	protocol := selectedProtocol
	if protocol == "" {
		protocol = "quicbaas"
	}

	// Create session
	session := NewSession(sessionID, protocol, conn)

	// Add to session manager
	s.sessions.Add(session)

	s.logger.Debugf("WebSocket connection established: session=%s, subprotocol=%s", sessionID, protocol)

	// Handle the connection in a goroutine
	go s.handleConnection(session)
}

// selectSubprotocol selects the first supported subprotocol from the requested list.
func (s *Server) selectSubprotocol(requested []string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, proto := range requested {
		if _, exists := s.subprotocols[proto]; exists {
			return proto
		}
	}
	return ""
}

// handleConnection manages a WebSocket session.
func (s *Server) handleConnection(session *Session) {
	defer func() {
		if err := session.Close(); err != nil {
			s.logger.Debugf("WebSocket session close error: %s, error=%v", session.ID, err)
		}
		s.sessions.Remove(session.ID)
		s.logger.Debugf("WebSocket session closed: %s", session.ID)
	}()

	// Get the handler for this session's subprotocol
	s.mu.RLock()
	handler, exists := s.subprotocols[session.Subprotocol]
	s.mu.RUnlock()

	if !exists {
		// Use default handler if no specific subprotocol handler
		s.logger.Debugf("No handler for subprotocol %s, using default quicbaas handler", session.Subprotocol)
		s.handleDefaultProtocol(session)
		return
	}

	// Run the subprotocol handler
	if err := handler.HandleConnection(session.Context(), session.Conn, session); err != nil {
		if !session.IsClosed() && !s.closed.Load() {
			s.logger.Warnf("Subprotocol handler error: session=%s, error=%v", session.ID, err)
		}
	}
}

// handleDefaultProtocol handles connections without a specific subprotocol.
// This implements a simple echo-like behavior for testing.
func (s *Server) handleDefaultProtocol(session *Session) {
	ctx := session.Context()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Read message with timeout
		msgType, data, err := session.Conn.Read(ctx)
		if err != nil {
			if !session.IsClosed() && !s.closed.Load() {
				s.logger.Debugf("Read error: session=%s, error=%v", session.ID, err)
			}
			return
		}

		session.UpdateActivity()

		// Echo the message back
		if err := session.Conn.Write(ctx, msgType, data); err != nil {
			if !session.IsClosed() {
				s.logger.Debugf("Write error: session=%s, error=%v", session.ID, err)
			}
			return
		}
	}
}

// GetSession retrieves a session by ID.
func (s *Server) GetSession(sessionID string) (*Session, bool) {
	return s.sessions.Get(sessionID)
}

// GetSessionCount returns the number of active sessions.
func (s *Server) GetSessionCount() uint64 {
	return s.sessions.Count()
}

// GetSessionStats returns session statistics.
func (s *Server) GetSessionStats() SessionStats {
	return s.sessions.GetStats()
}

// BroadcastToChannel sends a message to all sessions subscribed to a channel.
func (s *Server) BroadcastToChannel(ctx context.Context, channel string, msgType websocket.MessageType, data []byte) error {
	subscribers := s.sessions.GetSubscribersForChannel(channel)

	for _, session := range subscribers {
		if session.IsClosed() {
			continue
		}

		// Non-blocking write with timeout
		writeCtx, cancel := context.WithTimeout(ctx, s.config.WriteTimeout)
		err := session.Conn.Write(writeCtx, msgType, data)
		cancel()

		if err != nil {
			s.logger.Debugf("Broadcast write error: session=%s, channel=%s, error=%v",
				session.ID, channel, err)
			// Don't fail the entire broadcast for one failed write
			continue
		}
	}

	return nil
}

// Close shuts down the server and closes all sessions.
func (s *Server) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		s.sessions.CloseAll()
		s.logger.Debugf("WebSocket server closed")
	}
	return nil
}

// IsClosed returns true if the server has been closed.
func (s *Server) IsClosed() bool {
	return s.closed.Load()
}

// parseSubprotocols extracts WebSocket subprotocols from the Sec-WebSocket-Protocol header.
// The header may contain multiple comma-separated values or multiple header lines.
func parseSubprotocols(h http.Header) []string {
	key := textproto.CanonicalMIMEHeaderKey("Sec-WebSocket-Protocol")
	var protocols []string
	for _, v := range h[key] {
		v = strings.TrimSpace(v)
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				protocols = append(protocols, p)
			}
		}
	}
	return protocols
}
