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
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// convertSecret Tests
// =============================================================================

func TestConvertSecret_AllCases(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		result, err := convertSecret(nil)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("valid_secret_without_policy", func(t *testing.T) {
		now := time.Now()
		sec := &service.Secret{
			SecretID:       "secret-123",
			Name:           "test-secret",
			Description:    "A test secret",
			CurrentVersion: 5,
			CreatedAt:      now.Add(-1 * time.Hour),
			UpdatedAt:      now,
			RotationPolicy: nil,
			Tags:           map[string]string{"env": "test"},
			Owner:          "admin",
		}

		result, err := convertSecret(sec)
		require.NoError(t, err)

		require.NotNil(t, result)
		assert.Equal(t, "secret-123", result.SecretId)
		assert.Equal(t, "test-secret", result.Name)
		assert.Equal(t, "A test secret", result.Description)
		assert.Equal(t, uint64(5), result.CurrentVersion)
		assert.Equal(t, now.Add(-1*time.Hour).Unix(), result.CreatedAt)
		assert.Equal(t, now.Unix(), result.UpdatedAt)
		assert.Nil(t, result.RotationPolicy)
		assert.Equal(t, "test", result.Tags["env"])
		assert.Equal(t, "admin", result.Owner)
	})

	t.Run("valid_secret_with_rotation_policy", func(t *testing.T) {
		now := time.Now()
		sec := &service.Secret{
			SecretID:       "secret-456",
			Name:           "rotating-secret",
			Description:    "A rotating secret",
			CurrentVersion: 10,
			CreatedAt:      now.Add(-24 * time.Hour),
			UpdatedAt:      now,
			RotationPolicy: &service.SecretRotationPolicy{
				Enabled:        true,
				Interval:       24 * time.Hour,
				NextRotation:   now.Add(24 * time.Hour),
				RetainVersions: 5,
				RotationLambda: "my-rotation-function",
			},
			Tags:  map[string]string{"type": "db-creds"},
			Owner: "svc-account",
		}

		result, err := convertSecret(sec)
		require.NoError(t, err)

		require.NotNil(t, result)
		assert.Equal(t, "secret-456", result.SecretId)
		require.NotNil(t, result.RotationPolicy)
		assert.True(t, result.RotationPolicy.Enabled)
		assert.Equal(t, int64(86400), result.RotationPolicy.IntervalSeconds)
		assert.Equal(t, int32(5), result.RotationPolicy.RetainVersions)
		assert.Equal(t, "my-rotation-function", result.RotationPolicy.RotationLambda)
	})

	t.Run("secret_with_empty_tags", func(t *testing.T) {
		now := time.Now()
		sec := &service.Secret{
			SecretID:       "secret-789",
			Name:           "no-tags-secret",
			CurrentVersion: 1,
			CreatedAt:      now,
			UpdatedAt:      now,
			Tags:           map[string]string{},
		}

		result, err := convertSecret(sec)
		require.NoError(t, err)

		require.NotNil(t, result)
		assert.Empty(t, result.Tags)
	})
}

// =============================================================================
// convertSecretVersion Tests
// =============================================================================

func TestConvertSecretVersion_AllCases(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		result := convertSecretVersion(nil)
		assert.Nil(t, result)
	})

	t.Run("version_without_deleted_or_expires", func(t *testing.T) {
		now := time.Now()
		ver := &service.SecretVersion{
			SecretID:  "secret-123",
			Version:   3,
			Metadata:  map[string]string{"key": "value"},
			CreatedAt: now,
			CreatedBy: "user@example.com",
			DeletedAt: nil,
			ExpiresAt: nil,
		}

		result := convertSecretVersion(ver)

		require.NotNil(t, result)
		assert.Equal(t, "secret-123", result.SecretId)
		assert.Equal(t, uint64(3), result.Version)
		assert.Equal(t, "value", result.Metadata["key"])
		assert.Equal(t, now.Unix(), result.CreatedAt)
		assert.Equal(t, "user@example.com", result.CreatedBy)
		assert.Equal(t, int64(0), result.DeletedAt)
		assert.Equal(t, int64(0), result.ExpiresAt)
	})

	t.Run("version_with_deleted_at", func(t *testing.T) {
		now := time.Now()
		deletedTime := now.Add(-1 * time.Hour)
		ver := &service.SecretVersion{
			SecretID:  "secret-456",
			Version:   2,
			Metadata:  nil,
			CreatedAt: now.Add(-24 * time.Hour),
			CreatedBy: "admin",
			DeletedAt: &deletedTime,
			ExpiresAt: nil,
		}

		result := convertSecretVersion(ver)

		require.NotNil(t, result)
		assert.Equal(t, deletedTime.Unix(), result.DeletedAt)
		assert.Equal(t, int64(0), result.ExpiresAt)
	})

	t.Run("version_with_expires_at", func(t *testing.T) {
		now := time.Now()
		expiresTime := now.Add(7 * 24 * time.Hour)
		ver := &service.SecretVersion{
			SecretID:  "secret-789",
			Version:   1,
			Metadata:  map[string]string{"rotation": "auto"},
			CreatedAt: now,
			CreatedBy: "system",
			DeletedAt: nil,
			ExpiresAt: &expiresTime,
		}

		result := convertSecretVersion(ver)

		require.NotNil(t, result)
		assert.Equal(t, int64(0), result.DeletedAt)
		assert.Equal(t, expiresTime.Unix(), result.ExpiresAt)
	})

	t.Run("version_with_both_deleted_and_expires", func(t *testing.T) {
		now := time.Now()
		deletedTime := now.Add(-1 * time.Hour)
		expiresTime := now.Add(24 * time.Hour)
		ver := &service.SecretVersion{
			SecretID:  "secret-000",
			Version:   5,
			Metadata:  map[string]string{},
			CreatedAt: now.Add(-48 * time.Hour),
			CreatedBy: "rotation-service",
			DeletedAt: &deletedTime,
			ExpiresAt: &expiresTime,
		}

		result := convertSecretVersion(ver)

		require.NotNil(t, result)
		assert.Equal(t, deletedTime.Unix(), result.DeletedAt)
		assert.Equal(t, expiresTime.Unix(), result.ExpiresAt)
	})
}

// =============================================================================
// convertSecretRotationPolicy Tests
// =============================================================================

func TestConvertSecretRotationPolicy_AllCases(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		result, err := convertSecretRotationPolicy(nil)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("enabled_policy", func(t *testing.T) {
		now := time.Now()
		policy := &service.SecretRotationPolicy{
			Enabled:        true,
			Interval:       48 * time.Hour,
			NextRotation:   now.Add(48 * time.Hour),
			RetainVersions: 10,
			RotationLambda: "rotate-db-password",
		}

		result, err := convertSecretRotationPolicy(policy)
		require.NoError(t, err)

		require.NotNil(t, result)
		assert.True(t, result.Enabled)
		assert.Equal(t, int64(172800), result.IntervalSeconds) // 48 hours in seconds
		assert.Equal(t, now.Add(48*time.Hour).Unix(), result.NextRotation)
		assert.Equal(t, int32(10), result.RetainVersions)
		assert.Equal(t, "rotate-db-password", result.RotationLambda)
	})

	t.Run("disabled_policy", func(t *testing.T) {
		policy := &service.SecretRotationPolicy{
			Enabled:        false,
			Interval:       0,
			NextRotation:   time.Time{},
			RetainVersions: 0,
			RotationLambda: "",
		}

		result, err := convertSecretRotationPolicy(policy)
		require.NoError(t, err)

		require.NotNil(t, result)
		assert.False(t, result.Enabled)
		assert.Equal(t, int64(0), result.IntervalSeconds)
		assert.Empty(t, result.RotationLambda)
	})

	t.Run("policy_with_custom_lambda", func(t *testing.T) {
		now := time.Now()
		policy := &service.SecretRotationPolicy{
			Enabled:        true,
			Interval:       1 * time.Hour,
			NextRotation:   now.Add(1 * time.Hour),
			RetainVersions: 3,
			RotationLambda: "arn:aws:lambda:us-east-1:123456789:function:custom-rotate",
		}

		result, err := convertSecretRotationPolicy(policy)
		require.NoError(t, err)

		require.NotNil(t, result)
		assert.Equal(t, int64(3600), result.IntervalSeconds)
		assert.Equal(t, "arn:aws:lambda:us-east-1:123456789:function:custom-rotate", result.RotationLambda)
	})
}

// =============================================================================
// convertProtoSecretRotationPolicy Tests
// =============================================================================

func TestConvertProtoSecretRotationPolicy_AllCases(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		result := convertProtoSecretRotationPolicy(nil)
		assert.Nil(t, result)
	})

	t.Run("enabled_proto_policy", func(t *testing.T) {
		now := time.Now()
		protoPolicy := &pb.SecretRotationPolicy{
			Enabled:         true,
			IntervalSeconds: 86400, // 24 hours
			NextRotation:    now.Add(24 * time.Hour).Unix(),
			RetainVersions:  5,
			RotationLambda:  "my-lambda",
		}

		result := convertProtoSecretRotationPolicy(protoPolicy)

		require.NotNil(t, result)
		assert.True(t, result.Enabled)
		assert.Equal(t, 24*time.Hour, result.Interval)
		assert.Equal(t, 5, result.RetainVersions)
		assert.Equal(t, "my-lambda", result.RotationLambda)
	})

	t.Run("disabled_proto_policy", func(t *testing.T) {
		protoPolicy := &pb.SecretRotationPolicy{
			Enabled:         false,
			IntervalSeconds: 0,
			NextRotation:    0,
			RetainVersions:  0,
			RotationLambda:  "",
		}

		result := convertProtoSecretRotationPolicy(protoPolicy)

		require.NotNil(t, result)
		assert.False(t, result.Enabled)
		assert.Equal(t, time.Duration(0), result.Interval)
		assert.Equal(t, 0, result.RetainVersions)
	})

	t.Run("proto_policy_with_large_interval", func(t *testing.T) {
		protoPolicy := &pb.SecretRotationPolicy{
			Enabled:         true,
			IntervalSeconds: 604800, // 7 days
			NextRotation:    time.Now().Add(7 * 24 * time.Hour).Unix(),
			RetainVersions:  100,
			RotationLambda:  "weekly-rotate",
		}

		result := convertProtoSecretRotationPolicy(protoPolicy)

		require.NotNil(t, result)
		assert.Equal(t, 7*24*time.Hour, result.Interval)
		assert.Equal(t, 100, result.RetainVersions)
	})
}
