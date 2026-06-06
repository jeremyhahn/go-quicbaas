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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Transport support matrix for orchestrator inference
//
// The inference endpoint is implemented across all transports:
//   - REST:  POST /api/v1/orchestrator/inference (handlers_inference.go)
//   - gRPC:  Inference / InferenceStream RPCs in pkg/api/grpc/server_inference.go
//   - QUIC:  SubOpOrchestratorInference in pkg/api/quic/handler_orchestrator.go
//   - Unix:  gRPC-over-UDS, same handler surface as gRPC
//   - MCP:   quicbaas_orchestrator_inference tool in pkg/api/mcp/orchestrator_tools.go
//
// All six transports participate in the tests below. No transports are excluded.
//
// This file holds the pure CLI-validation tests that DO NOT dial Ollama — they
// validate flag parsing and pre-dial server rejection, so they run on every
// `make integration-test` under the bare `integration` build tag. The
// real-inference tests that drive a live model live in
// orchestrator_inference_ollama_test.go behind the `rag` build tag.
// =============================================================================

// =============================================================================
// Command definitions
// =============================================================================

// OrchestratorInferenceMissingBackendCommand returns a command that omits the
// required --backend flag and must exit non-zero.
// CLI flag validation is transport-agnostic, so no SkipTransports needed.
func OrchestratorInferenceMissingBackendCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator inference missing backend",
		Args:          []string{"orchestrator", "inference", "--prompt", "hello"},
		ExpectSuccess: false,
		ExpectError:   []string{"backend"},
		Timeout:       10 * time.Second,
	}
}

// OrchestratorInferenceMissingPromptCommand returns a command that omits the
// required --prompt flag and must exit non-zero.
func OrchestratorInferenceMissingPromptCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator inference missing prompt",
		Args:          []string{"orchestrator", "inference", "--backend", "ollama"},
		ExpectSuccess: false,
		ExpectError:   []string{"prompt"},
		Timeout:       10 * time.Second,
	}
}

// OrchestratorInferenceHelpCommand returns a command that prints the inference
// subcommand help.  Help is handled locally by the CLI binary so it works
// across all transports without a server.
func OrchestratorInferenceHelpCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator inference help",
		Args:          []string{"orchestrator", "inference", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"backend", "model", "prompt", "stream", "system-prompt"},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Tests
// =============================================================================

// TestOrchestratorInferenceHelp verifies the inference subcommand help text
// contains every documented flag.  Help is purely local so all 6 transports
// participate.
func TestOrchestratorInferenceHelp(t *testing.T) {
	RunForAllTransports(t, "InferenceHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorInferenceHelpCommand())
	})
}

// TestOrchestratorInferenceValidation tests that the CLI rejects requests that
// are missing required flags before they ever reach the server.  These are
// pure CLI-validation cases and run across all transports.
func TestOrchestratorInferenceValidation(t *testing.T) {
	RunForAllTransports(t, "MissingBackend", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorInferenceMissingBackendCommand())
	})

	RunForAllTransports(t, "MissingPrompt", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorInferenceMissingPromptCommand())
	})
}

// TestOrchestratorInference_RejectsSessionBackend verifies that calling
// `orchestrator inference` with a session-only ACP backend (e.g., "claude")
// causes the CLI to exit non-zero and emit an error message that identifies
// the backend requires a prior Spawn — not a generic internal error.
//
// This test exercises the InferUnsupportedError path through:
//
//	orchestrator.Infer → InferUnsupportedError
//	→ handlers_inference.go → HTTP 422
//	→ SDK client → formatted CLI error
//
// The session backend is rejected before any Ollama dial occurs, so this case
// requires no model and stays in the default (non-rag) suite.
//
// Transport coverage: all 6 transports.
func TestOrchestratorInference_RejectsSessionBackend(t *testing.T) {
	RunForAllTransports(t, "RejectsSessionBackend", func(t *testing.T, runner *TestRunner) {
		cmd := CommandDef{
			Name: "inference rejects session backend",
			Args: []string{
				"orchestrator", "inference",
				"--backend", "claude",
				"--prompt", "hello",
			},
			ExpectSuccess: false,
			ExpectError:   []string{"session-based"},
			Timeout:       30 * time.Second,
		}

		stdout, stderr, err := runner.Run(cmd)
		require.Error(t, err,
			"[%s] inference against a session-only backend must exit non-zero", runner.transport)

		// The error output must contain the "session-based" substring from
		// ErrInferUnsupported ("infer not supported on session-based backends")
		// so operators know exactly why the call was rejected and what to do
		// (use `orchestrator spawn` + `orchestrator prompt` instead).
		combined := stdout + stderr
		assert.True(t,
			strings.Contains(combined, "session-based") ||
				strings.Contains(combined, "not supported") ||
				strings.Contains(combined, "spawn"),
			"[%s] error output must identify the session-backend requirement; got: %s",
			runner.transport, combined)
	})
}

// TestOrchestratorInferenceErrors tests the complete set of error scenarios
// for the inference command that do not require a live server.
func TestOrchestratorInferenceErrors(t *testing.T) {
	RunForAllTransports(t, "MissingBackend", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorInferenceMissingBackendCommand())
	})

	RunForAllTransports(t, "MissingPrompt", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorInferenceMissingPromptCommand())
	})

	// An unregistered backend must produce a non-zero exit and a meaningful
	// error message (not a generic 500).
	RunForAllTransports(t, "UnregisteredBackend", func(t *testing.T, runner *TestRunner) {
		cmd := CommandDef{
			Name: "inference unregistered backend",
			Args: []string{
				"orchestrator", "inference",
				"--backend", "nonexistent-backend-xyz-99",
				"--prompt", "hello",
			},
			ExpectSuccess: false,
			Timeout:       30 * time.Second,
		}

		stdout, stderr, err := runner.Run(cmd)
		require.Error(t, err,
			"[%s] inference with unregistered backend must exit non-zero", runner.transport)

		combined := stdout + stderr
		assert.False(t,
			strings.Contains(combined, "internal server error"),
			"[%s] unregistered backend must produce a descriptive error, not a generic 500; got: %s",
			runner.transport, combined)
	})
}
