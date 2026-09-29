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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// newTestProjectService creates a service backed by an in-memory store.
func newTestProjectService() ProjectService {
	return NewProjectService(orchestration.NewInMemoryProjectStore())
}

// =============================================================================
// CreateProject
// =============================================================================

func TestProjectService_CreateProject_HappyPath(t *testing.T) {
	svc := newTestProjectService()
	ctx := context.Background()

	resp, err := svc.CreateProject(ctx, &CreateProjectRequest{
		TenantID: "tenant-1",
		Name:     "my-project",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Project == nil {
		t.Fatal("expected non-nil project")
	}
	if resp.Project.ID == "" {
		t.Error("expected non-empty project ID")
	}
	if resp.Project.Name != "my-project" {
		t.Errorf("expected name 'my-project', got %q", resp.Project.Name)
	}
}

func TestProjectService_CreateProject_DuplicateName(t *testing.T) {
	svc := newTestProjectService()
	ctx := context.Background()

	req := &CreateProjectRequest{TenantID: "tenant-1", Name: "dup"}
	if _, err := svc.CreateProject(ctx, req); err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	_, err := svc.CreateProject(ctx, req)
	if err == nil {
		t.Fatal("expected error for duplicate name, got nil")
	}
	var alreadyExists *ProjectAlreadyExistsError
	if !errors.As(err, &alreadyExists) {
		t.Errorf("expected ProjectAlreadyExistsError, got %T: %v", err, err)
	}
}

func TestProjectService_CreateProject_NilRequest(t *testing.T) {
	svc := newTestProjectService()
	_, err := svc.CreateProject(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
	var invalid *ProjectInvalidError
	if !errors.As(err, &invalid) {
		t.Errorf("expected ProjectInvalidError, got %T", err)
	}
}

func TestProjectService_CreateProject_MissingTenantID(t *testing.T) {
	svc := newTestProjectService()
	_, err := svc.CreateProject(context.Background(), &CreateProjectRequest{Name: "x"})
	if err == nil {
		t.Fatal("expected error for missing tenantID")
	}
	var invalid *ProjectInvalidError
	if !errors.As(err, &invalid) {
		t.Errorf("expected ProjectInvalidError, got %T", err)
	}
}

func TestProjectService_CreateProject_MissingName(t *testing.T) {
	svc := newTestProjectService()
	_, err := svc.CreateProject(context.Background(), &CreateProjectRequest{TenantID: "t"})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	var invalid *ProjectInvalidError
	if !errors.As(err, &invalid) {
		t.Errorf("expected ProjectInvalidError, got %T", err)
	}
}

// leadDirectory is a real agent directory holding one lead Agent, "lead-agent",
// and one coder, "coder", in tenant-1.
func leadDirectory(t *testing.T) *orchestration.AgentDirectory {
	t.Helper()
	store := orchestration.NewInMemoryAgentCatalogStore()
	for _, agent := range []orchestration.Agent{
		{ID: "lead-agent", Name: "Lead", Backend: "claude", Model: "claude-opus-5-5", Roles: []orchestration.AgentRole{orchestration.AgentRoleLead}},
		{ID: "coder", Name: "Coder", Backend: "claude", Model: "claude-opus-5-5", Roles: []orchestration.AgentRole{orchestration.AgentRoleCoding}},
	} {
		if err := store.CreateAgent(context.Background(), "tenant-1", &agent); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	dir, err := orchestration.NewAgentDirectory(orchestration.AgentDirectoryConfig{Store: store, Models: noModels{}})
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// noModels is a live catalog in which every backend is served with no curated
// model list.
type noModels struct{}

func (noModels) ListBackendModels(context.Context, string) ([]string, error) { return nil, nil }

func TestProjectService_DefaultLeadAgentIsCheckedAgainstTheCatalog(t *testing.T) {
	ctx := context.Background()
	svc := newTestProjectService()
	wirer := svc.(interface{ SetAgentRefChecker(AgentRefChecker) })

	create := func(ref orchestration.AgentRef) error {
		_, err := svc.CreateProject(ctx, &CreateProjectRequest{TenantID: "tenant-1", Name: "p-" + ref.String(), DefaultLeadAgent: ref})
		return err
	}
	if err := create(orchestration.AgentRefTo("lead-agent")); !errors.Is(err, orchestration.ErrAgentCatalogUnavailable) {
		t.Fatalf("with no catalog a default lead is refused, got %v", err)
	}
	wirer.SetAgentRefChecker(leadDirectory(t))
	if err := create(orchestration.AgentRefTo("lead-agent")); err != nil {
		t.Fatalf("a lead agent: %v", err)
	}
	if err := create(orchestration.AgentRefTo("coder")); !errors.Is(err, orchestration.ErrAgentRoleMismatch) {
		t.Fatalf("a coder as lead = %v", err)
	}
	if err := create(orchestration.AgentRefTo("ghost")); !errors.Is(err, orchestration.ErrAgentUnknown) {
		t.Fatalf("an unknown lead = %v", err)
	}
	if err := create(orchestration.AgentRef{}); err != nil {
		t.Fatalf("no default lead is fine: %v", err)
	}

	created, err := svc.CreateProject(ctx, &CreateProjectRequest{TenantID: "tenant-1", Name: "to-update"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: created.Project.ID, DefaultLeadAgent: orchestration.AgentRefTo("coder")})
	if !errors.Is(err, orchestration.ErrAgentRoleMismatch) {
		t.Fatalf("an update to a coder lead = %v", err)
	}
	updated, err := svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: created.Project.ID, DefaultLeadAgent: orchestration.AgentRefTo("lead-agent")})
	if err != nil || updated.Project.DefaultLeadAgent.AgentID != "lead-agent" {
		t.Fatalf("UpdateProject = %+v, %v", updated, err)
	}
}

func TestProjectService_CreateProject_WithAllFields(t *testing.T) {
	svc := newTestProjectService()
	svc.(*projectServiceImpl).SetFleetNodes(newTestFleet(t))
	svc.(interface{ SetAgentRefChecker(AgentRefChecker) }).SetAgentRefChecker(leadDirectory(t))
	ctx := context.Background()

	resp, err := svc.CreateProject(ctx, &CreateProjectRequest{
		TenantID:             "tenant-1",
		Name:                 "full-project",
		Description:          "A full project",
		WorkspaceFleetNodeID: "agent-1",
		WorkspacePath:        "/workspace/full",
		HostAffinity:         "host-a",
		DefaultLeadAgent:     orchestration.AgentRefTo("lead-agent"),
		GitRepoURL:           "https://github.com/example/repo",
		GitDefaultBranch:     "main",
		GPUMinutes:           60,
		MaxConcurrency:       4,
		Metadata:             map[string]string{"env": "prod"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Project.Quotas.GPUMinutes != 60 {
		t.Errorf("expected gpu minutes 60, got %d", resp.Project.Quotas.GPUMinutes)
	}
	if resp.Project.Git.RepoURL != "https://github.com/example/repo" {
		t.Errorf("unexpected git URL: %q", resp.Project.Git.RepoURL)
	}
}

// =============================================================================
// GetProject
// =============================================================================

func TestProjectService_GetProject_HappyPath(t *testing.T) {
	svc := newTestProjectService()
	ctx := context.Background()

	created, _ := svc.CreateProject(ctx, &CreateProjectRequest{
		TenantID: "tenant-1",
		Name:     "get-me",
	})

	resp, err := svc.GetProject(ctx, &GetProjectRequest{
		TenantID:  "tenant-1",
		ProjectID: created.Project.ID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Project.Name != "get-me" {
		t.Errorf("expected 'get-me', got %q", resp.Project.Name)
	}
}

func TestProjectService_GetProject_NotFound(t *testing.T) {
	svc := newTestProjectService()
	_, err := svc.GetProject(context.Background(), &GetProjectRequest{
		TenantID:  "tenant-1",
		ProjectID: "ghost",
	})
	if err == nil {
		t.Fatal("expected not-found error")
	}
	var notFound *ProjectNotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("expected ProjectNotFoundError, got %T", err)
	}
}

func TestProjectService_GetProject_NilRequest(t *testing.T) {
	svc := newTestProjectService()
	_, err := svc.GetProject(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

// =============================================================================
// GetProjectByName
// =============================================================================

func TestProjectService_GetProjectByName_HappyPath(t *testing.T) {
	svc := newTestProjectService()
	ctx := context.Background()

	if _, err := svc.CreateProject(ctx, &CreateProjectRequest{
		TenantID: "tenant-1",
		Name:     "named-project",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	resp, err := svc.GetProjectByName(ctx, &GetProjectByNameRequest{
		TenantID: "tenant-1",
		Name:     "named-project",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Project.Name != "named-project" {
		t.Errorf("expected 'named-project', got %q", resp.Project.Name)
	}
}

func TestProjectService_GetProjectByName_NotFound(t *testing.T) {
	svc := newTestProjectService()
	_, err := svc.GetProjectByName(context.Background(), &GetProjectByNameRequest{
		TenantID: "tenant-1",
		Name:     "ghost",
	})
	if err == nil {
		t.Fatal("expected not-found error")
	}
	var notFound *ProjectNotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("expected ProjectNotFoundError, got %T", err)
	}
}

// =============================================================================
// UpdateProject
// =============================================================================

func TestProjectService_UpdateProject_HappyPath(t *testing.T) {
	svc := newTestProjectService()
	ctx := context.Background()

	created, _ := svc.CreateProject(ctx, &CreateProjectRequest{
		TenantID: "tenant-1",
		Name:     "old-name",
	})

	resp, err := svc.UpdateProject(ctx, &UpdateProjectRequest{
		TenantID:    "tenant-1",
		ProjectID:   created.Project.ID,
		Description: "updated description",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Project.Description != "updated description" {
		t.Errorf("expected updated description, got %q", resp.Project.Description)
	}
}

func TestProjectService_UpdateProject_NotFound(t *testing.T) {
	svc := newTestProjectService()
	_, err := svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID:  "tenant-1",
		ProjectID: "ghost",
	})
	if err == nil {
		t.Fatal("expected not-found error")
	}
	var notFound *ProjectNotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("expected ProjectNotFoundError, got %T", err)
	}
}

func TestProjectService_UpdateProject_ClearDescription(t *testing.T) {
	svc := newTestProjectService()
	ctx := context.Background()

	created, _ := svc.CreateProject(ctx, &CreateProjectRequest{
		TenantID:    "tenant-1",
		Name:        "proj",
		Description: "original",
	})

	resp, err := svc.UpdateProject(ctx, &UpdateProjectRequest{
		TenantID:         "tenant-1",
		ProjectID:        created.Project.ID,
		ClearDescription: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Project.Description != "" {
		t.Errorf("expected empty description, got %q", resp.Project.Description)
	}
}

// =============================================================================
// DeleteProject
// =============================================================================

func TestProjectService_DeleteProject_HappyPath(t *testing.T) {
	svc := newTestProjectService()
	ctx := context.Background()

	created, _ := svc.CreateProject(ctx, &CreateProjectRequest{
		TenantID: "tenant-1",
		Name:     "delete-me",
	})

	resp, err := svc.DeleteProject(ctx, &DeleteProjectRequest{
		TenantID:  "tenant-1",
		ProjectID: created.Project.ID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Deleted {
		t.Error("expected Deleted=true")
	}
}

func TestProjectService_DeleteProject_NotFound(t *testing.T) {
	svc := newTestProjectService()
	_, err := svc.DeleteProject(context.Background(), &DeleteProjectRequest{
		TenantID:  "tenant-1",
		ProjectID: "ghost",
	})
	if err == nil {
		t.Fatal("expected not-found error")
	}
	var notFound *ProjectNotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("expected ProjectNotFoundError, got %T", err)
	}
}

// =============================================================================
// ListProjects
// =============================================================================

func TestProjectService_ListProjects_HappyPath(t *testing.T) {
	svc := newTestProjectService()
	ctx := context.Background()

	for _, name := range []string{"a", "b", "c"} {
		if _, err := svc.CreateProject(ctx, &CreateProjectRequest{
			TenantID: "tenant-1",
			Name:     name,
		}); err != nil {
			t.Fatalf("create %q failed: %v", name, err)
		}
	}

	resp, err := svc.ListProjects(ctx, &ListProjectsRequest{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if resp.Total != 3 {
		t.Errorf("expected total=3, got %d", resp.Total)
	}
	if len(resp.Projects) != 3 {
		t.Errorf("expected 3 projects, got %d", len(resp.Projects))
	}
}

func TestProjectService_ListProjects_Empty(t *testing.T) {
	svc := newTestProjectService()
	resp, err := svc.ListProjects(context.Background(), &ListProjectsRequest{TenantID: "tenant-nobody"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("expected 0, got %d", resp.Total)
	}
}

func TestProjectService_ListProjects_MissingTenantID(t *testing.T) {
	svc := newTestProjectService()
	_, err := svc.ListProjects(context.Background(), &ListProjectsRequest{})
	if err == nil {
		t.Fatal("expected error for missing tenantID")
	}
	var invalid *ProjectInvalidError
	if !errors.As(err, &invalid) {
		t.Errorf("expected ProjectInvalidError, got %T", err)
	}
}
