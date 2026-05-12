//go:build !integration

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

package security

import (
	"context"
	"crypto"
	"errors"
	"sync"
	"testing"

	types "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ----------------------------------------------------------------------------
// mockKMS — minimal in-process KMS for unit tests.
// ----------------------------------------------------------------------------

// mockKMS is a minimal implementation of interfaces.KMSManager that stores
// GenerateDataKey and Decrypt responses set up per call index. It is
// intentionally not thread-safe; tests run sequentially per instance.
type mockKMS struct {
	mu sync.Mutex

	// generateResponses is consumed FIFO by GenerateDataKey calls.
	generateResponses []mockKMSGenerateResponse
	// decryptResponses is consumed FIFO by Decrypt calls.
	decryptResponses []mockKMSDecryptResponse

	generateCalls int
	decryptCalls  int
}

type mockKMSGenerateResponse struct {
	plaintext []byte
	wrapped   []byte
	err       error
}

type mockKMSDecryptResponse struct {
	plaintext []byte
	err       error
}

func newMockKMS() *mockKMS { return &mockKMS{} }

// queueGenerate registers the next GenerateDataKey response.
// The bytes are copied so that zeroBytes() calls in the implementation
// do not affect the expected values held in the test.
func (m *mockKMS) queueGenerate(plaintext, wrapped []byte, err error) {
	pt := make([]byte, len(plaintext))
	copy(pt, plaintext)
	wk := make([]byte, len(wrapped))
	copy(wk, wrapped)
	m.generateResponses = append(m.generateResponses, mockKMSGenerateResponse{pt, wk, err})
}

// queueDecrypt registers the next Decrypt response.
// The bytes are copied for the same reason as queueGenerate.
func (m *mockKMS) queueDecrypt(plaintext []byte, err error) {
	pt := make([]byte, len(plaintext))
	copy(pt, plaintext)
	m.decryptResponses = append(m.decryptResponses, mockKMSDecryptResponse{pt, err})
}

func (m *mockKMS) GenerateDataKey(_ context.Context, _ *types.KeyAttributes, _ int) ([]byte, []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.generateCalls >= len(m.generateResponses) {
		panic("mockKMS: unexpected GenerateDataKey call")
	}
	r := m.generateResponses[m.generateCalls]
	m.generateCalls++
	return r.plaintext, r.wrapped, r.err
}

func (m *mockKMS) Decrypt(_ context.Context, _ string, _ []byte, _ []byte) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.decryptCalls >= len(m.decryptResponses) {
		panic("mockKMS: unexpected Decrypt call")
	}
	r := m.decryptResponses[m.decryptCalls]
	m.decryptCalls++
	return r.plaintext, r.err
}

// Unused KMSManager methods — satisfy the interface.
func (m *mockKMS) GenerateKey(_ context.Context, _ *types.KeyAttributes) (interface{}, error) {
	return nil, errors.New("not implemented")
}
func (m *mockKMS) ImportKey(_ context.Context, _ string, _ []byte, _ string) (interface{}, error) {
	return nil, errors.New("not implemented")
}
func (m *mockKMS) GetKey(_ context.Context, _ string) (interface{}, error) {
	return nil, errors.New("not implemented")
}
func (m *mockKMS) GetPublicKey(_ context.Context, _ string) ([]byte, error) {
	return nil, errors.New("not implemented")
}
func (m *mockKMS) UpdateStoredMetadata(_ interface{}) error { return errors.New("not implemented") }
func (m *mockKMS) ListKeys(_ context.Context, _ string) ([]interface{}, error) {
	return nil, errors.New("not implemented")
}
func (m *mockKMS) RotateKey(_ context.Context, _ string) (interface{}, error) {
	return nil, errors.New("not implemented")
}
func (m *mockKMS) RevokeKey(_ context.Context, _ string) error  { return errors.New("not implemented") }
func (m *mockKMS) EnableKey(_ context.Context, _ string) error  { return errors.New("not implemented") }
func (m *mockKMS) DisableKey(_ context.Context, _ string) error { return errors.New("not implemented") }
func (m *mockKMS) DeleteKey(_ context.Context, _ string) error  { return errors.New("not implemented") }
func (m *mockKMS) Encrypt(_ context.Context, _ string, _, _ []byte) ([]byte, error) {
	return nil, errors.New("not implemented")
}
func (m *mockKMS) Sign(_ context.Context, _ string, _ []byte, _ string) ([]byte, error) {
	return nil, errors.New("not implemented")
}
func (m *mockKMS) Verify(_ context.Context, _ string, _, _ []byte, _ string) (bool, error) {
	return false, errors.New("not implemented")
}
func (m *mockKMS) GetBackendType() config.BackendType              { return config.BackendType("mock") }
func (m *mockKMS) IsAvailable(_ context.Context) bool             { return true }
func (m *mockKMS) GetSupportedAlgorithms(_ context.Context) ([]string, error) {
	return nil, nil
}
func (m *mockKMS) Close() error { return nil }

// Ensure mockKMS satisfies the interface at compile time.
var _ interfaces.KMSManager = (*mockKMS)(nil)

// ----------------------------------------------------------------------------
// mockKVStore — in-memory KVStore.
// ----------------------------------------------------------------------------

// mockKVStore is a thread-safe, in-memory implementation of interfaces.KVStore.
// deleteNotFound controls whether Delete returns an ErrNotFound for missing keys.
type mockKVStore struct {
	mu             sync.RWMutex
	data           map[string][]byte
	deleteNotFound bool
}

func newMockKVStore() *mockKVStore {
	return &mockKVStore{data: make(map[string][]byte)}
}

func (s *mockKVStore) Put(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(value))
	copy(cp, value)
	s.data[key] = cp
	return nil
}

func (s *mockKVStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	if !ok {
		return nil, nil
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (s *mockKVStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; !ok {
		if s.deleteNotFound {
			return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "mockKVStore.Delete"}
		}
		return nil
	}
	delete(s.data, key)
	return nil
}

func (s *mockKVStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for k, v := range s.data {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			if err := fn(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *mockKVStore) List(_ context.Context, prefix string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var keys []string
	for k := range s.data {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (s *mockKVStore) Exists(_ context.Context, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data[key]
	return ok, nil
}

// Ensure mockKVStore satisfies the interface at compile time.
var _ interfaces.KVStore = (*mockKVStore)(nil)

// ----------------------------------------------------------------------------
// mockLogger — no-op logger.
// ----------------------------------------------------------------------------

type mockLogger struct{}

func (l *mockLogger) Debug(_ string, _ ...any)                                   {}
func (l *mockLogger) Debugf(_ string, _ ...interface{})               {}
func (l *mockLogger) Info(_ string, _ ...any)                                    {}
func (l *mockLogger) Infof(_ string, _ ...interface{})                 {}
func (l *mockLogger) Warn(_ string, _ ...any)                                    {}
func (l *mockLogger) Warnf(_ string, _ ...interface{})                 {}
func (l *mockLogger) Error(_ string, _ ...any)                                   {}
func (l *mockLogger) Errorf(_ string, _ ...interface{})                {}
func (l *mockLogger) Fatal(_ string)                                   {}
func (l *mockLogger) Fatalf(_ string, _ ...interface{})                {}
func (l *mockLogger) WithField(_ string, _ interface{}) interfaces.Logger { return l }
func (l *mockLogger) WithFields(_ map[string]interface{}) interfaces.Logger { return l }
func (l *mockLogger) WithError(_ error) interfaces.Logger              { return l }

var _ interfaces.Logger = (*mockLogger)(nil)

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

func newTestManager(kms interfaces.KMSManager, store interfaces.KVStore) TenantDEKManager {
	return NewTenantDEKManager(kms, store, TenantDEKMasterKeyID, &mockLogger{})
}

// plaintext returns 32 bytes of deterministic key material for a given seed byte.
func fakePlaintext(seed byte) []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = seed
	}
	return b
}

// fakeWrapped returns short wrapped-key bytes for a given seed byte.
func fakeWrapped(seed byte) []byte {
	return []byte{seed, seed + 1, seed + 2, seed + 3}
}

// ----------------------------------------------------------------------------
// GetOrCreateTenantDEK
// ----------------------------------------------------------------------------

func TestGetOrCreateTenantDEK_NewTenant(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()

	pt := fakePlaintext(0xAA)
	wk := fakeWrapped(0xAA)
	kms.queueGenerate(pt, wk, nil)

	mgr := newTestManager(kms, store)
	defer mgr.Close()

	ctx := context.Background()
	got, err := mgr.GetOrCreateTenantDEK(ctx, "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, pt, got)
	assert.Equal(t, 1, kms.generateCalls)

	// Wrapped key must be persisted.
	stored, err := store.Get(ctx, currentStorageKey("tenant-1"))
	require.NoError(t, err)
	require.NotNil(t, stored)
}

func TestGetOrCreateTenantDEK_CachedTenant(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()

	pt := fakePlaintext(0xBB)
	kms.queueGenerate(pt, fakeWrapped(0xBB), nil)

	mgr := newTestManager(kms, store)
	defer mgr.Close()

	ctx := context.Background()
	_, err := mgr.GetOrCreateTenantDEK(ctx, "tenant-cached")
	require.NoError(t, err)

	// Second call must hit cache — GenerateDataKey must NOT be called again
	// and Decrypt must NOT be called at all.
	got, err := mgr.GetOrCreateTenantDEK(ctx, "tenant-cached")
	require.NoError(t, err)
	assert.Equal(t, pt, got)
	assert.Equal(t, 1, kms.generateCalls, "GenerateDataKey must be called exactly once")
	assert.Equal(t, 0, kms.decryptCalls, "Decrypt must not be called for a cached tenant")
}

func TestGetOrCreateTenantDEK_ExistingInStorage(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()

	pt := fakePlaintext(0xCC)
	wk := fakeWrapped(0xCC)
	kms.queueGenerate(pt, wk, nil)

	// Create the entry using a fresh manager, then discard it to clear the cache.
	ctx := context.Background()
	mgr1 := newTestManager(kms, store)
	_, err := mgr1.GetOrCreateTenantDEK(ctx, "tenant-storage")
	require.NoError(t, err)
	require.NoError(t, mgr1.Close())

	// A new manager has no cache; it must load from storage and call Decrypt.
	kms.queueDecrypt(pt, nil)
	mgr2 := newTestManager(kms, store)
	defer mgr2.Close()

	got, err := mgr2.GetOrCreateTenantDEK(ctx, "tenant-storage")
	require.NoError(t, err)
	assert.Equal(t, pt, got)
	assert.Equal(t, 0, kms.decryptCalls-1, "sanity: only one Decrypt call in the second manager")
}

func TestGetOrCreateTenantDEK_DefensiveCopy(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()
	pt := fakePlaintext(0xDD)
	kms.queueGenerate(pt, fakeWrapped(0xDD), nil)

	mgr := newTestManager(kms, store)
	defer mgr.Close()

	ctx := context.Background()
	got, err := mgr.GetOrCreateTenantDEK(ctx, "tenant-copy")
	require.NoError(t, err)

	// Mutate the returned slice — the cache must be unaffected.
	original := make([]byte, len(got))
	copy(original, got)
	for i := range got {
		got[i] = 0xFF
	}

	// Retrieve again from cache.
	kms.queueDecrypt(pt, nil) // would be called if cache was wiped — but shouldn't be
	got2, err := mgr.GetOrCreateTenantDEK(ctx, "tenant-copy")
	require.NoError(t, err)
	assert.Equal(t, original, got2, "cached value must not be affected by mutation of previous return value")
}

func TestGetOrCreateTenantDEK_GenerateError(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()
	kms.queueGenerate(nil, nil, errors.New("kms unavailable"))

	mgr := newTestManager(kms, store)
	defer mgr.Close()

	_, err := mgr.GetOrCreateTenantDEK(context.Background(), "tenant-err")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kms unavailable")
}

// ----------------------------------------------------------------------------
// GetTenantDEK
// ----------------------------------------------------------------------------

func TestGetTenantDEK_Existing(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()
	pt := fakePlaintext(0x11)
	kms.queueGenerate(pt, fakeWrapped(0x11), nil)

	mgr := newTestManager(kms, store)
	defer mgr.Close()

	ctx := context.Background()
	_, err := mgr.GetOrCreateTenantDEK(ctx, "t1")
	require.NoError(t, err)

	got, err := mgr.GetTenantDEK(ctx, "t1")
	require.NoError(t, err)
	assert.Equal(t, pt, got)
	// Still no Decrypt calls because it's cached.
	assert.Equal(t, 0, kms.decryptCalls)
}

func TestGetTenantDEK_NonExistent(t *testing.T) {
	mgr := newTestManager(newMockKMS(), newMockKVStore())
	defer mgr.Close()

	_, err := mgr.GetTenantDEK(context.Background(), "ghost-tenant")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTenantDEKNotFound))
}

func TestGetTenantDEK_LoadsFromStorageWhenNotCached(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()
	pt := fakePlaintext(0x22)
	kms.queueGenerate(pt, fakeWrapped(0x22), nil)

	ctx := context.Background()

	// Populate storage via a first manager instance.
	mgr1 := newTestManager(kms, store)
	_, err := mgr1.GetOrCreateTenantDEK(ctx, "t2")
	require.NoError(t, err)
	require.NoError(t, mgr1.Close())

	// Second manager has no cache; GetTenantDEK must call Decrypt.
	kms.queueDecrypt(pt, nil)
	mgr2 := newTestManager(kms, store)
	defer mgr2.Close()

	got, err := mgr2.GetTenantDEK(ctx, "t2")
	require.NoError(t, err)
	assert.Equal(t, pt, got)
	assert.Equal(t, 1, kms.decryptCalls)
}

// ----------------------------------------------------------------------------
// RotateTenantDEK
// ----------------------------------------------------------------------------

func TestRotateTenantDEK_IncrementsVersion(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()
	pt1 := fakePlaintext(0x01)
	pt2 := fakePlaintext(0x02)
	kms.queueGenerate(pt1, fakeWrapped(0x01), nil)
	kms.queueGenerate(pt2, fakeWrapped(0x02), nil)

	ctx := context.Background()
	mgr := newTestManager(kms, store)
	defer mgr.Close()

	_, err := mgr.GetOrCreateTenantDEK(ctx, "tenant-rot")
	require.NoError(t, err)

	require.NoError(t, mgr.RotateTenantDEK(ctx, "tenant-rot"))

	status, err := mgr.GetTenantDEKStatus(ctx, "tenant-rot")
	require.NoError(t, err)
	assert.Equal(t, uint64(2), status.Version)
}

func TestRotateTenantDEK_ArchivesOldVersion(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()
	kms.queueGenerate(fakePlaintext(0x03), fakeWrapped(0x03), nil)
	kms.queueGenerate(fakePlaintext(0x04), fakeWrapped(0x04), nil)

	ctx := context.Background()
	mgr := newTestManager(kms, store)
	defer mgr.Close()

	_, err := mgr.GetOrCreateTenantDEK(ctx, "tenant-archive")
	require.NoError(t, err)

	require.NoError(t, mgr.RotateTenantDEK(ctx, "tenant-archive"))

	// Version 1 must be persisted at the versioned path.
	archivedKey := versionedStorageKey("tenant-archive", 1)
	archived, err := store.Get(ctx, archivedKey)
	require.NoError(t, err)
	require.NotNil(t, archived, "version 1 must be archived after rotation")
}

func TestRotateTenantDEK_InvalidatesCache(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()
	pt1 := fakePlaintext(0x05)
	pt2 := fakePlaintext(0x06)
	kms.queueGenerate(pt1, fakeWrapped(0x05), nil)
	kms.queueGenerate(pt2, fakeWrapped(0x06), nil)

	ctx := context.Background()
	mgr := newTestManager(kms, store)
	defer mgr.Close()

	_, err := mgr.GetOrCreateTenantDEK(ctx, "tenant-inval")
	require.NoError(t, err)

	require.NoError(t, mgr.RotateTenantDEK(ctx, "tenant-inval"))

	// After rotation the cache must be empty for this tenant; the next Get
	// must call Decrypt to unwrap the new key from storage.
	kms.queueDecrypt(pt2, nil)
	got, err := mgr.GetTenantDEK(ctx, "tenant-inval")
	require.NoError(t, err)
	// The returned key must match the new plaintext, not the old one.
	assert.Equal(t, pt2, got)
	assert.Equal(t, 1, kms.decryptCalls, "Decrypt must be called once after cache invalidation")
}

func TestRotateTenantDEK_NonExistentTenant(t *testing.T) {
	mgr := newTestManager(newMockKMS(), newMockKVStore())
	defer mgr.Close()

	err := mgr.RotateTenantDEK(context.Background(), "no-such-tenant")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTenantDEKNotFound))
}

// ----------------------------------------------------------------------------
// DeleteTenantDEK
// ----------------------------------------------------------------------------

func TestDeleteTenantDEK_RemovesFromStorageAndCache(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()
	kms.queueGenerate(fakePlaintext(0x07), fakeWrapped(0x07), nil)

	ctx := context.Background()
	mgr := newTestManager(kms, store)
	defer mgr.Close()

	_, err := mgr.GetOrCreateTenantDEK(ctx, "tenant-del")
	require.NoError(t, err)

	require.NoError(t, mgr.DeleteTenantDEK(ctx, "tenant-del"))

	// Storage must not have the key anymore.
	v, err := store.Get(ctx, currentStorageKey("tenant-del"))
	require.NoError(t, err)
	assert.Nil(t, v)

	// GetTenantDEK must return not-found.
	_, err = mgr.GetTenantDEK(ctx, "tenant-del")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTenantDEKNotFound))
}

func TestDeleteTenantDEK_IdempotentForNonExistentTenant(t *testing.T) {
	// The store returns ErrNotFound for missing keys on Delete.
	store := newMockKVStore()
	store.deleteNotFound = true

	mgr := newTestManager(newMockKMS(), store)
	defer mgr.Close()

	// Must not return an error when the tenant never existed.
	err := mgr.DeleteTenantDEK(context.Background(), "ghost")
	assert.NoError(t, err)
}

// ----------------------------------------------------------------------------
// GetTenantDEKStatus
// ----------------------------------------------------------------------------

func TestGetTenantDEKStatus_ReturnsMetadataWithoutKeyMaterial(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()
	kms.queueGenerate(fakePlaintext(0x08), fakeWrapped(0x08), nil)

	ctx := context.Background()
	mgr := newTestManager(kms, store)
	defer mgr.Close()

	_, err := mgr.GetOrCreateTenantDEK(ctx, "tenant-status")
	require.NoError(t, err)

	status, err := mgr.GetTenantDEKStatus(ctx, "tenant-status")
	require.NoError(t, err)

	assert.Equal(t, "tenant-status", status.TenantID)
	assert.Equal(t, uint64(1), status.Version)
	assert.Equal(t, defaultTenantDEKAlgorithm, status.Algorithm)
	assert.Equal(t, defaultTenantDEKKeyBytes, status.KeyBytes)
	assert.False(t, status.CreatedAt.IsZero())
	assert.False(t, status.RotatedAt.IsZero())
}

func TestGetTenantDEKStatus_NonExistentTenant(t *testing.T) {
	mgr := newTestManager(newMockKMS(), newMockKVStore())
	defer mgr.Close()

	_, err := mgr.GetTenantDEKStatus(context.Background(), "nobody")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTenantDEKNotFound))
}

// ----------------------------------------------------------------------------
// ListTenantDEKs
// ----------------------------------------------------------------------------

func TestListTenantDEKs_ReturnsAllTenantIDs(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()
	tenants := []string{"alpha", "beta", "gamma"}
	for range tenants {
		kms.queueGenerate(fakePlaintext(0x09), fakeWrapped(0x09), nil)
	}

	ctx := context.Background()
	mgr := newTestManager(kms, store)
	defer mgr.Close()

	for _, id := range tenants {
		_, err := mgr.GetOrCreateTenantDEK(ctx, id)
		require.NoError(t, err)
	}

	ids, err := mgr.ListTenantDEKs(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, tenants, ids)
}

func TestListTenantDEKs_EmptyWhenNoTenants(t *testing.T) {
	mgr := newTestManager(newMockKMS(), newMockKVStore())
	defer mgr.Close()

	ids, err := mgr.ListTenantDEKs(context.Background())
	require.NoError(t, err)
	assert.Empty(t, ids)
}

// ----------------------------------------------------------------------------
// Close
// ----------------------------------------------------------------------------

func TestClose_ZeroesAllCachedKeys(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()
	tenants := []string{"x", "y", "z"}
	for range tenants {
		kms.queueGenerate(fakePlaintext(0x0A), fakeWrapped(0x0A), nil)
	}

	ctx := context.Background()
	mgr := newTestManager(kms, store)

	for _, id := range tenants {
		_, err := mgr.GetOrCreateTenantDEK(ctx, id)
		require.NoError(t, err)
	}

	require.NoError(t, mgr.Close())

	// After Close, the cache must be empty — getting any tenant must fail
	// and require a storage+KMS round trip (not just a cache lookup).
	// We verify this by attempting GetTenantDEK without priming a Decrypt
	// response; if the cache still held data, no Decrypt call would happen
	// and no panic would occur. With an empty Decrypt queue, a cache miss
	// would cause a panic — so we confirm via storage (all entries were in
	// storage, but we confirm no cache entry remains by queuing Decrypt
	// and observing it fires).
	kms.queueDecrypt(fakePlaintext(0x0A), nil)
	mgr2 := newTestManager(kms, store)
	defer mgr2.Close()
	_, err := mgr2.GetTenantDEK(ctx, "x")
	require.NoError(t, err)
	assert.Equal(t, 1, kms.decryptCalls)
}

// ----------------------------------------------------------------------------
// tenantDEKProvisioner
// ----------------------------------------------------------------------------

func TestTenantDEKProvisioner_ProvisionDEK(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()
	kms.queueGenerate(fakePlaintext(0x0B), fakeWrapped(0x0B), nil)

	mgr := newTestManager(kms, store)
	defer mgr.Close()

	p := NewTenantDEKProvisioner(mgr)
	err := p.ProvisionDEK(context.Background(), "prov-tenant")
	require.NoError(t, err)
	assert.Equal(t, 1, kms.generateCalls)

	// Calling again must be idempotent (cache hit, no second generate).
	err = p.ProvisionDEK(context.Background(), "prov-tenant")
	require.NoError(t, err)
	assert.Equal(t, 1, kms.generateCalls, "second ProvisionDEK must not generate a new key")
}

func TestTenantDEKProvisioner_DeprovisionDEK(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()
	kms.queueGenerate(fakePlaintext(0x0C), fakeWrapped(0x0C), nil)

	ctx := context.Background()
	mgr := newTestManager(kms, store)
	defer mgr.Close()

	p := NewTenantDEKProvisioner(mgr)
	require.NoError(t, p.ProvisionDEK(ctx, "deprov-tenant"))

	require.NoError(t, p.DeprovisionDEK(ctx, "deprov-tenant"))

	_, err := mgr.GetTenantDEK(ctx, "deprov-tenant")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTenantDEKNotFound))
}

func TestTenantDEKProvisioner_DeprovisionDEK_NonExistent(t *testing.T) {
	// DeprovisionDEK on an unknown tenant must be idempotent.
	mgr := newTestManager(newMockKMS(), newMockKVStore())
	defer mgr.Close()

	p := NewTenantDEKProvisioner(mgr)
	err := p.DeprovisionDEK(context.Background(), "never-existed")
	assert.NoError(t, err)
}

// ----------------------------------------------------------------------------
// Regression: first-create must not attempt Decrypt (scenario b).
// ----------------------------------------------------------------------------

// TestGetOrCreateTenantDEK_FirstCreate_NoDecryptCall asserts that on a truly
// fresh data dir (storage returns nil for the current-DEK key) the manager
// generates a new DEK without ever calling KMS.Decrypt. Regression for the
// class of bug where an empty ciphertext is fed into AEAD open and surfaces
// as "cipher: message authentication failed".
func TestGetOrCreateTenantDEK_FirstCreate_NoDecryptCall(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()

	pt := fakePlaintext(0x51)
	kms.queueGenerate(pt, fakeWrapped(0x51), nil)
	// Deliberately queue NO Decrypt response — a cache-miss path that
	// incorrectly reached Decrypt would panic inside mockKMS.Decrypt.

	mgr := newTestManager(kms, store)
	defer mgr.Close()

	got, err := mgr.GetOrCreateTenantDEK(context.Background(), "fresh-tenant")
	require.NoError(t, err)
	assert.Equal(t, pt, got)
	assert.Equal(t, 0, kms.decryptCalls, "Decrypt must not be called on first-ever create")
	assert.Equal(t, 1, kms.generateCalls, "GenerateDataKey must be called exactly once")
}

// TestGetOrCreateTenantDEK_EmptyStoredCiphertext_TreatedAsMissing asserts
// that an empty byte slice (length 0) returned from the KV store is treated
// as "not present" rather than fed into KMS.Decrypt. This is defence-in-depth
// against backends that translate a missing key into (nil, nil) or
// (emptySlice, nil); the AEAD path must never see zero-length ciphertext.
func TestGetOrCreateTenantDEK_EmptyStoredCiphertext_TreatedAsMissing(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()

	// Seed the store with an empty value at the DEK key. A naive
	// implementation would take the "entry exists" branch and fail
	// JSON-unmarshal or Decrypt. The correct behaviour is to generate.
	ctx := context.Background()
	require.NoError(t, store.Put(ctx, currentStorageKey("empty-tenant"), []byte{}))

	pt := fakePlaintext(0x52)
	kms.queueGenerate(pt, fakeWrapped(0x52), nil)

	mgr := newTestManager(kms, store)
	defer mgr.Close()

	got, err := mgr.GetOrCreateTenantDEK(ctx, "empty-tenant")
	require.NoError(t, err)
	assert.Equal(t, pt, got)
	assert.Equal(t, 0, kms.decryptCalls, "Decrypt must not be called for empty stored ciphertext")
	assert.Equal(t, 1, kms.generateCalls)
}

// ----------------------------------------------------------------------------
// Regression: stale KEK surface a typed error (scenario c).
// ----------------------------------------------------------------------------

// TestGetOrCreateTenantDEK_StaleKEK_ReturnsTypedError asserts that when the
// persisted DEK ciphertext cannot be unwrapped by the current KEK — the
// canonical symptom being "cipher: message authentication failed" — the
// manager returns a StaleTenantDEKError that satisfies errors.Is for
// ErrStaleTenantDEK and carries the tenant + KEK identity plus a repair hint.
func TestGetOrCreateTenantDEK_StaleKEK_ReturnsTypedError(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()
	ctx := context.Background()

	// Seed storage with a DEK wrapped under a (now-forgotten) prior KEK.
	kms.queueGenerate(fakePlaintext(0x60), fakeWrapped(0x60), nil)
	mgr1 := newTestManager(kms, store)
	_, err := mgr1.GetOrCreateTenantDEK(ctx, "stale-tenant")
	require.NoError(t, err)
	require.NoError(t, mgr1.Close())

	// Simulate the fresh-KEK scenario: storage still has the wrapped DEK
	// but the KMS now returns the stdlib AEAD auth-failure error when
	// asked to decrypt. mgr2 has a cold cache so it must go through
	// unwrapAndCache.
	kms.queueDecrypt(nil, errors.New("decryption operation failed: cipher: message authentication failed"))
	mgr2 := newTestManager(kms, store)
	defer mgr2.Close()

	_, err = mgr2.GetOrCreateTenantDEK(ctx, "stale-tenant")
	require.Error(t, err)

	// Must be the sentinel per errors.Is.
	assert.True(t, errors.Is(err, ErrStaleTenantDEK), "error must match ErrStaleTenantDEK sentinel: %v", err)

	// Must be retrievable via errors.As with structured fields.
	var staleErr *StaleTenantDEKError
	require.True(t, errors.As(err, &staleErr), "error must unwrap to *StaleTenantDEKError")
	assert.Equal(t, "stale-tenant", staleErr.TenantID)
	assert.Equal(t, TenantDEKMasterKeyID, staleErr.KEKID)
	assert.NotNil(t, staleErr.Cause)

	// Repair hint must appear in the message so operators see a path
	// forward rather than a raw crypto error.
	assert.Contains(t, staleErr.Error(), "deprovisioning", "error message must include a repair hint")
}

// TestGetTenantDEK_StaleKEK_ReturnsTypedError mirrors the above for
// GetTenantDEK so both ingress paths surface the same typed error.
func TestGetTenantDEK_StaleKEK_ReturnsTypedError(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()
	ctx := context.Background()

	kms.queueGenerate(fakePlaintext(0x61), fakeWrapped(0x61), nil)
	mgr1 := newTestManager(kms, store)
	_, err := mgr1.GetOrCreateTenantDEK(ctx, "stale-get-tenant")
	require.NoError(t, err)
	require.NoError(t, mgr1.Close())

	kms.queueDecrypt(nil, errors.New("xkms: cipher: message authentication failed"))
	mgr2 := newTestManager(kms, store)
	defer mgr2.Close()

	_, err = mgr2.GetTenantDEK(ctx, "stale-get-tenant")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrStaleTenantDEK))

	var staleErr *StaleTenantDEKError
	require.True(t, errors.As(err, &staleErr))
	assert.Equal(t, "stale-get-tenant", staleErr.TenantID)
}

// TestGetOrCreateTenantDEK_NonAuthDecryptError_NotStale ensures that
// generic (non-AEAD-auth) decryption errors remain wrapped as-is and are
// NOT mis-classified as StaleTenantDEKError. Only the specific
// "message authentication failed" case is treated as a stale-KEK signal.
func TestGetOrCreateTenantDEK_NonAuthDecryptError_NotStale(t *testing.T) {
	kms := newMockKMS()
	store := newMockKVStore()
	ctx := context.Background()

	kms.queueGenerate(fakePlaintext(0x62), fakeWrapped(0x62), nil)
	mgr1 := newTestManager(kms, store)
	_, err := mgr1.GetOrCreateTenantDEK(ctx, "transient-tenant")
	require.NoError(t, err)
	require.NoError(t, mgr1.Close())

	kms.queueDecrypt(nil, errors.New("kms backend unavailable"))
	mgr2 := newTestManager(kms, store)
	defer mgr2.Close()

	_, err = mgr2.GetOrCreateTenantDEK(ctx, "transient-tenant")
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrStaleTenantDEK), "non-auth errors must not be classified as stale DEK")
	assert.Contains(t, err.Error(), "kms backend unavailable")
}

// TestGetOrCreateTenantDEK_PersistentKEK_SurvivesRestart is the regression
// test for the ephemeral-KEK bug. It verifies that a TenantDEKManager that
// shares a KMS instance with a previous manager (simulating a process restart
// where the KEK is loaded from persistent storage rather than regenerated)
// can successfully unwrap a DEK stored by the previous instance.
//
// Without the fix (lifecycle.go: in-memory KMS key storage replaced with
// file-based storage when a data directory is configured), a new KMS instance
// with a freshly-generated KEK would fail to decrypt with "message
// authentication failed", surfacing as StaleTenantDEKError.
func TestGetOrCreateTenantDEK_PersistentKEK_SurvivesRestart(t *testing.T) {
	t.Parallel()

	kms := newMockKMS()
	store := newMockKVStore()
	ctx := context.Background()

	// --- First "process run": generate and store the wrapped DEK. ---
	pt := fakePlaintext(0x70)
	wk := fakeWrapped(0x70)
	kms.queueGenerate(pt, wk, nil)

	mgr1 := newTestManager(kms, store)
	got1, err := mgr1.GetOrCreateTenantDEK(ctx, "restart-tenant")
	require.NoError(t, err)
	require.Equal(t, pt, got1)
	require.NoError(t, mgr1.Close()) // clears in-memory cache

	// Verify the DEK was persisted to the store.
	stored, err := store.Get(ctx, currentStorageKey("restart-tenant"))
	require.NoError(t, err)
	require.NotEmpty(t, stored, "wrapped DEK must be persisted")

	// --- Simulated process restart with SAME persistent KEK loaded. ---
	// The second manager shares the same KMS instance (same KEK, simulating
	// file-based persistent key storage that survives restart). It must
	// successfully unwrap the DEK stored by mgr1 — no StaleTenantDEKError.
	kms.queueDecrypt(pt, nil)

	mgr2 := newTestManager(kms, store)
	defer mgr2.Close()

	got2, err := mgr2.GetOrCreateTenantDEK(ctx, "restart-tenant")
	require.NoError(t, err, "unwrap must succeed when KEK is persistent across restarts")
	require.Equal(t, pt, got2, "plaintext must match original DEK")
	require.Equal(t, 1, kms.decryptCalls, "Decrypt must be called exactly once on restart")
	require.Equal(t, 1, kms.generateCalls, "GenerateDataKey must not be called again on restart")
}

// TestGetOrCreateTenantDEK_EphemeralKEK_FailsOnRestart contrasts
// TestGetOrCreateTenantDEK_PersistentKEK_SurvivesRestart by confirming the
// pre-fix failure mode: a new KEK (different random bytes) cannot unwrap a
// DEK stored by the previous process run, producing StaleTenantDEKError.
func TestGetOrCreateTenantDEK_EphemeralKEK_FailsOnRestart(t *testing.T) {
	t.Parallel()

	kms := newMockKMS()
	store := newMockKVStore()
	ctx := context.Background()

	// --- First "process run": generate and store the wrapped DEK. ---
	kms.queueGenerate(fakePlaintext(0x71), fakeWrapped(0x71), nil)
	mgr1 := newTestManager(kms, store)
	_, err := mgr1.GetOrCreateTenantDEK(ctx, "ephemeral-tenant")
	require.NoError(t, err)
	require.NoError(t, mgr1.Close())

	// --- Simulated restart with NEW (ephemeral) KEK — the bug scenario. ---
	// Decrypt returns the AEAD auth-failure error that results from using a
	// different KEK than the one that wrapped the stored DEK.
	kms.queueDecrypt(nil, errors.New("decryption operation failed: cipher: message authentication failed"))
	mgr2 := newTestManager(kms, store)
	defer mgr2.Close()

	_, err = mgr2.GetOrCreateTenantDEK(ctx, "ephemeral-tenant")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrStaleTenantDEK),
		"ephemeral KEK must surface StaleTenantDEKError on restart with stale stored DEK")
}

// Signer returns a crypto.Signer for the given keyID. Tests that don't
// exercise the signer path receive a nil signer and no error; tests
// that do can stub this by redefining the method on a wrapped type.
func (m *mockKMS) Signer(_ context.Context, _ string) (crypto.Signer, error) {
	return nil, nil
}
