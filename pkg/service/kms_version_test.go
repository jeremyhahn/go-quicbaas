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

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// requireNegativeVersionError asserts err reports a negative KMS key version.
func requireNegativeVersionError(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var negErr *conv.ConversionNegativeError
	require.ErrorAs(t, err, &negErr)
	assert.Contains(t, err.Error(), "invalid key version in metadata")
}

// negativeVersionKMSManager returns a mock whose GetKey reports a corrupt,
// negative key version.
func negativeVersionKMSManager() *mockKMSManager {
	mockMgr := newMockKMSManager()
	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &kms.KeyMetadata{KeyID: keyID, Algorithm: "AES-256-GCM", Version: -1}, nil
	}
	return mockMgr
}

func TestKMSKeyVersion(t *testing.T) {
	got, err := kmsKeyVersion("op", 7)
	require.NoError(t, err)
	assert.Equal(t, uint64(7), got)

	_, err = kmsKeyVersion("op", -1)
	requireNegativeVersionError(t, err)
}

func TestExtractKeyMetadata_NegativeVersion(t *testing.T) {
	metadata, err := extractKeyMetadata("key-1", &kms.KeyMetadata{KeyID: "key-1", Version: -3})
	assert.Nil(t, metadata)
	requireNegativeVersionError(t, err)
}

func TestKMSService_GetKey_NegativeVersion(t *testing.T) {
	svc := newKMSService(negativeVersionKMSManager(), newMockLogger())

	resp, err := svc.GetKey(systemTenantCtx(), &GetKeyRequest{KeyID: "test-key"})
	assert.Nil(t, resp)
	requireNegativeVersionError(t, err)
}

func TestKMSService_GetPublicKey_NegativeVersion(t *testing.T) {
	mockMgr := negativeVersionKMSManager()
	mockMgr.keys["test-key"] = &mockKeyData{keyID: "test-key", algorithm: "ECDSA-P256"}
	svc := newKMSService(mockMgr, newMockLogger())

	resp, err := svc.GetPublicKey(systemTenantCtx(), &GetPublicKeyRequest{KeyID: "test-key"})
	assert.Nil(t, resp)
	requireNegativeVersionError(t, err)
}

func TestKMSService_GetKeyMetadata_NegativeVersion(t *testing.T) {
	svc := newKMSService(negativeVersionKMSManager(), newMockLogger())

	resp, err := svc.GetKeyMetadata(systemTenantCtx(), &GetKeyMetadataRequest{KeyID: "test-key"})
	assert.Nil(t, resp)
	requireNegativeVersionError(t, err)
}

func TestKMSService_ExportKey_NegativeVersion(t *testing.T) {
	svc := newKMSService(negativeVersionKMSManager(), newMockLogger())

	resp, err := svc.ExportKey(context.Background(), &ExportKeyRequest{
		KeyID:             "source-key",
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-KW",
	})
	assert.Nil(t, resp)
	requireNegativeVersionError(t, err)
}

func TestKMSService_ListKeyVersions_NegativeVersion(t *testing.T) {
	svc := newKMSService(negativeVersionKMSManager(), newMockLogger())

	resp, err := svc.ListKeyVersions(systemTenantCtx(), &ListKeyVersionsRequest{KeyID: "test-key"})
	assert.Nil(t, resp)
	requireNegativeVersionError(t, err)
}

func TestKMSService_RotateKey_NegativeVersion(t *testing.T) {
	t.Run("before rotation", func(t *testing.T) {
		mockMgr := negativeVersionKMSManager()
		rotated := false
		mockMgr.rotateKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
			rotated = true
			return &kms.KeyMetadata{KeyID: keyID, Version: 2}, nil
		}
		svc := newKMSService(mockMgr, newMockLogger())

		resp, err := svc.RotateKey(systemTenantCtx(), &RotateKeyRequest{KeyID: "test-key"})
		assert.Nil(t, resp)
		requireNegativeVersionError(t, err)
		assert.False(t, rotated, "a corrupt current version must stop the rotation")
	})

	t.Run("after rotation", func(t *testing.T) {
		mockMgr := newMockKMSManager()
		mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
			return &kms.KeyMetadata{KeyID: keyID, Version: 1}, nil
		}
		mockMgr.rotateKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
			return &kms.KeyMetadata{KeyID: keyID, Version: -2}, nil
		}
		svc := newKMSService(mockMgr, newMockLogger())

		resp, err := svc.RotateKey(systemTenantCtx(), &RotateKeyRequest{KeyID: "test-key"})
		assert.Nil(t, resp)
		requireNegativeVersionError(t, err)
	})
}

// reflectedKey is a key record of a type ListKeys only reads by reflection.
type reflectedKey struct {
	KeyID   string
	Version int64
}

func TestKMSService_ListKeys_NegativeVersion(t *testing.T) {
	tests := []struct {
		name string
		key  interface{}
	}{
		{name: "kms metadata", key: &kms.KeyMetadata{KeyID: "test-key", Version: -1}},
		{name: "reflected record", key: &reflectedKey{KeyID: "test-key", Version: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMgr := newMockKMSManager()
			mockMgr.listKeysFn = func(ctx context.Context, prefix string) ([]interface{}, error) {
				return []interface{}{tt.key}, nil
			}
			svc := newKMSService(mockMgr, newMockLogger())

			resp, err := svc.ListKeys(systemTenantCtx(), &ListKeysRequest{})
			assert.Nil(t, resp)
			requireNegativeVersionError(t, err)
		})
	}
}
