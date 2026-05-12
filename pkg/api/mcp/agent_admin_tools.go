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
// Agent Admin Tool Registration
// =============================================================================

// registerAgentAdminTools registers MCP tools for global agent lifecycle management.
func (r *ToolRegistry) registerAgentAdminTools() {
	r.addTool(ToolDefinition{
		Name:        "agent_disable",
		Description: "Administratively disable an agent globally. Prevents all work claims and authentication until re-enabled. Emits agent.disabled audit event.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID that owns the agent",
				},
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "Agent UUID to disable",
				},
			},
			"required": []string{"tenant_id", "agent_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "agent_enable",
		Description: "Re-activate a previously disabled agent. Allows it to authenticate and claim work again. Emits agent.enabled audit event.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID that owns the agent",
				},
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "Agent UUID to re-enable",
				},
			},
			"required": []string{"tenant_id", "agent_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "project_grant_agent",
		Description: "Grant or update a project role for an agent. Roles: lead (full orchestration), worker (task execution), observer (read-only), denied (blocked).",
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
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "Agent UUID to grant the role to",
				},
				"role": map[string]interface{}{
					"type":        "string",
					"description": "Role to assign: lead, worker, observer, or denied",
					"enum":        []string{"lead", "worker", "observer", "denied"},
				},
			},
			"required": []string{"tenant_id", "project_id", "agent_id", "role"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "project_revoke_agent",
		Description: "Revoke an agent's project access by setting its role to denied. The agent stays online and can still work on other projects.",
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
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "Agent UUID to revoke access from",
				},
			},
			"required": []string{"tenant_id", "project_id", "agent_id"},
		},
	})
}

// =============================================================================
// Execution Handlers
// =============================================================================

// executeAgentDisable handles the agent_disable MCP tool call.
func (r *ToolRegistry) executeAgentDisable(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	adminSvc, err := r.getAgentAdminService()
	if err != nil {
		return nil, err
	}

	resp, err := adminSvc.DisableAgent(ctx, &service.DisableAgentRequest{
		TenantID: getString(args, "tenant_id"),
		AgentID:  getString(args, "agent_id"),
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// executeAgentEnable handles the agent_enable MCP tool call.
func (r *ToolRegistry) executeAgentEnable(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	adminSvc, err := r.getAgentAdminService()
	if err != nil {
		return nil, err
	}

	resp, err := adminSvc.EnableAgent(ctx, &service.EnableAgentRequest{
		TenantID: getString(args, "tenant_id"),
		AgentID:  getString(args, "agent_id"),
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// executeProjectGrantAgent handles the project_grant_agent MCP tool call.
func (r *ToolRegistry) executeProjectGrantAgent(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	projectSvc, err := r.getProjectService()
	if err != nil {
		return nil, err
	}

	roleStr := getString(args, "role")
	resp, err := projectSvc.GrantAgentRole(ctx, &service.GrantAgentRoleRequest{
		TenantID:  getString(args, "tenant_id"),
		ProjectID: getString(args, "project_id"),
		AgentID:   getString(args, "agent_id"),
		Role:      orchestration.ProjectRole(roleStr),
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// executeProjectRevokeAgent handles the project_revoke_agent MCP tool call.
func (r *ToolRegistry) executeProjectRevokeAgent(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	projectSvc, err := r.getProjectService()
	if err != nil {
		return nil, err
	}

	resp, err := projectSvc.RevokeAgentRole(ctx, &service.RevokeAgentRoleRequest{
		TenantID:  getString(args, "tenant_id"),
		ProjectID: getString(args, "project_id"),
		AgentID:   getString(args, "agent_id"),
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// =============================================================================
// Dispatch
// =============================================================================

// isAgentAdminTool returns true when the tool name belongs to the agent admin group.
func isAgentAdminTool(name string) bool {
	switch name {
	case "agent_disable", "agent_enable", "project_grant_agent", "project_revoke_agent":
		return true
	default:
		return false
	}
}

// executeAgentAdminTool dispatches agent admin tool execution to the appropriate handler.
func (r *ToolRegistry) executeAgentAdminTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	handlers := map[string]func(context.Context, map[string]interface{}) (interface{}, error){
		"agent_disable":        r.executeAgentDisable,
		"agent_enable":         r.executeAgentEnable,
		"project_grant_agent":  r.executeProjectGrantAgent,
		"project_revoke_agent": r.executeProjectRevokeAgent,
	}
	if h, ok := handlers[name]; ok {
		return h(ctx, args)
	}
	return nil, api.NewUnknownToolError(name)
}

// =============================================================================
// Helpers
// =============================================================================

// getAgentAdminService retrieves the AgentAdminService from the QuicBaaS service.
func (r *ToolRegistry) getAgentAdminService() (service.AgentAdminService, error) {
	adminSvc := r.service.AgentAdmin()
	if adminSvc == nil {
		return nil, api.NewServiceUnavailableError("agent-admin")
	}
	return adminSvc, nil
}
