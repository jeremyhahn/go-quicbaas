// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// Gap 1: email_verified enforcement. A correctly-signed IdP token whose
// email_verified == false must be REJECTED on the JIT/linking-by-email path,
// and no user may be provisioned (account-takeover guard).
func TestHandleCallback_EmailNotVerifiedRejected(t *testing.T) {
	h := newCallbackHarness(t, nil, []string{"role-default"})
	stateToken, nonce := h.begin(t)

	h.idp.idTokenFn = func(issuer string) string {
		return h.idp.signedIDToken(issuer, nonce, func(c jwt.MapClaims) {
			c["email_verified"] = false
		})
	}

	_, err := h.manager.HandleCallback(h.ctx, stateToken, "auth-code")
	if err == nil {
		t.Fatal("expected rejection when email_verified == false")
	}
	if !errors.Is(err, ErrEmailNotVerified) {
		t.Errorf("error = %v, want ErrEmailNotVerified", err)
	}
	if _, uerr := h.identityStore.GetUserByEmail(h.ctx, h.provider.TenantID, "alice@acme.com"); uerr == nil {
		t.Fatal("unverified-email login must not provision a user")
	}
}

// Gap 1 (variant): a missing email_verified claim is treated as NOT verified
// (fail-closed) when email-based JIT is in play.
func TestHandleCallback_EmailVerifiedMissingRejected(t *testing.T) {
	h := newCallbackHarness(t, nil, []string{"role-default"})
	stateToken, nonce := h.begin(t)

	h.idp.idTokenFn = func(issuer string) string {
		return h.idp.signedIDToken(issuer, nonce, func(c jwt.MapClaims) {
			delete(c, "email_verified")
		})
	}

	_, err := h.manager.HandleCallback(h.ctx, stateToken, "auth-code")
	if err == nil {
		t.Fatal("expected rejection when email_verified is absent")
	}
	if !errors.Is(err, ErrEmailNotVerified) {
		t.Errorf("error = %v, want ErrEmailNotVerified", err)
	}
}

// Gap 2: AllowedEmailDomains gating. An email outside the allowlist is rejected;
// inside is allowed; an empty list allows all.
func TestHandleCallback_AllowedEmailDomains(t *testing.T) {
	t.Run("outside allowlist rejected", func(t *testing.T) {
		h := newCallbackHarness(t, nil, []string{"role-default"})
		h.provider.AllowedEmailDomains = []string{"corp.example"}
		mustUpdateProvider(t, h)
		stateToken, nonce := h.begin(t)
		h.idp.idTokenFn = func(issuer string) string {
			return h.idp.signedIDToken(issuer, nonce, nil) // email alice@acme.com
		}

		_, err := h.manager.HandleCallback(h.ctx, stateToken, "auth-code")
		if err == nil {
			t.Fatal("expected rejection for domain outside allowlist")
		}
		if !errors.Is(err, ErrEmailDomainNotAllowed) {
			t.Errorf("error = %v, want ErrEmailDomainNotAllowed", err)
		}
		if _, uerr := h.identityStore.GetUserByEmail(h.ctx, h.provider.TenantID, "alice@acme.com"); uerr == nil {
			t.Fatal("disallowed-domain login must not provision a user")
		}
	})

	t.Run("inside allowlist allowed", func(t *testing.T) {
		h := newCallbackHarness(t, nil, []string{"role-default"})
		h.provider.AllowedEmailDomains = []string{"ACME.com"} // case-insensitive
		mustUpdateProvider(t, h)
		stateToken, nonce := h.begin(t)
		h.idp.idTokenFn = func(issuer string) string {
			return h.idp.signedIDToken(issuer, nonce, nil)
		}

		res, err := h.manager.HandleCallback(h.ctx, stateToken, "auth-code")
		if err != nil {
			t.Fatalf("expected success for in-allowlist domain, got: %v", err)
		}
		if !res.Provisioned {
			t.Error("expected user provisioning for allowed domain")
		}
	})

	t.Run("empty list allows all", func(t *testing.T) {
		h := newCallbackHarness(t, nil, []string{"role-default"})
		// AllowedEmailDomains left empty.
		stateToken, nonce := h.begin(t)
		h.idp.idTokenFn = func(issuer string) string {
			return h.idp.signedIDToken(issuer, nonce, nil)
		}

		if _, err := h.manager.HandleCallback(h.ctx, stateToken, "auth-code"); err != nil {
			t.Fatalf("empty allowlist should allow all, got: %v", err)
		}
	})
}

// Gap 3: empty-email hard reject when linking/JIT-by-email is in play.
func TestHandleCallback_EmptyEmailRejected(t *testing.T) {
	h := newCallbackHarness(t, nil, []string{"role-default"})
	// AllowLinking is true in the harness; AutoProvision is true too.
	stateToken, nonce := h.begin(t)
	h.idp.idTokenFn = func(issuer string) string {
		return h.idp.signedIDToken(issuer, nonce, func(c jwt.MapClaims) {
			delete(c, "email")
		})
	}

	_, err := h.manager.HandleCallback(h.ctx, stateToken, "auth-code")
	if err == nil {
		t.Fatal("expected rejection when email claim is missing on email-based flow")
	}
	if !errors.Is(err, ErrMissingEmailClaim) {
		t.Errorf("error = %v, want ErrMissingEmailClaim", err)
	}
}

// Gap 4: RequireExplicitRoleMapping. With it set and neither a mapping match nor
// DefaultRoles, the login is denied with no zero-role user. With it false, the
// DefaultRoles fallback applies as before.
func TestHandleCallback_RequireExplicitRoleMapping(t *testing.T) {
	t.Run("true + no roles resolvable -> denied", func(t *testing.T) {
		// No mappings, no defaults: resolution yields zero roles.
		h := newCallbackHarness(t, nil, nil)
		h.provider.RequireExplicitRoleMapping = true
		mustUpdateProvider(t, h)
		stateToken, nonce := h.begin(t)
		h.idp.idTokenFn = func(issuer string) string {
			return h.idp.signedIDToken(issuer, nonce, nil)
		}

		_, err := h.manager.HandleCallback(h.ctx, stateToken, "auth-code")
		if err == nil {
			t.Fatal("expected fail-closed denial when no roles resolvable")
		}
		if !errors.Is(err, ErrNoRolesResolvable) {
			t.Errorf("error = %v, want ErrNoRolesResolvable", err)
		}
		if _, uerr := h.identityStore.GetUserByEmail(h.ctx, h.provider.TenantID, "alice@acme.com"); uerr == nil {
			t.Fatal("no zero-role user may be provisioned")
		}
	})

	t.Run("false + no mapping -> DefaultRoles fallback", func(t *testing.T) {
		h := newCallbackHarness(t, nil, []string{"role-default"})
		// RequireExplicitRoleMapping defaults to false.
		stateToken, nonce := h.begin(t)
		h.idp.idTokenFn = func(issuer string) string {
			return h.idp.signedIDToken(issuer, nonce, nil)
		}

		res, err := h.manager.HandleCallback(h.ctx, stateToken, "auth-code")
		if err != nil {
			t.Fatalf("expected success with DefaultRoles fallback, got: %v", err)
		}
		roles, err := h.identityStore.GetUserRoles(h.ctx, h.provider.TenantID, res.User.ID)
		if err != nil {
			t.Fatalf("get roles: %v", err)
		}
		if len(roles) != 1 || roles[0].ID != "role-default" {
			t.Errorf("roles = %v, want [role-default]", roles)
		}
	})
}

// mustUpdateProvider persists harness provider mutations and invalidates the
// manager's per-provider caches so the change takes effect on the next callback.
func mustUpdateProvider(t *testing.T, h *callbackHarness) {
	t.Helper()
	if err := h.manager.providerStorage.UpdateProvider(h.ctx, h.provider); err != nil {
		t.Fatalf("update provider: %v", err)
	}
	h.manager.invalidateClientCache(h.provider.ID)
}
