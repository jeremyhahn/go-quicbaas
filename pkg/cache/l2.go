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

package cache

import (
	"context"
	"time"
)

// L2Cache defines the interface for a secondary cache layer.
// Implementations might include Redis, Memcached, or other distributed caches.
// All operations are context-aware for timeout and cancellation support.
type L2Cache interface {
	// Get retrieves a value from the L2 cache.
	// Returns NotFound error if key doesn't exist.
	Get(ctx context.Context, key string) ([]byte, error)

	// Set stores a value in the L2 cache with the specified TTL.
	// If TTL is 0, the entry should not expire.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete removes a value from the L2 cache.
	// Does not return an error if the key doesn't exist.
	Delete(ctx context.Context, key string) error

	// Ping checks if the L2 cache is accessible and healthy.
	Ping(ctx context.Context) error

	// Close releases resources associated with the L2 cache connection.
	Close() error
}
