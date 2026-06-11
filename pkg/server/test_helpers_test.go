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

package server

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// getFreePort returns a free port by binding to port 0 and releasing it.
func getFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}

// createTestConfig creates a test configuration with security features disabled.
func createTestConfig(t *testing.T) *config.Config {
	// Create temporary directory for test data
	tempDir, err := os.MkdirTemp("", "quicbaas-server-test-*")
	require.NoError(t, err)

	// Get free ports for testing to avoid conflicts
	httpPort := getFreePort(t)
	grpcPort := getFreePort(t)
	raftPort := getFreePort(t)

	cfg := config.Default()
	cfg.Storage.Backend = config.StorageBackendPebble
	cfg.Storage.DataDir = filepath.Join(tempDir, "data")
	cfg.Storage.WALDir = filepath.Join(tempDir, "wal")
	// Pin engine storage paths to the temp dir. config.Default() seeds these with
	// RELATIVE defaults (e.g. vector "./data/vector"), so without this the vector/
	// timeseries Pebble engines write databases into the source tree (pkg/server/
	// data, pkg/server/vector) during tests. Tests must never touch the host tree.
	cfg.Vector.StoragePath = filepath.Join(tempDir, "vector")
	cfg.TimeSeries.StoragePath = filepath.Join(tempDir, "timeseries")
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.REST.Port = httpPort
	cfg.Server.GRPC.Port = grpcPort
	cfg.Raft.NodeID = 1
	cfg.Raft.RaftAddress = "127.0.0.1:" + strconv.Itoa(raftPort)
	cfg.Raft.ListenAddress = "127.0.0.1:" + strconv.Itoa(raftPort)
	cfg.Logging.Level = "error" // Reduce noise in tests

	// Disable security features for unit tests
	cfg.Security.TLSEnabled = false
	cfg.Raft.MutualTLS = false
	// Unit tests exercise handlers via the in-process/anonymous path. The
	// default-deny scope -> RBAC gate (now the secure default in config.Default)
	// has its own dedicated unit tests; keep it OFF here so handler tests are not
	// blanket-401'd. Integration suites prove enforcement with authed clients.
	cfg.Security.RequireAuthz = false

	// Disable features that require additional setup for unit tests
	cfg.KMS.Enabled = false
	cfg.Secrets.Enabled = false
	cfg.Vector.Enabled = false
	cfg.Ollama.Enabled = false
	cfg.Backup.Enabled = false
	cfg.Discovery.Enabled = false
	cfg.Raft.EnableBootstrap = false

	// Disable MCP and QUIC for unit tests
	cfg.Server.MCP.Enabled = false
	cfg.Server.QUIC.Enabled = false

	return cfg
}

// cleanupTestData removes test data directory.
func cleanupTestData(t *testing.T, cfg *config.Config) {
	// Get the parent temp directory
	tempDir := filepath.Dir(cfg.Storage.DataDir)
	if err := os.RemoveAll(tempDir); err != nil {
		t.Logf("Warning: failed to cleanup test data: %v", err)
	}
}

// withTempServer creates a server with test configuration and temp directories.
// It automatically cleans up on test completion.
func withTempServer(t *testing.T) *Server {
	t.Helper()
	cfg := createTestConfig(t)
	t.Cleanup(func() {
		cleanupTestData(t, cfg)
	})
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	return srv
}

// dummyListener implements net.Listener for testing.
type dummyListener struct{}

func (d *dummyListener) Accept() (net.Conn, error) {
	return nil, &testHelperListenerError{msg: "closed"}
}

func (d *dummyListener) Close() error {
	return nil
}

func (d *dummyListener) Addr() net.Addr {
	return &net.TCPAddr{}
}

// testHelperListenerError is a typed error for test helpers.
type testHelperListenerError struct {
	msg string
}

func (e *testHelperListenerError) Error() string { return e.msg }

// createMinTestCfg creates a minimal test configuration with the specified temp directory.
func createMinTestCfg(tmpDir string) *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Host:              "127.0.0.1",
			ReadTimeout:       5 * time.Second,
			WriteTimeout:      5 * time.Second,
			IdleTimeout:       60 * time.Second,
			Embedded:          true,
			ExposeShardHeader: false,
			Shards:            []uint64{1},
			GRPC: config.ServerGRPCConfig{
				Enabled: false,
				Port:    0,
			},
			REST: config.ServerRESTConfig{
				Enabled: false,
				Port:    0,
			},
			QUIC: config.ServerQUICConfig{
				Enabled: false,
				Port:    0,
			},
			Unix: config.ServerUnixConfig{
				Socket: "",
			},
		},
		Raft: config.RaftConfig{
			NodeID:              1,
			RaftAddress:         "127.0.0.1:63001",
			ElectionRTT:         10,
			HeartbeatRTT:        1,
			RTTMillisecond:      100,
			SnapshotEntries:     10000,
			CompactionOverhead:  1000,
			MaxInMemLogSize:     1024 * 1024,
			MaxSendQueueSize:    1024,
			MaxReceiveQueueSize: 1024,
			MutualTLS:           false,
			EnableBootstrap:     false,
			JoinAddresses:       []string{},
		},
		Storage: config.StorageConfig{
			DataDir:               filepath.Join(tmpDir, "data"),
			WALDir:                filepath.Join(tmpDir, "wal"),
			CacheSize:             64 * 1024 * 1024,
			MaxOpenFiles:          1000,
			DisableWAL:            false,
			SyncWrites:            true,
			BytesPerSync:          1024 * 1024,
			MemTableSize:          64 * 1024 * 1024,
			MemTableStopWrites:    8,
			L0CompactionThreshold: 4,
			L0StopWritesThreshold: 12,
			MaxConcurrentReads:    4,
			MaxConcurrentWrites:   4,
		},
		Security: config.SecurityConfig{
			TLSEnabled:    false,
			TLSCertFile:   "",
			TLSKeyFile:    "",
			TLSCAFile:     "",
			TLSClientAuth: false,
			FIPSMode:      false,
		},
		Logging: config.LoggingConfig{
			Level:  "info",
			Format: "json",
			Output: "stdout",
			File:   "",
		},
		Discovery: config.DiscoveryConfig{
			Enabled: false,
		},
	}
}

// mustCreateLogger creates a logger for tests, panicking on failure.
func mustCreateLogger() *logger.Logger {
	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "test",
	}
	log, err := logger.New(logConfig)
	if err != nil {
		panic(fmt.Sprintf("failed to create logger: %v", err))
	}
	return log
}

// createFTConfig creates a test configuration for full-tags tests.
// It delegates to createTestConfig which provides a complete config with
// security features and optional subsystems disabled for unit testing.
func createFTConfig(t *testing.T) *config.Config {
	t.Helper()
	return createTestConfig(t)
}

// buildTestLogger builds a logger suitable for tests (discards output).
// It lives in this untagged file because it is shared by both default-tag
// and rest+iam test files.
func buildTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("failed to create test logger: %v", err)
	}
	return log
}
