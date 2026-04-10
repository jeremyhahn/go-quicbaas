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

	"github.com/stretchr/testify/assert"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ========================================
// Object Storage Helper Function Tests
// ========================================

func TestStringToObjectStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected pb.ObjectStatus
	}{
		{
			name:     "active",
			input:    "active",
			expected: pb.ObjectStatus_OBJECT_STATUS_ACTIVE,
		},
		{
			name:     "archived",
			input:    "archived",
			expected: pb.ObjectStatus_OBJECT_STATUS_ARCHIVED,
		},
		{
			name:     "restoring",
			input:    "restoring",
			expected: pb.ObjectStatus_OBJECT_STATUS_RESTORING,
		},
		{
			name:     "pending_delete",
			input:    "pending_delete",
			expected: pb.ObjectStatus_OBJECT_STATUS_PENDING_DELETE,
		},
		{
			name:     "deleted",
			input:    "deleted",
			expected: pb.ObjectStatus_OBJECT_STATUS_DELETED,
		},
		{
			name:     "corrupted",
			input:    "corrupted",
			expected: pb.ObjectStatus_OBJECT_STATUS_CORRUPTED,
		},
		{
			name:     "unknown defaults to unspecified",
			input:    "unknown",
			expected: pb.ObjectStatus_OBJECT_STATUS_UNSPECIFIED,
		},
		{
			name:     "empty defaults to unspecified",
			input:    "",
			expected: pb.ObjectStatus_OBJECT_STATUS_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringToObjectStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStringToBackendType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected pb.BackendType
	}{
		{
			name:     "local",
			input:    "local",
			expected: pb.BackendType_BACKEND_TYPE_LOCAL,
		},
		{
			name:     "s3",
			input:    "s3",
			expected: pb.BackendType_BACKEND_TYPE_S3,
		},
		{
			name:     "gcs",
			input:    "gcs",
			expected: pb.BackendType_BACKEND_TYPE_GCS,
		},
		{
			name:     "azure",
			input:    "azure",
			expected: pb.BackendType_BACKEND_TYPE_AZURE,
		},
		{
			name:     "minio",
			input:    "minio",
			expected: pb.BackendType_BACKEND_TYPE_MINIO,
		},
		{
			name:     "pebble",
			input:    "pebble",
			expected: pb.BackendType_BACKEND_TYPE_PEBBLE,
		},
		{
			name:     "raft",
			input:    "raft",
			expected: pb.BackendType_BACKEND_TYPE_RAFT,
		},
		{
			name:     "unknown defaults to unspecified",
			input:    "unknown",
			expected: pb.BackendType_BACKEND_TYPE_UNSPECIFIED,
		},
		{
			name:     "empty defaults to unspecified",
			input:    "",
			expected: pb.BackendType_BACKEND_TYPE_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringToBackendType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStringToStorageTier(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected pb.StorageTier
	}{
		{
			name:     "hot",
			input:    "hot",
			expected: pb.StorageTier_STORAGE_TIER_HOT,
		},
		{
			name:     "warm",
			input:    "warm",
			expected: pb.StorageTier_STORAGE_TIER_WARM,
		},
		{
			name:     "cold",
			input:    "cold",
			expected: pb.StorageTier_STORAGE_TIER_COLD,
		},
		{
			name:     "archive",
			input:    "archive",
			expected: pb.StorageTier_STORAGE_TIER_ARCHIVE,
		},
		{
			name:     "unknown defaults to unspecified",
			input:    "unknown",
			expected: pb.StorageTier_STORAGE_TIER_UNSPECIFIED,
		},
		{
			name:     "empty defaults to unspecified",
			input:    "",
			expected: pb.StorageTier_STORAGE_TIER_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringToStorageTier(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStringToReplicationMode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected pb.ReplicationMode
	}{
		{
			name:     "sync",
			input:    "sync",
			expected: pb.ReplicationMode_REPLICATION_MODE_SYNC,
		},
		{
			name:     "async",
			input:    "async",
			expected: pb.ReplicationMode_REPLICATION_MODE_ASYNC,
		},
		{
			name:     "quorum",
			input:    "quorum",
			expected: pb.ReplicationMode_REPLICATION_MODE_QUORUM,
		},
		{
			name:     "unknown defaults to unspecified",
			input:    "unknown",
			expected: pb.ReplicationMode_REPLICATION_MODE_UNSPECIFIED,
		},
		{
			name:     "empty defaults to unspecified",
			input:    "",
			expected: pb.ReplicationMode_REPLICATION_MODE_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringToReplicationMode(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProtoReplicationModeToString(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.ReplicationMode
		expected catalog.ReplicationMode
	}{
		{
			name:     "sync",
			input:    pb.ReplicationMode_REPLICATION_MODE_SYNC,
			expected: catalog.ReplicationModeSync,
		},
		{
			name:     "async",
			input:    pb.ReplicationMode_REPLICATION_MODE_ASYNC,
			expected: catalog.ReplicationModeAsync,
		},
		{
			name:     "quorum",
			input:    pb.ReplicationMode_REPLICATION_MODE_QUORUM,
			expected: catalog.ReplicationModeQuorum,
		},
		{
			name:     "unspecified defaults to async",
			input:    pb.ReplicationMode_REPLICATION_MODE_UNSPECIFIED,
			expected: catalog.ReplicationModeAsync,
		},
		{
			name:     "unknown defaults to async",
			input:    pb.ReplicationMode(999),
			expected: catalog.ReplicationModeAsync,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoReplicationModeToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReplicationPolicyToProto(t *testing.T) {
	t.Run("nil policy returns nil", func(t *testing.T) {
		result := replicationPolicyToProto(nil)
		assert.Nil(t, result)
	})

	t.Run("valid policy converts correctly", func(t *testing.T) {
		policy := &catalog.ReplicationPolicy{
			Enabled:      true,
			MinReplicas:  3,
			TargetStores: []string{"store1", "store2"},
			Mode:         catalog.ReplicationModeSync,
			Priority:     5,
		}
		result := replicationPolicyToProto(policy)
		assert.NotNil(t, result)
		assert.True(t, result.Enabled)
		assert.Equal(t, int32(3), result.MinReplicas)
		assert.Equal(t, []string{"store1", "store2"}, result.TargetStores)
		assert.Equal(t, pb.ReplicationMode_REPLICATION_MODE_SYNC, result.Mode)
		assert.Equal(t, int32(5), result.Priority)
	})
}

func TestArchivePolicyToProto(t *testing.T) {
	t.Run("nil policy returns nil", func(t *testing.T) {
		result := archivePolicyToProto(nil)
		assert.Nil(t, result)
	})

	t.Run("valid policy converts correctly", func(t *testing.T) {
		policy := &catalog.ArchivePolicy{
			Enabled:           true,
			ArchiveAfterDays:  30,
			TargetStore:       "archive-store",
			DeleteFromPrimary: true,
			CompressionLevel:  5,
		}
		result := archivePolicyToProto(policy)
		assert.NotNil(t, result)
		assert.True(t, result.Enabled)
		assert.Equal(t, int32(30), result.ArchiveAfterDays)
		assert.Equal(t, "archive-store", result.TargetStore)
		assert.True(t, result.DeleteFromPrimary)
		assert.Equal(t, int32(5), result.CompressionLevel)
	})
}

func TestRetentionPolicyToProto(t *testing.T) {
	t.Run("nil policy returns nil", func(t *testing.T) {
		result := retentionPolicyToProto(nil)
		assert.Nil(t, result)
	})

	t.Run("valid policy converts correctly", func(t *testing.T) {
		policy := &catalog.RetentionPolicy{
			Enabled:       true,
			RetentionDays: 90,
			MaxVersions:   10,
		}
		result := retentionPolicyToProto(policy)
		assert.NotNil(t, result)
		assert.True(t, result.Enabled)
		assert.Equal(t, int32(90), result.RetentionDays)
		assert.Equal(t, int32(10), result.MaxVersions)
	})
}

func TestObjectSummaryToProto(t *testing.T) {
	t.Run("nil summary returns nil", func(t *testing.T) {
		result := objectSummaryToProto(nil)
		assert.Nil(t, result)
	})

	t.Run("valid summary converts correctly", func(t *testing.T) {
		summary := &service.ObjectSummary{
			Key:         "test/object.txt",
			Size:        1024,
			ContentType: "text/plain",
			Version:     1,
			CreatedAt:   1000000000,
			ModifiedAt:  2000000000,
			Tags:        map[string]string{"env": "test"},
			Status:      "active",
		}
		result := objectSummaryToProto(summary)
		assert.NotNil(t, result)
		assert.Equal(t, "test/object.txt", result.Key)
		assert.Equal(t, int64(1024), result.Size)
		assert.Equal(t, "text/plain", result.ContentType)
		assert.Equal(t, map[string]string{"env": "test"}, result.Tags)
		assert.Equal(t, pb.ObjectStatus_OBJECT_STATUS_ACTIVE, result.Status)
	})
}

func TestBackendInfoToProto(t *testing.T) {
	t.Run("nil backend info returns nil", func(t *testing.T) {
		result := backendInfoToProto(nil)
		assert.Nil(t, result)
	})

	t.Run("valid backend info converts correctly", func(t *testing.T) {
		info := &service.BackendInfo{
			Name:        "primary-store",
			Type:        "s3",
			Enabled:     true,
			Tier:        "hot",
			Region:      "us-east-1",
			ObjectCount: 1000,
			TotalSize:   1024 * 1024,
			Health: &service.BackendHealthInfo{
				Healthy:   true,
				LastCheck: 3000000000,
			},
		}
		result := backendInfoToProto(info)
		assert.NotNil(t, result)
		assert.Equal(t, "primary-store", result.Name)
		assert.Equal(t, pb.BackendType_BACKEND_TYPE_S3, result.Type)
		assert.True(t, result.Enabled)
		assert.Equal(t, pb.StorageTier_STORAGE_TIER_HOT, result.Tier)
		assert.Equal(t, "us-east-1", result.Region)
		assert.Equal(t, int64(1000), result.ObjectCount)
		assert.Equal(t, int64(1024*1024), result.UsedBytes)
		assert.True(t, result.Healthy)
	})

	t.Run("backend info without health converts correctly", func(t *testing.T) {
		info := &service.BackendInfo{
			Name:    "secondary-store",
			Type:    "local",
			Enabled: true,
			Tier:    "warm",
			Health:  nil,
		}
		result := backendInfoToProto(info)
		assert.NotNil(t, result)
		assert.Equal(t, "secondary-store", result.Name)
		assert.False(t, result.Healthy)
	})
}
