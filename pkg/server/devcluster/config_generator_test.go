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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func createTestManager(t *testing.T, nodeCount int) *Manager {
	baseConfig := createTestBaseConfig()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	tempDir := t.TempDir()

	devClusterCfg := &config.DevClusterConfig{
		NodeCount:         nodeCount,
		DataDir:           tempDir,
		BasePorts:         config.DefaultDevClusterPorts(),
		UnixSocketPattern: filepath.Join(tempDir, "quicbaas-node-{n}.sock"),
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

	return mgr
}

// createBaseConfigWithoutSocket creates a test config without a pre-configured Unix socket.
// Uses devcluster mode with pebble backend - mode determines Raft usage, not backend.
func createBaseConfigWithoutSocket() *config.Config {
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
			QUIC: config.ServerQUICConfig{
				Enabled: true,
				Port:    10443,
			},
			MCP: config.ServerMCPConfig{
				Enabled: true,
				Port:    11443,
			},
			Unix: config.ServerUnixConfig{
				Enabled: false,
				Socket:  "", // No socket configured
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

func TestNewConfigGenerator(t *testing.T) {
	t.Run("ValidManager", func(t *testing.T) {
		mgr := createTestManager(t, 3)
		gen := NewConfigGenerator(mgr)
		require.NotNil(t, gen)
	})

	t.Run("NilManager", func(t *testing.T) {
		gen := NewConfigGenerator(nil)
		assert.Nil(t, gen)
	})
}

func TestEnsureDataDirs(t *testing.T) {
	t.Run("CreateDirectories", func(t *testing.T) {
		mgr := createTestManager(t, 3)
		gen := NewConfigGenerator(mgr)

		err := gen.EnsureDataDirs()
		require.NoError(t, err)

		// Verify directories were created.
		nodes := mgr.Nodes()
		for _, node := range nodes {
			info, err := os.Stat(node.DataDir)
			require.NoError(t, err)
			assert.True(t, info.IsDir())
			assert.Equal(t, os.FileMode(0750), info.Mode().Perm())
		}
	})

	t.Run("WALDirectories", func(t *testing.T) {
		mgr := createTestManager(t, 3)
		gen := NewConfigGenerator(mgr)

		err := gen.EnsureDataDirs()
		require.NoError(t, err)

		// Verify WAL directories were created.
		nodes := mgr.Nodes()
		for _, node := range nodes {
			if node.Config.Storage.WALDir != "" {
				info, err := os.Stat(node.Config.Storage.WALDir)
				require.NoError(t, err)
				assert.True(t, info.IsDir())
			}
		}
	})

	t.Run("NilManager", func(t *testing.T) {
		gen := &ConfigGenerator{manager: nil}
		err := gen.EnsureDataDirs()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not started")
	})

	t.Run("NoNodes", func(t *testing.T) {
		// Create manager but don't start it.
		baseConfig := createTestBaseConfig()
		log, err := logger.New(logger.Default())
		require.NoError(t, err)

		mgr, err := NewManager(&ManagerConfig{
			DevCluster: config.DefaultDevClusterConfig(),
			BaseConfig: baseConfig,
			Logger:     log,
		})
		require.NoError(t, err)
		// Don't call Start()

		gen := NewConfigGenerator(mgr)
		err = gen.EnsureDataDirs()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no nodes configured")
	})
}

func TestGenerateConfigFiles(t *testing.T) {
	t.Run("CreateConfigFiles", func(t *testing.T) {
		mgr := createTestManager(t, 3)
		gen := NewConfigGenerator(mgr)

		outputDir := t.TempDir()
		err := gen.GenerateConfigFiles(outputDir)
		require.NoError(t, err)

		// Verify config files were created.
		for i := 1; i <= 3; i++ {
			filename := filepath.Join(outputDir, "node-"+string(rune('0'+i))+".yaml")
			info, err := os.Stat(filename)
			require.NoError(t, err)
			assert.False(t, info.IsDir())
			assert.Equal(t, os.FileMode(0640), info.Mode().Perm())
		}
	})

	t.Run("ConfigFileContent", func(t *testing.T) {
		mgr := createTestManager(t, 3)
		gen := NewConfigGenerator(mgr)

		outputDir := t.TempDir()
		err := gen.GenerateConfigFiles(outputDir)
		require.NoError(t, err)

		// Read and verify node-1 config content.
		content, err := os.ReadFile(filepath.Join(outputDir, "node-1.yaml"))
		require.NoError(t, err)

		contentStr := string(content)
		assert.Contains(t, contentStr, "Node ID: 1")
		assert.Contains(t, contentStr, "server:")
		assert.Contains(t, contentStr, "mode: devcluster")
		assert.Contains(t, contentStr, "raft:")
		assert.Contains(t, contentStr, "node_id: 1")
		assert.Contains(t, contentStr, "enable_bootstrap: true")
		assert.Contains(t, contentStr, "storage:")
		// Backend is pebble - mode determines Raft usage
		assert.Contains(t, contentStr, "backend: pebble")
	})

	t.Run("NilManager", func(t *testing.T) {
		gen := &ConfigGenerator{manager: nil}
		err := gen.GenerateConfigFiles(t.TempDir())
		require.Error(t, err)
	})
}

func TestClusterInfo(t *testing.T) {
	t.Run("ThreeNodeCluster", func(t *testing.T) {
		mgr := createTestManager(t, 3)
		gen := NewConfigGenerator(mgr)

		info := gen.ClusterInfo()

		// Verify output contains expected information.
		assert.Contains(t, info, "DevCluster: 3 nodes")
		assert.Contains(t, info, "Node 1")
		assert.Contains(t, info, "Node 2")
		assert.Contains(t, info, "Node 3")
		assert.Contains(t, info, "[bootstrap]")
		assert.Contains(t, info, "gRPC:")
		assert.Contains(t, info, "REST:")
		assert.Contains(t, info, "Raft:")
		assert.Contains(t, info, "Data:")
	})

	t.Run("SingleNodeCluster", func(t *testing.T) {
		mgr := createTestManager(t, 1)
		gen := NewConfigGenerator(mgr)

		info := gen.ClusterInfo()

		assert.Contains(t, info, "DevCluster: 1 nodes")
		assert.Contains(t, info, "Node 1")
		assert.Contains(t, info, "[bootstrap]")
	})

	t.Run("NilManager", func(t *testing.T) {
		gen := &ConfigGenerator{manager: nil}
		info := gen.ClusterInfo()
		assert.Contains(t, info, "manager not initialized")
	})

	t.Run("NoNodesConfigured", func(t *testing.T) {
		// Create manager but don't start it.
		baseConfig := createTestBaseConfig()
		log, err := logger.New(logger.Default())
		require.NoError(t, err)

		mgr, err := NewManager(&ManagerConfig{
			DevCluster: config.DefaultDevClusterConfig(),
			BaseConfig: baseConfig,
			Logger:     log,
		})
		require.NoError(t, err)
		// Don't call Start()

		gen := NewConfigGenerator(mgr)
		info := gen.ClusterInfo()
		assert.Contains(t, info, "no nodes configured")
	})
}

func TestClusterAddresses(t *testing.T) {
	t.Run("ThreeNodeCluster", func(t *testing.T) {
		mgr := createTestManager(t, 3)
		gen := NewConfigGenerator(mgr)

		addrs := gen.ClusterAddresses()

		require.Len(t, addrs, 3)
		assert.Equal(t, "localhost:63001", addrs[1])
		assert.Equal(t, "localhost:63002", addrs[2])
		assert.Equal(t, "localhost:63003", addrs[3])
	})

	t.Run("NilManager", func(t *testing.T) {
		gen := &ConfigGenerator{manager: nil}
		addrs := gen.ClusterAddresses()
		assert.Nil(t, addrs)
	})
}

func TestCleanupDataDirs(t *testing.T) {
	t.Run("RemoveDirectories", func(t *testing.T) {
		mgr := createTestManager(t, 3)
		gen := NewConfigGenerator(mgr)

		// First create the directories.
		err := gen.EnsureDataDirs()
		require.NoError(t, err)

		// Verify they exist.
		devClusterCfg := mgr.DevClusterConfig()
		_, err = os.Stat(devClusterCfg.DataDir)
		require.NoError(t, err)

		// Clean up.
		err = gen.CleanupDataDirs()
		require.NoError(t, err)

		// Verify they're gone.
		_, err = os.Stat(devClusterCfg.DataDir)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("NilManager", func(t *testing.T) {
		gen := &ConfigGenerator{manager: nil}
		err := gen.CleanupDataDirs()
		require.Error(t, err)
	})

	t.Run("EmptyDataDir", func(t *testing.T) {
		baseConfig := createTestBaseConfig()
		log, err := logger.New(logger.Default())
		require.NoError(t, err)

		// Create config with empty data dir.
		devClusterCfg := &config.DevClusterConfig{
			NodeCount: 3,
			DataDir:   "", // Empty
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

		gen := NewConfigGenerator(mgr)

		// Should not error with empty data dir.
		err = gen.CleanupDataDirs()
		require.NoError(t, err)
	})
}

func TestYAMLGeneration(t *testing.T) {
	mgr := createTestManager(t, 3)
	gen := NewConfigGenerator(mgr)

	nodes := mgr.Nodes()
	require.NotEmpty(t, nodes)

	// Test YAML content for bootstrap node.
	content := gen.generateYAMLContent(nodes[0])

	t.Run("ServerSection", func(t *testing.T) {
		assert.Contains(t, content, "server:")
		assert.Contains(t, content, "mode: devcluster")
		assert.Contains(t, content, "grpc:")
		assert.Contains(t, content, "rest:")
		assert.Contains(t, content, "quic:")
		assert.Contains(t, content, "mcp:")
	})

	t.Run("RaftSection", func(t *testing.T) {
		assert.Contains(t, content, "raft:")
		assert.Contains(t, content, "node_id: 1")
		assert.Contains(t, content, "raft_address: localhost:")
		assert.Contains(t, content, "listen_address: localhost:")
		assert.Contains(t, content, "enable_bootstrap: true")
		assert.Contains(t, content, "join_addresses:")
	})

	t.Run("StorageSection", func(t *testing.T) {
		assert.Contains(t, content, "storage:")
		// Backend is pebble - mode determines Raft usage
		assert.Contains(t, content, "backend: pebble")
		assert.Contains(t, content, "data_dir:")
	})

	// Test non-bootstrap node.
	content2 := gen.generateYAMLContent(nodes[1])
	t.Run("NonBootstrapNode", func(t *testing.T) {
		assert.Contains(t, content2, "node_id: 2")
		assert.Contains(t, content2, "enable_bootstrap: false")
	})
}

func TestUnixSocketPattern(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("WithPattern", func(t *testing.T) {
		// Use base config with pre-configured socket to verify pattern overrides it.
		baseConfig := createTestBaseConfig()
		log, err := logger.New(logger.Default())
		require.NoError(t, err)

		devClusterCfg := &config.DevClusterConfig{
			NodeCount:         3,
			DataDir:           tempDir,
			BasePorts:         config.DefaultDevClusterPorts(),
			UnixSocketPattern: "/tmp/test-quicbaas-{n}.sock",
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
		assert.Equal(t, "/tmp/test-quicbaas-1.sock", nodes[0].UnixSocket)
		assert.Equal(t, "/tmp/test-quicbaas-2.sock", nodes[1].UnixSocket)
		assert.Equal(t, "/tmp/test-quicbaas-3.sock", nodes[2].UnixSocket)
	})

	t.Run("EmptyPatternWithNoBaseSocket", func(t *testing.T) {
		// Use base config without pre-configured socket.
		baseConfig := createBaseConfigWithoutSocket()
		log, err := logger.New(logger.Default())
		require.NoError(t, err)

		devClusterCfg := &config.DevClusterConfig{
			NodeCount:         3,
			DataDir:           tempDir,
			BasePorts:         config.DefaultDevClusterPorts(),
			UnixSocketPattern: "", // Empty pattern
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
		// Should have empty socket paths since base config has no socket
		// and pattern is empty.
		assert.Empty(t, nodes[0].UnixSocket)
		assert.Empty(t, nodes[1].UnixSocket)
		assert.Empty(t, nodes[2].UnixSocket)
	})

	t.Run("EmptyPatternPreservesBaseSocket", func(t *testing.T) {
		// When pattern is empty, the base config socket is preserved.
		baseConfig := createTestBaseConfig()
		log, err := logger.New(logger.Default())
		require.NoError(t, err)

		devClusterCfg := &config.DevClusterConfig{
			NodeCount:         3,
			DataDir:           tempDir,
			BasePorts:         config.DefaultDevClusterPorts(),
			UnixSocketPattern: "", // Empty pattern
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
		// When pattern is empty, the base config socket is preserved.
		// This is the expected behavior - empty pattern means "use base config".
		assert.Equal(t, "/tmp/quicbaas.sock", nodes[0].UnixSocket)
		assert.Equal(t, "/tmp/quicbaas.sock", nodes[1].UnixSocket)
		assert.Equal(t, "/tmp/quicbaas.sock", nodes[2].UnixSocket)
	})
}
