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

package permission

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/permscope"
)

// TestSpendReport_PermissionIsTenantScopedAndHeldByRosterReaders pins the
// spend report's own verb (go-quicbaas#885): self-scoped, so a tenant reads
// only its own report, and granted to exactly the roles that may read the
// tenant's pooled-account roster (agent_accounts:list).
func TestSpendReport_PermissionIsTenantScopedAndHeldByRosterReaders(t *testing.T) {
	contributed := make(map[qrdbsdk.Permission]contribution)
	for _, c := range contributions {
		contributed[c.permission] = c
	}
	c, ok := contributed[SpendReportRead]
	require.Truef(t, ok, "%s is not contributed", SpendReportRead)
	assert.Equal(t, scopeSelf, c.scope)
	assert.Equal(t, dutyOutsideSecurityOfficer, c.duty)
	assert.ElementsMatch(t, contributed[AgentAccountList].roles, c.roles,
		"the spend report reveals the account roster: its readers are the roster's readers")

	Register()
	resolver, err := permscope.New()
	require.NoError(t, err)
	got, err := resolver.Scope(context.Background(), SpendReportRead)
	require.NoError(t, err)
	assert.Equal(t, qrdbsdk.PermissionScopeSelf, got)

	assert.Contains(t, permissionSet(t, qrdbsdk.PermissionRoleAdmin), SpendReportRead)
	assert.Contains(t, permissionSet(t, qrdbsdk.PermissionRoleOperator), SpendReportRead)
}

// TestSpendReport_PermissionIsWithheldFromNonRosterRoles proves the report is
// not reachable through a role that may not read the account roster.
func TestSpendReport_PermissionIsWithheldFromNonRosterRoles(t *testing.T) {
	Register()
	for _, role := range []string{qrdbsdk.PermissionRoleAuditor, qrdbsdk.PermissionRoleSO} {
		assert.NotContainsf(t, permissionSet(t, role), SpendReportRead,
			"%s holds %s without holding the account roster", role, SpendReportRead)
	}
}
