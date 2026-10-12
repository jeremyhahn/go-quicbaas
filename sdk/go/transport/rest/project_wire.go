// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
)

// projectWire mirrors the server's flat REST project record
// (pkg/api/rest.ProjectRESTResponse). The REST API renders a project flat —
// workspace_path, git_repo_url, agent_roles — while project.ProjectInfo is
// nested, so decoding the record straight into ProjectInfo loses every nested
// field. toInfo is the one place the two shapes meet.
type projectWire struct {
	ID                   string                         `json:"id"`
	TenantID             string                         `json:"tenant_id"`
	Name                 string                         `json:"name"`
	Description          string                         `json:"description"`
	WorkspaceFleetNodeID string                         `json:"workspace_agent_id"`
	WorkspacePath        string                         `json:"workspace_path"`
	HostAffinity         string                         `json:"host_affinity"`
	DefaultLeadAgent     agents.AgentRef                `json:"default_lead_agent"`
	GitRepoURL           string                         `json:"git_repo_url"`
	GitDefaultBranch     string                         `json:"git_default_branch"`
	GitCurrentSHA        string                         `json:"git_current_sha"`
	FleetNodeRoles       map[string]project.ProjectRole `json:"agent_roles"`
	Spend                spend.Limits                   `json:"spend"`
	GPUMinutes           int64                          `json:"gpu_minutes"`
	MaxConcurrency       int                            `json:"max_concurrency"`
	Metadata             map[string]string              `json:"metadata"`
	CreatedAt            time.Time                      `json:"created_at"`
	UpdatedAt            time.Time                      `json:"updated_at"`

	DefaultWorkflow string                `json:"default_workflow"`
	Autonomous      bool                  `json:"autonomous"`
	Parked          bool                  `json:"parked"`
	ParkedAt        time.Time             `json:"parked_at"`
	ParkedBy        string                `json:"parked_by"`
	ContextRepos    []project.ContextRepo `json:"context_repos"`

	GitAuthorName       string                      `json:"git_author_name"`
	GitAuthorEmail      string                      `json:"git_author_email"`
	GitSigningEnabled   bool                        `json:"git_signing_enabled"`
	GitSigningAlgorithm string                      `json:"git_signing_algorithm"`
	GitSigningKeyName   string                      `json:"git_signing_key_name"`
	GitSigningKeyScope  string                      `json:"git_signing_key_scope"`
	GitSigningKeyOwner  string                      `json:"git_signing_key_owner"`
	GitSSHKeyName       string                      `json:"git_ssh_key_name"`
	GitSSHKeyScope      string                      `json:"git_ssh_key_scope"`
	GitSSHKeyOwner      string                      `json:"git_ssh_key_owner"`
	GitSigningRetired   []project.RetiredSigningKey `json:"git_signing_retired"`

	WorkspaceTier     project.WorkspaceTier         `json:"workspace_tier"`
	WorkspaceTmpfsMB  int                           `json:"workspace_tmpfs_mb"`
	ContainerMemoryMB int                           `json:"container_memory_mb"`
	SessionRotation   project.SessionRotationPolicy `json:"session_rotation"`

	WorkspaceManaged      bool                      `json:"workspace_managed"`
	ProvisioningState     project.ProvisioningState `json:"provisioning_state"`
	ProvisioningError     string                    `json:"provisioning_error"`
	ProvisioningUpdatedAt time.Time                 `json:"provisioning_updated_at"`
}

// projectEnvelopeWire is the {project, timestamp} body the server answers a
// create, get, get-by-name and update with.
type projectEnvelopeWire struct {
	Project *projectWire `json:"project"`
}

// projectListWire is the body of GET /api/v1/projects: flat projects plus
// the paging envelope.
type projectListWire struct {
	Projects []*projectWire `json:"projects"`
	page.Envelope
}

// toInfo nests the flat wire record into the SDK's ProjectInfo.
func (w *projectWire) toInfo() *project.ProjectInfo {
	return &project.ProjectInfo{
		ID:          w.ID,
		TenantID:    w.TenantID,
		Name:        w.Name,
		Description: w.Description,
		Workspace: project.ProjectWorkspace{
			FleetNodeID: w.WorkspaceFleetNodeID,
			Path:        w.WorkspacePath,
		},
		Storage: project.ProjectStorage{
			WorkspaceTier:     w.WorkspaceTier,
			WorkspaceTmpfsMB:  w.WorkspaceTmpfsMB,
			ContainerMemoryMB: w.ContainerMemoryMB,
		},
		HostAffinity:     w.HostAffinity,
		DefaultLeadAgent: w.DefaultLeadAgent,
		DefaultWorkflow:  w.DefaultWorkflow,
		Autonomous:       w.Autonomous,
		Parked:           w.Parked,
		ParkedAt:         w.ParkedAt,
		ParkedBy:         w.ParkedBy,
		ContextRepos:     w.ContextRepos,
		Git: project.ProjectGit{
			RepoURL:       w.GitRepoURL,
			DefaultBranch: w.GitDefaultBranch,
			CurrentSHA:    w.GitCurrentSHA,
			Author:        project.GitAuthor{Name: w.GitAuthorName, Email: w.GitAuthorEmail},
			Signing: project.CommitSigning{
				Enabled:   w.GitSigningEnabled,
				Algorithm: w.GitSigningAlgorithm,
				Key:       project.KMSKeyRef{Name: w.GitSigningKeyName, Scope: w.GitSigningKeyScope, Owner: w.GitSigningKeyOwner},
				Retired:   w.GitSigningRetired,
			},
			SSHKey: project.KMSKeyRef{Name: w.GitSSHKeyName, Scope: w.GitSSHKeyScope, Owner: w.GitSSHKeyOwner},
		},
		Provisioning: project.ProjectProvisioning{
			Managed:   w.WorkspaceManaged,
			State:     w.ProvisioningState,
			Error:     w.ProvisioningError,
			UpdatedAt: w.ProvisioningUpdatedAt,
		},
		Permissions: project.ProjectPermissions{FleetNodeRoles: w.FleetNodeRoles},
		Quotas: project.ProjectQuotas{
			GPUMinutes:     w.GPUMinutes,
			MaxConcurrency: w.MaxConcurrency,
		},
		Spend:           w.Spend,
		SessionRotation: w.SessionRotation,
		Metadata:        w.Metadata,
		CreatedAt:       w.CreatedAt,
		UpdatedAt:       w.UpdatedAt,
	}
}
