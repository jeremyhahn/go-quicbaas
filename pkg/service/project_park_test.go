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

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Park / resume through the project update
// =============================================================================

func TestProjectService_UpdateProject_ParksAndResumes(t *testing.T) {
	svc := newTestProjectService()
	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID: "user-1", Username: "ops", Authenticated: true,
	})
	created, err := svc.CreateProject(ctx, &CreateProjectRequest{TenantID: "tenant-1", Name: "parkable", Autonomous: true})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	park := true
	resp, err := svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: created.Project.ID, Parked: &park})
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	if !resp.Project.Parked || resp.Project.ParkedAt.IsZero() || resp.Project.ParkedBy != "ops" {
		t.Fatalf("parked record = %v %v %q, want parked now by ops", resp.Project.Parked, resp.Project.ParkedAt, resp.Project.ParkedBy)
	}
	if !resp.Project.Autonomous {
		t.Error("parking turned autonomy off; the two switches are independent")
	}

	// An update that does not name the switch leaves it alone.
	resp, err = svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: created.Project.ID, Description: "still parked"})
	if err != nil || !resp.Project.Parked {
		t.Fatalf("an unrelated update unparked the project: %v %v", resp.Project.Parked, err)
	}

	resume := false
	resp, err = svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: created.Project.ID, Parked: &resume})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resp.Project.Parked || !resp.Project.ParkedAt.IsZero() || resp.Project.ParkedBy != "" {
		t.Fatalf("resumed record = %v %v %q, want an open project", resp.Project.Parked, resp.Project.ParkedAt, resp.Project.ParkedBy)
	}
	if !resp.Project.Autonomous {
		t.Error("resume turned autonomy off")
	}
}

func TestProjectService_UpdateProject_ParkWithNoIdentityRecordsNobody(t *testing.T) {
	svc := newTestProjectService()
	ctx := context.Background()
	created, err := svc.CreateProject(ctx, &CreateProjectRequest{TenantID: "tenant-1", Name: "anonymous-park"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	park := true
	resp, err := svc.UpdateProject(ctx, &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: created.Project.ID, Parked: &park})
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	if !resp.Project.Parked || resp.Project.ParkedBy != "" {
		t.Fatalf("parked = %v by %q, want parked by nobody named", resp.Project.Parked, resp.Project.ParkedBy)
	}
}

func TestProjectService_UpdateProject_ParkingAMissingProjectIsNotFound(t *testing.T) {
	svc := newTestProjectService()
	park := true
	_, err := svc.UpdateProject(context.Background(), &UpdateProjectRequest{TenantID: "tenant-1", ProjectID: "ghost", Parked: &park})
	var notFound *ProjectNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("err = %v, want ProjectNotFoundError", err)
	}
}

func TestCallerName(t *testing.T) {
	cases := map[string]struct {
		subject *interfaces.Subject
		want    string
	}{
		"no subject":                 {nil, ""},
		"an unauthenticated subject": {&interfaces.Subject{ID: "u", Username: "x"}, ""},
		"a named subject":            {&interfaces.Subject{ID: "u", Username: "ops", Authenticated: true}, "ops"},
		"a subject with only its ID": {&interfaces.Subject{ID: "svc-1", Authenticated: true}, "svc-1"},
	}
	for name, tc := range cases {
		ctx := context.Background()
		if tc.subject != nil {
			ctx = auth.SubjectIntoContext(ctx, *tc.subject)
		}
		if got := callerName(ctx); got != tc.want {
			t.Errorf("%s: callerName = %q, want %q", name, got, tc.want)
		}
	}
}
