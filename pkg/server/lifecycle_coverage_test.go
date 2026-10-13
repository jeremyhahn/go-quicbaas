//go:build !integration

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//   See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//   Contact licensing@automatethethings.com for commercial licensing options.

package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	quicbaasseal "github.com/jeremyhahn/go-quicbaas/pkg/seal"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Typed test errors (never use fmt.Errorf per CLAUDE.md).
// =============================================================================

// lcTestError is a typed error for lifecycle coverage tests.
type lcTestError struct {
	msg string
}

func (e *lcTestError) Error() string { return e.msg }

// =============================================================================
// Helper: saveAndRestoreLifecycleSeams snapshots lifecycle seams and
// restores them when the test completes.
// =============================================================================

func saveAndRestoreLifecycleSeams(t *testing.T) {
	t.Helper()

	seamMu.Lock()
	origInitializeCAFn := initializeCAFn
	origNewBarrierManagerFn := newBarrierManagerFn
	origInitSealManagerFn := initSealManagerFn
	origNewCacheManagerFn := newCacheManagerFn
	origInitIAMComponentsFn := initIAMComponentsFn
	origPushServiceStartFn := pushServiceStartFn
	origWatchBridgeStartFn := watchBridgeStartFn
	origNewWatchBridgeFn := newWatchBridgeFn
	origCloseBackupMgrFn := closeBackupMgrFn
	seamMu.Unlock()

	t.Cleanup(func() {
		seamMu.Lock()
		initializeCAFn = origInitializeCAFn
		newBarrierManagerFn = origNewBarrierManagerFn
		initSealManagerFn = origInitSealManagerFn
		newCacheManagerFn = origNewCacheManagerFn
		initIAMComponentsFn = origInitIAMComponentsFn
		pushServiceStartFn = origPushServiceStartFn
		watchBridgeStartFn = origWatchBridgeStartFn
		newWatchBridgeFn = origNewWatchBridgeFn
		closeBackupMgrFn = origCloseBackupMgrFn
		seamMu.Unlock()
	})
}

// stubIAMSeam stubs the IAM components seam to return nil (no IAM).
func stubIAMSeam() {
	seamMu.Lock()
	initIAMComponentsFn = func(_ *Server) (service.IAMComponents, error) {
		return nil, nil
	}
	seamMu.Unlock()
}

// =============================================================================
// RAG noop stub tests (compiled under !rag tag)
// =============================================================================

func TestLC_StartRAG_NoopReturnsNil(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	err := srv.startRAG()
	assert.NoError(t, err)
}

func TestLC_StopRAG_NoopReturnsNil(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	err := srv.stopRAG()
	assert.NoError(t, err)
}

func TestLC_StartOllama_NoopReturnsNil(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	err := srv.startOllama()
	assert.NoError(t, err)
}

func TestLC_StopOllama_NoopReturnsNil(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	err := srv.stopOllama()
	assert.NoError(t, err)
}

func TestLC_RegisterOllamaService_NoopDoesNotPanic(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	// Should not panic with nil grpc server
	srv.registerOllamaService(nil)
}

// =============================================================================
// server.go: SetBarrierManager, GetRaftNode, TriggerShutdown
// =============================================================================

func TestLC_SetSealManager(t *testing.T) {
	t.Run("sets and retrieves seal manager", func(t *testing.T) {
		srv := withTempServer(t)
		assert.Nil(t, srv.GetBarrierManager())

		// Reuse the mockSealManager from new_methods_test.go (same package, no build tag)
		mock := &mockSealManager{}
		srv.SetBarrierManager(mock)
		assert.Equal(t, mock, srv.GetBarrierManager())
	})

	t.Run("sets nil seal manager", func(t *testing.T) {
		srv := withTempServer(t)
		srv.SetBarrierManager(nil)
		assert.Nil(t, srv.GetBarrierManager())
	})
}

func TestLC_GetRaftNode_NilBeforeInit(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	assert.Nil(t, srv.GetRaftNode())
}

func TestLC_TriggerShutdown_ClosesChannel(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.TriggerShutdown()

	select {
	case <-srv.shutdownChan:
		// Expected: channel is closed
	default:
		t.Fatal("shutdown channel should be closed after TriggerShutdown")
	}
}

func TestLC_TriggerShutdown_Idempotent(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.TriggerShutdown()
	srv.TriggerShutdown() // second call should not panic
}

// =============================================================================
// server.go: validateFIPSMode
// =============================================================================

func TestLC_ValidateFIPSMode_NoError(t *testing.T) {
	t.Parallel()
	log := mustCreateLogger()
	// validateFIPSMode should not fail even if FIPS is not compiled in
	err := validateFIPSMode(log)
	assert.NoError(t, err)
}

// =============================================================================
// lifecycle.go: defaultNewBarrierManager
// =============================================================================

func TestLC_DefaultNewSealManager_ReturnsManager(t *testing.T) {
	t.Parallel()
	log := mustCreateLogger()
	sealCfg := &quicbaasseal.Config{
		Mode: quicbaasseal.SealModeAuto,
	}
	mgr, err := defaultNewBarrierManager(sealCfg, log, t.TempDir())
	assert.NoError(t, err)
	assert.NotNil(t, mgr)
}

// =============================================================================
// lifecycle.go: startStorage
// =============================================================================

func TestLC_StartStorage_MemoryBackend(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Storage.Backend = config.StorageBackendMemory
	err := srv.startStorage()
	assert.NoError(t, err)
}

func TestLC_StartStorage_EmptyBackendDefaultsToMemory(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Storage.Backend = ""
	err := srv.startStorage()
	assert.NoError(t, err)
}

func TestLC_StartStorage_PebbleBackend(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Storage.Backend = config.StorageBackendPebble
	srv.config.Storage.DataDir = filepath.Join(t.TempDir(), "pebble-data")
	err := srv.startStorage()
	assert.NoError(t, err)
}

func TestLC_StartStorage_FileBackend(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Storage.Backend = config.StorageBackendFile
	srv.config.Storage.DataDir = filepath.Join(t.TempDir(), "file-data")
	err := srv.startStorage()
	assert.NoError(t, err)
}

func TestLC_StartStorage_PebbleEmptyDataDir(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Storage.Backend = config.StorageBackendPebble
	srv.config.Storage.DataDir = ""
	err := srv.startStorage()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "data directory")
}

func TestLC_StartStorage_UnrecognizedBackend(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Storage.Backend = "unknown-backend"
	err := srv.startStorage()
	assert.NoError(t, err) // falls back to memory
}

// =============================================================================
// lifecycle.go: startIndexes / stopIndexes (no-ops in BaaS mode)
// =============================================================================

func TestLC_StartIndexes_NoOp(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	assert.NoError(t, srv.startIndexes())
}

func TestLC_StopIndexes_NoOp(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	assert.NoError(t, srv.stopIndexes())
}

// =============================================================================
// lifecycle.go: startKMS disabled path
// =============================================================================

func TestLC_StartKMS_Disabled(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.KMS.Enabled = false
	assert.NoError(t, srv.startKMS())
	assert.Nil(t, srv.kmsManager)
}

// =============================================================================
// lifecycle.go: startSecrets
// =============================================================================

// TestLC_StartSecrets_Disabled verifies that startSecrets is always-on: even when
// Secrets.Enabled is false in config, the secrets manager is initialized because
// the operator decision (2026-04-22) is "if compiled in, it's enabled".
func TestLC_StartSecrets_Disabled(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Secrets.Enabled = false
	assert.NoError(t, srv.startSecrets())
	// always-on: manager is initialized regardless of the config flag
	assert.NotNil(t, srv.secretsManager)
}

func TestLC_StartSecrets_Enabled(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Secrets.Enabled = true
	assert.NoError(t, srv.startSecrets())
	assert.NotNil(t, srv.secretsManager)
}

// =============================================================================
// lifecycle.go: startBackupManager
// =============================================================================

func TestLC_StartBackupManager_Disabled(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Backup.Enabled = false
	assert.NoError(t, srv.startBackupManager())
	assert.Nil(t, srv.backupManager)
}

func TestLC_StartBackupManager_InvalidEncryptionKeySize(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Backup.Enabled = true
	srv.config.Backup.DefaultEncryption = "aes-256-gcm"
	srv.config.Backup.EncryptionKey = "tooshort"
	srv.config.Storage.DataDir = t.TempDir()

	err := srv.startBackupManager()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "encryption key")
}

// =============================================================================
// lifecycle.go: stopBackupManager
// =============================================================================

func TestLC_StopBackupManager_NilNoOp(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.backupManager = nil
	assert.NoError(t, srv.stopBackupManager())
}

// =============================================================================
// lifecycle.go: stopCatalogManager nil paths
// =============================================================================

func TestLC_StopCatalogManager_NilReturnsNil(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.catalogManager = nil
	assert.NoError(t, srv.stopCatalogManager())
}

func TestLC_StopBackendManager_NilReturnsNil(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.backendManager = nil
	assert.NoError(t, srv.stopBackendManager())
}

func TestLC_StopWorkerManager_NilReturnsNil(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.workerManager = nil
	assert.NoError(t, srv.stopWorkerManager())
}

// =============================================================================
// lifecycle.go: startCatalogManager
// =============================================================================

func TestLC_StartCatalogManager_NeitherEnabledNorConfigured(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.ObjectStorage.Enabled = false
	srv.config.ObjectStorage.Backends = nil
	assert.NoError(t, srv.startCatalogManager())
	assert.Nil(t, srv.catalogManager)
}

// =============================================================================
// lifecycle.go: startMessaging
// =============================================================================

func TestLC_StartMessaging_WithService(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory

	seamMu.Lock()
	initializeCAFn = func(_ *Server) error { return nil }
	seamMu.Unlock()
	stubIAMSeam()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	// Create service first (messaging needs it)
	require.NoError(t, srv.startService())
	require.NotNil(t, srv.quicBaaSService)

	err = srv.startMessaging()
	assert.NoError(t, err)
	assert.NotNil(t, srv.messagingService)
}

// =============================================================================
// lifecycle.go: startWatchBridge
// =============================================================================

func TestLC_StartWatchBridge_NilService(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.quicBaaSService = nil
	err := srv.startWatchBridge()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

// =============================================================================
// lifecycle.go: startService with standalone memory mode
// =============================================================================

func TestLC_StartService_StandaloneMemory(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory

	stubIAMSeam()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err = srv.startService()
	assert.NoError(t, err)
	assert.NotNil(t, srv.quicBaaSService)
}

func TestLC_StartService_InvalidMode(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = "bogus-mode"
	cfg.Storage.Backend = config.StorageBackendMemory

	stubIAMSeam()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	// Phase E: startService requires a real qrdb node. An unrecognised mode
	// string is treated as standalone for node construction purposes.
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err = srv.startService()
	assert.NoError(t, err, "bogus mode string is now accepted by the service builder")
}

func TestLC_StartService_WithCacheEnabled(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Cache.Enabled = true
	cfg.Cache.MaxSize = 500
	cfg.Cache.DefaultTTL = 10 * time.Second
	cfg.Cache.CleanupInterval = 5 * time.Second

	stubIAMSeam()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err = srv.startService()
	assert.NoError(t, err)
	assert.NotNil(t, srv.cacheManager)
}

// =============================================================================
// lifecycle.go: startGRPCServer in embedded mode
// =============================================================================

func TestLC_StartGRPCServer_EmbeddedMode(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	assert.NoError(t, srv.startGRPCServer())
	assert.Nil(t, srv.grpcServer)
}

// =============================================================================
// lifecycle.go: stopGRPCServer nil no-op
// =============================================================================

func TestLC_StopGRPCServer_NilNoOp(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.grpcServer = nil
	// should not panic
	srv.stopGRPCServer()
}

// =============================================================================
// lifecycle_databases.go: startDatabases
// =============================================================================

func TestLC_StartDatabases_Disabled(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Databases.Enabled = false
	assert.NoError(t, srv.startDatabases())
}

func TestLC_StartDatabases_NilService(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Databases.Enabled = true
	srv.quicBaaSService = nil
	assert.NoError(t, srv.startDatabases())
}

func TestLC_StartDatabases_WithService(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Databases.Enabled = true
	cfg.Databases.DataDir = filepath.Join(t.TempDir(), "databases")

	stubIAMSeam()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	require.NoError(t, srv.startService())
	require.NotNil(t, srv.quicBaaSService)

	err = srv.startDatabases()
	assert.NoError(t, err)
}

func TestLC_StartDatabases_DefaultDataDir(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Databases.Enabled = true
	cfg.Databases.DataDir = "" // empty means default
	cfg.Storage.DataDir = t.TempDir()

	stubIAMSeam()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	require.NoError(t, srv.startService())

	err = srv.startDatabases()
	assert.NoError(t, err)
}

// =============================================================================
// lifecycle.go: initBarrier
// =============================================================================

func TestLC_InitBarrier_Disabled(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Seal.Enabled = false
	assert.NoError(t, srv.initBarrier())
	assert.Nil(t, srv.barrierManager)
}

func TestLC_InitBarrier_Enabled(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)

	srv := withTempServer(t)
	srv.config.Seal.Enabled = true
	srv.config.Seal.Mode = "auto"
	srv.config.Storage.DataDir = t.TempDir()

	err := srv.initBarrier()
	assert.NoError(t, err)
	assert.NotNil(t, srv.barrierManager)
}

func TestLC_InitBarrier_CustomBarrierDir(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)

	srv := withTempServer(t)
	srv.config.Seal.Enabled = true
	srv.config.Seal.Mode = "auto"
	srv.config.Seal.BarrierDir = filepath.Join(t.TempDir(), "custom-barrier")
	srv.config.Storage.DataDir = t.TempDir()

	err := srv.initBarrier()
	assert.NoError(t, err)
	assert.NotNil(t, srv.barrierManager)
}

// =============================================================================
// server.go: Shutdown comprehensive paths
// =============================================================================

func TestLC_Shutdown_AlreadyClosed(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.mu.Lock()
	srv.closed = true
	srv.mu.Unlock()

	err := srv.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestLC_Shutdown_CleanServer(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	err := srv.Shutdown(context.Background())
	assert.NoError(t, err)
	assert.True(t, srv.IsClosed())
}

func TestLC_Shutdown_Idempotent(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	ctx := context.Background()
	assert.NoError(t, srv.Shutdown(ctx))
	assert.NoError(t, srv.Shutdown(ctx))
}

// =============================================================================
// server.go: initialize guards
// =============================================================================

func TestLC_Initialize_AlreadyStarted(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.mu.Lock()
	srv.started = true
	srv.mu.Unlock()

	err := srv.initialize()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")
}

func TestLC_Initialize_AlreadyClosed(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.mu.Lock()
	srv.closed = true
	srv.mu.Unlock()

	err := srv.initialize()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

// =============================================================================
// lifecycle.go: startBackupManager compression and encryption paths
// =============================================================================

func TestLC_StartBackupManager_GzipCompression(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Backup.Enabled = true
	srv.config.Backup.DefaultCompression = "gzip"
	srv.config.Backup.DefaultEncryption = "none"
	srv.config.Storage.DataDir = t.TempDir()
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err := srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	require.NoError(t, srv.stopBackupManager())
}

func TestLC_StartBackupManager_ZstdCompression(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Backup.Enabled = true
	srv.config.Backup.DefaultCompression = "zstd"
	srv.config.Backup.DefaultEncryption = "none"
	srv.config.Storage.DataDir = t.TempDir()
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err := srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	require.NoError(t, srv.stopBackupManager())
}

func TestLC_StartBackupManager_NoCompression(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Backup.Enabled = true
	srv.config.Backup.DefaultCompression = "none"
	srv.config.Backup.DefaultEncryption = "none"
	srv.config.Storage.DataDir = t.TempDir()
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err := srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	require.NoError(t, srv.stopBackupManager())
}

func TestLC_StartBackupManager_AES256GCMNoKey(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Backup.Enabled = true
	srv.config.Backup.DefaultEncryption = "aes-256-gcm"
	srv.config.Backup.EncryptionKey = "" // no key provided
	srv.config.Storage.DataDir = t.TempDir()
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	// Should succeed but disable encryption (logs a warning)
	err := srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	require.NoError(t, srv.stopBackupManager())
}

func TestLC_StartBackupManager_CustomWorkDir(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	workDir := filepath.Join(t.TempDir(), "custom-work")
	srv.config.Backup.Enabled = true
	srv.config.Backup.WorkDir = workDir
	srv.config.Backup.DefaultEncryption = "none"
	srv.config.Storage.DataDir = t.TempDir()
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err := srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	// Verify work directory was created
	_, statErr := os.Stat(filepath.Join(workDir, "storage"))
	assert.NoError(t, statErr)

	require.NoError(t, srv.stopBackupManager())
}

func TestLC_StartBackupManager_CustomTimeout(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Backup.Enabled = true
	srv.config.Backup.Timeout = 10 * time.Minute
	srv.config.Backup.DefaultEncryption = "none"
	srv.config.Storage.DataDir = t.TempDir()
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err := srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	require.NoError(t, srv.stopBackupManager())
}

func TestLC_StartBackupManager_DefaultCompressionEmpty(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Backup.Enabled = true
	srv.config.Backup.DefaultCompression = "" // defaults to zstd
	srv.config.Backup.DefaultEncryption = ""  // defaults to none
	srv.config.Storage.DataDir = t.TempDir()
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err := srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	require.NoError(t, srv.stopBackupManager())
}

func TestLC_StartBackupManager_UnknownCompressionDefaultsZstd(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Backup.Enabled = true
	srv.config.Backup.DefaultCompression = "unknown-algo"
	srv.config.Backup.DefaultEncryption = "unknown-enc" // defaults to none
	srv.config.Storage.DataDir = t.TempDir()
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err := srv.startBackupManager()
	assert.NoError(t, err)
	assert.NotNil(t, srv.backupManager)

	require.NoError(t, srv.stopBackupManager())
}

// =============================================================================
// lifecycle.go: stopBackupManager with backup storage adapter
// =============================================================================

func TestLC_StopBackupManager_WithBackupStorage(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)

	srv := withTempServer(t)
	srv.config.Backup.Enabled = true
	srv.config.Backup.DefaultEncryption = "none"
	srv.config.Storage.DataDir = t.TempDir()
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	require.NoError(t, srv.startBackupManager())
	require.NotNil(t, srv.backupManager)
	require.NotNil(t, srv.backupStorage)

	err := srv.stopBackupManager()
	assert.NoError(t, err)
	assert.Nil(t, srv.backupManager)
	assert.Nil(t, srv.backupStorage)
}

// =============================================================================
// Backup storage adapter: error propagation
// =============================================================================

func TestLC_BackupStorageAdapter_GetError(t *testing.T) {
	inner := &mockStorage{
		getFunc: func(key []byte) ([]byte, error) {
			return nil, &lcTestError{msg: "get failed"}
		},
	}
	adapter := newBackupStorageAdapter(inner)
	_, err := adapter.Get([]byte("k"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "get failed")
}

func TestLC_BackupStorageAdapter_SetError(t *testing.T) {
	inner := &mockStorage{
		setFunc: func(key, value []byte) error {
			return &lcTestError{msg: "set failed"}
		},
	}
	adapter := newBackupStorageAdapter(inner)
	err := adapter.Set([]byte("k"), []byte("v"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "set failed")
}

func TestLC_BackupStorageAdapter_DeleteError(t *testing.T) {
	inner := &mockStorage{
		deleteFunc: func(key []byte) error {
			return &lcTestError{msg: "delete failed"}
		},
	}
	adapter := newBackupStorageAdapter(inner)
	err := adapter.Delete([]byte("k"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delete failed")
}

func TestLC_BackupStorageAdapter_CloseError(t *testing.T) {
	inner := &mockStorage{
		closeFunc: func() error {
			return &lcTestError{msg: "close failed"}
		},
	}
	adapter := newBackupStorageAdapter(inner)
	err := adapter.Close()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "close failed")
}

// =============================================================================
// lifecycle.go: initializeCA - TLS disabled path
// =============================================================================

func TestLC_InitializeCA_TLSDisabled(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.config.Security.TLSEnabled = false
	assert.NoError(t, srv.initializeCA())
}

// =============================================================================
// server.go: Start in embedded mode
// =============================================================================

func TestLC_Start_EmbeddedMode(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Embedded = true
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory

	// Stub CA
	seamMu.Lock()
	initializeCAFn = func(_ *Server) error { return nil }
	seamMu.Unlock()
	stubIAMSeam()

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.Start()
	assert.NoError(t, err)
	assert.True(t, srv.IsStarted())

	// Cleanup
	require.NoError(t, srv.Shutdown(context.Background()))
}

// =============================================================================
// lifecycle.go: startService with scheduler config
// =============================================================================

func TestLC_StartService_WithScheduler(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Scheduler.Enabled = true
	cfg.Scheduler.TickInterval = 1 * time.Second
	cfg.Scheduler.MaxConcurrentJobs = 5

	stubIAMSeam()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err = srv.startService()
	assert.NoError(t, err)
	assert.NotNil(t, srv.quicBaaSService)
}

// =============================================================================
// server.go: full initialize and shutdown lifecycle in embedded mode
// =============================================================================

func TestLC_FullLifecycle_EmbeddedStandaloneMemory(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Server.Embedded = true

	// Stub CA and IAM
	seamMu.Lock()
	initializeCAFn = func(_ *Server) error { return nil }
	seamMu.Unlock()
	stubIAMSeam()

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.initialize()
	require.NoError(t, err)
	assert.True(t, srv.IsStarted())
	assert.NotNil(t, srv.quicBaaSService)

	// Shutdown covers all the stop paths
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = srv.Shutdown(ctx)
	assert.NoError(t, err)
	assert.True(t, srv.IsClosed())
}

// =============================================================================
// lifecycle.go: startMessaging with buffer and dedup config
// =============================================================================

func TestLC_StartMessaging_WithBufferConfig(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Messaging.BufferSize = 2048
	cfg.Messaging.DedupEnabled = true
	cfg.Messaging.DedupWindow = 60

	seamMu.Lock()
	initializeCAFn = func(_ *Server) error { return nil }
	seamMu.Unlock()
	stubIAMSeam()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	require.NoError(t, srv.startService())
	require.NotNil(t, srv.quicBaaSService)

	err = srv.startMessaging()
	assert.NoError(t, err)
	assert.NotNil(t, srv.messagingService)
}

// =============================================================================
// lifecycle.go: startService with vector and timeseries shard config
// =============================================================================

func TestLC_StartService_WithVectorEnabled(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Vector.Enabled = true
	cfg.Vector.ShardID = 200

	stubIAMSeam()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err = srv.startService()
	assert.NoError(t, err)
	assert.NotNil(t, srv.quicBaaSService)
}

func TestLC_StartService_WithTimeSeriesEnabled(t *testing.T) {
	saveAndRestoreLifecycleSeams(t)

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.TimeSeries.Enabled = true
	cfg.TimeSeries.ShardID = 300

	stubIAMSeam()

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err = srv.startService()
	assert.NoError(t, err)
	assert.NotNil(t, srv.quicBaaSService)
}
