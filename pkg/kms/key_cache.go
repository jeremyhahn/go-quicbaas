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

package kms

import (
	"crypto/rand"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DefaultCacheTTL is the default time-to-live for cached keys.
	DefaultCacheTTL = 5 * time.Minute

	// DefaultMaxTTL is the maximum allowed TTL for any cached key.
	DefaultMaxTTL = 1 * time.Hour

	// DefaultMaxEntries is the maximum number of keys to cache.
	DefaultMaxEntries = 1000

	// DefaultCleanupInterval is how often expired entries are purged.
	DefaultCleanupInterval = 1 * time.Minute
)

var (
	// ErrCacheClosed is returned when operating on a closed cache.
	ErrCacheClosed = errors.New("key cache is closed")

	// ErrCacheFull is returned when cache has reached max entries.
	ErrCacheFull = errors.New("key cache is full")

	// ErrTTLTooLarge is returned when TTL exceeds MaxTTL.
	ErrTTLTooLarge = errors.New("TTL exceeds maximum allowed")

	// ErrKeyTooLarge is returned when key material is too large.
	ErrKeyTooLarge = errors.New("key material too large")
)

// KeyTooLargeError is returned when key material exceeds the configured limit.
type KeyTooLargeError struct {
	Size  int
	Limit int
}

// Error implements the error interface.
func (e *KeyTooLargeError) Error() string {
	return "key material too large: " + strconv.Itoa(e.Size) + " bytes exceeds limit of " + strconv.Itoa(e.Limit)
}

// Is supports errors.Is matching against ErrKeyTooLarge.
func (e *KeyTooLargeError) Is(target error) bool {
	return target == ErrKeyTooLarge
}

// TTLTooLargeError is returned when a TTL exceeds the maximum allowed value.
type TTLTooLargeError struct {
	TTL    time.Duration
	MaxTTL time.Duration
}

// Error implements the error interface.
func (e *TTLTooLargeError) Error() string {
	return "TTL exceeds maximum allowed: " + e.TTL.String() + " exceeds " + e.MaxTTL.String()
}

// Is supports errors.Is matching against ErrTTLTooLarge.
func (e *TTLTooLargeError) Is(target error) bool {
	return target == ErrTTLTooLarge
}

// KeyCacheOptions configures the key cache behavior.
type KeyCacheOptions struct {
	// DefaultTTL is the default time-to-live for cached keys.
	// If zero, DefaultCacheTTL is used.
	DefaultTTL time.Duration

	// MaxTTL is the maximum allowed TTL for any cached key.
	// If zero, DefaultMaxTTL is used.
	MaxTTL time.Duration

	// MaxEntries is the maximum number of keys to cache.
	// If zero, DefaultMaxEntries is used.
	MaxEntries int

	// CleanupInterval is how often expired entries are purged.
	// If zero, DefaultCleanupInterval is used.
	CleanupInterval time.Duration

	// MaxKeySize limits the size of individual key material.
	// If zero, no limit is enforced (not recommended).
	MaxKeySize int
}

// cacheEntry represents a cached key with expiration.
//
// material, expiresAt and size are immutable for the lifetime of the entry
// with one exception: material is zeroed when the entry is evicted, and that
// only ever happens while KeyCache.mu is held for writing. Readers therefore
// copy material under the read lock and never observe an eviction in
// progress. accessedAt is the only field mutated in place, so it is a Unix
// nanosecond timestamp updated atomically - that keeps Get on the read lock
// instead of forcing every cache hit to serialize on the write lock.
type cacheEntry struct {
	material   []byte
	expiresAt  time.Time
	size       int
	accessedAt atomic.Int64
}

// zeroMemory overwrites the key material with random data then zeros.
// Must be called with KeyCache.mu held for writing.
func (e *cacheEntry) zeroMemory() {
	if len(e.material) > 0 {
		// First pass: fill with random data
		_, _ = rand.Read(e.material)

		// Second pass: zero out
		for i := range e.material {
			e.material[i] = 0
		}

		// Clear the slice
		e.material = nil
	}
}

// CacheStats provides statistics about cache usage.
type CacheStats struct {
	// Hits is the number of successful cache retrievals.
	Hits uint64

	// Misses is the number of failed cache retrievals.
	Misses uint64

	// Entries is the current number of cached keys.
	Entries int

	// BytesUsed is the total size of cached key material.
	BytesUsed int64

	// Evictions is the number of keys evicted (expired or LRU).
	Evictions uint64
}

// KeyCache provides secure, time-limited caching of key material.
// Keys are automatically evicted after their TTL expires.
// This is used to reduce calls to the underlying KMS backend.
type KeyCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	opts    KeyCacheOptions

	// Atomic counters for stats
	hits      atomic.Uint64
	misses    atomic.Uint64
	evictions atomic.Uint64

	// Cleanup goroutine management
	stopCleanup chan struct{}
	cleanupDone chan struct{}
	closed      atomic.Bool
}

// NewKeyCache creates a new key cache with the given options.
func NewKeyCache(opts *KeyCacheOptions) *KeyCache {
	if opts == nil {
		opts = &KeyCacheOptions{}
	}

	// Apply defaults
	if opts.DefaultTTL <= 0 {
		opts.DefaultTTL = DefaultCacheTTL
	}
	if opts.MaxTTL <= 0 {
		opts.MaxTTL = DefaultMaxTTL
	}
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = DefaultMaxEntries
	}
	if opts.CleanupInterval <= 0 {
		opts.CleanupInterval = DefaultCleanupInterval
	}

	kc := &KeyCache{
		entries:     make(map[string]*cacheEntry),
		opts:        *opts,
		stopCleanup: make(chan struct{}),
		cleanupDone: make(chan struct{}),
	}

	// Start background cleanup goroutine
	go kc.cleanupLoop()

	return kc
}

// Put stores key material in the cache with the specified TTL.
// If ttl is zero, DefaultTTL is used.
// The material is copied to prevent external mutation.
func (kc *KeyCache) Put(keyID string, material []byte, ttl time.Duration) error {
	if kc.closed.Load() {
		return ErrCacheClosed
	}

	if keyID == "" {
		return ErrKeyIDEmpty
	}

	if len(material) == 0 {
		return ErrMaterialEmpty
	}

	// Check key size limit
	if kc.opts.MaxKeySize > 0 && len(material) > kc.opts.MaxKeySize {
		return &KeyTooLargeError{Size: len(material), Limit: kc.opts.MaxKeySize}
	}

	// Use default TTL if not specified
	if ttl <= 0 {
		ttl = kc.opts.DefaultTTL
	}

	// Enforce max TTL
	if ttl > kc.opts.MaxTTL {
		return &TTLTooLargeError{TTL: ttl, MaxTTL: kc.opts.MaxTTL}
	}

	// Copy material to prevent external mutation
	materialCopy := make([]byte, len(material))
	copy(materialCopy, material)

	kc.mu.Lock()
	defer kc.mu.Unlock()

	// Check if cache is full (and this is a new entry)
	if _, exists := kc.entries[keyID]; !exists {
		if len(kc.entries) >= kc.opts.MaxEntries {
			// Evict least recently used entry
			kc.evictLRU()
		}
	} else {
		// Replacing existing entry - zero old material
		if old := kc.entries[keyID]; old != nil {
			old.zeroMemory()
		}
	}

	now := time.Now()
	entry := &cacheEntry{
		material:  materialCopy,
		expiresAt: now.Add(ttl),
		size:      len(materialCopy),
	}
	entry.accessedAt.Store(now.UnixNano())
	kc.entries[keyID] = entry

	return nil
}

// Get retrieves key material from the cache.
// Returns a copy of the material and true if found and not expired.
// Returns nil and false if not found or expired.
func (kc *KeyCache) Get(keyID string) ([]byte, bool) {
	if kc.closed.Load() {
		return nil, false
	}

	// The read lock is held across the copy. Eviction zeroes key material
	// under the write lock, so holding the read lock for the whole lookup is
	// what stops a reader from copying material that is being wiped out from
	// under it and reporting the result as a cache hit.
	kc.mu.RLock()

	entry, exists := kc.entries[keyID]
	if !exists {
		kc.mu.RUnlock()
		kc.misses.Add(1)
		return nil, false
	}

	// Check expiration
	now := time.Now()
	if now.After(entry.expiresAt) {
		kc.mu.RUnlock()
		kc.misses.Add(1)
		// Expired - remove it
		kc.Delete(keyID)
		return nil, false
	}

	// Update access time for LRU ordering
	entry.accessedAt.Store(now.UnixNano())

	// Return a copy to prevent external mutation
	materialCopy := make([]byte, len(entry.material))
	copy(materialCopy, entry.material)

	kc.mu.RUnlock()

	kc.hits.Add(1)
	return materialCopy, true
}

// Delete removes a key from the cache and zeros its memory.
func (kc *KeyCache) Delete(keyID string) {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	if entry, exists := kc.entries[keyID]; exists {
		entry.zeroMemory()
		delete(kc.entries, keyID)
		kc.evictions.Add(1)
	}
}

// Clear removes all keys from the cache and zeros their memory.
func (kc *KeyCache) Clear() {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	for keyID, entry := range kc.entries {
		entry.zeroMemory()
		delete(kc.entries, keyID)
		kc.evictions.Add(1)
	}
}

// Size returns the current number of cached keys.
func (kc *KeyCache) Size() int {
	kc.mu.RLock()
	defer kc.mu.RUnlock()
	return len(kc.entries)
}

// Stats returns cache statistics.
func (kc *KeyCache) Stats() CacheStats {
	kc.mu.RLock()
	defer kc.mu.RUnlock()

	var bytesUsed int64
	for _, entry := range kc.entries {
		bytesUsed += int64(entry.size)
	}

	return CacheStats{
		Hits:      kc.hits.Load(),
		Misses:    kc.misses.Load(),
		Entries:   len(kc.entries),
		BytesUsed: bytesUsed,
		Evictions: kc.evictions.Load(),
	}
}

// Close stops the cleanup goroutine and clears all cached keys.
// After Close, the cache cannot be used.
func (kc *KeyCache) Close() error {
	if kc.closed.Swap(true) {
		return ErrCacheClosed
	}

	// Stop cleanup goroutine
	close(kc.stopCleanup)

	// Wait for cleanup to finish
	<-kc.cleanupDone

	// Clear all entries
	kc.Clear()

	return nil
}

// cleanupLoop runs in the background and periodically removes expired entries.
func (kc *KeyCache) cleanupLoop() {
	defer close(kc.cleanupDone)

	ticker := time.NewTicker(kc.opts.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-kc.stopCleanup:
			return
		case <-ticker.C:
			kc.cleanupExpired()
		}
	}
}

// cleanupExpired removes all expired entries from the cache.
func (kc *KeyCache) cleanupExpired() {
	now := time.Now()

	kc.mu.Lock()
	defer kc.mu.Unlock()

	for keyID, entry := range kc.entries {
		if now.After(entry.expiresAt) {
			entry.zeroMemory()
			delete(kc.entries, keyID)
			kc.evictions.Add(1)
		}
	}
}

// evictLRU evicts the least recently used entry.
// Must be called with lock held.
func (kc *KeyCache) evictLRU() {
	if len(kc.entries) == 0 {
		return
	}

	var (
		oldestKey  string
		oldestTime int64
	)

	// Find least recently accessed entry
	first := true
	for keyID, entry := range kc.entries {
		accessedAt := entry.accessedAt.Load()
		if first || accessedAt < oldestTime {
			oldestKey = keyID
			oldestTime = accessedAt
			first = false
		}
	}

	// Evict it
	if entry, exists := kc.entries[oldestKey]; exists {
		entry.zeroMemory()
		delete(kc.entries, oldestKey)
		kc.evictions.Add(1)
	}
}
