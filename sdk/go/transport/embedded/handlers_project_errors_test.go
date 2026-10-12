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

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	projectSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
)

// TestEmbeddedProject_AuthzErrorsReachTheCallerTyped: the embedded transport
// has no status-code channel, so the typed authorization refusals the gRPC
// transport maps to PermissionDenied / NotFound / InvalidArgument must reach
// an in-process caller intact (go-quicbaas#1261).
func TestEmbeddedProject_AuthzErrorsReachTheCallerTyped(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		check func(error) bool
	}{
		{"access denied", &service.ProjectAccessDeniedError{ProjectID: "p1", FleetNodeID: "n1"},
			func(err error) bool { var e *service.ProjectAccessDeniedError; return errors.As(err, &e) }},
		{"unresolved", &service.ProjectUnresolvedError{TenantID: "t1", ProjectID: "p1"},
			func(err error) bool { var e *service.ProjectUnresolvedError; return errors.As(err, &e) }},
		{"invalid role", &service.ProjectRoleInvalidError{Role: "boss"},
			func(err error) bool { var e *service.ProjectRoleInvalidError; return errors.As(err, &e) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			facade := mocks.NewMockQuicBaaS()
			facade.MockProjectService = &mocks.MockProjectService{
				GetProjectFunc: func(context.Context, *service.GetProjectRequest) (*service.GetProjectResponse, error) {
					return nil, tc.err
				},
				GetProjectByNameFunc: func(context.Context, *service.GetProjectByNameRequest) (*service.GetProjectResponse, error) {
					return nil, tc.err
				},
				UpdateProjectFunc: func(context.Context, *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
					return nil, tc.err
				},
			}
			tr, err := New(facade)
			require.NoError(t, err)
			client := projectSDK.NewClient(tr)
			ctx := context.Background()

			_, err = client.Get(ctx, "t1", "p1")
			assert.True(t, tc.check(err), "Get: %v lost its type", err)
			_, err = client.GetByName(ctx, "t1", "p1")
			assert.True(t, tc.check(err), "GetByName: %v lost its type", err)
			_, err = client.Update(ctx, &projectSDK.UpdateProjectRequest{TenantID: "t1", ProjectID: "p1", Description: "d"})
			assert.True(t, tc.check(err), "Update: %v lost its type", err)
		})
	}
}

// TestEmbeddedProject_SuccessCarriesNoError pins the other side: a service
// answer reaches the caller as a ProjectInfo, not an error.
func TestEmbeddedProject_SuccessCarriesNoError(t *testing.T) {
	svc := newProjectTestHarness(t)
	tr, err := New(svc)
	require.NoError(t, err)
	client := projectSDK.NewClient(tr)
	ctx := context.Background()

	created, err := client.Create(ctx, &projectSDK.CreateProjectRequest{TenantID: "tenant-a", Name: "alpha"})
	require.NoError(t, err)
	got, err := client.GetByName(ctx, "tenant-a", "alpha")
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
}
