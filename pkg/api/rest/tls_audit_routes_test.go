//go:build rest

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

package rest

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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// =============================================================================
// Test Helpers for TLS Certificate Generation
// =============================================================================

// testCertificate holds a certificate and its key for testing.
type testCertificate struct {
	CertPEM []byte
	KeyPEM  []byte
}

// generateTestCertificate creates a self-signed test certificate with PEM encoding.
func generateTestCertificate(t *testing.T, cn string, isCA bool) *testCertificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err, "failed to generate ECDSA key")

	serial, err := rand.Int(rand.Reader, big.NewInt(1000000))
	require.NoError(t, err, "failed to generate serial number")

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   cn,
			Organization: []string{"QuicBaaS Test"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  isCA,
		DNSNames:              []string{"localhost"},
	}

	if isCA {
		template.KeyUsage |= x509.KeyUsageCertSign | x509.KeyUsageCRLSign
		template.MaxPathLen = 1
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err, "failed to create certificate")

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	require.NotNil(t, certPEM, "failed to encode certificate PEM")

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err, "failed to marshal private key")

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	require.NotNil(t, keyPEM, "failed to encode key PEM")

	return &testCertificate{
		CertPEM: certPEM,
		KeyPEM:  keyPEM,
	}
}

// writeTestCerts writes test certificate files to a temp directory and returns paths.
func writeTestCerts(t *testing.T, dir string, cert *testCertificate) (certPath, keyPath string) {
	t.Helper()

	certPath = filepath.Join(dir, "server.crt")
	keyPath = filepath.Join(dir, "server.key")

	err := os.WriteFile(certPath, cert.CertPEM, 0600)
	require.NoError(t, err, "failed to write certificate file")

	err = os.WriteFile(keyPath, cert.KeyPEM, 0600)
	require.NoError(t, err, "failed to write key file")

	return certPath, keyPath
}

// =============================================================================
// buildTLSConfig Tests
// =============================================================================

func TestBuildTLSConfig_Success(t *testing.T) {
	tmpDir := testutil.CreateTempDir(t)

	// Generate and write test certificate
	serverCert := generateTestCertificate(t, "localhost", false)
	certPath, keyPath := writeTestCerts(t, tmpDir, serverCert)

	// Create server with TLS config
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockService := testutil.NewMockQuicBaaS()
	config := DefaultConfig()
	config.TLSEnabled = true
	config.TLSCertFile = certPath
	config.TLSKeyFile = keyPath

	server, err := NewServer(mockService, config, log)
	require.NoError(t, err)
	defer server.Close()

	// Call buildTLSConfig
	tlsConfig, err := server.buildTLSConfig()

	assert.NoError(t, err)
	assert.NotNil(t, tlsConfig)
	assert.Len(t, tlsConfig.Certificates, 1)
	assert.Equal(t, uint16(tls.VersionTLS13), tlsConfig.MinVersion)
}

func TestBuildTLSConfig_InvalidCertFile(t *testing.T) {
	tmpDir := testutil.CreateTempDir(t)

	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockService := testutil.NewMockQuicBaaS()
	config := DefaultConfig()
	config.TLSEnabled = true
	config.TLSCertFile = filepath.Join(tmpDir, "nonexistent.crt")
	config.TLSKeyFile = filepath.Join(tmpDir, "nonexistent.key")

	server, err := NewServer(mockService, config, log)
	require.NoError(t, err)
	defer server.Close()

	// Call buildTLSConfig - should fail because cert files don't exist
	tlsConfig, err := server.buildTLSConfig()

	assert.Error(t, err)
	assert.Nil(t, tlsConfig)
	assert.Contains(t, err.Error(), "load server certificate failed")
}

func TestBuildTLSConfig_MismatchedKeyPair(t *testing.T) {
	tmpDir := testutil.CreateTempDir(t)

	// Generate two different certificates (mismatched key pair)
	cert1 := generateTestCertificate(t, "server1", false)
	cert2 := generateTestCertificate(t, "server2", false)

	certPath := filepath.Join(tmpDir, "server.crt")
	keyPath := filepath.Join(tmpDir, "server.key")

	// Write cert from cert1 but key from cert2 (mismatched)
	err := os.WriteFile(certPath, cert1.CertPEM, 0600)
	require.NoError(t, err)
	err = os.WriteFile(keyPath, cert2.KeyPEM, 0600)
	require.NoError(t, err)

	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockService := testutil.NewMockQuicBaaS()
	config := DefaultConfig()
	config.TLSEnabled = true
	config.TLSCertFile = certPath
	config.TLSKeyFile = keyPath

	server, err := NewServer(mockService, config, log)
	require.NoError(t, err)
	defer server.Close()

	// Call buildTLSConfig - should fail due to mismatched key pair
	tlsConfig, err := server.buildTLSConfig()

	assert.Error(t, err)
	assert.Nil(t, tlsConfig)
	assert.Contains(t, err.Error(), "load server certificate failed")
}

func TestBuildTLSConfig_WithClientAuth_Success(t *testing.T) {
	tmpDir := testutil.CreateTempDir(t)

	// Generate server certificate
	serverCert := generateTestCertificate(t, "server", false)
	certPath, keyPath := writeTestCerts(t, tmpDir, serverCert)

	// Generate CA certificate
	caCert := generateTestCertificate(t, "Test CA", true)
	caPath := filepath.Join(tmpDir, "ca.crt")
	err := os.WriteFile(caPath, caCert.CertPEM, 0600)
	require.NoError(t, err)

	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockService := testutil.NewMockQuicBaaS()
	config := DefaultConfig()
	config.TLSEnabled = true
	config.TLSCertFile = certPath
	config.TLSKeyFile = keyPath
	config.TLSClientAuth = true
	config.TLSCAFile = caPath

	server, err := NewServer(mockService, config, log)
	require.NoError(t, err)
	defer server.Close()

	// Call buildTLSConfig
	tlsConfig, err := server.buildTLSConfig()

	assert.NoError(t, err)
	assert.NotNil(t, tlsConfig)
	assert.Equal(t, tls.RequireAndVerifyClientCert, tlsConfig.ClientAuth)
	assert.NotNil(t, tlsConfig.ClientCAs)
}

func TestBuildTLSConfig_WithClientAuth_MissingCAFile(t *testing.T) {
	tmpDir := testutil.CreateTempDir(t)

	// Generate and write server certificate
	serverCert := generateTestCertificate(t, "server", false)
	certPath, keyPath := writeTestCerts(t, tmpDir, serverCert)

	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockService := testutil.NewMockQuicBaaS()
	config := DefaultConfig()
	config.TLSEnabled = true
	config.TLSCertFile = certPath
	config.TLSKeyFile = keyPath
	config.TLSClientAuth = true
	config.TLSCAFile = "" // Missing CA file

	server, err := NewServer(mockService, config, log)
	require.NoError(t, err)
	defer server.Close()

	// Call buildTLSConfig - should fail because CA file not specified
	tlsConfig, err := server.buildTLSConfig()

	assert.Error(t, err)
	assert.Nil(t, tlsConfig)
	assert.Contains(t, err.Error(), "TLSCAFile is required")
}

func TestBuildTLSConfig_WithClientAuth_InvalidCAFile(t *testing.T) {
	tmpDir := testutil.CreateTempDir(t)

	// Generate and write server certificate
	serverCert := generateTestCertificate(t, "server", false)
	certPath, keyPath := writeTestCerts(t, tmpDir, serverCert)

	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockService := testutil.NewMockQuicBaaS()
	config := DefaultConfig()
	config.TLSEnabled = true
	config.TLSCertFile = certPath
	config.TLSKeyFile = keyPath
	config.TLSClientAuth = true
	config.TLSCAFile = filepath.Join(tmpDir, "nonexistent_ca.crt")

	server, err := NewServer(mockService, config, log)
	require.NoError(t, err)
	defer server.Close()

	// Call buildTLSConfig - should fail because CA file doesn't exist
	tlsConfig, err := server.buildTLSConfig()

	assert.Error(t, err)
	assert.Nil(t, tlsConfig)
	assert.Contains(t, err.Error(), "read CA certificate failed")
}

func TestBuildTLSConfig_WithClientAuth_InvalidCAPEM(t *testing.T) {
	tmpDir := testutil.CreateTempDir(t)

	// Generate and write server certificate
	serverCert := generateTestCertificate(t, "server", false)
	certPath, keyPath := writeTestCerts(t, tmpDir, serverCert)

	// Write an invalid CA file (not valid PEM)
	caPath := filepath.Join(tmpDir, "invalid_ca.crt")
	err := os.WriteFile(caPath, []byte("not a valid PEM certificate"), 0600)
	require.NoError(t, err)

	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockService := testutil.NewMockQuicBaaS()
	config := DefaultConfig()
	config.TLSEnabled = true
	config.TLSCertFile = certPath
	config.TLSKeyFile = keyPath
	config.TLSClientAuth = true
	config.TLSCAFile = caPath

	server, err := NewServer(mockService, config, log)
	require.NoError(t, err)
	defer server.Close()

	// Call buildTLSConfig - should fail because CA PEM is invalid
	tlsConfig, err := server.buildTLSConfig()

	assert.Error(t, err)
	assert.Nil(t, tlsConfig)
	assert.Contains(t, err.Error(), "invalid CA certificate format")
}
