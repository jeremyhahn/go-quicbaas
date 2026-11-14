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
	"github.com/stretchr/testify/require"

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

func TestXKMSKeyName_IsInjectiveAndKeepsValidNames(t *testing.T) {
	require.Equal(t, "plain-key_1.v2", xkmsKeyName("plain-key_1.v2"), "a valid name is unchanged")

	ids := []string{"tenant/acme/k", "tenant/acme/k_v2", "tenant.acme.k", "a/b", "a.b"}
	seen := map[string]string{}
	for _, id := range ids {
		name := xkmsKeyName(id)
		require.Regexp(t, `^[A-Za-z0-9._-]+$`, name)
		require.NotContains(t, name, "..")
		if prev, ok := seen[name]; ok {
			t.Fatalf("%q and %q map to the same go-xkms name %q", prev, id, name)
		}
		seen[name] = id
		require.Equal(t, id, keyIDFromXKMSName(name), "the mapping round-trips")
	}
}

// TestXKMSKeyName_EveryValidNameMapsToItself pins the upgrade contract: a
// name go-xkms accepts that is not spelled as an encoded output is the
// go-xkms name, including one that starts with the encoding's prefix, and
// ListKeys reads it back unchanged.
func TestXKMSKeyName_EveryValidNameMapsToItself(t *testing.T) {
	for _, id := range []string{
		"qbx.production", "qbx.", "qbx.cHJvZA", "qbx.production.sign", "qbx-x", "QBX.prod",
	} {
		require.NoError(t, validation.ValidateKeyID(id), "fixture %q must be a valid go-xkms name", id)
		require.Equal(t, id, xkmsKeyName(id), "a valid name maps to itself")
		require.Equal(t, id, keyIDFromXKMSName(id), "ListKeys reads a literal name unchanged")
	}
}

// TestXKMSKeyName_EncodedOutputsAreReserved: an ID spelled as an encoded
// output is itself encoded, so a literal ID never shares a go-xkms name with
// a tenant-scoped one.
func TestXKMSKeyName_EncodedOutputsAreReserved(t *testing.T) {
	tenantName := xkmsKeyName("tenant/acme/k")
	require.NoError(t, validation.ValidateKeyID(tenantName))
	require.NotEqual(t, tenantName, xkmsKeyName(tenantName),
		"the literal ID %q must not resolve to tenant acme's key", tenantName)
	require.Equal(t, tenantName, keyIDFromXKMSName(xkmsKeyName(tenantName)), "the escaped literal round-trips")
	require.Equal(t, "tenant/acme/k", keyIDFromXKMSName(tenantName))

	// Every level of nesting stays distinct and round-trips.
	seen := map[string]string{}
	id := "a/b"
	for i := 0; i < 4; i++ {
		name := xkmsKeyName(id)
		require.NoError(t, validation.ValidateKeyID(name))
		if prev, ok := seen[name]; ok {
			t.Fatalf("%q and %q map to the same go-xkms name %q", prev, id, name)
		}
		seen[name] = id
		require.Equal(t, id, keyIDFromXKMSName(name))
		id = name
	}
}

// TestXKMSBackend_LiteralEncodedNameIsNotTheTenantKey is the isolation
// regression: a system key whose literal ID is the encoding of a tenant's
// key ID is a different go-xkms key, so the tenant cannot address it.
func TestXKMSBackend_LiteralEncodedNameIsNotTheTenantKey(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := context.Background()

	const tenantKeyID = "tenant/acme/k"
	systemKeyID := xkmsKeyName(tenantKeyID)
	_, err := backend.CreateKey(ctx, backend.algorithmToKeyAttributes(systemKeyID, AlgAES256GCM))
	require.NoError(t, err)

	_, err = backend.GetKeyMetadata(ctx, tenantKeyID)
	require.ErrorIs(t, err, ErrKeyNotFound, "the tenant ID must not resolve to the system key")
	_, err = backend.Encrypt(ctx, tenantKeyID, []byte("secret"), nil)
	require.Error(t, err, "the tenant ID must not encrypt under the system key")

	keys, _, err := backend.ListKeys(ctx, "", 0)
	require.NoError(t, err)
	var listed []string
	for _, k := range keys {
		listed = append(listed, k.KeyID)
	}
	require.Contains(t, listed, systemKeyID, "the system key lists under its own ID")
	require.NotContains(t, listed, tenantKeyID)

	require.NoError(t, backend.DeleteKey(ctx, systemKeyID))
}

// TestXKMSKeyName_DecodesOnlyEncodedOutputs: keyIDFromXKMSName decodes a name
// only when it is exactly what xkmsKeyName makes of an ID go-xkms rejects.
func TestXKMSKeyName_DecodesOnlyEncodedOutputs(t *testing.T) {
	for _, id := range []string{"tenant/acme/k", "a/b", "has space", "colon:id", "tenant/é/k"} {
		require.Error(t, validation.ValidateKeyID(id), "fixture %q must be rejected by go-xkms", id)
		name := xkmsKeyName(id)
		require.NoError(t, validation.ValidateKeyID(name), "the encoded name must be a valid go-xkms name")
		require.Equal(t, id, keyIDFromXKMSName(name))
	}
	// A prefixed name whose payload decodes to a VALID ID is never an
	// encoded output (a valid ID is not encoded), so it reads literally.
	literal := xkmsEncodedKeyNamePrefix + base64.RawURLEncoding.EncodeToString([]byte("plain"))
	require.Equal(t, literal, keyIDFromXKMSName(literal))
	// A non-canonical payload is not an encoded output either.
	require.Equal(t, "qbx.production", keyIDFromXKMSName("qbx.production"))
}

// TestXKMSBackend_LegacyQBXKeyName_StillWorks is the upgrade fixture: keys
// stored in go-xkms under literal "qbx." names before the mapping existed
// still encrypt, decrypt, sign, verify, list and delete by those names.
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
