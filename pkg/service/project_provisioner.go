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
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// defaultCloneTimeout bounds one background git clone so a hung remote can
// never pin a provisioning goroutine forever.
const defaultCloneTimeout = 10 * time.Minute

// ProjectProvisioningUnavailableError reports a project request that needs
// workspace provisioning on a server where it is not wired.
type ProjectProvisioningUnavailableError struct {
	// Reason states which capability is missing.
	Reason string
}

// Error implements the error interface.
func (e *ProjectProvisioningUnavailableError) Error() string {
	return "project provisioning unavailable: " + e.Reason
}

// ProjectGitSpec carries the clone parameters CreateProject hands to the
// provisioner. Credential fields are references only; the material they name
// is resolved inside the clone and never persisted.
type ProjectGitSpec struct {
	// RepoURL is the repository to clone.
	RepoURL string

	// Branch is the branch to clone. Empty clones the remote default.
	Branch string

	// TokenSecretRef names an HTTPS token secret in the secrets store.
	TokenSecretRef string

	// TokenUsername is the username paired with the HTTPS token.
	TokenUsername string

	// SSHKey references an xkms-held SSH key.
	SSHKey SSHKeyRef
}

// ProjectWorkspaceProvisioner provisions managed project workspaces and runs
// git-clone provisioning as a tracked background step. Directory lifecycle is
// delegated to the orchestration WorkspaceManager; clones are delegated to
// the GitProvisioner; state transitions are persisted on the project record
// so every observer (spawn gating included) sees the same provisioning state.
type ProjectWorkspaceProvisioner struct {
	workspaces *orchestration.WorkspaceManager
	git        *GitProvisioner
	store      orchestration.ProjectStore
	logger     interfaces.Logger

	// cloneTimeout bounds one background clone. Zero selects
	// defaultCloneTimeout.
	cloneTimeout time.Duration

	// mu guards done, the per-project completion channels that let callers
	// (and tests) observe background provisioning without polling.
	mu   sync.Mutex
	done map[string]chan struct{}
}

// ProjectWorkspaceProvisionerConfig configures NewProjectWorkspaceProvisioner.
type ProjectWorkspaceProvisionerConfig struct {
	// Workspaces manages the managed-workspace directory tree. Required.
	Workspaces *orchestration.WorkspaceManager

	// Git clones project repositories. Required only for projects that carry
	// a repository URL; a provisioner without it still manages directories.
	Git *GitProvisioner

	// Store persists provisioning state transitions onto project records.
	// Required.
	Store orchestration.ProjectStore

	// Logger receives background provisioning diagnostics. Optional.
	Logger interfaces.Logger

	// CloneTimeout bounds one background clone. Zero selects
	// defaultCloneTimeout.
	CloneTimeout time.Duration
}

// NewProjectWorkspaceProvisioner builds a provisioner from cfg.
func NewProjectWorkspaceProvisioner(cfg ProjectWorkspaceProvisionerConfig) (*ProjectWorkspaceProvisioner, error) {
	if cfg.Workspaces == nil {
		return nil, &ProjectProvisioningUnavailableError{Reason: "workspace manager is required"}
	}
	if cfg.Store == nil {
		return nil, &ProjectProvisioningUnavailableError{Reason: "project store is required"}
	}
	return &ProjectWorkspaceProvisioner{
		workspaces:   cfg.Workspaces,
		git:          cfg.Git,
		store:        cfg.Store,
		logger:       cfg.Logger,
		cloneTimeout: cfg.CloneTimeout,
		done:         make(map[string]chan struct{}),
	}, nil
}

// Provision creates the managed workspace directory for the project and
// returns its path. Idempotent.
func (p *ProjectWorkspaceProvisioner) Provision(tenantID, projectID string) (string, error) {
	return p.workspaces.Provision(tenantID, projectID)
}

// Teardown removes the project's managed directory tree. Idempotent.
func (p *ProjectWorkspaceProvisioner) Teardown(tenantID, projectID string) error {
	return p.workspaces.Remove(tenantID, projectID)
}

// CanClone reports whether the provisioner was wired with a git provisioner.
func (p *ProjectWorkspaceProvisioner) CanClone() bool {
	return p.git != nil
}

// StartClone launches the background clone for a freshly created managed
// project. The work runs on a context detached from the caller's request
// (the create RPC returns immediately) and bounded by the clone timeout.
// State transitions — cloning, then ready or failed — are persisted on the
// project record; the resulting HEAD SHA lands in Git.CurrentSHA.
func (p *ProjectWorkspaceProvisioner) StartClone(ctx context.Context, project *orchestration.Project, spec *ProjectGitSpec) error {
	if p.git == nil {
		return &ProjectProvisioningUnavailableError{Reason: "git provisioner is not configured"}
	}
	if project == nil || spec == nil || spec.RepoURL == "" {
		return &ProjectProvisioningUnavailableError{Reason: "a project and a repository URL are required"}
	}

	tenantID, projectID := project.TenantID, project.ID
	workspace := project.Workspace.Path
	cloneReq := &GitCloneRequest{
		RepoURL: spec.RepoURL,
		Branch:  spec.Branch,
		Dir:     workspace,
		GitCredentials: GitCredentials{
			TenantID:       tenantID,
			TokenSecretRef: spec.TokenSecretRef,
			TokenUsername:  spec.TokenUsername,
			SSHKey:         spec.SSHKey,
		},
	}

	doneCh := p.registerDone(tenantID, projectID)

	// The clone must survive the create RPC returning: detach from the
	// request context (values are preserved, cancellation is not) and bound
	// the work with the provisioner's own timeout instead. This mirrors the
	// orchestrator's dispatchTurn pattern.
	cloneCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.timeout())
	go func() {
		defer cancel()
		defer close(doneCh)
		p.runClone(cloneCtx, tenantID, projectID, cloneReq)
	}()
	return nil
}

// ProvisioningDone returns a channel closed when the project's background
// provisioning step finishes (ready or failed). A project with no tracked
// step in flight gets an already-closed channel.
func (p *ProjectWorkspaceProvisioner) ProvisioningDone(tenantID, projectID string) <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ch, ok := p.done[tenantID+"/"+projectID]; ok {
		return ch
	}
	closed := make(chan struct{})
	close(closed)
	return closed
}

// registerDone creates and registers the completion channel for one clone.
func (p *ProjectWorkspaceProvisioner) registerDone(tenantID, projectID string) chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	ch := make(chan struct{})
	p.done[tenantID+"/"+projectID] = ch
	return ch
}

// runClone performs the tracked clone and persists both state transitions.
func (p *ProjectWorkspaceProvisioner) runClone(ctx context.Context, tenantID, projectID string, req *GitCloneRequest) {
	p.transition(ctx, tenantID, projectID, func(project *orchestration.Project) {
		project.Provisioning.State = orchestration.ProvisioningStateCloning
		project.Provisioning.Error = ""
	})

	sha, err := p.git.Clone(ctx, req)
	if err != nil {
		p.warnf("project %s/%s clone failed: %v", tenantID, projectID, err)
		p.transition(ctx, tenantID, projectID, func(project *orchestration.Project) {
			project.Provisioning.State = orchestration.ProvisioningStateFailed
			project.Provisioning.Error = err.Error()
		})
		return
	}

	p.transition(ctx, tenantID, projectID, func(project *orchestration.Project) {
		project.Provisioning.State = orchestration.ProvisioningStateReady
		project.Provisioning.Error = ""
		project.Git.CurrentSHA = sha
	})
}

// transition applies a read-modify-write state mutation to the stored project
// record. A record that disappeared mid-provisioning (deleted while cloning)
// is not an error worth failing on — the teardown owns the directory.
func (p *ProjectWorkspaceProvisioner) transition(ctx context.Context, tenantID, projectID string, mutate func(*orchestration.Project)) {
	project, err := p.store.Get(ctx, tenantID, projectID)
	if err != nil {
		p.warnf("project %s/%s provisioning state read failed: %v", tenantID, projectID, err)
		return
	}
	mutate(project)
	project.Provisioning.UpdatedAt = time.Now().UTC()
	if err := p.store.Update(ctx, project); err != nil {
		p.warnf("project %s/%s provisioning state write failed: %v", tenantID, projectID, err)
	}
}

// timeout returns the effective clone timeout.
func (p *ProjectWorkspaceProvisioner) timeout() time.Duration {
	if p.cloneTimeout > 0 {
		return p.cloneTimeout
	}
	return defaultCloneTimeout
}

// warnf logs a background diagnostic when a logger is wired.
func (p *ProjectWorkspaceProvisioner) warnf(format string, args ...interface{}) {
	if p.logger != nil {
		p.logger.Warnf(format, args...)
	}
}
