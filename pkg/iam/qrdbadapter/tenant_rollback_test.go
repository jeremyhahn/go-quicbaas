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

package qrdbadapter

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// refusingPromptSeeder fails every tenant's prompt seed.
type refusingPromptSeeder struct{ err error }

func (s refusingPromptSeeder) SeedTenant(context.Context, string) error { return s.err }

func (f *failingRoleStorage) GetRole(context.Context, string, string) (*iam.Role, error) {
	return nil, f.err
}

// TestCreateTenant_PromptSeedFailureRemovesSeededRoles runs CreateTenant with
// the real role seeder and a prompt seeder that fails after it. The roles live
// in a store the tenant's deletion does not purge, as the engine's do, so only
// the rollback can remove them: none may remain for the refused tenant.
func TestCreateTenant_PromptSeedFailureRemovesSeededRoles(t *testing.T) {
	ctx := context.Background()
	roles := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{UserStorage: roles, RoleStorage: roles})
	mgr := iam.NewDefaultTenantManager(iam.NewMemoryStorage())
	mgr.SetRoleSeeder(WrapRole(store))
	boom := errors.New("prompt store sealed")
	mgr.SetPromptSeeder(refusingPromptSeeder{err: boom})

	_, err := mgr.CreateTenant(ctx, iam.CreateTenantOptions{ID: "burnin", Name: "Burn-in"})
	if !errors.Is(err, boom) {
		t.Fatalf("CreateTenant error = %v, want it to wrap the prompt seed cause", err)
	}
	left, err := store.ListRoles(ctx, "burnin", iam.ListOptions{})
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(left) != 0 {
		names := make([]string, 0, len(left))
		for _, role := range left {
			names = append(names, role.Name)
		}
		t.Fatalf("roles %v survived the refused tenant", names)
	}
}

// TestRoleAdapter_DeleteBuiltInRoles_RemovesEveryBuiltIn proves the undo of a
// seed removes the roles the seed created and is a no-op once they are gone.
func TestRoleAdapter_DeleteBuiltInRoles_RemovesEveryBuiltIn(t *testing.T) {
	ctx := context.Background()
	store := newStore()
	a := WrapRole(store)
	if err := a.SeedBuiltInRoles(ctx, "burnin"); err != nil {
		t.Fatalf("SeedBuiltInRoles: %v", err)
	}
	for range 2 {
		if err := a.DeleteBuiltInRoles(ctx, "burnin"); err != nil {
			t.Fatalf("DeleteBuiltInRoles: %v", err)
		}
		left, err := store.ListRoles(ctx, "burnin", iam.ListOptions{})
		if err != nil || len(left) != 0 {
			t.Fatalf("after DeleteBuiltInRoles: %d roles, %v", len(left), err)
		}
	}
}

// TestRoleAdapter_DeleteBuiltInRoles_Errors covers the refusals: no store,
// and a role store that cannot be read is reported naming the role.
func TestRoleAdapter_DeleteBuiltInRoles_Errors(t *testing.T) {
	ctx := context.Background()
	var nilAdapter *RoleAdapter
	var nilDep *NilDependencyError
	if err := nilAdapter.DeleteBuiltInRoles(ctx, "burnin"); !errors.As(err, &nilDep) {
		t.Fatalf("nil adapter: err = %v, want NilDependencyError", err)
	}

	boom := errors.New("role store unavailable")
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: iam.NewMemoryStorage(),
		RoleStorage: &failingRoleStorage{err: boom},
	})
	err := WrapRole(store).DeleteBuiltInRoles(ctx, "burnin")
	var seedErr *RoleSeedError
	if !errors.Is(err, boom) || !errors.As(err, &seedErr) {
		t.Fatalf("failing store: err = %v, want a RoleSeedError wrapping the cause", err)
	}
	if err := WrapRole(store).DeleteBuiltInRoles(ctx, ""); err != nil {
		t.Fatalf("empty tenant: err = %v, want a no-op", err)
	}
}
