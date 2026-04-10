//go:build iam

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

// Package main provides IAM CLI command success-path tests using mock clients.
package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// setupIAMMock injects a mock IAM client factory and returns a cleanup function.
func setupIAMMock(t *testing.T) (*testMockIAMClient, func()) {
	t.Helper()
	origFactory := iamClientFactory
	origTimeout := timeout
	mockClient := newTestMockIAMClient()
	iamClientFactory = func() (iam.Client, error) {
		return mockClient, nil
	}
	timeout = 5 * time.Second
	return mockClient, func() {
		iamClientFactory = origFactory
		timeout = origTimeout
	}
}

// =============================================================================
// Admin Assignment Commands - Success Paths
// =============================================================================

func TestIAMAdminAssignmentCreate_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminAssignmentCreateCommand()
	adminUserID = "user_abc123"
	adminRole = "tenant_admin"
	adminGrantedBy = "admin_xyz"
	adminExpiresAt = ""
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"
	adminPermissions = []string{"user:create", "user:read"}

	err := runIAMAdminAssignmentCreateCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminAssignmentCreate_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminAssignmentCreateCommand()
	adminUserID = "user_abc123"
	adminRole = "super_admin"
	adminGrantedBy = "admin_xyz"
	adminExpiresAt = time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"
	adminPermissions = nil

	err := runIAMAdminAssignmentCreateCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminAssignmentGet_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminAssignmentGetCommand()
	adminAssignmentID = "adm_test123"
	iamOutputFormat = "table"

	err := runIAMAdminAssignmentGetCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminAssignmentGet_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminAssignmentGetCommand()
	adminAssignmentID = "adm_test123"
	iamOutputFormat = "json"

	err := runIAMAdminAssignmentGetCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminAssignmentUpdate_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminAssignmentUpdateCommand()
	adminAssignmentID = "adm_test123"
	adminRole = ""
	adminExpiresAt = ""
	adminPermissions = nil
	iamOutputFormat = "table"

	err := runIAMAdminAssignmentUpdateCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminAssignmentUpdate_SuccessWithRole(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminAssignmentUpdateCommand()
	// Mark the role flag as changed so the code processes it
	cmd.Flags().Set("role", "policy_admin")
	adminAssignmentID = "adm_test123"
	adminExpiresAt = ""
	adminPermissions = nil
	iamOutputFormat = "json"

	err := runIAMAdminAssignmentUpdateCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminAssignmentDelete_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminAssignmentDeleteCommand()
	adminAssignmentID = "adm_test123"
	iamForce = true

	err := runIAMAdminAssignmentDeleteCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminAssignmentList_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminAssignmentListCommand()
	adminUserID = ""
	adminRole = ""
	adminActive = false
	iamOutputFormat = "table"

	err := runIAMAdminAssignmentListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminAssignmentList_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminAssignmentListCommand()
	adminUserID = "user_abc123"
	adminRole = "tenant_admin"
	adminActive = true
	iamOutputFormat = "json"

	err := runIAMAdminAssignmentListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminCheckPermission_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminCheckPermissionCommand()
	adminUserID = "user_abc123"
	adminCheckPermission = "user:create"
	adminCheckTenantID = "tenant_test"
	adminCheckResourceType = "users"
	adminCheckResourceID = "user_001"
	iamOutputFormat = "table"

	err := runIAMAdminCheckPermissionCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminCheckPermission_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminCheckPermissionCommand()
	adminUserID = "user_abc123"
	adminCheckPermission = "user:create"
	adminCheckTenantID = "tenant_test"
	adminCheckResourceType = ""
	adminCheckResourceID = ""
	iamOutputFormat = "json"

	err := runIAMAdminCheckPermissionCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminImpersonateStart_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminImpersonateStartCommand()
	impersonateTargetUserID = "user_target"
	impersonateTenantID = "tenant_test"
	impersonateReason = "Support ticket #123"
	impersonateDuration = "1h"
	iamOutputFormat = "table"

	err := runIAMAdminImpersonateStartCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminImpersonateStart_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminImpersonateStartCommand()
	impersonateTargetUserID = "user_target"
	impersonateTenantID = "tenant_test"
	impersonateReason = "Support ticket #123"
	impersonateDuration = "30m"
	iamOutputFormat = "json"

	err := runIAMAdminImpersonateStartCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminImpersonateEnd_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminImpersonateEndCommand()
	impersonateSessionID = "imp_test123"

	err := runIAMAdminImpersonateEndCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminImpersonateList_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminImpersonateListCommand()
	iamOutputFormat = "table"

	err := runIAMAdminImpersonateListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminImpersonateList_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminImpersonateListCommand()
	iamOutputFormat = "json"

	err := runIAMAdminImpersonateListCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// App Commands - Success Paths
// =============================================================================

func TestIAMAppRegister_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAppRegisterCommand()
	iamAppName = "test-app"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMAppRegisterCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAppRegister_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAppRegisterCommand()
	iamAppName = "test-app"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMAppRegisterCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAppGet_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAppGetCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMAppGetCommand(cmd, []string{"app_test123"})
	assert.NoError(t, err)
}

func TestIAMAppGet_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAppGetCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMAppGetCommand(cmd, []string{"app_test123"})
	assert.NoError(t, err)
}

func TestIAMAppUpdate_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAppUpdateCommand()
	// Must mark at least one flag as changed for the update validation to pass
	cmd.Flags().Set("name", "Updated App Name")
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMAppUpdateCommand(cmd, []string{"app_test123"})
	assert.NoError(t, err)
}

func TestIAMAppDelete_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAppDeleteCommand()
	globalTenantID = "tenant_test"
	iamForce = true

	err := runIAMAppDeleteCommand(cmd, []string{"app_test123"})
	assert.NoError(t, err)
}

func TestIAMAppList_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAppListCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMAppListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAppList_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAppListCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMAppListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAppRotateSecret_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAppRotateSecretCommand()
	globalTenantID = "tenant_test"
	iamForce = true
	iamOutputFormat = "table"

	err := runIAMAppRotateSecretCommand(cmd, []string{"app_test123"})
	assert.NoError(t, err)
}

// =============================================================================
// Audit Commands - Success Paths
// =============================================================================

func TestIAMAuditQuery_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAuditQueryCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMAuditQueryCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAuditQuery_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAuditQueryCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMAuditQueryCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAuditGet_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAuditGetCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMAuditGetCommand(cmd, []string{"evt_001"})
	assert.NoError(t, err)
}

func TestIAMAuditGet_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAuditGetCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMAuditGetCommand(cmd, []string{"evt_001"})
	assert.NoError(t, err)
}

// =============================================================================
// GDPR Commands - Success Paths
// =============================================================================

func TestIAMGDPRConsentGrant_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRConsentGrantCommand()
	gdprUserID = "user_001"
	gdprPurpose = "analytics"
	globalTenantID = "tenant_test"

	err := runIAMGDPRConsentGrantCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRConsentWithdraw_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRConsentWithdrawCommand()
	gdprUserID = "user_001"
	gdprPurpose = "analytics"
	iamForce = true
	globalTenantID = "tenant_test"

	err := runIAMGDPRConsentWithdrawCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRConsentGet_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRConsentGetCommand()
	gdprUserID = "user_001"
	gdprPurpose = "analytics"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMGDPRConsentGetCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRConsentList_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRConsentListCommand()
	gdprUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMGDPRConsentListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRExportRequest_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRExportRequestCommand()
	gdprUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMGDPRExportRequestCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRExportStatus_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRExportStatusCommand()
	gdprUserID = "user_001"
	gdprExportID = "export_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMGDPRExportStatusCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRDeletionRequest_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRDeletionRequestCommand()
	gdprUserID = "user_001"
	gdprDeletionReason = "user request"
	globalTenantID = "tenant_test"
	iamForce = true
	iamOutputFormat = "table"

	err := runIAMGDPRDeletionRequestCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRDeletionCancel_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRDeletionCancelCommand()
	gdprUserID = "user_001"
	gdprDeletionID = "del_001"
	globalTenantID = "tenant_test"

	err := runIAMGDPRDeletionCancelCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRDeletionStatus_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRDeletionStatusCommand()
	gdprUserID = "user_001"
	gdprDeletionID = "del_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMGDPRDeletionStatusCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// PEP Commands - Success Paths
// =============================================================================

func TestIAMPEPCheck_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPEPCheckCommand()
	pepSubject = `{"id":"user_001","roles":["admin"]}`
	pepAction = "read"
	pepResource = "orders://orders/*"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMPEPCheckCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMPEPCheck_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPEPCheckCommand()
	pepSubject = `{"id":"user_001","roles":["admin"]}`
	pepAction = "read"
	pepResource = "orders://orders/*"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMPEPCheckCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// Policy Commands - Success Paths
// =============================================================================

func TestIAMPolicyGet_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPolicyGetCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMPolicyGetCommand(cmd, []string{"test-policy"})
	assert.NoError(t, err)
}

func TestIAMPolicyGet_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPolicyGetCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMPolicyGetCommand(cmd, []string{"test-policy"})
	assert.NoError(t, err)
}

func TestIAMPolicyDelete_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPolicyDeleteCommand()
	globalTenantID = "tenant_test"
	iamForce = true

	err := runIAMPolicyDeleteCommand(cmd, []string{"test-policy"})
	assert.NoError(t, err)
}

func TestIAMPolicyList_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPolicyListCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMPolicyListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMPolicyList_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPolicyListCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMPolicyListCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// Session Commands - Success Paths
// =============================================================================

func TestIAMSessionGet_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMSessionGetCommand()
	iamOutputFormat = "table"

	err := runIAMSessionGetCommand(cmd, []string{"sess_001"})
	assert.NoError(t, err)
}

func TestIAMSessionGet_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMSessionGetCommand()
	iamOutputFormat = "json"

	err := runIAMSessionGetCommand(cmd, []string{"sess_001"})
	assert.NoError(t, err)
}

func TestIAMSessionList_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMSessionListCommand()
	sessionUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMSessionListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMSessionRevoke_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMSessionRevokeCommand()
	iamForce = true

	err := runIAMSessionRevokeCommand(cmd, []string{"sess_001"})
	assert.NoError(t, err)
}

func TestIAMSessionRevokeAll_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMSessionRevokeAllCommand()
	sessionUserID = "user_001"
	globalTenantID = "tenant_test"
	iamForce = true

	err := runIAMSessionRevokeAllCommand(cmd, []string{})
	assert.NoError(t, err)
}
