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
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
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
			name: "listed tenants are read once each, system included",
			tenants: &abacTestTenants{tenants: []*iam.Tenant{
				{ID: "acme"}, {ID: "globex"}, {ID: qrdbsdk.SystemTenantID}, {ID: "acme"},
			}},
			wantListed: []string{"acme", "globex", qrdbsdk.SystemTenantID},
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

	assert.Equal(t, want, policyToABAC(in))
}

func TestPolicyToABAC_NilAndEmpty(t *testing.T) {
	assert.Nil(t, policyToABAC(nil))

	got := policyToABAC(&iam.Policy{})
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
			assert.Equal(t, tt.want, conditionsToABAC(tt.conds))
		})
	}
}
