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
	TenantID             string                               `json:"tenant_id"`
	Name                 string                               `json:"name"`
	Description          string                               `json:"description,omitempty"`
	WorkspaceFleetNodeID string                               `json:"workspace_agent_id,omitempty"`
	WorkspacePath        string                               `json:"workspace_path,omitempty"`
	HostAffinity         string                               `json:"host_affinity,omitempty"`
	DefaultLeadAgent     orchestration.AgentRef               `json:"default_lead_agent,omitzero"`
	GitRepoURL           string                               `json:"git_repo_url,omitempty"`
	GitDefaultBranch     string                               `json:"git_default_branch,omitempty"`
	FleetNodeRoles       map[string]orchestration.ProjectRole `json:"agent_roles,omitempty"`
	Spend                orchestration.SpendLimits            `json:"spend,omitzero"`
	GPUMinutes           int64                                `json:"gpu_minutes,omitempty"`
	MaxConcurrency       int                                  `json:"max_concurrency,omitempty"`
	Metadata             map[string]string                    `json:"metadata,omitempty"`

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

	// Storage tier of the sandboxed checkout; see
	// docs/software-factory/storage-tiers.md. Flat, like every other field.
	WorkspaceTier     orchestration.WorkspaceTier `json:"workspace_tier,omitempty"`
	WorkspaceTmpfsMB  int                         `json:"workspace_tmpfs_mb,omitempty"`
	ContainerMemoryMB int                         `json:"container_memory_mb,omitempty"`

	// SessionRotation is what a persistent worker session does when the
	// pooled provider account under it rotates: "resume" or "fresh".
	SessionRotation orchestration.SessionRotationPolicy `json:"session_rotation,omitempty"`

	// WorkspaceManaged asks the server to provision the workspace and clone
	// git_repo_url into it; exclusive with workspace_path.
	// GitCredentialSecretRef names a tenant secret holding an HTTPS token for
	// that clone, optionally paired with GitCredentialUsername. The reference
	// is never stored on the project.
	WorkspaceManaged       bool   `json:"workspace_managed,omitempty"`
	GitCredentialSecretRef string `json:"git_credential_secret_ref,omitempty"`
	GitCredentialUsername  string `json:"git_credential_username,omitempty"`
}

// UpdateProjectRESTRequest is the JSON body for PUT /api/v1/projects/{id}.
type UpdateProjectRESTRequest struct {
	Name                 string                               `json:"name,omitempty"`
	Description          string                               `json:"description,omitempty"`
	ClearDescription     bool                                 `json:"clear_description,omitempty"`
	WorkspaceFleetNodeID string                               `json:"workspace_agent_id,omitempty"`
	WorkspacePath        string                               `json:"workspace_path,omitempty"`
	HostAffinity         string                               `json:"host_affinity,omitempty"`
	DefaultLeadAgent     orchestration.AgentRef               `json:"default_lead_agent,omitzero"`
	GitRepoURL           string                               `json:"git_repo_url,omitempty"`
	GitDefaultBranch     string                               `json:"git_default_branch,omitempty"`
	GitCurrentSHA        string                               `json:"git_current_sha,omitempty"`
	FleetNodeRoles       map[string]orchestration.ProjectRole `json:"agent_roles,omitempty"`
	Metadata             map[string]string                    `json:"metadata,omitempty"`

	// The quotas are POINTERS so zero is expressible: zero means UNLIMITED
	// on ProjectQuotas, so treating it as "no value" made lifting a cap
	// impossible — the save succeeded and the old cap stayed. Absent leaves
	// the stored value alone, the same rule GitSigningEnabled uses.
	// Spend replaces the project's spend limits when present; an empty
	// object clears them.
	Spend          *orchestration.SpendLimits `json:"spend,omitempty"`
	GPUMinutes     *int64                     `json:"gpu_minutes,omitempty"`
	MaxConcurrency *int                       `json:"max_concurrency,omitempty"`

	// DefaultWorkflow sets the auto-spawn workflow when non-empty;
	// ClearDefaultWorkflow removes it.
	DefaultWorkflow      string `json:"default_workflow,omitempty"`
	ClearDefaultWorkflow bool   `json:"clear_default_workflow,omitempty"`

	// Autonomous is a pointer so "unchanged" and "off" are distinct.
	Autonomous *bool `json:"autonomous,omitempty"`

	// Parked parks (true) or resumes (false) the project; absent leaves it
	// unchanged. A pointer so "unchanged" and "resume" are distinct.
	Parked *bool `json:"parked,omitempty"`

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

	// GitSigningRetireKey* name a KMS-held key to record in the project's
	// trusted signing history, retired at GitSigningRetiredAt (RFC 3339;
	// empty is now). See docs/software-factory/projects.md#rotating-the-signing-key.
	GitSigningRetireKeyName  string `json:"git_signing_retire_key_name,omitempty"`
	GitSigningRetireKeyScope string `json:"git_signing_retire_key_scope,omitempty"`
	GitSigningRetireKeyOwner string `json:"git_signing_retire_key_owner,omitempty"`
	GitSigningRetiredAt      string `json:"git_signing_retired_at,omitempty"`

	// Storage tier of the sandboxed checkout. WorkspaceTier updates when
	// non-empty; the sizes are POINTERS for the reason the quotas are —
	// zero means unsized / the runtime's default, so absent leaves the
	// stored value alone and zero lifts the limit.
	WorkspaceTier     orchestration.WorkspaceTier `json:"workspace_tier,omitempty"`
	WorkspaceTmpfsMB  *int                        `json:"workspace_tmpfs_mb,omitempty"`
	ContainerMemoryMB *int                        `json:"container_memory_mb,omitempty"`

	// SessionRotation updates the session rotation policy when non-empty.
	SessionRotation orchestration.SessionRotationPolicy `json:"session_rotation,omitempty"`
}

// ProjectRESTResponse mirrors orchestration.Project for JSON serialization.
type ProjectRESTResponse struct {
	ID                   string                               `json:"id"`
	TenantID             string                               `json:"tenant_id"`
	Name                 string                               `json:"name"`
	Description          string                               `json:"description,omitempty"`
	WorkspaceFleetNodeID string                               `json:"workspace_agent_id,omitempty"`
	WorkspacePath        string                               `json:"workspace_path,omitempty"`
	HostAffinity         string                               `json:"host_affinity,omitempty"`
	DefaultLeadAgent     orchestration.AgentRef               `json:"default_lead_agent,omitzero"`
	GitRepoURL           string                               `json:"git_repo_url,omitempty"`
	GitDefaultBranch     string                               `json:"git_default_branch,omitempty"`
	GitCurrentSHA        string                               `json:"git_current_sha,omitempty"`
	FleetNodeRoles       map[string]orchestration.ProjectRole `json:"agent_roles,omitempty"`
	Spend                orchestration.SpendLimits            `json:"spend,omitzero"`
	GPUMinutes           int64                                `json:"gpu_minutes,omitempty"`
	MaxConcurrency       int                                  `json:"max_concurrency,omitempty"`
	Metadata             map[string]string                    `json:"metadata,omitempty"`
	CreatedAt            time.Time                            `json:"created_at"`
	UpdatedAt            time.Time                            `json:"updated_at"`

	// DefaultWorkflow is the tenant-scoped auto-spawn workflow, if any.
	DefaultWorkflow string `json:"default_workflow,omitempty"`

	// Autonomous reports whether autonomous development is on.
	Autonomous bool `json:"autonomous,omitempty"`

	// Parked reports that the project's intake is closed; ParkedAt and
	// ParkedBy say when and by whom.
	Parked   bool      `json:"parked,omitempty"`
	ParkedAt time.Time `json:"parked_at,omitzero"`
	ParkedBy string    `json:"parked_by,omitempty"`

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

	// GitSigningRetired is the project's trusted signing history: keys it
	// signed with before the current one, each trusted only for commits made
	// before its retirement.
	GitSigningRetired []orchestration.RetiredSigningKey `json:"git_signing_retired,omitempty"`

	// Storage tier of the sandboxed checkout; see
	// docs/software-factory/storage-tiers.md. Flat, like every other field.
	WorkspaceTier     orchestration.WorkspaceTier `json:"workspace_tier,omitempty"`
	WorkspaceTmpfsMB  int                         `json:"workspace_tmpfs_mb,omitempty"`
	ContainerMemoryMB int                         `json:"container_memory_mb,omitempty"`

	// SessionRotation is what a persistent worker session does when the
	// pooled provider account under it rotates: "resume" or "fresh".
	SessionRotation orchestration.SessionRotationPolicy `json:"session_rotation,omitempty"`

	// Provisioning of a server-managed workspace, flat like every other
	// field; all empty for a bring-your-own-path project.
	WorkspaceManaged      bool                                   `json:"workspace_managed,omitempty"`
	ProvisioningState     orchestration.ProjectProvisioningState `json:"provisioning_state,omitempty"`
	ProvisioningError     string                                 `json:"provisioning_error,omitempty"`
	ProvisioningUpdatedAt time.Time                              `json:"provisioning_updated_at,omitzero"`
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
		TenantID:             tenantID,
		Name:                 req.Name,
		Description:          req.Description,
		WorkspaceFleetNodeID: req.WorkspaceFleetNodeID,
		WorkspacePath:        req.WorkspacePath,
		HostAffinity:         req.HostAffinity,
		DefaultLeadAgent:     req.DefaultLeadAgent,
		GitRepoURL:           req.GitRepoURL,
		GitDefaultBranch:     req.GitDefaultBranch,
		FleetNodeRoles:       req.FleetNodeRoles,
		Spend:                req.Spend,
		GPUMinutes:           req.GPUMinutes,
		MaxConcurrency:       req.MaxConcurrency,
		Metadata:             req.Metadata,

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
		WorkspaceTier:       req.WorkspaceTier,
		WorkspaceTmpfsMB:    req.WorkspaceTmpfsMB,
		ContainerMemoryMB:   req.ContainerMemoryMB,
		SessionRotation:     req.SessionRotation,

		WorkspaceManaged:       req.WorkspaceManaged,
		GitCredentialSecretRef: req.GitCredentialSecretRef,
		GitCredentialUsername:  req.GitCredentialUsername,
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

// HandleGetProjectByName handles GET /api/v1/projects/by-name/{name}.
func (s *Server) HandleGetProjectByName(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondBadRequest(w, r, "project name is required")
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

	resp, err := projectSvc.GetProjectByName(ctx, &service.GetProjectByNameRequest{
		TenantID: tenantID,
		Name:     name,
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

	retirement, err := service.ParseGitSigningRetirement(req.GitSigningRetireKeyName,
		req.GitSigningRetireKeyScope, req.GitSigningRetireKeyOwner, req.GitSigningRetiredAt)
	if err != nil {
		handleProjectError(w, r, err)
		return
	}

	projectSvc := s.service.Project()
	if projectSvc == nil {
		RespondError(w, r, http.StatusInternalServerError, errors.New("project service unavailable"))
		return
	}

	resp, err := projectSvc.UpdateProject(ctx, &service.UpdateProjectRequest{
		TenantID:             tenantID,
		ProjectID:            projectID,
		Name:                 req.Name,
		Description:          req.Description,
		ClearDescription:     req.ClearDescription,
		WorkspaceFleetNodeID: req.WorkspaceFleetNodeID,
		WorkspacePath:        req.WorkspacePath,
		HostAffinity:         req.HostAffinity,
		DefaultLeadAgent:     req.DefaultLeadAgent,
		GitRepoURL:           req.GitRepoURL,
		GitDefaultBranch:     req.GitDefaultBranch,
		GitCurrentSHA:        req.GitCurrentSHA,
		FleetNodeRoles:       req.FleetNodeRoles,
		Spend:                req.Spend,
		GPUMinutes:           req.GPUMinutes,
		MaxConcurrency:       req.MaxConcurrency,
		Metadata:             req.Metadata,

		DefaultWorkflow:      req.DefaultWorkflow,
		ClearDefaultWorkflow: req.ClearDefaultWorkflow,
		Autonomous:           req.Autonomous,
		Parked:               req.Parked,
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
		GitSigningRetire:     retirement,
		WorkspaceTier:        req.WorkspaceTier,
		WorkspaceTmpfsMB:     req.WorkspaceTmpfsMB,
		ContainerMemoryMB:    req.ContainerMemoryMB,
		SessionRotation:      req.SessionRotation,
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
		ID:                   p.ID,
		TenantID:             p.TenantID,
		Name:                 p.Name,
		Description:          p.Description,
		WorkspaceFleetNodeID: p.Workspace.FleetNodeID,
		WorkspacePath:        p.Workspace.Path,
		HostAffinity:         p.HostAffinity,
		DefaultLeadAgent:     p.DefaultLeadAgent,
		GitRepoURL:           p.Git.RepoURL,
		GitDefaultBranch:     p.Git.DefaultBranch,
		GitCurrentSHA:        p.Git.CurrentSHA,
		Spend:                p.Spend,
		GPUMinutes:           p.Quotas.GPUMinutes,
		MaxConcurrency:       p.Quotas.MaxConcurrency,
		Metadata:             p.Metadata,
		CreatedAt:            p.CreatedAt,
		UpdatedAt:            p.UpdatedAt,

		DefaultWorkflow:     p.DefaultWorkflow,
		Autonomous:          p.Autonomous,
		Parked:              p.Parked,
		ParkedAt:            p.ParkedAt,
		ParkedBy:            p.ParkedBy,
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
		GitSigningRetired:   p.Git.Signing.Retired,
		WorkspaceTier:       p.Storage.WorkspaceTier,
		WorkspaceTmpfsMB:    p.Storage.WorkspaceTmpfsMB,
		ContainerMemoryMB:   p.Storage.ContainerMemoryMB,
		SessionRotation:     p.SessionRotation,

		WorkspaceManaged:      p.Provisioning.Managed,
		ProvisioningState:     p.Provisioning.State,
		ProvisioningError:     p.Provisioning.Error,
		ProvisioningUpdatedAt: p.Provisioning.UpdatedAt,
	}
	if p.Permissions.FleetNodeRoles != nil {
		resp.FleetNodeRoles = p.Permissions.FleetNodeRoles
	}
	return resp
}

// =============================================================================
// Agent Role Management REST Request/Response Types
// =============================================================================

// GrantFleetNodeRoleRESTRequest is the JSON body for
// PUT /api/v1/projects/{id}/permissions/{node_id}.
type GrantFleetNodeRoleRESTRequest struct {
	Role orchestration.ProjectRole `json:"role"`
}

// GrantFleetNodeRoleRESTResponse is returned after a successful role grant.
type GrantFleetNodeRoleRESTResponse struct {
	ProjectID   string                    `json:"project_id"`
	FleetNodeID string                    `json:"node_id"`
	Role        orchestration.ProjectRole `json:"role"`
	Timestamp   time.Time                 `json:"timestamp"`
}

// RevokeFleetNodeRoleRESTResponse is returned after a successful role revocation.
type RevokeFleetNodeRoleRESTResponse struct {
	ProjectID   string    `json:"project_id"`
	FleetNodeID string    `json:"node_id"`
	Timestamp   time.Time `json:"timestamp"`
}

// =============================================================================
// Agent Role Management Handlers
// =============================================================================

// HandleGrantFleetNodeRole handles PUT /api/v1/projects/{id}/permissions/{node_id}.
// Sets or updates the role of a fleet node within a project.
func (s *Server) HandleGrantFleetNodeRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID := chi.URLParam(r, "id")
	if projectID == "" {
		RespondBadRequest(w, r, "project id is required")
		return
	}
	nodeID := chi.URLParam(r, "node_id")
	if nodeID == "" {
		RespondBadRequest(w, r, "node_id is required")
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

	var req GrantFleetNodeRoleRESTRequest
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

	resp, err := projectSvc.GrantFleetNodeRole(ctx, &service.GrantFleetNodeRoleRequest{
		TenantID:    tenantID,
		ProjectID:   projectID,
		FleetNodeID: nodeID,
		Role:        req.Role,
	})
	if err != nil {
		handleProjectError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &GrantFleetNodeRoleRESTResponse{
		ProjectID:   resp.ProjectID,
		FleetNodeID: resp.FleetNodeID,
		Role:        resp.Role,
		Timestamp:   resp.Timestamp,
	})
}

// HandleRevokeFleetNodeRole handles DELETE /api/v1/projects/{id}/permissions/{node_id}.
// Sets the fleet node's role to denied, blocking all project access.
func (s *Server) HandleRevokeFleetNodeRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID := chi.URLParam(r, "id")
	if projectID == "" {
		RespondBadRequest(w, r, "project id is required")
		return
	}
	nodeID := chi.URLParam(r, "node_id")
	if nodeID == "" {
		RespondBadRequest(w, r, "node_id is required")
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

	resp, err := projectSvc.RevokeFleetNodeRole(ctx, &service.RevokeFleetNodeRoleRequest{
		TenantID:    tenantID,
		ProjectID:   projectID,
		FleetNodeID: nodeID,
	})
	if err != nil {
		handleProjectError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &RevokeFleetNodeRoleRESTResponse{
		ProjectID:   resp.ProjectID,
		FleetNodeID: resp.FleetNodeID,
		Timestamp:   resp.Timestamp,
	})
}

// handleProjectError maps service-layer project errors to HTTP status codes.
func handleProjectError(w http.ResponseWriter, r *http.Request, err error) {
	if respondReferenceError(w, r, err) {
		return
	}
	var notFound *service.ProjectNotFoundError
	var alreadyExists *service.ProjectAlreadyExistsError
	var bound *service.ProjectBoundError
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
	case errors.As(err, &bound):
		// The project still has an SDLC binding: unbind first.
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
