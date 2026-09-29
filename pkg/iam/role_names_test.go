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
	"slices"
	"testing"
)

// TestIdentityStore_RoleNames covers the derivation behind the OIDC `groups`
// claim: the names of the roles the user is bound to.
func TestIdentityStore_RoleNames(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryStorage()
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: mem, RoleStorage: mem,
	})
	for _, r := range []*Role{
		{ID: "r-ops", TenantID: "acme", Name: "operators"},
		{ID: "r-dev", TenantID: "acme", Name: "developers"},
	} {
		if err := mem.CreateRole(ctx, r); err != nil {
			t.Fatalf("CreateRole: %v", err)
		}
	}

	t.Run("names in binding order, deleted and repeated roles dropped", func(t *testing.T) {
		user := &User{TenantID: "acme", Roles: []string{"r-dev", "r-gone", "r-ops", "r-dev"}}
		got, err := store.RoleNames(ctx, user)
		if err != nil {
			t.Fatalf("RoleNames: %v", err)
		}
		if want := []string{"developers", "operators"}; !slices.Equal(got, want) {
			t.Fatalf("RoleNames = %v, want %v", got, want)
		}
	})

	t.Run("no roles yields no names", func(t *testing.T) {
		got, err := store.RoleNames(ctx, &User{TenantID: "acme"})
		if err != nil || got != nil {
			t.Fatalf("RoleNames = %v, %v; want nil, nil", got, err)
		}
		if got, err := store.RoleNames(ctx, nil); err != nil || got != nil {
			t.Fatalf("RoleNames(nil) = %v, %v; want nil, nil", got, err)
		}
	})

	t.Run("a lookup failure is returned, not a partial claim", func(t *testing.T) {
		// An invalid tenant fails validation rather than resolving to not-found,
		// which is the shape of any non-not-found store failure.
		_, err := store.RoleNames(ctx, &User{TenantID: "", Roles: []string{"r-dev"}})
		if err == nil {
			t.Fatal("RoleNames must surface a lookup failure")
		}
	})
}
