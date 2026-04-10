//go:build raft && rest

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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// TestCheckGRPCWithInitialized tests gRPC health with initialized server.
func TestCheckGRPCWithInitialized(t *testing.T) {
	t.Run("GRPCInitialized", func(t *testing.T) {
		// Use withTempServer and manually set gRPC components to test health check
		s := withTempServer(t)

		// Manually set gRPC server and listener to simulate initialized state
		// Using dummy listener from test_helpers.go
		s.grpcServer = &ggrpc.Server{}
		s.grpcListener = &dummyListener{}

		health := s.GetHealthChecker()
		result := health.CheckGRPC()

		assert.Equal(t, "grpc", result.Name)
		assert.Equal(t, HealthStatusHealthy, result.Status)
		assert.Contains(t, result.Message, "listening")
		assert.NotZero(t, result.LastCheck)
	})

	t.Run("GRPCServerNil", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		health := srv.GetHealthChecker()
		result := health.CheckGRPC()

		assert.Equal(t, "grpc", result.Name)
		assert.Equal(t, HealthStatusUnhealthy, result.Status)
		assert.Contains(t, result.Message, "not initialized")
	})
}

// TestCheckRESTWithInitialized tests REST health with initialized server.
func TestCheckRESTWithInitialized(t *testing.T) {
	t.Run("RESTInitialized", func(t *testing.T) {
		// Use withTempServer and manually create REST server to test health check
		s := withTempServer(t)

		// Create a real REST server with mock service
		mockService := testutil.NewMockQuicBaaS()
		restCfg := rest.DefaultConfig()
		restCfg.Address = "127.0.0.1:0" // Use dynamic port

		restSrv, err := rest.NewServer(mockService, restCfg, s.logger)
		require.NoError(t, err)

		// Start the REST server
		err = restSrv.Start(restCfg.Address)
		require.NoError(t, err)
		defer restSrv.Close()

		// Set it on the server
		s.restServer = restSrv

		health := s.GetHealthChecker()
		result := health.CheckREST()

		assert.Equal(t, "rest", result.Name)
		assert.Equal(t, HealthStatusHealthy, result.Status)
		assert.Contains(t, result.Message, "operational")
		assert.NotZero(t, result.LastCheck)
	})

	t.Run("RESTNotInitialized", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		health := srv.GetHealthChecker()
		result := health.CheckREST()

		assert.Equal(t, "rest", result.Name)
		assert.Equal(t, HealthStatusUnhealthy, result.Status)
		assert.Contains(t, result.Message, "not initialized")
	})

	t.Run("RESTCreatedButNotStarted", func(t *testing.T) {
		// Covers health.go CheckREST: restServer != nil but !IsStarted()
		s := withTempServer(t)

		mockService := testutil.NewMockQuicBaaS()
		restCfg := rest.DefaultConfig()
		restCfg.Address = "127.0.0.1:0"

		restSrv, err := rest.NewServer(mockService, restCfg, s.logger)
		require.NoError(t, err)
		// Intentionally do NOT call restSrv.Start() to exercise the not-started path

		s.restServer = restSrv

		health := s.GetHealthChecker()
		result := health.CheckREST()

		assert.Equal(t, "rest", result.Name)
		assert.Equal(t, HealthStatusUnhealthy, result.Status)
		assert.Equal(t, "REST server not started", result.Message)
		assert.NotZero(t, result.LastCheck)
	})

	t.Run("RESTStartedThenClosed", func(t *testing.T) {
		// Covers health.go CheckREST: restServer.IsStarted() && restServer.IsClosed()
		s := withTempServer(t)

		mockService := testutil.NewMockQuicBaaS()
		restCfg := rest.DefaultConfig()
		restCfg.Address = "127.0.0.1:0"

		restSrv, err := rest.NewServer(mockService, restCfg, s.logger)
		require.NoError(t, err)

		err = restSrv.Start(restCfg.Address)
		require.NoError(t, err)

		// Close the server to exercise the IsClosed() path
		err = restSrv.Close()
		require.NoError(t, err)

		s.restServer = restSrv

		health := s.GetHealthChecker()
		result := health.CheckREST()

		assert.Equal(t, "rest", result.Name)
		assert.Equal(t, HealthStatusUnhealthy, result.Status)
		assert.Equal(t, "REST server is closed", result.Message)
		assert.NotZero(t, result.LastCheck)
	})
}

// TestIsReadyWithComponents tests readiness checks.
func TestIsReadyWithComponents(t *testing.T) {
	t.Run("NotReadyNoComponents", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		health := srv.GetHealthChecker()
		assert.False(t, health.IsReady())
	})

	t.Run("ReadyWithCriticalComponents", func(t *testing.T) {
		// Test readiness when critical components (REST, gRPC, Raft) are initialized
		s := withTempServer(t)

		// Mock gRPC server
		s.grpcServer = &ggrpc.Server{}
		s.grpcListener = &dummyListener{}

		// Mock REST server
		mockService := testutil.NewMockQuicBaaS()
		restCfg := rest.DefaultConfig()
		restCfg.Address = "127.0.0.1:0"
		restSrv, err := rest.NewServer(mockService, restCfg, s.logger)
		require.NoError(t, err)
		err = restSrv.Start(restCfg.Address)
		require.NoError(t, err)
		defer restSrv.Close()
		s.restServer = restSrv

		// Note: Cannot mock s.raftNode (concrete type), so Raft will be unhealthy
		// But IsReady checks if components are initialized, not if they're healthy

		health := s.GetHealthChecker()

		// IsReady checks REST and gRPC health only (not Raft).
		// With both components initialized and healthy, IsReady returns true.
		isReady := health.IsReady()
		assert.True(t, isReady, "Server should be ready with REST and gRPC initialized")
	})
}

// TestIsHealthyWithComponents tests health checks.
func TestIsHealthyWithComponents(t *testing.T) {
	t.Run("NotHealthyNoComponents", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		health := srv.GetHealthChecker()
		assert.False(t, health.IsHealthy())
	})
}

// TestOverallHealthTimestampAndUptime tests overall health metadata.
func TestOverallHealthTimestampAndUptime(t *testing.T) {
	cfg := createTestConfig(t)
	defer cleanupTestData(t, cfg)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Wait a bit to ensure uptime is measurable
	time.Sleep(50 * time.Millisecond)

	health := srv.GetHealthChecker()

	before := time.Now()
	overall := health.GetOverallHealth()
	after := time.Now()

	require.NotNil(t, overall)

	// Check timestamp
	assert.True(t, overall.Timestamp.After(before) || overall.Timestamp.Equal(before))
	assert.True(t, overall.Timestamp.Before(after) || overall.Timestamp.Equal(after))

	// Check uptime
	assert.True(t, overall.Uptime >= 50*time.Millisecond)
	assert.True(t, overall.Uptime < 1*time.Second)
}
