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

//go:build integration

package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Ceremony Group Discovery Tests
// =============================================================================

// TestCeremonyHelp verifies the top-level ceremony command group renders its
// help text and advertises every canonical verb. The help path does not hit
// the network, so it runs on every transport.
func TestCeremonyHelp(t *testing.T) {
	RunForTransports(t, "CeremonyHelp", ceremonyTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(CeremonyHelpCommand())
	})
}

// TestCeremonySubcommandHelp verifies every ceremony verb advertises its own
// help text and required flags. Catches accidental flag-deletion regressions
// across the entire verb tree on every transport.
func TestCeremonySubcommandHelp(t *testing.T) {
	verbs := []string{
		"status",
		"claim-cert",
		"claim-share",
		"sign-csr",
		"enroll-external-officer",
	}
	for _, verb := range verbs {
		verb := verb
		t.Run(verb, func(t *testing.T) {
			RunForTransports(t, "CeremonyVerbHelp/"+verb, ceremonyTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				runner.RunAndAssert(CeremonyVerbHelpCommand(verb))
			})
		})
	}
}

// =============================================================================
// Ceremony Status Tests (read-only — safe for shared daemon)
// =============================================================================

// TestCeremonyStatus exercises the read-only ceremony status verb on every
// transport. Status is a pure read and does not mutate ceremony state, so the
// test is safe to run against the shared integration daemon. If the daemon
// has not wired the ceremony service, the test skips after validating the
// daemon's "unavailable" response shape.
func TestCeremonyStatus(t *testing.T) {
	RunForTransports(t, "CeremonyStatus", ceremonyTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCeremonyAvailable() {
			return
		}
		cmd := CeremonyStatusCommand()
		stdout, stderr, err := runner.Run(cmd)
		require.NoError(t, err, "ceremony status should succeed: stderr=%s", stderr)
		// Default (table) output renders a "State:" or "CEREMONY STATUS" banner
		// from formatInitStatusResponse. Be liberal about which substring we
		// pin so the test survives table-format tweaks.
		assert.True(t,
			strings.Contains(stdout, "State") ||
				strings.Contains(stdout, "CEREMONY STATUS"),
			"ceremony status output should describe the state: %s", stdout)
		t.Logf("ceremony status output: %s", stdout)
	})
}

// TestCeremonyStatusJSON exercises the status verb with JSON output and
// validates that the payload parses and contains the expected "state" key.
func TestCeremonyStatusJSON(t *testing.T) {
	RunForTransports(t, "CeremonyStatusJSON", ceremonyTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCeremonyAvailable() {
			return
		}
		cmd := CeremonyStatusJSONCommand()
		stdout, stderr, err := runner.Run(cmd)
		require.NoError(t, err, "ceremony status --output json should succeed: stderr=%s", stderr)

		var payload map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(stdout), &payload),
			"ceremony status --output json must emit valid JSON, got: %s", stdout)

		// The CLI's formatInitStatusResponse always emits a "state" key. The
		// "spki_pin" key is only present once the ceremony has progressed
		// past the initial state, so we only assert on "state".
		_, hasState := payload["state"]
		assert.True(t, hasState, "JSON payload should contain a 'state' key: %s", stdout)

		t.Logf("ceremony status JSON: %s", stdout)
	})
}

// TestCeremonyStatusOutputFormats sweeps every supported format value
// (json, table, yaml) and verifies each path renders non-empty output. JSON
// payloads are additionally validated for syntactic correctness.
func TestCeremonyStatusOutputFormats(t *testing.T) {
	formats := []string{"json", "table", "yaml"}
	for _, format := range formats {
		format := format
		t.Run(format, func(t *testing.T) {
			RunForTransports(t, "CeremonyStatusFormat/"+format, ceremonyTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				if !runner.RequireCeremonyAvailable() {
					return
				}
				cmd := CeremonyStatusFormatCommand(format)
				stdout, stderr, err := runner.Run(cmd)
				require.NoError(t, err,
					"ceremony status --output %s should succeed: stderr=%s", format, stderr)
				assert.NotEmpty(t, stdout, "ceremony status output should not be empty for format %s", format)

				if format == "json" {
					var v interface{}
					require.NoError(t, json.Unmarshal([]byte(stdout), &v),
						"--output json must emit valid JSON: %s", stdout)
				}
				t.Logf("[format=%s] %s", format, stdout)
			})
		})
	}
}

// TestCeremonyStatusConsistency runs ceremony status multiple times and
// asserts the reported state is stable across calls (no flapping). State is
// daemon-global, so it must not change between successive read-only calls.
func TestCeremonyStatusConsistency(t *testing.T) {
	RunForTransports(t, "CeremonyStatusConsistency", ceremonyTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireCeremonyAvailable() {
			return
		}

		var states []string
		for i := 0; i < 3; i++ {
			cmd := CeremonyStatusJSONCommand()
			stdout, stderr, err := runner.Run(cmd)
			require.NoError(t, err, "ceremony status iteration %d should succeed: stderr=%s", i+1, stderr)

			var payload map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(stdout), &payload),
				"iteration %d JSON parse: %s", i+1, stdout)

			state, _ := payload["state"].(string)
			states = append(states, state)
		}

		for i := 1; i < len(states); i++ {
			assert.Equal(t, states[0], states[i],
				"ceremony state should be consistent across calls (call %d differs from call 1)", i+1)
		}

		t.Logf("ceremony state consistent across %d calls: %q", len(states), states[0])
	})
}

// =============================================================================
// Ceremony Claim-Cert Tests
// =============================================================================

// TestCeremonyClaimCertRequiredFlags drives the claim-cert verb with each
// required flag omitted in turn and asserts the CLI rejects every variant
// before contacting the server. Required-flag validation is client-side and
// transport-independent, so all transports exercise the same code path.
func TestCeremonyClaimCertRequiredFlags(t *testing.T) {
	omitted := []string{"username", "csr-key", "output"}
	for _, flag := range omitted {
		flag := flag
		t.Run("missing-"+flag, func(t *testing.T) {
			RunForTransports(t, "ClaimCertMissing/"+flag, ceremonyTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				cmd := CeremonyClaimCertMissingFlagCommand(flag)
				stdout, stderr, err := runner.Run(cmd)
				require.Error(t, err,
					"ceremony claim-cert missing --%s should fail: stdout=%s", flag, stdout)
				combined := stdout + stderr
				assert.True(t,
					strings.Contains(combined, "--"+flag) ||
						strings.Contains(combined, "required"),
					"error output should mention the missing --%s flag: %s", flag, combined)
				t.Logf("[%s] claim-cert missing --%s rejected: %s", runner.transport, flag, combined)
			})
		})
	}
}

// =============================================================================
// Ceremony Claim-Share Tests
// =============================================================================

// TestCeremonyClaimShareRequiredFlags exercises required-flag validation on
// the claim-share verb across all transports.
func TestCeremonyClaimShareRequiredFlags(t *testing.T) {
	omitted := []string{"username", "tls-cert", "tls-key", "output"}
	for _, flag := range omitted {
		flag := flag
		t.Run("missing-"+flag, func(t *testing.T) {
			RunForTransports(t, "ClaimShareMissing/"+flag, ceremonyTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				cmd := CeremonyClaimShareMissingFlagCommand(flag)
				stdout, stderr, err := runner.Run(cmd)
				require.Error(t, err,
					"ceremony claim-share missing --%s should fail: stdout=%s", flag, stdout)
				combined := stdout + stderr
				assert.True(t,
					strings.Contains(combined, "--"+flag) ||
						strings.Contains(combined, "required"),
					"error output should mention the missing --%s flag: %s", flag, combined)
				t.Logf("[%s] claim-share missing --%s rejected: %s", runner.transport, flag, combined)
			})
		})
	}
}

// =============================================================================
// Ceremony Sign-CSR Tests
// =============================================================================

// TestCeremonySignCSRRequiredFlags exercises required-flag validation on the
// sign-csr verb across all transports.
func TestCeremonySignCSRRequiredFlags(t *testing.T) {
	omitted := []string{"username", "so-pin", "csr", "role"}
	for _, flag := range omitted {
		flag := flag
		t.Run("missing-"+flag, func(t *testing.T) {
			RunForTransports(t, "SignCSRMissing/"+flag, ceremonyTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				cmd := CeremonySignCSRMissingFlagCommand(flag)
				stdout, stderr, err := runner.Run(cmd)
				require.Error(t, err,
					"ceremony sign-csr missing --%s should fail: stdout=%s", flag, stdout)
				combined := stdout + stderr
				assert.True(t,
					strings.Contains(combined, "--"+flag) ||
						strings.Contains(combined, "required"),
					"error output should mention the missing --%s flag: %s", flag, combined)
				t.Logf("[%s] sign-csr missing --%s rejected: %s", runner.transport, flag, combined)
			})
		})
	}
}

// =============================================================================
// Ceremony Enroll-External-Officer Tests
// =============================================================================

// TestCeremonyEnrollExternalOfficerRequiresUsername asserts the verb rejects
// a missing --username before any server interaction or file I/O.
func TestCeremonyEnrollExternalOfficerRequiresUsername(t *testing.T) {
	RunForTransports(t, "EnrollExternalOfficerMissingUsername", ceremonyTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		cmd := CeremonyEnrollExternalOfficerMissingUsernameCommand()
		stdout, stderr, err := runner.Run(cmd)
		require.Error(t, err, "enroll-external-officer without --username should fail")
		combined := stdout + stderr
		assert.True(t,
			strings.Contains(combined, "--username") ||
				strings.Contains(combined, "required"),
			"error output should mention --username: %s", combined)
	})
}

// TestCeremonyEnrollExternalOfficerMutualExclusion asserts the verb rejects
// callers that supply both --cert and --pubkey (mutually exclusive) before
// any server interaction.
func TestCeremonyEnrollExternalOfficerMutualExclusion(t *testing.T) {
	RunForTransports(t, "EnrollExternalOfficerBothCertAndPubKey", ceremonyTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		cmd := CeremonyEnrollExternalOfficerBothCertAndPubKeyCommand()
		stdout, stderr, err := runner.Run(cmd)
		require.Error(t, err, "enroll-external-officer with both --cert and --pubkey should fail")
		combined := stdout + stderr
		// The CLI emits an InitMutualExclusiveCertPubKeyError whose Error()
		// text mentions both flags. Accept either flag substring so a
		// rewording does not break the test.
		assert.True(t,
			strings.Contains(combined, "--cert") ||
				strings.Contains(combined, "--pubkey") ||
				strings.Contains(combined, "exactly one"),
			"error output should describe the cert/pubkey mutual exclusion: %s", combined)
	})
}

// TestCeremonyEnrollExternalOfficerRequiresOneOfCertOrPubKey asserts the verb
// rejects callers who supply neither --cert nor --pubkey.
func TestCeremonyEnrollExternalOfficerRequiresOneOfCertOrPubKey(t *testing.T) {
	RunForTransports(t, "EnrollExternalOfficerNoCertOrPubKey", ceremonyTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		cmd := CeremonyEnrollExternalOfficerNeitherCertNorPubKeyCommand()
		stdout, stderr, err := runner.Run(cmd)
		require.Error(t, err, "enroll-external-officer without --cert or --pubkey should fail")
		combined := stdout + stderr
		assert.True(t,
			strings.Contains(combined, "--cert") ||
				strings.Contains(combined, "--pubkey") ||
				strings.Contains(combined, "exactly one"),
			"error output should describe the cert/pubkey requirement: %s", combined)
	})
}

// TestCeremonyEnrollExternalOfficerInvalidAuthFactor asserts the verb rejects
// unknown --auth-factor values (only mtls / webauthn / piv are allowed).
func TestCeremonyEnrollExternalOfficerInvalidAuthFactor(t *testing.T) {
	RunForTransports(t, "EnrollExternalOfficerInvalidAuthFactor", ceremonyTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		cmd := CeremonyEnrollExternalOfficerInvalidAuthFactorCommand()
		stdout, stderr, err := runner.Run(cmd)
		require.Error(t, err, "enroll-external-officer with invalid --auth-factor should fail")
		combined := stdout + stderr
		assert.True(t,
			strings.Contains(combined, "auth-factor") ||
				strings.Contains(combined, "invalid"),
			"error output should describe the invalid auth factor: %s", combined)
	})
}

// =============================================================================
// Ceremony Group Error Handling
// =============================================================================

// TestCeremonyInvalidSubcommand verifies that an unknown subcommand surfaces
// the group help text rather than producing a confusing error. Cobra's
// default behavior renders the parent help and exits 0 for unknown verbs.
func TestCeremonyInvalidSubcommand(t *testing.T) {
	RunForTransports(t, "CeremonyInvalidSubcommand", ceremonyTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		cmd := CommandDef{
			Name:           "ceremony invalid-subcommand",
			Args:           []string{"ceremony", "definitely-not-a-real-verb"},
			ExpectSuccess:  true,
			ExpectOutput:   []string{"ceremony"},
			SkipTransports: ceremonyTransportsToSkip,
		}
		runner.RunAndAssert(cmd)
	})
}

// TestCeremonyStatusUnknownFlag verifies that an unknown flag on a ceremony
// verb is rejected by Cobra before the verb's RunE executes.
func TestCeremonyStatusUnknownFlag(t *testing.T) {
	RunForTransports(t, "CeremonyStatusUnknownFlag", ceremonyTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		cmd := CommandDef{
			Name:           "ceremony status unknown flag",
			Args:           []string{"ceremony", "status", "--definitely-not-a-real-flag"},
			ExpectSuccess:  false,
			SkipTransports: ceremonyTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(cmd)
		require.Error(t, err, "ceremony status with unknown flag should fail")
		t.Logf("unknown flag error: stdout=%s stderr=%s", stdout, stderr)
	})
}

// =============================================================================
// Cross-Transport Consistency
// =============================================================================

// TestCeremonyStatusAcrossTransports verifies the ceremony state observed by
// every enabled transport agrees. Ceremony state is daemon-global, so the
// "state" string returned by status --output json must be identical no matter
// which transport asks. This test is the ceremony-level analogue of
// TestSealStatusAcrossTransports.
func TestCeremonyStatusAcrossTransports(t *testing.T) {
	configs := GetTransportConfigs()

	excludeSet := make(map[Transport]struct{}, len(ceremonyTransportsToSkip))
	for _, tr := range ceremonyTransportsToSkip {
		excludeSet[tr] = struct{}{}
	}

	type result struct {
		transport Transport
		state     string
		available bool
	}
	var results []result

	for _, cfg := range configs {
		if !cfg.Enabled {
			continue
		}
		if _, excluded := excludeSet[cfg.Transport]; excluded {
			continue
		}

		t.Run(string(cfg.Transport), func(t *testing.T) {
			runner := NewTestRunner(t, cfg.Transport, cfg.Address)
			if cfg.Transport == TransportMCP {
				if err := runner.Authenticate(); err != nil {
					t.Fatalf("MCP authentication failed: %v", err)
				}
			}
			if !runner.RequireCeremonyAvailable() {
				results = append(results, result{transport: cfg.Transport, available: false})
				return
			}

			cmd := CeremonyStatusJSONCommand()
			stdout, _, err := runner.Run(cmd)
			require.NoError(t, err, "ceremony status should succeed on %s", cfg.Transport)

			var payload map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(stdout), &payload),
				"ceremony status JSON must parse on %s: %s", cfg.Transport, stdout)
			state, _ := payload["state"].(string)
			results = append(results, result{transport: cfg.Transport, state: state, available: true})
		})
	}

	// Verify all available transports observed the same state.
	var canonical *string
	for _, r := range results {
		if !r.available {
			continue
		}
		if canonical == nil {
			s := r.state
			canonical = &s
			continue
		}
		assert.Equal(t, *canonical, r.state,
			"transport %s observed ceremony state %q but expected %q",
			r.transport, r.state, *canonical)
	}
	if canonical != nil {
		t.Logf("ceremony state observed identically across %d transports: %q", len(results), *canonical)
	}
}
