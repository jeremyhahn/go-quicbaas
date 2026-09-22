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

package service

import (
	"context"
	stderrors "errors"
	"time"

	"github.com/google/uuid"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// ProjectService — Interface
// =============================================================================

// ProjectService provides project lifecycle management with RBAC enforcement
// and domain validation. It is the single authoritative boundary between the
// API transport layer and project storage.
type ProjectService interface {
	// CreateProject creates a new project for the given tenant.
	// Returns ProjectAlreadyExistsError if a project with the same name exists.
	// Returns ProjectInvalidError if required fields are missing.
	CreateProject(ctx context.Context, req *CreateProjectRequest) (*CreateProjectResponse, error)

	// GetProject retrieves a project by ID.
	// Returns ProjectNotFoundError if not found.
	GetProject(ctx context.Context, req *GetProjectRequest) (*GetProjectResponse, error)

	// GetProjectByName retrieves a project by name within a tenant.
	// Returns ProjectNotFoundError if not found.
	GetProjectByName(ctx context.Context, req *GetProjectByNameRequest) (*GetProjectResponse, error)

	// UpdateProject updates an existing project.
	// Returns ProjectNotFoundError if not found.
	// Returns ProjectAlreadyExistsError if the updated name conflicts.
	UpdateProject(ctx context.Context, req *UpdateProjectRequest) (*UpdateProjectResponse, error)

	// DeleteProject removes a project.
	// Returns ProjectNotFoundError if not found.
	DeleteProject(ctx context.Context, req *DeleteProjectRequest) (*DeleteProjectResponse, error)

	// ListProjects returns all projects for a given tenant.
	ListProjects(ctx context.Context, req *ListProjectsRequest) (*ListProjectsResponse, error)

	// AuthorizeAgent verifies that agentID holds at least requiredRole in projectID.
	// Returns ProjectAccessDeniedError if the agent lacks the required role.
	// Returns nil when the project has no role matrix (legacy unscoped behaviour).
	AuthorizeAgent(ctx context.Context, req *AuthorizeAgentRequest) error

	// GrantAgentRole sets or updates the role for agentID in projectID.
	// Returns ProjectNotFoundError if the project does not exist.
	GrantAgentRole(ctx context.Context, req *GrantAgentRoleRequest) (*GrantAgentRoleResponse, error)

	// RevokeAgentRole sets the agent's role to ProjectRoleDenied in projectID.
	// Returns ProjectNotFoundError if the project does not exist.
	RevokeAgentRole(ctx context.Context, req *RevokeAgentRoleRequest) (*RevokeAgentRoleResponse, error)
}

// =============================================================================
// Project Service Types
// =============================================================================

// CreateProjectRequest contains the parameters for creating a project.
type CreateProjectRequest struct {
	TenantID         string
	Name             string
	Description      string
	WorkspaceAgentID string
	WorkspacePath    string
	HostAffinity     string
	DefaultLeadAgent string
	GitRepoURL       string
	GitDefaultBranch string
	AgentRoles       map[string]orchestration.ProjectRole

	// DefaultWorkflow names the tenant-scoped workflow a Task under this
	// project auto-spawns into. Empty means never. A set name must be
	// registered in the tenant, or the create is refused.
	DefaultWorkflow string

	// Autonomous turns autonomous development on for the new project: the
	// task-source poller adopts work for it and the dispatcher starts jobs
	// for it. False leaves the project operator-driven.
	Autonomous bool

	// ContextRepos names the repositories the worker clones beside the
	// project checkout. Refused when two would occupy one directory.
	ContextRepos []orchestration.ContextRepo

	// GitAuthorName / GitAuthorEmail are the identity recorded on commits
	// made from the workspace. Both or neither.
	GitAuthorName  string
	GitAuthorEmail string

	// GitSigningEnabled turns commit signing on. An enabled policy must name
	// a key (GitSigningKeyName) and may pin an algorithm
	// (GitSigningAlgorithm: ED25519 default, ECDSA-P256 under FIPS).
	GitSigningEnabled   bool
	GitSigningAlgorithm string
	GitSigningKeyName   string
	GitSigningKeyScope  string
	GitSigningKeyOwner  string
	TokenBudget         int64
	GPUMinutes          int64
	MaxConcurrency      int
	Metadata            map[string]string

	// WorkspaceManaged requests a server-managed workspace: the server
	// provisions <projects_root>/<tenant_id>/<project_id>/workspace (0700)
	// and, when GitRepoURL is set, clones it there as a tracked background
	// provisioning step. Mutually exclusive with WorkspacePath
	// (bring-your-own-path projects are never provisioned).
	WorkspaceManaged bool

	// GitCredentialSecretRef names a secret (SecretID) in the quicbaas
	// secrets store holding an HTTPS access token used for the clone.
	// Requires WorkspaceManaged, an https:// GitRepoURL, and is mutually
	// exclusive with GitSSHKeyName. The token is resolved at clone time and
	// never persisted on the project record.
	GitCredentialSecretRef string

	// GitCredentialUsername optionally pairs a username with the HTTPS
	// token. Empty selects the provisioner default.
	GitCredentialUsername string

	// GitSSHKeyName / GitSSHKeyScope / GitSSHKeyOwner reference the
	// xkms-held SSH key an ssh:// GitRepoURL is reached with, mirroring the
	// deploy Spec's (name, scope, owner) triple. The reference (never the
	// material) is persisted on the project as Git.SSHKey; when empty, the
	// signing key doubles as the access key (Git.AccessKey). Mutually
	// exclusive with GitCredentialSecretRef. Scope defaults to "tenant" and
	// owner to the tenant ID.
	GitSSHKeyName  string
	GitSSHKeyScope string
	GitSSHKeyOwner string
}

// CreateProjectResponse is returned after a successful project creation.
type CreateProjectResponse struct {
	Project   *orchestration.Project
	Timestamp time.Time
}

// GetProjectRequest identifies a project by ID within a tenant.
type GetProjectRequest struct {
	TenantID  string
	ProjectID string
}

// GetProjectByNameRequest identifies a project by name within a tenant.
type GetProjectByNameRequest struct {
	TenantID string
	Name     string
}

// GetProjectResponse wraps the retrieved project.
type GetProjectResponse struct {
	Project   *orchestration.Project
	Timestamp time.Time
}

// UpdateProjectRequest specifies which fields to change. Empty string fields
// are left unchanged (except Description which can be cleared via ClearDescription).
type UpdateProjectRequest struct {
	TenantID         string
	ProjectID        string
	Name             string
	Description      string
	ClearDescription bool
	WorkspaceAgentID string
	WorkspacePath    string
	HostAffinity     string
	DefaultLeadAgent string
	GitRepoURL       string
	GitDefaultBranch string
	GitCurrentSHA    string
	AgentRoles       map[string]orchestration.ProjectRole
	Metadata         map[string]string

	// The quotas are POINTERS so that zero is expressible. Zero is not "no
	// value" here — ProjectQuotas defines it as UNLIMITED — so applying a
	// quota only when it was non-zero meant an operator could lift a cap,
	// watch the save succeed, and stay capped. Nil is "leave it alone",
	// which is the same presence rule GitSigningEnabled already uses.
	TokenBudget    *int64
	GPUMinutes     *int64
	MaxConcurrency *int

	// DefaultWorkflow sets the auto-spawn workflow when non-empty; the name
	// must be registered in the tenant. ClearDefaultWorkflow removes it.
	DefaultWorkflow      string
	ClearDefaultWorkflow bool

	// Autonomous updates the autonomous-development switch when non-nil, so
	// "unchanged" and "off" stay distinct.
	Autonomous *bool

	// ContextRepos replaces the whole context repository list when non-nil;
	// ClearContextRepos empties it.
	ContextRepos      []orchestration.ContextRepo
	ClearContextRepos bool

	// GitAuthorName / GitAuthorEmail update the commit identity when
	// non-empty.
	GitAuthorName  string
	GitAuthorEmail string

	// GitSigningEnabled updates the signing switch when non-nil; the other
	// signing fields update when non-empty. The resulting policy must still
	// validate (an enabled policy names a key).
	GitSigningEnabled   *bool
	GitSigningAlgorithm string
	GitSigningKeyName   string
	GitSigningKeyScope  string
	GitSigningKeyOwner  string

	// GitSSHKeyName / GitSSHKeyScope / GitSSHKeyOwner update the access key
	// reference when non-empty.
	GitSSHKeyName  string
	GitSSHKeyScope string
	GitSSHKeyOwner string
}

// UpdateProjectResponse wraps the updated project.
type UpdateProjectResponse struct {
	Project   *orchestration.Project
	Timestamp time.Time
}

// DeleteProjectRequest identifies a project to delete.
type DeleteProjectRequest struct {
	TenantID  string
	ProjectID string
}

// DeleteProjectResponse confirms deletion.
type DeleteProjectResponse struct {
	ProjectID string
	Deleted   bool
	Timestamp time.Time
}

// ListProjectsRequest specifies which tenant's projects to list.
type ListProjectsRequest struct {
	TenantID string

	// Autonomous narrows the listing to the projects with autonomous
	// development on (true) or off (false). Nil is "the caller did not ask"
	// and narrows nothing — which is why it is a pointer: a bool could not
	// tell "the supervised ones" from "no preference".
	Autonomous *bool

	// Page narrows the answer to one page of the tenant's projects and
	// orders it per orchestration.ProjectPageSpec. Nil asks for the first
	// orchestration.DefaultPageLimit, newest first.
	Page *orchestration.Page
}

// ListProjectsResponse contains one page of the matching projects.
type ListProjectsResponse struct {
	Projects []*orchestration.Project

	// Total is the number of projects the search left, BEFORE the page was
	// cut. It counted the whole collection before paging existed and still
	// does, so a consumer reading it keeps the meaning it had.
	Total int

	// Limit and Offset are the applied paging values.
	Limit  int
	Offset int

	// NextOffset is the offset of the following page, zero on the last.
	NextOffset int

	// Facets counts the projects by autonomy over the set the search left,
	// before the autonomy narrowing. Nil when no page was asked for.
	Facets orchestration.Facets

	Timestamp time.Time
}

// AuthorizeAgentRequest contains the parameters for authorizing an agent.
type AuthorizeAgentRequest struct {
	TenantID     string
	ProjectID    string
	AgentID      string
	RequiredRole orchestration.ProjectRole
}

// GrantAgentRoleRequest sets a specific role for an agent in a project.
type GrantAgentRoleRequest struct {
	TenantID  string
	ProjectID string
	AgentID   string
	Role      orchestration.ProjectRole
}

// GrantAgentRoleResponse confirms the role assignment.
type GrantAgentRoleResponse struct {
	ProjectID string
	AgentID   string
	Role      orchestration.ProjectRole
	Timestamp time.Time
}

// RevokeAgentRoleRequest denies a specific agent's access to a project.
type RevokeAgentRoleRequest struct {
	TenantID  string
	ProjectID string
	AgentID   string
}

// RevokeAgentRoleResponse confirms the revocation.
type RevokeAgentRoleResponse struct {
	ProjectID string
	AgentID   string
	Timestamp time.Time
}

// =============================================================================
// Project Service Typed Errors
// =============================================================================

// ProjectNotFoundError is returned when a project cannot be located.
type ProjectNotFoundError struct {
	TenantID  string
	ProjectID string
	Name      string
}

// Error implements the error interface.
func (e *ProjectNotFoundError) Error() string {
	if e.Name != "" {
		return "project not found: tenant=" + e.TenantID + " name=" + e.Name
	}
	return "project not found: tenant=" + e.TenantID + " id=" + e.ProjectID
}

// ProjectAlreadyExistsError is returned when a project with the same tenant+name exists.
type ProjectAlreadyExistsError struct {
	TenantID string
	Name     string
}

// Error implements the error interface.
func (e *ProjectAlreadyExistsError) Error() string {
	return "project already exists: tenant=" + e.TenantID + " name=" + e.Name
}

// ProjectInvalidError is returned when a project request fails validation.
type ProjectInvalidError struct {
	Field   string
	Message string

	// Err is the domain error that refused the field, when one exists, so
	// callers can still match it with errors.Is / errors.As.
	Err error
}

// Unwrap returns the domain error the service converted, if any.
func (e *ProjectInvalidError) Unwrap() error { return e.Err }

// Error implements the error interface.
func (e *ProjectInvalidError) Error() string {
	if e.Field != "" {
		return "project invalid: " + e.Field + ": " + e.Message
	}
	return "project invalid: " + e.Message
}

// ProjectAccessDeniedError is returned when an agent lacks the required project role.
type ProjectAccessDeniedError struct {
	ProjectID    string
	AgentID      string
	RequiredRole orchestration.ProjectRole
	ActualRole   orchestration.ProjectRole
}

// Error implements the error interface.
func (e *ProjectAccessDeniedError) Error() string {
	return "project access denied: project=" + e.ProjectID +
		" agent=" + e.AgentID +
		" required=" + string(e.RequiredRole) +
		" actual=" + string(e.ActualRole)
}

// ProjectUnresolvedError is returned when a project-scoped authorization names
// a project the store cannot produce. It is the service-layer face of
// orchestration.ProjectUnresolvedError: a refusal, never a pass.
type ProjectUnresolvedError struct {
	TenantID  string
	ProjectID string
	AgentID   string

	// Err is the orchestration error this converts, so callers can still
	// match orchestration.ErrProjectUnresolved with errors.Is.
	Err error
}

// Unwrap returns the orchestration error the service converted.
func (e *ProjectUnresolvedError) Unwrap() error { return e.Err }

// Error implements the error interface.
func (e *ProjectUnresolvedError) Error() string {
	return "project unresolved: tenant=" + e.TenantID +
		" project=" + e.ProjectID +
		" agent=" + e.AgentID
}

// ProjectRoleInvalidError is returned when a role string is not recognized.
type ProjectRoleInvalidError struct {
	Role string
}

// Error implements the error interface.
func (e *ProjectRoleInvalidError) Error() string {
	return "project role invalid: " + e.Role
}

// =============================================================================
// projectServiceImpl — concrete implementation
// =============================================================================

// WorkflowResolver answers whether a tenant has a workflow by name. The
// orchestration WorkflowLoader satisfies it; the project service consults it
// so a Project can never name a DefaultWorkflow the tenant does not have.
type WorkflowResolver interface {
	Get(tenantID, name string) (*orchestration.WorkflowDefinition, error)
}

// projectServiceImpl implements ProjectService using a ProjectStore.
type projectServiceImpl struct {
	store       orchestration.ProjectStore
	authz       *orchestration.ProjectAuthz
	provisioner *ProjectWorkspaceProvisioner
	workflows   WorkflowResolver
}

// projectStoreContext binds the request's tenant to the context the project
// store is addressed with.
//
// THE STORE THE SERVER INJECTS ROUTES BY CONTEXT. It sends every operation to
// the backend of the tenant bound to the operation's context, so a project
// written under a context that names no tenant lands in the shared store while
// every tenant-stamped reader — the orchestrator's ClaimTask project PEP above
// all — looks in that tenant's own namespace and finds nothing. The PEP then
// sees a project it cannot resolve on every scoped task, which is how the
// per-project role matrix stopped being enforced at all (go-quicbaas#634). A
// record whose owning tenant the request states is therefore addressed under
// that tenant.
//
// A context that already names a tenant is left exactly as it is. Which tenant
// a caller may act as is an admission decision made above this layer, and this
// helper must never be the thing that redirects a write into a tenant the
// caller was not admitted to.
func projectStoreContext(ctx context.Context, tenantID string) context.Context {
	if tenantID == "" || orchestration.TenantFromContext(ctx) != "" {
		return ctx
	}
	return orchestration.ContextWithTenant(ctx, tenantID)
}

// NewProjectService creates a new ProjectService backed by the given store.
func NewProjectService(store orchestration.ProjectStore) ProjectService {
	return &projectServiceImpl{
		store: store,
		authz: orchestration.NewProjectAuthz(store),
	}
}

// Store returns the underlying ProjectStore. Used by internal callers that
// need to share the store with other components (e.g. orchestrator ProjectAuthz).
func (s *projectServiceImpl) Store() orchestration.ProjectStore {
	return s.store
}

// SetWorkspaceProvisioner wires the managed-workspace provisioner. The server
// binds it after construction, once the projects root, secrets service and
// xkms SSH client it composes are all available — the same post-construction
// rebind pattern SetAgentRegistry uses. Without it, requests that ask for a
// managed workspace fail with ProjectProvisioningUnavailableError.
func (s *projectServiceImpl) SetWorkspaceProvisioner(p *ProjectWorkspaceProvisioner) {
	s.provisioner = p
}

// WorkspaceProvisioner returns the wired provisioner, or nil.
func (s *projectServiceImpl) WorkspaceProvisioner() *ProjectWorkspaceProvisioner {
	return s.provisioner
}

// SetWorkflowResolver wires the tenant-scoped workflow registry a
// DefaultWorkflow is verified against. The server binds it after the
// orchestration workflow loader is built and rehydrated. Without it, a
// request that names a DefaultWorkflow is refused rather than stored
// unverified.
func (s *projectServiceImpl) SetWorkflowResolver(r WorkflowResolver) {
	s.workflows = r
}

// validateDefaultWorkflow refuses a DefaultWorkflow the tenant does not have.
func (s *projectServiceImpl) validateDefaultWorkflow(tenantID, name string) error {
	if name == "" {
		return nil
	}
	if s.workflows == nil {
		return &ProjectInvalidError{
			Field:   "default_workflow",
			Message: "no workflow registry is attached; a default workflow cannot be verified",
		}
	}
	if _, err := s.workflows.Get(tenantID, name); err != nil {
		return &ProjectInvalidError{
			Field:   "default_workflow",
			Message: "unknown workflow " + name + " in tenant " + tenantID,
		}
	}
	return nil
}

// validateContextRepos refuses a context repository the worker could not
// clone beside the project, carrying the domain error rather than restating
// its message.
func (s *projectServiceImpl) validateContextRepos(repos []orchestration.ContextRepo) error {
	if err := orchestration.ValidateContextRepos(repos); err != nil {
		return &ProjectInvalidError{
			Field:   "context_repos",
			Message: err.Error(),
			Err:     err,
		}
	}
	return nil
}

// validateGit refuses workspace git metadata the provisioner could not honour.
func (s *projectServiceImpl) validateGit(git orchestration.ProjectGit) error {
	if err := git.Validate(); err != nil {
		return s.convertStoreError(err)
	}
	return nil
}

// CreateProject validates and persists a new project. A managed-workspace
// request additionally provisions the workspace directory and, when a git
// repository URL is present, starts the tracked background clone; the call
// returns immediately with the project's provisioning state (pending) rather
// than blocking on the clone.
func (s *projectServiceImpl) CreateProject(ctx context.Context, req *CreateProjectRequest) (*CreateProjectResponse, error) {
	if req == nil {
		return nil, &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.Name == "" {
		return nil, &ProjectInvalidError{Field: "name", Message: "project name is required"}
	}
	ctx = projectStoreContext(ctx, req.TenantID)

	if err := s.validateWorkspaceRequest(req); err != nil {
		return nil, err
	}
	if err := s.validateDefaultWorkflow(req.TenantID, req.DefaultWorkflow); err != nil {
		return nil, err
	}
	if err := s.validateContextRepos(req.ContextRepos); err != nil {
		return nil, err
	}

	git := orchestration.ProjectGit{
		RepoURL:       req.GitRepoURL,
		DefaultBranch: req.GitDefaultBranch,
		Author:        orchestration.GitAuthor{Name: req.GitAuthorName, Email: req.GitAuthorEmail},
		Signing: orchestration.CommitSigning{
			Enabled:   req.GitSigningEnabled,
			Algorithm: req.GitSigningAlgorithm,
			Key:       orchestration.KMSKeyRef{Name: req.GitSigningKeyName, Scope: req.GitSigningKeyScope, Owner: req.GitSigningKeyOwner},
		},
		SSHKey: orchestration.KMSKeyRef{Name: req.GitSSHKeyName, Scope: req.GitSSHKeyScope, Owner: req.GitSSHKeyOwner},
	}
	if err := s.validateGit(git); err != nil {
		return nil, err
	}

	project := &orchestration.Project{
		ID:               uuid.New().String(),
		TenantID:         req.TenantID,
		Name:             req.Name,
		Description:      req.Description,
		HostAffinity:     req.HostAffinity,
		DefaultLeadAgent: req.DefaultLeadAgent,
		DefaultWorkflow:  req.DefaultWorkflow,
		Autonomous:       req.Autonomous,
		ContextRepos:     req.ContextRepos,
		Workspace: orchestration.ProjectWorkspace{
			AgentID: req.WorkspaceAgentID,
			Path:    req.WorkspacePath,
		},
		Git: git,
		Quotas: orchestration.ProjectQuotas{
			TokenBudget:    req.TokenBudget,
			GPUMinutes:     req.GPUMinutes,
			MaxConcurrency: req.MaxConcurrency,
		},
		Metadata:  req.Metadata,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if req.AgentRoles != nil {
		project.Permissions = orchestration.ProjectPermissions{AgentRoles: req.AgentRoles}
	}

	if req.WorkspaceManaged {
		workspace, provErr := s.provisioner.Provision(req.TenantID, project.ID)
		if provErr != nil {
			return nil, provErr
		}
		project.Workspace.Path = workspace
		project.Provisioning = orchestration.ProjectProvisioning{
			Managed:   true,
			State:     orchestration.ProvisioningStateReady,
			UpdatedAt: time.Now().UTC(),
		}
		if req.GitRepoURL != "" {
			project.Provisioning.State = orchestration.ProvisioningStatePending
		}
	}

	if err := s.store.Create(ctx, project); err != nil {
		if req.WorkspaceManaged {
			// The record never existed, so the directory must not either.
			// A failed teardown leaves an orphan a retried create reuses
			// (Provision is idempotent); the create error is what matters.
			if tdErr := s.provisioner.Teardown(req.TenantID, project.ID); tdErr != nil {
				return nil, s.convertStoreError(err)
			}
		}
		return nil, s.convertStoreError(err)
	}

	if req.WorkspaceManaged && req.GitRepoURL != "" {
		spec := &ProjectGitSpec{
			RepoURL:        req.GitRepoURL,
			Branch:         req.GitDefaultBranch,
			TokenSecretRef: req.GitCredentialSecretRef,
			TokenUsername:  req.GitCredentialUsername,
			SSHKey:         sshKeyRefFrom(project.Git.AccessKey()),
		}
		if cloneErr := s.provisioner.StartClone(ctx, project, spec); cloneErr != nil {
			return nil, cloneErr
		}
	}

	return &CreateProjectResponse{
		Project:   project,
		Timestamp: time.Now().UTC(),
	}, nil
}

// validateWorkspaceRequest enforces the managed-workspace and git-credential
// request invariants before any side effects.
func (s *projectServiceImpl) validateWorkspaceRequest(req *CreateProjectRequest) error {
	hasToken := req.GitCredentialSecretRef != ""
	hasSSHKey := req.GitSSHKeyName != ""

	if req.WorkspaceManaged && req.WorkspacePath != "" {
		return &ProjectInvalidError{
			Field:   "workspace_path",
			Message: "workspace path and managed workspace are mutually exclusive",
		}
	}
	if hasToken && hasSSHKey {
		return &ProjectInvalidError{
			Field:   "git_credential_secret_ref",
			Message: "git credential secret ref and git ssh key ref are mutually exclusive",
		}
	}
	if (hasToken || hasSSHKey) && req.GitRepoURL == "" {
		return &ProjectInvalidError{
			Field:   "git_repo_url",
			Message: "git credentials require a git repository URL",
		}
	}
	if hasToken && !req.WorkspaceManaged {
		return &ProjectInvalidError{
			Field:   "workspace_managed",
			Message: "a git credential secret is resolved only for a managed workspace clone",
		}
	}
	if req.WorkspaceManaged {
		if s.provisioner == nil {
			return &ProjectProvisioningUnavailableError{Reason: "workspace provisioning is not wired on this server"}
		}
		if req.GitRepoURL != "" && !s.provisioner.CanClone() {
			return &ProjectProvisioningUnavailableError{Reason: "git provisioning is not wired on this server"}
		}
	}
	return nil
}

// GetProject retrieves a project by ID.
func (s *projectServiceImpl) GetProject(ctx context.Context, req *GetProjectRequest) (*GetProjectResponse, error) {
	if req == nil {
		return nil, &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.ProjectID == "" {
		return nil, &ProjectInvalidError{Field: "project_id", Message: "project ID is required"}
	}

	project, err := s.store.Get(projectStoreContext(ctx, req.TenantID), req.TenantID, req.ProjectID)
	if err != nil {
		return nil, s.convertStoreError(err)
	}

	return &GetProjectResponse{
		Project:   project,
		Timestamp: time.Now().UTC(),
	}, nil
}

// GetProjectByName retrieves a project by name within a tenant.
func (s *projectServiceImpl) GetProjectByName(ctx context.Context, req *GetProjectByNameRequest) (*GetProjectResponse, error) {
	if req == nil {
		return nil, &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.Name == "" {
		return nil, &ProjectInvalidError{Field: "name", Message: "project name is required"}
	}

	project, err := s.store.GetByName(projectStoreContext(ctx, req.TenantID), req.TenantID, req.Name)
	if err != nil {
		return nil, s.convertStoreError(err)
	}

	return &GetProjectResponse{
		Project:   project,
		Timestamp: time.Now().UTC(),
	}, nil
}

// UpdateProject applies field-level updates to an existing project.
func (s *projectServiceImpl) UpdateProject(ctx context.Context, req *UpdateProjectRequest) (*UpdateProjectResponse, error) {
	if req == nil {
		return nil, &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.ProjectID == "" {
		return nil, &ProjectInvalidError{Field: "project_id", Message: "project ID is required"}
	}

	ctx = projectStoreContext(ctx, req.TenantID)

	// Load the existing project.
	existing, err := s.store.Get(ctx, req.TenantID, req.ProjectID)
	if err != nil {
		return nil, s.convertStoreError(err)
	}

	// Apply field-level updates.
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.ClearDescription {
		existing.Description = ""
	} else if req.Description != "" {
		existing.Description = req.Description
	}
	if req.WorkspaceAgentID != "" {
		existing.Workspace.AgentID = req.WorkspaceAgentID
	}
	if req.WorkspacePath != "" {
		existing.Workspace.Path = req.WorkspacePath
	}
	if req.HostAffinity != "" {
		existing.HostAffinity = req.HostAffinity
	}
	if req.DefaultLeadAgent != "" {
		existing.DefaultLeadAgent = req.DefaultLeadAgent
	}
	if req.GitRepoURL != "" {
		existing.Git.RepoURL = req.GitRepoURL
	}
	if req.GitDefaultBranch != "" {
		existing.Git.DefaultBranch = req.GitDefaultBranch
	}
	if req.GitCurrentSHA != "" {
		existing.Git.CurrentSHA = req.GitCurrentSHA
	}
	if req.GitAuthorName != "" {
		existing.Git.Author.Name = req.GitAuthorName
	}
	if req.GitAuthorEmail != "" {
		existing.Git.Author.Email = req.GitAuthorEmail
	}
	if req.GitSigningEnabled != nil {
		existing.Git.Signing.Enabled = *req.GitSigningEnabled
	}
	if req.GitSigningAlgorithm != "" {
		existing.Git.Signing.Algorithm = req.GitSigningAlgorithm
	}
	if req.GitSigningKeyName != "" {
		existing.Git.Signing.Key.Name = req.GitSigningKeyName
	}
	if req.GitSigningKeyScope != "" {
		existing.Git.Signing.Key.Scope = req.GitSigningKeyScope
	}
	if req.GitSigningKeyOwner != "" {
		existing.Git.Signing.Key.Owner = req.GitSigningKeyOwner
	}
	if req.GitSSHKeyName != "" {
		existing.Git.SSHKey.Name = req.GitSSHKeyName
	}
	if req.GitSSHKeyScope != "" {
		existing.Git.SSHKey.Scope = req.GitSSHKeyScope
	}
	if req.GitSSHKeyOwner != "" {
		existing.Git.SSHKey.Owner = req.GitSSHKeyOwner
	}
	if err := s.validateGit(existing.Git); err != nil {
		return nil, err
	}
	if req.ClearDefaultWorkflow {
		existing.DefaultWorkflow = ""
	} else if req.DefaultWorkflow != "" {
		if err := s.validateDefaultWorkflow(req.TenantID, req.DefaultWorkflow); err != nil {
			return nil, err
		}
		existing.DefaultWorkflow = req.DefaultWorkflow
	}
	if req.Autonomous != nil {
		existing.Autonomous = *req.Autonomous
	}
	if req.ClearContextRepos {
		existing.ContextRepos = nil
	} else if req.ContextRepos != nil {
		if err := s.validateContextRepos(req.ContextRepos); err != nil {
			return nil, err
		}
		existing.ContextRepos = req.ContextRepos
	}
	if req.AgentRoles != nil {
		existing.Permissions.AgentRoles = req.AgentRoles
	}
	if req.TokenBudget != nil {
		existing.Quotas.TokenBudget = *req.TokenBudget
	}
	if req.GPUMinutes != nil {
		existing.Quotas.GPUMinutes = *req.GPUMinutes
	}
	if req.MaxConcurrency != nil {
		existing.Quotas.MaxConcurrency = *req.MaxConcurrency
	}
	if req.Metadata != nil {
		existing.Metadata = req.Metadata
	}

	if err := s.store.Update(ctx, existing); err != nil {
		return nil, s.convertStoreError(err)
	}

	return &UpdateProjectResponse{
		Project:   existing,
		Timestamp: time.Now().UTC(),
	}, nil
}

// DeleteProject removes a project. A managed workspace is torn down before
// the record is deleted, so a teardown failure leaves the project intact and
// the delete retryable rather than leaking an unowned directory tree.
// Bring-your-own-path workspaces are never touched.
func (s *projectServiceImpl) DeleteProject(ctx context.Context, req *DeleteProjectRequest) (*DeleteProjectResponse, error) {
	if req == nil {
		return nil, &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.ProjectID == "" {
		return nil, &ProjectInvalidError{Field: "project_id", Message: "project ID is required"}
	}

	ctx = projectStoreContext(ctx, req.TenantID)

	project, err := s.store.Get(ctx, req.TenantID, req.ProjectID)
	if err != nil {
		return nil, s.convertStoreError(err)
	}

	if project.Provisioning.Managed {
		if s.provisioner == nil {
			return nil, &ProjectProvisioningUnavailableError{
				Reason: "cannot tear down managed workspace: workspace provisioning is not wired on this server",
			}
		}
		if tdErr := s.provisioner.Teardown(req.TenantID, req.ProjectID); tdErr != nil {
			return nil, tdErr
		}
	}

	if err := s.store.Delete(ctx, req.TenantID, req.ProjectID); err != nil {
		return nil, s.convertStoreError(err)
	}

	return &DeleteProjectResponse{
		ProjectID: req.ProjectID,
		Deleted:   true,
		Timestamp: time.Now().UTC(),
	}, nil
}

// ListProjects returns all projects for a given tenant.
func (s *projectServiceImpl) ListProjects(ctx context.Context, req *ListProjectsRequest) (*ListProjectsResponse, error) {
	if req == nil {
		return nil, &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}

	projects, err := s.store.ListByTenant(projectStoreContext(ctx, req.TenantID), req.TenantID)
	if err != nil {
		return nil, s.convertStoreError(err)
	}

	page, err := orchestration.ApplyFacetedPage(
		projects, req.Page, orchestration.ProjectPageSpec(), orchestration.ProjectFacetSpec(),
		func(project *orchestration.Project) bool {
			return orchestration.MatchesProjectAutonomy(project, req.Autonomous)
		})
	if err != nil {
		return nil, err
	}

	return &ListProjectsResponse{
		Projects:   page.Items,
		Total:      page.Total,
		Limit:      page.Limit,
		Offset:     page.Offset,
		NextOffset: page.NextOffset,
		Facets:     page.Facets,
		Timestamp:  time.Now().UTC(),
	}, nil
}

// convertStoreError translates orchestration store errors into service-layer errors.
func (s *projectServiceImpl) convertStoreError(err error) error {
	var (
		storeNotFound *orchestration.ProjectStoreNotFoundError
		notFound      *orchestration.ProjectNotFoundError
		alreadyExists *orchestration.ProjectAlreadyExistsError
		invalid       *orchestration.ProjectInvalidError
	)
	switch {
	case stderrors.As(err, &storeNotFound):
		return &ProjectNotFoundError{
			TenantID:  storeNotFound.TenantID,
			ProjectID: storeNotFound.ProjectID,
			Name:      storeNotFound.Name,
		}
	case stderrors.As(err, &notFound):
		// Legacy agent-domain not-found error: only has ProjectID.
		return &ProjectNotFoundError{
			ProjectID: notFound.ProjectID,
		}
	case stderrors.As(err, &alreadyExists):
		return &ProjectAlreadyExistsError{
			TenantID: alreadyExists.TenantID,
			Name:     alreadyExists.Name,
		}
	case stderrors.As(err, &invalid):
		return &ProjectInvalidError{
			Field:   invalid.Field,
			Message: invalid.Message,
		}
	default:
		return err
	}
}

// AuthorizeAgent verifies that agentID holds at least requiredRole in projectID.
func (s *projectServiceImpl) AuthorizeAgent(ctx context.Context, req *AuthorizeAgentRequest) error {
	if req == nil {
		return &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.AgentID == "" {
		return &ProjectInvalidError{Field: "agent_id", Message: "agent ID is required"}
	}
	if req.ProjectID == "" {
		return nil // unscoped — legacy behaviour
	}

	err := s.authz.AuthorizeAgentForTenant(
		projectStoreContext(ctx, req.TenantID), req.TenantID, req.ProjectID, req.AgentID, req.RequiredRole)
	if err != nil {
		var (
			denied      *orchestration.ProjectAccessDeniedError
			roleInvalid *orchestration.ProjectRoleInvalidError
			unresolved  *orchestration.ProjectUnresolvedError
		)
		switch {
		case stderrors.As(err, &denied):
			return &ProjectAccessDeniedError{
				ProjectID:    denied.ProjectID,
				AgentID:      denied.AgentID,
				RequiredRole: denied.RequiredRole,
				ActualRole:   denied.ActualRole,
			}
		case stderrors.As(err, &unresolved):
			return &ProjectUnresolvedError{
				TenantID:  unresolved.TenantID,
				ProjectID: unresolved.ProjectID,
				AgentID:   unresolved.AgentID,
				Err:       err,
			}
		case stderrors.As(err, &roleInvalid):
			return &ProjectRoleInvalidError{Role: roleInvalid.Role}
		default:
			return err
		}
	}
	return nil
}

// GrantAgentRole sets or updates a specific role for an agent in a project.
func (s *projectServiceImpl) GrantAgentRole(ctx context.Context, req *GrantAgentRoleRequest) (*GrantAgentRoleResponse, error) {
	if req == nil {
		return nil, &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.ProjectID == "" {
		return nil, &ProjectInvalidError{Field: "project_id", Message: "project ID is required"}
	}
	if req.AgentID == "" {
		return nil, &ProjectInvalidError{Field: "agent_id", Message: "agent ID is required"}
	}

	ctx = projectStoreContext(ctx, req.TenantID)

	project, err := s.store.Get(ctx, req.TenantID, req.ProjectID)
	if err != nil {
		return nil, s.convertStoreError(err)
	}

	if project.Permissions.AgentRoles == nil {
		project.Permissions.AgentRoles = make(map[string]orchestration.ProjectRole)
	}
	project.Permissions.AgentRoles[req.AgentID] = req.Role
	project.UpdatedAt = time.Now().UTC()

	if err := s.store.Update(ctx, project); err != nil {
		return nil, s.convertStoreError(err)
	}

	return &GrantAgentRoleResponse{
		ProjectID: req.ProjectID,
		AgentID:   req.AgentID,
		Role:      req.Role,
		Timestamp: time.Now().UTC(),
	}, nil
}

// RevokeAgentRole sets the agent's role to ProjectRoleDenied in projectID.
func (s *projectServiceImpl) RevokeAgentRole(ctx context.Context, req *RevokeAgentRoleRequest) (*RevokeAgentRoleResponse, error) {
	if req == nil {
		return nil, &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.ProjectID == "" {
		return nil, &ProjectInvalidError{Field: "project_id", Message: "project ID is required"}
	}
	if req.AgentID == "" {
		return nil, &ProjectInvalidError{Field: "agent_id", Message: "agent ID is required"}
	}

	_, err := s.GrantAgentRole(ctx, &GrantAgentRoleRequest{
		TenantID:  req.TenantID,
		ProjectID: req.ProjectID,
		AgentID:   req.AgentID,
		Role:      orchestration.ProjectRoleDenied,
	})
	if err != nil {
		return nil, err
	}

	return &RevokeAgentRoleResponse{
		ProjectID: req.ProjectID,
		AgentID:   req.AgentID,
		Timestamp: time.Now().UTC(),
	}, nil
}

// Compile-time interface satisfaction check.
var _ ProjectService = (*projectServiceImpl)(nil)
