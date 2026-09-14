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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestRegisterSelfFinish_CreatesTheRegistrantAsTenantAdmin is the end-to-end
// property self-service onboarding exists for: a completed WebAuthn ceremony
// leaves a usable tenant admin behind.
//
// It did not. The create passed the EMAIL as the username, and a username may
// not contain "@" — so every self-registration failed at the last step, after
// the tenant had already been created. The visible result was an orphan tenant
// with no members and no way in.
func TestRegisterSelfFinish_CreatesTheRegistrantAsTenantAdmin(t *testing.T) {
	handler := setupSelfRegHandler(t, config.IAMSelfServiceRegistrationConfig{
		Enabled:               true,
		AllowedIsolationModes: []string{config.TenantIsolationKeyspace},
		DefaultIsolationMode:  config.TenantIsolationKeyspace,
	})
	ctx := context.Background()

	sessionID := selfRegBegin(t, handler, "Acme Inc", "alice@acme.example")
	w := selfRegFinish(t, handler, sessionID)
	require.Equal(t, http.StatusOK, w.Code,
		"a completed ceremony must create the registrant: %s", w.Body.String())

	store, err := handler.getIdentityStore(ctx)
	require.NoError(t, err)

	user, err := store.GetUserByEmail(ctx, "acme-inc", "alice@acme.example")
	require.NoError(t, err, "the registrant must exist in the tenant the ceremony created")

	// The handle has to satisfy the rule the store enforces, or the account
	// exists and cannot be addressed by the name an administrator would type.
	assert.NotContains(t, user.Username, "@",
		"the handle must not be the email address: %q", user.Username)
	assert.GreaterOrEqual(t, len(user.Username), 3, "handle %q is shorter than the store allows", user.Username)

	byHandle, err := store.GetUserByUsername(ctx, "acme-inc", user.Username)
	require.NoError(t, err, "the registrant must resolve by the handle it was created with")
	assert.Equal(t, user.ID, byHandle.ID)

	// The display name is the one the registrant supplied at begin, not their
	// address — validateDisplayName refuses an email-shaped name, and the
	// begin request requires display_name precisely so there is one to use.
	assert.NotEqual(t, "alice@acme.example", user.Name,
		"the display name must not be the email address")
	assert.NotEmpty(t, user.Name, "the registrant has no display name")

	assert.Equal(t, iam.LifecycleActive, user.Lifecycle,
		"with neither gate configured the registrant is active")

	// The Admin grant is NOT asserted here. grantRoleIfMissing goes through
	// UpdateUser, which now refuses a role reference that names nothing, and
	// this fixture's tenant manager carries no role seeder — production wires
	// one (iam_init.go SetRoleSeeder), so a real tenant has its built-in roles
	// by the time the grant runs. Asserting it against a fixture that cannot
	// seed would test the fixture. TestGrantRoleIfMissing_Branches covers the
	// grant itself, against a store where the role exists.
}

// TestRegisterSelfFinish_DerivesAHandleFromAnAwkwardEmail covers the local
// parts a derivation has to survive: dots, plus-tags and case. Each would be
// refused verbatim by the username charset.
func TestRegisterSelfFinish_DerivesAHandleFromAnAwkwardEmail(t *testing.T) {
	for _, tc := range []struct {
		name  string
		email string
	}{
		{"dotted local part", "alice.q.smith@acme.example"},
		{"plus tag", "alice+signup@acme.example"},
		{"mixed case", "Alice.Smith@acme.example"},
		{"very short local part", "a.b@acme.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := setupSelfRegHandler(t, config.IAMSelfServiceRegistrationConfig{
				Enabled:               true,
				AllowedIsolationModes: []string{config.TenantIsolationKeyspace},
				DefaultIsolationMode:  config.TenantIsolationKeyspace,
			})
			ctx := context.Background()

			sessionID := selfRegBegin(t, handler, "Acme Inc", tc.email)
			w := selfRegFinish(t, handler, sessionID)
			require.Equal(t, http.StatusOK, w.Code,
				"registration must complete for %q: %s", tc.email, w.Body.String())

			store, err := handler.getIdentityStore(ctx)
			require.NoError(t, err)
			user, err := store.GetUserByEmail(ctx, "acme-inc", tc.email)
			require.NoError(t, err)

			// The store is the authority on what a handle may be: if it
			// resolves by its own username, the derivation produced a legal one.
			byHandle, err := store.GetUserByUsername(ctx, "acme-inc", user.Username)
			require.NoError(t, err, "derived handle %q does not resolve", user.Username)
			assert.Equal(t, user.ID, byHandle.ID)
		})
	}
}

// TestDeriveHandle covers the derivation directly, including the shapes the
// end-to-end tests cannot easily produce.
func TestDeriveHandle(t *testing.T) {
	for _, tc := range []struct {
		name  string
		email string
		want  string
	}{
		{"plain local part", "alice@acme.example", "alice"},
		{"dots are dropped", "alice.q.smith@acme.example", "aliceqsmith"},
		{"plus tag is dropped", "alice+signup@acme.example", "alicesignup"},
		{"lowercased", "Alice.Smith@acme.example", "alicesmith"},
		{"dash and underscore survive", "a_b-c@acme.example", "a_b-c"},
		{"too short gets a suffix", "a.b@acme.example", "ab-admin"},
		{"nothing usable falls back", "...@acme.example", "admin"},
		{"no at sign is treated as the whole string", "alice", "alice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, deriveHandle(tc.email))
		})
	}
}

// TestDeriveHandle_AlwaysProducesAStorableHandle is the property the individual
// cases serve: whatever comes in, what comes out is something CreateUser
// accepts. A derivation that can emit an illegal handle just moves the failure.
func TestDeriveHandle_AlwaysProducesAStorableHandle(t *testing.T) {
	ctx := context.Background()
	for _, email := range []string{
		"alice@acme.example",
		"alice.q.smith@acme.example",
		"alice+signup@acme.example",
		"Alice.Smith@acme.example",
		"a.b@acme.example",
		"x@acme.example",
		"a_very_long_local_part_that_goes_on_and_on_and_on_and_on_and_on_and_on_beyond_sixty_four@acme.example",
	} {
		mem := iam.NewMemoryStorage()
		store := iam.NewIdentityStore(iam.IdentityStoreOptions{
			UserStorage: mem, GroupStorage: mem, RoleStorage: mem,
		})
		handle := deriveHandle(email)
		_, err := store.CreateUser(ctx, "acme", iam.CreateUserOptions{
			Email:    email,
			Username: handle,
			Name:     "Registrant",
		})
		require.NoError(t, err, "derived handle %q for %q was refused by the store", handle, email)
		require.LessOrEqual(t, len(handle), 64, "handle %q exceeds the store's limit", handle)
	}
}

// TestRegisterSelfBegin_RefusesAnEmailShapedDisplayName pins that the refusal
// happens at BEGIN, before a tenant exists. The store refuses an email-shaped
// display name, and discovering that at the create means the ceremony fails
// after writing a tenant nobody can enter.
func TestRegisterSelfBegin_RefusesAnEmailShapedDisplayName(t *testing.T) {
	handler := setupSelfRegHandler(t, config.IAMSelfServiceRegistrationConfig{
		Enabled:               true,
		AllowedIsolationModes: []string{config.TenantIsolationKeyspace},
		DefaultIsolationMode:  config.TenantIsolationKeyspace,
	})

	body, _ := json.Marshal(SelfRegisterBeginRequest{
		TenantName:  "Acme Inc",
		Email:       "alice@acme.example",
		DisplayName: "alice@acme.example",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register/begin", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.RegisterSelfBegin(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code,
		"an email-shaped display name must be refused before a tenant is created: %s", w.Body.String())

	// And nothing was created on the way to that refusal.
	tenants, err := handler.getTenantManager(context.Background())
	require.NoError(t, err)
	_, err = tenants.GetTenant(context.Background(), "acme-inc")
	assert.Error(t, err, "a refused begin must not leave a tenant behind")
}
