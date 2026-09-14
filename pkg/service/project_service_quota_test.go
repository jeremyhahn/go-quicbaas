// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package service

import (
	"context"
	"testing"
)

// int64Ptr and intPtr build the presence-carrying quota values an update
// sends. They exist so a test reads as "the operator asked for zero" rather
// than as pointer ceremony.
func int64Ptr(v int64) *int64 { return &v }
func intPtr(v int) *int       { return &v }

// newCappedProject creates a project with every quota capped.
func newCappedProject(t *testing.T, svc ProjectService) string {
	t.Helper()
	resp, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID:       "tenant-1",
		Name:           "api",
		TokenBudget:    1_000_000,
		GPUMinutes:     600,
		MaxConcurrency: 2,
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return resp.Project.ID
}

// TestProjectService_UpdateProject_ZeroQuotaLiftsTheCap is the operator story
// the old shape could not tell.
//
// A quota of zero means UNLIMITED — the project record says so, the dialog
// says so, and the docs say so — but the update applied a quota only when it
// was NON-zero. So an operator could set a cap to 0, watch the save succeed,
// and remain capped, with nothing anywhere reporting a problem.
func TestProjectService_UpdateProject_ZeroQuotaLiftsTheCap(t *testing.T) {
	svc := newAutonomyProjectService()
	id := newCappedProject(t, svc)

	resp, err := svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID:       "tenant-1",
		ProjectID:      id,
		TokenBudget:    int64Ptr(0),
		GPUMinutes:     int64Ptr(0),
		MaxConcurrency: intPtr(0),
	})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	quotas := resp.Project.Quotas
	if quotas.TokenBudget != 0 || quotas.GPUMinutes != 0 || quotas.MaxConcurrency != 0 {
		t.Fatalf("quotas = %+v, want every cap lifted to unlimited", quotas)
	}
}

// TestProjectService_UpdateProject_AbsentQuotaLeavesTheCap: nil is still
// "leave it alone", which is what makes zero mean something.
func TestProjectService_UpdateProject_AbsentQuotaLeavesTheCap(t *testing.T) {
	svc := newAutonomyProjectService()
	id := newCappedProject(t, svc)

	resp, err := svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID:  "tenant-1",
		ProjectID: id,
		// No quota named at all.
	})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	quotas := resp.Project.Quotas
	if quotas.TokenBudget != 1_000_000 || quotas.GPUMinutes != 600 || quotas.MaxConcurrency != 2 {
		t.Fatalf("quotas = %+v, want the caps untouched", quotas)
	}
}

// TestProjectService_UpdateProject_QuotaMovesToANewCap: the ordinary case
// still works, and one quota moving leaves the others where they were.
func TestProjectService_UpdateProject_QuotaMovesToANewCap(t *testing.T) {
	svc := newAutonomyProjectService()
	id := newCappedProject(t, svc)

	resp, err := svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID:       "tenant-1",
		ProjectID:      id,
		MaxConcurrency: intPtr(8),
	})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	quotas := resp.Project.Quotas
	if quotas.MaxConcurrency != 8 {
		t.Errorf("MaxConcurrency = %d, want 8", quotas.MaxConcurrency)
	}
	if quotas.TokenBudget != 1_000_000 || quotas.GPUMinutes != 600 {
		t.Errorf("quotas = %+v, want the untouched caps kept", quotas)
	}
}
