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

// recordingSandboxedBackend records what wireSandboxedBackends hands an ACP
// backend.
type recordingSandboxedBackend struct {
	runtime orchestration.ContainerRuntime
	owner   string
	goCache orchestration.GoBuildCache
}

func (b *recordingSandboxedBackend) SetContainerRuntime(r orchestration.ContainerRuntime) {
	b.runtime = r
}
func (b *recordingSandboxedBackend) SetSandboxOwner(owner string) { b.owner = owner }
func (b *recordingSandboxedBackend) SetGoBuildCache(c orchestration.GoBuildCache) {
	b.goCache = c
}

func TestAgentContainerRuntime_OverrideReachesEveryBuiltInBackendWithoutDiscovery(t *testing.T) {
	override := &struct{ orchestration.ContainerRuntime }{}
	srv, err := NewServer(config.Default(), WithAgentContainerRuntime(override))
	require.NoError(t, err)

	runtime, host := srv.agentContainerRuntime(func() (*orchestration.ExecContainerRuntime, error) {
		t.Fatal("host runtime discovery ran although an override was supplied")
		return nil, nil
	})
	require.Same(t, override, runtime)
	assert.Nil(t, host, "an injected runtime is not swept or reaped as the host's")

	backends := map[string]any{
		"claude":   &recordingSandboxedBackend{},
		"codex":    &recordingSandboxedBackend{},
		"opencode": &recordingSandboxedBackend{},
		"custom":   &recordingSandboxedBackend{},
	}
	goCache := orchestration.GoBuildCache{Root: "/cache"}
	wireSandboxedBackends(backends, runtime, "owner-1", goCache)

	for _, name := range []string{"claude", "codex", "opencode"} {
		backend := backends[name].(*recordingSandboxedBackend)
		assert.Same(t, override, backend.runtime, name)
		assert.Equal(t, "owner-1", backend.owner, name)
		assert.Equal(t, goCache, backend.goCache, name)
	}
	assert.Nil(t, backends["custom"].(*recordingSandboxedBackend).runtime,
		"only the built-in ACP backends are sandboxed")
}

func TestAgentContainerRuntime_WithoutOverrideUsesDiscovery(t *testing.T) {
	srv, err := NewServer(config.Default())
	require.NoError(t, err)

	discovered := &orchestration.ExecContainerRuntime{}
	runtime, host := srv.agentContainerRuntime(func() (*orchestration.ExecContainerRuntime, error) {
		return discovered, nil
	})
	assert.Same(t, discovered, runtime)
	assert.Same(t, discovered, host)
}

func TestAgentContainerRuntime_NoHostRuntimeYieldsNone(t *testing.T) {
	srv, err := NewServer(config.Default())
	require.NoError(t, err)

	runtime, host := srv.agentContainerRuntime(func() (*orchestration.ExecContainerRuntime, error) {
		return nil, orchestration.ErrContainerRuntimeNotAvailable
	})
	assert.Nil(t, runtime)
	assert.Nil(t, host)
}

func TestWireSandboxedBackends_SkipsBackendsThatAreNotSandboxed(t *testing.T) {
	backends := map[string]any{"claude": struct{}{}}

	assert.NotPanics(t, func() {
		wireSandboxedBackends(backends, &struct{ orchestration.ContainerRuntime }{}, "owner", orchestration.GoBuildCache{})
	})
}

// Without the options a server adds no backend and replaces no runtime: the
// orchestration service keeps its built-in backends and the host's runtime.
func TestAgentOptions_AbsentLeaveTheBuiltInsAlone(t *testing.T) {
	srv, err := NewServer(config.Default())
	require.NoError(t, err)

	assert.Empty(t, srv.opts.agentBackends)
	assert.Nil(t, srv.opts.agentContainerRuntime)
}
