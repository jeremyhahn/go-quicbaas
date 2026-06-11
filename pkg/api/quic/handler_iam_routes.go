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
	"encoding/json"
	"fmt"
)

// IAMSubRequest wraps a sub-operation code with the actual request payload.
// Payload uses json.RawMessage so the inner JSON object is preserved as-is
// during marshaling and unmarshaling, avoiding base64 encoding issues that
// occur when using []byte with encoding/json.
type IAMSubRequest struct {
	SubOp   byte            `json:"subop" msgpack:"subop"`
	Payload json.RawMessage `json:"payload" msgpack:"payload"`
}

// HandleRequestIAM extends HandleRequest with IAM-specific opcodes.
// This function is called from HandleRequest.
// Supports both legacy sub-operation pattern (0xF0-0xFF) and new direct opcodes (0x95-0xCE).
func (h *Handler) HandleRequestIAM(ctx context.Context, req *Request) *Response {
	switch req.Opcode {
	// ==========================================================================
	// Legacy Sub-Operation Pattern (0xF0-0xFF)
	// ==========================================================================
	case OpIAMAdmin:
		return h.handleIAMAdminOps(ctx, req)
	case OpIAMGDPR:
		return h.handleIAMGDPROps(ctx, req)
	case OpIAMFederation:
		return h.handleIAMFederationOps(ctx, req)
	case OpIAMTenant:
		return h.handleIAMTenantOps(ctx, req)
	case OpIAMUser:
		return h.handleIAMUserOps(ctx, req)
	case OpIAMGroup:
		return h.handleIAMGroupOps(ctx, req)
	case OpIAMRole:
		return h.handleIAMRoleOps(ctx, req)
	case OpIAMPolicy:
		return h.handleIAMPolicyOps(ctx, req)
	case OpIAMApp:
		return h.handleIAMAppOps(ctx, req)
	case OpMFA:
		return h.handleIAMMFAOps(ctx, req)
	case OpPEP:
		return h.handleIAMPEPOps(ctx, req)

	// ==========================================================================
	// Direct Opcode Pattern (0x95-0xCE) - New direct opcodes for IAM operations
	// ==========================================================================

	// Direct IAM Tenant Operations (0x95-0x99)
	case OpIAMCreateTenant:
		return h.handleIAMCreateTenant(ctx, req)
	case OpIAMGetTenant:
		return h.handleIAMGetTenant(ctx, req)
	case OpIAMUpdateTenant:
		return h.handleIAMUpdateTenant(ctx, req)
	case OpIAMDeleteTenant:
		return h.handleIAMDeleteTenant(ctx, req)
	case OpIAMListTenants:
		return h.handleIAMListTenants(ctx, req)

	// Direct IAM User Operations (0x9A-0x9E)
	case OpIAMCreateUser:
		return h.handleIAMCreateUser(ctx, req)
	case OpIAMGetUser:
		return h.handleIAMGetUser(ctx, req)
	case OpIAMUpdateUser:
		return h.handleIAMUpdateUser(ctx, req)
	case OpIAMDeleteUser:
		return h.handleIAMDeleteUser(ctx, req)
	case OpIAMListUsers:
		return h.handleIAMListUsers(ctx, req)

	// Direct IAM Group Operations (0x9F, 0xAB-0xAF, 0xB5)
	case OpIAMCreateGroup:
		return h.handleIAMCreateGroup(ctx, req)
	case OpIAMGetGroup:
		return h.handleIAMGetGroup(ctx, req)
	case OpIAMUpdateGroup:
		return h.handleIAMUpdateGroup(ctx, req)
	case OpIAMDeleteGroup:
		return h.handleIAMDeleteGroup(ctx, req)
	case OpIAMListGroups:
		return h.handleIAMListGroups(ctx, req)
	case OpIAMAddUserToGroup:
		return h.handleIAMAddGroupMember(ctx, req)
	case OpIAMRemoveUserFromGroup:
		return h.handleIAMRemoveGroupMember(ctx, req)

	// Direct IAM Role Operations (0xB6-0xBC)
	case OpIAMCreateRole:
		return h.handleIAMCreateRole(ctx, req)
	case OpIAMGetRole:
		return h.handleIAMGetRole(ctx, req)
	case OpIAMUpdateRole:
		return h.handleIAMUpdateRole(ctx, req)
	case OpIAMDeleteRole:
		return h.handleIAMDeleteRole(ctx, req)
	case OpIAMListRoles:
		return h.handleIAMListRoles(ctx, req)
	case OpIAMAssignRole:
		return h.handleIAMAssignUserRole(ctx, req)
	case OpIAMRevokeRole:
		return h.handleIAMRevokeUserRole(ctx, req)

	// Direct IAM App Operations (0xBD-0xBF, 0xC5-0xC7)
	case OpIAMRegisterApp:
		return h.handleIAMRegisterApp(ctx, req)
	case OpIAMGetApp:
		return h.handleIAMGetApp(ctx, req)
	case OpIAMUpdateApp:
		return h.handleIAMUpdateApp(ctx, req)
	case OpIAMDeleteApp:
		return h.handleIAMDeleteApp(ctx, req)
	case OpIAMListApps:
		return h.handleIAMListApps(ctx, req)
	case OpIAMRotateAppSecret:
		return h.handleIAMRotateAppSecret(ctx, req)

	// Direct IAM Policy Operations (0xC8-0xCE)
	case OpIAMCreatePolicy:
		return h.handleIAMCreatePolicy(ctx, req)
	case OpIAMGetPolicy:
		return h.handleIAMGetPolicy(ctx, req)
	case OpIAMUpdatePolicy:
		return h.handleIAMUpdatePolicy(ctx, req)
	case OpIAMDeletePolicy:
		return h.handleIAMDeletePolicy(ctx, req)
	case OpIAMListPolicies:
		return h.handleIAMListPolicies(ctx, req)
	case OpIAMValidatePolicy:
		return h.handleIAMValidatePolicy(ctx, req)
	case OpIAMTestPolicy:
		return h.handleIAMTestPolicy(ctx, req)

	// ==========================================================================
	// Direct PEP Operations (0x5B-0x5D)
	// ==========================================================================
	case OpPEPCheck:
		return h.handlePEPCheck(ctx, req)
	case OpPEPCheckBatch:
		return h.handlePEPCheckBatch(ctx, req)
	case OpPEPSubscribe:
		return h.handlePEPSubscribe(ctx, req)

	default:
		return nil // Not an IAM opcode, let base handler try
	}
}

// handleIAMAdminOps routes admin sub-operations.
func (h *Handler) handleIAMAdminOps(ctx context.Context, req *Request) *Response {
	var subReq IAMSubRequest
	if err := DecodePayload(req.Payload, &subReq); err != nil {
		return h.errorResponse(err)
	}

	// Create a new request with the sub-payload
	subRequest := &Request{
		Opcode:  subReq.SubOp,
		Payload: subReq.Payload,
	}

	switch subReq.SubOp {
	case SubOpCreateAdminAssignment:
		return h.handleIAMCreateAdminAssignment(ctx, subRequest)
	case SubOpGetAdminAssignment:
		return h.handleIAMGetAdminAssignment(ctx, subRequest)
	case SubOpUpdateAdminAssignment:
		return h.handleIAMUpdateAdminAssignment(ctx, subRequest)
	case SubOpDeleteAdminAssignment:
		return h.handleIAMDeleteAdminAssignment(ctx, subRequest)
	case SubOpListAdminAssignments:
		return h.handleIAMListAdminAssignments(ctx, subRequest)
	case SubOpCheckAdminPermission:
		return h.handleIAMCheckAdminPermission(ctx, subRequest)
	case SubOpStartImpersonation:
		return h.handleIAMStartImpersonation(ctx, subRequest)
	case SubOpEndImpersonation:
		return h.handleIAMEndImpersonation(ctx, subRequest)
	case SubOpGetActiveImpersonations:
		return h.handleIAMGetActiveImpersonations(ctx, subRequest)
	default:
		return &Response{
			Status:  StatusInvalidRequest,
			Payload: []byte(fmt.Sprintf("unknown admin sub-operation: 0x%02X", subReq.SubOp)),
		}
	}
}

// handleIAMGDPROps routes GDPR sub-operations.
func (h *Handler) handleIAMGDPROps(ctx context.Context, req *Request) *Response {
	var subReq IAMSubRequest
	if err := DecodePayload(req.Payload, &subReq); err != nil {
		return h.errorResponse(err)
	}

	// Create a new request with the sub-payload
	subRequest := &Request{
		Opcode:  subReq.SubOp,
		Payload: subReq.Payload,
	}

	switch subReq.SubOp {
	// Consent operations
	case SubOpGrantConsent:
		return h.handleIAMGrantConsent(ctx, subRequest)
	case SubOpWithdrawConsent:
		return h.handleIAMWithdrawConsent(ctx, subRequest)
	case SubOpGetConsent:
		return h.handleIAMGetConsent(ctx, subRequest)
	case SubOpListUserConsents:
		return h.handleIAMListUserConsents(ctx, subRequest)
	// Export operations
	case SubOpRequestExport:
		return h.handleIAMRequestExport(ctx, subRequest)
	case SubOpGetExportStatus:
		return h.handleIAMGetExportStatus(ctx, subRequest)
	case SubOpGenerateExport:
		return h.handleIAMGenerateExport(ctx, subRequest)
	// Deletion operations
	case SubOpRequestDeletion:
		return h.handleIAMRequestDeletion(ctx, subRequest)
	case SubOpCancelDeletion:
		return h.handleIAMCancelDeletion(ctx, subRequest)
	case SubOpProcessDeletion:
		return h.handleIAMProcessDeletion(ctx, subRequest)
	case SubOpGetDeletionStatus:
		return h.handleIAMGetDeletionStatus(ctx, subRequest)
	default:
		return &Response{
			Status:  StatusInvalidRequest,
			Payload: []byte(fmt.Sprintf("unknown GDPR sub-operation: 0x%02X", subReq.SubOp)),
		}
	}
}

// handleIAMTenantOps routes tenant sub-operations.
func (h *Handler) handleIAMTenantOps(ctx context.Context, req *Request) *Response {
	var subReq IAMSubRequest
	if err := DecodePayload(req.Payload, &subReq); err != nil {
		return h.errorResponse(err)
	}

	subRequest := &Request{
		Opcode:  subReq.SubOp,
		Payload: subReq.Payload,
	}

	switch subReq.SubOp {
	case SubOpCreateTenant:
		return h.handleIAMCreateTenant(ctx, subRequest)
	case SubOpGetTenant:
		return h.handleIAMGetTenant(ctx, subRequest)
	case SubOpUpdateTenant:
		return h.handleIAMUpdateTenant(ctx, subRequest)
	case SubOpDeleteTenant:
		return h.handleIAMDeleteTenant(ctx, subRequest)
	case SubOpListTenants:
		return h.handleIAMListTenants(ctx, subRequest)
	default:
		return &Response{
			Status:  StatusInvalidRequest,
			Payload: []byte(fmt.Sprintf("unknown tenant sub-operation: 0x%02X", subReq.SubOp)),
		}
	}
}

// handleIAMUserOps routes user sub-operations.
func (h *Handler) handleIAMUserOps(ctx context.Context, req *Request) *Response {
	var subReq IAMSubRequest
	if err := DecodePayload(req.Payload, &subReq); err != nil {
		return h.errorResponse(err)
	}

	subRequest := &Request{
		Opcode:  subReq.SubOp,
		Payload: subReq.Payload,
	}

	switch subReq.SubOp {
	case SubOpCreateUser:
		return h.handleIAMCreateUser(ctx, subRequest)
	case SubOpGetUser:
		return h.handleIAMGetUser(ctx, subRequest)
	case SubOpUpdateUser:
		return h.handleIAMUpdateUser(ctx, subRequest)
	case SubOpDeleteUser:
		return h.handleIAMDeleteUser(ctx, subRequest)
	case SubOpListUsers:
		return h.handleIAMListUsers(ctx, subRequest)
	case SubOpAssignUserRole:
		return h.handleIAMAssignUserRole(ctx, subRequest)
	case SubOpRevokeUserRole:
		return h.handleIAMRevokeUserRole(ctx, subRequest)
	default:
		return &Response{
			Status:  StatusInvalidRequest,
			Payload: []byte(fmt.Sprintf("unknown user sub-operation: 0x%02X", subReq.SubOp)),
		}
	}
}

// handleIAMGroupOps routes group sub-operations.
func (h *Handler) handleIAMGroupOps(ctx context.Context, req *Request) *Response {
	var subReq IAMSubRequest
	if err := DecodePayload(req.Payload, &subReq); err != nil {
		return h.errorResponse(err)
	}

	subRequest := &Request{
		Opcode:  subReq.SubOp,
		Payload: subReq.Payload,
	}

	switch subReq.SubOp {
	case SubOpCreateGroup:
		return h.handleIAMCreateGroup(ctx, subRequest)
	case SubOpGetGroup:
		return h.handleIAMGetGroup(ctx, subRequest)
	case SubOpUpdateGroup:
		return h.handleIAMUpdateGroup(ctx, subRequest)
	case SubOpDeleteGroup:
		return h.handleIAMDeleteGroup(ctx, subRequest)
	case SubOpListGroups:
		return h.handleIAMListGroups(ctx, subRequest)
	case SubOpAddGroupMember:
		return h.handleIAMAddGroupMember(ctx, subRequest)
	case SubOpRemoveGroupMember:
		return h.handleIAMRemoveGroupMember(ctx, subRequest)
	case SubOpListGroupMembers:
		return h.handleIAMListGroupMembers(ctx, subRequest)
	default:
		return &Response{
			Status:  StatusInvalidRequest,
			Payload: []byte(fmt.Sprintf("unknown group sub-operation: 0x%02X", subReq.SubOp)),
		}
	}
}

// handleIAMRoleOps routes role sub-operations.
func (h *Handler) handleIAMRoleOps(ctx context.Context, req *Request) *Response {
	var subReq IAMSubRequest
	if err := DecodePayload(req.Payload, &subReq); err != nil {
		return h.errorResponse(err)
	}

	subRequest := &Request{
		Opcode:  subReq.SubOp,
		Payload: subReq.Payload,
	}

	switch subReq.SubOp {
	case SubOpCreateRole:
		return h.handleIAMCreateRole(ctx, subRequest)
	case SubOpGetRole:
		return h.handleIAMGetRole(ctx, subRequest)
	case SubOpUpdateRole:
		return h.handleIAMUpdateRole(ctx, subRequest)
	case SubOpDeleteRole:
		return h.handleIAMDeleteRole(ctx, subRequest)
	case SubOpListRoles:
		return h.handleIAMListRoles(ctx, subRequest)
	default:
		return &Response{
			Status:  StatusInvalidRequest,
			Payload: []byte(fmt.Sprintf("unknown role sub-operation: 0x%02X", subReq.SubOp)),
		}
	}
}

// handleIAMPolicyOps routes policy sub-operations.
func (h *Handler) handleIAMPolicyOps(ctx context.Context, req *Request) *Response {
	var subReq IAMSubRequest
	if err := DecodePayload(req.Payload, &subReq); err != nil {
		return h.errorResponse(err)
	}

	subRequest := &Request{
		Opcode:  subReq.SubOp,
		Payload: subReq.Payload,
	}

	switch subReq.SubOp {
	case SubOpCreatePolicy:
		return h.handleIAMCreatePolicy(ctx, subRequest)
	case SubOpGetPolicy:
		return h.handleIAMGetPolicy(ctx, subRequest)
	case SubOpUpdatePolicy:
		return h.handleIAMUpdatePolicy(ctx, subRequest)
	case SubOpDeletePolicy:
		return h.handleIAMDeletePolicy(ctx, subRequest)
	case SubOpListPolicies:
		return h.handleIAMListPolicies(ctx, subRequest)
	case SubOpValidatePolicy:
		return h.handleIAMValidatePolicy(ctx, subRequest)
	case SubOpTestPolicy:
		return h.handleIAMTestPolicy(ctx, subRequest)
	default:
		return &Response{
			Status:  StatusInvalidRequest,
			Payload: []byte(fmt.Sprintf("unknown policy sub-operation: 0x%02X", subReq.SubOp)),
		}
	}
}

// handleIAMAppOps routes app sub-operations.
func (h *Handler) handleIAMAppOps(ctx context.Context, req *Request) *Response {
	var subReq IAMSubRequest
	if err := DecodePayload(req.Payload, &subReq); err != nil {
		return h.errorResponse(err)
	}

	subRequest := &Request{
		Opcode:  subReq.SubOp,
		Payload: subReq.Payload,
	}

	switch subReq.SubOp {
	case SubOpRegisterApp:
		return h.handleIAMRegisterApp(ctx, subRequest)
	case SubOpGetApp:
		return h.handleIAMGetApp(ctx, subRequest)
	case SubOpUpdateApp:
		return h.handleIAMUpdateApp(ctx, subRequest)
	case SubOpDeleteApp:
		return h.handleIAMDeleteApp(ctx, subRequest)
	case SubOpListApps:
		return h.handleIAMListApps(ctx, subRequest)
	case SubOpRotateAppSecret:
		return h.handleIAMRotateAppSecret(ctx, subRequest)
	default:
		return &Response{
			Status:  StatusInvalidRequest,
			Payload: []byte(fmt.Sprintf("unknown app sub-operation: 0x%02X", subReq.SubOp)),
		}
	}
}

// handleIAMMFAOps routes MFA sub-operations.
func (h *Handler) handleIAMMFAOps(ctx context.Context, req *Request) *Response {
	var subReq IAMSubRequest
	if err := DecodePayload(req.Payload, &subReq); err != nil {
		return h.errorResponse(err)
	}

	subRequest := &Request{
		Opcode:  subReq.SubOp,
		Payload: subReq.Payload,
	}

	switch subReq.SubOp {
	case SubOpMFABeginEnroll:
		return h.handleMFABeginEnroll(ctx, subRequest)
	case SubOpMFACompleteEnroll:
		return h.handleMFACompleteEnroll(ctx, subRequest)
	case SubOpMFACancelEnroll:
		return h.handleMFACancelEnroll(ctx, subRequest)
	case SubOpMFAGetStatus:
		return h.handleMFAGetStatus(ctx, subRequest)
	case SubOpMFADisable:
		return h.handleMFADisable(ctx, subRequest)
	case SubOpMFABeginChallenge:
		return h.handleMFABeginChallenge(ctx, subRequest)
	case SubOpMFAVerifyChallenge:
		return h.handleMFAVerifyChallenge(ctx, subRequest)
	case SubOpMFAGenerateBackupCodes:
		return h.handleMFAGenerateBackupCodes(ctx, subRequest)
	case SubOpMFAVerifyBackupCode:
		return h.handleMFAVerifyBackupCode(ctx, subRequest)
	case SubOpMFAGetBackupCodeStatus:
		return h.handleMFAGetBackupCodeStatus(ctx, subRequest)
	default:
		return &Response{
			Status:  StatusInvalidRequest,
			Payload: []byte(fmt.Sprintf("unknown MFA sub-operation: 0x%02X", subReq.SubOp)),
		}
	}
}

// handleIAMPEPOps routes PEP sub-operations via the legacy envelope pattern.
func (h *Handler) handleIAMPEPOps(ctx context.Context, req *Request) *Response {
	var subReq IAMSubRequest
	if err := DecodePayload(req.Payload, &subReq); err != nil {
		return h.errorResponse(err)
	}

	subRequest := &Request{
		Opcode:  subReq.SubOp,
		Payload: subReq.Payload,
	}

	switch subReq.SubOp {
	case SubOpPEPCheck:
		return h.handlePEPCheckAuthorization(ctx, subRequest)
	case SubOpPEPCheckBatch:
		return h.handlePEPCheckAuthorizationBatch(ctx, subRequest)
	default:
		return &Response{
			Status:  StatusInvalidRequest,
			Payload: []byte(fmt.Sprintf("unknown PEP sub-operation: 0x%02X", subReq.SubOp)),
		}
	}
}
