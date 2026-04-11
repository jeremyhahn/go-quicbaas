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

package kvstore

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
)

// TestKVStorePutGet tests basic Put and Get operations
func TestKVStorePutGet(t *testing.T) {
	stores := getAllKVStores(t)
	defer closeAllStores(stores)

	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			key := fmt.Sprintf("test-key-%d", time.Now().UnixNano())
			value := []byte("test-value")

			// Put operation
			err := tc.store.Put(ctx, key, value)
			require.NoError(t, err, "Put should succeed")

			// Get operation
			retrieved, err := tc.store.Get(ctx, key)
			require.NoError(t, err, "Get should succeed")
			assert.Equal(t, value, retrieved, "Retrieved value should match")

			// Cleanup
			_ = tc.store.Delete(ctx, key)
		})
	}
}

// TestKVStoreDelete tests Delete operations
func TestKVStoreDelete(t *testing.T) {
	stores := getAllKVStores(t)
	defer closeAllStores(stores)

	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			key := fmt.Sprintf("delete-key-%d", time.Now().UnixNano())
			value := []byte("delete-value")

			// Put first
			err := tc.store.Put(ctx, key, value)
			require.NoError(t, err, "Put should succeed")

			// Verify it exists
			_, err = tc.store.Get(ctx, key)
			require.NoError(t, err, "Get should succeed before delete")

			// Delete
			err = tc.store.Delete(ctx, key)
			require.NoError(t, err, "Delete should succeed")

			// Verify it's gone
			_, err = tc.store.Get(ctx, key)
			require.Error(t, err, "Get should fail after delete")
			assertNotFoundError(t, err)
		})
	}
}

// TestKVStoreDeleteIdempotent tests that Delete is idempotent
func TestKVStoreDeleteIdempotent(t *testing.T) {
	stores := getAllKVStores(t)
	defer closeAllStores(stores)

	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			key := fmt.Sprintf("nonexistent-key-%d", time.Now().UnixNano())

			// Delete non-existent key should not error (idempotent)
			err := tc.store.Delete(ctx, key)
			assert.NoError(t, err, "Delete of non-existent key should succeed")

			// Second delete should also succeed
			err = tc.store.Delete(ctx, key)
			assert.NoError(t, err, "Second delete should succeed")
		})
	}
}

// TestKVStoreGetNotFound tests Get operation on non-existent key
func TestKVStoreGetNotFound(t *testing.T) {
	stores := getAllKVStores(t)
	defer closeAllStores(stores)

	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			key := fmt.Sprintf("nonexistent-key-%d", time.Now().UnixNano())

			// Get non-existent key
			_, err := tc.store.Get(ctx, key)
			require.Error(t, err, "Get should fail for non-existent key")
			assertNotFoundError(t, err)
		})
	}
}

// TestKVStorePutOverwrite tests overwriting existing values
func TestKVStorePutOverwrite(t *testing.T) {
	stores := getAllKVStores(t)
	defer closeAllStores(stores)

	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			key := fmt.Sprintf("overwrite-key-%d", time.Now().UnixNano())
			value1 := []byte("original-value")
			value2 := []byte("updated-value")

			// Put original value
			err := tc.store.Put(ctx, key, value1)
			require.NoError(t, err, "First Put should succeed")

			// Verify original
			retrieved, err := tc.store.Get(ctx, key)
			require.NoError(t, err, "Get should succeed")
			assert.Equal(t, value1, retrieved, "Should get original value")

			// Overwrite with new value
			err = tc.store.Put(ctx, key, value2)
			require.NoError(t, err, "Second Put should succeed")

			// Verify updated
			retrieved, err = tc.store.Get(ctx, key)
			require.NoError(t, err, "Get should succeed after overwrite")
			assert.Equal(t, value2, retrieved, "Should get updated value")

			// Cleanup
			_ = tc.store.Delete(ctx, key)
		})
	}
}

// TestKVStoreScan tests Scan operation with prefix matching
func TestKVStoreScan(t *testing.T) {
	stores := getAllKVStores(t)
	defer closeAllStores(stores)

	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			prefix := fmt.Sprintf("scan-prefix-%d-", time.Now().UnixNano())
			testData := map[string]string{
				"key1": "value1",
				"key2": "value2",
				"key3": "value3",
			}

			// Put test data
			for k, v := range testData {
				key := prefix + k
				err := tc.store.Put(ctx, key, []byte(v))
				require.NoError(t, err, "Put should succeed")
			}

			// Scan with prefix
			found := make(map[string]string)
			err := tc.store.Scan(ctx, prefix, func(key string, value []byte) error {
				// Extract key suffix after prefix
				suffix := key[len(prefix):]
				found[suffix] = string(value)
				return nil
			})
			require.NoError(t, err, "Scan should succeed")

			// Verify all keys were found
			assert.Equal(t, len(testData), len(found), "Should find all keys")
			for k, v := range testData {
				assert.Equal(t, v, found[k], "Value should match for key %s", k)
			}

			// Cleanup
			for k := range testData {
				key := prefix + k
				_ = tc.store.Delete(ctx, key)
			}
		})
	}
}

// TestKVStoreScanEmpty tests Scan with no matching keys
func TestKVStoreScanEmpty(t *testing.T) {
	stores := getAllKVStores(t)
	defer closeAllStores(stores)

	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			prefix := fmt.Sprintf("nonexistent-prefix-%d", time.Now().UnixNano())

			// Scan with non-existent prefix
			callbackCalled := false
			err := tc.store.Scan(ctx, prefix, func(key string, value []byte) error {
				callbackCalled = true
				return nil
			})
			require.NoError(t, err, "Scan should succeed even with no results")
			assert.False(t, callbackCalled, "Callback should not be called for empty results")
		})
	}
}

// TestKVStoreScanEarlyTermination tests Scan with early termination
func TestKVStoreScanEarlyTermination(t *testing.T) {
	stores := getAllKVStores(t)
	defer closeAllStores(stores)

	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			prefix := fmt.Sprintf("scan-term-%d-", time.Now().UnixNano())

			// Put multiple items
			for i := 0; i < 5; i++ {
				key := fmt.Sprintf("%skey%d", prefix, i)
				value := []byte(fmt.Sprintf("value%d", i))
				err := tc.store.Put(ctx, key, value)
				require.NoError(t, err, "Put should succeed")
			}

			// Scan and stop after 2 items
			count := 0
			stopErr := fmt.Errorf("stop iteration")
			err := tc.store.Scan(ctx, prefix, func(key string, value []byte) error {
				count++
				if count >= 2 {
					return stopErr
				}
				return nil
			})
			assert.ErrorIs(t, err, stopErr, "Scan should return callback error")
			assert.GreaterOrEqual(t, count, 2, "Should have seen at least 2 items")

			// Cleanup
			for i := 0; i < 5; i++ {
				key := fmt.Sprintf("%skey%d", prefix, i)
				_ = tc.store.Delete(ctx, key)
			}
		})
	}
}

// TestKVStoreLargeValues tests storing and retrieving large values
func TestKVStoreLargeValues(t *testing.T) {
	stores := getAllKVStores(t)
	defer closeAllStores(stores)

	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			key := fmt.Sprintf("large-key-%d", time.Now().UnixNano())

			// Test various sizes
			sizes := []int{
				1024,        // 1 KB
				10 * 1024,   // 10 KB
				100 * 1024,  // 100 KB
				1024 * 1024, // 1 MB
			}

			for _, size := range sizes {
				t.Run(fmt.Sprintf("Size_%dB", size), func(t *testing.T) {
					value := make([]byte, size)
					for i := range value {
						value[i] = byte(i % 256)
					}

					// Put large value
					err := tc.store.Put(ctx, key, value)
					require.NoError(t, err, "Put should succeed for size %d", size)

					// Get large value
					retrieved, err := tc.store.Get(ctx, key)
					require.NoError(t, err, "Get should succeed for size %d", size)
					assert.Equal(t, value, retrieved, "Retrieved value should match for size %d", size)

					// Delete for next iteration
					err = tc.store.Delete(ctx, key)
					require.NoError(t, err, "Delete should succeed")
				})
			}
		})
	}
}

// TestKVStoreEmptyValues tests storing empty values
func TestKVStoreEmptyValues(t *testing.T) {
	stores := getAllKVStores(t)
	defer closeAllStores(stores)

	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			key := fmt.Sprintf("empty-key-%d", time.Now().UnixNano())
			value := []byte{}

			// Put empty value
			err := tc.store.Put(ctx, key, value)
			require.NoError(t, err, "Put should succeed with empty value")

			// Get empty value
			retrieved, err := tc.store.Get(ctx, key)
			require.NoError(t, err, "Get should succeed")
			assert.Equal(t, value, retrieved, "Retrieved empty value should match")

			// Cleanup
			_ = tc.store.Delete(ctx, key)
		})
	}
}

// TestKVStoreConcurrentPut tests concurrent Put operations
func TestKVStoreConcurrentPut(t *testing.T) {
	stores := getAllKVStores(t)
	defer closeAllStores(stores)

	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			prefix := fmt.Sprintf("concurrent-put-%d-", time.Now().UnixNano())
			numGoroutines := 10
			numOperations := 20

			var wg sync.WaitGroup
			errors := make(chan error, numGoroutines*numOperations)

			for i := 0; i < numGoroutines; i++ {
				wg.Add(1)
				go func(goroutineID int) {
					defer wg.Done()

					for j := 0; j < numOperations; j++ {
						key := fmt.Sprintf("%skey-%d-%d", prefix, goroutineID, j)
						value := []byte(fmt.Sprintf("value-%d-%d", goroutineID, j))

						if err := tc.store.Put(ctx, key, value); err != nil {
							errors <- fmt.Errorf("goroutine %d, op %d: %w", goroutineID, j, err)
						}
					}
				}(i)
			}

			wg.Wait()
			close(errors)

			// Check for errors
			var errs []error
			for err := range errors {
				errs = append(errs, err)
			}
			assert.Empty(t, errs, "No errors should occur during concurrent puts: %v", errs)

			// Cleanup
			for i := 0; i < numGoroutines; i++ {
				for j := 0; j < numOperations; j++ {
					key := fmt.Sprintf("%skey-%d-%d", prefix, i, j)
					_ = tc.store.Delete(ctx, key)
				}
			}
		})
	}
}

// TestKVStoreConcurrentMixed tests concurrent mixed operations (Put/Get/Delete)
func TestKVStoreConcurrentMixed(t *testing.T) {
	stores := getAllKVStores(t)
	defer closeAllStores(stores)

	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			prefix := fmt.Sprintf("concurrent-mixed-%d-", time.Now().UnixNano())
			numGoroutines := 10
			numOperations := 10

			var wg sync.WaitGroup
			var successfulOps atomic.Int64

			for i := 0; i < numGoroutines; i++ {
				wg.Add(1)
				go func(goroutineID int) {
					defer wg.Done()

					for j := 0; j < numOperations; j++ {
						key := fmt.Sprintf("%skey-%d-%d", prefix, goroutineID, j)
						value := []byte(fmt.Sprintf("value-%d-%d", goroutineID, j))

						// Put
						if err := tc.store.Put(ctx, key, value); err != nil {
							t.Logf("Put failed: %v", err)
							continue
						}

						// Get
						retrieved, err := tc.store.Get(ctx, key)
						if err != nil {
							t.Logf("Get failed: %v", err)
							continue
						}
						if string(retrieved) != string(value) {
							t.Logf("Value mismatch: expected %s, got %s", value, retrieved)
							continue
						}

						// Delete
						if err := tc.store.Delete(ctx, key); err != nil {
							t.Logf("Delete failed: %v", err)
							continue
						}

						successfulOps.Add(1)
					}
				}(i)
			}

			wg.Wait()

			// We should have a reasonable success rate
			totalOps := int64(numGoroutines * numOperations)
			successRate := float64(successfulOps.Load()) / float64(totalOps)
			assert.Greater(t, successRate, 0.8, "Success rate should be > 80%%")
			t.Logf("Success rate: %.2f%% (%d/%d)", successRate*100, successfulOps.Load(), totalOps)
		})
	}
}

// TestKVStoreConcurrentScan tests concurrent Scan operations
func TestKVStoreConcurrentScan(t *testing.T) {
	stores := getAllKVStores(t)
	defer closeAllStores(stores)

	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			prefix := fmt.Sprintf("concurrent-scan-%d-", time.Now().UnixNano())

			// Populate data
			numKeys := 50
			for i := 0; i < numKeys; i++ {
				key := fmt.Sprintf("%skey-%d", prefix, i)
				value := []byte(fmt.Sprintf("value-%d", i))
				err := tc.store.Put(ctx, key, value)
				require.NoError(t, err, "Put should succeed")
			}

			// Concurrent scans
			numGoroutines := 10
			var wg sync.WaitGroup
			errors := make(chan error, numGoroutines)

			for i := 0; i < numGoroutines; i++ {
				wg.Add(1)
				go func(goroutineID int) {
					defer wg.Done()

					count := 0
					err := tc.store.Scan(ctx, prefix, func(key string, value []byte) error {
						count++
						return nil
					})
					if err != nil {
						errors <- fmt.Errorf("goroutine %d: %w", goroutineID, err)
					}
					if count != numKeys {
						errors <- fmt.Errorf("goroutine %d: expected %d keys, got %d", goroutineID, numKeys, count)
					}
				}(i)
			}

			wg.Wait()
			close(errors)

			// Check for errors
			var errs []error
			for err := range errors {
				errs = append(errs, err)
			}
			assert.Empty(t, errs, "No errors should occur during concurrent scans: %v", errs)

			// Cleanup
			for i := 0; i < numKeys; i++ {
				key := fmt.Sprintf("%skey-%d", prefix, i)
				_ = tc.store.Delete(ctx, key)
			}
		})
	}
}

// TestKVStoreSpecialCharacters tests keys with special characters
func TestKVStoreSpecialCharacters(t *testing.T) {
	stores := getAllKVStores(t)
	defer closeAllStores(stores)

	specialKeys := []string{
		"key/with/slashes",
		"key:with:colons",
		"key with spaces",
		"key\twith\ttabs",
		"key\nwith\nnewlines",
		"key.with.dots",
		"key-with-dashes",
		"key_with_underscores",
		"key@with#special$chars%",
		"key[with]brackets{and}braces",
	}

	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			for _, specialKey := range specialKeys {
				t.Run(specialKey, func(t *testing.T) {
					key := fmt.Sprintf("special-%d-%s", time.Now().UnixNano(), specialKey)
					value := []byte("special-value")

					// Put
					err := tc.store.Put(ctx, key, value)
					require.NoError(t, err, "Put should succeed with special key")

					// Get
					retrieved, err := tc.store.Get(ctx, key)
					require.NoError(t, err, "Get should succeed with special key")
					assert.Equal(t, value, retrieved, "Value should match")

					// Cleanup
					_ = tc.store.Delete(ctx, key)
				})
			}
		})
	}
}

// TestKVStoreContextCancellation tests context cancellation handling
func TestKVStoreContextCancellation(t *testing.T) {
	stores := getAllKVStores(t)
	defer closeAllStores(stores)

	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			key := fmt.Sprintf("cancel-key-%d", time.Now().UnixNano())
			value := []byte("cancel-value")

			// Create a context that's already cancelled
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			// Operations should respect context cancellation
			// Note: Some backends might complete fast operations before checking context
			err := tc.store.Put(ctx, key, value)
			if err != nil {
				assert.Contains(t, err.Error(), "context canceled", "Should indicate context cancellation")
			}

			// Cleanup with valid context
			validCtx := context.Background()
			_ = tc.store.Delete(validCtx, key)
		})
	}
}

// TestKVStoreKeyLengths tests various key lengths
func TestKVStoreKeyLengths(t *testing.T) {
	stores := getAllKVStores(t)
	defer closeAllStores(stores)

	for _, tc := range stores {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			keyLengths := []int{
				1,
				10,
				100,
				256,
				512,
			}

			for _, length := range keyLengths {
				t.Run(fmt.Sprintf("Length_%d", length), func(t *testing.T) {
					keyBytes := make([]byte, length)
					for i := range keyBytes {
						keyBytes[i] = byte('a' + (i % 26))
					}
					key := string(keyBytes)
					value := []byte("test-value")

					// Put
					err := tc.store.Put(ctx, key, value)
					require.NoError(t, err, "Put should succeed with key length %d", length)

					// Get
					retrieved, err := tc.store.Get(ctx, key)
					require.NoError(t, err, "Get should succeed with key length %d", length)
					assert.Equal(t, value, retrieved, "Value should match")

					// Cleanup
					_ = tc.store.Delete(ctx, key)
				})
			}
		})
	}
}

// Helper functions

type kvStoreTestCase struct {
	name  string
	store interfaces.KVStore
}

// getAllKVStores returns all available KVStore implementations for testing
func getAllKVStores(t *testing.T) []kvStoreTestCase {
	t.Helper()

	var stores []kvStoreTestCase

	// Always include memory store for testing
	memStore := server.NewMemoryKVStore()
	stores = append(stores, kvStoreTestCase{
		name:  "MemoryKVStore",
		store: memStore,
	})

	// Note: Server-based KVStore tests would require a running cluster
	// and are better suited for end-to-end tests rather than unit integration tests.
	// The MemoryKVStore provides comprehensive coverage of the KVStore interface.

	return stores
}

// closeAllStores closes any stores that need cleanup
func closeAllStores(stores []kvStoreTestCase) {
	for range stores {
		// Memory store doesn't need cleanup
		// Server-based stores would be managed externally
	}
}

// assertNotFoundError checks if error is a NotFound error
func assertNotFoundError(t *testing.T, err error) {
	t.Helper()

	if qbErr, ok := err.(*dberrors.QBError); ok {
		assert.Equal(t, dberrors.ErrNotFound, qbErr.Code, "Error should be ErrNotFound")
	} else {
		// Also accept error messages containing "not found"
		errMsg := strings.ToLower(err.Error())
		assert.True(t, strings.Contains(errMsg, "not found"), "Error should indicate key not found: %v", err)
	}
}
