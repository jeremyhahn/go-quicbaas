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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage/memory"
)

// TestBackupManager_ListBackups tests listing backups with time.Sleep
func TestBackupManager_ListBackups(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManager(t)

	ctx := context.Background()

	// Create multiple backups
	backup1, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond) // Ensure different timestamps

	backup2, err := manager.CreateBackup(ctx, BackupTypeIncremental)
	require.NoError(t, err)

	// List backups
	list, err := manager.ListBackups(ctx, 10)
	require.NoError(t, err)

	assert.Len(t, list.Backups, 2)
	assert.Equal(t, 2, list.TotalCount)

	// Verify backups are sorted by timestamp (newest first)
	assert.Equal(t, backup2.ID, list.Backups[0].ID)
	assert.Equal(t, backup1.ID, list.Backups[1].ID)

	// Verify backup details
	assert.NotEmpty(t, mockBackend.backups)
}

// TestBackupManager_ApplyRetentionPolicy tests retention policy with time.Sleep
func TestBackupManager_ApplyRetentionPolicy(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManager(t)

	ctx := context.Background()

	// Create multiple backups
	for i := 0; i < 5; i++ {
		_, err := manager.CreateBackup(ctx, BackupTypeFull)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond)
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
	list, err := mockBackend.List(ctx, 0, "")
	require.NoError(t, err)
	assert.Len(t, list.Backups, 3)
}

// TestBackupManager_ApplyRetentionPolicyMaxAge tests max age retention with time.Sleep
func TestBackupManager_ApplyRetentionPolicyMaxAge(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManager(t)

	ctx := context.Background()

	// Create a backup
	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Modify timestamp to be old
	metadata, _ := mockBackend.GetMetadata(ctx, backup.ID)
	metadata.Timestamp = time.Now().Add(-48 * time.Hour)
	mockBackend.backups[backup.ID] = metadata

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
	list, err := mockBackend.List(ctx, 0, "")
	require.NoError(t, err)
	assert.Empty(t, list.Backups)
}

// TestBackupManager_ApplyRetentionPolicyDeleteWarning tests retention policy deletion with time.Sleep
func TestBackupManager_ApplyRetentionPolicyDeleteWarning(t *testing.T) {
	manager, memStorage, _ := setupTestBackupManager(t)
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

	time.Sleep(10 * time.Millisecond)

	backup2, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	backup3, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

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
		memStorage.Delete(key)
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

	manager.Close()
}

// TestNewBackupManager_WithSchedule tests creating manager with schedule (has goroutine)
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

	// Give the scheduler goroutine a moment to start
	time.Sleep(10 * time.Millisecond)

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
		time.Sleep(10 * time.Millisecond)
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
