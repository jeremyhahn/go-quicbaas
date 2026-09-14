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
	"strconv"

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
				"default_workflow": map[string]interface{}{
					"type":        "string",
					"description": "Tenant-scoped workflow a Task under this project auto-spawns into (must be registered in the tenant)",
				},
				"git_author_name": map[string]interface{}{
					"type":        "string",
					"description": "Author name recorded on commits made from the workspace",
				},
				"git_author_email": map[string]interface{}{
					"type":        "string",
					"description": "Author email recorded on commits made from the workspace",
				},
				"git_signing_enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "Sign commits with the KMS-held signing key (an enabled policy must name git_signing_key_name)",
				},
				"git_signing_algorithm": map[string]interface{}{
					"type":        "string",
					"description": "Signing key algorithm: ED25519 (default) or ECDSA-P256 (FIPS)",
				},
				"git_signing_key_name": map[string]interface{}{
					"type":        "string",
					"description": "KMS signing key name",
				},
				"git_signing_key_scope": map[string]interface{}{
					"type":        "string",
					"description": "KMS signing key scope (user, tenant, service)",
				},
				"git_signing_key_owner": map[string]interface{}{
					"type":        "string",
					"description": "KMS signing key owner",
				},
				"git_ssh_key_name": map[string]interface{}{
					"type":        "string",
					"description": "KMS SSH key an ssh:// repository is reached with (defaults to the signing key)",
				},
				"git_ssh_key_scope": map[string]interface{}{
					"type":        "string",
					"description": "KMS SSH key scope (user, tenant, service)",
				},
				"git_ssh_key_owner": map[string]interface{}{
					"type":        "string",
					"description": "KMS SSH key owner",
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
				"autonomous": map[string]interface{}{
					"type":        "boolean",
					"description": "Turn autonomous development on: the poller adopts work for this project and the dispatcher starts jobs for it",
				},
				"context_repos": map[string]interface{}{
					"type":        "array",
					"description": "Repositories the worker clones beside the project checkout",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"name":     map[string]interface{}{"type": "string", "description": "Directory the repository is cloned into (one path segment)"},
							"repo_url": map[string]interface{}{"type": "string", "description": "Remote the worker clones from"},
							"ref":      map[string]interface{}{"type": "string", "description": "Branch, tag or commit the worker checks out"},
						},
						"required": []string{"name", "repo_url", "ref"},
					},
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
				"default_workflow": map[string]interface{}{
					"type":        "string",
					"description": "Tenant-scoped workflow a Task under this project auto-spawns into (must be registered in the tenant)",
				},
				"git_author_name": map[string]interface{}{
					"type":        "string",
					"description": "Author name recorded on commits made from the workspace",
				},
				"git_author_email": map[string]interface{}{
					"type":        "string",
					"description": "Author email recorded on commits made from the workspace",
				},
				"git_signing_enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "Sign commits with the KMS-held signing key (an enabled policy must name git_signing_key_name)",
				},
				"git_signing_algorithm": map[string]interface{}{
					"type":        "string",
					"description": "Signing key algorithm: ED25519 (default) or ECDSA-P256 (FIPS)",
				},
				"git_signing_key_name": map[string]interface{}{
					"type":        "string",
					"description": "KMS signing key name",
				},
				"git_signing_key_scope": map[string]interface{}{
					"type":        "string",
					"description": "KMS signing key scope (user, tenant, service)",
				},
				"git_signing_key_owner": map[string]interface{}{
					"type":        "string",
					"description": "KMS signing key owner",
				},
				"git_ssh_key_name": map[string]interface{}{
					"type":        "string",
					"description": "KMS SSH key an ssh:// repository is reached with (defaults to the signing key)",
				},
				"git_ssh_key_scope": map[string]interface{}{
					"type":        "string",
					"description": "KMS SSH key scope (user, tenant, service)",
				},
				"git_ssh_key_owner": map[string]interface{}{
					"type":        "string",
					"description": "KMS SSH key owner",
				},
				"clear_default_workflow": map[string]interface{}{
					"type":        "boolean",
					"description": "Remove the default workflow so Tasks are never auto-spawned",
				},
				"autonomous": map[string]interface{}{
					"type":        "boolean",
					"description": "Turn autonomous development on: the poller adopts work for this project and the dispatcher starts jobs for it",
				},
				"context_repos": map[string]interface{}{
					"type":        "array",
					"description": "Repositories the worker clones beside the project checkout",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"name":     map[string]interface{}{"type": "string", "description": "Directory the repository is cloned into (one path segment)"},
							"repo_url": map[string]interface{}{"type": "string", "description": "Remote the worker clones from"},
							"ref":      map[string]interface{}{"type": "string", "description": "Branch, tag or commit the worker checks out"},
						},
						"required": []string{"name", "repo_url", "ref"},
					},
				},
				"clear_context_repos": map[string]interface{}{
					"type":        "boolean",
					"description": "Remove every context repository from the project",
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

	contextRepos, err := getContextRepos(args, "context_repos")
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

		DefaultWorkflow:     getString(args, "default_workflow"),
		Autonomous:          getBool(args, "autonomous"),
		ContextRepos:        contextRepos,
		GitAuthorName:       getString(args, "git_author_name"),
		GitAuthorEmail:      getString(args, "git_author_email"),
		GitSigningEnabled:   getBool(args, "git_signing_enabled"),
		GitSigningAlgorithm: getString(args, "git_signing_algorithm"),
		GitSigningKeyName:   getString(args, "git_signing_key_name"),
		GitSigningKeyScope:  getString(args, "git_signing_key_scope"),
		GitSigningKeyOwner:  getString(args, "git_signing_key_owner"),
		GitSSHKeyName:       getString(args, "git_ssh_key_name"),
		GitSSHKeyScope:      getString(args, "git_ssh_key_scope"),
		GitSSHKeyOwner:      getString(args, "git_ssh_key_owner"),
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

	contextRepos, err := getContextRepos(args, "context_repos")
	if err != nil {
		return nil, err
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
		TokenBudget:      getOptionalInt64(args, "token_budget"),
		GPUMinutes:       getOptionalInt64(args, "gpu_minutes"),
		MaxConcurrency:   getOptionalInt(args, "max_concurrency"),

		DefaultWorkflow:      getString(args, "default_workflow"),
		ClearDefaultWorkflow: getBool(args, "clear_default_workflow"),
		Autonomous:           getOptionalBool(args, "autonomous"),
		ContextRepos:         contextRepos,
		ClearContextRepos:    getBool(args, "clear_context_repos"),
		GitAuthorName:        getString(args, "git_author_name"),
		GitAuthorEmail:       getString(args, "git_author_email"),
		GitSigningEnabled:    getOptionalBool(args, "git_signing_enabled"),
		GitSigningAlgorithm:  getString(args, "git_signing_algorithm"),
		GitSigningKeyName:    getString(args, "git_signing_key_name"),
		GitSigningKeyScope:   getString(args, "git_signing_key_scope"),
		GitSigningKeyOwner:   getString(args, "git_signing_key_owner"),
		GitSSHKeyName:        getString(args, "git_ssh_key_name"),
		GitSSHKeyScope:       getString(args, "git_ssh_key_scope"),
		GitSSHKeyOwner:       getString(args, "git_ssh_key_owner"),
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

// getOptionalBool returns the boolean argument when present, or nil when the
// caller did not supply it — so an update can tell "unchanged" from "off".
func getOptionalBool(args map[string]interface{}, key string) *bool {
	v, ok := args[key].(bool)
	if !ok {
		return nil
	}
	return &v
}

// getOptionalInt64 reads a numeric argument that may legitimately be ZERO.
//
// An update quota of zero means UNLIMITED, so an absent argument and an
// explicit zero are different instructions and getInt — which answers 0 for
// both — could not tell them apart. JSON numbers decode as float64, and an
// integer-valued float is the only shape a quota arrives in.
func getOptionalInt64(args map[string]interface{}, key string) *int64 {
	raw, present := args[key]
	if !present {
		return nil
	}
	switch v := raw.(type) {
	case float64:
		n := int64(v)
		return &n
	case int64:
		return &v
	case int:
		n := int64(v)
		return &n
	default:
		return nil
	}
}

// getOptionalInt is getOptionalInt64 narrowed to int, for the one quota the
// project record keeps as an int.
func getOptionalInt(args map[string]interface{}, key string) *int {
	v := getOptionalInt64(args, key)
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}

// ContextRepoArgumentError reports a context_repos tool argument the registry
// could not read as a list of context repositories.
type ContextRepoArgumentError struct {
	// Key is the tool argument that carried the bad value.
	Key string

	// Index is the offending element, or -1 when the argument itself is
	// the wrong shape.
	Index int
}

// Error implements the error interface.
func (e *ContextRepoArgumentError) Error() string {
	if e.Index < 0 {
		return "mcp: " + e.Key + " must be an array of context repository objects"
	}
	return "mcp: " + e.Key + " element " + strconv.Itoa(e.Index) +
		" must be an object with name, repo_url and ref"
}

// getContextRepos reads a context repository list from the tool arguments.
// A missing argument yields a nil list, so an update leaves the project's
// repositories alone; a malformed one is a typed error rather than a silent
// drop.
func getContextRepos(args map[string]interface{}, key string) ([]orchestration.ContextRepo, error) {
	raw, present := args[key]
	if !present || raw == nil {
		return nil, nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil, &ContextRepoArgumentError{Key: key, Index: -1}
	}
	repos := make([]orchestration.ContextRepo, 0, len(items))
	for i, item := range items {
		fields, ok := item.(map[string]interface{})
		if !ok {
			return nil, &ContextRepoArgumentError{Key: key, Index: i}
		}
		repos = append(repos, orchestration.ContextRepo{
			Name:    getStringFromMap(fields, "name"),
			RepoURL: getStringFromMap(fields, "repo_url"),
			Ref:     getStringFromMap(fields, "ref"),
		})
	}
	return repos, nil
}
