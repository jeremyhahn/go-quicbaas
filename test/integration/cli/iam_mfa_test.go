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
// IAM MFA Status Tests
// =============================================================================

// TestIAMMFAStatus verifies that checking MFA status for a user within a tenant
// succeeds and reports MFA as not enabled for a fresh user.
func TestIAMMFAStatus(t *testing.T) {
	RunForTransports(t, "MFAStatus", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("mfa-status")
		userID := UniqueKey("mfa-user")

		createTenant := IAMTenantCreateCommand(tenantID, "MFA Status Test")
		runner.RunAndAssert(createTenant)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		statusCmd := IAMMFAStatusCommand(tenantID, userID)
		stdout, stderr, err := runner.Run(statusCmd)
		if runner.IsSkipped(statusCmd) {
			return
		}
		require.NoError(t, err, "iam mfa status should succeed: stdout=%s stderr=%s", stdout, stderr)

		combined := stdout + stderr
		assert.Contains(t, strings.ToLower(combined), "false",
			"MFA should not be enabled for a fresh user, got: %s", combined)
	})
}

// TestIAMMFAStatusJSON verifies that MFA status with JSON output returns valid
// JSON containing MFA enablement state.
func TestIAMMFAStatusJSON(t *testing.T) {
	RunForTransports(t, "MFAStatusJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("mfa-json")
		userID := UniqueKey("mfa-user")

		createTenant := IAMTenantCreateCommand(tenantID, "MFA Status JSON Test")
		runner.RunAndAssert(createTenant)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		statusCmd := CommandDef{
			Name:           "iam mfa status json",
			Args:           []string{"iam", "mfa", "status", "--tenant", tenantID, "--user", userID, "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(statusCmd)
		if runner.IsSkipped(statusCmd) {
			return
		}
		require.NoError(t, err, "iam mfa status --output json should succeed: stdout=%s stderr=%s", stdout, stderr)

		trimmed := strings.TrimSpace(stdout)
		if trimmed != "" {
			assert.True(t, json.Valid([]byte(trimmed)),
				"MFA status JSON output should be valid JSON, got: %s", trimmed)
		}
	})
}

// =============================================================================
// IAM MFA Enroll Tests
// =============================================================================

// TestIAMMFAEnroll verifies that enrolling a user in MFA succeeds and returns
// an enrollment identifier and a TOTP secret or provisioning URI.
func TestIAMMFAEnroll(t *testing.T) {
	RunForTransports(t, "MFAEnroll", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("mfa-enroll")
		userID := UniqueKey("mfa-user")

		createTenant := IAMTenantCreateCommand(tenantID, "MFA Enroll Test")
		runner.RunAndAssert(createTenant)
		defer func() {
			// Disable MFA before deleting tenant to clean up state
			runner.Run(IAMMFADisableCommand(tenantID, userID))
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		enrollCmd := IAMMFAEnrollCommand(tenantID, userID)
		stdout, stderr, err := runner.Run(enrollCmd)
		if runner.IsSkipped(enrollCmd) {
			return
		}
		require.NoError(t, err, "iam mfa enroll should succeed: stdout=%s stderr=%s", stdout, stderr)

		combined := stdout + stderr
		// Enrollment must return at least one of: enrollment ID, secret, or provisioning URI
		hasEnrollmentData := strings.Contains(combined, "enrollment") ||
			strings.Contains(combined, "secret") ||
			strings.Contains(combined, "otpauth://") ||
			strings.Contains(combined, userID)
		assert.True(t, hasEnrollmentData,
			"MFA enroll should return enrollment data (enrollment ID, secret, or URI), got: %s", combined)
	})
}

// TestIAMMFAEnrollWithMethod verifies that enrolling a user in MFA with an
// explicit --method totp flag succeeds and returns enrollment data.
func TestIAMMFAEnrollWithMethod(t *testing.T) {
	RunForTransports(t, "MFAEnrollMethod", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("mfa-method")
		userID := UniqueKey("mfa-user")

		createTenant := IAMTenantCreateCommand(tenantID, "MFA Enroll Method Test")
		runner.RunAndAssert(createTenant)
		defer func() {
			runner.Run(IAMMFADisableCommand(tenantID, userID))
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		enrollCmd := CommandDef{
			Name:           "iam mfa enroll with method",
			Args:           []string{"iam", "mfa", "enroll", "--tenant", tenantID, "--user", userID, "--method", "totp"},
			ExpectSuccess:  true,
			ExpectOutput:   []string{userID},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(enrollCmd)
		if runner.IsSkipped(enrollCmd) {
			return
		}
		require.NoError(t, err, "iam mfa enroll --method totp should succeed: stdout=%s stderr=%s", stdout, stderr)

		combined := stdout + stderr
		hasEnrollmentData := strings.Contains(combined, "enrollment") ||
			strings.Contains(combined, "secret") ||
			strings.Contains(combined, "otpauth://") ||
			strings.Contains(combined, userID)
		assert.True(t, hasEnrollmentData,
			"MFA enroll with --method totp should return enrollment data, got: %s", combined)
	})
}

// =============================================================================
// IAM MFA Verify Tests
// =============================================================================

// TestIAMMFAVerifyInvalidCode verifies that MFA verification fails when
// provided with an invalid TOTP code, ensuring proper error handling.
func TestIAMMFAVerifyInvalidCode(t *testing.T) {
	RunForTransports(t, "MFAVerifyInvalid", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("mfa-verify")
		userID := UniqueKey("mfa-user")

		createTenant := IAMTenantCreateCommand(tenantID, "MFA Verify Test")
		runner.RunAndAssert(createTenant)
		defer func() {
			runner.Run(IAMMFADisableCommand(tenantID, userID))
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Enroll first so we have a valid enrollment to verify against
		enrollCmd := IAMMFAEnrollCommand(tenantID, userID)
		stdout, _, err := runner.Run(enrollCmd)
		if runner.IsSkipped(enrollCmd) {
			return
		}
		require.NoError(t, err, "iam mfa enroll should succeed before verify test")

		// Extract enrollment ID from output; use a placeholder if extraction fails
		enrollmentID := extractEnrollmentID(stdout)
		if enrollmentID == "" {
			enrollmentID = "unknown-enrollment"
		}

		// Attempt verification with an invalid code
		verifyCmd := IAMMFAVerifyCommand(enrollmentID, "000000")
		verifyCmd.ExpectSuccess = false
		verifyStdout, verifyStderr, verifyErr := runner.Run(verifyCmd)
		if runner.IsSkipped(verifyCmd) {
			return
		}

		// Verification with an invalid code must fail
		assert.Error(t, verifyErr,
			"iam mfa verify with invalid code should fail: stdout=%s stderr=%s",
			verifyStdout, verifyStderr)

		combined := verifyStdout + verifyStderr
		hasErrorIndication := strings.Contains(strings.ToLower(combined), "invalid") ||
			strings.Contains(strings.ToLower(combined), "fail") ||
			strings.Contains(strings.ToLower(combined), "error") ||
			strings.Contains(strings.ToLower(combined), "incorrect") ||
			verifyErr != nil
		assert.True(t, hasErrorIndication,
			"MFA verify with invalid code should indicate failure, got: %s", combined)
	})
}

// =============================================================================
// IAM MFA Disable Tests
// =============================================================================

// TestIAMMFADisableNotEnrolled verifies that disabling MFA for a user who
// has not enrolled either fails gracefully with an appropriate error or
// succeeds as a no-op.
func TestIAMMFADisableNotEnrolled(t *testing.T) {
	RunForTransports(t, "MFADisableNotEnrolled", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("mfa-dis-nop")
		userID := UniqueKey("mfa-user")

		createTenant := IAMTenantCreateCommand(tenantID, "MFA Disable Not Enrolled Test")
		runner.RunAndAssert(createTenant)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		disableCmd := IAMMFADisableCommand(tenantID, userID)
		disableCmd.ExpectSuccess = false // May fail for unenrolled user
		stdout, stderr, err := runner.Run(disableCmd)
		if runner.IsSkipped(disableCmd) {
			return
		}

		combined := stdout + stderr
		if err != nil {
			// Failure is expected; verify the error is meaningful
			hasErrorIndication := strings.Contains(strings.ToLower(combined), "not enrolled") ||
				strings.Contains(strings.ToLower(combined), "not enabled") ||
				strings.Contains(strings.ToLower(combined), "not found") ||
				strings.Contains(strings.ToLower(combined), "error") ||
				strings.Contains(strings.ToLower(combined), "fail")
			assert.True(t, hasErrorIndication,
				"MFA disable for unenrolled user should return a meaningful error, got: %s", combined)
		} else {
			// If it succeeds, that is also acceptable (idempotent disable)
			t.Logf("MFA disable for unenrolled user succeeded as no-op: %s", combined)
		}
	})
}

// =============================================================================
// IAM MFA Backup Codes Status Tests
// =============================================================================

// TestIAMMFABackupCodesStatus verifies that checking backup codes status
// for a user within a tenant succeeds.
func TestIAMMFABackupCodesStatus(t *testing.T) {
	RunForTransports(t, "MFABackupCodesStatus", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("mfa-bcs")
		userID := UniqueKey("mfa-user")

		createTenant := IAMTenantCreateCommand(tenantID, "MFA Backup Codes Status Test")
		runner.RunAndAssert(createTenant)
		defer func() {
			runner.Run(IAMMFADisableCommand(tenantID, userID))
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Enroll user in MFA first (backup codes status requires MFA enrollment).
		// Note: BeginEnrollment starts MFA but does not complete it. The server
		// may require completed enrollment (verified TOTP code) before backup
		// codes are available.
		enrollCmd := IAMMFAEnrollCommand(tenantID, userID)
		_, _, enrollErr := runner.Run(enrollCmd)
		if runner.IsSkipped(enrollCmd) {
			return
		}
		require.NoError(t, enrollErr, "MFA enrollment should succeed before checking backup codes status")

		statusCmd := IAMMFABackupCodesStatusCommand(tenantID, userID)
		statusCmd.ExpectSuccess = false // May fail if enrollment is not completed
		stdout, stderr, err := runner.Run(statusCmd)
		if runner.IsSkipped(statusCmd) {
			return
		}

		combined := strings.ToLower(stdout + stderr)
		if err != nil {
			// Enrollment was begun but not completed; "not enrolled" is acceptable.
			assert.True(t,
				strings.Contains(combined, "not enrolled") ||
					strings.Contains(combined, "not found") ||
					strings.Contains(combined, "not enabled"),
				"backup codes status error should indicate MFA not enrolled, got: %s", stdout+stderr)
		} else {
			assert.NotEmpty(t, stdout, "backup codes status should produce output")
		}
	})
}

// TestIAMMFABackupCodesStatusJSON verifies that backup codes status with JSON
// output returns valid JSON.
func TestIAMMFABackupCodesStatusJSON(t *testing.T) {
	RunForTransports(t, "MFABackupCodesStatusJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("mfa-bcj")
		userID := UniqueKey("mfa-user")

		createTenant := IAMTenantCreateCommand(tenantID, "MFA Backup Codes JSON Test")
		runner.RunAndAssert(createTenant)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		statusCmd := CommandDef{
			Name:           "iam mfa backup-codes status json",
			Args:           []string{"iam", "mfa", "backup-codes", "status", "--tenant", tenantID, "--user", userID, "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(statusCmd)
		if runner.IsSkipped(statusCmd) {
			return
		}
		require.NoError(t, err, "iam mfa backup-codes status --output json should succeed: stdout=%s stderr=%s", stdout, stderr)

		trimmed := strings.TrimSpace(stdout)
		if trimmed != "" {
			assert.True(t, json.Valid([]byte(trimmed)),
				"backup codes status JSON output should be valid JSON, got: %s", trimmed)
		}
	})
}

// =============================================================================
// IAM MFA Backup Codes Generate Tests
// =============================================================================

// TestIAMMFABackupCodesGenerate verifies that generating backup codes for a
// user succeeds and returns code data in the output.
func TestIAMMFABackupCodesGenerate(t *testing.T) {
	RunForTransports(t, "MFABackupCodesGenerate", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("mfa-bcg")
		userID := UniqueKey("mfa-user")

		createTenant := IAMTenantCreateCommand(tenantID, "MFA Backup Codes Generate Test")
		runner.RunAndAssert(createTenant)
		defer func() {
			runner.Run(IAMMFADisableCommand(tenantID, userID))
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Enroll user in MFA first (backup codes typically require MFA enrollment).
		// Note: BeginEnrollment starts MFA but does not complete it. The server
		// may require completed enrollment (verified TOTP code) before backup
		// codes can be generated.
		enrollCmd := IAMMFAEnrollCommand(tenantID, userID)
		_, _, enrollErr := runner.Run(enrollCmd)
		if runner.IsSkipped(enrollCmd) {
			return
		}
		require.NoError(t, enrollErr, "MFA enrollment should succeed before generating backup codes")

		genCmd := IAMMFABackupCodesGenerateCommand(tenantID, userID)
		genCmd.ExpectSuccess = false // May fail if enrollment is not completed
		stdout, stderr, err := runner.Run(genCmd)
		if runner.IsSkipped(genCmd) {
			return
		}

		combined := stdout + stderr
		if err != nil {
			// Enrollment was begun but not completed; "not enrolled" is acceptable.
			combinedLower := strings.ToLower(combined)
			assert.True(t,
				strings.Contains(combinedLower, "not enrolled") ||
					strings.Contains(combinedLower, "not found") ||
					strings.Contains(combinedLower, "not enabled"),
				"backup codes generate error should indicate MFA not enrolled, got: %s", combined)
		} else {
			// Backup codes generation should return codes or a confirmation
			hasCodeData := strings.Contains(combined, "code") ||
				strings.Contains(combined, "Code") ||
				strings.Contains(combined, "generated") ||
				strings.Contains(combined, "Generated") ||
				strings.Contains(combined, userID)
			assert.True(t, hasCodeData,
				"backup codes generate should return code data or confirmation, got: %s", combined)
		}
	})
}

// =============================================================================
// IAM MFA Lifecycle Tests
// =============================================================================

// TestIAMMFALifecycle performs an end-to-end MFA lifecycle test: checks initial
// status (not enrolled), enrolls the user, verifies status changes to enrolled,
// and finally disables MFA.
func TestIAMMFALifecycle(t *testing.T) {
	RunForTransports(t, "MFALifecycle", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("mfa-life")
		userID := UniqueKey("mfa-user")

		createTenant := IAMTenantCreateCommand(tenantID, "MFA Lifecycle Test")
		runner.RunAndAssert(createTenant)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Step 1: Check initial MFA status (should not be enabled)
		statusCmd := IAMMFAStatusCommand(tenantID, userID)
		statusStdout, statusStderr, statusErr := runner.Run(statusCmd)
		if runner.IsSkipped(statusCmd) {
			return
		}
		require.NoError(t, statusErr,
			"initial MFA status check should succeed: stdout=%s stderr=%s", statusStdout, statusStderr)

		initialCombined := strings.ToLower(statusStdout + statusStderr)
		assert.Contains(t, initialCombined, "false",
			"MFA should initially report as not enabled, got: %s", statusStdout+statusStderr)

		// Step 2: Enroll user in MFA
		enrollCmd := IAMMFAEnrollCommand(tenantID, userID)
		enrollStdout, enrollStderr, enrollErr := runner.Run(enrollCmd)
		require.NoError(t, enrollErr,
			"MFA enrollment should succeed: stdout=%s stderr=%s", enrollStdout, enrollStderr)

		enrollCombined := enrollStdout + enrollStderr
		hasEnrollmentData := strings.Contains(enrollCombined, "enrollment") ||
			strings.Contains(enrollCombined, "secret") ||
			strings.Contains(enrollCombined, "otpauth://") ||
			strings.Contains(enrollCombined, userID)
		assert.True(t, hasEnrollmentData,
			"MFA enrollment should return enrollment data, got: %s", enrollCombined)

		// Step 3: Check MFA status after enrollment (should reflect pending or enabled)
		postEnrollStatus := IAMMFAStatusCommand(tenantID, userID)
		postStdout, postStderr, postErr := runner.Run(postEnrollStatus)
		require.NoError(t, postErr,
			"post-enrollment MFA status check should succeed: stdout=%s stderr=%s", postStdout, postStderr)

		postCombined := postStdout + postStderr
		// After enrollment, status should indicate MFA is enrolled or pending verification
		hasStatusChange := strings.Contains(strings.ToLower(postCombined), "true") ||
			strings.Contains(strings.ToLower(postCombined), "pending") ||
			strings.Contains(strings.ToLower(postCombined), "enrolled")
		assert.True(t, hasStatusChange,
			"MFA status after enrollment should indicate enabled or pending, got: %s", postCombined)

		// Step 4: Disable MFA
		disableCmd := IAMMFADisableCommand(tenantID, userID)
		disableStdout, disableStderr, disableErr := runner.Run(disableCmd)
		require.NoError(t, disableErr,
			"MFA disable should succeed: stdout=%s stderr=%s", disableStdout, disableStderr)

		// Step 5: Verify MFA is disabled
		finalStatus := IAMMFAStatusCommand(tenantID, userID)
		finalStdout, finalStderr, finalErr := runner.Run(finalStatus)
		require.NoError(t, finalErr,
			"final MFA status check should succeed: stdout=%s stderr=%s", finalStdout, finalStderr)

		finalCombined := strings.ToLower(finalStdout + finalStderr)
		assert.Contains(t, finalCombined, "false",
			"MFA should report as not enabled after disable, got: %s", finalStdout+finalStderr)
	})
}

// =============================================================================
// Helpers
// =============================================================================

// extractEnrollmentID attempts to extract an enrollment ID from MFA enroll
// command output. It looks for common patterns in both table and JSON output
// formats. Returns an empty string if no enrollment ID can be extracted.
func extractEnrollmentID(stdout string) string {
	// Try JSON format: "enrollment_id": "some-id"
	if strings.Contains(stdout, "enrollment_id") {
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &result); err == nil {
			if id, ok := result["enrollment_id"].(string); ok && id != "" {
				return id
			}
		}
	}

	// Try table format: look for lines containing "Enrollment ID:" followed by a value
	for _, line := range strings.Split(stdout, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Enrollment ID:") || strings.HasPrefix(trimmed, "enrollment_id:") {
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) == 2 {
				value := strings.TrimSpace(parts[1])
				if value != "" {
					return value
				}
			}
		}
	}

	return ""
}
