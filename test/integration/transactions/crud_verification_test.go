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
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCRUDPutAndVerify tests that a key can be stored and retrieved with matching value
func TestCRUDPutAndVerify(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := helpers.NewTestClient(t, helpers.GetTestEndpoint())
	defer client.Close()

	// Generate unique key to avoid test interference
	key := fmt.Sprintf("test-crud-put-%d", time.Now().UnixNano())
	expectedValue := []byte("hello world")

	// Put the key
	err := client.Set(ctx, key, expectedValue)
	require.NoError(t, err, "Put should succeed")

	// Verify the key exists and value matches
	actualValue, err := client.Get(ctx, key)
	require.NoError(t, err, "Get should succeed after Put")
	assert.Equal(t, expectedValue, actualValue, "Retrieved value should match stored value")

	// Cleanup
	_ = client.Delete(ctx, key)
}

// TestCRUDDeleteAndVerify tests that a deleted key cannot be retrieved
func TestCRUDDeleteAndVerify(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := helpers.NewTestClient(t, helpers.GetTestEndpoint())
	defer client.Close()

	key := fmt.Sprintf("test-crud-delete-%d", time.Now().UnixNano())
	value := []byte("to be deleted")

	// Put the key first
	err := client.Set(ctx, key, value)
	require.NoError(t, err, "Put should succeed")

	// Verify it exists
	_, err = client.Get(ctx, key)
	require.NoError(t, err, "Key should exist before delete")

	// Delete the key
	err = client.Delete(ctx, key)
	require.NoError(t, err, "Delete should succeed")

	// Verify the key no longer exists
	_, err = client.Get(ctx, key)
	assert.Error(t, err, "Get should fail after Delete")
	assert.Contains(t, err.Error(), "not found", "Error should indicate key not found")
}

// TestCRUDOverwrite tests that overwriting a key updates the value
func TestCRUDOverwrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := helpers.NewTestClient(t, helpers.GetTestEndpoint())
	defer client.Close()

	key := fmt.Sprintf("test-crud-overwrite-%d", time.Now().UnixNano())
	originalValue := []byte("original value")
	newValue := []byte("new value")

	// Put original value
	err := client.Set(ctx, key, originalValue)
	require.NoError(t, err, "First Put should succeed")

	// Verify original value
	actual, err := client.Get(ctx, key)
	require.NoError(t, err, "Get original value should succeed")
	assert.Equal(t, originalValue, actual, "Should get original value")

	// Overwrite with new value
	err = client.Set(ctx, key, newValue)
	require.NoError(t, err, "Overwrite Put should succeed")

	// Verify new value
	actual, err = client.Get(ctx, key)
	require.NoError(t, err, "Get new value should succeed")
	assert.Equal(t, newValue, actual, "Should get new value after overwrite")

	// Cleanup
	_ = client.Delete(ctx, key)
}

// TestCRUDNonExistentGet tests that getting a non-existent key returns proper error
func TestCRUDNonExistentGet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := helpers.NewTestClient(t, helpers.GetTestEndpoint())
	defer client.Close()

	// Try to get a key that definitely doesn't exist
	key := fmt.Sprintf("nonexistent-key-%d", time.Now().UnixNano())

	_, err := client.Get(ctx, key)
	assert.Error(t, err, "Get of non-existent key should return error")
	assert.Contains(t, err.Error(), "not found", "Error should indicate key not found")
}

// TestCRUDEmptyValue tests that empty values can be stored and retrieved
func TestCRUDEmptyValue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := helpers.NewTestClient(t, helpers.GetTestEndpoint())
	defer client.Close()

	key := fmt.Sprintf("test-crud-empty-%d", time.Now().UnixNano())
	emptyValue := []byte{}

	// Put empty value
	err := client.Set(ctx, key, emptyValue)
	require.NoError(t, err, "Put with empty value should succeed")

	// Verify empty value can be retrieved
	actual, err := client.Get(ctx, key)
	require.NoError(t, err, "Get of empty value should succeed")
	assert.Equal(t, emptyValue, actual, "Retrieved empty value should match")

	// Cleanup
	_ = client.Delete(ctx, key)
}

// TestCRUDLargeValue tests that large values can be stored and retrieved
func TestCRUDLargeValue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := helpers.NewTestClient(t, helpers.GetTestEndpoint())
	defer client.Close()

	key := fmt.Sprintf("test-crud-large-%d", time.Now().UnixNano())
	// Create a 1MB value
	largeValue := make([]byte, 1024*1024)
	for i := range largeValue {
		largeValue[i] = byte(i % 256)
	}

	// Put large value
	err := client.Set(ctx, key, largeValue)
	require.NoError(t, err, "Put with large value should succeed")

	// Verify large value can be retrieved intact
	actual, err := client.Get(ctx, key)
	require.NoError(t, err, "Get of large value should succeed")
	assert.Equal(t, len(largeValue), len(actual), "Retrieved value length should match")
	assert.Equal(t, largeValue, actual, "Retrieved large value should match exactly")

	// Cleanup
	_ = client.Delete(ctx, key)
}

// TestCRUDMultipleKeys tests storing and retrieving multiple keys
func TestCRUDMultipleKeys(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := helpers.NewTestClient(t, helpers.GetTestEndpoint())
	defer client.Close()

	prefix := fmt.Sprintf("test-crud-multi-%d", time.Now().UnixNano())
	numKeys := 10
	keys := make([]string, numKeys)
	values := make([][]byte, numKeys)

	// Store multiple keys
	for i := 0; i < numKeys; i++ {
		keys[i] = fmt.Sprintf("%s-key-%d", prefix, i)
		values[i] = []byte(fmt.Sprintf("value-%d", i))
		err := client.Set(ctx, keys[i], values[i])
		require.NoError(t, err, "Put key %d should succeed", i)
	}

	// Verify all keys exist with correct values
	for i := 0; i < numKeys; i++ {
		actual, err := client.Get(ctx, keys[i])
		require.NoError(t, err, "Get key %d should succeed", i)
		assert.Equal(t, values[i], actual, "Value for key %d should match", i)
	}

	// Cleanup
	for i := 0; i < numKeys; i++ {
		_ = client.Delete(ctx, keys[i])
	}
}

// TestCRUDDeleteNonExistent tests that deleting a non-existent key handles gracefully
func TestCRUDDeleteNonExistent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := helpers.NewTestClient(t, helpers.GetTestEndpoint())
	defer client.Close()

	key := fmt.Sprintf("nonexistent-delete-%d", time.Now().UnixNano())

	// Delete should handle non-existent keys (may return error or succeed silently)
	err := client.Delete(ctx, key)
	// The behavior depends on implementation - just verify it doesn't panic
	_ = err
}

// TestCRUDRapidPutGet tests rapid sequential operations
func TestCRUDRapidPutGet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := helpers.NewTestClient(t, helpers.GetTestEndpoint())
	defer client.Close()

	prefix := fmt.Sprintf("test-rapid-%d", time.Now().UnixNano())
	iterations := 50

	for i := 0; i < iterations; i++ {
		key := fmt.Sprintf("%s-%d", prefix, i)
		value := []byte(fmt.Sprintf("rapid-value-%d", i))

		err := client.Set(ctx, key, value)
		require.NoError(t, err, "Rapid Put %d should succeed", i)

		actual, err := client.Get(ctx, key)
		require.NoError(t, err, "Rapid Get %d should succeed", i)
		assert.Equal(t, value, actual, "Rapid value %d should match", i)

		// Cleanup immediately
		_ = client.Delete(ctx, key)
	}
}

// TestCRUDSpecialCharactersInKey tests keys with special characters
func TestCRUDSpecialCharactersInKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := helpers.NewTestClient(t, helpers.GetTestEndpoint())
	defer client.Close()

	specialKeys := []string{
		fmt.Sprintf("test/path/key/%d", time.Now().UnixNano()),
		fmt.Sprintf("test:colon:key:%d", time.Now().UnixNano()),
		fmt.Sprintf("test.dot.key.%d", time.Now().UnixNano()),
		fmt.Sprintf("test-dash-key-%d", time.Now().UnixNano()),
		fmt.Sprintf("test_underscore_key_%d", time.Now().UnixNano()),
	}

	for _, key := range specialKeys {
		value := []byte("special-value")

		err := client.Set(ctx, key, value)
		require.NoError(t, err, "Put with special key %q should succeed", key)

		actual, err := client.Get(ctx, key)
		require.NoError(t, err, "Get with special key %q should succeed", key)
		assert.Equal(t, value, actual, "Value for special key %q should match", key)

		// Cleanup
		_ = client.Delete(ctx, key)
	}
}
