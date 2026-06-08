// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errKVStore wraps inMemKV and injects faults on selected operations so
// the KVBackend error-propagation branches can be exercised. Each fault
// is keyed by a substring of the key; when the operation's key contains
// the substring the configured error is returned instead of the real
// result. This drives the "store returned an error" branches that the
// fault-free in-memory fake can never reach.
type errKVStore struct {
	*inMemKV
	getErrOn    string // substring; Get returns getErr when key contains it
	getErr      error
	putErrOn    string
	putErr      error
	deleteErrOn string
	deleteErr   error
}

func newErrKV() *errKVStore {
	return &errKVStore{inMemKV: newInMemKV()}
}

func (e *errKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	if e.getErr != nil && strings.Contains(key, e.getErrOn) {
		return nil, e.getErr
	}
	return e.inMemKV.Get(ctx, key)
}

func (e *errKVStore) Put(ctx context.Context, key string, value []byte) error {
	if e.putErr != nil && strings.Contains(key, e.putErrOn) {
		return e.putErr
	}
	return e.inMemKV.Put(ctx, key, value)
}

func (e *errKVStore) Delete(ctx context.Context, key string) error {
	if e.deleteErr != nil && strings.Contains(key, e.deleteErrOn) {
		return e.deleteErr
	}
	return e.inMemKV.Delete(ctx, key)
}

var errInjected = errors.New("injected store failure")

// markVersionDestroyed flips the stored entry's destroyed flag to true so
// the read/rollback paths that guard against destroyed versions can be
// exercised. The kvEntry serialises destroyed without omitempty, so the
// literal "destroyed":false is always present in the raw JSON.
func markVersionDestroyed(t *testing.T, kv *inMemKV, path string, version int) {
	t.Helper()
	ctx := context.Background()
	raw, err := kv.Get(ctx, versionKey(path, version))
	require.NoError(t, err)
	require.Contains(t, string(raw), `"destroyed":false`)
	corrupted := strings.Replace(string(raw), `"destroyed":false`, `"destroyed":true`, 1)
	require.NoError(t, kv.Put(ctx, versionKey(path, version), []byte(corrupted)))
}

// TestKVBackend_LoadCurrentGetError covers loadCurrent's store.Get error
// branch, which every read/write verb depends on. A backend store that
// cannot read its pointer must surface a wrapped SecretsCauseError.
func TestKVBackend_LoadCurrentGetError(t *testing.T) {
	kv := newErrKV()
	kv.getErr = errInjected
	kv.getErrOn = "/current"
	b := NewKVBackend(kv)
	ctx := context.Background()

	_, _, err := b.Get(ctx, "app/secret")
	require.Error(t, err)
	var ce *SecretsCauseError
	require.True(t, errors.As(err, &ce), "want *SecretsCauseError, got %T", err)
	assert.ErrorIs(t, err, errInjected)
}

// TestKVBackend_LoadCurrentUnmarshalError covers the branch where the
// current pointer exists but holds corrupt JSON.
func TestKVBackend_LoadCurrentUnmarshalError(t *testing.T) {
	kv := newInMemKV()
	require.NoError(t, kv.Put(context.Background(), currentKey("corrupt/ptr"), []byte("{not json")))
	b := NewKVBackend(kv)

	_, _, err := b.Get(context.Background(), "corrupt/ptr")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDeserializationFailed)
}

// TestKVBackend_WriteEntryPutError covers writeEntry's store.Put error
// branch via Store: a version-write failure must abort the Store.
func TestKVBackend_WriteEntryPutError(t *testing.T) {
	kv := newErrKV()
	kv.putErr = errInjected
	kv.putErrOn = "/v/"
	b := NewKVBackend(kv)

	err := b.Store(context.Background(), "app/secret", map[string][]byte{"k": []byte("v")}, nil)
	require.Error(t, err)
	var ce *SecretsCauseError
	require.True(t, errors.As(err, &ce), "want *SecretsCauseError, got %T", err)
	assert.ErrorIs(t, err, errInjected)
}

// TestKVBackend_WriteCurrentPutError covers writeCurrent's store.Put
// error branch: the version entry writes fine but the pointer update
// fails.
func TestKVBackend_WriteCurrentPutError(t *testing.T) {
	kv := newErrKV()
	kv.putErr = errInjected
	kv.putErrOn = "/current"
	b := NewKVBackend(kv)

	err := b.Store(context.Background(), "app/secret", map[string][]byte{"k": []byte("v")}, nil)
	require.Error(t, err)
	var ce *SecretsCauseError
	require.True(t, errors.As(err, &ce), "want *SecretsCauseError, got %T", err)
	assert.ErrorIs(t, err, errInjected)
}

// TestKVBackend_GetVersionStoreError covers getVersion's store.Get error
// branch via the explicit GetVersion verb.
func TestKVBackend_GetVersionStoreError(t *testing.T) {
	kv := newErrKV()
	kv.getErr = errInjected
	kv.getErrOn = "/v/"
	b := NewKVBackend(kv)

	_, _, err := b.GetVersion(context.Background(), "app/secret", 1)
	require.Error(t, err)
	var ce *SecretsCauseError
	require.True(t, errors.As(err, &ce), "want *SecretsCauseError, got %T", err)
	assert.ErrorIs(t, err, errInjected)
}

// TestKVBackend_GetVersionNotFound covers getVersion's raw == nil branch:
// the version key is absent.
func TestKVBackend_GetVersionNotFound(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	_, _, err := b.GetVersion(context.Background(), "app/secret", 7)
	assert.ErrorIs(t, err, ErrVersionNotFound)
}

// TestKVBackend_GetVersionUnmarshalError covers getVersion's unmarshal
// error branch via a corrupt version entry.
func TestKVBackend_GetVersionUnmarshalError(t *testing.T) {
	kv := newInMemKV()
	require.NoError(t, kv.Put(context.Background(), versionKey("app/secret", 1), []byte("{bad")))
	b := NewKVBackend(kv)

	_, _, err := b.GetVersion(context.Background(), "app/secret", 1)
	assert.ErrorIs(t, err, ErrDeserializationFailed)
}

// TestKVBackend_GetVersionDestroyed covers getVersion's Destroyed branch.
func TestKVBackend_GetVersionDestroyed(t *testing.T) {
	kv := newInMemKV()
	b := NewKVBackend(kv)
	ctx := context.Background()
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v")}, nil))

	markVersionDestroyed(t, kv, "app/secret", 1)

	_, _, err := b.GetVersion(ctx, "app/secret", 1)
	assert.ErrorIs(t, err, ErrVersionDestroyed)
}

// TestKVBackend_DestroyVersionDeleteError covers Destroy's branch where
// deleting a version entry fails mid-loop.
func TestKVBackend_DestroyVersionDeleteError(t *testing.T) {
	kv := newErrKV()
	b := NewKVBackend(kv)
	ctx := context.Background()
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v")}, nil))

	kv.deleteErr = errInjected
	kv.deleteErrOn = "/v/"
	err := b.Destroy(ctx, "app/secret")
	require.Error(t, err)
	var ce *SecretsCauseError
	require.True(t, errors.As(err, &ce), "want *SecretsCauseError, got %T", err)
	assert.ErrorIs(t, err, errInjected)
}

// TestKVBackend_RollbackVersionTooHigh covers Rollback's guard rejecting
// a version greater than the current one.
func TestKVBackend_RollbackVersionTooHigh(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v")}, nil))

	assert.ErrorIs(t, b.Rollback(ctx, "app/secret", 99), ErrVersionNotFound)
}

// TestKVBackend_RollbackMissingTargetVersion covers Rollback's branch
// where the current pointer claims a version that has no readable entry.
func TestKVBackend_RollbackMissingTargetVersion(t *testing.T) {
	kv := newErrKV()
	b := NewKVBackend(kv)
	ctx := context.Background()
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v")}, nil))

	kv.getErr = errInjected
	kv.getErrOn = "/v/"
	assert.ErrorIs(t, b.Rollback(ctx, "app/secret", 1), ErrVersionNotFound)
}

// TestKVBackend_RollbackCorruptTargetVersion covers Rollback's unmarshal
// error branch on the target version entry.
func TestKVBackend_RollbackCorruptTargetVersion(t *testing.T) {
	kv := newInMemKV()
	b := NewKVBackend(kv)
	ctx := context.Background()
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v")}, nil))

	require.NoError(t, kv.Put(ctx, versionKey("app/secret", 1), []byte("{bad")))
	err := b.Rollback(ctx, "app/secret", 1)
	assert.ErrorIs(t, err, ErrDeserializationFailed)
}

// TestKVBackend_RollbackDestroyedTarget covers Rollback's branch refusing
// to restore a destroyed version.
func TestKVBackend_RollbackDestroyedTarget(t *testing.T) {
	kv := newInMemKV()
	b := NewKVBackend(kv)
	ctx := context.Background()
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v")}, nil))

	markVersionDestroyed(t, kv, "app/secret", 1)

	assert.ErrorIs(t, b.Rollback(ctx, "app/secret", 1), ErrVersionDestroyed)
}

// TestKVBackend_ListVersionsSkipsUnreadable covers ListVersions' branches
// that skip versions which cannot be read or parsed, returning only the
// well-formed ones.
func TestKVBackend_ListVersionsSkipsUnreadable(t *testing.T) {
	kv := newInMemKV()
	b := NewKVBackend(kv)
	ctx := context.Background()

	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("a")}, nil))
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("b")}, nil))
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("c")}, nil))

	// Corrupt version 2 so it is skipped; remove version 3's raw entry.
	require.NoError(t, kv.Put(ctx, versionKey("app/secret", 2), []byte("{bad")))
	require.NoError(t, kv.Delete(ctx, versionKey("app/secret", 3)))

	versions, err := b.ListVersions(ctx, "app/secret")
	require.NoError(t, err)
	require.Len(t, versions, 1)
	assert.Equal(t, 1, versions[0].Version)
}

// TestKVBackend_ListVersionsDeletedAt covers ListVersions' branch that
// copies a non-nil DeletedAt into the returned metadata.
func TestKVBackend_ListVersionsDeletedAt(t *testing.T) {
	kv := newInMemKV()
	b := NewKVBackend(kv)
	ctx := context.Background()
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v")}, nil))

	raw, _ := kv.Get(ctx, versionKey("app/secret", 1))
	withDeletedAt := strings.Replace(
		string(raw),
		`"updated_at"`,
		`"deleted_at":"2026-01-01T00:00:00Z","updated_at"`,
		1,
	)
	require.NoError(t, kv.Put(ctx, versionKey("app/secret", 1), []byte(withDeletedAt)))

	versions, err := b.ListVersions(ctx, "app/secret")
	require.NoError(t, err)
	require.Len(t, versions, 1)
	require.NotNil(t, versions[0].DeletedAt)
}

// TestKVBackend_EmptyPathGuards covers the ErrInvalidPath guard on every
// verb that accepts a path. These guards reject malformed callers before
// any store access happens.
func TestKVBackend_EmptyPathGuards(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()

	assert.ErrorIs(t, b.Store(ctx, "", map[string][]byte{"k": []byte("v")}, nil), ErrInvalidPath)
	_, _, err := b.Get(ctx, "")
	assert.ErrorIs(t, err, ErrInvalidPath)
	_, _, err = b.GetVersion(ctx, "", 1)
	assert.ErrorIs(t, err, ErrInvalidPath)
	assert.ErrorIs(t, b.Delete(ctx, ""), ErrInvalidPath)
	assert.ErrorIs(t, b.Destroy(ctx, ""), ErrInvalidPath)
	_, err = b.ListVersions(ctx, "")
	assert.ErrorIs(t, err, ErrInvalidPath)
	assert.ErrorIs(t, b.Rollback(ctx, "", 1), ErrInvalidPath)
}

// TestKVBackend_RollbackVersionNotPositive covers Rollback's positive
// version guard.
func TestKVBackend_RollbackVersionNotPositive(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	assert.ErrorIs(t, b.Rollback(context.Background(), "app/secret", 0), ErrVersionMustBePositive)
}

// TestKVBackend_StoreLoadCurrentError covers Store's branch where the
// initial loadCurrent fails before any write is attempted.
func TestKVBackend_StoreLoadCurrentError(t *testing.T) {
	kv := newErrKV()
	kv.getErr = errInjected
	kv.getErrOn = "/current"
	b := NewKVBackend(kv)

	err := b.Store(context.Background(), "app/secret", map[string][]byte{"k": []byte("v")}, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, errInjected)
}

// TestKVBackend_ListVersionsLoadError covers ListVersions' branch where
// the current pointer read itself fails.
func TestKVBackend_ListVersionsLoadError(t *testing.T) {
	kv := newErrKV()
	kv.getErr = errInjected
	kv.getErrOn = "/current"
	b := NewKVBackend(kv)

	_, err := b.ListVersions(context.Background(), "app/secret")
	require.Error(t, err)
	assert.ErrorIs(t, err, errInjected)
}

// TestKVBackend_ListVersionsNotFound covers ListVersions on a path that
// has never been stored.
func TestKVBackend_ListVersionsNotFound(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	_, err := b.ListVersions(context.Background(), "never/stored")
	assert.ErrorIs(t, err, ErrSecretNotFound)
}

// TestKVBackend_RollbackWriteEntryError covers the branch where Rollback
// loads a valid target version but the new-version write fails.
func TestKVBackend_RollbackWriteEntryError(t *testing.T) {
	kv := newErrKV()
	b := NewKVBackend(kv)
	ctx := context.Background()
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("a")}, nil))
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("b")}, nil))

	// Fail only the brand-new version write (v/3) by failing all version puts.
	kv.putErr = errInjected
	kv.putErrOn = "/v/"
	err := b.Rollback(ctx, "app/secret", 1)
	require.Error(t, err)
	assert.ErrorIs(t, err, errInjected)
}

// TestKVBackend_ListSkipsMalformedKeys covers List's parsing branches
// that ignore keys which are neither a "current" pointer nor a versioned
// entry, so stray keys under the prefix are not mistaken for secrets.
func TestKVBackend_ListSkipsMalformedKeys(t *testing.T) {
	kv := newInMemKV()
	b := NewKVBackend(kv)
	ctx := context.Background()

	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v")}, nil))

	// A single-component key (len(parts) < 2) and a key whose final
	// component is neither "current" nor under a "v/" segment.
	require.NoError(t, kv.Put(ctx, kvBackendKeyPrefix+"stray", []byte("x")))
	require.NoError(t, kv.Put(ctx, kvBackendKeyPrefix+"app/secret/weird", []byte("x")))

	paths, err := b.List(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"app/secret"}, paths)
}
