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

package iam

import (
	"context"
	"testing"
)

func newImportEnabledFixture(t *testing.T) (*BatchManager, *IdentityStore, *MemoryStorage) {
	t.Helper()
	mem := NewMemoryStorage()
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  mem,
		GroupStorage: mem,
		RoleStorage:  mem,
	})
	return NewBatchManager(BatchManagerOptions{IdentityStore: store}), store, mem
}

// TestImportUsers_PreservesDisabled is the fail-open direction: an export of a
// disabled account must not come back enabled.
//
// This never worked. Before CreateUser honoured the option the value was
// dropped inside the store; the import passed `Enabled: false` and the store
// ignored it. So the round trip re-enabled every disabled account it carried,
// and the code read as though it did not.
func TestImportUsers_PreservesDisabled(t *testing.T) {
	bm, _, mem := newImportEnabledFixture(t)
	ctx := context.Background()

	if _, err := bm.ImportUsers(ctx, "acme", []*UserImport{{
		Email:    "disabled@example.com",
		Username: "disabled",
		Enabled:  boolPtr(false),
	}}, BatchImportOptions{}); err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}

	user, err := mem.GetUserByEmail(ctx, "acme", "disabled@example.com")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if user.CanAuthenticate() {
		t.Error("an import that said Enabled=false produced an enabled user")
	}
}

// TestImportUsers_UnspecifiedEnabledLeavesUserEnabled is the other direction,
// and the reason the field is a pointer rather than a bool. An import record
// that says nothing about Enabled must not suspend anyone: as a plain bool,
// "said nothing" and "said disabled" were the same value.
func TestImportUsers_UnspecifiedEnabledLeavesUserEnabled(t *testing.T) {
	bm, _, mem := newImportEnabledFixture(t)
	ctx := context.Background()

	if _, err := bm.ImportUsers(ctx, "acme", []*UserImport{{
		Email:    "quiet@example.com",
		Username: "quiet",
	}}, BatchImportOptions{}); err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}

	user, err := mem.GetUserByEmail(ctx, "acme", "quiet@example.com")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	// Saying nothing must not DISABLE anyone. The account lands in pending —
	// waiting for its owner to activate — which is a different thing from an
	// administrator having turned it off, and the distinction is exactly what
	// the old single bool could not express.
	if user.Lifecycle != LifecyclePending {
		t.Errorf("Lifecycle = %q, want pending: an import that said nothing must not disable anyone", user.Lifecycle)
	}
}

// TestImportUsers_UpdateWithoutEnabledDoesNotSuspend covers the update path,
// where the same defect was already live and did not need CreateUser to start
// honouring anything: `updateOpts.Enabled = &user.Enabled` over a plain bool is
// never nil, so refreshing an existing user's name through an import record
// that did not mention Enabled SUSPENDED them.
func TestImportUsers_UpdateWithoutEnabledDoesNotSuspend(t *testing.T) {
	bm, store, mem := newImportEnabledFixture(t)
	ctx := context.Background()

	if _, err := store.CreateUser(ctx, "acme", CreateUserOptions{
		Email:    "alice@example.com",
		Username: "alice",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := bm.ImportUsers(ctx, "acme", []*UserImport{{
		Email:    "alice@example.com",
		Username: "alice",
		Name:     "Alice Updated",
	}}, BatchImportOptions{UpdateExisting: true}); err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}

	user, err := mem.GetUserByEmail(ctx, "acme", "alice@example.com")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if user.Lifecycle == LifecycleDisabled {
		t.Error("re-importing a user without mentioning Enabled disabled them")
	}
	if user.Name != "Alice Updated" {
		t.Errorf("Name = %q, want the updated value — the import must still have applied", user.Name)
	}
}

// TestImportUsers_UpdateHonoursExplicitDisable proves the update path still
// disables when the import actually says so, so the fix above did not buy
// safety by making the field inert.
func TestImportUsers_UpdateHonoursExplicitDisable(t *testing.T) {
	bm, store, mem := newImportEnabledFixture(t)
	ctx := context.Background()

	if _, err := store.CreateUser(ctx, "acme", CreateUserOptions{
		Email:    "bob@example.com",
		Username: "bob",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := bm.ImportUsers(ctx, "acme", []*UserImport{{
		Email:    "bob@example.com",
		Username: "bob",
		Enabled:  boolPtr(false),
	}}, BatchImportOptions{UpdateExisting: true}); err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}

	user, err := mem.GetUserByEmail(ctx, "acme", "bob@example.com")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if user.CanAuthenticate() {
		t.Error("an import that explicitly said Enabled=false did not suspend the user")
	}
}

// TestExportImportRoundTrip_PreservesDisabled is the property the other tests
// serve, stated end to end over the two functions an operator actually calls.
func TestExportImportRoundTrip_PreservesDisabled(t *testing.T) {
	bm, _, mem := newImportEnabledFixture(t)
	ctx := context.Background()

	export := &BatchExportResult{
		TenantID: "source",
		Users: []*User{
			{ID: "u1", Email: "live@example.com", Username: "live", Name: "live", Lifecycle: LifecycleActive},
			// Pending is the case an Enabled-derived bool cannot carry: it is
			// non-authenticating, so it exports as Enabled=false, and if that
			// shorthand overruled the explicit state the account would come back
			// DISABLED — an administrator's decision invented by a round trip.
			{ID: "u4", Email: "pending@example.com", Username: "pending", Name: "pending", Lifecycle: LifecyclePending},
			{ID: "u2", Email: "susp@example.com", Username: "susp", Name: "susp", Lifecycle: LifecycleSuspended},
			{ID: "u3", Email: "lock@example.com", Username: "lock", Name: "lock", Lifecycle: LifecycleLocked},
		},
	}
	req := export.ConvertToImportRequest("acme")

	if _, err := bm.ImportUsers(ctx, "acme", req.Users, BatchImportOptions{}); err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}

	for _, tc := range []struct {
		email         string
		wantLifecycle Lifecycle
	}{
		{"live@example.com", LifecycleActive},
		{"pending@example.com", LifecyclePending},
		// Neither suspended nor locked is creatable, and both are reachable only
		// FROM active, so an import cannot reconstruct them without opening a
		// window in which the account authenticates. They land on disabled.
		{"susp@example.com", LifecycleDisabled},
		{"lock@example.com", LifecycleDisabled},
	} {
		user, err := mem.GetUserByEmail(ctx, "acme", tc.email)
		if err != nil {
			t.Fatalf("read back %s: %v", tc.email, err)
		}
		if user.Lifecycle != tc.wantLifecycle {
			t.Errorf("%s: Lifecycle = %q, want %q after an export/import round trip",
				tc.email, user.Lifecycle, tc.wantLifecycle)
		}
		if want := tc.wantLifecycle == LifecycleActive; user.CanAuthenticate() != want {
			t.Errorf("%s: CanAuthenticate = %v, want %v", tc.email, user.CanAuthenticate(), want)
		}
	}
}

// TestImportUsers_UpdateExistingPreservesTheExactLifecycle.
//
// The update path applied only Enabled, so a re-imported PENDING account — which
// exports as pending + Enabled=false — became disabled. Enabled is one bool over
// six states; it must not overrule a caller who named the state, on the update
// path any more than on the create path.
func TestImportUsers_UpdateExistingPreservesTheExactLifecycle(t *testing.T) {
	for _, state := range []Lifecycle{LifecyclePending, LifecycleActive, LifecycleDisabled} {
		t.Run(string(state), func(t *testing.T) {
			bm, is, mem := newImportEnabledFixture(t)
			ctx := context.Background()

			if _, err := is.CreateUser(ctx, "acme", CreateUserOptions{
				Email: "existing@example.com", Username: "existing",
			}); err != nil {
				t.Fatalf("seed: %v", err)
			}

			enabled := state == LifecycleActive
			if _, err := bm.ImportUsers(ctx, "acme", []*UserImport{{
				Email:     "existing@example.com",
				Username:  "existing",
				Name:      "Existing Updated",
				Lifecycle: state,
				Enabled:   &enabled,
			}}, BatchImportOptions{UpdateExisting: true}); err != nil {
				t.Fatalf("ImportUsers: %v", err)
			}

			user, err := mem.GetUserByEmail(ctx, "acme", "existing@example.com")
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if user.Lifecycle != state {
				t.Errorf("Lifecycle = %q, want the imported %q", user.Lifecycle, state)
			}
			if user.Name != "Existing Updated" {
				t.Errorf("Name = %q, want the update to have applied too", user.Name)
			}
		})
	}
}
