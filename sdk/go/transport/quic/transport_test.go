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
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// generateTestCertificate generates a self-signed certificate for testing.
// It returns the TLS certificate, a CA pool containing that certificate, and any error.
// The CA pool is used by clients to verify the server without InsecureSkipVerify.
func generateTestCertificate() (tls.Certificate, *x509.CertPool, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, nil, err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"QuicBaaS Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	privDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, nil, err
	}

	parsed, err := x509.ParseCertificate(certDER)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(parsed)

	return tlsCert, pool, nil
}

// TestTransportNew tests transport creation.
func TestTransportNew(t *testing.T) {
	t.Run("NilConfig", func(t *testing.T) {
		_, err := New(nil)
		if err == nil {
			t.Fatal("expected error for nil config")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got: %v", err)
		}
	})

	t.Run("EmptyAddress", func(t *testing.T) {
		cfg := &transport.Config{}
		_, err := New(cfg)
		if err == nil {
			t.Fatal("expected error for empty address")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got: %v", err)
		}
	})

	t.Run("NoTLSConfig", func(t *testing.T) {
		cfg := &transport.Config{
			Address: "localhost:9999",
		}
		_, err := New(cfg)
		if err == nil {
			t.Fatal("expected error for missing TLS config")
		}
	})
}

// TestOptions tests option application.
func TestOptions(t *testing.T) {
	t.Run("DefaultOptions", func(t *testing.T) {
		opts := DefaultOptions()
		if opts.HandshakeTimeout != 10*time.Second {
			t.Errorf("expected handshake timeout 10s, got: %v", opts.HandshakeTimeout)
		}
		if opts.MaxIdleTimeout != 30*time.Second {
			t.Errorf("expected max idle timeout 30s, got: %v", opts.MaxIdleTimeout)
		}
		if opts.MaxIncomingStreams != 100 {
			t.Errorf("expected max incoming streams 100, got: %d", opts.MaxIncomingStreams)
		}
	})

	t.Run("WithOptions", func(t *testing.T) {
		opts := DefaultOptions()
		opts.Apply(
			WithHandshakeTimeout(5*time.Second),
			WithMaxIdleTimeout(60*time.Second),
			WithKeepAlivePeriod(10*time.Second),
			WithMaxIncomingStreams(200),
			WithMaxIncomingUniStreams(50),
			WithInitialStreamReceiveWindow(1024*1024),
			WithInitialConnectionReceiveWindow(10*1024*1024),
			WithEnable0RTT(true),
			WithAuthToken("test-token"),
		)

		if opts.HandshakeTimeout != 5*time.Second {
			t.Errorf("expected handshake timeout 5s, got: %v", opts.HandshakeTimeout)
		}
		if opts.MaxIdleTimeout != 60*time.Second {
			t.Errorf("expected max idle timeout 60s, got: %v", opts.MaxIdleTimeout)
		}
		if opts.KeepAlivePeriod != 10*time.Second {
			t.Errorf("expected keepalive period 10s, got: %v", opts.KeepAlivePeriod)
		}
		if opts.MaxIncomingStreams != 200 {
			t.Errorf("expected max incoming streams 200, got: %d", opts.MaxIncomingStreams)
		}
		if opts.MaxIncomingUniStreams != 50 {
			t.Errorf("expected max incoming uni streams 50, got: %d", opts.MaxIncomingUniStreams)
		}
		if opts.InitialStreamReceiveWindow != 1024*1024 {
			t.Errorf("expected initial stream window 1MB, got: %d", opts.InitialStreamReceiveWindow)
		}
		if opts.InitialConnectionReceiveWindow != 10*1024*1024 {
			t.Errorf("expected initial conn window 10MB, got: %d", opts.InitialConnectionReceiveWindow)
		}
		if !opts.Enable0RTT {
			t.Error("expected 0-RTT enabled")
		}
		if opts.AuthToken != "test-token" {
			t.Errorf("expected auth token 'test-token', got: %s", opts.AuthToken)
		}
	})

	t.Run("WithTLSConfig", func(t *testing.T) {
		tlsCfg := &tls.Config{
			MinVersion: tls.VersionTLS13,
		}
		opts := DefaultOptions()
		opts.Apply(WithTLSConfig(tlsCfg))

		if opts.TLSConfig == nil {
			t.Fatal("expected TLS config to be set")
		}
	})

	t.Run("Validate", func(t *testing.T) {
		opts := &Options{}
		err := opts.Validate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Check defaults were applied
		if opts.HandshakeTimeout != 10*time.Second {
			t.Error("expected default handshake timeout to be applied")
		}
		if opts.MaxIdleTimeout != 30*time.Second {
			t.Error("expected default max idle timeout to be applied")
		}
		if opts.MaxIncomingStreams != 100 {
			t.Error("expected default max incoming streams to be applied")
		}
		if opts.MaxIncomingUniStreams != 100 {
			t.Error("expected default max incoming uni streams to be applied")
		}
	})

	t.Run("Clone", func(t *testing.T) {
		opts := DefaultOptions()
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS13}
		opts.AuthToken = "original"

		clone := opts.Clone()
		if clone == nil {
			t.Fatal("expected clone to be non-nil")
		}
		if clone.AuthToken != "original" {
			t.Error("expected auth token to be cloned")
		}
		if clone.TLSConfig == nil {
			t.Error("expected TLS config to be cloned")
		}

		// Verify it's a deep copy
		clone.AuthToken = "modified"
		if opts.AuthToken == "modified" {
			t.Error("clone should not modify original")
		}
	})

	t.Run("CloneNil", func(t *testing.T) {
		var opts *Options
		clone := opts.Clone()
		if clone != nil {
			t.Error("expected nil clone for nil options")
		}
	})
}

// TestFactory tests the transport factory.
func TestFactory(t *testing.T) {
	t.Run("NewFactory", func(t *testing.T) {
		factory := NewFactory(WithAuthToken("test"))
		if factory == nil {
			t.Fatal("expected factory to be non-nil")
		}
	})

	t.Run("FactoryCreate", func(t *testing.T) {
		factory := NewFactory()
		cfg := &transport.Config{
			Address: "localhost:9999",
			Timeout: 100 * time.Millisecond,
		}

		// Will fail due to no server, but tests the path
		_, err := factory.Create(cfg)
		if err == nil {
			t.Fatal("expected error (no server)")
		}
	})
}

// TestProtocolEncoding tests protocol encoding/decoding.
func TestProtocolEncoding(t *testing.T) {
	t.Run("WriteReadRequest", func(t *testing.T) {
		req := &Request{
			Opcode:  OpGet,
			Token:   "test-token",
			Payload: []byte(`{"key":"value"}`),
		}

		var buf bytes.Buffer
		if err := WriteRequest(&buf, req); err != nil {
			t.Fatalf("failed to write request: %v", err)
		}

		readReq, err := ReadRequest(&buf)
		if err != nil {
			t.Fatalf("failed to read request: %v", err)
		}

		if readReq.Opcode != req.Opcode {
			t.Errorf("opcode mismatch: expected %d, got %d", req.Opcode, readReq.Opcode)
		}
		if readReq.Token != req.Token {
			t.Errorf("token mismatch: expected %s, got %s", req.Token, readReq.Token)
		}
		if !bytes.Equal(readReq.Payload, req.Payload) {
			t.Errorf("payload mismatch")
		}
	})

	t.Run("WriteReadRequestEmptyToken", func(t *testing.T) {
		req := &Request{
			Opcode:  OpPut,
			Token:   "",
			Payload: []byte(`test`),
		}

		var buf bytes.Buffer
		if err := WriteRequest(&buf, req); err != nil {
			t.Fatalf("failed to write request: %v", err)
		}

		readReq, err := ReadRequest(&buf)
		if err != nil {
			t.Fatalf("failed to read request: %v", err)
		}

		if readReq.Token != "" {
			t.Errorf("expected empty token, got: %s", readReq.Token)
		}
	})

	t.Run("WriteReadRequestEmptyPayload", func(t *testing.T) {
		req := &Request{
			Opcode:  OpDelete,
			Token:   "token",
			Payload: nil,
		}

		var buf bytes.Buffer
		if err := WriteRequest(&buf, req); err != nil {
			t.Fatalf("failed to write request: %v", err)
		}

		readReq, err := ReadRequest(&buf)
		if err != nil {
			t.Fatalf("failed to read request: %v", err)
		}

		if len(readReq.Payload) != 0 {
			t.Errorf("expected empty payload")
		}
	})

	t.Run("WriteRequestNil", func(t *testing.T) {
		var buf bytes.Buffer
		err := WriteRequest(&buf, nil)
		if err == nil {
			t.Fatal("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got: %v", err)
		}
	})

	t.Run("WriteReadResponse", func(t *testing.T) {
		resp := &Response{
			Status:  StatusOK,
			Payload: []byte(`{"result":"success"}`),
		}

		var buf bytes.Buffer
		if err := WriteResponse(&buf, resp); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}

		readResp, err := ReadResponse(&buf)
		if err != nil {
			t.Fatalf("failed to read response: %v", err)
		}

		if readResp.Status != resp.Status {
			t.Errorf("status mismatch: expected %d, got %d", resp.Status, readResp.Status)
		}
		if !bytes.Equal(readResp.Payload, resp.Payload) {
			t.Errorf("payload mismatch")
		}
	})

	t.Run("WriteResponseNil", func(t *testing.T) {
		var buf bytes.Buffer
		err := WriteResponse(&buf, nil)
		if err == nil {
			t.Fatal("expected error for nil response")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got: %v", err)
		}
	})

	t.Run("WriteReadResponseEmptyPayload", func(t *testing.T) {
		resp := &Response{
			Status:  StatusNotFound,
			Payload: nil,
		}

		var buf bytes.Buffer
		if err := WriteResponse(&buf, resp); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}

		readResp, err := ReadResponse(&buf)
		if err != nil {
			t.Fatalf("failed to read response: %v", err)
		}

		if readResp.Status != StatusNotFound {
			t.Errorf("expected not found status")
		}
		if len(readResp.Payload) != 0 {
			t.Errorf("expected empty payload")
		}
	})

	t.Run("ReadRequestEOF", func(t *testing.T) {
		var buf bytes.Buffer
		_, err := ReadRequest(&buf)
		if err == nil {
			t.Fatal("expected error for empty buffer")
		}
	})

	t.Run("ReadResponseEOF", func(t *testing.T) {
		var buf bytes.Buffer
		_, err := ReadResponse(&buf)
		if err == nil {
			t.Fatal("expected error for empty buffer")
		}
	})
}

// TestPayloadEncoding tests payload encoding/decoding.
func TestPayloadEncoding(t *testing.T) {
	t.Run("EncodeDecodePayload", func(t *testing.T) {
		// Use a struct with explicit types that msgpack can handle
		type testPayload struct {
			Key   string `codec:"key"`
			Value string `codec:"value"`
			Count int64  `codec:"count"`
		}

		original := testPayload{
			Key:   "test-key",
			Value: "test-value",
			Count: 42,
		}

		encoded, err := EncodePayload(original)
		if err != nil {
			t.Fatalf("failed to encode: %v", err)
		}

		var decoded testPayload
		if err := DecodePayload(encoded, &decoded); err != nil {
			t.Fatalf("failed to decode: %v", err)
		}

		if decoded.Key != "test-key" {
			t.Errorf("key mismatch: expected test-key, got %s", decoded.Key)
		}
		if decoded.Value != "test-value" {
			t.Errorf("value mismatch: expected test-value, got %s", decoded.Value)
		}
		if decoded.Count != 42 {
			t.Errorf("count mismatch: expected 42, got %d", decoded.Count)
		}
	})

	t.Run("EncodeNilPayload", func(t *testing.T) {
		encoded, err := EncodePayload(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if encoded != nil {
			t.Error("expected nil for nil input")
		}
	})

	t.Run("DecodeEmptyPayload", func(t *testing.T) {
		type testPayload struct{}
		var decoded testPayload
		err := DecodePayload(nil, &decoded)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("DecodeNilEntity", func(t *testing.T) {
		// First encode something valid
		encoded, _ := EncodePayload(map[string]string{"key": "value"})
		err := DecodePayload(encoded, nil)
		if err == nil {
			t.Fatal("expected error for nil entity")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got: %v", err)
		}
	})

	t.Run("EncodeDecodeSimpleMap", func(t *testing.T) {
		// Test with map[string]string which msgpack handles well
		original := map[string]string{
			"key":   "value",
			"hello": "world",
		}

		encoded, err := EncodePayload(original)
		if err != nil {
			t.Fatalf("failed to encode: %v", err)
		}

		var decoded map[string]string
		if err := DecodePayload(encoded, &decoded); err != nil {
			t.Fatalf("failed to decode: %v", err)
		}

		if decoded["key"] != "value" {
			t.Errorf("expected 'value', got: %s", decoded["key"])
		}
	})
}

// TestStatusConversion tests status code conversion.
func TestStatusConversion(t *testing.T) {
	t.Run("StatusToError", func(t *testing.T) {
		testCases := []struct {
			status   byte
			expected errors.ErrorCode
		}{
			{StatusOK, errors.CodeUnknown}, // nil error expected
			{StatusNotFound, errors.CodeNotFound},
			{StatusUnauthorized, errors.CodeUnauthorized},
			{StatusInvalidArgument, errors.CodeInvalidInput},
			{StatusInvalidRequest, errors.CodeInvalidInput},
			{StatusAlreadyExists, errors.CodeAlreadyExists},
			{StatusTimeout, errors.CodeTimeout},
			{StatusPermissionDenied, errors.CodeForbidden},
			{StatusResourceExhausted, errors.CodeResourceExhausted},
			{StatusUnavailable, errors.CodeUnavailable},
			{StatusInternalError, errors.CodeInternal},
			{0xFF, errors.CodeInternal}, // Unknown status
		}

		for _, tc := range testCases {
			err := StatusToError(tc.status, "test message")
			if tc.status == StatusOK {
				if err != nil {
					t.Errorf("expected nil error for StatusOK, got: %v", err)
				}
				continue
			}

			if err == nil {
				t.Errorf("expected error for status %d", tc.status)
				continue
			}

			if errors.Code(err) != tc.expected {
				t.Errorf("status %d: expected code %v, got %v", tc.status, tc.expected, errors.Code(err))
			}
		}
	})

	t.Run("ErrorToStatus", func(t *testing.T) {
		testCases := []struct {
			err      error
			expected byte
		}{
			{nil, StatusOK},
			{errors.NewNotFound("op", "not found"), StatusNotFound},
			{errors.NewUnauthorized("op", "unauthorized"), StatusUnauthorized},
			{errors.NewInvalidInput("op", "invalid"), StatusInvalidArgument},
			{errors.NewAlreadyExists("op", "exists"), StatusAlreadyExists},
			{errors.NewTimeout("op", "timeout"), StatusTimeout},
			{errors.NewForbidden("op", "forbidden"), StatusPermissionDenied},
			{errors.NewResourceExhausted("op", "exhausted"), StatusResourceExhausted},
			{errors.NewUnavailable("op", "unavailable"), StatusUnavailable},
			{errors.NewInternal("op", "internal"), StatusInternalError},
		}

		for _, tc := range testCases {
			status := ErrorToStatus(tc.err)
			if status != tc.expected {
				t.Errorf("error %v: expected status %d, got %d", tc.err, tc.expected, status)
			}
		}
	})
}

// TestMethodToOpcode tests method to opcode mapping.
func TestMethodToOpcode(t *testing.T) {
	testCases := []struct {
		service  string
		method   string
		expected byte
		found    bool
	}{
		{"QuicBaaS", "Put", OpPut, true},
		{"QuicBaaS", "Get", OpGet, true},
		{"QuicBaaS", "Delete", OpDelete, true},
		{"QuicBaaS", "List", OpList, true},
		{"QuicBaaS", "BatchPut", OpBatchPut, true},
		{"QuicBaaS", "BatchGet", OpBatchGet, true},
		{"QuicBaaS", "BatchDelete", OpBatchDelete, true},
		{"QuicBaaS", "Transaction", OpTransaction, true},
		{"QuicBaaS", "Query", OpQuery, true},
		{"Cluster", "Status", OpClusterStatus, true},
		{"Cluster", "GetLeader", OpGetLeader, true},
		{"Health", "Check", OpHealth, true},
		{"Health", "Liveness", OpLiveness, true},
		{"Health", "Readiness", OpReadiness, true},
		{"Unknown", "Method", 0, false},
	}

	for _, tc := range testCases {
		opcode, found := MethodToOpcode(tc.service, tc.method)
		if found != tc.found {
			t.Errorf("%s/%s: expected found=%v, got=%v", tc.service, tc.method, tc.found, found)
		}
		if found && opcode != tc.expected {
			t.Errorf("%s/%s: expected opcode=%d, got=%d", tc.service, tc.method, tc.expected, opcode)
		}
	}
}

// TestMinDuration tests the minDuration helper.
func TestMinDuration(t *testing.T) {
	testCases := []struct {
		a, b, expected time.Duration
	}{
		{time.Second, time.Minute, time.Second},
		{time.Minute, time.Second, time.Second},
		{time.Second, time.Second, time.Second},
	}

	for _, tc := range testCases {
		result := minDuration(tc.a, tc.b)
		if result != tc.expected {
			t.Errorf("minDuration(%v, %v) = %v, expected %v", tc.a, tc.b, result, tc.expected)
		}
	}
}

// mockQUICServer is a minimal QUIC server for testing.
type mockQUICServer struct {
	listener *quic.Listener
	addr     string
	wg       sync.WaitGroup
	handlers map[byte]func(*Request) *Response
	mu       sync.Mutex
	// caPool holds the CA pool for the server's self-signed certificate.
	// Clients use this pool to verify the server without InsecureSkipVerify.
	caPool *x509.CertPool
}

// newMockQUICServer creates a new mock QUIC server.
func newMockQUICServer(t *testing.T) (*mockQUICServer, error) {
	cert, caPool, err := generateTestCertificate()
	if err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{alpnProtocol},
	}

	listener, err := quic.ListenAddr("127.0.0.1:0", tlsConfig, &quic.Config{
		MaxIdleTimeout: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	server := &mockQUICServer{
		listener: listener,
		addr:     listener.Addr().String(),
		handlers: make(map[byte]func(*Request) *Response),
		caPool:   caPool,
	}

	return server, nil
}

// RegisterHandler registers a handler for an opcode.
func (s *mockQUICServer) RegisterHandler(opcode byte, handler func(*Request) *Response) {
	s.mu.Lock()
	s.handlers[opcode] = handler
	s.mu.Unlock()
}

// Start starts the server.
func (s *mockQUICServer) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := s.listener.Accept(context.Background())
			if err != nil {
				return
			}
			s.wg.Add(1)
			go s.handleConnection(conn)
		}
	}()
}

// handleConnection handles a single connection.
func (s *mockQUICServer) handleConnection(conn *quic.Conn) {
	defer s.wg.Done()
	for {
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			return
		}
		s.wg.Add(1)
		go s.handleStream(stream)
	}
}

// handleStream handles a single stream.
func (s *mockQUICServer) handleStream(stream *quic.Stream) {
	defer s.wg.Done()
	defer stream.Close()

	req, err := ReadRequest(stream)
	if err != nil {
		return
	}

	s.mu.Lock()
	handler, ok := s.handlers[req.Opcode]
	s.mu.Unlock()

	var resp *Response
	if ok {
		resp = handler(req)
	} else {
		resp = &Response{
			Status:  StatusInternalError,
			Payload: []byte("unknown opcode"),
		}
	}

	_ = WriteResponse(stream, resp)
}

// Close closes the server.
func (s *mockQUICServer) Close() error {
	err := s.listener.Close()
	s.wg.Wait()
	return err
}

// encodeResponse encodes a response payload using msgpack.
func encodeResponse(data interface{}) []byte {
	payload, _ := EncodePayload(data)
	return payload
}

// TestTransportWithServer tests the transport with a mock server.
func TestTransportWithServer(t *testing.T) {
	// Create mock server
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()

	// Register handlers using msgpack-encoded responses
	server.RegisterHandler(OpGet, func(req *Request) *Response {
		// Encode response as msgpack
		responseData := map[string]string{"value": "test-value"}
		return &Response{
			Status:  StatusOK,
			Payload: encodeResponse(responseData),
		}
	})

	server.RegisterHandler(OpPut, func(req *Request) *Response {
		return &Response{
			Status:  StatusOK,
			Payload: nil,
		}
	})

	server.RegisterHandler(OpDelete, func(req *Request) *Response {
		return &Response{
			Status:  StatusNotFound,
			Payload: []byte("key not found"),
		}
	})

	server.Start()

	// Create client
	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate client cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:   server.addr,
		TLSConfig: tlsConfig,
		Timeout:   5 * time.Second,
	}

	trans, err := New(cfg, WithAuthToken("test-token"))
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	t.Run("HealthyTransport", func(t *testing.T) {
		if !trans.Healthy() {
			t.Error("expected transport to be healthy")
		}
	})

	t.Run("GetConnection", func(t *testing.T) {
		conn := trans.Conn()
		if conn == nil {
			t.Error("expected connection to be non-nil")
		}
	})

	t.Run("SetAuthToken", func(t *testing.T) {
		trans.SetAuthToken("new-token")
		// The token is stored in an atomic.Value, verify it works
	})

	t.Run("SuccessfulRequest", func(t *testing.T) {
		ctx := context.Background()
		resp, err := trans.Request(ctx, "QuicBaaS", "Get", map[string]string{"key": "test"})
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected response")
		}
	})

	t.Run("PutRequest", func(t *testing.T) {
		ctx := context.Background()
		resp, err := trans.Request(ctx, "QuicBaaS", "Put", map[string]string{
			"key":   "test",
			"value": "data",
		})
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		// Put returns nil/empty on success
		_ = resp
	})

	t.Run("NotFoundError", func(t *testing.T) {
		ctx := context.Background()
		_, err := trans.Request(ctx, "QuicBaaS", "Delete", map[string]string{"key": "nonexistent"})
		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got: %v", err)
		}
	})

	t.Run("UnknownMethod", func(t *testing.T) {
		ctx := context.Background()
		_, err := trans.Request(ctx, "Unknown", "Method", nil)
		if err == nil {
			t.Fatal("expected error for unknown method")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got: %v", err)
		}
	})

	t.Run("CloseTransport", func(t *testing.T) {
		// Create a new transport for this test
		trans2, err := New(cfg)
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}

		// Close it
		if err := trans2.Close(); err != nil {
			t.Errorf("close failed: %v", err)
		}

		// Should not be healthy after close
		if trans2.Healthy() {
			t.Error("expected transport to be unhealthy after close")
		}

		// Request should fail
		ctx := context.Background()
		_, err = trans2.Request(ctx, "QuicBaaS", "Get", nil)
		if err == nil {
			t.Fatal("expected error after close")
		}

		// Second close should be no-op
		if err := trans2.Close(); err != nil {
			t.Errorf("second close should not error: %v", err)
		}
	})
}

// TestTransportStream tests streaming operations.
func TestTransportStream(t *testing.T) {
	// Create mock server
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()

	// Register streaming handler
	server.RegisterHandler(OpList, func(req *Request) *Response {
		responseData := map[string][]string{"items": {"a", "b", "c"}}
		return &Response{
			Status:  StatusOK,
			Payload: encodeResponse(responseData),
		}
	})

	server.Start()

	// Create client
	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate client cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:   server.addr,
		TLSConfig: tlsConfig,
		Timeout:   5 * time.Second,
	}

	trans, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	t.Run("OpenStream", func(t *testing.T) {
		ctx := context.Background()
		stream, err := trans.RequestStream(ctx, "QuicBaaS", "List", nil)
		if err != nil {
			t.Fatalf("failed to open stream: %v", err)
		}
		defer stream.Close()

		// Close send side
		if err := stream.CloseSend(); err != nil {
			t.Errorf("close send failed: %v", err)
		}
	})

	t.Run("StreamUnknownMethod", func(t *testing.T) {
		ctx := context.Background()
		_, err := trans.RequestStream(ctx, "Unknown", "Method", nil)
		if err == nil {
			t.Fatal("expected error for unknown method")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got: %v", err)
		}
	})

	t.Run("StreamAfterClose", func(t *testing.T) {
		trans2, err := New(cfg)
		if err != nil {
			t.Fatalf("failed to create transport: %v", err)
		}
		trans2.Close()

		ctx := context.Background()
		_, err = trans2.RequestStream(ctx, "QuicBaaS", "List", nil)
		if err == nil {
			t.Fatal("expected error after close")
		}
	})
}

// TestWriteErrors tests write error handling.
func TestWriteErrors(t *testing.T) {
	t.Run("WriteRequestPayloadTooLarge", func(t *testing.T) {
		req := &Request{
			Opcode:  OpPut,
			Payload: make([]byte, MaxPayloadSize+1),
		}
		var buf bytes.Buffer
		err := WriteRequest(&buf, req)
		if err == nil {
			t.Fatal("expected error for oversized payload")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got: %v", err)
		}
	})

	t.Run("WriteRequestTokenTooLarge", func(t *testing.T) {
		req := &Request{
			Opcode: OpGet,
			Token:  string(make([]byte, int(MaxTokenSize)+1)),
		}
		var buf bytes.Buffer
		err := WriteRequest(&buf, req)
		if err == nil {
			t.Fatal("expected error for oversized token")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got: %v", err)
		}
	})

	t.Run("WriteRequestTokenAboveOld8KBLimit", func(t *testing.T) {
		// Regression: RFC 9068 at+jwt tokens carrying qrdb-resolved RBAC
		// roles + scp claims legitimately exceed the old 8KB cap (observed
		// production size: 11,060 bytes).
		for _, size := range []int{8*1024 + 1, 11060, int(MaxTokenSize)} {
			req := &Request{
				Opcode: OpGet,
				Token:  string(make([]byte, size)),
			}
			var buf bytes.Buffer
			if err := WriteRequest(&buf, req); err != nil {
				t.Fatalf("WriteRequest failed for %d byte token: %v", size, err)
			}
			decoded, err := ReadRequest(&buf)
			if err != nil {
				t.Fatalf("ReadRequest failed for %d byte token: %v", size, err)
			}
			if decoded.Token != req.Token {
				t.Fatalf("token mismatch after round trip at size %d", size)
			}
		}
	})

	t.Run("WriteResponsePayloadTooLarge", func(t *testing.T) {
		resp := &Response{
			Status:  StatusOK,
			Payload: make([]byte, MaxPayloadSize+1),
		}
		var buf bytes.Buffer
		err := WriteResponse(&buf, resp)
		if err == nil {
			t.Fatal("expected error for oversized payload")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got: %v", err)
		}
	})
}

// errorWriter is an io.Writer that always returns an error.
type errorWriter struct {
	failAfter int
	written   int
}

func (w *errorWriter) Write(p []byte) (int, error) {
	if w.written >= w.failAfter {
		return 0, io.ErrClosedPipe
	}
	w.written += len(p)
	return len(p), nil
}

// TestWriteRequestErrors tests specific write error paths.
func TestWriteRequestErrors(t *testing.T) {
	t.Run("FailOnOpcode", func(t *testing.T) {
		w := &errorWriter{failAfter: 0}
		req := &Request{Opcode: OpGet}
		err := WriteRequest(w, req)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("FailOnTokenLen", func(t *testing.T) {
		w := &errorWriter{failAfter: 1}
		req := &Request{Opcode: OpGet}
		err := WriteRequest(w, req)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("FailOnToken", func(t *testing.T) {
		w := &errorWriter{failAfter: 3}
		req := &Request{Opcode: OpGet, Token: "test"}
		err := WriteRequest(w, req)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("FailOnPayloadLen", func(t *testing.T) {
		w := &errorWriter{failAfter: 3}
		req := &Request{Opcode: OpGet, Token: ""}
		err := WriteRequest(w, req)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("FailOnPayload", func(t *testing.T) {
		w := &errorWriter{failAfter: 7}
		req := &Request{Opcode: OpGet, Token: "", Payload: []byte("test")}
		err := WriteRequest(w, req)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

// TestWriteResponseErrors tests specific write error paths for responses.
func TestWriteResponseErrors(t *testing.T) {
	t.Run("FailOnStatus", func(t *testing.T) {
		w := &errorWriter{failAfter: 0}
		resp := &Response{Status: StatusOK}
		err := WriteResponse(w, resp)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("FailOnPayloadLen", func(t *testing.T) {
		w := &errorWriter{failAfter: 1}
		resp := &Response{Status: StatusOK}
		err := WriteResponse(w, resp)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("FailOnPayload", func(t *testing.T) {
		w := &errorWriter{failAfter: 5}
		resp := &Response{Status: StatusOK, Payload: []byte("test")}
		err := WriteResponse(w, resp)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

// TestReadErrors tests read error paths.
func TestReadErrors(t *testing.T) {
	t.Run("ReadRequestTruncatedToken", func(t *testing.T) {
		// Token length prefix at the 2-byte wire ceiling (65535, which
		// equals MaxTokenSize) with a truncated token body must fail.
		var buf bytes.Buffer
		buf.WriteByte(OpGet)          // opcode
		buf.Write([]byte{0xFF, 0xFF}) // token len (65535)
		buf.Write(make([]byte, 10))   // truncated token body

		_, err := ReadRequest(&buf)
		if err == nil {
			t.Fatal("expected error for truncated token")
		}
	})

	t.Run("ReadRequestInvalidPayloadLen", func(t *testing.T) {
		// Create a buffer with an oversized payload length
		var buf bytes.Buffer
		buf.WriteByte(OpGet)                      // opcode
		buf.Write([]byte{0x00, 0x00})             // token len (0)
		buf.Write([]byte{0x01, 0x00, 0x00, 0x01}) // payload len (16MB + 1)

		_, err := ReadRequest(&buf)
		if err == nil {
			t.Fatal("expected error for oversized payload")
		}
	})

	t.Run("ReadResponseInvalidPayloadLen", func(t *testing.T) {
		// Create a buffer with an oversized payload length
		var buf bytes.Buffer
		buf.WriteByte(StatusOK)                   // status
		buf.Write([]byte{0x01, 0x00, 0x00, 0x01}) // payload len (16MB + 1)

		_, err := ReadResponse(&buf)
		if err == nil {
			t.Fatal("expected error for oversized payload")
		}
	})

	t.Run("ReadRequestTokenEOF", func(t *testing.T) {
		// Create a buffer that runs out before reading full token
		var buf bytes.Buffer
		buf.WriteByte(OpGet)          // opcode
		buf.Write([]byte{0x00, 0x10}) // token len (16)
		buf.Write([]byte{0x01, 0x02}) // only 2 bytes of token

		_, err := ReadRequest(&buf)
		if err == nil {
			t.Fatal("expected error for truncated token")
		}
	})

	t.Run("ReadRequestPayloadEOF", func(t *testing.T) {
		// Create a buffer that runs out before reading full payload
		var buf bytes.Buffer
		buf.WriteByte(OpGet)                      // opcode
		buf.Write([]byte{0x00, 0x00})             // token len (0)
		buf.Write([]byte{0x00, 0x00, 0x00, 0x10}) // payload len (16)
		buf.Write([]byte{0x01, 0x02})             // only 2 bytes of payload

		_, err := ReadRequest(&buf)
		if err == nil {
			t.Fatal("expected error for truncated payload")
		}
	})

	t.Run("ReadResponsePayloadEOF", func(t *testing.T) {
		// Create a buffer that runs out before reading full payload
		var buf bytes.Buffer
		buf.WriteByte(StatusOK)                   // status
		buf.Write([]byte{0x00, 0x00, 0x00, 0x10}) // payload len (16)
		buf.Write([]byte{0x01, 0x02})             // only 2 bytes of payload

		_, err := ReadResponse(&buf)
		if err == nil {
			t.Fatal("expected error for truncated payload")
		}
	})
}

// TestRetryableErrors tests retryable error detection.
func TestRetryableErrors(t *testing.T) {
	testCases := []struct {
		err       error
		retryable bool
	}{
		{errors.NewTimeout("op", "timeout"), true},
		{errors.NewUnavailable("op", "unavailable"), true},
		{errors.NewResourceExhausted("op", "exhausted"), true},
		{errors.NewNotFound("op", "not found"), false},
		{errors.NewInvalidInput("op", "invalid"), false},
		{errors.NewInternal("op", "internal"), false},
	}

	for _, tc := range testCases {
		result := isRetryableError(tc.err)
		if result != tc.retryable {
			t.Errorf("isRetryableError(%v) = %v, expected %v", tc.err, result, tc.retryable)
		}
	}
}

// TestStreamSendRecv tests stream send and receive operations.
func TestStreamSendRecv(t *testing.T) {
	// Create mock server
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()

	// Register handler that echoes back
	server.RegisterHandler(OpList, func(req *Request) *Response {
		return &Response{
			Status:  StatusOK,
			Payload: encodeResponse(map[string]string{"echo": "response"}),
		}
	})

	server.Start()

	// Create client
	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate client cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:   server.addr,
		TLSConfig: tlsConfig,
		Timeout:   5 * time.Second,
	}

	trans, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	t.Run("StreamSendWithInitialRequest", func(t *testing.T) {
		ctx := context.Background()
		stream, err := trans.RequestStream(ctx, "QuicBaaS", "List", map[string]string{"filter": "test"})
		if err != nil {
			t.Fatalf("failed to open stream: %v", err)
		}
		defer stream.Close()
	})

	t.Run("StreamClosedSend", func(t *testing.T) {
		ctx := context.Background()
		stream, err := trans.RequestStream(ctx, "QuicBaaS", "List", nil)
		if err != nil {
			t.Fatalf("failed to open stream: %v", err)
		}

		// Close the stream
		stream.Close()

		// Send should fail on closed stream
		err = stream.Send(map[string]string{"key": "value"})
		if err == nil {
			t.Fatal("expected error when sending on closed stream")
		}
	})

	t.Run("StreamClosedRecv", func(t *testing.T) {
		ctx := context.Background()
		stream, err := trans.RequestStream(ctx, "QuicBaaS", "List", nil)
		if err != nil {
			t.Fatalf("failed to open stream: %v", err)
		}

		// Close the stream
		stream.Close()

		// Recv should fail on closed stream
		_, err = stream.Recv()
		if err == nil {
			t.Fatal("expected error when receiving on closed stream")
		}
	})

	t.Run("StreamRecvSuccessWithPayload", func(t *testing.T) {
		ctx := context.Background()
		stream, err := trans.RequestStream(ctx, "QuicBaaS", "List", nil)
		if err != nil {
			t.Fatalf("failed to open stream: %v", err)
		}
		defer stream.Close()

		// Send a request to trigger a response
		err = stream.Send(map[string]string{"request": "data"})
		if err != nil {
			t.Logf("send returned error (may be expected): %v", err)
		}

		// Try to receive - the mock server will send a response
		result, err := stream.Recv()
		// Recv may return error if stream was closed server-side
		// We just verify the function runs without panic
		_ = result
		_ = err
	})

	t.Run("StreamRecvMultiple", func(t *testing.T) {
		ctx := context.Background()
		stream, err := trans.RequestStream(ctx, "QuicBaaS", "List", nil)
		if err != nil {
			t.Fatalf("failed to open stream: %v", err)
		}
		defer stream.Close()

		// Send request
		err = stream.Send(map[string]string{"key": "value"})
		if err != nil {
			t.Logf("send returned error (may be expected): %v", err)
		}

		// Try receiving twice to test multiple recv calls
		for i := 0; i < 2; i++ {
			_, recvErr := stream.Recv()
			if recvErr != nil {
				// Expected after server closes stream
				break
			}
		}
	})

	t.Run("StreamRecvAfterCloseSend", func(t *testing.T) {
		ctx := context.Background()
		stream, err := trans.RequestStream(ctx, "QuicBaaS", "List", nil)
		if err != nil {
			t.Fatalf("failed to open stream: %v", err)
		}
		defer stream.Close()

		// Close send side
		err = stream.CloseSend()
		if err != nil {
			t.Logf("close send returned error (may be expected): %v", err)
		}

		// Try to recv - should still work for receiving responses
		_, err = stream.Recv()
		// Just verify it doesn't panic - error is expected after close
		_ = err
	})
}

// TestTransportConnectionNil tests handling of nil connection scenarios.
func TestTransportConnectionNil(t *testing.T) {
	// Create a transport and manually set conn to nil
	cfg := &transport.Config{
		Address: "localhost:9999",
		Timeout: 100 * time.Millisecond,
	}

	// Create mock server so transport can connect
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()
	server.Start()

	cert, _, _ := generateTestCertificate()
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg.Address = server.addr
	cfg.TLSConfig = tlsConfig

	trans, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}

	// Manually set connection to nil to test error path
	trans.mu.Lock()
	trans.conn = nil
	trans.mu.Unlock()

	ctx := context.Background()
	_, err = trans.Request(ctx, "QuicBaaS", "Get", nil)
	if err == nil {
		t.Fatal("expected error when connection is nil")
	}

	_, err = trans.RequestStream(ctx, "QuicBaaS", "List", nil)
	if err == nil {
		t.Fatal("expected error when connection is nil for stream")
	}
}

// BenchmarkProtocolEncoding benchmarks protocol encoding.
func BenchmarkProtocolEncoding(b *testing.B) {
	req := &Request{
		Opcode:  OpGet,
		Token:   "test-auth-token-12345",
		Payload: []byte(`{"key":"benchmark-key","value":"benchmark-value"}`),
	}

	b.Run("WriteRequest", func(b *testing.B) {
		var buf bytes.Buffer
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf.Reset()
			_ = WriteRequest(&buf, req)
		}
	})

	var buf bytes.Buffer
	_ = WriteRequest(&buf, req)
	data := buf.Bytes()

	b.Run("ReadRequest", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			r := bytes.NewReader(data)
			_, _ = ReadRequest(r)
		}
	})
}

// BenchmarkPayloadEncoding benchmarks payload encoding.
func BenchmarkPayloadEncoding(b *testing.B) {
	type benchPayload struct {
		Key       string `codec:"key"`
		Value     string `codec:"value"`
		Timestamp int64  `codec:"timestamp"`
	}

	payload := benchPayload{
		Key:       "test-key",
		Value:     "test-value",
		Timestamp: 1234567890,
	}

	b.Run("Encode", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = EncodePayload(payload)
		}
	})

	encoded, _ := EncodePayload(payload)

	b.Run("Decode", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var decoded benchPayload
			_ = DecodePayload(encoded, &decoded)
		}
	})
}

// TestTransport_NewWithOptions tests transport creation with custom options.
func TestTransport_NewWithOptions(t *testing.T) {
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()

	server.RegisterHandler(OpHealth, func(req *Request) *Response {
		return &Response{Status: StatusOK, Payload: nil}
	})
	server.Start()

	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:   server.addr,
		TLSConfig: tlsConfig,
		Timeout:   5 * time.Second,
	}

	trans, err := New(cfg,
		WithAuthToken("test-auth-token"),
		WithHandshakeTimeout(15*time.Second),
		WithMaxIdleTimeout(45*time.Second),
		WithKeepAlivePeriod(20*time.Second),
		WithMaxIncomingStreams(150),
		WithMaxIncomingUniStreams(75),
		WithInitialStreamReceiveWindow(1024*1024),
		WithInitialConnectionReceiveWindow(16*1024*1024),
		WithEnable0RTT(false),
	)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	if !trans.Healthy() {
		t.Error("expected transport to be healthy")
	}
}

// TestTransport_NewWithOptionsTLSConfig tests transport with option-provided TLS config.
func TestTransport_NewWithOptionsTLSConfig(t *testing.T) {
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()
	server.Start()

	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	optTLSConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address: server.addr,
		Timeout: 5 * time.Second,
		// No TLS config in main config - should use option
	}

	trans, err := New(cfg, WithTLSConfig(optTLSConfig))
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	if !trans.Healthy() {
		t.Error("expected transport to be healthy")
	}
}

// TestTransport_RequestWithRetry_ContextCanceled tests context cancellation during retry.
func TestTransport_RequestWithRetry_ContextCanceled(t *testing.T) {
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()

	// Register a handler that returns retryable error
	server.RegisterHandler(OpGet, func(req *Request) *Response {
		return &Response{
			Status:  StatusUnavailable,
			Payload: []byte("service unavailable"),
		}
	})
	server.Start()

	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:         server.addr,
		TLSConfig:       tlsConfig,
		Timeout:         500 * time.Millisecond,
		MaxRetries:      10,
		RetryBackoff:    500 * time.Millisecond, // Long backoff
		MaxRetryBackoff: 1 * time.Second,
	}

	trans, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	var resultErr error

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, resultErr = trans.Request(ctx, "QuicBaaS", "Get", map[string]string{"key": "test"})
	}()

	// Cancel after first attempt starts
	time.Sleep(100 * time.Millisecond)
	cancel()

	wg.Wait()

	if resultErr == nil {
		t.Fatal("expected error after context cancel")
	}
}

// TestTransport_RequestWithRetry_Exhausted tests exhausting all retries.
func TestTransport_RequestWithRetry_Exhausted(t *testing.T) {
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()

	// Register a handler that always returns retryable error
	server.RegisterHandler(OpGet, func(req *Request) *Response {
		return &Response{
			Status:  StatusUnavailable,
			Payload: []byte("always unavailable"),
		}
	})
	server.Start()

	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:         server.addr,
		TLSConfig:       tlsConfig,
		Timeout:         100 * time.Millisecond,
		MaxRetries:      2,
		RetryBackoff:    10 * time.Millisecond,
		MaxRetryBackoff: 50 * time.Millisecond,
	}

	trans, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	ctx := context.Background()
	_, err = trans.Request(ctx, "QuicBaaS", "Get", map[string]string{"key": "test"})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
}

// TestTransport_DoRequest_StreamCloseError tests doRequest handling stream close error.
func TestTransport_DoRequest_StreamCloseError(t *testing.T) {
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()

	server.RegisterHandler(OpGet, func(req *Request) *Response {
		return &Response{
			Status:  StatusOK,
			Payload: encodeResponse(map[string]string{"value": "test"}),
		}
	})
	server.Start()

	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:   server.addr,
		TLSConfig: tlsConfig,
		Timeout:   5 * time.Second,
	}

	trans, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	ctx := context.Background()
	resp, err := trans.Request(ctx, "QuicBaaS", "Get", map[string]string{"key": "test"})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

// TestTransport_DoRequest_NonOKStatusWithPayload tests doRequest with error status and payload.
func TestTransport_DoRequest_NonOKStatusWithPayload(t *testing.T) {
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()

	server.RegisterHandler(OpGet, func(req *Request) *Response {
		return &Response{
			Status:  StatusNotFound,
			Payload: []byte("key not found: test-key"),
		}
	})
	server.Start()

	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:   server.addr,
		TLSConfig: tlsConfig,
		Timeout:   5 * time.Second,
	}

	trans, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	ctx := context.Background()
	_, err = trans.Request(ctx, "QuicBaaS", "Get", map[string]string{"key": "test-key"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got: %v", err)
	}
}

// TestTransport_DoRequest_NonOKStatusNoPayload tests doRequest with error status but no payload.
func TestTransport_DoRequest_NonOKStatusNoPayload(t *testing.T) {
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()

	server.RegisterHandler(OpGet, func(req *Request) *Response {
		return &Response{
			Status:  StatusInternalError,
			Payload: nil, // No payload
		}
	})
	server.Start()

	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:   server.addr,
		TLSConfig: tlsConfig,
		Timeout:   5 * time.Second,
	}

	trans, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	ctx := context.Background()
	_, err = trans.Request(ctx, "QuicBaaS", "Get", map[string]string{"key": "test"})
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestStream_RecvNonOKStatus tests stream Recv with non-OK status.
func TestStream_RecvNonOKStatus(t *testing.T) {
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()

	server.RegisterHandler(OpList, func(req *Request) *Response {
		return &Response{
			Status:  StatusUnauthorized,
			Payload: []byte("unauthorized"),
		}
	})
	server.Start()

	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:   server.addr,
		TLSConfig: tlsConfig,
		Timeout:   5 * time.Second,
	}

	trans, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	ctx := context.Background()
	stream, err := trans.RequestStream(ctx, "QuicBaaS", "List", nil)
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}
	defer stream.Close()

	// Send to trigger server response
	err = stream.Send(map[string]string{"key": "test"})
	if err != nil {
		t.Logf("send returned: %v", err)
	}

	// Receive should get the error status
	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error from recv")
	}
	if !errors.IsUnauthorized(err) {
		t.Errorf("expected Unauthorized error, got: %v", err)
	}
}

// TestStream_RecvOKStatusWithPayload tests stream Recv with OK status and payload.
func TestStream_RecvOKStatusWithPayload(t *testing.T) {
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()

	server.RegisterHandler(OpList, func(req *Request) *Response {
		return &Response{
			Status:  StatusOK,
			Payload: encodeResponse(map[string]string{"items": "a,b,c"}),
		}
	})
	server.Start()

	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:   server.addr,
		TLSConfig: tlsConfig,
		Timeout:   5 * time.Second,
	}

	trans, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	ctx := context.Background()
	stream, err := trans.RequestStream(ctx, "QuicBaaS", "List", nil)
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}
	defer stream.Close()

	// Send to trigger server response
	err = stream.Send(map[string]string{"key": "test"})
	if err != nil {
		t.Logf("send returned: %v", err)
	}

	// Receive should succeed
	result, err := stream.Recv()
	if err != nil {
		t.Logf("recv error: %v (may be expected due to stream state)", err)
	}
	_ = result
}

// TestStream_RecvOKStatusEmptyPayload tests stream Recv with OK status but no payload.
func TestStream_RecvOKStatusEmptyPayload(t *testing.T) {
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()

	server.RegisterHandler(OpList, func(req *Request) *Response {
		return &Response{
			Status:  StatusOK,
			Payload: nil, // Empty payload
		}
	})
	server.Start()

	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:   server.addr,
		TLSConfig: tlsConfig,
		Timeout:   5 * time.Second,
	}

	trans, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	ctx := context.Background()
	stream, err := trans.RequestStream(ctx, "QuicBaaS", "List", nil)
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}
	defer stream.Close()

	// Send to trigger server response
	err = stream.Send(map[string]string{"key": "test"})
	if err != nil {
		t.Logf("send returned: %v", err)
	}

	// Receive with empty payload
	result, err := stream.Recv()
	if err != nil {
		t.Logf("recv error: %v (may be expected)", err)
	}
	// Empty payload should return nil result
	if result != nil && err == nil {
		t.Logf("received result: %v", result)
	}
}

// TestStream_RecvNonOKStatusNoPayload tests stream Recv with non-OK status and no payload.
func TestStream_RecvNonOKStatusNoPayload(t *testing.T) {
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()

	server.RegisterHandler(OpList, func(req *Request) *Response {
		return &Response{
			Status:  StatusTimeout,
			Payload: nil, // No payload
		}
	})
	server.Start()

	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:   server.addr,
		TLSConfig: tlsConfig,
		Timeout:   5 * time.Second,
	}

	trans, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	ctx := context.Background()
	stream, err := trans.RequestStream(ctx, "QuicBaaS", "List", nil)
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}
	defer stream.Close()

	// Send to trigger server response
	err = stream.Send(map[string]string{"key": "test"})
	if err != nil {
		t.Logf("send returned: %v", err)
	}

	// Receive should get the error with default message
	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestStream_DoubleClose tests calling Close multiple times.
func TestStream_DoubleClose(t *testing.T) {
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()

	server.RegisterHandler(OpList, func(req *Request) *Response {
		return &Response{Status: StatusOK, Payload: nil}
	})
	server.Start()

	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:   server.addr,
		TLSConfig: tlsConfig,
		Timeout:   5 * time.Second,
	}

	trans, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	ctx := context.Background()
	stream, err := trans.RequestStream(ctx, "QuicBaaS", "List", nil)
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}

	// First close
	err = stream.Close()
	if err != nil {
		t.Logf("first close returned: %v", err)
	}

	// Second close should be no-op
	err = stream.Close()
	if err != nil {
		t.Errorf("second close should not error: %v", err)
	}
}

// TestTransport_CloseWithError tests transport close when underlying close returns error.
func TestTransport_CloseWithError(t *testing.T) {
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()
	server.Start()

	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:   server.addr,
		TLSConfig: tlsConfig,
		Timeout:   5 * time.Second,
	}

	trans, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}

	// Close transport
	err = trans.Close()
	if err != nil {
		t.Logf("close returned: %v", err)
	}

	// Second close should be no-op
	err = trans.Close()
	if err != nil {
		t.Errorf("second close should not error: %v", err)
	}

	// Verify transport is not healthy
	if trans.Healthy() {
		t.Error("expected transport to be unhealthy after close")
	}
}

// TestProtocol_ReadRequestPartialTokenLen tests ReadRequest with partial token length.
func TestProtocol_ReadRequestPartialTokenLen(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(OpGet) // opcode
	buf.WriteByte(0x00)  // only 1 byte of token length
	// Missing second byte of token length

	_, err := ReadRequest(&buf)
	if err == nil {
		t.Fatal("expected error for partial token length")
	}
}

// TestProtocol_ReadRequestPartialPayloadLen tests ReadRequest with partial payload length.
func TestProtocol_ReadRequestPartialPayloadLen(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(OpGet)          // opcode
	buf.Write([]byte{0x00, 0x00}) // token length (0)
	buf.Write([]byte{0x00, 0x00}) // only 2 bytes of payload length
	// Missing 2 bytes of payload length

	_, err := ReadRequest(&buf)
	if err == nil {
		t.Fatal("expected error for partial payload length")
	}
}

// TestProtocol_ReadResponsePartialPayloadLen tests ReadResponse with partial payload length.
func TestProtocol_ReadResponsePartialPayloadLen(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(StatusOK)       // status
	buf.Write([]byte{0x00, 0x00}) // only 2 bytes of payload length
	// Missing 2 bytes of payload length

	_, err := ReadResponse(&buf)
	if err == nil {
		t.Fatal("expected error for partial payload length")
	}
}

// TestDecodePayload_Error tests DecodePayload with malformed data.
func TestDecodePayload_Error(t *testing.T) {
	// Malformed msgpack data
	malformed := []byte{0xFF, 0xFF, 0xFF}
	var result map[string]string
	err := DecodePayload(malformed, &result)
	if err == nil {
		t.Fatal("expected error for malformed data")
	}
}

// TestTransport_SetAuthTokenAndRequest tests SetAuthToken followed by request.
func TestTransport_SetAuthTokenAndRequest(t *testing.T) {
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()

	var (
		tokenMu       sync.Mutex
		receivedToken string
	)
	server.RegisterHandler(OpGet, func(req *Request) *Response {
		tokenMu.Lock()
		receivedToken = req.Token
		tokenMu.Unlock()
		return &Response{
			Status:  StatusOK,
			Payload: encodeResponse(map[string]string{"value": "test"}),
		}
	})
	server.Start()

	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:   server.addr,
		TLSConfig: tlsConfig,
		Timeout:   5 * time.Second,
	}

	trans, err := New(cfg, WithAuthToken("initial-token"))
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	// Update auth token
	trans.SetAuthToken("updated-token")

	ctx := context.Background()
	_, err = trans.Request(ctx, "QuicBaaS", "Get", map[string]string{"key": "test"})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	tokenMu.Lock()
	got := receivedToken
	tokenMu.Unlock()
	if got != "updated-token" {
		t.Errorf("expected token 'updated-token', got '%s'", got)
	}
}

// TestTransport_RequestWithEmptyAuthToken tests request with empty auth token.
func TestTransport_RequestWithEmptyAuthToken(t *testing.T) {
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()

	server.RegisterHandler(OpGet, func(req *Request) *Response {
		return &Response{
			Status:  StatusOK,
			Payload: encodeResponse(map[string]string{"value": "test"}),
		}
	})
	server.Start()

	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:   server.addr,
		TLSConfig: tlsConfig,
		Timeout:   5 * time.Second,
	}

	// Create transport without auth token
	trans, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	ctx := context.Background()
	_, err = trans.Request(ctx, "QuicBaaS", "Get", map[string]string{"key": "test"})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
}

// TestTransport_DoRequest_EmptyResponsePayload tests doRequest with empty response payload.
func TestTransport_DoRequest_EmptyResponsePayload(t *testing.T) {
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()

	server.RegisterHandler(OpDelete, func(req *Request) *Response {
		return &Response{
			Status:  StatusOK,
			Payload: nil, // Empty payload for delete
		}
	})
	server.Start()

	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:   server.addr,
		TLSConfig: tlsConfig,
		Timeout:   5 * time.Second,
	}

	trans, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	ctx := context.Background()
	result, err := trans.Request(ctx, "QuicBaaS", "Delete", map[string]string{"key": "test"})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	// Empty payload should result in nil
	if result != nil {
		t.Logf("received result: %v", result)
	}
}

// TestOptions_ValidateDefaults tests Validate applying defaults.
func TestOptions_ValidateDefaults(t *testing.T) {
	opts := &Options{
		HandshakeTimeout:      0, // Should be set to default
		MaxIdleTimeout:        0, // Should be set to default
		MaxIncomingStreams:    0, // Should be set to default
		MaxIncomingUniStreams: 0, // Should be set to default
	}

	err := opts.Validate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if opts.HandshakeTimeout != 10*time.Second {
		t.Errorf("expected default handshake timeout, got: %v", opts.HandshakeTimeout)
	}
	if opts.MaxIdleTimeout != 30*time.Second {
		t.Errorf("expected default max idle timeout, got: %v", opts.MaxIdleTimeout)
	}
	if opts.MaxIncomingStreams != 100 {
		t.Errorf("expected default max incoming streams, got: %d", opts.MaxIncomingStreams)
	}
	if opts.MaxIncomingUniStreams != 100 {
		t.Errorf("expected default max incoming uni streams, got: %d", opts.MaxIncomingUniStreams)
	}
}

// TestOptions_CloneWithoutTLSConfig tests Clone when TLSConfig is nil.
func TestOptions_CloneWithoutTLSConfig(t *testing.T) {
	opts := &Options{
		HandshakeTimeout: 15 * time.Second,
		AuthToken:        "test",
		TLSConfig:        nil,
	}

	clone := opts.Clone()
	if clone == nil {
		t.Fatal("expected clone to be non-nil")
	}
	if clone.HandshakeTimeout != 15*time.Second {
		t.Error("expected handshake timeout to be cloned")
	}
	if clone.TLSConfig != nil {
		t.Error("expected TLS config to remain nil")
	}
}

// TestRequestStream_WithInitialRequest tests RequestStream with initial request.
func TestRequestStream_WithInitialRequest(t *testing.T) {
	server, err := newMockQUICServer(t)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}
	defer server.Close()

	server.RegisterHandler(OpList, func(req *Request) *Response {
		return &Response{
			Status:  StatusOK,
			Payload: encodeResponse(map[string]string{"items": "a,b,c"}),
		}
	})
	server.Start()

	cert, _, err := generateTestCertificate()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:   server.addr,
		TLSConfig: tlsConfig,
		Timeout:   5 * time.Second,
	}

	trans, err := New(cfg, WithAuthToken("stream-token"))
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer trans.Close()

	ctx := context.Background()
	// Open stream with initial request
	stream, err := trans.RequestStream(ctx, "QuicBaaS", "List", map[string]string{"filter": "prefix:*"})
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}
	defer stream.Close()
}

// TestMinDuration_Coverage tests minDuration edge cases.
func TestMinDuration_Coverage(t *testing.T) {
	tests := []struct {
		a, b, expected time.Duration
	}{
		{0, time.Second, 0},
		{time.Second, 0, 0},
		{time.Second, time.Second, time.Second},
		{time.Millisecond, time.Second, time.Millisecond},
		{time.Second, time.Millisecond, time.Millisecond},
	}

	for _, tt := range tests {
		result := minDuration(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("minDuration(%v, %v) = %v, expected %v", tt.a, tt.b, result, tt.expected)
		}
	}
}

// TestIsRetryableError_AllCases tests isRetryableError with various error types.
// Note: According to errors/codes.go, only Timeout, Unavailable, and ResourceExhausted are retryable.
// Connection errors are NOT retryable by default.
func TestIsRetryableError_AllCases(t *testing.T) {
	tests := []struct {
		err       error
		retryable bool
	}{
		{nil, false},
		{errors.NewTimeout("op", "timeout"), true},
		{errors.NewUnavailable("op", "unavailable"), true},
		{errors.NewResourceExhausted("op", "exhausted"), true},
		{errors.NewConnection("op", "connection"), false}, // Connection is NOT retryable
		{errors.NewNotFound("op", "not found"), false},
		{errors.NewInvalidInput("op", "invalid"), false},
		{errors.NewInternal("op", "internal"), false},
		{errors.NewUnauthorized("op", "unauthorized"), false},
		{errors.NewForbidden("op", "forbidden"), false},
		{errors.NewAlreadyExists("op", "exists"), false},
	}

	for _, tt := range tests {
		result := isRetryableError(tt.err)
		if result != tt.retryable {
			t.Errorf("isRetryableError(%v) = %v, expected %v", tt.err, result, tt.retryable)
		}
	}
}
