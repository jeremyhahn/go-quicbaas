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
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// PEP Test Helpers
// =============================================================================

// pepSetupTenant provisions a tenant for PEP tests, makes the runner act as
// its admin, and registers a deferred cleanup.
// Returns the tenant ID.
func pepSetupTenant(t *testing.T, runner *TestRunner, prefix string) string {
	t.Helper()
	tenantID := UniqueKey(prefix)
	runner.ActAsTenantAdmin(tenantID)
	t.Cleanup(func() {
		deleteCmd := IAMTenantDeleteCommand(tenantID)
		runner.Run(deleteCmd)
	})
	return tenantID
}

// writeTempBatchFile creates a temporary batch file for PEP check-batch tests
// and registers automatic cleanup via t.Cleanup.
func writeTempBatchFile(t *testing.T) string {
	t.Helper()
	content := `{
  "requests": [
    {"subject": {"id": "user1", "roles": ["admin"]}, "action": "read", "resource": "keys/foo"},
    {"subject": {"id": "user2", "roles": ["viewer"]}, "action": "write", "resource": "keys/bar"}
  ]
}`
	tmpFile, err := os.CreateTemp("", "pep-batch-*.json")
	require.NoError(t, err, "failed to create temp batch file")
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err, "failed to write temp batch file")
	require.NoError(t, tmpFile.Close(), "failed to close temp batch file")
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })
	return tmpFile.Name()
}

// writeTempBatchFileWithContent creates a temporary batch file with custom content.
func writeTempBatchFileWithContent(t *testing.T, content string) string {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "pep-batch-*.json")
	require.NoError(t, err, "failed to create temp batch file")
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err, "failed to write temp batch file")
	require.NoError(t, tmpFile.Close(), "failed to close temp batch file")
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })
	return tmpFile.Name()
}

// =============================================================================
// PEP Check Tests
// =============================================================================

// TestIAMPEPCheck tests a basic PEP access check with an admin role performing
// a read action. Verifies the output contains a Decision indicator.
func TestIAMPEPCheck(t *testing.T) {
	RunForTransports(t, "PEPCheck", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := pepSetupTenant(t, runner, "pep-check")

		checkCmd := IAMPEPCheckCommand(tenantID,
			`{"id":"user1","roles":["admin"]}`,
			"read",
			"keys/test-key")
		stdout, stderr, err := runner.Run(checkCmd)
		require.NoError(t, err, "PEP check should succeed: stdout=%s stderr=%s", stdout, stderr)

		combined := stdout + stderr
		assert.True(t,
			containsAny(combined, "Decision", "decision", "ALLOW", "DENY", "allow", "deny"),
			"PEP check output should contain a decision indicator: %s", combined)
	})
}

// TestIAMPEPCheckJSON tests PEP access check with --output json flag and verifies
// the response is valid JSON containing a decision field.
func TestIAMPEPCheckJSON(t *testing.T) {
	RunForTransports(t, "PEPCheckJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := pepSetupTenant(t, runner, "pep-check-json")

		checkCmd := CommandDef{
			Name: "iam pep check json",
			Args: []string{
				"iam", "pep", "check",
				"--tenant", tenantID,
				"--subject", `{"id":"user1","roles":["admin"]}`,
				"--action", "read",
				"--resource", "keys/test-key",
				"--output", "json",
			},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(checkCmd)
		require.NoError(t, err, "PEP check JSON should succeed: stdout=%s stderr=%s", stdout, stderr)

		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "PEP check output should be valid JSON: %s", stdout)

		// Verify the JSON response contains a decision field
		assert.True(t, len(result) > 0, "JSON response should not be empty")
		hasDecision := false
		for key := range result {
			lower := strings.ToLower(key)
			if lower == "decision" || lower == "allowed" || lower == "result" {
				hasDecision = true
				break
			}
		}
		assert.True(t, hasDecision,
			"JSON response should contain a decision-related field, got keys: %v", keysOf(result))
	})
}

// TestIAMPEPCheckWithContext tests PEP access check with additional context
// provided via the --context flag.
func TestIAMPEPCheckWithContext(t *testing.T) {
	RunForTransports(t, "PEPCheckWithContext", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := pepSetupTenant(t, runner, "pep-ctx")

		checkCmd := CommandDef{
			Name: "iam pep check with context",
			Args: []string{
				"iam", "pep", "check",
				"--tenant", tenantID,
				"--subject", `{"id":"user1","roles":["admin"]}`,
				"--action", "read",
				"--resource", "keys/test-key",
				"--context", `{"ip":"192.168.1.1","time":"2025-01-01T12:00:00Z"}`,
				"--output", "json",
			},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(checkCmd)
		require.NoError(t, err, "PEP check with context should succeed: stdout=%s stderr=%s", stdout, stderr)

		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "PEP check with context output should be valid JSON: %s", stdout)
		assert.True(t, len(result) > 0, "JSON response should not be empty")
	})
}

// TestIAMPEPCheckDeny tests that a viewer role attempting an admin-level action
// receives a DENY decision.
func TestIAMPEPCheckDeny(t *testing.T) {
	RunForTransports(t, "PEPCheckDeny", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := pepSetupTenant(t, runner, "pep-deny")

		checkCmd := CommandDef{
			Name: "iam pep check deny",
			Args: []string{
				"iam", "pep", "check",
				"--tenant", tenantID,
				"--subject", `{"id":"viewer-user","roles":["viewer"]}`,
				"--action", "admin:delete-tenant",
				"--resource", "system/tenants",
				"--output", "json",
			},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(checkCmd)
		require.NoError(t, err, "PEP check for deny should succeed (returns decision, not error): stdout=%s stderr=%s",
			stdout, stderr)

		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "PEP deny output should be valid JSON: %s", stdout)

		// Verify the decision is DENY or allowed=false
		combined := strings.ToLower(stdout)
		assert.True(t,
			strings.Contains(combined, "deny") ||
				strings.Contains(combined, `"allowed":false`) ||
				strings.Contains(combined, `"allowed": false`) ||
				strings.Contains(combined, `"decision":"deny"`),
			"PEP check for viewer on admin action should produce a DENY decision: %s", stdout)
	})
}

// TestIAMPEPCheckWithGroups tests PEP access check with a subject that has
// both roles and groups attributes.
func TestIAMPEPCheckWithGroups(t *testing.T) {
	RunForTransports(t, "PEPCheckWithGroups", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := pepSetupTenant(t, runner, "pep-groups")

		subjectJSON := `{"id":"user1","roles":["developer"],"groups":["engineering","platform"]}`
		checkCmd := CommandDef{
			Name: "iam pep check with groups",
			Args: []string{
				"iam", "pep", "check",
				"--tenant", tenantID,
				"--subject", subjectJSON,
				"--action", "read",
				"--resource", "keys/team-key",
				"--output", "json",
			},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(checkCmd)
		require.NoError(t, err, "PEP check with groups should succeed: stdout=%s stderr=%s", stdout, stderr)

		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "PEP check with groups output should be valid JSON: %s", stdout)
		assert.True(t, len(result) > 0,
			"JSON response should not be empty for subject with groups")
	})
}

// =============================================================================
// PEP Batch Check Tests
// =============================================================================

// TestIAMPEPCheckBatch tests batch PEP check using a file containing multiple
// access check requests.
func TestIAMPEPCheckBatch(t *testing.T) {
	RunForTransports(t, "PEPCheckBatch", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := pepSetupTenant(t, runner, "pep-batch")
		batchFile := writeTempBatchFile(t)

		batchCmd := IAMPEPCheckBatchCommand(tenantID, batchFile)
		stdout, stderr, err := runner.Run(batchCmd)
		require.NoError(t, err, "PEP batch check should succeed: stdout=%s stderr=%s", stdout, stderr)

		combined := stdout + stderr
		// Batch response should contain results for multiple requests
		assert.True(t,
			containsAny(combined, "Decision", "decision", "ALLOW", "DENY", "allow", "deny", "result"),
			"PEP batch check output should contain decision indicators: %s", combined)
	})
}

// TestIAMPEPCheckBatchJSON tests batch PEP check with --output json flag and
// verifies the response is valid JSON containing results for all requests.
func TestIAMPEPCheckBatchJSON(t *testing.T) {
	RunForTransports(t, "PEPCheckBatchJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := pepSetupTenant(t, runner, "pep-batch-json")
		batchFile := writeTempBatchFile(t)

		batchCmd := CommandDef{
			Name: "iam pep check-batch json",
			Args: []string{
				"iam", "pep", "check-batch",
				"--tenant", tenantID,
				"-f", batchFile,
				"--output", "json",
			},
			ExpectSuccess:  true,
			Timeout:        15 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(batchCmd)
		require.NoError(t, err, "PEP batch check JSON should succeed: stdout=%s stderr=%s", stdout, stderr)

		// Try to parse as JSON object or array
		var resultObj map[string]interface{}
		var resultArr []interface{}
		errObj := json.Unmarshal([]byte(stdout), &resultObj)
		errArr := json.Unmarshal([]byte(stdout), &resultArr)
		assert.True(t, errObj == nil || errArr == nil,
			"PEP batch check output should be valid JSON (object or array): %s", stdout)

		// Verify multiple results are present
		if errArr == nil {
			assert.GreaterOrEqual(t, len(resultArr), 2,
				"batch JSON array response should contain at least 2 results")
		} else if errObj == nil {
			// Check for a results/responses array field
			foundResults := false
			for key, val := range resultObj {
				lower := strings.ToLower(key)
				if lower == "results" || lower == "responses" || lower == "decisions" {
					if arr, ok := val.([]interface{}); ok {
						assert.GreaterOrEqual(t, len(arr), 2,
							"batch JSON %s field should contain at least 2 results", key)
						foundResults = true
					}
				}
			}
			assert.True(t, foundResults || len(resultObj) >= 2,
				"batch JSON response should contain multiple results: %v", keysOf(resultObj))
		}
	})
}

// =============================================================================
// PEP Error Handling Tests
// =============================================================================

// TestIAMPEPCheckMissingTenant tests that a PEP check without the required
// --tenant flag produces an error.
func TestIAMPEPCheckMissingTenant(t *testing.T) {
	RunForTransports(t, "PEPCheckMissingTenant", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		checkCmd := CommandDef{
			Name: "iam pep check missing tenant",
			Args: []string{
				"--tenant", "",
				"iam", "pep", "check",
				"--subject", `{"id":"user1","roles":["admin"]}`,
				"--action", "read",
				"--resource", "keys/test-key",
			},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(checkCmd)
		require.Error(t, err, "PEP check without tenant should fail")

		combined := strings.ToLower(stdout + stderr)
		assert.True(t,
			containsAny(combined, "tenant", "required", "missing", "flag"),
			"error should indicate missing tenant: stdout=%s stderr=%s", stdout, stderr)
	})
}

// TestIAMPEPCheckMissingSubject tests that a PEP check without the required
// --subject flag produces an error.
func TestIAMPEPCheckMissingSubject(t *testing.T) {
	RunForTransports(t, "PEPCheckMissingSubject", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := pepSetupTenant(t, runner, "pep-no-subj")

		checkCmd := CommandDef{
			Name: "iam pep check missing subject",
			Args: []string{
				"iam", "pep", "check",
				"--tenant", tenantID,
				"--action", "read",
				"--resource", "keys/test-key",
			},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(checkCmd)
		require.Error(t, err, "PEP check without subject should fail")

		combined := strings.ToLower(stdout + stderr)
		assert.True(t,
			containsAny(combined, "subject", "required", "missing", "flag"),
			"error should indicate missing subject: stdout=%s stderr=%s", stdout, stderr)
	})
}

// TestIAMPEPCheckInvalidSubjectJSON tests that a PEP check with malformed
// JSON in the --subject flag produces an error.
func TestIAMPEPCheckInvalidSubjectJSON(t *testing.T) {
	RunForTransports(t, "PEPCheckInvalidSubjectJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := pepSetupTenant(t, runner, "pep-bad-json")

		checkCmd := CommandDef{
			Name: "iam pep check invalid subject json",
			Args: []string{
				"iam", "pep", "check",
				"--tenant", tenantID,
				"--subject", `{invalid-json!!!`,
				"--action", "read",
				"--resource", "keys/test-key",
			},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(checkCmd)
		require.Error(t, err, "PEP check with invalid subject JSON should fail")

		combined := strings.ToLower(stdout + stderr)
		assert.True(t,
			containsAny(combined, "json", "invalid", "parse", "unmarshal", "syntax", "subject"),
			"error should indicate invalid JSON: stdout=%s stderr=%s", stdout, stderr)
	})
}

// TestIAMPEPCheckBatchInvalidFile tests that a batch PEP check referencing
// a nonexistent file produces an error.
func TestIAMPEPCheckBatchInvalidFile(t *testing.T) {
	RunForTransports(t, "PEPCheckBatchInvalidFile", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := pepSetupTenant(t, runner, "pep-bad-file")

		batchCmd := CommandDef{
			Name: "iam pep check-batch invalid file",
			Args: []string{
				"iam", "pep", "check-batch",
				"--tenant", tenantID,
				"-f", "/tmp/nonexistent-pep-batch-file-99999.json",
			},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(batchCmd)
		require.Error(t, err, "PEP batch check with nonexistent file should fail")

		combined := strings.ToLower(stdout + stderr)
		assert.True(t,
			containsAny(combined, "no such file", "not found", "does not exist", "open", "file", "read"),
			"error should indicate file not found: stdout=%s stderr=%s", stdout, stderr)
	})
}

// TestIAMPEPCheckBatchEmptyRequests tests that a batch PEP check with an empty
// requests array in the batch file produces an error.
func TestIAMPEPCheckBatchEmptyRequests(t *testing.T) {
	RunForTransports(t, "PEPCheckBatchEmptyRequests", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := pepSetupTenant(t, runner, "pep-empty-batch")
		batchFile := writeTempBatchFileWithContent(t, `{"requests": []}`)

		batchCmd := CommandDef{
			Name: fmt.Sprintf("iam pep check-batch empty requests %s", batchFile),
			Args: []string{
				"iam", "pep", "check-batch",
				"--tenant", tenantID,
				"-f", batchFile,
			},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(batchCmd)
		require.Error(t, err, "PEP batch check with empty requests should fail")

		combined := strings.ToLower(stdout + stderr)
		assert.True(t,
			containsAny(combined, "empty", "no requests", "at least", "required", "invalid"),
			"error should indicate empty requests: stdout=%s stderr=%s", stdout, stderr)
	})
}

// =============================================================================
// Utility
// =============================================================================

// containsAny returns true if s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// keysOf returns the keys of a map for diagnostic output.
func keysOf(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
