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
	"context"
	"slices"
	"testing"
)

// TestACPModelCatalog_OffersTheCodingTierDefault pins the catalog to
// DefaultClaudeModel for every command that runs Claude models. A backend
// whose default the catalog does not list advertises one model and runs
// another, and a caller picking from the catalog cannot select the model
// every unnamed spawn already uses (go-quicbaas#1012).
func TestACPModelCatalog_OffersTheCodingTierDefault(t *testing.T) {
	cases := map[string]string{
		"claude":      DefaultClaudeModel,
		"claude-code": DefaultClaudeModel,
		"opencode":    "anthropic/" + DefaultClaudeModel,
	}
	for command, want := range cases {
		b := NewACPBackend(ACPBackendConfig{Command: "/usr/local/bin/" + command})
		models, err := b.ListModels(context.Background())
		if err != nil {
			t.Fatalf("%s: ListModels: %v", command, err)
		}
		if !slices.Contains(models, want) {
			t.Errorf("%s: catalog %v does not offer the coding-tier default %q", command, models, want)
		}
	}
}

// TestACPModelCatalog_DropsTheSupersededOpus asserts the Opus version the
// default replaced is no longer selectable. The model-usage policy prohibits
// a superseded version outright, and a catalog entry that still resolves is
// exactly how work quietly runs on one.
func TestACPModelCatalog_DropsTheSupersededOpus(t *testing.T) {
	superseded := []string{"claude-opus-5", "anthropic/claude-opus-5"}
	for command, models := range acpModelCatalog {
		for _, old := range superseded {
			if slices.Contains(models, old) {
				t.Errorf("%s: catalog still offers superseded model %q: %v", command, old, models)
			}
		}
	}
}

// TestACPBackend_DefaultModelReportsTheConfiguredModel covers the getter a
// deployment's wiring is checked through.
func TestACPBackend_DefaultModelReportsTheConfiguredModel(t *testing.T) {
	b := NewACPBackend(ACPBackendConfig{Command: "claude", DefaultModel: DefaultClaudeModel})
	if got := b.DefaultModel(); got != DefaultClaudeModel {
		t.Errorf("DefaultModel() = %q, want %q", got, DefaultClaudeModel)
	}
}

// TestACPBackend_DefaultModelEmptyWhenUnset asserts an unconfigured backend
// reports no default rather than inventing one: empty is what hands the
// choice to the agent CLI, and the getter must not hide that.
func TestACPBackend_DefaultModelEmptyWhenUnset(t *testing.T) {
	b := NewACPBackend(ACPBackendConfig{Command: "codex"})
	if got := b.DefaultModel(); got != "" {
		t.Errorf("DefaultModel() = %q, want empty for an unconfigured backend", got)
	}
}
