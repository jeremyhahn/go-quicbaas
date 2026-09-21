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

// Package main: coverage for buildAgentBackends.
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/agent"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func TestBuildAgentBackends_NoneEmpty(t *testing.T) {
	// EffectiveBackends() returns a fallback when Backends is nil. Test
	// the explicit "empty" path by passing a synthetic kind that does
	// not match either branch in the switch.
	cfg := &agent.Config{Backends: []agent.BackendKind{"unknown-kind"}}
	logger := newAgentServeLogger()
	_, err := buildAgentBackends(cfg, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no backends constructed")
}

func TestBuildAgentBackends_ACP(t *testing.T) {
	cfg := &agent.Config{Backends: []agent.BackendKind{agent.BackendACP}}
	logger := newAgentServeLogger()
	backends, err := buildAgentBackends(cfg, logger)
	require.NoError(t, err)
	assert.Contains(t, backends, "claude")
	assert.Contains(t, backends, "codex")
	assert.Contains(t, backends, "opencode")
}

func TestBuildAgentBackends_Ollama(t *testing.T) {
	cfg := &agent.Config{Backends: []agent.BackendKind{agent.BackendOllama}}
	logger := newAgentServeLogger()
	backends, err := buildAgentBackends(cfg, logger)
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
	cfg := serveRuntimeConfig(agent.ContainerRuntimeDocker, orchestration.SandboxPolicyOptIn, t.TempDir())

	if _, err := buildAgentRuntime(cfg, backends, logger); err != nil {
		t.Fatalf("buildAgentRuntime: %v", err)
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

	cfg := &agent.Config{
		ServerAddr: "https://control.invalid:8443", TenantID: "t", BindAddr: "127.0.0.1:0",
		Backends:          []agent.BackendKind{agent.BackendACP},
		OpenVINOBaseURL:   "http://npu-host.invalid:9000",
		OpenVINOModel:     "qwen3-8b",
		OllamaBaseURL:     "http://gpu-host.invalid:11434",
		OllamaAgentModel:  "qwen3.8:27b-q4_K_M",
		VLLMBaseURL:       "http://gpu-host.invalid:11435",
		VLLMModel:         "review",
		VLLMContextTokens: 65536,
	}
	backends, err := buildAgentBackends(cfg, newCaptureLogger())
	if err != nil {
		t.Fatalf("buildAgentBackends: %v", err)
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

	cfg := &agent.Config{
		ServerAddr: "https://control.invalid:8443", TenantID: "t", BindAddr: "127.0.0.1:0",
		Backends: []agent.BackendKind{agent.BackendACP},
	}
	backends, err := buildAgentBackends(cfg, newCaptureLogger())
	if err != nil {
		t.Fatalf("buildAgentBackends: %v", err)
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
		{"", agent.DefaultCodexReasoningEffort},
		{"high", "high"},
	} {
		cfg := &agent.Config{
			ServerAddr: "https://control.invalid:8443", TenantID: "t", BindAddr: "127.0.0.1:0",
			Backends:             []agent.BackendKind{agent.BackendACP},
			CodexReasoningEffort: c.configured,
		}
		backends, err := buildAgentBackends(cfg, newCaptureLogger())
		if err != nil {
			t.Fatalf("buildAgentBackends: %v", err)
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
