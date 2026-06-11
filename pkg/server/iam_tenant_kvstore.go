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

package server

import (
	"context"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// iamTenantKVStore returns the interfaces.KVStore that backs all quicbaas IAM
// data. When the embedded qrdb node exposes a per-tenant StoreResolver (seal
// enabled with a barrier registry, see go-qrdb lifecycle.BuildServices), the
// returned store routes every operation to the context tenant's backend so a
// barrier-isolated tenant's IAM data is stored in its own barrier (own DEK +
// namespace) and a prefix / system tenant uses the shared store. A sealed
// barrier tenant fails closed.
//
// When no resolver is available (seal disabled, storage-less / legacy boot),
// the store is the shared platform shard — the prior single-store behaviour —
// so existing single-tenant and dev deployments are unaffected.
func (s *Server) iamTenantKVStore() interfaces.KVStore {
	if s.qrdbNode != nil {
		if svcs := s.qrdbNode.Services(); svcs != nil && svcs.StoreResolver != nil {
			return newTenantRoutingKVStore(&resolverAdapter{inner: svcs.StoreResolver})
		}
	}
	return s.GetKVStore(qbshards.PlatformShardID)
}

// iamTenantScopeStore returns a per-tenant permission scope resolver built over
// the embedded qrdb node's StoreResolver, or nil when no resolver is available
// (seal disabled). The TenantScopeStore persists and resolves caller-defined
// permission scopes per tenant: a barrier-isolated tenant's scope definitions
// are stored (and encrypted) in that tenant's barrier; a prefix / system
// tenant's live in the shared store; a sealed barrier tenant fails closed
// (tenant.ErrTenantSealed). The resolver is installed on the REST RBAC provider
// so an admitted permission is gated by its scope in the subject's own tenant.
func (s *Server) iamTenantScopeStore() qrdbsdk.PermissionScopeResolver {
	if s.qrdbNode == nil {
		return nil
	}
	svcs := s.qrdbNode.Services()
	if svcs == nil || svcs.StoreResolver == nil {
		return nil
	}
	store, err := qrdbsdk.NewTenantScopeStore(svcs.StoreResolver)
	if err != nil {
		s.logger.WithError(err).Warn("iam: failed to build tenant scope store; per-tenant permission scopes disabled")
		return nil
	}
	return store
}

// storeResolver is the minimal slice of qrdb's tenant.StoreResolver that the
// per-tenant routing KVStore depends on. Declaring it locally keeps the
// routing store testable with a fake resolver and decouples quicbaas from the
// concrete resolver type while still consuming the canonical qrdb interface
// (tenant.StoreResolver satisfies it directly).
type storeResolver interface {
	Resolve(ctx context.Context, tenantID string) (interfaces.KVStore, error)
}

// resolverAdapter adapts qrdb's tenant.StoreResolver to the local
// storeResolver interface. The qrdb resolver returns a qrdb kvstore.KVStore,
// which is a structural superset of quicbaas's interfaces.KVStore (it adds
// index methods on top of the six core verbs), so it satisfies the narrower
// interface directly.
type resolverAdapter struct {
	inner qrdbsdk.TenantStoreResolver
}

// Resolve routes to the qrdb StoreResolver and returns the per-tenant store
// as an interfaces.KVStore. A sealed barrier tenant surfaces
// tenant.ErrTenantSealed unchanged so the routing store fails closed.
func (a *resolverAdapter) Resolve(ctx context.Context, tenantID string) (interfaces.KVStore, error) {
	store, err := a.inner.Resolve(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return store, nil
}

// tenantRoutingKVStore is an interfaces.KVStore that routes every operation to
// the per-tenant backend selected by the qrdb StoreResolver, using the tenant
// bound to the operation's context (iam.TenantFromContext). A barrier-isolated
// tenant's IAM data therefore lands in that tenant's own TenantBarrier-backed
// store (own DEK + namespace); a prefix / system / legacy tenant lands in the
// shared store. When the StoreResolver fails — most importantly when a barrier
// tenant is sealed (tenant.ErrTenantSealed) — the operation fails closed and
// NEVER falls back to the shared store.
//
// This wraps the routing at the KVStore seam so the IAM BackendStorage (and
// every other Backend* store) is unchanged: it still sees one interfaces.KVStore.
//
// The IAM stores embed the tenant ID in their key segments
// ("__iam:tenant:{tenantID}:..."), so co-locating multiple logical-prefix
// tenants in the shared store remains correct; barrier tenants additionally get
// physical + cryptographic isolation from the resolver.
type tenantRoutingKVStore struct {
	resolver storeResolver
}

// newTenantRoutingKVStore constructs a routing KVStore over the given resolver.
func newTenantRoutingKVStore(resolver storeResolver) *tenantRoutingKVStore {
	return &tenantRoutingKVStore{resolver: resolver}
}

var _ interfaces.KVStore = (*tenantRoutingKVStore)(nil)

// resolve selects the per-tenant store for the context's tenant. It fails
// closed: any resolver error (sealed barrier, lookup failure) propagates and
// the caller must not touch the shared store.
func (s *tenantRoutingKVStore) resolve(ctx context.Context) (interfaces.KVStore, error) {
	tenantID := iam.TenantFromContext(ctx)
	store, err := s.resolver.Resolve(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return store, nil
}

// Put routes the write to the context tenant's store.
func (s *tenantRoutingKVStore) Put(ctx context.Context, key string, value []byte) error {
	store, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	return store.Put(ctx, key, value)
}

// Get routes the read to the context tenant's store. The qrdb-backed stores
// return (nil, nil) for a missing key; this translates that to the
// quicbaas QBError{ErrNotFound} so the IAM BackendStorage existence checks
// (which rely on err==nil meaning "exists") keep their contract.
func (s *tenantRoutingKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	store, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	data, err := store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.kv.get"}
	}
	return data, nil
}

// Delete routes the delete to the context tenant's store.
func (s *tenantRoutingKVStore) Delete(ctx context.Context, key string) error {
	store, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	return store.Delete(ctx, key)
}

// Scan routes the prefix scan to the context tenant's store.
func (s *tenantRoutingKVStore) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	store, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	return store.Scan(ctx, prefix, fn)
}

// List routes the prefix listing to the context tenant's store.
func (s *tenantRoutingKVStore) List(ctx context.Context, prefix string) ([]string, error) {
	store, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return store.List(ctx, prefix)
}

// Exists routes the existence check to the context tenant's store.
func (s *tenantRoutingKVStore) Exists(ctx context.Context, key string) (bool, error) {
	store, err := s.resolve(ctx)
	if err != nil {
		return false, err
	}
	return store.Exists(ctx, key)
}
