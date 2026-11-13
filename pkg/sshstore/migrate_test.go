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
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	sshscope "github.com/jeremyhahn/go-xkms/pkg/ssh/scope"
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

// seedLegacyStore writes a legacy file store under a fresh data dir: two keys,
// one host pin and one authorized-keys entry. It returns the store directory
// and each key's authorized_keys line as generated.
func seedLegacyStore(t *testing.T) (string, map[string]string) {
	t.Helper()
	ctx := context.Background()
	dir := LegacyDir(t.TempDir())

	client, err := xkmsssh.NewEmbeddedFile(xkmsssh.EmbeddedFileOptions{RootDir: dir})
	require.NoError(t, err)

	pubs := make(map[string]string, len(legacyKeys))
	for _, k := range legacyKeys {
		key, err := client.Keys().Generate(ctx, xkmsssh.GenerateRequest{
			Scope: k.Scope, Owner: k.Owner, Name: k.Name, Algorithm: "ed25519",
		})
		require.NoError(t, err)
		pubs[k.Name] = key.PublicKey
	}

	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	hostSigner, err := gossh.NewSignerFromKey(hostPriv)
	require.NoError(t, err)
	hostPub := hostSigner.PublicKey()
	require.NoError(t, client.KnownHosts().Add(ctx, xkmsssh.AddKnownHostRequest{
		Scope: "user", Owner: "alice", Host: pinnedHost,
		Fingerprint: gossh.FingerprintSHA256(hostPub),
		PublicKey:   string(gossh.MarshalAuthorizedKey(hostPub)),
	}))

	require.NoError(t, client.AuthorizedKeys().Add(ctx, xkmsssh.AddAuthorizedKeyRequest{
		Scope: "user", Owner: "alice", Name: "laptop", Line: pubs["fleet-deploy"],
	}))

	require.NoError(t, client.Close())
	return dir, pubs
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

// requireLegacyIntact asserts the legacy store still opens and still serves
// every seeded key.
func requireLegacyIntact(t *testing.T, dir string) {
	t.Helper()
	client, err := xkmsssh.NewEmbeddedFile(xkmsssh.EmbeddedFileOptions{RootDir: dir})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	for _, k := range legacyKeys {
		_, err := client.Keys().Get(context.Background(), k)
		require.NoError(t, err, "legacy key %s must survive a failed migration", k.Name)
	}
}

// recordBytes reads the raw legacy record for key.
func recordBytes(t *testing.T, dir, key string) []byte {
	t.Helper()
	backend, err := xkmsssh.NewFileStorage(dir)
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

	client, err := xkmsssh.NewEmbedded(xkmsssh.EmbeddedOptions{Backend: sealed})
	require.NoError(t, err)
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

	// A previous run copied this record and then failed.
	key := keyRecordPath(t, "alice", "fleet-deploy")
	require.NoError(t, sealed.Put(ctx, key, recordBytes(t, dir, key)))

	report, err := MigrateLegacy(ctx, dir, sealed, nil)
	require.NoError(t, err)
	assert.Equal(t, 4, report.Records())
	assert.NoDirExists(t, dir)
}

func TestMigrateLegacy_ConflictLeavesLegacyIntact(t *testing.T) {
	ctx := context.Background()
	dir, _ := seedLegacyStore(t)
	sealed := newSealed(t)

	key := keyRecordPath(t, "alice", "fleet-deploy")
	require.NoError(t, sealed.Put(ctx, key, []byte(`{"v":2,"name":"someone-else"}`)))

	_, err := MigrateLegacy(ctx, dir, sealed, nil)
	var conflict *RecordConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, key, conflict.Key)

	stored, err := sealed.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, `{"v":2,"name":"someone-else"}`, string(stored), "a conflicting record must not be overwritten")
	requireLegacyIntact(t, dir)
}

func TestMigrateLegacy_UnparseableKeyRecordLeavesLegacyIntact(t *testing.T) {
	ctx := context.Background()
	dir, _ := seedLegacyStore(t)

	backend, err := xkmsssh.NewFileStorage(dir)
	require.NoError(t, err)
	require.NoError(t, backend.Put(ctx, keyRecordPath(t, "alice", "corrupt"), []byte("not an envelope")))
	require.NoError(t, backend.Close())

	_, err = MigrateLegacy(ctx, dir, newSealed(t), nil)
	var verifyErr *KeyVerificationError
	var unparsed *UnparsedKeysError
	require.True(t, errors.As(err, &verifyErr) || errors.As(err, &unparsed),
		"an unparseable key record must stop the migration, got %v", err)
	requireLegacyIntact(t, dir)
}

// failingBackend fails the configured operation and delegates the rest.
type failingBackend struct {
	xkmssdk.StorageBackend
	failPut    bool
	failExists bool
	corrupt    bool
}

func (b *failingBackend) Exists(ctx context.Context, key string) (bool, error) {
	if b.failExists {
		return false, errInjected
	}
	return b.StorageBackend.Exists(ctx, key)
}

var errInjected = errors.New("injected storage failure")

func (b *failingBackend) Put(ctx context.Context, key string, value []byte) error {
	if b.failPut {
		return errInjected
	}
	return b.StorageBackend.Put(ctx, key, value)
}

func (b *failingBackend) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := b.StorageBackend.Get(ctx, key)
	if err != nil || !b.corrupt {
		return value, err
	}
	return append(value, '!'), nil
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
	client, err := xkmsssh.NewEmbedded(xkmsssh.EmbeddedOptions{Backend: sealed})
	require.NoError(t, err)

	real, err := client.Keys().Generate(ctx, xkmsssh.GenerateRequest{Scope: "user", Owner: "alice", Name: "real", Algorithm: "ed25519"})
	require.NoError(t, err)
	other, err := client.Keys().Generate(ctx, xkmsssh.GenerateRequest{Scope: "user", Owner: "alice", Name: "other", Algorithm: "ed25519"})
	require.NoError(t, err)

	// Graft "other"'s public half onto "real"'s private half.
	key := keyRecordPath(t, "alice", "real")
	raw, err := sealed.Get(ctx, key)
	require.NoError(t, err)
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(raw, &envelope))
	envelope["pub"] = other.PublicKey
	envelope["fp"] = other.Fingerprint
	tampered, err := json.Marshal(envelope)
	require.NoError(t, err)
	require.NoError(t, sealed.Put(ctx, key, tampered))

	claimed := &xkmsssh.Key{Scope: "user", Owner: "alice", Name: "real", PublicKey: other.PublicKey, Fingerprint: other.Fingerprint}
	err = verifyKey(ctx, client.Keys(), claimed)
	var verifyErr *KeyVerificationError
	require.ErrorAs(t, err, &verifyErr)
	assert.Contains(t, verifyErr.Reason, "does not verify")
	assert.NotEqual(t, real.PublicKey, other.PublicKey)
}

func TestVerifyKey_Failures(t *testing.T) {
	ctx := context.Background()
	sealed := newSealed(t)
	client, err := xkmsssh.NewEmbedded(xkmsssh.EmbeddedOptions{Backend: sealed})
	require.NoError(t, err)
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
