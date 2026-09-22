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

package permscope

import (
	"context"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolver_Scope_ReadsTheProcessGlobalDeclaration proves the fallback
// this package relies on actually reaches go-qrdb's own declarations, using
// three permissions qrdb declares with three different scopes. If
// ResolveScope ever stopped falling through to the global layer, every
// answer here would become an error rather than a wrong scope — so the
// assertion cannot quietly degrade into "self for everything", which is the
// failure mode this package exists to detect elsewhere.
func TestResolver_Scope_ReadsTheProcessGlobalDeclaration(t *testing.T) {
	resolver, err := New()
	require.NoError(t, err)

	cases := []struct {
		permission qrdbsdk.Permission
		want       qrdbsdk.PermissionScope
	}{
		// The credential store has no tenant namespacing, so qrdb confines
		// the whole family to the system tenant.
		{"credentials:store", qrdbsdk.PermissionScopeCross},
		// A tenant's secret material is denied to the platform operator.
		{"secrets:read", qrdbsdk.PermissionScopeTenantOnly},
		// An ordinary self-scoped built-in, for contrast.
		{"keys:sign", qrdbsdk.PermissionScopeSelf},
	}
	for _, tc := range cases {
		t.Run(string(tc.permission), func(t *testing.T) {
			got, err := resolver.Scope(context.Background(), tc.permission)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestResolver_Scope_UndeclaredPermissionIsAnError proves the undeclared
// case surfaces as an error rather than as self-scope. That distinction is
// the whole point: go-qrdb#504 removed the self-scope default precisely
// because a permission nobody had decided on was admitted on the same terms
// as one deliberately confined, and a helper that re-invented the default
// would hide every omission it was built to find.
func TestResolver_Scope_UndeclaredPermissionIsAnError(t *testing.T) {
	resolver, err := New()
	require.NoError(t, err)

	got, err := resolver.Scope(context.Background(), "permscope:nobody_declared_this")
	require.Error(t, err)
	assert.IsType(t, &qrdbsdk.PermissionUndeclaredScopeError{}, err)
	assert.Equal(t, qrdbsdk.PermissionScopeSelf, got,
		"the zero value comes back with the error and must not be read as a decision")
}
