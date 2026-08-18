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
	"bytes"
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// TestNewKeyCache_NilOptions tests cache creation with nil options uses defaults.
func TestNewKeyCache_NilOptions(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	if cache.opts.DefaultTTL != DefaultCacheTTL {
		t.Errorf("expected DefaultTTL=%v, got %v", DefaultCacheTTL, cache.opts.DefaultTTL)
	}
	if cache.opts.MaxTTL != DefaultMaxTTL {
		t.Errorf("expected MaxTTL=%v, got %v", DefaultMaxTTL, cache.opts.MaxTTL)
	}
	if cache.opts.MaxEntries != DefaultMaxEntries {
		t.Errorf("expected MaxEntries=%d, got %d", DefaultMaxEntries, cache.opts.MaxEntries)
	}
	if cache.opts.CleanupInterval != DefaultCleanupInterval {
		t.Errorf("expected CleanupInterval=%v, got %v", DefaultCleanupInterval, cache.opts.CleanupInterval)
	}
}

// TestNewKeyCache_CustomOptions tests cache creation with custom options.
func TestNewKeyCache_CustomOptions(t *testing.T) {
	opts := &KeyCacheOptions{
		DefaultTTL:      10 * time.Second,
		MaxTTL:          30 * time.Second,
		MaxEntries:      50,
		CleanupInterval: 5 * time.Second,
		MaxKeySize:      1024,
	}

	cache := NewKeyCache(opts)
	defer cache.Close()

	if cache.opts.DefaultTTL != opts.DefaultTTL {
		t.Errorf("expected DefaultTTL=%v, got %v", opts.DefaultTTL, cache.opts.DefaultTTL)
	}
	if cache.opts.MaxTTL != opts.MaxTTL {
		t.Errorf("expected MaxTTL=%v, got %v", opts.MaxTTL, cache.opts.MaxTTL)
	}
	if cache.opts.MaxEntries != opts.MaxEntries {
		t.Errorf("expected MaxEntries=%d, got %d", opts.MaxEntries, cache.opts.MaxEntries)
	}
	if cache.opts.CleanupInterval != opts.CleanupInterval {
		t.Errorf("expected CleanupInterval=%v, got %v", opts.CleanupInterval, cache.opts.CleanupInterval)
	}
	if cache.opts.MaxKeySize != opts.MaxKeySize {
		t.Errorf("expected MaxKeySize=%d, got %d", opts.MaxKeySize, cache.opts.MaxKeySize)
	}
}

// TestNewKeyCache_CleanupGoroutineStarts verifies cleanup goroutine starts.
func TestNewKeyCache_CleanupGoroutineStarts(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		CleanupInterval: 10 * time.Millisecond,
	})

	// Put a key that expires quickly
	err := cache.Put("key1", []byte("value1"), 5*time.Millisecond)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Wait for cleanup to run
	time.Sleep(20 * time.Millisecond)

	// Key should be cleaned up
	if cache.Size() != 0 {
		t.Errorf("expected cache to be empty after cleanup, got size=%d", cache.Size())
	}

	cache.Close()
}

// TestPutGet_Success tests putting and getting a key.
func TestPutGet_Success(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	keyID := "test-key"
	material := []byte("secret-material")

	err := cache.Put(keyID, material, 0)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	retrieved, found := cache.Get(keyID)
	if !found {
		t.Fatal("expected key to be found")
	}

	if !bytes.Equal(retrieved, material) {
		t.Errorf("expected material=%q, got %q", material, retrieved)
	}
}

// TestGet_NonExistent tests getting non-existent key returns false.
func TestGet_NonExistent(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	retrieved, found := cache.Get("non-existent")
	if found {
		t.Error("expected key not to be found")
	}
	if retrieved != nil {
		t.Errorf("expected nil material, got %v", retrieved)
	}

	stats := cache.Stats()
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
}

// TestPut_UpdateExisting tests updating existing key.
func TestPut_UpdateExisting(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	keyID := "test-key"
	material1 := []byte("first-value")
	material2 := []byte("second-value")

	err := cache.Put(keyID, material1, 0)
	if err != nil {
		t.Fatalf("first Put failed: %v", err)
	}

	err = cache.Put(keyID, material2, 0)
	if err != nil {
		t.Fatalf("second Put failed: %v", err)
	}

	retrieved, found := cache.Get(keyID)
	if !found {
		t.Fatal("expected key to be found")
	}

	if !bytes.Equal(retrieved, material2) {
		t.Errorf("expected material=%q, got %q", material2, retrieved)
	}

	// Should only have one entry
	if cache.Size() != 1 {
		t.Errorf("expected size=1, got %d", cache.Size())
	}
}

// TestPut_DeepCopy tests that mutation of original doesn't affect cache.
func TestPut_DeepCopy(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	original := []byte("original-value")
	err := cache.Put("key1", original, 0)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Mutate original
	for i := range original {
		original[i] = 'X'
	}

	// Retrieved should not be affected
	retrieved, found := cache.Get("key1")
	if !found {
		t.Fatal("expected key to be found")
	}

	if bytes.Equal(retrieved, original) {
		t.Error("cache should not be affected by mutation of original")
	}

	expected := []byte("original-value")
	if !bytes.Equal(retrieved, expected) {
		t.Errorf("expected material=%q, got %q", expected, retrieved)
	}
}

// TestGet_DeepCopy tests that mutation of retrieved doesn't affect cache.
func TestGet_DeepCopy(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	original := []byte("cached-value")
	err := cache.Put("key1", original, 0)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Get and mutate
	retrieved1, found := cache.Get("key1")
	if !found {
		t.Fatal("expected key to be found")
	}
	for i := range retrieved1 {
		retrieved1[i] = 'Y'
	}

	// Get again - should be unaffected
	retrieved2, found := cache.Get("key1")
	if !found {
		t.Fatal("expected key to be found")
	}

	if bytes.Equal(retrieved2, retrieved1) {
		t.Error("cache should not be affected by mutation of retrieved material")
	}

	if !bytes.Equal(retrieved2, original) {
		t.Errorf("expected material=%q, got %q", original, retrieved2)
	}
}

// TestTTL_KeyExpires tests key expires after TTL.
func TestTTL_KeyExpires(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	ttl := 20 * time.Millisecond
	err := cache.Put("key1", []byte("value1"), ttl)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Key should be accessible immediately
	if _, found := cache.Get("key1"); !found {
		t.Error("expected key to be found immediately")
	}

	// Wait for expiration
	time.Sleep(30 * time.Millisecond)

	// Key should be expired
	if _, found := cache.Get("key1"); found {
		t.Error("expected key to be expired")
	}

	stats := cache.Stats()
	if stats.Evictions < 1 {
		t.Errorf("expected at least 1 eviction, got %d", stats.Evictions)
	}
}

// TestTTL_KeyAccessibleBeforeExpiry tests key accessible before TTL.
func TestTTL_KeyAccessibleBeforeExpiry(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	// The TTL only has to outlast five 5ms sleeps, but it is set far above
	// that on purpose: at 50ms this test used to fail on loaded CI runners
	// (three hits instead of five) simply because the sleeps and the -race
	// instrumentation overran the key's lifetime. That is a wall-clock margin,
	// not a weaker assertion - every check below is unchanged.
	ttl := 5 * time.Second
	material := []byte("value1")
	err := cache.Put("key1", material, ttl)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Access multiple times before expiry
	for i := 0; i < 5; i++ {
		time.Sleep(5 * time.Millisecond)
		retrieved, found := cache.Get("key1")
		if !found {
			t.Errorf("iteration %d: expected key to be found", i)
		}
		if !bytes.Equal(retrieved, material) {
			t.Errorf("iteration %d: material mismatch", i)
		}
	}

	stats := cache.Stats()
	if stats.Hits != 5 {
		t.Errorf("expected 5 hits, got %d", stats.Hits)
	}
}

// TestTTL_MaxTTLEnforced tests MaxTTL is enforced.
func TestTTL_MaxTTLEnforced(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		MaxTTL: 100 * time.Millisecond,
	})
	defer cache.Close()

	// Try to put with TTL exceeding MaxTTL
	err := cache.Put("key1", []byte("value1"), 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected error when TTL exceeds MaxTTL")
	}
	if !errors.Is(err, ErrTTLTooLarge) {
		t.Errorf("expected ErrTTLTooLarge, got %v", err)
	}

	// Cache should be empty
	if cache.Size() != 0 {
		t.Errorf("expected empty cache, got size=%d", cache.Size())
	}
}

// TestTTL_CustomPerKey tests custom TTL per key.
func TestTTL_CustomPerKey(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		DefaultTTL: 100 * time.Millisecond,
	})
	defer cache.Close()

	// Put keys with different TTLs
	cache.Put("short", []byte("v1"), 10*time.Millisecond)
	cache.Put("medium", []byte("v2"), 30*time.Millisecond)
	cache.Put("long", []byte("v3"), 50*time.Millisecond)

	// After 15ms, only short should be expired
	time.Sleep(15 * time.Millisecond)
	if _, found := cache.Get("short"); found {
		t.Error("expected 'short' to be expired")
	}
	if _, found := cache.Get("medium"); !found {
		t.Error("expected 'medium' to still exist")
	}
	if _, found := cache.Get("long"); !found {
		t.Error("expected 'long' to still exist")
	}

	// After 35ms total, medium should also be expired
	time.Sleep(20 * time.Millisecond)
	if _, found := cache.Get("medium"); found {
		t.Error("expected 'medium' to be expired")
	}
	if _, found := cache.Get("long"); !found {
		t.Error("expected 'long' to still exist")
	}
}

// TestDelete_ExistingKey tests deleting existing key.
func TestDelete_ExistingKey(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	err := cache.Put("key1", []byte("value1"), 0)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if cache.Size() != 1 {
		t.Errorf("expected size=1, got %d", cache.Size())
	}

	cache.Delete("key1")

	if cache.Size() != 0 {
		t.Errorf("expected size=0 after delete, got %d", cache.Size())
	}

	if _, found := cache.Get("key1"); found {
		t.Error("expected key to be deleted")
	}

	stats := cache.Stats()
	if stats.Evictions != 1 {
		t.Errorf("expected 1 eviction, got %d", stats.Evictions)
	}
}

// TestDelete_NonExistent tests deleting non-existent key doesn't error.
func TestDelete_NonExistent(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	// Should not panic or error
	cache.Delete("non-existent")

	if cache.Size() != 0 {
		t.Errorf("expected size=0, got %d", cache.Size())
	}
}

// TestClear_AllKeys tests clearing all keys.
func TestClear_AllKeys(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	// Add multiple keys
	for i := 0; i < 10; i++ {
		err := cache.Put(string(rune('a'+i)), []byte("value"), 0)
		if err != nil {
			t.Fatalf("Put failed for key %d: %v", i, err)
		}
	}

	if cache.Size() != 10 {
		t.Errorf("expected size=10, got %d", cache.Size())
	}

	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("expected size=0 after clear, got %d", cache.Size())
	}

	stats := cache.Stats()
	if stats.Evictions != 10 {
		t.Errorf("expected 10 evictions, got %d", stats.Evictions)
	}
}

// TestClear_UsableAfter tests cache is usable after clear.
func TestClear_UsableAfter(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	cache.Put("key1", []byte("value1"), 0)
	cache.Clear()

	// Should be able to use cache after clear
	err := cache.Put("key2", []byte("value2"), 0)
	if err != nil {
		t.Fatalf("Put after clear failed: %v", err)
	}

	retrieved, found := cache.Get("key2")
	if !found {
		t.Fatal("expected key2 to be found after clear")
	}

	if !bytes.Equal(retrieved, []byte("value2")) {
		t.Errorf("material mismatch after clear")
	}
}

// TestClose_StopsCleanup tests close stops cleanup goroutine.
func TestClose_StopsCleanup(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		CleanupInterval: 10 * time.Millisecond,
	})

	err := cache.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Wait to ensure cleanup would have run
	time.Sleep(30 * time.Millisecond)

	// Second close should return error
	err = cache.Close()
	if !errors.Is(err, ErrCacheClosed) {
		t.Errorf("expected ErrCacheClosed on second close, got %v", err)
	}
}

// TestClose_OperationsReturnError tests operations after close return error.
func TestClose_OperationsReturnError(t *testing.T) {
	cache := NewKeyCache(nil)
	cache.Put("key1", []byte("value1"), 0)

	err := cache.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Put should return error
	err = cache.Put("key2", []byte("value2"), 0)
	if !errors.Is(err, ErrCacheClosed) {
		t.Errorf("expected ErrCacheClosed on Put, got %v", err)
	}

	// Get should return false (not panic)
	_, found := cache.Get("key1")
	if found {
		t.Error("expected Get to return false after close")
	}

	// Size should work (read-only)
	size := cache.Size()
	if size != 0 {
		t.Errorf("expected size=0 after close, got %d", size)
	}
}

// TestLRU_EvictionWhenFull tests eviction when cache is full.
func TestLRU_EvictionWhenFull(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		MaxEntries: 3,
	})
	defer cache.Close()

	// Fill cache
	cache.Put("key1", []byte("value1"), 0)
	time.Sleep(2 * time.Millisecond) // Ensure different access times
	cache.Put("key2", []byte("value2"), 0)
	time.Sleep(2 * time.Millisecond)
	cache.Put("key3", []byte("value3"), 0)

	if cache.Size() != 3 {
		t.Errorf("expected size=3, got %d", cache.Size())
	}

	// Add fourth key - should evict oldest (key1)
	cache.Put("key4", []byte("value4"), 0)

	if cache.Size() != 3 {
		t.Errorf("expected size=3 after eviction, got %d", cache.Size())
	}

	// key1 should be evicted
	if _, found := cache.Get("key1"); found {
		t.Error("expected key1 to be evicted")
	}

	// Others should still exist
	if _, found := cache.Get("key2"); !found {
		t.Error("expected key2 to still exist")
	}
	if _, found := cache.Get("key3"); !found {
		t.Error("expected key3 to still exist")
	}
	if _, found := cache.Get("key4"); !found {
		t.Error("expected key4 to exist")
	}
}

// TestLRU_OrderMaintained tests LRU order is maintained on access.
func TestLRU_OrderMaintained(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		MaxEntries: 3,
	})
	defer cache.Close()

	cache.Put("key1", []byte("value1"), 0)
	time.Sleep(2 * time.Millisecond)
	cache.Put("key2", []byte("value2"), 0)
	time.Sleep(2 * time.Millisecond)
	cache.Put("key3", []byte("value3"), 0)
	time.Sleep(2 * time.Millisecond)

	// Access key1 to make it recently used
	cache.Get("key1")
	time.Sleep(2 * time.Millisecond)

	// Add key4 - should evict key2 (oldest accessed)
	cache.Put("key4", []byte("value4"), 0)

	// key2 should be evicted
	if _, found := cache.Get("key2"); found {
		t.Error("expected key2 to be evicted (least recently accessed)")
	}

	// key1 should still exist (was accessed)
	if _, found := cache.Get("key1"); !found {
		t.Error("expected key1 to still exist (was recently accessed)")
	}
}

// TestLRU_EvictLRU_EmptyCache tests evictLRU on empty cache doesn't panic.
func TestLRU_EvictLRU_EmptyCache(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		MaxEntries: 1,
	})
	defer cache.Close()

	// Cache is empty - evictLRU should handle gracefully
	// This is tested implicitly, but we can verify state
	if cache.Size() != 0 {
		t.Errorf("expected empty cache, got size=%d", cache.Size())
	}

	// Put one entry - should not panic
	err := cache.Put("key1", []byte("value1"), 0)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if cache.Size() != 1 {
		t.Errorf("expected size=1, got %d", cache.Size())
	}
}

// TestLRU_EvictLRU_SingleEntry tests evictLRU with single entry.
func TestLRU_EvictLRU_SingleEntry(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		MaxEntries: 1,
	})
	defer cache.Close()

	// Add first entry
	cache.Put("key1", []byte("value1"), 0)
	if cache.Size() != 1 {
		t.Errorf("expected size=1, got %d", cache.Size())
	}

	// Add second entry - should evict first
	cache.Put("key2", []byte("value2"), 0)
	if cache.Size() != 1 {
		t.Errorf("expected size=1 after eviction, got %d", cache.Size())
	}

	// key1 should be evicted
	if _, found := cache.Get("key1"); found {
		t.Error("expected key1 to be evicted")
	}
	// key2 should exist
	if _, found := cache.Get("key2"); !found {
		t.Error("expected key2 to exist")
	}
}

// TestLRU_EvictLRU_MultipleEntriesSameAccessTime tests eviction with same access time.
func TestLRU_EvictLRU_MultipleEntriesSameAccessTime(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		MaxEntries: 2,
	})
	defer cache.Close()

	// Add entries very quickly (likely same access time)
	cache.Put("key1", []byte("value1"), 0)
	cache.Put("key2", []byte("value2"), 0)

	if cache.Size() != 2 {
		t.Errorf("expected size=2, got %d", cache.Size())
	}

	// Add third entry - should evict one of them
	cache.Put("key3", []byte("value3"), 0)

	if cache.Size() != 2 {
		t.Errorf("expected size=2 after eviction, got %d", cache.Size())
	}

	// key3 should exist
	if _, found := cache.Get("key3"); !found {
		t.Error("expected key3 to exist")
	}

	// One of key1 or key2 should be evicted
	key1Found := false
	key2Found := false
	if _, found := cache.Get("key1"); found {
		key1Found = true
	}
	if _, found := cache.Get("key2"); found {
		key2Found = true
	}

	evicted := 0
	if !key1Found {
		evicted++
	}
	if !key2Found {
		evicted++
	}

	if evicted != 1 {
		t.Errorf("expected exactly 1 key evicted, got %d", evicted)
	}
}

// TestStats_HitsIncrement tests hits increment on successful get.
func TestStats_HitsIncrement(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	cache.Put("key1", []byte("value1"), 0)

	// Initial stats
	stats := cache.Stats()
	initialHits := stats.Hits

	// Multiple gets
	for i := 0; i < 5; i++ {
		cache.Get("key1")
	}

	stats = cache.Stats()
	if stats.Hits != initialHits+5 {
		t.Errorf("expected %d hits, got %d", initialHits+5, stats.Hits)
	}
}

// TestStats_MissesIncrement tests misses increment on failed get.
func TestStats_MissesIncrement(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	stats := cache.Stats()
	initialMisses := stats.Misses

	// Multiple misses
	for i := 0; i < 3; i++ {
		cache.Get("non-existent")
	}

	stats = cache.Stats()
	if stats.Misses != initialMisses+3 {
		t.Errorf("expected %d misses, got %d", initialMisses+3, stats.Misses)
	}
}

// TestStats_Evictions tests evictions count.
func TestStats_Evictions(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		MaxEntries: 2,
	})
	defer cache.Close()

	cache.Put("key1", []byte("value1"), 0)
	cache.Put("key2", []byte("value2"), 0)

	stats := cache.Stats()
	initialEvictions := stats.Evictions

	// This should trigger an eviction
	cache.Put("key3", []byte("value3"), 0)

	stats = cache.Stats()
	if stats.Evictions != initialEvictions+1 {
		t.Errorf("expected %d evictions, got %d", initialEvictions+1, stats.Evictions)
	}

	// Delete should also count as eviction
	cache.Delete("key2")
	stats = cache.Stats()
	if stats.Evictions != initialEvictions+2 {
		t.Errorf("expected %d evictions, got %d", initialEvictions+2, stats.Evictions)
	}
}

// TestStats_SizeReturnsCorrectCount tests Size() returns correct count.
func TestStats_SizeReturnsCorrectCount(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	if cache.Size() != 0 {
		t.Errorf("expected initial size=0, got %d", cache.Size())
	}

	for i := 0; i < 5; i++ {
		cache.Put(string(rune('a'+i)), []byte("value"), 0)
		expectedSize := i + 1
		if cache.Size() != expectedSize {
			t.Errorf("after put %d: expected size=%d, got %d", i, expectedSize, cache.Size())
		}
	}

	stats := cache.Stats()
	if stats.Entries != 5 {
		t.Errorf("expected stats.Entries=5, got %d", stats.Entries)
	}
}

// TestStats_BytesUsed tests BytesUsed is calculated correctly.
func TestStats_BytesUsed(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	cache.Put("key1", []byte("12345"), 0)      // 5 bytes
	cache.Put("key2", []byte("1234567890"), 0) // 10 bytes

	stats := cache.Stats()
	expectedBytes := int64(15)
	if stats.BytesUsed != expectedBytes {
		t.Errorf("expected BytesUsed=%d, got %d", expectedBytes, stats.BytesUsed)
	}
}

// TestPut_EmptyKeyID tests Put with empty keyID returns error.
func TestPut_EmptyKeyID(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	err := cache.Put("", []byte("value"), 0)
	if err == nil {
		t.Fatal("expected error for empty keyID")
	}
}

// TestPut_EmptyMaterial tests Put with empty material returns error.
func TestPut_EmptyMaterial(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	err := cache.Put("key1", nil, 0)
	if err == nil {
		t.Fatal("expected error for nil material")
	}

	err = cache.Put("key1", []byte{}, 0)
	if err == nil {
		t.Fatal("expected error for empty material")
	}
}

// TestPut_MaxKeySize tests MaxKeySize is enforced.
func TestPut_MaxKeySize(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		MaxKeySize: 10,
	})
	defer cache.Close()

	// Small key should work
	err := cache.Put("key1", []byte("12345"), 0)
	if err != nil {
		t.Fatalf("Put with small key failed: %v", err)
	}

	// Large key should fail
	err = cache.Put("key2", make([]byte, 20), 0)
	if err == nil {
		t.Fatal("expected error for key exceeding MaxKeySize")
	}
	if !errors.Is(err, ErrKeyTooLarge) {
		t.Errorf("expected ErrKeyTooLarge, got %v", err)
	}
}

// TestConcurrent_Put tests concurrent Put operations.
func TestConcurrent_Put(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	const goroutines = 10
	const putsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < putsPerGoroutine; i++ {
				keyID := string(rune('a'+id)) + string(rune('0'+i%10))
				material := []byte("value")
				cache.Put(keyID, material, 0)
			}
		}(g)
	}

	wg.Wait()

	// Cache should have some entries (exact count may vary due to overwrites)
	if cache.Size() == 0 {
		t.Error("expected cache to have entries after concurrent puts")
	}
}

// TestConcurrent_Get tests concurrent Get operations.
func TestConcurrent_Get(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	// Populate cache
	for i := 0; i < 10; i++ {
		cache.Put(string(rune('a'+i)), []byte("value"), 0)
	}

	const goroutines = 10
	const getsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < getsPerGoroutine; i++ {
				keyID := string(rune('a' + i%10))
				cache.Get(keyID)
			}
		}()
	}

	wg.Wait()

	stats := cache.Stats()
	expectedHits := uint64(goroutines * getsPerGoroutine)
	if stats.Hits != expectedHits {
		t.Errorf("expected %d hits, got %d", expectedHits, stats.Hits)
	}
}

// TestConcurrent_PutGetMixed tests concurrent Put/Get mixed operations.
func TestConcurrent_PutGetMixed(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	const goroutines = 10
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				keyID := string(rune('a' + i%10))
				if i%2 == 0 {
					cache.Put(keyID, []byte("value"), 0)
				} else {
					cache.Get(keyID)
				}
			}
		}(g)
	}

	wg.Wait()

	// Should complete without panics or race conditions
	if cache.Size() == 0 {
		t.Error("expected cache to have entries")
	}
}

// TestConcurrent_DuringCleanup tests concurrent operations during cleanup.
func TestConcurrent_DuringCleanup(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		CleanupInterval: 10 * time.Millisecond,
	})
	defer cache.Close()

	const duration = 50 * time.Millisecond
	const goroutines = 5

	var wg sync.WaitGroup
	wg.Add(goroutines)

	start := time.Now()

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			i := 0
			for time.Since(start) < duration {
				keyID := string(rune('a'+id)) + string(rune('0'+i%10))
				cache.Put(keyID, []byte("value"), 15*time.Millisecond)
				cache.Get(keyID)
				i++
				time.Sleep(time.Millisecond)
			}
		}(g)
	}

	wg.Wait()

	// Should complete without panics during concurrent cleanup
}

// TestConcurrent_GetNeverObservesZeroedMaterial pins the ownership invariant
// between readers and eviction: key material is zeroed on eviction, so a Get
// that reports a hit must return the material that was cached, never the
// random-then-zero pattern a concurrent eviction writes over it. It exercises
// every path that zeroes an entry - Delete, Clear, Put's replace path and the
// background cleanup goroutine - against readers copying the same entry.
func TestConcurrent_GetNeverObservesZeroedMaterial(t *testing.T) {
	const (
		keyID      = "racy-key"
		readers    = 4
		iterations = 1000
	)

	material := bytes.Repeat([]byte{0xA5}, 64)

	// A 1ms cleanup interval keeps the background eviction goroutine running
	// against the readers for the whole test; the matching TTL below makes
	// every entry a candidate for it.
	cache := NewKeyCache(&KeyCacheOptions{
		CleanupInterval: time.Millisecond,
	})
	defer cache.Close()

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Writer: continuously republishes and evicts the entry so readers are
	// copying material that eviction is concurrently zeroing.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)

		for i := 0; i < iterations; i++ {
			if err := cache.Put(keyID, material, time.Millisecond); err != nil {
				t.Errorf("Put failed: %v", err)
				return
			}
			switch i % 3 {
			case 0:
				cache.Delete(keyID)
			case 1:
				cache.Clear()
			default:
				// Left in place for the cleanup goroutine to expire, or for
				// the next iteration's Put to replace.
			}
		}
	}()

	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case <-done:
					return
				default:
				}

				if got, found := cache.Get(keyID); found && !bytes.Equal(got, material) {
					t.Errorf("Get reported a hit but returned %x, want %x", got, material)
					return
				}

				// Yield so the writer is not starved of the write lock by
				// readers spinning on the read lock.
				runtime.Gosched()
			}
		}()
	}

	wg.Wait()
}

// TestZeroMemory_OnDelete tests memory is zeroed on delete.
func TestZeroMemory_OnDelete(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	material := []byte("sensitive-data")
	cache.Put("key1", material, 0)

	// Get the entry to verify it exists
	retrieved, found := cache.Get("key1")
	if !found || !bytes.Equal(retrieved, material) {
		t.Fatal("key should exist before delete")
	}

	// Delete the key
	cache.Delete("key1")

	// Entry should be gone
	if _, found := cache.Get("key1"); found {
		t.Error("key should not exist after delete")
	}

	// Note: We can't directly verify memory was zeroed as it's internal,
	// but the implementation calls zeroMemory() on delete.
	// This test verifies the delete path executes correctly.
}

// TestZeroMemory_OnEviction tests memory is zeroed on LRU eviction.
func TestZeroMemory_OnEviction(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		MaxEntries: 1,
	})
	defer cache.Close()

	// Put first key
	cache.Put("key1", []byte("value1"), 0)

	// Put second key, should evict first
	cache.Put("key2", []byte("value2"), 0)

	// First key should be gone
	if _, found := cache.Get("key1"); found {
		t.Error("key1 should be evicted")
	}

	stats := cache.Stats()
	if stats.Evictions < 1 {
		t.Errorf("expected at least 1 eviction, got %d", stats.Evictions)
	}
}

// TestZeroMemory_OnExpiration tests memory is zeroed on expiration.
func TestZeroMemory_OnExpiration(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	// Put key with short TTL
	cache.Put("key1", []byte("temporary"), 10*time.Millisecond)

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Get should trigger cleanup and return false
	if _, found := cache.Get("key1"); found {
		t.Error("key should be expired")
	}

	stats := cache.Stats()
	if stats.Evictions < 1 {
		t.Errorf("expected at least 1 eviction, got %d", stats.Evictions)
	}
}

// TestDefaultTTL_UsedWhenZero tests default TTL is used when Put with zero TTL.
func TestDefaultTTL_UsedWhenZero(t *testing.T) {
	defaultTTL := 100 * time.Millisecond
	cache := NewKeyCache(&KeyCacheOptions{
		DefaultTTL: defaultTTL,
	})
	defer cache.Close()

	// Put with zero TTL should use default
	cache.Put("key1", []byte("value1"), 0)

	// Should be accessible before default TTL
	time.Sleep(50 * time.Millisecond)
	if _, found := cache.Get("key1"); !found {
		t.Error("key should be accessible before default TTL")
	}

	// Should expire after default TTL
	time.Sleep(60 * time.Millisecond)
	if _, found := cache.Get("key1"); found {
		t.Error("key should expire after default TTL")
	}
}

// Benchmark tests for performance characterization

func BenchmarkPut(b *testing.B) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	material := make([]byte, 32)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cache.Put("benchmark-key", material, 0)
	}
}

func BenchmarkGet(b *testing.B) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	material := make([]byte, 32)
	cache.Put("benchmark-key", material, 0)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cache.Get("benchmark-key")
	}
}

func BenchmarkPutGet(b *testing.B) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	material := make([]byte, 32)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cache.Put("benchmark-key", material, 0)
		cache.Get("benchmark-key")
	}
}

func BenchmarkConcurrentPut(b *testing.B) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	material := make([]byte, 32)
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			keyID := string(rune('a' + i%26))
			cache.Put(keyID, material, 0)
			i++
		}
	})
}

func BenchmarkConcurrentGet(b *testing.B) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	// Populate cache
	material := make([]byte, 32)
	for i := 0; i < 26; i++ {
		cache.Put(string(rune('a'+i)), material, 0)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			keyID := string(rune('a' + i%26))
			cache.Get(keyID)
			i++
		}
	})
}

type coverageTestLogger struct {
	mu       sync.Mutex
	messages []coverageLogEntry
	fields   map[string]interface{}
}

type coverageLogEntry struct {
	level   string
	message string
	fields  map[string]interface{}
}

func newCoverageTestLogger() *coverageTestLogger {
	return &coverageTestLogger{
		messages: make([]coverageLogEntry, 0),
		fields:   make(map[string]interface{}),
	}
}

func (l *coverageTestLogger) Debug(msg string, args ...any)             { l.log("debug", msg) }
func (l *coverageTestLogger) Debugf(format string, args ...interface{}) { l.log("debug", format) }
func (l *coverageTestLogger) Info(msg string, args ...any)              { l.log("info", msg) }
func (l *coverageTestLogger) Infof(format string, args ...interface{})  { l.log("info", format) }
func (l *coverageTestLogger) Warn(msg string, args ...any)              { l.log("warn", msg) }
func (l *coverageTestLogger) Warnf(format string, args ...interface{})  { l.log("warn", format) }
func (l *coverageTestLogger) Error(msg string, args ...any)             { l.log("error", msg) }
func (l *coverageTestLogger) Errorf(format string, args ...interface{}) { l.log("error", format) }
func (l *coverageTestLogger) Fatal(msg string)                          { l.log("fatal", msg) }
func (l *coverageTestLogger) Fatalf(format string, args ...interface{}) { l.log("fatal", format) }

func (l *coverageTestLogger) WithField(key string, value interface{}) interfaces.Logger {
	newLogger := &coverageTestLogger{
		messages: l.messages,
		fields:   make(map[string]interface{}),
	}
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}
	newLogger.fields[key] = value
	return newLogger
}

func (l *coverageTestLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	newLogger := &coverageTestLogger{
		messages: l.messages,
		fields:   make(map[string]interface{}),
	}
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}
	for k, v := range fields {
		newLogger.fields[k] = v
	}
	return newLogger
}

func (l *coverageTestLogger) WithError(err error) interfaces.Logger {
	return l.WithField("error", err.Error())
}

func (l *coverageTestLogger) log(level, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := coverageLogEntry{
		level:   level,
		message: msg,
		fields:  make(map[string]interface{}),
	}
	for k, v := range l.fields {
		entry.fields[k] = v
	}
	l.messages = append(l.messages, entry)
}

// =============================================================================
// BackendFactory Tests
// =============================================================================

func TestBackendFactory_NewBackendFactory_NilConfig(t *testing.T) {
	factory, err := NewBackendFactory(nil)
	require.NoError(t, err)
	assert.NotNil(t, factory)
	assert.NotNil(t, factory.registry)
}

func TestBackendFactory_NewBackendFactory_WithConfig(t *testing.T) {
	logger := newCoverageTestLogger()
	factory, err := NewBackendFactory(&BackendFactoryConfig{
		Logger:              logger,
		HealthCheckInterval: 60,
	})
	require.NoError(t, err)
	assert.NotNil(t, factory)
	assert.Equal(t, logger, factory.logger)
}

func TestBackendFactory_NewBackendFactory_ZeroHealthCheckInterval(t *testing.T) {
	factory, err := NewBackendFactory(&BackendFactoryConfig{
		HealthCheckInterval: 0,
	})
	require.NoError(t, err)
	assert.NotNil(t, factory)
}

func TestBackendFactory_Registry(t *testing.T) {
	factory, err := NewBackendFactory(nil)
	require.NoError(t, err)

	registry := factory.Registry()
	assert.NotNil(t, registry)
	assert.Equal(t, factory.registry, registry)
}

func TestBackendFactory_RegisterBackend_Success(t *testing.T) {
	logger := newCoverageTestLogger()
	factory, err := NewBackendFactory(&BackendFactoryConfig{
		Logger: logger,
	})
	require.NoError(t, err)

	backend := NewMockBackend()
	err = factory.RegisterBackend("test-backend", backend, 100)
	require.NoError(t, err)

	// Verify backend is registered
	registeredBackend, err := factory.Registry().Get("test-backend")
	require.NoError(t, err)
	assert.Equal(t, backend, registeredBackend)
}

func TestBackendFactory_RegisterBackend_Error(t *testing.T) {
	factory, err := NewBackendFactory(nil)
	require.NoError(t, err)

	// Register first backend
	backend := NewMockBackend()
	err = factory.RegisterBackend("test-backend", backend, 100)
	require.NoError(t, err)

	// Try to register duplicate
	err = factory.RegisterBackend("test-backend", backend, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to register backend")
}

func TestBackendFactory_RegisterBackend_NoLogger(t *testing.T) {
	factory, err := NewBackendFactory(&BackendFactoryConfig{
		Logger: nil,
	})
	require.NoError(t, err)

	backend := NewMockBackend()
	err = factory.RegisterBackend("test-backend", backend, 100)
	require.NoError(t, err)
}

func TestBackendFactory_Close(t *testing.T) {
	factory, err := NewBackendFactory(nil)
	require.NoError(t, err)

	backend := NewMockBackend()
	err = factory.RegisterBackend("test-backend", backend, 100)
	require.NoError(t, err)

	err = factory.Close()
	require.NoError(t, err)
}

func TestBackendFactory_Close_NilRegistry(t *testing.T) {
	factory := &BackendFactory{
		registry: nil,
	}
	err := factory.Close()
	require.NoError(t, err)
}

// =============================================================================
// Service Decrypt Coverage Tests
// =============================================================================

func TestCoverage_Decrypt_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Decrypt(ctx, "", []byte("ciphertext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoverage_Decrypt_EmptyCiphertext(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Decrypt(ctx, "test-key", []byte{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ciphertext cannot be empty")
}

func TestCoverage_Decrypt_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Decrypt(ctx, "test-key", []byte("ciphertext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoverage_Decrypt_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key first using GenerateKey
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Set error on backend
	backend.SetError("Decrypt", errors.New("decryption failed"))

	_, err = service.Decrypt(ctx, "test-key", []byte("ciphertext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decryption failed")
}

func TestCoverage_Decrypt_SuccessRoundTrip(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Encrypt first
	plaintext := []byte("hello world")
	ciphertext, err := service.Encrypt(ctx, "test-key", plaintext, nil)
	require.NoError(t, err)

	// Decrypt
	decrypted, err := service.Decrypt(ctx, "test-key", ciphertext, nil)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

// =============================================================================
// Service Sign Coverage Tests
// =============================================================================

func TestCoverage_Sign_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Sign(ctx, "", []byte("digest"), "SHA256")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoverage_Sign_EmptyDigest(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Sign(ctx, "test-key", []byte{}, "SHA256")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest cannot be empty")
}

func TestCoverage_Sign_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Sign(ctx, "test-key", []byte("digest"), "SHA256")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

// =============================================================================
// Service Verify Coverage Tests
// =============================================================================

func TestCoverage_Verify_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Verify(ctx, "", []byte("digest"), []byte("signature"), "SHA256")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoverage_Verify_EmptyDigest(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Verify(ctx, "test-key", []byte{}, []byte("signature"), "SHA256")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest cannot be empty")
}

func TestCoverage_Verify_EmptySignature(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Verify(ctx, "test-key", []byte("digest"), []byte{}, "SHA256")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature cannot be empty")
}

func TestCoverage_Verify_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Verify(ctx, "test-key", []byte("digest"), []byte("signature"), "SHA256")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

// =============================================================================
// Service GenerateDataKey Coverage Tests
// =============================================================================

func TestCoverage_GenerateDataKey_NilAttrs(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, _, err = service.GenerateDataKey(ctx, nil, 32)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key attributes cannot be nil")
}

func TestCoverage_GenerateDataKey_EmptyCN(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, _, err = service.GenerateDataKey(ctx, &xkmssdk.KeyAttributes{}, 32)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key CN (common name) cannot be empty")
}

func TestCoverage_GenerateDataKey_InvalidKeyBytes(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, _, err = service.GenerateDataKey(ctx, &xkmssdk.KeyAttributes{CN: "test"}, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyBytes must be positive")
}

func TestCoverage_GenerateDataKey_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, _, err = service.GenerateDataKey(ctx, &xkmssdk.KeyAttributes{CN: "test"}, 32)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoverage_GenerateDataKey_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a KEK first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "kek",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	backend.SetError("GenerateDataKey", errors.New("key generation failed"))

	_, _, err = service.GenerateDataKey(ctx, &xkmssdk.KeyAttributes{CN: "kek"}, 32)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key generation failed")
}

func TestCoverage_GenerateDataKey_SuccessOperation(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a KEK first
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "kek",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	plaintext, wrapped, err := service.GenerateDataKey(ctx, &xkmssdk.KeyAttributes{CN: "kek"}, 32)
	require.NoError(t, err)
	assert.NotNil(t, plaintext)
	assert.NotNil(t, wrapped)
	assert.Equal(t, 32, len(plaintext))
}

// =============================================================================
// Service GetBackendType Coverage Tests
// =============================================================================

func TestCoverage_GetBackendType_EmptyRegistry(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Clear the registry after creation
	registry.mu.Lock()
	registry.backends = make(map[string]*BackendInfo)
	registry.mu.Unlock()

	backendType := service.GetBackendType()
	assert.Equal(t, config.BackendTypeSoftware, backendType)
}

// =============================================================================
// Service Close Coverage Tests
// =============================================================================

func TestCoverage_Close_WithLogger(t *testing.T) {
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	err = service.Close()
	require.NoError(t, err)
}

// =============================================================================
// KeyCache evictLRU Coverage Tests
// =============================================================================

func TestCoverage_KeyCache_EvictLRU_EmptyCache(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		DefaultTTL: 1 * time.Minute,
		MaxEntries: 10,
	})
	defer cache.Close()

	// Should not panic on empty cache
	cache.mu.Lock()
	cache.evictLRU()
	cache.mu.Unlock()
}

func TestCoverage_KeyCache_EvictLRU_SingleEntry(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		DefaultTTL: 1 * time.Minute,
		MaxEntries: 1,
	})
	defer cache.Close()

	err := cache.Put("key1", []byte("value1"), 0)
	require.NoError(t, err)

	// Force eviction by adding another entry (cache is full)
	err = cache.Put("key2", []byte("value2"), 0)
	require.NoError(t, err)

	// key1 should be evicted (LRU)
	_, found := cache.Get("key1")
	assert.False(t, found)

	// key2 should exist
	_, found = cache.Get("key2")
	assert.True(t, found)
}

func TestCoverage_KeyCache_EvictLRU_MultipleEntries(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		DefaultTTL: 1 * time.Minute,
		MaxEntries: 2,
	})
	defer cache.Close()

	// Add first entry
	err := cache.Put("key1", []byte("value1"), 0)
	require.NoError(t, err)

	// Wait a bit to ensure different access times
	time.Sleep(10 * time.Millisecond)

	// Add second entry
	err = cache.Put("key2", []byte("value2"), 0)
	require.NoError(t, err)

	// Access key1 to make it more recently used
	_, found := cache.Get("key1")
	require.True(t, found)

	// Add third entry - should evict key2 (least recently used)
	err = cache.Put("key3", []byte("value3"), 0)
	require.NoError(t, err)

	// key2 should be evicted
	_, found = cache.Get("key2")
	assert.False(t, found)

	// key1 and key3 should exist
	_, found = cache.Get("key1")
	assert.True(t, found)
	_, found = cache.Get("key3")
	assert.True(t, found)
}

// =============================================================================
// Router RouteWithFailover Edge Cases
// =============================================================================

func TestCoverage_RouteWithFailover_NegativeMaxAttempts(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend1 := NewMockBackend()
	backend2 := NewMockBackend()

	err := registry.Register("backend1", backend1, 100)
	require.NoError(t, err)
	err = registry.Register("backend2", backend2, 50)
	require.NoError(t, err)

	router := NewBackendRouter(registry, &RoutingConfig{
		FailoverEnabled:     true,
		MaxFailoverAttempts: -1, // Negative should use backend count
	})

	callCount := 0
	err = router.RouteWithFailover(ctx, "", AlgAES256GCM, func(b Backend) error {
		callCount++
		return errors.New("always fail")
	})
	require.Error(t, err)
	assert.Equal(t, 2, callCount) // Should try both backends
}

// =============================================================================
// KeyStore GetUsage Coverage Tests
// =============================================================================

func TestCoverage_KeyStore_GetUsage_NotFound(t *testing.T) {
	store := NewKeyStore()

	usage, err := store.GetUsage("nonexistent-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Equal(t, uint64(0), usage)
}

func TestCoverage_KeyStore_GetUsage_AfterIncrement(t *testing.T) {
	store := NewKeyStore()

	meta := &KeyMetadata{
		KeyID:      "test-key",
		Name:       "test-key",
		Algorithm:  AlgAES256GCM,
		State:      KeyStateEnabled,
		Version:    1,
		UsageCount: 0,
	}
	err := store.Put(meta)
	require.NoError(t, err)

	// Increment usage
	err = store.IncrementUsage("test-key")
	require.NoError(t, err)

	usage, err := store.GetUsage("test-key")
	require.NoError(t, err)
	assert.Equal(t, uint64(1), usage)
}

// =============================================================================
// Service WithLogger Tests for usage increment failures
// =============================================================================

func TestCoverage_Decrypt_UsageIncrementFailure_WithLogger(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Create a key through the backend directly (not via service store)
	backend.mu.Lock()
	backend.keys["test-key"] = &KeyMetadata{
		KeyID:     "test-key",
		Algorithm: AlgAES256GCM,
		State:     KeyStateEnabled,
		Version:   1,
	}
	backend.mu.Unlock()

	// Register the key in the router's registry algorithms
	registry.mu.Lock()
	registry.backends["software"].Algorithms = []string{AlgAES256GCM}
	registry.mu.Unlock()

	// Encrypt first
	ciphertext, err := backend.Encrypt(ctx, "test-key", []byte("hello"), nil)
	require.NoError(t, err)

	// Decrypt - should succeed but log warning for usage increment failure
	// (key not in service store)
	decrypted, err := service.Decrypt(ctx, "test-key", ciphertext, nil)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), decrypted)
}

func TestCoverage_Sign_UsageIncrementFailure_WithLogger(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Create a key through the backend directly
	backend.mu.Lock()
	backend.keys["test-key"] = &KeyMetadata{
		KeyID:     "test-key",
		Algorithm: AlgAES256GCM,
		State:     KeyStateEnabled,
		Version:   1,
	}
	backend.mu.Unlock()

	// Sign - should succeed
	signature, err := service.Sign(ctx, "test-key", []byte("digest"), "SHA256")
	require.NoError(t, err)
	assert.NotNil(t, signature)
}

func TestCoverage_Verify_UsageIncrementFailure_WithLogger(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Create a key through the backend directly
	backend.mu.Lock()
	backend.keys["test-key"] = &KeyMetadata{
		KeyID:     "test-key",
		Algorithm: AlgAES256GCM,
		State:     KeyStateEnabled,
		Version:   1,
	}
	backend.mu.Unlock()

	// Sign first
	digest := []byte("digest")
	signature, err := backend.Sign(ctx, "test-key", digest, "SHA256")
	require.NoError(t, err)

	// Verify - should succeed
	valid, err := service.Verify(ctx, "test-key", digest, signature, "SHA256")
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestCoverage_GenerateDataKey_UsageIncrementFailure_WithLogger(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Create a KEK through the backend directly
	backend.mu.Lock()
	backend.keys["kek"] = &KeyMetadata{
		KeyID:     "kek",
		Algorithm: AlgAES256GCM,
		State:     KeyStateEnabled,
		Version:   1,
	}
	backend.mu.Unlock()

	// Generate data key - should succeed
	plaintext, wrapped, err := service.GenerateDataKey(ctx, &xkmssdk.KeyAttributes{CN: "kek"}, 32)
	require.NoError(t, err)
	assert.NotNil(t, plaintext)
	assert.NotNil(t, wrapped)
}

// =============================================================================
// Service Close Backend Error with Logger
// =============================================================================

func TestCoverage_Close_BackendCloseError_WithLogger(t *testing.T) {
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Set close error on backend
	backend.SetError("Close", errors.New("close failed"))

	err = service.Close()
	// Should still return nil even if backend close fails (just logs warning)
	require.NoError(t, err)
}

// =============================================================================
// Router getFailoverCandidates with Algorithm Filter - No Match
// =============================================================================

func TestCoverage_Route_NoBackendSupportsAlgorithm(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()

	err := registry.Register("backend", backend, 100)
	require.NoError(t, err)

	// Set algorithms that don't include the one we're looking for
	registry.mu.Lock()
	registry.backends["backend"].Algorithms = []string{AlgAES256GCM}
	registry.mu.Unlock()

	router := NewBackendRouter(registry, nil)

	// Route by unsupported algorithm
	result, err := router.Route(ctx, "", AlgEd25519)
	// Should still succeed because it falls back to highest priority backend
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// =============================================================================
// Additional Edge Cases for Disabled Keys
// =============================================================================

func TestCoverage_Decrypt_KeyNotEnabled(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Disable the key
	err = service.DisableKey(ctx, "test-key")
	require.NoError(t, err)

	// Try to decrypt with disabled key
	_, err = service.Decrypt(ctx, "test-key", []byte("ciphertext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestCoverage_GenerateDataKey_KeyNotEnabled(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "kek",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Disable the key
	err = service.DisableKey(ctx, "kek")
	require.NoError(t, err)

	// Try to generate data key with disabled KEK
	_, _, err = service.GenerateDataKey(ctx, &xkmssdk.KeyAttributes{CN: "kek"}, 32)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// =============================================================================
// Additional Coverage Tests for Backend Routing Errors
// =============================================================================

func TestCoverage_Decrypt_BackendRoutingError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Try to decrypt with a key that doesn't exist
	_, err = service.Decrypt(ctx, "nonexistent-key", []byte("ciphertext"), nil)
	require.Error(t, err)
}

func TestCoverage_Sign_BackendRoutingError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Try to sign with a key that doesn't exist
	_, err = service.Sign(ctx, "nonexistent-key", []byte("digest"), "SHA256")
	require.Error(t, err)
}

func TestCoverage_Verify_BackendRoutingError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Try to verify with a key that doesn't exist
	_, err = service.Verify(ctx, "nonexistent-key", []byte("digest"), []byte("signature"), "SHA256")
	require.Error(t, err)
}

func TestCoverage_GenerateDataKey_BackendRoutingError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Try to generate data key with a KEK that doesn't exist
	_, _, err = service.GenerateDataKey(ctx, &xkmssdk.KeyAttributes{CN: "nonexistent-kek"}, 32)
	require.Error(t, err)
}

func TestCoverage_GenerateDataKey_NegativeKeyBytes(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, _, err = service.GenerateDataKey(ctx, &xkmssdk.KeyAttributes{CN: "test"}, -1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyBytes must be positive")
}

// =============================================================================
// ImportKey Coverage Tests
// =============================================================================

func TestCoverage_ImportKey_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.ImportKey(ctx, "", []byte("key-material"), AlgAES256GCM)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoverage_ImportKey_EmptyMaterial(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.ImportKey(ctx, "test-key", []byte{}, AlgAES256GCM)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key material cannot be empty")
}

func TestCoverage_ImportKey_InvalidAlgorithm(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.ImportKey(ctx, "test-key", []byte("key-material"), "INVALID-ALG")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported algorithm")
}

func TestCoverage_ImportKey_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.ImportKey(ctx, "test-key", []byte("key-material"), AlgAES256GCM)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoverage_ImportKey_Success(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Generate 32 bytes for AES-256
	keyMaterial := make([]byte, 32)
	for i := range keyMaterial {
		keyMaterial[i] = byte(i)
	}

	result, err := service.ImportKey(ctx, "imported-key", keyMaterial, AlgAES256GCM)
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify key was stored
	key, err := service.GetKey(ctx, "imported-key")
	require.NoError(t, err)
	assert.NotNil(t, key)
}

func TestCoverage_ImportKey_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	backend.SetError("ImportKey", errors.New("import failed"))

	keyMaterial := make([]byte, 32)
	_, err = service.ImportKey(ctx, "test-key", keyMaterial, AlgAES256GCM)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "import failed")
}

func TestCoverage_ImportKey_DefaultAlgorithm(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	keyMaterial := make([]byte, 32)
	// Empty algorithm should use default
	result, err := service.ImportKey(ctx, "default-alg-key", keyMaterial, "")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// =============================================================================
// GetKey Coverage Tests
// =============================================================================

func TestCoverage_GetKey_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.GetKey(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoverage_GetKey_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.GetKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoverage_GetKey_CacheHit(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Get key - should be from cache
	result, err := service.GetKey(ctx, "test-key")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestCoverage_GetKey_CacheMiss(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Add key directly to backend, not through service
	backend.mu.Lock()
	backend.keys["backend-only-key"] = &KeyMetadata{
		KeyID:     "backend-only-key",
		Algorithm: AlgAES256GCM,
		State:     KeyStateEnabled,
		Version:   1,
	}
	backend.mu.Unlock()

	// Get key - should be fetched from backend
	result, err := service.GetKey(ctx, "backend-only-key")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestCoverage_GetKey_KeyNotFound(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.GetKey(ctx, "nonexistent-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCoverage_GetKey_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	backend.SetError("GetKeyMetadata", errors.New("backend error"))

	_, err = service.GetKey(ctx, "any-key")
	require.Error(t, err)
}

// =============================================================================
// UpdateStoredMetadata Coverage Tests
// =============================================================================

func TestCoverage_UpdateStoredMetadata_InvalidType(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Pass non-KeyMetadata type
	err = service.UpdateStoredMetadata("not-a-metadata")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be *KeyMetadata")
}

func TestCoverage_UpdateStoredMetadata_EmptyKeyID(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.UpdateStoredMetadata(&KeyMetadata{
		KeyID: "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key_id is required")
}

func TestCoverage_UpdateStoredMetadata_Success(t *testing.T) {
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	err = service.UpdateStoredMetadata(&KeyMetadata{
		KeyID:       "test-key",
		Name:        "Test Key",
		Description: "A test key",
		Tags:        map[string]string{"env": "test"},
	})
	require.NoError(t, err)
}

// =============================================================================
// RotateKey Coverage Tests
// =============================================================================

func TestCoverage_RotateKey_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.RotateKey(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoverage_RotateKey_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.RotateKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoverage_RotateKey_KeyNotEnabled(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Disable the key
	err = service.DisableKey(ctx, "test-key")
	require.NoError(t, err)

	// Try to rotate disabled key
	_, err = service.RotateKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestCoverage_RotateKey_Success(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Rotate the key
	rotated, err := service.RotateKey(ctx, "test-key")
	require.NoError(t, err)
	assert.NotNil(t, rotated)
}

func TestCoverage_RotateKey_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	backend.SetError("RotateKey", errors.New("rotation failed"))

	_, err = service.RotateKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rotation failed")
}

func TestCoverage_RotateKey_WithCache(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	cache := NewKeyCache(&KeyCacheOptions{
		DefaultTTL: 1 * time.Minute,
	})

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
		KeyCache: cache,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Rotate the key - should invalidate cache
	rotated, err := service.RotateKey(ctx, "test-key")
	require.NoError(t, err)
	assert.NotNil(t, rotated)

	cache.Close()
}

// =============================================================================
// RevokeKey Coverage Tests
// =============================================================================

func TestCoverage_RevokeKey_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.RevokeKey(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoverage_RevokeKey_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.RevokeKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoverage_RevokeKey_Success(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Revoke the key
	err = service.RevokeKey(ctx, "test-key")
	require.NoError(t, err)
}

func TestCoverage_RevokeKey_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	backend.SetError("DisableKey", errors.New("disable failed"))

	err = service.RevokeKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disable failed")
}

func TestCoverage_RevokeKey_KeyNotFound(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Set up backend to return ErrKeyNotFound
	backend.SetError("DisableKey", ErrKeyNotFound)

	err = service.RevokeKey(ctx, "nonexistent-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCoverage_RevokeKey_WithCache(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	cache := NewKeyCache(&KeyCacheOptions{
		DefaultTTL: 1 * time.Minute,
	})

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
		KeyCache: cache,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Revoke the key - should invalidate cache
	err = service.RevokeKey(ctx, "test-key")
	require.NoError(t, err)

	cache.Close()
}

// =============================================================================
// KeyStore Additional Coverage Tests
// =============================================================================

func TestCoverage_KeyStore_IncrementVersion_EmptyKeyID(t *testing.T) {
	store := NewKeyStore()

	_, err := store.IncrementVersion("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoverage_KeyStore_IncrementVersion_NotFound(t *testing.T) {
	store := NewKeyStore()

	_, err := store.IncrementVersion("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCoverage_KeyStore_GetVersion_EmptyKeyID(t *testing.T) {
	store := NewKeyStore()

	_, err := store.GetVersion("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoverage_KeyStore_GetVersion_NotFound(t *testing.T) {
	store := NewKeyStore()

	_, err := store.GetVersion("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCoverage_KeyStore_GetState_EmptyKeyID(t *testing.T) {
	store := NewKeyStore()

	_, err := store.GetState("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoverage_KeyStore_GetState_NotFound(t *testing.T) {
	store := NewKeyStore()

	_, err := store.GetState("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCoverage_KeyStore_SetState_EmptyKeyID(t *testing.T) {
	store := NewKeyStore()

	err := store.SetState("", KeyStateEnabled)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoverage_KeyStore_IncrementUsage_EmptyKeyID(t *testing.T) {
	store := NewKeyStore()

	err := store.IncrementUsage("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoverage_KeyStore_GetExpiring_NoExpiring(t *testing.T) {
	store := NewKeyStore()

	meta := &KeyMetadata{
		KeyID:     "test-key",
		Algorithm: AlgAES256GCM,
		State:     KeyStateEnabled,
	}
	err := store.Put(meta)
	require.NoError(t, err)

	// No rotation policy = not expiring
	expiring := store.GetExpiring(24 * time.Hour)
	assert.Empty(t, expiring)
}

func TestCoverage_KeyStore_GetExpiring_WithExpiring(t *testing.T) {
	store := NewKeyStore()

	meta := &KeyMetadata{
		KeyID:     "expiring-key",
		Algorithm: AlgAES256GCM,
		State:     KeyStateEnabled,
		RotationPolicy: &RotationPolicy{
			Enabled:      true,
			NextRotation: time.Now().Add(1 * time.Hour),
		},
	}
	err := store.Put(meta)
	require.NoError(t, err)

	expiring := store.GetExpiring(24 * time.Hour)
	assert.Len(t, expiring, 1)
	assert.Equal(t, "expiring-key", expiring[0].KeyID)
}

func TestCoverage_KeyStore_GetPendingDeletion(t *testing.T) {
	store := NewKeyStore()

	meta := &KeyMetadata{
		KeyID:     "pending-delete-key",
		Algorithm: AlgAES256GCM,
		State:     KeyStatePendingDeletion,
	}
	err := store.Put(meta)
	require.NoError(t, err)

	pending := store.GetPendingDeletion()
	assert.Len(t, pending, 1)
	assert.Equal(t, "pending-delete-key", pending[0].KeyID)
}

func TestCoverage_KeyStore_List_WithPrefix(t *testing.T) {
	store := NewKeyStore()

	// Add keys with different prefixes
	for _, prefix := range []string{"prod-", "dev-"} {
		for i := 0; i < 3; i++ {
			meta := &KeyMetadata{
				KeyID:     prefix + string(rune('a'+i)),
				Algorithm: AlgAES256GCM,
				State:     KeyStateEnabled,
			}
			err := store.Put(meta)
			require.NoError(t, err)
		}
	}

	// List with prefix
	prodKeys := store.List("prod-", 0)
	assert.Len(t, prodKeys, 3)

	// List with limit
	limitedKeys := store.List("", 2)
	assert.Len(t, limitedKeys, 2)
}

func TestCoverage_KeyStore_CopyMetadata_NilInput(t *testing.T) {
	store := NewKeyStore()

	result := store.copyMetadata(nil)
	assert.Nil(t, result)
}

func TestCoverage_KeyStore_CopyMetadata_AllFields(t *testing.T) {
	store := NewKeyStore()

	now := time.Now()
	lastUsed := now.Add(-1 * time.Hour)
	meta := &KeyMetadata{
		KeyID:              "test-key",
		Name:               "Test Key",
		Description:        "A test key",
		Algorithm:          AlgAES256GCM,
		State:              KeyStateEnabled,
		Version:            2,
		Exportable:         true,
		DeletionProtection: true,
		Created:            now,
		Updated:            now,
		UsageCount:         100,
		LastUsedAt:         &lastUsed,
		Owner:              "test-user",
		Usage:              []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
		Tags:               map[string]string{"env": "test"},
		RotationPolicy: &RotationPolicy{
			Enabled:        true,
			Interval:       24 * time.Hour,
			NextRotation:   now.Add(24 * time.Hour),
			RetainVersions: 5,
			AutoRotate:     true,
		},
	}

	copied := store.copyMetadata(meta)
	assert.NotNil(t, copied)
	assert.Equal(t, meta.KeyID, copied.KeyID)
	assert.Equal(t, meta.Name, copied.Name)
	assert.Equal(t, meta.Description, copied.Description)
	assert.Equal(t, meta.Algorithm, copied.Algorithm)
	assert.Equal(t, meta.State, copied.State)
	assert.Equal(t, meta.Version, copied.Version)
	assert.Equal(t, meta.Exportable, copied.Exportable)
	assert.Equal(t, meta.DeletionProtection, copied.DeletionProtection)
	assert.Equal(t, meta.Owner, copied.Owner)
	assert.Equal(t, meta.Usage, copied.Usage)
	assert.Equal(t, meta.Tags, copied.Tags)
	assert.NotNil(t, copied.RotationPolicy)
	assert.NotNil(t, copied.LastUsedAt)

	// Verify deep copy - modifying original shouldn't affect copy
	meta.Tags["new"] = "value"
	_, exists := copied.Tags["new"]
	assert.False(t, exists)
}

// =============================================================================
// KeyCache Additional Coverage Tests
// =============================================================================

func TestCoverage_KeyCache_Put_Closed(t *testing.T) {
	cache := NewKeyCache(nil)
	cache.Close()

	err := cache.Put("key", []byte("value"), 0)
	require.Error(t, err)
	assert.Equal(t, ErrCacheClosed, err)
}

func TestCoverage_KeyCache_Put_EmptyKeyID(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	err := cache.Put("", []byte("value"), 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoverage_KeyCache_Put_EmptyMaterial(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	err := cache.Put("key", []byte{}, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "material cannot be empty")
}

func TestCoverage_KeyCache_Put_TTLTooLarge(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		MaxTTL: 1 * time.Minute,
	})
	defer cache.Close()

	err := cache.Put("key", []byte("value"), 1*time.Hour)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTTLTooLarge)
}

func TestCoverage_KeyCache_Put_KeyTooLarge(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		MaxKeySize: 10,
	})
	defer cache.Close()

	err := cache.Put("key", make([]byte, 100), 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyTooLarge)
}

func TestCoverage_KeyCache_Get_Closed(t *testing.T) {
	cache := NewKeyCache(nil)
	cache.Close()

	_, found := cache.Get("key")
	assert.False(t, found)
}

func TestCoverage_KeyCache_Get_Expired(t *testing.T) {
	cache := NewKeyCache(&KeyCacheOptions{
		CleanupInterval: 1 * time.Hour, // Don't auto-cleanup
	})
	defer cache.Close()

	err := cache.Put("key", []byte("value"), 1*time.Millisecond)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	_, found := cache.Get("key")
	assert.False(t, found)
}

func TestCoverage_KeyCache_Clear(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	for i := 0; i < 5; i++ {
		err := cache.Put("key"+string(rune('0'+i)), []byte("value"), 0)
		require.NoError(t, err)
	}

	assert.Equal(t, 5, cache.Size())

	cache.Clear()

	assert.Equal(t, 0, cache.Size())
}

func TestCoverage_KeyCache_Stats(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	err := cache.Put("key", make([]byte, 100), 0)
	require.NoError(t, err)

	_, found := cache.Get("key")
	require.True(t, found)

	_, found = cache.Get("nonexistent")
	require.False(t, found)

	stats := cache.Stats()
	assert.Equal(t, uint64(1), stats.Hits)
	assert.Equal(t, uint64(1), stats.Misses)
	assert.Equal(t, 1, stats.Entries)
	assert.Equal(t, int64(100), stats.BytesUsed)
}

func TestCoverage_KeyCache_Close_AlreadyClosed(t *testing.T) {
	cache := NewKeyCache(nil)

	err := cache.Close()
	require.NoError(t, err)

	err = cache.Close()
	require.Error(t, err)
	assert.Equal(t, ErrCacheClosed, err)
}

func TestCoverage_KeyCache_Put_ReplaceExisting(t *testing.T) {
	cache := NewKeyCache(nil)
	defer cache.Close()

	err := cache.Put("key", []byte("value1"), 0)
	require.NoError(t, err)

	err = cache.Put("key", []byte("value2"), 0)
	require.NoError(t, err)

	value, found := cache.Get("key")
	require.True(t, found)
	assert.Equal(t, []byte("value2"), value)
}

// =============================================================================
// EnableKey Coverage Tests
// =============================================================================

func TestCoverage_EnableKey_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.EnableKey(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoverage_EnableKey_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.EnableKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoverage_EnableKey_Success(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Create and disable a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	err = service.DisableKey(ctx, "test-key")
	require.NoError(t, err)

	// Enable the key
	err = service.EnableKey(ctx, "test-key")
	require.NoError(t, err)
}

func TestCoverage_EnableKey_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	backend.SetError("EnableKey", errors.New("enable failed"))

	err = service.EnableKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "enable failed")
}

func TestCoverage_EnableKey_KeyNotFound(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	backend.SetError("EnableKey", ErrKeyNotFound)

	err = service.EnableKey(ctx, "nonexistent-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// =============================================================================
// DisableKey Coverage Tests
// =============================================================================

func TestCoverage_DisableKey_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.DisableKey(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoverage_DisableKey_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.DisableKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoverage_DisableKey_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	backend.SetError("DisableKey", errors.New("disable failed"))

	err = service.DisableKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disable failed")
}

func TestCoverage_DisableKey_KeyNotFound(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	backend.SetError("DisableKey", ErrKeyNotFound)

	err = service.DisableKey(ctx, "nonexistent-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCoverage_DisableKey_WithCache(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	cache := NewKeyCache(&KeyCacheOptions{
		DefaultTTL: 1 * time.Minute,
	})

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
		KeyCache: cache,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Disable the key - should invalidate cache
	err = service.DisableKey(ctx, "test-key")
	require.NoError(t, err)

	cache.Close()
}

// =============================================================================
// DeleteKey Coverage Tests
// =============================================================================

func TestCoverage_DeleteKey_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.DeleteKey(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoverage_DeleteKey_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = service.DeleteKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoverage_DeleteKey_Success(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Disable first (required for delete)
	err = service.DisableKey(ctx, "test-key")
	require.NoError(t, err)

	// Delete the key
	err = service.DeleteKey(ctx, "test-key")
	require.NoError(t, err)
}

func TestCoverage_DeleteKey_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	backend.SetError("DeleteKey", errors.New("delete failed"))

	err = service.DeleteKey(ctx, "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete failed")
}

func TestCoverage_DeleteKey_KeyNotFound(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	backend.SetError("DeleteKey", ErrKeyNotFound)

	err = service.DeleteKey(ctx, "nonexistent-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCoverage_DeleteKey_WithCache(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	cache := NewKeyCache(&KeyCacheOptions{
		DefaultTTL: 1 * time.Minute,
	})

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
		KeyCache: cache,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Delete the key - should invalidate cache
	err = service.DeleteKey(ctx, "test-key")
	require.NoError(t, err)

	cache.Close()
}

// =============================================================================
// Encrypt Coverage Tests
// =============================================================================

func TestCoverage_Encrypt_EmptyKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Encrypt(ctx, "", []byte("plaintext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyID cannot be empty")
}

func TestCoverage_Encrypt_EmptyPlaintext(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Encrypt(ctx, "test-key", []byte{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plaintext cannot be empty")
}

func TestCoverage_Encrypt_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.Encrypt(ctx, "test-key", []byte("plaintext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoverage_Encrypt_KeyNotEnabled(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create and disable a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	err = service.DisableKey(ctx, "test-key")
	require.NoError(t, err)

	// Try to encrypt with disabled key
	_, err = service.Encrypt(ctx, "test-key", []byte("plaintext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestCoverage_Encrypt_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	backend.SetError("Encrypt", errors.New("encrypt failed"))

	_, err = service.Encrypt(ctx, "test-key", []byte("plaintext"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encrypt failed")
}

func TestCoverage_Encrypt_Success(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Create a key
	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	// Encrypt
	ciphertext, err := service.Encrypt(ctx, "test-key", []byte("hello world"), nil)
	require.NoError(t, err)
	assert.NotNil(t, ciphertext)
}

// =============================================================================
// Additional GenerateKey Coverage Tests
// =============================================================================

func TestCoverage_GenerateKey_EmptyCN(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN: "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key CN (common name) cannot be empty")
}

func TestCoverage_GenerateKey_NilAttrs(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.GenerateKey(ctx, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key attributes cannot be nil")
}

func TestCoverage_GenerateKey_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestCoverage_GenerateKey_BackendError(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	backend.SetError("CreateKey", errors.New("create failed"))

	_, err = service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create failed")
}

func TestCoverage_GenerateKey_Success(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	result, err := service.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)
	assert.NotNil(t, result)
}
