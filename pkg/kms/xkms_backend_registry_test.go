package kms

import (
	"context"
	"fmt"
	"sync"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

func newRegistryTestBackend(t *testing.T, registry interfaces.KVStore) *XKMSBackend {
	t.Helper()
	b := newKeyNameTestBackend(t)
	b.SetKeyRegistry(registry)
	return b
}

// A system key stored under the literal name that a tenant key ID encodes to
// is not that tenant's key: only a key whose owner record names the tenant key
// ID resolves for it.
func TestXKMSBackend_LegacyEncodedNameNotResolvedForTenant(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	ctx := context.Background()

	backend := newRegistryTestBackend(t, kvstore.NewMemory())
	const tenantKeyID = "tenant/alice/k1"
	literal, err := xkmsKeyName(tenantKeyID)
	require.NoError(t, err)

	// The legacy key, stored directly through the xkms service under the
	// literal encoded name, without any owner record.
	createRawKeyNameTestKey(t, backend, tenantKeyID)

	_, err = backend.GetKeyMetadata(ctx, tenantKeyID)
	assert.ErrorIs(t, err, ErrKeyNotFound)
	_, err = backend.Encrypt(ctx, tenantKeyID, []byte("p"), nil)
	assert.ErrorIs(t, err, ErrKeyNotFound)

	_, err = backend.CreateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 tenantKeyID,
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	assert.Error(t, err, "a create colliding with an unowned stored name must be refused")
	_, err = backend.GetKeyMetadata(ctx, tenantKeyID)
	assert.ErrorIs(t, err, ErrKeyNotFound)

	// The legacy key stays reachable by its literal system name.
	kid := fmt.Sprintf("%s:encryption:aes256-gcm:%s", config.BackendTypeSoftware, literal)
	_, err = dbxkms.GetSymmetricKey(ctx, kid)
	assert.NoError(t, err)
}

// Deleting a key never deletes an independent key named like one of its
// versions: only versions recorded at rotation are deleted.
func TestXKMSBackend_DeleteKeepsIndependentVersionNamedKey(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	ctx := context.Background()

	registry := kvstore.NewMemory()
	backend := newRegistryTestBackend(t, registry)
	createKeyNameTestKey(t, backend, "report")
	createKeyNameTestKey(t, backend, "report_v2")

	require.NoError(t, backend.DeleteKey(ctx, "report"))

	_, err := backend.GetKeyMetadata(ctx, "report_v2")
	require.NoError(t, err, "the independent key must survive the delete")
	assertKeyRoundTrips(t, backend, "report_v2")

	fresh := newRegistryTestBackend(t, registry)
	_, err = fresh.GetKeyMetadata(ctx, "report")
	assert.ErrorIs(t, err, ErrKeyNotFound)
	meta, err := fresh.GetKeyMetadata(ctx, "report_v2")
	require.NoError(t, err)
	assert.Equal(t, 1, meta.Version)
	assertKeyRoundTrips(t, fresh, "report_v2")
}

// Decrypt reads the cached version while RotateKey changes it.
func TestXKMSBackend_DecryptConcurrentRotate_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	ctx := context.Background()

	backend := newRegistryTestBackend(t, kvstore.NewMemory())
	const keyID = "rot-dec-race"
	createKeyNameTestKey(t, backend, keyID)

	const (
		decrypters = 4
		perWorker  = 15
		rotations  = 8
	)
	plaintext := []byte("sealed before rotation")
	blob, err := backend.Encrypt(ctx, keyID, plaintext, nil)
	require.NoError(t, err)

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for w := 0; w < decrypters; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				got, decErr := backend.Decrypt(ctx, keyID, blob, nil)
				mu.Lock()
				if decErr != nil {
					errs = append(errs, decErr)
				} else if string(got) != string(plaintext) {
					errs = append(errs, fmt.Errorf("plaintext mismatch: %q", got))
				}
				mu.Unlock()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for r := 0; r < rotations; r++ {
			if _, rotErr := backend.RotateKey(ctx, keyID); rotErr != nil {
				mu.Lock()
				errs = append(errs, rotErr)
				mu.Unlock()
			}
		}
	}()
	wg.Wait()
	require.Empty(t, errs)
}
