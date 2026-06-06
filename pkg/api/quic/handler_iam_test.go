//go:build quic && iam

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
	"time"

	"errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// --- Content moved from handler_coverage_iam_extended_test.go ---

// =============================================================================
// IAM Admin Handler Tests - Service Unavailable and Bad Payload
// =============================================================================

func TestIAMCreateAdminAssignment_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMCreateAdminAssignment(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable admin service")
	}
}

func TestIAMCreateAdminAssignment_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMCreateAdminAssignment(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMGetAdminAssignment_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGetAdminAssignment(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable admin service")
	}
}

func TestIAMGetAdminAssignment_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGetAdminAssignment(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMUpdateAdminAssignment_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMUpdateAdminAssignment(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable admin service")
	}
}

func TestIAMUpdateAdminAssignment_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMUpdateAdminAssignment(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMDeleteAdminAssignment_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMDeleteAdminAssignment(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable admin service")
	}
}

func TestIAMDeleteAdminAssignment_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMDeleteAdminAssignment(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMListAdminAssignments_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMListAdminAssignments(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable admin service")
	}
}

func TestIAMListAdminAssignments_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMListAdminAssignments(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMCheckAdminPermission_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMCheckAdminPermission(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable admin service")
	}
}

func TestIAMCheckAdminPermission_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMCheckAdminPermission(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMStartImpersonation_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMStartImpersonation(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable admin service")
	}
}

func TestIAMStartImpersonation_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMStartImpersonation(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMEndImpersonation_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMEndImpersonation(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable admin service")
	}
}

func TestIAMEndImpersonation_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMEndImpersonation(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMGetActiveImpersonations_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGetActiveImpersonations(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable admin service")
	}
}

func TestIAMGetActiveImpersonations_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGetActiveImpersonations(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

// =============================================================================
// IAM GDPR Handler Tests - Service Unavailable and Bad Payload
// =============================================================================

func TestIAMGrantConsent_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGrantConsent(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable GDPR service")
	}
}

func TestIAMGrantConsent_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGrantConsent(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMWithdrawConsent_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMWithdrawConsent(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable GDPR service")
	}
}

func TestIAMWithdrawConsent_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMWithdrawConsent(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMGetConsent_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGetConsent(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable GDPR service")
	}
}

func TestIAMGetConsent_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGetConsent(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMListUserConsents_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMListUserConsents(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable GDPR service")
	}
}

func TestIAMListUserConsents_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMListUserConsents(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMRequestExport_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMRequestExport(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable GDPR service")
	}
}

func TestIAMRequestExport_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMRequestExport(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMGetExportStatus_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGetExportStatus(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable GDPR service")
	}
}

func TestIAMGetExportStatus_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGetExportStatus(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMGenerateExport_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGenerateExport(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable GDPR service")
	}
}

func TestIAMGenerateExport_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGenerateExport(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMRequestDeletion_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMRequestDeletion(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable GDPR service")
	}
}

func TestIAMRequestDeletion_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMRequestDeletion(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMCancelDeletion_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMCancelDeletion(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable GDPR service")
	}
}

func TestIAMCancelDeletion_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMCancelDeletion(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMProcessDeletion_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMProcessDeletion(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable GDPR service")
	}
}

func TestIAMProcessDeletion_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMProcessDeletion(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMGetDeletionStatus_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGetDeletionStatus(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable GDPR service")
	}
}

func TestIAMGetDeletionStatus_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGetDeletionStatus(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

// =============================================================================
// MFA Handler Tests - Service Unavailable and Bad Payload
// =============================================================================

func TestMFABeginEnroll_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFABeginEnroll(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil MFA service")
	}
}

func TestMFABeginEnroll_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFABeginEnroll(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestMFACompleteEnroll_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFACompleteEnroll(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil MFA service")
	}
}

func TestMFACompleteEnroll_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFACompleteEnroll(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestMFACancelEnroll_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFACancelEnroll(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil MFA service")
	}
}

func TestMFACancelEnroll_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFACancelEnroll(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestMFAGetStatus_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFAGetStatus(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil MFA service")
	}
}

func TestMFAGetStatus_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFAGetStatus(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestMFADisable_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFADisable(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil MFA service")
	}
}

func TestMFADisable_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFADisable(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestMFABeginChallenge_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFABeginChallenge(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil MFA service")
	}
}

func TestMFABeginChallenge_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFABeginChallenge(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestMFAVerifyChallenge_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFAVerifyChallenge(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil MFA service")
	}
}

func TestMFAVerifyChallenge_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFAVerifyChallenge(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestMFAGenerateBackupCodes_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFAGenerateBackupCodes(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil MFA service")
	}
}

func TestMFAGenerateBackupCodes_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFAGenerateBackupCodes(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestMFAVerifyBackupCode_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFAVerifyBackupCode(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil MFA service")
	}
}

func TestMFAVerifyBackupCode_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFAVerifyBackupCode(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestMFAGetBackupCodeStatus_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFAGetBackupCodeStatus(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil MFA service")
	}
}

func TestMFAGetBackupCodeStatus_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleMFAGetBackupCodeStatus(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

// =============================================================================
// PEP Handler Tests - Service Unavailable and Bad Payload
// =============================================================================

func TestPEPCheck_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handlePEPCheck(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable PEP service")
	}
}

func TestPEPCheck_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handlePEPCheck(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestPEPCheckBatch_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handlePEPCheckBatch(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable PEP service")
	}
}

func TestPEPCheckBatch_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handlePEPCheckBatch(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestPEPSubscribe_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handlePEPSubscribe(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable PEP subscribe service")
	}
}

func TestPEPSubscribe_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handlePEPSubscribe(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestPEPCheckAuthorization_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handlePEPCheckAuthorization(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable PEP service")
	}
}

func TestPEPCheckAuthorization_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handlePEPCheckAuthorization(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestPEPCheckAuthorizationBatch_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handlePEPCheckAuthorizationBatch(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable PEP service")
	}
}

func TestPEPCheckAuthorizationBatch_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handlePEPCheckAuthorizationBatch(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

// =============================================================================
// PEP Validation Functions Tests
// =============================================================================

func TestValidateCheckRequest_NilRequest(t *testing.T) {
	err := validateCheckRequest(nil)
	if err == nil {
		t.Fatal("Expected error for nil request")
	}
}

func TestValidateBatchCheckRequest_NilRequest(t *testing.T) {
	err := validateBatchCheckRequest(nil)
	if err == nil {
		t.Fatal("Expected error for nil request")
	}
}

func TestValidateSubscribeRequest_NilRequest(t *testing.T) {
	err := validateSubscribeRequest(nil)
	if err == nil {
		t.Fatal("Expected error for nil request")
	}
}

// =============================================================================
// IAM Routes Handler Tests - Sub-Operations
// =============================================================================

func TestHandleIAMAdminOps_DecodeError(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMAdminOps(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestHandleIAMAdminOps_UnknownSubOp(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: 0xFF, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.handleIAMAdminOps(ctx, &Request{Payload: payload})

	if resp.Status != StatusInvalidRequest {
		t.Fatalf("Expected StatusInvalidRequest, got 0x%02X", resp.Status)
	}
}

func TestHandleIAMGDPROps_DecodeError(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGDPROps(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestHandleIAMGDPROps_UnknownSubOp(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: 0xFF, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.handleIAMGDPROps(ctx, &Request{Payload: payload})

	if resp.Status != StatusInvalidRequest {
		t.Fatalf("Expected StatusInvalidRequest, got 0x%02X", resp.Status)
	}
}

func TestHandleIAMTenantOps_DecodeError(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMTenantOps(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestHandleIAMTenantOps_UnknownSubOp(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: 0xFF, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.handleIAMTenantOps(ctx, &Request{Payload: payload})

	if resp.Status != StatusInvalidRequest {
		t.Fatalf("Expected StatusInvalidRequest, got 0x%02X", resp.Status)
	}
}

func TestHandleIAMUserOps_DecodeError(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMUserOps(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestHandleIAMUserOps_UnknownSubOp(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: 0xFF, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.handleIAMUserOps(ctx, &Request{Payload: payload})

	if resp.Status != StatusInvalidRequest {
		t.Fatalf("Expected StatusInvalidRequest, got 0x%02X", resp.Status)
	}
}

func TestHandleIAMGroupOps_DecodeError(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGroupOps(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestHandleIAMGroupOps_UnknownSubOp(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: 0xFF, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.handleIAMGroupOps(ctx, &Request{Payload: payload})

	if resp.Status != StatusInvalidRequest {
		t.Fatalf("Expected StatusInvalidRequest, got 0x%02X", resp.Status)
	}
}

func TestHandleIAMRoleOps_DecodeError(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMRoleOps(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestHandleIAMRoleOps_UnknownSubOp(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: 0xFF, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.handleIAMRoleOps(ctx, &Request{Payload: payload})

	if resp.Status != StatusInvalidRequest {
		t.Fatalf("Expected StatusInvalidRequest, got 0x%02X", resp.Status)
	}
}

func TestHandleIAMPolicyOps_DecodeError(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMPolicyOps(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestHandleIAMPolicyOps_UnknownSubOp(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: 0xFF, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.handleIAMPolicyOps(ctx, &Request{Payload: payload})

	if resp.Status != StatusInvalidRequest {
		t.Fatalf("Expected StatusInvalidRequest, got 0x%02X", resp.Status)
	}
}

func TestHandleIAMAppOps_DecodeError(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMAppOps(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestHandleIAMAppOps_UnknownSubOp(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: 0xFF, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.handleIAMAppOps(ctx, &Request{Payload: payload})

	if resp.Status != StatusInvalidRequest {
		t.Fatalf("Expected StatusInvalidRequest, got 0x%02X", resp.Status)
	}
}

func TestHandleIAMMFAOps_DecodeError(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMMFAOps(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestHandleIAMMFAOps_UnknownSubOp(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: 0xFF, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.handleIAMMFAOps(ctx, &Request{Payload: payload})

	if resp.Status != StatusInvalidRequest {
		t.Fatalf("Expected StatusInvalidRequest, got 0x%02X", resp.Status)
	}
}

func TestHandleIAMPEPOps_DecodeError(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMPEPOps(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestHandleIAMPEPOps_UnknownSubOp(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: 0xFF, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.handleIAMPEPOps(ctx, &Request{Payload: payload})

	if resp.Status != StatusInvalidRequest {
		t.Fatalf("Expected StatusInvalidRequest, got 0x%02X", resp.Status)
	}
}

// =============================================================================
// HandleRequestIAM Tests - Direct Opcode Pattern
// =============================================================================

func TestHandleRequestIAM_UnknownOpcode(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.HandleRequestIAM(ctx, &Request{Opcode: 0x50, Payload: []byte("{}")})

	// Should return nil for unknown opcode (let base handler try)
	if resp != nil {
		t.Fatal("Expected nil for unknown opcode")
	}
}

func TestHandleRequestIAM_OpIAMAdmin(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	// Valid sub-request but service unavailable
	subReq := IAMSubRequest{SubOp: SubOpCreateAdminAssignment, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.HandleRequestIAM(ctx, &Request{Opcode: OpIAMAdmin, Payload: payload})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable service")
	}
}

func TestHandleRequestIAM_OpIAMGDPR(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: SubOpGrantConsent, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.HandleRequestIAM(ctx, &Request{Opcode: OpIAMGDPR, Payload: payload})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable service")
	}
}

func TestHandleRequestIAM_OpIAMTenant(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: SubOpCreateTenant, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.HandleRequestIAM(ctx, &Request{Opcode: OpIAMTenant, Payload: payload})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable service")
	}
}

func TestHandleRequestIAM_OpIAMUser(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: SubOpCreateUser, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.HandleRequestIAM(ctx, &Request{Opcode: OpIAMUser, Payload: payload})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable service")
	}
}

func TestHandleRequestIAM_OpIAMGroup(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: SubOpCreateGroup, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.HandleRequestIAM(ctx, &Request{Opcode: OpIAMGroup, Payload: payload})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable service")
	}
}

func TestHandleRequestIAM_OpIAMRole(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: SubOpCreateRole, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.HandleRequestIAM(ctx, &Request{Opcode: OpIAMRole, Payload: payload})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable service")
	}
}

func TestHandleRequestIAM_OpIAMPolicy(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: SubOpCreatePolicy, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.HandleRequestIAM(ctx, &Request{Opcode: OpIAMPolicy, Payload: payload})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable service")
	}
}

func TestHandleRequestIAM_OpIAMApp(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: SubOpRegisterApp, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.HandleRequestIAM(ctx, &Request{Opcode: OpIAMApp, Payload: payload})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable service")
	}
}

func TestHandleRequestIAM_OpMFA(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: SubOpMFABeginEnroll, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.HandleRequestIAM(ctx, &Request{Opcode: OpMFA, Payload: payload})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable service")
	}
}

func TestHandleRequestIAM_OpPEP(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	subReq := IAMSubRequest{SubOp: SubOpPEPCheck, Payload: json.RawMessage("{}")}
	payload, _ := EncodePayload(subReq)

	resp := h.HandleRequestIAM(ctx, &Request{Opcode: OpPEP, Payload: payload})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for unavailable service")
	}
}

// =============================================================================
// IAM Service Management Tests
// =============================================================================

func TestWithIAMService(t *testing.T) {
	h := createTestHandler()

	// Use WithIAMService option
	opt := WithIAMService(nil)
	opt(h)

	// Service should still be nil since we passed nil
	if h.getIAMService() != nil {
		t.Fatal("Expected nil IAM service")
	}
}

func TestWithMFAService(t *testing.T) {
	h := createTestHandler()

	// Use WithMFAService option
	opt := WithMFAService(nil)
	opt(h)

	// Service should still be nil since we passed nil
	if h.getMFAService() != nil {
		t.Fatal("Expected nil MFA service")
	}
}

func TestClearIAMService(t *testing.T) {
	h := createTestHandler()

	// Clear should work without error
	h.clearIAMService()

	if h.getIAMService() != nil {
		t.Fatal("Expected nil IAM service after clear")
	}
}

func TestClearMFAService(t *testing.T) {
	h := createTestHandler()

	// Clear should work without error
	h.clearMFAService()

	if h.getMFAService() != nil {
		t.Fatal("Expected nil MFA service after clear")
	}
}

// =============================================================================
// OpcodeToStringIAM Tests
// =============================================================================

func TestOpcodeToStringIAM(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{OpIAMAdmin, "IAMAdmin"},
		{OpIAMGDPR, "IAMGDPR"},
		{OpIAMFederation, "IAMFederation"},
		{OpIAMTenant, "IAMTenant"},
		{OpIAMUser, "IAMUser"},
		{OpIAMGroup, "IAMGroup"},
		{OpIAMRole, "IAMRole"},
		{OpIAMPolicy, "IAMPolicy"},
		{OpIAMApp, "IAMApp"},
		{OpMFA, "MFA"},
		{OpPEP, "PEP"},
		{0x00, ""}, // Unknown should return empty
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := OpcodeToStringIAM(tt.opcode)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// --- Content moved from handler_iam_coverage_91_test.go ---

// =============================================================================
// Mock IAM Service - success responses
// =============================================================================

type mockIAMService91 struct{}

var _ service.IAMService = (*mockIAMService91)(nil)

func (m *mockIAMService91) CreateTenant(_ context.Context, _ *service.CreateTenantRequest) (*service.CreateTenantResponse, error) {
	return &service.CreateTenantResponse{}, nil
}
func (m *mockIAMService91) GetTenant(_ context.Context, _ *service.GetTenantRequest) (*service.GetTenantResponse, error) {
	return &service.GetTenantResponse{}, nil
}
func (m *mockIAMService91) UpdateTenant(_ context.Context, _ *service.UpdateTenantRequest) (*service.UpdateTenantResponse, error) {
	return &service.UpdateTenantResponse{}, nil
}
func (m *mockIAMService91) DeleteTenant(_ context.Context, _ *service.DeleteTenantRequest) (*service.DeleteTenantResponse, error) {
	return &service.DeleteTenantResponse{}, nil
}
func (m *mockIAMService91) ListTenants(_ context.Context, _ *service.ListTenantsRequest) (*service.ListTenantsResponse, error) {
	return &service.ListTenantsResponse{}, nil
}
func (m *mockIAMService91) CreateUser(_ context.Context, _ *service.IAMCreateUserRequest) (*service.IAMCreateUserResponse, error) {
	return &service.IAMCreateUserResponse{}, nil
}
func (m *mockIAMService91) GetUser(_ context.Context, _ *service.IAMGetUserRequest) (*service.IAMGetUserResponse, error) {
	return &service.IAMGetUserResponse{}, nil
}
func (m *mockIAMService91) UpdateUser(_ context.Context, _ *service.IAMUpdateUserRequest) (*service.IAMUpdateUserResponse, error) {
	return &service.IAMUpdateUserResponse{}, nil
}
func (m *mockIAMService91) DeleteUser(_ context.Context, _ *service.IAMDeleteUserRequest) (*service.IAMDeleteUserResponse, error) {
	return &service.IAMDeleteUserResponse{}, nil
}
func (m *mockIAMService91) ListUsers(_ context.Context, _ *service.IAMListUsersRequest) (*service.IAMListUsersResponse, error) {
	return &service.IAMListUsersResponse{}, nil
}
func (m *mockIAMService91) CreateGroup(_ context.Context, _ *service.CreateGroupRequest) (*service.CreateGroupResponse, error) {
	return &service.CreateGroupResponse{}, nil
}
func (m *mockIAMService91) GetGroup(_ context.Context, _ *service.GetGroupRequest) (*service.GetGroupResponse, error) {
	return &service.GetGroupResponse{Group: &iam.Group{Members: []string{"user1"}}}, nil
}
func (m *mockIAMService91) UpdateGroup(_ context.Context, _ *service.UpdateGroupRequest) (*service.UpdateGroupResponse, error) {
	return &service.UpdateGroupResponse{}, nil
}
func (m *mockIAMService91) DeleteGroup(_ context.Context, _ *service.DeleteGroupRequest) (*service.DeleteGroupResponse, error) {
	return &service.DeleteGroupResponse{}, nil
}
func (m *mockIAMService91) ListGroups(_ context.Context, _ *service.ListGroupsRequest) (*service.ListGroupsResponse, error) {
	return &service.ListGroupsResponse{}, nil
}
func (m *mockIAMService91) AddUserToGroup(_ context.Context, _ *service.AddUserToGroupRequest) (*service.AddUserToGroupResponse, error) {
	return &service.AddUserToGroupResponse{}, nil
}
func (m *mockIAMService91) RemoveUserFromGroup(_ context.Context, _ *service.RemoveUserFromGroupRequest) (*service.RemoveUserFromGroupResponse, error) {
	return &service.RemoveUserFromGroupResponse{}, nil
}
func (m *mockIAMService91) CreateRole(_ context.Context, _ *service.IAMCreateRoleRequest) (*service.IAMCreateRoleResponse, error) {
	return &service.IAMCreateRoleResponse{}, nil
}
func (m *mockIAMService91) GetRole(_ context.Context, _ *service.IAMGetRoleRequest) (*service.IAMGetRoleResponse, error) {
	return &service.IAMGetRoleResponse{}, nil
}
func (m *mockIAMService91) UpdateRole(_ context.Context, _ *service.IAMUpdateRoleRequest) (*service.IAMUpdateRoleResponse, error) {
	return &service.IAMUpdateRoleResponse{}, nil
}
func (m *mockIAMService91) DeleteRole(_ context.Context, _ *service.IAMDeleteRoleRequest) (*service.IAMDeleteRoleResponse, error) {
	return &service.IAMDeleteRoleResponse{}, nil
}
func (m *mockIAMService91) ListRoles(_ context.Context, _ *service.IAMListRolesRequest) (*service.IAMListRolesResponse, error) {
	return &service.IAMListRolesResponse{}, nil
}
func (m *mockIAMService91) AssignRole(_ context.Context, _ *service.AssignRoleRequest) (*service.AssignRoleResponse, error) {
	return &service.AssignRoleResponse{}, nil
}
func (m *mockIAMService91) RevokeRole(_ context.Context, _ *service.RevokeRoleRequest) (*service.RevokeRoleResponse, error) {
	return &service.RevokeRoleResponse{}, nil
}
func (m *mockIAMService91) RegisterApp(_ context.Context, _ *service.RegisterAppRequest) (*service.RegisterAppResponse, error) {
	return &service.RegisterAppResponse{}, nil
}
func (m *mockIAMService91) GetApp(_ context.Context, _ *service.GetAppRequest) (*service.GetAppResponse, error) {
	return &service.GetAppResponse{}, nil
}
func (m *mockIAMService91) UpdateApp(_ context.Context, _ *service.UpdateAppRequest) (*service.UpdateAppResponse, error) {
	return &service.UpdateAppResponse{}, nil
}
func (m *mockIAMService91) DeleteApp(_ context.Context, _ *service.DeleteAppRequest) (*service.DeleteAppResponse, error) {
	return &service.DeleteAppResponse{}, nil
}
func (m *mockIAMService91) ListApps(_ context.Context, _ *service.ListAppsRequest) (*service.ListAppsResponse, error) {
	return &service.ListAppsResponse{}, nil
}
func (m *mockIAMService91) RotateAppSecret(_ context.Context, _ *service.RotateAppSecretRequest) (*service.RotateAppSecretResponse, error) {
	return &service.RotateAppSecretResponse{}, nil
}
func (m *mockIAMService91) CreatePolicy(_ context.Context, _ *service.CreatePolicyRequest) (*service.CreatePolicyResponse, error) {
	return &service.CreatePolicyResponse{}, nil
}
func (m *mockIAMService91) GetPolicy(_ context.Context, _ *service.GetPolicyRequest) (*service.GetPolicyResponse, error) {
	return &service.GetPolicyResponse{}, nil
}
func (m *mockIAMService91) UpdatePolicy(_ context.Context, _ *service.UpdatePolicyRequest) (*service.UpdatePolicyResponse, error) {
	return &service.UpdatePolicyResponse{}, nil
}
func (m *mockIAMService91) DeletePolicy(_ context.Context, _ *service.DeletePolicyRequest) (*service.DeletePolicyResponse, error) {
	return &service.DeletePolicyResponse{}, nil
}
func (m *mockIAMService91) ListPolicies(_ context.Context, _ *service.ListPoliciesRequest) (*service.ListPoliciesResponse, error) {
	return &service.ListPoliciesResponse{}, nil
}
func (m *mockIAMService91) ValidatePolicy(_ context.Context, _ *service.ValidatePolicyRequest) (*service.ValidatePolicyResponse, error) {
	return &service.ValidatePolicyResponse{}, nil
}
func (m *mockIAMService91) TestPolicy(_ context.Context, _ *service.TestPolicyRequest) (*service.TestPolicyResponse, error) {
	return &service.TestPolicyResponse{}, nil
}

// =============================================================================
// Error IAM Service - every method returns an error
// =============================================================================

var errIAMService91 = errors.New("service error")

type errorIAMService91 struct{}

var _ service.IAMService = (*errorIAMService91)(nil)

func (m *errorIAMService91) CreateTenant(_ context.Context, _ *service.CreateTenantRequest) (*service.CreateTenantResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) GetTenant(_ context.Context, _ *service.GetTenantRequest) (*service.GetTenantResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) UpdateTenant(_ context.Context, _ *service.UpdateTenantRequest) (*service.UpdateTenantResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) DeleteTenant(_ context.Context, _ *service.DeleteTenantRequest) (*service.DeleteTenantResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) ListTenants(_ context.Context, _ *service.ListTenantsRequest) (*service.ListTenantsResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) CreateUser(_ context.Context, _ *service.IAMCreateUserRequest) (*service.IAMCreateUserResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) GetUser(_ context.Context, _ *service.IAMGetUserRequest) (*service.IAMGetUserResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) UpdateUser(_ context.Context, _ *service.IAMUpdateUserRequest) (*service.IAMUpdateUserResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) DeleteUser(_ context.Context, _ *service.IAMDeleteUserRequest) (*service.IAMDeleteUserResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) ListUsers(_ context.Context, _ *service.IAMListUsersRequest) (*service.IAMListUsersResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) CreateGroup(_ context.Context, _ *service.CreateGroupRequest) (*service.CreateGroupResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) GetGroup(_ context.Context, _ *service.GetGroupRequest) (*service.GetGroupResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) UpdateGroup(_ context.Context, _ *service.UpdateGroupRequest) (*service.UpdateGroupResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) DeleteGroup(_ context.Context, _ *service.DeleteGroupRequest) (*service.DeleteGroupResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) ListGroups(_ context.Context, _ *service.ListGroupsRequest) (*service.ListGroupsResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) AddUserToGroup(_ context.Context, _ *service.AddUserToGroupRequest) (*service.AddUserToGroupResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) RemoveUserFromGroup(_ context.Context, _ *service.RemoveUserFromGroupRequest) (*service.RemoveUserFromGroupResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) CreateRole(_ context.Context, _ *service.IAMCreateRoleRequest) (*service.IAMCreateRoleResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) GetRole(_ context.Context, _ *service.IAMGetRoleRequest) (*service.IAMGetRoleResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) UpdateRole(_ context.Context, _ *service.IAMUpdateRoleRequest) (*service.IAMUpdateRoleResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) DeleteRole(_ context.Context, _ *service.IAMDeleteRoleRequest) (*service.IAMDeleteRoleResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) ListRoles(_ context.Context, _ *service.IAMListRolesRequest) (*service.IAMListRolesResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) AssignRole(_ context.Context, _ *service.AssignRoleRequest) (*service.AssignRoleResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) RevokeRole(_ context.Context, _ *service.RevokeRoleRequest) (*service.RevokeRoleResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) RegisterApp(_ context.Context, _ *service.RegisterAppRequest) (*service.RegisterAppResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) GetApp(_ context.Context, _ *service.GetAppRequest) (*service.GetAppResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) UpdateApp(_ context.Context, _ *service.UpdateAppRequest) (*service.UpdateAppResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) DeleteApp(_ context.Context, _ *service.DeleteAppRequest) (*service.DeleteAppResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) ListApps(_ context.Context, _ *service.ListAppsRequest) (*service.ListAppsResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) RotateAppSecret(_ context.Context, _ *service.RotateAppSecretRequest) (*service.RotateAppSecretResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) CreatePolicy(_ context.Context, _ *service.CreatePolicyRequest) (*service.CreatePolicyResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) GetPolicy(_ context.Context, _ *service.GetPolicyRequest) (*service.GetPolicyResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) UpdatePolicy(_ context.Context, _ *service.UpdatePolicyRequest) (*service.UpdatePolicyResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) DeletePolicy(_ context.Context, _ *service.DeletePolicyRequest) (*service.DeletePolicyResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) ListPolicies(_ context.Context, _ *service.ListPoliciesRequest) (*service.ListPoliciesResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) ValidatePolicy(_ context.Context, _ *service.ValidatePolicyRequest) (*service.ValidatePolicyResponse, error) {
	return nil, errIAMService91
}
func (m *errorIAMService91) TestPolicy(_ context.Context, _ *service.TestPolicyRequest) (*service.TestPolicyResponse, error) {
	return nil, errIAMService91
}

// =============================================================================
// Error MFA Service - every method returns an error
// =============================================================================

type errorMFAService91 struct{}

func (m *errorMFAService91) BeginEnrollment(_ context.Context, _, _ string, _ iam.MFAMethod) (*iam.MFAEnrollResponse, error) {
	return nil, errIAMService91
}
func (m *errorMFAService91) CompleteEnrollment(_ context.Context, _, _ string) error {
	return errIAMService91
}
func (m *errorMFAService91) CancelEnrollment(_ context.Context, _ string) error {
	return errIAMService91
}
func (m *errorMFAService91) BeginChallenge(_ context.Context, _, _, _ string) (*iam.MFAChallenge, error) {
	return nil, errIAMService91
}
func (m *errorMFAService91) VerifyChallenge(_ context.Context, _, _ string) (*iam.MFAVerifyResponse, error) {
	return nil, errIAMService91
}
func (m *errorMFAService91) VerifyCode(_ context.Context, _, _, _ string) error {
	return errIAMService91
}
func (m *errorMFAService91) GetMFAStatus(_ context.Context, _, _ string) (*iam.MFAStatus, error) {
	return nil, errIAMService91
}
func (m *errorMFAService91) IsMFARequired(_ context.Context, _, _ string) (bool, error) {
	return false, errIAMService91
}
func (m *errorMFAService91) DisableMFA(_ context.Context, _, _ string) error {
	return errIAMService91
}
func (m *errorMFAService91) GenerateBackupCodes(_ context.Context, _, _ string) ([]string, error) {
	return nil, errIAMService91
}
func (m *errorMFAService91) VerifyBackupCode(_ context.Context, _, _, _ string) error {
	return errIAMService91
}
func (m *errorMFAService91) GetBackupCodeStatus(_ context.Context, _, _ string) (*iam.BackupCodeStatus, error) {
	return nil, errIAMService91
}

// =============================================================================
// Mock MFA Service - success responses
// =============================================================================

type mockMFAService91 struct{}

func (m *mockMFAService91) BeginEnrollment(_ context.Context, _, _ string, _ iam.MFAMethod) (*iam.MFAEnrollResponse, error) {
	return &iam.MFAEnrollResponse{}, nil
}
func (m *mockMFAService91) CompleteEnrollment(_ context.Context, _, _ string) error { return nil }
func (m *mockMFAService91) CancelEnrollment(_ context.Context, _ string) error      { return nil }
func (m *mockMFAService91) BeginChallenge(_ context.Context, _, _, _ string) (*iam.MFAChallenge, error) {
	return &iam.MFAChallenge{}, nil
}
func (m *mockMFAService91) VerifyChallenge(_ context.Context, _, _ string) (*iam.MFAVerifyResponse, error) {
	return &iam.MFAVerifyResponse{}, nil
}
func (m *mockMFAService91) VerifyCode(_ context.Context, _, _, _ string) error { return nil }
func (m *mockMFAService91) GetMFAStatus(_ context.Context, _, _ string) (*iam.MFAStatus, error) {
	return &iam.MFAStatus{}, nil
}
func (m *mockMFAService91) IsMFARequired(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}
func (m *mockMFAService91) DisableMFA(_ context.Context, _, _ string) error { return nil }
func (m *mockMFAService91) GenerateBackupCodes(_ context.Context, _, _ string) ([]string, error) {
	return []string{"code1", "code2"}, nil
}
func (m *mockMFAService91) VerifyBackupCode(_ context.Context, _, _, _ string) error { return nil }
func (m *mockMFAService91) GetBackupCodeStatus(_ context.Context, _, _ string) (*iam.BackupCodeStatus, error) {
	return &iam.BackupCodeStatus{}, nil
}

// =============================================================================
// Mock PEP+Admin+GDPR Service (combined)
// =============================================================================

type mockCombinedIAMService91 struct{ mockService }

func (m *mockCombinedIAMService91) Check(_ context.Context, _ *service.CheckRequest) (*service.CheckResponse, error) {
	return &service.CheckResponse{Allowed: true}, nil
}
func (m *mockCombinedIAMService91) CheckBatch(_ context.Context, _ *service.BatchCheckRequest) (*service.BatchCheckResponse, error) {
	return &service.BatchCheckResponse{}, nil
}
func (m *mockCombinedIAMService91) Subscribe(_ context.Context, _ *service.SubscribePolicyUpdatesRequest) (<-chan *service.PolicyUpdate, error) {
	ch := make(chan *service.PolicyUpdate)
	close(ch)
	return ch, nil
}
func (m *mockCombinedIAMService91) CreateAdminAssignment(_ context.Context, _ iam.CreateAdminAssignmentOptions) (*iam.AdminAssignment, error) {
	return &iam.AdminAssignment{}, nil
}
func (m *mockCombinedIAMService91) GetAdminAssignment(_ context.Context, _ string) (*iam.AdminAssignment, error) {
	return &iam.AdminAssignment{}, nil
}
func (m *mockCombinedIAMService91) UpdateAdminAssignment(_ context.Context, _ string, _ iam.UpdateAdminAssignmentOptions) (*iam.AdminAssignment, error) {
	return &iam.AdminAssignment{}, nil
}
func (m *mockCombinedIAMService91) DeleteAdminAssignment(_ context.Context, _ string) error {
	return nil
}
func (m *mockCombinedIAMService91) ListAdminAssignments(_ context.Context, _ iam.ListAdminAssignmentsOptions) ([]*iam.AdminAssignment, error) {
	return []*iam.AdminAssignment{}, nil
}
func (m *mockCombinedIAMService91) CheckAdminPermission(_ context.Context, _ *iam.AdminCheckRequest) (*iam.AdminCheckResponse, error) {
	return &iam.AdminCheckResponse{}, nil
}
func (m *mockCombinedIAMService91) StartImpersonation(_ context.Context, _ *iam.StartImpersonationOptions) (*iam.ImpersonationSession, error) {
	return &iam.ImpersonationSession{}, nil
}
func (m *mockCombinedIAMService91) EndImpersonation(_ context.Context, _ string) error { return nil }
func (m *mockCombinedIAMService91) GetActiveImpersonations(_ context.Context, _ string) ([]*iam.ImpersonationSession, error) {
	return []*iam.ImpersonationSession{}, nil
}
func (m *mockCombinedIAMService91) GrantConsent(_ context.Context, _, _ string, _ iam.ConsentPurpose, _ string, _ *time.Time, _, _ string) (*iam.Consent, error) {
	return &iam.Consent{}, nil
}
func (m *mockCombinedIAMService91) WithdrawConsent(_ context.Context, _, _ string, _ iam.ConsentPurpose, _, _ string) error {
	return nil
}
func (m *mockCombinedIAMService91) GetConsent(_ context.Context, _, _ string, _ iam.ConsentPurpose) (*iam.Consent, error) {
	return &iam.Consent{}, nil
}
func (m *mockCombinedIAMService91) ListUserConsents(_ context.Context, _, _ string) ([]*iam.Consent, error) {
	return []*iam.Consent{}, nil
}
func (m *mockCombinedIAMService91) RequestExport(_ context.Context, _, _, _ string, _ iam.DataExportFormat, _, _ []string) (*iam.DataExportRequest, error) {
	return &iam.DataExportRequest{}, nil
}
func (m *mockCombinedIAMService91) GetExportStatus(_ context.Context, _ string) (*iam.DataExportRequest, error) {
	return &iam.DataExportRequest{}, nil
}
func (m *mockCombinedIAMService91) GenerateExport(_ context.Context, _ string) (*iam.UserDataExport, error) {
	return &iam.UserDataExport{}, nil
}
func (m *mockCombinedIAMService91) RequestDeletion(_ context.Context, _, _, _, _ string, _, _ []string, _ int) (*iam.DataDeletionRequest, error) {
	return &iam.DataDeletionRequest{}, nil
}
func (m *mockCombinedIAMService91) CancelDeletion(_ context.Context, _, _ string) error { return nil }
func (m *mockCombinedIAMService91) ProcessDeletion(_ context.Context, _ string) (*iam.DeletionSummary, error) {
	return &iam.DeletionSummary{}, nil
}
func (m *mockCombinedIAMService91) GetDeletionStatus(_ context.Context, _ string) (*iam.DataDeletionRequest, error) {
	return &iam.DataDeletionRequest{}, nil
}

// =============================================================================
// Error combined service
// =============================================================================

type errorCombinedIAMService91 struct{ mockService }

func (m *errorCombinedIAMService91) Check(_ context.Context, _ *service.CheckRequest) (*service.CheckResponse, error) {
	return nil, errIAMService91
}
func (m *errorCombinedIAMService91) CheckBatch(_ context.Context, _ *service.BatchCheckRequest) (*service.BatchCheckResponse, error) {
	return nil, errIAMService91
}
func (m *errorCombinedIAMService91) Subscribe(_ context.Context, _ *service.SubscribePolicyUpdatesRequest) (<-chan *service.PolicyUpdate, error) {
	return nil, errIAMService91
}
func (m *errorCombinedIAMService91) CreateAdminAssignment(_ context.Context, _ iam.CreateAdminAssignmentOptions) (*iam.AdminAssignment, error) {
	return nil, errIAMService91
}
func (m *errorCombinedIAMService91) GetAdminAssignment(_ context.Context, _ string) (*iam.AdminAssignment, error) {
	return nil, errIAMService91
}
func (m *errorCombinedIAMService91) UpdateAdminAssignment(_ context.Context, _ string, _ iam.UpdateAdminAssignmentOptions) (*iam.AdminAssignment, error) {
	return nil, errIAMService91
}
func (m *errorCombinedIAMService91) DeleteAdminAssignment(_ context.Context, _ string) error {
	return errIAMService91
}
func (m *errorCombinedIAMService91) ListAdminAssignments(_ context.Context, _ iam.ListAdminAssignmentsOptions) ([]*iam.AdminAssignment, error) {
	return nil, errIAMService91
}
func (m *errorCombinedIAMService91) CheckAdminPermission(_ context.Context, _ *iam.AdminCheckRequest) (*iam.AdminCheckResponse, error) {
	return nil, errIAMService91
}
func (m *errorCombinedIAMService91) StartImpersonation(_ context.Context, _ *iam.StartImpersonationOptions) (*iam.ImpersonationSession, error) {
	return nil, errIAMService91
}
func (m *errorCombinedIAMService91) EndImpersonation(_ context.Context, _ string) error {
	return errIAMService91
}
func (m *errorCombinedIAMService91) GetActiveImpersonations(_ context.Context, _ string) ([]*iam.ImpersonationSession, error) {
	return nil, errIAMService91
}
func (m *errorCombinedIAMService91) GrantConsent(_ context.Context, _, _ string, _ iam.ConsentPurpose, _ string, _ *time.Time, _, _ string) (*iam.Consent, error) {
	return nil, errIAMService91
}
func (m *errorCombinedIAMService91) WithdrawConsent(_ context.Context, _, _ string, _ iam.ConsentPurpose, _, _ string) error {
	return errIAMService91
}
func (m *errorCombinedIAMService91) GetConsent(_ context.Context, _, _ string, _ iam.ConsentPurpose) (*iam.Consent, error) {
	return nil, errIAMService91
}
func (m *errorCombinedIAMService91) ListUserConsents(_ context.Context, _, _ string) ([]*iam.Consent, error) {
	return nil, errIAMService91
}
func (m *errorCombinedIAMService91) RequestExport(_ context.Context, _, _, _ string, _ iam.DataExportFormat, _, _ []string) (*iam.DataExportRequest, error) {
	return nil, errIAMService91
}
func (m *errorCombinedIAMService91) GetExportStatus(_ context.Context, _ string) (*iam.DataExportRequest, error) {
	return nil, errIAMService91
}
func (m *errorCombinedIAMService91) GenerateExport(_ context.Context, _ string) (*iam.UserDataExport, error) {
	return nil, errIAMService91
}
func (m *errorCombinedIAMService91) RequestDeletion(_ context.Context, _, _, _, _ string, _, _ []string, _ int) (*iam.DataDeletionRequest, error) {
	return nil, errIAMService91
}
func (m *errorCombinedIAMService91) CancelDeletion(_ context.Context, _, _ string) error {
	return errIAMService91
}
func (m *errorCombinedIAMService91) ProcessDeletion(_ context.Context, _ string) (*iam.DeletionSummary, error) {
	return nil, errIAMService91
}
func (m *errorCombinedIAMService91) GetDeletionStatus(_ context.Context, _ string) (*iam.DataDeletionRequest, error) {
	return nil, errIAMService91
}

// =============================================================================
// Helpers
// =============================================================================

func createIAMHandler91() *Handler {
	return NewHandler(&mockService{}, nil, nil, nil, createTestLogger(), WithIAMService(&mockIAMService91{}), WithMFAService(&mockMFAService91{}))
}
func createErrorIAMHandler91() *Handler {
	return NewHandler(&mockService{}, nil, nil, nil, createTestLogger(), WithIAMService(&errorIAMService91{}), WithMFAService(&errorMFAService91{}))
}
func createCombinedHandler91() *Handler {
	return NewHandler(&mockCombinedIAMService91{}, nil, nil, nil, createTestLogger(), WithIAMService(&mockIAMService91{}), WithMFAService(&mockMFAService91{}))
}
func createErrorCombinedHandler91() *Handler {
	return NewHandler(&errorCombinedIAMService91{}, nil, nil, nil, createTestLogger(), WithIAMService(&errorIAMService91{}), WithMFAService(&errorMFAService91{}))
}

// =============================================================================
// IAM CRUD Handlers - Bad Payload + Success + Service Error
// =============================================================================

type iamHandlerTestCase struct {
	name       string
	handler    func(context.Context, *Request) *Response
	errHandler func(context.Context, *Request) *Response
}

func runIAMHandlerTests(t *testing.T, tests []iamHandlerTestCase, validPayload []byte) {
	t.Helper()
	ctx := context.Background()
	badPayload := []byte("{invalid")
	for _, tt := range tests {
		t.Run(tt.name+"/BadPayload", func(t *testing.T) {
			if resp := tt.handler(ctx, &Request{Payload: badPayload}); resp.Status == StatusOK {
				t.Fatalf("%s: expected error for bad payload", tt.name)
			}
		})
		t.Run(tt.name+"/Success", func(t *testing.T) {
			if resp := tt.handler(ctx, &Request{Payload: validPayload}); resp.Status != StatusOK {
				t.Fatalf("%s: expected StatusOK, got %d: %s", tt.name, resp.Status, string(resp.Payload))
			}
		})
		t.Run(tt.name+"/ServiceError", func(t *testing.T) {
			if resp := tt.errHandler(ctx, &Request{Payload: validPayload}); resp.Status == StatusOK {
				t.Fatalf("%s: expected error when service fails", tt.name)
			}
		})
	}
}

func TestIAMHandler91_TenantHandlers(t *testing.T) {
	t.Parallel()
	h, hErr := createIAMHandler91(), createErrorIAMHandler91()
	runIAMHandlerTests(t, []iamHandlerTestCase{
		{"CreateTenant", h.handleIAMCreateTenant, hErr.handleIAMCreateTenant},
		{"GetTenant", h.handleIAMGetTenant, hErr.handleIAMGetTenant},
		{"UpdateTenant", h.handleIAMUpdateTenant, hErr.handleIAMUpdateTenant},
		{"DeleteTenant", h.handleIAMDeleteTenant, hErr.handleIAMDeleteTenant},
		{"ListTenants", h.handleIAMListTenants, hErr.handleIAMListTenants},
	}, []byte("{}"))
}

func TestIAMHandler91_UserHandlers(t *testing.T) {
	t.Parallel()
	h, hErr := createIAMHandler91(), createErrorIAMHandler91()
	runIAMHandlerTests(t, []iamHandlerTestCase{
		{"CreateUser", h.handleIAMCreateUser, hErr.handleIAMCreateUser},
		{"GetUser", h.handleIAMGetUser, hErr.handleIAMGetUser},
		{"UpdateUser", h.handleIAMUpdateUser, hErr.handleIAMUpdateUser},
		{"DeleteUser", h.handleIAMDeleteUser, hErr.handleIAMDeleteUser},
		{"ListUsers", h.handleIAMListUsers, hErr.handleIAMListUsers},
		{"AssignUserRole", h.handleIAMAssignUserRole, hErr.handleIAMAssignUserRole},
		{"RevokeUserRole", h.handleIAMRevokeUserRole, hErr.handleIAMRevokeUserRole},
	}, []byte("{}"))
}

func TestIAMHandler91_GroupHandlers(t *testing.T) {
	t.Parallel()
	h, hErr := createIAMHandler91(), createErrorIAMHandler91()
	runIAMHandlerTests(t, []iamHandlerTestCase{
		{"CreateGroup", h.handleIAMCreateGroup, hErr.handleIAMCreateGroup},
		{"GetGroup", h.handleIAMGetGroup, hErr.handleIAMGetGroup},
		{"UpdateGroup", h.handleIAMUpdateGroup, hErr.handleIAMUpdateGroup},
		{"DeleteGroup", h.handleIAMDeleteGroup, hErr.handleIAMDeleteGroup},
		{"ListGroups", h.handleIAMListGroups, hErr.handleIAMListGroups},
		{"AddGroupMember", h.handleIAMAddGroupMember, hErr.handleIAMAddGroupMember},
		{"RemoveGroupMember", h.handleIAMRemoveGroupMember, hErr.handleIAMRemoveGroupMember},
		{"ListGroupMembers", h.handleIAMListGroupMembers, hErr.handleIAMListGroupMembers},
	}, []byte("{}"))
}

func TestIAMHandler91_RoleHandlers(t *testing.T) {
	t.Parallel()
	h, hErr := createIAMHandler91(), createErrorIAMHandler91()
	runIAMHandlerTests(t, []iamHandlerTestCase{
		{"CreateRole", h.handleIAMCreateRole, hErr.handleIAMCreateRole},
		{"GetRole", h.handleIAMGetRole, hErr.handleIAMGetRole},
		{"UpdateRole", h.handleIAMUpdateRole, hErr.handleIAMUpdateRole},
		{"DeleteRole", h.handleIAMDeleteRole, hErr.handleIAMDeleteRole},
		{"ListRoles", h.handleIAMListRoles, hErr.handleIAMListRoles},
	}, []byte("{}"))
}

func TestIAMHandler91_PolicyHandlers(t *testing.T) {
	t.Parallel()
	h, hErr := createIAMHandler91(), createErrorIAMHandler91()
	runIAMHandlerTests(t, []iamHandlerTestCase{
		{"CreatePolicy", h.handleIAMCreatePolicy, hErr.handleIAMCreatePolicy},
		{"GetPolicy", h.handleIAMGetPolicy, hErr.handleIAMGetPolicy},
		{"UpdatePolicy", h.handleIAMUpdatePolicy, hErr.handleIAMUpdatePolicy},
		{"DeletePolicy", h.handleIAMDeletePolicy, hErr.handleIAMDeletePolicy},
		{"ListPolicies", h.handleIAMListPolicies, hErr.handleIAMListPolicies},
		{"ValidatePolicy", h.handleIAMValidatePolicy, hErr.handleIAMValidatePolicy},
		{"TestPolicy", h.handleIAMTestPolicy, hErr.handleIAMTestPolicy},
	}, []byte("{}"))
}

func TestIAMHandler91_AppHandlers(t *testing.T) {
	t.Parallel()
	h, hErr := createIAMHandler91(), createErrorIAMHandler91()
	runIAMHandlerTests(t, []iamHandlerTestCase{
		{"RegisterApp", h.handleIAMRegisterApp, hErr.handleIAMRegisterApp},
		{"GetApp", h.handleIAMGetApp, hErr.handleIAMGetApp},
		{"UpdateApp", h.handleIAMUpdateApp, hErr.handleIAMUpdateApp},
		{"DeleteApp", h.handleIAMDeleteApp, hErr.handleIAMDeleteApp},
		{"ListApps", h.handleIAMListApps, hErr.handleIAMListApps},
		{"RotateAppSecret", h.handleIAMRotateAppSecret, hErr.handleIAMRotateAppSecret},
	}, []byte("{}"))
}

// =============================================================================
// MFA Handlers - Bad Payload + Success + Service Error
// handleMFAVerifyBackupCode swallows errors (returns StatusOK with Success=false)
// =============================================================================

func TestIAMHandler91_MFAHandlers(t *testing.T) {
	t.Parallel()
	h, hErr := createIAMHandler91(), createErrorIAMHandler91()
	ctx := context.Background()
	badPayload := []byte("{invalid")

	type mfaTest struct {
		name          string
		handler       func(context.Context, *Request) *Response
		errHandler    func(context.Context, *Request) *Response
		payload       interface{}
		swallowsError bool // handler returns StatusOK even on service error
	}

	tests := []mfaTest{
		{"BeginEnroll", h.handleMFABeginEnroll, hErr.handleMFABeginEnroll, &MFABeginEnrollRequest{UserID: "u1", TenantID: "t1", Method: "totp"}, false},
		{"CompleteEnroll", h.handleMFACompleteEnroll, hErr.handleMFACompleteEnroll, &MFACompleteEnrollRequest{EnrollmentID: "e1", Code: "123456"}, false},
		{"CancelEnroll", h.handleMFACancelEnroll, hErr.handleMFACancelEnroll, &MFACancelEnrollRequest{EnrollmentID: "e1"}, false},
		{"GetStatus", h.handleMFAGetStatus, hErr.handleMFAGetStatus, &MFAGetStatusRequest{UserID: "u1", TenantID: "t1"}, false},
		{"Disable", h.handleMFADisable, hErr.handleMFADisable, &MFADisableRequest{UserID: "u1", TenantID: "t1"}, false},
		{"BeginChallenge", h.handleMFABeginChallenge, hErr.handleMFABeginChallenge, &MFABeginChallengeRequest{UserID: "u1", TenantID: "t1", SessionToken: "s1"}, false},
		{"VerifyChallenge", h.handleMFAVerifyChallenge, hErr.handleMFAVerifyChallenge, &MFAVerifyChallengeRequest{ChallengeID: "c1", Code: "123456"}, false},
		{"GenerateBackupCodes", h.handleMFAGenerateBackupCodes, hErr.handleMFAGenerateBackupCodes, &MFAGenerateBackupCodesRequest{UserID: "u1", TenantID: "t1"}, false},
		{"VerifyBackupCode", h.handleMFAVerifyBackupCode, hErr.handleMFAVerifyBackupCode, &MFAVerifyBackupCodeRequest{UserID: "u1", TenantID: "t1", Code: "backup1"}, true},
		{"GetBackupCodeStatus", h.handleMFAGetBackupCodeStatus, hErr.handleMFAGetBackupCodeStatus, &MFAGetBackupCodeStatusRequest{UserID: "u1", TenantID: "t1"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/BadPayload", func(t *testing.T) {
			if resp := tt.handler(ctx, &Request{Payload: badPayload}); resp.Status == StatusOK {
				t.Fatalf("%s: expected error for bad payload", tt.name)
			}
		})
		t.Run(tt.name+"/Success", func(t *testing.T) {
			payload, _ := EncodePayload(tt.payload)
			if resp := tt.handler(ctx, &Request{Payload: payload}); resp.Status != StatusOK {
				t.Fatalf("%s: expected StatusOK, got %d: %s", tt.name, resp.Status, string(resp.Payload))
			}
		})
		t.Run(tt.name+"/ServiceError", func(t *testing.T) {
			payload, _ := EncodePayload(tt.payload)
			resp := tt.errHandler(ctx, &Request{Payload: payload})
			if tt.swallowsError {
				// Handler converts error to Success=false in response body
				if resp.Status != StatusOK {
					t.Fatalf("%s: expected StatusOK with success=false, got %d", tt.name, resp.Status)
				}
			} else {
				if resp.Status == StatusOK {
					t.Fatalf("%s: expected error when service fails", tt.name)
				}
			}
		})
	}
}

// =============================================================================
// PEP Handlers
// =============================================================================

func TestIAMHandler91_PEPHandlers(t *testing.T) {
	t.Parallel()
	h, hErr := createCombinedHandler91(), createErrorCombinedHandler91()
	ctx := context.Background()
	badPayload := []byte("{invalid")

	// PEPCheckAuthorization (handler_iam.go)
	t.Run("PEPCheckAuthorization/BadPayload", func(t *testing.T) {
		if resp := h.handlePEPCheckAuthorization(ctx, &Request{Payload: badPayload}); resp.Status == StatusOK {
			t.Fatal("expected error")
		}
	})
	t.Run("PEPCheckAuthorization/Success", func(t *testing.T) {
		p, _ := EncodePayload(&service.CheckRequest{})
		if resp := h.handlePEPCheckAuthorization(ctx, &Request{Payload: p}); resp.Status != StatusOK {
			t.Fatalf("expected StatusOK, got %d", resp.Status)
		}
	})
	t.Run("PEPCheckAuthorization/ServiceError", func(t *testing.T) {
		p, _ := EncodePayload(&service.CheckRequest{})
		if resp := hErr.handlePEPCheckAuthorization(ctx, &Request{Payload: p}); resp.Status == StatusOK {
			t.Fatal("expected error")
		}
	})

	// PEPCheckAuthorizationBatch (handler_iam.go)
	t.Run("PEPCheckAuthorizationBatch/BadPayload", func(t *testing.T) {
		if resp := h.handlePEPCheckAuthorizationBatch(ctx, &Request{Payload: badPayload}); resp.Status == StatusOK {
			t.Fatal("expected error")
		}
	})
	t.Run("PEPCheckAuthorizationBatch/Success", func(t *testing.T) {
		p, _ := EncodePayload(&service.BatchCheckRequest{})
		if resp := h.handlePEPCheckAuthorizationBatch(ctx, &Request{Payload: p}); resp.Status != StatusOK {
			t.Fatalf("expected StatusOK, got %d", resp.Status)
		}
	})
	t.Run("PEPCheckAuthorizationBatch/ServiceError", func(t *testing.T) {
		p, _ := EncodePayload(&service.BatchCheckRequest{})
		if resp := hErr.handlePEPCheckAuthorizationBatch(ctx, &Request{Payload: p}); resp.Status == StatusOK {
			t.Fatal("expected error")
		}
	})

	// PEPCheck (handler_pep.go - with validation)
	t.Run("PEPCheck/BadPayload", func(t *testing.T) {
		if resp := h.handlePEPCheck(ctx, &Request{Payload: badPayload}); resp.Status == StatusOK {
			t.Fatal("expected error")
		}
	})
	t.Run("PEPCheck/ValidationError", func(t *testing.T) {
		p, _ := EncodePayload(&service.CheckRequest{})
		if resp := h.handlePEPCheck(ctx, &Request{Payload: p}); resp.Status == StatusOK {
			t.Fatal("expected validation error")
		}
	})
	t.Run("PEPCheck/Success", func(t *testing.T) {
		p, _ := EncodePayload(&service.CheckRequest{Subject: iam.Subject{ID: "u1"}, Action: "read", Resource: "keys"})
		if resp := h.handlePEPCheck(ctx, &Request{Payload: p}); resp.Status != StatusOK {
			t.Fatalf("expected StatusOK, got %d", resp.Status)
		}
	})
	t.Run("PEPCheck/ServiceError", func(t *testing.T) {
		p, _ := EncodePayload(&service.CheckRequest{Subject: iam.Subject{ID: "u1"}, Action: "read", Resource: "keys"})
		if resp := hErr.handlePEPCheck(ctx, &Request{Payload: p}); resp.Status == StatusOK {
			t.Fatal("expected error")
		}
	})

	// PEPCheckBatch (handler_pep.go - with validation)
	t.Run("PEPCheckBatch/BadPayload", func(t *testing.T) {
		if resp := h.handlePEPCheckBatch(ctx, &Request{Payload: badPayload}); resp.Status == StatusOK {
			t.Fatal("expected error")
		}
	})
	t.Run("PEPCheckBatch/ValidationError_Empty", func(t *testing.T) {
		p, _ := EncodePayload(&service.BatchCheckRequest{})
		if resp := h.handlePEPCheckBatch(ctx, &Request{Payload: p}); resp.Status == StatusOK {
			t.Fatal("expected validation error")
		}
	})
	t.Run("PEPCheckBatch/ValidationError_BadCheck", func(t *testing.T) {
		p, _ := EncodePayload(&service.BatchCheckRequest{Checks: []*service.CheckRequest{{Subject: iam.Subject{ID: ""}, Action: "read", Resource: "keys"}}})
		if resp := h.handlePEPCheckBatch(ctx, &Request{Payload: p}); resp.Status == StatusOK {
			t.Fatal("expected validation error")
		}
	})
	t.Run("PEPCheckBatch/Success", func(t *testing.T) {
		p, _ := EncodePayload(&service.BatchCheckRequest{Checks: []*service.CheckRequest{{Subject: iam.Subject{ID: "u1"}, Action: "read", Resource: "keys"}}})
		if resp := h.handlePEPCheckBatch(ctx, &Request{Payload: p}); resp.Status != StatusOK {
			t.Fatalf("expected StatusOK, got %d", resp.Status)
		}
	})
	t.Run("PEPCheckBatch/ServiceError", func(t *testing.T) {
		p, _ := EncodePayload(&service.BatchCheckRequest{Checks: []*service.CheckRequest{{Subject: iam.Subject{ID: "u1"}, Action: "read", Resource: "keys"}}})
		if resp := hErr.handlePEPCheckBatch(ctx, &Request{Payload: p}); resp.Status == StatusOK {
			t.Fatal("expected error")
		}
	})

	// PEPSubscribe (handler_pep.go)
	t.Run("PEPSubscribe/BadPayload", func(t *testing.T) {
		if resp := h.handlePEPSubscribe(ctx, &Request{Payload: badPayload}); resp.Status == StatusOK {
			t.Fatal("expected error")
		}
	})
	t.Run("PEPSubscribe/ValidationError", func(t *testing.T) {
		p, _ := EncodePayload(&service.SubscribePolicyUpdatesRequest{})
		if resp := h.handlePEPSubscribe(ctx, &Request{Payload: p}); resp.Status == StatusOK {
			t.Fatal("expected validation error")
		}
	})
	t.Run("PEPSubscribe/Success", func(t *testing.T) {
		p, _ := EncodePayload(&service.SubscribePolicyUpdatesRequest{TenantID: "t1"})
		if resp := h.handlePEPSubscribe(ctx, &Request{Payload: p}); resp.Status != StatusOK {
			t.Fatalf("expected StatusOK, got %d", resp.Status)
		}
	})
	t.Run("PEPSubscribe/ServiceError", func(t *testing.T) {
		p, _ := EncodePayload(&service.SubscribePolicyUpdatesRequest{TenantID: "t1"})
		if resp := hErr.handlePEPSubscribe(ctx, &Request{Payload: p}); resp.Status == StatusOK {
			t.Fatal("expected error")
		}
	})
}

// =============================================================================
// Admin Handlers
// =============================================================================

func TestIAMHandler91_AdminHandlers(t *testing.T) {
	t.Parallel()
	h, hErr := createCombinedHandler91(), createErrorCombinedHandler91()
	ctx := context.Background()
	badPayload := []byte("{invalid")
	type tc struct {
		name       string
		handler    func(context.Context, *Request) *Response
		errHandler func(context.Context, *Request) *Response
		payload    interface{}
	}
	tests := []tc{
		{"CreateAdminAssignment", h.handleIAMCreateAdminAssignment, hErr.handleIAMCreateAdminAssignment, &CreateAdminAssignmentRequest{UserID: "u1", GrantedBy: "admin"}},
		{"GetAdminAssignment", h.handleIAMGetAdminAssignment, hErr.handleIAMGetAdminAssignment, &GetAdminAssignmentRequest{ID: "a1"}},
		{"UpdateAdminAssignment", h.handleIAMUpdateAdminAssignment, hErr.handleIAMUpdateAdminAssignment, &UpdateAdminAssignmentRequest{ID: "a1"}},
		{"DeleteAdminAssignment", h.handleIAMDeleteAdminAssignment, hErr.handleIAMDeleteAdminAssignment, &DeleteAdminAssignmentRequest{ID: "a1"}},
		{"ListAdminAssignments", h.handleIAMListAdminAssignments, hErr.handleIAMListAdminAssignments, &ListAdminAssignmentsRequest{}},
		{"CheckAdminPermission", h.handleIAMCheckAdminPermission, hErr.handleIAMCheckAdminPermission, &CheckAdminPermissionRequest{UserID: "u1", Permission: "manage_users"}},
		{"StartImpersonation", h.handleIAMStartImpersonation, hErr.handleIAMStartImpersonation, &StartImpersonationRequest{AdminUserID: "admin", TargetUserID: "user1", TenantID: "t1", Reason: "support"}},
		{"EndImpersonation", h.handleIAMEndImpersonation, hErr.handleIAMEndImpersonation, &EndImpersonationRequest{SessionID: "s1"}},
		{"GetActiveImpersonations", h.handleIAMGetActiveImpersonations, hErr.handleIAMGetActiveImpersonations, &GetActiveImpersonationsRequest{AdminUserID: "admin"}},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/BadPayload", func(t *testing.T) {
			if resp := tt.handler(ctx, &Request{Payload: badPayload}); resp.Status == StatusOK {
				t.Fatalf("%s: expected error", tt.name)
			}
		})
		t.Run(tt.name+"/Success", func(t *testing.T) {
			p, _ := EncodePayload(tt.payload)
			if resp := tt.handler(ctx, &Request{Payload: p}); resp.Status != StatusOK {
				t.Fatalf("%s: expected StatusOK, got %d", tt.name, resp.Status)
			}
		})
		t.Run(tt.name+"/ServiceError", func(t *testing.T) {
			p, _ := EncodePayload(tt.payload)
			if resp := tt.errHandler(ctx, &Request{Payload: p}); resp.Status == StatusOK {
				t.Fatalf("%s: expected error", tt.name)
			}
		})
	}
}

// =============================================================================
// GDPR Handlers
// =============================================================================

func TestIAMHandler91_GDPRHandlers(t *testing.T) {
	t.Parallel()
	h, hErr := createCombinedHandler91(), createErrorCombinedHandler91()
	ctx := context.Background()
	badPayload := []byte("{invalid")
	type tc struct {
		name       string
		handler    func(context.Context, *Request) *Response
		errHandler func(context.Context, *Request) *Response
		payload    interface{}
	}
	tests := []tc{
		{"GrantConsent", h.handleIAMGrantConsent, hErr.handleIAMGrantConsent, &GrantConsentRequest{TenantID: "t1", UserID: "u1", Purpose: "analytics", Version: "v1"}},
		{"WithdrawConsent", h.handleIAMWithdrawConsent, hErr.handleIAMWithdrawConsent, &WithdrawConsentRequest{TenantID: "t1", UserID: "u1", Purpose: "analytics"}},
		{"GetConsent", h.handleIAMGetConsent, hErr.handleIAMGetConsent, &GetConsentRequest{TenantID: "t1", UserID: "u1", Purpose: "analytics"}},
		{"ListUserConsents", h.handleIAMListUserConsents, hErr.handleIAMListUserConsents, &ListUserConsentsRequest{TenantID: "t1", UserID: "u1"}},
		{"RequestExport", h.handleIAMRequestExport, hErr.handleIAMRequestExport, &RequestExportRequest{TenantID: "t1", UserID: "u1", RequestedBy: "admin", Format: "json"}},
		{"GetExportStatus", h.handleIAMGetExportStatus, hErr.handleIAMGetExportStatus, &GetExportStatusRequest{ExportID: "e1"}},
		{"GenerateExport", h.handleIAMGenerateExport, hErr.handleIAMGenerateExport, &GenerateExportRequest{ExportID: "e1"}},
		{"RequestDeletion", h.handleIAMRequestDeletion, hErr.handleIAMRequestDeletion, &RequestDeletionRequest{TenantID: "t1", UserID: "u1", RequestedBy: "admin"}},
		{"CancelDeletion", h.handleIAMCancelDeletion, hErr.handleIAMCancelDeletion, &CancelDeletionRequest{DeletionID: "d1", CancelledBy: "admin"}},
		{"ProcessDeletion", h.handleIAMProcessDeletion, hErr.handleIAMProcessDeletion, &ProcessDeletionRequest{DeletionID: "d1"}},
		{"GetDeletionStatus", h.handleIAMGetDeletionStatus, hErr.handleIAMGetDeletionStatus, &GetDeletionStatusRequest{DeletionID: "d1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/BadPayload", func(t *testing.T) {
			if resp := tt.handler(ctx, &Request{Payload: badPayload}); resp.Status == StatusOK {
				t.Fatalf("%s: expected error", tt.name)
			}
		})
		t.Run(tt.name+"/Success", func(t *testing.T) {
			p, _ := EncodePayload(tt.payload)
			if resp := tt.handler(ctx, &Request{Payload: p}); resp.Status != StatusOK {
				t.Fatalf("%s: expected StatusOK, got %d", tt.name, resp.Status)
			}
		})
		t.Run(tt.name+"/ServiceError", func(t *testing.T) {
			p, _ := EncodePayload(tt.payload)
			if resp := tt.errHandler(ctx, &Request{Payload: p}); resp.Status == StatusOK {
				t.Fatalf("%s: expected error", tt.name)
			}
		})
	}
}

// =============================================================================
// IAM Service Management (handler_iam_service.go)
// =============================================================================

func TestIAMHandler91_SetIAMServiceWithValue(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	h.setIAMService(&mockIAMService91{})
	if h.getIAMService() == nil {
		t.Fatal("expected non-nil IAM service")
	}
	h.clearIAMService()
	if h.getIAMService() != nil {
		t.Fatal("expected nil after clear")
	}
}
func TestIAMHandler91_SetIAMServiceNil(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	h.setIAMService(nil)
	if h.getIAMService() != nil {
		t.Fatal("expected nil")
	}
}
func TestIAMHandler91_SetMFAServiceWithValue(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	h.setMFAService(&mockMFAService91{})
	if h.getMFAService() == nil {
		t.Fatal("expected non-nil MFA service")
	}
	h.clearMFAService()
	if h.getMFAService() != nil {
		t.Fatal("expected nil after clear")
	}
}
func TestIAMHandler91_SetMFAServiceNil(t *testing.T) {
	t.Parallel()
	h := createTestHandler()
	h.setMFAService(nil)
	if h.getMFAService() != nil {
		t.Fatal("expected nil")
	}
}

// =============================================================================
// server_iam.go
// =============================================================================

type mockServiceWithIAM struct {
	mockService
	iamSvc service.IAMService
}

func (m *mockServiceWithIAM) IAM() service.IAMService { return m.iamSvc }

type mockServiceWithMFA struct {
	mockService
	mfaSvc service.MFAManager
	mfaErr error
}

func (m *mockServiceWithMFA) GetMFAManager(_ context.Context) (service.MFAManager, error) {
	return m.mfaSvc, m.mfaErr
}

func TestIAMHandler91_GetIAMServiceOption(t *testing.T) {
	t.Parallel()
	t.Run("NilService", func(t *testing.T) {
		h := createTestHandler()
		getIAMServiceOption(&mockService{})(h)
		if h.getIAMService() != nil {
			t.Fatal("expected nil")
		}
	})
	t.Run("WithIAMProvider", func(t *testing.T) {
		h := createTestHandler()
		getIAMServiceOption(&mockServiceWithIAM{iamSvc: &mockIAMService91{}})(h)
		if h.getIAMService() == nil {
			t.Fatal("expected non-nil")
		}
	})
	t.Run("WithIAMProviderNilReturn", func(t *testing.T) {
		h := createTestHandler()
		getIAMServiceOption(&mockServiceWithIAM{iamSvc: nil})(h)
		if h.getIAMService() != nil {
			t.Fatal("expected nil")
		}
	})
}

func TestIAMHandler91_GetMFAServiceOption(t *testing.T) {
	t.Parallel()
	t.Run("NonProvider", func(t *testing.T) {
		h := createTestHandler()
		getMFAServiceOption(&mockService{})(h)
		if h.getMFAService() != nil {
			t.Fatal("expected nil")
		}
	})
	t.Run("WithMFAProvider", func(t *testing.T) {
		h := createTestHandler()
		getMFAServiceOption(&mockServiceWithMFA{mfaSvc: &mockMFAService91{}})(h)
		if h.getMFAService() == nil {
			t.Fatal("expected non-nil")
		}
	})
	t.Run("WithMFAProviderError", func(t *testing.T) {
		h := createTestHandler()
		getMFAServiceOption(&mockServiceWithMFA{mfaErr: errors.New("no mfa")})(h)
		if h.getMFAService() != nil {
			t.Fatal("expected nil")
		}
	})
	t.Run("WithMFAProviderNilReturn", func(t *testing.T) {
		h := createTestHandler()
		getMFAServiceOption(&mockServiceWithMFA{mfaSvc: nil})(h)
		if h.getMFAService() != nil {
			t.Fatal("expected nil")
		}
	})
}

// =============================================================================
// Validation Functions
// =============================================================================

func TestIAMHandler91_ValidateCheckRequest(t *testing.T) {
	t.Parallel()
	t.Run("NilRequest", func(t *testing.T) {
		if err := validateCheckRequest(nil); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("EmptyAction", func(t *testing.T) {
		if err := validateCheckRequest(&service.CheckRequest{Subject: iam.Subject{ID: "u1"}, Resource: "keys"}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("EmptyResource", func(t *testing.T) {
		if err := validateCheckRequest(&service.CheckRequest{Subject: iam.Subject{ID: "u1"}, Action: "read"}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("Valid", func(t *testing.T) {
		if err := validateCheckRequest(&service.CheckRequest{Subject: iam.Subject{ID: "u1"}, Action: "read", Resource: "keys"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestIAMHandler91_ValidateBatchCheckRequest(t *testing.T) {
	t.Parallel()
	t.Run("NilRequest", func(t *testing.T) {
		if err := validateBatchCheckRequest(nil); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("EmptyChecks", func(t *testing.T) {
		if err := validateBatchCheckRequest(&service.BatchCheckRequest{}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("InvalidCheckInBatch", func(t *testing.T) {
		if err := validateBatchCheckRequest(&service.BatchCheckRequest{Checks: []*service.CheckRequest{{Subject: iam.Subject{ID: ""}, Action: "read", Resource: "keys"}}}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("Valid", func(t *testing.T) {
		if err := validateBatchCheckRequest(&service.BatchCheckRequest{Checks: []*service.CheckRequest{{Subject: iam.Subject{ID: "u1"}, Action: "read", Resource: "keys"}}}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestIAMHandler91_ValidateSubscribeRequest(t *testing.T) {
	t.Parallel()
	t.Run("NilRequest", func(t *testing.T) {
		if err := validateSubscribeRequest(nil); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("EmptyTenantID", func(t *testing.T) {
		if err := validateSubscribeRequest(&service.SubscribePolicyUpdatesRequest{}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("Valid", func(t *testing.T) {
		if err := validateSubscribeRequest(&service.SubscribePolicyUpdatesRequest{TenantID: "t1"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// =============================================================================
// Option Tests
// =============================================================================

func TestIAMHandler91_WithIAMServiceOption(t *testing.T) {
	t.Parallel()
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(), WithIAMService(&mockIAMService91{}))
	if h.getIAMService() == nil {
		t.Fatal("expected non-nil")
	}
}

func TestIAMHandler91_WithMFAServiceOption(t *testing.T) {
	t.Parallel()
	h := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(), WithMFAService(&mockMFAService91{}))
	if h.getMFAService() == nil {
		t.Fatal("expected non-nil")
	}
}

// --- Content moved from handler_iam_coverage_test.go ---

// =============================================================================
// IAM Handler Coverage Tests - Nil Service
// These tests verify proper error handling when IAM service is not available.
// The tests use empty/minimal payloads since we're testing the nil service check.
// =============================================================================

func TestIAMCreateTenant_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMCreateTenant(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMCreateTenant_BadPayload(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMCreateTenant(ctx, &Request{Payload: []byte("{invalid")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestIAMGetTenant_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGetTenant(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMUpdateTenant_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMUpdateTenant(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMDeleteTenant_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMDeleteTenant(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMListTenants_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMListTenants(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

// =============================================================================
// User Operations - Nil Service Tests
// =============================================================================

func TestIAMCreateUser_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMCreateUser(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMGetUser_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGetUser(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMUpdateUser_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMUpdateUser(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMDeleteUser_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMDeleteUser(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMListUsers_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMListUsers(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMAssignUserRole_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMAssignUserRole(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMRevokeUserRole_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMRevokeUserRole(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

// =============================================================================
// Group Operations - Nil Service Tests
// =============================================================================

func TestIAMCreateGroup_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMCreateGroup(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMGetGroup_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGetGroup(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMUpdateGroup_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMUpdateGroup(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMDeleteGroup_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMDeleteGroup(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMListGroups_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMListGroups(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMAddGroupMember_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMAddGroupMember(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMRemoveGroupMember_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMRemoveGroupMember(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMListGroupMembers_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMListGroupMembers(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

// =============================================================================
// Role Operations - Nil Service Tests
// =============================================================================

func TestIAMCreateRole_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMCreateRole(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMGetRole_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGetRole(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMUpdateRole_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMUpdateRole(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMDeleteRole_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMDeleteRole(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMListRoles_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMListRoles(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

// =============================================================================
// Policy Operations - Nil Service Tests
// =============================================================================

func TestIAMCreatePolicy_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMCreatePolicy(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMGetPolicy_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGetPolicy(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMUpdatePolicy_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMUpdatePolicy(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMDeletePolicy_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMDeletePolicy(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMListPolicies_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMListPolicies(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMValidatePolicy_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMValidatePolicy(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMTestPolicy_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMTestPolicy(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

// =============================================================================
// Application Operations - Nil Service Tests
// =============================================================================

func TestIAMRegisterApp_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMRegisterApp(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMGetApp_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMGetApp(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMUpdateApp_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMUpdateApp(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMDeleteApp_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMDeleteApp(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMListApps_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMListApps(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

func TestIAMRotateAppSecret_NilService(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	resp := h.handleIAMRotateAppSecret(ctx, &Request{Payload: []byte("{}")})

	if resp.Status == StatusOK {
		t.Fatal("Expected error for nil IAM service")
	}
}

// =============================================================================
// IAM Service Management Tests
// =============================================================================

func TestIAMServiceManagement(t *testing.T) {
	h := createTestHandler()

	// Test that getIAMService returns nil initially
	if h.getIAMService() != nil {
		t.Fatal("Expected nil IAM service initially")
	}

	// Test getMFAService returns nil initially
	if h.getMFAService() != nil {
		t.Fatal("Expected nil MFA service initially")
	}
}

// =============================================================================
// iamServiceUnavailableError Test
// =============================================================================

func TestIAMServiceUnavailableError(t *testing.T) {
	h := createTestHandler()

	resp := h.iamServiceUnavailableError("test.operation")

	if resp.Status == StatusOK {
		t.Fatal("Expected non-OK status for unavailable error")
	}

	if len(resp.Payload) == 0 {
		t.Fatal("Expected non-empty payload")
	}
}
