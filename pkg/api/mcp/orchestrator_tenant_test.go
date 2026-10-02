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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestOrchestratorTenantContext pins how the orchestrator tools resolve their
// tenant (go-quicbaas#1150): the caller's bound tenant, which a tenant_id
// argument may only repeat. The resolved tenant is stamped on both the central
// key and the orchestration key, the one the orchestrator's tenant guard reads.
func TestOrchestratorTenantContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ctx  context.Context
		args map[string]interface{}
		want string
	}{
		{"the caller's binding", tenantctx.WithTenant(context.Background(), "acme"), map[string]interface{}{}, "acme"},
		{"an argument repeating the binding", tenantctx.WithTenant(context.Background(), "acme"), map[string]interface{}{"tenant_id": "acme"}, "acme"},
		{"the system tenant acting for itself", tenantctx.WithSystem(context.Background()), map[string]interface{}{}, tenantctx.SystemTenantID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, tenantID, err := orchestratorTenantContext(tc.ctx, tc.args)
			require.NoError(t, err)
			assert.Equal(t, tc.want, tenantID)
			assert.Equal(t, tc.want, orchestration.TenantFromContext(ctx))
			bound, err := tenantctx.FromContext(ctx, "test")
			require.NoError(t, err)
			assert.Equal(t, tc.want, bound)
		})
	}
}

// TestOrchestratorTenantContext_RefusesWhatTheCallerMayNotName is the
// go-quicbaas#1150 regression: a tenant_id argument used to outrank the
// caller's binding, and an unbound caller was trusted to name its own tenant.
func TestOrchestratorTenantContext_RefusesWhatTheCallerMayNotName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ctx  context.Context
		args map[string]interface{}
	}{
		{"argument from an unbound caller", context.Background(), map[string]interface{}{"tenant_id": "acme"}},
		{"no tenant anywhere", context.Background(), map[string]interface{}{}},
		{"an orchestration binding alone", orchestration.ContextWithTenant(context.Background(), "acme"), map[string]interface{}{}},
		{"a tenant naming another", tenantctx.WithTenant(context.Background(), "acme"), map[string]interface{}{"tenant_id": "globex"}},
		{"the system tenant naming a tenant", tenantctx.WithSystem(context.Background()), map[string]interface{}{"tenant_id": "acme"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := orchestratorTenantContext(tc.ctx, tc.args)
			require.Error(t, err)
			var missing *tenantctx.MissingTenantError
			var cross *tenantctx.CrossTenantError
			assert.True(t, errors.As(err, &missing) || errors.As(err, &cross), "want a tenantctx refusal, got %v", err)
		})
	}
}

// TestAttentionTools_ActForTheBoundTenant: the inbox is read and resolved in
// the caller's bound tenant.
func TestAttentionTools_ActForTheBoundTenant(t *testing.T) {
	t.Parallel()
	reg, _ := newMCPSDLCRegistry(t)
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

	_, err := reg.ExecuteTool(ctx, "quicbaas_orchestrator_attention_list",
		map[string]interface{}{"tenant_id": "tenant-a"})
	require.NoError(t, err)

	_, err = reg.ExecuteTool(ctx, "quicbaas_orchestrator_attention_resolve",
		map[string]interface{}{"task_id": "t1", "action": "readmit"})
	var invalid *sdlc.InvalidInputError
	if errors.As(err, &invalid) && invalid.Field == "tenant_id" {
		t.Fatalf("resolve did not act for the bound tenant: %v", err)
	}
}

// TestAttentionTools_RefuseAnUnboundCaller proves both tools refuse a caller
// bound to no tenant, whether or not it names one.
func TestAttentionTools_RefuseAnUnboundCaller(t *testing.T) {
	t.Parallel()
	reg, _ := newMCPSDLCRegistry(t)
	var missing *tenantctx.MissingTenantError

	_, err := reg.ExecuteTool(context.Background(), "quicbaas_orchestrator_attention_list",
		map[string]interface{}{"tenant_id": "tenant-a"})
	require.ErrorAs(t, err, &missing)

	_, err = reg.ExecuteTool(context.Background(), "quicbaas_orchestrator_attention_resolve",
		map[string]interface{}{"task_id": "t1", "action": "readmit"})
	require.ErrorAs(t, err, &missing)
}

// TestTaskTools_ActForTheBoundTenant covers the task tools: each hands the
// caller's tenant to the service, on a context the tenant guard can read.
func TestTaskTools_ActForTheBoundTenant(t *testing.T) {
	t.Parallel()
	var gotTenant, gotCtxTenant string
	svc := &mockAgentService{readmitTaskFn: func(ctx context.Context, tenantID, _ string) error {
		gotTenant, gotCtxTenant = tenantID, orchestration.TenantFromContext(ctx)
		return nil
	}}
	_, err := newFleetRegistry(t, svc).executeAgentTool(tenantctx.WithTenant(context.Background(), "acme"),
		"quicbaas_orchestrator_task_readmit", map[string]interface{}{"tenant_id": "acme", "task_id": "task-1"})
	require.NoError(t, err)
	assert.Equal(t, "acme", gotTenant)
	assert.Equal(t, "acme", gotCtxTenant)
}
