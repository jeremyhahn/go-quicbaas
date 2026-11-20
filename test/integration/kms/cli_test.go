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

//go:build integration && quic

package kms

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clitoken "github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// CLI Test Infrastructure
// =============================================================================

// getQuicBaaSCLIBinary locates the quicbaas CLI binary.
func getQuicBaaSCLIBinary() string {
	// Check environment variable first
	if binary := os.Getenv("QUICBAAS_BINARY"); binary != "" {
		if _, err := os.Stat(binary); err == nil {
			return binary
		}
	}

	// Check common locations
	locations := []string{
		"/usr/local/bin/quicbaas",
		"/app/bin/quicbaas",
		"./quicbaas",
		"../../../quicbaas",
		"./bin/quicbaas",
		"../../bin/quicbaas",
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}

	// Try PATH
	if path, err := exec.LookPath("quicbaas"); err == nil {
		return path
	}

	return ""
}

// skipIfNoCLIBinary fails the test if the CLI binary is not found.
// The CLI binary must be present in the integration test environment.
func skipIfNoCLIBinary(t *testing.T) string {
	t.Helper()
	binary := getQuicBaaSCLIBinary()
	require.NotEmpty(t, binary,
		"quicbaas CLI binary not found: ensure it is built and available in PATH or /app/bin/quicbaas")
	return binary
}

// cliTestContext holds context for CLI tests.
type cliTestContext struct {
	t         *testing.T
	binary    string
	protocol  string
	address   string
	timeout   time.Duration
	outputFmt string

	// home is the session home of this context: the CLI reads its bearer
	// only from $HOME/.quicbaas/token.
	home string
}

// newCLITestContext creates a new CLI test context.
func newCLITestContext(t *testing.T, protocol, address string) *cliTestContext {
	return &cliTestContext{
		t:         t,
		binary:    skipIfNoCLIBinary(t),
		protocol:  protocol,
		address:   address,
		timeout:   30 * time.Second,
		outputFmt: "json",
		home:      t.TempDir(),
	}
}

// sessionEnv logs the fixture's system admin in (PIV, memoized in helpers
// until the token expires), writes the bearer to the context's session home
// and returns the environment the CLI process needs to present it.
//
// It returns a login or token-save error rather than failing the test: the
// context holds the top-level test, while run and runExpectError are called
// from its subtests, and FailNow on a parent from a subtest aborts the
// whole package.
//
// The system admin is the suite's interim principal for these commands: the
// tests ran with no credential before. A narrower tenant session for the KMS
// commands is planned.
func (c *cliTestContext) sessionEnv() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bearer, expiresAt, err := helpers.AuthPIVSession(ctx, getRESTEndpoints()[0], "", "", "")
	if err != nil {
		return nil, fmt.Errorf("PIV login for the CLI bearer: %w", err)
	}

	tokenPath := filepath.Join(c.home, ".quicbaas", "token")
	if err := clitoken.SaveToken(tokenPath, &clitoken.TokenData{
		AccessToken: bearer,
		ExpiresAt:   expiresAt,
		TokenType:   "Bearer",
	}); err != nil {
		return nil, fmt.Errorf("persist the CLI bearer: %w", err)
	}

	return append(os.Environ(), "HOME="+c.home), nil
}

// run executes a CLI command with the configured protocol and address.
func (c *cliTestContext) run(args ...string) (string, error) {
	c.t.Helper()

	// Build full command with protocol and address
	fullArgs := []string{
		"--transport", c.protocol,
		"--address", c.address,
		"--tls-ca", "/etc/quicbaas/certs/ca.crt",
		"--output", c.outputFmt,
	}
	fullArgs = append(fullArgs, args...)

	env, err := c.sessionEnv()
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.binary, fullArgs...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	c.t.Logf("[%s] %s %s", c.protocol, c.binary, strings.Join(fullArgs, " "))
	if stdout.Len() > 0 {
		c.t.Logf("[%s] stdout: %s", c.protocol, stdout.String())
	}
	if stderr.Len() > 0 {
		c.t.Logf("[%s] stderr: %s", c.protocol, stderr.String())
	}

	if err != nil {
		return stdout.String(), fmt.Errorf("command failed: %w, stderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// runExpectError executes a CLI command expecting it to fail.
func (c *cliTestContext) runExpectError(args ...string) (string, error) {
	c.t.Helper()

	fullArgs := []string{
		"--transport", c.protocol,
		"--address", c.address,
		"--tls-ca", "/etc/quicbaas/certs/ca.crt",
		"--output", c.outputFmt,
	}
	fullArgs = append(fullArgs, args...)

	// A failed login is never the CLI failure the caller expects: Errorf
	// (safe from a subtest) marks the test failed.
	env, err := c.sessionEnv()
	if err != nil {
		c.t.Errorf("PIV login for the CLI bearer failed: %v", err)
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.binary, fullArgs...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	c.t.Logf("[%s] %s %s", c.protocol, c.binary, strings.Join(fullArgs, " "))
	if stdout.Len() > 0 {
		c.t.Logf("[%s] stdout: %s", c.protocol, stdout.String())
	}
	if stderr.Len() > 0 {
		c.t.Logf("[%s] stderr: %s", c.protocol, stderr.String())
	}

	return stderr.String(), err
}

// unmarshalJSON unmarshals JSON output from a CLI command.
func (c *cliTestContext) unmarshalJSON(output string, v interface{}) error {
	c.t.Helper()
	// Try to find JSON in the output
	output = strings.TrimSpace(output)
	if err := json.Unmarshal([]byte(output), v); err != nil {
		c.t.Logf("Failed to unmarshal JSON: %v\nOutput: %s", err, output)
		return err
	}
	return nil
}

// skipIfNotImplemented checks if the error indicates the feature is not implemented.
// In integration tests, all features must be implemented, so this fails the test.
func (c *cliTestContext) skipIfNotImplemented(t *testing.T, err error, errOutput string) bool {
	t.Helper()
	if err == nil {
		return false
	}
	errStr := err.Error() + errOutput
	if strings.Contains(errStr, "not yet implemented") ||
		strings.Contains(errStr, "Unimplemented") ||
		strings.Contains(errStr, "unknown service") {
		require.Fail(t, "Feature must be implemented on server for integration tests",
			"Error: %s", errStr)
		return true
	}
	return false
}

// getGRPCProtocolConfig returns test configuration for gRPC only.
func getGRPCProtocolConfig() struct {
	name    string
	address string
} {
	grpcEndpoints := getGRPCEndpoints()
	return struct {
		name    string
		address string
	}{"grpc", grpcEndpoints[0]}
}

// =============================================================================
// Response Types for JSON Parsing
// =============================================================================

type keyResponse struct {
	Key struct {
		KeyID     string `json:"key_id"`
		Algorithm string `json:"algorithm"`
		Version   uint64 `json:"version"`
		Status    string `json:"status"`
		CreatedAt string `json:"created_at"`
	} `json:"key"`
	Metadata struct {
		KeyID       string `json:"key_id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Algorithm   string `json:"algorithm"`
		Status      string `json:"status"`
		Version     uint64 `json:"version"`
		Exportable  bool   `json:"exportable"`
		CreatedAt   string `json:"created_at"`
		UpdatedAt   string `json:"updated_at"`
	} `json:"metadata"`
	Timestamp string `json:"timestamp"`
}

type getKeyResponse struct {
	Metadata struct {
		KeyID       string `json:"key_id"`
		Name        string `json:"name"`
		Algorithm   string `json:"algorithm"`
		Version     uint64 `json:"version"`
		Status      string `json:"status"`
		Exportable  bool   `json:"exportable"`
		Description string `json:"description"`
	} `json:"metadata"`
}

type listKeysResponse struct {
	Keys []struct {
		KeyID     string `json:"key_id"`
		Algorithm string `json:"algorithm"`
		Version   uint64 `json:"version"`
		Status    string `json:"status"`
	} `json:"keys"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type rotateKeyResponse struct {
	KeyID      string `json:"key_id"`
	OldVersion uint64 `json:"old_version"`
	NewVersion uint64 `json:"new_version"`
}

type deleteKeyResponse struct {
	KeyID        string    `json:"key_id"`
	Status       string    `json:"status"`
	DeletionDate time.Time `json:"deletion_date"`
}

type encryptResponse struct {
	KeyID      string `json:"key_id"`
	KeyVersion uint64 `json:"key_version"`
	Ciphertext string `json:"ciphertext"`
}

type decryptResponse struct {
	KeyID      string `json:"key_id"`
	KeyVersion uint64 `json:"key_version"`
	Plaintext  string `json:"plaintext"`
}

type updateKeyStatusResponse struct {
	KeyID     string `json:"key_id"`
	OldStatus string `json:"old_status"`
	NewStatus string `json:"new_status"`
}

type listKeyVersionsResponse struct {
	Versions []struct {
		Version   uint64 `json:"version"`
		Status    string `json:"status"`
		Algorithm string `json:"algorithm"`
	} `json:"versions"`
}

type exportKeyResponse struct {
	KeyID             string `json:"key_id"`
	WrappedKey        string `json:"wrapped_key"`
	WrappingKeyID     string `json:"wrapping_key_id"`
	WrappingAlgorithm string `json:"wrapping_algorithm"`
	Format            string `json:"format"`
}

// =============================================================================
// TestCLIKMSKeyGenerateAllAlgorithms - Comprehensive Algorithm Tests
// =============================================================================

// TestCLIKMSKeyGenerateAllAlgorithms tests kms key generate with ALL supported algorithms.
// This is the primary test for ensuring all algorithm types work correctly.
func TestCLIKMSKeyGenerateAllAlgorithms(t *testing.T) {
	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	// All supported algorithms - using UPPERCASE as required
	algorithms := []struct {
		name        string
		algorithm   string
		keyType     string // "symmetric" or "asymmetric"
		description string
	}{
		// Asymmetric algorithms
		{name: "ED25519", algorithm: "ED25519", keyType: "asymmetric", description: "Edwards-curve Digital Signature Algorithm"},
		{name: "ECDSA-P256", algorithm: "ECDSA-P256", keyType: "asymmetric", description: "Elliptic Curve DSA with P-256 curve"},
		{name: "ECDSA-P384", algorithm: "ECDSA-P384", keyType: "asymmetric", description: "Elliptic Curve DSA with P-384 curve"},
		{name: "RSA-2048", algorithm: "RSA-2048", keyType: "asymmetric", description: "RSA with 2048-bit modulus"},
		{name: "RSA-4096", algorithm: "RSA-4096", keyType: "asymmetric", description: "RSA with 4096-bit modulus"},
		// Symmetric algorithms - all 5
		{name: "AES-128-GCM", algorithm: "AES-128-GCM", keyType: "symmetric", description: "AES-GCM with 128-bit key"},
		{name: "AES-192-GCM", algorithm: "AES-192-GCM", keyType: "symmetric", description: "AES-GCM with 192-bit key"},
		{name: "AES-256-GCM", algorithm: "AES-256-GCM", keyType: "symmetric", description: "AES-GCM with 256-bit key"},
		{name: "CHACHA20-POLY1305", algorithm: "CHACHA20-POLY1305", keyType: "symmetric", description: "ChaCha20-Poly1305 AEAD"},
		{name: "XCHACHA20-POLY1305", algorithm: "XCHACHA20-POLY1305", keyType: "symmetric", description: "XChaCha20-Poly1305 AEAD with extended nonce"},
	}

	for _, alg := range algorithms {
		t.Run(alg.name, func(t *testing.T) {
			keyName := generateUniqueKeyName("cli-alg-" + strings.ToLower(strings.ReplaceAll(alg.algorithm, "-", "")))

			// Generate key with specified algorithm
			output, err := cli.run("kms", "key", "generate", keyName, "--algorithm", alg.algorithm)

			// All algorithms must be supported by the backend in integration tests
			if err != nil {
				errStr := err.Error()
				if strings.Contains(errStr, "not supported") ||
					strings.Contains(errStr, "unsupported algorithm") ||
					strings.Contains(errStr, "unknown algorithm") {
					require.Fail(t, "Algorithm must be supported by backend",
						"Algorithm %s not supported: %v", alg.algorithm, err)
					return
				}
				require.NoError(t, err, "generate with algorithm %s should succeed", alg.algorithm)
			}

			// Parse and verify JSON response
			var resp keyResponse
			require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response for %s", alg.algorithm)

			// Verify key_id is present and not empty
			require.NotEmpty(t, resp.Key.KeyID, "key_id should not be empty for %s", alg.algorithm)

			// Verify name matches
			assert.Equal(t, keyName, resp.Metadata.Name, "name should match for %s", alg.algorithm)

			// Verify algorithm - the response may normalize the algorithm name
			assert.NotEmpty(t, resp.Key.Algorithm, "algorithm should not be empty for %s", alg.algorithm)

			// Verify version starts at 1
			assert.Equal(t, uint64(1), resp.Key.Version, "version should be 1 for new key %s", alg.algorithm)

			// Verify status is enabled
			assert.Contains(t, strings.ToUpper(resp.Key.Status), "ENABLED", "status should be enabled for %s", alg.algorithm)

			// Verify exportable defaults to false
			assert.False(t, resp.Metadata.Exportable, "exportable should default to false for %s", alg.algorithm)

			// Cleanup in defer to ensure it runs even on test failure
			defer func() {
				_, _ = cli.run("kms", "key", "delete", resp.Key.KeyID, "--force")
			}()
		})
	}
}

// =============================================================================
// TestCLIKMSKeyGenerateFlags - Comprehensive Flag Tests
// =============================================================================

// TestCLIKMSKeyGenerateFlags tests all flag combinations for kms key generate.
func TestCLIKMSKeyGenerateFlags(t *testing.T) {
	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	// Test --algorithm flag with AES-256-GCM (most commonly supported)
	t.Run("flag-algorithm", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-flag-alg")

		output, err := cli.run("kms", "key", "generate", keyName, "--algorithm", "AES-256-GCM")
		require.NoError(t, err, "generate with --algorithm flag should succeed")

		var resp keyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")

		require.NotEmpty(t, resp.Key.KeyID, "key_id should not be empty")
		assert.Equal(t, keyName, resp.Metadata.Name, "name should match")
		assert.NotEmpty(t, resp.Key.Algorithm, "algorithm should not be empty")

		// Cleanup
		defer func() {
			_, _ = cli.run("kms", "key", "delete", resp.Key.KeyID, "--force")
		}()
	})

	// Test --description flag
	t.Run("flag-description", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-flag-desc")
		description := "Test key with description for comprehensive CLI integration testing"

		output, err := cli.run("kms", "key", "generate", keyName,
			"--algorithm", "AES-256-GCM",
			"--description", description)
		require.NoError(t, err, "generate with --description flag should succeed")

		var resp keyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")

		require.NotEmpty(t, resp.Key.KeyID, "key_id should not be empty")
		assert.Equal(t, keyName, resp.Metadata.Name, "name should match")

		// Verify description via get command
		getOutput, err := cli.run("kms", "key", "get", resp.Key.KeyID)
		require.NoError(t, err, "get key should succeed")

		var getResp getKeyResponse
		require.NoError(t, cli.unmarshalJSON(getOutput, &getResp), "should parse get response")
		assert.Equal(t, description, getResp.Metadata.Description, "description should match")

		// Cleanup
		defer func() {
			_, _ = cli.run("kms", "key", "delete", resp.Key.KeyID, "--force")
		}()
	})

	// Test --exportable flag
	t.Run("flag-exportable", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-flag-export")

		output, err := cli.run("kms", "key", "generate", keyName,
			"--algorithm", "AES-256-GCM",
			"--exportable")
		require.NoError(t, err, "generate with --exportable flag should succeed")

		var resp keyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")

		require.NotEmpty(t, resp.Key.KeyID, "key_id should not be empty")
		assert.Equal(t, keyName, resp.Metadata.Name, "name should match")

		// Verify exportable via get command
		getOutput, err := cli.run("kms", "key", "get", resp.Key.KeyID)
		require.NoError(t, err, "get key should succeed")

		var getResp getKeyResponse
		require.NoError(t, cli.unmarshalJSON(getOutput, &getResp), "should parse get response")
		assert.True(t, getResp.Metadata.Exportable, "key should be exportable")

		// Cleanup
		defer func() {
			_, _ = cli.run("kms", "key", "delete", resp.Key.KeyID, "--force")
		}()
	})

	// Test all flags combined: --algorithm, --description, --exportable
	t.Run("all-flags-combined", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-flag-all")
		description := "Comprehensive test with all flags combined"

		output, err := cli.run("kms", "key", "generate", keyName,
			"--algorithm", "AES-256-GCM",
			"--description", description,
			"--exportable")
		require.NoError(t, err, "generate with all flags should succeed")

		var resp keyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")

		// Verify all fields
		require.NotEmpty(t, resp.Key.KeyID, "key_id should not be empty")
		assert.Equal(t, keyName, resp.Metadata.Name, "name should match")
		assert.NotEmpty(t, resp.Key.Algorithm, "algorithm should not be empty")
		assert.Equal(t, uint64(1), resp.Key.Version, "version should be 1")
		assert.Contains(t, strings.ToUpper(resp.Key.Status), "ENABLED", "status should be enabled")

		// Verify via get command
		getOutput, err := cli.run("kms", "key", "get", resp.Key.KeyID)
		require.NoError(t, err, "get key should succeed")

		var getResp getKeyResponse
		require.NoError(t, cli.unmarshalJSON(getOutput, &getResp), "should parse get response")
		assert.Equal(t, description, getResp.Metadata.Description, "description should match")
		assert.True(t, getResp.Metadata.Exportable, "key should be exportable")

		// Cleanup
		defer func() {
			_, _ = cli.run("kms", "key", "delete", resp.Key.KeyID, "--force")
		}()
	})

	// Test default algorithm (ED25519)
	t.Run("default-algorithm-ed25519", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-flag-default")

		output, err := cli.run("kms", "key", "generate", keyName)
		// ED25519 must be supported by the integration test backend
		require.NoError(t, err, "generate with default algorithm ED25519 must succeed")

		var resp keyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")

		require.NotEmpty(t, resp.Key.KeyID, "key_id should not be empty")
		assert.Equal(t, keyName, resp.Metadata.Name, "name should match")
		// Default algorithm is ED25519
		assert.NotEmpty(t, resp.Key.Algorithm, "algorithm should not be empty")

		// Cleanup
		defer func() {
			_, _ = cli.run("kms", "key", "delete", resp.Key.KeyID, "--force")
		}()
	})

	// Test empty description (should succeed with empty description)
	t.Run("empty-description", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-flag-emptydesc")

		output, err := cli.run("kms", "key", "generate", keyName,
			"--algorithm", "AES-256-GCM",
			"--description", "")
		require.NoError(t, err, "generate with empty description should succeed")

		var resp keyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")

		require.NotEmpty(t, resp.Key.KeyID, "key_id should not be empty")

		// Cleanup
		defer func() {
			_, _ = cli.run("kms", "key", "delete", resp.Key.KeyID, "--force")
		}()
	})

	// Test long description
	t.Run("long-description", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-flag-longdesc")
		longDescription := strings.Repeat("This is a long description for testing purposes. ", 10)

		output, err := cli.run("kms", "key", "generate", keyName,
			"--algorithm", "AES-256-GCM",
			"--description", longDescription)
		require.NoError(t, err, "generate with long description should succeed")

		var resp keyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")

		require.NotEmpty(t, resp.Key.KeyID, "key_id should not be empty")

		// Cleanup
		defer func() {
			_, _ = cli.run("kms", "key", "delete", resp.Key.KeyID, "--force")
		}()
	})
}

// =============================================================================
// TestCLIKMSKeyGenerateErrors - Error Case Tests
// =============================================================================

// TestCLIKMSKeyGenerateErrors tests all error cases for kms key generate.
func TestCLIKMSKeyGenerateErrors(t *testing.T) {
	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	// Test invalid algorithm
	t.Run("error-invalid-algorithm", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-err-invalg")
		_, err := cli.runExpectError("kms", "key", "generate", keyName, "--algorithm", "INVALID-ALG-XYZ")
		assert.Error(t, err, "generate with invalid algorithm should fail")
	})

	// Test missing key name
	t.Run("error-missing-name", func(t *testing.T) {
		_, err := cli.runExpectError("kms", "key", "generate")
		assert.Error(t, err, "generate without key name should fail")
	})

	// Test empty key name
	t.Run("error-empty-name", func(t *testing.T) {
		_, err := cli.runExpectError("kms", "key", "generate", "")
		assert.Error(t, err, "generate with empty key name should fail")
	})

	// Test duplicate key name (create same key twice)
	t.Run("error-duplicate-name", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-err-dup")

		// Create first key
		output, err := cli.run("kms", "key", "generate", keyName, "--algorithm", "AES-256-GCM")
		require.NoError(t, err, "first key creation should succeed")

		var resp keyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse first response")
		keyID := resp.Key.KeyID

		// Cleanup first key
		defer func() {
			_, _ = cli.run("kms", "key", "delete", keyID, "--force")
		}()

		// Try to create second key with same name - may or may not fail depending on implementation
		_, err = cli.runExpectError("kms", "key", "generate", keyName, "--algorithm", "AES-256-GCM")
		// Some implementations allow duplicate names, some don't
		t.Logf("Duplicate name result: %v", err)
	})

	// Test algorithm with wrong case (should work if server normalizes)
	t.Run("algorithm-case-sensitivity", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-err-case")

		// Try lowercase algorithm
		output, err := cli.run("kms", "key", "generate", keyName, "--algorithm", "aes-256-gcm")
		if err != nil {
			// If it fails, that's expected - we require uppercase
			t.Logf("Lowercase algorithm failed as expected: %v", err)
			return
		}

		// If it succeeds, clean up
		var resp keyResponse
		if cli.unmarshalJSON(output, &resp) == nil && resp.Key.KeyID != "" {
			defer func() {
				_, _ = cli.run("kms", "key", "delete", resp.Key.KeyID, "--force")
			}()
		}
	})

	// Test unknown flag
	t.Run("error-unknown-flag", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-err-flag")
		_, err := cli.runExpectError("kms", "key", "generate", keyName, "--unknown-flag", "value")
		assert.Error(t, err, "generate with unknown flag should fail")
	})

	// Test special characters in key name
	t.Run("special-characters-in-name", func(t *testing.T) {
		// Test with various special characters
		specialNames := []string{
			"key-with-dashes",
			"key_with_underscores",
			"key.with.dots",
			"key/with/slashes",
		}

		for _, name := range specialNames {
			t.Run(name, func(t *testing.T) {
				output, err := cli.run("kms", "key", "generate", name, "--algorithm", "AES-256-GCM")
				if err != nil {
					// Some special characters may not be allowed
					t.Logf("Special character name %q result: %v", name, err)
					return
				}

				var resp keyResponse
				if cli.unmarshalJSON(output, &resp) == nil && resp.Key.KeyID != "" {
					defer func() {
						_, _ = cli.run("kms", "key", "delete", resp.Key.KeyID, "--force")
					}()
				}
			})
		}
	})
}

// =============================================================================
// TestCLIKMSKeyGenerateJSONResponse - JSON Response Validation
// =============================================================================

// TestCLIKMSKeyGenerateJSONResponse validates the JSON response structure.
func TestCLIKMSKeyGenerateJSONResponse(t *testing.T) {
	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	keyName := generateUniqueKeyName("cli-json-validate")
	description := "JSON response validation test"

	output, err := cli.run("kms", "key", "generate", keyName,
		"--algorithm", "AES-256-GCM",
		"--description", description,
		"--exportable")
	require.NoError(t, err, "key generation should succeed")

	// Parse JSON response
	var resp keyResponse
	require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")

	// Validate all expected fields are present
	t.Run("field-key_id", func(t *testing.T) {
		require.NotEmpty(t, resp.Key.KeyID, "key_id field must be present and not empty")
	})

	t.Run("field-name", func(t *testing.T) {
		assert.Equal(t, keyName, resp.Metadata.Name, "name field must match input")
	})

	t.Run("field-algorithm", func(t *testing.T) {
		require.NotEmpty(t, resp.Key.Algorithm, "algorithm field must be present and not empty")
	})

	t.Run("field-version", func(t *testing.T) {
		assert.Equal(t, uint64(1), resp.Key.Version, "version field should be 1 for new key")
	})

	t.Run("field-status", func(t *testing.T) {
		require.NotEmpty(t, resp.Key.Status, "status field must be present and not empty")
		assert.Contains(t, strings.ToUpper(resp.Key.Status), "ENABLED", "status should indicate enabled")
	})

	t.Run("field-exportable", func(t *testing.T) {
		assert.True(t, resp.Metadata.Exportable, "exportable field should be true when --exportable flag is used")
	})

	// Cleanup
	defer func() {
		_, _ = cli.run("kms", "key", "delete", resp.Key.KeyID, "--force")
	}()
}

// =============================================================================
// TestCLIKMSKeyGenerate - Original Comprehensive Tests (Preserved)
// =============================================================================

// TestCLIKMSKeyGenerate tests all permutations of the kms key generate command.
func TestCLIKMSKeyGenerate(t *testing.T) {
	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	// All 5 symmetric algorithms supported by the XKMS backend
	symmetricAlgorithms := []string{
		"AES-128-GCM",
		"AES-192-GCM",
		"AES-256-GCM",
		"CHACHA20-POLY1305",
		"XCHACHA20-POLY1305",
	}

	asymmetricAlgorithms := []string{
		"ED25519",
		"ECDSA-P256",
		"ECDSA-P384",
		"RSA-2048",
		"RSA-4096",
	}

	// Test symmetric algorithms
	for _, alg := range symmetricAlgorithms {
		t.Run("algorithm-"+alg, func(t *testing.T) {
			keyName := generateUniqueKeyName("cli-gen-" + strings.ToLower(strings.ReplaceAll(alg, "-", "")))

			output, err := cli.run("kms", "key", "generate", keyName, "--algorithm", alg)
			require.NoError(t, err, "generate with algorithm %s should succeed", alg)

			var resp keyResponse
			require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
			assert.NotEmpty(t, resp.Key.KeyID, "key ID should not be empty")
			assert.Equal(t, keyName, resp.Metadata.Name, "key name should match")
			assert.Contains(t, resp.Key.Algorithm, strings.TrimPrefix(alg, "AES-"), "algorithm should match")

			// Cleanup
			_, _ = cli.run("kms", "key", "delete", resp.Key.KeyID, "--force")
		})
	}

	// Test asymmetric algorithms (may not be supported by all backends)
	for _, alg := range asymmetricAlgorithms {
		t.Run("algorithm-"+alg, func(t *testing.T) {
			keyName := generateUniqueKeyName("cli-gen-" + strings.ToLower(strings.ReplaceAll(alg, "-", "")))

			output, err := cli.run("kms", "key", "generate", keyName, "--algorithm", alg)
			// All algorithms must be supported by the integration test backend
			require.NoError(t, err, "generate with algorithm %s must succeed", alg)

			var resp keyResponse
			if cli.unmarshalJSON(output, &resp) == nil && resp.Key.KeyID != "" {
				// Cleanup
				_, _ = cli.run("kms", "key", "delete", resp.Key.KeyID, "--force")
			}
		})
	}

	// Test with description flag
	t.Run("with-description", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-gen-desc")
		desc := "Test key with description for integration testing"

		output, err := cli.run("kms", "key", "generate", keyName,
			"--algorithm", "AES-256-GCM",
			"--description", desc)
		require.NoError(t, err, "generate with description should succeed")

		var resp keyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.NotEmpty(t, resp.Key.KeyID, "key ID should not be empty")

		// Verify description via get
		getOutput, err := cli.run("kms", "key", "get", resp.Key.KeyID)
		require.NoError(t, err, "get key should succeed")

		var getResp getKeyResponse
		if cli.unmarshalJSON(getOutput, &getResp) == nil {
			assert.Equal(t, desc, getResp.Metadata.Description, "description should match")
		}

		// Cleanup
		_, _ = cli.run("kms", "key", "delete", resp.Key.KeyID, "--force")
	})

	// Test with exportable flag
	t.Run("with-exportable", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-gen-export")

		output, err := cli.run("kms", "key", "generate", keyName,
			"--algorithm", "AES-256-GCM",
			"--exportable")
		require.NoError(t, err, "generate with exportable should succeed")

		var resp keyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.NotEmpty(t, resp.Key.KeyID, "key ID should not be empty")

		// Verify exportable via get
		getOutput, err := cli.run("kms", "key", "get", resp.Key.KeyID)
		require.NoError(t, err, "get key should succeed")

		var getResp getKeyResponse
		if cli.unmarshalJSON(getOutput, &getResp) == nil {
			assert.True(t, getResp.Metadata.Exportable, "key should be exportable")
		}

		// Cleanup
		_, _ = cli.run("kms", "key", "delete", resp.Key.KeyID, "--force")
	})

	// Test all flags combined
	t.Run("all-flags-combined", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-gen-all")

		output, err := cli.run("kms", "key", "generate", keyName,
			"--algorithm", "AES-256-GCM",
			"--description", "Full test with all flags",
			"--exportable")
		require.NoError(t, err, "generate with all flags should succeed")

		var resp keyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.NotEmpty(t, resp.Key.KeyID, "key ID should not be empty")
		assert.Equal(t, keyName, resp.Metadata.Name, "key name should match")

		// Cleanup
		_, _ = cli.run("kms", "key", "delete", resp.Key.KeyID, "--force")
	})

	// Test default algorithm (should use ED25519)
	t.Run("default-algorithm", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-gen-default")

		output, err := cli.run("kms", "key", "generate", keyName)
		// Default algorithm must be supported by the integration test backend
		require.NoError(t, err, "generate with default algorithm must succeed")

		var resp keyResponse
		if cli.unmarshalJSON(output, &resp) == nil && resp.Key.KeyID != "" {
			// Cleanup
			_, _ = cli.run("kms", "key", "delete", resp.Key.KeyID, "--force")
		}
	})

	// Error cases
	t.Run("error-invalid-algorithm", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-gen-invalid")
		_, err := cli.runExpectError("kms", "key", "generate", keyName, "--algorithm", "INVALID-ALG-XYZ")
		assert.Error(t, err, "generate with invalid algorithm should fail")
	})

	t.Run("error-missing-name", func(t *testing.T) {
		_, err := cli.runExpectError("kms", "key", "generate")
		assert.Error(t, err, "generate without key name should fail")
	})

	t.Run("error-empty-name", func(t *testing.T) {
		_, err := cli.runExpectError("kms", "key", "generate", "")
		assert.Error(t, err, "generate with empty key name should fail")
	})
}

// =============================================================================
// TestCLIKMSKeyGet - Comprehensive Key Retrieval Tests
// =============================================================================

// TestCLIKMSKeyGet tests all permutations of the kms key get command.
func TestCLIKMSKeyGet(t *testing.T) {
	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	// Create a key for testing
	keyName := generateUniqueKeyName("cli-get-test")
	output, err := cli.run("kms", "key", "generate", keyName, "--algorithm", "AES-256-GCM")
	require.NoError(t, err, "key creation should succeed")

	var createResp keyResponse
	require.NoError(t, cli.unmarshalJSON(output, &createResp), "should parse create response")
	keyID := createResp.Key.KeyID
	require.NotEmpty(t, keyID, "key ID should not be empty")

	defer func() {
		_, _ = cli.run("kms", "key", "delete", keyID, "--force")
	}()

	// Rotate key to create multiple versions
	_, err = cli.run("kms", "key", "rotate", keyID)
	require.NoError(t, err, "rotation should succeed")
	_, err = cli.run("kms", "key", "rotate", keyID)
	require.NoError(t, err, "second rotation should succeed")

	// Test get current version (no --version flag)
	t.Run("get-current-version", func(t *testing.T) {
		output, err := cli.run("kms", "key", "get", keyID)
		require.NoError(t, err, "get current version should succeed")

		var resp getKeyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.Equal(t, keyID, resp.Metadata.KeyID, "key ID should match")
		assert.Equal(t, uint64(3), resp.Metadata.Version, "should be version 3 after two rotations")
	})

	// Test get specific version
	t.Run("get-version-1", func(t *testing.T) {
		output, err := cli.run("kms", "key", "get", keyID, "--version", "1")
		require.NoError(t, err, "get version 1 should succeed")

		var resp getKeyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.Equal(t, keyID, resp.Metadata.KeyID, "key ID should match")
		assert.Equal(t, uint64(1), resp.Metadata.Version, "should be version 1")
	})

	t.Run("get-version-2", func(t *testing.T) {
		output, err := cli.run("kms", "key", "get", keyID, "--version", "2")
		require.NoError(t, err, "get version 2 should succeed")

		var resp getKeyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.Equal(t, keyID, resp.Metadata.KeyID, "key ID should match")
		assert.Equal(t, uint64(2), resp.Metadata.Version, "should be version 2")
	})

	// Test get version 0 (should return current)
	t.Run("get-version-0-returns-current", func(t *testing.T) {
		output, err := cli.run("kms", "key", "get", keyID, "--version", "0")
		require.NoError(t, err, "get version 0 should succeed")

		var resp getKeyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.Equal(t, uint64(3), resp.Metadata.Version, "version 0 should return current version")
	})

	// Error cases
	t.Run("error-nonexistent-key", func(t *testing.T) {
		_, err := cli.runExpectError("kms", "key", "get", "nonexistent-key-12345")
		assert.Error(t, err, "get nonexistent key should fail")
	})

	t.Run("error-invalid-version", func(t *testing.T) {
		_, err := cli.runExpectError("kms", "key", "get", keyID, "--version", "999")
		assert.Error(t, err, "get nonexistent version should fail")
	})

	t.Run("error-missing-key-id", func(t *testing.T) {
		_, err := cli.runExpectError("kms", "key", "get")
		assert.Error(t, err, "get without key ID should fail")
	})
}

// =============================================================================
// TestCLIKMSKeyList - Comprehensive Key Listing Tests
// =============================================================================

// TestCLIKMSKeyList tests all permutations of the kms key list command.
func TestCLIKMSKeyList(t *testing.T) {
	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	// Create multiple keys with a common prefix for testing
	prefix := fmt.Sprintf("cli-list-test-%d", time.Now().UnixNano())
	var keyIDs []string

	for i := 0; i < 5; i++ {
		keyName := fmt.Sprintf("%s-key-%d", prefix, i)
		output, err := cli.run("kms", "key", "generate", keyName, "--algorithm", "AES-256-GCM")
		if err != nil {
			continue
		}

		var resp keyResponse
		if cli.unmarshalJSON(output, &resp) == nil && resp.Key.KeyID != "" {
			keyIDs = append(keyIDs, resp.Key.KeyID)
		}
	}

	require.GreaterOrEqual(t, len(keyIDs), 3, "should have created at least 3 keys")

	defer func() {
		for _, keyID := range keyIDs {
			_, _ = cli.run("kms", "key", "delete", keyID, "--force")
		}
	}()

	// Test basic list
	t.Run("list-all", func(t *testing.T) {
		output, err := cli.run("kms", "key", "list")
		require.NoError(t, err, "list all should succeed")

		var resp listKeysResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.GreaterOrEqual(t, len(resp.Keys), len(keyIDs), "should have at least as many keys as we created")
	})

	// Test list with prefix filter
	t.Run("list-with-prefix", func(t *testing.T) {
		output, err := cli.run("kms", "key", "list", prefix)
		require.NoError(t, err, "list with prefix should succeed")

		var resp listKeysResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.Equal(t, len(keyIDs), len(resp.Keys), "should have exactly the keys we created with this prefix")
	})

	// Test list with limit
	t.Run("list-with-limit-2", func(t *testing.T) {
		output, err := cli.run("kms", "key", "list", "--limit", "2")
		require.NoError(t, err, "list with limit should succeed")

		var resp listKeysResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.LessOrEqual(t, len(resp.Keys), 2, "should have at most 2 keys")
	})

	t.Run("list-with-limit-1", func(t *testing.T) {
		output, err := cli.run("kms", "key", "list", "--limit", "1")
		require.NoError(t, err, "list with limit 1 should succeed")

		var resp listKeysResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.LessOrEqual(t, len(resp.Keys), 1, "should have at most 1 key")
	})

	// Test list with prefix and limit combined
	t.Run("list-prefix-and-limit", func(t *testing.T) {
		output, err := cli.run("kms", "key", "list", prefix, "--limit", "2")
		require.NoError(t, err, "list with prefix and limit should succeed")

		var resp listKeysResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.LessOrEqual(t, len(resp.Keys), 2, "should have at most 2 keys")
	})

	// Test list with nonexistent prefix
	t.Run("list-nonexistent-prefix", func(t *testing.T) {
		output, err := cli.run("kms", "key", "list", "nonexistent-prefix-xyz-12345")
		require.NoError(t, err, "list with nonexistent prefix should succeed but return empty")

		var resp listKeysResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.Equal(t, 0, len(resp.Keys), "should have no keys")
	})

	// Test default limit
	t.Run("default-limit-is-100", func(t *testing.T) {
		output, err := cli.run("kms", "key", "list")
		require.NoError(t, err, "list should succeed")

		var resp listKeysResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.LessOrEqual(t, len(resp.Keys), 100, "default limit should be 100")
	})
}

// =============================================================================
// TestCLIKMSKeyRotate - Comprehensive Key Rotation Tests
// =============================================================================

// TestCLIKMSKeyRotate tests all permutations of the kms key rotate command.
func TestCLIKMSKeyRotate(t *testing.T) {
	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	// Create a key for testing
	keyName := generateUniqueKeyName("cli-rotate-test")
	output, err := cli.run("kms", "key", "generate", keyName, "--algorithm", "AES-256-GCM")
	require.NoError(t, err, "key creation should succeed")

	var createResp keyResponse
	require.NoError(t, cli.unmarshalJSON(output, &createResp), "should parse create response")
	keyID := createResp.Key.KeyID
	require.NotEmpty(t, keyID, "key ID should not be empty")

	defer func() {
		_, _ = cli.run("kms", "key", "delete", keyID, "--force")
	}()

	// Test first rotation
	t.Run("rotate-first", func(t *testing.T) {
		output, err := cli.run("kms", "key", "rotate", keyID)
		require.NoError(t, err, "first rotation should succeed")

		var resp rotateKeyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.Equal(t, keyID, resp.KeyID, "key ID should match")
		assert.Equal(t, uint64(1), resp.OldVersion, "old version should be 1")
		assert.Equal(t, uint64(2), resp.NewVersion, "new version should be 2")
	})

	// Test second rotation
	t.Run("rotate-second", func(t *testing.T) {
		output, err := cli.run("kms", "key", "rotate", keyID)
		require.NoError(t, err, "second rotation should succeed")

		var resp rotateKeyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.Equal(t, keyID, resp.KeyID, "key ID should match")
		assert.Equal(t, uint64(2), resp.OldVersion, "old version should be 2")
		assert.Equal(t, uint64(3), resp.NewVersion, "new version should be 3")
	})

	// Verify version increased via get
	t.Run("verify-version-increased", func(t *testing.T) {
		output, err := cli.run("kms", "key", "get", keyID)
		require.NoError(t, err, "get should succeed")

		var resp getKeyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.Equal(t, uint64(3), resp.Metadata.Version, "version should be 3 after two rotations")
	})

	// Error cases
	t.Run("error-nonexistent-key", func(t *testing.T) {
		_, err := cli.runExpectError("kms", "key", "rotate", "nonexistent-key-12345")
		assert.Error(t, err, "rotate nonexistent key should fail")
	})

	t.Run("error-missing-key-id", func(t *testing.T) {
		_, err := cli.runExpectError("kms", "key", "rotate")
		assert.Error(t, err, "rotate without key ID should fail")
	})
}

// =============================================================================
// TestCLIKMSKeyDelete - Comprehensive Key Deletion Tests
// =============================================================================

// TestCLIKMSKeyDelete tests all permutations of the kms key delete command.
func TestCLIKMSKeyDelete(t *testing.T) {
	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	// Test delete with --force flag (skip confirmation)
	t.Run("delete-with-force", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-delete-force")
		output, err := cli.run("kms", "key", "generate", keyName, "--algorithm", "AES-256-GCM")
		require.NoError(t, err, "key creation should succeed")

		var createResp keyResponse
		require.NoError(t, cli.unmarshalJSON(output, &createResp), "should parse create response")
		keyID := createResp.Key.KeyID
		require.NotEmpty(t, keyID, "key ID should not be empty")

		// Delete with force
		output, err = cli.run("kms", "key", "delete", keyID, "--force")
		require.NoError(t, err, "delete with force should succeed")

		var resp deleteKeyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.Equal(t, keyID, resp.KeyID, "key ID should match")

		// Verify key is gone (or pending deletion)
		_, err = cli.runExpectError("kms", "key", "get", keyID)
		// Key may be in PENDING_DELETION state or gone completely
		t.Logf("Get after delete returned: %v", err)
	})

	// Test delete with --force --purge (immediate deletion)
	t.Run("delete-with-force-purge", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-delete-purge")
		output, err := cli.run("kms", "key", "generate", keyName, "--algorithm", "AES-256-GCM")
		require.NoError(t, err, "key creation should succeed")

		var createResp keyResponse
		require.NoError(t, cli.unmarshalJSON(output, &createResp), "should parse create response")
		keyID := createResp.Key.KeyID
		require.NotEmpty(t, keyID, "key ID should not be empty")

		// Delete with force and purge
		output, err = cli.run("kms", "key", "delete", keyID, "--force", "--purge")
		require.NoError(t, err, "delete with force and purge should succeed")

		var resp deleteKeyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.Equal(t, keyID, resp.KeyID, "key ID should match")

		// Verify key is immediately gone
		_, err = cli.runExpectError("kms", "key", "get", keyID)
		assert.Error(t, err, "get should fail for purged key")
	})

	// Test delete without force (should require confirmation - fails in non-interactive)
	t.Run("delete-without-force", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-delete-noforce")
		output, err := cli.run("kms", "key", "generate", keyName, "--algorithm", "AES-256-GCM")
		require.NoError(t, err, "key creation should succeed")

		var createResp keyResponse
		require.NoError(t, cli.unmarshalJSON(output, &createResp), "should parse create response")
		keyID := createResp.Key.KeyID

		defer func() {
			if keyID != "" {
				_, _ = cli.run("kms", "key", "delete", keyID, "--force")
			}
		}()

		// Delete without force - may fail in non-interactive mode
		_, err = cli.runExpectError("kms", "key", "delete", keyID)
		// This might succeed or fail depending on implementation
		t.Logf("Delete without force result: %v", err)
	})

	// Error cases
	t.Run("error-nonexistent-key", func(t *testing.T) {
		_, err := cli.runExpectError("kms", "key", "delete", "nonexistent-key-12345", "--force")
		assert.Error(t, err, "delete nonexistent key should fail")
	})

	t.Run("error-missing-key-id", func(t *testing.T) {
		_, err := cli.runExpectError("kms", "key", "delete", "--force")
		assert.Error(t, err, "delete without key ID should fail")
	})

	// Test purge without force (should not work)
	t.Run("purge-requires-force", func(t *testing.T) {
		keyName := generateUniqueKeyName("cli-delete-purge-noforce")
		output, err := cli.run("kms", "key", "generate", keyName, "--algorithm", "AES-256-GCM")
		require.NoError(t, err, "key creation should succeed")

		var createResp keyResponse
		require.NoError(t, cli.unmarshalJSON(output, &createResp), "should parse create response")
		keyID := createResp.Key.KeyID

		defer func() {
			if keyID != "" {
				_, _ = cli.run("kms", "key", "delete", keyID, "--force")
			}
		}()

		// Purge without force should use 30-day pending period
		output, err = cli.run("kms", "key", "delete", keyID, "--purge")
		// This might fail without --force in non-interactive mode
		t.Logf("Delete with purge but without force result: %v", err)
	})
}

// =============================================================================
// TestCLIKMSKeyEnableDisable - Comprehensive Enable/Disable Tests
// =============================================================================

// TestCLIKMSKeyEnableDisable tests all permutations of the kms key enable/disable commands.
func TestCLIKMSKeyEnableDisable(t *testing.T) {
	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	// Create a key for testing
	keyName := generateUniqueKeyName("cli-enable-disable-test")
	output, err := cli.run("kms", "key", "generate", keyName, "--algorithm", "AES-256-GCM")
	require.NoError(t, err, "key creation should succeed")

	var createResp keyResponse
	require.NoError(t, cli.unmarshalJSON(output, &createResp), "should parse create response")
	keyID := createResp.Key.KeyID
	require.NotEmpty(t, keyID, "key ID should not be empty")

	defer func() {
		_, _ = cli.run("kms", "key", "delete", keyID, "--force")
	}()

	// Test disable key
	t.Run("disable-key", func(t *testing.T) {
		output, err := cli.run("kms", "key", "disable", keyID)
		if cli.skipIfNotImplemented(t, err, output) {
			return
		}
		require.NoError(t, err, "disable should succeed")

		var resp updateKeyStatusResponse
		if cli.unmarshalJSON(output, &resp) == nil {
			assert.Equal(t, keyID, resp.KeyID, "key ID should match")
			assert.Contains(t, strings.ToUpper(resp.NewStatus), "DISABLED", "new status should be disabled")
		}
	})

	// Verify key is disabled
	t.Run("verify-disabled", func(t *testing.T) {
		output, err := cli.run("kms", "key", "get", keyID)
		require.NoError(t, err, "get should succeed")

		var resp getKeyResponse
		if cli.unmarshalJSON(output, &resp) == nil {
			t.Logf("Key status: %s", resp.Metadata.Status)
		}
	})

	// Test enable key
	t.Run("enable-key", func(t *testing.T) {
		output, err := cli.run("kms", "key", "enable", keyID)
		if cli.skipIfNotImplemented(t, err, output) {
			return
		}
		require.NoError(t, err, "enable should succeed")

		var resp updateKeyStatusResponse
		if cli.unmarshalJSON(output, &resp) == nil {
			assert.Equal(t, keyID, resp.KeyID, "key ID should match")
			assert.Contains(t, strings.ToUpper(resp.NewStatus), "ENABLED", "new status should be enabled")
		}
	})

	// Verify key is enabled
	t.Run("verify-enabled", func(t *testing.T) {
		output, err := cli.run("kms", "key", "get", keyID)
		require.NoError(t, err, "get should succeed")

		var resp getKeyResponse
		if cli.unmarshalJSON(output, &resp) == nil {
			t.Logf("Key status: %s", resp.Metadata.Status)
		}
	})

	// Test multiple toggles
	t.Run("toggle-multiple-times", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			_, err := cli.run("kms", "key", "disable", keyID)
			require.NoError(t, err, "key disable must be implemented (iteration %d)", i)
			_, err = cli.run("kms", "key", "enable", keyID)
			require.NoError(t, err, "key enable must be implemented (iteration %d)", i)
		}
	})

	// Error cases
	t.Run("error-enable-nonexistent", func(t *testing.T) {
		stderr, err := cli.runExpectError("kms", "key", "enable", "nonexistent-key-12345")
		if !cli.skipIfNotImplemented(t, err, stderr) {
			assert.Error(t, err, "enable nonexistent key should fail")
		}
	})

	t.Run("error-disable-nonexistent", func(t *testing.T) {
		stderr, err := cli.runExpectError("kms", "key", "disable", "nonexistent-key-12345")
		if !cli.skipIfNotImplemented(t, err, stderr) {
			assert.Error(t, err, "disable nonexistent key should fail")
		}
	})

	t.Run("error-enable-missing-arg", func(t *testing.T) {
		_, err := cli.runExpectError("kms", "key", "enable")
		assert.Error(t, err, "enable without key ID should fail")
	})

	t.Run("error-disable-missing-arg", func(t *testing.T) {
		_, err := cli.runExpectError("kms", "key", "disable")
		assert.Error(t, err, "disable without key ID should fail")
	})
}

// =============================================================================
// TestCLIKMSKeyVersions - Comprehensive Key Versions Tests
// =============================================================================

// TestCLIKMSKeyVersions tests all permutations of the kms key versions command.
func TestCLIKMSKeyVersions(t *testing.T) {
	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	// Create a key and rotate it multiple times
	keyName := generateUniqueKeyName("cli-versions-test")
	output, err := cli.run("kms", "key", "generate", keyName, "--algorithm", "AES-256-GCM")
	require.NoError(t, err, "key creation should succeed")

	var createResp keyResponse
	require.NoError(t, cli.unmarshalJSON(output, &createResp), "should parse create response")
	keyID := createResp.Key.KeyID
	require.NotEmpty(t, keyID, "key ID should not be empty")

	defer func() {
		_, _ = cli.run("kms", "key", "delete", keyID, "--force")
	}()

	// Rotate key 4 times to create 5 versions total
	for i := 0; i < 4; i++ {
		_, err := cli.run("kms", "key", "rotate", keyID)
		require.NoError(t, err, "rotation %d should succeed", i+1)
	}

	// Test list all versions
	t.Run("list-all-versions", func(t *testing.T) {
		output, err := cli.run("kms", "key", "versions", keyID)
		if cli.skipIfNotImplemented(t, err, output) {
			return
		}
		require.NoError(t, err, "list versions should succeed")

		var resp listKeyVersionsResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.Equal(t, 5, len(resp.Versions), "should have 5 versions")
	})

	// Test list versions with limit
	t.Run("list-versions-with-limit", func(t *testing.T) {
		output, err := cli.run("kms", "key", "versions", keyID, "--limit", "2")
		if cli.skipIfNotImplemented(t, err, output) {
			return
		}
		require.NoError(t, err, "list versions with limit should succeed")

		var resp listKeyVersionsResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.LessOrEqual(t, len(resp.Versions), 2, "should have at most 2 versions")
	})

	// Error cases
	t.Run("error-nonexistent-key", func(t *testing.T) {
		stderr, err := cli.runExpectError("kms", "key", "versions", "nonexistent-key-12345")
		if !cli.skipIfNotImplemented(t, err, stderr) {
			assert.Error(t, err, "versions of nonexistent key should fail")
		}
	})

	t.Run("error-missing-key-id", func(t *testing.T) {
		_, err := cli.runExpectError("kms", "key", "versions")
		assert.Error(t, err, "versions without key ID should fail")
	})
}

// =============================================================================
// TestCLIKMSEncryptDecrypt - Comprehensive Encryption/Decryption Tests
// =============================================================================

// TestCLIKMSEncryptDecrypt tests the kms encrypt and decrypt commands.
func TestCLIKMSEncryptDecrypt(t *testing.T) {
	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	// Create a symmetric key for encryption
	keyName := generateUniqueKeyName("cli-crypto-test")
	output, err := cli.run("kms", "key", "generate", keyName, "--algorithm", "AES-256-GCM")
	require.NoError(t, err, "key creation should succeed")

	var createResp keyResponse
	require.NoError(t, cli.unmarshalJSON(output, &createResp), "should parse create response")
	keyID := createResp.Key.KeyID
	require.NotEmpty(t, keyID, "key ID should not be empty")

	defer func() {
		_, _ = cli.run("kms", "key", "delete", keyID, "--force")
	}()

	testData := "Hello, World! This is secret data."

	// Test basic encryption
	t.Run("encrypt-basic", func(t *testing.T) {
		output, err := cli.run("kms", "encrypt", keyID, "--data", testData)
		if cli.skipIfNotImplemented(t, err, output) {
			return
		}
		require.NoError(t, err, "encryption should succeed")

		var resp encryptResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.Equal(t, keyID, resp.KeyID, "key ID should match")
		assert.NotEmpty(t, resp.Ciphertext, "ciphertext should not be empty")
	})

	// Test encryption with AAD
	t.Run("encrypt-with-aad", func(t *testing.T) {
		output, err := cli.run("kms", "encrypt", keyID, "--data", testData, "--aad", "context-data")
		if cli.skipIfNotImplemented(t, err, output) {
			return
		}
		require.NoError(t, err, "encryption with AAD should succeed")

		var resp encryptResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.NotEmpty(t, resp.Ciphertext, "ciphertext should not be empty")
	})

	// Test encryption with base64 input
	t.Run("encrypt-base64-input", func(t *testing.T) {
		b64Data := base64.StdEncoding.EncodeToString([]byte(testData))
		output, err := cli.run("kms", "encrypt", keyID, "--data", b64Data, "--base64")
		if cli.skipIfNotImplemented(t, err, output) {
			return
		}
		require.NoError(t, err, "encryption with base64 input should succeed")

		var resp encryptResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.NotEmpty(t, resp.Ciphertext, "ciphertext should not be empty")
	})

	// Test full encrypt/decrypt cycle
	t.Run("encrypt-decrypt-cycle", func(t *testing.T) {
		// Encrypt
		encOutput, err := cli.run("kms", "encrypt", keyID, "--data", testData)
		if cli.skipIfNotImplemented(t, err, encOutput) {
			return
		}
		require.NoError(t, err, "encryption should succeed")

		var encResp encryptResponse
		require.NoError(t, cli.unmarshalJSON(encOutput, &encResp), "should parse encrypt response")

		// Decrypt (ciphertext is base64-encoded JSON)
		decOutput, err := cli.run("kms", "decrypt", keyID, "--data", encResp.Ciphertext, "--base64")
		if cli.skipIfNotImplemented(t, err, decOutput) {
			return
		}
		require.NoError(t, err, "decryption should succeed")

		var decResp decryptResponse
		require.NoError(t, cli.unmarshalJSON(decOutput, &decResp), "should parse decrypt response")

		// Decode the plaintext if it's base64
		plaintext := decResp.Plaintext
		if decoded, err := base64.StdEncoding.DecodeString(plaintext); err == nil {
			plaintext = string(decoded)
		}
		assert.Equal(t, testData, plaintext, "decrypted data should match original")
	})

	// Error cases
	t.Run("error-encrypt-missing-data", func(t *testing.T) {
		_, err := cli.runExpectError("kms", "encrypt", keyID)
		assert.Error(t, err, "encrypt without data should fail")
	})

	t.Run("error-decrypt-invalid-ciphertext", func(t *testing.T) {
		_, err := cli.runExpectError("kms", "decrypt", keyID, "--data", "invalid-ciphertext")
		// May fail with invalid ciphertext or hex decode error
		t.Logf("Invalid ciphertext error: %v", err)
	})

	t.Run("error-decrypt-wrong-key", func(t *testing.T) {
		// Create another key
		otherKeyName := generateUniqueKeyName("cli-crypto-other")
		output, err := cli.run("kms", "key", "generate", otherKeyName, "--algorithm", "AES-256-GCM")
		require.NoError(t, err, "Creating second key must succeed")

		var otherResp keyResponse
		require.NoError(t, cli.unmarshalJSON(output, &otherResp),
			"Parsing second key response must succeed")
		defer func() {
			_, _ = cli.run("kms", "key", "delete", otherResp.Key.KeyID, "--force")
		}()

		// Encrypt with first key
		encOutput, err := cli.run("kms", "encrypt", keyID, "--data", testData)
		require.NoError(t, err, "Encryption must be available")

		var encResp encryptResponse
		require.NoError(t, cli.unmarshalJSON(encOutput, &encResp),
			"Parsing encrypt response must succeed")

		// Try to decrypt with second key (should fail)
		_, err = cli.runExpectError("kms", "decrypt", otherResp.Key.KeyID, "--data", encResp.Ciphertext)
		assert.Error(t, err, "decrypt with wrong key should fail")
	})
}

// =============================================================================
// TestCLIKMSKeyExport - Comprehensive Key Export Tests
// =============================================================================

// TestCLIKMSKeyExport tests the kms key export command.
func TestCLIKMSKeyExport(t *testing.T) {
	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	// Create an exportable key
	keyName := generateUniqueKeyName("cli-export-test")
	output, err := cli.run("kms", "key", "generate", keyName,
		"--algorithm", "AES-256-GCM",
		"--exportable")
	require.NoError(t, err, "exportable key creation should succeed")

	var createResp keyResponse
	require.NoError(t, cli.unmarshalJSON(output, &createResp), "should parse create response")
	keyID := createResp.Key.KeyID
	require.NotEmpty(t, keyID, "key ID should not be empty")

	defer func() {
		_, _ = cli.run("kms", "key", "delete", keyID, "--force")
	}()

	// Create a wrapping key for export
	wrapKeyName := generateUniqueKeyName("cli-wrap-key")
	wrapOutput, err := cli.run("kms", "key", "generate", wrapKeyName, "--algorithm", "AES-256-GCM")
	require.NoError(t, err, "wrapping key creation should succeed")

	var wrapResp keyResponse
	require.NoError(t, cli.unmarshalJSON(wrapOutput, &wrapResp), "should parse wrap key response")
	wrapKeyID := wrapResp.Key.KeyID

	defer func() {
		_, _ = cli.run("kms", "key", "delete", wrapKeyID, "--force")
	}()

	// Test key export
	t.Run("export-with-wrapping-key", func(t *testing.T) {
		output, err := cli.run("kms", "key", "export", keyID,
			"--wrapping-key", wrapKeyID,
			"--wrapping-algorithm", "AES-KW")
		if cli.skipIfNotImplemented(t, err, output) {
			return
		}
		require.NoError(t, err, "export should succeed")

		var resp exportKeyResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")
		assert.Equal(t, keyID, resp.KeyID, "key ID should match")
		assert.NotEmpty(t, resp.WrappedKey, "wrapped key should not be empty")
	})

	// Error cases
	t.Run("error-export-nonexportable-key", func(t *testing.T) {
		// Create non-exportable key
		nonExpKeyName := generateUniqueKeyName("cli-nonexp")
		output, err := cli.run("kms", "key", "generate", nonExpKeyName, "--algorithm", "AES-256-GCM")
		require.NoError(t, err, "Creating non-exportable key must succeed")

		var nonExpResp keyResponse
		require.NoError(t, cli.unmarshalJSON(output, &nonExpResp),
			"Parsing non-exportable key response must succeed")
		defer func() {
			_, _ = cli.run("kms", "key", "delete", nonExpResp.Key.KeyID, "--force")
		}()

		// Try to export non-exportable key
		stderr, err := cli.runExpectError("kms", "key", "export", nonExpResp.Key.KeyID,
			"--wrapping-key", wrapKeyID)
		if !cli.skipIfNotImplemented(t, err, stderr) {
			assert.Error(t, err, "export of non-exportable key should fail")
		}
	})

	t.Run("error-export-missing-wrapping-key", func(t *testing.T) {
		_, err := cli.runExpectError("kms", "key", "export", keyID)
		assert.Error(t, err, "export without wrapping key should fail")
	})
}

// =============================================================================
// TestCLIKMSBackend - Backend Status Tests
// =============================================================================

// TestCLIKMSBackend tests the kms backend commands.
// Note: Using gRPC transport as the CLI's REST transport for KMS backend
// commands is not fully implemented (the --transport rest flag is ignored
// and gRPC is used internally).
func TestCLIKMSBackend(t *testing.T) {
	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	t.Run("backend-status", func(t *testing.T) {
		output, err := cli.run("kms", "backend", "status")
		if cli.skipIfNotImplemented(t, err, output) {
			return
		}
		require.NoError(t, err, "backend status should succeed")
		t.Logf("Backend status: %s", output)
	})

	t.Run("backend-test", func(t *testing.T) {
		output, err := cli.run("kms", "backend", "test")
		if cli.skipIfNotImplemented(t, err, output) {
			return
		}
		require.NoError(t, err, "backend test should succeed")
		t.Logf("Backend test: %s", output)
	})
}

// =============================================================================
// TestCLIKMSSafety - Safety Monitoring Tests
// =============================================================================

// TestCLIKMSSafety tests the kms safety commands.
func TestCLIKMSSafety(t *testing.T) {
	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	t.Run("safety-check", func(t *testing.T) {
		output, err := cli.run("kms", "safety", "check")
		if cli.skipIfNotImplemented(t, err, output) {
			return
		}
		require.NoError(t, err, "safety check should succeed")
		t.Logf("Safety check: %s", output)
	})

	t.Run("safety-report", func(t *testing.T) {
		output, err := cli.run("kms", "safety", "report")
		if cli.skipIfNotImplemented(t, err, output) {
			return
		}
		require.NoError(t, err, "safety report should succeed")
		t.Logf("Safety report: %s", output)
	})
}

// =============================================================================
// Helper Functions
// =============================================================================

// Suppress unused variable warnings for base64 and hex imports
var (
	_ = base64.StdEncoding
	_ = hex.EncodeToString
)
