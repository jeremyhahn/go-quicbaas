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
	"context"
	"crypto/elliptic"
	"errors"
	"strings"
	"testing"
)

// federation_idtoken_errors.go: every error type renders a message and unwraps
// its cause where applicable.
func TestIDTokenErrorTypes_MessagesAndUnwrap(t *testing.T) {
	cause := errors.New("boom")

	cases := []struct {
		name    string
		err     error
		want    string
		unwraps error
	}{
		{"config", &IDTokenValidatorConfigError{Reason: "bad"}, "config invalid: bad", nil},
		{"invalid", &IDTokenInvalidError{Reason: "nope"}, "idtoken invalid: nope", nil},
		{"sig-cause", &IDTokenSignatureError{Cause: cause}, "signature invalid: boom", cause},
		{"sig-nocause", &IDTokenSignatureError{}, "signature invalid", nil},
		{"issuer", &IDTokenIssuerError{}, "issuer mismatch", nil},
		{"audience", &IDTokenAudienceError{}, "audience mismatch", nil},
		{"expired", &IDTokenExpiredError{}, "idtoken expired", nil},
		{"nonce", &IDTokenNonceError{}, "nonce mismatch", nil},
		{"jwks-status", &IDTokenJWKSError{Reason: "x", StatusCode: 503}, "status 503", nil},
		{"jwks-cause", &IDTokenJWKSError{Reason: "x", Cause: cause}, "boom", cause},
		{"jwks-plain", &IDTokenJWKSError{Reason: "plain"}, "jwks error: plain", nil},
		{"no-group", &NoGroupMatchError{ProviderID: "p"}, "no IdP group", nil},
		{"email-unverified", &EmailNotVerifiedError{Email: "a@b"}, "not verified", nil},
		{"email-missing", &MissingEmailClaimError{}, "email claim missing", nil},
		{"email-domain", &EmailDomainNotAllowedError{Domain: "b.com"}, "not in the provider allowlist", nil},
		{"no-roles", &NoRolesResolvableError{ProviderID: "p"}, "no roles resolvable", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if msg := tc.err.Error(); !strings.Contains(msg, tc.want) {
				t.Fatalf("Error() = %q, want substring %q", msg, tc.want)
			}
			if tc.unwraps != nil && !errors.Is(tc.err, tc.unwraps) {
				t.Fatalf("errors.Is did not match wrapped cause")
			}
		})
	}
}

// ecCurveForName resolves the supported curves and rejects unknown ones.
func TestECCurveForName(t *testing.T) {
	for crv, want := range map[string]elliptic.Curve{
		"P-256": elliptic.P256(),
		"P-384": elliptic.P384(),
		"P-521": elliptic.P521(),
	} {
		got, err := ecCurveForName(crv)
		if err != nil || got != want {
			t.Fatalf("ecCurveForName(%q) = %v, %v", crv, got, err)
		}
	}
	if _, err := ecCurveForName("P-999"); err == nil {
		t.Fatal("ecCurveForName(unknown) want error")
	} else {
		var jwksErr *IDTokenJWKSError
		if !errors.As(err, &jwksErr) {
			t.Fatalf("want IDTokenJWKSError, got %T", err)
		}
	}
}

// oidc_keymanager.go error types.
func TestKeyManagerErrorTypes(t *testing.T) {
	if msg := (&UnknownKeyIDError{KID: "abc"}).Error(); !strings.Contains(msg, "abc") {
		t.Fatalf("UnknownKeyIDError = %q", msg)
	}
	if msg := (&SigningKeyUnavailableError{}).Error(); !strings.Contains(msg, "no signing key") {
		t.Fatalf("SigningKeyUnavailableError = %q", msg)
	}
	cause := errors.New("marshal-fail")
	pkErr := &PublicKeyMarshalError{Cause: cause}
	if !strings.Contains(pkErr.Error(), "marshal-fail") {
		t.Fatalf("PublicKeyMarshalError = %q", pkErr.Error())
	}
	if !errors.Is(pkErr, cause) {
		t.Fatal("PublicKeyMarshalError must unwrap its cause")
	}
}

// oktaonboard.go error types.
func TestOktaOnboardErrorTypes(t *testing.T) {
	cause := errors.New("step-cause")
	onboard := &OktaOnboardingError{Step: "register_domain", Err: cause}
	if !strings.Contains(onboard.Error(), "register_domain") || !strings.Contains(onboard.Error(), "step-cause") {
		t.Fatalf("OktaOnboardingError = %q", onboard.Error())
	}
	if !errors.Is(onboard, cause) {
		t.Fatal("OktaOnboardingError must unwrap its cause")
	}
	if msg := (&MissingCollaboratorError{Name: "rbac"}).Error(); !strings.Contains(msg, "rbac") {
		t.Fatalf("MissingCollaboratorError = %q", msg)
	}
	if msg := (&OnboardingInputError{Reason: "blank"}).Error(); !strings.Contains(msg, "blank") {
		t.Fatalf("OnboardingInputError = %q", msg)
	}
}

// operator_sso.go BootstrapNotReadyError.
func TestBootstrapNotReadyError(t *testing.T) {
	if msg := (&BootstrapNotReadyError{}).Error(); !strings.Contains(msg, "bootstrap admin") {
		t.Fatalf("BootstrapNotReadyError = %q", msg)
	}
}

// oidc_tenant_signing.go TenantSigningKeyUnavailableError.
func TestTenantSigningKeyUnavailableError(t *testing.T) {
	cause := errors.New("sealed")
	err := &TenantSigningKeyUnavailableError{TenantID: "acme", Cause: cause}
	if !strings.Contains(err.Error(), "acme") || !strings.Contains(err.Error(), "sealed") {
		t.Fatalf("TenantSigningKeyUnavailableError = %q", err.Error())
	}
	if !errors.Is(err, cause) {
		t.Fatal("must unwrap cause")
	}
	// No-cause variant still renders.
	if msg := (&TenantSigningKeyUnavailableError{TenantID: "x"}).Error(); !strings.Contains(msg, "x") {
		t.Fatalf("no-cause TenantSigningKeyUnavailableError = %q", msg)
	}
}

// TenantManagerOIDCSettings adapter: OIDCSettingsFor and DedicatedSigningTenants.
func TestTenantManagerOIDCSettings_Adapter(t *testing.T) {
	ctx := context.Background()

	// Nil adapter / nil manager returns (nil, nil) for both methods.
	var nilAdapter *TenantManagerOIDCSettings
	if cfg, err := nilAdapter.OIDCSettingsFor(ctx, "t"); cfg != nil || err != nil {
		t.Fatalf("nil OIDCSettingsFor = %v, %v", cfg, err)
	}
	if ids, err := nilAdapter.DedicatedSigningTenants(ctx); ids != nil || err != nil {
		t.Fatalf("nil DedicatedSigningTenants = %v, %v", ids, err)
	}

	mgr := newTestTenantManager()
	dedicated, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		Name: "Dedicated",
		Settings: &TenantSettings{
			OIDC: &TenantOIDCConfig{DedicatedSigningKey: true},
		},
	})
	if err != nil {
		t.Fatalf("CreateTenant(dedicated): %v", err)
	}
	if _, err := mgr.CreateTenant(ctx, CreateTenantOptions{Name: "Plain"}); err != nil {
		t.Fatalf("CreateTenant(plain): %v", err)
	}

	adapter := NewTenantManagerOIDCSettings(mgr)

	cfg, err := adapter.OIDCSettingsFor(ctx, dedicated.ID)
	if err != nil {
		t.Fatalf("OIDCSettingsFor: %v", err)
	}
	if cfg == nil || !cfg.DedicatedSigningKey {
		t.Fatalf("OIDCSettingsFor = %v, want dedicated", cfg)
	}

	ids, err := adapter.DedicatedSigningTenants(ctx)
	if err != nil {
		t.Fatalf("DedicatedSigningTenants: %v", err)
	}
	if len(ids) != 1 || ids[0] != dedicated.ID {
		t.Fatalf("DedicatedSigningTenants = %v, want [%s]", ids, dedicated.ID)
	}
}
