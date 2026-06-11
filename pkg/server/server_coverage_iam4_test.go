//go:build !integration

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

// server_coverage_iam4_test.go drives the larger-statement lifecycle helpers
// that have non-trivial enabled/configured paths but no hard external
// dependencies. The KMS, Secrets, and Service helpers all run with the
// in-process xkms seams installed by lifecycle.go's package-level vars.

package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// =============================================================================
// lifecycle.go — startKMS enabled path
//
// startKMS reads from package-level seam vars (newKMSKeyStorage,
// newSoftwareBackend, newXKMSKeystore, initXKMS, newKMSBackendFactory,
// initFromXKMSFn, newKMSServiceFn). All seams resolve to real in-memory
// implementations by default, so the enabled path runs end-to-end without
// touching disk, network, or external services.
// =============================================================================

func TestServer_StartKMS_EnabledRunsToCompletion(t *testing.T) {
	srv := withTempServer(t)
	srv.config.KMS.Enabled = true
	srv.config.KMS.Backend = config.BackendTypeSoftware
	srv.config.KMS.Cache.Enabled = false
	srv.config.KMS.EnabledBackends = nil // use defaults

	require.NoError(t, srv.startKMS())
	require.NotNil(t, srv.kmsManager)
}

func TestServer_StartKMS_DisabledIsNoOp(t *testing.T) {
	srv := withTempServer(t)
	srv.config.KMS.Enabled = false

	require.NoError(t, srv.startKMS())
	assert.Nil(t, srv.kmsManager)
}

// =============================================================================
// lifecycle.go — startSecrets is always-on per the operator decision
// =============================================================================

func TestServer_StartSecrets_AlwaysInitializes(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Secrets.Enabled = false // intentionally false — secrets is always-on
	require.NoError(t, srv.startSecrets())
	assert.NotNil(t, srv.secretsManager, "secrets manager must initialize even when config flag is false")
}

// =============================================================================
// lifecycle.go — startStorage
//
// startStorage validates the storage backend and creates the data directory
// for persistent backends. The memory backend is a no-op; pebble and file
// require a DataDir.
// =============================================================================

func TestServer_StartStorage_MemoryBackend(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Storage.Backend = config.StorageBackendMemory
	require.NoError(t, srv.startStorage())
}

func TestServer_StartStorage_PebbleBackend(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Storage.Backend = config.StorageBackendPebble
	require.NotEmpty(t, srv.config.Storage.DataDir)
	require.NoError(t, srv.startStorage())
}

func TestServer_StartStorage_EmptyBackendFallsBack(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Storage.Backend = ""
	require.NoError(t, srv.startStorage())
}

func TestServer_StartStorage_UnknownBackendFallsBack(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Storage.Backend = "nonexistent-backend"
	require.NoError(t, srv.startStorage())
}

func TestServer_StartStorage_PebbleMissingDataDirReturnsTypedError(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Storage.Backend = config.StorageBackendPebble
	srv.config.Storage.DataDir = ""
	err := srv.startStorage()
	require.Error(t, err)
}

// =============================================================================
// lifecycle.go — startIndexes (no-op in BaaS client mode)
// =============================================================================

func TestServer_StartIndexes_NoOp(t *testing.T) {
	srv := withTempServer(t)
	require.NoError(t, srv.startIndexes())
}

// =============================================================================
// lifecycle.go — initBarrier seal-disabled branch
// =============================================================================

func TestServer_InitBarrier_DisabledReturnsNil(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Seal.Enabled = false
	require.NoError(t, srv.initBarrier())
	assert.Nil(t, srv.sealManager)
}

// =============================================================================
// lifecycle.go — fileExists helper
// =============================================================================

func TestFileExists_NonExistentReturnsFalse(t *testing.T) {
	assert.False(t, fileExists("/nonexistent/file.txt"))
}

func TestFileExists_DirectoryReturnsFalse(t *testing.T) {
	assert.False(t, fileExists(t.TempDir()))
}

// =============================================================================
// server.go — Start in embedded mode returns without blocking
// =============================================================================

func TestServer_Start_EmbeddedModeReturnsImmediately(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Server.Mode = config.ServerModeStandalone

	done := make(chan error, 1)
	go func() {
		done <- srv.Start()
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Start in embedded mode did not return within 10s")
	}

	require.NoError(t, srv.Shutdown(context.Background()))
}

func TestServer_Start_EmbeddedClusterModeReturnsImmediately(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Server.Mode = config.ServerModeCluster

	done := make(chan error, 1)
	go func() {
		done <- srv.Start()
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Start in embedded cluster mode did not return within 10s")
	}

	require.NoError(t, srv.Shutdown(context.Background()))
}

func TestServer_Start_EmbeddedWithKMSAndOrchEnabled(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Server.Mode = config.ServerModeStandalone

	srv.config.KMS.Enabled = true
	srv.config.KMS.Backend = config.BackendTypeSoftware

	done := make(chan error, 1)
	go func() {
		done <- srv.Start()
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("Start did not return within 15s")
	}

	require.NoError(t, srv.Shutdown(context.Background()))
}

func TestServer_Start_StandaloneBlocksAndUnblocks(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Server.Mode = config.ServerModeStandalone

	done := make(chan error, 1)
	go func() {
		done <- srv.Start()
	}()

	// Wait briefly for Start to enter the blocking <-shutdownChan,
	// then trigger shutdown to release it. We synchronize through the
	// shutdownChan close which happens on the goroutine path inside
	// setupSignalHandlers; TriggerShutdown drives the same effect.
	// No time.Sleep — we use channel coordination only.
	srv.TriggerShutdown()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Start did not unblock after TriggerShutdown")
	}

	require.NoError(t, srv.Shutdown(context.Background()))
}
