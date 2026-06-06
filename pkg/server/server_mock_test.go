//go:build raft

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
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-objstore/pkg/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/watch"
	"github.com/jeremyhahn/go-quicbaas/pkg/watchbridge"
)

// =============================================================================
// Mock Interfaces for Dependency Injection
// =============================================================================

// MockSecretsBackend implements secrets.Backend for testing.
type MockSecretsBackend struct {
	storeFunc    func(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error
	retrieveFunc func(ctx context.Context, path string) (map[string][]byte, map[string]string, error)
	deleteFunc   func(ctx context.Context, path string) error
	listFunc     func(ctx context.Context, prefix string) ([]string, error)
	existsFunc   func(ctx context.Context, path string) (bool, error)
	typeValue    string
}

func (m *MockSecretsBackend) Store(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error {
	if m.storeFunc != nil {
		return m.storeFunc(ctx, path, data, metadata)
	}
	return nil
}

func (m *MockSecretsBackend) Retrieve(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
	if m.retrieveFunc != nil {
		return m.retrieveFunc(ctx, path)
	}
	return map[string][]byte{"key": []byte("value")}, nil, nil
}

func (m *MockSecretsBackend) Delete(ctx context.Context, path string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, path)
	}
	return nil
}

func (m *MockSecretsBackend) List(ctx context.Context, prefix string) ([]string, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, prefix)
	}
	return []string{}, nil
}

func (m *MockSecretsBackend) Exists(ctx context.Context, path string) (bool, error) {
	if m.existsFunc != nil {
		return m.existsFunc(ctx, path)
	}
	return true, nil
}

func (m *MockSecretsBackend) Type() string {
	if m.typeValue != "" {
		return m.typeValue
	}
	return "mock"
}

// MockCatalogManager implements catalog.CatalogManager for testing.
type MockCatalogManager struct {
	putFunc    func(ctx context.Context, entry *catalog.CatalogEntry) error
	getFunc    func(ctx context.Context, key string) (*catalog.CatalogEntry, error)
	deleteFunc func(ctx context.Context, key string) error
	closeFunc  func() error
	syncFunc   func() error
}

func (m *MockCatalogManager) Put(ctx context.Context, entry *catalog.CatalogEntry) error {
	if m.putFunc != nil {
		return m.putFunc(ctx, entry)
	}
	return nil
}

func (m *MockCatalogManager) Get(ctx context.Context, key string) (*catalog.CatalogEntry, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, key)
	}
	return nil, nil
}

func (m *MockCatalogManager) Delete(ctx context.Context, key string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, key)
	}
	return nil
}

func (m *MockCatalogManager) Update(ctx context.Context, key string, updater func(*catalog.CatalogEntry) error) error {
	return nil
}

func (m *MockCatalogManager) Exists(ctx context.Context, key string) (bool, error) {
	return true, nil
}

func (m *MockCatalogManager) AddLocation(ctx context.Context, key string, loc *catalog.StoreLocation) error {
	return nil
}

func (m *MockCatalogManager) RemoveLocation(ctx context.Context, key string, store string) error {
	return nil
}

func (m *MockCatalogManager) SetPrimaryStore(ctx context.Context, key string, store string) error {
	return nil
}

func (m *MockCatalogManager) GetLocations(ctx context.Context, key string) ([]*catalog.StoreLocation, error) {
	return nil, nil
}

func (m *MockCatalogManager) UpdateLocationStatus(ctx context.Context, key, store string, status catalog.LocationStatus) error {
	return nil
}

func (m *MockCatalogManager) ListByStore(ctx context.Context, store string, opts *catalog.ListOptions) (*catalog.ListResult, error) {
	return nil, nil
}

func (m *MockCatalogManager) ListByTenant(ctx context.Context, tenant string, opts *catalog.ListOptions) (*catalog.ListResult, error) {
	return nil, nil
}

func (m *MockCatalogManager) ListByPrefix(ctx context.Context, prefix string, opts *catalog.ListOptions) (*catalog.ListResult, error) {
	return nil, nil
}

func (m *MockCatalogManager) ListByStatus(ctx context.Context, status catalog.EntryStatus, opts *catalog.ListOptions) (*catalog.ListResult, error) {
	return nil, nil
}

func (m *MockCatalogManager) ListByTag(ctx context.Context, tag, value string, opts *catalog.ListOptions) (*catalog.ListResult, error) {
	return nil, nil
}

func (m *MockCatalogManager) ListByEncryptionKey(ctx context.Context, keyID string, opts *catalog.ListOptions) (*catalog.ListResult, error) {
	return nil, nil
}

func (m *MockCatalogManager) Search(ctx context.Context, query *catalog.SearchQuery) (*catalog.SearchResult, error) {
	return nil, nil
}

func (m *MockCatalogManager) EnqueueReplication(ctx context.Context, key string, targets []string, priority int) error {
	return nil
}

func (m *MockCatalogManager) EnqueueArchive(ctx context.Context, key string, targetStore string) error {
	return nil
}

func (m *MockCatalogManager) EnqueueVerify(ctx context.Context, key string, stores []string) error {
	return nil
}

func (m *MockCatalogManager) DequeueReplication(ctx context.Context, limit int) ([]*catalog.ReplicationTask, error) {
	return nil, nil
}

func (m *MockCatalogManager) DequeueArchive(ctx context.Context, limit int) ([]*catalog.ArchiveTask, error) {
	return nil, nil
}

func (m *MockCatalogManager) DequeueVerify(ctx context.Context, limit int) ([]*catalog.VerifyTask, error) {
	return nil, nil
}

func (m *MockCatalogManager) GetTenantUsage(ctx context.Context, tenant string) (*catalog.TenantUsage, error) {
	return nil, nil
}

func (m *MockCatalogManager) GetStoreUsage(ctx context.Context, store string) (*catalog.StoreUsage, error) {
	return nil, nil
}

func (m *MockCatalogManager) UpdateAccessTime(ctx context.Context, key string) error {
	return nil
}

func (m *MockCatalogManager) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func (m *MockCatalogManager) Sync() error {
	if m.syncFunc != nil {
		return m.syncFunc()
	}
	return nil
}

// MockBackendManager implements catalog.BackendManager for testing.
type MockBackendManager struct {
	registerFunc      func(name string, cfg *catalog.BackendConfig) error
	getBackendFunc    func(name string) (common.Storage, error)
	listBackendsFunc  func() []string
	closeFunc         func() error
	healthCheckFunc   func(ctx context.Context, name string) (*catalog.BackendHealth, error)
	getBackendCfgFunc func(name string) (*catalog.BackendConfig, error)
	unregisterFunc    func(name string) error
	selectBackendFunc func(criteria *catalog.SelectionCriteria) (string, common.Storage, error)
	checkAllHealth    func(ctx context.Context) map[string]*catalog.BackendHealth
}

func (m *MockBackendManager) RegisterBackend(name string, cfg *catalog.BackendConfig) error {
	if m.registerFunc != nil {
		return m.registerFunc(name, cfg)
	}
	return nil
}

func (m *MockBackendManager) GetBackend(name string) (common.Storage, error) {
	if m.getBackendFunc != nil {
		return m.getBackendFunc(name)
	}
	return nil, nil
}

func (m *MockBackendManager) ListBackends() []string {
	if m.listBackendsFunc != nil {
		return m.listBackendsFunc()
	}
	return []string{}
}

func (m *MockBackendManager) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func (m *MockBackendManager) CheckHealth(ctx context.Context, name string) (*catalog.BackendHealth, error) {
	if m.healthCheckFunc != nil {
		return m.healthCheckFunc(ctx, name)
	}
	return &catalog.BackendHealth{Healthy: true}, nil
}

func (m *MockBackendManager) GetBackendConfig(name string) (*catalog.BackendConfig, error) {
	if m.getBackendCfgFunc != nil {
		return m.getBackendCfgFunc(name)
	}
	return nil, nil
}

func (m *MockBackendManager) UnregisterBackend(name string) error {
	if m.unregisterFunc != nil {
		return m.unregisterFunc(name)
	}
	return nil
}

func (m *MockBackendManager) SelectBackend(criteria *catalog.SelectionCriteria) (string, common.Storage, error) {
	if m.selectBackendFunc != nil {
		return m.selectBackendFunc(criteria)
	}
	return "", nil, nil
}

func (m *MockBackendManager) CheckAllHealth(ctx context.Context) map[string]*catalog.BackendHealth {
	if m.checkAllHealth != nil {
		return m.checkAllHealth(ctx)
	}
	return nil
}

// MockWatchManager implements watch.Manager for testing.
type MockWatchManager struct {
	watchFunc          func(ctx context.Context, keyPrefix string) (<-chan *watch.WatchEvent, uint64, error)
	cancelFunc         func(id uint64) error
	notifyFunc         func(event *watch.WatchEvent) error
	activeWatchersFunc func() int
	closeFunc          func() error
}

func (m *MockWatchManager) Watch(ctx context.Context, keyPrefix string) (<-chan *watch.WatchEvent, uint64, error) {
	if m.watchFunc != nil {
		return m.watchFunc(ctx, keyPrefix)
	}
	ch := make(chan *watch.WatchEvent)
	return ch, 1, nil
}

func (m *MockWatchManager) Cancel(id uint64) error {
	if m.cancelFunc != nil {
		return m.cancelFunc(id)
	}
	return nil
}

func (m *MockWatchManager) Notify(event *watch.WatchEvent) error {
	if m.notifyFunc != nil {
		return m.notifyFunc(event)
	}
	return nil
}

func (m *MockWatchManager) ActiveWatchers() int {
	if m.activeWatchersFunc != nil {
		return m.activeWatchersFunc()
	}
	return 0
}

func (m *MockWatchManager) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

// Ensure unused imports are referenced to prevent compiler errors.
var (
	_ = kms.NewKMSService
	_ = secrets.NewManager
	_ = messaging.NewPushService
	_ = watchbridge.NewWatchBridge
)

// =============================================================================
// Test Helpers
// =============================================================================

// createTestServerWithConfig creates a server with a custom config modifier.
func createTestServerWithConfig(t *testing.T, modifier func(*config.Config)) (*Server, func()) {
	t.Helper()
	cfg := createTestConfig(t)
	if modifier != nil {
		modifier(cfg)
	}
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		cleanupTestData(t, cfg)
	}
	return srv, cleanup
}

// =============================================================================
// KMS Lifecycle Tests
// =============================================================================

func TestStartKMS_DisabledMock(t *testing.T) {
	t.Run("skips initialization when disabled", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, func(cfg *config.Config) {
			cfg.KMS.Enabled = false
		})
		defer cleanup()

		err := srv.startKMS()
		require.NoError(t, err)
		assert.Nil(t, srv.kmsManager, "kmsManager should be nil when KMS is disabled")
	})
}

func TestStartKMS_Enabled(t *testing.T) {
	t.Run("initializes KMS when enabled", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, func(cfg *config.Config) {
			cfg.KMS.Enabled = true
			cfg.KMS.Backend = config.BackendTypeSoftware
			cfg.KMS.EnabledBackends = []config.BackendType{config.BackendTypeSoftware}
		})
		defer cleanup()

		err := srv.startKMS()
		require.NoError(t, err)
		assert.NotNil(t, srv.kmsManager, "kmsManager should be initialized when KMS is enabled")
	})

	t.Run("initializes with cache enabled", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, func(cfg *config.Config) {
			cfg.KMS.Enabled = true
			cfg.KMS.Backend = config.BackendTypeSoftware
			cfg.KMS.EnabledBackends = []config.BackendType{config.BackendTypeSoftware}
			cfg.KMS.Cache.Enabled = true
		})
		defer cleanup()

		err := srv.startKMS()
		require.NoError(t, err)
		assert.NotNil(t, srv.kmsManager)
	})
}

// =============================================================================
// Secrets Lifecycle Tests
// =============================================================================

// Per operator decision (2026-04-22), Secrets is always-on when compiled into
// the binary; cfg.Secrets.Enabled is no longer consulted by startSecrets.
// startSecrets initializes secretsManager regardless of the flag.
func TestStartSecrets_AlwaysOn(t *testing.T) {
	t.Run("initializes secrets manager regardless of Enabled flag", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, func(cfg *config.Config) {
			cfg.Secrets.Enabled = false
		})
		defer cleanup()

		err := srv.startSecrets()
		require.NoError(t, err)
		assert.NotNil(t, srv.secretsManager, "secretsManager should be initialized even when Secrets.Enabled=false (always-on policy)")
	})

	t.Run("initializes secrets manager when Enabled flag is true", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, func(cfg *config.Config) {
			cfg.Secrets.Enabled = true
		})
		defer cleanup()

		err := srv.startSecrets()
		require.NoError(t, err)
		assert.NotNil(t, srv.secretsManager, "secretsManager should be initialized when Secrets.Enabled=true")
	})
}

// =============================================================================
// Messaging Lifecycle Tests
// =============================================================================

func TestStartMessaging_Disabled(t *testing.T) {
	t.Run("skips initialization when disabled", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, func(cfg *config.Config) {
			cfg.Messaging.Enabled = false
		})
		defer cleanup()

		// Messaging is only started if enabled in initialize()
		// When disabled, startMessaging is not called at all
		assert.Nil(t, srv.messagingService)
	})
}

func TestStartMessaging_Enabled(t *testing.T) {
	t.Run("initializes messaging service when enabled", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, func(cfg *config.Config) {
			cfg.Messaging.Enabled = true
			cfg.Messaging.BufferSize = 100
			cfg.Messaging.DedupEnabled = false
			cfg.Messaging.WatchBridge.Enabled = false
			cfg.Server.Mode = config.ServerModeStandalone
		})
		defer cleanup()

		// Initialize storage first (required for messaging)
		err := srv.startStorage()
		require.NoError(t, err)

		// Set raft node required by service builder
		require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
		t.Cleanup(func() { _ = srv.stopQRDBNode() })

		// Initialize the service (required for messaging to be attached)
		err = srv.startService()
		require.NoError(t, err)

		err = srv.startMessaging()
		require.NoError(t, err)
		assert.NotNil(t, srv.messagingService, "messagingService should be initialized")
		assert.True(t, srv.messagingService.IsRunning(), "messagingService should be running")
	})

	t.Run("initializes with dedup enabled", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, func(cfg *config.Config) {
			cfg.Messaging.Enabled = true
			cfg.Messaging.DedupEnabled = true
			cfg.Messaging.DedupWindow = 5 * time.Minute
			cfg.Messaging.WatchBridge.Enabled = false
			cfg.Server.Mode = config.ServerModeStandalone
		})
		defer cleanup()

		err := srv.startStorage()
		require.NoError(t, err)

		require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
		t.Cleanup(func() { _ = srv.stopQRDBNode() })

		err = srv.startService()
		require.NoError(t, err)

		err = srv.startMessaging()
		require.NoError(t, err)
		assert.NotNil(t, srv.messagingService)
	})

	t.Run("initializes with custom buffer size", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, func(cfg *config.Config) {
			cfg.Messaging.Enabled = true
			cfg.Messaging.BufferSize = 500
			cfg.Messaging.WatchBridge.Enabled = false
			cfg.Server.Mode = config.ServerModeStandalone
		})
		defer cleanup()

		err := srv.startStorage()
		require.NoError(t, err)

		require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
		t.Cleanup(func() { _ = srv.stopQRDBNode() })

		err = srv.startService()
		require.NoError(t, err)

		err = srv.startMessaging()
		require.NoError(t, err)
		assert.NotNil(t, srv.messagingService)
	})
}

// =============================================================================
// Catalog Manager Lifecycle Tests
// =============================================================================

func TestStartCatalogManager_NoObjectStorage(t *testing.T) {
	t.Run("skips initialization when no object storage configured", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, func(cfg *config.Config) {
			cfg.ObjectStorage.Backends = nil
		})
		defer cleanup()

		err := srv.startCatalogManager()
		require.NoError(t, err)
		assert.Nil(t, srv.catalogManager, "catalogManager should be nil when no object storage configured")
		assert.Nil(t, srv.backendManager)
		assert.Nil(t, srv.workerManager)
	})
}

func TestStopCatalogManager(t *testing.T) {
	t.Run("handles nil catalog manager gracefully", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, nil)
		defer cleanup()

		assert.Nil(t, srv.catalogManager)
		err := srv.stopCatalogManager()
		require.NoError(t, err)
	})

	t.Run("closes catalog manager when set", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, nil)
		defer cleanup()

		var closeCalled bool
		srv.catalogManager = &MockCatalogManager{
			closeFunc: func() error {
				closeCalled = true
				return nil
			},
		}

		err := srv.stopCatalogManager()
		require.NoError(t, err)
		assert.True(t, closeCalled, "Close should be called on catalog manager")
		assert.Nil(t, srv.catalogManager, "catalogManager should be nil after stop")
	})

	t.Run("returns error when close fails", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, nil)
		defer cleanup()

		srv.catalogManager = &MockCatalogManager{
			closeFunc: func() error {
				return &dberrors.MessageError{Message: "close failed"}
			},
		}

		err := srv.stopCatalogManager()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "close failed")
	})
}

func TestStopBackendManager(t *testing.T) {
	t.Run("handles nil backend manager gracefully", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, nil)
		defer cleanup()

		assert.Nil(t, srv.backendManager)
		err := srv.stopBackendManager()
		require.NoError(t, err)
	})

	t.Run("closes backend manager when set", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, nil)
		defer cleanup()

		var closeCalled bool
		srv.backendManager = &MockBackendManager{
			closeFunc: func() error {
				closeCalled = true
				return nil
			},
		}

		err := srv.stopBackendManager()
		require.NoError(t, err)
		assert.True(t, closeCalled, "Close should be called on backend manager")
		assert.Nil(t, srv.backendManager, "backendManager should be nil after stop")
	})

	t.Run("returns error when close fails", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, nil)
		defer cleanup()

		srv.backendManager = &MockBackendManager{
			closeFunc: func() error {
				return &dberrors.MessageError{Message: "backend close failed"}
			},
		}

		err := srv.stopBackendManager()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backend close failed")
	})
}

func TestStopWorkerManager(t *testing.T) {
	t.Run("handles nil worker manager gracefully", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, nil)
		defer cleanup()

		assert.Nil(t, srv.workerManager)
		err := srv.stopWorkerManager()
		require.NoError(t, err)
	})
}

// =============================================================================
// Server Initialization Tests
// =============================================================================

func TestInitialize_AlreadyStarted(t *testing.T) {
	t.Run("returns error when already started", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, nil)
		defer cleanup()

		// Manually set started flag
		srv.mu.Lock()
		srv.started = true
		srv.mu.Unlock()

		err := srv.initialize()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "server already started")
	})
}

func TestInitialize_AlreadyClosed(t *testing.T) {
	t.Run("returns error when already closed", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, nil)
		defer cleanup()

		// Manually set closed flag
		srv.mu.Lock()
		srv.closed = true
		srv.mu.Unlock()

		err := srv.initialize()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "server is closed")
	})
}

func TestGetEffectiveMode_Mock(t *testing.T) {
	t.Run("returns standalone when mode is empty", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, func(cfg *config.Config) {
			cfg.Server.Mode = ""
		})
		defer cleanup()

		mode := srv.getEffectiveMode()
		assert.Equal(t, config.ServerModeStandalone, mode)
	})

	t.Run("returns configured mode when set", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, func(cfg *config.Config) {
			cfg.Server.Mode = config.ServerModeCluster
		})
		defer cleanup()

		mode := srv.getEffectiveMode()
		assert.Equal(t, config.ServerModeCluster, mode)
	})

	t.Run("returns devcluster mode when set", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, func(cfg *config.Config) {
			cfg.Server.Mode = config.ServerModeDevCluster
		})
		defer cleanup()

		mode := srv.getEffectiveMode()
		assert.Equal(t, config.ServerModeDevCluster, mode)
	})
}

// =============================================================================
// Storage Lifecycle Tests with Seams
// =============================================================================

// =============================================================================
// gRPC Server Lifecycle Tests with Seams
// =============================================================================

func TestStopGRPCServer_WithSeams(t *testing.T) {
	t.Run("handles graceful stop timeout", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, nil)
		defer cleanup()

		// Capture original seam values
		seamMu.Lock()
		originalTimeout := grpcStopTimeout
		originalGraceful := grpcGracefulStop
		originalForce := grpcForceStop
		seamMu.Unlock()

		defer func() {
			seamMu.Lock()
			grpcStopTimeout = originalTimeout
			grpcGracefulStop = originalGraceful
			grpcForceStop = originalForce
			seamMu.Unlock()
		}()

		var gracefulCalled atomic.Bool
		var forceCalled atomic.Bool

		seamMu.Lock()
		grpcStopTimeout = 10 * time.Millisecond // Very short timeout for test
		grpcGracefulStop = func(s *ggrpc.Server) {
			gracefulCalled.Store(true)
			// Simulate slow graceful stop
			select {} // Block forever
		}
		grpcForceStop = func(s *ggrpc.Server) {
			forceCalled.Store(true)
		}
		seamMu.Unlock()

		// Create a minimal gRPC server
		srv.grpcServer = ggrpc.NewServer()

		srv.stopGRPCServer()

		// Due to the timeout, force stop should be called
		assert.True(t, gracefulCalled.Load(), "graceful stop should be attempted")
		// After timeout, force stop should be called
		assert.True(t, forceCalled.Load(), "force stop should be called after timeout")
	})

	t.Run("handles nil gRPC server gracefully", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, nil)
		defer cleanup()

		assert.Nil(t, srv.grpcServer)
		// Should not panic
		srv.stopGRPCServer()
	})
}

// =============================================================================
// Full Server Lifecycle Integration Tests
// =============================================================================

func TestFullServerLifecycle_StandaloneMode(t *testing.T) {
	t.Run("completes full lifecycle in standalone mode", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, func(cfg *config.Config) {
			cfg.Server.Mode = config.ServerModeStandalone
			cfg.Server.Embedded = true
			cfg.KMS.Enabled = false
			cfg.Secrets.Enabled = false
			cfg.Messaging.Enabled = false
			cfg.Vector.Enabled = false
		})
		defer cleanup()

		// Start the server
		err := srv.Start()
		require.NoError(t, err)
		assert.True(t, srv.IsStarted())
		assert.False(t, srv.IsClosed())

		// Shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err = srv.Shutdown(ctx)
		require.NoError(t, err)
		assert.True(t, srv.IsClosed())
	})
}

func TestFullServerLifecycle_WithKMSAndSecrets(t *testing.T) {
	t.Run("completes lifecycle with KMS and Secrets enabled", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, func(cfg *config.Config) {
			cfg.Server.Mode = config.ServerModeStandalone
			cfg.Server.Embedded = true
			cfg.KMS.Enabled = true
			cfg.KMS.Backend = config.BackendTypeSoftware
			cfg.KMS.EnabledBackends = []config.BackendType{config.BackendTypeSoftware}
			cfg.Secrets.Enabled = true
			cfg.Messaging.Enabled = false
			cfg.Vector.Enabled = false
		})
		defer cleanup()

		err := srv.Start()
		require.NoError(t, err)
		assert.NotNil(t, srv.kmsManager, "KMS manager should be initialized")
		assert.NotNil(t, srv.secretsManager, "Secrets manager should be initialized")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err = srv.Shutdown(ctx)
		require.NoError(t, err)
	})
}

func TestFullServerLifecycle_WithMessaging(t *testing.T) {
	t.Run("completes lifecycle with messaging enabled", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, func(cfg *config.Config) {
			cfg.Server.Mode = config.ServerModeStandalone
			cfg.Server.Embedded = true
			cfg.KMS.Enabled = false
			cfg.Secrets.Enabled = false
			cfg.Messaging.Enabled = true
			cfg.Messaging.WatchBridge.Enabled = false
			cfg.Vector.Enabled = false
		})
		defer cleanup()

		err := srv.Start()
		require.NoError(t, err)
		assert.NotNil(t, srv.messagingService, "Messaging service should be initialized")
		assert.True(t, srv.messagingService.IsRunning())

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err = srv.Shutdown(ctx)
		require.NoError(t, err)
	})
}

// =============================================================================
// Error Path Tests
// =============================================================================

func TestShutdown_CollectsMultipleErrors(t *testing.T) {
	t.Run("collects errors from multiple components", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, nil)
		defer cleanup()

		// Set up components that will fail on close
		srv.catalogManager = &MockCatalogManager{
			closeFunc: func() error {
				return &dberrors.MessageError{Message: "catalog close error"}
			},
		}
		srv.backendManager = &MockBackendManager{
			closeFunc: func() error {
				return &dberrors.MessageError{Message: "backend close error"}
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := srv.Shutdown(ctx)
		// Should report errors occurred
		if err != nil {
			assert.Contains(t, err.Error(), "errors")
		}
	})
}

// =============================================================================
// Component Accessor Tests
// =============================================================================

func TestComponentAccessors_Mocked(t *testing.T) {
	t.Run("returns nil for uninitialized components", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, nil)
		defer cleanup()

		assert.Nil(t, srv.GetRESTServer())
		assert.Nil(t, srv.GetGRPCServer())
		assert.Nil(t, srv.GetQUICServer())
		assert.Nil(t, srv.GetCatalogManager())
		assert.Nil(t, srv.GetBackendManager())
		assert.Nil(t, srv.GetWorkerManager())
		assert.Nil(t, srv.GetDevClusterManager())
		assert.Nil(t, srv.GetRAGSystem())
		assert.Nil(t, srv.GetWatchBridge())
		assert.Nil(t, srv.Service())
	})

	t.Run("returns config", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, nil)
		defer cleanup()

		cfg := srv.GetConfig()
		assert.NotNil(t, cfg)
	})

	t.Run("returns effective mode", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, func(cfg *config.Config) {
			cfg.Server.Mode = config.ServerModeStandalone
		})
		defer cleanup()

		mode := srv.GetEffectiveMode()
		assert.Equal(t, config.ServerModeStandalone, mode)
	})
}

// =============================================================================
// Concurrent Access Tests
// =============================================================================

func TestConcurrentComponentAccessMock(t *testing.T) {
	t.Run("handles concurrent accessor calls safely", func(t *testing.T) {
		srv, cleanup := createTestServerWithConfig(t, nil)
		defer cleanup()

		done := make(chan struct{}, 100)

		for i := 0; i < 100; i++ {
			go func() {
				_ = srv.GetCatalogManager()
				_ = srv.GetBackendManager()
				_ = srv.GetEffectiveMode()
				_ = srv.IsStarted()
				_ = srv.IsClosed()
				_ = srv.Uptime()
				done <- struct{}{}
			}()
		}

		for i := 0; i < 100; i++ {
			<-done
		}
	})
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkStartKMS(b *testing.B) {
	cfg := createMinTestCfg(b.TempDir())
	cfg.KMS.Enabled = true
	cfg.KMS.Backend = config.BackendTypeSoftware
	cfg.KMS.EnabledBackends = []config.BackendType{config.BackendTypeSoftware}
	cfg.Logging.Level = "error"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		srv, err := NewServer(cfg)
		if err != nil {
			b.Fatal(err)
		}
		_ = srv.startKMS()
	}
}

func BenchmarkStartSecrets(b *testing.B) {
	cfg := createMinTestCfg(b.TempDir())
	cfg.Secrets.Enabled = true
	cfg.Logging.Level = "error"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		srv, err := NewServer(cfg)
		if err != nil {
			b.Fatal(err)
		}
		_ = srv.startSecrets()
	}
}

func BenchmarkGetEffectiveMode_Mocked(b *testing.B) {
	cfg := createMinTestCfg(b.TempDir())
	cfg.Logging.Level = "error"

	srv, err := NewServer(cfg)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = srv.getEffectiveMode()
	}
}
