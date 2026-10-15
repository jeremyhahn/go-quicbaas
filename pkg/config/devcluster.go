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

// Package config provides configuration structures for QuicBaaS.
package config

import "github.com/spf13/viper"

// DefaultDevClusterHost is the defaults-layer host every devcluster node
// listens on and advertises for Raft. Override it with devcluster.host in
// the config file or QUICBAAS_DEVCLUSTER_HOST in the environment.
const DefaultDevClusterHost = "localhost"

// DevClusterConfig configures local multi-node development clusters.
type DevClusterConfig struct {
	// Host is the host each node's Raft address is built from, paired with
	// its auto-incremented Raft port (default: DefaultDevClusterHost).
	Host string `mapstructure:"host"`

	// NodeCount is the number of local nodes to start (default: 3)
	NodeCount int `mapstructure:"node_count"`

	// DataDir is the base directory for all node data (default: ./dev-data)
	// Each node gets a subdirectory: ./dev-data/node-1, ./dev-data/node-2, etc.
	DataDir string `mapstructure:"data_dir"`

	// BasePorts defines starting ports for auto-increment per node
	BasePorts DevClusterPorts `mapstructure:"base_ports"`

	// UnixSocketPattern is the pattern for Unix sockets (default: /tmp/quicbaas-node-{n}.sock)
	UnixSocketPattern string `mapstructure:"unix_socket_pattern"`
}

// DevClusterPorts defines base ports for DevCluster auto-increment.
type DevClusterPorts struct {
	GRPC int `mapstructure:"grpc"` // Default: 9290 (nodes: 9290, 9291, 9292; clear of the fleet agent's 9091)
	REST int `mapstructure:"rest"` // Default: 8080 (nodes: 8080, 8081, 8082)
	MCP  int `mapstructure:"mcp"`  // Default: 11443 (nodes: 11443, 11444, 11445)
	Raft int `mapstructure:"raft"` // Default: 63001 (nodes: 63001, 63002, 63003)
}

// DefaultDevClusterConfig returns sensible defaults for DevCluster.
func DefaultDevClusterConfig() *DevClusterConfig {
	return &DevClusterConfig{
		Host:              DefaultDevClusterHost,
		NodeCount:         3,
		DataDir:           "./dev-data",
		BasePorts:         DefaultDevClusterPorts(),
		UnixSocketPattern: "/tmp/quicbaas-node-{n}.sock",
	}
}

// DefaultDevClusterPorts returns the default base ports for DevCluster.
func DefaultDevClusterPorts() DevClusterPorts {
	return DevClusterPorts{
		GRPC: 9290,
		REST: 8080,
		MCP:  11443,
		Raft: 63001,
	}
}

// BindDevClusterHost registers devcluster.host with v: its defaults-layer
// value and its QUICBAAS_DEVCLUSTER_HOST environment binding. Without the
// explicit binding viper's Unmarshal never consults the environment for a
// key absent from the config file.
func BindDevClusterHost(v *viper.Viper) {
	v.SetDefault("devcluster.host", DefaultDevClusterHost)
	_ = v.BindEnv("devcluster.host", "QUICBAAS_DEVCLUSTER_HOST")
}
