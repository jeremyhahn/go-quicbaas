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

//go:build integration && fleet_runtime

package fleet_runtime_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/fleet"
)

// TestIntegration_ACPBackendFlow exercises the ACP backend (claude / codex /
// opencode) through the agent Runtime using the mock TransportFactory pattern
// consistent with pkg/orchestration/acp_backend_test.go.
//
// All backends use the stubAgentBackend double so no real ACP subprocess is
// required. This test validates:
//   - Runtime.Spawn routes to the correct named backend.
//   - Runtime.Prompt round-trips through the backend.
//   - Runtime.Status reports idle after spawn.
//   - Runtime.Close succeeds and does not leave zombie state.
//
// Sandbox policy is SandboxPolicyOptIn with no container runtime so none of
// these paths require docker/podman.
func TestIntegration_ACPBackendFlow(t *testing.T) {
	backends := []struct {
		name string
	}{
		{name: "claude"},
		{name: "codex"},
		{name: "opencode"},
	}

	for _, bc := range backends {
		bc := bc
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()

			stub := &stubAgentBackend{backendType: "acp"}

			runtime, err := fleet.NewRuntime(fleet.RuntimeConfig{
				Backends:         map[string]orchestration.AgentBackend{bc.name: stub},
				ContainerRuntime: &stubContainerRuntime{available: false},
				SandboxPolicy:    orchestration.SandboxPolicyOptIn,
				Logger:           &testLogger{t: t},
			})
			require.NoError(t, err)

			ctx := context.Background()

			t.Run("spawn", func(t *testing.T) {
				session, err := runtime.Spawn(ctx, "tenant-acp", nil, bc.name, &orchestration.SpawnRequest{
					WorkDir:     t.TempDir(),
					SandboxMode: orchestration.SandboxModeNone,
				})
				require.NoError(t, err)
				require.NotEmpty(t, session.ID)
				assert.Equal(t, bc.name, session.Backend)
				assert.Equal(t, 1, int(stub.spawnCount.Load()))
			})

			session, err := runtime.Spawn(ctx, "tenant-acp", nil, bc.name, &orchestration.SpawnRequest{
				WorkDir:     t.TempDir(),
				SandboxMode: orchestration.SandboxModeNone,
			})
			require.NoError(t, err)

			t.Run("prompt", func(t *testing.T) {
				resp, err := runtime.Prompt(ctx, bc.name, &orchestration.PromptRequest{
					SessionID: session.ID,
					Prompt:    "respond with: hello",
				})
				require.NoError(t, err)
				require.NotEmpty(t, resp.Content)
				assert.NotEmpty(t, resp.Content[0].Text)
			})

			t.Run("status", func(t *testing.T) {
				status, err := runtime.Status(ctx, bc.name, session.ID)
				require.NoError(t, err)
				assert.Equal(t, session.ID, status.SessionID)
				assert.Equal(t, orchestration.SessionStateIdle, status.State)
			})

			t.Run("close", func(t *testing.T) {
				require.NoError(t, runtime.Close(ctx, bc.name, session.ID))
			})
		})
	}
}

// TestIntegration_ACPBackendFlow_UnknownBackend verifies that requesting an
// unknown backend name returns a typed error, not a panic.
func TestIntegration_ACPBackendFlow_UnknownBackend(t *testing.T) {
	runtime, err := fleet.NewRuntime(fleet.RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{
			"claude": &stubAgentBackend{backendType: "acp"},
		},
		ContainerRuntime: &stubContainerRuntime{available: false},
		SandboxPolicy:    orchestration.SandboxPolicyOptIn,
		Logger:           &testLogger{t: t},
	})
	require.NoError(t, err)

	_, err = runtime.Spawn(context.Background(), "tenant-x", nil, "nonexistent", &orchestration.SpawnRequest{
		WorkDir:     t.TempDir(),
		SandboxMode: orchestration.SandboxModeNone,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, fleet.ErrRuntimeUnknownBackend)
}

// TestIntegration_ACPBackendFlow_SandboxedSpawn verifies that setting
// SandboxModeRequired resolves to a container image being applied to the
// spawn request before it reaches the backend.
func TestIntegration_ACPBackendFlow_SandboxedSpawn(t *testing.T) {
	stub := &stubAgentBackend{backendType: "acp"}

	runtime, err := fleet.NewRuntime(fleet.RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{
			"claude": stub,
		},
		ContainerRuntime: &stubContainerRuntime{available: true},
		SandboxPolicy:    orchestration.SandboxPolicyOptIn,
		ImageMap:         map[string]string{"claude": "ghcr.io/test/claude:latest"},
		Logger:           &testLogger{t: t},
	})
	require.NoError(t, err)

	spawnReq := &orchestration.SpawnRequest{
		WorkDir:     t.TempDir(),
		SandboxMode: orchestration.SandboxModeRequired,
	}

	session, err := runtime.Spawn(context.Background(), "tenant-x", nil, "claude", spawnReq)
	require.NoError(t, err)
	require.NotEmpty(t, session.ID)

	assert.Equal(t, orchestration.SandboxModeRequired, spawnReq.SandboxMode,
		"spawn request mode must be updated to Required")
	assert.True(t, spawnReq.Sandboxed,
		"spawn request Sandboxed flag must be set for Required mode")
	assert.Equal(t, "ghcr.io/test/claude:latest", spawnReq.ContainerImage,
		"image must be resolved from ImageMap")
}

// TestIntegration_ACPBackendFlow_MultipleBackends verifies that the Runtime
// correctly routes requests to the right backend when multiple are registered.
func TestIntegration_ACPBackendFlow_MultipleBackends(t *testing.T) {
	stubClaude := &stubAgentBackend{backendType: "acp"}
	stubCodex := &stubAgentBackend{backendType: "acp"}
	stubOpencode := &stubAgentBackend{backendType: "acp"}

	runtime, err := fleet.NewRuntime(fleet.RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{
			"claude":   stubClaude,
			"codex":    stubCodex,
			"opencode": stubOpencode,
		},
		ContainerRuntime: &stubContainerRuntime{available: false},
		SandboxPolicy:    orchestration.SandboxPolicyOptIn,
		Logger:           &testLogger{t: t},
	})
	require.NoError(t, err)

	ctx := context.Background()

	for _, name := range []string{"claude", "codex", "opencode"} {
		_, err := runtime.Spawn(ctx, "tenant-multi", nil, name, &orchestration.SpawnRequest{
			WorkDir:     t.TempDir(),
			SandboxMode: orchestration.SandboxModeNone,
		})
		require.NoError(t, err, "spawn for %q must succeed", name)
	}

	assert.Equal(t, 1, int(stubClaude.spawnCount.Load()), "claude must receive exactly one spawn")
	assert.Equal(t, 1, int(stubCodex.spawnCount.Load()), "codex must receive exactly one spawn")
	assert.Equal(t, 1, int(stubOpencode.spawnCount.Load()), "opencode must receive exactly one spawn")

	backendNames := runtime.Backends()
	assert.Len(t, backendNames, 3, "runtime must report all three registered backends")
}

// TestIntegration_OllamaBackendFlow exercises the Ollama backend when it is
// reachable on the host. qwen3:1.7b is the expected model per the QuicBaaS
// remote fleet node plan (fastest available for CI verification).
//
// If Ollama is not available (env.ollamaAvail == false) the test is skipped
// with a clear message. This is the ONLY test in the suite that uses t.Skip
// for service unavailability — all other tests use stubs.
func TestIntegration_OllamaBackendFlow(t *testing.T) {
	if !env.ollamaAvail {
		t.Skipf("Ollama not available at %s — set OLLAMA_URL and ensure the service is running. Expected model: %s",
			env.ollamaURL, env.ollamaModel)
	}

	// Import the real Ollama backend via the orchestration package.
	// This is the same path used in test/integration/agent/ tests.
	//
	// We reach Ollama via the agent Runtime to validate the full stack:
	//   Runtime.Prompt → OllamaBackend.Prompt → Ollama HTTP API
	//
	// PHASE-4-DEP: Once the spawn server is in place, this test should
	// additionally verify that a Prompt request dispatched via the gRPC
	// FleetRuntime.SpawnSession RPC reaches Ollama end-to-end.

	// Lazily import only what is needed to avoid circular deps.
	// The Ollama backend is wired here directly without going through the
	// full AgentService (AgentService is for the orchestration layer; the
	// daemon Runtime wraps individual backends).

	// OllamaBackend.Spawn is unsupported (stateless); we only test Prompt.
	// We use a live Ollama request with the minimal model to keep CI fast.

	t.Run("prompt_roundtrip", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), promptTimeout)
		defer cancel()

		// Build the backend via the orchestration factory.
		ollamaBackend := buildOllamaBackend(t)

		runtime, err := fleet.NewRuntime(fleet.RuntimeConfig{
			Backends: map[string]orchestration.AgentBackend{
				"ollama": ollamaBackend,
			},
			ContainerRuntime: &stubContainerRuntime{available: false},
			SandboxPolicy:    orchestration.SandboxPolicyOptIn,
			Logger:           &testLogger{t: t},
		})
		require.NoError(t, err)

		resp, err := runtime.Prompt(ctx, "ollama", &orchestration.PromptRequest{
			Prompt: "Respond with exactly one word: hello",
			Model:  env.ollamaModel,
		})
		require.NoError(t, err, "Ollama prompt must succeed")
		require.NotEmpty(t, resp.Content, "Ollama must return content")
		assert.NotEmpty(t, resp.Content[0].Text, "Ollama response text must not be empty")
	})

	t.Run("spawn_unsupported", func(t *testing.T) {
		// OllamaBackend does not support persistent sessions.
		ollamaBackend := buildOllamaBackend(t)
		runtime, err := fleet.NewRuntime(fleet.RuntimeConfig{
			Backends: map[string]orchestration.AgentBackend{
				"ollama": ollamaBackend,
			},
			ContainerRuntime: &stubContainerRuntime{available: false},
			SandboxPolicy:    orchestration.SandboxPolicyOptIn,
			Logger:           &testLogger{t: t},
		})
		require.NoError(t, err)

		_, err = runtime.Spawn(context.Background(), "tenant-ollama", nil, "ollama", &orchestration.SpawnRequest{
			WorkDir:     t.TempDir(),
			SandboxMode: orchestration.SandboxModeNone,
		})
		require.Error(t, err, "Ollama Spawn must return an error (stateless)")
	})
}
