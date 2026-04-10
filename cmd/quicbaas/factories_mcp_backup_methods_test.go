//go:build !integration && hashicorpmetrics && local

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// TestMCPBackupClient_CreateBackup tests the MCP backup create method.
func TestMCPBackupClient_CreateBackup(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		respMap := map[string]interface{}{
			"backup_id":   "b-create-1",
			"type":        "full",
			"size":        float64(1024),
			"compression": "gzip",
			"encryption":  "aes256",
			"success":     true,
			"message":     "backup created",
		}
		c := &mcpBackupClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.CreateBackup(ctx, &BackupCreateRequest{
			Name:        "my-backup",
			BackupType:  "full",
			Compression: "gzip",
			Encryption:  "aes256",
			StoragePath: "/data/backups",
			StorageType: "s3",
			Tags:        map[string]string{"env": "prod"},
		})
		require.NoError(t, err)
		assert.Equal(t, "b-create-1", resp.BackupID)
		assert.Equal(t, "full", resp.Type)
		assert.Equal(t, int64(1024), resp.Size)
		assert.True(t, resp.Success)
	})

	t.Run("MinimalArgs", func(t *testing.T) {
		respMap := map[string]interface{}{
			"backup_id": "b-create-2",
			"success":   true,
		}
		c := &mcpBackupClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.CreateBackup(ctx, &BackupCreateRequest{})
		require.NoError(t, err)
		assert.Equal(t, "b-create-2", resp.BackupID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpBackupClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.CreateBackup(ctx, &BackupCreateRequest{Name: "b"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPBackupClient_ListBackups tests the MCP backup list method.
func TestMCPBackupClient_ListBackups(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		respMap := map[string]interface{}{
			"total_count": float64(2),
			"backups": []interface{}{
				map[string]interface{}{
					"backup_id": "b-1",
					"type":      "full",
					"size":      float64(100),
				},
				map[string]interface{}{
					"backup_id": "b-2",
					"type":      "incremental",
					"size":      float64(50),
				},
			},
		}
		c := &mcpBackupClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.ListBackups(ctx, &BackupListRequest{
			Limit:       10,
			Offset:      0,
			StoragePath: "/backups",
			StorageType: "local",
		})
		require.NoError(t, err)
		assert.Equal(t, 2, resp.TotalCount)
		assert.Len(t, resp.Backups, 2)
		assert.Equal(t, "b-1", resp.Backups[0].ID)
	})

	t.Run("EmptyList", func(t *testing.T) {
		respMap := map[string]interface{}{
			"total_count": float64(0),
			"backups":     []interface{}{},
		}
		c := &mcpBackupClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.ListBackups(ctx, &BackupListRequest{})
		require.NoError(t, err)
		assert.Equal(t, 0, resp.TotalCount)
		assert.Empty(t, resp.Backups)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpBackupClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.ListBackups(ctx, &BackupListRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPBackupClient_RestoreBackup tests the MCP backup restore method.
func TestMCPBackupClient_RestoreBackup(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		respMap := map[string]interface{}{
			"backup_id":     "b-restore-1",
			"keys_restored": float64(500),
			"success":       true,
			"message":       "restore complete",
		}
		c := &mcpBackupClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.RestoreBackup(ctx, &BackupRestoreRequest{
			BackupID:       "b-restore-1",
			StoragePath:    "/data/backups",
			VerifyFirst:    true,
			VerifyChecksum: true,
			SkipExisting:   true,
			StorageType:    "s3",
			EncryptionKey:  "my-key",
		})
		require.NoError(t, err)
		assert.Equal(t, "b-restore-1", resp.BackupID)
		assert.Equal(t, int64(500), resp.KeysRestored)
		assert.True(t, resp.Success)
	})

	t.Run("MinimalArgs", func(t *testing.T) {
		respMap := map[string]interface{}{
			"backup_id": "b-restore-2",
			"success":   true,
		}
		c := &mcpBackupClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.RestoreBackup(ctx, &BackupRestoreRequest{
			BackupID: "b-restore-2",
		})
		require.NoError(t, err)
		assert.Equal(t, "b-restore-2", resp.BackupID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpBackupClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.RestoreBackup(ctx, &BackupRestoreRequest{BackupID: "b"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPBackupClient_DeleteBackup tests the MCP backup delete method.
func TestMCPBackupClient_DeleteBackup(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		respMap := map[string]interface{}{
			"backup_id": "b-del-1",
			"deleted":   true,
			"message":   "deleted",
		}
		c := &mcpBackupClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.DeleteBackup(ctx, &BackupDeleteRequest{
			BackupID:    "b-del-1",
			StoragePath: "/data/backups",
			Force:       true,
		})
		require.NoError(t, err)
		assert.Equal(t, "b-del-1", resp.BackupID)
		assert.True(t, resp.Deleted)
	})

	t.Run("MinimalArgs", func(t *testing.T) {
		respMap := map[string]interface{}{
			"backup_id": "b-del-2",
			"deleted":   true,
		}
		c := &mcpBackupClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.DeleteBackup(ctx, &BackupDeleteRequest{
			BackupID: "b-del-2",
		})
		require.NoError(t, err)
		assert.Equal(t, "b-del-2", resp.BackupID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpBackupClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.DeleteBackup(ctx, &BackupDeleteRequest{BackupID: "b"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPBackupClient_VerifyBackup tests the MCP backup verify method.
func TestMCPBackupClient_VerifyBackup(t *testing.T) {
	ctx := context.Background()

	t.Run("Valid", func(t *testing.T) {
		respMap := map[string]interface{}{
			"backup_id":      "b-verify-1",
			"valid":          true,
			"checksum_valid": true,
			"shards_valid":   float64(5),
		}
		c := &mcpBackupClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.VerifyBackup(ctx, &BackupVerifyRequest{
			BackupID:      "b-verify-1",
			StoragePath:   "/data/backups",
			StorageType:   "s3",
			EncryptionKey: "key",
		})
		require.NoError(t, err)
		assert.Equal(t, "b-verify-1", resp.BackupID)
		assert.True(t, resp.Valid)
		assert.True(t, resp.ChecksumValid)
	})

	t.Run("MinimalArgs", func(t *testing.T) {
		respMap := map[string]interface{}{
			"backup_id": "b-verify-2",
			"valid":     true,
		}
		c := &mcpBackupClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.VerifyBackup(ctx, &BackupVerifyRequest{
			BackupID: "b-verify-2",
		})
		require.NoError(t, err)
		assert.True(t, resp.Valid)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpBackupClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.VerifyBackup(ctx, &BackupVerifyRequest{BackupID: "b"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPBackupClient_Close tests the MCP backup client close method.
func TestMCPBackupClient_Close(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		c := &mcpBackupClient{sdk: newTestSDKClient(nil, nil)}
		err := c.Close()
		assert.NoError(t, err)
	})
}

// TestMCPBackupClient_CallTool tests the internal callTool method.
func TestMCPBackupClient_CallTool(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		expected := map[string]interface{}{"result": "ok"}
		c := &mcpBackupClient{sdk: newTestSDKClient(expected, nil)}
		result, err := c.callTool(ctx, "test_tool", map[string]interface{}{"arg": "val"})
		require.NoError(t, err)
		assert.Equal(t, expected, result)
	})

	t.Run("Error", func(t *testing.T) {
		c := &mcpBackupClient{sdk: newTestSDKClient(nil, fmt.Errorf("transport error"))}
		result, err := c.callTool(ctx, "test_tool", nil)
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}

// TestCreateMCPBackupClient tests the factory function.
func TestCreateMCPBackupClient(t *testing.T) {
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	t.Run("Success", func(t *testing.T) {
		sdkClientFactory = func() (quicbaas.Client, error) {
			return newTestSDKClient(nil, nil), nil
		}
		client, err := createMCPBackupClient()
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("FactoryError", func(t *testing.T) {
		sdkClientFactory = func() (quicbaas.Client, error) {
			return nil, fmt.Errorf("factory error")
		}
		client, err := createMCPBackupClient()
		assert.Error(t, err)
		assert.Nil(t, client)
	})
}
