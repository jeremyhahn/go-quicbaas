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
	"testing"

	qrdbkvstore "github.com/jeremyhahn/go-qrdb/pkg/kvstore"
	qrdbtenant "github.com/jeremyhahn/go-qrdb/pkg/tenant"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// fakeStoreResolver is a per-tenant routing resolver test double. Each tenant
// ID maps to its own in-memory store; the special set "sealed" returns
// tenant.ErrTenantSealed to exercise the fail-closed path. The empty / system
// tenant resolves to a designated shared store, mirroring the production
// resolver's prefix/system behaviour.
type fakeStoreResolver struct {
	shared interfaces.KVStore
	stores map[string]interfaces.KVStore
	sealed map[string]bool
}

func newFakeStoreResolver(shared interfaces.KVStore) *fakeStoreResolver {
	return &fakeStoreResolver{
		shared: shared,
		stores: make(map[string]interfaces.KVStore),
		sealed: make(map[string]bool),
	}
}

// barrierTenant registers tenantID as a barrier-isolated tenant with its own
// store and returns that store so the test can assert isolation.
func (f *fakeStoreResolver) barrierTenant(tenantID string) interfaces.KVStore {
	st := kvstore.NewMemory()
	f.stores[tenantID] = st
	return st
}

// sealTenant marks a barrier tenant as sealed so Resolve fails closed.
func (f *fakeStoreResolver) sealTenant(tenantID string) {
	f.sealed[tenantID] = true
}

func (f *fakeStoreResolver) Resolve(_ context.Context, tenantID string) (interfaces.KVStore, error) {
	if f.sealed[tenantID] {
		return nil, qrdbtenant.ErrTenantSealed
	}
	if qrdbtenant.IsSystemTenant(tenantID) {
		return f.shared, nil
	}
	if st, ok := f.stores[tenantID]; ok {
		return st, nil
	}
	// Unknown / prefix tenant: shared store (pre-isolation behaviour).
	return f.shared, nil
}

// nilGetStore returns (nil, nil) for any Get, mirroring the qrdb kvstore /
// TenantBarrier convention for a missing key. It lets us assert the routing
// store translates that into a quicbaas ErrNotFound.
type nilGetStore struct{ interfaces.KVStore }

func (nilGetStore) Get(context.Context, string) ([]byte, error) { return nil, nil }

// The qrdb (nil,nil) missing-key convention is translated to ErrNotFound by
// the routing store's Get.
func TestTenantRoutingKVStore_NilGetTranslatedToNotFound(t *testing.T) {
	resolver := newFakeStoreResolver(nilGetStore{})
	kv := newTenantRoutingKVStore(resolver)
	ctx := iam.ContextWithTenant(context.Background(), qrdbtenant.SystemTenantID)

	_, err := kv.Get(ctx, "__iam:any")
	if !dberrors.IsNotFound(err) {
		t.Fatalf("Get over (nil,nil) store err = %v, want ErrNotFound", err)
	}
}

// (a) A barrier-isolated tenant's IAM write lands in THAT tenant's store and
// not the shared store.
func TestTenantRoutingKVStore_BarrierTenantIsolated(t *testing.T) {
	shared := kvstore.NewMemory()
	resolver := newFakeStoreResolver(shared)
	tenantStore := resolver.barrierTenant("acme")

	kv := newTenantRoutingKVStore(resolver)
	ctx := iam.ContextWithTenant(context.Background(), "acme")

	if err := kv.Put(ctx, "__iam:tenant:acme:policy:p1", []byte("policy-data")); err != nil {
		t.Fatalf("Put(acme): %v", err)
	}

	// The value must be readable from the tenant's own store...
	got, err := tenantStore.Get(context.Background(), "__iam:tenant:acme:policy:p1")
	if err != nil {
		t.Fatalf("tenant store Get: %v", err)
	}
	if string(got) != "policy-data" {
		t.Fatalf("tenant store value = %q, want %q", got, "policy-data")
	}

	// ...and MUST NOT appear in the shared store.
	if _, err := shared.Get(context.Background(), "__iam:tenant:acme:policy:p1"); err == nil {
		t.Fatal("barrier tenant data leaked into the shared store")
	}

	// Round-trip through the routing store with the same tenant context.
	rt, err := kv.Get(ctx, "__iam:tenant:acme:policy:p1")
	if err != nil {
		t.Fatalf("routed Get(acme): %v", err)
	}
	if string(rt) != "policy-data" {
		t.Fatalf("routed value = %q, want %q", rt, "policy-data")
	}
}

// (b) A prefix / system tenant routes to the shared store.
func TestTenantRoutingKVStore_PrefixTenantSharedStore(t *testing.T) {
	shared := kvstore.NewMemory()
	resolver := newFakeStoreResolver(shared)
	resolver.barrierTenant("acme") // a separate barrier tenant must stay empty

	kv := newTenantRoutingKVStore(resolver)
	ctx := iam.ContextWithTenant(context.Background(), "prefixco")

	if err := kv.Put(ctx, "__iam:tenant:prefixco:app:c1", []byte("app-data")); err != nil {
		t.Fatalf("Put(prefixco): %v", err)
	}

	// The shared store holds it.
	got, err := shared.Get(context.Background(), "__iam:tenant:prefixco:app:c1")
	if err != nil {
		t.Fatalf("shared Get: %v", err)
	}
	if string(got) != "app-data" {
		t.Fatalf("shared value = %q, want %q", got, "app-data")
	}

	// The unrelated barrier tenant's store stays empty.
	if _, err := resolver.stores["acme"].Get(context.Background(), "__iam:tenant:prefixco:app:c1"); err == nil {
		t.Fatal("prefix tenant data leaked into a barrier tenant store")
	}
}

// (c) A sealed barrier tenant fails closed: the IAM op returns
// tenant.ErrTenantSealed and NEVER falls back to the shared store.
func TestTenantRoutingKVStore_SealedTenantFailsClosed(t *testing.T) {
	shared := kvstore.NewMemory()
	resolver := newFakeStoreResolver(shared)
	resolver.barrierTenant("acme")
	resolver.sealTenant("acme")

	kv := newTenantRoutingKVStore(resolver)
	ctx := iam.ContextWithTenant(context.Background(), "acme")

	// Put must fail closed.
	if err := kv.Put(ctx, "__iam:tenant:acme:user:u1", []byte("x")); !errors.Is(err, qrdbtenant.ErrTenantSealed) {
		t.Fatalf("Put(sealed) err = %v, want ErrTenantSealed", err)
	}
	// Get must fail closed.
	if _, err := kv.Get(ctx, "__iam:tenant:acme:user:u1"); !errors.Is(err, qrdbtenant.ErrTenantSealed) {
		t.Fatalf("Get(sealed) err = %v, want ErrTenantSealed", err)
	}
	// Delete / List / Exists / Scan likewise.
	if err := kv.Delete(ctx, "k"); !errors.Is(err, qrdbtenant.ErrTenantSealed) {
		t.Fatalf("Delete(sealed) err = %v, want ErrTenantSealed", err)
	}
	if _, err := kv.List(ctx, "p"); !errors.Is(err, qrdbtenant.ErrTenantSealed) {
		t.Fatalf("List(sealed) err = %v, want ErrTenantSealed", err)
	}
	if _, err := kv.Exists(ctx, "k"); !errors.Is(err, qrdbtenant.ErrTenantSealed) {
		t.Fatalf("Exists(sealed) err = %v, want ErrTenantSealed", err)
	}
	if err := kv.Scan(ctx, "p", func(string, []byte) error { return nil }); !errors.Is(err, qrdbtenant.ErrTenantSealed) {
		t.Fatalf("Scan(sealed) err = %v, want ErrTenantSealed", err)
	}

	// CRITICAL: nothing was written to the shared store on the failed op.
	keys, _ := shared.List(context.Background(), "__iam:")
	if len(keys) != 0 {
		t.Fatalf("sealed tenant op wrote %d keys to the shared store; must be 0", len(keys))
	}
}

// Get on a missing key translates the qrdb (nil,nil) convention into a
// quicbaas ErrNotFound so the IAM BackendStorage existence checks keep their
// contract (err==nil ⇒ exists).
func TestTenantRoutingKVStore_MissingKeyNotFound(t *testing.T) {
	shared := kvstore.NewMemory()
	resolver := newFakeStoreResolver(shared)
	kv := newTenantRoutingKVStore(resolver)
	ctx := iam.ContextWithTenant(context.Background(), qrdbtenant.SystemTenantID)

	_, err := kv.Get(ctx, "__iam:missing")
	if err == nil {
		t.Fatal("Get(missing) returned nil error; want ErrNotFound")
	}
	if !dberrors.IsNotFound(err) {
		t.Fatalf("Get(missing) err = %v, want ErrNotFound", err)
	}
}

// Success paths for Delete/Scan/List/Exists route to the resolved tenant store.
func TestTenantRoutingKVStore_SuccessPaths(t *testing.T) {
	shared := kvstore.NewMemory()
	resolver := newFakeStoreResolver(shared)
	kv := newTenantRoutingKVStore(resolver)
	ctx := iam.ContextWithTenant(context.Background(), qrdbtenant.SystemTenantID)

	if err := kv.Put(ctx, "__iam:k1", []byte("v1")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := kv.Put(ctx, "__iam:k2", []byte("v2")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	ok, err := kv.Exists(ctx, "__iam:k1")
	if err != nil || !ok {
		t.Fatalf("Exists = %v, %v; want true, nil", ok, err)
	}

	keys, err := kv.List(ctx, "__iam:")
	if err != nil || len(keys) != 2 {
		t.Fatalf("List = %v, %v; want 2 keys", keys, err)
	}

	var scanned int
	if err := kv.Scan(ctx, "__iam:", func(string, []byte) error { scanned++; return nil }); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if scanned != 2 {
		t.Fatalf("Scan visited %d keys, want 2", scanned)
	}

	if err := kv.Delete(ctx, "__iam:k1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ok, _ := kv.Exists(ctx, "__iam:k1"); ok {
		t.Fatalf("key still exists after Delete")
	}
}

// resolveError makes the resolver itself fail (not sealed) so the non-sealed
// error branch of resolve() is exercised on every verb.
type errResolver struct{ err error }

func (e errResolver) Resolve(context.Context, string) (interfaces.KVStore, error) {
	return nil, e.err
}

func TestTenantRoutingKVStore_ResolverErrorAllVerbs(t *testing.T) {
	want := errors.New("resolver down")
	kv := newTenantRoutingKVStore(errResolver{err: want})
	ctx := context.Background()

	if err := kv.Put(ctx, "k", nil); !errors.Is(err, want) {
		t.Fatalf("Put err = %v", err)
	}
	if _, err := kv.Get(ctx, "k"); !errors.Is(err, want) {
		t.Fatalf("Get err = %v", err)
	}
	if err := kv.Delete(ctx, "k"); !errors.Is(err, want) {
		t.Fatalf("Delete err = %v", err)
	}
	if _, err := kv.List(ctx, "p"); !errors.Is(err, want) {
		t.Fatalf("List err = %v", err)
	}
	if _, err := kv.Exists(ctx, "k"); !errors.Is(err, want) {
		t.Fatalf("Exists err = %v", err)
	}
	if err := kv.Scan(ctx, "p", func(string, []byte) error { return nil }); !errors.Is(err, want) {
		t.Fatalf("Scan err = %v", err)
	}
}

// tenantRoutedKVStore returns a usable store. Since go-qrdb v0.10.0 (#41)
// every node exposes a StoreResolver — a seal-disabled node routes IAM
// through a confined "{tenant}/" keyspace too — so a memory standalone node
// yields the routing store, and TenantRoutedKVStore is that same store.
func TestIamTenantKVStore_SealDisabledNodeRoutes(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	s := &Server{
		logger:     buildTestLogger(t),
		qrdbNode:   node,
		qrdbClient: newNodeBackedShardedClient(node),
	}

	if node.Services().StoreResolver == nil {
		t.Fatal("a seal-disabled qrdb node must expose a StoreResolver (go-qrdb #41)")
	}
	kv := s.tenantRoutedKVStore()
	if _, ok := kv.(*tenantRoutingKVStore); !ok {
		t.Fatalf("tenantRoutedKVStore = %T, want the tenant routing store", kv)
	}
	if _, ok := s.TenantRoutedKVStore().(*tenantRoutingKVStore); !ok {
		t.Fatalf("TenantRoutedKVStore = %T, want the tenant routing store", s.TenantRoutedKVStore())
	}
}

// tenantRoutedKVStore falls back to the shared platform shard when the node
// exposes no StoreResolver.
func TestIamTenantKVStore_NodeNoResolver(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })
	node.Services().StoreResolver = nil

	s := &Server{
		logger:     buildTestLogger(t),
		qrdbNode:   node,
		qrdbClient: newNodeBackedShardedClient(node),
	}

	kv := s.tenantRoutedKVStore()
	if kv == nil {
		t.Fatal("tenantRoutedKVStore must return a store")
	}
	if _, ok := kv.(*tenantRoutingKVStore); ok {
		t.Fatal("tenantRoutedKVStore must not route without a StoreResolver")
	}
}

// iamTenantScopeStore is built over the resolver every node now exposes.
func TestIamTenantScopeStore_SealDisabledNodeBuilds(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	s := &Server{
		logger:     buildTestLogger(t),
		qrdbNode:   node,
		qrdbClient: newNodeBackedShardedClient(node),
	}

	if s.iamTenantScopeStore() == nil {
		t.Fatal("iamTenantScopeStore must be built when the node exposes a StoreResolver")
	}
}

// iamTenantScopeStore returns a real resolver on a seal-disabled node.
//
// This test asserted the opposite until go-qrdb v0.10.0-alpha, and the
// inversion is the point rather than an accident. Upstream go-qrdb#41 closed a
// hole in which the seal-disabled boot path installed no StoreResolver, so IAM
// ran unrouted and every tenant's identity records shared one keyspace — "no
// seal, no isolation". The resolver is now installed on that path too, so a
// standalone memory node exposes one and per-tenant permission scopes work
// where they previously degraded silently.
//
// Asserting non-nil here is what stops that hole reopening: a future upgrade
// that dropped the resolver from the seal-disabled path would restore the old
// behaviour, and the old test would have called that a pass.
func TestIamTenantScopeStore_NodeProvidesResolver(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	s := &Server{
		logger:     buildTestLogger(t),
		qrdbNode:   node,
		qrdbClient: newNodeBackedShardedClient(node),
	}

	svcs := node.Services()
	if svcs == nil || svcs.StoreResolver == nil {
		t.Fatal("a standalone memory node must expose a StoreResolver; without one IAM runs unrouted (go-qrdb#41)")
	}
	if s.iamTenantScopeStore() == nil {
		t.Fatal("iamTenantScopeStore must build a scope store when the node exposes a StoreResolver")
	}
}

// iamTenantScopeStore returns nil when there is no node at all.
func TestIamTenantScopeStore_NilNode(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	if s.iamTenantScopeStore() != nil {
		t.Fatal("iamTenantScopeStore must be nil without a node")
	}
}

// fakeQRDBResolver implements qrdb's tenant.StoreResolver so the
// resolverAdapter wrapper can be exercised without a live qrdb node. Resolve
// returns the embedded qrdb kvstore.KVStore on success, or err when set.
type fakeQRDBResolver struct {
	store qrdbkvstore.KVStore
	err   error
}

func (f fakeQRDBResolver) Resolve(context.Context, string) (qrdbkvstore.KVStore, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.store, nil
}

// fakeQRDBStore satisfies qrdb's kvstore.KVStore by embedding the interface.
// The resolverAdapter only passes the store through; none of the embedded
// methods are invoked, so the nil embedding is safe and keeps the double minimal.
type fakeQRDBStore struct{ qrdbkvstore.KVStore }

// resolverAdapter.Resolve passes the qrdb store through as interfaces.KVStore on
// success.
func TestResolverAdapter_ResolveSuccess(t *testing.T) {
	want := fakeQRDBStore{}
	a := &resolverAdapter{inner: fakeQRDBResolver{store: want}}

	got, err := a.Resolve(context.Background(), "acme")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got == nil {
		t.Fatal("Resolve returned nil store on success")
	}
}

// resolverAdapter.Resolve surfaces the underlying resolver error (e.g. a sealed
// barrier tenant) unchanged and returns a nil store.
func TestResolverAdapter_ResolveError(t *testing.T) {
	a := &resolverAdapter{inner: fakeQRDBResolver{err: qrdbtenant.ErrTenantSealed}}

	store, err := a.Resolve(context.Background(), "sealed")
	if !errors.Is(err, qrdbtenant.ErrTenantSealed) {
		t.Fatalf("Resolve err = %v, want ErrTenantSealed", err)
	}
	if store != nil {
		t.Fatalf("Resolve returned non-nil store on error: %v", store)
	}
}

// An orchestration request must resolve to its tenant's barrier just like an
// IAM request does.
//
// pkg/iam and pkg/orchestration each keep their own unexported context key, so
// before the routing store consulted both, an orchestration write resolved to
// the empty tenant and silently landed in the shared store instead of the
// caller's barrier. This pins the fix.
func TestTenantRoutingKVStore_RoutesOrchestrationContexts(t *testing.T) {
	tests := []struct {
		name    string
		withCtx func(context.Context, string) context.Context
	}{
		{name: "iam context key", withCtx: iam.ContextWithTenant},
		{name: "orchestration context key", withCtx: orchestration.ContextWithTenant},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shared := kvstore.NewMemory()
			resolver := newFakeStoreResolver(shared)
			tenantStore := resolver.barrierTenant("acme")
			kv := newTenantRoutingKVStore(resolver)

			ctx := tc.withCtx(context.Background(), "acme")
			const key = "orch/project/acme/p1"

			if err := kv.Put(ctx, key, []byte("project-data")); err != nil {
				t.Fatalf("Put: %v", err)
			}

			got, err := tenantStore.Get(context.Background(), key)
			if err != nil {
				t.Fatalf("tenant store Get: %v", err)
			}
			if string(got) != "project-data" {
				t.Fatalf("tenant store value = %q, want %q", got, "project-data")
			}
			if _, err := shared.Get(context.Background(), key); !dberrors.IsNotFound(err) {
				t.Fatal("a barrier tenant's record must never land in the shared store")
			}
		})
	}
}

// A sealed barrier must fail closed for orchestration records too — never fall
// back to the shared store.
func TestTenantRoutingKVStore_OrchestrationContextFailsClosedWhenSealed(t *testing.T) {
	shared := kvstore.NewMemory()
	resolver := newFakeStoreResolver(shared)
	resolver.barrierTenant("acme")
	resolver.sealTenant("acme")
	kv := newTenantRoutingKVStore(resolver)

	ctx := orchestration.ContextWithTenant(context.Background(), "acme")
	const key = "orch/task/acme/t1"

	if err := kv.Put(ctx, key, []byte("task-data")); !errors.Is(err, qrdbtenant.ErrTenantSealed) {
		t.Fatalf("Put against a sealed barrier err = %v, want ErrTenantSealed", err)
	}
	if _, err := shared.Get(context.Background(), key); !dberrors.IsNotFound(err) {
		t.Fatal("a sealed tenant's write must not fall back to the shared store")
	}
}

// tenantFromAnyContext prefers the IAM key and falls back to orchestration's.
func TestTenantFromAnyContext(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{name: "no tenant", ctx: context.Background(), want: ""},
		{
			name: "iam only",
			ctx:  iam.ContextWithTenant(context.Background(), "from-iam"),
			want: "from-iam",
		},
		{
			name: "orchestration only",
			ctx:  orchestration.ContextWithTenant(context.Background(), "from-orch"),
			want: "from-orch",
		},
		{
			name: "iam wins when both are set",
			ctx: orchestration.ContextWithTenant(
				iam.ContextWithTenant(context.Background(), "from-iam"), "from-orch"),
			want: "from-iam",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tenantFromAnyContext(tc.ctx); got != tc.want {
				t.Fatalf("tenantFromAnyContext = %q, want %q", got, tc.want)
			}
		})
	}
}

// =============================================================================
// Guarded-write forwarding
// =============================================================================

// TestTenantRoutingKVStore_ForwardsGuardedWrites proves the router carries the
// guarded verbs through to the tenant's own store, so a caller that needs a
// decision under concurrency gets the tenant store's decision rather than the
// router's approximation of one.
func TestTenantRoutingKVStore_ForwardsGuardedWrites(t *testing.T) {
	resolver := newFakeStoreResolver(qrdbsdk.NewMemoryKVStore())
	kv := newTenantRoutingKVStore(resolver)
	ctx := context.Background()

	stored, err := kv.PutIfAbsent(ctx, "k", []byte("first"))
	if err != nil {
		t.Fatalf("PutIfAbsent: %v", err)
	}
	if !stored {
		t.Fatal("the first guarded create on an empty key was refused")
	}

	// The second must LOSE rather than overwrite: that is the whole guarantee.
	stored, err = kv.PutIfAbsent(ctx, "k", []byte("second"))
	if err != nil {
		t.Fatalf("second PutIfAbsent: %v", err)
	}
	if stored {
		t.Fatal("a guarded create overwrote an existing key")
	}
	got, err := kv.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "first" {
		t.Fatalf("the losing create changed the value to %q", got)
	}

	// The release half: a stale expectation must not delete the live value.
	deleted, err := kv.CompareAndDelete(ctx, "k", []byte("second"))
	if err != nil {
		t.Fatalf("CompareAndDelete: %v", err)
	}
	if deleted {
		t.Fatal("a guarded delete removed a key holding a different value")
	}
	if deleted, err = kv.CompareAndDelete(ctx, "k", []byte("first")); err != nil || !deleted {
		t.Fatalf("CompareAndDelete on the held value = (%v, %v), want (true, nil)", deleted, err)
	}
}

// TestTenantRoutingKVStore_GuardedWritesRefuseANonAtomicStore proves the router
// FAILS CLOSED when the resolved tenant store cannot decide a contested write.
//
// The refusal must be an error, never a plausible-looking false. A caller
// reading deleted=false concludes "the key holds something else, my claim was
// replaced" and stops trying to release it — so reporting that when the store
// could not be asked at all would make a live claim be abandoned. "I cannot
// answer" and "the answer is no" are different facts.
func TestTenantRoutingKVStore_GuardedWritesRefuseANonAtomicStore(t *testing.T) {
	// kvstore.NewMemory wraps a raw storage engine with no conditional write,
	// which is exactly the store this must refuse.
	kv := newTenantRoutingKVStore(newFakeStoreResolver(kvstore.NewMemory()))
	ctx := context.Background()

	stored, err := kv.PutIfAbsent(ctx, "k", []byte("v"))
	if !errors.Is(err, ErrTenantStoreNotAtomic) {
		t.Fatalf("PutIfAbsent err = %T: %v, want ErrTenantStoreNotAtomic", err, err)
	}
	if stored {
		t.Error("a refused guarded create reported stored")
	}

	deleted, err := kv.CompareAndDelete(ctx, "k", []byte("v"))
	if !errors.Is(err, ErrTenantStoreNotAtomic) {
		t.Fatalf("CompareAndDelete err = %T: %v, want ErrTenantStoreNotAtomic", err, err)
	}
	if deleted {
		t.Error("a refused guarded delete reported deleted")
	}

	// And nothing was written by the refusal.
	if exists, existsErr := kv.Exists(ctx, "k"); existsErr != nil || exists {
		t.Errorf("a refused guarded create left a key behind (exists=%v, err=%v)", exists, existsErr)
	}
}

// TestTenantRoutingKVStore_GuardedWritesFailClosedOnResolverError proves a
// resolver failure — a sealed barrier tenant, most importantly — refuses the
// guarded verbs exactly as it refuses the six plain ones, rather than falling
// back to the shared store.
func TestTenantRoutingKVStore_GuardedWritesFailClosedOnResolverError(t *testing.T) {
	want := errors.New("resolver down")
	kv := newTenantRoutingKVStore(errResolver{err: want})
	ctx := context.Background()

	if _, err := kv.PutIfAbsent(ctx, "k", nil); !errors.Is(err, want) {
		t.Fatalf("PutIfAbsent err = %v, want the resolver error", err)
	}
	if _, err := kv.CompareAndDelete(ctx, "k", nil); !errors.Is(err, want) {
		t.Fatalf("CompareAndDelete err = %v, want the resolver error", err)
	}
}

// =============================================================================
// The assumption pkg/iam's refresh-token value index depends on (#274).
//
// iam.tokenValueIndexContext clears the IAM tenant so the index is written to,
// and read from, one store regardless of which tenant issued the credential.
// That works because a cleared IAM tenant resolves to the shared store — but
// tenantFromAnyContext falls through to the ORCHESTRATION tenant key when the
// IAM one is empty, and pkg/iam cannot clear a key it must not depend on.
//
// The pair below pins both halves. If the fall-through is ever reordered, or an
// orchestration tenant is ever stamped on a token-storage context, the second
// test is the one that says so — loudly, here, rather than as an unredeemable
// refresh credential in production.
// =============================================================================

// A cleared IAM tenant routes to the shared store, which is what makes the
// value index resolvable from the untenanted public refresh endpoint.
func TestTenantRoutingKVStore_ClearedIAMTenantRoutesToSharedStore(t *testing.T) {
	shared := kvstore.NewMemory()
	resolver := newFakeStoreResolver(shared)
	tenantStore := resolver.barrierTenant("burnin")

	kv := newTenantRoutingKVStore(resolver)
	const key = "__iam:tokenval:deadbeef"

	// Written from inside a tenant's login context, with the tenant cleared.
	issueCtx := iam.ContextWithTenant(context.Background(), "burnin")
	if err := kv.Put(iam.ContextWithTenant(issueCtx, ""), key, []byte("burnin:jti-1")); err != nil {
		t.Fatalf("Put with cleared tenant: %v", err)
	}

	// Read back with no tenant at all — the public refresh endpoint's context.
	got, err := kv.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("untenanted Get: %v (the #274 401)", err)
	}
	if string(got) != "burnin:jti-1" {
		t.Fatalf("value = %q, want %q", got, "burnin:jti-1")
	}
	if _, err := tenantStore.Get(context.Background(), key); err == nil {
		t.Fatal("the index landed in the tenant's own store, where an untenanted read cannot reach it")
	}
}

// An ORCHESTRATION tenant overrides a cleared IAM tenant. This is the documented
// hazard, not a desired behaviour: it is safe today only because token issuance
// and refresh are IAM request paths and the orchestration key is stamped solely
// by the orchestrator handlers. A change that lets the two meet must revisit
// iam.tokenValueIndexContext, and this test is the tripwire.
func TestTenantRoutingKVStore_OrchestrationTenantOverridesClearedIAMTenant(t *testing.T) {
	shared := kvstore.NewMemory()
	resolver := newFakeStoreResolver(shared)
	orchStore := resolver.barrierTenant("burnin")

	kv := newTenantRoutingKVStore(resolver)
	const key = "__iam:tokenval:deadbeef"

	ctx := orchestration.ContextWithTenant(context.Background(), "burnin")
	if err := kv.Put(iam.ContextWithTenant(ctx, ""), key, []byte("burnin:jti-1")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := orchStore.Get(context.Background(), key); err != nil {
		t.Fatalf("expected the orchestration tenant to win the routing decision: %v", err)
	}
	if _, err := shared.Get(context.Background(), key); err == nil {
		t.Fatal("a cleared IAM tenant reached the shared store despite an orchestration tenant; " +
			"the fall-through changed and iam.tokenValueIndexContext must be revisited")
	}
}
