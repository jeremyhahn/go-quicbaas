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
	"strings"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/netport"
)

// testTrustDomain is the SPIFFE trust domain these tests configure. First-boot
// fails closed without one (see ErrNoSPIFFETrustDomain), because the admin
// certificate would carry no tenant SAN and could not authenticate.
const testTrustDomain = "quicbaas.example.test"

// stopCatalogStackForTest tears the catalog stack down in the order Shutdown
// uses: background workers first, then the backend manager, then the catalog
// store they read. Closing the store while the retention worker is mid-scan
// leaves that scan iterating a closed PebbleDB, which panics on a goroutine and
// takes the whole test binary down with it (go-quicbaas#986).
func stopCatalogStackForTest(t *testing.T, srv *Server) {
	t.Helper()
	require.NoError(t, srv.stopWorkerManager())
	require.NoError(t, srv.stopBackendManager())
	require.NoError(t, srv.stopCatalogManager())
}

// getFreePort returns a single port reserved for this test binary.
func getFreePort(t *testing.T) int {
	t.Helper()
	return netport.MustReserveOne(t)
}

// getFreePorts returns n DISTINCT ports reserved for this test binary.
//
// Reserved, not merely observed free. netport takes a host-wide lock on each
// port and draws only from outside the kernel's ephemeral range, so nothing
// between this call and the server's eventual bind can be handed the same
// number.
//
// The helper this replaced listened on 127.0.0.1:0, recorded the kernel's
// choice and CLOSED the listener before returning. Holding all n probe
// listeners until the last was drawn fixed the collision within a single call
// — three ports coming back equal — but could not touch the wider one: from
// the close until initStandaloneQRDBNode binds, the port is genuinely free and
// anything on the host may take it. This package boots roughly seventy-five
// standalone qrdb nodes in parallel and several worktrees gate at once, so
// that interval was crossed often enough to fail tests whose diff had nothing
// to do with it (go-quicbaas#706, and go#497 before it).
func getFreePorts(t *testing.T, n int) []int {
	t.Helper()
	return netport.MustReserve(t, n)
}

// createTestConfig creates a test configuration with security features disabled.
func createTestConfig(t *testing.T) *config.Config {
	// Create temporary directory for test data. t.TempDir() is used rather than
	// os.MkdirTemp so the testing package reclaims it even if a test mutates
	// cfg.Storage.DataDir and cleanupTestData's safety guard declines to remove
	// the mutated path.
	tempDir := t.TempDir()

	// Get free ports for testing to avoid conflicts. Drawn in ONE call so the
	// three are distinct — see getFreePorts.
	ports := getFreePorts(t, 3)
	httpPort, grpcPort, raftPort := ports[0], ports[1], ports[2]

	cfg := config.Default()
	// Unit tests run in memory (#1770): the embedded qrdb node opens its
	// database on the memory engine. TestWithTempServer_StartOpensNoPebbleDatabase
	// guards this and lists the stores that still have no in-memory option.
	cfg.Storage.Backend = config.StorageBackendMemory
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

	// Disable MCP for unit tests
	cfg.Server.MCP.Enabled = false

	// A test server that brings up a CA must be configured like a deployment
	// that brings up a CA. First-boot issues the platform admin's certificate
	// through it, and that certificate needs a SPIFFE trust domain to carry a
	// tenant SAN login can resolve — so first-boot FAILS CLOSED without one
	// (ErrNoSPIFFETrustDomain), and since #57 that failure refuses the boot
	// rather than warning. Tests that enable TLS previously got away with the
	// misconfiguration because the failure was swallowed; setting it here keeps
	// the fixture honest instead of asking every such test to remember.
	cfg.IAM.SPIFFETrustDomain = testTrustDomain

	return cfg
}

// cleanupTestData removes test data directory.
func cleanupTestData(t *testing.T, cfg *config.Config) {
	// The parent of the data dir is the temp dir createTestConfig made.
	tempDir := filepath.Dir(cfg.Storage.DataDir)

	// HARD SAFETY GUARD. Tests are free to point Storage.DataDir at a path that
	// cannot be created in order to exercise a storage-init failure — e.g.
	// "/proc/cannot-create-quicbaas-storage". filepath.Dir of that is "/proc",
	// and blindly removing it both violates the project rule that unit tests
	// must never modify the host and hangs the suite for minutes walking
	// procfs's self-referential symlinks. Only ever delete inside the OS temp
	// dir, which is the only place createTestConfig ever allocates.
	root := os.TempDir()
	rel, err := filepath.Rel(root, tempDir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Logf("Skipping cleanup of %q: outside the temp dir (%q)", tempDir, root)
		return
	}

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

// newTestFleetCertRecords returns a fleet node certificate record store over
// an in-memory KV store, for tests whose server has no qrdb client.
func newTestFleetCertRecords(t *testing.T) service.FleetNodeCertRecords {
	t.Helper()
	records, err := service.NewKVFleetNodeCertRecords(qrdbsdk.NewMemoryKVStore())
	require.NoError(t, err)
	return records
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
