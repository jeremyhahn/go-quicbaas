package iam

import (
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// getFailingKVStore is a KVStore (not atomic) whose Get of failKey fails.
type getFailingKVStore struct {
	interfaces.KVStore
	failKey string
	err     error
}

func (s *getFailingKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	if key == s.failKey {
		return nil, s.err
	}
	return s.KVStore.Get(ctx, key)
}

// notFoundMemoryKV is qrdb's atomic in-memory store with the KVStore
// contract's not-found error for an absent key (qrdb returns nil, nil).
type notFoundMemoryKV struct {
	qrdbsdk.AtomicKVStore
}

func newNotFoundMemoryKV() *notFoundMemoryKV {
	return &notFoundMemoryKV{AtomicKVStore: qrdbsdk.NewMemoryKVStore()}
}

func (s *notFoundMemoryKV) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := s.AtomicKVStore.Get(ctx, key)
	if err == nil && value == nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test.get"}
	}
	return value, err
}

func TestBackendStorage_CreatePolicyPropagatesReadError(t *testing.T) {
	ctx := context.Background()
	readErr := errors.New("transient read failure")
	key := policyKey("tenant1", "p")
	inner := newMockKVStore()
	seeded := []byte(`{"seeded":"deny"}`)
	require.NoError(t, inner.Put(ctx, key, seeded))
	storage := NewBackendStorage(&getFailingKVStore{KVStore: inner, failKey: key, err: readErr})

	err := storage.CreatePolicy(ctx, "tenant1", createTestPolicy("tenant1", "p"))
	assert.ErrorIs(t, err, readErr)

	got, err := inner.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, seeded, got, "a failed existence read must write nothing")
}

func TestBackendStorage_CreatePolicyAtomicRefusesExisting(t *testing.T) {
	ctx := context.Background()
	kv := newNotFoundMemoryKV()
	storage := NewBackendStorage(kv)
	key := policyKey("tenant1", "p")
	existing := []byte(`{"existing":true}`)
	require.NoError(t, kv.Put(ctx, key, existing))

	err := storage.CreatePolicy(ctx, "tenant1", createTestPolicy("tenant1", "p"))
	assert.ErrorIs(t, err, ErrAlreadyExists)
	got, err := kv.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, existing, got)

	fresh := createTestPolicy("tenant1", "q")
	require.NoError(t, storage.CreatePolicy(ctx, "tenant1", fresh))
	want, err := encodeStoredPolicy(fresh)
	require.NoError(t, err)
	got, err = kv.Get(ctx, policyKey("tenant1", "q"))
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestBackendStorage_RevertCreatedPolicyOnlyOwnRecord(t *testing.T) {
	ctx := context.Background()
	kv := newNotFoundMemoryKV()
	storage := NewBackendStorage(kv)
	p := createTestPolicy("tenant1", "p")
	key := policyKey("tenant1", "p")

	require.NoError(t, storage.CreatePolicy(ctx, "tenant1", p))
	require.NoError(t, storage.RevertCreatedPolicy(ctx, "tenant1", p))
	_, err := kv.Get(ctx, key)
	assert.Error(t, err, "the reverted create must be gone")

	require.NoError(t, storage.CreatePolicy(ctx, "tenant1", p))
	other := []byte(`{"other":"writer"}`)
	require.NoError(t, kv.Put(ctx, key, other))
	require.NoError(t, storage.RevertCreatedPolicy(ctx, "tenant1", p))
	got, err := kv.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, other, got, "a revert must keep another writer's record")

	assert.NoError(t, storage.RevertCreatedPolicy(ctx, "tenant1", createTestPolicy("tenant1", "absent")))
}

// narrowGuardedKV has the plain KVStore methods plus only the two guarded
// writes, as the tenant routing store does; its Put and Delete fail the test.
type narrowGuardedKV struct {
	interfaces.KVStore
	guard          qrdbsdk.AtomicKVStore
	t              *testing.T
	putIfAbsentErr error
}

func newNarrowGuardedKV(t *testing.T) *narrowGuardedKV {
	kv := newNotFoundMemoryKV()
	return &narrowGuardedKV{KVStore: kv, guard: kv.AtomicKVStore, t: t}
}

func (s *narrowGuardedKV) Put(ctx context.Context, key string, value []byte) error {
	s.t.Errorf("Put(%q) called; the guarded PutIfAbsent must be used", key)
	return errors.New("put not allowed")
}

func (s *narrowGuardedKV) Delete(ctx context.Context, key string) error {
	s.t.Errorf("Delete(%q) called; the guarded CompareAndDelete must be used", key)
	return errors.New("delete not allowed")
}

func (s *narrowGuardedKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if s.putIfAbsentErr != nil {
		return false, s.putIfAbsentErr
	}
	return s.guard.PutIfAbsent(ctx, key, value)
}

func (s *narrowGuardedKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	return s.guard.CompareAndDelete(ctx, key, expected)
}

func TestBackendStorage_CreatePolicyUsesNarrowGuardedStore(t *testing.T) {
	ctx := context.Background()
	kv := newNarrowGuardedKV(t)
	storage := NewBackendStorage(kv)
	p := createTestPolicy("tenant1", "p")

	require.NoError(t, storage.CreatePolicy(ctx, "tenant1", p))
	got, err := storage.GetPolicy(ctx, "tenant1", "p")
	require.NoError(t, err)
	assert.Equal(t, "p", got.Metadata.Name)

	err = storage.CreatePolicy(ctx, "tenant1", createTestPolicy("tenant1", "p"))
	assert.True(t, dberrors.Is(err, dberrors.ErrAlreadyExists), "second create: err = %v", err)

	require.NoError(t, storage.RevertCreatedPolicy(ctx, "tenant1", p))
	_, err = storage.GetPolicy(ctx, "tenant1", "p")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "after revert: err = %v", err)
}

func TestBackendStorage_CreatePolicyGuardedErrorIsReturned(t *testing.T) {
	ctx := context.Background()
	kv := newNarrowGuardedKV(t)
	kv.putIfAbsentErr = errors.New("guarded write refused")
	storage := NewBackendStorage(kv)

	err := storage.CreatePolicy(ctx, "tenant1", createTestPolicy("tenant1", "p"))
	assert.ErrorIs(t, err, kv.putIfAbsentErr)
	_, err = storage.GetPolicy(ctx, "tenant1", "p")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "nothing written: err = %v", err)
}

// sharingKV answers SharesStore with a fixed result.
type sharingKV struct {
	interfaces.KVStore
	shares bool
	err    error
}

func (s *sharingKV) SharesStore(a, b context.Context) (bool, error) {
	return s.shares, s.err
}

func TestBackendStorage_SharesPolicyRecord(t *testing.T) {
	ctx := context.Background()
	shares, err := NewBackendStorage(newNotFoundMemoryKV()).SharesPolicyRecord(ctx, ctx)
	require.NoError(t, err)
	assert.True(t, shares, "a store without SharesStore reaches one store from every context")

	shares, err = NewBackendStorage(&sharingKV{KVStore: newNotFoundMemoryKV()}).SharesPolicyRecord(ctx, ctx)
	require.NoError(t, err)
	assert.False(t, shares)

	resolveErr := errors.New("resolve failed")
	_, err = NewBackendStorage(&sharingKV{KVStore: newNotFoundMemoryKV(), shares: true, err: resolveErr}).SharesPolicyRecord(ctx, ctx)
	assert.ErrorIs(t, err, resolveErr)
}
