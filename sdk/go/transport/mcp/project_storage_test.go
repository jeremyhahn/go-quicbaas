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

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
)

// TestProjectMCPArgumentsCarryStorage asserts the tool arguments derived from
// the SDK request use the flat keys pkg/api/mcp's project_create and
// project_update declare, and that an unset size is ABSENT — the tool reads
// absent as "leave it alone" and zero as "lift the limit".
func TestProjectMCPArgumentsCarryStorage(t *testing.T) {
	tr := &Transport{}

	create := tr.convertToArguments(&project.CreateProjectRequest{
		TenantID: "t1", Name: "api",
		WorkspaceTier:     project.WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  4096,
		ContainerMemoryMB: 8192,
	})
	for key, want := range map[string]interface{}{
		"workspace_tier":      "ephemeral",
		"workspace_tmpfs_mb":  float64(4096),
		"container_memory_mb": float64(8192),
	} {
		if create[key] != want {
			t.Errorf("create %s = %#v, want %#v", key, create[key], want)
		}
	}

	unchanged := tr.convertToArguments(&project.UpdateProjectRequest{TenantID: "t1", ProjectID: "p1"})
	for _, key := range []string{"workspace_tier", "workspace_tmpfs_mb", "container_memory_mb"} {
		if _, present := unchanged[key]; present {
			t.Errorf("an unset %s appeared in the update arguments", key)
		}
	}

	zero := 0
	cleared := tr.convertToArguments(&project.UpdateProjectRequest{
		TenantID: "t1", ProjectID: "p1", WorkspaceTmpfsMB: &zero, ContainerMemoryMB: &zero,
	})
	for _, key := range []string{"workspace_tmpfs_mb", "container_memory_mb"} {
		if v, present := cleared[key]; !present || v != float64(0) {
			t.Errorf("update %s = %#v (present=%v), want an explicit 0", key, v, present)
		}
	}
}

// TestProjectMCPTransformResponse_CarriesStorage drives the tool result of the
// REAL project service — create, then read back — through the transform, so
// the SDK hydrates storage from the key the server actually sends.
func TestProjectMCPTransformResponse_CarriesStorage(t *testing.T) {
	h := newProjectMCPHandler()
	svc := service.NewProjectService(orchestration.NewInMemoryProjectStore())
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, &service.CreateProjectRequest{
		TenantID: "t1", Name: "api",
		WorkspaceTier:     orchestration.WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  4096,
		ContainerMemoryMB: 8192,
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	got, err := svc.GetProject(ctx, &service.GetProjectRequest{TenantID: "t1", ProjectID: created.Project.ID})
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}

	want := project.ProjectStorage{
		WorkspaceTier:     project.WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  4096,
		ContainerMemoryMB: 8192,
	}
	for method, envelope := range map[string]interface{}{
		"Create": &service.CreateProjectResponse{Project: created.Project, Timestamp: time.Now()},
		"Get":    got,
	} {
		out, err := h.transformResponse(method, toolResult(t, envelope))
		if err != nil {
			t.Fatalf("transformResponse(%s): %v", method, err)
		}
		info, ok := out.(*project.ProjectInfo)
		if !ok {
			t.Fatalf("transformResponse(%s) returned %T", method, out)
		}
		if info.Storage != want {
			t.Errorf("%s: storage = %+v, want %+v", method, info.Storage, want)
		}
	}
}
