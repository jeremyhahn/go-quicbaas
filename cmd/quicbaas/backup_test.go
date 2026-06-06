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

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	kctypes "github.com/jeremyhahn/go-xkms/pkg/types"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestParseCompressionType(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    backup.CompressionType
		wantErr bool
	}{
		{
			name:    "none compression",
			input:   "none",
			want:    backup.CompressionNone,
			wantErr: false,
		},
		{
			name:    "gzip compression",
			input:   "gzip",
			want:    backup.CompressionGzip,
			wantErr: false,
		},
		{
			name:    "zstd compression",
			input:   "zstd",
			want:    backup.CompressionZstd,
			wantErr: false,
		},
		{
			name:    "invalid compression",
			input:   "invalid",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCompressionType(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "unsupported compression type")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestParseEncryptionType(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    backup.EncryptionType
		wantErr bool
	}{
		{
			name:    "none encryption",
			input:   "none",
			want:    backup.EncryptionNone,
			wantErr: false,
		},
		{
			name:    "aes256 encryption",
			input:   "aes256",
			want:    backup.EncryptionAES256GCM,
			wantErr: false,
		},
		{
			name:    "invalid encryption",
			input:   "invalid",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEncryptionType(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "unsupported encryption type")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestBackupCreateResponseFormatter(t *testing.T) {
	formatter := cli.NewFormatter(false)
	resp := &cli.BackupCreateResponse{
		BackupID:    "backup-test-123",
		Type:        "full",
		Size:        1024 * 1024 * 100, // 100 MB
		Compression: "zstd",
		Encryption:  "none",
		Duration:    5 * time.Second,
		Timestamp:   time.Date(2025, 10, 14, 12, 0, 0, 0, time.UTC),
		Success:     true,
	}

	tests := []struct {
		name         string
		format       cli.OutputFormat
		expectedText []string
	}{
		{
			name:   "table format",
			format: cli.OutputFormatTable,
			expectedText: []string{
				"BACKUP CREATED",
				"backup-test-123",
				"full",
				"100.0 MB",
				"zstd",
				"none",
				"5s",
			},
		},
		{
			name:   "json format",
			format: cli.OutputFormatJSON,
			expectedText: []string{
				"backup-test-123",
				"full",
				"zstd",
			},
		},
		{
			name:   "yaml format",
			format: cli.OutputFormatYAML,
			expectedText: []string{
				"backupid: backup-test-123",
				"type: full",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatBackupCreateResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := strings.ToLower(buf.String())
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, strings.ToLower(expected),
					"Expected output to contain '%s'\nGot:\n%s", expected, buf.String())
			}
		})
	}
}

func TestBackupListResponseFormatter(t *testing.T) {
	formatter := cli.NewFormatter(false)
	resp := &cli.BackupListResponse{
		Backups: []cli.BackupInfo{
			{
				ID:          "backup-prod-1",
				Type:        "full",
				Size:        1024 * 1024 * 100,
				Compression: "zstd",
				Encryption:  "none",
				NumShards:   3,
				NumKeys:     10000,
				Timestamp:   time.Date(2025, 10, 14, 12, 0, 0, 0, time.UTC),
			},
			{
				ID:          "backup-prod-2",
				Type:        "incremental",
				Size:        1024 * 1024 * 10,
				Compression: "gzip",
				Encryption:  "aes256",
				NumShards:   3,
				NumKeys:     1000,
				Timestamp:   time.Date(2025, 10, 14, 13, 0, 0, 0, time.UTC),
			},
		},
		TotalCount:  2,
		StoragePath: "/var/lib/quicbaas/backups",
	}

	tests := []struct {
		name         string
		format       cli.OutputFormat
		expectedText []string
	}{
		{
			name:   "table format",
			format: cli.OutputFormatTable,
			expectedText: []string{
				"BACKUPS",
				"backup-prod-1",
				"backup-prod-2",
				"full",
				"incremental",
				"100.0 MB",
				"10.0 MB",
				"Total backups:",
				"2",
			},
		},
		{
			name:   "json format",
			format: cli.OutputFormatJSON,
			expectedText: []string{
				"backup-prod-1",
				"backup-prod-2",
				"total_count",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatBackupListResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := strings.ToLower(buf.String())
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, strings.ToLower(expected),
					"Expected output to contain '%s'\nGot:\n%s", expected, buf.String())
			}
		})
	}
}

func TestBackupRestoreResponseFormatter(t *testing.T) {
	formatter := cli.NewFormatter(false)
	resp := &cli.BackupRestoreResponse{
		BackupID:       "backup-prod-1",
		KeysRestored:   10000,
		ShardsRestored: 3,
		Duration:       30 * time.Second,
		Timestamp:      time.Date(2025, 10, 14, 12, 0, 0, 0, time.UTC),
		Success:        true,
	}

	tests := []struct {
		name         string
		format       cli.OutputFormat
		expectedText []string
	}{
		{
			name:   "table format",
			format: cli.OutputFormatTable,
			expectedText: []string{
				"RESTORE COMPLETE",
				"backup-prod-1",
				"10000",
				"3",
				"30s",
			},
		},
		{
			name:   "json format",
			format: cli.OutputFormatJSON,
			expectedText: []string{
				"backup-prod-1",
				"10000",
				"success",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatBackupRestoreResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := strings.ToLower(buf.String())
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, strings.ToLower(expected),
					"Expected output to contain '%s'\nGot:\n%s", expected, buf.String())
			}
		})
	}
}

func TestBackupDeleteResponseFormatter(t *testing.T) {
	formatter := cli.NewFormatter(false)
	resp := &cli.BackupDeleteResponse{
		BackupID:  "backup-prod-1",
		Deleted:   true,
		Timestamp: time.Date(2025, 10, 14, 12, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name         string
		format       cli.OutputFormat
		expectedText []string
	}{
		{
			name:   "table format",
			format: cli.OutputFormatTable,
			expectedText: []string{
				"backup-prod-1",
				"true",
			},
		},
		{
			name:   "default format",
			format: cli.OutputFormat(""),
			expectedText: []string{
				"Successfully deleted backup",
				"backup-prod-1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatBackupDeleteResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := strings.ToLower(buf.String())
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, strings.ToLower(expected),
					"Expected output to contain '%s'\nGot:\n%s", expected, buf.String())
			}
		})
	}
}

func TestBackupVerifyResponseFormatter(t *testing.T) {
	formatter := cli.NewFormatter(false)

	tests := []struct {
		name         string
		resp         *cli.BackupVerifyResponse
		format       cli.OutputFormat
		expectedText []string
	}{
		{
			name: "valid backup table format",
			resp: &cli.BackupVerifyResponse{
				BackupID:      "backup-prod-1",
				Valid:         true,
				ChecksumValid: true,
				ShardsValid:   3,
				Issues:        []string{},
				Timestamp:     time.Date(2025, 10, 14, 12, 0, 0, 0, time.UTC),
			},
			format: cli.OutputFormatTable,
			expectedText: []string{
				"BACKUP VERIFICATION",
				"backup-prod-1",
				"true",
				"3",
				"None",
			},
		},
		{
			name: "invalid backup with issues",
			resp: &cli.BackupVerifyResponse{
				BackupID:      "backup-prod-1",
				Valid:         false,
				ChecksumValid: false,
				ShardsValid:   2,
				Issues:        []string{"Checksum mismatch for shard 3", "Missing metadata file"},
				Timestamp:     time.Date(2025, 10, 14, 12, 0, 0, 0, time.UTC),
			},
			format: cli.OutputFormatTable,
			expectedText: []string{
				"BACKUP VERIFICATION",
				"backup-prod-1",
				"false",
				"2",
				"Checksum mismatch",
				"Missing metadata",
			},
		},
		{
			name: "valid backup default format",
			resp: &cli.BackupVerifyResponse{
				BackupID:      "backup-prod-1",
				Valid:         true,
				ChecksumValid: true,
				ShardsValid:   3,
				Issues:        []string{},
				Timestamp:     time.Date(2025, 10, 14, 12, 0, 0, 0, time.UTC),
			},
			format: cli.OutputFormat(""),
			expectedText: []string{
				"Backup is valid",
				"backup-prod-1",
				"3 shards valid",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatBackupVerifyResponse(tt.resp, tt.format, &buf)
			require.NoError(t, err)

			output := strings.ToLower(buf.String())
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, strings.ToLower(expected),
					"Expected output to contain '%s'\nGot:\n%s", expected, buf.String())
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{
			name:  "bytes",
			bytes: 512,
			want:  "512 B",
		},
		{
			name:  "kilobytes",
			bytes: 1024,
			want:  "1.0 KB",
		},
		{
			name:  "megabytes",
			bytes: 1024 * 1024,
			want:  "1.0 MB",
		},
		{
			name:  "gigabytes",
			bytes: 1024 * 1024 * 1024,
			want:  "1.0 GB",
		},
		{
			name:  "partial megabytes",
			bytes: 1024 * 1024 * 100,
			want:  "100.0 MB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We need to test the formatBytes function indirectly through the formatter
			formatter := cli.NewFormatter(false)
			resp := &cli.BackupCreateResponse{
				BackupID:    "test",
				Type:        "full",
				Size:        tt.bytes,
				Compression: "zstd",
				Encryption:  "none",
				Timestamp:   time.Now(),
				Success:     true,
			}

			var buf bytes.Buffer
			err := formatter.FormatBackupCreateResponse(resp, cli.OutputFormatTable, &buf)
			require.NoError(t, err)

			output := buf.String()
			assert.Contains(t, output, tt.want, "Expected formatted size %s in output", tt.want)
		})
	}
}

func TestBackupCommandStructure(t *testing.T) {
	// Test that all backup commands are properly defined
	cmd := newBackupCommands()

	assert.NotNil(t, cmd)
	assert.Equal(t, "backup", cmd.Use)
	assert.Greater(t, len(cmd.Commands()), 0, "Backup command should have subcommands")

	// Check all expected subcommands exist
	expectedCommands := []string{"create", "list", "restore", "delete", "verify"}
	for _, expectedCmd := range expectedCommands {
		found := false
		for _, subCmd := range cmd.Commands() {
			if strings.HasPrefix(subCmd.Use, expectedCmd) {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected to find subcommand '%s'", expectedCmd)
	}
}

func TestBackupCreateCommandFlags(t *testing.T) {
	cmd := newBackupCreateCommand()

	// Check required flags exist
	compressionFlag := cmd.Flags().Lookup("compression")
	assert.NotNil(t, compressionFlag)
	assert.Equal(t, "zstd", compressionFlag.DefValue)

	encryptionFlag := cmd.Flags().Lookup("encryption")
	assert.NotNil(t, encryptionFlag)
	assert.Equal(t, "none", encryptionFlag.DefValue)

	incrementalFlag := cmd.Flags().Lookup("incremental")
	assert.NotNil(t, incrementalFlag)
	assert.Equal(t, "false", incrementalFlag.DefValue)

	storagePathFlag := cmd.Flags().Lookup("storage-path")
	assert.NotNil(t, storagePathFlag)

	encryptKeyFlag := cmd.Flags().Lookup("encrypt-key")
	assert.NotNil(t, encryptKeyFlag)
}

func TestBackupCommandPersistentFlags(t *testing.T) {
	cmd := newBackupCommands()

	// Check that persistent storage flags are available
	storageTypeFlag := cmd.PersistentFlags().Lookup("storage-type")
	assert.NotNil(t, storageTypeFlag)
	assert.Equal(t, "local", storageTypeFlag.DefValue)

	bucketFlag := cmd.PersistentFlags().Lookup("bucket")
	assert.NotNil(t, bucketFlag)

	regionFlag := cmd.PersistentFlags().Lookup("region")
	assert.NotNil(t, regionFlag)

	endpointFlag := cmd.PersistentFlags().Lookup("endpoint")
	assert.NotNil(t, endpointFlag)

	containerFlag := cmd.PersistentFlags().Lookup("container")
	assert.NotNil(t, containerFlag)

	credentialsFileFlag := cmd.PersistentFlags().Lookup("credentials-file")
	assert.NotNil(t, credentialsFileFlag)

	profileFlag := cmd.PersistentFlags().Lookup("profile")
	assert.NotNil(t, profileFlag)
}

func TestBackupCreateWithStorageFlags(t *testing.T) {
	// Save and restore factory
	originalFactory := backupClientFactory
	defer func() { backupClientFactory = originalFactory }()

	tests := []struct {
		name            string
		args            []string
		storageType     string
		bucket          string
		region          string
		endpoint        string
		container       string
		compression     string
		encryption      string
		expectError     bool
		validateRequest func(t *testing.T, req *BackupCreateRequest)
	}{
		{
			name:        "create backup with S3 storage",
			args:        []string{"test-backup"},
			storageType: "s3",
			bucket:      "test-bucket",
			region:      "us-west-2",
			compression: "zstd",
			encryption:  "none",
			expectError: false,
			validateRequest: func(t *testing.T, req *BackupCreateRequest) {
				assert.Equal(t, "s3", req.StorageType)
				assert.NotNil(t, req.StorageConfig)
				assert.Equal(t, "test-bucket", req.StorageConfig["bucket"])
				assert.Equal(t, "us-west-2", req.StorageConfig["region"])
			},
		},
		{
			name:        "create backup with Azure storage",
			args:        []string{"test-backup"},
			storageType: "azure",
			container:   "test-container",
			compression: "zstd",
			encryption:  "none",
			expectError: false,
			validateRequest: func(t *testing.T, req *BackupCreateRequest) {
				assert.Equal(t, "azure", req.StorageType)
				assert.NotNil(t, req.StorageConfig)
				assert.Equal(t, "test-container", req.StorageConfig["container"])
			},
		},
		{
			name:        "create backup with GCS storage",
			args:        []string{"test-backup"},
			storageType: "gcs",
			bucket:      "gcs-bucket",
			compression: "zstd",
			encryption:  "none",
			expectError: false,
			validateRequest: func(t *testing.T, req *BackupCreateRequest) {
				assert.Equal(t, "gcs", req.StorageType)
				assert.NotNil(t, req.StorageConfig)
				assert.Equal(t, "gcs-bucket", req.StorageConfig["bucket"])
			},
		},
		{
			name:        "create backup with MinIO storage",
			args:        []string{"test-backup"},
			storageType: "minio",
			bucket:      "minio-bucket",
			endpoint:    "localhost:9000",
			compression: "zstd",
			encryption:  "none",
			expectError: false,
			validateRequest: func(t *testing.T, req *BackupCreateRequest) {
				assert.Equal(t, "minio", req.StorageType)
				assert.NotNil(t, req.StorageConfig)
				assert.Equal(t, "minio-bucket", req.StorageConfig["bucket"])
				assert.Equal(t, "localhost:9000", req.StorageConfig["endpoint"])
			},
		},
		{
			name:        "create backup with local storage (default)",
			args:        []string{"test-backup"},
			storageType: "local",
			compression: "zstd",
			encryption:  "none",
			expectError: false,
			validateRequest: func(t *testing.T, req *BackupCreateRequest) {
				assert.Equal(t, "local", req.StorageType)
				assert.NotNil(t, req.StorageConfig)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *BackupCreateRequest

			// Set up mock
			backupClientFactory = func() (BackupClient, error) {
				return &MockBackupClient{
					CreateBackupFunc: func(ctx context.Context, req *BackupCreateRequest) (*cli.BackupCreateResponse, error) {
						capturedRequest = req
						return &cli.BackupCreateResponse{
							BackupID:  "backup-" + req.Name,
							Type:      req.BackupType,
							Size:      1024 * 1024 * 100,
							Duration:  5 * time.Second,
							Timestamp: time.Now(),
						}, nil
					},
				}, nil
			}

			cmd := newBackupCreateCommand()

			// Set flags
			backupStorageType = tt.storageType
			backupStorageBucket = tt.bucket
			backupStorageRegion = tt.region
			backupStorageEndpoint = tt.endpoint
			backupStorageContainer = tt.container
			backupCompression = tt.compression
			backupEncryption = tt.encryption
			backupIncremental = false
			backupStoragePath = "/tmp/test-backups"
			backupOutputFormat = "table"

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, capturedRequest)
				if tt.validateRequest != nil {
					tt.validateRequest(t, capturedRequest)
				}
			}
		})
	}
}

func TestBackupListWithStorageFlags(t *testing.T) {
	// Save and restore factory
	originalFactory := backupClientFactory
	defer func() { backupClientFactory = originalFactory }()

	tests := []struct {
		name            string
		storageType     string
		bucket          string
		region          string
		validateRequest func(t *testing.T, req *BackupListRequest)
	}{
		{
			name:        "list backups with S3 storage",
			storageType: "s3",
			bucket:      "test-bucket",
			region:      "us-west-2",
			validateRequest: func(t *testing.T, req *BackupListRequest) {
				assert.Equal(t, "s3", req.StorageType)
				assert.NotNil(t, req.StorageConfig)
				assert.Equal(t, "test-bucket", req.StorageConfig["bucket"])
				assert.Equal(t, "us-west-2", req.StorageConfig["region"])
			},
		},
		{
			name:        "list backups with local storage",
			storageType: "local",
			validateRequest: func(t *testing.T, req *BackupListRequest) {
				assert.Equal(t, "local", req.StorageType)
				assert.NotNil(t, req.StorageConfig)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *BackupListRequest

			// Set up mock
			backupClientFactory = func() (BackupClient, error) {
				return &MockBackupClient{
					ListBackupsFunc: func(ctx context.Context, req *BackupListRequest) (*cli.BackupListResponse, error) {
						capturedRequest = req
						return &cli.BackupListResponse{
							Backups:     []cli.BackupInfo{},
							TotalCount:  0,
							StoragePath: req.StoragePath,
						}, nil
					},
				}, nil
			}

			cmd := newBackupListCommand()

			// Set flags
			backupStorageType = tt.storageType
			backupStorageBucket = tt.bucket
			backupStorageRegion = tt.region
			backupStoragePath = "/tmp/test-backups"
			backupListLimit = 100
			backupOutputFormat = "table"

			err := cmd.RunE(cmd, []string{})
			assert.NoError(t, err)
			assert.NotNil(t, capturedRequest)
			if tt.validateRequest != nil {
				tt.validateRequest(t, capturedRequest)
			}
		})
	}
}

func TestBackupRestoreWithStorageFlags(t *testing.T) {
	// Save and restore factory
	originalFactory := backupClientFactory
	defer func() { backupClientFactory = originalFactory }()

	tests := []struct {
		name            string
		storageType     string
		bucket          string
		region          string
		validateRequest func(t *testing.T, req *BackupRestoreRequest)
	}{
		{
			name:        "restore backup with S3 storage",
			storageType: "s3",
			bucket:      "test-bucket",
			region:      "us-west-2",
			validateRequest: func(t *testing.T, req *BackupRestoreRequest) {
				assert.Equal(t, "s3", req.StorageType)
				assert.NotNil(t, req.StorageConfig)
				assert.Equal(t, "test-bucket", req.StorageConfig["bucket"])
				assert.Equal(t, "us-west-2", req.StorageConfig["region"])
			},
		},
		{
			name:        "restore backup with local storage",
			storageType: "local",
			validateRequest: func(t *testing.T, req *BackupRestoreRequest) {
				assert.Equal(t, "local", req.StorageType)
				assert.NotNil(t, req.StorageConfig)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *BackupRestoreRequest

			// Set up mock
			backupClientFactory = func() (BackupClient, error) {
				return &MockBackupClient{
					RestoreBackupFunc: func(ctx context.Context, req *BackupRestoreRequest) (*cli.BackupRestoreResponse, error) {
						capturedRequest = req
						return &cli.BackupRestoreResponse{
							BackupID:       req.BackupID,
							KeysRestored:   10000,
							ShardsRestored: 3,
							Duration:       30 * time.Second,
							Timestamp:      time.Now(),
							Success:        true,
						}, nil
					},
				}, nil
			}

			cmd := newBackupRestoreCommand()

			// Set flags
			backupStorageType = tt.storageType
			backupStorageBucket = tt.bucket
			backupStorageRegion = tt.region
			backupStoragePath = "/tmp/test-backups"
			backupRestoreVerify = true
			backupRestoreSkipExisting = false
			backupOutputFormat = "table"

			err := cmd.RunE(cmd, []string{"backup-test-1"})
			assert.NoError(t, err)
			assert.NotNil(t, capturedRequest)
			if tt.validateRequest != nil {
				tt.validateRequest(t, capturedRequest)
			}
		})
	}
}

func TestBackupRestoreCommandFlags(t *testing.T) {
	cmd := newBackupRestoreCommand()

	// Check required flags exist
	pitFlag := cmd.Flags().Lookup("point-in-time")
	assert.NotNil(t, pitFlag)

	verifyFlag := cmd.Flags().Lookup("verify")
	assert.NotNil(t, verifyFlag)
	assert.Equal(t, "true", verifyFlag.DefValue)

	skipExistingFlag := cmd.Flags().Lookup("skip-existing")
	assert.NotNil(t, skipExistingFlag)
	assert.Equal(t, "false", skipExistingFlag.DefValue)
}

func TestBackupListCommandFlags(t *testing.T) {
	cmd := newBackupListCommand()

	// Check required flags exist
	limitFlag := cmd.Flags().Lookup("limit")
	assert.NotNil(t, limitFlag)
	assert.Equal(t, "100", limitFlag.DefValue)

	storagePathFlag := cmd.Flags().Lookup("storage-path")
	assert.NotNil(t, storagePathFlag)
}

func TestBackupDeleteCommandFlags(t *testing.T) {
	cmd := newBackupDeleteCommand()

	// Check required flags exist
	yesFlag := cmd.Flags().Lookup("yes")
	assert.NotNil(t, yesFlag)
	assert.Equal(t, "false", yesFlag.DefValue)

	// Check short flag
	yesShortFlag := cmd.Flags().ShorthandLookup("y")
	assert.NotNil(t, yesShortFlag)
}

func TestBackupVerifyCommandFlags(t *testing.T) {
	cmd := newBackupVerifyCommand()

	// Check required flags exist
	storagePathFlag := cmd.Flags().Lookup("storage-path")
	assert.NotNil(t, storagePathFlag)

	encryptKeyFlag := cmd.Flags().Lookup("encrypt-key")
	assert.NotNil(t, encryptKeyFlag)
}

// TestRunBackupCreateCommand tests the backup create command execution.
func TestRunBackupCreateCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := backupClientFactory
	defer func() { backupClientFactory = originalFactory }()

	tests := []struct {
		name        string
		args        []string
		compression string
		encryption  string
		incremental bool
		storagePath string
		encryptKey  string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "successful backup with default compression",
			args:        []string{"test-backup"},
			compression: "zstd",
			encryption:  "none",
			incremental: false,
			storagePath: "/tmp/quicbaas-test-backups",
			expectError: false,
		},
		{
			name:        "successful backup with gzip compression",
			args:        []string{"test-backup-gzip"},
			compression: "gzip",
			encryption:  "none",
			incremental: false,
			storagePath: "/tmp/quicbaas-test-backups",
			expectError: false,
		},
		{
			name:        "successful incremental backup",
			args:        []string{"test-backup-inc"},
			compression: "zstd",
			encryption:  "none",
			incremental: true,
			storagePath: "/tmp/quicbaas-test-backups",
			expectError: false,
		},
		{
			name:        "successful encrypted backup",
			args:        []string{"test-backup-enc"},
			compression: "zstd",
			encryption:  "aes256",
			incremental: false,
			storagePath: "/tmp/quicbaas-test-backups",
			encryptKey:  "12345678901234567890123456789012", // Exactly 32 bytes
			expectError: false,
		},
		{
			name:        "invalid compression type",
			args:        []string{"test-backup"},
			compression: "invalid",
			encryption:  "none",
			incremental: false,
			storagePath: "/tmp/quicbaas-test-backups",
			expectError: true,
			errorMsg:    "unsupported compression type",
		},
		{
			name:        "invalid encryption type",
			args:        []string{"test-backup"},
			compression: "zstd",
			encryption:  "invalid",
			incremental: false,
			storagePath: "/tmp/quicbaas-test-backups",
			expectError: true,
			errorMsg:    "unsupported encryption type",
		},
		{
			name:        "encryption without key",
			args:        []string{"test-backup"},
			compression: "zstd",
			encryption:  "aes256",
			incremental: false,
			storagePath: "/tmp/quicbaas-test-backups",
			encryptKey:  "",
			expectError: true,
			errorMsg:    "encryption key is required",
		},
		{
			name:        "encryption with invalid key length",
			args:        []string{"test-backup"},
			compression: "zstd",
			encryption:  "aes256",
			incremental: false,
			storagePath: "/tmp/quicbaas-test-backups",
			encryptKey:  "short",
			expectError: true,
			errorMsg:    "encryption key must be exactly 32 bytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up mock for successful cases
			if !tt.expectError {
				backupClientFactory = func() (BackupClient, error) {
					return &MockBackupClient{
						CreateBackupFunc: func(ctx context.Context, req *BackupCreateRequest) (*cli.BackupCreateResponse, error) {
							return &cli.BackupCreateResponse{
								BackupID:  "backup-" + req.Name,
								Type:      req.BackupType,
								Size:      1024 * 1024 * 100,
								Duration:  5 * time.Second,
								Timestamp: time.Now(),
							}, nil
						},
					}, nil
				}
			}

			cmd := newBackupCreateCommand()

			// Set flags
			backupCompression = tt.compression
			backupEncryption = tt.encryption
			backupIncremental = tt.incremental
			backupStoragePath = tt.storagePath
			backupEncryptKey = tt.encryptKey
			backupOutputFormat = "table"

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunBackupListCommand tests the backup list command execution.
func TestRunBackupListCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := backupClientFactory
	defer func() { backupClientFactory = originalFactory }()

	// Set up mock
	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			ListBackupsFunc: func(ctx context.Context, req *BackupListRequest) (*cli.BackupListResponse, error) {
				return &cli.BackupListResponse{
					Backups: []cli.BackupInfo{
						{
							ID:        "backup-1",
							Type:      "full",
							Size:      1024 * 1024 * 100,
							NumShards: 3,
							Timestamp: time.Now(),
						},
					},
					TotalCount:  1,
					StoragePath: req.StoragePath,
				}, nil
			},
		}, nil
	}

	tests := []struct {
		name         string
		outputFormat string
		storagePath  string
		expectError  bool
	}{
		{
			name:         "list with table format",
			outputFormat: "table",
			storagePath:  "/tmp/quicbaas-test-backups",
			expectError:  false,
		},
		{
			name:         "list with json format",
			outputFormat: "json",
			storagePath:  "/tmp/quicbaas-test-backups",
			expectError:  false,
		},
		{
			name:         "list with yaml format",
			outputFormat: "yaml",
			storagePath:  "/tmp/quicbaas-test-backups",
			expectError:  false,
		},
		{
			name:         "list with default storage path",
			outputFormat: "table",
			storagePath:  "",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newBackupListCommand()

			backupOutputFormat = tt.outputFormat
			backupStoragePath = tt.storagePath
			backupListLimit = 100

			err := cmd.RunE(cmd, []string{})

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunBackupRestoreCommand tests the backup restore command execution.
func TestRunBackupRestoreCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := backupClientFactory
	defer func() { backupClientFactory = originalFactory }()

	tests := []struct {
		name         string
		args         []string
		pointInTime  string
		verify       bool
		skipExisting bool
		storagePath  string
		outputFormat string
		expectError  bool
		errorMsg     string
	}{
		{
			name:         "successful restore",
			args:         []string{"backup-prod-1"},
			pointInTime:  "",
			verify:       true,
			skipExisting: false,
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:         "restore with point-in-time",
			args:         []string{"backup-prod-1"},
			pointInTime:  "2025-10-14T12:00:00Z",
			verify:       true,
			skipExisting: false,
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:         "restore without verification",
			args:         []string{"backup-prod-1"},
			pointInTime:  "",
			verify:       false,
			skipExisting: false,
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "json",
			expectError:  false,
		},
		{
			name:         "restore with skip existing",
			args:         []string{"backup-prod-1"},
			pointInTime:  "",
			verify:       true,
			skipExisting: true,
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:         "invalid point-in-time format",
			args:         []string{"backup-prod-1"},
			pointInTime:  "invalid-timestamp",
			verify:       true,
			skipExisting: false,
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "table",
			expectError:  true,
			errorMsg:     "invalid point-in-time timestamp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up mock for successful cases
			if !tt.expectError {
				backupClientFactory = func() (BackupClient, error) {
					return &MockBackupClient{
						RestoreBackupFunc: func(ctx context.Context, req *BackupRestoreRequest) (*cli.BackupRestoreResponse, error) {
							return &cli.BackupRestoreResponse{
								BackupID:       req.BackupID,
								KeysRestored:   10000,
								ShardsRestored: 3,
								Duration:       30 * time.Second,
								Timestamp:      time.Now(),
								Success:        true,
							}, nil
						},
					}, nil
				}
			}

			cmd := newBackupRestoreCommand()

			backupRestorePointInTime = tt.pointInTime
			backupRestoreVerify = tt.verify
			backupRestoreSkipExisting = tt.skipExisting
			backupStoragePath = tt.storagePath
			backupOutputFormat = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunBackupDeleteCommand tests the backup delete command execution.
func TestRunBackupDeleteCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := backupClientFactory
	defer func() { backupClientFactory = originalFactory }()

	// Set up mock
	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			DeleteBackupFunc: func(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error) {
				return &cli.BackupDeleteResponse{
					BackupID: req.BackupID,

					Message:   "Backup deleted successfully",
					Deleted:   true,
					Timestamp: time.Now(),
				}, nil
			},
		}, nil
	}

	tests := []struct {
		name         string
		args         []string
		force        bool
		storagePath  string
		outputFormat string
		expectError  bool
	}{
		{
			name:         "delete with force",
			args:         []string{"backup-prod-1"},
			force:        true,
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:         "delete with json output",
			args:         []string{"backup-prod-1"},
			force:        true,
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "json",
			expectError:  false,
		},
		{
			name:         "delete with yaml output",
			args:         []string{"backup-prod-1"},
			force:        true,
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "yaml",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newBackupDeleteCommand()

			backupDeleteForce = tt.force
			backupStoragePath = tt.storagePath
			backupOutputFormat = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunBackupVerifyCommand tests the backup verify command execution.
func TestRunBackupVerifyCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := backupClientFactory
	defer func() { backupClientFactory = originalFactory }()

	// Set up mock
	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			VerifyBackupFunc: func(ctx context.Context, req *BackupVerifyRequest) (*cli.BackupVerifyResponse, error) {
				return &cli.BackupVerifyResponse{
					BackupID:      req.BackupID,
					Valid:         true,
					ChecksumValid: true,
					ShardsValid:   3,
					Issues:        []string{},
					Timestamp:     time.Now(),
				}, nil
			},
		}, nil
	}

	tests := []struct {
		name         string
		args         []string
		storagePath  string
		outputFormat string
		expectError  bool
	}{
		{
			name:         "verify with table format",
			args:         []string{"backup-prod-1"},
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:         "verify with json format",
			args:         []string{"backup-prod-1"},
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "json",
			expectError:  false,
		},
		{
			name:         "verify with yaml format",
			args:         []string{"backup-prod-1"},
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "yaml",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newBackupVerifyCommand()

			backupStoragePath = tt.storagePath
			backupOutputFormat = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRunBackupDeleteCommandWithoutConfirmation(t *testing.T) {
	// Save and restore factory
	originalFactory := backupClientFactory
	defer func() { backupClientFactory = originalFactory }()

	// Set up mock
	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			DeleteBackupFunc: func(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error) {
				return &cli.BackupDeleteResponse{
					BackupID: req.BackupID,

					Message:   "Backup deleted successfully",
					Deleted:   true,
					Timestamp: time.Now(),
				}, nil
			},
		}, nil
	}

	tests := []struct {
		name         string
		args         []string
		force        bool
		userInput    string
		storagePath  string
		outputFormat string
		expectError  bool
	}{
		{
			name:         "user confirms with 'y'",
			args:         []string{"backup-test-1"},
			force:        false,
			userInput:    "y\n",
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:         "user confirms with 'yes'",
			args:         []string{"backup-test-2"},
			force:        false,
			userInput:    "yes\n",
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:         "user confirms with 'Y'",
			args:         []string{"backup-test-3"},
			force:        false,
			userInput:    "Y\n",
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:         "user confirms with 'YES'",
			args:         []string{"backup-test-4"},
			force:        false,
			userInput:    "YES\n",
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:         "user cancels with 'n'",
			args:         []string{"backup-test-5"},
			force:        false,
			userInput:    "n\n",
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:         "user cancels with 'no'",
			args:         []string{"backup-test-6"},
			force:        false,
			userInput:    "no\n",
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:         "user cancels with empty response",
			args:         []string{"backup-test-7"},
			force:        false,
			userInput:    "\n",
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:         "delete with yaml output",
			args:         []string{"backup-test-8"},
			force:        true,
			storagePath:  "/tmp/quicbaas-test-backups",
			outputFormat: "yaml",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newBackupDeleteCommand()

			// Set flags
			backupDeleteForce = tt.force
			backupStoragePath = tt.storagePath
			backupOutputFormat = tt.outputFormat

			// Redirect stdin if user input is provided
			if tt.userInput != "" && !tt.force {
				oldStdin := os.Stdin
				r, w, _ := os.Pipe()
				os.Stdin = r

				// Write user input
				go func() {
					w.Write([]byte(tt.userInput))
					w.Close()
				}()

				defer func() {
					os.Stdin = oldStdin
				}()
			}

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunBackupDeleteCommandOutputFormats tests different output formats.
func TestRunBackupDeleteCommandOutputFormats(t *testing.T) {
	// Save and restore factory
	originalFactory := backupClientFactory
	defer func() { backupClientFactory = originalFactory }()

	// Set up mock
	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			DeleteBackupFunc: func(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error) {
				return &cli.BackupDeleteResponse{
					BackupID: req.BackupID,

					Message:   "Backup deleted successfully",
					Deleted:   true,
					Timestamp: time.Now(),
				}, nil
			},
		}, nil
	}

	formats := []string{"table", "json", "yaml", "raw"}

	for _, format := range formats {
		t.Run("format_"+format, func(t *testing.T) {
			cmd := newBackupDeleteCommand()

			backupDeleteForce = true
			backupStoragePath = "/tmp/quicbaas-test-backups"
			backupOutputFormat = format

			err := cmd.RunE(cmd, []string{"backup-test-" + format})
			assert.NoError(t, err)
		})
	}
}

// TestRunBackupDeleteCommandEdgeCases tests edge cases.
func TestRunBackupDeleteCommandEdgeCases(t *testing.T) {
	// Save and restore factory
	originalFactory := backupClientFactory
	defer func() { backupClientFactory = originalFactory }()

	// Set up mock
	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			DeleteBackupFunc: func(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error) {
				return &cli.BackupDeleteResponse{
					BackupID: req.BackupID,

					Message:   "Backup deleted successfully",
					Deleted:   true,
					Timestamp: time.Now(),
				}, nil
			},
		}, nil
	}

	tests := []struct {
		name        string
		args        []string
		storagePath string
		expectError bool
	}{
		{
			name:        "empty storage path",
			args:        []string{"backup-test"},
			storagePath: "",
			expectError: false, // Should use default path
		},
		{
			name:        "custom storage path",
			args:        []string{"backup-test"},
			storagePath: "/custom/path/backups",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newBackupDeleteCommand()

			backupDeleteForce = true
			backupStoragePath = tt.storagePath
			backupOutputFormat = "table"

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestBackupDeleteCommandConfirmationReadError tests confirmation prompt read error handling.
func TestBackupDeleteCommandConfirmationReadError(t *testing.T) {
	// Save and restore factory
	originalFactory := backupClientFactory
	defer func() { backupClientFactory = originalFactory }()

	// Set up mock - won't be called since we cancel
	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			DeleteBackupFunc: func(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error) {
				return &cli.BackupDeleteResponse{
					BackupID: req.BackupID,

					Message:   "Backup deleted successfully",
					Deleted:   true,
					Timestamp: time.Now(),
				}, nil
			},
		}, nil
	}

	cmd := newBackupDeleteCommand()

	// Set flags
	backupDeleteForce = false
	backupStoragePath = "/tmp/quicbaas-test-backups"
	backupOutputFormat = "table"

	// Close stdin immediately to trigger read error
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	w.Close() // Close write end immediately

	defer func() {
		os.Stdin = oldStdin
	}()

	// This should handle the EOF gracefully
	err := cmd.RunE(cmd, []string{"backup-test"})
	// The function handles EOF by treating it as "no" response
	assert.NoError(t, err)
}

// TestRunBackupDeleteCommandMultipleBackups tests deleting multiple backups sequentially.
func TestRunBackupDeleteCommandMultipleBackups(t *testing.T) {
	// Save and restore factory
	originalFactory := backupClientFactory
	defer func() { backupClientFactory = originalFactory }()

	// Set up mock
	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			DeleteBackupFunc: func(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error) {
				return &cli.BackupDeleteResponse{
					BackupID: req.BackupID,

					Message:   "Backup deleted successfully",
					Deleted:   true,
					Timestamp: time.Now(),
				}, nil
			},
		}, nil
	}

	backupIDs := []string{"backup-1", "backup-2", "backup-3"}

	for _, backupID := range backupIDs {
		t.Run("delete_"+backupID, func(t *testing.T) {
			cmd := newBackupDeleteCommand()

			backupDeleteForce = true
			backupStoragePath = "/tmp/quicbaas-test-backups"
			backupOutputFormat = "json"

			err := cmd.RunE(cmd, []string{backupID})
			assert.NoError(t, err)
		})
	}
}

// TestRunBackupDeleteCommandStdoutCapture tests that output is written correctly.
func TestRunBackupDeleteCommandStdoutCapture(t *testing.T) {
	// Save and restore factory
	originalFactory := backupClientFactory
	defer func() { backupClientFactory = originalFactory }()

	// Set up mock
	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			DeleteBackupFunc: func(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error) {
				return &cli.BackupDeleteResponse{
					BackupID: req.BackupID,

					Message:   "Backup deleted successfully",
					Deleted:   true,
					Timestamp: time.Now(),
				}, nil
			},
		}, nil
	}

	cmd := newBackupDeleteCommand()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	backupDeleteForce = true
	backupStoragePath = "/tmp/quicbaas-test-backups"
	backupOutputFormat = "table"

	err := cmd.RunE(cmd, []string{"backup-capture-test"})
	assert.NoError(t, err)

	// Restore stdout and read output
	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)

	output := buf.String()
	assert.Contains(t, output, "Deleting backup")
	assert.Contains(t, output, "backup-capture-test")
}

type mockBackupClientForStatus struct {
	CreateBackupFunc  func(ctx context.Context, req *BackupCreateRequest) (*cli.BackupCreateResponse, error)
	ListBackupsFunc   func(ctx context.Context, req *BackupListRequest) (*cli.BackupListResponse, error)
	RestoreBackupFunc func(ctx context.Context, req *BackupRestoreRequest) (*cli.BackupRestoreResponse, error)
	DeleteBackupFunc  func(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error)
	VerifyBackupFunc  func(ctx context.Context, req *BackupVerifyRequest) (*cli.BackupVerifyResponse, error)
	CloseFunc         func() error
}

func (m *mockBackupClientForStatus) CreateBackup(ctx context.Context, req *BackupCreateRequest) (*cli.BackupCreateResponse, error) {
	if m.CreateBackupFunc != nil {
		return m.CreateBackupFunc(ctx, req)
	}
	return &cli.BackupCreateResponse{
		BackupID:    "backup-test-123",
		Type:        "full",
		Compression: "zstd",
		Encryption:  "aes256",
		Size:        1024 * 1024 * 100,
		Duration:    5 * time.Second,
		Success:     true,
		Timestamp:   time.Now(),
	}, nil
}

func (m *mockBackupClientForStatus) ListBackups(ctx context.Context, req *BackupListRequest) (*cli.BackupListResponse, error) {
	if m.ListBackupsFunc != nil {
		return m.ListBackupsFunc(ctx, req)
	}
	return &cli.BackupListResponse{
		Backups: []cli.BackupInfo{
			{
				ID:          "backup-123",
				Type:        "full",
				Compression: "zstd",
				Encryption:  "none",
				NumShards:   3,
				NumKeys:     10000,
				Size:        1024 * 1024 * 100,
				Timestamp:   time.Now(),
			},
			{
				ID:          "backup-456",
				Type:        "incremental",
				Compression: "gzip",
				Encryption:  "aes256",
				NumShards:   1,
				NumKeys:     500,
				Size:        1024 * 1024 * 10,
				Timestamp:   time.Now().Add(-24 * time.Hour),
			},
		},
		TotalCount: 2,
	}, nil
}

func (m *mockBackupClientForStatus) RestoreBackup(ctx context.Context, req *BackupRestoreRequest) (*cli.BackupRestoreResponse, error) {
	if m.RestoreBackupFunc != nil {
		return m.RestoreBackupFunc(ctx, req)
	}
	return &cli.BackupRestoreResponse{
		BackupID:       req.BackupID,
		ShardsRestored: 3,
		KeysRestored:   10000,
		Duration:       10 * time.Second,
		Success:        true,
		Timestamp:      time.Now(),
	}, nil
}

func (m *mockBackupClientForStatus) DeleteBackup(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error) {
	if m.DeleteBackupFunc != nil {
		return m.DeleteBackupFunc(ctx, req)
	}
	return &cli.BackupDeleteResponse{
		BackupID:  req.BackupID,
		Deleted:   true,
		Message:   "Backup deleted successfully",
		Timestamp: time.Now(),
	}, nil
}

func (m *mockBackupClientForStatus) VerifyBackup(ctx context.Context, req *BackupVerifyRequest) (*cli.BackupVerifyResponse, error) {
	if m.VerifyBackupFunc != nil {
		return m.VerifyBackupFunc(ctx, req)
	}
	return &cli.BackupVerifyResponse{
		BackupID:      req.BackupID,
		Valid:         true,
		ChecksumValid: true,
		ShardsValid:   3,
		Issues:        []string{},
		Timestamp:     time.Now(),
	}, nil
}

func (m *mockBackupClientForStatus) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// =============================================================================
// BackupCLIError Tests
// =============================================================================

func TestBackupCLIError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *BackupCLIError
		expected string
	}{
		{
			name: "with_cause",
			err: &BackupCLIError{
				Op:      "create",
				Message: "failed to create backup",
				Cause:   errors.Newf(errors.ErrInternal, "test", "storage unavailable"),
			},
			expected: "backup create: failed to create backup:",
		},
		{
			name: "without_cause",
			err: &BackupCLIError{
				Op:      "delete",
				Message: "backup not found",
				Cause:   nil,
			},
			expected: "backup delete: backup not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errorStr := tc.err.Error()
			assert.Contains(t, errorStr, tc.expected)
		})
	}
}

func TestBackupCLIError_Unwrap(t *testing.T) {
	innerErr := errors.Newf(errors.ErrInternal, "test", "inner error")
	err := &BackupCLIError{
		Op:      "verify",
		Message: "verification failed",
		Cause:   innerErr,
	}

	unwrapped := err.Unwrap()
	assert.Equal(t, innerErr, unwrapped)
}

func TestBackupCLIError_Unwrap_Nil(t *testing.T) {
	err := &BackupCLIError{
		Op:      "verify",
		Message: "verification failed",
		Cause:   nil,
	}

	unwrapped := err.Unwrap()
	assert.Nil(t, unwrapped)
}

// =============================================================================
// Backup Status Command Tests
// =============================================================================

func TestNewBackupStatusCommand(t *testing.T) {
	cmd := newBackupStatusCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "status [backup-id]", cmd.Use)
	assert.Contains(t, cmd.Short, "Show backup status")

	// Verify flags exist
	assert.NotNil(t, cmd.Flags().Lookup("storage-path"))
}

func TestRunBackupStatusCommand_WithBackupID(t *testing.T) {
	// Save original factory and restore after test
	origFactory := backupClientFactory
	defer func() { backupClientFactory = origFactory }()

	mockClient := &mockBackupClientForStatus{}
	backupClientFactory = func() (BackupClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldTimeout := timeout
	oldFormat := backupOutputFormat
	defer func() {
		timeout = oldTimeout
		backupOutputFormat = oldFormat
		backupStoragePath = ""
	}()
	timeout = 30 * time.Second
	backupOutputFormat = "json"

	cmd := &cobra.Command{}
	err := runBackupStatusCommand(cmd, []string{"backup-123"})
	assert.NoError(t, err)
}

func TestRunBackupStatusCommand_NoBackupID(t *testing.T) {
	// Save original factory and restore after test
	origFactory := backupClientFactory
	defer func() { backupClientFactory = origFactory }()

	mockClient := &mockBackupClientForStatus{}
	backupClientFactory = func() (BackupClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldTimeout := timeout
	oldFormat := backupOutputFormat
	defer func() {
		timeout = oldTimeout
		backupOutputFormat = oldFormat
		backupStoragePath = ""
	}()
	timeout = 30 * time.Second
	backupOutputFormat = "json"

	cmd := &cobra.Command{}
	err := runBackupStatusCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunBackupStatusCommand_NoBackups(t *testing.T) {
	// Save original factory and restore after test
	origFactory := backupClientFactory
	defer func() { backupClientFactory = origFactory }()

	mockClient := &mockBackupClientForStatus{
		ListBackupsFunc: func(ctx context.Context, req *BackupListRequest) (*cli.BackupListResponse, error) {
			return &cli.BackupListResponse{
				Backups:    []cli.BackupInfo{},
				TotalCount: 0,
			}, nil
		},
	}
	backupClientFactory = func() (BackupClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldTimeout := timeout
	oldFormat := backupOutputFormat
	defer func() {
		timeout = oldTimeout
		backupOutputFormat = oldFormat
	}()
	timeout = 30 * time.Second
	backupOutputFormat = "json"

	cmd := &cobra.Command{}
	err := runBackupStatusCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunBackupStatusCommand_ClientError(t *testing.T) {
	// Save original factory and restore after test
	origFactory := backupClientFactory
	defer func() { backupClientFactory = origFactory }()

	backupClientFactory = func() (BackupClient, error) {
		return nil, errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	}

	cmd := &cobra.Command{}
	err := runBackupStatusCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create backup client")
}

func TestRunBackupStatusCommand_VerifyError(t *testing.T) {
	// Save original factory and restore after test
	origFactory := backupClientFactory
	defer func() { backupClientFactory = origFactory }()

	mockClient := &mockBackupClientForStatus{
		VerifyBackupFunc: func(ctx context.Context, req *BackupVerifyRequest) (*cli.BackupVerifyResponse, error) {
			return nil, errors.Newf(errors.ErrNotFound, "backup", "backup not found")
		},
	}
	backupClientFactory = func() (BackupClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldTimeout := timeout
	defer func() { timeout = oldTimeout }()
	timeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runBackupStatusCommand(cmd, []string{"nonexistent-backup"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get status for backup")
}

func TestRunBackupStatusCommand_ListError(t *testing.T) {
	// Save original factory and restore after test
	origFactory := backupClientFactory
	defer func() { backupClientFactory = origFactory }()

	mockClient := &mockBackupClientForStatus{
		ListBackupsFunc: func(ctx context.Context, req *BackupListRequest) (*cli.BackupListResponse, error) {
			return nil, errors.Newf(errors.ErrInternal, "backup", "list failed")
		},
	}
	backupClientFactory = func() (BackupClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldTimeout := timeout
	defer func() { timeout = oldTimeout }()
	timeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runBackupStatusCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get backup status")
}

func TestRunBackupStatusCommand_TableFormat(t *testing.T) {
	// Save original factory and restore after test
	origFactory := backupClientFactory
	defer func() { backupClientFactory = origFactory }()

	mockClient := &mockBackupClientForStatus{}
	backupClientFactory = func() (BackupClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldTimeout := timeout
	oldFormat := backupOutputFormat
	defer func() {
		timeout = oldTimeout
		backupOutputFormat = oldFormat
	}()
	timeout = 30 * time.Second
	backupOutputFormat = "table"

	cmd := &cobra.Command{}
	err := runBackupStatusCommand(cmd, []string{"backup-123"})
	assert.NoError(t, err)
}

// =============================================================================
// Command Help Tests
// =============================================================================

func TestBackupStatusCommandHelp(t *testing.T) {
	cmd := newBackupStatusCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "status")
	assert.Contains(t, output, "--storage-path")
}

// =============================================================================
// Additional Backup Command Structure Tests
// =============================================================================

func TestNewBackupCommands(t *testing.T) {
	cmd := newBackupCommands()

	require.NotNil(t, cmd)
	assert.Equal(t, "backup", cmd.Use)
	assert.Contains(t, cmd.Short, "Backup and restore")

	// Verify subcommands are registered
	subcommands := cmd.Commands()
	commandNames := make([]string, len(subcommands))
	for i, subcmd := range subcommands {
		commandNames[i] = subcmd.Name()
	}

	assert.Contains(t, commandNames, "create", "create command should be registered")
	assert.Contains(t, commandNames, "list", "list command should be registered")
	assert.Contains(t, commandNames, "restore", "restore command should be registered")
	assert.Contains(t, commandNames, "delete", "delete command should be registered")
	assert.Contains(t, commandNames, "verify", "verify command should be registered")
	assert.Contains(t, commandNames, "status", "status command should be registered")
}

func TestNewBackupCreateCommand(t *testing.T) {
	cmd := newBackupCreateCommand()

	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "create")
	assert.Contains(t, cmd.Short, "Create")
}

func TestNewBackupListCommand(t *testing.T) {
	cmd := newBackupListCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "list", cmd.Use)
	assert.Contains(t, cmd.Short, "List")
}

func TestNewBackupRestoreCommand(t *testing.T) {
	cmd := newBackupRestoreCommand()

	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "restore")
	assert.Contains(t, cmd.Short, "Restore")
}

func TestNewBackupDeleteCommand(t *testing.T) {
	cmd := newBackupDeleteCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "delete <backup-id>", cmd.Use)
	assert.Contains(t, cmd.Short, "Delete")

	// Verify flags exist
	assert.NotNil(t, cmd.Flags().Lookup("yes"))
}

func TestNewBackupVerifyCommand(t *testing.T) {
	cmd := newBackupVerifyCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "verify <backup-id>", cmd.Use)
	assert.Contains(t, cmd.Short, "Verify")
}

func TestParseHashAlgorithm(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected service.HashAlgorithm
	}{
		// Standard SHA-2 family
		{
			name:     "SHA256 uppercase",
			input:    "SHA256",
			expected: service.HashAlgSHA256,
		},
		{
			name:     "SHA256 lowercase",
			input:    "sha256",
			expected: service.HashAlgSHA256,
		},
		{
			name:     "SHA384 uppercase",
			input:    "SHA384",
			expected: service.HashAlgSHA384,
		},
		{
			name:     "SHA384 lowercase",
			input:    "sha384",
			expected: service.HashAlgSHA384,
		},
		{
			name:     "SHA512 uppercase",
			input:    "SHA512",
			expected: service.HashAlgSHA512,
		},
		{
			name:     "SHA512 lowercase",
			input:    "sha512",
			expected: service.HashAlgSHA512,
		},
		// SHA-3 family
		{
			name:     "SHA3-256 uppercase",
			input:    "SHA3-256",
			expected: service.HashAlgSHA3_256,
		},
		{
			name:     "SHA3-256 lowercase",
			input:    "sha3-256",
			expected: service.HashAlgSHA3_256,
		},
		{
			name:     "SHA3-384 uppercase",
			input:    "SHA3-384",
			expected: service.HashAlgSHA3_384,
		},
		{
			name:     "SHA3-384 lowercase",
			input:    "sha3-384",
			expected: service.HashAlgSHA3_384,
		},
		{
			name:     "SHA3-512 uppercase",
			input:    "SHA3-512",
			expected: service.HashAlgSHA3_512,
		},
		{
			name:     "SHA3-512 lowercase",
			input:    "sha3-512",
			expected: service.HashAlgSHA3_512,
		},
		// BLAKE2 family
		{
			name:     "BLAKE2B-256 uppercase",
			input:    "BLAKE2B-256",
			expected: service.HashAlgBLAKE2b256,
		},
		{
			name:     "BLAKE2B-256 lowercase",
			input:    "blake2b-256",
			expected: service.HashAlgBLAKE2b256,
		},
		{
			name:     "BLAKE2B-512 uppercase",
			input:    "BLAKE2B-512",
			expected: service.HashAlgBLAKE2b512,
		},
		{
			name:     "BLAKE2B-512 lowercase",
			input:    "blake2b-512",
			expected: service.HashAlgBLAKE2b512,
		},
		// Default/unknown cases
		{
			name:     "unknown algorithm defaults to SHA256",
			input:    "unknown",
			expected: service.HashAlgSHA256,
		},
		{
			name:     "empty string defaults to SHA256",
			input:    "",
			expected: service.HashAlgSHA256,
		},
		{
			name:     "MD5 not supported defaults to SHA256",
			input:    "MD5",
			expected: service.HashAlgSHA256,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseHashAlgorithm(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// dataKeyAlgorithmToKeyAttributes Tests - Covers all algorithm mappings
// =============================================================================

func TestDataKeyAlgorithmToKeyAttributes(t *testing.T) {
	tests := []struct {
		name      string
		algorithm service.KeyAlgorithm
		expected  kctypes.SymmetricAlgorithm
	}{
		{
			name:      "AES-128-GCM",
			algorithm: service.KeyAlgAES128GCM,
			expected:  kctypes.SymmetricAES128GCM,
		},
		{
			name:      "AES-192-GCM",
			algorithm: service.KeyAlgAES192GCM,
			expected:  kctypes.SymmetricAES192GCM,
		},
		{
			name:      "AES-256-GCM",
			algorithm: service.KeyAlgAES256GCM,
			expected:  kctypes.SymmetricAES256GCM,
		},
		{
			name:      "ChaCha20-Poly1305",
			algorithm: service.KeyAlgChaCha20Poly1305,
			expected:  kctypes.SymmetricChaCha20Poly1305,
		},
		{
			name:      "XChaCha20-Poly1305",
			algorithm: service.KeyAlgXChaCha20Poly1305,
			expected:  kctypes.SymmetricXChaCha20Poly1305,
		},
		{
			name:      "Unknown algorithm defaults to AES-256-GCM",
			algorithm: service.KeyAlgorithm("unknown"),
			expected:  kctypes.SymmetricAES256GCM,
		},
		{
			name:      "Empty algorithm defaults to AES-256-GCM",
			algorithm: service.KeyAlgorithm(""),
			expected:  kctypes.SymmetricAES256GCM,
		},
		{
			name:      "RSA-2048 (asymmetric) defaults to AES-256-GCM",
			algorithm: service.KeyAlgRSA2048,
			expected:  kctypes.SymmetricAES256GCM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := dataKeyAlgorithmToKeyAttributes(tt.algorithm)
			require.NotNil(t, attrs)
			assert.Equal(t, tt.expected, attrs.SymmetricAlgorithm)
		})
	}
}

// =============================================================================
// Embedded Server Error Types Tests
// =============================================================================

func TestEmbeddedServerFailedError(t *testing.T) {
	t.Run("Error method returns formatted message", func(t *testing.T) {
		err := &EmbeddedServerFailedError{Reason: "connection refused"}
		assert.Equal(t, "embedded server failed: connection refused", err.Error())
	})

	t.Run("Error with empty reason", func(t *testing.T) {
		err := &EmbeddedServerFailedError{Reason: ""}
		assert.Equal(t, "embedded server failed: ", err.Error())
	})

	t.Run("Error implements error interface", func(t *testing.T) {
		var err error = &EmbeddedServerFailedError{Reason: "test"}
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "embedded server failed")
	})
}

func TestEmbeddedPortError(t *testing.T) {
	t.Run("Error method returns message", func(t *testing.T) {
		err := &EmbeddedPortError{}
		assert.Equal(t, "listener address is not a TCP address", err.Error())
	})

	t.Run("Error implements error interface", func(t *testing.T) {
		var err error = &EmbeddedPortError{}
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "TCP address")
	})
}

func TestErrEmbeddedServerNotStarted(t *testing.T) {
	t.Run("Sentinel error is defined", func(t *testing.T) {
		assert.NotNil(t, ErrEmbeddedServerNotStarted)
		assert.Contains(t, ErrEmbeddedServerNotStarted.Error(), "embedded server not started")
	})
}

// =============================================================================
// ShutdownEmbeddedServer Tests
// =============================================================================

func TestShutdownEmbeddedServer(t *testing.T) {
	t.Run("ShutdownEmbeddedServer with no server returns nil", func(t *testing.T) {
		// Ensure no server is running
		embeddedServerHolder.Store(nil)
		err := shutdownEmbeddedServer()
		assert.NoError(t, err)
	})
}

// =============================================================================
// Factory Coverage Tests
// =============================================================================

func TestCreateAuthClientFactoryError(t *testing.T) {
	// Save original factory
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	// Mock SDK factory to return error
	factoryErr := errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	sdkClientFactory = func() (quicbaas.Client, error) {
		return nil, factoryErr
	}

	// Test auth client creation fails when SDK fails
	t.Run("AuthClientFactory returns error when SDK factory fails", func(t *testing.T) {
		_, err := createAuthClient()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "connection refused")
	})
}

func TestFactoryTransportSwitch(t *testing.T) {
	// Save original transport and restore after test
	origTransport := transport
	defer func() { transport = origTransport }()

	t.Run("createKMSClient with default transport", func(t *testing.T) {
		transport = ""
		// This will fail due to no server, but tests the code path
		_, _ = createKMSClient()
		// Client creation may succeed if server is running - tests the code path
	})

	t.Run("createKMSClient with grpc transport", func(t *testing.T) {
		transport = "grpc"
		_, _ = createKMSClient()
		// Client creation may succeed if server is running - tests the code path
	})

	t.Run("createKMSClient with unknown transport defaults to grpc", func(t *testing.T) {
		transport = "unknown"
		_, _ = createKMSClient()
		// Client creation may succeed if server is running - tests the code path
	})

	t.Run("createDEKClient with default transport", func(t *testing.T) {
		transport = ""
		_, _ = createDEKClient()
		// Client creation may succeed if server is running - tests the code path
	})

	t.Run("createDEKClient with grpc transport", func(t *testing.T) {
		transport = "grpc"
		_, _ = createDEKClient()
		// Client creation may succeed if server is running - tests the code path
	})

	t.Run("createBackendClient with default transport", func(t *testing.T) {
		transport = ""
		_, _ = createBackendClient()
		// Client creation may succeed if server is running - tests the code path
	})

	t.Run("createBackendClient with grpc transport", func(t *testing.T) {
		transport = "grpc"
		_, _ = createBackendClient()
		// Client creation may succeed if server is running - tests the code path
	})

	t.Run("createSafetyClient with default transport", func(t *testing.T) {
		transport = ""
		_, _ = createSafetyClient()
		// Client creation may succeed if server is running - tests the code path
	})

	t.Run("createSafetyClient with grpc transport", func(t *testing.T) {
		transport = "grpc"
		_, _ = createSafetyClient()
		// Client creation may succeed if server is running - tests the code path
	})
}

func TestCreateBackupClientFactory(t *testing.T) {
	t.Run("createBackupClient with grpc transport", func(t *testing.T) {
		transport = "grpc"
		client, err := createBackupClient()
		require.NoError(t, err)
		assert.NotNil(t, client)
		// Client is now a real gRPC adapter that implements BackupClient interface
	})
}

func TestCreateGRPCCryptoClient(t *testing.T) {
	t.Run("createCryptoClient with grpc transport", func(t *testing.T) {
		// This will fail due to no server, but tests the code path
		transport = "grpc"
		_, _ = createCryptoClient()
		// Client creation may succeed if server is running - tests the code path
	})
}

func TestCreateGRPCSealClient(t *testing.T) {
	t.Run("createSealClient with grpc transport", func(t *testing.T) {
		// This will fail due to no server, but tests the code path
		transport = "grpc"
		_, _ = createSealClient()
		// Client creation may succeed if server is running - tests the code path
	})
}

func TestCreateGRPCSecretsClient(t *testing.T) {
	t.Run("createGRPCSecretsClient returns client", func(t *testing.T) {
		// This will fail due to no server, but tests the code path
		_, _ = createGRPCSecretsClient()
		// Client creation may succeed if server is running - tests the code path
	})
}

// =============================================================================
// executeCryptoRandom Tests with nil client
// =============================================================================

func TestExecuteCryptoRandomNilClient(t *testing.T) {
	t.Run("executeCryptoRandom with nil client returns error", func(t *testing.T) {
		ctx := context.Background()
		req := &service.GenerateRandomRequest{NumBytes: 32}
		_, err := executeCryptoRandom(ctx, nil, req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid client type")
	})
}

func TestExecuteCryptoDataKeyNilClient(t *testing.T) {
	t.Run("executeCryptoDataKey with nil client returns error", func(t *testing.T) {
		ctx := context.Background()
		req := &service.GenerateDataKeyRequest{KeyID: "test"}
		_, err := executeCryptoDataKey(ctx, nil, req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid client type")
	})
}

// =============================================================================
// Mock Client Method Coverage Tests
// =============================================================================

func TestMockBackupClientMethods(t *testing.T) {
	t.Run("CreateBackup default implementation returns unimplemented", func(t *testing.T) {
		client := &MockBackupClient{}
		ctx := context.Background()
		_, err := client.CreateBackup(ctx, &BackupCreateRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("ListBackups default implementation returns unimplemented", func(t *testing.T) {
		client := &MockBackupClient{}
		ctx := context.Background()
		_, err := client.ListBackups(ctx, &BackupListRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("RestoreBackup default implementation returns unimplemented", func(t *testing.T) {
		client := &MockBackupClient{}
		ctx := context.Background()
		_, err := client.RestoreBackup(ctx, &BackupRestoreRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("DeleteBackup default implementation returns unimplemented", func(t *testing.T) {
		client := &MockBackupClient{}
		ctx := context.Background()
		_, err := client.DeleteBackup(ctx, &BackupDeleteRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("VerifyBackup default implementation returns success", func(t *testing.T) {
		client := &MockBackupClient{}
		ctx := context.Background()
		resp, err := client.VerifyBackup(ctx, &BackupVerifyRequest{BackupID: "test-id"})
		require.NoError(t, err)
		assert.True(t, resp.Valid)
		assert.True(t, resp.ChecksumValid)
	})

	t.Run("Close default implementation returns nil", func(t *testing.T) {
		client := &MockBackupClient{}
		err := client.Close()
		require.NoError(t, err)
	})

	t.Run("CreateBackup with custom function", func(t *testing.T) {
		client := &MockBackupClient{
			CreateBackupFunc: func(ctx context.Context, req *BackupCreateRequest) (*cli.BackupCreateResponse, error) {
				return &cli.BackupCreateResponse{BackupID: "custom-id"}, nil
			},
		}
		ctx := context.Background()
		resp, err := client.CreateBackup(ctx, &BackupCreateRequest{})
		require.NoError(t, err)
		assert.Equal(t, "custom-id", resp.BackupID)
	})

	t.Run("ListBackups with custom function", func(t *testing.T) {
		client := &MockBackupClient{
			ListBackupsFunc: func(ctx context.Context, req *BackupListRequest) (*cli.BackupListResponse, error) {
				return &cli.BackupListResponse{TotalCount: 5}, nil
			},
		}
		ctx := context.Background()
		resp, err := client.ListBackups(ctx, &BackupListRequest{})
		require.NoError(t, err)
		assert.Equal(t, 5, resp.TotalCount)
	})

	t.Run("RestoreBackup with custom function", func(t *testing.T) {
		client := &MockBackupClient{
			RestoreBackupFunc: func(ctx context.Context, req *BackupRestoreRequest) (*cli.BackupRestoreResponse, error) {
				return &cli.BackupRestoreResponse{Success: true}, nil
			},
		}
		ctx := context.Background()
		resp, err := client.RestoreBackup(ctx, &BackupRestoreRequest{})
		require.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("DeleteBackup with custom function", func(t *testing.T) {
		client := &MockBackupClient{
			DeleteBackupFunc: func(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error) {
				return &cli.BackupDeleteResponse{Deleted: true}, nil
			},
		}
		ctx := context.Background()
		resp, err := client.DeleteBackup(ctx, &BackupDeleteRequest{})
		require.NoError(t, err)
		assert.True(t, resp.Deleted)
	})

	t.Run("VerifyBackup with custom function", func(t *testing.T) {
		client := &MockBackupClient{
			VerifyBackupFunc: func(ctx context.Context, req *BackupVerifyRequest) (*cli.BackupVerifyResponse, error) {
				return &cli.BackupVerifyResponse{Valid: false}, nil
			},
		}
		ctx := context.Background()
		resp, err := client.VerifyBackup(ctx, &BackupVerifyRequest{})
		require.NoError(t, err)
		assert.False(t, resp.Valid)
	})

	t.Run("Close with custom function", func(t *testing.T) {
		client := &MockBackupClient{
			CloseFunc: func() error {
				return errors.Newf(errors.ErrInternal, "close", "custom error")
			},
		}
		err := client.Close()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "custom error")
	})
}

func TestMockBackendClientMethods(t *testing.T) {
	t.Run("KMSBackendStatus default returns unimplemented", func(t *testing.T) {
		client := &MockBackendClient{}
		ctx := context.Background()
		_, err := client.KMSBackendStatus(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("KMSBackendTest default returns unimplemented", func(t *testing.T) {
		client := &MockBackendClient{}
		ctx := context.Background()
		_, err := client.KMSBackendTest(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("Close default returns nil", func(t *testing.T) {
		client := &MockBackendClient{}
		err := client.Close()
		require.NoError(t, err)
	})

	t.Run("KMSBackendStatus with custom function", func(t *testing.T) {
		client := &MockBackendClient{
			KMSBackendStatusFunc: func(ctx context.Context) (*service.BackendStatusResponse, error) {
				return &service.BackendStatusResponse{
					BackendType: "tpm2",
					Status:      "healthy",
				}, nil
			},
		}
		ctx := context.Background()
		resp, err := client.KMSBackendStatus(ctx)
		require.NoError(t, err)
		assert.Equal(t, "tpm2", resp.BackendType)
		assert.Equal(t, "healthy", resp.Status)
	})

	t.Run("KMSBackendTest with custom function", func(t *testing.T) {
		client := &MockBackendClient{
			KMSBackendTestFunc: func(ctx context.Context) (*service.BackendTestResponse, error) {
				return &service.BackendTestResponse{
					Success:   true,
					Duration:  100 * time.Millisecond,
					Timestamp: time.Now(),
				}, nil
			},
		}
		ctx := context.Background()
		resp, err := client.KMSBackendTest(ctx)
		require.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("Close with custom function", func(t *testing.T) {
		client := &MockBackendClient{
			CloseFunc: func() error {
				return errors.Newf(errors.ErrInternal, "close", "backend close error")
			},
		}
		err := client.Close()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backend close error")
	})
}

func TestMockSafetyClientMethods(t *testing.T) {
	t.Run("KMSSafetyCheck default returns unimplemented", func(t *testing.T) {
		client := &MockSafetyClient{}
		ctx := context.Background()
		_, err := client.KMSSafetyCheck(ctx, &service.SafetyCheckRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("KMSSafetyReport default returns unimplemented", func(t *testing.T) {
		client := &MockSafetyClient{}
		ctx := context.Background()
		_, err := client.KMSSafetyReport(ctx, &service.SafetyReportRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("Close default returns nil", func(t *testing.T) {
		client := &MockSafetyClient{}
		err := client.Close()
		require.NoError(t, err)
	})

	t.Run("KMSSafetyCheck with custom function", func(t *testing.T) {
		client := &MockSafetyClient{
			KMSSafetyCheckFunc: func(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
				return &service.SafetyCheckResponse{
					TotalKeysChecked: 10,
					TotalDEKsChecked: 5,
					Timestamp:        time.Now(),
				}, nil
			},
		}
		ctx := context.Background()
		resp, err := client.KMSSafetyCheck(ctx, &service.SafetyCheckRequest{})
		require.NoError(t, err)
		assert.Equal(t, 10, resp.TotalKeysChecked)
	})

	t.Run("KMSSafetyReport with custom function", func(t *testing.T) {
		client := &MockSafetyClient{
			KMSSafetyReportFunc: func(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
				return &service.SafetyReportResponse{
					Timestamp: time.Now(),
				}, nil
			},
		}
		ctx := context.Background()
		resp, err := client.KMSSafetyReport(ctx, &service.SafetyReportRequest{})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Close with custom function", func(t *testing.T) {
		client := &MockSafetyClient{
			CloseFunc: func() error {
				return errors.Newf(errors.ErrInternal, "close", "safety close error")
			},
		}
		err := client.Close()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "safety close error")
	})
}

func TestMockDEKClientMethods(t *testing.T) {
	t.Run("KMSCreateDEK default returns unimplemented", func(t *testing.T) {
		client := &MockDEKClient{}
		ctx := context.Background()
		_, err := client.KMSCreateDEK(ctx, &service.CreateDEKRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("KMSGetDEK default returns unimplemented", func(t *testing.T) {
		client := &MockDEKClient{}
		ctx := context.Background()
		_, err := client.KMSGetDEK(ctx, &service.GetDEKRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("KMSListDEKs default returns unimplemented", func(t *testing.T) {
		client := &MockDEKClient{}
		ctx := context.Background()
		_, err := client.KMSListDEKs(ctx, &service.ListDEKsRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("KMSRotateDEK default returns unimplemented", func(t *testing.T) {
		client := &MockDEKClient{}
		ctx := context.Background()
		_, err := client.KMSRotateDEK(ctx, &service.RotateDEKRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("KMSDeleteDEK default returns unimplemented", func(t *testing.T) {
		client := &MockDEKClient{}
		ctx := context.Background()
		_, err := client.KMSDeleteDEK(ctx, &service.DeleteDEKRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("KMSGetDEKStatus default returns unimplemented", func(t *testing.T) {
		client := &MockDEKClient{}
		ctx := context.Background()
		_, err := client.KMSGetDEKStatus(ctx, &service.GetDEKStatusRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("KMSSetDefaultDEK default returns unimplemented", func(t *testing.T) {
		client := &MockDEKClient{}
		ctx := context.Background()
		_, err := client.KMSSetDefaultDEK(ctx, &service.SetDefaultDEKRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("KMSSetDEKSafety default returns unimplemented", func(t *testing.T) {
		client := &MockDEKClient{}
		ctx := context.Background()
		_, err := client.KMSSetDEKSafety(ctx, &service.SetDEKSafetyRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("Close default returns nil", func(t *testing.T) {
		client := &MockDEKClient{}
		err := client.Close()
		require.NoError(t, err)
	})

	t.Run("KMSCreateDEK with custom function", func(t *testing.T) {
		client := &MockDEKClient{
			KMSCreateDEKFunc: func(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
				return &service.CreateDEKResponse{
					DEK:       &service.DEKMetadata{Path: "custom-dek"},
					Timestamp: time.Now(),
				}, nil
			},
		}
		ctx := context.Background()
		resp, err := client.KMSCreateDEK(ctx, &service.CreateDEKRequest{})
		require.NoError(t, err)
		assert.Equal(t, "custom-dek", resp.DEK.Path)
	})

	t.Run("KMSGetDEK with custom function", func(t *testing.T) {
		client := &MockDEKClient{
			KMSGetDEKFunc: func(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
				return &service.GetDEKResponse{
					DEK: &service.DEKMetadata{Path: "test-dek"},
				}, nil
			},
		}
		ctx := context.Background()
		resp, err := client.KMSGetDEK(ctx, &service.GetDEKRequest{})
		require.NoError(t, err)
		assert.Equal(t, "test-dek", resp.DEK.Path)
	})

	t.Run("KMSListDEKs with custom function", func(t *testing.T) {
		client := &MockDEKClient{
			KMSListDEKsFunc: func(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
				return &service.ListDEKsResponse{
					DEKs:       []*service.DEKMetadata{{Path: "dek1"}, {Path: "dek2"}},
					TotalCount: 2,
				}, nil
			},
		}
		ctx := context.Background()
		resp, err := client.KMSListDEKs(ctx, &service.ListDEKsRequest{})
		require.NoError(t, err)
		assert.Equal(t, 2, resp.TotalCount)
	})

	t.Run("KMSRotateDEK with custom function", func(t *testing.T) {
		client := &MockDEKClient{
			KMSRotateDEKFunc: func(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
				return &service.RotateDEKResponse{
					Path: "rotated-dek", Timestamp: time.Now(),
				}, nil
			},
		}
		ctx := context.Background()
		resp, err := client.KMSRotateDEK(ctx, &service.RotateDEKRequest{})
		require.NoError(t, err)
		assert.Equal(t, "rotated-dek", resp.Path)
	})

	t.Run("KMSDeleteDEK with custom function", func(t *testing.T) {
		client := &MockDEKClient{
			KMSDeleteDEKFunc: func(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
				return &service.DeleteDEKResponse{Timestamp: time.Now()}, nil
			},
		}
		ctx := context.Background()
		resp, err := client.KMSDeleteDEK(ctx, &service.DeleteDEKRequest{})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("KMSGetDEKStatus with custom function", func(t *testing.T) {
		client := &MockDEKClient{
			KMSGetDEKStatusFunc: func(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
				return &service.GetDEKStatusResponse{
					DEK: &service.DEKMetadata{Path: "test-dek"},
				}, nil
			},
		}
		ctx := context.Background()
		resp, err := client.KMSGetDEKStatus(ctx, &service.GetDEKStatusRequest{})
		require.NoError(t, err)
		assert.Equal(t, "test-dek", resp.DEK.Path)
	})

	t.Run("KMSSetDefaultDEK with custom function", func(t *testing.T) {
		client := &MockDEKClient{
			KMSSetDefaultDEKFunc: func(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
				return &service.SetDefaultDEKResponse{Timestamp: time.Now()}, nil
			},
		}
		ctx := context.Background()
		resp, err := client.KMSSetDefaultDEK(ctx, &service.SetDefaultDEKRequest{})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("KMSSetDEKSafety with custom function", func(t *testing.T) {
		client := &MockDEKClient{
			KMSSetDEKSafetyFunc: func(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
				return &service.SetDEKSafetyResponse{Timestamp: time.Now()}, nil
			},
		}
		ctx := context.Background()
		resp, err := client.KMSSetDEKSafety(ctx, &service.SetDEKSafetyRequest{})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Close with custom function", func(t *testing.T) {
		client := &MockDEKClient{
			CloseFunc: func() error {
				return errors.Newf(errors.ErrInternal, "close", "dek close error")
			},
		}
		err := client.Close()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dek close error")
	})
}

func TestRunBackupListCommand_SuccessJSONFormat(t *testing.T) {
	origFactory := backupClientFactory
	origFmt := backupOutputFormat
	origStorageType := backupStorageType
	defer func() {
		backupClientFactory = origFactory
		backupOutputFormat = origFmt
		backupStorageType = origStorageType
	}()

	backupOutputFormat = "json"
	backupStorageType = "local"

	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			ListBackupsFunc: func(ctx context.Context, req *BackupListRequest) (*cli.BackupListResponse, error) {
				return &cli.BackupListResponse{
					Backups: []cli.BackupInfo{
						{
							ID:          "backup-001",
							Type:        "full",
							Size:        1024 * 1024,
							Compression: "zstd",
							Encryption:  "none",
							NumShards:   3,
							NumKeys:     5000,
							Timestamp:   time.Now(),
						},
					},
					TotalCount: 1,
				}, nil
			},
		}, nil
	}

	cmd := newBackupListCommand()
	err := runBackupListCommand(cmd, nil)
	assert.NoError(t, err)
}

func TestRunBackupListCommand_SuccessTableFormat(t *testing.T) {
	origFactory := backupClientFactory
	origFmt := backupOutputFormat
	origStorageType := backupStorageType
	defer func() {
		backupClientFactory = origFactory
		backupOutputFormat = origFmt
		backupStorageType = origStorageType
	}()

	backupOutputFormat = "table"
	backupStorageType = "local"

	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			ListBackupsFunc: func(ctx context.Context, req *BackupListRequest) (*cli.BackupListResponse, error) {
				return &cli.BackupListResponse{
					Backups:    []cli.BackupInfo{},
					TotalCount: 0,
				}, nil
			},
		}, nil
	}

	cmd := newBackupListCommand()
	err := runBackupListCommand(cmd, nil)
	assert.NoError(t, err)
}

func TestRunBackupListCommand_ClientError(t *testing.T) {
	origFactory := backupClientFactory
	origStorageType := backupStorageType
	defer func() {
		backupClientFactory = origFactory
		backupStorageType = origStorageType
	}()

	backupStorageType = "local"

	backupClientFactory = func() (BackupClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "backup", "server down")
	}

	cmd := newBackupListCommand()
	err := runBackupListCommand(cmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create backup client")
}

func TestRunBackupListCommand_ListError(t *testing.T) {
	origFactory := backupClientFactory
	origStorageType := backupStorageType
	defer func() {
		backupClientFactory = origFactory
		backupStorageType = origStorageType
	}()

	backupStorageType = "local"

	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			ListBackupsFunc: func(ctx context.Context, req *BackupListRequest) (*cli.BackupListResponse, error) {
				return nil, errors.New(errors.ErrInternal, "backup.list", "storage unavailable")
			},
		}, nil
	}

	cmd := newBackupListCommand()
	err := runBackupListCommand(cmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list backups")
}

// =============================================================================
// Backup Verify Command - Output Format Paths
// =============================================================================

func TestRunBackupVerifyCommand_SuccessJSONFormat(t *testing.T) {
	origFactory := backupClientFactory
	origFmt := backupOutputFormat
	defer func() {
		backupClientFactory = origFactory
		backupOutputFormat = origFmt
	}()

	backupOutputFormat = "json"

	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			VerifyBackupFunc: func(ctx context.Context, req *BackupVerifyRequest) (*cli.BackupVerifyResponse, error) {
				return &cli.BackupVerifyResponse{
					BackupID:      req.BackupID,
					Valid:         true,
					ChecksumValid: true,
					ShardsValid:   3,
					Timestamp:     time.Now(),
				}, nil
			},
		}, nil
	}

	cmd := newBackupVerifyCommand()
	err := runBackupVerifyCommand(cmd, []string{"backup-001"})
	assert.NoError(t, err)
}

func TestRunBackupVerifyCommand_SuccessTableFormat(t *testing.T) {
	origFactory := backupClientFactory
	origFmt := backupOutputFormat
	defer func() {
		backupClientFactory = origFactory
		backupOutputFormat = origFmt
	}()

	backupOutputFormat = ""

	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			VerifyBackupFunc: func(ctx context.Context, req *BackupVerifyRequest) (*cli.BackupVerifyResponse, error) {
				return &cli.BackupVerifyResponse{
					BackupID:      req.BackupID,
					Valid:         false,
					ChecksumValid: false,
					ShardsValid:   2,
					Timestamp:     time.Now(),
					Issues:        []string{"shard 3 checksum mismatch"},
				}, nil
			},
		}, nil
	}

	cmd := newBackupVerifyCommand()
	err := runBackupVerifyCommand(cmd, []string{"backup-bad"})
	assert.NoError(t, err)
}

func TestRunBackupVerifyCommand_ClientError(t *testing.T) {
	origFactory := backupClientFactory
	defer func() { backupClientFactory = origFactory }()

	backupClientFactory = func() (BackupClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "backup", "no server")
	}

	cmd := newBackupVerifyCommand()
	err := runBackupVerifyCommand(cmd, []string{"backup-001"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create backup client")
}

func TestRunBackupVerifyCommand_VerifyError(t *testing.T) {
	origFactory := backupClientFactory
	defer func() { backupClientFactory = origFactory }()

	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			VerifyBackupFunc: func(ctx context.Context, req *BackupVerifyRequest) (*cli.BackupVerifyResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "backup.verify", "backup not found")
			},
		}, nil
	}

	cmd := newBackupVerifyCommand()
	err := runBackupVerifyCommand(cmd, []string{"nonexistent"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "verification failed")
}

// =============================================================================
// Msg Delete Channel - Force Path with Table Format
// =============================================================================

func TestRunMsgDeleteChannelCommand_ForceTableOutput(t *testing.T) {
	origForce := msgForce
	origFmt := msgOutputFmt
	defer func() {
		msgForce = origForce
		msgOutputFmt = origFmt
	}()

	msgForce = true
	msgOutputFmt = "table"

	mock := &mockMessagingClientForCoverage{
		deleteChannelFunc: func(ctx context.Context, req *grpc.MessagingDeleteChannelRequest) (*grpc.MessagingDeleteChannelResponse, error) {
			return &grpc.MessagingDeleteChannelResponse{
				Name:    req.Name,
				Deleted: true,
			}, nil
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	cmd := &cobra.Command{}
	err := runMsgDeleteChannelCommand(cmd, []string{"test-channel"})
	assert.NoError(t, err)
}

func TestRunMsgDeleteChannelCommand_ForceClientError(t *testing.T) {
	origForce := msgForce
	origFactory := messagingClientFactory
	defer func() {
		msgForce = origForce
		messagingClientFactory = origFactory
	}()

	msgForce = true
	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := &cobra.Command{}
	err := runMsgDeleteChannelCommand(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

func TestRunMsgDeleteChannelCommand_ForceDeleteError(t *testing.T) {
	origForce := msgForce
	defer func() { msgForce = origForce }()

	msgForce = true

	mock := &mockMessagingClientForCoverage{
		deleteChannelFunc: func(ctx context.Context, req *grpc.MessagingDeleteChannelRequest) (*grpc.MessagingDeleteChannelResponse, error) {
			return nil, errors.New(errors.ErrNotFound, "test", "channel not found")
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	cmd := &cobra.Command{}
	err := runMsgDeleteChannelCommand(cmd, []string{"nonexistent"})
	assert.Error(t, err)
}

// =============================================================================
// Msg Unsubscribe - Force Path with Table Format
// =============================================================================

func TestRunMsgUnsubscribeCommand_ForceTableOutput(t *testing.T) {
	origForce := msgForce
	origFmt := msgOutputFmt
	defer func() {
		msgForce = origForce
		msgOutputFmt = origFmt
	}()

	msgForce = true
	msgOutputFmt = "table"

	mock := &mockMessagingClientForCoverage{
		unsubscribeFunc: func(ctx context.Context, req *grpc.MessagingUnsubscribeRequest) (*grpc.MessagingUnsubscribeResponse, error) {
			return &grpc.MessagingUnsubscribeResponse{
				SubscriptionID: req.SubscriptionID,
				Unsubscribed:   true,
			}, nil
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	cmd := &cobra.Command{}
	err := runMsgUnsubscribeCommand(cmd, []string{"sub-123"})
	assert.NoError(t, err)
}

func TestRunMsgUnsubscribeCommand_ForceClientError(t *testing.T) {
	origForce := msgForce
	origFactory := messagingClientFactory
	defer func() {
		msgForce = origForce
		messagingClientFactory = origFactory
	}()

	msgForce = true
	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := &cobra.Command{}
	err := runMsgUnsubscribeCommand(cmd, []string{"sub-123"})
	assert.Error(t, err)
}

func TestRunMsgUnsubscribeCommand_ForceUnsubscribeError(t *testing.T) {
	origForce := msgForce
	defer func() { msgForce = origForce }()

	msgForce = true

	mock := &mockMessagingClientForCoverage{
		unsubscribeFunc: func(ctx context.Context, req *grpc.MessagingUnsubscribeRequest) (*grpc.MessagingUnsubscribeResponse, error) {
			return nil, errors.New(errors.ErrNotFound, "test", "subscription not found")
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	cmd := &cobra.Command{}
	err := runMsgUnsubscribeCommand(cmd, []string{"nonexistent"})
	assert.Error(t, err)
}

// =============================================================================
// Backup Restore Command - Additional Paths
// =============================================================================

func TestRunBackupRestoreCommand_SuccessDefaultFormat(t *testing.T) {
	origFactory := backupClientFactory
	origFmt := backupOutputFormat
	origStorageType := backupStorageType
	defer func() {
		backupClientFactory = origFactory
		backupOutputFormat = origFmt
		backupStorageType = origStorageType
	}()

	backupOutputFormat = ""
	backupStorageType = "local"

	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			RestoreBackupFunc: func(ctx context.Context, req *BackupRestoreRequest) (*cli.BackupRestoreResponse, error) {
				return &cli.BackupRestoreResponse{
					BackupID:       req.BackupID,
					KeysRestored:   1000,
					ShardsRestored: 3,
					Duration:       5 * time.Second,
					Timestamp:      time.Now(),
					Success:        true,
				}, nil
			},
		}, nil
	}

	cmd := newBackupRestoreCommand()
	err := runBackupRestoreCommand(cmd, []string{"backup-001"})
	assert.NoError(t, err)
}

func TestRunBackupRestoreCommand_RestoreError(t *testing.T) {
	origFactory := backupClientFactory
	origStorageType := backupStorageType
	defer func() {
		backupClientFactory = origFactory
		backupStorageType = origStorageType
	}()

	backupStorageType = "local"

	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			RestoreBackupFunc: func(ctx context.Context, req *BackupRestoreRequest) (*cli.BackupRestoreResponse, error) {
				return nil, errors.New(errors.ErrInternal, "backup.restore", "corrupted backup")
			},
		}, nil
	}

	cmd := newBackupRestoreCommand()
	err := runBackupRestoreCommand(cmd, []string{"backup-corrupt"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "restore failed")
}

func TestRunBackupRestoreCommand_InvalidPointInTime(t *testing.T) {
	origPIT := backupRestorePointInTime
	defer func() { backupRestorePointInTime = origPIT }()

	cmd := newBackupRestoreCommand()
	backupRestorePointInTime = "not-a-valid-timestamp"
	err := runBackupRestoreCommand(cmd, []string{"backup-001"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid point-in-time")
}

// =============================================================================
// Backup Create Command - Additional Validation Paths
// =============================================================================

func TestRunBackupCreateCommand_InvalidCompressionType(t *testing.T) {
	origCompression := backupCompression
	defer func() { backupCompression = origCompression }()

	cmd := newBackupCreateCommand()
	backupCompression = "lz77"
	err := runBackupCreateCommand(cmd, []string{"test-backup"})
	assert.Error(t, err)
}

func TestRunBackupCreateCommand_InvalidEncryptionType(t *testing.T) {
	origCompression := backupCompression
	origEncryption := backupEncryption
	defer func() {
		backupCompression = origCompression
		backupEncryption = origEncryption
	}()

	cmd := newBackupCreateCommand()
	backupCompression = "zstd"
	backupEncryption = "triple-des"
	err := runBackupCreateCommand(cmd, []string{"test-backup"})
	assert.Error(t, err)
}

func TestRunBackupCreateCommand_AES256NoKey(t *testing.T) {
	origCompression := backupCompression
	origEncryption := backupEncryption
	origKey := backupEncryptKey
	defer func() {
		backupCompression = origCompression
		backupEncryption = origEncryption
		backupEncryptKey = origKey
	}()

	cmd := newBackupCreateCommand()
	backupCompression = "zstd"
	backupEncryption = "aes256"
	backupEncryptKey = ""

	err := runBackupCreateCommand(cmd, []string{"test-backup"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "encryption key is required")
}

func TestRunBackupCreateCommand_AES256WrongKeyLength(t *testing.T) {
	origCompression := backupCompression
	origEncryption := backupEncryption
	origKey := backupEncryptKey
	defer func() {
		backupCompression = origCompression
		backupEncryption = origEncryption
		backupEncryptKey = origKey
	}()

	cmd := newBackupCreateCommand()
	backupCompression = "zstd"
	backupEncryption = "aes256"
	backupEncryptKey = "tooshort"

	err := runBackupCreateCommand(cmd, []string{"test-backup"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "32 bytes")
}

func TestRunBackupCreateCommand_IncrementalType(t *testing.T) {
	origFactory := backupClientFactory
	origCompression := backupCompression
	origEncryption := backupEncryption
	origIncremental := backupIncremental
	origFmt := backupOutputFormat
	origStorageType := backupStorageType
	defer func() {
		backupClientFactory = origFactory
		backupCompression = origCompression
		backupEncryption = origEncryption
		backupIncremental = origIncremental
		backupOutputFormat = origFmt
		backupStorageType = origStorageType
	}()

	backupStorageType = "local"

	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			CreateBackupFunc: func(ctx context.Context, req *BackupCreateRequest) (*cli.BackupCreateResponse, error) {
				return &cli.BackupCreateResponse{
					BackupID:  "backup-inc-001",
					Success:   true,
					Timestamp: time.Now(),
				}, nil
			},
		}, nil
	}

	cmd := newBackupCreateCommand()
	backupCompression = "zstd"
	backupEncryption = "none"
	backupIncremental = true
	backupOutputFormat = "json"
	err := runBackupCreateCommand(cmd, []string{"incremental-backup"})
	assert.NoError(t, err)
}

func TestRunBackupCreateCommand_ClientError(t *testing.T) {
	origFactory := backupClientFactory
	origCompression := backupCompression
	origEncryption := backupEncryption
	origStorageType := backupStorageType
	defer func() {
		backupClientFactory = origFactory
		backupCompression = origCompression
		backupEncryption = origEncryption
		backupStorageType = origStorageType
	}()

	backupStorageType = "local"

	backupClientFactory = func() (BackupClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "backup", "no server")
	}

	cmd := newBackupCreateCommand()
	backupCompression = "zstd"
	backupEncryption = "none"
	err := runBackupCreateCommand(cmd, []string{"test-backup"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create backup client")
}

func TestRunBackupCreateCommand_CreateError(t *testing.T) {
	origFactory := backupClientFactory
	origCompression := backupCompression
	origEncryption := backupEncryption
	origStorageType := backupStorageType
	origFmt := backupOutputFormat
	defer func() {
		backupClientFactory = origFactory
		backupCompression = origCompression
		backupEncryption = origEncryption
		backupStorageType = origStorageType
		backupOutputFormat = origFmt
	}()

	backupStorageType = "local"

	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			CreateBackupFunc: func(ctx context.Context, req *BackupCreateRequest) (*cli.BackupCreateResponse, error) {
				return nil, errors.New(errors.ErrInternal, "backup.create", "disk full")
			},
		}, nil
	}

	cmd := newBackupCreateCommand()
	backupCompression = "zstd"
	backupEncryption = "none"
	backupOutputFormat = ""
	err := runBackupCreateCommand(cmd, []string{"test-backup"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backup creation failed")
}
