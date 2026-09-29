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

// TestProjectMCPArgumentsCarrySessionRotation: the tool arguments derived from
// the SDK request use the key pkg/api/mcp's project tools declare, and an
// update that does not name a policy leaves the key absent.
func TestProjectMCPArgumentsCarrySessionRotation(t *testing.T) {
	tr := &Transport{}
	create := tr.convertToArguments(&project.CreateProjectRequest{
		TenantID: "t1", Name: "api", SessionRotation: project.SessionRotationFresh,
	})
	if create["session_rotation"] != "fresh" {
		t.Errorf("create session_rotation = %#v, want \"fresh\"", create["session_rotation"])
	}
	unchanged := tr.convertToArguments(&project.UpdateProjectRequest{TenantID: "t1", ProjectID: "p1"})
	if v, present := unchanged["session_rotation"]; present {
		t.Errorf("an unset session_rotation appeared in the update arguments: %#v", v)
	}
}

// TestProjectMCPTransformResponse_CarriesSessionRotation drives the REAL
// project service's result through the transform, so the SDK hydrates the
// policy from the key the server actually sends.
func TestProjectMCPTransformResponse_CarriesSessionRotation(t *testing.T) {
	h := newProjectMCPHandler()
	svc := service.NewProjectService(orchestration.NewInMemoryProjectStore())
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, &service.CreateProjectRequest{
		TenantID: "t1", Name: "api", SessionRotation: orchestration.SessionRotationFresh,
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	out, err := h.transformResponse("Create", toolResult(t, &service.CreateProjectResponse{Project: created.Project, Timestamp: time.Now()}))
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	info, ok := out.(*project.ProjectInfo)
	if !ok {
		t.Fatalf("transformResponse returned %T", out)
	}
	if info.SessionRotation != project.SessionRotationFresh {
		t.Errorf("session_rotation = %q, want fresh", info.SessionRotation)
	}
}
