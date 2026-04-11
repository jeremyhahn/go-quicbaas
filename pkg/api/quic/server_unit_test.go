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
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"testing"
	"time"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock Service for Testing
// =============================================================================

type testMockService struct {
	service.QuicBaaS
}

// =============================================================================
// Test TLS Helper
// =============================================================================

func generateTestTLS(t *testing.T) *tls.Config {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
			CommonName:   "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Failed to load key pair: %v", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}
}

// =============================================================================
// Protocol Error Path Tests - Covering Write Errors
// =============================================================================

func TestWriteRequest_WriterError_Opcode(t *testing.T) {
	req := &Request{
		Opcode:  OpPut,
		Payload: []byte("test"),
	}

	// Create a writer that always returns an error
	errWriter := &errorWriter{errOnWrite: true}

	err := WriteRequest(errWriter, req)
	if err == nil {
		t.Fatal("WriteRequest() should fail with error writer")
	}

	var qbErr *qberrors.QBError
	if !errors.As(err, &qbErr) {
		t.Errorf("expected QBError, got %T", err)
	}
}

func TestWriteRequest_WriterError_Length(t *testing.T) {
	req := &Request{
		Opcode:  OpPut,
		Payload: []byte("test"),
	}

	// Create a writer that fails on second write (length write)
	errWriter := &errorWriter{failAfterN: 1}

	err := WriteRequest(errWriter, req)
	if err == nil {
		t.Fatal("WriteRequest() should fail when writing length")
	}

	var qbErr *qberrors.QBError
	if !errors.As(err, &qbErr) {
		t.Errorf("expected QBError, got %T", err)
	}
}

func TestWriteRequest_WriterError_Payload(t *testing.T) {
	req := &Request{
		Opcode:  OpPut,
		Payload: []byte("test"),
	}

	// Create a writer that fails on third write (payload write)
	errWriter := &errorWriter{failAfterN: 2}

	err := WriteRequest(errWriter, req)
	if err == nil {
		t.Fatal("WriteRequest() should fail when writing payload")
	}

	var qbErr *qberrors.QBError
	if !errors.As(err, &qbErr) {
		t.Errorf("expected QBError, got %T", err)
	}
}

func TestWriteResponse_WriterError_Status(t *testing.T) {
	resp := &Response{
		Status:  StatusOK,
		Payload: []byte("test"),
	}

	// Create a writer that always returns an error
	errWriter := &errorWriter{errOnWrite: true}

	err := WriteResponse(errWriter, resp)
	if err == nil {
		t.Fatal("WriteResponse() should fail with error writer")
	}

	var qbErr *qberrors.QBError
	if !errors.As(err, &qbErr) {
		t.Errorf("expected QBError, got %T", err)
	}
}

func TestWriteResponse_WriterError_Length(t *testing.T) {
	resp := &Response{
		Status:  StatusOK,
		Payload: []byte("test"),
	}

	// Create a writer that fails on second write
	errWriter := &errorWriter{failAfterN: 1}

	err := WriteResponse(errWriter, resp)
	if err == nil {
		t.Fatal("WriteResponse() should fail when writing length")
	}

	var qbErr *qberrors.QBError
	if !errors.As(err, &qbErr) {
		t.Errorf("expected QBError, got %T", err)
	}
}

func TestWriteResponse_WriterError_Payload(t *testing.T) {
	resp := &Response{
		Status:  StatusOK,
		Payload: []byte("test"),
	}

	// Create a writer that fails on third write
	errWriter := &errorWriter{failAfterN: 2}

	err := WriteResponse(errWriter, resp)
	if err == nil {
		t.Fatal("WriteResponse() should fail when writing payload")
	}

	var qbErr *qberrors.QBError
	if !errors.As(err, &qbErr) {
		t.Errorf("expected QBError, got %T", err)
	}
}

// =============================================================================
// EncodePayload Error Path Tests
// =============================================================================

func TestEncodePayload_UnencodableType_Channel(t *testing.T) {
	// Create a type that can't be encoded (channel)
	ch := make(chan int)

	_, err := EncodePayload(ch)
	if err == nil {
		t.Error("EncodePayload() should fail for channel type")
	}

	var qbErr *qberrors.QBError
	if !errors.As(err, &qbErr) {
		t.Errorf("expected QBError, got %T", err)
	}
}

func TestEncodePayload_UnencodableType_Function(t *testing.T) {
	// Create a type that can't be encoded (function)
	fn := func() {}

	_, err := EncodePayload(fn)
	if err == nil {
		t.Error("EncodePayload() should fail for function type")
	}

	var qbErr *qberrors.QBError
	if !errors.As(err, &qbErr) {
		t.Errorf("expected QBError, got %T", err)
	}
}

func TestEncodePayload_ComplexUnencodableStruct(t *testing.T) {
	type badStruct struct {
		Ch chan int
	}

	bs := badStruct{Ch: make(chan int)}

	_, err := EncodePayload(bs)
	if err == nil {
		t.Error("EncodePayload() should fail for struct with channel field")
	}

	var qbErr *qberrors.QBError
	if !errors.As(err, &qbErr) {
		t.Errorf("expected QBError, got %T", err)
	}
}

// =============================================================================
// Additional Opcode Coverage Tests
// =============================================================================

func TestOpcodeToString_ExtendedOpcodes(t *testing.T) {
	tests := []struct {
		opcode byte
		want   string
	}{
		{OpListNodes, "ListNodes"},
		{OpRebalanceStatus, "RebalanceStatus"},
		{OpRebalancePlan, "RebalancePlan"},
		{OpCancelMigration, "CancelMigration"},
		{OpNodeReplaceRequest, "NodeReplaceRequest"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := OpcodeToString(tt.opcode)
			if got != tt.want {
				t.Errorf("OpcodeToString(0x%02X) = %s, want %s", tt.opcode, got, tt.want)
			}
		})
	}
}

// =============================================================================
// Additional Error Conversion Tests
// =============================================================================

func TestErrorToStatus_UnknownErrorCode(t *testing.T) {
	// Test error with unknown code - should map to StatusInternalError
	err := &qberrors.QBError{
		Code: 999, // Unknown error code
		Op:   "test",
		Err:  errors.New("unknown error"),
	}

	status := ErrorToStatus(err)
	if status != StatusInternalError {
		t.Errorf("ErrorToStatus() for unknown code = %d, want %d", status, StatusInternalError)
	}
}

func TestErrorToStatus_StandardError(t *testing.T) {
	// Test non-QBError - should map to StatusInternalError
	err := errors.New("standard error")

	status := ErrorToStatus(err)
	if status != StatusInternalError {
		t.Errorf("ErrorToStatus() for standard error = %d, want %d", status, StatusInternalError)
	}
}

func TestStatusToError_AllStatusCodes(t *testing.T) {
	tests := []struct {
		status  byte
		name    string
		wantNil bool
	}{
		{StatusOK, "ok", true},
		{StatusAlreadyExists, "already exists", false},
		{StatusTimeout, "timeout", false},
		{StatusPermissionDenied, "permission denied", false},
		{StatusResourceExhausted, "resource exhausted", false},
		{StatusUnavailable, "unavailable", false},
		{StatusInvalidRequest, "invalid request", false},
		{0xFE, "unknown status", false}, // Test default case
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := StatusToError(tt.status, tt.name)

			if tt.wantNil {
				if err != nil {
					t.Errorf("StatusToError(%d) should return nil, got %v", tt.status, err)
				}
			} else {
				if err == nil {
					t.Errorf("StatusToError(%d) should return error", tt.status)
				}

				var qbErr *qberrors.QBError
				if !errors.As(err, &qbErr) {
					t.Errorf("expected QBError, got %T", err)
				}
			}
		})
	}
}

// =============================================================================
// NewServer Error Path Tests
// =============================================================================

func TestNewServer_NilService_WithValidTLS(t *testing.T) {
	cfg := &Config{
		ListenAddr: "0.0.0.0:4433",
		TLSConfig:  &tls.Config{MinVersion: tls.VersionTLS13},
	}

	srv, err := NewServer(nil, cfg, nil)
	if err == nil {
		t.Fatal("NewServer() with nil service should return error")
	}

	if srv != nil {
		t.Error("NewServer() should return nil server on error")
	}

	var qbErr *qberrors.QBError
	if !errors.As(err, &qbErr) {
		t.Errorf("expected QBError, got %T", err)
	}

	if qbErr.Code != qberrors.ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", qbErr.Code)
	}
}

func TestNewServer_NilConfig_UseDefault(t *testing.T) {
	svc := &testMockService{}
	tlsCfg := generateTestTLS(t)

	// Pass nil config - should use DefaultConfig but fail on nil TLS
	srv, err := NewServer(svc, nil, nil)
	if err == nil {
		t.Fatal("NewServer() with nil config should fail (no TLS)")
	}

	if srv != nil {
		t.Error("NewServer() should return nil server on TLS error")
	}

	// Now pass config with TLS
	cfg := DefaultConfig()
	cfg.TLSConfig = tlsCfg

	srv, err = NewServer(svc, cfg, nil)
	if err != nil {
		t.Fatalf("NewServer() with valid config failed: %v", err)
	}

	if srv == nil {
		t.Fatal("NewServer() should return valid server")
	}

	if srv.config.ListenAddr != "0.0.0.0:4433" {
		t.Errorf("expected default listen addr, got %s", srv.config.ListenAddr)
	}
}

func TestNewServer_NilTLSConfig(t *testing.T) {
	svc := &testMockService{}
	cfg := &Config{
		ListenAddr: "0.0.0.0:4433",
		TLSConfig:  nil,
	}

	srv, err := NewServer(svc, cfg, nil)
	if err == nil {
		t.Fatal("NewServer() without TLS should return error")
	}

	if srv != nil {
		t.Error("NewServer() should return nil server on error")
	}

	var qbErr *qberrors.QBError
	if !errors.As(err, &qbErr) {
		t.Errorf("expected QBError, got %T", err)
	}

	if qbErr.Code != qberrors.ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", qbErr.Code)
	}
}

func TestNewServer_WithCustomLogger(t *testing.T) {
	svc := &testMockService{}
	cfg := DefaultConfig()
	cfg.TLSConfig = generateTestTLS(t)

	// Create custom logger
	logCfg := logger.LogConfig{
		Level:     "debug",
		Format:    "json",
		Output:    "stdout",
		Component: "test-quic",
	}
	customLog, err := logger.New(logCfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	srv, err := NewServer(svc, cfg, customLog)
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}

	if srv.logger != customLog {
		t.Error("NewServer() should use provided logger")
	}
}

func TestNewServer_NilLogger_AutoCreate(t *testing.T) {
	svc := &testMockService{}
	cfg := DefaultConfig()
	cfg.TLSConfig = generateTestTLS(t)

	srv, err := NewServer(svc, cfg, nil)
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}

	if srv.logger == nil {
		t.Error("NewServer() should auto-create logger when nil")
	}
}

func TestNewServer_ServerState(t *testing.T) {
	svc := &testMockService{}
	cfg := DefaultConfig()
	cfg.TLSConfig = generateTestTLS(t)

	srv, err := NewServer(svc, cfg, nil)
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}

	// Check initial state
	if srv.running.Load() {
		t.Error("Server should not be running initially")
	}

	if srv.activeStreams.Load() != 0 {
		t.Error("activeStreams should be 0 initially")
	}

	if srv.totalRequests.Load() != 0 {
		t.Error("totalRequests should be 0 initially")
	}

	if srv.totalErrors.Load() != 0 {
		t.Error("totalErrors should be 0 initially")
	}

	if srv.activeConns.Load() != 0 {
		t.Error("activeConns should be 0 initially")
	}

	if srv.ctx == nil {
		t.Error("context should be initialized")
	}

	if srv.cancel == nil {
		t.Error("cancel function should be initialized")
	}
}

// =============================================================================
// Helper Types for Error Testing
// =============================================================================

// errorWriter is a writer that can be configured to fail at specific points.
type errorWriter struct {
	writeCount int
	failAfterN int
	errOnWrite bool
}

func (w *errorWriter) Write(p []byte) (n int, err error) {
	if w.errOnWrite {
		return 0, errors.New("write error")
	}

	w.writeCount++
	if w.failAfterN > 0 && w.writeCount > w.failAfterN {
		return 0, errors.New("write error after N writes")
	}

	return len(p), nil
}

// mockStream implements a simple in-memory stream for testing.
type mockStream struct {
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
	closed   bool
}

func newMockStream() *mockStream {
	return &mockStream{
		readBuf:  &bytes.Buffer{},
		writeBuf: &bytes.Buffer{},
	}
}

func (s *mockStream) Read(p []byte) (n int, err error) {
	if s.closed {
		return 0, io.EOF
	}
	return s.readBuf.Read(p)
}

func (s *mockStream) Write(p []byte) (n int, err error) {
	if s.closed {
		return 0, errors.New("stream closed")
	}
	return s.writeBuf.Write(p)
}

func (s *mockStream) Close() error {
	s.closed = true
	return nil
}

func (s *mockStream) SetDeadline(t time.Time) error {
	return nil
}

func (s *mockStream) SetReadDeadline(t time.Time) error {
	return nil
}

func (s *mockStream) SetWriteDeadline(t time.Time) error {
	return nil
}
