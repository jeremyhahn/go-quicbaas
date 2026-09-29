//go:build !integration

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

// lifecycle_remote_resolver_served_backends_test.go covers the seam an sdlc
// job's worker session travels: JobRun.Spawn -> SpawnSession ->
// resolveBackendForSpawn -> this resolver, keyed on the BACKEND NAME the task
// was routed on and never on an agent id. When the resolver declines, the
// dispatcher falls through to the central's own locally-registered backend and
// the worker session is spawned in-process with no container, which the sdlc
// runner then refuses (go-quicbaas#681).
//
// These tests deliberately use the REAL registry rather than a mock, because
// the defect lived in the registry's own backend filter: the resolver
// pre-filters candidates with ListFleetNodes(&FleetNodeFilter{Backend: name}), so a
// mock that reimplements the filter would test the mock.

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

const servedBackendsTenant = "automatethethings"

// liveFleetPool builds a resolver over the real registry holding the two
// daemons the burn-in had online: agent-04 started with --backends acp
// (serving claude, codex and opencode) and gpu-host serving ollama on a GPU.
func liveFleetPool(t *testing.T) *remoteBackendPool {
	t.Helper()

	registry := orchestration.NewInMemoryFleetRegistry()
	ctx := context.Background()

	require.NoError(t, registry.RegisterFleetNode(ctx, &orchestration.FleetNode{
		ID:           "agent-04-id",
		TenantID:     servedBackendsTenant,
		Name:         "agent-04",
		Host:         "198.51.100.91:9091",
		Backend:      "claude",
		Kind:         orchestration.FleetNodeKindDaemon,
		Status:       orchestration.FleetNodeStatusRunning,
		Capabilities: []string{"claude", "codex", "opencode"},
	}))
	require.NoError(t, registry.RegisterFleetNode(ctx, &orchestration.FleetNode{
		ID:           "gpu-host-id",
		TenantID:     servedBackendsTenant,
		Name:         "gpu-host",
		Host:         "198.51.100.92:9091",
		Backend:      "ollama",
		Kind:         orchestration.FleetNodeKindDaemon,
		Status:       orchestration.FleetNodeStatusRunning,
		Capabilities: []string{"ollama", "gpu"},
	}))

	return &remoteBackendPool{
		registry:        registry,
		hostAffinityCfg: config.SchedulerHostAffinityConfig{Mode: config.HostAffinityModeSoft},
		logger:          strictNoopLogger{},
		entries:         map[string]*pooledDaemon{},
	}
}

// TestResolve_RoutesEveryServedBackendToItsDaemon is the live case. A worker
// session routed on "claude" — and equally on "codex" or "opencode" — must
// reach agent-04, because that is the daemon that can run it.
func TestResolve_RoutesEveryServedBackendToItsDaemon(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		backend  string
		wantHost string
	}{
		"claude reaches the acp daemon":   {"claude", "198.51.100.91:9091"},
		"codex reaches the acp daemon":    {"codex", "198.51.100.91:9091"},
		"opencode reaches the acp daemon": {"opencode", "198.51.100.91:9091"},
		"ollama reaches the gpu host":     {"ollama", "198.51.100.92:9091"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			pool := liveFleetPool(t)
			ctx := orchestration.ContextWithTenant(context.Background(), servedBackendsTenant)

			backend, ok := pool.resolve(ctx, tc.backend, "")
			require.True(t, ok, "a daemon serving %q must be routed to", tc.backend)
			require.NotNil(t, backend)

			remote, isRemote := backend.(*orchestration.RemoteACPBackend)
			require.True(t, isRemote, "the session must run on the daemon, not in-process")
			require.Equal(t, tc.wantHost, remote.Registration().Host)
		})
	}
}

// TestResolve_DeclinesWhenNoDaemonServesTheBackend pins the fall-through the
// dispatcher relies on, so it stays deliberate rather than accidental: the
// resolver returns (nil, false) and SpawnSession then resolves against the
// central's local backend table. That is the correct answer for a backend no
// daemon serves — and it is exactly what an operator sees as an in-process
// session with no container, so the two cases must stay distinguishable.
func TestResolve_DeclinesWhenNoDaemonServesTheBackend(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"the transport kind is not a backend": "acp",
		"a backend no daemon serves":          "gemini",
		"an empty backend name":               "",
	}

	for name, backendName := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			pool := liveFleetPool(t)
			ctx := orchestration.ContextWithTenant(context.Background(), servedBackendsTenant)

			backend, ok := pool.resolve(ctx, backendName, "")
			require.False(t, ok, "no daemon serves %q", backendName)
			require.Nil(t, backend)
		})
	}
}

// TestResolve_GpuAndHostConstraintsStillNarrow pins that matching on the
// served list did not weaken the other two filters: a GPU-routed session
// cannot land on the daemon with no GPU even though it serves the backend,
// and host affinity still picks a host.
func TestResolve_GpuAndHostConstraintsStillNarrow(t *testing.T) {
	t.Parallel()

	t.Run("gpu routing declines the acp daemon", func(t *testing.T) {
		t.Parallel()
		pool := liveFleetPool(t)
		ctx := orchestration.WithGpuRequired(
			orchestration.ContextWithTenant(context.Background(), servedBackendsTenant))

		backend, ok := pool.resolve(ctx, "codex", "")
		require.False(t, ok, "agent-04 serves codex but has no gpu")
		require.Nil(t, backend)
	})

	t.Run("host affinity picks the named daemon", func(t *testing.T) {
		t.Parallel()
		pool := liveFleetPool(t)
		ctx := orchestration.WithHostAffinity(
			orchestration.ContextWithTenant(context.Background(), servedBackendsTenant), "agent-04")

		backend, ok := pool.resolve(ctx, "opencode", "")
		require.True(t, ok)
		require.NotNil(t, backend)
	})

	t.Run("host affinity declines a daemon that serves the backend elsewhere", func(t *testing.T) {
		t.Parallel()
		pool := liveFleetPool(t)
		ctx := orchestration.WithHostAffinity(
			orchestration.ContextWithTenant(context.Background(), servedBackendsTenant), "gpu-host")

		backend, ok := pool.resolve(ctx, "opencode", "")
		require.False(t, ok, "gpu-host does not serve opencode")
		require.Nil(t, backend)
	})
}
