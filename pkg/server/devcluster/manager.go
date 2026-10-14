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
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// devclusterDeploymentID is the shared quicraft deployment identity every
// node of a local devcluster runs under.
//
// Any non-zero constant works; what matters is that all members agree, which
// is exactly what deriving it per node from NodeID fails to do. It is fixed
// rather than generated so a devcluster restarted against the same data
// directory rejoins the cluster it left.
const devclusterDeploymentID uint64 = 9001

// Manager coordinates multiple local QuicBaaS nodes for development.
type Manager struct {
	cfg        *config.DevClusterConfig
	baseConfig *config.Config
	nodes      []*NodeInstance
	logger     *logger.Logger

	started atomic.Bool
	stopped atomic.Bool
	mu      sync.RWMutex
}

// NodeInstance represents a single node in the devcluster.
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
	MCPPort    int
	RaftPort   int
	UnixSocket string

	// RaftAddress is the node's advertised Raft address (host:port).
	RaftAddress string

	// State tracking using atomic operations for thread safety.
	running atomic.Bool
	ready   atomic.Bool
	errChan chan error
	stopCh  chan struct{}
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

// NewManager creates a new devcluster manager.
func NewManager(cfg *ManagerConfig) (*Manager, error) {
	if cfg == nil {
		cfg = &ManagerConfig{}
	}

	if cfg.DevCluster == nil {
		cfg.DevCluster = config.DefaultDevClusterConfig()
	}

	// Copied so filling in the defaults-layer host never mutates the caller's
	// configuration.
	devCluster := *cfg.DevCluster
	if devCluster.Host == "" {
		devCluster.Host = config.DefaultDevClusterHost
	}

	if cfg.BaseConfig == nil {
		return nil, &qberrors.QBError{
			Code: qberrors.ErrInvalidInput,
			Op:   "devcluster.NewManager",
			Err:  ErrBaseConfigRequired,
		}
	}

	if cfg.DevCluster.NodeCount <= 0 {
		return nil, &qberrors.QBError{
			Code: qberrors.ErrInvalidInput,
			Op:   "devcluster.NewManager",
			Err:  &InvalidNodeCountError{Count: cfg.DevCluster.NodeCount},
		}
	}

	log := cfg.Logger
	if log == nil {
		var err error
		log, err = logger.New(logger.Default())
		if err != nil {
			return nil, &qberrors.QBError{
				Code: qberrors.ErrInternal,
				Op:   "devcluster.NewManager",
				Err:  &DevClusterCauseError{Message: "failed to create default logger", Cause: err},
			}
		}
	}

	return &Manager{
		cfg:        &devCluster,
		baseConfig: cfg.BaseConfig,
		nodes:      make([]*NodeInstance, 0, devCluster.NodeCount),
		logger:     log.WithField("component", "devcluster-manager"),
	}, nil
}

// Start initializes and generates configurations for all nodes in the devcluster.
// It does not start actual server instances; that is handled by the server package.
func (m *Manager) Start(ctx context.Context) error {
	if !m.started.CompareAndSwap(false, true) {
		return &qberrors.QBError{
			Code: qberrors.ErrInternal,
			Op:   "devcluster.Start",
			Err:  ErrManagerAlreadyStarted,
		}
	}

	m.logger.Infof("Starting devcluster configuration with %d nodes", m.cfg.NodeCount)

	// Check for context cancellation.
	select {
	case <-ctx.Done():
		return &qberrors.QBError{
			Code: qberrors.ErrCanceled,
			Op:   "devcluster.Start",
			Err:  ctx.Err(),
		}
	default:
	}

	// Generate node configurations.
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := 0; i < m.cfg.NodeCount; i++ {
		nodeConfig := m.generateNodeConfig(i)
		node := &NodeInstance{
			NodeID:      nodeConfig.Raft.NodeID,
			Config:      nodeConfig,
			DataDir:     nodeConfig.Storage.DataDir,
			GRPCPort:    nodeConfig.Server.GRPC.Port,
			RESTPort:    nodeConfig.Server.REST.Port,
			MCPPort:     nodeConfig.Server.MCP.Port,
			RaftPort:    m.parseRaftPort(nodeConfig.Raft.RaftAddress),
			UnixSocket:  nodeConfig.Server.Unix.Socket,
			RaftAddress: nodeConfig.Raft.RaftAddress,
			errChan:     make(chan error, 1),
			stopCh:      make(chan struct{}),
		}
		m.nodes = append(m.nodes, node)

		m.logger.WithFields(map[string]interface{}{
			"node_id":   node.NodeID,
			"grpc_port": node.GRPCPort,
			"rest_port": node.RESTPort,
			"raft_port": node.RaftPort,
			"data_dir":  node.DataDir,
		}).Debug("Generated node configuration")
	}

	m.logger.Info("DevCluster node configurations generated successfully")
	m.logger.Info("Note: Actual node startup is delegated to server - devcluster provides config only")

	return nil
}

// generateNodeConfig creates configuration for node at index i (0-indexed).
func (m *Manager) generateNodeConfig(nodeIndex int) *config.Config {
	nodeNum := nodeIndex + 1

	// Clone base config to avoid modifying the original.
	cfg := m.baseConfig.Clone()

	// Set node-specific Raft values.
	// #nosec G115 - nodeNum is always positive and small (bounded by NodeCount config).
	cfg.Raft.NodeID = uint64(nodeNum)
	cfg.Raft.RaftAddress = m.raftAddress(nodeIndex)
	cfg.Raft.ListenAddress = cfg.Raft.RaftAddress

	// Data directory per node.
	cfg.Storage.DataDir = fmt.Sprintf("%s/node-%d", m.cfg.DataDir, nodeNum)
	cfg.Storage.WALDir = fmt.Sprintf("%s/node-%d/wal", m.cfg.DataDir, nodeNum)

	// Ports per node (auto-incremented from base ports).
	cfg.Server.GRPC.Port = m.cfg.BasePorts.GRPC + nodeIndex
	cfg.Server.REST.Port = m.cfg.BasePorts.REST + nodeIndex
	cfg.Server.MCP.Port = m.cfg.BasePorts.MCP + nodeIndex

	// Unix socket per node.
	if m.cfg.UnixSocketPattern != "" {
		cfg.Server.Unix.Socket = strings.ReplaceAll(
			m.cfg.UnixSocketPattern,
			"{n}",
			fmt.Sprint(nodeNum),
		)
	}

	// All nodes in the cluster share one deployment identity. Without it each
	// node falls back to deriving DeploymentID from its own NodeID
	// (buildQRDBConfig), so every node would isolate itself into a cluster of
	// one — quicraft uses the value to separate co-located clusters, and a
	// devcluster where each member disagrees about which cluster it is in
	// never forms.
	cfg.Raft.DeploymentID = devclusterDeploymentID

	// The full member roster, in "id=addr" form, INCLUDING this node.
	//
	// Two things this must get right, both learned by getting them wrong. The
	// entries carry the replica ID because buildClusterMembers keeps only
	// entries containing "=" and falls back to a single-member map when it
	// finds none — raw "host:port" addresses left every devcluster node
	// bootstrapping alone. And self is included because the list is consumed
	// as the cluster's MEMBERSHIP, not as a list of peers to dial: a roster
	// missing the local node describes a cluster it is not a member of.
	joinAddrs := make([]string, 0, m.cfg.NodeCount)
	for j := 0; j < m.cfg.NodeCount; j++ {
		joinAddrs = append(joinAddrs, fmt.Sprintf("%d=%s", j+1, m.raftAddress(j)))
	}
	cfg.Raft.JoinAddresses = joinAddrs

	// Enable bootstrap on the first node only.
	cfg.Raft.EnableBootstrap = (nodeIndex == 0)
	cfg.Raft.MinNodes = m.cfg.NodeCount

	return cfg
}

// parseRaftPort extracts the port number from a raft address.
func (m *Manager) parseRaftPort(addr string) int {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0
	}
	return port
}

// raftAddress is the Raft address of the node at nodeIndex: the configured
// devcluster host paired with the node's auto-incremented Raft port. It is
// the single place a devcluster Raft address is built.
func (m *Manager) raftAddress(nodeIndex int) string {
	return net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.BasePorts.Raft+nodeIndex))
}

// Stop gracefully stops all node instances.
func (m *Manager) Stop(ctx context.Context) error {
	if !m.stopped.CompareAndSwap(false, true) {
		return nil
	}

	m.logger.Info("Stopping devcluster...")

	m.mu.RLock()
	nodes := m.nodes
	m.mu.RUnlock()

	// Signal all nodes to stop.
	for _, node := range nodes {
		select {
		case <-node.stopCh:
			// Already closed.
		default:
			close(node.stopCh)
		}
	}

	// Wait for all nodes with timeout from context.
	done := make(chan struct{})
	go func() {
		for _, node := range nodes {
			for node.running.Load() {
				select {
				case <-ctx.Done():
					return
				case <-time.After(50 * time.Millisecond):
				}
			}
		}
		close(done)
	}()

	select {
	case <-done:
		m.logger.Info("All devcluster nodes stopped")
	case <-ctx.Done():
		m.logger.Warn("Devcluster stop timed out waiting for context")
		return &qberrors.QBError{
			Code: qberrors.ErrTimeout,
			Op:   "devcluster.Stop",
			Err:  ctx.Err(),
		}
	}

	return nil
}

// Nodes returns all node instances.
func (m *Manager) Nodes() []*NodeInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to prevent external modification.
	result := make([]*NodeInstance, len(m.nodes))
	copy(result, m.nodes)
	return result
}

// NodeCount returns the number of configured nodes.
func (m *Manager) NodeCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.nodes)
}

// GetNodeConfig returns the config for node by NodeID (1-indexed like Raft).
func (m *Manager) GetNodeConfig(nodeID uint64) (*config.Config, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, node := range m.nodes {
		if node.NodeID == nodeID {
			return node.Config, nil
		}
	}

	return nil, &qberrors.QBError{
		Code: qberrors.ErrNotFound,
		Op:   "devcluster.GetNodeConfig",
		Err:  &NodeNotFoundError{NodeID: nodeID},
	}
}

// GetNode returns the NodeInstance for the given NodeID.
func (m *Manager) GetNode(nodeID uint64) (*NodeInstance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, node := range m.nodes {
		if node.NodeID == nodeID {
			return node, nil
		}
	}

	return nil, &qberrors.QBError{
		Code: qberrors.ErrNotFound,
		Op:   "devcluster.GetNode",
		Err:  &NodeNotFoundError{NodeID: nodeID},
	}
}

// IsStarted returns true if the manager has been started.
func (m *Manager) IsStarted() bool {
	return m.started.Load()
}

// IsStopped returns true if the manager has been stopped.
func (m *Manager) IsStopped() bool {
	return m.stopped.Load()
}

// DevClusterConfig returns the devcluster configuration.
func (m *Manager) DevClusterConfig() *config.DevClusterConfig {
	return m.cfg
}

// IsRunning returns true if the node is running.
func (n *NodeInstance) IsRunning() bool {
	return n.running.Load()
}

// IsReady returns true if the node is ready to accept requests.
func (n *NodeInstance) IsReady() bool {
	return n.ready.Load()
}

// SetRunning sets the running state of the node.
func (n *NodeInstance) SetRunning(running bool) {
	n.running.Store(running)
}

// SetReady sets the ready state of the node.
func (n *NodeInstance) SetReady(ready bool) {
	n.ready.Store(ready)
}

// StopCh returns the stop channel for the node.
func (n *NodeInstance) StopCh() <-chan struct{} {
	return n.stopCh
}

// ErrCh returns the error channel for the node.
func (n *NodeInstance) ErrCh() <-chan error {
	return n.errChan
}

// SendError sends an error to the node's error channel (non-blocking).
func (n *NodeInstance) SendError(err error) {
	select {
	case n.errChan <- err:
	default:
		// Channel full or closed, drop the error.
	}
}
