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
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtenant "github.com/jeremyhahn/go-qrdb/pkg/tenant"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
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
	byTenant map[string][]*iam.Policy
	err      error
	listed   []string
}

func (s *abacTestPolicies) ListPolicies(_ context.Context, tenantID string, _ iam.ListOptions) ([]*iam.Policy, error) {
	s.listed = append(s.listed, tenantID)
	if s.err != nil {
		return nil, s.err
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
			got, err := newQRDBABACSync(&Server{}, tt.tenants, storage).allPolicies(context.Background())
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
			got, err := newQRDBABACSync(&Server{}, tm, bs).allPolicies(ctx)
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
			got, err := newQRDBABACSync(&Server{}, tt.tenants, tt.storage).allPolicies(context.Background())
			assert.ErrorIs(t, err, abacTestStorageError{})
			assert.Nil(t, got)
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
func newABACTenantRoutedStorage(t *testing.T) (*fakeStoreResolver, iam.TenantManager, *iam.BackendStorage) {
	t.Helper()
	r := newFakeStoreResolver(kvstore.NewMemory())
	r.barrierTenant("acme")
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

	got, err := newQRDBABACSync(&Server{}, tm, bs).allPolicies(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"sys-deny"}, abacPolicyNames(got))
}

func TestQRDBABACSync_allPolicies_SealedSystemTenantAborts(t *testing.T) {
	got, err := newQRDBABACSync(&Server{}, nil, &abacTestPolicies{err: qrdbtenant.ErrTenantSealed}).allPolicies(context.Background())
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
	newQRDBABACSync(&Server{}, tm, bs).resyncOnUnseal(hooks, func(ctx context.Context, prepare func(context.Context)) error {
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
	got, err := a.allPolicies(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"acme-allow", "dup", "legacy-deny", "sys-deny"}, abacPolicyNames(got))
	assert.Equal(t, iam.EffectAllow, abacPolicyEffect(t, got, "dup"))

	r.sealTenant("acme")
	got, err = a.allPolicies(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"dup", "legacy-deny", "sys-deny"}, abacPolicyNames(got))
	assert.Equal(t, iam.EffectDeny, abacPolicyEffect(t, got, "dup"))
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

		got, err := a.allPolicies(context.Background())
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
		a.resyncOnUnseal(hooks, func(ctx context.Context, prepare func(context.Context)) error {
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
