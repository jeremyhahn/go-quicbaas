//go:build mcp

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

package mcp

import (
	"context"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// executeIAMTool routes IAM tool execution to the appropriate handler.
// This is called from the main ExecuteTool switch statement.
func (r *ToolRegistry) executeIAMTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	// Tenant Operations
	case "quicbaas_iam_tenant_create":
		return r.executeIAMTenantCreate(ctx, args)
	case "quicbaas_iam_tenant_get":
		return r.executeIAMTenantGet(ctx, args)
	case "quicbaas_iam_tenant_update":
		return r.executeIAMTenantUpdate(ctx, args)
	case "quicbaas_iam_tenant_delete":
		return r.executeIAMTenantDelete(ctx, args)
	case "quicbaas_iam_tenant_list":
		return r.executeIAMTenantList(ctx, args)

	// User Operations
	case "quicbaas_iam_user_create":
		return r.executeIAMUserCreate(ctx, args)
	case "quicbaas_iam_user_get":
		return r.executeIAMUserGet(ctx, args)
	case "quicbaas_iam_user_update":
		return r.executeIAMUserUpdate(ctx, args)
	case "quicbaas_iam_user_delete":
		return r.executeIAMUserDelete(ctx, args)
	case "quicbaas_iam_user_list":
		return r.executeIAMUserList(ctx, args)
	case "quicbaas_iam_user_assign_role":
		return r.executeIAMUserAssignRole(ctx, args)
	case "quicbaas_iam_user_revoke_role":
		return r.executeIAMUserRevokeRole(ctx, args)

	// Group Operations
	case "quicbaas_iam_group_create":
		return r.executeIAMGroupCreate(ctx, args)
	case "quicbaas_iam_group_get":
		return r.executeIAMGroupGet(ctx, args)
	case "quicbaas_iam_group_update":
		return r.executeIAMGroupUpdate(ctx, args)
	case "quicbaas_iam_group_delete":
		return r.executeIAMGroupDelete(ctx, args)
	case "quicbaas_iam_group_list":
		return r.executeIAMGroupList(ctx, args)
	case "quicbaas_iam_group_add_member":
		return r.executeIAMGroupAddMember(ctx, args)
	case "quicbaas_iam_group_remove_member":
		return r.executeIAMGroupRemoveMember(ctx, args)
	case "quicbaas_iam_group_list_members":
		return r.executeIAMGroupListMembers(ctx, args)

	// Role Operations
	case "quicbaas_iam_role_create":
		return r.executeIAMRoleCreate(ctx, args)
	case "quicbaas_iam_role_get":
		return r.executeIAMRoleGet(ctx, args)
	case "quicbaas_iam_role_update":
		return r.executeIAMRoleUpdate(ctx, args)
	case "quicbaas_iam_role_delete":
		return r.executeIAMRoleDelete(ctx, args)
	case "quicbaas_iam_role_list":
		return r.executeIAMRoleList(ctx, args)
	case "quicbaas_iam_role_reconcile":
		return r.executeIAMRoleReconcile(ctx, args)

	// Policy Operations
	case "quicbaas_iam_policy_create":
		return r.executeIAMPolicyCreate(ctx, args)
	case "quicbaas_iam_policy_get":
		return r.executeIAMPolicyGet(ctx, args)
	case "quicbaas_iam_policy_update":
		return r.executeIAMPolicyUpdate(ctx, args)
	case "quicbaas_iam_policy_delete":
		return r.executeIAMPolicyDelete(ctx, args)
	case "quicbaas_iam_policy_list":
		return r.executeIAMPolicyList(ctx, args)
	case "quicbaas_iam_policy_test":
		return r.executeIAMPolicyTest(ctx, args)
	case "quicbaas_iam_policy_validate":
		return r.executeIAMPolicyValidate(ctx, args)

	// Audit Operations
	case "quicbaas_iam_audit_query":
		return r.executeIAMAuditQuery(ctx, args)
	case "quicbaas_iam_audit_get":
		return r.executeIAMAuditGet(ctx, args)
	case "quicbaas_iam_audit_delete":
		return r.executeIAMAuditDelete(ctx, args)

	// Application Operations
	case "quicbaas_iam_app_register":
		return r.executeIAMAppRegister(ctx, args)
	case "quicbaas_iam_app_get":
		return r.executeIAMAppGet(ctx, args)
	case "quicbaas_iam_app_update":
		return r.executeIAMAppUpdate(ctx, args)
	case "quicbaas_iam_app_delete":
		return r.executeIAMAppDelete(ctx, args)
	case "quicbaas_iam_app_list":
		return r.executeIAMAppList(ctx, args)
	case "quicbaas_iam_app_rotate_secret":
		return r.executeIAMAppRotateSecret(ctx, args)

	// MFA Operations
	case "quicbaas_mfa_enroll_begin":
		return r.executeMFABeginEnroll(ctx, args)
	case "quicbaas_mfa_enroll_complete":
		return r.executeMFACompleteEnroll(ctx, args)
	case "quicbaas_mfa_enroll_cancel":
		return r.executeMFACancelEnroll(ctx, args)
	case "quicbaas_mfa_status":
		return r.executeMFAGetStatus(ctx, args)
	case "quicbaas_mfa_disable":
		return r.executeMFADisable(ctx, args)
	case "quicbaas_mfa_challenge_begin":
		return r.executeMFABeginChallenge(ctx, args)
	case "quicbaas_mfa_challenge_verify":
		return r.executeMFAVerifyChallenge(ctx, args)
	case "quicbaas_mfa_backup_codes_generate":
		return r.executeMFAGenerateBackupCodes(ctx, args)
	case "quicbaas_mfa_backup_codes_verify":
		return r.executeMFAVerifyBackupCode(ctx, args)
	case "quicbaas_mfa_backup_codes_status":
		return r.executeMFAGetBackupCodeStatus(ctx, args)

	// WebAuthn credential administration
	case "quicbaas_iam_webauthn_credentials_list":
		return r.executeIAMWebAuthnCredentialsList(ctx, args)
	case "quicbaas_iam_webauthn_credential_delete":
		return r.executeIAMWebAuthnCredentialDelete(ctx, args)
	case "quicbaas_iam_webauthn_credentials_delete_all":
		return r.executeIAMWebAuthnCredentialsDeleteAll(ctx, args)

	// PEP Operations
	case "quicbaas_pep_check":
		return r.executePEPCheck(ctx, args)
	case "quicbaas_pep_check_batch":
		return r.executePEPCheckBatch(ctx, args)

	// Delegated Admin Assignment Operations
	case "quicbaas_iam_admin_assignment_create":
		return r.executeIAMAdminAssignmentCreate(ctx, args)
	case "quicbaas_iam_admin_assignment_get":
		return r.executeIAMAdminAssignmentGet(ctx, args)
	case "quicbaas_iam_admin_assignment_update":
		return r.executeIAMAdminAssignmentUpdate(ctx, args)
	case "quicbaas_iam_admin_assignment_delete":
		return r.executeIAMAdminAssignmentDelete(ctx, args)
	case "quicbaas_iam_admin_assignment_list":
		return r.executeIAMAdminAssignmentList(ctx, args)

	// Admin Permission Check
	case "quicbaas_iam_admin_check_permission":
		return r.executeIAMAdminCheckPermission(ctx, args)

	// Impersonation Operations
	case "quicbaas_iam_impersonation_start":
		return r.executeIAMImpersonationStart(ctx, args)
	case "quicbaas_iam_impersonation_end":
		return r.executeIAMImpersonationEnd(ctx, args)
	case "quicbaas_iam_impersonation_list":
		return r.executeIAMImpersonationList(ctx, args)

	// GDPR Consent Operations
	case "quicbaas_iam_gdpr_consent_grant":
		return r.executeIAMGDPRConsentGrant(ctx, args)
	case "quicbaas_iam_gdpr_consent_withdraw":
		return r.executeIAMGDPRConsentWithdraw(ctx, args)
	case "quicbaas_iam_gdpr_consent_get":
		return r.executeIAMGDPRConsentGet(ctx, args)
	case "quicbaas_iam_gdpr_consent_list":
		return r.executeIAMGDPRConsentList(ctx, args)

	// GDPR Export Operations
	case "quicbaas_iam_gdpr_export_request":
		return r.executeIAMGDPRExportRequest(ctx, args)
	case "quicbaas_iam_gdpr_export_status":
		return r.executeIAMGDPRExportStatus(ctx, args)
	case "quicbaas_iam_gdpr_export_generate":
		return r.executeIAMGDPRExportGenerate(ctx, args)

	// GDPR Deletion Operations
	case "quicbaas_iam_gdpr_deletion_request":
		return r.executeIAMGDPRDeletionRequest(ctx, args)
	case "quicbaas_iam_gdpr_deletion_cancel":
		return r.executeIAMGDPRDeletionCancel(ctx, args)
	case "quicbaas_iam_gdpr_deletion_process":
		return r.executeIAMGDPRDeletionProcess(ctx, args)
	case "quicbaas_iam_gdpr_deletion_status":
		return r.executeIAMGDPRDeletionStatus(ctx, args)

	// Federation Provider Operations
	case "quicbaas_iam_federation_createprovider":
		return r.executeIAMFederationCreateProvider(ctx, args)
	case "quicbaas_iam_federation_getprovider":
		return r.executeIAMFederationGetProvider(ctx, args)
	case "quicbaas_iam_federation_updateprovider":
		return r.executeIAMFederationUpdateProvider(ctx, args)
	case "quicbaas_iam_federation_deleteprovider":
		return r.executeIAMFederationDeleteProvider(ctx, args)
	case "quicbaas_iam_federation_listproviders":
		return r.executeIAMFederationListProviders(ctx, args)

	// Federation Authentication Flow
	case "quicbaas_iam_federation_beginlogin":
		return r.executeIAMFederationBeginLogin(ctx, args)
	case "quicbaas_iam_federation_handlecallback":
		return r.executeIAMFederationHandleCallback(ctx, args)

	// Federation Account Linking
	case "quicbaas_iam_federation_linkaccount":
		return r.executeIAMFederationLinkAccount(ctx, args)
	case "quicbaas_iam_federation_unlinkaccount":
		return r.executeIAMFederationUnlinkAccount(ctx, args)
	case "quicbaas_iam_federation_getuserlinks":
		return r.executeIAMFederationGetUserLinks(ctx, args)

	// Identity Operations
	case "quicbaas_iam_identity_linkidentity":
		return r.executeIAMIdentityLink(ctx, args)
	case "quicbaas_iam_identity_unlinkidentity":
		return r.executeIAMIdentityUnlink(ctx, args)
	case "quicbaas_iam_identity_listuseridentities":
		return r.executeIAMIdentityListUserIdentities(ctx, args)

	// Session Operations
	case "quicbaas_iam_session_get":
		return r.executeIAMSessionGet(ctx, args)
	case "quicbaas_iam_session_list":
		return r.executeIAMSessionList(ctx, args)
	case "quicbaas_iam_session_revoke":
		return r.executeIAMSessionRevoke(ctx, args)
	case "quicbaas_iam_session_revoke_all":
		return r.executeIAMSessionRevokeAll(ctx, args)
	case "quicbaas_iam_session_revoke_others":
		return r.executeIAMSessionRevokeOthers(ctx, args)
	case "quicbaas_iam_session_touch":
		return r.executeIAMSessionTouch(ctx, args)

	default:
		// Tool name matched isIAMTool() but has no handler - return error
		return nil, api.NewUnknownToolError(name)
	}
}
