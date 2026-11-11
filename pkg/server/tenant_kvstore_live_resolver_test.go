//go:build !integration

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

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbkvstore "github.com/jeremyhahn/go-qrdb/pkg/kvstore"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	pkgerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// namedQRDBStore is a qrdb KVStore double told apart by its name. Resolve only
// passes the store through, so none of the embedded methods is called.
type namedQRDBStore struct {
	qrdbkvstore.KVStore
	name string
}

// liveStoreResolver asks for the current resolver on every Resolve: it serves
// the resolver in place now, passes that resolver's error through unchanged,
// and refuses as unavailable while there is none.
func TestLiveStoreResolver_FollowsTheCurrentResolver(t *testing.T) {
	var current qrdbsdk.TenantStoreResolver
	live := &liveStoreResolver{current: func() qrdbsdk.TenantStoreResolver { return current }}
	ctx := context.Background()

	_, err := live.Resolve(ctx, "acme")
	require.ErrorIs(t, err, ErrTenantStoreUnavailable)
	require.True(t, hasErrorCode(err, pkgerrors.ErrUnavailable), "no resolver must be answered as unavailable, got %v", err)

	first := namedQRDBStore{name: "first graph"}
	current = fakeQRDBResolver{store: first}
	got, err := live.Resolve(ctx, "acme")
	require.NoError(t, err)
	require.Equal(t, first, got)

	// The node replaced its graph: the next call is served by the new resolver.
	second := namedQRDBStore{name: "second graph"}
	current = fakeQRDBResolver{store: second}
	got, err = live.Resolve(ctx, "acme")
	require.NoError(t, err)
	require.Equal(t, second, got, "the store must come from the resolver in place now")

	current = fakeQRDBResolver{err: qrdbsdk.ErrTenantStoreSealed}
	_, err = live.Resolve(ctx, "acme")
	require.ErrorIs(t, err, qrdbsdk.ErrTenantStoreSealed, "a sealed tenant must stay sealed, not unavailable")
	require.False(t, errors.Is(err, ErrTenantStoreUnavailable))

	current = nil
	_, err = live.Resolve(ctx, "acme")
	require.ErrorIs(t, err, ErrTenantStoreUnavailable, "a resolver seen earlier must not be kept")
}

// A routing store taken while one node was current keeps working after the
// server's node (and so its service graph) is replaced and the first node is
// closed: it reads and writes the second node's storage, sees none of the
// first's records, and fails closed while there is no node at all.
func TestTenantRoutedKVStore_FollowsAReplacedServiceGraph(t *testing.T) {
	first := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = first.Close() })
	second := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = second.Close() })

	s := &Server{logger: buildTestLogger(t), qrdbNode: first}
	store := s.tenantRoutedKVStore()
	require.IsType(t, &tenantRoutingKVStore{}, store)

	ctx := tenantctx.WithTenant(context.Background(), "acme")
	require.NoError(t, store.Put(ctx, "graph/first", []byte("one")))
	got, err := store.Get(ctx, "graph/first")
	require.NoError(t, err)
	require.Equal(t, "one", string(got))

	s.setQRDBNode(second)
	require.NoError(t, first.Close(), "the replaced graph's storage is closed, as a migration closes it")

	_, err = store.Get(ctx, "graph/first")
	require.True(t, hasErrorCode(err, pkgerrors.ErrNotFound),
		"the store must read the second node, which never held the key, got %v", err)
	require.NoError(t, store.Put(ctx, "graph/second", []byte("two")))
	got, err = store.Get(ctx, "graph/second")
	require.NoError(t, err)
	require.Equal(t, "two", string(got))

	// The record is in the second node's own resolver, not in a private copy.
	direct, err := second.Services().StoreResolver.Resolve(ctx, "acme")
	require.NoError(t, err)
	raw, err := direct.Get(ctx, "graph/second")
	require.NoError(t, err)
	require.Equal(t, "two", string(raw))

	s.setQRDBNode(nil)
	_, err = store.Get(ctx, "graph/second")
	require.ErrorIs(t, err, ErrTenantStoreUnavailable)
	require.Error(t, store.Put(ctx, "graph/none", []byte("x")), "no node, no write")
}

// newMigratableABACServer returns a Server holding a standalone qrdb node a
// system encryption migration can run on: a seal-enabled, none-strategy
// barrier over an on-disk pebble store, composed with WithABACLiveReload.
func newMigratableABACServer(t *testing.T) (*Server, *qrdbnode.Node) {
	t.Helper()
	cfg := qrdbsdk.DefaultDaemonConfig()
	cfg.Raft.DataDir = t.TempDir()
	cfg.Databases = []qrdbsdk.DaemonDatabaseConfig{{Name: "default", Backend: "pebble"}}
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "none"
	cfg.Protocols.REST = false
	cfg.Protocols.GRPC = false
	cfg.Protocols.QUIC = false
	cfg.Protocols.Unix = false
	cfg.Protocols.MCP = false

	node, err := qrdbnode.NewStandaloneNode(context.Background(), cfg, nil, qrdbnode.WithABACLiveReload())
	require.NoError(t, err)
	t.Cleanup(func() { _ = node.Close() })
	require.False(t, node.IsSealed(), "a none-strategy node boots unsealed")

	s := &Server{logger: buildTestLogger(t)}
	s.setQRDBNode(node)
	return s, node
}

// The policy storage and the unseal hook are built once, at boot. A real
// system encryption migration then closes the node's storage and publishes a
// new service graph with an empty ABAC decorator. A tenant unseal after it
// must reload the stored policies through the new graph's store, so the
// stored deny is enforced again.
func TestTenantUnseal_AfterSystemEncryptionMigration_EnforcesTheStoredDeny(t *testing.T) {
	const tenant = "acme"
	ctx := context.Background()
	s, node := newMigratableABACServer(t)

	iamKV := s.tenantRoutedKVStore()
	require.IsType(t, &tenantRoutingKVStore{}, iamKV)
	storage := newIAMBackendStorage(iamKV, s.logger)
	tenants := &abacTestTenants{tenants: []*iam.Tenant{{ID: tenant}}}
	sync := newQRDBABACSync(s, tenants, storage)
	s.resyncABACOnTenantUnseal(sync.Sync)

	tenantCtx := tenantctx.WithTenant(ctx, tenant)
	require.NoError(t, storage.CreatePolicy(tenantCtx, tenant,
		abacTestPolicy("allow-kv", tenant, "alice", "kv/**", iam.EffectAllow)))
	require.NoError(t, storage.CreatePolicy(tenantCtx, tenant,
		abacTestPolicy("deny-secrets", tenant, "alice", "kv/secrets/**", iam.EffectDeny)))
	require.NoError(t, sync.Sync(ctx))

	authorize := func(resource string) error {
		return node.Client().RBAC().Authorize(qrdbsdk.ContextWithTenant(ctx, tenant),
			&qrdbtransport.Identity{Subject: "alice", TenantID: tenant}, resource, "read")
	}
	require.NoError(t, authorize("kv/orders"), "the stored allow applies before the migration")
	require.Error(t, authorize("kv/secrets/root"), "the stored deny applies before the migration")

	before := node.Services()
	require.NoError(t, node.EnableEncryption(ctx, qrdbsdk.BarrierScope{Kind: qrdbsdk.ScopeSystem}, qrdbsdk.MigrationOptions{
		TargetStrategy: qrdbsdk.StrategyPassphrase,
		Mechanism:      qrdbsdk.MechanismSwap,
		Availability:   qrdbsdk.AvailabilityQuiesced,
		Credentials:    qrdbsdk.MustCredentials([]byte("policy-store-migration-pass")),
	}))
	after := node.Services()
	require.NotNil(t, after)
	require.NotSame(t, before, after, "the migration must publish a new service graph")
	require.Error(t, authorize("kv/orders"),
		"the rebuilt node holds no policies until they are reloaded: alice has no role of her own")

	barrier := service.NewTenantBarrierService(unsealingTenantClient{}, service.WithTenantUnsealHooks(&s.tenantUnsealHooks))
	require.NoError(t, barrier.TenantBarrierUnseal(tenantAdminCtx(tenant), tenant, []byte("key")))

	assert.NoError(t, authorize("kv/orders"),
		"the unseal must reload the stored policies through the migrated node's store")
	assert.Error(t, authorize("kv/secrets/root"),
		"the stored deny must be enforced again after the unseal")

	stored, err := storage.ListPolicies(tenantCtx, tenant, iam.ListOptions{})
	require.NoError(t, err, "the storage built at boot must read the migrated store")
	assert.Len(t, stored, 2)
}
