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
	"errors"
	"strings"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// newRegistrationTestManager builds an AppManager over in-memory storage with
// no tenant quota manager (quotas are exercised elsewhere).
func newRegistrationTestManager() *AppManager {
	return NewAppManager(NewMemoryStorage(), nil)
}

// dcrOptions is a minimal, valid RFC 7591 registration.
func dcrOptions(name string) RegisterAppOptions {
	return RegisterAppOptions{
		Name:         name,
		RedirectURIs: []string{"https://app.example/cb"},
		GrantTypes:   []string{GrantTypeAuthorizationCode},
	}
}

// TestRegisterDCRApp_IssuesUsableToken verifies a DCR registration returns a
// plaintext registration access token that authorizes exactly that client.
func TestRegisterDCRApp_IssuesUsableToken(t *testing.T) {
	t.Parallel()

	m := newRegistrationTestManager()
	ctx := context.Background()

	app, token, err := m.RegisterDCRApp(ctx, "tenant-a", dcrOptions("app"))
	if err != nil {
		t.Fatalf("RegisterDCRApp: %v", err)
	}
	if token == "" {
		t.Fatal("registration access token must be returned")
	}
	if !strings.HasPrefix(token, registrationAccessTokenPrefix) {
		t.Errorf("token %q missing %q prefix", token, registrationAccessTokenPrefix)
	}
	if app.RegistrationTokenHash != "" {
		t.Error("the returned application must not carry the token hash")
	}
	if app.ClientSecret == "" {
		t.Error("the returned application must carry the plaintext client secret")
	}

	authorized, err := m.AuthorizeRegistrationAccess(ctx, "", app.ClientID, token)
	if err != nil {
		t.Fatalf("AuthorizeRegistrationAccess with the issued token: %v", err)
	}
	if authorized.ClientID != app.ClientID {
		t.Errorf("authorized client = %q, want %q", authorized.ClientID, app.ClientID)
	}
	if authorized.ClientSecret != "" || authorized.RegistrationTokenHash != "" {
		t.Error("the authorized application must be redacted")
	}
}

// TestRegisterDCRApp_TokensAreUniquePerClient verifies each registration mints
// a distinct credential, and that the stored form is a hash rather than the
// secret itself.
func TestRegisterDCRApp_TokensAreUniquePerClient(t *testing.T) {
	t.Parallel()

	storage := NewMemoryStorage()
	m := NewAppManager(storage, nil)
	ctx := context.Background()

	appA, tokenA, err := m.RegisterDCRApp(ctx, "tenant-a", dcrOptions("a"))
	if err != nil {
		t.Fatalf("RegisterDCRApp a: %v", err)
	}
	appB, tokenB, err := m.RegisterDCRApp(ctx, "tenant-a", dcrOptions("b"))
	if err != nil {
		t.Fatalf("RegisterDCRApp b: %v", err)
	}

	if tokenA == tokenB {
		t.Fatal("two registrations issued the same registration access token")
	}
	if appA.ClientID == appB.ClientID {
		t.Fatal("two registrations issued the same client id")
	}

	stored, err := storage.GetApp(ctx, appA.ClientID)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if stored.RegistrationTokenHash == tokenA {
		t.Fatal("the token was stored verbatim; it must be stored hashed")
	}
	if stored.RegistrationTokenHash != HashTokenValue(tokenA) {
		t.Fatal("the stored hash is not the canonical HashTokenValue derivation")
	}
}

// TestAuthorizeRegistrationAccess_Denials verifies every failure mode is
// refused with the typed error and no application.
func TestAuthorizeRegistrationAccess_Denials(t *testing.T) {
	t.Parallel()

	m := newRegistrationTestManager()
	ctx := context.Background()

	appA, tokenA, err := m.RegisterDCRApp(ctx, "tenant-a", dcrOptions("a"))
	if err != nil {
		t.Fatalf("RegisterDCRApp a: %v", err)
	}
	appB, _, err := m.RegisterDCRApp(ctx, "tenant-b", dcrOptions("b"))
	if err != nil {
		t.Fatalf("RegisterDCRApp b: %v", err)
	}

	// A client created WITHOUT dynamic registration owns no management
	// credential at all.
	plain, err := m.RegisterApp(ctx, "tenant-a", dcrOptions("plain"))
	if err != nil {
		t.Fatalf("RegisterApp: %v", err)
	}

	cases := []struct {
		name     string
		tenantID string
		clientID string
		token    string
	}{
		{"empty client id", "", "", tokenA},
		{"empty token", "", appA.ClientID, ""},
		{"unknown client", "", "app_tenant-a_missing", tokenA},
		{"token belongs to another client", "", appB.ClientID, tokenA},
		{"token from another tenant's client", "tenant-b", appA.ClientID, tokenA},
		{"wrong tenant scope", "tenant-b", appA.ClientID, tokenA},
		{"client has no registration token", "", plain.ClientID, tokenA},
		{"forged token", "", appA.ClientID, "rat_forged"},
		{"token hash presented instead of token", "", appA.ClientID, HashTokenValue(tokenA)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, err := m.AuthorizeRegistrationAccess(ctx, tc.tenantID, tc.clientID, tc.token)
			if err == nil {
				t.Fatal("expected denial, got success")
			}
			if app != nil {
				t.Fatal("a denied authorization must not return an application")
			}
			var denied *RegistrationAccessDeniedError
			if !errors.As(err, &denied) {
				t.Fatalf("error %T is not *RegistrationAccessDeniedError", err)
			}
		})
	}
}

// TestAuthorizeRegistrationAccess_TenantScopeNarrows verifies the tenant
// argument narrows (and never widens) the lookup.
func TestAuthorizeRegistrationAccess_TenantScopeNarrows(t *testing.T) {
	t.Parallel()

	m := newRegistrationTestManager()
	ctx := context.Background()

	app, token, err := m.RegisterDCRApp(ctx, "tenant-a", dcrOptions("a"))
	if err != nil {
		t.Fatalf("RegisterDCRApp: %v", err)
	}

	if _, err := m.AuthorizeRegistrationAccess(ctx, "tenant-a", app.ClientID, token); err != nil {
		t.Fatalf("the owning tenant must be accepted: %v", err)
	}
	if _, err := m.AuthorizeRegistrationAccess(ctx, "tenant-z", app.ClientID, token); err == nil {
		t.Fatal("a foreign tenant scope must be refused")
	}
}

// TestUpdateAppInTenant_Scoping verifies the tenant-scoped mutation helpers
// refuse a client that belongs to another tenant, and leave it untouched.
func TestUpdateAppInTenant_Scoping(t *testing.T) {
	t.Parallel()

	m := newRegistrationTestManager()
	ctx := context.Background()

	app, _, err := m.RegisterDCRApp(ctx, "tenant-a", dcrOptions("a"))
	if err != nil {
		t.Fatalf("RegisterDCRApp: %v", err)
	}

	hostile := []string{"https://attacker.example/steal"}
	if _, err := m.UpdateAppInTenant(ctx, "tenant-b", app.ClientID, UpdateAppOptions{RedirectURIs: &hostile}); err == nil {
		t.Fatal("cross-tenant update must be refused")
	}

	current, err := m.GetAppInTenant(ctx, "tenant-a", app.ClientID)
	if err != nil {
		t.Fatalf("GetAppInTenant: %v", err)
	}
	if len(current.RedirectURIs) != 1 || current.RedirectURIs[0] != "https://app.example/cb" {
		t.Fatalf("redirect URIs were mutated across tenants: %v", current.RedirectURIs)
	}

	updated, err := m.UpdateAppInTenant(ctx, "tenant-a", app.ClientID, UpdateAppOptions{RedirectURIs: &hostile})
	if err != nil {
		t.Fatalf("in-tenant update must succeed: %v", err)
	}
	if updated.RedirectURIs[0] != hostile[0] {
		t.Fatalf("update did not apply: %v", updated.RedirectURIs)
	}
	if updated.RegistrationTokenHash != "" || updated.ClientSecret != "" {
		t.Fatal("the updated application must be redacted")
	}
}

// TestDeleteAppInTenant_Scoping verifies a cross-tenant delete is refused and
// the client survives.
func TestDeleteAppInTenant_Scoping(t *testing.T) {
	t.Parallel()

	m := newRegistrationTestManager()
	ctx := context.Background()

	app, token, err := m.RegisterDCRApp(ctx, "tenant-a", dcrOptions("a"))
	if err != nil {
		t.Fatalf("RegisterDCRApp: %v", err)
	}

	if err := m.DeleteAppInTenant(ctx, "tenant-b", app.ClientID); err == nil {
		t.Fatal("cross-tenant delete must be refused")
	}
	if _, err := m.AuthorizeRegistrationAccess(ctx, "tenant-a", app.ClientID, token); err != nil {
		t.Fatalf("the client must survive a refused cross-tenant delete: %v", err)
	}

	if err := m.DeleteAppInTenant(ctx, "tenant-a", app.ClientID); err != nil {
		t.Fatalf("in-tenant delete must succeed: %v", err)
	}
	if _, err := m.AuthorizeRegistrationAccess(ctx, "", app.ClientID, token); err == nil {
		t.Fatal("the credential must not outlive the client")
	}
}

func TestRotateSecretInTenant_Scoping(t *testing.T) {
	t.Parallel()

	m := newRegistrationTestManager()
	ctx := context.Background()

	app, _, err := m.RegisterDCRApp(ctx, "tenant-a", dcrOptions("a"))
	if err != nil {
		t.Fatalf("RegisterDCRApp: %v", err)
	}
	before, err := m.storage.GetApp(ctx, app.ClientID)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	oldHash := before.ClientSecret

	_, err = m.RotateSecretInTenant(ctx, "tenant-b", app.ClientID)
	if !dberrors.IsNotFound(err) {
		t.Fatalf("cross-tenant rotate must be refused with not-found, got %v", err)
	}
	after, err := m.storage.GetApp(ctx, app.ClientID)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if after.ClientSecret != oldHash {
		t.Fatal("a refused cross-tenant rotate must leave the secret unchanged")
	}

	rotated, err := m.RotateSecretInTenant(ctx, "tenant-a", app.ClientID)
	if err != nil {
		t.Fatalf("in-tenant rotate must succeed: %v", err)
	}
	if _, err := m.ValidateClient(ctx, app.ClientID, rotated.ClientSecret); err != nil {
		t.Fatalf("the rotated secret must validate: %v", err)
	}
}

// TestGetAppRedactsCredentials verifies no read path leaks the stored client
// secret or the registration token hash.
func TestGetAppRedactsCredentials(t *testing.T) {
	t.Parallel()

	m := newRegistrationTestManager()
	ctx := context.Background()

	app, _, err := m.RegisterDCRApp(ctx, "tenant-a", dcrOptions("a"))
	if err != nil {
		t.Fatalf("RegisterDCRApp: %v", err)
	}

	got, err := m.GetApp(ctx, app.ClientID)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if got.ClientSecret != "" || got.RegistrationTokenHash != "" {
		t.Error("GetApp leaked a credential")
	}

	apps, err := m.ListApps(ctx, "tenant-a", ListOptions{})
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	for _, a := range apps {
		if a.ClientSecret != "" || a.RegistrationTokenHash != "" {
			t.Errorf("ListApps leaked a credential for %s", a.ClientID)
		}
	}

	rotated, err := m.RotateSecret(ctx, app.ClientID)
	if err != nil {
		t.Fatalf("RotateSecret: %v", err)
	}
	if rotated.RegistrationTokenHash != "" {
		t.Error("RotateSecret leaked the registration token hash")
	}
}

// TestRegisterDCRApp_InvalidOptions verifies the DCR path rejects the same
// invalid metadata the ordinary registration path does, and issues no token.
func TestRegisterDCRApp_InvalidOptions(t *testing.T) {
	m := newRegistrationTestManager()
	ctx := context.Background()

	cases := map[string]RegisterAppOptions{
		"missing name":         {RedirectURIs: []string{"https://app.example/cb"}},
		"missing redirect uri": {Name: "a", GrantTypes: []string{GrantTypeAuthorizationCode}},
		"bad grant type":       {Name: "a", RedirectURIs: []string{"https://app.example/cb"}, GrantTypes: []string{"nope"}},
		"insecure redirect":    {Name: "a", RedirectURIs: []string{"http://evil.example/cb"}},
	}

	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			app, token, err := m.RegisterDCRApp(ctx, "tenant-a", opts)
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if app != nil || token != "" {
				t.Fatal("a refused registration must issue no client and no token")
			}
		})
	}
}

// TestGenerateRegistrationAccessToken verifies the generator produces distinct
// high-entropy values whose hash matches the canonical derivation.
func TestGenerateRegistrationAccessToken(t *testing.T) {
	seen := make(map[string]struct{}, 64)
	for i := 0; i < 64; i++ {
		token, hash, err := generateRegistrationAccessToken()
		if err != nil {
			t.Fatalf("generateRegistrationAccessToken: %v", err)
		}
		if hash != HashTokenValue(token) {
			t.Fatal("hash is not HashTokenValue(token)")
		}
		if len(token) <= len(registrationAccessTokenPrefix) {
			t.Fatalf("token %q carries no entropy", token)
		}
		if _, dup := seen[token]; dup {
			t.Fatalf("duplicate token %q", token)
		}
		seen[token] = struct{}{}
	}
}

// TestRegistrationAccessDeniedError_Message verifies the typed error carries
// the audit detail callers log.
func TestRegistrationAccessDeniedError_Message(t *testing.T) {
	err := &RegistrationAccessDeniedError{ClientID: "app_t_1", Reason: "token mismatch"}
	msg := err.Error()
	if !strings.Contains(msg, "app_t_1") || !strings.Contains(msg, "token mismatch") {
		t.Fatalf("unexpected message %q", msg)
	}
}
