//go:build raft

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

package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestFileKVStore_NewFileKVStore(t *testing.T) {
	t.Run("creates data directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		dataDir := filepath.Join(tmpDir, "subdir", "data")

		store, err := NewFileKVStore(dataDir)
		require.NoError(t, err)
		require.NotNil(t, store)

		// Verify directory was created
		info, err := os.Stat(dataDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("returns error for empty data dir", func(t *testing.T) {
		store, err := NewFileKVStore("")
		assert.Error(t, err)
		assert.Nil(t, store)

		var dragonErr *dberrors.DragonError
		assert.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
	})
}

func TestFileKVStore_PutGet(t *testing.T) {
	ctx := context.Background()
	store := createTestFileStore(t)

	t.Run("stores and retrieves value", func(t *testing.T) {
		key := "test-key"
		value := []byte("test-value")

		err := store.Put(ctx, key, value)
		require.NoError(t, err)

		retrieved, err := store.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value, retrieved)
	})

	t.Run("overwrites existing value", func(t *testing.T) {
		key := "overwrite-key"
		value1 := []byte("value-1")
		value2 := []byte("value-2")

		err := store.Put(ctx, key, value1)
		require.NoError(t, err)

		err = store.Put(ctx, key, value2)
		require.NoError(t, err)

		retrieved, err := store.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value2, retrieved)
	})

	t.Run("returns error for non-existent key", func(t *testing.T) {
		_, err := store.Get(ctx, "non-existent-key")
		assert.Error(t, err)

		var dragonErr *dberrors.DragonError
		assert.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, dberrors.ErrNotFound, dragonErr.Code)
	})
}

func TestFileKVStore_Delete(t *testing.T) {
	ctx := context.Background()
	store := createTestFileStore(t)

	t.Run("deletes existing key", func(t *testing.T) {
		key := "delete-key"
		value := []byte("delete-value")

		err := store.Put(ctx, key, value)
		require.NoError(t, err)

		err = store.Delete(ctx, key)
		require.NoError(t, err)

		_, err = store.Get(ctx, key)
		assert.Error(t, err)
	})

	t.Run("no error for non-existent key", func(t *testing.T) {
		err := store.Delete(ctx, "non-existent-delete-key")
		assert.NoError(t, err)
	})
}

func TestFileKVStore_Scan(t *testing.T) {
	ctx := context.Background()
	store := createTestFileStore(t)

	// Populate store with test data
	testData := map[string]string{
		"prefix:a": "value-a",
		"prefix:b": "value-b",
		"prefix:c": "value-c",
		"other:x":  "value-x",
	}

	for k, v := range testData {
		err := store.Put(ctx, k, []byte(v))
		require.NoError(t, err)
	}

	t.Run("scans keys with prefix", func(t *testing.T) {
		var found []string
		err := store.Scan(ctx, "prefix:", func(key string, value []byte) error {
			found = append(found, key)
			return nil
		})
		require.NoError(t, err)
		assert.Len(t, found, 3)
	})

	t.Run("scans all keys with empty prefix", func(t *testing.T) {
		var found []string
		err := store.Scan(ctx, "", func(key string, value []byte) error {
			found = append(found, key)
			return nil
		})
		require.NoError(t, err)
		assert.Len(t, found, 4)
	})

	t.Run("returns empty for non-matching prefix", func(t *testing.T) {
		var found []string
		err := store.Scan(ctx, "nonexistent:", func(key string, value []byte) error {
			found = append(found, key)
			return nil
		})
		require.NoError(t, err)
		assert.Empty(t, found)
	})
}

func TestFileKVStore_List(t *testing.T) {
	ctx := context.Background()
	store := createTestFileStore(t)

	// Populate store with test data
	testData := map[string]string{
		"list:a":  "value-a",
		"list:b":  "value-b",
		"list:c":  "value-c",
		"other:y": "value-y",
	}

	for k, v := range testData {
		err := store.Put(ctx, k, []byte(v))
		require.NoError(t, err)
	}

	t.Run("lists keys with prefix", func(t *testing.T) {
		keys, err := store.List(ctx, "list:")
		require.NoError(t, err)
		assert.Len(t, keys, 3)
	})

	t.Run("lists all keys with empty prefix", func(t *testing.T) {
		keys, err := store.List(ctx, "")
		require.NoError(t, err)
		assert.Len(t, keys, 4)
	})

	t.Run("returns empty for non-matching prefix", func(t *testing.T) {
		keys, err := store.List(ctx, "nonexistent:")
		require.NoError(t, err)
		assert.Empty(t, keys)
	})
}

func TestFileKVStore_Close(t *testing.T) {
	store := createTestFileStore(t)

	err := store.Close()
	assert.NoError(t, err)
}

func TestFileKVStore_DataDir(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileKVStore(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, tmpDir, store.DataDir())
}

func TestFileKVStore_KeyEncoding(t *testing.T) {
	ctx := context.Background()
	store := createTestFileStore(t)

	t.Run("handles special characters in keys", func(t *testing.T) {
		// Keys with special characters that would be invalid filenames
		specialKeys := []string{
			"key/with/slashes",
			"key:with:colons",
			"key with spaces",
			"key\x00with\x00nulls",
		}

		for _, key := range specialKeys {
			value := []byte("value for: " + key)

			err := store.Put(ctx, key, value)
			require.NoError(t, err, "failed to put key: %q", key)

			retrieved, err := store.Get(ctx, key)
			require.NoError(t, err, "failed to get key: %q", key)
			assert.Equal(t, value, retrieved)
		}
	})

	t.Run("handles binary-like keys", func(t *testing.T) {
		binaryKey := string([]byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0xfd})
		value := []byte("binary key value")

		err := store.Put(ctx, binaryKey, value)
		require.NoError(t, err)

		retrieved, err := store.Get(ctx, binaryKey)
		require.NoError(t, err)
		assert.Equal(t, value, retrieved)
	})
}

func TestFileKVStore_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	// Create store and write data
	store1, err := NewFileKVStore(tmpDir)
	require.NoError(t, err)

	key := "persistent-key"
	value := []byte("persistent-value")

	err = store1.Put(ctx, key, value)
	require.NoError(t, err)

	// Close first store (simulating shutdown)
	err = store1.Close()
	require.NoError(t, err)

	// Create new store pointing to same directory
	store2, err := NewFileKVStore(tmpDir)
	require.NoError(t, err)

	// Verify data persisted
	retrieved, err := store2.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, value, retrieved)
}

func TestFileKVStore_Exists(t *testing.T) {
	ctx := context.Background()
	store := createTestFileStore(t)

	t.Run("returns true for existing key", func(t *testing.T) {
		key := "exists-key"
		err := store.Put(ctx, key, []byte("value"))
		require.NoError(t, err)

		exists, err := store.Exists(ctx, key)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("returns false for non-existent key", func(t *testing.T) {
		exists, err := store.Exists(ctx, "non-existent-key")
		require.NoError(t, err)
		assert.False(t, exists)
	})
}

// createTestFileStore creates a FileKVStore for testing.
func createTestFileStore(t *testing.T) *FileKVStore {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := NewFileKVStore(tmpDir)
	require.NoError(t, err)
	t.Cleanup(func() {
		store.Close()
	})
	return store
}
