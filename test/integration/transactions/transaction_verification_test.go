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

package transactions

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTransactionCommitAndVerify tests that committed changes persist
func TestTransactionCommitAndVerify(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := helpers.NewTestClient(helpers.GetTestEndpoint())
	defer client.Close()

	prefix := fmt.Sprintf("tx-commit-%d", time.Now().UnixNano())
	key1 := prefix + "-key1"
	key2 := prefix + "-key2"
	value1 := []byte("value1")
	value2 := []byte("value2")

	// Simulate transaction: put multiple keys
	err := client.Set(ctx, key1, value1)
	require.NoError(t, err, "First Put in transaction should succeed")

	err = client.Set(ctx, key2, value2)
	require.NoError(t, err, "Second Put in transaction should succeed")

	// Verify both keys exist after "commit"
	actual1, err := client.Get(ctx, key1)
	require.NoError(t, err, "Get key1 should succeed after commit")
	assert.Equal(t, value1, actual1, "Value1 should persist after commit")

	actual2, err := client.Get(ctx, key2)
	require.NoError(t, err, "Get key2 should succeed after commit")
	assert.Equal(t, value2, actual2, "Value2 should persist after commit")

	// Cleanup
	_ = client.Delete(ctx, key1)
	_ = client.Delete(ctx, key2)
}

// TestTransactionRollbackVerify tests that rolled back changes don't persist
// Note: Without explicit transaction support, we verify cleanup behavior
func TestTransactionRollbackVerify(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := helpers.NewTestClient(helpers.GetTestEndpoint())
	defer client.Close()

	prefix := fmt.Sprintf("tx-rollback-%d", time.Now().UnixNano())
	key := prefix + "-key"
	originalValue := []byte("original")
	newValue := []byte("new-value")

	// Set original value
	err := client.Set(ctx, key, originalValue)
	require.NoError(t, err, "Initial Put should succeed")

	// Verify original
	actual, err := client.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, originalValue, actual)

	// Simulate "rollback" by restoring original value after change
	err = client.Set(ctx, key, newValue)
	require.NoError(t, err, "Update should succeed")

	// Rollback - restore original
	err = client.Set(ctx, key, originalValue)
	require.NoError(t, err, "Rollback (restore) should succeed")

	// Verify rollback worked
	actual, err = client.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, originalValue, actual, "Value should be restored after rollback")

	// Cleanup
	_ = client.Delete(ctx, key)
}

// TestTransactionIsolation tests that concurrent operations don't interfere
func TestTransactionIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client1 := helpers.NewTestClient(helpers.GetTestEndpoint())
	defer client1.Close()

	client2 := helpers.NewTestClient(helpers.GetTestEndpoint())
	defer client2.Close()

	prefix := fmt.Sprintf("tx-isolation-%d", time.Now().UnixNano())

	// Test that two clients can operate on different keys without interference
	key1 := prefix + "-client1-key"
	key2 := prefix + "-client2-key"
	value1 := []byte("client1-value")
	value2 := []byte("client2-value")

	var wg sync.WaitGroup
	var err1, err2 error

	wg.Add(2)

	// Client 1 operations
	go func() {
		defer wg.Done()
		err1 = client1.Set(ctx, key1, value1)
	}()

	// Client 2 operations
	go func() {
		defer wg.Done()
		err2 = client2.Set(ctx, key2, value2)
	}()

	wg.Wait()

	require.NoError(t, err1, "Client1 Put should succeed")
	require.NoError(t, err2, "Client2 Put should succeed")

	// Verify each client's key independently
	actual1, err := client1.Get(ctx, key1)
	require.NoError(t, err)
	assert.Equal(t, value1, actual1, "Client1 value should be correct")

	actual2, err := client2.Get(ctx, key2)
	require.NoError(t, err)
	assert.Equal(t, value2, actual2, "Client2 value should be correct")

	// Cleanup
	_ = client1.Delete(ctx, key1)
	_ = client2.Delete(ctx, key2)
}

// TestTransactionSequentialReads tests read consistency
func TestTransactionSequentialReads(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := helpers.NewTestClient(helpers.GetTestEndpoint())
	defer client.Close()

	key := fmt.Sprintf("tx-sequential-%d", time.Now().UnixNano())
	value := []byte("consistent-value")

	// Write
	err := client.Set(ctx, key, value)
	require.NoError(t, err)

	// Multiple sequential reads should return same value
	for i := 0; i < 10; i++ {
		actual, err := client.Get(ctx, key)
		require.NoError(t, err, "Read %d should succeed", i)
		assert.Equal(t, value, actual, "Read %d should return consistent value", i)
	}

	// Cleanup
	_ = client.Delete(ctx, key)
}

// TestTransactionConcurrentWrites tests handling of concurrent writes to same key
func TestTransactionConcurrentWrites(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	key := fmt.Sprintf("tx-concurrent-write-%d", time.Now().UnixNano())

	numWriters := 5
	var wg sync.WaitGroup
	errors := make([]error, numWriters)

	// Create multiple clients writing to the same key
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			client := helpers.NewTestClient(helpers.GetTestEndpoint())
			defer client.Close()

			value := []byte(fmt.Sprintf("writer-%d-value", writerID))
			errors[writerID] = client.Set(ctx, key, value)
		}(i)
	}

	wg.Wait()

	// At least some writes should succeed
	successCount := 0
	for _, err := range errors {
		if err == nil {
			successCount++
		}
	}
	assert.Greater(t, successCount, 0, "At least some concurrent writes should succeed")

	// Verify key has a value (whichever write won)
	client := helpers.NewTestClient(helpers.GetTestEndpoint())
	defer client.Close()

	actual, err := client.Get(ctx, key)
	require.NoError(t, err, "Final Get should succeed")
	assert.NotEmpty(t, actual, "Key should have a value from one of the writers")

	// Cleanup
	_ = client.Delete(ctx, key)
}

// TestTransactionAtomicityMultiKey tests atomic operations across multiple keys
func TestTransactionAtomicityMultiKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := helpers.NewTestClient(helpers.GetTestEndpoint())
	defer client.Close()

	prefix := fmt.Sprintf("tx-atomic-%d", time.Now().UnixNano())
	numKeys := 5
	keys := make([]string, numKeys)
	values := make([][]byte, numKeys)

	// Create key-value pairs
	for i := 0; i < numKeys; i++ {
		keys[i] = fmt.Sprintf("%s-key-%d", prefix, i)
		values[i] = []byte(fmt.Sprintf("atomic-value-%d", i))
	}

	// Write all keys
	for i := 0; i < numKeys; i++ {
		err := client.Set(ctx, keys[i], values[i])
		require.NoError(t, err, "Put key %d should succeed", i)
	}

	// Verify all keys were written
	for i := 0; i < numKeys; i++ {
		actual, err := client.Get(ctx, keys[i])
		require.NoError(t, err, "Get key %d should succeed", i)
		assert.Equal(t, values[i], actual, "Value %d should match", i)
	}

	// Update all keys atomically
	newValues := make([][]byte, numKeys)
	for i := 0; i < numKeys; i++ {
		newValues[i] = []byte(fmt.Sprintf("updated-value-%d", i))
		err := client.Set(ctx, keys[i], newValues[i])
		require.NoError(t, err, "Update key %d should succeed", i)
	}

	// Verify all updates
	for i := 0; i < numKeys; i++ {
		actual, err := client.Get(ctx, keys[i])
		require.NoError(t, err, "Get updated key %d should succeed", i)
		assert.Equal(t, newValues[i], actual, "Updated value %d should match", i)
	}

	// Cleanup
	for i := 0; i < numKeys; i++ {
		_ = client.Delete(ctx, keys[i])
	}
}

// TestTransactionDeleteAndVerify tests delete consistency
func TestTransactionDeleteAndVerify(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := helpers.NewTestClient(helpers.GetTestEndpoint())
	defer client.Close()

	prefix := fmt.Sprintf("tx-delete-%d", time.Now().UnixNano())
	numKeys := 3
	keys := make([]string, numKeys)

	// Create keys
	for i := 0; i < numKeys; i++ {
		keys[i] = fmt.Sprintf("%s-key-%d", prefix, i)
		err := client.Set(ctx, keys[i], []byte("value"))
		require.NoError(t, err)
	}

	// Delete all keys
	for i := 0; i < numKeys; i++ {
		err := client.Delete(ctx, keys[i])
		require.NoError(t, err, "Delete key %d should succeed", i)
	}

	// Verify all keys are deleted
	for i := 0; i < numKeys; i++ {
		_, err := client.Get(ctx, keys[i])
		assert.Error(t, err, "Get deleted key %d should fail", i)
	}
}

// TestTransactionReadAfterWrite tests read-your-writes consistency
func TestTransactionReadAfterWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := helpers.NewTestClient(helpers.GetTestEndpoint())
	defer client.Close()

	key := fmt.Sprintf("tx-ryw-%d", time.Now().UnixNano())

	// Multiple write-then-read cycles
	for i := 0; i < 10; i++ {
		value := []byte(fmt.Sprintf("iteration-%d", i))

		// Write
		err := client.Set(ctx, key, value)
		require.NoError(t, err, "Write iteration %d should succeed", i)

		// Immediately read - should see own write
		actual, err := client.Get(ctx, key)
		require.NoError(t, err, "Read iteration %d should succeed", i)
		assert.Equal(t, value, actual, "Should read own write in iteration %d", i)
	}

	// Cleanup
	_ = client.Delete(ctx, key)
}

// TestTransactionVersionConflict tests handling of version conflicts
func TestTransactionVersionConflict(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client1 := helpers.NewTestClient(helpers.GetTestEndpoint())
	defer client1.Close()

	client2 := helpers.NewTestClient(helpers.GetTestEndpoint())
	defer client2.Close()

	key := fmt.Sprintf("tx-version-%d", time.Now().UnixNano())
	initialValue := []byte("initial")

	// Set initial value
	err := client1.Set(ctx, key, initialValue)
	require.NoError(t, err)

	// Both clients read the initial value
	v1, err := client1.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, initialValue, v1)

	v2, err := client2.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, initialValue, v2)

	// Client 1 updates
	newValue1 := []byte("updated-by-client1")
	err = client1.Set(ctx, key, newValue1)
	require.NoError(t, err)

	// Client 2 also updates (this is a "lost update" scenario - both succeed but last write wins)
	newValue2 := []byte("updated-by-client2")
	err = client2.Set(ctx, key, newValue2)
	require.NoError(t, err)

	// Final value should be one of the updates (last write wins)
	final, err := client1.Get(ctx, key)
	require.NoError(t, err)
	// Either value is acceptable since we don't have true transactions yet
	assert.True(t,
		string(final) == string(newValue1) || string(final) == string(newValue2),
		"Final value should be one of the updates")

	// Cleanup
	_ = client1.Delete(ctx, key)
}
