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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
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

	// List returns one page of a tenant's projects. The request's
	// page.Params pick the page; the zero value takes the server default.
	List(ctx context.Context, req *ListProjectsRequest) (*ListProjectsResponse, error)

	// GrantFleetNodeRole sets or updates the role for agentID in projectID.
	// Passing ProjectRoleDenied is equivalent to calling RevokeFleetNodeRole.
	GrantFleetNodeRole(ctx context.Context, req *GrantFleetNodeRoleRequest) (*GrantFleetNodeRoleResponse, error)

	// RevokeFleetNodeRole sets the agent's role to ProjectRoleDenied, effectively
	// removing the agent's access to the project.
	RevokeFleetNodeRole(ctx context.Context, req *RevokeFleetNodeRoleRequest) (*RevokeFleetNodeRoleResponse, error)
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
	FleetNodeID string `json:"agent_id,omitempty"`
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
	// Retired is the trusted signing history: keys the project signed with
	// before Key, each trusted only for commits made before its retirement.
	Retired []RetiredSigningKey `json:"retired,omitempty"`
}

// RetiredSigningKey is one entry of a project's trusted signing history.
type RetiredSigningKey struct {
	// PublicKey is the retired key's public half, one OpenSSH line.
	PublicKey string `json:"public_key"`
	// Name is the KMS name the key was held under.
	Name string `json:"name,omitempty"`
	// RetiredAt is when the key stopped being the project's signing key.
	RetiredAt time.Time `json:"retired_at"`
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
	// FleetNodeRoles maps agent IDs to their assigned role.
	FleetNodeRoles map[string]ProjectRole `json:"agent_roles,omitempty"`
}

// ProjectQuotas defines resource limits for a project.
type ProjectQuotas struct {
	// GPUMinutes is the maximum GPU compute budget in minutes (0 = unlimited).
	GPUMinutes int64 `json:"gpu_minutes,omitempty"`
	// MaxConcurrency is the maximum concurrent workflow runs (0 = unlimited).
	MaxConcurrency int `json:"max_concurrency,omitempty"`
}

// WorkspaceTier names the storage tier a project's sandboxed checkout is
// mounted on. See docs/software-factory/storage-tiers.md.
type WorkspaceTier string

const (
	// WorkspaceTierUnset keeps the host bind mount every project has always
	// had; it behaves exactly like WorkspaceTierPersistent.
	WorkspaceTierUnset WorkspaceTier = ""
	// WorkspaceTierPersistent bind-mounts the checkout from the host.
	WorkspaceTierPersistent WorkspaceTier = "persistent"
	// WorkspaceTierEphemeral puts the checkout on a sized tmpfs inside the
	// container; the work survives only by being pushed.
	WorkspaceTierEphemeral WorkspaceTier = "ephemeral"
)

// SessionRotationPolicy names what a persistent worker session does when the
// pooled provider account under it rotates. See
// docs/software-factory/sdlc.md.
type SessionRotationPolicy string

const (
	// SessionRotationUnset is what a project that never chose a policy
	// stores; it runs SessionRotationResume.
	SessionRotationUnset SessionRotationPolicy = ""
	// SessionRotationResume continues the session's conversation in the
	// replacement session. The default.
	SessionRotationResume SessionRotationPolicy = "resume"
	// SessionRotationFresh starts the replacement session on a new
	// conversation on the same branch.
	SessionRotationFresh SessionRotationPolicy = "fresh"
)

// ProjectStorage is the storage tier of a project's sandboxed checkout.
// Every field is inert at its zero value.
type ProjectStorage struct {
	// WorkspaceTier selects the tier; empty keeps the host bind mount.
	WorkspaceTier WorkspaceTier `json:"workspace_tier,omitempty"`
	// WorkspaceTmpfsMB sizes the ephemeral tier's tmpfs in MiB (0 = unsized).
	WorkspaceTmpfsMB int `json:"workspace_tmpfs_mb,omitempty"`
	// ContainerMemoryMB is the container's memory limit in MiB (0 = the
	// runtime's default).
	ContainerMemoryMB int `json:"container_memory_mb,omitempty"`
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
	ID               string           `json:"id"`
	TenantID         string           `json:"tenant_id"`
	Name             string           `json:"name"`
	Description      string           `json:"description,omitempty"`
	Workspace        ProjectWorkspace `json:"workspace"`
	Storage          ProjectStorage   `json:"storage,omitzero"`
	HostAffinity     string           `json:"host_affinity,omitempty"`
	DefaultLeadAgent agents.AgentRef  `json:"default_lead_agent,omitzero"`
	DefaultWorkflow  string           `json:"default_workflow,omitempty"`
	Autonomous       bool             `json:"autonomous,omitempty"`
	// Parked says the project's intake is closed: nothing that has not
	// started is dispatched, adopted or triaged, and work in flight
	// finishes. ParkedAt and ParkedBy say when and by whom; both are zero
	// while the project is open.
	Parked       bool               `json:"parked,omitempty"`
	ParkedAt     time.Time          `json:"parked_at,omitzero"`
	ParkedBy     string             `json:"parked_by,omitempty"`
	ContextRepos []ContextRepo      `json:"context_repos,omitempty"`
	Git          ProjectGit         `json:"git"`
	Permissions  ProjectPermissions `json:"permissions"`
	Quotas       ProjectQuotas      `json:"quotas"`
	Spend        spend.Limits       `json:"spend,omitzero"`
	// SessionRotation is the project's session rotation policy: "resume",
	// "fresh", or empty for a project that never chose one, which runs
	// "resume".
	SessionRotation SessionRotationPolicy `json:"session_rotation,omitempty"`
	Metadata        map[string]string     `json:"metadata,omitempty"`
	CreatedAt       time.Time             `json:"created_at"`
	UpdatedAt       time.Time             `json:"updated_at"`
}

// CreateProjectRequest is a request to create a new project.
type CreateProjectRequest struct {
	// TenantID is the tenant that owns the project. Required.
	TenantID string `json:"tenant_id"`
	// Name is the unique project name within the tenant. Required.
	Name string `json:"name"`
	// Description is a human-readable description of the project.
	Description string `json:"description,omitempty"`
	// WorkspaceFleetNodeID is the ID of the agent that owns the workspace.
	WorkspaceFleetNodeID string `json:"workspace_agent_id,omitempty"`
	// WorkspacePath is the filesystem path to the workspace on the agent host.
	WorkspacePath string `json:"workspace_path,omitempty"`
	// HostAffinity pins operations to a preferred agent host.
	HostAffinity string `json:"host_affinity,omitempty"`
	// DefaultLeadAgent is the Agent or pool that leads a job under this
	// project when the job names none; its Agents must declare the lead role.
	DefaultLeadAgent agents.AgentRef `json:"default_lead_agent,omitzero"`
	// GitRepoURL is the remote git repository URL.
	GitRepoURL string `json:"git_repo_url,omitempty"`
	// GitDefaultBranch is the default git branch.
	GitDefaultBranch string `json:"git_default_branch,omitempty"`
	// Spend is the project's spend limits: sliding windows in tokens and/or
	// list-price dollars over every turn charged to it. The zero value meters
	// nothing. The limits are stored; usage reporting against them and
	// admission enforcement are not wired yet.
	Spend spend.Limits `json:"spend,omitzero"`
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

	// WorkspaceTier / WorkspaceTmpfsMB / ContainerMemoryMB configure the
	// storage tier of the sandboxed checkout. Every one is inert at its zero
	// value; an unrecognized tier is refused by the server.
	WorkspaceTier     WorkspaceTier `json:"workspace_tier,omitempty"`
	WorkspaceTmpfsMB  int           `json:"workspace_tmpfs_mb,omitempty"`
	ContainerMemoryMB int           `json:"container_memory_mb,omitempty"`

	// SessionRotation is what a persistent worker session does when the
	// pooled provider account under it rotates. Empty is "resume"; an
	// unrecognized policy is refused by the server.
	SessionRotation SessionRotationPolicy `json:"session_rotation,omitempty"`
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
	// WorkspaceFleetNodeID updates the workspace agent.
	WorkspaceFleetNodeID string `json:"workspace_agent_id,omitempty"`
	// WorkspacePath updates the workspace filesystem path.
	WorkspacePath string `json:"workspace_path,omitempty"`
	// HostAffinity updates the preferred host.
	HostAffinity string `json:"host_affinity,omitempty"`
	// DefaultLeadAgent updates the default lead Agent or pool; the zero
	// reference leaves it unchanged.
	DefaultLeadAgent agents.AgentRef `json:"default_lead_agent,omitzero"`
	// GitRepoURL updates the remote git repository URL.
	GitRepoURL string `json:"git_repo_url,omitempty"`
	// GitDefaultBranch updates the default git branch.
	GitDefaultBranch string `json:"git_default_branch,omitempty"`
	// GitCurrentSHA records the latest commit SHA observed by an agent.
	GitCurrentSHA string `json:"git_current_sha,omitempty"`
	// FleetNodeRoles replaces the agent role map when non-nil.
	FleetNodeRoles map[string]ProjectRole `json:"agent_roles,omitempty"`
	// The quotas are POINTERS so zero is expressible. Zero means UNLIMITED
	// on the project record, so nil ("leave it alone") and zero ("lift the
	// cap") are different instructions. As plain ints they could only say
	// the first: an operator lifting a cap watched the save succeed and
	// stayed capped, while the UI and the docs both called zero unlimited.
	//
	// Spend replaces the project's spend limits when non-nil; a non-nil
	// empty value clears them. Nil leaves them unchanged.
	Spend *spend.Limits `json:"spend,omitempty"`
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

	// Parked parks (true) or resumes (false) the project when non-nil.
	// Parking closes the intake and lets work in flight finish; resuming
	// reopens it. Neither touches Autonomous.
	Parked *bool `json:"parked,omitempty"`

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

	// GitSigningRetireKeyName / Scope / Owner name a KMS-held key the
	// project signed with before its current one; the server records its
	// public half in the project's trusted signing history, retired at
	// GitSigningRetiredAt (RFC 3339; empty is now). Commits it signed before
	// then keep verifying after the rotation.
	GitSigningRetireKeyName  string `json:"git_signing_retire_key_name,omitempty"`
	GitSigningRetireKeyScope string `json:"git_signing_retire_key_scope,omitempty"`
	GitSigningRetireKeyOwner string `json:"git_signing_retire_key_owner,omitempty"`
	GitSigningRetiredAt      string `json:"git_signing_retired_at,omitempty"`

	// WorkspaceTier updates the storage tier when non-empty.
	//
	// The sizes are POINTERS for the reason the quotas are: zero means an
	// unsized tmpfs / the runtime's default memory limit, so nil ("leave it
	// alone") and zero ("lift the limit") are different instructions.
	WorkspaceTier     WorkspaceTier `json:"workspace_tier,omitempty"`
	WorkspaceTmpfsMB  *int          `json:"workspace_tmpfs_mb,omitempty"`
	ContainerMemoryMB *int          `json:"container_memory_mb,omitempty"`

	// SessionRotation updates the session rotation policy when non-empty.
	SessionRotation SessionRotationPolicy `json:"session_rotation,omitempty"`
}

// ListProjectsResponse is the response from listing projects.
type ListProjectsResponse struct {
	Projects []*ProjectInfo `json:"projects"`

	// Envelope carries how many records matched and where the next page
	// starts; see package page.
	page.Envelope
}

// GrantFleetNodeRoleRequest sets a role for an agent in a project.
type GrantFleetNodeRoleRequest struct {
	// TenantID is the owning tenant. Required.
	TenantID string `json:"tenant_id"`
	// ProjectID is the target project UUID. Required.
	ProjectID string `json:"project_id"`
	// AgentID is the agent to grant the role to. Required.
	FleetNodeID string `json:"node_id"`
	// Role is the role to assign. Required. One of: lead, worker, observer, denied.
	Role ProjectRole `json:"role"`
}

// GrantFleetNodeRoleResponse confirms the role assignment.
type GrantFleetNodeRoleResponse struct {
	ProjectID   string      `json:"project_id"`
	FleetNodeID string      `json:"node_id"`
	Role        ProjectRole `json:"role"`
	Timestamp   time.Time   `json:"timestamp"`
}

// RevokeFleetNodeRoleRequest removes an agent's access to a project.
type RevokeFleetNodeRoleRequest struct {
	// TenantID is the owning tenant. Required.
	TenantID string `json:"tenant_id"`
	// ProjectID is the target project UUID. Required.
	ProjectID string `json:"project_id"`
	// AgentID is the agent to revoke. Required.
	FleetNodeID string `json:"node_id"`
}

// RevokeFleetNodeRoleResponse confirms the revocation.
type RevokeFleetNodeRoleResponse struct {
	ProjectID   string    `json:"project_id"`
	FleetNodeID string    `json:"node_id"`
	Timestamp   time.Time `json:"timestamp"`
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

	// Autonomous narrows the listing to the projects with autonomous
	// development on (true) or off (false). Nil does not narrow: a bool
	// could not tell "the supervised ones" from "no preference".
	Autonomous *bool `json:"autonomous,omitempty"`

	// Params pages, orders and searches the listing. The zero value takes
	// the server's default page; see package page.
	page.Params
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

// List returns one page of a tenant's projects.
func (c *client) List(ctx context.Context, req *ListProjectsRequest) (*ListProjectsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("project.List", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("project.List", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("project.List", "tenant ID is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "List", req)
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

// GrantFleetNodeRole sets or updates the role for agentID in projectID.
func (c *client) GrantFleetNodeRole(ctx context.Context, req *GrantFleetNodeRoleRequest) (*GrantFleetNodeRoleResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("project.GrantFleetNodeRole", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("project.GrantFleetNodeRole", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("project.GrantFleetNodeRole", "tenant ID is required")
	}
	if req.ProjectID == "" {
		return nil, errors.NewInvalidInput("project.GrantFleetNodeRole", "project ID is required")
	}
	if req.FleetNodeID == "" {
		return nil, errors.NewInvalidInput("project.GrantFleetNodeRole", "agent ID is required")
	}
	if req.Role == "" {
		return nil, errors.NewInvalidInput("project.GrantFleetNodeRole", "role is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GrantFleetNodeRole", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("project.GrantFleetNodeRole", "nil response from server")
	}

	result, ok := resp.(*GrantFleetNodeRoleResponse)
	if !ok {
		return nil, errors.NewInternal("project.GrantFleetNodeRole", "invalid response type")
	}
	return result, nil
}

// RevokeFleetNodeRole sets the agent's role to ProjectRoleDenied.
func (c *client) RevokeFleetNodeRole(ctx context.Context, req *RevokeFleetNodeRoleRequest) (*RevokeFleetNodeRoleResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("project.RevokeFleetNodeRole", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("project.RevokeFleetNodeRole", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("project.RevokeFleetNodeRole", "tenant ID is required")
	}
	if req.ProjectID == "" {
		return nil, errors.NewInvalidInput("project.RevokeFleetNodeRole", "project ID is required")
	}
	if req.FleetNodeID == "" {
		return nil, errors.NewInvalidInput("project.RevokeFleetNodeRole", "agent ID is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "RevokeFleetNodeRole", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("project.RevokeFleetNodeRole", "nil response from server")
	}

	result, ok := resp.(*RevokeFleetNodeRoleResponse)
	if !ok {
		return nil, errors.NewInternal("project.RevokeFleetNodeRole", "invalid response type")
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
