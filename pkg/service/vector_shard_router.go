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

// Package service provides the unified service facade for QuicBaaS.
// This file implements namespace-to-shard routing for distributed vector search.
package service

import (
	"sync"
	"sync/atomic"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// DistributedShardInfo describes a shard that holds vector namespace data
// for distributed search coordination.
type DistributedShardInfo struct {
	// ShardID is the unique shard identifier.
	ShardID uint64

	// NodeID identifies the node that owns this shard.
	NodeID string

	// Address is the network address for RPC to the shard owner.
	Address string
}

// RoutingMode determines how namespaces are distributed across shards.
type RoutingMode int

const (
	// RoutingSingleShard routes all vectors in a namespace to one shard.
	RoutingSingleShard RoutingMode = iota

	// RoutingMultiShard distributes a namespace across multiple shards.
	RoutingMultiShard
)

// shardList wraps a slice of shards in a pointer-comparable struct
// for use with atomic.Pointer CAS operations.
type shardList struct {
	shards []DistributedShardInfo
}

// VectorShardRouter maps namespaces to the shards that hold their data.
// All operations are lock-free using sync.Map with atomic.Pointer for
// concurrent access without mutexes.
type VectorShardRouter struct {
	// shardMap maps namespace (string) -> *atomic.Pointer[shardList]
	shardMap sync.Map
	mode     RoutingMode
}

// NamespaceNotRoutedError indicates that no shards are registered for the namespace.
type NamespaceNotRoutedError struct {
	Namespace string
}

// Error implements the error interface.
func (e *NamespaceNotRoutedError) Error() string {
	return "no shards registered for namespace: " + e.Namespace
}

// NewNamespaceNotRoutedError creates a QBError for an unrouted namespace.
func NewNamespaceNotRoutedError(op, namespace string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   op,
		Err:  &NamespaceNotRoutedError{Namespace: namespace},
	}
}

// NewVectorShardRouter creates a new VectorShardRouter with the specified routing mode.
func NewVectorShardRouter(mode RoutingMode) *VectorShardRouter {
	return &VectorShardRouter{
		mode: mode,
	}
}

// GetShards returns the shards holding data for the given namespace.
func (r *VectorShardRouter) GetShards(namespace string) ([]DistributedShardInfo, error) {
	val, ok := r.shardMap.Load(namespace)
	if !ok {
		return nil, NewNamespaceNotRoutedError("VectorShardRouter.GetShards", namespace)
	}
	ptr, ok := val.(*atomic.Pointer[shardList])
	if !ok {
		return nil, NewNamespaceNotRoutedError("VectorShardRouter.GetShards", namespace)
	}
	list := ptr.Load()
	if list == nil || len(list.shards) == 0 {
		return nil, NewNamespaceNotRoutedError("VectorShardRouter.GetShards", namespace)
	}
	// Return a copy to prevent callers from mutating internal state.
	result := make([]DistributedShardInfo, len(list.shards))
	copy(result, list.shards)
	return result, nil
}

// RegisterShard registers a shard for a namespace. If the shard is already
// registered (by ShardID), this is a no-op.
func (r *VectorShardRouter) RegisterShard(namespace string, shard DistributedShardInfo) {
	// Try to load or create an atomic pointer for this namespace.
	val, loaded := r.shardMap.Load(namespace)
	if !loaded {
		ptr := &atomic.Pointer[shardList]{}
		ptr.Store(&shardList{shards: []DistributedShardInfo{shard}})
		val, loaded = r.shardMap.LoadOrStore(namespace, ptr)
		if !loaded {
			return // Successfully stored new entry.
		}
	}

	// Entry exists; CAS loop to append the shard.
	ptr, ok := val.(*atomic.Pointer[shardList])
	if !ok {
		return
	}
	for {
		current := ptr.Load()

		// Check for duplicate.
		for _, s := range current.shards {
			if s.ShardID == shard.ShardID {
				return
			}
		}

		// Build new list with the shard appended.
		updated := make([]DistributedShardInfo, len(current.shards)+1)
		copy(updated, current.shards)
		updated[len(current.shards)] = shard
		newList := &shardList{shards: updated}

		if ptr.CompareAndSwap(current, newList) {
			return
		}
		// CAS failed; retry.
	}
}

// RemoveShard removes a shard from a namespace by ShardID.
func (r *VectorShardRouter) RemoveShard(namespace string, shardID uint64) {
	val, ok := r.shardMap.Load(namespace)
	if !ok {
		return
	}

	ptr, ok := val.(*atomic.Pointer[shardList])
	if !ok {
		return
	}
	for {
		current := ptr.Load()
		if current == nil {
			return
		}

		idx := -1
		for i, s := range current.shards {
			if s.ShardID == shardID {
				idx = i
				break
			}
		}
		if idx == -1 {
			return
		}

		if len(current.shards) == 1 {
			// Last shard; remove the namespace entry entirely.
			r.shardMap.Delete(namespace)
			return
		}

		updated := make([]DistributedShardInfo, 0, len(current.shards)-1)
		updated = append(updated, current.shards[:idx]...)
		updated = append(updated, current.shards[idx+1:]...)
		newList := &shardList{shards: updated}

		if ptr.CompareAndSwap(current, newList) {
			return
		}
		// CAS failed; retry.
	}
}

// IsDistributed returns true if the namespace spans multiple shards.
func (r *VectorShardRouter) IsDistributed(namespace string) bool {
	val, ok := r.shardMap.Load(namespace)
	if !ok {
		return false
	}
	ptr, ok := val.(*atomic.Pointer[shardList])
	if !ok {
		return false
	}
	list := ptr.Load()
	return list != nil && len(list.shards) > 1
}

// Mode returns the current routing mode.
func (r *VectorShardRouter) Mode() RoutingMode {
	return r.mode
}
