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

	"github.com/jeremyhahn/go-xkms/pkg/validation"
	"github.com/stretchr/testify/require"
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
// TestXKMSBackend_LegacyQBXKeyName_StillWorks is the upgrade fixture: keys
// stored in go-xkms under literal "qbx." names before the mapping existed
// still encrypt, decrypt, sign, verify, list and delete by those names.
