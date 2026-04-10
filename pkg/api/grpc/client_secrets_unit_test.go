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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestConvertProtoSecret(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		result := convertProtoSecret(nil)
		assert.Nil(t, result)
	})

	t.Run("converts valid secret", func(t *testing.T) {
		now := time.Now().Unix()
		protoSecret := &pb.Secret{
			SecretId:       "secret-123",
			Name:           "test-secret",
			Description:    "A test secret",
			CurrentVersion: 3,
			CreatedAt:      now,
			UpdatedAt:      now,
			Tags:           map[string]string{"env": "prod", "team": "security"},
			Owner:          "user-1",
			RotationPolicy: &pb.SecretRotationPolicy{
				Enabled:         true,
				IntervalSeconds: 30 * 24 * 60 * 60, // 30 days in seconds
				RetainVersions:  10,
			},
		}

		result := convertProtoSecret(protoSecret)

		assert.NotNil(t, result)
		assert.Equal(t, "secret-123", result.SecretID)
		assert.Equal(t, "test-secret", result.Name)
		assert.Equal(t, "A test secret", result.Description)
		assert.Equal(t, uint64(3), result.CurrentVersion)
		assert.Equal(t, time.Unix(now, 0), result.CreatedAt)
		assert.Equal(t, time.Unix(now, 0), result.UpdatedAt)
		assert.Equal(t, "user-1", result.Owner)
		assert.NotNil(t, result.RotationPolicy)
	})

	t.Run("handles nil rotation policy", func(t *testing.T) {
		protoSecret := &pb.Secret{
			SecretId:       "secret-456",
			Name:           "no-rotation",
			RotationPolicy: nil,
		}

		result := convertProtoSecret(protoSecret)

		assert.NotNil(t, result)
		assert.Nil(t, result.RotationPolicy)
	})
}

func TestConvertProtoSecretVersion(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		result := convertProtoSecretVersion(nil)
		assert.Nil(t, result)
	})

	t.Run("converts valid version without optional times", func(t *testing.T) {
		now := time.Now().Unix()
		protoVersion := &pb.SecretVersion{
			SecretId:  "secret-123",
			Version:   2,
			Metadata:  map[string]string{"env": "prod"},
			CreatedAt: now,
			CreatedBy: "admin",
			DeletedAt: 0,
			ExpiresAt: 0,
		}

		result := convertProtoSecretVersion(protoVersion)

		assert.NotNil(t, result)
		assert.Equal(t, "secret-123", result.SecretID)
		assert.Equal(t, uint64(2), result.Version)
		assert.Equal(t, map[string]string{"env": "prod"}, result.Metadata)
		assert.Equal(t, time.Unix(now, 0), result.CreatedAt)
		assert.Equal(t, "admin", result.CreatedBy)
		assert.Nil(t, result.DeletedAt)
		assert.Nil(t, result.ExpiresAt)
	})

	t.Run("converts version with deleted and expires times", func(t *testing.T) {
		now := time.Now().Unix()
		deletedAt := now + 3600
		expiresAt := now + 7200

		protoVersion := &pb.SecretVersion{
			SecretId:  "secret-789",
			Version:   5,
			CreatedAt: now,
			DeletedAt: deletedAt,
			ExpiresAt: expiresAt,
		}

		result := convertProtoSecretVersion(protoVersion)

		assert.NotNil(t, result)
		assert.NotNil(t, result.DeletedAt)
		assert.NotNil(t, result.ExpiresAt)
		assert.Equal(t, time.Unix(deletedAt, 0), *result.DeletedAt)
		assert.Equal(t, time.Unix(expiresAt, 0), *result.ExpiresAt)
	})
}

func TestConvertServiceSecretStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   service.SecretStatus
		expected pb.SecretStatus
	}{
		{
			name:     "enabled status",
			status:   service.SecretStatusEnabled,
			expected: pb.SecretStatus_SECRET_STATUS_ENABLED,
		},
		{
			name:     "disabled status",
			status:   service.SecretStatusDisabled,
			expected: pb.SecretStatus_SECRET_STATUS_DISABLED,
		},
		{
			name:     "unknown status defaults to unspecified",
			status:   service.SecretStatus("unknown"),
			expected: pb.SecretStatus_SECRET_STATUS_UNSPECIFIED,
		},
		{
			name:     "empty status defaults to unspecified",
			status:   service.SecretStatus(""),
			expected: pb.SecretStatus_SECRET_STATUS_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertServiceSecretStatus(tt.status)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertProtoSecretStatusToService(t *testing.T) {
	tests := []struct {
		name     string
		status   pb.SecretStatus
		expected service.SecretStatus
	}{
		{
			name:     "enabled status",
			status:   pb.SecretStatus_SECRET_STATUS_ENABLED,
			expected: service.SecretStatusEnabled,
		},
		{
			name:     "disabled status",
			status:   pb.SecretStatus_SECRET_STATUS_DISABLED,
			expected: service.SecretStatusDisabled,
		},
		{
			name:     "unspecified defaults to enabled",
			status:   pb.SecretStatus_SECRET_STATUS_UNSPECIFIED,
			expected: service.SecretStatusEnabled,
		},
		{
			name:     "unknown value defaults to enabled",
			status:   pb.SecretStatus(999),
			expected: service.SecretStatusEnabled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoSecretStatusToService(tt.status)
			assert.Equal(t, tt.expected, result)
		})
	}
}
