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
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// IAM service prefixes for routing.
const (
	iamUserServicePrefix       = "quicbaas.IAM.User"
	iamTenantServicePrefix     = "quicbaas.IAM.Tenant"
	iamRoleServicePrefix       = "quicbaas.IAM.Role"
	iamPolicyServicePrefix     = "quicbaas.IAM.Policy"
	iamAppServicePrefix        = "quicbaas.IAM.App"
	iamSessionServicePrefix    = "quicbaas.IAM.Session"
	iamAuditServicePrefix      = "quicbaas.IAM.Audit"
	iamAdminServicePrefix      = "quicbaas.IAM.Admin"
	iamGDPRServicePrefix       = "quicbaas.IAM.GDPR"
	iamPEPServicePrefix        = "quicbaas.IAM.PEP"
	iamMFAServicePrefix        = "quicbaas.IAM.MFA"
	iamFederationServicePrefix = "quicbaas.IAM.Federation"
	iamIdentityServicePrefix   = "quicbaas.IAM.Identity"
	iamWebAuthnServicePrefix   = "quicbaas.IAM.WebAuthn"
	iamInviteServicePrefix     = "quicbaas.IAM.Invite"

	// Proto service name.
	iamProtoServiceName     = "quicbaas.iam.v1.IAMService"
	pepProtoServiceName     = "quicbaas.iam.v1.PEPService"
	mfaProtoServiceName     = "quicbaas.iam.v1.MFAService"
	sessionProtoServiceName = "quicbaas.iam.v1.SessionService"
	auditProtoServiceName   = "quicbaas.iam.v1.AuditService"
)

// isIAMService checks if the service name is an IAM service.
func isIAMService(service string) bool {
	return strings.HasPrefix(service, "quicbaas.IAM.")
}

// iamHandler handles IAM service requests by converting between SDK types and protobuf types.
type iamHandler struct {
	conn *grpc.ClientConn
}

// newIAMHandler creates a new IAM handler.
func newIAMHandler(conn *grpc.ClientConn) *iamHandler {
	return &iamHandler{conn: conn}
}

// handleRequest routes IAM requests to the appropriate handler.
func (h *iamHandler) handleRequest(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	switch {
	case strings.HasPrefix(service, iamUserServicePrefix):
		return h.handleUserRequest(ctx, method, req)
	case strings.HasPrefix(service, iamTenantServicePrefix):
		return h.handleTenantRequest(ctx, method, req)
	case strings.HasPrefix(service, iamRoleServicePrefix):
		return h.handleRoleRequest(ctx, method, req)
	case strings.HasPrefix(service, iamPolicyServicePrefix):
		return h.handlePolicyRequest(ctx, method, req)
	case strings.HasPrefix(service, iamAppServicePrefix):
		return h.handleAppRequest(ctx, method, req)
	case strings.HasPrefix(service, iamSessionServicePrefix):
		return h.handleSessionRequest(ctx, method, req)
	case strings.HasPrefix(service, iamAuditServicePrefix):
		return h.handleAuditRequest(ctx, method, req)
	case strings.HasPrefix(service, iamAdminServicePrefix):
		return h.handleAdminRequest(ctx, method, req)
	case strings.HasPrefix(service, iamGDPRServicePrefix):
		return h.handleGDPRRequest(ctx, method, req)
	case strings.HasPrefix(service, iamPEPServicePrefix):
		return h.handlePEPRequest(ctx, method, req)
	case strings.HasPrefix(service, iamMFAServicePrefix):
		return h.handleMFARequest(ctx, method, req)
	case strings.HasPrefix(service, iamFederationServicePrefix):
		return h.handleFederationRequest(ctx, method, req)
	case strings.HasPrefix(service, iamWebAuthnServicePrefix):
		return h.handleWebAuthnRequest(ctx, method, req)
	case strings.HasPrefix(service, iamInviteServicePrefix):
		return h.handleInviteRequest(ctx, method, req)
	case strings.HasPrefix(service, iamIdentityServicePrefix):
		return h.handleIdentityRequest(ctx, method, req)
	default:
		return nil, errors.NewUnimplemented("iamHandler.handleRequest",
			"unsupported IAM service: "+service)
	}
}

// handleUserRequest handles User service requests.
func (h *iamHandler) handleUserRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	client := iampb.NewIAMServiceClient(h.conn)

	switch method {
	case "Create":
		return h.handleCreateUser(ctx, client, req)
	case "Get":
		return h.handleGetUser(ctx, client, req)
	case "Update":
		return h.handleUpdateUser(ctx, client, req)
	case "Delete":
		return h.handleDeleteUser(ctx, client, req)
	case "List":
		return h.handleListUsers(ctx, client, req)
	case "AssignRole":
		return h.handleAssignRole(ctx, client, req)
	case "RevokeRole":
		return h.handleRevokeRole(ctx, client, req)
	default:
		return nil, errors.NewUnimplemented("iamHandler.handleUserRequest",
			"unsupported method: "+method)
	}
}

// handleTenantRequest handles Tenant service requests.
func (h *iamHandler) handleTenantRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	client := iampb.NewIAMServiceClient(h.conn)

	switch method {
	case "Create":
		return h.handleCreateTenant(ctx, client, req)
	case "Get":
		return h.handleGetTenant(ctx, client, req)
	case "Update":
		return h.handleUpdateTenant(ctx, client, req)
	case "Delete":
		return h.handleDeleteTenant(ctx, client, req)
	case "List":
		return h.handleListTenants(ctx, client, req)
	case "Provision":
		return h.handleProvisionTenant(ctx, client, req)
	default:
		return nil, errors.NewUnimplemented("iamHandler.handleTenantRequest",
			"unsupported method: "+method)
	}
}

// handleRoleRequest handles Role service requests.
func (h *iamHandler) handleRoleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	client := iampb.NewIAMServiceClient(h.conn)

	switch method {
	case "Create":
		return h.handleCreateRole(ctx, client, req)
	case "Get":
		return h.handleGetRole(ctx, client, req)
	case "Update":
		return h.handleUpdateRole(ctx, client, req)
	case "Delete":
		return h.handleDeleteRole(ctx, client, req)
	case "List":
		return h.handleListRoles(ctx, client, req)
	case "Reconcile":
		return h.handleReconcileRoles(ctx, client, req)
	default:
		return nil, errors.NewUnimplemented("iamHandler.handleRoleRequest",
			"unsupported method: "+method)
	}
}

// handlePolicyRequest handles Policy service requests.
func (h *iamHandler) handlePolicyRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	client := iampb.NewIAMServiceClient(h.conn)

	switch method {
	case "Create":
		return h.handleCreatePolicy(ctx, client, req)
	case "Get":
		return h.handleGetPolicy(ctx, client, req)
	case "Update":
		return h.handleUpdatePolicy(ctx, client, req)
	case "Delete":
		return h.handleDeletePolicy(ctx, client, req)
	case "List":
		return h.handleListPolicies(ctx, client, req)
	case "Validate":
		return h.handleValidatePolicy(ctx, client, req)
	case "Test":
		return h.handleTestPolicy(ctx, client, req)
	case "Simulate":
		return h.handleSimulatePolicy(ctx, client, req)
	default:
		return nil, errors.NewUnimplemented("iamHandler.handlePolicyRequest",
			"unsupported method: "+method)
	}
}

// handleAppRequest handles Application service requests.
func (h *iamHandler) handleAppRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	client := iampb.NewIAMServiceClient(h.conn)

	switch method {
	case "Register":
		return h.handleRegisterApp(ctx, client, req)
	case "Get":
		return h.handleGetApp(ctx, client, req)
	case "Update":
		return h.handleUpdateApp(ctx, client, req)
	case "Delete":
		return h.handleDeleteApp(ctx, client, req)
	case "List":
		return h.handleListApps(ctx, client, req)
	case "RotateSecret":
		return h.handleRotateAppSecret(ctx, client, req)
	default:
		return nil, errors.NewUnimplemented("iamHandler.handleAppRequest",
			"unsupported method: "+method)
	}
}

// handleSessionRequest handles Session service requests.
func (h *iamHandler) handleSessionRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	client := iampb.NewSessionServiceClient(h.conn)

	switch method {
	case "List":
		return h.handleListSessions(ctx, client, req)
	case "Get":
		return h.handleGetSession(ctx, client, req)
	case "Revoke":
		return h.handleRevokeSession(ctx, client, req)
	case "RevokeAll":
		return h.handleRevokeAllSessions(ctx, client, req)
	case "RevokeOther", "RevokeOthers":
		return h.handleRevokeOtherSessions(ctx, client, req)
	default:
		return nil, errors.NewUnimplemented("iamHandler.handleSessionRequest",
			"unsupported method: "+method)
	}
}

// handleAuditRequest handles Audit service requests.
func (h *iamHandler) handleAuditRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	client := iampb.NewAuditServiceClient(h.conn)

	switch method {
	case "Query":
		return h.handleQueryAuditEvents(ctx, client, req)
	case "Get":
		return h.handleGetAuditEvent(ctx, client, req)
	case "Record", "RecordAsync":
		return h.handleRecordAuditEvent(ctx, client, req)
	default:
		return nil, errors.NewUnimplemented("iamHandler.handleAuditRequest",
			"unsupported method: "+method)
	}
}

// handleAdminRequest handles Admin service requests.
func (h *iamHandler) handleAdminRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	client := iampb.NewIAMServiceClient(h.conn)

	switch method {
	case "CreateAssignment":
		return h.handleCreateAdminAssignment(ctx, client, req)
	case "GetAssignment":
		return h.handleGetAdminAssignment(ctx, client, req)
	case "UpdateAssignment":
		return h.handleUpdateAdminAssignment(ctx, client, req)
	case "DeleteAssignment":
		return h.handleDeleteAdminAssignment(ctx, client, req)
	case "ListAssignments":
		return h.handleListAdminAssignments(ctx, client, req)
	case "CheckPermission":
		return h.handleCheckAdminPermission(ctx, client, req)
	case "StartImpersonation":
		return h.handleStartImpersonation(ctx, client, req)
	case "EndImpersonation":
		return h.handleEndImpersonation(ctx, client, req)
	case "GetActiveImpersonations", "ListActiveImpersonations":
		return h.handleGetActiveImpersonations(ctx, client, req)
	default:
		return nil, errors.NewUnimplemented("iamHandler.handleAdminRequest",
			"unsupported method: "+method)
	}
}

// handleGDPRRequest handles GDPR service requests.
func (h *iamHandler) handleGDPRRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	client := iampb.NewIAMServiceClient(h.conn)

	switch method {
	case "GrantConsent":
		return h.handleGrantConsent(ctx, client, req)
	case "WithdrawConsent":
		return h.handleWithdrawConsent(ctx, client, req)
	case "GetConsent":
		return h.handleGetConsent(ctx, client, req)
	case "ListConsents":
		return h.handleListUserConsents(ctx, client, req)
	case "RequestExport":
		return h.handleRequestExport(ctx, client, req)
	case "GetExportStatus":
		return h.handleGetExportStatus(ctx, client, req)
	case "GenerateExport":
		return h.handleGenerateExport(ctx, client, req)
	case "RequestDeletion":
		return h.handleRequestDeletion(ctx, client, req)
	case "CancelDeletion":
		return h.handleCancelDeletion(ctx, client, req)
	case "ProcessDeletion":
		return h.handleProcessDeletion(ctx, client, req)
	case "GetDeletionStatus":
		return h.handleGetDeletionStatus(ctx, client, req)
	default:
		return nil, errors.NewUnimplemented("iamHandler.handleGDPRRequest",
			"unsupported method: "+method)
	}
}

// handlePEPRequest handles PEP service requests.
func (h *iamHandler) handlePEPRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	client := iampb.NewPEPServiceClient(h.conn)

	switch method {
	case "Check":
		return h.handlePEPCheck(ctx, client, req)
	case "CheckBatch":
		return h.handlePEPCheckBatch(ctx, client, req)
	default:
		return nil, errors.NewUnimplemented("iamHandler.handlePEPRequest",
			"unsupported method: "+method)
	}
}

// handleMFARequest handles MFA service requests.
func (h *iamHandler) handleMFARequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	client := iampb.NewMFAServiceClient(h.conn)

	switch method {
	case "BeginEnrollment", "BeginMFAEnrollment":
		return h.handleBeginMFAEnrollment(ctx, client, req)
	case "CompleteEnrollment", "CompleteMFAEnrollment":
		return h.handleCompleteMFAEnrollment(ctx, client, req)
	case "CancelEnrollment":
		return h.handleCancelMFAEnrollment(ctx, client, req)
	case "BeginChallenge":
		return h.handleBeginMFAChallenge(ctx, client, req)
	case "VerifyChallenge":
		return h.handleVerifyMFAChallenge(ctx, client, req)
	case "VerifyCode":
		return h.handleVerifyMFACode(ctx, client, req)
	case "GetStatus", "GetMFAStatus":
		return h.handleGetMFAStatus(ctx, client, req)
	case "Disable", "DisableMFA":
		return h.handleDisableMFA(ctx, client, req)
	case "GenerateBackupCodes":
		return h.handleGenerateBackupCodes(ctx, client, req)
	case "VerifyBackupCode":
		return h.handleVerifyBackupCode(ctx, client, req)
	case "GetBackupCodeStatus":
		return h.handleGetBackupCodeStatus(ctx, client, req)
	default:
		return nil, errors.NewUnimplemented("iamHandler.handleMFARequest",
			"unsupported method: "+method)
	}
}

// timestampToProto converts a time.Time to a protobuf timestamp.
func timestampToProto(t interface{}) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.Now() // Placeholder, actual conversion in specific handlers
}
