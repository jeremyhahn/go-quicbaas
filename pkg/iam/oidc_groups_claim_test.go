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

// seedGroupsClaimUser stores two named roles and a user bound to them, and
// returns the user. The account carries no group list; the `groups` claim has
// only the role bindings to come from.
func seedGroupsClaimUser(t *testing.T, storage *MemoryStorage) *User {
	t.Helper()
	ctx := context.Background()
	for _, r := range []*Role{
		{ID: "role-dev", TenantID: "acme", Name: "developers"},
		{ID: "role-lead", TenantID: "acme", Name: "team-leads"},
	} {
		if err := storage.CreateRole(ctx, r); err != nil {
			t.Fatalf("CreateRole: %v", err)
		}
	}
	user := &User{
		ID:        7001,
		TenantID:  "acme",
		Username:  "alice",
		Name:      "Alice",
		Email:     "alice@acme.test",
		Roles:     []string{"role-dev", "role-lead"},
		Lifecycle: LifecycleActive,
	}
	if err := storage.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return user
}

var wantGroupsClaim = []string{"developers", "team-leads"}

// TestGroupsClaim_CodeGrantDerivesFromRoleBindings pins go-quicbaas#1027 on the
// authorization-code path: the ID token and the access token (which userinfo
// answers from) state the names of the roles the user is bound to.
func TestGroupsClaim_CodeGrantDerivesFromRoleBindings(t *testing.T) {
	provider, _, _, _, storage := createTestOIDCProvider(t)
	user := seedGroupsClaimUser(t, storage)
	ctx := context.Background()

	resp, err := provider.generateTokenResponse(ctx, user, "app_groups_client", []string{"openid"}, "", "", nil)
	if err != nil {
		t.Fatalf("generateTokenResponse: %v", err)
	}

	idClaims, err := provider.ValidateIDToken(ctx, resp.IDToken)
	if err != nil {
		t.Fatalf("ValidateIDToken: %v", err)
	}
	if !slices.Equal(idClaims.Groups, wantGroupsClaim) {
		t.Fatalf("ID token groups = %v, want the bound role names %v", idClaims.Groups, wantGroupsClaim)
	}

	access, err := provider.ValidateAccessToken(ctx, resp.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if !slices.Equal(access.Groups, wantGroupsClaim) {
		t.Fatalf("access token groups = %v, want %v", access.Groups, wantGroupsClaim)
	}

	info, err := provider.UserInfo(ctx, resp.AccessToken)
	if err != nil {
		t.Fatalf("UserInfo: %v", err)
	}
	if !slices.Equal(info.Groups, wantGroupsClaim) {
		t.Fatalf("userinfo groups = %v, want %v", info.Groups, wantGroupsClaim)
	}
}

// TestGroupsClaim_HardwareLoginDerivesFromRoleBindings covers the hardware
// issuance path, which resolves the claim in the provider before minting.
func TestGroupsClaim_HardwareLoginDerivesFromRoleBindings(t *testing.T) {
	provider, _, _, _, storage := createTestOIDCProvider(t)
	user := seedGroupsClaimUser(t, storage)
	ctx := context.Background()

	set, err := provider.IssueHardwareTokens(ctx, HardwareTokenOptions{
		User:     user,
		ClientID: "app_groups_client",
		AMR:      []string{"webauthn"},
	})
	if err != nil {
		t.Fatalf("IssueHardwareTokens: %v", err)
	}
	access, err := provider.ValidateAccessToken(ctx, set.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if !slices.Equal(access.Groups, wantGroupsClaim) {
		t.Fatalf("hardware access token groups = %v, want %v", access.Groups, wantGroupsClaim)
	}
}

// TestGroupsClaim_UnresolvableRolesRefuseIssuance pins that a failed role
// lookup stops issuance rather than minting a token whose groups claim
// silently under-states the account.
func TestGroupsClaim_UnresolvableRolesRefuseIssuance(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	// No tenant: the role lookup fails validation, which is the shape of any
	// store failure other than not-found.
	user := &User{ID: 7002, Roles: []string{"role-dev"}, Lifecycle: LifecycleActive}

	if _, err := provider.generateTokenResponse(ctx, user, "app_groups_client", []string{"openid"}, "", "", nil); err == nil {
		t.Fatal("generateTokenResponse must refuse when the bound roles cannot be read")
	}
	if _, err := provider.IssueHardwareTokens(ctx, HardwareTokenOptions{User: user, ClientID: "app_groups_client"}); err == nil {
		t.Fatal("IssueHardwareTokens must refuse when the bound roles cannot be read")
	}
}
