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

package id

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHashGenerator_Generate tests basic ID generation.
func TestHashGenerator_Generate(t *testing.T) {
	gen := NewHashGenerator()

	id1, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	id2, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if id1 == id2 {
		t.Errorf("Generate() produced duplicate IDs: %d", id1)
	}

	if id1 == 0 || id2 == 0 {
		t.Errorf("Generate() produced zero ID")
	}
}

// TestHashGenerator_GenerateFromKey tests deterministic key-based generation.
func TestHashGenerator_GenerateFromKey(t *testing.T) {
	tests := []struct {
		name string
		key  []byte
	}{
		{"simple key", []byte("test-key")},
		{"numeric key", []byte("12345")},
		{"complex key", []byte("user:1234:profile")},
		{"binary key", []byte{0x01, 0x02, 0x03, 0xFF}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := NewHashGenerator()

			// Generate ID twice with same key
			id1, err := gen.GenerateFromKey(tt.key)
			if err != nil {
				t.Fatalf("GenerateFromKey() error = %v", err)
			}

			id2, err := gen.GenerateFromKey(tt.key)
			if err != nil {
				t.Fatalf("GenerateFromKey() error = %v", err)
			}

			// Should produce same ID for same key
			if id1 != id2 {
				t.Errorf("GenerateFromKey() not deterministic: %d != %d", id1, id2)
			}

			if id1 == 0 {
				t.Errorf("GenerateFromKey() produced zero ID")
			}
		})
	}
}

// TestHashGenerator_EmptyKey tests behavior with empty key.
func TestHashGenerator_EmptyKey(t *testing.T) {
	gen := NewHashGenerator()

	id1, err := gen.GenerateFromKey([]byte{})
	if err != nil {
		t.Fatalf("GenerateFromKey() error = %v", err)
	}

	id2, err := gen.GenerateFromKey(nil)
	if err != nil {
		t.Fatalf("GenerateFromKey() error = %v", err)
	}

	// Empty keys should generate unique IDs (not the same)
	if id1 == id2 {
		t.Errorf("GenerateFromKey() with empty keys should produce unique IDs")
	}
}

// TestTimeSeriesGenerator_Generate tests time-series ID generation.
func TestTimeSeriesGenerator_Generate(t *testing.T) {
	gen := NewTimeSeriesGenerator()

	id1, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	id2, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// IDs should be monotonically increasing
	if id2 <= id1 {
		t.Errorf("Generate() not monotonic: %d <= %d", id2, id1)
	}

	if id1 == 0 || id2 == 0 {
		t.Errorf("Generate() produced zero ID")
	}
}

// TestTimeSeriesGenerator_Monotonic tests monotonic ID generation under load.
func TestTimeSeriesGenerator_Monotonic(t *testing.T) {
	gen := NewTimeSeriesGenerator()
	count := 10000
	ids := make([]uint64, count)

	for i := 0; i < count; i++ {
		id, err := gen.Generate()
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		ids[i] = id
	}

	// Verify all IDs are strictly increasing
	for i := 1; i < count; i++ {
		if ids[i] <= ids[i-1] {
			t.Errorf("IDs not monotonic at index %d: %d <= %d", i, ids[i], ids[i-1])
		}
	}
}

// TestSequentialGenerator_Generate tests sequential ID generation.
func TestSequentialGenerator_Generate(t *testing.T) {
	gen := NewSequentialGenerator()

	id1, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	id2, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Should increment by 1
	if id2 != id1+1 {
		t.Errorf("Generate() not sequential: %d != %d+1", id2, id1)
	}

	if id1 != 1 {
		t.Errorf("Generate() first ID should be 1, got %d", id1)
	}
}

// TestSequentialGenerator_WithStart tests sequential generation with custom start.
func TestSequentialGenerator_WithStart(t *testing.T) {
	start := uint64(1000)
	gen := NewSequentialGeneratorWithStart(start)

	id, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if id != start+1 {
		t.Errorf("Generate() expected %d, got %d", start+1, id)
	}
}

// TestUniqueness_HashGenerator tests uniqueness across many IDs.
func TestUniqueness_HashGenerator(t *testing.T) {
	gen := NewHashGenerator()
	count := 100000
	seen := make(map[uint64]bool, count)

	for i := 0; i < count; i++ {
		id, err := gen.Generate()
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}

		if seen[id] {
			t.Errorf("Duplicate ID generated: %d", id)
		}
		seen[id] = true
	}

	if len(seen) != count {
		t.Errorf("Expected %d unique IDs, got %d", count, len(seen))
	}
}

// TestUniqueness_TimeSeriesGenerator tests uniqueness for time-series IDs.
func TestUniqueness_TimeSeriesGenerator(t *testing.T) {
	gen := NewTimeSeriesGenerator()
	count := 100000
	seen := make(map[uint64]bool, count)

	for i := 0; i < count; i++ {
		id, err := gen.Generate()
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}

		if seen[id] {
			t.Errorf("Duplicate ID generated: %d", id)
		}
		seen[id] = true
	}

	if len(seen) != count {
		t.Errorf("Expected %d unique IDs, got %d", count, len(seen))
	}
}

// TestUniqueness_SequentialGenerator tests uniqueness for sequential IDs.
func TestUniqueness_SequentialGenerator(t *testing.T) {
	gen := NewSequentialGenerator()
	count := 100000
	seen := make(map[uint64]bool, count)

	for i := 0; i < count; i++ {
		id, err := gen.Generate()
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}

		if seen[id] {
			t.Errorf("Duplicate ID generated: %d", id)
		}
		seen[id] = true
	}

	if len(seen) != count {
		t.Errorf("Expected %d unique IDs, got %d", count, len(seen))
	}
}

// TestConcurrency_HashGenerator tests thread-safety of hash generator.
func TestConcurrency_HashGenerator(t *testing.T) {
	gen := NewHashGenerator()
	goroutines := 100
	idsPerGoroutine := 1000
	totalIDs := goroutines * idsPerGoroutine

	var mu sync.Mutex
	seen := make(map[uint64]bool, totalIDs)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < idsPerGoroutine; j++ {
				id, err := gen.Generate()
				if err != nil {
					t.Errorf("Generate() error = %v", err)
					return
				}

				mu.Lock()
				if seen[id] {
					t.Errorf("Duplicate ID in concurrent test: %d", id)
				}
				seen[id] = true
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if len(seen) != totalIDs {
		t.Errorf("Expected %d unique IDs, got %d", totalIDs, len(seen))
	}
}

// TestConcurrency_TimeSeriesGenerator tests thread-safety of time-series generator.
func TestConcurrency_TimeSeriesGenerator(t *testing.T) {
	gen := NewTimeSeriesGenerator()
	goroutines := 100
	idsPerGoroutine := 1000
	totalIDs := goroutines * idsPerGoroutine

	var mu sync.Mutex
	seen := make(map[uint64]bool, totalIDs)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < idsPerGoroutine; j++ {
				id, err := gen.Generate()
				if err != nil {
					t.Errorf("Generate() error = %v", err)
					return
				}

				mu.Lock()
				if seen[id] {
					t.Errorf("Duplicate ID in concurrent test: %d", id)
				}
				seen[id] = true
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if len(seen) != totalIDs {
		t.Errorf("Expected %d unique IDs, got %d", totalIDs, len(seen))
	}
}

// TestConcurrency_SequentialGenerator tests thread-safety of sequential generator.
func TestConcurrency_SequentialGenerator(t *testing.T) {
	gen := NewSequentialGenerator()
	goroutines := 100
	idsPerGoroutine := 1000
	totalIDs := goroutines * idsPerGoroutine

	var mu sync.Mutex
	seen := make(map[uint64]bool, totalIDs)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < idsPerGoroutine; j++ {
				id, err := gen.Generate()
				if err != nil {
					t.Errorf("Generate() error = %v", err)
					return
				}

				mu.Lock()
				if seen[id] {
					t.Errorf("Duplicate ID in concurrent test: %d", id)
				}
				seen[id] = true
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if len(seen) != totalIDs {
		t.Errorf("Expected %d unique IDs, got %d", totalIDs, len(seen))
	}
}

// TestCollisionResistance tests hash collision resistance.
func TestCollisionResistance(t *testing.T) {
	gen := NewHashGenerator()
	count := 1000000
	seen := make(map[uint64]bool, count)
	collisions := 0

	for i := 0; i < count; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		id, err := gen.GenerateFromKey(key)
		if err != nil {
			t.Fatalf("GenerateFromKey() error = %v", err)
		}

		if seen[id] {
			collisions++
		}
		seen[id] = true
	}

	// Allow very minimal collisions (less than 0.01%)
	maxCollisions := count / 10000
	if collisions > maxCollisions {
		t.Errorf("Too many collisions: %d (max allowed: %d)", collisions, maxCollisions)
	}

	t.Logf("Generated %d IDs with %d collisions (%.4f%%)",
		count, collisions, float64(collisions)/float64(count)*100)
}

// Benchmark tests for performance validation

// BenchmarkHashGenerator_Generate benchmarks hash-based ID generation.
func BenchmarkHashGenerator_Generate(b *testing.B) {
	gen := NewHashGenerator()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = gen.Generate()
	}
}

// BenchmarkHashGenerator_GenerateFromKey benchmarks key-based hash generation.
func BenchmarkHashGenerator_GenerateFromKey(b *testing.B) {
	gen := NewHashGenerator()
	key := []byte("benchmark-key-12345")
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = gen.GenerateFromKey(key)
	}
}

// BenchmarkTimeSeriesGenerator_Generate benchmarks time-series ID generation.
func BenchmarkTimeSeriesGenerator_Generate(b *testing.B) {
	gen := NewTimeSeriesGenerator()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = gen.Generate()
	}
}

// BenchmarkSequentialGenerator_Generate benchmarks sequential ID generation.
func BenchmarkSequentialGenerator_Generate(b *testing.B) {
	gen := NewSequentialGenerator()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = gen.Generate()
	}
}

// BenchmarkHashGenerator_Parallel benchmarks parallel hash generation.
func BenchmarkHashGenerator_Parallel(b *testing.B) {
	gen := NewHashGenerator()
	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = gen.Generate()
		}
	})
}

// BenchmarkTimeSeriesGenerator_Parallel benchmarks parallel time-series generation.
func BenchmarkTimeSeriesGenerator_Parallel(b *testing.B) {
	gen := NewTimeSeriesGenerator()
	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = gen.Generate()
		}
	})
}

// BenchmarkSequentialGenerator_Parallel benchmarks parallel sequential generation.
func BenchmarkSequentialGenerator_Parallel(b *testing.B) {
	gen := NewSequentialGenerator()
	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = gen.Generate()
		}
	})
}

// BenchmarkFNV1a64 benchmarks the raw hash function.
func BenchmarkFNV1a64(b *testing.B) {
	data := []byte("benchmark-data-for-fnv1a-hash-function")
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = fnv1a64(data)
	}
}

// BenchmarkHashBytes benchmarks the public hash API.
func BenchmarkHashBytes(b *testing.B) {
	data := []byte("benchmark-data-for-hash-bytes-function")
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = HashBytes(data)
	}
}

// BenchmarkHashString benchmarks string hashing.
func BenchmarkHashString(b *testing.B) {
	s := "benchmark-string-for-hash-function"
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = HashString(s)
	}
}

// =============================================================================
// Consolidated from generator_additional_test.go
// =============================================================================

// Tests for TimeSeriesGenerator.GenerateFromKey.
func TestTimeSeriesGenerator_GenerateFromKey(t *testing.T) {
	gen := NewTimeSeriesGenerator()

	tests := []struct {
		name string
		key  []byte
	}{
		{
			name: "non-empty key",
			key:  []byte("test-key"),
		},
		{
			name: "numeric key",
			key:  []byte("12345"),
		},
		{
			name: "binary key",
			key:  []byte{0x01, 0x02, 0x03, 0xFF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate ID from key
			id1, err := gen.GenerateFromKey(tt.key)
			require.NoError(t, err)
			assert.NotZero(t, id1)

			// Generate again with same key
			id2, err := gen.GenerateFromKey(tt.key)
			require.NoError(t, err)
			assert.NotZero(t, id2)

			// IDs should be monotonically increasing
			assert.Greater(t, id2, id1)
		})
	}
}

func TestTimeSeriesGenerator_GenerateFromKey_EmptyKey(t *testing.T) {
	gen := NewTimeSeriesGenerator()

	// Empty key should generate unique ID
	id1, err := gen.GenerateFromKey([]byte{})
	require.NoError(t, err)
	assert.NotZero(t, id1)

	// Nil key should also generate unique ID
	id2, err := gen.GenerateFromKey(nil)
	require.NoError(t, err)
	assert.NotZero(t, id2)

	// IDs should be monotonically increasing
	assert.Greater(t, id2, id1)
}

func TestTimeSeriesGenerator_GenerateFromKey_Monotonic(t *testing.T) {
	gen := NewTimeSeriesGenerator()

	// Generate multiple IDs from the same key
	key := []byte("test-key")
	count := 1000
	ids := make([]uint64, count)

	for i := 0; i < count; i++ {
		id, err := gen.GenerateFromKey(key)
		require.NoError(t, err)
		ids[i] = id
	}

	// Verify all IDs are strictly increasing
	for i := 1; i < count; i++ {
		assert.Greater(t, ids[i], ids[i-1], "IDs must be monotonic at index %d", i)
	}
}

// Tests for SequentialGenerator.GenerateFromKey.
func TestSequentialGenerator_GenerateFromKey(t *testing.T) {
	gen := NewSequentialGenerator()

	tests := []struct {
		name string
		key  []byte
	}{
		{
			name: "simple key",
			key:  []byte("test-key"),
		},
		{
			name: "numeric key",
			key:  []byte("12345"),
		},
		{
			name: "complex key",
			key:  []byte("user:1234:profile"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id1, err := gen.GenerateFromKey(tt.key)
			require.NoError(t, err)

			id2, err := gen.GenerateFromKey(tt.key)
			require.NoError(t, err)

			// Same key should produce same hash
			assert.Equal(t, id1, id2)
		})
	}
}

func TestSequentialGenerator_GenerateFromKey_EmptyKey(t *testing.T) {
	gen := NewSequentialGenerator()

	// Empty key should generate unique ID
	id1, err := gen.GenerateFromKey([]byte{})
	require.NoError(t, err)
	assert.NotZero(t, id1)

	// Nil key should also generate unique ID
	id2, err := gen.GenerateFromKey(nil)
	require.NoError(t, err)
	assert.NotZero(t, id2)

	// Empty keys should produce different IDs (sequential)
	assert.NotEqual(t, id1, id2)
}

func TestSequentialGenerator_GenerateFromKey_Consistency(t *testing.T) {
	gen := NewSequentialGenerator()

	key := []byte("consistent-key")

	// Generate ID from key multiple times
	id1, err := gen.GenerateFromKey(key)
	require.NoError(t, err)

	id2, err := gen.GenerateFromKey(key)
	require.NoError(t, err)

	id3, err := gen.GenerateFromKey(key)
	require.NoError(t, err)

	// All should be the same
	assert.Equal(t, id1, id2)
	assert.Equal(t, id2, id3)
}

// Additional concurrency tests.
func TestTimeSeriesGenerator_GenerateFromKey_Concurrent(t *testing.T) {
	gen := NewTimeSeriesGenerator()
	key := []byte("concurrent-key")

	count := 1000
	ids := make([]uint64, count)
	var wg sync.WaitGroup

	// Generate IDs concurrently
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func(idx int) {
			defer wg.Done()
			id, err := gen.GenerateFromKey(key)
			require.NoError(t, err)
			ids[idx] = id
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify all IDs are non-zero and unique
	seen := make(map[uint64]bool)
	for _, id := range ids {
		assert.NotZero(t, id)
		assert.False(t, seen[id], "duplicate ID: %d", id)
		seen[id] = true
	}
}

func TestSequentialGenerator_GenerateFromKey_DifferentKeys(t *testing.T) {
	gen := NewSequentialGenerator()

	key1 := []byte("key1")
	key2 := []byte("key2")

	id1, err := gen.GenerateFromKey(key1)
	require.NoError(t, err)

	id2, err := gen.GenerateFromKey(key2)
	require.NoError(t, err)

	// Different keys should (likely) produce different IDs
	assert.NotEqual(t, id1, id2)
}

// Benchmark tests for missing functions.
func BenchmarkTimeSeriesGenerator_GenerateFromKey(b *testing.B) {
	gen := NewTimeSeriesGenerator()
	key := []byte("benchmark-key-for-timeseries")
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = gen.GenerateFromKey(key)
	}
}

func BenchmarkSequentialGenerator_GenerateFromKey(b *testing.B) {
	gen := NewSequentialGenerator()
	key := []byte("benchmark-key-for-sequential")
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = gen.GenerateFromKey(key)
	}
}

func BenchmarkTimeSeriesGenerator_GenerateFromKey_Parallel(b *testing.B) {
	gen := NewTimeSeriesGenerator()
	key := []byte("benchmark-key-parallel")
	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = gen.GenerateFromKey(key)
		}
	})
}

func BenchmarkSequentialGenerator_GenerateFromKey_Parallel(b *testing.B) {
	gen := NewSequentialGenerator()
	key := []byte("benchmark-key-parallel")
	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = gen.GenerateFromKey(key)
		}
	})
}
