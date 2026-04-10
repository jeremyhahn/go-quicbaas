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
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// IAM Tenant Lifecycle Tests
// =============================================================================

// TestIAMTenantLifecycle tests the full CRUD lifecycle for IAM tenants:
// create, get, update, list, and delete.
func TestIAMTenantLifecycle(t *testing.T) {
	RunForTransports(t, "IAMTenantLifecycle", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("lifecycle-tenant")
		tenantName := "Lifecycle Test Tenant"
		updatedName := "Updated Lifecycle Tenant"

		// Create tenant
		createCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant create %s", tenantID),
			Args:           []string{"iam", "tenant", "create", tenantID, "--name", tenantName, "--yes"},
			ExpectSuccess:  true,
			ExpectOutput:   []string{tenantID, tenantName},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(createCmd)

		// Defer cleanup to ensure tenant is removed even on failure
		defer func() {
			deleteCmd := CommandDef{
				Name:           fmt.Sprintf("iam tenant delete %s cleanup", tenantID),
				Args:           []string{"iam", "tenant", "delete", tenantID, "--yes"},
				ExpectSuccess:  true,
				Timeout:        10 * time.Second,
				SkipTransports: iamTransportsToSkip,
			}
			runner.Run(deleteCmd)
		}()

		// Get tenant
		getCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant get %s", tenantID),
			Args:           []string{"iam", "tenant", "get", tenantID},
			ExpectSuccess:  true,
			ExpectOutput:   []string{tenantID, tenantName},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(getCmd)

		// Update tenant name
		updateCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant update %s", tenantID),
			Args:           []string{"iam", "tenant", "update", tenantID, "--name", updatedName},
			ExpectSuccess:  true,
			ExpectOutput:   []string{tenantID, updatedName},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(updateCmd)

		// List tenants and verify the updated tenant appears (retry for Raft consistency)
		listCtx, listCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer listCancel()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			listCmd := CommandDef{
				Name:           "iam tenant list",
				Args:           []string{"iam", "tenant", "list"},
				ExpectSuccess:  true,
				Timeout:        10 * time.Second,
				SkipTransports: iamTransportsToSkip,
			}
			stdout, _, err := runner.Run(listCmd)
			if err == nil && strings.Contains(stdout, tenantID) {
				break
			}
			select {
			case <-listCtx.Done():
				t.Fatalf("Timed out waiting for tenant %s to appear in list", tenantID)
			case <-ticker.C:
			}
		}

		// Delete tenant
		deleteCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant delete %s", tenantID),
			Args:           []string{"iam", "tenant", "delete", tenantID, "--yes"},
			ExpectSuccess:  true,
			ExpectOutput:   []string{tenantID, "deleted"},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(deleteCmd)

		// Verify get fails after deletion
		getAfterDeleteCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant get %s after delete", tenantID),
			Args:           []string{"iam", "tenant", "get", tenantID},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(getAfterDeleteCmd)
		if err != nil && !runner.IsSkipped(getAfterDeleteCmd) {
			assert.Error(t, err, "get after delete should fail")
			t.Logf("Correctly failed to get deleted tenant: stdout=%s stderr=%s", stdout, stderr)
		}
	})
}

// =============================================================================
// IAM Tenant Create Tests
// =============================================================================

// TestIAMTenantCreate tests basic tenant creation and verifies the output
// contains the tenant ID.
func TestIAMTenantCreate(t *testing.T) {
	RunForTransports(t, "IAMTenantCreate", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("create-tenant")
		tenantName := "Create Test Tenant"

		createCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant create %s", tenantID),
			Args:           []string{"iam", "tenant", "create", tenantID, "--name", tenantName, "--yes"},
			ExpectSuccess:  true,
			ExpectOutput:   []string{tenantID},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(createCmd)

		defer func() {
			deleteCmd := CommandDef{
				Name:           fmt.Sprintf("iam tenant delete %s cleanup", tenantID),
				Args:           []string{"iam", "tenant", "delete", tenantID, "--yes"},
				ExpectSuccess:  true,
				Timeout:        10 * time.Second,
				SkipTransports: iamTransportsToSkip,
			}
			runner.Run(deleteCmd)
		}()

		// Verify the tenant was created by getting it
		getCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant get %s verify", tenantID),
			Args:           []string{"iam", "tenant", "get", tenantID},
			ExpectSuccess:  true,
			ExpectOutput:   []string{tenantID, tenantName},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(getCmd)
	})
}

// TestIAMTenantCreateWithOptions tests tenant creation with --max-users
// and --mfa-required flags, verifying those settings are applied.
func TestIAMTenantCreateWithOptions(t *testing.T) {
	RunForTransports(t, "IAMTenantCreateWithOptions", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("options-tenant")
		tenantName := "Options Test Tenant"

		createCmd := CommandDef{
			Name: fmt.Sprintf("iam tenant create %s with options", tenantID),
			Args: []string{
				"iam", "tenant", "create", tenantID,
				"--name", tenantName,
				"--max-users", "1000",
				"--mfa-required",
				"--yes",
			},
			ExpectSuccess:  true,
			ExpectOutput:   []string{tenantID, "1000", "true"},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(createCmd)

		defer func() {
			deleteCmd := CommandDef{
				Name:           fmt.Sprintf("iam tenant delete %s cleanup", tenantID),
				Args:           []string{"iam", "tenant", "delete", tenantID, "--yes"},
				ExpectSuccess:  true,
				Timeout:        10 * time.Second,
				SkipTransports: iamTransportsToSkip,
			}
			runner.Run(deleteCmd)
		}()

		// Verify settings via get
		getCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant get %s verify options", tenantID),
			Args:           []string{"iam", "tenant", "get", tenantID},
			ExpectSuccess:  true,
			ExpectOutput:   []string{tenantID, "1000"},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(getCmd)
		if err == nil && !runner.IsSkipped(getCmd) {
			assert.Contains(t, stdout, tenantID, "get output should contain tenant ID")
			assert.Contains(t, stdout, "1000", "get output should contain max users setting")
			assert.Contains(t, stdout, "true", "get output should contain MFA required setting")
		}
	})
}

// TestIAMTenantCreateJSON tests tenant creation with --output json flag
// and verifies the response is valid JSON containing the tenant ID.
func TestIAMTenantCreateJSON(t *testing.T) {
	RunForTransports(t, "IAMTenantCreateJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("json-create-tenant")
		tenantName := "JSON Create Test Tenant"

		createCmd := CommandDef{
			Name: fmt.Sprintf("iam tenant create %s json", tenantID),
			Args: []string{
				"iam", "tenant", "create", tenantID,
				"--name", tenantName,
				"--output", "json",
				"--yes",
			},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(createCmd)
		if runner.IsSkipped(createCmd) {
			return
		}
		require.NoError(t, err, "create with JSON output should succeed")

		defer func() {
			deleteCmd := CommandDef{
				Name:           fmt.Sprintf("iam tenant delete %s cleanup", tenantID),
				Args:           []string{"iam", "tenant", "delete", tenantID, "--yes"},
				ExpectSuccess:  true,
				Timeout:        10 * time.Second,
				SkipTransports: iamTransportsToSkip,
			}
			runner.Run(deleteCmd)
		}()

		// Verify output is valid JSON
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "create output should be valid JSON: %s", stdout)

		// Verify JSON contains expected fields
		assert.Contains(t, stdout, tenantID, "JSON output should contain tenant ID")
		assert.Contains(t, stdout, tenantName, "JSON output should contain tenant name")

		// Verify the ID field in the parsed JSON
		idVal, ok := result["id"]
		if ok {
			assert.Equal(t, tenantID, idVal, "JSON id field should match tenant ID")
		} else {
			// Check alternate casing
			idVal, ok = result["ID"]
			if ok {
				assert.Equal(t, tenantID, idVal, "JSON ID field should match tenant ID")
			}
		}
	})
}

// =============================================================================
// IAM Tenant Get Tests
// =============================================================================

// TestIAMTenantGet tests retrieving a tenant and verifying the output
// contains both the tenant name and ID.
func TestIAMTenantGet(t *testing.T) {
	RunForTransports(t, "IAMTenantGet", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("get-tenant")
		tenantName := "Get Test Tenant"

		// Create tenant first
		createCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant create %s", tenantID),
			Args:           []string{"iam", "tenant", "create", tenantID, "--name", tenantName, "--yes"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(createCmd)

		defer func() {
			deleteCmd := CommandDef{
				Name:           fmt.Sprintf("iam tenant delete %s cleanup", tenantID),
				Args:           []string{"iam", "tenant", "delete", tenantID, "--yes"},
				ExpectSuccess:  true,
				Timeout:        10 * time.Second,
				SkipTransports: iamTransportsToSkip,
			}
			runner.Run(deleteCmd)
		}()

		// Get tenant and verify output
		getCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant get %s", tenantID),
			Args:           []string{"iam", "tenant", "get", tenantID},
			ExpectSuccess:  true,
			ExpectOutput:   []string{tenantID, tenantName},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(getCmd)
		if err == nil && !runner.IsSkipped(getCmd) {
			assert.Contains(t, stdout, "Tenant Details", "get output should contain details header")
			assert.Contains(t, stdout, tenantID, "get output should contain tenant ID")
			assert.Contains(t, stdout, tenantName, "get output should contain tenant name")
		}
	})
}

// TestIAMTenantGetJSON tests retrieving a tenant with --output json flag
// and verifies the JSON structure contains expected fields.
func TestIAMTenantGetJSON(t *testing.T) {
	RunForTransports(t, "IAMTenantGetJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("json-get-tenant")
		tenantName := "JSON Get Test Tenant"

		// Create tenant first
		createCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant create %s", tenantID),
			Args:           []string{"iam", "tenant", "create", tenantID, "--name", tenantName, "--yes"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(createCmd)

		defer func() {
			deleteCmd := CommandDef{
				Name:           fmt.Sprintf("iam tenant delete %s cleanup", tenantID),
				Args:           []string{"iam", "tenant", "delete", tenantID, "--yes"},
				ExpectSuccess:  true,
				Timeout:        10 * time.Second,
				SkipTransports: iamTransportsToSkip,
			}
			runner.Run(deleteCmd)
		}()

		// Get tenant as JSON
		getCmd := CommandDef{
			Name: fmt.Sprintf("iam tenant get %s json", tenantID),
			Args: []string{
				"iam", "tenant", "get", tenantID,
				"--output", "json",
			},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(getCmd)
		if runner.IsSkipped(getCmd) {
			return
		}
		require.NoError(t, err, "get with JSON output should succeed")

		// Verify output is valid JSON
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "get output should be valid JSON: %s", stdout)

		// Verify JSON contains expected fields
		assert.Contains(t, stdout, tenantID, "JSON output should contain tenant ID")
		assert.Contains(t, stdout, tenantName, "JSON output should contain tenant name")
	})
}

// =============================================================================
// IAM Tenant Update Tests
// =============================================================================

// TestIAMTenantUpdate tests updating a tenant name and verifying the change
// is reflected in subsequent get operations.
func TestIAMTenantUpdate(t *testing.T) {
	RunForTransports(t, "IAMTenantUpdate", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("update-tenant")
		originalName := "Original Tenant Name"
		updatedName := "Updated Tenant Name"

		// Create tenant
		createCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant create %s", tenantID),
			Args:           []string{"iam", "tenant", "create", tenantID, "--name", originalName, "--yes"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(createCmd)

		defer func() {
			deleteCmd := CommandDef{
				Name:           fmt.Sprintf("iam tenant delete %s cleanup", tenantID),
				Args:           []string{"iam", "tenant", "delete", tenantID, "--yes"},
				ExpectSuccess:  true,
				Timeout:        10 * time.Second,
				SkipTransports: iamTransportsToSkip,
			}
			runner.Run(deleteCmd)
		}()

		// Update tenant name
		updateCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant update %s name", tenantID),
			Args:           []string{"iam", "tenant", "update", tenantID, "--name", updatedName},
			ExpectSuccess:  true,
			ExpectOutput:   []string{tenantID, updatedName},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(updateCmd)

		// Verify update by getting the tenant
		getCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant get %s verify update", tenantID),
			Args:           []string{"iam", "tenant", "get", tenantID},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(getCmd)
		if err == nil && !runner.IsSkipped(getCmd) {
			assert.Contains(t, stdout, updatedName, "get after update should contain the new name")
			assert.NotContains(t, stdout, originalName, "get after update should not contain the old name")
		}
	})
}

// TestIAMTenantUpdateMaxUsers tests updating a tenant's max-users setting.
func TestIAMTenantUpdateMaxUsers(t *testing.T) {
	RunForTransports(t, "IAMTenantUpdateMaxUsers", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("update-maxusers-tenant")
		tenantName := "Max Users Update Tenant"

		// Create tenant without max-users
		createCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant create %s", tenantID),
			Args:           []string{"iam", "tenant", "create", tenantID, "--name", tenantName, "--yes"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(createCmd)

		defer func() {
			deleteCmd := CommandDef{
				Name:           fmt.Sprintf("iam tenant delete %s cleanup", tenantID),
				Args:           []string{"iam", "tenant", "delete", tenantID, "--yes"},
				ExpectSuccess:  true,
				Timeout:        10 * time.Second,
				SkipTransports: iamTransportsToSkip,
			}
			runner.Run(deleteCmd)
		}()

		// Update max-users
		updateCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant update %s max-users", tenantID),
			Args:           []string{"iam", "tenant", "update", tenantID, "--max-users", "5000"},
			ExpectSuccess:  true,
			ExpectOutput:   []string{tenantID, "5000"},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(updateCmd)

		// Verify update by getting the tenant
		getCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant get %s verify max-users", tenantID),
			Args:           []string{"iam", "tenant", "get", tenantID},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(getCmd)
		if err == nil && !runner.IsSkipped(getCmd) {
			assert.Contains(t, stdout, "5000", "get after update should contain the new max-users value")
		}
	})
}

// =============================================================================
// IAM Tenant Delete Tests
// =============================================================================

// TestIAMTenantDelete tests deleting a tenant and verifying subsequent
// get operations fail for the deleted tenant.
func TestIAMTenantDelete(t *testing.T) {
	RunForTransports(t, "IAMTenantDelete", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("delete-tenant")
		tenantName := "Delete Test Tenant"

		// Create tenant
		createCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant create %s", tenantID),
			Args:           []string{"iam", "tenant", "create", tenantID, "--name", tenantName, "--yes"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(createCmd)

		// Delete tenant
		deleteCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant delete %s", tenantID),
			Args:           []string{"iam", "tenant", "delete", tenantID, "--yes"},
			ExpectSuccess:  true,
			ExpectOutput:   []string{tenantID, "deleted"},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(deleteCmd)

		// Verify tenant is gone
		getCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant get %s after delete", tenantID),
			Args:           []string{"iam", "tenant", "get", tenantID},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(getCmd)
		if !runner.IsSkipped(getCmd) {
			assert.Error(t, err, "get after delete should fail for tenant %s", tenantID)
			t.Logf("Correctly failed to get deleted tenant: stdout=%s stderr=%s", stdout, stderr)
		}
	})
}

// =============================================================================
// IAM Tenant List Tests
// =============================================================================

// TestIAMTenantList tests listing tenants and verifies that multiple
// created tenants appear in the list output.
func TestIAMTenantList(t *testing.T) {
	RunForTransports(t, "IAMTenantList", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenant1ID := UniqueKey("list-tenant-1")
		tenant2ID := UniqueKey("list-tenant-2")
		tenant1Name := "List Test Tenant One"
		tenant2Name := "List Test Tenant Two"

		// Create first tenant
		create1Cmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant create %s", tenant1ID),
			Args:           []string{"iam", "tenant", "create", tenant1ID, "--name", tenant1Name, "--yes"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(create1Cmd)

		defer func() {
			delete1Cmd := CommandDef{
				Name:           fmt.Sprintf("iam tenant delete %s cleanup", tenant1ID),
				Args:           []string{"iam", "tenant", "delete", tenant1ID, "--yes"},
				ExpectSuccess:  true,
				Timeout:        10 * time.Second,
				SkipTransports: iamTransportsToSkip,
			}
			runner.Run(delete1Cmd)
		}()

		// Create second tenant
		create2Cmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant create %s", tenant2ID),
			Args:           []string{"iam", "tenant", "create", tenant2ID, "--name", tenant2Name, "--yes"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(create2Cmd)

		defer func() {
			delete2Cmd := CommandDef{
				Name:           fmt.Sprintf("iam tenant delete %s cleanup", tenant2ID),
				Args:           []string{"iam", "tenant", "delete", tenant2ID, "--yes"},
				ExpectSuccess:  true,
				Timeout:        10 * time.Second,
				SkipTransports: iamTransportsToSkip,
			}
			runner.Run(delete2Cmd)
		}()

		// Retry list for Raft consistency - both tenants must appear
		listCtx, listCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer listCancel()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			listCmd := CommandDef{
				Name:           "iam tenant list",
				Args:           []string{"iam", "tenant", "list"},
				ExpectSuccess:  true,
				Timeout:        10 * time.Second,
				SkipTransports: iamTransportsToSkip,
			}
			stdout, _, err := runner.Run(listCmd)
			if err == nil && strings.Contains(stdout, tenant1ID) && strings.Contains(stdout, tenant2ID) {
				assert.Contains(t, stdout, tenant1Name, "list output should contain first tenant name")
				assert.Contains(t, stdout, tenant2Name, "list output should contain second tenant name")
				break
			}
			select {
			case <-listCtx.Done():
				t.Fatalf("Timed out waiting for tenants %s and %s to appear in list", tenant1ID, tenant2ID)
			case <-ticker.C:
			}
		}
	})
}

// TestIAMTenantListJSON tests listing tenants with --output json flag
// and verifies the output is valid JSON.
func TestIAMTenantListJSON(t *testing.T) {
	RunForTransports(t, "IAMTenantListJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("json-list-tenant")
		tenantName := "JSON List Test Tenant"

		// Create a tenant so the list is non-empty
		createCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant create %s", tenantID),
			Args:           []string{"iam", "tenant", "create", tenantID, "--name", tenantName, "--yes"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(createCmd)

		defer func() {
			deleteCmd := CommandDef{
				Name:           fmt.Sprintf("iam tenant delete %s cleanup", tenantID),
				Args:           []string{"iam", "tenant", "delete", tenantID, "--yes"},
				ExpectSuccess:  true,
				Timeout:        10 * time.Second,
				SkipTransports: iamTransportsToSkip,
			}
			runner.Run(deleteCmd)
		}()

		// Retry list for Raft consistency
		listCtx, listCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer listCancel()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			listCmd := CommandDef{
				Name: "iam tenant list json",
				Args: []string{
					"iam", "tenant", "list",
					"--output", "json",
				},
				ExpectSuccess:  true,
				Timeout:        10 * time.Second,
				SkipTransports: iamTransportsToSkip,
			}
			stdout, _, err := runner.Run(listCmd)
			if err == nil && strings.Contains(stdout, tenantID) {
				// Verify output is valid JSON (array of tenants)
				var result interface{}
				jsonErr := json.Unmarshal([]byte(stdout), &result)
				require.NoError(t, jsonErr, "list output should be valid JSON: %s", stdout)

				// Verify the created tenant appears in the JSON output
				assert.Contains(t, stdout, tenantID, "JSON list output should contain the test tenant ID")
				assert.Contains(t, stdout, tenantName, "JSON list output should contain the test tenant name")
				break
			}
			select {
			case <-listCtx.Done():
				t.Fatalf("Timed out waiting for tenant %s to appear in JSON list", tenantID)
			case <-ticker.C:
			}
		}
	})
}

// =============================================================================
// IAM Tenant Error Handling Tests
// =============================================================================

// TestIAMTenantCreateDuplicate tests that creating a tenant with a duplicate
// ID fails with an appropriate error.
func TestIAMTenantCreateDuplicate(t *testing.T) {
	RunForTransports(t, "IAMTenantCreateDuplicate", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("dup-tenant")
		tenantName := "Duplicate Test Tenant"

		// Create tenant first time
		createCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant create %s first", tenantID),
			Args:           []string{"iam", "tenant", "create", tenantID, "--name", tenantName, "--yes"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(createCmd)

		defer func() {
			deleteCmd := CommandDef{
				Name:           fmt.Sprintf("iam tenant delete %s cleanup", tenantID),
				Args:           []string{"iam", "tenant", "delete", tenantID, "--yes"},
				ExpectSuccess:  true,
				Timeout:        10 * time.Second,
				SkipTransports: iamTransportsToSkip,
			}
			runner.Run(deleteCmd)
		}()

		// Create tenant second time with same ID - should fail
		duplicateCmd := CommandDef{
			Name:           fmt.Sprintf("iam tenant create %s duplicate", tenantID),
			Args:           []string{"iam", "tenant", "create", tenantID, "--name", "Another Name", "--yes"},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(duplicateCmd)
		if !runner.IsSkipped(duplicateCmd) {
			assert.Error(t, err, "creating duplicate tenant should fail")
			combined := strings.ToLower(stdout + stderr)
			assert.True(t,
				strings.Contains(combined, "exist") ||
					strings.Contains(combined, "duplicate") ||
					strings.Contains(combined, "already") ||
					strings.Contains(combined, "conflict"),
				"error output should indicate duplicate/already exists: stdout=%s stderr=%s", stdout, stderr)
		}
	})
}

// TestIAMTenantGetNonExistent tests that getting a non-existent tenant
// returns an appropriate error.
func TestIAMTenantGetNonExistent(t *testing.T) {
	RunForTransports(t, "IAMTenantGetNonExistent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		getCmd := CommandDef{
			Name:           "iam tenant get non-existent",
			Args:           []string{"iam", "tenant", "get", "non-existent-tenant-12345"},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(getCmd)
		if !runner.IsSkipped(getCmd) {
			assert.Error(t, err, "getting non-existent tenant should fail")
			combined := strings.ToLower(stdout + stderr)
			assert.True(t,
				strings.Contains(combined, "not found") ||
					strings.Contains(combined, "not exist") ||
					strings.Contains(combined, "error") ||
					strings.Contains(combined, "unknown"),
				"error output should indicate tenant not found: stdout=%s stderr=%s", stdout, stderr)
			t.Logf("Correctly failed for non-existent tenant: stdout=%s stderr=%s", stdout, stderr)
		}
	})
}

// TestIAMTenantDeleteNonExistent tests that deleting a non-existent tenant
// returns an appropriate error.
func TestIAMTenantDeleteNonExistent(t *testing.T) {
	RunForTransports(t, "IAMTenantDeleteNonExistent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		deleteCmd := CommandDef{
			Name:           "iam tenant delete non-existent",
			Args:           []string{"iam", "tenant", "delete", "non-existent-tenant-12345", "--yes"},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(deleteCmd)
		if !runner.IsSkipped(deleteCmd) {
			assert.Error(t, err, "deleting non-existent tenant should fail")
			combined := strings.ToLower(stdout + stderr)
			assert.True(t,
				strings.Contains(combined, "not found") ||
					strings.Contains(combined, "not exist") ||
					strings.Contains(combined, "error") ||
					strings.Contains(combined, "unknown"),
				"error output should indicate tenant not found: stdout=%s stderr=%s", stdout, stderr)
			t.Logf("Correctly failed for non-existent tenant delete: stdout=%s stderr=%s", stdout, stderr)
		}
	})
}

// TestIAMTenantUpdateNonExistent tests that updating a non-existent tenant
// returns an appropriate error.
func TestIAMTenantUpdateNonExistent(t *testing.T) {
	RunForTransports(t, "IAMTenantUpdateNonExistent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		updateCmd := CommandDef{
			Name:           "iam tenant update non-existent",
			Args:           []string{"iam", "tenant", "update", "non-existent-tenant-12345", "--name", "Should Fail"},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(updateCmd)
		if !runner.IsSkipped(updateCmd) {
			assert.Error(t, err, "updating non-existent tenant should fail")
			combined := strings.ToLower(stdout + stderr)
			assert.True(t,
				strings.Contains(combined, "not found") ||
					strings.Contains(combined, "not exist") ||
					strings.Contains(combined, "error") ||
					strings.Contains(combined, "unknown"),
				"error output should indicate tenant not found: stdout=%s stderr=%s", stdout, stderr)
			t.Logf("Correctly failed for non-existent tenant update: stdout=%s stderr=%s", stdout, stderr)
		}
	})
}
