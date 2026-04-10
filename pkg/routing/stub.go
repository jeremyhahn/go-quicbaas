//go:build !raft

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

package routing

// Router maps (entityType, id) to a shard ID.
// This is a stub implementation when Raft is not compiled.
type Router interface {
	// Route returns the shard ID for the given entity type and id.
	Route(entityType string, id uint64) uint64

	// SetEntityShard pins an entity type to a specific shard ID.
	SetEntityShard(entityType string, shardID uint64)

	// SetShards sets the list of available shard IDs used for fallback routing.
	SetShards(shardIDs []uint64)
}

type router struct {
	entityMap map[string]uint64
	shardIDs  []uint64
}

// New creates a Router with a default single shard [1].
// In stub mode, this provides basic single-shard routing.
func New() Router {
	return &router{
		entityMap: make(map[string]uint64),
		shardIDs:  []uint64{1},
	}
}

// NewWithShards creates a Router with the provided shard ID list.
func NewWithShards(shards []uint64) Router {
	r := &router{entityMap: make(map[string]uint64)}
	if len(shards) == 0 {
		r.shardIDs = []uint64{1}
	} else {
		r.shardIDs = append([]uint64(nil), shards...)
	}
	return r
}

func (r *router) SetEntityShard(entityType string, shardID uint64) {
	r.entityMap[entityType] = shardID
}

func (r *router) SetShards(shardIDs []uint64) {
	if len(shardIDs) == 0 {
		r.shardIDs = []uint64{1}
		return
	}
	r.shardIDs = append([]uint64(nil), shardIDs...)
}

func (r *router) Route(entityType string, id uint64) uint64 {
	// Pinned entity type wins
	if shard, ok := r.entityMap[entityType]; ok {
		return shard
	}

	if len(r.shardIDs) == 0 {
		return 1
	}
	if id == 0 {
		// No ID provided, fall back to first shard
		return r.shardIDs[0]
	}
	// Simple modulo over configured shard IDs.
	// The result of id % uint64(len) is always < len(r.shardIDs), so the
	// conversion to int is safe since slice length is always <= max int.
	idx := id % uint64(len(r.shardIDs)) //nolint:gosec // G115: len() is bounded by max int
	return r.shardIDs[idx]
}
