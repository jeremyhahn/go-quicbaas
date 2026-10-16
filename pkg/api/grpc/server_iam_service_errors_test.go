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
)

// =============================================================================
// Tenant Operation Service Error Tests
// =============================================================================

func TestIAMServer_GetTenant_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("GetTenant", mock.Anything, "t-1").Return(nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.GetTenant(callerContext("t-1"), &iampb.GetTenantRequest{Id: "t-1"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestIAMServer_UpdateTenant_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("UpdateTenant", mock.Anything, mock.Anything).Return(nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.UpdateTenant(callerContext("t-1"), &iampb.UpdateTenantRequest{Id: "t-1", Name: "n"})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestIAMServer_DeleteTenant_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("DeleteTenant", mock.Anything, "t-1").Return(&dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.DeleteTenant(callerContext("t-1"), &iampb.DeleteTenantRequest{Id: "t-1"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// =============================================================================
// User Operation Service Error Tests
// =============================================================================

func TestIAMServer_GetUser_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("GetUser", mock.Anything, "t-1", "u-1").Return(nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.GetUser(callerContext("t-1"), &iampb.GetUserRequest{TenantId: "t-1", UserId: "u-1"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestIAMServer_CreateUser_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("CreateUser", mock.Anything, mock.Anything).Return(nil, &dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "test"})

	_, err := srv.CreateUser(callerContext("t-1"), &iampb.CreateUserRequest{
		TenantId: "t-1", Email: "a@b.c", Name: "user",
	})
	require.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, status.Code(err))
}

func TestIAMServer_UpdateUser_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("UpdateUser", mock.Anything, mock.Anything).Return(nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.UpdateUser(callerContext("t-1"), &iampb.UpdateUserRequest{
		TenantId: "t-1", UserId: "10001", Email: "a@b.c",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestIAMServer_DeleteUser_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("DeleteUser", mock.Anything, "t-1", "u-1").Return(&dberrors.QBError{Code: dberrors.ErrForbidden, Op: "test"})

	_, err := srv.DeleteUser(callerContext("t-1"), &iampb.DeleteUserRequest{TenantId: "t-1", UserId: "u-1"})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestIAMServer_ListUsers_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("ListUsers", mock.Anything, "t-1", 10, 0, mock.Anything).Return(nil, 0, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.ListUsers(callerContext("t-1"), &iampb.ListUsersRequest{TenantId: "t-1", Limit: 10})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// =============================================================================
// Role Operation Service Error Tests
// =============================================================================

func TestIAMServer_CreateRole_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("CreateRole", mock.Anything, mock.Anything).Return(nil, &dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "test"})

	_, err := srv.CreateRole(callerContext("t-1"), &iampb.CreateRoleRequest{
		TenantId: "t-1", Id: "r-1", Name: "role",
	})
	require.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, status.Code(err))
}

func TestIAMServer_GetRole_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("GetRole", mock.Anything, "t-1", "r-1").Return(nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.GetRole(callerContext("t-1"), &iampb.GetRoleRequest{TenantId: "t-1", RoleId: "r-1"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestIAMServer_UpdateRole_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("UpdateRole", mock.Anything, mock.Anything).Return(nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.UpdateRole(callerContext("t-1"), &iampb.UpdateRoleRequest{
		TenantId: "t-1", RoleId: "r-1", Name: "role",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestIAMServer_DeleteRole_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("DeleteRole", mock.Anything, "t-1", "r-1").Return(&dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.DeleteRole(callerContext("t-1"), &iampb.DeleteRoleRequest{TenantId: "t-1", RoleId: "r-1"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestIAMServer_ListRoles_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("ListRoles", mock.Anything, "t-1", 10, 0).Return(nil, 0, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.ListRoles(callerContext("t-1"), &iampb.ListRolesRequest{TenantId: "t-1", Limit: 10})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestIAMServer_AssignRole_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("AssignRole", mock.Anything, "t-1", uint64(1), "r-1").Return(&dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.AssignRole(callerContext("t-1"), &iampb.AssignRoleRequest{
		TenantId: "t-1", UserId: "1", Role: "r-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestIAMServer_RevokeRole_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("RevokeRole", mock.Anything, "t-1", uint64(1), "r-1").Return(&dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.RevokeRole(callerContext("t-1"), &iampb.RevokeRoleRequest{
		TenantId: "t-1", UserId: "1", Role: "r-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// =============================================================================
// Application Operation Service Error Tests
// =============================================================================

func TestIAMServer_RegisterApp_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("RegisterApp", mock.Anything, mock.Anything).Return(nil, &dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "test"})

	_, err := srv.RegisterApp(callerContext("t-1"), &iampb.RegisterAppRequest{
		TenantId: "t-1", Name: "app",
	})
	require.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, status.Code(err))
}

func TestIAMServer_GetApp_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("GetApp", mock.Anything, "t-1", "c-1").Return(nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.GetApp(callerContext("t-1"), &iampb.GetAppRequest{TenantId: "t-1", ClientId: "c-1"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestIAMServer_UpdateApp_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("UpdateApp", mock.Anything, mock.Anything).Return(nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.UpdateApp(callerContext("t-1"), &iampb.UpdateAppRequest{
		TenantId: "t-1", ClientId: "c-1", Name: "app",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestIAMServer_DeleteApp_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("DeleteApp", mock.Anything, "t-1", "c-1").Return(&dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.DeleteApp(callerContext("t-1"), &iampb.DeleteAppRequest{TenantId: "t-1", ClientId: "c-1"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestIAMServer_ListApps_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("ListApps", mock.Anything, "t-1", 10, 0).Return(nil, 0, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.ListApps(callerContext("t-1"), &iampb.ListAppsRequest{TenantId: "t-1", Limit: 10})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestIAMServer_RotateAppSecret_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("RotateAppSecret", mock.Anything, "t-1", "c-1").Return(nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.RotateAppSecret(callerContext("t-1"), &iampb.RotateAppSecretRequest{TenantId: "t-1", ClientId: "c-1"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// =============================================================================
// Policy Operation Service Error Tests
// =============================================================================

func TestIAMServer_CreatePolicy_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("CreatePolicy", mock.Anything, mock.Anything).Return(nil, &dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "test"})

	_, err := srv.CreatePolicy(callerContext("t-1"), &iampb.CreatePolicyRequest{
		TenantId: "t-1", Name: "pol", SpecYaml: []byte("rules: []"),
	})
	require.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, status.Code(err))
}

func TestIAMServer_GetPolicy_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("GetPolicy", mock.Anything, "t-1", "pol").Return(nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.GetPolicy(callerContext("t-1"), &iampb.GetPolicyRequest{TenantId: "t-1", PolicyName: "pol"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestIAMServer_UpdatePolicy_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("UpdatePolicy", mock.Anything, mock.Anything).Return(nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.UpdatePolicy(callerContext("t-1"), &iampb.UpdatePolicyRequest{
		TenantId: "t-1", PolicyName: "pol", SpecYaml: []byte("rules: []"),
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestIAMServer_DeletePolicy_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("DeletePolicy", mock.Anything, "t-1", "pol").Return(&dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.DeletePolicy(callerContext("t-1"), &iampb.DeletePolicyRequest{TenantId: "t-1", PolicyName: "pol"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestIAMServer_ListPolicies_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("ListPolicies", mock.Anything, "t-1", 10, 0).Return(nil, 0, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.ListPolicies(callerContext("t-1"), &iampb.ListPoliciesRequest{TenantId: "t-1", Limit: 10})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestIAMServer_ValidatePolicy_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("ValidatePolicy", mock.Anything, "t-1", "", []byte("spec")).Return(false, []string(nil), &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.ValidatePolicy(callerContext("t-1"), &iampb.ValidatePolicyRequest{TenantId: "t-1", SpecYaml: []byte("spec")})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestIAMServer_TestPolicy_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("TestPolicy", mock.Anything, "t-1", mock.Anything).Return(nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.TestPolicy(callerContext("t-1"), &iampb.TestPolicyRequest{
		TenantId: "t-1",
		Subject:  &iampb.Subject{Id: "u-1", TenantId: "t-1"},
		Action:   "read",
		Resource: "doc:1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// =============================================================================
// Delegated Admin Service Error Tests
// =============================================================================

func TestIAMServer_GetAdminAssignment_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("GetAdminAssignment", mock.Anything, "a-1").Return(nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.GetAdminAssignment(callerContext("t-1"), &iampb.GetAdminAssignmentRequest{Id: "a-1"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestIAMServer_DeleteAdminAssignment_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("DeleteAdminAssignment", mock.Anything, "a-1").Return(&dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.DeleteAdminAssignment(callerContext("t-1"), &iampb.DeleteAdminAssignmentRequest{Id: "a-1"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestIAMServer_ListAdminAssignments_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("ListAdminAssignments", mock.Anything, mock.Anything).Return(nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.ListAdminAssignments(callerContext("t-1"), &iampb.ListAdminAssignmentsRequest{TenantId: "t-1"})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestIAMServer_CheckAdminPermission_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("CheckAdminPermission", mock.Anything, mock.Anything).Return(nil, &dberrors.QBError{Code: dberrors.ErrForbidden, Op: "test"})

	_, err := srv.CheckAdminPermission(callerContext("t-1"), &iampb.CheckAdminPermissionRequest{
		UserId: "30021", TenantId: "t-1", Permission: "manage_users",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestIAMServer_EndImpersonation_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("EndImpersonation", mock.Anything, "sess-1").Return(&dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.EndImpersonation(callerContext("t-1"), &iampb.EndImpersonationRequest{SessionId: "sess-1"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestIAMServer_GetActiveImpersonations_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("GetActiveImpersonations", mock.Anything, uint64(30021)).Return(nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.GetActiveImpersonations(callerContext("t-1"), &iampb.GetActiveImpersonationsRequest{AdminUserId: "30021"})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// =============================================================================
// GDPR Consent Service Error Tests
// =============================================================================

func TestIAMServer_WithdrawConsent_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("WithdrawConsent", mock.Anything, "t-1", uint64(1), mock.Anything, "", "").Return(&dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.WithdrawConsent(callerContext("t-1"), &iampb.WithdrawConsentRequest{
		TenantId: "t-1", UserId: "1", Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_ANALYTICS,
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestIAMServer_GetConsent_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("GetConsent", mock.Anything, "t-1", uint64(1), mock.Anything).Return(nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.GetConsent(callerContext("t-1"), &iampb.GetConsentRequest{
		TenantId: "t-1", UserId: "1", Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_ANALYTICS,
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestIAMServer_ListUserConsents_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("ListUserConsents", mock.Anything, "t-1", uint64(1)).Return(nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.ListUserConsents(callerContext("t-1"), &iampb.ListUserConsentsRequest{
		TenantId: "t-1", UserId: "1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// =============================================================================
// GDPR Data Export Service Error Tests
// =============================================================================

func TestIAMServer_GetExportStatus_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("GetExportStatus", mock.Anything, "exp-1").Return(nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.GetExportStatus(callerContext("t-1"), &iampb.GetExportStatusRequest{ExportId: "exp-1"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestIAMServer_GenerateExport_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("GenerateExport", mock.Anything, "exp-1").Return(nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.GenerateExport(callerContext("t-1"), &iampb.GenerateExportRequest{ExportId: "exp-1"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// =============================================================================
// GDPR Data Deletion Service Error Tests
// =============================================================================

func TestIAMServer_CancelDeletion_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("CancelDeletion", mock.Anything, "del-1", "admin-1").Return(&dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.CancelDeletion(callerContext("t-1"), &iampb.CancelDeletionRequest{
		DeletionId: "del-1", CancelledBy: "admin-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestIAMServer_ProcessDeletion_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("ProcessDeletion", mock.Anything, "del-1").Return(nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.ProcessDeletion(callerContext("t-1"), &iampb.ProcessDeletionRequest{DeletionId: "del-1"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestIAMServer_GetDeletionStatus_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("GetDeletionStatus", mock.Anything, "del-1").Return(nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.GetDeletionStatus(callerContext("t-1"), &iampb.GetDeletionStatusRequest{DeletionId: "del-1"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}
