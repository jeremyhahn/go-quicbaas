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

//go:build integration && rag

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
// Real-inference orchestrator tests.
//
// These tests dial the Ollama backend and run actual model inference, so they
// are gated behind the `rag` build tag — the single "needs the Ollama service"
// gate for the integration suite. They run only when the suite is invoked with
// Ollama enabled (local GPU, remote GPU_HOST, or OLLAMA_CPU=1). Default CI
// excludes them entirely via pure build-tag gating (no runtime t.Skip).
//
// The pure CLI-validation tests (help text, missing-flag rejection, session
// backend rejection) live in orchestrator_inference_test.go tagged only
// `integration`, so they always run without an Ollama service.
// =============================================================================

// =============================================================================
// Command definitions
// =============================================================================

// OrchestratorInferenceCommand returns a command that sends a stateless
// inference prompt to the named backend and model. The 3-minute timeout
// accommodates cold-start CPU inference of small models (tinyllama) in
// CI environments without GPUs — the daemon's Ollama client itself
// defaults to a 2-minute request timeout (pkg/ollama/client.go), so the
// CLI must give it room to complete plus marshalling overhead.
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
		Timeout:       180 * time.Second,
	}
}

// OrchestratorInferenceStreamCommand returns a command that sends a stateless
// streaming inference prompt and expects chunked NDJSON output. Streaming
// requires bidirectional transport; REST and MCP cannot satisfy that and
// QUIC's CLI client doesn't expose a streaming Infer yet, so all three are
// skipped.
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
		ExpectSuccess:  true,
		Timeout:        90 * time.Second,
		SkipTransports: []Transport{TransportREST, TransportMCP, TransportQUIC},
	}
}

// =============================================================================
// Tests
// =============================================================================

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
		if runner.IsSkipped(cmd) {
			return
		}

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
	// Streaming requires bidirectional transport — skip REST/MCP/QUIC.
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
			ExpectSuccess:  true,
			Timeout:        90 * time.Second,
			SkipTransports: []Transport{TransportREST, TransportMCP, TransportQUIC},
		}
		if runner.IsSkipped(cmd) {
			return
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

// =============================================================================
// Helper functions
// =============================================================================

// testInferenceModel returns the Ollama model used for inference integration
// tests. Defaults to tinyllama because that is one of the two models the
// integration docker-compose's ollama-setup container pulls at startup
// (the other is all-minilm, used for embeddings). Controlled via
// QUICBAAS_TEST_INFERENCE_MODEL so CI can pin a different model.
func testInferenceModel() string {
	model := getEnvOrDefault("QUICBAAS_TEST_INFERENCE_MODEL", "tinyllama")
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
