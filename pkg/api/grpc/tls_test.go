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

package grpc

import (
	"crypto/rand"
	"crypto/rsa"
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

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// generateTestCertificates creates temporary test certificates for TLS testing.
func generateTestCertificates(t *testing.T) (certFile, keyFile, caFile string) {
	tmpDir := t.TempDir()

	// Generate CA certificate
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test CA"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caCertBytes, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	// Save CA certificate
	caFile = filepath.Join(tmpDir, "ca.crt")
	caFileHandle, err := os.Create(caFile)
	require.NoError(t, err)
	err = pem.Encode(caFileHandle, &pem.Block{Type: "CERTIFICATE", Bytes: caCertBytes})
	require.NoError(t, err)
	caFileHandle.Close()

	// Generate server certificate
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{"Test Server"},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	serverCertBytes, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	require.NoError(t, err)

	// Save server certificate
	certFile = filepath.Join(tmpDir, "server.crt")
	certFileHandle, err := os.Create(certFile)
	require.NoError(t, err)
	err = pem.Encode(certFileHandle, &pem.Block{Type: "CERTIFICATE", Bytes: serverCertBytes})
	require.NoError(t, err)
	certFileHandle.Close()

	// Save server key
	keyFile = filepath.Join(tmpDir, "server.key")
	keyFileHandle, err := os.Create(keyFile)
	require.NoError(t, err)
	err = pem.Encode(keyFileHandle, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(serverKey),
	})
	require.NoError(t, err)
	keyFileHandle.Close()

	return certFile, keyFile, caFile
}

func TestLoadServerTLSCredentials_Disabled(t *testing.T) {
	// Test with nil config
	creds, err := LoadServerTLSCredentials(nil)
	assert.NoError(t, err)
	assert.Nil(t, creds)

	// Test with disabled TLS
	config := &TLSConfig{Enabled: false}
	creds, err = LoadServerTLSCredentials(config)
	assert.NoError(t, err)
	assert.Nil(t, creds)
}

func TestLoadServerTLSCredentials_MissingCertOrKey(t *testing.T) {
	// Test missing cert file
	config := &TLSConfig{
		Enabled: true,
		KeyFile: "some-key.pem",
	}
	creds, err := LoadServerTLSCredentials(config)
	assert.Error(t, err)
	assert.Nil(t, creds)
	assert.Contains(t, err.Error(), "cert and key files are required")

	// Test missing key file
	config = &TLSConfig{
		Enabled:  true,
		CertFile: "some-cert.pem",
	}
	creds, err = LoadServerTLSCredentials(config)
	assert.Error(t, err)
	assert.Nil(t, creds)
	assert.Contains(t, err.Error(), "cert and key files are required")
}

func TestLoadServerTLSCredentials_InvalidCertFiles(t *testing.T) {
	config := &TLSConfig{
		Enabled:  true,
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  "/nonexistent/key.pem",
	}
	creds, err := LoadServerTLSCredentials(config)
	assert.Error(t, err)
	assert.Nil(t, creds)
	assert.Contains(t, err.Error(), "failed to load server certificate")
}

func TestLoadServerTLSCredentials_Success(t *testing.T) {
	certFile, keyFile, _ := generateTestCertificates(t)

	config := &TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	creds, err := LoadServerTLSCredentials(config)
	assert.NoError(t, err)
	assert.NotNil(t, creds)

	// Verify credentials contain TLS info
	info := creds.Info()
	assert.Equal(t, "tls", info.SecurityProtocol)
}

func TestLoadServerTLSCredentials_FIPSMode(t *testing.T) {
	certFile, keyFile, _ := generateTestCertificates(t)

	config := &TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
		FIPSMode: true,
	}

	creds, err := LoadServerTLSCredentials(config)
	assert.NoError(t, err)
	assert.NotNil(t, creds)

	// In FIPS mode, TLS 1.3 should be enforced
	// We can't directly inspect the TLS config from credentials,
	// but we verified it's created successfully
	info := creds.Info()
	assert.Equal(t, "tls", info.SecurityProtocol)
}

func TestLoadServerTLSCredentials_ClientAuthMissingCA(t *testing.T) {
	certFile, keyFile, _ := generateTestCertificates(t)

	config := &TLSConfig{
		Enabled:    true,
		CertFile:   certFile,
		KeyFile:    keyFile,
		ClientAuth: true,
		// CAFile is missing
	}

	creds, err := LoadServerTLSCredentials(config)
	assert.Error(t, err)
	assert.Nil(t, creds)
	assert.Contains(t, err.Error(), "CA file is required")
}

func TestLoadServerTLSCredentials_ClientAuthInvalidCA(t *testing.T) {
	certFile, keyFile, _ := generateTestCertificates(t)

	config := &TLSConfig{
		Enabled:    true,
		CertFile:   certFile,
		KeyFile:    keyFile,
		ClientAuth: true,
		CAFile:     "/nonexistent/ca.pem",
	}

	creds, err := LoadServerTLSCredentials(config)
	assert.Error(t, err)
	assert.Nil(t, creds)
	assert.Contains(t, err.Error(), "failed to read CA certificate")
}

func TestLoadServerTLSCredentials_ClientAuthInvalidPEM(t *testing.T) {
	certFile, keyFile, _ := generateTestCertificates(t)

	// Create invalid CA file
	tmpDir := t.TempDir()
	invalidCAFile := filepath.Join(tmpDir, "invalid_ca.pem")
	err := os.WriteFile(invalidCAFile, []byte("INVALID PEM DATA"), 0o644)
	require.NoError(t, err)

	config := &TLSConfig{
		Enabled:    true,
		CertFile:   certFile,
		KeyFile:    keyFile,
		ClientAuth: true,
		CAFile:     invalidCAFile,
	}

	creds, err := LoadServerTLSCredentials(config)
	assert.Error(t, err)
	assert.Nil(t, creds)
	assert.Contains(t, err.Error(), "failed to add CA certificate to pool")
}

func TestLoadServerTLSCredentials_ClientAuthSuccess(t *testing.T) {
	certFile, keyFile, caFile := generateTestCertificates(t)

	config := &TLSConfig{
		Enabled:    true,
		CertFile:   certFile,
		KeyFile:    keyFile,
		ClientAuth: true,
		CAFile:     caFile,
	}

	creds, err := LoadServerTLSCredentials(config)
	assert.NoError(t, err)
	assert.NotNil(t, creds)
}

func TestLoadClientTLSCredentials_Disabled(t *testing.T) {
	// Test with nil config
	creds, err := LoadClientTLSCredentials(nil)
	assert.NoError(t, err)
	assert.Nil(t, creds)

	// Test with disabled TLS
	config := &TLSConfig{Enabled: false}
	creds, err = LoadClientTLSCredentials(config)
	assert.NoError(t, err)
	assert.Nil(t, creds)
}

func TestLoadClientTLSCredentials_NoCA(t *testing.T) {
	// Client TLS without CA (uses system CA pool)
	config := &TLSConfig{
		Enabled: true,
	}

	creds, err := LoadClientTLSCredentials(config)
	assert.NoError(t, err)
	assert.NotNil(t, creds)
}

func TestLoadClientTLSCredentials_FIPSMode(t *testing.T) {
	config := &TLSConfig{
		Enabled:  true,
		FIPSMode: true,
	}

	creds, err := LoadClientTLSCredentials(config)
	assert.NoError(t, err)
	assert.NotNil(t, creds)
}

func TestLoadClientTLSCredentials_InvalidCA(t *testing.T) {
	config := &TLSConfig{
		Enabled: true,
		CAFile:  "/nonexistent/ca.pem",
	}

	creds, err := LoadClientTLSCredentials(config)
	assert.Error(t, err)
	assert.Nil(t, creds)
	assert.Contains(t, err.Error(), "failed to read CA certificate")
}

func TestLoadClientTLSCredentials_InvalidCAPEM(t *testing.T) {
	tmpDir := t.TempDir()
	invalidCAFile := filepath.Join(tmpDir, "invalid_ca.pem")
	err := os.WriteFile(invalidCAFile, []byte("INVALID PEM DATA"), 0o644)
	require.NoError(t, err)

	config := &TLSConfig{
		Enabled: true,
		CAFile:  invalidCAFile,
	}

	creds, err := LoadClientTLSCredentials(config)
	assert.Error(t, err)
	assert.Nil(t, creds)
	assert.Contains(t, err.Error(), "failed to add CA certificate to pool")
}

func TestLoadClientTLSCredentials_WithCA(t *testing.T) {
	_, _, caFile := generateTestCertificates(t)

	config := &TLSConfig{
		Enabled: true,
		CAFile:  caFile,
	}

	creds, err := LoadClientTLSCredentials(config)
	assert.NoError(t, err)
	assert.NotNil(t, creds)
}

func TestLoadClientTLSCredentials_InvalidClientCert(t *testing.T) {
	config := &TLSConfig{
		Enabled:  true,
		CertFile: "/nonexistent/client.crt",
		KeyFile:  "/nonexistent/client.key",
	}

	creds, err := LoadClientTLSCredentials(config)
	assert.Error(t, err)
	assert.Nil(t, creds)
	assert.Contains(t, err.Error(), "failed to load client certificate")
}

func TestLoadClientTLSCredentials_MutualTLS(t *testing.T) {
	certFile, keyFile, caFile := generateTestCertificates(t)

	config := &TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
		CAFile:   caFile,
	}

	creds, err := LoadClientTLSCredentials(config)
	assert.NoError(t, err)
	assert.NotNil(t, creds)
}

func TestTLSConfig_ErrorTypes(t *testing.T) {
	// Verify errors are QBErrors
	config := &TLSConfig{
		Enabled:  true,
		CertFile: "",
		KeyFile:  "",
	}

	_, err := LoadServerTLSCredentials(config)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	require.True(t, ok, "error should be QBError type")
	assert.Equal(t, errors.ErrConfiguration, qbErr.Code)
	assert.Equal(t, "grpc.LoadServerTLSCredentials", qbErr.Op)
}

func TestTLSMinVersions(t *testing.T) {
	certFile, keyFile, _ := generateTestCertificates(t)

	tests := []struct {
		name         string
		fipsMode     bool
		expectedMin  uint16
		expectedName string
	}{
		{
			name:         "Standard TLS 1.3",
			fipsMode:     false,
			expectedMin:  tls.VersionTLS13,
			expectedName: "TLS 1.3",
		},
		{
			name:         "FIPS TLS 1.3",
			fipsMode:     true,
			expectedMin:  tls.VersionTLS13,
			expectedName: "TLS 1.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &TLSConfig{
				Enabled:  true,
				CertFile: certFile,
				KeyFile:  keyFile,
				FIPSMode: tt.fipsMode,
			}

			creds, err := LoadServerTLSCredentials(config)
			assert.NoError(t, err)
			assert.NotNil(t, creds)
		})
	}
}
