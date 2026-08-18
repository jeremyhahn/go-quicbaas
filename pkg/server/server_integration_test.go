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

	ggrpc "google.golang.org/grpc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// TestServerStart tests the Start method error conditions
func TestServerStart(t *testing.T) {
	t.Run("DoubleStart", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Manually set started to true to simulate already started
		srv.mu.Lock()
		srv.started = true
		srv.mu.Unlock()

		err = srv.Start()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already started")
	})

	t.Run("StartAfterClose", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Manually set closed to true
		srv.mu.Lock()
		srv.closed = true
		srv.mu.Unlock()

		err = srv.Start()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server is closed")
	})
}

// TestServerShutdownWithComponents tests shutdown with actual components
func TestServerShutdownWithComponents(t *testing.T) {
	t.Run("ShutdownWithGRPC", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Manually set components using seams (don't actually start them)
		srv.grpcServer = ggrpc.NewServer()
		srv.grpcListener = &dummyListener{}

		// Override seams for shutdown to succeed quickly
		origGraceful := grpcGracefulStop
		origTimeout := grpcStopTimeout
		grpcGracefulStop = func(s *ggrpc.Server) {} // No-op
		grpcStopTimeout = 1 * time.Millisecond
		defer func() {
			grpcGracefulStop = origGraceful
			grpcStopTimeout = origTimeout
		}()

		// Test shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = srv.Shutdown(ctx)
		assert.NoError(t, err)
		assert.True(t, srv.IsClosed())
	})

	t.Run("ShutdownWithREST", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Create a minimal REST server using mock service
		restCfg := restapi.DefaultConfig()
		restSrv, err := restapi.NewServer(testutil.NewMockQuicBaaS(), restCfg, srv.logger)
		require.NoError(t, err)
		srv.restServer = restSrv

		// Override seam for REST shutdown
		origRest := restShutdownFn
		restShutdownFn = func(s *restapi.Server, ctx context.Context) error { return nil }
		defer func() { restShutdownFn = origRest }()

		// Test shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = srv.Shutdown(ctx)
		assert.NoError(t, err)
		assert.True(t, srv.IsClosed())
	})
}

// TestTriggerShutdownMultiple tests TriggerShutdown idempotency
func TestTriggerShutdownMultiple(t *testing.T) {
	cfg := createTestConfig(t)
	defer cleanupTestData(t, cfg)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// First trigger
	srv.TriggerShutdown()

	// Verify channel is closed
	awaitSignal(t, srv.shutdownChan, "TriggerShutdown closing the shutdown channel")

	// Second trigger should not panic
	srv.TriggerShutdown()
}

// TestServerStateCryptoions tests state transitions
func TestServerStateCryptoions(t *testing.T) {
	t.Run("InitialState", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		assert.False(t, srv.IsStarted())
		assert.False(t, srv.IsClosed())
	})

	t.Run("AfterShutdown", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = srv.Shutdown(ctx)
		assert.NoError(t, err)

		assert.False(t, srv.IsStarted())
		assert.True(t, srv.IsClosed())
	})
}

// TestServerUptime tests uptime calculation
func TestServerUptime(t *testing.T) {
	cfg := createTestConfig(t)
	defer cleanupTestData(t, cfg)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	uptime := srv.Uptime()
	assert.True(t, uptime >= 100*time.Millisecond)
	assert.True(t, uptime < 1*time.Second)
}

// TestValidateFIPSMode tests FIPS validation
func TestValidateFIPSMode(t *testing.T) {
	t.Run("FIPSModeEnabled", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		cfg.Security.FIPSMode = true

		srv, err := NewServer(cfg)
		// Should not error even if FIPS is not available (dev builds)
		assert.NoError(t, err)
		assert.NotNil(t, srv)
	})

	t.Run("FIPSModeDisabled", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		cfg.Security.FIPSMode = false

		srv, err := NewServer(cfg)
		assert.NoError(t, err)
		assert.NotNil(t, srv)
	})
}

// TestWaitForConditionHelper tests the testutil WaitForCondition helper
func TestWaitForConditionHelper(t *testing.T) {
	t.Run("ConditionBecomesTrue", func(t *testing.T) {
		counter := 0
		condition := func() bool {
			counter++
			return counter > 3
		}

		err := testutil.WaitForCondition(1*time.Second, 50*time.Millisecond, condition)
		assert.NoError(t, err)
		assert.Greater(t, counter, 3)
	})

	t.Run("ConditionTimeout", func(t *testing.T) {
		condition := func() bool {
			return false
		}

		err := testutil.WaitForCondition(200*time.Millisecond, 50*time.Millisecond, condition)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "condition not met")
	})
}
