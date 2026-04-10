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

// Package cluster provides cluster membership information for load balancing
// and routing WebSocket connections to non-leader nodes.
package cluster

import (
	"time"
)

// NodeInfo represents a cluster node with its current state and connectivity information.
type NodeInfo struct {
	// NodeID is the unique identifier of this node within the cluster.
	NodeID uint64 `json:"node_id"`

	// Address is the client-facing address for this node (e.g., "host:8443").
	Address string `json:"address"`

	// RaftAddress is the internal Raft address for inter-node communication.
	RaftAddress string `json:"raft_address"`

	// IsLeader indicates whether this node is currently the Raft leader.
	IsLeader bool `json:"is_leader"`

	// IsHealthy indicates whether this node is currently healthy and accepting connections.
	IsHealthy bool `json:"is_healthy"`

	// LastSeen is the timestamp of the last health check or heartbeat from this node.
	LastSeen time.Time `json:"last_seen"`
}

// IsAvailable returns true if the node is healthy and can accept client connections.
func (n *NodeInfo) IsAvailable() bool {
	return n.IsHealthy && n.Address != ""
}

// NodeList represents a collection of cluster nodes.
type NodeList []NodeInfo

// Filter returns a new NodeList containing only nodes that match the predicate.
func (nl NodeList) Filter(predicate func(NodeInfo) bool) NodeList {
	result := make(NodeList, 0, len(nl))
	for _, node := range nl {
		if predicate(node) {
			result = append(result, node)
		}
	}
	return result
}

// FindByID returns the node with the given ID, or nil if not found.
func (nl NodeList) FindByID(nodeID uint64) *NodeInfo {
	for i := range nl {
		if nl[i].NodeID == nodeID {
			return &nl[i]
		}
	}
	return nil
}

// Healthy returns only healthy nodes from the list.
func (nl NodeList) Healthy() NodeList {
	return nl.Filter(func(n NodeInfo) bool {
		return n.IsHealthy
	})
}

// NonLeaders returns only non-leader nodes from the list.
func (nl NodeList) NonLeaders() NodeList {
	return nl.Filter(func(n NodeInfo) bool {
		return !n.IsLeader
	})
}

// Leaders returns only leader nodes from the list.
func (nl NodeList) Leaders() NodeList {
	return nl.Filter(func(n NodeInfo) bool {
		return n.IsLeader
	})
}
