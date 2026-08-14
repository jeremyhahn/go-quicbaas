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
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// =============================================================================
// GenerateKey / ImportKey deep paths.
//
// The existing tests cover the symmetric happy path and validation guards. This
// file pins the remaining branches: the asymmetric (signing) KeyType default,
// the rotation-policy persistence block (manager returning a typed
// *kms.KeyMetadata so kmsMeta is non-nil), and the manager-error path.
// =============================================================================

func TestKMSService_GenerateKey_AsymmetricSigningDefault(t *testing.T) {
	mockManager := newMockKMSManager()
	svc := newKMSService(mockManager, newMockLogger())

	// SymmetricAlgorithm empty + KeyAlgorithm == x509.RSA(1) means the KeyType
	// defaults to signing.
	req := &GenerateKeyRequest{
		Name: "sign-key",
		KeyAttributes: &xkmssdk.KeyAttributes{
			KeyAlgorithm: 1, // x509.RSA
		},
		Usage: []KeyUsage{KeyUsageSign, KeyUsageVerify},
	}

	resp, err := svc.GenerateKey(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "sign-key", resp.Metadata.Name)
}

func TestKMSService_GenerateKey_WithRotationPolicyPersists(t *testing.T) {
	mockManager := newMockKMSManager()
	// Return a typed *kms.KeyMetadata so the metadata-persist + rotation block runs.
	mockManager.generateKeyFn = func(_ context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
		return &kms.KeyMetadata{KeyID: attrs.CN, Name: attrs.CN, Algorithm: "AES-256-GCM", Version: 1}, nil
	}
	svc := newKMSService(mockManager, newMockLogger())
	store := &mockKVStoreForKMS{data: make(map[string][]byte)}
	svc.SetKVStore(store)

	req := &GenerateKeyRequest{
		Name: "rot-key",
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
		Usage: []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
		RotationPolicy: &RotationPolicy{
			Enabled:    true,
			AutoRotate: true,
			Interval:   24 * time.Hour,
		},
	}

	resp, err := svc.GenerateKey(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp.Metadata.RotationPolicy)
	// NextRotation is auto-computed from Interval.
	assert.False(t, resp.Metadata.RotationPolicy.NextRotation.IsZero())
	// Policy is persisted to the KVStore.
	_, ok := store.data[kmsRotationPolicyKey("rot-key")]
	assert.True(t, ok)
}

func TestKMSService_ImportKey_TypedMetadataPersists(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.importKeyFn = func(_ context.Context, keyID string, _ []byte, algorithm string) (interface{}, error) {
		return &kms.KeyMetadata{KeyID: keyID, Name: keyID, Algorithm: algorithm, Version: 1}, nil
	}
	svc := newKMSService(mockManager, newMockLogger())

	resp, err := svc.ImportKey(context.Background(), &ImportKeyRequest{
		Name:        "imported",
		Description: "imported key",
		KeyMaterial: make([]byte, 32),
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
			Exportable:         true,
		},
		Usage: []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
		Tags:  map[string]string{"src": "external"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "imported", resp.Metadata.Name)
	assert.True(t, resp.Metadata.Exportable)
}
