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

package membership

import "context"

// RaftNode is an interface for Raft node operations needed by membership manager.
// This interface is implemented by pkg/raft/node.RaftNode and allows for testing with mocks.
type RaftNode interface {
	// RequestAddNode requests adding a new replica to a shard.
	// The configChangeIndex should be obtained from GetShardRaftState
	// to ensure ordered config changes. Pass 0 to skip the ordering check.
	RequestAddNode(ctx context.Context, shardID, replicaID uint64, addr string, configChangeIndex uint64) error

	// RequestRemoveNode requests removing a replica from a shard.
	// The configChangeIndex should be obtained from GetShardRaftState.
	RequestRemoveNode(ctx context.Context, shardID, replicaID uint64, configChangeIndex uint64) error

	// GetLeaderID returns the current leader ID for the given shard,
	// whether a leader is known, and an error if the shard does not exist.
	GetLeaderID(shardID uint64) (uint64, bool, error)
}
