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

//go:build integration
// +build integration

package memory

import (
	"bytes"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TestMemoryStorage_BasicIntegration tests the basic storage workflow
func TestMemoryStorage_BasicIntegration(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	// Test complete CRUD workflow
	t.Run("create", func(t *testing.T) {
		err := s.Set([]byte("user:1"), []byte(`{"name":"Alice","age":30}`))
		require.NoError(t, err)
	})

	t.Run("read", func(t *testing.T) {
		value, err := s.Get([]byte("user:1"))
		require.NoError(t, err)
		assert.Equal(t, []byte(`{"name":"Alice","age":30}`), value)
	})

	t.Run("update", func(t *testing.T) {
		err := s.Set([]byte("user:1"), []byte(`{"name":"Alice","age":31}`))
		require.NoError(t, err)

		value, err := s.Get([]byte("user:1"))
		require.NoError(t, err)
		assert.Equal(t, []byte(`{"name":"Alice","age":31}`), value)
	})

	t.Run("delete", func(t *testing.T) {
		err := s.Delete([]byte("user:1"))
		require.NoError(t, err)

		_, err = s.Get([]byte("user:1"))
		assert.Error(t, err)
		assert.True(t, errors.IsNotFound(err))
	})
}

// TestMemoryStorage_NamespacePattern tests namespace-based key organization
func TestMemoryStorage_NamespacePattern(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	// Insert data with namespace prefixes
	namespaces := map[string][]string{
		"users:":    {"1", "2", "3"},
		"products:": {"A", "B", "C"},
		"orders:":   {"X", "Y", "Z"},
	}

	for namespace, ids := range namespaces {
		for _, id := range ids {
			key := []byte(namespace + id)
			value := []byte(fmt.Sprintf("data-%s%s", namespace, id))
			err := s.Set(key, value)
			require.NoError(t, err)
		}
	}

	// Scan each namespace
	for namespace, ids := range namespaces {
		start := []byte(namespace)
		// Calculate end by incrementing last byte
		end := make([]byte, len(namespace))
		copy(end, namespace)
		end[len(end)-1]++

		result, err := s.Scan(start, end)
		require.NoError(t, err)
		assert.Len(t, result, len(ids), "namespace %s", namespace)

		// Verify all keys have correct prefix
		for _, pair := range result {
			key := string(pair[0])
			assert.Contains(t, key, namespace)
		}
	}
}

// TestMemoryStorage_LargeDataset tests performance with larger datasets
func TestMemoryStorage_LargeDataset(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	const numRecords = 10000

	t.Run("bulk insert", func(t *testing.T) {
		for i := 0; i < numRecords; i++ {
			key := []byte(fmt.Sprintf("record:%06d", i))
			value := []byte(fmt.Sprintf("data-%06d", i))
			err := s.Set(key, value)
			require.NoError(t, err)
		}

		assert.Equal(t, numRecords, s.Len())
	})

	t.Run("random access", func(t *testing.T) {
		// Access every 100th record
		for i := 0; i < numRecords; i += 100 {
			key := []byte(fmt.Sprintf("record:%06d", i))
			value, err := s.Get(key)
			require.NoError(t, err)
			assert.Equal(t, []byte(fmt.Sprintf("data-%06d", i)), value)
		}
	})

	t.Run("range scan", func(t *testing.T) {
		// Scan middle 1000 records
		start := []byte("record:004500")
		end := []byte("record:005500")

		result, err := s.Scan(start, end)
		require.NoError(t, err)
		assert.Len(t, result, 1000)
	})

	t.Run("bulk delete", func(t *testing.T) {
		// Delete every other record
		for i := 0; i < numRecords; i += 2 {
			key := []byte(fmt.Sprintf("record:%06d", i))
			err := s.Delete(key)
			require.NoError(t, err)
		}

		assert.Equal(t, numRecords/2, s.Len())
	})
}

// TestMemoryStorage_SnapshotRestore tests full snapshot/restore workflow
func TestMemoryStorage_SnapshotRestore(t *testing.T) {
	s1 := New(testLogger(t))
	defer s1.Close()

	// Populate first storage
	testData := map[string]string{
		"config:db:host":     "localhost",
		"config:db:port":     "5432",
		"config:app:name":    "quicbaas",
		"config:app:version": "1.0.0",
	}

	for k, v := range testData {
		err := s1.Set([]byte(k), []byte(v))
		require.NoError(t, err)
	}

	// Create snapshot
	snapshot := s1.Snapshot()
	assert.Len(t, snapshot, len(testData))

	// Create second storage and restore
	s2 := New(testLogger(t))
	defer s2.Close()

	err := s2.Restore(snapshot)
	require.NoError(t, err)

	// Verify all data was restored
	for k, expectedValue := range testData {
		value, err := s2.Get([]byte(k))
		require.NoError(t, err)
		assert.Equal(t, []byte(expectedValue), value)
	}

	// Modify s2 and verify s1 is unchanged
	err = s2.Set([]byte("config:db:host"), []byte("127.0.0.1"))
	require.NoError(t, err)

	value1, err := s1.Get([]byte("config:db:host"))
	require.NoError(t, err)
	assert.Equal(t, []byte("localhost"), value1)

	value2, err := s2.Get([]byte("config:db:host"))
	require.NoError(t, err)
	assert.Equal(t, []byte("127.0.0.1"), value2)
}

// TestMemoryStorage_ConcurrentNamespaces tests concurrent access to different namespaces
func TestMemoryStorage_ConcurrentNamespaces(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	const numNamespaces = 10
	const numRecordsPerNamespace = 100

	var wg sync.WaitGroup
	wg.Add(numNamespaces)

	// Concurrent writes to different namespaces (use zero-padded namespace IDs)
	for i := 0; i < numNamespaces; i++ {
		go func(namespace int) {
			defer wg.Done()

			// Use zero-padded namespace for proper lexicographic ordering
			prefix := fmt.Sprintf("ns%02d:", namespace)
			for j := 0; j < numRecordsPerNamespace; j++ {
				key := []byte(fmt.Sprintf("%s%04d", prefix, j))
				value := []byte(fmt.Sprintf("value-%d-%d", namespace, j))
				err := s.Set(key, value)
				assert.NoError(t, err)
			}
		}(i)
	}

	wg.Wait()

	// Verify all namespaces have correct data
	expectedTotal := numNamespaces * numRecordsPerNamespace
	assert.Equal(t, expectedTotal, s.Len())

	// Scan each namespace and verify
	for i := 0; i < numNamespaces; i++ {
		prefix := fmt.Sprintf("ns%02d:", i)
		start := []byte(prefix)

		// Calculate end by incrementing the last character of prefix
		end := make([]byte, len(prefix))
		copy(end, prefix)
		end[len(end)-1]++ // "ns00:" -> "ns00;" (next char after ':')

		result, err := s.Scan(start, end)
		require.NoError(t, err)
		assert.Len(t, result, numRecordsPerNamespace, "namespace ns%02d", i)

		// Verify all keys have correct prefix
		for _, pair := range result {
			key := string(pair[0])
			assert.True(t, bytes.HasPrefix(pair[0], start), "key %s should have prefix %s", key, prefix)
		}
	}
}

// TestMemoryStorage_StressTest tests storage under heavy concurrent load
func TestMemoryStorage_StressTest(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	const numWorkers = 50
	const opsPerWorker = 500

	var wg sync.WaitGroup
	wg.Add(numWorkers * 3) // readers, writers, deleters

	// Writer workers
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				key := []byte(fmt.Sprintf("stress:%d:%d", workerID, j))
				value := []byte(fmt.Sprintf("value-%d-%d", workerID, j))
				s.Set(key, value)
			}
		}(i)
	}

	// Reader workers
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				key := []byte(fmt.Sprintf("stress:%d:%d", workerID, j))
				s.Get(key) // May or may not exist
			}
		}(i)
	}

	// Scanner workers
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				s.Scan([]byte("stress:"), nil)
			}
		}()
	}

	wg.Wait()

	// Verify storage is still functional
	err := s.Set([]byte("final-test"), []byte("ok"))
	assert.NoError(t, err)

	value, err := s.Get([]byte("final-test"))
	assert.NoError(t, err)
	assert.Equal(t, []byte("ok"), value)
}

// TestMemoryStorage_EdgeCases tests various edge cases
func TestMemoryStorage_EdgeCases(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	t.Run("unicode keys", func(t *testing.T) {
		keys := [][]byte{
			[]byte("用户:1"),           // Chinese
			[]byte("пользователь:1"), // Russian
			[]byte("משתמש:1"),        // Hebrew
			[]byte("مستخدم:1"),       // Arabic
		}

		for _, key := range keys {
			err := s.Set(key, []byte("unicode-value"))
			require.NoError(t, err)

			value, err := s.Get(key)
			require.NoError(t, err)
			assert.Equal(t, []byte("unicode-value"), value)
		}
	})

	t.Run("binary keys and values", func(t *testing.T) {
		binaryKey := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}
		binaryValue := []byte{0xFF, 0xFE, 0xFD, 0x00, 0x01, 0x02}

		err := s.Set(binaryKey, binaryValue)
		require.NoError(t, err)

		value, err := s.Get(binaryKey)
		require.NoError(t, err)
		assert.Equal(t, binaryValue, value)
	})

	t.Run("very long keys", func(t *testing.T) {
		longKey := make([]byte, 10000)
		for i := range longKey {
			longKey[i] = byte(i % 256)
		}

		err := s.Set(longKey, []byte("long-key-value"))
		require.NoError(t, err)

		value, err := s.Get(longKey)
		require.NoError(t, err)
		assert.Equal(t, []byte("long-key-value"), value)
	})

	t.Run("very long values", func(t *testing.T) {
		longValue := make([]byte, 1024*1024) // 1MB
		for i := range longValue {
			longValue[i] = byte(i % 256)
		}

		err := s.Set([]byte("long-value-key"), longValue)
		require.NoError(t, err)

		value, err := s.Get([]byte("long-value-key"))
		require.NoError(t, err)
		assert.Equal(t, longValue, value)
	})

	t.Run("nil value", func(t *testing.T) {
		err := s.Set([]byte("nil-value"), nil)
		require.NoError(t, err)

		value, err := s.Get([]byte("nil-value"))
		require.NoError(t, err)
		assert.Equal(t, []byte{}, value)
	})

	t.Run("scan with identical start and end", func(t *testing.T) {
		err := s.Set([]byte("test"), []byte("value"))
		require.NoError(t, err)

		// Scan with start == end should return empty
		result, err := s.Scan([]byte("test"), []byte("test"))
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("scan with inverted range", func(t *testing.T) {
		// Insert some data
		s.Set([]byte("a"), []byte("va"))
		s.Set([]byte("b"), []byte("vb"))
		s.Set([]byte("c"), []byte("vc"))

		// Scan with start > end should return empty
		result, err := s.Scan([]byte("z"), []byte("a"))
		require.NoError(t, err)
		assert.Empty(t, result)
	})
}

// TestMemoryStorage_MemoryLeaks tests for potential memory leaks
func TestMemoryStorage_MemoryLeaks(t *testing.T) {
	t.Run("repeated open/close", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			s := New(testLogger(t))

			// Add some data
			for j := 0; j < 100; j++ {
				key := []byte(fmt.Sprintf("key-%d", j))
				value := []byte(fmt.Sprintf("value-%d", j))
				s.Set(key, value)
			}

			// Close
			err := s.Close()
			require.NoError(t, err)
		}
	})

	t.Run("repeated set/delete", func(t *testing.T) {
		s := New(testLogger(t))
		defer s.Close()

		for i := 0; i < 1000; i++ {
			key := []byte("reused-key")
			value := []byte(fmt.Sprintf("value-%d", i))

			err := s.Set(key, value)
			require.NoError(t, err)

			err = s.Delete(key)
			require.NoError(t, err)
		}

		// Should have no data left
		assert.Equal(t, 0, s.Len())
	})

	t.Run("repeated snapshots", func(t *testing.T) {
		s := New(testLogger(t))
		defer s.Close()

		// Add initial data
		for i := 0; i < 100; i++ {
			key := []byte(fmt.Sprintf("key-%d", i))
			value := []byte(fmt.Sprintf("value-%d", i))
			s.Set(key, value)
		}

		// Create many snapshots
		for i := 0; i < 100; i++ {
			snapshot := s.Snapshot()
			assert.Len(t, snapshot, 100)
			// Snapshot goes out of scope and should be GC'd
		}
	})
}

// TestMemoryStorage_Pagination simulates pagination pattern
func TestMemoryStorage_Pagination(t *testing.T) {
	s := New(testLogger(t))
	defer s.Close()

	// Insert 100 records
	const totalRecords = 100
	for i := 0; i < totalRecords; i++ {
		key := []byte(fmt.Sprintf("page:%03d", i))
		value := []byte(fmt.Sprintf("data-%03d", i))
		err := s.Set(key, value)
		require.NoError(t, err)
	}

	// Paginate through results
	const pageSize = 10
	var allKeys [][]byte

	var nextKey []byte = []byte("page:")
	for {
		// Scan page
		result, err := s.Scan(nextKey, nil)
		require.NoError(t, err)

		if len(result) == 0 {
			break
		}

		// Take up to pageSize results
		pageEnd := pageSize
		if len(result) < pageSize {
			pageEnd = len(result)
		}

		for i := 0; i < pageEnd; i++ {
			allKeys = append(allKeys, result[i][0])
		}

		if len(result) <= pageSize {
			break
		}

		// Set next key for pagination
		nextKey = result[pageSize][0]
	}

	// Should have retrieved all records
	assert.Len(t, allKeys, totalRecords)
}
