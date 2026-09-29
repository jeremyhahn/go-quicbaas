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
	"strconv"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// go-quicbaas #541 — RoleAdapter is qrdb's role.NameResolver.
//
// first-boot is idempotent, so a re-run over an already-seeded system tenant
// finds the built-in roles present and must bind the admin's grant to the
// identifier they are ACTUALLY persisted under. With no resolver it refuses to
// guess (UnresolvableSystemRoleError); with one that lies about absence it
// would bind a DERIVED identifier that may name no role at all.
// =============================================================================

// TestRoleAdapter_GetRoleByName_Hit resolves a stored role, projecting the
// broker record onto qrdb's canonical role core.
func TestRoleAdapter_GetRoleByName_Hit(t *testing.T) {
	store := newStore()
	a := WrapRole(store)
	ctx := context.Background()

	if _, err := store.CreateRole(ctx, "burnin", iam.CreateRoleOptions{
		ID:          "role-1",
		Name:        "auditor",
		Permissions: []string{"kv:read"},
	}); err != nil {
		t.Fatalf("seed role: %v", err)
	}

	got, err := a.GetRoleByName(ctx, "burnin", "auditor")
	if err != nil {
		t.Fatalf("GetRoleByName: %v", err)
	}
	if got == nil {
		t.Fatal("GetRoleByName returned no role and no error")
	}
	if got.ID != "role-1" {
		t.Errorf("id = %q, want role-1", got.ID)
	}
	if got.Name != "auditor" {
		t.Errorf("name = %q, want auditor", got.Name)
	}
	if got.TenantID != "burnin" {
		t.Errorf("tenant = %q, want burnin", got.TenantID)
	}
	if len(got.Permissions) != 1 || got.Permissions[0].String() != "kv:read" {
		t.Errorf("permissions = %v, want [kv:read]", got.Permissions)
	}
}

// TestRoleAdapter_GetRoleByName_NotFound reports an absent role as qrdb's OWN
// *RoleNotFoundError. The exact type is load-bearing: ResolveSystemRoleID
// matches it with errors.As to fall back to the derived identifier, and reads
// every other error as a failure to resolve.
func TestRoleAdapter_GetRoleByName_NotFound(t *testing.T) {
	a := WrapRole(newStore())

	got, err := a.GetRoleByName(context.Background(), "burnin", "nosuchrole")
	if got != nil {
		t.Errorf("got role %+v, want nil", got)
	}
	var notFound *qrdbsdk.RoleNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("err = %v (%T), want *qrdbsdk.RoleNotFoundError", err, err)
	}
	if notFound.ID != "nosuchrole" {
		t.Errorf("RoleNotFoundError.ID = %q, want the name asked for", notFound.ID)
	}
}

// TestRoleAdapter_GetRoleByName_BadInput fails closed on a nil store and on an
// unnamed role, and does NOT dress either up as an absent role — an absence
// sends first-boot down the derived-identifier path.
func TestRoleAdapter_GetRoleByName_BadInput(t *testing.T) {
	ctx := context.Background()

	var nilAdapter *RoleAdapter
	if _, err := nilAdapter.GetRoleByName(ctx, "burnin", "auditor"); err == nil {
		t.Fatal("nil adapter: want an error")
	} else {
		var dep *NilDependencyError
		if !errors.As(err, &dep) {
			t.Errorf("nil adapter: err = %v (%T), want *NilDependencyError", err, err)
		}
	}

	if _, err := (&RoleAdapter{}).GetRoleByName(ctx, "burnin", "auditor"); err == nil {
		t.Fatal("adapter without a store: want an error")
	}

	a := WrapRole(newStore())
	for _, tc := range []struct{ tenantID, name string }{
		{"", "auditor"},
		{"burnin", ""},
	} {
		_, err := a.GetRoleByName(ctx, tc.tenantID, tc.name)
		if err == nil {
			t.Fatalf("tenant %q name %q: want a validation error", tc.tenantID, tc.name)
		}
		var notFound *qrdbsdk.RoleNotFoundError
		if errors.As(err, &notFound) {
			t.Errorf("tenant %q name %q: invalid input reported as an absent role", tc.tenantID, tc.name)
		}
	}
}

// TestRoleAdapter_ResolveSystemRoleID_UsesPersistedID is the end the whole
// change serves: qrdb's ResolveSystemRoleID, handed this adapter, answers with
// the identifier the role is stored under rather than the derived one.
func TestRoleAdapter_ResolveSystemRoleID_UsesPersistedID(t *testing.T) {
	store := newStore()
	a := WrapRole(store)
	ctx := context.Background()

	// A cluster seeded before system role IDs moved off UUIDv5 keeps its roles
	// under legacy identifiers; the derived one names nothing.
	const legacyID = "legacy-uuidv5-identifier"
	if _, err := store.CreateRole(ctx, qrdbsdk.SystemTenantID, iam.CreateRoleOptions{
		ID:          legacyID,
		Name:        qrdbsdk.RoleNameAdmin,
		Permissions: []string{"kv:read"},
	}); err != nil {
		t.Fatalf("seed legacy role: %v", err)
	}

	derived := qrdbsdk.SystemRoleID(qrdbsdk.SystemTenantID, qrdbsdk.RoleNameAdmin)
	if derived == legacyID {
		t.Fatal("fixture is void: the derived identifier must differ from the legacy one")
	}

	resolved, err := qrdbsdk.ResolveSystemRoleID(ctx, a, qrdbsdk.SystemTenantID, qrdbsdk.RoleNameAdmin)
	if err != nil {
		t.Fatalf("ResolveSystemRoleID: %v", err)
	}
	if resolved != legacyID {
		t.Errorf("resolved = %q, want the persisted %q (a grant against %q would confer nothing)", resolved, legacyID, derived)
	}
}

// TestRoleAdapter_ResolveSystemRoleID_FallsBackWhenUnseeded keeps the other
// half of the contract: when the tenant holds no role of that name, the derived
// identifier is the answer — that is the FIRST run, where first-boot is about
// to create the role under exactly that identifier.
func TestRoleAdapter_ResolveSystemRoleID_FallsBackWhenUnseeded(t *testing.T) {
	ctx := context.Background()
	a := WrapRole(newStore())

	resolved, err := qrdbsdk.ResolveSystemRoleID(ctx, a, qrdbsdk.SystemTenantID, qrdbsdk.RoleNameAdmin)
	if err != nil {
		t.Fatalf("ResolveSystemRoleID: %v", err)
	}
	want := qrdbsdk.SystemRoleID(qrdbsdk.SystemTenantID, qrdbsdk.RoleNameAdmin)
	if resolved != want {
		t.Errorf("resolved = %q, want the derived %q", resolved, want)
	}
}

// TestRoleAdapter_ResolveSystemRoleID_FailsClosedOnStoreError proves a store
// outage does NOT silently become the derived identifier. This is the reason
// the adapter must not flatten every error into not-found.
func TestRoleAdapter_ResolveSystemRoleID_FailsClosedOnStoreError(t *testing.T) {
	ctx := context.Background()
	outage := errors.New("shard offline")
	mem := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: mem,
		RoleStorage: &lookupFailingRoleStorage{RoleStorage: mem, err: outage},
	})
	a := WrapRole(store)

	resolved, err := qrdbsdk.ResolveSystemRoleID(ctx, a, qrdbsdk.SystemTenantID, qrdbsdk.RoleNameAdmin)
	if err == nil {
		t.Fatalf("ResolveSystemRoleID returned %q, want the store error", resolved)
	}
	if !errors.Is(err, outage) {
		t.Errorf("err = %v, want the store outage", err)
	}
}

// lookupFailingRoleStorage fails every by-name lookup, delegating the rest.
type lookupFailingRoleStorage struct {
	iam.RoleStorage
	err error
}

func (f *lookupFailingRoleStorage) GetRoleByName(context.Context, string, string) (*iam.Role, error) {
	return nil, f.err
}

// TestRoleAdapter_SeedBuiltInRoles_VerifiesByName proves the seed's
// already-exists verification resolves the incumbent by NAME. It used to list
// the tenant's roles and scan the page, so a tenant holding more roles than one
// page could report the name as taken and then not show the role — refusing a
// seed whose incumbent is canonical and present.
func TestRoleAdapter_SeedBuiltInRoles_VerifiesByName(t *testing.T) {
	store := newStore()
	a := WrapRole(store)
	ctx := context.Background()

	// Seed once, then bulk the tenant out well past any single page.
	if err := a.SeedBuiltInRoles(ctx, "burnin"); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	for i := 0; i < 250; i++ {
		if _, err := store.CreateRole(ctx, "burnin", iam.CreateRoleOptions{
			ID:          "filler-" + strconv.Itoa(i),
			Name:        "filler-" + strconv.Itoa(i),
			Permissions: []string{"kv:read"},
		}); err != nil {
			t.Fatalf("filler role %d: %v", i, err)
		}
	}

	// The re-run meets every built-in as already existing and must verify each
	// one by name rather than by scanning a page of 350+ roles.
	if err := a.SeedBuiltInRoles(ctx, "burnin"); err != nil {
		t.Fatalf("idempotent re-seed over a large tenant: %v", err)
	}
}
