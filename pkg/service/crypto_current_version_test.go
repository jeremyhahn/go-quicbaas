package service

import (
	"context"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// TestCryptoService_CurrentVersionGate_MissingKeyIsNotFound checks the
// encryption and data-key gates, with the KMS service configured, report a
// key that does not exist as NotFound, not as Unavailable: a client must not
// retry a permanent absence as an outage.
func TestCryptoService_CurrentVersionGate_MissingKeyIsNotFound(t *testing.T) {
	f := newVersionedCryptoFixture(t)
	ctx := systemTenantCtx()
	const missing = "no-such-key"
	// What the production manager (kms.KMSService.GetKey) returns for a key
	// absent from the caller's scope.
	f.kmsMgr.getKeyFn = func(_ context.Context, keyID string) (interface{}, error) {
		return nil, &errors.QBError{Code: errors.ErrNotFound, Op: "KMSService.GetKey", Err: kms.ErrKeyNotFound}
	}

	_, err := f.svc.Encrypt(ctx, &EncryptRequest{KeyID: missing, Plaintext: []byte("p")})
	require.Error(t, err)
	require.True(t, errors.IsNotFound(err), "Encrypt: want NotFound, got %v", err)

	_, err = f.svc.GenerateDataKey(ctx, &GenerateDataKeyRequest{KeyID: missing})
	require.Error(t, err)
	require.True(t, errors.IsNotFound(err), "GenerateDataKey: want NotFound, got %v", err)
}

// TestCryptoService_Encrypt_WarmNodeAfterRotationOnAnotherNode warms node A's
// metadata caches at v1, rotates the key to v2 on node B and disables v1 (the
// state v1's grace-period job leaves), and checks node A still encrypts: its
// gate reads the current version from the durable record, not the v1 its
// caches hold.
func TestCryptoService_Encrypt_WarmNodeAfterRotationOnAnotherNode(t *testing.T) {
	managerA, cleanup := realKMSManagerForTest(t)
	defer cleanup()
	ctx := systemTenantCtx()
	shared := kvstore.NewMemory()

	backendB, err := kms.NewXKMSBackend(&kms.XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      newMockLogger(),
	})
	require.NoError(t, err)
	registryB := kms.NewBackendRegistry(nil)
	require.NoError(t, registryB.Register("software", backendB, 100))
	managerB, err := kms.NewKMSService(&kms.KMSServiceOptions{Registry: registryB})
	require.NoError(t, err)

	logger := newMockLogger()
	nodeA := newKMSService(managerA, logger)
	nodeA.SetKVStore(shared)
	nodeB := newKMSService(managerB, logger)
	nodeB.SetKVStore(shared)

	const keyID = "warm-rotate"
	scoped, err := scopeKeyID(ctx, keyID)
	require.NoError(t, err)
	_, err = managerA.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 scoped,
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	svcA := newCryptoService(newMockCryptoManager(), managerA, logger, nil)
	svcA.kmsSvc = nodeA
	_, err = svcA.Encrypt(ctx, &EncryptRequest{KeyID: keyID, Plaintext: []byte("before")})
	require.NoError(t, err)
	_, err = managerA.GetKey(ctx, scoped)
	require.NoError(t, err, "node A's caches hold v1")

	_, err = managerB.GetKey(ctx, scoped)
	require.NoError(t, err)
	rotated, err := managerB.RotateKey(ctx, scoped)
	require.NoError(t, err)
	require.Equal(t, 2, rotated.(*kms.KeyMetadata).Version)
	nodeB.setVersionStatus(scoped, 1, KeyStatusDisabled, nil)

	_, err = svcA.Encrypt(ctx, &EncryptRequest{KeyID: keyID, Plaintext: []byte("after")})
	require.NoError(t, err, "current v2 is enabled: node A must not refuse on its stale v1")
	_, err = svcA.GenerateDataKey(ctx, &GenerateDataKeyRequest{KeyID: keyID})
	require.NoError(t, err)

	meta, err := managerA.GetKey(ctx, scoped)
	require.NoError(t, err)
	require.Equal(t, 2, meta.(*kms.KeyMetadata).Version, "node A's cached metadata is refreshed")
}

// TestCryptoService_Encrypt_BudgetResetsAfterRotationOnAnotherNode: node A
// exhausts a key's AEAD budget and node B rotates the key. Node A's next
// encrypting operation reads the new current version and starts its budget
// over, so it is no longer refused.
func TestCryptoService_Encrypt_BudgetResetsAfterRotationOnAnotherNode(t *testing.T) {
	managerA, cleanup := realKMSManagerForTest(t)
	defer cleanup()
	ctx := systemTenantCtx()
	shared := kvstore.NewMemory()

	backendB, err := kms.NewXKMSBackend(&kms.XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      newMockLogger(),
	})
	require.NoError(t, err)
	registryB := kms.NewBackendRegistry(nil)
	require.NoError(t, registryB.Register("software", backendB, 100))
	managerB, err := kms.NewKMSService(&kms.KMSServiceOptions{Registry: registryB})
	require.NoError(t, err)

	logger := newMockLogger()
	nodeA := newKMSService(managerA, logger)
	nodeA.SetKVStore(shared)
	nodeB := newKMSService(managerB, logger)
	nodeB.SetKVStore(shared)

	const keyID = "budget-rotate"
	scoped, err := scopeKeyID(ctx, keyID)
	require.NoError(t, err)
	_, err = managerA.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 scoped,
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	trackerA := NewAEADSafetyTracker()
	svcA := newCryptoService(newMockCryptoManager(), managerA, logger, trackerA)
	svcA.kmsSvc = nodeA
	trackerA.SetKeyConfig(scoped, AEADSafetyConfig{MaxOperations: 3})

	for i := 0; i < 2; i++ {
		_, err = svcA.Encrypt(ctx, &EncryptRequest{KeyID: keyID, Plaintext: []byte("before")})
		require.NoError(t, err)
	}
	_, err = svcA.Encrypt(ctx, &EncryptRequest{KeyID: keyID, Plaintext: []byte("before")})
	require.Error(t, err, "budget exhausted on node A")

	_, err = managerB.GetKey(ctx, scoped)
	require.NoError(t, err)
	rotated, err := managerB.RotateKey(ctx, scoped)
	require.NoError(t, err)
	require.Equal(t, 2, rotated.(*kms.KeyMetadata).Version)
	nodeB.setVersionStatus(scoped, 1, KeyStatusDisabled, nil)

	_, err = svcA.Encrypt(ctx, &EncryptRequest{KeyID: keyID, Plaintext: []byte("after")})
	require.NoError(t, err, "node A starts the v2 budget")
	_, err = svcA.GenerateDataKey(ctx, &GenerateDataKeyRequest{KeyID: keyID})
	require.NoError(t, err)

	ops, _, _ := trackerA.GetKeyUsage(scoped)
	require.Equal(t, uint64(2), ops, "node A counts the v2 operations only")
}
