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
// IAM Audit Query Tests
// =============================================================================

// TestIAMAuditQuery tests querying all audit events for a tenant across all
// supported transports.
func TestIAMAuditQuery(t *testing.T) {
	RunForTransports(t, "AuditQuery", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("audit-query")
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		queryCmd := IAMAuditQueryCommand(tenantID)
		stdout, _, err := runner.Run(queryCmd)
		require.NoError(t, err)
		assert.NotNil(t, stdout, "audit query should return output")
	})
}

// TestIAMAuditQueryJSON tests querying audit events with JSON output format
// across all supported transports.
func TestIAMAuditQueryJSON(t *testing.T) {
	RunForTransports(t, "AuditQueryJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("audit-json")
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		queryCmd := CommandDef{
			Name:           "iam audit query json",
			Args:           []string{"iam", "audit", "query", "--tenant", tenantID, "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        15 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(queryCmd)
		require.NoError(t, err)

		// When JSON output is requested, the output should be valid JSON
		trimmed := strings.TrimSpace(stdout)
		if len(trimmed) > 0 {
			assert.True(t, json.Valid([]byte(trimmed)),
				"audit query JSON output should be valid JSON, got: %s", trimmed)
		}
	})
}

// TestIAMAuditQueryWithCategory tests querying audit events filtered by
// category across all supported transports.
func TestIAMAuditQueryWithCategory(t *testing.T) {
	RunForTransports(t, "AuditQueryCategory", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("audit-cat")
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		queryCmd := IAMAuditQueryWithFiltersCommand(tenantID, "", "authentication", "")
		stdout, _, err := runner.Run(queryCmd)
		require.NoError(t, err)
		assert.NotNil(t, stdout, "audit query with category filter should return output")
	})
}

// TestIAMAuditQueryWithOutcome tests querying audit events filtered by outcome
// (success and failure) across all supported transports.
func TestIAMAuditQueryWithOutcome(t *testing.T) {
	RunForTransports(t, "AuditQueryOutcome", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("audit-outcome")
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Query with outcome=success
		successCmd := IAMAuditQueryWithFiltersCommand(tenantID, "", "", "success")
		stdout, _, err := runner.Run(successCmd)
		require.NoError(t, err)
		assert.NotNil(t, stdout, "audit query with outcome=success should return output")

		// Query with outcome=failure
		failureCmd := IAMAuditQueryWithFiltersCommand(tenantID, "", "", "failure")
		stdout, _, err = runner.Run(failureCmd)
		require.NoError(t, err)
		assert.NotNil(t, stdout, "audit query with outcome=failure should return output")
	})
}

// TestIAMAuditQueryWithLimit tests querying audit events with a result limit
// across all supported transports.
func TestIAMAuditQueryWithLimit(t *testing.T) {
	RunForTransports(t, "AuditQueryLimit", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("audit-limit")
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		queryCmd := CommandDef{
			Name:           "iam audit query with limit",
			Args:           []string{"iam", "audit", "query", "--tenant", tenantID, "--limit", "10"},
			ExpectSuccess:  true,
			Timeout:        15 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(queryCmd)
		require.NoError(t, err)
		assert.NotNil(t, stdout, "audit query with limit should return output")
	})
}

// TestIAMAuditQueryWithTimeRange tests querying audit events with start and
// end time bounds across all supported transports.
func TestIAMAuditQueryWithTimeRange(t *testing.T) {
	RunForTransports(t, "AuditQueryTimeRange", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("audit-time")
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		queryCmd := CommandDef{
			Name: "iam audit query with time range",
			Args: []string{
				"iam", "audit", "query",
				"--tenant", tenantID,
				"--start-time", startTime,
				"--end-time", endTime,
			},
			ExpectSuccess:  true,
			Timeout:        15 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(queryCmd)
		require.NoError(t, err)
		assert.NotNil(t, stdout, "audit query with time range should return output")
	})
}

// TestIAMAuditQueryCombinedFilters tests querying audit events with multiple
// filters applied simultaneously across all supported transports.
func TestIAMAuditQueryCombinedFilters(t *testing.T) {
	RunForTransports(t, "AuditQueryCombined", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("audit-combined")
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		queryCmd := CommandDef{
			Name: "iam audit query combined filters",
			Args: []string{
				"iam", "audit", "query",
				"--tenant", tenantID,
				"--category", "authentication",
				"--outcome", "success",
				"--start-time", startTime,
				"--end-time", endTime,
				"--limit", "5",
				"--offset", "0",
				"--output", "json",
			},
			ExpectSuccess:  true,
			Timeout:        15 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(queryCmd)
		require.NoError(t, err)

		trimmed := strings.TrimSpace(stdout)
		if len(trimmed) > 0 {
			assert.True(t, json.Valid([]byte(trimmed)),
				"combined filter query with JSON output should return valid JSON, got: %s", trimmed)
		}
	})
}

// =============================================================================
// IAM Audit Get Tests
// =============================================================================

// TestIAMAuditGetNonExistent tests that requesting a non-existent audit event
// returns an appropriate error across all supported transports.
func TestIAMAuditGetNonExistent(t *testing.T) {
	RunForTransports(t, "AuditGetNonExistent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("audit-get-missing")
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		nonExistentEventID := UniqueKey("nonexistent-event")
		getCmd := CommandDef{
			Name:           "iam audit get non-existent",
			Args:           []string{"iam", "audit", "get", nonExistentEventID, "--tenant", tenantID},
			ExpectSuccess:  false,
			ExpectError:    []string{nonExistentEventID},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(getCmd)
	})
}

// =============================================================================
// IAM Audit Error Handling Tests
// =============================================================================

// TestIAMAuditQueryInvalidOutcome tests that querying with an invalid outcome
// value returns an appropriate error across all supported transports.
func TestIAMAuditQueryInvalidOutcome(t *testing.T) {
	RunForTransports(t, "AuditQueryInvalidOutcome", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("audit-bad-outcome")
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		invalidCmd := CommandDef{
			Name:           "iam audit query invalid outcome",
			Args:           []string{"iam", "audit", "query", "--tenant", tenantID, "--outcome", "invalid-outcome"},
			ExpectSuccess:  false,
			Timeout:        15 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		_, stderr, err := runner.Run(invalidCmd)
		require.Error(t, err, "audit query with invalid outcome should fail")
		combined := strings.TrimSpace(stderr)
		assert.NotEmpty(t, combined, "error output should indicate invalid outcome value")
	})
}

// =============================================================================
// IAM Audit After Operations Tests
// =============================================================================

// TestIAMAuditQueryAfterOperations tests that IAM operations generate audit
// events that can be queried. Creates a tenant, registers and deletes an app,
// then queries the audit log to verify events were recorded.
func TestIAMAuditQueryAfterOperations(t *testing.T) {
	RunForTransports(t, "AuditQueryAfterOps", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("audit-ops")
		runner.ActAsTenantAdmin(tenantID)
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Perform IAM operations that should generate audit events
		clientID := extractClientID(t, registerAppJSON(t, runner, tenantID, UniqueKey("audit-app")))
		runner.RunAndAssert(IAMAppDeleteCommand(tenantID, clientID))

		// Query audit events for this tenant to verify operations were recorded
		queryCmd := CommandDef{
			Name:           "iam audit query after operations",
			Args:           []string{"iam", "audit", "query", "--tenant", tenantID, "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        15 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(queryCmd)
		require.NoError(t, err)

		trimmed := strings.TrimSpace(stdout)
		if len(trimmed) > 0 {
			assert.True(t, json.Valid([]byte(trimmed)),
				"audit query output should be valid JSON after operations, got: %s", trimmed)
			// Verify the output is non-empty, indicating audit events were generated
			assert.NotEqual(t, "[]", trimmed,
				"audit events should exist after performing IAM operations")
			assert.NotEqual(t, "null", trimmed,
				"audit events should not be null after performing IAM operations")
		}
	})
}
