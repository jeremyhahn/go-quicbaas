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

package orchestration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// newScopedProject returns a Project with a non-empty AgentRoles map.
func newScopedProject(id, tenantID string, roles map[string]orchestration.ProjectRole) *orchestration.Project {
	return &orchestration.Project{
		ID:       id,
		TenantID: tenantID,
		Name:     "test-project",
		Permissions: orchestration.ProjectPermissions{
			AgentRoles: roles,
		},
	}
}

// seedStore creates a store seeded with the given project.
func seedStore(ctx context.Context, t *testing.T, project *orchestration.Project) orchestration.ProjectStore {
	t.Helper()
	store := orchestration.NewInMemoryProjectStore()
	if err := store.Create(ctx, project); err != nil {
		t.Fatalf("seedStore: %v", err)
	}
	return store
}

// ---------------------------------------------------------------------------
// RoleAtLeast
// ---------------------------------------------------------------------------

func TestRoleAtLeast_OrderCorrect(t *testing.T) {
	tests := []struct {
		actual   orchestration.ProjectRole
		required orchestration.ProjectRole
		want     bool
	}{
		{orchestration.ProjectRoleLead, orchestration.ProjectRoleLead, true},
		{orchestration.ProjectRoleLead, orchestration.ProjectRoleWorker, true},
		{orchestration.ProjectRoleLead, orchestration.ProjectRoleObserver, true},
		{orchestration.ProjectRoleLead, orchestration.ProjectRoleDenied, true},
		{orchestration.ProjectRoleWorker, orchestration.ProjectRoleLead, false},
		{orchestration.ProjectRoleWorker, orchestration.ProjectRoleWorker, true},
		{orchestration.ProjectRoleWorker, orchestration.ProjectRoleObserver, true},
		{orchestration.ProjectRoleObserver, orchestration.ProjectRoleWorker, false},
		{orchestration.ProjectRoleObserver, orchestration.ProjectRoleObserver, true},
		{orchestration.ProjectRoleDenied, orchestration.ProjectRoleObserver, false},
		{orchestration.ProjectRoleDenied, orchestration.ProjectRoleDenied, true},
	}
	for _, tt := range tests {
		got := orchestration.RoleAtLeast(tt.actual, tt.required)
		if got != tt.want {
			t.Errorf("RoleAtLeast(%q, %q) = %v, want %v", tt.actual, tt.required, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// ProjectAuthz.AuthorizeAgentForTenant — happy paths
// ---------------------------------------------------------------------------

func TestProjectAuthz_WorkerCanClaimWorkerTask(t *testing.T) {
	ctx := context.Background()
	project := newScopedProject("proj-1", "tenant-1", map[string]orchestration.ProjectRole{
		"agent-A": orchestration.ProjectRoleWorker,
	})
	authz := orchestration.NewProjectAuthz(seedStore(ctx, t, project))

	err := authz.AuthorizeAgentForTenant(ctx, "tenant-1", "proj-1", "agent-A", orchestration.ProjectRoleWorker)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestProjectAuthz_LeadCanClaimLeadTask(t *testing.T) {
	ctx := context.Background()
	project := newScopedProject("proj-2", "tenant-1", map[string]orchestration.ProjectRole{
		"agent-B": orchestration.ProjectRoleLead,
	})
	authz := orchestration.NewProjectAuthz(seedStore(ctx, t, project))

	err := authz.AuthorizeAgentForTenant(ctx, "tenant-1", "proj-2", "agent-B", orchestration.ProjectRoleLead)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestProjectAuthz_LegacyUnscopedProjectAllowsAll(t *testing.T) {
	ctx := context.Background()
	// Project with empty AgentRoles map — legacy unscoped.
	project := &orchestration.Project{
		ID:       "proj-3",
		TenantID: "tenant-1",
		Name:     "legacy",
	}
	authz := orchestration.NewProjectAuthz(seedStore(ctx, t, project))

	// Any agent should be allowed even with the strictest role requirement.
	err := authz.AuthorizeAgentForTenant(ctx, "tenant-1", "proj-3", "agent-X", orchestration.ProjectRoleLead)
	if err != nil {
		t.Fatalf("legacy project should allow all agents: %v", err)
	}
}

func TestProjectAuthz_MissingProjectAllowsAll(t *testing.T) {
	ctx := context.Background()
	store := orchestration.NewInMemoryProjectStore()
	authz := orchestration.NewProjectAuthz(store)

	// Project doesn't exist — should fall through to legacy behaviour.
	err := authz.AuthorizeAgentForTenant(ctx, "tenant-1", "no-such-project", "agent-A", orchestration.ProjectRoleWorker)
	if err != nil {
		t.Fatalf("missing project should allow all agents: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ProjectAuthz.AuthorizeAgentForTenant — denial paths
// ---------------------------------------------------------------------------

func TestProjectAuthz_DeniedRoleRejected(t *testing.T) {
	ctx := context.Background()
	project := newScopedProject("proj-4", "tenant-1", map[string]orchestration.ProjectRole{
		"agent-C": orchestration.ProjectRoleDenied,
	})
	authz := orchestration.NewProjectAuthz(seedStore(ctx, t, project))

	err := authz.AuthorizeAgentForTenant(ctx, "tenant-1", "proj-4", "agent-C", orchestration.ProjectRoleObserver)
	if err == nil {
		t.Fatal("expected error for denied agent, got nil")
	}
	var denied *orchestration.ProjectAccessDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected ProjectAccessDeniedError, got %T: %v", err, err)
	}
	if denied.ActualRole != orchestration.ProjectRoleDenied {
		t.Errorf("ActualRole = %q, want %q", denied.ActualRole, orchestration.ProjectRoleDenied)
	}
}

func TestProjectAuthz_AbsentAgentInScopedProjectRejected(t *testing.T) {
	ctx := context.Background()
	// Scoped project — only "agent-D" has a role; "agent-E" is not in the matrix.
	project := newScopedProject("proj-5", "tenant-1", map[string]orchestration.ProjectRole{
		"agent-D": orchestration.ProjectRoleWorker,
	})
	authz := orchestration.NewProjectAuthz(seedStore(ctx, t, project))

	err := authz.AuthorizeAgentForTenant(ctx, "tenant-1", "proj-5", "agent-E", orchestration.ProjectRoleObserver)
	if err == nil {
		t.Fatal("expected error for absent agent, got nil")
	}
	var denied *orchestration.ProjectAccessDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected ProjectAccessDeniedError, got %T", err)
	}
}

func TestProjectAuthz_WorkerCannotClaimLeadTask(t *testing.T) {
	ctx := context.Background()
	project := newScopedProject("proj-6", "tenant-1", map[string]orchestration.ProjectRole{
		"agent-F": orchestration.ProjectRoleWorker,
	})
	authz := orchestration.NewProjectAuthz(seedStore(ctx, t, project))

	err := authz.AuthorizeAgentForTenant(ctx, "tenant-1", "proj-6", "agent-F", orchestration.ProjectRoleLead)
	if err == nil {
		t.Fatal("expected denial for worker claiming lead task, got nil")
	}
	var denied *orchestration.ProjectAccessDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected ProjectAccessDeniedError, got %T", err)
	}
}

func TestProjectAuthz_ObserverCannotClaimWorkerTask(t *testing.T) {
	ctx := context.Background()
	project := newScopedProject("proj-7", "tenant-1", map[string]orchestration.ProjectRole{
		"agent-G": orchestration.ProjectRoleObserver,
	})
	authz := orchestration.NewProjectAuthz(seedStore(ctx, t, project))

	err := authz.AuthorizeAgentForTenant(ctx, "tenant-1", "proj-7", "agent-G", orchestration.ProjectRoleWorker)
	if err == nil {
		t.Fatal("expected denial for observer claiming worker task, got nil")
	}
}

func TestProjectAuthz_InvalidRequiredRoleReturnsError(t *testing.T) {
	ctx := context.Background()
	store := orchestration.NewInMemoryProjectStore()
	authz := orchestration.NewProjectAuthz(store)

	err := authz.AuthorizeAgentForTenant(ctx, "tenant-1", "proj-1", "agent-A", orchestration.ProjectRole("superuser"))
	if err == nil {
		t.Fatal("expected error for invalid role, got nil")
	}
	var invalid *orchestration.ProjectRoleInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected ProjectRoleInvalidError, got %T", err)
	}
}

func TestProjectAuthz_EmptyProjectIDSkipsCheck(t *testing.T) {
	ctx := context.Background()
	store := orchestration.NewInMemoryProjectStore()
	authz := orchestration.NewProjectAuthz(store)

	// Empty projectID → legacy unscoped path → nil.
	err := authz.AuthorizeAgentForTenant(ctx, "tenant-1", "", "agent-A", orchestration.ProjectRoleLead)
	if err != nil {
		t.Fatalf("empty projectID should be a no-op: %v", err)
	}
}
