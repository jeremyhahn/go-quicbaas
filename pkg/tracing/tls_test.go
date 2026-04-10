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

package tracing

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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
)

// TestTLSConfigValidation tests TLS configuration validation.
func TestTLSConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		wantErr     bool
		errContains string
	}{
		{
			name: "TLS disabled - no validation needed",
			config: &Config{
				Enabled:       false,
				ServiceName:   "test-service",
				SamplingRatio: 1.0,
				TLS: &TLSConfig{
					Enabled: false,
				},
			},
			wantErr: false,
		},
		{
			name: "TLS enabled - valid config with CA only",
			config: &Config{
				Enabled:          true,
				ServiceName:      "test-service",
				ExporterType:     ExporterStdout,
				SamplingRatio:    1.0,
				ExporterEndpoint: "",
				TLS: &TLSConfig{
					Enabled: true,
					CAFile:  "/path/to/ca.crt",
				},
			},
			wantErr: false,
		},
		{
			name: "TLS enabled - missing key file when cert provided",
			config: &Config{
				Enabled:          true,
				ServiceName:      "test-service",
				ExporterType:     ExporterOTLP,
				ExporterEndpoint: "localhost:4317",
				SamplingRatio:    1.0,
				TLS: &TLSConfig{
					Enabled:  true,
					CertFile: "/path/to/cert.crt",
					KeyFile:  "",
				},
			},
			wantErr:     true,
			errContains: "TLS key file is required",
		},
		{
			name: "TLS enabled - missing cert file when key provided",
			config: &Config{
				Enabled:          true,
				ServiceName:      "test-service",
				ExporterType:     ExporterOTLP,
				ExporterEndpoint: "localhost:4317",
				SamplingRatio:    1.0,
				TLS: &TLSConfig{
					Enabled:  true,
					CertFile: "",
					KeyFile:  "/path/to/key.pem",
				},
			},
			wantErr:     true,
			errContains: "TLS cert file is required",
		},
		{
			name: "TLS enabled - valid mTLS config",
			config: &Config{
				Enabled:          true,
				ServiceName:      "test-service",
				ExporterType:     ExporterStdout,
				SamplingRatio:    1.0,
				ExporterEndpoint: "",
				TLS: &TLSConfig{
					Enabled:  true,
					CertFile: "/path/to/cert.crt",
					KeyFile:  "/path/to/key.pem",
					CAFile:   "/path/to/ca.crt",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.config)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidateTLSConfig tests the validateTLSConfig function directly.
func TestValidateTLSConfig(t *testing.T) {
	tests := []struct {
		name        string
		tlsConfig   *TLSConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid - empty config",
			tlsConfig: &TLSConfig{
				Enabled: true,
			},
			wantErr: false,
		},
		{
			name: "valid - CA only",
			tlsConfig: &TLSConfig{
				Enabled: true,
				CAFile:  "/path/to/ca.crt",
			},
			wantErr: false,
		},
		{
			name: "valid - full mTLS config",
			tlsConfig: &TLSConfig{
				Enabled:  true,
				CertFile: "/path/to/cert.crt",
				KeyFile:  "/path/to/key.pem",
				CAFile:   "/path/to/ca.crt",
			},
			wantErr: false,
		},
		{
			name: "invalid - cert without key",
			tlsConfig: &TLSConfig{
				Enabled:  true,
				CertFile: "/path/to/cert.crt",
			},
			wantErr:     true,
			errContains: "TLS key file is required",
		},
		{
			name: "invalid - key without cert",
			tlsConfig: &TLSConfig{
				Enabled: true,
				KeyFile: "/path/to/key.pem",
			},
			wantErr:     true,
			errContains: "TLS cert file is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTLSConfig(tt.tlsConfig)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestLoadOTLPTLSCredentials tests the loadOTLPTLSCredentials function with real certificates.
func TestLoadOTLPTLSCredentials(t *testing.T) {
	// Create temporary directory for test certificates
	tempDir := t.TempDir()

	// Generate test CA certificate
	caKey, caCert := generateTestCA(t)
	caCertPath := filepath.Join(tempDir, "ca.crt")
	writeTestCert(t, caCertPath, caCert)

	// Generate test client certificate signed by CA
	clientCertPath := filepath.Join(tempDir, "client.crt")
	clientKeyPath := filepath.Join(tempDir, "client.key")
	generateTestClientCert(t, caKey, caCert, clientCertPath, clientKeyPath)

	tests := []struct {
		name        string
		tlsConfig   *TLSConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid - CA only",
			tlsConfig: &TLSConfig{
				Enabled: true,
				CAFile:  caCertPath,
			},
			wantErr: false,
		},
		{
			name: "valid - full mTLS",
			tlsConfig: &TLSConfig{
				Enabled:  true,
				CertFile: clientCertPath,
				KeyFile:  clientKeyPath,
				CAFile:   caCertPath,
			},
			wantErr: false,
		},
		{
			name: "valid - client cert only (no CA)",
			tlsConfig: &TLSConfig{
				Enabled:  true,
				CertFile: clientCertPath,
				KeyFile:  clientKeyPath,
			},
			wantErr: false,
		},
		{
			name: "error - CA file not found",
			tlsConfig: &TLSConfig{
				Enabled: true,
				CAFile:  "/nonexistent/ca.crt",
			},
			wantErr:     true,
			errContains: "failed to read CA certificate",
		},
		{
			name: "error - client cert not found",
			tlsConfig: &TLSConfig{
				Enabled:  true,
				CertFile: "/nonexistent/client.crt",
				KeyFile:  clientKeyPath,
			},
			wantErr:     true,
			errContains: "failed to load client certificate",
		},
		{
			name: "error - client key not found",
			tlsConfig: &TLSConfig{
				Enabled:  true,
				CertFile: clientCertPath,
				KeyFile:  "/nonexistent/client.key",
			},
			wantErr:     true,
			errContains: "failed to load client certificate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := loadOTLPTLSCredentials(tt.tlsConfig)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, creds)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, creds)
			}
		})
	}
}

// TestLoadOTLPTLSCredentials_InvalidCACert tests loading with an invalid CA certificate.
func TestLoadOTLPTLSCredentials_InvalidCACert(t *testing.T) {
	tempDir := t.TempDir()

	// Write invalid CA certificate (not a valid PEM)
	invalidCAPath := filepath.Join(tempDir, "invalid-ca.crt")
	err := os.WriteFile(invalidCAPath, []byte("not a valid certificate"), 0600)
	require.NoError(t, err)

	tlsConfig := &TLSConfig{
		Enabled: true,
		CAFile:  invalidCAPath,
	}

	creds, err := loadOTLPTLSCredentials(tlsConfig)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add CA certificate")
	assert.Nil(t, creds)
}

// TestTLSErrorTypes tests the TLS error types.
func TestTLSErrorTypes(t *testing.T) {
	t.Run("TLSCAReadError", func(t *testing.T) {
		err := &TLSCAReadError{
			Path: "/path/to/ca.crt",
			Err:  os.ErrNotExist,
		}
		assert.Contains(t, err.Error(), "/path/to/ca.crt")
		assert.Contains(t, err.Error(), "failed to read CA certificate")
		assert.ErrorIs(t, err.Unwrap(), os.ErrNotExist)
	})

	t.Run("TLSCAParseError", func(t *testing.T) {
		err := &TLSCAParseError{
			Path: "/path/to/ca.crt",
		}
		assert.Contains(t, err.Error(), "/path/to/ca.crt")
		assert.Contains(t, err.Error(), "failed to add CA certificate")
	})

	t.Run("TLSCertLoadError", func(t *testing.T) {
		err := &TLSCertLoadError{
			CertPath: "/path/to/cert.crt",
			KeyPath:  "/path/to/key.pem",
			Err:      os.ErrNotExist,
		}
		assert.Contains(t, err.Error(), "/path/to/cert.crt")
		assert.Contains(t, err.Error(), "/path/to/key.pem")
		assert.Contains(t, err.Error(), "failed to load client certificate")
		assert.ErrorIs(t, err.Unwrap(), os.ErrNotExist)
	})
}

// TestTLSConfigStruct tests the TLSConfig struct fields.
func TestTLSConfigStruct(t *testing.T) {
	tlsConfig := &TLSConfig{
		Enabled:  true,
		CertFile: "/path/to/cert.crt",
		KeyFile:  "/path/to/key.pem",
		CAFile:   "/path/to/ca.crt",
	}

	assert.True(t, tlsConfig.Enabled)
	assert.Equal(t, "/path/to/cert.crt", tlsConfig.CertFile)
	assert.Equal(t, "/path/to/key.pem", tlsConfig.KeyFile)
	assert.Equal(t, "/path/to/ca.crt", tlsConfig.CAFile)
}

// TestConfigWithTLS tests the Config struct with TLS configuration.
func TestConfigWithTLS(t *testing.T) {
	cfg := &Config{
		Enabled:          true,
		ServiceName:      "test-service",
		ServiceVersion:   "1.0.0",
		ExporterType:     ExporterOTLP,
		ExporterEndpoint: "localhost:4317",
		SamplingRatio:    1.0,
		Environment:      "test",
		NodeID:           1,
		ShutdownTimeout:  5 * time.Second,
		TLS: &TLSConfig{
			Enabled:  true,
			CertFile: "/path/to/cert.crt",
			KeyFile:  "/path/to/key.pem",
			CAFile:   "/path/to/ca.crt",
		},
	}

	assert.NotNil(t, cfg.TLS)
	assert.True(t, cfg.TLS.Enabled)
	assert.Equal(t, "/path/to/cert.crt", cfg.TLS.CertFile)
	assert.Equal(t, "/path/to/key.pem", cfg.TLS.KeyFile)
	assert.Equal(t, "/path/to/ca.crt", cfg.TLS.CAFile)
}

// Helper functions for generating test certificates

func generateTestCA(t *testing.T) (*ecdsa.PrivateKey, *x509.Certificate) {
	t.Helper()

	// Generate CA private key
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Create CA certificate template
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test CA"},
			CommonName:   "Test CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// Self-sign the CA certificate
	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	caCert, err := x509.ParseCertificate(caCertDER)
	require.NoError(t, err)

	return caKey, caCert
}

func generateTestClientCert(t *testing.T, caKey *ecdsa.PrivateKey, caCert *x509.Certificate, certPath, keyPath string) {
	t.Helper()

	// Generate client private key
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Create client certificate template
	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{"Test Client"},
			CommonName:   "Test Client",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	// Sign with CA
	clientCertDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caCert, &clientKey.PublicKey, caKey)
	require.NoError(t, err)

	// Write client certificate
	certFile, err := os.Create(certPath)
	require.NoError(t, err)
	defer certFile.Close()

	err = pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: clientCertDER})
	require.NoError(t, err)

	// Write client key
	keyFile, err := os.Create(keyPath)
	require.NoError(t, err)
	defer keyFile.Close()

	clientKeyBytes, err := x509.MarshalECPrivateKey(clientKey)
	require.NoError(t, err)

	err = pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: clientKeyBytes})
	require.NoError(t, err)
}

func writeTestCert(t *testing.T, path string, cert *x509.Certificate) {
	t.Helper()

	certFile, err := os.Create(path)
	require.NoError(t, err)
	defer certFile.Close()

	err = pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	require.NoError(t, err)
}
