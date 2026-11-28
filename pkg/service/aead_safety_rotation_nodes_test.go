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
	"testing"

	"github.com/stretchr/testify/require"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/crypto"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// kmsManagerOverInitializedXKMSForTest builds a key manager with its own
// XKMSBackend over the go-xkms store a caller already initialized, so two
// managers model two nodes with separate caches over one key store.
func kmsManagerOverInitializedXKMSForTest(t *testing.T) *kms.KMSService {
	t.Helper()

	backend, err := kms.NewXKMSBackend(&kms.XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      newMockLogger(),
	})
	require.NoError(t, err)

	registry := kms.NewBackendRegistry(nil)
	require.NoError(t, registry.Register("software", backend, 100))

	manager, err := kms.NewKMSService(&kms.KMSServiceOptions{Registry: registry})
	require.NoError(t, err)
	return manager
}

// TestAEADSafety_RotationOnOtherNodeRestartsUsage_RealXKMS runs two service
// instances, each with its own AEAD tracker and its own key manager and
// backend, over one go-xkms store and one replicated store. Node B exhausts
// the key's allowance; node A rotates the key. Node B must seal under the new
// version, count against it and accept crypto calls again, without any reset
// on its own tracker.
func TestAEADSafety_RotationOnOtherNodeRestartsUsage_RealXKMS(t *testing.T) {
	_, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	shared := newMockKVStore()
	newNode := func() (*kmsServiceImpl, *cryptoService) {
		manager := kmsManagerOverInitializedXKMSForTest(t)
		logger := newMockLogger()
		kmsSvc := newKMSService(manager, logger)
		kmsSvc.SetKVStore(shared)
		cs := newCryptoService(crypto.NewMemoryCryptoManager(manager), manager, logger, kmsSvc.GetSafetyTracker())
		cs.kmsSvc = kmsSvc
		return kmsSvc, cs
	}
	kmsA, _ := newNode()
	kmsB, csB := newNode()
	require.NotSame(t, kmsA.GetSafetyTracker(), kmsB.GetSafetyTracker())
	ctx := systemTenantCtx()

	const keyID = "aead-rotate-nodes-key"
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
	scoped, err := scopeKeyID(ctx, keyID)
	require.NoError(t, err)

	const maxOps = 3
	limits := AEADSafetyConfig{MaxOperations: maxOps, MaxBytes: 1 << 30}
	kmsA.GetSafetyTracker().SetKeyConfig(scoped, limits)
	kmsB.GetSafetyTracker().SetKeyConfig(scoped, limits)

	var lastCiphertext []byte
	encrypt := func() error {
		resp, err := csB.Encrypt(ctx, &EncryptRequest{KeyID: keyID, Plaintext: []byte("payload")})
		if err == nil {
			lastCiphertext = resp.Ciphertext
		}
		return err
	}

	// Node B exhausts v1's allowance.
	var exhausted bool
	for i := 0; i < maxOps+1; i++ {
		if err := encrypt(); err != nil {
			exhausted = true
			break
		}
	}
	require.True(t, exhausted, "node B must reach the key's operation limit")
	require.Error(t, encrypt(), "node B stays refused while v1 is current")

	// Node A rotates; node B counts against v2 from zero.
	rotated, err := kmsA.RotateKey(ctx, &RotateKeyRequest{KeyID: keyID})
	require.NoError(t, err)
	require.Greater(t, rotated.NewVersion, rotated.OldVersion)

	require.NoError(t, encrypt(), "node B must accept calls once another node rotated the key")
	sealed, ok := kms.CiphertextKeyVersion(lastCiphertext)
	require.True(t, ok)
	require.Equal(t, 2, sealed, "node B's backend must seal under the version node A created")
	ops, _, _ := kmsB.GetSafetyTracker().GetKeyUsage(scoped)
	require.Equal(t, uint64(1), ops, "node B's count must restart at the new version")
}
