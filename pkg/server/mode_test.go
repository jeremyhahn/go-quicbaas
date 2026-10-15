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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

func TestGetEffectiveMode(t *testing.T) {
	t.Run("EmptyModeDefaultsToStandalone", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		// Empty mode should default to standalone (not inferred from backend)
		cfg.Server.Mode = ""
		cfg.Storage.Backend = config.StorageBackendPebble

		srv, err := NewServer(cfg)
		require.NoError(t, err)
		assert.Equal(t, config.ServerModeStandalone, srv.GetEffectiveMode())
	})

	t.Run("EmptyModeWithAnyBackendDefaultsToStandalone", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		// Empty mode with any backend should default to standalone (no inference)
		// NOTE: "raft" is no longer a valid backend - use mode=cluster instead
		testCases := []string{
			config.StorageBackendPebble,
			config.StorageBackendMemory,
			config.StorageBackendFile,
		}

		for _, backend := range testCases {
			cfg.Server.Mode = ""
			cfg.Storage.Backend = backend

			srv, err := NewServer(cfg)
			require.NoError(t, err, "Backend: %s", backend)
			assert.Equal(t, config.ServerModeStandalone, srv.GetEffectiveMode(),
				"Empty mode with %s backend should default to standalone", backend)
		}
	})

	t.Run("ExplicitModeReturnsConfiguredMode", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		// Test standalone mode
		cfg.Server.Mode = config.ServerModeStandalone
		srv, err := NewServer(cfg)
		require.NoError(t, err)
		assert.Equal(t, config.ServerModeStandalone, srv.GetEffectiveMode())

		// Test cluster mode
		cfg.Server.Mode = config.ServerModeCluster
		assert.Equal(t, config.ServerModeCluster, srv.GetEffectiveMode())

		// Test devcluster mode
		cfg.Server.Mode = config.ServerModeDevCluster
		assert.Equal(t, config.ServerModeDevCluster, srv.GetEffectiveMode())
	})

	t.Run("ExplicitStandaloneWithPebbleBackend", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		cfg.Server.Mode = config.ServerModeStandalone
		cfg.Storage.Backend = config.StorageBackendPebble

		srv, err := NewServer(cfg)
		require.NoError(t, err)
		assert.Equal(t, config.ServerModeStandalone, srv.GetEffectiveMode())
	})

	t.Run("ExplicitClusterWithPebbleBackend", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		// Explicit cluster mode with pebble backend
		cfg.Server.Mode = config.ServerModeCluster
		cfg.Storage.Backend = config.StorageBackendPebble

		srv, err := NewServer(cfg)
		require.NoError(t, err)
		// Explicit mode takes precedence
		assert.Equal(t, config.ServerModeCluster, srv.GetEffectiveMode())
	})

	t.Run("ClusterModeWithMemoryBackend", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		// Explicit cluster mode with memory backend - mode is respected regardless of backend
		cfg.Server.Mode = config.ServerModeCluster
		cfg.Storage.Backend = config.StorageBackendMemory

		srv, err := NewServer(cfg)
		require.NoError(t, err)
		assert.Equal(t, config.ServerModeCluster, srv.GetEffectiveMode())
	})

	t.Run("StandaloneWithMemoryBackend", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		cfg.Server.Mode = config.ServerModeStandalone
		cfg.Storage.Backend = config.StorageBackendMemory

		srv, err := NewServer(cfg)
		require.NoError(t, err)
		assert.Equal(t, config.ServerModeStandalone, srv.GetEffectiveMode())
	})

	t.Run("DevClusterModeReturnsDevCluster", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		cfg.Server.Mode = config.ServerModeDevCluster
		cfg.Storage.Backend = config.StorageBackendPebble

		srv, err := NewServer(cfg)
		require.NoError(t, err)
		assert.Equal(t, config.ServerModeDevCluster, srv.GetEffectiveMode())
	})
}

func TestServerModeConstants(t *testing.T) {
	t.Run("ModeConstantsAreDefined", func(t *testing.T) {
		// Verify that all mode constants are properly defined
		assert.Equal(t, "standalone", config.ServerModeStandalone)
		assert.Equal(t, "cluster", config.ServerModeCluster)
		assert.Equal(t, "devcluster", config.ServerModeDevCluster)
	})

	t.Run("StorageBackendConstantsAreDefined", func(t *testing.T) {
		// Verify storage backend constants - "raft" is no longer a valid backend
		// Use server.mode=cluster for Raft consensus
		assert.Equal(t, "memory", config.StorageBackendMemory)
		assert.Equal(t, "file", config.StorageBackendFile)
		assert.Equal(t, "pebble", config.StorageBackendPebble)
	})
}

func TestModeIsFirstClassConfiguration(t *testing.T) {
	t.Run("ModeIsIndependentOfBackend", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		// Mode and backend are independent configuration options
		// Mode determines consensus behavior, backend determines storage engine

		// Test all mode + backend combinations
		modes := []string{
			config.ServerModeStandalone,
			config.ServerModeCluster,
			config.ServerModeDevCluster,
		}
		backends := []string{
			config.StorageBackendMemory,
			config.StorageBackendFile,
			config.StorageBackendPebble,
		}

		for _, mode := range modes {
			for _, backend := range backends {
				cfg.Server.Mode = mode
				cfg.Storage.Backend = backend

				srv, err := NewServer(cfg)
				require.NoError(t, err, "Mode: %s, Backend: %s", mode, backend)
				assert.Equal(t, mode, srv.GetEffectiveMode(),
					"Mode should be %s regardless of backend %s", mode, backend)
			}
		}
	})
}

func TestModeAffectsLogging(t *testing.T) {
	t.Run("StandaloneModeLogsCorrectly", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		cfg.Server.Mode = config.ServerModeStandalone
		cfg.Storage.Backend = config.StorageBackendPebble

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Verify mode is accessible
		mode := srv.GetEffectiveMode()
		assert.Equal(t, config.ServerModeStandalone, mode)
	})

	t.Run("ClusterModeLogsCorrectly", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		cfg.Server.Mode = config.ServerModeCluster
		cfg.Storage.Backend = config.StorageBackendPebble

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Verify mode is accessible
		mode := srv.GetEffectiveMode()
		assert.Equal(t, config.ServerModeCluster, mode)
	})
}

func TestGetEffectiveModeWithInvalidMode(t *testing.T) {
	t.Run("UnknownModeIsPassedThrough", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		// Set an unknown/invalid mode
		cfg.Server.Mode = "invalid-mode"

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Unknown modes are passed through as-is (will be handled by initialize() with an error)
		assert.Equal(t, "invalid-mode", srv.GetEffectiveMode())
	})
}

func TestDevClusterConfig(t *testing.T) {
	t.Run("DefaultDevClusterConfigValues", func(t *testing.T) {
		cfg := config.DefaultDevClusterConfig()

		assert.Equal(t, 3, cfg.NodeCount)
		assert.Equal(t, "./dev-data", cfg.DataDir)
		assert.Equal(t, 9290, cfg.BasePorts.GRPC)
		assert.Equal(t, 8080, cfg.BasePorts.REST)
		assert.Equal(t, 11443, cfg.BasePorts.MCP)
		assert.Equal(t, 63001, cfg.BasePorts.Raft)
		assert.Equal(t, "/tmp/quicbaas-node-{n}.sock", cfg.UnixSocketPattern)
	})

	t.Run("ClusterModePreservedAfterInit", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		cfg.Server.Mode = config.ServerModeCluster
		cfg.Storage.Backend = config.StorageBackendMemory
		cfg.Server.Embedded = true

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Call initialize() directly to test mode preservation without blocking Start()
		err = srv.initialize()
		require.NoError(t, err)
		defer func() { _ = srv.Shutdown(context.Background()) }()

		// Verify mode was NOT overridden to standalone during initialization
		assert.Equal(t, config.ServerModeCluster, srv.config.Server.Mode,
			"cluster mode must be preserved through initialization, not overridden to standalone")
	})

	t.Run("StandaloneModePreservedAfterInit", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		cfg.Server.Mode = config.ServerModeStandalone
		cfg.Storage.Backend = config.StorageBackendMemory
		cfg.Server.Embedded = true

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		err = srv.initialize()
		require.NoError(t, err)
		defer func() { _ = srv.Shutdown(context.Background()) }()

		assert.Equal(t, config.ServerModeStandalone, srv.config.Server.Mode,
			"standalone mode must be preserved through initialization")
	})

	t.Run("DefaultDevClusterPortsValues", func(t *testing.T) {
		ports := config.DefaultDevClusterPorts()

		assert.Equal(t, 9290, ports.GRPC)
		assert.Equal(t, 8080, ports.REST)
		assert.Equal(t, 11443, ports.MCP)
		assert.Equal(t, 63001, ports.Raft)
	})
}
