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
	stderrors "errors"
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

	resp, err := svc.GenerateKey(systemTenantCtx(), req)
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

	resp, err := svc.GenerateKey(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp.Metadata.RotationPolicy)
	// NextRotation is auto-computed from Interval.
	assert.False(t, resp.Metadata.RotationPolicy.NextRotation.IsZero())
	// Policy is persisted to the KVStore.
	_, ok := store.data[kmsRotationPolicyKey("rot-key")]
	assert.True(t, ok)
}

var (
	errMetadataStoreDown   = stderrors.New("metadata store down")
	errMetadataDeleteDown  = stderrors.New("key delete down")
	errMetadataKeyNotFound = stderrors.New("key not found")
	errPolicyDeleteDown    = stderrors.New("rotation policy delete down")
)

// metadataStoreKMSManager is a mockKMSManager whose keys and stored metadata
// live in one map, so a test can see whether a key outlived a failed
// metadata store.
type metadataStoreKMSManager struct {
	*mockKMSManager
	live      map[string]*kms.KeyMetadata
	storeErr  error
	deleteErr error
}

func newMetadataStoreKMSManager() *metadataStoreKMSManager {
	m := &metadataStoreKMSManager{mockKMSManager: newMockKMSManager(), live: make(map[string]*kms.KeyMetadata)}
	m.generateKeyFn = func(_ context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
		m.live[attrs.CN] = &kms.KeyMetadata{KeyID: attrs.CN, Name: attrs.CN, Algorithm: "AES-256-GCM", Version: 1}
		return &kms.KeyMetadata{KeyID: attrs.CN, Name: attrs.CN, Algorithm: "AES-256-GCM", Version: 1}, nil
	}
	m.getKeyFn = func(_ context.Context, keyID string) (interface{}, error) {
		meta, ok := m.live[keyID]
		if !ok {
			return nil, errMetadataKeyNotFound
		}
		return meta, nil
	}
	m.deleteKeyFn = func(_ context.Context, keyID string) error {
		if m.deleteErr != nil {
			return m.deleteErr
		}
		delete(m.live, keyID)
		return nil
	}
	return m
}

func (m *metadataStoreKMSManager) UpdateStoredMetadata(metadata interface{}) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	meta, ok := metadata.(*kms.KeyMetadata)
	if !ok {
		return errMetadataKeyNotFound
	}
	stored := *meta
	m.live[meta.KeyID] = &stored
	return nil
}

func newMetadataGenerateRequest(name string) *GenerateKeyRequest {
	return &GenerateKeyRequest{
		Name:          name,
		KeyAttributes: &xkmssdk.KeyAttributes{SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM},
		Usage:         []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
		Description:   "d",
		Tags:          map[string]string{"env": "prod"},
		Owner:         "svc-a",
	}
}

func TestKMSService_GenerateKey_MetadataStoreFailureDeletesKey(t *testing.T) {
	mgr := newMetadataStoreKMSManager()
	mgr.storeErr = errMetadataStoreDown
	svc := newKMSService(mgr, newMockLogger())
	ctx := systemTenantCtx()

	resp, err := svc.GenerateKey(ctx, newMetadataGenerateRequest("meta-key"))
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, errMetadataStoreDown)
	assert.Empty(t, mgr.live, "the key outlived its failed metadata store")

	_, err = svc.GetKeyMetadata(ctx, &GetKeyMetadataRequest{KeyID: "meta-key"})
	assert.ErrorIs(t, err, errMetadataKeyNotFound)

	// The name was never recorded, so the same name succeeds once the store recovers.
	mgr.storeErr = nil
	_, err = svc.GenerateKey(ctx, newMetadataGenerateRequest("meta-key"))
	require.NoError(t, err)
}

func TestKMSService_GenerateKey_MetadataStoreAndDeleteFailureCarriesBoth(t *testing.T) {
	mgr := newMetadataStoreKMSManager()
	mgr.storeErr = errMetadataStoreDown
	mgr.deleteErr = errMetadataDeleteDown
	svc := newKMSService(mgr, newMockLogger())

	_, err := svc.GenerateKey(systemTenantCtx(), newMetadataGenerateRequest("meta-key"))
	require.Error(t, err)
	assert.ErrorIs(t, err, errMetadataStoreDown)
	assert.ErrorIs(t, err, errMetadataDeleteDown)
}

// failingDeleteKVStore is a mockKVStoreForKMS whose Delete can fail, so a test
// can see a rotation-policy cleanup error reach the caller.
type failingDeleteKVStore struct {
	*mockKVStoreForKMS
	deleteErr error
}

func (s *failingDeleteKVStore) Delete(ctx context.Context, key string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return s.mockKVStoreForKMS.Delete(ctx, key)
}

func newRotationGenerateRequest(name string) *GenerateKeyRequest {
	req := newMetadataGenerateRequest(name)
	req.RotationPolicy = &RotationPolicy{Enabled: true, AutoRotate: true, Interval: 24 * time.Hour}
	return req
}

// TestKMSService_GenerateKey_MetadataStoreFailureRemovesRotationPolicy pins
// the rollback of the rotation policy: a creation whose metadata store fails
// leaves no policy in the KVStore, so a retry of the same name that asks for
// no policy does not answer the failed attempt's.
func TestKMSService_GenerateKey_MetadataStoreFailureRemovesRotationPolicy(t *testing.T) {
	mgr := newMetadataStoreKMSManager()
	mgr.storeErr = errMetadataStoreDown
	svc := newKMSService(mgr, newMockLogger())
	store := &mockKVStoreForKMS{data: make(map[string][]byte)}
	svc.SetKVStore(store)
	ctx := systemTenantCtx()

	resp, err := svc.GenerateKey(ctx, newRotationGenerateRequest("meta-key"))
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, errMetadataStoreDown)
	assert.Empty(t, mgr.live, "the key outlived its failed metadata store")
	_, kept := store.data[kmsRotationPolicyKey("meta-key")]
	assert.False(t, kept, "the failed attempt's rotation policy outlived the rollback")

	// The retry asks for no rotation policy and must not inherit one.
	mgr.storeErr = nil
	_, err = svc.GenerateKey(ctx, newMetadataGenerateRequest("meta-key"))
	require.NoError(t, err)

	got, err := svc.GetKeyRotationPolicy(ctx, &GetKeyRotationPolicyRequest{KeyID: "meta-key"})
	require.NoError(t, err)
	assert.Nil(t, got.Policy, "the retried key answers the failed attempt's rotation policy")
}

// TestKMSService_GenerateKey_RotationPolicyCleanupFailureCarriesBoth pins
// that a rotation-policy cleanup that fails during the rollback is not lost:
// the returned error carries it next to the metadata store's error.
func TestKMSService_GenerateKey_RotationPolicyCleanupFailureCarriesBoth(t *testing.T) {
	mgr := newMetadataStoreKMSManager()
	mgr.storeErr = errMetadataStoreDown
	svc := newKMSService(mgr, newMockLogger())
	svc.SetKVStore(&failingDeleteKVStore{
		mockKVStoreForKMS: &mockKVStoreForKMS{data: make(map[string][]byte)},
		deleteErr:         errPolicyDeleteDown,
	})

	_, err := svc.GenerateKey(systemTenantCtx(), newRotationGenerateRequest("meta-key"))
	require.Error(t, err)
	assert.ErrorIs(t, err, errMetadataStoreDown)
	assert.ErrorIs(t, err, errPolicyDeleteDown)
	assert.Empty(t, mgr.live, "the key outlived its failed metadata store")
}

func TestKMSService_GenerateKey_StoresDescriptionTagsOwner(t *testing.T) {
	mgr := newMetadataStoreKMSManager()
	svc := newKMSService(mgr, newMockLogger())
	ctx := systemTenantCtx()

	_, err := svc.GenerateKey(ctx, newMetadataGenerateRequest("meta-key"))
	require.NoError(t, err)

	got, err := svc.GetKeyMetadata(ctx, &GetKeyMetadataRequest{KeyID: "meta-key"})
	require.NoError(t, err)
	assert.Equal(t, "d", got.Metadata.Description)
	assert.Equal(t, map[string]string{"env": "prod"}, got.Metadata.Tags)
	assert.Equal(t, "svc-a", got.Metadata.Owner)
}

func TestKMSService_ImportKey_TypedMetadataPersists(t *testing.T) {
	mockManager := newMockKMSManager()
	mockManager.importKeyFn = func(_ context.Context, keyID string, _ []byte, algorithm string) (interface{}, error) {
		return &kms.KeyMetadata{KeyID: keyID, Name: keyID, Algorithm: algorithm, Version: 1}, nil
	}
	svc := newKMSService(mockManager, newMockLogger())

	resp, err := svc.ImportKey(systemTenantCtx(), &ImportKeyRequest{
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
