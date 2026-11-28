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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestSecretsPutRawFromFile proves #143 end to end on every transport: a
// file's raw bytes (not KEY=VALUE JSON) stored with `secrets put --from-file`
// read back verbatim with `secrets get`.
func TestSecretsPutRawFromFile(t *testing.T) {
	RunForAllTransports(t, "SecretsPutRawFromFile", func(t *testing.T, runner *TestRunner) {
		secretPath := UniqueKey("raw-secret")
		raw := "whsec_raw-webhook-hmac-key:not=json\n"
		file := filepath.Join(t.TempDir(), "hmac.key")
		require.NoError(t, os.WriteFile(file, []byte(raw), 0o600))

		put := SecretsPutFromFileCommand(secretPath, file)
		if runner.IsSkipped(put) {
			return
		}
		runner.RunAndAssert(put)

		stdout, stderr, err := runner.Run(SecretsGetOnEveryTransportCommand(secretPath))
		require.NoError(t, err, "secrets get: %s", stderr)
		assert.Equal(t, raw, stdout, "the raw bytes read back verbatim")

		runner.Run(SecretsRoundTripCleanupCommand(runner.transport, secretPath))
	})
}

// TestSecretsPutRawFromFileJSONObject round-trips a file whose content is a
// JSON object (an auth.json): `secrets get` prints it byte for byte, both by
// default and with --output raw, on every transport.
func TestSecretsPutRawFromFileJSONObject(t *testing.T) {
	RunForAllTransports(t, "SecretsPutRawFromFileJSONObject", func(t *testing.T, runner *TestRunner) {
		secretPath := UniqueKey("raw-json-secret")
		raw := "{\n  \"token\": \"t0k\",\n  \"nested\": {\"a\": [1, 2]}\n}\n"
		file := filepath.Join(t.TempDir(), "auth.json")
		require.NoError(t, os.WriteFile(file, []byte(raw), 0o600))

		put := SecretsPutFromFileCommand(secretPath, file)
		if runner.IsSkipped(put) {
			return
		}
		runner.RunAndAssert(put)

		get := SecretsGetOnEveryTransportCommand(secretPath)
		stdout, stderr, err := runner.Run(get)
		require.NoError(t, err, "secrets get: %s", stderr)
		assert.Equal(t, raw, stdout, "the JSON file reads back verbatim")

		get.Args = append(get.Args, "--output", "raw")
		stdout, stderr, err = runner.Run(get)
		require.NoError(t, err, "secrets get --output raw: %s", stderr)
		assert.Equal(t, raw, stdout, "--output raw is the file verbatim")

		runner.Run(SecretsRoundTripCleanupCommand(runner.transport, secretPath))
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
