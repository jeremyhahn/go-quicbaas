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

	// Disable TLS/encryption for this test (they require cert/key files)
	cfg.Security.TLSEnabled = false
	cfg.Security.EncryptionEnabled = false

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
			name: "invalid QUIC port",
			modify: func(cfg *Config) {
				cfg.Server.QUIC.Enabled = true
				cfg.Server.QUIC.Port = 0
			},
			expectError: true,
			errorField:  "server.quic.port",
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
				cfg.Server.QUIC.Enabled = false
				cfg.Server.QUIC.Port = 0
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

			// Disable TLS/encryption for tests that don't expect security errors
			cfg.Security.TLSEnabled = false
			cfg.Security.EncryptionEnabled = false

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

			// Disable TLS/encryption for tests that don't expect security errors
			cfg.Security.TLSEnabled = false
			cfg.Security.EncryptionEnabled = false

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
			name: "empty listen address",
			modify: func(cfg *Config) {
				cfg.Raft.NodeID = 1
				cfg.Raft.ListenAddress = ""
			},
			expectError: true,
			errorField:  "raft.listen_address",
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

			// Disable TLS/encryption for tests that don't expect security errors
			cfg.Security.TLSEnabled = false
			cfg.Security.EncryptionEnabled = false

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

			// Disable TLS/encryption for tests that don't expect security errors
			cfg.Security.TLSEnabled = false
			cfg.Security.EncryptionEnabled = false

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
	encKeyFile := filepath.Join(tmpDir, "enc.key")

	os.WriteFile(certFile, []byte("cert"), 0o644)
	os.WriteFile(keyFile, []byte("key"), 0o644)
	os.WriteFile(caFile, []byte("ca"), 0o644)
	os.WriteFile(encKeyFile, []byte("enckey"), 0o644)

	tests := []struct {
		name        string
		modify      func(*Config)
		expectError bool
		errorField  string
	}{
		{
			name: "valid security config - disabled",
			modify: func(cfg *Config) {
				cfg.Security.TLSEnabled = false
				cfg.Security.EncryptionEnabled = false
			},
			expectError: false,
		},
		{
			name: "valid TLS config",
			modify: func(cfg *Config) {
				cfg.Security.TLSEnabled = true
				cfg.Security.TLSCertFile = certFile
				cfg.Security.TLSKeyFile = keyFile
				cfg.Security.EncryptionEnabled = false
			},
			expectError: false,
		},
		{
			name: "TLS enabled but missing cert",
			modify: func(cfg *Config) {
				cfg.Security.TLSEnabled = true
				cfg.Security.TLSCertFile = ""
				cfg.Security.TLSKeyFile = keyFile
				cfg.Security.EncryptionEnabled = false
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
				cfg.Security.EncryptionEnabled = false
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
				cfg.Security.EncryptionEnabled = false
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
				cfg.Security.EncryptionEnabled = false
			},
			expectError: true,
			errorField:  "security.tls_ca_file",
		},
		{
			name: "encryption enabled but missing key file",
			modify: func(cfg *Config) {
				cfg.Security.TLSEnabled = false
				cfg.Security.EncryptionEnabled = true
				cfg.Security.EncryptionKeyFile = ""
			},
			expectError: true,
			errorField:  "security.encryption_key_file",
		},
		{
			name: "valid encryption config",
			modify: func(cfg *Config) {
				cfg.Security.TLSEnabled = false
				cfg.Security.EncryptionEnabled = true
				cfg.Security.EncryptionKeyFile = encKeyFile
			},
			expectError: false,
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

			// Disable TLS/encryption for tests that don't expect security errors
			cfg.Security.TLSEnabled = false
			cfg.Security.EncryptionEnabled = false

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

		// Disable TLS/encryption for this test
		cfg.Security.TLSEnabled = false
		cfg.Security.EncryptionEnabled = false

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

		// Disable TLS/encryption for this test
		cfg.Security.TLSEnabled = false
		cfg.Security.EncryptionEnabled = false

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

		// Disable TLS/encryption for this test
		cfg.Security.TLSEnabled = false
		cfg.Security.EncryptionEnabled = false

		err := Validate(cfg)
		assert.NoError(t, err)
	})

	t.Run("memory backend with standalone mode skips raft validation", func(t *testing.T) {
		cfg := Default()
		cfg.Server.Mode = ServerModeStandalone
		cfg.Storage.Backend = StorageBackendMemory
		cfg.Raft.NodeID = 0

		// Disable TLS/encryption for this test
		cfg.Security.TLSEnabled = false
		cfg.Security.EncryptionEnabled = false

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

		// Disable TLS/encryption for this test
		cfg.Security.TLSEnabled = false
		cfg.Security.EncryptionEnabled = false

		err := Validate(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "storage.backend")
		assert.Contains(t, err.Error(), "no longer valid")
		assert.Contains(t, err.Error(), "server.mode")
	})
}

func BenchmarkValidate(b *testing.B) {
	cfg := Default()
	tmpDir := b.TempDir()
	cfg.Storage.DataDir = tmpDir
	cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
	cfg.Storage.Backend = StorageBackendPebble
	cfg.Raft.NodeID = 0

	// Disable TLS/encryption for benchmark
	cfg.Security.TLSEnabled = false
	cfg.Security.EncryptionEnabled = false

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
