//go:build raft && rest && grpc && quic && mcp && kms && objectstorage

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

func TestStartKMSLifecycle_EnabledSuccess(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.KMS.Enabled = true
	cfg.KMS.Backend = config.BackendTypeSoftware
	cfg.KMS.EnabledBackends = []config.BackendType{config.BackendTypeSoftware}

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startKMS()
	require.NoError(t, err)
	assert.NotNil(t, srv.kmsManager)
}

func TestStartKMSLifecycle_EmptyBackendDefaults(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.KMS.Enabled = true
	cfg.KMS.Backend = ""
	cfg.KMS.EnabledBackends = []config.BackendType{config.BackendTypeSoftware}

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startKMS()
	require.NoError(t, err)
	assert.NotNil(t, srv.kmsManager)
}

func TestStartCatalogManagerLifecycle_WithBackends(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.ObjectStorage.Backends = map[string]*config.StorageBackendConfig{
		"test": {
			Type: "filesystem",
			Config: map[string]string{
				"root_dir": t.TempDir(),
			},
		},
	}

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startCatalogManager()
	require.NoError(t, err)
	assert.NotNil(t, srv.catalogManager)
	assert.NotNil(t, srv.backendManager)
	assert.NotNil(t, srv.workerManager)

	// Clean up
	require.NoError(t, srv.stopWorkerManager())
	require.NoError(t, srv.stopBackendManager())
	require.NoError(t, srv.stopCatalogManager())
}

func TestStopCatalogManagerLifecycle_WithManager(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.ObjectStorage.Backends = map[string]*config.StorageBackendConfig{
		"test": {
			Type: "filesystem",
			Config: map[string]string{
				"root_dir": t.TempDir(),
			},
		},
	}

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	require.NoError(t, srv.startCatalogManager())
	require.NotNil(t, srv.catalogManager)

	// Stop workers first, then backend, then catalog
	require.NoError(t, srv.stopWorkerManager())
	require.NoError(t, srv.stopBackendManager())
	require.NoError(t, srv.stopCatalogManager())
	assert.Nil(t, srv.catalogManager)
}

func TestStopBackendManagerLifecycle_WithManager(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.ObjectStorage.Backends = map[string]*config.StorageBackendConfig{
		"test": {
			Type: "filesystem",
			Config: map[string]string{
				"root_dir": t.TempDir(),
			},
		},
	}

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	require.NoError(t, srv.startCatalogManager())
	require.NotNil(t, srv.backendManager)

	require.NoError(t, srv.stopWorkerManager())
	require.NoError(t, srv.stopBackendManager())
	assert.Nil(t, srv.backendManager)

	// Clean up remaining
	require.NoError(t, srv.stopCatalogManager())
}

func TestStopWorkerManagerLifecycle_WithManager(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.ObjectStorage.Backends = map[string]*config.StorageBackendConfig{
		"test": {
			Type: "filesystem",
			Config: map[string]string{
				"root_dir": t.TempDir(),
			},
		},
	}

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	require.NoError(t, srv.startCatalogManager())
	require.NotNil(t, srv.workerManager)

	require.NoError(t, srv.stopWorkerManager())
	assert.Nil(t, srv.workerManager)

	// Clean up remaining
	require.NoError(t, srv.stopBackendManager())
	require.NoError(t, srv.stopCatalogManager())
}

func TestStartMessagingLifecycle_WithService(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Need a service first
	require.NoError(t, srv.startService())
	require.NotNil(t, srv.quicBaaSService)

	err = srv.startMessaging()
	require.NoError(t, err)
	assert.NotNil(t, srv.messagingService)
}

func TestStartMessagingLifecycle_WithBufferConfig(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Messaging.BufferSize = 2048
	cfg.Messaging.DedupEnabled = true
	cfg.Messaging.DedupWindow = 60

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	require.NoError(t, srv.startService())

	err = srv.startMessaging()
	require.NoError(t, err)
	assert.NotNil(t, srv.messagingService)
}

func TestStartMessagingLifecycle_WithStorage(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Start with pebble storage available
	require.NoError(t, srv.startStorage())
	t.Cleanup(func() { srv.storage.Close() })
	require.NoError(t, srv.startService())

	err = srv.startMessaging()
	require.NoError(t, err)
	assert.NotNil(t, srv.messagingService)
}

func TestStartWatchBridgeLifecycle_NilService(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.quicBaaSService = nil

	err := srv.startWatchBridge()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestStartWatchBridgeLifecycle_WithService(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	require.NoError(t, srv.startService())

	err = srv.startWatchBridge()
	require.NoError(t, err)
}
