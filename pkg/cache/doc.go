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

// Package cache provides an in-memory LRU caching system for QuicBaaS.
//
// The cache package implements a single-level in-memory LRU (Least Recently Used) cache
// for fast, local access. This is a READ-ONLY cache that does not persist to storage.
//
// # Architecture
//
// The cache manager provides thread-safe access to an in-memory LRU cache with
// TTL-based expiration and automatic cleanup of expired entries.
//
//	┌─────────────┐
//	│   Client    │
//	└──────┬──────┘
//	       │
//	       ▼
//	┌─────────────────────────┐
//	│   Cache Manager         │
//	│  - Metrics tracking     │
//	│  - TTL management       │
//	│  - Cleanup goroutine    │
//	└──────┬──────────────────┘
//	       │
//	       ▼
//	┌──────────────┐
//	│  L1 Cache    │
//	│   (LRU)      │
//	└──────────────┘
//
// # Usage
//
// Basic usage:
//
//	config := cache.DefaultConfig()
//	logger := logger.New(logger.LogConfig{})
//
//	cm, err := cache.NewManager(config, logger)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer cm.Close()
//
//	ctx := context.Background()
//
//	// Set a value (cache-only, does not persist)
//	err = cm.Set(ctx, "user:123", []byte(`{"name":"John"}`))
//
//	// Get a value
//	value, err := cm.Get(ctx, "user:123")
//
//	// Delete a value
//	err = cm.Delete(ctx, "user:123")
//
// # Features
//
// LRU Eviction:
// The L1 cache uses a doubly-linked list to maintain LRU order, providing O(1)
// eviction when the cache reaches capacity.
//
// TTL Expiration:
// The cache supports time-to-live (TTL) for automatic expiration.
// A background goroutine periodically cleans expired entries.
//
// Metrics:
// The cache manager tracks hit/miss rates, evictions, and sets using
// thread-safe atomic operations.
//
// READ-ONLY:
// This cache is for read optimization only. Set() operations populate the cache
// but do NOT persist data to storage. Always write to storage separately.
//
// # Invalidation Strategies
//
// The cache supports multiple invalidation strategies:
//
//   - TTL-based: Entries expire after configured time
//   - Manual: Explicit Delete() calls
//   - Automatic cleanup: Background goroutine removes expired entries
//
// # Performance Characteristics
//
// L1 Cache (LRU):
//   - Get: O(1) average case
//   - Set: O(1) average case
//   - Delete: O(1) average case
//   - Memory: O(capacity)
//
// # Thread Safety
//
// All cache operations are thread-safe and can be called concurrently from
// multiple goroutines. The implementation uses:
//
//   - sync.RWMutex for cache operations
//   - atomic operations for metrics
//   - Defensive copying of byte slices
//
// # Best Practices
//
// 1. Size cache based on working set:
//   - Too small: High miss rate, frequent evictions
//   - Too large: Memory pressure, slower GC
//
// 2. Set appropriate TTLs:
//   - Consider data volatility when setting TTLs
//   - Shorter TTLs for frequently changing data
//
// 3. Monitor metrics:
//   - Track hit rates to tune cache size
//   - Monitor eviction rate to detect capacity issues
//
// 4. Remember it's READ-ONLY:
//   - Cache Set() does NOT persist to storage
//   - Always write to storage layer for durability
//
// # Example: Monitoring
//
//	stats := cm.Stats()
//	fmt.Printf("L1 hit rate: %.2f%%\n", stats.L1HitRate*100)
//	fmt.Printf("L1 size: %d/%d\n", stats.L1Size, stats.L1Capacity)
//
//	metrics := cm.GetMetrics()
//	fmt.Printf("L1 hits: %d\n", metrics.L1Hits())
//	fmt.Printf("L1 misses: %d\n", metrics.L1Misses())
//	fmt.Printf("L1 evictions: %d\n", metrics.Get("l1_evictions"))
//
// # Integration with QuicBaaS
//
// The cache manager can be integrated with QuicBaaS's storage layer to provide
// transparent read caching:
//
//	type CachedStorage struct {
//	    storage storage.Storage
//	    cache   *cache.CacheManager
//	}
//
//	func (s *CachedStorage) Get(key []byte) ([]byte, error) {
//	    // Try cache first
//	    if value, err := s.cache.Get(ctx, string(key)); err == nil {
//	        return value, nil
//	    }
//
//	    // Cache miss - query storage
//	    value, err := s.storage.Get(key)
//	    if err != nil {
//	        return nil, err
//	    }
//
//	    // Populate cache (read-only, does not affect storage)
//	    s.cache.Set(ctx, string(key), value)
//	    return value, nil
//	}
//
//	func (s *CachedStorage) Put(key, value []byte) error {
//	    // Write to storage FIRST (cache is read-only)
//	    if err := s.storage.Put(key, value); err != nil {
//	        return err
//	    }
//
//	    // Populate cache for future reads
//	    s.cache.Set(ctx, string(key), value)
//	    return nil
//	}
package cache
