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

package routing

import (
	"sync"
)

// Router maps (entityType, id) to a shard ID.
// It supports explicit entity-type pinning and a simple shard list fallback.
type Router interface {
	// Route returns the shard ID for the given entity type and id.
	Route(entityType string, id uint64) uint64

	// SetEntityShard pins an entity type to a specific shard ID.
	SetEntityShard(entityType string, shardID uint64)

	// SetShards sets the list of available shard IDs used for fallback routing.
	// When id > 0 and the entity is not pinned, a simple modulo is used over the list.
	// When id == 0 and not pinned, the first shard in the list is used.
	SetShards(shardIDs []uint64)
}

type router struct {
	mu        sync.RWMutex
	entityMap map[string]uint64
	shardIDs  []uint64
}

// New creates a Router with a default single shard [1] if none provided.
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
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entityMap[entityType] = shardID
}

func (r *router) SetShards(shardIDs []uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(shardIDs) == 0 {
		r.shardIDs = []uint64{1}
		return
	}
	r.shardIDs = append([]uint64(nil), shardIDs...)
}

func (r *router) Route(entityType string, id uint64) uint64 {
	r.mu.RLock()
	// Pinned entity type wins
	if shard, ok := r.entityMap[entityType]; ok {
		r.mu.RUnlock()
		return shard
	}
	// Copy shard list snapshot
	shards := r.shardIDs
	r.mu.RUnlock()

	if len(shards) == 0 {
		return 1
	}
	if id == 0 {
		// No ID provided, fall back to first shard deterministically
		return shards[0]
	}
	// Simple modulo over configured shard IDs for determinism
	// Modulo result is bounded by shard count (< 1000 typical)
	idx := int(id % uint64(len(shards))) //nolint:gosec // G115: modulo result bounded by len(shards)
	return shards[idx]
}
