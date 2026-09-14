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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// ConfigGenerator generates configuration files and directories for devcluster nodes.
type ConfigGenerator struct {
	manager *Manager
}

// NewConfigGenerator creates a config generator for the devcluster.
func NewConfigGenerator(mgr *Manager) *ConfigGenerator {
	if mgr == nil {
		return nil
	}
	return &ConfigGenerator{manager: mgr}
}

// EnsureDataDirs creates data directories for all nodes.
func (g *ConfigGenerator) EnsureDataDirs() error {
	if g.manager == nil {
		return &qberrors.QBError{
			Code: qberrors.ErrInternal,
			Op:   "devcluster.EnsureDataDirs",
			Err:  ErrManagerNotStarted,
		}
	}

	nodes := g.manager.Nodes()
	if len(nodes) == 0 {
		return &qberrors.QBError{
			Code: qberrors.ErrInternal,
			Op:   "devcluster.EnsureDataDirs",
			Err:  &DevClusterMessageError{Message: "devcluster manager not started: no nodes configured"},
		}
	}

	for _, node := range nodes {
		// Create main data directory.
		// #nosec G301 - Data directories need 0750 for group access.
		if err := os.MkdirAll(node.DataDir, 0750); err != nil {
			return &qberrors.QBError{
				Code: qberrors.ErrInternal,
				Op:   "devcluster.EnsureDataDirs",
				Err:  &DataDirNodeError{NodeID: node.NodeID, Cause: err},
			}
		}

		// Create WAL directory if configured separately.
		if node.Config != nil && node.Config.Storage.WALDir != "" {
			// #nosec G301 - WAL directories need 0750 for group access.
			if err := os.MkdirAll(node.Config.Storage.WALDir, 0750); err != nil {
				return &qberrors.QBError{
					Code: qberrors.ErrInternal,
					Op:   "devcluster.EnsureDataDirs",
					Err:  &DataDirNodeError{NodeID: node.NodeID, Subdir: "WAL", Cause: err},
				}
			}
		}
	}

	return nil
}

// GenerateConfigFiles writes config files for each node to the specified output directory.
func (g *ConfigGenerator) GenerateConfigFiles(outputDir string) error {
	if g.manager == nil {
		return &qberrors.QBError{
			Code: qberrors.ErrInternal,
			Op:   "devcluster.GenerateConfigFiles",
			Err:  ErrManagerNotStarted,
		}
	}

	nodes := g.manager.Nodes()
	if len(nodes) == 0 {
		return &qberrors.QBError{
			Code: qberrors.ErrInternal,
			Op:   "devcluster.GenerateConfigFiles",
			Err:  &DevClusterMessageError{Message: "devcluster manager not started: no nodes configured"},
		}
	}

	// Create output directory.
	// #nosec G301 - Config directories need 0750 for group access.
	if err := os.MkdirAll(outputDir, 0750); err != nil {
		return &qberrors.QBError{
			Code: qberrors.ErrInternal,
			Op:   "devcluster.GenerateConfigFiles",
			Err:  &DevClusterCauseError{Message: "failed to generate node configuration", Cause: err},
		}
	}

	for _, node := range nodes {
		filename := filepath.Join(outputDir, fmt.Sprintf("node-%d.yaml", node.NodeID))

		// Generate YAML content from node configuration.
		content := g.generateYAMLContent(node)

		// #nosec G306 - Config files need 0640 for owner read/write, group read.
		if err := os.WriteFile(filename, []byte(content), 0640); err != nil {
			return &qberrors.QBError{
				Code: qberrors.ErrInternal,
				Op:   "devcluster.GenerateConfigFiles",
				Err:  &ConfigGenNodeError{NodeID: node.NodeID, Cause: err},
			}
		}
	}

	return nil
}

// generateYAMLContent creates YAML configuration content for a node.
func (g *ConfigGenerator) generateYAMLContent(node *NodeInstance) string {
	var sb strings.Builder

	sb.WriteString("# QuicBaaS DevCluster Node Configuration\n")
	sb.WriteString(fmt.Sprintf("# Node ID: %d\n", node.NodeID))
	sb.WriteString("# Auto-generated - do not edit\n")
	sb.WriteString("\n")

	// Server section.
	sb.WriteString("server:\n")
	sb.WriteString("  mode: devcluster\n")
	sb.WriteString("  grpc:\n")
	sb.WriteString("    enabled: true\n")
	sb.WriteString(fmt.Sprintf("    port: %d\n", node.GRPCPort))
	sb.WriteString("  rest:\n")
	sb.WriteString("    enabled: true\n")
	sb.WriteString(fmt.Sprintf("    port: %d\n", node.RESTPort))
	sb.WriteString("  mcp:\n")
	sb.WriteString("    enabled: true\n")
	sb.WriteString(fmt.Sprintf("    port: %d\n", node.MCPPort))
	if node.UnixSocket != "" {
		sb.WriteString("  unix:\n")
		sb.WriteString(fmt.Sprintf("    socket: %s\n", node.UnixSocket))
	}
	sb.WriteString("\n")

	// Raft section.
	sb.WriteString("raft:\n")
	sb.WriteString(fmt.Sprintf("  node_id: %d\n", node.NodeID))
	sb.WriteString(fmt.Sprintf("  raft_address: localhost:%d\n", node.RaftPort))
	sb.WriteString(fmt.Sprintf("  listen_address: localhost:%d\n", node.RaftPort))
	if node.Config != nil && len(node.Config.Raft.JoinAddresses) > 0 {
		sb.WriteString("  join_addresses:\n")
		for _, addr := range node.Config.Raft.JoinAddresses {
			sb.WriteString(fmt.Sprintf("    - %s\n", addr))
		}
	}
	// Emitted because a generated config is run on its own: without it the
	// node derives a deployment identity from its own node_id and isolates
	// itself from the cluster this file was generated for.
	sb.WriteString(fmt.Sprintf("  deployment_id: %d\n", devclusterDeploymentID))
	sb.WriteString(fmt.Sprintf("  enable_bootstrap: %t\n", node.Config != nil && node.Config.Raft.EnableBootstrap))
	sb.WriteString(fmt.Sprintf("  min_nodes: %d\n", g.manager.cfg.NodeCount))
	sb.WriteString("\n")

	// Storage section.
	sb.WriteString("storage:\n")
	sb.WriteString("  backend: pebble\n")
	sb.WriteString(fmt.Sprintf("  data_dir: %s\n", node.DataDir))
	if node.Config != nil && node.Config.Storage.WALDir != "" {
		sb.WriteString(fmt.Sprintf("  wal_dir: %s\n", node.Config.Storage.WALDir))
	}

	return sb.String()
}

// ClusterInfo returns a human-readable summary string of the devcluster.
func (g *ConfigGenerator) ClusterInfo() string {
	if g.manager == nil {
		return "DevCluster: manager not initialized"
	}

	nodes := g.manager.Nodes()
	if len(nodes) == 0 {
		return "DevCluster: no nodes configured"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("DevCluster: %d nodes\n", len(nodes)))
	sb.WriteString("─────────────────────────────────────────────────────────\n")

	for _, node := range nodes {
		bootstrapMarker := ""
		if node.Config != nil && node.Config.Raft.EnableBootstrap {
			bootstrapMarker = " [bootstrap]"
		}

		sb.WriteString(fmt.Sprintf("  Node %d%s:\n", node.NodeID, bootstrapMarker))
		sb.WriteString(fmt.Sprintf("    gRPC: %d, REST: %d, MCP: %d\n",
			node.GRPCPort, node.RESTPort, node.MCPPort))
		sb.WriteString(fmt.Sprintf("    Raft: localhost:%d\n", node.RaftPort))
		sb.WriteString(fmt.Sprintf("    Data: %s\n", node.DataDir))
		if node.UnixSocket != "" {
			sb.WriteString(fmt.Sprintf("    Unix: %s\n", node.UnixSocket))
		}
	}

	return sb.String()
}

// ClusterAddresses returns a map of node IDs to their Raft addresses.
func (g *ConfigGenerator) ClusterAddresses() map[uint64]string {
	if g.manager == nil {
		return nil
	}

	nodes := g.manager.Nodes()
	result := make(map[uint64]string, len(nodes))

	for _, node := range nodes {
		result[node.NodeID] = fmt.Sprintf("localhost:%d", node.RaftPort)
	}

	return result
}

// CleanupDataDirs removes all data directories for the devcluster.
// Use with caution as this deletes all persisted data.
func (g *ConfigGenerator) CleanupDataDirs() error {
	if g.manager == nil {
		return &qberrors.QBError{
			Code: qberrors.ErrInternal,
			Op:   "devcluster.CleanupDataDirs",
			Err:  ErrManagerNotStarted,
		}
	}

	cfg := g.manager.DevClusterConfig()
	if cfg == nil || cfg.DataDir == "" {
		return nil
	}

	// Remove the entire devcluster data directory.
	if err := os.RemoveAll(cfg.DataDir); err != nil {
		return &qberrors.QBError{
			Code: qberrors.ErrInternal,
			Op:   "devcluster.CleanupDataDirs",
			Err:  &DevClusterCauseError{Message: "failed to remove data directory " + cfg.DataDir, Cause: err},
		}
	}

	return nil
}
