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
	"strconv"
	"sync"
	"testing"
)

func TestExpressionCache_PutAndGet(t *testing.T) {
	cache := NewExpressionCache(10)
	expr := &TermExpr{Field: "age", Op: OpGreaterThan, Value: "25"}

	cache.Put("age > 25", expr)

	got, ok := cache.Get("age > 25")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got != expr {
		t.Error("cached expression does not match original")
	}
}

func TestExpressionCache_Miss(t *testing.T) {
	cache := NewExpressionCache(10)

	_, ok := cache.Get("nonexistent")
	if ok {
		t.Error("expected cache miss for nonexistent key")
	}
}

func TestExpressionCache_Size(t *testing.T) {
	cache := NewExpressionCache(10)
	if cache.Size() != 0 {
		t.Errorf("new cache size = %d, want 0", cache.Size())
	}

	cache.Put("a", &TermExpr{Field: "a", Op: OpEqual, Value: "1"})
	cache.Put("b", &TermExpr{Field: "b", Op: OpEqual, Value: "2"})
	if cache.Size() != 2 {
		t.Errorf("cache size after 2 puts = %d, want 2", cache.Size())
	}
}

func TestExpressionCache_DuplicatePut(t *testing.T) {
	cache := NewExpressionCache(10)
	expr1 := &TermExpr{Field: "a", Op: OpEqual, Value: "1"}
	expr2 := &TermExpr{Field: "a", Op: OpEqual, Value: "2"}

	cache.Put("key", expr1)
	cache.Put("key", expr2) // Should not increment size.

	if cache.Size() != 1 {
		t.Errorf("cache size after duplicate put = %d, want 1", cache.Size())
	}

	// Should return the original expression (not replaced).
	got, ok := cache.Get("key")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got != expr1 {
		t.Error("duplicate Put should not replace existing entry")
	}
}

func TestExpressionCache_Eviction(t *testing.T) {
	cache := NewExpressionCache(3)

	// Fill cache to capacity.
	for i := 0; i < 3; i++ {
		key := "key" + strconv.Itoa(i)
		cache.Put(key, &TermExpr{Field: key, Op: OpEqual, Value: strconv.Itoa(i)})
	}

	if cache.Size() != 3 {
		t.Fatalf("cache size = %d, want 3", cache.Size())
	}

	// Access key1 and key2 to update their access times, leaving key0 as least recent.
	cache.Get("key1")
	cache.Get("key2")

	// Adding a new entry should trigger eviction.
	cache.Put("key3", &TermExpr{Field: "key3", Op: OpEqual, Value: "3"})

	// Cache should still be at or below max size.
	if cache.Size() > 3 {
		t.Errorf("cache size after eviction = %d, want <= 3", cache.Size())
	}
}

func TestExpressionCache_Clear(t *testing.T) {
	cache := NewExpressionCache(10)
	cache.Put("a", &TermExpr{Field: "a", Op: OpEqual, Value: "1"})
	cache.Put("b", &TermExpr{Field: "b", Op: OpEqual, Value: "2"})

	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("cache size after clear = %d, want 0", cache.Size())
	}

	_, ok := cache.Get("a")
	if ok {
		t.Error("expected cache miss after clear")
	}
}

func TestExpressionCache_DefaultMaxSize(t *testing.T) {
	cache := NewExpressionCache(0)
	if cache.maxSize != 1024 {
		t.Errorf("default maxSize = %d, want 1024", cache.maxSize)
	}

	cacheNeg := NewExpressionCache(-5)
	if cacheNeg.maxSize != 1024 {
		t.Errorf("negative maxSize default = %d, want 1024", cacheNeg.maxSize)
	}
}

func TestExpressionCache_ConcurrentAccess(t *testing.T) {
	cache := NewExpressionCache(100)
	var wg sync.WaitGroup

	// Concurrent writes.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := "key" + strconv.Itoa(idx)
			cache.Put(key, &TermExpr{Field: key, Op: OpEqual, Value: strconv.Itoa(idx)})
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := "key" + strconv.Itoa(idx)
			cache.Get(key)
		}(i)
	}

	wg.Wait()

	// Verify cache is in a valid state (no panics, size is reasonable).
	size := cache.Size()
	if size < 0 || size > 100 {
		t.Errorf("cache size after concurrent access = %d, expected 0-100", size)
	}
}

func TestExpressionCache_ConcurrentClear(t *testing.T) {
	cache := NewExpressionCache(100)

	// Fill cache.
	for i := 0; i < 50; i++ {
		cache.Put("key"+strconv.Itoa(i), &TermExpr{Field: "f", Op: OpEqual, Value: "v"})
	}

	var wg sync.WaitGroup

	// Concurrent clears and reads.
	wg.Add(1)
	go func() {
		defer wg.Done()
		cache.Clear()
	}()

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cache.Get("key" + strconv.Itoa(idx))
		}(i)
	}

	wg.Wait()
	// Just verifying no panics or deadlocks.
}
