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

package sshstore

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	sshscope "github.com/jeremyhahn/go-xkms/pkg/ssh/scope"
	xkmsstorage "github.com/jeremyhahn/go-xkms/pkg/storage"
	"github.com/jeremyhahn/go-xkms/pkg/storage/file"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
)

// legacyKeys are the keys seedLegacyStore generates, as (scope, owner, name).
var legacyKeys = []xkmsssh.GetKeyRequest{
	{Scope: "user", Owner: "alice", Name: "fleet-deploy"},
	{Scope: "service", Owner: "agent-quicbaas", Name: "agent-quicbaas-signing"},
}

const pinnedHost = "node01.example.internal"

// seedLegacyStore writes a legacy file store under a fresh data dir, in the
// legacy on-disk format: two keys (ed25519 and RSA), one host pin and one
// authorized-keys entry. It returns the store directory and each key's
// authorized_keys line as generated.
func seedLegacyStore(t *testing.T) (string, map[string]string) {
	t.Helper()
	dir := LegacyDir(t.TempDir())

	pubs := make(map[string]string, len(legacyKeys))
	for i, k := range legacyKeys {
		sc := sshscope.Scope(k.Scope)
		key, algo := newEd25519TestKey(t), "ed25519"
		if i == 1 {
			priv, err := rsa.GenerateKey(rand.Reader, 2048)
			require.NoError(t, err)
			key, algo = newTestKey(t, priv), "rsa"
		}
		putLegacyRecord(t, dir, sshscope.KindKeys, sc, k.Owner, k.Name, legacyKeyJSON(t, sc, k.Owner, k.Name, algo, key))
		pubs[k.Name] = key.line
	}

	host := newEd25519TestKey(t)
	putLegacyRecord(t, dir, sshscope.KindKnownHosts, sshscope.User, "alice", pinnedHost,
		legacyPinJSON(t, sshscope.User, "alice", pinnedHost, host.pub))
	putLegacyRecord(t, dir, sshscope.KindAuthorizedKeys, sshscope.User, "alice", "laptop",
		legacyAuthorizedJSON(t, sshscope.User, "alice", "laptop", pubs["fleet-deploy"]))
	return dir, pubs
}

// putLegacyRecord writes value at the record path of (kind, sc, owner, name)
// in the legacy file store at dir, and returns that path.
func putLegacyRecord(t *testing.T, dir string, kind sshscope.Kind, sc sshscope.Scope, owner, name string, value []byte) string {
	t.Helper()
	path, err := kind.RecordPath(sc, owner, name)
	require.NoError(t, err)
	backend, err := file.New(dir)
	require.NoError(t, err)
	defer func() { require.NoError(t, backend.Close()) }()
	require.NoError(t, backend.Put(context.Background(), path, value))
	return path
}

// keyRecordPath is where go-xkms's scope layout stores a user-scoped SSH
// key record.
func keyRecordPath(t *testing.T, owner, name string) string {
	t.Helper()
	path, err := sshscope.KindKeys.RecordPath(sshscope.User, owner, name)
	require.NoError(t, err)
	return path
}

func newSealed(t *testing.T) xkmssdk.StorageBackend {
	t.Helper()
	backend, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)
	return backend
}

// newSealedStores builds the go-xkms SSH DAO stores over sealed.
func newSealedStores(t *testing.T, sealed xkmssdk.StorageBackend) *xkmsssh.Stores {
	t.Helper()
	daos, err := xkmssdk.NewBackendDAOFactory(sealed)
	require.NoError(t, err)
	stores, err := xkmsssh.NewStores(daos)
	require.NoError(t, err)
	return stores
}

// newSealedClient is an embedded SSH client over the sealed stores.
func newSealedClient(t *testing.T, sealed xkmssdk.StorageBackend) xkmsssh.Client {
	t.Helper()
	client, err := xkmsssh.NewEmbedded(xkmsssh.EmbeddedOptions{Stores: newSealedStores(t, sealed)})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return client
}

// requireLegacyIntact asserts the legacy store still opens and still holds
// every seeded key as a decodable legacy record.
func requireLegacyIntact(t *testing.T, dir string) {
	t.Helper()
	backend, err := file.New(dir)
	require.NoError(t, err)
	defer func() { require.NoError(t, backend.Close()) }()
	for _, k := range legacyKeys {
		path, err := sshscope.KindKeys.RecordPath(sshscope.Scope(k.Scope), k.Owner, k.Name)
		require.NoError(t, err)
		value, err := backend.Get(context.Background(), path)
		require.NoError(t, err, "legacy key %s must survive a failed migration", k.Name)
		key, err := decodeLegacyKey(value)
		require.NoError(t, err, "legacy key %s must survive a failed migration", k.Name)
		clear(key.PEM)
	}
}

// recordBytes reads the raw legacy record for key.
func recordBytes(t *testing.T, dir, key string) []byte {
	t.Helper()
	backend, err := file.New(dir)
	require.NoError(t, err)
	defer func() { require.NoError(t, backend.Close()) }()
	value, err := backend.Get(context.Background(), key)
	require.NoError(t, err)
	return value
}

func TestMigrateLegacy_MovesVerifiesAndWipes(t *testing.T) {
	ctx := context.Background()
	dir, pubs := seedLegacyStore(t)
	sealed := newSealed(t)

	report, err := MigrateLegacy(ctx, dir, sealed, nil)
	require.NoError(t, err)

	assert.True(t, report.Found)
	assert.False(t, report.ResumedWipe)
	assert.Equal(t, dir, report.LegacyDir)
	assert.Equal(t, 2, report.Keys)
	assert.Equal(t, 1, report.KnownHosts)
	assert.Equal(t, 1, report.AuthorizedKeys)
	assert.Equal(t, 0, report.Other)
	assert.Equal(t, 4, report.Records())

	assert.NoDirExists(t, dir, "the plaintext store must be gone")
	assert.NoDirExists(t, dir+wipingSuffix, "the wipe must complete")

	client := newSealedClient(t, sealed)
	for _, k := range legacyKeys {
		got, err := client.Keys().Get(ctx, k)
		require.NoError(t, err)
		assert.Equal(t, pubs[k.Name], got.PublicKey)

		signer, err := client.Keys().Signer(ctx, k)
		require.NoError(t, err)
		pub, _, _, _, err := gossh.ParseAuthorizedKey([]byte(pubs[k.Name]))
		require.NoError(t, err)
		sig, err := signer.Sign(rand.Reader, []byte("challenge"))
		require.NoError(t, err)
		require.NoError(t, pub.Verify([]byte("challenge"), sig))
	}
	pins, err := client.KnownHosts().List(ctx, xkmsssh.ListKnownHostsRequest{Scope: "user", Owner: "alice"})
	require.NoError(t, err)
	require.Len(t, pins, 1)
	entries, err := client.AuthorizedKeys().List(ctx, xkmsssh.ListAuthorizedKeysRequest{Scope: "user", Owner: "alice"})
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestMigrateLegacy_NothingToMigrate(t *testing.T) {
	dir := LegacyDir(t.TempDir())

	report, err := MigrateLegacy(context.Background(), dir, newSealed(t), nil)
	require.NoError(t, err)
	assert.False(t, report.Found)
	assert.Zero(t, report.Records())
}

func TestMigrateLegacy_RetryAcceptsIdenticalRecords(t *testing.T) {
	ctx := context.Background()
	dir, _ := seedLegacyStore(t)
	sealed := newSealed(t)

	// A previous run copied these records and then failed.
	stores := newSealedStores(t, sealed)
	key := keyRecordPath(t, "alice", "fleet-deploy")
	legacyKey, err := decodeLegacyKey(recordBytes(t, dir, key))
	require.NoError(t, err)
	require.NoError(t, copyKey(ctx, stores.Keys, decodedKey{path: key, key: legacyKey}))
	pinPath, err := sshscope.KindKnownHosts.RecordPath(sshscope.User, "alice", pinnedHost)
	require.NoError(t, err)
	legacyPin, err := decodeLegacyPin(recordBytes(t, dir, pinPath))
	require.NoError(t, err)
	require.NoError(t, copyPin(ctx, stores.KnownHosts, decodedPin{path: pinPath, pin: legacyPin}))

	report, err := MigrateLegacy(ctx, dir, sealed, nil)
	require.NoError(t, err)
	assert.Equal(t, 4, report.Records())
	assert.NoDirExists(t, dir)
}

func TestMigrateLegacy_ConflictLeavesLegacyIntact(t *testing.T) {
	ctx := context.Background()
	dir, _ := seedLegacyStore(t)
	sealed := newSealed(t)

	// The sealed store already holds a different key under the same name.
	client := newSealedClient(t, sealed)
	other, err := client.Keys().Generate(ctx, xkmsssh.GenerateRequest{Scope: "user", Owner: "alice", Name: "fleet-deploy", Algorithm: "ed25519"})
	require.NoError(t, err)

	_, err = MigrateLegacy(ctx, dir, sealed, nil)
	var conflict *RecordConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, keyRecordPath(t, "alice", "fleet-deploy"), conflict.Key)

	stored, err := client.Keys().Get(ctx, legacyKeys[0])
	require.NoError(t, err)
	assert.Equal(t, other.PublicKey, stored.PublicKey, "a conflicting record must not be overwritten")
	assert.Equal(t, other.Fingerprint, stored.Fingerprint, "a conflicting record must not be overwritten")
	requireLegacyIntact(t, dir)
}

func TestMigrateLegacy_UnparseableKeyRecordLeavesLegacyIntact(t *testing.T) {
	ctx := context.Background()
	dir, _ := seedLegacyStore(t)
	putLegacyRecord(t, dir, sshscope.KindKeys, sshscope.User, "alice", "corrupt", []byte("not an envelope"))

	sealed := newSealed(t)
	_, err := MigrateLegacy(ctx, dir, sealed, nil)
	var unparsed *UnparsedKeysError
	require.ErrorAs(t, err, &unparsed, "an unparseable key record must stop the migration, got %v", err)
	assert.Equal(t, 3, unparsed.Records)
	assert.Equal(t, 2, unparsed.Parsed)
	requireLegacyIntact(t, dir)

	listed, err := newSealedClient(t, sealed).Keys().List(ctx, xkmsssh.ListKeysRequest{})
	require.NoError(t, err)
	assert.Empty(t, listed, "nothing is written before every key record decodes")
}

// failingBackend fails the configured operation and delegates the rest. The
// sealed DAO stores write through Put, PutIfAbsent and CompareAndSwap and
// check for a record by reading it, so failPut fails every write, failExists
// fails every read and existence check, and corrupt alters the fingerprint
// of every record read back.
type failingBackend struct {
	xkmssdk.StorageBackend
	failPut    bool
	failExists bool
	corrupt    bool
}

var errInjected = errors.New("injected storage failure")

func (b *failingBackend) Exists(ctx context.Context, key string) (bool, error) {
	if b.failExists {
		return false, errInjected
	}
	return b.StorageBackend.Exists(ctx, key)
}

func (b *failingBackend) Put(ctx context.Context, key string, value []byte) error {
	if b.failPut {
		return errInjected
	}
	return b.StorageBackend.Put(ctx, key, value)
}

func (b *failingBackend) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if b.failPut {
		return false, errInjected
	}
	return b.StorageBackend.PutIfAbsent(ctx, key, value)
}

func (b *failingBackend) CompareAndSwap(ctx context.Context, key string, expected, value []byte) (bool, []byte, error) {
	if b.failPut {
		return false, nil, errInjected
	}
	return b.StorageBackend.(xkmsstorage.CompareAndSwapper).CompareAndSwap(ctx, key, expected, value)
}

func (b *failingBackend) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	return b.StorageBackend.(xkmsstorage.CompareAndDeleter).CompareAndDelete(ctx, key, expected)
}

func (b *failingBackend) Get(ctx context.Context, key string) ([]byte, error) {
	if b.failExists {
		return nil, errInjected
	}
	value, err := b.StorageBackend.Get(ctx, key)
	if err != nil || !b.corrupt {
		return value, err
	}
	return tamperFingerprint(value), nil
}

// tamperFingerprint replaces the fingerprint in a stored DAO record, so the
// record still decodes but no longer matches what was written.
func tamperFingerprint(value []byte) []byte {
	var rec map[string]any
	if err := json.Unmarshal(value, &rec); err != nil {
		return append(value, '!')
	}
	body, ok := rec["body"].(map[string]any)
	if !ok {
		return append(value, '!')
	}
	body["fp"] = "SHA256:tampered"
	tampered, err := json.Marshal(rec)
	if err != nil {
		return append(value, '!')
	}
	return tampered
}

func TestMigrateLegacy_SealedStoreFailures(t *testing.T) {
	tests := []struct {
		name    string
		backend func(xkmssdk.StorageBackend) xkmssdk.StorageBackend
		check   func(t *testing.T, err error)
	}{
		{
			name: "write fails",
			backend: func(inner xkmssdk.StorageBackend) xkmssdk.StorageBackend {
				return &failingBackend{StorageBackend: inner, failPut: true}
			},
			check: func(t *testing.T, err error) {
				var sealedErr *SealedStoreError
				require.ErrorAs(t, err, &sealedErr)
				assert.Equal(t, "put", sealedErr.Op)
				assert.ErrorIs(t, err, errInjected)
			},
		},
		{
			name: "existence check fails",
			backend: func(inner xkmssdk.StorageBackend) xkmssdk.StorageBackend {
				return &failingBackend{StorageBackend: inner, failExists: true}
			},
			check: func(t *testing.T, err error) {
				var sealedErr *SealedStoreError
				require.ErrorAs(t, err, &sealedErr)
				assert.Equal(t, "exists", sealedErr.Op)
				assert.ErrorIs(t, err, errInjected)
			},
		},
		{
			name: "read-back differs",
			backend: func(inner xkmssdk.StorageBackend) xkmssdk.StorageBackend {
				return &failingBackend{StorageBackend: inner, corrupt: true}
			},
			check: func(t *testing.T, err error) {
				var mismatch *RecordMismatchError
				require.ErrorAs(t, err, &mismatch)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, _ := seedLegacyStore(t)
			_, err := MigrateLegacy(context.Background(), dir, tt.backend(newSealed(t)), nil)
			tt.check(t, err)
			requireLegacyIntact(t, dir)
		})
	}
}

// TestVerifyKey_RejectsForeignPrivateKey covers the check a byte comparison
// cannot: a sealed envelope whose public half was swapped for another key's
// passes the public-key comparison, and only the signature proves the private
// half is not that key's.
func TestVerifyKey_RejectsForeignPrivateKey(t *testing.T) {
	ctx := context.Background()
	sealed := newSealed(t)
	client := newSealedClient(t, sealed)

	real, err := client.Keys().Generate(ctx, xkmsssh.GenerateRequest{Scope: "user", Owner: "alice", Name: "real", Algorithm: "ed25519"})
	require.NoError(t, err)
	other, err := client.Keys().Generate(ctx, xkmsssh.GenerateRequest{Scope: "user", Owner: "alice", Name: "other", Algorithm: "ed25519"})
	require.NoError(t, err)

	// Graft "other"'s public half onto "real"'s private half.
	stores := newSealedStores(t, sealed)
	grafted, err := stores.Keys.Get(ctx, sshscope.User, "alice", "real")
	require.NoError(t, err)
	defer grafted.Destroy()
	otherKey, err := stores.Keys.Get(ctx, sshscope.User, "alice", "other")
	require.NoError(t, err)
	defer otherKey.Destroy()
	grafted.PublicKey = otherKey.PublicKey
	grafted.Fingerprint = otherKey.Fingerprint
	require.NoError(t, stores.Keys.Put(ctx, grafted))

	claimed := &xkmsssh.Key{Scope: "user", Owner: "alice", Name: "real", PublicKey: other.PublicKey, Fingerprint: other.Fingerprint}
	err = verifyKey(ctx, client.Keys(), claimed)
	var verifyErr *KeyVerificationError
	require.ErrorAs(t, err, &verifyErr)
	assert.Contains(t, verifyErr.Reason, "does not verify")
	assert.NotEqual(t, real.PublicKey, other.PublicKey)
}

func TestVerifyKey_Failures(t *testing.T) {
	ctx := context.Background()
	client := newSealedClient(t, newSealed(t))
	key, err := client.Keys().Generate(ctx, xkmsssh.GenerateRequest{Scope: "user", Owner: "alice", Name: "k", Algorithm: "ed25519"})
	require.NoError(t, err)

	tests := []struct {
		name   string
		legacy *xkmsssh.Key
		reason string
	}{
		{
			name:   "missing from the sealed store",
			legacy: &xkmsssh.Key{Scope: "user", Owner: "alice", Name: "absent", PublicKey: key.PublicKey, Fingerprint: key.Fingerprint},
			reason: "read sealed key",
		},
		{
			name:   "public key differs",
			legacy: &xkmsssh.Key{Scope: "user", Owner: "alice", Name: "k", PublicKey: "ssh-ed25519 AAAA", Fingerprint: key.Fingerprint},
			reason: "differs",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyKey(ctx, client.Keys(), tt.legacy)
			var verifyErr *KeyVerificationError
			require.ErrorAs(t, err, &verifyErr)
			assert.Contains(t, verifyErr.Reason, tt.reason)
		})
	}
	require.NoError(t, verifyKey(ctx, client.Keys(), key))
}

func TestMigrateLegacy_ResumesInterruptedWipe(t *testing.T) {
	dir, _ := seedLegacyStore(t)
	require.NoError(t, os.Rename(dir, dir+wipingSuffix))

	report, err := MigrateLegacy(context.Background(), dir, newSealed(t), nil)
	require.NoError(t, err)
	assert.True(t, report.Found)
	assert.True(t, report.ResumedWipe)
	assert.Zero(t, report.Records(), "an interrupted wipe was already migrated")
	assert.NoDirExists(t, dir+wipingSuffix)
}

func TestMigrateLegacy_InvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		dir    string
		sealed xkmssdk.StorageBackend
		field  string
	}{
		{name: "no legacy dir", dir: "", sealed: newSealed(t), field: "legacyDir"},
		{name: "no sealed store", dir: LegacyDir(t.TempDir()), sealed: nil, field: "sealed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := MigrateLegacy(context.Background(), tt.dir, tt.sealed, nil)
			var nilErr *NilInputError
			require.ErrorAs(t, err, &nilErr)
			assert.Equal(t, tt.field, nilErr.Field)
		})
	}
}

func TestMigrateLegacy_LegacyPathIsNotADirectory(t *testing.T) {
	dir := LegacyDir(t.TempDir())
	require.NoError(t, os.WriteFile(dir, []byte("x"), 0o600))

	_, err := MigrateLegacy(context.Background(), dir, newSealed(t), nil)
	var unexpected *UnexpectedEntryError
	require.ErrorAs(t, err, &unexpected)
	assert.Equal(t, dir, unexpected.Path)
	assert.FileExists(t, dir)
}

func TestLegacyPresent(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  bool
	}{
		{name: "absent", setup: func(*testing.T, string) {}, want: false},
		{name: "legacy store", setup: func(t *testing.T, dir string) { require.NoError(t, os.MkdirAll(dir, 0o700)) }, want: true},
		{name: "interrupted wipe", setup: func(t *testing.T, dir string) { require.NoError(t, os.MkdirAll(dir+wipingSuffix, 0o700)) }, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := LegacyDir(t.TempDir())
			tt.setup(t, dir)
			got, err := LegacyPresent(dir)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLegacyPresent_NotADirectory(t *testing.T) {
	dir := LegacyDir(t.TempDir())
	require.NoError(t, os.WriteFile(dir, []byte("x"), 0o600))

	_, err := LegacyPresent(dir)
	var unexpected *UnexpectedEntryError
	require.ErrorAs(t, err, &unexpected)
}

func TestLegacyDir(t *testing.T) {
	assert.Equal(t, filepath.Join("/var/lib/quicbaas", "xkms-ssh"), LegacyDir("/var/lib/quicbaas"))
}
