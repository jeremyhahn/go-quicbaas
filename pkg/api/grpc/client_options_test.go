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
	"crypto/tls"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func TestWithKeepalive(t *testing.T) {
	tests := []struct {
		name        string
		time        time.Duration
		timeout     time.Duration
		expectError bool
	}{
		{
			name:        "valid keepalive",
			time:        30 * time.Second,
			timeout:     10 * time.Second,
			expectError: false,
		},
		{
			name:        "zero time",
			time:        0,
			timeout:     10 * time.Second,
			expectError: true,
		},
		{
			name:        "zero timeout",
			time:        30 * time.Second,
			timeout:     0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := defaultClientOptions()
			err := WithKeepalive(tt.time, tt.timeout)(opts)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.time, opts.keepaliveTime)
				assert.Equal(t, tt.timeout, opts.keepaliveTimeout)
			}
		})
	}
}

func TestWithTLSConfig(t *testing.T) {
	tests := []struct {
		name        string
		tlsConfig   *tls.Config
		expectError bool
	}{
		{
			name: "valid TLS config",
			tlsConfig: &tls.Config{
				MinVersion: tls.VersionTLS13,
			},
			expectError: false,
		},
		{
			name:        "nil TLS config",
			tlsConfig:   nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := defaultClientOptions()
			err := WithTLSConfig(tt.tlsConfig)(opts)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.True(t, opts.tlsEnabled)
				assert.Equal(t, tt.tlsConfig, opts.tlsConfig)
			}
		})
	}
}

func TestWithDialOptions(t *testing.T) {
	t.Run("add custom dial options", func(t *testing.T) {
		opts := defaultClientOptions()
		initialLen := len(opts.dialOptions)

		err := WithDialOptions()(opts)
		assert.NoError(t, err)
		assert.Equal(t, initialLen, len(opts.dialOptions))
	})
}

func TestWithLogger_NilLogger(t *testing.T) {
	opts := defaultClientOptions()
	err := WithLogger(nil)(opts)
	assert.Error(t, err)
}

func TestWithLogger_ValidLogger(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	assert.NoError(t, err)

	opts := defaultClientOptions()
	err = WithLogger(log)(opts)
	assert.NoError(t, err)
	assert.Equal(t, log, opts.logger)
}

func TestWithMetadata_EmptyKey(t *testing.T) {
	opts := defaultClientOptions()
	err := WithMetadata("", "value")(opts)
	assert.Error(t, err)
}

func TestWithMetadata_ValidKeyValue(t *testing.T) {
	opts := defaultClientOptions()
	err := WithMetadata("key", "value")(opts)
	assert.NoError(t, err)
	assert.Equal(t, "value", opts.metadata["key"])
}

func TestWithMaxRetryBackoff_Invalid(t *testing.T) {
	opts := defaultClientOptions()
	err := WithMaxRetryBackoff(0)(opts)
	assert.Error(t, err)
}

func TestWithMaxRetryBackoff_Valid(t *testing.T) {
	opts := defaultClientOptions()
	err := WithMaxRetryBackoff(10 * time.Second)(opts)
	assert.NoError(t, err)
	assert.Equal(t, 10*time.Second, opts.maxRetryBackoff)
}

func TestWithRetry_NegativeRetries(t *testing.T) {
	opts := defaultClientOptions()
	err := WithRetry(-1, 100*time.Millisecond)(opts)
	assert.Error(t, err)
}

func TestWithRetry_ZeroBackoff(t *testing.T) {
	opts := defaultClientOptions()
	err := WithRetry(3, 0)(opts)
	assert.Error(t, err)
}

func TestWithRetry_Valid(t *testing.T) {
	opts := defaultClientOptions()
	err := WithRetry(5, 200*time.Millisecond)(opts)
	assert.NoError(t, err)
	assert.Equal(t, 5, opts.maxRetries)
	assert.Equal(t, 200*time.Millisecond, opts.retryBackoff)
}

func TestBuildDialOptions(t *testing.T) {
	t.Run("with_TLS", func(t *testing.T) {
		opts := defaultClientOptions()
		opts.tlsEnabled = true
		opts.tlsConfig = &tls.Config{
			MinVersion: tls.VersionTLS13,
		}

		dialOpts, err := opts.buildDialOptions()
		assert.NoError(t, err)
		assert.NotNil(t, dialOpts)
	})

	t.Run("without_TLS", func(t *testing.T) {
		opts := defaultClientOptions()
		opts.tlsEnabled = false

		dialOpts, err := opts.buildDialOptions()
		assert.Error(t, err)
		assert.Nil(t, dialOpts)
		assert.Contains(t, err.Error(), "TLS configuration is required")
	})
}

func TestWithTLS(t *testing.T) {
	// Helper function to create test certificates
	createTestCerts := func(t *testing.T) (certFile, keyFile, caFile string) {
		// Use the existing helper from tls_test.go
		return generateTestCertificates(t)
	}

	t.Run("valid TLS with cert and key", func(t *testing.T) {
		certFile, keyFile, _ := createTestCerts(t)

		opts := defaultClientOptions()
		err := WithTLS(certFile, keyFile, "")(opts)

		assert.NoError(t, err)
		assert.True(t, opts.tlsEnabled)
		assert.Equal(t, certFile, opts.certFile)
		assert.Equal(t, keyFile, opts.keyFile)
		assert.NotNil(t, opts.tlsConfig)
		assert.Equal(t, tls.VersionTLS13, int(opts.tlsConfig.MinVersion))
	})

	t.Run("valid TLS with cert, key, and CA", func(t *testing.T) {
		certFile, keyFile, caFile := createTestCerts(t)

		opts := defaultClientOptions()
		err := WithTLS(certFile, keyFile, caFile)(opts)

		assert.NoError(t, err)
		assert.True(t, opts.tlsEnabled)
		assert.Equal(t, certFile, opts.certFile)
		assert.Equal(t, keyFile, opts.keyFile)
		assert.Equal(t, caFile, opts.caFile)
		assert.NotNil(t, opts.tlsConfig)
		assert.NotNil(t, opts.tlsConfig.RootCAs)
	})

	t.Run("missing cert file", func(t *testing.T) {
		_, keyFile, _ := createTestCerts(t)

		opts := defaultClientOptions()
		err := WithTLS("", keyFile, "")(opts)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "certFile and keyFile are required")
	})

	t.Run("missing key file", func(t *testing.T) {
		certFile, _, _ := createTestCerts(t)

		opts := defaultClientOptions()
		err := WithTLS(certFile, "", "")(opts)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "certFile and keyFile are required")
	})

	t.Run("invalid cert file path", func(t *testing.T) {
		opts := defaultClientOptions()
		err := WithTLS("/nonexistent/cert.pem", "/nonexistent/key.pem", "")(opts)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load client certificate")
	})

	t.Run("invalid CA file path", func(t *testing.T) {
		certFile, keyFile, _ := createTestCerts(t)

		opts := defaultClientOptions()
		err := WithTLS(certFile, keyFile, "/nonexistent/ca.pem")(opts)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read CA certificate")
	})

	t.Run("invalid CA PEM data", func(t *testing.T) {
		certFile, keyFile, _ := createTestCerts(t)

		// Create invalid CA file
		tmpDir := t.TempDir()
		invalidCAFile := tmpDir + "/invalid_ca.pem"
		err := os.WriteFile(invalidCAFile, []byte("INVALID PEM DATA"), 0o644)
		assert.NoError(t, err)

		opts := defaultClientOptions()
		err = WithTLS(certFile, keyFile, invalidCAFile)(opts)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to append CA certificate")
	})
}
