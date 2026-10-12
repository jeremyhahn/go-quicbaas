//go:build mcp

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

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestSessionTools_RefuseACrossTenantRequest is the go-quicbaas#884 regression:
// every agent-session tool, spawn included, refuses a caller that names a
// tenant other than its own, before the orchestrator is reached.
func TestSessionTools_RefuseACrossTenantRequest(t *testing.T) {
	t.Parallel()
	tools := []string{
		"quicbaas_orchestrator_spawn",
		"quicbaas_orchestrator_prompt",
		"quicbaas_orchestrator_status",
		"quicbaas_orchestrator_close",
		"quicbaas_orchestrator_list_sessions",
		"quicbaas_orchestrator_session_history",
		"quicbaas_orchestrator_session_conversation",
		"quicbaas_orchestrator_cost",
	}
	for _, tool := range tools {
		t.Run(tool, func(t *testing.T) {
			spawned := false
			svc := &mockAgentService{spawnFn: func(context.Context, *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
				spawned = true
				return &orchestration.SpawnSessionResponse{}, nil
			}}

			_, err := newFleetRegistry(t, svc).executeAgentTool(tenantctx.WithTenant(context.Background(), "acme"), tool,
				map[string]interface{}{"tenant_id": "globex"})

			var cross *tenantctx.CrossTenantError
			require.ErrorAs(t, err, &cross)
			assert.Equal(t, "globex", cross.TargetTenant)
			assert.False(t, spawned)
		})
	}
}

// TestSessionTenantContext_StillRequiresTheArgument: the session tools keep
// their explicit tenant_id contract; a caller that omits it is told so rather
// than silently defaulted.
func TestSessionTenantContext_StillRequiresTheArgument(t *testing.T) {
	t.Parallel()

	_, _, err := sessionTenantContext(tenantctx.WithTenant(context.Background(), "acme"), map[string]interface{}{})

	require.Error(t, err)
	var cross *tenantctx.CrossTenantError
	assert.NotErrorAs(t, err, &cross)
}
