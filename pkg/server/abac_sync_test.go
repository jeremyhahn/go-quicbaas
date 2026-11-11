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
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtenant "github.com/jeremyhahn/go-qrdb/pkg/tenant"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// abacTestStorageError is the failure the storage and tenant doubles inject.
type abacTestStorageError struct{}

func (abacTestStorageError) Error() string { return "abac test: storage failure" }

// abacTestTenants is a TenantManager double serving a fixed tenant list.
// Only ListTenants is implemented; the embedded interface is nil, so any
// other call panics and flags an unexpected dependency.
type abacTestTenants struct {
	iam.TenantManager
	tenants []*iam.Tenant
	err     error
}

func (m *abacTestTenants) ListTenants(context.Context, iam.ListOptions) ([]*iam.Tenant, error) {
	return m.tenants, m.err
}

// abacTestPolicies is a PolicyStorage double holding policies per tenant and
// recording which tenants were listed.
type abacTestPolicies struct {
	iam.PolicyStorage
	byTenant    map[string][]*iam.Policy
	err         error
	errByTenant map[string]error
	listed      []string
}

func (s *abacTestPolicies) ListPolicies(_ context.Context, tenantID string, _ iam.ListOptions) ([]*iam.Policy, error) {
	s.listed = append(s.listed, tenantID)
	if s.err != nil {
		return nil, s.err
	}
	if err := s.errByTenant[tenantID]; err != nil {
		return nil, err
	}
	return s.byTenant[tenantID], nil
}

// newABACTestServer returns a Server holding a standalone in-memory qrdb
// node. liveReload composes the node with WithABACLiveReload so it exposes
// an ABACPolicyReloader.
func newABACTestServer(t *testing.T, liveReload bool) (*Server, *qrdbnode.Node) {
	t.Helper()
	cfg := &qrdbsdk.DaemonConfig{}
	cfg.Raft.DataDir = t.TempDir()
	cfg.Databases = []qrdbsdk.DaemonDatabaseConfig{
		{Name: "default", Engine: "mvcc", Backend: "memory"},
	}
	var opts []qrdbnode.NodeOption
	if liveReload {
		opts = append(opts, qrdbnode.WithABACLiveReload())
	}
	node, err := qrdbnode.NewStandaloneNode(context.Background(), cfg, nil, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, node.Close()) })

	s := &Server{}
	s.setQRDBNode(node)
	return s, node
}

// abacTestPolicy applies effect to every action on resource for user in
// tenant.
func abacTestPolicy(name, tenant, user, resource string, effect iam.Effect) *iam.Policy {
	return &iam.Policy{
		APIVersion: "abac.qrdb.io/v1",
		Kind:       "Policy",
		Metadata:   iam.PolicyMetadata{Name: name, Tenant: tenant},
		Spec: iam.PolicySpec{
			Subjects: iam.SubjectSelector{Users: []string{user}},
			Rules: []iam.Rule{{
				Name:      string(effect) + "-" + user,
				Effect:    effect,
				Actions:   []string{"*"},
				Resources: []string{resource},
			}},
		},
	}
}

func TestQRDBABACSync_Sync_NoNode(t *testing.T) {
	storage := &abacTestPolicies{}
	sync := newQRDBABACSync(&Server{}, nil, storage)

	err := sync.Sync(context.Background())

	var unavailable *QRDBNodeClientUnavailableError
	require.ErrorAs(t, err, &unavailable)
	assert.Empty(t, storage.listed, "storage must not be read without a node")
}

func TestQRDBABACSync_Sync_NodeWithoutLiveReload(t *testing.T) {
	s, _ := newABACTestServer(t, false)
	storage := &abacTestPolicies{err: abacTestStorageError{}}
	sync := newQRDBABACSync(s, nil, storage)

	require.NoError(t, sync.Sync(context.Background()))
	assert.Empty(t, storage.listed, "storage must not be read when the node has no reloader")
}

func TestQRDBABACSync_Sync_ReloadsPoliciesIntoNode(t *testing.T) {
	s, node := newABACTestServer(t, true)
	const tenant = "acme"
	storage := &abacTestPolicies{byTenant: map[string][]*iam.Policy{
		tenant: {abacTestPolicy("grant-alice", tenant, "alice", "kv/**", iam.EffectAllow)},
	}}
	tenants := &abacTestTenants{tenants: []*iam.Tenant{{ID: tenant}}}
	sync := newQRDBABACSync(s, tenants, storage)

	ctx := qrdbsdk.ContextWithTenant(context.Background(), tenant)
	identity := &qrdbtransport.Identity{Subject: "alice", TenantID: tenant}
	require.Error(t, node.Client().RBAC().Authorize(ctx, identity, "kv/orders", "read"),
		"alice holds no role, so qrdb must deny before the allow policy is synced")

	require.NoError(t, sync.Sync(context.Background()))

	assert.NoError(t, node.Client().RBAC().Authorize(ctx, identity, "kv/orders", "read"),
		"the synced allow policy must grant alice")
}

func TestQRDBABACSync_Sync_Errors(t *testing.T) {
	invalid := abacTestPolicy("bad", "acme", "mallory", "*", iam.EffectDeny)
	invalid.Kind = "NotAPolicy"

	tests := []struct {
		name    string
		tenants *abacTestTenants
		storage *abacTestPolicies
		wantErr error
	}{
		{
			name:    "tenant listing fails",
			tenants: &abacTestTenants{err: abacTestStorageError{}},
			storage: &abacTestPolicies{},
			wantErr: abacTestStorageError{},
		},
		{
			name:    "policy listing fails",
			tenants: &abacTestTenants{},
			storage: &abacTestPolicies{err: abacTestStorageError{}},
			wantErr: abacTestStorageError{},
		},
		{
			name:    "qrdb rejects an invalid policy",
			tenants: &abacTestTenants{tenants: []*iam.Tenant{{ID: "acme"}}},
			storage: &abacTestPolicies{byTenant: map[string][]*iam.Policy{"acme": {invalid}}},
		},
	}
	s, _ := newABACTestServer(t, true)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newQRDBABACSync(s, tt.tenants, tt.storage).Sync(context.Background())
			require.Error(t, err)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}

func TestQRDBABACSync_allPolicies(t *testing.T) {
	system := &iam.Policy{Metadata: iam.PolicyMetadata{Name: "sys", Tenant: qrdbsdk.SystemTenantID}}
	acme := &iam.Policy{Metadata: iam.PolicyMetadata{Name: "acme", Tenant: "acme"}}
	globex := &iam.Policy{Metadata: iam.PolicyMetadata{Name: "globex", Tenant: "globex"}}
	byTenant := map[string][]*iam.Policy{
		qrdbsdk.SystemTenantID: {system},
		"acme":                 {acme},
		"globex":               {globex},
	}

	tests := []struct {
		name       string
		tenants    iam.TenantManager
		wantListed []string
		wantNames  []string
	}{
		{
			name:       "no tenant manager reads only the system tenant",
			wantListed: []string{qrdbsdk.SystemTenantID},
			wantNames:  []string{"sys"},
		},
		{
			name: "listed tenants are read once each, system included, plus each non-system tenant's legacy records",
			tenants: &abacTestTenants{tenants: []*iam.Tenant{
				{ID: "acme"}, {ID: "globex"}, {ID: qrdbsdk.SystemTenantID}, {ID: "acme"},
			}},
			wantListed: []string{"acme", "acme", "globex", "globex", qrdbsdk.SystemTenantID},
			wantNames:  []string{"acme", "globex", "sys"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := &abacTestPolicies{byTenant: byTenant}
			got, _, err := newQRDBABACSync(&Server{}, tt.tenants, storage).allPolicies(context.Background())
			require.NoError(t, err)

			names := make([]string, 0, len(got))
			for _, p := range got {
				names = append(names, p.Metadata.Name)
			}
			sort.Strings(names)
			sort.Strings(storage.listed)
			want := append([]string(nil), tt.wantListed...)
			sort.Strings(want)
			assert.Equal(t, tt.wantNames, names)
			assert.Equal(t, want, storage.listed)
		})
	}
}

// TestQRDBABACSync_allPolicies_TenantRoutedStorage drives allPolicies over
// storage that routes by the context's tenant, as production does. A reload
// triggered from a tenant-bound context must still read the tenant catalog
// and the system policies in the system namespace, and each tenant's
// policies in that tenant's own namespace.
func TestQRDBABACSync_allPolicies_TenantRoutedStorage(t *testing.T) {
	r := newFakeStoreResolver(kvstore.NewMemory())
	r.barrierTenant("acme")
	bs := iam.NewBackendStorage(newTenantRoutingKVStore(r))
	tm := iam.NewDefaultTenantManager(bs)

	sys := tenantctx.WithSystem(context.Background())
	_, err := tm.CreateTenant(sys, iam.CreateTenantOptions{ID: "acme", Name: "Acme"})
	require.NoError(t, err)
	require.NoError(t, bs.CreatePolicy(sys, qrdbsdk.SystemTenantID,
		abacTestPolicy("sys-deny", qrdbsdk.SystemTenantID, "alice", "kv/**", iam.EffectDeny)))

	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")
	require.NoError(t, bs.CreatePolicy(acmeCtx, "acme",
		abacTestPolicy("acme-allow", "acme", "alice", "kv/**", iam.EffectAllow)))

	for name, ctx := range map[string]context.Context{
		"acme-bound context": acmeCtx,
		"unbound context":    context.Background(),
	} {
		t.Run(name, func(t *testing.T) {
			got, _, err := newQRDBABACSync(&Server{}, tm, bs).allPolicies(ctx)
			require.NoError(t, err)

			names := make([]string, 0, len(got))
			for _, p := range got {
				names = append(names, p.Metadata.Name)
			}
			sort.Strings(names)
			assert.Equal(t, []string{"acme-allow", "sys-deny"}, names)
		})
	}
}

func TestQRDBABACSync_allPolicies_Errors(t *testing.T) {
	tests := []struct {
		name    string
		tenants iam.TenantManager
		storage *abacTestPolicies
	}{
		{name: "tenant listing fails", tenants: &abacTestTenants{err: abacTestStorageError{}}, storage: &abacTestPolicies{}},
		{name: "policy listing fails", storage: &abacTestPolicies{err: abacTestStorageError{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := newQRDBABACSync(&Server{}, tt.tenants, tt.storage).allPolicies(context.Background())
			assert.ErrorIs(t, err, abacTestStorageError{})
			assert.Nil(t, got)
		})
	}
}

// newSealedTenantABACSync returns a sync over tenants acme and globex whose
// acme listing fails with sealedErr, the storage behind it, and a func that
// authorizes alice in a tenant on the node. The system tenant grants alice
// everything, so only the sealed stand-in can deny her.
func newSealedTenantABACSync(t *testing.T, sealedErr error) (*qrdbABACSync, *abacTestPolicies, func(tenant, resource, action string) error) {
	t.Helper()
	s, node := newABACTestServer(t, true)
	storage := &abacTestPolicies{
		byTenant: map[string][]*iam.Policy{
			qrdbsdk.SystemTenantID: {abacTestPolicy("grant-alice", qrdbsdk.SystemTenantID, "alice", "**", iam.EffectAllow)},
		},
		errByTenant: map[string]error{"acme": sealedErr},
	}
	tenants := &abacTestTenants{tenants: []*iam.Tenant{{ID: "acme"}, {ID: "globex"}}}
	authorize := func(tenant, resource, action string) error {
		ctx := qrdbsdk.ContextWithTenant(context.Background(), tenant)
		identity := &qrdbtransport.Identity{Subject: "alice", TenantID: tenant}
		return node.Client().RBAC().Authorize(ctx, identity, resource, action)
	}
	return newQRDBABACSync(s, tenants, storage), storage, authorize
}

func TestQRDBABACSync_Sync_SealedTenantDoesNotStopTheReload(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"tenant store sealed", qrdbsdk.ErrTenantStoreSealed},
		{"tenant sealed", qrdbsdk.ErrTenantSealed},
		{"wrapped", fmt.Errorf("list: %w", qrdbsdk.ErrTenantStoreSealed)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sync, _, authorize := newSealedTenantABACSync(t, tt.err)

			require.NoError(t, sync.Sync(context.Background()),
				"one sealed tenant must not fail the reload")

			assert.NoError(t, authorize("globex", "kv/orders", "read"),
				"the readable tenant must get the platform-wide allow")
			err := authorize("acme", "kv/orders", "read")
			require.Error(t, err, "the sealed tenant's requests must be denied")
			require.ErrorContains(t, err, sealedTenantDenyPolicyName)
			assert.NoError(t, authorize("acme", "tenant_barrier", "unseal"),
				"the stand-in must not deny the unseal")
			assert.NoError(t, authorize("acme", "tenant_barrier", "status"),
				"the stand-in must not deny the barrier status")
		})
	}
}

func TestQRDBABACSync_Sync_UnsealedTenantLoadsItsPoliciesAgain(t *testing.T) {
	sync, storage, authorize := newSealedTenantABACSync(t, qrdbsdk.ErrTenantStoreSealed)
	require.NoError(t, sync.Sync(context.Background()))
	require.Error(t, authorize("acme", "kv/orders", "read"),
		"the sealed tenant's requests must be denied")

	delete(storage.errByTenant, "acme")
	require.NoError(t, sync.Sync(context.Background()))

	assert.NoError(t, authorize("acme", "kv/orders", "read"),
		"the stand-in must be gone once the tenant's policies can be read")
}

func TestQRDBABACSync_allPolicies_SealedSystemTenantIsAnError(t *testing.T) {
	storage := &abacTestPolicies{
		byTenant: map[string][]*iam.Policy{
			"globex": {abacTestPolicy("globex-kv", "globex", "alice", "kv/**", iam.EffectAllow)},
		},
		errByTenant: map[string]error{
			qrdbsdk.SystemTenantID: qrdbsdk.ErrTenantStoreSealed,
			"acme":                 qrdbsdk.ErrTenantStoreSealed,
		},
	}
	tenants := &abacTestTenants{tenants: []*iam.Tenant{{ID: "acme"}, {ID: "globex"}}}

	got, sealed, err := newQRDBABACSync(nil, tenants, storage).allPolicies(context.Background())

	require.ErrorIs(t, err, qrdbsdk.ErrTenantStoreSealed)
	assert.Nil(t, got)
	assert.Nil(t, sealed)
}

func TestQRDBABACSync_allPolicies_ReportsSealedTenantsSorted(t *testing.T) {
	storage := &abacTestPolicies{
		byTenant: map[string][]*iam.Policy{
			qrdbsdk.SystemTenantID: {abacTestPolicy("platform", qrdbsdk.SystemTenantID, "alice", "**", iam.EffectAllow)},
			"globex":               {abacTestPolicy("globex-kv", "globex", "alice", "kv/**", iam.EffectAllow)},
			"acme":                 {abacTestPolicy("acme-kv", "acme", "alice", "kv/**", iam.EffectAllow)},
		},
		errByTenant: map[string]error{
			"zeta": qrdbsdk.ErrTenantStoreSealed,
			"acme": qrdbsdk.ErrTenantSealed,
		},
	}
	tenants := &abacTestTenants{tenants: []*iam.Tenant{{ID: "zeta"}, {ID: "globex"}, {ID: "acme"}}}

	got, sealed, err := newQRDBABACSync(nil, tenants, storage).allPolicies(context.Background())

	require.NoError(t, err)
	names := make([]string, 0, len(got))
	for _, p := range got {
		names = append(names, p.Metadata.Name)
	}
	sort.Strings(names)
	assert.Equal(t, []string{"globex-kv", "platform"}, names)
	assert.Equal(t, []string{"acme", "zeta"}, sealed)
}

func TestServer_ABACResyncsWhenATenantIsUnsealed(t *testing.T) {
	tests := []struct {
		name    string
		syncErr error
	}{
		{"sync succeeds", nil},
		{"sync fails", errors.New("reload refused")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{}
			calls := 0
			s.resyncABACOnTenantUnseal(func(context.Context) error {
				calls++
				return tt.syncErr
			})

			barrier := service.NewTenantBarrierService(unsealingTenantClient{}, service.WithTenantUnsealHooks(&s.tenantUnsealHooks))
			require.NoError(t, barrier.TenantBarrierUnseal(tenantAdminCtx("acme"), "acme", []byte("key")),
				"a failed policy sync must not fail the unseal")

			assert.Equal(t, 1, calls)
		})
	}
}

// abacPolicyNames returns the sorted record names of policies.
func abacPolicyNames(policies []*iam.Policy) []string {
	names := make([]string, 0, len(policies))
	for _, p := range policies {
		if p.LoadError != nil {
			names = append(names, p.LoadError.Name)
			continue
		}
		names = append(names, p.Metadata.Name)
	}
	sort.Strings(names)
	return names
}

// newABACTenantRoutedStorage builds tenant-routed IAM storage with a barrier
// tenant acme, sys-deny in the system tenant and acme-allow in acme.
// notFoundAtomicStore is qrdb's atomic in-memory store with the KVStore
// contract's not-found error for an absent key (qrdb returns nil, nil).
type notFoundAtomicStore struct {
	qrdbsdk.AtomicKVStore
}

func (s *notFoundAtomicStore) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := s.AtomicKVStore.Get(ctx, key)
	if err == nil && value == nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test.get"}
	}
	return value, err
}

func newABACTenantRoutedStorage(t *testing.T) (*fakeStoreResolver, iam.TenantManager, *iam.BackendStorage) {
	t.Helper()
	r := newFakeStoreResolver(&notFoundAtomicStore{AtomicKVStore: qrdbsdk.NewMemoryKVStore()})
	r.barrierTenantStore("acme", qrdbsdk.NewMemoryKVStore())
	bs := iam.NewBackendStorage(newTenantRoutingKVStore(r))
	tm := iam.NewDefaultTenantManager(bs)

	sys := tenantctx.WithSystem(context.Background())
	_, err := tm.CreateTenant(sys, iam.CreateTenantOptions{ID: "acme", Name: "Acme"})
	require.NoError(t, err)
	require.NoError(t, bs.CreatePolicy(sys, qrdbsdk.SystemTenantID,
		abacTestPolicy("sys-deny", qrdbsdk.SystemTenantID, "alice", "kv/**", iam.EffectDeny)))
	require.NoError(t, bs.CreatePolicy(tenantctx.WithTenant(context.Background(), "acme"), "acme",
		abacTestPolicy("acme-allow", "acme", "alice", "kv/**", iam.EffectAllow)))
	return r, tm, bs
}

func TestQRDBABACSync_allPolicies_SkipsASealedTenant(t *testing.T) {
	r, tm, bs := newABACTenantRoutedStorage(t)
	r.sealTenant("acme")

	got, _, err := newQRDBABACSync(&Server{}, tm, bs).allPolicies(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"sys-deny"}, abacPolicyNames(got))
}

func TestQRDBABACSync_allPolicies_SealedSystemTenantAborts(t *testing.T) {
	got, _, err := newQRDBABACSync(&Server{}, nil, &abacTestPolicies{err: qrdbtenant.ErrTenantSealed}).allPolicies(context.Background())
	assert.True(t, errors.Is(err, qrdbtenant.ErrTenantSealed), "err = %v", err)
	assert.Nil(t, got)
}

// fireABACTestUnseal unseals acme through a barrier service that fires hooks.
func fireABACTestUnseal(t *testing.T, hooks *service.TenantUnsealHooks) {
	t.Helper()
	barrier := service.NewTenantBarrierService(unsealingTenantClient{}, service.WithTenantUnsealHooks(hooks))
	require.NoError(t, barrier.TenantBarrierUnseal(tenantAdminCtx("acme"), "acme", []byte("key")))
}

func TestQRDBABACSync_resyncOnUnseal(t *testing.T) {
	_, tm, bs := newABACTenantRoutedStorage(t)
	hooks := &service.TenantUnsealHooks{}

	calls := 0
	var syncCtx context.Context
	newQRDBABACSync(&Server{}, tm, bs).resyncOnUnseal(hooks, func(ctx context.Context, _ string, prepare func(context.Context)) error {
		if prepare != nil {
			prepare(ctx)
		}
		calls++
		syncCtx = ctx
		return nil
	})
	fireABACTestUnseal(t, hooks)

	assert.Equal(t, 1, calls)
	assert.NotNil(t, syncCtx)
}

// resealingTenantClient unseals every tenant and records the tenant whose
// barrier is sealed again.
type sealRecordingTenantClient struct {
	unsealingTenantClient
	sealed []string
}

// TenantBarrierSeal records the seal and refuses it as a Shamir barrier does
// without a quorum.
func (c *sealRecordingTenantClient) TenantBarrierSeal(_ context.Context, req *qrdbtransport.TenantBarrierSealRequest) error {
	c.sealed = append(c.sealed, req.TenantID)
	return errors.New("shamir barrier requires quorum")
}

// TestQRDBABACSync_resyncOnUnseal_TenantStaysClosedUntilReload drives an
// unseal whose ABAC reload blocks: while it is blocked the tenant's routed
// stores refuse it as sealed, and they open only when the reload succeeds. A
// failed reload keeps the tenant closed and never seals the barrier.
func TestQRDBABACSync_resyncOnUnseal_TenantStaysClosedUntilReload(t *testing.T) {
	reloadErr := errors.New("catalog read failed")
	for name, result := range map[string]error{"reload succeeds": nil, "reload fails": reloadErr} {
		t.Run(name, func(t *testing.T) {
			_, tm, bs := newABACTenantRoutedStorage(t)
			s := &Server{}
			entered := make(chan struct{}, 1)
			proceed := make(chan error)
			newQRDBABACSync(s, tm, bs).resyncOnUnseal(&s.tenantUnsealHooks, func(ctx context.Context, _ string, prepare func(context.Context)) error {
				if prepare != nil {
					prepare(ctx)
				}
				entered <- struct{}{}
				return <-proceed
			})
			client := &sealRecordingTenantClient{}
			barrier := service.NewTenantBarrierService(client, service.WithTenantUnsealHooks(&s.tenantUnsealHooks))
			resolver := s.admittedStoreResolver(fakeQRDBResolver{store: fakeQRDBStore{}})

			done := make(chan error, 1)
			go func() { done <- barrier.TenantBarrierUnseal(tenantAdminCtx("acme"), "acme", []byte("key")) }()
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("the unseal never reached its ABAC reload")
			}
			_, err := resolver.Resolve(context.Background(), "acme")
			require.ErrorIs(t, err, qrdbsdk.ErrTenantStoreSealed, "a tenant must stay closed while its reload runs")

			proceed <- result
			err = <-done
			_, resolveErr := resolver.Resolve(context.Background(), "acme")
			if result == nil {
				require.NoError(t, err)
				require.NoError(t, resolveErr)
			} else {
				require.ErrorIs(t, err, reloadErr)
				assert.Contains(t, err.Error(), "stays closed")
				assert.ErrorIs(t, resolveErr, qrdbsdk.ErrTenantStoreSealed)
			}
			assert.Empty(t, client.sealed, "nothing may seal the barrier as a rollback")
		})
	}
}

// seedLegacyABACPolicy stores p for tenantID the way an earlier release wrote
// a system admin's tenant policy: with the system context, so it lands in
// the shared store under the tenant's key.
func seedLegacyABACPolicy(t *testing.T, bs *iam.BackendStorage, tenantID string, p *iam.Policy) {
	t.Helper()
	require.NoError(t, bs.CreatePolicy(tenantctx.WithSystem(context.Background()), tenantID, p))
}

// abacPolicyEffect returns the effect of the first rule of the policy named
// name in policies.
func abacPolicyEffect(t *testing.T, policies []*iam.Policy, name string) iam.Effect {
	t.Helper()
	for _, p := range policies {
		if p.LoadError == nil && p.Metadata.Name == name {
			require.NotEmpty(t, p.Spec.Rules)
			return p.Spec.Rules[0].Effect
		}
	}
	t.Fatalf("policy %q not listed", name)
	return ""
}

func TestQRDBABACSync_allPolicies_ReadsLegacyTenantPolicies(t *testing.T) {
	r, tm, bs := newABACTenantRoutedStorage(t)
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")
	seedLegacyABACPolicy(t, bs, "acme", abacTestPolicy("legacy-deny", "acme", "alice", "kv/**", iam.EffectDeny))
	seedLegacyABACPolicy(t, bs, "acme", abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectDeny))
	require.NoError(t, bs.CreatePolicy(acmeCtx, "acme", abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectAllow)))

	a := newQRDBABACSync(&Server{}, tm, bs)
	got, _, err := a.allPolicies(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"acme-allow", "dup", "legacy-deny", "sys-deny"}, abacPolicyNames(got))
	dup := abacPolicyEntry(t, got, "dup")
	require.NotNil(t, dup.LoadError, "a differing legacy copy fails closed")
	assert.Equal(t, "acme", dup.LoadError.TenantID)

	r.sealTenant("acme")
	got, _, err = a.allPolicies(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"dup", "legacy-deny", "sys-deny"}, abacPolicyNames(got))
	assert.Equal(t, iam.EffectDeny, abacPolicyEffect(t, got, "dup"))
}

// abacPolicyEntry returns the one listed entry named name, flagged or not.
func abacPolicyEntry(t *testing.T, policies []*iam.Policy, name string) *iam.Policy {
	t.Helper()
	var found *iam.Policy
	for _, p := range policies {
		if policyRecordName(p) == name {
			require.Nil(t, found, "policy %q listed twice", name)
			found = p
		}
	}
	require.NotNil(t, found, "policy %q not listed", name)
	return found
}

func TestQRDBABACSync_allPolicies_IdenticalLegacyCopyMerges(t *testing.T) {
	_, tm, bs := newABACTenantRoutedStorage(t)
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")
	seedLegacyABACPolicy(t, bs, "acme", abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectDeny))
	require.NoError(t, bs.CreatePolicy(acmeCtx, "acme", abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectDeny)))

	got, _, err := newQRDBABACSync(&Server{}, tm, bs).allPolicies(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"acme-allow", "dup", "sys-deny"}, abacPolicyNames(got))
	assert.Nil(t, abacPolicyEntry(t, got, "dup").LoadError)
	assert.Equal(t, iam.EffectDeny, abacPolicyEffect(t, got, "dup"))
}

func TestQRDBABACSync_allPolicies_FlaggedLegacyCopyFailsClosed(t *testing.T) {
	r, tm, bs := newABACTenantRoutedStorage(t)
	require.NoError(t, r.shared.Put(context.Background(),
		iam.KeyPrefixTenant+"acme"+iam.KeyPrefixPolicy+"dup", []byte("{not json")))
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")
	require.NoError(t, bs.CreatePolicy(acmeCtx, "acme", abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectAllow)))

	got, _, err := newQRDBABACSync(&Server{}, tm, bs).allPolicies(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"acme-allow", "dup", "sys-deny"}, abacPolicyNames(got))
	assert.NotNil(t, abacPolicyEntry(t, got, "dup").LoadError, "a flagged legacy copy fails closed")
}

func TestQRDBABACSync_migrateLegacyPolicies(t *testing.T) {
	sysCtx := tenantctx.WithSystem(context.Background())
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")
	list := func(t *testing.T, bs *iam.BackendStorage, ctx context.Context, tenantID string) []*iam.Policy {
		t.Helper()
		got, err := bs.ListPolicies(ctx, tenantID, iam.ListOptions{})
		require.NoError(t, err)
		return got
	}

	t.Run("barrier tenant moved", func(t *testing.T) {
		_, tm, bs := newABACTenantRoutedStorage(t)
		seedLegacyABACPolicy(t, bs, "acme", abacTestPolicy("legacy-deny", "acme", "alice", "kv/**", iam.EffectDeny))

		newQRDBABACSync(&Server{}, tm, bs).migrateLegacyPolicies(context.Background())

		assert.Equal(t, []string{"acme-allow", "legacy-deny"}, abacPolicyNames(list(t, bs, acmeCtx, "acme")))
		assert.Empty(t, list(t, bs, sysCtx, "acme"))
	})

	t.Run("tenant copy kept", func(t *testing.T) {
		_, tm, bs := newABACTenantRoutedStorage(t)
		require.NoError(t, bs.CreatePolicy(acmeCtx, "acme", abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectAllow)))
		seedLegacyABACPolicy(t, bs, "acme", abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectDeny))

		newQRDBABACSync(&Server{}, tm, bs).migrateLegacyPolicies(context.Background())

		assert.Equal(t, iam.EffectAllow, abacPolicyEffect(t, list(t, bs, acmeCtx, "acme"), "dup"))
		assert.Equal(t, iam.EffectDeny, abacPolicyEffect(t, list(t, bs, sysCtx, "acme"), "dup"))
	})

	t.Run("prefix tenant untouched", func(t *testing.T) {
		_, tm, bs := newABACTenantRoutedStorage(t)
		_, err := tm.CreateTenant(sysCtx, iam.CreateTenantOptions{ID: "beta", Name: "Beta"})
		require.NoError(t, err)
		require.NoError(t, bs.CreatePolicy(tenantctx.WithTenant(context.Background(), "beta"), "beta",
			abacTestPolicy("beta-allow", "beta", "alice", "kv/**", iam.EffectAllow)))

		newQRDBABACSync(&Server{}, tm, bs).migrateLegacyPolicies(context.Background())

		assert.Equal(t, []string{"beta-allow"}, abacPolicyNames(list(t, bs, sysCtx, "beta")))
	})

	t.Run("undecodable record left", func(t *testing.T) {
		r, tm, bs := newABACTenantRoutedStorage(t)
		require.NoError(t, r.shared.Put(context.Background(),
			iam.KeyPrefixTenant+"acme"+iam.KeyPrefixPolicy+"bad", []byte("{not json")))

		a := newQRDBABACSync(&Server{}, tm, bs)
		a.migrateLegacyPolicies(context.Background())

		legacy := list(t, bs, sysCtx, "acme")
		require.Len(t, legacy, 1)
		require.NotNil(t, legacy[0].LoadError)
		assert.Equal(t, "bad", legacy[0].LoadError.Name)

		got, _, err := a.allPolicies(context.Background())
		require.NoError(t, err)
		flagged := false
		for _, p := range got {
			if p.LoadError != nil && p.LoadError.Name == "bad" {
				flagged = true
			}
		}
		assert.True(t, flagged, "the undecodable legacy record is listed with LoadError set")
	})

	t.Run("sealed, then unsealed", func(t *testing.T) {
		r, tm, bs := newABACTenantRoutedStorage(t)
		seedLegacyABACPolicy(t, bs, "acme", abacTestPolicy("legacy-deny", "acme", "alice", "kv/**", iam.EffectDeny))
		r.sealTenant("acme")

		a := newQRDBABACSync(&Server{}, tm, bs)
		a.migrateLegacyPolicies(context.Background())
		assert.Equal(t, []string{"legacy-deny"}, abacPolicyNames(list(t, bs, sysCtx, "acme")))

		delete(r.sealed, "acme")
		hooks := &service.TenantUnsealHooks{}
		calls := 0
		a.resyncOnUnseal(hooks, func(ctx context.Context, _ string, prepare func(context.Context)) error {
			if prepare != nil {
				prepare(ctx)
			}
			calls++
			assert.Equal(t, []string{"acme-allow", "legacy-deny"}, abacPolicyNames(list(t, bs, acmeCtx, "acme")))
			assert.Empty(t, list(t, bs, sysCtx, "acme"))
			return nil
		})
		fireABACTestUnseal(t, hooks)

		assert.Equal(t, 1, calls)
	})
}

// policyGetFailingKVStore is a KVStore whose Get of failKey fails.
type policyGetFailingKVStore struct {
	interfaces.KVStore
	failKey string
}

func (s *policyGetFailingKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	if key == s.failKey {
		return nil, errors.New("transient read failure")
	}
	return s.KVStore.Get(ctx, key)
}

func TestQRDBABACSync_MigrationKeepsBothRecordsOnReadFailure(t *testing.T) {
	ctx := context.Background()
	r, tm, bs := newABACTenantRoutedStorage(t)
	require.NoError(t, bs.CreatePolicy(tenantctx.WithTenant(ctx, "acme"), "acme", abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectDeny)))
	seedLegacyABACPolicy(t, bs, "acme", abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectAllow))
	key := iam.KeyPrefixTenant + "acme" + iam.KeyPrefixPolicy + "dup"
	acme := r.stores["acme"]
	tenantBefore, err := acme.Get(ctx, key)
	require.NoError(t, err)
	legacyBefore, err := r.shared.Get(ctx, key)
	require.NoError(t, err)
	r.stores["acme"] = &policyGetFailingKVStore{KVStore: acme, failKey: key}

	newQRDBABACSync(&Server{}, tm, bs).migrateLegacyPolicies(ctx)

	tenantAfter, err := acme.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, tenantBefore, tenantAfter, "the tenant's deny must not be replaced")
	legacyAfter, err := r.shared.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, legacyBefore, legacyAfter, "the legacy record must not be deleted")
}

// admittedFakeResolver resolves through the real admittingStoreResolver's
// admission check, then serves the fake resolver's per-tenant store.
type admittedFakeResolver struct {
	admit  qrdbsdk.TenantStoreResolver
	stores *fakeStoreResolver
}

func (r admittedFakeResolver) Resolve(ctx context.Context, tenantID string) (interfaces.KVStore, error) {
	if _, err := r.admit.Resolve(ctx, tenantID); err != nil {
		return nil, err
	}
	return r.stores.Resolve(ctx, tenantID)
}

func TestQRDBABACSync_resyncOnUnseal_GateReadsThroughAdmittedStore(t *testing.T) {
	s, _ := newABACTestServer(t, true)
	r := newFakeStoreResolver(qrdbsdk.NewMemoryKVStore())
	r.barrierTenantStore("acme", qrdbsdk.NewMemoryKVStore())
	resolver := admittedFakeResolver{admit: s.admittedStoreResolver(fakeQRDBResolver{store: fakeQRDBStore{}}), stores: r}
	bs := iam.NewBackendStorage(newTenantRoutingKVStore(resolver))
	tm := iam.NewDefaultTenantManager(bs)

	sysCtx := tenantctx.WithSystem(context.Background())
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")
	_, err := tm.CreateTenant(sysCtx, iam.CreateTenantOptions{ID: "acme", Name: "Acme"})
	require.NoError(t, err)
	require.NoError(t, bs.CreatePolicy(sysCtx, qrdbsdk.SystemTenantID,
		abacTestPolicy("sys-deny", qrdbsdk.SystemTenantID, "alice", "kv/**", iam.EffectDeny)))
	require.NoError(t, bs.CreatePolicy(acmeCtx, "acme",
		abacTestPolicy("acme-allow", "acme", "alice", "kv/**", iam.EffectAllow)))
	seedLegacyABACPolicy(t, bs, "acme", abacTestPolicy("legacy-deny", "acme", "alice", "kv/**", iam.EffectDeny))

	abacSync := newQRDBABACSync(s, tm, bs)
	pe := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: bs, ABACSync: abacSync})
	abacSync.resyncOnUnseal(&s.tenantUnsealHooks, pe.SyncABACAtUnseal)

	fireABACTestUnseal(t, &s.tenantUnsealHooks)
	require.NoError(t, s.tenantUnsealHooks.Admit("acme"))
	moved, err := bs.ListPolicies(acmeCtx, "acme", iam.ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"acme-allow", "legacy-deny"}, abacPolicyNames(moved))
	legacy, err := bs.ListPolicies(sysCtx, "acme", iam.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, legacy)

	// Outside the gate, a read of the tenant being admitted stays refused.
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	s.tenantUnsealHooks.RegisterGate(service.TenantUnsealGateFunc(func(ctx context.Context, tenantID string) error {
		entered <- ctx
		<-release
		return nil
	}))
	done := make(chan error, 1)
	go func() {
		barrier := service.NewTenantBarrierService(unsealingTenantClient{}, service.WithTenantUnsealHooks(&s.tenantUnsealHooks))
		done <- barrier.TenantBarrierUnseal(tenantAdminCtx("acme"), "acme", []byte("key"))
	}()

	var gateCtx context.Context
	select {
	case gateCtx = <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("the second unseal never reached its gate")
	}
	outside := s.admittedStoreResolver(fakeQRDBResolver{store: fakeQRDBStore{}})
	_, err = outside.Resolve(context.Background(), "acme")
	assert.ErrorIs(t, err, qrdbsdk.ErrTenantStoreSealed)
	_, err = outside.Resolve(gateCtx, "acme")
	assert.NoError(t, err)
	close(release)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("the second unseal never returned")
	}
	require.NoError(t, s.tenantUnsealHooks.Admit("acme"))
}

func TestQRDBABACSync_allPolicies_KeepsAHeldTenantsPolicies(t *testing.T) {
	s, _ := newABACTestServer(t, true)
	r := newFakeStoreResolver(qrdbsdk.NewMemoryKVStore())
	r.barrierTenantStore("acme", qrdbsdk.NewMemoryKVStore())
	resolver := admittedFakeResolver{admit: s.admittedStoreResolver(fakeQRDBResolver{store: fakeQRDBStore{}}), stores: r}
	bs := iam.NewBackendStorage(newTenantRoutingKVStore(resolver))
	tm := iam.NewDefaultTenantManager(bs)

	sysCtx := tenantctx.WithSystem(context.Background())
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")
	for _, id := range []string{"acme", "beta"} {
		_, err := tm.CreateTenant(sysCtx, iam.CreateTenantOptions{ID: id, Name: id})
		require.NoError(t, err)
	}
	require.NoError(t, bs.CreatePolicy(acmeCtx, "acme",
		abacTestPolicy("acme-deny", "acme", "alice", "kv/**", iam.EffectDeny)))

	// Hold acme: its barrier opens but a failing gate keeps it from admission.
	gateErr := errors.New("policy reload failed")
	s.tenantUnsealHooks.RegisterGate(service.TenantUnsealGateFunc(func(context.Context, string) error {
		return gateErr
	}))
	barrier := service.NewTenantBarrierService(unsealingTenantClient{}, service.WithTenantUnsealHooks(&s.tenantUnsealHooks))
	require.ErrorIs(t, barrier.TenantBarrierUnseal(tenantAdminCtx("acme"), "acme", []byte("key")), gateErr)

	_, err := s.admittedStoreResolver(fakeQRDBResolver{store: fakeQRDBStore{}}).Resolve(acmeCtx, "acme")
	assert.ErrorIs(t, err, qrdbsdk.ErrTenantStoreSealed, "a request to a held tenant must stay refused")

	all, _, err := newQRDBABACSync(s, tm, bs).allPolicies(tenantctx.WithTenant(context.Background(), "beta"))
	require.NoError(t, err)
	assert.Contains(t, abacPolicyNames(all), "acme-deny", "a reload for beta must keep held acme's deny rules")
}

func TestLegacyCleanupPolicyStorage_DeleteRemovesTheLegacyCopy(t *testing.T) {
	_, tm, bs := newABACTenantRoutedStorage(t)
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")
	seedLegacyABACPolicy(t, bs, "acme", abacTestPolicy("acme-allow", "acme", "alice", "kv/**", iam.EffectDeny))

	require.NoError(t, newLegacyCleanupPolicyStorage(bs).DeletePolicy(acmeCtx, "acme", "acme-allow"))

	_, err := bs.GetPolicy(acmeCtx, "acme", "acme-allow")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "tenant copy: err = %v", err)
	_, err = bs.GetPolicy(tenantctx.WithSystem(context.Background()), "acme", "acme-allow")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "legacy copy: err = %v", err)
	got, _, err := newQRDBABACSync(&Server{}, tm, bs).allPolicies(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"sys-deny"}, abacPolicyNames(got))
}

func TestLegacyCleanupPolicyStorage_ListsAndGetsAFlaggedLegacyRecord(t *testing.T) {
	r, _, bs := newABACTenantRoutedStorage(t)
	require.NoError(t, r.shared.Put(context.Background(),
		iam.KeyPrefixTenant+"acme"+iam.KeyPrefixPolicy+"bad", []byte("{not json")))
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")
	storage := newLegacyCleanupPolicyStorage(bs)

	listed, err := storage.ListPolicies(acmeCtx, "acme", iam.ListOptions{})
	require.NoError(t, err)
	flagged := false
	for _, p := range listed {
		if p.LoadError != nil && p.LoadError.Name == "bad" {
			flagged = true
		}
	}
	assert.True(t, flagged, "the flagged legacy record is listed")

	_, err = storage.GetPolicy(acmeCtx, "acme", "bad")
	var de *iam.StoredPolicyDecodeError
	assert.True(t, errors.As(err, &de), "a flagged legacy record reads as its decode error, got %v", err)
}

func TestLegacyCleanupPolicyStorage_EngineDeletesAFlaggedLegacyRecord(t *testing.T) {
	r, _, bs := newABACTenantRoutedStorage(t)
	require.NoError(t, r.shared.Put(context.Background(),
		iam.KeyPrefixTenant+"acme"+iam.KeyPrefixPolicy+"bad", []byte("{not json")))
	pe := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: newLegacyCleanupPolicyStorage(bs)})

	require.NoError(t, pe.DeletePolicy(tenantctx.WithTenant(context.Background(), "acme"), "acme", "bad"))

	_, err := bs.GetPolicy(tenantctx.WithSystem(context.Background()), "acme", "bad")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "legacy copy: err = %v", err)
}

func TestLegacyCleanupPolicyStorage_UpdateMovesALegacyRecord(t *testing.T) {
	_, _, bs := newABACTenantRoutedStorage(t)
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")
	seedLegacyABACPolicy(t, bs, "acme", abacTestPolicy("legacy-deny", "acme", "alice", "kv/**", iam.EffectDeny))

	require.NoError(t, newLegacyCleanupPolicyStorage(bs).UpdatePolicy(acmeCtx, "acme",
		abacTestPolicy("legacy-deny", "acme", "alice", "kv/**", iam.EffectAllow)))

	got, err := bs.GetPolicy(acmeCtx, "acme", "legacy-deny")
	require.NoError(t, err)
	assert.Equal(t, "legacy-deny", got.Metadata.Name)
	_, err = bs.GetPolicy(tenantctx.WithSystem(context.Background()), "acme", "legacy-deny")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "legacy copy: err = %v", err)
}

func TestLegacyCleanupPolicyStorage_UpdateResolvesAConflict(t *testing.T) {
	_, tm, bs := newABACTenantRoutedStorage(t)
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")
	seedLegacyABACPolicy(t, bs, "acme", abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectDeny))
	require.NoError(t, bs.CreatePolicy(acmeCtx, "acme", abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectAllow)))

	require.NoError(t, newLegacyCleanupPolicyStorage(bs).UpdatePolicy(acmeCtx, "acme",
		abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectAllow)))

	_, err := bs.GetPolicy(tenantctx.WithSystem(context.Background()), "acme", "dup")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "legacy copy: err = %v", err)
	got, _, err := newQRDBABACSync(&Server{}, tm, bs).allPolicies(context.Background())
	require.NoError(t, err)
	assert.Nil(t, abacPolicyEntry(t, got, "dup").LoadError)
	assert.Equal(t, iam.EffectAllow, abacPolicyEffect(t, got, "dup"))
}

func TestLegacyCleanupPolicyStorage_ListRejectsNegativeOffset(t *testing.T) {
	_, _, bs := newABACTenantRoutedStorage(t)
	storage := newLegacyCleanupPolicyStorage(bs)
	tests := []struct {
		name   string
		tenant string
	}{
		{"non-empty tenant", "acme"},
		{"empty tenant", "empty"},
		{"system tenant", qrdbsdk.SystemTenantID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tenantctx.WithTenant(context.Background(), tt.tenant)
			if tt.tenant == qrdbsdk.SystemTenantID {
				ctx = tenantctx.WithSystem(context.Background())
			}
			var (
				got []*iam.Policy
				err error
			)
			require.NotPanics(t, func() {
				got, err = storage.ListPolicies(ctx, tt.tenant, iam.ListOptions{Offset: -1})
			})
			require.Error(t, err)
			assert.True(t, dberrors.Is(err, dberrors.ErrInvalidInput), "err = %v", err)
			assert.Nil(t, got)
		})
	}
}

func TestPaginatePolicies_NegativeBoundsNeverPanic(t *testing.T) {
	policies := []*iam.Policy{
		abacTestPolicy("a", "acme", "alice", "kv/**", iam.EffectAllow),
		abacTestPolicy("b", "acme", "alice", "kv/**", iam.EffectAllow),
	}
	tests := []struct {
		name string
		in   []*iam.Policy
		opts iam.ListOptions
		want []string
	}{
		{"negative offset", policies, iam.ListOptions{Offset: -1}, []string{"a", "b"}},
		{"negative limit", policies, iam.ListOptions{Limit: -1}, []string{"a", "b"}},
		{"both negative", policies, iam.ListOptions{Offset: -5, Limit: -5}, []string{"a", "b"}},
		{"negative offset with limit", policies, iam.ListOptions{Offset: -1, Limit: 1}, []string{"a"}},
		{"negative offset on empty", nil, iam.ListOptions{Offset: -1}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []*iam.Policy
			require.NotPanics(t, func() { got = paginatePolicies(tt.in, tt.opts) })
			assert.Equal(t, tt.want, abacPolicyNames(got))
		})
	}
}

// rejectingABACSync is an ABACSync whose reload always fails, so every
// engine write through it is rejected.
type rejectingABACSync struct{}

func (rejectingABACSync) Sync(context.Context) error { return errors.New("abac reload rejected") }

// abacPolicyRecords returns the raw tenant and legacy records of acme's policy name.
func abacPolicyRecords(t *testing.T, bs *iam.BackendStorage, name string) (tenant, legacy []byte) {
	t.Helper()
	tenant, err := bs.GetPolicyRecord(tenantctx.WithTenant(context.Background(), "acme"), "acme", name)
	require.NoError(t, err)
	legacy, err = bs.GetPolicyRecord(tenantctx.WithSystem(context.Background()), "acme", name)
	require.NoError(t, err)
	return tenant, legacy
}

// seedABACConflict stores a tenant allow and a conflicting legacy deny "dup" for acme.
func seedABACConflict(t *testing.T, bs *iam.BackendStorage) {
	t.Helper()
	seedLegacyABACPolicy(t, bs, "acme", abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectDeny))
	require.NoError(t, bs.CreatePolicy(tenantctx.WithTenant(context.Background(), "acme"), "acme",
		abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectAllow)))
}

func TestLegacyCleanupPolicyStorage_RejectedUpdateKeepsBothCopies(t *testing.T) {
	_, tm, bs := newABACTenantRoutedStorage(t)
	seedABACConflict(t, bs)
	tenantBefore, legacyBefore := abacPolicyRecords(t, bs, "dup")
	pe := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: newLegacyCleanupPolicyStorage(bs), ABACSync: rejectingABACSync{}})

	err := pe.UpdatePolicy(tenantctx.WithTenant(context.Background(), "acme"), "acme",
		abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectAllow))
	require.Error(t, err)

	tenantAfter, legacyAfter := abacPolicyRecords(t, bs, "dup")
	assert.Equal(t, tenantBefore, tenantAfter)
	assert.Equal(t, legacyBefore, legacyAfter)
	got, _, err := newQRDBABACSync(&Server{}, tm, bs).allPolicies(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, abacPolicyEntry(t, got, "dup").LoadError, "the conflict stays flagged")
}

func TestLegacyCleanupPolicyStorage_RejectedDeleteKeepsBothCopies(t *testing.T) {
	_, tm, bs := newABACTenantRoutedStorage(t)
	seedABACConflict(t, bs)
	tenantBefore, legacyBefore := abacPolicyRecords(t, bs, "dup")
	pe := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: newLegacyCleanupPolicyStorage(bs), ABACSync: rejectingABACSync{}})

	require.Error(t, pe.DeletePolicy(tenantctx.WithTenant(context.Background(), "acme"), "acme", "dup"))

	tenantAfter, legacyAfter := abacPolicyRecords(t, bs, "dup")
	assert.Equal(t, tenantBefore, tenantAfter)
	assert.Equal(t, legacyBefore, legacyAfter)
	got, _, err := newQRDBABACSync(&Server{}, tm, bs).allPolicies(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, abacPolicyEntry(t, got, "dup").LoadError, "the conflict stays flagged")
}

func TestLegacyCleanupPolicyStorage_SnapshotRestoresAFlaggedLegacyRecord(t *testing.T) {
	r, _, bs := newABACTenantRoutedStorage(t)
	key := iam.KeyPrefixTenant + "acme" + iam.KeyPrefixPolicy + "bad"
	require.NoError(t, r.shared.Put(context.Background(), key, []byte("{not json")))
	storage := newLegacyCleanupPolicyStorage(bs)
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")

	restore, err := storage.(iam.PolicyWriteSnapshotter).SnapshotPolicy(acmeCtx, "acme", "bad", nil)
	require.NoError(t, err)
	require.NoError(t, storage.DeletePolicy(acmeCtx, "acme", "bad"))
	_, err = r.shared.Get(context.Background(), key)
	require.True(t, dberrors.Is(err, dberrors.ErrNotFound), "legacy record deleted: err = %v", err)

	require.NoError(t, restore(acmeCtx))
	got, err := r.shared.Get(context.Background(), key)
	require.NoError(t, err)
	assert.Equal(t, []byte("{not json"), got)
	_, err = bs.GetPolicyRecord(acmeCtx, "acme", "bad")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "no tenant copy appears: err = %v", err)

	pe := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: storage, ABACSync: rejectingABACSync{}})
	require.Error(t, pe.DeletePolicy(acmeCtx, "acme", "bad"))
	got, err = r.shared.Get(context.Background(), key)
	require.NoError(t, err)
	assert.Equal(t, []byte("{not json"), got, "a rejected delete keeps the flagged record")
}

func TestLegacyCleanupPolicyStorage_CreateRefusedOverMalformedLegacyRecord(t *testing.T) {
	r, _, bs := newABACTenantRoutedStorage(t)
	key := iam.KeyPrefixTenant + "acme" + iam.KeyPrefixPolicy + "dup"
	require.NoError(t, r.shared.Put(context.Background(), key, []byte("{not json")))
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")

	err := newLegacyCleanupPolicyStorage(bs).CreatePolicy(acmeCtx, "acme",
		abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectAllow))
	var exists *iam.PolicyExistsError
	require.ErrorAs(t, err, &exists)

	_, err = bs.GetPolicyRecord(acmeCtx, "acme", "dup")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "no tenant copy appears: err = %v", err)
	got, err := r.shared.Get(context.Background(), key)
	require.NoError(t, err)
	assert.Equal(t, []byte("{not json"), got)
}

func TestLegacyCleanupPolicyStorage_CreateRefusedOverLegacyRecord(t *testing.T) {
	_, _, bs := newABACTenantRoutedStorage(t)
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")
	sysCtx := tenantctx.WithSystem(context.Background())
	seedLegacyABACPolicy(t, bs, "acme", abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectDeny))
	before, err := bs.GetPolicyRecord(sysCtx, "acme", "dup")
	require.NoError(t, err)

	err = newLegacyCleanupPolicyStorage(bs).CreatePolicy(acmeCtx, "acme",
		abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectAllow))
	var exists *iam.PolicyExistsError
	require.ErrorAs(t, err, &exists)

	_, err = bs.GetPolicyRecord(acmeCtx, "acme", "dup")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "no tenant copy appears: err = %v", err)
	after, err := bs.GetPolicyRecord(sysCtx, "acme", "dup")
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestLegacyCleanupPolicyStorage_RejectedCreateKeepsLegacyRecord(t *testing.T) {
	r, _, bs := newABACTenantRoutedStorage(t)
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")
	sysCtx := tenantctx.WithSystem(context.Background())
	pe := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: newLegacyCleanupPolicyStorage(bs), ABACSync: rejectingABACSync{}})

	err := pe.CreatePolicy(acmeCtx, "acme", abacTestPolicy("fresh", "acme", "alice", "kv/**", iam.EffectAllow))
	require.EqualError(t, err, "abac reload rejected")
	_, err = bs.GetPolicyRecord(acmeCtx, "acme", "fresh")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "tenant copy: err = %v", err)
	_, err = bs.GetPolicyRecord(sysCtx, "acme", "fresh")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "legacy copy: err = %v", err)

	key := iam.KeyPrefixTenant + "acme" + iam.KeyPrefixPolicy + "dup"
	require.NoError(t, r.shared.Put(context.Background(), key, []byte("{not json")))
	err = pe.CreatePolicy(acmeCtx, "acme", abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectAllow))
	var exists *iam.PolicyExistsError
	require.ErrorAs(t, err, &exists, "refused before any sync")
	got, err := r.shared.Get(context.Background(), key)
	require.NoError(t, err)
	assert.Equal(t, []byte("{not json"), got)
	_, err = bs.GetPolicyRecord(acmeCtx, "acme", "dup")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "no tenant copy appears: err = %v", err)
}

func TestLegacyCleanupPolicyStorage_UpdateRollsBackWhenLegacyDeleteFails(t *testing.T) {
	_, _, bs := newABACTenantRoutedStorage(t)
	seedABACConflict(t, bs)
	tenantBefore, _ := abacPolicyRecords(t, bs, "dup")
	storage := newLegacyCleanupPolicyStorage(&rawDeleteFailingStorage{BackendStorage: bs})

	err := storage.UpdatePolicy(tenantctx.WithTenant(context.Background(), "acme"), "acme",
		abacTestPolicy("dup", "acme", "bob", "kv/**", iam.EffectAllow))
	require.Error(t, err)
	tenantAfter, _ := abacPolicyRecords(t, bs, "dup")
	assert.Equal(t, tenantBefore, tenantAfter)
}

// rawDeleteFailingStorage keeps BackendStorage's raw record access and fails
// every delete; an update's only delete is of the legacy copy.
type rawDeleteFailingStorage struct {
	*iam.BackendStorage
}

func (s *rawDeleteFailingStorage) DeletePolicy(context.Context, string, string) error {
	return errors.New("legacy delete failed")
}

func TestLegacyCleanupPolicyStorage_ListPaginatesTheMergedSet(t *testing.T) {
	_, _, bs := newABACTenantRoutedStorage(t)
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")
	require.NoError(t, bs.DeletePolicy(acmeCtx, "acme", "acme-allow"))
	require.NoError(t, bs.CreatePolicy(acmeCtx, "acme", abacTestPolicy("a", "acme", "alice", "kv/**", iam.EffectAllow)))
	require.NoError(t, bs.CreatePolicy(acmeCtx, "acme", abacTestPolicy("c", "acme", "alice", "kv/**", iam.EffectAllow)))
	seedLegacyABACPolicy(t, bs, "acme", abacTestPolicy("b", "acme", "alice", "kv/**", iam.EffectDeny))
	seedLegacyABACPolicy(t, bs, "acme", abacTestPolicy("c", "acme", "alice", "kv/**", iam.EffectAllow))
	storage := newLegacyCleanupPolicyStorage(bs)

	want := [][]string{{"a"}, {"b"}, {"c"}, {}}
	for offset, names := range want {
		page, err := storage.ListPolicies(acmeCtx, "acme", iam.ListOptions{Offset: offset, Limit: 1})
		require.NoError(t, err)
		require.NotNil(t, page)
		assert.Equal(t, names, abacPolicyNames(page), "offset %d", offset)
		if offset == 2 {
			assert.Nil(t, page[0].LoadError, "the c page is the tenant copy")
		}
	}

	page, err := storage.ListPolicies(acmeCtx, "acme", iam.ListOptions{Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, abacPolicyNames(page))
}

func TestLegacyCleanupPolicyStorage_SharedStoreDeletesOnce(t *testing.T) {
	_, tm, bs := newABACTenantRoutedStorage(t)
	_, err := tm.CreateTenant(tenantctx.WithSystem(context.Background()), iam.CreateTenantOptions{ID: "beta", Name: "Beta"})
	require.NoError(t, err)
	betaCtx := tenantctx.WithTenant(context.Background(), "beta")
	require.NoError(t, bs.CreatePolicy(betaCtx, "beta",
		abacTestPolicy("beta-allow", "beta", "alice", "kv/**", iam.EffectAllow)))

	require.NoError(t, newLegacyCleanupPolicyStorage(bs).DeletePolicy(betaCtx, "beta", "beta-allow"))

	_, err = bs.GetPolicy(betaCtx, "beta", "beta-allow")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "err = %v", err)
}

func TestLegacyCleanupPolicyStorage_PrefixTenantCreateUpdateGet(t *testing.T) {
	shared := qrdbsdk.NewMemoryKVStore()
	ps := newLegacyCleanupPolicyStorage(iam.NewBackendStorage(newTenantRoutingKVStore(newFakeStoreResolver(shared))))
	betaCtx := tenantctx.WithTenant(context.Background(), "beta")
	key := iam.KeyPrefixTenant + "beta" + iam.KeyPrefixPolicy + "beta-p"

	require.NoError(t, ps.CreatePolicy(betaCtx, "beta", abacTestPolicy("beta-p", "beta", "alice", "kv/**", iam.EffectAllow)))
	require.NoError(t, ps.UpdatePolicy(betaCtx, "beta", abacTestPolicy("beta-p", "beta", "alice", "kv/**", iam.EffectDeny)))
	got, err := ps.GetPolicy(betaCtx, "beta", "beta-p")
	require.NoError(t, err)
	require.Len(t, got.Spec.Rules, 1)
	assert.Equal(t, iam.EffectDeny, got.Spec.Rules[0].Effect)
	present, err := shared.Exists(context.Background(), key)
	require.NoError(t, err)
	assert.True(t, present, "an update must not delete the prefix tenant's one record")

	pe := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: ps, ABACSync: &recordingABACSync{}})
	require.NoError(t, pe.UpdatePolicy(betaCtx, "beta", abacTestPolicy("beta-p", "beta", "bob", "kv/**", iam.EffectDeny)))
	got, err = pe.GetPolicy(betaCtx, "beta", "beta-p")
	require.NoError(t, err)
	require.Len(t, got.Spec.Rules, 1)
	assert.Equal(t, iam.EffectDeny, got.Spec.Rules[0].Effect)
	assert.Equal(t, []string{"bob"}, got.Spec.Subjects.Users)
	present, err = shared.Exists(context.Background(), key)
	require.NoError(t, err)
	assert.True(t, present, "an engine update must not delete the prefix tenant's one record")

	require.NoError(t, ps.DeletePolicy(betaCtx, "beta", "beta-p"))
	_, err = ps.GetPolicy(betaCtx, "beta", "beta-p")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "err = %v", err)
}

func TestLegacyCleanupPolicyStorage_BarrierTenantUpdateDeletesLegacy(t *testing.T) {
	_, _, bs := newABACTenantRoutedStorage(t)
	ps := newLegacyCleanupPolicyStorage(bs)
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")
	sys := tenantctx.WithSystem(context.Background())
	seedLegacyABACPolicy(t, bs, "acme", abacTestPolicy("acme-p", "acme", "alice", "kv/**", iam.EffectAllow))

	require.NoError(t, ps.UpdatePolicy(acmeCtx, "acme", abacTestPolicy("acme-p", "acme", "alice", "kv/**", iam.EffectDeny)))
	got, err := bs.GetPolicy(acmeCtx, "acme", "acme-p")
	require.NoError(t, err)
	require.Len(t, got.Spec.Rules, 1)
	assert.Equal(t, iam.EffectDeny, got.Spec.Rules[0].Effect)
	_, err = bs.GetPolicy(sys, "acme", "acme-p")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "legacy copy: err = %v", err)

	require.NoError(t, ps.CreatePolicy(acmeCtx, "acme", abacTestPolicy("acme-new", "acme", "alice", "kv/**", iam.EffectDeny)))
	got, err = ps.GetPolicy(acmeCtx, "acme", "acme-new")
	require.NoError(t, err)
	assert.Equal(t, "acme-new", got.Metadata.Name)
	_, err = bs.GetPolicy(sys, "acme", "acme-new")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "a create writes no legacy copy: err = %v", err)
}

// putRefusingAtomicStore is an atomic store whose plain Put and Delete fail
// the test, so only the guarded writes may change it.
type putRefusingAtomicStore struct {
	interfaces.KVStore
	atomicStore
	t *testing.T
}

func newPutRefusingAtomicStore(t *testing.T) *putRefusingAtomicStore {
	st := qrdbsdk.NewMemoryKVStore()
	return &putRefusingAtomicStore{KVStore: st, atomicStore: st, t: t}
}

func (s *putRefusingAtomicStore) Put(_ context.Context, key string, _ []byte) error {
	s.t.Errorf("Put(%q) called; the guarded PutIfAbsent must be used", key)
	return errors.New("put not allowed")
}

func (s *putRefusingAtomicStore) Delete(_ context.Context, key string) error {
	s.t.Errorf("Delete(%q) called; the guarded CompareAndDelete must be used", key)
	return errors.New("delete not allowed")
}

func TestLegacyCleanupPolicyStorage_CreateThroughRouterIsGuarded(t *testing.T) {
	ctx := context.Background()
	acmeCtx := tenantctx.WithTenant(ctx, "acme")
	key := iam.KeyPrefixTenant + "acme" + iam.KeyPrefixPolicy + "acme-p"
	p := abacTestPolicy("acme-p", "acme", "alice", "kv/**", iam.EffectDeny)

	r := newFakeStoreResolver(qrdbsdk.NewMemoryKVStore())
	guarded := r.barrierTenantStore("acme", newPutRefusingAtomicStore(t))
	ps := newLegacyCleanupPolicyStorage(iam.NewBackendStorage(newTenantRoutingKVStore(r)))
	require.NoError(t, ps.CreatePolicy(acmeCtx, "acme", p))
	present, err := guarded.Exists(ctx, key)
	require.NoError(t, err)
	assert.True(t, present, "the create must store through PutIfAbsent")
	reverter, ok := ps.(iam.PolicyCreateReverter)
	require.True(t, ok, "the wrapper must revert a create")
	require.NoError(t, reverter.RevertCreatedPolicy(acmeCtx, "acme", p))
	present, err = guarded.Exists(ctx, key)
	require.NoError(t, err)
	assert.False(t, present, "the revert must remove through CompareAndDelete")

	r = newFakeStoreResolver(qrdbsdk.NewMemoryKVStore())
	plain := r.barrierTenantStore("acme", kvstore.NewMemory())
	ps = newLegacyCleanupPolicyStorage(iam.NewBackendStorage(newTenantRoutingKVStore(r)))
	assert.ErrorIs(t, ps.CreatePolicy(acmeCtx, "acme", p), ErrTenantStoreNotAtomic)
	present, err = plain.Exists(ctx, key)
	require.NoError(t, err)
	assert.False(t, present, "a store without guarded writes must not be written")
}

func TestLegacyCleanupPolicyStorage_RestoreKeepsACompetingWrite(t *testing.T) {
	ctx := context.Background()
	acmeCtx := tenantctx.WithTenant(ctx, "acme")
	const name = "acme-p"
	allow := abacTestPolicy(name, "acme", "alice", "kv/**", iam.EffectAllow)
	denyA := abacTestPolicy(name, "acme", "alice", "kv/**", iam.EffectDeny)
	denyB := abacTestPolicy(name, "acme", "bob", "kv/**", iam.EffectDeny)
	recreated := abacTestPolicy(name, "acme", "carol", "kv/**", iam.EffectAllow)

	r := newFakeStoreResolver(qrdbsdk.NewMemoryKVStore())
	guarded := newPutRefusingAtomicStore(t)
	r.barrierTenantStore("acme", guarded)
	nodeA := newLegacyCleanupPolicyStorage(iam.NewBackendStorage(newTenantRoutingKVStore(r)))
	// Node B shares the tenant's records through its own router.
	rB := newFakeStoreResolver(qrdbsdk.NewMemoryKVStore())
	rB.barrierTenantStore("acme", guarded.KVStore)
	nodeB := newLegacyCleanupPolicyStorage(iam.NewBackendStorage(newTenantRoutingKVStore(rB)))
	snapshotter, ok := nodeA.(iam.PolicyWriteSnapshotter)
	require.True(t, ok, "the wrapper must snapshot a write")

	// Update: A snapshots its allow, B replaces it with a deny before A writes.
	require.NoError(t, nodeA.CreatePolicy(acmeCtx, "acme", allow))
	restore, err := snapshotter.SnapshotPolicy(acmeCtx, "acme", name, denyA)
	require.NoError(t, err)
	require.NoError(t, nodeB.UpdatePolicy(acmeCtx, "acme", denyB))
	want, err := nodeB.GetPolicy(acmeCtx, "acme", name)
	require.NoError(t, err)
	require.NoError(t, restore(acmeCtx))
	got, err := nodeA.GetPolicy(acmeCtx, "acme", name)
	require.NoError(t, err)
	assert.Equal(t, want, got, "the restore must keep B's deny")

	// Delete: A snapshots for a delete; the record goes, and B re-creates it.
	restore, err = snapshotter.SnapshotPolicy(acmeCtx, "acme", name, nil)
	require.NoError(t, err)
	require.NoError(t, nodeB.DeletePolicy(acmeCtx, "acme", name))
	require.NoError(t, nodeB.CreatePolicy(acmeCtx, "acme", recreated))
	want, err = nodeB.GetPolicy(acmeCtx, "acme", name)
	require.NoError(t, err)
	require.NoError(t, restore(acmeCtx))
	got, err = nodeA.GetPolicy(acmeCtx, "acme", name)
	require.NoError(t, err)
	assert.Equal(t, want, got, "the restore must keep B's re-created policy")
}

// legacyDeleteFailingStorage fails every DeletePolicy made with the system
// context and passes the rest to the wrapped storage.
type legacyDeleteFailingStorage struct {
	iam.PolicyStorage
}

func (s legacyDeleteFailingStorage) DeletePolicy(ctx context.Context, tenantID, name string) error {
	if bound, ok := tenantctx.Lookup(ctx); ok && tenantctx.IsSystem(bound) {
		return abacTestStorageError{}
	}
	return s.PolicyStorage.DeletePolicy(ctx, tenantID, name)
}

func TestLegacyCleanupPolicyStorage_LegacyDeleteFailureKeepsTheTenantCopy(t *testing.T) {
	_, _, bs := newABACTenantRoutedStorage(t)
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")

	err := newLegacyCleanupPolicyStorage(legacyDeleteFailingStorage{bs}).DeletePolicy(acmeCtx, "acme", "acme-allow")
	assert.ErrorIs(t, err, abacTestStorageError{})

	p, err := bs.GetPolicy(acmeCtx, "acme", "acme-allow")
	require.NoError(t, err)
	assert.Equal(t, "acme-allow", p.Metadata.Name)
}

// recordingABACSync counts the engine's ABAC syncs and accepts every one.
type recordingABACSync struct{ calls int }

func (r *recordingABACSync) Sync(context.Context) error {
	r.calls++
	return nil
}

// requireSystemBoundPolicyKept checks that acme's policy "p" holds effect in
// acme's own namespace, has no legacy system-namespace copy, and is listed.
func requireSystemBoundPolicyKept(t *testing.T, bs *iam.BackendStorage, listed []*iam.Policy, effect iam.Effect) {
	t.Helper()
	got, err := bs.GetPolicy(tenantctx.WithTenant(context.Background(), "acme"), "acme", "p")
	require.NoError(t, err)
	require.Len(t, got.Spec.Rules, 1)
	assert.Equal(t, effect, got.Spec.Rules[0].Effect)
	_, err = bs.GetPolicy(tenantctx.WithSystem(context.Background()), "acme", "p")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "legacy copy: err = %v", err)
	assert.Equal(t, []string{"acme-allow", "p"}, abacPolicyNames(listed))
}

func TestLegacyCleanupPolicyStorage_SystemBoundCreateThenUpdateKeepsThePolicy(t *testing.T) {
	_, _, bs := newABACTenantRoutedStorage(t)
	ctx := tenantctx.WithSystem(context.Background())
	w := newLegacyCleanupPolicyStorage(bs)

	require.NoError(t, w.CreatePolicy(ctx, "acme", abacTestPolicy("p", "acme", "alice", "kv/**", iam.EffectAllow)))
	require.NoError(t, w.UpdatePolicy(ctx, "acme", abacTestPolicy("p", "acme", "alice", "kv/**", iam.EffectDeny)))

	listed, err := w.ListPolicies(ctx, "acme", iam.ListOptions{})
	require.NoError(t, err)
	requireSystemBoundPolicyKept(t, bs, listed, iam.EffectDeny)
}

func TestLegacyCleanupPolicyStorage_SystemBoundEngineCreateThenUpdateKeepsThePolicy(t *testing.T) {
	_, _, bs := newABACTenantRoutedStorage(t)
	ctx := tenantctx.WithSystem(context.Background())
	sync := &recordingABACSync{}
	pe := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: newLegacyCleanupPolicyStorage(bs), ABACSync: sync})

	require.NoError(t, pe.CreatePolicy(ctx, "acme", abacTestPolicy("p", "acme", "alice", "kv/**", iam.EffectAllow)))
	require.NoError(t, pe.UpdatePolicy(ctx, "acme", abacTestPolicy("p", "acme", "alice", "kv/**", iam.EffectDeny)))
	assert.Equal(t, 2, sync.calls)

	listed, err := pe.ListPolicies(ctx, "acme", iam.ListOptions{})
	require.NoError(t, err)
	requireSystemBoundPolicyKept(t, bs, listed, iam.EffectDeny)
}

func TestLegacyCleanupPolicyStorage_TenantCallerCannotNameAnotherTenant(t *testing.T) {
	_, _, bs := newABACTenantRoutedStorage(t)
	seedABACConflict(t, bs)
	tenantBefore, legacyBefore := abacPolicyRecords(t, bs, "dup")
	betaCtx := tenantctx.WithTenant(context.Background(), "beta")
	w := newLegacyCleanupPolicyStorage(bs)
	requireForbidden := func(t *testing.T, err error) {
		t.Helper()
		assert.True(t, dberrors.Is(err, dberrors.ErrForbidden), "err = %v", err)
	}

	requireForbidden(t, w.CreatePolicy(betaCtx, "acme", abacTestPolicy("p", "acme", "alice", "kv/**", iam.EffectAllow)))
	requireForbidden(t, w.UpdatePolicy(betaCtx, "acme", abacTestPolicy("dup", "acme", "alice", "kv/**", iam.EffectAllow)))
	_, err := w.GetPolicy(betaCtx, "acme", "dup")
	requireForbidden(t, err)
	_, err = w.ListPolicies(betaCtx, "acme", iam.ListOptions{})
	requireForbidden(t, err)
	requireForbidden(t, w.DeletePolicy(betaCtx, "acme", "dup"))

	tenantAfter, legacyAfter := abacPolicyRecords(t, bs, "dup")
	assert.Equal(t, tenantBefore, tenantAfter)
	assert.Equal(t, legacyBefore, legacyAfter)
	got, err := bs.ListPolicies(tenantctx.WithTenant(context.Background(), "acme"), "acme", iam.ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"acme-allow", "dup"}, abacPolicyNames(got))
}

func TestLegacyCleanupPolicyStorage_UnboundContextRefused(t *testing.T) {
	_, _, bs := newABACTenantRoutedStorage(t)
	w := newLegacyCleanupPolicyStorage(bs)

	err := w.CreatePolicy(context.Background(), "acme", abacTestPolicy("p", "acme", "alice", "kv/**", iam.EffectAllow))
	assert.True(t, dberrors.Is(err, dberrors.ErrForbidden), "create: err = %v", err)
	err = w.UpdatePolicy(context.Background(), "acme", abacTestPolicy("p", "acme", "alice", "kv/**", iam.EffectAllow))
	assert.True(t, dberrors.Is(err, dberrors.ErrForbidden), "update: err = %v", err)

	_, err = bs.GetPolicy(tenantctx.WithTenant(context.Background(), "acme"), "acme", "p")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "tenant copy: err = %v", err)
	_, err = bs.GetPolicy(tenantctx.WithSystem(context.Background()), "acme", "p")
	assert.True(t, dberrors.Is(err, dberrors.ErrNotFound), "legacy copy: err = %v", err)
}

func TestQRDBABACSync_InvalidTenantPolicyFailsInitialLoad(t *testing.T) {
	s, _ := newABACTestServer(t, true)
	_, tm, bs := newABACTenantRoutedStorage(t)
	abacSync := newQRDBABACSync(s, tm, bs)
	pe := iam.NewPolicyEngine(iam.PolicyEngineOptions{Storage: newLegacyCleanupPolicyStorage(bs), ABACSync: abacSync})
	require.NoError(t, initialABACLoad(context.Background(), pe.SyncABACAfter, abacSync.migrateLegacyPolicies),
		"the valid system deny and tenant allow load")

	invalid := abacTestPolicy("empty-when", "acme", "alice", "kv/**", iam.EffectAllow)
	invalid.Spec.Rules[0].When = []iam.Condition{{}}
	data, err := json.Marshal(invalid)
	require.NoError(t, err)
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")
	require.NoError(t, bs.PutPolicyRecord(acmeCtx, "acme", "empty-when", data))
	stored, err := bs.GetPolicy(acmeCtx, "acme", "empty-when")
	require.NoError(t, err, "the stored record decodes")
	require.Nil(t, stored.LoadError)

	err = initialABACLoad(context.Background(), pe.SyncABACAfter, abacSync.migrateLegacyPolicies)
	var initErr *IAMComponentInitError
	require.True(t, errors.As(err, &initErr), "err = %v", err)
	assert.Equal(t, "initial ABAC policy load", initErr.Component)
}

func TestPolicyToABAC(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	updated := created.Add(time.Hour)
	in := &iam.Policy{
		APIVersion: "abac.qrdb.io/v1",
		Kind:       "Policy",
		Metadata: iam.PolicyMetadata{
			Name:        "office-hours",
			Tenant:      "acme",
			Description: "reads only in office hours",
			Labels:      map[string]string{"team": "ops"},
			Priority:    7,
			Created:     created,
			Updated:     updated,
		},
		Spec: iam.PolicySpec{
			Subjects: iam.SubjectSelector{
				Users:         []string{"alice"},
				Groups:        []string{"ops"},
				Roles:         []string{"reader"},
				Services:      []string{"billing"},
				Match:         map[string]interface{}{"department": "ops"},
				Authenticated: true,
				Any:           true,
			},
			Variables: map[string]string{"open": "9"},
			Rules: []iam.Rule{{
				Name:      "allow-read",
				Effect:    iam.EffectAllow,
				Priority:  3,
				Actions:   []string{"read"},
				Resources: []string{"kv/*"},
				When:      []iam.Condition{{Expression: "hour >= 9"}},
				Unless:    []iam.Condition{{Expression: "holiday"}},
			}},
		},
	}

	want := &qrdbsdk.ABACPolicy{
		APIVersion: "abac.qrdb.io/v1",
		Kind:       "Policy",
		Metadata: qrdbsdk.ABACPolicyMetadata{
			Name:        "office-hours",
			Tenant:      "acme",
			Description: "reads only in office hours",
			Labels:      map[string]string{"team": "ops"},
			Priority:    7,
			Created:     created,
			Updated:     updated,
		},
		Spec: qrdbsdk.ABACPolicySpec{
			Subjects: qrdbsdk.ABACSubjectSelector{
				Users:         []string{"alice"},
				Groups:        []string{"ops"},
				Roles:         []string{"reader"},
				Services:      []string{"billing"},
				Match:         map[string]interface{}{"department": "ops"},
				Authenticated: true,
				Any:           true,
			},
			Variables: map[string]string{"open": "9"},
			Rules: []qrdbsdk.ABACRule{{
				Name:      "allow-read",
				Effect:    qrdbsdk.ABACEffectAllow,
				Priority:  3,
				Actions:   []string{"read"},
				Resources: []string{"kv/*"},
				When:      []qrdbsdk.ABACCondition{{Expression: "hour >= 9"}},
				Unless:    []qrdbsdk.ABACCondition{{Expression: "holiday"}},
			}},
		},
	}

	got, err := policyToABAC(in)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestPolicyToABAC_NilAndEmpty(t *testing.T) {
	none, err := policyToABAC(nil)
	require.NoError(t, err)
	assert.Nil(t, none)

	got, err := policyToABAC(&iam.Policy{})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.NotNil(t, got.Spec.Rules, "rules must be an empty slice, not nil")
	assert.Empty(t, got.Spec.Rules)
}

func TestConditionsToABAC(t *testing.T) {
	tests := []struct {
		name  string
		conds []iam.Condition
		want  []qrdbsdk.ABACCondition
	}{
		{name: "nil", conds: nil, want: nil},
		{name: "empty", conds: []iam.Condition{}, want: nil},
		{
			name:  "expressions keep their order",
			conds: []iam.Condition{{Expression: "a"}, {Expression: "b"}},
			want:  []qrdbsdk.ABACCondition{{Expression: "a"}, {Expression: "b"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := conditionsToABAC("when", tt.conds)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func nestedConditionPolicy(nested iam.Condition) *iam.Policy {
	return &iam.Policy{
		Metadata: iam.PolicyMetadata{Name: "p1", Tenant: "t1"},
		Spec: iam.PolicySpec{Rules: []iam.Rule{{
			Name:      "r1",
			Effect:    iam.EffectAllow,
			Actions:   []string{"read"},
			Resources: []string{"*"},
			Unless:    []iam.Condition{{Expression: "x == 1"}, nested},
		}}},
	}
}

func requireNestedConditionError(t *testing.T, err error, field string, index int) {
	t.Helper()
	var nested *iam.NestedConditionError
	if !errors.As(err, &nested) {
		t.Fatalf("expected iam.NestedConditionError, got %T: %v", err, err)
	}
	if nested.Field != field || nested.Index != index {
		t.Errorf("got %s[%d], want %s[%d]", nested.Field, nested.Index, field, index)
	}
}

func TestConditionsToABAC_Flat(t *testing.T) {
	got, err := conditionsToABAC("when", []iam.Condition{{Expression: "a == 1"}, {Expression: "b and not c"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0].Expression != "a == 1" || got[1].Expression != "b and not c" {
		t.Errorf("converted conditions = %+v", got)
	}

	empty, err := conditionsToABAC("when", nil)
	if err != nil || empty != nil {
		t.Errorf("empty conditions: got %+v, %v", empty, err)
	}
}

func TestConditionsToABAC_NestedRefused(t *testing.T) {
	cases := map[string]iam.Condition{
		"and": {And: []iam.Condition{{Expression: "a == 1"}}},
		"or":  {Or: []iam.Condition{{Expression: "a == 1"}}},
		"not": {Not: &iam.Condition{Expression: "a == 1"}},
	}
	for name, nested := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := conditionsToABAC("when", []iam.Condition{nested})
			requireNestedConditionError(t, err, "when", 0)
			if got != nil {
				t.Errorf("refused conversion returned conditions: %+v", got)
			}
		})
	}
}

func TestPolicyToABAC_NestedRefused(t *testing.T) {
	got, err := policyToABAC(nestedConditionPolicy(iam.Condition{Not: &iam.Condition{Expression: "a == 1"}}))
	requireNestedConditionError(t, err, "unless", 1)
	if got != nil {
		t.Errorf("refused conversion returned a policy: %+v", got)
	}
}

func TestPolicyToABAC_FlatConverted(t *testing.T) {
	got, err := policyToABAC(nestedConditionPolicy(iam.Condition{Expression: "y == 2"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	unless := got.Spec.Rules[0].Unless
	if len(unless) != 2 || unless[1].Expression != "y == 2" {
		t.Errorf("converted unless = %+v", unless)
	}
}

func TestQRDBPolicySimulator_NestedRefused(t *testing.T) {
	policy := nestedConditionPolicy(iam.Condition{Or: []iam.Condition{{Expression: "a == 1"}}})
	resp, err := newQRDBPolicySimulator().Simulate(context.Background(), policy, &iam.CheckRequest{})
	requireNestedConditionError(t, err, "unless", 1)
	if resp != nil {
		t.Errorf("refused simulation returned a response: %+v", resp)
	}
}

// TestQRDBPolicySimulator_EmptyWhenCondition records what go-qrdb does with a
// condition that has no expression: the shape a YAML block with an unknown
// key (all:, any:, none:) decoded to before ParsePolicyYAML became strict.
// go-qrdb refuses to compile it. A block that set an expression beside the
// unknown key compiled, without the dropped part, which is why the decode
// is strict.
func TestQRDBPolicySimulator_EmptyWhenCondition(t *testing.T) {
	policy := &iam.Policy{
		APIVersion: iam.PolicyYAMLAPIVersion,
		Kind:       "Policy",
		Metadata:   iam.PolicyMetadata{Name: "p1", Tenant: "t1"},
		Spec: iam.PolicySpec{Rules: []iam.Rule{{
			Name:      "r1",
			Effect:    iam.EffectAllow,
			Actions:   []string{"read"},
			Resources: []string{"*"},
			When:      []iam.Condition{{}},
		}}},
	}
	resp, err := newQRDBPolicySimulator().Simulate(context.Background(), policy,
		&iam.CheckRequest{Resource: "doc", Action: "read"})
	if err == nil || resp != nil {
		t.Fatalf("Simulate() = %+v, %v; want go-qrdb to refuse the empty expression", resp, err)
	}
}

// A tenant held by several stored policies that do not compile is repaired
// one policy at a time through the real engine and sync: each repair stays
// stored although the sync still fails on the policies not yet repaired, a
// repair that does not compile is refused, and the next unseal admits the
// tenant.
func TestQRDBABACSync_HeldTenantPoliciesAreRepairedOneAtATime(t *testing.T) {
	s, _ := newABACTestServer(t, true)
	r := newFakeStoreResolver(qrdbsdk.NewMemoryKVStore())
	r.barrierTenantStore("acme", qrdbsdk.NewMemoryKVStore())
	resolver := admittedFakeResolver{admit: s.admittedStoreResolver(fakeQRDBResolver{store: fakeQRDBStore{}}), stores: r}
	bs := iam.NewBackendStorage(newTenantRoutingKVStore(resolver))
	tm := iam.NewDefaultTenantManager(bs)

	sysCtx := tenantctx.WithSystem(context.Background())
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")
	_, err := tm.CreateTenant(sysCtx, iam.CreateTenantOptions{ID: "acme", Name: "Acme"})
	require.NoError(t, err)
	fixed := func(name string) *iam.Policy {
		return abacTestPolicy(name, "acme", "alice", "kv/**", iam.EffectAllow)
	}
	broken := func(name string) *iam.Policy {
		p := fixed(name)
		p.Spec.Rules[0].When = []iam.Condition{{}}
		return p
	}
	for _, name := range []string{"bad-a", "bad-b", "bad-c"} {
		data, err := json.Marshal(broken(name))
		require.NoError(t, err)
		require.NoError(t, bs.PutPolicyRecord(acmeCtx, "acme", name, data))
	}

	abacSync := newQRDBABACSync(s, tm, bs)
	pe := iam.NewPolicyEngine(iam.PolicyEngineOptions{
		Storage:  bs,
		ABACSync: abacSync,
		Repair:   newHeldTenantPolicyRepair(s.tenantUnsealHooks.Admit),
	})
	abacSync.resyncOnUnseal(&s.tenantUnsealHooks, pe.SyncABACAtUnseal)
	barrier := service.NewTenantBarrierService(unsealingTenantClient{}, service.WithTenantUnsealHooks(&s.tenantUnsealHooks))

	require.Error(t, barrier.TenantBarrierUnseal(tenantAdminCtx("acme"), "acme", []byte("key")),
		"three policies that do not compile fail the unseal checks")
	require.Error(t, s.tenantUnsealHooks.Admit("acme"), "the tenant is held")

	repairCtx := tenantctx.WithPolicyRepair(acmeCtx, "acme")

	// First repair: stored although bad-b and bad-c still fail the sync.
	require.NoError(t, pe.UpdatePolicy(repairCtx, "acme", fixed("bad-a")))
	got, err := bs.GetPolicy(repairCtx, "acme", "bad-a")
	require.NoError(t, err)
	require.NotEmpty(t, got.Spec.Rules)
	assert.Empty(t, got.Spec.Rules[0].When, "the repaired bad-a is stored")
	require.Error(t, s.tenantUnsealHooks.Admit("acme"), "one repair does not admit the tenant")

	// A repair that does not compile is refused and changes nothing.
	err = pe.UpdatePolicy(repairCtx, "acme", broken("bad-a"))
	var invalid *iam.InvalidPolicyError
	require.True(t, errors.As(err, &invalid), "err = %v", err)
	got, err = bs.GetPolicy(repairCtx, "acme", "bad-a")
	require.NoError(t, err)
	require.NotEmpty(t, got.Spec.Rules)
	assert.Empty(t, got.Spec.Rules[0].When, "the refused repair left bad-a as it was")

	// Second repair removes a broken policy; bad-c still fails the sync.
	require.NoError(t, pe.DeletePolicy(repairCtx, "acme", "bad-b"))
	left, err := bs.ListPolicies(repairCtx, "acme", iam.ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"bad-a", "bad-c"}, abacPolicyNames(left))
	require.Error(t, s.tenantUnsealHooks.Admit("acme"), "two repairs do not admit the tenant")

	// Last repair: the whole set compiles, and the next unseal admits the tenant.
	require.NoError(t, pe.UpdatePolicy(repairCtx, "acme", fixed("bad-c")))
	require.NoError(t, barrier.TenantBarrierUnseal(tenantAdminCtx("acme"), "acme", []byte("key")))
	require.NoError(t, s.tenantUnsealHooks.Admit("acme"))
}

// A repair that arrives after the tenant's unseal reload has passed, but
// before the unseal releases the hold, is a strict write: when its sync
// fails it is rolled back, so the tenant never serves with an acknowledged
// policy change the live authorizer has not loaded.
func TestQRDBABACSync_RepairAfterUnsealReloadIsStrict(t *testing.T) {
	s, _ := newABACTestServer(t, true)
	r := newFakeStoreResolver(qrdbsdk.NewMemoryKVStore())
	r.barrierTenantStore("acme", qrdbsdk.NewMemoryKVStore())
	resolver := admittedFakeResolver{admit: s.admittedStoreResolver(fakeQRDBResolver{store: fakeQRDBStore{}}), stores: r}
	bs := iam.NewBackendStorage(newTenantRoutingKVStore(resolver))
	tm := iam.NewDefaultTenantManager(bs)

	sysCtx := tenantctx.WithSystem(context.Background())
	acmeCtx := tenantctx.WithTenant(context.Background(), "acme")
	repairCtx := tenantctx.WithPolicyRepair(acmeCtx, "acme")
	_, err := tm.CreateTenant(sysCtx, iam.CreateTenantOptions{ID: "acme", Name: "Acme"})
	require.NoError(t, err)
	good, err := json.Marshal(abacTestPolicy("good", "acme", "alice", "kv/**", iam.EffectAllow))
	require.NoError(t, err)
	require.NoError(t, bs.PutPolicyRecord(acmeCtx, "acme", "good", good))

	abacSync := newQRDBABACSync(s, tm, bs)
	pe := iam.NewPolicyEngine(iam.PolicyEngineOptions{
		Storage:  bs,
		ABACSync: abacSync,
		Repair:   newHeldTenantPolicyRepair(s.tenantUnsealHooks.Admit),
	})
	abacSync.resyncOnUnseal(&s.tenantUnsealHooks, pe.SyncABACAtUnseal)

	// This gate runs after the reload gate has passed and before the hold is
	// released. It stores a policy that does not compile, so the sync after
	// the repair below fails, and then repairs "good" as the system admin.
	ran := false
	var heldInWindow, putErr, repairErr error
	s.tenantUnsealHooks.RegisterGate(service.TenantUnsealGateFunc(func(context.Context, string) error {
		ran = true
		heldInWindow = s.tenantUnsealHooks.Admit("acme")
		bad := abacTestPolicy("bad", "acme", "alice", "kv/**", iam.EffectAllow)
		bad.Spec.Rules[0].When = []iam.Condition{{}}
		data, err := json.Marshal(bad)
		if err != nil {
			putErr = err
			return nil
		}
		putErr = bs.PutPolicyRecord(repairCtx, "acme", "bad", data)
		repairErr = pe.UpdatePolicy(repairCtx, "acme", abacTestPolicy("good", "acme", "alice", "kv/**", iam.EffectDeny))
		return nil
	}))
	barrier := service.NewTenantBarrierService(unsealingTenantClient{}, service.WithTenantUnsealHooks(&s.tenantUnsealHooks))

	require.NoError(t, barrier.TenantBarrierUnseal(tenantAdminCtx("acme"), "acme", []byte("key")))
	require.True(t, ran, "the second gate ran")
	require.Error(t, heldInWindow, "the tenant is still held between the reload and the release")
	require.NoError(t, putErr)
	require.Error(t, repairErr, "a repair after the reload is rejected when its sync fails")
	require.NoError(t, s.tenantUnsealHooks.Admit("acme"), "the unseal released the hold")

	got, err := bs.GetPolicy(acmeCtx, "acme", "good")
	require.NoError(t, err)
	require.NotEmpty(t, got.Spec.Rules)
	assert.Equal(t, iam.EffectAllow, got.Spec.Rules[0].Effect, "the rejected repair was rolled back")
}
