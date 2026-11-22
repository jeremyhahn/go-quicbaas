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

	"github.com/stretchr/testify/require"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/crypto"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TestCryptoDecrypt_DisabledKeyVersionRefused_RealXKMS checks that, on the
// real XKMS backend, Decrypt and DecryptBatch refuse a ciphertext sealed under
// a key version the grace job has disabled, while the current version still
// decrypts.
func TestCryptoDecrypt_DisabledKeyVersionRefused_RealXKMS(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	logger := newMockLogger()
	kmsSvc := newKMSService(manager, logger)
	cs := newCryptoService(crypto.NewMemoryCryptoManager(manager), manager, logger, kmsSvc.GetSafetyTracker())
	cs.kmsSvc = kmsSvc
	ctx := systemTenantCtx()

	const keyID = "vstatus-key"
	_, err := kmsSvc.GenerateKey(ctx, &GenerateKeyRequest{
		Name:  keyID,
		Usage: []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
		KeyAttributes: &xkmssdk.KeyAttributes{
			StoreType:          xkmssdk.StoreType("software"),
			KeyType:            xkmssdk.KeyTypeEncryption,
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
	})
	require.NoError(t, err)

	v1, err := cs.Encrypt(ctx, &EncryptRequest{KeyID: keyID, Plaintext: []byte("sealed under v1")})
	require.NoError(t, err)

	rot, err := kmsSvc.RotateKey(ctx, &RotateKeyRequest{KeyID: keyID})
	require.NoError(t, err)
	require.Equal(t, uint64(1), rot.OldVersion)
	require.Equal(t, uint64(2), rot.NewVersion)

	v2, err := cs.Encrypt(ctx, &EncryptRequest{KeyID: keyID, Plaintext: []byte("sealed under v2")})
	require.NoError(t, err)

	// Grace window: v1 still decrypts before it is disabled.
	got, err := cs.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: v1.Ciphertext})
	require.NoError(t, err)
	require.Equal(t, []byte("sealed under v1"), got.Plaintext)
	require.Equal(t, uint64(1), got.KeyVersion, "the response reports the ciphertext's version")

	// The response reports the version the ciphertext was sealed under, not
	// the request's hint; a hint that contradicts it is refused.
	got, err = cs.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: v2.Ciphertext})
	require.NoError(t, err)
	require.Equal(t, uint64(2), got.KeyVersion)
	got, err = cs.Decrypt(ctx, &DecryptRequest{KeyID: keyID, KeyVersion: 2, Ciphertext: v2.Ciphertext})
	require.NoError(t, err)
	require.Equal(t, uint64(2), got.KeyVersion)
	for _, wrong := range []uint64{1, 99} {
		_, err = cs.Decrypt(ctx, &DecryptRequest{KeyID: keyID, KeyVersion: wrong, Ciphertext: v2.Ciphertext})
		require.Error(t, err, "key_version %d contradicts the v2 ciphertext", wrong)
		var qbErr *dberrors.QBError
		require.ErrorAs(t, err, &qbErr)
		require.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
	}

	// Disable v1 the way the grace job does.
	scoped, err := scopeKeyID(ctx, keyID)
	require.NoError(t, err)
	_, err = kmsSvc.disableKeyVersion(ctx, scoped, 1)
	require.NoError(t, err)

	// Single decrypt.
	_, err = cs.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: v1.Ciphertext})
	require.Error(t, err, "a v1 ciphertext must not decrypt once v1 is disabled")
	var disabled *KeyVersionDisabledError
	require.ErrorAs(t, err, &disabled)
	require.Equal(t, uint64(1), disabled.Version)
	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	require.Equal(t, dberrors.ErrPreconditionFailed, qbErr.Code)

	got, err = cs.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: v2.Ciphertext})
	require.NoError(t, err)
	require.Equal(t, []byte("sealed under v2"), got.Plaintext)

	// Batch decrypt: one disabled item refuses the whole batch.
	_, err = cs.DecryptBatch(ctx, &DecryptBatchRequest{KeyID: keyID, Items: []BatchDecryptItem{
		{Ciphertext: v2.Ciphertext},
		{Ciphertext: v1.Ciphertext},
	}})
	require.Error(t, err)
	disabled = nil
	require.ErrorAs(t, err, &disabled)
	require.Equal(t, uint64(1), disabled.Version)

	batch, err := cs.DecryptBatch(ctx, &DecryptBatchRequest{KeyID: keyID, Items: []BatchDecryptItem{
		{Ciphertext: v2.Ciphertext},
	}})
	require.NoError(t, err)
	require.Len(t, batch.Results, 1)
	require.Equal(t, []byte("sealed under v2"), batch.Results[0].Plaintext)
}

// getFailingKVStore is a shared KVStore whose Get can be made to fail.
type getFailingKVStore struct {
	*mockKVStore
	failGet bool
	failPut bool
}

func (s *getFailingKVStore) Put(ctx context.Context, key string, value []byte) error {
	if s.failPut {
		return stderrors.New("kv store unavailable")
	}
	return s.mockKVStore.Put(ctx, key, value)
}

func (s *getFailingKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	if s.failGet {
		return nil, stderrors.New("kv store unavailable")
	}
	return s.mockKVStore.Get(ctx, key)
}

// TestCryptoDecrypt_VersionStatusAcrossNodes_RealXKMS runs two service
// instances over one key manager and one replicated KVStore. A version node A
// disables after node B cached it as enabled must be refused on node B, and a
// status lookup that fails must refuse the decrypt.
func TestCryptoDecrypt_VersionStatusAcrossNodes_RealXKMS(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	shared := &getFailingKVStore{mockKVStore: newMockKVStore()}
	newNode := func() (*kmsServiceImpl, *cryptoService) {
		logger := newMockLogger()
		kmsSvc := newKMSService(manager, logger)
		kmsSvc.SetKVStore(shared)
		cs := newCryptoService(crypto.NewMemoryCryptoManager(manager), manager, logger, kmsSvc.GetSafetyTracker())
		cs.kmsSvc = kmsSvc
		return kmsSvc, cs
	}
	kmsA, _ := newNode()
	kmsB, csB := newNode()
	ctx := systemTenantCtx()

	const keyID = "vstatus-nodes-key"
	_, err := kmsA.GenerateKey(ctx, &GenerateKeyRequest{
		Name:  keyID,
		Usage: []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
		KeyAttributes: &xkmssdk.KeyAttributes{
			StoreType:          xkmssdk.StoreType("software"),
			KeyType:            xkmssdk.KeyTypeEncryption,
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
	})
	require.NoError(t, err)
	v1, err := csB.Encrypt(ctx, &EncryptRequest{KeyID: keyID, Plaintext: []byte("sealed under v1")})
	require.NoError(t, err)
	_, err = kmsA.RotateKey(ctx, &RotateKeyRequest{KeyID: keyID})
	require.NoError(t, err)

	scoped, err := scopeKeyID(ctx, keyID)
	require.NoError(t, err)

	// Node B explicitly enables v1 and caches that status.
	kmsB.setVersionStatus(scoped, 1, KeyStatusEnabled, nil)
	status, _ := kmsB.getVersionStatus(ctx, scoped, 1)
	require.Equal(t, KeyStatusEnabled, status)
	got, err := csB.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: v1.Ciphertext})
	require.NoError(t, err)
	require.Equal(t, []byte("sealed under v1"), got.Plaintext)

	// Node A disables v1; node B's stale cache must not let it decrypt.
	_, err = kmsA.disableKeyVersion(ctx, scoped, 1)
	require.NoError(t, err)
	_, err = csB.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: v1.Ciphertext})
	var disabled *KeyVersionDisabledError
	require.ErrorAs(t, err, &disabled, "node B must see node A's disable")
	_, err = csB.DecryptBatch(ctx, &DecryptBatchRequest{KeyID: keyID, Items: []BatchDecryptItem{{Ciphertext: v1.Ciphertext}}})
	require.ErrorAs(t, err, &disabled)

	// Node A re-enables v1; node B follows.
	kmsA.setVersionStatus(scoped, 1, KeyStatusEnabled, nil)
	_, err = csB.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: v1.Ciphertext})
	require.NoError(t, err)

	// A status lookup that fails refuses the decrypt (fail closed).
	shared.failGet = true
	_, err = csB.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: v1.Ciphertext})
	require.Error(t, err, "a failed status lookup must refuse the decrypt")
	require.NotErrorAs(t, err, &disabled)
	_, err = csB.DecryptBatch(ctx, &DecryptBatchRequest{KeyID: keyID, Items: []BatchDecryptItem{{Ciphertext: v1.Ciphertext}}})
	require.Error(t, err)
	shared.failGet = false
}

// TestCryptoDecrypt_FailedDisableWriteStaysDisabled_RealXKMS persists an
// enabled record for v1, then makes the disable write fail. The disable must
// report failure, the node must still refuse v1 ciphertexts rather than trust
// the stale enabled record, and a retry once storage recovers must persist
// the disable for every node.
func TestCryptoDecrypt_FailedDisableWriteStaysDisabled_RealXKMS(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	shared := &getFailingKVStore{mockKVStore: newMockKVStore()}
	newNode := func() (*kmsServiceImpl, *cryptoService) {
		logger := newMockLogger()
		kmsSvc := newKMSService(manager, logger)
		kmsSvc.SetKVStore(shared)
		cs := newCryptoService(crypto.NewMemoryCryptoManager(manager), manager, logger, kmsSvc.GetSafetyTracker())
		cs.kmsSvc = kmsSvc
		return kmsSvc, cs
	}
	kmsA, csA := newNode()
	_, csB := newNode()
	ctx := systemTenantCtx()

	const keyID = "vstatus-putfail-key"
	_, err := kmsA.GenerateKey(ctx, &GenerateKeyRequest{
		Name:  keyID,
		Usage: []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
		KeyAttributes: &xkmssdk.KeyAttributes{
			StoreType:          xkmssdk.StoreType("software"),
			KeyType:            xkmssdk.KeyTypeEncryption,
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
	})
	require.NoError(t, err)
	v1, err := csA.Encrypt(ctx, &EncryptRequest{KeyID: keyID, Plaintext: []byte("sealed under v1")})
	require.NoError(t, err)
	_, err = kmsA.RotateKey(ctx, &RotateKeyRequest{KeyID: keyID})
	require.NoError(t, err)
	scoped, err := scopeKeyID(ctx, keyID)
	require.NoError(t, err)

	// A persisted enabled record for v1.
	require.NoError(t, kmsA.setVersionStatus(scoped, 1, KeyStatusEnabled, nil))
	_, err = csA.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: v1.Ciphertext})
	require.NoError(t, err)

	// The disable write fails: both entry points report it.
	shared.failPut = true
	_, err = kmsA.disableKeyVersion(ctx, scoped, 1)
	require.Error(t, err, "a disable that was not persisted must not report success")
	_, err = kmsA.UpdateKeyStatus(ctx, &UpdateKeyStatusRequest{KeyID: keyID, Version: 1, Status: KeyStatusDisabled})
	require.Error(t, err, "UpdateKeyStatus must not report a disable it could not persist")

	// This node refuses v1 even though storage still says enabled.
	var disabled *KeyVersionDisabledError
	_, err = csA.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: v1.Ciphertext})
	require.ErrorAs(t, err, &disabled, "the stale enabled record must not release plaintext")
	_, err = csA.DecryptBatch(ctx, &DecryptBatchRequest{KeyID: keyID, Items: []BatchDecryptItem{{Ciphertext: v1.Ciphertext}}})
	require.ErrorAs(t, err, &disabled)

	// Storage recovers: the retried disable persists and every node sees it.
	shared.failPut = false
	_, err = kmsA.disableKeyVersion(ctx, scoped, 1)
	require.NoError(t, err)
	require.False(t, kmsA.versionStatusUnpersisted(scoped, 1))
	_, err = csB.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: v1.Ciphertext})
	require.ErrorAs(t, err, &disabled, "the retried disable must reach the other node")
}
