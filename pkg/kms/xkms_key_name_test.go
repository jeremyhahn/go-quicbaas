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
	"testing"

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

	ids := []string{"tenant/acme/k", "tenant/acme/k_v2", "tenant.acme.k", "a/b", "a.b", "qbx.production", "qbx.YQ"}
	seen := map[string]string{}
	for _, id := range ids {
		require.NoError(t, checkNewXKMSKeyID(id), "%q is an admissible new key ID", id)
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

// TestXKMSKeyName_KeepsPrefixedNamesOfEarlierReleases checks that a valid
// name with the encoding prefix, which earlier releases stored verbatim, is
// still stored and listed verbatim, and that only the encoded name of another
// ID is refused for a new key.
func TestXKMSKeyName_KeepsPrefixedNamesOfEarlierReleases(t *testing.T) {
	for _, legacy := range []string{"qbx.production", "qbx.key", "qbx.YQ", "qbx.a-b_c.d"} {
		require.Equal(t, legacy, xkmsKeyName(legacy), "%q keeps its go-xkms name", legacy)
		require.Equal(t, legacy, keyIDFromXKMSName(legacy), "%q lists under its own name", legacy)
	}

	encoded := xkmsKeyName("a/b")
	require.Equal(t, "qbx.YS9i", encoded)
	require.ErrorIs(t, checkNewXKMSKeyID(encoded), ErrXKMSKeyIDReserved,
		"the go-xkms name of another key is not a new key ID")
}

// TestXKMSBackend_KeyNamedByEarlierReleaseStaysUsable creates a key in
// go-xkms under a literal "qbx." name, as releases before the name mapping
// did, and uses it through the backend by that ID.
func TestXKMSBackend_KeyNamedByEarlierReleaseStaysUsable(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := context.Background()

	const keyID = "qbx.production"
	attrs := backend.algorithmToKeyAttributes(keyID, AlgAES256GCM)
	require.Equal(t, keyID, attrs.CN, "the earlier release's go-xkms name is the key ID itself")
	_, err := xkms.GenerateSymmetricKey(ctx, string(backend.backendType), attrs)
	require.NoError(t, err)

	_, err = backend.GetKeyMetadata(ctx, keyID)
	require.NoError(t, err, "the key is found under its ID")

	ciphertext, err := backend.Encrypt(ctx, keyID, []byte("secret"), nil)
	require.NoError(t, err, "the key encrypts under its ID")
	plaintext, err := backend.Decrypt(ctx, keyID, ciphertext, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("secret"), plaintext)

	keys, _, err := backend.ListKeys(ctx, "qbx.", 0)
	require.NoError(t, err)
	var listed []string
	for _, k := range keys {
		listed = append(listed, k.KeyID)
	}
	require.Contains(t, listed, keyID, "the key lists under its own name")

	_, err = backend.CreateKey(ctx, backend.algorithmToKeyAttributes(xkmsKeyName("tenant/acme/k"), AlgAES256GCM))
	require.ErrorIs(t, err, ErrXKMSKeyIDReserved, "a new key cannot take another key's go-xkms name")

	require.NoError(t, backend.DeleteKey(ctx, keyID))
}
