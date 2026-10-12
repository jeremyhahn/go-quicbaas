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

package orchestration

import (
	"encoding/json"
	"reflect"
	"testing"
)

// renderFleetNode marshals one view the way a JSON transport would, so the
// assertions are about the bytes an operator actually receives.
func renderFleetNode(t *testing.T, node *FleetNode) map[string]any {
	t.Helper()
	raw, err := json.Marshal(NewFleetNodeView(node))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return got
}

// TestFleetNodeView_RendersTheServedBackends is the operator-facing half of
// go-quicbaas#681: a daemon serving claude, codex and opencode is one record
// whose "backend" names only the first. Without "backends", reading the
// registry told an operator the daemon could not take a task routed on
// "codex" — the opposite of what dispatch does.
func TestFleetNodeView_RendersTheServedBackends(t *testing.T) {
	t.Parallel()

	got := renderFleetNode(t, &FleetNode{
		ID:           "agent-04",
		TenantID:     "automatethethings",
		Backend:      "claude",
		Capabilities: []string{"claude", "codex", "opencode"},
	})

	want := []any{"claude", "codex", "opencode"}
	if !reflect.DeepEqual(got["backends"], want) {
		t.Fatalf("backends = %v, want %v: the primary once, and every served name", got["backends"], want)
	}
	// Additive: the fields the TypeScript SDK and the Agent Fleet UI read are
	// rendered unchanged beside it.
	if got["backend"] != "claude" || got["id"] != "agent-04" || got["tenant_id"] != "automatethethings" {
		t.Fatalf("record fields changed: %v", got)
	}
	if !reflect.DeepEqual(got["capabilities"], want) {
		t.Fatalf("capabilities = %v, want them unchanged", got["capabilities"])
	}
}

// TestFleetNode_ServedBackendsShape covers the record shapes the fleet
// actually produces, including the ones where the primary is absent from the
// capability list.
func TestFleetNode_ServedBackendsShape(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		node *FleetNode
		want []string
	}{
		"gpu trait rides beside the backend": {
			&FleetNode{Backend: "ollama", Capabilities: []string{"ollama", "gpu"}},
			[]string{"ollama", "gpu"},
		},
		"primary absent from capabilities is still first": {
			&FleetNode{Backend: "claude", Capabilities: []string{"gpu"}},
			[]string{"claude", "gpu"},
		},
		"no capabilities at all": {
			&FleetNode{Backend: "ollama"},
			[]string{"ollama"},
		},
		"a repeated capability is reported once": {
			&FleetNode{Backend: "claude", Capabilities: []string{"codex", "codex", ""}},
			[]string{"claude", "codex"},
		},
		"a record naming nothing serves an empty list": {
			&FleetNode{ID: "unknown"},
			[]string{},
		},
		"a nil node serves an empty list": {
			nil,
			[]string{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := tc.node.ServedBackends()
			if got == nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ServedBackends() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestFleetNodeView_NeverRendersNull pins that an operator never has to guess
// whether a missing list means "serves nothing" or "field absent".
func TestFleetNodeView_NeverRendersNull(t *testing.T) {
	t.Parallel()

	if got := renderFleetNode(t, &FleetNode{ID: "unknown"})["backends"]; !reflect.DeepEqual(got, []any{}) {
		t.Fatalf("backends = %#v, want []", got)
	}
	raw, err := json.Marshal(FleetNodeViews(nil))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(raw) != "[]" {
		t.Fatalf("an empty fleet rendered %s, want []", raw)
	}
}

func TestFleetNodeViews_RendersEveryNode(t *testing.T) {
	t.Parallel()

	views := FleetNodeViews([]*FleetNode{
		{ID: "agent-04", Backend: "claude", Capabilities: []string{"claude", "codex"}},
		{ID: "gpu-host", Backend: "ollama", Capabilities: []string{"ollama", "gpu"}},
	})
	if len(views) != 2 {
		t.Fatalf("len = %d, want 2", len(views))
	}
	if !reflect.DeepEqual(views[0].Backends, []string{"claude", "codex"}) ||
		!reflect.DeepEqual(views[1].Backends, []string{"ollama", "gpu"}) {
		t.Fatalf("views = %+v", views)
	}
}
