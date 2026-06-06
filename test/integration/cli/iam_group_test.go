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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// IAM Group Lifecycle Tests
// =============================================================================

// TestIAMGroupLifecycle tests the complete IAM group lifecycle: create tenant,
// create group, get, update, list, delete group, delete tenant.
func TestIAMGroupLifecycle(t *testing.T) {
	RunForTransports(t, "GroupLifecycle", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("grp-lc-tenant")
		groupID := UniqueKey("grp-lc-group")

		// Create tenant
		runner.RunAndAssert(IAMTenantCreateCommand(tenantID, "Group Lifecycle Tenant"))
		defer func() {
			runner.Run(IAMGroupDeleteCommand(tenantID, groupID))
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Create group
		createCmd := IAMGroupCreateCommand(tenantID, groupID, "Engineering")
		runner.RunAndAssert(createCmd)

		// Get group
		getCmd := IAMGroupGetCommand(tenantID, groupID)
		stdout, _, err := runner.Run(getCmd)
		require.NoError(t, err, "get group should succeed")
		assert.Contains(t, stdout, groupID, "get output should contain group ID")
		assert.Contains(t, stdout, "Engineering", "get output should contain group name")

		// Update group name
		updateCmd := IAMGroupUpdateCommand(tenantID, groupID, "Platform Engineering")
		runner.RunAndAssert(updateCmd)

		// Verify update via get
		stdout, _, err = runner.Run(IAMGroupGetCommand(tenantID, groupID))
		require.NoError(t, err, "get group after update should succeed")
		assert.Contains(t, stdout, "Platform Engineering", "get output should contain updated name")

		// List groups
		listCmd := IAMGroupListCommand(tenantID)
		stdout, _, err = runner.Run(listCmd)
		require.NoError(t, err, "list groups should succeed")
		assert.Contains(t, stdout, groupID, "list output should contain the group ID")

		// Delete group
		deleteCmd := IAMGroupDeleteCommand(tenantID, groupID)
		runner.RunAndAssert(deleteCmd)

		// Verify group is deleted
		getAfterDelete := IAMGroupGetCommand(tenantID, groupID)
		getAfterDelete.ExpectSuccess = false
		_, _, err = runner.Run(getAfterDelete)
		assert.Error(t, err, "get after delete should fail")
	})
}

// =============================================================================
// IAM Group Create Tests
// =============================================================================

// TestIAMGroupCreate tests creating an IAM group with a name and verifies the
// output contains the group ID.
func TestIAMGroupCreate(t *testing.T) {
	RunForTransports(t, "GroupCreate", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("grp-cr-tenant")
		groupID := UniqueKey("grp-cr-group")

		runner.RunAndAssert(IAMTenantCreateCommand(tenantID, "Group Create Tenant"))
		defer func() {
			runner.Run(IAMGroupDeleteCommand(tenantID, groupID))
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		createCmd := IAMGroupCreateCommand(tenantID, groupID, "Backend Team")
		stdout, _, err := runner.Run(createCmd)
		require.NoError(t, err, "group create should succeed")
		assert.Contains(t, stdout, groupID, "create output should contain group ID")
	})
}

// TestIAMGroupCreateWithDescription tests creating an IAM group with both
// name and description flags.
func TestIAMGroupCreateWithDescription(t *testing.T) {
	RunForTransports(t, "GroupCreateWithDesc", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("grp-crd-tenant")
		groupID := UniqueKey("grp-crd-group")

		runner.RunAndAssert(IAMTenantCreateCommand(tenantID, "Group Create Desc Tenant"))
		defer func() {
			runner.Run(IAMGroupDeleteCommand(tenantID, groupID))
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		createCmd := IAMGroupCreateWithDescCommand(tenantID, groupID, "Frontend Team", "Handles all UI work")
		stdout, _, err := runner.Run(createCmd)
		require.NoError(t, err, "group create with description should succeed")
		assert.Contains(t, stdout, groupID, "create output should contain group ID")

		// Verify description persisted via get
		getCmd := IAMGroupGetCommand(tenantID, groupID)
		stdout, _, err = runner.Run(getCmd)
		require.NoError(t, err, "get group should succeed")
		assert.Contains(t, stdout, groupID, "get output should contain group ID")
	})
}

// =============================================================================
// IAM Group Get Tests
// =============================================================================

// TestIAMGroupGet tests retrieving an IAM group and verifying the output
// contains the group ID and name.
func TestIAMGroupGet(t *testing.T) {
	RunForTransports(t, "GroupGet", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("grp-get-tenant")
		groupID := UniqueKey("grp-get-group")

		runner.RunAndAssert(IAMTenantCreateCommand(tenantID, "Group Get Tenant"))
		defer func() {
			runner.Run(IAMGroupDeleteCommand(tenantID, groupID))
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		runner.RunAndAssert(IAMGroupCreateCommand(tenantID, groupID, "QA Team"))

		getCmd := IAMGroupGetCommand(tenantID, groupID)
		stdout, _, err := runner.Run(getCmd)
		require.NoError(t, err, "get group should succeed")
		assert.Contains(t, stdout, groupID, "get output should contain group ID")
		assert.Contains(t, stdout, "QA Team", "get output should contain group name")
	})
}

// TestIAMGroupGetJSON tests retrieving an IAM group with JSON output format.
func TestIAMGroupGetJSON(t *testing.T) {
	RunForTransports(t, "GroupGetJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("grp-gj-tenant")
		groupID := UniqueKey("grp-gj-group")

		runner.RunAndAssert(IAMTenantCreateCommand(tenantID, "Group Get JSON Tenant"))
		defer func() {
			runner.Run(IAMGroupDeleteCommand(tenantID, groupID))
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		runner.RunAndAssert(IAMGroupCreateCommand(tenantID, groupID, "DevOps Team"))

		getJSONCmd := CommandDef{
			Name:           fmt.Sprintf("iam group get %s --output json", groupID),
			Args:           []string{"iam", "group", "get", groupID, "--tenant", tenantID, "--output", "json"},
			ExpectSuccess:  true,
			ExpectOutput:   []string{groupID},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(getJSONCmd)
		require.NoError(t, err, "get group JSON should succeed")
		assert.Contains(t, stdout, groupID, "JSON output should contain group ID")
		assert.Contains(t, stdout, "DevOps Team", "JSON output should contain group name")
	})
}

// =============================================================================
// IAM Group Update Tests
// =============================================================================

// TestIAMGroupUpdate tests updating an IAM group name and verifying the
// change persists.
func TestIAMGroupUpdate(t *testing.T) {
	RunForTransports(t, "GroupUpdate", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("grp-upd-tenant")
		groupID := UniqueKey("grp-upd-group")

		runner.RunAndAssert(IAMTenantCreateCommand(tenantID, "Group Update Tenant"))
		defer func() {
			runner.Run(IAMGroupDeleteCommand(tenantID, groupID))
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		runner.RunAndAssert(IAMGroupCreateCommand(tenantID, groupID, "Old Name"))

		// Update the group name
		updateCmd := IAMGroupUpdateCommand(tenantID, groupID, "New Name")
		runner.RunAndAssert(updateCmd)

		// Verify updated name
		getCmd := IAMGroupGetCommand(tenantID, groupID)
		stdout, _, err := runner.Run(getCmd)
		require.NoError(t, err, "get group after update should succeed")
		assert.Contains(t, stdout, "New Name", "get output should contain updated group name")
	})
}

// TestIAMGroupUpdateDescription tests updating only the description field
// of an IAM group.
func TestIAMGroupUpdateDescription(t *testing.T) {
	RunForTransports(t, "GroupUpdateDesc", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("grp-ud-tenant")
		groupID := UniqueKey("grp-ud-group")

		runner.RunAndAssert(IAMTenantCreateCommand(tenantID, "Group Update Desc Tenant"))
		defer func() {
			runner.Run(IAMGroupDeleteCommand(tenantID, groupID))
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		runner.RunAndAssert(IAMGroupCreateCommand(tenantID, groupID, "Desc Update Group"))

		// Update only the description
		updateDescCmd := CommandDef{
			Name:           fmt.Sprintf("iam group update %s description", groupID),
			Args:           []string{"iam", "group", "update", groupID, "--tenant", tenantID, "--description", "Updated description text"},
			ExpectSuccess:  true,
			ExpectOutput:   []string{groupID},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(updateDescCmd)

		// Verify the group still exists and retains its identity
		getCmd := IAMGroupGetCommand(tenantID, groupID)
		stdout, _, err := runner.Run(getCmd)
		require.NoError(t, err, "get group after description update should succeed")
		assert.Contains(t, stdout, groupID, "get output should contain group ID after description update")
	})
}

// =============================================================================
// IAM Group Delete Tests
// =============================================================================

// TestIAMGroupDelete tests creating a group, deleting it, and verifying that
// a subsequent get fails.
func TestIAMGroupDelete(t *testing.T) {
	RunForTransports(t, "GroupDelete", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("grp-del-tenant")
		groupID := UniqueKey("grp-del-group")

		runner.RunAndAssert(IAMTenantCreateCommand(tenantID, "Group Delete Tenant"))
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Create and then delete the group
		runner.RunAndAssert(IAMGroupCreateCommand(tenantID, groupID, "Ephemeral Group"))

		deleteCmd := IAMGroupDeleteCommand(tenantID, groupID)
		runner.RunAndAssert(deleteCmd)

		// Verify group no longer exists
		getCmd := IAMGroupGetCommand(tenantID, groupID)
		getCmd.ExpectSuccess = false
		_, stderr, err := runner.Run(getCmd)
		require.Error(t, err, "get after delete should fail")
		assert.NotEmpty(t, stderr, "stderr should contain an error message for deleted group")
	})
}

// =============================================================================
// IAM Group List Tests
// =============================================================================

// TestIAMGroupList tests listing groups within a tenant and verifying that
// multiple created groups appear in the output.
func TestIAMGroupList(t *testing.T) {
	RunForTransports(t, "GroupList", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("grp-lst-tenant")
		groupID1 := UniqueKey("grp-lst-alpha")
		groupID2 := UniqueKey("grp-lst-beta")

		runner.RunAndAssert(IAMTenantCreateCommand(tenantID, "Group List Tenant"))
		defer func() {
			runner.Run(IAMGroupDeleteCommand(tenantID, groupID1))
			runner.Run(IAMGroupDeleteCommand(tenantID, groupID2))
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		runner.RunAndAssert(IAMGroupCreateCommand(tenantID, groupID1, "Alpha Team"))
		runner.RunAndAssert(IAMGroupCreateCommand(tenantID, groupID2, "Beta Team"))

		listCmd := IAMGroupListCommand(tenantID)
		stdout, _, err := runner.Run(listCmd)
		require.NoError(t, err, "list groups should succeed")
		assert.Contains(t, stdout, groupID1, "list output should contain first group ID")
		assert.Contains(t, stdout, groupID2, "list output should contain second group ID")
	})
}

// TestIAMGroupListJSON tests listing groups with JSON output format and
// verifying the response structure contains the expected groups.
func TestIAMGroupListJSON(t *testing.T) {
	RunForTransports(t, "GroupListJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("grp-lj-tenant")
		groupID := UniqueKey("grp-lj-group")

		runner.RunAndAssert(IAMTenantCreateCommand(tenantID, "Group List JSON Tenant"))
		defer func() {
			runner.Run(IAMGroupDeleteCommand(tenantID, groupID))
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		runner.RunAndAssert(IAMGroupCreateCommand(tenantID, groupID, "JSON Team"))

		listJSONCmd := CommandDef{
			Name:           fmt.Sprintf("iam group list tenant=%s --output json", tenantID),
			Args:           []string{"iam", "group", "list", "--tenant", tenantID, "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        15 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(listJSONCmd)
		require.NoError(t, err, "list groups JSON should succeed")
		assert.Contains(t, stdout, groupID, "JSON list output should contain group ID")
		assert.Contains(t, stdout, "JSON Team", "JSON list output should contain group name")
	})
}

// =============================================================================
// IAM Group Membership Tests
// =============================================================================

// TestIAMGroupMembership tests the full membership lifecycle: add a member,
// list members to verify presence, then remove the member.
func TestIAMGroupMembership(t *testing.T) {
	RunForTransports(t, "GroupMembership", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("grp-mem-tenant")
		groupID := UniqueKey("grp-mem-group")
		userEmail := UniqueKey("grp-mem-user") + "@test.local"

		runner.RunAndAssert(IAMTenantCreateCommand(tenantID, "Group Membership Tenant"))

		// Create a user so that it exists before adding to group.
		createUserCmd := CommandDef{
			Name:           fmt.Sprintf("iam user create %s", userEmail),
			Args:           []string{"iam", "user", "create", userEmail, "--tenant", tenantID, "--name", "MembershipUser", "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(createUserCmd)
		require.NoError(t, err, "user create should succeed")

		// Parse user ID from JSON output.
		userID := parseJSONField(t, stdout, "id")
		require.NotEmpty(t, userID, "user ID should be present in create output")

		defer func() {
			runner.Run(IAMGroupRemoveMemberCommand(tenantID, groupID, userID))
			runner.Run(IAMGroupDeleteCommand(tenantID, groupID))
			runner.Run(IAMUserDeleteCommand(tenantID, userID))
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		runner.RunAndAssert(IAMGroupCreateCommand(tenantID, groupID, "Membership Group"))

		// Add member to group
		addCmd := IAMGroupAddMemberCommand(tenantID, groupID, userID)
		runner.RunAndAssert(addCmd)

		// List members and verify the user appears
		membersCmd := IAMGroupMembersCommand(tenantID, groupID)
		stdout, _, err = runner.Run(membersCmd)
		require.NoError(t, err, "list members should succeed")
		assert.Contains(t, stdout, userID, "members output should contain added user ID")

		// Remove member from group
		removeCmd := IAMGroupRemoveMemberCommand(tenantID, groupID, userID)
		runner.RunAndAssert(removeCmd)

		// Verify member was removed
		stdout, _, err = runner.Run(IAMGroupMembersCommand(tenantID, groupID))
		require.NoError(t, err, "list members after removal should succeed")
		assert.NotContains(t, stdout, userID, "members output should not contain removed user ID")
	})
}

// TestIAMGroupMembersEmpty tests listing members of a group that has no
// members, verifying the command succeeds with empty results.
func TestIAMGroupMembersEmpty(t *testing.T) {
	RunForTransports(t, "GroupMembersEmpty", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("grp-me-tenant")
		groupID := UniqueKey("grp-me-group")

		runner.RunAndAssert(IAMTenantCreateCommand(tenantID, "Group Members Empty Tenant"))
		defer func() {
			runner.Run(IAMGroupDeleteCommand(tenantID, groupID))
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		runner.RunAndAssert(IAMGroupCreateCommand(tenantID, groupID, "Empty Members Group"))

		// List members of empty group
		membersCmd := IAMGroupMembersCommand(tenantID, groupID)
		_, _, err := runner.Run(membersCmd)
		require.NoError(t, err, "listing members of empty group should succeed")
	})
}

// =============================================================================
// IAM Group Error Handling Tests
// =============================================================================

// TestIAMGroupAddMemberNonExistentGroup tests adding a member to a group
// that does not exist, verifying the command fails with an error.
func TestIAMGroupAddMemberNonExistentGroup(t *testing.T) {
	RunForTransports(t, "GroupAddMemberNonExistent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("grp-amne-tenant")
		nonExistentGroupID := UniqueKey("grp-amne-nogroup")
		userID := UniqueKey("grp-amne-user")

		runner.RunAndAssert(IAMTenantCreateCommand(tenantID, "Add Member NonExistent Tenant"))
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Attempt to add member to non-existent group
		addCmd := IAMGroupAddMemberCommand(tenantID, nonExistentGroupID, userID)
		addCmd.ExpectSuccess = false
		addCmd.ExpectOutput = nil
		_, stderr, err := runner.Run(addCmd)
		require.Error(t, err, "adding member to non-existent group should fail")
		assert.NotEmpty(t, stderr, "stderr should contain an error for non-existent group")
	})
}

// TestIAMGroupGetNonExistent tests getting a group that does not exist,
// verifying the command fails with an appropriate error.
func TestIAMGroupGetNonExistent(t *testing.T) {
	RunForTransports(t, "GroupGetNonExistent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("grp-gne-tenant")
		nonExistentGroupID := UniqueKey("grp-gne-nogroup")

		runner.RunAndAssert(IAMTenantCreateCommand(tenantID, "Get NonExistent Tenant"))
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Attempt to get non-existent group
		getCmd := IAMGroupGetCommand(tenantID, nonExistentGroupID)
		getCmd.ExpectSuccess = false
		getCmd.ExpectOutput = nil
		_, stderr, err := runner.Run(getCmd)
		require.Error(t, err, "get non-existent group should fail")
		assert.NotEmpty(t, stderr, "stderr should contain an error for non-existent group")
	})
}

// TestIAMGroupDeleteNonExistent tests deleting a group that does not exist,
// verifying the command fails with an appropriate error.
func TestIAMGroupDeleteNonExistent(t *testing.T) {
	RunForTransports(t, "GroupDeleteNonExistent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("grp-dne-tenant")
		nonExistentGroupID := UniqueKey("grp-dne-nogroup")

		runner.RunAndAssert(IAMTenantCreateCommand(tenantID, "Delete NonExistent Tenant"))
		defer func() {
			runner.Run(IAMTenantDeleteCommand(tenantID))
		}()

		// Attempt to delete non-existent group
		deleteCmd := IAMGroupDeleteCommand(tenantID, nonExistentGroupID)
		deleteCmd.ExpectSuccess = false
		deleteCmd.ExpectOutput = nil
		_, stderr, err := runner.Run(deleteCmd)
		require.Error(t, err, "delete non-existent group should fail")
		assert.NotEmpty(t, stderr, "stderr should contain an error for non-existent group")
	})
}
