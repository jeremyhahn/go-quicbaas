// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// Package main: coverage for buildFleetBackends.
package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func TestBuildAgentBackends_NoneEmpty(t *testing.T) {
	// EffectiveBackends() returns a fallback when Backends is nil. Test
	// the explicit "empty" path by passing a synthetic kind that does
	// not match either branch in the switch.
	cfg := &fleet.Config{Backends: []fleet.BackendKind{"unknown-kind"}}
	logger := newFleetServeLogger()
	_, err := buildFleetBackends(cfg, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no backends constructed")
}

func TestBuildAgentBackends_ACP(t *testing.T) {
	cfg := &fleet.Config{Backends: []fleet.BackendKind{fleet.BackendACP}}
	logger := newFleetServeLogger()
	backends, err := buildFleetBackends(cfg, logger)
	require.NoError(t, err)
	assert.Contains(t, backends, "claude")
	assert.Contains(t, backends, "codex")
	assert.Contains(t, backends, "opencode")
}

// TestBuildAgentBackends_RefusesABadSandboxUlimit: a ulimit the runtime would
// refuse fails backend construction, not the daemon's first sandboxed spawn.
func TestBuildAgentBackends_RefusesABadSandboxUlimit(t *testing.T) {
	cfg := &fleet.Config{
		Backends:       []fleet.BackendKind{fleet.BackendACP},
		SandboxUlimits: []string{"memlock=2:1"},
	}
	_, err := buildFleetBackends(cfg, newFleetServeLogger())
	require.ErrorIs(t, err, fleet.ErrConfigInvalidSandboxSecurity)
}

// TestBuildAgentBackends_ClaudeRunsTheCodingTierByDefault asserts the claude
// backend names DefaultClaudeModel rather than deferring to the CLI. Claude
// Code's own default is a Sonnet model, below the Opus floor, so an SDLC
// binding that leaves worker.model empty would otherwise run every task on
// the utility tier (go-quicbaas#1012).
func TestBuildAgentBackends_ClaudeRunsTheCodingTierByDefault(t *testing.T) {
	cfg := &fleet.Config{Backends: []fleet.BackendKind{fleet.BackendACP}}
	backends, err := buildFleetBackends(cfg, newFleetServeLogger())
	require.NoError(t, err)

	claude, ok := backends["claude"].(*orchestration.ACPBackend)
	require.True(t, ok, "the claude backend must be an ACP backend")
	assert.Equal(t, orchestration.DefaultClaudeModel, claude.DefaultModel())
}

// TestBuildAgentBackends_CodexKeepsItsAccountDefault asserts the change to the
// claude default does not leak into the reviewer: codex runs whatever its
// account is pinned to, and naming a model here would override that pin.
func TestBuildAgentBackends_CodexKeepsItsAccountDefault(t *testing.T) {
	cfg := &fleet.Config{Backends: []fleet.BackendKind{fleet.BackendACP}}
	backends, err := buildFleetBackends(cfg, newFleetServeLogger())
	require.NoError(t, err)

	codex, ok := backends["codex"].(*orchestration.ACPBackend)
	require.True(t, ok, "the codex backend must be an ACP backend")
	assert.Empty(t, codex.DefaultModel())
}

func TestBuildAgentBackends_Ollama(t *testing.T) {
	cfg := &fleet.Config{Backends: []fleet.BackendKind{fleet.BackendOllama}}
	logger := newFleetServeLogger()
	backends, err := buildFleetBackends(cfg, logger)
	require.NoError(t, err)
	assert.Contains(t, backends, "ollama")
}

func TestStripURLScheme_AllCases(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"host:8443", "host:8443"},
		{"https://host:8443", "host:8443"},
		{"http://host:8443", "host:8443"},
		{"https://host:8443/", "host:8443"},
		{"  https://host:8443  ", "host:8443"},
	}
	for _, tc := range cases {
		got := stripURLScheme(tc.in)
		assert.Equal(t, tc.want, got, "stripURLScheme(%q)", tc.in)
	}
}

// TestBuildAgentRuntime_OpenVINOBackendReceivesTheContainerRuntime is the
// regression that took the NPU off the fleet the moment triage was pointed at
// it.
//
// The openvino backend is not a CLI: it drives a remote model over HTTP and
// reaches the repository through a tool it execs INSIDE a container of its
// own, so it holds the container runtime directly rather than being handed one
// at spawn like the ACP backends. Constructed without it, every sandboxed
// spawn is refused with "a sandboxed openvino session needs a container
// runtime and none is installed" — which on a daemon running
// --sandbox-policy required is every spawn there is. Measured on the burn-in
// node 2026-09-16: every triage job failed at spawn.
func TestBuildAgentRuntime_OpenVINOBackendReceivesTheContainerRuntime(t *testing.T) {
	// NOT PARALLEL: the engine this test resolves is a shim it puts on PATH.
	// The resolver proves an engine by running `<command> version`; on a host
	// with none — the factory's own worker image — the runtime came back nil
	// and this test was trunk-red there (go-quicbaas#62, 2026-09-21). The
	// test supplies the engine it needs rather than requiring one installed.
	engineDir := t.TempDir()
	shim := filepath.Join(engineDir, "docker")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write the engine shim: %v", err)
	}
	t.Setenv("PATH", engineDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ov := orchestration.NewOpenVINOBackend(orchestration.OpenVINOBackendConfig{
		BaseURL:      "http://gpu-host.invalid:9000",
		DefaultModel: "qwen3-8b",
	})
	backends := map[string]orchestration.AgentBackend{"openvino": ov}
	logger := newCaptureLogger()
	cfg := serveRuntimeConfig(fleet.ContainerRuntimeDocker, orchestration.SandboxPolicyOptIn, t.TempDir())

	if _, err := buildFleetRuntime(cfg, backends, logger); err != nil {
		t.Fatalf("buildFleetRuntime: %v", err)
	}
	if !ov.HasContainerRuntime() {
		t.Fatal("the openvino backend was left without a container runtime; every sandboxed spawn would be refused")
	}
}

// TestBuildAgentRuntime_OpenVINOContextDeclarationReachesTheBackend: the
// declaration is the only rung of the context ladder that can be right BEFORE
// the first request, so a flag that stops at the config teaches the backend
// nothing and the first oversized prompt is still a wasted round trip.
func TestBuildAgentRuntime_OpenVINOContextDeclarationReachesTheBackend(t *testing.T) {
	t.Parallel()

	ov := orchestration.NewOpenVINOBackend(orchestration.OpenVINOBackendConfig{
		BaseURL:       "http://gpu-host.invalid:9000",
		DefaultModel:  "qwen3-8b",
		ContextTokens: 8192,
	})
	if got := ov.ContextBound(); got != 8192 {
		t.Fatalf("ContextBound() = %d, want the declared 8192", got)
	}
	// And an undeclared backend stays unbounded until something refuses it.
	bare := orchestration.NewOpenVINOBackend(orchestration.OpenVINOBackendConfig{
		BaseURL: "http://gpu-host.invalid:9000", DefaultModel: "qwen3-8b",
	})
	if got := bare.ContextBound(); got != 0 {
		t.Fatalf("ContextBound() = %d, want 0 when nothing is declared or learned", got)
	}
}

// TestBuildAgentBackends_EveryNamedAgentEndpointIsRegistered pins the property
// that makes switching runtimes a binding edit rather than a redeploy: all of
// them are registered at once, each only when the deployment names a server.
func TestBuildAgentBackends_EveryNamedAgentEndpointIsRegistered(t *testing.T) {
	t.Parallel()

	cfg := &fleet.Config{
		ServerAddr: "https://control.invalid:8443", TenantID: "t", BindAddr: "127.0.0.1:0",
		Backends:          []fleet.BackendKind{fleet.BackendACP},
		OpenVINOBaseURL:   "http://npu-host.invalid:9000",
		OpenVINOModel:     "qwen3-8b",
		OllamaBaseURL:     "http://gpu-host.invalid:11434",
		OllamaAgentModel:  "qwen3.8:27b-q4_K_M",
		VLLMBaseURL:       "http://gpu-host.invalid:11435",
		VLLMModel:         "review",
		VLLMContextTokens: 65536,
	}
	backends, err := buildFleetBackends(cfg, newCaptureLogger())
	if err != nil {
		t.Fatalf("buildFleetBackends: %v", err)
	}
	for _, name := range []string{"openvino", "ollama-agent", "vllm"} {
		if _, ok := backends[name]; !ok {
			t.Fatalf("%q was not registered; a project cannot switch onto a backend the daemon does not serve", name)
		}
	}
	// The declared context must reach the backend, or the first oversized
	// prompt is a wasted round trip instead of an immediate refusal.
	if ov, ok := backends["vllm"].(*orchestration.OpenVINOBackend); !ok || ov.ContextBound() != 65536 {
		t.Fatalf("vllm context bound not carried through")
	}
}

// TestBuildAgentBackends_AnUnnamedEndpointIsNotRegistered: a backend built
// against a guessed address fails on first use, where an operator is not
// looking, instead of at startup where they are.
func TestBuildAgentBackends_AnUnnamedEndpointIsNotRegistered(t *testing.T) {
	t.Parallel()

	cfg := &fleet.Config{
		ServerAddr: "https://control.invalid:8443", TenantID: "t", BindAddr: "127.0.0.1:0",
		Backends: []fleet.BackendKind{fleet.BackendACP},
	}
	backends, err := buildFleetBackends(cfg, newCaptureLogger())
	if err != nil {
		t.Fatalf("buildFleetBackends: %v", err)
	}
	for _, name := range []string{"openvino", "vllm"} {
		if _, ok := backends[name]; ok {
			t.Fatalf("%q was registered without an endpoint", name)
		}
	}
}

// TestBuildAgentBackends_CodexReasoningEffortIsStated: review is a GATE, and a
// shallower pass does not fail visibly — it approves what it should have
// caught. The depth must therefore be a deployment decision that reaches the
// CLI, not whatever the installed build defaults to.
func TestBuildAgentBackends_CodexReasoningEffortIsStated(t *testing.T) {
	t.Parallel()

	for _, c := range []struct{ configured, want string }{
		{"", fleet.DefaultCodexReasoningEffort},
		{"high", "high"},
	} {
		cfg := &fleet.Config{
			ServerAddr: "https://control.invalid:8443", TenantID: "t", BindAddr: "127.0.0.1:0",
			Backends:             []fleet.BackendKind{fleet.BackendACP},
			CodexReasoningEffort: c.configured,
		}
		backends, err := buildFleetBackends(cfg, newCaptureLogger())
		if err != nil {
			t.Fatalf("buildFleetBackends: %v", err)
		}
		if _, ok := backends["codex"]; !ok {
			t.Fatal("codex was not registered")
		}
		// The effort must reach the CLI's argv. Config is the seam we can
		// assert without reaching into the backend's private state.
		if got := cfg.EffectiveCodexReasoningEffort(); got != c.want {
			t.Fatalf("configured %q: effective effort = %q, want %q", c.configured, got, c.want)
		}
	}
}

// TestBuildAgentBackends_GPUHostServesOnlyWhatItNames is the placement
// guarantee for a GPU node that must never take hosted-model work: naming
// opencode, openvino and ollama builds exactly those three, and nothing a
// claude or codex task could be routed to.
func TestBuildAgentBackends_GPUHostServesOnlyWhatItNames(t *testing.T) {
	cfg := &fleet.Config{
		Backends:        []fleet.BackendKind{fleet.BackendOpencode, fleet.BackendOpenVINO, fleet.BackendOllama},
		OpenVINOBaseURL: "http://127.0.0.1:9000",
		OpenVINOModel:   "qwen3-8b",
		OllamaBaseURL:   "http://127.0.0.1:11434",
	}
	backends, err := buildFleetBackends(cfg, newFleetServeLogger())
	require.NoError(t, err)
	names := make([]string, 0, len(backends))
	for name := range backends {
		names = append(names, name)
	}
	assert.ElementsMatch(t, []string{"opencode", "openvino", "ollama"}, names)
	_, isOpenVINO := backends["openvino"].(*orchestration.OpenVINOBackend)
	assert.True(t, isOpenVINO, "openvino must be the container-and-tools agent loop")
	_, isOllama := backends["ollama"].(*orchestration.OllamaBackend)
	assert.True(t, isOllama, "ollama must be the model server backend")
}

// TestBuildAgentBackends_OpencodeWithoutOllamaListsOnlyTheCatalog: a node
// that does not serve ollama offers no ollama models through opencode.
func TestBuildAgentBackends_OpencodeWithoutOllamaListsOnlyTheCatalog(t *testing.T) {
	cfg := &fleet.Config{Backends: []fleet.BackendKind{fleet.BackendOpencode}}
	backends, err := buildFleetBackends(cfg, newFleetServeLogger())
	require.NoError(t, err)
	models, err := backends["opencode"].(orchestration.ModelLister).ListModels(context.Background())
	require.NoError(t, err)
	for _, model := range models {
		assert.NotContains(t, model, "ollama/")
	}
}

// TestBuildServeConfig_ValidatesIndividualBackends: --backends names each
// backend on its own, and a name that is not a backend is refused by the
// validation the daemon runs before it starts.
func TestBuildServeConfig_ValidatesIndividualBackends(t *testing.T) {
	saved := serveBackendsCSV
	t.Cleanup(func() { serveBackendsCSV = saved })
	complete := func(cfg *fleet.Config) *fleet.Config {
		cfg.ServerAddr, cfg.TenantID, cfg.BindAddr = "https://cp.example.com:8443", "acme", "127.0.0.1:9091"
		cfg.WorkDir, cfg.NodeCertFile, cfg.NodeKeyFile, cfg.ServerCAFile = t.TempDir(), "a.crt", "a.key", "ca.crt"
		cfg.OpenVINOBaseURL = "http://127.0.0.1:9000"
		return cfg
	}

	serveBackendsCSV = "opencode, openvino ,ollama"
	cfg, err := buildServeConfig()
	require.NoError(t, err)
	assert.Equal(t, []fleet.BackendKind{fleet.BackendOpencode, fleet.BackendOpenVINO, fleet.BackendOllama}, cfg.Backends)
	require.NoError(t, complete(cfg).Validate())

	serveBackendsCSV = "opencode,gemini"
	cfg, err = buildServeConfig()
	require.NoError(t, err)
	require.ErrorIs(t, complete(cfg).Validate(), fleet.ErrConfigUnknownBackend)
}
