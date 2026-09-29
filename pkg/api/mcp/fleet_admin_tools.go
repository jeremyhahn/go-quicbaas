//go:build mcp

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

package mcp

import (
	"context"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Fleet Admin Tool Registration
// =============================================================================

// registerFleetAdminTools registers MCP tools for global fleet node lifecycle management.
func (r *ToolRegistry) registerFleetAdminTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_fleet_disable",
		Description: "Administratively disable a fleet node globally. Prevents all work claims and authentication until re-enabled. Emits agent.disabled audit event.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID that owns the fleet node",
				},
				"node_id": map[string]interface{}{
					"type":        "string",
					"description": "Fleet node ID to disable",
				},
			},
			"required": []string{"tenant_id", "node_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_fleet_enable",
		Description: "Re-activate a previously disabled fleet node. Allows it to authenticate and claim work again. Emits agent.enabled audit event.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID that owns the fleet node",
				},
				"node_id": map[string]interface{}{
					"type":        "string",
					"description": "Fleet node ID to re-enable",
				},
			},
			"required": []string{"tenant_id", "node_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "project_grant_fleet_node",
		Description: "Grant or update a project role for a fleet node. Roles: lead (full orchestration), worker (task execution), observer (read-only), denied (blocked).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID that owns the project",
				},
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "Project UUID",
				},
				"node_id": map[string]interface{}{
					"type":        "string",
					"description": "Fleet node ID to grant the role to",
				},
				"role": map[string]interface{}{
					"type":        "string",
					"description": "Role to assign: lead, worker, observer, or denied",
					"enum":        []string{"lead", "worker", "observer", "denied"},
				},
			},
			"required": []string{"tenant_id", "project_id", "node_id", "role"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "project_revoke_fleet_node",
		Description: "Revoke a fleet node's project access by setting its role to denied. The node stays online and can still work on other projects.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID that owns the project",
				},
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "Project UUID",
				},
				"node_id": map[string]interface{}{
					"type":        "string",
					"description": "Fleet node ID to revoke access from",
				},
			},
			"required": []string{"tenant_id", "project_id", "node_id"},
		},
	})
}

// =============================================================================
// Execution Handlers
// =============================================================================

// executeFleetDisable handles the quicbaas_fleet_disable MCP tool call.
func (r *ToolRegistry) executeFleetDisable(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	adminSvc, err := r.getFleetAdminService()
	if err != nil {
		return nil, err
	}

	resp, err := adminSvc.DisableFleetNode(ctx, &service.DisableFleetNodeRequest{
		TenantID: getString(args, "tenant_id"),
		NodeID:   getString(args, "node_id"),
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// executeFleetEnable handles the quicbaas_fleet_enable MCP tool call.
func (r *ToolRegistry) executeFleetEnable(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	adminSvc, err := r.getFleetAdminService()
	if err != nil {
		return nil, err
	}

	resp, err := adminSvc.EnableFleetNode(ctx, &service.EnableFleetNodeRequest{
		TenantID: getString(args, "tenant_id"),
		NodeID:   getString(args, "node_id"),
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// executeProjectGrantFleetNode handles the project_grant_fleet_node MCP tool call.
func (r *ToolRegistry) executeProjectGrantFleetNode(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	projectSvc, err := r.getProjectService()
	if err != nil {
		return nil, err
	}

	roleStr := getString(args, "role")
	resp, err := projectSvc.GrantFleetNodeRole(ctx, &service.GrantFleetNodeRoleRequest{
		TenantID:    getString(args, "tenant_id"),
		ProjectID:   getString(args, "project_id"),
		FleetNodeID: getString(args, "node_id"),
		Role:        orchestration.ProjectRole(roleStr),
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// executeProjectRevokeFleetNode handles the project_revoke_fleet_node MCP tool call.
func (r *ToolRegistry) executeProjectRevokeFleetNode(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	projectSvc, err := r.getProjectService()
	if err != nil {
		return nil, err
	}

	resp, err := projectSvc.RevokeFleetNodeRole(ctx, &service.RevokeFleetNodeRoleRequest{
		TenantID:    getString(args, "tenant_id"),
		ProjectID:   getString(args, "project_id"),
		FleetNodeID: getString(args, "node_id"),
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// =============================================================================
// Dispatch
// =============================================================================

// isFleetAdminTool returns true when the tool name belongs to the fleet admin group.
func isFleetAdminTool(name string) bool {
	switch name {
	case "quicbaas_fleet_disable", "quicbaas_fleet_enable", "project_grant_fleet_node", "project_revoke_fleet_node":
		return true
	default:
		return false
	}
}

// executeFleetAdminTool dispatches fleet admin tool execution to the appropriate handler.
func (r *ToolRegistry) executeFleetAdminTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	handlers := map[string]func(context.Context, map[string]interface{}) (interface{}, error){
		"quicbaas_fleet_disable":    r.executeFleetDisable,
		"quicbaas_fleet_enable":     r.executeFleetEnable,
		"project_grant_fleet_node":  r.executeProjectGrantFleetNode,
		"project_revoke_fleet_node": r.executeProjectRevokeFleetNode,
	}
	if h, ok := handlers[name]; ok {
		return h(ctx, args)
	}
	return nil, api.NewUnknownToolError(name)
}

// =============================================================================
// Helpers
// =============================================================================

// getFleetAdminService retrieves the FleetAdminService from the QuicBaaS service.
func (r *ToolRegistry) getFleetAdminService() (service.FleetAdminService, error) {
	adminSvc := r.service.FleetAdmin()
	if adminSvc == nil {
		return nil, api.NewServiceUnavailableError("fleet-admin")
	}
	return adminSvc, nil
}
