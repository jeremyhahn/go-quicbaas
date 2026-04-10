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
	"container/list"
	"sync"
)

// EvictCallback is called when an entry is evicted from the cache.
// The callback receives the evicted key and entry.
// Callbacks are invoked synchronously under lock, so they should be fast
// and delegate any expensive operations to background goroutines.
type EvictCallback func(key string, entry *Entry)

// LRUCache implements a thread-safe LRU (Least Recently Used) cache.
// It uses a doubly-linked list for O(1) eviction and a map for O(1) lookups.
type LRUCache struct {
	capacity      int
	items         map[string]*list.Element
	lruList       *list.List
	mu            sync.RWMutex
	evictionCount int64
	onEvict       EvictCallback
}

// lruEntry represents an entry in the LRU cache.
type lruEntry struct {
	key   string
	value *Entry
}

// NewLRUCache creates a new LRU cache with the given capacity.
func NewLRUCache(capacity int) *LRUCache {
	if capacity <= 0 {
		capacity = 100 // Default minimum capacity
	}

	return &LRUCache{
		capacity:      capacity,
		items:         make(map[string]*list.Element, capacity),
		lruList:       list.New(),
		evictionCount: 0,
	}
}

// SetEvictCallback sets the callback to be invoked when entries are evicted.
// The callback is invoked synchronously under lock, so it should delegate
// any expensive operations to background goroutines to avoid blocking.
func (c *LRUCache) SetEvictCallback(callback EvictCallback) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onEvict = callback
}

// Get retrieves a value from the cache and marks it as recently used.
func (c *LRUCache) Get(key string) (*Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, exists := c.items[key]
	if !exists {
		return nil, false
	}

	// Move to front (most recently used)
	c.lruList.MoveToFront(elem)

	entry, ok := elem.Value.(*lruEntry)
	if !ok {
		return nil, false
	}
	return entry.value, true
}

// Add adds or updates a value in the cache.
// If the cache is full, it evicts the least recently used item.
func (c *LRUCache) Add(key string, value *Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If key exists, update it
	if elem, exists := c.items[key]; exists {
		c.lruList.MoveToFront(elem)
		if entry, ok := elem.Value.(*lruEntry); ok {
			entry.value = value
		}
		return
	}

	// Add new entry
	entry := &lruEntry{
		key:   key,
		value: value,
	}

	elem := c.lruList.PushFront(entry)
	c.items[key] = elem

	// Evict if over capacity
	if c.lruList.Len() > c.capacity {
		c.evictOldest()
	}
}

// Remove removes a value from the cache.
func (c *LRUCache) Remove(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, exists := c.items[key]
	if !exists {
		return false
	}

	c.removeElement(elem)
	return true
}

// Clear removes all entries from the cache.
func (c *LRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*list.Element, c.capacity)
	c.lruList = list.New()
}

// Len returns the current number of items in the cache.
func (c *LRUCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lruList.Len()
}

// Cap returns the cache capacity.
func (c *LRUCache) Cap() int {
	return c.capacity
}

// Keys returns a slice of all keys in the cache.
// The order is not guaranteed.
func (c *LRUCache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(c.items))
	for k := range c.items {
		keys = append(keys, k)
	}
	return keys
}

// Peek retrieves a value without updating its recency.
func (c *LRUCache) Peek(key string) (*Entry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	elem, exists := c.items[key]
	if !exists {
		return nil, false
	}

	entry, ok := elem.Value.(*lruEntry)
	if !ok {
		return nil, false
	}
	return entry.value, true
}

// Contains checks if a key exists in the cache without updating recency.
func (c *LRUCache) Contains(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	_, exists := c.items[key]
	return exists
}

// GetOldest returns the oldest (least recently used) entry without removing it.
func (c *LRUCache) GetOldest() (string, *Entry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	elem := c.lruList.Back()
	if elem == nil {
		return "", nil, false
	}

	entry, ok := elem.Value.(*lruEntry)
	if !ok {
		return "", nil, false
	}
	return entry.key, entry.value, true
}

// EvictionCount returns the total number of evictions that have occurred.
func (c *LRUCache) EvictionCount() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.evictionCount
}

// evictOldest removes the least recently used item from the cache.
// Must be called with lock held.
func (c *LRUCache) evictOldest() {
	elem := c.lruList.Back()
	if elem != nil {
		c.removeElement(elem)
		c.evictionCount++
	}
}

// removeElement removes an element from the cache.
// Must be called with lock held.
func (c *LRUCache) removeElement(elem *list.Element) {
	c.lruList.Remove(elem)
	if entry, ok := elem.Value.(*lruEntry); ok {
		delete(c.items, entry.key)

		// Invoke eviction callback if set
		if c.onEvict != nil {
			c.onEvict(entry.key, entry.value)
		}
	}
}

// Resize changes the cache capacity.
// If the new capacity is smaller, it evicts the oldest entries.
func (c *LRUCache) Resize(newCapacity int) {
	if newCapacity <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.capacity = newCapacity

	// Evict oldest entries if over capacity
	for c.lruList.Len() > c.capacity {
		c.evictOldest()
	}
}
