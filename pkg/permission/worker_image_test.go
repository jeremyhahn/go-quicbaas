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

// TestWorkerImage_PermissionsAreCrossTenant pins the worker image refresh's
// two verbs as HOST verbs: cross-tenant, so only a system-tenant identity
// exercises them; reading to Admin and Operator, requesting a pass — which
// builds, smokes on a pooled credential and rebinds every tenant's projects —
// to Admin alone.
func TestWorkerImage_PermissionsAreCrossTenant(t *testing.T) {
	contributed := make(map[qrdbsdk.Permission]contribution)
	for _, c := range contributions {
		contributed[c.permission] = c
	}
	for _, perm := range []qrdbsdk.Permission{WorkerImageRead, WorkerImageRefresh} {
		c, ok := contributed[perm]
		require.Truef(t, ok, "%s is not contributed", perm)
		assert.Equalf(t, scopeCross, c.scope, "%s must be cross-tenant", perm)
		assert.Equalf(t, dutyOutsideSecurityOfficer, c.duty, "%s carries a governance or crypto duty", perm)
		assert.Contains(t, c.roles, qrdbsdk.PermissionRoleAdmin, perm)
	}
	assert.Contains(t, contributed[WorkerImageRead].roles, qrdbsdk.PermissionRoleOperator)
	assert.NotContains(t, contributed[WorkerImageRefresh].roles, qrdbsdk.PermissionRoleOperator,
		"requesting a pass rebinds every tenant's projects: not Operator's")

	Register()
	resolver, err := permscope.New()
	require.NoError(t, err)
	for _, perm := range []qrdbsdk.Permission{WorkerImageRead, WorkerImageRefresh} {
		got, err := resolver.Scope(context.Background(), perm)
		require.NoError(t, err, perm)
		assert.Equal(t, qrdbsdk.PermissionScopeCross, got, perm)
	}
}
