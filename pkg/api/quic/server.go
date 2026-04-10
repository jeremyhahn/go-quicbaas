//go:build quic

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

package quic

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
	"sync/atomic"
	"time"

	quicgo "github.com/quic-go/quic-go"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Server implements a QUIC transport layer for QuicBaaS.
// It uses multiplexed streams for concurrent operations and provides
// low-latency, connection migration support ideal for mobile clients.
type Server struct {
	service  service.QuicBaaS
	listener *quicgo.Listener
	logger   *logger.Logger
	config   *Config

	// Authorization dependencies
	authzService interfaces.AuthorizationService
	authManager  interfaces.AuthManager

	// Server state
	running atomic.Bool
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc

	// Metrics
	activeStreams atomic.Int64
	totalRequests atomic.Uint64
	totalErrors   atomic.Uint64
	activeConns   atomic.Int64
}

// Config contains configuration for the QUIC server.
type Config struct {
	// ListenAddr is the address to listen on (e.g., "0.0.0.0:4433")
	ListenAddr string

	// TLSConfig is the TLS configuration for QUIC
	// QUIC requires TLS 1.3, so this must be properly configured
	TLSConfig *tls.Config

	// MaxStreams is the maximum number of concurrent streams per connection
	// Default: 1000
	MaxStreams int

	// MaxIdleTimeout is the maximum idle timeout for connections
	// Default: 30 seconds
	MaxIdleTimeout time.Duration

	// MaxIncomingStreams limits the number of incoming streams
	// Default: 1000
	MaxIncomingStreams int64

	// MaxIncomingUniStreams limits the number of incoming unidirectional streams
	// Default: 100
	MaxIncomingUniStreams int64

	// KeepAlivePeriod is the period for sending keep-alive pings
	// Default: 15 seconds
	KeepAlivePeriod time.Duration

	// HandshakeIdleTimeout is the timeout for completing the handshake
	// Default: 5 seconds
	HandshakeIdleTimeout time.Duration

	// MaxReceiveBufferSize is the maximum receive buffer size
	// Default: 1MB
	MaxReceiveBufferSize uint64

	// InitialStreamReceiveWindow is the initial stream receive window
	// Default: 512KB
	InitialStreamReceiveWindow uint64

	// MaxStreamReceiveWindow is the maximum stream receive window
	// Default: 6MB
	MaxStreamReceiveWindow uint64

	// EnableMetrics enables request/error metrics collection
	// Default: true
	EnableMetrics bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		ListenAddr:                 "0.0.0.0:4433",
		MaxStreams:                 1000,
		MaxIdleTimeout:             30 * time.Second,
		MaxIncomingStreams:         1000,
		MaxIncomingUniStreams:      100,
		KeepAlivePeriod:            15 * time.Second,
		HandshakeIdleTimeout:       5 * time.Second,
		MaxReceiveBufferSize:       1 * 1024 * 1024, // 1MB
		InitialStreamReceiveWindow: 512 * 1024,      // 512KB
		MaxStreamReceiveWindow:     6 * 1024 * 1024, // 6MB
		EnableMetrics:              true,
	}
}

// ServerOption is a functional option for configuring the Server.
type ServerOption func(*Server)

// WithAuthorizationService sets the authorization service for the server.
func WithAuthorizationService(authz interfaces.AuthorizationService) ServerOption {
	return func(s *Server) {
		s.authzService = authz
	}
}

// WithAuthManager sets the authentication manager for the server.
func WithAuthManager(auth interfaces.AuthManager) ServerOption {
	return func(s *Server) {
		s.authManager = auth
	}
}

// NewServer creates a new QUIC server with the given service and configuration.
// Returns an error if the configuration is invalid or TLS is not properly configured.
func NewServer(svc service.QuicBaaS, config *Config, log *logger.Logger, opts ...ServerOption) (*Server, error) {
	if svc == nil {
		return nil, api.NewNilInputError("quic.NewServer", "service")
	}

	if config == nil {
		config = DefaultConfig()
	}

	if config.TLSConfig == nil {
		return nil, api.NewRequiredFieldError("TLS configuration")
	}

	if log == nil {
		logCfg := logger.LogConfig{
			Level:     "info",
			Format:    "json",
			Output:    "stdout",
			Component: "quic-server",
		}
		var err error
		log, err = logger.New(logCfg)
		if err != nil {
			return nil, api.NewConfigurationError("quic.NewServer", "create logger", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	server := &Server{
		service: svc,
		logger:  log,
		config:  config,
		ctx:     ctx,
		cancel:  cancel,
	}

	// Apply options
	for _, opt := range opts {
		opt(server)
	}

	return server, nil
}

// ServerAlreadyRunningError indicates the server is already running.
type ServerAlreadyRunningError struct{}

// Error implements the error interface.
func (e *ServerAlreadyRunningError) Error() string {
	return "server is already running"
}

// ServerNotRunningError indicates the server is not running.
type ServerNotRunningError struct{}

// Error implements the error interface.
func (e *ServerNotRunningError) Error() string {
	return "server is not running"
}

// Start starts the QUIC server and begins accepting connections.
// Returns an error if the server is already running or fails to start.
func (s *Server) Start() error {
	if s.running.Load() {
		return &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "quic.Start",
			Err:  &ServerAlreadyRunningError{},
		}
	}

	// Create UDP listener
	udpAddr, err := net.ResolveUDPAddr("udp", s.config.ListenAddr)
	if err != nil {
		return api.NewConnectionError("quic.Start", "resolve UDP address", s.config.ListenAddr, err)
	}

	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return api.NewConnectionError("quic.Start", "create UDP listener", s.config.ListenAddr, err)
	}

	// Configure QUIC transport
	quicConfig := &quicgo.Config{
		MaxIdleTimeout:                 s.config.MaxIdleTimeout,
		MaxIncomingStreams:             s.config.MaxIncomingStreams,
		MaxIncomingUniStreams:          s.config.MaxIncomingUniStreams,
		KeepAlivePeriod:                s.config.KeepAlivePeriod,
		HandshakeIdleTimeout:           s.config.HandshakeIdleTimeout,
		InitialStreamReceiveWindow:     s.config.InitialStreamReceiveWindow,
		MaxStreamReceiveWindow:         s.config.MaxStreamReceiveWindow,
		InitialConnectionReceiveWindow: s.config.MaxReceiveBufferSize,
		MaxConnectionReceiveWindow:     s.config.MaxReceiveBufferSize * 2,
		EnableDatagrams:                false, // We use streams, not datagrams
	}

	// Create QUIC listener
	listener, err := quicgo.Listen(udpConn, s.config.TLSConfig, quicConfig)
	if err != nil {
		_ = udpConn.Close()
		return api.NewConnectionError("quic.Start", "create QUIC listener", s.config.ListenAddr, err)
	}

	s.listener = listener
	s.running.Store(true)

	s.logger.WithFields(map[string]interface{}{
		"addr":              s.config.ListenAddr,
		"max_streams":       s.config.MaxStreams,
		"max_idle_timeout":  s.config.MaxIdleTimeout,
		"keep_alive_period": s.config.KeepAlivePeriod,
		"auth_enabled":      s.authzService != nil,
	}).Info("QUIC server started")

	// Start accepting connections
	s.wg.Add(1)
	go s.acceptConnections()

	return nil
}

// Stop gracefully stops the QUIC server.
// It waits for all active connections and streams to complete.
func (s *Server) Stop() error {
	if !s.running.Load() {
		return &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "quic.Stop",
			Err:  &ServerNotRunningError{},
		}
	}

	s.logger.Info("Stopping QUIC server")

	// Signal shutdown
	s.running.Store(false)
	s.cancel()

	// Close listener
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			s.logger.WithError(err).Warn("Error closing QUIC listener")
		}
	}

	// Wait for all connections to complete with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("QUIC server stopped gracefully")
	case <-time.After(30 * time.Second):
		s.logger.Warn("QUIC server stop timeout, forcing shutdown")
	}

	// Log final metrics
	if s.config.EnableMetrics {
		s.logger.WithFields(map[string]interface{}{
			"total_requests": s.totalRequests.Load(),
			"total_errors":   s.totalErrors.Load(),
		}).Info("QUIC server metrics")
	}

	return nil
}

// acceptConnections accepts incoming QUIC connections.
func (s *Server) acceptConnections() {
	defer s.wg.Done()

	for s.running.Load() {
		conn, err := s.listener.Accept(s.ctx)
		if err != nil {
			if !s.running.Load() {
				// Server is shutting down
				return
			}
			s.logger.WithError(err).Error("Failed to accept QUIC connection")
			continue
		}

		s.activeConns.Add(1)
		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

// handleConnection handles a single QUIC connection.
// The connection is a *quicgo.Conn (pointer to connection struct).
func (s *Server) handleConnection(conn *quicgo.Conn) {
	defer s.wg.Done()
	defer s.activeConns.Add(-1)

	connLogger := s.logger.WithFields(map[string]interface{}{
		"remote_addr": conn.RemoteAddr().String(),
	})

	connLogger.Debug("New QUIC connection")

	// Accept streams from this connection
	for s.running.Load() {
		stream, err := conn.AcceptStream(s.ctx)
		if err != nil {
			if !s.running.Load() {
				return
			}
			// Connection closed or error
			connLogger.WithError(err).Debug("Connection closed")
			return
		}

		// Check stream limit
		if s.activeStreams.Load() >= int64(s.config.MaxStreams) {
			s.logger.Warn("Maximum concurrent streams reached, rejecting request")
			_ = stream.Close() // Best effort close
			continue
		}

		s.activeStreams.Add(1)
		s.wg.Add(1)
		go s.handleStream(stream, connLogger)
	}
}

// handleStream handles a single QUIC stream (one request/response).
// The stream is a *quicgo.Stream (pointer to stream struct), which implements io.Reader/Writer.
func (s *Server) handleStream(stream *quicgo.Stream, connLogger *logger.Logger) {
	defer s.wg.Done()
	defer s.activeStreams.Add(-1)
	defer stream.Close()

	startTime := time.Now()

	// Read request (stream pointer implements io.Reader/Writer)
	req, err := ReadRequest(stream)
	if err != nil {
		s.totalErrors.Add(1)
		connLogger.WithError(err).Error("Failed to read request")
		// Send error response
		resp := &Response{
			Status:  StatusInvalidRequest,
			Payload: []byte(err.Error()),
		}
		_ = WriteResponse(stream, resp)
		return
	}

	s.totalRequests.Add(1)

	streamLogger := connLogger.WithFields(map[string]interface{}{
		"opcode": OpcodeToString(req.Opcode),
	})

	streamLogger.Debug("Processing request")

	// Get sub-services from the main QuicBaaS service
	kmsService := s.service.KMS()
	secretsService := s.service.Secrets()
	cryptoService := s.service.Crypto()

	// Try to extract seal service via type assertion (QuicBaaS implements SealService)
	var sealService service.SealService
	if svc, ok := s.service.(service.SealService); ok {
		sealService = svc
	}

	// Create a request-specific context with a deadline for raft operations
	// The server context may not have a deadline, but raft requires one
	requestCtx, requestCancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer requestCancel()

	// Create handler with service dependencies and process request
	// Include IAM and MFA service options (provided by server_iam.go when IAM is enabled)
	handler := NewHandler(s.service, kmsService, secretsService, sealService, cryptoService, streamLogger,
		WithHandlerAuthManager(s.authManager),
		WithHandlerAuthorizationService(s.authzService),
		getIAMServiceOption(s.service),
		getMFAServiceOption(s.service),
	)
	resp := handler.HandleRequest(requestCtx, req)

	// Write response
	if err := WriteResponse(stream, resp); err != nil {
		s.totalErrors.Add(1)
		streamLogger.WithError(err).Error("Failed to write response")
		return
	}

	duration := time.Since(startTime)
	streamLogger.WithFields(map[string]interface{}{
		"duration_ms": duration.Milliseconds(),
		"status":      resp.Status,
	}).Debug("Request completed")
}

// GetMetrics returns current server metrics.
func (s *Server) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"active_connections": s.activeConns.Load(),
		"active_streams":     s.activeStreams.Load(),
		"total_requests":     s.totalRequests.Load(),
		"total_errors":       s.totalErrors.Load(),
		"running":            s.running.Load(),
	}
}

// IsRunning returns true if the server is currently running.
func (s *Server) IsRunning() bool {
	return s.running.Load()
}
