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

// Package permscope reads back the tenancy scope a permission was declared
// with, through the go-qrdb SDK alone.
//
// A test that asserts what this repository contributed to go-qrdb's
// permission registry needs the read side of RegisterPermissionScope, and
// the SDK re-exports the write side only — reaching for
// go-qrdb/pkg/permission.LookupScope to close the gap would cross the locked
// SDK boundary, in a test as much as in production code.
//
// The seam that IS re-exported is the per-tenant scope store
// (qrdbsdk.NewTenantScopeStore, named in this repository's SDK boundary as
// an approved re-export). Its ResolveScope applies a documented precedence:
// the tenant's own stored definition first, then the process-global
// declaration layer. Backing it with an in-memory store nothing ever writes
// to leaves only the second step, which is exactly the declaration a module
// contributes — and a permission nobody declared surfaces as the same error
// the RBAC authorizer denies on, rather than as a scope.
package permscope

import (
	"context"
	"sync"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// probeTenant is the tenant ResolveScope is asked about. Any non-system
// tenant does: the backing store is empty, so no per-tenant definition can
// shadow the process-global one under test.
const probeTenant = "permscope-probe"

// Resolver answers a permission's declared scope.
type Resolver struct {
	store *qrdbsdk.TenantScopeStore
}

// New returns a Resolver over an empty in-memory store. It allocates
// nothing beyond that store and touches no process-global state, so
// separate tests may hold separate Resolvers.
func New() (*Resolver, error) {
	store, err := qrdbsdk.NewTenantScopeStore(newMemoryResolver())
	if err != nil {
		return nil, err
	}
	return &Resolver{store: store}, nil
}

// Scope returns the tenancy scope declared for p. A permission no layer has
// declared a scope for yields a *qrdbsdk.PermissionUndeclaredScopeError —
// never a scope — because that is what the authorizer sees and denies on.
func (r *Resolver) Scope(ctx context.Context, p qrdbsdk.Permission) (qrdbsdk.PermissionScope, error) {
	return r.store.ResolveScope(ctx, probeTenant, p)
}

// memoryResolver hands every tenant its own in-memory store, which stays
// empty for the lifetime of a Resolver.
type memoryResolver struct {
	mu     sync.Mutex
	stores map[string]qrdbsdk.KVStore
}

func newMemoryResolver() *memoryResolver {
	return &memoryResolver{stores: map[string]qrdbsdk.KVStore{}}
}

// Resolve implements qrdbsdk.TenantStoreResolver.
func (r *memoryResolver) Resolve(_ context.Context, tenantID string) (qrdbsdk.KVStore, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	store, ok := r.stores[tenantID]
	if !ok {
		store = qrdbsdk.NewMemoryKVStore()
		r.stores[tenantID] = store
	}
	return store, nil
}
