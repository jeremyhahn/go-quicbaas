package kms

import (
	"context"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

func newKeyNameTestBackend(t *testing.T) *XKMSBackend {
	t.Helper()
	b, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)
	return b
}

func createKeyNameTestKey(t *testing.T, b *XKMSBackend, keyID string) {
	t.Helper()
	_, err := b.CreateKey(context.Background(), &xkmssdk.KeyAttributes{
		CN:                 keyID,
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)
}

// TestXKMSBackend_VersionNameNotAKeyID rotates a key and checks that the name
// go-xkms stores its new version under (<key>_v2) is not addressable as a
// key ID, for a system key and for a tenant-scoped key, from the backend that
// rotated it and from a fresh backend with an empty metadata cache.
func TestXKMSBackend_VersionNameNotAKeyID(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	ctx := context.Background()

	for _, keyID := range []string{"vn-sys-k", "tenant/vn-alice/k"} {
		t.Run(keyID, func(t *testing.T) {
			backend := newKeyNameTestBackend(t)
			createKeyNameTestKey(t, backend, keyID)
			_, err := backend.RotateKey(ctx, keyID)
			require.NoError(t, err)

			ciphertext, err := backend.Encrypt(ctx, keyID, []byte("p"), nil)
			require.NoError(t, err)

			version := keyID + "_v2"
			for name, b := range map[string]*XKMSBackend{"same": backend, "fresh": newKeyNameTestBackend(t)} {
				t.Run(name, func(t *testing.T) {
					_, err := b.Encrypt(ctx, version, []byte("p"), nil)
					require.ErrorIs(t, err, ErrKeyNotFound)
					_, err = b.Decrypt(ctx, version, ciphertext, nil)
					require.ErrorIs(t, err, ErrKeyNotFound)
					_, err = b.GetKeyMetadata(ctx, version)
					require.ErrorIs(t, err, ErrKeyNotFound)
					_, err = b.RotateKey(ctx, version)
					require.ErrorIs(t, err, ErrKeyNotFound)
					require.ErrorIs(t, b.DisableKey(ctx, version), ErrKeyNotFound)
					require.ErrorIs(t, b.EnableKey(ctx, version), ErrKeyNotFound)
					require.ErrorIs(t, b.DeleteKey(ctx, version), ErrKeyNotFound)
				})
			}

			_, err = backend.CreateKey(ctx, &xkmssdk.KeyAttributes{
				CN:                 version,
				StoreType:          xkmssdk.StoreType("software"),
				KeyType:            xkmssdk.KeyTypeEncryption,
				SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
			})
			require.ErrorIs(t, err, ErrInvalidKeyID)

			plaintext, err := backend.Decrypt(ctx, keyID, ciphertext, nil)
			require.NoError(t, err)
			assert.Equal(t, []byte("p"), plaintext)
		})
	}
}

// TestXKMSBackend_VersionLikeNameWithoutBaseIsAKey checks that a key whose ID
// ends _v<digits> but whose base does not exist is an ordinary key.
func TestXKMSBackend_VersionLikeNameWithoutBaseIsAKey(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	ctx := context.Background()

	backend := newKeyNameTestBackend(t)
	const keyID = "vn-x_v2"
	createKeyNameTestKey(t, backend, keyID)

	ciphertext, err := backend.Encrypt(ctx, keyID, []byte("p"), nil)
	require.NoError(t, err)
	plaintext, err := backend.Decrypt(ctx, keyID, ciphertext, nil)
	require.NoError(t, err)
	assert.Equal(t, []byte("p"), plaintext)

	_, err = backend.RotateKey(ctx, keyID)
	require.NoError(t, err)
	_, err = newKeyNameTestBackend(t).GetKeyMetadata(ctx, keyID)
	require.NoError(t, err)
	require.NoError(t, backend.DeleteKey(ctx, keyID))
}

// TestXKMSBackend_VersionNameLookupErrorFailsClosed checks that a metadata
// lookup of the base that fails with an error other than ErrKeyNotFound makes
// refuseVersionName return that error, not treat keyID as an ordinary key.
func TestXKMSBackend_VersionNameLookupErrorFailsClosed(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newKeyNameTestBackend(t)
	// With xkms reset, every metadata lookup fails with ErrBackendNotReady.
	dbxkms.Reset()

	err := backend.refuseVersionName(context.Background(), "vn-fc_v2")
	require.ErrorIs(t, err, ErrBackendNotReady)
	assert.NotErrorIs(t, err, ErrKeyNotFound)
}

func TestVersionNameBase(t *testing.T) {
	tests := []struct {
		keyID string
		base  string
		ok    bool
	}{
		{"k_v2", "k", true},
		{"t/a/k_v10", "t/a/k", true},
		{"k_v", "", false},
		{"k_vx", "", false},
		{"k_v2a", "", false},
		{"_v2", "", false},
		{"t/_v2", "", false},
		{"k", "", false},
	}
	for _, tc := range tests {
		base, ok := versionNameBase(tc.keyID)
		assert.Equal(t, tc.ok, ok, tc.keyID)
		assert.Equal(t, tc.base, base, tc.keyID)
	}
}

// TestXKMSBackend_LegacyQBTNames checks that "/"-free system keys whose names
// start with the encoded-name prefix stay usable under their own names, that
// a system ID equal to the encoding of a tenant-scoped ID is refused, and
// that tenant keys still round-trip through ListKeys.
func TestXKMSBackend_LegacyQBTNames(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	ctx := context.Background()

	listed := func(b *XKMSBackend) []string {
		keys, _, err := b.ListKeys(ctx, "", 0)
		require.NoError(t, err)
		ids := make([]string, 0, len(keys))
		for _, k := range keys {
			ids = append(ids, k.KeyID)
		}
		return ids
	}

	backend := newKeyNameTestBackend(t)
	const tenantKey = "t1/legacy-k"
	createKeyNameTestKey(t, backend, tenantKey)

	for _, keyID := range []string{"qbt.legacy", "qbt.6162"} {
		t.Run(keyID, func(t *testing.T) {
			createKeyNameTestKey(t, backend, keyID)
			ciphertext, err := backend.Encrypt(ctx, keyID, []byte("p"), nil)
			require.NoError(t, err)

			fresh := newKeyNameTestBackend(t)
			assert.Contains(t, listed(fresh), keyID)
			plaintext, err := fresh.Decrypt(ctx, keyID, ciphertext, nil)
			require.NoError(t, err)
			assert.Equal(t, []byte("p"), plaintext)

			require.NoError(t, backend.DeleteKey(ctx, keyID))
			assert.NotContains(t, listed(newKeyNameTestBackend(t)), keyID)
		})
	}

	encoded, err := xkmsKeyName("t1/k")
	require.NoError(t, err)
	_, err = backend.CreateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 encoded,
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.ErrorIs(t, err, ErrInvalidKeyID)

	keys, _, err := newKeyNameTestBackend(t).ListKeys(ctx, "t1/", 0)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, tenantKey, keys[0].KeyID)
	require.NoError(t, backend.DeleteKey(ctx, tenantKey))
}

// Deleting a rotated key deletes its version keys too: none is left behind
// to surface as an ordinary key once the base is gone.
func TestXKMSBackend_DeleteKeyDeletesVersions(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	ctx := context.Background()

	backend := newKeyNameTestBackend(t)
	const keyID = "dv-k"
	createKeyNameTestKey(t, backend, keyID)

	_, err := backend.RotateKey(ctx, keyID)
	require.NoError(t, err)
	// A ciphertext under the version-2 key, relabelled as version 1 so that
	// a Decrypt on a key named keyID_v2 uses that key's own material.
	ciphertext, err := backend.Encrypt(ctx, keyID, []byte("p"), nil)
	require.NoError(t, err)
	encrypted, err := backend.deserializeEncryptedData(ciphertext)
	require.NoError(t, err)
	encrypted.Metadata[keyVersionMetadataKey] = []byte("1")
	oldV2Ciphertext, err := backend.serializeEncryptedData(encrypted)
	require.NoError(t, err)

	_, err = backend.RotateKey(ctx, keyID)
	require.NoError(t, err)
	require.NoError(t, backend.DeleteKey(ctx, keyID))

	for _, versionID := range []string{keyID + "_v2", keyID + "_v3"} {
		_, err = backend.GetKeyMetadata(ctx, versionID)
		assert.ErrorIs(t, err, ErrKeyNotFound, versionID)
		_, err = backend.Encrypt(ctx, versionID, []byte("p"), nil)
		assert.ErrorIs(t, err, ErrKeyNotFound, versionID)
		_, err = backend.Decrypt(ctx, versionID, ciphertext, nil)
		assert.ErrorIs(t, err, ErrKeyNotFound, versionID)
	}

	createKeyNameTestKey(t, backend, keyID+"_v2")
	_, err = backend.Decrypt(ctx, keyID+"_v2", oldV2Ciphertext, nil)
	assert.Error(t, err, "a fresh key must not carry the deleted version's material")
	require.NoError(t, backend.DeleteKey(ctx, keyID+"_v2"))
}

// assertKeyRoundTrips checks that Encrypt then Decrypt on keyID still works.
func assertKeyRoundTrips(t *testing.T, b *XKMSBackend, keyID string) {
	t.Helper()
	ctx := context.Background()
	ciphertext, err := b.Encrypt(ctx, keyID, []byte("p"), nil)
	require.NoError(t, err)
	plaintext, err := b.Decrypt(ctx, keyID, ciphertext, nil)
	require.NoError(t, err)
	assert.Equal(t, []byte("p"), plaintext)
}

func TestXKMSBackend_CreateKeyRefusesHidingName(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	ctx := context.Background()

	backend := newKeyNameTestBackend(t)
	createKeyNameTestKey(t, backend, "report_v2026")

	_, err := backend.CreateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "report",
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.ErrorIs(t, err, ErrInvalidKeyID)
	assert.Contains(t, err.Error(), "report_v2026")

	assertKeyRoundTrips(t, backend, "report_v2026")
	require.NoError(t, backend.DeleteKey(ctx, "report_v2026"))
}

func TestXKMSBackend_ImportKeyRefusesHidingName(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	ctx := context.Background()

	backend := newKeyNameTestBackend(t)
	createKeyNameTestKey(t, backend, "report_v2026")

	_, err := backend.ImportKey(ctx, "report", make([]byte, 32), "AES-256-GCM", nil)
	require.ErrorIs(t, err, ErrInvalidKeyID)
	assert.Contains(t, err.Error(), "report_v2026")

	assertKeyRoundTrips(t, backend, "report_v2026")
	require.NoError(t, backend.DeleteKey(ctx, "report_v2026"))
}

func TestXKMSBackend_CreateKeyAfterDeletingRotatedKey(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	ctx := context.Background()

	backend := newKeyNameTestBackend(t)
	createKeyNameTestKey(t, backend, "report")
	_, err := backend.RotateKey(ctx, "report")
	require.NoError(t, err)
	require.NoError(t, backend.DeleteKey(ctx, "report"))

	createKeyNameTestKey(t, backend, "report")
	assertKeyRoundTrips(t, backend, "report")
	require.NoError(t, backend.DeleteKey(ctx, "report"))
}

func TestXKMSBackend_CreateExistingRotatedKeyKeepsError(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	ctx := context.Background()

	backend := newKeyNameTestBackend(t)
	createKeyNameTestKey(t, backend, "report")
	_, err := backend.RotateKey(ctx, "report")
	require.NoError(t, err)

	_, err = backend.CreateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "report",
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrInvalidKeyID)
	require.NoError(t, backend.DeleteKey(ctx, "report"))
}
