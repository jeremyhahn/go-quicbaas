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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

func TestHealthStatus(t *testing.T) {
	t.Run("StatusConstants", func(t *testing.T) {
		assert.Equal(t, HealthStatus("healthy"), HealthStatusHealthy)
		assert.Equal(t, HealthStatus("degraded"), HealthStatusDegraded)
		assert.Equal(t, HealthStatus("unhealthy"), HealthStatusUnhealthy)
		assert.Equal(t, HealthStatus("unknown"), HealthStatusUnknown)
	})
}

func TestNewHealthChecker(t *testing.T) {
	t.Run("ValidInput", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		health := NewHealthChecker(srv, srv.logger)
		require.NotNil(t, health)
		assert.Equal(t, srv, health.server)
		assert.NotNil(t, health.logger)
	})
}

func TestComponentHealth(t *testing.T) {
	cfg := createTestConfig(t)
	defer cleanupTestData(t, cfg)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	health := srv.GetHealthChecker()

	t.Run("CheckGRPC_NotInitialized", func(t *testing.T) {
		result := health.CheckGRPC()
		assert.Equal(t, "grpc", result.Name)
		assert.Equal(t, HealthStatusUnhealthy, result.Status)
		assert.Contains(t, result.Message, "not initialized")
		assert.NotZero(t, result.LastCheck)
	})

	t.Run("CheckREST_NotInitialized", func(t *testing.T) {
		result := health.CheckREST()
		assert.Equal(t, "rest", result.Name)
		assert.Equal(t, HealthStatusUnhealthy, result.Status)
		assert.Contains(t, result.Message, "not initialized")
		assert.NotZero(t, result.LastCheck)
	})
}

func TestOverallHealthStatus(t *testing.T) {
	cfg := createTestConfig(t)
	defer cleanupTestData(t, cfg)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	health := srv.GetHealthChecker()

	t.Run("UnhealthyComponents", func(t *testing.T) {
		overall := health.GetOverallHealth()
		require.NotNil(t, overall)

		// All components should be unhealthy initially
		assert.Equal(t, HealthStatusUnhealthy, overall.Status)
	})

	t.Run("TimestampAndUptime", func(t *testing.T) {
		overall := health.GetOverallHealth()
		require.NotNil(t, overall)

		assert.NotZero(t, overall.Timestamp)
		assert.True(t, overall.Uptime >= 0)
	})
}

func TestHealthCheckerMethods(t *testing.T) {
	cfg := createTestConfig(t)
	defer cleanupTestData(t, cfg)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	health := srv.GetHealthChecker()

	t.Run("IsHealthy_BeforeStart", func(t *testing.T) {
		// Should be unhealthy before starting
		assert.False(t, health.IsHealthy())
	})

	t.Run("IsReady_BeforeStart", func(t *testing.T) {
		// Should not be ready before starting
		assert.False(t, health.IsReady())
	})
}

func TestHealthStatusDetermination(t *testing.T) {
	t.Run("AllUnhealthy", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		health := srv.GetHealthChecker()
		overall := health.GetOverallHealth()

		// With all components unhealthy, overall should be unhealthy
		assert.Equal(t, HealthStatusUnhealthy, overall.Status)
	})
}

func BenchmarkGetOverallHealth(b *testing.B) {
	cfg := config.Default()
	cfg.Logging.Level = "error"

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
