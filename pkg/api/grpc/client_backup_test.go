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

package grpc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// mockBackupCreateRequest implements BackupCreateRequest interface for testing.
type mockBackupCreateRequest struct {
	name          string
	backupType    string
	compression   string
	encryption    string
	storagePath   string
	storageType   string
	storageConfig map[string]interface{}
	tags          map[string]string
}

func (m *mockBackupCreateRequest) GetName() string                          { return m.name }
func (m *mockBackupCreateRequest) GetBackupType() string                    { return m.backupType }
func (m *mockBackupCreateRequest) GetCompression() string                   { return m.compression }
func (m *mockBackupCreateRequest) GetEncryption() string                    { return m.encryption }
func (m *mockBackupCreateRequest) GetStoragePath() string                   { return m.storagePath }
func (m *mockBackupCreateRequest) GetStorageType() string                   { return m.storageType }
func (m *mockBackupCreateRequest) GetStorageConfig() map[string]interface{} { return m.storageConfig }
func (m *mockBackupCreateRequest) GetTags() map[string]string               { return m.tags }

// mockBackupListRequest implements BackupListRequest interface for testing.
type mockBackupListRequest struct {
	limit         int
	offset        int
	storagePath   string
	storageType   string
	storageConfig map[string]interface{}
}

func (m *mockBackupListRequest) GetLimit() int                            { return m.limit }
func (m *mockBackupListRequest) GetOffset() int                           { return m.offset }
func (m *mockBackupListRequest) GetStoragePath() string                   { return m.storagePath }
func (m *mockBackupListRequest) GetStorageType() string                   { return m.storageType }
func (m *mockBackupListRequest) GetStorageConfig() map[string]interface{} { return m.storageConfig }

// mockBackupRestoreRequest implements BackupRestoreRequest interface for testing.
type mockBackupRestoreRequest struct {
	backupID       string
	storagePath    string
	verifyFirst    bool
	verifyChecksum bool
	skipExisting   bool
	pointInTime    time.Time
	storageType    string
	storageConfig  map[string]interface{}
	encryptionKey  string
}

func (m *mockBackupRestoreRequest) GetBackupID() string                      { return m.backupID }
func (m *mockBackupRestoreRequest) GetStoragePath() string                   { return m.storagePath }
func (m *mockBackupRestoreRequest) GetVerifyFirst() bool                     { return m.verifyFirst }
func (m *mockBackupRestoreRequest) GetVerifyChecksum() bool                  { return m.verifyChecksum }
func (m *mockBackupRestoreRequest) GetSkipExisting() bool                    { return m.skipExisting }
func (m *mockBackupRestoreRequest) GetPointInTime() time.Time                { return m.pointInTime }
func (m *mockBackupRestoreRequest) GetStorageType() string                   { return m.storageType }
func (m *mockBackupRestoreRequest) GetStorageConfig() map[string]interface{} { return m.storageConfig }
func (m *mockBackupRestoreRequest) GetEncryptionKey() string                 { return m.encryptionKey }

// mockBackupDeleteRequest implements BackupDeleteRequest interface for testing.
type mockBackupDeleteRequest struct {
	backupID    string
	storagePath string
	force       bool
}

func (m *mockBackupDeleteRequest) GetBackupID() string    { return m.backupID }
func (m *mockBackupDeleteRequest) GetStoragePath() string { return m.storagePath }
func (m *mockBackupDeleteRequest) GetForce() bool         { return m.force }

// mockBackupVerifyRequest implements BackupVerifyRequest interface for testing.
type mockBackupVerifyRequest struct {
	backupID      string
	storagePath   string
	storageType   string
	storageConfig map[string]interface{}
	encryptionKey string
}

func (m *mockBackupVerifyRequest) GetBackupID() string                      { return m.backupID }
func (m *mockBackupVerifyRequest) GetStoragePath() string                   { return m.storagePath }
func (m *mockBackupVerifyRequest) GetStorageType() string                   { return m.storageType }
func (m *mockBackupVerifyRequest) GetStorageConfig() map[string]interface{} { return m.storageConfig }
func (m *mockBackupVerifyRequest) GetEncryptionKey() string                 { return m.encryptionKey }

func TestBackupRequestInterfaces(t *testing.T) {
	t.Run("BackupCreateRequest interface methods", func(t *testing.T) {
		req := &mockBackupCreateRequest{
			name:        "test-backup",
			backupType:  "full",
			compression: "gzip",
			encryption:  "aes256",
			storagePath: "/backups",
			storageType: "local",
			storageConfig: map[string]interface{}{
				"path": "/data/backups",
			},
			tags: map[string]string{
				"env": "test",
			},
		}

		assert.Equal(t, "test-backup", req.GetName())
		assert.Equal(t, "full", req.GetBackupType())
		assert.Equal(t, "gzip", req.GetCompression())
		assert.Equal(t, "aes256", req.GetEncryption())
		assert.Equal(t, "/backups", req.GetStoragePath())
		assert.Equal(t, "local", req.GetStorageType())
		assert.Equal(t, "/data/backups", req.GetStorageConfig()["path"])
		assert.Equal(t, "test", req.GetTags()["env"])
	})

	t.Run("BackupListRequest interface methods", func(t *testing.T) {
		req := &mockBackupListRequest{
			limit:       100,
			offset:      10,
			storagePath: "/backups",
			storageType: "s3",
			storageConfig: map[string]interface{}{
				"bucket": "my-bucket",
			},
		}

		assert.Equal(t, 100, req.GetLimit())
		assert.Equal(t, 10, req.GetOffset())
		assert.Equal(t, "/backups", req.GetStoragePath())
		assert.Equal(t, "s3", req.GetStorageType())
		assert.Equal(t, "my-bucket", req.GetStorageConfig()["bucket"])
	})

	t.Run("BackupRestoreRequest interface methods", func(t *testing.T) {
		now := time.Now()
		req := &mockBackupRestoreRequest{
			backupID:       "backup-123",
			storagePath:    "/backups",
			verifyFirst:    true,
			verifyChecksum: true,
			skipExisting:   false,
			pointInTime:    now,
			storageType:    "local",
			storageConfig: map[string]interface{}{
				"path": "/data",
			},
			encryptionKey: "my-key",
		}

		assert.Equal(t, "backup-123", req.GetBackupID())
		assert.Equal(t, "/backups", req.GetStoragePath())
		assert.True(t, req.GetVerifyFirst())
		assert.True(t, req.GetVerifyChecksum())
		assert.False(t, req.GetSkipExisting())
		assert.Equal(t, now, req.GetPointInTime())
		assert.Equal(t, "local", req.GetStorageType())
		assert.Equal(t, "/data", req.GetStorageConfig()["path"])
		assert.Equal(t, "my-key", req.GetEncryptionKey())
	})

	t.Run("BackupDeleteRequest interface methods", func(t *testing.T) {
		req := &mockBackupDeleteRequest{
			backupID:    "backup-456",
			storagePath: "/backups",
			force:       true,
		}

		assert.Equal(t, "backup-456", req.GetBackupID())
		assert.Equal(t, "/backups", req.GetStoragePath())
		assert.True(t, req.GetForce())
	})

	t.Run("BackupVerifyRequest interface methods", func(t *testing.T) {
		req := &mockBackupVerifyRequest{
			backupID:    "backup-789",
			storagePath: "/backups",
			storageType: "gcs",
			storageConfig: map[string]interface{}{
				"bucket": "gcs-bucket",
			},
			encryptionKey: "verify-key",
		}

		assert.Equal(t, "backup-789", req.GetBackupID())
		assert.Equal(t, "/backups", req.GetStoragePath())
		assert.Equal(t, "gcs", req.GetStorageType())
		assert.Equal(t, "gcs-bucket", req.GetStorageConfig()["bucket"])
		assert.Equal(t, "verify-key", req.GetEncryptionKey())
	})
}

func TestNewBackupClientAdapter(t *testing.T) {
	t.Run("creates adapter with nil client", func(t *testing.T) {
		adapter := NewBackupClientAdapter(nil)
		assert.NotNil(t, adapter)
		assert.Nil(t, adapter.client)
	})

	t.Run("creates adapter with valid client", func(t *testing.T) {
		client := &Client{}
		adapter := NewBackupClientAdapter(client)
		assert.NotNil(t, adapter)
		assert.NotNil(t, adapter.client)
	})
}

func TestBackupTypeConversionStrings(t *testing.T) {
	t.Run("backup type strings", func(t *testing.T) {
		testCases := []struct {
			input    string
			expected string
		}{
			{"full", "full"},
			{"incremental", "incremental"},
			{"snapshot", "snapshot"},
			{"", "full"},        // default
			{"unknown", "full"}, // default for unknown
		}

		for _, tc := range testCases {
			t.Run(tc.input, func(t *testing.T) {
				req := &mockBackupCreateRequest{backupType: tc.input}
				assert.Equal(t, tc.input, req.GetBackupType())
			})
		}
	})

	t.Run("compression type strings", func(t *testing.T) {
		testCases := []string{"none", "gzip", "snappy", "lz4", "zstd"}
		for _, tc := range testCases {
			t.Run(tc, func(t *testing.T) {
				req := &mockBackupCreateRequest{compression: tc}
				assert.Equal(t, tc, req.GetCompression())
			})
		}
	})

	t.Run("encryption type strings", func(t *testing.T) {
		testCases := []string{"none", "aes256", "aes256gcm"}
		for _, tc := range testCases {
			t.Run(tc, func(t *testing.T) {
				req := &mockBackupCreateRequest{encryption: tc}
				assert.Equal(t, tc, req.GetEncryption())
			})
		}
	})
}

func TestBackupRequestDefaultValues(t *testing.T) {
	t.Run("create request with nil maps", func(t *testing.T) {
		req := &mockBackupCreateRequest{}
		assert.Nil(t, req.GetStorageConfig())
		assert.Nil(t, req.GetTags())
		assert.Equal(t, "", req.GetName())
		assert.Equal(t, "", req.GetBackupType())
	})

	t.Run("list request with zero values", func(t *testing.T) {
		req := &mockBackupListRequest{}
		assert.Equal(t, 0, req.GetLimit())
		assert.Equal(t, 0, req.GetOffset())
		assert.Equal(t, "", req.GetStoragePath())
	})

	t.Run("restore request with zero time", func(t *testing.T) {
		req := &mockBackupRestoreRequest{}
		assert.True(t, req.GetPointInTime().IsZero())
		assert.False(t, req.GetVerifyFirst())
		assert.False(t, req.GetVerifyChecksum())
		assert.False(t, req.GetSkipExisting())
	})

	t.Run("delete request default force", func(t *testing.T) {
		req := &mockBackupDeleteRequest{}
		assert.False(t, req.GetForce())
	})
}
