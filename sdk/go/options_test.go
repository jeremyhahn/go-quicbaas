// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package quicbaas

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.transportType != TransportGRPC {
		t.Errorf("expected transportType=TransportGRPC, got %v", opts.transportType)
	}
	if opts.timeout != 30*time.Second {
		t.Errorf("expected timeout=30s, got %v", opts.timeout)
	}
	if opts.keepaliveTime != 0 {
		t.Errorf("expected keepaliveTime=0 (disabled), got %v", opts.keepaliveTime)
	}
	if opts.keepaliveTimeout != 20*time.Second {
		t.Errorf("expected keepaliveTimeout=20s, got %v", opts.keepaliveTimeout)
	}
	if opts.maxRetries != 3 {
		t.Errorf("expected maxRetries=3, got %d", opts.maxRetries)
	}
	if opts.retryBackoff != 100*time.Millisecond {
		t.Errorf("expected retryBackoff=100ms, got %v", opts.retryBackoff)
	}
	if opts.maxRetryBackoff != 5*time.Second {
		t.Errorf("expected maxRetryBackoff=5s, got %v", opts.maxRetryBackoff)
	}
	if opts.tlsEnabled {
		t.Error("expected tlsEnabled=false")
	}
	if opts.insecure {
		t.Error("expected insecure=false")
	}
	if opts.poolEnabled {
		t.Error("expected poolEnabled=false")
	}
	if opts.poolMinSize != 5 {
		t.Errorf("expected poolMinSize=5, got %d", opts.poolMinSize)
	}
	if opts.poolMaxSize != 50 {
		t.Errorf("expected poolMaxSize=50, got %d", opts.poolMaxSize)
	}
	if opts.poolMaxAge != time.Hour {
		t.Errorf("expected poolMaxAge=1h, got %v", opts.poolMaxAge)
	}
	if opts.poolHealthInterval != 30*time.Second {
		t.Errorf("expected poolHealthInterval=30s, got %v", opts.poolHealthInterval)
	}
}

func TestWithTimeout(t *testing.T) {
	t.Run("valid timeout", func(t *testing.T) {
		opts := defaultOptions()
		err := WithTimeout(5 * time.Second)(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if opts.timeout != 5*time.Second {
			t.Errorf("expected timeout=5s, got %v", opts.timeout)
		}
	})

	t.Run("zero timeout", func(t *testing.T) {
		opts := defaultOptions()
		err := WithTimeout(0)(opts)
		if err == nil {
			t.Fatal("expected error for zero timeout")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput error, got %v", err)
		}
	})

	t.Run("negative timeout", func(t *testing.T) {
		opts := defaultOptions()
		err := WithTimeout(-1 * time.Second)(opts)
		if err == nil {
			t.Fatal("expected error for negative timeout")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput error, got %v", err)
		}
	})
}

func TestWithKeepalive(t *testing.T) {
	t.Run("valid keepalive", func(t *testing.T) {
		opts := defaultOptions()
		err := WithKeepalive(15*time.Second, 10*time.Second)(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if opts.keepaliveTime != 15*time.Second {
			t.Errorf("expected keepaliveTime=15s, got %v", opts.keepaliveTime)
		}
		if opts.keepaliveTimeout != 10*time.Second {
			t.Errorf("expected keepaliveTimeout=10s, got %v", opts.keepaliveTimeout)
		}
	})

	t.Run("zero time", func(t *testing.T) {
		opts := defaultOptions()
		err := WithKeepalive(0, 5*time.Second)(opts)
		if err == nil {
			t.Fatal("expected error for zero time")
		}
	})

	t.Run("zero timeout", func(t *testing.T) {
		opts := defaultOptions()
		err := WithKeepalive(10*time.Second, 0)(opts)
		if err == nil {
			t.Fatal("expected error for zero timeout")
		}
	})

	t.Run("negative time", func(t *testing.T) {
		opts := defaultOptions()
		err := WithKeepalive(-1*time.Second, 5*time.Second)(opts)
		if err == nil {
			t.Fatal("expected error for negative time")
		}
	})

	t.Run("negative timeout", func(t *testing.T) {
		opts := defaultOptions()
		err := WithKeepalive(10*time.Second, -1*time.Second)(opts)
		if err == nil {
			t.Fatal("expected error for negative timeout")
		}
	})
}

func TestWithRetry(t *testing.T) {
	t.Run("valid retry", func(t *testing.T) {
		opts := defaultOptions()
		err := WithRetry(5, 200*time.Millisecond)(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if opts.maxRetries != 5 {
			t.Errorf("expected maxRetries=5, got %d", opts.maxRetries)
		}
		if opts.retryBackoff != 200*time.Millisecond {
			t.Errorf("expected retryBackoff=200ms, got %v", opts.retryBackoff)
		}
	})

	t.Run("zero retries is valid", func(t *testing.T) {
		opts := defaultOptions()
		err := WithRetry(0, 100*time.Millisecond)(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("negative retries", func(t *testing.T) {
		opts := defaultOptions()
		err := WithRetry(-1, 100*time.Millisecond)(opts)
		if err == nil {
			t.Fatal("expected error for negative retries")
		}
	})

	t.Run("negative backoff", func(t *testing.T) {
		opts := defaultOptions()
		err := WithRetry(3, -1*time.Millisecond)(opts)
		if err == nil {
			t.Fatal("expected error for negative backoff")
		}
	})
}

func TestWithMaxRetryBackoff(t *testing.T) {
	t.Run("valid max backoff", func(t *testing.T) {
		opts := defaultOptions()
		err := WithMaxRetryBackoff(10 * time.Second)(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if opts.maxRetryBackoff != 10*time.Second {
			t.Errorf("expected maxRetryBackoff=10s, got %v", opts.maxRetryBackoff)
		}
	})

	t.Run("zero max backoff", func(t *testing.T) {
		opts := defaultOptions()
		err := WithMaxRetryBackoff(0)(opts)
		if err == nil {
			t.Fatal("expected error for zero max backoff")
		}
	})

	t.Run("negative max backoff", func(t *testing.T) {
		opts := defaultOptions()
		err := WithMaxRetryBackoff(-1 * time.Second)(opts)
		if err == nil {
			t.Fatal("expected error for negative max backoff")
		}
	})
}

func TestWithTLSConfig(t *testing.T) {
	t.Run("valid TLS config", func(t *testing.T) {
		opts := defaultOptions()
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13}
		err := WithTLSConfig(tlsCfg)(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if opts.tlsConfig != tlsCfg {
			t.Error("expected tlsConfig to be set")
		}
		if !opts.tlsEnabled {
			t.Error("expected tlsEnabled=true")
		}
		if opts.insecure {
			t.Error("expected insecure=false")
		}
	})

	t.Run("nil TLS config", func(t *testing.T) {
		opts := defaultOptions()
		err := WithTLSConfig(nil)(opts)
		if err == nil {
			t.Fatal("expected error for nil TLS config")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput error, got %v", err)
		}
	})
}

// TestWithInsecure verifies WithInsecure is banned per QuicBaaS's FedRAMP
// security policy. Callers MUST use WithTLSConfig(cfg) with cfg.RootCAs
// set; there is no insecure escape hatch in the SDK.
func TestWithInsecure(t *testing.T) {
	opts := defaultOptions()
	err := WithInsecure()(opts)
	if err == nil {
		t.Fatal("WithInsecure must return an error (it is banned by policy)")
	}
	if opts.insecure {
		t.Error("WithInsecure must NOT set insecure=true even when called")
	}
}

func TestWithConnectionPool(t *testing.T) {
	t.Run("valid pool config", func(t *testing.T) {
		opts := defaultOptions()
		err := WithConnectionPool(10, 100)(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !opts.poolEnabled {
			t.Error("expected poolEnabled=true")
		}
		if opts.poolMinSize != 10 {
			t.Errorf("expected poolMinSize=10, got %d", opts.poolMinSize)
		}
		if opts.poolMaxSize != 100 {
			t.Errorf("expected poolMaxSize=100, got %d", opts.poolMaxSize)
		}
	})

	t.Run("zero min size", func(t *testing.T) {
		opts := defaultOptions()
		err := WithConnectionPool(0, 10)(opts)
		if err == nil {
			t.Fatal("expected error for zero min size")
		}
	})

	t.Run("negative min size", func(t *testing.T) {
		opts := defaultOptions()
		err := WithConnectionPool(-1, 10)(opts)
		if err == nil {
			t.Fatal("expected error for negative min size")
		}
	})

	t.Run("max size less than min size", func(t *testing.T) {
		opts := defaultOptions()
		err := WithConnectionPool(10, 5)(opts)
		if err == nil {
			t.Fatal("expected error for max < min")
		}
	})
}

func TestWithPoolMaxAge(t *testing.T) {
	t.Run("valid max age", func(t *testing.T) {
		opts := defaultOptions()
		err := WithPoolMaxAge(2 * time.Hour)(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if opts.poolMaxAge != 2*time.Hour {
			t.Errorf("expected poolMaxAge=2h, got %v", opts.poolMaxAge)
		}
	})

	t.Run("zero max age", func(t *testing.T) {
		opts := defaultOptions()
		err := WithPoolMaxAge(0)(opts)
		if err == nil {
			t.Fatal("expected error for zero max age")
		}
	})
}

func TestWithPoolHealthInterval(t *testing.T) {
	t.Run("valid interval", func(t *testing.T) {
		opts := defaultOptions()
		err := WithPoolHealthInterval(time.Minute)(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if opts.poolHealthInterval != time.Minute {
			t.Errorf("expected poolHealthInterval=1m, got %v", opts.poolHealthInterval)
		}
	})

	t.Run("zero interval", func(t *testing.T) {
		opts := defaultOptions()
		err := WithPoolHealthInterval(0)(opts)
		if err == nil {
			t.Fatal("expected error for zero interval")
		}
	})
}

func TestWithREST(t *testing.T) {
	opts := defaultOptions()
	err := WithREST()(opts)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if opts.transportType != TransportREST {
		t.Errorf("expected transportType=TransportREST, got %v", opts.transportType)
	}
}

func TestWithQUIC(t *testing.T) {
	opts := defaultOptions()
	err := WithQUIC()(opts)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if opts.transportType != TransportQUIC {
		t.Errorf("expected transportType=TransportQUIC, got %v", opts.transportType)
	}
}

func TestWithUnix_Success(t *testing.T) {
	opts := defaultOptions()
	socketPath := "/var/run/quicbaas.sock"
	err := WithUnix(socketPath)(opts)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if opts.transportType != TransportUnix {
		t.Errorf("expected transportType=TransportUnix, got %v", opts.transportType)
	}
	if opts.unixSocketPath != socketPath {
		t.Errorf("expected unixSocketPath=%q, got %q", socketPath, opts.unixSocketPath)
	}
}

func TestWithUnix_EmptyPath(t *testing.T) {
	opts := defaultOptions()
	err := WithUnix("")(opts)
	if err == nil {
		t.Fatal("expected error for empty socket path")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestWithEmbedded_Success(t *testing.T) {
	opts := defaultOptions()
	mockService := &struct{}{}
	err := WithEmbedded(mockService)(opts)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if opts.transportType != TransportEmbedded {
		t.Errorf("expected transportType=TransportEmbedded, got %v", opts.transportType)
	}
	if opts.embeddedService != mockService {
		t.Error("expected embeddedService to be set")
	}
}

func TestWithEmbedded_NilService(t *testing.T) {
	opts := defaultOptions()
	err := WithEmbedded(nil)(opts)
	if err == nil {
		t.Fatal("expected error for nil service")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

// Test helper: create temp certificates
func createTestCerts(t *testing.T) (certFile, keyFile, caFile string, cleanup func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "tls-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	cleanup = func() {
		os.RemoveAll(tmpDir)
	}

	// Generate CA private key
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		cleanup()
		t.Fatalf("failed to generate CA key: %v", err)
	}

	// Create CA certificate
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test CA"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		cleanup()
		t.Fatalf("failed to create CA cert: %v", err)
	}

	// Write CA certificate
	caFile = filepath.Join(tmpDir, "ca.pem")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})
	if err := os.WriteFile(caFile, caPEM, 0600); err != nil {
		cleanup()
		t.Fatalf("failed to write CA file: %v", err)
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		cleanup()
		t.Fatalf("failed to parse CA cert: %v", err)
	}

	// Generate client private key
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		cleanup()
		t.Fatalf("failed to generate client key: %v", err)
	}

	// Create client certificate
	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{"Test Client"},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	clientCertDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		cleanup()
		t.Fatalf("failed to create client cert: %v", err)
	}

	// Write client certificate
	certFile = filepath.Join(tmpDir, "client.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientCertDER})
	if err := os.WriteFile(certFile, certPEM, 0600); err != nil {
		cleanup()
		t.Fatalf("failed to write cert file: %v", err)
	}

	// Write client key
	keyFile = filepath.Join(tmpDir, "client-key.pem")
	keyDER, err := x509.MarshalECPrivateKey(clientKey)
	if err != nil {
		cleanup()
		t.Fatalf("failed to marshal client key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		cleanup()
		t.Fatalf("failed to write key file: %v", err)
	}

	return certFile, keyFile, caFile, cleanup
}

func TestWithTLS(t *testing.T) {
	certFile, keyFile, caFile, cleanup := createTestCerts(t)
	defer cleanup()

	t.Run("full mTLS config", func(t *testing.T) {
		opts := defaultOptions()
		err := WithTLS(TLSConfig{
			CertFile:   certFile,
			KeyFile:    keyFile,
			CAFile:     caFile,
			ServerName: "test.local",
		})(opts)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !opts.tlsEnabled {
			t.Error("expected tlsEnabled=true")
		}
		if opts.tlsConfig == nil {
			t.Error("expected tlsConfig to be set")
		}
		if opts.insecure {
			t.Error("expected insecure=false")
		}
	})

	t.Run("invalid cert file", func(t *testing.T) {
		opts := defaultOptions()
		err := WithTLS(TLSConfig{
			CertFile: "/nonexistent/cert.pem",
			KeyFile:  keyFile,
		})(opts)
		if err == nil {
			t.Fatal("expected error for invalid cert file")
		}
	})

	t.Run("invalid CA file", func(t *testing.T) {
		opts := defaultOptions()
		err := WithTLS(TLSConfig{
			CertFile: certFile,
			KeyFile:  keyFile,
			CAFile:   "/nonexistent/ca.pem",
		})(opts)
		if err == nil {
			t.Fatal("expected error for invalid CA file")
		}
	})
}

func TestLoadTLSConfig(t *testing.T) {
	certFile, keyFile, caFile, cleanup := createTestCerts(t)
	defer cleanup()

	t.Run("empty config", func(t *testing.T) {
		cfg, err := loadTLSConfig(TLSConfig{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if cfg.MinVersion != tls.VersionTLS13 {
			t.Error("expected MinVersion TLS1.3")
		}
	})

	t.Run("with server name", func(t *testing.T) {
		cfg, err := loadTLSConfig(TLSConfig{
			ServerName: "example.com",
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if cfg.ServerName != "example.com" {
			t.Errorf("expected ServerName='example.com', got %q", cfg.ServerName)
		}
	})

	t.Run("with client cert", func(t *testing.T) {
		cfg, err := loadTLSConfig(TLSConfig{
			CertFile: certFile,
			KeyFile:  keyFile,
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(cfg.Certificates) != 1 {
			t.Errorf("expected 1 certificate, got %d", len(cfg.Certificates))
		}
	})

	t.Run("with CA cert", func(t *testing.T) {
		cfg, err := loadTLSConfig(TLSConfig{
			CAFile: caFile,
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if cfg.RootCAs == nil {
			t.Error("expected RootCAs to be set")
		}
	})

	t.Run("invalid CA content", func(t *testing.T) {
		tmpDir, _ := os.MkdirTemp("", "tls-test-*")
		defer os.RemoveAll(tmpDir)

		invalidCA := filepath.Join(tmpDir, "invalid-ca.pem")
		os.WriteFile(invalidCA, []byte("not a valid certificate"), 0600)

		_, err := loadTLSConfig(TLSConfig{
			CAFile: invalidCA,
		})
		if err == nil {
			t.Fatal("expected error for invalid CA content")
		}
	})
}

func TestTLSConfig_Struct(t *testing.T) {
	cfg := TLSConfig{
		CertFile:   "/path/to/cert.pem",
		KeyFile:    "/path/to/key.pem",
		CAFile:     "/path/to/ca.pem",
		ServerName: "server.example.com",
	}

	if cfg.CertFile != "/path/to/cert.pem" {
		t.Error("CertFile not set correctly")
	}
	if cfg.KeyFile != "/path/to/key.pem" {
		t.Error("KeyFile not set correctly")
	}
	if cfg.CAFile != "/path/to/ca.pem" {
		t.Error("CAFile not set correctly")
	}
	if cfg.ServerName != "server.example.com" {
		t.Error("ServerName not set correctly")
	}
}

func TestTransportTypeConstants(t *testing.T) {
	// Verify transport type constants are properly defined and ordered
	if TransportGRPC != 0 {
		t.Errorf("expected TransportGRPC=0, got %d", TransportGRPC)
	}
	if TransportREST != 1 {
		t.Errorf("expected TransportREST=1, got %d", TransportREST)
	}
	if TransportQUIC != 2 {
		t.Errorf("expected TransportQUIC=2, got %d", TransportQUIC)
	}
	if TransportUnix != 3 {
		t.Errorf("expected TransportUnix=3, got %d", TransportUnix)
	}
	if TransportEmbedded != 4 {
		t.Errorf("expected TransportEmbedded=4, got %d", TransportEmbedded)
	}
}

// TestWithAuthToken verifies the transport-agnostic platform token option sets
// the shared authToken field that every network transport reads.
func TestWithAuthToken(t *testing.T) {
	t.Run("sets token", func(t *testing.T) {
		opts := defaultOptions()
		if err := WithAuthToken("platform-token-abc")(opts); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.authToken != "platform-token-abc" {
			t.Errorf("expected authToken to be set, got %q", opts.authToken)
		}
	})

	t.Run("empty token is allowed (anonymous)", func(t *testing.T) {
		opts := defaultOptions()
		if err := WithAuthToken("")(opts); err != nil {
			t.Fatalf("unexpected error for empty token: %v", err)
		}
		if opts.authToken != "" {
			t.Errorf("expected empty authToken, got %q", opts.authToken)
		}
	})
}
