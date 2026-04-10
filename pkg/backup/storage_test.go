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
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Note: S3Storage tests have been removed because we now use go-objstore's
// unified storage interface. To use cloud providers (S3, GCS, Azure), build
// with the appropriate tags: -tags awss3, -tags gcpstorage, -tags azureblob

// TestNewLocalStorage moved to storage_integration_test.go
// TestLocalStorage_UploadDownload moved to storage_integration_test.go
// TestLocalStorage_UploadLargeFile moved to storage_integration_test.go

func TestLocalStorage_DownloadNotFound(t *testing.T) {
	storage, err := NewLocalStorage(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()

	reader, err := storage.Download(ctx, "nonexistent-backup")
	assert.Error(t, err)
	assert.Nil(t, reader)
	assert.Contains(t, err.Error(), "not found")
}

// TestLocalStorage_Delete moved to storage_integration_test.go

func TestLocalStorage_DeleteNonexistent(t *testing.T) {
	storage, err := NewLocalStorage(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()

	// Delete should succeed even if backup doesn't exist
	err = storage.Delete(ctx, "nonexistent")
	require.NoError(t, err)
}

// TestLocalStorage_List moved to storage_integration_test.go
// TestLocalStorage_ListWithLimit moved to manager_integration_test.go

func TestLocalStorage_ListEmpty(t *testing.T) {
	storage, err := NewLocalStorage(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()

	list, err := storage.List(ctx, 0, "")
	require.NoError(t, err)

	assert.Empty(t, list.Backups)
	assert.Equal(t, 0, list.TotalCount)
}

// TestLocalStorage_GetMetadata moved to storage_integration_test.go

func TestLocalStorage_GetMetadataNotFound(t *testing.T) {
	storage, err := NewLocalStorage(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()

	metadata, err := storage.GetMetadata(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Nil(t, metadata)
	assert.Contains(t, err.Error(), "not found")
}

func TestLocalStorage_Close(t *testing.T) {
	storage, err := NewLocalStorage(t.TempDir())
	require.NoError(t, err)

	err = storage.Close()
	require.NoError(t, err)
}

func TestNewStorageBackend(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		wantErr     bool
		errContains string
	}{
		// NOTE: Local backend test is in integration tests because go-objstore
		// file backend requires proper initialization with real filesystem.
		{
			name: "unsupported backend",
			config: &Config{
				StorageBackend: "unsupported",
				StorageConfig:  map[string]string{},
			},
			wantErr:     true,
			errContains: "unsupported storage backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend, err := NewStorageBackend(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, backend)

			// Note: go-objstore backends don't implement io.Closer directly
			// They are stateless and don't require cleanup
		})
	}
}

func TestMockStorage(t *testing.T) {
	storage := NewMockStorage()
	ctx := context.Background()

	// Upload
	backup := &Backup{
		ID:          "mock-backup",
		Type:        BackupTypeFull,
		Timestamp:   time.Now(),
		Compression: CompressionNone,
		Encryption:  EncryptionNone,
		Metadata:    &BackupMetadata{},
	}

	data := []byte("mock data")
	err := storage.Upload(ctx, backup, bytes.NewReader(data))
	require.NoError(t, err)

	// Download
	reader, err := storage.Download(ctx, backup.ID)
	require.NoError(t, err)
	defer reader.Close()

	downloaded, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, data, downloaded)

	// Get backup metadata (using backup-specific method)
	backupMeta, err := storage.GetBackupMetadata(ctx, backup.ID)
	require.NoError(t, err)
	assert.Equal(t, backup.ID, backupMeta.ID)

	// List backups (using backup-specific method)
	list, err := storage.ListBackups(ctx, 0, "")
	require.NoError(t, err)
	assert.Len(t, list.Backups, 1)

	// Delete backup (using backup-specific method)
	err = storage.DeleteBackup(ctx, backup.ID)
	require.NoError(t, err)

	// Verify deleted
	_, err = storage.Download(ctx, backup.ID)
	assert.Error(t, err)

	// Close
	err = storage.Close()
	require.NoError(t, err)
}

func TestMockStorage_MultipleBackups(t *testing.T) {
	storage := NewMockStorage()
	ctx := context.Background()

	// Upload multiple backups
	for i := 0; i < 5; i++ {
		backup := &Backup{
			ID:          "backup-" + string(rune('1'+i)),
			Type:        BackupTypeFull,
			Timestamp:   time.Now().Add(time.Duration(i) * time.Second),
			Compression: CompressionNone,
			Encryption:  EncryptionNone,
			Metadata:    &BackupMetadata{},
		}
		err := storage.Upload(ctx, backup, bytes.NewReader([]byte("data")))
		require.NoError(t, err)
	}

	// List all backups (using backup-specific method)
	list, err := storage.ListBackups(ctx, 0, "")
	require.NoError(t, err)
	assert.Len(t, list.Backups, 5)

	// List backups with limit (using backup-specific method)
	list, err = storage.ListBackups(ctx, 2, "")
	require.NoError(t, err)
	assert.Len(t, list.Backups, 2)
}

// TestLocalStorage_CorruptedMetadata moved to storage_integration_test.go
