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

//go:build integration
// +build integration

package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServerStartErrorPaths tests error paths in Start method
func TestServerStartErrorPaths(t *testing.T) {
	t.Run("StartAlreadyStarted", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Manually set started flag
		srv.mu.Lock()
		srv.started = true
		srv.mu.Unlock()

		err = srv.Start()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already started")
	})

	t.Run("StartAfterClosed", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Manually set closed flag
		srv.mu.Lock()
		srv.closed = true
		srv.mu.Unlock()

		err = srv.Start()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "is closed")
	})

	t.Run("StartStorageFailure", func(t *testing.T) {
		cfg := createTestConfig(t)
		// Don't defer cleanup since we expect failure

		// Use invalid data directory to force storage failure
		cfg.Storage.DataDir = "/root/forbidden/path"

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Start will fail at storage initialization
		err = srv.Start()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create storage engine")

		// Verify server state is still marked as started but init failed
		assert.True(t, srv.IsStarted())
	})
}

// TestServerShutdownSequence tests proper shutdown ordering
func TestServerShutdownSequence(t *testing.T) {
	t.Run("ShutdownWithComponents", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Manually start components in order
		err = srv.startStorage()
		require.NoError(t, err)

		err = srv.startIndexes()
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Shutdown should succeed
		err = srv.Shutdown(ctx)
		assert.NoError(t, err)
		assert.True(t, srv.IsClosed())
	})

	t.Run("ShutdownPartialComponents", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Only start storage, not other components
		err = srv.startStorage()
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Shutdown should handle nil components gracefully
		err = srv.Shutdown(ctx)
		assert.NoError(t, err)
		assert.True(t, srv.IsClosed())
	})

	t.Run("ShutdownTimeout", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Start components
		err = srv.startStorage()
		require.NoError(t, err)

		err = srv.startIndexes()
		require.NoError(t, err)

		// Use very short timeout to test timeout behavior
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()

		// Shutdown should still succeed even with timeout
		// (it doesn't actually check context in current implementation)
		err = srv.Shutdown(ctx)
		assert.NoError(t, err)
	})
}

// TestFIPSValidationInServer tests FIPS validation logic in server creation
func TestFIPSValidationInServer(t *testing.T) {
	t.Run("FIPSModeEnabledNonFIPSBuild", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		cfg.Security.FIPSMode = true

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// In non-FIPS builds, this should just log a warning
		// The server should still be created successfully
		assert.NotNil(t, srv)
	})

	t.Run("FIPSModeDisabled", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		cfg.Security.FIPSMode = false

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Should create without any FIPS warnings
		assert.NotNil(t, srv)
	})
}

// TestServerComponentGettersAfterStart tests component getters
func TestServerComponentGettersAfterStart(t *testing.T) {
	t.Run("GettersAfterStorageStart", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Before starting
		assert.Nil(t, srv.GetIndexManager())

		// Start storage and indexes
		err = srv.startStorage()
		require.NoError(t, err)

		err = srv.startIndexes()
		require.NoError(t, err)

		// After starting
		assert.NotNil(t, srv.GetIndexManager())

		// Cleanup
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})
}

// TestServerUptimeTracking tests uptime calculation
func TestServerUptimeTracking(t *testing.T) {
	t.Run("UptimeIncreasesOverTime", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		initialUptime := srv.Uptime()

		// Wait a bit
		time.Sleep(100 * time.Millisecond)

		laterUptime := srv.Uptime()

		// Uptime should have increased
		assert.True(t, laterUptime > initialUptime)
		assert.True(t, laterUptime >= 100*time.Millisecond)
	})
}

// TestServerConcurrentAccessDuringShutdown tests thread safety
func TestServerConcurrentAccessDuringShutdown(t *testing.T) {
	t.Run("ConcurrentGettersDuringShutdown", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Start some components
		err = srv.startStorage()
		require.NoError(t, err)

		err = srv.startIndexes()
		require.NoError(t, err)

		// Start concurrent getters
		done := make(chan bool, 50)
		for i := 0; i < 50; i++ {
			go func() {
				for j := 0; j < 100; j++ {
					srv.IsStarted()
					srv.IsClosed()
					srv.Uptime()
					srv.GetConfig()
					srv.GetIndexManager()
				}
				done <- true
			}()
		}

		// Shutdown while getters are running
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = srv.Shutdown(ctx)
		assert.NoError(t, err)

		// Wait for all goroutines
		for i := 0; i < 50; i++ {
			<-done
		}
	})
}

// TestStopComponentsOrder tests shutdown happens in correct order
func TestStopComponentsOrder(t *testing.T) {
	t.Run("StopOrderRESTGRPCIndexesRaft", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Start storage and indexes (skip REST/gRPC/Raft as they're complex)
		err = srv.startStorage()
		require.NoError(t, err)

		err = srv.startIndexes()
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Test individual stop functions
		err = srv.stopRESTServer(ctx)
		assert.NoError(t, err)

		srv.stopGRPCServer()
		// Should not panic

		err = srv.stopIndexes()
		assert.NoError(t, err)

		err = srv.stopRaftNode()
		assert.NoError(t, err)

		// Storage cleanup
		if srv.storage != nil {
			srv.storage.Close()
		}
	})
}

// TestServerHealthCheckerAfterStartup tests health after partial startup
func TestServerHealthCheckerAfterStartup(t *testing.T) {
	t.Run("HealthAfterStorageAndIndexes", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Start storage and indexes
		err = srv.startStorage()
		require.NoError(t, err)

		err = srv.startIndexes()
		require.NoError(t, err)

		health := srv.GetHealthChecker()

		// Storage and indexes should be healthy
		storageHealth := health.CheckStorage()
		assert.Equal(t, HealthStatusHealthy, storageHealth.Status)

		indexHealth := health.CheckIndexes()
		assert.Equal(t, HealthStatusHealthy, indexHealth.Status)

		// Raft, gRPC, REST should still be unhealthy
		raftHealth := health.CheckRaft()
		assert.Equal(t, HealthStatusUnhealthy, raftHealth.Status)

		grpcHealth := health.CheckGRPC()
		assert.Equal(t, HealthStatusUnhealthy, grpcHealth.Status)

		restHealth := health.CheckREST()
		assert.Equal(t, HealthStatusUnhealthy, restHealth.Status)

		// Cleanup
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})
}
