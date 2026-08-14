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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/agent"
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
