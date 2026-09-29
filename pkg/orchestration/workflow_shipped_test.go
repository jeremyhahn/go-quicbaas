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
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shippedWorkflowDir is the repository's workflows/ directory, the one
// `quicbaas orchestrator` loads from the project root.
var shippedWorkflowDir = filepath.Join("..", "..", "workflows")

// TestShippedWorkflows_ModelsAreInTheBackendCatalog pins every model a
// shipped workflow names to the catalog its backend advertises. A role that
// names no model runs the backend's default, which is the coding tier; a role
// that names one must name a model the backend still offers, so a retired ID
// (claude-sonnet-4-5-20250514, go-quicbaas#1015) cannot ship again.
func TestShippedWorkflows_ModelsAreInTheBackendCatalog(t *testing.T) {
	loader := NewWorkflowLoader()
	require.NoError(t, loader.LoadFromDir(SystemTenantID, shippedWorkflowDir))

	defs := loader.ListDefinitions(SystemTenantID)
	require.NotEmpty(t, defs, "no workflows loaded from %s", shippedWorkflowDir)

	for _, wf := range defs {
		for i := range wf.Agents {
			spec := wf.Agents[i]
			t.Run(wf.Name+"/"+spec.Role, func(t *testing.T) {
				catalog, err := NewACPBackend(ACPBackendConfig{Command: spec.Backend}).
					ListModels(context.Background())
				require.NoError(t, err)
				require.NotEmpty(t, catalog,
					"backend %q advertises no model catalog, so the role's model cannot be checked", spec.Backend)
				if spec.Model == "" {
					return
				}
				assert.Contains(t, catalog, spec.Model,
					"workflow %q role %q names model %q, which backend %q does not offer; "+
						"omit model: to run the backend default", wf.Name, spec.Role, spec.Model, spec.Backend)
			})
		}
	}
}
