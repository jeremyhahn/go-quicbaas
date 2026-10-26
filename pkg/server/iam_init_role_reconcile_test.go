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

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// reconcilingRoleStorage is a role storage that can reconcile built-ins: it
// records the tenants it was asked to converge and answers with report or err.
type reconcilingRoleStorage struct {
	iam.RoleStorage
	report  *iam.RoleReconcileReport
	err     error
	tenants []string
}

func (s *reconcilingRoleStorage) ReconcileBuiltInRoles(_ context.Context, tenantID string) (*iam.RoleReconcileReport, error) {
	s.tenants = append(s.tenants, tenantID)
	return s.report, s.err
}

func reconcilingStore(roles iam.RoleStorage) *iam.IdentityStore {
	return iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: iam.NewMemoryStorage(),
		RoleStorage: roles,
	})
}

// TestReconcileBuiltInRoles_ConvergesTheSystemTenant proves the boot pass
// converges the persisted system-tenant built-ins onto this build's
// permission set, so a data dir seeded before a permission rename gets the
// renamed verbs instead of keeping roles that grant names nothing checks.
func TestReconcileBuiltInRoles_ConvergesTheSystemTenant(t *testing.T) {
	roles := &reconcilingRoleStorage{
		RoleStorage: iam.NewMemoryStorage(),
		report: &iam.RoleReconcileReport{
			TenantID: qrdbsdk.SystemTenantID,
			Roles: []iam.RoleReconcileResult{{
				RoleID: AdminRoleID, Name: qrdbsdk.RoleNameAdmin,
				Added: []string{"user_authfactors:login"},
			}},
		},
	}

	report, err := reconcileBuiltInRoles(context.Background(), reconcilingStore(roles))

	require.NoError(t, err)
	assert.Equal(t, []string{qrdbsdk.SystemTenantID}, roles.tenants)
	require.True(t, report.Changed())
	assert.Equal(t, []string{"user_authfactors:login"}, report.Roles[0].Added)
}

// TestReconcileBuiltInRoles_SurfacesTheStoreFailure proves a refused or
// failed reconcile is returned, not swallowed: stale built-ins left in place
// must reach the boot log.
func TestReconcileBuiltInRoles_SurfacesTheStoreFailure(t *testing.T) {
	cause := &iam.IAMMessageError{Message: "barrier sealed"}
	roles := &reconcilingRoleStorage{RoleStorage: iam.NewMemoryStorage(), err: cause}

	_, err := reconcileBuiltInRoles(context.Background(), reconcilingStore(roles))

	require.Error(t, err)
	assert.ErrorIs(t, err, cause)
}

// TestReconcileBuiltInRoles_NoQRDBRolesIsANoOp proves a role storage that is
// not qrdb's — so carries no qrdb built-ins to drift — is not an error.
func TestReconcileBuiltInRoles_NoQRDBRolesIsANoOp(t *testing.T) {
	report, err := reconcileBuiltInRoles(context.Background(), reconcilingStore(iam.NewMemoryStorage()))

	require.NoError(t, err)
	assert.False(t, report.Changed())

	report, err = reconcileBuiltInRoles(context.Background(), nil)
	require.NoError(t, err)
	assert.False(t, report.Changed())
}
