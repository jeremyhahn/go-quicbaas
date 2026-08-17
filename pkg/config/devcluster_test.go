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
		assert.Equal(t, 9090, cfg.BasePorts.GRPC, "default GRPC base port should be 9090")
		assert.Equal(t, 8080, cfg.BasePorts.REST, "default REST base port should be 8080")
		assert.Equal(t, 11443, cfg.BasePorts.MCP, "default MCP base port should be 11443")
		assert.Equal(t, 63001, cfg.BasePorts.Raft, "default Raft base port should be 63001")
	})
}

func TestDefaultDevClusterPorts(t *testing.T) {
	t.Run("returns correct default ports", func(t *testing.T) {
		ports := DefaultDevClusterPorts()
		assert.Equal(t, 9090, ports.GRPC)
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
