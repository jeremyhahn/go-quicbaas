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
// Helper Functions
// =============================================================================

// createTestTenant provisions a tenant for use in IAM app tests, makes the
// runner act as its admin, and returns the tenant ID.
// The caller is responsible for cleaning up the tenant after the test.
func createTestTenant(t *testing.T, runner *TestRunner, prefix string) string {
	t.Helper()
	tenantID := UniqueKey(prefix)
	runner.ActAsTenantAdmin(tenantID)
	return tenantID
}

// deleteTestTenant deletes a tenant (best-effort cleanup).
func deleteTestTenant(t *testing.T, runner *TestRunner, tenantID string) {
	t.Helper()
	cmd := IAMTenantDeleteCommand(tenantID)
	_, _, _ = runner.Run(cmd)
}

// registerAppJSON registers an app using --output json and returns the parsed JSON map.
func registerAppJSON(t *testing.T, runner *TestRunner, tenantID, appName string) map[string]interface{} {
	t.Helper()
	cmd := CommandDef{
		Name:           "iam app register json",
		Args:           []string{"iam", "app", "register", "--name", appName, "--tenant", tenantID, "--redirect-uri", "http://localhost:8080/callback", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
	stdout, stderr, err := runner.Run(cmd)
	require.NoError(t, err, "failed to register app %s: stdout=%s stderr=%s", appName, stdout, stderr)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result),
		"register output should be valid JSON: %s", stdout)
	return result
}

// extractClientID extracts the client_id from a JSON response map.
func extractClientID(t *testing.T, result map[string]interface{}) string {
	t.Helper()
	clientID, ok := result["client_id"].(string)
	require.True(t, ok, "response should contain client_id as string, got: %v", result)
	require.NotEmpty(t, clientID, "client_id should not be empty")
	return clientID
}

// extractClientSecret extracts the client_secret from a JSON response map.
func extractClientSecret(t *testing.T, result map[string]interface{}) string {
	t.Helper()
	clientSecret, ok := result["client_secret"].(string)
	require.True(t, ok, "response should contain client_secret as string, got: %v", result)
	require.NotEmpty(t, clientSecret, "client_secret should not be empty")
	return clientSecret
}

// deleteApp deletes an app (best-effort cleanup).
func deleteApp(t *testing.T, runner *TestRunner, tenantID, clientID string) {
	t.Helper()
	cmd := IAMAppDeleteCommand(tenantID, clientID)
	_, _, _ = runner.Run(cmd)
}

// outputContainsAny returns true if the output contains any of the given substrings.
func outputContainsAny(output string, substrings ...string) bool {
	for _, sub := range substrings {
		if strings.Contains(output, sub) {
			return true
		}
	}
	return false
}

// =============================================================================
// Lifecycle Test
// =============================================================================

// TestIAMAppLifecycle tests the complete IAM app lifecycle: create tenant,
// register app, get, update, list, rotate-secret, delete app, delete tenant.
func TestIAMAppLifecycle(t *testing.T) {
	RunForTransports(t, "IAMAppLifecycle", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		// Create tenant
		tenantID := createTestTenant(t, runner, "app-lifecycle")
		defer deleteTestTenant(t, runner, tenantID)

		appName := UniqueKey("lifecycle-app")

		// Register app
		result := registerAppJSON(t, runner, tenantID, appName)
		clientID := extractClientID(t, result)
		clientSecret := extractClientSecret(t, result)
		assert.NotEmpty(t, clientSecret, "initial registration should return a client secret")

		// Get app
		getCmd := IAMAppGetCommand(tenantID, clientID)
		stdout, _, err := runner.Run(getCmd)
		require.NoError(t, err, "get app should succeed")
		assert.Contains(t, stdout, clientID, "get output should contain client ID")

		// Update app name
		updatedName := UniqueKey("updated-lifecycle-app")
		updateCmd := IAMAppUpdateCommand(tenantID, clientID, updatedName)
		stdout, _, err = runner.Run(updateCmd)
		require.NoError(t, err, "update app should succeed")
		assert.Contains(t, stdout, clientID, "update output should contain client ID")

		// List apps
		listCmd := IAMAppListCommand(tenantID)
		stdout, _, err = runner.Run(listCmd)
		require.NoError(t, err, "list apps should succeed")
		assert.Contains(t, stdout, clientID, "list output should contain the registered app")

		// Rotate secret
		rotateCmd := IAMAppRotateSecretCommand(tenantID, clientID)
		stdout, _, err = runner.Run(rotateCmd)
		require.NoError(t, err, "rotate secret should succeed")
		assert.Contains(t, stdout, clientID, "rotate output should contain client ID")

		// Delete app
		deleteCmd := IAMAppDeleteCommand(tenantID, clientID)
		stdout, _, err = runner.Run(deleteCmd)
		require.NoError(t, err, "delete app should succeed")
		assert.Contains(t, stdout, clientID, "delete output should contain client ID")
	})
}

// =============================================================================
// Register Tests
// =============================================================================

// TestIAMAppRegister tests basic app registration and verifies that the output
// contains both a Client ID and a Client Secret.
func TestIAMAppRegister(t *testing.T) {
	RunForTransports(t, "IAMAppRegister", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createTestTenant(t, runner, "app-register")
		defer deleteTestTenant(t, runner, tenantID)

		appName := UniqueKey("register-app")
		cmd := IAMAppRegisterCommand(tenantID, appName)
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err, "register app should succeed")
		assert.Contains(t, stdout, appName, "register output should contain the app name")

		// The default text output should mention Client ID and Secret indicators.
		assert.True(t,
			outputContainsAny(stdout, "client_id", "Client ID", "client id"),
			"register output should contain a client ID indicator: %s", stdout)
		assert.True(t,
			outputContainsAny(stdout, "client_secret", "Client Secret", "client secret"),
			"register output should contain a client secret indicator: %s", stdout)
	})
}

// TestIAMAppRegisterWithRedirectURI tests app registration with a --redirect-uri flag.
func TestIAMAppRegisterWithRedirectURI(t *testing.T) {
	RunForTransports(t, "IAMAppRegisterWithRedirectURI", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createTestTenant(t, runner, "app-redirect")
		defer deleteTestTenant(t, runner, tenantID)

		appName := UniqueKey("redirect-app")
		redirectURI := "https://example.com/callback"
		cmd := IAMAppRegisterWithRedirectCommand(tenantID, appName, redirectURI)
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err, "register with redirect URI should succeed")
		assert.Contains(t, stdout, appName, "output should contain app name")
	})
}

// TestIAMAppRegisterJSON tests app registration with --output json and verifies
// the JSON response structure contains client_id and client_secret fields.
func TestIAMAppRegisterJSON(t *testing.T) {
	RunForTransports(t, "IAMAppRegisterJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createTestTenant(t, runner, "app-register-json")
		defer deleteTestTenant(t, runner, tenantID)

		appName := UniqueKey("json-app")
		result := registerAppJSON(t, runner, tenantID, appName)

		clientID := extractClientID(t, result)
		clientSecret := extractClientSecret(t, result)
		defer deleteApp(t, runner, tenantID, clientID)

		assert.NotEmpty(t, clientID, "JSON response must contain non-empty client_id")
		assert.NotEmpty(t, clientSecret, "JSON response must contain non-empty client_secret")

		// Verify additional fields if present
		if name, ok := result["name"].(string); ok {
			assert.Equal(t, appName, name, "JSON response name should match requested name")
		}
	})
}

// =============================================================================
// Get Tests
// =============================================================================

// TestIAMAppGet tests retrieving an app by client ID and verifying the app name
// appears in the output.
func TestIAMAppGet(t *testing.T) {
	RunForTransports(t, "IAMAppGet", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createTestTenant(t, runner, "app-get")
		defer deleteTestTenant(t, runner, tenantID)

		appName := UniqueKey("get-app")
		result := registerAppJSON(t, runner, tenantID, appName)
		clientID := extractClientID(t, result)
		defer deleteApp(t, runner, tenantID, clientID)

		getCmd := IAMAppGetCommand(tenantID, clientID)
		stdout, _, err := runner.Run(getCmd)
		require.NoError(t, err, "get app should succeed")
		assert.Contains(t, stdout, clientID, "get output should contain client ID")
		assert.Contains(t, stdout, appName, "get output should contain app name")
	})
}

// TestIAMAppGetJSON tests retrieving an app with --output json and verifies
// the JSON structure contains expected fields.
func TestIAMAppGetJSON(t *testing.T) {
	RunForTransports(t, "IAMAppGetJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createTestTenant(t, runner, "app-get-json")
		defer deleteTestTenant(t, runner, tenantID)

		appName := UniqueKey("getjson-app")
		registerResult := registerAppJSON(t, runner, tenantID, appName)
		clientID := extractClientID(t, registerResult)
		defer deleteApp(t, runner, tenantID, clientID)

		getCmd := CommandDef{
			Name:           "iam app get json",
			Args:           []string{"iam", "app", "get", clientID, "--tenant", tenantID, "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(getCmd)
		require.NoError(t, err, "get app with JSON output should succeed")

		var getResult map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(stdout), &getResult),
			"get output should be valid JSON: %s", stdout)

		gotClientID, ok := getResult["client_id"].(string)
		require.True(t, ok, "JSON response should contain client_id")
		assert.Equal(t, clientID, gotClientID, "returned client_id should match registered app")

		if name, ok := getResult["name"].(string); ok {
			assert.Equal(t, appName, name, "returned name should match registered app")
		}
	})
}

// =============================================================================
// Update Tests
// =============================================================================

// TestIAMAppUpdate tests updating an app name and verifying the change.
func TestIAMAppUpdate(t *testing.T) {
	RunForTransports(t, "IAMAppUpdate", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createTestTenant(t, runner, "app-update")
		defer deleteTestTenant(t, runner, tenantID)

		appName := UniqueKey("update-app")
		result := registerAppJSON(t, runner, tenantID, appName)
		clientID := extractClientID(t, result)
		defer deleteApp(t, runner, tenantID, clientID)

		// Update the name
		updatedName := UniqueKey("updated-app")
		updateCmd := IAMAppUpdateCommand(tenantID, clientID, updatedName)
		stdout, _, err := runner.Run(updateCmd)
		require.NoError(t, err, "update app should succeed")
		assert.Contains(t, stdout, clientID, "update output should contain client ID")

		// Verify the name was updated by getting the app
		getCmd := CommandDef{
			Name:           "iam app get after update",
			Args:           []string{"iam", "app", "get", clientID, "--tenant", tenantID, "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err = runner.Run(getCmd)
		require.NoError(t, err, "get app after update should succeed")

		var getResult map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(stdout), &getResult),
			"get output should be valid JSON: %s", stdout)

		if name, ok := getResult["name"].(string); ok {
			assert.Equal(t, updatedName, name, "app name should be updated")
		}
	})
}

// TestIAMAppUpdateRedirectURI tests updating an app's redirect URI.
func TestIAMAppUpdateRedirectURI(t *testing.T) {
	RunForTransports(t, "IAMAppUpdateRedirectURI", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createTestTenant(t, runner, "app-update-uri")
		defer deleteTestTenant(t, runner, tenantID)

		appName := UniqueKey("uri-app")
		result := registerAppJSON(t, runner, tenantID, appName)
		clientID := extractClientID(t, result)
		defer deleteApp(t, runner, tenantID, clientID)

		// Update redirect URI
		newRedirectURI := "https://updated.example.com/oauth/callback"
		updateCmd := CommandDef{
			Name:           "iam app update redirect-uri",
			Args:           []string{"iam", "app", "update", clientID, "--tenant", tenantID, "--redirect-uri", newRedirectURI},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(updateCmd)
		require.NoError(t, err, "update redirect URI should succeed")
		assert.Contains(t, stdout, clientID, "update output should contain client ID")

		// Verify the redirect URI was updated
		getCmd := CommandDef{
			Name:           "iam app get after redirect update",
			Args:           []string{"iam", "app", "get", clientID, "--tenant", tenantID, "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err = runner.Run(getCmd)
		require.NoError(t, err, "get app after redirect update should succeed")

		var getResult map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(stdout), &getResult),
			"get output should be valid JSON: %s", stdout)

		// Check redirect URIs in response - handle both array and single-value formats
		if uris, ok := getResult["redirect_uris"].([]interface{}); ok {
			found := false
			for _, uri := range uris {
				if uriStr, ok := uri.(string); ok && uriStr == newRedirectURI {
					found = true
					break
				}
			}
			assert.True(t, found, "updated redirect URI should appear in app's redirect_uris: %v", uris)
		} else if uri, ok := getResult["redirect_uri"].(string); ok {
			assert.Equal(t, newRedirectURI, uri, "updated redirect URI should match")
		}
	})
}

// =============================================================================
// Delete Tests
// =============================================================================

// TestIAMAppDelete tests deleting an app and verifying that a subsequent get fails.
func TestIAMAppDelete(t *testing.T) {
	RunForTransports(t, "IAMAppDelete", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createTestTenant(t, runner, "app-delete")
		defer deleteTestTenant(t, runner, tenantID)

		appName := UniqueKey("delete-app")
		result := registerAppJSON(t, runner, tenantID, appName)
		clientID := extractClientID(t, result)

		// Delete the app
		deleteCmd := IAMAppDeleteCommand(tenantID, clientID)
		stdout, _, err := runner.Run(deleteCmd)
		require.NoError(t, err, "delete app should succeed")
		assert.Contains(t, stdout, clientID, "delete output should contain client ID")

		// Verify the app is gone by attempting to get it
		getCmd := CommandDef{
			Name:           "iam app get after delete",
			Args:           []string{"iam", "app", "get", clientID, "--tenant", tenantID},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		_, _, err = runner.Run(getCmd)
		require.Error(t, err, "get deleted app should fail")
	})
}

// =============================================================================
// List Tests
// =============================================================================

// TestIAMAppList tests listing apps within a tenant after registering multiple apps.
func TestIAMAppList(t *testing.T) {
	RunForTransports(t, "IAMAppList", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createTestTenant(t, runner, "app-list")
		defer deleteTestTenant(t, runner, tenantID)

		// Register two apps
		app1Name := UniqueKey("list-app-1")
		result1 := registerAppJSON(t, runner, tenantID, app1Name)
		clientID1 := extractClientID(t, result1)
		defer deleteApp(t, runner, tenantID, clientID1)

		app2Name := UniqueKey("list-app-2")
		result2 := registerAppJSON(t, runner, tenantID, app2Name)
		clientID2 := extractClientID(t, result2)
		defer deleteApp(t, runner, tenantID, clientID2)

		// List apps
		listCmd := IAMAppListCommand(tenantID)
		stdout, _, err := runner.Run(listCmd)
		require.NoError(t, err, "list apps should succeed")
		assert.Contains(t, stdout, clientID1, "list output should contain first app's client ID")
		assert.Contains(t, stdout, clientID2, "list output should contain second app's client ID")
	})
}

// TestIAMAppListJSON tests listing apps with --output json and verifies valid JSON.
func TestIAMAppListJSON(t *testing.T) {
	RunForTransports(t, "IAMAppListJSON", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createTestTenant(t, runner, "app-list-json")
		defer deleteTestTenant(t, runner, tenantID)

		// Register two apps
		app1Name := UniqueKey("listjson-app-1")
		result1 := registerAppJSON(t, runner, tenantID, app1Name)
		clientID1 := extractClientID(t, result1)
		defer deleteApp(t, runner, tenantID, clientID1)

		app2Name := UniqueKey("listjson-app-2")
		result2 := registerAppJSON(t, runner, tenantID, app2Name)
		clientID2 := extractClientID(t, result2)
		defer deleteApp(t, runner, tenantID, clientID2)

		// List apps with JSON output
		listCmd := CommandDef{
			Name:           "iam app list json",
			Args:           []string{"iam", "app", "list", "--tenant", tenantID, "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        15 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(listCmd)
		require.NoError(t, err, "list apps with JSON output should succeed")

		// Verify the output is valid JSON
		var listResult interface{}
		require.NoError(t, json.Unmarshal([]byte(stdout), &listResult),
			"list output should be valid JSON: %s", stdout)

		// Verify both client IDs appear in the JSON output
		assert.Contains(t, stdout, clientID1, "JSON list should contain first app's client ID")
		assert.Contains(t, stdout, clientID2, "JSON list should contain second app's client ID")
	})
}

// =============================================================================
// Rotate Secret Tests
// =============================================================================

// TestIAMAppRotateSecret tests rotating an app's client secret and verifies
// that a new secret is returned that differs from the original.
func TestIAMAppRotateSecret(t *testing.T) {
	RunForTransports(t, "IAMAppRotateSecret", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createTestTenant(t, runner, "app-rotate")
		defer deleteTestTenant(t, runner, tenantID)

		appName := UniqueKey("rotate-app")
		result := registerAppJSON(t, runner, tenantID, appName)
		clientID := extractClientID(t, result)
		originalSecret := extractClientSecret(t, result)
		defer deleteApp(t, runner, tenantID, clientID)

		// Rotate the secret with JSON output to capture the new secret
		rotateCmd := CommandDef{
			Name:           "iam app rotate-secret json",
			Args:           []string{"iam", "app", "rotate-secret", clientID, "--tenant", tenantID, "--output", "json"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, _, err := runner.Run(rotateCmd)
		require.NoError(t, err, "rotate secret should succeed")

		var rotateResult map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(stdout), &rotateResult),
			"rotate output should be valid JSON: %s", stdout)

		newSecret, ok := rotateResult["client_secret"].(string)
		require.True(t, ok, "rotate response should contain client_secret: %v", rotateResult)
		require.NotEmpty(t, newSecret, "new client secret should not be empty")
		assert.NotEqual(t, originalSecret, newSecret, "rotated secret should differ from original")
	})
}

// =============================================================================
// Error Handling Tests
// =============================================================================

// TestIAMAppGetNonExistent tests that getting a nonexistent app client ID returns
// an error with a meaningful message.
func TestIAMAppGetNonExistent(t *testing.T) {
	RunForTransports(t, "IAMAppGetNonExistent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createTestTenant(t, runner, "app-get-noexist")
		defer deleteTestTenant(t, runner, tenantID)

		nonExistentClientID := "non-existent-client-id-00000000"
		getCmd := CommandDef{
			Name:           "iam app get non-existent",
			Args:           []string{"iam", "app", "get", nonExistentClientID, "--tenant", tenantID},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(getCmd)
		require.Error(t, err, "get non-existent app should fail")

		combined := stdout + stderr
		assert.True(t,
			outputContainsAny(combined, "not found", "not_found", "does not exist", "no such", "unknown"),
			"error output should indicate the app was not found: %s", combined)
	})
}

// TestIAMAppDeleteNonExistent tests that deleting a nonexistent app client ID
// returns an error with a meaningful message.
func TestIAMAppDeleteNonExistent(t *testing.T) {
	RunForTransports(t, "IAMAppDeleteNonExistent", iamTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := createTestTenant(t, runner, "app-del-noexist")
		defer deleteTestTenant(t, runner, tenantID)

		nonExistentClientID := "non-existent-client-id-99999999"
		deleteCmd := CommandDef{
			Name:           "iam app delete non-existent",
			Args:           []string{"iam", "app", "delete", nonExistentClientID, "--tenant", tenantID, "--yes"},
			ExpectSuccess:  false,
			Timeout:        10 * time.Second,
			SkipTransports: iamTransportsToSkip,
		}
		stdout, stderr, err := runner.Run(deleteCmd)
		require.Error(t, err, "delete non-existent app should fail")

		combined := stdout + stderr
		assert.True(t,
			outputContainsAny(combined, "not found", "not_found", "does not exist", "no such", "unknown"),
			"error output should indicate the app was not found: %s", combined)
	})
}
