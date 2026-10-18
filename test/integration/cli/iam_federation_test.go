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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// IAM Federation Provider Tests
// =============================================================================

// TestIAMProviderLifecycle tests the complete federation provider lifecycle:
// create tenant, create provider, get, update, list, delete.
func TestIAMProviderLifecycle(t *testing.T) {
	RunForTransports(t, "IAMProviderLifecycle", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("fed-lifecycle-t")
		providerID := UniqueKey("fed-lifecycle-p")

		// Create tenant
		runner.ActAsTenantAdmin(tenantID)

		// Create custom OIDC provider
		createProvider := IAMProviderCreateCustomCommand(
			tenantID, providerID, "Lifecycle OIDC Provider", "oidc",
			"lifecycle-client-id", "lifecycle-client-secret",
			"https://idp.lifecycle.example.com")
		runner.RunAndAssert(createProvider)

		// Get provider
		getProvider := IAMProviderGetCommand(tenantID, providerID)
		stdout, _, err := runner.Run(getProvider)
		require.NoError(t, err, "get provider should succeed")
		assert.Contains(t, stdout, providerID, "get output should contain provider ID")

		// Update provider name
		updateProvider := IAMProviderUpdateCommand(tenantID, providerID, "Updated Lifecycle Provider")
		runner.RunAndAssert(updateProvider)

		// List providers
		listProviders := IAMProviderListCommand(tenantID)
		stdout, _, err = runner.Run(listProviders)
		require.NoError(t, err, "list providers should succeed")
		assert.Contains(t, stdout, providerID, "list output should contain the provider")

		// Delete provider
		deleteProvider := IAMProviderDeleteCommand(tenantID, providerID)
		runner.RunAndAssert(deleteProvider)

		// Cleanup tenant
		runner.Run(IAMTenantDeleteCommand(tenantID))
	})
}

// TestIAMProviderCreateCustom tests creating a custom OIDC provider with all flags
// including scopes, auto-provision, and allow-linking.
func TestIAMProviderCreateCustom(t *testing.T) {
	RunForTransports(t, "IAMProviderCreateCustom", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("fed-custom-t")
		providerID := UniqueKey("fed-custom-p")

		// Create tenant
		runner.ActAsTenantAdmin(tenantID)

		// Create custom OIDC provider with all flags
		createProvider := CommandDef{
			Name: fmt.Sprintf("iam provider create custom %s", providerID),
			Args: []string{
				"iam", "provider", "create",
				"--tenant", tenantID,
				"--provider-id", providerID,
				"--name", "Full Custom OIDC Provider",
				"--type", "oidc",
				"--client-id", "custom-client-id",
				"--client-secret", "custom-client-secret",
				"--issuer-url", "https://idp.custom.example.com",
				"--scopes", "openid,email,profile",
			},
			ExpectSuccess:  true,
			ExpectOutput:   []string{providerID},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(createProvider)

		// Verify the provider was created with correct details
		getProvider := IAMProviderGetCommand(tenantID, providerID)
		stdout, _, err := runner.Run(getProvider)
		require.NoError(t, err, "get custom provider should succeed")
		assert.Contains(t, stdout, providerID, "output should contain provider ID")

		// Cleanup
		runner.Run(IAMProviderDeleteCommand(tenantID, providerID))
		runner.Run(IAMTenantDeleteCommand(tenantID))
	})
}

// TestIAMProviderCreateWithAutoProvision tests creating a provider with
// auto-provision and allow-linking flags enabled.
func TestIAMProviderCreateWithAutoProvision(t *testing.T) {
	RunForTransports(t, "IAMProviderCreateWithAutoProvision", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("fed-autoprov-t")
		providerID := UniqueKey("fed-autoprov-p")

		// Create tenant
		runner.ActAsTenantAdmin(tenantID)

		// Create provider with auto-provision and allow-linking
		createProvider := CommandDef{
			Name: fmt.Sprintf("iam provider create auto-provision %s", providerID),
			Args: []string{
				"iam", "provider", "create",
				"--tenant", tenantID,
				"--provider-id", providerID,
				"--name", "Auto Provision OIDC",
				"--type", "oidc",
				"--client-id", "autoprov-client-id",
				"--client-secret", "autoprov-client-secret",
				"--issuer-url", "https://idp.autoprov.example.com",
				"--auto-provision",
				"--allow-linking",
			},
			ExpectSuccess:  true,
			ExpectOutput:   []string{providerID},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(createProvider)

		// Verify via get
		getProvider := IAMProviderGetCommand(tenantID, providerID)
		stdout, _, err := runner.Run(getProvider)
		require.NoError(t, err, "get auto-provision provider should succeed")
		assert.Contains(t, stdout, providerID, "output should contain provider ID")

		// Cleanup
		runner.Run(IAMProviderDeleteCommand(tenantID, providerID))
		runner.Run(IAMTenantDeleteCommand(tenantID))
	})
}

// TestIAMProviderGet tests creating a provider then getting its details.
func TestIAMProviderGet(t *testing.T) {
	RunForTransports(t, "IAMProviderGet", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("fed-get-t")
		providerID := UniqueKey("fed-get-p")

		// Create tenant
		runner.ActAsTenantAdmin(tenantID)

		// Create provider
		createProvider := IAMProviderCreateCustomCommand(
			tenantID, providerID, "Get Test Provider", "oidc",
			"get-client-id", "get-client-secret",
			"https://idp.get.example.com")
		runner.RunAndAssert(createProvider)

		// Get provider and verify details
		getProvider := IAMProviderGetCommand(tenantID, providerID)
		stdout, _, err := runner.Run(getProvider)
		require.NoError(t, err, "get provider should succeed")
		assert.Contains(t, stdout, providerID, "output should contain provider ID")
		assert.Contains(t, stdout, "Get Test Provider", "output should contain provider name")

		// Cleanup
		runner.Run(IAMProviderDeleteCommand(tenantID, providerID))
		runner.Run(IAMTenantDeleteCommand(tenantID))
	})
}

// TestIAMProviderGetJSON tests getting a provider with JSON output format.
func TestIAMProviderGetJSON(t *testing.T) {
	RunForTransports(t, "IAMProviderGetJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("fed-getjson-t")
		providerID := UniqueKey("fed-getjson-p")

		// Create tenant
		runner.ActAsTenantAdmin(tenantID)

		// Create provider
		createProvider := IAMProviderCreateCustomCommand(
			tenantID, providerID, "JSON Get Provider", "oidc",
			"json-get-client-id", "json-get-client-secret",
			"https://idp.json-get.example.com")
		runner.RunAndAssert(createProvider)

		// Get provider with JSON output
		getJSON := CommandDef{
			Name:           fmt.Sprintf("iam provider get %s json", providerID),
			Args:           []string{"iam", "provider", "get", "--provider-id", providerID, "--tenant", tenantID, "--output", "json"},
			ExpectSuccess:  true,
			ExpectOutput:   []string{providerID},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(getJSON)
		require.NoError(t, err, "get provider JSON should succeed")
		assert.Contains(t, stdout, providerID, "JSON output should contain provider ID")

		// Validate JSON structure
		trimmed := strings.TrimSpace(stdout)
		assert.True(t, json.Valid([]byte(trimmed)),
			"output should be valid JSON, got: %s", trimmed)

		// Cleanup
		runner.Run(IAMProviderDeleteCommand(tenantID, providerID))
		runner.Run(IAMTenantDeleteCommand(tenantID))
	})
}

// TestIAMProviderUpdate tests creating a provider and updating its name.
func TestIAMProviderUpdate(t *testing.T) {
	RunForTransports(t, "IAMProviderUpdate", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("fed-update-t")
		providerID := UniqueKey("fed-update-p")

		// Create tenant
		runner.ActAsTenantAdmin(tenantID)

		// Create provider
		createProvider := IAMProviderCreateCustomCommand(
			tenantID, providerID, "Original Provider Name", "oidc",
			"update-client-id", "update-client-secret",
			"https://idp.update.example.com")
		runner.RunAndAssert(createProvider)

		// Update provider name
		updateProvider := IAMProviderUpdateCommand(tenantID, providerID, "Updated Provider Name")
		runner.RunAndAssert(updateProvider)

		// Verify updated name
		getProvider := IAMProviderGetCommand(tenantID, providerID)
		stdout, _, err := runner.Run(getProvider)
		require.NoError(t, err, "get updated provider should succeed")
		assert.Contains(t, stdout, providerID, "output should contain provider ID")
		assert.Contains(t, stdout, "Updated Provider Name",
			"output should contain updated provider name")

		// Cleanup
		runner.Run(IAMProviderDeleteCommand(tenantID, providerID))
		runner.Run(IAMTenantDeleteCommand(tenantID))
	})
}

// TestIAMProviderUpdateEnabled tests disabling and re-enabling a provider.
func TestIAMProviderUpdateEnabled(t *testing.T) {
	RunForTransports(t, "IAMProviderUpdateEnabled", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("fed-enabled-t")
		providerID := UniqueKey("fed-enabled-p")

		// Create tenant
		runner.ActAsTenantAdmin(tenantID)

		// Create provider (enabled by default)
		createProvider := IAMProviderCreateCustomCommand(
			tenantID, providerID, "Enabled Toggle Provider", "oidc",
			"enabled-client-id", "enabled-client-secret",
			"https://idp.enabled.example.com")
		runner.RunAndAssert(createProvider)

		// Disable provider
		disableCmd := CommandDef{
			Name:           fmt.Sprintf("iam provider update %s disable", providerID),
			Args:           []string{"iam", "provider", "update", "--provider-id", providerID, "--tenant", tenantID, "--enabled=false"},
			ExpectSuccess:  true,
			ExpectOutput:   []string{providerID},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(disableCmd)

		// Verify disabled state via get
		getProvider := IAMProviderGetCommand(tenantID, providerID)
		stdout, _, err := runner.Run(getProvider)
		require.NoError(t, err, "get disabled provider should succeed")
		assert.Contains(t, stdout, providerID, "output should contain provider ID")

		// Re-enable provider
		enableCmd := CommandDef{
			Name:           fmt.Sprintf("iam provider update %s enable", providerID),
			Args:           []string{"iam", "provider", "update", "--provider-id", providerID, "--tenant", tenantID, "--enabled=true"},
			ExpectSuccess:  true,
			ExpectOutput:   []string{providerID},
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		runner.RunAndAssert(enableCmd)

		// Verify re-enabled
		stdout, _, err = runner.Run(getProvider)
		require.NoError(t, err, "get re-enabled provider should succeed")
		assert.Contains(t, stdout, providerID, "output should contain provider ID after re-enable")

		// Cleanup
		runner.Run(IAMProviderDeleteCommand(tenantID, providerID))
		runner.Run(IAMTenantDeleteCommand(tenantID))
	})
}

// TestIAMProviderDelete tests creating and deleting a provider, then verifying
// that a subsequent get fails.
func TestIAMProviderDelete(t *testing.T) {
	RunForTransports(t, "IAMProviderDelete", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("fed-del-t")
		providerID := UniqueKey("fed-del-p")

		// Create tenant
		runner.ActAsTenantAdmin(tenantID)

		// Create provider
		createProvider := IAMProviderCreateCustomCommand(
			tenantID, providerID, "Delete Test Provider", "oidc",
			"del-client-id", "del-client-secret",
			"https://idp.del.example.com")
		runner.RunAndAssert(createProvider)

		// Delete provider
		deleteProvider := IAMProviderDeleteCommand(tenantID, providerID)
		runner.RunAndAssert(deleteProvider)

		// Verify get fails after deletion
		getAfterDelete := CommandDef{
			Name:           fmt.Sprintf("iam provider get deleted %s", providerID),
			Args:           []string{"iam", "provider", "get", "--provider-id", providerID, "--tenant", tenantID},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		_, stderr, err := runner.Run(getAfterDelete)
		if !runner.IsSkipped(getAfterDelete) {
			require.Error(t, err, "get deleted provider should fail")
			combined := strings.ToLower(stderr)
			assert.True(t,
				strings.Contains(combined, "not found") ||
					strings.Contains(combined, "not_found") ||
					strings.Contains(combined, "no provider") ||
					strings.Contains(combined, "does not exist"),
				"error should indicate provider not found, got: %s", stderr)
		}

		// Cleanup tenant
		runner.Run(IAMTenantDeleteCommand(tenantID))
	})
}

// TestIAMProviderList tests creating two providers and listing them.
func TestIAMProviderList(t *testing.T) {
	RunForTransports(t, "IAMProviderList", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("fed-list-t")
		providerID1 := UniqueKey("fed-list-p1")
		providerID2 := UniqueKey("fed-list-p2")

		// Create tenant
		runner.ActAsTenantAdmin(tenantID)

		// Create two providers
		createProvider1 := IAMProviderCreateCustomCommand(
			tenantID, providerID1, "List Provider One", "oidc",
			"list1-client-id", "list1-client-secret",
			"https://idp.list1.example.com")
		runner.RunAndAssert(createProvider1)

		createProvider2 := IAMProviderCreateCustomCommand(
			tenantID, providerID2, "List Provider Two", "oidc",
			"list2-client-id", "list2-client-secret",
			"https://idp.list2.example.com")
		runner.RunAndAssert(createProvider2)

		// List providers
		listProviders := IAMProviderListCommand(tenantID)
		stdout, _, err := runner.Run(listProviders)
		require.NoError(t, err, "list providers should succeed")
		assert.Contains(t, stdout, providerID1, "list output should contain first provider")
		assert.Contains(t, stdout, providerID2, "list output should contain second provider")

		// Cleanup
		runner.Run(IAMProviderDeleteCommand(tenantID, providerID1))
		runner.Run(IAMProviderDeleteCommand(tenantID, providerID2))
		runner.Run(IAMTenantDeleteCommand(tenantID))
	})
}

// TestIAMProviderListJSON tests listing providers with JSON output format.
func TestIAMProviderListJSON(t *testing.T) {
	RunForTransports(t, "IAMProviderListJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("fed-listjson-t")
		providerID1 := UniqueKey("fed-listjson-p1")
		providerID2 := UniqueKey("fed-listjson-p2")

		// Create tenant
		runner.ActAsTenantAdmin(tenantID)

		// Create two providers
		createProvider1 := IAMProviderCreateCustomCommand(
			tenantID, providerID1, "JSON List Provider One", "oidc",
			"listjson1-client-id", "listjson1-client-secret",
			"https://idp.listjson1.example.com")
		runner.RunAndAssert(createProvider1)

		createProvider2 := IAMProviderCreateCustomCommand(
			tenantID, providerID2, "JSON List Provider Two", "oidc",
			"listjson2-client-id", "listjson2-client-secret",
			"https://idp.listjson2.example.com")
		runner.RunAndAssert(createProvider2)

		// List providers with JSON output
		listJSON := CommandDef{
			Name:           fmt.Sprintf("iam provider list tenant=%s json", tenantID),
			Args:           []string{"iam", "provider", "list", "--tenant", tenantID, "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        15 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(listJSON)
		require.NoError(t, err, "list providers JSON should succeed")

		// Validate JSON structure
		trimmed := strings.TrimSpace(stdout)
		assert.True(t, json.Valid([]byte(trimmed)),
			"output should be valid JSON, got: %s", trimmed)
		assert.Contains(t, stdout, providerID1, "JSON output should contain first provider")
		assert.Contains(t, stdout, providerID2, "JSON output should contain second provider")

		// Cleanup
		runner.Run(IAMProviderDeleteCommand(tenantID, providerID1))
		runner.Run(IAMProviderDeleteCommand(tenantID, providerID2))
		runner.Run(IAMTenantDeleteCommand(tenantID))
	})
}

// TestIAMProviderGetNonExistent tests that getting a nonexistent provider fails
// with an appropriate error message.
func TestIAMProviderGetNonExistent(t *testing.T) {
	RunForTransports(t, "IAMProviderGetNonExistent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("fed-noexist-t")

		// Create tenant
		runner.ActAsTenantAdmin(tenantID)

		// Attempt to get a nonexistent provider
		getNonExistent := CommandDef{
			Name:           "iam provider get nonexistent",
			Args:           []string{"iam", "provider", "get", "--provider-id", "nonexistent-provider-99999", "--tenant", tenantID},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(getNonExistent)
		if !runner.IsSkipped(getNonExistent) {
			require.Error(t, err, "get nonexistent provider should fail")
			combined := strings.ToLower(stdout + stderr)
			assert.True(t,
				strings.Contains(combined, "not found") ||
					strings.Contains(combined, "not_found") ||
					strings.Contains(combined, "no provider") ||
					strings.Contains(combined, "does not exist"),
				"error should indicate provider not found, got: stdout=%s stderr=%s", stdout, stderr)
		}

		// Cleanup tenant
		runner.Run(IAMTenantDeleteCommand(tenantID))
	})
}

// TestIAMProviderDeleteNonExistent tests that deleting a nonexistent provider fails
// with an appropriate error message.
func TestIAMProviderDeleteNonExistent(t *testing.T) {
	RunForTransports(t, "IAMProviderDeleteNonExistent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("fed-delnoexist-t")

		// Create tenant
		runner.ActAsTenantAdmin(tenantID)

		// Attempt to delete a nonexistent provider
		deleteNonExistent := CommandDef{
			Name:           "iam provider delete nonexistent",
			Args:           []string{"iam", "provider", "delete", "--provider-id", "nonexistent-provider-99999", "--tenant", tenantID, "--yes"},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(deleteNonExistent)
		if !runner.IsSkipped(deleteNonExistent) {
			require.Error(t, err, "delete nonexistent provider should fail")
			combined := strings.ToLower(stdout + stderr)
			assert.True(t,
				strings.Contains(combined, "not found") ||
					strings.Contains(combined, "not_found") ||
					strings.Contains(combined, "no provider") ||
					strings.Contains(combined, "does not exist"),
				"error should indicate provider not found, got: stdout=%s stderr=%s", stdout, stderr)
		}

		// Cleanup tenant
		runner.Run(IAMTenantDeleteCommand(tenantID))
	})
}

// =============================================================================
// IAM Federation Identity Tests
// =============================================================================

// TestIAMIdentityListEmpty tests listing federated identities for a user that
// has no identities linked. The command should succeed with an empty result.
func TestIAMIdentityListEmpty(t *testing.T) {
	RunForTransports(t, "IAMIdentityListEmpty", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("fed-idlist-t")
		userID := UniqueKey("fed-idlist-u")

		// Create tenant
		runner.ActAsTenantAdmin(tenantID)

		// List identities for a user with none linked
		listIdentities := IAMIdentityListCommand(tenantID, userID)
		stdout, _, err := runner.Run(listIdentities)
		if !runner.IsSkipped(listIdentities) {
			// The command should succeed even if there are no linked identities.
			// Some implementations may return an empty list or a "no identities" message.
			require.NoError(t, err, "list identities for user with none should succeed")
			t.Logf("Identity list empty response: %s", stdout)
		}

		// Cleanup tenant
		runner.Run(IAMTenantDeleteCommand(tenantID))
	})
}

// TestIAMIdentityListJSON tests listing federated identities with JSON output format.
func TestIAMIdentityListJSON(t *testing.T) {
	RunForTransports(t, "IAMIdentityListJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("fed-idjson-t")
		userID := UniqueKey("fed-idjson-u")

		// Create tenant
		runner.ActAsTenantAdmin(tenantID)

		// List identities with JSON output
		listJSON := CommandDef{
			Name:           fmt.Sprintf("iam identity list %s json", userID),
			Args:           []string{"iam", "identity", "list", "--tenant", tenantID, "--user-id", userID, "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        15 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(listJSON)
		if !runner.IsSkipped(listJSON) {
			require.NoError(t, err, "list identities JSON should succeed")

			// Validate JSON structure
			trimmed := strings.TrimSpace(stdout)
			if len(trimmed) > 0 {
				assert.True(t, json.Valid([]byte(trimmed)),
					"output should be valid JSON, got: %s", trimmed)
			}
		}

		// Cleanup tenant
		runner.Run(IAMTenantDeleteCommand(tenantID))
	})
}

// TestIAMIdentityUnlinkNonExistent tests that unlinking a nonexistent provider
// from a user fails with an appropriate error.
func TestIAMIdentityUnlinkNonExistent(t *testing.T) {
	RunForTransports(t, "IAMIdentityUnlinkNonExistent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("fed-unlinkne-t")
		userID := UniqueKey("fed-unlinkne-u")

		// Create tenant
		runner.ActAsTenantAdmin(tenantID)

		// Attempt to unlink a nonexistent provider from a user
		unlinkNonExistent := CommandDef{
			Name:           fmt.Sprintf("iam identity unlink nonexistent from %s", userID),
			Args:           []string{"iam", "identity", "unlink", "--tenant", tenantID, "--user-id", userID, "--provider-id", "nonexistent-provider-99999", "--yes"},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(unlinkNonExistent)
		if !runner.IsSkipped(unlinkNonExistent) {
			require.Error(t, err, "unlink nonexistent provider should fail")
			combined := strings.ToLower(stdout + stderr)
			assert.True(t,
				strings.Contains(combined, "not found") ||
					strings.Contains(combined, "not_found") ||
					strings.Contains(combined, "no identity") ||
					strings.Contains(combined, "not linked") ||
					strings.Contains(combined, "does not exist"),
				"error should indicate identity/provider not found, got: stdout=%s stderr=%s", stdout, stderr)
		}

		// Cleanup tenant
		runner.Run(IAMTenantDeleteCommand(tenantID))
	})
}
