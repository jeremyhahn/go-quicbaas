//go:build mcp

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestMFAObjectTools_StayInCallerTenant proves the MCP MFA
// complete/cancel/verify tools bind the enrollment or challenge id to the
// caller's tenant: another tenant gets not-found and the record survives, a
// call with no tenant is refused, and the owning tenant reaches it.
func TestMFAObjectTools_StayInCallerTenant(t *testing.T) {
	t.Parallel()

	storage := iam.NewMemoryMFAStorage()
	mgr, err := iam.NewMFAManager(iam.WithMFAStorage(storage))
	require.NoError(t, err)
	reg := newMFATestRegistry(mgr)

	enrollment, err := mgr.BeginEnrollment(context.Background(), 20002, "tenant-b", iam.MFAMethodTOTP)
	require.NoError(t, err)
	enrollArgs := map[string]interface{}{"enrollment_id": enrollment.EnrollmentID, "code": "000000"}

	intruder := tenantctx.WithTenant(context.Background(), "tenant-a")
	_, err = reg.executeMFACompleteEnroll(intruder, enrollArgs)
	assert.ErrorIs(t, err, iam.ErrMFAEnrollmentNotFound)
	_, err = reg.executeMFACancelEnroll(intruder, enrollArgs)
	assert.ErrorIs(t, err, iam.ErrMFAEnrollmentNotFound)
	_, err = reg.executeMFAVerifyChallenge(intruder, map[string]interface{}{"challenge_id": "challenge-b", "code": "000000"})
	assert.ErrorIs(t, err, iam.ErrMFAChallengeNotFound)

	_, err = reg.executeMFACancelEnroll(context.Background(), enrollArgs)
	var missing *tenantctx.MissingTenantError
	assert.ErrorAs(t, err, &missing, "a call with no tenant must be refused")

	stored, err := storage.GetEnrollment(context.Background(), enrollment.EnrollmentID)
	require.NoError(t, err, "refused tools must leave the enrollment in place")
	assert.Zero(t, stored.Attempts)

	_, err = reg.executeMFACancelEnroll(tenantctx.WithTenant(context.Background(), "tenant-b"), enrollArgs)
	require.NoError(t, err)
	_, err = storage.GetEnrollment(context.Background(), enrollment.EnrollmentID)
	assert.ErrorIs(t, err, iam.ErrMFAEnrollmentNotFound)
}

// TestMFAUserTools_NameOnlyCallerTenant proves the MCP MFA tools that take a
// tenant_id honour it only when it is the caller's own tenant: naming another
// tenant is refused before the MFA manager is reached, and the owning tenant
// still succeeds.
func TestMFAUserTools_NameOnlyCallerTenant(t *testing.T) {
	t.Parallel()

	mgr, err := iam.NewMFAManager(iam.WithMFAStorage(iam.NewMemoryMFAStorage()))
	require.NoError(t, err)
	reg := newMFATestRegistry(mgr)

	victimArgs := map[string]interface{}{
		"user_id":       "20002",
		"tenant_id":     "tenant-b",
		"session_token": "session-b",
		"code":          "00000000",
	}
	tools := map[string]func(context.Context, map[string]interface{}) (interface{}, error){
		"begin_enroll":           reg.executeMFABeginEnroll,
		"get_status":             reg.executeMFAGetStatus,
		"disable":                reg.executeMFADisable,
		"begin_challenge":        reg.executeMFABeginChallenge,
		"generate_backup_codes":  reg.executeMFAGenerateBackupCodes,
		"verify_backup_code":     reg.executeMFAVerifyBackupCode,
		"get_backup_code_status": reg.executeMFAGetBackupCodeStatus,
	}

	intruder := tenantctx.WithTenant(context.Background(), "tenant-a")
	for name, tool := range tools {
		_, err := tool(intruder, victimArgs)
		var crossTenant *tenantctx.CrossTenantError
		assert.ErrorAs(t, err, &crossTenant, "%s naming another tenant must be refused", name)
	}

	owner := tenantctx.WithTenant(context.Background(), "tenant-b")
	_, err = reg.executeMFABeginEnroll(owner, victimArgs)
	require.NoError(t, err)
	_, err = reg.executeMFAGetStatus(owner, victimArgs)
	require.NoError(t, err)
}
