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

package iam

import (
	"context"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// errTenantSealedTest mirrors qrdb's tenant.ErrTenantSealed for the iam-package
// unit tests. The Backend* stores never inspect the sentinel — they only require
// that a resolve failure propagates and is NEVER swallowed by a shared-store
// fallback — so a local sentinel exercises the fail-closed contract faithfully.
type sealedError struct{}

func (sealedError) Error() string { return "tenant sealed" }

var errTenantSealedTest = sealedError{}

// memStore is a minimal in-memory interfaces.KVStore used as a per-tenant
// backend in the routing test double.
type memStore struct {
	data map[string][]byte
}

func newMemStore() *memStore { return &memStore{data: make(map[string][]byte)} }

func (m *memStore) Put(_ context.Context, key string, value []byte) error {
	cp := make([]byte, len(value))
	copy(cp, value)
	m.data[key] = cp
	return nil
}

func (m *memStore) Get(_ context.Context, key string) ([]byte, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "memstore.get"}
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (m *memStore) Delete(_ context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func (m *memStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	for k, v := range m.data {
		if strings.HasPrefix(k, prefix) {
			if err := fn(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *memStore) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *memStore) Exists(_ context.Context, key string) (bool, error) {
	_, ok := m.data[key]
	return ok, nil
}

func (m *memStore) keyCount() int { return len(m.data) }

// routingTestKV is an interfaces.KVStore that mirrors the production
// tenantRoutingKVStore: it reads the tenant from context, routes to that
// tenant's store (its own store for a barrier tenant, the shared store for the
// empty/system tenant), and fails closed for a sealed tenant — NEVER falling
// back to the shared store. It lets the iam-package Backend* tests exercise
// per-tenant isolation, by-id routing, and sealed-fail-closed end to end.
type routingTestKV struct {
	shared *memStore
	stores map[string]*memStore
	sealed map[string]bool
}

func newRoutingTestKV() *routingTestKV {
	return &routingTestKV{
		shared: newMemStore(),
		stores: make(map[string]*memStore),
		sealed: make(map[string]bool),
	}
}

// barrierTenant registers tenantID as barrier-isolated with its own store.
func (r *routingTestKV) barrierTenant(tenantID string) *memStore {
	st := newMemStore()
	r.stores[tenantID] = st
	return st
}

// sealTenant marks a barrier tenant sealed so resolve fails closed.
func (r *routingTestKV) sealTenant(tenantID string) { r.sealed[tenantID] = true }

func (r *routingTestKV) resolve(ctx context.Context) (*memStore, error) {
	tenantID := TenantFromContext(ctx)
	if r.sealed[tenantID] {
		return nil, errTenantSealedTest
	}
	if qrdbsdk.IsSystemTenant(tenantID) {
		return r.shared, nil
	}
	if st, ok := r.stores[tenantID]; ok {
		return st, nil
	}
	return r.shared, nil
}

func (r *routingTestKV) Put(ctx context.Context, key string, value []byte) error {
	st, err := r.resolve(ctx)
	if err != nil {
		return err
	}
	return st.Put(ctx, key, value)
}

func (r *routingTestKV) Get(ctx context.Context, key string) ([]byte, error) {
	st, err := r.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return st.Get(ctx, key)
}

func (r *routingTestKV) Delete(ctx context.Context, key string) error {
	st, err := r.resolve(ctx)
	if err != nil {
		return err
	}
	return st.Delete(ctx, key)
}

func (r *routingTestKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	st, err := r.resolve(ctx)
	if err != nil {
		return err
	}
	return st.Scan(ctx, prefix, fn)
}

func (r *routingTestKV) List(ctx context.Context, prefix string) ([]string, error) {
	st, err := r.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return st.List(ctx, prefix)
}

func (r *routingTestKV) Exists(ctx context.Context, key string) (bool, error) {
	st, err := r.resolve(ctx)
	if err != nil {
		return false, err
	}
	return st.Exists(ctx, key)
}

var _ interfaces.KVStore = (*routingTestKV)(nil)

// staticTenantLister implements tenantLister from a fixed set of tenant ids so
// cross-tenant sweeps can be exercised deterministically.
type staticTenantLister struct {
	ids []string
}

func (l staticTenantLister) ListTenants(_ context.Context, _ ListOptions) ([]*Tenant, error) {
	out := make([]*Tenant, 0, len(l.ids))
	for _, id := range l.ids {
		out = append(out, &Tenant{ID: id})
	}
	return out, nil
}
