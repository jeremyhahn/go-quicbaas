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

package main

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage/memory"
)

func TestCreateStorageFromURL(t *testing.T) {
	testLog := createTestLoggerForMigrate(t)
	log = testLog

	t.Run("MemoryStorage", func(t *testing.T) {
		storage, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		require.NotNil(t, storage)
		defer storage.Close()
	})

	t.Run("PebbleStorage", func(t *testing.T) {
		tempDir := t.TempDir()
		_, err := createStorageFromURL("pebble://" + tempDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported storage scheme")
	})

	t.Run("FileStorage", func(t *testing.T) {
		tempDir := t.TempDir()
		_, err := createStorageFromURL("file://" + tempDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported storage scheme")
	})

	t.Run("InvalidURL", func(t *testing.T) {
		_, err := createStorageFromURL("invalid_url")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid storage URL format")
	})

	t.Run("UnsupportedScheme", func(t *testing.T) {
		_, err := createStorageFromURL("redis://localhost:6379")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported storage scheme")
	})

	t.Run("EmptyURL", func(t *testing.T) {
		_, err := createStorageFromURL("")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid storage URL format")
	})
}

// createTestLoggerForMigrate creates a logger for migration tests.
func createTestLoggerForMigrate(t *testing.T) *logger.Logger {
	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	if err != nil {
		t.Fatalf("Failed to create test logger: %v", err)
	}
	return testLog
}

// TestMemoryBatchOperations tests the memoryBatch implementation.
func TestMemoryBatchOperations(t *testing.T) {
	testLog := createTestLoggerForMigrate(t)
	log = testLog

	t.Run("Set and Commit", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		// Use Batch to get the batch
		batch := store.Batch()
		require.NotNil(t, batch)

		err = batch.Set([]byte("key1"), []byte("value1"))
		assert.NoError(t, err)

		err = batch.Set([]byte("key2"), []byte("value2"))
		assert.NoError(t, err)

		err = batch.Commit()
		assert.NoError(t, err)

		// Verify values were written
		val1, err := store.Get([]byte("key1"))
		assert.NoError(t, err)
		assert.Equal(t, []byte("value1"), val1)

		val2, err := store.Get([]byte("key2"))
		assert.NoError(t, err)
		assert.Equal(t, []byte("value2"), val2)
	})

	t.Run("Delete and Commit", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		// Set initial value
		err = store.Set([]byte("delete-key"), []byte("value"))
		require.NoError(t, err)

		batch := store.Batch()
		err = batch.Delete([]byte("delete-key"))
		assert.NoError(t, err)

		err = batch.Commit()
		assert.NoError(t, err)

		// Verify key was deleted
		_, err = store.Get([]byte("delete-key"))
		assert.Error(t, err) // Key should not exist
	})

	t.Run("Rollback", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		batch := store.Batch()
		err = batch.Set([]byte("rollback-key"), []byte("value"))
		assert.NoError(t, err)

		err = batch.Rollback()
		assert.NoError(t, err)

		// Verify nothing was written
		_, err = store.Get([]byte("rollback-key"))
		assert.Error(t, err)
	})

	t.Run("Close", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		batch := store.Batch()
		err = batch.Set([]byte("close-key"), []byte("value"))
		assert.NoError(t, err)

		err = batch.Close()
		assert.NoError(t, err)

		// Verify nothing was written (close calls rollback)
		_, err = store.Get([]byte("close-key"))
		assert.Error(t, err)
	})
}

// TestMemoryIteratorOperations tests the memoryIterator implementation.
func TestMemoryIteratorOperations(t *testing.T) {
	testLog := createTestLoggerForMigrate(t)
	log = testLog

	iterOpts := &storage.IteratorOptions{
		LowerBound: nil,
		UpperBound: nil,
	}

	t.Run("SeekToFirst and Next", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		// Add test data
		require.NoError(t, store.Set([]byte("a"), []byte("1")))
		require.NoError(t, store.Set([]byte("b"), []byte("2")))
		require.NoError(t, store.Set([]byte("c"), []byte("3")))

		iter := store.NewIterator(iterOpts)
		require.NotNil(t, iter)
		defer iter.Close()

		assert.True(t, iter.SeekToFirst())
		assert.True(t, iter.Valid())

		// Iterate through all items
		count := 1
		for iter.Next() {
			count++
		}
		assert.GreaterOrEqual(t, count, 3)
	})

	t.Run("SeekToLast and Prev", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		// Add test data
		require.NoError(t, store.Set([]byte("x"), []byte("1")))
		require.NoError(t, store.Set([]byte("y"), []byte("2")))
		require.NoError(t, store.Set([]byte("z"), []byte("3")))

		iter := store.NewIterator(iterOpts)
		require.NotNil(t, iter)
		defer iter.Close()

		assert.True(t, iter.SeekToLast())
		assert.True(t, iter.Valid())

		// Iterate backwards
		count := 1
		for iter.Prev() {
			count++
		}
		assert.GreaterOrEqual(t, count, 3)
	})

	t.Run("Seek", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		// Add test data
		require.NoError(t, store.Set([]byte("key1"), []byte("val1")))
		require.NoError(t, store.Set([]byte("key2"), []byte("val2")))
		require.NoError(t, store.Set([]byte("key3"), []byte("val3")))

		iter := store.NewIterator(iterOpts)
		require.NotNil(t, iter)
		defer iter.Close()

		found := iter.Seek([]byte("key2"))
		assert.True(t, found)
		assert.True(t, iter.Valid())
	})

	t.Run("Key and Value", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		// Add test data
		require.NoError(t, store.Set([]byte("testkey"), []byte("testvalue")))

		iter := store.NewIterator(iterOpts)
		require.NotNil(t, iter)
		defer iter.Close()

		assert.True(t, iter.SeekToFirst())
		assert.True(t, iter.Valid())
		assert.NotNil(t, iter.Key())
		assert.NotNil(t, iter.Value())
	})

	t.Run("Error on empty storage", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		iter := store.NewIterator(iterOpts)
		require.NotNil(t, iter)
		defer iter.Close()

		// Empty storage should have no error but no valid position
		valid := iter.SeekToFirst()
		assert.False(t, valid)
		assert.False(t, iter.Valid())
		assert.Nil(t, iter.Key())
		assert.Nil(t, iter.Value())
		assert.NoError(t, iter.Error())
	})
}

// TestMemorySnapshotOperations tests the memorySnapshot implementation.
func TestMemorySnapshotOperations(t *testing.T) {
	testLog := createTestLoggerForMigrate(t)
	log = testLog

	iterOpts := &storage.IteratorOptions{}

	t.Run("Get existing key", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		// Add test data
		require.NoError(t, store.Set([]byte("snap-key"), []byte("snap-value")))

		snapshot, err := store.Snapshot()
		require.NoError(t, err)
		defer snapshot.Close()

		val, err := snapshot.Get([]byte("snap-key"))
		assert.NoError(t, err)
		assert.Equal(t, []byte("snap-value"), val)
	})

	t.Run("Get non-existing key", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		snapshot, err := store.Snapshot()
		require.NoError(t, err)
		defer snapshot.Close()

		_, err = snapshot.Get([]byte("nonexistent"))
		assert.Error(t, err)
	})

	t.Run("Snapshot iterator SeekToFirst and Next", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		// Add test data
		require.NoError(t, store.Set([]byte("key1"), []byte("val1")))
		require.NoError(t, store.Set([]byte("key2"), []byte("val2")))
		require.NoError(t, store.Set([]byte("key3"), []byte("val3")))

		snapshot, err := store.Snapshot()
		require.NoError(t, err)
		defer snapshot.Close()

		iter := snapshot.NewIterator(iterOpts)
		require.NotNil(t, iter)
		defer iter.Close()

		assert.True(t, iter.SeekToFirst())
		assert.True(t, iter.Valid())
		assert.NotNil(t, iter.Key())
		assert.NotNil(t, iter.Value())

		// Iterate through all items
		count := 1
		for iter.Next() {
			count++
		}
		assert.Equal(t, 3, count)
	})

	t.Run("Snapshot iterator SeekToLast and Prev", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		// Add test data
		require.NoError(t, store.Set([]byte("a"), []byte("1")))
		require.NoError(t, store.Set([]byte("b"), []byte("2")))
		require.NoError(t, store.Set([]byte("c"), []byte("3")))

		snapshot, err := store.Snapshot()
		require.NoError(t, err)
		defer snapshot.Close()

		iter := snapshot.NewIterator(iterOpts)
		require.NotNil(t, iter)
		defer iter.Close()

		assert.True(t, iter.SeekToLast())
		assert.True(t, iter.Valid())

		// Iterate backwards
		count := 1
		for iter.Prev() {
			count++
		}
		assert.Equal(t, 3, count)
	})

	t.Run("Snapshot iterator Seek", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		// Add test data
		require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
		require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
		require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

		snapshot, err := store.Snapshot()
		require.NoError(t, err)
		defer snapshot.Close()

		iter := snapshot.NewIterator(iterOpts)
		require.NotNil(t, iter)
		defer iter.Close()

		// Seek should find the target or greater
		found := iter.Seek([]byte("bbb"))
		assert.True(t, found)
		assert.True(t, iter.Valid())
	})

	t.Run("Snapshot iterator Seek not found", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		// Add test data
		require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
		require.NoError(t, store.Set([]byte("bbb"), []byte("2")))

		snapshot, err := store.Snapshot()
		require.NoError(t, err)
		defer snapshot.Close()

		iter := snapshot.NewIterator(iterOpts)
		require.NotNil(t, iter)
		defer iter.Close()

		// Seek for key greater than all existing keys
		found := iter.Seek([]byte("zzz"))
		assert.False(t, found)
	})

	t.Run("Snapshot iterator Error and Close", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		require.NoError(t, store.Set([]byte("test"), []byte("val")))

		snapshot, err := store.Snapshot()
		require.NoError(t, err)
		defer snapshot.Close()

		iter := snapshot.NewIterator(iterOpts)
		require.NotNil(t, iter)

		assert.NoError(t, iter.Error())
		assert.NoError(t, iter.Close())
	})

	t.Run("Empty snapshot iterator", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		snapshot, err := store.Snapshot()
		require.NoError(t, err)
		defer snapshot.Close()

		iter := snapshot.NewIterator(iterOpts)
		require.NotNil(t, iter)
		defer iter.Close()

		assert.False(t, iter.SeekToFirst())
		assert.False(t, iter.Valid())
		assert.Nil(t, iter.Key())
		assert.Nil(t, iter.Value())
	})
}

// TestMemoryStorageAdapterMethods tests individual adapter methods.
func TestMemoryStorageAdapterMethods(t *testing.T) {
	testLog := createTestLoggerForMigrate(t)
	log = testLog

	t.Run("GetWithContext", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		ctx := context.Background()
		key := []byte("context-key")
		value := []byte("context-value")

		err = store.Set(key, value)
		require.NoError(t, err)

		result, err := store.GetWithContext(ctx, key)
		assert.NoError(t, err)
		assert.Equal(t, value, result)
	})

	t.Run("SetWithContext", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		ctx := context.Background()
		key := []byte("ctx-set-key")
		value := []byte("ctx-set-value")

		err = store.SetWithContext(ctx, key, value)
		assert.NoError(t, err)

		result, err := store.Get(key)
		assert.NoError(t, err)
		assert.Equal(t, value, result)
	})

	t.Run("Compact", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		// Compact should be a no-op for memory storage
		err = store.Compact([]byte("a"), []byte("z"))
		assert.NoError(t, err)
	})

	t.Run("Flush", func(t *testing.T) {
		store, err := createStorageFromURL("memory://")
		require.NoError(t, err)
		defer store.Close()

		// Flush should be a no-op for memory storage
		err = store.Flush()
		assert.NoError(t, err)
	})
}

// TestDisplayMigrationProgress tests the displayMigrationProgress function.
func TestDisplayMigrationProgress(t *testing.T) {
	tests := []struct {
		name     string
		progress *MigrationProgress
	}{
		{
			name: "With estimate",
			progress: &MigrationProgress{
				KeysProcessed:      1000,
				BytesProcessed:     10240,
				ElapsedTime:        time.Minute,
				CurrentRate:        100.5,
				EstimatedTotal:     5000,
				PercentComplete:    20.0,
				EstimatedRemaining: time.Minute * 4,
			},
		},
		{
			name: "Without estimate",
			progress: &MigrationProgress{
				KeysProcessed:  500,
				BytesProcessed: 5120,
				ElapsedTime:    time.Second * 30,
				CurrentRate:    50.0,
				EstimatedTotal: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Function writes to stdout, just verify it doesn't panic
			assert.NotPanics(t, func() {
				displayMigrationProgress(tt.progress)
			})
		})
	}
}

// TestDisplayMigrationProgressWithoutEstimate tests progress display without estimate.
// TestDisplayMigrationResult tests the displayMigrationResult function.
func TestDisplayMigrationResult(t *testing.T) {
	now := time.Now()

	t.Run("JSON format", func(t *testing.T) {
		migrateOutputFormat = "json"
		defer func() { migrateOutputFormat = "table" }()

		result := &MigrationResult{
			KeysProcessed:    1000,
			KeysFailed:       5,
			BytesTransferred: 10240,
			Duration:         time.Minute,
			StartTime:        now.Add(-time.Minute),
			EndTime:          now,
		}

		err := displayMigrationResult(result)
		assert.NoError(t, err)
	})

	t.Run("Table format without errors", func(t *testing.T) {
		migrateOutputFormat = "table"

		result := &MigrationResult{
			KeysProcessed:    1000,
			KeysFailed:       0,
			BytesTransferred: 10240,
			Duration:         time.Minute,
			StartTime:        now.Add(-time.Minute),
			EndTime:          now,
		}

		err := displayMigrationResult(result)
		assert.NoError(t, err)
	})

	t.Run("Table format with many errors", func(t *testing.T) {
		migrateOutputFormat = "table"

		result := &MigrationResult{
			KeysProcessed:    1000,
			KeysFailed:       15,
			BytesTransferred: 10240,
			Duration:         time.Minute,
			StartTime:        now.Add(-time.Minute),
			EndTime:          now,
			Errors: []string{
				"error 1", "error 2", "error 3",
				"error 4", "error 5", "error 6",
				"error 7", "error 8", "error 9",
				"error 10", "error 11", "error 12",
			},
		}

		err := displayMigrationResult(result)
		// Function returns error when there are failed keys
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed keys")
	})

	t.Run("Table format with few errors", func(t *testing.T) {
		migrateOutputFormat = "table"

		result := &MigrationResult{
			KeysProcessed:    1000,
			KeysFailed:       3,
			BytesTransferred: 10240,
			Duration:         time.Minute,
			StartTime:        now.Add(-time.Minute),
			EndTime:          now,
			Errors:           []string{"error 1", "error 2", "error 3"},
		}

		err := displayMigrationResult(result)
		// Function returns error when there are failed keys
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed keys")
	})

	t.Run("Invalid format", func(t *testing.T) {
		migrateOutputFormat = "invalid"
		defer func() { migrateOutputFormat = "table" }()

		result := &MigrationResult{
			KeysProcessed: 100,
		}

		err := displayMigrationResult(result)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported output format")
	})
}

// TestFormatBytesMigrate tests the formatBytes function with additional cases.
func TestFormatBytesMigrate(t *testing.T) {
	tests := []struct {
		name     string
		bytes    uint64
		expected string
	}{
		{"ZeroBytes", 0, "0 B"},
		{"FewBytes", 512, "512 B"},
		{"JustUnderKB", 1023, "1023 B"},
		{"ExactlyOneKB", 1024, "1.0 KB"},
		{"JustUnderMB", 1024*1024 - 1, "1024.0 KB"},
		{"ExactlyOneMB", 1024 * 1024, "1.0 MB"},
		{"ExactlyOneGB", 1024 * 1024 * 1024, "1.0 GB"},
		{"ExactlyOneTB", 1024 * 1024 * 1024 * 1024, "1.0 TB"},
		{"ExactlyOnePB", 1024 * 1024 * 1024 * 1024 * 1024, "1.0 PB"},
		{"ExactlyOneEB", 1024 * 1024 * 1024 * 1024 * 1024 * 1024, "1.0 EB"},
		{"PartialKB", 2048, "2.0 KB"},
		{"PartialMB", 5 * 1024 * 1024, "5.0 MB"},
		{"MaxUint64", ^uint64(0), "16.0 EB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatBytes(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMigrateCommandStructure tests that migrate commands are properly structured.
func TestMigrateCommandStructure(t *testing.T) {
	cmd := newMigrateCommands()

	assert.NotNil(t, cmd)
	assert.Equal(t, "migrate", cmd.Use)
	assert.Greater(t, len(cmd.Commands()), 0, "Migrate command should have subcommands")

	// Check all expected subcommands exist
	expectedCommands := []string{"start", "status", "cancel"}
	for _, expectedCmd := range expectedCommands {
		found := false
		for _, subCmd := range cmd.Commands() {
			if subCmd.Use == expectedCmd || subCmd.Use[:len(expectedCmd)] == expectedCmd {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected to find subcommand '%s'", expectedCmd)
	}
}

// TestMigrateStartCommandFlags tests the flags for migrate start command.
func TestMigrateStartCommandFlags(t *testing.T) {
	cmd := newMigrateStartCommand()

	// Check required flags exist
	sourceFlag := cmd.Flags().Lookup("source")
	assert.NotNil(t, sourceFlag)

	destFlag := cmd.Flags().Lookup("dest")
	assert.NotNil(t, destFlag)

	batchSizeFlag := cmd.Flags().Lookup("batch-size")
	assert.NotNil(t, batchSizeFlag)
	assert.Equal(t, "1000", batchSizeFlag.DefValue)

	concurrencyFlag := cmd.Flags().Lookup("concurrency")
	assert.NotNil(t, concurrencyFlag)
	assert.Equal(t, "4", concurrencyFlag.DefValue)

	verifyFlag := cmd.Flags().Lookup("verify")
	assert.NotNil(t, verifyFlag)
	assert.Equal(t, "true", verifyFlag.DefValue)

	deleteSourceFlag := cmd.Flags().Lookup("delete-source")
	assert.NotNil(t, deleteSourceFlag)
	assert.Equal(t, "false", deleteSourceFlag.DefValue)

	rateLimitFlag := cmd.Flags().Lookup("rate-limit")
	assert.NotNil(t, rateLimitFlag)
	assert.Equal(t, "0", rateLimitFlag.DefValue)

	checkpointFlag := cmd.Flags().Lookup("checkpoint")
	assert.NotNil(t, checkpointFlag)

	outputFlag := cmd.Flags().Lookup("output")
	assert.NotNil(t, outputFlag)
	assert.Equal(t, "table", outputFlag.DefValue)

	followFlag := cmd.Flags().Lookup("follow")
	assert.NotNil(t, followFlag)
	assert.Equal(t, "false", followFlag.DefValue)
}

// TestMigrateStatusCommandFlags tests the flags for migrate status command.
func TestMigrateStatusCommandFlags(t *testing.T) {
	cmd := newMigrateStatusCommand()

	outputFlag := cmd.Flags().Lookup("output")
	assert.NotNil(t, outputFlag)
	assert.Equal(t, "table", outputFlag.DefValue)
}

// TestMigrateCancelCommandFlags tests the flags for migrate cancel command.
func TestMigrateCancelCommandFlags(t *testing.T) {
	cmd := newMigrateCancelCommand()

	rollbackFlag := cmd.Flags().Lookup("rollback")
	assert.NotNil(t, rollbackFlag)
	assert.Equal(t, "false", rollbackFlag.DefValue)
}

// TestRunMigrateStart tests the migrate start command execution.
func TestRunMigrateStart(t *testing.T) {
	testLog := createTestLoggerForMigrate(t)
	log = testLog

	savedSource := migrateSourceURL
	savedDest := migrateDestURL
	savedVerify := migrateVerify
	savedFollow := migrateFollowProgress
	savedFormat := migrateOutputFormat
	savedMigration := currentMigration
	defer func() {
		migrateSourceURL = savedSource
		migrateDestURL = savedDest
		migrateVerify = savedVerify
		migrateFollowProgress = savedFollow
		migrateOutputFormat = savedFormat
		currentMigration = savedMigration
	}()

	tests := []struct {
		name           string
		sourceURL      string
		destURL        string
		verify         bool
		outputFormat   string
		followProgress bool
		expectError    bool
		errorMsg       string
	}{
		{
			name:         "Invalid source URL",
			sourceURL:    "invalid_url",
			destURL:      "memory://",
			verify:       true,
			outputFormat: "table",
			expectError:  true,
			errorMsg:     "create source storage failed",
		},
		{
			name:         "Invalid dest URL",
			sourceURL:    "memory://",
			destURL:      "invalid_url",
			verify:       true,
			outputFormat: "table",
			expectError:  true,
			errorMsg:     "create destination storage failed",
		},
		{
			name:         "Unsupported source scheme",
			sourceURL:    "redis://localhost",
			destURL:      "memory://",
			verify:       true,
			outputFormat: "table",
			expectError:  true,
			errorMsg:     "unsupported storage scheme",
		},
		{
			name:         "Memory to memory migration without follow",
			sourceURL:    "memory://",
			destURL:      "memory://",
			outputFormat: "table",
		},
		{
			name:         "JSON output format",
			sourceURL:    "memory://",
			destURL:      "memory://",
			verify:       true,
			outputFormat: "json",
		},
		{
			name:           "Follow progress",
			sourceURL:      "memory://",
			destURL:        "memory://",
			outputFormat:   "table",
			followProgress: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newMigrateStartCommand()

			// Set global flags after command creation
			migrateSourceURL = tt.sourceURL
			migrateDestURL = tt.destURL
			migrateBatchSize = 100
			migrateConcurrency = 2
			migrateVerify = tt.verify
			migrateDeleteSource = false
			migrateRateLimit = 0
			migrateCheckpoint = 10 * time.Second
			migrateOutputFormat = tt.outputFormat
			migrateFollowProgress = tt.followProgress

			err := cmd.RunE(cmd, []string{})

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}

			currentMigration = nil
		})
	}
}

// TestRunMigrateStatus tests the migrate status command execution.
func TestRunMigrateStatus(t *testing.T) {
	testLog := createTestLoggerForMigrate(t)
	log = testLog

	tests := []struct {
		name           string
		outputFormat   string
		setupMigration bool
		expectError    bool
		errorMsg       string
	}{
		{
			name:           "No active migration",
			outputFormat:   "table",
			setupMigration: false,
			expectError:    true,
			errorMsg:       "no active migration found",
		},
		{
			name:           "Table output with active migration",
			outputFormat:   "table",
			setupMigration: true,
			expectError:    false,
		},
		{
			name:           "JSON output with active migration",
			outputFormat:   "json",
			setupMigration: true,
			expectError:    false,
		},
		{
			name:           "Invalid output format",
			outputFormat:   "xml",
			setupMigration: true,
			expectError:    true,
			errorMsg:       "unsupported output format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newMigrateStatusCommand()

			// Setup migration if needed
			if tt.setupMigration {
				source, err := createStorageFromURL("memory://")
				require.NoError(t, err)
				defer source.Close()

				dest, err := createStorageFromURL("memory://")
				require.NoError(t, err)
				defer dest.Close()

				config := &MigrationConfig{
					BatchSize:   100,
					Concurrency: 2,
					VerifyData:  false,
				}

				mgr, err := NewMigrationManager(source, dest, config, asInterfaceLogger(testLog))
				require.NoError(t, err)
				currentMigration = mgr
			} else {
				currentMigration = nil
			}

			migrateOutputFormat = tt.outputFormat

			err := cmd.RunE(cmd, []string{})

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}

			// Clean up
			currentMigration = nil
		})
	}
}

// TestRunMigrateCancel tests the migrate cancel command execution.
func TestRunMigrateCancel(t *testing.T) {
	testLog := createTestLoggerForMigrate(t)
	log = testLog

	tests := []struct {
		name           string
		rollback       bool
		setupMigration bool
		expectError    bool
		errorMsg       string
	}{
		{
			name:           "No active migration",
			rollback:       false,
			setupMigration: false,
			expectError:    true,
			errorMsg:       "no active migration found",
		},
		{
			name:           "Cancel without rollback",
			rollback:       false,
			setupMigration: true,
			expectError:    false,
		},
		{
			name:           "Cancel with rollback",
			rollback:       true,
			setupMigration: true,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newMigrateCancelCommand()

			// Setup migration if needed
			if tt.setupMigration {
				source, err := createStorageFromURL("memory://")
				require.NoError(t, err)
				defer source.Close()

				dest, err := createStorageFromURL("memory://")
				require.NoError(t, err)
				defer dest.Close()

				config := &MigrationConfig{
					BatchSize:   100,
					Concurrency: 2,
					VerifyData:  false,
				}

				mgr, err := NewMigrationManager(source, dest, config, asInterfaceLogger(testLog))
				require.NoError(t, err)
				currentMigration = mgr
			} else {
				currentMigration = nil
			}

			// Set rollback flag
			cmd.Flags().Set("rollback", fmt.Sprintf("%v", tt.rollback))

			err := cmd.RunE(cmd, []string{})

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				// After cancel, currentMigration should be nil
				assert.Nil(t, currentMigration)
			}

			// Clean up
			currentMigration = nil
		})
	}
}

// NOTE: Migration start with follow progress is tested in integration tests
// where proper goroutine synchronization can be achieved with real services.

// TestNewMemoryStorageAdapter tests the memory storage adapter creation.
// TestMemoryIteratorNewIterator tests NewIterator method.
func TestMemoryStorageReaderAdapter_Get(t *testing.T) {
	ensureTestLoggerInitialized(t)

	memStorage := memory.New(log)
	adapter := &memoryStorageReaderAdapter{storage: memStorage}
	defer adapter.Close()

	// Put a key-value pair directly into the memory storage
	err := memStorage.Set([]byte("test-key"), []byte("test-value"))
	require.NoError(t, err)

	// Get via adapter
	val, err := adapter.Get([]byte("test-key"))
	assert.NoError(t, err)
	assert.Equal(t, []byte("test-value"), val)
}

func TestMemoryStorageReaderAdapter_Get_KeyNotFound(t *testing.T) {
	ensureTestLoggerInitialized(t)

	memStorage := memory.New(log)
	adapter := &memoryStorageReaderAdapter{storage: memStorage}
	defer adapter.Close()

	val, err := adapter.Get([]byte("nonexistent"))
	assert.Error(t, err)
	assert.Nil(t, val)
}

func TestMemoryStorageReaderAdapter_NewIterator_WithData(t *testing.T) {
	ensureTestLoggerInitialized(t)

	memStorage := memory.New(log)
	adapter := &memoryStorageReaderAdapter{storage: memStorage}
	defer adapter.Close()

	// Insert sorted data
	err := memStorage.Set([]byte("aaa"), []byte("v1"))
	require.NoError(t, err)
	err = memStorage.Set([]byte("bbb"), []byte("v2"))
	require.NoError(t, err)
	err = memStorage.Set([]byte("ccc"), []byte("v3"))
	require.NoError(t, err)

	// Create iterator with bounds
	opts := &storage.IteratorOptions{
		LowerBound: []byte("aaa"),
		UpperBound: []byte("ddd"),
	}
	iter := adapter.NewIterator(opts)
	require.NotNil(t, iter)
	defer iter.Close()

	assert.True(t, iter.SeekToFirst())
	assert.True(t, iter.Valid())
	assert.Equal(t, []byte("aaa"), iter.Key())
}

func TestMemoryStorageReaderAdapter_NewIterator_NilOpts(t *testing.T) {
	ensureTestLoggerInitialized(t)

	memStorage := memory.New(log)
	adapter := &memoryStorageReaderAdapter{storage: memStorage}
	defer adapter.Close()

	// Insert data
	err := memStorage.Set([]byte("key1"), []byte("val1"))
	require.NoError(t, err)

	// Create iterator with nil opts
	iter := adapter.NewIterator(nil)
	require.NotNil(t, iter)
	defer iter.Close()

	assert.True(t, iter.SeekToFirst())
}

func TestMemoryStorageReaderAdapter_Close(t *testing.T) {
	ensureTestLoggerInitialized(t)

	memStorage := memory.New(log)
	adapter := &memoryStorageReaderAdapter{storage: memStorage}

	err := adapter.Close()
	assert.NoError(t, err)
}

// --------------------------------------------------------------------------
// openStorageForWarming success path (covers the non-nil config branch)
// --------------------------------------------------------------------------

func TestOpenStorageForWarming_Success(t *testing.T) {
	ensureTestLoggerInitialized(t)

	savedCfg := cfg
	defer func() { cfg = savedCfg }()

	cfg = config.Default()

	reader, err := openStorageForWarming()
	assert.NoError(t, err)
	require.NotNil(t, reader)

	// StorageReader does not have Close; use type assertion to clean up
	if closer, ok := reader.(io.Closer); ok {
		closer.Close()
	}
}

// --------------------------------------------------------------------------
// initializeCacheWarmerDependencies success path (23.5% coverage)
// --------------------------------------------------------------------------

func TestInitializeCacheWarmerDependencies_Success(t *testing.T) {
	ensureTestLoggerInitialized(t)

	savedCfg := cfg
	defer func() { cfg = savedCfg }()

	cfg = config.Default()

	manager, reader, cleanup, err := initializeCacheWarmerDependencies()
	assert.NoError(t, err)
	require.NotNil(t, manager)
	require.NotNil(t, reader)
	require.NotNil(t, cleanup)

	// Exercise cleanup function
	cleanup()
}

// --------------------------------------------------------------------------
// grpcBackupAdapter tests (all 5 methods at 0% coverage)
// --------------------------------------------------------------------------

func TestGRPCBackupAdapter_CreateBackup(t *testing.T) {
	adapter := &grpcBackupAdapter{client: nil}
	req := &BackupCreateRequest{
		Name:       "test-backup",
		BackupType: "full",
	}

	resp, err := adapter.CreateBackup(context.Background(), req)
	assert.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "full", resp.Type)
}

func TestGRPCBackupAdapter_ListBackups(t *testing.T) {
	adapter := &grpcBackupAdapter{client: nil}
	req := &BackupListRequest{
		StoragePath: "/tmp/backups",
	}

	resp, err := adapter.ListBackups(context.Background(), req)
	assert.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "/tmp/backups", resp.StoragePath)
	assert.Equal(t, 0, resp.TotalCount)
}

func TestGRPCBackupAdapter_RestoreBackup(t *testing.T) {
	adapter := &grpcBackupAdapter{client: nil}
	req := &BackupRestoreRequest{
		BackupID: "backup-123",
	}

	resp, err := adapter.RestoreBackup(context.Background(), req)
	assert.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "backup-123", resp.BackupID)
}

func TestGRPCBackupAdapter_DeleteBackup(t *testing.T) {
	adapter := &grpcBackupAdapter{client: nil}
	req := &BackupDeleteRequest{
		BackupID: "backup-456",
	}

	resp, err := adapter.DeleteBackup(context.Background(), req)
	assert.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Deleted)
	assert.Equal(t, "backup-456", resp.BackupID)
}

func TestGRPCBackupAdapter_VerifyBackup(t *testing.T) {
	adapter := &grpcBackupAdapter{client: nil}
	req := &BackupVerifyRequest{
		BackupID: "backup-789",
	}

	resp, err := adapter.VerifyBackup(context.Background(), req)
	assert.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Valid)
	assert.True(t, resp.ChecksumValid)
	assert.Equal(t, "backup-789", resp.BackupID)
}

// --------------------------------------------------------------------------
// grpcTLSOption tests (57.1% coverage - covering all branches)
// --------------------------------------------------------------------------

func TestGRPCTLSOption(t *testing.T) {
	// Save and restore globals
	savedCert := tlsCert
	savedKey := tlsKey
	savedCA := tlsCA
	defer func() {
		tlsCert = savedCert
		tlsKey = savedKey
		tlsCA = savedCA
	}()

	tests := []struct {
		name string
		cert string
		key  string
		ca   string
	}{
		{name: "Default TLS", cert: "", key: "", ca: ""},
		{name: "With cert and key", cert: "/path/to/cert.pem", key: "/path/to/key.pem", ca: ""},
		{name: "With CA only", cert: "", key: "", ca: "/path/to/ca.pem"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tlsCert = tt.cert
			tlsKey = tt.key
			tlsCA = tt.ca

			opts := grpcTLSOption()
			assert.NotEmpty(t, opts)
		})
	}
}

// --------------------------------------------------------------------------
// MigrationManager with VerifyData enabled
// --------------------------------------------------------------------------

// --------------------------------------------------------------------------
// MigrationManager with DeleteSource enabled
// --------------------------------------------------------------------------

// --------------------------------------------------------------------------
// NodeReplacementManager: AddNode duplicate error
// --------------------------------------------------------------------------

func TestNodeReplacementManager_AddDuplicateNode(t *testing.T) {
	ensureTestLoggerInitialized(t)

	source, err := createStorageFromURL("memory://")
	require.NoError(t, err)
	defer source.Close()

	replacementCfg := DefaultNodeReplacementConfig()
	mgr, err := NewNodeReplacementManager(source, replacementCfg, asInterfaceLogger(log))
	require.NoError(t, err)

	err = mgr.AddNode(context.Background(), "dup-node", "addr1")
	require.NoError(t, err)

	err = mgr.AddNode(context.Background(), "dup-node", "addr2")
	assert.Error(t, err, "adding duplicate node should fail")
}

// --------------------------------------------------------------------------
// NodeReplacementManager: ReplaceNode when old node does not exist
// succeeds silently and registers the new node.
// --------------------------------------------------------------------------

// --------------------------------------------------------------------------
// MigrationManager: constructor accepts nil logger (no validation)
// --------------------------------------------------------------------------

func TestNewMigrationManager_NilLogger(t *testing.T) {
	ensureTestLoggerInitialized(t)

	source, err := createStorageFromURL("memory://")
	require.NoError(t, err)
	defer source.Close()

	dest, err := createStorageFromURL("memory://")
	require.NoError(t, err)
	defer dest.Close()

	// Constructor accepts nil logger without error
	mgr, err := NewMigrationManager(source, dest, &MigrationConfig{BatchSize: 10}, nil)
	assert.NoError(t, err)
	assert.NotNil(t, mgr)
}

// --------------------------------------------------------------------------
// NewNodeReplacementManager: constructor accepts nil logger (no validation)
// --------------------------------------------------------------------------

func TestNewNodeReplacementManager_NilLogger(t *testing.T) {
	ensureTestLoggerInitialized(t)

	source, err := createStorageFromURL("memory://")
	require.NoError(t, err)
	defer source.Close()

	mgr, err := NewNodeReplacementManager(source, DefaultNodeReplacementConfig(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, mgr)
}

func TestRunCacheWarmCommand_NoPrefixes(t *testing.T) {
	ensureTestLoggerInitialized(t)

	savedCfg := cfg
	savedPrefixes := cacheWarmPrefixes
	defer func() {
		cfg = savedCfg
		cacheWarmPrefixes = savedPrefixes
	}()

	cfg = config.Default()

	cmd := newCacheWarmCommand()
	// Set AFTER command creation since StringArrayVar initializes to default
	cacheWarmPrefixes = nil
	err := cmd.RunE(cmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no prefixes specified")
}

func TestRunCacheWarmCommand_InvalidFormat(t *testing.T) {
	ensureTestLoggerInitialized(t)

	savedCfg := cfg
	savedPrefixes := cacheWarmPrefixes
	savedFmt := cacheOutputFmt
	defer func() {
		cfg = savedCfg
		cacheWarmPrefixes = savedPrefixes
		cacheOutputFmt = savedFmt
	}()

	cfg = config.Default()

	cmd := newCacheWarmCommand()
	// Set AFTER command creation
	cacheWarmPrefixes = []string{"test/"}
	cacheOutputFmt = "invalid_format"

	err := cmd.RunE(cmd, nil)
	assert.Error(t, err)
}

func TestRunCacheWarmCommand_SuccessJSON(t *testing.T) {
	ensureTestLoggerInitialized(t)

	savedCfg := cfg
	savedPrefixes := cacheWarmPrefixes
	savedFmt := cacheOutputFmt
	savedBatch := cacheWarmBatchSize
	savedRate := cacheWarmRate
	savedMaxKeys := cacheWarmMaxKeys
	savedTimeout := globalTimeout
	defer func() {
		cfg = savedCfg
		cacheWarmPrefixes = savedPrefixes
		cacheOutputFmt = savedFmt
		cacheWarmBatchSize = savedBatch
		cacheWarmRate = savedRate
		cacheWarmMaxKeys = savedMaxKeys
		globalTimeout = savedTimeout
	}()

	cfg = config.Default()

	cmd := newCacheWarmCommand()
	// Set AFTER command creation
	cacheWarmPrefixes = []string{"warm-prefix/"}
	cacheOutputFmt = "json"
	cacheWarmBatchSize = 100
	cacheWarmRate = 0
	cacheWarmMaxKeys = 0
	globalTimeout = 5 * time.Second

	err := cmd.RunE(cmd, nil)
	assert.NoError(t, err)
}

func TestRunCacheWarmCommand_SuccessTable(t *testing.T) {
	ensureTestLoggerInitialized(t)

	savedCfg := cfg
	savedPrefixes := cacheWarmPrefixes
	savedFmt := cacheOutputFmt
	savedBatch := cacheWarmBatchSize
	savedRate := cacheWarmRate
	savedMaxKeys := cacheWarmMaxKeys
	savedTimeout := globalTimeout
	defer func() {
		cfg = savedCfg
		cacheWarmPrefixes = savedPrefixes
		cacheOutputFmt = savedFmt
		cacheWarmBatchSize = savedBatch
		cacheWarmRate = savedRate
		cacheWarmMaxKeys = savedMaxKeys
		globalTimeout = savedTimeout
	}()

	cfg = config.Default()

	cmd := newCacheWarmCommand()
	// Set AFTER command creation
	cacheWarmPrefixes = []string{"test/"}
	cacheOutputFmt = "table"
	cacheWarmBatchSize = 50
	cacheWarmRate = 0
	cacheWarmMaxKeys = 10
	globalTimeout = 5 * time.Second

	err := cmd.RunE(cmd, nil)
	assert.NoError(t, err)
}

// --------------------------------------------------------------------------
// getNodeManager tests (74.1% -> higher coverage)
// --------------------------------------------------------------------------

func TestGetNodeManager_Config(t *testing.T) {
	ensureTestLoggerInitialized(t)

	savedManager := currentNodeManager
	savedHCI := nodeHealthCheckInterval
	savedHCT := nodeHealthCheckTimeout
	savedET := nodeEvacuationTimeout
	savedGP := nodeGracePeriod
	savedMT := nodeMaxTransfers
	savedVD := nodeVerifyData
	savedAR := nodeAutoReplace
	defer func() {
		currentNodeManager = savedManager
		nodeHealthCheckInterval = savedHCI
		nodeHealthCheckTimeout = savedHCT
		nodeEvacuationTimeout = savedET
		nodeGracePeriod = savedGP
		nodeMaxTransfers = savedMT
		nodeVerifyData = savedVD
		nodeAutoReplace = savedAR
	}()

	tests := []struct {
		name                string
		healthCheckInterval time.Duration
		healthCheckTimeout  time.Duration
		evacuationTimeout   time.Duration
		gracePeriod         time.Duration
		maxTransfers        int
		verifyData          bool
		autoReplace         bool
	}{
		{
			name:       "Zero values use defaults",
			verifyData: true,
		},
		{
			name:                "Custom values",
			healthCheckInterval: 10 * time.Second,
			healthCheckTimeout:  3 * time.Second,
			evacuationTimeout:   15 * time.Minute,
			gracePeriod:         2 * time.Minute,
			maxTransfers:        5,
			autoReplace:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentNodeManager = nil
			nodeHealthCheckInterval = tt.healthCheckInterval
			nodeHealthCheckTimeout = tt.healthCheckTimeout
			nodeEvacuationTimeout = tt.evacuationTimeout
			nodeGracePeriod = tt.gracePeriod
			nodeMaxTransfers = tt.maxTransfers
			nodeVerifyData = tt.verifyData
			nodeAutoReplace = tt.autoReplace

			mgr, err := getNodeManager()
			assert.NoError(t, err)
			require.NotNil(t, mgr)
			assert.Equal(t, mgr, currentNodeManager)
		})
	}
}

func TestGetNodeManager_Cached(t *testing.T) {
	ensureTestLoggerInitialized(t)

	savedManager := currentNodeManager
	defer func() { currentNodeManager = savedManager }()

	currentNodeManager = nil
	nodeHealthCheckInterval = 0
	nodeHealthCheckTimeout = 0
	nodeEvacuationTimeout = 0
	nodeGracePeriod = 0
	nodeMaxTransfers = 0

	mgr1, err := getNodeManager()
	require.NoError(t, err)
	require.NotNil(t, mgr1)

	mgr2, err := getNodeManager()
	require.NoError(t, err)
	assert.Equal(t, mgr1, mgr2, "should return cached node manager")
}

// --------------------------------------------------------------------------
// runNodeAdd / runNodeList full execution tests
// --------------------------------------------------------------------------

func TestRunNodeAdd_OutputFormats(t *testing.T) {
	ensureTestLoggerInitialized(t)

	savedManager := currentNodeManager
	savedOutput := nodeOutputFormat
	defer func() {
		currentNodeManager = savedManager
		nodeOutputFormat = savedOutput
	}()

	tests := []struct {
		format string
		args   []string
	}{
		{format: "table", args: []string{"test-node-add-1", "localhost:9001"}},
		{format: "json", args: []string{"json-node-add", "localhost:9002"}},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			currentNodeManager = nil

			cmd := newNodeAddCommand()
			// Set output format AFTER command creation
			nodeOutputFormat = tt.format
			err := cmd.RunE(cmd, tt.args)
			assert.NoError(t, err)
		})
	}
}

func TestRunNodeReplace_OutputFormats(t *testing.T) {
	ensureTestLoggerInitialized(t)

	savedManager := currentNodeManager
	savedOutput := nodeOutputFormat
	defer func() {
		currentNodeManager = savedManager
		nodeOutputFormat = savedOutput
	}()

	tests := []struct {
		format string
		args   []string
	}{
		{format: "table", args: []string{"old-node-r", "new-node-r", "localhost:9003"}},
		{format: "json", args: []string{"old-j-r", "new-j-r", "localhost:9004"}},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			currentNodeManager = nil

			cmd := newNodeReplaceCommand()
			nodeOutputFormat = tt.format
			err := cmd.RunE(cmd, tt.args)
			assert.NoError(t, err)
		})
	}
}

func TestRunNodeList_EmptyJSON(t *testing.T) {
	ensureTestLoggerInitialized(t)

	savedManager := currentNodeManager
	savedOutput := nodeOutputFormat
	defer func() {
		currentNodeManager = savedManager
		nodeOutputFormat = savedOutput
	}()

	currentNodeManager = nil

	cmd := newNodeListCommand()
	nodeOutputFormat = "json"
	err := cmd.RunE(cmd, nil)
	assert.NoError(t, err)
}

func TestRunNodeList_WithNodesTable(t *testing.T) {
	ensureTestLoggerInitialized(t)

	savedManager := currentNodeManager
	savedOutput := nodeOutputFormat
	defer func() {
		currentNodeManager = savedManager
		nodeOutputFormat = savedOutput
	}()

	currentNodeManager = nil
	nodeOutputFormat = "table"

	// Add a node first
	addCmd := newNodeAddCommand()
	err := addCmd.RunE(addCmd, []string{"list-node-tbl", "localhost:9005"})
	require.NoError(t, err)

	// Now list them
	listCmd := newNodeListCommand()
	nodeOutputFormat = "table"
	err = listCmd.RunE(listCmd, nil)
	assert.NoError(t, err)
}

// --------------------------------------------------------------------------
// runNodeRemove full execution test
// --------------------------------------------------------------------------

func TestRunNodeRemove_TableOutput(t *testing.T) {
	ensureTestLoggerInitialized(t)

	savedManager := currentNodeManager
	savedOutput := nodeOutputFormat
	defer func() {
		currentNodeManager = savedManager
		nodeOutputFormat = savedOutput
	}()

	// Create a manager with a node to remove
	storage, err := createStorageFromURL("memory://")
	require.NoError(t, err)
	defer storage.Close()

	mgr, err := NewNodeReplacementManager(storage, DefaultNodeReplacementConfig(), asInterfaceLogger(log))
	require.NoError(t, err)

	err = mgr.AddNode(context.Background(), "remove-me", "localhost:9006")
	require.NoError(t, err)

	currentNodeManager = mgr

	cmd := newNodeRemoveCommand()
	nodeOutputFormat = "table"
	err = cmd.RunE(cmd, []string{"remove-me"})
	assert.NoError(t, err)
}

// --------------------------------------------------------------------------
// runMigrateStart with follow progress path
// --------------------------------------------------------------------------

// --------------------------------------------------------------------------
// runMigrateStatus JSON format and unsupported format
// --------------------------------------------------------------------------

// --------------------------------------------------------------------------
// displayMigrationResult JSON path and unsupported format
// --------------------------------------------------------------------------

// --------------------------------------------------------------------------
// runMigrateCancel with rollback
// --------------------------------------------------------------------------

// --------------------------------------------------------------------------
// initializeCacheRegistry tests
// --------------------------------------------------------------------------

func TestInitializeCacheRegistry_NilCfg(t *testing.T) {
	ensureTestLoggerInitialized(t)

	savedCfg := cfg
	defer func() { cfg = savedCfg }()

	cfg = nil

	_, err := initializeCacheRegistry()
	assert.Error(t, err)
}

func TestInitializeCacheRegistry_Success(t *testing.T) {
	ensureTestLoggerInitialized(t)

	savedCfg := cfg
	defer func() { cfg = savedCfg }()

	cfg = config.Default()

	registry, err := initializeCacheRegistry()
	assert.NoError(t, err)
	require.NotNil(t, registry)
}

// --------------------------------------------------------------------------
// initializeCacheWarmerDependencies: nil log path
// --------------------------------------------------------------------------

func TestInitializeCacheWarmerDependencies_NilLog(t *testing.T) {
	savedCfg := cfg
	savedLog := log
	defer func() {
		cfg = savedCfg
		log = savedLog
	}()

	cfg = config.Default()
	log = nil

	_, _, _, err := initializeCacheWarmerDependencies()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "logger not initialized")
}

// --------------------------------------------------------------------------
// displayNodeState JSON path
// --------------------------------------------------------------------------

func TestDisplayNodeState_JSON(t *testing.T) {
	ensureTestLoggerInitialized(t)

	savedOutput := nodeOutputFormat
	defer func() { nodeOutputFormat = savedOutput }()

	nodeOutputFormat = "json"

	state := &NodeState{
		NodeID:  "display-test",
		Address: "localhost:9999",
		Status:  NodeStatusHealthy,
	}

	err := displayNodeState(state)
	assert.NoError(t, err)
}

// --------------------------------------------------------------------------
// displayNodeReplacementReport JSON path
// --------------------------------------------------------------------------

func TestDisplayNodeReplacementReport_JSON(t *testing.T) {
	ensureTestLoggerInitialized(t)

	savedOutput := nodeOutputFormat
	defer func() { nodeOutputFormat = savedOutput }()

	nodeOutputFormat = "json"

	report := &NodeReplacementReport{
		OldNodeID: "old-display",
		NewNodeID: "new-display",
		Success:   true,
	}

	err := displayNodeReplacementReport(report)
	assert.NoError(t, err)
}

func ensureTestLoggerInitialized(t *testing.T) {
	t.Helper()
	if log == nil {
		logConfig := logger.LogConfig{
			Level:     "error",
			Format:    "json",
			Output:    "stderr",
			Component: "test",
		}
		l, err := logger.New(logConfig)
		require.NoError(t, err)
		log = l
	}
}

// --------------------------------------------------------------------------
// memoryIterator tests (covers all 0% methods: SeekToFirst, SeekToLast,
// Seek, Next, Prev, Valid, Key, Value, Error, Close)
// --------------------------------------------------------------------------

func TestMemoryIterator_EmptyPairs(t *testing.T) {
	it := &memoryIterator{pairs: nil, pos: -1}

	assert.False(t, it.SeekToFirst(), "SeekToFirst on empty should return false")
	assert.False(t, it.SeekToLast(), "SeekToLast on empty should return false")
	assert.False(t, it.Seek([]byte("anything")), "Seek on empty should return false")
	assert.False(t, it.Valid(), "Valid on empty should return false")
	assert.Nil(t, it.Key(), "Key on invalid position should return nil")
	assert.Nil(t, it.Value(), "Value on invalid position should return nil")
	assert.NoError(t, it.Error(), "Error on empty should be nil")
	assert.NoError(t, it.Close(), "Close should succeed")
}

func TestMemoryIterator_WithError(t *testing.T) {
	testErr := &KeyNotFoundError{}
	it := &memoryIterator{pairs: [][2][]byte{{[]byte("k"), []byte("v")}}, pos: -1, err: testErr}

	assert.False(t, it.SeekToFirst(), "SeekToFirst with error should return false")
	assert.False(t, it.SeekToLast(), "SeekToLast with error should return false")
	assert.False(t, it.Seek([]byte("k")), "Seek with error should return false")
	assert.False(t, it.Next(), "Next with error should return false")
	assert.False(t, it.Prev(), "Prev with error should return false")
	assert.False(t, it.Valid(), "Valid with error should return false")
	assert.ErrorIs(t, it.Error(), testErr)
}

func TestMemoryIterator_Navigation(t *testing.T) {
	pairs := [][2][]byte{
		{[]byte("alpha"), []byte("v1")},
		{[]byte("beta"), []byte("v2")},
		{[]byte("gamma"), []byte("v3")},
	}
	it := &memoryIterator{pairs: pairs, pos: -1}

	// SeekToFirst
	assert.True(t, it.SeekToFirst())
	assert.True(t, it.Valid())
	assert.Equal(t, []byte("alpha"), it.Key())
	assert.Equal(t, []byte("v1"), it.Value())

	// Next
	assert.True(t, it.Next())
	assert.Equal(t, []byte("beta"), it.Key())
	assert.Equal(t, []byte("v2"), it.Value())

	assert.True(t, it.Next())
	assert.Equal(t, []byte("gamma"), it.Key())
	assert.Equal(t, []byte("v3"), it.Value())

	// Next past end
	assert.False(t, it.Next())
	assert.False(t, it.Valid())
	assert.Nil(t, it.Key())
	assert.Nil(t, it.Value())

	// SeekToLast
	assert.True(t, it.SeekToLast())
	assert.True(t, it.Valid())
	assert.Equal(t, []byte("gamma"), it.Key())

	// Prev
	assert.True(t, it.Prev())
	assert.Equal(t, []byte("beta"), it.Key())

	assert.True(t, it.Prev())
	assert.Equal(t, []byte("alpha"), it.Key())

	// Prev past beginning
	assert.False(t, it.Prev())
	assert.False(t, it.Valid())
}

func TestMemoryIterator_Seek(t *testing.T) {
	pairs := [][2][]byte{
		{[]byte("aaa"), []byte("1")},
		{[]byte("bbb"), []byte("2")},
		{[]byte("ccc"), []byte("3")},
		{[]byte("ddd"), []byte("4")},
	}

	t.Run("SeekExact", func(t *testing.T) {
		it := &memoryIterator{pairs: pairs, pos: -1}
		assert.True(t, it.Seek([]byte("bbb")))
		assert.Equal(t, []byte("bbb"), it.Key())
	})

	t.Run("SeekBetween", func(t *testing.T) {
		it := &memoryIterator{pairs: pairs, pos: -1}
		assert.True(t, it.Seek([]byte("bbz")))
		assert.Equal(t, []byte("ccc"), it.Key())
	})

	t.Run("SeekPastEnd", func(t *testing.T) {
		it := &memoryIterator{pairs: pairs, pos: -1}
		assert.False(t, it.Seek([]byte("zzz")))
		assert.False(t, it.Valid())
	})

	t.Run("SeekBeforeAll", func(t *testing.T) {
		it := &memoryIterator{pairs: pairs, pos: -1}
		assert.True(t, it.Seek([]byte("a")))
		assert.Equal(t, []byte("aaa"), it.Key())
	})
}

func TestMemoryIterator_Close(t *testing.T) {
	pairs := [][2][]byte{{[]byte("k"), []byte("v")}}
	it := &memoryIterator{pairs: pairs, pos: 0}

	err := it.Close()
	assert.NoError(t, err)
	assert.Nil(t, it.pairs, "Close should nil out pairs")
}

// --------------------------------------------------------------------------
// memoryStorageReaderAdapter tests
// --------------------------------------------------------------------------

func TestMemoryStorageReaderAdapter_NewIterator(t *testing.T) {
	ensureTestLoggerInitialized(t)

	// Use the createStorageFromURL helper to get a memory storage
	memStorage, err := createStorageFromURL("memory://")
	require.NoError(t, err)
	defer memStorage.Close()

	// Put some data
	err = memStorage.Set([]byte("key1"), []byte("val1"))
	require.NoError(t, err)
	err = memStorage.Set([]byte("key2"), []byte("val2"))
	require.NoError(t, err)

	// Use storage.NewIterator (the storage implements the iterator interface)
	opts := &storage.IteratorOptions{
		LowerBound: []byte("key1"),
		UpperBound: []byte("key3"),
	}
	iter := memStorage.NewIterator(opts)
	require.NotNil(t, iter)
	defer iter.Close()

	ok := iter.SeekToFirst()
	assert.True(t, ok)
	assert.True(t, iter.Valid())
}

// --------------------------------------------------------------------------
// NodeStatus.String tests (covers the default "unknown" branch)
// --------------------------------------------------------------------------

func TestNodeStatus_String(t *testing.T) {
	tests := []struct {
		name     string
		status   NodeStatus
		expected string
	}{
		{"Healthy", NodeStatusHealthy, "healthy"},
		{"Degraded", NodeStatusDegraded, "degraded"},
		{"Failed", NodeStatusFailed, "failed"},
		{"Unknown", NodeStatus(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.status.String())
		})
	}
}

// --------------------------------------------------------------------------
// MigrationManager tests (covers Migrate context cancel, dest.Set failures,
// GetProgress, Rollback)
// --------------------------------------------------------------------------

func TestMigrationManager_MigrateWithContextCancellation(t *testing.T) {
	ensureTestLoggerInitialized(t)

	source, err := createStorageFromURL("memory://")
	require.NoError(t, err)
	defer source.Close()

	dest, err := createStorageFromURL("memory://")
	require.NoError(t, err)
	defer dest.Close()

	// Add test data to source
	for i := 0; i < 10; i++ {
		key := []byte("migrate-key-" + string(rune('a'+i)))
		val := []byte("migrate-val-" + string(rune('a'+i)))
		err := source.Set(key, val)
		require.NoError(t, err)
	}

	migrationConfig := &MigrationConfig{
		BatchSize:          5,
		Concurrency:        1,
		VerifyData:         false,
		DeleteSource:       false,
		RateLimitKBps:      0,
		CheckpointInterval: time.Second,
	}

	mgr, err := NewMigrationManager(source, dest, migrationConfig, asInterfaceLogger(log))
	require.NoError(t, err)

	// Create already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := mgr.Migrate(ctx)
	assert.Error(t, err)
	assert.NotNil(t, result)
	assert.Greater(t, len(result.Errors), 0, "should have error messages")
}

func TestMigrationManager_MigrateSuccess(t *testing.T) {
	ensureTestLoggerInitialized(t)

	tests := []struct {
		name   string
		config *MigrationConfig
		data   map[string]string
	}{
		{
			name:   "Plain copy",
			config: &MigrationConfig{BatchSize: 10, Concurrency: 1},
			data:   map[string]string{"k1": "v1", "k2": "v2"},
		},
		{
			name: "With verify data",
			config: &MigrationConfig{
				BatchSize:          100,
				Concurrency:        1,
				VerifyData:         true,
				CheckpointInterval: time.Second,
			},
			data: map[string]string{"verify-k1": "verify-v1", "verify-k2": "verify-v2"},
		},
		{
			name:   "With delete source",
			config: &MigrationConfig{BatchSize: 100, Concurrency: 1, DeleteSource: true},
			data:   map[string]string{"del-k1": "del-v1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := createStorageFromURL("memory://")
			require.NoError(t, err)
			defer source.Close()

			dest, err := createStorageFromURL("memory://")
			require.NoError(t, err)
			defer dest.Close()

			for k, v := range tt.data {
				require.NoError(t, source.Set([]byte(k), []byte(v)))
			}

			mgr, err := NewMigrationManager(source, dest, tt.config, asInterfaceLogger(log))
			require.NoError(t, err)

			result, err := mgr.Migrate(context.Background())
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, int64(len(tt.data)), result.KeysProcessed)
			assert.Equal(t, int64(0), result.KeysFailed)
			assert.True(t, result.BytesTransferred > 0)

			for k, v := range tt.data {
				got, err := dest.Get([]byte(k))
				assert.NoError(t, err)
				assert.Equal(t, []byte(v), got)
			}
		})
	}
}

func TestMigrationManager_GetProgressAndRollback(t *testing.T) {
	ensureTestLoggerInitialized(t)

	source, err := createStorageFromURL("memory://")
	require.NoError(t, err)
	defer source.Close()

	dest, err := createStorageFromURL("memory://")
	require.NoError(t, err)
	defer dest.Close()

	migrationConfig := &MigrationConfig{BatchSize: 10}
	mgr, err := NewMigrationManager(source, dest, migrationConfig, asInterfaceLogger(log))
	require.NoError(t, err)

	progress := mgr.GetProgress()
	assert.NotNil(t, progress)

	err = mgr.Rollback(context.Background())
	assert.NoError(t, err)
}

// --------------------------------------------------------------------------
// DefaultNodeReplacementConfig tests
// --------------------------------------------------------------------------

func TestDefaultNodeReplacementConfig(t *testing.T) {
	replacementCfg := DefaultNodeReplacementConfig()
	require.NotNil(t, replacementCfg)

	assert.Equal(t, 30*time.Second, replacementCfg.HealthCheckInterval)
	assert.Equal(t, 5*time.Second, replacementCfg.HealthCheckTimeout)
	assert.Equal(t, 30*time.Minute, replacementCfg.EvacuationTimeout)
	assert.True(t, replacementCfg.VerifyData)
	assert.False(t, replacementCfg.AutoReplace)
	assert.Equal(t, 5*time.Minute, replacementCfg.GracePeriod)
	assert.Equal(t, 2, replacementCfg.MaxConcurrentTransfers)
}

// --------------------------------------------------------------------------
// NodeReplacementManager edge cases
// --------------------------------------------------------------------------

func TestNodeReplacementManager_RemoveNonexistentNode(t *testing.T) {
	ensureTestLoggerInitialized(t)

	source, err := createStorageFromURL("memory://")
	require.NoError(t, err)
	defer source.Close()

	replacementCfg := DefaultNodeReplacementConfig()
	mgr, err := NewNodeReplacementManager(source, replacementCfg, asInterfaceLogger(log))
	require.NoError(t, err)

	report, err := mgr.RemoveNode(context.Background(), "nonexistent")
	assert.Error(t, err)
	assert.Nil(t, report)
}

func TestNodeReplacementManager_GetNodeState(t *testing.T) {
	ensureTestLoggerInitialized(t)

	source, err := createStorageFromURL("memory://")
	require.NoError(t, err)
	defer source.Close()

	replacementCfg := DefaultNodeReplacementConfig()
	mgr, err := NewNodeReplacementManager(source, replacementCfg, asInterfaceLogger(log))
	require.NoError(t, err)

	// Not found
	state, found := mgr.GetNodeState("missing")
	assert.False(t, found)
	assert.Nil(t, state)

	// Add then get
	err = mgr.AddNode(context.Background(), "node-1", "localhost:8080")
	require.NoError(t, err)

	state, found = mgr.GetNodeState("node-1")
	assert.True(t, found)
	assert.Equal(t, "node-1", state.NodeID)
	assert.Equal(t, "localhost:8080", state.Address)
	assert.Equal(t, NodeStatusHealthy, state.Status)
}

func TestNodeReplacementManager_GetAllNodes(t *testing.T) {
	ensureTestLoggerInitialized(t)

	source, err := createStorageFromURL("memory://")
	require.NoError(t, err)
	defer source.Close()

	replacementCfg := DefaultNodeReplacementConfig()
	mgr, err := NewNodeReplacementManager(source, replacementCfg, asInterfaceLogger(log))
	require.NoError(t, err)

	// Empty
	nodes := mgr.GetAllNodes()
	assert.Empty(t, nodes)

	// Add two nodes
	err = mgr.AddNode(context.Background(), "n1", "addr1")
	require.NoError(t, err)
	err = mgr.AddNode(context.Background(), "n2", "addr2")
	require.NoError(t, err)

	nodes = mgr.GetAllNodes()
	assert.Len(t, nodes, 2)
}

func TestNodeReplacementManager_ReplaceNode(t *testing.T) {
	ensureTestLoggerInitialized(t)

	tests := []struct {
		name        string
		oldNodeID   string
		registerOld bool
	}{
		{name: "Registered old node", oldNodeID: "old", registerOld: true},
		// ReplaceNode succeeds even when old node is not registered
		{name: "Unregistered old node", oldNodeID: "ghost", registerOld: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := createStorageFromURL("memory://")
			require.NoError(t, err)
			defer source.Close()

			mgr, err := NewNodeReplacementManager(source, DefaultNodeReplacementConfig(), asInterfaceLogger(log))
			require.NoError(t, err)

			if tt.registerOld {
				require.NoError(t, mgr.AddNode(context.Background(), tt.oldNodeID, "addr-old"))
			}

			report, err := mgr.ReplaceNode(context.Background(), tt.oldNodeID, "new", "addr-new")
			assert.NoError(t, err)
			require.NotNil(t, report)
			assert.True(t, report.Success)
			assert.Equal(t, tt.oldNodeID, report.OldNodeID)
			assert.Equal(t, "new", report.NewNodeID)

			// Old node should be gone
			_, found := mgr.GetNodeState(tt.oldNodeID)
			assert.False(t, found)

			// New node should exist
			state, found := mgr.GetNodeState("new")
			assert.True(t, found)
			assert.Equal(t, "addr-new", state.Address)
		})
	}
}

// TestRunMigrateStartPebbleStorage tests migration with pebble storage.
// TestRunMigrateStartFileStorage tests migration with file storage.
// TestRunMigrateStartOutputFormats tests different output formats.
// TestMigrateStartWithInvalidURLs tests invalid URL formats.
// TestCreateStorageFromURLEdgeCases tests edge cases for storage creation.
// TestFormatBytesBoundaries tests formatBytes with boundary values.
// TestDisplayMigrationProgressEdgeCases tests progress display edge cases.
// TestDisplayMigrationResultEdgeCases tests result display edge cases.
