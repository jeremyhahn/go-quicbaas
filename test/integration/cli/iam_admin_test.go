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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Helper Functions
// =============================================================================

// createAssignmentJSON creates an admin assignment with --output json and returns
// the parsed JSON map. The caller is responsible for cleanup.
func createAssignmentJSON(t *testing.T, runner *TestRunner, userID, role, grantedBy string) map[string]interface{} {
	t.Helper()
	cmd := CommandDef{
		Name: fmt.Sprintf("iam admin assignment create %s %s json", userID, role),
		Args: []string{
			"iam", "admin", "assignment", "create",
			"--user", userID, "--role", role,
			"--granted-by", grantedBy, "--output", "json",
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
	stdout, stderr, err := runner.Run(cmd)
	require.NoError(t, err, "failed to create assignment: stdout=%s stderr=%s", stdout, stderr)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result),
		"create assignment output should be valid JSON: %s", stdout)
	return result
}

// extractAssignmentID extracts the assignment ID from a JSON response map.
func extractAssignmentID(t *testing.T, result map[string]interface{}) string {
	t.Helper()
	id, ok := result["id"].(string)
	require.True(t, ok, "response should contain id as string, got: %v", result)
	require.NotEmpty(t, id, "assignment id should not be empty")
	return id
}

// deleteAssignment deletes an admin assignment (best-effort cleanup).
func deleteAssignment(t *testing.T, runner *TestRunner, assignmentID string) {
	t.Helper()
	cmd := IAMAdminAssignmentDeleteCommand(assignmentID)
	_, _, _ = runner.Run(cmd)
}

// startImpersonationJSON starts an impersonation session with --output json and
// returns the parsed JSON map. The caller is responsible for cleanup.
//
// There is deliberately NO admin_user_id argument: the acting admin is the
// AUTHENTICATED caller behind the runner's credential, resolved server-side
// (go-quicbaas#383). The runner authenticates as the integration cluster's
// firstboot super_admin (the admin.crt identity), which already holds
// user:impersonate, so no test-created assignment is needed — and none
// could name a different actor if it tried.
func startImpersonationJSON(t *testing.T, runner *TestRunner, targetUserID, tenantID, reason string) map[string]interface{} {
	t.Helper()
	cmd := CommandDef{
		Name: fmt.Sprintf("iam admin impersonate start as %s json", targetUserID),
		Args: []string{
			"iam", "admin", "impersonate", "start",
			"--target-user-id", targetUserID,
			"--tenant-id", tenantID, "--reason", reason, "--output", "json",
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
	stdout, stderr, err := runner.Run(cmd)
	require.NoError(t, err, "failed to start impersonation: stdout=%s stderr=%s", stdout, stderr)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result),
		"impersonate start output should be valid JSON: %s", stdout)
	return result
}

// extractSessionID extracts the session ID from a JSON response map.
// The ImpersonationSession struct serializes the field as "id" (json:"id").
func extractSessionID(t *testing.T, result map[string]interface{}) string {
	t.Helper()
	id, ok := result["id"].(string)
	require.True(t, ok, "response should contain id as string, got: %v", result)
	require.NotEmpty(t, id, "id should not be empty")
	return id
}

// endImpersonation ends an impersonation session (best-effort cleanup).
func endImpersonation(t *testing.T, runner *TestRunner, sessionID string) {
	t.Helper()
	cmd := IAMAdminImpersonateEndCommand(sessionID)
	_, _, _ = runner.Run(cmd)
}

// =============================================================================
// Assignment Lifecycle Tests
// =============================================================================

// TestIAMAdminAssignmentLifecycle tests the full CRUD lifecycle for admin
// role assignments: create, get, update, list, and delete.
func TestIAMAdminAssignmentLifecycle(t *testing.T) {
	RunForTransports(t, "IAMAdminAssignmentLifecycle", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		userID := UniqueKey("lifecycle-admin-user")
		role := "tenant_admin"
		grantedBy := "admin"

		// Create assignment via JSON to capture ID
		result := createAssignmentJSON(t, runner, userID, role, grantedBy)
		assignmentID := extractAssignmentID(t, result)

		defer deleteAssignment(t, runner, assignmentID)

		// Get assignment and verify details
		getCmd := IAMAdminAssignmentGetCommand(assignmentID)
		stdout, _, err := runner.Run(getCmd)
		require.NoError(t, err, "get assignment should succeed")
		assert.Contains(t, stdout, assignmentID, "get output should contain assignment ID")
		assert.Contains(t, stdout, userID, "get output should contain user ID")

		// Update role
		updatedRole := "policy_admin"
		updateCmd := IAMAdminAssignmentUpdateCommand(assignmentID, updatedRole)
		stdout, _, err = runner.Run(updateCmd)
		require.NoError(t, err, "update assignment should succeed")
		assert.Contains(t, stdout, assignmentID, "update output should contain assignment ID")

		// List assignments and verify presence
		listCmd := IAMAdminAssignmentListCommand()
		stdout, _, err = runner.Run(listCmd)
		require.NoError(t, err, "list assignments should succeed")
		assert.Contains(t, stdout, assignmentID, "list output should contain assignment ID")

		// Delete assignment
		deleteCmd := IAMAdminAssignmentDeleteCommand(assignmentID)
		stdout, _, err = runner.Run(deleteCmd)
		require.NoError(t, err, "delete assignment should succeed")
		assert.Contains(t, stdout, assignmentID, "delete output should confirm assignment ID")

		// Verify get after delete fails
		getAfterDeleteCmd := CommandDef{
			Name:           fmt.Sprintf("iam admin assignment get %s after delete", assignmentID),
			Args:           []string{"iam", "admin", "assignment", "get", "--assignment-id", assignmentID},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		_, _, err = runner.Run(getAfterDeleteCmd)
		assert.Error(t, err, "get after delete should fail")
	})
}

// TestIAMAdminAssignmentCreate tests creating a tenant_admin assignment
// and verifying the output contains expected fields.
func TestIAMAdminAssignmentCreate(t *testing.T) {
	RunForTransports(t, "IAMAdminAssignmentCreate", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		userID := UniqueKey("create-admin-user")
		role := "tenant_admin"
		grantedBy := "admin"

		createCmd := IAMAdminAssignmentCreateCommand(userID, role, grantedBy, nil)
		stdout, _, err := runner.Run(createCmd)
		require.NoError(t, err, "create assignment should succeed")
		assert.Contains(t, stdout, userID, "output should contain user ID")

		// Clean up: extract ID from JSON create for deletion
		result := createAssignmentJSON(t, runner, UniqueKey("create-cleanup"), role, grantedBy)
		cleanupID := extractAssignmentID(t, result)
		defer deleteAssignment(t, runner, cleanupID)
	})
}

// TestIAMAdminAssignmentCreateJSON tests creating an assignment with --output json
// and verifying the response is valid JSON with an assignment ID.
func TestIAMAdminAssignmentCreateJSON(t *testing.T) {
	RunForTransports(t, "IAMAdminAssignmentCreateJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		userID := UniqueKey("createjson-admin-user")
		role := "user_admin"
		grantedBy := "admin"

		result := createAssignmentJSON(t, runner, userID, role, grantedBy)
		assignmentID := extractAssignmentID(t, result)
		defer deleteAssignment(t, runner, assignmentID)

		// Verify JSON response contains expected fields
		assert.NotEmpty(t, result["id"], "JSON response should have id field")
		assert.Equal(t, userID, result["user_id"], "JSON response should have correct user_id")
	})
}

// TestIAMAdminAssignmentCreateWithPermissions tests creating an assignment
// with explicit permissions using the --permissions flag.
func TestIAMAdminAssignmentCreateWithPermissions(t *testing.T) {
	RunForTransports(t, "IAMAdminAssignmentCreateWithPermissions", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		userID := UniqueKey("perms-admin-user")
		role := "tenant_admin"
		grantedBy := "admin"

		cmd := CommandDef{
			Name: "iam admin assignment create with permissions",
			Args: []string{
				"iam", "admin", "assignment", "create",
				"--user", userID, "--role", role,
				"--granted-by", grantedBy,
				"--permissions", "user:create,user:read",
				"--output", "json",
			},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(cmd)
		require.NoError(t, err, "create with permissions should succeed: stdout=%s stderr=%s", stdout, stderr)

		var result map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(stdout), &result),
			"output should be valid JSON: %s", stdout)

		assignmentID := extractAssignmentID(t, result)
		defer deleteAssignment(t, runner, assignmentID)

		assert.NotEmpty(t, assignmentID, "assignment should have an ID")
	})
}

// TestIAMAdminAssignmentGet tests creating an assignment and then retrieving it
// to verify all details are returned correctly.
func TestIAMAdminAssignmentGet(t *testing.T) {
	RunForTransports(t, "IAMAdminAssignmentGet", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		userID := UniqueKey("get-admin-user")
		role := "group_admin"
		grantedBy := "admin"

		result := createAssignmentJSON(t, runner, userID, role, grantedBy)
		assignmentID := extractAssignmentID(t, result)
		defer deleteAssignment(t, runner, assignmentID)

		getCmd := IAMAdminAssignmentGetCommand(assignmentID)
		stdout, _, err := runner.Run(getCmd)
		require.NoError(t, err, "get assignment should succeed")
		assert.Contains(t, stdout, assignmentID, "get output should contain assignment ID")
		assert.Contains(t, stdout, userID, "get output should contain user ID")
	})
}

// TestIAMAdminAssignmentGetJSON tests retrieving an assignment with --output json
// and verifying the JSON structure.
func TestIAMAdminAssignmentGetJSON(t *testing.T) {
	RunForTransports(t, "IAMAdminAssignmentGetJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		userID := UniqueKey("getjson-admin-user")
		role := "audit_admin"
		grantedBy := "admin"

		createResult := createAssignmentJSON(t, runner, userID, role, grantedBy)
		assignmentID := extractAssignmentID(t, createResult)
		defer deleteAssignment(t, runner, assignmentID)

		getCmd := CommandDef{
			Name:           fmt.Sprintf("iam admin assignment get %s json", assignmentID),
			Args:           []string{"iam", "admin", "assignment", "get", "--assignment-id", assignmentID, "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(getCmd)
		require.NoError(t, err, "get assignment JSON should succeed")

		var result map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(stdout), &result),
			"get output should be valid JSON: %s", stdout)
		assert.Equal(t, assignmentID, result["id"], "JSON id should match")
		assert.Equal(t, userID, result["user_id"], "JSON user_id should match")
	})
}

// TestIAMAdminAssignmentUpdate tests updating an assignment's role and verifying
// the change is reflected in subsequent get operations.
func TestIAMAdminAssignmentUpdate(t *testing.T) {
	RunForTransports(t, "IAMAdminAssignmentUpdate", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		userID := UniqueKey("update-admin-user")
		initialRole := "tenant_admin"
		updatedRole := "policy_admin"
		grantedBy := "admin"

		result := createAssignmentJSON(t, runner, userID, initialRole, grantedBy)
		assignmentID := extractAssignmentID(t, result)
		defer deleteAssignment(t, runner, assignmentID)

		// Update role
		updateCmd := IAMAdminAssignmentUpdateCommand(assignmentID, updatedRole)
		stdout, _, err := runner.Run(updateCmd)
		require.NoError(t, err, "update assignment should succeed")
		assert.Contains(t, stdout, assignmentID, "update output should contain assignment ID")

		// Verify update via get
		getCmd := CommandDef{
			Name:           fmt.Sprintf("iam admin assignment get %s after update", assignmentID),
			Args:           []string{"iam", "admin", "assignment", "get", "--assignment-id", assignmentID, "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err = runner.Run(getCmd)
		require.NoError(t, err, "get after update should succeed")

		var getResult map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(stdout), &getResult),
			"get output should be valid JSON: %s", stdout)
		assert.Equal(t, assignmentID, getResult["id"], "id should be unchanged after update")
	})
}

// TestIAMAdminAssignmentUpdateDeactivate tests deactivating an assignment
// by setting --active=false.
func TestIAMAdminAssignmentUpdateDeactivate(t *testing.T) {
	RunForTransports(t, "IAMAdminAssignmentUpdateDeactivate", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		userID := UniqueKey("deactivate-admin-user")
		role := "tenant_admin"
		grantedBy := "admin"

		result := createAssignmentJSON(t, runner, userID, role, grantedBy)
		assignmentID := extractAssignmentID(t, result)
		defer deleteAssignment(t, runner, assignmentID)

		// Deactivate assignment
		deactivateCmd := CommandDef{
			Name: fmt.Sprintf("iam admin assignment update %s deactivate", assignmentID),
			Args: []string{
				"iam", "admin", "assignment", "update", "--assignment-id", assignmentID,
				"--active=false",
			},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(deactivateCmd)
		require.NoError(t, err, "deactivate assignment should succeed")
		assert.Contains(t, stdout, assignmentID, "deactivate output should contain assignment ID")

		// Verify via get JSON
		getCmd := CommandDef{
			Name:           fmt.Sprintf("iam admin assignment get %s after deactivate", assignmentID),
			Args:           []string{"iam", "admin", "assignment", "get", "--assignment-id", assignmentID, "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err = runner.Run(getCmd)
		require.NoError(t, err, "get after deactivate should succeed")

		var getResult map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(stdout), &getResult),
			"get output should be valid JSON: %s", stdout)
		assert.Equal(t, false, getResult["active"], "assignment should be inactive after deactivation")
	})
}

// TestIAMAdminAssignmentDelete tests creating and then deleting an assignment,
// verifying that a subsequent get fails.
func TestIAMAdminAssignmentDelete(t *testing.T) {
	RunForTransports(t, "IAMAdminAssignmentDelete", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		userID := UniqueKey("delete-admin-user")
		role := "read_only"
		grantedBy := "admin"

		result := createAssignmentJSON(t, runner, userID, role, grantedBy)
		assignmentID := extractAssignmentID(t, result)

		// Delete assignment
		deleteCmd := IAMAdminAssignmentDeleteCommand(assignmentID)
		stdout, _, err := runner.Run(deleteCmd)
		require.NoError(t, err, "delete assignment should succeed")
		assert.Contains(t, stdout, assignmentID, "delete output should confirm assignment ID")

		// Verify get after delete fails
		getCmd := CommandDef{
			Name:           fmt.Sprintf("iam admin assignment get %s after delete", assignmentID),
			Args:           []string{"iam", "admin", "assignment", "get", "--assignment-id", assignmentID},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		_, _, err = runner.Run(getCmd)
		assert.Error(t, err, "get after delete should fail with not found")
	})
}

// TestIAMAdminAssignmentList tests creating multiple assignments and verifying
// both appear in the list output.
func TestIAMAdminAssignmentList(t *testing.T) {
	RunForTransports(t, "IAMAdminAssignmentList", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		userID1 := UniqueKey("list-admin-user-1")
		userID2 := UniqueKey("list-admin-user-2")
		grantedBy := "admin"

		result1 := createAssignmentJSON(t, runner, userID1, "tenant_admin", grantedBy)
		assignmentID1 := extractAssignmentID(t, result1)
		defer deleteAssignment(t, runner, assignmentID1)

		result2 := createAssignmentJSON(t, runner, userID2, "policy_admin", grantedBy)
		assignmentID2 := extractAssignmentID(t, result2)
		defer deleteAssignment(t, runner, assignmentID2)

		// List all assignments
		listCmd := IAMAdminAssignmentListCommand()
		stdout, _, err := runner.Run(listCmd)
		require.NoError(t, err, "list assignments should succeed")
		assert.Contains(t, stdout, assignmentID1, "list output should contain first assignment")
		assert.Contains(t, stdout, assignmentID2, "list output should contain second assignment")
	})
}

// TestIAMAdminAssignmentListActiveOnly tests listing assignments with the
// --active-only flag, verifying deactivated assignments are excluded.
func TestIAMAdminAssignmentListActiveOnly(t *testing.T) {
	RunForTransports(t, "IAMAdminAssignmentListActiveOnly", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		activeUserID := UniqueKey("active-list-user")
		inactiveUserID := UniqueKey("inactive-list-user")
		grantedBy := "admin"

		// Create active assignment
		activeResult := createAssignmentJSON(t, runner, activeUserID, "tenant_admin", grantedBy)
		activeID := extractAssignmentID(t, activeResult)
		defer deleteAssignment(t, runner, activeID)

		// Create assignment and deactivate it
		inactiveResult := createAssignmentJSON(t, runner, inactiveUserID, "policy_admin", grantedBy)
		inactiveID := extractAssignmentID(t, inactiveResult)
		defer deleteAssignment(t, runner, inactiveID)

		deactivateCmd := CommandDef{
			Name: fmt.Sprintf("deactivate assignment %s", inactiveID),
			Args: []string{
				"iam", "admin", "assignment", "update", "--assignment-id", inactiveID,
				"--active=false",
			},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		_, _, err := runner.Run(deactivateCmd)
		require.NoError(t, err, "deactivate should succeed")

		// List with --active-only
		listCmd := CommandDef{
			Name:           "iam admin assignment list active-only",
			Args:           []string{"iam", "admin", "assignment", "list", "--active-only"},
			ExpectSuccess:  true,
			Timeout:        15 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(listCmd)
		require.NoError(t, err, "list active-only should succeed")
		assert.Contains(t, stdout, activeID, "active-only list should contain the active assignment")
		assert.NotContains(t, stdout, inactiveID, "active-only list should not contain the deactivated assignment")
	})
}

// TestIAMAdminAssignmentCreateInvalidRole tests that creating an assignment
// with an invalid role returns an error.
func TestIAMAdminAssignmentCreateInvalidRole(t *testing.T) {
	RunForTransports(t, "IAMAdminAssignmentCreateInvalidRole", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		userID := UniqueKey("invalid-role-user")
		invalidRole := "nonexistent_role"
		grantedBy := "admin"

		cmd := CommandDef{
			Name: "iam admin assignment create invalid role",
			Args: []string{
				"iam", "admin", "assignment", "create",
				"--user", userID, "--role", invalidRole,
				"--granted-by", grantedBy,
			},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		_, stderr, err := runner.Run(cmd)
		assert.Error(t, err, "create with invalid role should fail")
		assert.NotEmpty(t, stderr, "stderr should contain an error message for invalid role")
	})
}

// TestIAMAdminAssignmentGetNonExistent tests that getting a nonexistent
// assignment returns an appropriate error.
func TestIAMAdminAssignmentGetNonExistent(t *testing.T) {
	RunForTransports(t, "IAMAdminAssignmentGetNonExistent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		nonExistentID := UniqueKey("nonexistent-assignment")
		cmd := CommandDef{
			Name:           fmt.Sprintf("iam admin assignment get %s nonexistent", nonExistentID),
			Args:           []string{"iam", "admin", "assignment", "get", "--assignment-id", nonExistentID},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		_, _, err := runner.Run(cmd)
		assert.Error(t, err, "get nonexistent assignment should fail")
	})
}

// =============================================================================
// Check Permission Tests
// =============================================================================

// TestIAMAdminCheckPermission tests checking a permission for a user and
// verifying the response contains permission check results.
func TestIAMAdminCheckPermission(t *testing.T) {
	RunForTransports(t, "IAMAdminCheckPermission", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		userID := UniqueKey("checkperm-user")
		permission := "user:read"
		grantedBy := "admin"

		// Create an assignment so the user has a role
		result := createAssignmentJSON(t, runner, userID, "tenant_admin", grantedBy)
		assignmentID := extractAssignmentID(t, result)
		defer deleteAssignment(t, runner, assignmentID)

		// Check permission
		checkCmd := IAMAdminCheckPermissionCommand(userID, permission)
		stdout, _, err := runner.Run(checkCmd)
		require.NoError(t, err, "check-permission should succeed")
		assert.Contains(t, stdout, userID, "check output should contain user ID")
	})
}

// TestIAMAdminCheckPermissionWithTenant tests checking a permission with a
// specific tenant scope using the --tenant flag.
func TestIAMAdminCheckPermissionWithTenant(t *testing.T) {
	RunForTransports(t, "IAMAdminCheckPermissionWithTenant", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		userID := UniqueKey("checkperm-tenant-user")
		tenantID := UniqueKey("checkperm-tenant")
		permission := "user:create"
		grantedBy := "admin"

		// Create an assignment with tenant scope
		result := createAssignmentJSON(t, runner, userID, "tenant_admin", grantedBy)
		assignmentID := extractAssignmentID(t, result)
		defer deleteAssignment(t, runner, assignmentID)

		// Check permission with tenant
		checkCmd := IAMAdminCheckPermissionWithTenantCommand(userID, permission, tenantID)
		stdout, _, err := runner.Run(checkCmd)
		require.NoError(t, err, "check-permission with tenant should succeed")
		assert.Contains(t, stdout, userID, "check output should contain user ID")
	})
}

// TestIAMAdminCheckPermissionWithResource tests checking a permission with
// --resource-type and --resource-id flags for fine-grained resource access checks.
func TestIAMAdminCheckPermissionWithResource(t *testing.T) {
	RunForTransports(t, "IAMAdminCheckPermissionWithResource", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		userID := UniqueKey("checkperm-resource-user")
		permission := "document:read"
		resourceType := "document"
		resourceID := UniqueKey("resource")
		grantedBy := "admin"

		// Create an assignment so the user has a role
		result := createAssignmentJSON(t, runner, userID, "tenant_admin", grantedBy)
		assignmentID := extractAssignmentID(t, result)
		defer deleteAssignment(t, runner, assignmentID)

		// Check permission with resource type and ID
		checkCmd := CommandDef{
			Name: fmt.Sprintf("iam admin check-permission %s %s resource", userID, permission),
			Args: []string{
				"iam", "admin", "check-permission",
				"--user-id", userID, "--permission", permission,
				"--resource-type", resourceType, "--resource-id", resourceID,
			},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(checkCmd)
		require.NoError(t, err, "check-permission with resource should succeed")
		assert.Contains(t, stdout, userID, "check output should contain user ID")
	})
}

// =============================================================================
// Impersonate Tests
// =============================================================================

// TestIAMAdminImpersonateStart tests starting an impersonation session and
// verifying a session ID is returned.
func TestIAMAdminImpersonateStart(t *testing.T) {
	RunForTransports(t, "IAMAdminImpersonateStart", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		targetUserID := UniqueKey("impersonate-target")
		tenantID := UniqueKey("impersonate-tenant")
		reason := "integration test impersonation"

		// No test-created admin assignment: the authenticated caller (the
		// runner's firstboot super_admin credential) already holds
		// user:impersonate, and the actor can no longer be named by the
		// request (go-quicbaas#383).
		result := startImpersonationJSON(t, runner, targetUserID, tenantID, reason)
		sessionID := extractSessionID(t, result)
		defer endImpersonation(t, runner, sessionID)

		assert.NotEmpty(t, sessionID, "impersonation session should have a session ID")
		assert.Equal(t, targetUserID, result["target_user_id"],
			"response should contain the target user ID")

		// The recorded actor is the AUTHENTICATED caller, resolved
		// server-side — never a client-chosen value, and never the target.
		// The CLI serializes the session's acting admin as "user_id"
		// (ImpersonationSession.AdminID json:"user_id").
		actor, _ := result["user_id"].(string)
		assert.NotEmpty(t, actor, "session must record the authenticated caller as the acting admin")
		assert.NotEqual(t, targetUserID, actor, "the acting admin must not be the impersonation target")
	})
}

// TestIAMAdminImpersonateEnd tests starting and then ending an impersonation
// session, verifying the session is terminated successfully.
func TestIAMAdminImpersonateEnd(t *testing.T) {
	RunForTransports(t, "IAMAdminImpersonateEnd", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		targetUserID := UniqueKey("end-impersonate-target")
		tenantID := UniqueKey("end-impersonate-tenant")
		reason := "integration test end impersonation"

		// The authenticated caller is already privileged (#383).
		result := startImpersonationJSON(t, runner, targetUserID, tenantID, reason)
		sessionID := extractSessionID(t, result)

		// End the impersonation session
		endCmd := IAMAdminImpersonateEndCommand(sessionID)
		stdout, _, err := runner.Run(endCmd)
		require.NoError(t, err, "end impersonation should succeed")
		assert.Contains(t, stdout, sessionID, "end output should confirm session ID")
	})
}

// TestIAMAdminImpersonateList tests listing active impersonation sessions
// for an admin user.
func TestIAMAdminImpersonateList(t *testing.T) {
	RunForTransports(t, "IAMAdminImpersonateList", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		targetUserID := UniqueKey("list-impersonate-target")
		tenantID := UniqueKey("list-impersonate-tenant")
		reason := "integration test list impersonations"

		// Start a session so the list has something to return. The actor
		// is the authenticated caller (#383), so list by the id the
		// session actually recorded rather than a client-chosen one.
		result := startImpersonationJSON(t, runner, targetUserID, tenantID, reason)
		sessionID := extractSessionID(t, result)
		defer endImpersonation(t, runner, sessionID)

		// The CLI serializes the acting admin as "user_id"
		// (ImpersonationSession.AdminID json:"user_id").
		actorID, _ := result["user_id"].(string)
		require.NotEmpty(t, actorID, "session must record the authenticated caller as the acting admin")

		// List impersonation sessions for the acting admin (the caller).
		listCmd := IAMAdminImpersonateListCommand(actorID)
		stdout, _, err := runner.Run(listCmd)
		require.NoError(t, err, "list impersonations should succeed")
		assert.Contains(t, stdout, sessionID, "list output should contain the active session ID")
		// Table output truncates long user IDs, so check for a short prefix.
		targetPrefix := targetUserID
		if len(targetPrefix) > 15 {
			targetPrefix = targetPrefix[:15]
		}
		assert.Contains(t, stdout, targetPrefix, "list output should contain target user ID (or prefix)")
	})
}

// TestIAMAdminImpersonateEndNonExistent tests that ending a nonexistent
// impersonation session returns an appropriate error.
func TestIAMAdminImpersonateEndNonExistent(t *testing.T) {
	RunForTransports(t, "IAMAdminImpersonateEndNonExistent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		nonExistentSessionID := UniqueKey("nonexistent-session")
		cmd := CommandDef{
			Name:           fmt.Sprintf("iam admin impersonate end %s nonexistent", nonExistentSessionID),
			Args:           []string{"iam", "admin", "impersonate", "end", "--session-id", nonExistentSessionID},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		_, _, err := runner.Run(cmd)
		assert.Error(t, err, "end nonexistent impersonation session should fail")
	})
}
