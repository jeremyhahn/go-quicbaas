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
	"sync"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// MemoryStoreResolver is a qrdbsdk.TenantStoreResolver that gives every tenant
// its own qrdb in-memory store. That store performs the guarded writes an
// activation claim needs, so claims built over it arbitrate within one
// process. A test whose property is that the store decides a race across
// nodes needs a real node instead (pkg/testutil/qrdbnode).
type MemoryStoreResolver struct {
	mu     sync.Mutex
	stores map[string]qrdbsdk.KVStore
}

// NewMemoryStoreResolver creates a resolver with no tenant stores yet.
func NewMemoryStoreResolver() *MemoryStoreResolver {
	return &MemoryStoreResolver{stores: make(map[string]qrdbsdk.KVStore)}
}

// Resolve implements qrdbsdk.TenantStoreResolver.
func (r *MemoryStoreResolver) Resolve(_ context.Context, tenantID string) (qrdbsdk.KVStore, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	store, ok := r.stores[tenantID]
	if !ok {
		store = qrdbsdk.NewMemoryKVStore()
		r.stores[tenantID] = store
	}
	return store, nil
}

// NewMemoryActivationClaims builds user activation claims over a fresh
// MemoryStoreResolver.
func NewMemoryActivationClaims() (*iam.UserActivationClaims, error) {
	return iam.NewUserActivationClaims(NewMemoryStoreResolver())
}
