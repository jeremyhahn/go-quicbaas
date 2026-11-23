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

// TestXKMSBackend_PreRegistryVersionNamesAreNotAdopted: a key rotated before
// the registry has stored versions but no version record. Its ledger_v2 cannot
// be told apart from an independent key created under that name, so
// installing the registry adopts no version: ledger records version 1, its
// version-2 ciphertext is not opened with ledger_v2's material, ledger_v2
// stays an ordinary key, rotation is refused while the next version name is
// taken, and DeleteKey keeps ledger_v2.
func TestXKMSBackend_PreRegistryVersionNamesAreNotAdopted(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	ctx := context.Background()

	legacy := newKeyNameTestBackend(t)
	createKeyNameTestKey(t, legacy, "ledger")
	_, err := legacy.RotateKey(ctx, "ledger")
	require.NoError(t, err)
	v2Ciphertext, err := legacy.Encrypt(ctx, "ledger", []byte("sealed under v2"), nil)
	require.NoError(t, err)
	v, err := CiphertextKeyVersion(v2Ciphertext)
	require.NoError(t, err)
	require.Equal(t, uint64(2), v)

	registry := kvstore.NewMemory()
	upgraded := newRegistryTestBackend(t, registry)
	current, err := upgraded.CurrentVersion(ctx, "ledger")
	require.NoError(t, err)
	assert.Equal(t, 1, current, "no pre-registry version name is adopted")
	record, err := registry.Get(ctx, upgraded.versionRecordKey("ledger"))
	require.NoError(t, err)
	assert.Equal(t, "1", string(record), "the migrated version is recorded")

	meta, err := upgraded.GetKeyMetadata(ctx, "ledger")
	require.NoError(t, err)
	assert.Equal(t, 1, meta.Version)
	_, err = upgraded.Decrypt(ctx, "ledger", v2Ciphertext, nil)
	assert.Error(t, err, "a version-2 ciphertext under ledger is not opened with another key's material")

	_, err = upgraded.GetKeyMetadata(ctx, "ledger_v2")
	require.NoError(t, err, "the pre-registry ledger_v2 is an ordinary key")

	_, err = upgraded.RotateKey(ctx, "ledger")
	assert.ErrorIs(t, err, ErrVersionNameTaken, "rotation does not take over the existing ledger_v2")

	require.NoError(t, upgraded.DeleteKey(ctx, "ledger"))
	stored, err := upgraded.listStoredKeyIDs(ctx, "ledger")
	require.NoError(t, err)
	assert.Equal(t, map[string]struct{}{"ledger_v2": {}}, stored,
		"DeleteKey keeps the pre-registry ledger_v2 as an ordinary key")
}

// TestXKMSBackend_PreRegistryIndependentVersionNamedKeyIsPreserved: before
// the registry, report and report_v2 could be created as independent keys.
// The migration cannot tell report_v2 from a rotated version, so it stays
// addressable and survives the deletion of report.
func TestXKMSBackend_PreRegistryIndependentVersionNamedKeyIsPreserved(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	ctx := context.Background()

	legacy := newKeyNameTestBackend(t)
	createKeyNameTestKey(t, legacy, "report")
	createKeyNameTestKey(t, legacy, "report_v2")

	registry := kvstore.NewMemory()
	upgraded := newRegistryTestBackend(t, registry)
	_, err := upgraded.GetKeyMetadata(ctx, "report")
	require.NoError(t, err)
	_, err = upgraded.GetKeyMetadata(ctx, "report_v2")
	require.NoError(t, err, "a pre-registry key named like a version stays addressable")
	assertKeyRoundTrips(t, upgraded, "report_v2")
	ct, err := upgraded.Encrypt(ctx, "report", []byte("p"), nil)
	require.NoError(t, err)
	v, err := CiphertextKeyVersion(ct)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), v, "report keeps encrypting under its own material")
	plaintext, err := newRegistryTestBackend(t, registry).Decrypt(ctx, "report", ct, nil)
	require.NoError(t, err)
	assert.Equal(t, []byte("p"), plaintext)

	require.NoError(t, upgraded.DeleteKey(ctx, "report"))

	fresh := newRegistryTestBackend(t, registry)
	_, err = fresh.GetKeyMetadata(ctx, "report")
	assert.ErrorIs(t, err, ErrKeyNotFound)
	_, err = fresh.GetKeyMetadata(ctx, "report_v2")
	require.NoError(t, err, "deleting report keeps the independent report_v2")
	assertKeyRoundTrips(t, fresh, "report_v2")
}

// TestXKMSBackend_MissingKeyWritesNoVersionRecord: a version lookup of a key
// that is not stored reports version 1 and records nothing for it.
func TestXKMSBackend_MissingKeyWritesNoVersionRecord(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	ctx := context.Background()

	registry := kvstore.NewMemory()
	backend := newRegistryTestBackend(t, registry)
	version, err := backend.storedVersion(ctx, "absent")
	require.NoError(t, err)
	assert.Equal(t, 1, version)
	exists, err := registry.Exists(ctx, backend.versionRecordKey("absent"))
	require.NoError(t, err)
	assert.False(t, exists)
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
