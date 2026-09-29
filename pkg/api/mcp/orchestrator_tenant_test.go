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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// TestOrchestratorTenantContext pins the resolution order REST's
// resolveOrchestratorTenant uses: the explicit argument, then the caller's
// binding. The resolved tenant is always stamped on the orchestration key,
// which is the only key the orchestrator's tenant guard reads.
func TestOrchestratorTenantContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ctx  context.Context
		args map[string]interface{}
		want string
	}{
		{"argument from an untenanted caller", context.Background(), map[string]interface{}{"tenant_id": "acme"}, "acme"},
		{"the gate's iam binding", iam.ContextWithTenant(context.Background(), "acme"), map[string]interface{}{}, "acme"},
		{"an orchestration binding", orchestration.ContextWithTenant(context.Background(), "acme"), map[string]interface{}{}, "acme"},
		{"the argument outranks the binding", iam.ContextWithTenant(context.Background(), "system"), map[string]interface{}{"tenant_id": "acme"}, "acme"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, tenantID, err := orchestratorTenantContext(tc.ctx, tc.args)
			require.NoError(t, err)
			assert.Equal(t, tc.want, tenantID)
			assert.Equal(t, tc.want, orchestration.TenantFromContext(ctx))
		})
	}
}

// TestOrchestratorTenantContext_NoTenantAnywhere is the bad-input side: with
// neither an argument nor a binding the tool refuses, naming the field.
func TestOrchestratorTenantContext_NoTenantAnywhere(t *testing.T) {
	t.Parallel()
	_, _, err := orchestratorTenantContext(context.Background(), map[string]interface{}{})
	var required *api.RequiredFieldError
	require.ErrorAs(t, err, &required)
	assert.Equal(t, "tenant_id", required.Field)
}

// TestAttentionTools_TakeTheTenantArgument is the watchdog's X2: an MCP
// principal that is not tenant-bound passes tenant_id, and the inbox must be
// read and resolved in that tenant rather than refused as "tenant-scoped".
func TestAttentionTools_TakeTheTenantArgument(t *testing.T) {
	t.Parallel()
	reg, _ := newMCPSDLCRegistry(t)

	_, err := reg.ExecuteTool(context.Background(), "quicbaas_orchestrator_attention_list",
		map[string]interface{}{"tenant_id": "tenant-a"})
	require.NoError(t, err)

	_, err = reg.ExecuteTool(context.Background(), "quicbaas_orchestrator_attention_resolve",
		map[string]interface{}{"tenant_id": "tenant-a", "task_id": "t1", "action": "readmit"})
	var invalid *sdlc.InvalidInputError
	if errors.As(err, &invalid) && invalid.Field == "tenant_id" {
		t.Fatalf("resolve ignored the tenant_id argument: %v", err)
	}
}

// TestAttentionTools_RefuseWithNoTenant proves both tools still refuse a call
// that names no tenant from a caller bound to none.
func TestAttentionTools_RefuseWithNoTenant(t *testing.T) {
	t.Parallel()
	reg, _ := newMCPSDLCRegistry(t)
	var required *api.RequiredFieldError

	_, err := reg.ExecuteTool(context.Background(), "quicbaas_orchestrator_attention_list", map[string]interface{}{})
	require.ErrorAs(t, err, &required)

	_, err = reg.ExecuteTool(context.Background(), "quicbaas_orchestrator_attention_resolve",
		map[string]interface{}{"task_id": "t1", "action": "readmit"})
	require.ErrorAs(t, err, &required)
}

// TestTaskTools_TakeTheTenantArgument covers the task tools that shared the
// defect: each hands the argument's tenant to the service, on a context the
// tenant guard can read.
func TestTaskTools_TakeTheTenantArgument(t *testing.T) {
	t.Parallel()
	var gotTenant, gotCtxTenant string
	svc := &mockAgentService{readmitTaskFn: func(ctx context.Context, tenantID, _ string) error {
		gotTenant, gotCtxTenant = tenantID, orchestration.TenantFromContext(ctx)
		return nil
	}}
	_, err := newFleetRegistry(t, svc).executeAgentTool(context.Background(), "quicbaas_orchestrator_task_readmit",
		map[string]interface{}{"tenant_id": "acme", "task_id": "task-1"})
	require.NoError(t, err)
	assert.Equal(t, "acme", gotTenant)
	assert.Equal(t, "acme", gotCtxTenant)
}
