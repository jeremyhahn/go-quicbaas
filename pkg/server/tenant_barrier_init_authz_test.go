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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/qrdbservice"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// tenantBarrierInitCaller is an authenticated admin of tenantID whose token
// carries tenant_barrier:init and nothing else, as quicbaas's transports put
// it on a request: the quicbaas subject the facade's binding checks, and the
// qrdb identity the node authorizes.
func tenantBarrierInitCaller(tenantID string) context.Context {
	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID:            tenantID + "-admin",
		Username:      tenantID + "-admin",
		TenantID:      tenantID,
		Authenticated: true,
	})
	ctx = qrdbsdk.WithIdentity(ctx, &qrdbsdk.Identity{
		Subject:          tenantID + "-admin",
		TenantID:         tenantID,
		ScopePermissions: []string{"tenant_barrier:init"},
	})
	return tenantctx.WithTenant(ctx, tenantID)
}

// createNodeTenant onboards tenantID with isolation on the server's node,
// through the node's own tenant service, as quicbaas's IAM tenant lifecycle
// does (qrdbTenantProvisionerAdapter).
func createNodeTenant(t *testing.T, s *Server, tenantID string, isolation qrdbsdk.TenantIsolationType) {
	t.Helper()
	ctx, err := s.ServiceContext(context.Background())
	require.NoError(t, err)
	_, err = s.getQRDBNode().Services().Tenant.CreateTenant(ctx, tenantID, tenantID, isolation)
	require.NoError(t, err, "onboarding %s", tenantID)
}

// An own-tenant caller holding only tenant_barrier:init initializes its
// barrier, under prefix isolation and under a strict isolation floor. The
// facade's raft-isolation check (requireInitStatement) reads the tenant
// through the facade's own in-process tenant client, which carries no qrdb
// AuthMiddleware (newTenantBarrierClient), so the read checks no tenants:read;
// the route's tenant_barrier:init and the facade's tenant binding are the
// only gates.
func TestTenantBarrierInit_NeedsOnlyTheInitVerb(t *testing.T) {
	cases := []struct {
		name      string
		strict    bool
		isolation qrdbsdk.TenantIsolationType
	}{
		{name: "prefix isolation", isolation: qrdbsdk.TenantIsolationPrefix},
		{name: "strict isolation floor", strict: true, isolation: qrdbsdk.TenantIsolationBarrier},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newPersistentStandaloneServer(t)
			require.NoError(t, s.provisionDefaultVolumePool())
			if tc.strict {
				// qrdb reads the floor from the system tenant's cluster
				// isolation-policy record, as newStrictPrincipalServer sets it.
				node := s.getQRDBNode()
				require.NoError(t, qrdbservice.KV(node, node.Client().KV()).Put(context.Background(),
					"__sys:cluster/isolation_policy", []byte("strict")))
			}
			createNodeTenant(t, s, "acme", tc.isolation)

			svc := s.buildTenantBarrierService()
			require.NotNil(t, svc)
			t.Cleanup(func() { _ = s.tenantSDKClient.Close() })

			resp, err := svc.TenantBarrierInit(tenantBarrierInitCaller("acme"),
				&qrdbtransport.TenantBarrierInitRequest{TenantID: "acme", Strategy: "none"})
			require.NoError(t, err, "an own-tenant init with only tenant_barrier:init was refused")
			assert.Equal(t, "acme", resp.TenantID)
		})
	}
}
