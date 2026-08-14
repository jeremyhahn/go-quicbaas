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

// ProjectGit describes the git repository linked to a project.
type ProjectGit struct {
	// RepoURL is the remote git repository URL.
	RepoURL string `json:"repo_url,omitempty"`
	// DefaultBranch is the default branch (e.g. "main").
	DefaultBranch string `json:"default_branch,omitempty"`
	// CurrentSHA is the commit SHA last recorded by an agent job run.
	CurrentSHA string `json:"current_sha,omitempty"`
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
	// TokenBudget updates the LLM token budget (0 = no change).
	TokenBudget int64 `json:"token_budget,omitempty"`
	// GPUMinutes updates the GPU compute budget (0 = no change).
	GPUMinutes int64 `json:"gpu_minutes,omitempty"`
	// MaxConcurrency updates the max concurrent workflow runs (0 = no change).
	MaxConcurrency int `json:"max_concurrency,omitempty"`
	// Metadata merges additional key-value annotations.
	Metadata map[string]string `json:"metadata,omitempty"`
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
