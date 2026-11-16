// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package kms

import (
	"context"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// seedLegacySymmetricKey stores a symmetric key the way the backend did before
// symmetric keys moved to KeyTypeSecret — under KeyTypeEncryption — and
// returns ciphertext it encrypted, tagged with version, as the backend
// serialized it.
func seedLegacySymmetricKey(t *testing.T, backend *XKMSBackend, name string, version int, plaintext []byte) []byte {
	t.Helper()
	ctx := context.Background()
	_, err := dbxkms.GenerateSymmetricKey(ctx, "software", &xkmssdk.KeyAttributes{
		CN:                 name,
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)
	encrypted, err := dbxkms.Encrypt(ctx, "software:encryption:aes256-gcm:"+name, plaintext, &xkmssdk.EncryptOptions{})
	require.NoError(t, err)
	if encrypted.Metadata == nil {
		encrypted.Metadata = make(map[string][]byte)
	}
	encrypted.Metadata[keyVersionMetadataKey] = []byte{byte('0' + version)}
	ciphertext, err := backend.serializeEncryptedData(encrypted)
	require.NoError(t, err)
	return ciphertext
}

// A key stored before the upgrade, and the data it encrypted, stay usable
// after it: the backend finds the key under its old type, decrypts what it
// encrypted, encrypts new data with it and deletes it.
func TestXKMSBackend_ALegacyEncryptionTypedKeyStillDecryptsAfterTheUpgrade(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	backend, err := NewXKMSBackend(&XKMSBackendConfig{BackendType: config.BackendTypeSoftware, Logger: &mockLogger{}})
	require.NoError(t, err)
	ctx := context.Background()

	plaintext := []byte("encrypted before the upgrade")
	ciphertext := seedLegacySymmetricKey(t, backend, "legacy-key", 1, plaintext)

	meta, err := backend.GetKeyMetadata(ctx, "legacy-key")
	require.NoError(t, err, "a key stored under the legacy type must still be found")
	require.Equal(t, "AES-256-GCM", meta.Algorithm)

	decrypted, err := backend.Decrypt(ctx, "legacy-key", ciphertext, nil)
	require.NoError(t, err, "data encrypted before the upgrade must still decrypt")
	require.Equal(t, plaintext, decrypted)

	again, err := backend.Encrypt(ctx, "legacy-key", []byte("after"), nil)
	require.NoError(t, err)
	roundTrip, err := backend.Decrypt(ctx, "legacy-key", again, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("after"), roundTrip)

	require.NoError(t, backend.DeleteKey(ctx, "legacy-key"))
	_, err = backend.GetKeyMetadata(ctx, "legacy-key")
	require.Error(t, err, "the deleted legacy key is still found")
}

// A version rotated in before the upgrade is stored under the legacy type too,
// and data it encrypted still decrypts.
func TestXKMSBackend_ALegacyRotatedVersionStillDecryptsAfterTheUpgrade(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	backend, err := NewXKMSBackend(&XKMSBackendConfig{BackendType: config.BackendTypeSoftware, Logger: &mockLogger{}})
	require.NoError(t, err)
	ctx := context.Background()

	seedLegacySymmetricKey(t, backend, "rotated-key", 1, []byte("v1"))
	plaintext := []byte("encrypted by version 2 before the upgrade")
	ciphertext := seedLegacySymmetricKey(t, backend, backend.buildVersionedKeyID("rotated-key", 2), 2, plaintext)

	decrypted, err := backend.Decrypt(ctx, "rotated-key", ciphertext, nil)
	require.NoError(t, err, "data a legacy rotated version encrypted must still decrypt")
	require.Equal(t, plaintext, decrypted)
}

// A new key never shadows a legacy key of the same name: create and import
// are refused, as go-xkms refused a duplicate before the type changed, and
// the legacy key still decrypts what it encrypted.
func TestXKMSBackend_ANewKeyNeverShadowsALegacyKey(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	backend, err := NewXKMSBackend(&XKMSBackendConfig{BackendType: config.BackendTypeSoftware, Logger: &mockLogger{}})
	require.NoError(t, err)
	ctx := context.Background()

	plaintext := []byte("encrypted before the upgrade")
	ciphertext := seedLegacySymmetricKey(t, backend, "held-key", 1, plaintext)

	_, err = backend.CreateKey(ctx, &xkmssdk.KeyAttributes{CN: "held-key", SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM})
	require.ErrorIs(t, err, ErrKeyAlreadyExists, "create must not shadow the legacy key")
	_, err = backend.ImportKey(ctx, "held-key", make([]byte, 32), "AES-256-GCM", nil)
	require.ErrorIs(t, err, ErrKeyAlreadyExists, "import must not shadow the legacy key")

	decrypted, err := backend.Decrypt(ctx, "held-key", ciphertext, nil)
	require.NoError(t, err, "the legacy key must still decrypt its data")
	require.Equal(t, plaintext, decrypted)

	// A name the legacy type does not hold is still created.
	_, err = backend.CreateKey(ctx, &xkmssdk.KeyAttributes{CN: "fresh-key", SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM})
	require.NoError(t, err)
}

// Rotation moves past a version the legacy type holds instead of writing the
// same version under the current type, so data that version encrypted still
// decrypts.
func TestXKMSBackend_RotationNeverShadowsALegacyVersion(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	backend, err := NewXKMSBackend(&XKMSBackendConfig{BackendType: config.BackendTypeSoftware, Logger: &mockLogger{}})
	require.NoError(t, err)
	ctx := context.Background()

	seedLegacySymmetricKey(t, backend, "rotating-key", 1, []byte("v1"))
	plaintext := []byte("encrypted by legacy version 2")
	ciphertext := seedLegacySymmetricKey(t, backend, backend.buildVersionedKeyID("rotating-key", 2), 2, plaintext)

	_, err = backend.GetKeyMetadata(ctx, "rotating-key")
	require.NoError(t, err)
	backend.mu.Lock()
	backend.metadata["rotating-key"].Version = 1
	backend.mu.Unlock()

	meta, err := backend.RotateKey(ctx, "rotating-key")
	require.NoError(t, err)
	require.Equal(t, 3, meta.Version, "rotation must skip the version the legacy type holds")

	decrypted, err := backend.Decrypt(ctx, "rotating-key", ciphertext, nil)
	require.NoError(t, err, "data legacy version 2 encrypted must still decrypt")
	require.Equal(t, plaintext, decrypted)
}
