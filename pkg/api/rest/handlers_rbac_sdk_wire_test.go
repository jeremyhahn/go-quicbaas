//go:build rest

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

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rbac"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	resttransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/rest"
)

// The SDK RBAC client over the REST transport, driven against this
// package's real router: every verb must reach the tenant-scoped IAM role
// routes and land in the tenant's identity store.

const rbacWireTenant = "tenant1"

// newRBACWireClient serves the IAM routes for rbacWireTenant, seeds user 11
// in it, and returns an RBAC client whose transport defaults to that tenant.
func newRBACWireClient(t *testing.T) (rbac.Client, *iam.IdentityStore) {
	t.Helper()
	server, mockService, memStorage, _, cleanup := setupIAMTestServer(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	require.NoError(t, memStorage.CreateTenant(ctx, &iam.Tenant{ID: rbacWireTenant, Name: "Tenant 1"}))
	require.NoError(t, memStorage.CreateUser(ctx, &iam.User{ID: 11, TenantID: rbacWireTenant, Email: "user1@example.com", Name: "User 1"}))

	// Bind the tenant go-qrdb's AuthMiddleware binds for an authenticated
	// caller; the tenant-scoped IAM routes refuse a foreign tenant.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.router.ServeHTTP(w, r.WithContext(tenantctx.WithTenant(r.Context(), rbacWireTenant)))
	}))
	t.Cleanup(srv.Close)
	tr, err := resttransport.New(&sdktransport.Config{Address: srv.URL},
		resttransport.WithDefaultTenant(rbacWireTenant))
	require.NoError(t, err)
	return rbac.NewClient(tr), mockService.identityStore
}

func TestSDKRESTRBAC_RoleLifecycleReachesTheIdentityStore(t *testing.T) {
	client, store := newRBACWireClient(t)
	ctx := context.Background()

	created, err := client.CreateRole(ctx, &rbac.CreateRoleRequest{
		RoleName:    "auditor",
		Permissions: []string{"audit:read"},
	})
	require.NoError(t, err)
	require.Equal(t, "auditor", created.RoleID)
	require.NotZero(t, created.Timestamp)

	listed, err := client.ListRoles(ctx, &rbac.ListRolesRequest{Limit: 1000})
	require.NoError(t, err)
	var found *rbac.RoleInfo
	for _, r := range listed.Roles {
		if r.RoleID == "auditor" {
			found = r
		}
	}
	require.NotNil(t, found, "the created role must be listed")
	require.Equal(t, "auditor", found.RoleName)
	require.Equal(t, []string{"audit:read"}, found.Permissions)

	assigned, err := client.AssignRole(ctx, &rbac.AssignRoleRequest{Username: "11", RoleName: "auditor"})
	require.NoError(t, err)
	require.True(t, assigned.Success)
	roles, err := store.GetUserRoles(ctx, rbacWireTenant, 11)
	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.Equal(t, "auditor", roles[0].ID)

	revoked, err := client.RevokeRole(ctx, &rbac.RevokeRoleRequest{Username: "11", RoleName: "auditor"})
	require.NoError(t, err)
	require.True(t, revoked.Success)
	roles, err = store.GetUserRoles(ctx, rbacWireTenant, 11)
	require.NoError(t, err)
	require.Empty(t, roles)

	deleted, err := client.DeleteRole(ctx, &rbac.DeleteRoleRequest{RoleName: "auditor"})
	require.NoError(t, err)
	require.True(t, deleted.Success)
	_, err = store.GetRole(ctx, rbacWireTenant, "auditor")
	require.Error(t, err, "the deleted role must be gone from the store")
}

func TestSDKRESTRBAC_NonNumericUsernameIsInvalidInput(t *testing.T) {
	client, _ := newRBACWireClient(t)

	_, err := client.AssignRole(context.Background(), &rbac.AssignRoleRequest{Username: "alice", RoleName: "auditor"})
	require.Error(t, err)
	require.True(t, sdkerrors.IsInvalidInput(err), "got %v", err)
}
