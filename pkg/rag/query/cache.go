//go:build rag

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

package query

import (
	"container/list"
	"sync"
	"time"
)

// Cache provides LRU caching with TTL for query results.
type Cache interface {
	// Get retrieves a cached response.
	Get(key string) (*Response, bool)

	// Set stores a response in cache with TTL.
	Set(key string, value *Response, ttl time.Duration)

	// Clear removes all cached entries.
	Clear()

	// Stats returns cache statistics.
	Stats() CacheStats
}

// CacheStats contains cache performance statistics.
type CacheStats struct {
	// Hits is the number of cache hits.
	Hits int64

	// Misses is the number of cache misses.
	Misses int64

	// Size is the current cache size.
	Size int

	// HitRate is the cache hit ratio (0-1).
	HitRate float64
}

// lruCache implements an LRU cache with TTL.
type lruCache struct {
	mu       sync.RWMutex
	capacity int
	items    map[string]*list.Element
	eviction *list.List
	hits     int64
	misses   int64
}

// cacheEntry represents a cached item.
type cacheEntry struct {
	key      string
	value    *Response
	expireAt time.Time
}

// NewCache creates a new LRU cache with the given capacity.
func NewCache(capacity int) Cache {
	return &lruCache{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		eviction: list.New(),
	}
}

// Get retrieves a cached response.
func (c *lruCache) Get(key string) (*Response, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		c.misses++
		return nil, false
	}

	entry, ok := elem.Value.(*cacheEntry)
	if !ok {
		c.misses++
		return nil, false
	}

	// Check if expired
	if time.Now().After(entry.expireAt) {
		c.removeElement(elem)
		c.misses++
		return nil, false
	}

	// Move to front (most recently used)
	c.eviction.MoveToFront(elem)
	c.hits++

	return entry.value, true
}

// Set stores a response in cache with TTL.
func (c *lruCache) Set(key string, value *Response, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if key exists
	if elem, ok := c.items[key]; ok {
		c.eviction.MoveToFront(elem)
		entry, ok := elem.Value.(*cacheEntry)
		if ok {
			entry.value = value
			entry.expireAt = time.Now().Add(ttl)
		}
		return
	}

	// Add new entry
	entry := &cacheEntry{
		key:      key,
		value:    value,
		expireAt: time.Now().Add(ttl),
	}

	elem := c.eviction.PushFront(entry)
	c.items[key] = elem

	// Evict if over capacity
	if len(c.items) > c.capacity {
		c.evictOldest()
	}
}

// Clear removes all cached entries.
func (c *lruCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*list.Element)
	c.eviction.Init()
	c.hits = 0
	c.misses = 0
}

// Stats returns cache statistics.
func (c *lruCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.hits + c.misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(c.hits) / float64(total)
	}

	return CacheStats{
		Hits:    c.hits,
		Misses:  c.misses,
		Size:    len(c.items),
		HitRate: hitRate,
	}
}

// evictOldest removes the least recently used item.
func (c *lruCache) evictOldest() {
	elem := c.eviction.Back()
	if elem != nil {
		c.removeElement(elem)
	}
}

// removeElement removes an element from the cache.
func (c *lruCache) removeElement(elem *list.Element) {
	entry, ok := elem.Value.(*cacheEntry)
	if !ok {
		return
	}
	delete(c.items, entry.key)
	c.eviction.Remove(elem)
}

// CleanExpired removes expired entries from the cache.
// This should be called periodically to prevent memory leaks.
func (c *lruCache) CleanExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	removed := 0

	var next *list.Element
	for elem := c.eviction.Front(); elem != nil; elem = next {
		next = elem.Next()
		entry, ok := elem.Value.(*cacheEntry)
		if !ok {
			continue
		}

		if now.After(entry.expireAt) {
			c.removeElement(elem)
			removed++
		}
	}

	return removed
}
