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
	stderrors "errors"
	"testing"
	"time"

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

// TestXKMSBackend_VersionedDecrypt_RefusesALegacyVersionName seeds a system
// key, the pre-change way, under the literal name a tenant key's version 2
// would encode to. The tenant owns its base key but no version 2, so a
// ciphertext whose metadata claims version 2 must not decrypt under the
// system key.
func TestXKMSBackend_VersionedDecrypt_RefusesALegacyVersionName(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := context.Background()

	const tenantKeyID = "tenant/acme/k"
	_, err := backend.CreateKey(ctx, backend.algorithmToKeyAttributes(tenantKeyID, AlgAES256GCM))
	require.NoError(t, err)

	systemKeyID := xkmsKeyName(backend.buildVersionedKeyID(tenantKeyID, 2))
	_, err = xkms.GenerateSymmetricKey(ctx, string(backend.backendType),
		backend.algorithmToKeyAttributes(systemKeyID, AlgAES256GCM))
	require.NoError(t, err)

	ciphertext, err := backend.Encrypt(ctx, systemKeyID, []byte("system secret"), nil)
	require.NoError(t, err)
	encrypted, err := backend.deserializeEncryptedData(ciphertext)
	require.NoError(t, err)
	if encrypted.Metadata == nil {
		encrypted.Metadata = map[string][]byte{}
	}
	encrypted.Metadata[keyVersionMetadataKey] = []byte("2")
	forged, err := backend.serializeEncryptedData(encrypted)
	require.NoError(t, err)

	plaintext, err := backend.Decrypt(ctx, tenantKeyID, forged, nil)
	require.Error(t, err, "a version the tenant never rotated to must not resolve to the system key")
	require.Nil(t, plaintext)

	plaintext, err = backend.Decrypt(ctx, systemKeyID, ciphertext, nil)
	require.NoError(t, err, "the system key's owner keeps access")
	require.Equal(t, []byte("system secret"), plaintext)
}

// TestXKMSBackend_LegacyAES192KeyName_IsNotTakenOver seeds an AES-192 system
// key, an algorithm the metadata probe used to miss, under the encoding of a
// tenant key ID. Creating the tenant key must be refused rather than record
// tenant ownership of the system key's name.
func TestXKMSBackend_LegacyAES192KeyName_IsNotTakenOver(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := context.Background()

	const tenantKeyID = "tenant/acme/k"
	systemKeyID := xkmsKeyName(tenantKeyID)
	_, err := xkms.GenerateSymmetricKey(ctx, string(backend.backendType),
		backend.algorithmToKeyAttributes(systemKeyID, "AES-192-GCM"))
	require.NoError(t, err)

	_, err = backend.CreateKey(ctx, backend.algorithmToKeyAttributes(tenantKeyID, AlgAES256GCM))
	require.ErrorIs(t, err, ErrKeyNameTaken)

	keys, _, err := backend.ListKeys(ctx, "", 0)
	require.NoError(t, err)
	var listed []string
	for _, k := range keys {
		listed = append(listed, k.KeyID)
	}
	require.Contains(t, listed, systemKeyID, "the system key lists under its own ID")
	require.NotContains(t, listed, tenantKeyID)
}

// TestXKMSNameOwners_FailedConcurrentCreate_KeepsTheWinnersRecord drives the
// interleaving where request A writes the record, request B creates the key,
// and A's own creation then fails. A's rollback must not drop the record B's
// key relies on.
func TestXKMSNameOwners_FailedConcurrentCreate_KeepsTheWinnersRecord(t *testing.T) {
	ctx := context.Background()
	store, err := goxkmssdk.NewMemoryBackend()
	require.NoError(t, err)
	names := &xkmsNameOwners{backend: "software", store: store}
	const keyID = "tenant/acme/k"
	absent := func(string) (bool, error) { return false, nil }

	bDone := make(chan struct{})
	aInCreate := make(chan struct{})
	var bRanDuringA bool
	var name string
	aErr := make(chan error, 1)
	go func() {
		aErr <- names.claim(ctx, keyID, absent, func(string) error {
			close(aInCreate)
			// Give B every chance to create its key while A's create runs.
			select {
			case <-bDone:
				bRanDuringA = true
			case <-time.After(200 * time.Millisecond):
			}
			return stderrors.New("key already exists")
		})
	}()
	<-aInCreate
	bErr := names.claim(ctx, keyID, absent, func(string) error { return nil })
	close(bDone)

	require.Error(t, <-aErr)
	require.NoError(t, bErr)
	require.False(t, bRanDuringA, "a create must not run while another holds the name's reservation")
	name, err = names.resolve(ctx, keyID)
	require.NoError(t, err, "the successful create's ownership record must survive A's rollback")
	require.Equal(t, xkmsKeyName(keyID), name)

	// A failed create over an existing record never drops it either.
	err = names.claim(ctx, keyID, absent, func(string) error { return stderrors.New("exists") })
	require.Error(t, err)
	_, err = names.resolve(ctx, keyID)
	require.NoError(t, err)

	// An inconclusive existence lookup fails the claim and writes nothing.
	const other = "tenant/acme/other"
	lookupErr := stderrors.New("backend unavailable")
	err = names.claim(ctx, other, func(string) (bool, error) { return false, lookupErr },
		func(string) error { t.Fatal("create must not run"); return nil })
	require.ErrorIs(t, err, lookupErr)
	_, err = names.resolve(ctx, other)
	require.ErrorIs(t, err, ErrKeyNotFound)
}

// TestXKMSNameOwners_DeleteThenConcurrentImport_KeepsTheImportsRecord drives
// a delete and an import of the same tenant key ID: the import starts while
// the delete's backend deletion runs. Release holds the ownership lock across
// the backend deletion and the record removal, so the import waits, then
// claims a fresh record, and the key it created still resolves.
func TestXKMSNameOwners_DeleteThenConcurrentImport_KeepsTheImportsRecord(t *testing.T) {
	ctx := context.Background()
	store, err := goxkmssdk.NewMemoryBackend()
	require.NoError(t, err)
	names := &xkmsNameOwners{backend: "software", store: store}
	const keyID = "tenant/acme/k"
	var held bool
	exists := func(string) (bool, error) { return held, nil }
	require.NoError(t, names.claim(ctx, keyID, exists, func(string) error { held = true; return nil }))

	inDelete := make(chan struct{})
	importDone := make(chan struct{})
	var importRanDuringDelete bool
	delErr := make(chan error, 1)
	go func() {
		delErr <- names.release(ctx, keyID, func() error {
			close(inDelete)
			select {
			case <-importDone:
				importRanDuringDelete = true
			case <-time.After(200 * time.Millisecond):
			}
			held = false
			return nil
		})
	}()
	<-inDelete
	importErr := names.claim(ctx, keyID, exists, func(string) error { held = true; return nil })
	close(importDone)

	require.NoError(t, <-delErr)
	require.NoError(t, importErr)
	require.False(t, importRanDuringDelete, "an import must not run while a delete of the same key holds its record")
	name, err := names.resolve(ctx, keyID)
	require.NoError(t, err, "the recreated key's ownership record must survive the delete")
	require.Equal(t, xkmsKeyName(keyID), name)

	// A failed backend deletion keeps the record: the key is still there.
	boom := stderrors.New("backend unavailable")
	require.ErrorIs(t, names.release(ctx, keyID, func() error { return boom }), boom)
	_, err = names.resolve(ctx, keyID)
	require.NoError(t, err)
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
