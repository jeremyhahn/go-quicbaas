//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// grantRoleTestFixture wires up an IAMHandler over the self-registration
// harness and exposes the backing identity store + tenant manager so the
// grantRoleIfMissing helper can be exercised against real storage.
func grantRoleTestFixture(t *testing.T) (*IAMHandler, *iam.IdentityStore, *testTenantManager) {
	t.Helper()
	handler := setupSelfRegHandler(t, config.IAMSelfServiceRegistrationConfig{Enabled: true})
	svc, ok := handler.server.service.(*selfRegService)
	require.True(t, ok, "expected selfRegService backing the handler")
	return handler, svc.store, svc.tenants
}

// TestGrantRoleIfMissing_Branches exercises every branch of grantRoleIfMissing:
// the user-not-found early return, the already-has-role idempotent return, and
// the successful append-and-persist path.
func TestGrantRoleIfMissing_Branches(t *testing.T) {
	ctx := context.Background()
	handler, store, tenants := grantRoleTestFixture(t)

	const tenantID = "grant-tenant"
	_, err := tenants.CreateTenant(ctx, iam.CreateTenantOptions{
		ID:   tenantID,
		Name: "Grant Tenant",
	})
	require.NoError(t, err)

	t.Run("user not found is a no-op", func(t *testing.T) {
		// Must not panic or error for a missing user.
		handler.grantRoleIfMissing(ctx, store, tenantID, "no-such-user", "admin")
	})

	user, err := store.CreateUser(ctx, tenantID, iam.CreateUserOptions{
		Username: "grantee",
		Email:    "grantee@example.com",
		Name:     "Grantee",
		Enabled:  true,
		Roles:    []string{"member"},
	})
	require.NoError(t, err)

	t.Run("grants a missing role and persists it", func(t *testing.T) {
		handler.grantRoleIfMissing(ctx, store, tenantID, user.ID, "admin")

		updated, err := store.GetUser(ctx, tenantID, user.ID)
		require.NoError(t, err)
		require.Contains(t, updated.Roles, "admin")
		require.Contains(t, updated.Roles, "member")
	})

	t.Run("already-present role is left unchanged", func(t *testing.T) {
		before, err := store.GetUser(ctx, tenantID, user.ID)
		require.NoError(t, err)

		handler.grantRoleIfMissing(ctx, store, tenantID, user.ID, "admin")

		after, err := store.GetUser(ctx, tenantID, user.ID)
		require.NoError(t, err)
		require.Equal(t, len(before.Roles), len(after.Roles), "role list should be unchanged when role already present")
	})
}
