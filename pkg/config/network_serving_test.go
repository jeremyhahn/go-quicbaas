// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// networkServingTestConfig returns a config that validates clean with
// security TLS enabled (empty cert/key paths auto-provision via the built-in
// CA), ready for the network-serving permutations below.
func networkServingTestConfig(t *testing.T) *Config {
	t.Helper()
	cfg := Default()
	tmpDir := t.TempDir()
	cfg.Storage.DataDir = tmpDir
	cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
	cfg.Storage.Backend = StorageBackendPebble
	cfg.Security.TLSEnabled = true
	require.NoError(t, Validate(cfg), "base config must validate clean before network-serving permutations")
	return cfg
}

// TestNetworkServing_EnabledWithTLSAndPorts proves the happy path: any of
// REST/GRPC/MCP enabled with TLS enabled and a valid port validates clean,
// and NetworkServing() reports true.
func TestNetworkServing_EnabledWithTLSAndPorts(t *testing.T) {
	cfg := networkServingTestConfig(t)
	cfg.Server.REST.Enabled = true
	cfg.Server.REST.Port = 18443
	cfg.Server.GRPC.Enabled = true
	cfg.Server.GRPC.Port = 19443
	cfg.Server.MCP.Enabled = true
	cfg.Server.MCP.Port = 17443

	assert.NoError(t, Validate(cfg))
	assert.True(t, cfg.NetworkServing())
}

// TestNetworkServing_RequiresTLS proves the fail-closed rule: enabling any
// network protocol without security TLS is refused — the embedded qrdb
// node's unified server transports are TLS-only, so the listeners could
// never serve a handshake.
func TestNetworkServing_RequiresTLS(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*ServerConfig)
	}{
		{"rest enabled", func(s *ServerConfig) { s.REST.Enabled = true; s.REST.Port = 18443 }},
		{"grpc enabled", func(s *ServerConfig) { s.GRPC.Enabled = true; s.GRPC.Port = 19443 }},
		{"mcp enabled", func(s *ServerConfig) { s.MCP.Enabled = true; s.MCP.Port = 17443 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := networkServingTestConfig(t)
			cfg.Security.TLSEnabled = false
			tt.modify(&cfg.Server)

			err := Validate(cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "security.tls_enabled")
			assert.Contains(t, err.Error(), "TLS")
		})
	}
}

// TestNetworkServing_DisabledIgnoresTLS proves that with every protocol
// disabled, NetworkServing() reports false and the config validates clean
// regardless of the TLS setting — no network listener means no TLS-only
// constraint to enforce.
func TestNetworkServing_DisabledIgnoresTLS(t *testing.T) {
	cfg := networkServingTestConfig(t)
	cfg.Security.TLSEnabled = false
	cfg.Server.REST.Enabled = false
	cfg.Server.GRPC.Enabled = false
	cfg.Server.MCP.Enabled = false

	assert.NoError(t, Validate(cfg))
	assert.False(t, cfg.NetworkServing())
}

// TestNetworkServing_PerProtocol proves NetworkServing() is true when ANY
// single protocol is enabled, not only when all three are.
func TestNetworkServing_PerProtocol(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*ServerConfig)
	}{
		{"rest only", func(s *ServerConfig) { s.REST.Enabled = true; s.REST.Port = 18443 }},
		{"grpc only", func(s *ServerConfig) { s.GRPC.Enabled = true; s.GRPC.Port = 19443 }},
		{"mcp only", func(s *ServerConfig) { s.MCP.Enabled = true; s.MCP.Port = 17443 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := networkServingTestConfig(t)
			tt.modify(&cfg.Server)

			assert.NoError(t, Validate(cfg))
			assert.True(t, cfg.NetworkServing())
		})
	}
}
