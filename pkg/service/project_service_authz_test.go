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

package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// buildProjectSvc returns a projectService with a seeded project.
func buildProjectSvc(t *testing.T, project *orchestration.Project) service.ProjectService {
	t.Helper()
	ctx := context.Background()
	store := orchestration.NewInMemoryProjectStore()
	if err := store.Create(ctx, project); err != nil {
		t.Fatalf("buildProjectSvc: create: %v", err)
	}
	return service.NewProjectService(store)
}

// ---------------------------------------------------------------------------
// AuthorizeAgent
// ---------------------------------------------------------------------------

func TestProjectService_AuthorizeAgent_WorkerAllowed(t *testing.T) {
	ctx := context.Background()
	project := &orchestration.Project{
		ID:       "proj-1",
		TenantID: "t1",
		Name:     "p",
		Permissions: orchestration.ProjectPermissions{
			AgentRoles: map[string]orchestration.ProjectRole{
				"agent-A": orchestration.ProjectRoleWorker,
			},
		},
	}
	svc := buildProjectSvc(t, project)

	err := svc.AuthorizeAgent(ctx, &service.AuthorizeAgentRequest{
		TenantID:     "t1",
		ProjectID:    "proj-1",
		AgentID:      "agent-A",
		RequiredRole: orchestration.ProjectRoleWorker,
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestProjectService_AuthorizeAgent_DeniedRejected(t *testing.T) {
	ctx := context.Background()
	project := &orchestration.Project{
		ID:       "proj-2",
		TenantID: "t1",
		Name:     "p2",
		Permissions: orchestration.ProjectPermissions{
			AgentRoles: map[string]orchestration.ProjectRole{
				"agent-B": orchestration.ProjectRoleDenied,
			},
		},
	}
	svc := buildProjectSvc(t, project)

	err := svc.AuthorizeAgent(ctx, &service.AuthorizeAgentRequest{
		TenantID:     "t1",
		ProjectID:    "proj-2",
		AgentID:      "agent-B",
		RequiredRole: orchestration.ProjectRoleObserver,
	})
	if err == nil {
		t.Fatal("expected error for denied agent, got nil")
	}
	var denied *service.ProjectAccessDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected ProjectAccessDeniedError, got %T: %v", err, err)
	}
}

func TestProjectService_AuthorizeAgent_NilRequestError(t *testing.T) {
	svc := service.NewProjectService(orchestration.NewInMemoryProjectStore())
	err := svc.AuthorizeAgent(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
}

func TestProjectService_AuthorizeAgent_EmptyTenantIDError(t *testing.T) {
	svc := service.NewProjectService(orchestration.NewInMemoryProjectStore())
	err := svc.AuthorizeAgent(context.Background(), &service.AuthorizeAgentRequest{
		ProjectID:    "p",
		AgentID:      "a",
		RequiredRole: orchestration.ProjectRoleWorker,
	})
	if err == nil {
		t.Fatal("expected error for empty tenant_id, got nil")
	}
}

// ---------------------------------------------------------------------------
// GrantAgentRole
// ---------------------------------------------------------------------------

func TestProjectService_GrantAgentRole_Success(t *testing.T) {
	ctx := context.Background()
	project := &orchestration.Project{
		ID:       "proj-3",
		TenantID: "t1",
		Name:     "p3",
	}
	svc := buildProjectSvc(t, project)

	resp, err := svc.GrantAgentRole(ctx, &service.GrantAgentRoleRequest{
		TenantID:  "t1",
		ProjectID: "proj-3",
		AgentID:   "agent-C",
		Role:      orchestration.ProjectRoleLead,
	})
	if err != nil {
		t.Fatalf("GrantAgentRole: %v", err)
	}
	if resp.Role != orchestration.ProjectRoleLead {
		t.Errorf("role = %q, want %q", resp.Role, orchestration.ProjectRoleLead)
	}

	// Verify by authorizing.
	authErr := svc.AuthorizeAgent(ctx, &service.AuthorizeAgentRequest{
		TenantID:     "t1",
		ProjectID:    "proj-3",
		AgentID:      "agent-C",
		RequiredRole: orchestration.ProjectRoleLead,
	})
	if authErr != nil {
		t.Fatalf("expected authorized after grant, got %v", authErr)
	}
}

func TestProjectService_GrantAgentRole_ProjectNotFound(t *testing.T) {
	ctx := context.Background()
	svc := service.NewProjectService(orchestration.NewInMemoryProjectStore())

	_, err := svc.GrantAgentRole(ctx, &service.GrantAgentRoleRequest{
		TenantID:  "t1",
		ProjectID: "no-such-project",
		AgentID:   "agent-D",
		Role:      orchestration.ProjectRoleWorker,
	})
	if err == nil {
		t.Fatal("expected error for missing project, got nil")
	}
	var notFound *service.ProjectNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected ProjectNotFoundError, got %T: %v", err, err)
	}
}

// ---------------------------------------------------------------------------
// RevokeAgentRole
// ---------------------------------------------------------------------------

func TestProjectService_RevokeAgentRole_SetsRoleToDenied(t *testing.T) {
	ctx := context.Background()
	project := &orchestration.Project{
		ID:       "proj-4",
		TenantID: "t1",
		Name:     "p4",
		Permissions: orchestration.ProjectPermissions{
			AgentRoles: map[string]orchestration.ProjectRole{
				"agent-E": orchestration.ProjectRoleWorker,
			},
		},
	}
	svc := buildProjectSvc(t, project)

	resp, err := svc.RevokeAgentRole(ctx, &service.RevokeAgentRoleRequest{
		TenantID:  "t1",
		ProjectID: "proj-4",
		AgentID:   "agent-E",
	})
	if err != nil {
		t.Fatalf("RevokeAgentRole: %v", err)
	}
	if resp.AgentID != "agent-E" {
		t.Errorf("AgentID = %q, want %q", resp.AgentID, "agent-E")
	}

	// Authorization should now fail.
	authErr := svc.AuthorizeAgent(ctx, &service.AuthorizeAgentRequest{
		TenantID:     "t1",
		ProjectID:    "proj-4",
		AgentID:      "agent-E",
		RequiredRole: orchestration.ProjectRoleObserver,
	})
	if authErr == nil {
		t.Fatal("expected denial after revoke, got nil")
	}
	var denied *service.ProjectAccessDeniedError
	if !errors.As(authErr, &denied) {
		t.Fatalf("expected ProjectAccessDeniedError, got %T", authErr)
	}
}

func TestProjectService_RevokeAgentRole_ProjectNotFound(t *testing.T) {
	ctx := context.Background()
	svc := service.NewProjectService(orchestration.NewInMemoryProjectStore())

	_, err := svc.RevokeAgentRole(ctx, &service.RevokeAgentRoleRequest{
		TenantID:  "t1",
		ProjectID: "no-such-project",
		AgentID:   "agent-F",
	})
	if err == nil {
		t.Fatal("expected error for missing project, got nil")
	}
	var notFound *service.ProjectNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected ProjectNotFoundError, got %T: %v", err, err)
	}
}
