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
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jeremyhahn/go-xkms/pkg/api/transport"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
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
	_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes("tenant/acme/k", AlgAES256GCM))
	require.NoError(t, err)

	other, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypePKCS11, Logger: &mockLogger{}, KeyNameStore: store,
	})
	require.NoError(t, err)

	owned, err := kb.ownsXKMSKey(ctx, attrs.CN, kb.xkmsKeyIdentity(attrs), true)
	require.NoError(t, err)
	require.True(t, owned)
	owned, err = other.ownsXKMSKey(ctx, attrs.CN, other.xkmsKeyIdentity(attrs), true)
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

// newTwoBackendXKMS initializes go-xkms with two software service backends:
// "software", the default, and "pkcs11", the backend under test. It returns
// the key storage of each.
func newTwoBackendXKMS(t *testing.T) (defaultStore, targetStore xkmssdk.StorageBackend) {
	t.Helper()
	return newTwoBackendXKMSNamed(t, "software", string(config.BackendTypePKCS11))
}

// newTwoBackendXKMSNamed initializes go-xkms with two software service
// backends registered as defaultName, the default, and targetName.
func newTwoBackendXKMSNamed(t *testing.T, defaultName, targetName string) (defaultStore, targetStore xkmssdk.StorageBackend) {
	t.Helper()
	dbxkms.Reset()
	t.Cleanup(dbxkms.Reset)

	newKS := func() (xkmssdk.StorageBackend, xkmssdk.ServiceBackend) {
		keyStorage, err := xkmssdk.NewMemoryBackend()
		require.NoError(t, err)
		certStorage, err := xkmssdk.NewMemoryBackend()
		require.NoError(t, err)
		swBackend, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{KeyStorage: keyStorage})
		require.NoError(t, err)
		ks, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{Backend: swBackend, CertStorage: certStorage})
		require.NoError(t, err)
		return keyStorage, ks
	}
	defaultStore, defaultKS := newKS()
	targetStore, targetKS := newKS()
	require.NoError(t, dbxkms.Initialize(&dbxkms.Config{
		DefaultBackend: defaultName,
		Backends: map[string]xkmssdk.ServiceBackend{
			defaultName: defaultKS,
			targetName:  targetKS,
		},
		Logger: &mockLogger{},
	}))
	return defaultStore, targetStore
}

// newTwoBackendTestBackends returns an XKMSBackend on the "pkcs11" backend and
// one on the default "software" backend, both over store.
func newTwoBackendTestBackends(t *testing.T, store xkmssdk.StorageBackend) (target, def *XKMSBackend) {
	t.Helper()
	target, err := NewXKMSBackend(&XKMSBackendConfig{BackendType: config.BackendTypePKCS11, Logger: &mockLogger{}, KeyNameStore: store})
	require.NoError(t, err)
	def, err = NewXKMSBackend(&XKMSBackendConfig{BackendType: config.BackendTypeSoftware, Logger: &mockLogger{}, KeyNameStore: store})
	require.NoError(t, err)
	return target, def
}

// plantDefaultBackendDecoy stores an AES-256-GCM key under the go-xkms name of
// keyID in the default backend and returns a check that it is still there.
func plantDefaultBackendDecoy(t *testing.T, ctx context.Context, kb, def *XKMSBackend, keyID string) func() {
	t.Helper()
	require.Equal(t, kb.xkmsKeyAttributes(keyID, AlgAES256GCM).CN, def.xkmsKeyAttributes(keyID, AlgAES256GCM).CN)
	_, err := dbxkms.GenerateSymmetricKey(ctx, "software", def.xkmsKeyAttributes(keyID, AlgAES256GCM))
	require.NoError(t, err)
	decoyID := def.buildFullKeyID(keyID, AlgAES256GCM)
	require.True(t, strings.HasPrefix(decoyID, "software:"), decoyID)
	kept := func() {
		t.Helper()
		_, err := dbxkms.GetSymmetricKey(ctx, decoyID)
		require.NoError(t, err, "the default backend's key is untouched")
	}
	kept()
	return kept
}

// TestXKMSBackend_OwnershipRollbackDeletesFromItsOwnBackend checks that when
// the ownership record cannot be written, the rollback deletes the new key
// from the backend that created it, not from go-xkms's default backend.
func TestXKMSBackend_OwnershipRollbackDeletesFromItsOwnBackend(t *testing.T) {
	ctx := context.Background()
	t.Run("create", func(t *testing.T) {
		_, targetStore := newTwoBackendXKMS(t)
		store := &failOwnerPutStore{StorageBackend: targetStore}
		kb, def := newTwoBackendTestBackends(t, store)
		const id = "tenant/acme/rollback-create-k"
		decoyKept := plantDefaultBackendDecoy(t, ctx, kb, def, id)

		store.fails.Store(1)
		_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgAES256GCM))
		var ownErr *XKMSKeyOwnershipError
		require.ErrorAs(t, err, &ownErr)
		require.Equal(t, "write", ownErr.Op)
		requireNoXKMSKey(t, ctx, kb, store, id, AlgAES256GCM)
		decoyKept()

		_, err = kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgAES256GCM))
		require.NoError(t, err)
	})
	t.Run("rotate", func(t *testing.T) {
		_, targetStore := newTwoBackendXKMS(t)
		store := &failOwnerPutStore{StorageBackend: targetStore}
		kb, def := newTwoBackendTestBackends(t, store)
		const id = "tenant/acme/rollback-rotate-k"
		_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgAES256GCM))
		require.NoError(t, err)
		v2 := kb.buildVersionedKeyID(id, 2)
		decoyKept := plantDefaultBackendDecoy(t, ctx, kb, def, v2)

		store.fails.Store(1)
		_, err = kb.RotateKey(ctx, id)
		var ownErr *XKMSKeyOwnershipError
		require.ErrorAs(t, err, &ownErr)
		require.Equal(t, "write", ownErr.Op)
		requireNoXKMSKey(t, ctx, kb, store, v2, AlgAES256GCM)
		decoyKept()

		_, err = kb.RotateKey(ctx, id)
		require.NoError(t, err)
	})
}

// TestXKMSBackend_DeleteKeyDeletesFromItsOwnBackend checks that DeleteKey
// removes the key from this backend, not from go-xkms's default backend.
func TestXKMSBackend_DeleteKeyDeletesFromItsOwnBackend(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"cached", "uncached"} {
		t.Run(name, func(t *testing.T) {
			_, targetStore := newTwoBackendXKMS(t)
			kb, def := newTwoBackendTestBackends(t, targetStore)
			const id = "tenant/acme/delete-k"
			_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgAES256GCM))
			require.NoError(t, err)
			decoyKept := plantDefaultBackendDecoy(t, ctx, kb, def, id)

			deleter := kb
			if name == "uncached" {
				deleter, _ = newTwoBackendTestBackends(t, targetStore)
			}
			require.NoError(t, deleter.DeleteKey(ctx, id))
			requireNoXKMSKey(t, ctx, kb, targetStore, id, AlgAES256GCM)
			decoyKept()
		})
	}
}

// TestXKMSBackend_LegacyLookupReadsItsOwnBackend checks that a literal key
// name resolves only from this backend: a key of that name in go-xkms's
// default backend is not this backend's key, and a key of that name in this
// backend resolves. The backend under test is
// "software" and the default is a second keystore named "pkcs11", because a
// software keystore refuses signing kids that name another backend type, so
// the metadata probes of a "pkcs11" backend never reach the legacy lookup.
func TestXKMSBackend_LegacyLookupReadsItsOwnBackend(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, id, alg string
		generate      func(ctx context.Context, backend string, attrs *xkmssdk.KeyAttributes) error
	}{
		{"symmetric", "legacy-sym-key", AlgAES256GCM, func(ctx context.Context, backend string, attrs *xkmssdk.KeyAttributes) error {
			_, err := dbxkms.GenerateSymmetricKey(ctx, backend, attrs)
			return err
		}},
		{"signing", "legacy-sign-key", AlgRSA2048, func(ctx context.Context, backend string, attrs *xkmssdk.KeyAttributes) error {
			_, err := dbxkms.GenerateKeyWithBackend(ctx, backend, attrs)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkcs11 := string(config.BackendTypePKCS11)
			_, targetStore := newTwoBackendXKMSNamed(t, pkcs11, "software")
			def, kb := newTwoBackendTestBackends(t, targetStore)
			require.Equal(t, tc.id, xkmsKeyName(tc.id))
			require.NoError(t, tc.generate(ctx, pkcs11, def.xkmsKeyAttributes(tc.id, tc.alg)))

			_, err := kb.GetKeyMetadata(ctx, tc.id)
			require.ErrorIs(t, err, ErrKeyNotFound)

			require.NoError(t, tc.generate(ctx, "software", kb.xkmsKeyAttributes(tc.id, tc.alg)))
			_, fresh := newTwoBackendTestBackends(t, targetStore)
			_, err = fresh.GetKeyMetadata(ctx, tc.id)
			require.NoError(t, err)
		})
	}
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

// failOwnerDeleteStore fails the next fails Deletes of ownership records;
// every other delete goes through.
type failOwnerDeleteStore struct {
	xkmssdk.StorageBackend
	fails atomic.Int32
}

func (s *failOwnerDeleteStore) Delete(ctx context.Context, key string) error {
	if strings.HasPrefix(key, xkmsKeyOwnerRecordPrefix) {
		for {
			n := s.fails.Load()
			if n <= 0 {
				break
			}
			if s.fails.CompareAndSwap(n, n-1) {
				return errors.New("injected ownership delete failure")
			}
		}
	}
	return s.StorageBackend.Delete(ctx, key)
}

// failedOwnerRecordDelete creates an AES key and deletes it while the
// ownership record's delete fails: the material is gone, the record stays.
func failedOwnerRecordDelete(t *testing.T, ctx context.Context, kb *XKMSBackend, store *failOwnerDeleteStore, id string) {
	t.Helper()
	_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgAES256GCM))
	require.NoError(t, err)

	store.fails.Store(1)
	err = kb.DeleteKey(ctx, id)
	var ownErr *XKMSKeyOwnershipError
	require.ErrorAs(t, err, &ownErr)
	require.Equal(t, "delete", ownErr.Op)

	_, err = dbxkms.GetSymmetricKey(ctx, kb.buildFullKeyID(id, AlgAES256GCM))
	require.Error(t, err, "the material went before the record")
	ok, err := store.Exists(ctx, xkmsKeyOwnerRecord(kb.xkmsKeyIdentity(kb.xkmsKeyAttributes(id, AlgAES256GCM))))
	require.NoError(t, err)
	require.True(t, ok, "the record is left by the failed release")
}

// TestXKMSBackend_DeleteKey_RecordDeleteFailureRetrySucceeds: a delete whose
// record release fails after the material went is completed by its retry, on
// the same backend and on a fresh one (a restart with an empty cache).
func TestXKMSBackend_DeleteKey_RecordDeleteFailureRetrySucceeds(t *testing.T) {
	ctx := context.Background()

	t.Run("same backend", func(t *testing.T) {
		store := &failOwnerDeleteStore{StorageBackend: newKeyNameGuardXKMS(t)}
		kb := newOwnerTestBackend(t, ctx, store)
		const id = "tenant/acme/delete-retry-k"
		failedOwnerRecordDelete(t, ctx, kb, store, id)

		require.NoError(t, kb.DeleteKey(ctx, id), "the retry releases the record")
		requireNoXKMSKey(t, ctx, kb, store, id, AlgAES256GCM)
		_, err := kb.GetKeyMetadata(ctx, id)
		require.ErrorIs(t, err, ErrKeyNotFound)
		require.ErrorIs(t, kb.DeleteKey(ctx, id), ErrKeyNotFound, "a completed delete is not found again")
	})

	t.Run("fresh backend", func(t *testing.T) {
		store := &failOwnerDeleteStore{StorageBackend: newKeyNameGuardXKMS(t)}
		kb := newOwnerTestBackend(t, ctx, store)
		const id = "tenant/acme/delete-restart-k"
		failedOwnerRecordDelete(t, ctx, kb, store, id)

		kbR := newOwnerTestBackend(t, ctx, store)
		require.NoError(t, kbR.DeleteKey(ctx, id), "the retry after a restart releases the record")
		requireNoXKMSKey(t, ctx, kbR, store, id, AlgAES256GCM)
		require.ErrorIs(t, kbR.DeleteKey(ctx, id), ErrKeyNotFound)
	})
}

// TestXKMSBackend_DeleteKey_NativeReuseAfterRecoveredDeleteIsNotOwned: once
// the retry completes the delete, native material later stored under the
// same identity is not the deleted key.
func TestXKMSBackend_DeleteKey_NativeReuseAfterRecoveredDeleteIsNotOwned(t *testing.T) {
	ctx := context.Background()
	store := &failOwnerDeleteStore{StorageBackend: newKeyNameGuardXKMS(t)}
	kb := newOwnerTestBackend(t, ctx, store)
	const id = "tenant/acme/delete-reuse-k"
	failedOwnerRecordDelete(t, ctx, kb, store, id)
	require.NoError(t, kb.DeleteKey(ctx, id))

	attrs := kb.xkmsKeyAttributes(id, AlgAES256GCM)
	attrs.StoreType = xkmssdk.StoreType(string(config.BackendTypeSoftware))
	_, err := dbxkms.GenerateSymmetricKey(ctx, "software", attrs)
	require.NoError(t, err)

	for name, b := range map[string]*XKMSBackend{"same": kb, "fresh": newOwnerTestBackend(t, ctx, store)} {
		t.Run(name, func(t *testing.T) {
			owned, err := b.ownsXKMSKey(ctx, attrs.CN, b.xkmsKeyIdentity(attrs), true)
			require.NoError(t, err)
			require.False(t, owned)
			_, err = b.GetKeyMetadata(ctx, id)
			require.ErrorIs(t, err, ErrKeyNotFound)
			require.NotContains(t, listedKeyIDs(t, ctx, b, "tenant/"), id)
			_, err = b.Encrypt(ctx, id, []byte("not yours"), nil)
			require.Error(t, err)
		})
	}
}

// TestXKMSBackend_DeleteKey_NotFoundWithoutRecordReturnsNotFound: a key that
// never existed is not found, on a warm and on a fresh backend.
func TestXKMSBackend_DeleteKey_NotFoundWithoutRecordReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	store := newKeyNameGuardXKMS(t)
	kb := newOwnerTestBackend(t, ctx, store)
	require.ErrorIs(t, kb.DeleteKey(ctx, "tenant/acme/never-k"), ErrKeyNotFound)
	require.ErrorIs(t, newOwnerTestBackend(t, ctx, store).DeleteKey(ctx, "tenant/acme/never-k"), ErrKeyNotFound)
}

// failMaterialDeleteStore fails the next fails Deletes of key material (any
// key that is not an ownership record); every other delete goes through.
type failMaterialDeleteStore struct {
	xkmssdk.StorageBackend
	fails atomic.Int32
}

func (s *failMaterialDeleteStore) Delete(ctx context.Context, key string) error {
	if !strings.HasPrefix(key, xkmsKeyOwnerRecordPrefix) {
		for {
			n := s.fails.Load()
			if n <= 0 {
				break
			}
			if s.fails.CompareAndSwap(n, n-1) {
				return errors.New("injected key material delete failure")
			}
		}
	}
	return s.StorageBackend.Delete(ctx, key)
}

// newFailMaterialDeleteXKMS is newKeyNameGuardXKMS with go-xkms's key
// storage behind a failMaterialDeleteStore.
func newFailMaterialDeleteXKMS(t *testing.T) (xkmssdk.StorageBackend, *failMaterialDeleteStore) {
	t.Helper()
	dbxkms.Reset()
	t.Cleanup(dbxkms.Reset)

	memStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)
	keyStorage := &failMaterialDeleteStore{StorageBackend: memStorage}
	certStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)
	swBackend, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{KeyStorage: keyStorage})
	require.NoError(t, err)
	ks, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{Backend: swBackend, CertStorage: certStorage})
	require.NoError(t, err)
	require.NoError(t, dbxkms.Initialize(&dbxkms.Config{
		DefaultBackend: "software",
		Backends:       map[string]xkmssdk.ServiceBackend{"software": ks},
		Logger:         &mockLogger{},
	}))
	return memStorage, keyStorage
}

// TestXKMSBackend_DeleteKey_FailureNeverDeletesFallbackIdentity: when the
// key's own material fails to delete, DeleteKey returns a key deletion error
// and keeps the record. It never deletes another identity under the same
// name, such as a native AES-256-GCM key.
func TestXKMSBackend_DeleteKey_FailureNeverDeletesFallbackIdentity(t *testing.T) {
	ctx := context.Background()
	store, material := newFailMaterialDeleteXKMS(t)
	kb := newOwnerTestBackend(t, ctx, store)
	const id = "tenant/acme/fallback-k"

	_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgEd25519))
	require.NoError(t, err)
	native := kb.xkmsKeyAttributes(id, AlgAES256GCM)
	native.StoreType = xkmssdk.StoreType(string(config.BackendTypeSoftware))
	_, err = dbxkms.GenerateSymmetricKey(ctx, "software", native)
	require.NoError(t, err)

	material.fails.Store(1)
	err = kb.DeleteKey(ctx, id)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrKeyNotFound)
	var delErr *api.KeyDeletionError
	require.ErrorAs(t, err, &delErr)

	_, err = dbxkms.GetSymmetricKey(ctx, kb.buildFullKeyID(id, AlgAES256GCM))
	require.NoError(t, err, "the native AES key under the same name is not deleted")
	ok, err := store.Exists(ctx, xkmsKeyOwnerRecord(kb.xkmsKeyIdentity(kb.xkmsKeyAttributes(id, AlgEd25519))))
	require.NoError(t, err)
	require.True(t, ok, "the record of the undeleted key is kept")
	meta, err := kb.GetKeyMetadata(ctx, id)
	require.NoError(t, err)
	require.Equal(t, AlgEd25519, meta.Algorithm)

	require.NoError(t, kb.DeleteKey(ctx, id), "the retry deletes the key")
	requireNoXKMSKey(t, ctx, kb, store, id, AlgEd25519)
}

// failOwnerExistsStore fails Exists of ownership records while fail is set.
type failOwnerExistsStore struct {
	xkmssdk.StorageBackend
	fail atomic.Bool
}

func (s *failOwnerExistsStore) Exists(ctx context.Context, key string) (bool, error) {
	if s.fail.Load() && strings.HasPrefix(key, xkmsKeyOwnerRecordPrefix) {
		return false, errors.New("injected ownership read failure")
	}
	return s.StorageBackend.Exists(ctx, key)
}

// TestXKMSBackend_DeleteKey_OwnershipReadErrorIsReturned: a store whose
// ownership read fails makes DeleteKey return that error, never "not
// found", and the key is left whole.
func TestXKMSBackend_DeleteKey_OwnershipReadErrorIsReturned(t *testing.T) {
	ctx := context.Background()
	store := &failOwnerExistsStore{StorageBackend: newKeyNameGuardXKMS(t)}
	kb := newOwnerTestBackend(t, ctx, store)
	const id = "tenant/acme/read-error-k"
	_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgAES256GCM))
	require.NoError(t, err)

	kbR := newOwnerTestBackend(t, ctx, store)
	store.fail.Store(true)
	err = kbR.DeleteKey(ctx, id)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrKeyNotFound)
	var ownErr *XKMSKeyOwnershipError
	require.ErrorAs(t, err, &ownErr)
	require.Equal(t, "read", ownErr.Op)

	store.fail.Store(false)
	meta, err := kbR.GetKeyMetadata(ctx, id)
	require.NoError(t, err, "the key is left whole")
	require.Equal(t, AlgAES256GCM, meta.Algorithm)
}

// TestXKMSBackend_ForgedVersionNeverSelectsNativeKey: a ciphertext version
// is caller input. A forged version naming "<id>_v3", where native material
// that this backend never rotated in is stored, is refused rather than
// decrypted under that native key.
func TestXKMSBackend_ForgedVersionNeverSelectsNativeKey(t *testing.T) {
	ctx := context.Background()
	store := newKeyNameGuardXKMS(t)
	kb := newOwnerTestBackend(t, ctx, store)
	const id = "tenant/acme/forged-version-k"
	_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgAES256GCM))
	require.NoError(t, err)

	v3 := kb.buildVersionedKeyID(id, 3)
	native := kb.xkmsKeyAttributes(v3, AlgAES256GCM)
	native.StoreType = xkmssdk.StoreType(string(config.BackendTypeSoftware))
	_, err = dbxkms.GenerateSymmetricKey(ctx, "software", native)
	require.NoError(t, err)

	// A ciphertext made with the native _v3 key and labelled version 3.
	encrypted, err := dbxkms.Encrypt(ctx, kb.buildFullKeyID(v3, AlgAES256GCM), []byte("forged"), &xkmssdk.EncryptOptions{})
	require.NoError(t, err)
	if encrypted.Metadata == nil {
		encrypted.Metadata = make(map[string][]byte)
	}
	encrypted.Metadata[keyVersionMetadataKey] = []byte("3")
	forged, err := kb.serializeEncryptedData(encrypted)
	require.NoError(t, err)

	for name, b := range map[string]*XKMSBackend{"same": kb, "fresh": newOwnerTestBackend(t, ctx, store)} {
		t.Run(name, func(t *testing.T) {
			pt, err := b.Decrypt(ctx, id, forged, nil)
			require.ErrorIs(t, err, ErrKeyNotFound)
			require.Nil(t, pt)
		})
	}

	// The key's own versions still work.
	ct, err := kb.Encrypt(ctx, id, []byte("own"), nil)
	require.NoError(t, err)
	pt, err := kb.Decrypt(ctx, id, ct, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("own"), pt)
}

// TestXKMSBackend_VerifyRefusesNativeSigningKey: Verify, like Sign, never
// uses native signing material under a tenant key's name that this backend
// did not record.
func TestXKMSBackend_VerifyRefusesNativeSigningKey(t *testing.T) {
	ctx := context.Background()
	store := newKeyNameGuardXKMS(t)
	kb := newOwnerTestBackend(t, ctx, store)
	const id = "tenant/acme/native-verify-k"

	svc, err := xkmssdk.Get()
	require.NoError(t, err)
	_, err = svc.GenerateKey(ctx, &transport.GenerateKeyRequest{
		KeyID: xkmsKeyName(id), Backend: "software", KeyType: "signing", Algorithm: "ed25519",
	})
	require.NoError(t, err)

	ok, err := kb.Verify(ctx, id, make([]byte, 32), make([]byte, 64), AlgEd25519)
	require.ErrorIs(t, err, ErrKeyNotFound)
	require.False(t, ok)
	_, err = kb.Sign(ctx, id, make([]byte, 32), AlgEd25519)
	require.ErrorIs(t, err, ErrKeyNotFound)
}

// TestXKMSBackend_NativeDeleteAndRecreateIsNotOwned: native go-xkms deletes a
// tenant's AES key and stores new material under the same identity. The
// ownership record proves the deleted material, so the replacement is not the
// tenant's key, on the backend that cached it or on a fresh one, and deleting
// through this backend leaves the native key alone.
func TestXKMSBackend_NativeDeleteAndRecreateIsNotOwned(t *testing.T) {
	ctx := context.Background()
	store := newKeyNameGuardXKMS(t)
	kb := newOwnerTestBackend(t, ctx, store)
	const id = "tenant/acme/k"
	_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgAES256GCM))
	require.NoError(t, err)
	_, err = kb.Encrypt(ctx, id, []byte("secret"), nil)
	require.NoError(t, err)

	attrs := kb.xkmsKeyAttributes(id, AlgAES256GCM)
	attrs.StoreType = xkmssdk.StoreType(string(config.BackendTypeSoftware))
	require.NoError(t, dbxkms.DeleteKey(ctx, attrs))
	_, err = dbxkms.GenerateSymmetricKey(ctx, "software", attrs)
	require.NoError(t, err)

	native, err := dbxkms.Encrypt(ctx, kb.buildFullKeyID(id, AlgAES256GCM), []byte("native"), &xkmssdk.EncryptOptions{})
	require.NoError(t, err)
	nativeCT, err := kb.serializeEncryptedData(native)
	require.NoError(t, err)

	for name, b := range map[string]*XKMSBackend{"same": kb, "fresh": newOwnerTestBackend(t, ctx, store)} {
		t.Run(name, func(t *testing.T) {
			owned, err := b.ownsXKMSKey(ctx, attrs.CN, b.xkmsKeyIdentity(attrs), true)
			require.NoError(t, err)
			require.False(t, owned)
			_, err = b.GetKeyMetadata(ctx, id)
			require.ErrorIs(t, err, ErrKeyNotFound)
			_, err = b.Encrypt(ctx, id, []byte("not yours"), nil)
			require.Error(t, err)
			_, err = b.Decrypt(ctx, id, nativeCT, nil)
			require.Error(t, err)
			require.NotContains(t, listedKeyIDs(t, ctx, b, "tenant/"), id)
			require.ErrorIs(t, b.DeleteKey(ctx, id), ErrKeyNotFound)
			_, err = dbxkms.GetSymmetricKey(ctx, kb.buildFullKeyID(id, AlgAES256GCM))
			require.NoError(t, err, "the native key is not deleted")
		})
	}
}

// errTransientDecrypt stands for a throttled or denied decrypt.
var errTransientDecrypt = errors.New("decrypt throttled")

// TestXKMSBackend_TransientProofDecryptFailureKeepsRotatedKey checks that a
// proof decrypt failing with a non-authentication error is a verify error,
// not a missing key: the rotated key's cached version survives, and once the
// decrypt recovers the key encrypts under its current version.
func TestXKMSBackend_TransientProofDecryptFailureKeepsRotatedKey(t *testing.T) {
	ctx := context.Background()
	store := newKeyNameGuardXKMS(t)
	kb := newOwnerTestBackend(t, ctx, store)
	const id = "tenant/acme/k"
	_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgAES256GCM))
	require.NoError(t, err)
	_, err = kb.RotateKey(ctx, id)
	require.NoError(t, err)
	meta, err := kb.GetKeyMetadata(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 2, int(meta.Version))

	var armed atomic.Bool
	kb.decrypt = func(ctx context.Context, kid string, data *xkmssdk.EncryptedData, opts *xkmssdk.DecryptOptions) ([]byte, error) {
		if armed.Load() {
			return nil, errTransientDecrypt
		}
		return dbxkms.Decrypt(ctx, kid, data, opts)
	}
	armed.Store(true)

	_, err = kb.GetKeyMetadata(ctx, id)
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrKeyNotFound), "a transient decrypt failure is not a missing key: %v", err)
	var ownErr *XKMSKeyOwnershipError
	require.ErrorAs(t, err, &ownErr)
	require.Equal(t, "verify", ownErr.Op)
	require.ErrorIs(t, err, errTransientDecrypt)

	kb.mu.RLock()
	cached, ok := kb.metadata[id]
	var cachedVersion int
	if ok {
		cachedVersion = int(cached.Version)
	}
	kb.mu.RUnlock()
	require.True(t, ok, "the live key's cached metadata is kept")
	require.Equal(t, 2, cachedVersion)

	armed.Store(false)
	meta, err = kb.GetKeyMetadata(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 2, int(meta.Version))
	ct, err := kb.Encrypt(ctx, id, []byte("after"), nil)
	require.NoError(t, err)
	enc, err := kb.deserializeEncryptedData(ct)
	require.NoError(t, err)
	require.Equal(t, "2", string(enc.Metadata[keyVersionMetadataKey]))
	pt, err := kb.Decrypt(ctx, id, ct, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("after"), pt)
}

// TestXKMSBackend_FirstProofDecryptFailureKeepsRotatedKey checks that a proof
// decrypt failing once with a non-authentication error is a verify error even
// though the key's material works again right after: the check makes no
// second decrypt, the rotated key's cached version survives, and the next
// lookup serves the key under its current version.
func TestXKMSBackend_FirstProofDecryptFailureKeepsRotatedKey(t *testing.T) {
	ctx := context.Background()
	store := newKeyNameGuardXKMS(t)
	kb := newOwnerTestBackend(t, ctx, store)
	const id = "tenant/acme/k1"
	_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgAES256GCM))
	require.NoError(t, err)
	_, err = kb.RotateKey(ctx, id)
	require.NoError(t, err)
	meta, err := kb.GetKeyMetadata(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 2, int(meta.Version))

	var calls atomic.Int64
	kb.decrypt = func(ctx context.Context, kid string, data *xkmssdk.EncryptedData, opts *xkmssdk.DecryptOptions) ([]byte, error) {
		if calls.Add(1) == 1 {
			return nil, errTransientDecrypt
		}
		return dbxkms.Decrypt(ctx, kid, data, opts)
	}

	_, err = kb.GetKeyMetadata(ctx, id)
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrKeyNotFound), "a transient decrypt failure is not a missing key: %v", err)
	var ownErr *XKMSKeyOwnershipError
	require.ErrorAs(t, err, &ownErr)
	require.Equal(t, "verify", ownErr.Op)
	require.ErrorIs(t, err, errTransientDecrypt)
	require.Equal(t, int64(1), calls.Load(), "the check makes no second decrypt")

	kb.mu.RLock()
	cached, ok := kb.metadata[id]
	var cachedVersion int
	if ok {
		cachedVersion = int(cached.Version)
	}
	kb.mu.RUnlock()
	require.True(t, ok, "the live key's cached metadata is kept")
	require.Equal(t, 2, cachedVersion)

	meta, err = kb.GetKeyMetadata(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 2, int(meta.Version))
	ct, err := kb.Encrypt(ctx, id, []byte("after"), nil)
	require.NoError(t, err)
	enc, err := kb.deserializeEncryptedData(ct)
	require.NoError(t, err)
	require.Equal(t, "2", string(enc.Metadata[keyVersionMetadataKey]))
	pt, err := kb.Decrypt(ctx, id, ct, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("after"), pt)
}

// TestXKMSBackend_ProofMismatchIsNotOwned checks, for every symmetric
// algorithm, that a proof which does not authenticate under its record is a
// confirmed mismatch from the one proof decrypt: not owned, no error.
func TestXKMSBackend_ProofMismatchIsNotOwned(t *testing.T) {
	for _, alg := range []string{AlgAES128GCM, AlgAES192GCM, AlgAES256GCM, AlgChaCha20Poly1305, AlgXChaCha20Poly1305} {
		t.Run(alg, func(t *testing.T) {
			ctx := context.Background()
			store := newKeyNameGuardXKMS(t)
			kb := newOwnerTestBackend(t, ctx, store)
			id := "tenant/acme/mismatch-" + strings.ToLower(alg)
			_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, alg))
			require.NoError(t, err)

			attrs := kb.xkmsKeyAttributes(id, alg)
			attrs.StoreType = xkmssdk.StoreType(string(config.BackendTypeSoftware))
			identity := kb.xkmsKeyIdentity(attrs)
			require.Equal(t, kb.buildFullKeyID(id, alg), identity)
			proof, err := kb.xkmsKeyOwnerProofFor(ctx, identity, xkmsKeyOwnerRecord(identity)+"#other", true)
			require.NoError(t, err)
			raw, err := json.Marshal(proof)
			require.NoError(t, err)
			require.NoError(t, kb.keyNames.Put(ctx, xkmsKeyOwnerRecord(identity), raw))

			var calls atomic.Int64
			kb.decrypt = func(ctx context.Context, kid string, data *xkmssdk.EncryptedData, opts *xkmssdk.DecryptOptions) ([]byte, error) {
				calls.Add(1)
				return dbxkms.Decrypt(ctx, kid, data, opts)
			}
			owned, err := kb.ownsXKMSKey(ctx, attrs.CN, identity, true)
			require.NoError(t, err)
			require.False(t, owned)
			require.Equal(t, int64(1), calls.Load(), "only the proof decrypt")

			_, err = newOwnerTestBackend(t, ctx, store).GetKeyMetadata(ctx, id)
			require.ErrorIs(t, err, ErrKeyNotFound)
		})
	}
}

// TestIsXKMSProofMismatch checks that only AEAD authentication failures
// classify as a proof mismatch.
func TestIsXKMSProofMismatch(t *testing.T) {
	require.Len(t, xkmsAEADOpenErrors, 3)
	for _, e := range xkmsAEADOpenErrors {
		require.Error(t, e)
	}
	for _, err := range []error{nil, errTransientDecrypt, context.DeadlineExceeded, &api.XKMSDecryptionError{Cause: errTransientDecrypt}} {
		require.False(t, isXKMSProofMismatch(err), "%v", err)
	}
	for _, err := range []error{
		&api.XKMSDecryptionError{Cause: fmt.Errorf("decryption failed (authentication error): %w", xkmsAEADOpenErrors[0])},
		xkmssdk.AESGCMErrDecryptionFailed,
	} {
		require.True(t, isXKMSProofMismatch(err), "%v", err)
	}
}

// TestXKMSBackend_NativeRecreatedSigningKeyIsNotOwned is the signing-key
// form: the public-key fingerprint in the record does not match the native
// replacement.
func TestXKMSBackend_NativeRecreatedSigningKeyIsNotOwned(t *testing.T) {
	ctx := context.Background()
	store := newKeyNameGuardXKMS(t)
	kb := newOwnerTestBackend(t, ctx, store)
	const id = "tenant/acme/s"
	_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgECDSAP256))
	require.NoError(t, err)
	_, err = kb.ExportPublicKey(ctx, id)
	require.NoError(t, err)

	attrs := kb.xkmsKeyAttributes(id, AlgECDSAP256)
	attrs.StoreType = xkmssdk.StoreType(string(config.BackendTypeSoftware))
	require.NoError(t, dbxkms.DeleteKey(ctx, attrs))
	_, err = dbxkms.GenerateKeyWithBackend(ctx, "software", attrs)
	require.NoError(t, err)
	_, err = dbxkms.SignerByID(ctx, kb.buildSigningKeyID(id, AlgECDSAP256))
	require.NoError(t, err)

	for name, b := range map[string]*XKMSBackend{"same": kb, "fresh": newOwnerTestBackend(t, ctx, store)} {
		t.Run(name, func(t *testing.T) {
			owned, err := b.ownsXKMSKey(ctx, attrs.CN, b.xkmsKeyIdentity(attrs), false)
			require.NoError(t, err)
			require.False(t, owned)
			_, err = b.GetKeyMetadata(ctx, id)
			require.ErrorIs(t, err, ErrKeyNotFound)
			_, err = b.Sign(ctx, id, make([]byte, 32), AlgECDSAP256)
			require.Error(t, err)
			_, err = b.ExportPublicKey(ctx, id)
			require.Error(t, err)
		})
	}
}

// failMaterialGetStore fails every read of go-xkms key material while armed;
// ownership records still read.
type failMaterialGetStore struct {
	xkmssdk.StorageBackend
	fail atomic.Bool
}

func (s *failMaterialGetStore) Get(ctx context.Context, key string) ([]byte, error) {
	if s.fail.Load() && !strings.HasPrefix(key, xkmsKeyOwnerRecordPrefix) {
		return nil, errors.New("injected material read failure")
	}
	return s.StorageBackend.Get(ctx, key)
}

// newFailMaterialGetXKMS is newKeyNameGuardXKMS with go-xkms's key storage
// behind a failMaterialGetStore.
func newFailMaterialGetXKMS(t *testing.T) (xkmssdk.StorageBackend, *failMaterialGetStore) {
	t.Helper()
	dbxkms.Reset()
	t.Cleanup(dbxkms.Reset)

	memStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)
	keyStorage := &failMaterialGetStore{StorageBackend: memStorage}
	certStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)
	swBackend, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{KeyStorage: keyStorage})
	require.NoError(t, err)
	ks, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{Backend: swBackend, CertStorage: certStorage})
	require.NoError(t, err)
	require.NoError(t, dbxkms.Initialize(&dbxkms.Config{
		DefaultBackend: "software",
		Backends:       map[string]xkmssdk.ServiceBackend{"software": ks},
		Logger:         &mockLogger{},
	}))
	return memStorage, keyStorage
}

// TestXKMSBackend_DeleteKey_MaterialReadFailureKeepsOwnership: while the
// material store refuses reads, a lookup and a delete fail instead of
// reporting "not found", and the live key keeps its ownership record. Once
// reads recover the key works and deletes normally.
func TestXKMSBackend_DeleteKey_MaterialReadFailureKeepsOwnership(t *testing.T) {
	ctx := context.Background()
	store, keyStorage := newFailMaterialGetXKMS(t)
	kb := newOwnerTestBackend(t, ctx, store)
	const id = "tenant/acme/read-fail-k"
	_, err := kb.CreateKey(ctx, kb.algorithmToKeyAttributes(id, AlgAES256GCM))
	require.NoError(t, err)
	identity := kb.xkmsKeyIdentity(kb.xkmsKeyAttributes(id, AlgAES256GCM))

	kbR := newOwnerTestBackend(t, ctx, store)
	keyStorage.fail.Store(true)
	_, err = kbR.GetKeyMetadata(ctx, id)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrKeyNotFound)
	err = kbR.DeleteKey(ctx, id)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrKeyNotFound)
	ok, err := store.Exists(ctx, xkmsKeyOwnerRecord(identity))
	require.NoError(t, err)
	require.True(t, ok, "the live key keeps its ownership record")

	keyStorage.fail.Store(false)
	_, err = kbR.GetKeyMetadata(ctx, id)
	require.NoError(t, err)
	ct, err := kbR.Encrypt(ctx, id, []byte("still mine"), nil)
	require.NoError(t, err)
	pt, err := kbR.Decrypt(ctx, id, ct, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("still mine"), pt)
	require.NoError(t, kbR.DeleteKey(ctx, id))
	requireNoXKMSKey(t, ctx, kbR, store, id, AlgAES256GCM)
}
