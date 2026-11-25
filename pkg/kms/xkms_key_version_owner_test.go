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

	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// TestXKMSBackend_RotatedTenantVersionIsOnlyItsParentsVersion checks that a
// rotated version of an encoded key is reachable only as that key's version:
// it never resolves, lists, encrypts, rotates or deletes as a key of its own,
// a forged ciphertext version does not select it through its own name, and a
// key claimed under a version name for another parent is not used.
func TestXKMSBackend_RotatedTenantVersionIsOnlyItsParentsVersion(t *testing.T) {
	ctx := context.Background()
	store := newKeyNameGuardXKMS(t)
	kb := newOwnerTestBackend(t, ctx, store)
	id := "tenant/acme/k"
	v2 := kb.buildVersionedKeyID(id, 2)

	_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgAES256GCM))
	require.NoError(t, err)
	ct1, err := kb.Encrypt(ctx, id, []byte("one"), nil)
	require.NoError(t, err)
	_, err = kb.RotateKey(ctx, id)
	require.NoError(t, err)
	ct2, err := kb.Encrypt(ctx, id, []byte("two"), nil)
	require.NoError(t, err)

	pt, err := kb.Decrypt(ctx, id, ct1, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("one"), pt)
	pt, err = kb.Decrypt(ctx, id, ct2, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("two"), pt)

	_, err = kb.GetKeyMetadata(ctx, v2)
	require.ErrorIs(t, err, ErrKeyNotFound)
	_, err = kb.Encrypt(ctx, v2, []byte("x"), nil)
	require.Error(t, err)
	require.Equal(t, []string{id}, listedKeyIDs(t, ctx, kb, "tenant/acme/"))
	_, err = kb.RotateKey(ctx, v2)
	require.Error(t, err)
	require.ErrorIs(t, kb.DeleteKey(ctx, v2), ErrKeyNotFound)
	pt, err = kb.Decrypt(ctx, id, ct2, nil)
	require.NoError(t, err, "a refused delete of the version leaves it usable through its key")
	require.Equal(t, []byte("two"), pt)

	// A version-2 ciphertext relabelled as version 1 and sent to the
	// version's own name must not decrypt.
	enc, err := kb.deserializeEncryptedData(ct2)
	require.NoError(t, err)
	enc.Metadata[keyVersionMetadataKey] = []byte("1")
	forged, err := kb.serializeEncryptedData(enc)
	require.NoError(t, err)
	_, err = kb.Decrypt(ctx, v2, forged, nil)
	require.Error(t, err)

	// A key claimed under k's version-3 name as another key's version is not
	// k's version.
	_, err = kb.CreateKey(ctx, kb.algorithmToKeyAttributes("tenant/acme/j", AlgAES256GCM))
	require.NoError(t, err)
	v3attrs := kb.xkmsKeyAttributes(kb.buildVersionedKeyID(id, 3), AlgAES256GCM)
	_, err = dbxkms.GenerateSymmetricKey(ctx, "software", v3attrs)
	require.NoError(t, err)
	require.NoError(t, kb.claimXKMSKeyAs(ctx, v3attrs, "tenant/acme/j"))
	require.ErrorIs(t, kb.checkOwnedXKMSKeyVersion(ctx, id, 3, AlgAES256GCM), ErrKeyNotFound)
	owned, err := kb.ownsXKMSKeyAs(ctx, v3attrs.CN, kb.xkmsKeyIdentity(v3attrs), true, "tenant/acme/j")
	require.NoError(t, err)
	require.True(t, owned, "the record vouches for the key as the parent it names")
	owned, err = kb.ownsXKMSKey(ctx, v3attrs.CN, kb.xkmsKeyIdentity(v3attrs), true)
	require.NoError(t, err)
	require.False(t, owned, "a version record is not a standalone key")
}
