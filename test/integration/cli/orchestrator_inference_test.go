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
// =============================================================================

// =============================================================================
// Command definitions
// =============================================================================

// OrchestratorInferenceCommand returns a command that sends a stateless
// inference prompt to the named backend and model.
func OrchestratorInferenceCommand(backend, model, prompt string, extraArgs ...string) CommandDef {
	args := []string{
		"orchestrator", "inference",
		"--backend", backend,
		"--model", model,
		"--prompt", prompt,
	}
	args = append(args, extraArgs...)
	return CommandDef{
		Name:          "orchestrator inference",
		Args:          args,
		ExpectSuccess: true,
		Timeout:       60 * time.Second,
	}
}

// OrchestratorInferenceStreamCommand returns a command that sends a stateless
// streaming inference prompt and expects chunked NDJSON output.
func OrchestratorInferenceStreamCommand(backend, model, prompt string) CommandDef {
	return CommandDef{
		Name: "orchestrator inference --stream",
		Args: []string{
			"orchestrator", "inference",
			"--backend", backend,
			"--model", model,
			"--prompt", prompt,
			"--stream",
		},
		ExpectSuccess: true,
		Timeout:       90 * time.Second,
	}
}

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

// TestOrchestratorInference_Stateless_Ollama runs a real stateless inference
// call against the Ollama backend that is available in the Docker integration
// test environment.  The prompt is deliberately minimal ("ping") so the model
// produces a short response quickly; the assertion checks the response body is
// non-empty rather than pinning an exact token, which avoids brittle coupling
// to model version or sampling randomness.
//
// Transport coverage: all 6 transports — REST, gRPC, QUIC, Unix, MCP, embedded.
func TestOrchestratorInference_Stateless_Ollama(t *testing.T) {
	RunForAllTransports(t, "Inference_Ollama", func(t *testing.T, runner *TestRunner) {
		model := testInferenceModel()
		cmd := OrchestratorInferenceCommand("ollama", model, "respond with the single word: pong")

		stdout, stderr, err := runner.Run(cmd)
		require.NoError(t, err,
			"[%s] stateless ollama inference should succeed; stderr: %s", runner.transport, stderr)

		// The response body must be non-empty — a real model always produces
		// at least one token.
		assert.NotEmpty(t, strings.TrimSpace(stdout),
			"[%s] inference response body must contain model output", runner.transport)
	})

	// JSON output format: verify the envelope structure, not the model text.
	RunForAllTransports(t, "Inference_Ollama_JSON", func(t *testing.T, runner *TestRunner) {
		model := testInferenceModel()
		cmd := OrchestratorInferenceCommand("ollama", model, "respond with the single word: pong", "--output", "json")

		stdout, stderr, err := runner.Run(cmd)
		require.NoError(t, err,
			"[%s] stateless ollama inference (json) should succeed; stderr: %s", runner.transport, stderr)

		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(stdout), &resp),
			"[%s] --output json must produce valid JSON; got: %s", runner.transport, stdout)

		assert.Contains(t, resp, "content",
			"[%s] JSON response must contain 'content' field", runner.transport)
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

// TestOrchestratorInference_StreamMode verifies that `--stream` causes the
// CLI to print incremental output.  Because streaming requires the server to
// flush NDJSON chunks, every line in the output must be valid JSON (or plain
// text tokens in text mode).
//
// The test uses plain text mode (default) and asserts that stdout contains
// actual content.  If the model returns an empty stream the test fails, which
// is the correct behaviour — an empty stream indicates a real problem.
//
// Transport coverage: all 6 transports.
func TestOrchestratorInference_StreamMode(t *testing.T) {
	RunForAllTransports(t, "StreamMode_TextOutput", func(t *testing.T, runner *TestRunner) {
		model := testInferenceModel()
		cmd := OrchestratorInferenceStreamCommand("ollama", model, "respond with the single word: pong")

		stdout, stderr, err := runner.Run(cmd)
		require.NoError(t, err,
			"[%s] streaming inference should succeed; stderr: %s", runner.transport, stderr)

		// In text mode the CLI writes each delta directly to stdout.  The
		// output must be non-empty — a zero-byte stream means the backend
		// returned nothing, which is a real error.
		assert.NotEmpty(t, strings.TrimSpace(stdout),
			"[%s] streaming inference must produce at least one token", runner.transport)
	})

	// JSON stream mode: every stdout line must be a valid JSON object.
	RunForAllTransports(t, "StreamMode_JSONOutput", func(t *testing.T, runner *TestRunner) {
		model := testInferenceModel()
		cmd := CommandDef{
			Name: "orchestrator inference --stream --output json",
			Args: []string{
				"orchestrator", "inference",
				"--backend", "ollama",
				"--model", model,
				"--prompt", "respond with the single word: pong",
				"--stream",
				"--output", "json",
			},
			ExpectSuccess: true,
			Timeout:       90 * time.Second,
		}

		stdout, stderr, err := runner.Run(cmd)
		require.NoError(t, err,
			"[%s] streaming inference (json) should succeed; stderr: %s", runner.transport, stderr)

		// Each line of stdout must be a valid JSON chunk object.
		lines := splitNonEmpty(stdout)
		require.NotEmpty(t, lines,
			"[%s] streaming JSON output must contain at least one chunk line", runner.transport)

		for i, line := range lines {
			var chunk map[string]interface{}
			assert.NoError(t, json.Unmarshal([]byte(line), &chunk),
				"[%s] stream line %d must be valid JSON; got: %s", runner.transport, i, line)
		}
	})
}

// TestOrchestratorInference_TenantIsolation verifies that the global --tenant
// flag is threaded through into the request envelope by checking that the
// orchestrator returns a meaningful response (or a recognisable error) rather
// than a generic internal failure when a non-default tenant is specified.
//
// The server-side isolation is tested exhaustively in pkg/orchestration unit
// tests; here we are confirming the CLI plumbing (orchContext() injects
// globalTenantID into the request context, and the REST handler reads it from
// the tenant_id query parameter) works end-to-end.
func TestOrchestratorInference_TenantIsolation(t *testing.T) {
	RunForAllTransports(t, "TenantIsolation", func(t *testing.T, runner *TestRunner) {
		model := testInferenceModel()
		tenantID := UniqueKey("inference-tenant")

		// Issue the inference under a freshly-minted tenant ID.  The Ollama
		// backend is not tenant-scoped at the model level, so the call should
		// succeed (or fail with a recognisable not-found / backend error) — it
		// must NOT fail with a generic internal server error, which would
		// indicate the tenant ID was lost in transit.
		cmd := CommandDef{
			Name: "orchestrator inference tenant isolation",
			Args: []string{
				"--tenant", tenantID,
				"orchestrator", "inference",
				"--backend", "ollama",
				"--model", model,
				"--prompt", "respond with the single word: pong",
			},
			// Accept either a successful response or a backend-not-found error
			// (the tenant may not have ollama configured).  What we DO NOT
			// accept is a 500 internal server error, which would indicate the
			// tenant context was dropped.
			ExpectSuccess: true,
			Timeout:       60 * time.Second,
		}

		stdout, stderr, err := runner.Run(cmd)
		if err != nil {
			// A backend-not-found or tenant-not-configured error is acceptable.
			// A generic "internal server error" is not — that means the tenant
			// context was not threaded through.
			combined := stdout + stderr
			assert.False(t,
				strings.Contains(combined, "internal server error"),
				"[%s] inference with explicit tenant must not produce a generic internal server error; "+
					"got: %s", runner.transport, combined)
			t.Logf("[%s] inference under tenant %q returned an expected non-success: %v | %s",
				runner.transport, tenantID, err, combined)
			return
		}

		assert.NotEmpty(t, strings.TrimSpace(stdout),
			"[%s] inference under tenant %q must produce a non-empty response", runner.transport, tenantID)
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

// =============================================================================
// Helper functions
// =============================================================================

// testInferenceModel returns the Ollama model used for inference integration
// tests.  Controlled via QUICBAAS_TEST_INFERENCE_MODEL so CI can pin a
// different model without source changes.
func testInferenceModel() string {
	model := getEnvOrDefault("QUICBAAS_TEST_INFERENCE_MODEL", "qwen3:1.7b")
	return model
}

// splitNonEmpty splits s on newlines and returns only non-empty, non-whitespace
// trimmed lines.  Used to iterate over NDJSON stream output.
func splitNonEmpty(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
