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

package orchestration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// The fleet shapes these tests use are the ones the burn-in produced
// (go-quicbaas#681), so a regression here is the live failure and not an
// abstraction of it.
//
//   - acpDaemonShape: a daemon started with --backends acp. It serves claude,
//     codex and opencode; the registrar records the first as the primary.
//   - gpuDaemonShape: a daemon started with --backends ollama on a GPU host. It
//     serves ollama and advertises the gpu trait beside it.
func acpDaemonShape(tenantID string) *FleetNode {
	return &FleetNode{
		ID:           "agent-04",
		TenantID:     tenantID,
		Name:         "agent-04",
		Host:         "198.51.100.91:9091",
		Backend:      "claude",
		NodeType:     "agent-runtime",
		Kind:         FleetNodeKindDaemon,
		Status:       FleetNodeStatusRunning,
		Capabilities: []string{"claude", "codex", "opencode"},
	}
}

func gpuDaemonShape(tenantID string) *FleetNode {
	return &FleetNode{
		ID:           "gpu-host",
		TenantID:     tenantID,
		Name:         "gpu-host",
		Host:         "198.51.100.92:9091",
		Backend:      "ollama",
		NodeType:     "agent-runtime",
		Kind:         FleetNodeKindDaemon,
		Status:       FleetNodeStatusRunning,
		Capabilities: []string{"ollama", "gpu"},
	}
}

// TestAgentServesBackend covers the one predicate every backend filter and
// both fleet matchers now share.
func TestAgentServesBackend(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		agent   *FleetNode
		backend string
		want    bool
	}{
		"primary backend":                 {acpDaemonShape("t"), "claude", true},
		"capability backend":              {acpDaemonShape("t"), "codex", true},
		"last capability backend":         {acpDaemonShape("t"), "opencode", true},
		"transport name is not a backend": {acpDaemonShape("t"), "acp", false},
		"backend of another daemon":       {acpDaemonShape("t"), "ollama", false},
		"gpu trait beside a backend":      {gpuDaemonShape("t"), "gpu", true},
		"unserved backend":                {gpuDaemonShape("t"), "claude", false},
		"empty name matches nothing":      {acpDaemonShape("t"), "", false},
		"no capabilities at all": {
			&FleetNode{Backend: "ollama"}, "ollama", true,
		},
		"no capabilities, wrong primary": {
			&FleetNode{Backend: "ollama"}, "codex", false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, FleetNodeServesBackend(tc.agent, tc.backend))
		})
	}
}

// TestListAgents_BackendFilterMatchesServedList is the registry-level half of
// the live failure: the remote backend resolver's candidate list comes from
// ListFleetNodes(&FleetNodeFilter{Backend: name}), so a filter that answered only for
// the primary backend hid a multi-backend daemon from every other name it
// serves, and the spawn fell through to a local in-process backend.
func TestListAgents_BackendFilterMatchesServedList(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryFleetRegistry()
	ctx := context.Background()
	const tenantID = "automatethethings"

	require.NoError(t, registry.RegisterFleetNode(ctx, acpDaemonShape(tenantID)))
	require.NoError(t, registry.RegisterFleetNode(ctx, gpuDaemonShape(tenantID)))

	tests := map[string]struct {
		backend string
		wantIDs []string
	}{
		"primary backend finds the daemon":    {"claude", []string{"agent-04"}},
		"served sub-backend finds the daemon": {"codex", []string{"agent-04"}},
		"other sub-backend finds the daemon":  {"opencode", []string{"agent-04"}},
		"ollama finds only the gpu host":      {"ollama", []string{"gpu-host"}},
		"gpu trait is filterable":             {"gpu", []string{"gpu-host"}},
		"the transport name serves nothing":   {"acp", nil},
		"a backend nobody serves":             {"gemini", nil},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := registry.ListFleetNodes(ctx, tenantID, &FleetNodeFilter{Backend: tc.backend})
			require.NoError(t, err)

			ids := make([]string, 0, len(got))
			for _, a := range got {
				ids = append(ids, a.ID)
			}
			require.ElementsMatch(t, tc.wantIDs, ids)
		})
	}
}

// TestListAgents_BackendFilterCombinesWithOtherCriteria pins that widening the
// backend clause did not widen the filter as a whole: a served backend on the
// wrong host, or in the wrong status, is still excluded.
func TestListAgents_BackendFilterCombinesWithOtherCriteria(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryFleetRegistry()
	ctx := context.Background()
	const tenantID = "automatethethings"

	offline := acpDaemonShape(tenantID)
	offline.ID = "agent-05"
	offline.Host = "198.51.100.93:9091"
	offline.Status = FleetNodeStatusOffline

	require.NoError(t, registry.RegisterFleetNode(ctx, acpDaemonShape(tenantID)))
	require.NoError(t, registry.RegisterFleetNode(ctx, offline))

	t.Run("host narrows a served backend", func(t *testing.T) {
		t.Parallel()
		got, err := registry.ListFleetNodes(ctx, tenantID, &FleetNodeFilter{
			Backend: "codex",
			Host:    "198.51.100.93:9091",
		})
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "agent-05", got[0].ID)
	})

	t.Run("status narrows a served backend", func(t *testing.T) {
		t.Parallel()
		got, err := registry.ListFleetNodes(ctx, tenantID, &FleetNodeFilter{
			Backend: "codex",
			Status:  FleetNodeStatusRunning,
		})
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "agent-04", got[0].ID)
	})

	t.Run("host that serves no such backend stays empty", func(t *testing.T) {
		t.Parallel()
		got, err := registry.ListFleetNodes(ctx, tenantID, &FleetNodeFilter{
			Backend: "ollama",
			Host:    "198.51.100.93:9091",
		})
		require.NoError(t, err)
		require.Empty(t, got)
	})
}

// TestMatchAgentForTask_ServedBackends is the fleet-matcher half: the
// workflow runner's dispatch picks an agent by the task's routed backend, and
// must reach a daemon that serves it under any of its names.
func TestMatchAgentForTask_ServedBackends(t *testing.T) {
	t.Parallel()

	const tenantID = "automatethethings"
	fleet := []*FleetNode{acpDaemonShape(tenantID), gpuDaemonShape(tenantID)}
	loop := NewControlLoop(nil, nil)

	tests := map[string]struct {
		backend     string
		gpuRequired bool
		wantAgentID string
		wantErr     bool
	}{
		"claude reaches the acp daemon":      {"claude", false, "agent-04", false},
		"codex reaches the acp daemon":       {"codex", false, "agent-04", false},
		"opencode reaches the acp daemon":    {"opencode", false, "agent-04", false},
		"ollama reaches the gpu host":        {"ollama", false, "gpu-host", false},
		"gpu routing reaches the gpu host":   {"ollama", true, "gpu-host", false},
		"the transport name reaches nobody":  {"acp", false, "", true},
		"an unserved backend reaches nobody": {"gemini", false, "", true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			item := &WorkItem{ID: "wi-1", Backend: tc.backend}
			if tc.gpuRequired {
				item.RequiredCapabilities = []string{"gpu"}
			}

			agentID, backendName, err := loop.matchFleetNodeForTask(item, fleet, nil)
			if tc.wantErr {
				require.Error(t, err)
				var noMatch *NoMatchingFleetNodeError
				require.ErrorAs(t, err, &noMatch)
				require.Equal(t, "backend", noMatch.Constraint)
				require.Empty(t, agentID)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantAgentID, agentID)
			require.Equal(t, tc.backend, backendName)
		})
	}
}

// TestMatchAgentForTask_GpuRoutingExcludesTheAcpDaemon pins that a GPU-routed
// task cannot land on a daemon with no gpu capability even when it serves the
// requested backend — the capability list is not a free-for-all.
func TestMatchAgentForTask_GpuRoutingExcludesTheAcpDaemon(t *testing.T) {
	t.Parallel()

	const tenantID = "automatethethings"
	fleet := []*FleetNode{acpDaemonShape(tenantID), gpuDaemonShape(tenantID)}
	loop := NewControlLoop(nil, nil)

	agentID, _, err := loop.matchFleetNodeForTask(&WorkItem{
		ID:                   "wi-gpu",
		Backend:              "claude",
		RequiredCapabilities: []string{"gpu"},
	}, fleet, nil)

	require.Error(t, err)
	var noMatch *NoMatchingFleetNodeError
	require.ErrorAs(t, err, &noMatch)
	require.Empty(t, agentID)
}
