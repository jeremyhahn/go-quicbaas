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

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// Every WithAgentBackend is kept under its own name, and a second backend
// under one name replaces the first rather than being dropped.
func TestWithAgentBackend_RecordsEachNameAndTheLastWins(t *testing.T) {
	first := orchestration.NewCodexExecBackend(orchestration.CodexExecBackendConfig{Command: "first"})
	second := orchestration.NewCodexExecBackend(orchestration.CodexExecBackendConfig{Command: "second"})
	other := orchestration.NewCodexExecBackend(orchestration.CodexExecBackendConfig{Command: "other"})

	srv, err := NewServer(config.Default(),
		WithAgentBackend("e2e", first),
		WithAgentBackend("host", other),
		WithAgentBackend("e2e", second))
	require.NoError(t, err)

	require.Len(t, srv.opts.agentBackends, 2)
	assert.Same(t, second, srv.opts.agentBackends["e2e"])
	assert.Same(t, other, srv.opts.agentBackends["host"])
}

// WithAgentContainerRuntime records the runtime the orchestration service is
// wired with in place of the one it would find on the host.
func TestWithAgentContainerRuntime_StoredOnServerOptions(t *testing.T) {
	runtime := &struct{ orchestration.ContainerRuntime }{}

	srv, err := NewServer(config.Default(), WithAgentContainerRuntime(runtime))
	require.NoError(t, err)

	assert.Same(t, runtime, srv.opts.agentContainerRuntime)
}

// Without the options a server adds no backend and replaces no runtime: the
// orchestration service keeps its built-in backends and the host's runtime.
func TestAgentOptions_AbsentLeaveTheBuiltInsAlone(t *testing.T) {
	srv, err := NewServer(config.Default())
	require.NoError(t, err)

	assert.Empty(t, srv.opts.agentBackends)
	assert.Nil(t, srv.opts.agentContainerRuntime)
}
