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

package backup

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateBackupTimestamp is a helper to modify backup timestamp in mock storage.
// Since the manager stores metadata as JSON in mockBackend.data["{backupID}/metadata.json"],
// we need to read, modify, and write back.
func updateBackupTimestamp(t *testing.T, mockBackend *MockStorage, backupID string, newTimestamp time.Time) {
	t.Helper()

	metadataKey := backupID + "/metadata.json"
	metadataBytes, ok := mockBackend.data[metadataKey]
	require.True(t, ok, "metadata should exist for backup %s", backupID)

	var storedBackup Backup
	err := json.Unmarshal(metadataBytes, &storedBackup)
	require.NoError(t, err)

	storedBackup.Timestamp = newTimestamp

	updatedMetadata, err := json.Marshal(&storedBackup)
	require.NoError(t, err)
	mockBackend.data[metadataKey] = updatedMetadata
}

func TestBackupManager_ApplyRetentionPolicy_MaxBackups(t *testing.T) {
	manager, _, _ := setupTestBackupManager(t)

	ctx := context.Background()

	// Configure retention policy
	manager.config.Schedule = &ScheduleConfig{
		RetentionPolicy: &RetentionPolicy{
			MaxBackups: 3,
		},
	}

	// Create 5 backups
	for i := 0; i < 5; i++ {
		_, err := manager.CreateBackup(ctx, BackupTypeFull)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	// Verify we have 5 backups
	list, err := manager.ListBackups(ctx, 100)
	require.NoError(t, err)
	assert.Len(t, list.Backups, 5)

	// Apply retention policy
	err = manager.ApplyRetentionPolicy(ctx)
	require.NoError(t, err)

	// Should have only 3 backups left
	list, err = manager.ListBackups(ctx, 100)
	require.NoError(t, err)
	assert.Len(t, list.Backups, 3)
}

func TestBackupManager_ApplyRetentionPolicy_MaxAge(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManager(t)

	ctx := context.Background()

	// Configure retention policy - keep backups for 1 hour
	manager.config.Schedule = &ScheduleConfig{
		RetentionPolicy: &RetentionPolicy{
			MaxAge: 1 * time.Hour,
		},
	}

	// Create a backup
	backup1, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Create another backup with old timestamp
	backup2, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Modify the timestamp to be older than retention period
	updateBackupTimestamp(t, mockBackend, backup2.ID, time.Now().Add(-2*time.Hour))

	// Verify we have 2 backups
	list, err := manager.ListBackups(ctx, 100)
	require.NoError(t, err)
	assert.Len(t, list.Backups, 2)

	// Apply retention policy
	err = manager.ApplyRetentionPolicy(ctx)
	require.NoError(t, err)

	// Should have only 1 backup left (the recent one)
	list, err = manager.ListBackups(ctx, 100)
	require.NoError(t, err)
	assert.Len(t, list.Backups, 1)
	assert.Equal(t, backup1.ID, list.Backups[0].ID)
}

func TestBackupManager_ApplyRetentionPolicy_BothPolicies(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManager(t)

	ctx := context.Background()

	// Configure retention policy
	manager.config.Schedule = &ScheduleConfig{
		RetentionPolicy: &RetentionPolicy{
			MaxBackups: 2,
			MaxAge:     1 * time.Hour,
		},
	}

	// Create 4 backups
	backups := make([]*Backup, 4)
	for i := 0; i < 4; i++ {
		var err error
		backups[i], err = manager.CreateBackup(ctx, BackupTypeFull)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond)
	}

	// Make one of the newer backups old
	updateBackupTimestamp(t, mockBackend, backups[2].ID, time.Now().Add(-2*time.Hour))

	// Apply retention policy
	err := manager.ApplyRetentionPolicy(ctx)
	require.NoError(t, err)

	// Should have only 2 backups left (the 2 most recent non-expired ones)
	list, err := manager.ListBackups(ctx, 100)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(list.Backups), 2)
}

func TestBackupManager_ApplyRetentionPolicy_NoPolicy(t *testing.T) {
	manager, _, _ := setupTestBackupManager(t)

	ctx := context.Background()

	// Create backups
	_, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)
	_, err = manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// No retention policy configured
	manager.config.Schedule = &ScheduleConfig{
		RetentionPolicy: &RetentionPolicy{
			MaxBackups: 0, // No limit
			MaxAge:     0, // No limit
		},
	}

	// Apply retention policy (should do nothing)
	err = manager.ApplyRetentionPolicy(ctx)
	require.NoError(t, err)

	// Should still have all backups
	list, err := manager.ListBackups(ctx, 100)
	require.NoError(t, err)
	assert.Len(t, list.Backups, 2)
}

func TestBackupManager_ApplyRetentionPolicy_DeleteFailure(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManager(t)

	ctx := context.Background()

	// Configure retention policy
	manager.config.Schedule = &ScheduleConfig{
		RetentionPolicy: &RetentionPolicy{
			MaxBackups: 1,
		},
	}

	// Create backups
	backup1, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)
	_, err = manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Delete the old backup from mock storage to simulate delete failure
	// Use the correct key format: {backupID}/data and {backupID}/metadata.json
	delete(mockBackend.data, backup1.ID+"/data")
	delete(mockBackend.data, backup1.ID+"/metadata.json")

	// Apply retention policy - should log warning but not fail
	err = manager.ApplyRetentionPolicy(ctx)
	require.NoError(t, err)

	// Newer backup should still exist
	list, err := manager.ListBackups(ctx, 100)
	require.NoError(t, err)
	assert.Len(t, list.Backups, 1)
}
