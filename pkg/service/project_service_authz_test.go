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
// AuthorizeFleetNode
// ---------------------------------------------------------------------------

func TestProjectService_AuthorizeAgent_WorkerAllowed(t *testing.T) {
	ctx := context.Background()
	project := &orchestration.Project{
		ID:       "proj-1",
		TenantID: "t1",
		Name:     "p",
		Permissions: orchestration.ProjectPermissions{
			FleetNodeRoles: map[string]orchestration.ProjectRole{
				"agent-A": orchestration.ProjectRoleWorker,
			},
		},
	}
	svc := buildProjectSvc(t, project)

	err := svc.AuthorizeFleetNode(ctx, &service.AuthorizeFleetNodeRequest{
		TenantID:     "t1",
		ProjectID:    "proj-1",
		FleetNodeID:  "agent-A",
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
			FleetNodeRoles: map[string]orchestration.ProjectRole{
				"agent-B": orchestration.ProjectRoleDenied,
			},
		},
	}
	svc := buildProjectSvc(t, project)

	err := svc.AuthorizeFleetNode(ctx, &service.AuthorizeFleetNodeRequest{
		TenantID:     "t1",
		ProjectID:    "proj-2",
		FleetNodeID:  "agent-B",
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
	err := svc.AuthorizeFleetNode(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
}

func TestProjectService_AuthorizeAgent_EmptyTenantIDError(t *testing.T) {
	svc := service.NewProjectService(orchestration.NewInMemoryProjectStore())
	err := svc.AuthorizeFleetNode(context.Background(), &service.AuthorizeFleetNodeRequest{
		ProjectID:    "p",
		FleetNodeID:  "a",
		RequiredRole: orchestration.ProjectRoleWorker,
	})
	if err == nil {
		t.Fatal("expected error for empty tenant_id, got nil")
	}
}

// ---------------------------------------------------------------------------
// GrantFleetNodeRole
// ---------------------------------------------------------------------------

func TestProjectService_GrantAgentRole_Success(t *testing.T) {
	ctx := context.Background()
	project := &orchestration.Project{
		ID:       "proj-3",
		TenantID: "t1",
		Name:     "p3",
	}
	svc := buildProjectSvc(t, project)

	resp, err := svc.GrantFleetNodeRole(ctx, &service.GrantFleetNodeRoleRequest{
		TenantID:    "t1",
		ProjectID:   "proj-3",
		FleetNodeID: "agent-C",
		Role:        orchestration.ProjectRoleLead,
	})
	if err != nil {
		t.Fatalf("GrantFleetNodeRole: %v", err)
	}
	if resp.Role != orchestration.ProjectRoleLead {
		t.Errorf("role = %q, want %q", resp.Role, orchestration.ProjectRoleLead)
	}

	// Verify by authorizing.
	authErr := svc.AuthorizeFleetNode(ctx, &service.AuthorizeFleetNodeRequest{
		TenantID:     "t1",
		ProjectID:    "proj-3",
		FleetNodeID:  "agent-C",
		RequiredRole: orchestration.ProjectRoleLead,
	})
	if authErr != nil {
		t.Fatalf("expected authorized after grant, got %v", authErr)
	}
}

func TestProjectService_GrantAgentRole_ProjectNotFound(t *testing.T) {
	ctx := context.Background()
	svc := service.NewProjectService(orchestration.NewInMemoryProjectStore())

	_, err := svc.GrantFleetNodeRole(ctx, &service.GrantFleetNodeRoleRequest{
		TenantID:    "t1",
		ProjectID:   "no-such-project",
		FleetNodeID: "agent-D",
		Role:        orchestration.ProjectRoleWorker,
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
// RevokeFleetNodeRole
// ---------------------------------------------------------------------------

func TestProjectService_RevokeAgentRole_SetsRoleToDenied(t *testing.T) {
	ctx := context.Background()
	project := &orchestration.Project{
		ID:       "proj-4",
		TenantID: "t1",
		Name:     "p4",
		Permissions: orchestration.ProjectPermissions{
			FleetNodeRoles: map[string]orchestration.ProjectRole{
				"agent-E": orchestration.ProjectRoleWorker,
			},
		},
	}
	svc := buildProjectSvc(t, project)

	resp, err := svc.RevokeFleetNodeRole(ctx, &service.RevokeFleetNodeRoleRequest{
		TenantID:    "t1",
		ProjectID:   "proj-4",
		FleetNodeID: "agent-E",
	})
	if err != nil {
		t.Fatalf("RevokeFleetNodeRole: %v", err)
	}
	if resp.FleetNodeID != "agent-E" {
		t.Errorf("AgentID = %q, want %q", resp.FleetNodeID, "agent-E")
	}

	// Authorization should now fail.
	authErr := svc.AuthorizeFleetNode(ctx, &service.AuthorizeFleetNodeRequest{
		TenantID:     "t1",
		ProjectID:    "proj-4",
		FleetNodeID:  "agent-E",
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

	_, err := svc.RevokeFleetNodeRole(ctx, &service.RevokeFleetNodeRoleRequest{
		TenantID:    "t1",
		ProjectID:   "no-such-project",
		FleetNodeID: "agent-F",
	})
	if err == nil {
		t.Fatal("expected error for missing project, got nil")
	}
	var notFound *service.ProjectNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected ProjectNotFoundError, got %T: %v", err, err)
	}
}
