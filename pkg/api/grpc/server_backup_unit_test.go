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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func createBackupTestServer(t *testing.T) (*Server, *testutil.MockQuicBaaS) {
	t.Helper()

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "grpc-test",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()
	srv, err := NewServer(mockSvc, log, 1, "test-v1.0.0")
	require.NoError(t, err)
	return srv, mockSvc
}

// =============================================================================
// CreateBackup Tests
// =============================================================================

func TestCreateBackupHandlers(t *testing.T) {
	srv, mockSvc := createBackupTestServer(t)
	ctx := context.Background()

	t.Run("create full backup", func(t *testing.T) {
		mockSvc.On("CreateBackup", mock.Anything, mock.MatchedBy(func(req *service.CreateBackupRequest) bool {
			return req.Name == "full"
		})).Return(&service.CreateBackupResponse{
			BackupID:  "backup-123",
			Timestamp: time.Now().UnixNano(),
			Size:      1024 * 1024,
		}, nil).Once()

		resp, err := srv.CreateBackup(ctx, &pb.CreateBackupRequest{
			BackupType:  pb.BackupType_BACKUP_TYPE_FULL,
			Compression: pb.CompressionType_COMPRESSION_TYPE_GZIP,
			Encryption:  pb.EncryptionType_ENCRYPTION_TYPE_AES256,
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, "backup-123", resp.BackupId)
		mockSvc.AssertExpectations(t)
	})

	t.Run("create incremental backup", func(t *testing.T) {
		mockSvc.On("CreateBackup", mock.Anything, mock.MatchedBy(func(req *service.CreateBackupRequest) bool {
			return req.Name == "incremental"
		})).Return(&service.CreateBackupResponse{
			BackupID:  "backup-456",
			Timestamp: time.Now().UnixNano(),
			Size:      512 * 1024,
		}, nil).Once()

		resp, err := srv.CreateBackup(ctx, &pb.CreateBackupRequest{
			BackupType: pb.BackupType_BACKUP_TYPE_INCREMENTAL,
		})

		require.NoError(t, err)
		assert.Equal(t, "backup-456", resp.BackupId)
		mockSvc.AssertExpectations(t)
	})

	t.Run("create snapshot backup", func(t *testing.T) {
		mockSvc.On("CreateBackup", mock.Anything, mock.MatchedBy(func(req *service.CreateBackupRequest) bool {
			return req.Name == "snapshot"
		})).Return(&service.CreateBackupResponse{
			BackupID:  "backup-789",
			Timestamp: time.Now().UnixNano(),
			Size:      2 * 1024 * 1024,
		}, nil).Once()

		resp, err := srv.CreateBackup(ctx, &pb.CreateBackupRequest{
			BackupType: pb.BackupType_BACKUP_TYPE_SNAPSHOT,
		})

		require.NoError(t, err)
		assert.Equal(t, "backup-789", resp.BackupId)
		mockSvc.AssertExpectations(t)
	})

	t.Run("create backup service error", func(t *testing.T) {
		mockSvc.On("CreateBackup", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		_, err := srv.CreateBackup(ctx, &pb.CreateBackupRequest{
			BackupType: pb.BackupType_BACKUP_TYPE_FULL,
		})

		assert.Error(t, err)
		mockSvc.AssertExpectations(t)
	})
}

// =============================================================================
// ListBackups Tests
// =============================================================================

func TestListBackupsHandlers(t *testing.T) {
	srv, mockSvc := createBackupTestServer(t)
	ctx := context.Background()

	t.Run("list backups success", func(t *testing.T) {
		mockSvc.On("ListBackups", mock.Anything, mock.Anything).Return(&service.ListBackupsResponse{
			Backups: []*service.BackupMetadata{
				{
					BackupID:  "backup-1",
					Name:      "full",
					Timestamp: time.Now().UnixNano(),
					Size:      1024 * 1024,
					Shards:    []uint64{1, 2, 3},
				},
				{
					BackupID:  "backup-2",
					Name:      "incremental",
					Timestamp: time.Now().Add(-24 * time.Hour).UnixNano(),
					Size:      512 * 1024,
					Shards:    []uint64{1, 2},
				},
				{
					BackupID:  "backup-3",
					Name:      "snapshot",
					Timestamp: time.Now().Add(-48 * time.Hour).UnixNano(),
					Size:      2 * 1024 * 1024,
					Shards:    []uint64{1},
				},
			},
		}, nil).Once()

		resp, err := srv.ListBackups(ctx, &pb.ListBackupsRequest{
			Limit: 10,
		})

		require.NoError(t, err)
		assert.Len(t, resp.Backups, 3)
		assert.Equal(t, "backup-1", resp.Backups[0].BackupId)
		assert.Equal(t, pb.BackupType_BACKUP_TYPE_FULL, resp.Backups[0].BackupType)
		assert.Equal(t, pb.BackupType_BACKUP_TYPE_INCREMENTAL, resp.Backups[1].BackupType)
		assert.Equal(t, pb.BackupType_BACKUP_TYPE_SNAPSHOT, resp.Backups[2].BackupType)
		mockSvc.AssertExpectations(t)
	})

	t.Run("list backups empty result", func(t *testing.T) {
		mockSvc.On("ListBackups", mock.Anything, mock.Anything).Return(&service.ListBackupsResponse{
			Backups: []*service.BackupMetadata{},
		}, nil).Once()

		resp, err := srv.ListBackups(ctx, &pb.ListBackupsRequest{})

		require.NoError(t, err)
		assert.Empty(t, resp.Backups)
		mockSvc.AssertExpectations(t)
	})

	t.Run("list backups service error", func(t *testing.T) {
		mockSvc.On("ListBackups", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		_, err := srv.ListBackups(ctx, &pb.ListBackupsRequest{})

		assert.Error(t, err)
		mockSvc.AssertExpectations(t)
	})
}

// =============================================================================
// RestoreBackup Tests
// =============================================================================

func TestRestoreBackupHandlers(t *testing.T) {
	srv, mockSvc := createBackupTestServer(t)
	ctx := context.Background()

	t.Run("restore backup success", func(t *testing.T) {
		mockSvc.On("RestoreBackup", mock.Anything, mock.MatchedBy(func(req *service.RestoreBackupRequest) bool {
			return req.BackupID == "backup-123"
		})).Return(&service.RestoreBackupResponse{
			Status: "completed",
		}, nil).Once()

		resp, err := srv.RestoreBackup(ctx, &pb.RestoreBackupRequest{
			BackupId:       "backup-123",
			VerifyChecksum: true,
		})

		require.NoError(t, err)
		assert.Equal(t, "backup-123", resp.BackupId)
		mockSvc.AssertExpectations(t)
	})

	t.Run("restore backup with target shards", func(t *testing.T) {
		mockSvc.On("RestoreBackup", mock.Anything, mock.MatchedBy(func(req *service.RestoreBackupRequest) bool {
			return len(req.TargetShards) == 2
		})).Return(&service.RestoreBackupResponse{
			Status: "completed",
		}, nil).Once()

		resp, err := srv.RestoreBackup(ctx, &pb.RestoreBackupRequest{
			BackupId:     "backup-456",
			TargetShards: []uint64{1, 2},
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		mockSvc.AssertExpectations(t)
	})

	t.Run("restore backup in progress", func(t *testing.T) {
		mockSvc.On("RestoreBackup", mock.Anything, mock.Anything).Return(&service.RestoreBackupResponse{
			Status: "in_progress",
		}, nil).Once()

		resp, err := srv.RestoreBackup(ctx, &pb.RestoreBackupRequest{
			BackupId: "backup-789",
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		mockSvc.AssertExpectations(t)
	})

	t.Run("restore backup empty backup id", func(t *testing.T) {
		_, err := srv.RestoreBackup(ctx, &pb.RestoreBackupRequest{
			BackupId: "",
		})

		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("restore backup service error", func(t *testing.T) {
		mockSvc.On("RestoreBackup", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		_, err := srv.RestoreBackup(ctx, &pb.RestoreBackupRequest{
			BackupId: "backup-error",
		})

		assert.Error(t, err)
		mockSvc.AssertExpectations(t)
	})
}

// =============================================================================
// DeleteBackup Tests
// =============================================================================

func TestDeleteBackupHandlers(t *testing.T) {
	srv, mockSvc := createBackupTestServer(t)
	ctx := context.Background()

	t.Run("delete backup success", func(t *testing.T) {
		mockSvc.On("DeleteBackup", mock.Anything, mock.MatchedBy(func(req *service.DeleteBackupRequest) bool {
			return req.BackupID == "backup-123"
		})).Return(&service.DeleteBackupResponse{
			Success: true,
		}, nil).Once()

		resp, err := srv.DeleteBackup(ctx, &pb.DeleteBackupRequest{
			BackupId: "backup-123",
		})

		require.NoError(t, err)
		assert.True(t, resp.Success)
		mockSvc.AssertExpectations(t)
	})

	t.Run("delete backup not found", func(t *testing.T) {
		mockSvc.On("DeleteBackup", mock.Anything, mock.Anything).Return(&service.DeleteBackupResponse{
			Success: false,
		}, nil).Once()

		resp, err := srv.DeleteBackup(ctx, &pb.DeleteBackupRequest{
			BackupId: "backup-nonexistent",
		})

		require.NoError(t, err)
		assert.False(t, resp.Success)
		mockSvc.AssertExpectations(t)
	})

	t.Run("delete backup empty backup id", func(t *testing.T) {
		_, err := srv.DeleteBackup(ctx, &pb.DeleteBackupRequest{
			BackupId: "",
		})

		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("delete backup service error", func(t *testing.T) {
		mockSvc.On("DeleteBackup", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		_, err := srv.DeleteBackup(ctx, &pb.DeleteBackupRequest{
			BackupId: "backup-error",
		})

		assert.Error(t, err)
		mockSvc.AssertExpectations(t)
	})
}

// =============================================================================
// VerifyBackup Tests
// =============================================================================

func TestVerifyBackupHandlers(t *testing.T) {
	srv, mockSvc := createBackupTestServer(t)
	ctx := context.Background()

	t.Run("verify backup valid", func(t *testing.T) {
		mockSvc.On("VerifyBackup", mock.Anything, mock.MatchedBy(func(req *service.VerifyBackupRequest) bool {
			return req.BackupID == "backup-123"
		})).Return(&service.VerifyBackupResponse{
			Valid:         true,
			ChecksumMatch: true,
		}, nil).Once()

		resp, err := srv.VerifyBackup(ctx, &pb.VerifyBackupRequest{
			BackupId: "backup-123",
		})

		require.NoError(t, err)
		assert.True(t, resp.Valid)
		assert.True(t, resp.ChecksumValid)
		assert.Equal(t, "Backup verification passed", resp.Message)
		mockSvc.AssertExpectations(t)
	})

	t.Run("verify backup invalid with issues", func(t *testing.T) {
		mockSvc.On("VerifyBackup", mock.Anything, mock.Anything).Return(&service.VerifyBackupResponse{
			Valid:         false,
			ChecksumMatch: false,
			Issues:        []string{"corrupted block at offset 1024", "missing shard data"},
		}, nil).Once()

		resp, err := srv.VerifyBackup(ctx, &pb.VerifyBackupRequest{
			BackupId: "backup-corrupted",
		})

		require.NoError(t, err)
		assert.False(t, resp.Valid)
		assert.False(t, resp.ChecksumValid)
		assert.Contains(t, resp.Message, "corrupted block")
		mockSvc.AssertExpectations(t)
	})

	t.Run("verify backup invalid without issues", func(t *testing.T) {
		mockSvc.On("VerifyBackup", mock.Anything, mock.Anything).Return(&service.VerifyBackupResponse{
			Valid:         false,
			ChecksumMatch: true,
			Issues:        []string{},
		}, nil).Once()

		resp, err := srv.VerifyBackup(ctx, &pb.VerifyBackupRequest{
			BackupId: "backup-invalid",
		})

		require.NoError(t, err)
		assert.False(t, resp.Valid)
		assert.Equal(t, "Backup verification failed", resp.Message)
		mockSvc.AssertExpectations(t)
	})

	t.Run("verify backup empty backup id", func(t *testing.T) {
		_, err := srv.VerifyBackup(ctx, &pb.VerifyBackupRequest{
			BackupId: "",
		})

		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("verify backup service error", func(t *testing.T) {
		mockSvc.On("VerifyBackup", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		_, err := srv.VerifyBackup(ctx, &pb.VerifyBackupRequest{
			BackupId: "backup-error",
		})

		assert.Error(t, err)
		mockSvc.AssertExpectations(t)
	})
}

// =============================================================================
// Backup Type Conversion Tests
// =============================================================================

func TestBackupTypeConversions(t *testing.T) {
	t.Run("proto to service name", func(t *testing.T) {
		tests := []struct {
			name     string
			input    pb.BackupType
			expected string
		}{
			{"full", pb.BackupType_BACKUP_TYPE_FULL, "full"},
			{"incremental", pb.BackupType_BACKUP_TYPE_INCREMENTAL, "incremental"},
			{"snapshot", pb.BackupType_BACKUP_TYPE_SNAPSHOT, "snapshot"},
			{"unspecified defaults to full", pb.BackupType_BACKUP_TYPE_UNSPECIFIED, "full"},
			{"unknown defaults to full", pb.BackupType(999), "full"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := protoBackupTypeToServiceName(tt.input)
				assert.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("service name to proto", func(t *testing.T) {
		tests := []struct {
			name     string
			input    string
			expected pb.BackupType
		}{
			{"full", "full", pb.BackupType_BACKUP_TYPE_FULL},
			{"incremental", "incremental", pb.BackupType_BACKUP_TYPE_INCREMENTAL},
			{"snapshot", "snapshot", pb.BackupType_BACKUP_TYPE_SNAPSHOT},
			{"unknown defaults to unspecified", "unknown", pb.BackupType_BACKUP_TYPE_UNSPECIFIED},
			{"empty defaults to unspecified", "", pb.BackupType_BACKUP_TYPE_UNSPECIFIED},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := serviceNameToProtoBackupType(tt.input)
				assert.Equal(t, tt.expected, result)
			})
		}
	})
}

// =============================================================================
// Compression Type Conversion Tests
// =============================================================================

func TestCompressionTypeConversion(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.CompressionType
		expected string
	}{
		{"none", pb.CompressionType_COMPRESSION_TYPE_NONE, "none"},
		{"gzip", pb.CompressionType_COMPRESSION_TYPE_GZIP, "gzip"},
		{"snappy", pb.CompressionType_COMPRESSION_TYPE_SNAPPY, "snappy"},
		{"lz4", pb.CompressionType_COMPRESSION_TYPE_LZ4, "lz4"},
		{"unspecified defaults to empty", pb.CompressionType_COMPRESSION_TYPE_UNSPECIFIED, ""},
		{"unknown defaults to empty", pb.CompressionType(999), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoCompressionTypeToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Encryption Type Conversion Tests
// =============================================================================

func TestEncryptionTypeConversion(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.EncryptionType
		expected string
	}{
		{"none", pb.EncryptionType_ENCRYPTION_TYPE_NONE, "none"},
		{"aes256", pb.EncryptionType_ENCRYPTION_TYPE_AES256, "aes256"},
		{"unspecified defaults to empty", pb.EncryptionType_ENCRYPTION_TYPE_UNSPECIFIED, ""},
		{"unknown defaults to empty", pb.EncryptionType(999), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoEncryptionTypeToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
