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

package fleet

import (
	"strconv"
	"strings"
)

// =============================================================================
// Which backends a daemon serves
// =============================================================================
//
// EVERY BACKEND IS SELECTED BY ITS OWN NAME. A node's backend list is what it
// advertises to the fleet, and placement routes work by that list: a node that
// serves claude receives claude work. So a node that must NOT take hosted-model
// work — a GPU host that runs the local review model and the NPU triage model —
// names exactly what it runs (opencode, openvino, ollama) and nothing else.
//
// "acp" IS A GROUP, NOT AN ALIAS. It names every agent backend this daemon can
// construct — the three CLIs and each OpenAI-compatible agent endpoint the
// configuration points at — which is what every node deployed before
// individual selection existed runs. It expands here, once, so nothing
// downstream ever sees it.

// BackendKind names a backend a daemon can serve, or a group of them.
type BackendKind string

// The individual backends.
const (
	// BackendClaude is the Claude Code CLI.
	BackendClaude BackendKind = "claude"

	// BackendCodex is the codex CLI.
	BackendCodex BackendKind = "codex"

	// BackendOpencode is the opencode CLI, driven over ACP.
	BackendOpencode BackendKind = "opencode"

	// BackendOpenVINO is the container-and-tools agent loop against an
	// OpenVINO Model Server (openvino_base_url).
	BackendOpenVINO BackendKind = "openvino"

	// BackendOllamaAgent is the container-and-tools agent loop against the
	// daemon's ollama server.
	BackendOllamaAgent BackendKind = "ollama-agent"

	// BackendVLLM is the container-and-tools agent loop against a vLLM server
	// (vllm_base_url).
	BackendVLLM BackendKind = "vllm"

	// BackendOllama is the plain ollama model server.
	BackendOllama BackendKind = "ollama"
)

// BackendACP is the group of every agent backend: claude, codex, opencode,
// ollama-agent, and openvino and vllm when their servers are configured.
const BackendACP BackendKind = "acp"

// backendEndpoint returns the server an endpoint backend is registered
// against, for the backends that need one named. A backend absent from this
// map needs no server of its own.
var backendEndpoint = map[BackendKind]func(*Config) string{
	BackendOpenVINO: func(c *Config) string { return c.OpenVINOBaseURL },
	BackendVLLM:     func(c *Config) string { return c.VLLMBaseURL },
}

// individualBackends is every backend a daemon can serve, for constant-time
// validation.
var individualBackends = map[BackendKind]bool{
	BackendClaude:      true,
	BackendCodex:       true,
	BackendOpencode:    true,
	BackendOpenVINO:    true,
	BackendOllamaAgent: true,
	BackendVLLM:        true,
	BackendOllama:      true,
}

// acpGroup is what BackendACP expands to, in registration order.
var acpGroup = []BackendKind{
	BackendClaude, BackendCodex, BackendOpencode, BackendOpenVINO, BackendOllamaAgent, BackendVLLM,
}

// defaultBackends is the selection an empty list means.
var defaultBackends = []BackendKind{BackendACP, BackendOllama}

// ParseBackendKinds splits a comma-separated backend list into kinds. It does
// not validate them; Config.Validate does, with the configuration in hand.
func ParseBackendKinds(csv string) []BackendKind {
	kinds := make([]BackendKind, 0, strings.Count(csv, ",")+1)
	for _, raw := range strings.Split(csv, ",") {
		if name := strings.TrimSpace(raw); name != "" {
			kinds = append(kinds, BackendKind(name))
		}
	}
	return kinds
}

// validateBackends refuses a name that is neither a backend nor the acp
// group, and an endpoint backend named explicitly with no server to reach.
func (c *Config) validateBackends() error {
	for _, kind := range c.Backends {
		if kind == BackendACP {
			continue
		}
		if !individualBackends[kind] {
			return &SentinelDetailError{
				Sentinel: ErrConfigUnknownBackend,
				Detail:   strconv.Quote(string(kind)) + "; known: " + knownBackendNames(),
			}
		}
		if endpoint, ok := backendEndpoint[kind]; ok && strings.TrimSpace(endpoint(c)) == "" {
			return &SentinelDetailError{
				Sentinel: ErrConfigBackendEndpointRequired,
				Detail:   string(kind) + " names no server; set its base URL or leave it out",
			}
		}
	}
	return nil
}

// knownBackendNames lists the accepted names for a refusal.
func knownBackendNames() string {
	names := make([]string, 0, len(acpGroup)+2)
	for _, kind := range acpGroup {
		names = append(names, string(kind))
	}
	names = append(names, string(BackendOllama), string(BackendACP))
	return strings.Join(names, ", ")
}

// EffectiveBackends returns the individual backends this daemon serves, in
// registration order and without repeats: the configured list, or
// defaultBackends when it is empty, with the acp group expanded.
//
// THE GROUP INCLUDES AN ENDPOINT BACKEND ONLY WHEN ITS SERVER IS CONFIGURED.
// Naming one explicitly without a server is refused by Validate instead; a
// group that registered a backend against no server would advertise work it
// fails on first use.
func (c *Config) EffectiveBackends() []BackendKind {
	selected := c.Backends
	if len(selected) == 0 {
		selected = defaultBackends
	}
	seen := make(map[BackendKind]bool, len(individualBackends))
	kinds := make([]BackendKind, 0, len(individualBackends))
	add := func(kind BackendKind) {
		if !seen[kind] {
			seen[kind] = true
			kinds = append(kinds, kind)
		}
	}
	for _, kind := range selected {
		if kind != BackendACP {
			add(kind)
			continue
		}
		for _, member := range acpGroup {
			if endpoint, ok := backendEndpoint[member]; ok && strings.TrimSpace(endpoint(c)) == "" {
				continue
			}
			add(member)
		}
	}
	return kinds
}

// Serves reports whether this daemon serves kind.
func (c *Config) Serves(kind BackendKind) bool {
	for _, served := range c.EffectiveBackends() {
		if served == kind {
			return true
		}
	}
	return false
}
