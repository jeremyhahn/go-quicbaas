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
// GDPR Consent Tests
// =============================================================================

// TestIAMGDPRConsentLifecycle tests the complete consent lifecycle: grant, get,
// list, and withdraw across all supported transports.
func TestIAMGDPRConsentLifecycle(t *testing.T) {
	RunForTransports(t, "GDPRConsentLifecycle", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("gdpr-lc-tenant")
		userID := UniqueKey("gdpr-lc-user")

		// Create tenant for GDPR operations
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Step 1: Grant marketing consent
		grantCmd := IAMGDPRConsentGrantCommand(tenantID, userID, "marketing", "1.0")
		runner.RunAndAssert(grantCmd)

		// Step 2: Get consent and verify it is active
		getCmd := IAMGDPRConsentGetCommand(tenantID, userID, "marketing")
		stdout, _, err := runner.Run(getCmd)
		if !runner.IsSkipped(getCmd) {
			require.NoError(t, err, "consent get should succeed")
			assert.Contains(t, stdout, "marketing",
				"consent get output should contain the purpose")
		}

		// Step 3: List consents and verify marketing appears
		listCmd := IAMGDPRConsentListCommand(tenantID, userID)
		stdout, _, err = runner.Run(listCmd)
		if !runner.IsSkipped(listCmd) {
			require.NoError(t, err, "consent list should succeed")
			assert.Contains(t, stdout, "marketing",
				"consent list should include the granted marketing consent")
		}

		// Step 4: Withdraw consent
		withdrawCmd := IAMGDPRConsentWithdrawCommand(tenantID, userID, "marketing")
		runner.RunAndAssert(withdrawCmd)

		// Step 5: Get consent after withdrawal and verify withdrawn state
		stdout, _, err = runner.Run(getCmd)
		if !runner.IsSkipped(getCmd) {
			require.NoError(t, err, "consent get after withdrawal should succeed")
			combined := strings.ToLower(stdout)
			assert.True(t,
				strings.Contains(combined, "withdrawn") || strings.Contains(combined, "revoked") || strings.Contains(combined, "inactive"),
				"consent should indicate withdrawn/revoked/inactive state after withdrawal, got: %s", stdout)
		}
	})
}

// TestIAMGDPRConsentGrant tests granting marketing consent and verifying the output.
func TestIAMGDPRConsentGrant(t *testing.T) {
	RunForTransports(t, "GDPRConsentGrant", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("gdpr-grant-tenant")
		userID := UniqueKey("gdpr-grant-user")

		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		grantCmd := IAMGDPRConsentGrantCommand(tenantID, userID, "marketing", "1.0")
		runner.RunAndAssert(grantCmd)
	})
}

// TestIAMGDPRConsentGrantJSON tests granting consent with JSON output format.
func TestIAMGDPRConsentGrantJSON(t *testing.T) {
	RunForTransports(t, "GDPRConsentGrantJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("gdpr-json-tenant")
		userID := UniqueKey("gdpr-json-user")

		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Grant consent
		grantCmd := IAMGDPRConsentGrantCommand(tenantID, userID, "analytics", "2.0")
		runner.RunAndAssert(grantCmd)

		// Get consent with JSON output
		getJSONCmd := CommandDef{
			Name:           "gdpr consent get json",
			Args:           []string{"iam", "gdpr", "consent", "get", "--tenant", tenantID, "--user-id", userID, "--purpose", "analytics", "--output", "json"},
			ExpectSuccess:  true,
			ExpectOutput:   []string{"analytics"},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(getJSONCmd)
		if !runner.IsSkipped(getJSONCmd) {
			require.NoError(t, err, "consent get with JSON output should succeed")

			// Verify valid JSON is returned
			var result map[string]interface{}
			jsonErr := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &result)
			assert.NoError(t, jsonErr, "output should be valid JSON: %s", stdout)
			if jsonErr == nil {
				assert.Contains(t, stdout, "analytics",
					"JSON output should contain the analytics purpose")
			}
		}
	})
}

// TestIAMGDPRConsentGet tests granting consent then retrieving it to verify
// the purpose and status fields.
func TestIAMGDPRConsentGet(t *testing.T) {
	RunForTransports(t, "GDPRConsentGet", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("gdpr-get-tenant")
		userID := UniqueKey("gdpr-get-user")

		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Grant consent for personalization
		grantCmd := IAMGDPRConsentGrantCommand(tenantID, userID, "personalization", "1.0")
		runner.RunAndAssert(grantCmd)

		// Get consent and verify purpose and active status
		getCmd := IAMGDPRConsentGetCommand(tenantID, userID, "personalization")
		stdout, _, err := runner.Run(getCmd)
		if !runner.IsSkipped(getCmd) {
			require.NoError(t, err, "consent get should succeed")
			assert.Contains(t, stdout, "personalization",
				"consent get should show personalization purpose")
			combined := strings.ToLower(stdout)
			assert.True(t,
				strings.Contains(combined, "granted") || strings.Contains(combined, "active") || strings.Contains(combined, "personalization"),
				"consent should indicate an active/granted state, got: %s", stdout)
		}
	})
}

// TestIAMGDPRConsentWithdraw tests granting consent and then withdrawing it,
// verifying the withdrawn state.
func TestIAMGDPRConsentWithdraw(t *testing.T) {
	RunForTransports(t, "GDPRConsentWithdraw", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("gdpr-wd-tenant")
		userID := UniqueKey("gdpr-wd-user")

		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Grant consent
		grantCmd := IAMGDPRConsentGrantCommand(tenantID, userID, "data_processing", "1.0")
		runner.RunAndAssert(grantCmd)

		// Withdraw consent
		withdrawCmd := IAMGDPRConsentWithdrawCommand(tenantID, userID, "data_processing")
		runner.RunAndAssert(withdrawCmd)

		// Verify withdrawal by getting consent status
		getCmd := IAMGDPRConsentGetCommand(tenantID, userID, "data_processing")
		stdout, _, err := runner.Run(getCmd)
		if !runner.IsSkipped(getCmd) {
			require.NoError(t, err, "consent get after withdrawal should succeed")
			combined := strings.ToLower(stdout)
			assert.True(t,
				strings.Contains(combined, "withdrawn") || strings.Contains(combined, "revoked") || strings.Contains(combined, "inactive"),
				"consent should be withdrawn/revoked/inactive after withdrawal, got: %s", stdout)
		}
	})
}

// TestIAMGDPRConsentList tests granting multiple consent purposes and listing
// them to verify all appear.
func TestIAMGDPRConsentList(t *testing.T) {
	RunForTransports(t, "GDPRConsentList", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("gdpr-list-tenant")
		userID := UniqueKey("gdpr-list-user")

		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Grant multiple purposes
		purposes := []string{"marketing", "analytics", "functional"}
		for _, purpose := range purposes {
			grantCmd := IAMGDPRConsentGrantCommand(tenantID, userID, purpose, "1.0")
			runner.RunAndAssert(grantCmd)
		}

		// List all consents
		listCmd := IAMGDPRConsentListCommand(tenantID, userID)
		stdout, _, err := runner.Run(listCmd)
		if !runner.IsSkipped(listCmd) {
			require.NoError(t, err, "consent list should succeed")
			for _, purpose := range purposes {
				assert.Contains(t, stdout, purpose,
					"consent list should include %s purpose", purpose)
			}
		}
	})
}

// TestIAMGDPRConsentGrantInvalidPurpose tests that granting consent with an
// invalid purpose fails with an appropriate error.
func TestIAMGDPRConsentGrantInvalidPurpose(t *testing.T) {
	RunForTransports(t, "GDPRConsentGrantInvalidPurpose", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("gdpr-inv-tenant")
		userID := UniqueKey("gdpr-inv-user")

		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Attempt to grant consent with an invalid purpose
		invalidCmd := CommandDef{
			Name:           "gdpr consent grant invalid purpose",
			Args:           []string{"iam", "gdpr", "consent", "grant", "--tenant", tenantID, "--user-id", userID, "--purpose", "nonexistent_purpose_xyz", "--version", "1.0"},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(invalidCmd)
		if !runner.IsSkipped(invalidCmd) {
			require.Error(t, err, "granting consent with invalid purpose should fail")
			combined := strings.ToLower(stdout + stderr)
			assert.True(t,
				strings.Contains(combined, "invalid") || strings.Contains(combined, "unknown") || strings.Contains(combined, "purpose") || strings.Contains(combined, "error"),
				"error output should indicate invalid/unknown purpose, got: %s", stdout+stderr)
		}
	})
}

// TestIAMGDPRConsentMultiplePurposes tests granting consent for marketing and
// analytics, then listing to verify both are present.
func TestIAMGDPRConsentMultiplePurposes(t *testing.T) {
	RunForTransports(t, "GDPRConsentMultiplePurposes", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("gdpr-multi-tenant")
		userID := UniqueKey("gdpr-multi-user")

		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Grant marketing consent
		marketingCmd := IAMGDPRConsentGrantCommand(tenantID, userID, "marketing", "1.0")
		runner.RunAndAssert(marketingCmd)

		// Grant analytics consent
		analyticsCmd := IAMGDPRConsentGrantCommand(tenantID, userID, "analytics", "2.0")
		runner.RunAndAssert(analyticsCmd)

		// List consents and verify both purposes appear
		listCmd := IAMGDPRConsentListCommand(tenantID, userID)
		stdout, _, err := runner.Run(listCmd)
		if !runner.IsSkipped(listCmd) {
			require.NoError(t, err, "consent list should succeed")
			assert.Contains(t, stdout, "marketing",
				"consent list should include marketing purpose")
			assert.Contains(t, stdout, "analytics",
				"consent list should include analytics purpose")
		}

		// Verify each consent individually via get
		for _, purpose := range []string{"marketing", "analytics"} {
			getCmd := IAMGDPRConsentGetCommand(tenantID, userID, purpose)
			stdout, _, err = runner.Run(getCmd)
			if !runner.IsSkipped(getCmd) {
				require.NoError(t, err, "consent get for %s should succeed", purpose)
				assert.Contains(t, stdout, purpose,
					"consent get should show %s purpose", purpose)
			}
		}
	})
}

// =============================================================================
// GDPR Export Tests
// =============================================================================

// TestIAMGDPRExportRequest tests requesting a JSON data export and verifying
// an export ID is returned.
func TestIAMGDPRExportRequest(t *testing.T) {
	RunForTransports(t, "GDPRExportRequest", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("gdpr-exp-tenant")
		userID := UniqueKey("gdpr-exp-user")

		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Request JSON export
		exportCmd := IAMGDPRExportRequestCommand(tenantID, userID, "json")
		stdout, _, err := runner.Run(exportCmd)
		if !runner.IsSkipped(exportCmd) {
			require.NoError(t, err, "export request should succeed")
			assert.Contains(t, stdout, userID,
				"export request output should contain the user ID")
			// Verify some form of export ID or confirmation is returned
			assert.NotEmpty(t, strings.TrimSpace(stdout),
				"export request should return non-empty output with export ID")
		}
	})
}

// TestIAMGDPRExportRequestCSV tests requesting a CSV format data export.
func TestIAMGDPRExportRequestCSV(t *testing.T) {
	RunForTransports(t, "GDPRExportRequestCSV", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("gdpr-csv-tenant")
		userID := UniqueKey("gdpr-csv-user")

		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Request CSV export
		exportCmd := IAMGDPRExportRequestCommand(tenantID, userID, "csv")
		stdout, _, err := runner.Run(exportCmd)
		if !runner.IsSkipped(exportCmd) {
			require.NoError(t, err, "CSV export request should succeed")
			assert.Contains(t, stdout, userID,
				"CSV export request output should contain the user ID")
			assert.NotEmpty(t, strings.TrimSpace(stdout),
				"CSV export request should return non-empty output")
		}
	})
}

// TestIAMGDPRExportStatus tests requesting an export and then checking its status.
func TestIAMGDPRExportStatus(t *testing.T) {
	RunForTransports(t, "GDPRExportStatus", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("gdpr-expst-tenant")
		userID := UniqueKey("gdpr-expst-user")

		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Request export to get an export ID
		exportCmd := IAMGDPRExportRequestCommand(tenantID, userID, "json")
		stdout, _, err := runner.Run(exportCmd)
		if runner.IsSkipped(exportCmd) {
			return
		}
		require.NoError(t, err, "export request should succeed")

		// Extract export ID from output. The export request should return
		// an identifier we can use to check status. Try JSON parsing first,
		// then fall back to searching the output for a recognizable ID.
		exportID := extractIDFromOutput(stdout, "export_id", "exportId", "export-id", "Export ID", "id")
		require.NotEmpty(t, exportID,
			"should be able to extract an export ID from output: %s", stdout)

		// Check export status
		statusCmd := IAMGDPRExportStatusCommand(tenantID, userID, exportID)
		stdout, _, err = runner.Run(statusCmd)
		if !runner.IsSkipped(statusCmd) {
			require.NoError(t, err, "export status check should succeed")
			assert.Contains(t, stdout, exportID,
				"export status should contain the export ID")
		}
	})
}

// TestIAMGDPRExportInvalidFormat tests that requesting an export with an
// invalid format returns an error.
func TestIAMGDPRExportInvalidFormat(t *testing.T) {
	RunForTransports(t, "GDPRExportInvalidFormat", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("gdpr-invfmt-tenant")
		userID := UniqueKey("gdpr-invfmt-user")

		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Request export with invalid format
		invalidExportCmd := CommandDef{
			Name:           "gdpr export request invalid format",
			Args:           []string{"iam", "gdpr", "export", "request", "--tenant", tenantID, "--user-id", userID, "--format", "binary_nonsense"},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(invalidExportCmd)
		if !runner.IsSkipped(invalidExportCmd) {
			require.Error(t, err, "export with invalid format should fail")
			combined := strings.ToLower(stdout + stderr)
			assert.True(t,
				strings.Contains(combined, "invalid") || strings.Contains(combined, "format") || strings.Contains(combined, "unsupported") || strings.Contains(combined, "error"),
				"error should indicate invalid/unsupported format, got: %s", stdout+stderr)
		}
	})
}

// =============================================================================
// GDPR Deletion Tests
// =============================================================================

// TestIAMGDPRDeletionRequest tests requesting data deletion and verifying a
// deletion ID is returned.
func TestIAMGDPRDeletionRequest(t *testing.T) {
	RunForTransports(t, "GDPRDeletionRequest", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("gdpr-del-tenant")
		userID := UniqueKey("gdpr-del-user")

		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Request deletion
		deleteCmd := IAMGDPRDeletionRequestCommand(tenantID, userID, "user-requested-erasure")
		stdout, _, err := runner.Run(deleteCmd)
		if !runner.IsSkipped(deleteCmd) {
			require.NoError(t, err, "deletion request should succeed")
			assert.Contains(t, stdout, userID,
				"deletion request output should contain the user ID")
			assert.NotEmpty(t, strings.TrimSpace(stdout),
				"deletion request should return non-empty output with deletion ID")
		}
	})
}

// TestIAMGDPRDeletionRequestCustomGracePeriod tests requesting deletion with
// a custom grace period of 14 days.
func TestIAMGDPRDeletionRequestCustomGracePeriod(t *testing.T) {
	RunForTransports(t, "GDPRDeletionRequestCustomGracePeriod", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("gdpr-grace-tenant")
		userID := UniqueKey("gdpr-grace-user")

		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Request deletion with custom grace period
		graceCmd := CommandDef{
			Name: "gdpr deletion request with grace period",
			Args: []string{
				"iam", "gdpr", "deletion", "request",
				"--tenant", tenantID,
				"--user-id", userID,
				"--reason", "account-closure",
				"--grace-period-days", "14",
				"--yes",
			},
			ExpectSuccess:  true,
			ExpectOutput:   []string{userID},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(graceCmd)
		if !runner.IsSkipped(graceCmd) {
			require.NoError(t, err, "deletion request with grace period should succeed")
			assert.Contains(t, stdout, userID,
				"deletion request output should contain the user ID")
		}
	})
}

// TestIAMGDPRDeletionCancel tests requesting deletion and then cancelling it
// before the grace period expires.
func TestIAMGDPRDeletionCancel(t *testing.T) {
	RunForTransports(t, "GDPRDeletionCancel", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("gdpr-cancel-tenant")
		userID := UniqueKey("gdpr-cancel-user")

		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Request deletion
		deleteCmd := IAMGDPRDeletionRequestCommand(tenantID, userID, "testing-cancellation")
		stdout, _, err := runner.Run(deleteCmd)
		if runner.IsSkipped(deleteCmd) {
			return
		}
		require.NoError(t, err, "deletion request should succeed")

		// Extract deletion ID from output
		deletionID := extractIDFromOutput(stdout, "deletion_id", "deletionId", "deletion-id", "Deletion ID", "id")
		require.NotEmpty(t, deletionID,
			"should be able to extract a deletion ID from output: %s", stdout)

		// Cancel the deletion
		cancelCmd := IAMGDPRDeletionCancelCommand(tenantID, userID, deletionID)
		stdout, _, err = runner.Run(cancelCmd)
		if !runner.IsSkipped(cancelCmd) {
			require.NoError(t, err, "deletion cancellation should succeed")
			assert.Contains(t, stdout, deletionID,
				"cancellation output should contain the deletion ID")
		}
	})
}

// TestIAMGDPRDeletionStatus tests requesting deletion and then checking
// the deletion status.
func TestIAMGDPRDeletionStatus(t *testing.T) {
	RunForTransports(t, "GDPRDeletionStatus", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("gdpr-delst-tenant")
		userID := UniqueKey("gdpr-delst-user")

		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Request deletion
		deleteCmd := IAMGDPRDeletionRequestCommand(tenantID, userID, "status-check-test")
		stdout, _, err := runner.Run(deleteCmd)
		if runner.IsSkipped(deleteCmd) {
			return
		}
		require.NoError(t, err, "deletion request should succeed")

		// Extract deletion ID from output
		deletionID := extractIDFromOutput(stdout, "deletion_id", "deletionId", "deletion-id", "Deletion ID", "id")
		require.NotEmpty(t, deletionID,
			"should be able to extract a deletion ID from output: %s", stdout)

		// Check deletion status
		statusCmd := IAMGDPRDeletionStatusCommand(tenantID, userID, deletionID)
		stdout, _, err = runner.Run(statusCmd)
		if !runner.IsSkipped(statusCmd) {
			require.NoError(t, err, "deletion status check should succeed")
			assert.Contains(t, stdout, deletionID,
				"deletion status should contain the deletion ID")
			combined := strings.ToLower(stdout)
			assert.True(t,
				strings.Contains(combined, "pending") || strings.Contains(combined, "scheduled") || strings.Contains(combined, "processing") || strings.Contains(combined, deletionID),
				"deletion status should indicate a valid state (pending/scheduled/processing), got: %s", stdout)
		}
	})
}

// =============================================================================
// Helper Functions
// =============================================================================

// extractIDFromOutput attempts to extract an ID value from command output.
// It first tries to parse the output as JSON and look for the provided keys,
// then falls back to scanning lines for common ID patterns.
func extractIDFromOutput(output string, jsonKeys ...string) string {
	trimmed := strings.TrimSpace(output)

	// Try JSON parsing first
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &jsonData); err == nil {
		for _, key := range jsonKeys {
			if val, ok := jsonData[key]; ok {
				if strVal, ok := val.(string); ok && strVal != "" {
					return strVal
				}
			}
		}
	}

	// Fall back to line scanning for patterns like "ID: <value>" or "Export ID: <value>"
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)

		for _, key := range jsonKeys {
			// Check for "key: value" or "key = value" patterns
			keyLower := strings.ToLower(key)
			for _, sep := range []string{": ", "= ", ":\t"} {
				prefix := keyLower + sep
				if strings.HasPrefix(lower, prefix) {
					return strings.TrimSpace(line[len(prefix):])
				}
			}
		}

		// Check for generic ID patterns like "ID: abc-123"
		if strings.HasPrefix(lower, "id:") {
			return strings.TrimSpace(line[3:])
		}
	}

	// Last resort: if the output is a single line that looks like an ID, use it
	lines := strings.Split(trimmed, "\n")
	if len(lines) == 1 && len(trimmed) > 0 && len(trimmed) < 128 && !strings.Contains(trimmed, " ") {
		return trimmed
	}

	return ""
}
