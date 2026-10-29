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

	quicbaaserrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	projectSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
)

// newProjectLifecycleClient returns an SDK project client over the embedded
// transport, backed by the real project service of newProjectTestHarness.
func newProjectLifecycleClient(t *testing.T) (projectSDK.Client, *mockQuicBaaSForProject) {
	t.Helper()
	svc := newProjectTestHarness(t)
	tr, err := New(svc)
	require.NoError(t, err)
	return projectSDK.NewClient(tr), svc
}

// TestEmbeddedProject_DeletedProjectIsNotFound asserts that a delete through
// the embedded transport removes the project, and that a later Get reaches
// the caller as the service's typed ProjectNotFoundError naming the project.
func TestEmbeddedProject_DeletedProjectIsNotFound(t *testing.T) {
	client, _ := newProjectLifecycleClient(t)
	ctx := context.Background()

	created, err := client.Create(ctx, &projectSDK.CreateProjectRequest{TenantID: "tenant-a", Name: "doomed"})
	require.NoError(t, err)
	fetched, err := client.Get(ctx, "tenant-a", created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, fetched.ID)

	require.NoError(t, client.Delete(ctx, "tenant-a", created.ID))

	_, err = client.Get(ctx, "tenant-a", created.ID)
	var notFound *service.ProjectNotFoundError
	require.True(t, errors.As(err, &notFound), "want *service.ProjectNotFoundError, got %T: %v", err, err)
	assert.Equal(t, created.ID, notFound.ProjectID)
}

// TestEmbeddedProject_ListSeesOnlyTheRequestedTenant asserts that List over
// the embedded transport returns the requested tenant's projects and none of
// another tenant's.
func TestEmbeddedProject_ListSeesOnlyTheRequestedTenant(t *testing.T) {
	client, _ := newProjectLifecycleClient(t)
	ctx := context.Background()

	pA, err := client.Create(ctx, &projectSDK.CreateProjectRequest{TenantID: "tenant-a", Name: "shared-name"})
	require.NoError(t, err)
	pB, err := client.Create(ctx, &projectSDK.CreateProjectRequest{TenantID: "tenant-b", Name: "shared-name"})
	require.NoError(t, err)

	cases := []struct {
		tenant string
		own    string
		other  string
	}{
		{"tenant-a", pA.ID, pB.ID},
		{"tenant-b", pB.ID, pA.ID},
	}
	for _, tc := range cases {
		t.Run(tc.tenant, func(t *testing.T) {
			listed, err := client.List(ctx, &projectSDK.ListProjectsRequest{TenantID: tc.tenant})
			require.NoError(t, err)
			ids := make([]string, 0, len(listed.Projects))
			for _, p := range listed.Projects {
				ids = append(ids, p.ID)
			}
			assert.Equal(t, []string{tc.own}, ids, "a tenant sees its own project and no other tenant's")
		})
	}
}

// TestEmbeddedProject_GrantThenRevokeFleetNodeRole asserts that a role
// granted over the embedded transport is persisted on the project, and that
// a revoke persists the explicit denied role.
func TestEmbeddedProject_GrantThenRevokeFleetNodeRole(t *testing.T) {
	client, svc := newProjectLifecycleClient(t)
	ctx := context.Background()

	created, err := client.Create(ctx, &projectSDK.CreateProjectRequest{TenantID: "tenant-a", Name: "perms"})
	require.NoError(t, err)
	persistedRole := func() orchestration.ProjectRole {
		t.Helper()
		got, err := svc.Project().GetProject(ctx, &service.GetProjectRequest{TenantID: "tenant-a", ProjectID: created.ID})
		require.NoError(t, err)
		return got.Project.Permissions.FleetNodeRoles["node-7"]
	}

	granted, err := client.GrantFleetNodeRole(ctx, &projectSDK.GrantFleetNodeRoleRequest{
		TenantID: "tenant-a", ProjectID: created.ID, FleetNodeID: "node-7", Role: projectSDK.ProjectRoleWorker,
	})
	require.NoError(t, err)
	assert.Equal(t, projectSDK.ProjectRoleWorker, granted.Role)
	assert.Equal(t, orchestration.ProjectRoleWorker, persistedRole())

	revoked, err := client.RevokeFleetNodeRole(ctx, &projectSDK.RevokeFleetNodeRoleRequest{
		TenantID: "tenant-a", ProjectID: created.ID, FleetNodeID: "node-7",
	})
	require.NoError(t, err)
	assert.Equal(t, "node-7", revoked.FleetNodeID)
	assert.Equal(t, orchestration.ProjectRoleDenied, persistedRole())
}

// TestEmbeddedProject_ADanglingHostAffinityIsRefusedAndStoresNothing asserts
// that a host affinity naming no fleet node reaches the embedded caller as a
// typed ReferenceNotFoundError on host_affinity, and that the refused create
// leaves no project behind. The harness wires the fleet by hand; that server
// Start wires it is checked in test/integration/project.
func TestEmbeddedProject_ADanglingHostAffinityIsRefusedAndStoresNothing(t *testing.T) {
	client, _ := newProjectLifecycleClient(t)
	ctx := context.Background()

	_, err := client.Create(ctx, &projectSDK.CreateProjectRequest{
		TenantID: "tenant-a", Name: "dangling", HostAffinity: "host-nowhere",
	})
	var notFound *quicbaaserrors.ReferenceNotFoundError
	require.True(t, errors.As(err, &notFound), "want *ReferenceNotFoundError, got %T: %v", err, err)
	assert.Equal(t, "host_affinity", notFound.Field)

	listed, err := client.List(ctx, &projectSDK.ListProjectsRequest{TenantID: "tenant-a"})
	require.NoError(t, err)
	assert.Empty(t, listed.Projects, "a refused create must store nothing")
}
