//go:build !integration && raft && local

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

// server_coverage_boost5_test.go targets the three remaining
// buildEmbeddedFleetNodeBackends factory branches (claude / codex / opencode)
// and a handful of small uncovered helpers. Existing tests only exercise
// the ollama factory; adding the others walks the lambdas at
// embedded_fleet_node.go:375, 384, 393.

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// buildEmbeddedFleetNodeBackends — per-backend factory coverage
//
// Each factory closure in the map is dispatched by kind. Calling
// buildEmbeddedFleetNodeBackends with the kind name triggers the matching
// closure and the associated orchestration backend constructor.
// =============================================================================

func TestBoost5_BuildEmbeddedAgentBackends_ClaudeFactory(t *testing.T) {
	t.Parallel()

	log := makeNoopLogger()
	backends, err := buildEmbeddedFleetNodeBackends([]string{"claude"}, "", orchestration.SandboxNetworkDefault, orchestration.SandboxSecurity{}, nil, log)
	require.NoError(t, err)
	_, ok := backends["claude"]
	assert.True(t, ok, "claude factory must register a 'claude' backend")
}

func TestBoost5_BuildEmbeddedAgentBackends_CodexFactory(t *testing.T) {
	t.Parallel()

	log := makeNoopLogger()
	backends, err := buildEmbeddedFleetNodeBackends([]string{"codex"}, "", orchestration.SandboxNetworkDefault, orchestration.SandboxSecurity{}, nil, log)
	require.NoError(t, err)
	_, ok := backends["codex"]
	assert.True(t, ok, "codex factory must register a 'codex' backend")
}

func TestBoost5_BuildEmbeddedAgentBackends_OpenCodeFactory(t *testing.T) {
	t.Parallel()

	log := makeNoopLogger()
	backends, err := buildEmbeddedFleetNodeBackends([]string{"opencode"}, "", orchestration.SandboxNetworkDefault, orchestration.SandboxSecurity{}, nil, log)
	require.NoError(t, err)
	_, ok := backends["opencode"]
	assert.True(t, ok, "opencode factory must register an 'opencode' backend")
}

func TestBoost5_BuildEmbeddedAgentBackends_AllSupportedKinds(t *testing.T) {
	t.Parallel()

	log := makeNoopLogger()
	backends, err := buildEmbeddedFleetNodeBackends(
		[]string{"ollama", "claude", "codex", "opencode"},
		"http://127.0.0.1:11434",
		orchestration.SandboxNetworkDefault,
		orchestration.SandboxSecurity{},
		nil,
		log,
	)
	require.NoError(t, err)
	for _, kind := range []string{"ollama", "claude", "codex", "opencode"} {
		_, ok := backends[kind]
		assert.True(t, ok, "expected backend %q to be registered", kind)
	}
}

// TestBoost5_BuildEmbeddedAgentBackends_OllamaDefaultURL covers the inner
// "url == empty → default to 127.0.0.1:11434" branch in the ollama
// factory closure, which the existing ollama test does not specifically
// assert (it passes a non-empty URL).
func TestBoost5_BuildEmbeddedAgentBackends_OllamaDefaultURL(t *testing.T) {
	t.Parallel()

	log := makeNoopLogger()
	backends, err := buildEmbeddedFleetNodeBackends([]string{"ollama"}, "", orchestration.SandboxNetworkDefault, orchestration.SandboxSecurity{}, nil, log)
	require.NoError(t, err)
	_, ok := backends["ollama"]
	assert.True(t, ok, "ollama factory must default URL when blank")
}
