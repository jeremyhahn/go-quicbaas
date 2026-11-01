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
	origTimeout := globalTimeout
	mockClient := newTestMockIAMClient()
	iamClientFactory = func() (iam.Client, error) {
		return mockClient, nil
	}
	globalTimeout = 5 * time.Second
	return mockClient, func() {
		iamClientFactory = origFactory
		globalTimeout = origTimeout
	}
}

// iamSuccessCase is one success-path row: run sets the command's package
// flag values and executes it against the mock IAM client.
type iamSuccessCase struct {
	name string
	run  func() error
}

// runIAMSuccessCases runs each case as a subtest with a fresh mock IAM
// client and logger, and asserts that the command succeeds.
func runIAMSuccessCases(t *testing.T, cases []iamSuccessCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, cleanup := setupIAMMock(t)
			defer cleanup()
			cleanupLog := testInitLogger(t)
			defer cleanupLog()

			assert.NoError(t, tc.run())
		})
	}
}

// =============================================================================
// Admin Assignment Commands - Success Paths
// =============================================================================

func TestIAMAdminAssignment_Success(t *testing.T) {
	runIAMSuccessCases(t, []iamSuccessCase{
		{"create table", func() error {
			cmd := newIAMAdminAssignmentCreateCommand()
			adminUserID = "user_abc123"
			adminRole = "tenant_admin"
			adminGrantedBy = "admin_xyz"
			adminExpiresAt = ""
			globalTenantID = "tenant_test"
			iamOutputFormat = "table"
			adminPermissions = []string{"user:create", "user:read"}
			return runIAMAdminAssignmentCreateCommand(cmd, []string{})
		}},
		{"create json with expiry", func() error {
			cmd := newIAMAdminAssignmentCreateCommand()
			adminUserID = "user_abc123"
			adminRole = "super_admin"
			adminGrantedBy = "admin_xyz"
			adminExpiresAt = time.Now().Add(24 * time.Hour).Format(time.RFC3339)
			globalTenantID = "tenant_test"
			iamOutputFormat = "json"
			adminPermissions = nil
			return runIAMAdminAssignmentCreateCommand(cmd, []string{})
		}},
		{"get table", func() error {
			cmd := newIAMAdminAssignmentGetCommand()
			adminAssignmentID = "adm_test123"
			iamOutputFormat = "table"
			return runIAMAdminAssignmentGetCommand(cmd, []string{})
		}},
		{"get json", func() error {
			cmd := newIAMAdminAssignmentGetCommand()
			adminAssignmentID = "adm_test123"
			iamOutputFormat = "json"
			return runIAMAdminAssignmentGetCommand(cmd, []string{})
		}},
		{"update table without changed flags", func() error {
			cmd := newIAMAdminAssignmentUpdateCommand()
			adminAssignmentID = "adm_test123"
			adminRole = ""
			adminExpiresAt = ""
			adminPermissions = nil
			iamOutputFormat = "table"
			return runIAMAdminAssignmentUpdateCommand(cmd, []string{})
		}},
		{"update json with changed role", func() error {
			cmd := newIAMAdminAssignmentUpdateCommand()
			// Mark the role flag as changed so the code validates it.
			if err := cmd.Flags().Set("role", "policy_admin"); err != nil {
				return err
			}
			adminAssignmentID = "adm_test123"
			adminExpiresAt = ""
			adminPermissions = nil
			iamOutputFormat = "json"
			return runIAMAdminAssignmentUpdateCommand(cmd, []string{})
		}},
		{"delete forced", func() error {
			cmd := newIAMAdminAssignmentDeleteCommand()
			adminAssignmentID = "adm_test123"
			iamForce = true
			return runIAMAdminAssignmentDeleteCommand(cmd, []string{})
		}},
		{"list table unfiltered", func() error {
			cmd := newIAMAdminAssignmentListCommand()
			adminUserID = ""
			adminRole = ""
			adminActive = false
			iamOutputFormat = "table"
			return runIAMAdminAssignmentListCommand(cmd, []string{})
		}},
		{"list json active only", func() error {
			cmd := newIAMAdminAssignmentListCommand()
			adminUserID = "user_abc123"
			adminRole = "tenant_admin"
			adminActive = true
			iamOutputFormat = "json"
			return runIAMAdminAssignmentListCommand(cmd, []string{})
		}},
	})
}

func TestIAMAdminCheckPermission_Success(t *testing.T) {
	runIAMSuccessCases(t, []iamSuccessCase{
		{"table with resource", func() error {
			cmd := newIAMAdminCheckPermissionCommand()
			adminUserID = "user_abc123"
			adminCheckPermission = "user:create"
			adminCheckTenantID = "tenant_test"
			adminCheckResourceType = "users"
			adminCheckResourceID = "user_001"
			iamOutputFormat = "table"
			return runIAMAdminCheckPermissionCommand(cmd, []string{})
		}},
		{"json without resource", func() error {
			cmd := newIAMAdminCheckPermissionCommand()
			adminUserID = "user_abc123"
			adminCheckPermission = "user:create"
			adminCheckTenantID = "tenant_test"
			adminCheckResourceType = ""
			adminCheckResourceID = ""
			iamOutputFormat = "json"
			return runIAMAdminCheckPermissionCommand(cmd, []string{})
		}},
	})
}

func TestIAMAdminImpersonate_Success(t *testing.T) {
	runIAMSuccessCases(t, []iamSuccessCase{
		{"start table", func() error {
			cmd := newIAMAdminImpersonateStartCommand()
			impersonateTargetUserID = "user_target"
			impersonateTenantID = "tenant_test"
			impersonateReason = "Support ticket #123"
			impersonateDuration = "1h"
			iamOutputFormat = "table"
			return runIAMAdminImpersonateStartCommand(cmd, []string{})
		}},
		{"start json", func() error {
			cmd := newIAMAdminImpersonateStartCommand()
			impersonateTargetUserID = "user_target"
			impersonateTenantID = "tenant_test"
			impersonateReason = "Support ticket #123"
			impersonateDuration = "30m"
			iamOutputFormat = "json"
			return runIAMAdminImpersonateStartCommand(cmd, []string{})
		}},
		{"end", func() error {
			cmd := newIAMAdminImpersonateEndCommand()
			impersonateSessionID = "imp_test123"
			return runIAMAdminImpersonateEndCommand(cmd, []string{})
		}},
		{"list table", func() error {
			cmd := newIAMAdminImpersonateListCommand()
			iamOutputFormat = "table"
			return runIAMAdminImpersonateListCommand(cmd, []string{})
		}},
		{"list json", func() error {
			cmd := newIAMAdminImpersonateListCommand()
			iamOutputFormat = "json"
			return runIAMAdminImpersonateListCommand(cmd, []string{})
		}},
	})
}

// =============================================================================
// App Commands - Success Paths
// =============================================================================

func TestIAMApp_Success(t *testing.T) {
	runIAMSuccessCases(t, []iamSuccessCase{
		{"register table", func() error {
			cmd := newIAMAppRegisterCommand()
			iamAppName = "test-app"
			globalTenantID = "tenant_test"
			iamOutputFormat = "table"
			return runIAMAppRegisterCommand(cmd, []string{})
		}},
		{"register json", func() error {
			cmd := newIAMAppRegisterCommand()
			iamAppName = "test-app"
			globalTenantID = "tenant_test"
			iamOutputFormat = "json"
			return runIAMAppRegisterCommand(cmd, []string{})
		}},
		{"get table", func() error {
			cmd := newIAMAppGetCommand()
			globalTenantID = "tenant_test"
			iamOutputFormat = "table"
			return runIAMAppGetCommand(cmd, []string{"app_test123"})
		}},
		{"get json", func() error {
			cmd := newIAMAppGetCommand()
			globalTenantID = "tenant_test"
			iamOutputFormat = "json"
			return runIAMAppGetCommand(cmd, []string{"app_test123"})
		}},
		{"update changed name", func() error {
			cmd := newIAMAppUpdateCommand()
			// At least one flag must be changed for the update validation to pass.
			if err := cmd.Flags().Set("name", "Updated App Name"); err != nil {
				return err
			}
			globalTenantID = "tenant_test"
			iamOutputFormat = "table"
			return runIAMAppUpdateCommand(cmd, []string{"app_test123"})
		}},
		{"delete forced", func() error {
			cmd := newIAMAppDeleteCommand()
			globalTenantID = "tenant_test"
			iamForce = true
			return runIAMAppDeleteCommand(cmd, []string{"app_test123"})
		}},
		{"list table", func() error {
			cmd := newIAMAppListCommand()
			globalTenantID = "tenant_test"
			iamOutputFormat = "table"
			return runIAMAppListCommand(cmd, []string{})
		}},
		{"list json", func() error {
			cmd := newIAMAppListCommand()
			globalTenantID = "tenant_test"
			iamOutputFormat = "json"
			return runIAMAppListCommand(cmd, []string{})
		}},
		{"rotate secret forced", func() error {
			cmd := newIAMAppRotateSecretCommand()
			globalTenantID = "tenant_test"
			iamForce = true
			iamOutputFormat = "table"
			return runIAMAppRotateSecretCommand(cmd, []string{"app_test123"})
		}},
	})
}

// =============================================================================
// Audit Commands - Success Paths
// =============================================================================

func TestIAMAudit_Success(t *testing.T) {
	runIAMSuccessCases(t, []iamSuccessCase{
		{"query table", func() error {
			cmd := newIAMAuditQueryCommand()
			globalTenantID = "tenant_test"
			iamOutputFormat = "table"
			return runIAMAuditQueryCommand(cmd, []string{})
		}},
		{"query json", func() error {
			cmd := newIAMAuditQueryCommand()
			globalTenantID = "tenant_test"
			iamOutputFormat = "json"
			return runIAMAuditQueryCommand(cmd, []string{})
		}},
		{"get table", func() error {
			cmd := newIAMAuditGetCommand()
			globalTenantID = "tenant_test"
			iamOutputFormat = "table"
			return runIAMAuditGetCommand(cmd, []string{"evt_001"})
		}},
		{"get json", func() error {
			cmd := newIAMAuditGetCommand()
			globalTenantID = "tenant_test"
			iamOutputFormat = "json"
			return runIAMAuditGetCommand(cmd, []string{"evt_001"})
		}},
	})
}

// =============================================================================
// GDPR Commands - Success Paths
// =============================================================================

func TestIAMGDPR_Success(t *testing.T) {
	runIAMSuccessCases(t, []iamSuccessCase{
		{"consent grant", func() error {
			cmd := newIAMGDPRConsentGrantCommand()
			gdprUserID = "user_001"
			gdprPurpose = "analytics"
			globalTenantID = "tenant_test"
			return runIAMGDPRConsentGrantCommand(cmd, []string{})
		}},
		{"consent withdraw forced", func() error {
			cmd := newIAMGDPRConsentWithdrawCommand()
			gdprUserID = "user_001"
			gdprPurpose = "analytics"
			iamForce = true
			globalTenantID = "tenant_test"
			return runIAMGDPRConsentWithdrawCommand(cmd, []string{})
		}},
		{"consent get", func() error {
			cmd := newIAMGDPRConsentGetCommand()
			gdprUserID = "user_001"
			gdprPurpose = "analytics"
			globalTenantID = "tenant_test"
			iamOutputFormat = "table"
			return runIAMGDPRConsentGetCommand(cmd, []string{})
		}},
		{"consent list", func() error {
			cmd := newIAMGDPRConsentListCommand()
			gdprUserID = "user_001"
			globalTenantID = "tenant_test"
			iamOutputFormat = "table"
			return runIAMGDPRConsentListCommand(cmd, []string{})
		}},
		{"export request", func() error {
			cmd := newIAMGDPRExportRequestCommand()
			gdprUserID = "user_001"
			globalTenantID = "tenant_test"
			iamOutputFormat = "table"
			return runIAMGDPRExportRequestCommand(cmd, []string{})
		}},
		{"export status", func() error {
			cmd := newIAMGDPRExportStatusCommand()
			gdprUserID = "user_001"
			gdprExportID = "export_001"
			globalTenantID = "tenant_test"
			iamOutputFormat = "table"
			return runIAMGDPRExportStatusCommand(cmd, []string{})
		}},
		{"deletion request forced", func() error {
			cmd := newIAMGDPRDeletionRequestCommand()
			gdprUserID = "user_001"
			gdprDeletionReason = "user request"
			globalTenantID = "tenant_test"
			iamForce = true
			iamOutputFormat = "table"
			return runIAMGDPRDeletionRequestCommand(cmd, []string{})
		}},
		{"deletion cancel", func() error {
			cmd := newIAMGDPRDeletionCancelCommand()
			gdprUserID = "user_001"
			gdprDeletionID = "del_001"
			globalTenantID = "tenant_test"
			return runIAMGDPRDeletionCancelCommand(cmd, []string{})
		}},
		{"deletion status", func() error {
			cmd := newIAMGDPRDeletionStatusCommand()
			gdprUserID = "user_001"
			gdprDeletionID = "del_001"
			globalTenantID = "tenant_test"
			iamOutputFormat = "table"
			return runIAMGDPRDeletionStatusCommand(cmd, []string{})
		}},
	})
}

// =============================================================================
// PEP Commands - Success Paths
// =============================================================================

func TestIAMPEPCheck_Success(t *testing.T) {
	runIAMSuccessCases(t, []iamSuccessCase{
		{"table", func() error {
			cmd := newIAMPEPCheckCommand()
			pepSubject = `{"id":"user_001","roles":["admin"]}`
			pepAction = "read"
			pepResource = "orders://orders/*"
			globalTenantID = "tenant_test"
			iamOutputFormat = "table"
			return runIAMPEPCheckCommand(cmd, []string{})
		}},
		{"json", func() error {
			cmd := newIAMPEPCheckCommand()
			pepSubject = `{"id":"user_001","roles":["admin"]}`
			pepAction = "read"
			pepResource = "orders://orders/*"
			globalTenantID = "tenant_test"
			iamOutputFormat = "json"
			return runIAMPEPCheckCommand(cmd, []string{})
		}},
	})
}

// =============================================================================
// Policy Commands - Success Paths
// =============================================================================

func TestIAMPolicy_Success(t *testing.T) {
	runIAMSuccessCases(t, []iamSuccessCase{
		{"get table", func() error {
			cmd := newIAMPolicyGetCommand()
			globalTenantID = "tenant_test"
			iamOutputFormat = "table"
			return runIAMPolicyGetCommand(cmd, []string{"test-policy"})
		}},
		{"get json", func() error {
			cmd := newIAMPolicyGetCommand()
			globalTenantID = "tenant_test"
			iamOutputFormat = "json"
			return runIAMPolicyGetCommand(cmd, []string{"test-policy"})
		}},
		{"delete forced", func() error {
			cmd := newIAMPolicyDeleteCommand()
			globalTenantID = "tenant_test"
			iamForce = true
			return runIAMPolicyDeleteCommand(cmd, []string{"test-policy"})
		}},
		{"list table", func() error {
			cmd := newIAMPolicyListCommand()
			globalTenantID = "tenant_test"
			iamOutputFormat = "table"
			return runIAMPolicyListCommand(cmd, []string{})
		}},
		{"list json", func() error {
			cmd := newIAMPolicyListCommand()
			globalTenantID = "tenant_test"
			iamOutputFormat = "json"
			return runIAMPolicyListCommand(cmd, []string{})
		}},
	})
}

// =============================================================================
// Session Commands - Success Paths
// =============================================================================

func TestIAMSession_Success(t *testing.T) {
	runIAMSuccessCases(t, []iamSuccessCase{
		{"get table", func() error {
			cmd := newIAMSessionGetCommand()
			iamOutputFormat = "table"
			return runIAMSessionGetCommand(cmd, []string{"sess_001"})
		}},
		{"get json", func() error {
			cmd := newIAMSessionGetCommand()
			iamOutputFormat = "json"
			return runIAMSessionGetCommand(cmd, []string{"sess_001"})
		}},
		{"list", func() error {
			cmd := newIAMSessionListCommand()
			sessionUserID = "user_001"
			globalTenantID = "tenant_test"
			iamOutputFormat = "table"
			return runIAMSessionListCommand(cmd, []string{})
		}},
		{"revoke forced", func() error {
			cmd := newIAMSessionRevokeCommand()
			iamForce = true
			return runIAMSessionRevokeCommand(cmd, []string{"sess_001"})
		}},
		{"revoke all forced", func() error {
			cmd := newIAMSessionRevokeAllCommand()
			sessionUserID = "user_001"
			globalTenantID = "tenant_test"
			iamForce = true
			return runIAMSessionRevokeAllCommand(cmd, []string{})
		}},
	})
}
