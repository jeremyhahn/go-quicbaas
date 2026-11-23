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

package kms

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/jeremyhahn/go-xkms/pkg/validation"
	goxkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// The service layer scopes a tenant's key as "tenant/<tenantID>/<name>"; "/"
// is not a go-xkms key-name character. The backend must still create, use,
// rotate, list and delete that key through go-xkms.
func TestXKMSBackend_TenantScopedKeyID_EncryptsAndDecrypts(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := context.Background()

	const keyID = "tenant/acme/crypto-key"
	_, err := backend.CreateKey(ctx, backend.algorithmToKeyAttributes(keyID, AlgAES256GCM))
	require.NoError(t, err)

	ciphertext, err := backend.Encrypt(ctx, keyID, []byte("secret"), nil)
	require.NoError(t, err, "a tenant-scoped key must encrypt")

	plaintext, err := backend.Decrypt(ctx, keyID, ciphertext, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("secret"), plaintext)

	_, err = backend.RotateKey(ctx, keyID)
	require.NoError(t, err)
	rotated, err := backend.Encrypt(ctx, keyID, []byte("after"), nil)
	require.NoError(t, err, "the rotated version must encrypt")
	plaintext, err = backend.Decrypt(ctx, keyID, rotated, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("after"), plaintext)

	keys, _, err := backend.ListKeys(ctx, "tenant/acme/", 0)
	require.NoError(t, err)
	var listed []string
	for _, k := range keys {
		listed = append(listed, k.KeyID)
	}
	require.Contains(t, listed, keyID, "the key lists under its service ID")

	require.NoError(t, backend.DeleteKey(ctx, keyID))
}

func TestXKMSKeyName_ValidNamesAreLiteral(t *testing.T) {
	tenantName := xkmsKeyName("tenant/acme/k")
	for _, id := range []string{"plain-key_1.v2", "qbx.production", tenantName} {
		require.NoError(t, validation.ValidateKeyID(id))
		require.Equal(t, id, xkmsKeyName(id), "a name go-xkms accepts is its own name, as before the mapping")
	}

	seen := map[string]string{}
	for _, id := range []string{"tenant/acme/k", "tenant/acme/k2", "a/b", "a:b"} {
		name := xkmsKeyName(id)
		require.NoError(t, validation.ValidateKeyID(name), "the encoded name of %q is a valid go-xkms name", id)
		require.Equal(t, xkmsEncodedKeyNamePrefix+base64.RawURLEncoding.EncodeToString([]byte(id)), name)
		_, dup := seen[name]
		require.False(t, dup, "two IDs share the encoded name %q", name)
		seen[name] = id
	}
}

// TestXKMSBackend_LegacyKeySpelledLikeATenantEncoding_StaysASystemKey seeds a
// key the way the code before the mapping stored it (its go-xkms name is its
// service ID) under a name that spells the encoding of a tenant ID. Its owner
// keeps it, and the tenant ID never reaches it.
func TestXKMSBackend_LegacyKeySpelledLikeATenantEncoding_StaysASystemKey(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := context.Background()

	const tenantKeyID = "tenant/acme/k"
	systemKeyID := xkmsEncodedKeyNamePrefix + base64.RawURLEncoding.EncodeToString([]byte(tenantKeyID))
	_, err := xkms.GenerateSymmetricKey(ctx, string(backend.backendType),
		backend.algorithmToKeyAttributes(systemKeyID, AlgAES256GCM))
	require.NoError(t, err)

	ciphertext, err := backend.Encrypt(ctx, systemKeyID, []byte("system"), nil)
	require.NoError(t, err, "the existing key's owner keeps access")
	plaintext, err := backend.Decrypt(ctx, systemKeyID, ciphertext, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("system"), plaintext)

	_, err = backend.GetKeyMetadata(ctx, tenantKeyID)
	require.ErrorIs(t, err, ErrKeyNotFound, "the tenant ID must not resolve to the system key")
	_, err = backend.Encrypt(ctx, tenantKeyID, []byte("tenant"), nil)
	require.Error(t, err, "the tenant ID must not encrypt under the system key")
	_, err = backend.Decrypt(ctx, tenantKeyID, ciphertext, nil)
	require.Error(t, err, "the tenant ID must not decrypt under the system key")

	_, err = backend.CreateKey(ctx, backend.algorithmToKeyAttributes(tenantKeyID, AlgAES256GCM))
	require.ErrorIs(t, err, ErrKeyNameTaken, "a tenant key must not take over the system key's name")
	_, err = backend.GetKeyMetadata(ctx, tenantKeyID)
	require.ErrorIs(t, err, ErrKeyNotFound, "a refused create leaves no ownership record")

	keys, _, err := backend.ListKeys(ctx, "", 0)
	require.NoError(t, err)
	var listed []string
	for _, k := range keys {
		listed = append(listed, k.KeyID)
	}
	require.Contains(t, listed, systemKeyID, "the system key lists under its own ID")
	require.NotContains(t, listed, tenantKeyID, "the system key is never relabelled a tenant key")

	require.NoError(t, backend.DeleteKey(ctx, systemKeyID))
}

// TestXKMSBackend_TenantKeyName_IsOwnedPersistently checks that a tenant
// key's encoded name is held by its ownership record: across a restart that
// keeps the record store, against a literal ID spelled like the encoding, and
// failing closed when the record is gone.
func TestXKMSBackend_TenantKeyName_IsOwnedPersistently(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	ctx := context.Background()
	store, err := goxkmssdk.NewMemoryBackend()
	require.NoError(t, err)
	newBackend := func(names goxkmssdk.StorageBackend) *XKMSBackend {
		b, err := NewXKMSBackend(&XKMSBackendConfig{
			BackendType: config.BackendTypeSoftware,
			Logger:      &mockLogger{},
			KeyNames:    names,
		})
		require.NoError(t, err)
		return b
	}

	const tenantKeyID = "tenant/acme/owned"
	literalID := xkmsKeyName(tenantKeyID)
	backend := newBackend(store)
	_, err = backend.CreateKey(ctx, backend.algorithmToKeyAttributes(tenantKeyID, AlgAES256GCM))
	require.NoError(t, err)
	ciphertext, err := backend.Encrypt(ctx, tenantKeyID, []byte("tenant"), nil)
	require.NoError(t, err)

	_, err = backend.CreateKey(ctx, backend.algorithmToKeyAttributes(literalID, AlgAES256GCM))
	require.ErrorIs(t, err, ErrKeyNameTaken, "a literal ID must not take over a tenant key's name")

	restarted := newBackend(store)
	_, err = restarted.GetKeyMetadata(ctx, literalID)
	require.ErrorIs(t, err, ErrKeyNotFound, "the literal ID must not resolve to the tenant key")
	plaintext, err := restarted.Decrypt(ctx, tenantKeyID, ciphertext, nil)
	require.NoError(t, err, "the tenant key resolves through its persisted record after a restart")
	require.Equal(t, []byte("tenant"), plaintext)
	keys, _, err := restarted.ListKeys(ctx, "tenant/acme/", 0)
	require.NoError(t, err)
	var listed []string
	for _, k := range keys {
		listed = append(listed, k.KeyID)
	}
	require.Contains(t, listed, tenantKeyID)

	emptyStore, err := goxkmssdk.NewMemoryBackend()
	require.NoError(t, err)
	_, err = newBackend(emptyStore).GetKeyMetadata(ctx, tenantKeyID)
	require.ErrorIs(t, err, ErrKeyNotFound, "without its record the encoded name fails closed")

	require.NoError(t, restarted.DeleteKey(ctx, tenantKeyID))
	owner, found, err := restarted.keyNames().owner(ctx, literalID)
	require.NoError(t, err)
	require.False(t, found, "deleting the key releases its name, owner %q", owner)
}

func TestXKMSBackend_LegacyQBXKeyName_StillWorks(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := context.Background()

	// Store the keys as the pre-mapping code did: the go-xkms name IS the
	// service key ID.
	const encKeyID = "qbx.production"
	_, err := xkms.GenerateSymmetricKey(ctx, string(backend.backendType),
		backend.algorithmToKeyAttributes(encKeyID, AlgAES256GCM))
	require.NoError(t, err)
	const signKeyID = "qbx.production.sign"
	_, err = xkms.GenerateKeyWithBackend(ctx, string(backend.backendType),
		backend.algorithmToKeyAttributes(signKeyID, AlgECDSAP256))
	require.NoError(t, err)

	ciphertext, err := backend.Encrypt(ctx, encKeyID, []byte("secret"), nil)
	require.NoError(t, err, "the legacy key must still encrypt")
	plaintext, err := backend.Decrypt(ctx, encKeyID, ciphertext, nil)
	require.NoError(t, err, "the legacy key must still decrypt")
	require.Equal(t, []byte("secret"), plaintext)

	digest := sha256.Sum256([]byte("legacy"))
	signature, err := backend.Sign(ctx, signKeyID, digest[:], "SHA-256")
	require.NoError(t, err, "the legacy key must still sign")
	valid, err := backend.Verify(ctx, signKeyID, digest[:], signature, "SHA-256")
	require.NoError(t, err)
	require.True(t, valid)

	keys, _, err := backend.ListKeys(ctx, "qbx.", 0)
	require.NoError(t, err)
	var listed []string
	for _, k := range keys {
		listed = append(listed, k.KeyID)
	}
	require.Contains(t, listed, encKeyID, "the legacy key lists under its own name")
	require.Contains(t, listed, signKeyID, "the legacy key lists under its own name")

	require.NoError(t, backend.DeleteKey(ctx, encKeyID), "the legacy key must still delete")
	require.NoError(t, backend.DeleteKey(ctx, signKeyID), "the legacy key must still delete")
}
