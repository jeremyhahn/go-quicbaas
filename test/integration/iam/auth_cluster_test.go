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

//go:build integration
// +build integration

package iam_test

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// asSystemAdmin returns a view of tc whose HTTP helpers send the fixture
// system admin's bearer. It is for system-tenant calls only: creating,
// listing, reading, updating and deleting tenants. The cluster it is called
// on is unchanged and stays unauthenticated. The view shares tc's HTTP
// client and holds no gRPC connection, so closing it is a no-op.
func (tc *testCluster) asSystemAdmin() *testCluster {
	view := *tc
	view.grpcConn = nil
	view.bearer = func(ctx context.Context) (string, error) {
		return helpers.AuthSystemAdminContext(ctx, tc.httpEndpoint)
	}
	return &view
}

// asTenantAdmin returns a view of tc whose HTTP helpers send the bearer of
// tenantID's own admin. The first call for a tenant has the system admin
// provision it and deletes it when t ends (helpers.TenantAdmin), so the test
// does not create or delete that tenant itself. The bearer is bound to
// tenantID and is renewed with the admin certificate when it nears expiry.
// The view shares tc's HTTP client and holds no gRPC connection, so closing
// it is a no-op.
func (tc *testCluster) asTenantAdmin(t *testing.T, tenantID string) *testCluster {
	t.Helper()
	session := helpers.TenantAdmin(t, tc.httpEndpoint, tenantID)
	view := *tc
	view.grpcConn = nil
	view.bearer = session.Bearer
	return &view
}
