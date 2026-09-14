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

package server

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// xkmsAcceptedTokens mirrors go-xkms's own ceremony-policy validator
// (go-xkms@v0.8.0-alpha pkg/webauthn/config.go Validate), which switches on the
// tokens EXACTLY — no trimming, no case folding. It is restated here rather
// than imported because the point of the test is to catch quicbaas forwarding a
// value that module would refuse; sharing a normaliser with it would defeat
// that, and pkg/webauthn is behind the xkms SDK boundary.
var xkmsAcceptedTokens = map[string]map[string]bool{
	"attestation_preference":   {"": true, "none": true, "indirect": true, "direct": true, "enterprise": true},
	"user_verification":        {"": true, "required": true, "preferred": true, "discouraged": true},
	"resident_key":             {"": true, "required": true, "preferred": true, "discouraged": true},
	"authenticator_attachment": {"": true, "platform": true, "cross-platform": true},
}

// TestQuicbaasWebAuthnConfigToXKMS_ForwardsCanonicalTokens is the end-to-end
// half of the canonicalisation fix.
//
// Asserting that Validate() ACCEPTS a padded, mixed-case value proves only that
// the gate is permissive. What matters is the value that leaves this process:
// these fields are handed to the composed xkms module, whose validator matches
// the tokens exactly, so a config that passed startup here used to fail later
// when the WebAuthn service was built. This test follows the value all the way
// to the handoff.
func TestQuicbaasWebAuthnConfigToXKMS_ForwardsCanonicalTokens(t *testing.T) {
	cfg := config.IAMWebAuthnConfig{
		RPID:                    "example.com",
		RPDisplayName:           "Example",
		RPOrigins:               []string{"https://example.com"},
		AttestationPreference:   " Direct ",
		UserVerification:        "REQUIRED",
		ResidentKeyRequirement:  "  Preferred",
		AuthenticatorAttachment: "Cross-Platform ",
	}

	// The boot path validates before anything is forwarded; that is where
	// canonicalisation happens.
	if err := cfg.Validate(); err != nil {
		t.Fatalf("documented values were refused at boot: %v", err)
	}

	forwarded := quicbaasWebAuthnConfigToXKMS(cfg, true)

	for _, tc := range []struct {
		field string
		got   string
		want  string
	}{
		{"attestation_preference", forwarded.AttestationPreference, "direct"},
		{"user_verification", forwarded.UserVerification, "required"},
		{"resident_key", forwarded.ResidentKey, "preferred"},
		{"authenticator_attachment", forwarded.AuthenticatorAttachment, "cross-platform"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s forwarded as %q, want %q", tc.field, tc.got, tc.want)
		}
		if !xkmsAcceptedTokens[tc.field][tc.got] {
			t.Errorf("%s = %q is not a token go-xkms accepts; the WebAuthn service "+
				"will fail to build after this config passed startup validation", tc.field, tc.got)
		}
	}
}

// TestQuicbaasWebAuthnConfigToXKMS_ForwardsEmptyAsEmpty keeps the fix from
// inventing a value. An unset field selects the library default on both sides,
// and canonicalisation must not turn it into something.
func TestQuicbaasWebAuthnConfigToXKMS_ForwardsEmptyAsEmpty(t *testing.T) {
	cfg := config.IAMWebAuthnConfig{
		RPID:          "example.com",
		RPDisplayName: "Example",
		RPOrigins:     []string{"https://example.com"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("an unset ceremony policy was refused: %v", err)
	}

	forwarded := quicbaasWebAuthnConfigToXKMS(cfg, true)
	for field, got := range map[string]string{
		"attestation_preference":   forwarded.AttestationPreference,
		"user_verification":        forwarded.UserVerification,
		"resident_key":             forwarded.ResidentKey,
		"authenticator_attachment": forwarded.AuthenticatorAttachment,
	} {
		if got != "" {
			t.Errorf("%s = %q, want empty — an unset policy must stay unset", field, got)
		}
	}
}
