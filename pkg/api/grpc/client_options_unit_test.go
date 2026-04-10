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
)

func TestWithTimeout(t *testing.T) {
	tests := []struct {
		name        string
		timeout     time.Duration
		expectError bool
	}{
		{
			name:        "valid timeout",
			timeout:     5 * time.Second,
			expectError: false,
		},
		{
			name:        "zero timeout",
			timeout:     0,
			expectError: true,
		},
		{
			name:        "negative timeout",
			timeout:     -1 * time.Second,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := defaultClientOptions()
			err := WithTimeout(tt.timeout)(opts)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.timeout, opts.timeout)
			}
		})
	}
}

func TestWithConnectionPool(t *testing.T) {
	tests := []struct {
		name        string
		minSize     int
		maxSize     int
		expectError bool
	}{
		{
			name:        "valid pool size",
			minSize:     5,
			maxSize:     20,
			expectError: false,
		},
		{
			name:        "min equals max",
			minSize:     10,
			maxSize:     10,
			expectError: false,
		},
		{
			name:        "min size zero",
			minSize:     0,
			maxSize:     10,
			expectError: true,
		},
		{
			name:        "min size negative",
			minSize:     -1,
			maxSize:     10,
			expectError: true,
		},
		{
			name:        "max less than min",
			minSize:     20,
			maxSize:     10,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := defaultClientOptions()
			err := WithConnectionPool(tt.minSize, tt.maxSize)(opts)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.True(t, opts.usePool)
				assert.Equal(t, tt.minSize, opts.poolMinSize)
				assert.Equal(t, tt.maxSize, opts.poolMaxSize)
			}
		})
	}
}

func TestWithPoolMaxConnectionAge(t *testing.T) {
	tests := []struct {
		name        string
		duration    time.Duration
		expectError bool
	}{
		{
			name:        "valid duration",
			duration:    30 * time.Minute,
			expectError: false,
		},
		{
			name:        "zero duration",
			duration:    0,
			expectError: true,
		},
		{
			name:        "negative duration",
			duration:    -1 * time.Minute,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := defaultClientOptions()
			err := WithPoolMaxConnectionAge(tt.duration)(opts)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.duration, opts.poolMaxAge)
			}
		})
	}
}

func TestWithPoolMaxRequestsPerConn(t *testing.T) {
	tests := []struct {
		name        string
		count       uint64
		expectError bool
	}{
		{
			name:        "valid count",
			count:       100000,
			expectError: false,
		},
		{
			name:        "zero count",
			count:       0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := defaultClientOptions()
			err := WithPoolMaxRequestsPerConn(tt.count)(opts)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.count, opts.poolMaxRequests)
			}
		})
	}
}

func TestWithPoolHealthCheckInterval(t *testing.T) {
	tests := []struct {
		name        string
		duration    time.Duration
		expectError bool
	}{
		{
			name:        "valid duration",
			duration:    30 * time.Second,
			expectError: false,
		},
		{
			name:        "zero duration (disabled)",
			duration:    0,
			expectError: false,
		},
		{
			name:        "negative duration",
			duration:    -1 * time.Second,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := defaultClientOptions()
			err := WithPoolHealthCheckInterval(tt.duration)(opts)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.duration, opts.poolHealthCheck)
			}
		})
	}
}

func TestBuildDialOptions_TLSEnabled(t *testing.T) {
	t.Run("TLS enabled but no credentials", func(t *testing.T) {
		opts := defaultClientOptions()
		opts.tlsEnabled = true
		opts.tlsConfig = nil
		opts.certFile = ""
		opts.keyFile = ""

		_, err := opts.buildDialOptions()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "TLS configuration is required")
	})
}

func TestBuildDialOptions_ServiceConfig(t *testing.T) {
	t.Run("builds service config with retry policy", func(t *testing.T) {
		opts := defaultClientOptions()
		opts.tlsEnabled = true
		opts.tlsConfig = &tls.Config{MinVersion: tls.VersionTLS13}
		opts.maxRetries = 5
		opts.retryBackoff = 200 * time.Millisecond
		opts.maxRetryBackoff = 10 * time.Second

		dialOpts, err := opts.buildDialOptions()
		require.NoError(t, err)
		assert.NotEmpty(t, dialOpts)
	})
}
