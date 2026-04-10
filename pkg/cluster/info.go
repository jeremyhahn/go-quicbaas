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

package cluster

import (
	"context"
)

// ClusterInfo provides cluster membership information for load balancing
// and routing WebSocket connections to non-leader nodes.
//
// In a Raft-based system, the leader handles all write operations and must
// forward client requests. For read-heavy workloads and WebSocket connections,
// clients can connect to follower nodes to distribute load and reduce latency.
//
// This interface allows load balancers and routing components to discover
// which nodes are available for handling client connections.
type ClusterInfo interface {
	// NonLeaderNodes returns all healthy non-leader nodes for load balancer routing.
	// WebSocket clients should connect to these nodes for push notifications.
	// Returns an empty slice if no non-leader nodes are available.
	// Returns an error if the cluster state cannot be determined.
	NonLeaderNodes(ctx context.Context) ([]NodeInfo, error)

	// AllNodes returns all nodes in the cluster, including both healthy and unhealthy nodes.
	// Returns an error if the cluster state cannot be determined.
	AllNodes(ctx context.Context) ([]NodeInfo, error)

	// Leader returns the current cluster leader.
	// Returns ErrNoLeader if no leader is currently elected.
	Leader(ctx context.Context) (*NodeInfo, error)

	// LocalNode returns information about the local node.
	// This always returns a valid NodeInfo as the local node is always known.
	LocalNode() *NodeInfo

	// IsLeader returns true if the local node is the current Raft leader.
	IsLeader() bool
}
