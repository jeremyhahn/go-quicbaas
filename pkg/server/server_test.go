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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

func TestNewServer(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)
		require.NotNil(t, srv)

		assert.Equal(t, cfg, srv.config)
		assert.NotNil(t, srv.logger)
		assert.NotNil(t, srv.health)
		assert.NotNil(t, srv.shutdownChan)
		assert.False(t, srv.started)
		assert.False(t, srv.closed)
	})

	t.Run("NilConfig", func(t *testing.T) {
		srv, err := NewServer(nil)
		assert.Error(t, err)
		assert.Nil(t, srv)
		assert.Contains(t, err.Error(), "config cannot be nil")
	})

	t.Run("FIPSModeDisabled", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		cfg.Security.FIPSMode = false

		srv, err := NewServer(cfg)
		require.NoError(t, err)
		require.NotNil(t, srv)
	})
}

func TestServerLifecycle(t *testing.T) {
	t.Run("StateTracking", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Initial state
		assert.False(t, srv.IsStarted())
		assert.False(t, srv.IsClosed())

		// Uptime should be minimal
		assert.True(t, srv.Uptime() < 1*time.Second)
	})

	t.Run("GetConfig", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		assert.Equal(t, cfg, srv.GetConfig())
	})

	t.Run("ComponentGetters", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Before start, components should be nil
		assert.Nil(t, srv.GetRaftNode())
		assert.Nil(t, srv.GetRESTServer())
		assert.Nil(t, srv.GetGRPCServer())
	})
}

func TestServerShutdown(t *testing.T) {
	t.Run("ShutdownBeforeStart", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Should be able to shutdown before starting
		err = srv.Shutdown(ctx)
		assert.NoError(t, err)
		assert.True(t, srv.IsClosed())
	})

	t.Run("DoubleShutdown", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// First shutdown
		err = srv.Shutdown(ctx)
		assert.NoError(t, err)
		assert.True(t, srv.IsClosed())

		// Second shutdown should be idempotent
		err = srv.Shutdown(ctx)
		assert.NoError(t, err)
	})

	t.Run("TriggerShutdown", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Trigger shutdown
		srv.TriggerShutdown()

		// Shutdown channel should be closed
		awaitSignal(t, srv.shutdownChan, "TriggerShutdown closing the shutdown channel")
	})
}

func TestHealthChecker(t *testing.T) {
	t.Run("GetHealthChecker", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		health := srv.GetHealthChecker()
		require.NotNil(t, health)
		assert.Equal(t, srv, health.server)
	})

	t.Run("HealthCheckBeforeStart", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		health := srv.GetHealthChecker()

		// Check individual components that exist on HealthChecker
		grpcHealth := health.CheckGRPC()
		assert.Equal(t, HealthStatusUnhealthy, grpcHealth.Status)

		restHealth := health.CheckREST()
		assert.Equal(t, HealthStatusUnhealthy, restHealth.Status)
	})

	t.Run("OverallHealth", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		health := srv.GetHealthChecker()
		overall := health.GetOverallHealth()

		require.NotNil(t, overall)
		assert.Equal(t, HealthStatusUnhealthy, overall.Status)
		assert.NotEmpty(t, overall.Components)
		assert.NotZero(t, overall.Timestamp)
		assert.True(t, overall.Uptime >= 0)

		// Should have grpc and rest components
		assert.Contains(t, overall.Components, "grpc")
		assert.Contains(t, overall.Components, "rest")
	})

	t.Run("IsHealthy", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		health := srv.GetHealthChecker()
		assert.False(t, health.IsHealthy())
	})

	t.Run("IsReady", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		health := srv.GetHealthChecker()
		assert.False(t, health.IsReady())
	})
}

func TestFIPSValidation(t *testing.T) {
	// This is tested in the FIPS package, but we can verify it's called
	t.Run("FIPSModeWarning", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		// Enable FIPS mode
		cfg.Security.FIPSMode = true

		// Should create server without error (FIPS not enforced in dev builds)
		srv, err := NewServer(cfg)
		require.NoError(t, err)
		require.NotNil(t, srv)
	})
}

func TestServerConcurrency(t *testing.T) {
	t.Run("ConcurrentShutdown", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Start multiple goroutines trying to shutdown
		done := make(chan bool, 10)
		for i := 0; i < 10; i++ {
			go func() {
				srv.Shutdown(ctx)
				done <- true
			}()
		}

		// Wait for all goroutines
		for i := 0; i < 10; i++ {
			<-done
		}

		// Server should be closed exactly once
		assert.True(t, srv.IsClosed())
	})

	t.Run("ConcurrentTriggerShutdown", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Start multiple goroutines trying to trigger shutdown
		done := make(chan bool, 10)
		for i := 0; i < 10; i++ {
			go func() {
				srv.TriggerShutdown()
				done <- true
			}()
		}

		// Wait for all goroutines
		for i := 0; i < 10; i++ {
			<-done
		}

		// Shutdown channel should be closed
		awaitSignal(t, srv.shutdownChan, "TriggerShutdown closing the shutdown channel")
	})

	t.Run("ConcurrentStateReads", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		done := make(chan bool, 100)

		// Concurrent reads of state
		for i := 0; i < 100; i++ {
			go func() {
				srv.IsStarted()
				srv.IsClosed()
				srv.Uptime()
				srv.GetConfig()
				done <- true
			}()
		}

		// Wait for all goroutines
		for i := 0; i < 100; i++ {
			<-done
		}
	})
}

// Benchmark tests.
func BenchmarkNewServer(b *testing.B) {
	cfg := config.Default()
	cfg.Logging.Level = "error"

	tempDir, _ := os.MkdirTemp("", "quicbaas-bench-*")
	defer os.RemoveAll(tempDir)

	cfg.Storage.DataDir = filepath.Join(tempDir, "data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		srv, err := NewServer(cfg)
		if err != nil {
			b.Fatal(err)
		}
		_ = srv
	}
}

func BenchmarkHealthCheck(b *testing.B) {
	cfg := config.Default()
	cfg.Logging.Level = "error"

	tempDir, _ := os.MkdirTemp("", "quicbaas-bench-*")
	defer os.RemoveAll(tempDir)

	cfg.Storage.DataDir = filepath.Join(tempDir, "data")

	srv, err := NewServer(cfg)
	if err != nil {
		b.Fatal(err)
	}

	health := srv.GetHealthChecker()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		health.GetOverallHealth()
	}
}

// ---------------------------------------------------------------------------
// Additional server accessor and lifecycle tests
// ---------------------------------------------------------------------------

func TestServer_GetConfig(t *testing.T) {
	cfg := createTestConfig(t)
	defer cleanupTestData(t, cfg)

	srv, err := NewServer(cfg)
	assert.NoError(t, err)

	gotCfg := srv.GetConfig()
	assert.Equal(t, cfg, gotCfg)
}

func TestServer_GetRaftNode(t *testing.T) {
	cfg := createTestConfig(t)
	defer cleanupTestData(t, cfg)

	srv, err := NewServer(cfg)
	assert.NoError(t, err)

	// Before starting, should be nil
	node := srv.GetRaftNode()
	assert.Nil(t, node)
}

func TestServer_GetRESTServer(t *testing.T) {
	cfg := createTestConfig(t)
	defer cleanupTestData(t, cfg)

	srv, err := NewServer(cfg)
	assert.NoError(t, err)

	// Before starting, should be nil
	rest := srv.GetRESTServer()
	assert.Nil(t, rest)
}

func TestServer_GetGRPCServer(t *testing.T) {
	cfg := createTestConfig(t)
	defer cleanupTestData(t, cfg)

	srv, err := NewServer(cfg)
	assert.NoError(t, err)

	// Before starting, should be nil
	grpc := srv.GetGRPCServer()
	assert.Nil(t, grpc)
}

func TestServer_Uptime(t *testing.T) {
	cfg := createTestConfig(t)
	defer cleanupTestData(t, cfg)

	srv, err := NewServer(cfg)
	assert.NoError(t, err)

	uptime := srv.Uptime()
	assert.GreaterOrEqual(t, uptime.Nanoseconds(), int64(0))
}

func TestServer_IsStarted_Initial(t *testing.T) {
	cfg := createTestConfig(t)
	defer cleanupTestData(t, cfg)

	srv, err := NewServer(cfg)
	assert.NoError(t, err)

	// Should not be started initially
	assert.False(t, srv.IsStarted())
}

func TestServer_IsClosed_Initial(t *testing.T) {
	cfg := createTestConfig(t)
	defer cleanupTestData(t, cfg)

	srv, err := NewServer(cfg)
	assert.NoError(t, err)

	// Should not be closed initially
	assert.False(t, srv.IsClosed())
}

func TestServer_TriggerShutdown(t *testing.T) {
	cfg := createTestConfig(t)
	defer cleanupTestData(t, cfg)

	srv, err := NewServer(cfg)
	assert.NoError(t, err)

	// Test trigger shutdown (should not panic)
	srv.TriggerShutdown()

	// Calling again should be safe (idempotent)
	srv.TriggerShutdown()
}
