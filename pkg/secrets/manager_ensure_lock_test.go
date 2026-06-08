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
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLocker is a minimal qrdbsdk.Locker used to drive EnsureSecret's
// distributed-lock acquire/release branches without a live qrdb engine.
// Only Lock and Unlock are exercised by EnsureSecret; the remaining
// interface methods are present solely to satisfy the type and panic if
// the code under test ever calls them unexpectedly.
type fakeLocker struct {
	lockErr     error // returned from Lock when non-nil
	unlockErr   error // returned from Unlock when non-nil
	lockCalls   int
	unlockCalls int
}

func (f *fakeLocker) Lock(_ context.Context, opts qrdbsdk.LockOptions) (*qrdbsdk.Lock, error) {
	f.lockCalls++
	if f.lockErr != nil {
		return nil, f.lockErr
	}
	return &qrdbsdk.Lock{Key: opts.Key, FencingToken: 42}, nil
}

func (f *fakeLocker) Unlock(_ context.Context, _ string, _ int64) error {
	f.unlockCalls++
	return f.unlockErr
}

func (f *fakeLocker) TryLock(context.Context, qrdbsdk.LockOptions) (*qrdbsdk.Lock, error) {
	panic("fakeLocker.TryLock should not be called by EnsureSecret")
}

func (f *fakeLocker) Extend(context.Context, string, int64) (*qrdbsdk.Lock, error) {
	panic("fakeLocker.Extend should not be called by EnsureSecret")
}

func (f *fakeLocker) Status(context.Context, string) (*qrdbsdk.Lock, error) {
	panic("fakeLocker.Status should not be called by EnsureSecret")
}

func (f *fakeLocker) List(context.Context, string) ([]*qrdbsdk.Lock, error) {
	panic("fakeLocker.List should not be called by EnsureSecret")
}

func newManagerWithLocker(t *testing.T, l qrdbsdk.Locker) *Manager {
	t.Helper()
	m, err := NewManager(&ManagerOptions{Backend: NewMemoryBackend(), Locker: l})
	require.NoError(t, err)
	return m
}

// TestEnsureSecret_DistributedLockHappyPath covers acquireEnsureLock's
// distributed-locker branch: the lock is acquired before the critical
// section and released afterwards using the fencing token.
func TestEnsureSecret_DistributedLockHappyPath(t *testing.T) {
	locker := &fakeLocker{}
	m := newManagerWithLocker(t, locker)
	ctx := context.Background()

	got, generated, err := m.EnsureSecret(ctx, "app/lock-test",
		&SecretSpec{Length: 16, Encoding: EncodingBase64})
	require.NoError(t, err)
	assert.True(t, generated)
	assert.NotEmpty(t, got)
	assert.Equal(t, 1, locker.lockCalls, "Lock must be acquired exactly once")
	assert.Equal(t, 1, locker.unlockCalls, "lock must be released exactly once")
}

// TestEnsureSecret_DistributedLockAcquireError covers the branch where
// the distributed Lock call fails; EnsureSecret must surface an internal
// error and never touch the backend.
func TestEnsureSecret_DistributedLockAcquireError(t *testing.T) {
	lockFailure := errors.New("lock service unavailable")
	locker := &fakeLocker{lockErr: lockFailure}
	m := newManagerWithLocker(t, locker)

	_, _, err := m.EnsureSecret(context.Background(), "app/lock-fail",
		&SecretSpec{Length: 16, Encoding: EncodingHex})
	require.Error(t, err)
	assert.ErrorIs(t, err, lockFailure)
	assert.Equal(t, 0, locker.unlockCalls, "Unlock must not run when Lock failed")
}

// TestEnsureSecret_DistributedUnlockError covers the release path's
// error branch: a failed Unlock is logged but must not change the
// successful EnsureSecret result.
func TestEnsureSecret_DistributedUnlockError(t *testing.T) {
	locker := &fakeLocker{unlockErr: errors.New("unlock failed")}
	m := newManagerWithLocker(t, locker)

	got, generated, err := m.EnsureSecret(context.Background(), "app/unlock-fail",
		&SecretSpec{Length: 8, Encoding: EncodingRaw})
	require.NoError(t, err, "a failed Unlock must not fail EnsureSecret")
	assert.True(t, generated)
	assert.NotEmpty(t, got)
	assert.Equal(t, 1, locker.unlockCalls)
}

// stubBackend is a configurable Backend used to drive EnsureSecret's
// Get/Store error and corrupt-storage branches. Only Get and Store are
// meaningfully configurable; the remaining methods delegate to an
// embedded MemoryBackend so the type fully satisfies Backend.
type stubBackend struct {
	*MemoryBackend
	getData  map[string][]byte
	getMeta  map[string]string
	getErr   error
	storeErr error
}

func (s *stubBackend) Get(_ context.Context, _ string) (map[string][]byte, map[string]string, error) {
	if s.getErr != nil {
		return nil, nil, s.getErr
	}
	return s.getData, s.getMeta, nil
}

func (s *stubBackend) Store(_ context.Context, _ string, _ map[string][]byte, _ map[string]string) error {
	return s.storeErr
}

func newStubManager(t *testing.T, b Backend) *Manager {
	t.Helper()
	m, err := NewManager(&ManagerOptions{Backend: b})
	require.NoError(t, err)
	return m
}

// TestEnsureSecret_EmptyPath covers the up-front path guard.
func TestEnsureSecret_EmptyPath(t *testing.T) {
	m := newManagerWithLocker(t, &fakeLocker{})
	_, _, err := m.EnsureSecret(context.Background(), "", &SecretSpec{Length: 8, Encoding: EncodingHex})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidPath)
}

// TestEnsureSecret_ContextCancelled covers the early ctx.Err() guard
// before any lock or backend access happens.
func TestEnsureSecret_ContextCancelled(t *testing.T) {
	m := newManagerWithLocker(t, &fakeLocker{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := m.EnsureSecret(ctx, "app/cancelled", &SecretSpec{Length: 8, Encoding: EncodingHex})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// TestEnsureSecret_CorruptSpecMetadata covers the branch where a stored
// auto-generated secret carries unparseable spec metadata.
func TestEnsureSecret_CorruptSpecMetadata(t *testing.T) {
	b := &stubBackend{
		MemoryBackend: NewMemoryBackend(),
		getData:       map[string][]byte{EnsureSecretDataKey: []byte("x")},
		getMeta: map[string]string{
			MetaAutoGenerated: "true",
			MetaSpecLength:    "not-a-number",
			MetaSpecEncoding:  EncodingHex,
		},
	}
	m := newStubManager(t, b)
	_, _, err := m.EnsureSecret(context.Background(), "app/corrupt-meta",
		&SecretSpec{Length: 8, Encoding: EncodingHex})
	require.Error(t, err)
}

// TestEnsureSecret_MissingValueKey covers the corrupt-storage branch
// where the stored secret's spec matches but the canonical value key is
// absent from the data map.
func TestEnsureSecret_MissingValueKey(t *testing.T) {
	b := &stubBackend{
		MemoryBackend: NewMemoryBackend(),
		getData:       map[string][]byte{"wrong-key": []byte("x")},
		getMeta: map[string]string{
			MetaAutoGenerated: "true",
			MetaSpecLength:    "8",
			MetaSpecEncoding:  EncodingHex,
		},
	}
	m := newStubManager(t, b)
	_, _, err := m.EnsureSecret(context.Background(), "app/missing-value",
		&SecretSpec{Length: 8, Encoding: EncodingHex})
	require.Error(t, err)
}

// TestEnsureSecret_BackendGetError covers the branch where Get fails with
// an error that is neither NotFound nor Deleted — a fatal storage error.
func TestEnsureSecret_BackendGetError(t *testing.T) {
	b := &stubBackend{
		MemoryBackend: NewMemoryBackend(),
		getErr:        errors.New("storage exploded"),
	}
	m := newStubManager(t, b)
	_, _, err := m.EnsureSecret(context.Background(), "app/get-error",
		&SecretSpec{Length: 8, Encoding: EncodingHex})
	require.Error(t, err)
	assert.ErrorIs(t, err, b.getErr)
}

// TestEnsureSecret_BackendStoreError covers the generate+store path's
// failure branch: when Store fails the freshly generated bytes are
// zeroed and an internal error is returned.
func TestEnsureSecret_BackendStoreError(t *testing.T) {
	b := &stubBackend{
		MemoryBackend: NewMemoryBackend(),
		getErr:        ErrSecretNotFound,
		storeErr:      errors.New("store failed"),
	}
	m := newStubManager(t, b)
	_, generated, err := m.EnsureSecret(context.Background(), "app/store-error",
		&SecretSpec{Length: 8, Encoding: EncodingHex})
	require.Error(t, err)
	assert.False(t, generated)
	assert.ErrorIs(t, err, b.storeErr)
}
