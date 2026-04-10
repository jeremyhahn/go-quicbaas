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
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackupManager_VerifyBackup(t *testing.T) {
	manager, _, _ := setupTestBackupManager(t)

	ctx := context.Background()

	// Create a backup
	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Verify the backup
	result, err := manager.VerifyBackup(ctx, backup.ID)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Valid)
	assert.True(t, result.ChecksumValid)
	assert.Empty(t, result.Errors)
	assert.Contains(t, result.Message, "successful")
}

func TestBackupManager_VerifyBackup_NotFound(t *testing.T) {
	manager, _, _ := setupTestBackupManager(t)

	ctx := context.Background()

	// Verify non-existent backup
	result, err := manager.VerifyBackup(ctx, "nonexistent-backup")
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Valid)
	assert.False(t, result.ChecksumValid)
	assert.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Message, "metadata")
}

func TestBackupManager_VerifyBackup_CorruptedChecksum(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManager(t)

	ctx := context.Background()

	// Create a backup
	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Corrupt the checksum in metadata - read, modify, write back
	metadataKey := backup.ID + "/metadata.json"
	metadataBytes, ok := mockBackend.data[metadataKey]
	require.True(t, ok, "metadata should exist")

	var storedBackup Backup
	err = json.Unmarshal(metadataBytes, &storedBackup)
	require.NoError(t, err)

	storedBackup.Checksum = "invalid-checksum"

	updatedMetadata, err := json.Marshal(&storedBackup)
	require.NoError(t, err)
	mockBackend.data[metadataKey] = updatedMetadata

	// Verify the backup
	result, err := manager.VerifyBackup(ctx, backup.ID)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Valid)
	assert.False(t, result.ChecksumValid)
	assert.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Message, "checksum")
}

func TestBackupManager_VerifyBackup_WithEncryption(t *testing.T) {
	manager, _, _ := setupTestBackupManager(t)

	// Set encryption key
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	manager.config.EncryptionKey = key
	manager.config.DefaultEncryption = EncryptionAES256GCM

	ctx := context.Background()

	// Create encrypted backup
	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Verify the backup
	result, err := manager.VerifyBackup(ctx, backup.ID)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Valid)
	assert.True(t, result.ChecksumValid)
	assert.Empty(t, result.Errors)
}

func TestBackupManager_VerifyBackup_WithCompression(t *testing.T) {
	manager, _, _ := setupTestBackupManager(t)
	manager.config.DefaultCompression = CompressionZstd

	ctx := context.Background()

	// Create compressed backup
	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Verify the backup
	result, err := manager.VerifyBackup(ctx, backup.ID)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Valid)
	assert.True(t, result.ChecksumValid)
	assert.Empty(t, result.Errors)
}

func TestBackupManager_VerifyBackup_InvalidCompression(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManager(t)

	ctx := context.Background()

	// Create a backup
	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Corrupt the compression type - read, modify, write back
	metadataKey := backup.ID + "/metadata.json"
	metadataBytes, ok := mockBackend.data[metadataKey]
	require.True(t, ok, "metadata should exist")

	var storedBackup Backup
	err = json.Unmarshal(metadataBytes, &storedBackup)
	require.NoError(t, err)

	storedBackup.Compression = CompressionType("invalid")

	updatedMetadata, err := json.Marshal(&storedBackup)
	require.NoError(t, err)
	mockBackend.data[metadataKey] = updatedMetadata

	// Verify the backup
	result, err := manager.VerifyBackup(ctx, backup.ID)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Valid)
	assert.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Message, "compression")
}

func TestBackupManager_VerifyBackup_InvalidEncryption(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManager(t)

	ctx := context.Background()

	// Create a backup
	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Set invalid encryption type - read, modify, write back
	metadataKey := backup.ID + "/metadata.json"
	metadataBytes, ok := mockBackend.data[metadataKey]
	require.True(t, ok, "metadata should exist")

	var storedBackup Backup
	err = json.Unmarshal(metadataBytes, &storedBackup)
	require.NoError(t, err)

	storedBackup.Encryption = EncryptionType("invalid")

	updatedMetadata, err := json.Marshal(&storedBackup)
	require.NoError(t, err)
	mockBackend.data[metadataKey] = updatedMetadata

	// Verify the backup
	result, err := manager.VerifyBackup(ctx, backup.ID)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Valid)
	assert.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Message, "encryption")
}

func TestBackupManager_VerifyBackup_CorruptedData(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManager(t)

	// Set encryption for better coverage
	key := make([]byte, 32)
	manager.config.EncryptionKey = key
	manager.config.DefaultEncryption = EncryptionAES256GCM

	ctx := context.Background()

	// Create a backup
	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Corrupt the data using correct key format: {backupID}/data
	mockBackend.data[backup.ID+"/data"] = []byte("corrupted data")

	// Verify the backup (should fail on decryption/decompression)
	result, err := manager.VerifyBackup(ctx, backup.ID)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Valid)
	// Should fail on either checksum or decryption
	assert.True(t, !result.ChecksumValid || len(result.Errors) > 0)
}

func TestBackupManager_VerifyBackup_CorruptedShardChecksum(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManager(t)

	ctx := context.Background()

	// Test with unencrypted backup so we can modify shard checksums
	manager.config.DefaultEncryption = EncryptionNone
	manager.config.DefaultCompression = CompressionNone

	// Create new backup without encryption
	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// We need to modify the actual data that will be decompressed/decrypted
	// So we create a corrupted backup struct and re-encode it
	corruptedBackup := *backup
	for _, shard := range corruptedBackup.Shards {
		shard.Checksum = "invalid-checksum"
	}

	// Re-encode the corrupted backup
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	err = encoder.Encode(&corruptedBackup)
	require.NoError(t, err)

	// Replace the data in mock backend using correct key format: {backupID}/data
	mockBackend.data[backup.ID+"/data"] = buf.Bytes()

	// Update checksum in metadata to match corrupted data
	metadataKey := backup.ID + "/metadata.json"
	metadataBytes, ok := mockBackend.data[metadataKey]
	require.True(t, ok, "metadata should exist")

	var storedBackup Backup
	err = json.Unmarshal(metadataBytes, &storedBackup)
	require.NoError(t, err)

	storedBackup.Checksum = ComputeChecksum(buf.Bytes())

	updatedMetadata, err := json.Marshal(&storedBackup)
	require.NoError(t, err)
	mockBackend.data[metadataKey] = updatedMetadata

	// Verify should detect the corrupted shard
	result, err := manager.VerifyBackup(ctx, backup.ID)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Valid)
	assert.False(t, result.ChecksumValid)
	assert.NotEmpty(t, result.Errors)
}

func TestBackupManager_VerifyBackup_Closed(t *testing.T) {
	manager, _, _ := setupTestBackupManager(t)

	ctx := context.Background()

	// Close the manager
	err := manager.Close()
	require.NoError(t, err)

	// Verify should fail
	result, err := manager.VerifyBackup(ctx, "backup-id")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "closed")
}
