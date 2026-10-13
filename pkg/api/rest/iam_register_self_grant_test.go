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
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// requireHoldsBuiltInRoles asserts that user holds exactly the tenant's
// built-in roles with the given names, by the role IDs the tenant carries.
// A role NAME in User.Roles fails it: Roles holds IDs, and a name resolves to
// no role.
func requireHoldsBuiltInRoles(t *testing.T, store *iam.IdentityStore, tenantID string, user *iam.User, names ...string) {
	t.Helper()
	want := make([]string, 0, len(names))
	for _, name := range names {
		role, err := store.GetRoleByName(context.Background(), tenantID, name)
		require.NoError(t, err, "tenant %s carries no built-in role %q", tenantID, name)
		want = append(want, role.ID)
	}
	assert.ElementsMatch(t, want, user.Roles,
		"the registrant must hold the role IDs of %v, not their names", names)
}

// selfRegFixture returns the handler with the store and tenant manager behind
// it, so a test can inspect the registrant or change how tenants are seeded.
func selfRegFixture(t *testing.T, cfg config.IAMSelfServiceRegistrationConfig) (*IAMHandler, *selfRegService) {
	t.Helper()
	handler := setupSelfRegHandler(t, cfg)
	svc, ok := handler.server.service.(*selfRegService)
	require.True(t, ok, "expected selfRegService backing the handler")
	return handler, svc
}

func TestRegisterSelfFinish_GrantsAdminAndDefaultRoleIDs(t *testing.T) {
	for _, tc := range []struct {
		name        string
		defaultRole string
		want        []string
	}{
		{"configured default role", "Auditor", []string{qrdbsdk.RoleNameAdmin, "auditor"}},
		{"default role is Admin", "Admin", []string{qrdbsdk.RoleNameAdmin}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, svc := selfRegFixture(t, config.IAMSelfServiceRegistrationConfig{
				Enabled:               true,
				DefaultRole:           tc.defaultRole,
				AllowedIsolationModes: []string{config.TenantIsolationKeyspace},
				DefaultIsolationMode:  config.TenantIsolationKeyspace,
			})

			sessionID := selfRegBegin(t, handler, "Grant Co", "grace@grant.example")
			w := selfRegFinish(t, handler, sessionID)
			require.Equal(t, http.StatusOK, w.Code, "finish body: %s", w.Body.String())

			user, err := svc.store.GetUserByEmail(context.Background(), "grant-co", "grace@grant.example")
			require.NoError(t, err)
			requireHoldsBuiltInRoles(t, svc.store, "grant-co", user, tc.want...)
		})
	}
}

func TestRegisterSelfFinish_RoleGrantFailureRefusesRegistration(t *testing.T) {
	handler, svc := selfRegFixture(t, config.IAMSelfServiceRegistrationConfig{
		Enabled:               true,
		AllowedIsolationModes: []string{config.TenantIsolationKeyspace},
		DefaultIsolationMode:  config.TenantIsolationKeyspace,
	})
	// A tenant created without its built-in roles: the Admin role cannot be
	// resolved, so the grant fails.
	svc.tenants.roleSeeder = nil

	sessionID := selfRegBegin(t, handler, "Bare Co", "bob@bare.example")
	w := selfRegFinish(t, handler, sessionID)

	require.Equal(t, http.StatusInternalServerError, w.Code, "finish body: %s", w.Body.String())
	assert.Empty(t, w.Header().Values("Set-Cookie"), "a refused registration must set no session cookie")

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.NotContains(t, body, "access_token", "a refused registration must mint no token")
	assert.NotContains(t, body, "refresh_token", "a refused registration must mint no token")

	_, err := svc.store.GetUserByEmail(context.Background(), "bare-co", "bob@bare.example")
	assert.True(t, iam.IsUserNotFound(err),
		"the account must be deleted when its roles cannot be granted, got %v", err)
}

func TestGrantBuiltInRole(t *testing.T) {
	ctx := context.Background()
	_, svc := selfRegFixture(t, config.IAMSelfServiceRegistrationConfig{Enabled: true})

	const tenantID = "grant-tenant"
	_, err := svc.tenants.CreateTenant(ctx, iam.CreateTenantOptions{ID: tenantID, Name: "Grant Tenant"})
	require.NoError(t, err)
	user, err := svc.store.CreateUser(ctx, tenantID, iam.CreateUserOptions{
		Username: "grantee",
		Email:    "grantee@example.com",
		Name:     "Grantee",
	})
	require.NoError(t, err)

	t.Run("re-granting a held role is a no-op success", func(t *testing.T) {
		require.NoError(t, grantBuiltInRole(ctx, svc.store, tenantID, user.ID, qrdbsdk.RoleNameAdmin))
		require.NoError(t, grantBuiltInRole(ctx, svc.store, tenantID, user.ID, qrdbsdk.RoleNameAdmin))

		got, err := svc.store.GetUser(ctx, tenantID, user.ID)
		require.NoError(t, err)
		requireHoldsBuiltInRoles(t, svc.store, tenantID, got, qrdbsdk.RoleNameAdmin)
	})

	t.Run("a role the tenant does not carry is a typed error", func(t *testing.T) {
		err := grantBuiltInRole(ctx, svc.store, tenantID, user.ID, "no-such-role")

		var grantErr *SelfRegistrationRoleGrantError
		require.True(t, errors.As(err, &grantErr), "got %v", err)
		assert.Equal(t, "no-such-role", grantErr.Role)
		assert.Equal(t, tenantID, grantErr.TenantID)
		assert.True(t, iam.IsRoleNotFound(err), "the store's cause must stay readable, got %v", err)
	})

	t.Run("an unknown user is a typed error", func(t *testing.T) {
		err := grantBuiltInRole(ctx, svc.store, tenantID, "no-such-user", qrdbsdk.RoleNameAdmin)

		var grantErr *SelfRegistrationRoleGrantError
		require.True(t, errors.As(err, &grantErr), "got %v", err)
		assert.Equal(t, qrdbsdk.RoleNameAdmin, grantErr.Role)
	})
}

func TestBuiltInRoleName(t *testing.T) {
	assert.Equal(t, qrdbsdk.RoleNameAdmin, builtInRoleName("Admin"))
	assert.Equal(t, "so", builtInRoleName("SO"))
	assert.Equal(t, "user", builtInRoleName("User"))
}
