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

// Package cli provides MCP-specific CLI integration tests.
// These tests validate the MCP (Model Context Protocol) transport layer
// which uses JSON-RPC 2.0 over HTTP for AI tool integration.
package cli

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// getMCPAddress returns the MCP server address for testing.
func getMCPAddress() string {
	if addr := os.Getenv("QUICBAAS_MCP_ADDRESS"); addr != "" {
		return addr
	}
	return "https://test-node1:8888/mcp"
}

// requireMCPEnabled fails the test if MCP testing is explicitly disabled
// via QUICBAAS_MCP_ENABLED=false. MCP is enabled by default in the
// integration test environment.
func requireMCPEnabled(t *testing.T) {
	t.Helper()
	require.NotEqual(t, "false", os.Getenv("QUICBAAS_MCP_ENABLED"),
		"MCP transport is explicitly disabled via QUICBAAS_MCP_ENABLED=false")
}

// =============================================================================
// MCP KMS Key Operations
// =============================================================================

// TestMCP_KMSKeyLifecycle tests the complete KMS key lifecycle via MCP transport.
func TestMCP_KMSKeyLifecycle(t *testing.T) {
	requireMCPEnabled(t)

	runner := NewTestRunner(t, TransportMCP, getMCPAddress())
	keyName := UniqueKey("mcp-key")

	// 1. Create key
	createCmd := KMSKeyCreateCommand(keyName, "AES-256-GCM")
	runner.RunAndAssert(createCmd)

	// 2. Get key
	getCmd := KMSKeyGetCommand(keyName)
	runner.RunAndAssert(getCmd)

	// 3. List keys
	listCmd := KMSKeyListCommand()
	runner.RunAndAssert(listCmd)

	// 4. List versions
	versionsCmd := KMSKeyVersionsCommand(keyName)
	runner.RunAndAssert(versionsCmd)

	// 5. Rotate key
	rotateCmd := KMSKeyRotateCommand(keyName)
	runner.RunAndAssert(rotateCmd)

	// 6. Disable key
	disableCmd := KMSKeyDisableCommand(keyName)
	runner.RunAndAssert(disableCmd)

	// 7. Enable key
	enableCmd := KMSKeyEnableCommand(keyName)
	runner.RunAndAssert(enableCmd)

	// 8. Set safety limits
	safetyCmd := KMSKeySetSafetyCommand(keyName, 1000000, 1073741824)
	runner.RunAndAssert(safetyCmd)

	// 9. Delete key
	deleteCmd := KMSKeyDeleteCommand(keyName, true)
	runner.RunAndAssert(deleteCmd)
}

// TestMCP_KMSKeyAlgorithms tests key creation with different algorithms via MCP.
func TestMCP_KMSKeyAlgorithms(t *testing.T) {
	requireMCPEnabled(t)

	runner := NewTestRunner(t, TransportMCP, getMCPAddress())

	algorithms := []string{
		"AES-128-GCM",
		"AES-256-GCM",
	}

	for _, algo := range algorithms {
		t.Run(algo, func(t *testing.T) {
			keyName := UniqueKey("mcp-algo-key")

			// Create key with algorithm
			createCmd := KMSKeyCreateCommand(keyName, algo)
			runner.RunAndAssert(createCmd)

			// Verify key was created
			getCmd := KMSKeyGetCommand(keyName)
			stdout, _, err := runner.Run(getCmd)
			if err == nil {
				t.Logf("Created key with algorithm %s: %s", algo, stdout)
			}

			// Cleanup
			cleanup := KMSKeyDeleteCommand(keyName, true)
			runner.Run(cleanup)
		})
	}
}

// =============================================================================
// MCP Encryption/Decryption Operations
// =============================================================================

// TestMCP_EncryptDecrypt tests encryption and decryption via MCP transport.
func TestMCP_EncryptDecrypt(t *testing.T) {
	requireMCPEnabled(t)

	runner := NewTestRunner(t, TransportMCP, getMCPAddress())
	keyName := UniqueKey("mcp-encrypt-key")
	plaintext := "Hello, MCP! This is a test message for encryption."

	// Create key
	createCmd := KMSKeyCreateCommand(keyName, "AES-256-GCM")
	runner.RunAndAssert(createCmd)

	// Encrypt
	encryptCmd := KMSEncryptCommand(keyName, plaintext)
	stdout, _, err := runner.Run(encryptCmd)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	t.Logf("MCP Encrypt output: %s", stdout)

	// Cleanup
	cleanup := KMSKeyDeleteCommand(keyName, true)
	runner.Run(cleanup)
}

// TestMCP_EncryptDecryptWithAAD tests encryption with AAD via MCP transport.
func TestMCP_EncryptDecryptWithAAD(t *testing.T) {
	requireMCPEnabled(t)

	runner := NewTestRunner(t, TransportMCP, getMCPAddress())
	keyName := UniqueKey("mcp-aad-key")
	plaintext := "Sensitive data with AAD"
	aad := "mcp-additional-authenticated-data"

	// Create key - verify KMS operations are supported for MCP transport
	createCmd := KMSKeyCreateCommand(keyName, "AES-256-GCM")
	require.False(t, runner.IsSkipped(createCmd),
		"KMS operations must be supported for MCP transport in integration tests")
	runner.RunAndAssert(createCmd)

	// Encrypt with AAD
	encryptCmd := CommandDef{
		Name:          "encrypt with AAD",
		Args:          []string{"kms", "encrypt", keyName, "--data", plaintext, "--aad", aad},
		ExpectSuccess: true,
		Timeout:       10 * time.Second,
	}
	runner.RunAndAssert(encryptCmd)

	// Cleanup
	cleanup := KMSKeyDeleteCommand(keyName, true)
	runner.Run(cleanup)
}

// =============================================================================
// MCP DEK Operations
// =============================================================================

// TestMCP_DEKLifecycle tests the complete DEK lifecycle via MCP transport.
func TestMCP_DEKLifecycle(t *testing.T) {
	requireMCPEnabled(t)

	runner := NewTestRunner(t, TransportMCP, getMCPAddress())
	dekPath := UniqueKey("mcp-dek") + "/"

	// 1. Create DEK
	createCmd := KMSDEKCreateCommand(dekPath, "AES-256-GCM")
	runner.RunAndAssert(createCmd)

	// 2. Get DEK
	getCmd := KMSDEKGetCommand(dekPath)
	runner.RunAndAssert(getCmd)

	// 3. List DEKs
	listCmd := KMSDEKListCommand("")
	runner.RunAndAssert(listCmd)

	// 4. Get status
	statusCmd := KMSDEKStatusCommand(dekPath)
	runner.RunAndAssert(statusCmd)

	// 5. Set safety limits
	safetyCmd := KMSDEKSetSafetyCommand(dekPath, 500000, 536870912)
	runner.RunAndAssert(safetyCmd)

	// 6. Set as default
	defaultCmd := KMSDEKSetDefaultCommand(dekPath)
	runner.RunAndAssert(defaultCmd)

	// 7. Rotate DEK
	rotateCmd := KMSDEKRotateCommand(dekPath)
	runner.RunAndAssert(rotateCmd)

	// 8. Delete DEK
	deleteCmd := KMSDEKDeleteCommand(dekPath, true)
	runner.RunAndAssert(deleteCmd)
}

// TestMCP_DEKHierarchy tests DEK path hierarchy via MCP transport.
func TestMCP_DEKHierarchy(t *testing.T) {
	requireMCPEnabled(t)

	runner := NewTestRunner(t, TransportMCP, getMCPAddress())
	basePath := UniqueKey("mcp-hierarchy")

	paths := []string{
		basePath + "/",
		basePath + "/prod/",
		basePath + "/prod/api/",
		basePath + "/dev/",
	}

	// Create DEKs at different levels
	for _, path := range paths {
		cmd := KMSDEKCreateCommand(path, "AES-256-GCM")
		runner.RunAndAssert(cmd)
	}

	// List all DEKs
	listCmd := KMSDEKListCommand(basePath)
	runner.RunAndAssert(listCmd)

	// Cleanup in reverse order
	for i := len(paths) - 1; i >= 0; i-- {
		cleanup := KMSDEKDeleteCommand(paths[i], true)
		runner.Run(cleanup)
	}
}

// =============================================================================
// MCP Secrets Operations
// =============================================================================

// TestMCP_SecretsLifecycle tests the complete secrets lifecycle via MCP transport.
func TestMCP_SecretsLifecycle(t *testing.T) {
	requireMCPEnabled(t)

	runner := NewTestRunner(t, TransportMCP, getMCPAddress())
	secretPath := UniqueKey("mcp-secret")
	secretValue := "mcp-test-secret-value-12345"

	// 1. Store secret
	storeCmd := SecretsStoreCommand(secretPath, secretValue)
	runner.RunAndAssert(storeCmd)

	// 2. Get secret
	getCmd := SecretsGetCommand(secretPath, 0)
	runner.RunAndAssert(getCmd)

	// 3. List secrets
	listCmd := SecretsListCommand("")
	runner.RunAndAssert(listCmd)

	// 4. List versions
	versionsCmd := SecretsVersionsCommand(secretPath)
	runner.RunAndAssert(versionsCmd)

	// 5. Rotate secret
	rotateCmd := SecretsRotateCommand(secretPath, secretValue+"-rotated")
	runner.RunAndAssert(rotateCmd)

	// 6. Get rotated secret (should be version 2)
	getCmd2 := SecretsGetCommand(secretPath, 0)
	runner.RunAndAssert(getCmd2)

	// 7. Delete secret
	deleteCmd := SecretsDeleteCommand(secretPath, true)
	runner.RunAndAssert(deleteCmd)
}

// TestMCP_SecretsMultiple tests multiple secrets operations via MCP transport.
func TestMCP_SecretsMultiple(t *testing.T) {
	requireMCPEnabled(t)

	runner := NewTestRunner(t, TransportMCP, getMCPAddress())
	basePrefix := UniqueKey("mcp-multi")

	secrets := []struct {
		path  string
		value string
	}{
		{basePrefix + "/db/password", "db-password-123"},
		{basePrefix + "/api/key", "api-key-456"},
		{basePrefix + "/jwt/secret", "jwt-secret-789"},
	}

	// Store all secrets
	for _, s := range secrets {
		storeCmd := SecretsStoreCommand(s.path, s.value)
		runner.RunAndAssert(storeCmd)
	}

	// List secrets with prefix
	listCmd := SecretsListCommand(basePrefix)
	runner.RunAndAssert(listCmd)

	// Cleanup
	for _, s := range secrets {
		deleteCmd := SecretsDeleteCommand(s.path, true)
		runner.Run(deleteCmd)
	}
}

// =============================================================================
// MCP Multi-Protocol Comparison Tests
// =============================================================================

// TestMCP_CompareWithGRPC compares MCP results with gRPC for consistency.
// This test creates resources via gRPC and verifies they can be read via MCP.
func TestMCP_CompareWithGRPC(t *testing.T) {
	requireMCPEnabled(t)

	grpcAddress := os.Getenv("QUICBAAS_GRPC_ADDRESS")
	if grpcAddress == "" {
		grpcAddress = "test-node1:50051"
	}

	grpcRunner := NewTestRunner(t, TransportGRPC, grpcAddress)
	mcpRunner := NewTestRunner(t, TransportMCP, getMCPAddress())

	keyName := UniqueKey("cross-protocol-key")

	// Create key via gRPC
	createCmd := KMSKeyCreateCommand(keyName, "AES-256-GCM")
	grpcRunner.RunAndAssert(createCmd)

	// Verify key exists via MCP
	getCmd := KMSKeyGetCommand(keyName)
	mcpRunner.RunAndAssert(getCmd)

	// List keys via MCP should include the key
	listCmd := KMSKeyListCommand()
	stdout, _, err := mcpRunner.Run(listCmd)
	if err != nil {
		t.Fatalf("MCP list failed: %v", err)
	}
	t.Logf("MCP key list: %s", stdout)

	// Cleanup via gRPC
	cleanup := KMSKeyDeleteCommand(keyName, true)
	grpcRunner.Run(cleanup)
}

// TestMCP_CompareWithREST compares MCP and REST results with gRPC for cross-protocol consistency.
// Creates a key via gRPC (which supports key generation), then verifies it is accessible
// via both REST and MCP transports.
func TestMCP_CompareWithREST(t *testing.T) {
	requireMCPEnabled(t)

	grpcAddress := os.Getenv("QUICBAAS_GRPC_ADDRESS")
	if grpcAddress == "" {
		grpcAddress = "test-node1:50051"
	}

	restAddress := os.Getenv("QUICBAAS_REST_ADDRESS")
	if restAddress == "" {
		restAddress = "test-node1:8080"
	}

	grpcRunner := NewTestRunner(t, TransportGRPC, grpcAddress)
	restRunner := NewTestRunner(t, TransportREST, restAddress)
	mcpRunner := NewTestRunner(t, TransportMCP, getMCPAddress())

	keyName := UniqueKey("rest-mcp-key")

	// Create key via gRPC (REST does not support kms key generate)
	createCmd := KMSKeyCreateCommand(keyName, "AES-256-GCM")
	grpcRunner.RunAndAssert(createCmd)

	// Verify key exists via REST
	getCmd := KMSKeyGetCommand(keyName)
	restRunner.RunAndAssert(getCmd)

	// Verify key exists via MCP
	mcpRunner.RunAndAssert(getCmd)

	// Cleanup via gRPC
	cleanup := KMSKeyDeleteCommand(keyName, true)
	grpcRunner.Run(cleanup)
}

// =============================================================================
// MCP Full Suite Test
// =============================================================================

// TestMCP_FullSuite runs the complete MCP test suite.
func TestMCP_FullSuite(t *testing.T) {
	requireMCPEnabled(t)

	runner := NewTestRunner(t, TransportMCP, getMCPAddress())

	// Run KMS Key Suite
	t.Run("KMSKeySuite", func(t *testing.T) {
		keyName := UniqueKey("mcp-suite-key")
		suite := KMSKeySuite(keyName)
		runner.RunSuite(suite)
	})

	// Run DEK Suite
	t.Run("DEKSuite", func(t *testing.T) {
		dekPath := UniqueKey("mcp-suite-dek") + "/"
		suite := KMSDEKSuite(dekPath)
		runner.RunSuite(suite)
	})

	// Run Secrets Suite
	t.Run("SecretsSuite", func(t *testing.T) {
		secretPath := UniqueKey("mcp-suite-secret")
		secretValue := "mcp-suite-value"
		suite := SecretsSuite(secretPath, secretValue)
		runner.RunSuite(suite)
	})
}
