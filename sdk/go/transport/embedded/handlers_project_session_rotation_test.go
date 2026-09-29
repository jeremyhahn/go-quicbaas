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

package embedded

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	projectSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
)

// TestHandleProject_SessionRotationRoundTrip: set the policy in-process, read
// it back, and assert it survived; an update omitting it leaves it and one
// naming it replaces it.
func TestHandleProject_SessionRotationRoundTrip(t *testing.T) {
	svc := newProjectTestHarness(t)
	ctx := context.Background()

	out, err := handleProjectCreate(ctx, svc, &projectSDK.CreateProjectRequest{
		TenantID: "tenant-a", Name: "alpha", SessionRotation: projectSDK.SessionRotationFresh,
	})
	created := embeddedProject(t, out, err)
	assert.Equal(t, projectSDK.SessionRotationFresh, created.SessionRotation)

	get := func() projectSDK.SessionRotationPolicy {
		t.Helper()
		out, err := handleProjectGet(ctx, svc, &projectSDK.GetProjectRequest{TenantID: "tenant-a", ProjectID: created.ID})
		return embeddedProject(t, out, err).SessionRotation
	}

	out, err = handleProjectUpdate(ctx, svc, &projectSDK.UpdateProjectRequest{
		TenantID: "tenant-a", ProjectID: created.ID, Description: "renamed",
	})
	embeddedProject(t, out, err)
	assert.Equal(t, projectSDK.SessionRotationFresh, get(), "an update omitting the policy must leave it")

	out, err = handleProjectUpdate(ctx, svc, &projectSDK.UpdateProjectRequest{
		TenantID: "tenant-a", ProjectID: created.ID, SessionRotation: projectSDK.SessionRotationResume,
	})
	embeddedProject(t, out, err)
	assert.Equal(t, projectSDK.SessionRotationResume, get())
}

// TestHandleProject_AnUnknownSessionRotationIsRefused: the embedded transport
// surfaces the service's refusal unchanged.
func TestHandleProject_AnUnknownSessionRotationIsRefused(t *testing.T) {
	svc := newProjectTestHarness(t)
	_, err := handleProjectCreate(context.Background(), svc, &projectSDK.CreateProjectRequest{
		TenantID: "tenant-a", Name: "alpha", SessionRotation: "restart",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, orchestration.ErrSessionRotationPolicyInvalid), "err = %v", err)
}
