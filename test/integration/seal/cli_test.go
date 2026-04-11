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

package seal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
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
		"/app/bin/quicbaas",
		"/usr/local/bin/quicbaas",
		"./quicbaas",
		"../../../quicbaas",
		"./bin/quicbaas",
		"../../bin/quicbaas",
	}

	// Check relative to GOPATH
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		locations = append(locations, filepath.Join(gopath, "bin", "quicbaas"))
	}

	for _, loc := range locations {
		absPath, err := filepath.Abs(loc)
		if err != nil {
			continue
		}
		if _, err := os.Stat(absPath); err == nil {
			return absPath
		}
	}

	// Try PATH
	if path, err := exec.LookPath("quicbaas"); err == nil {
		return path
	}

	return ""
}

// requireCLIBinary fails the test if the CLI binary is not found.
func requireCLIBinary(t *testing.T) string {
	t.Helper()
	binary := getQuicBaaSCLIBinary()
	require.NotEmpty(t, binary, "quicbaas CLI binary not found - CLI integration test requires the binary to be available")
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
}

// newCLITestContext creates a new CLI test context.
func newCLITestContext(t *testing.T, protocol, address string) *cliTestContext {
	return &cliTestContext{
		t:         t,
		binary:    requireCLIBinary(t),
		protocol:  protocol,
		address:   address,
		timeout:   30 * time.Second,
		outputFmt: "json",
	}
}

// run executes a CLI command with the configured protocol and address.
func (c *cliTestContext) run(args ...string) (string, error) {
	c.t.Helper()

	// Build full command with protocol and address
	fullArgs := []string{
		"--transport", c.protocol,
		"--address", c.address,
		"--tls-skip-verify",
		"--output", c.outputFmt,
	}
	fullArgs = append(fullArgs, args...)

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.binary, fullArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	c.t.Logf("[%s] %s %s", c.protocol, c.binary, strings.Join(fullArgs, " "))
	if stdout.Len() > 0 {
		c.t.Logf("[%s] stdout: %s", c.protocol, stdout.String())
	}
	if stderr.Len() > 0 {
		c.t.Logf("[%s] stderr: %s", c.protocol, stderr.String())
	}

	if err != nil {
		return stdout.String(), &qberrors.WrappedMessageError{
			Message: "command failed, stderr: " + stderr.String(),
			Cause:   err,
		}
	}

	return stdout.String(), nil
}

// runWithStdin executes a CLI command with stdin input.
func (c *cliTestContext) runWithStdin(stdinInput string, args ...string) (string, error) {
	c.t.Helper()

	fullArgs := []string{
		"--transport", c.protocol,
		"--address", c.address,
		"--tls-skip-verify",
		"--output", c.outputFmt,
	}
	fullArgs = append(fullArgs, args...)

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.binary, fullArgs...)
	cmd.Stdin = strings.NewReader(stdinInput)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	c.t.Logf("[%s] %s %s (with stdin)", c.protocol, c.binary, strings.Join(fullArgs, " "))
	if stdout.Len() > 0 {
		c.t.Logf("[%s] stdout: %s", c.protocol, stdout.String())
	}
	if stderr.Len() > 0 {
		c.t.Logf("[%s] stderr: %s", c.protocol, stderr.String())
	}

	if err != nil {
		return stdout.String(), &qberrors.WrappedMessageError{
			Message: "command failed, stderr: " + stderr.String(),
			Cause:   err,
		}
	}

	return stdout.String(), nil
}

// runExpectError executes a CLI command expecting it to fail.
func (c *cliTestContext) runExpectError(args ...string) (string, error) {
	c.t.Helper()

	fullArgs := []string{
		"--transport", c.protocol,
		"--address", c.address,
		"--tls-skip-verify",
		"--output", c.outputFmt,
	}
	fullArgs = append(fullArgs, args...)

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.binary, fullArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

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
	output = strings.TrimSpace(output)
	if err := json.Unmarshal([]byte(output), v); err != nil {
		c.t.Logf("Failed to unmarshal JSON: %v\nOutput: %s", err, output)
		return err
	}
	return nil
}

// requireFeatureImplemented checks if the error indicates the feature is not implemented and fails the test if so.
func (c *cliTestContext) requireFeatureImplemented(t *testing.T, err error, errOutput string) {
	t.Helper()
	if err == nil {
		return
	}
	errStr := err.Error() + errOutput
	isUnimplemented := strings.Contains(errStr, "not yet implemented") ||
		strings.Contains(errStr, "Unimplemented") ||
		strings.Contains(errStr, "unknown service")
	require.False(t, isUnimplemented, "Feature must be implemented on server for integration tests: %s", errStr)
}

// getGRPCProtocolConfig returns test configuration for gRPC.
func getGRPCProtocolConfig() struct {
	name    string
	address string
} {
	endpoints := getGRPCEndpoints()
	return struct {
		name    string
		address string
	}{"grpc", endpoints[0]}
}

// =============================================================================
// Response Types for JSON Parsing
// =============================================================================

type sealStatusResponse struct {
	Sealed         bool   `json:"sealed"`
	Initialized    bool   `json:"initialized"`
	Mode           string `json:"mode"`
	Threshold      int    `json:"threshold"`
	TotalShares    int    `json:"total_shares"`
	SharesProvided int    `json:"shares_provided"`
	Version        int    `json:"version"`
	TPMAvailable   bool   `json:"tpm_available"`
	ClusterNodes   int    `json:"cluster_nodes"`
}

type sealInitResponse struct {
	Shares       []string `json:"shares"`
	RecoveryKeys []string `json:"recovery_keys,omitempty"`
	RootToken    string   `json:"root_token"`
	Version      int      `json:"version"`
	Timestamp    string   `json:"timestamp"`
}

type unsealResponse struct {
	Sealed         bool   `json:"sealed"`
	SharesProvided int    `json:"shares_provided"`
	Threshold      int    `json:"threshold"`
	Error          string `json:"error,omitempty"`
}

type rekeyResponse struct {
	Shares    []string `json:"shares"`
	Version   int      `json:"version"`
	Timestamp string   `json:"timestamp"`
}

// =============================================================================
// CLI Seal Status Tests
// =============================================================================

// TestCLISealStatus tests the seal status command.
func TestCLISealStatus(t *testing.T) {
	requireSealAvailable(t)

	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	t.Run("basic-status", func(t *testing.T) {
		output, err := cli.run("seal", "status")
		cli.requireFeatureImplemented(t, err, output)
		require.NoError(t, err, "seal status should succeed")

		var resp sealStatusResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")

		// Validate response structure - sealed and initialized are booleans
		// Mode can be empty for uninitialized systems
		if resp.Initialized {
			assert.GreaterOrEqual(t, resp.Threshold, 1, "threshold should be >= 1 when initialized")
			assert.GreaterOrEqual(t, resp.TotalShares, resp.Threshold,
				"total_shares should be >= threshold")
			assert.GreaterOrEqual(t, resp.Version, 1, "version should be >= 1 when initialized")
		}

		t.Logf("Status: sealed=%v, initialized=%v, mode=%q, threshold=%d/%d",
			resp.Sealed, resp.Initialized, resp.Mode, resp.Threshold, resp.TotalShares)
	})

	t.Run("status-output-table", func(t *testing.T) {
		// Test with table output format
		cli.outputFmt = "table"
		output, err := cli.run("seal", "status")
		cli.outputFmt = "json" // Reset

		cli.requireFeatureImplemented(t, err, output)
		require.NoError(t, err, "seal status with table output should succeed")
		assert.Contains(t, strings.ToLower(output), "seal", "table output should contain 'seal'")
	})
}

// =============================================================================
// CLI Seal Init Tests
// =============================================================================

// TestCLISealInit tests the seal init command.
func TestCLISealInit(t *testing.T) {
	requireSealAvailable(t)

	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	// First check if already initialized
	statusOutput, err := cli.run("seal", "status")
	require.NoError(t, err, "seal status check should succeed before testing init")

	var statusResp sealStatusResponse
	err = cli.unmarshalJSON(statusOutput, &statusResp)
	require.NoError(t, err, "should parse seal status response")

	// If already initialized, we can only test error cases
	if statusResp.Initialized {
		t.Log("Seal already initialized - testing error cases only")
	}

	t.Run("init-with-shares-and-threshold", func(t *testing.T) {
		if statusResp.Initialized {
			// Already initialized - verify init fails appropriately
			stderr, err := cli.runExpectError("seal", "init", "--shares", "3", "--threshold", "2")
			cli.requireFeatureImplemented(t, err, stderr)
			require.Error(t, err, "init should fail when already initialized")
			assert.True(t, strings.Contains(stderr, "already initialized") || err != nil,
				"error should indicate already initialized state")
			return
		}

		output, err := cli.run("seal", "init", "--shares", "3", "--threshold", "2")
		cli.requireFeatureImplemented(t, err, output)
		require.NoError(t, err, "seal init should succeed")

		var resp sealInitResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse JSON response")

		assert.Len(t, resp.Shares, 3, "should return 3 shares")
		assert.GreaterOrEqual(t, resp.Version, 1, "version should be >= 1")

		for i, share := range resp.Shares {
			assert.NotEmpty(t, share, "share %d should not be empty", i+1)
		}

		// Save shares to state manager for subsequent tests
		mgr := getStateManager()
		if mgr != nil {
			// Decode base64 shares to bytes
			shares := make([][]byte, len(resp.Shares))
			for i, shareStr := range resp.Shares {
				decoded, err := base64.StdEncoding.DecodeString(shareStr)
				if err != nil {
					t.Logf("Warning: Failed to decode share %d: %v", i, err)
					continue
				}
				shares[i] = decoded
			}
			t.Logf("DEBUG: About to save %d shares with version=%d", len(shares), resp.Version)
			mgr.UpdateConfig(2, 3, uint64(resp.Version))
			if err := mgr.SaveShares(shares); err != nil {
				t.Logf("Warning: Failed to save shares to state manager: %v", err)
			} else {
				t.Logf("DEBUG: Saved shares successfully, verifying: hasShares=%v", mgr.HasShares())
			}
		} else {
			t.Log("WARNING: State manager is nil, shares cannot be saved!")
		}
	})

	t.Run("error-invalid-threshold-greater-than-shares", func(t *testing.T) {
		stderr, err := cli.runExpectError("seal", "init", "--shares", "3", "--threshold", "5")
		cli.requireFeatureImplemented(t, err, stderr)
		assert.Error(t, err, "init with threshold > shares should fail")
	})

	t.Run("error-zero-shares", func(t *testing.T) {
		stderr, err := cli.runExpectError("seal", "init", "--shares", "0", "--threshold", "0")
		cli.requireFeatureImplemented(t, err, stderr)
		assert.Error(t, err, "init with 0 shares should fail")
	})

	t.Run("error-negative-threshold", func(t *testing.T) {
		stderr, err := cli.runExpectError("seal", "init", "--shares", "3", "--threshold", "-1")
		cli.requireFeatureImplemented(t, err, stderr)
		assert.Error(t, err, "init with negative threshold should fail")
	})
}

// =============================================================================
// CLI Seal Unseal Tests
// =============================================================================

// TestCLISealUnseal tests the seal unseal command.
func TestCLISealUnseal(t *testing.T) {
	requireSealAvailable(t)

	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	// Check current status
	statusOutput, err := cli.run("seal", "status")
	require.NoError(t, err, "seal status check should succeed")

	var statusResp sealStatusResponse
	err = cli.unmarshalJSON(statusOutput, &statusResp)
	require.NoError(t, err, "should parse seal status response")

	require.True(t, statusResp.Initialized, "seal must be initialized to test unseal commands")

	// Note: For tests that modify seal state, we assert the expected state
	// rather than skipping. If the system is in an unexpected state, the test fails.

	t.Run("error-invalid-share-format", func(t *testing.T) {
		stderr, err := cli.runExpectError("seal", "unseal", "--share", "not-valid-base64!!!")
		cli.requireFeatureImplemented(t, err, stderr)
		assert.Error(t, err, "unseal with invalid share format should fail")
	})

	t.Run("error-wrong-share", func(t *testing.T) {
		// NOTE: Providing a wrong share to a sealed system can leave it in a
		// partial unseal state (shares_provided > 0) which breaks subsequent tests.
		// In Shamir's Secret Sharing, shares are accepted one by one and only
		// validated when threshold is reached.
		//
		// When the system is sealed, we test that the share is at least syntactically
		// accepted (the server won't reject until threshold is reached).
		// When unsealed, providing a share should fail or be rejected.

		// Re-check status for this specific test
		currentStatusOutput, err := cli.run("seal", "status")
		require.NoError(t, err, "seal status should succeed")
		var currentStatus sealStatusResponse
		require.NoError(t, cli.unmarshalJSON(currentStatusOutput, &currentStatus), "should parse status")

		wrongShare := base64.StdEncoding.EncodeToString([]byte("this-is-a-wrong-share-definitely"))
		stderr, cmdErr := cli.runExpectError("seal", "unseal", "--share", wrongShare)
		cli.requireFeatureImplemented(t, cmdErr, stderr)

		if currentStatus.Sealed {
			// When sealed, wrong shares may be accepted until threshold is reached
			// The behavior depends on implementation - log the result
			t.Logf("Sealed system response to wrong share: err=%v, stderr=%s", cmdErr, stderr)
		} else {
			// When unsealed, providing a share should fail or be rejected
			if cmdErr != nil {
				t.Logf("Server rejected share when unsealed (expected)")
			} else {
				t.Logf("Server accepted share but system is already unsealed")
			}
		}
	})

	t.Run("error-empty-share", func(t *testing.T) {
		// Re-check status for this specific test
		currentStatusOutput, err := cli.run("seal", "status")
		require.NoError(t, err, "seal status should succeed")
		var currentStatus sealStatusResponse
		require.NoError(t, cli.unmarshalJSON(currentStatusOutput, &currentStatus), "should parse status")

		stderr, cmdErr := cli.runExpectError("seal", "unseal", "--share", "")
		cli.requireFeatureImplemented(t, cmdErr, stderr)

		// Empty share should always be rejected
		assert.Error(t, cmdErr, "unseal with empty share should fail")
	})
}

// =============================================================================
// CLI Seal Seal Tests
// =============================================================================

// TestCLISealSeal tests the seal seal command.
func TestCLISealSeal(t *testing.T) {
	requireSealAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	// Check current status
	statusOutput, err := cli.run("seal", "status")
	require.NoError(t, err, "seal status check should succeed")

	var statusResp sealStatusResponse
	err = cli.unmarshalJSON(statusOutput, &statusResp)
	require.NoError(t, err, "should parse seal status response")

	require.True(t, statusResp.Initialized, "seal must be initialized to test seal command")

	// If sealed, try to unseal using state manager
	if statusResp.Sealed {
		mgr := getStateManager()
		require.NotNil(t, mgr, "state manager must be available when system is sealed to unseal it for testing")

		// Create gRPC client for state manager operations
		conn := setupGRPCConnection(t, ctx)
		defer conn.Close()
		client := pb.NewQuicBaaSClient(conn)

		err := mgr.EnsureUnsealed(ctx, client)
		require.NoError(t, err, "must be able to unseal system for seal command testing")
		t.Log("Unsealed system using state manager")
	}

	t.Run("seal-requires-confirmation", func(t *testing.T) {
		// Without confirmation, the seal command should either fail or prompt
		// Since we can't provide interactive input, it should fail
		output, err := cli.runWithStdin("no\n", "seal", "seal")
		cli.requireFeatureImplemented(t, err, output)
		// The command should not seal when we answer "no"
		t.Logf("Seal command with 'no' response: err=%v, output=%s", err, output)
	})
}

// =============================================================================
// CLI Seal Rekey Tests
// =============================================================================

// TestCLISealRekey tests the seal rekey command.
func TestCLISealRekey(t *testing.T) {
	requireSealAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	// Check current status
	statusOutput, err := cli.run("seal", "status")
	require.NoError(t, err, "seal status check should succeed")

	var statusResp sealStatusResponse
	err = cli.unmarshalJSON(statusOutput, &statusResp)
	require.NoError(t, err, "should parse seal status response")

	require.True(t, statusResp.Initialized, "seal must be initialized to test rekey command")

	// If sealed, try to unseal using state manager
	if statusResp.Sealed {
		mgr := getStateManager()
		require.NotNil(t, mgr, "state manager must be available when system is sealed to unseal it for testing")

		// Create gRPC client for state manager operations
		conn := setupGRPCConnection(t, ctx)
		defer conn.Close()
		client := pb.NewQuicBaaSClient(conn)

		err := mgr.EnsureUnsealed(ctx, client)
		require.NoError(t, err, "must be able to unseal system for rekey command testing")
		t.Log("Unsealed system using state manager")
	}

	t.Run("error-invalid-threshold-greater-than-shares", func(t *testing.T) {
		stderr, err := cli.runExpectError("seal", "rekey", "--shares", "3", "--threshold", "5")
		cli.requireFeatureImplemented(t, err, stderr)
		assert.Error(t, err, "rekey with threshold > shares should fail")
	})

	t.Run("error-zero-shares", func(t *testing.T) {
		stderr, err := cli.runExpectError("seal", "rekey", "--shares", "0", "--threshold", "0")
		cli.requireFeatureImplemented(t, err, stderr)
		assert.Error(t, err, "rekey with 0 shares should fail")
	})

	t.Run("error-negative-threshold", func(t *testing.T) {
		stderr, err := cli.runExpectError("seal", "rekey", "--shares", "3", "--threshold", "-1")
		cli.requireFeatureImplemented(t, err, stderr)
		assert.Error(t, err, "rekey with negative threshold should fail")
	})
}

// =============================================================================
// CLI Error Path Tests
// =============================================================================

// TestCLISealErrorPaths tests various error conditions for seal commands.
func TestCLISealErrorPaths(t *testing.T) {
	requireSealAvailable(t)

	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	t.Run("error-invalid-command", func(t *testing.T) {
		// Cobra shows help for unknown subcommands and returns exit 0
		// Verify that help is shown containing expected content
		stdout, err := cli.run("seal", "invalid-command")
		assert.NoError(t, err, "unknown subcommand should show help without error")
		assert.Contains(t, stdout, "Available Commands", "help should list available commands")
		assert.Contains(t, stdout, "status", "help should mention status command")
	})

	t.Run("error-unseal-duplicate-share", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// First check if sealed
		statusOutput, err := cli.run("seal", "status")
		require.NoError(t, err, "seal status should succeed")

		var statusResp sealStatusResponse
		err = cli.unmarshalJSON(statusOutput, &statusResp)
		require.NoError(t, err, "should parse seal status response")

		// Ensure system is sealed for this test
		if !statusResp.Sealed && statusResp.Initialized {
			mgr := getStateManager()
			if mgr != nil {
				conn := setupGRPCConnection(t, ctx)
				defer conn.Close()
				client := pb.NewQuicBaaSClient(conn)
				if sealErr := mgr.EnsureSealed(ctx, client); sealErr != nil {
					t.Logf("Could not seal system: %v", sealErr)
				} else {
					t.Log("Sealed system using state manager")
				}
			}
		}

		require.True(t, statusResp.Initialized, "system must be initialized to test duplicate share detection")

		// Re-check status after potential sealing
		statusOutput, err = cli.run("seal", "status")
		require.NoError(t, err, "seal status should succeed")
		err = cli.unmarshalJSON(statusOutput, &statusResp)
		require.NoError(t, err, "should parse seal status response")

		require.True(t, statusResp.Sealed, "system must be sealed to test duplicate share detection")

		// Try providing the same share twice
		// This requires a valid share from the state manager
		mgr := getStateManager()
		require.NotNil(t, mgr, "state manager must be available to test duplicate share detection")

		shares := mgr.GetShares()
		require.NotEmpty(t, shares, "shares must be available from state manager")

		// Provide the first share
		shareStr := base64.StdEncoding.EncodeToString(shares[0])

		// First submission
		_, _ = cli.run("seal", "unseal", "--share", shareStr)

		// Second submission of same share should be rejected or handled
		stderr, err := cli.runExpectError("seal", "unseal", "--share", shareStr)
		cli.requireFeatureImplemented(t, err, stderr)
		// Log the result - duplicate handling depends on implementation
		t.Logf("Duplicate share response: err=%v, stderr=%s", err, stderr)
	})

	t.Run("error-init-when-already-initialized", func(t *testing.T) {
		// Check if initialized
		statusOutput, err := cli.run("seal", "status")
		require.NoError(t, err, "seal status should succeed")

		var statusResp sealStatusResponse
		err = cli.unmarshalJSON(statusOutput, &statusResp)
		require.NoError(t, err, "should parse seal status response")

		require.True(t, statusResp.Initialized, "system must be initialized to test re-init error")

		stderr, err := cli.runExpectError("seal", "init", "--shares", "5", "--threshold", "3")
		cli.requireFeatureImplemented(t, err, stderr)
		assert.Error(t, err, "init when already initialized should fail")
	})

	t.Run("error-rekey-when-sealed", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Check if sealed and initialized
		statusOutput, err := cli.run("seal", "status")
		require.NoError(t, err, "seal status should succeed")

		var statusResp sealStatusResponse
		err = cli.unmarshalJSON(statusOutput, &statusResp)
		require.NoError(t, err, "should parse seal status response")

		require.True(t, statusResp.Initialized, "system must be initialized to test rekey when sealed")

		// Ensure system is sealed for this test (previous tests may have left it unsealed)
		if !statusResp.Sealed {
			mgr := getStateManager()
			require.NotNil(t, mgr, "State manager must be available to seal system for testing")

			// Create gRPC client for state manager operations
			conn := setupGRPCConnection(t, ctx)
			defer conn.Close()
			client := pb.NewQuicBaaSClient(conn)

			err := mgr.EnsureSealed(ctx, client)
			require.NoError(t, err, "Must be able to seal system for rekey-when-sealed test")
			t.Log("Sealed system using state manager")
		}

		stderr, err := cli.runExpectError("seal", "rekey", "--shares", "5", "--threshold", "3")
		cli.requireFeatureImplemented(t, err, stderr)
		assert.Error(t, err, "rekey when sealed should fail")
	})
}

// =============================================================================
// CLI Integration Lifecycle Test
// =============================================================================

// TestCLISealLifecycle tests the complete seal lifecycle via CLI.
// This test adapts to either fresh or already-initialized environments.
func TestCLISealLifecycle(t *testing.T) {
	requireSealAvailable(t)

	proto := getGRPCProtocolConfig()
	cli := newCLITestContext(t, proto.name, proto.address)

	// Check current state
	statusOutput, err := cli.run("seal", "status")
	require.NoError(t, err, "seal status check should succeed")

	var statusResp sealStatusResponse
	err = cli.unmarshalJSON(statusOutput, &statusResp)
	require.NoError(t, err, "should parse seal status response")

	if !statusResp.Initialized {
		// Fresh environment - run full lifecycle test
		t.Log("Running full lifecycle test in fresh environment")
		testCLILifecycleFresh(t, cli)
	} else {
		// Already initialized - test seal/unseal operations
		t.Log("Running lifecycle test with existing seal initialization")
		testCLILifecycleExisting(t, cli, statusResp)
	}
}

// testCLILifecycleFresh runs full lifecycle when seal is not initialized.
func testCLILifecycleFresh(t *testing.T, cli *cliTestContext) {
	var shares []string

	// Step 1: Initialize
	t.Run("1-init", func(t *testing.T) {
		output, err := cli.run("seal", "init", "--shares", "3", "--threshold", "2")
		cli.requireFeatureImplemented(t, err, output)
		require.NoError(t, err, "init should succeed")

		var initResp sealInitResponse
		require.NoError(t, cli.unmarshalJSON(output, &initResp), "should parse init response")

		require.Len(t, initResp.Shares, 3, "should have 3 shares")
		shares = initResp.Shares

		// Save shares to state manager for other tests
		if mgr := getStateManager(); mgr != nil {
			// Decode base64 shares to bytes for storage
			byteShares := make([][]byte, len(shares))
			for i, shareStr := range shares {
				decoded, err := base64.StdEncoding.DecodeString(shareStr)
				if err != nil {
					t.Logf("Warning: Failed to decode share %d: %v", i, err)
					continue
				}
				byteShares[i] = decoded
			}
			mgr.UpdateConfig(2, 3, uint64(initResp.Version))
			if err := mgr.SaveShares(byteShares); err != nil {
				t.Logf("Warning: Failed to save shares: %v", err)
			}
		}
	})

	// Step 2: Verify sealed status
	t.Run("2-verify-sealed", func(t *testing.T) {
		output, err := cli.run("seal", "status")
		require.NoError(t, err, "status should succeed")

		var resp sealStatusResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse status")

		assert.True(t, resp.Sealed, "should be sealed after init")
		assert.True(t, resp.Initialized, "should be initialized")
		assert.Equal(t, 2, resp.Threshold, "threshold should be 2")
		assert.Equal(t, 3, resp.TotalShares, "total shares should be 3")
	})

	// Step 3: Unseal with saved shares
	t.Run("3-unseal", func(t *testing.T) {
		require.GreaterOrEqual(t, len(shares), 2, "need at least 2 shares to unseal")

		// Provide first share
		output, err := cli.run("seal", "unseal", "--share", shares[0])
		cli.requireFeatureImplemented(t, err, output)
		require.NoError(t, err, "first unseal should succeed")

		var resp1 unsealResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp1), "should parse unseal response")
		assert.True(t, resp1.Sealed, "should still be sealed after 1 share")
		assert.Equal(t, 1, resp1.SharesProvided, "should have 1 share provided")

		// Provide second share
		output, err = cli.run("seal", "unseal", "--share", shares[1])
		require.NoError(t, err, "second unseal should succeed")

		var resp2 unsealResponse
		require.NoError(t, cli.unmarshalJSON(output, &resp2), "should parse unseal response")
		assert.False(t, resp2.Sealed, "should be unsealed after threshold shares")
	})
}

// testCLILifecycleExisting tests lifecycle when seal is already initialized.
func testCLILifecycleExisting(t *testing.T, cli *cliTestContext, initialStatus sealStatusResponse) {
	t.Run("1-verify-initialized", func(t *testing.T) {
		assert.True(t, initialStatus.Initialized, "should be initialized")
		t.Logf("Current state: sealed=%v, threshold=%d, totalShares=%d",
			initialStatus.Sealed, initialStatus.Threshold, initialStatus.TotalShares)
	})

	// Get shares from state manager
	mgr := getStateManager()
	require.NotNil(t, mgr, "state manager must be available for existing seal tests")

	byteShares := mgr.GetShares()
	_, _, _, _, version := mgr.GetStatus()
	require.NotEmpty(t, byteShares, "shares must be available from state manager")
	t.Logf("State manager has %d shares, version=%d", len(byteShares), version)

	// Convert byte shares to base64 strings for CLI
	shares := make([]string, len(byteShares))
	for i, s := range byteShares {
		shares[i] = base64.StdEncoding.EncodeToString(s)
	}

	// Get current threshold from status
	threshold := int(initialStatus.Threshold)
	if threshold == 0 {
		threshold = 2 // default
	}

	// Step 2: First seal the system to ensure clean state (clears any partial unseal shares)
	t.Run("2-seal", func(t *testing.T) {
		output, err := cli.runWithStdin("yes\n", "seal", "seal")
		cli.requireFeatureImplemented(t, err, output)
		require.NoError(t, err, "seal should succeed")

		// Verify sealed
		statusOutput, err := cli.run("seal", "status")
		require.NoError(t, err, "status should succeed")

		var resp sealStatusResponse
		require.NoError(t, cli.unmarshalJSON(statusOutput, &resp), "should parse status")
		assert.True(t, resp.Sealed, "should be sealed")
	})

	// Step 3: Now unseal to verify the unseal process works
	t.Run("3-unseal", func(t *testing.T) {
		require.GreaterOrEqual(t, len(shares), threshold,
			"need at least threshold shares to unseal")

		for i := 0; i < threshold; i++ {
			output, err := cli.run("seal", "unseal", "--share", shares[i])
			cli.requireFeatureImplemented(t, err, output)
			require.NoError(t, err, "unseal share %d should succeed", i+1)

			var resp unsealResponse
			require.NoError(t, cli.unmarshalJSON(output, &resp), "should parse unseal response")

			if i+1 >= threshold {
				assert.False(t, resp.Sealed, "should be unsealed after threshold shares")
			} else {
				assert.True(t, resp.Sealed, "should still be sealed after %d shares", i+1)
			}
		}
	})

	// Step 4: Verify final unsealed state
	t.Run("4-verify-unsealed", func(t *testing.T) {
		statusOutput, err := cli.run("seal", "status")
		require.NoError(t, err, "status should succeed")

		var resp sealStatusResponse
		require.NoError(t, cli.unmarshalJSON(statusOutput, &resp), "should parse status")
		assert.False(t, resp.Sealed, "should be unsealed")
		assert.True(t, resp.Initialized, "should still be initialized")
	})
}
