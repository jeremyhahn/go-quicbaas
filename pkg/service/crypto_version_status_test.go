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
	require.Equal(t, uint64(1), got.KeyVersion, "the response reports the version the ciphertext was sealed under")

	// Without a KeyVersion hint, a v2 ciphertext reports v2, not a default 1.
	got, err = cs.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: v2.Ciphertext})
	require.NoError(t, err)
	require.Equal(t, uint64(2), got.KeyVersion)

	// A matching hint is accepted; a conflicting one is refused, never echoed.
	got, err = cs.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: v2.Ciphertext, KeyVersion: 2})
	require.NoError(t, err)
	require.Equal(t, uint64(2), got.KeyVersion)
	for _, hint := range []uint64{1, 99} {
		_, err = cs.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: v2.Ciphertext, KeyVersion: hint})
		require.Error(t, err, "KeyVersion %d conflicts with the v2 ciphertext", hint)
		var hintErr *dberrors.QBError
		require.ErrorAs(t, err, &hintErr)
		require.Equal(t, dberrors.ErrInvalidInput, hintErr.Code)
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

// getFailingKVStore is a shared KVStore whose reads can be made to fail.
type getFailingKVStore struct {
	*mockKVStore
	getErr error
}

func (s *getFailingKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.mockKVStore.Get(ctx, key)
}

// Two service instances share replicated storage: a version node A disables
// is refused on node B at once, even when B cached it as enabled, and B
// refuses (fails closed) when the status cannot be read.
func TestCryptoDecrypt_VersionStatusAcrossInstances_RealXKMS(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	logger := newMockLogger()
	shared := &getFailingKVStore{mockKVStore: newMockKVStore()}
	kmsA := newKMSService(manager, logger)
	kmsA.SetKVStore(shared)
	kmsB := newKMSService(manager, logger)
	kmsB.SetKVStore(shared)
	csB := newCryptoService(crypto.NewMemoryCryptoManager(manager), manager, logger, kmsB.GetSafetyTracker())
	csB.kmsSvc = kmsB
	ctx := systemTenantCtx()

	const keyID = "vstatus-shared-key"
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
	scoped, err := scopeKeyID(ctx, keyID)
	require.NoError(t, err)

	// B records v1 as explicitly enabled and caches it.
	kmsB.setVersionStatus(scoped, 1, KeyStatusEnabled, nil)
	_, err = csB.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: v1.Ciphertext})
	require.NoError(t, err)

	// A disables v1: B refuses at once, despite its cached enabled status.
	_, err = kmsA.disableKeyVersion(ctx, scoped, 1)
	require.NoError(t, err)
	_, err = csB.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: v1.Ciphertext})
	var disabled *KeyVersionDisabledError
	require.ErrorAs(t, err, &disabled, "node B must see node A's disable")
	_, err = csB.DecryptBatch(ctx, &DecryptBatchRequest{KeyID: keyID, Items: []BatchDecryptItem{{Ciphertext: v1.Ciphertext}}})
	require.ErrorAs(t, err, &disabled)

	// A re-enables v1: B decrypts again.
	kmsA.setVersionStatus(scoped, 1, KeyStatusEnabled, nil)
	got, err := csB.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: v1.Ciphertext})
	require.NoError(t, err)
	require.Equal(t, []byte("sealed under v1"), got.Plaintext)

	// The status store fails: B refuses rather than treating it as enabled.
	shared.getErr = stderrors.New("storage unavailable")
	_, err = csB.Decrypt(ctx, &DecryptRequest{KeyID: keyID, Ciphertext: v1.Ciphertext})
	require.Error(t, err, "an unreadable version status must fail closed")
	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	require.Equal(t, dberrors.ErrStorage, qbErr.Code)
	_, err = csB.DecryptBatch(ctx, &DecryptBatchRequest{KeyID: keyID, Items: []BatchDecryptItem{{Ciphertext: v1.Ciphertext}}})
	require.Error(t, err)
}
