//go:build quic

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

package quic

import (
	"context"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// IAM Service Interfaces
// =============================================================================

// mfaServiceInterface defines MFA operations for type assertion.
type mfaServiceInterface interface {
	// Enrollment
	BeginEnrollment(ctx context.Context, userID, tenantID string, method iam.MFAMethod) (*iam.MFAEnrollResponse, error)
	CompleteEnrollment(ctx context.Context, enrollmentID, code string) error
	CancelEnrollment(ctx context.Context, enrollmentID string) error

	// Verification
	BeginChallenge(ctx context.Context, userID, tenantID, sessionToken string) (*iam.MFAChallenge, error)
	VerifyChallenge(ctx context.Context, challengeID, code string) (*iam.MFAVerifyResponse, error)

	// Management
	DisableMFA(ctx context.Context, userID, tenantID string) error
	GetMFAStatus(ctx context.Context, userID, tenantID string) (*iam.MFAStatus, error)

	// Backup Codes
	GenerateBackupCodes(ctx context.Context, userID, tenantID string) ([]string, error)
	VerifyBackupCode(ctx context.Context, userID, tenantID, code string) error
	GetBackupCodeStatus(ctx context.Context, userID, tenantID string) (*iam.BackupCodeStatus, error)
}

// pepServiceInterface defines PEP operations for type assertion.
type pepServiceInterface interface {
	Check(ctx context.Context, req *service.CheckRequest) (*service.CheckResponse, error)
	CheckBatch(ctx context.Context, req *service.BatchCheckRequest) (*service.BatchCheckResponse, error)
}

// =============================================================================
// Helper function for IAM service access
// =============================================================================

// iamServiceUnavailableError creates a standardized error response for unavailable IAM service.
func (h *Handler) iamServiceUnavailableError(_ string) *Response {
	return h.errorResponse(api.NewServiceUnavailableError("IAM"))
}

// =============================================================================
// Tenant Operations Handlers
// =============================================================================

func (h *Handler) handleIAMCreateTenant(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMCreateTenant")
	}

	var createReq service.CreateTenantRequest
	if err := DecodePayload(req.Payload, &createReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.CreateTenant(ctx, &createReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMGetTenant(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMGetTenant")
	}

	var getReq service.GetTenantRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.GetTenant(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMUpdateTenant(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMUpdateTenant")
	}

	var updateReq service.UpdateTenantRequest
	if err := DecodePayload(req.Payload, &updateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.UpdateTenant(ctx, &updateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMDeleteTenant(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMDeleteTenant")
	}

	var deleteReq service.DeleteTenantRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.DeleteTenant(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMListTenants(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMListTenants")
	}

	var listReq service.ListTenantsRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.ListTenants(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// User Operations Handlers
// =============================================================================

func (h *Handler) handleIAMCreateUser(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMCreateUser")
	}

	var createReq service.IAMCreateUserRequest
	if err := DecodePayload(req.Payload, &createReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.CreateUser(ctx, &createReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMGetUser(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMGetUser")
	}

	var getReq service.IAMGetUserRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.GetUser(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMUpdateUser(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMUpdateUser")
	}

	var updateReq service.IAMUpdateUserRequest
	if err := DecodePayload(req.Payload, &updateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.UpdateUser(ctx, &updateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMDeleteUser(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMDeleteUser")
	}

	var deleteReq service.IAMDeleteUserRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.DeleteUser(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMListUsers(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMListUsers")
	}

	var listReq service.IAMListUsersRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.ListUsers(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMAssignUserRole(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMAssignUserRole")
	}

	var assignReq service.AssignRoleRequest
	if err := DecodePayload(req.Payload, &assignReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.AssignRole(ctx, &assignReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMRevokeUserRole(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMRevokeUserRole")
	}

	var revokeReq service.RevokeRoleRequest
	if err := DecodePayload(req.Payload, &revokeReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.RevokeRole(ctx, &revokeReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Group Operations Handlers
// =============================================================================

func (h *Handler) handleIAMCreateGroup(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMCreateGroup")
	}

	var createReq service.CreateGroupRequest
	if err := DecodePayload(req.Payload, &createReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.CreateGroup(ctx, &createReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMGetGroup(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMGetGroup")
	}

	var getReq service.GetGroupRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.GetGroup(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMUpdateGroup(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMUpdateGroup")
	}

	var updateReq service.UpdateGroupRequest
	if err := DecodePayload(req.Payload, &updateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.UpdateGroup(ctx, &updateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMDeleteGroup(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMDeleteGroup")
	}

	var deleteReq service.DeleteGroupRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.DeleteGroup(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMListGroups(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMListGroups")
	}

	var listReq service.ListGroupsRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.ListGroups(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMAddGroupMember(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMAddGroupMember")
	}

	var addReq service.AddUserToGroupRequest
	if err := DecodePayload(req.Payload, &addReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.AddUserToGroup(ctx, &addReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMRemoveGroupMember(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMRemoveGroupMember")
	}

	var removeReq service.RemoveUserFromGroupRequest
	if err := DecodePayload(req.Payload, &removeReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.RemoveUserFromGroup(ctx, &removeReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMListGroupMembers(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMListGroupMembers")
	}

	var getReq service.GetGroupRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	// Get the group which includes members
	groupResp, err := iamSvc.GetGroup(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	// Return just the members list
	type listMembersResponse struct {
		Members []string `json:"members"`
	}

	resp := &listMembersResponse{
		Members: groupResp.Group.Members,
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Role Operations Handlers
// =============================================================================

func (h *Handler) handleIAMCreateRole(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMCreateRole")
	}

	var createReq service.IAMCreateRoleRequest
	if err := DecodePayload(req.Payload, &createReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.CreateRole(ctx, &createReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMGetRole(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMGetRole")
	}

	var getReq service.IAMGetRoleRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.GetRole(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMUpdateRole(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMUpdateRole")
	}

	var updateReq service.IAMUpdateRoleRequest
	if err := DecodePayload(req.Payload, &updateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.UpdateRole(ctx, &updateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMDeleteRole(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMDeleteRole")
	}

	var deleteReq service.IAMDeleteRoleRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.DeleteRole(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMListRoles(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMListRoles")
	}

	var listReq service.IAMListRolesRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.ListRoles(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Policy Operations Handlers
// =============================================================================

func (h *Handler) handleIAMCreatePolicy(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMCreatePolicy")
	}

	var createReq service.CreatePolicyRequest
	if err := DecodePayload(req.Payload, &createReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.CreatePolicy(ctx, &createReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMGetPolicy(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMGetPolicy")
	}

	var getReq service.GetPolicyRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.GetPolicy(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMUpdatePolicy(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMUpdatePolicy")
	}

	var updateReq service.UpdatePolicyRequest
	if err := DecodePayload(req.Payload, &updateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.UpdatePolicy(ctx, &updateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMDeletePolicy(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMDeletePolicy")
	}

	var deleteReq service.DeletePolicyRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.DeletePolicy(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMListPolicies(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMListPolicies")
	}

	var listReq service.ListPoliciesRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.ListPolicies(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMValidatePolicy(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMValidatePolicy")
	}

	var validateReq service.ValidatePolicyRequest
	if err := DecodePayload(req.Payload, &validateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.ValidatePolicy(ctx, &validateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMTestPolicy(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMTestPolicy")
	}

	var testReq service.TestPolicyRequest
	if err := DecodePayload(req.Payload, &testReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.TestPolicy(ctx, &testReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Application Operations Handlers
// =============================================================================

func (h *Handler) handleIAMRegisterApp(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMRegisterApp")
	}

	var registerReq service.RegisterAppRequest
	if err := DecodePayload(req.Payload, &registerReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.RegisterApp(ctx, &registerReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMGetApp(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMGetApp")
	}

	var getReq service.GetAppRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.GetApp(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMUpdateApp(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMUpdateApp")
	}

	var updateReq service.UpdateAppRequest
	if err := DecodePayload(req.Payload, &updateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.UpdateApp(ctx, &updateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMDeleteApp(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMDeleteApp")
	}

	var deleteReq service.DeleteAppRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.DeleteApp(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMListApps(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMListApps")
	}

	var listReq service.ListAppsRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.ListApps(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleIAMRotateAppSecret(ctx context.Context, req *Request) *Response {
	iamSvc := h.getIAMService()
	if iamSvc == nil {
		return h.iamServiceUnavailableError("quic.handleIAMRotateAppSecret")
	}

	var rotateReq service.RotateAppSecretRequest
	if err := DecodePayload(req.Payload, &rotateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := iamSvc.RotateAppSecret(ctx, &rotateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// MFA Operations Handlers
// =============================================================================

// MFABeginEnrollRequest represents a request to begin MFA enrollment.
type MFABeginEnrollRequest struct {
	UserID   string        `json:"user_id"`
	TenantID string        `json:"tenant_id"`
	Method   iam.MFAMethod `json:"method"`
}

// MFACompleteEnrollRequest represents a request to complete MFA enrollment.
type MFACompleteEnrollRequest struct {
	EnrollmentID string `json:"enrollment_id"`
	Code         string `json:"code"`
}

// MFACancelEnrollRequest represents a request to cancel MFA enrollment.
type MFACancelEnrollRequest struct {
	EnrollmentID string `json:"enrollment_id"`
}

// MFAGetStatusRequest represents a request to get MFA status.
type MFAGetStatusRequest struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
}

// MFADisableRequest represents a request to disable MFA.
type MFADisableRequest struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
}

// MFABeginChallengeRequest represents a request to begin MFA challenge.
type MFABeginChallengeRequest struct {
	UserID       string `json:"user_id"`
	TenantID     string `json:"tenant_id"`
	SessionToken string `json:"session_token"`
}

// MFAVerifyChallengeRequest represents a request to verify MFA challenge.
type MFAVerifyChallengeRequest struct {
	ChallengeID string `json:"challenge_id"`
	Code        string `json:"code"`
}

// MFAGenerateBackupCodesRequest represents a request to generate backup codes.
type MFAGenerateBackupCodesRequest struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
}

// MFAGenerateBackupCodesResponse represents the response with backup codes.
type MFAGenerateBackupCodesResponse struct {
	Codes []string `json:"codes"`
}

// MFAVerifyBackupCodeRequest represents a request to verify a backup code.
type MFAVerifyBackupCodeRequest struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
	Code     string `json:"code"`
}

// MFAVerifyBackupCodeResponse represents the response to backup code verification.
type MFAVerifyBackupCodeResponse struct {
	Success bool `json:"success"`
}

// MFAGetBackupCodeStatusRequest represents a request to get backup code status.
type MFAGetBackupCodeStatusRequest struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
}

func (h *Handler) handleMFABeginEnroll(ctx context.Context, req *Request) *Response {
	mfaSvc := h.getMFAService()
	if mfaSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("MFA"))
	}

	var enrollReq MFABeginEnrollRequest
	if err := DecodePayload(req.Payload, &enrollReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := mfaSvc.BeginEnrollment(ctx, enrollReq.UserID, enrollReq.TenantID, enrollReq.Method)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleMFACompleteEnroll(ctx context.Context, req *Request) *Response {
	mfaSvc := h.getMFAService()
	if mfaSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("MFA"))
	}

	var completeReq MFACompleteEnrollRequest
	if err := DecodePayload(req.Payload, &completeReq); err != nil {
		return h.errorResponse(err)
	}

	err := mfaSvc.CompleteEnrollment(ctx, completeReq.EnrollmentID, completeReq.Code)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: nil}
}

func (h *Handler) handleMFACancelEnroll(ctx context.Context, req *Request) *Response {
	mfaSvc := h.getMFAService()
	if mfaSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("MFA"))
	}

	var cancelReq MFACancelEnrollRequest
	if err := DecodePayload(req.Payload, &cancelReq); err != nil {
		return h.errorResponse(err)
	}

	err := mfaSvc.CancelEnrollment(ctx, cancelReq.EnrollmentID)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: nil}
}

func (h *Handler) handleMFAGetStatus(ctx context.Context, req *Request) *Response {
	mfaSvc := h.getMFAService()
	if mfaSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("MFA"))
	}

	var statusReq MFAGetStatusRequest
	if err := DecodePayload(req.Payload, &statusReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := mfaSvc.GetMFAStatus(ctx, statusReq.UserID, statusReq.TenantID)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleMFADisable(ctx context.Context, req *Request) *Response {
	mfaSvc := h.getMFAService()
	if mfaSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("MFA"))
	}

	var disableReq MFADisableRequest
	if err := DecodePayload(req.Payload, &disableReq); err != nil {
		return h.errorResponse(err)
	}

	err := mfaSvc.DisableMFA(ctx, disableReq.UserID, disableReq.TenantID)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: nil}
}

func (h *Handler) handleMFABeginChallenge(ctx context.Context, req *Request) *Response {
	mfaSvc := h.getMFAService()
	if mfaSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("MFA"))
	}

	var challengeReq MFABeginChallengeRequest
	if err := DecodePayload(req.Payload, &challengeReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := mfaSvc.BeginChallenge(ctx, challengeReq.UserID, challengeReq.TenantID, challengeReq.SessionToken)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleMFAVerifyChallenge(ctx context.Context, req *Request) *Response {
	mfaSvc := h.getMFAService()
	if mfaSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("MFA"))
	}

	var verifyReq MFAVerifyChallengeRequest
	if err := DecodePayload(req.Payload, &verifyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := mfaSvc.VerifyChallenge(ctx, verifyReq.ChallengeID, verifyReq.Code)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleMFAGenerateBackupCodes(ctx context.Context, req *Request) *Response {
	mfaSvc := h.getMFAService()
	if mfaSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("MFA"))
	}

	var generateReq MFAGenerateBackupCodesRequest
	if err := DecodePayload(req.Payload, &generateReq); err != nil {
		return h.errorResponse(err)
	}

	codes, err := mfaSvc.GenerateBackupCodes(ctx, generateReq.UserID, generateReq.TenantID)
	if err != nil {
		return h.errorResponse(err)
	}

	resp := &MFAGenerateBackupCodesResponse{Codes: codes}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleMFAVerifyBackupCode(ctx context.Context, req *Request) *Response {
	mfaSvc := h.getMFAService()
	if mfaSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("MFA"))
	}

	var verifyReq MFAVerifyBackupCodeRequest
	if err := DecodePayload(req.Payload, &verifyReq); err != nil {
		return h.errorResponse(err)
	}

	err := mfaSvc.VerifyBackupCode(ctx, verifyReq.UserID, verifyReq.TenantID, verifyReq.Code)
	success := err == nil

	resp := &MFAVerifyBackupCodeResponse{Success: success}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleMFAGetBackupCodeStatus(ctx context.Context, req *Request) *Response {
	mfaSvc := h.getMFAService()
	if mfaSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("MFA"))
	}

	var statusReq MFAGetBackupCodeStatusRequest
	if err := DecodePayload(req.Payload, &statusReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := mfaSvc.GetBackupCodeStatus(ctx, statusReq.UserID, statusReq.TenantID)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// PEP Operations Handlers
// =============================================================================

func (h *Handler) handlePEPCheckAuthorization(ctx context.Context, req *Request) *Response {
	pepSvc, ok := h.service.(pepServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("PEP"))
	}

	var checkReq service.CheckRequest
	if err := DecodePayload(req.Payload, &checkReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := pepSvc.Check(ctx, &checkReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handlePEPCheckAuthorizationBatch(ctx context.Context, req *Request) *Response {
	pepSvc, ok := h.service.(pepServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("PEP"))
	}

	var batchReq service.BatchCheckRequest
	if err := DecodePayload(req.Payload, &batchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := pepSvc.CheckBatch(ctx, &batchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
