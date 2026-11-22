package service

import (
	"context"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// TestSetKVStore_WiresKeyRegistryIntoManagerBackends builds the KMS service
// the way newQuicBaaS does (newKMSService over the production *kms.KMSService
// manager, then SetKVStore, with no SetBackendRegistry call) over a real
// XKMSBackend, and checks the durable key registry reaches that backend:
// deleting "report" must leave an independent key "report_v2" intact, which
// only holds when the backend records versions in the registry instead of
// inferring them from names.
func TestSetKVStore_WiresKeyRegistryIntoManagerBackends(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()
	ctx := context.Background()

	kv := kvstore.NewMemory()
	svc := newKMSService(manager, newMockLogger())
	svc.SetKVStore(kv)
	require.NotNil(t, svc.keyBackendRegistry(), "the manager's registry must be found")

	for _, name := range []string{"report", "report_v2"} {
		_, err := manager.GenerateKey(ctx, &xkmssdk.KeyAttributes{
			CN:                 name,
			StoreType:          xkmssdk.StoreType("software"),
			KeyType:            xkmssdk.KeyTypeEncryption,
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		})
		require.NoError(t, err)
	}

	require.NoError(t, manager.DeleteKey(ctx, "report"))

	// A fresh backend over the same registry, with an empty metadata cache.
	fresh, err := kms.NewXKMSBackend(&kms.XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      newMockLogger(),
	})
	require.NoError(t, err)
	fresh.SetKeyRegistry(kv)

	_, err = fresh.GetKeyMetadata(ctx, "report")
	assert.ErrorIs(t, err, kms.ErrKeyNotFound)
	meta, err := fresh.GetKeyMetadata(ctx, "report_v2")
	require.NoError(t, err, "the independent key must survive the delete")
	assert.Equal(t, 1, meta.Version)

	blob, err := fresh.Encrypt(ctx, "report_v2", []byte("p"), nil)
	require.NoError(t, err)
	plain, err := fresh.Decrypt(ctx, "report_v2", blob, nil)
	require.NoError(t, err)
	assert.Equal(t, []byte("p"), plain)
}
