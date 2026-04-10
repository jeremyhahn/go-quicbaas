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
	"testing"
)

// =============================================================================
// Secrets Lifecycle Tests
// =============================================================================

// TestSecretsLifecycle tests the complete secrets lifecycle across all transports.
func TestSecretsLifecycle(t *testing.T) {
	RunForAllTransports(t, "SecretsLifecycle", func(t *testing.T, runner *TestRunner) {
		secretPath := UniqueKey("test-secret")
		secretValue := "super-secret-value-12345"
		suite := SecretsSuite(secretPath, secretValue)
		runner.RunSuite(suite)
	})
}

// TestSecretsStore tests storing secrets across all transports.
func TestSecretsStore(t *testing.T) {
	RunForAllTransports(t, "SecretsStore", func(t *testing.T, runner *TestRunner) {
		secretPath := UniqueKey("store-secret")
		secretValue := "test-secret-value"

		cmd := SecretsStoreCommand(secretPath, secretValue)
		runner.RunAndAssert(cmd)

		// Cleanup
		cleanup := SecretsDeleteCommand(secretPath, true)
		runner.Run(cleanup)
	})
}

// TestSecretsGet tests getting secrets across all transports.
func TestSecretsGet(t *testing.T) {
	RunForAllTransports(t, "SecretsGet", func(t *testing.T, runner *TestRunner) {
		secretPath := UniqueKey("get-secret")
		secretValue := "get-secret-value"

		// Store first
		storeCmd := SecretsStoreCommand(secretPath, secretValue)
		runner.RunAndAssert(storeCmd)

		// Get
		getCmd := SecretsGetCommand(secretPath, 0)
		runner.RunAndAssert(getCmd)

		// Cleanup
		cleanup := SecretsDeleteCommand(secretPath, true)
		runner.Run(cleanup)
	})
}

// TestSecretsVersioning tests secret versioning across all transports.
func TestSecretsVersioning(t *testing.T) {
	RunForAllTransports(t, "SecretsVersioning", func(t *testing.T, runner *TestRunner) {
		secretPath := UniqueKey("versioned-secret")

		// Store initial version
		storeCmd := SecretsStoreCommand(secretPath, "version-1")
		runner.RunAndAssert(storeCmd)

		// Rotate to create version 2
		rotateCmd := SecretsRotateCommand(secretPath, "version-2")
		runner.RunAndAssert(rotateCmd)

		// Rotate to create version 3
		rotateCmd2 := SecretsRotateCommand(secretPath, "version-3")
		runner.RunAndAssert(rotateCmd2)

		// List versions
		versionsCmd := SecretsVersionsCommand(secretPath)
		runner.RunAndAssert(versionsCmd)

		// Get specific version
		getV1Cmd := SecretsGetCommand(secretPath, 1)
		stdout, _, err := runner.Run(getV1Cmd)
		if err == nil {
			t.Logf("Version 1: %s", stdout)
		}

		// Cleanup
		cleanup := SecretsDeleteCommand(secretPath, true)
		runner.Run(cleanup)
	})
}

// TestSecretsHierarchy tests secret path hierarchy across all transports.
func TestSecretsHierarchy(t *testing.T) {
	RunForAllTransports(t, "SecretsHierarchy", func(t *testing.T, runner *TestRunner) {
		basePath := UniqueKey("hierarchy")
		paths := []string{
			basePath + "/db/password",
			basePath + "/db/username",
			basePath + "/api/key",
			basePath + "/api/secret",
		}

		// Create secrets at different paths
		for i, path := range paths {
			cmd := SecretsStoreCommand(path, "secret-value-"+string(rune('a'+i)))
			runner.RunAndAssert(cmd)
		}

		// List all secrets under base path
		listCmd := SecretsListCommand(basePath)
		runner.RunAndAssert(listCmd)

		// Cleanup
		for _, path := range paths {
			cleanup := SecretsDeleteCommand(path, true)
			runner.Run(cleanup)
		}
	})
}

// TestSecretsDelete tests deleting secrets across all transports.
func TestSecretsDelete(t *testing.T) {
	RunForAllTransports(t, "SecretsDelete", func(t *testing.T, runner *TestRunner) {
		secretPath := UniqueKey("delete-secret")
		secretValue := "delete-me"

		// Store
		storeCmd := SecretsStoreCommand(secretPath, secretValue)
		runner.RunAndAssert(storeCmd)

		// Delete
		deleteCmd := SecretsDeleteCommand(secretPath, true)
		runner.RunAndAssert(deleteCmd)

		// Verify deletion - should fail
		getCmd := SecretsGetCommand(secretPath, 0)
		getCmd.ExpectSuccess = false // Should fail after deletion
		stdout, stderr, err := runner.Run(getCmd)
		if err != nil {
			t.Logf("Get after delete correctly failed: stdout=%s stderr=%s", stdout, stderr)
		}
	})
}
