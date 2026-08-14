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

// Package grpc provides the gRPC transport implementation for the QuicBaaS SDK.
package grpc

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// Transport implements the transport.Transport interface for gRPC.
type Transport struct {
	cfg    *transport.Config
	conn   *grpc.ClientConn
	pool   *Pool
	closed atomic.Bool

	mu sync.RWMutex
}

// New creates a new gRPC transport.
func New(cfg *transport.Config) (*Transport, error) {
	if cfg == nil {
		return nil, errors.NewInvalidInput("grpc.New", "config is required")
	}

	if cfg.Address == "" {
		return nil, errors.NewInvalidInput("grpc.New", "address is required")
	}

	t := &Transport{
		cfg: cfg,
	}

	// Build dial options
	dialOpts, err := t.buildDialOptions()
	if err != nil {
		return nil, err
	}

	// Parse address for gRPC
	parsed := transport.ParseAddress(cfg.Address)
	dialAddr := parsed.NormalizeForGRPC()

	// Create connection or pool
	if cfg.PoolEnabled {
		pool, err := NewPool(dialAddr, dialOpts, &PoolConfig{
			MinSize:        cfg.PoolMinSize,
			MaxSize:        cfg.PoolMaxSize,
			MaxAge:         cfg.PoolMaxAge,
			HealthInterval: cfg.PoolHealthInterval,
		})
		if err != nil {
			return nil, errors.Wrap(err, errors.CodeConnection, "grpc.New",
				"failed to create connection pool")
		}
		t.pool = pool
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
		defer cancel()

		conn, err := grpc.DialContext(ctx, dialAddr, dialOpts...)
		if err != nil {
			return nil, errors.Wrap(err, errors.CodeConnection, "grpc.New",
				"failed to connect to server")
		}
		t.conn = conn
	}

	return t, nil
}

// buildDialOptions constructs gRPC dial options from config.
func (t *Transport) buildDialOptions() ([]grpc.DialOption, error) {
	var opts []grpc.DialOption

	// TLS configuration is required for all connections
	if t.cfg.TLSConfig == nil {
		return nil, errors.NewConfiguration("grpc.buildDialOptions",
			"TLS configuration is required for all connections")
	}
	creds := credentials.NewTLS(t.cfg.TLSConfig)
	opts = append(opts, grpc.WithTransportCredentials(creds))

	// Keepalive configuration
	if t.cfg.KeepaliveTime > 0 {
		opts = append(opts, grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                t.cfg.KeepaliveTime,
			Timeout:             t.cfg.KeepaliveTimeout,
			PermitWithoutStream: true,
		}))
	}

	// Platform bearer token: attach it as "authorization: Bearer <token>"
	// outgoing metadata on every RPC so the caller authenticates against the
	// server's default-deny scope -> RBAC gate. Empty token leaves RPCs
	// anonymous (no interceptor installed), preserving prior behavior.
	if t.cfg.AuthToken != "" {
		bearer := "Bearer " + t.cfg.AuthToken
		opts = append(opts,
			grpc.WithUnaryInterceptor(bearerUnaryInterceptor(bearer)),
			grpc.WithStreamInterceptor(bearerStreamInterceptor(bearer)),
		)
	}

	return opts, nil
}

// bearerUnaryInterceptor returns a unary client interceptor that appends the
// given "Bearer <token>" authorization metadata to every outgoing unary RPC.
func bearerUnaryInterceptor(bearer string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", bearer)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// bearerStreamInterceptor returns a stream client interceptor that appends the
// given "Bearer <token>" authorization metadata to every outgoing streaming RPC.
func bearerStreamInterceptor(bearer string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn,
		method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", bearer)
		return streamer(ctx, desc, cc, method, opts...)
	}
}

// getConn returns a connection from the pool or the single connection.
func (t *Transport) getConn() (*grpc.ClientConn, error) {
	if t.closed.Load() {
		return nil, errors.NewConnection("grpc.getConn", "transport is closed")
	}

	if t.pool != nil {
		return t.pool.Get()
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.conn == nil {
		return nil, errors.NewConnection("grpc.getConn", "connection not available")
	}

	return t.conn, nil
}

// returnConn returns a connection to the pool (no-op for single connection).
func (t *Transport) returnConn(conn *grpc.ClientConn) {
	if t.pool != nil {
		t.pool.Put(conn)
	}
}

// Request sends a unary request and returns the response.
func (t *Transport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	return t.requestWithRetry(ctx, service, method, req)
}

// requestWithRetry executes a request with retry logic.
func (t *Transport) requestWithRetry(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	var lastErr error
	backoff := t.cfg.RetryBackoff

	for attempt := 0; attempt <= t.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retry
			select {
			case <-ctx.Done():
				return nil, errors.Wrap(ctx.Err(), errors.CodeCanceled, "grpc.Request",
					"request canceled during retry")
			case <-time.After(backoff):
				// Exponential backoff
				backoff = minDuration(backoff*2, t.cfg.MaxRetryBackoff)
			}
		}

		resp, err := t.doRequest(ctx, service, method, req)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		// Check if error is retryable
		if !isRetryableError(err) {
			return nil, err
		}
	}

	return nil, lastErr
}

// doRequest executes a single request.
func (t *Transport) doRequest(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	conn, err := t.getConn()
	if err != nil {
		return nil, err
	}
	defer t.returnConn(conn)

	// Apply default timeout only when the caller has not set a deadline.
	// This allows callers (e.g., CLI --timeout flag) to provide longer
	// deadlines that are not clamped by the SDK default.
	if t.cfg.Timeout > 0 {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, t.cfg.Timeout)
			defer cancel()
		}
	}

	// Check if this is an Auth service request and route through Auth handler.
	// Auth services use plain Go structs which don't implement proto.Message,
	// so we need to convert them to protobuf types before calling gRPC.
	if isAuthService(service) {
		handler := newAuthHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is an IAM service request and route through IAM handler.
	// IAM services use plain Go structs which don't implement proto.Message,
	// so we need to convert them to protobuf types before calling gRPC.
	if isIAMService(service) {
		handler := newIAMHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is a KV/Batch/Transaction service request and route through KV handler.
	// KV services use plain Go structs which don't implement proto.Message,
	// so we need to convert them to protobuf types before calling gRPC.
	if isKVService(service) {
		handler := newKVHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is a Health service request and route through health handler.
	// Health services use plain Go structs which don't implement proto.Message,
	// so we need to convert them to protobuf types before calling gRPC.
	if isHealthService(service) {
		handler := newHealthHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is a Cluster service request and route through cluster handler.
	// Cluster services use plain Go structs which don't implement proto.Message,
	// so we need to convert them to protobuf types before calling gRPC.
	if isClusterService(service) {
		handler := newClusterHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is a Vector service request and route through vector handler.
	// Vector services use plain Go structs which don't implement proto.Message,
	// so we need to convert them to protobuf types before calling gRPC.
	if isVectorService(service) {
		handler := newVectorHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is a RAG service request and route through RAG handler.
	// RAG services use plain Go structs which don't implement proto.Message,
	// so we need to convert them to JSON format before calling gRPC.
	if isRAGService(service) {
		handler := newRAGHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is an Embedding service request and route through embedding handler.
	// Embedding services use plain Go structs which don't implement proto.Message,
	// so we need to convert them to protobuf types before calling gRPC.
	if isEmbeddingService(service) {
		handler := newEmbeddingHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is an RBAC service request and route through RBAC handler.
	// RBAC services use plain Go structs which don't implement proto.Message,
	// so we need to convert them to protobuf types before calling gRPC.
	if isRBACService(service) {
		handler := newRBACHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is an Orchestrator service request and route through orchestrator handler.
	// Orchestrator services use plain Go structs which don't implement proto.Message,
	// so we need to convert them to JSON format before calling gRPC.
	if isOrchestratorService(service) {
		handler := newOrchestratorHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is a pooled provider Account service request. Account
	// SDK types are plain Go structs, so the handler converts them to the
	// generated AccountService proto types before calling gRPC. This branch
	// also serves the Unix transport, which delegates to gRPC verbatim.
	if isAccountService(service) {
		handler := newAccountHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is a Project service request and route through the project
	// handler. Project SDK types are plain Go structs, so the handler converts
	// them to the generated ProjectService proto types before calling gRPC.
	// This branch also serves the Unix transport, which delegates to gRPC
	// verbatim.
	if isProjectService(service) {
		handler := newProjectHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is a Certificate service request and route through the
	// certificate handler. Certificate services use plain Go structs which
	// don't implement proto.Message, so we need to convert them to the
	// generated CertificateService proto types before calling gRPC.
	if isCertificateService(service) {
		handler := newCertificateHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is an SSH service request and route through the SSH
	// handler. The SDK SSH surface reuses the go-xkms SSH DTOs, which are
	// plain Go structs, so they must be converted to the generated
	// SSHService proto types before calling gRPC.
	if isSSHService(service) {
		handler := newSSHHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is a Trust service request and route through the trust
	// handler. The SDK trust surface uses plain Go structs that don't
	// implement proto.Message, so they must be converted to the generated
	// TrustService proto types before calling gRPC.
	if isTrustService(service) {
		handler := newTrustHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is an Attestation service request and route through the
	// attestation handler, for the same reason as Trust: the SDK
	// attestation surface uses plain Go structs that don't implement
	// proto.Message and must be converted to the generated
	// AttestationService proto types first.
	if isAttestationService(service) {
		handler := newAttestationHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is a FIDO2 ceremony request and route through the FIDO2
	// handler. The SDK FIDO2 surface exchanges the go-xkms transport wire
	// structs, which don't implement proto.Message, so they must be
	// converted to the generated FIDO2Service proto types first. The tenant
	// scope is resolved from the request context and rides the proto
	// tenant_id field. This branch also serves the Unix transport, which
	// delegates to gRPC verbatim.
	if isFIDO2Service(service) {
		handler := newFIDO2Handler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is a PIV service request and route through the PIV
	// handler, for the same reason as FIDO2: the SDK PIV surface exchanges
	// the go-xkms transport wire structs, which must be converted to the
	// generated PIVService proto types. This branch also serves the Unix
	// transport, which delegates to gRPC verbatim.
	if isPIVService(service) {
		handler := newPIVHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is an Operations service request and route through the
	// operations handler. The SDK operations client uses plain Go structs which
	// don't implement proto.Message, so we need to convert them to the generated
	// QuicBaaS proto types (UpgradeNode/MigrateData/ReplaceNode/...) before
	// calling gRPC; otherwise the untyped conn.Invoke path below fails to
	// marshal the non-proto request struct.
	if isOperationsService(service) {
		handler := newOperationsHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is a KMS or DEK service request and route through the KMS
	// handler. The KMS/DEK SDK clients use plain Go structs which don't
	// implement proto.Message; the handler converts them to the generated
	// KeyManagement/DEKManagement proto types (reusing the pkg/api/grpc
	// converters) before calling gRPC.
	if isKMSService(service) || isDEKService(service) {
		handler, herr := newKMSHandler(conn)
		if herr != nil {
			return nil, herr
		}
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is a DynamicSecrets service request and route it through
	// the dynamic secrets handler. The SDK dynamic secrets client sends plain
	// Go structs; the handler converts them to the generated
	// DynamicSecretsService proto types and decodes the reply back into the
	// leaf client/dynamicsecrets response types.
	if isDynamicSecretsService(service) {
		handler := newDynamicSecretsHandler(conn)
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is a Secrets service request and route through the secrets
	// handler. The secrets SDK client uses plain Go structs which don't
	// implement proto.Message; the handler converts them to the generated
	// SecretsManagement proto types before calling gRPC.
	if isSecretsService(service) {
		handler, herr := newSecretsHandler(conn)
		if herr != nil {
			return nil, herr
		}
		return handler.handleRequest(ctx, service, method, req)
	}

	// Check if this is a Crypto service request and route through the crypto
	// handler. The crypto SDK client converts its inputs to canonical service.*
	// requests, but those still don't implement proto.Message; the handler
	// relays them through the generated Crypto proto service.
	if isCryptoService(service) {
		handler, herr := newCryptoHandler(conn)
		if herr != nil {
			return nil, herr
		}
		return handler.handleRequest(ctx, service, method, req)
	}

	// Build full method path for other services.
	fullMethod := "/" + service + "/" + method

	// Execute the call for other services.
	var resp interface{}
	err = conn.Invoke(ctx, fullMethod, req, &resp)
	if err != nil {
		return nil, convertGRPCError(err, "grpc.Request")
	}

	return resp, nil
}

// RequestStream initiates a streaming request.
func (t *Transport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	conn, err := t.getConn()
	if err != nil {
		return nil, err
	}

	// Route orchestrator streaming requests through the orchestrator handler
	// which converts SDK types to proto types.
	if isOrchestratorService(service) {
		handler := newOrchestratorHandler(conn)
		s, streamErr := handler.handleStream(ctx, method, req, t, conn)
		if streamErr != nil {
			t.returnConn(conn)
			return nil, streamErr
		}
		return s, nil
	}

	// Build full method path
	fullMethod := "/" + service + "/" + method

	// Create stream descriptor
	desc := &grpc.StreamDesc{
		StreamName:    method,
		ServerStreams: true,
		ClientStreams: true,
	}

	// Create stream
	clientStream, err := conn.NewStream(ctx, desc, fullMethod)
	if err != nil {
		t.returnConn(conn)
		return nil, convertGRPCError(err, "grpc.RequestStream")
	}

	// Send initial request if provided
	if req != nil {
		if err := clientStream.SendMsg(req); err != nil {
			t.returnConn(conn)
			return nil, convertGRPCError(err, "grpc.RequestStream")
		}
	}

	return &stream{
		stream:    clientStream,
		transport: t,
		conn:      conn,
	}, nil
}

// Conn returns the underlying connection.
func (t *Transport) Conn() interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.conn
}

// Close closes the transport.
func (t *Transport) Close() error {
	if !t.closed.CompareAndSwap(false, true) {
		return nil
	}

	var closeErr error

	if t.pool != nil {
		closeErr = t.pool.Close()
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.conn != nil {
		if err := t.conn.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
		t.conn = nil
	}

	return closeErr
}

// Healthy returns true if the transport is healthy.
func (t *Transport) Healthy() bool {
	if t.closed.Load() {
		return false
	}

	if t.pool != nil {
		return t.pool.Healthy()
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.conn == nil {
		return false
	}

	return t.conn.GetState() == connectivity.Ready
}

// stream implements transport.Stream for gRPC.
type stream struct {
	stream    grpc.ClientStream
	transport *Transport
	conn      *grpc.ClientConn
	closed    atomic.Bool
}

// Send sends a message on the stream.
func (s *stream) Send(msg interface{}) error {
	if s.closed.Load() {
		return errors.NewConnection("grpc.stream.Send", "stream is closed")
	}
	if err := s.stream.SendMsg(msg); err != nil {
		return convertGRPCError(err, "grpc.stream.Send")
	}
	return nil
}

// Recv receives a message from the stream.
func (s *stream) Recv() (interface{}, error) {
	if s.closed.Load() {
		return nil, errors.NewConnection("grpc.stream.Recv", "stream is closed")
	}
	var msg interface{}
	if err := s.stream.RecvMsg(&msg); err != nil {
		return nil, convertGRPCError(err, "grpc.stream.Recv")
	}
	return msg, nil
}

// CloseSend closes the send side of the stream.
func (s *stream) CloseSend() error {
	return s.stream.CloseSend()
}

// Close closes the stream entirely.
func (s *stream) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	s.transport.returnConn(s.conn)
	return nil
}

// convertGRPCError converts a gRPC error to a QBError.
func convertGRPCError(err error, op string) error {
	if err == nil {
		return nil
	}

	st, ok := status.FromError(err)
	if !ok {
		return errors.Wrap(err, errors.CodeUnknown, op, "unknown error")
	}

	var code errors.ErrorCode
	switch st.Code() {
	case codes.OK:
		return nil
	case codes.InvalidArgument:
		code = errors.CodeInvalidInput
	case codes.NotFound:
		code = errors.CodeNotFound
	case codes.AlreadyExists:
		code = errors.CodeAlreadyExists
	case codes.PermissionDenied:
		code = errors.CodeForbidden
	case codes.Unauthenticated:
		code = errors.CodeUnauthorized
	case codes.ResourceExhausted:
		code = errors.CodeResourceExhausted
	case codes.FailedPrecondition:
		code = errors.CodePreconditionFailed
	case codes.Aborted:
		code = errors.CodeConflict
	case codes.Unimplemented:
		code = errors.CodeUnimplemented
	case codes.Internal:
		code = errors.CodeInternal
	case codes.Unavailable:
		code = errors.CodeUnavailable
	case codes.DeadlineExceeded:
		code = errors.CodeTimeout
	case codes.Canceled:
		code = errors.CodeCanceled
	default:
		code = errors.CodeUnknown
	}

	return errors.Wrap(err, code, op, st.Message())
}

// isRetryableError returns true if the error can be retried.
func isRetryableError(err error) bool {
	if errors.IsRetryable(err) {
		return true
	}

	st, ok := status.FromError(err)
	if !ok {
		return false
	}

	switch st.Code() {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return true
	default:
		return false
	}
}

// minDuration returns the minimum of two durations.
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// Factory implements transport.Factory for gRPC.
type Factory struct{}

// Create creates a new gRPC transport.
func (f *Factory) Create(cfg *transport.Config) (transport.Transport, error) {
	return New(cfg)
}
