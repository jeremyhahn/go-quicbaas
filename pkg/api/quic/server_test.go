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
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// =============================================================================
// Test Helpers
// =============================================================================

func generateTestTLSConfig(t *testing.T) *tls.Config {
	// Generate a self-signed certificate for testing
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate private key: %v", err)
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(365 * 24 * time.Hour)

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("failed to generate serial number: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Test Co"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("failed to parse key pair: %v", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"quic-quicbaas"},
	}
}

func createServerTestLogger() *logger.Logger {
	cfg := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "quic-server-test",
	}
	log, _ := logger.New(cfg)
	return log
}

// =============================================================================
// Server Constructor Tests
// =============================================================================

func TestNewServer_Success(t *testing.T) {
	svc := &mockService{}
	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if server == nil {
		t.Fatal("expected server to not be nil")
	}
}

func TestNewServer_NilService(t *testing.T) {
	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	log := createServerTestLogger()

	server, err := NewServer(nil, config, log)
	if err == nil {
		t.Error("expected error when service is nil")
	}
	if server != nil {
		t.Error("expected server to be nil when service is nil")
	}
}

func TestNewServer_WithNilTLSConfig(t *testing.T) {
	svc := &mockService{}
	config := DefaultConfig()
	config.TLSConfig = nil
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err == nil {
		t.Error("expected error when TLS config is nil")
	}
	if server != nil {
		t.Error("expected server to be nil when TLS config is nil")
	}
}

func TestNewServer_DefaultConfig(t *testing.T) {
	svc := &mockService{}
	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)

	server, err := NewServer(svc, config, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if server == nil {
		t.Fatal("expected server to not be nil")
	}
	if server.logger == nil {
		t.Error("expected logger to be created when nil is passed")
	}
}

func TestNewServer_NilConfig(t *testing.T) {
	svc := &mockService{}
	log := createServerTestLogger()

	server, err := NewServer(svc, nil, log)
	// Should fail because default config has no TLS config
	if err == nil {
		t.Error("expected error when config is nil (no TLS)")
	}
	if server != nil {
		t.Error("expected server to be nil")
	}
}

// =============================================================================
// Server Lifecycle Tests
// =============================================================================

func TestServer_Start_AlreadyRunning(t *testing.T) {
	svc := &mockService{}
	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	config.ListenAddr = "127.0.0.1:0" // Use random port
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Start the server
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	// Try to start again
	err = server.Start()
	if err == nil {
		t.Error("expected error when starting already running server")
	}
}

func TestServer_Stop_NotRunning(t *testing.T) {
	svc := &mockService{}
	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	err = server.Stop()
	if err == nil {
		t.Error("expected error when stopping non-running server")
	}
}

func TestServer_StartStop(t *testing.T) {
	svc := &mockService{}
	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	config.ListenAddr = "127.0.0.1:0"
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Start server
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// Verify it's running
	if !server.IsRunning() {
		t.Error("expected server to be running")
	}

	// Check metrics
	metrics := server.GetMetrics()
	if metrics["running"] != true {
		t.Error("expected running metric to be true")
	}

	// Stop server
	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}

	// Verify it's not running
	if server.IsRunning() {
		t.Error("expected server to not be running after stop")
	}
}

func TestServer_Start_InvalidAddress(t *testing.T) {
	svc := &mockService{}
	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	config.ListenAddr = "invalid-address-format"
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	err = server.Start()
	if err == nil {
		t.Error("expected error when starting with invalid address")
	}
}

// =============================================================================
// Server Metrics Tests
// =============================================================================

func TestServer_Metrics(t *testing.T) {
	svc := &mockService{}
	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	config.EnableMetrics = true
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	metrics := server.GetMetrics()

	// Check all expected metrics exist
	expectedKeys := []string{
		"active_connections",
		"active_streams",
		"total_requests",
		"total_errors",
		"running",
	}

	for _, key := range expectedKeys {
		if _, exists := metrics[key]; !exists {
			t.Errorf("expected metric %s to exist", key)
		}
	}

	// Verify initial values
	if metrics["active_connections"] != int64(0) {
		t.Errorf("expected active_connections to be 0, got %v", metrics["active_connections"])
	}
	if metrics["total_requests"] != uint64(0) {
		t.Errorf("expected total_requests to be 0, got %v", metrics["total_requests"])
	}
	if metrics["running"] != false {
		t.Errorf("expected running to be false, got %v", metrics["running"])
	}
}

func TestServer_MetricsAfterStart(t *testing.T) {
	svc := &mockService{}
	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	config.ListenAddr = "127.0.0.1:0"
	config.EnableMetrics = true
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	metrics := server.GetMetrics()
	if metrics["running"] != true {
		t.Errorf("expected running to be true, got %v", metrics["running"])
	}
}

// =============================================================================
// Server Configuration Tests
// =============================================================================

func TestDefaultConfig_Values(t *testing.T) {
	config := DefaultConfig()

	tests := []struct {
		name     string
		value    interface{}
		expected interface{}
	}{
		{"ListenAddr", config.ListenAddr, "0.0.0.0:4433"},
		{"MaxStreams", config.MaxStreams, 1000},
		{"MaxIdleTimeout", config.MaxIdleTimeout, 30 * time.Second},
		{"MaxIncomingStreams", config.MaxIncomingStreams, int64(1000)},
		{"MaxIncomingUniStreams", config.MaxIncomingUniStreams, int64(100)},
		{"KeepAlivePeriod", config.KeepAlivePeriod, 15 * time.Second},
		{"HandshakeIdleTimeout", config.HandshakeIdleTimeout, 5 * time.Second},
		{"MaxReceiveBufferSize", config.MaxReceiveBufferSize, uint64(1 * 1024 * 1024)},
		{"InitialStreamReceiveWindow", config.InitialStreamReceiveWindow, uint64(512 * 1024)},
		{"MaxStreamReceiveWindow", config.MaxStreamReceiveWindow, uint64(6 * 1024 * 1024)},
		{"EnableMetrics", config.EnableMetrics, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.expected {
				t.Errorf("expected %s to be %v, got %v", tt.name, tt.expected, tt.value)
			}
		})
	}
}

func TestServer_CustomConfig(t *testing.T) {
	svc := &mockService{}
	config := &Config{
		ListenAddr:                 "127.0.0.1:9999",
		TLSConfig:                  generateTestTLSConfig(t),
		MaxStreams:                 500,
		MaxIdleTimeout:             60 * time.Second,
		MaxIncomingStreams:         500,
		MaxIncomingUniStreams:      50,
		KeepAlivePeriod:            30 * time.Second,
		HandshakeIdleTimeout:       10 * time.Second,
		MaxReceiveBufferSize:       2 * 1024 * 1024,
		InitialStreamReceiveWindow: 1024 * 1024,
		MaxStreamReceiveWindow:     10 * 1024 * 1024,
		EnableMetrics:              false,
	}
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if server.config.MaxStreams != 500 {
		t.Errorf("expected MaxStreams to be 500, got %d", server.config.MaxStreams)
	}
	if server.config.EnableMetrics != false {
		t.Error("expected EnableMetrics to be false")
	}
}

// =============================================================================
// Error Response Tests
// =============================================================================

// =============================================================================
// Protocol Coverage Tests
// =============================================================================

// Concurrent Operations Tests
// =============================================================================

func TestServer_ConcurrentRequests(t *testing.T) {
	svc := &mockService{}
	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	config.ListenAddr = "127.0.0.1:0"
	config.MaxStreams = 10
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	// Simulate concurrent metric access
	done := make(chan bool)
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				_ = server.GetMetrics()
				_ = server.IsRunning()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 5; i++ {
		<-done
	}
}

// =============================================================================
// Edge Case Tests
// =============================================================================

func TestServer_StopTimeout(t *testing.T) {
	// This test verifies the timeout mechanism in Stop()
	// We can't easily trigger it, but we can test normal stop
	svc := &mockService{}
	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	config.ListenAddr = "127.0.0.1:0"
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// Stop with a context timeout would be tested here
	// For now, just verify normal stop works
	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}
