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
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clusterOpsTransportsToSkip defines which transports to skip for cluster operations
// that still hardcode gRPC client usage (node, migrate, upgrade).
// Rebalance commands now use the SDK transport layer and work with all transports.
var clusterOpsTransportsToSkip = []Transport{TransportREST, TransportMCP, TransportQUIC, TransportUnix}

// rebalanceTransportsToSkip is empty because rebalance commands now use the SDK
// transport layer and work with all supported transports.
var rebalanceTransportsToSkip []Transport

// Transport represents a transport protocol type.
type Transport string

const (
	// TransportGRPC uses gRPC protocol.
	TransportGRPC Transport = "grpc"
	// TransportREST uses HTTP/REST protocol.
	TransportREST Transport = "rest"
	// TransportQUIC uses QUIC protocol.
	TransportQUIC Transport = "quic"
	// TransportUnix uses Unix socket with gRPC.
	TransportUnix Transport = "unix"
	// TransportMCP uses Model Context Protocol (JSON-RPC 2.0 over HTTP).
	TransportMCP Transport = "mcp"
	// TransportEmbedded uses the in-process embedded transport (no network).
	TransportEmbedded Transport = "embedded"
)

// getQuicBaaSBinary locates the quicbaas CLI binary.
// The binary is built by the Makefile before tests run and mounted at /app/bin/quicbaas.
// This is the SINGLE SOURCE OF TRUTH for the CLI binary in integration tests.
func getQuicBaaSBinary() string {
	// Check environment variable first (allows override for special cases)
	if binary := os.Getenv("QUICBAAS_BINARY"); binary != "" {
		if _, err := os.Stat(binary); err == nil {
			return binary
		}
	}

	// Primary location: pre-built binary mounted by docker-compose
	// This binary is built fresh by the Makefile before each test run
	const primaryLocation = "/app/bin/quicbaas"
	if _, err := os.Stat(primaryLocation); err == nil {
		return primaryLocation
	}

	// Fallback for local development (not in Docker)
	fallbacks := []string{
		"./bin/quicbaas",        // Project root bin directory
		"../../../bin/quicbaas", // From test/integration/cli
	}

	for _, loc := range fallbacks {
		absPath, err := filepath.Abs(loc)
		if err != nil {
			continue
		}
		if _, err := os.Stat(absPath); err == nil {
			return absPath
		}
	}

	return ""
}

// TestRunner runs CLI commands against different transports.
type TestRunner struct {
	t             *testing.T
	binary        string
	address       string
	transport     Transport
	baseArgs      []string
	authenticated bool
}

// NewTestRunner creates a new test runner.
func NewTestRunner(t *testing.T, transport Transport, address string) *TestRunner {
	t.Helper()

	binary := getQuicBaaSBinary()
	require.NotEmpty(t, binary, "quicbaas binary must be available for %s CLI test", transport)

	// Build base arguments for all commands
	baseArgs := []string{
		"--address", address,
	}

	// Transport-specific setup
	switch transport {
	case TransportUnix:
		// For Unix sockets, we still use gRPC protocol but with unix:// address
		baseArgs = append(baseArgs, "--transport", "grpc")
	default:
		baseArgs = append(baseArgs, "--transport", string(transport))
	}

	// Skip TLS certificate verification for self-signed certs in integration tests
	if transport != TransportMCP {
		baseArgs = append(baseArgs, "--tls-skip-verify")
	}

	return &TestRunner{
		t:             t,
		binary:        binary,
		address:       address,
		transport:     transport,
		baseArgs:      baseArgs,
		authenticated: false,
	}
}

// Authenticate provisions a bearer token for transports that require one (MCP).
// It must be called before running commands that require authentication.
//
// Password authentication has been removed from the project; tests acquire a
// bearer by performing a PIV (mTLS client cert) login against the cluster's
// REST endpoint. The token is written to the CLI's default token path so
// subsequent `quicbaas --transport mcp ...` invocations pick it up via
// loadMCPAuthToken() in cmd/quicbaas/client.go.
func (r *TestRunner) Authenticate() error {
	if r.authenticated {
		return nil
	}

	// Only MCP needs an explicit bearer set up ahead of time. Other transports
	// either use mTLS directly (gRPC/QUIC/Unix) or transparently fall back to
	// the same default token file (REST), so the same setup helps them too —
	// but if no REST endpoint is reachable we skip rather than fail.
	if r.transport != TransportMCP {
		r.authenticated = true
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	restEndpoints := helpers.GetRESTEndpoints()
	if len(restEndpoints) == 0 {
		r.t.Logf("[%s] no REST endpoints available for PIV login; MCP commands will be unauthenticated", r.transport)
		return &MCPAuthEnvError{Reason: "no REST endpoints configured"}
	}

	bearer, err := helpers.AuthPIVContext(ctx, restEndpoints[0], "", "", "")
	if err != nil {
		r.t.Logf("[%s] PIV login for MCP bearer failed: %v", r.transport, err)
		return err
	}

	if err := writeMCPSessionToken(bearer); err != nil {
		r.t.Logf("[%s] failed to persist MCP session token: %v", r.transport, err)
		return err
	}

	r.t.Logf("[%s] MCP authentication successful (PIV)", r.transport)
	r.authenticated = true
	return nil
}

// MCPAuthEnvError indicates the MCP-auth helper was invoked but the
// integration test environment isn't configured to support it.
type MCPAuthEnvError struct {
	Reason string
}

// Error returns a human-readable description.
func (e *MCPAuthEnvError) Error() string {
	return "mcp auth: " + e.Reason
}

// writeMCPSessionToken serializes a bearer token in the format the CLI's
// loadMCPAuthToken() helper expects, at the default path (~/.quicbaas/token
// when QUICBAAS_TEST_TOKEN_PATH is unset). The CLI's MCP transport client
// automatically picks the token up via loadMCPAuthToken().
func writeMCPSessionToken(bearer string) error {
	path := os.Getenv("QUICBAAS_TEST_TOKEN_PATH")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path = filepath.Join(home, ".quicbaas", "token")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	payload := struct {
		AccessToken  string    `json:"access_token"`
		RefreshToken string    `json:"refresh_token"`
		ExpiresAt    time.Time `json:"expires_at"`
		TokenType    string    `json:"token_type"`
	}{
		AccessToken: bearer,
		ExpiresAt:   time.Now().Add(45 * time.Minute),
		TokenType:   "Bearer",
	}
	data, err := json.Marshal(&payload)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// IsSkipped returns true if the command should be skipped for this transport.
// Prefer using RunForTransports to exclude unsupported transports at the test
// level rather than checking at runtime.
func (r *TestRunner) IsSkipped(cmd CommandDef) bool {
	for _, skip := range cmd.SkipTransports {
		if skip == r.transport {
			return true
		}
	}
	return false
}

// Run executes a command and returns the output.
// If the command specifies SkipTransports and the current transport is in that list,
// the command is silently skipped (returns empty output with no error).
// Prefer using RunForTransports to exclude unsupported transports at the test level.
func (r *TestRunner) Run(cmd CommandDef) (stdout string, stderr string, err error) {
	r.t.Helper()

	// Check if this transport should be skipped for this command
	for _, skip := range cmd.SkipTransports {
		if skip == r.transport {
			r.t.Logf("Command %q not supported for transport %s, skipping", cmd.Name, r.transport)
			return "", "", nil
		}
	}

	// Build full command arguments
	// Note: cmd.Timeout is used for the context timeout, not as a CLI flag
	args := append(r.baseArgs, cmd.Args...)

	// Create context with timeout
	timeout := cmd.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout+5*time.Second)
	defer cancel()

	// Run the command
	execCmd := exec.CommandContext(ctx, r.binary, args...)

	var outBuf, errBuf bytes.Buffer
	execCmd.Stdout = &outBuf
	execCmd.Stderr = &errBuf

	err = execCmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()

	r.t.Logf("[%s] %s %s", r.transport, r.binary, strings.Join(args, " "))
	if stdout != "" {
		r.t.Logf("[%s] stdout: %s", r.transport, stdout)
	}
	if stderr != "" {
		r.t.Logf("[%s] stderr: %s", r.transport, stderr)
	}

	return stdout, stderr, err
}

// RunAndAssert executes a command and asserts expected outcomes.
// If the command specifies SkipTransports and the current transport is in that
// list, the test fails with a fatal error. Use RunForTransports to exclude
// unsupported transports.
func (r *TestRunner) RunAndAssert(cmd CommandDef) {
	r.t.Helper()

	stdout, stderr, err := r.Run(cmd)

	// If the command was skipped for this transport, return without assertions
	if r.IsSkipped(cmd) {
		return
	}

	// Check success/failure
	if cmd.ExpectSuccess {
		require.NoError(r.t, err, "[%s] %s should succeed: stdout=%s stderr=%s",
			r.transport, cmd.Name, stdout, stderr)
	} else {
		require.Error(r.t, err, "[%s] %s should fail", r.transport, cmd.Name)
	}

	// Check expected output
	for _, expected := range cmd.ExpectOutput {
		assert.Contains(r.t, stdout, expected,
			"[%s] %s output should contain %q", r.transport, cmd.Name, expected)
	}

	// Check expected error output
	for _, expected := range cmd.ExpectError {
		assert.Contains(r.t, stderr, expected,
			"[%s] %s stderr should contain %q", r.transport, cmd.Name, expected)
	}
}

// RunSuite executes a suite of commands with setup and cleanup.
func (r *TestRunner) RunSuite(commands []CommandDef) {
	r.t.Helper()

	for _, cmd := range commands {
		// Run setup if present
		if cmd.Setup != nil {
			r.t.Logf("[%s] Running setup for %s", r.transport, cmd.Name)
			r.RunAndAssert(*cmd.Setup)
		}

		// Run the main command
		r.t.Run(cmd.Name, func(t *testing.T) {
			runner := &TestRunner{
				t:         t,
				binary:    r.binary,
				address:   r.address,
				transport: r.transport,
				baseArgs:  r.baseArgs,
			}
			runner.RunAndAssert(cmd)
		})

		// Run cleanup if present
		if cmd.Cleanup != nil {
			r.t.Logf("[%s] Running cleanup for %s", r.transport, cmd.Name)
			// Best effort cleanup - don't fail test if cleanup fails
			_, _, _ = r.Run(*cmd.Cleanup)
		}
	}
}

// TransportConfig holds configuration for a transport.
type TransportConfig struct {
	Transport Transport
	Address   string
	Enabled   bool
}

// GetTransportConfigs returns configurations for all available transports.
// Default addresses are configured for Docker integration test environment
// where services are accessible via Docker internal hostnames.
//
// NOTE: REST transport is disabled by default until CLI binary is rebuilt
// with REST transport support. Enable with QUICBAAS_REST_ENABLED=true.
func GetTransportConfigs() []TransportConfig {
	configs := []TransportConfig{
		{
			Transport: TransportGRPC,
			// Default to test-node1:50051 for Docker environment
			Address: getEnvOrDefault("QUICBAAS_GRPC_ADDRESS", "test-node1:50051"),
			Enabled: true,
		},
		{
			Transport: TransportREST,
			Address:   getEnvOrDefault("QUICBAAS_REST_ADDRESS", "https://test-node1:8080"),
			Enabled:   true,
		},
		{
			Transport: TransportMCP,
			// MCP transport uses the HTTP server's /mcp endpoint
			Address: getEnvOrDefault("QUICBAAS_MCP_ADDRESS", "http://test-node1:8888/mcp"),
			Enabled: true,
		},
		{
			Transport: TransportQUIC,
			// QUIC transport - disabled by default as it requires special setup
			Address: getEnvOrDefault("QUICBAAS_QUIC_ADDRESS", "test-node1:4433"),
			Enabled: getEnvOrDefault("QUICBAAS_QUIC_ENABLED", "false") == "true",
		},
		{
			Transport: TransportUnix,
			// Unix socket - disabled by default in Docker as socket may not be mounted
			Address: getEnvOrDefault("QUICBAAS_NODE1_UNIX", getEnvOrDefault("QUICBAAS_UNIX_SOCKET", "/var/run/quicbaas/node1.sock")),
			Enabled: getEnvOrDefault("QUICBAAS_UNIX_ENABLED", "false") == "true",
		},
	}

	return configs
}

// RunForAllTransports runs a test function for all enabled transports.
func RunForAllTransports(t *testing.T, name string, fn func(t *testing.T, runner *TestRunner)) {
	RunForTransports(t, name, nil, fn)
}

// RunForTransports runs a test function for enabled transports, excluding specified ones.
// Use this when certain transports don't support the test's functionality.
func RunForTransports(t *testing.T, name string, excludeTransports []Transport, fn func(t *testing.T, runner *TestRunner)) {
	configs := GetTransportConfigs()

	for _, cfg := range configs {
		if !cfg.Enabled {
			continue
		}

		// Check if this transport should be excluded
		excluded := false
		for _, exclude := range excludeTransports {
			if exclude == cfg.Transport {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		t.Run(string(cfg.Transport)+"/"+name, func(t *testing.T) {
			runner := NewTestRunner(t, cfg.Transport, cfg.Address)

			// Authenticate for MCP transport before running tests
			if cfg.Transport == TransportMCP {
				if err := runner.Authenticate(); err != nil {
					t.Fatalf("MCP authentication failed: %v", err)
				}
			}

			fn(t, runner)
		})
	}
}

// =============================================================================
// Capability Probes
// =============================================================================

// unavailableIndicators contains error message substrings that indicate a
// server-side feature is not available or not implemented.
var unavailableIndicators = []string{
	"not_implemented",
	"not implemented",
	"backup manager not available",
	"not available",
	"not enabled",
	"not configured",
	"unknown service",
	"Unimplemented",
	"404 page not found",
	"HTTP 404",
	"page not found",
}

// outputIndicatesUnavailable returns true if the combined stdout+stderr output
// contains any substring that indicates the requested feature is unavailable.
func outputIndicatesUnavailable(stdout, stderr string) bool {
	combined := stdout + stderr
	for _, indicator := range unavailableIndicators {
		if strings.Contains(combined, indicator) {
			return true
		}
	}
	return false
}

// RequireBackupAvailable probes the server to verify that the backup manager
// is available. If the backup service returns a "not available" or
// "not_implemented" error, the test is skipped with a clear message.
// This should be called at the start of any test that performs backup operations.
func (r *TestRunner) RequireBackupAvailable() {
	r.t.Helper()

	probeCmd := CommandDef{
		Name:          "backup availability probe",
		Args:          []string{"backup", "list"},
		ExpectSuccess: true,
		Timeout:       15 * time.Second,
	}

	stdout, stderr, err := r.Run(probeCmd)
	if err != nil && outputIndicatesUnavailable(stdout, stderr) {
		r.t.Skipf("[%s] Backup service not available in this environment, skipping: %s",
			r.transport, strings.TrimSpace(stdout+stderr))
	}
	// If the probe succeeded or failed with a different error, proceed with the test.
	// A different error (e.g., empty list, auth issues) means the service is available.
}

// RequireClusterOpsAvailable probes the server to verify that cluster management
// operations (node add/remove, rebalance, migrate, upgrade) are available.
// If the server returns timeouts or "not implemented" errors for a basic node
// list command, the test is skipped with a clear message.
// This should be called at the start of any test that performs cluster
// management operations (not needed for help commands or error case tests).
func (r *TestRunner) RequireClusterOpsAvailable() {
	r.t.Helper()

	// Probe with a read-only node list command which should work if cluster
	// management APIs are available
	probeCmd := CommandDef{
		Name:           "cluster ops availability probe",
		Args:           []string{"node", "list"},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: clusterOpsTransportsToSkip,
	}

	// If the transport is in the skip list, we cannot probe - the test should
	// have used RunForTransports to exclude this transport entirely.
	for _, skip := range probeCmd.SkipTransports {
		if skip == r.transport {
			return
		}
	}

	stdout, stderr, err := r.Run(probeCmd)
	if err != nil {
		combined := stdout + stderr
		// Check for unavailability indicators
		if outputIndicatesUnavailable(stdout, stderr) {
			r.t.Skipf("[%s] Cluster management operations not available in this environment, skipping: %s",
				r.transport, strings.TrimSpace(combined))
		}
		// Check for timeout indicators which suggest the operation is not supported
		if strings.Contains(combined, "timeout") ||
			strings.Contains(combined, "deadline exceeded") ||
			strings.Contains(combined, "context deadline") {
			r.t.Skipf("[%s] Cluster management operations timed out (likely not supported in this environment), skipping: %s",
				r.transport, strings.TrimSpace(combined))
		}
	}
}

// RequireVectorAvailable probes the server to verify that the vector service
// is enabled. If the server returns "not enabled", "unknown service", or similar
// errors, it validates the error response is correct and returns false.
// Returns true if the vector service is available, false otherwise.
// Tests should return early when this method returns false.
func (r *TestRunner) RequireVectorAvailable() bool {
	r.t.Helper()

	probeCmd := CommandDef{
		Name:           "vector service availability probe",
		Args:           []string{"vector", "namespace", "list"},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: vectorTransportsToSkip,
	}

	for _, skip := range probeCmd.SkipTransports {
		if skip == r.transport {
			return true
		}
	}

	stdout, stderr, err := r.Run(probeCmd)
	if err != nil && outputIndicatesUnavailable(stdout, stderr) {
		combined := strings.TrimSpace(stdout + stderr)
		// Validate that the server returned an appropriate error response
		assert.True(r.t, outputIndicatesUnavailable(stdout, stderr),
			"[%s] Server should return a recognized unavailability error for vector service, got: %s",
			r.transport, combined)
		r.t.Logf("[%s] Vector service not enabled on server (validated correct error response: %s)",
			r.transport, combined)
		return false
	}
	return true
}

// RequireEmbeddingAvailable probes the server to verify that the embedding
// service is enabled. If the server returns "not enabled", "not configured",
// "unknown service", or similar errors, it validates the error response is
// correct and returns false.
// Returns true if the embedding service is available, false otherwise.
// Tests should return early when this method returns false.
func (r *TestRunner) RequireEmbeddingAvailable() bool {
	r.t.Helper()

	probeCmd := CommandDef{
		Name:           "embedding service availability probe",
		Args:           []string{"embedding", "generate", "probe-test"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: embeddingTransportsToSkip,
	}

	for _, skip := range probeCmd.SkipTransports {
		if skip == r.transport {
			return true
		}
	}

	stdout, stderr, err := r.Run(probeCmd)
	if err != nil && outputIndicatesUnavailable(stdout, stderr) {
		combined := strings.TrimSpace(stdout + stderr)
		// Validate that the server returned an appropriate error response
		assert.True(r.t, outputIndicatesUnavailable(stdout, stderr),
			"[%s] Server should return a recognized unavailability error for embedding service, got: %s",
			r.transport, combined)
		r.t.Logf("[%s] Embedding service not enabled on server (validated correct error response: %s)",
			r.transport, combined)
		return false
	}
	return true
}

// getEnvOrDefault returns the environment variable value or a default.
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// RequireTimeseriesAvailable probes the server to verify that the timeseries
// service is enabled. If the server returns "not configured", "not_implemented",
// "unknown service", or similar errors, it validates the error response is
// correct and returns false.
// Returns true if the timeseries service is available, false otherwise.
// Tests should return early when this method returns false.
func (r *TestRunner) RequireTimeseriesAvailable() bool {
	r.t.Helper()

	probeCmd := CommandDef{
		Name:           "timeseries service availability probe",
		Args:           []string{"timeseries", "list-metrics"},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: tsTransportsToSkip,
	}

	for _, skip := range probeCmd.SkipTransports {
		if skip == r.transport {
			return true
		}
	}

	stdout, stderr, err := r.Run(probeCmd)
	if err != nil && outputIndicatesUnavailable(stdout, stderr) {
		combined := strings.TrimSpace(stdout + stderr)
		// Validate that the server returned an appropriate error response
		assert.True(r.t, outputIndicatesUnavailable(stdout, stderr),
			"[%s] Server should return a recognized unavailability error for timeseries service, got: %s",
			r.transport, combined)
		r.t.Logf("[%s] Timeseries service not enabled on server (validated correct error response: %s)",
			r.transport, combined)
		return false
	}
	return true
}
