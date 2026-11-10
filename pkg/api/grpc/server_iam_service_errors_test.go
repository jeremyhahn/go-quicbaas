// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestIAMServer_ServiceError checks that every IAMServer RPC maps the error
// its IAM service call returns onto the matching gRPC status code.
func TestIAMServer_ServiceError(t *testing.T) {
	ctx := callerContext("t-1")
	notFound := &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"}
	internal := &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"}
	exists := &dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "test"}
	forbidden := &dberrors.QBError{Code: dberrors.ErrForbidden, Op: "test"}

	tests := []struct {
		name  string
		setup func(svc *mockIAMService)
		call  func(srv *IAMServer) error
		want  codes.Code
		// after holds extra assertions on the service mock, when a row needs them.
		after func(t *testing.T, svc *mockIAMService)
	}{
		// Tenant operations.
		{
			name:  "GetTenant",
			setup: func(svc *mockIAMService) { svc.On("GetTenant", mock.Anything, "t-1").Return(nil, notFound) },
			call: func(srv *IAMServer) error {
				_, err := srv.GetTenant(ctx, &iampb.GetTenantRequest{Id: "t-1"})
				return err
			},
			want: codes.NotFound,
		},
		{
			name: "UpdateTenant",
			setup: func(svc *mockIAMService) {
				svc.On("GetTenant", mock.Anything, mock.Anything).Return(&iam.Tenant{ID: "t1"}, nil)
				svc.On("UpdateTenant", mock.Anything, mock.Anything).Return(nil, internal)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.UpdateTenant(ctx, &iampb.UpdateTenantRequest{Id: "t-1", Name: "n"})
				return err
			},
			want: codes.Internal,
		},
		{
			name:  "DeleteTenant",
			setup: func(svc *mockIAMService) { svc.On("DeleteTenant", mock.Anything, "t-1").Return(notFound) },
			call: func(srv *IAMServer) error {
				_, err := srv.DeleteTenant(ctx, &iampb.DeleteTenantRequest{Id: "t-1"})
				return err
			},
			want: codes.NotFound,
		},

		// User operations.
		{
			name:  "GetUser",
			setup: func(svc *mockIAMService) { svc.On("GetUser", mock.Anything, "t-1", "u-1").Return(nil, notFound) },
			call: func(srv *IAMServer) error {
				_, err := srv.GetUser(ctx, &iampb.GetUserRequest{TenantId: "t-1", UserId: "u-1"})
				return err
			},
			want: codes.NotFound,
		},
		{
			name:  "CreateUser",
			setup: func(svc *mockIAMService) { svc.On("CreateUser", mock.Anything, mock.Anything).Return(nil, exists) },
			call: func(srv *IAMServer) error {
				_, err := srv.CreateUser(ctx, &iampb.CreateUserRequest{TenantId: "t-1", Email: "a@b.c", Name: "user"})
				return err
			},
			want: codes.AlreadyExists,
		},
		{
			name:  "UpdateUser",
			setup: func(svc *mockIAMService) { svc.On("UpdateUser", mock.Anything, mock.Anything).Return(nil, internal) },
			call: func(srv *IAMServer) error {
				_, err := srv.UpdateUser(ctx, &iampb.UpdateUserRequest{TenantId: "t-1", UserId: "10001", Email: "a@b.c"})
				return err
			},
			want: codes.Internal,
		},
		{
			name:  "DeleteUser",
			setup: func(svc *mockIAMService) { svc.On("DeleteUser", mock.Anything, "t-1", "u-1").Return(forbidden) },
			call: func(srv *IAMServer) error {
				_, err := srv.DeleteUser(ctx, &iampb.DeleteUserRequest{TenantId: "t-1", UserId: "u-1"})
				return err
			},
			want: codes.PermissionDenied,
		},
		{
			name: "ListUsers",
			setup: func(svc *mockIAMService) {
				svc.On("ListUsers", mock.Anything, "t-1", 10, 0, mock.Anything).Return(nil, 0, internal)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.ListUsers(ctx, &iampb.ListUsersRequest{TenantId: "t-1", Limit: 10})
				return err
			},
			want: codes.Internal,
		},

		// Role operations.
		{
			name:  "CreateRole",
			setup: func(svc *mockIAMService) { svc.On("CreateRole", mock.Anything, mock.Anything).Return(nil, exists) },
			call: func(srv *IAMServer) error {
				_, err := srv.CreateRole(ctx, &iampb.CreateRoleRequest{TenantId: "t-1", Id: "r-1", Name: "role"})
				return err
			},
			want: codes.AlreadyExists,
		},
		{
			name:  "GetRole",
			setup: func(svc *mockIAMService) { svc.On("GetRole", mock.Anything, "t-1", "r-1").Return(nil, notFound) },
			call: func(srv *IAMServer) error {
				_, err := srv.GetRole(ctx, &iampb.GetRoleRequest{TenantId: "t-1", RoleId: "r-1"})
				return err
			},
			want: codes.NotFound,
		},
		{
			name:  "UpdateRole",
			setup: func(svc *mockIAMService) { svc.On("UpdateRole", mock.Anything, mock.Anything).Return(nil, internal) },
			call: func(srv *IAMServer) error {
				_, err := srv.UpdateRole(ctx, &iampb.UpdateRoleRequest{TenantId: "t-1", RoleId: "r-1", Name: "role"})
				return err
			},
			want: codes.Internal,
		},
		{
			name:  "DeleteRole",
			setup: func(svc *mockIAMService) { svc.On("DeleteRole", mock.Anything, "t-1", "r-1").Return(notFound) },
			call: func(srv *IAMServer) error {
				_, err := srv.DeleteRole(ctx, &iampb.DeleteRoleRequest{TenantId: "t-1", RoleId: "r-1"})
				return err
			},
			want: codes.NotFound,
		},
		{
			name:  "ListRoles",
			setup: func(svc *mockIAMService) { svc.On("ListRoles", mock.Anything, "t-1", 10, 0).Return(nil, 0, internal) },
			call: func(srv *IAMServer) error {
				_, err := srv.ListRoles(ctx, &iampb.ListRolesRequest{TenantId: "t-1", Limit: 10})
				return err
			},
			want: codes.Internal,
		},
		{
			name: "AssignRole",
			setup: func(svc *mockIAMService) {
				svc.On("AssignRole", mock.Anything, "t-1", uint64(1), "r-1").Return(notFound)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.AssignRole(ctx, &iampb.AssignRoleRequest{TenantId: "t-1", UserId: "1", Role: "r-1"})
				return err
			},
			want: codes.NotFound,
		},
		{
			name: "RevokeRole",
			setup: func(svc *mockIAMService) {
				svc.On("RevokeRole", mock.Anything, "t-1", uint64(1), "r-1").Return(notFound)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.RevokeRole(ctx, &iampb.RevokeRoleRequest{TenantId: "t-1", UserId: "1", Role: "r-1"})
				return err
			},
			want: codes.NotFound,
		},

		// Application operations.
		{
			name:  "RegisterApp",
			setup: func(svc *mockIAMService) { svc.On("RegisterApp", mock.Anything, mock.Anything).Return(nil, exists) },
			call: func(srv *IAMServer) error {
				_, err := srv.RegisterApp(ctx, &iampb.RegisterAppRequest{TenantId: "t-1", Name: "app"})
				return err
			},
			want: codes.AlreadyExists,
		},
		{
			name:  "GetApp",
			setup: func(svc *mockIAMService) { svc.On("GetApp", mock.Anything, "t-1", "c-1").Return(nil, notFound) },
			call: func(srv *IAMServer) error {
				_, err := srv.GetApp(ctx, &iampb.GetAppRequest{TenantId: "t-1", ClientId: "c-1"})
				return err
			},
			want: codes.NotFound,
		},
		{
			name: "UpdateApp",
			setup: func(svc *mockIAMService) {
				svc.On("UpdateApp", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, internal)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.UpdateApp(ctx, &iampb.UpdateAppRequest{TenantId: "t-1", ClientId: "c-1", Name: "app"})
				return err
			},
			want: codes.Internal,
		},
		{
			name:  "DeleteApp",
			setup: func(svc *mockIAMService) { svc.On("DeleteApp", mock.Anything, "t-1", "c-1").Return(notFound) },
			call: func(srv *IAMServer) error {
				_, err := srv.DeleteApp(ctx, &iampb.DeleteAppRequest{TenantId: "t-1", ClientId: "c-1"})
				return err
			},
			want: codes.NotFound,
		},
		{
			name:  "ListApps",
			setup: func(svc *mockIAMService) { svc.On("ListApps", mock.Anything, "t-1", 10, 0).Return(nil, 0, internal) },
			call: func(srv *IAMServer) error {
				_, err := srv.ListApps(ctx, &iampb.ListAppsRequest{TenantId: "t-1", Limit: 10})
				return err
			},
			want: codes.Internal,
		},
		{
			name: "RotateAppSecret",
			setup: func(svc *mockIAMService) {
				svc.On("RotateAppSecret", mock.Anything, "t-1", "c-1").Return(nil, notFound)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.RotateAppSecret(ctx, &iampb.RotateAppSecretRequest{TenantId: "t-1", ClientId: "c-1"})
				return err
			},
			want: codes.NotFound,
		},

		// Policy operations.
		{
			name:  "CreatePolicy",
			setup: func(svc *mockIAMService) { svc.On("CreatePolicy", mock.Anything, mock.Anything).Return(nil, exists) },
			call: func(srv *IAMServer) error {
				_, err := srv.CreatePolicy(ctx, &iampb.CreatePolicyRequest{
					TenantId: "t-1", Name: "pol", SpecYaml: []byte("rules: []"),
				})
				return err
			},
			want: codes.AlreadyExists,
		},
		{
			name:  "GetPolicy",
			setup: func(svc *mockIAMService) { svc.On("GetPolicy", mock.Anything, "t-1", "pol").Return(nil, notFound) },
			call: func(srv *IAMServer) error {
				_, err := srv.GetPolicy(ctx, &iampb.GetPolicyRequest{TenantId: "t-1", PolicyName: "pol"})
				return err
			},
			want: codes.NotFound,
		},
		{
			name: "UpdatePolicy",
			setup: func(svc *mockIAMService) {
				svc.On("GetPolicy", mock.Anything, "t-1", "pol").
					Return(&iam.Policy{Metadata: iam.PolicyMetadata{Name: "pol", Tenant: "t-1"}}, nil)
				svc.On("UpdatePolicy", mock.Anything, mock.Anything).Return(nil, internal)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.UpdatePolicy(ctx, &iampb.UpdatePolicyRequest{
					TenantId: "t-1", PolicyName: "pol", SpecYaml: []byte("rules: []"),
				})
				return err
			},
			want: codes.Internal,
		},
		{
			// A failed read of the stored policy aborts the update before any write.
			name:  "UpdatePolicy read error",
			setup: func(svc *mockIAMService) { svc.On("GetPolicy", mock.Anything, "t-1", "pol").Return(nil, internal) },
			call: func(srv *IAMServer) error {
				_, err := srv.UpdatePolicy(ctx, &iampb.UpdatePolicyRequest{
					TenantId: "t-1", PolicyName: "pol", Description: "never written",
				})
				return err
			},
			want: codes.Internal,
			after: func(t *testing.T, svc *mockIAMService) {
				svc.AssertNotCalled(t, "UpdatePolicy", mock.Anything, mock.Anything)
			},
		},
		{
			name:  "DeletePolicy",
			setup: func(svc *mockIAMService) { svc.On("DeletePolicy", mock.Anything, "t-1", "pol").Return(notFound) },
			call: func(srv *IAMServer) error {
				_, err := srv.DeletePolicy(ctx, &iampb.DeletePolicyRequest{TenantId: "t-1", PolicyName: "pol"})
				return err
			},
			want: codes.NotFound,
		},
		{
			name: "ListPolicies",
			setup: func(svc *mockIAMService) {
				svc.On("ListPolicies", mock.Anything, "t-1", 10, 0).Return(nil, 0, internal)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.ListPolicies(ctx, &iampb.ListPoliciesRequest{TenantId: "t-1", Limit: 10})
				return err
			},
			want: codes.Internal,
		},
		{
			name: "ValidatePolicy",
			setup: func(svc *mockIAMService) {
				svc.On("ValidatePolicy", mock.Anything, "t-1", "", []byte("spec")).Return(false, []string(nil), internal)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.ValidatePolicy(ctx, &iampb.ValidatePolicyRequest{TenantId: "t-1", SpecYaml: []byte("spec")})
				return err
			},
			want: codes.Internal,
		},
		{
			name: "TestPolicy",
			setup: func(svc *mockIAMService) {
				svc.On("TestPolicy", mock.Anything, "t-1", mock.Anything).Return(nil, internal)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.TestPolicy(ctx, &iampb.TestPolicyRequest{
					TenantId: "t-1",
					Subject:  &iampb.Subject{Id: "u-1", TenantId: "t-1"},
					Action:   "read",
					Resource: "doc:1",
				})
				return err
			},
			want: codes.Internal,
		},

		// Delegated admin operations.
		{
			name:  "GetAdminAssignment",
			setup: func(svc *mockIAMService) { svc.On("GetAdminAssignment", mock.Anything, "a-1").Return(nil, notFound) },
			call: func(srv *IAMServer) error {
				_, err := srv.GetAdminAssignment(ctx, &iampb.GetAdminAssignmentRequest{Id: "a-1"})
				return err
			},
			want: codes.NotFound,
		},
		{
			name:  "DeleteAdminAssignment",
			setup: func(svc *mockIAMService) { svc.On("DeleteAdminAssignment", mock.Anything, "a-1").Return(notFound) },
			call: func(srv *IAMServer) error {
				_, err := srv.DeleteAdminAssignment(ctx, &iampb.DeleteAdminAssignmentRequest{Id: "a-1"})
				return err
			},
			want: codes.NotFound,
		},
		{
			name: "ListAdminAssignments",
			setup: func(svc *mockIAMService) {
				svc.On("ListAdminAssignments", mock.Anything, mock.Anything).Return(nil, internal)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.ListAdminAssignments(ctx, &iampb.ListAdminAssignmentsRequest{TenantId: "t-1"})
				return err
			},
			want: codes.Internal,
		},
		{
			name: "CheckAdminPermission",
			setup: func(svc *mockIAMService) {
				svc.On("CheckAdminPermission", mock.Anything, mock.Anything).Return(nil, forbidden)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.CheckAdminPermission(ctx, &iampb.CheckAdminPermissionRequest{
					UserId: "30021", TenantId: "t-1", Permission: "manage_users",
				})
				return err
			},
			want: codes.PermissionDenied,
		},
		{
			name:  "EndImpersonation",
			setup: func(svc *mockIAMService) { svc.On("EndImpersonation", mock.Anything, "sess-1").Return(notFound) },
			call: func(srv *IAMServer) error {
				_, err := srv.EndImpersonation(ctx, &iampb.EndImpersonationRequest{SessionId: "sess-1"})
				return err
			},
			want: codes.NotFound,
		},
		{
			name: "GetActiveImpersonations",
			setup: func(svc *mockIAMService) {
				svc.On("GetActiveImpersonations", mock.Anything, uint64(30021)).Return(nil, internal)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.GetActiveImpersonations(ctx, &iampb.GetActiveImpersonationsRequest{AdminUserId: "30021"})
				return err
			},
			want: codes.Internal,
		},

		// GDPR consent operations.
		{
			name: "WithdrawConsent",
			setup: func(svc *mockIAMService) {
				svc.On("WithdrawConsent", mock.Anything, "t-1", uint64(1), mock.Anything, "", "").Return(notFound)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.WithdrawConsent(ctx, &iampb.WithdrawConsentRequest{
					TenantId: "t-1", UserId: "1", Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_ANALYTICS,
				})
				return err
			},
			want: codes.NotFound,
		},
		{
			name: "GetConsent",
			setup: func(svc *mockIAMService) {
				svc.On("GetConsent", mock.Anything, "t-1", uint64(1), mock.Anything).Return(nil, notFound)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.GetConsent(ctx, &iampb.GetConsentRequest{
					TenantId: "t-1", UserId: "1", Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_ANALYTICS,
				})
				return err
			},
			want: codes.NotFound,
		},
		{
			name: "ListUserConsents",
			setup: func(svc *mockIAMService) {
				svc.On("ListUserConsents", mock.Anything, "t-1", uint64(1)).Return(nil, internal)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.ListUserConsents(ctx, &iampb.ListUserConsentsRequest{TenantId: "t-1", UserId: "1"})
				return err
			},
			want: codes.Internal,
		},

		// GDPR data export operations.
		{
			name:  "GetExportStatus",
			setup: func(svc *mockIAMService) { svc.On("GetExportStatus", mock.Anything, "exp-1").Return(nil, notFound) },
			call: func(srv *IAMServer) error {
				_, err := srv.GetExportStatus(ctx, &iampb.GetExportStatusRequest{ExportId: "exp-1"})
				return err
			},
			want: codes.NotFound,
		},
		{
			name:  "GenerateExport",
			setup: func(svc *mockIAMService) { svc.On("GenerateExport", mock.Anything, "exp-1").Return(nil, notFound) },
			call: func(srv *IAMServer) error {
				_, err := srv.GenerateExport(ctx, &iampb.GenerateExportRequest{ExportId: "exp-1"})
				return err
			},
			want: codes.NotFound,
		},

		// GDPR data deletion operations.
		{
			name: "CancelDeletion",
			setup: func(svc *mockIAMService) {
				svc.On("CancelDeletion", mock.Anything, "del-1", "admin-1").Return(notFound)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.CancelDeletion(ctx, &iampb.CancelDeletionRequest{DeletionId: "del-1", CancelledBy: "admin-1"})
				return err
			},
			want: codes.NotFound,
		},
		{
			name:  "ProcessDeletion",
			setup: func(svc *mockIAMService) { svc.On("ProcessDeletion", mock.Anything, "del-1").Return(nil, notFound) },
			call: func(srv *IAMServer) error {
				_, err := srv.ProcessDeletion(ctx, &iampb.ProcessDeletionRequest{DeletionId: "del-1"})
				return err
			},
			want: codes.NotFound,
		},
		{
			name:  "GetDeletionStatus",
			setup: func(svc *mockIAMService) { svc.On("GetDeletionStatus", mock.Anything, "del-1").Return(nil, notFound) },
			call: func(srv *IAMServer) error {
				_, err := srv.GetDeletionStatus(ctx, &iampb.GetDeletionStatusRequest{DeletionId: "del-1"})
				return err
			},
			want: codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, svc := newTestIAMServer(t)
			tt.setup(svc)

			err := tt.call(srv)
			require.Error(t, err)
			assert.Equal(t, tt.want, status.Code(err))
			if tt.after != nil {
				tt.after(t, svc)
			}
		})
	}
}
