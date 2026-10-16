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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

func TestSubjectToQRDBIdentity(t *testing.T) {
	for _, tt := range subjectIdentityCases {
		t.Run(tt.name, func(t *testing.T) {
			got := subjectToQRDBIdentity(tt.subject)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantSubject, got.Subject)
			assert.Equal(t, tt.wantTenantID, got.TenantID)
			assert.Equal(t, tt.wantRoles, got.Roles)
			assert.Equal(t, tt.wantMetadata, got.Metadata)
		})
	}
}

func TestQRDBPEPRBACAuthorizer_Authorize_NoNode(t *testing.T) {
	pep := newQRDBPEPRBACAuthorizer(&Server{})

	err := pep.Authorize(context.Background(), &iam.Subject{ID: "alice"}, "kv/orders", "read")

	var unavailable *QRDBNodeClientUnavailableError
	require.ErrorAs(t, err, &unavailable)
	assert.Equal(t, "pep: qrdb node client is not yet available (server still composing)", err.Error())
}

// TestQRDBPEPRBACAuthorizer_Authorize_DelegatesToQRDB proves the decision is
// qrdb's: the node's composed authorizer grants alice through a reloaded
// allow policy, and denies everyone it does not grant.
func TestQRDBPEPRBACAuthorizer_Authorize_DelegatesToQRDB(t *testing.T) {
	const tenant = "acme"
	s, node := newABACTestServer(t, true)
	grant := policyToABAC(abacTestPolicy("grant-alice", tenant, "alice", "kv/**", iam.EffectAllow))
	require.NoError(t, node.ABACPolicyReloader().Reload([]*qrdbsdk.ABACPolicy{grant}))
	pep := newQRDBPEPRBACAuthorizer(s)
	ctx := qrdbsdk.ContextWithTenant(context.Background(), tenant)

	tests := []struct {
		name    string
		subject *iam.Subject
		wantErr bool
	}{
		{name: "granted subject", subject: &iam.Subject{ID: "alice", TenantID: tenant}},
		{name: "ungranted subject", subject: &iam.Subject{ID: "bob", TenantID: tenant}, wantErr: true},
		{name: "nil subject", subject: nil, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pep.Authorize(ctx, tt.subject, "kv/orders", "read")
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
