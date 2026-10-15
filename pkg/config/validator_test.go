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

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateValidConfig(t *testing.T) {
	cfg := Default()

	// Use temp directory for testing
	tmpDir := t.TempDir()
	cfg.Storage.DataDir = tmpDir
	cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
	cfg.Storage.Backend = StorageBackendPebble

	// Disable TLS for this test (it requires cert/key files). Network
	// serving requires TLS (validateNetworkServingTLS), so also disable it.
	cfg.Security.TLSEnabled = false
	cfg.Server.REST.Enabled = false
	cfg.Server.GRPC.Enabled = false
	cfg.Server.MCP.Enabled = false

	err := Validate(cfg)
	assert.NoError(t, err)
}

func TestValidateNilConfig(t *testing.T) {
	err := Validate(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "configuration is nil")
}

func TestValidateServerConfig(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(*Config)
		expectError bool
		errorField  string
	}{
		{
			name: "valid server config",
			modify: func(cfg *Config) {
				cfg.Server.Host = "0.0.0.0"
				cfg.Server.REST.Enabled = true
				cfg.Server.REST.Port = 8080
				cfg.Server.GRPC.Enabled = true
				cfg.Server.GRPC.Port = 9090
				// Network serving requires TLS (validateNetworkServingTLS).
				cfg.Security.TLSEnabled = true
			},
			expectError: false,
		},
		{
			name: "empty host",
			modify: func(cfg *Config) {
				cfg.Server.Host = ""
			},
			expectError: true,
			errorField:  "server.host",
		},
		{
			name: "invalid REST port - too low",
			modify: func(cfg *Config) {
				cfg.Server.REST.Enabled = true
				cfg.Server.REST.Port = 0
			},
			expectError: true,
			errorField:  "server.rest.port",
		},
		{
			name: "invalid REST port - too high",
			modify: func(cfg *Config) {
				cfg.Server.REST.Enabled = true
				cfg.Server.REST.Port = 70000
			},
			expectError: true,
			errorField:  "server.rest.port",
		},
		{
			name: "REST rate limit opt-out is valid",
			modify: func(cfg *Config) {
				cfg.Server.REST.Enabled = true
				cfg.Server.REST.RateLimit = -1
				cfg.Security.TLSEnabled = true
			},
			expectError: false,
		},
		{
			name: "REST rate limit below the opt-out value",
			modify: func(cfg *Config) {
				cfg.Server.REST.Enabled = true
				cfg.Server.REST.RateLimit = -2
			},
			expectError: true,
			errorField:  "server.rest.rate_limit",
		},
		{
			name: "invalid gRPC port",
			modify: func(cfg *Config) {
				cfg.Server.GRPC.Enabled = true
				cfg.Server.GRPC.Port = -1
			},
			expectError: true,
			errorField:  "server.grpc.port",
		},
		{
			name: "same REST and gRPC port",
			modify: func(cfg *Config) {
				cfg.Server.REST.Enabled = true
				cfg.Server.REST.Port = 8080
				cfg.Server.GRPC.Enabled = true
				cfg.Server.GRPC.Port = 8080
			},
			expectError: true,
			errorField:  "server.grpc.port",
		},
		{
			name: "invalid MCP port",
			modify: func(cfg *Config) {
				cfg.Server.MCP.Enabled = true
				cfg.Server.MCP.Port = 0
			},
			expectError: true,
			errorField:  "server.mcp.port",
		},
		{
			name: "invalid read timeout",
			modify: func(cfg *Config) {
				cfg.Server.ReadTimeout = 0
			},
			expectError: true,
			errorField:  "server.read_timeout",
		},
		{
			name: "invalid max connections",
			modify: func(cfg *Config) {
				cfg.Server.MaxConnections = 0
			},
			expectError: true,
			errorField:  "server.max_connections",
		},
		{
			name: "disabled protocols skip port validation",
			modify: func(cfg *Config) {
				cfg.Server.REST.Enabled = false
				cfg.Server.REST.Port = 0
				cfg.Server.GRPC.Enabled = false
				cfg.Server.GRPC.Port = 0
				cfg.Server.MCP.Enabled = false
				cfg.Server.MCP.Port = 0
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tmpDir := t.TempDir()
			cfg.Storage.DataDir = tmpDir
			cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
			cfg.Storage.Backend = StorageBackendPebble

			// Disable raft to avoid raft validation issues
			cfg.Raft.NodeID = 0

			// Disable TLS for tests that do not expect security errors. Network
			// serving requires TLS (validateNetworkServingTLS), so also disable
			// it here — these tests target unrelated config sections.
			cfg.Security.TLSEnabled = false
			cfg.Server.REST.Enabled = false
			cfg.Server.GRPC.Enabled = false
			cfg.Server.MCP.Enabled = false

			tt.modify(cfg)

			err := Validate(cfg)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorField)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateServerMode(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(*Config)
		expectError bool
		errorField  string
	}{
		{
			name: "valid standalone mode (default)",
			modify: func(cfg *Config) {
				cfg.Server.Mode = ServerModeStandalone
			},
			expectError: false,
		},
		{
			name: "valid cluster mode with pebble backend",
			modify: func(cfg *Config) {
				cfg.Server.Mode = ServerModeCluster
				cfg.Storage.Backend = StorageBackendPebble
				cfg.Raft.NodeID = 1
			},
			expectError: false,
		},
		{
			name: "valid devcluster mode with pebble backend",
			modify: func(cfg *Config) {
				cfg.Server.Mode = ServerModeDevCluster
				cfg.Storage.Backend = StorageBackendPebble
				cfg.Raft.NodeID = 1
			},
			expectError: false,
		},
		{
			name: "empty mode defaults to standalone",
			modify: func(cfg *Config) {
				cfg.Server.Mode = ""
			},
			expectError: false,
		},
		{
			name: "invalid server mode",
			modify: func(cfg *Config) {
				cfg.Server.Mode = "invalid"
			},
			expectError: true,
			errorField:  "server.mode",
		},
		{
			name: "raft backend is no longer valid - use mode instead",
			modify: func(cfg *Config) {
				cfg.Server.Mode = ServerModeCluster
				cfg.Storage.Backend = "raft"
				cfg.Raft.NodeID = 1
			},
			expectError: true,
			errorField:  "storage.backend",
		},
		{
			name: "pebble backend with cluster mode is valid",
			modify: func(cfg *Config) {
				cfg.Server.Mode = ServerModeCluster
				cfg.Storage.Backend = StorageBackendPebble
				cfg.Raft.NodeID = 1 // Required for cluster mode
			},
			expectError: false,
		},
		{
			name: "additional SAN IPs in any notation are valid",
			modify: func(cfg *Config) {
				cfg.Server.AdditionalSANIPs = []string{"192.0.2.10", "0:0:0:0:0:0:0:1"}
			},
			expectError: false,
		},
		{
			name: "invalid additional SAN IP",
			modify: func(cfg *Config) {
				cfg.Server.AdditionalSANIPs = []string{"192.0.2.10", "192.0.2.300"}
			},
			expectError: true,
			errorField:  "server.additional_san_ips",
		},
		{
			name: "memory backend with cluster mode is valid",
			modify: func(cfg *Config) {
				cfg.Server.Mode = ServerModeCluster
				cfg.Storage.Backend = StorageBackendMemory
				cfg.Raft.NodeID = 1 // Required for cluster mode
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tmpDir := t.TempDir()
			cfg.Storage.DataDir = tmpDir
			cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
			cfg.Storage.Backend = StorageBackendPebble

			// Disable raft to avoid raft validation issues by default
			cfg.Raft.NodeID = 0

			// Disable TLS for tests that do not expect security errors. Network
			// serving requires TLS (validateNetworkServingTLS), so also disable
			// it here — these tests target unrelated config sections.
			cfg.Security.TLSEnabled = false
			cfg.Server.REST.Enabled = false
			cfg.Server.GRPC.Enabled = false
			cfg.Server.MCP.Enabled = false

			tt.modify(cfg)

			err := Validate(cfg)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorField)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateRaftConfig(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(*Config)
		expectError bool
		errorField  string
	}{
		{
			name: "valid raft config",
			modify: func(cfg *Config) {
				cfg.Raft.NodeID = 1
				cfg.Raft.ElectionRTT = 10
				cfg.Raft.HeartbeatRTT = 1
			},
			expectError: false,
		},
		{
			name: "zero node ID with cluster mode",
			modify: func(cfg *Config) {
				cfg.Server.Mode = ServerModeCluster
				cfg.Storage.Backend = StorageBackendPebble
				cfg.Raft.NodeID = 0
			},
			expectError: true,
			errorField:  "raft.node_id",
		},
		{
			name: "empty raft address with cluster mode",
			modify: func(cfg *Config) {
				cfg.Server.Mode = ServerModeCluster
				cfg.Storage.Backend = StorageBackendPebble
				cfg.Raft.NodeID = 1
				cfg.Raft.RaftAddress = ""
			},
			expectError: true,
			errorField:  "raft.raft_address",
		},
		{
			name: "skip raft validation for standalone mode",
			modify: func(cfg *Config) {
				cfg.Server.Mode = ServerModeStandalone
				cfg.Storage.Backend = StorageBackendPebble
				cfg.Raft.NodeID = 0
			},
			expectError: false,
		},
		{
			name: "election RTT too low",
			modify: func(cfg *Config) {
				cfg.Raft.NodeID = 1
				cfg.Raft.ElectionRTT = 0
			},
			expectError: true,
			errorField:  "raft.election_rtt",
		},
		{
			name: "heartbeat RTT too low",
			modify: func(cfg *Config) {
				cfg.Raft.NodeID = 1
				cfg.Raft.HeartbeatRTT = 0
			},
			expectError: true,
			errorField:  "raft.heartbeat_rtt",
		},
		{
			name: "election RTT not greater than heartbeat",
			modify: func(cfg *Config) {
				cfg.Raft.NodeID = 1
				cfg.Raft.ElectionRTT = 5
				cfg.Raft.HeartbeatRTT = 10
			},
			expectError: true,
			errorField:  "raft.election_rtt",
		},
		{
			name: "invalid snapshot entries",
			modify: func(cfg *Config) {
				cfg.Raft.NodeID = 1
				cfg.Raft.SnapshotEntries = 0
			},
			expectError: true,
			errorField:  "raft.snapshot_entries",
		},
		{
			name: "empty raft address",
			modify: func(cfg *Config) {
				cfg.Raft.NodeID = 1
				cfg.Raft.RaftAddress = ""
			},
			expectError: true,
			errorField:  "raft.raft_address",
		},
		{
			name: "empty listen settings bind the advertised host",
			modify: func(cfg *Config) {
				cfg.Raft.NodeID = 1
				cfg.Raft.ListenAddress = ""
				cfg.Raft.ListenHost = ""
				cfg.Raft.RaftAddress = "node1:63001"
			},
			expectError: false,
		},
		{
			name: "wildcard advertised host with no listen setting",
			modify: func(cfg *Config) {
				cfg.Raft.NodeID = 1
				cfg.Raft.ListenAddress = ""
				cfg.Raft.ListenHost = ""
				cfg.Raft.RaftAddress = "0.0.0.0:63001"
			},
			expectError: true,
			errorField:  "raft.listen_address",
		},
		{
			name: "empty advertised host with no listen setting",
			modify: func(cfg *Config) {
				cfg.Raft.NodeID = 1
				cfg.Raft.ListenAddress = ""
				cfg.Raft.ListenHost = ""
				cfg.Raft.RaftAddress = ":63001"
			},
			expectError: true,
			errorField:  "raft.listen_address",
		},
		{
			name: "wildcard advertised host with an explicit listen address",
			modify: func(cfg *Config) {
				cfg.Raft.NodeID = 1
				cfg.Raft.ListenAddress = "0.0.0.0:63001"
				cfg.Raft.RaftAddress = "0.0.0.0:63001"
			},
			expectError: false,
		},
		{
			name: "empty listen address with portless raft address",
			modify: func(cfg *Config) {
				cfg.Raft.NodeID = 1
				cfg.Raft.ListenAddress = ""
				cfg.Raft.RaftAddress = "node1"
			},
			expectError: true,
			errorField:  "raft.listen_address",
		},
		{
			name: "empty listen address derives from listen host",
			modify: func(cfg *Config) {
				cfg.Raft.NodeID = 1
				cfg.Raft.ListenAddress = ""
				cfg.Raft.ListenHost = "10.0.0.5"
			},
			expectError: false,
		},
		{
			name: "invalid max in-mem log size",
			modify: func(cfg *Config) {
				cfg.Raft.NodeID = 1
				cfg.Raft.MaxInMemLogSize = 1024 // Less than 1MB
			},
			expectError: true,
			errorField:  "raft.max_in_mem_log_size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tmpDir := t.TempDir()
			cfg.Storage.DataDir = tmpDir
			cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
			cfg.Storage.Backend = StorageBackendPebble

			// Disable TLS for tests that do not expect security errors. Network
			// serving requires TLS (validateNetworkServingTLS), so also disable
			// it here — these tests target unrelated config sections.
			cfg.Security.TLSEnabled = false
			cfg.Server.REST.Enabled = false
			cfg.Server.GRPC.Enabled = false
			cfg.Server.MCP.Enabled = false

			tt.modify(cfg)

			err := Validate(cfg)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorField)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateStorageConfig(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(*Config)
		expectError bool
		errorField  string
	}{
		{
			name: "valid storage config",
			modify: func(cfg *Config) {
				cfg.Storage.Backend = StorageBackendPebble
			},
			expectError: false,
		},
		{
			name: "empty storage backend",
			modify: func(cfg *Config) {
				cfg.Storage.Backend = ""
			},
			expectError: true,
			errorField:  "storage.backend",
		},
		{
			name: "invalid storage backend",
			modify: func(cfg *Config) {
				cfg.Storage.Backend = "invalid"
			},
			expectError: true,
			errorField:  "storage.backend",
		},
		{
			name: "valid memory backend",
			modify: func(cfg *Config) {
				cfg.Storage.Backend = StorageBackendMemory
			},
			expectError: false,
		},
		{
			name: "valid file backend",
			modify: func(cfg *Config) {
				cfg.Storage.Backend = StorageBackendFile
			},
			expectError: false,
		},
		{
			name: "valid pebble backend",
			modify: func(cfg *Config) {
				cfg.Storage.Backend = StorageBackendPebble
			},
			expectError: false,
		},
		{
			name: "raft backend is no longer valid",
			modify: func(cfg *Config) {
				cfg.Server.Mode = ServerModeCluster
				cfg.Storage.Backend = "raft"
				cfg.Raft.NodeID = 1
			},
			expectError: true,
			errorField:  "storage.backend",
		},
		{
			name: "memory backend skips directory validation",
			modify: func(cfg *Config) {
				cfg.Storage.Backend = StorageBackendMemory
				cfg.Storage.DataDir = ""
				cfg.Storage.WALDir = ""
			},
			expectError: false,
		},
		{
			name: "empty data directory",
			modify: func(cfg *Config) {
				cfg.Storage.Backend = StorageBackendPebble
				cfg.Storage.DataDir = ""
			},
			expectError: true,
			errorField:  "storage.data_dir",
		},
		{
			name: "empty WAL directory",
			modify: func(cfg *Config) {
				cfg.Storage.Backend = StorageBackendPebble
				cfg.Storage.WALDir = ""
			},
			expectError: true,
			errorField:  "storage.wal_dir",
		},
		{
			name: "cache size too small",
			modify: func(cfg *Config) {
				cfg.Storage.Backend = StorageBackendPebble
				cfg.Storage.CacheSize = 1024 // Less than 1MB
			},
			expectError: true,
			errorField:  "storage.cache_size",
		},
		{
			name: "max open files too low",
			modify: func(cfg *Config) {
				cfg.Storage.Backend = StorageBackendPebble
				cfg.Storage.MaxOpenFiles = 5
			},
			expectError: true,
			errorField:  "storage.max_open_files",
		},
		{
			name: "invalid compression type",
			modify: func(cfg *Config) {
				cfg.Storage.Backend = StorageBackendPebble
				cfg.Storage.CompressionType = "invalid"
			},
			expectError: true,
			errorField:  "storage.compression_type",
		},
		{
			name: "L0 stop threshold not greater than compaction threshold",
			modify: func(cfg *Config) {
				cfg.Storage.Backend = StorageBackendPebble
				cfg.Storage.L0CompactionThreshold = 10
				cfg.Storage.L0StopWritesThreshold = 5
			},
			expectError: true,
			errorField:  "storage.l0_stop_writes_threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tmpDir := t.TempDir()
			cfg.Storage.DataDir = tmpDir
			cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
			cfg.Storage.Backend = StorageBackendPebble

			// Disable raft to avoid raft validation issues
			cfg.Raft.NodeID = 0

			// Disable TLS for tests that do not expect security errors. Network
			// serving requires TLS (validateNetworkServingTLS), so also disable
			// it here — these tests target unrelated config sections.
			cfg.Security.TLSEnabled = false
			cfg.Server.REST.Enabled = false
			cfg.Server.GRPC.Enabled = false
			cfg.Server.MCP.Enabled = false

			tt.modify(cfg)

			err := Validate(cfg)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorField)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateSecurityConfig(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test certificate files
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	caFile := filepath.Join(tmpDir, "ca.pem")

	os.WriteFile(certFile, []byte("cert"), 0o644)
	os.WriteFile(keyFile, []byte("key"), 0o644)
	os.WriteFile(caFile, []byte("ca"), 0o644)

	tests := []struct {
		name        string
		modify      func(*Config)
		expectError bool
		errorField  string
	}{
		{
			name: "valid security config - disabled",
			modify: func(cfg *Config) {
				// Network serving requires TLS (validateNetworkServingTLS); also
				// disable it so this case isolates the security-config outcome.
				cfg.Security.TLSEnabled = false
				cfg.Server.REST.Enabled = false
				cfg.Server.GRPC.Enabled = false
				cfg.Server.MCP.Enabled = false
			},
			expectError: false,
		},
		{
			name: "valid TLS config",
			modify: func(cfg *Config) {
				cfg.Security.TLSEnabled = true
				cfg.Security.TLSCertFile = certFile
				cfg.Security.TLSKeyFile = keyFile
			},
			expectError: false,
		},
		{
			name: "TLS enabled but missing cert",
			modify: func(cfg *Config) {
				cfg.Security.TLSEnabled = true
				cfg.Security.TLSCertFile = ""
				cfg.Security.TLSKeyFile = keyFile
			},
			expectError: true,
			errorField:  "security.tls_cert_file",
		},
		{
			name: "TLS enabled but cert file doesn't exist",
			modify: func(cfg *Config) {
				cfg.Security.TLSEnabled = true
				cfg.Security.TLSCertFile = "/nonexistent/cert.pem"
				cfg.Security.TLSKeyFile = keyFile
			},
			expectError: true,
			errorField:  "security.tls_cert_file",
		},
		{
			name: "TLS enabled but missing key",
			modify: func(cfg *Config) {
				cfg.Security.TLSEnabled = true
				cfg.Security.TLSCertFile = certFile
				cfg.Security.TLSKeyFile = ""
			},
			expectError: true,
			errorField:  "security.tls_cert_file",
		},
		{
			name: "TLS enabled with no cert material auto-provisions via built-in CA",
			modify: func(cfg *Config) {
				cfg.Security.TLSEnabled = true
				cfg.Security.TLSCertFile = ""
				cfg.Security.TLSKeyFile = ""
			},
			expectError: false,
		},
		{
			name: "TLS enabled but key file doesn't exist",
			modify: func(cfg *Config) {
				cfg.Security.TLSEnabled = true
				cfg.Security.TLSCertFile = certFile
				cfg.Security.TLSKeyFile = "/nonexistent/key.pem"
			},
			expectError: true,
			errorField:  "security.tls_key_file",
		},
		{
			name: "client auth enabled but missing CA",
			modify: func(cfg *Config) {
				cfg.Security.TLSEnabled = true
				cfg.Security.TLSCertFile = certFile
				cfg.Security.TLSKeyFile = keyFile
				cfg.Security.TLSClientAuth = true
				cfg.Security.TLSCAFile = ""
			},
			expectError: true,
			errorField:  "security.tls_ca_file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			cfg.Storage.DataDir = tmpDir
			cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
			cfg.Storage.Backend = StorageBackendPebble

			// Disable raft to avoid raft validation issues
			cfg.Raft.NodeID = 0

			tt.modify(cfg)

			err := Validate(cfg)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorField)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateLoggingConfig(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(*Config)
		expectError bool
		errorField  string
	}{
		{
			name: "valid logging config",
			modify: func(cfg *Config) {
				cfg.Logging.Level = "info"
				cfg.Logging.Format = "json"
				cfg.Logging.Output = "stdout"
			},
			expectError: false,
		},
		{
			name: "invalid log level",
			modify: func(cfg *Config) {
				cfg.Logging.Level = "invalid"
			},
			expectError: true,
			errorField:  "logging.level",
		},
		{
			name: "invalid log format",
			modify: func(cfg *Config) {
				cfg.Logging.Format = "xml"
			},
			expectError: true,
			errorField:  "logging.format",
		},
		{
			name: "invalid log output",
			modify: func(cfg *Config) {
				cfg.Logging.Output = "network"
			},
			expectError: true,
			errorField:  "logging.output",
		},
		{
			name: "file output but no file specified",
			modify: func(cfg *Config) {
				cfg.Logging.Output = "file"
				cfg.Logging.File = ""
			},
			expectError: true,
			errorField:  "logging.file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tmpDir := t.TempDir()
			cfg.Storage.DataDir = tmpDir
			cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
			cfg.Storage.Backend = StorageBackendPebble

			// Disable raft to avoid raft validation issues
			cfg.Raft.NodeID = 0

			// Disable TLS for tests that do not expect security errors. Network
			// serving requires TLS (validateNetworkServingTLS), so also disable
			// it here — these tests target unrelated config sections.
			cfg.Security.TLSEnabled = false
			cfg.Server.REST.Enabled = false
			cfg.Server.GRPC.Enabled = false
			cfg.Server.MCP.Enabled = false

			tt.modify(cfg)

			err := Validate(cfg)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorField)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidationErrors(t *testing.T) {
	cfg := Default()
	tmpDir := t.TempDir()
	cfg.Storage.DataDir = tmpDir
	cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
	cfg.Storage.Backend = StorageBackendPebble

	// Create multiple validation errors
	cfg.Server.Host = ""
	cfg.Server.REST.Enabled = true
	cfg.Server.REST.Port = 0
	cfg.Logging.Level = "invalid"

	// Disable raft to avoid raft validation issues
	cfg.Raft.NodeID = 0

	err := Validate(cfg)

	require.Error(t, err)

	// Check that multiple errors are reported
	errMsg := err.Error()
	assert.Contains(t, errMsg, "server.host")
	assert.Contains(t, errMsg, "server.rest.port")
	assert.Contains(t, errMsg, "logging.level")
}

func TestValidationError(t *testing.T) {
	err := &ValidationError{
		Field:   "test.field",
		Message: "test message",
	}

	assert.Equal(t, "validation error for field 'test.field': test message", err.Error())
}

func TestValidationErrorsError(t *testing.T) {
	tests := []struct {
		name   string
		errors ValidationErrors
		want   string
	}{
		{
			name:   "empty errors",
			errors: ValidationErrors{},
			want:   "",
		},
		{
			name: "single error",
			errors: ValidationErrors{
				{Field: "field1", Message: "error1"},
			},
			want: "configuration validation failed with 1 error(s):",
		},
		{
			name: "multiple errors",
			errors: ValidationErrors{
				{Field: "field1", Message: "error1"},
				{Field: "field2", Message: "error2"},
			},
			want: "configuration validation failed with 2 error(s):",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.errors.Error()
			if tt.want == "" {
				assert.Equal(t, "", got)
			} else {
				assert.Contains(t, got, tt.want)
			}
		})
	}
}

func TestNewValidator(t *testing.T) {
	v := NewValidator()
	require.NotNil(t, v)
	require.NotNil(t, v.errors)
	assert.Equal(t, 0, len(v.errors))
}

func TestEnsureDirectoryWritable(t *testing.T) {
	v := NewValidator()

	t.Run("create new directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		newDir := filepath.Join(tmpDir, "new", "directory")

		err := v.ensureDirectoryWritable(newDir)
		assert.NoError(t, err)

		// Verify directory exists
		info, err := os.Stat(newDir)
		assert.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("existing writable directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		err := v.ensureDirectoryWritable(tmpDir)
		assert.NoError(t, err)
	})

	t.Run("path is file not directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		file := filepath.Join(tmpDir, "file.txt")
		os.WriteFile(file, []byte("test"), 0o644)

		err := v.ensureDirectoryWritable(file)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a directory")
	})
}

func TestValidateStorageBackendConstants(t *testing.T) {
	// Verify the storage backend constants are correctly defined
	// NOTE: "raft" is NO LONGER a valid backend - use server.mode=cluster for Raft consensus
	assert.Equal(t, "memory", StorageBackendMemory)
	assert.Equal(t, "file", StorageBackendFile)
	assert.Equal(t, "pebble", StorageBackendPebble)

	// Verify the validStorageBackends map contains valid backends
	assert.True(t, validStorageBackends[StorageBackendMemory])
	assert.True(t, validStorageBackends[StorageBackendFile])
	assert.True(t, validStorageBackends[StorageBackendPebble])
	assert.False(t, validStorageBackends["raft"]) // "raft" is no longer valid
	assert.False(t, validStorageBackends["invalid"])
}

func TestValidateServerModeConstants(t *testing.T) {
	// Verify the server mode constants are correctly defined
	assert.Equal(t, "standalone", ServerModeStandalone)
	assert.Equal(t, "cluster", ServerModeCluster)
	assert.Equal(t, "devcluster", ServerModeDevCluster)

	// Verify the validServerModes map contains all modes
	assert.True(t, validServerModes[ServerModeStandalone])
	assert.True(t, validServerModes[ServerModeCluster])
	assert.True(t, validServerModes[ServerModeDevCluster])
	assert.True(t, validServerModes[""]) // Empty defaults to standalone
	assert.False(t, validServerModes["invalid"])
}

func TestValidateRaftWithClusterMode(t *testing.T) {
	t.Run("cluster mode requires node_id", func(t *testing.T) {
		cfg := Default()
		tmpDir := t.TempDir()
		cfg.Storage.DataDir = tmpDir
		cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
		cfg.Server.Mode = ServerModeCluster
		cfg.Storage.Backend = StorageBackendPebble
		cfg.Raft.NodeID = 0

		// Disable TLS for this test. Network serving requires TLS
		// (validateNetworkServingTLS), so also disable it.
		cfg.Security.TLSEnabled = false
		cfg.Server.REST.Enabled = false
		cfg.Server.GRPC.Enabled = false
		cfg.Server.MCP.Enabled = false

		err := Validate(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "raft.node_id")
	})

	t.Run("cluster mode requires raft_address", func(t *testing.T) {
		cfg := Default()
		tmpDir := t.TempDir()
		cfg.Storage.DataDir = tmpDir
		cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
		cfg.Server.Mode = ServerModeCluster
		cfg.Storage.Backend = StorageBackendPebble
		cfg.Raft.NodeID = 1
		cfg.Raft.RaftAddress = ""

		// Disable TLS for this test. Network serving requires TLS
		// (validateNetworkServingTLS), so also disable it.
		cfg.Security.TLSEnabled = false
		cfg.Server.REST.Enabled = false
		cfg.Server.GRPC.Enabled = false
		cfg.Server.MCP.Enabled = false

		err := Validate(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "raft.raft_address")
	})

	t.Run("standalone mode skips raft validation when node_id is 0", func(t *testing.T) {
		cfg := Default()
		tmpDir := t.TempDir()
		cfg.Storage.DataDir = tmpDir
		cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
		cfg.Server.Mode = ServerModeStandalone
		cfg.Storage.Backend = StorageBackendPebble
		cfg.Raft.NodeID = 0

		// Disable TLS for this test. Network serving requires TLS
		// (validateNetworkServingTLS), so also disable it.
		cfg.Security.TLSEnabled = false
		cfg.Server.REST.Enabled = false
		cfg.Server.GRPC.Enabled = false
		cfg.Server.MCP.Enabled = false

		err := Validate(cfg)
		assert.NoError(t, err)
	})

	t.Run("memory backend with standalone mode skips raft validation", func(t *testing.T) {
		cfg := Default()
		cfg.Server.Mode = ServerModeStandalone
		cfg.Storage.Backend = StorageBackendMemory
		cfg.Raft.NodeID = 0

		// Disable TLS for this test. Network serving requires TLS
		// (validateNetworkServingTLS), so also disable it.
		cfg.Security.TLSEnabled = false
		cfg.Server.REST.Enabled = false
		cfg.Server.GRPC.Enabled = false
		cfg.Server.MCP.Enabled = false

		err := Validate(cfg)
		assert.NoError(t, err)
	})
}

func TestRaftBackendIsNoLongerValid(t *testing.T) {
	t.Run("raft backend returns clear error message", func(t *testing.T) {
		cfg := Default()
		tmpDir := t.TempDir()
		cfg.Storage.DataDir = tmpDir
		cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
		cfg.Storage.Backend = "raft" // No longer valid

		// Disable TLS for this test. Network serving requires TLS
		// (validateNetworkServingTLS), so also disable it.
		cfg.Security.TLSEnabled = false
		cfg.Server.REST.Enabled = false
		cfg.Server.GRPC.Enabled = false
		cfg.Server.MCP.Enabled = false

		err := Validate(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "storage.backend")
		assert.Contains(t, err.Error(), "no longer valid")
		assert.Contains(t, err.Error(), "server.mode")
	})
}

// newAuthzPostureConfig returns a baseline config that validates cleanly
// (writable storage, no TLS) so that the only validation outcome under test is
// the authorization-posture gate.
func newAuthzPostureConfig(t *testing.T) *Config {
	t.Helper()
	cfg := Default()
	tmpDir := t.TempDir()
	cfg.Storage.DataDir = tmpDir
	cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
	cfg.Storage.Backend = StorageBackendPebble
	// Baseline: no network serving, no TLS. Subtests that specifically
	// exercise network-serving posture enable REST/GRPC/MCP (and, per
	// validateNetworkServingTLS, TLS) themselves.
	cfg.Security.TLSEnabled = false
	cfg.Server.REST.Enabled = false
	cfg.Server.GRPC.Enabled = false
	cfg.Server.MCP.Enabled = false
	return cfg
}

func TestValidateAuthzPosture(t *testing.T) {
	t.Run("require_authz=false + REST enabled + no ack fails", func(t *testing.T) {
		cfg := newAuthzPostureConfig(t)
		cfg.Security.RequireAuthz = false
		cfg.Security.InsecureDevMode = false
		cfg.Server.REST.Enabled = true
		cfg.Server.GRPC.Enabled = false
		cfg.Server.MCP.Enabled = false

		err := Validate(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "security.require_authz")
		assert.Contains(t, err.Error(), "server.rest")
		assert.Contains(t, err.Error(), "insecure_dev_mode")
	})

	t.Run("require_authz=false + InsecureDevMode=true passes", func(t *testing.T) {
		cfg := newAuthzPostureConfig(t)
		cfg.Security.RequireAuthz = false
		cfg.Security.InsecureDevMode = true
		// Network serving requires TLS regardless of authz posture
		// (validateNetworkServingTLS); enable it here so this case isolates
		// the authz-posture outcome under test.
		cfg.Security.TLSEnabled = true
		cfg.Server.REST.Enabled = true

		err := Validate(cfg)
		assert.NoError(t, err)
	})

	t.Run("require_authz=false + all network transports disabled passes without ack", func(t *testing.T) {
		cfg := newAuthzPostureConfig(t)
		cfg.Security.RequireAuthz = false
		cfg.Security.InsecureDevMode = false
		cfg.Server.REST.Enabled = false
		cfg.Server.GRPC.Enabled = false
		cfg.Server.MCP.Enabled = false
		// Unix domain socket is NOT a network transport and must not trip the gate.
		cfg.Server.Unix.Enabled = true

		err := Validate(cfg)
		assert.NoError(t, err)
	})

	t.Run("require_authz=true is unaffected", func(t *testing.T) {
		cfg := newAuthzPostureConfig(t)
		cfg.Security.RequireAuthz = true
		cfg.Security.InsecureDevMode = false
		// Network serving requires TLS regardless of authz posture
		// (validateNetworkServingTLS); enable it here so this case isolates
		// the authz-posture outcome under test.
		cfg.Security.TLSEnabled = true
		cfg.Server.REST.Enabled = true
		cfg.Server.GRPC.Enabled = true
		cfg.Server.MCP.Enabled = true

		err := Validate(cfg)
		assert.NoError(t, err)
	})

	t.Run("error names every offending network transport", func(t *testing.T) {
		cfg := newAuthzPostureConfig(t)
		cfg.Security.RequireAuthz = false
		cfg.Security.InsecureDevMode = false
		cfg.Server.REST.Enabled = true
		cfg.Server.GRPC.Enabled = true
		cfg.Server.MCP.Enabled = true

		err := Validate(cfg)
		require.Error(t, err)
		for _, transport := range []string{"server.rest", "server.grpc", "server.mcp"} {
			assert.Contains(t, err.Error(), transport)
		}
	})

	t.Run("gRPC-only offending transport is named", func(t *testing.T) {
		cfg := newAuthzPostureConfig(t)
		cfg.Security.RequireAuthz = false
		cfg.Security.InsecureDevMode = false
		cfg.Server.REST.Enabled = false
		cfg.Server.GRPC.Enabled = true
		cfg.Server.MCP.Enabled = false

		err := Validate(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "server.grpc")
		assert.NotContains(t, err.Error(), "server.rest")
	})
}

func TestDevelopmentConfigValidatesClean(t *testing.T) {
	cfg := DevelopmentConfig()
	tmpDir := t.TempDir()
	cfg.Storage.DataDir = tmpDir
	cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
	cfg.Storage.Backend = StorageBackendMemory

	// DevelopmentConfig disables authz but enables network transports; it must
	// pass validation by virtue of acknowledging InsecureDevMode.
	require.False(t, cfg.Security.RequireAuthz)
	require.True(t, cfg.Security.InsecureDevMode)

	err := Validate(cfg)
	assert.NoError(t, err)
}

// TestDefaultConfigValidatesClean guards the quicbaasd boot gate: runDaemon
// refuses to start when config.Validate fails, so the production defaults must
// validate cleanly. Storage paths are redirected to a temp dir because the
// validator probes directory writability (the daemon creates the real data
// directory in prepareDaemon before Validate runs, mirroring this setup).
func TestDefaultConfigValidatesClean(t *testing.T) {
	cfg := DefaultConfig()
	tmpDir := t.TempDir()
	cfg.Storage.DataDir = tmpDir
	cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")

	// Production defaults are fail-closed: authz required, no dev-mode ack.
	require.True(t, cfg.Security.RequireAuthz)
	require.False(t, cfg.Security.InsecureDevMode)

	err := Validate(cfg)
	assert.NoError(t, err)
}

func TestLoaderBindsInsecureDevMode(t *testing.T) {
	t.Run("env true sets InsecureDevMode", func(t *testing.T) {
		t.Setenv("QUICBAAS_SECURITY_INSECURE_DEV_MODE", "true")
		cfg := Default()
		cfg.Security.InsecureDevMode = false
		require.NoError(t, NewLoader().applyEnvironmentOverrides(cfg))
		assert.True(t, cfg.Security.InsecureDevMode)
	})

	t.Run("env false sets InsecureDevMode", func(t *testing.T) {
		t.Setenv("QUICBAAS_SECURITY_INSECURE_DEV_MODE", "false")
		cfg := Default()
		cfg.Security.InsecureDevMode = true
		require.NoError(t, NewLoader().applyEnvironmentOverrides(cfg))
		assert.False(t, cfg.Security.InsecureDevMode)
	})

	t.Run("file binds insecure_dev_mode via mapstructure", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "quicbaas.yaml")
		require.NoError(t, os.WriteFile(path, []byte("security:\n  insecure_dev_mode: true\n"), 0o600))

		cfg, err := NewLoader().Load(path)
		require.NoError(t, err)
		assert.True(t, cfg.Security.InsecureDevMode)
	})
}

func BenchmarkValidate(b *testing.B) {
	cfg := Default()
	tmpDir := b.TempDir()
	cfg.Storage.DataDir = tmpDir
	cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
	cfg.Storage.Backend = StorageBackendPebble
	cfg.Raft.NodeID = 0

	// Disable TLS for benchmark. Network serving requires TLS
	// (validateNetworkServingTLS), so also disable it.
	cfg.Security.TLSEnabled = false
	cfg.Server.REST.Enabled = false
	cfg.Server.GRPC.Enabled = false
	cfg.Server.MCP.Enabled = false

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Validate(cfg)
	}
}

func BenchmarkValidateWithErrors(b *testing.B) {
	cfg := Default()
	cfg.Server.Host = ""
	cfg.Server.REST.Enabled = true
	cfg.Server.REST.Port = 0
	cfg.Storage.Backend = ""

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Validate(cfg)
	}
}

// TestValidateSDLC_RefusesTheZerosThatMeanNever: with the module enabled, the
// defaults validate and each setting's fail-open value is refused by name.
//
// Mutation evidence: dropping any one refusal fails its sub-test (the
// sub-test asserts the field appears in the error).
func TestValidateSDLC_RefusesTheZerosThatMeanNever(t *testing.T) {
	base := func() *Config {
		cfg := DefaultConfig()
		tmp := t.TempDir()
		cfg.Storage.DataDir = tmp
		cfg.Storage.WALDir = filepath.Join(tmp, "wal")
		cfg.SDLC.Enabled = true
		// Enabling the module now requires the node to say where it lives;
		// onboarding provisions forge webhooks pointing at it.
		cfg.SDLC.Webhook.PublicURL = "https://factory.test"
		return cfg
	}
	require.NoError(t, Validate(base()), "the enabled defaults must validate")

	disabled := base()
	disabled.SDLC.Enabled = false
	disabled.SDLC.MergeAttempts = 0
	require.NoError(t, Validate(disabled), "a disabled module is not validated")

	for name, tc := range map[string]struct {
		mutate func(*SDLCConfig)
		field  string
	}{
		"page limit":           {func(c *SDLCConfig) { c.IssuePageLimit = 0 }, "sdlc.issue_page_limit"},
		"merge attempts":       {func(c *SDLCConfig) { c.MergeAttempts = 0 }, "sdlc.merge_attempts"},
		"webhook body":         {func(c *SDLCConfig) { c.Webhook.MaxBody = 0 }, "sdlc.webhook.max_body"},
		"empty command":        {func(c *SDLCConfig) { c.VerificationCommands = []string{"make test", " "} }, "sdlc.verification_commands[1]"},
		"empty host":           {func(c *SDLCConfig) { c.RepoHosts = []string{""} }, "sdlc.repo_hosts[0]"},
		"negative retries":     {func(c *SDLCConfig) { c.Surfacing.RetryAttempts = -1 }, "sdlc.surfacing.retry_attempts"},
		"negative interval":    {func(c *SDLCConfig) { c.Surfacing.RetryInterval = -1 }, "sdlc.surfacing.retry_interval"},
		"negative post":        {func(c *SDLCConfig) { c.Surfacing.PostTimeout = -1 }, "sdlc.surfacing.post_timeout"},
		"post below the floor": {func(c *SDLCConfig) { c.Surfacing.PostTimeout = SDLCSurfacingMinPostTimeout / 2 }, "sdlc.surfacing.post_timeout"},
		"post above the ceiling": {
			func(c *SDLCConfig) { c.Surfacing.PostTimeout = SDLCSurfacingMaxPostTimeout + 1 },
			"sdlc.surfacing.post_timeout",
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base()
			tc.mutate(&cfg.SDLC)
			err := Validate(cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.field)
		})
	}
}
