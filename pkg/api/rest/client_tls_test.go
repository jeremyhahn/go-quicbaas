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
	"crypto/tls"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func TestWithTLS(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	t.Run("sets up TLS without certificates", func(t *testing.T) {
		client, err := NewRESTClient("http://localhost:8080", log, WithTLS("", "", ""))
		require.NoError(t, err)
		defer client.Close()

		transport, ok := client.httpClient.Transport.(*http.Transport)
		require.True(t, ok)
		assert.NotNil(t, transport.TLSClientConfig)
		assert.Equal(t, uint16(tls.VersionTLS13), transport.TLSClientConfig.MinVersion)
	})

	t.Run("returns error for non-existent cert file", func(t *testing.T) {
		_, err := NewRESTClient("http://localhost:8080", log, WithTLS("/nonexistent/cert.pem", "/nonexistent/key.pem", ""))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "load client certificates failed")
	})

	t.Run("returns error for non-existent CA file", func(t *testing.T) {
		_, err := NewRESTClient("http://localhost:8080", log, WithTLS("", "", "/nonexistent/ca.pem"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read CA certificate failed")
	})

	t.Run("returns error for invalid CA certificate", func(t *testing.T) {
		// Create a temp file with invalid certificate content
		tmpDir := t.TempDir()
		caFile := filepath.Join(tmpDir, "invalid_ca.pem")
		err := os.WriteFile(caFile, []byte("not a valid certificate"), 0600)
		require.NoError(t, err)

		_, err = NewRESTClient("http://localhost:8080", log, WithTLS("", "", caFile))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid CA certificate format")
	})
}

func TestWithTLSConfig(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	t.Run("returns error for nil config", func(t *testing.T) {
		_, err := NewRESTClient("http://localhost:8080", log, WithTLSConfig(nil))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tlsConfig cannot be nil")
	})

	t.Run("sets TLS config correctly", func(t *testing.T) {
		tlsConfig := &tls.Config{
			MinVersion:             tls.VersionTLS13,
			SessionTicketsDisabled: true,
		}
		client, err := NewRESTClient("http://localhost:8080", log, WithTLSConfig(tlsConfig))
		require.NoError(t, err)
		defer client.Close()

		transport, ok := client.httpClient.Transport.(*http.Transport)
		require.True(t, ok)
		assert.Equal(t, uint16(tls.VersionTLS13), transport.TLSClientConfig.MinVersion)
		assert.True(t, transport.TLSClientConfig.SessionTicketsDisabled)
	})
}

func TestWithTimeout(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	t.Run("returns error for zero timeout", func(t *testing.T) {
		_, err := NewRESTClient("http://localhost:8080", log, WithTimeout(0))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout must be positive")
	})

	t.Run("returns error for negative timeout", func(t *testing.T) {
		_, err := NewRESTClient("http://localhost:8080", log, WithTimeout(-1))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout must be positive")
	})
}
