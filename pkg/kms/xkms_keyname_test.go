package kms

import (
	"context"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
