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

// Package helpers provides test utilities for integration tests.
package helpers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Transport represents a CLI transport protocol.
type Transport string

const (
	// TransportGRPC uses gRPC protocol (default).
	TransportGRPC Transport = "grpc"
	// TransportREST uses REST/HTTP protocol.
	TransportREST Transport = "rest"
	// TransportQUIC uses QUIC protocol.
	TransportQUIC Transport = "quic"
	// TransportUnix uses gRPC over Unix Domain Socket.
	TransportUnix Transport = "unix"
	// TransportMCP uses Model Context Protocol (JSON-RPC 2.0 over HTTP).
	TransportMCP Transport = "mcp"
	// TransportEmbedded uses in-process direct service access.
	TransportEmbedded Transport = "embedded"
)

// AllTransports returns all supported transports for testing.
func AllTransports() []Transport {
	return []Transport{
		TransportEmbedded,
		TransportGRPC,
		TransportREST,
		TransportQUIC,
		TransportUnix,
		TransportMCP,
	}
}

// CLIExecutor executes QuicBaaS CLI commands with a specific transport.
type CLIExecutor struct {
	binaryPath string
	transport  Transport
	address    string
	timeout    time.Duration
	extraArgs  []string
}

// CLIExecutorOption is a functional option for CLIExecutor.
type CLIExecutorOption func(*CLIExecutor)

// WithAddress sets the server address for the CLI executor.
func WithAddress(addr string) CLIExecutorOption {
	return func(e *CLIExecutor) {
		e.address = addr
	}
}

// WithCLITimeout sets the command timeout.
func WithCLITimeout(d time.Duration) CLIExecutorOption {
	return func(e *CLIExecutor) {
		e.timeout = d
	}
}

// WithExtraArgs adds extra arguments to all CLI commands.
func WithExtraArgs(args ...string) CLIExecutorOption {
	return func(e *CLIExecutor) {
		e.extraArgs = append(e.extraArgs, args...)
	}
}

var (
	cliBinaryPath     string
	cliBinaryOnce     sync.Once
	cliBinaryBuildErr error
)

// buildCLIBinary builds the CLI binary if not already built.
func buildCLIBinary() (string, error) {
	cliBinaryOnce.Do(func() {
		// Check if a pre-built binary exists in common locations
		possiblePaths := []string{
			os.Getenv("QUICBAAS_CLI_PATH"),
			"/usr/local/bin/quicbaas",
			"/usr/bin/quicbaas",
			"./quicbaas",
			"../../cmd/quicbaas/quicbaas",
		}

		for _, path := range possiblePaths {
			if path == "" {
				continue
			}
			if _, err := os.Stat(path); err == nil {
				cliBinaryPath = path
				return
			}
		}

		// Build the binary in a temp directory
		tempDir, err := os.MkdirTemp("", "quicbaas-cli-test-*")
		if err != nil {
			cliBinaryBuildErr = fmt.Errorf("failed to create temp dir: %w", err)
			return
		}

		binaryPath := tempDir + "/quicbaas"

		// Build with all necessary tags
		cmd := exec.Command("go", "build", "-tags", "mcp,quic", "-o", binaryPath, "./cmd/quicbaas/...")

		// Find project root by looking for go.mod
		projectRoot := findProjectRoot()
		if projectRoot != "" {
			cmd.Dir = projectRoot
		}

		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			cliBinaryBuildErr = fmt.Errorf("failed to build CLI: %w: %s", err, stderr.String())
			return
		}

		cliBinaryPath = binaryPath
	})

	return cliBinaryPath, cliBinaryBuildErr
}

// findProjectRoot finds the project root by looking for go.mod.
func findProjectRoot() string {
	// Start from current directory and walk up
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		if _, err := os.Stat(dir + "/go.mod"); err == nil {
			return dir
		}

		parent := dir[:strings.LastIndex(dir, "/")]
		if parent == dir || parent == "" {
			break
		}
		dir = parent
	}

	return ""
}

// NewCLIExecutor creates a new CLI executor for the given transport.
func NewCLIExecutor(t *testing.T, transport Transport, opts ...CLIExecutorOption) (*CLIExecutor, error) {
	binaryPath, err := buildCLIBinary()
	if err != nil {
		return nil, fmt.Errorf("failed to get CLI binary: %w", err)
	}

	e := &CLIExecutor{
		binaryPath: binaryPath,
		transport:  transport,
		timeout:    30 * time.Second,
	}

	// Set default address based on transport
	switch transport {
	case TransportEmbedded:
		e.address = "" // Embedded doesn't need an address
	case TransportGRPC:
		e.address = GetClusterEndpoints()[0]
	case TransportREST:
		e.address = GetRESTEndpoints()[0]
	case TransportQUIC:
		e.address = GetQUICEndpoints()[0]
	case TransportUnix:
		e.address = os.Getenv("QUICBAAS_NODE1_UNIX")
		if e.address == "" {
			e.address = "/var/run/quicbaas/node1.sock"
		}
	case TransportMCP:
		e.address = GetMCPEndpoints()[0]
	}

	for _, opt := range opts {
		opt(e)
	}

	// Register cleanup
	t.Cleanup(func() {
		// Cleanup is handled at package level for the binary
	})

	return e, nil
}

// CLIResult represents the result of a CLI command execution.
type CLIResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// JSON parses the stdout as JSON into the given target.
func (r *CLIResult) JSON(target interface{}) error {
	return json.Unmarshal([]byte(r.Stdout), target)
}

// Execute runs a CLI command with the configured transport.
func (e *CLIExecutor) Execute(ctx context.Context, args ...string) (*CLIResult, error) {
	// Build the full command arguments
	fullArgs := []string{
		"--transport", string(e.transport),
		"--timeout", e.timeout.String(),
	}

	// Add address if not embedded
	if e.transport != TransportEmbedded && e.address != "" {
		fullArgs = append(fullArgs, "--address", e.address)
	}

	// Skip TLS verification for integration tests with self-signed certs
	fullArgs = append(fullArgs, "--tls-skip-verify")

	// Add extra args
	fullArgs = append(fullArgs, e.extraArgs...)

	// Add the command args
	fullArgs = append(fullArgs, args...)

	// Create command with context
	execCtx, cancel := context.WithTimeout(ctx, e.timeout+5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, e.binaryPath, fullArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &CLIResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if exitError, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitError.ExitCode()
	} else if err != nil {
		return result, fmt.Errorf("command execution failed: %w", err)
	}

	return result, nil
}

// Transport returns the transport type of this executor.
func (e *CLIExecutor) Transport() Transport {
	return e.transport
}

// Name returns the human-readable name of this executor's transport.
func (e *CLIExecutor) Name() string {
	return string(e.transport)
}

// IsAvailable checks if the transport endpoint is available.
func (e *CLIExecutor) IsAvailable(ctx context.Context) bool {
	// For embedded, always available
	if e.transport == TransportEmbedded {
		return true
	}

	// For other transports, check if the endpoint is reachable
	switch e.transport {
	case TransportGRPC:
		endpoints := GetClusterEndpoints()
		return len(endpoints) > 0
	case TransportREST:
		endpoints := GetRESTEndpoints()
		return len(endpoints) > 0
	case TransportQUIC:
		endpoints := GetQUICEndpoints()
		return len(endpoints) > 0
	case TransportUnix:
		socketPath := os.Getenv("QUICBAAS_NODE1_UNIX")
		if socketPath == "" {
			socketPath = "/var/run/quicbaas/node1.sock"
		}
		_, err := os.Stat(socketPath)
		return err == nil
	case TransportMCP:
		endpoints := GetMCPEndpoints()
		return len(endpoints) > 0
	}

	return false
}

// RunMultiProtocolTest is a helper that runs a test function across all available transports.
func RunMultiProtocolTest(t *testing.T, testFn func(t *testing.T, executor *CLIExecutor)) {
	ctx := context.Background()

	for _, transport := range AllTransports() {
		t.Run(string(transport), func(t *testing.T) {
			executor, err := NewCLIExecutor(t, transport)
			require.NoError(t, err, "%s: failed to create executor", transport)

			require.True(t, executor.IsAvailable(ctx), "%s: endpoint must be available", transport)

			testFn(t, executor)
		})
	}
}
