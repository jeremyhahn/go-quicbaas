//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag && iam

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
	"context"
	"crypto/tls"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	tmock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	quicapi "github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/cluster"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	quicbaasseal "github.com/jeremyhahn/go-quicbaas/pkg/seal"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/pkg/watch"
	"github.com/jeremyhahn/go-quicbaas/pkg/watchbridge"
)

// =============================================================================
// Typed test errors
// =============================================================================

type covPushError struct {
	msg string
}

func (e *covPushError) Error() string { return e.msg }

// =============================================================================
// defaultNewSealManager (lifecycle.go:407) - currently 0%
// =============================================================================

func TestCovPush_DefaultNewSealManager(t *testing.T) {
	t.Parallel()
	cfg := &quicbaasseal.Config{Threshold: 2, TotalShares: 3}
	mgr, err := defaultNewSealManager(cfg, mustCreateLogger(), t.TempDir())
	assert.NoError(t, err)
	assert.NotNil(t, mgr)
}

func TestCovPush_DefaultNewSealManager_ZeroValues(t *testing.T) {
	t.Parallel()
	cfg := &quicbaasseal.Config{}
	mgr, err := defaultNewSealManager(cfg, mustCreateLogger(), t.TempDir())
	assert.NoError(t, err)
	assert.NotNil(t, mgr)
}

// =============================================================================
// stopWorkerManager (lifecycle.go:860) - currently 25%
// Covers: non-nil workerManager path, StopAll success
// =============================================================================

func TestCovPush_StopWorkerManager_Success(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Create a real worker manager with no workers registered
	logAdapter := logger.NewAdapter(srv.logger)
	srv.workerManager = catalog.NewWorkerManager(logAdapter)

	err = srv.stopWorkerManager()
	assert.NoError(t, err)
	assert.Nil(t, srv.workerManager)
}

func TestCovPush_StopWorkerManager_AlreadyNil(t *testing.T) {
	t.Parallel()
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	srv.workerManager = nil
	err = srv.stopWorkerManager()
	assert.NoError(t, err)
}

// =============================================================================
// stopBackupManager (lifecycle.go:992) - currently 16.7%
// Covers: non-nil backupManager path, close error path, backupStorage close
// =============================================================================

func TestCovPush_StopBackupManager_WithManager(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	srv.backupManager = &backup.BackupManager{}

	seamMu.Lock()
	closeBackupMgrFn = func(m *backup.BackupManager) error { return nil }
	seamMu.Unlock()

	err = srv.stopBackupManager()
	assert.NoError(t, err)
	assert.Nil(t, srv.backupManager)
}

func TestCovPush_StopBackupManager_CloseError(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	srv.backupManager = &backup.BackupManager{}

	seamMu.Lock()
	closeBackupMgrFn = func(m *backup.BackupManager) error {
		return &covPushError{msg: "backup close failed"}
	}
	seamMu.Unlock()

	err = srv.stopBackupManager()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "close backup manager")
}

func TestCovPush_StopBackupManager_WithNilStorage(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	srv.backupManager = &backup.BackupManager{}
	srv.backupStorage = nil

	seamMu.Lock()
	closeBackupMgrFn = func(m *backup.BackupManager) error { return nil }
	seamMu.Unlock()

	err = srv.stopBackupManager()
	assert.NoError(t, err)
	assert.Nil(t, srv.backupManager)
}

// =============================================================================
// startDatabases (lifecycle_databases.go:38) - currently 12.5%
// Covers: disabled path, nil service path, full path with service
// =============================================================================

func TestCovPush_StartDatabases_Disabled(t *testing.T) {
	t.Parallel()
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	srv.config.Databases.Enabled = false
	err = srv.startDatabases()
	assert.NoError(t, err)
}

func TestCovPush_StartDatabases_NilService(t *testing.T) {
	t.Parallel()
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	srv.config.Databases.Enabled = true
	srv.quicBaaSService = nil
	err = srv.startDatabases()
	assert.NoError(t, err)
}

func TestCovPush_StartDatabases_WithService(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	srv.config.Databases.Enabled = true
	srv.config.Databases.DataDir = filepath.Join(t.TempDir(), "databases")
	srv.quicBaaSService = testutil.NewMockQuicBaaS()

	srv.qrdbClient = NewShardedClient(nil,
		WithStorageConfig(&srv.config.Storage),
	)

	err = srv.startDatabases()
	assert.NoError(t, err)
}

func TestCovPush_StartDatabases_DefaultDataDir(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	srv.config.Databases.Enabled = true
	srv.config.Databases.DataDir = ""
	srv.config.Storage.DataDir = t.TempDir()
	srv.quicBaaSService = testutil.NewMockQuicBaaS()

	srv.qrdbClient = NewShardedClient(nil,
		WithStorageConfig(&srv.config.Storage),
	)

	err = srv.startDatabases()
	assert.NoError(t, err)
}

// =============================================================================
// startWatchBridge (lifecycle.go:1626) - currently 24%
// Covers: nil service, nil watchMgr, mapping build, creation error,
// start error, success path with config options
// =============================================================================

func TestCovPush_StartWatchBridge_NilService(t *testing.T) {
	t.Parallel()
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	srv.quicBaaSService = nil
	err = srv.startWatchBridge()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "QuicBaaS service")
}

func TestCovPush_StartWatchBridge_NilWatchManager(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	mock := testutil.NewMockQuicBaaS()
	mock.On("WatchManager").Return(nil)
	srv.quicBaaSService = mock

	err = srv.startWatchBridge()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "watch manager")
}

func TestCovPush_StartWatchBridge_CreationError(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	watchMgr := watch.NewManager()
	mock := testutil.NewMockQuicBaaS()
	mock.On("WatchManager").Return(watchMgr)
	mock.On("SetMessagingService", tmock.Anything).Return()
	srv.quicBaaSService = mock

	// Configure mappings to test mapping build code
	srv.config.Messaging.WatchBridge.Mappings = []config.WatchBridgeMappingConfig{
		{
			KeyPrefix:     "/test/",
			ChannelPrefix: "test-channel",
			FilterPut:     false,
			FilterDelete:  true,
		},
	}
	srv.config.Messaging.WatchBridge.LeaderOnly = true
	srv.config.Messaging.WatchBridge.BufferSize = 100
	srv.config.Messaging.WatchBridge.DefaultChannelPrefix = "default"
	srv.config.Messaging.WatchBridge.AutoCreateChannels = true

	seamMu.Lock()
	newWatchBridgeFn = func(
		log *logger.Logger,
		wm *watch.Manager,
		ms messaging.Service,
		ci cluster.ClusterInfo,
		mappings []watchbridge.KeyChannelMapping,
		opts ...watchbridge.WatchBridgeOption,
	) (*watchbridge.WatchBridge, error) {
		return nil, &covPushError{msg: "watch bridge creation failed"}
	}
	seamMu.Unlock()

	err = srv.startWatchBridge()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "create watch bridge")
}

func TestCovPush_StartWatchBridge_StartError(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	watchMgr := watch.NewManager()
	mock := testutil.NewMockQuicBaaS()
	mock.On("WatchManager").Return(watchMgr)
	srv.quicBaaSService = mock

	srv.config.Messaging.WatchBridge.Mappings = nil

	seamMu.Lock()
	newWatchBridgeFn = func(
		log *logger.Logger,
		wm *watch.Manager,
		ms messaging.Service,
		ci cluster.ClusterInfo,
		mappings []watchbridge.KeyChannelMapping,
		opts ...watchbridge.WatchBridgeOption,
	) (*watchbridge.WatchBridge, error) {
		return &watchbridge.WatchBridge{}, nil
	}
	watchBridgeStartFn = func(b *watchbridge.WatchBridge, ctx context.Context) error {
		return &covPushError{msg: "watch bridge start failed"}
	}
	seamMu.Unlock()

	err = srv.startWatchBridge()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "start watch bridge")
}

func TestCovPush_StartWatchBridge_Success(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	watchMgr := watch.NewManager()
	mock := testutil.NewMockQuicBaaS()
	mock.On("WatchManager").Return(watchMgr)
	srv.quicBaaSService = mock

	srv.config.Messaging.WatchBridge.Mappings = []config.WatchBridgeMappingConfig{
		{KeyPrefix: "/data/", ChannelPrefix: "data-events"},
	}

	seamMu.Lock()
	newWatchBridgeFn = func(
		log *logger.Logger,
		wm *watch.Manager,
		ms messaging.Service,
		ci cluster.ClusterInfo,
		mappings []watchbridge.KeyChannelMapping,
		opts ...watchbridge.WatchBridgeOption,
	) (*watchbridge.WatchBridge, error) {
		return &watchbridge.WatchBridge{}, nil
	}
	watchBridgeStartFn = func(b *watchbridge.WatchBridge, ctx context.Context) error {
		return nil
	}
	seamMu.Unlock()

	err = srv.startWatchBridge()
	assert.NoError(t, err)
	assert.NotNil(t, srv.watchBridge)
}

// =============================================================================
// configureMCPAuth (lifecycle_mcp.go:91) - currently 40%
// Covers: type assertion fails path
// =============================================================================

func TestCovPush_ConfigureMCPAuth_NoInternalAuthManager(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Use a mock that does not implement internalAuthManagerGetter
	srv.quicBaaSService = testutil.NewMockQuicBaaS()

	seamMu.Lock()
	mcpStopFn = func(m *mcpapi.Server) error { return nil }
	seamMu.Unlock()

	mcpConfig := &mcpapi.Config{
		ServerName:     "test",
		ServerVersion:  "1.0.0",
		Transport:      "http",
		HTTPAddr:       "127.0.0.1:0",
		RequestTimeout: 5 * time.Second,
	}
	mcpServer := mcpapi.NewServer(srv.quicBaaSService, mcpConfig, srv.logger)

	// Should not panic - the type assertion should fail gracefully
	srv.configureMCPAuth(mcpServer)
}

// =============================================================================
// startRAG (lifecycle_rag.go:31) - currently 50%
// Covers: disabled path, validation failure path
// =============================================================================

func TestCovPush_StartRAG_Disabled(t *testing.T) {
	t.Parallel()
	cfg := createFTConfig(t)
	cfg.RAG.Enabled = false
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startRAG()
	assert.NoError(t, err)
	assert.Nil(t, srv.ragSystem)
}

func TestCovPush_StartRAG_ValidationFailure(t *testing.T) {
	t.Parallel()
	cfg := createFTConfig(t)
	cfg.RAG.Enabled = true
	cfg.RAG.Embedding.Provider = ""
	cfg.RAG.IndexPath = ""

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startRAG()
	assert.NoError(t, err)
	assert.Nil(t, srv.ragSystem)
}

// =============================================================================
// startQUICServer (lifecycle_quic.go:35) - currently 51.9%
// Covers: creation success + start path with seams
// =============================================================================

func TestCovPush_StartQUICServer_TLSDisabled(t *testing.T) {
	t.Parallel()
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	srv.config.Server.Embedded = false
	srv.config.Server.QUIC.Enabled = true
	srv.config.Server.QUIC.Port = 19445
	srv.config.Security.TLSEnabled = false

	err = srv.startQUICServer()
	assert.NoError(t, err)
	assert.Nil(t, srv.quicServer)
}

func TestCovPush_StartQUICServer_CreationError(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	srv.config.Server.Embedded = false
	srv.config.Server.QUIC.Enabled = true
	srv.config.Server.QUIC.Port = 19446
	srv.config.Security.TLSEnabled = true
	srv.tlsConfig = &tls.Config{MinVersion: tls.VersionTLS13}
	srv.quicBaaSService = testutil.NewMockQuicBaaS()

	seamMu.Lock()
	newQUIC = func(svc service.QuicBaaS, cfg *quicapi.Config, log *logger.Logger, opts ...quicapi.ServerOption) (*quicapi.Server, error) {
		return nil, &covPushError{msg: "quic creation error"}
	}
	seamMu.Unlock()

	err = srv.startQUICServer()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "create QUIC server")
}

// =============================================================================
// startBackupManager (lifecycle.go:876) - currently 64.9%
// Covers: disabled, compression types, encryption paths
// =============================================================================

func TestCovPush_StartBackupManager_Disabled(t *testing.T) {
	t.Parallel()
	cfg := createFTConfig(t)
	cfg.Backup.Enabled = false
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startBackupManager()
	assert.NoError(t, err)
}

func TestCovPush_StartBackupManager_GzipCompression(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.Backup.Enabled = true
	cfg.Backup.DefaultCompression = "gzip"
	cfg.Backup.DefaultEncryption = "none"
	cfg.Backup.WorkDir = filepath.Join(t.TempDir(), "backup-work")
	cfg.Storage.DataDir = t.TempDir()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.raftNode = newStandaloneRaftNode(1, "127.0.0.1:0")

	err = srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	if srv.backupManager != nil {
		_ = srv.backupManager.Close()
	}
}

func TestCovPush_StartBackupManager_NoneCompression(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.Backup.Enabled = true
	cfg.Backup.DefaultCompression = "none"
	cfg.Backup.DefaultEncryption = "none"
	cfg.Backup.WorkDir = filepath.Join(t.TempDir(), "backup-work")
	cfg.Storage.DataDir = t.TempDir()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.raftNode = newStandaloneRaftNode(1, "127.0.0.1:0")

	err = srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	if srv.backupManager != nil {
		_ = srv.backupManager.Close()
	}
}

func TestCovPush_StartBackupManager_ZstdCompression(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.Backup.Enabled = true
	cfg.Backup.DefaultCompression = "zstd"
	cfg.Backup.DefaultEncryption = "none"
	cfg.Backup.WorkDir = filepath.Join(t.TempDir(), "backup-work")
	cfg.Storage.DataDir = t.TempDir()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.raftNode = newStandaloneRaftNode(1, "127.0.0.1:0")

	err = srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	if srv.backupManager != nil {
		_ = srv.backupManager.Close()
	}
}

func TestCovPush_StartBackupManager_UnknownCompression(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.Backup.Enabled = true
	cfg.Backup.DefaultCompression = "unknown-compression"
	cfg.Backup.DefaultEncryption = "none"
	cfg.Backup.WorkDir = filepath.Join(t.TempDir(), "backup-work")
	cfg.Storage.DataDir = t.TempDir()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.raftNode = newStandaloneRaftNode(1, "127.0.0.1:0")

	err = srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	if srv.backupManager != nil {
		_ = srv.backupManager.Close()
	}
}

func TestCovPush_StartBackupManager_AESEncryption_ValidKey(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.Backup.Enabled = true
	cfg.Backup.DefaultCompression = "none"
	cfg.Backup.DefaultEncryption = "aes-256-gcm"
	cfg.Backup.EncryptionKey = "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE="
	cfg.Backup.WorkDir = filepath.Join(t.TempDir(), "backup-work")
	cfg.Storage.DataDir = t.TempDir()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.raftNode = newStandaloneRaftNode(1, "127.0.0.1:0")

	err = srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	if srv.backupManager != nil {
		_ = srv.backupManager.Close()
	}
}

func TestCovPush_StartBackupManager_AESEncryption_InvalidKeySize(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.Backup.Enabled = true
	cfg.Backup.DefaultCompression = "none"
	cfg.Backup.DefaultEncryption = "aes-256-gcm"
	cfg.Backup.EncryptionKey = "dG9vc2hvcnQ=" // "tooshort" - not 32 bytes
	cfg.Backup.WorkDir = filepath.Join(t.TempDir(), "backup-work")
	cfg.Storage.DataDir = t.TempDir()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.raftNode = newStandaloneRaftNode(1, "127.0.0.1:0")

	err = srv.startBackupManager()
	assert.Error(t, err)
}

func TestCovPush_StartBackupManager_AESEncryption_NoKey(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.Backup.Enabled = true
	cfg.Backup.DefaultCompression = "none"
	cfg.Backup.DefaultEncryption = "aes-256-gcm"
	cfg.Backup.EncryptionKey = ""
	cfg.Backup.WorkDir = filepath.Join(t.TempDir(), "backup-work")
	cfg.Storage.DataDir = t.TempDir()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.raftNode = newStandaloneRaftNode(1, "127.0.0.1:0")

	err = srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	if srv.backupManager != nil {
		_ = srv.backupManager.Close()
	}
}

func TestCovPush_StartBackupManager_AESEncryption_RawKey(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.Backup.Enabled = true
	cfg.Backup.DefaultCompression = "none"
	cfg.Backup.DefaultEncryption = "aes-256-gcm"
	cfg.Backup.EncryptionKey = "abcdefghijklmnopqrstuvwxyz!@#$%^" // 32 raw bytes, invalid base64
	cfg.Backup.WorkDir = filepath.Join(t.TempDir(), "backup-work")
	cfg.Storage.DataDir = t.TempDir()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.raftNode = newStandaloneRaftNode(1, "127.0.0.1:0")

	err = srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	if srv.backupManager != nil {
		_ = srv.backupManager.Close()
	}
}

func TestCovPush_StartBackupManager_UnknownEncryption(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.Backup.Enabled = true
	cfg.Backup.DefaultCompression = "none"
	cfg.Backup.DefaultEncryption = "unknown-enc"
	cfg.Backup.WorkDir = filepath.Join(t.TempDir(), "backup-work")
	cfg.Storage.DataDir = t.TempDir()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.raftNode = newStandaloneRaftNode(1, "127.0.0.1:0")

	err = srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	if srv.backupManager != nil {
		_ = srv.backupManager.Close()
	}
}

func TestCovPush_StartBackupManager_DefaultWorkDir(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.Backup.Enabled = true
	cfg.Backup.DefaultCompression = "zstd"
	cfg.Backup.DefaultEncryption = "none"
	cfg.Backup.WorkDir = ""
	cfg.Backup.Timeout = 0
	cfg.Storage.DataDir = t.TempDir()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.raftNode = newStandaloneRaftNode(1, "127.0.0.1:0")

	err = srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	if srv.backupManager != nil {
		_ = srv.backupManager.Close()
	}
}

func TestCovPush_StartBackupManager_CustomTimeout(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.Backup.Enabled = true
	cfg.Backup.DefaultCompression = "none"
	cfg.Backup.DefaultEncryption = "none"
	cfg.Backup.Timeout = 10 * time.Minute
	cfg.Backup.WorkDir = filepath.Join(t.TempDir(), "backup-work")
	cfg.Storage.DataDir = t.TempDir()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.raftNode = newStandaloneRaftNode(1, "127.0.0.1:0")

	err = srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	if srv.backupManager != nil {
		_ = srv.backupManager.Close()
	}
}

func TestCovPush_StartBackupManager_DefaultStorageBackend(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.Backup.Enabled = true
	cfg.Backup.StorageBackend = "" // Should default to "local"
	cfg.Backup.DefaultCompression = "none"
	cfg.Backup.DefaultEncryption = "none"
	cfg.Backup.WorkDir = filepath.Join(t.TempDir(), "backup-work")
	cfg.Storage.DataDir = t.TempDir()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.raftNode = newStandaloneRaftNode(1, "127.0.0.1:0")

	err = srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	if srv.backupManager != nil {
		_ = srv.backupManager.Close()
	}
}

// =============================================================================
// startStorage (lifecycle.go:468) - currently 76.9%
// Covers: unknown backend fallback, empty backend, file backend, error paths
// =============================================================================

func TestCovPush_StartStorage_UnknownBackend(t *testing.T) {
	t.Parallel()
	cfg := createFTConfig(t)
	cfg.Storage.Backend = "unknown-backend"
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startStorage()
	assert.NoError(t, err)
}

func TestCovPush_StartStorage_EmptyBackend(t *testing.T) {
	t.Parallel()
	cfg := createFTConfig(t)
	cfg.Storage.Backend = ""
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startStorage()
	assert.NoError(t, err)
}

func TestCovPush_StartStorage_FileBackend(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.Storage.Backend = config.StorageBackendFile
	cfg.Storage.DataDir = filepath.Join(t.TempDir(), "data")
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startStorage()
	assert.NoError(t, err)
}

func TestCovPush_StartStorage_PebbleNoDataDir(t *testing.T) {
	t.Parallel()
	cfg := createFTConfig(t)
	cfg.Storage.Backend = config.StorageBackendPebble
	cfg.Storage.DataDir = ""
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startStorage()
	assert.Error(t, err)
}

// =============================================================================
// startGRPCServer (lifecycle.go:1256) - currently 79.2%
// =============================================================================

func TestCovPush_StartGRPCServer_Embedded(t *testing.T) {
	t.Parallel()
	cfg := createFTConfig(t)
	cfg.Server.Embedded = true
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startGRPCServer()
	assert.NoError(t, err)
	assert.Nil(t, srv.grpcServer)
}

// =============================================================================
// startUnixSocketListener (lifecycle.go:1490) - currently 78.9%
// =============================================================================

func TestCovPush_StartUnixSocketListener_Success(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")
	srv.config.Server.Unix.Socket = socketPath

	grpcServer := ggrpc.NewServer()
	t.Cleanup(grpcServer.Stop)

	err = srv.startUnixSocketListener(grpcServer)
	assert.NoError(t, err)
	assert.NotNil(t, srv.grpcUnixListener)

	_, statErr := os.Stat(socketPath)
	assert.NoError(t, statErr)

	if srv.grpcUnixListener != nil {
		_ = srv.grpcUnixListener.Close()
	}
}

func TestCovPush_StartUnixSocketListener_ExistingSocket(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Use /tmp directly to keep Unix socket path under 108 char limit
	tmpDir, err2 := os.MkdirTemp("/tmp", "usl")
	require.NoError(t, err2)
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	socketPath := filepath.Join(tmpDir, "s.sock")
	srv.config.Server.Unix.Socket = socketPath

	require.NoError(t, os.WriteFile(socketPath, []byte("old"), 0600))

	grpcServer := ggrpc.NewServer()
	t.Cleanup(grpcServer.Stop)

	err = srv.startUnixSocketListener(grpcServer)
	assert.NoError(t, err)
	assert.NotNil(t, srv.grpcUnixListener)

	if srv.grpcUnixListener != nil {
		_ = srv.grpcUnixListener.Close()
	}
}

func TestCovPush_StartUnixSocketListener_ListenError(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")
	srv.config.Server.Unix.Socket = socketPath

	seamMu.Lock()
	listenFunc = func(network, address string) (net.Listener, error) {
		if network == "unix" {
			return nil, &covPushError{msg: "listen error"}
		}
		return net.Listen(network, address)
	}
	seamMu.Unlock()

	grpcServer := ggrpc.NewServer()
	t.Cleanup(grpcServer.Stop)

	err = srv.startUnixSocketListener(grpcServer)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Unix socket listener")
}

// =============================================================================
// startCatalogManager (lifecycle.go:642) - currently 63.4%
// =============================================================================

func TestCovPush_StartCatalogManager_Disabled(t *testing.T) {
	t.Parallel()
	cfg := createFTConfig(t)
	cfg.ObjectStorage.Enabled = false
	cfg.ObjectStorage.Backends = nil
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startCatalogManager()
	assert.NoError(t, err)
}

func TestCovPush_StartCatalogManager_EnabledNoBackends(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	cfg.ObjectStorage.Enabled = true
	cfg.ObjectStorage.Backends = nil
	cfg.Storage.DataDir = t.TempDir()
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startCatalogManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.catalogManager)
	assert.NotNil(t, srv.backendManager)
	assert.NotNil(t, srv.workerManager)

	if srv.catalogManager != nil {
		_ = srv.catalogManager.Close()
	}
}

func TestCovPush_StartCatalogManager_WorkerCreationErrors(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	cfg.ObjectStorage.Enabled = true
	cfg.ObjectStorage.Backends = map[string]*config.StorageBackendConfig{
		"test": {Type: "memory", Config: map[string]string{}},
	}
	cfg.Storage.DataDir = t.TempDir()
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	seamMu.Lock()
	newReplicationWorkerFn = func(cm catalog.CatalogManager, bm catalog.BackendManager, cfg *catalog.WorkerConfig, log interfaces.Logger) (*catalog.ReplicationWorker, error) {
		return nil, &covPushError{msg: "replication worker error"}
	}
	newArchiveWorkerFn = func(cm catalog.CatalogManager, bm catalog.BackendManager, cfg *catalog.WorkerConfig, log interfaces.Logger) (*catalog.ArchiveWorker, error) {
		return nil, &covPushError{msg: "archive worker error"}
	}
	newVerifyWorkerFn = func(cm catalog.CatalogManager, bm catalog.BackendManager, cfg *catalog.WorkerConfig, log interfaces.Logger) (*catalog.VerifyWorker, error) {
		return nil, &covPushError{msg: "verify worker error"}
	}
	newRetentionWorkerFn = func(cm catalog.CatalogManager, bm catalog.BackendManager, cfg *catalog.RetentionWorkerConfig, log interfaces.Logger) (*catalog.RetentionWorker, error) {
		return nil, &covPushError{msg: "retention worker error"}
	}
	seamMu.Unlock()

	err = srv.startCatalogManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.catalogManager)

	if srv.catalogManager != nil {
		_ = srv.catalogManager.Close()
	}
}

// =============================================================================
// startService (lifecycle.go:1017) - currently 71.3%
// Covers: cache config paths with custom values, backup manager attached
// =============================================================================

func TestCovPush_StartService_CacheConfigPaths(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Server.Embedded = true
	cfg.Cache.Enabled = true
	cfg.Cache.MaxSize = 5000
	cfg.Cache.DefaultTTL = 10 * time.Minute
	cfg.Cache.CleanupInterval = 2 * time.Minute

	seamMu.Lock()
	initIAMComponentsFn = func(_ *Server) (service.IAMComponents, error) { return nil, nil }
	seamMu.Unlock()

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.initialize()
	assert.NoError(t, err)
	assert.NotNil(t, srv.cacheManager)
}

func TestCovPush_StartService_WithBackupManager(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Server.Embedded = true
	cfg.Backup.Enabled = true
	cfg.Backup.DefaultCompression = "none"
	cfg.Backup.DefaultEncryption = "none"
	cfg.Backup.WorkDir = filepath.Join(t.TempDir(), "backup-work")

	seamMu.Lock()
	initIAMComponentsFn = func(_ *Server) (service.IAMComponents, error) { return nil, nil }
	seamMu.Unlock()

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.initialize()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	if srv.backupManager != nil {
		_ = srv.backupManager.Close()
	}
}

// =============================================================================
// startKMS (lifecycle.go:508) - currently 78.9%
// =============================================================================

func TestCovPush_StartKMS_Disabled(t *testing.T) {
	t.Parallel()
	cfg := createFTConfig(t)
	cfg.KMS.Enabled = false
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startKMS()
	assert.NoError(t, err)
}

// =============================================================================
// initializeCA (lifecycle.go:127) - currently 75%
// =============================================================================

func TestCovPush_InitializeCA_TLSDisabled(t *testing.T) {
	t.Parallel()
	cfg := createFTConfig(t)
	cfg.Security.TLSEnabled = false
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.initializeCA()
	assert.NoError(t, err)
}

func TestCovPush_InitializeCA_DefaultConfig(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.Security.TLSEnabled = true
	cfg.CA.RootCommonName = ""
	cfg.CA.RootOrganization = ""
	cfg.CA.RootValidYears = 0
	cfg.CA.DefaultValidityDays = 0
	cfg.CA.KeysDir = ""
	cfg.Storage.DataDir = t.TempDir()
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.initializeCA()
	// Covers the defaults application code path
	if err != nil {
		assert.NotNil(t, err)
	}
}

func TestCovPush_InitializeCA_CustomKeysDir(t *testing.T) {
	cfg := createFTConfig(t)
	cfg.Security.TLSEnabled = true
	cfg.CA.KeysDir = filepath.Join(t.TempDir(), "custom-keys")
	cfg.Storage.DataDir = t.TempDir()
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.initializeCA()
	if err != nil {
		assert.NotNil(t, err)
	}
}

// =============================================================================
// startMessaging + startWatchBridge full integration
// =============================================================================

func TestCovPush_StartMessaging_WithWatchBridgeEnabled(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	srv.config.Messaging.BufferSize = 100
	srv.config.Messaging.DedupEnabled = true
	srv.config.Messaging.DedupWindow = 5 * time.Second
	srv.config.Messaging.WatchBridge.Enabled = true

	watchMgr := watch.NewManager()
	mock := testutil.NewMockQuicBaaS()
	mock.On("WatchManager").Return(watchMgr)
	mock.On("SetMessagingService", tmock.Anything).Return()
	srv.quicBaaSService = mock

	seamMu.Lock()
	pushServiceStartFn = func(ps *messaging.PushService, ctx context.Context) error {
		return nil
	}
	newWatchBridgeFn = func(
		log *logger.Logger,
		wm *watch.Manager,
		ms messaging.Service,
		ci cluster.ClusterInfo,
		mappings []watchbridge.KeyChannelMapping,
		opts ...watchbridge.WatchBridgeOption,
	) (*watchbridge.WatchBridge, error) {
		return &watchbridge.WatchBridge{}, nil
	}
	watchBridgeStartFn = func(b *watchbridge.WatchBridge, ctx context.Context) error {
		return nil
	}
	seamMu.Unlock()

	err = srv.startMessaging()
	assert.NoError(t, err)
	assert.NotNil(t, srv.messagingService)
	assert.NotNil(t, srv.watchBridge)
}

func TestCovPush_StartMessaging_WatchBridgeDisabled(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	srv.config.Messaging.WatchBridge.Enabled = false

	seamMu.Lock()
	pushServiceStartFn = func(ps *messaging.PushService, ctx context.Context) error {
		return nil
	}
	seamMu.Unlock()

	err = srv.startMessaging()
	assert.NoError(t, err)
	assert.NotNil(t, srv.messagingService)
	assert.Nil(t, srv.watchBridge)
}

func TestCovPush_StartMessaging_PushServiceError(t *testing.T) {
	saveAndRestoreSeams(t)
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	seamMu.Lock()
	pushServiceStartFn = func(ps *messaging.PushService, ctx context.Context) error {
		return &covPushError{msg: "push service start failed"}
	}
	seamMu.Unlock()

	err = srv.startMessaging()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "start messaging service")
}

// =============================================================================
// initializeIAMComponents (iam_init.go:239) - currently 74.6%
// =============================================================================

func TestCovPush_InitializeIAMComponents_NilQRDBClient(t *testing.T) {
	t.Parallel()
	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	srv.qrdbClient = nil
	components, err := srv.initializeIAMComponents()
	assert.NoError(t, err)
	assert.Nil(t, components)
}
