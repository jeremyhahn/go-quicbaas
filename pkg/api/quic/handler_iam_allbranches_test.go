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
	"testing"
)

// =============================================================================
// HandleRequestIAM All Direct Opcode Branches - Coverage Boost
// Tests all remaining direct opcodes in HandleRequestIAM switch statement
// =============================================================================

func TestHandleRequestIAM_AllDirectTenantOpcodes(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	ctx := context.Background()

	tests := []struct {
		name   string
		opcode byte
	}{
		{"CreateTenant", OpIAMCreateTenant},
		{"GetTenant", OpIAMGetTenant},
		{"UpdateTenant", OpIAMUpdateTenant},
		{"DeleteTenant", OpIAMDeleteTenant},
		{"ListTenants", OpIAMListTenants},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.HandleRequestIAM(ctx, &Request{
				Opcode:  tt.opcode,
				Payload: []byte(`{}`),
			})
			// All should fail due to missing IAM service
			if resp == nil || resp.Status == StatusOK {
				t.Fatalf("%s: Expected error for unavailable IAM service", tt.name)
			}
		})
	}
}

func TestHandleRequestIAM_AllDirectUserOpcodes(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	ctx := context.Background()

	tests := []struct {
		name   string
		opcode byte
	}{
		{"CreateUser", OpIAMCreateUser},
		{"GetUser", OpIAMGetUser},
		{"UpdateUser", OpIAMUpdateUser},
		{"DeleteUser", OpIAMDeleteUser},
		{"ListUsers", OpIAMListUsers},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.HandleRequestIAM(ctx, &Request{
				Opcode:  tt.opcode,
				Payload: []byte(`{}`),
			})
			// All should fail due to missing IAM service
			if resp == nil || resp.Status == StatusOK {
				t.Fatalf("%s: Expected error for unavailable IAM service", tt.name)
			}
		})
	}
}

func TestHandleRequestIAM_AllDirectGroupOpcodes(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	ctx := context.Background()

	tests := []struct {
		name   string
		opcode byte
	}{
		{"CreateGroup", OpIAMCreateGroup},
		{"GetGroup", OpIAMGetGroup},
		{"UpdateGroup", OpIAMUpdateGroup},
		{"DeleteGroup", OpIAMDeleteGroup},
		{"ListGroups", OpIAMListGroups},
		{"AddUserToGroup", OpIAMAddUserToGroup},
		{"RemoveUserFromGroup", OpIAMRemoveUserFromGroup},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.HandleRequestIAM(ctx, &Request{
				Opcode:  tt.opcode,
				Payload: []byte(`{}`),
			})
			// All should fail due to missing IAM service
			if resp == nil || resp.Status == StatusOK {
				t.Fatalf("%s: Expected error for unavailable IAM service", tt.name)
			}
		})
	}
}

func TestHandleRequestIAM_AllDirectRoleOpcodes(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	ctx := context.Background()

	tests := []struct {
		name   string
		opcode byte
	}{
		{"CreateRole", OpIAMCreateRole},
		{"GetRole", OpIAMGetRole},
		{"UpdateRole", OpIAMUpdateRole},
		{"DeleteRole", OpIAMDeleteRole},
		{"ListRoles", OpIAMListRoles},
		{"AssignRole", OpIAMAssignRole},
		{"RevokeRole", OpIAMRevokeRole},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.HandleRequestIAM(ctx, &Request{
				Opcode:  tt.opcode,
				Payload: []byte(`{}`),
			})
			// All should fail due to missing IAM service
			if resp == nil || resp.Status == StatusOK {
				t.Fatalf("%s: Expected error for unavailable IAM service", tt.name)
			}
		})
	}
}

func TestHandleRequestIAM_AllDirectAppOpcodes(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	ctx := context.Background()

	tests := []struct {
		name   string
		opcode byte
	}{
		{"RegisterApp", OpIAMRegisterApp},
		{"GetApp", OpIAMGetApp},
		{"UpdateApp", OpIAMUpdateApp},
		{"DeleteApp", OpIAMDeleteApp},
		{"ListApps", OpIAMListApps},
		{"RotateAppSecret", OpIAMRotateAppSecret},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.HandleRequestIAM(ctx, &Request{
				Opcode:  tt.opcode,
				Payload: []byte(`{}`),
			})
			// All should fail due to missing IAM service
			if resp == nil || resp.Status == StatusOK {
				t.Fatalf("%s: Expected error for unavailable IAM service", tt.name)
			}
		})
	}
}

func TestHandleRequestIAM_AllDirectPolicyOpcodes(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	ctx := context.Background()

	tests := []struct {
		name   string
		opcode byte
	}{
		{"CreatePolicy", OpIAMCreatePolicy},
		{"GetPolicy", OpIAMGetPolicy},
		{"UpdatePolicy", OpIAMUpdatePolicy},
		{"DeletePolicy", OpIAMDeletePolicy},
		{"ListPolicies", OpIAMListPolicies},
		{"ValidatePolicy", OpIAMValidatePolicy},
		{"TestPolicy", OpIAMTestPolicy},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.HandleRequestIAM(ctx, &Request{
				Opcode:  tt.opcode,
				Payload: []byte(`{}`),
			})
			// All should fail due to missing IAM service
			if resp == nil || resp.Status == StatusOK {
				t.Fatalf("%s: Expected error for unavailable IAM service", tt.name)
			}
		})
	}
}

func TestHandleRequestIAM_AllDirectPEPOpcodes(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	ctx := context.Background()

	tests := []struct {
		name   string
		opcode byte
	}{
		{"PEPCheck", OpPEPCheck},
		{"PEPCheckBatch", OpPEPCheckBatch},
		{"PEPSubscribe", OpPEPSubscribe},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.HandleRequestIAM(ctx, &Request{
				Opcode:  tt.opcode,
				Payload: []byte(`{}`),
			})
			// All should fail due to missing PEP service
			if resp == nil || resp.Status == StatusOK {
				t.Fatalf("%s: Expected error for unavailable PEP service", tt.name)
			}
		})
	}
}

// =============================================================================
// handleIAMAdminOps All Sub-Operation Branches
// =============================================================================

func TestHandleIAMAdminOps_AllSubOps(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	ctx := context.Background()

	tests := []struct {
		name  string
		subOp byte
	}{
		{"CreateAdminAssignment", SubOpCreateAdminAssignment},
		{"GetAdminAssignment", SubOpGetAdminAssignment},
		{"UpdateAdminAssignment", SubOpUpdateAdminAssignment},
		{"DeleteAdminAssignment", SubOpDeleteAdminAssignment},
		{"ListAdminAssignments", SubOpListAdminAssignments},
		{"CheckAdminPermission", SubOpCheckAdminPermission},
		{"StartImpersonation", SubOpStartImpersonation},
		{"EndImpersonation", SubOpEndImpersonation},
		{"GetActiveImpersonations", SubOpGetActiveImpersonations},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subReq := IAMSubRequest{
				SubOp:   tt.subOp,
				Payload: json.RawMessage(`{}`),
			}
			payload, _ := EncodePayload(subReq)

			resp := h.handleIAMAdminOps(ctx, &Request{Payload: payload})
			if resp.Status == StatusOK {
				t.Fatalf("%s: Expected error for unavailable admin service", tt.name)
			}
		})
	}
}

// =============================================================================
// handleIAMGDPROps All Sub-Operation Branches
// =============================================================================

func TestHandleIAMGDPROps_AllSubOps(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	ctx := context.Background()

	tests := []struct {
		name  string
		subOp byte
	}{
		{"GrantConsent", SubOpGrantConsent},
		{"WithdrawConsent", SubOpWithdrawConsent},
		{"GetConsent", SubOpGetConsent},
		{"ListUserConsents", SubOpListUserConsents},
		{"RequestExport", SubOpRequestExport},
		{"GetExportStatus", SubOpGetExportStatus},
		{"GenerateExport", SubOpGenerateExport},
		{"RequestDeletion", SubOpRequestDeletion},
		{"CancelDeletion", SubOpCancelDeletion},
		{"ProcessDeletion", SubOpProcessDeletion},
		{"GetDeletionStatus", SubOpGetDeletionStatus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subReq := IAMSubRequest{
				SubOp:   tt.subOp,
				Payload: json.RawMessage(`{}`),
			}
			payload, _ := EncodePayload(subReq)

			resp := h.handleIAMGDPROps(ctx, &Request{Payload: payload})
			if resp.Status == StatusOK {
				t.Fatalf("%s: Expected error for unavailable GDPR service", tt.name)
			}
		})
	}
}

// =============================================================================
// handleIAMTenantOps All Sub-Operation Branches
// =============================================================================

func TestHandleIAMTenantOps_AllSubOps(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	ctx := context.Background()

	tests := []struct {
		name  string
		subOp byte
	}{
		{"CreateTenant", SubOpCreateTenant},
		{"GetTenant", SubOpGetTenant},
		{"UpdateTenant", SubOpUpdateTenant},
		{"DeleteTenant", SubOpDeleteTenant},
		{"ListTenants", SubOpListTenants},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subReq := IAMSubRequest{
				SubOp:   tt.subOp,
				Payload: json.RawMessage(`{}`),
			}
			payload, _ := EncodePayload(subReq)

			resp := h.handleIAMTenantOps(ctx, &Request{Payload: payload})
			if resp.Status == StatusOK {
				t.Fatalf("%s: Expected error for unavailable IAM service", tt.name)
			}
		})
	}
}

// =============================================================================
// handleIAMUserOps All Sub-Operation Branches
// =============================================================================

func TestHandleIAMUserOps_AllSubOps(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	ctx := context.Background()

	tests := []struct {
		name  string
		subOp byte
	}{
		{"CreateUser", SubOpCreateUser},
		{"GetUser", SubOpGetUser},
		{"UpdateUser", SubOpUpdateUser},
		{"DeleteUser", SubOpDeleteUser},
		{"ListUsers", SubOpListUsers},
		{"AssignUserRole", SubOpAssignUserRole},
		{"RevokeUserRole", SubOpRevokeUserRole},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subReq := IAMSubRequest{
				SubOp:   tt.subOp,
				Payload: json.RawMessage(`{}`),
			}
			payload, _ := EncodePayload(subReq)

			resp := h.handleIAMUserOps(ctx, &Request{Payload: payload})
			if resp.Status == StatusOK {
				t.Fatalf("%s: Expected error for unavailable IAM service", tt.name)
			}
		})
	}
}

// =============================================================================
// handleIAMGroupOps All Sub-Operation Branches
// =============================================================================

func TestHandleIAMGroupOps_AllSubOps(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	ctx := context.Background()

	tests := []struct {
		name  string
		subOp byte
	}{
		{"CreateGroup", SubOpCreateGroup},
		{"GetGroup", SubOpGetGroup},
		{"UpdateGroup", SubOpUpdateGroup},
		{"DeleteGroup", SubOpDeleteGroup},
		{"ListGroups", SubOpListGroups},
		{"AddGroupMember", SubOpAddGroupMember},
		{"RemoveGroupMember", SubOpRemoveGroupMember},
		{"ListGroupMembers", SubOpListGroupMembers},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subReq := IAMSubRequest{
				SubOp:   tt.subOp,
				Payload: json.RawMessage(`{}`),
			}
			payload, _ := EncodePayload(subReq)

			resp := h.handleIAMGroupOps(ctx, &Request{Payload: payload})
			if resp.Status == StatusOK {
				t.Fatalf("%s: Expected error for unavailable IAM service", tt.name)
			}
		})
	}
}

// =============================================================================
// handleIAMRoleOps All Sub-Operation Branches
// =============================================================================

func TestHandleIAMRoleOps_AllSubOps(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	ctx := context.Background()

	tests := []struct {
		name  string
		subOp byte
	}{
		{"CreateRole", SubOpCreateRole},
		{"GetRole", SubOpGetRole},
		{"UpdateRole", SubOpUpdateRole},
		{"DeleteRole", SubOpDeleteRole},
		{"ListRoles", SubOpListRoles},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subReq := IAMSubRequest{
				SubOp:   tt.subOp,
				Payload: json.RawMessage(`{}`),
			}
			payload, _ := EncodePayload(subReq)

			resp := h.handleIAMRoleOps(ctx, &Request{Payload: payload})
			if resp.Status == StatusOK {
				t.Fatalf("%s: Expected error for unavailable IAM service", tt.name)
			}
		})
	}
}

// =============================================================================
// handleIAMPolicyOps All Sub-Operation Branches
// =============================================================================

func TestHandleIAMPolicyOps_AllSubOps(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	ctx := context.Background()

	tests := []struct {
		name  string
		subOp byte
	}{
		{"CreatePolicy", SubOpCreatePolicy},
		{"GetPolicy", SubOpGetPolicy},
		{"UpdatePolicy", SubOpUpdatePolicy},
		{"DeletePolicy", SubOpDeletePolicy},
		{"ListPolicies", SubOpListPolicies},
		{"ValidatePolicy", SubOpValidatePolicy},
		{"TestPolicy", SubOpTestPolicy},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subReq := IAMSubRequest{
				SubOp:   tt.subOp,
				Payload: json.RawMessage(`{}`),
			}
			payload, _ := EncodePayload(subReq)

			resp := h.handleIAMPolicyOps(ctx, &Request{Payload: payload})
			if resp.Status == StatusOK {
				t.Fatalf("%s: Expected error for unavailable IAM service", tt.name)
			}
		})
	}
}

// =============================================================================
// handleIAMAppOps All Sub-Operation Branches
// =============================================================================

func TestHandleIAMAppOps_AllSubOps(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	ctx := context.Background()

	tests := []struct {
		name  string
		subOp byte
	}{
		{"RegisterApp", SubOpRegisterApp},
		{"GetApp", SubOpGetApp},
		{"UpdateApp", SubOpUpdateApp},
		{"DeleteApp", SubOpDeleteApp},
		{"ListApps", SubOpListApps},
		{"RotateAppSecret", SubOpRotateAppSecret},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subReq := IAMSubRequest{
				SubOp:   tt.subOp,
				Payload: json.RawMessage(`{}`),
			}
			payload, _ := EncodePayload(subReq)

			resp := h.handleIAMAppOps(ctx, &Request{Payload: payload})
			if resp.Status == StatusOK {
				t.Fatalf("%s: Expected error for unavailable IAM service", tt.name)
			}
		})
	}
}

// =============================================================================
// handleIAMMFAOps All Sub-Operation Branches
// =============================================================================

func TestHandleIAMMFAOps_AllSubOps(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	ctx := context.Background()

	tests := []struct {
		name  string
		subOp byte
	}{
		{"MFABeginEnroll", SubOpMFABeginEnroll},
		{"MFACompleteEnroll", SubOpMFACompleteEnroll},
		{"MFACancelEnroll", SubOpMFACancelEnroll},
		{"MFAGetStatus", SubOpMFAGetStatus},
		{"MFADisable", SubOpMFADisable},
		{"MFABeginChallenge", SubOpMFABeginChallenge},
		{"MFAVerifyChallenge", SubOpMFAVerifyChallenge},
		{"MFAGenerateBackupCodes", SubOpMFAGenerateBackupCodes},
		{"MFAVerifyBackupCode", SubOpMFAVerifyBackupCode},
		{"MFAGetBackupCodeStatus", SubOpMFAGetBackupCodeStatus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subReq := IAMSubRequest{
				SubOp:   tt.subOp,
				Payload: json.RawMessage(`{}`),
			}
			payload, _ := EncodePayload(subReq)

			resp := h.handleIAMMFAOps(ctx, &Request{Payload: payload})
			if resp.Status == StatusOK {
				t.Fatalf("%s: Expected error for unavailable MFA service", tt.name)
			}
		})
	}
}

// =============================================================================
// handleIAMPEPOps All Sub-Operation Branches
// =============================================================================

func TestHandleIAMPEPOps_AllSubOps(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	ctx := context.Background()

	tests := []struct {
		name  string
		subOp byte
	}{
		{"PEPCheck", SubOpPEPCheck},
		{"PEPCheckBatch", SubOpPEPCheckBatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subReq := IAMSubRequest{
				SubOp:   tt.subOp,
				Payload: json.RawMessage(`{}`),
			}
			payload, _ := EncodePayload(subReq)

			resp := h.handleIAMPEPOps(ctx, &Request{Payload: payload})
			if resp.Status == StatusOK {
				t.Fatalf("%s: Expected error for unavailable PEP service", tt.name)
			}
		})
	}
}

// =============================================================================
// HandleRequestIAM Federation Opcode
// =============================================================================

func TestHandleRequestIAM_OpIAMFederation(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{
		SubOp:   0x01, // Some sub-operation
		Payload: json.RawMessage(`{}`),
	}
	payload, _ := EncodePayload(subReq)

	resp := h.HandleRequestIAM(ctx, &Request{
		Opcode:  OpIAMFederation,
		Payload: payload,
	})
	// Should fail due to missing federation service
	if resp == nil || resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable federation service")
	}
}
