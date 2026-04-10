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
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewLocalStorage tests creating local storage with file I/O
func TestNewLocalStorage(t *testing.T) {
	t.Run("creates directory if not exists", func(t *testing.T) {
		tmpDir := filepath.Join(t.TempDir(), "backups")
		storage, err := NewLocalStorage(tmpDir)
		require.NoError(t, err)
		assert.NotNil(t, storage)

		// Verify directory was created
		info, err := os.Stat(tmpDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("works with existing directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage, err := NewLocalStorage(tmpDir)
		require.NoError(t, err)
		assert.NotNil(t, storage)
	})

	t.Run("fails with invalid path", func(t *testing.T) {
		// Try to create storage in a file (not directory)
		tmpFile := filepath.Join(t.TempDir(), "file")
		err := os.WriteFile(tmpFile, []byte("test"), 0o644)
		require.NoError(t, err)

		_, err = NewLocalStorage(filepath.Join(tmpFile, "subdir"))
		assert.Error(t, err)
	})
}

// TestLocalStorage_UploadDownload tests actual file I/O operations
func TestLocalStorage_UploadDownload(t *testing.T) {
	storage, err := NewLocalStorage(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()
	backup := &Backup{
		ID:          "test-backup-1",
		Type:        BackupTypeFull,
		Timestamp:   time.Now(),
		Compression: CompressionNone,
		Encryption:  EncryptionNone,
		Metadata:    &BackupMetadata{},
	}

	data := []byte("test backup data")

	// Upload
	err = storage.Upload(ctx, backup, bytes.NewReader(data))
	require.NoError(t, err)

	// Download
	reader, err := storage.Download(ctx, backup.ID)
	require.NoError(t, err)
	defer reader.Close()

	downloaded, err := io.ReadAll(reader)
	require.NoError(t, err)

	assert.Equal(t, data, downloaded)
}

// TestLocalStorage_UploadLargeFile tests uploading 10MB file to disk
func TestLocalStorage_UploadLargeFile(t *testing.T) {
	storage, err := NewLocalStorage(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()
	backup := &Backup{
		ID:          "large-backup",
		Type:        BackupTypeFull,
		Timestamp:   time.Now(),
		Compression: CompressionNone,
		Encryption:  EncryptionNone,
		Metadata:    &BackupMetadata{},
	}

	// Create 10MB of data
	largeData := make([]byte, 10*1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	err = storage.Upload(ctx, backup, bytes.NewReader(largeData))
	require.NoError(t, err)

	// Verify size was set
	assert.Equal(t, int64(len(largeData)), backup.Size)

	// Download and verify
	reader, err := storage.Download(ctx, backup.ID)
	require.NoError(t, err)
	defer reader.Close()

	downloaded, err := io.ReadAll(reader)
	require.NoError(t, err)

	assert.Equal(t, largeData, downloaded)
}

// TestLocalStorage_Delete tests file deletion on disk
func TestLocalStorage_Delete(t *testing.T) {
	storage, err := NewLocalStorage(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()
	backup := &Backup{
		ID:          "test-backup-delete",
		Type:        BackupTypeFull,
		Timestamp:   time.Now(),
		Compression: CompressionNone,
		Encryption:  EncryptionNone,
		Metadata:    &BackupMetadata{},
	}

	// Upload
	err = storage.Upload(ctx, backup, bytes.NewReader([]byte("test data")))
	require.NoError(t, err)

	// Verify exists
	_, err = storage.Download(ctx, backup.ID)
	require.NoError(t, err)

	// Delete
	err = storage.Delete(ctx, backup.ID)
	require.NoError(t, err)

	// Verify deleted
	_, err = storage.Download(ctx, backup.ID)
	assert.Error(t, err)
}

// TestLocalStorage_List tests listing backups from disk
func TestLocalStorage_List(t *testing.T) {
	storage, err := NewLocalStorage(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()

	// Create multiple backups
	backups := []string{"backup-1", "backup-2", "backup-3"}
	for i, id := range backups {
		backup := &Backup{
			ID:          id,
			Type:        BackupTypeFull,
			Timestamp:   time.Now().Add(time.Duration(i) * time.Second),
			Compression: CompressionNone,
			Encryption:  EncryptionNone,
			Metadata:    &BackupMetadata{},
		}
		err = storage.Upload(ctx, backup, bytes.NewReader([]byte("data")))
		require.NoError(t, err)
	}

	// List all
	list, err := storage.List(ctx, 0, "")
	require.NoError(t, err)

	assert.Len(t, list.Backups, 3)
	assert.Equal(t, 3, list.TotalCount)

	// Verify sorted by timestamp (newest first)
	assert.True(t, list.Backups[0].Timestamp.After(list.Backups[1].Timestamp) ||
		list.Backups[0].Timestamp.Equal(list.Backups[1].Timestamp))
}

// TestLocalStorage_GetMetadata tests reading metadata from disk
func TestLocalStorage_GetMetadata(t *testing.T) {
	storage, err := NewLocalStorage(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()
	backup := &Backup{
		ID:          "test-metadata",
		Type:        BackupTypeFull,
		Timestamp:   time.Now(),
		Compression: CompressionGzip,
		Encryption:  EncryptionAES256GCM,
		Metadata: &BackupMetadata{
			Version:   "1.0.0",
			NumShards: 3,
		},
	}

	// Upload
	err = storage.Upload(ctx, backup, bytes.NewReader([]byte("data")))
	require.NoError(t, err)

	// Get metadata
	metadata, err := storage.GetMetadata(ctx, backup.ID)
	require.NoError(t, err)

	assert.Equal(t, backup.ID, metadata.ID)
	assert.Equal(t, backup.Type, metadata.Type)
	assert.Equal(t, backup.Compression, metadata.Compression)
	assert.Equal(t, backup.Encryption, metadata.Encryption)
	assert.Equal(t, "1.0.0", metadata.Metadata.Version)
	assert.Equal(t, 3, metadata.Metadata.NumShards)
}

// TestLocalStorage_CorruptedMetadata tests handling corrupted metadata files on disk
func TestLocalStorage_CorruptedMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewLocalStorage(tmpDir)
	require.NoError(t, err)

	ctx := context.Background()

	// Create a backup directory with corrupted metadata
	backupID := "corrupted-backup"
	backupDir := filepath.Join(tmpDir, backupID)
	err = os.MkdirAll(backupDir, 0o755)
	require.NoError(t, err)

	// Write invalid JSON metadata
	metadataPath := filepath.Join(backupDir, "metadata.json")
	err = os.WriteFile(metadataPath, []byte("invalid json"), 0o644)
	require.NoError(t, err)

	// GetMetadata should fail
	_, err = storage.GetMetadata(ctx, backupID)
	assert.Error(t, err)

	// List should skip the corrupted backup
	list, err := storage.List(ctx, 0, "")
	require.NoError(t, err)
	assert.Empty(t, list.Backups)
}

// TestLocalStorage_ListReadDirError tests list with non-directory entries on disk
func TestLocalStorage_ListReadDirError(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := NewLocalStorage(tmpDir)
	require.NoError(t, err)

	ctx := context.Background()

	// Create a file instead of a directory to test non-directory skip
	backupPath := filepath.Join(tmpDir, "file.txt")
	err = os.WriteFile(backupPath, []byte("test"), 0o644)
	require.NoError(t, err)

	// List should skip the file
	list, err := storage.List(ctx, 0, "")
	require.NoError(t, err)
	assert.Empty(t, list.Backups)
}

// TestLocalStorage_ListPagination tests pagination with file I/O
func TestLocalStorage_ListPagination(t *testing.T) {
	storage, err := NewLocalStorage(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()

	// Create multiple backups
	backupIDs := []string{"backup-a", "backup-b", "backup-c", "backup-d"}
	for _, id := range backupIDs {
		backup := &Backup{
			ID:          id,
			Type:        BackupTypeFull,
			Timestamp:   time.Now(),
			Compression: CompressionNone,
			Encryption:  EncryptionNone,
			Metadata:    &BackupMetadata{},
		}
		err = storage.Upload(ctx, backup, bytes.NewReader([]byte("data")))
		require.NoError(t, err)
	}

	// List with pagination - skip first backup
	list, err := storage.List(ctx, 0, "backup-a")
	require.NoError(t, err)

	// Should not include backup-a (it's the nextToken, so we skip it)
	for _, b := range list.Backups {
		assert.NotEqual(t, "backup-a", b.ID)
	}
}
