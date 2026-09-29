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
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	projectSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
)

func embeddedProject(t *testing.T, out interface{}, err error) *projectSDK.ProjectInfo {
	t.Helper()
	require.NoError(t, err)
	info, ok := out.(*projectSDK.ProjectInfo)
	require.True(t, ok, "handler returned %T", out)
	return info
}

// TestHandleProject_StorageRoundTrip: set the tier in-process, read it back,
// and assert it survived; an update omitting the sizes leaves them and one
// sending zero clears them.
func TestHandleProject_StorageRoundTrip(t *testing.T) {
	svc := newProjectTestHarness(t)
	ctx := context.Background()

	out, err := handleProjectCreate(ctx, svc, &projectSDK.CreateProjectRequest{
		TenantID: "tenant-a", Name: "alpha",
		WorkspaceTier:     projectSDK.WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  4096,
		ContainerMemoryMB: 8192,
	})
	created := embeddedProject(t, out, err)
	want := projectSDK.ProjectStorage{
		WorkspaceTier:     projectSDK.WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  4096,
		ContainerMemoryMB: 8192,
	}
	assert.Equal(t, want, created.Storage)

	get := func() projectSDK.ProjectStorage {
		t.Helper()
		out, err := handleProjectGet(ctx, svc, &projectSDK.GetProjectRequest{TenantID: "tenant-a", ProjectID: created.ID})
		return embeddedProject(t, out, err).Storage
	}
	assert.Equal(t, want, get())

	out, err = handleProjectUpdate(ctx, svc, &projectSDK.UpdateProjectRequest{
		TenantID: "tenant-a", ProjectID: created.ID, Description: "renamed",
	})
	embeddedProject(t, out, err)
	assert.Equal(t, want, get(), "an update omitting the sizes must leave them")

	zero := 0
	out, err = handleProjectUpdate(ctx, svc, &projectSDK.UpdateProjectRequest{
		TenantID: "tenant-a", ProjectID: created.ID, WorkspaceTmpfsMB: &zero, ContainerMemoryMB: &zero,
	})
	embeddedProject(t, out, err)
	assert.Equal(t, projectSDK.ProjectStorage{WorkspaceTier: projectSDK.WorkspaceTierEphemeral}, get(),
		"an update sending zero must clear the sizes")
}

// TestHandleProject_InvalidTierIsRefusedByTheService: the embedded transport
// surfaces the service's typed refusal unchanged.
func TestHandleProject_InvalidTierIsRefusedByTheService(t *testing.T) {
	svc := newProjectTestHarness(t)
	ctx := context.Background()

	_, err := handleProjectCreate(ctx, svc, &projectSDK.CreateProjectRequest{
		TenantID: "tenant-a", Name: "alpha", WorkspaceTier: "ramdisk",
	})
	var invalid *service.ProjectInvalidError
	require.True(t, errors.As(err, &invalid), "err = %v", err)
	assert.Equal(t, "workspace_tier", invalid.Field)
	var input *orchestration.InputValidationError
	assert.True(t, errors.As(err, &input), "err = %v does not carry the domain refusal", err)

	out, err := handleProjectCreate(ctx, svc, &projectSDK.CreateProjectRequest{TenantID: "tenant-a", Name: "alpha"})
	created := embeddedProject(t, out, err)
	_, err = handleProjectUpdate(ctx, svc, &projectSDK.UpdateProjectRequest{
		TenantID: "tenant-a", ProjectID: created.ID, WorkspaceTier: "ramdisk",
	})
	assert.True(t, errors.As(err, &input), "update err = %v does not carry the domain refusal", err)
}
