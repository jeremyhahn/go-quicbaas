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

package devcluster

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// createTestBaseConfig creates a test configuration with devcluster mode.
// Uses pebble backend - mode determines Raft usage, not backend.
func createTestBaseConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Mode: config.ServerModeDevCluster,
			Host: "localhost",
			GRPC: config.ServerGRPCConfig{
				Enabled: true,
				Port:    9090,
			},
			REST: config.ServerRESTConfig{
				Enabled: true,
				Port:    8080,
			},
			MCP: config.ServerMCPConfig{
				Enabled: true,
				Port:    11443,
			},
			Unix: config.ServerUnixConfig{
				Enabled: true,
				Socket:  "/tmp/quicbaas.sock",
			},
		},
		Raft: config.RaftConfig{
			NodeID:      1,
			RaftAddress: "localhost:63001",
		},
		Storage: config.StorageConfig{
			// Use pebble backend - mode determines Raft usage, not backend
			Backend: config.StorageBackendPebble,
			DataDir: "./test-data",
			WALDir:  "./test-data/wal",
		},
		Logging: config.LoggingConfig{
			Level:  "debug",
			Format: "json",
			Output: "stdout",
		},
	}
}

func createTestLogger(t *testing.T) *logger.Logger {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)
	return log
}

func TestNewManager(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		baseConfig := createTestBaseConfig()
		log := createTestLogger(t)

		mgr, err := NewManager(&ManagerConfig{
			DevCluster: config.DefaultDevClusterConfig(),
			BaseConfig: baseConfig,
			Logger:     log,
		})

		require.NoError(t, err)
		require.NotNil(t, mgr)
		assert.Equal(t, 3, mgr.cfg.NodeCount)
		assert.False(t, mgr.IsStarted())
		assert.False(t, mgr.IsStopped())
	})

	t.Run("DefaultDevClusterConfig", func(t *testing.T) {
		baseConfig := createTestBaseConfig()
		log := createTestLogger(t)

		mgr, err := NewManager(&ManagerConfig{
			DevCluster: nil, // Should use default
			BaseConfig: baseConfig,
			Logger:     log,
		})

		require.NoError(t, err)
		require.NotNil(t, mgr)
		assert.Equal(t, 3, mgr.cfg.NodeCount)
	})

	t.Run("NilConfig", func(t *testing.T) {
		baseConfig := createTestBaseConfig()

		mgr, err := NewManager(&ManagerConfig{
			BaseConfig: baseConfig,
		})

		require.NoError(t, err)
		require.NotNil(t, mgr)
	})

	t.Run("NilBaseConfig", func(t *testing.T) {
		mgr, err := NewManager(&ManagerConfig{
			DevCluster: config.DefaultDevClusterConfig(),
			BaseConfig: nil,
		})

		require.Error(t, err)
		assert.Nil(t, mgr)
		assert.Contains(t, err.Error(), "base config is required")
	})

	t.Run("InvalidNodeCount", func(t *testing.T) {
		baseConfig := createTestBaseConfig()

		mgr, err := NewManager(&ManagerConfig{
			DevCluster: &config.DevClusterConfig{
				NodeCount: 0,
			},
			BaseConfig: baseConfig,
		})

		require.Error(t, err)
		assert.Nil(t, mgr)
		assert.Contains(t, err.Error(), "invalid node count")
	})

	t.Run("NegativeNodeCount", func(t *testing.T) {
		baseConfig := createTestBaseConfig()

		mgr, err := NewManager(&ManagerConfig{
			DevCluster: &config.DevClusterConfig{
				NodeCount: -1,
			},
			BaseConfig: baseConfig,
		})

		require.Error(t, err)
		assert.Nil(t, mgr)
		assert.Contains(t, err.Error(), "invalid node count")
	})

	t.Run("NilManagerConfig", func(t *testing.T) {
		mgr, err := NewManager(nil)

		require.Error(t, err)
		assert.Nil(t, mgr)
		assert.Contains(t, err.Error(), "base config is required")
	})
}

func TestManagerStart(t *testing.T) {
	t.Run("SuccessfulStart", func(t *testing.T) {
		baseConfig := createTestBaseConfig()
		log := createTestLogger(t)

		mgr, err := NewManager(&ManagerConfig{
			DevCluster: config.DefaultDevClusterConfig(),
			BaseConfig: baseConfig,
			Logger:     log,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = mgr.Start(ctx)

		require.NoError(t, err)
		assert.True(t, mgr.IsStarted())
		assert.Equal(t, 3, mgr.NodeCount())
	})

	t.Run("DoubleStartFails", func(t *testing.T) {
		baseConfig := createTestBaseConfig()
		log := createTestLogger(t)

		mgr, err := NewManager(&ManagerConfig{
			DevCluster: config.DefaultDevClusterConfig(),
			BaseConfig: baseConfig,
			Logger:     log,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = mgr.Start(ctx)
		require.NoError(t, err)

		// Second start should fail.
		err = mgr.Start(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already started")
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		baseConfig := createTestBaseConfig()
		log := createTestLogger(t)

		mgr, err := NewManager(&ManagerConfig{
			DevCluster: config.DefaultDevClusterConfig(),
			BaseConfig: baseConfig,
			Logger:     log,
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately.

		err = mgr.Start(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "canceled")
	})
}

func TestNodeConfigGeneration(t *testing.T) {
	t.Run("ThreeNodeCluster", func(t *testing.T) {
		baseConfig := createTestBaseConfig()
		log := createTestLogger(t)

		devClusterCfg := &config.DevClusterConfig{
			NodeCount: 3,
			DataDir:   "./dev-data",
			BasePorts: config.DevClusterPorts{
				GRPC: 9090,
				REST: 8080,
				MCP:  11443,
				Raft: 63001,
			},
			UnixSocketPattern: "/tmp/quicbaas-node-{n}.sock",
		}

		mgr, err := NewManager(&ManagerConfig{
			DevCluster: devClusterCfg,
			BaseConfig: baseConfig,
			Logger:     log,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = mgr.Start(ctx)
		require.NoError(t, err)

		nodes := mgr.Nodes()
		require.Len(t, nodes, 3)

		// Verify node 1 (bootstrap node).
		assert.Equal(t, uint64(1), nodes[0].NodeID)
		assert.Equal(t, 9090, nodes[0].GRPCPort)
		assert.Equal(t, 8080, nodes[0].RESTPort)
		assert.Equal(t, 11443, nodes[0].MCPPort)
		assert.Equal(t, 63001, nodes[0].RaftPort)
		assert.Equal(t, "./dev-data/node-1", nodes[0].DataDir)
		assert.Equal(t, "/tmp/quicbaas-node-1.sock", nodes[0].UnixSocket)
		assert.True(t, nodes[0].Config.Raft.EnableBootstrap)
		assert.Len(t, nodes[0].Config.Raft.JoinAddresses, 2)

		// Verify node 2.
		assert.Equal(t, uint64(2), nodes[1].NodeID)
		assert.Equal(t, 9091, nodes[1].GRPCPort)
		assert.Equal(t, 8081, nodes[1].RESTPort)
		assert.Equal(t, 11444, nodes[1].MCPPort)
		assert.Equal(t, 63002, nodes[1].RaftPort)
		assert.Equal(t, "./dev-data/node-2", nodes[1].DataDir)
		assert.Equal(t, "/tmp/quicbaas-node-2.sock", nodes[1].UnixSocket)
		assert.False(t, nodes[1].Config.Raft.EnableBootstrap)

		// Verify node 3.
		assert.Equal(t, uint64(3), nodes[2].NodeID)
		assert.Equal(t, 9092, nodes[2].GRPCPort)
		assert.Equal(t, 8082, nodes[2].RESTPort)
		assert.Equal(t, 11445, nodes[2].MCPPort)
		assert.Equal(t, 63003, nodes[2].RaftPort)
		assert.Equal(t, "./dev-data/node-3", nodes[2].DataDir)
		assert.Equal(t, "/tmp/quicbaas-node-3.sock", nodes[2].UnixSocket)
		assert.False(t, nodes[2].Config.Raft.EnableBootstrap)
	})

	t.Run("JoinAddresses", func(t *testing.T) {
		baseConfig := createTestBaseConfig()
		log := createTestLogger(t)

		devClusterCfg := &config.DevClusterConfig{
			NodeCount: 3,
			DataDir:   "./dev-data",
			BasePorts: config.DevClusterPorts{
				GRPC: 9090,
				REST: 8080,
				MCP:  11443,
				Raft: 63001,
			},
		}

		mgr, err := NewManager(&ManagerConfig{
			DevCluster: devClusterCfg,
			BaseConfig: baseConfig,
			Logger:     log,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = mgr.Start(ctx)
		require.NoError(t, err)

		nodes := mgr.Nodes()

		// Node 1 should have join addresses for nodes 2 and 3.
		assert.Contains(t, nodes[0].Config.Raft.JoinAddresses, "localhost:63002")
		assert.Contains(t, nodes[0].Config.Raft.JoinAddresses, "localhost:63003")

		// Node 2 should have join addresses for nodes 1 and 3.
		assert.Contains(t, nodes[1].Config.Raft.JoinAddresses, "localhost:63001")
		assert.Contains(t, nodes[1].Config.Raft.JoinAddresses, "localhost:63003")

		// Node 3 should have join addresses for nodes 1 and 2.
		assert.Contains(t, nodes[2].Config.Raft.JoinAddresses, "localhost:63001")
		assert.Contains(t, nodes[2].Config.Raft.JoinAddresses, "localhost:63002")
	})

	t.Run("SingleNodeCluster", func(t *testing.T) {
		baseConfig := createTestBaseConfig()
		log := createTestLogger(t)

		devClusterCfg := &config.DevClusterConfig{
			NodeCount: 1,
			DataDir:   "./dev-data",
			BasePorts: config.DefaultDevClusterPorts(),
		}

		mgr, err := NewManager(&ManagerConfig{
			DevCluster: devClusterCfg,
			BaseConfig: baseConfig,
			Logger:     log,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = mgr.Start(ctx)
		require.NoError(t, err)

		nodes := mgr.Nodes()
		require.Len(t, nodes, 1)

		// Single node should be bootstrap node with no join addresses.
		assert.True(t, nodes[0].Config.Raft.EnableBootstrap)
		assert.Empty(t, nodes[0].Config.Raft.JoinAddresses)
	})

	t.Run("FiveNodeCluster", func(t *testing.T) {
		baseConfig := createTestBaseConfig()
		log := createTestLogger(t)

		devClusterCfg := &config.DevClusterConfig{
			NodeCount: 5,
			DataDir:   "./dev-data",
			BasePorts: config.DefaultDevClusterPorts(),
		}

		mgr, err := NewManager(&ManagerConfig{
			DevCluster: devClusterCfg,
			BaseConfig: baseConfig,
			Logger:     log,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = mgr.Start(ctx)
		require.NoError(t, err)

		nodes := mgr.Nodes()
		require.Len(t, nodes, 5)

		// Each node should have 4 join addresses (all other nodes).
		for i, node := range nodes {
			if i == 0 {
				assert.True(t, node.Config.Raft.EnableBootstrap)
			} else {
				assert.False(t, node.Config.Raft.EnableBootstrap)
			}
			assert.Len(t, node.Config.Raft.JoinAddresses, 4)
		}
	})
}

func TestGetNodeConfig(t *testing.T) {
	baseConfig := createTestBaseConfig()
	log := createTestLogger(t)

	mgr, err := NewManager(&ManagerConfig{
		DevCluster: config.DefaultDevClusterConfig(),
		BaseConfig: baseConfig,
		Logger:     log,
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = mgr.Start(ctx)
	require.NoError(t, err)

	t.Run("ExistingNode", func(t *testing.T) {
		cfg, err := mgr.GetNodeConfig(1)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, uint64(1), cfg.Raft.NodeID)
	})

	t.Run("NonExistingNode", func(t *testing.T) {
		cfg, err := mgr.GetNodeConfig(99)
		require.Error(t, err)
		assert.Nil(t, cfg)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestGetNode(t *testing.T) {
	baseConfig := createTestBaseConfig()
	log := createTestLogger(t)

	mgr, err := NewManager(&ManagerConfig{
		DevCluster: config.DefaultDevClusterConfig(),
		BaseConfig: baseConfig,
		Logger:     log,
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = mgr.Start(ctx)
	require.NoError(t, err)

	t.Run("ExistingNode", func(t *testing.T) {
		node, err := mgr.GetNode(2)
		require.NoError(t, err)
		require.NotNil(t, node)
		assert.Equal(t, uint64(2), node.NodeID)
	})

	t.Run("NonExistingNode", func(t *testing.T) {
		node, err := mgr.GetNode(100)
		require.Error(t, err)
		assert.Nil(t, node)
	})
}

func TestManagerStop(t *testing.T) {
	t.Run("SuccessfulStop", func(t *testing.T) {
		baseConfig := createTestBaseConfig()
		log := createTestLogger(t)

		mgr, err := NewManager(&ManagerConfig{
			DevCluster: config.DefaultDevClusterConfig(),
			BaseConfig: baseConfig,
			Logger:     log,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = mgr.Start(ctx)
		require.NoError(t, err)

		err = mgr.Stop(ctx)
		require.NoError(t, err)
		assert.True(t, mgr.IsStopped())
	})

	t.Run("DoubleStopIsIdempotent", func(t *testing.T) {
		baseConfig := createTestBaseConfig()
		log := createTestLogger(t)

		mgr, err := NewManager(&ManagerConfig{
			DevCluster: config.DefaultDevClusterConfig(),
			BaseConfig: baseConfig,
			Logger:     log,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = mgr.Start(ctx)
		require.NoError(t, err)

		err = mgr.Stop(ctx)
		require.NoError(t, err)

		// Second stop should be a no-op.
		err = mgr.Stop(ctx)
		require.NoError(t, err)
	})

	t.Run("StopWithTimeout", func(t *testing.T) {
		baseConfig := createTestBaseConfig()
		log := createTestLogger(t)

		mgr, err := NewManager(&ManagerConfig{
			DevCluster: config.DefaultDevClusterConfig(),
			BaseConfig: baseConfig,
			Logger:     log,
		})
		require.NoError(t, err)

		ctx := context.Background()
		err = mgr.Start(ctx)
		require.NoError(t, err)

		// Use a timeout context.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = mgr.Stop(ctx)
		require.NoError(t, err)
	})
}

func TestNodeInstanceState(t *testing.T) {
	baseConfig := createTestBaseConfig()
	log := createTestLogger(t)

	mgr, err := NewManager(&ManagerConfig{
		DevCluster: config.DefaultDevClusterConfig(),
		BaseConfig: baseConfig,
		Logger:     log,
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = mgr.Start(ctx)
	require.NoError(t, err)

	node, err := mgr.GetNode(1)
	require.NoError(t, err)

	t.Run("InitialState", func(t *testing.T) {
		assert.False(t, node.IsRunning())
		assert.False(t, node.IsReady())
	})

	t.Run("SetRunning", func(t *testing.T) {
		node.SetRunning(true)
		assert.True(t, node.IsRunning())

		node.SetRunning(false)
		assert.False(t, node.IsRunning())
	})

	t.Run("SetReady", func(t *testing.T) {
		node.SetReady(true)
		assert.True(t, node.IsReady())

		node.SetReady(false)
		assert.False(t, node.IsReady())
	})

	t.Run("Channels", func(t *testing.T) {
		// StopCh should not be nil.
		assert.NotNil(t, node.StopCh())

		// ErrCh should not be nil.
		assert.NotNil(t, node.ErrCh())
	})

	t.Run("SendError", func(t *testing.T) {
		// Create a fresh node for this test.
		node2, err := mgr.GetNode(2)
		require.NoError(t, err)

		// Send an error.
		testErr := assert.AnError
		node2.SendError(testErr)

		// Should be able to receive the error.
		select {
		case err := <-node2.ErrCh():
			assert.Equal(t, testErr, err)
		case <-time.After(time.Second):
			t.Fatal("expected to receive error from channel")
		}
	})
}

func TestDevClusterConfig(t *testing.T) {
	baseConfig := createTestBaseConfig()
	log := createTestLogger(t)

	devClusterCfg := &config.DevClusterConfig{
		NodeCount:         5,
		DataDir:           "/custom/path",
		BasePorts:         config.DefaultDevClusterPorts(),
		UnixSocketPattern: "/var/run/quicbaas-{n}.sock",
	}

	mgr, err := NewManager(&ManagerConfig{
		DevCluster: devClusterCfg,
		BaseConfig: baseConfig,
		Logger:     log,
	})
	require.NoError(t, err)

	returnedCfg := mgr.DevClusterConfig()
	assert.Equal(t, devClusterCfg, returnedCfg)
	assert.Equal(t, 5, returnedCfg.NodeCount)
	assert.Equal(t, "/custom/path", returnedCfg.DataDir)
}
