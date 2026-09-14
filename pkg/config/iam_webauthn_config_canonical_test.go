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

package config

import (
	"errors"
	"testing"
)

// =============================================================================
// A value this gate ACCEPTS must be a value the rest of the stack accepts.
//
// The ceremony-policy enums are forwarded to go-xkms, whose validator matches
// the tokens exactly (pkg/webauthn/config.go switches on "none"/"indirect"/
// "direct"/"enterprise" and friends, with no trimming). Boot-time validation
// normalised the value it CHECKED but forwarded the operator's raw spelling, so
// `attestation_preference: " Direct "` passed startup and then failed when the
// WebAuthn service was built. An acceptance check that does not bind the value
// actually used is not authoritative — it is a misleading instrument.
// =============================================================================

// webauthnConfigWithDefaults returns a config whose non-enum fields are valid,
// so a subtest asserts on the enum behaviour alone.
func webauthnConfigWithDefaults() *IAMWebAuthnConfig {
	return &IAMWebAuthnConfig{
		RPID:          "example.com",
		RPDisplayName: "Example",
		RPOrigins:     []string{"https://example.com"},
	}
}

// TestIAMWebAuthnConfig_CanonicalisesAcceptedEnumValues is the criterion: a
// padded, mixed-case value is accepted AND left in the config in the exact form
// the downstream validators require.
func TestIAMWebAuthnConfig_CanonicalisesAcceptedEnumValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		set   func(*IAMWebAuthnConfig)
		get   func(*IAMWebAuthnConfig) string
		input string
		want  string
	}{
		{
			name:  "attestation_preference",
			set:   func(c *IAMWebAuthnConfig) { c.AttestationPreference = " Direct " },
			get:   func(c *IAMWebAuthnConfig) string { return c.AttestationPreference },
			input: " Direct ",
			want:  "direct",
		},
		{
			name:  "user_verification",
			set:   func(c *IAMWebAuthnConfig) { c.UserVerification = "REQUIRED" },
			get:   func(c *IAMWebAuthnConfig) string { return c.UserVerification },
			input: "REQUIRED",
			want:  "required",
		},
		{
			name:  "resident_key_requirement",
			set:   func(c *IAMWebAuthnConfig) { c.ResidentKeyRequirement = "  Preferred" },
			get:   func(c *IAMWebAuthnConfig) string { return c.ResidentKeyRequirement },
			input: "  Preferred",
			want:  "preferred",
		},
		{
			name:  "authenticator_attachment",
			set:   func(c *IAMWebAuthnConfig) { c.AuthenticatorAttachment = "Cross-Platform " },
			get:   func(c *IAMWebAuthnConfig) string { return c.AuthenticatorAttachment },
			input: "Cross-Platform ",
			want:  "cross-platform",
		},
		{
			name:  "assurance",
			set:   func(c *IAMWebAuthnConfig) { c.Assurance = " HARDWARE " },
			get:   func(c *IAMWebAuthnConfig) string { return c.Assurance },
			input: " HARDWARE ",
			want:  "hardware",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := webauthnConfigWithDefaults()
			tc.set(cfg)
			if err := cfg.Validate(); err != nil {
				t.Fatalf("a documented value %q was refused: %v", tc.input, err)
			}
			if got := tc.get(cfg); got != tc.want {
				t.Errorf("forwarded value = %q, want %q — the raw spelling reaches "+
					"go-xkms, whose validator matches the token exactly", got, tc.want)
			}
		})
	}
}

// TestIAMWebAuthnConfig_CanonicalisationSurvivesValidateResolved covers the
// other entry point: the resolved config is what the relying party is built
// from, so it must carry canonical values too.
func TestIAMWebAuthnConfig_CanonicalisationSurvivesValidateResolved(t *testing.T) {
	cfg := webauthnConfigWithDefaults()
	cfg.AttestationPreference = " ENTERPRISE "
	cfg.UserVerification = " Discouraged "

	if err := cfg.ValidateResolved(); err != nil {
		t.Fatalf("ValidateResolved refused documented values: %v", err)
	}
	if cfg.AttestationPreference != "enterprise" {
		t.Errorf("attestation_preference = %q, want %q", cfg.AttestationPreference, "enterprise")
	}
	if cfg.UserVerification != "discouraged" {
		t.Errorf("user_verification = %q, want %q", cfg.UserVerification, "discouraged")
	}
}

// TestIAMWebAuthnConfig_StillRefusesValuesOutsideTheSet keeps the widening
// honest: canonicalising is not the same as accepting anything. A value that is
// not a member after trimming and lowercasing is still refused, and the error
// still names the field.
func TestIAMWebAuthnConfig_StillRefusesValuesOutsideTheSet(t *testing.T) {
	for _, tc := range []struct {
		name  string
		set   func(*IAMWebAuthnConfig)
		field string
	}{
		{
			name:  "attestation_preference",
			set:   func(c *IAMWebAuthnConfig) { c.AttestationPreference = "Directly" },
			field: "iam.webauthn.attestation_preference",
		},
		{
			name:  "user_verification",
			set:   func(c *IAMWebAuthnConfig) { c.UserVerification = "mandatory" },
			field: "iam.webauthn.user_verification",
		},
		{
			name:  "assurance",
			set:   func(c *IAMWebAuthnConfig) { c.Assurance = "software" },
			field: "iam.webauthn.assurance",
		},
		{
			name:  "authenticator_attachment",
			set:   func(c *IAMWebAuthnConfig) { c.AuthenticatorAttachment = "internal" },
			field: "iam.webauthn.authenticator_attachment",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := webauthnConfigWithDefaults()
			tc.set(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("a value outside the set was accepted")
			}
			var invalid *ConfigValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("want *ConfigValidationError, got %T: %v", err, err)
			}
			if invalid.Field != tc.field {
				t.Errorf("Field = %q, want %q", invalid.Field, tc.field)
			}
		})
	}
}
