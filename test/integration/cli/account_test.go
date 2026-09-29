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

//go:build integration

// Package cli provides E2E integration tests for the `quicbaas account`
// command tree — the pooled provider account surface.
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Helpers
// =============================================================================

// accountCredentialMarkers are the distinctive values the test credential
// carries. None of them may appear in ANY command's output on ANY transport.
var accountCredentialMarkers = []string{
	"SECRET-ACCESS-TOKEN",
	"SECRET-REFRESH-TOKEN",
	"SECRET-ENV-VALUE",
	"access_token",
	"refresh_token",
}

// accountFixtureTokenEndpoint is the operator-supplied OAuth2 token endpoint
// the test credential carries. It is never contacted: the lifecycle verbs only
// store and read the account, and no refresh is driven. A placeholder host
// keeps the suite from depending on (or documenting) a vendor endpoint.
const accountFixtureTokenEndpoint = "https://oauth2.invalid/token"

// writeAccountCredential writes a credential JSON document to a temp file and
// returns its path. Credentials are never passed as flag values, so every
// create/rotate command reads one from disk.
func writeAccountCredential(t *testing.T, accessToken string) string {
	t.Helper()
	body := map[string]interface{}{
		"access_token":  accessToken,
		"refresh_token": "SECRET-REFRESH-TOKEN",
		// The built-in claude descriptor deliberately ships NO default token
		// endpoint (no vendor documents one; see
		// TestBuiltinAccountProviders_ShipNoUnverifiedVendorDetails), and
		// ValidateCredential rejects an oauth_refresh account whose endpoint
		// resolves to empty. The operator supplies it per account, so the
		// fixture supplies a placeholder rather than a real vendor URL.
		"token_endpoint":       accountFixtureTokenEndpoint,
		"access_token_env_key": "CLAUDE_CODE_OAUTH_TOKEN",
		"env":                  map[string]string{"POOLED_ACCOUNT_MARKER": "SECRET-ENV-VALUE"},
	}
	encoded, err := json.Marshal(body)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "credential.json")
	require.NoError(t, os.WriteFile(path, encoded, 0o600))
	return path
}

// assertNoCredentialLeak fails when any credential marker appears in output.
func assertNoCredentialLeak(t *testing.T, transport Transport, command, output string) {
	t.Helper()
	for _, marker := range accountCredentialMarkers {
		assert.NotContains(t, output, marker,
			"[%s] %s leaked credential marker %q", transport, command, marker)
	}
}

// accountIDFromJSON extracts the account ID from a JSON account document.
func accountIDFromJSON(t *testing.T, transport Transport, stdout string) string {
	t.Helper()
	var info struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &info),
		"[%s] create output is not an account document: %s", transport, stdout)
	require.NotEmpty(t, info.ID, "[%s] create returned no account ID: %s", transport, stdout)
	return info.ID
}

// =============================================================================
// Command Definitions
// =============================================================================

// AccountCreateCommand creates a pooled provider account.
func AccountCreateCommand(credentialPath string) CommandDef {
	return CommandDef{
		Name: "account create",
		Args: []string{
			"account", "create",
			"--provider", "claude",
			"--display-name", "integration-pool",
			"--refresh-policy", "oauth2",
			"--credential-file", credentialPath,
			"--output", "json",
		},
		ExpectSuccess: true,
		Timeout:       20 * time.Second,
	}
}

// AccountListCommand lists pooled provider accounts.
func AccountListCommand() CommandDef {
	return CommandDef{
		Name:          "agent account list",
		Args:          []string{"account", "list", "--output", "json"},
		ExpectSuccess: true,
		Timeout:       20 * time.Second,
	}
}

// AccountGetCommand retrieves one account.
func AccountGetCommand(accountID string) CommandDef {
	return CommandDef{
		Name:          "agent account get",
		Args:          []string{"account", "get", accountID, "--output", "json"},
		ExpectSuccess: true,
		ExpectOutput:  []string{accountID},
		Timeout:       20 * time.Second,
	}
}

// AccountUpdateCommand renames an account.
func AccountUpdateCommand(accountID string) CommandDef {
	return CommandDef{
		Name: "account update",
		Args: []string{"account", "update", accountID,
			"--display-name", "integration-pool-renamed", "--output", "json"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"integration-pool-renamed"},
		Timeout:       20 * time.Second,
	}
}

// AccountSetDefaultCommand marks an account the tenant default.
func AccountSetDefaultCommand(accountID string) CommandDef {
	return CommandDef{
		Name:          "agent account set-default",
		Args:          []string{"account", "set-default", accountID, "--output", "json"},
		ExpectSuccess: true,
		ExpectOutput:  []string{accountID},
		Timeout:       20 * time.Second,
	}
}

// AccountRotateCommand replaces an account's credential.
func AccountRotateCommand(accountID, credentialPath string) CommandDef {
	return CommandDef{
		Name: "account rotate",
		Args: []string{"account", "rotate", accountID,
			"--credential-file", credentialPath, "--output", "json"},
		ExpectSuccess: true,
		ExpectOutput:  []string{accountID},
		Timeout:       20 * time.Second,
	}
}

// AccountRevokeCommand revokes an account.
func AccountRevokeCommand(accountID string) CommandDef {
	return CommandDef{
		Name: "account revoke", Args: []string{"account", "revoke", accountID,
			"--reason", "integration test", "--output", "json"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"revoked"},
		Timeout:       20 * time.Second,
	}
}

// AccountDeleteCommand deletes an account.
func AccountDeleteCommand(accountID string) CommandDef {
	return CommandDef{
		Name:          "agent account delete",
		Args:          []string{"account", "delete", accountID, "--output", "json"},
		ExpectSuccess: true,
		Timeout:       20 * time.Second,
	}
}

// =============================================================================
// Tests
// =============================================================================

// TestAgentAccountLifecycle exercises the full pooled-account verb set on
// every transport. It is the Phase 8 parity proof for the account surface: a
// transport that drops a verb fails here rather than silently answering
// "not supported by SDK".
func TestAgentAccountLifecycle(t *testing.T) {
	RunForAllTransports(t, "AgentAccountLifecycle", func(t *testing.T, runner *TestRunner) {
		credentialPath := writeAccountCredential(t, "SECRET-ACCESS-TOKEN")

		create := AccountCreateCommand(credentialPath)
		stdout, stderr, err := runner.Run(create)
		if runner.IsSkipped(create) {
			return
		}
		require.NoError(t, err, "[%s] create failed: stdout=%s stderr=%s", runner.transport, stdout, stderr)
		assertNoCredentialLeak(t, runner.transport, create.Name, stdout)

		accountID := accountIDFromJSON(t, runner.transport, stdout)

		// Every read/write verb in sequence. Each output is checked for
		// credential leakage in addition to the per-command assertions.
		follow := []CommandDef{
			AccountListCommand(),
			AccountGetCommand(accountID),
			AccountUpdateCommand(accountID),
			AccountSetDefaultCommand(accountID),
			AccountRotateCommand(accountID, writeAccountCredential(t, "SECRET-ACCESS-TOKEN-2")),
			AccountRevokeCommand(accountID),
			AccountDeleteCommand(accountID),
		}
		for _, cmd := range follow {
			out, errOut, runErr := runner.Run(cmd)
			require.NoError(t, runErr, "[%s] %s failed: stdout=%s stderr=%s",
				runner.transport, cmd.Name, out, errOut)
			for _, expected := range cmd.ExpectOutput {
				assert.Contains(t, out, expected,
					"[%s] %s output should contain %q", runner.transport, cmd.Name, expected)
			}
			assertNoCredentialLeak(t, runner.transport, cmd.Name, out)
		}

		// The account is gone: a get must now fail.
		gone := AccountGetCommand(accountID)
		gone.Name = "agent account get after delete"
		gone.ExpectSuccess = false
		gone.ExpectOutput = nil
		if _, _, getErr := runner.Run(gone); getErr == nil {
			t.Errorf("[%s] get succeeded for a deleted account", runner.transport)
		}
	})
}

// TestAgentAccountUsage checks the usage report verb separately: it is the one
// verb that depends on a wired CostTracker, so it is asserted to either
// succeed or fail with the explicit "usage tracker not configured" condition
// rather than an unmapped-transport error.
func TestAgentAccountUsage(t *testing.T) {
	RunForAllTransports(t, "AgentAccountUsage", func(t *testing.T, runner *TestRunner) {
		credentialPath := writeAccountCredential(t, "SECRET-ACCESS-TOKEN")

		create := AccountCreateCommand(credentialPath)
		stdout, stderr, err := runner.Run(create)
		if runner.IsSkipped(create) {
			return
		}
		require.NoError(t, err, "[%s] create failed: stdout=%s stderr=%s", runner.transport, stdout, stderr)
		accountID := accountIDFromJSON(t, runner.transport, stdout)

		usage := CommandDef{
			Name:          "agent account usage",
			Args:          []string{"account", "usage", accountID, "--output", "json"},
			ExpectSuccess: true,
			Timeout:       20 * time.Second,
		}
		out, errOut, usageErr := runner.Run(usage)
		if usageErr != nil {
			// The only acceptable failure is an explicitly unconfigured usage
			// tracker; anything else means the verb is not reachable.
			combined := out + errOut
			assert.True(t,
				strings.Contains(combined, "usage tracker not configured"),
				"[%s] usage failed for an unexpected reason: %s", runner.transport, combined)
		} else {
			assertNoCredentialLeak(t, runner.transport, usage.Name, out)
		}

		_, _, _ = runner.Run(AccountDeleteCommand(accountID))
	})
}

// TestAgentAccountInvalidInput proves the CLI rejects bad input before it
// reaches the server, identically on every transport.
func TestAgentAccountInvalidInput(t *testing.T) {
	RunForAllTransports(t, "AgentAccountInvalidInput", func(t *testing.T, runner *TestRunner) {
		credentialPath := writeAccountCredential(t, "SECRET-ACCESS-TOKEN")

		cases := []CommandDef{
			{
				Name: "create with unknown provider",
				Args: []string{"account", "create", "--provider", "gemini",
					"--credential-file", credentialPath},
				ExpectSuccess: false,
				Timeout:       15 * time.Second,
			},
			{
				Name:          "create with no credential source",
				Args:          []string{"account", "create", "--provider", "claude"},
				ExpectSuccess: false,
				Timeout:       15 * time.Second,
			},
			{
				Name: "create with unknown refresh policy",
				Args: []string{"account", "create", "--provider", "claude",
					"--refresh-policy", "magic", "--credential-file", credentialPath},
				ExpectSuccess: false,
				Timeout:       15 * time.Second,
			},
			{
				Name:          "get an unknown account",
				Args:          []string{"account", "get", "acct-does-not-exist"},
				ExpectSuccess: false,
				Timeout:       15 * time.Second,
			},
		}
		for _, cmd := range cases {
			runner.RunAndAssert(cmd)
		}
	})
}
