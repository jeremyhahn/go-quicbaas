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

package config

import (
	"errors"
	"strings"
	"testing"
)

// TestIAMWebAuthnConfig_Validate_HappyPath verifies that a fully populated
// configuration with realistic production values passes validation cleanly.
func TestIAMWebAuthnConfig_Validate_HappyPath(t *testing.T) {
	cfg := &IAMWebAuthnConfig{
		RPID:                    "example.com",
		RPDisplayName:           "Example, Inc.",
		RPOrigins:               []string{"https://app.example.com", "https://admin.example.com"},
		Timeout:                 "60s",
		UserVerification:        "preferred",
		AttestationPreference:   "none",
		ResidentKeyRequirement:  "preferred",
		AuthenticatorAttachment: "platform",
		Debug:                   false,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

// TestIAMWebAuthnConfig_Validate_MinimalRequiredFields verifies that the
// minimum-required field set (RPID + RPDisplayName + at least one origin)
// is sufficient when the optional ceremony-policy fields are empty.
func TestIAMWebAuthnConfig_Validate_MinimalRequiredFields(t *testing.T) {
	cfg := &IAMWebAuthnConfig{
		RPID:          "localhost",
		RPDisplayName: "QuicBaaS Dev",
		RPOrigins:     []string{"https://localhost"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected minimal config to validate, got: %v", err)
	}
}

// TestIAMWebAuthnConfig_Validate_RequiredFieldErrors exercises every
// required-field branch in Validate with a table-driven test.
func TestIAMWebAuthnConfig_Validate_RequiredFieldErrors(t *testing.T) {
	cases := []struct {
		name      string
		cfg       IAMWebAuthnConfig
		wantField string
	}{
		{
			name: "missing rp_id",
			cfg: IAMWebAuthnConfig{
				RPDisplayName: "QuicBaaS",
				RPOrigins:     []string{"https://localhost"},
			},
			wantField: "iam.webauthn.rp_id",
		},
		{
			name: "missing rp_display_name",
			cfg: IAMWebAuthnConfig{
				RPID:      "example.com",
				RPOrigins: []string{"https://example.com"},
			},
			wantField: "iam.webauthn.rp_display_name",
		},
		{
			name: "missing rp_origins (nil)",
			cfg: IAMWebAuthnConfig{
				RPID:          "example.com",
				RPDisplayName: "Example",
			},
			wantField: "iam.webauthn.rp_origins",
		},
		{
			name: "rp_origins empty slice",
			cfg: IAMWebAuthnConfig{
				RPID:          "example.com",
				RPDisplayName: "Example",
				RPOrigins:     []string{},
			},
			wantField: "iam.webauthn.rp_origins",
		},
		{
			name: "rp_origins contains empty string",
			cfg: IAMWebAuthnConfig{
				RPID:          "example.com",
				RPDisplayName: "Example",
				RPOrigins:     []string{"https://example.com", ""},
			},
			wantField: "iam.webauthn.rp_origins",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			var cve *ConfigValidationError
			if !errors.As(err, &cve) {
				t.Fatalf("expected *ConfigValidationError, got %T: %v", err, err)
			}
			if cve.Field != tc.wantField {
				t.Fatalf("expected field %q, got %q", tc.wantField, cve.Field)
			}
		})
	}
}

// TestIAMWebAuthnConfig_Validate_PolicyEnumErrors exercises the enum
// validation for ceremony policy fields.
func TestIAMWebAuthnConfig_Validate_PolicyEnumErrors(t *testing.T) {
	base := IAMWebAuthnConfig{
		RPID:          "example.com",
		RPDisplayName: "Example",
		RPOrigins:     []string{"https://example.com"},
	}
	cases := []struct {
		name      string
		mutate    func(*IAMWebAuthnConfig)
		wantField string
	}{
		{
			name:      "user_verification invalid",
			mutate:    func(c *IAMWebAuthnConfig) { c.UserVerification = "yolo" },
			wantField: "iam.webauthn.user_verification",
		},
		{
			name:      "attestation_preference invalid",
			mutate:    func(c *IAMWebAuthnConfig) { c.AttestationPreference = "magic" },
			wantField: "iam.webauthn.attestation_preference",
		},
		{
			name:      "resident_key_requirement invalid",
			mutate:    func(c *IAMWebAuthnConfig) { c.ResidentKeyRequirement = "always" },
			wantField: "iam.webauthn.resident_key_requirement",
		},
		{
			name:      "authenticator_attachment invalid",
			mutate:    func(c *IAMWebAuthnConfig) { c.AuthenticatorAttachment = "wireless" },
			wantField: "iam.webauthn.authenticator_attachment",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			var cve *ConfigValidationError
			if !errors.As(err, &cve) {
				t.Fatalf("expected *ConfigValidationError, got %T: %v", err, err)
			}
			if cve.Field != tc.wantField {
				t.Fatalf("expected field %q, got %q (msg=%q)", tc.wantField, cve.Field, cve.Message)
			}
		})
	}
}

// TestIAMWebAuthnConfig_Validate_OriginIndexInMessage verifies the error
// message points at the offending origin index when an origin is empty.
func TestIAMWebAuthnConfig_Validate_OriginIndexInMessage(t *testing.T) {
	cfg := &IAMWebAuthnConfig{
		RPID:          "example.com",
		RPDisplayName: "Example",
		RPOrigins:     []string{"https://example.com", "https://admin.example.com", ""},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "entry 2") {
		t.Fatalf("expected error message to identify entry index 2, got: %v", err)
	}
}
