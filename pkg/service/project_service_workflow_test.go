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
	"reflect"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Project.DefaultWorkflow — verified against the tenant-scoped registry
// =============================================================================

// newProjectServiceWithWorkflows wires a real WorkflowLoader carrying one
// workflow for tenant-1, so the service's registry lookup is the real thing.
func newProjectServiceWithWorkflows(t *testing.T) (ProjectService, *orchestration.WorkflowLoader) {
	t.Helper()
	loader := orchestration.NewWorkflowLoader()
	def := &orchestration.WorkflowDefinition{
		Name: "sdlc",
		Mode: orchestration.WorkflowModeAutonomous,
		Agents: []orchestration.AgentRoleSpec{
			{Role: "lead", Backend: "mock", Model: "mock-model"},
		},
	}
	if err := loader.Register("tenant-1", def); err != nil {
		t.Fatalf("Register: %v", err)
	}
	svc := NewProjectService(orchestration.NewInMemoryProjectStore())
	svc.(*projectServiceImpl).SetWorkflowResolver(loader)
	return svc, loader
}

func requireProjectInvalidField(t *testing.T, err error, field string) {
	t.Helper()
	var invalid *ProjectInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected *ProjectInvalidError, got %T: %v", err, err)
	}
	if invalid.Field != field {
		t.Fatalf("field = %q, want %q (%v)", invalid.Field, field, err)
	}
}

func TestProjectService_CreateProject_KnownDefaultWorkflowIsStored(t *testing.T) {
	svc, _ := newProjectServiceWithWorkflows(t)
	resp, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID: "tenant-1", Name: "api", DefaultWorkflow: "sdlc",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if resp.Project.DefaultWorkflow != "sdlc" {
		t.Fatalf("DefaultWorkflow = %q, want sdlc", resp.Project.DefaultWorkflow)
	}
}

func TestProjectService_CreateProject_UnknownDefaultWorkflowIsRefused(t *testing.T) {
	svc, _ := newProjectServiceWithWorkflows(t)
	_, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID: "tenant-1", Name: "api", DefaultWorkflow: "ghost",
	})
	requireReferenceNotFound(t, err, "default_workflow")
}

// TestProjectService_CreateProject_DefaultWorkflowIsTenantScoped: the name
// exists in tenant-1 and must not be visible to tenant-2.
func TestProjectService_CreateProject_DefaultWorkflowIsTenantScoped(t *testing.T) {
	svc, _ := newProjectServiceWithWorkflows(t)
	_, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID: "tenant-2", Name: "api", DefaultWorkflow: "sdlc",
	})
	requireReferenceNotFound(t, err, "default_workflow")
}

// TestProjectService_CreateProject_NoRegistryRefusesADefaultWorkflow pins the
// fail-closed choice: without a registry a name cannot be verified, and an
// unverified name is refused rather than stored.
func TestProjectService_CreateProject_NoRegistryRefusesADefaultWorkflow(t *testing.T) {
	svc := newTestProjectService()
	_, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID: "tenant-1", Name: "api", DefaultWorkflow: "sdlc",
	})
	requireReferenceUnavailable(t, err, "default_workflow")

	// An empty DefaultWorkflow never consults the registry.
	if _, err := svc.CreateProject(context.Background(), &CreateProjectRequest{TenantID: "tenant-1", Name: "plain"}); err != nil {
		t.Fatalf("a project without a default workflow was refused: %v", err)
	}
}

func TestProjectService_UpdateProject_DefaultWorkflow(t *testing.T) {
	svc, loader := newProjectServiceWithWorkflows(t)
	ctx := context.Background()
	created, err := svc.CreateProject(ctx, &CreateProjectRequest{TenantID: "tenant-1", Name: "api"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	id := created.Project.ID

	t.Run("unknown name refused", func(t *testing.T) {
		_, err := svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: id, DefaultWorkflow: "ghost"})
		requireReferenceNotFound(t, err, "default_workflow")
	})

	t.Run("known name stored", func(t *testing.T) {
		resp, err := svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: id, DefaultWorkflow: "sdlc"})
		if err != nil {
			t.Fatalf("UpdateProject: %v", err)
		}
		if resp.Project.DefaultWorkflow != "sdlc" {
			t.Fatalf("DefaultWorkflow = %q, want sdlc", resp.Project.DefaultWorkflow)
		}
	})

	t.Run("empty leaves it unchanged", func(t *testing.T) {
		resp, err := svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: id, Description: "touched"})
		if err != nil {
			t.Fatalf("UpdateProject: %v", err)
		}
		if resp.Project.DefaultWorkflow != "sdlc" {
			t.Fatalf("DefaultWorkflow = %q, want sdlc", resp.Project.DefaultWorkflow)
		}
	})

	t.Run("clear removes it", func(t *testing.T) {
		resp, err := svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: id, ClearDefaultWorkflow: true})
		if err != nil {
			t.Fatalf("UpdateProject: %v", err)
		}
		if resp.Project.DefaultWorkflow != "" {
			t.Fatalf("DefaultWorkflow = %q, want empty", resp.Project.DefaultWorkflow)
		}
	})

	t.Run("a workflow deleted after being named is refused on the next set", func(t *testing.T) {
		if err := loader.Delete("tenant-1", "sdlc"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		_, err := svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: id, DefaultWorkflow: "sdlc"})
		requireReferenceNotFound(t, err, "default_workflow")
	})
}

// =============================================================================
// Project.Git — author, signing, access key
// =============================================================================

// newProjectServiceWithKeys is a project service whose SSH key references
// resolve against keys, addressed "scope/owner/name".
func newProjectServiceWithKeys(keys ...string) ProjectService {
	svc := newTestProjectService()
	svc.(*projectServiceImpl).SetSSHKeys(newFakeSSHKeys(keys...))
	return svc
}

func TestProjectService_CreateProject_GitIdentityIsStored(t *testing.T) {
	svc := newProjectServiceWithKeys("tenant/tenant-1/api-signing", "service/deploy/api-access")
	resp, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID:            "tenant-1",
		Name:                "api",
		GitRepoURL:          "ssh://git@forge.example.test/acme/api.git",
		GitAuthorName:       "agent-api",
		GitAuthorEmail:      "agent-api@example.test",
		GitSigningEnabled:   true,
		GitSigningAlgorithm: orchestration.SigningAlgorithmECDSAP256,
		GitSigningKeyName:   "api-signing",
		GitSigningKeyScope:  "tenant",
		GitSigningKeyOwner:  "tenant-1",
		GitSSHKeyName:       "api-access",
		GitSSHKeyScope:      "service",
		GitSSHKeyOwner:      "deploy",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	want := orchestration.ProjectGit{
		RepoURL: "ssh://git@forge.example.test/acme/api.git",
		Author:  orchestration.GitAuthor{Name: "agent-api", Email: "agent-api@example.test"},
		Signing: orchestration.CommitSigning{
			Enabled:   true,
			Algorithm: orchestration.SigningAlgorithmECDSAP256,
			Key:       orchestration.KMSKeyRef{Name: "api-signing", Scope: "tenant", Owner: "tenant-1"},
		},
		SSHKey: orchestration.KMSKeyRef{Name: "api-access", Scope: "service", Owner: "deploy"},
	}
	if !reflect.DeepEqual(resp.Project.Git, want) {
		t.Fatalf("Git = %+v\nwant %+v", resp.Project.Git, want)
	}
	if got := resp.Project.Git.AccessKey(); got != want.SSHKey {
		t.Fatalf("AccessKey() = %+v, want the configured SSH key", got)
	}
}

// TestProjectService_CreateProject_SSHKeyNoLongerNeedsAManagedWorkspace: the
// SSH key reference is a property of the workspace, persisted on the record,
// not a clone-only credential.
func TestProjectService_CreateProject_SSHKeyNoLongerNeedsAManagedWorkspace(t *testing.T) {
	svc := newProjectServiceWithKeys("user/tenant-1/api-access")
	resp, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID:      "tenant-1",
		Name:          "api",
		WorkspacePath: "/srv/api",
		GitRepoURL:    "ssh://git@forge.example.test/acme/api.git",
		GitSSHKeyName: "api-access",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if resp.Project.Git.SSHKey.Name != "api-access" {
		t.Fatalf("SSHKey = %+v, want api-access", resp.Project.Git.SSHKey)
	}
}

func TestProjectService_CreateProject_GitIdentityRefusals(t *testing.T) {
	svc := newTestProjectService()
	ctx := context.Background()
	cases := map[string]struct {
		req   CreateProjectRequest
		field string
	}{
		"half author":     {CreateProjectRequest{TenantID: "t", Name: "p", GitAuthorName: "agent"}, "git.author.email"},
		"keyless signing": {CreateProjectRequest{TenantID: "t", Name: "p", GitSigningEnabled: true}, "git.signing.key"},
		"unruled algo":    {CreateProjectRequest{TenantID: "t", Name: "p", GitSigningEnabled: true, GitSigningKeyName: "k", GitSigningAlgorithm: "RSA-2048"}, "git.signing.algorithm"},
		"token and key":   {CreateProjectRequest{TenantID: "t", Name: "p", GitRepoURL: "https://x/y.git", WorkspaceManaged: true, GitCredentialSecretRef: "tok", GitSSHKeyName: "k"}, "git_credential_secret_ref"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := tc.req
			_, err := svc.CreateProject(ctx, &req)
			requireProjectInvalidField(t, err, tc.field)
		})
	}
}

func TestProjectService_UpdateProject_GitIdentity(t *testing.T) {
	svc := newProjectServiceWithKeys("user/tenant-1/signing", "tenant/tenant-1/signing", "service/deploy/access")
	ctx := context.Background()
	created, err := svc.CreateProject(ctx, &CreateProjectRequest{
		TenantID:          "tenant-1",
		Name:              "api",
		GitAuthorName:     "agent",
		GitAuthorEmail:    "agent@example.test",
		GitSigningEnabled: true,
		GitSigningKeyName: "signing",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	id := created.Project.ID
	enabled, disabled := true, false

	t.Run("fields update independently", func(t *testing.T) {
		resp, err := svc.UpdateProject(ctx, &UpdateProjectRequest{
			TenantID: "tenant-1", ProjectID: id,
			GitAuthorEmail:      "agent@example.com",
			GitSigningAlgorithm: orchestration.SigningAlgorithmECDSAP256,
			GitSigningKeyScope:  "tenant",
			GitSigningKeyOwner:  "tenant-1",
			GitSSHKeyName:       "access",
			GitSSHKeyScope:      "service",
			GitSSHKeyOwner:      "deploy",
		})
		if err != nil {
			t.Fatalf("UpdateProject: %v", err)
		}
		git := resp.Project.Git
		if git.Author != (orchestration.GitAuthor{Name: "agent", Email: "agent@example.com"}) {
			t.Errorf("author = %+v", git.Author)
		}
		if !git.Signing.Enabled || git.Signing.Algorithm != orchestration.SigningAlgorithmECDSAP256 ||
			git.Signing.Key != (orchestration.KMSKeyRef{Name: "signing", Scope: "tenant", Owner: "tenant-1"}) {
			t.Errorf("signing = %+v", git.Signing)
		}
		if git.SSHKey != (orchestration.KMSKeyRef{Name: "access", Scope: "service", Owner: "deploy"}) {
			t.Errorf("ssh key = %+v", git.SSHKey)
		}
	})

	t.Run("nil enabled leaves signing on", func(t *testing.T) {
		resp, err := svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: id, Description: "x"})
		if err != nil {
			t.Fatalf("UpdateProject: %v", err)
		}
		if !resp.Project.Git.Signing.Enabled {
			t.Error("an update that said nothing about signing turned it off")
		}
	})

	t.Run("disable then enable", func(t *testing.T) {
		resp, err := svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: id, GitSigningEnabled: &disabled})
		if err != nil {
			t.Fatalf("UpdateProject: %v", err)
		}
		if resp.Project.Git.Signing.Enabled {
			t.Error("signing was not turned off")
		}
		resp, err = svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: id, GitSigningEnabled: &enabled})
		if err != nil {
			t.Fatalf("UpdateProject: %v", err)
		}
		if !resp.Project.Git.Signing.Enabled {
			t.Error("signing was not turned back on")
		}
	})

	t.Run("an update that breaks the policy is refused and not stored", func(t *testing.T) {
		_, err := svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: id, GitSigningAlgorithm: "RSA-2048"})
		requireProjectInvalidField(t, err, "git.signing.algorithm")
		got, err := svc.GetProject(ctx, &GetProjectRequest{TenantID: "tenant-1", ProjectID: id})
		if err != nil {
			t.Fatalf("GetProject: %v", err)
		}
		if got.Project.Git.Signing.Algorithm != orchestration.SigningAlgorithmECDSAP256 {
			t.Errorf("a refused update was stored: %+v", got.Project.Git.Signing)
		}
	})
}
