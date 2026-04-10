//go:build raft && rest && grpc && quic && mcp

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"context"
	"net"
	"testing"
	"time"

	ggrpc "google.golang.org/grpc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestShutdownLifecycle_CleanServer(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	err := srv.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestShutdownLifecycle_IdempotentShutdown(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	ctx := context.Background()

	require.NoError(t, srv.Shutdown(ctx))
	require.NoError(t, srv.Shutdown(ctx))
}

func TestShutdownLifecycle_AlreadyClosedFlag(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.mu.Lock()
	srv.closed = true
	srv.mu.Unlock()

	err := srv.Shutdown(context.Background())
	require.NoError(t, err, "should be a no-op when already closed")
}

func TestShutdownLifecycle_WithGRPCResources(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)

	grpcServer := ggrpc.NewServer()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv.grpcServer = grpcServer
	srv.grpcListener = lis

	go func() {
		grpcServer.Serve(lis) //nolint:errcheck
	}()
	time.Sleep(10 * time.Millisecond)

	require.NoError(t, srv.Shutdown(context.Background()))
}

func TestInitializeLifecycle_AlreadyStartedGuard(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.mu.Lock()
	srv.started = true
	srv.mu.Unlock()

	err := srv.initialize()
	require.Error(t, err)
	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Contains(t, dragonErr.Error(), "already started")
}

func TestInitializeLifecycle_AlreadyClosedGuard(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.mu.Lock()
	srv.closed = true
	srv.mu.Unlock()

	err := srv.initialize()
	require.Error(t, err)
	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Contains(t, dragonErr.Error(), "closed")
}

func TestInitializeLifecycle_InvalidServerMode(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = "bogus-mode"

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.initialize()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown server mode")
}

func TestStopIndexesLifecycle_NilManagerNoOp(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	require.NoError(t, srv.stopIndexes(), "should be no-op when indexManager is nil")
}

func TestStopGRPCServerLifecycle_NilServerNoOp(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.stopGRPCServer()
}

func TestStopGRPCServerLifecycle_WithUnixSocketCleanup(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)

	grpcServer := ggrpc.NewServer()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv.grpcServer = grpcServer
	srv.grpcListener = lis
	srv.grpcUnixListener = &dummyListener{}

	go func() {
		grpcServer.Serve(lis) //nolint:errcheck
	}()
	time.Sleep(10 * time.Millisecond)

	srv.stopGRPCServer()
}

func TestStopCatalogManagerLifecycle_NilNoOp(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	require.NoError(t, srv.stopCatalogManager())
}

func TestStopBackendManagerLifecycle_NilNoOp(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	require.NoError(t, srv.stopBackendManager())
}

func TestStopWorkerManagerLifecycle_NilNoOp(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	require.NoError(t, srv.stopWorkerManager())
}

func TestStartGRPCServerLifecycle_EmbeddedModeSkips(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Embedded = true

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	require.NoError(t, srv.startGRPCServer())
	assert.Nil(t, srv.grpcServer)
}

func TestStartKMSLifecycle_DisabledSkips(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.config.KMS.Enabled = false
	require.NoError(t, srv.startKMS())
}

func TestStartSecretsLifecycle_DisabledSkips(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.config.Secrets.Enabled = false
	require.NoError(t, srv.startSecrets())
}

func TestStartSecretsLifecycle_EnabledCreatesManager(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.config.Secrets.Enabled = true

	require.NoError(t, srv.startSecrets())
	assert.NotNil(t, srv.secretsManager)
}

func TestStartCatalogManagerLifecycle_NoBackendsSkips(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.config.ObjectStorage.Backends = nil
	require.NoError(t, srv.startCatalogManager())
}

func TestEnsureTLSCertificatesLifecycle_DisabledSkips(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.config.Security.TLSEnabled = false
	require.NoError(t, srv.initializeCA())
}

func TestEnsureTLSCertificatesLifecycle_EnabledGeneratesCerts(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Security.TLSEnabled = true
	cfg.Server.Host = "127.0.0.1"

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	require.NoError(t, srv.initializeCA())
	assert.NotEmpty(t, srv.config.Security.TLSCertFile)
	assert.NotEmpty(t, srv.config.Security.TLSKeyFile)
}

func TestStartServiceLifecycle_MemoryBackend(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	require.NoError(t, srv.startService())
	assert.NotNil(t, srv.quicBaaSService)
}

func TestStartServiceLifecycle_InvalidMode(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = "invalid-mode"

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startService()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown server mode")
}

func TestStartServiceLifecycle_WithCacheEnabled(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Cache.Enabled = true
	cfg.Cache.MaxSize = 500
	cfg.Cache.DefaultTTL = 10 * time.Second
	cfg.Cache.CleanupInterval = 5 * time.Second

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	require.NoError(t, srv.startService())
	assert.NotNil(t, srv.cacheManager)
}

func TestStartServiceLifecycle_WithSchedulerConfig(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Scheduler.Enabled = true
	cfg.Scheduler.TickInterval = 1 * time.Second
	cfg.Scheduler.MaxConcurrentJobs = 5

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	require.NoError(t, srv.startService())
	assert.NotNil(t, srv.quicBaaSService)
}
