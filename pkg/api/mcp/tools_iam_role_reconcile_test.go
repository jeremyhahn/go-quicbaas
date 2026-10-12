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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// reconcileToolName is the wire name of the built-in role reconciliation tool.
// It is fixed across every transport, so a rename here is a breaking change.
const reconcileToolName = "quicbaas_iam_role_reconcile"

// TestIAMRoleReconcileTool_Registered pins the tool onto the registry with the
// schema the other transports send: a single required tenant_id.
func TestIAMRoleReconcileTool_Registered(t *testing.T) {
	registry := NewToolRegistry(&iamQuicBaaSWrapper{
		MockQuicBaaS: &MockQuicBaaS{},
		iamSvc:       &mockIAMService{},
	}, newTestLogger(t))

	var tool ToolDefinition
	found := false
	for _, candidate := range registry.ListTools() {
		if candidate.Name == reconcileToolName {
			tool = candidate
			found = true
			break
		}
	}
	require.True(t, found, "tool %s is not registered", reconcileToolName)

	assert.NotEmpty(t, tool.Description)
	assert.Equal(t, "object", tool.InputSchema["type"])

	properties, ok := tool.InputSchema["properties"].(map[string]interface{})
	require.True(t, ok, "schema has no properties object")
	assert.Contains(t, properties, "tenant_id")

	required, ok := tool.InputSchema["required"].([]string)
	require.True(t, ok, "schema has no required array")
	assert.Equal(t, []string{"tenant_id"}, required)
}

// TestExecuteIAMRoleReconcile_ReturnsReport proves the executor forwards the
// tenant onto the service and marshals the whole report back — the per-role
// added/removed verbs are the answer, so dropping them would make the tool
// report a silent success.
func TestExecuteIAMRoleReconcile_ReturnsReport(t *testing.T) {
	var captured *service.IAMReconcileRolesRequest
	reg := newIAMTestRegistry(&mockIAMService{
		reconcileRolesFn: func(ctx context.Context, req *service.IAMReconcileRolesRequest) (*service.IAMReconcileRolesResponse, error) {
			captured = req
			return &service.IAMReconcileRolesResponse{
				TenantID: req.TenantID,
				Roles: []iam.RoleReconcileResult{
					{
						RoleID:  "role-admin",
						Name:    "admin",
						Added:   []string{"roles:create"},
						Removed: []string{"roles:reconcile"},
					},
					{RoleID: "role-viewer", Name: "viewer", Created: true},
				},
				Changed:   true,
				Timestamp: time.Now(),
			}, nil
		},
	})

	result, err := reg.executeIAMTool(tenantctx.WithTenant(context.Background(), "acme"), reconcileToolName, map[string]interface{}{
		"tenant_id": "acme",
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "acme", captured.TenantID)

	report, ok := result.(map[string]interface{})
	require.True(t, ok, "result is %T, want a marshalled object", result)
	assert.Equal(t, "acme", report["tenant_id"])
	assert.Equal(t, true, report["changed"])

	roles, ok := report["roles"].([]interface{})
	require.True(t, ok, "roles is %T, want an array", report["roles"])
	require.Len(t, roles, 2)

	admin, ok := roles[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "role-admin", admin["role_id"])
	assert.Equal(t, "admin", admin["name"])
	assert.Equal(t, []interface{}{"roles:create"}, admin["added"])
	assert.Equal(t, []interface{}{"roles:reconcile"}, admin["removed"])

	viewer, ok := roles[1].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, viewer["created"])
}

// TestExecuteIAMRoleReconcile_Idempotent proves a converged tenant reports no
// changed roles rather than an error: reconciling twice is a supported call.
func TestExecuteIAMRoleReconcile_Idempotent(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{
		reconcileRolesFn: func(ctx context.Context, req *service.IAMReconcileRolesRequest) (*service.IAMReconcileRolesResponse, error) {
			return &service.IAMReconcileRolesResponse{
				TenantID:  req.TenantID,
				Changed:   false,
				Timestamp: time.Now(),
			}, nil
		},
	})

	result, err := reg.executeIAMRoleReconcile(tenantctx.WithTenant(context.Background(), "acme"), map[string]interface{}{
		"tenant_id": "acme",
	})
	require.NoError(t, err)

	report, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, false, report["changed"])
	assert.NotContains(t, report, "roles")
}

// TestExecuteIAMRoleReconcile_ServiceError proves a service failure surfaces
// unchanged instead of being reported as an empty reconciliation.
func TestExecuteIAMRoleReconcile_ServiceError(t *testing.T) {
	reconcileErr := errors.New("barrier is sealed")
	reg := newIAMTestRegistry(&mockIAMService{
		reconcileRolesFn: func(ctx context.Context, req *service.IAMReconcileRolesRequest) (*service.IAMReconcileRolesResponse, error) {
			return nil, reconcileErr
		},
	})

	result, err := reg.executeIAMTool(tenantctx.WithTenant(context.Background(), "acme"), reconcileToolName, map[string]interface{}{
		"tenant_id": "acme",
	})
	require.Error(t, err)
	assert.Equal(t, reconcileErr, err)
	assert.Nil(t, result)
}

// TestExecuteIAMRoleReconcile_ServiceUnavailable proves the tool refuses when
// the host exposes no IAM service, rather than panicking on a nil accessor.
func TestExecuteIAMRoleReconcile_ServiceUnavailable(t *testing.T) {
	// mockService does not implement iamServiceAccessor.
	reg := NewToolRegistry(&mockService{}, newTestLogger(t))

	_, err := reg.executeIAMRoleReconcile(tenantctx.WithTenant(context.Background(), "acme"), map[string]interface{}{
		"tenant_id": "acme",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IAM service not available")
}

// TestIAMRoleReconcile_CarriesRoleCreateGrant pins the authz mapping onto the
// role-CREATE family. A dedicated roles:reconcile verb would be missing from
// every role seeded before it existed, so nobody would hold the grant needed to
// repair that exact absence.
func TestIAMRoleReconcile_CarriesRoleCreateGrant(t *testing.T) {
	resource, action, mapped := getToolQRDBPermission(reconcileToolName)
	require.True(t, mapped, "tool %s has no qrdb permission mapping", reconcileToolName)

	createResource, createAction, createMapped := getToolQRDBPermission("quicbaas_iam_role_create")
	require.True(t, createMapped)

	assert.Equal(t, createResource, resource)
	assert.Equal(t, createAction, action)
	assert.False(t, IsPublicTool(reconcileToolName), "reconciliation is an admin verb, never public")
}
