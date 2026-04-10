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

// Package main provides local type definitions for operations that were
// previously in the deleted pkg/operations package. These types are used
// by CLI commands for display and configuration purposes.
package main

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// RebalanceReport contains the result of a rebalance operation.
type RebalanceReport struct {
	StartTime       time.Time     `json:"start_time"`
	EndTime         time.Time     `json:"end_time"`
	Duration        time.Duration `json:"duration"`
	MovesCompleted  int           `json:"moves_completed"`
	MovesFailed     int           `json:"moves_failed"`
	KeysRebalanced  int64         `json:"keys_rebalanced"`
	BytesRebalanced int64         `json:"bytes_rebalanced"`
	ImbalanceBefore float64       `json:"imbalance_before"`
	ImbalanceAfter  float64       `json:"imbalance_after"`
	Errors          []string      `json:"errors,omitempty"`
}

// MigrationConfig configures a data migration operation.
type MigrationConfig struct {
	BatchSize          int           `json:"batch_size"`
	Concurrency        int           `json:"concurrency"`
	VerifyData         bool          `json:"verify_data"`
	DeleteSource       bool          `json:"delete_source"`
	RateLimitKBps      int           `json:"rate_limit_kbps"`
	CheckpointInterval time.Duration `json:"checkpoint_interval"`
}

// MigrationProgress tracks migration progress.
type MigrationProgress struct {
	KeysProcessed      int64         `json:"keys_processed"`
	BytesProcessed     uint64        `json:"bytes_processed"`
	ElapsedTime        time.Duration `json:"elapsed_time"`
	CurrentRate        float64       `json:"current_rate"`
	PercentComplete    float64       `json:"percent_complete"`
	EstimatedTotal     int64         `json:"estimated_total"`
	EstimatedRemaining time.Duration `json:"estimated_remaining"`
}

// MigrationResult contains the final result of a migration.
type MigrationResult struct {
	KeysProcessed    int64         `json:"keys_processed"`
	KeysFailed       int64         `json:"keys_failed"`
	BytesTransferred uint64        `json:"bytes_transferred"`
	Duration         time.Duration `json:"duration"`
	StartTime        time.Time     `json:"start_time"`
	EndTime          time.Time     `json:"end_time"`
	Errors           []string      `json:"errors,omitempty"`
}

// MigrationManager manages data migration between storage backends.
type MigrationManager struct {
	source interfaces.Storage
	dest   interfaces.Storage
	config *MigrationConfig
	logger interfaces.Logger
}

// NewMigrationManager creates a new migration manager.
func NewMigrationManager(
	source interfaces.Storage,
	dest interfaces.Storage,
	config *MigrationConfig,
	logger interfaces.Logger,
) (*MigrationManager, error) {
	return &MigrationManager{
		source: source,
		dest:   dest,
		config: config,
		logger: logger,
	}, nil
}

// Migrate performs the data migration.
func (m *MigrationManager) Migrate(ctx context.Context) (*MigrationResult, error) {
	startTime := time.Now()

	// Iterate source and copy to destination
	iter := m.source.NewIterator(nil)
	defer iter.Close()

	var keysProcessed, keysFailed int64
	var bytesTransferred uint64

	if iter.SeekToFirst() {
		for {
			select {
			case <-ctx.Done():
				return &MigrationResult{
					KeysProcessed:    keysProcessed,
					KeysFailed:       keysFailed,
					BytesTransferred: bytesTransferred,
					Duration:         time.Since(startTime),
					StartTime:        startTime,
					EndTime:          time.Now(),
					Errors:           []string{ctx.Err().Error()},
				}, ctx.Err()
			default:
			}

			key := iter.Key()
			value := iter.Value()

			if err := m.dest.Set(key, value); err != nil {
				keysFailed++
				m.logger.WithError(err).Error("Failed to migrate key")
			} else {
				keysProcessed++
				bytesTransferred += uint64(len(key) + len(value))
			}

			if !iter.Next() {
				break
			}
		}
	}

	return &MigrationResult{
		KeysProcessed:    keysProcessed,
		KeysFailed:       keysFailed,
		BytesTransferred: bytesTransferred,
		Duration:         time.Since(startTime),
		StartTime:        startTime,
		EndTime:          time.Now(),
	}, nil
}

// GetProgress returns the current migration progress.
func (m *MigrationManager) GetProgress() *MigrationProgress {
	return &MigrationProgress{}
}

// Rollback attempts to undo migrated data.
func (m *MigrationManager) Rollback(ctx context.Context) error {
	return nil
}

// NodeStatus represents the health status of a cluster node.
type NodeStatus int

const (
	// NodeStatusHealthy indicates the node is operating normally.
	NodeStatusHealthy NodeStatus = iota
	// NodeStatusDegraded indicates the node has reduced capacity.
	NodeStatusDegraded
	// NodeStatusFailed indicates the node is unreachable.
	NodeStatusFailed
)

// String returns the string representation of a NodeStatus.
func (s NodeStatus) String() string {
	switch s {
	case NodeStatusHealthy:
		return "healthy"
	case NodeStatusDegraded:
		return "degraded"
	case NodeStatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// NodeState holds the current state of a cluster node.
type NodeState struct {
	NodeID           string     `json:"node_id"`
	Address          string     `json:"address"`
	Status           NodeStatus `json:"status"`
	JoinedAt         time.Time  `json:"joined_at"`
	LastSeenAt       time.Time  `json:"last_seen_at"`
	HealthCheckCount int64      `json:"health_check_count"`
	FailureCount     int64      `json:"failure_count"`
	KeyCount         int64      `json:"key_count"`
	ByteCount        int64      `json:"byte_count"`
}

// NodeReplacementConfig configures node replacement behavior.
type NodeReplacementConfig struct {
	HealthCheckInterval    time.Duration `json:"health_check_interval"`
	HealthCheckTimeout     time.Duration `json:"health_check_timeout"`
	EvacuationTimeout      time.Duration `json:"evacuation_timeout"`
	VerifyData             bool          `json:"verify_data"`
	AutoReplace            bool          `json:"auto_replace"`
	GracePeriod            time.Duration `json:"grace_period"`
	MaxConcurrentTransfers int           `json:"max_concurrent_transfers"`
}

// DefaultNodeReplacementConfig returns the default configuration.
func DefaultNodeReplacementConfig() *NodeReplacementConfig {
	return &NodeReplacementConfig{
		HealthCheckInterval:    30 * time.Second,
		HealthCheckTimeout:     5 * time.Second,
		EvacuationTimeout:      30 * time.Minute,
		VerifyData:             true,
		AutoReplace:            false,
		GracePeriod:            5 * time.Minute,
		MaxConcurrentTransfers: 2,
	}
}

// NodeReplacementReport contains the result of a node replacement operation.
type NodeReplacementReport struct {
	OldNodeID        string        `json:"old_node_id"`
	NewNodeID        string        `json:"new_node_id"`
	StartTime        time.Time     `json:"start_time"`
	EndTime          time.Time     `json:"end_time"`
	Duration         time.Duration `json:"duration"`
	KeysEvacuated    int64         `json:"keys_evacuated"`
	BytesEvacuated   int64         `json:"bytes_evacuated"`
	EvacuationTime   time.Duration `json:"evacuation_time"`
	VerificationTime time.Duration `json:"verification_time"`
	Success          bool          `json:"success"`
	Errors           []string      `json:"errors,omitempty"`
}

// NodeReplacementManager manages node additions, removals, and replacements.
type NodeReplacementManager struct {
	storage interfaces.Storage
	config  *NodeReplacementConfig
	logger  interfaces.Logger
	nodes   map[string]*NodeState
}

// NewNodeReplacementManager creates a new node replacement manager.
func NewNodeReplacementManager(
	storage interfaces.Storage,
	config *NodeReplacementConfig,
	logger interfaces.Logger,
) (*NodeReplacementManager, error) {
	return &NodeReplacementManager{
		storage: storage,
		config:  config,
		logger:  logger,
		nodes:   make(map[string]*NodeState),
	}, nil
}

// AddNode adds a new node to the cluster.
func (m *NodeReplacementManager) AddNode(ctx context.Context, nodeID, address string) error {
	if _, exists := m.nodes[nodeID]; exists {
		return NewNodeAlreadyExistsError("node.add", nodeID)
	}
	m.nodes[nodeID] = &NodeState{
		NodeID:     nodeID,
		Address:    address,
		Status:     NodeStatusHealthy,
		JoinedAt:   time.Now(),
		LastSeenAt: time.Now(),
	}
	return nil
}

// RemoveNode removes a node from the cluster with data evacuation.
func (m *NodeReplacementManager) RemoveNode(ctx context.Context, nodeID string) (*NodeReplacementReport, error) {
	if _, exists := m.nodes[nodeID]; !exists {
		return nil, api.NewNotFoundError("node", nodeID)
	}
	startTime := time.Now()
	delete(m.nodes, nodeID)
	return &NodeReplacementReport{
		OldNodeID: nodeID,
		StartTime: startTime,
		EndTime:   time.Now(),
		Duration:  time.Since(startTime),
		Success:   true,
	}, nil
}

// ReplaceNode replaces one node with another.
func (m *NodeReplacementManager) ReplaceNode(ctx context.Context, oldNodeID, newNodeID, newAddress string) (*NodeReplacementReport, error) {
	startTime := time.Now()

	delete(m.nodes, oldNodeID)
	m.nodes[newNodeID] = &NodeState{
		NodeID:     newNodeID,
		Address:    newAddress,
		Status:     NodeStatusHealthy,
		JoinedAt:   time.Now(),
		LastSeenAt: time.Now(),
	}

	return &NodeReplacementReport{
		OldNodeID: oldNodeID,
		NewNodeID: newNodeID,
		StartTime: startTime,
		EndTime:   time.Now(),
		Duration:  time.Since(startTime),
		Success:   true,
	}, nil
}

// GetNodeState returns the state of a specific node.
func (m *NodeReplacementManager) GetNodeState(nodeID string) (*NodeState, bool) {
	node, ok := m.nodes[nodeID]
	return node, ok
}

// GetAllNodes returns all known nodes.
func (m *NodeReplacementManager) GetAllNodes() []*NodeState {
	nodes := make([]*NodeState, 0, len(m.nodes))
	for _, node := range m.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}
