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

package grpc

import (
	"context"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestProjectGRPC_SessionRotationRoundTrip: set the policy over gRPC, read it
// back, and assert it survived; an update omitting it leaves it and one naming
// it replaces it.
func TestProjectGRPC_SessionRotationRoundTrip(t *testing.T) {
	h := newRealProjectHandler(t)

	created := grpcProject(t, h, "Create", &project.CreateProjectRequest{
		TenantID: "t1", Name: "api", SessionRotation: project.SessionRotationFresh,
	})
	if created.SessionRotation != project.SessionRotationFresh {
		t.Fatalf("create returned session_rotation %q, want fresh", created.SessionRotation)
	}
	get := func() project.SessionRotationPolicy {
		t.Helper()
		return grpcProject(t, h, "Get", &project.GetProjectRequest{TenantID: "t1", ProjectID: created.ID}).SessionRotation
	}

	grpcProject(t, h, "Update", &project.UpdateProjectRequest{TenantID: "t1", ProjectID: created.ID, Description: "renamed"})
	if got := get(); got != project.SessionRotationFresh {
		t.Fatalf("an update omitting the policy changed it to %q", got)
	}
	grpcProject(t, h, "Update", &project.UpdateProjectRequest{
		TenantID: "t1", ProjectID: created.ID, SessionRotation: project.SessionRotationResume,
	})
	if got := get(); got != project.SessionRotationResume {
		t.Fatalf("session_rotation = %q, want resume", got)
	}
}

// TestProjectGRPC_AnUnknownSessionRotationIsRefused: the refusal is the
// service's, surfaced as invalid input naming the field.
func TestProjectGRPC_AnUnknownSessionRotationIsRefused(t *testing.T) {
	h := newRealProjectHandler(t)
	_, err := h.handleRequest(context.Background(), projectServicePrefix, "Create", &project.CreateProjectRequest{
		TenantID: "t1", Name: "api", SessionRotation: "restart",
	})
	if !errors.IsInvalidInput(err) || !strings.Contains(err.Error(), "session_rotation") {
		t.Fatalf("create with an unknown policy: err = %v, want an invalid-input refusal naming session_rotation", err)
	}
}
