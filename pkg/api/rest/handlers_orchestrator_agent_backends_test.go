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

//go:build rest

package rest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// renderAgent marshals one rendered registration the way RespondJSON would,
// so the assertions are about the bytes an operator actually receives.
func renderAgent(t *testing.T, agent *orchestration.AgentRegistration) map[string]any {
	t.Helper()

	raw, err := json.Marshal(newAgentResponse(agent))
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	return got
}

// TestAgentResponse_RendersTheServedBackends is the operator-facing half of
// go-quicbaas#681: a daemon serving claude, codex and opencode is one record
// whose "backend" names only the first. Without "backends", reading the
// registry told an operator the daemon could not take a task routed on
// "codex" — the opposite of what dispatch does.
func TestAgentResponse_RendersTheServedBackends(t *testing.T) {
	t.Parallel()

	got := renderAgent(t, &orchestration.AgentRegistration{
		ID:           "vm-host-4",
		TenantID:     "automatethethings",
		Backend:      "claude",
		Capabilities: []string{"claude", "codex", "opencode"},
	})

	require.Equal(t, []any{"claude", "codex", "opencode"}, got["backends"],
		"the primary appears once, and every served name appears")

	// Additive: the fields the TypeScript SDK and the Agent Fleet UI read are
	// rendered unchanged beside it.
	require.Equal(t, "claude", got["backend"])
	require.Equal(t, []any{"claude", "codex", "opencode"}, got["capabilities"])
	require.Equal(t, "vm-host-4", got["id"])
	require.Equal(t, "automatethethings", got["tenant_id"])
}

// TestAgentResponse_BackendsShape covers the record shapes the fleet actually
// produces, including the ones where the primary is absent from the
// capability list.
func TestAgentResponse_BackendsShape(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		agent *orchestration.AgentRegistration
		want  []any
	}{
		"gpu trait rides beside the backend": {
			&orchestration.AgentRegistration{
				Backend:      "ollama",
				Capabilities: []string{"ollama", "gpu"},
			},
			[]any{"ollama", "gpu"},
		},
		"primary absent from capabilities is still first": {
			&orchestration.AgentRegistration{
				Backend:      "claude",
				Capabilities: []string{"gpu"},
			},
			[]any{"claude", "gpu"},
		},
		"no capabilities at all": {
			&orchestration.AgentRegistration{Backend: "ollama"},
			[]any{"ollama"},
		},
		"a record naming nothing renders an empty list, never null": {
			&orchestration.AgentRegistration{ID: "unknown"},
			[]any{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, renderAgent(t, tc.agent)["backends"])
		})
	}
}

// TestAgentsResponse_RendersEveryAgent pins the listing wrapper, including the
// empty fleet an operator sees before the first daemon enrolls.
func TestAgentsResponse_RendersEveryAgent(t *testing.T) {
	t.Parallel()

	t.Run("a populated fleet", func(t *testing.T) {
		t.Parallel()
		rendered := agentsResponse([]*orchestration.AgentRegistration{
			{ID: "vm-host-4", Backend: "claude", Capabilities: []string{"claude", "codex"}},
			{ID: "gpu-host", Backend: "ollama", Capabilities: []string{"ollama", "gpu"}},
		})
		require.Len(t, rendered, 2)
		require.Equal(t, []string{"claude", "codex"}, rendered[0].Backends)
		require.Equal(t, []string{"ollama", "gpu"}, rendered[1].Backends)
	})

	t.Run("an empty fleet marshals to an array", func(t *testing.T) {
		t.Parallel()
		raw, err := json.Marshal(agentsResponse(nil))
		require.NoError(t, err)
		require.Equal(t, "[]", string(raw))
	})
}
