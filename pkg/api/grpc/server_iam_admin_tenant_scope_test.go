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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// newTenantObjectsIAMServer returns a server whose mock holds one admin
// assignment, one active impersonation session, one export and one deletion
// request, all in owner's tenant. Only the read verbs are expected; a test
// that reaches a write verb adds its own expectation.
func newTenantObjectsIAMServer(t *testing.T, owner string) (*IAMServer, *mockIAMService) {
	t.Helper()
	srv, svc := newTestIAMServer(t)
	svc.sessions = map[string]*iam.ImpersonationSession{
		"s1": {ID: "s1", AdminUserID: 30021, TargetUserID: 30001, TenantID: owner},
	}
	svc.On("GetAdminAssignment", mock.Anything, "a1").
		Return(&iam.AdminAssignment{ID: "a1", UserID: 30001, TenantIDs: []string{owner}}, nil).Maybe()
	svc.On("ListAdminAssignments", mock.Anything, mock.Anything).
		Return([]*iam.AdminAssignment{{ID: "a1", UserID: 30001, TenantIDs: []string{owner}}}, nil).Maybe()
	svc.On("GetActiveImpersonations", mock.Anything, uint64(30021)).
		Return([]*iam.ImpersonationSession{svc.sessions["s1"]}, nil).Maybe()
	svc.On("GetExportStatus", mock.Anything, "e1").
		Return(&iam.DataExportRequest{ID: "e1", TenantID: owner, UserID: 30001}, nil).Maybe()
	svc.On("GetDeletionStatus", mock.Anything, "d1").
		Return(&iam.DataDeletionRequest{ID: "d1", TenantID: owner, UserID: 30001}, nil).Maybe()
	return srv, svc
}

// TestIAMServer_AnotherTenantsAdminAndGDPRObjectsAreNotFound proves the gRPC
// delegated-admin, impersonation and data-request verbs answer another
// tenant's object as NotFound without writing it, refuse a request naming
// another tenant as PermissionDenied, and leave another tenant's objects out
// of their lists.
func TestIAMServer_AnotherTenantsAdminAndGDPRObjectsAreNotFound(t *testing.T) {
	t.Parallel()

	srv, svc := newTenantObjectsIAMServer(t, "tenant-b")
	intruder := tenantctx.WithTenant(context.Background(), "tenant-a")

	notFound := map[string]func() error{
		"GetAdminAssignment": func() error {
			_, err := srv.GetAdminAssignment(intruder, &iampb.GetAdminAssignmentRequest{Id: "a1"})
			return err
		},
		"UpdateAdminAssignment": func() error {
			_, err := srv.UpdateAdminAssignment(intruder, &iampb.UpdateAdminAssignmentRequest{Id: "a1", Active: true})
			return err
		},
		"DeleteAdminAssignment": func() error {
			_, err := srv.DeleteAdminAssignment(intruder, &iampb.DeleteAdminAssignmentRequest{Id: "a1"})
			return err
		},
		"EndImpersonation": func() error {
			_, err := srv.EndImpersonation(intruder, &iampb.EndImpersonationRequest{SessionId: "s1"})
			return err
		},
		"GetExportStatus": func() error {
			_, err := srv.GetExportStatus(intruder, &iampb.GetExportStatusRequest{ExportId: "e1"})
			return err
		},
		"GenerateExport": func() error {
			_, err := srv.GenerateExport(intruder, &iampb.GenerateExportRequest{ExportId: "e1"})
			return err
		},
		"CancelDeletion": func() error {
			_, err := srv.CancelDeletion(intruder, &iampb.CancelDeletionRequest{DeletionId: "d1", CancelledBy: "30021"})
			return err
		},
		"ProcessDeletion": func() error {
			_, err := srv.ProcessDeletion(intruder, &iampb.ProcessDeletionRequest{DeletionId: "d1"})
			return err
		},
		"GetDeletionStatus": func() error {
			_, err := srv.GetDeletionStatus(intruder, &iampb.GetDeletionStatusRequest{DeletionId: "d1"})
			return err
		},
	}
	for name, verb := range notFound {
		assert.Equal(t, codes.NotFound, status.Code(verb()), "%s on another tenant's object", name)
	}

	_, err := srv.CreateAdminAssignment(intruder, &iampb.CreateAdminAssignmentRequest{
		UserId: "30001", AdminRole: iampb.AdminRole_ADMIN_ROLE_TENANT_ADMIN, GrantedBy: "30021",
		TenantIds: []string{"tenant-b"},
	})
	assert.Equal(t, codes.PermissionDenied, status.Code(err), "create naming another tenant: %v", err)
	_, err = srv.ListAdminAssignments(intruder, &iampb.ListAdminAssignmentsRequest{TenantId: "tenant-b"})
	assert.Equal(t, codes.PermissionDenied, status.Code(err), "list naming another tenant: %v", err)
	_, err = srv.CheckAdminPermission(intruder, &iampb.CheckAdminPermissionRequest{
		UserId: "30001", Permission: "users:read", TenantId: "tenant-b",
	})
	assert.Equal(t, codes.PermissionDenied, status.Code(err), "check naming another tenant: %v", err)

	list, err := srv.ListAdminAssignments(intruder, &iampb.ListAdminAssignmentsRequest{})
	require.NoError(t, err)
	assert.Empty(t, list.Assignments, "another tenant's assignment must not be listed")
	assert.Zero(t, list.Total)

	sessions, err := srv.GetActiveImpersonations(intruder, &iampb.GetActiveImpersonationsRequest{AdminUserId: "30021"})
	require.NoError(t, err)
	assert.Empty(t, sessions.Sessions, "another tenant's session must not be listed")

	for _, write := range []string{
		"CreateAdminAssignment", "UpdateAdminAssignment", "DeleteAdminAssignment", "CheckAdminPermission",
		"EndImpersonation", "GenerateExport", "CancelDeletion", "ProcessDeletion",
	} {
		svc.AssertNotCalled(t, write)
	}
}

// TestIAMServer_OwnTenantAdminAndGDPRObjectsAreServed is the owning-tenant
// twin: the same objects in the caller's tenant are served and written.
func TestIAMServer_OwnTenantAdminAndGDPRObjectsAreServed(t *testing.T) {
	t.Parallel()

	srv, svc := newTenantObjectsIAMServer(t, "tenant-a")
	owner := tenantctx.WithTenant(context.Background(), "tenant-a")

	svc.On("CreateAdminAssignment", mock.Anything, mock.MatchedBy(func(opts iam.CreateAdminAssignmentOptions) bool {
		return len(opts.TenantIDs) == 1 && opts.TenantIDs[0] == "tenant-a"
	})).Return(&iam.AdminAssignment{ID: "a2", UserID: 30001, TenantIDs: []string{"tenant-a"}}, nil)
	svc.On("UpdateAdminAssignment", mock.Anything, "a1", mock.Anything).
		Return(&iam.AdminAssignment{ID: "a1", UserID: 30001, TenantIDs: []string{"tenant-a"}}, nil)
	svc.On("DeleteAdminAssignment", mock.Anything, "a1").Return(nil)
	svc.On("CheckAdminPermission", mock.Anything, mock.MatchedBy(func(req *iam.AdminCheckRequest) bool {
		return req.TenantID == "tenant-a"
	})).Return(&iam.AdminCheckResponse{Allowed: true, AssignmentID: "a1"}, nil)
	svc.On("EndImpersonation", mock.Anything, "s1").Return(nil)
	svc.On("GenerateExport", mock.Anything, "e1").
		Return(&iam.UserDataExport{ExportMetadata: iam.ExportMetadata{ExportID: "e1"}}, nil)
	svc.On("CancelDeletion", mock.Anything, "d1", "30021").Return(nil)
	svc.On("ProcessDeletion", mock.Anything, "d1").Return(&iam.DeletionSummary{ProfileDeleted: true}, nil)

	created, err := srv.CreateAdminAssignment(owner, &iampb.CreateAdminAssignmentRequest{
		UserId: "30001", AdminRole: iampb.AdminRole_ADMIN_ROLE_TENANT_ADMIN, GrantedBy: "30021",
		TenantIds: []string{"tenant-a"},
	})
	require.NoError(t, err)
	assert.Equal(t, "a2", created.Id)

	got, err := srv.GetAdminAssignment(owner, &iampb.GetAdminAssignmentRequest{Id: "a1"})
	require.NoError(t, err)
	assert.Equal(t, "a1", got.Id)
	_, err = srv.UpdateAdminAssignment(owner, &iampb.UpdateAdminAssignmentRequest{Id: "a1", Active: true})
	require.NoError(t, err)
	_, err = srv.DeleteAdminAssignment(owner, &iampb.DeleteAdminAssignmentRequest{Id: "a1"})
	require.NoError(t, err)

	list, err := srv.ListAdminAssignments(owner, &iampb.ListAdminAssignmentsRequest{TenantId: "tenant-a"})
	require.NoError(t, err)
	assert.Len(t, list.Assignments, 1)
	check, err := srv.CheckAdminPermission(owner, &iampb.CheckAdminPermissionRequest{
		UserId: "30001", Permission: "users:read", TenantId: "tenant-a",
	})
	require.NoError(t, err)
	assert.True(t, check.Allowed)

	sessions, err := srv.GetActiveImpersonations(owner, &iampb.GetActiveImpersonationsRequest{AdminUserId: "30021"})
	require.NoError(t, err)
	assert.Len(t, sessions.Sessions, 1)
	_, err = srv.EndImpersonation(owner, &iampb.EndImpersonationRequest{SessionId: "s1"})
	require.NoError(t, err)

	_, err = srv.GetExportStatus(owner, &iampb.GetExportStatusRequest{ExportId: "e1"})
	require.NoError(t, err)
	_, err = srv.GenerateExport(owner, &iampb.GenerateExportRequest{ExportId: "e1"})
	require.NoError(t, err)

	_, err = srv.GetDeletionStatus(owner, &iampb.GetDeletionStatusRequest{DeletionId: "d1"})
	require.NoError(t, err)
	_, err = srv.CancelDeletion(owner, &iampb.CancelDeletionRequest{DeletionId: "d1", CancelledBy: "30021"})
	require.NoError(t, err)
	_, err = srv.ProcessDeletion(owner, &iampb.ProcessDeletionRequest{DeletionId: "d1"})
	require.NoError(t, err)

	svc.AssertExpectations(t)
}
