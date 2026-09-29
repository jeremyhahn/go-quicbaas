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
	stderrors "errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// newEphemeralProject creates a project on the ephemeral tier with both sizes
// set, so an update test can tell "left alone" from "cleared".
func newEphemeralProject(t *testing.T, svc ProjectService) string {
	t.Helper()
	resp, err := svc.CreateProject(context.Background(), &CreateProjectRequest{
		TenantID:          "tenant-1",
		Name:              "api",
		WorkspaceTier:     orchestration.WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  4096,
		ContainerMemoryMB: 8192,
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return resp.Project.ID
}

func storedStorage(t *testing.T, svc ProjectService, id string) orchestration.ProjectStorage {
	t.Helper()
	got, err := svc.GetProject(context.Background(), &GetProjectRequest{TenantID: "tenant-1", ProjectID: id})
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	return got.Project.Storage
}

// TestProjectService_CreateProject_CarriesStorage: the three fields reach the
// durable record, and read back from it.
func TestProjectService_CreateProject_CarriesStorage(t *testing.T) {
	svc := newAutonomyProjectService()
	id := newEphemeralProject(t, svc)

	want := orchestration.ProjectStorage{
		WorkspaceTier:     orchestration.WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  4096,
		ContainerMemoryMB: 8192,
	}
	if got := storedStorage(t, svc, id); got != want {
		t.Fatalf("stored storage = %+v, want %+v", got, want)
	}
}

// TestProjectService_CreateProject_DefaultsToUnsetStorage: an unconfigured
// project keeps the zero value, which is the host bind mount it always had.
func TestProjectService_CreateProject_DefaultsToUnsetStorage(t *testing.T) {
	svc := newAutonomyProjectService()
	resp, err := svc.CreateProject(context.Background(), &CreateProjectRequest{TenantID: "tenant-1", Name: "api"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if got := storedStorage(t, svc, resp.Project.ID); got != (orchestration.ProjectStorage{}) {
		t.Fatalf("stored storage = %+v, want the zero value", got)
	}
}

// assertStorageRefusal asserts the refusal every transport maps: a
// *ProjectInvalidError naming the flat wire key, still matchable as the
// domain *orchestration.InputValidationError.
func assertStorageRefusal(t *testing.T, err error, field string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected the storage configuration to be refused")
	}
	var invalid *ProjectInvalidError
	if !stderrors.As(err, &invalid) {
		t.Fatalf("err = %T %v, want *ProjectInvalidError", err, err)
	}
	if invalid.Field != field {
		t.Errorf("Field = %q, want %q", invalid.Field, field)
	}
	var input *orchestration.InputValidationError
	if !stderrors.As(err, &input) {
		t.Fatalf("err = %v does not carry *orchestration.InputValidationError", err)
	}
	if !stderrors.Is(err, orchestration.ErrInvalidInput) {
		t.Errorf("err = %v is not orchestration.ErrInvalidInput", err)
	}
}

// TestProjectService_CreateProject_RefusesInvalidStorage: the service layer is
// where a bad tier is refused, so no transport can forget to.
func TestProjectService_CreateProject_RefusesInvalidStorage(t *testing.T) {
	cases := []struct {
		name  string
		req   CreateProjectRequest
		field string
	}{
		{"unknown tier", CreateProjectRequest{WorkspaceTier: "ephemeral-ish"}, "workspace_tier"},
		{"negative tmpfs", CreateProjectRequest{WorkspaceTier: orchestration.WorkspaceTierEphemeral, WorkspaceTmpfsMB: -1}, "workspace_tmpfs_mb"},
		{"negative memory", CreateProjectRequest{ContainerMemoryMB: -1}, "container_memory_mb"},
		{"tmpfs off the ephemeral tier", CreateProjectRequest{WorkspaceTier: orchestration.WorkspaceTierPersistent, WorkspaceTmpfsMB: 512}, "workspace_tmpfs_mb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newAutonomyProjectService()
			req := tc.req
			req.TenantID, req.Name = "tenant-1", "api"
			_, err := svc.CreateProject(context.Background(), &req)
			assertStorageRefusal(t, err, tc.field)

			list, listErr := svc.ListProjects(context.Background(), &ListProjectsRequest{TenantID: "tenant-1"})
			if listErr != nil {
				t.Fatalf("ListProjects: %v", listErr)
			}
			if len(list.Projects) != 0 {
				t.Fatalf("a refused create stored %d project(s)", len(list.Projects))
			}
		})
	}
}

// TestProjectService_UpdateProject_AbsentSizesLeaveStorage: nil is "leave it
// alone", so an unrelated update does not touch the tier or its sizes.
func TestProjectService_UpdateProject_AbsentSizesLeaveStorage(t *testing.T) {
	svc := newAutonomyProjectService()
	id := newEphemeralProject(t, svc)

	if _, err := svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID: "tenant-1", ProjectID: id, Description: "renamed",
	}); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	want := orchestration.ProjectStorage{
		WorkspaceTier:     orchestration.WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  4096,
		ContainerMemoryMB: 8192,
	}
	if got := storedStorage(t, svc, id); got != want {
		t.Fatalf("stored storage = %+v, want it untouched %+v", got, want)
	}
}

// TestProjectService_UpdateProject_ZeroSizesClearStorage: zero is a real
// instruction — an unsized tmpfs, the runtime's default memory limit.
func TestProjectService_UpdateProject_ZeroSizesClearStorage(t *testing.T) {
	svc := newAutonomyProjectService()
	id := newEphemeralProject(t, svc)

	if _, err := svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID: "tenant-1", ProjectID: id,
		WorkspaceTmpfsMB: intPtr(0), ContainerMemoryMB: intPtr(0),
	}); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	want := orchestration.ProjectStorage{WorkspaceTier: orchestration.WorkspaceTierEphemeral}
	if got := storedStorage(t, svc, id); got != want {
		t.Fatalf("stored storage = %+v, want both sizes cleared %+v", got, want)
	}
}

// TestProjectService_UpdateProject_ChangesTier: a tier move that also clears
// the tmpfs size is accepted, because the merged record is what is validated.
func TestProjectService_UpdateProject_ChangesTier(t *testing.T) {
	svc := newAutonomyProjectService()
	id := newEphemeralProject(t, svc)

	if _, err := svc.UpdateProject(context.Background(), &UpdateProjectRequest{
		TenantID: "tenant-1", ProjectID: id,
		WorkspaceTier: orchestration.WorkspaceTierPersistent, WorkspaceTmpfsMB: intPtr(0),
	}); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	want := orchestration.ProjectStorage{WorkspaceTier: orchestration.WorkspaceTierPersistent, ContainerMemoryMB: 8192}
	if got := storedStorage(t, svc, id); got != want {
		t.Fatalf("stored storage = %+v, want %+v", got, want)
	}
}

// TestProjectService_UpdateProject_RefusesInvalidStorage: the update applier
// validates the MERGED storage, and a refusal stores nothing.
func TestProjectService_UpdateProject_RefusesInvalidStorage(t *testing.T) {
	cases := []struct {
		name  string
		req   UpdateProjectRequest
		field string
	}{
		{"unknown tier", UpdateProjectRequest{WorkspaceTier: "ramdisk"}, "workspace_tier"},
		{"negative tmpfs", UpdateProjectRequest{WorkspaceTmpfsMB: intPtr(-1)}, "workspace_tmpfs_mb"},
		{"negative memory", UpdateProjectRequest{ContainerMemoryMB: intPtr(-5)}, "container_memory_mb"},
		// The stored tmpfs size survives a tier move that forgot to clear it.
		{"tier move leaves a tmpfs size", UpdateProjectRequest{WorkspaceTier: orchestration.WorkspaceTierPersistent}, "workspace_tmpfs_mb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newAutonomyProjectService()
			id := newEphemeralProject(t, svc)
			req := tc.req
			req.TenantID, req.ProjectID = "tenant-1", id
			_, err := svc.UpdateProject(context.Background(), &req)
			assertStorageRefusal(t, err, tc.field)

			want := orchestration.ProjectStorage{
				WorkspaceTier:     orchestration.WorkspaceTierEphemeral,
				WorkspaceTmpfsMB:  4096,
				ContainerMemoryMB: 8192,
			}
			if got := storedStorage(t, svc, id); got != want {
				t.Fatalf("a refused update changed the stored storage to %+v", got)
			}
		})
	}
}
