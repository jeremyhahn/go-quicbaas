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

import (
	"errors"
	"strings"
	"time"
)

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

	// Storage is the storage tier the project's sandboxed checkouts are
	// mounted on, and the sizes that tier needs. The zero value keeps the
	// host bind mount every project has always had — see ProjectStorage and
	// workspace_storage.go.
	Storage ProjectStorage `json:"storage,omitempty" yaml:"storage,omitempty"`

	// HostAffinity is the preferred agent host for project operations (optional).
	HostAffinity string `json:"host_affinity,omitempty" yaml:"host_affinity,omitempty"`

	// DefaultLeadAgent is the Agent or pool that leads a job started under
	// this project when the job names no lead of its own. Its Agents must
	// declare the lead role.
	DefaultLeadAgent AgentRef `json:"default_lead_agent,omitempty" yaml:"default_lead_agent,omitempty"`

	// DefaultWorkflow names the tenant-scoped workflow a Task created under
	// this project is auto-spawned into. Empty means a Task is never
	// auto-spawned. A set-but-unknown name is refused on create and update.
	DefaultWorkflow string `json:"default_workflow,omitempty" yaml:"default_workflow,omitempty"`

	// Autonomous turns autonomous development on for this project: the
	// task-source poller adopts work for it and the dispatcher starts jobs
	// for it, bounded by Quotas.MaxConcurrency. False leaves the project
	// entirely operator-driven — tasks are created and started by hand.
	Autonomous bool `json:"autonomous,omitempty" yaml:"autonomous,omitempty"`

	// Parked closes the project's intake while leaving everything already in
	// flight to finish: no task that has not started is dispatched, no issue
	// is adopted or triaged, and every job serving started work runs to its
	// normal end. It is independent of Autonomous; see project_park.go.
	Parked bool `json:"parked,omitempty" yaml:"parked,omitempty"`

	// ParkedAt is when the project was parked, zero while it is not.
	ParkedAt time.Time `json:"parked_at,omitzero" yaml:"parked_at,omitempty"`

	// ParkedBy names the principal that parked the project, empty while it
	// is not parked or when the request carried no identity.
	ParkedBy string `json:"parked_by,omitempty" yaml:"parked_by,omitempty"`

	// Git contains the repository metadata.
	Git ProjectGit `json:"git" yaml:"git"`

	// ContextRepos names the repositories the worker clones beside the
	// project checkout. The daemon never clones them; see ContextRepo.
	ContextRepos []ContextRepo `json:"context_repos,omitempty" yaml:"context_repos,omitempty"`

	// Provisioning tracks server-managed workspace provisioning. The zero
	// value describes a bring-your-own-path project the server never
	// provisions.
	Provisioning ProjectProvisioning `json:"provisioning" yaml:"provisioning"`

	// Permissions defines the per-agent role matrix for this project.
	Permissions ProjectPermissions `json:"permissions" yaml:"permissions"`

	// Quotas defines resource limits for work done under this project.
	Quotas ProjectQuotas `json:"quotas" yaml:"quotas"`

	// Spend is the project's own limits: sliding windows, in tokens and/or
	// list-price dollars, over every turn charged to the project — pooled and
	// unpooled alike, from the project quota plane. The zero value meters
	// nothing. A project over a window is DEFERRED at admission rather than
	// refused mid-job (go-quicbaas#1058, ruling F5).
	Spend SpendLimits `json:"spend,omitzero" yaml:"spend,omitempty"`

	// SessionRotation is what a persistent worker session does when the
	// pooled provider account under it rotates: resume the same conversation,
	// or start a fresh one on the same branch. Empty is
	// SessionRotationResume; see session_rotation.go.
	SessionRotation SessionRotationPolicy `json:"session_rotation,omitempty" yaml:"session_rotation,omitempty"`

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
	FleetNodeID string `json:"agent_id" yaml:"agent_id"`

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

	// Author is the identity recorded on commits made from this workspace.
	// Required only when the workspace commits; see GitAuthor.
	Author GitAuthor `json:"author,omitzero" yaml:"author,omitempty"`

	// Signing decides whether and how commits made from this workspace are
	// signed. The zero value signs nothing, explicitly; see CommitSigning.
	Signing CommitSigning `json:"signing,omitzero" yaml:"signing,omitempty"`

	// SSHKey references the KMS-held key an ssh:// RepoURL is reached with.
	// Empty means the signing key is used; see AccessKey.
	SSHKey KMSKeyRef `json:"ssh_key,omitzero" yaml:"ssh_key,omitempty"`
}

// ProjectProvisioningState is the lifecycle state of a server-managed
// project workspace.
type ProjectProvisioningState string

const (
	// ProvisioningStatePending means the workspace directory exists but the
	// git clone has not started yet.
	ProvisioningStatePending ProjectProvisioningState = "pending"

	// ProvisioningStateCloning means the git clone is in progress.
	ProvisioningStateCloning ProjectProvisioningState = "cloning"

	// ProvisioningStateReady means the workspace is fully provisioned and
	// sessions may run in it.
	ProvisioningStateReady ProjectProvisioningState = "ready"

	// ProvisioningStateFailed means provisioning failed; see
	// ProjectProvisioning.Error for the reason.
	ProvisioningStateFailed ProjectProvisioningState = "failed"
)

// ProjectProvisioning tracks server-managed workspace provisioning on a
// project record. Credential material is never stored here — credential
// references live on the create request and are resolved at clone time only.
type ProjectProvisioning struct {
	// Managed is true when the server owns the workspace directory
	// lifecycle (provision on create, teardown on delete).
	Managed bool `json:"managed,omitempty" yaml:"managed,omitempty"`

	// State is the provisioning lifecycle state. Empty for unmanaged
	// projects.
	State ProjectProvisioningState `json:"state,omitempty" yaml:"state,omitempty"`

	// Error carries the failure reason when State is failed.
	Error string `json:"error,omitempty" yaml:"error,omitempty"`

	// UpdatedAt is the time of the last state transition.
	UpdatedAt time.Time `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
}

// ProjectWorkspaceNotReadyError reports a spawn attempt against a managed
// workspace that is not in the ready state.
type ProjectWorkspaceNotReadyError struct {
	// TenantID owns the project.
	TenantID string

	// ProjectID identifies the project whose workspace is not ready.
	ProjectID string

	// State is the workspace's current provisioning state.
	State ProjectProvisioningState

	// Reason carries the provisioning error when State is failed.
	Reason string
}

// Error implements the error interface.
func (e *ProjectWorkspaceNotReadyError) Error() string {
	msg := "project workspace not ready: tenant=" + e.TenantID +
		" project=" + e.ProjectID + " state=" + string(e.State)
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	return msg
}

// ProjectPermissions holds the per-agent role matrix for a project.
type ProjectPermissions struct {
	// FleetNodeRoles maps agent_id to its ProjectRole within the project.
	FleetNodeRoles map[string]ProjectRole `json:"agent_roles,omitempty" yaml:"agent_roles,omitempty"`
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

// ProjectQuotas defines resource limits for work done under a project. The
// project's SPEND limits are Project.Spend: sliding token and dollar windows
// the ledger enforces (go-quicbaas#1058). The lifetime TokenBudget that used to
// live here was never enforced by anything and was removed.
type ProjectQuotas struct {
	// GPUMinutes is the total GPU compute budget in minutes (0 = unlimited).
	GPUMinutes int64 `json:"gpu_minutes,omitempty" yaml:"gpu_minutes,omitempty"`

	// MaxConcurrency is the maximum number of concurrent workflow runs (0 = unlimited).
	MaxConcurrency int `json:"max_concurrency,omitempty" yaml:"max_concurrency,omitempty"`
}

// =============================================================================
// Context repositories
// =============================================================================
//
// A CONTEXT REPOSITORY IS CLONED BY THE WORKER, NEVER BY THE DAEMON. The
// project record names what the worker needs beside the checkout — the
// company handbook, a spec repo, a template collection — and the daemon hands
// that list to the container at spawn as a spec. The container clones the
// project and every context repository fresh, side by side, at start. Nothing
// on this record is a path on the daemon's filesystem, because the daemon has
// no copy to point at.

// ContextRepo names one repository the worker clones beside the project.
type ContextRepo struct {
	// Name is the directory the repository is cloned into, beside the
	// project checkout. One path segment: it is joined, never resolved.
	Name string `json:"name" yaml:"name"`

	// RepoURL is the remote the worker clones from.
	RepoURL string `json:"repo_url" yaml:"repo_url"`

	// Ref is the branch, tag or commit the worker checks out.
	Ref string `json:"ref" yaml:"ref"`
}

// contextRepoForbiddenInName is what may not appear in a context repository's
// directory name: the two path separators and the NUL byte.
const contextRepoForbiddenInName = "/\\\x00"

// Validate checks one context repository can be cloned into its own
// directory beside the project.
func (r ContextRepo) Validate() error {
	switch {
	case r.Name == "":
		return &ContextRepoInvalidError{Field: "name", Message: "a context repository is cloned into a named directory"}
	case r.Name == "." || r.Name == "..":
		return &ContextRepoInvalidError{Name: r.Name, Field: "name", Message: "must name a directory, not a relative path"}
	case strings.ContainsAny(r.Name, contextRepoForbiddenInName):
		return &ContextRepoInvalidError{Name: r.Name, Field: "name", Message: "must be one path segment, with no separator"}
	case r.RepoURL == "":
		return &ContextRepoInvalidError{Name: r.Name, Field: "repo_url", Message: "names the remote the worker clones"}
	case r.Ref == "":
		return &ContextRepoInvalidError{Name: r.Name, Field: "ref", Message: "names the branch, tag or commit the worker checks out"}
	}
	return nil
}

// ValidateContextRepos checks every repository in the list and refuses two
// that would occupy the same directory.
func ValidateContextRepos(repos []ContextRepo) error {
	seen := make(map[string]struct{}, len(repos))
	for _, repo := range repos {
		if err := repo.Validate(); err != nil {
			return err
		}
		if _, duplicate := seen[repo.Name]; duplicate {
			return &ContextRepoInvalidError{
				Name:    repo.Name,
				Field:   "name",
				Message: "duplicates an earlier context repository; two cannot occupy one directory",
			}
		}
		seen[repo.Name] = struct{}{}
	}
	return nil
}

// ErrContextRepoInvalid indicates a context repository the worker could not
// clone beside the project.
var ErrContextRepoInvalid = errors.New("project: invalid context repository")

// ContextRepoInvalidError reports a context repository the project refused.
type ContextRepoInvalidError struct {
	// Name is the repository's directory name, empty when that is what is
	// wrong.
	Name string

	// Field is the offending field.
	Field string

	// Message says what the field must be.
	Message string
}

// Error implements the error interface.
func (e *ContextRepoInvalidError) Error() string {
	msg := ErrContextRepoInvalid.Error() + ": "
	if e.Name != "" {
		msg += e.Name + ": "
	}
	return msg + e.Field + " " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ContextRepoInvalidError) Unwrap() error { return ErrContextRepoInvalid }
