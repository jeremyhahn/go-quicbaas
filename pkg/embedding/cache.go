//go:build vector

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

package embedding

import (
	"bytes"
	"compress/gzip"
	"container/list"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// Cache is an LRU cache for embeddings with optional compression.
type Cache struct {
	capacity  int
	ttl       int
	compress  bool
	items     map[string]*cacheEntry
	evictList *list.List
	mu        sync.RWMutex
	hits      atomic.Uint64
	misses    atomic.Uint64
	evictions atomic.Uint64
}

type cacheEntry struct {
	key        string
	value      []byte // compressed or raw embedding data
	compressed bool
	element    *list.Element
	timestamp  time.Time
}

// NewCache creates a new LRU cache.
func NewCache(capacity, ttlSeconds int, compress bool) (*Cache, error) {
	if capacity <= 0 {
		return nil, api.NewEmbeddingCacheCapacityError("embedding.NewCache")
	}

	return &Cache{
		capacity:  capacity,
		ttl:       ttlSeconds,
		compress:  compress,
		items:     make(map[string]*cacheEntry),
		evictList: list.New(),
	}, nil
}

// Get retrieves an embedding from the cache.
func (c *Cache) Get(key string) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Hash the key for consistent lookup
	hashKey := c.hashKey(key)

	entry, exists := c.items[hashKey]
	if !exists {
		c.misses.Add(1)
		return nil, false
	}

	// Check TTL
	if c.ttl > 0 && time.Since(entry.timestamp) > time.Duration(c.ttl)*time.Second {
		c.removeElement(entry.element)
		c.misses.Add(1)
		return nil, false
	}

	// Move to front (most recently used)
	c.evictList.MoveToFront(entry.element)
	c.hits.Add(1)

	// Decompress if needed
	embedding, err := c.decodeValue(entry.value, entry.compressed)
	if err != nil {
		c.misses.Add(1)
		return nil, false
	}

	return embedding, true
}

// Put stores an embedding in the cache.
func (c *Cache) Put(key string, embedding []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	hashKey := c.hashKey(key)

	// Encode value
	value, compressed, err := c.encodeValue(embedding, c.compress)
	if err != nil {
		return
	}

	// Check if key exists
	if entry, exists := c.items[hashKey]; exists {
		entry.value = value
		entry.compressed = compressed
		entry.timestamp = time.Now()
		c.evictList.MoveToFront(entry.element)
		return
	}

	// Add new entry
	entry := &cacheEntry{
		key:        hashKey,
		value:      value,
		compressed: compressed,
		timestamp:  time.Now(),
	}

	element := c.evictList.PushFront(entry)
	entry.element = element
	c.items[hashKey] = entry

	// Evict oldest if over capacity
	if c.evictList.Len() > c.capacity {
		c.evictOldest()
	}
}

// Size returns the current number of items in the cache.
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.evictList.Len()
}

// Hits returns the number of cache hits.
func (c *Cache) Hits() uint64 {
	return c.hits.Load()
}

// Misses returns the number of cache misses.
func (c *Cache) Misses() uint64 {
	return c.misses.Load()
}

// Evictions returns the number of evictions.
func (c *Cache) Evictions() uint64 {
	return c.evictions.Load()
}

// Clear removes all items from the cache.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*cacheEntry)
	c.evictList = list.New()
}

// evictOldest removes the oldest item from the cache.
func (c *Cache) evictOldest() {
	element := c.evictList.Back()
	if element != nil {
		c.removeElement(element)
		c.evictions.Add(1)
	}
}

// removeElement removes a specific element from the cache.
func (c *Cache) removeElement(element *list.Element) {
	c.evictList.Remove(element)
	entry, ok := element.Value.(*cacheEntry)
	if !ok {
		return
	}
	delete(c.items, entry.key)
}

// hashKey creates a hash of the key for consistent lookup.
func (c *Cache) hashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", hash)
}

// encodeValue encodes a float32 slice to bytes with optional compression.
func (c *Cache) encodeValue(embedding []float32, compress bool) ([]byte, bool, error) {
	// Convert float32 slice to bytes
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, embedding); err != nil {
		return nil, false, err
	}

	data := buf.Bytes()

	// Compress if enabled
	if compress {
		compressed := new(bytes.Buffer)
		gzipWriter := gzip.NewWriter(compressed)
		if _, err := gzipWriter.Write(data); err != nil {
			return nil, false, err
		}
		if err := gzipWriter.Close(); err != nil {
			return nil, false, err
		}

		// Only use compression if it actually saves space
		if compressed.Len() < len(data) {
			return compressed.Bytes(), true, nil
		}
	}

	return data, false, nil
}

// decodeValue decodes bytes back to a float32 slice.
func (c *Cache) decodeValue(data []byte, compressed bool) ([]float32, error) {
	var buf *bytes.Buffer

	// Decompress if needed
	if compressed {
		gzipReader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer gzipReader.Close()

		decompressed, err := io.ReadAll(gzipReader)
		if err != nil {
			return nil, err
		}
		buf = bytes.NewBuffer(decompressed)
	} else {
		buf = bytes.NewBuffer(data)
	}

	// Decode to float32 slice
	var embedding []float32
	for buf.Len() > 0 {
		var f float32
		if err := binary.Read(buf, binary.LittleEndian, &f); err != nil {
			return nil, err
		}
		embedding = append(embedding, f)
	}

	return embedding, nil
}
