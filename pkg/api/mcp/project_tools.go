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

// registerProjectTools registers all project management MCP tools.
func (r *ToolRegistry) registerProjectTools() {
	r.addTool(ToolDefinition{
		Name:        "project_create",
		Description: "Create a new project within a tenant. Projects are long-running agentic work units with their own workspace, git repo, and permissions.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID that owns this project",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Unique project name within the tenant",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Human-readable description of the project",
				},
				"workspace_agent_id": map[string]interface{}{
					"type":        "string",
					"description": "ID of the agent that owns the project workspace",
				},
				"workspace_path": map[string]interface{}{
					"type":        "string",
					"description": "Filesystem path of the workspace on the agent host",
				},
				"host_affinity": map[string]interface{}{
					"type":        "string",
					"description": "Preferred agent host for project operations (optional)",
				},
				"default_lead_agent": map[string]interface{}{
					"type":        "string",
					"description": "Default lead agent ID for workflow runs (optional)",
				},
				"git_repo_url": map[string]interface{}{
					"type":        "string",
					"description": "Remote git repository URL",
				},
				"git_default_branch": map[string]interface{}{
					"type":        "string",
					"description": "Default git branch (e.g. 'main')",
				},
				"token_budget": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum LLM token budget (0 = unlimited)",
				},
				"gpu_minutes": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum GPU compute budget in minutes (0 = unlimited)",
				},
				"max_concurrency": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum concurrent workflow runs (0 = unlimited)",
				},
			},
			"required": []string{"tenant_id", "name"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "project_get",
		Description: "Retrieve a project by ID within a tenant.",
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
			},
			"required": []string{"tenant_id", "project_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "project_list",
		Description: "List all projects for a given tenant.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID whose projects to list",
				},
			},
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "project_update",
		Description: "Update an existing project. Only supplied fields are changed.",
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
				"name": map[string]interface{}{
					"type":        "string",
					"description": "New project name (optional)",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Updated description (optional)",
				},
				"workspace_agent_id": map[string]interface{}{
					"type":        "string",
					"description": "Updated workspace agent ID (optional)",
				},
				"workspace_path": map[string]interface{}{
					"type":        "string",
					"description": "Updated workspace path (optional)",
				},
				"git_current_sha": map[string]interface{}{
					"type":        "string",
					"description": "Updated current git SHA, set by agents during job runs",
				},
				"git_repo_url": map[string]interface{}{
					"type":        "string",
					"description": "Updated git repo URL (optional)",
				},
			},
			"required": []string{"tenant_id", "project_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "project_delete",
		Description: "Delete a project by ID.",
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
			},
			"required": []string{"tenant_id", "project_id"},
		},
	})
}

// =============================================================================
// Execution Handlers
// =============================================================================

// executeProjectCreate handles the project_create MCP tool call.
func (r *ToolRegistry) executeProjectCreate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	projectSvc, err := r.getProjectService()
	if err != nil {
		return nil, err
	}

	resp, err := projectSvc.CreateProject(ctx, &service.CreateProjectRequest{
		TenantID:         getString(args, "tenant_id"),
		Name:             getString(args, "name"),
		Description:      getString(args, "description"),
		WorkspaceAgentID: getString(args, "workspace_agent_id"),
		WorkspacePath:    getString(args, "workspace_path"),
		HostAffinity:     getString(args, "host_affinity"),
		DefaultLeadAgent: getString(args, "default_lead_agent"),
		GitRepoURL:       getString(args, "git_repo_url"),
		GitDefaultBranch: getString(args, "git_default_branch"),
		TokenBudget:      int64(getInt(args, "token_budget")),
		GPUMinutes:       int64(getInt(args, "gpu_minutes")),
		MaxConcurrency:   getInt(args, "max_concurrency"),
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// executeProjectGet handles the project_get MCP tool call.
func (r *ToolRegistry) executeProjectGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	projectSvc, err := r.getProjectService()
	if err != nil {
		return nil, err
	}

	resp, err := projectSvc.GetProject(ctx, &service.GetProjectRequest{
		TenantID:  getString(args, "tenant_id"),
		ProjectID: getString(args, "project_id"),
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// executeProjectList handles the project_list MCP tool call.
func (r *ToolRegistry) executeProjectList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	projectSvc, err := r.getProjectService()
	if err != nil {
		return nil, err
	}

	resp, err := projectSvc.ListProjects(ctx, &service.ListProjectsRequest{
		TenantID: getString(args, "tenant_id"),
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// executeProjectUpdate handles the project_update MCP tool call.
func (r *ToolRegistry) executeProjectUpdate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	projectSvc, err := r.getProjectService()
	if err != nil {
		return nil, err
	}

	// Convert agent_roles from map[string]interface{} to map[string]ProjectRole.
	var agentRoles map[string]orchestration.ProjectRole
	if raw, ok := args["agent_roles"].(map[string]interface{}); ok {
		agentRoles = make(map[string]orchestration.ProjectRole, len(raw))
		for k, v := range raw {
			if s, ok := v.(string); ok {
				agentRoles[k] = orchestration.ProjectRole(s)
			}
		}
	}

	resp, err := projectSvc.UpdateProject(ctx, &service.UpdateProjectRequest{
		TenantID:         getString(args, "tenant_id"),
		ProjectID:        getString(args, "project_id"),
		Name:             getString(args, "name"),
		Description:      getString(args, "description"),
		WorkspaceAgentID: getString(args, "workspace_agent_id"),
		WorkspacePath:    getString(args, "workspace_path"),
		HostAffinity:     getString(args, "host_affinity"),
		DefaultLeadAgent: getString(args, "default_lead_agent"),
		GitRepoURL:       getString(args, "git_repo_url"),
		GitDefaultBranch: getString(args, "git_default_branch"),
		GitCurrentSHA:    getString(args, "git_current_sha"),
		AgentRoles:       agentRoles,
		TokenBudget:      int64(getInt(args, "token_budget")),
		GPUMinutes:       int64(getInt(args, "gpu_minutes")),
		MaxConcurrency:   getInt(args, "max_concurrency"),
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// executeProjectDelete handles the project_delete MCP tool call.
func (r *ToolRegistry) executeProjectDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	projectSvc, err := r.getProjectService()
	if err != nil {
		return nil, err
	}

	resp, err := projectSvc.DeleteProject(ctx, &service.DeleteProjectRequest{
		TenantID:  getString(args, "tenant_id"),
		ProjectID: getString(args, "project_id"),
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// =============================================================================
// Helpers
// =============================================================================

// getProjectService retrieves the ProjectService from the QuicBaaS service.
func (r *ToolRegistry) getProjectService() (service.ProjectService, error) {
	projectSvc := r.service.Project()
	if projectSvc == nil {
		return nil, api.NewServiceUnavailableError("project")
	}
	return projectSvc, nil
}

// isProjectTool returns true if the tool name is a project management tool.
func isProjectTool(name string) bool {
	switch name {
	case "project_create", "project_get", "project_list", "project_update", "project_delete":
		return true
	default:
		return false
	}
}

// executeProjectTool dispatches project tool execution to the appropriate handler.
func (r *ToolRegistry) executeProjectTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	handlers := map[string]func(context.Context, map[string]interface{}) (interface{}, error){
		"project_create": r.executeProjectCreate,
		"project_get":    r.executeProjectGet,
		"project_list":   r.executeProjectList,
		"project_update": r.executeProjectUpdate,
		"project_delete": r.executeProjectDelete,
	}
	if h, ok := handlers[name]; ok {
		return h(ctx, args)
	}
	return nil, api.NewUnknownToolError(name)
}

