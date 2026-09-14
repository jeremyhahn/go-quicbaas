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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// requireValidationField asserts that err is a *ConfigValidationError naming
// the expected config field.
func requireValidationField(t *testing.T, err error, wantField string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected validation error for %s, got nil", wantField)
	}
	var cve *ConfigValidationError
	if !errors.As(err, &cve) {
		t.Fatalf("expected *ConfigValidationError, got %T: %v", err, err)
	}
	if cve.Field != wantField {
		t.Fatalf("expected field %q, got %q (msg=%q)", wantField, cve.Field, cve.Message)
	}
}

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
	if err := cfg.ValidateResolved(); err != nil {
		t.Fatalf("a fully populated config is also a valid resolved config: %v", err)
	}
}

// TestIAMWebAuthnConfig_Validate_EmptySectionIsValid is the regression guard
// for the defect this validator used to carry: it demanded RPID,
// RPDisplayName and RPOrigins unconditionally, while the live path derives all
// three from the issuer URL. Ten of the eleven configuration files shipped in
// this repository omit the whole block, so an unconditional requirement would
// have refused to boot all but one of them.
func TestIAMWebAuthnConfig_Validate_EmptySectionIsValid(t *testing.T) {
	var cfg IAMWebAuthnConfig
	if err := cfg.Validate(); err != nil {
		t.Fatalf("an omitted iam.webauthn section derives its identity and must validate, got: %v", err)
	}
}

// TestIAMWebAuthnConfig_Validate_PartialIdentityIsValid verifies that
// supplying one half of the relying-party identity and deriving the other is
// accepted by input validation. An operator who pins rp_id but lets the origin
// follow the issuer (or the reverse) is not misconfigured.
func TestIAMWebAuthnConfig_Validate_PartialIdentityIsValid(t *testing.T) {
	cases := map[string]IAMWebAuthnConfig{
		"rp_id only":      {RPID: "example.com"},
		"rp_origins only": {RPOrigins: []string{"https://app.example.com"}},
		"display only":    {RPDisplayName: "Example, Inc."},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if err := cfg.Validate(); err != nil {
				t.Fatalf("expected partial identity to validate as input, got: %v", err)
			}
		})
	}
}

// TestIAMWebAuthnConfig_ValidateResolved_RequiresIdentity verifies the other
// half of the contract: after derivation, an RP ID, a display name and at
// least one origin are genuinely required, because a relying party without
// them accepts no ceremony.
func TestIAMWebAuthnConfig_ValidateResolved_RequiresIdentity(t *testing.T) {
	cases := []struct {
		name      string
		cfg       IAMWebAuthnConfig
		wantField string
	}{
		{
			name:      "no rp_id",
			cfg:       IAMWebAuthnConfig{RPDisplayName: "QuicBaaS", RPOrigins: []string{"https://localhost"}},
			wantField: "iam.webauthn.rp_id",
		},
		{
			name:      "no display name",
			cfg:       IAMWebAuthnConfig{RPID: "localhost", RPOrigins: []string{"https://localhost"}},
			wantField: "iam.webauthn.rp_display_name",
		},
		{
			name:      "no origins (nil)",
			cfg:       IAMWebAuthnConfig{RPID: "localhost", RPDisplayName: "QuicBaaS"},
			wantField: "iam.webauthn.rp_origins",
		},
		{
			name:      "no origins (empty slice)",
			cfg:       IAMWebAuthnConfig{RPID: "localhost", RPDisplayName: "QuicBaaS", RPOrigins: []string{}},
			wantField: "iam.webauthn.rp_origins",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireValidationField(t, tc.cfg.ValidateResolved(), tc.wantField)
			// The same shape is fine as operator input — derivation fills it in.
			if err := tc.cfg.Validate(); err != nil {
				t.Fatalf("input validation must accept a derivable identity, got: %v", err)
			}
		})
	}
}

// TestIAMWebAuthnConfig_Validate_RPIDShape exercises the RP ID rule, which is
// delegated to protocol.ValidateRPID — the same check the relying party
// applies when it is constructed. A value rejected here already fails at RP
// construction today; the change is only that the operator hears about it at
// boot instead of losing the WebAuthn surface to a log line.
func TestIAMWebAuthnConfig_Validate_RPIDShape(t *testing.T) {
	invalid := map[string]string{
		"scheme included": "https://example.com",
		"path included":   "example.com/app",
		"port included":   "example.com:8443",
		"single label":    "myserver",
	}
	for name, rpID := range invalid {
		t.Run(name, func(t *testing.T) {
			cfg := IAMWebAuthnConfig{RPID: rpID}
			requireValidationField(t, cfg.Validate(), "iam.webauthn.rp_id")
		})
	}

	// IP literals are accepted on purpose. Browsers reject them, but
	// protocol.ValidateRPID does not, the non-browser software authenticator
	// works with them, and iam_init.go offers an explicit rp_id as the
	// documented escape hatch for a node whose issuer host is an IP address.
	// Refusing them here would close that hatch.
	valid := []string{
		"example.com",
		"localhost",
		"app.example.co.uk",
		"xn--80ak6aa92e.com",
		"192.168.1.10",
		"::1",
	}
	for _, rpID := range valid {
		t.Run("valid/"+rpID, func(t *testing.T) {
			cfg := IAMWebAuthnConfig{RPID: rpID}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("expected %q to be a valid RP ID, got: %v", rpID, err)
			}
		})
	}
}

// TestIAMWebAuthnConfig_Validate_OriginShape exercises the entries that are not
// usable origins at all — the empty string a half-failed derivation produced,
// and the hostless URL a "https://" typo leaves behind.
func TestIAMWebAuthnConfig_Validate_OriginShape(t *testing.T) {
	invalid := map[string]string{
		"empty":            "",
		"no host":          "https://",
		"no host, port on": "http://:8080",
	}
	for name, origin := range invalid {
		t.Run(name, func(t *testing.T) {
			cfg := IAMWebAuthnConfig{RPOrigins: []string{origin}}
			requireValidationField(t, cfg.Validate(), "iam.webauthn.rp_origins")
		})
	}
}

// TestIAMWebAuthnConfig_Validate_OriginShapesTheRPAccepts pins the deliberate
// limit on strictness. The embedded relying party compares origins by scheme
// and host only (github.com/go-webauthn/webauthn protocol.IsOriginInHaystack),
// and matches an Android native-application origin verbatim. Every entry here
// therefore works in a live deployment, so the validator must not refuse it —
// failing closed on a working configuration is the outage this issue exists to
// avoid.
func TestIAMWebAuthnConfig_Validate_OriginShapesTheRPAccepts(t *testing.T) {
	accepted := []string{
		"https://app.example.com",
		"http://localhost:8080",
		"https://app.example.com:8443",
		"https://app.example.com/",       // path is ignored by the origin comparison
		"HTTPS://APP.EXAMPLE.COM",        // scheme and host compare case-insensitively
		"https://app.example.com?x=1",    // query is ignored
		"https://app.example.com#frag",   // fragment is ignored
		"android:apk-key-hash:abc123def", // native app origin, matched verbatim
		"app.example.com",                // not a URL origin: matched verbatim, not refused
		"myscheme://app",                 // ditto — the RP compares non-URL entries by string
		// Unparseable, but upstream pins it as an exact-string match
		// (protocol.IsOriginInHaystack, ShouldHandleInvalidURLAsSimpleStringMatch).
		"http://app.example.%%%&123?1",
	}
	for _, origin := range accepted {
		t.Run(origin, func(t *testing.T) {
			cfg := IAMWebAuthnConfig{RPOrigins: []string{origin}}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("origin %q is accepted by the relying party and must validate, got: %v", origin, err)
			}
		})
	}
}

// TestIAMWebAuthnConfig_Validate_OriginIndexInMessage verifies the error
// message points at the offending origin index.
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

// TestIAMWebAuthnConfig_Validate_Timeout verifies that a ceremony timeout is
// checked rather than silently discarded. The resolution path used to swallow
// the parse error, so a typo reverted to the library default with no signal.
func TestIAMWebAuthnConfig_Validate_Timeout(t *testing.T) {
	for _, bad := range []string{"not-a-duration", "60", "0s", "-30s"} {
		t.Run("invalid/"+bad, func(t *testing.T) {
			cfg := IAMWebAuthnConfig{Timeout: bad}
			requireValidationField(t, cfg.Validate(), "iam.webauthn.timeout")
		})
	}
	for _, good := range []string{"", "60s", "2m", "1h30m"} {
		t.Run("valid/"+good, func(t *testing.T) {
			cfg := IAMWebAuthnConfig{Timeout: good}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("expected %q to validate, got: %v", good, err)
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
			requireValidationField(t, cfg.Validate(), tc.wantField)
			// The enums are policy, not identity: both modes reject them.
			requireValidationField(t, cfg.ValidateResolved(), tc.wantField)
		})
	}
}

// TestValidator_IAMWebAuthn_WiredIntoBootGate verifies the invocation this
// issue is about: the validator is reached from the top-level configuration
// gate that cmd/quicbaasd runs before starting, not merely from its own tests.
func TestValidator_IAMWebAuthn_WiredIntoBootGate(t *testing.T) {
	v := NewValidator()
	cfg := IAMConfig{WebAuthn: IAMWebAuthnConfig{RPID: "https://example.com"}}
	v.validateIAM(&cfg)
	err := v.Errors()
	if err == nil {
		t.Fatal("expected the IAM section validator to record the malformed rp_id")
	}
	if !strings.Contains(err.Error(), "iam.webauthn.rp_id") {
		t.Fatalf("expected the error to name iam.webauthn.rp_id, got: %v", err)
	}

	// And a default config — which carries no iam.webauthn block at all —
	// must pass the full boot gate.
	clean := NewValidator()
	clean.validateIAM(&IAMConfig{})
	if err := clean.Errors(); err != nil {
		t.Fatalf("an unconfigured IAM section must pass the boot gate, got: %v", err)
	}
}

// TestIAMWebAuthnConfig_Validate_ShippedConfigsStillBoot is the evidence that
// enforcing this validator breaks no existing deployment: every configuration
// file shipped in the repository is loaded through the real loader and put
// through the boot-time gate. They all omit iam.webauthn entirely (deriving
// the relying party from the issuer URL) except the integration fixture, which
// pins it explicitly — the two shapes this contract has to keep working.
func TestIAMWebAuthnConfig_Validate_ShippedConfigsStillBoot(t *testing.T) {
	root := repoRoot(t)
	fixtures := []string{
		".quicbaas.example.yaml",
		"configs/memory.yaml",
		"configs/file-local.yaml",
		"configs/pebble-local.yaml",
		"configs/raft-single-node.yaml",
		"configs/raft-3-node/node1.yaml",
		"configs/raft-3-node/node2.yaml",
		"configs/raft-3-node/node3.yaml",
		"test/integration/quicbaas.yaml",
		"test/integration/quicbaas-cluster.yaml",
		"test/integration/quicbaas-tenantbarrier.yaml",
	}
	var derived int
	for _, rel := range fixtures {
		t.Run(rel, func(t *testing.T) {
			path := filepath.Join(root, rel)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("shipped config %s is missing: %v", rel, err)
			}
			cfg, err := LoadFromFile(path)
			if err != nil {
				t.Fatalf("loading %s failed: %v", rel, err)
			}
			if err := cfg.IAM.WebAuthn.Validate(); err != nil {
				t.Fatalf("shipped config %s no longer passes the WebAuthn boot gate: %v", rel, err)
			}
			if cfg.IAM.WebAuthn.RPID == "" {
				derived++
			}
		})
	}
	// The count is the point, not a detail: these are the configurations the
	// previous unconditional rp_id requirement would have refused to boot had
	// anyone ever called it.
	if derived == 0 {
		t.Fatal("no shipped config relies on relying-party derivation; " +
			"this test no longer proves the derive-shape deployment still boots")
	}
}

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate the module root from the test working directory")
		}
		dir = parent
	}
}

// TestIAMWebAuthnConfig_CeremonyTimeout verifies the single place the timeout
// string is interpreted: the parsed value on success, and a typed error the
// caller must handle rather than a silent fallback to the library default.
func TestIAMWebAuthnConfig_CeremonyTimeout(t *testing.T) {
	cfg := IAMWebAuthnConfig{Timeout: "90s"}
	d, err := cfg.CeremonyTimeout()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 90*time.Second {
		t.Fatalf("expected 90s, got %v", d)
	}

	unset := IAMWebAuthnConfig{}
	if d, err := unset.CeremonyTimeout(); err != nil || d != 0 {
		t.Fatalf("an unset timeout means the library default: got %v, %v", d, err)
	}
}

// TestIAMWebAuthnConfig_CeremonyTimeout_Invalid verifies the error path
// reports the offending field rather than falling back silently.
func TestIAMWebAuthnConfig_CeremonyTimeout_Invalid(t *testing.T) {
	for _, bad := range []string{"sixty", "60", "-1s", "0"} {
		t.Run(bad, func(t *testing.T) {
			cfg := IAMWebAuthnConfig{Timeout: bad}
			d, err := cfg.CeremonyTimeout()
			requireValidationField(t, err, "iam.webauthn.timeout")
			if d != 0 {
				t.Fatalf("expected zero duration alongside the error, got %v", d)
			}
		})
	}
}

// TestValidate_AssuranceMatchesTheRuntimeParser is the drift guard between
// boot-time validation and iam.ParseWebAuthnAssurance.
//
// The parser trims and lowercases, so it accepts "HARDWARE" and "  any  ".
// Validation used to do an exact map lookup, which meant a config the parser's
// own contract accepted was refused at boot — making a documented accepted
// value unusable. The two must agree on what is spellable.
func TestValidate_AssuranceMatchesTheRuntimeParser(t *testing.T) {
	for _, value := range []string{
		"", "hardware", "HARDWARE", "  Hardware  ",
		"any", "ANY", "  any  ",
		"insecure-allow-software-administrators",
		"INSECURE-ALLOW-SOFTWARE-ADMINISTRATORS",
	} {
		cfg := &IAMWebAuthnConfig{Assurance: value}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() refused assurance %q that the parser accepts: %v", value, err)
		}
	}

	// A genuine typo is still refused — normalising must not become accepting.
	for _, value := range []string{"hardwar", "none", "insecure", "any-admins"} {
		cfg := &IAMWebAuthnConfig{Assurance: value}
		if err := cfg.Validate(); err == nil {
			t.Errorf("Validate() accepted the invalid assurance %q", value)
		}
	}
}
