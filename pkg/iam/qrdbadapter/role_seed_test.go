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
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestRoleAdapter_SeedBuiltInRoles_MaterializesEveryBuiltIn proves a tenant
// seeded through the adapter holds every canonical built-in role, under the
// deterministic ID qrdb derives, carrying the live permission projection.
// This is the defect in #269: `iam tenant create` produced a tenant with zero
// roles, so `iam user create --roles admin` failed with "role not found".
func TestRoleAdapter_SeedBuiltInRoles_MaterializesEveryBuiltIn(t *testing.T) {
	store := newStore()
	a := WrapRole(store)
	ctx := context.Background()

	if err := a.SeedBuiltInRoles(ctx, "burnin"); err != nil {
		t.Fatalf("SeedBuiltInRoles: %v", err)
	}

	want := qrdbsdk.BuiltInRoles("burnin")
	if len(want) == 0 {
		t.Fatal("qrdbsdk.BuiltInRoles returned no roles; the built-in set is the fixture under test")
	}
	for _, w := range want {
		got, err := store.GetRole(ctx, "burnin", w.ID)
		if err != nil {
			t.Fatalf("role %q (id %s) not seeded: %v", w.Name, w.ID, err)
		}
		if got.Name != w.Name {
			t.Fatalf("role %s name = %q, want %q", w.ID, got.Name, w.Name)
		}
		if got.TenantID != "burnin" {
			t.Fatalf("role %s tenant = %q, want burnin", w.ID, got.TenantID)
		}
		// A role with an empty permission set is seeded in name only: it
		// resolves, so user creation succeeds, and then confers nothing.
		if len(got.Permissions) == 0 {
			t.Fatalf("role %s (%s) seeded with an empty permission set", w.Name, w.ID)
		}
	}
}

// TestRoleAdapter_SeedBuiltInRoles_AdminIsGrantable proves the specific grant
// the burn-in needed — `--roles admin` — resolves after seeding.
func TestRoleAdapter_SeedBuiltInRoles_AdminIsGrantable(t *testing.T) {
	store := newStore()
	ctx := context.Background()

	if err := WrapRole(store).SeedBuiltInRoles(ctx, "burnin"); err != nil {
		t.Fatalf("SeedBuiltInRoles: %v", err)
	}

	roles, err := store.ListRoles(ctx, "burnin", iam.ListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	var admin *iam.Role
	for _, r := range roles {
		if r.Name == qrdbsdk.RoleNameAdmin {
			admin = r
			break
		}
	}
	if admin == nil {
		t.Fatalf("no role named %q among %d seeded roles", qrdbsdk.RoleNameAdmin, len(roles))
	}
	if admin.ID != qrdbsdk.SystemRoleID("burnin", qrdbsdk.RoleNameAdmin) {
		t.Fatalf("admin role ID = %q, want the deterministic system role ID %q",
			admin.ID, qrdbsdk.SystemRoleID("burnin", qrdbsdk.RoleNameAdmin))
	}
}

// TestRoleAdapter_SeedBuiltInRoles_Idempotent proves a second seed over an
// already-seeded tenant succeeds and adds nothing, so a re-run (or a tenant
// seeded by an earlier version) converges instead of failing on a duplicate.
func TestRoleAdapter_SeedBuiltInRoles_Idempotent(t *testing.T) {
	store := newStore()
	a := WrapRole(store)
	ctx := context.Background()

	if err := a.SeedBuiltInRoles(ctx, "burnin"); err != nil {
		t.Fatalf("first SeedBuiltInRoles: %v", err)
	}
	first, err := store.ListRoles(ctx, "burnin", iam.ListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if err := a.SeedBuiltInRoles(ctx, "burnin"); err != nil {
		t.Fatalf("second SeedBuiltInRoles: %v", err)
	}
	second, err := store.ListRoles(ctx, "burnin", iam.ListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("role count changed on re-seed: %d -> %d", len(first), len(second))
	}
}

// TestRoleAdapter_SeedBuiltInRoles_SkipsSystemTenant proves seeding leaves the
// system tenant untouched. This is not tidiness: first-boot creates the system
// tenant through the same tenant manager and then registers the built-in roles
// itself, deciding how to resolve the platform admin's role ID from whether it
// found them already present. Seeding them here would make first-boot see its
// own roles as preexisting and — with no role.NameResolver wired anywhere in
// quicbaas — fail admin creation with UnresolvableSystemRoleError, breaking
// bootstrap on a fresh install.
func TestRoleAdapter_SeedBuiltInRoles_SkipsSystemTenant(t *testing.T) {
	store := newStore()
	ctx := context.Background()

	if err := WrapRole(store).SeedBuiltInRoles(ctx, qrdbsdk.SystemTenantID); err != nil {
		t.Fatalf("SeedBuiltInRoles(system): %v", err)
	}

	roles, err := store.ListRoles(ctx, qrdbsdk.SystemTenantID, iam.ListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles) != 0 {
		t.Fatalf("seeded %d roles into the system tenant; first-boot owns them", len(roles))
	}
}

// TestRoleAdapter_SeedBuiltInRoles_SkipsEmptyTenant proves an empty tenant ID
// seeds nothing. qrdbsdk.BuiltInRoles defaults an empty tenant to the system
// tenant, so seeding it would write system-tenant roles under a caller's bug.
func TestRoleAdapter_SeedBuiltInRoles_SkipsEmptyTenant(t *testing.T) {
	store := newStore()
	ctx := context.Background()

	if err := WrapRole(store).SeedBuiltInRoles(ctx, ""); err != nil {
		t.Fatalf("SeedBuiltInRoles(\"\"): %v", err)
	}

	roles, err := store.ListRoles(ctx, qrdbsdk.SystemTenantID, iam.ListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles) != 0 {
		t.Fatalf("empty tenant ID seeded %d system-tenant roles", len(roles))
	}
}

// TestRoleAdapter_SeedBuiltInRoles_NilStore proves the zero adapter reports a
// typed dependency error rather than panicking.
func TestRoleAdapter_SeedBuiltInRoles_NilStore(t *testing.T) {
	var a *RoleAdapter
	err := a.SeedBuiltInRoles(context.Background(), "burnin")
	var nilDep *NilDependencyError
	if !errors.As(err, &nilDep) {
		t.Fatalf("SeedBuiltInRoles on nil adapter = %v, want *NilDependencyError", err)
	}
}

// TestRoleAdapter_SeedBuiltInRoles_NamesFailingRole proves a store failure is
// reported as a RoleSeedError naming the role and tenant, and still unwraps to
// the cause. Seeding walks the whole set, so the bare cause is ambiguous.
func TestRoleAdapter_SeedBuiltInRoles_NamesFailingRole(t *testing.T) {
	boom := errors.New("role store unavailable")
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  iam.NewMemoryStorage(),
		GroupStorage: iam.NewMemoryStorage(),
		RoleStorage:  &failingRoleStorage{err: boom},
	})

	err := WrapRole(store).SeedBuiltInRoles(context.Background(), "burnin")
	var seedErr *RoleSeedError
	if !errors.As(err, &seedErr) {
		t.Fatalf("SeedBuiltInRoles = %v, want *RoleSeedError", err)
	}
	if seedErr.TenantID != "burnin" {
		t.Fatalf("RoleSeedError.TenantID = %q, want burnin", seedErr.TenantID)
	}
	if seedErr.Role == "" {
		t.Fatal("RoleSeedError.Role is empty; the failing role must be named")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("RoleSeedError does not unwrap to the store cause: %v", err)
	}
}

// failingRoleStorage fails every create so the seed loop's error path is
// exercised; the remaining RoleStorage methods are unreachable in this test.
type failingRoleStorage struct {
	iam.RoleStorage
	err error
}

func (f *failingRoleStorage) CreateRole(context.Context, *iam.Role) error { return f.err }

// TestRoleAdapter_SeedBuiltInRoles_RefusesImpostorID proves seeding fails
// closed when a role already carries a built-in NAME under a non-canonical
// identifier.
//
// qrdb does not purge role records when a tenant is deleted, so recreating a
// tenant can meet a leftover `admin` from its previous life. Treating a name
// match as idempotent success would adopt it, and every later
// `--roles admin` user would receive whatever that role happens to hold under
// a name that promises qrdb's canonical grants.
func TestRoleAdapter_SeedBuiltInRoles_RefusesImpostorID(t *testing.T) {
	store := newStore()
	a := WrapRole(store)
	ctx := context.Background()

	// A leftover role wearing the built-in name, with its own id.
	if _, err := store.CreateRole(ctx, "burnin", iam.CreateRoleOptions{
		ID:          "leftover-id-from-a-previous-tenant",
		Name:        qrdbsdk.RoleNameAdmin,
		Permissions: []string{"keys:read"},
	}); err != nil {
		t.Fatalf("seed leftover role: %v", err)
	}

	err := a.SeedBuiltInRoles(ctx, "burnin")

	var conflict *RoleConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("SeedBuiltInRoles = %v, want *RoleConflictError", err)
	}
	if conflict.Role != qrdbsdk.RoleNameAdmin {
		t.Errorf("RoleConflictError.Role = %q, want %q", conflict.Role, qrdbsdk.RoleNameAdmin)
	}
}

// TestRoleAdapter_SeedBuiltInRoles_RefusesAlteredPermissions proves a role with
// the canonical name AND id but a tampered permission set is refused too.
// Matching the identifier is not evidence the grants are canonical.
func TestRoleAdapter_SeedBuiltInRoles_RefusesAlteredPermissions(t *testing.T) {
	store := newStore()
	a := WrapRole(store)
	ctx := context.Background()

	if _, err := store.CreateRole(ctx, "burnin", iam.CreateRoleOptions{
		ID:          qrdbsdk.SystemRoleID("burnin", qrdbsdk.RoleNameAdmin),
		Name:        qrdbsdk.RoleNameAdmin,
		Permissions: []string{"keys:read", "an:extra-grant-nobody-asked-for"},
	}); err != nil {
		t.Fatalf("seed tampered role: %v", err)
	}

	err := a.SeedBuiltInRoles(ctx, "burnin")

	var conflict *RoleConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("SeedBuiltInRoles = %v, want *RoleConflictError", err)
	}
	if !strings.Contains(conflict.Reason, "permissions") {
		t.Errorf("RoleConflictError.Reason = %q, want it to name the permission mismatch", conflict.Reason)
	}
}

// TestRoleAdapter_SeedBuiltInRoles_AcceptsCanonicalDuplicate proves the
// genuine idempotent case still converges: a role already present under the
// canonical id AND the canonical permission set is accepted, not refused.
// Verification must not turn a legitimate re-run into a failure.
func TestRoleAdapter_SeedBuiltInRoles_AcceptsCanonicalDuplicate(t *testing.T) {
	store := newStore()
	a := WrapRole(store)
	ctx := context.Background()

	if err := a.SeedBuiltInRoles(ctx, "burnin"); err != nil {
		t.Fatalf("first SeedBuiltInRoles: %v", err)
	}
	if err := a.SeedBuiltInRoles(ctx, "burnin"); err != nil {
		t.Fatalf("re-seeding a canonically seeded tenant failed: %v", err)
	}
}

// TestRoleAdapter_SeedBuiltInRoles_PartialSeedRollsBack proves a seed that
// fails part-way removes the roles it created.
//
// Otherwise a failed attempt leaves orphans behind, and the NEXT attempt meets
// its own debris — which the conflict check would then, correctly but
// unhelpfully, refuse forever.
func TestRoleAdapter_SeedBuiltInRoles_PartialSeedRollsBack(t *testing.T) {
	mem := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  mem,
		GroupStorage: mem,
		RoleStorage:  &failAfterNRoleStorage{RoleStorage: mem, allow: 2, err: errors.New("role store went away")},
	})
	ctx := context.Background()

	if err := WrapRole(store).SeedBuiltInRoles(ctx, "burnin"); err == nil {
		t.Fatal("SeedBuiltInRoles succeeded despite the store failing part-way")
	}

	roles, err := mem.ListRoles(ctx, "burnin", iam.ListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles) != 0 {
		names := make([]string, 0, len(roles))
		for _, r := range roles {
			names = append(names, r.Name)
		}
		t.Errorf("a failed seed left %d role(s) behind: %v", len(roles), names)
	}
}

// failAfterNRoleStorage lets the first `allow` creates through and fails the
// rest, so a seed breaks mid-way with real roles already written.
type failAfterNRoleStorage struct {
	iam.RoleStorage
	allow int
	n     int
	err   error
}

func (f *failAfterNRoleStorage) CreateRole(ctx context.Context, r *iam.Role) error {
	if f.n >= f.allow {
		return f.err
	}
	f.n++
	return f.RoleStorage.CreateRole(ctx, r)
}

// TestSamePermissions honours its stated contract: same SET means equal,
// whatever the order and however often an entry repeats.
//
// Consuming one side against the other as it walks would make a repeated entry
// read as a mismatch, refusing a role whose grants are in fact canonical — a
// conflict no operator could resolve, because the two sets are equal.
func TestSamePermissions(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{"identical", []string{"keys:read", "keys:write"}, []string{"keys:read", "keys:write"}, true},
		{"reordered", []string{"keys:read", "keys:write"}, []string{"keys:write", "keys:read"}, true},
		{"duplicate on the right", []string{"keys:read"}, []string{"keys:read", "keys:read"}, true},
		{"duplicate on the left", []string{"keys:read", "keys:read"}, []string{"keys:read"}, true},
		{"both empty", nil, nil, true},
		{"missing on the right", []string{"keys:read", "keys:write"}, []string{"keys:read"}, false},
		{"extra on the right", []string{"keys:read"}, []string{"keys:read", "keys:write"}, false},
		{"one empty", []string{"keys:read"}, nil, false},
		{"disjoint", []string{"keys:read"}, []string{"keys:write"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := samePermissions(tc.a, tc.b); got != tc.want {
				t.Errorf("samePermissions(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
