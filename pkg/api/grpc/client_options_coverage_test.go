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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/common/circuitbreaker"
)

// TestWithCircuitBreaker tests the WithCircuitBreaker client option.
func TestWithCircuitBreaker(t *testing.T) {
	t.Run("valid_config_sets_circuit_breaker", func(t *testing.T) {
		cfg := circuitbreaker.Config{
			Name:             "test-cb",
			FailureThreshold: 5,
			SuccessThreshold: 2,
			OpenTimeout:      30 * time.Second,
		}
		opt := WithCircuitBreaker(cfg)
		opts := &clientOptions{}
		err := opt(opts)
		require.NoError(t, err)
		require.NotNil(t, opts.circuitBreakerConfig)
		assert.Equal(t, "test-cb", opts.circuitBreakerConfig.Name)
	})

	t.Run("empty_name_returns_error", func(t *testing.T) {
		cfg := circuitbreaker.Config{
			FailureThreshold: 5,
			SuccessThreshold: 2,
			OpenTimeout:      30 * time.Second,
		}
		opt := WithCircuitBreaker(cfg)
		opts := &clientOptions{}
		err := opt(opts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name must not be empty")
	})

	t.Run("zero_failure_threshold_returns_error", func(t *testing.T) {
		cfg := circuitbreaker.Config{
			Name:             "test-cb",
			FailureThreshold: 0,
			SuccessThreshold: 2,
			OpenTimeout:      30 * time.Second,
		}
		opt := WithCircuitBreaker(cfg)
		opts := &clientOptions{}
		err := opt(opts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failure threshold must be positive")
	})

	t.Run("zero_success_threshold_returns_error", func(t *testing.T) {
		cfg := circuitbreaker.Config{
			Name:             "test-cb",
			FailureThreshold: 5,
			SuccessThreshold: 0,
			OpenTimeout:      30 * time.Second,
		}
		opt := WithCircuitBreaker(cfg)
		opts := &clientOptions{}
		err := opt(opts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "success threshold must be positive")
	})

	t.Run("zero_open_timeout_returns_error", func(t *testing.T) {
		cfg := circuitbreaker.Config{
			Name:             "test-cb",
			FailureThreshold: 5,
			SuccessThreshold: 2,
			OpenTimeout:      0,
		}
		opt := WithCircuitBreaker(cfg)
		opts := &clientOptions{}
		err := opt(opts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "open timeout must be positive")
	})
}

// TestBuildDialOptions tests the buildDialOptions method of clientOptions.
func TestBuildDialOptionsCoverage(t *testing.T) {
	t.Run("no_tls_returns_error", func(t *testing.T) {
		opts := &clientOptions{
			keepaliveTime:    30 * time.Second,
			keepaliveTimeout: 10 * time.Second,
		}
		_, err := opts.buildDialOptions()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TLS configuration is required")
	})

	t.Run("tls_config_uses_provided_config", func(t *testing.T) {
		opts := &clientOptions{
			tlsConfig:        &tls.Config{MinVersion: tls.VersionTLS13},
			keepaliveTime:    30 * time.Second,
			keepaliveTimeout: 10 * time.Second,
		}
		dialOpts, err := opts.buildDialOptions()
		require.NoError(t, err)
		assert.NotEmpty(t, dialOpts)
	})

	t.Run("cert_files_with_nonexistent_files_returns_error", func(t *testing.T) {
		opts := &clientOptions{
			certFile:         "/nonexistent/cert.pem",
			keyFile:          "/nonexistent/key.pem",
			keepaliveTime:    30 * time.Second,
			keepaliveTimeout: 10 * time.Second,
		}
		_, err := opts.buildDialOptions()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load TLS credentials")
	})
}
