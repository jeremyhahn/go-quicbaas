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

// Package kms provides key management services for QuicBaaS.
package kms

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock KVStore for Testing
// =============================================================================

// MockKVStore is a test implementation of interfaces.KVStore.
type MockKVStore struct {
	mu      sync.RWMutex
	data    map[string][]byte
	errors  map[string]error
	scanErr error
}

// NewMockKVStore creates a new mock KVStore for testing.
func NewMockKVStore() *MockKVStore {
	return &MockKVStore{
		data:   make(map[string][]byte),
		errors: make(map[string]error),
	}
}

// SetError configures the mock to return an error for a specific method.
func (m *MockKVStore) SetError(method string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors[method] = err
}

// SetScanError configures the mock to return an error during Scan.
func (m *MockKVStore) SetScanError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scanErr = err
}

// ClearError removes the configured error for a method.
func (m *MockKVStore) ClearError(method string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.errors, method)
}

// Put stores a key-value pair.
func (m *MockKVStore) Put(_ context.Context, key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.errors["Put"]; ok {
		return err
	}
	m.data[key] = append([]byte(nil), value...)
	return nil
}

// Get retrieves a value by key.
func (m *MockKVStore) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err, ok := m.errors["Get"]; ok {
		return nil, err
	}
	val, ok := m.data[key]
	if !ok {
		return nil, errors.New("key not found")
	}
	return append([]byte(nil), val...), nil
}

// Delete removes a key-value pair.
func (m *MockKVStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.errors["Delete"]; ok {
		return err
	}
	delete(m.data, key)
	return nil
}

// Scan iterates over all key-value pairs matching the given prefix.
func (m *MockKVStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.scanErr != nil {
		return m.scanErr
	}
	if err, ok := m.errors["Scan"]; ok {
		return err
	}
	for k, v := range m.data {
		if strings.HasPrefix(k, prefix) {
			if err := fn(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

// List returns all keys matching the given prefix.
func (m *MockKVStore) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err, ok := m.errors["List"]; ok {
		return nil, err
	}
	var keys []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// Exists checks whether a key exists in the store.
func (m *MockKVStore) Exists(_ context.Context, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err, ok := m.errors["Exists"]; ok {
		return false, err
	}
	_, ok := m.data[key]
	return ok, nil
}

// =============================================================================
// KVStoreStorageAdapter Tests
// =============================================================================

func TestNewKVStoreStorageAdapter(t *testing.T) {
	t.Run("nil kvstore returns error", func(t *testing.T) {
		adapter, err := NewKVStoreStorageAdapter(nil)
		assert.Nil(t, adapter)
		assert.ErrorIs(t, err, ErrNilStorage)
	})

	t.Run("valid kvstore returns adapter", func(t *testing.T) {
		mockStore := NewMockKVStore()
		adapter, err := NewKVStoreStorageAdapter(mockStore)
		require.NoError(t, err)
		assert.NotNil(t, adapter)

		// Verify it implements xkmssdk.StorageBackend
		_, ok := adapter.(xkmssdk.StorageBackend)
		assert.True(t, ok, "adapter should implement xkmssdk.StorageBackend")
	})
}

func TestKVStoreStorageAdapter_Get_Success(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	// Pre-populate data with the correct prefix
	fullKey := KeyPrefix + "test-key"
	mockStore.data[fullKey] = []byte("test-value")

	value, err := adapter.Get(ctx, "test-key")
	require.NoError(t, err)
	assert.Equal(t, []byte("test-value"), value)
}

func TestKVStoreStorageAdapter_Get_NotFound(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	value, err := adapter.Get(ctx, "non-existent-key")
	assert.Nil(t, value)
	assert.ErrorIs(t, err, xkmssdk.ErrStorageNotFound)
}

func TestKVStoreStorageAdapter_Get_StoreError(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	// Configure the mock to return an error
	mockStore.SetError("Get", errors.New("storage unavailable"))

	value, err := adapter.Get(ctx, "any-key")
	assert.Nil(t, value)
	assert.ErrorIs(t, err, xkmssdk.ErrStorageNotFound)
}

func TestKVStoreStorageAdapter_Put_Success(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	err = adapter.Put(ctx, "test-key", []byte("test-value"))
	require.NoError(t, err)

	// Verify the data was stored with the correct prefix
	fullKey := KeyPrefix + "test-key"
	storedValue, exists := mockStore.data[fullKey]
	assert.True(t, exists)
	assert.Equal(t, []byte("test-value"), storedValue)
}

func TestKVStoreStorageAdapter_Put_WithOptions(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	// Options are ignored in this adapter but should not cause errors

	err = adapter.Put(ctx, "test-key", []byte("test-value"))
	require.NoError(t, err)

	// Verify the data was stored
	fullKey := KeyPrefix + "test-key"
	storedValue, exists := mockStore.data[fullKey]
	assert.True(t, exists)
	assert.Equal(t, []byte("test-value"), storedValue)
}

func TestKVStoreStorageAdapter_Put_Error(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	putErr := errors.New("storage full")
	mockStore.SetError("Put", putErr)

	err = adapter.Put(ctx, "test-key", []byte("test-value"))
	assert.Error(t, err)
	assert.Equal(t, putErr, err)
}

func TestKVStoreStorageAdapter_Put_Overwrite(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	// Put initial value
	err = adapter.Put(ctx, "test-key", []byte("original"))
	require.NoError(t, err)

	// Put new value
	err = adapter.Put(ctx, "test-key", []byte("updated"))
	require.NoError(t, err)

	// Verify the value was updated
	fullKey := KeyPrefix + "test-key"
	storedValue := mockStore.data[fullKey]
	assert.Equal(t, []byte("updated"), storedValue)
}

func TestKVStoreStorageAdapter_Delete_Success(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	// Pre-populate data
	fullKey := KeyPrefix + "test-key"
	mockStore.data[fullKey] = []byte("test-value")

	err = adapter.Delete(ctx, "test-key")
	require.NoError(t, err)

	// Verify key was deleted
	_, exists := mockStore.data[fullKey]
	assert.False(t, exists)
}

func TestKVStoreStorageAdapter_Delete_NonExistent(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	// Delete is idempotent - should not error for non-existent keys
	err = adapter.Delete(ctx, "non-existent-key")
	assert.NoError(t, err)
}

func TestKVStoreStorageAdapter_Delete_Error(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	deleteErr := errors.New("delete failed")
	mockStore.SetError("Delete", deleteErr)

	err = adapter.Delete(ctx, "test-key")
	assert.Error(t, err)
	assert.Equal(t, deleteErr, err)
}

func TestKVStoreStorageAdapter_List_Success(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	// Pre-populate data with correct prefix
	mockStore.data[KeyPrefix+"app/key1"] = []byte("value1")
	mockStore.data[KeyPrefix+"app/key2"] = []byte("value2")
	mockStore.data[KeyPrefix+"app/db/key3"] = []byte("value3")
	mockStore.data[KeyPrefix+"other/key4"] = []byte("value4")

	t.Run("list all keys", func(t *testing.T) {
		keys, err := adapter.List(ctx, "")
		require.NoError(t, err)
		assert.Len(t, keys, 4)
	})

	t.Run("list with prefix", func(t *testing.T) {
		keys, err := adapter.List(ctx, "app/")
		require.NoError(t, err)
		assert.Len(t, keys, 3)
		for _, key := range keys {
			assert.True(t, strings.HasPrefix(key, "app/"))
		}
	})

	t.Run("list with specific prefix", func(t *testing.T) {
		keys, err := adapter.List(ctx, "app/db/")
		require.NoError(t, err)
		assert.Len(t, keys, 1)
		assert.Equal(t, "app/db/key3", keys[0])
	})
}

func TestKVStoreStorageAdapter_List_Empty(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	t.Run("empty store", func(t *testing.T) {
		keys, err := adapter.List(ctx, "")
		require.NoError(t, err)
		assert.Empty(t, keys)
	})

	t.Run("no matching prefix", func(t *testing.T) {
		// Add some data
		mockStore.data[KeyPrefix+"app/key1"] = []byte("value1")

		keys, err := adapter.List(ctx, "nonexistent/")
		require.NoError(t, err)
		assert.Empty(t, keys)
	})
}

func TestKVStoreStorageAdapter_List_Error(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	scanErr := errors.New("scan failed")
	mockStore.SetScanError(scanErr)

	keys, err := adapter.List(ctx, "")
	assert.Nil(t, keys)
	assert.Error(t, err)
	assert.Equal(t, scanErr, err)
}

func TestKVStoreStorageAdapter_Exists_True(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	// Pre-populate data
	fullKey := KeyPrefix + "test-key"
	mockStore.data[fullKey] = []byte("test-value")

	exists, err := adapter.Exists(ctx, "test-key")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestKVStoreStorageAdapter_Exists_False(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	exists, err := adapter.Exists(ctx, "non-existent-key")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestKVStoreStorageAdapter_Exists_StoreError(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	// Configure error - Exists returns false on error (per implementation)
	mockStore.SetError("Get", errors.New("storage error"))

	exists, err := adapter.Exists(ctx, "any-key")
	require.NoError(t, err)
	assert.False(t, exists, "should return false when Get returns error")
}

func TestKVStoreStorageAdapter_Close(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	// Close should be a no-op and return nil
	err = adapter.Close()
	require.NoError(t, err)

	// Should still be able to use the adapter after close
	err = adapter.Put(ctx, "after-close", []byte("value"))
	assert.NoError(t, err)
}

func TestKVStoreStorageAdapter_InterfaceCompliance(t *testing.T) {
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	// Verify the adapter implements xkmssdk.StorageBackend
	var _ xkmssdk.StorageBackend = adapter
}

func TestKVStoreStorageAdapter_KeyPrefixing(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	// Test that keys are properly prefixed
	testKey := "my/test/key"
	testValue := []byte("test-value")

	err = adapter.Put(ctx, testKey, testValue)
	require.NoError(t, err)

	// Verify the key is stored with the KMS prefix
	expectedKey := KeyPrefix + testKey
	storedValue, exists := mockStore.data[expectedKey]
	assert.True(t, exists, "key should be stored with prefix")
	assert.Equal(t, testValue, storedValue)

	// Verify Get retrieves with prefix
	value, err := adapter.Get(ctx, testKey)
	require.NoError(t, err)
	assert.Equal(t, testValue, value)
}

func TestKVStoreStorageAdapter_EmptyKey(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	// Put with empty key
	err = adapter.Put(ctx, "", []byte("value"))
	require.NoError(t, err)

	// Get with empty key
	value, err := adapter.Get(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, []byte("value"), value)
}

func TestKVStoreStorageAdapter_BinaryData(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	// Test with binary data containing null bytes and special characters
	binaryData := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}

	err = adapter.Put(ctx, "binary-key", binaryData)
	require.NoError(t, err)

	value, err := adapter.Get(ctx, "binary-key")
	require.NoError(t, err)
	assert.Equal(t, binaryData, value)
}

func TestKVStoreStorageAdapter_LargeValue(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)

	// Test with a large value (1MB)
	largeValue := make([]byte, 1024*1024)
	for i := range largeValue {
		largeValue[i] = byte(i % 256)
	}

	err = adapter.Put(ctx, "large-key", largeValue)
	require.NoError(t, err)

	value, err := adapter.Get(ctx, "large-key")
	require.NoError(t, err)
	assert.Equal(t, largeValue, value)
}

// =============================================================================
// KeyPrefix Constant Test
// =============================================================================

func TestKeyPrefix(t *testing.T) {
	// Verify the KeyPrefix constant value
	assert.Equal(t, "kms/keys/", KeyPrefix)
}
