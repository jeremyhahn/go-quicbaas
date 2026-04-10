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

//go:build !raft

// Package devcluster provides stub implementations when Raft support is not compiled.
// Build with -tags raft to enable full devcluster functionality.
package devcluster

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// ErrRaftNotCompiled is returned when devcluster functions are called without the raft build tag.
var ErrRaftNotCompiled = errors.New("raft support not compiled - build with -tags raft")

// Manager coordinates multiple local QuicBaaS nodes for development.
// This is a stub implementation when Raft support is not compiled.
type Manager struct{}

// NodeInstance represents a single node in the devcluster.
// This is a stub implementation when Raft support is not compiled.
type NodeInstance struct {
	// NodeID is the Raft node ID (1-indexed).
	NodeID uint64

	// Config is the complete configuration for this node.
	Config *config.Config

	// DataDir is the data directory for this node.
	DataDir string

	// Port assignments for this node.
	GRPCPort   int
	RESTPort   int
	QUICPort   int
	MCPPort    int
	RaftPort   int
	UnixSocket string

	// State tracking using atomic operations for thread safety.
	// These fields are part of the interface contract with the raft implementation.
	//nolint:unused // Required for interface compatibility with raft build
	running atomic.Bool
	//nolint:unused // Required for interface compatibility with raft build
	ready atomic.Bool
}

// ManagerConfig configures the devcluster manager.
type ManagerConfig struct {
	// DevCluster contains the devcluster-specific configuration.
	DevCluster *config.DevClusterConfig

	// BaseConfig is the template configuration used to derive node configs.
	BaseConfig *config.Config

	// Logger is the logger instance for the manager.
	Logger *logger.Logger
}

// NewManager returns an error because Raft support is not compiled.
func NewManager(_ *ManagerConfig) (*Manager, error) {
	return nil, ErrRaftNotCompiled
}

// Start returns an error because Raft support is not compiled.
func (m *Manager) Start(_ context.Context) error {
	return ErrRaftNotCompiled
}

// Stop returns an error because Raft support is not compiled.
func (m *Manager) Stop(_ context.Context) error {
	return ErrRaftNotCompiled
}

// Nodes returns nil because Raft support is not compiled.
func (m *Manager) Nodes() []*NodeInstance {
	return nil
}

// NodeCount returns 0 because Raft support is not compiled.
func (m *Manager) NodeCount() int {
	return 0
}

// GetNodeConfig returns an error because Raft support is not compiled.
func (m *Manager) GetNodeConfig(_ uint64) (*config.Config, error) {
	return nil, ErrRaftNotCompiled
}

// GetNode returns an error because Raft support is not compiled.
func (m *Manager) GetNode(_ uint64) (*NodeInstance, error) {
	return nil, ErrRaftNotCompiled
}

// IsStarted returns false because Raft support is not compiled.
func (m *Manager) IsStarted() bool {
	return false
}

// IsStopped returns true because Raft support is not compiled.
func (m *Manager) IsStopped() bool {
	return true
}

// DevClusterConfig returns nil because Raft support is not compiled.
func (m *Manager) DevClusterConfig() *config.DevClusterConfig {
	return nil
}

// IsRunning returns false because Raft support is not compiled.
func (n *NodeInstance) IsRunning() bool {
	return false
}

// IsReady returns false because Raft support is not compiled.
func (n *NodeInstance) IsReady() bool {
	return false
}

// SetRunning is a no-op because Raft support is not compiled.
func (n *NodeInstance) SetRunning(_ bool) {
}

// SetReady is a no-op because Raft support is not compiled.
func (n *NodeInstance) SetReady(_ bool) {
}

// StopCh returns nil because Raft support is not compiled.
func (n *NodeInstance) StopCh() <-chan struct{} {
	return nil
}

// ErrCh returns nil because Raft support is not compiled.
func (n *NodeInstance) ErrCh() <-chan error {
	return nil
}

// SendError is a no-op because Raft support is not compiled.
func (n *NodeInstance) SendError(_ error) {
}
