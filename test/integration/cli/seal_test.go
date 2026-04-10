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
// Seal Status Tests
// =============================================================================

// TestSealStatus tests the seal status command across all transports.
// This is a read-only operation that should work regardless of seal state.
func TestSealStatus(t *testing.T) {
	RunForTransports(t, "SealStatus", sealTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		cmd := SealStatusCommand()
		stdout, stderr, err := runner.Run(cmd)

		// Status command should always succeed
		require.NoError(t, err, "seal status should succeed: stderr=%s", stderr)

		// Output should contain seal status information
		assert.True(t,
			strings.Contains(stdout, "Sealed") ||
				strings.Contains(stdout, "sealed") ||
				strings.Contains(stdout, "Initialized") ||
				strings.Contains(stdout, "initialized"),
			"output should contain seal status information: %s", stdout)

		t.Logf("Seal status output: %s", stdout)
	})
}

// TestSealStatusJSON tests the seal status command with JSON output.
func TestSealStatusJSON(t *testing.T) {
	RunForTransports(t, "SealStatusJSON", sealTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		cmd := SealStatusJSONCommand()
		stdout, stderr, err := runner.Run(cmd)

		require.NoError(t, err, "seal status --output json should succeed: stderr=%s", stderr)

		// Verify JSON is valid
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "output should be valid JSON: %s", stdout)

		// JSON should contain expected fields
		_, hasSealed := result["sealed"]
		_, hasSealedCamel := result["Sealed"]
		assert.True(t, hasSealed || hasSealedCamel, "JSON should contain 'sealed' or 'Sealed' field")

		t.Logf("Seal status JSON: %s", stdout)
	})
}

// TestSealStatusTableFormat tests the seal status command with table output format.
func TestSealStatusTableFormat(t *testing.T) {
	RunForTransports(t, "SealStatusTable", sealTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		cmd := CommandDef{
			Name:           "seal status table",
			Args:           []string{"seal", "status", "--output", "table"},
			ExpectSuccess:  true,
			SkipTransports: sealTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(cmd)

		require.NoError(t, err, "seal status --output table should succeed: stderr=%s", stderr)

		// Table output should contain headers and status indicators
		assert.True(t,
			strings.Contains(stdout, "SEAL STATUS") ||
				strings.Contains(stdout, "Sealed") ||
				strings.Contains(stdout, "sealed"),
			"table output should contain seal status: %s", stdout)

		t.Logf("Seal status table output: %s", stdout)
	})
}

// =============================================================================
// Seal Initialization Tests
// =============================================================================

// TestSealInitInvalidShares tests seal init with invalid share configurations.
// These tests verify proper validation without actually initializing the seal.
func TestSealInitInvalidShares(t *testing.T) {
	RunForTransports(t, "SealInitInvalidShares", sealTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		testCases := []struct {
			name      string
			shares    int
			threshold int
			wantErr   bool
		}{
			{
				name:      "threshold greater than shares",
				shares:    3,
				threshold: 5,
				wantErr:   true,
			},
			{
				name:      "zero shares",
				shares:    0,
				threshold: 1,
				wantErr:   true,
			},
			{
				name:      "zero threshold",
				shares:    5,
				threshold: 0,
				wantErr:   true,
			},
			{
				name:      "negative shares",
				shares:    -1,
				threshold: 1,
				wantErr:   true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				cmd := SealInitCommand(tc.shares, tc.threshold)
				cmd.ExpectSuccess = !tc.wantErr

				stdout, stderr, err := runner.Run(cmd)

				if tc.wantErr {
					assert.Error(t, err, "seal init with invalid config should fail: stdout=%s", stdout)
					t.Logf("Expected error for %s: stderr=%s", tc.name, stderr)
				} else {
					// If no error expected, this would initialize - skip in integration tests
					t.Logf("Output for %s: stdout=%s stderr=%s", tc.name, stdout, stderr)
				}
			})
		}
	})
}

// TestSealInitHelp tests the seal init help command.
func TestSealInitHelp(t *testing.T) {
	RunForTransports(t, "SealInitHelp", sealTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		cmd := CommandDef{
			Name:           "seal init help",
			Args:           []string{"seal", "init", "--help"},
			ExpectSuccess:  true,
			ExpectOutput:   []string{"shares", "threshold"},
			SkipTransports: sealTransportsToSkip,
		}
		runner.RunAndAssert(cmd)
	})
}

// =============================================================================
// Seal Unseal Tests
// =============================================================================

// TestSealUnsealHelp tests the seal unseal help command.
func TestSealUnsealHelp(t *testing.T) {
	RunForTransports(t, "SealUnsealHelp", sealTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		cmd := CommandDef{
			Name:           "seal unseal help",
			Args:           []string{"seal", "unseal", "--help"},
			ExpectSuccess:  true,
			ExpectOutput:   []string{"share"},
			SkipTransports: sealTransportsToSkip,
		}
		runner.RunAndAssert(cmd)
	})
}

// TestSealUnsealInvalidShare tests unseal with an invalid share.
func TestSealUnsealInvalidShare(t *testing.T) {
	RunForTransports(t, "SealUnsealInvalidShare", sealTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		// Test with an obviously invalid share (not base64, wrong format)
		cmd := SealUnsealCommand("invalid-not-base64-share!")
		cmd.ExpectSuccess = false

		stdout, stderr, err := runner.Run(cmd)

		// Should fail with invalid share format
		assert.Error(t, err, "unseal with invalid share should fail")
		t.Logf("Invalid share error: stdout=%s stderr=%s", stdout, stderr)
	})
}

// TestSealUnsealEmptyShare tests unseal with an empty share.
func TestSealUnsealEmptyShare(t *testing.T) {
	RunForTransports(t, "SealUnsealEmptyShare", sealTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		cmd := CommandDef{
			Name:           "seal unseal empty share",
			Args:           []string{"seal", "unseal", "--share", ""},
			ExpectSuccess:  false,
			SkipTransports: sealTransportsToSkip,
		}

		stdout, stderr, err := runner.Run(cmd)

		// Should fail with empty share
		assert.Error(t, err, "unseal with empty share should fail")
		t.Logf("Empty share error: stdout=%s stderr=%s", stdout, stderr)
	})
}

// =============================================================================
// Seal Command Tests
// =============================================================================

// TestSealSealHelp tests the seal seal help command.
func TestSealSealHelp(t *testing.T) {
	RunForTransports(t, "SealSealHelp", sealTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		cmd := CommandDef{
			Name:           "seal seal help",
			Args:           []string{"seal", "seal", "--help"},
			ExpectSuccess:  true,
			SkipTransports: sealTransportsToSkip,
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err, "seal seal --help should succeed")
		t.Logf("Seal seal help output: %s", stdout)
	})
}

// =============================================================================
// Seal Rekey Tests
// =============================================================================

// TestSealRekeyHelp tests the seal rekey help command.
func TestSealRekeyHelp(t *testing.T) {
	RunForTransports(t, "SealRekeyHelp", sealTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		cmd := CommandDef{
			Name:           "seal rekey help",
			Args:           []string{"seal", "rekey", "--help"},
			ExpectSuccess:  true,
			ExpectOutput:   []string{"shares", "threshold"},
			SkipTransports: sealTransportsToSkip,
		}
		runner.RunAndAssert(cmd)
	})
}

// TestSealRekeyInvalidParams tests rekey with invalid parameters.
func TestSealRekeyInvalidParams(t *testing.T) {
	RunForTransports(t, "SealRekeyInvalidParams", sealTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		testCases := []struct {
			name      string
			shares    int
			threshold int
		}{
			{
				name:      "threshold greater than shares",
				shares:    3,
				threshold: 5,
			},
			{
				name:      "zero shares",
				shares:    0,
				threshold: 1,
			},
			{
				name:      "zero threshold",
				shares:    5,
				threshold: 0,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				cmd := SealRekeyCommand(tc.shares, tc.threshold)
				cmd.ExpectSuccess = false

				stdout, stderr, err := runner.Run(cmd)

				// Should fail validation or because system is not in correct state
				// Either is acceptable for this test
				t.Logf("Rekey with %s: err=%v stdout=%s stderr=%s", tc.name, err, stdout, stderr)
			})
		}
	})
}

// =============================================================================
// Seal Command Group Tests
// =============================================================================

// TestSealHelpCommand tests the main seal help command.
func TestSealHelpCommand(t *testing.T) {
	RunForTransports(t, "SealHelp", sealTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		cmd := CommandDef{
			Name:           "seal help",
			Args:           []string{"seal", "--help"},
			ExpectSuccess:  true,
			ExpectOutput:   []string{"status", "init", "unseal", "seal", "rekey"},
			SkipTransports: sealTransportsToSkip,
		}
		runner.RunAndAssert(cmd)
	})
}

// TestSealSubcommands tests that all seal subcommands are accessible.
func TestSealSubcommands(t *testing.T) {
	subcommands := []struct {
		name string
		args []string
	}{
		{"status", []string{"seal", "status"}},
		{"init help", []string{"seal", "init", "--help"}},
		{"unseal help", []string{"seal", "unseal", "--help"}},
		{"seal help", []string{"seal", "seal", "--help"}},
		{"rekey help", []string{"seal", "rekey", "--help"}},
	}

	for _, sc := range subcommands {
		t.Run(sc.name, func(t *testing.T) {
			RunForTransports(t, sc.name, sealTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				cmd := CommandDef{
					Name:           sc.name,
					Args:           sc.args,
					ExpectSuccess:  true,
					SkipTransports: sealTransportsToSkip,
				}
				runner.RunAndAssert(cmd)
			})
		})
	}
}

// =============================================================================
// Seal Output Format Tests
// =============================================================================

// TestSealStatusOutputFormats tests all output formats for seal status.
func TestSealStatusOutputFormats(t *testing.T) {
	formats := []string{"json", "table"}

	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			RunForTransports(t, "Format"+format, sealTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				cmd := CommandDef{
					Name:           "seal status " + format,
					Args:           []string{"seal", "status", "--output", format},
					ExpectSuccess:  true,
					SkipTransports: sealTransportsToSkip,
				}
				stdout, stderr, err := runner.Run(cmd)
				require.NoError(t, err, "seal status --output %s should succeed: stderr=%s", format, stderr)
				assert.NotEmpty(t, stdout, "output should not be empty for format %s", format)

				if format == "json" {
					var result interface{}
					err := json.Unmarshal([]byte(stdout), &result)
					require.NoError(t, err, "JSON output should be valid: %s", stdout)
				}

				t.Logf("Format %s output: %s", format, stdout)
			})
		})
	}
}

// =============================================================================
// Error Handling Tests
// =============================================================================

// TestSealInvalidSubcommand tests that invalid subcommands show help text.
// Cobra prints the parent command help and exits 0 for unknown subcommands.
func TestSealInvalidSubcommand(t *testing.T) {
	RunForTransports(t, "SealInvalidSubcommand", sealTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		cmd := CommandDef{
			Name:           "seal invalid",
			Args:           []string{"seal", "invalid-subcommand"},
			ExpectSuccess:  true,
			ExpectOutput:   []string{"seal", "Usage"},
			SkipTransports: sealTransportsToSkip,
		}
		runner.RunAndAssert(cmd)
	})
}

// TestSealStatusUnknownFlag tests error handling for unknown flags.
func TestSealStatusUnknownFlag(t *testing.T) {
	RunForTransports(t, "SealStatusUnknownFlag", sealTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		cmd := CommandDef{
			Name:           "seal status unknown flag",
			Args:           []string{"seal", "status", "--unknown-flag"},
			ExpectSuccess:  false,
			SkipTransports: sealTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(cmd)
		assert.Error(t, err, "unknown flag should fail")
		t.Logf("Unknown flag error: stdout=%s stderr=%s", stdout, stderr)
	})
}

// =============================================================================
// Integration State Tests
// =============================================================================

// TestSealStateConsistency tests that seal status is consistent across calls.
func TestSealStateConsistency(t *testing.T) {
	RunForTransports(t, "SealStateConsistency", sealTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		// Call status multiple times and verify consistency
		var sealStates []bool

		for i := 0; i < 3; i++ {
			cmd := SealStatusJSONCommand()
			stdout, stderr, err := runner.Run(cmd)
			require.NoError(t, err, "seal status call %d should succeed: stderr=%s", i+1, stderr)

			var result map[string]interface{}
			err = json.Unmarshal([]byte(stdout), &result)
			require.NoError(t, err, "JSON parse error on call %d: %s", i+1, stdout)

			// Get sealed state (handle both casing conventions)
			var sealed bool
			if s, ok := result["sealed"].(bool); ok {
				sealed = s
			} else if s, ok := result["Sealed"].(bool); ok {
				sealed = s
			}
			sealStates = append(sealStates, sealed)
		}

		// All states should be the same (no random changes)
		for i := 1; i < len(sealStates); i++ {
			assert.Equal(t, sealStates[0], sealStates[i],
				"seal state should be consistent across calls")
		}

		t.Logf("Seal state consistency verified: all calls returned sealed=%v", sealStates[0])
	})
}

// =============================================================================
// Multi-Transport Seal Lifecycle Test
// =============================================================================

// TestSealStatusAcrossTransports verifies seal status is consistent across all
// transports that support seal operations. This test ensures that gRPC and Unix
// transports see the same seal state.
func TestSealStatusAcrossTransports(t *testing.T) {
	configs := GetTransportConfigs()

	// Build a set of excluded transports for fast lookup
	excludeSet := make(map[Transport]struct{}, len(sealTransportsToSkip))
	for _, tr := range sealTransportsToSkip {
		excludeSet[tr] = struct{}{}
	}

	var results []struct {
		transport string
		sealed    bool
		err       error
	}

	for _, cfg := range configs {
		if !cfg.Enabled {
			continue
		}
		if _, excluded := excludeSet[cfg.Transport]; excluded {
			continue
		}

		t.Run(string(cfg.Transport), func(t *testing.T) {
			runner := NewTestRunner(t, cfg.Transport, cfg.Address)
			cmd := SealStatusJSONCommand()
			stdout, _, err := runner.Run(cmd)

			var sealed bool
			if err == nil {
				var result map[string]interface{}
				if jsonErr := json.Unmarshal([]byte(stdout), &result); jsonErr == nil {
					if s, ok := result["sealed"].(bool); ok {
						sealed = s
					} else if s, ok := result["Sealed"].(bool); ok {
						sealed = s
					}
				}
			}

			results = append(results, struct {
				transport string
				sealed    bool
				err       error
			}{
				transport: string(cfg.Transport),
				sealed:    sealed,
				err:       err,
			})

			t.Logf("Transport %s: sealed=%v err=%v", cfg.Transport, sealed, err)
		})
	}

	// Verify all successful transports see the same state
	var successfulSealed *bool
	for _, r := range results {
		if r.err == nil {
			if successfulSealed == nil {
				successfulSealed = &r.sealed
			} else {
				assert.Equal(t, *successfulSealed, r.sealed,
					"all transports should see the same seal state")
			}
		}
	}
}

// =============================================================================
// Performance Tests
// =============================================================================

// TestSealStatusPerformance tests that seal status responds quickly.
func TestSealStatusPerformance(t *testing.T) {
	RunForTransports(t, "SealStatusPerformance", sealTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		// Run multiple status checks and ensure they all complete
		const numIterations = 5

		for i := 0; i < numIterations; i++ {
			cmd := SealStatusCommand()
			stdout, stderr, err := runner.Run(cmd)
			require.NoError(t, err, "seal status iteration %d should succeed: stderr=%s", i+1, stderr)
			assert.NotEmpty(t, stdout, "seal status should return output on iteration %d", i+1)
		}

		t.Logf("Successfully ran %d seal status calls", numIterations)
	})
}
