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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// subjectIdentityCase is one iam.Subject -> qrdb identity conversion
// expectation, shared by the ABAC simulator and the PEP RBAC authorizer,
// which convert subjects the same way into their own identity types.
type subjectIdentityCase struct {
	name         string
	subject      *iam.Subject
	wantSubject  string
	wantTenantID string
	wantRoles    []string
	wantMetadata map[string]string
}

var subjectIdentityCases = []subjectIdentityCase{
	{name: "nil subject yields an empty identity", subject: nil},
	{
		name:         "TenantID wins over Tenant",
		subject:      &iam.Subject{ID: "alice", TenantID: "acme", Tenant: "globex", Roles: []string{"reader"}},
		wantSubject:  "alice",
		wantTenantID: "acme",
		wantRoles:    []string{"reader"},
	},
	{
		name:         "Tenant is the fallback when TenantID is empty",
		subject:      &iam.Subject{ID: "bob", Tenant: "globex"},
		wantSubject:  "bob",
		wantTenantID: "globex",
	},
	{
		name: "only string attributes become metadata",
		subject: &iam.Subject{ID: "carol", Attributes: map[string]interface{}{
			"department": "ops",
			"level":      3,
			"admin":      true,
		}},
		wantSubject:  "carol",
		wantMetadata: map[string]string{"department": "ops"},
	},
	{
		name:         "no attributes leaves metadata nil",
		subject:      &iam.Subject{ID: "dave", Attributes: map[string]interface{}{}},
		wantSubject:  "dave",
		wantMetadata: nil,
	},
}

func TestSubjectToABACIdentity(t *testing.T) {
	for _, tt := range subjectIdentityCases {
		t.Run(tt.name, func(t *testing.T) {
			got := subjectToABACIdentity(tt.subject)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantSubject, got.Subject)
			assert.Equal(t, tt.wantTenantID, got.TenantID)
			assert.Equal(t, tt.wantRoles, got.Roles)
			assert.Equal(t, tt.wantMetadata, got.Metadata)
		})
	}
}

func TestSimulationDefaultDenyError(t *testing.T) {
	err := denyAllAuthorizer{}.Authorize(context.Background(), nil, "kv/orders", "read")

	var defaultDeny *SimulationDefaultDenyError
	require.ErrorAs(t, err, &defaultDeny)
	assert.Equal(t, "iam: no matching allow policy", err.Error())
}

func TestQRDBPolicySimulator_Simulate(t *testing.T) {
	const tenant = "acme"
	allow := abacTestPolicy("grant-alice", tenant, "alice", "kv/**", iam.EffectAllow)
	deny := abacTestPolicy("block-mallory", tenant, "mallory", "kv/**", iam.EffectDeny)

	tests := []struct {
		name    string
		policy  *iam.Policy
		subject string
		want    *iam.CheckResponse
	}{
		{
			name:    "matching allow rule",
			policy:  allow,
			subject: "alice",
			want: &iam.CheckResponse{
				Allowed:       true,
				MatchedPolicy: "grant-alice",
				Reason:        "allowed by policy 'grant-alice'",
			},
		},
		{
			name:    "matching deny rule",
			policy:  deny,
			subject: "mallory",
			want: &iam.CheckResponse{
				MatchedPolicy: "block-mallory",
				MatchedRule:   "deny-mallory",
				Reason:        "denied by policy 'block-mallory', rule 'deny-mallory'",
			},
		},
		{
			name:    "no matching rule is a default deny",
			policy:  allow,
			subject: "bob",
			want:    &iam.CheckResponse{Reason: "default deny"},
		},
	}
	sim := newQRDBPolicySimulator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &iam.CheckRequest{
				Subject:  iam.Subject{ID: tt.subject, TenantID: tenant},
				Action:   "read",
				Resource: "kv/orders",
			}
			got, err := sim.Simulate(context.Background(), tt.policy, req)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestQRDBPolicySimulator_Simulate_InvalidPolicy(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(p *iam.Policy)
	}{
		{name: "wrong kind", mutate: func(p *iam.Policy) { p.Kind = "NotAPolicy" }},
		{name: "no rules", mutate: func(p *iam.Policy) { p.Spec.Rules = nil }},
		{name: "no tenant", mutate: func(p *iam.Policy) { p.Metadata.Tenant = "" }},
	}
	sim := newQRDBPolicySimulator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := abacTestPolicy("p", "acme", "alice", "kv/**", iam.EffectAllow)
			tt.mutate(policy)
			req := &iam.CheckRequest{Subject: iam.Subject{ID: "alice", TenantID: "acme"}, Action: "read", Resource: "kv/orders"}

			got, err := sim.Simulate(context.Background(), policy, req)
			require.Error(t, err)
			assert.Nil(t, got)
		})
	}
}
