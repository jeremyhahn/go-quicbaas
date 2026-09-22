//go:build rest

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

package rest

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Route Registration
// =============================================================================

// =============================================================================
// REST Request/Response Types
// =============================================================================

// CreateProjectRESTRequest is the JSON body for POST /api/v1/projects.
type CreateProjectRESTRequest struct {
	TenantID         string                               `json:"tenant_id"`
	Name             string                               `json:"name"`
	Description      string                               `json:"description,omitempty"`
	WorkspaceAgentID string                               `json:"workspace_agent_id,omitempty"`
	WorkspacePath    string                               `json:"workspace_path,omitempty"`
	HostAffinity     string                               `json:"host_affinity,omitempty"`
	DefaultLeadAgent string                               `json:"default_lead_agent,omitempty"`
	GitRepoURL       string                               `json:"git_repo_url,omitempty"`
	GitDefaultBranch string                               `json:"git_default_branch,omitempty"`
	AgentRoles       map[string]orchestration.ProjectRole `json:"agent_roles,omitempty"`
	TokenBudget      int64                                `json:"token_budget,omitempty"`
	GPUMinutes       int64                                `json:"gpu_minutes,omitempty"`
	MaxConcurrency   int                                  `json:"max_concurrency,omitempty"`
	Metadata         map[string]string                    `json:"metadata,omitempty"`

	// DefaultWorkflow names the tenant-scoped workflow a Task auto-spawns
	// into; it must be registered in the tenant.
	DefaultWorkflow string `json:"default_workflow,omitempty"`

	// Autonomous turns autonomous development on for the new project.
	Autonomous bool `json:"autonomous,omitempty"`

	// ContextRepos are cloned by the worker beside the project checkout.
	ContextRepos []orchestration.ContextRepo `json:"context_repos,omitempty"`

	// Commit identity, signing policy and access key of the git workspace.
	GitAuthorName       string `json:"git_author_name,omitempty"`
	GitAuthorEmail      string `json:"git_author_email,omitempty"`
	GitSigningEnabled   bool   `json:"git_signing_enabled,omitempty"`
	GitSigningAlgorithm string `json:"git_signing_algorithm,omitempty"`
	GitSigningKeyName   string `json:"git_signing_key_name,omitempty"`
	GitSigningKeyScope  string `json:"git_signing_key_scope,omitempty"`
	GitSigningKeyOwner  string `json:"git_signing_key_owner,omitempty"`
	GitSSHKeyName       string `json:"git_ssh_key_name,omitempty"`
	GitSSHKeyScope      string `json:"git_ssh_key_scope,omitempty"`
	GitSSHKeyOwner      string `json:"git_ssh_key_owner,omitempty"`
}

// UpdateProjectRESTRequest is the JSON body for PUT /api/v1/projects/{id}.
type UpdateProjectRESTRequest struct {
	Name             string                               `json:"name,omitempty"`
	Description      string                               `json:"description,omitempty"`
	ClearDescription bool                                 `json:"clear_description,omitempty"`
	WorkspaceAgentID string                               `json:"workspace_agent_id,omitempty"`
	WorkspacePath    string                               `json:"workspace_path,omitempty"`
	HostAffinity     string                               `json:"host_affinity,omitempty"`
	DefaultLeadAgent string                               `json:"default_lead_agent,omitempty"`
	GitRepoURL       string                               `json:"git_repo_url,omitempty"`
	GitDefaultBranch string                               `json:"git_default_branch,omitempty"`
	GitCurrentSHA    string                               `json:"git_current_sha,omitempty"`
	AgentRoles       map[string]orchestration.ProjectRole `json:"agent_roles,omitempty"`
	Metadata         map[string]string                    `json:"metadata,omitempty"`

	// The quotas are POINTERS so zero is expressible: zero means UNLIMITED
	// on ProjectQuotas, so treating it as "no value" made lifting a cap
	// impossible — the save succeeded and the old cap stayed. Absent leaves
	// the stored value alone, the same rule GitSigningEnabled uses.
	TokenBudget    *int64 `json:"token_budget,omitempty"`
	GPUMinutes     *int64 `json:"gpu_minutes,omitempty"`
	MaxConcurrency *int   `json:"max_concurrency,omitempty"`

	// DefaultWorkflow sets the auto-spawn workflow when non-empty;
	// ClearDefaultWorkflow removes it.
	DefaultWorkflow      string `json:"default_workflow,omitempty"`
	ClearDefaultWorkflow bool   `json:"clear_default_workflow,omitempty"`

	// Autonomous is a pointer so "unchanged" and "off" are distinct.
	Autonomous *bool `json:"autonomous,omitempty"`

	// ContextRepos replaces the whole list when non-nil; ClearContextRepos
	// empties it.
	ContextRepos      []orchestration.ContextRepo `json:"context_repos,omitempty"`
	ClearContextRepos bool                        `json:"clear_context_repos,omitempty"`

	// Commit identity, signing policy and access key of the git workspace.
	// GitSigningEnabled is a pointer so "unchanged" and "off" are distinct.
	GitAuthorName       string `json:"git_author_name,omitempty"`
	GitAuthorEmail      string `json:"git_author_email,omitempty"`
	GitSigningEnabled   *bool  `json:"git_signing_enabled,omitempty"`
	GitSigningAlgorithm string `json:"git_signing_algorithm,omitempty"`
	GitSigningKeyName   string `json:"git_signing_key_name,omitempty"`
	GitSigningKeyScope  string `json:"git_signing_key_scope,omitempty"`
	GitSigningKeyOwner  string `json:"git_signing_key_owner,omitempty"`
	GitSSHKeyName       string `json:"git_ssh_key_name,omitempty"`
	GitSSHKeyScope      string `json:"git_ssh_key_scope,omitempty"`
	GitSSHKeyOwner      string `json:"git_ssh_key_owner,omitempty"`
}

// ProjectRESTResponse mirrors orchestration.Project for JSON serialization.
type ProjectRESTResponse struct {
	ID               string                               `json:"id"`
	TenantID         string                               `json:"tenant_id"`
	Name             string                               `json:"name"`
	Description      string                               `json:"description,omitempty"`
	WorkspaceAgentID string                               `json:"workspace_agent_id,omitempty"`
	WorkspacePath    string                               `json:"workspace_path,omitempty"`
	HostAffinity     string                               `json:"host_affinity,omitempty"`
	DefaultLeadAgent string                               `json:"default_lead_agent,omitempty"`
	GitRepoURL       string                               `json:"git_repo_url,omitempty"`
	GitDefaultBranch string                               `json:"git_default_branch,omitempty"`
	GitCurrentSHA    string                               `json:"git_current_sha,omitempty"`
	AgentRoles       map[string]orchestration.ProjectRole `json:"agent_roles,omitempty"`
	TokenBudget      int64                                `json:"token_budget,omitempty"`
	GPUMinutes       int64                                `json:"gpu_minutes,omitempty"`
	MaxConcurrency   int                                  `json:"max_concurrency,omitempty"`
	Metadata         map[string]string                    `json:"metadata,omitempty"`
	CreatedAt        time.Time                            `json:"created_at"`
	UpdatedAt        time.Time                            `json:"updated_at"`

	// DefaultWorkflow is the tenant-scoped auto-spawn workflow, if any.
	DefaultWorkflow string `json:"default_workflow,omitempty"`

	// Autonomous reports whether autonomous development is on.
	Autonomous bool `json:"autonomous,omitempty"`

	// ContextRepos are cloned by the worker beside the project checkout.
	ContextRepos []orchestration.ContextRepo `json:"context_repos,omitempty"`

	// Commit identity, signing policy and access key of the git workspace.
	GitAuthorName       string `json:"git_author_name,omitempty"`
	GitAuthorEmail      string `json:"git_author_email,omitempty"`
	GitSigningEnabled   bool   `json:"git_signing_enabled,omitempty"`
	GitSigningAlgorithm string `json:"git_signing_algorithm,omitempty"`
	GitSigningKeyName   string `json:"git_signing_key_name,omitempty"`
	GitSigningKeyScope  string `json:"git_signing_key_scope,omitempty"`
	GitSigningKeyOwner  string `json:"git_signing_key_owner,omitempty"`
	GitSSHKeyName       string `json:"git_ssh_key_name,omitempty"`
	GitSSHKeyScope      string `json:"git_ssh_key_scope,omitempty"`
	GitSSHKeyOwner      string `json:"git_ssh_key_owner,omitempty"`
}

// CreateProjectRESTResponse is returned on successful project creation.
type CreateProjectRESTResponse struct {
	Project   *ProjectRESTResponse `json:"project"`
	Timestamp time.Time            `json:"timestamp"`
}

// GetProjectRESTResponse is returned on GET /api/v1/projects/{id}.
type GetProjectRESTResponse struct {
	Project   *ProjectRESTResponse `json:"project"`
	Timestamp time.Time            `json:"timestamp"`
}

// UpdateProjectRESTResponse is returned on successful project update.
type UpdateProjectRESTResponse struct {
	Project   *ProjectRESTResponse `json:"project"`
	Timestamp time.Time            `json:"timestamp"`
}

// DeleteProjectRESTResponse is returned on successful project deletion.
type DeleteProjectRESTResponse struct {
	ProjectID string    `json:"project_id"`
	Deleted   bool      `json:"deleted"`
	Timestamp time.Time `json:"timestamp"`
}

// ListProjectsRESTResponse is returned on GET /api/v1/projects. It carries
// the paging envelope every list endpoint answers with; see
// docs/software-factory/api.md.
type ListProjectsRESTResponse struct {
	Projects []*ProjectRESTResponse `json:"projects"`

	// Total is the number of projects the search left, before paging.
	Total int `json:"total"`

	// Limit and Offset are the applied paging values.
	Limit  int `json:"limit"`
	Offset int `json:"offset"`

	// NextOffset is the offset of the following page, absent on the last.
	NextOffset int `json:"next_offset,omitempty"`

	// Facets counts the projects by autonomy; see
	// docs/software-factory/api.md.
	Facets orchestration.Facets `json:"facets,omitempty"`

	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// Handlers
// =============================================================================

// HandleCreateProject handles POST /api/v1/projects.
func (s *Server) HandleCreateProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req CreateProjectRESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = r.URL.Query().Get("tenant_id")
	}
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant_id is required")
		return
	}
	if req.Name == "" {
		RespondBadRequest(w, r, "name is required")
		return
	}

	projectSvc := s.service.Project()
	if projectSvc == nil {
		RespondError(w, r, http.StatusInternalServerError, errors.New("project service unavailable"))
		return
	}

	resp, err := projectSvc.CreateProject(ctx, &service.CreateProjectRequest{
		TenantID:         tenantID,
		Name:             req.Name,
		Description:      req.Description,
		WorkspaceAgentID: req.WorkspaceAgentID,
		WorkspacePath:    req.WorkspacePath,
		HostAffinity:     req.HostAffinity,
		DefaultLeadAgent: req.DefaultLeadAgent,
		GitRepoURL:       req.GitRepoURL,
		GitDefaultBranch: req.GitDefaultBranch,
		AgentRoles:       req.AgentRoles,
		TokenBudget:      req.TokenBudget,
		GPUMinutes:       req.GPUMinutes,
		MaxConcurrency:   req.MaxConcurrency,
		Metadata:         req.Metadata,

		DefaultWorkflow:     req.DefaultWorkflow,
		Autonomous:          req.Autonomous,
		ContextRepos:        req.ContextRepos,
		GitAuthorName:       req.GitAuthorName,
		GitAuthorEmail:      req.GitAuthorEmail,
		GitSigningEnabled:   req.GitSigningEnabled,
		GitSigningAlgorithm: req.GitSigningAlgorithm,
		GitSigningKeyName:   req.GitSigningKeyName,
		GitSigningKeyScope:  req.GitSigningKeyScope,
		GitSigningKeyOwner:  req.GitSigningKeyOwner,
		GitSSHKeyName:       req.GitSSHKeyName,
		GitSSHKeyScope:      req.GitSSHKeyScope,
		GitSSHKeyOwner:      req.GitSSHKeyOwner,
	})
	if err != nil {
		handleProjectError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusCreated, &CreateProjectRESTResponse{
		Project:   projectToREST(resp.Project),
		Timestamp: resp.Timestamp,
	})
}

// HandleGetProject handles GET /api/v1/projects/{id}.
func (s *Server) HandleGetProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID := chi.URLParam(r, "id")
	if projectID == "" {
		RespondBadRequest(w, r, "project id is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant_id query parameter is required")
		return
	}

	projectSvc := s.service.Project()
	if projectSvc == nil {
		RespondError(w, r, http.StatusInternalServerError, errors.New("project service unavailable"))
		return
	}

	resp, err := projectSvc.GetProject(ctx, &service.GetProjectRequest{
		TenantID:  tenantID,
		ProjectID: projectID,
	})
	if err != nil {
		handleProjectError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &GetProjectRESTResponse{
		Project:   projectToREST(resp.Project),
		Timestamp: resp.Timestamp,
	})
}

// HandleUpdateProject handles PUT /api/v1/projects/{id}.
func (s *Server) HandleUpdateProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID := chi.URLParam(r, "id")
	if projectID == "" {
		RespondBadRequest(w, r, "project id is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant_id query parameter is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req UpdateProjectRESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	projectSvc := s.service.Project()
	if projectSvc == nil {
		RespondError(w, r, http.StatusInternalServerError, errors.New("project service unavailable"))
		return
	}

	resp, err := projectSvc.UpdateProject(ctx, &service.UpdateProjectRequest{
		TenantID:         tenantID,
		ProjectID:        projectID,
		Name:             req.Name,
		Description:      req.Description,
		ClearDescription: req.ClearDescription,
		WorkspaceAgentID: req.WorkspaceAgentID,
		WorkspacePath:    req.WorkspacePath,
		HostAffinity:     req.HostAffinity,
		DefaultLeadAgent: req.DefaultLeadAgent,
		GitRepoURL:       req.GitRepoURL,
		GitDefaultBranch: req.GitDefaultBranch,
		GitCurrentSHA:    req.GitCurrentSHA,
		AgentRoles:       req.AgentRoles,
		TokenBudget:      req.TokenBudget,
		GPUMinutes:       req.GPUMinutes,
		MaxConcurrency:   req.MaxConcurrency,
		Metadata:         req.Metadata,

		DefaultWorkflow:      req.DefaultWorkflow,
		ClearDefaultWorkflow: req.ClearDefaultWorkflow,
		Autonomous:           req.Autonomous,
		ContextRepos:         req.ContextRepos,
		ClearContextRepos:    req.ClearContextRepos,
		GitAuthorName:        req.GitAuthorName,
		GitAuthorEmail:       req.GitAuthorEmail,
		GitSigningEnabled:    req.GitSigningEnabled,
		GitSigningAlgorithm:  req.GitSigningAlgorithm,
		GitSigningKeyName:    req.GitSigningKeyName,
		GitSigningKeyScope:   req.GitSigningKeyScope,
		GitSigningKeyOwner:   req.GitSigningKeyOwner,
		GitSSHKeyName:        req.GitSSHKeyName,
		GitSSHKeyScope:       req.GitSSHKeyScope,
		GitSSHKeyOwner:       req.GitSSHKeyOwner,
	})
	if err != nil {
		handleProjectError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &UpdateProjectRESTResponse{
		Project:   projectToREST(resp.Project),
		Timestamp: resp.Timestamp,
	})
}

// HandleDeleteProject handles DELETE /api/v1/projects/{id}.
func (s *Server) HandleDeleteProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID := chi.URLParam(r, "id")
	if projectID == "" {
		RespondBadRequest(w, r, "project id is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant_id query parameter is required")
		return
	}

	projectSvc := s.service.Project()
	if projectSvc == nil {
		RespondError(w, r, http.StatusInternalServerError, errors.New("project service unavailable"))
		return
	}

	resp, err := projectSvc.DeleteProject(ctx, &service.DeleteProjectRequest{
		TenantID:  tenantID,
		ProjectID: projectID,
	})
	if err != nil {
		handleProjectError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &DeleteProjectRESTResponse{
		ProjectID: resp.ProjectID,
		Deleted:   resp.Deleted,
		Timestamp: resp.Timestamp,
	})
}

// HandleListProjects handles GET /api/v1/projects.
func (s *Server) HandleListProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant_id query parameter is required")
		return
	}

	projectSvc := s.service.Project()
	if projectSvc == nil {
		RespondError(w, r, http.StatusInternalServerError, errors.New("project service unavailable"))
		return
	}

	page, err := pageFromRequest(r)
	if err != nil {
		RespondBadRequest(w, r, err.Error())
		return
	}

	autonomous, err := orchestration.ParseOptionalBool("autonomous", r.URL.Query().Get("autonomous"))
	if err != nil {
		RespondBadRequest(w, r, err.Error())
		return
	}

	resp, err := projectSvc.ListProjects(ctx, &service.ListProjectsRequest{
		TenantID:   tenantID,
		Autonomous: autonomous,
		Page:       page,
	})
	if err != nil {
		handleProjectError(w, r, err)
		return
	}

	projects := make([]*ProjectRESTResponse, 0, len(resp.Projects))
	for _, p := range resp.Projects {
		projects = append(projects, projectToREST(p))
	}

	RespondJSON(w, r, http.StatusOK, &ListProjectsRESTResponse{
		Projects:   projects,
		Total:      resp.Total,
		Limit:      resp.Limit,
		Offset:     resp.Offset,
		NextOffset: resp.NextOffset,
		Facets:     resp.Facets,
		Timestamp:  resp.Timestamp,
	})
}

// =============================================================================
// Helpers
// =============================================================================

// projectToREST converts an orchestration.Project to its REST representation.
func projectToREST(p *orchestration.Project) *ProjectRESTResponse {
	if p == nil {
		return nil
	}
	resp := &ProjectRESTResponse{
		ID:               p.ID,
		TenantID:         p.TenantID,
		Name:             p.Name,
		Description:      p.Description,
		WorkspaceAgentID: p.Workspace.AgentID,
		WorkspacePath:    p.Workspace.Path,
		HostAffinity:     p.HostAffinity,
		DefaultLeadAgent: p.DefaultLeadAgent,
		GitRepoURL:       p.Git.RepoURL,
		GitDefaultBranch: p.Git.DefaultBranch,
		GitCurrentSHA:    p.Git.CurrentSHA,
		TokenBudget:      p.Quotas.TokenBudget,
		GPUMinutes:       p.Quotas.GPUMinutes,
		MaxConcurrency:   p.Quotas.MaxConcurrency,
		Metadata:         p.Metadata,
		CreatedAt:        p.CreatedAt,
		UpdatedAt:        p.UpdatedAt,

		DefaultWorkflow:     p.DefaultWorkflow,
		Autonomous:          p.Autonomous,
		ContextRepos:        p.ContextRepos,
		GitAuthorName:       p.Git.Author.Name,
		GitAuthorEmail:      p.Git.Author.Email,
		GitSigningEnabled:   p.Git.Signing.Enabled,
		GitSigningAlgorithm: p.Git.Signing.Algorithm,
		GitSigningKeyName:   p.Git.Signing.Key.Name,
		GitSigningKeyScope:  p.Git.Signing.Key.Scope,
		GitSigningKeyOwner:  p.Git.Signing.Key.Owner,
		GitSSHKeyName:       p.Git.SSHKey.Name,
		GitSSHKeyScope:      p.Git.SSHKey.Scope,
		GitSSHKeyOwner:      p.Git.SSHKey.Owner,
	}
	if p.Permissions.AgentRoles != nil {
		resp.AgentRoles = p.Permissions.AgentRoles
	}
	return resp
}

// =============================================================================
// Agent Role Management REST Request/Response Types
// =============================================================================

// GrantAgentRoleRESTRequest is the JSON body for
// PUT /api/v1/projects/{id}/permissions/{agent_id}.
type GrantAgentRoleRESTRequest struct {
	Role orchestration.ProjectRole `json:"role"`
}

// GrantAgentRoleRESTResponse is returned after a successful role grant.
type GrantAgentRoleRESTResponse struct {
	ProjectID string                    `json:"project_id"`
	AgentID   string                    `json:"agent_id"`
	Role      orchestration.ProjectRole `json:"role"`
	Timestamp time.Time                 `json:"timestamp"`
}

// RevokeAgentRoleRESTResponse is returned after a successful role revocation.
type RevokeAgentRoleRESTResponse struct {
	ProjectID string    `json:"project_id"`
	AgentID   string    `json:"agent_id"`
	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// Agent Role Management Handlers
// =============================================================================

// HandleGrantAgentRole handles PUT /api/v1/projects/{id}/permissions/{agent_id}.
// Sets or updates the role for an agent within a project.
func (s *Server) HandleGrantAgentRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID := chi.URLParam(r, "id")
	if projectID == "" {
		RespondBadRequest(w, r, "project id is required")
		return
	}
	agentID := chi.URLParam(r, "agent_id")
	if agentID == "" {
		RespondBadRequest(w, r, "agent_id is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant_id query parameter is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req GrantAgentRoleRESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}
	if req.Role == "" {
		RespondBadRequest(w, r, "role is required")
		return
	}

	projectSvc := s.service.Project()
	if projectSvc == nil {
		RespondError(w, r, http.StatusInternalServerError, errors.New("project service unavailable"))
		return
	}

	resp, err := projectSvc.GrantAgentRole(ctx, &service.GrantAgentRoleRequest{
		TenantID:  tenantID,
		ProjectID: projectID,
		AgentID:   agentID,
		Role:      req.Role,
	})
	if err != nil {
		handleProjectError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &GrantAgentRoleRESTResponse{
		ProjectID: resp.ProjectID,
		AgentID:   resp.AgentID,
		Role:      resp.Role,
		Timestamp: resp.Timestamp,
	})
}

// HandleRevokeAgentRole handles DELETE /api/v1/projects/{id}/permissions/{agent_id}.
// Sets the agent's role to denied, blocking all project access.
func (s *Server) HandleRevokeAgentRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID := chi.URLParam(r, "id")
	if projectID == "" {
		RespondBadRequest(w, r, "project id is required")
		return
	}
	agentID := chi.URLParam(r, "agent_id")
	if agentID == "" {
		RespondBadRequest(w, r, "agent_id is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant_id query parameter is required")
		return
	}

	projectSvc := s.service.Project()
	if projectSvc == nil {
		RespondError(w, r, http.StatusInternalServerError, errors.New("project service unavailable"))
		return
	}

	resp, err := projectSvc.RevokeAgentRole(ctx, &service.RevokeAgentRoleRequest{
		TenantID:  tenantID,
		ProjectID: projectID,
		AgentID:   agentID,
	})
	if err != nil {
		handleProjectError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &RevokeAgentRoleRESTResponse{
		ProjectID: resp.ProjectID,
		AgentID:   resp.AgentID,
		Timestamp: resp.Timestamp,
	})
}

// handleProjectError maps service-layer project errors to HTTP status codes.
func handleProjectError(w http.ResponseWriter, r *http.Request, err error) {
	var notFound *service.ProjectNotFoundError
	var alreadyExists *service.ProjectAlreadyExistsError
	var invalid *service.ProjectInvalidError
	var accessDenied *service.ProjectAccessDeniedError
	var unresolved *service.ProjectUnresolvedError
	var roleInvalid *service.ProjectRoleInvalidError

	switch {
	case isPageInputError(err):
		RespondError(w, r, http.StatusBadRequest, err)
	case errors.As(err, &notFound):
		RespondError(w, r, http.StatusNotFound, err)
	case errors.As(err, &alreadyExists):
		RespondError(w, r, http.StatusConflict, err)
	case errors.As(err, &invalid):
		RespondError(w, r, http.StatusBadRequest, err)
	case errors.As(err, &accessDenied):
		RespondError(w, r, http.StatusForbidden, err)
	case errors.As(err, &unresolved):
		// A refusal, not a server fault: the authorizer could not read the
		// project, so it could not read the rule, so it refuses.
		RespondError(w, r, http.StatusForbidden, err)
	case errors.As(err, &roleInvalid):
		RespondError(w, r, http.StatusBadRequest, err)
	default:
		RespondError(w, r, http.StatusInternalServerError, err)
	}
}
