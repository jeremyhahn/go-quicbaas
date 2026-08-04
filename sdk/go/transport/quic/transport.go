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
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// alpnProtocol is the ALPN protocol identifier for QuicBaaS QUIC.
const alpnProtocol = "quicbaas-quic"

// Transport implements the transport.Transport interface for QUIC.
type Transport struct {
	cfg       *transport.Config
	opts      *Options
	conn      *quic.Conn
	closed    atomic.Bool
	healthy   atomic.Bool
	authToken atomic.Value

	mu sync.RWMutex
}

// New creates a new QUIC transport.
func New(cfg *transport.Config, opts ...Option) (*Transport, error) {
	if cfg == nil {
		return nil, errors.NewInvalidInput("quic.New", "config is required")
	}

	if cfg.Address == "" {
		return nil, errors.NewInvalidInput("quic.New", "address is required")
	}

	// Apply options
	options := DefaultOptions()
	options.Apply(opts...)

	if err := options.Validate(); err != nil {
		return nil, err
	}

	t := &Transport{
		cfg:  cfg,
		opts: options,
	}

	// Set initial auth token
	if options.AuthToken != "" {
		t.authToken.Store(options.AuthToken)
	} else {
		t.authToken.Store("")
	}

	// Connect to the server
	if err := t.connect(); err != nil {
		return nil, err
	}

	return t, nil
}

// connect establishes the QUIC connection.
func (t *Transport) connect() error {
	// Build TLS configuration
	tlsConfig, err := t.buildTLSConfig()
	if err != nil {
		return err
	}

	// Build QUIC configuration
	quicConfig := t.buildQUICConfig()

	// Parse address
	parsed := transport.ParseAddress(t.cfg.Address)

	// Create context with timeout
	ctx := context.Background()
	if t.opts.HandshakeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, t.opts.HandshakeTimeout)
		defer cancel()
	}

	// Dial the server
	conn, err := quic.DialAddr(ctx, parsed.Address, tlsConfig, quicConfig)
	if err != nil {
		return errors.Wrap(err, errors.CodeConnection, "quic.connect",
			"failed to connect to server")
	}

	t.mu.Lock()
	t.conn = conn
	t.mu.Unlock()

	t.healthy.Store(true)

	return nil
}

// buildTLSConfig creates the TLS configuration for QUIC.
func (t *Transport) buildTLSConfig() (*tls.Config, error) {
	var tlsConfig *tls.Config

	if t.opts.TLSConfig != nil {
		tlsConfig = t.opts.TLSConfig.Clone()
	} else if t.cfg.TLSConfig != nil {
		tlsConfig = t.cfg.TLSConfig.Clone()
	} else {
		return nil, errors.NewConfiguration("quic.buildTLSConfig",
			"TLS configuration required for QUIC transport")
	}

	// QUIC requires TLS 1.3
	tlsConfig.MinVersion = tls.VersionTLS13

	// Set ALPN protocol
	if len(tlsConfig.NextProtos) == 0 {
		tlsConfig.NextProtos = []string{alpnProtocol}
	}

	return tlsConfig, nil
}

// buildQUICConfig creates the QUIC configuration.
func (t *Transport) buildQUICConfig() *quic.Config {
	return &quic.Config{
		HandshakeIdleTimeout:           t.opts.HandshakeTimeout,
		MaxIdleTimeout:                 t.opts.MaxIdleTimeout,
		KeepAlivePeriod:                t.opts.KeepAlivePeriod,
		MaxIncomingStreams:             t.opts.MaxIncomingStreams,
		MaxIncomingUniStreams:          t.opts.MaxIncomingUniStreams,
		InitialStreamReceiveWindow:     t.opts.InitialStreamReceiveWindow,
		InitialConnectionReceiveWindow: t.opts.InitialConnectionReceiveWindow,
		Allow0RTT:                      t.opts.Enable0RTT,
	}
}

// Request sends a unary request and returns the response.
func (t *Transport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	if t.closed.Load() {
		return nil, errors.NewConnection("quic.Request", "transport is closed")
	}

	return t.requestWithRetry(ctx, service, method, req)
}

// requestWithRetry executes a request with retry logic.
func (t *Transport) requestWithRetry(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	var lastErr error
	backoff := t.cfg.RetryBackoff

	for attempt := 0; attempt <= t.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, errors.Wrap(ctx.Err(), errors.CodeCanceled, "quic.Request",
					"request canceled during retry")
			case <-time.After(backoff):
				backoff = minDuration(backoff*2, t.cfg.MaxRetryBackoff)
			}
		}

		resp, err := t.doRequest(ctx, service, method, req)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		if !isRetryableError(err) {
			return nil, err
		}
	}

	return nil, lastErr
}

// doRequest executes a single request.
func (t *Transport) doRequest(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
	// Get opcode for the method
	opcode, ok := MethodToOpcode(svc, method)
	if !ok {
		return nil, errors.NewInvalidInput("quic.doRequest",
			"unknown method: "+svc+"/"+method)
	}

	// Transform requests based on service type
	var transformedReq interface{} = req
	var isVector bool
	var isIAM bool
	var isRAG bool
	var isCluster bool
	var isEmbedding bool
	var isOrchestrator bool

	// Transform Vector requests to server-side format
	if isVectorService(svc) {
		isVector = true
		vh := newVectorHandler(t)
		var err error
		transformedReq, err = vh.transformRequest(method, req)
		if err != nil {
			return nil, err
		}
	}

	// Transform IAM requests to server-side format
	if isIAMService(svc) {
		isIAM = true
		ih := newIAMHandler(t)
		var err error
		transformedReq, err = ih.transformRequest(svc, method, req)
		if err != nil {
			return nil, err
		}
	}

	// Transform RAG requests to server-side format
	if isRAGService(svc) {
		isRAG = true
		var err error
		transformedReq, err = transformRAGRequest(method, req)
		if err != nil {
			return nil, err
		}
	}

	// Check if this is a Cluster service request
	if isClusterService(svc) {
		isCluster = true
	}

	// Check if this is an Embedding service request
	if isEmbeddingService(svc) {
		isEmbedding = true
	}

	// Check if this is an Orchestrator service request
	if isOrchestratorService(svc) {
		isOrchestrator = true
	}

	// Detect Barrier and Ceremony services — both use a single dispatch
	// opcode (OpBarrier / OpCeremony) with the sub-operation byte prepended
	// to the JSON payload. Identical wire pattern to Orchestrator.
	isBarrier := isBarrierService(svc)
	isCeremony := isCeremonyService(svc)

	// Detect Backup service. It uses distinct per-method opcodes
	// (OpBackupCreate, OpBackupList, ...) and needs no request transform; only
	// the response must be decoded into the canonical service.*BackupResponse
	// types so callers get the same shape as every other transport.
	isBackup := isBackupService(svc)

	// Detect the credential / crypto services. Each uses distinct per-method
	// opcodes and needs no request transform; only the response must be decoded
	// into the canonical service.*Response types so callers receive the same
	// shape every other transport returns.
	isKMS := isKMSService(svc)
	isDEK := isDEKService(svc)
	if isKMS {
		// A few KMS SDK request shapes differ from the canonical
		// service.*Request the server decodes (flat rotation-policy body,
		// PascalCase metadata update); re-encode those into the server wire
		// shape. All other KMS methods pass through unchanged.
		var err error
		transformedReq, err = transformKMSRequest(method, transformedReq)
		if err != nil {
			return nil, err
		}
	}
	isCrypto := isCryptoService(svc)
	isSecrets := isSecretsService(svc)

	// Detect the Certificate and ObjectStorage services. Both use distinct
	// per-method opcodes and require no request transform — their SDK request
	// structs encode to the same wire shape the server unmarshals (Certificate
	// shares snake_case JSON tags with the server; ObjectStorage shares the
	// server's untagged PascalCase fields). Only the response must be decoded
	// into the bespoke SDK types so callers get the same shape every other
	// transport returns.
	isCertificate := isCertificateService(svc)
	isTrust := isTrustService(svc)
	isObjectStorage := isObjectStorageService(svc)

	// Detect the unified login ceremony (quicbaas.Auth/BeginLogin |
	// FinishLogin). Both use distinct per-method opcodes (OpAuthBeginLogin /
	// OpAuthFinishLogin) and need no request transform — the qrdb transport
	// request structs encode directly via JSON. Only the response must be
	// decoded into the canonical qrdb transport types.
	isLogin := isLoginCeremonyMethod(svc, method)

	// Detect the TimeSeries service. The high-level client sends "Write"/"Query"
	// with its own request structs (int aggregate enums, duration intervals); the
	// server decodes service.WritePointRequest / service.TSQueryRequest. We
	// re-encode the request into the server wire shape here.
	isTimeseries := isTimeseriesService(svc)
	if isTimeseries {
		var err error
		transformedReq, err = transformTimeseriesRequest(method, req)
		if err != nil {
			return nil, err
		}
	}

	// Detect the Messaging service. The server dispatches every messaging method
	// on the OpMsg (0x00) opcode with a sub-operation code in payload[0] (range
	// 0x20-0x2F, disjoint from Scheduler's 0x01-0x1F). The SDK messaging client
	// sends PascalCase request structs that differ from the server's snake_case
	// Msg*Request types, so we re-encode the request into the server wire shape
	// here. Keyed on the service name so it never collides with Scheduler.
	isMessaging := isMessagingService(svc)
	if isMessaging {
		var err error
		transformedReq, err = transformMessagingRequest(method, req)
		if err != nil {
			return nil, err
		}
	}

	// Detect the Scheduler service. The server dispatches every scheduler method
	// on a single opcode (OpScheduler = 0x00) with the per-method sub-operation
	// code in payload[0], followed by the JSON-encoded request. The SDK scheduler
	// client sends its own request structs whose field names and enum encodings
	// differ from the canonical service.*Request types the server unmarshals, so
	// we re-encode the request into the server wire shape here.
	isScheduler := isSchedulerService(svc)
	if isScheduler {
		var err error
		transformedReq, err = transformSchedulerRequest(method, req)
		if err != nil {
			return nil, err
		}
	}

	// Encode request payload
	payload, err := EncodePayload(transformedReq)
	if err != nil {
		return nil, err
	}

	// For Orchestrator operations the server uses OpOrchestrator (0x8F) as the
	// top-level opcode with the sub-operation code in payload[0], followed by
	// the JSON-encoded request body. We prepend the SubOp byte here so the wire
	// format matches what handleOrchestratorDispatch expects.
	if isOrchestrator {
		// Inject tenant_id from context into the JSON body before SubOp prepend.
		// The orchestration service validates tenant_id at the business logic layer,
		// and the SDK request structs deliberately do not expose a TenantID field
		// (it is resolved from context by every transport, matching gRPC behaviour).
		payload = injectTenantID(ctx, payload)

		subOp, ok := OrchestratorMethodToSubOp(method)
		if !ok {
			return nil, errors.NewInvalidInput("quic.doRequest",
				"unknown orchestrator method: "+method)
		}
		withSubOp := make([]byte, 1+len(payload))
		withSubOp[0] = subOp
		copy(withSubOp[1:], payload)
		payload = withSubOp
	}

	// For Barrier operations the server uses OpBarrier (0xC0) as the
	// top-level opcode with the sub-operation code in payload[0]. Same
	// wire pattern as Orchestrator above.
	if isBarrier {
		subOp, ok := BarrierMethodToSubOp(method)
		if !ok {
			return nil, errors.NewInvalidInput("quic.doRequest",
				"unknown barrier method: "+method)
		}
		withSubOp := make([]byte, 1+len(payload))
		withSubOp[0] = subOp
		copy(withSubOp[1:], payload)
		payload = withSubOp
	}

	// For Ceremony operations the server uses OpCeremony (0xC1) as the
	// top-level opcode with the sub-operation code in payload[0].
	if isCeremony {
		subOp, ok := CeremonyMethodToSubOp(method)
		if !ok {
			return nil, errors.NewInvalidInput("quic.doRequest",
				"unknown ceremony method: "+method)
		}
		withSubOp := make([]byte, 1+len(payload))
		withSubOp[0] = subOp
		copy(withSubOp[1:], payload)
		payload = withSubOp
	}

	// For Scheduler operations the server uses OpScheduler (0x00) as the
	// top-level opcode with the sub-operation code in payload[0]. The scheduler
	// sub-op range (0x01-0x1F) is disjoint from the messaging (0x20-0x2F) and
	// extended-vector (0x40-0x4F) ranges that share the same OpScheduler dispatch
	// byte on the server, so keying on the service name keeps dispatch unambiguous.
	if isScheduler {
		subOp, ok := SchedulerMethodToSubOp(method)
		if !ok {
			return nil, errors.NewInvalidInput("quic.doRequest",
				"unknown scheduler method: "+method)
		}
		withSubOp := make([]byte, 1+len(payload))
		withSubOp[0] = subOp
		copy(withSubOp[1:], payload)
		payload = withSubOp
	}

	// For Messaging operations the server uses OpMsg (0x00) as the top-level
	// opcode with the sub-operation code in payload[0]. The messaging sub-op
	// range (0x20-0x2F) is disjoint from the scheduler range that shares the same
	// 0x00 dispatch byte, so keying on the service name keeps dispatch
	// unambiguous on the wire.
	if isMessaging {
		subOp, ok := MessagingMethodToSubOp(method)
		if !ok {
			return nil, errors.NewInvalidInput("quic.doRequest",
				"unknown messaging method: "+method)
		}
		withSubOp := make([]byte, 1+len(payload))
		withSubOp[0] = subOp
		copy(withSubOp[1:], payload)
		payload = withSubOp
	}

	// Get auth token
	token := ""
	if v := t.authToken.Load(); v != nil {
		token = v.(string)
	}

	protoReq := &Request{
		Opcode:  opcode,
		Token:   token,
		Payload: payload,
	}

	// Open a new stream
	t.mu.RLock()
	conn := t.conn
	t.mu.RUnlock()

	if conn == nil {
		return nil, errors.NewConnection("quic.doRequest", "connection not available")
	}

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

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		t.healthy.Store(false)
		return nil, errors.Wrap(err, errors.CodeConnection, "quic.doRequest",
			"failed to open stream")
	}
	defer stream.Close()

	// Write request
	if err := WriteRequest(stream, protoReq); err != nil {
		return nil, err
	}

	// Close the write side to signal request complete
	if err := stream.Close(); err != nil {
		return nil, errors.Wrap(err, errors.CodeTransport, "quic.doRequest",
			"failed to close write side")
	}

	// Read response
	resp, err := ReadResponse(stream)
	if err != nil {
		return nil, err
	}

	// Check status
	if resp.Status != StatusOK {
		var errMsg string
		if len(resp.Payload) > 0 {
			errMsg = string(resp.Payload)
		} else {
			errMsg = "request failed"
		}
		return nil, StatusToError(resp.Status, errMsg)
	}

	// Decode response payload
	if len(resp.Payload) == 0 {
		// Several services accept a nil response as success (void methods like
		// messaging Publish/Delete). Route the empty payload through their decoder
		// so the caller's type expectations are met instead of a bare nil.
		if isTimeseries {
			return decodeTimeseriesResponse(method, resp.Payload)
		}
		if isMessaging {
			return decodeMessagingResponse(method, resp.Payload)
		}
		return nil, nil
	}

	// Orchestrator responses may be JSON arrays (e.g., ListBackends returns
	// []*AgentBackendInfo directly). We decode the raw bytes instead of going
	// through the generic map[string]interface{} path to handle both shapes.
	if isOrchestrator {
		return decodeOrchestratorResponse(method, resp.Payload)
	}

	// Barrier / Ceremony responses are decoded by domain-specific helpers
	// so per-method converters can be registered without touching the core
	// transport loop. Both helpers fall back to a generic map decode for
	// methods that do not yet have a typed converter.
	if isBarrier {
		return decodeBarrierResponse(method, resp.Payload)
	}
	if isCeremony {
		return decodeCeremonyResponse(method, resp.Payload)
	}
	if isBackup {
		return decodeBackupResponse(method, resp.Payload)
	}
	if isKMS {
		return decodeKMSResponse(method, resp.Payload)
	}
	if isDEK {
		return decodeDEKResponse(method, resp.Payload)
	}
	if isCrypto {
		return decodeCryptoResponse(method, resp.Payload)
	}
	if isSecrets {
		return decodeSecretsResponse(method, resp.Payload)
	}
	if isScheduler {
		return decodeSchedulerResponse(method, resp.Payload)
	}
	if isTimeseries {
		return decodeTimeseriesResponse(method, resp.Payload)
	}
	if isMessaging {
		return decodeMessagingResponse(method, resp.Payload)
	}
	if isCertificate {
		return decodeCertificateResponse(method, resp.Payload)
	}
	if isTrust {
		return decodeTrustResponse(method, resp.Payload)
	}
	if isObjectStorage {
		return decodeObjectStorageResponse(method, resp.Payload)
	}
	if isLogin {
		return decodeLoginResponse(method, resp.Payload)
	}

	var decoded map[string]interface{}
	if err := DecodePayload(resp.Payload, &decoded); err != nil {
		return nil, err
	}

	// Convert to appropriate SDK type for KV services
	if isKVService(svc) {
		return convertKVResponse(method, decoded)
	}

	// Convert to appropriate SDK type for Vector services
	if isVector {
		vh := newVectorHandler(t)
		return vh.transformResponse(method, decoded)
	}

	// Convert to appropriate SDK type for IAM services
	if isIAM {
		ih := newIAMHandler(t)
		return ih.transformResponse(svc, method, decoded)
	}

	// Convert to appropriate SDK type for RAG services
	if isRAG {
		return convertRAGResponse(method, decoded)
	}

	// Convert to appropriate SDK type for Cluster services
	if isCluster {
		return convertClusterResponse(method, decoded)
	}

	// Convert to appropriate SDK type for Embedding services
	if isEmbedding {
		return convertEmbeddingResponse(method, decoded)
	}

	// Return raw map for other services
	return decoded, nil
}

// RequestStream initiates a streaming request.
func (t *Transport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	if t.closed.Load() {
		return nil, errors.NewConnection("quic.RequestStream", "transport is closed")
	}

	// Get opcode for the method
	opcode, ok := MethodToOpcode(service, method)
	if !ok {
		return nil, errors.NewInvalidInput("quic.RequestStream",
			"unknown method: "+service+"/"+method)
	}

	t.mu.RLock()
	conn := t.conn
	t.mu.RUnlock()

	if conn == nil {
		return nil, errors.NewConnection("quic.RequestStream", "connection not available")
	}

	// Open a bidirectional stream
	quicStream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		t.healthy.Store(false)
		return nil, errors.Wrap(err, errors.CodeConnection, "quic.RequestStream",
			"failed to open stream")
	}

	// Get auth token
	token := ""
	if v := t.authToken.Load(); v != nil {
		token = v.(string)
	}

	// Create wrapper stream
	s := &stream{
		stream:    quicStream,
		transport: t,
		opcode:    opcode,
		token:     token,
	}

	// Send initial request if provided
	if req != nil {
		if err := s.Send(req); err != nil {
			quicStream.Close()
			return nil, err
		}
	}

	return s, nil
}

// Conn returns the underlying QUIC connection.
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

	t.healthy.Store(false)

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.conn != nil {
		err := t.conn.CloseWithError(0, "transport closed")
		t.conn = nil
		if err != nil {
			return errors.Wrap(err, errors.CodeConnection, "quic.Close",
				"failed to close connection")
		}
	}

	return nil
}

// Healthy returns true if the transport is healthy.
func (t *Transport) Healthy() bool {
	if t.closed.Load() {
		return false
	}
	return t.healthy.Load()
}

// SetAuthToken sets the authentication token for future requests.
func (t *Transport) SetAuthToken(token string) {
	t.authToken.Store(token)
}

// stream implements transport.Stream for QUIC.
type stream struct {
	stream    *quic.Stream
	transport *Transport
	opcode    byte
	token     string
	closed    atomic.Bool
}

// Send sends a message on the stream.
func (s *stream) Send(msg interface{}) error {
	if s.closed.Load() {
		return errors.NewConnection("quic.stream.Send", "stream is closed")
	}

	// Encode payload
	payload, err := EncodePayload(msg)
	if err != nil {
		return err
	}

	// Build and write request
	req := &Request{
		Opcode:  s.opcode,
		Token:   s.token,
		Payload: payload,
	}

	return WriteRequest(s.stream, req)
}

// Recv receives a message from the stream.
func (s *stream) Recv() (interface{}, error) {
	if s.closed.Load() {
		return nil, errors.NewConnection("quic.stream.Recv", "stream is closed")
	}

	// Read response
	resp, err := ReadResponse(s.stream)
	if err != nil {
		return nil, err
	}

	// Check status
	if resp.Status != StatusOK {
		var errMsg string
		if len(resp.Payload) > 0 {
			errMsg = string(resp.Payload)
		} else {
			errMsg = "stream receive failed"
		}
		return nil, StatusToError(resp.Status, errMsg)
	}

	// Decode payload
	if len(resp.Payload) == 0 {
		return nil, nil
	}

	var result map[string]interface{}
	if err := DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// CloseSend closes the send side of the stream.
func (s *stream) CloseSend() error {
	return s.stream.Close()
}

// Close closes the stream entirely.
func (s *stream) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	s.stream.CancelRead(0)
	return s.stream.Close()
}

// Factory implements transport.Factory for QUIC.
type Factory struct {
	opts []Option
}

// NewFactory creates a new QUIC transport factory with the given options.
func NewFactory(opts ...Option) *Factory {
	return &Factory{opts: opts}
}

// Create creates a new QUIC transport.
func (f *Factory) Create(cfg *transport.Config) (transport.Transport, error) {
	return New(cfg, f.opts...)
}

// isRetryableError returns true if the error can be retried.
func isRetryableError(err error) bool {
	return errors.IsRetryable(err)
}

// minDuration returns the minimum of two durations.
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
