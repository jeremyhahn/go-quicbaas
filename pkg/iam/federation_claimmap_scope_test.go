//go:build iam

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package iam

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
)

func provWithScopeMappings(mappings []GroupScopeMapping, defaults []string) *ExternalProvider {
	return &ExternalProvider{
		ID:            "provider-scope",
		DefaultScopes: defaults,
		ClaimMappings: &ClaimMappings{
			Groups:             "groups",
			Roles:              "roles",
			GroupScopeMappings: mappings,
		},
	}
}

// Match: an IdP group maps to a platform scope (the `scp` catalog form).
func TestResolveScopes_MappedGroupsMatch(t *testing.T) {
	p := provWithScopeMappings([]GroupScopeMapping{
		{Group: "Acme Administrators", Scope: "admin.write"},
		{Group: "Acme Auditors", Scope: "audit.read"},
	}, nil)

	claims := map[string]interface{}{
		"groups": []interface{}{"Acme Auditors", "Some Other Group"},
	}
	res, err := resolveScopes(p, claims)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UsedDefaults {
		t.Error("expected mapped resolution, not defaults")
	}
	if len(res.Scopes) != 1 || res.Scopes[0] != "audit.read" {
		t.Errorf("Scopes = %v, want [audit.read]", res.Scopes)
	}
}

// Multiple matches across the groups and roles claims are de-duplicated.
func TestResolveScopes_MultipleMatchesDeduped(t *testing.T) {
	p := provWithScopeMappings([]GroupScopeMapping{
		{Group: "admins", Scope: "admin.write"},
		{Group: "platform-admin", Scope: "admin.write"}, // same scope, distinct group
		{Group: "devs", Scope: "build.run"},
	}, nil)
	claims := map[string]interface{}{
		"groups": []interface{}{"admins", "platform-admin"},
		"roles":  []interface{}{"devs"},
	}
	res, err := resolveScopes(p, claims)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := append([]string{}, res.Scopes...)
	sort.Strings(got)
	if len(got) != 2 || got[0] != "admin.write" || got[1] != "build.run" {
		t.Errorf("Scopes = %v, want [admin.write build.run]", got)
	}
}

// STARTS_WITH prefix matching mirrors role resolution.
func TestResolveScopes_PrefixMatch(t *testing.T) {
	p := provWithScopeMappings([]GroupScopeMapping{
		{Group: "svc-", Scope: "service.invoke", Prefix: true},
	}, nil)
	claims := map[string]interface{}{"roles": []interface{}{"svc-billing"}}
	res, err := resolveScopes(p, claims)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Scopes) != 1 || res.Scopes[0] != "service.invoke" {
		t.Errorf("Scopes = %v, want [service.invoke]", res.Scopes)
	}
}

// No-match fail-closed: scope mappings are configured but nothing matched.
func TestResolveScopes_FailClosedNoMatch(t *testing.T) {
	p := provWithScopeMappings([]GroupScopeMapping{
		{Group: "admins", Scope: "admin.write"},
	}, []string{"should.not.be.used"})
	claims := map[string]interface{}{"groups": []interface{}{"contractors"}}

	_, err := resolveScopes(p, claims)
	if !errors.As(err, new(*NoScopeMatchError)) {
		t.Fatalf("expected NoScopeMatchError, got %v", err)
	}
}

// No-mappings passthrough: DefaultScopes are used when no GroupScopeMappings.
func TestResolveScopes_DefaultsFallback(t *testing.T) {
	p := provWithScopeMappings(nil, []string{"openid", "profile.read"})
	claims := map[string]interface{}{"groups": []interface{}{"anything"}}

	res, err := resolveScopes(p, claims)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.UsedDefaults {
		t.Error("expected UsedDefaults true")
	}
	if len(res.Scopes) != 2 || res.Scopes[0] != "openid" || res.Scopes[1] != "profile.read" {
		t.Errorf("Scopes = %v, want [openid profile.read]", res.Scopes)
	}
}

// No-mappings, nil ClaimMappings: still falls back to DefaultScopes without panic.
func TestResolveScopes_DefaultsFallbackNilMappings(t *testing.T) {
	p := &ExternalProvider{ID: "p", DefaultScopes: []string{"d.read"}}
	res, err := resolveScopes(p, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.UsedDefaults || len(res.Scopes) != 1 || res.Scopes[0] != "d.read" {
		t.Errorf("res = %+v, want defaults [d.read]", res)
	}
}

// NoScopeMatchError exposes a stable, non-secret message.
func TestNoScopeMatchError_Message(t *testing.T) {
	e := &NoScopeMatchError{ProviderID: "p"}
	if e.Error() == "" {
		t.Fatal("empty error message")
	}
}

// federationTokenScopes unions base OIDC scopes with resolved scopes, dedups,
// and is order-stable (base first). A nil resolution yields just base scopes.
func TestFederationTokenScopes_Union(t *testing.T) {
	got := federationTokenScopes(&ScopeResolution{Scopes: []string{"admin.write", "email", "audit.read"}})
	want := []string{"openid", "profile", "email", "admin.write", "audit.read"}
	if len(got) != len(want) {
		t.Fatalf("scopes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("scopes = %v, want %v", got, want)
		}
	}
}

func TestFederationTokenScopes_NilResolution(t *testing.T) {
	got := federationTokenScopes(nil)
	want := []string{"openid", "profile", "email"}
	if len(got) != len(want) {
		t.Fatalf("scopes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("scopes = %v, want %v", got, want)
		}
	}
}

// Empty resolved scope entries are skipped (never injected as a blank scope).
func TestFederationTokenScopes_SkipsEmpty(t *testing.T) {
	got := federationTokenScopes(&ScopeResolution{Scopes: []string{"", "x.read", ""}})
	want := []string{"openid", "profile", "email", "x.read"}
	if len(got) != len(want) {
		t.Fatalf("scopes = %v, want %v", got, want)
	}
}

// End-to-end: a JIT-provisioned federated login carries the scopes resolved from
// its IdP claim->scope mappings on the FederationResult.
func TestProcessAuthentication_ScopesGranted(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)
	ctx := context.Background()

	provider := createTestProvider("tenant1", "okta")
	provider.ApplyDefaults()
	provider.ID = "provider-scope-e2e"
	provider.AutoProvision = true
	provider.AllowLinking = false
	provider.ClaimMappings.Groups = "groups"
	provider.ClaimMappings.GroupScopeMappings = []GroupScopeMapping{
		{Group: "platform-admins", Scope: "admin.write"},
	}

	claims := map[string]interface{}{
		"sub":            "scope-user",
		"email":          "scopeuser@example.com",
		"email_verified": true,
		"groups":         []interface{}{"platform-admins"},
	}
	state := &FederationCallbackState{Nonce: "n", ProviderID: provider.ID, TenantID: "tenant1"}

	result, err := manager.processAuthentication(ctx, provider, claims, state)
	if err != nil {
		t.Fatalf("processAuthentication: %v", err)
	}
	if !containsString(result.ScopesGranted, "admin.write") {
		t.Fatalf("ScopesGranted = %v, want it to include admin.write", result.ScopesGranted)
	}
	for _, base := range []string{"openid", "profile", "email"} {
		if !containsString(result.ScopesGranted, base) {
			t.Fatalf("ScopesGranted = %v, missing base scope %q", result.ScopesGranted, base)
		}
	}
}

// End-to-end fail-closed: scope mappings configured but the IdP groups match
// none -> the login is denied before any user is provisioned.
func TestProcessAuthentication_ScopeMappingFailClosed(t *testing.T) {
	manager, _, _, _, _, _ := setupTestFederationManager(t)
	ctx := context.Background()

	provider := createTestProvider("tenant1", "okta")
	provider.ApplyDefaults()
	provider.ID = "provider-scope-denied"
	provider.AutoProvision = true
	provider.ClaimMappings.Groups = "groups"
	provider.ClaimMappings.GroupScopeMappings = []GroupScopeMapping{
		{Group: "platform-admins", Scope: "admin.write"},
	}

	claims := map[string]interface{}{
		"sub":            "denied-user",
		"email":          "denied@example.com",
		"email_verified": true,
		"groups":         []interface{}{"contractors"},
	}
	state := &FederationCallbackState{Nonce: "n", ProviderID: provider.ID, TenantID: "tenant1"}

	_, err := manager.processAuthentication(ctx, provider, claims, state)
	if err == nil {
		t.Fatal("expected fail-closed denial when no scope mapping matches")
	}
}

// End-to-end through a real OIDC provider: resolved scopes are unioned with the
// base scopes and carried onto the issued access token's `scope`/`scp`.
func TestGenerateTokenResult_OIDCScopesOnToken(t *testing.T) {
	setupTestXKMS(t)
	oidcProvider, _ := newManagerBackedProvider(t)

	manager, err := NewFederationManager(
		WithProviderStorage(NewMemoryProviderStorage()),
		WithLinkStorage(NewMemoryFederationStorage()),
		WithStateStorage(NewMemoryStateStorage()),
		WithFederationIdentityStore(NewIdentityStore(IdentityStoreOptions{
			UserStorage:  newMockUserStorage(),
			GroupStorage: newMockGroupStorage(),
			RoleStorage:  newMockRoleStorage(),
		})),
		WithFederationOIDCProvider(oidcProvider),
	)
	if err != nil {
		t.Fatalf("NewFederationManager: %v", err)
	}

	ctx := context.Background()
	user := &User{ID: "user-scope-token", TenantID: "tenant1", Email: "u@example.com"}
	identity := &FederatedIdentity{ID: "id-1", UserID: user.ID, TenantID: "tenant1", ProviderID: "p"}

	result, err := manager.generateTokenResult(ctx, user, identity, false,
		&ScopeResolution{Scopes: []string{"admin.write", "audit.read"}})
	if err != nil {
		t.Fatalf("generateTokenResult: %v", err)
	}
	if result.AccessToken == "" {
		t.Fatal("expected an access token from the OIDC provider")
	}
	if !containsString(result.ScopesGranted, "admin.write") {
		t.Fatalf("ScopesGranted = %v, want admin.write", result.ScopesGranted)
	}

	claims, err := oidcProvider.ValidateAccessToken(ctx, result.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	for _, want := range []string{"openid", "profile", "email", "admin.write", "audit.read"} {
		if !strings.Contains(claims.Scope, want) {
			t.Fatalf("token scope %q missing %q", claims.Scope, want)
		}
	}
}

// scopeGroupMatches rejects empty mapping or empty IdP values (no blank match).
func TestScopeGroupMatches_EmptyValues(t *testing.T) {
	if scopeGroupMatches(GroupScopeMapping{Group: "", Scope: "x"}, "anything") {
		t.Error("empty mapping group must not match")
	}
	if scopeGroupMatches(GroupScopeMapping{Group: "admins", Scope: "x"}, "") {
		t.Error("empty IdP value must not match")
	}
}
