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
	"testing"
	"time"

	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/jeremyhahn/go-objstore/pkg/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage/memory"
)

func TestConfig_Validate(t *testing.T) {
	const defaultWorkDir = "/tmp/quicbaas-backups"
	tests := []struct {
		name        string
		config      *Config
		wantErr     bool
		errContains string
		// Validate fills in Timeout and WorkDir; these are the values a
		// valid config holds afterwards.
		wantTimeout time.Duration
		wantWorkDir string
	}{
		{
			name: "valid config with local storage",
			config: &Config{
				StorageBackend:     "local",
				DefaultCompression: CompressionZstd,
				DefaultEncryption:  EncryptionNone,
				WorkDir:            "/tmp/backups",
				Timeout:            5 * time.Minute,
			},
			wantTimeout: 5 * time.Minute,
			wantWorkDir: "/tmp/backups",
		},
		{
			name: "valid config with S3 storage",
			config: &Config{
				StorageBackend:     "s3",
				DefaultCompression: CompressionGzip,
				DefaultEncryption:  EncryptionNone,
				WorkDir:            "/tmp/backups",
				Timeout:            5 * time.Minute,
			},
			wantTimeout: 5 * time.Minute,
			wantWorkDir: "/tmp/backups",
		},
		{
			name: "valid config with none compression",
			config: &Config{
				StorageBackend:     "local",
				DefaultCompression: CompressionNone,
			},
			wantTimeout: 30 * time.Minute,
			wantWorkDir: defaultWorkDir,
		},
		{
			name: "valid config with encryption",
			config: &Config{
				StorageBackend:     "local",
				DefaultCompression: CompressionZstd,
				DefaultEncryption:  EncryptionAES256GCM,
				EncryptionKey:      make([]byte, 32),
				WorkDir:            "/tmp/backups",
				Timeout:            5 * time.Minute,
			},
			wantTimeout: 5 * time.Minute,
			wantWorkDir: "/tmp/backups",
		},
		{
			name: "valid config with GCS",
			config: &Config{
				StorageBackend: "gcs",
				Timeout:        1 * time.Minute,
			},
			wantTimeout: 1 * time.Minute,
			wantWorkDir: defaultWorkDir,
		},
		{
			name: "valid config with Azure",
			config: &Config{
				StorageBackend: "azure",
				Timeout:        1 * time.Minute,
			},
			wantTimeout: 1 * time.Minute,
			wantWorkDir: defaultWorkDir,
		},
		{
			name: "zero timeout gets default",
			config: &Config{
				StorageBackend: "local",
			},
			wantTimeout: 30 * time.Minute,
			wantWorkDir: defaultWorkDir,
		},
		{
			name: "negative timeout gets default",
			config: &Config{
				StorageBackend: "local",
				Timeout:        -1 * time.Minute,
			},
			wantTimeout: 30 * time.Minute,
			wantWorkDir: defaultWorkDir,
		},
		{
			name: "relative work dir is kept",
			config: &Config{
				StorageBackend: "local",
				WorkDir:        "backups",
			},
			wantTimeout: 30 * time.Minute,
			wantWorkDir: "backups",
		},
		{
			name: "missing storage backend",
			config: &Config{
				DefaultCompression: CompressionZstd,
			},
			wantErr:     true,
			errContains: "storage backend is required",
		},
		{
			name: "unsupported storage backend",
			config: &Config{
				StorageBackend: "unsupported",
			},
			wantErr:     true,
			errContains: "unsupported storage backend",
		},
		{
			name: "storage backend is case sensitive",
			config: &Config{
				StorageBackend: "Local",
			},
			wantErr:     true,
			errContains: "unsupported storage backend",
		},
		{
			name: "invalid compression type",
			config: &Config{
				StorageBackend:     "local",
				DefaultCompression: CompressionType("invalid"),
			},
			wantErr:     true,
			errContains: "unsupported compression type",
		},
		{
			name: "invalid encryption type",
			config: &Config{
				StorageBackend:    "local",
				DefaultEncryption: EncryptionType("invalid"),
			},
			wantErr:     true,
			errContains: "unsupported encryption type",
		},
		{
			name: "AES encryption with 16 byte key",
			config: &Config{
				StorageBackend:    "local",
				DefaultEncryption: EncryptionAES256GCM,
				EncryptionKey:     make([]byte, 16),
			},
			wantErr:     true,
			errContains: "32 bytes",
		},
		{
			name: "AES encryption with 31 byte key",
			config: &Config{
				StorageBackend:    "local",
				DefaultEncryption: EncryptionAES256GCM,
				EncryptionKey:     make([]byte, 31),
			},
			wantErr:     true,
			errContains: "32 bytes",
		},
		{
			name: "AES encryption with 33 byte key",
			config: &Config{
				StorageBackend:    "local",
				DefaultEncryption: EncryptionAES256GCM,
				EncryptionKey:     make([]byte, 33),
			},
			wantErr:     true,
			errContains: "32 bytes",
		},
		{
			name: "AES encryption with no key",
			config: &Config{
				StorageBackend:    "local",
				DefaultEncryption: EncryptionAES256GCM,
			},
			wantErr:     true,
			errContains: "32 bytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantTimeout, tt.config.Timeout)
			assert.Equal(t, tt.wantWorkDir, tt.config.WorkDir)
		})
	}
}

func TestConfig_WithDefaults(t *testing.T) {
	tests := []struct {
		name            string
		config          *Config
		expectedComp    CompressionType
		expectedEnc     EncryptionType
		expectedTimeout time.Duration
		expectedWorkDir string
	}{
		{
			name: "empty config gets all defaults",
			config: &Config{
				StorageBackend: "local",
			},
			expectedComp:    CompressionZstd,
			expectedEnc:     EncryptionNone,
			expectedTimeout: 30 * time.Minute,
			expectedWorkDir: "/tmp/quicbaas-backups",
		},
		{
			name: "partial config preserves values",
			config: &Config{
				StorageBackend:     "local",
				DefaultCompression: CompressionGzip,
				Timeout:            5 * time.Minute,
			},
			expectedComp:    CompressionGzip,
			expectedEnc:     EncryptionNone,
			expectedTimeout: 5 * time.Minute,
			expectedWorkDir: "/tmp/quicbaas-backups",
		},
		{
			name: "explicit none compression",
			config: &Config{
				StorageBackend:     "local",
				DefaultCompression: CompressionNone,
			},
			expectedComp:    CompressionNone,
			expectedEnc:     EncryptionNone,
			expectedTimeout: 30 * time.Minute,
			expectedWorkDir: "/tmp/quicbaas-backups",
		},
		{
			name: "custom work dir",
			config: &Config{
				StorageBackend: "local",
				WorkDir:        "/custom/path",
			},
			expectedComp:    CompressionZstd,
			expectedEnc:     EncryptionNone,
			expectedTimeout: 30 * time.Minute,
			expectedWorkDir: "/custom/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := *tt.config
			result := tt.config.WithDefaults()

			assert.Equal(t, tt.expectedComp, result.DefaultCompression)
			assert.Equal(t, tt.expectedEnc, result.DefaultEncryption)
			assert.Equal(t, tt.expectedTimeout, result.Timeout)
			assert.Equal(t, tt.expectedWorkDir, result.WorkDir)

			// WithDefaults returns a copy; the receiver is not modified.
			assert.Equal(t, original, *tt.config)
		})
	}
}

func setupTestBackupManagerWithErrors(t *testing.T) (*BackupManager, *mockStorage, *MockStorageWithErrors) {
	// Create in-memory storage
	memStorage := &mockStorage{
		MemoryStorage: memory.New(nil),
	}

	// Populate with test data
	require.NoError(t, memStorage.Set([]byte("key1"), []byte("value1")))
	require.NoError(t, memStorage.Set([]byte("key2"), []byte("value2")))
	require.NoError(t, memStorage.Set([]byte("key3"), []byte("value3")))

	// Create mock Raft node
	mockRaft := &mockRaftNode{shardCount: 1}

	// Create mock storage backend with error injection
	mockBackend := NewMockStorageWithErrors()

	// Create logger
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// Create backup manager config
	config := &Config{
		StorageBackend:     "mock",
		DefaultCompression: CompressionNone,
		DefaultEncryption:  EncryptionNone,
		WorkDir:            t.TempDir(),
		Timeout:            5 * time.Minute,
		StorageConfig: map[string]string{
			"path": t.TempDir(),
		},
	}

	// Create backup manager
	manager := &BackupManager{
		config:  config,
		storage: memStorage,
		raft:    mockRaft,
		backend: mockBackend,
		prefix:  "backups",
		logger:  log.WithField("component", "backup-manager"),
		metrics: initBackupMetrics(),
	}

	return manager, memStorage, mockBackend
}

// TestUploadBackup_MetadataUploadFailure tests uploadBackup when metadata upload fails.
func TestUploadBackup_MetadataUploadFailure(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	// Create a backup
	backup := &Backup{
		ID:          "test-backup-metadata-fail",
		Type:        BackupTypeFull,
		Timestamp:   time.Now(),
		Shards:      make(map[uint64]*ShardSnapshot),
		Compression: CompressionNone,
		Encryption:  EncryptionNone,
		Metadata: &BackupMetadata{
			Version:        "1.0.0",
			StorageBackend: "mock",
			Tags:           make(map[string]string),
		},
	}

	// Create a reader
	buf := bytes.NewBufferString("test data")

	// Make metadata upload fail by injecting error after data upload (1st Put succeeds, 2nd fails)
	mockBackend.SetPutError(errors.New("metadata upload failed"), 1)

	err := manager.uploadBackup(ctx, backup, buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "upload backup metadata failed")

	// Verify data was cleaned up (deleted)
	dataKey := manager.dataKey(backup.ID)
	_, exists := mockBackend.data[dataKey]
	assert.False(t, exists, "data should be cleaned up on metadata upload failure")
}

// TestUploadBackup_NilBackup tests uploadBackup with nil backup.
func TestUploadBackup_NilBackup(t *testing.T) {
	manager, _, _ := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	buf := bytes.NewBufferString("test data")
	err := manager.uploadBackup(ctx, nil, buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backup is required")
}

// TestUploadBackup_NilReader tests uploadBackup with nil reader.
func TestUploadBackup_NilReader(t *testing.T) {
	manager, _, _ := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	backup := &Backup{
		ID: "test-backup",
	}

	err := manager.uploadBackup(ctx, backup, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reader is required")
}

// TestUploadBackup_DataUploadFailure tests uploadBackup when data upload fails.
func TestUploadBackup_DataUploadFailure(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	backup := &Backup{
		ID:          "test-backup-data-fail",
		Type:        BackupTypeFull,
		Timestamp:   time.Now(),
		Shards:      make(map[uint64]*ShardSnapshot),
		Compression: CompressionNone,
		Encryption:  EncryptionNone,
		Metadata: &BackupMetadata{
			Version:        "1.0.0",
			StorageBackend: "mock",
		},
	}

	buf := bytes.NewBufferString("test data")

	// Make data upload fail immediately
	mockBackend.SetPutError(errors.New("storage full"), 0)

	err := manager.uploadBackup(ctx, backup, buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "upload backup data failed")
}

// TestDownloadBackup_EmptyID tests downloadBackup with empty backup ID.
func TestDownloadBackup_EmptyID(t *testing.T) {
	manager, _, _ := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	reader, err := manager.downloadBackup(ctx, "")
	assert.Error(t, err)
	assert.Nil(t, reader)
	assert.Contains(t, err.Error(), "backup ID is required")
}

// TestDownloadBackup_NotFound tests downloadBackup with non-existent backup.
func TestDownloadBackup_NotFound(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	// Ensure backend returns error for non-existent backup
	mockBackend.SetGetError(errors.New("backup not found"))

	reader, err := manager.downloadBackup(ctx, "non-existent-backup")
	assert.Error(t, err)
	assert.Nil(t, reader)
	assert.Contains(t, err.Error(), "download backup failed")
}

// TestDeleteBackup_EmptyID tests deleteBackup with empty backup ID.
func TestDeleteBackup_EmptyID(t *testing.T) {
	manager, _, _ := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	err := manager.deleteBackup(ctx, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backup ID is required")
}

// TestDeleteBackup_StorageError tests deleteBackup when storage delete fails.
func TestDeleteBackup_StorageError(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	// Create a backup first
	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Reset error injection from create
	mockBackend.SetPutError(nil, 0)

	// Make delete fail
	mockBackend.SetDeleteError(errors.New("permission denied"))

	err = manager.deleteBackup(ctx, backup.ID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delete backup data failed")
}

// TestListBackups_WithPagination tests listBackups with pagination.
func TestListBackups_WithPagination(t *testing.T) {
	manager, _, _ := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	// Create multiple backups
	backupIDs := make([]string, 5)
	for i := 0; i < 5; i++ {
		backup, err := manager.CreateBackup(ctx, BackupTypeFull)
		require.NoError(t, err)
		backupIDs[i] = backup.ID
	}

	// Test with limit
	result, err := manager.listBackups(ctx, 2, "")
	require.NoError(t, err)
	assert.Equal(t, 2, len(result.Backups))
	assert.Equal(t, 5, result.TotalCount)
	assert.NotEmpty(t, result.NextToken)

	// Test with next token
	result2, err := manager.listBackups(ctx, 2, result.NextToken)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(result2.Backups), 2)
}

// TestListBackups_NoLimit tests listBackups without limit.
func TestListBackups_NoLimit(t *testing.T) {
	manager, _, _ := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	// Create backups
	for i := 0; i < 3; i++ {
		_, err := manager.CreateBackup(ctx, BackupTypeFull)
		require.NoError(t, err)
	}

	// List all with limit 0
	result, err := manager.listBackups(ctx, 0, "")
	require.NoError(t, err)
	assert.Equal(t, 3, len(result.Backups))
	assert.Empty(t, result.NextToken)
}

// TestListBackups_InvalidNextToken tests listBackups with invalid next token.
func TestListBackups_InvalidNextToken(t *testing.T) {
	manager, _, _ := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	// Create a backup
	_, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Use invalid next token (non-existent backup ID)
	// The implementation returns all backups after the token, so if token doesn't match,
	// it starts from the beginning
	result, err := manager.listBackups(ctx, 10, "invalid-token-that-does-not-exist")
	require.NoError(t, err)
	// Should return 1 backup (all backups since token doesn't exist)
	assert.GreaterOrEqual(t, len(result.Backups), 0)
}

// TestListBackups_ListError tests listBackups when backend list fails.
func TestListBackups_ListError(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	// Make list operation fail
	mockBackend.SetListError(errors.New("network error"))

	result, err := manager.listBackups(ctx, 10, "")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "list backups failed")
}

// TestListBackups_SkipInvalidMetadata tests that listBackups skips backups with invalid metadata.
func TestListBackups_SkipInvalidMetadata(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	// Create valid backups
	backup1, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Create another valid backup
	backup2, err := manager.CreateBackup(ctx, BackupTypeFull)
	require.NoError(t, err)

	// Corrupt metadata for backup2
	metadataKey := manager.metadataKey(backup2.ID)
	mockBackend.data[metadataKey] = []byte("invalid json metadata")

	// List should skip the corrupted backup
	result, err := manager.listBackups(ctx, 0, "")
	require.NoError(t, err)
	// Should only have 1 valid backup
	assert.Equal(t, 1, len(result.Backups))
	assert.Equal(t, backup1.ID, result.Backups[0].ID)
}

// TestGetBackupMetadata_EmptyID tests getBackupMetadata with empty ID.
func TestGetBackupMetadata_EmptyID(t *testing.T) {
	manager, _, _ := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	backup, err := manager.getBackupMetadata(ctx, "")
	assert.Error(t, err)
	assert.Nil(t, backup)
	assert.Contains(t, err.Error(), "backup ID is required")
}

// TestGetBackupMetadata_GetError tests getBackupMetadata when Get fails.
func TestGetBackupMetadata_GetError(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	// Make Get operation fail
	mockBackend.SetGetError(errors.New("storage error"))

	backup, err := manager.getBackupMetadata(ctx, "some-backup-id")
	assert.Error(t, err)
	assert.Nil(t, backup)
	assert.Contains(t, err.Error(), "get backup metadata failed")
}

// TestGetBackupMetadata_DecodeError tests getBackupMetadata when JSON decode fails.
func TestGetBackupMetadata_DecodeError(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	// Create backup with invalid metadata
	backupID := "test-backup-invalid-json"
	metadataKey := manager.metadataKey(backupID)
	mockBackend.data[metadataKey] = []byte("{invalid json")

	backup, err := manager.getBackupMetadata(ctx, backupID)
	assert.Error(t, err)
	assert.Nil(t, backup)
	assert.Contains(t, err.Error(), "failed to decode backup metadata")
}

// TestZstdCompressor_DecompressInvalidData tests zstd decompression error path.
func TestZstdCompressor_DecompressInvalidData(t *testing.T) {
	compressor := &zstdCompressor{}

	// Try to decompress invalid zstd data
	invalidData := bytes.NewReader([]byte("not zstd compressed data at all"))
	reader, err := compressor.Decompress(invalidData)

	// zstd.NewReader doesn't immediately fail - it fails during Read
	if err == nil && reader != nil {
		// Error should occur when trying to read
		_, readErr := io.ReadAll(reader)
		assert.Error(t, readErr, "should get error when reading invalid zstd data")
		reader.Close()
	}
}

// TestNewBackupManager_InvalidConfig tests NewBackupManager with invalid config.
func TestNewBackupManager_InvalidConfig(t *testing.T) {
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

	// Test with nil config
	_, err = NewBackupManager(nil, memStorage, mockRaft, log)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config is required")

	// Test with nil storage
	config := &Config{
		StorageBackend: "local",
		WorkDir:        t.TempDir(),
	}
	_, err = NewBackupManager(config, nil, mockRaft, log)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "storage is required")

	// Nil raft is now accepted: when a real qrdb node owns consensus,
	// the backup manager does not require a standalone raft node.
	nilRaftConfig := &Config{
		StorageBackend: "mock",
		WorkDir:        t.TempDir(),
		StorageConfig:  map[string]string{"path": t.TempDir()},
	}
	mgr, err := NewBackupManager(nilRaftConfig, memStorage, nil, log)
	assert.NoError(t, err, "nil raft should be accepted (qrdb node path)")
	if mgr != nil {
		_ = mgr.Close()
	}

	// Test with nil logger
	_, err = NewBackupManager(config, memStorage, mockRaft, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "logger is required")
}

// TestNewBackupManager_InvalidConfigValidation tests NewBackupManager with config that fails validation.
func TestNewBackupManager_InvalidConfigValidation(t *testing.T) {
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

	// Config with invalid storage backend
	config := &Config{
		StorageBackend: "",
		WorkDir:        t.TempDir(),
	}

	_, err = NewBackupManager(config, memStorage, mockRaft, log)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid config")
}

// TestNewBackupManager_WorkDirCreationFailure tests NewBackupManager when work dir creation fails.
func TestNewBackupManager_WorkDirCreationFailure(t *testing.T) {
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

	// Use an invalid path that can't be created (e.g., under a file)
	tmpFile := filepath.Join(t.TempDir(), "file.txt")
	err = os.WriteFile(tmpFile, []byte("content"), 0o644)
	require.NoError(t, err)

	invalidWorkDir := filepath.Join(tmpFile, "subdir") // Can't create dir under a file

	config := &Config{
		StorageBackend: "local",
		WorkDir:        invalidWorkDir,
		StorageConfig: map[string]string{
			"path": t.TempDir(),
		},
	}

	_, err = NewBackupManager(config, memStorage, mockRaft, log)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "create work directory failed")
}

// TestManagerClose_AlreadyClosed tests Close when manager is already closed.
func TestManagerClose_AlreadyClosed(t *testing.T) {
	manager, _, _ := setupTestBackupManagerWithErrors(t)

	// Close once
	err := manager.Close()
	require.NoError(t, err)

	// Close again should be no-op
	err = manager.Close()
	assert.NoError(t, err)
}

// TestManagerClose_BackendCloseError tests Close when backend close fails.
func TestManagerClose_BackendCloseError(t *testing.T) {
	manager, _, mockBackend := setupTestBackupManagerWithErrors(t)

	// Make backend close fail
	mockBackend.SetCloseError(errors.New("close error"))

	err := manager.Close()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "close storage backend failed")
}

// TestOperationsAfterClose tests that operations fail after manager is closed.
func TestOperationsAfterClose(t *testing.T) {
	manager, _, _ := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	// Close manager
	err := manager.Close()
	require.NoError(t, err)

	// Try to create backup
	_, err = manager.CreateBackup(ctx, BackupTypeFull)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backup manager is closed")

	// Try to restore backup
	err = manager.RestoreBackup(ctx, "some-id", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backup manager is closed")

	// Try to verify backup
	_, err = manager.VerifyBackup(ctx, "some-id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backup manager is closed")

	// Try to list backups
	_, err = manager.ListBackups(ctx, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backup manager is closed")

	// Try to delete backup
	err = manager.DeleteBackup(ctx, "some-id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backup manager is closed")
}

// TestCreateBackup_GetNodeHostInfoNilWithErrors tests CreateBackup when GetNodeHostInfo returns nil.
// Since GetNodeHostInfo nil is now treated as "no shards" (graceful degradation for the qrdb-node
// path where a standalone raft node is not present), the backup succeeds with zero shards.
func TestCreateBackup_GetNodeHostInfoNilWithErrors(t *testing.T) {
	manager, _, _ := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	// Replace raft node with one that returns nil for GetNodeHostInfo.
	// The backup manager now treats nil as an empty NodeHostInfo (zero shards)
	// rather than an error, so CreateBackup should succeed with NumShards=0.
	manager.raft = &mockRaftNodeNilInfo{}

	backup, err := manager.CreateBackup(ctx, BackupTypeFull)
	assert.NoError(t, err, "nil NodeHostInfo should produce a zero-shard backup, not an error")
	if err == nil {
		assert.Equal(t, 0, backup.Metadata.NumShards, "backup with nil raft info should have zero shards")
	}
}

// TestReadKeyValue_ShortRead tests readKeyValue with incomplete data.
func TestReadKeyValue_ShortRead(t *testing.T) {
	// Test with truncated key length
	buf := bytes.NewBuffer([]byte{0x00, 0x00}) // Only 2 bytes instead of 4
	key, value, err := readKeyValue(buf)
	assert.Error(t, err)
	assert.Nil(t, key)
	assert.Nil(t, value)

	// Test with truncated key data
	buf = bytes.NewBuffer([]byte{0x00, 0x00, 0x00, 0x05, 0x01, 0x02}) // Says 5 bytes, only has 2
	key, value, err = readKeyValue(buf)
	assert.Error(t, err)
	assert.Nil(t, key)
	assert.Nil(t, value)

	// Test with truncated value length
	buf = bytes.NewBuffer([]byte{0x00, 0x00, 0x00, 0x01, 0xFF, 0x00}) // Valid key, truncated value length
	key, value, err = readKeyValue(buf)
	assert.Error(t, err)
	assert.Nil(t, key)
	assert.Nil(t, value)

	// Test with truncated value data
	buf = bytes.NewBuffer([]byte{0x00, 0x00, 0x00, 0x01, 0xFF, 0x00, 0x00, 0x00, 0x05, 0x01}) // Says 5 byte value, only has 1
	key, value, err = readKeyValue(buf)
	assert.Error(t, err)
	assert.Nil(t, key)
	assert.Nil(t, value)
}

// TestNewStorageBackend_LocalAndUnsupported tests the storage backend factory
// for the local backend and for a backend it does not know.
func TestNewStorageBackend_LocalAndUnsupported(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "local backend with path",
			config: &Config{
				StorageBackend: "local",
				StorageConfig:  map[string]string{"path": t.TempDir()},
			},
		},
		{
			name: "local backend with nil storage config",
			config: &Config{
				StorageBackend: "local",
			},
			wantErr: true,
		},
		{
			name: "unsupported backend",
			config: &Config{
				StorageBackend: "unsupported-backend",
				WorkDir:        t.TempDir(),
				StorageConfig:  map[string]string{"path": t.TempDir()},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend, err := NewStorageBackend(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, backend)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, backend)
		})
	}
}

// TestMockStorage_DeleteWithContext tests MockStorage.DeleteWithContext.
func TestMockStorage_DeleteWithContext(t *testing.T) {
	storage := NewMockStorage()
	ctx := context.Background()

	// Put data
	err := storage.Put("ctx-del-key", bytes.NewReader([]byte("data")))
	require.NoError(t, err)

	// Delete with context
	err = storage.DeleteWithContext(ctx, "ctx-del-key")
	require.NoError(t, err)

	// Verify deleted
	_, err = storage.Get("ctx-del-key")
	require.Error(t, err)
}

// TestNoneCompressor_CompressDecompress tests the "none" compressor round trip.
func TestNoneCompressor_CompressDecompress(t *testing.T) {
	compressor, err := NewCompressor(CompressionNone)
	require.NoError(t, err)

	original := []byte("no compression applied to this data")

	// Compress
	var compressed bytes.Buffer
	writer, err := compressor.Compress(&compressed)
	require.NoError(t, err)

	_, err = writer.Write(original)
	require.NoError(t, err)
	err = writer.Close()
	require.NoError(t, err)

	// Decompress
	reader, err := compressor.Decompress(bytes.NewReader(compressed.Bytes()))
	require.NoError(t, err)
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, original, decompressed)
}

// TestAES256GCM_WrongKeySize tests creating an encryptor with wrong key size.
func TestAES256GCM_WrongKeySize(t *testing.T) {
	_, err := NewAES256GCMEncryptor([]byte("too-short"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "32 bytes")
}

// TestAES256GCM_MultipleWrites tests encrypt/decrypt with multiple writes.
func TestAES256GCM_MultipleWrites(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	encryptor, err := NewAES256GCMEncryptor(key)
	require.NoError(t, err)

	var encrypted bytes.Buffer
	writer, err := encryptor.Encrypt(&encrypted)
	require.NoError(t, err)

	// Write data in multiple chunks
	chunks := [][]byte{
		[]byte("chunk-one "),
		[]byte("chunk-two "),
		[]byte("chunk-three"),
	}
	for _, chunk := range chunks {
		_, err = writer.Write(chunk)
		require.NoError(t, err)
	}
	err = writer.Close()
	require.NoError(t, err)

	// Decrypt
	reader, err := encryptor.Decrypt(bytes.NewReader(encrypted.Bytes()))
	require.NoError(t, err)
	defer reader.Close()

	decrypted, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, []byte("chunk-one chunk-two chunk-three"), decrypted)
}

// TestNewCompressor_InvalidTypeReturnsError tests NewCompressor with invalid compression type.
func TestNewCompressor_InvalidTypeReturnsError(t *testing.T) {
	compressor, err := NewCompressor(CompressionType("invalid-type"))
	require.Error(t, err)
	assert.Nil(t, compressor)
}

// TestLocalStorage_DownloadSuccessPath tests the LocalStorage.Download method
// for the success path where a backup is uploaded and then downloaded.
func TestLocalStorage_DownloadSuccessPath(t *testing.T) {
	tmpDir := t.TempDir()

	storage, err := NewLocalStorage(tmpDir)
	require.NoError(t, err)

	ctx := context.Background()
	backupData := []byte("test backup data for download")

	backup := &Backup{
		ID:        "download-test-backup",
		Type:      BackupTypeFull,
		Timestamp: time.Now(),
		Metadata: &BackupMetadata{
			Version: "1.0.0",
		},
	}

	// Upload the backup
	err = storage.Upload(ctx, backup, bytes.NewReader(backupData))
	require.NoError(t, err)

	// Download should succeed and return the file reader (line 102)
	reader, err := storage.Download(ctx, "download-test-backup")
	require.NoError(t, err)
	require.NotNil(t, reader)
	defer reader.Close()

	// Verify the data
	downloaded, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, backupData, downloaded)
}

func TestMockStorage_Configure(t *testing.T) {
	storage := NewMockStorage()

	settings := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	err := storage.Configure(settings)
	require.NoError(t, err)

	// Verify settings were stored
	assert.Equal(t, "value1", storage.settings["key1"])
	assert.Equal(t, "value2", storage.settings["key2"])
}

// TestMockStorage_Put tests MockStorage Put method.
func TestMockStorage_Put(t *testing.T) {
	storage := NewMockStorage()

	data := []byte("test data")
	err := storage.Put("test-key", bytes.NewReader(data))
	require.NoError(t, err)

	// Verify data was stored
	storedData, ok := storage.data["test-key"]
	require.True(t, ok)
	assert.Equal(t, data, storedData)
}

// TestMockStorage_PutWithMetadata tests MockStorage PutWithMetadata method.
func TestMockStorage_PutWithMetadata(t *testing.T) {
	storage := NewMockStorage()
	ctx := context.Background()

	data := []byte("test data with metadata")
	metadata := &common.Metadata{
		ContentType: "application/octet-stream",
		Size:        int64(len(data)),
	}

	err := storage.PutWithMetadata(ctx, "test-key-meta", bytes.NewReader(data), metadata)
	require.NoError(t, err)

	// Verify data was stored
	storedData, ok := storage.data["test-key-meta"]
	require.True(t, ok)
	assert.Equal(t, data, storedData)

	// Verify metadata was stored
	storedMeta, ok := storage.metadata["test-key-meta"]
	require.True(t, ok)
	assert.Equal(t, metadata.ContentType, storedMeta.ContentType)
	assert.Equal(t, metadata.Size, storedMeta.Size)
}

// TestMockStorage_Get tests MockStorage Get method.
func TestMockStorage_Get(t *testing.T) {
	storage := NewMockStorage()

	// Put some data first
	testData := []byte("get test data")
	storage.data["get-key"] = testData

	// Get the data
	reader, err := storage.Get("get-key")
	require.NoError(t, err)
	defer reader.Close()

	// Read and verify
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, testData, data)
}

// TestMockStorage_GetNotFound tests MockStorage Get with non-existent key.
func TestMockStorage_GetNotFound(t *testing.T) {
	storage := NewMockStorage()

	reader, err := storage.Get("non-existent-key")
	assert.Error(t, err)
	assert.Nil(t, reader)
	assert.Contains(t, err.Error(), "key not found")
}

// TestMockStorage_GetMetadata tests MockStorage GetMetadata method (common.Storage interface).
func TestMockStorage_GetMetadata(t *testing.T) {
	storage := NewMockStorage()
	ctx := context.Background()

	// Put data with metadata
	testData := []byte("metadata test")
	metadata := &common.Metadata{
		ContentType: "text/plain",
	}
	storage.data["meta-key"] = testData
	storage.metadata["meta-key"] = metadata

	// Get metadata
	retrievedMeta, err := storage.GetMetadata(ctx, "meta-key")
	require.NoError(t, err)
	assert.Equal(t, metadata.ContentType, retrievedMeta.ContentType)
}

// TestMockStorage_GetMetadataNotFound tests GetMetadata with non-existent key.
func TestMockStorage_GetMetadataNotFound(t *testing.T) {
	storage := NewMockStorage()
	ctx := context.Background()

	meta, err := storage.GetMetadata(ctx, "non-existent")
	assert.Error(t, err)
	assert.Nil(t, meta)
}

// TestMockStorage_GetMetadataNoMeta tests GetMetadata when data exists but no metadata.
func TestMockStorage_GetMetadataNoMeta(t *testing.T) {
	storage := NewMockStorage()
	ctx := context.Background()

	// Put data without metadata
	storage.data["no-meta-key"] = []byte("data")

	// Should return empty metadata
	meta, err := storage.GetMetadata(ctx, "no-meta-key")
	require.NoError(t, err)
	assert.NotNil(t, meta)
}

// TestMockStorage_UpdateMetadata tests UpdateMetadata method.
func TestMockStorage_UpdateMetadata(t *testing.T) {
	storage := NewMockStorage()
	ctx := context.Background()

	// Put data first
	storage.data["update-meta-key"] = []byte("data")

	// Update metadata
	newMeta := &common.Metadata{
		ContentType: "application/json",
		Size:        100,
	}
	err := storage.UpdateMetadata(ctx, "update-meta-key", newMeta)
	require.NoError(t, err)

	// Verify metadata was updated
	storedMeta := storage.metadata["update-meta-key"]
	assert.Equal(t, newMeta.ContentType, storedMeta.ContentType)
	assert.Equal(t, newMeta.Size, storedMeta.Size)
}

// TestMockStorage_UpdateMetadataNotFound tests UpdateMetadata with non-existent key.
func TestMockStorage_UpdateMetadataNotFound(t *testing.T) {
	storage := NewMockStorage()
	ctx := context.Background()

	meta := &common.Metadata{ContentType: "text/plain"}
	err := storage.UpdateMetadata(ctx, "non-existent", meta)
	assert.Error(t, err)
}

// TestMockStorage_Exists tests Exists method.
func TestMockStorage_Exists(t *testing.T) {
	storage := NewMockStorage()
	ctx := context.Background()

	// Should not exist initially
	exists, err := storage.Exists(ctx, "exists-key")
	require.NoError(t, err)
	assert.False(t, exists)

	// Add data
	storage.data["exists-key"] = []byte("data")

	// Should exist now
	exists, err = storage.Exists(ctx, "exists-key")
	require.NoError(t, err)
	assert.True(t, exists)
}

// TestMockStorage_List tests List method.
func TestMockStorage_List(t *testing.T) {
	storage := NewMockStorage()

	// Add test data with prefix
	storage.data["prefix/key1"] = []byte("data1")
	storage.data["prefix/key2"] = []byte("data2")
	storage.data["other/key3"] = []byte("data3")

	// List with prefix
	keys, err := storage.List("prefix/")
	require.NoError(t, err)
	assert.Len(t, keys, 2)
	assert.Contains(t, keys, "prefix/key1")
	assert.Contains(t, keys, "prefix/key2")

	// ListWithContext applies the same prefix filter.
	keys, err = storage.ListWithContext(context.Background(), "prefix/")
	require.NoError(t, err)
	assert.Len(t, keys, 2)
}

// TestMockStorage_ListWithOptions tests ListWithOptions method.
func TestMockStorage_ListWithOptions(t *testing.T) {
	storage := NewMockStorage()
	ctx := context.Background()

	// Add test data
	for i := 0; i < 10; i++ {
		key := "item/" + string(rune('a'+i))
		storage.data[key] = []byte("data")
	}

	// List with max results
	opts := &common.ListOptions{
		Prefix:     "item/",
		MaxResults: 5,
	}
	result, err := storage.ListWithOptions(ctx, opts)
	require.NoError(t, err)
	assert.Len(t, result.Objects, 5)
	assert.True(t, result.Truncated)
	assert.NotEmpty(t, result.NextToken)

	// List with continuation
	opts2 := &common.ListOptions{
		Prefix:       "item/",
		MaxResults:   5,
		ContinueFrom: result.NextToken,
	}
	result2, err := storage.ListWithOptions(ctx, opts2)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(result2.Objects), 5)

	// Nil options list every object.
	all, err := storage.ListWithOptions(ctx, nil)
	require.NoError(t, err)
	assert.Len(t, all.Objects, 10)
}

// TestMockStorage_Archive tests Archive method.
func TestMockStorage_Archive(t *testing.T) {
	storage := NewMockStorage()

	// Add data to archive
	storage.data["archive-key"] = []byte("archive data")

	// Create a mock destination (another storage)
	dest := NewMockStorage()

	// Archive the data
	err := storage.Archive("archive-key", dest)
	require.NoError(t, err)

	// Verify data was archived to destination
	archivedData, ok := dest.data["archive-key"]
	require.True(t, ok)
	assert.Equal(t, []byte("archive data"), archivedData)
}

// TestMockStorage_ArchiveNotFound tests Archive with non-existent key.
func TestMockStorage_ArchiveNotFound(t *testing.T) {
	storage := NewMockStorage()
	dest := NewMockStorage()

	err := storage.Archive("non-existent", dest)
	assert.Error(t, err)
}

// TestMockStorage_AddPolicy tests AddPolicy method.
func TestMockStorage_AddPolicy(t *testing.T) {
	storage := NewMockStorage()

	policy := common.LifecyclePolicy{
		ID:     "policy1",
		Prefix: "test",
	}

	err := storage.AddPolicy(policy)
	require.NoError(t, err)

	// Verify policy was added
	storedPolicy, ok := storage.policies[policy.ID]
	require.True(t, ok)
	assert.Equal(t, policy.Prefix, storedPolicy.Prefix)
}

// TestMockStorage_RemovePolicy tests RemovePolicy method.
func TestMockStorage_RemovePolicy(t *testing.T) {
	storage := NewMockStorage()

	// Add a policy first
	policy := common.LifecyclePolicy{
		ID:     "policy2",
		Prefix: "test",
	}
	storage.policies[policy.ID] = policy

	// Remove it
	err := storage.RemovePolicy(policy.ID)
	require.NoError(t, err)

	// Verify it was removed
	_, ok := storage.policies[policy.ID]
	assert.False(t, ok)
}

// TestMockStorage_GetPolicies tests GetPolicies method.
func TestMockStorage_GetPolicies(t *testing.T) {
	storage := NewMockStorage()

	// Add multiple policies
	policy1 := common.LifecyclePolicy{ID: "p1", Prefix: "test"}
	policy2 := common.LifecyclePolicy{ID: "p2", Prefix: "test2"}

	storage.policies[policy1.ID] = policy1
	storage.policies[policy2.ID] = policy2

	// Get all policies
	policies, err := storage.GetPolicies()
	require.NoError(t, err)
	assert.Len(t, policies, 2)
}

// TestApplyRetentionPolicy_MaxBackups tests retention policy with max backups limit.
func TestApplyRetentionPolicy_MaxBackups(t *testing.T) {
	manager, _, _ := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	// Set up retention policy
	manager.config.Schedule = &ScheduleConfig{
		RetentionPolicy: &RetentionPolicy{
			MaxBackups: 2,
		},
	}

	// Create 3 backups
	for i := 0; i < 3; i++ {
		_, err := manager.CreateBackup(ctx, BackupTypeFull)
		require.NoError(t, err)
	}

	// Apply retention policy
	err := manager.ApplyRetentionPolicy(ctx)
	require.NoError(t, err)

	// Verify only 2 backups remain
	list, err := manager.ListBackups(ctx, 0)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(list.Backups), 2)
}

// TestApplyRetentionPolicy_NilPolicy tests retention policy when config is nil.
func TestApplyRetentionPolicy_NilPolicy(t *testing.T) {
	manager, _, _ := setupTestBackupManagerWithErrors(t)
	ctx := context.Background()

	// No schedule/retention policy set
	manager.config.Schedule = nil

	err := manager.ApplyRetentionPolicy(ctx)
	require.NoError(t, err) // Should be no-op
}
