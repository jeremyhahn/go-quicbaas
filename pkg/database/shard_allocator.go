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

package database

import (
	"sync/atomic"

	"github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// ShardAllocator provides monotonically increasing shard IDs for tenant
// databases. It uses atomic operations for lock-free allocation.
type ShardAllocator struct {
	counter atomic.Uint64
}

// NewShardAllocator creates a new ShardAllocator starting from the given
// initial value. The initial value must be >= shards.TenantShardMin.
func NewShardAllocator(initial uint64) (*ShardAllocator, error) {
	if initial < shards.TenantShardMin {
		initial = shards.TenantShardMin
	}
	a := &ShardAllocator{}
	a.counter.Store(initial)
	return a, nil
}

// Allocate returns the next available shard ID. The returned ID is
// guaranteed to be >= shards.TenantShardMin and unique across callers.
func (a *ShardAllocator) Allocate() uint64 {
	return a.counter.Add(1)
}

// Current returns the current counter value (the last allocated shard ID).
func (a *ShardAllocator) Current() uint64 {
	return a.counter.Load()
}
