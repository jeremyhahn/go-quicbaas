//go:build !integration && hashicorpmetrics && local

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBackupCreateResponse(t *testing.T) {
	t.Run("SnakeCaseKeys", func(t *testing.T) {
		data := map[string]interface{}{
			"backup_id":   "b-123",
			"type":        "full",
			"size":        float64(1024),
			"compression": "gzip",
			"encryption":  "aes",
			"success":     true,
			"message":     "backup created",
		}
		resp, err := parseBackupCreateResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "b-123", resp.BackupID)
		assert.Equal(t, "full", resp.Type)
		assert.Equal(t, int64(1024), resp.Size)
		assert.Equal(t, "gzip", resp.Compression)
		assert.Equal(t, "aes", resp.Encryption)
		assert.True(t, resp.Success)
		assert.Equal(t, "backup created", resp.Message)
	})

	t.Run("PascalCaseKeys", func(t *testing.T) {
		data := map[string]interface{}{
			"BackupID": "b-456",
			"Status":   "completed",
			"Size":     float64(2048),
		}
		resp, err := parseBackupCreateResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "b-456", resp.BackupID)
		assert.True(t, resp.Success) // Status mapped to Success
		assert.Equal(t, "completed", resp.Message)
	})

	t.Run("NilInput", func(t *testing.T) {
		resp, err := parseBackupCreateResponse(nil)
		require.NoError(t, err)
		assert.Equal(t, "", resp.BackupID)
	})

	t.Run("EmptyMap", func(t *testing.T) {
		resp, err := parseBackupCreateResponse(map[string]interface{}{})
		require.NoError(t, err)
		assert.Equal(t, "", resp.BackupID)
		assert.False(t, resp.Timestamp.IsZero())
	})
}

func TestParseBackupListResponse(t *testing.T) {
	t.Run("WithBackups", func(t *testing.T) {
		data := map[string]interface{}{
			"total_count": float64(2),
			"backups": []interface{}{
				map[string]interface{}{
					"backup_id": "b-1",
					"type":      "full",
					"size":      float64(100),
				},
				map[string]interface{}{
					"BackupID": "b-2",
					"Type":     "incremental",
					"Size":     float64(200),
				},
			},
		}
		resp, err := parseBackupListResponse(data)
		require.NoError(t, err)
		assert.Equal(t, 2, resp.TotalCount)
		assert.Len(t, resp.Backups, 2)
		assert.Equal(t, "b-1", resp.Backups[0].ID)
		assert.Equal(t, "b-2", resp.Backups[1].ID)
	})

	t.Run("EmptyBackups", func(t *testing.T) {
		data := map[string]interface{}{
			"total_count": float64(0),
			"backups":     []interface{}{},
		}
		resp, err := parseBackupListResponse(data)
		require.NoError(t, err)
		assert.Equal(t, 0, resp.TotalCount)
		assert.Empty(t, resp.Backups)
	})

	t.Run("NilInput", func(t *testing.T) {
		resp, err := parseBackupListResponse(nil)
		require.NoError(t, err)
		assert.Empty(t, resp.Backups)
	})

	t.Run("NonMapBackupEntry", func(t *testing.T) {
		data := map[string]interface{}{
			"backups": []interface{}{
				"not-a-map",
				map[string]interface{}{"backup_id": "b-valid"},
			},
		}
		resp, err := parseBackupListResponse(data)
		require.NoError(t, err)
		assert.Len(t, resp.Backups, 1) // skipped the non-map entry
		assert.Equal(t, "b-valid", resp.Backups[0].ID)
	})
}

func TestParseBackupRestoreResponse(t *testing.T) {
	t.Run("SnakeCaseKeys", func(t *testing.T) {
		data := map[string]interface{}{
			"backup_id":     "b-restore",
			"keys_restored": float64(500),
			"success":       true,
			"message":       "restore complete",
		}
		resp, err := parseBackupRestoreResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "b-restore", resp.BackupID)
		assert.Equal(t, int64(500), resp.KeysRestored)
		assert.True(t, resp.Success)
	})

	t.Run("PascalCaseWithStatus", func(t *testing.T) {
		data := map[string]interface{}{
			"JobID":  "job-123",
			"Status": "completed",
		}
		resp, err := parseBackupRestoreResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "job-123", resp.BackupID)
		assert.True(t, resp.Success)
		assert.Equal(t, "completed", resp.Message)
	})

	t.Run("NilInput", func(t *testing.T) {
		resp, err := parseBackupRestoreResponse(nil)
		require.NoError(t, err)
		assert.Equal(t, "", resp.BackupID)
	})
}

func TestParseBackupDeleteResponse(t *testing.T) {
	t.Run("SnakeCaseSuccess", func(t *testing.T) {
		data := map[string]interface{}{
			"backup_id": "b-del",
			"deleted":   true,
			"message":   "deleted successfully",
		}
		resp, err := parseBackupDeleteResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "b-del", resp.BackupID)
		assert.True(t, resp.Deleted)
		assert.Equal(t, "deleted successfully", resp.Message)
	})

	t.Run("PascalCaseSuccess", func(t *testing.T) {
		data := map[string]interface{}{
			"BackupID": "b-del-2",
			"Success":  true,
			"Message":  "ok",
		}
		resp, err := parseBackupDeleteResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "b-del-2", resp.BackupID)
		assert.True(t, resp.Deleted)
	})

	t.Run("NilInput", func(t *testing.T) {
		resp, err := parseBackupDeleteResponse(nil)
		require.NoError(t, err)
		assert.Equal(t, "", resp.BackupID)
		assert.False(t, resp.Deleted)
	})
}

func TestParseBackupVerifyResponse(t *testing.T) {
	t.Run("ValidWithIssues", func(t *testing.T) {
		data := map[string]interface{}{
			"backup_id":      "b-verify",
			"valid":          false,
			"checksum_valid": false,
			"shards_valid":   float64(3),
			"issues":         []interface{}{"checksum mismatch", "missing shard 4"},
		}
		resp, err := parseBackupVerifyResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "b-verify", resp.BackupID)
		assert.False(t, resp.Valid)
		assert.False(t, resp.ChecksumValid)
		assert.Equal(t, 3, resp.ShardsValid)
		assert.Len(t, resp.Issues, 2)
		assert.Equal(t, "checksum mismatch", resp.Issues[0])
	})

	t.Run("PascalCaseKeys", func(t *testing.T) {
		data := map[string]interface{}{
			"BackupID":      "b-verify-2",
			"Valid":         true,
			"ChecksumMatch": true,
		}
		resp, err := parseBackupVerifyResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "b-verify-2", resp.BackupID)
		assert.True(t, resp.Valid)
		assert.True(t, resp.ChecksumValid)
	})

	t.Run("NilInput", func(t *testing.T) {
		resp, err := parseBackupVerifyResponse(nil)
		require.NoError(t, err)
		assert.Equal(t, "", resp.BackupID)
		assert.Empty(t, resp.Issues)
	})

	t.Run("NonStringIssues", func(t *testing.T) {
		data := map[string]interface{}{
			"issues": []interface{}{42, "valid-issue"},
		}
		resp, err := parseBackupVerifyResponse(data)
		require.NoError(t, err)
		assert.Len(t, resp.Issues, 1) // only string issues parsed
		assert.Equal(t, "valid-issue", resp.Issues[0])
	})

	t.Run("NilIssues", func(t *testing.T) {
		data := map[string]interface{}{
			"valid": true,
		}
		resp, err := parseBackupVerifyResponse(data)
		require.NoError(t, err)
		assert.Empty(t, resp.Issues)
	})
}

// Interface compliance
func TestMCPBackupClientInterfaceCompliance(t *testing.T) {
	var _ BackupClient = (*mcpBackupClient)(nil)
}
