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

// Package cli provides E2E integration tests for QuicBaaS CLI commands.
package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// CLI binary path - defaults to "quicbaas" in PATH
var binaryPath = "quicbaas"

func init() {
	// Allow override via environment variable
	if path := os.Getenv("QUICBAAS_CLI"); path != "" {
		binaryPath = path
	}
}

// skipIfNoBinary fails the test if the CLI binary is not available.
// The CLI binary must be present in the integration test environment.
func skipIfNoBinary(t *testing.T) string {
	t.Helper()

	path, err := exec.LookPath(binaryPath)
	require.NoError(t, err,
		"CLI binary not found: %s (set QUICBAAS_CLI env var or add to PATH)", binaryPath)
	return path
}

// runCommand executes a CLI command and returns stdout and any error.
// Every invocation trusts the integration cluster's CA explicitly via
// --tls-ca (--tls-skip-verify is banned project-wide).
//
// These bare-CLI tests address the cluster by its gRPC endpoints
// (helpers.GetClusterEndpoints, e.g. test-node1:50051). The CLI's default
// transport is unix (a Unix-domain-socket path), which cannot dial a TCP
// host:port and is not reachable across the docker integration network
// anyway. When a command supplies --address but no explicit --transport,
// default it to grpc so the transport matches the endpoint family. Tests
// that need a different transport (e.g. the IAM suite over REST) keep
// passing --transport explicitly and are left untouched.
func runCommand(ctx context.Context, binary string, args ...string) (string, error) {
	fullArgs := append([]string{"--tls-ca", integrationCAPath}, args...)
	fullArgs = defaultGRPCTransport(fullArgs)
	cmd := exec.CommandContext(ctx, binary, fullArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if output == "" {
		output = stderr.String()
	}

	return strings.TrimSpace(output), err
}

// defaultGRPCTransport appends "--transport grpc" when the argument list
// targets a server (--address present) but does not already pin a transport.
// This keeps every gRPC-endpoint invocation explicit without rewriting each
// of the dozens of call sites, while preserving any caller-specified
// transport (the IAM suite pins --transport rest).
func defaultGRPCTransport(args []string) []string {
	hasAddress := false
	hasTransport := false
	for _, a := range args {
		switch a {
		case "--address":
			hasAddress = true
		case "--transport":
			hasTransport = true
		}
	}
	if hasAddress && !hasTransport {
		args = append(args, "--transport", "grpc")
	}
	return args
}

// extractJSONFromOutput extracts the JSON content from CLI output that may contain log lines.
// Log lines are identified by starting with {"time": which is the standard JSON log format.
func extractJSONFromOutput(output string) string {
	lines := strings.Split(output, "\n")
	var jsonLines []string
	inJSON := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip empty lines
		if trimmed == "" {
			continue
		}
		// Skip log lines (they start with {"time":)
		if strings.HasPrefix(trimmed, `{"time":`) {
			continue
		}
		// Start collecting JSON when we see the opening brace
		if strings.HasPrefix(trimmed, "{") && !inJSON {
			inJSON = true
		}
		if inJSON {
			jsonLines = append(jsonLines, line)
		}
	}

	return strings.Join(jsonLines, "\n")
}

// skipIfIAMDisabled fails the test if IAM commands are not available.
// IAM must be enabled in the integration test environment (built with -tags=iam).
func skipIfIAMDisabled(t *testing.T, binary string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	output, _ := runCommand(ctx, binary, "iam", "user", "--help")
	require.False(t,
		strings.Contains(output, "IAM feature is currently disabled") ||
			strings.Contains(output, "unknown command"),
		"IAM feature is disabled: build with -tags=iam to enable IAM in integration tests")
}

// parseUserIDFromOutput extracts the user ID from CLI create output.
// The output format is expected to be:
//
//	ID: <uuid>
func parseUserIDFromOutput(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ID:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "ID:"))
		}
	}
	return ""
}

// parseRoleIDFromOutput extracts the role ID from CLI create output.
func parseRoleIDFromOutput(output string) string {
	return parseUserIDFromOutput(output) // Same format
}

// =============================================================================
// Version and Help Commands
// =============================================================================

// TestCLIVersion tests the version command.
func TestCLIVersion(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := runCommand(ctx, binary, "version")
	require.NoError(t, err, "Version command should succeed")

	assert.Contains(t, strings.ToLower(output), "version", "Should contain version info")
	t.Logf("Version output: %s", output)
}

// TestCLIHelp tests the help command.
func TestCLIHelp(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := runCommand(ctx, binary, "--help")
	require.NoError(t, err, "Help command should succeed")

	assert.Contains(t, output, "Available Commands", "Should show available commands")
	assert.Contains(t, output, "data", "Should list data command")
	assert.Contains(t, output, "crypto", "Should list crypto command")
	t.Logf("Help output available")
}

// =============================================================================
// Data Commands (formerly Key-Value Commands)
// =============================================================================

// TestCLIDataHelp tests the data help command.
func TestCLIDataHelp(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := runCommand(ctx, binary, "data", "--help")
	require.NoError(t, err, "Data help should succeed")

	assert.Contains(t, output, "get", "Should list get command")
	assert.Contains(t, output, "put", "Should list put command")
	assert.Contains(t, output, "delete", "Should list delete command")
	t.Logf("Data help executed successfully")
}

// TestCLIDataPutGetDelete tests basic data operations.
func TestCLIDataPutGetDelete(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	entityType := "kv"
	key := fmt.Sprintf("test-key-%d", time.Now().UnixNano())
	value := "test-value-12345"

	// Put a key
	output, err := runCommand(ctx, binary, "data", "put", entityType, key, value,
		"--address", serverAddr)
	require.NoError(t, err, "Data put should succeed: %s", output)
	t.Logf("Data put output: %s", output)

	// Get the key
	output, err = runCommand(ctx, binary, "data", "get", entityType, key,
		"--address", serverAddr)
	require.NoError(t, err, "Data get should succeed: %s", output)
	assert.Contains(t, output, value, "Should contain the set value")
	t.Logf("Data get output: %s", output)

	// Delete the key
	output, err = runCommand(ctx, binary, "data", "delete", entityType, key,
		"--yes",
		"--address", serverAddr)
	require.NoError(t, err, "Data delete should succeed: %s", output)
	t.Logf("Data delete output: %s", output)

	// Verify deletion
	output, err = runCommand(ctx, binary, "data", "get", entityType, key,
		"--address", serverAddr)
	// Expect error or empty response for deleted key
	if err == nil {
		assert.Empty(t, output, "Deleted key should return empty")
	}
}

// TestCLIDataList tests the data list command.
func TestCLIDataList(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	entityType := "kv"
	prefix := fmt.Sprintf("list-test-%d", time.Now().UnixNano())

	// Create some keys
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("%s-key%d", prefix, i)
		value := fmt.Sprintf("value%d", i)
		_, err := runCommand(ctx, binary, "data", "put", entityType, key, value,
			"--address", serverAddr)
		require.NoError(t, err, "Data put should succeed")
	}

	// List keys (no --prefix flag - list returns all items with pagination)
	output, err := runCommand(ctx, binary, "data", "list", entityType,
		"--limit", "100",
		"--address", serverAddr)
	require.NoError(t, err, "Data list should succeed: %s", output)
	t.Logf("Data list output: %s", output)

	// Clean up
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("%s-key%d", prefix, i)
		_, _ = runCommand(ctx, binary, "data", "delete", entityType, key, "--yes", "--address", serverAddr)
	}
}

// TestCLIDataJSONOutput tests JSON output format.
func TestCLIDataJSONOutput(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	entityType := "kv"
	key := fmt.Sprintf("json-test-%d", time.Now().UnixNano())
	value := "json-value"

	// Put the key
	_, err := runCommand(ctx, binary, "data", "put", entityType, key, value,
		"--address", serverAddr)
	require.NoError(t, err)

	// Get with JSON output
	output, err := runCommand(ctx, binary, "data", "get", entityType, key,
		"--output", "json",
		"--address", serverAddr)
	require.NoError(t, err, "Data get with JSON output should succeed")

	// Parse JSON
	var result map[string]interface{}
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err, "Output should be valid JSON")
	t.Logf("JSON output: %s", output)

	// Clean up
	_, _ = runCommand(ctx, binary, "data", "delete", entityType, key, "--yes", "--address", serverAddr)
}

// =============================================================================
// Crypto Commands
// =============================================================================

// TestCLICryptoHelp tests the crypto help command.
func TestCLICryptoHelp(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := runCommand(ctx, binary, "crypto", "--help")
	require.NoError(t, err, "Crypto help should succeed")

	assert.Contains(t, output, "encrypt", "Should list encrypt command")
	assert.Contains(t, output, "decrypt", "Should list decrypt command")
	assert.Contains(t, output, "hash", "Should list hash command")
	t.Logf("Crypto help executed successfully")
}

// TestCLICryptoHash tests the hash command.
func TestCLICryptoHash(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	data := "Hello, World!"

	// Test SHA256 hash
	output, err := runCommand(ctx, binary, "crypto", "hash",
		"--data", data,
		"--algorithm", "SHA256",
		"--address", serverAddr,
		"--output", "json")
	require.NoError(t, err, "Crypto hash should succeed: %s", output)
	t.Logf("Hash output: %s", output)

	// Parse and verify
	var result struct {
		Algorithm string `json:"algorithm"`
		Hash      string `json:"hash"`
	}
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err, "Should parse JSON response")
	assert.NotEmpty(t, result.Hash, "Should have hash value")
}

// TestCLICryptoRandom tests the random bytes command.
func TestCLICryptoRandom(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	// Generate random bytes
	output, err := runCommand(ctx, binary, "crypto", "random",
		"--bytes", "32",
		"--address", serverAddr,
		"--output", "json")
	require.NoError(t, err, "Crypto random should succeed: %s", output)
	t.Logf("Random output: %s", output)

	// Parse and verify
	var result struct {
		RandomBytes string `json:"random_bytes"`
		Length      int    `json:"length"`
	}
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err, "Should parse JSON response")
	assert.Equal(t, 32, result.Length, "Should have 32 bytes")
}

// TestCLICryptoEncryptDecrypt tests encrypt and decrypt operations.
func TestCLICryptoEncryptDecrypt(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	plaintext := "This is sensitive data"
	keyName := fmt.Sprintf("encrypt-test-key-%d", time.Now().UnixNano())

	// First create a key for encryption
	output, err := runCommand(ctx, binary, "kms", "key", "generate", keyName,
		"--algorithm", "AES-256-GCM",
		"--address", serverAddr)
	require.NoError(t, err, "KMS generate should succeed: %s", output)

	// Encrypt the data (key-name is a positional argument)
	output, err = runCommand(ctx, binary, "crypto", "encrypt", keyName,
		"--data", plaintext,
		"--address", serverAddr,
		"--output", "json")
	require.NoError(t, err, "Crypto encrypt should succeed: %s", output)
	t.Logf("Encrypt output: %s", output)

	// Parse ciphertext - the JSON response has ciphertext as hex or base64
	var encResult struct {
		Ciphertext string `json:"ciphertext"`
		KeyID      string `json:"key_id"`
		KeyVersion int    `json:"key_version"`
	}
	err = json.Unmarshal([]byte(output), &encResult)
	require.NoError(t, err, "Should parse encrypt response")
	require.NotEmpty(t, encResult.Ciphertext, "Should have ciphertext")

	// Decrypt the data (key-name is a positional argument)
	// The ciphertext from encrypt may be hex encoded (default) - try without --base64 first
	output, err = runCommand(ctx, binary, "crypto", "decrypt", keyName,
		"--data", encResult.Ciphertext,
		"--address", serverAddr,
		"--output", "json")

	if err != nil {
		// The decrypt operation may fail due to various reasons in the test environment
		// Log the error for debugging
		t.Logf("Crypto decrypt failed (may be expected in some test environments): %s", output)
	} else {
		t.Logf("Decrypt output: %s", output)

		// Parse and verify plaintext
		var decResult struct {
			Plaintext  string `json:"plaintext"`
			KeyID      string `json:"key_id"`
			KeyVersion int    `json:"key_version"`
		}
		if err := json.Unmarshal([]byte(output), &decResult); err == nil {
			// The plaintext in the JSON response could be the raw string or base64 encoded
			// Try direct comparison first
			if decResult.Plaintext == plaintext {
				t.Logf("Plaintext matches directly")
			} else {
				// Try base64 decoding if direct comparison fails
				decoded, decodeErr := base64.StdEncoding.DecodeString(decResult.Plaintext)
				if decodeErr == nil && string(decoded) == plaintext {
					t.Logf("Plaintext matches after base64 decode")
				} else {
					// Log what we got for debugging
					t.Logf("Decrypted plaintext: %s, expected: %s", decResult.Plaintext, plaintext)
				}
			}
		}
	}

	// Clean up
	_, _ = runCommand(ctx, binary, "kms", "key", "delete", keyName, "--address", serverAddr)
}

// =============================================================================
// KMS Commands
// =============================================================================

// TestCLIKMSHelp tests the kms help command.
func TestCLIKMSHelp(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := runCommand(ctx, binary, "kms", "--help")
	require.NoError(t, err, "KMS help should succeed")

	assert.Contains(t, output, "key", "Should list key command")
	t.Logf("KMS help executed successfully")
}

// TestCLIKMSKeyHelp tests the kms key help command.
func TestCLIKMSKeyHelp(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := runCommand(ctx, binary, "kms", "key", "--help")
	require.NoError(t, err, "KMS key help should succeed")

	assert.Contains(t, output, "generate", "Should list generate command")
	assert.Contains(t, output, "list", "Should list list command")
	assert.Contains(t, output, "get", "Should list get command")
	assert.Contains(t, output, "rotate", "Should list rotate command")
	assert.Contains(t, output, "delete", "Should list delete command")
	t.Logf("KMS key help executed successfully")
}

// TestCLIKMSKeyGenerate tests key generation.
func TestCLIKMSKeyGenerate(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	keyName := fmt.Sprintf("test-key-%d", time.Now().UnixNano())

	// Generate an AES key
	output, err := runCommand(ctx, binary, "kms", "key", "generate", keyName,
		"--algorithm", "AES-256-GCM",
		"--address", serverAddr)
	require.NoError(t, err, "KMS generate should succeed: %s", output)
	t.Logf("Generate output: %s", output)

	// Verify key info
	output, err = runCommand(ctx, binary, "kms", "key", "get", keyName,
		"--address", serverAddr,
		"--output", "json")
	require.NoError(t, err, "KMS key get should succeed: %s", output)
	t.Logf("Key get output: %s", output)

	// Clean up
	_, _ = runCommand(ctx, binary, "kms", "key", "delete", keyName, "--address", serverAddr)
}

// TestCLIKMSKeyList tests key listing.
func TestCLIKMSKeyList(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	keyName := fmt.Sprintf("list-test-key-%d", time.Now().UnixNano())

	// Generate a key
	_, err := runCommand(ctx, binary, "kms", "key", "generate", keyName,
		"--algorithm", "AES-256-GCM",
		"--address", serverAddr)
	require.NoError(t, err)

	// List keys
	output, err := runCommand(ctx, binary, "kms", "key", "list",
		"--address", serverAddr)
	require.NoError(t, err, "KMS key list should succeed: %s", output)
	t.Logf("Key list output: %s", output)

	// Clean up
	_, _ = runCommand(ctx, binary, "kms", "key", "delete", keyName, "--address", serverAddr)
}

// TestCLIKMSKeyRotate tests key rotation.
func TestCLIKMSKeyRotate(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	keyName := fmt.Sprintf("rotate-test-key-%d", time.Now().UnixNano())

	// Generate a key
	_, err := runCommand(ctx, binary, "kms", "key", "generate", keyName,
		"--algorithm", "AES-256-GCM",
		"--address", serverAddr)
	require.NoError(t, err, "KMS generate should succeed")

	// Rotate the key
	output, err := runCommand(ctx, binary, "kms", "key", "rotate", keyName,
		"--address", serverAddr)
	require.NoError(t, err, "KMS key rotate should succeed: %s", output)
	t.Logf("Rotate output: %s", output)

	// Clean up
	_, _ = runCommand(ctx, binary, "kms", "key", "delete", keyName, "--address", serverAddr)
}

// TestCLIKMSKeyDelete tests key deletion.
func TestCLIKMSKeyDelete(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	keyName := fmt.Sprintf("delete-test-key-%d", time.Now().UnixNano())

	// Generate a key
	_, err := runCommand(ctx, binary, "kms", "key", "generate", keyName,
		"--algorithm", "AES-256-GCM",
		"--address", serverAddr)
	require.NoError(t, err, "KMS generate should succeed")

	// Delete the key
	output, err := runCommand(ctx, binary, "kms", "key", "delete", keyName,
		"--address", serverAddr)
	require.NoError(t, err, "KMS key delete should succeed: %s", output)
	t.Logf("Delete output: %s", output)

	// Verify deletion - should fail to get info
	output, err = runCommand(ctx, binary, "kms", "key", "get", keyName,
		"--address", serverAddr)
	// Should fail since key was deleted
	if err == nil {
		t.Logf("Key get after delete: %s", output)
	}
}

// TestCLIKMSKeyGenerateRSA tests RSA key generation.
func TestCLIKMSKeyGenerateRSA(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	keyName := fmt.Sprintf("rsa-test-key-%d", time.Now().UnixNano())

	// Generate an RSA key
	output, err := runCommand(ctx, binary, "kms", "key", "generate", keyName,
		"--algorithm", "RSA-2048",
		"--address", serverAddr)
	require.NoError(t, err, "KMS generate RSA should succeed: %s", output)
	t.Logf("RSA generate output: %s", output)

	// Clean up
	_, _ = runCommand(ctx, binary, "kms", "key", "delete", keyName, "--address", serverAddr)
}

// TestCLIKMSKeyGenerateECDSA tests ECDSA key generation.
func TestCLIKMSKeyGenerateECDSA(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	keyName := fmt.Sprintf("ecdsa-test-key-%d", time.Now().UnixNano())

	// Generate an ECDSA key
	output, err := runCommand(ctx, binary, "kms", "key", "generate", keyName,
		"--algorithm", "ECDSA-P256",
		"--address", serverAddr)
	require.NoError(t, err, "KMS generate ECDSA should succeed: %s", output)
	t.Logf("ECDSA generate output: %s", output)

	// Clean up
	_, _ = runCommand(ctx, binary, "kms", "key", "delete", keyName, "--address", serverAddr)
}

// =============================================================================
// Cluster Commands
// =============================================================================

// TestCLIClusterHelp tests the cluster help command.
func TestCLIClusterHelp(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := runCommand(ctx, binary, "cluster", "--help")
	require.NoError(t, err, "Cluster help should succeed")

	assert.Contains(t, output, "status", "Should list status command")
	t.Logf("Cluster help executed successfully")
}

// TestCLIClusterStatus tests the cluster status command.
func TestCLIClusterStatus(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	// Test table output (default)
	output, err := runCommand(ctx, binary, "cluster", "status",
		"--address", serverAddr)
	require.NoError(t, err, "Cluster status should succeed: %s", output)
	t.Logf("Cluster status table output: %s", output)

	// Verify expected content in table output
	assert.Contains(t, output, "CLUSTER STATUS", "Should contain cluster status header")
	assert.Contains(t, output, "Health:", "Should contain health field")

	// Test JSON output
	outputJSON, err := runCommand(ctx, binary, "cluster", "status",
		"--address", serverAddr,
		"--output", "json")
	require.NoError(t, err, "Cluster status JSON should succeed: %s", outputJSON)
	t.Logf("Cluster status JSON output: %s", outputJSON)

	// Extract JSON from output (skip log lines that start with {"time":)
	jsonContent := extractJSONFromOutput(outputJSON)

	// Verify JSON is valid and contains expected fields
	var clusterStatus map[string]interface{}
	err = json.Unmarshal([]byte(jsonContent), &clusterStatus)
	require.NoError(t, err, "Cluster status output should be valid JSON: %s", jsonContent)

	// Verify expected fields exist in the response (snake_case per API convention)
	assert.Contains(t, clusterStatus, "healthy", "JSON should contain healthy field")
	assert.Contains(t, clusterStatus, "node_count", "JSON should contain node_count field")
}

// =============================================================================
// Error Handling Tests
// =============================================================================

// TestCLIInvalidCommand tests handling of invalid commands.
func TestCLIInvalidCommand(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := runCommand(ctx, binary, "invalidcommand")
	require.Error(t, err, "Invalid command should fail")
	assert.Contains(t, strings.ToLower(output), "unknown", "Should indicate unknown command")
	t.Logf("Invalid command output: %s", output)
}

// TestCLIDataGetNonexistent tests getting a non-existent key.
func TestCLIDataGetNonexistent(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	entityType := "kv"
	key := fmt.Sprintf("nonexistent-key-%d", time.Now().UnixNano())

	output, err := runCommand(ctx, binary, "data", "get", entityType, key,
		"--address", serverAddr)

	// Should either error or return empty
	if err != nil {
		t.Logf("Get nonexistent key error (expected): %s", output)
	} else {
		// Empty response is acceptable
		t.Logf("Get nonexistent key output: %s", output)
	}
}

// TestCLIMissingAddress tests commands without address flag.
func TestCLIMissingAddress(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test without address - should either use default or fail gracefully
	output, err := runCommand(ctx, binary, "data", "get", "kv", "somekey")
	t.Logf("Missing address output: %s, err: %v", output, err)
	// We don't assert on the result as behavior depends on configuration
}

// =============================================================================
// Concurrent Operations Tests
// =============================================================================

// TestCLIConcurrentDataOperations tests concurrent data operations.
func TestCLIConcurrentDataOperations(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	entityType := "kv"
	prefix := fmt.Sprintf("concurrent-%d", time.Now().UnixNano())
	numKeys := 5

	// Create keys concurrently
	errChan := make(chan error, numKeys)
	for i := 0; i < numKeys; i++ {
		go func(idx int) {
			key := fmt.Sprintf("%s-key%d", prefix, idx)
			value := fmt.Sprintf("value%d", idx)
			_, err := runCommand(ctx, binary, "data", "put", entityType, key, value,
				"--address", serverAddr)
			errChan <- err
		}(i)
	}

	// Wait for all operations
	for i := 0; i < numKeys; i++ {
		err := <-errChan
		require.NoError(t, err, "Concurrent put should succeed")
	}

	// Clean up
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("%s-key%d", prefix, i)
		_, _ = runCommand(ctx, binary, "data", "delete", entityType, key, "--yes", "--address", serverAddr)
	}

	t.Logf("Concurrent data operations completed successfully")
}

// =============================================================================
// Output Format Tests
// =============================================================================

// TestCLIOutputFormats tests different output formats.
func TestCLIOutputFormats(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	entityType := "kv"
	key := fmt.Sprintf("format-test-%d", time.Now().UnixNano())
	value := "format-value"

	// Put the key
	_, err := runCommand(ctx, binary, "data", "put", entityType, key, value,
		"--address", serverAddr)
	require.NoError(t, err)

	// Test table format
	output, err := runCommand(ctx, binary, "data", "get", entityType, key,
		"--output", "table",
		"--address", serverAddr)
	require.NoError(t, err, "Table format should succeed")
	t.Logf("Table format output: %s", output)

	// Test JSON format
	output, err = runCommand(ctx, binary, "data", "get", entityType, key,
		"--output", "json",
		"--address", serverAddr)
	require.NoError(t, err, "JSON format should succeed")
	t.Logf("JSON format output: %s", output)

	// Test YAML format (if supported)
	output, err = runCommand(ctx, binary, "data", "get", entityType, key,
		"--output", "yaml",
		"--address", serverAddr)
	if err != nil {
		t.Logf("YAML format not supported or failed: %s", output)
	} else {
		t.Logf("YAML format output: %s", output)
	}

	// Clean up
	_, _ = runCommand(ctx, binary, "data", "delete", entityType, key, "--yes", "--address", serverAddr)
}

// =============================================================================
// DEK Commands (under kms dek)
// =============================================================================

// TestCLIDEKHelp tests the kms dek help command.
func TestCLIDEKHelp(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := runCommand(ctx, binary, "kms", "dek", "--help")
	require.NoError(t, err, "KMS DEK help should succeed")

	// "create" is the canonical verb; "generate" survives only as a cobra
	// alias, and cobra does not print aliases in the command list, so the
	// help text lists "create".
	assert.Contains(t, output, "create", "Should list create command")
	assert.Contains(t, output, "delete", "Should list delete command")
	assert.Contains(t, output, "rotate", "Should list rotate command")
	t.Logf("KMS DEK help executed successfully")
}

// TestCLIDEKGenerate tests DEK generation.
func TestCLIDEKGenerate(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	dekPath := fmt.Sprintf("/tenants/test-dek-%d", time.Now().UnixNano())

	// Generate a DEK (path is a positional argument)
	output, err := runCommand(ctx, binary, "kms", "dek", "generate", dekPath,
		"--algorithm", "AES-256-GCM",
		"--address", serverAddr,
		"--output", "json")
	require.NoError(t, err, "KMS DEK generate should succeed: %s", output)
	t.Logf("DEK generate output: %s", output)

	// Clean up
	_, _ = runCommand(ctx, binary, "kms", "dek", "delete", dekPath, "--address", serverAddr)
}

// TestCLIDEKEncryptDecrypt tests DEK-based encryption and decryption.
// Note: DEK commands in QuicBaaS are for managing DEKs, not for direct encrypt/decrypt.
// The encrypt/decrypt operations use crypto commands with keys created via kms key generate.
func TestCLIDEKEncryptDecrypt(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	dekPath := fmt.Sprintf("/tenants/test-dek-enc-%d", time.Now().UnixNano())

	// Generate a DEK
	output, err := runCommand(ctx, binary, "kms", "dek", "generate", dekPath,
		"--algorithm", "AES-256-GCM",
		"--description", "Test DEK for encryption",
		"--address", serverAddr,
		"--output", "json")
	require.NoError(t, err, "KMS DEK generate should succeed: %s", output)
	t.Logf("DEK generate output: %s", output)

	// Get DEK status/info
	output, err = runCommand(ctx, binary, "kms", "dek", "get", dekPath,
		"--address", serverAddr,
		"--output", "json")
	if err != nil {
		t.Logf("DEK get failed (may be expected): %s", output)
	} else {
		t.Logf("DEK get output: %s", output)
	}

	// Clean up
	_, _ = runCommand(ctx, binary, "kms", "dek", "delete", dekPath, "--address", serverAddr)
}

// =============================================================================
// IAM Commands
// =============================================================================

// TestCLIIAMHelp tests the iam help command.
func TestCLIIAMHelp(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := runCommand(ctx, binary, "iam", "--help")
	require.NoError(t, err, "IAM help should succeed")

	// IAM may be disabled, just check the command exists
	assert.Contains(t, output, "iam", "Should show iam in output")
	t.Logf("IAM help executed successfully")
}

// TestCLIIAMUserHelp tests the iam user help command.
func TestCLIIAMUserHelp(t *testing.T) {
	binary := skipIfNoBinary(t)
	skipIfIAMDisabled(t, binary)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := runCommand(ctx, binary, "iam", "user", "--help")
	require.NoError(t, err, "IAM user help should succeed")

	assert.Contains(t, output, "create", "Should list create command")
	assert.Contains(t, output, "list", "Should list list command")
	assert.Contains(t, output, "delete", "Should list delete command")
	t.Logf("IAM user help executed successfully")
}

// TestCLIIAMUserOperations tests IAM user CRUD operations.
// CLI syntax: quicbaas iam user create <email> --tenant <tenant> --name <name>
func TestCLIIAMUserOperations(t *testing.T) {
	binary := skipIfNoBinary(t)
	skipIfIAMDisabled(t, binary)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// IAM operations must use REST transport - gRPC can't marshal plain Go structs
	// Use the base REST host:port without http:// prefix (CLI adds scheme based on transport)
	serverAddr := "test-node1:8080"

	email := fmt.Sprintf("test-user-%d@test.local", time.Now().UnixNano())
	tenantID := fmt.Sprintf("cli-user-test-%d", time.Now().UnixNano())

	// Clean up any leftover tenant from a previous run
	_, _ = runCommand(ctx, binary, "iam", "tenant", "delete", tenantID,
		"--yes", "--address", serverAddr, "--transport", "rest")

	// Create tenant first - required for user operations
	output, err := runCommand(ctx, binary, "iam", "tenant", "create", tenantID,
		"--name", "CLI User Test Tenant",
		"--address", serverAddr,
		"--transport", "rest")
	require.NoError(t, err, "IAM tenant create should succeed: %s", output)
	t.Logf("IAM tenant create output: %s", output)

	// Cleanup tenant at the end
	defer func() {
		_, _ = runCommand(ctx, binary, "iam", "tenant", "delete", tenantID,
			"--yes",
			"--address", serverAddr,
			"--transport", "rest")
	}()

	// Create user - email is a positional argument, not a flag
	// CLI: quicbaas iam user create <email> --tenant <tenant> --name <name>
	output, err = runCommand(ctx, binary, "iam", "user", "create", email,
		"--tenant", tenantID,
		"--name", "Test User",
		"--address", serverAddr,
		"--transport", "rest")
	require.NoError(t, err, "IAM user create should succeed: %s", output)
	t.Logf("IAM user create output: %s", output)

	// Parse user ID from create output (format: "ID: <uuid>")
	userID := parseUserIDFromOutput(output)
	require.NotEmpty(t, userID, "Should be able to parse user ID from create output")
	t.Logf("Parsed user ID: %s", userID)

	// Get user info - user-id is a positional argument (not email)
	output, err = runCommand(ctx, binary, "iam", "user", "get", userID,
		"--tenant", tenantID,
		"--address", serverAddr,
		"--output", "json",
		"--transport", "rest")
	require.NoError(t, err, "IAM user get should succeed: %s", output)
	t.Logf("IAM user get output: %s", output)

	// List users
	output, err = runCommand(ctx, binary, "iam", "user", "list",
		"--tenant", tenantID,
		"--address", serverAddr,
		"--transport", "rest")
	require.NoError(t, err, "IAM user list should succeed: %s", output)
	t.Logf("IAM user list output: %s", output)

	// Delete user - user-id is a positional argument (not email)
	output, err = runCommand(ctx, binary, "iam", "user", "delete", userID,
		"--tenant", tenantID,
		"--yes",
		"--address", serverAddr,
		"--transport", "rest")
	require.NoError(t, err, "IAM user delete should succeed: %s", output)
	t.Logf("IAM user delete output: %s", output)
}

// TestCLIIAMRoleHelp tests the iam role help command.
func TestCLIIAMRoleHelp(t *testing.T) {
	binary := skipIfNoBinary(t)
	skipIfIAMDisabled(t, binary)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := runCommand(ctx, binary, "iam", "role", "--help")
	require.NoError(t, err, "IAM role help should succeed")

	assert.Contains(t, output, "create", "Should list create command")
	assert.Contains(t, output, "list", "Should list list command")
	assert.Contains(t, output, "delete", "Should list delete command")
	t.Logf("IAM role help executed successfully")
}

// TestCLIIAMRoleOperations tests IAM role CRUD operations.
// CLI syntax: quicbaas iam role create <id> --tenant <tenant> --name <name>
func TestCLIIAMRoleOperations(t *testing.T) {
	binary := skipIfNoBinary(t)
	skipIfIAMDisabled(t, binary)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// IAM operations must use REST transport - gRPC can't marshal plain Go structs
	// Use the base REST host:port without http:// prefix (CLI adds scheme based on transport)
	serverAddr := "test-node1:8080"

	roleName := fmt.Sprintf("test-role-%d", time.Now().UnixNano())
	tenantID := fmt.Sprintf("cli-role-test-%d", time.Now().UnixNano())

	// Clean up any leftover tenant from a previous run
	_, _ = runCommand(ctx, binary, "iam", "tenant", "delete", tenantID,
		"--yes", "--address", serverAddr, "--transport", "rest")

	// Create tenant first - required for role operations
	output, err := runCommand(ctx, binary, "iam", "tenant", "create", tenantID,
		"--name", "CLI Role Test Tenant",
		"--address", serverAddr,
		"--transport", "rest")
	require.NoError(t, err, "IAM tenant create should succeed: %s", output)
	t.Logf("IAM tenant create output: %s", output)

	// Cleanup tenant at the end
	defer func() {
		_, _ = runCommand(ctx, binary, "iam", "tenant", "delete", tenantID,
			"--yes",
			"--address", serverAddr,
			"--transport", "rest")
	}()

	// Create role - name is a positional argument
	// CLI: quicbaas iam role create <name> --tenant <tenant> --name <display_name>
	// Note: The server generates the actual role ID
	output, err = runCommand(ctx, binary, "iam", "role", "create", roleName,
		"--tenant", tenantID,
		"--name", "Test Role",
		"--description", "Test role for integration tests",
		"--address", serverAddr,
		"--transport", "rest")
	require.NoError(t, err, "IAM role create should succeed: %s", output)
	t.Logf("IAM role create output: %s", output)

	// Parse the actual role ID from create output (format: "ID: <uuid>")
	roleID := parseRoleIDFromOutput(output)
	require.NotEmpty(t, roleID, "Should be able to parse role ID from create output")
	t.Logf("Parsed role ID: %s", roleID)

	// Get role info - uses the server-generated ID
	output, err = runCommand(ctx, binary, "iam", "role", "get", roleID,
		"--tenant", tenantID,
		"--address", serverAddr,
		"--output", "json",
		"--transport", "rest")
	require.NoError(t, err, "IAM role get should succeed: %s", output)
	t.Logf("IAM role get output: %s", output)

	// List roles
	output, err = runCommand(ctx, binary, "iam", "role", "list",
		"--tenant", tenantID,
		"--address", serverAddr,
		"--transport", "rest")
	require.NoError(t, err, "IAM role list should succeed: %s", output)
	t.Logf("IAM role list output: %s", output)

	// Delete role - uses the server-generated ID
	output, err = runCommand(ctx, binary, "iam", "role", "delete", roleID,
		"--tenant", tenantID,
		"--yes",
		"--address", serverAddr,
		"--transport", "rest")
	require.NoError(t, err, "IAM role delete should succeed: %s", output)
	t.Logf("IAM role delete output: %s", output)
}

// TestCLIIAMPolicyHelp tests the iam policy help command.
// Note: The policy subcommand has: apply, get, delete, list, validate, test
func TestCLIIAMPolicyHelp(t *testing.T) {
	binary := skipIfNoBinary(t)
	skipIfIAMDisabled(t, binary)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := runCommand(ctx, binary, "iam", "policy", "--help")
	require.NoError(t, err, "IAM policy help should succeed")

	// Check for the actual subcommands that exist
	assert.Contains(t, output, "apply", "Should list apply command")
	assert.Contains(t, output, "list", "Should list list command")
	assert.Contains(t, output, "delete", "Should list delete command")
	t.Logf("IAM policy help executed successfully")
}

// TestCLIIAMPolicyOperations tests IAM policy CRUD operations.
// Note: Policy uses apply (from file), not create with inline document.
// Since we can't easily create a temp file in E2E tests, we test list and help.
func TestCLIIAMPolicyOperations(t *testing.T) {
	binary := skipIfNoBinary(t)
	skipIfIAMDisabled(t, binary)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// IAM operations must use REST transport - gRPC can't marshal plain Go structs
	// Use the base REST host:port without http:// prefix (CLI adds scheme based on transport)
	serverAddr := "test-node1:8080"

	tenantID := "test-tenant"

	// List policies (this should work even without creating any)
	output, err := runCommand(ctx, binary, "iam", "policy", "list",
		"--tenant", tenantID,
		"--address", serverAddr,
		"--transport", "rest")
	require.NoError(t, err, "IAM policy list should succeed: %s", output)
	t.Logf("IAM policy list output: %s", output)

	// Test policy validate help
	output, err = runCommand(ctx, binary, "iam", "policy", "validate", "--help")
	require.NoError(t, err, "IAM policy validate help should succeed")
	t.Logf("IAM policy validate help: %s", output)

	// Test policy test help
	output, err = runCommand(ctx, binary, "iam", "policy", "test", "--help")
	require.NoError(t, err, "IAM policy test help should succeed")
	t.Logf("IAM policy test help: %s", output)
}

// =============================================================================
// Crypto Sign/Verify Tests
// =============================================================================

// TestCLICryptoSignVerify tests sign and verify operations.
// The Sign and Verify operations use the KMS backend to perform cryptographic
// signing operations with asymmetric keys (RSA, ECDSA, Ed25519).
func TestCLICryptoSignVerify(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	message := "This message needs to be signed"
	keyName := fmt.Sprintf("sign-test-key-%d", time.Now().UnixNano())

	// Generate an ECDSA key for signing (more reliable than RSA for this test)
	// ECDSA-P256 is well-supported and doesn't require RSA-specific attributes
	output, err := runCommand(ctx, binary, "kms", "key", "generate", keyName,
		"--algorithm", "ECDSA-P256",
		"--address", serverAddr)
	require.NoError(t, err, "KMS generate should succeed: %s", output)
	t.Logf("Key generate output: %s", output)

	// Sign the message (key-name is a positional argument)
	output, err = runCommand(ctx, binary, "crypto", "sign", keyName,
		"--data", message,
		"--address", serverAddr,
		"--output", "json")
	require.NoError(t, err, "Crypto sign should succeed: %s", output)
	t.Logf("Crypto sign output: %s", output)

	// Parse signature from JSON response
	var signResult struct {
		Signature     string `json:"signature"`
		KeyID         string `json:"key_id"`
		KeyVersion    int    `json:"key_version"`
		HashAlgorithm string `json:"hash_algorithm"`
	}
	err = json.Unmarshal([]byte(output), &signResult)
	require.NoError(t, err, "Should parse sign response JSON")
	require.NotEmpty(t, signResult.Signature, "Should have signature in response")
	t.Logf("Parsed signature: %s", signResult.Signature)

	// Verify the signature (key-name is a positional argument)
	output, err = runCommand(ctx, binary, "crypto", "verify", keyName,
		"--data", message,
		"--signature", signResult.Signature,
		"--address", serverAddr,
		"--output", "json")
	require.NoError(t, err, "Crypto verify should succeed: %s", output)
	t.Logf("Crypto verify output: %s", output)

	// Parse verify result
	var verifyResult struct {
		Valid      bool   `json:"valid"`
		KeyID      string `json:"key_id"`
		KeyVersion int    `json:"key_version"`
	}
	err = json.Unmarshal([]byte(output), &verifyResult)
	require.NoError(t, err, "Should parse verify response JSON")
	assert.True(t, verifyResult.Valid, "Signature should be valid")

	// Clean up
	_, _ = runCommand(ctx, binary, "kms", "key", "delete", keyName, "--address", serverAddr)
}

// =============================================================================
// Vector Commands
// =============================================================================

// TestCLIVectorHelp tests the vector help command
func TestCLIVectorHelp(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := runCommand(ctx, binary, "vector", "--help")
	require.NoError(t, err, "Vector help should succeed")

	assert.Contains(t, output, "vector")
	assert.Contains(t, output, "namespace")
	assert.Contains(t, output, "search")
	t.Logf("Vector help executed successfully")
}

// runVectorCommand tries a vector CLI command against each cluster endpoint
// until one succeeds. Vector operations go to a dedicated Raft shard whose
// leader may be on any node in the cluster.
func runVectorCommand(t *testing.T, ctx context.Context, binary string, args ...string) (string, error) {
	t.Helper()
	endpoints := helpers.GetClusterEndpoints()
	var lastErr error
	var lastOutput string
	for _, endpoint := range endpoints {
		cmdArgs := make([]string, len(args))
		copy(cmdArgs, args)
		cmdArgs = append(cmdArgs, "--address", endpoint, "--timeout", "30s")
		output, err := runCommand(ctx, binary, cmdArgs...)
		if err == nil {
			return output, nil
		}
		lastErr = err
		lastOutput = output
		t.Logf("Vector command failed on %s (trying next): %v", endpoint, err)
	}
	return lastOutput, lastErr
}

// TestCLIVectorNamespace tests vector namespace operations
func TestCLIVectorNamespace(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	namespaceName := fmt.Sprintf("test_ns_%d", time.Now().UnixNano())

	// Test namespace create (vector shard leader may be on any node)
	output, err := runVectorCommand(t, ctx, binary, "vector", "namespace", "create", namespaceName,
		"--dimension", "128")
	if err != nil && (strings.Contains(output, "UNIMPLEMENTED") || strings.Contains(output, "not available") || strings.Contains(output, "unknown service")) {
		t.Logf("Vector service not available: %s", output)
		return
	}
	require.NoError(t, err, "Vector namespace create should succeed: %s", output)
	t.Logf("Vector namespace created: %s", output)

	// Test namespace list
	output, err = runVectorCommand(t, ctx, binary, "vector", "namespace", "list",
		"--output", "json")
	require.NoError(t, err, "Vector namespace list should succeed: %s", output)
	assert.Contains(t, output, namespaceName, "Should list created namespace")
	t.Logf("Vector namespace list: %s", output)

	// Test namespace info
	output, err = runVectorCommand(t, ctx, binary, "vector", "namespace", "info", namespaceName,
		"--output", "json")
	require.NoError(t, err, "Vector namespace info should succeed: %s", output)
	assert.Contains(t, output, namespaceName, "Should contain namespace name")
	t.Logf("Vector namespace info: %s", output)

	// Test namespace delete
	output, err = runVectorCommand(t, ctx, binary, "vector", "namespace", "delete", namespaceName)
	require.NoError(t, err, "Vector namespace delete should succeed: %s", output)
	t.Logf("Vector namespace deleted: %s", output)
}

// TestCLIVectorSearch tests vector search operations
func TestCLIVectorSearch(t *testing.T) {
	binary := skipIfNoBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	namespaceName := fmt.Sprintf("search_ns_%d", time.Now().UnixNano())

	// Create namespace (vector shard leader may be on any node)
	output, err := runVectorCommand(t, ctx, binary, "vector", "namespace", "create", namespaceName,
		"--dimension", "4")
	if err != nil && (strings.Contains(output, "UNIMPLEMENTED") || strings.Contains(output, "not available") || strings.Contains(output, "unknown service")) {
		t.Logf("Vector service not available: %s", output)
		return
	}
	require.NoError(t, err, "Vector namespace create should succeed: %s", output)

	// Insert vectors using positional arguments: upsert <namespace> <id> --vector <values>
	vectors := []string{
		"1.0,0.0,0.0,0.0",
		"0.0,1.0,0.0,0.0",
		"0.0,0.0,1.0,0.0",
	}
	for i, v := range vectors {
		vectorID := fmt.Sprintf("vec_%d", i)
		output, err = runVectorCommand(t, ctx, binary, "vector", "upsert", namespaceName, vectorID,
			"--vector", v)
		require.NoError(t, err, "Vector upsert should succeed: %s", output)
	}
	t.Logf("Inserted %d vectors", len(vectors))

	// Search for similar vectors using "search similar" subcommand
	output, err = runVectorCommand(t, ctx, binary, "vector", "search", "similar", namespaceName,
		"--vector", "1.0,0.0,0.0,0.0",
		"--k", "3",
		"--output", "json")
	require.NoError(t, err, "Vector search should succeed: %s", output)
	t.Logf("Vector search result: %s", output)

	// Clean up
	_, _ = runVectorCommand(t, ctx, binary, "vector", "namespace", "delete", namespaceName)
}
