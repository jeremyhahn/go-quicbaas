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
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// =============================================================================
// protoToArchivePolicy Tests
// =============================================================================

func TestProtoToArchivePolicy_AllCases(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		result := protoToArchivePolicy(nil)
		assert.Nil(t, result)
	})

	t.Run("enabled_policy", func(t *testing.T) {
		policy := &pb.ArchivePolicy{
			Enabled:           true,
			ArchiveAfterDays:  90,
			TargetStore:       "s3://archive-bucket",
			DeleteFromPrimary: true,
			CompressionLevel:  6,
		}

		result := protoToArchivePolicy(policy)

		require.NotNil(t, result)
		assert.True(t, result.Enabled)
		assert.Equal(t, 90, result.ArchiveAfterDays)
		assert.Equal(t, "s3://archive-bucket", result.TargetStore)
		assert.True(t, result.DeleteFromPrimary)
		assert.Equal(t, 6, result.CompressionLevel)
	})

	t.Run("disabled_policy", func(t *testing.T) {
		policy := &pb.ArchivePolicy{
			Enabled:           false,
			ArchiveAfterDays:  0,
			TargetStore:       "",
			DeleteFromPrimary: false,
			CompressionLevel:  0,
		}

		result := protoToArchivePolicy(policy)

		require.NotNil(t, result)
		assert.False(t, result.Enabled)
		assert.Equal(t, 0, result.ArchiveAfterDays)
		assert.Empty(t, result.TargetStore)
	})

	t.Run("high_compression_level", func(t *testing.T) {
		policy := &pb.ArchivePolicy{
			Enabled:           true,
			ArchiveAfterDays:  365,
			TargetStore:       "gcs://archive-bucket",
			DeleteFromPrimary: false,
			CompressionLevel:  9,
		}

		result := protoToArchivePolicy(policy)

		require.NotNil(t, result)
		assert.Equal(t, 9, result.CompressionLevel)
		assert.Equal(t, 365, result.ArchiveAfterDays)
	})
}

// =============================================================================
// protoToRetentionPolicy Tests
// =============================================================================

func TestProtoToRetentionPolicy_AllCases(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		result := protoToRetentionPolicy(nil)
		assert.Nil(t, result)
	})

	t.Run("enabled_policy", func(t *testing.T) {
		policy := &pb.RetentionPolicy{
			Enabled:       true,
			RetentionDays: 365,
			MaxVersions:   10,
		}

		result := protoToRetentionPolicy(policy)

		require.NotNil(t, result)
		assert.True(t, result.Enabled)
		assert.Equal(t, 365, result.RetentionDays)
		assert.Equal(t, 10, result.MaxVersions)
	})

	t.Run("disabled_policy", func(t *testing.T) {
		policy := &pb.RetentionPolicy{
			Enabled:       false,
			RetentionDays: 0,
			MaxVersions:   0,
		}

		result := protoToRetentionPolicy(policy)

		require.NotNil(t, result)
		assert.False(t, result.Enabled)
		assert.Equal(t, 0, result.RetentionDays)
		assert.Equal(t, 0, result.MaxVersions)
	})

	t.Run("long_retention_many_versions", func(t *testing.T) {
		policy := &pb.RetentionPolicy{
			Enabled:       true,
			RetentionDays: 3650, // 10 years
			MaxVersions:   100,
		}

		result := protoToRetentionPolicy(policy)

		require.NotNil(t, result)
		assert.Equal(t, 3650, result.RetentionDays)
		assert.Equal(t, 100, result.MaxVersions)
	})
}

// =============================================================================
// replicationPolicyToProto Tests
// =============================================================================

func TestReplicationPolicyToProto_AllCases(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		result := replicationPolicyToProto(nil)
		assert.Nil(t, result)
	})
}

// =============================================================================
// archivePolicyToProto Tests
// =============================================================================

func TestArchivePolicyToProto_AllCases(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		result := archivePolicyToProto(nil)
		assert.Nil(t, result)
	})
}

// =============================================================================
// retentionPolicyToProto Tests
// =============================================================================

func TestRetentionPolicyToProto_AllCases(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		result := retentionPolicyToProto(nil)
		assert.Nil(t, result)
	})
}
