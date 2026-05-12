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

package orchestration

import "time"

// =============================================================================
// Project — First-class entity between Tenant and Workflow
// =============================================================================

// Project is a long-running unit of agentic work with its own identity,
// workspace, permissions, and git metadata. Multiple workflows can target the
// same project. Each project may live on different physical machines
// (filesystem locality matters for git operations performed by agents).
type Project struct {
	// ID is a UUID that uniquely identifies this project.
	ID string `json:"id" yaml:"id"`

	// TenantID is the tenant that owns this project.
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Name is unique within the owning tenant.
	Name string `json:"name" yaml:"name"`

	// Description is a human-readable description of the project.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Workspace describes where the project code lives.
	Workspace ProjectWorkspace `json:"workspace" yaml:"workspace"`

	// HostAffinity is the preferred agent host for project operations (optional).
	HostAffinity string `json:"host_affinity,omitempty" yaml:"host_affinity,omitempty"`

	// DefaultLeadAgent is the default agent ID for leading workflow runs
	// under this project. Individual jobs may override this.
	DefaultLeadAgent string `json:"default_lead_agent,omitempty" yaml:"default_lead_agent,omitempty"`

	// Git contains the repository metadata.
	Git ProjectGit `json:"git" yaml:"git"`

	// Permissions defines the per-agent role matrix for this project.
	Permissions ProjectPermissions `json:"permissions" yaml:"permissions"`

	// Quotas defines resource limits for work done under this project.
	Quotas ProjectQuotas `json:"quotas" yaml:"quotas"`

	// Metadata holds arbitrary key-value pairs for extension.
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	// CreatedAt is the time the project was created.
	CreatedAt time.Time `json:"created_at" yaml:"created_at"`

	// UpdatedAt is the time the project was last modified.
	UpdatedAt time.Time `json:"updated_at" yaml:"updated_at"`
}

// ProjectWorkspace describes the filesystem location of the project.
type ProjectWorkspace struct {
	// AgentID is the primary agent that owns the workspace.
	AgentID string `json:"agent_id" yaml:"agent_id"`

	// Path is the filesystem path on the owning agent.
	Path string `json:"path" yaml:"path"`
}

// ProjectGit holds repository metadata for a project.
type ProjectGit struct {
	// RepoURL is the remote repository URL.
	RepoURL string `json:"repo_url,omitempty" yaml:"repo_url,omitempty"`

	// DefaultBranch is the primary branch (e.g. "main").
	DefaultBranch string `json:"default_branch,omitempty" yaml:"default_branch,omitempty"`

	// CurrentSHA is updated by agents during job runs to track HEAD.
	CurrentSHA string `json:"current_sha,omitempty" yaml:"current_sha,omitempty"`
}

// ProjectPermissions holds the per-agent role matrix for a project.
type ProjectPermissions struct {
	// AgentRoles maps agent_id to its ProjectRole within the project.
	AgentRoles map[string]ProjectRole `json:"agent_roles,omitempty" yaml:"agent_roles,omitempty"`
}

// ProjectRole defines the level of access an agent has to a project.
type ProjectRole string

const (
	// ProjectRoleLead grants full orchestration rights within the project.
	ProjectRoleLead ProjectRole = "lead"

	// ProjectRoleWorker grants task execution rights but not orchestration.
	ProjectRoleWorker ProjectRole = "worker"

	// ProjectRoleObserver grants read-only visibility into project state.
	ProjectRoleObserver ProjectRole = "observer"

	// ProjectRoleDenied explicitly blocks the agent from all project access.
	ProjectRoleDenied ProjectRole = "denied"
)

// validProjectRoles is the set of valid project roles for O(1) validation.
var validProjectRoles = map[ProjectRole]struct{}{
	ProjectRoleLead:     {},
	ProjectRoleWorker:   {},
	ProjectRoleObserver: {},
	ProjectRoleDenied:   {},
}

// ProjectQuotas defines resource limits for work done under a project.
type ProjectQuotas struct {
	// TokenBudget is the total LLM token budget (0 = unlimited).
	TokenBudget int64 `json:"token_budget,omitempty" yaml:"token_budget,omitempty"`

	// GPUMinutes is the total GPU compute budget in minutes (0 = unlimited).
	GPUMinutes int64 `json:"gpu_minutes,omitempty" yaml:"gpu_minutes,omitempty"`

	// MaxConcurrency is the maximum number of concurrent workflow runs (0 = unlimited).
	MaxConcurrency int `json:"max_concurrency,omitempty" yaml:"max_concurrency,omitempty"`
}
