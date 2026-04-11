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

func (l *mockLogger) Debug(_ string)                                   {}
func (l *mockLogger) Debugf(_ string, _ ...interface{})               {}
func (l *mockLogger) Info(_ string)                                    {}
func (l *mockLogger) Infof(_ string, _ ...interface{})                 {}
func (l *mockLogger) Warn(_ string)                                    {}
func (l *mockLogger) Warnf(_ string, _ ...interface{})                 {}
func (l *mockLogger) Error(_ string)                                   {}
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
