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
	stderrors "errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// storedSessionRotation reads a project's stored session rotation policy back.
func storedSessionRotation(t *testing.T, svc ProjectService, id string) orchestration.SessionRotationPolicy {
	t.Helper()
	got, err := svc.GetProject(context.Background(), &GetProjectRequest{TenantID: "tenant-1", ProjectID: id})
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	return got.Project.SessionRotation
}

// assertSessionRotationRefusal asserts the refusal every transport maps: a
// *ProjectInvalidError naming session_rotation, still matchable as the domain
// error.
func assertSessionRotationRefusal(t *testing.T, err error) {
	t.Helper()
	var invalid *ProjectInvalidError
	if !stderrors.As(err, &invalid) {
		t.Fatalf("err = %T %v, want *ProjectInvalidError", err, err)
	}
	if invalid.Field != "session_rotation" {
		t.Errorf("Field = %q, want session_rotation", invalid.Field)
	}
	if !stderrors.Is(err, orchestration.ErrSessionRotationPolicyInvalid) {
		t.Errorf("err = %v is not ErrSessionRotationPolicyInvalid", err)
	}
}

// TestProjectService_SessionRotation_CreateStoresTheChoice: a project created
// with a policy keeps it; one created without keeps the unset value, which
// runs resume.
func TestProjectService_SessionRotation_CreateStoresTheChoice(t *testing.T) {
	svc := newAutonomyProjectService()
	fresh, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID: "tenant-1", Name: "fresh", SessionRotation: orchestration.SessionRotationFresh,
	})
	if err != nil {
		t.Fatalf("CreateProject(fresh): %v", err)
	}
	if got := storedSessionRotation(t, svc, fresh.Project.ID); got != orchestration.SessionRotationFresh {
		t.Errorf("stored policy = %q, want fresh", got)
	}

	unset, err := svc.CreateProject(context.Background(), &CreateProjectRequest{TenantID: "tenant-1", Name: "unset"})
	if err != nil {
		t.Fatalf("CreateProject(unset): %v", err)
	}
	got := storedSessionRotation(t, svc, unset.Project.ID)
	if got != orchestration.SessionRotationUnset || got.Effective() != orchestration.SessionRotationResume {
		t.Errorf("stored policy = %q (effective %q), want unset running resume", got, got.Effective())
	}
}

// TestProjectService_SessionRotation_CreateRefusesAnUnknownPolicy: the service
// is where a bad policy is refused, so no transport can forget to.
func TestProjectService_SessionRotation_CreateRefusesAnUnknownPolicy(t *testing.T) {
	svc := newAutonomyProjectService()
	_, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID: "tenant-1", Name: "api", SessionRotation: "frseh",
	})
	assertSessionRotationRefusal(t, err)
}

// TestProjectService_SessionRotation_UpdateIsPresenceBased: an update that
// does not name a policy leaves it alone; one that does replaces it, including
// back to the default by naming it.
func TestProjectService_SessionRotation_UpdateIsPresenceBased(t *testing.T) {
	svc := newAutonomyProjectService()
	created, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID: "tenant-1", Name: "api", SessionRotation: orchestration.SessionRotationFresh,
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	id := created.Project.ID

	if _, err := svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID: "tenant-1", ProjectID: id, Description: "renamed",
	}); err != nil {
		t.Fatalf("UpdateProject(unrelated): %v", err)
	}
	if got := storedSessionRotation(t, svc, id); got != orchestration.SessionRotationFresh {
		t.Errorf("an update naming no policy changed it to %q", got)
	}

	if _, err := svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID: "tenant-1", ProjectID: id, SessionRotation: orchestration.SessionRotationResume,
	}); err != nil {
		t.Fatalf("UpdateProject(resume): %v", err)
	}
	if got := storedSessionRotation(t, svc, id); got != orchestration.SessionRotationResume {
		t.Errorf("stored policy = %q, want resume", got)
	}
}

// TestProjectService_SessionRotation_UpdateRefusesAnUnknownPolicy: a refused
// update leaves the stored policy as it was.
func TestProjectService_SessionRotation_UpdateRefusesAnUnknownPolicy(t *testing.T) {
	svc := newAutonomyProjectService()
	created, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID: "tenant-1", Name: "api", SessionRotation: orchestration.SessionRotationFresh,
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	_, err = svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID: "tenant-1", ProjectID: created.Project.ID, SessionRotation: "restart",
	})
	assertSessionRotationRefusal(t, err)
	if got := storedSessionRotation(t, svc, created.Project.ID); got != orchestration.SessionRotationFresh {
		t.Errorf("a refused update changed the policy to %q", got)
	}
}
