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
	"errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// tenantRoutedKVStore returns the interfaces.KVStore that backs quicbaas's
// tenant-owned data — IAM records and durable orchestration records (projects,
// agent registrations, tasks). When the embedded qrdb node exposes a per-tenant
// StoreResolver (seal enabled with a barrier registry, see go-qrdb
// lifecycle.BuildServices), the returned store routes every operation to the
// context tenant's backend so a barrier-isolated tenant's data is stored in its
// own barrier (own DEK + namespace) and a prefix / system tenant uses the
// shared store. A sealed barrier tenant fails closed.
//
// When no resolver is available (seal disabled, storage-less / legacy boot),
// the store is the shared platform shard — the prior single-store behaviour —
// so existing single-tenant and dev deployments are unaffected.
func (s *Server) tenantRoutedKVStore() interfaces.KVStore {
	if store := s.tenantRoutingStore(); store != nil {
		return store
	}
	return s.GetKVStore(qbshards.PlatformShardID)
}

// TenantRoutedKVStore exposes the durable store IAM and the other tenant-aware
// subsystems actually write through.
//
// GetKVStore(PlatformShardID) reaches the shard beneath the routing layer, and
// the two answered identically for as long as no StoreResolver was installed on
// seal-disabled boots. Since go-qrdb v0.10.0-alpha a resolver is always
// installed, so the routed store confines records to their tenant's namespace
// and the raw shard no longer sees them at the keys IAM used. A caller that
// wants the records IAM persisted — an out-of-package test proving the on-disk
// contract, most of all — must resolve through the same layer that wrote them,
// or it reads an empty keyspace and concludes persistence is broken.
//
// This returns a freshly resolved durable store, never a cached in-memory
// component, so it remains a legitimate way to assert what survived a restart.
//
// SCOPE FENCE. This exists for black-box verification of the on-disk contract.
// It is NOT a consumer surface, and it must never be re-exported through
// sdk/go: a routed-store accessor in consumer hands is a service-layer bypass,
// handing callers the raw tenant keyspace with none of the admission,
// authorization or audit the services above it apply. Wanting it in the SDK is
// therefore a PROMOTION, not a re-export — it triggers the security re-review
// rule (company conventions, sdk-conventions.md "Promotion raises the security
// bar") and is decided there, never by widening this comment.
func (s *Server) TenantRoutedKVStore() interfaces.KVStore {
	return s.tenantRoutedKVStore()
}

// tenantRoutingStore returns the per-tenant routing store, or nil when the
// embedded node exposes no StoreResolver.
func (s *Server) tenantRoutingStore() interfaces.KVStore {
	node := s.getQRDBNode()
	if node == nil {
		return nil
	}
	svcs := node.Services()
	if svcs == nil || svcs.StoreResolver == nil {
		return nil
	}
	return newTenantRoutingKVStore(&resolverAdapter{inner: s.admittedStoreResolver(svcs.StoreResolver)})
}

// durableKVStore is tenantRoutedKVStore without the panic: it returns nil when
// the server has no qrdb SDK client, which is the case for storage-less boots
// and for unit tests that construct a bare Server. Callers decide what a
// missing durable store means for them rather than crashing the daemon.
func (s *Server) durableKVStore() interfaces.KVStore {
	if store := s.tenantRoutingStore(); store != nil {
		return store
	}
	if s.qrdbClient == nil {
		return nil
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
	node := s.getQRDBNode()
	if node == nil {
		return nil
	}
	svcs := node.Services()
	if svcs == nil || svcs.StoreResolver == nil {
		return nil
	}
	store, err := qrdbsdk.NewTenantScopeStore(s.admittedStoreResolver(svcs.StoreResolver))
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

// admittingStoreResolver refuses to resolve a tenant whose unseal has not yet
// passed its unseal gates: such a tenant's barrier may already be open, but
// its stores stay closed until the hooks admit it.
type admittingStoreResolver struct {
	inner qrdbsdk.TenantStoreResolver
	hooks *service.TenantUnsealHooks
}

// Resolve returns the hooks' admission error for a held tenant, and the inner
// resolver's store otherwise. An unseal gate's own read of the tenant it is
// admitting (service.InUnsealGate) reaches the inner resolver. The token-liveness
// reads of a held tenant (tenantctx.InAuthRecordRead) reach it too, so its admin
// can authenticate the retry or the seal that clears the hold, and every other
// read stays refused. The ABAC sync's reads of a held tenant's policies
// (tenantctx.InPolicySyncRead) reach it too, so a reload for another tenant
// keeps its deny rules. The system admin's repair of a held tenant's policies
// (tenantctx.InPolicyRepair) reaches it too, so the policy that holds the
// tenant can be fixed and the next unseal's gate can pass.
func (a *admittingStoreResolver) Resolve(ctx context.Context, tenantID string) (qrdbsdk.KVStore, error) {
	if !service.InUnsealGate(ctx, tenantID) && !tenantctx.InAuthRecordRead(ctx, tenantID) && !tenantctx.InPolicySyncRead(ctx, tenantID) && !tenantctx.InPolicyRepair(ctx, tenantID) {
		if err := a.hooks.Admit(tenantID); err != nil {
			return nil, err
		}
	}
	return a.inner.Resolve(ctx, tenantID)
}

// admittedStoreResolver wraps r so that it resolves no tenant the server's
// unseal hooks hold.
func (s *Server) admittedStoreResolver(r qrdbsdk.TenantStoreResolver) qrdbsdk.TenantStoreResolver {
	return &admittingStoreResolver{inner: r, hooks: &s.tenantUnsealHooks}
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
// bound to the operation's context (see tenantFromAnyContext). A
// barrier-isolated tenant's data therefore lands in that tenant's own
// TenantBarrier-backed store (own DEK + namespace); a prefix / system / legacy
// tenant lands in the shared store. When the StoreResolver fails — most
// importantly when a barrier tenant is sealed (tenant.ErrTenantSealed) — the
// operation fails closed and NEVER falls back to the shared store.
//
// This wraps the routing at the KVStore seam so its consumers are unchanged:
// the IAM BackendStorage (and every other Backend* store) and the durable
// orchestration stores each still see one plain interfaces.KVStore.
//
// Both consumers also embed the tenant ID in their key segments — IAM as
// "__iam:tenant:{tenantID}:...", orchestration as "orch/{entity}/{tenantID}/..."
// — so co-locating multiple logical-prefix tenants in the shared store remains
// correct; barrier tenants additionally get physical + cryptographic isolation
// from the resolver.
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
	store, err := s.resolver.Resolve(ctx, tenantFromAnyContext(ctx))
	if err != nil {
		return nil, err
	}
	return store, nil
}

// SharesStore reports whether contexts a and b resolve to the same store. One
// key names one physical record under both contexts exactly when they do; the
// resolvers cache one store per tenant namespace, so the comparison is identity.
func (s *tenantRoutingKVStore) SharesStore(a, b context.Context) (bool, error) {
	storeA, err := s.resolve(a)
	if err != nil {
		return false, err
	}
	storeB, err := s.resolve(b)
	if err != nil {
		return false, err
	}
	return storeA == storeB, nil
}

// tenantFromAnyContext reads the request tenant from whichever context key the
// calling subsystem stamped.
//
// pkg/iam and pkg/orchestration each define their own unexported context key —
// orchestration deliberately keeps a local one to avoid depending on the IAM
// package — so the two are distinct keys carrying the same value. The routing
// store sits underneath both subsystems and must honour either, otherwise an
// orchestration request would resolve to the empty tenant and land in the
// shared store instead of the caller's barrier.
func tenantFromAnyContext(ctx context.Context) string {
	if tenantID, ok := tenantctx.Lookup(ctx); ok {
		return tenantID
	}
	return orchestration.TenantFromContext(ctx)
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

// =============================================================================
// Guarded writes
// =============================================================================

// ErrTenantStoreNotAtomic reports a resolved per-tenant store that cannot
// perform a write whose outcome is defined under concurrency.
//
// Reported rather than emulated, ALWAYS. A caller reaching for a guarded write
// is one that cannot be correct without it — a uniqueness invariant, a claim, a
// single-use token — and answering with an Exists-then-Put emulation would
// deliver exactly the race the verb exists to eliminate while reporting
// success. The caller must be able to fail rather than proceed on a guarantee
// it did not get.
var ErrTenantStoreNotAtomic = errors.New("tenant kv: resolved store cannot perform guarded writes")

// atomicStore is the guarded half of go-qrdb's AtomicKVStore: the two verbs
// whose outcome is defined under concurrency.
//
// Declared here as the narrow capability actually forwarded rather than reusing
// qrdbsdk.AtomicKVStore, because that interface embeds go-qrdb's full twelve
// method KVStore — including the secondary-index methods — and the per-tenant
// stores this router hands out satisfy it while the ROUTER, which speaks the
// six-verb interfaces.KVStore, never could. Asserting against the wide
// interface here would refuse every store, including the ones that can do the
// work.
//
// ALL THREE VERBS TOGETHER, never a subset. go-qrdb defines them as one
// capability, and each pair without the third is half a primitive: a guarded
// create paired with an unguarded delete lets a racing releaser delete a third
// party's claim (go-qrdb#41), and a guarded create and delete with no guarded
// EDIT between them leaves the transition in the middle decided nowhere — which
// is where a read-modify-write on a shared record silently loses one of two
// concurrent changes. A router forwarding a subset presents a capability it only
// partly has.
//
// CompareAndSwap was the missing one, and IAM group membership is what noticed:
// adding a user to a group was read-append-write, so two concurrent creates
// naming one group each wrote their own member over the other's.
type atomicStore interface {
	PutIfAbsent(ctx context.Context, key string, value []byte) (stored bool, err error)
	CompareAndDelete(ctx context.Context, key string, expected []byte) (deleted bool, err error)
	CompareAndSwap(ctx context.Context, key string, expected, next []byte) (swapped bool, err error)
}

// guarded resolves the context tenant's store and asserts it can decide a
// contested write, failing closed when it cannot.
func (s *tenantRoutingKVStore) guarded(ctx context.Context) (atomicStore, error) {
	store, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	atomic, ok := store.(atomicStore)
	if !ok {
		return nil, ErrTenantStoreNotAtomic
	}
	return atomic, nil
}

// PutIfAbsent routes a guarded create to the context tenant's store.
//
// The tenant stores decide this inside go-qrdb's state machine — a Raft
// proposal whose condition is evaluated at the entry's position in the
// committed log — so the uniqueness it reports holds across nodes and not
// merely within this process. That is the guarantee callers are asking for, and
// the reason a store that cannot give it must refuse rather than approximate
// it.
func (s *tenantRoutingKVStore) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	store, err := s.guarded(ctx)
	if err != nil {
		return false, err
	}
	return store.PutIfAbsent(ctx, key, value)
}

// CompareAndDelete routes a guarded delete to the context tenant's store.
//
// A refusal here is NOT reported as deleted=false. That answer means something
// specific — the key is absent, or holds a value other than the one passed —
// and a holder reads it as "my claim was already replaced, stop trying to
// release it". Returning it when the store could not be asked at all would make
// that holder abandon a claim that is still live and still its own. "I cannot
// answer" and "the answer is no" are different, and only the error distinguishes
// them.
func (s *tenantRoutingKVStore) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	store, err := s.guarded(ctx)
	if err != nil {
		return false, err
	}
	return store.CompareAndDelete(ctx, key, expected)
}

// CompareAndSwap routes a guarded edit to the context tenant's store.
//
// It is the verb a record with more than one writer needs: read, change,
// commit-only-if-unchanged. Without it a caller has to read and then write
// unconditionally, and the second writer's copy — assembled before the first
// committed — erases the first's change with no error anywhere. That is not a
// rare interleaving for a shared record like a group's member list, where every
// user added to the group is a writer.
//
// A refusal is an error, not swapped=false, for the same reason it is on
// CompareAndDelete: swapped=false tells the caller the record moved under it,
// which is an instruction to re-read and retry. Saying that when the store
// could not be asked would send a caller round a retry loop that can never
// succeed.
func (s *tenantRoutingKVStore) CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error) {
	store, err := s.guarded(ctx)
	if err != nil {
		return false, err
	}
	return store.CompareAndSwap(ctx, key, expected, next)
}

// InviteStoreError reports a failure constructing the per-tenant invitation
// store.
type InviteStoreError struct {
	Cause error
}

// Error implements the error interface.
func (e *InviteStoreError) Error() string {
	return "iam: failed to build the per-tenant invite store: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *InviteStoreError) Unwrap() error { return e.Cause }

// buildTenantInviteStore constructs the IAM invitation store over go-qrdb's
// per-tenant invitation store.
//
// quicbaas keeps no invite store of its own. Token generation, digesting,
// single-use redemption and tenant routing are all qrdb's, which is the point:
// this repository previously kept a parallel copy of each. A nil node yields a
// nil store, which the caller treats as "invitations unavailable" rather than
// silently falling back to a non-persistent one — an invite that vanished on
// restart would strand the operator holding the activation URL.
func (s *Server) buildTenantInviteStore() (iam.InviteStore, error) {
	node := s.getQRDBNode()
	if node == nil {
		return nil, nil
	}
	svcs := node.Services()
	if svcs == nil || svcs.StoreResolver == nil {
		return nil, nil
	}
	store, err := qrdbsdk.NewTenantInviteStore(s.admittedStoreResolver(svcs.StoreResolver))
	if err != nil {
		return nil, &InviteStoreError{Cause: err}
	}
	return iam.NewQRDBInviteStore(store), nil
}

// buildUserActivationClaims constructs the per-user activation claims over
// the same tenant store resolver the invitation store uses, so a claim lives
// beside the invites it arbitrates. A nil node yields nil claims, which the
// PIV activator refuses exactly as it refuses a nil invitation store.
func (s *Server) buildUserActivationClaims() (*iam.UserActivationClaims, error) {
	node := s.getQRDBNode()
	if node == nil {
		return nil, nil
	}
	svcs := node.Services()
	if svcs == nil || svcs.StoreResolver == nil {
		return nil, nil
	}
	return iam.NewUserActivationClaims(s.admittedStoreResolver(svcs.StoreResolver))
}
