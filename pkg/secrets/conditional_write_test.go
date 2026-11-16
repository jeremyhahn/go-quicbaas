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

package secrets

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// conditionalBackends are the backends whose StoreVersionIfCurrent the
// rotation relies on.
func conditionalBackends(t *testing.T) map[string]ConditionalVersionedBackend {
	t.Helper()
	return map[string]ConditionalVersionedBackend{
		"memory":    NewMemoryBackend(),
		"kv":        NewKVBackend(newInMemKV()),
		"encrypted": newGuardedEncryptedBackend(t, newGuardedMockKV(), newTestMockSecretsEncryptor()),
	}
}

// newGuardedEncryptedBackend is an EncryptedBackend over store, which must
// offer the guarded verbs a conditional write needs.
func newGuardedEncryptedBackend(t *testing.T, store interfaces.KVStore, enc *testMockSecretsEncryptor) *EncryptedBackend {
	t.Helper()
	eb, err := NewEncryptedBackend(&EncryptedBackendOptions{
		Store:                store,
		Encryptor:            enc,
		DefaultKeyIdentifier: string(DEKIDGlobal),
		Logger:               newTestMockLogger(),
	})
	require.NoError(t, err)
	return eb
}

// guardedMockKV is the encrypted backend's mock store with the guarded
// verbs. onCAS runs once, just before the first CompareAndSwap reaches the
// store: the window between a writer's check and its publication.
type guardedMockKV struct {
	*testMockKVStore
	onCAS func()
}

func newGuardedMockKV() *guardedMockKV {
	return &guardedMockKV{testMockKVStore: newTestMockKVStore()}
}

func (k *guardedMockKV) PutIfAbsent(_ context.Context, key string, value []byte) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.data[key]; ok {
		return false, nil
	}
	k.data[key] = bytes.Clone(value)
	return true, nil
}

func (k *guardedMockKV) CompareAndDelete(_ context.Context, key string, expected []byte) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if current, ok := k.data[key]; !ok || !bytes.Equal(current, expected) {
		return false, nil
	}
	delete(k.data, key)
	return true, nil
}

func (k *guardedMockKV) CompareAndSwap(_ context.Context, key string, expected, next []byte) (bool, error) {
	if hook := k.onCAS; hook != nil {
		k.onCAS = nil
		hook()
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if current, ok := k.data[key]; !ok || !bytes.Equal(current, expected) {
		return false, nil
	}
	k.data[key] = bytes.Clone(next)
	return true, nil
}

// readCurrent returns the record a conditional writer read at path.
func readCurrent(t *testing.T, b ConditionalVersionedBackend, path string, version int) ReadVersion {
	t.Helper()
	data, metadata, err := b.Get(context.Background(), path)
	require.NoError(t, err)
	return ReadVersion{Version: version, Data: data, Metadata: metadata}
}

func requireStale(t *testing.T, err error) *StaleVersionError {
	t.Helper()
	require.ErrorIs(t, err, ErrWriteConflict)
	var stale *StaleVersionError
	require.True(t, errors.As(err, &stale), "a conflict carries a *StaleVersionError: %v", err)
	return stale
}

func TestStoreVersionIfCurrent_WritesOverTheRecordRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, b := range conditionalBackends(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, b.Store(ctx, "app/s", map[string][]byte{"k": []byte("v1")}, map[string]string{"owner": "a"}))
			read := readCurrent(t, b, "app/s", 1)

			v, err := b.StoreVersionIfCurrent(ctx, "app/s", read, read.Data, map[string]string{"owner": "a", "rotation_reason": "scheduled"})
			require.NoError(t, err)
			assert.Equal(t, 2, v)

			// The same read is now stale: version 2 superseded it.
			_, err = b.StoreVersionIfCurrent(ctx, "app/s", read, read.Data, nil)
			stale := requireStale(t, err)
			assert.Equal(t, 1, stale.Expected)
			assert.Equal(t, 2, stale.Current)
		})
	}
}

func TestStoreVersionIfCurrent_RefusesARecreationAtTheSameVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, b := range conditionalBackends(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, b.Store(ctx, "app/s", map[string][]byte{"k": []byte("old")}, nil))
			read := readCurrent(t, b, "app/s", 1)

			require.NoError(t, b.Destroy(ctx, "app/s"))
			require.NoError(t, b.Store(ctx, "app/s", map[string][]byte{"k": []byte("new")}, nil))

			_, err := b.StoreVersionIfCurrent(ctx, "app/s", read, read.Data, nil)
			stale := requireStale(t, err)
			assert.True(t, stale.Replaced, "the record at version 1 is not the one read")
			assert.Contains(t, stale.Error(), "destroyed and recreated")

			data, _, err := b.Get(ctx, "app/s")
			require.NoError(t, err)
			assert.Equal(t, []byte("new"), data["k"], "the recreation stands")
		})
	}
}

func TestStoreVersionIfCurrent_RefusesADeletedSecretAndNoVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, b := range conditionalBackends(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, b.Store(ctx, "app/s", map[string][]byte{"k": []byte("v")}, nil))
			read := readCurrent(t, b, "app/s", 1)

			_, err := b.StoreVersionIfCurrent(ctx, "app/s", ReadVersion{}, read.Data, nil)
			requireStale(t, err)

			require.NoError(t, b.Delete(ctx, "app/s"))
			_, err = b.StoreVersionIfCurrent(ctx, "app/s", read, read.Data, nil)
			requireStale(t, err)
		})
	}
}

// TestStoreVersionIfCurrent_RefusesAStoreWithoutGuardedVerbs: a backend's
// lock covers only its own instance, so on a store that cannot
// compare-and-swap a conditional write is refused, not written blind.
func TestStoreVersionIfCurrent_RefusesAStoreWithoutGuardedVerbs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	backends := map[string]ConditionalVersionedBackend{
		"kv":        NewKVBackend(&plainKV{base: newInMemKV()}),
		"encrypted": createTestEncryptedBackend(t),
	}
	for name, b := range backends {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, b.Store(ctx, "app/s", map[string][]byte{"k": []byte("v")}, nil))
			read := readCurrent(t, b, "app/s", 1)

			_, err := b.StoreVersionIfCurrent(ctx, "app/s", read, read.Data, nil)
			require.ErrorIs(t, err, ErrConditionalWriteUnsupported)
			versions, err := b.ListVersions(ctx, "app/s")
			require.NoError(t, err)
			assert.Len(t, versions, 1, "nothing is written")
		})
	}
}

// TestEncryptedBackend_ConditionalWriteLosesToAnotherInstance: a second
// EncryptedBackend on the same store (its own lock) deletes the secret
// after the first one's check and before its publication. The publication
// refuses, and the secret stays deleted.
func TestEncryptedBackend_ConditionalWriteLosesToAnotherInstance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newGuardedMockKV()
	enc := newTestMockSecretsEncryptor()
	rotator := newGuardedEncryptedBackend(t, store, enc)
	other := newGuardedEncryptedBackend(t, store, enc)

	require.NoError(t, rotator.Store(ctx, "app/s", map[string][]byte{"k": []byte("v")}, nil))
	read := readCurrent(t, rotator, "app/s", 1)
	store.onCAS = func() { require.NoError(t, other.Delete(ctx, "app/s")) }

	_, err := rotator.StoreVersionIfCurrent(ctx, "app/s", read, read.Data, nil)
	requireStale(t, err)
	_, _, err = rotator.Get(ctx, "app/s")
	require.ErrorIs(t, err, ErrSecretDeleted, "the delete is not overwritten")
}

// TestEncryptedBackend_ConditionalWriteKeepsTheRecordsKey: a rotation of a
// secret stored under a tenant DEK and a non-default algorithm encrypts the
// new version under the same key and algorithm, not the backend's defaults.
func TestEncryptedBackend_ConditionalWriteKeepsTheRecordsKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	enc := newTestMockSecretsEncryptor()
	tenantKey := string(NewTenantDEKID("acme"))
	enc.AddKey(tenantKey, make([]byte, 32))
	eb := newGuardedEncryptedBackend(t, newGuardedMockKV(), enc)

	require.NoError(t, eb.StoreWithOptions(ctx, "app/s", map[string][]byte{"k": []byte("v")}, nil,
		&StoreOptions{KeyIdentifier: tenantKey, Algorithm: "CHACHA20-POLY1305"}))
	read := readCurrent(t, eb, "app/s", 1)

	v, err := eb.StoreVersionIfCurrent(ctx, "app/s", read, read.Data, read.Metadata)
	require.NoError(t, err)
	require.Equal(t, 2, v)

	versions, err := eb.loadVersions(ctx, "app/s")
	require.NoError(t, err)
	latest := versions.Versions[len(versions.Versions)-1]
	assert.Equal(t, tenantKey, latest.KeyIdentifier)
	assert.Equal(t, "CHACHA20-POLY1305", latest.Algorithm)
	data, _, err := eb.Get(ctx, "app/s")
	require.NoError(t, err)
	assert.Equal(t, []byte("v"), data["k"])
}
