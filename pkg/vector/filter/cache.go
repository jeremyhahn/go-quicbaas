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

package filter

import (
	"sync"
	"sync/atomic"
	"time"
)

// cacheEntry holds a parsed expression and its last access time for LRU eviction.
type cacheEntry struct {
	expr       Expression
	accessTime atomic.Int64
}

// ExpressionCache provides a lock-free LRU cache for parsed filter expressions.
// It uses sync.Map for concurrent reads without locking and an atomic counter
// for tracking size.
type ExpressionCache struct {
	entries sync.Map
	size    atomic.Int64
	maxSize int
}

// NewExpressionCache creates a new expression cache with the given maximum size.
// The maxSize must be positive; if zero or negative, a default of 1024 is used.
func NewExpressionCache(maxSize int) *ExpressionCache {
	if maxSize <= 0 {
		maxSize = 1024
	}
	return &ExpressionCache{
		maxSize: maxSize,
	}
}

// Get retrieves a cached expression by its string key.
// Returns the expression and true on cache hit, or nil and false on miss.
func (c *ExpressionCache) Get(expression string) (Expression, bool) {
	val, ok := c.entries.Load(expression)
	if !ok {
		return nil, false
	}
	entry, ok := val.(*cacheEntry)
	if !ok {
		return nil, false
	}
	entry.accessTime.Store(time.Now().UnixNano())
	return entry.expr, true
}

// Put stores a parsed expression in the cache. If the cache is at capacity,
// the least recently accessed entries are evicted first.
func (c *ExpressionCache) Put(expression string, expr Expression) {
	// Check if already present (update access time only).
	if val, ok := c.entries.Load(expression); ok {
		entry, ok := val.(*cacheEntry)
		if !ok {
			return
		}
		entry.accessTime.Store(time.Now().UnixNano())
		return
	}

	// Evict if at capacity.
	if int(c.size.Load()) >= c.maxSize {
		c.evict()
	}

	entry := &cacheEntry{expr: expr}
	entry.accessTime.Store(time.Now().UnixNano())

	// Use LoadOrStore to handle concurrent insertion of the same key.
	if _, loaded := c.entries.LoadOrStore(expression, entry); !loaded {
		c.size.Add(1)
	}
}

// evict removes the least recently accessed entry from the cache.
func (c *ExpressionCache) evict() {
	var oldestKey interface{}
	oldestTime := maxInt64

	c.entries.Range(func(key, value interface{}) bool {
		entry, ok := value.(*cacheEntry)
		if !ok {
			return true
		}
		t := entry.accessTime.Load()
		if t < oldestTime {
			oldestTime = t
			oldestKey = key
		}
		return true
	})

	if oldestKey != nil {
		if _, loaded := c.entries.LoadAndDelete(oldestKey); loaded {
			c.size.Add(-1)
		}
	}
}

// Size returns the current number of entries in the cache.
func (c *ExpressionCache) Size() int {
	return int(c.size.Load())
}

// Clear removes all entries from the cache.
func (c *ExpressionCache) Clear() {
	c.entries.Range(func(key, _ interface{}) bool {
		c.entries.Delete(key)
		return true
	})
	c.size.Store(0)
}

const maxInt64 = int64(^uint64(0) >> 1)
