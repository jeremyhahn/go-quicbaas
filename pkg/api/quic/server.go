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
	"crypto/x509"
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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
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

	// barrierService is the storage barrier facade used to serve OpBarrier
	// requests. Wired by lifecycle via SetBarrierService once the qrdb SDK
	// BarrierClient is online; nil before wiring completes.
	barrierService service.BarrierServicer

	// ceremonyService is the M-of-N officer ceremony facade used to serve
	// OpCeremony requests. Wired by lifecycle via SetCeremonyService once
	// the qrdb SDK CeremonyClient is online; nil before wiring completes.
	ceremonyService service.CeremonyServicer

	// qrdbCredentialSvc is the qrdb credential management facade used to
	// serve credential.* methods over the qrdb-quic ALPN. Wired by
	// lifecycle via SetQRDBCredentialService once the credential service
	// is online; nil before wiring completes.
	qrdbCredentialSvc qrdbsdk.ServiceCredentialServicer

	// tenantResolver derives the caller's tenant from the verified mTLS peer
	// certificate and stamps it onto the request context. Wired by lifecycle
	// via SetTenantResolver; nil before wiring (and in non-iam builds), in
	// which case requests proceed without a tenant. Best-effort and
	// permissive: it never rejects a request for lack of a tenant.
	tenantResolver TenantContextFunc

	// svcMu guards barrierService, ceremonyService, and qrdbCredentialSvc
	// against the racey lifecycle pattern where the server starts before
	// the facades are installed concurrently.
	svcMu sync.RWMutex

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

// WithServerBarrierService installs the BarrierServicer used by the OpBarrier
// handler tree. Wired by lifecycle before Start() is invoked; safe to call
// concurrently with request handling thanks to the server's svcMu.
func WithServerBarrierService(svc service.BarrierServicer) ServerOption {
	return func(s *Server) {
		s.svcMu.Lock()
		s.barrierService = svc
		s.svcMu.Unlock()
	}
}

// WithServerCeremonyService installs the CeremonyServicer used by the
// OpCeremony handler tree. Wired by lifecycle before Start() is invoked;
// safe to call concurrently with request handling thanks to the server's
// svcMu.
func WithServerCeremonyService(svc service.CeremonyServicer) ServerOption {
	return func(s *Server) {
		s.svcMu.Lock()
		s.ceremonyService = svc
		s.svcMu.Unlock()
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
		return &errors.QBError{
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
		return &errors.QBError{
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
//
// Each connection negotiated exactly one ALPN at the TLS handshake. When
// the negotiated ALPN is QRDBALPN ("qrdb-quic") the connection's streams
// are dispatched via the qrdb wire-format handler (method-name frames,
// protobuf payloads). All other ALPNs use the legacy quicbaas opcode
// handler.
func (s *Server) handleConnection(conn *quicgo.Conn) {
	defer s.wg.Done()
	defer s.activeConns.Add(-1)

	connState := conn.ConnectionState().TLS
	alpn := connState.NegotiatedProtocol
	connLogger := s.logger.WithFields(map[string]interface{}{
		"remote_addr": conn.RemoteAddr().String(),
		"alpn":        alpn,
	})

	// Capture the VERIFIED mTLS peer certificate (if any) once per connection.
	// It is threaded into each request context so the unified login ceremony
	// can derive the mtls factor lookup key from the handshake rather than
	// trusting a client-supplied value. Using VerifiedChains guarantees the
	// certificate passed chain validation against the configured client CA.
	var peerCert *x509.Certificate
	if len(connState.VerifiedChains) > 0 && len(connState.VerifiedChains[0]) > 0 {
		peerCert = connState.VerifiedChains[0][0]
	}

	connLogger.Debug("New QUIC connection")

	useQRDBWire := alpn == QRDBALPN

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
		if useQRDBWire {
			go s.handleQRDBStream(stream)
		} else {
			go s.handleStream(stream, connLogger, peerCert)
		}
	}
}

// handleStream handles a single QUIC stream (one request/response).
// The stream is a *quicgo.Stream (pointer to stream struct), which implements io.Reader/Writer.
func (s *Server) handleStream(stream *quicgo.Stream, connLogger *logger.Logger, peerCert *x509.Certificate) {
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

	// Snapshot lifecycle-wired facades under svcMu so concurrent
	// SetBarrierService/SetCeremonyService calls cannot race with handler
	// construction.
	s.svcMu.RLock()
	barrierService := s.barrierService
	ceremonyService := s.ceremonyService
	tenantResolver := s.tenantResolver
	s.svcMu.RUnlock()

	// Create a request-specific context with a deadline for raft operations
	// The server context may not have a deadline, but raft requires one
	requestCtx, requestCancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer requestCancel()

	// Thread the verified mTLS peer certificate into the request context so the
	// login-ceremony handler can derive the mtls lookup key from it.
	if peerCert != nil {
		requestCtx = service.WithVerifiedPeerCert(requestCtx, peerCert)

		// Best-effort: derive the caller's tenant from the verified peer cert
		// and stamp it onto the context so tenant-scoped operations observe it.
		// Never rejects — a cert that maps to no tenant simply proceeds.
		if tenantResolver != nil {
			requestCtx = tenantResolver(requestCtx, peerCert)
		}
	}

	// Create handler with service dependencies and process request
	// Include IAM and MFA service options (provided by server_iam.go when IAM is enabled)
	handler := NewHandler(s.service, kmsService, secretsService, cryptoService, streamLogger,
		WithHandlerAuthManager(s.authManager),
		WithHandlerAuthorizationService(s.authzService),
		WithBarrierService(barrierService),
		WithCeremonyService(ceremonyService),
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

// SetBarrierService installs (or replaces) the BarrierServicer used by the
// OpBarrier handler tree. Lifecycle wiring invokes this once the qrdb SDK
// BarrierClient is online. Safe to call concurrently with request handling.
func (s *Server) SetBarrierService(svc service.BarrierServicer) {
	s.svcMu.Lock()
	s.barrierService = svc
	s.svcMu.Unlock()
}

// SetCeremonyService installs (or replaces) the CeremonyServicer used by the
// OpCeremony handler tree. Lifecycle wiring invokes this once the qrdb SDK
// CeremonyClient is online. Safe to call concurrently with request handling.
func (s *Server) SetCeremonyService(svc service.CeremonyServicer) {
	s.svcMu.Lock()
	s.ceremonyService = svc
	s.svcMu.Unlock()
}

// TenantContextFunc derives the caller's tenant from a VERIFIED mTLS peer
// certificate and returns a context carrying it. It is supplied by the server
// layer (which owns the SPIFFE trust domain + home-realm directory) so this
// transport package needs no dependency on the iam build tag.
//
// The contract is BEST-EFFORT and PERMISSIVE: it MUST return a usable context
// in every case — a tenant-scoped child context on success, or the input
// context unchanged otherwise. It MUST NOT reject the request. Tenant
// enforcement (e.g. login) belongs to the specific operation, never this hook.
type TenantContextFunc func(ctx context.Context, cert *x509.Certificate) context.Context

// SetTenantResolver installs (or replaces) the best-effort tenant-derivation
// hook applied to every request that presents a verified peer certificate.
// Lifecycle wiring invokes this with a resolver bound to the operator-
// configured SPIFFE trust domain and home-realm directory. Safe to call
// concurrently with request handling.
func (s *Server) SetTenantResolver(resolve TenantContextFunc) {
	s.svcMu.Lock()
	s.tenantResolver = resolve
	s.svcMu.Unlock()
}
