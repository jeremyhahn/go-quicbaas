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
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jeremyhahn/go-xkms/pkg/api/transport"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// newOwnerTestBackend boots a backend factory over store the way the
// server does and returns its software XKMSBackend.
func newOwnerTestBackend(t *testing.T, ctx context.Context, store xkmssdk.StorageBackend) *XKMSBackend {
	t.Helper()
	factory, err := NewBackendFactory(&BackendFactoryConfig{Logger: &mockLogger{}, KeyNameStore: store})
	require.NoError(t, err)
	require.NoError(t, factory.InitializeFromXKMS(ctx, nil))
	b, err := factory.Registry().Get("software")
	require.NoError(t, err)
	kb, ok := b.(*XKMSBackend)
	require.True(t, ok)
	return kb
}

func listedKeyIDs(t *testing.T, ctx context.Context, kb *XKMSBackend, prefix string) []string {
	t.Helper()
	keys, _, err := kb.ListKeys(ctx, prefix, 0)
	require.NoError(t, err)
	ids := make([]string, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.KeyID)
	}
	return ids
}

// TestXKMSBackend_NativeSystemKeyNeverResolvesAsTenantKey: the node's
// native go-xkms service writes the same key store under any valid literal
// name, including one shaped like an encoded tenant key name. Such a key must
// never resolve, or list, as the tenant key it decodes to; only names this
// backend wrote (and recorded) do.
func TestXKMSBackend_NativeSystemKeyNeverResolvesAsTenantKey(t *testing.T) {
	ctx := context.Background()
	store := newKeyNameGuardXKMS(t)

	// Clean boot: the marker is written.
	kb := newOwnerTestBackend(t, ctx, store)

	// A system caller of the native service plants an encoded-shaped name
	// after boot.
	svc, err := xkmssdk.Get()
	require.NoError(t, err)
	const planted = "qbx.dGVuYW50L2FjbWUvaw"
	require.Equal(t, "tenant/acme/k", keyIDFromXKMSName(planted))
	_, err = svc.GenerateKey(ctx, &transport.GenerateKeyRequest{
		KeyID: planted, Backend: "software", KeyType: "signing", Algorithm: "ed25519",
	})
	require.NoError(t, err)

	// Tenant acme's "k" is not the native key.
	_, err = kb.GetKeyMetadata(ctx, "tenant/acme/k")
	require.ErrorIs(t, err, ErrKeyNotFound)
	_, err = kb.ExportPublicKey(ctx, "tenant/acme/k")
	require.Error(t, err)
	require.NotContains(t, listedKeyIDs(t, ctx, kb, ""), "tenant/acme/k")
	require.Empty(t, listedKeyIDs(t, ctx, kb, "tenant/"))

	// Tenant acme now creates its own AES "k". go-xkms keeps it beside the
	// native Ed25519 key under the same name, and after a restart the
	// tenant's "k" is its AES key, never the native signing key.
	_, err = kb.CreateKey(ctx, kb.algorithmToKeyAttributes("tenant/acme/k", AlgAES256GCM))
	require.NoError(t, err)
	kbR := newOwnerTestBackend(t, ctx, store)
	meta, err := kbR.GetKeyMetadata(ctx, "tenant/acme/k")
	require.NoError(t, err)
	require.Equal(t, AlgAES256GCM, meta.Algorithm)
	_, err = kbR.ExportPublicKey(ctx, "tenant/acme/k")
	require.Error(t, err)
	_, err = kbR.Sign(ctx, "tenant/acme/k", make([]byte, 32), AlgEd25519)
	require.Error(t, err)
	listed := 0
	for _, id := range listedKeyIDs(t, ctx, kbR, "tenant/") {
		if id == "tenant/acme/k" {
			listed++
		}
	}
	require.Equal(t, 1, listed)
	require.NoError(t, kbR.DeleteKey(ctx, "tenant/acme/k"))
	kbR2 := newOwnerTestBackend(t, ctx, store)
	_, err = kbR2.GetKeyMetadata(ctx, "tenant/acme/k")
	require.ErrorIs(t, err, ErrKeyNotFound)

	// The backend's own encoded key still works, and its record persists
	// across a restart (a fresh backend with an empty cache).
	_, err = kb.CreateKey(ctx, kb.algorithmToKeyAttributes("tenant/beta/k", AlgAES256GCM))
	require.NoError(t, err)
	kb2 := newOwnerTestBackend(t, ctx, store)
	_, err = kb2.GetKeyMetadata(ctx, "tenant/beta/k")
	require.NoError(t, err)
	require.Contains(t, listedKeyIDs(t, ctx, kb2, "tenant/"), "tenant/beta/k")

	// Delete releases the name: a later native key under it is not the
	// deleted tenant key.
	require.NoError(t, kb2.DeleteKey(ctx, "tenant/beta/k"))
	_, err = svc.GenerateKey(ctx, &transport.GenerateKeyRequest{
		KeyID: xkmsKeyName("tenant/beta/k"), Backend: "software", KeyType: "signing", Algorithm: "ed25519",
	})
	require.NoError(t, err)
	_, err = kb2.GetKeyMetadata(ctx, "tenant/beta/k")
	require.ErrorIs(t, err, ErrKeyNotFound)
}

// TestXKMSBackend_OwnershipRecordIsPerBackend pins that an ownership record
// binds the full go-xkms key identity: a record one backend writes never
// vouches for the same name in another backend sharing the key store.
func TestXKMSBackend_OwnershipRecordIsPerBackend(t *testing.T) {
	ctx := context.Background()
	store := newKeyNameGuardXKMS(t)
	kb := newOwnerTestBackend(t, ctx, store)

	attrs := kb.xkmsKeyAttributes("tenant/acme/k", AlgAES256GCM)
	require.NoError(t, kb.claimXKMSKey(ctx, attrs))

	other, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypePKCS11, Logger: &mockLogger{}, KeyNameStore: store,
	})
	require.NoError(t, err)

	owned, err := kb.ownsXKMSKey(ctx, attrs.CN, kb.xkmsKeyIdentity(attrs))
	require.NoError(t, err)
	require.True(t, owned)
	owned, err = other.ownsXKMSKey(ctx, attrs.CN, other.xkmsKeyIdentity(attrs))
	require.NoError(t, err)
	require.False(t, owned)

	require.Equal(t, kb.buildFullKeyID("tenant/acme/k", AlgAES256GCM), kb.xkmsKeyIdentity(attrs))
	signing := kb.xkmsKeyAttributes("tenant/acme/k", AlgEd25519)
	require.Equal(t, kb.buildSigningKeyID("tenant/acme/k", AlgEd25519), kb.xkmsKeyIdentity(signing))
}

// failOwnerPutStore fails the next fails Puts of ownership records; every
// other write goes through.
type failOwnerPutStore struct {
	xkmssdk.StorageBackend
	fails atomic.Int32
}

func (s *failOwnerPutStore) Put(ctx context.Context, key string, value []byte) error {
	if strings.HasPrefix(key, xkmsKeyOwnerRecordPrefix) {
		for {
			n := s.fails.Load()
			if n <= 0 {
				break
			}
			if s.fails.CompareAndSwap(n, n-1) {
				return errors.New("injected ownership write failure")
			}
		}
	}
	return s.StorageBackend.Put(ctx, key, value)
}

// requireNoXKMSKey checks that neither the material nor the ownership record of
// keyID under algorithm exists.
func requireNoXKMSKey(t *testing.T, ctx context.Context, kb *XKMSBackend, store xkmssdk.StorageBackend, keyID, algorithm string) {
	t.Helper()
	if kb.isSymmetricAlgorithm(algorithm) {
		_, err := dbxkms.GetSymmetricKey(ctx, kb.buildFullKeyID(keyID, algorithm))
		require.Error(t, err, "key material of %s remains", keyID)
	} else {
		_, err := dbxkms.SignerByID(ctx, kb.buildSigningKeyID(keyID, algorithm))
		require.Error(t, err, "key material of %s remains", keyID)
	}
	ok, err := store.Exists(ctx, xkmsKeyOwnerRecord(kb.xkmsKeyIdentity(kb.xkmsKeyAttributes(keyID, algorithm))))
	require.NoError(t, err)
	require.False(t, ok, "ownership record of %s remains", keyID)
}

func TestXKMSBackend_CreateKey_OwnershipWriteFailureRollsBackAndRetrySucceeds(t *testing.T) {
	ctx := context.Background()
	store := &failOwnerPutStore{StorageBackend: newKeyNameGuardXKMS(t)}
	kb := newOwnerTestBackend(t, ctx, store)
	const id = "tenant/acme/create-k"

	store.fails.Store(1)
	_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgAES256GCM))
	var ownErr *XKMSKeyOwnershipError
	require.ErrorAs(t, err, &ownErr)
	require.Equal(t, "write", ownErr.Op)
	requireNoXKMSKey(t, ctx, kb, store, id, AlgAES256GCM)
	_, err = kb.GetKeyMetadata(ctx, id)
	require.ErrorIs(t, err, ErrKeyNotFound)

	_, err = kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgAES256GCM))
	require.NoError(t, err)
	meta, err := newOwnerTestBackend(t, ctx, store).GetKeyMetadata(ctx, id)
	require.NoError(t, err)
	require.Equal(t, AlgAES256GCM, meta.Algorithm)
}

func TestXKMSBackend_RotateKey_OwnershipWriteFailureRollsBackAndRetrySucceeds(t *testing.T) {
	ctx := context.Background()
	store := &failOwnerPutStore{StorageBackend: newKeyNameGuardXKMS(t)}
	kb := newOwnerTestBackend(t, ctx, store)
	const id = "tenant/acme/rotate-k"

	_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgAES256GCM))
	require.NoError(t, err)

	store.fails.Store(1)
	_, err = kb.RotateKey(ctx, id)
	var ownErr *XKMSKeyOwnershipError
	require.ErrorAs(t, err, &ownErr)
	meta, err := kb.GetKeyMetadata(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 1, meta.Version)
	requireNoXKMSKey(t, ctx, kb, store, kb.buildVersionedKeyID(id, 2), AlgAES256GCM)

	meta, err = kb.RotateKey(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 2, meta.Version)
	ct, err := kb.Encrypt(ctx, id, []byte("after rotation"), nil)
	require.NoError(t, err)
	pt, err := kb.Decrypt(ctx, id, ct, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("after rotation"), pt)
}

func TestXKMSBackend_ImportKey_OwnershipWriteFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	store := &failOwnerPutStore{StorageBackend: newKeyNameGuardXKMS(t)}
	kb := newOwnerTestBackend(t, ctx, store)
	const id = "tenant/acme/import-k"

	store.fails.Store(1)
	_, err := kb.ImportKey(ctx, id, make([]byte, 32), AlgAES256GCM, nil)
	var ownErr *XKMSKeyOwnershipError
	require.ErrorAs(t, err, &ownErr)
	requireNoXKMSKey(t, ctx, kb, store, id, AlgAES256GCM)
	_, err = kb.GetKeyMetadata(ctx, id)
	require.ErrorIs(t, err, ErrKeyNotFound)
}

func TestXKMSBackend_EverySymmetricAlgorithmResolvesAfterRestart(t *testing.T) {
	ctx := context.Background()
	store := newKeyNameGuardXKMS(t)
	kb := newOwnerTestBackend(t, ctx, store)

	for _, alg := range []string{AlgAES128GCM, AlgAES192GCM, AlgAES256GCM, AlgChaCha20Poly1305, AlgXChaCha20Poly1305} {
		t.Run(alg, func(t *testing.T) {
			id := "tenant/acme/restart-" + alg
			_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, alg))
			require.NoError(t, err)
			ct, err := kb.Encrypt(ctx, id, []byte("survives a restart"), nil)
			require.NoError(t, err)

			kbR := newOwnerTestBackend(t, ctx, store)
			meta, err := kbR.GetKeyMetadata(ctx, id)
			require.NoError(t, err)
			require.Equal(t, alg, meta.Algorithm)
			pt, err := kbR.Decrypt(ctx, id, ct, nil)
			require.NoError(t, err)
			require.Equal(t, []byte("survives a restart"), pt)
		})
	}

	for _, alg := range []string{AlgECDSAP256, AlgEd25519, AlgRSA3072} {
		t.Run(alg, func(t *testing.T) {
			id := "tenant/acme/restart-" + alg
			_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, alg))
			require.NoError(t, err)
			pub, err := kb.ExportPublicKey(ctx, id)
			require.NoError(t, err)

			kbR := newOwnerTestBackend(t, ctx, store)
			meta, err := kbR.GetKeyMetadata(ctx, id)
			require.NoError(t, err)
			require.Equal(t, alg, meta.Algorithm)
			pubR, err := kbR.ExportPublicKey(ctx, id)
			require.NoError(t, err)
			require.Equal(t, pub, pubR)
		})
	}
}

func TestXKMSBackend_MetadataProbesDeriveFromSupportedAlgorithms(t *testing.T) {
	ctx := context.Background()
	store := newKeyNameGuardXKMS(t)
	kb := newOwnerTestBackend(t, ctx, store)
	const id = "tenant/acme/probe-k"

	probes := kb.xkmsKeyProbes(ctx, id)
	kids := make(map[string]bool, len(probes))
	probed := make(map[string]bool, len(probes))
	sawSymmetric := false
	for _, p := range probes {
		require.False(t, kids[p.kid], "kid %s probed twice", p.kid)
		kids[p.kid] = true
		probed[p.algorithm] = true
		if p.symmetric {
			sawSymmetric = true
		} else {
			require.False(t, sawSymmetric, "signing probe %s after a symmetric one", p.algorithm)
		}
	}
	symmetricSupported := 0
	for _, alg := range kb.SupportedAlgorithms(ctx) {
		if kb.isSymmetricAlgorithm(alg) {
			symmetricSupported++
			require.True(t, probed[alg], "supported symmetric algorithm %s has no probe", alg)
		}
	}
	require.Equal(t, 5, symmetricSupported)
}
