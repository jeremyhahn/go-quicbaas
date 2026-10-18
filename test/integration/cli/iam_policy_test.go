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
// Policy YAML Helpers
// =============================================================================

// writeTempPolicyFile creates a temporary YAML policy file and registers cleanup.
func writeTempPolicyFile(t *testing.T, tenantID, policyName string) string {
	t.Helper()
	content := fmt.Sprintf(`apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: %s
  tenant: %s
spec:
  rules:
    - name: allow-read
      effect: allow
      actions: [read]
      resources:
        - keys/**
      subjects:
        authenticated: true
`, policyName, tenantID)

	tmpFile, err := os.CreateTemp("", "policy-*.yaml")
	require.NoError(t, err)
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })
	return tmpFile.Name()
}

// writeTempPolicyFileWithRules creates a temporary YAML policy file with custom rules.
func writeTempPolicyFileWithRules(t *testing.T, tenantID, policyName, rulesYAML string) string {
	t.Helper()
	content := fmt.Sprintf(`apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: %s
  tenant: %s
spec:
  rules:
%s
`, policyName, tenantID, rulesYAML)

	tmpFile, err := os.CreateTemp("", "policy-*.yaml")
	require.NoError(t, err)
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })
	return tmpFile.Name()
}

// writeTempInvalidPolicyFile creates a temporary YAML policy file missing required fields.
func writeTempInvalidPolicyFile(t *testing.T) string {
	t.Helper()
	content := `apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  tenant: some-tenant
spec:
  rules:
    - name: allow-read
      effect: allow
      actions: [read]
      resources:
        - keys/**
`

	tmpFile, err := os.CreateTemp("", "policy-invalid-*.yaml")
	require.NoError(t, err)
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })
	return tmpFile.Name()
}

// createPolicyTestTenant provisions a tenant for policy tests, makes the runner
// act as its admin, and registers cleanup.
// Returns the tenant ID.
func createPolicyTestTenant(t *testing.T, runner *TestRunner) string {
	t.Helper()
	tenantID := UniqueKey("pol-tenant")
	cmd := IAMTenantCreateCommand(tenantID, tenantID+"-name")
	if runner.IsSkipped(cmd) {
		return ""
	}
	runner.ActAsTenantAdmin(tenantID)
	t.Cleanup(func() {
		delCmd := IAMTenantDeleteCommand(tenantID)
		requireCleanupRun(t, runner, delCmd)
	})
	return tenantID
}

// requireCleanupRun runs a cleanup command and fails the test when it does not
// succeed, so a resource a test created is never silently left behind.
func requireCleanupRun(t *testing.T, runner *TestRunner, cmd CommandDef) {
	t.Helper()
	stdout, stderr, err := runner.Run(cmd)
	require.NoError(t, err, "%s should succeed: stdout=%s stderr=%s", cmd.Name, stdout, stderr)
}

// =============================================================================
// Policy Lifecycle Tests
// =============================================================================

// TestIAMPolicyLifecycle tests the full create-get-list-delete lifecycle for a policy.
func TestIAMPolicyLifecycle(t *testing.T) {
	RunForTransports(t, "IAMPolicyLifecycle", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createPolicyTestTenant(t, runner)
		if tenantID == "" {
			return
		}
		policyName := UniqueKey("lifecycle-policy")
		policyFile := writeTempPolicyFile(t, tenantID, policyName)

		// Apply
		applyCmd := IAMPolicyApplyCommand(policyFile)
		stdout, stderr, err := runner.Run(applyCmd)
		if runner.IsSkipped(applyCmd) {
			return
		}
		require.NoError(t, err, "policy apply should succeed: stdout=%s stderr=%s", stdout, stderr)

		// Get
		getCmd := IAMPolicyGetCommand(tenantID, policyName)
		stdout, stderr, err = runner.Run(getCmd)
		require.NoError(t, err, "policy get should succeed: stdout=%s stderr=%s", stdout, stderr)
		assert.Contains(t, stdout, policyName, "get output should contain policy name")

		// List
		listCmd := IAMPolicyListCommand(tenantID)
		stdout, stderr, err = runner.Run(listCmd)
		require.NoError(t, err, "policy list should succeed: stdout=%s stderr=%s", stdout, stderr)
		assert.Contains(t, stdout, policyName, "list output should contain applied policy")

		// Delete
		deleteCmd := IAMPolicyDeleteCommand(tenantID, policyName)
		stdout, stderr, err = runner.Run(deleteCmd)
		require.NoError(t, err, "policy delete should succeed: stdout=%s stderr=%s", stdout, stderr)
		assert.Contains(t, stdout, policyName, "delete output should confirm policy name")

		// Verify deletion: get should fail
		getCmd.ExpectSuccess = false
		_, _, err = runner.Run(getCmd)
		assert.Error(t, err, "get after delete should fail")
	})
}

// =============================================================================
// Policy Apply Tests
// =============================================================================

// TestIAMPolicyApply tests applying a valid policy and verifying success output.
func TestIAMPolicyApply(t *testing.T) {
	RunForTransports(t, "IAMPolicyApply", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createPolicyTestTenant(t, runner)
		if tenantID == "" {
			return
		}
		policyName := UniqueKey("apply-policy")
		policyFile := writeTempPolicyFile(t, tenantID, policyName)

		applyCmd := IAMPolicyApplyCommand(policyFile)
		stdout, stderr, err := runner.Run(applyCmd)
		if runner.IsSkipped(applyCmd) {
			return
		}
		require.NoError(t, err, "policy apply should succeed: stdout=%s stderr=%s", stdout, stderr)
		assert.NotEmpty(t, stdout, "apply output should not be empty")

		// Cleanup
		delCmd := IAMPolicyDeleteCommand(tenantID, policyName)
		requireCleanupRun(t, runner, delCmd)
	})
}

// TestIAMPolicyApplyJSON tests applying a policy with --output json and verifying valid JSON.
func TestIAMPolicyApplyJSON(t *testing.T) {
	RunForTransports(t, "IAMPolicyApplyJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createPolicyTestTenant(t, runner)
		if tenantID == "" {
			return
		}
		policyName := UniqueKey("apply-json-policy")
		policyFile := writeTempPolicyFile(t, tenantID, policyName)

		applyJSONCmd := CommandDef{
			Name:           fmt.Sprintf("iam policy apply %s json", policyFile),
			Args:           []string{"iam", "policy", "apply", "-f", policyFile, "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(applyJSONCmd)
		if runner.IsSkipped(applyJSONCmd) {
			return
		}
		require.NoError(t, err, "policy apply --output json should succeed: stdout=%s stderr=%s", stdout, stderr)

		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "apply output should be valid JSON: %s", stdout)

		// Cleanup
		delCmd := IAMPolicyDeleteCommand(tenantID, policyName)
		requireCleanupRun(t, runner, delCmd)
	})
}

// TestIAMPolicyApplyInvalidFile tests applying a nonexistent file, expecting failure.
func TestIAMPolicyApplyInvalidFile(t *testing.T) {
	RunForTransports(t, "IAMPolicyApplyInvalidFile", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		applyCmd := CommandDef{
			Name:           "iam policy apply nonexistent",
			Args:           []string{"iam", "policy", "apply", "-f", "/tmp/nonexistent-policy-file-12345.yaml"},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		_, stderr, err := runner.Run(applyCmd)
		if runner.IsSkipped(applyCmd) {
			return
		}
		assert.Error(t, err, "apply with nonexistent file should fail")
		combined := stderr
		assert.True(t, len(combined) > 0, "error output should not be empty for nonexistent file")
	})
}

// =============================================================================
// Policy Get Tests
// =============================================================================

// TestIAMPolicyGet tests getting an applied policy and verifying its name and rules.
func TestIAMPolicyGet(t *testing.T) {
	RunForTransports(t, "IAMPolicyGet", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createPolicyTestTenant(t, runner)
		if tenantID == "" {
			return
		}
		policyName := UniqueKey("get-policy")
		policyFile := writeTempPolicyFile(t, tenantID, policyName)

		// Apply first
		applyCmd := IAMPolicyApplyCommand(policyFile)
		stdout, stderr, err := runner.Run(applyCmd)
		if runner.IsSkipped(applyCmd) {
			return
		}
		require.NoError(t, err, "policy apply should succeed: stdout=%s stderr=%s", stdout, stderr)

		// Get
		getCmd := IAMPolicyGetCommand(tenantID, policyName)
		stdout, stderr, err = runner.Run(getCmd)
		require.NoError(t, err, "policy get should succeed: stdout=%s stderr=%s", stdout, stderr)
		assert.Contains(t, stdout, policyName, "get output should contain policy name")
		assert.Contains(t, stdout, "allow-read", "get output should contain rule name")

		// Cleanup
		delCmd := IAMPolicyDeleteCommand(tenantID, policyName)
		requireCleanupRun(t, runner, delCmd)
	})
}

// TestIAMPolicyGetJSON tests getting a policy with --output json.
func TestIAMPolicyGetJSON(t *testing.T) {
	RunForTransports(t, "IAMPolicyGetJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createPolicyTestTenant(t, runner)
		if tenantID == "" {
			return
		}
		policyName := UniqueKey("get-json-policy")
		policyFile := writeTempPolicyFile(t, tenantID, policyName)

		// Apply first
		applyCmd := IAMPolicyApplyCommand(policyFile)
		stdout, stderr, err := runner.Run(applyCmd)
		if runner.IsSkipped(applyCmd) {
			return
		}
		require.NoError(t, err, "policy apply should succeed: stdout=%s stderr=%s", stdout, stderr)

		// Get with JSON output
		getJSONCmd := CommandDef{
			Name:           fmt.Sprintf("iam policy get %s json", policyName),
			Args:           []string{"iam", "policy", "get", policyName, "--tenant", tenantID, "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err = runner.Run(getJSONCmd)
		require.NoError(t, err, "policy get --output json should succeed: stdout=%s stderr=%s", stdout, stderr)

		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "get output should be valid JSON: %s", stdout)
		assert.Contains(t, stdout, policyName, "JSON output should contain policy name")

		// Cleanup
		delCmd := IAMPolicyDeleteCommand(tenantID, policyName)
		requireCleanupRun(t, runner, delCmd)
	})
}

// TestIAMPolicyGetNonExistent tests getting a nonexistent policy, expecting failure.
func TestIAMPolicyGetNonExistent(t *testing.T) {
	RunForTransports(t, "IAMPolicyGetNonExistent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createPolicyTestTenant(t, runner)
		if tenantID == "" {
			return
		}

		getCmd := CommandDef{
			Name:           "iam policy get nonexistent",
			Args:           []string{"iam", "policy", "get", "nonexistent-policy-xyz", "--tenant", tenantID},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		_, stderr, err := runner.Run(getCmd)
		if runner.IsSkipped(getCmd) {
			return
		}
		assert.Error(t, err, "get nonexistent policy should fail")
		combined := stderr
		assert.True(t, len(combined) > 0, "error output should not be empty for nonexistent policy")
	})
}

// =============================================================================
// Policy List Tests
// =============================================================================

// TestIAMPolicyList tests listing policies after applying two policies to a tenant.
func TestIAMPolicyList(t *testing.T) {
	RunForTransports(t, "IAMPolicyList", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createPolicyTestTenant(t, runner)
		if tenantID == "" {
			return
		}

		policyName1 := UniqueKey("list-policy-a")
		policyName2 := UniqueKey("list-policy-b")
		policyFile1 := writeTempPolicyFile(t, tenantID, policyName1)
		policyFile2 := writeTempPolicyFile(t, tenantID, policyName2)

		// Apply both
		applyCmd1 := IAMPolicyApplyCommand(policyFile1)
		stdout, stderr, err := runner.Run(applyCmd1)
		if runner.IsSkipped(applyCmd1) {
			return
		}
		require.NoError(t, err, "apply policy 1 should succeed: stdout=%s stderr=%s", stdout, stderr)

		applyCmd2 := IAMPolicyApplyCommand(policyFile2)
		stdout, stderr, err = runner.Run(applyCmd2)
		require.NoError(t, err, "apply policy 2 should succeed: stdout=%s stderr=%s", stdout, stderr)

		// List
		listCmd := IAMPolicyListCommand(tenantID)
		stdout, stderr, err = runner.Run(listCmd)
		require.NoError(t, err, "policy list should succeed: stdout=%s stderr=%s", stdout, stderr)
		assert.Contains(t, stdout, policyName1, "list output should contain first policy")
		assert.Contains(t, stdout, policyName2, "list output should contain second policy")

		// Cleanup
		delCmd1 := IAMPolicyDeleteCommand(tenantID, policyName1)
		requireCleanupRun(t, runner, delCmd1)
		delCmd2 := IAMPolicyDeleteCommand(tenantID, policyName2)
		requireCleanupRun(t, runner, delCmd2)
	})
}

// TestIAMPolicyListJSON tests listing policies with --output json.
func TestIAMPolicyListJSON(t *testing.T) {
	RunForTransports(t, "IAMPolicyListJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createPolicyTestTenant(t, runner)
		if tenantID == "" {
			return
		}

		policyName1 := UniqueKey("listjson-policy-a")
		policyName2 := UniqueKey("listjson-policy-b")
		policyFile1 := writeTempPolicyFile(t, tenantID, policyName1)
		policyFile2 := writeTempPolicyFile(t, tenantID, policyName2)

		// Apply both
		applyCmd1 := IAMPolicyApplyCommand(policyFile1)
		stdout, stderr, err := runner.Run(applyCmd1)
		if runner.IsSkipped(applyCmd1) {
			return
		}
		require.NoError(t, err, "apply policy 1 should succeed: stdout=%s stderr=%s", stdout, stderr)

		applyCmd2 := IAMPolicyApplyCommand(policyFile2)
		stdout, stderr, err = runner.Run(applyCmd2)
		require.NoError(t, err, "apply policy 2 should succeed: stdout=%s stderr=%s", stdout, stderr)

		// List with JSON output
		listJSONCmd := CommandDef{
			Name:           fmt.Sprintf("iam policy list tenant=%s json", tenantID),
			Args:           []string{"iam", "policy", "list", "--tenant", tenantID, "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        15 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err = runner.Run(listJSONCmd)
		require.NoError(t, err, "policy list --output json should succeed: stdout=%s stderr=%s", stdout, stderr)

		var result interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "list output should be valid JSON: %s", stdout)
		assert.Contains(t, stdout, policyName1, "JSON list should contain first policy")
		assert.Contains(t, stdout, policyName2, "JSON list should contain second policy")

		// Cleanup
		delCmd1 := IAMPolicyDeleteCommand(tenantID, policyName1)
		requireCleanupRun(t, runner, delCmd1)
		delCmd2 := IAMPolicyDeleteCommand(tenantID, policyName2)
		requireCleanupRun(t, runner, delCmd2)
	})
}

// =============================================================================
// Policy Delete Tests
// =============================================================================

// TestIAMPolicyDelete tests applying a policy, deleting it, and verifying get fails.
func TestIAMPolicyDelete(t *testing.T) {
	RunForTransports(t, "IAMPolicyDelete", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createPolicyTestTenant(t, runner)
		if tenantID == "" {
			return
		}
		policyName := UniqueKey("delete-policy")
		policyFile := writeTempPolicyFile(t, tenantID, policyName)

		// Apply
		applyCmd := IAMPolicyApplyCommand(policyFile)
		stdout, stderr, err := runner.Run(applyCmd)
		if runner.IsSkipped(applyCmd) {
			return
		}
		require.NoError(t, err, "policy apply should succeed: stdout=%s stderr=%s", stdout, stderr)

		// Delete
		deleteCmd := IAMPolicyDeleteCommand(tenantID, policyName)
		stdout, stderr, err = runner.Run(deleteCmd)
		require.NoError(t, err, "policy delete should succeed: stdout=%s stderr=%s", stdout, stderr)
		assert.Contains(t, stdout, policyName, "delete output should confirm policy name")

		// Verify get fails after deletion
		getAfterDelete := CommandDef{
			Name:           fmt.Sprintf("iam policy get %s after delete", policyName),
			Args:           []string{"iam", "policy", "get", policyName, "--tenant", tenantID},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		_, _, err = runner.Run(getAfterDelete)
		assert.Error(t, err, "get after delete should fail")
	})
}

// TestIAMPolicyDeleteNonExistent tests deleting a nonexistent policy, expecting failure.
func TestIAMPolicyDeleteNonExistent(t *testing.T) {
	RunForTransports(t, "IAMPolicyDeleteNonExistent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createPolicyTestTenant(t, runner)
		if tenantID == "" {
			return
		}

		deleteCmd := CommandDef{
			Name:           "iam policy delete nonexistent",
			Args:           []string{"iam", "policy", "delete", "nonexistent-policy-xyz", "--tenant", tenantID, "--yes"},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		_, stderr, err := runner.Run(deleteCmd)
		if runner.IsSkipped(deleteCmd) {
			return
		}
		assert.Error(t, err, "delete nonexistent policy should fail")
		assert.True(t, len(stderr) > 0, "error output should not be empty for nonexistent policy delete")
	})
}

// =============================================================================
// Policy Validate Tests
// =============================================================================

// TestIAMPolicyValidateValid tests validating a well-formed policy YAML file.
func TestIAMPolicyValidateValid(t *testing.T) {
	RunForTransports(t, "IAMPolicyValidateValid", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("val-tenant")
		policyName := UniqueKey("valid-policy")
		policyFile := writeTempPolicyFile(t, tenantID, policyName)

		validateCmd := IAMPolicyValidateCommand(policyFile)
		stdout, stderr, err := runner.Run(validateCmd)
		if runner.IsSkipped(validateCmd) {
			return
		}
		require.NoError(t, err, "validate valid policy should succeed: stdout=%s stderr=%s", stdout, stderr)
		// The CLI should indicate the policy is valid
		combined := strings.ToLower(stdout + stderr)
		assert.True(t,
			strings.Contains(combined, "valid") || strings.Contains(combined, "ok") || strings.Contains(combined, "success"),
			"validate output should indicate validity: %s", stdout)
	})
}

// TestIAMPolicyValidateInvalid tests validating an invalid policy YAML file
// (missing metadata.name), expecting a validation failure.
func TestIAMPolicyValidateInvalid(t *testing.T) {
	RunForTransports(t, "IAMPolicyValidateInvalid", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		invalidFile := writeTempInvalidPolicyFile(t)

		validateCmd := CommandDef{
			Name:           "iam policy validate invalid",
			Args:           []string{"iam", "policy", "validate", "-f", invalidFile},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		_, stderr, err := runner.Run(validateCmd)
		if runner.IsSkipped(validateCmd) {
			return
		}
		assert.Error(t, err, "validate invalid policy should fail")
		combined := strings.ToLower(stderr)
		assert.True(t,
			strings.Contains(combined, "name") || strings.Contains(combined, "required") ||
				strings.Contains(combined, "invalid") || strings.Contains(combined, "validation"),
			"validate error should indicate what is wrong: %s", stderr)
	})
}

// =============================================================================
// Policy Test (Evaluation) Tests
// =============================================================================

// TestIAMPolicyTest tests applying a policy and then evaluating a request that
// should be allowed by the policy rules.
func TestIAMPolicyTest(t *testing.T) {
	RunForTransports(t, "IAMPolicyTest", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createPolicyTestTenant(t, runner)
		if tenantID == "" {
			return
		}
		policyName := UniqueKey("test-allow-policy")
		policyFile := writeTempPolicyFile(t, tenantID, policyName)

		// Apply the policy that allows read on keys/**
		applyCmd := IAMPolicyApplyCommand(policyFile)
		stdout, stderr, err := runner.Run(applyCmd)
		if runner.IsSkipped(applyCmd) {
			return
		}
		require.NoError(t, err, "policy apply should succeed: stdout=%s stderr=%s", stdout, stderr)

		// Test a read on keys/foo with an authenticated subject - should be ALLOW
		subject := `{"id":"user1","authenticated":true}`
		testCmd := IAMPolicyTestCommand(tenantID, subject, "read", "keys/foo")
		stdout, stderr, err = runner.Run(testCmd)
		require.NoError(t, err, "policy test should succeed: stdout=%s stderr=%s", stdout, stderr)
		combined := strings.ToUpper(stdout + stderr)
		assert.True(t,
			strings.Contains(combined, "ALLOW") || strings.Contains(combined, "PERMIT") || strings.Contains(combined, "GRANTED"),
			"policy test for allowed action should indicate ALLOW: %s", stdout)

		// Cleanup
		delCmd := IAMPolicyDeleteCommand(tenantID, policyName)
		requireCleanupRun(t, runner, delCmd)
	})
}

// TestIAMPolicyTestDeny tests evaluating a request for an action not covered by the
// policy, expecting a DENY result.
func TestIAMPolicyTestDeny(t *testing.T) {
	RunForTransports(t, "IAMPolicyTestDeny", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createPolicyTestTenant(t, runner)
		if tenantID == "" {
			return
		}
		policyName := UniqueKey("test-deny-policy")
		// Policy only allows "read" on keys/**
		policyFile := writeTempPolicyFile(t, tenantID, policyName)

		// Apply the policy
		applyCmd := IAMPolicyApplyCommand(policyFile)
		stdout, stderr, err := runner.Run(applyCmd)
		if runner.IsSkipped(applyCmd) {
			return
		}
		require.NoError(t, err, "policy apply should succeed: stdout=%s stderr=%s", stdout, stderr)

		// Test a "write" action which is NOT covered by the read-only policy
		subject := `{"id":"user1","authenticated":true}`
		testCmd := IAMPolicyTestCommand(tenantID, subject, "write", "keys/foo")
		stdout, stderr, err = runner.Run(testCmd)
		// The command may succeed (exit 0) but output DENY, or it may fail
		combined := strings.ToUpper(stdout + stderr)
		if err == nil {
			// Command succeeded; output should indicate denial
			assert.True(t,
				strings.Contains(combined, "DENY") || strings.Contains(combined, "DENIED") || strings.Contains(combined, "REJECT"),
				"policy test for uncovered action should indicate DENY: %s", stdout)
		} else {
			// Command failed; the error or stderr should indicate denial
			assert.True(t,
				strings.Contains(combined, "DENY") || strings.Contains(combined, "DENIED") ||
					strings.Contains(combined, "REJECT") || strings.Contains(combined, "NOT"),
				"policy test failure for uncovered action should indicate denial: stdout=%s stderr=%s", stdout, stderr)
		}

		// Cleanup
		delCmd := IAMPolicyDeleteCommand(tenantID, policyName)
		requireCleanupRun(t, runner, delCmd)
	})
}

// TestIAMPolicyTestJSON tests policy evaluation with --output json.
func TestIAMPolicyTestJSON(t *testing.T) {
	RunForTransports(t, "IAMPolicyTestJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createPolicyTestTenant(t, runner)
		if tenantID == "" {
			return
		}
		policyName := UniqueKey("test-json-policy")
		policyFile := writeTempPolicyFile(t, tenantID, policyName)

		// Apply
		applyCmd := IAMPolicyApplyCommand(policyFile)
		stdout, stderr, err := runner.Run(applyCmd)
		if runner.IsSkipped(applyCmd) {
			return
		}
		require.NoError(t, err, "policy apply should succeed: stdout=%s stderr=%s", stdout, stderr)

		// Test with JSON output
		subject := `{"id":"user1","authenticated":true}`
		testJSONCmd := CommandDef{
			Name: "iam policy test json",
			Args: []string{"iam", "policy", "test", "--tenant", tenantID,
				"--subject", subject,
				"--action", "read",
				"--resource", "keys/foo",
				"--output", "json"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err = runner.Run(testJSONCmd)
		require.NoError(t, err, "policy test --output json should succeed: stdout=%s stderr=%s", stdout, stderr)

		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "policy test output should be valid JSON: %s", stdout)

		// Cleanup
		delCmd := IAMPolicyDeleteCommand(tenantID, policyName)
		requireCleanupRun(t, runner, delCmd)
	})
}
