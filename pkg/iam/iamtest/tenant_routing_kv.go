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

package iamtest

import (
	"context"
	"strings"
	"sync"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TenantSealedError reports a store that could not be resolved because the
// tenant's barrier is sealed. It stands in for go-qrdb's tenant.ErrTenantSealed:
// the Backend* stores never inspect the sentinel, they only require that a
// resolve failure propagates and is NEVER swallowed by a shared-store fallback.
type TenantSealedError struct {
	TenantID string
}

func (e *TenantSealedError) Error() string { return "tenant sealed: " + e.TenantID }

// memStore is a minimal in-memory interfaces.KVStore used as one tenant's
// backend inside TenantRoutingKV.
type memStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newMemStore() *memStore { return &memStore{data: make(map[string][]byte)} }

func (m *memStore) Put(_ context.Context, key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(value))
	copy(cp, value)
	m.data[key] = cp
	return nil
}

func (m *memStore) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	if !ok {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iamtest.kv.get"}
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (m *memStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *memStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	for _, key := range m.keysWithPrefix(prefix) {
		m.mu.RLock()
		value, ok := m.data[key]
		m.mu.RUnlock()
		if !ok {
			continue
		}
		if err := fn(key, value); err != nil {
			return err
		}
	}
	return nil
}

func (m *memStore) List(_ context.Context, prefix string) ([]string, error) {
	return m.keysWithPrefix(prefix), nil
}

func (m *memStore) Exists(_ context.Context, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[key]
	return ok, nil
}

func (m *memStore) keysWithPrefix(prefix string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var keys []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys
}

// TenantRoutingKV is an interfaces.KVStore that mirrors production's
// pkg/server tenantRoutingKVStore: it reads the tenant from the CONTEXT,
// routes to that tenant's store (its own store for a barrier-isolated tenant,
// the shared store otherwise), and fails closed for a sealed tenant rather than
// falling back to the shared store.
//
// It exists because the tenant-namespace defects this repository keeps hitting
// -- go-quicbaas#535, #634 and #644 -- are created entirely by a write and a
// read resolving to different keyspaces. A flat store cannot reproduce any of
// them, so a test that uses one proves nothing about them.
type TenantRoutingKV struct {
	mu     sync.RWMutex
	shared *memStore
	stores map[string]*memStore
	sealed map[string]bool

	// readers are the extra context readers resolve consults after iam's, in
	// order. See AlsoReadTenantFrom.
	readers []func(context.Context) string
}

// NewTenantRoutingKV creates a routing store whose tenants all share one
// namespace until BarrierTenant isolates them.
func NewTenantRoutingKV() *TenantRoutingKV {
	return &TenantRoutingKV{
		shared: newMemStore(),
		stores: make(map[string]*memStore),
		sealed: make(map[string]bool),
	}
}

// BarrierTenant gives tenantID its own physically separate store, the way a
// go-qrdb TenantBarrier does.
func (r *TenantRoutingKV) BarrierTenant(tenantID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stores[tenantID] = newMemStore()
}

// SealTenant makes tenantID's store unresolvable, so every operation bound to
// it must fail closed.
func (r *TenantRoutingKV) SealTenant(tenantID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sealed[tenantID] = true
}

// SharedKeyCount reports how many keys landed in the shared namespace, which is
// how a test asserts that a tenant's record did NOT land there.
func (r *TenantRoutingKV) SharedKeyCount() int {
	r.shared.mu.RLock()
	defer r.shared.mu.RUnlock()
	return len(r.shared.data)
}

// AlsoReadTenantFrom adds a context reader consulted after iam's.
//
// PRODUCTION READS MORE THAN ONE KEY AND THIS HARNESS READ ONLY ONE, which made
// it blind to exactly the defects it exists to catch for every package that
// carries its own tenant context key. tenantFromAnyContext
// (pkg/server/tenant_kvstore.go) tries iam.TenantFromContext and then
// orchestration.TenantFromContext; a test using orchestration's
// ContextWithTenant against this store resolved to the SHARED namespace no
// matter what it did, so the write and the read agreed by accident and the test
// proved nothing.
//
// It is a registration rather than an import because this package must not
// depend on the packages it is used to test.
//
// The ORDER matches production: iam's key wins, then each reader in the order it
// was added.
func (r *TenantRoutingKV) AlsoReadTenantFrom(read func(context.Context) string) {
	if read == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.readers = append(r.readers, read)
}

// tenantFrom mirrors production's tenantFromAnyContext: iam's key first, then
// every registered reader in order.
//
// Callers hold r.mu, so the readers slice is read under the same lock that
// AlsoReadTenantFrom writes it under.
func (r *TenantRoutingKV) tenantFrom(ctx context.Context) string {
	if tenantID, ok := tenantctx.Lookup(ctx); ok {
		return tenantID
	}
	for _, read := range r.readers {
		if tenantID := read(ctx); tenantID != "" {
			return tenantID
		}
	}
	return ""
}

func (r *TenantRoutingKV) resolve(ctx context.Context) (*memStore, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tenantID := r.tenantFrom(ctx)
	if r.sealed[tenantID] {
		return nil, &TenantSealedError{TenantID: tenantID}
	}
	if st, ok := r.stores[tenantID]; ok {
		return st, nil
	}
	return r.shared, nil
}

// Put routes the write to the context tenant's store.
func (r *TenantRoutingKV) Put(ctx context.Context, key string, value []byte) error {
	st, err := r.resolve(ctx)
	if err != nil {
		return err
	}
	return st.Put(ctx, key, value)
}

// Get routes the read to the context tenant's store.
func (r *TenantRoutingKV) Get(ctx context.Context, key string) ([]byte, error) {
	st, err := r.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return st.Get(ctx, key)
}

// Delete routes the removal to the context tenant's store.
func (r *TenantRoutingKV) Delete(ctx context.Context, key string) error {
	st, err := r.resolve(ctx)
	if err != nil {
		return err
	}
	return st.Delete(ctx, key)
}

// Scan routes the prefix walk to the context tenant's store.
func (r *TenantRoutingKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	st, err := r.resolve(ctx)
	if err != nil {
		return err
	}
	return st.Scan(ctx, prefix, fn)
}

// List routes the key listing to the context tenant's store.
func (r *TenantRoutingKV) List(ctx context.Context, prefix string) ([]string, error) {
	st, err := r.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return st.List(ctx, prefix)
}

// Exists routes the presence check to the context tenant's store.
func (r *TenantRoutingKV) Exists(ctx context.Context, key string) (bool, error) {
	st, err := r.resolve(ctx)
	if err != nil {
		return false, err
	}
	return st.Exists(ctx, key)
}

var _ interfaces.KVStore = (*TenantRoutingKV)(nil)
