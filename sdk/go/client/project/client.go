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

package project

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Project"
)

// Client provides project lifecycle management operations.
type Client interface {
	// Create creates a new project within a tenant.
	Create(ctx context.Context, req *CreateProjectRequest) (*ProjectInfo, error)

	// Get retrieves a project by ID.
	Get(ctx context.Context, tenantID, projectID string) (*ProjectInfo, error)

	// GetByName retrieves a project by name within a tenant.
	GetByName(ctx context.Context, tenantID, name string) (*ProjectInfo, error)

	// Update updates an existing project. Only non-zero fields are applied.
	Update(ctx context.Context, req *UpdateProjectRequest) (*ProjectInfo, error)

	// Delete removes a project by ID.
	Delete(ctx context.Context, tenantID, projectID string) error

	// List returns all projects for a given tenant.
	List(ctx context.Context, tenantID string) (*ListProjectsResponse, error)

	// GrantAgentRole sets or updates the role for agentID in projectID.
	// Passing ProjectRoleDenied is equivalent to calling RevokeAgentRole.
	GrantAgentRole(ctx context.Context, req *GrantAgentRoleRequest) (*GrantAgentRoleResponse, error)

	// RevokeAgentRole sets the agent's role to ProjectRoleDenied, effectively
	// removing the agent's access to the project.
	RevokeAgentRole(ctx context.Context, req *RevokeAgentRoleRequest) (*RevokeAgentRoleResponse, error)
}

// ProjectRole represents the role an agent has within a project.
type ProjectRole string

const (
	// ProjectRoleLead gives full control over the project and its workflows.
	ProjectRoleLead ProjectRole = "lead"

	// ProjectRoleWorker can execute tasks but cannot modify project configuration.
	ProjectRoleWorker ProjectRole = "worker"

	// ProjectRoleObserver has read-only access to the project.
	ProjectRoleObserver ProjectRole = "observer"

	// ProjectRoleDenied explicitly prohibits access.
	ProjectRoleDenied ProjectRole = "denied"
)

// ProjectWorkspace describes the agent workspace associated with a project.
type ProjectWorkspace struct {
	// AgentID is the ID of the agent that owns the workspace.
	AgentID string `json:"agent_id,omitempty"`
	// Path is the filesystem path to the workspace on the agent host.
	Path string `json:"path,omitempty"`
}

// GitAuthor is the identity recorded on commits made from the workspace.
type GitAuthor struct {
	// Name is the author name.
	Name string `json:"name,omitempty"`
	// Email is the author email.
	Email string `json:"email,omitempty"`
}

// KMSKeyRef names a KMS-held key by (name, scope, owner). Never material.
type KMSKeyRef struct {
	// Name identifies the key within (Scope, Owner).
	Name string `json:"name,omitempty"`
	// Scope is the key store scope ("user", "tenant", "service").
	Scope string `json:"scope,omitempty"`
	// Owner is the scope-specific owner.
	Owner string `json:"owner,omitempty"`
}

// CommitSigning decides whether and how commits from the workspace are
// signed. The zero value signs nothing.
type CommitSigning struct {
	// Enabled turns signing on; an enabled policy names a Key.
	Enabled bool `json:"enabled,omitempty"`
	// Algorithm is the KMS key algorithm (ED25519 default, ECDSA-P256 FIPS).
	Algorithm string `json:"algorithm,omitempty"`
	// Key identifies the KMS-held signing key.
	Key KMSKeyRef `json:"key,omitzero"`
}

// ProjectGit describes the git repository linked to a project.
type ProjectGit struct {
	// RepoURL is the remote git repository URL.
	RepoURL string `json:"repo_url,omitempty"`
	// DefaultBranch is the default branch (e.g. "main").
	DefaultBranch string `json:"default_branch,omitempty"`
	// CurrentSHA is the commit SHA last recorded by an agent job run.
	CurrentSHA string `json:"current_sha,omitempty"`
	// Author is the identity recorded on commits made from the workspace.
	Author GitAuthor `json:"author,omitzero"`
	// Signing decides whether and how commits are signed.
	Signing CommitSigning `json:"signing,omitzero"`
	// SSHKey is the KMS key an ssh:// RepoURL is reached with; empty means
	// the signing key is used.
	SSHKey KMSKeyRef `json:"ssh_key,omitzero"`
}

// ProjectPermissions defines agent-level access roles for a project.
type ProjectPermissions struct {
	// AgentRoles maps agent IDs to their assigned role.
	AgentRoles map[string]ProjectRole `json:"agent_roles,omitempty"`
}

// ProjectQuotas defines resource limits for a project.
type ProjectQuotas struct {
	// TokenBudget is the maximum LLM token budget (0 = unlimited).
	TokenBudget int64 `json:"token_budget,omitempty"`
	// GPUMinutes is the maximum GPU compute budget in minutes (0 = unlimited).
	GPUMinutes int64 `json:"gpu_minutes,omitempty"`
	// MaxConcurrency is the maximum concurrent workflow runs (0 = unlimited).
	MaxConcurrency int `json:"max_concurrency,omitempty"`
}

// ContextRepo names one repository the worker clones beside the project
// checkout. The daemon never clones it.
type ContextRepo struct {
	// Name is the directory the repository is cloned into, beside the
	// project checkout. One path segment.
	Name string `json:"name"`
	// RepoURL is the remote the worker clones from.
	RepoURL string `json:"repo_url"`
	// Ref is the branch, tag or commit the worker checks out.
	Ref string `json:"ref"`
}

// ProjectInfo contains information about a project.
//
// The JSON tags are the canonical project wire shape. Every transport that
// carries a project over JSON (MCP tool results, QUIC frame payloads) encodes
// these exact keys, so the type decodes a server response directly instead of
// each transport re-declaring a parallel wire struct.
type ProjectInfo struct {
	ID               string             `json:"id"`
	TenantID         string             `json:"tenant_id"`
	Name             string             `json:"name"`
	Description      string             `json:"description,omitempty"`
	Workspace        ProjectWorkspace   `json:"workspace"`
	HostAffinity     string             `json:"host_affinity,omitempty"`
	DefaultLeadAgent string             `json:"default_lead_agent,omitempty"`
	DefaultWorkflow  string             `json:"default_workflow,omitempty"`
	Autonomous       bool               `json:"autonomous,omitempty"`
	ContextRepos     []ContextRepo      `json:"context_repos,omitempty"`
	Git              ProjectGit         `json:"git"`
	Permissions      ProjectPermissions `json:"permissions"`
	Quotas           ProjectQuotas      `json:"quotas"`
	Metadata         map[string]string  `json:"metadata,omitempty"`
	CreatedAt        time.Time          `json:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
}

// CreateProjectRequest is a request to create a new project.
type CreateProjectRequest struct {
	// TenantID is the tenant that owns the project. Required.
	TenantID string `json:"tenant_id"`
	// Name is the unique project name within the tenant. Required.
	Name string `json:"name"`
	// Description is a human-readable description of the project.
	Description string `json:"description,omitempty"`
	// WorkspaceAgentID is the ID of the agent that owns the workspace.
	WorkspaceAgentID string `json:"workspace_agent_id,omitempty"`
	// WorkspacePath is the filesystem path to the workspace on the agent host.
	WorkspacePath string `json:"workspace_path,omitempty"`
	// HostAffinity pins operations to a preferred agent host.
	HostAffinity string `json:"host_affinity,omitempty"`
	// DefaultLeadAgent is the default lead agent ID for workflow runs.
	DefaultLeadAgent string `json:"default_lead_agent,omitempty"`
	// GitRepoURL is the remote git repository URL.
	GitRepoURL string `json:"git_repo_url,omitempty"`
	// GitDefaultBranch is the default git branch.
	GitDefaultBranch string `json:"git_default_branch,omitempty"`
	// TokenBudget is the maximum LLM token budget (0 = unlimited).
	TokenBudget int64 `json:"token_budget,omitempty"`
	// GPUMinutes is the maximum GPU compute budget in minutes (0 = unlimited).
	GPUMinutes int64 `json:"gpu_minutes,omitempty"`
	// MaxConcurrency is the maximum concurrent workflow runs (0 = unlimited).
	MaxConcurrency int `json:"max_concurrency,omitempty"`
	// Metadata is an arbitrary key-value map for caller-defined annotations.
	Metadata map[string]string `json:"metadata,omitempty"`

	// DefaultWorkflow names the tenant-scoped workflow a Task auto-spawns
	// into; it must be registered in the tenant.
	DefaultWorkflow string `json:"default_workflow,omitempty"`

	// Autonomous turns autonomous development on for the new project.
	Autonomous bool `json:"autonomous,omitempty"`

	// ContextRepos are cloned by the worker beside the project checkout.
	ContextRepos []ContextRepo `json:"context_repos,omitempty"`

	// GitAuthorName / GitAuthorEmail are the commit identity of the
	// workspace. Both or neither.
	GitAuthorName  string `json:"git_author_name,omitempty"`
	GitAuthorEmail string `json:"git_author_email,omitempty"`

	// GitSigningEnabled turns commit signing on; an enabled policy names
	// GitSigningKeyName. GitSigningAlgorithm is ED25519 (default) or
	// ECDSA-P256 (FIPS).
	GitSigningEnabled   bool   `json:"git_signing_enabled,omitempty"`
	GitSigningAlgorithm string `json:"git_signing_algorithm,omitempty"`
	GitSigningKeyName   string `json:"git_signing_key_name,omitempty"`
	GitSigningKeyScope  string `json:"git_signing_key_scope,omitempty"`
	GitSigningKeyOwner  string `json:"git_signing_key_owner,omitempty"`

	// GitSSHKeyName / GitSSHKeyScope / GitSSHKeyOwner reference the KMS key
	// an ssh:// GitRepoURL is reached with. Empty means the signing key.
	GitSSHKeyName  string `json:"git_ssh_key_name,omitempty"`
	GitSSHKeyScope string `json:"git_ssh_key_scope,omitempty"`
	GitSSHKeyOwner string `json:"git_ssh_key_owner,omitempty"`
}

// UpdateProjectRequest is a request to update an existing project.
// Only non-zero fields are applied. Use ClearDescription to explicitly
// remove the description.
type UpdateProjectRequest struct {
	// TenantID identifies the owning tenant. Required.
	TenantID string `json:"tenant_id"`
	// ProjectID is the project UUID. Required.
	ProjectID string `json:"project_id"`
	// Name sets a new project name when non-empty.
	Name string `json:"name,omitempty"`
	// Description replaces the project description when non-empty.
	Description string `json:"description,omitempty"`
	// ClearDescription removes the description when true.
	ClearDescription bool `json:"clear_description,omitempty"`
	// WorkspaceAgentID updates the workspace agent.
	WorkspaceAgentID string `json:"workspace_agent_id,omitempty"`
	// WorkspacePath updates the workspace filesystem path.
	WorkspacePath string `json:"workspace_path,omitempty"`
	// HostAffinity updates the preferred host.
	HostAffinity string `json:"host_affinity,omitempty"`
	// DefaultLeadAgent updates the default lead agent.
	DefaultLeadAgent string `json:"default_lead_agent,omitempty"`
	// GitRepoURL updates the remote git repository URL.
	GitRepoURL string `json:"git_repo_url,omitempty"`
	// GitDefaultBranch updates the default git branch.
	GitDefaultBranch string `json:"git_default_branch,omitempty"`
	// GitCurrentSHA records the latest commit SHA observed by an agent.
	GitCurrentSHA string `json:"git_current_sha,omitempty"`
	// AgentRoles replaces the agent role map when non-nil.
	AgentRoles map[string]ProjectRole `json:"agent_roles,omitempty"`
	// The quotas are POINTERS so zero is expressible. Zero means UNLIMITED
	// on the project record, so nil ("leave it alone") and zero ("lift the
	// cap") are different instructions. As plain ints they could only say
	// the first: an operator lifting a cap watched the save succeed and
	// stayed capped, while the UI and the docs both called zero unlimited.
	//
	// TokenBudget updates the LLM token budget; zero means unlimited.
	TokenBudget *int64 `json:"token_budget,omitempty"`
	// GPUMinutes updates the GPU compute budget; zero means unlimited.
	GPUMinutes *int64 `json:"gpu_minutes,omitempty"`
	// MaxConcurrency updates the concurrent-run cap; zero means unlimited.
	MaxConcurrency *int `json:"max_concurrency,omitempty"`
	// Metadata merges additional key-value annotations.
	Metadata map[string]string `json:"metadata,omitempty"`

	// DefaultWorkflow sets the auto-spawn workflow when non-empty;
	// ClearDefaultWorkflow removes it.
	DefaultWorkflow      string `json:"default_workflow,omitempty"`
	ClearDefaultWorkflow bool   `json:"clear_default_workflow,omitempty"`

	// Autonomous updates the autonomous-development switch when non-nil, so
	// "unchanged" and "off" stay distinct.
	Autonomous *bool `json:"autonomous,omitempty"`

	// ContextRepos replaces the whole list when non-nil; ClearContextRepos
	// empties it.
	ContextRepos      []ContextRepo `json:"context_repos,omitempty"`
	ClearContextRepos bool          `json:"clear_context_repos,omitempty"`

	// GitAuthorName / GitAuthorEmail update the commit identity when
	// non-empty.
	GitAuthorName  string `json:"git_author_name,omitempty"`
	GitAuthorEmail string `json:"git_author_email,omitempty"`

	// GitSigningEnabled updates the signing switch when non-nil (so
	// "unchanged" and "off" are distinct); the other signing fields update
	// when non-empty.
	GitSigningEnabled   *bool  `json:"git_signing_enabled,omitempty"`
	GitSigningAlgorithm string `json:"git_signing_algorithm,omitempty"`
	GitSigningKeyName   string `json:"git_signing_key_name,omitempty"`
	GitSigningKeyScope  string `json:"git_signing_key_scope,omitempty"`
	GitSigningKeyOwner  string `json:"git_signing_key_owner,omitempty"`

	// GitSSHKeyName / GitSSHKeyScope / GitSSHKeyOwner update the access key
	// reference when non-empty.
	GitSSHKeyName  string `json:"git_ssh_key_name,omitempty"`
	GitSSHKeyScope string `json:"git_ssh_key_scope,omitempty"`
	GitSSHKeyOwner string `json:"git_ssh_key_owner,omitempty"`
}

// ListProjectsResponse is the response from listing projects.
type ListProjectsResponse struct {
	Projects []*ProjectInfo `json:"projects"`
	Total    int            `json:"total"`
}

// GrantAgentRoleRequest sets a role for an agent in a project.
type GrantAgentRoleRequest struct {
	// TenantID is the owning tenant. Required.
	TenantID string `json:"tenant_id"`
	// ProjectID is the target project UUID. Required.
	ProjectID string `json:"project_id"`
	// AgentID is the agent to grant the role to. Required.
	AgentID string `json:"agent_id"`
	// Role is the role to assign. Required. One of: lead, worker, observer, denied.
	Role ProjectRole `json:"role"`
}

// GrantAgentRoleResponse confirms the role assignment.
type GrantAgentRoleResponse struct {
	ProjectID string      `json:"project_id"`
	AgentID   string      `json:"agent_id"`
	Role      ProjectRole `json:"role"`
	Timestamp time.Time   `json:"timestamp"`
}

// RevokeAgentRoleRequest removes an agent's access to a project.
type RevokeAgentRoleRequest struct {
	// TenantID is the owning tenant. Required.
	TenantID string `json:"tenant_id"`
	// ProjectID is the target project UUID. Required.
	ProjectID string `json:"project_id"`
	// AgentID is the agent to revoke. Required.
	AgentID string `json:"agent_id"`
}

// RevokeAgentRoleResponse confirms the revocation.
type RevokeAgentRoleResponse struct {
	ProjectID string    `json:"project_id"`
	AgentID   string    `json:"agent_id"`
	Timestamp time.Time `json:"timestamp"`
}

// GetProjectRequest identifies a project by ID for transport dispatch.
type GetProjectRequest struct {
	TenantID  string `json:"tenant_id"`
	ProjectID string `json:"project_id"`
}

// GetProjectByNameRequest identifies a project by name for transport dispatch.
type GetProjectByNameRequest struct {
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
}

// DeleteProjectRequest identifies a project to delete for transport dispatch.
type DeleteProjectRequest struct {
	TenantID  string `json:"tenant_id"`
	ProjectID string `json:"project_id"`
}

// DeleteProjectResponse confirms deletion.
type DeleteProjectResponse struct {
	Deleted bool `json:"deleted"`
}

// ListProjectsRequest specifies which tenant's projects to list.
type ListProjectsRequest struct {
	TenantID string `json:"tenant_id"`
}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new project client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{transport: t}
}

// Create creates a new project within a tenant.
func (c *client) Create(ctx context.Context, req *CreateProjectRequest) (*ProjectInfo, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("project.Create", "context is required")
	}
	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "Create", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("project.Create", "nil response from server")
	}

	info, ok := resp.(*ProjectInfo)
	if !ok {
		return nil, errors.NewInternal("project.Create", "invalid response type")
	}
	return info, nil
}

// Get retrieves a project by ID.
func (c *client) Get(ctx context.Context, tenantID, projectID string) (*ProjectInfo, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("project.Get", "context is required")
	}
	if tenantID == "" {
		return nil, errors.NewInvalidInput("project.Get", "tenant ID is required")
	}
	if projectID == "" {
		return nil, errors.NewInvalidInput("project.Get", "project ID is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Get", &GetProjectRequest{
		TenantID:  tenantID,
		ProjectID: projectID,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewNotFound("project.Get", "project not found")
	}

	info, ok := resp.(*ProjectInfo)
	if !ok {
		return nil, errors.NewInternal("project.Get", "invalid response type")
	}
	return info, nil
}

// GetByName retrieves a project by name within a tenant.
func (c *client) GetByName(ctx context.Context, tenantID, name string) (*ProjectInfo, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("project.GetByName", "context is required")
	}
	if tenantID == "" {
		return nil, errors.NewInvalidInput("project.GetByName", "tenant ID is required")
	}
	if name == "" {
		return nil, errors.NewInvalidInput("project.GetByName", "name is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetByName", &GetProjectByNameRequest{
		TenantID: tenantID,
		Name:     name,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewNotFound("project.GetByName", "project not found")
	}

	info, ok := resp.(*ProjectInfo)
	if !ok {
		return nil, errors.NewInternal("project.GetByName", "invalid response type")
	}
	return info, nil
}

// Update updates an existing project.
func (c *client) Update(ctx context.Context, req *UpdateProjectRequest) (*ProjectInfo, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("project.Update", "context is required")
	}
	if err := validateUpdateRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "Update", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("project.Update", "nil response from server")
	}

	info, ok := resp.(*ProjectInfo)
	if !ok {
		return nil, errors.NewInternal("project.Update", "invalid response type")
	}
	return info, nil
}

// Delete removes a project by ID.
func (c *client) Delete(ctx context.Context, tenantID, projectID string) error {
	if ctx == nil {
		return errors.NewInvalidInput("project.Delete", "context is required")
	}
	if tenantID == "" {
		return errors.NewInvalidInput("project.Delete", "tenant ID is required")
	}
	if projectID == "" {
		return errors.NewInvalidInput("project.Delete", "project ID is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Delete", &DeleteProjectRequest{
		TenantID:  tenantID,
		ProjectID: projectID,
	})
	if err != nil {
		return err
	}
	if resp == nil {
		return nil
	}
	if _, ok := resp.(*DeleteProjectResponse); !ok {
		return errors.NewInternal("project.Delete", "invalid response type")
	}
	return nil
}

// List returns all projects for a given tenant.
func (c *client) List(ctx context.Context, tenantID string) (*ListProjectsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("project.List", "context is required")
	}
	if tenantID == "" {
		return nil, errors.NewInvalidInput("project.List", "tenant ID is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "List", &ListProjectsRequest{
		TenantID: tenantID,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return &ListProjectsResponse{Projects: []*ProjectInfo{}}, nil
	}

	listResp, ok := resp.(*ListProjectsResponse)
	if !ok {
		return nil, errors.NewInternal("project.List", "invalid response type")
	}
	return listResp, nil
}

// GrantAgentRole sets or updates the role for agentID in projectID.
func (c *client) GrantAgentRole(ctx context.Context, req *GrantAgentRoleRequest) (*GrantAgentRoleResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("project.GrantAgentRole", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("project.GrantAgentRole", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("project.GrantAgentRole", "tenant ID is required")
	}
	if req.ProjectID == "" {
		return nil, errors.NewInvalidInput("project.GrantAgentRole", "project ID is required")
	}
	if req.AgentID == "" {
		return nil, errors.NewInvalidInput("project.GrantAgentRole", "agent ID is required")
	}
	if req.Role == "" {
		return nil, errors.NewInvalidInput("project.GrantAgentRole", "role is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GrantAgentRole", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("project.GrantAgentRole", "nil response from server")
	}

	result, ok := resp.(*GrantAgentRoleResponse)
	if !ok {
		return nil, errors.NewInternal("project.GrantAgentRole", "invalid response type")
	}
	return result, nil
}

// RevokeAgentRole sets the agent's role to ProjectRoleDenied.
func (c *client) RevokeAgentRole(ctx context.Context, req *RevokeAgentRoleRequest) (*RevokeAgentRoleResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("project.RevokeAgentRole", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("project.RevokeAgentRole", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("project.RevokeAgentRole", "tenant ID is required")
	}
	if req.ProjectID == "" {
		return nil, errors.NewInvalidInput("project.RevokeAgentRole", "project ID is required")
	}
	if req.AgentID == "" {
		return nil, errors.NewInvalidInput("project.RevokeAgentRole", "agent ID is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "RevokeAgentRole", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("project.RevokeAgentRole", "nil response from server")
	}

	result, ok := resp.(*RevokeAgentRoleResponse)
	if !ok {
		return nil, errors.NewInternal("project.RevokeAgentRole", "invalid response type")
	}
	return result, nil
}

// =============================================================================
// Validation
// =============================================================================

func validateCreateRequest(req *CreateProjectRequest) error {
	if req == nil {
		return errors.NewInvalidInput("project.Create", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("project.Create", "tenant ID is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("project.Create", "project name is required")
	}
	return nil
}

func validateUpdateRequest(req *UpdateProjectRequest) error {
	if req == nil {
		return errors.NewInvalidInput("project.Update", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("project.Update", "tenant ID is required")
	}
	if req.ProjectID == "" {
		return errors.NewInvalidInput("project.Update", "project ID is required")
	}
	return nil
}
