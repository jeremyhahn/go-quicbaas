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
	tenantID, cn, err := xkmsKeyRef("plain-key_1.v2")
	require.NoError(t, err)
	require.Equal(t, "", tenantID, "a platform name has no tenant")
	require.Equal(t, "plain-key_1.v2", cn, "a platform name is unchanged")

	ids := []string{"tenant/acme/k", "tenant/acme/k_v2", "tenant/globex/k", "tenant.acme.k", "acme.k", "k"}
	type pair struct{ tenantID, cn string }
	seen := map[pair]string{}
	for _, id := range ids {
		tenantID, cn, err := xkmsKeyRef(id)
		require.NoError(t, err, id)
		require.NoError(t, validation.ValidateKeyID(cn), "the CN must be a valid go-xkms name")
		p := pair{tenantID, cn}
		if prev, ok := seen[p]; ok {
			t.Fatalf("%q and %q map to the same go-xkms key %+v", prev, id, p)
		}
		seen[p] = id
		require.Equal(t, id, keyIDFromXKMS(tenantID, cn), "the mapping round-trips")
	}

	for _, bad := range []string{"tenant/", "tenant//k", "tenant/acme", "tenant/acme/a/b", "a/b", "has space"} {
		_, _, err := xkmsKeyRef(bad)
		require.Error(t, err, "%q is refused", bad)
	}
}

// TestXKMSKeyName_EveryValidNameMapsToItself pins the upgrade contract: a
// name go-xkms accepts is the go-xkms name, always, including one that
// starts with the encoding's prefix, and ListKeys reads it back unchanged.
func TestXKMSKeyName_EveryValidNameMapsToItself(t *testing.T) {
	for _, id := range []string{
		"qbx.production", "qbx.", "qbx.YS9i", "qbx.cHJvZA", "qbx.production.sign", "qbx-x", "QBX.prod",
	} {
		require.NoError(t, validation.ValidateKeyID(id), "fixture %q must be a valid go-xkms name", id)
		tenantID, cn, err := xkmsKeyRef(id)
		require.NoError(t, err)
		require.Equal(t, "", tenantID, "a platform name has no tenant")
		require.Equal(t, id, cn, "a valid name maps to itself")
		require.Equal(t, id, keyIDFromXKMS("", id), "ListKeys reads a platform name unchanged")
	}
}

// TestXKMSKeyName_DecodesOnlyEncodedOutputs: keyIDFromXKMSName decodes a name
// only when it is exactly what xkmsKeyName makes of an ID go-xkms rejects.
func TestXKMSKeyName_DecodesOnlyEncodedOutputs(t *testing.T) {
	// The interim encoding is gone: a "qbx.<b64 of tenant/acme/k>" name is a
	// platform name like any other, and is never decoded into a tenant key.
	literal := "qbx." + base64.RawURLEncoding.EncodeToString([]byte("tenant/acme/k"))
	tenantID, cn, err := xkmsKeyRef(literal)
	require.NoError(t, err)
	require.Equal(t, "", tenantID)
	require.Equal(t, literal, cn)
	require.Equal(t, literal, keyIDFromXKMS("", literal))
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
