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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// provisioningWaitBudget bounds how long a test waits for a background
// provisioning step before declaring it hung.
const provisioningWaitBudget = 60 * time.Second

// newManagedProjectService builds a ProjectService with a wired workspace
// provisioner rooted in a test tmpdir, sharing one in-memory project store.
func newManagedProjectService(t *testing.T) (ProjectService, *ProjectWorkspaceProvisioner, string) {
	t.Helper()

	store := orchestration.NewInMemoryProjectStore()
	svc := NewProjectService(store)

	root := t.TempDir()
	workspaces, err := orchestration.NewWorkspaceManager(root)
	if err != nil {
		t.Fatalf("NewWorkspaceManager error: %v", err)
	}
	provisioner, err := NewProjectWorkspaceProvisioner(ProjectWorkspaceProvisionerConfig{
		Workspaces: workspaces,
		Git:        &GitProvisioner{},
		Store:      store,
	})
	if err != nil {
		t.Fatalf("NewProjectWorkspaceProvisioner error: %v", err)
	}

	wirer, ok := svc.(interface {
		SetWorkspaceProvisioner(*ProjectWorkspaceProvisioner)
	})
	if !ok {
		t.Fatal("project service does not implement SetWorkspaceProvisioner")
	}
	wirer.SetWorkspaceProvisioner(provisioner)
	return svc, provisioner, root
}

// waitProvisioned blocks until the project's background provisioning step
// completes, then returns the fresh project record.
func waitProvisioned(t *testing.T, svc ProjectService, provisioner *ProjectWorkspaceProvisioner, tenantID, projectID string) *orchestration.Project {
	t.Helper()
	select {
	case <-provisioner.ProvisioningDone(tenantID, projectID):
	case <-time.After(provisioningWaitBudget):
		t.Fatal("timed out waiting for background provisioning")
	}
	resp, err := svc.GetProject(context.Background(), &GetProjectRequest{TenantID: tenantID, ProjectID: projectID})
	if err != nil {
		t.Fatalf("GetProject error: %v", err)
	}
	return resp.Project
}

func TestNewProjectWorkspaceProvisioner_MissingDependencies(t *testing.T) {
	t.Parallel()

	workspaces, err := orchestration.NewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspaceManager error: %v", err)
	}
	store := orchestration.NewInMemoryProjectStore()

	if _, err := NewProjectWorkspaceProvisioner(ProjectWorkspaceProvisionerConfig{Store: store}); err == nil {
		t.Error("expected error without workspace manager")
	}
	if _, err := NewProjectWorkspaceProvisioner(ProjectWorkspaceProvisionerConfig{Workspaces: workspaces}); err == nil {
		t.Error("expected error without project store")
	}
}

func TestCreateProject_ManagedWithoutGit_ReadyImmediately(t *testing.T) {
	t.Parallel()

	svc, _, root := newManagedProjectService(t)
	resp, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID:         "tenant-a",
		Name:             "managed-no-git",
		WorkspaceManaged: true,
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}

	project := resp.Project
	if !project.Provisioning.Managed {
		t.Error("Provisioning.Managed = false, want true")
	}
	if project.Provisioning.State != orchestration.ProvisioningStateReady {
		t.Errorf("Provisioning.State = %q, want ready", project.Provisioning.State)
	}
	wantPath := filepath.Join(root, "tenant-a", project.ID, "workspace")
	if project.Workspace.Path != wantPath {
		t.Errorf("Workspace.Path = %q, want %q", project.Workspace.Path, wantPath)
	}
	info, statErr := os.Stat(wantPath)
	if statErr != nil {
		t.Fatalf("workspace not provisioned: %v", statErr)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("workspace perm = %o, want 0700", perm)
	}
}

func TestCreateProject_ManagedWithGit_ClonesInBackground(t *testing.T) {
	t.Parallel()

	repoURL, wantSHA, _ := initLocalGitRepo(t)
	svc, provisioner, _ := newManagedProjectService(t)

	resp, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID:         "tenant-a",
		Name:             "managed-git",
		WorkspaceManaged: true,
		GitRepoURL:       repoURL,
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}
	// Create returns immediately with the tracked state, not a finished clone.
	if got := resp.Project.Provisioning.State; got != orchestration.ProvisioningStatePending {
		t.Errorf("create-time Provisioning.State = %q, want pending", got)
	}

	project := waitProvisioned(t, svc, provisioner, "tenant-a", resp.Project.ID)
	if project.Provisioning.State != orchestration.ProvisioningStateReady {
		t.Fatalf("final Provisioning.State = %q (error: %s), want ready",
			project.Provisioning.State, project.Provisioning.Error)
	}
	if project.Git.CurrentSHA != wantSHA {
		t.Errorf("Git.CurrentSHA = %q, want %q", project.Git.CurrentSHA, wantSHA)
	}
	if _, statErr := os.Stat(filepath.Join(project.Workspace.Path, "README.md")); statErr != nil {
		t.Errorf("cloned file missing from workspace: %v", statErr)
	}
}

func TestCreateProject_ManagedWithBadGitURL_Fails(t *testing.T) {
	t.Parallel()

	svc, provisioner, _ := newManagedProjectService(t)
	resp, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID:         "tenant-a",
		Name:             "managed-bad-git",
		WorkspaceManaged: true,
		GitRepoURL:       "file://" + filepath.Join(t.TempDir(), "missing-repo"),
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}

	project := waitProvisioned(t, svc, provisioner, "tenant-a", resp.Project.ID)
	if project.Provisioning.State != orchestration.ProvisioningStateFailed {
		t.Fatalf("final Provisioning.State = %q, want failed", project.Provisioning.State)
	}
	if !strings.Contains(project.Provisioning.Error, "clone") {
		t.Errorf("Provisioning.Error = %q, want a clone failure", project.Provisioning.Error)
	}
	if project.Git.CurrentSHA != "" {
		t.Errorf("Git.CurrentSHA = %q, want empty on failure", project.Git.CurrentSHA)
	}
}

func TestCreateProject_WorkspaceRequestValidation(t *testing.T) {
	t.Parallel()

	svc, _, _ := newManagedProjectService(t)
	tests := []struct {
		name string
		req  *CreateProjectRequest
	}{
		{"managed with explicit path", &CreateProjectRequest{
			TenantID:         "tenant-a",
			Name:             "p",
			WorkspaceManaged: true,
			WorkspacePath:    "/somewhere",
		}},
		{"both credential kinds", &CreateProjectRequest{
			TenantID:               "tenant-a",
			Name:                   "p",
			WorkspaceManaged:       true,
			GitRepoURL:             "https://example.com/r.git",
			GitCredentialSecretRef: "sec-1",
			GitSSHKeyName:          "key-1",
		}},
		{"credentials without repo url", &CreateProjectRequest{
			TenantID:               "tenant-a",
			Name:                   "p",
			WorkspaceManaged:       true,
			GitCredentialSecretRef: "sec-1",
		}},
		{"credentials without managed workspace", &CreateProjectRequest{
			TenantID:      "tenant-a",
			Name:          "p",
			GitRepoURL:    "git@github.com:org/repo.git",
			GitSSHKeyName: "key-1",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := svc.CreateProject(context.Background(), tt.req)
			var invalid *ProjectInvalidError
			if !errors.As(err, &invalid) {
				t.Fatalf("expected ProjectInvalidError, got %T: %v", err, err)
			}
		})
	}
}

func TestCreateProject_ManagedWithoutProvisioner_Unavailable(t *testing.T) {
	t.Parallel()

	svc := NewProjectService(orchestration.NewInMemoryProjectStore())
	_, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID:         "tenant-a",
		Name:             "p",
		WorkspaceManaged: true,
	})
	var unavailable *ProjectProvisioningUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("expected ProjectProvisioningUnavailableError, got %T: %v", err, err)
	}
}

func TestCreateProject_ManagedGitWithoutGitProvisioner_Unavailable(t *testing.T) {
	t.Parallel()

	store := orchestration.NewInMemoryProjectStore()
	svc := NewProjectService(store)
	workspaces, err := orchestration.NewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspaceManager error: %v", err)
	}
	provisioner, err := NewProjectWorkspaceProvisioner(ProjectWorkspaceProvisionerConfig{
		Workspaces: workspaces,
		Store:      store,
		// no Git provisioner
	})
	if err != nil {
		t.Fatalf("NewProjectWorkspaceProvisioner error: %v", err)
	}
	svc.(interface {
		SetWorkspaceProvisioner(*ProjectWorkspaceProvisioner)
	}).SetWorkspaceProvisioner(provisioner)

	_, err = svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID:         "tenant-a",
		Name:             "p",
		WorkspaceManaged: true,
		GitRepoURL:       "https://example.com/r.git",
	})
	var unavailable *ProjectProvisioningUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("expected ProjectProvisioningUnavailableError, got %T: %v", err, err)
	}
}

func TestCreateProject_BringYourOwnPath_NotProvisioned(t *testing.T) {
	t.Parallel()

	svc, _, root := newManagedProjectService(t)
	resp, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID:      "tenant-a",
		Name:          "byo",
		WorkspacePath: "/opt/existing/checkout",
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}
	if resp.Project.Provisioning.Managed {
		t.Error("bring-your-own-path project marked managed")
	}
	if resp.Project.Workspace.Path != "/opt/existing/checkout" {
		t.Errorf("Workspace.Path = %q, want caller's path", resp.Project.Workspace.Path)
	}
	// Nothing was provisioned under the projects root.
	entries, readErr := os.ReadDir(root)
	if readErr != nil {
		t.Fatalf("ReadDir error: %v", readErr)
	}
	if len(entries) != 0 {
		t.Errorf("projects root not empty: %v", entries)
	}
}

func TestDeleteProject_ManagedWorkspaceTornDown(t *testing.T) {
	t.Parallel()

	svc, _, root := newManagedProjectService(t)
	resp, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID:         "tenant-a",
		Name:             "managed-delete",
		WorkspaceManaged: true,
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}
	projectDir := filepath.Join(root, "tenant-a", resp.Project.ID)
	if _, statErr := os.Stat(projectDir); statErr != nil {
		t.Fatalf("project dir missing before delete: %v", statErr)
	}

	if _, err := svc.DeleteProject(context.Background(), &DeleteProjectRequest{
		TenantID:  "tenant-a",
		ProjectID: resp.Project.ID,
	}); err != nil {
		t.Fatalf("DeleteProject error: %v", err)
	}

	if _, statErr := os.Stat(projectDir); !os.IsNotExist(statErr) {
		t.Errorf("project dir still exists after delete (stat err: %v)", statErr)
	}
	_, err = svc.GetProject(context.Background(), &GetProjectRequest{
		TenantID:  "tenant-a",
		ProjectID: resp.Project.ID,
	})
	var notFound *ProjectNotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("expected ProjectNotFoundError after delete, got %T: %v", err, err)
	}
}

func TestDeleteProject_UnmanagedWorkspaceUntouched(t *testing.T) {
	t.Parallel()

	svc, _, _ := newManagedProjectService(t)
	byoDir := t.TempDir()
	marker := filepath.Join(byoDir, "keep-me")
	if err := os.WriteFile(marker, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	resp, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID:      "tenant-a",
		Name:          "byo-delete",
		WorkspacePath: byoDir,
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}
	if _, err := svc.DeleteProject(context.Background(), &DeleteProjectRequest{
		TenantID:  "tenant-a",
		ProjectID: resp.Project.ID,
	}); err != nil {
		t.Fatalf("DeleteProject error: %v", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Errorf("bring-your-own workspace was touched by delete: %v", statErr)
	}
}

func TestProvisioningDone_UntrackedProjectIsClosed(t *testing.T) {
	t.Parallel()

	_, provisioner, _ := newManagedProjectService(t)
	select {
	case <-provisioner.ProvisioningDone("tenant-a", "never-created"):
		// already closed — correct
	default:
		t.Fatal("ProvisioningDone for an untracked project must be closed")
	}
}

func TestStartClone_InvalidInputs(t *testing.T) {
	t.Parallel()

	_, provisioner, _ := newManagedProjectService(t)

	if err := provisioner.StartClone(context.Background(), nil, &ProjectGitSpec{RepoURL: "x"}); err == nil {
		t.Error("expected error for nil project")
	}
	if err := provisioner.StartClone(context.Background(), &orchestration.Project{}, nil); err == nil {
		t.Error("expected error for nil spec")
	}
	if err := provisioner.StartClone(context.Background(), &orchestration.Project{}, &ProjectGitSpec{}); err == nil {
		t.Error("expected error for empty repo URL")
	}
}
