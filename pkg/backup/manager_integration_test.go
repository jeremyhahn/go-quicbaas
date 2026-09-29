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

package backup

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage/memory"
)

// TestBackupManager_ListBackups tests that every created backup is listed,
// in backup-ID order, with its metadata persisted in the backend.
func TestBackupManager_ListBackups(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManager(t)

	ctx := context.Background()

	// Create multiple backups
	backup1, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	backup2, err := manager.CreateBackup(ctx, BackupTypeIncremental)
	require.NoError(t, err)

	// List backups
	list, err := manager.ListBackups(ctx, 10)
	require.NoError(t, err)

	assert.Len(t, list.Backups, 2)
	assert.Equal(t, 2, list.TotalCount)

	// listBackups orders by backup ID, not by timestamp.
	assert.True(t, sort.SliceIsSorted(list.Backups, func(i, j int) bool {
		return list.Backups[i].ID < list.Backups[j].ID
	}))
	listed := []string{list.Backups[0].ID, list.Backups[1].ID}
	assert.ElementsMatch(t, []string{backup1.ID, backup2.ID}, listed)

	// Verify the metadata landed in the backend.
	for _, id := range listed {
		exists, err := mockBackend.Exists(ctx, id+"/metadata.json")
		require.NoError(t, err)
		assert.True(t, exists, "metadata for %s", id)
	}
}

// TestBackupManager_ApplyRetentionPolicy tests MaxBackups retention.
func TestBackupManager_ApplyRetentionPolicy(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManager(t)

	ctx := context.Background()

	// Create 5 backups, each an hour older than the next.
	now := time.Now()
	for i := 0; i < 5; i++ {
		backup, err := manager.CreateBackup(ctx, BackupTypeFull)
		require.NoError(t, err)
		updateBackupTimestamp(t, mockBackend, backup.ID, now.Add(-time.Duration(5-i)*time.Hour))
	}

	// Set retention policy
	manager.config.Schedule = &ScheduleConfig{
		RetentionPolicy: &RetentionPolicy{
			MaxBackups: 3,
		},
	}

	// Apply retention policy
	err := manager.ApplyRetentionPolicy(ctx)
	require.NoError(t, err)

	// Should only have 3 backups
	list, err := manager.ListBackups(ctx, 100)
	require.NoError(t, err)
	assert.Len(t, list.Backups, 3)
}

// TestBackupManager_ApplyRetentionPolicyMaxAge tests MaxAge retention.
func TestBackupManager_ApplyRetentionPolicyMaxAge(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManager(t)

	ctx := context.Background()

	// Create a backup
	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Modify timestamp to be old
	updateBackupTimestamp(t, mockBackend, backup.ID, time.Now().Add(-48*time.Hour))

	// Set retention policy with max age
	manager.config.Schedule = &ScheduleConfig{
		RetentionPolicy: &RetentionPolicy{
			MaxAge: 24 * time.Hour,
		},
	}

	// Apply retention policy
	err = manager.ApplyRetentionPolicy(ctx)
	require.NoError(t, err)

	// Old backup should be deleted
	list, err := manager.ListBackups(ctx, 100)
	require.NoError(t, err)
	assert.Empty(t, list.Backups)
}

// TestBackupManager_ApplyRetentionPolicyDeleteWarning tests that MaxBackups
// retention deletes the oldest backup.
func TestBackupManager_ApplyRetentionPolicyDeleteWarning(t *testing.T) {
	manager, memStorage, mockBackend := setupTestBackupManager(t)
	ctx := context.Background()

	// Add test data
	require.NoError(t, memStorage.Set([]byte("key1"), []byte("value1")))

	// Set retention policy
	manager.config.Schedule = &ScheduleConfig{
		RetentionPolicy: &RetentionPolicy{
			MaxBackups: 2,
			MaxAge:     24 * time.Hour,
		},
	}

	// Create multiple backups
	backup1, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	backup2, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	backup3, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Order the three explicitly, all inside MaxAge, backup1 oldest.
	now := time.Now()
	updateBackupTimestamp(t, mockBackend, backup1.ID, now.Add(-3*time.Minute))
	updateBackupTimestamp(t, mockBackend, backup2.ID, now.Add(-2*time.Minute))
	updateBackupTimestamp(t, mockBackend, backup3.ID, now.Add(-1*time.Minute))

	// Apply retention policy - should delete oldest backup
	err = manager.ApplyRetentionPolicy(ctx)
	require.NoError(t, err)

	// Verify backup1 was deleted
	list, err := manager.ListBackups(ctx, 0)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(list.Backups), 2)

	// Check that the remaining backups are the newer ones
	ids := make(map[string]bool)
	for _, b := range list.Backups {
		ids[b.ID] = true
	}
	// backup1 (oldest) should be deleted
	assert.False(t, ids[backup1.ID])
	assert.True(t, ids[backup2.ID])
	assert.True(t, ids[backup3.ID])
}

// TestBackupManager_LargeBackup tests backup with 1000 keys (large dataset)
func TestBackupManager_LargeBackup(t *testing.T) {
	memStorage := &mockStorage{
		MemoryStorage: memory.New(nil),
	}

	// Add many keys
	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		value := []byte(fmt.Sprintf("value-%d", i))
		require.NoError(t, memStorage.Set(key, value))
	}

	mockRaft := &mockRaftNode{shardCount: 1}
	mockBackend := NewMockStorage()

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	config := &Config{
		StorageBackend:     "local",
		DefaultCompression: CompressionZstd,
		DefaultEncryption:  EncryptionNone,
		WorkDir:            t.TempDir(),
		Timeout:            5 * time.Minute,
		StorageConfig: map[string]string{
			"path": t.TempDir(),
		},
	}

	manager := &BackupManager{
		config:  config,
		storage: memStorage,
		raft:    mockRaft,
		backend: mockBackend,
		logger:  log.WithField("component", "backup-manager"),
		metrics: initBackupMetrics(),
	}

	ctx := context.Background()
	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	assert.Greater(t, backup.Size, int64(0))
	assert.NotEmpty(t, backup.Checksum)

	// Clear storage
	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		require.NoError(t, memStorage.Delete(key))
	}

	// Restore
	err = manager.RestoreBackup(ctx, backup.ID, &RestoreOptions{
		VerifyChecksum: true,
	})
	require.NoError(t, err)

	// Verify all keys restored
	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		value := []byte(fmt.Sprintf("value-%d", i))

		actual, err := memStorage.Get(key)
		require.NoError(t, err)
		assert.Equal(t, value, actual)
	}

	require.NoError(t, manager.Close())
}

// TestNewBackupManager_WithSchedule tests that a manager with a schedule
// starts its scheduler goroutine and closes it cleanly.
func TestNewBackupManager_WithSchedule(t *testing.T) {
	memStorage := &mockStorage{
		MemoryStorage: memory.New(nil),
	}

	mockRaft := &mockRaftNode{shardCount: 1}

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	config := &Config{
		StorageBackend:     "local",
		DefaultCompression: CompressionNone,
		DefaultEncryption:  EncryptionNone,
		WorkDir:            t.TempDir(),
		Timeout:            5 * time.Minute,
		StorageConfig: map[string]string{
			"path": t.TempDir(),
		},
		Schedule: &ScheduleConfig{
			Enabled:        true,
			CronExpression: "0 2 * * *",
			BackupType:     BackupTypeFull,
			RetentionPolicy: &RetentionPolicy{
				MaxBackups: 10,
			},
		},
	}

	manager, err := NewBackupManager(config, memStorage, mockRaft, log)
	require.NoError(t, err)
	assert.NotNil(t, manager)

	err = manager.Close()
	require.NoError(t, err)
}

// TestBackupManager_ListBackupsLimit tests listing with pagination
func TestBackupManager_ListBackupsLimit(t *testing.T) {
	manager, _, _ := setupTestBackupManager(t)

	ctx := context.Background()

	// Create multiple backups
	for i := 0; i < 5; i++ {
		_, err := manager.CreateBackup(ctx, BackupTypeFull)
		require.NoError(t, err)
	}

	// List with limit
	list, err := manager.ListBackups(ctx, 3)
	require.NoError(t, err)
	assert.Len(t, list.Backups, 3)
}

// TestLocalStorage_ListWithLimit tests listing with limit and file I/O
func TestLocalStorage_ListWithLimit(t *testing.T) {
	storage, err := NewLocalStorage(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()

	// Create multiple backups
	for i := 0; i < 5; i++ {
		backup := &Backup{
			ID:          "backup-" + string(rune('1'+i)),
			Type:        BackupTypeFull,
			Timestamp:   time.Now().Add(time.Duration(i) * time.Second),
			Compression: CompressionNone,
			Encryption:  EncryptionNone,
			Metadata:    &BackupMetadata{},
		}
		err = storage.Upload(ctx, backup, bytes.NewReader([]byte("data")))
		require.NoError(t, err)
	}

	// List with limit
	list, err := storage.List(ctx, 2, "")
	require.NoError(t, err)

	assert.Len(t, list.Backups, 2)
}
