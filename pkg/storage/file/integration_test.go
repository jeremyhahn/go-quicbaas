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

package file

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-codec"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// codecTestCase defines a test case for codec verification.
type codecTestCase struct {
	name      string
	codecType codec.Type
	extension string
}

// supportedCodecs returns codec types that are currently supported.
// Note: CBOR and ProtoBuf are not yet available in go-codec v1.0.0.
func supportedCodecs() []codecTestCase {
	return []codecTestCase{
		{"JSON", codec.JSON, ".json"},
		{"YAML", codec.YAML, ".yaml"},
		{"TOML", codec.TOML, ".toml"},
		{"MsgPack", codec.MsgPack, ".msgpack"},
		{"BSON", codec.BSON, ".bson"},
	}
}

// allCodecExtensions returns all codec types including unsupported ones for extension testing.
func allCodecExtensions() []codecTestCase {
	return []codecTestCase{
		{"JSON", codec.JSON, ".json"},
		{"YAML", codec.YAML, ".yaml"},
		{"TOML", codec.TOML, ".toml"},
		{"MsgPack", codec.MsgPack, ".msgpack"},
		{"BSON", codec.BSON, ".bson"},
		{"ProtoBuf", codec.ProtoBuf, ".pb"},
		{"CBOR", codec.CBOR, ".cbor"},
	}
}

// newTestStorage creates a FileStorage for testing with the given codec type.
func newTestStorage(t *testing.T, dataDir string, codecType codec.Type) *FileStorage {
	t.Helper()

	log, err := logger.New(logger.Default())
	require.NoError(t, err, "failed to create logger")

	config := DefaultConfig()
	config.DataDir = dataDir
	config.CodecType = codecType
	config.SyncWrites = true

	storage, err := New(config, log)
	require.NoError(t, err, "failed to create storage")

	return storage
}

// TestIntegration_MultiCodecSupport verifies that all supported codec types
// work correctly and create files with the correct extensions.
func TestIntegration_MultiCodecSupport(t *testing.T) {
	for _, tc := range supportedCodecs() {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			storage := newTestStorage(t, dataDir, tc.codecType)
			defer storage.Close()

			// Test basic set and get
			key := []byte("test-key")
			value := []byte("test-value-for-" + tc.name)

			err := storage.Set(key, value)
			require.NoError(t, err, "Set failed for codec %s", tc.name)

			retrieved, err := storage.Get(key)
			require.NoError(t, err, "Get failed for codec %s", tc.name)
			assert.Equal(t, value, retrieved, "Value mismatch for codec %s", tc.name)

			// Verify file extension
			expectedPath := filepath.Join(dataDir, "test-key"+tc.extension)
			_, err = os.Stat(expectedPath)
			assert.NoError(t, err, "File with extension %s not found", tc.extension)

			// Verify no other extension files exist
			files, err := os.ReadDir(dataDir)
			require.NoError(t, err)
			assert.Len(t, files, 1, "Expected exactly one file")
			assert.True(t, strings.HasSuffix(files[0].Name(), tc.extension),
				"File should have extension %s, got %s", tc.extension, files[0].Name())
		})
	}
}

// TestIntegration_CodecExtensions verifies that ExtensionForCodec returns
// the correct extension for all codec types.
func TestIntegration_CodecExtensions(t *testing.T) {
	for _, tc := range allCodecExtensions() {
		t.Run(tc.name, func(t *testing.T) {
			ext := ExtensionForCodec(tc.codecType)
			assert.Equal(t, tc.extension, ext, "Extension mismatch for codec %s", tc.name)
		})
	}
}

// TestIntegration_AtomicWriteVerification verifies that writes are atomic
// and no partial writes occur on failure scenarios.
func TestIntegration_AtomicWriteVerification(t *testing.T) {
	t.Run("TempFileCleanupOnSuccess", func(t *testing.T) {
		dataDir := t.TempDir()
		storage := newTestStorage(t, dataDir, codec.JSON)
		defer storage.Close()

		key := []byte("atomic-key")
		value := []byte("atomic-value")

		err := storage.Set(key, value)
		require.NoError(t, err)

		// Verify no temp files remain
		err = filepath.Walk(dataDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if strings.HasSuffix(path, ".tmp") {
				t.Errorf("Temp file found after successful write: %s", path)
			}
			return nil
		})
		require.NoError(t, err)
	})

	t.Run("ConsistentStateOnMultipleWrites", func(t *testing.T) {
		dataDir := t.TempDir()
		storage := newTestStorage(t, dataDir, codec.JSON)
		defer storage.Close()

		key := []byte("consistent-key")

		// Write multiple values to the same key
		for i := 0; i < 100; i++ {
			value := []byte(fmt.Sprintf("value-%d", i))
			err := storage.Set(key, value)
			require.NoError(t, err)

			// Immediately read back and verify
			retrieved, err := storage.Get(key)
			require.NoError(t, err)
			assert.Equal(t, value, retrieved, "Value mismatch at iteration %d", i)
		}

		// Verify final value
		expected := []byte("value-99")
		retrieved, err := storage.Get(key)
		require.NoError(t, err)
		assert.Equal(t, expected, retrieved)
	})

	t.Run("NoPartialWritesVisible", func(t *testing.T) {
		dataDir := t.TempDir()
		storage := newTestStorage(t, dataDir, codec.JSON)
		defer storage.Close()

		key := []byte("partial-write-key")
		originalValue := []byte("original-value")
		largeValue := make([]byte, 1024*1024) // 1MB
		rand.Read(largeValue)

		// Set original value
		err := storage.Set(key, originalValue)
		require.NoError(t, err)

		// Update with large value
		err = storage.Set(key, largeValue)
		require.NoError(t, err)

		// Read should return complete new value or complete old value, never partial
		retrieved, err := storage.Get(key)
		require.NoError(t, err)

		isOriginal := bytes.Equal(retrieved, originalValue)
		isNew := bytes.Equal(retrieved, largeValue)
		assert.True(t, isOriginal || isNew, "Retrieved value is neither original nor new (partial write detected)")
		assert.True(t, isNew, "Expected new value after successful write")
	})
}

// TestIntegration_HierarchicalKeys verifies that hierarchical keys create
// the correct directory structure.
func TestIntegration_HierarchicalKeys(t *testing.T) {
	t.Run("DeeplyNestedKeys", func(t *testing.T) {
		dataDir := t.TempDir()
		storage := newTestStorage(t, dataDir, codec.JSON)
		defer storage.Close()

		// Test deeply nested key
		key := []byte("a/b/c/d/e/f/g/key")
		value := []byte("deeply-nested-value")

		err := storage.Set(key, value)
		require.NoError(t, err)

		// Verify directory structure
		expectedDirs := []string{"a", "a/b", "a/b/c", "a/b/c/d", "a/b/c/d/e", "a/b/c/d/e/f", "a/b/c/d/e/f/g"}
		for _, dir := range expectedDirs {
			dirPath := filepath.Join(dataDir, dir)
			info, err := os.Stat(dirPath)
			require.NoError(t, err, "Directory %s should exist", dir)
			assert.True(t, info.IsDir(), "%s should be a directory", dir)
		}

		// Verify file exists with correct extension
		expectedFile := filepath.Join(dataDir, "a/b/c/d/e/f/g/key.json")
		_, err = os.Stat(expectedFile)
		require.NoError(t, err, "File should exist at %s", expectedFile)

		// Verify value can be read back
		retrieved, err := storage.Get(key)
		require.NoError(t, err)
		assert.Equal(t, value, retrieved)
	})

	t.Run("MultipleHierarchicalKeys", func(t *testing.T) {
		dataDir := t.TempDir()
		storage := newTestStorage(t, dataDir, codec.JSON)
		defer storage.Close()

		keys := []string{
			"users/123/profile",
			"users/123/settings",
			"users/456/profile",
			"groups/admin/members",
			"groups/users/members",
			"config/database/primary",
			"config/database/replica",
		}

		// Set all keys
		for i, key := range keys {
			value := []byte(fmt.Sprintf("value-%d", i))
			err := storage.Set([]byte(key), value)
			require.NoError(t, err, "Failed to set key %s", key)
		}

		// Verify all keys can be read back
		for i, key := range keys {
			expected := []byte(fmt.Sprintf("value-%d", i))
			retrieved, err := storage.Get([]byte(key))
			require.NoError(t, err, "Failed to get key %s", key)
			assert.Equal(t, expected, retrieved, "Value mismatch for key %s", key)
		}

		// Verify scan returns all keys in sorted order
		results, err := storage.Scan(nil, nil)
		require.NoError(t, err)
		assert.Len(t, results, len(keys))

		// Extract returned keys
		var returnedKeys []string
		for _, kv := range results {
			returnedKeys = append(returnedKeys, string(kv[0]))
		}

		// Sort original keys for comparison
		sortedKeys := make([]string, len(keys))
		copy(sortedKeys, keys)
		sort.Strings(sortedKeys)

		assert.Equal(t, sortedKeys, returnedKeys, "Scan should return keys in sorted order")
	})

	t.Run("KeysWithSpecialCharacters", func(t *testing.T) {
		dataDir := t.TempDir()
		storage := newTestStorage(t, dataDir, codec.JSON)
		defer storage.Close()

		// Test keys with various characters
		testCases := []struct {
			key   string
			value string
		}{
			{"simple", "value1"},
			{"with-dash", "value2"},
			{"with_underscore", "value3"},
			{"with.dot", "value4"},
			{"nested/key/path", "value5"},
			{"deeply/nested/key/with-dashes/and_underscores", "value6"},
		}

		for _, tc := range testCases {
			err := storage.Set([]byte(tc.key), []byte(tc.value))
			require.NoError(t, err, "Failed to set key %s", tc.key)

			retrieved, err := storage.Get([]byte(tc.key))
			require.NoError(t, err, "Failed to get key %s", tc.key)
			assert.Equal(t, tc.value, string(retrieved), "Value mismatch for key %s", tc.key)
		}
	})
}

// TestIntegration_LargeDataHandling verifies the storage can handle large values
// and large numbers of keys.
func TestIntegration_LargeDataHandling(t *testing.T) {
	t.Run("LargeValueOneMB", func(t *testing.T) {
		dataDir := t.TempDir()
		storage := newTestStorage(t, dataDir, codec.JSON)
		defer storage.Close()

		key := []byte("large-value-key")
		value := make([]byte, 1024*1024) // 1MB
		rand.Read(value)

		err := storage.Set(key, value)
		require.NoError(t, err, "Failed to set large value")

		retrieved, err := storage.Get(key)
		require.NoError(t, err, "Failed to get large value")
		assert.Equal(t, value, retrieved, "Large value mismatch")
	})

	t.Run("ManyKeys", func(t *testing.T) {
		dataDir := t.TempDir()
		storage := newTestStorage(t, dataDir, codec.JSON)
		defer storage.Close()

		numKeys := 1000
		expectedValues := make(map[string][]byte)

		// Set many keys
		for i := 0; i < numKeys; i++ {
			key := fmt.Sprintf("key-%05d", i)
			value := []byte(fmt.Sprintf("value-for-%s", key))
			expectedValues[key] = value

			err := storage.Set([]byte(key), value)
			require.NoError(t, err, "Failed to set key %s", key)
		}

		// Verify length
		count, err := storage.Len()
		require.NoError(t, err)
		assert.Equal(t, numKeys, count, "Key count mismatch")

		// Verify all keys can be read back
		for key, expectedValue := range expectedValues {
			retrieved, err := storage.Get([]byte(key))
			require.NoError(t, err, "Failed to get key %s", key)
			assert.Equal(t, expectedValue, retrieved, "Value mismatch for key %s", key)
		}

		// Verify scan returns all keys
		results, err := storage.Scan(nil, nil)
		require.NoError(t, err)
		assert.Len(t, results, numKeys)
	})

	t.Run("LargeValuesWithManyKeys", func(t *testing.T) {
		dataDir := t.TempDir()
		storage := newTestStorage(t, dataDir, codec.JSON)
		defer storage.Close()

		numKeys := 100
		valueSize := 10 * 1024 // 10KB per value
		expectedValues := make(map[string][]byte)

		// Set many large values
		for i := 0; i < numKeys; i++ {
			key := fmt.Sprintf("large-key-%03d", i)
			value := make([]byte, valueSize)
			rand.Read(value)
			expectedValues[key] = value

			err := storage.Set([]byte(key), value)
			require.NoError(t, err, "Failed to set key %s", key)
		}

		// Verify all can be read back
		for key, expectedValue := range expectedValues {
			retrieved, err := storage.Get([]byte(key))
			require.NoError(t, err, "Failed to get key %s", key)
			assert.Equal(t, expectedValue, retrieved, "Value mismatch for key %s", key)
		}
	})
}

// TestIntegration_SnapshotRestoreCycle verifies snapshot creation and restoration
// across different storage instances.
func TestIntegration_SnapshotRestoreCycle(t *testing.T) {
	t.Run("BasicSnapshotRestore", func(t *testing.T) {
		// Create source storage and populate with data
		sourceDir := t.TempDir()
		sourceStorage := newTestStorage(t, sourceDir, codec.JSON)

		testData := map[string][]byte{
			"key1":           []byte("value1"),
			"key2":           []byte("value2"),
			"nested/key1":    []byte("nested-value1"),
			"nested/key2":    []byte("nested-value2"),
			"deep/nest/key1": []byte("deep-nested-value"),
		}

		for key, value := range testData {
			err := sourceStorage.Set([]byte(key), value)
			require.NoError(t, err)
		}

		// Create snapshot
		snapshot, err := sourceStorage.CreateSnapshotMap()
		require.NoError(t, err)
		assert.Len(t, snapshot, len(testData))

		// Close source storage
		err = sourceStorage.Close()
		require.NoError(t, err)

		// Create destination storage and restore
		destDir := t.TempDir()
		destStorage := newTestStorage(t, destDir, codec.JSON)
		defer destStorage.Close()

		err = destStorage.Restore(snapshot)
		require.NoError(t, err)

		// Verify all data was restored
		for key, expectedValue := range testData {
			retrieved, err := destStorage.Get([]byte(key))
			require.NoError(t, err, "Failed to get key %s after restore", key)
			assert.Equal(t, expectedValue, retrieved, "Value mismatch for key %s after restore", key)
		}

		// Verify length matches
		count, err := destStorage.Len()
		require.NoError(t, err)
		assert.Equal(t, len(testData), count)
	})

	t.Run("SnapshotRestoreWithExistingData", func(t *testing.T) {
		// Create source with data
		sourceDir := t.TempDir()
		sourceStorage := newTestStorage(t, sourceDir, codec.JSON)

		sourceData := map[string][]byte{
			"source-key1": []byte("source-value1"),
			"source-key2": []byte("source-value2"),
		}

		for key, value := range sourceData {
			err := sourceStorage.Set([]byte(key), value)
			require.NoError(t, err)
		}

		snapshot, err := sourceStorage.CreateSnapshotMap()
		require.NoError(t, err)
		require.NoError(t, sourceStorage.Close())

		// Create destination with existing data
		destDir := t.TempDir()
		destStorage := newTestStorage(t, destDir, codec.JSON)
		defer destStorage.Close()

		// Add existing data to destination
		err = destStorage.Set([]byte("existing-key"), []byte("existing-value"))
		require.NoError(t, err)

		// Restore should replace all existing data
		err = destStorage.Restore(snapshot)
		require.NoError(t, err)

		// Verify source data exists
		for key, expectedValue := range sourceData {
			retrieved, err := destStorage.Get([]byte(key))
			require.NoError(t, err)
			assert.Equal(t, expectedValue, retrieved)
		}

		// Verify existing data was cleared
		_, err = destStorage.Get([]byte("existing-key"))
		assert.Error(t, err, "Existing key should have been cleared")

		// Verify count matches source
		count, err := destStorage.Len()
		require.NoError(t, err)
		assert.Equal(t, len(sourceData), count)
	})

	t.Run("SnapshotRestoreLargeDataset", func(t *testing.T) {
		sourceDir := t.TempDir()
		sourceStorage := newTestStorage(t, sourceDir, codec.JSON)

		numKeys := 500
		testData := make(map[string][]byte)

		for i := 0; i < numKeys; i++ {
			key := fmt.Sprintf("snap-key-%04d", i)
			value := make([]byte, 1024) // 1KB values
			rand.Read(value)
			testData[key] = value
			err := sourceStorage.Set([]byte(key), value)
			require.NoError(t, err)
		}

		snapshot, err := sourceStorage.CreateSnapshotMap()
		require.NoError(t, err)
		assert.Len(t, snapshot, numKeys)
		sourceStorage.Close()

		// Restore to new storage
		destDir := t.TempDir()
		destStorage := newTestStorage(t, destDir, codec.JSON)
		defer destStorage.Close()

		err = destStorage.Restore(snapshot)
		require.NoError(t, err)

		// Verify all data
		for key, expectedValue := range testData {
			retrieved, err := destStorage.Get([]byte(key))
			require.NoError(t, err)
			assert.Equal(t, expectedValue, retrieved)
		}
	})
}

// TestIntegration_PermissionHandling verifies custom file and directory permissions.
func TestIntegration_PermissionHandling(t *testing.T) {
	t.Run("CustomFilePermissions", func(t *testing.T) {
		dataDir := t.TempDir()

		log, err := logger.New(logger.Default())
		require.NoError(t, err)

		config := DefaultConfig()
		config.DataDir = dataDir
		config.CodecType = codec.JSON
		config.FilePermissions = 0600 // Read/write for owner only
		config.DirPermissions = 0700  // Read/write/execute for owner only

		storage, err := New(config, log)
		require.NoError(t, err)
		defer storage.Close()

		key := []byte("permission-test-key")
		value := []byte("permission-test-value")

		err = storage.Set(key, value)
		require.NoError(t, err)

		// Verify file permissions
		filePath := filepath.Join(dataDir, "permission-test-key.json")
		info, err := os.Stat(filePath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), "File permissions mismatch")
	})

	t.Run("CustomDirPermissions", func(t *testing.T) {
		dataDir := t.TempDir()

		log, err := logger.New(logger.Default())
		require.NoError(t, err)

		config := DefaultConfig()
		config.DataDir = dataDir
		config.CodecType = codec.JSON
		config.FilePermissions = 0644
		config.DirPermissions = 0750 // Custom directory permissions

		storage, err := New(config, log)
		require.NoError(t, err)
		defer storage.Close()

		key := []byte("nested/dir/permission-key")
		value := []byte("nested-permission-value")

		err = storage.Set(key, value)
		require.NoError(t, err)

		// Verify nested directory permissions
		nestedDir := filepath.Join(dataDir, "nested", "dir")
		info, err := os.Stat(nestedDir)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0750), info.Mode().Perm(), "Directory permissions mismatch")
	})

	t.Run("RestrictivePermissions", func(t *testing.T) {
		dataDir := t.TempDir()

		log, err := logger.New(logger.Default())
		require.NoError(t, err)

		config := DefaultConfig()
		config.DataDir = dataDir
		config.CodecType = codec.JSON
		config.FilePermissions = 0400 // Read-only for owner
		config.DirPermissions = 0500  // Read/execute for owner

		storage, err := New(config, log)
		require.NoError(t, err)
		defer storage.Close()

		key := []byte("readonly-key")
		value := []byte("readonly-value")

		err = storage.Set(key, value)
		require.NoError(t, err)

		// Read should still work
		retrieved, err := storage.Get(key)
		require.NoError(t, err)
		assert.Equal(t, value, retrieved)
	})
}

// TestIntegration_StressTest verifies concurrent read and write operations.
func TestIntegration_StressTest(t *testing.T) {
	t.Run("ConcurrentWrites", func(t *testing.T) {
		dataDir := t.TempDir()
		storage := newTestStorage(t, dataDir, codec.JSON)
		defer storage.Close()

		numGoroutines := 10
		writesPerGoroutine := 100
		var wg sync.WaitGroup
		var errorCount atomic.Int64

		for g := 0; g < numGoroutines; g++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()
				for i := 0; i < writesPerGoroutine; i++ {
					key := fmt.Sprintf("goroutine-%d-key-%03d", goroutineID, i)
					value := fmt.Sprintf("goroutine-%d-value-%03d", goroutineID, i)
					if err := storage.Set([]byte(key), []byte(value)); err != nil {
						errorCount.Add(1)
					}
				}
			}(g)
		}

		wg.Wait()
		assert.Equal(t, int64(0), errorCount.Load(), "Concurrent writes should not fail")

		// Verify total count
		expectedCount := numGoroutines * writesPerGoroutine
		count, err := storage.Len()
		require.NoError(t, err)
		assert.Equal(t, expectedCount, count, "Total key count mismatch after concurrent writes")
	})

	t.Run("ConcurrentReads", func(t *testing.T) {
		dataDir := t.TempDir()
		storage := newTestStorage(t, dataDir, codec.JSON)
		defer storage.Close()

		// Pre-populate data
		numKeys := 100
		testData := make(map[string][]byte)
		for i := 0; i < numKeys; i++ {
			key := fmt.Sprintf("read-key-%03d", i)
			value := []byte(fmt.Sprintf("read-value-%03d", i))
			testData[key] = value
			err := storage.Set([]byte(key), value)
			require.NoError(t, err)
		}

		// Concurrent reads
		numReaders := 20
		readsPerReader := 50
		var wg sync.WaitGroup
		var errorCount atomic.Int64
		var mismatchCount atomic.Int64

		for r := 0; r < numReaders; r++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < readsPerReader; i++ {
					keyIdx := i % numKeys
					key := fmt.Sprintf("read-key-%03d", keyIdx)
					retrieved, err := storage.Get([]byte(key))
					if err != nil {
						errorCount.Add(1)
						continue
					}
					expectedValue := testData[key]
					if !bytes.Equal(retrieved, expectedValue) {
						mismatchCount.Add(1)
					}
				}
			}()
		}

		wg.Wait()
		assert.Equal(t, int64(0), errorCount.Load(), "Concurrent reads should not fail")
		assert.Equal(t, int64(0), mismatchCount.Load(), "Concurrent reads should return correct values")
	})

	t.Run("ConcurrentReadWrite", func(t *testing.T) {
		dataDir := t.TempDir()
		storage := newTestStorage(t, dataDir, codec.JSON)
		defer storage.Close()

		numWriters := 5
		numReaders := 10
		operationsPerGoroutine := 50
		var wg sync.WaitGroup
		var writeErrors atomic.Int64
		var readErrors atomic.Int64

		// Writers
		for w := 0; w < numWriters; w++ {
			wg.Add(1)
			go func(writerID int) {
				defer wg.Done()
				for i := 0; i < operationsPerGoroutine; i++ {
					key := fmt.Sprintf("shared-key-%03d", i%20) // Shared keys to create contention
					value := fmt.Sprintf("writer-%d-iteration-%d", writerID, i)
					if err := storage.Set([]byte(key), []byte(value)); err != nil {
						writeErrors.Add(1)
					}
				}
			}(w)
		}

		// Readers
		for r := 0; r < numReaders; r++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < operationsPerGoroutine; i++ {
					key := fmt.Sprintf("shared-key-%03d", i%20)
					_, err := storage.Get([]byte(key))
					// NotFound is acceptable for keys not yet written.
					if err != nil && !errors.IsNotFound(err) {
						readErrors.Add(1)
					}
				}
			}()
		}

		wg.Wait()
		assert.Equal(t, int64(0), writeErrors.Load(), "Concurrent writes should not fail")
		assert.Equal(t, int64(0), readErrors.Load(), "Concurrent reads should fail only with not-found")
	})

	t.Run("ConcurrentScans", func(t *testing.T) {
		dataDir := t.TempDir()
		storage := newTestStorage(t, dataDir, codec.JSON)
		defer storage.Close()

		// Pre-populate data
		numKeys := 50
		for i := 0; i < numKeys; i++ {
			key := fmt.Sprintf("scan-key-%03d", i)
			value := []byte(fmt.Sprintf("scan-value-%03d", i))
			err := storage.Set([]byte(key), value)
			require.NoError(t, err)
		}

		// Concurrent scans with writes
		numScanners := 5
		numWriters := 3
		operationsPerGoroutine := 20
		var wg sync.WaitGroup
		var scanErrors atomic.Int64
		var writeErrors atomic.Int64

		// Scanners
		for s := 0; s < numScanners; s++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < operationsPerGoroutine; i++ {
					_, err := storage.Scan(nil, nil)
					if err != nil {
						scanErrors.Add(1)
					}
				}
			}()
		}

		// Writers
		for w := 0; w < numWriters; w++ {
			wg.Add(1)
			go func(writerID int) {
				defer wg.Done()
				for i := 0; i < operationsPerGoroutine; i++ {
					key := fmt.Sprintf("new-scan-key-%d-%03d", writerID, i)
					value := []byte(fmt.Sprintf("new-value-%d-%03d", writerID, i))
					if err := storage.Set([]byte(key), value); err != nil {
						writeErrors.Add(1)
					}
				}
			}(w)
		}

		wg.Wait()
		assert.Equal(t, int64(0), scanErrors.Load(), "Concurrent scans should not fail")
		assert.Equal(t, int64(0), writeErrors.Load(), "Concurrent writes during scans should not fail")
	})

	t.Run("ConcurrentDeletes", func(t *testing.T) {
		dataDir := t.TempDir()
		storage := newTestStorage(t, dataDir, codec.JSON)
		defer storage.Close()

		numKeys := 100
		// Pre-populate
		for i := 0; i < numKeys; i++ {
			key := fmt.Sprintf("delete-key-%03d", i)
			value := []byte(fmt.Sprintf("delete-value-%03d", i))
			err := storage.Set([]byte(key), value)
			require.NoError(t, err)
		}

		// Concurrent deletes
		numDeleters := 10
		var wg sync.WaitGroup
		var deleteErrors atomic.Int64

		for d := 0; d < numDeleters; d++ {
			wg.Add(1)
			go func(deleterID int) {
				defer wg.Done()
				for i := deleterID; i < numKeys; i += numDeleters {
					key := fmt.Sprintf("delete-key-%03d", i)
					if err := storage.Delete([]byte(key)); err != nil {
						deleteErrors.Add(1)
					}
				}
			}(d)
		}

		wg.Wait()
		assert.Equal(t, int64(0), deleteErrors.Load(), "Concurrent deletes should not fail")

		// Verify all keys deleted
		count, err := storage.Len()
		require.NoError(t, err)
		assert.Equal(t, 0, count, "All keys should be deleted")
	})
}

// TestIntegration_ScanRanges verifies scan operations with various ranges.
func TestIntegration_ScanRanges(t *testing.T) {
	dataDir := t.TempDir()
	storage := newTestStorage(t, dataDir, codec.JSON)
	defer storage.Close()

	// Pre-populate with sorted keys
	keys := []string{"aaa", "bbb", "ccc", "ddd", "eee", "fff", "ggg", "hhh", "iii", "jjj"}
	for i, key := range keys {
		err := storage.Set([]byte(key), []byte(fmt.Sprintf("value-%d", i)))
		require.NoError(t, err)
	}

	t.Run("FullScan", func(t *testing.T) {
		results, err := storage.Scan(nil, nil)
		require.NoError(t, err)
		assert.Len(t, results, len(keys))
	})

	t.Run("ScanFromStart", func(t *testing.T) {
		results, err := storage.Scan([]byte("ccc"), nil)
		require.NoError(t, err)
		assert.Len(t, results, 8) // ccc to jjj inclusive

		// First key should be "ccc"
		assert.Equal(t, "ccc", string(results[0][0]))
	})

	t.Run("ScanToEnd", func(t *testing.T) {
		results, err := storage.Scan(nil, []byte("fff"))
		require.NoError(t, err)
		assert.Len(t, results, 5) // aaa to eee (exclusive of fff)

		// Last key should be "eee"
		assert.Equal(t, "eee", string(results[len(results)-1][0]))
	})

	t.Run("ScanRange", func(t *testing.T) {
		results, err := storage.Scan([]byte("ccc"), []byte("ggg"))
		require.NoError(t, err)
		assert.Len(t, results, 4) // ccc, ddd, eee, fff (exclusive of ggg)

		var resultKeys []string
		for _, kv := range results {
			resultKeys = append(resultKeys, string(kv[0]))
		}
		assert.Equal(t, []string{"ccc", "ddd", "eee", "fff"}, resultKeys)
	})

	t.Run("ScanEmptyRange", func(t *testing.T) {
		results, err := storage.Scan([]byte("zzz"), nil)
		require.NoError(t, err)
		assert.Len(t, results, 0)
	})
}

// TestIntegration_DeleteBehavior verifies delete operation behavior.
func TestIntegration_DeleteBehavior(t *testing.T) {
	t.Run("DeleteExistingKey", func(t *testing.T) {
		dataDir := t.TempDir()
		storage := newTestStorage(t, dataDir, codec.JSON)
		defer storage.Close()

		key := []byte("delete-me")
		value := []byte("to-be-deleted")

		err := storage.Set(key, value)
		require.NoError(t, err)

		err = storage.Delete(key)
		require.NoError(t, err)

		_, err = storage.Get(key)
		assert.Error(t, err, "Key should not exist after delete")
	})

	t.Run("DeleteNonExistentKey", func(t *testing.T) {
		dataDir := t.TempDir()
		storage := newTestStorage(t, dataDir, codec.JSON)
		defer storage.Close()

		// Delete should be idempotent - no error for non-existent key
		err := storage.Delete([]byte("non-existent-key"))
		assert.NoError(t, err, "Delete of non-existent key should not error")
	})

	t.Run("DeleteAndRecreate", func(t *testing.T) {
		dataDir := t.TempDir()
		storage := newTestStorage(t, dataDir, codec.JSON)
		defer storage.Close()

		key := []byte("recreate-key")
		value1 := []byte("first-value")
		value2 := []byte("second-value")

		err := storage.Set(key, value1)
		require.NoError(t, err)

		err = storage.Delete(key)
		require.NoError(t, err)

		err = storage.Set(key, value2)
		require.NoError(t, err)

		retrieved, err := storage.Get(key)
		require.NoError(t, err)
		assert.Equal(t, value2, retrieved)
	})
}

// TestIntegration_ClosedStorageBehavior verifies operations on closed storage.
func TestIntegration_ClosedStorageBehavior(t *testing.T) {
	dataDir := t.TempDir()
	storage := newTestStorage(t, dataDir, codec.JSON)

	// Pre-populate
	err := storage.Set([]byte("key"), []byte("value"))
	require.NoError(t, err)

	// Close storage
	err = storage.Close()
	require.NoError(t, err)

	t.Run("GetOnClosed", func(t *testing.T) {
		_, err := storage.Get([]byte("key"))
		assert.Error(t, err, "Get on closed storage should error")
	})

	t.Run("SetOnClosed", func(t *testing.T) {
		err := storage.Set([]byte("new-key"), []byte("new-value"))
		assert.Error(t, err, "Set on closed storage should error")
	})

	t.Run("DeleteOnClosed", func(t *testing.T) {
		err := storage.Delete([]byte("key"))
		assert.Error(t, err, "Delete on closed storage should error")
	})

	t.Run("ScanOnClosed", func(t *testing.T) {
		_, err := storage.Scan(nil, nil)
		assert.Error(t, err, "Scan on closed storage should error")
	})

	t.Run("SnapshotOnClosed", func(t *testing.T) {
		_, err := storage.Snapshot()
		assert.Error(t, err, "Snapshot on closed storage should error")
	})

	t.Run("RestoreOnClosed", func(t *testing.T) {
		err := storage.Restore(map[string][]byte{"k": []byte("v")})
		assert.Error(t, err, "Restore on closed storage should error")
	})

	t.Run("LenOnClosed", func(t *testing.T) {
		_, err := storage.Len()
		assert.Error(t, err, "Len on closed storage should error")
	})

	t.Run("CloseIdempotent", func(t *testing.T) {
		// Closing again should not error
		err := storage.Close()
		assert.NoError(t, err, "Second close should be idempotent")
	})
}
