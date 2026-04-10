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

package mock

import (
	"context"
	"net"
	"sync"
	"sync/atomic"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// HandlerFunc is the function signature for mock service handlers.
type HandlerFunc func(ctx context.Context, method string, req interface{}) (interface{}, error)

// StreamHandlerFunc is the function signature for mock stream handlers.
type StreamHandlerFunc func(ctx context.Context, method string, req interface{}, stream ServerStream) error

// ServerStream represents the server side of a bidirectional stream.
type ServerStream interface {
	// Send sends a message to the client.
	Send(msg interface{}) error
	// Recv receives a message from the client.
	Recv() (interface{}, error)
	// Close closes the stream.
	Close() error
}

// ServerConfig configures the mock server.
type ServerConfig struct {
	// Address is the address to listen on.
	Address string
	// Network is the network type (tcp, unix, etc).
	Network string
}

// DefaultServerConfig returns a default server configuration.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Address: "127.0.0.1:0",
		Network: "tcp",
	}
}

// Server is a mock server for integration testing.
type Server struct {
	mu sync.RWMutex

	// config is the server configuration.
	config ServerConfig

	// handlers maps service names to handler functions.
	handlers map[string]HandlerFunc

	// streamHandlers maps service names to stream handler functions.
	streamHandlers map[string]StreamHandlerFunc

	// listener is the network listener.
	listener net.Listener

	// running indicates whether the server is running.
	running atomic.Bool

	// requests stores all captured requests.
	requests []ServerRequest

	// defaultHandler is called if no specific handler is registered.
	defaultHandler HandlerFunc

	// defaultStreamHandler is called if no specific stream handler is registered.
	defaultStreamHandler StreamHandlerFunc
}

// ServerRequest captures a request received by the server.
type ServerRequest struct {
	Service string
	Method  string
	Payload interface{}
}

// NewServer creates a new mock server.
func NewServer(config ServerConfig) *Server {
	return &Server{
		config:         config,
		handlers:       make(map[string]HandlerFunc),
		streamHandlers: make(map[string]StreamHandlerFunc),
		requests:       make([]ServerRequest, 0),
	}
}

// Start starts the mock server.
func (s *Server) Start() error {
	if s.running.Load() {
		return errors.NewInternal("MockServer.Start", "server already running")
	}

	listener, err := net.Listen(s.config.Network, s.config.Address)
	if err != nil {
		return errors.Wrap(err, errors.CodeConnection, "MockServer.Start", "failed to start listener")
	}

	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()

	s.running.Store(true)

	go s.acceptLoop()

	return nil
}

// Stop stops the mock server.
func (s *Server) Stop() error {
	if !s.running.Swap(false) {
		return errors.NewInternal("MockServer.Stop", "server not running")
	}

	s.mu.Lock()
	listener := s.listener
	s.listener = nil
	s.mu.Unlock()

	if listener != nil {
		return listener.Close()
	}

	return nil
}

// Address returns the server's listen address.
func (s *Server) Address() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Network returns the server's network type.
func (s *Server) Network() string {
	return s.config.Network
}

// IsRunning returns true if the server is running.
func (s *Server) IsRunning() bool {
	return s.running.Load()
}

// RegisterHandler registers a handler for a service.
func (s *Server) RegisterHandler(service string, handler HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[service] = handler
}

// RegisterStreamHandler registers a stream handler for a service.
func (s *Server) RegisterStreamHandler(service string, handler StreamHandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streamHandlers[service] = handler
}

// SetDefaultHandler sets the default handler for unregistered services.
func (s *Server) SetDefaultHandler(handler HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaultHandler = handler
}

// SetDefaultStreamHandler sets the default stream handler for unregistered services.
func (s *Server) SetDefaultStreamHandler(handler StreamHandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaultStreamHandler = handler
}

// Requests returns a copy of all captured requests.
func (s *Server) Requests() []ServerRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]ServerRequest, len(s.requests))
	copy(result, s.requests)
	return result
}

// LastRequest returns the last captured request, or nil if none.
func (s *Server) LastRequest() *ServerRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.requests) == 0 {
		return nil
	}
	r := s.requests[len(s.requests)-1]
	return &r
}

// RequestCount returns the number of captured requests.
func (s *Server) RequestCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.requests)
}

// Reset clears all captured requests and registered handlers.
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = make([]ServerRequest, 0)
	s.handlers = make(map[string]HandlerFunc)
	s.streamHandlers = make(map[string]StreamHandlerFunc)
	s.defaultHandler = nil
	s.defaultStreamHandler = nil
}

// HandleRequest processes a request using the registered handler.
func (s *Server) HandleRequest(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	s.mu.Lock()
	s.requests = append(s.requests, ServerRequest{
		Service: service,
		Method:  method,
		Payload: req,
	})
	s.mu.Unlock()

	s.mu.RLock()
	handler, ok := s.handlers[service]
	defaultHandler := s.defaultHandler
	s.mu.RUnlock()

	if ok {
		return handler(ctx, method, req)
	}

	if defaultHandler != nil {
		return defaultHandler(ctx, method, req)
	}

	return nil, errors.NewNotFound("MockServer.HandleRequest", "no handler registered for service: "+service)
}

// HandleStreamRequest processes a stream request using the registered handler.
func (s *Server) HandleStreamRequest(ctx context.Context, service, method string, req interface{}, stream ServerStream) error {
	s.mu.Lock()
	s.requests = append(s.requests, ServerRequest{
		Service: service,
		Method:  method,
		Payload: req,
	})
	s.mu.Unlock()

	s.mu.RLock()
	handler, ok := s.streamHandlers[service]
	defaultHandler := s.defaultStreamHandler
	s.mu.RUnlock()

	if ok {
		return handler(ctx, method, req, stream)
	}

	if defaultHandler != nil {
		return defaultHandler(ctx, method, req, stream)
	}

	return errors.NewNotFound("MockServer.HandleStreamRequest", "no stream handler registered for service: "+service)
}

// acceptLoop handles incoming connections.
func (s *Server) acceptLoop() {
	for s.running.Load() {
		s.mu.RLock()
		listener := s.listener
		s.mu.RUnlock()

		if listener == nil {
			return
		}

		conn, err := listener.Accept()
		if err != nil {
			if !s.running.Load() {
				return
			}
			continue
		}

		go s.handleConnection(conn)
	}
}

// handleConnection handles a single connection.
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	// In a real implementation, this would parse the protocol and dispatch
	// to the appropriate handler. For the mock, connections are handled
	// via the HandleRequest and HandleStreamRequest methods directly.
}

// MockServerStream is a mock implementation of ServerStream for testing.
type MockServerStream struct {
	mu sync.Mutex

	// sendMessages stores messages sent to the client.
	sendMessages []interface{}

	// recvMessages contains messages to return on Recv.
	recvMessages []interface{}

	// recvPosition is the current position in recvMessages.
	recvPosition int

	// sendError is returned on Send.
	sendError error

	// recvError is returned after all messages are received.
	recvError error

	// closed indicates the stream is closed.
	closed bool
}

// NewMockServerStream creates a new mock server stream.
func NewMockServerStream() *MockServerStream {
	return &MockServerStream{
		sendMessages: make([]interface{}, 0),
		recvMessages: make([]interface{}, 0),
	}
}

// Send implements ServerStream.
func (s *MockServerStream) Send(msg interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.NewConnection("MockServerStream.Send", "stream is closed")
	}
	if s.sendError != nil {
		return s.sendError
	}

	s.sendMessages = append(s.sendMessages, msg)
	return nil
}

// Recv implements ServerStream.
func (s *MockServerStream) Recv() (interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, errors.NewConnection("MockServerStream.Recv", "stream is closed")
	}

	if s.recvPosition >= len(s.recvMessages) {
		if s.recvError != nil {
			return nil, s.recvError
		}
		return nil, errors.NewInternal("MockServerStream.Recv", "no more messages")
	}

	msg := s.recvMessages[s.recvPosition]
	s.recvPosition++
	return msg, nil
}

// Close implements ServerStream.
func (s *MockServerStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.NewConnection("MockServerStream.Close", "stream already closed")
	}

	s.closed = true
	return nil
}

// SetRecvMessages sets the messages to return on Recv.
func (s *MockServerStream) SetRecvMessages(messages []interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recvMessages = messages
	s.recvPosition = 0
}

// SetSendError sets the error to return on Send.
func (s *MockServerStream) SetSendError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendError = err
}

// SetRecvError sets the error to return after all messages are received.
func (s *MockServerStream) SetRecvError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recvError = err
}

// SentMessages returns a copy of all sent messages.
func (s *MockServerStream) SentMessages() []interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]interface{}, len(s.sendMessages))
	copy(result, s.sendMessages)
	return result
}

// IsClosed returns true if the stream is closed.
func (s *MockServerStream) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Verify that MockServerStream implements ServerStream.
var _ ServerStream = (*MockServerStream)(nil)
