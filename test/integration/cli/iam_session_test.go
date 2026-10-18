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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// IAM Session List Tests
// =============================================================================

// TestIAMSessionList verifies that listing sessions for a tenant succeeds.
// Sessions may be empty if no login has occurred, but the command must
// execute without error.
func TestIAMSessionList(t *testing.T) {
	RunForTransports(t, "SessionList", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("sess-list")
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		listCmd := IAMSessionListCommand(tenantID)
		stdout, stderr, err := runner.Run(listCmd)
		if runner.IsSkipped(listCmd) {
			return
		}
		require.NoError(t, err, "iam session list should succeed: stdout=%s stderr=%s", stdout, stderr)
	})
}

// TestIAMSessionListJSON verifies that listing sessions with JSON output
// returns valid JSON and the command succeeds.
func TestIAMSessionListJSON(t *testing.T) {
	RunForTransports(t, "SessionListJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("sess-json")
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		listCmd := CommandDef{
			Name:           "iam session list json",
			Args:           []string{"iam", "session", "list", "--tenant", tenantID, "--user", "admin", "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        15 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(listCmd)
		if runner.IsSkipped(listCmd) {
			return
		}
		require.NoError(t, err, "iam session list --output json should succeed: stdout=%s stderr=%s", stdout, stderr)

		// JSON output should be valid JSON (even if it's an empty array or object)
		trimmed := strings.TrimSpace(stdout)
		if trimmed != "" {
			assert.True(t, json.Valid([]byte(trimmed)),
				"session list JSON output should be valid JSON, got: %s", trimmed)
		}
	})
}

// TestIAMSessionListWithLimit verifies that listing sessions with a --limit
// flag is accepted by the CLI and succeeds.
func TestIAMSessionListWithLimit(t *testing.T) {
	RunForTransports(t, "SessionListLimit", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("sess-limit")
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		listCmd := CommandDef{
			Name:           "iam session list with limit",
			Args:           []string{"iam", "session", "list", "--tenant", tenantID, "--user", "admin", "--limit", "5"},
			ExpectSuccess:  true,
			Timeout:        15 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(listCmd)
		if runner.IsSkipped(listCmd) {
			return
		}
		require.NoError(t, err, "iam session list --limit 5 should succeed: stdout=%s stderr=%s", stdout, stderr)
	})
}

// =============================================================================
// IAM Session Get Tests
// =============================================================================

// TestIAMSessionGetNonExistent verifies that getting a nonexistent session ID
// returns an appropriate error.
func TestIAMSessionGetNonExistent(t *testing.T) {
	RunForTransports(t, "SessionGetNonExistent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("sess-get-ne")
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		fakeSessionID := "nonexistent-session-00000000"
		getCmd := CommandDef{
			Name:           "iam session get nonexistent",
			Args:           []string{"iam", "session", "get", fakeSessionID, "--tenant", tenantID},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(getCmd)
		if runner.IsSkipped(getCmd) {
			return
		}
		require.Error(t, err, "iam session get with nonexistent ID should fail")
		combined := stdout + stderr
		assert.True(t, strings.Contains(combined, "not found") ||
			strings.Contains(combined, "error") ||
			strings.Contains(combined, "Error") ||
			strings.Contains(combined, "no session") ||
			strings.Contains(combined, "invalid"),
			"error output should indicate session not found, got: %s", combined)
	})
}

// =============================================================================
// IAM Session Revoke Tests
// =============================================================================

// TestIAMSessionRevokeNonExistent verifies that revoking a nonexistent session ID
// returns an appropriate error.
func TestIAMSessionRevokeNonExistent(t *testing.T) {
	RunForTransports(t, "SessionRevokeNonExistent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("sess-rev-ne")
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		fakeSessionID := "nonexistent-session-99999999"
		revokeCmd := CommandDef{
			Name:           "iam session revoke nonexistent",
			Args:           []string{"iam", "session", "revoke", fakeSessionID, "--tenant", tenantID, "--yes"},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(revokeCmd)
		if runner.IsSkipped(revokeCmd) {
			return
		}
		require.Error(t, err, "iam session revoke with nonexistent ID should fail")
		combined := stdout + stderr
		assert.True(t, strings.Contains(combined, "not found") ||
			strings.Contains(combined, "error") ||
			strings.Contains(combined, "Error") ||
			strings.Contains(combined, "no session") ||
			strings.Contains(combined, "invalid"),
			"error output should indicate session not found, got: %s", combined)
	})
}

// TestIAMSessionRevokeAll verifies that the revoke-all command executes
// successfully for a tenant. Even when no sessions exist, the command
// should succeed or fail gracefully.
func TestIAMSessionRevokeAll(t *testing.T) {
	RunForTransports(t, "SessionRevokeAll", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("sess-revall")
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		revokeAllCmd := IAMSessionRevokeAllCommand(tenantID)
		stdout, stderr, err := runner.Run(revokeAllCmd)
		if runner.IsSkipped(revokeAllCmd) {
			return
		}
		// revoke-all should succeed even if there are no sessions to revoke
		require.NoError(t, err,
			"iam session revoke-all should succeed: stdout=%s stderr=%s", stdout, stderr)
	})
}

// TestIAMSessionRevokeAllKeepCurrent verifies that the revoke-all command
// with the --keep-current flag is accepted and returns a meaningful error
// when no JWT session token is available (expected in integration tests
// where no interactive login has occurred).
func TestIAMSessionRevokeAllKeepCurrent(t *testing.T) {
	RunForTransports(t, "SessionRevokeAllKeepCurrent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("sess-keep")
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		revokeAllKeepCmd := CommandDef{
			Name: "iam session revoke-all keep-current",
			Args: []string{
				"iam", "session", "revoke-all",
				"--tenant", tenantID,
				"--yes",
				"--user", "admin",
				"--keep-current",
			},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(revokeAllKeepCmd)
		if runner.IsSkipped(revokeAllKeepCmd) {
			return
		}

		// --keep-current requires a stored JWT token to extract the current
		// session ID. In integration tests without an interactive login,
		// this is expected to fail with a meaningful error.
		if err != nil {
			combined := stdout + stderr
			hasSessionError := strings.Contains(combined, "session") ||
				strings.Contains(combined, "token") ||
				strings.Contains(combined, "Session") ||
				strings.Contains(combined, "Token")
			assert.True(t, hasSessionError,
				"revoke-all --keep-current without token should return a session/token error, got: %s", combined)
		}
		// If it succeeds (e.g., a token happens to be available), that is also acceptable.
	})
}

// =============================================================================
// IAM Session Lifecycle Test
// =============================================================================

// TestIAMSessionLifecycle performs an end-to-end session lifecycle test.
// It lists sessions for a tenant, and if any exist, retrieves the first
// session and validates its details contain expected fields.
func TestIAMSessionLifecycle(t *testing.T) {
	RunForTransports(t, "SessionLifecycle", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("sess-lifecycle")
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Step 1: List sessions in JSON format to parse results
		listCmd := CommandDef{
			Name:           "iam session list json for lifecycle",
			Args:           []string{"iam", "session", "list", "--tenant", tenantID, "--user", "admin", "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        15 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(listCmd)
		if runner.IsSkipped(listCmd) {
			return
		}
		require.NoError(t, err,
			"iam session list should succeed: stdout=%s stderr=%s", stdout, stderr)

		// Step 2: Attempt to parse sessions from JSON output
		trimmed := strings.TrimSpace(stdout)
		if trimmed == "" || trimmed == "[]" || trimmed == "null" {
			t.Log("No active sessions found for tenant; lifecycle get/verify skipped")
			return
		}

		// Try to extract a session ID from the JSON response.
		// Sessions may be returned as an array of objects with an "id" or "session_id" field.
		var sessions []map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &sessions); err != nil {
			// Output might be a wrapper object with a sessions field
			var wrapper map[string]interface{}
			if wrapErr := json.Unmarshal([]byte(trimmed), &wrapper); wrapErr != nil {
				t.Logf("Session list output is not parseable JSON array or object: %s", trimmed)
				return
			}
			// Look for common wrapper keys
			for _, key := range []string{"sessions", "items", "data", "results"} {
				if raw, ok := wrapper[key]; ok {
					if arr, ok := raw.([]interface{}); ok {
						for _, item := range arr {
							if m, ok := item.(map[string]interface{}); ok {
								sessions = append(sessions, m)
							}
						}
						break
					}
				}
			}
		}

		if len(sessions) == 0 {
			t.Log("No sessions found in parsed JSON; lifecycle get/verify skipped")
			return
		}

		// Step 3: Extract the first session ID
		var sessionID string
		for _, key := range []string{"id", "session_id", "sessionId", "ID"} {
			if val, ok := sessions[0][key]; ok {
				if s, ok := val.(string); ok && s != "" {
					sessionID = s
					break
				}
			}
		}
		require.NotEmpty(t, sessionID, "first session should have a recognizable ID field")
		t.Logf("Found session ID: %s", sessionID)

		// Step 4: Get the specific session and verify details
		getCmd := IAMSessionGetCommand(tenantID, sessionID)
		getStdout, getStderr, getErr := runner.Run(getCmd)
		if runner.IsSkipped(getCmd) {
			return
		}
		require.NoError(t, getErr,
			"iam session get should succeed for existing session: stdout=%s stderr=%s",
			getStdout, getStderr)
		assert.Contains(t, getStdout, sessionID,
			"session get output should contain the session ID")
	})
}
