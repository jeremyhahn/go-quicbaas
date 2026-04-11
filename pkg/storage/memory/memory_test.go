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

package memory

import (
	"bytes"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// testLogger creates a logger for testing.
func testLogger(t *testing.T) *logger.Logger {
	cfg := logger.LogConfig{
		Level:  "error", // Reduce noise in tests
		Format: "console",
		Output: "stdout",
	}
	log, err := logger.New(cfg)
	require.NoError(t, err)
	return log
}

func TestNew(t *testing.T) {
	t.Run("with logger", func(t *testing.T) {
		log := testLogger(t)
		s := New(log)
		require.NotNil(t, s)
		assert.NotNil(t, s.data)
		assert.NotNil(t, s.logger)
		assert.False(t, s.closed)
	})

	t.Run("with nil logger", func(t *testing.T) {
		s := New(nil)
		require.NotNil(t, s)
		assert.NotNil(t, s.data)
		assert.NotNil(t, s.logger) // Should create default logger
		assert.False(t, s.closed)
	})
}

func TestGet_Found(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	key := []byte("test-key")
	value := []byte("test-value")

	err := s.Set(key, value)
	require.NoError(t, err)

	got, err := s.Get(key)
	require.NoError(t, err)
	assert.Equal(t, value, got)
}

func TestGet_NotFound(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	key := []byte("non-existent")
	_, err := s.Get(key)
	assert.Error(t, err)

	var qbErr *errors.QBError
	assert.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrNotFound, qbErr.Code)
}

func TestGet_EmptyKey(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	_, err := s.Get([]byte{})
	assert.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err))
}

func TestSet_New(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	key := []byte("new-key")
	value := []byte("new-value")

	err := s.Set(key, value)
	require.NoError(t, err)

	got, err := s.Get(key)
	require.NoError(t, err)
	assert.Equal(t, value, got)
}

func TestSet_Overwrite(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	key := []byte("key")
	value1 := []byte("value1")
	value2 := []byte("value2")

	err := s.Set(key, value1)
	require.NoError(t, err)

	err = s.Set(key, value2)
	require.NoError(t, err)

	got, err := s.Get(key)
	require.NoError(t, err)
	assert.Equal(t, value2, got)
}

func TestSet_EmptyKey(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	err := s.Set([]byte{}, []byte("value"))
	assert.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err))
}

func TestSet_EmptyValue(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	key := []byte("key")
	err := s.Set(key, []byte{})
	require.NoError(t, err)

	got, err := s.Get(key)
	require.NoError(t, err)
	assert.Equal(t, []byte{}, got)
}

func TestDelete_Exists(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	key := []byte("key")
	value := []byte("value")

	err := s.Set(key, value)
	require.NoError(t, err)

	err = s.Delete(key)
	require.NoError(t, err)

	_, err = s.Get(key)
	assert.Error(t, err)
	assert.True(t, errors.IsNotFound(err))
}

func TestDelete_NotExists(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	// Deleting non-existent key should not error (idempotent)
	err := s.Delete([]byte("non-existent"))
	assert.NoError(t, err)
}

func TestDelete_EmptyKey(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	err := s.Delete([]byte{})
	assert.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err))
}

func TestScan_Empty(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	result, err := s.Scan(nil, nil)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestScan_Range(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	// Insert test data
	testData := map[string]string{
		"key01": "value01",
		"key02": "value02",
		"key03": "value03",
		"key04": "value04",
		"key05": "value05",
	}
	for k, v := range testData {
		err := s.Set([]byte(k), []byte(v))
		require.NoError(t, err)
	}

	// Scan range [key02, key04)
	result, err := s.Scan([]byte("key02"), []byte("key04"))
	require.NoError(t, err)
	assert.Len(t, result, 2) // key02, key03

	// Verify keys are in range
	for _, pair := range result {
		key := string(pair[0])
		assert.True(t, key >= "key02" && key < "key04")
	}
}

func TestScan_Sorted(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	// Insert keys in random order
	keys := []string{"key05", "key01", "key03", "key02", "key04"}
	for _, k := range keys {
		err := s.Set([]byte(k), []byte("value-"+k))
		require.NoError(t, err)
	}

	// Scan all
	result, err := s.Scan(nil, nil)
	require.NoError(t, err)
	assert.Len(t, result, len(keys))

	// Verify results are sorted
	for i := 0; i < len(result)-1; i++ {
		key1 := result[i][0]
		key2 := result[i+1][0]
		assert.True(t, bytes.Compare(key1, key2) < 0, "Keys should be sorted")
	}
}

func TestScan_NoEnd(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	// Insert test data
	for i := 0; i < 10; i++ {
		key := []byte(fmt.Sprintf("key%02d", i))
		value := []byte(fmt.Sprintf("value%02d", i))
		err := s.Set(key, value)
		require.NoError(t, err)
	}

	// Scan from key05 to end (nil end)
	result, err := s.Scan([]byte("key05"), nil)
	require.NoError(t, err)
	assert.Len(t, result, 5) // key05 through key09

	// Verify first key
	assert.Equal(t, []byte("key05"), result[0][0])
}

func TestScan_NoStart(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	// Insert test data
	for i := 0; i < 10; i++ {
		key := []byte(fmt.Sprintf("key%02d", i))
		value := []byte(fmt.Sprintf("value%02d", i))
		err := s.Set(key, value)
		require.NoError(t, err)
	}

	// Scan from beginning to key05 (nil start)
	result, err := s.Scan(nil, []byte("key05"))
	require.NoError(t, err)
	assert.Len(t, result, 5) // key00 through key04

	// Verify last key
	assert.Equal(t, []byte("key04"), result[len(result)-1][0])
}

func TestClose(t *testing.T) {
	s := New(testLogger(t))

	// Add some data
	err := s.Set([]byte("key"), []byte("value"))
	require.NoError(t, err)

	err = s.Close()
	assert.NoError(t, err)
	assert.True(t, s.closed)
	assert.Nil(t, s.data)
}

func TestClose_Multiple(t *testing.T) {
	s := New(testLogger(t))

	err := s.Close()
	assert.NoError(t, err)

	// Second close should not error (idempotent)
	err = s.Close()
	assert.NoError(t, err)
}

func TestOperations_AfterClose(t *testing.T) {
	s := New(testLogger(t))
	s.Close()

	t.Run("get after close", func(t *testing.T) {
		_, err := s.Get([]byte("key"))
		assert.Error(t, err)
	})

	t.Run("set after close", func(t *testing.T) {
		err := s.Set([]byte("key"), []byte("value"))
		assert.Error(t, err)
	})

	t.Run("delete after close", func(t *testing.T) {
		err := s.Delete([]byte("key"))
		assert.Error(t, err)
	})

	t.Run("scan after close", func(t *testing.T) {
		_, err := s.Scan(nil, nil)
		assert.Error(t, err)
	})

	t.Run("restore after close", func(t *testing.T) {
		snapshot := make(map[string][]byte)
		err := s.Restore(snapshot)
		assert.Error(t, err)
	})
}

func TestSnapshot(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	// Insert test data
	testData := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}
	for k, v := range testData {
		err := s.Set([]byte(k), []byte(v))
		require.NoError(t, err)
	}

	// Create snapshot
	snapshot := s.Snapshot()
	assert.Len(t, snapshot, len(testData))

	// Verify snapshot contains all data
	for k, v := range testData {
		value, exists := snapshot[k]
		assert.True(t, exists)
		assert.Equal(t, []byte(v), value)
	}
}

func TestSnapshot_IsolatedCopy(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	// Insert initial data
	err := s.Set([]byte("key1"), []byte("value1"))
	require.NoError(t, err)

	// Create snapshot
	snapshot := s.Snapshot()

	// Modify original storage
	err = s.Set([]byte("key1"), []byte("modified"))
	require.NoError(t, err)
	err = s.Set([]byte("key2"), []byte("value2"))
	require.NoError(t, err)

	// Snapshot should have original values
	assert.Equal(t, []byte("value1"), snapshot["key1"])
	_, exists := snapshot["key2"]
	assert.False(t, exists)

	// Modify snapshot
	snapshot["key1"] = []byte("snapshot-modified")

	// Original storage should be unaffected
	value, err := s.Get([]byte("key1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("modified"), value)
}

func TestRestore(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	// Insert initial data
	err := s.Set([]byte("old-key"), []byte("old-value"))
	require.NoError(t, err)

	// Create snapshot to restore
	snapshot := map[string][]byte{
		"key1": []byte("value1"),
		"key2": []byte("value2"),
		"key3": []byte("value3"),
	}

	// Restore from snapshot
	err = s.Restore(snapshot)
	require.NoError(t, err)

	// Verify old data is gone
	_, err = s.Get([]byte("old-key"))
	assert.Error(t, err)

	// Verify new data is present
	for k, expectedValue := range snapshot {
		value, err := s.Get([]byte(k))
		require.NoError(t, err)
		assert.Equal(t, expectedValue, value)
	}
}

func TestLen(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	assert.Equal(t, 0, s.Len())

	err := s.Set([]byte("key1"), []byte("value1"))
	require.NoError(t, err)
	assert.Equal(t, 1, s.Len())

	err = s.Set([]byte("key2"), []byte("value2"))
	require.NoError(t, err)
	assert.Equal(t, 2, s.Len())

	err = s.Delete([]byte("key1"))
	require.NoError(t, err)
	assert.Equal(t, 1, s.Len())
}

func TestDefensiveCopy_Get(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	key := []byte("key")
	value := []byte("original-value")

	err := s.Set(key, value)
	require.NoError(t, err)

	// Get the value
	got, err := s.Get(key)
	require.NoError(t, err)

	// Modify the returned value
	copy(got, []byte("modified-value"))

	// Original should be unchanged
	got2, err := s.Get(key)
	require.NoError(t, err)
	assert.Equal(t, value, got2)
}

func TestDefensiveCopy_Set(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	key := []byte("key")
	value := []byte("original-value")

	err := s.Set(key, value)
	require.NoError(t, err)

	// Modify the original slice
	copy(value, []byte("modified-value"))

	// Storage should have original value
	got, err := s.Get(key)
	require.NoError(t, err)
	assert.Equal(t, []byte("original-value"), got)
}

func TestDefensiveCopy_Scan(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	// Insert data
	err := s.Set([]byte("key1"), []byte("value1"))
	require.NoError(t, err)

	// Scan
	result, err := s.Scan(nil, nil)
	require.NoError(t, err)
	require.Len(t, result, 1)

	// Modify returned key and value
	copy(result[0][0], []byte("modifiedkey"))
	copy(result[0][1], []byte("modifiedval"))

	// Storage should be unchanged
	got, err := s.Get([]byte("key1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value1"), got)
}

func TestConcurrent_ReadWrite(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	const numGoroutines = 10
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Concurrent writers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := []byte(fmt.Sprintf("key-%d-%d", id, j))
				value := []byte(fmt.Sprintf("value-%d-%d", id, j))
				err := s.Set(key, value)
				assert.NoError(t, err)
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := []byte(fmt.Sprintf("key-%d-%d", id, j))
				// May or may not find the key depending on timing
				s.Get(key)
			}
		}(i)
	}

	wg.Wait()
}

func TestConcurrent_MultipleWriters(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	const numGoroutines = 50
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Multiple concurrent writers to the same keys
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := []byte(fmt.Sprintf("shared-key-%d", j))
				value := []byte(fmt.Sprintf("value-%d-%d", id, j))
				err := s.Set(key, value)
				assert.NoError(t, err)
			}
		}(i)
	}

	wg.Wait()

	// Verify all keys exist (some writer won)
	for j := 0; j < numOperations; j++ {
		key := []byte(fmt.Sprintf("shared-key-%d", j))
		_, err := s.Get(key)
		assert.NoError(t, err)
	}
}

func TestConcurrent_MixedOperations(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	const numGoroutines = 20
	const numOperations = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 4)

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := []byte(fmt.Sprintf("key-%d", j))
				s.Get(key)
			}
		}(i)
	}

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := []byte(fmt.Sprintf("key-%d", j))
				value := []byte(fmt.Sprintf("value-%d-%d", id, j))
				s.Set(key, value)
			}
		}(i)
	}

	// Concurrent deletes
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := []byte(fmt.Sprintf("key-%d", j))
				s.Delete(key)
			}
		}(i)
	}

	// Concurrent scans
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				s.Scan(nil, nil)
			}
		}()
	}

	wg.Wait()
}

// Benchmarks

func BenchmarkMemoryStorage_Get(b *testing.B) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	s := New(log)
	defer s.Close()

	key := []byte("bench-key")
	value := []byte("bench-value")
	s.Set(key, value)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.Get(key)
		}
	})
}

func BenchmarkMemoryStorage_Set(b *testing.B) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	s := New(log)
	defer s.Close()

	value := []byte("bench-value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("bench-key-%d", i))
		s.Set(key, value)
	}
}

func BenchmarkMemoryStorage_Delete(b *testing.B) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	s := New(log)
	defer s.Close()

	// Pre-populate
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("bench-key-%d", i))
		s.Set(key, []byte("value"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("bench-key-%d", i))
		s.Delete(key)
	}
}

func BenchmarkMemoryStorage_Scan(b *testing.B) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	s := New(log)
	defer s.Close()

	// Populate with 1000 keys
	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("key-%04d", i))
		value := []byte(fmt.Sprintf("value-%04d", i))
		s.Set(key, value)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Scan(nil, nil)
	}
}

func BenchmarkMemoryStorage_Snapshot(b *testing.B) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	s := New(log)
	defer s.Close()

	// Populate with 1000 keys
	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("key-%04d", i))
		value := []byte(fmt.Sprintf("value-%04d", i))
		s.Set(key, value)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Snapshot()
	}
}

func BenchmarkMemoryStorage_Concurrent(b *testing.B) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	s := New(log)
	defer s.Close()

	// Pre-populate
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		value := []byte(fmt.Sprintf("value-%d", i))
		s.Set(key, value)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := []byte(fmt.Sprintf("key-%d", i%100))
			value := []byte(fmt.Sprintf("value-%d", i))

			switch i % 3 {
			case 0:
				s.Get(key)
			case 1:
				s.Set(key, value)
			case 2:
				s.Scan(nil, nil)
			}
			i++
		}
	})
}
