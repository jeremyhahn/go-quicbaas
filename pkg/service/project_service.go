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
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
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

	// AuthorizeFleetNode verifies that agentID holds at least requiredRole in projectID.
	// Returns ProjectAccessDeniedError if the agent lacks the required role.
	// Returns nil when the project has no role matrix (legacy unscoped behaviour).
	AuthorizeFleetNode(ctx context.Context, req *AuthorizeFleetNodeRequest) error

	// GrantFleetNodeRole sets or updates the role for agentID in projectID.
	// Returns ProjectNotFoundError if the project does not exist.
	GrantFleetNodeRole(ctx context.Context, req *GrantFleetNodeRoleRequest) (*GrantFleetNodeRoleResponse, error)

	// RevokeFleetNodeRole sets the agent's role to ProjectRoleDenied in projectID.
	// Returns ProjectNotFoundError if the project does not exist.
	RevokeFleetNodeRole(ctx context.Context, req *RevokeFleetNodeRoleRequest) (*RevokeFleetNodeRoleResponse, error)
}

// =============================================================================
// Project Service Types
// =============================================================================

// CreateProjectRequest contains the parameters for creating a project.
type CreateProjectRequest struct {
	TenantID             string
	Name                 string
	Description          string
	WorkspaceFleetNodeID string
	WorkspacePath        string
	HostAffinity         string
	DefaultLeadAgent     orchestration.AgentRef
	GitRepoURL           string
	GitDefaultBranch     string
	FleetNodeRoles       map[string]orchestration.ProjectRole

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
	GPUMinutes          int64
	MaxConcurrency      int
	Metadata            map[string]string

	// Spend is the project's spend limits: sliding windows in tokens and/or
	// list-price dollars. The zero value meters nothing.
	Spend orchestration.SpendLimits

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
	// exclusive with GitCredentialSecretRef. Scope defaults to "user" and
	// owner to the tenant ID.
	GitSSHKeyName  string
	GitSSHKeyScope string
	GitSSHKeyOwner string

	// WorkspaceTier / WorkspaceTmpfsMB / ContainerMemoryMB are the storage
	// tier of the project's sandboxed checkout
	// (docs/software-factory/storage-tiers.md). Every one is inert at its
	// zero value; the combination is refused unless
	// orchestration.ProjectStorage.Validate accepts it.
	WorkspaceTier     orchestration.WorkspaceTier
	WorkspaceTmpfsMB  int
	ContainerMemoryMB int

	// SessionRotation is what a persistent worker session does when the
	// pooled provider account under it rotates. Empty is the default,
	// orchestration.SessionRotationResume; an unrecognized value is refused.
	SessionRotation orchestration.SessionRotationPolicy
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
	TenantID             string
	ProjectID            string
	Name                 string
	Description          string
	ClearDescription     bool
	WorkspaceFleetNodeID string
	WorkspacePath        string
	HostAffinity         string
	DefaultLeadAgent     orchestration.AgentRef
	GitRepoURL           string
	GitDefaultBranch     string
	GitCurrentSHA        string
	FleetNodeRoles       map[string]orchestration.ProjectRole
	Metadata             map[string]string

	// The quotas are POINTERS so that zero is expressible. Zero is not "no
	// value" here — ProjectQuotas defines it as UNLIMITED — so applying a
	// quota only when it was non-zero meant an operator could lift a cap,
	// watch the save succeed, and stay capped. Nil is "leave it alone",
	// which is the same presence rule GitSigningEnabled already uses.
	GPUMinutes     *int64
	MaxConcurrency *int

	// Spend replaces the project's spend limits when non-nil; a non-nil empty
	// value clears them.
	Spend *orchestration.SpendLimits

	// DefaultWorkflow sets the auto-spawn workflow when non-empty; the name
	// must be registered in the tenant. ClearDefaultWorkflow removes it.
	DefaultWorkflow      string
	ClearDefaultWorkflow bool

	// Autonomous updates the autonomous-development switch when non-nil, so
	// "unchanged" and "off" stay distinct.
	Autonomous *bool

	// Parked parks (true) or resumes (false) the project when non-nil.
	// Parking closes the intake and lets work in flight finish; resuming
	// reopens it. Neither touches Autonomous. See
	// orchestration/project_park.go.
	Parked *bool

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

	// GitSigningRetire records a key the project signed with before its
	// current one in the project's trusted signing history, when non-nil.
	// See retireSigningKey.
	GitSigningRetire *GitSigningRetirement

	// WorkspaceTier updates the storage tier when non-empty.
	//
	// The sizes are POINTERS for the reason the quotas are: zero is a
	// meaningful value — an unsized tmpfs, the runtime's default memory
	// limit — so nil ("leave it alone") and zero ("lift the limit") are
	// different instructions. The merged storage must still validate.
	WorkspaceTier     orchestration.WorkspaceTier
	WorkspaceTmpfsMB  *int
	ContainerMemoryMB *int

	// SessionRotation updates the session rotation policy when non-empty.
	// Setting it back to the default is naming it: "resume".
	SessionRotation orchestration.SessionRotationPolicy
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

// AuthorizeFleetNodeRequest contains the parameters for authorizing an agent.
type AuthorizeFleetNodeRequest struct {
	TenantID     string
	ProjectID    string
	FleetNodeID  string
	RequiredRole orchestration.ProjectRole
}

// GrantFleetNodeRoleRequest sets a specific role for an agent in a project.
type GrantFleetNodeRoleRequest struct {
	TenantID    string
	ProjectID   string
	FleetNodeID string
	Role        orchestration.ProjectRole
}

// GrantFleetNodeRoleResponse confirms the role assignment.
type GrantFleetNodeRoleResponse struct {
	ProjectID   string
	FleetNodeID string
	Role        orchestration.ProjectRole
	Timestamp   time.Time
}

// RevokeFleetNodeRoleRequest denies a specific agent's access to a project.
type RevokeFleetNodeRoleRequest struct {
	TenantID    string
	ProjectID   string
	FleetNodeID string
}

// RevokeFleetNodeRoleResponse confirms the revocation.
type RevokeFleetNodeRoleResponse struct {
	ProjectID   string
	FleetNodeID string
	Timestamp   time.Time
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

// ProjectBoundError is returned when a delete names a project that still has
// an SDLC binding. Delete does not offboard: the binding, its webhook route
// and the forge webhook are removed by unbind, which must run first.
type ProjectBoundError struct {
	TenantID  string
	ProjectID string
}

// Error implements the error interface. The remedy comes first.
func (e *ProjectBoundError) Error() string {
	return "run unbind (quicbaas sdlc unbind) before deleting project " + e.ProjectID +
		": it has an SDLC binding: tenant=" + e.TenantID
}

// ProjectBindingCheckError is returned when a delete cannot read the
// project's SDLC binding. The delete is refused: an unreadable store does not
// prove the project is unbound.
type ProjectBindingCheckError struct {
	TenantID  string
	ProjectID string
	Cause     error
}

// Error implements the error interface.
func (e *ProjectBindingCheckError) Error() string {
	return "cannot check project " + e.ProjectID + " for an SDLC binding: tenant=" + e.TenantID +
		": " + e.Cause.Error()
}

// Unwrap returns the binding store's error.
func (e *ProjectBindingCheckError) Unwrap() error {
	return e.Cause
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
	FleetNodeID  string
	RequiredRole orchestration.ProjectRole
	ActualRole   orchestration.ProjectRole
}

// Error implements the error interface.
func (e *ProjectAccessDeniedError) Error() string {
	return "project access denied: project=" + e.ProjectID +
		" agent=" + e.FleetNodeID +
		" required=" + string(e.RequiredRole) +
		" actual=" + string(e.ActualRole)
}

// ProjectUnresolvedError is returned when a project-scoped authorization names
// a project the store cannot produce. It is the service-layer face of
// orchestration.ProjectUnresolvedError: a refusal, never a pass.
type ProjectUnresolvedError struct {
	TenantID    string
	ProjectID   string
	FleetNodeID string

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
		" agent=" + e.FleetNodeID
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

	// secrets, sshKeys and fleet are the stores a saved reference is
	// verified against: the git credential secret, the git signing and
	// access keys, and the host affinity. A save that names a reference
	// whose store is not attached is refused as unavailable, never stored
	// unverified.
	secrets SecretsService
	sshKeys SSHKeyGetter
	fleet   orchestration.FleetNodeLister
	agents  AgentRefChecker

	// bindings is read by DeleteProject: a bound project is refused.
	bindings sdlc.BindingReader
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
// rebind pattern SetFleetRegistry uses. Without it, requests that ask for a
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

// SetSecrets wires the secrets store a git credential secret reference is
// verified against.
func (s *projectServiceImpl) SetSecrets(secrets SecretsService) {
	s.secrets = secrets
}

// SetSSHKeys wires the KMS key store the git signing and access key
// references are verified against.
func (s *projectServiceImpl) SetSSHKeys(keys SSHKeyGetter) {
	s.sshKeys = keys
}

// SetFleetNodes wires the fleet registry a host affinity is verified
// against. The server binds it once the shared registry exists.
func (s *projectServiceImpl) SetFleetNodes(fleet orchestration.FleetNodeLister) {
	s.fleet = fleet
}

// AgentRefChecker checks a reference into the tenant agent catalog;
// *orchestration.AgentDirectory satisfies it.
type AgentRefChecker interface {
	CheckRef(ctx context.Context, tenantID string, ref orchestration.AgentRef, field string, role orchestration.AgentRole) error
}

// SetAgentRefChecker wires the tenant agent catalog a DefaultLeadAgent is
// verified against. Without it, a request that names a default lead is
// refused rather than stored unverified.
func (s *projectServiceImpl) SetAgentRefChecker(checker AgentRefChecker) {
	s.agents = checker
}

// SetBindingReader wires the SDLC binding store DeleteProject consults. The
// server binds it on every node whose qrdb node has a tenant store resolver,
// whether or not the SDLC module runs there, because bindings persist in the
// tenant keyspace either way. A node with no resolver has no binding store,
// so no binding can exist; without a reader a delete proceeds.
func (s *projectServiceImpl) SetBindingReader(bindings sdlc.BindingReader) {
	s.bindings = bindings
}

// refuseBound returns a *ProjectBoundError when the project has an SDLC
// binding, and the read error when the binding store cannot be read: a store
// that cannot answer does not prove the project is unbound.
func (s *projectServiceImpl) refuseBound(ctx context.Context, tenantID, projectID string) error {
	if s.bindings == nil {
		return nil
	}
	_, err := s.bindings.GetBinding(ctx, tenantID, projectID)
	if err == nil {
		return &ProjectBoundError{TenantID: tenantID, ProjectID: projectID}
	}
	if stderrors.Is(err, sdlc.ErrNotFound) {
		return nil
	}
	return &ProjectBindingCheckError{TenantID: tenantID, ProjectID: projectID, Cause: err}
}

// validateDefaultLead refuses a default lead the tenant does not define, or
// an Agent that does not declare the lead role.
func (s *projectServiceImpl) validateDefaultLead(ctx context.Context, tenantID string, ref orchestration.AgentRef) error {
	if ref.IsZero() {
		return nil
	}
	if s.agents == nil {
		return &orchestration.AgentCatalogUnavailableError{Ref: ref}
	}
	return s.agents.CheckRef(ctx, tenantID, ref, "default_lead_agent", orchestration.AgentRoleLead)
}

// validateDefaultWorkflow refuses a DefaultWorkflow the tenant does not have.
func (s *projectServiceImpl) validateDefaultWorkflow(tenantID, name string) error {
	if name == "" {
		return nil
	}
	return orchestration.RequireWorkflow(s.workflows, "default_workflow", tenantID, name)
}

// validateHostAffinity refuses a host affinity naming no fleet node.
func (s *projectServiceImpl) validateHostAffinity(ctx context.Context, tenantID, hostAffinity string) error {
	if hostAffinity == "" {
		return nil
	}
	return orchestration.RequireFleetNode(ctx, s.fleet, "host_affinity", tenantID, hostAffinity)
}

// validateGitKey refuses a git key reference naming no SSH key. An unset
// reference checks nothing.
func (s *projectServiceImpl) validateGitKey(ctx context.Context, tenantID, field string, ref orchestration.KMSKeyRef) error {
	if !ref.Set() {
		return nil
	}
	return requireSSHKeyRef(ctx, s.sshKeys, tenantID, field, ref)
}

// validateGitCredentialSecret refuses a git credential secret reference
// naming no secret.
func (s *projectServiceImpl) validateGitCredentialSecret(ctx context.Context, tenantID, ref string) error {
	if ref == "" {
		return nil
	}
	_, err := requireSecretRef(ctx, s.secrets, tenantID, "git_credential_secret_ref", ref)
	return err
}

// validateCreateReferences refuses a new project naming an object that does
// not exist. Each check runs only for a field the request sets.
func (s *projectServiceImpl) validateCreateReferences(ctx context.Context, req *CreateProjectRequest, git orchestration.ProjectGit) error {
	if err := s.validateHostAffinity(ctx, req.TenantID, req.HostAffinity); err != nil {
		return err
	}
	if err := s.validateGitKey(ctx, req.TenantID, "git_signing_key", git.Signing.Key); err != nil {
		return err
	}
	if err := s.validateGitKey(ctx, req.TenantID, "git_ssh_key", git.SSHKey); err != nil {
		return err
	}
	return s.validateGitCredentialSecret(ctx, req.TenantID, req.GitCredentialSecretRef)
}

// validateUpdateReferences refuses an update that points a reference at an
// object that does not exist. Only a reference the request changes is
// checked: stored data is not re-validated.
func (s *projectServiceImpl) validateUpdateReferences(ctx context.Context, req *UpdateProjectRequest, git orchestration.ProjectGit) error {
	if err := s.validateHostAffinity(ctx, req.TenantID, req.HostAffinity); err != nil {
		return err
	}
	if req.GitSigningKeyName != "" || req.GitSigningKeyScope != "" || req.GitSigningKeyOwner != "" {
		if err := s.validateGitKey(ctx, req.TenantID, "git_signing_key", git.Signing.Key); err != nil {
			return err
		}
	}
	if req.GitSSHKeyName != "" || req.GitSSHKeyScope != "" || req.GitSSHKeyOwner != "" {
		if err := s.validateGitKey(ctx, req.TenantID, "git_ssh_key", git.SSHKey); err != nil {
			return err
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

// validateProjectSpend refuses spend limits that cannot mean what they
// appear to, naming the field the operator wrote.
func validateProjectSpend(limits orchestration.SpendLimits) error {
	err := limits.Validate()
	if err == nil {
		return nil
	}
	var invalid *orchestration.QuotaInvalidError
	if stderrors.As(err, &invalid) {
		return &ProjectInvalidError{Field: "spend." + invalid.Field, Message: invalid.Message, Err: err}
	}
	return &ProjectInvalidError{Field: "spend", Message: err.Error(), Err: err}
}

// validateStorage refuses a storage-tier configuration the spawn paths could
// not honour. It is the ONE place every transport's create and update reach,
// so an unrecognized tier is refused identically over REST, gRPC, QUIC, MCP,
// Unix and the embedded transport rather than by each handler separately.
// The domain *orchestration.InputValidationError is carried as Err, so a
// caller can still match it with errors.As.
func (s *projectServiceImpl) validateStorage(storage orchestration.ProjectStorage) error {
	if err := storage.Validate(); err != nil {
		invalid := &ProjectInvalidError{Message: err.Error(), Err: err}
		var input *orchestration.InputValidationError
		if stderrors.As(err, &input) {
			// The domain names the field by its record path
			// ("storage.workspace_tier"); the wire is flat.
			invalid.Field = strings.TrimPrefix(input.Field, "storage.")
			invalid.Message = input.Message
		}
		return invalid
	}
	return nil
}

// validateSessionRotation refuses a session rotation policy the runner could
// not act on. Like validateStorage it is the one place every transport's create
// and update reach, so an unknown policy is refused identically everywhere.
func validateSessionRotation(policy orchestration.SessionRotationPolicy) error {
	if err := policy.Validate(); err != nil {
		return &ProjectInvalidError{Field: "session_rotation", Message: err.Error(), Err: err}
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
	if err := s.validateDefaultLead(ctx, req.TenantID, req.DefaultLeadAgent); err != nil {
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
	if err := validateProjectSpend(req.Spend); err != nil {
		return nil, err
	}

	storage := orchestration.ProjectStorage{
		WorkspaceTier:     req.WorkspaceTier,
		WorkspaceTmpfsMB:  req.WorkspaceTmpfsMB,
		ContainerMemoryMB: req.ContainerMemoryMB,
	}
	if err := s.validateStorage(storage); err != nil {
		return nil, err
	}
	if err := validateSessionRotation(req.SessionRotation); err != nil {
		return nil, err
	}
	if err := s.validateCreateReferences(ctx, req, git); err != nil {
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
			FleetNodeID: req.WorkspaceFleetNodeID,
			Path:        req.WorkspacePath,
		},
		Git:     git,
		Storage: storage,
		Quotas: orchestration.ProjectQuotas{
			GPUMinutes:     req.GPUMinutes,
			MaxConcurrency: req.MaxConcurrency,
		},
		Spend:           req.Spend,
		SessionRotation: req.SessionRotation,
		Metadata:        req.Metadata,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}

	if req.FleetNodeRoles != nil {
		project.Permissions = orchestration.ProjectPermissions{FleetNodeRoles: req.FleetNodeRoles}
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
	if req.WorkspaceFleetNodeID != "" {
		existing.Workspace.FleetNodeID = req.WorkspaceFleetNodeID
	}
	if req.WorkspacePath != "" {
		existing.Workspace.Path = req.WorkspacePath
	}
	if req.HostAffinity != "" {
		existing.HostAffinity = req.HostAffinity
	}
	if !req.DefaultLeadAgent.IsZero() {
		if err := s.validateDefaultLead(ctx, req.TenantID, req.DefaultLeadAgent); err != nil {
			return nil, err
		}
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
	if req.GitSigningRetire != nil {
		if err := s.retireSigningKey(ctx, existing, *req.GitSigningRetire); err != nil {
			return nil, err
		}
	}
	if err := s.validateGit(existing.Git); err != nil {
		return nil, err
	}
	if err := s.validateUpdateReferences(ctx, req, existing.Git); err != nil {
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
	if req.Parked != nil {
		if *req.Parked {
			existing.Park(callerName(ctx), time.Now())
		} else {
			existing.Resume()
		}
	}
	if req.ClearContextRepos {
		existing.ContextRepos = nil
	} else if req.ContextRepos != nil {
		if err := s.validateContextRepos(req.ContextRepos); err != nil {
			return nil, err
		}
		existing.ContextRepos = req.ContextRepos
	}
	if req.FleetNodeRoles != nil {
		existing.Permissions.FleetNodeRoles = req.FleetNodeRoles
	}
	if req.Spend != nil {
		if err := validateProjectSpend(*req.Spend); err != nil {
			return nil, err
		}
		existing.Spend = *req.Spend
	}
	if req.GPUMinutes != nil {
		existing.Quotas.GPUMinutes = *req.GPUMinutes
	}
	if req.MaxConcurrency != nil {
		existing.Quotas.MaxConcurrency = *req.MaxConcurrency
	}
	if req.WorkspaceTier != "" {
		existing.Storage.WorkspaceTier = req.WorkspaceTier
	}
	if req.WorkspaceTmpfsMB != nil {
		existing.Storage.WorkspaceTmpfsMB = *req.WorkspaceTmpfsMB
	}
	if req.ContainerMemoryMB != nil {
		existing.Storage.ContainerMemoryMB = *req.ContainerMemoryMB
	}
	if err := s.validateStorage(existing.Storage); err != nil {
		return nil, err
	}
	if req.SessionRotation != "" {
		if err := validateSessionRotation(req.SessionRotation); err != nil {
			return nil, err
		}
		existing.SessionRotation = req.SessionRotation
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

// callerName names the authenticated principal behind ctx for a record that
// says who acted, preferring the human-readable name. Empty when the request
// carried no authenticated subject.
func callerName(ctx context.Context) string {
	subject, ok := auth.SubjectFromContext(ctx)
	if !ok || !subject.Authenticated {
		return ""
	}
	if subject.Username != "" {
		return subject.Username
	}
	return subject.ID
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

	// Refused before the teardown, so a refused delete leaves the managed
	// workspace intact.
	if err := s.refuseBound(ctx, req.TenantID, req.ProjectID); err != nil {
		return nil, err
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

// AuthorizeFleetNode verifies that agentID holds at least requiredRole in projectID.
func (s *projectServiceImpl) AuthorizeFleetNode(ctx context.Context, req *AuthorizeFleetNodeRequest) error {
	if req == nil {
		return &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.FleetNodeID == "" {
		return &ProjectInvalidError{Field: "agent_id", Message: "agent ID is required"}
	}
	if req.ProjectID == "" {
		return nil // unscoped — legacy behaviour
	}

	err := s.authz.AuthorizeFleetNodeForTenant(
		projectStoreContext(ctx, req.TenantID), req.TenantID, req.ProjectID, req.FleetNodeID, req.RequiredRole)
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
				FleetNodeID:  denied.FleetNodeID,
				RequiredRole: denied.RequiredRole,
				ActualRole:   denied.ActualRole,
			}
		case stderrors.As(err, &unresolved):
			return &ProjectUnresolvedError{
				TenantID:    unresolved.TenantID,
				ProjectID:   unresolved.ProjectID,
				FleetNodeID: unresolved.FleetNodeID,
				Err:         err,
			}
		case stderrors.As(err, &roleInvalid):
			return &ProjectRoleInvalidError{Role: roleInvalid.Role}
		default:
			return err
		}
	}
	return nil
}

// GrantFleetNodeRole sets or updates a specific role for an agent in a project.
func (s *projectServiceImpl) GrantFleetNodeRole(ctx context.Context, req *GrantFleetNodeRoleRequest) (*GrantFleetNodeRoleResponse, error) {
	if req == nil {
		return nil, &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.ProjectID == "" {
		return nil, &ProjectInvalidError{Field: "project_id", Message: "project ID is required"}
	}
	if req.FleetNodeID == "" {
		return nil, &ProjectInvalidError{Field: "agent_id", Message: "agent ID is required"}
	}

	ctx = projectStoreContext(ctx, req.TenantID)

	project, err := s.store.Get(ctx, req.TenantID, req.ProjectID)
	if err != nil {
		return nil, s.convertStoreError(err)
	}

	if project.Permissions.FleetNodeRoles == nil {
		project.Permissions.FleetNodeRoles = make(map[string]orchestration.ProjectRole)
	}
	project.Permissions.FleetNodeRoles[req.FleetNodeID] = req.Role
	project.UpdatedAt = time.Now().UTC()

	if err := s.store.Update(ctx, project); err != nil {
		return nil, s.convertStoreError(err)
	}

	return &GrantFleetNodeRoleResponse{
		ProjectID:   req.ProjectID,
		FleetNodeID: req.FleetNodeID,
		Role:        req.Role,
		Timestamp:   time.Now().UTC(),
	}, nil
}

// RevokeFleetNodeRole sets the agent's role to ProjectRoleDenied in projectID.
func (s *projectServiceImpl) RevokeFleetNodeRole(ctx context.Context, req *RevokeFleetNodeRoleRequest) (*RevokeFleetNodeRoleResponse, error) {
	if req == nil {
		return nil, &ProjectInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.ProjectID == "" {
		return nil, &ProjectInvalidError{Field: "project_id", Message: "project ID is required"}
	}
	if req.FleetNodeID == "" {
		return nil, &ProjectInvalidError{Field: "agent_id", Message: "agent ID is required"}
	}

	_, err := s.GrantFleetNodeRole(ctx, &GrantFleetNodeRoleRequest{
		TenantID:    req.TenantID,
		ProjectID:   req.ProjectID,
		FleetNodeID: req.FleetNodeID,
		Role:        orchestration.ProjectRoleDenied,
	})
	if err != nil {
		return nil, err
	}

	return &RevokeFleetNodeRoleResponse{
		ProjectID:   req.ProjectID,
		FleetNodeID: req.FleetNodeID,
		Timestamp:   time.Now().UTC(),
	}, nil
}

// Compile-time interface satisfaction check.
var _ ProjectService = (*projectServiceImpl)(nil)
