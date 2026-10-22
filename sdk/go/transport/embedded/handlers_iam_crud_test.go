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

package embedded

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// TestEmbeddedIAMUser_GetListDelete asserts that a user stored in the
// identity store is read, listed and deleted through the embedded user
// client, and that a deleted user can no longer be read.
func TestEmbeddedIAMUser_GetListDelete(t *testing.T) {
	client, store := newUserClient(t)
	ctx := context.Background()
	userID := createTestUser(t, store, iam.LifecycleActive)

	got, err := client.Get(ctx, "t-1", userID)
	require.NoError(t, err)
	assert.Equal(t, userID, got.ID)
	assert.Equal(t, "ada@acme.test", got.Email)
	assert.Equal(t, "ada", got.Username)

	listed, err := client.List(ctx, "t-1", &iamsdk.ListUsersRequest{})
	require.NoError(t, err)
	require.Len(t, listed.Users, 1)
	assert.Equal(t, userID, listed.Users[0].ID)

	require.NoError(t, client.Delete(ctx, "t-1", userID))
	_, err = client.Get(ctx, "t-1", userID)
	assert.Error(t, err, "a deleted user must not be readable")
	listed, err = client.List(ctx, "t-1", &iamsdk.ListUsersRequest{})
	require.NoError(t, err)
	assert.Empty(t, listed.Users)
}

// TestEmbeddedIAMUser_GetAndDeleteRefuseBadIDs asserts that the embedded
// user handlers refuse a malformed user ID and report a missing user.
func TestEmbeddedIAMUser_GetAndDeleteRefuseBadIDs(t *testing.T) {
	client, _ := newUserClient(t)
	ctx := context.Background()

	_, err := client.Get(ctx, "t-1", "not-a-number")
	assert.Error(t, err, "a malformed user ID must be refused on Get")
	_, err = client.Get(ctx, "t-1", "999")
	assert.Error(t, err, "a missing user must be reported on Get")
	assert.Error(t, client.Delete(ctx, "t-1", "not-a-number"), "a malformed user ID must be refused on Delete")
}

// TestEmbeddedIAMRole_CreateUpdateGet asserts that a role created through
// the embedded role client is stored with its fields, and that an update
// replaces the name, description and permissions it names.
func TestEmbeddedIAMRole_CreateUpdateGet(t *testing.T) {
	svc, store := newIAMTestService()
	tr, err := New(svc)
	require.NoError(t, err)
	client := iamsdk.NewRoleClient(tr)
	ctx := context.Background()

	created, err := client.Create(ctx, "t-1", &iamsdk.CreateRoleRequest{
		Name: "auditor", Description: "reads audit logs", Permissions: []string{"audit:read"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	assert.Equal(t, "auditor", created.Name)
	assert.Equal(t, "t-1", created.TenantID)
	assert.Equal(t, []string{"audit:read"}, created.Permissions)

	updated, err := client.Update(ctx, "t-1", created.ID, &iamsdk.UpdateRoleRequest{
		Name: "senior-auditor", Description: "reads and exports audit logs", Permissions: []string{"audit:read", "audit:export"},
	})
	require.NoError(t, err)
	assert.Equal(t, "senior-auditor", updated.Name)
	assert.Equal(t, "reads and exports audit logs", updated.Description)
	assert.Equal(t, []string{"audit:read", "audit:export"}, updated.Permissions)

	stored, err := store.GetRole(ctx, "t-1", created.ID)
	require.NoError(t, err)
	assert.Equal(t, "senior-auditor", stored.Name)
	assert.Equal(t, []string{"audit:read", "audit:export"}, stored.Permissions)
}

// TestEmbeddedIAMRole_UpdateUnknownRoleIsRefused asserts that updating a
// role the tenant does not hold reaches the caller as an error.
func TestEmbeddedIAMRole_UpdateUnknownRoleIsRefused(t *testing.T) {
	svc, _ := newIAMTestService()
	tr, err := New(svc)
	require.NoError(t, err)
	client := iamsdk.NewRoleClient(tr)

	_, err = client.Update(context.Background(), "t-1", "no-such-role", &iamsdk.UpdateRoleRequest{Name: "x"})
	assert.Error(t, err)
}

// newTenantClient returns an embedded tenant client over an in-memory tenant
// manager.
func newTenantClient(t *testing.T) (iamsdk.TenantClient, iam.TenantManager) {
	t.Helper()
	mgr := iam.NewDefaultTenantManager(iam.NewMemoryStorage())
	tr, err := New(&mockQuicBaaSForTenants{tenants: mgr})
	require.NoError(t, err)
	return iamsdk.NewTenantClient(tr), mgr
}

// TestEmbeddedIAMTenant_CreateListDelete asserts that a tenant created
// through the embedded tenant client is stored and listed, and that a delete
// removes it.
func TestEmbeddedIAMTenant_CreateListDelete(t *testing.T) {
	client, mgr := newTenantClient(t)
	ctx := context.Background()

	created, err := client.Create(ctx, &iamsdk.CreateTenantRequest{ID: "acme", Name: "acme"})
	require.NoError(t, err)
	assert.Equal(t, "acme", created.ID)
	assert.Equal(t, "acme", created.Name)

	listed, err := client.List(ctx, &iamsdk.ListTenantsRequest{})
	require.NoError(t, err)
	ids := make([]string, 0, len(listed.Tenants))
	for _, tn := range listed.Tenants {
		ids = append(ids, tn.ID)
	}
	assert.Contains(t, ids, "acme")

	require.NoError(t, client.Delete(ctx, "acme"))
	_, err = mgr.GetTenant(ctx, "acme")
	assert.Error(t, err, "a deleted tenant must not be readable")
}

// TestEmbeddedIAMTenant_ErrorsReachTheCaller asserts that a duplicate create
// and a delete of an unknown tenant are refused through the embedded client.
func TestEmbeddedIAMTenant_ErrorsReachTheCaller(t *testing.T) {
	client, _ := newTenantClient(t)
	ctx := context.Background()

	_, err := client.Create(ctx, &iamsdk.CreateTenantRequest{ID: "acme", Name: "acme"})
	require.NoError(t, err)
	_, err = client.Create(ctx, &iamsdk.CreateTenantRequest{ID: "acme", Name: "acme"})
	assert.Error(t, err, "a duplicate tenant must be refused")
	assert.Error(t, client.Delete(ctx, "no-such-tenant"), "an unknown tenant must not delete")
}
