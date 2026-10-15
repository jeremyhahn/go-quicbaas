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

package config

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultDevClusterConfig(t *testing.T) {
	t.Run("returns non-nil config", func(t *testing.T) {
		cfg := DefaultDevClusterConfig()
		require.NotNil(t, cfg)
	})

	t.Run("has correct default node count", func(t *testing.T) {
		cfg := DefaultDevClusterConfig()
		assert.Equal(t, 3, cfg.NodeCount, "default node count should be 3")
	})

	t.Run("has correct default data directory", func(t *testing.T) {
		cfg := DefaultDevClusterConfig()
		assert.Equal(t, "./dev-data", cfg.DataDir, "default data dir should be ./dev-data")
	})

	t.Run("has correct default Unix socket pattern", func(t *testing.T) {
		cfg := DefaultDevClusterConfig()
		assert.Equal(t, "/tmp/quicbaas-node-{n}.sock", cfg.UnixSocketPattern, "default Unix socket pattern should include {n} placeholder")
	})

	t.Run("has correct default base ports", func(t *testing.T) {
		cfg := DefaultDevClusterConfig()
		assert.Equal(t, 9290, cfg.BasePorts.GRPC, "default GRPC base port should be 9290")
		assert.Equal(t, 8080, cfg.BasePorts.REST, "default REST base port should be 8080")
		assert.Equal(t, 11443, cfg.BasePorts.MCP, "default MCP base port should be 11443")
		assert.Equal(t, 63001, cfg.BasePorts.Raft, "default Raft base port should be 63001")
	})
}

func TestDefaultDevClusterPorts(t *testing.T) {
	t.Run("returns correct default ports", func(t *testing.T) {
		ports := DefaultDevClusterPorts()
		assert.Equal(t, 9290, ports.GRPC)
		assert.Equal(t, 8080, ports.REST)
		assert.Equal(t, 11443, ports.MCP)
		assert.Equal(t, 63001, ports.Raft)
	})

	t.Run("ports do not conflict with each other", func(t *testing.T) {
		ports := DefaultDevClusterPorts()
		portSet := map[int]string{
			ports.GRPC: "GRPC",
			ports.REST: "REST",
			ports.MCP:  "MCP",
			ports.Raft: "Raft",
		}
		// If there were duplicates, the map would have fewer entries
		assert.Equal(t, 4, len(portSet), "all default ports should be unique")
	})

	// A fleet node agent co-located with a dev cluster node listens on 9091
	// (pkg/fleet, `fleet serve --bind`), so no node may take it, and the gRPC
	// ports of three nodes stay clear of every other default this repo binds.
	t.Run("three nodes stay off the agent port and the server defaults", func(t *testing.T) {
		const agentPort = 9091
		const nodes = 3
		ports := DefaultDevClusterPorts()
		_, raftPort, err := net.SplitHostPort(DefaultRaftAddress)
		require.NoError(t, err)
		server := DefaultServerConfig()
		reserved := map[string]bool{
			strconv.Itoa(agentPort):        true,
			raftPort:                       true,
			strconv.Itoa(server.GRPC.Port): true,
			strconv.Itoa(server.REST.Port): true,
			strconv.Itoa(server.MCP.Port):  true,
		}
		for n := 0; n < nodes; n++ {
			grpc := ports.GRPC + n
			assert.False(t, reserved[strconv.Itoa(grpc)], "node %d gRPC port %d collides with a default", n+1, grpc)
			for _, port := range []int{grpc, ports.REST + n, ports.MCP + n, ports.Raft + n} {
				assert.NotEqual(t, agentPort, port, "node %d takes the agent's port", n+1)
			}
		}
	})
}

func TestDevClusterConfig_CustomValues(t *testing.T) {
	t.Run("custom node count", func(t *testing.T) {
		cfg := &DevClusterConfig{
			NodeCount: 5,
			DataDir:   "/custom/data",
		}
		assert.Equal(t, 5, cfg.NodeCount)
		assert.Equal(t, "/custom/data", cfg.DataDir)
	})

	t.Run("custom base ports", func(t *testing.T) {
		cfg := &DevClusterConfig{
			BasePorts: DevClusterPorts{
				GRPC: 19090,
				REST: 18080,
				MCP:  21443,
				Raft: 73001,
			},
		}
		assert.Equal(t, 19090, cfg.BasePorts.GRPC)
		assert.Equal(t, 18080, cfg.BasePorts.REST)
		assert.Equal(t, 21443, cfg.BasePorts.MCP)
		assert.Equal(t, 73001, cfg.BasePorts.Raft)
	})

	t.Run("zero values are valid for disabling features", func(t *testing.T) {
		cfg := &DevClusterConfig{
			NodeCount: 0, // Could indicate disabled or single-node
			BasePorts: DevClusterPorts{
				GRPC: 0, // Zero could mean disabled
			},
		}
		assert.Equal(t, 0, cfg.NodeCount)
		assert.Equal(t, 0, cfg.BasePorts.GRPC)
	})
}

func TestDevClusterPorts_ZeroValue(t *testing.T) {
	t.Run("zero value struct has all zero ports", func(t *testing.T) {
		var ports DevClusterPorts
		assert.Equal(t, 0, ports.GRPC)
		assert.Equal(t, 0, ports.REST)
		assert.Equal(t, 0, ports.MCP)
		assert.Equal(t, 0, ports.Raft)
	})
}

// TestLoader_DevClusterHost proves devcluster.host is settable by every path
// the daemon loader wires (defaults, config file, environment) and that the
// environment wins over the file.
func TestLoader_DevClusterHost(t *testing.T) {
	writeConfig := func(t *testing.T, body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "quicbaas.yaml")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		return path
	}

	t.Run("defaults layer", func(t *testing.T) {
		cfg, err := NewLoader().Load(writeConfig(t, "server:\n  mode: devcluster\n"))
		require.NoError(t, err)
		assert.Equal(t, DefaultDevClusterHost, cfg.DevCluster.Host)
	})

	t.Run("config file", func(t *testing.T) {
		cfg, err := NewLoader().Load(writeConfig(t, "devcluster:\n  host: file.devcluster.test\n"))
		require.NoError(t, err)
		assert.Equal(t, "file.devcluster.test", cfg.DevCluster.Host)
	})

	t.Run("environment without a file", func(t *testing.T) {
		t.Setenv("QUICBAAS_DEVCLUSTER_HOST", "env.devcluster.test")
		cfg, err := NewLoader().Load(filepath.Join(t.TempDir(), "missing.yaml"))
		require.NoError(t, err)
		assert.Equal(t, "env.devcluster.test", cfg.DevCluster.Host)
	})

	t.Run("environment beats config file", func(t *testing.T) {
		t.Setenv("QUICBAAS_DEVCLUSTER_HOST", "env.devcluster.test")
		cfg, err := NewLoader().Load(writeConfig(t, "devcluster:\n  host: file.devcluster.test\n"))
		require.NoError(t, err)
		assert.Equal(t, "env.devcluster.test", cfg.DevCluster.Host)
	})
}
